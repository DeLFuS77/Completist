// Package notify tells you about changes in your collection through Discord, Telegram or any
// webhook that accepts JSON: games that appear in your collection, sets you complete,
// damaged files found and new entries in the DATs. Only what is new since the last check is
// reported; the first check only records the current state.
//
// Adapted from Switch Library Manager Web by the same author.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DeLFuS77/completist/internal/collection"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
)

const (
	settingsKey = "notifications"
	stateFile   = "notifications.json"
	maxLines    = 20
)

// Options configure the channels and what is reported.
type Options struct {
	DiscordWebhookURL string `json:"discordWebhookUrl"`
	TelegramBotToken  string `json:"telegramBotToken"`
	TelegramChatID    string `json:"telegramChatId"`
	WebhookURL        string `json:"webhookUrl"`
	// language of the messages: en or es
	Language     string `json:"language"`
	NewGames     bool   `json:"newGames"`
	CompleteSets bool   `json:"completeSets"`
	Damaged      bool   `json:"damaged"`
	DatChanges   bool   `json:"datChanges"`
	// a game of the wishlist is in the collection now
	Wishlist bool `json:"wishlist"`
}

func DefaultOptions() Options {
	return Options{Language: "en", NewGames: true, CompleteSets: true, Damaged: true, DatChanges: false, Wishlist: true}
}

// Configured reports whether a channel is set.
func (o Options) Configured() bool {
	return o.DiscordWebhookURL != "" || (o.TelegramBotToken != "" && o.TelegramChatID != "") || o.WebhookURL != ""
}

var (
	// redirects are not followed: a webhook answering with one could send the request to an
	// address of the local network
	client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	discordPattern  = regexp.MustCompile(`^https://(discord\.com|discordapp\.com|ptb\.discord\.com|canary\.discord\.com)/api/webhooks/[0-9]+/[A-Za-z0-9_-]+$`)
	telegramToken   = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]{20,}$`)
	telegramChat    = regexp.MustCompile(`^(-?[0-9]+|@[A-Za-z0-9_]{5,})$`)
	webhookPattern  = regexp.MustCompile(`^https?://[^\s]+$`)
	telegramAPIBase = "https://api.telegram.org"
)

// FieldError tells which setting is wrong; the interface translates the message.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Validate checks the options.
func (o Options) Validate() []FieldError {
	errs := []FieldError{}
	if o.DiscordWebhookURL != "" && !discordPattern.MatchString(o.DiscordWebhookURL) {
		errs = append(errs, FieldError{"discordWebhookUrl", "Enter the webhook URL copied from Discord (https://discord.com/api/webhooks/...)."})
	}
	if (o.TelegramBotToken == "") != (o.TelegramChatID == "") {
		errs = append(errs, FieldError{"telegramChatId", "Telegram needs both the bot token and the chat ID."})
	} else if o.TelegramBotToken != "" {
		if !telegramToken.MatchString(o.TelegramBotToken) {
			errs = append(errs, FieldError{"telegramBotToken", "The bot token is not valid (as given by @BotFather)."})
		}
		if !telegramChat.MatchString(o.TelegramChatID) {
			errs = append(errs, FieldError{"telegramChatId", "The chat ID is not valid (a number or @channel)."})
		}
	}
	if o.WebhookURL != "" && !webhookPattern.MatchString(o.WebhookURL) {
		errs = append(errs, FieldError{"webhookUrl", "The URL is not valid."})
	}
	return errs
}

// Item is one thing to report.
type Item struct {
	Kind   string `json:"kind"` // new, wish, complete, damaged, dat, test
	System string `json:"system,omitempty"`
	Name   string `json:"name"`
	Count  int    `json:"count,omitempty"`
}

// Notifier remembers what was reported and sends the news.
type Notifier struct {
	Store      *store.Store
	Progress   *collection.Cache
	DataFolder string
	Log        *slog.Logger
	mutex      sync.Mutex
}

func (n *Notifier) Options() Options {
	o := DefaultOptions()
	n.Store.GetSetting(settingsKey, &o)
	return o
}

func (n *Notifier) SaveOptions(o Options) error {
	if o.Language != "es" {
		o.Language = "en"
	}
	return n.Store.SetSetting(settingsKey, o)
}

// state is what was true at the last check, by key.
type state map[string]bool

func (n *Notifier) loadState() state {
	data, err := os.ReadFile(filepath.Join(n.DataFolder, stateFile))
	if err != nil {
		return nil
	}
	s := state{}
	if json.Unmarshal(data, &s) != nil {
		return nil
	}
	return s
}

func (n *Notifier) saveState(s state) {
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	path := filepath.Join(n.DataFolder, stateFile)
	if os.WriteFile(path+".tmp", data, 0o600) == nil {
		os.Rename(path+".tmp", path)
	}
}

// current reads the owned games, complete sets and damaged files.
func (n *Notifier) current(ctx context.Context) (state, map[string]Item, error) {
	s := state{}
	items := map[string]Item{}
	rows, err := n.Store.DB().QueryContext(ctx, `SELECT DISTINCT g.system, g.name FROM files f JOIN roms r ON r.id = f.rom_id JOIN games g ON g.id = r.game_id`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var system, name string
		if err := rows.Scan(&system, &name); err != nil {
			rows.Close()
			return nil, nil, err
		}
		key := "new:" + system + ":" + name
		s[key] = true
		items[key] = Item{Kind: "new", System: system, Name: name}
	}
	rows.Close()
	rows, err = n.Store.DB().QueryContext(ctx, `SELECT system, path, entry FROM files WHERE status = 'bad'`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var system, path, entry string
		if err := rows.Scan(&system, &path, &entry); err != nil {
			rows.Close()
			return nil, nil, err
		}
		name := filepath.Base(path)
		if entry != "" {
			name += " › " + entry
		}
		key := "damaged:" + path + "\x00" + entry
		s[key] = true
		items[key] = Item{Kind: "damaged", System: system, Name: name}
	}
	rows.Close()
	wishes, err := collection.Wishlist(ctx, n.Store)
	if err != nil {
		return nil, nil, err
	}
	for _, w := range wishes {
		if w.Owned {
			key := "wish:" + w.System + ":" + w.Name
			s[key] = true
			items[key] = Item{Kind: "wish", System: w.System, Name: w.Name}
		}
	}
	all, err := n.Progress.All(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range all {
		if p.HasDat && p.Total > 0 && p.Missing == 0 {
			key := "complete:" + p.System.ID
			s[key] = true
			items[key] = Item{Kind: "complete", System: p.System.ID, Name: p.System.Short, Count: p.Total}
		}
	}
	return s, items, nil
}

// Record saves the current state without reporting it, after a DAT changed: files that match
// a new DAT are not new in the collection.
func (n *Notifier) Record(ctx context.Context) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	s, _, err := n.current(ctx)
	if err != nil {
		n.Log.Warn("the notification state could not be read", "error", err)
		return
	}
	n.saveState(s)
}

// AfterScan reports what is new since the last check.
func (n *Notifier) AfterScan(ctx context.Context) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	o := n.Options()
	s, items, err := n.current(ctx)
	if err != nil {
		n.Log.Warn("the notification state could not be read", "error", err)
		return
	}
	previous := n.loadState()
	if previous == nil || !o.Configured() {
		n.saveState(s)
		return
	}
	news := []Item{}
	keys := make([]string, 0, len(s))
	for key := range s {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// a game of the wishlist is told once, as such, and not again as new
	wished := map[string]bool{}
	if o.Wishlist {
		for _, key := range keys {
			if item := items[key]; item.Kind == "wish" && !previous[key] {
				wished[item.System+":"+item.Name] = true
			}
		}
	}
	for _, key := range keys {
		if previous[key] {
			continue
		}
		item := items[key]
		if item.Kind == "new" && wished[item.System+":"+item.Name] {
			continue
		}
		if (item.Kind == "new" && o.NewGames) || (item.Kind == "wish" && o.Wishlist) || (item.Kind == "complete" && o.CompleteSets) || (item.Kind == "damaged" && o.Damaged) {
			news = append(news, item)
		}
	}
	// the wishlist first: the best news
	sort.SliceStable(news, func(i, j int) bool { return news[i].Kind == "wish" && news[j].Kind != "wish" })
	if len(news) > 0 {
		if err := Send(o, news); err != nil {
			// keep the previous state, so the news are reported after the next scan
			n.Log.Warn("the notification could not be sent", "error", err)
			return
		}
		n.Log.Info("notification sent", "items", len(news))
	}
	n.saveState(s)
}

// AfterDatUpdate reports DATs that gained entries, and records the new state silently.
func (n *Notifier) AfterDatUpdate(ctx context.Context, changes []store.DatChange) {
	o := n.Options()
	if o.Configured() && o.DatChanges {
		items := []Item{}
		for _, c := range changes {
			if c.Previous != "" && c.Added > 0 {
				name := c.System
				if s, ok := systems.ByID(c.System); ok {
					name = s.Short
				}
				items = append(items, Item{Kind: "dat", System: c.System, Name: name, Count: c.Added})
			}
		}
		if len(items) > 0 {
			if err := Send(o, items); err != nil {
				n.Log.Warn("the notification could not be sent", "error", err)
			}
		}
	}
	n.Record(ctx)
}

// ---- messages ----

var texts = map[string]map[string]string{
	"en": {
		"title":    "Completist: %d news",
		"new":      "New in your collection: %s",
		"wish":     "From your wishlist, now in your collection: %s",
		"complete": "Set complete: %s (%d games)",
		"damaged":  "Damaged or modified file: %s",
		"dat":      "%s: %d new entries in the DAT",
		"test":     "Test notification: notifications work.",
		"more":     "and %d more",
	},
	"es": {
		"title":    "Completist: %d novedades",
		"new":      "Nuevo en tu colección: %s",
		"wish":     "De tu lista de deseos, ya en tu colección: %s",
		"complete": "Set completo: %s (%d juegos)",
		"damaged":  "Archivo dañado o modificado: %s",
		"dat":      "%s: %d entradas nuevas en el DAT",
		"test":     "Notificación de prueba: las notificaciones funcionan.",
		"more":     "y %d más",
	},
}

// Text builds the title and the body of a message.
func Text(lang string, items []Item) (string, string) {
	t, ok := texts[lang]
	if !ok {
		t = texts["en"]
	}
	lines := []string{}
	for i, item := range items {
		if i == maxLines {
			lines = append(lines, fmt.Sprintf(t["more"], len(items)-maxLines))
			break
		}
		var line string
		switch item.Kind {
		case "complete":
			line = fmt.Sprintf(t["complete"], item.Name, item.Count)
		case "dat":
			line = fmt.Sprintf(t["dat"], item.Name, item.Count)
		case "test":
			line = t["test"]
		default:
			line = fmt.Sprintf(t[item.Kind], item.Name)
		}
		lines = append(lines, "• "+line)
	}
	return fmt.Sprintf(t["title"], len(items)), strings.Join(lines, "\n")
}

// Send sends the items to every configured channel.
func Send(o Options, items []Item) error {
	title, body := Text(o.Language, items)
	errs := []string{}
	if o.DiscordWebhookURL != "" {
		if err := postJSON(o.DiscordWebhookURL, map[string]string{"content": "**" + title + "**\n" + body}); err != nil {
			errs = append(errs, "Discord: "+err.Error())
		}
	}
	if o.TelegramBotToken != "" && o.TelegramChatID != "" {
		url := telegramAPIBase + "/bot" + o.TelegramBotToken + "/sendMessage"
		if err := postJSON(url, map[string]string{"chat_id": o.TelegramChatID, "text": title + "\n" + body}); err != nil {
			errs = append(errs, "Telegram: "+err.Error())
		}
	}
	if o.WebhookURL != "" {
		if err := postJSON(o.WebhookURL, map[string]any{"title": title, "message": body, "items": items}); err != nil {
			errs = append(errs, "Webhook: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func postJSON(url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// the error may contain the URL with a secret (Discord, Telegram): not shown
		return errors.New("request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server answered %d", resp.StatusCode)
	}
	return nil
}
