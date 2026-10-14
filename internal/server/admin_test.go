package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeLFuS77/completist/internal/activity"
	"github.com/DeLFuS77/completist/internal/auth"
	"github.com/DeLFuS77/completist/internal/backup"
	"github.com/DeLFuS77/completist/internal/collection"
	"github.com/DeLFuS77/completist/internal/notify"
	"github.com/DeLFuS77/completist/internal/scanner"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/tasks"
	"github.com/DeLFuS77/completist/internal/watcher"
)

// fullFixture is a server with the activity log, backups and notifications.
type fullFixture struct {
	*fixture
	data     string
	store    *store.Store
	notifier *notify.Notifier
	backups  *backup.Manager
	activity *activity.Log
}

func newFullFixture(t *testing.T) *fullFixture {
	t.Helper()
	data := t.TempDir()
	st, err := store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a, err := auth.New(data)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	act := activity.Load(data)
	a.OnAction = act.Add
	taskLog := tasks.Load("")
	progress := collection.NewCache(st)
	n := &notify.Notifier{Store: st, Progress: progress, DataFolder: data, Log: log}
	sc := scanner.New(st, taskLog, 2)
	sc.OnChange = func() {
		progress.Invalidate()
		n.AfterScan(context.Background())
	}
	b := &backup.Manager{DataFolder: data, Store: st, Auth: a, Activity: act}
	b.OnRestore = progress.Invalidate
	srv := &Server{Store: st, Tasks: taskLog, Scanner: sc, Progress: progress, Auth: a, Log: log, Activity: act, Backups: b, Notifier: n}
	return &fullFixture{fixture: &fixture{t: t, handler: srv.Handler(), scanner: sc, library: t.TempDir()}, data: data, store: st, notifier: n, backups: b, activity: act}
}

func gbDat(games map[string][]byte) string {
	var dat strings.Builder
	dat.WriteString("clrmamepro (\n\tname \"Nintendo - Game Boy\"\n)\n")
	for name, b := range games {
		dat.WriteString("game (\n\tname \"" + name + "\"\n" + romLine(name+".gb", b) + ")\n")
	}
	return dat.String()
}

func TestActivityIsRecorded(t *testing.T) {
	f := newFullFixture(t)
	f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	f.do("POST", "/api/dats/import", gbDat(map[string][]byte{"A (World)": content(1, 64)}))
	f.do("PUT", "/api/preferences", map[string]any{"defaultMode": "full", "modes": map[string]string{}, "regions": []string{"World"}})
	f.do("POST", "/api/users", map[string]any{"name": "admin", "password": "a long secret phrase"})

	got := []string{}
	for _, e := range f.activity.List() {
		got = append(got, e.Action)
	}
	want := []string{activity.UserCreated, activity.CountingChanged, activity.DatImported, activity.LibraryAdded}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("activity: %v", got)
	}
	// the first user is recorded with its own name
	if f.activity.List()[0].User != "admin" {
		t.Fatalf("user: %+v", f.activity.List()[0])
	}
	// administrators only
	f.do("POST", "/api/users", map[string]any{"name": "viewer", "password": "another long phrase", "role": "viewer"})
	if rec, _ := f.do("GET", "/api/activity", nil); rec.Code != 401 {
		t.Fatalf("without login: %d", rec.Code)
	}
}

func TestBackupRoundTrip(t *testing.T) {
	f := newFullFixture(t)
	f.do("POST", "/api/dats/import", gbDat(map[string][]byte{"A (World)": content(1, 64), "B (World)": content(2, 64)}))
	f.do("PUT", "/api/preferences", map[string]any{"defaultMode": "official", "modes": map[string]string{}, "regions": []string{"Japan"}})

	rec, out := f.do("POST", "/api/backups", nil)
	if rec.Code != 201 {
		t.Fatalf("make: %d %s", rec.Code, rec.Body)
	}
	name := out["name"].(string)
	if _, list := f.do("GET", "/api/backups", nil); list == nil {
		t.Fatal("list")
	}

	// change everything, then restore
	f.do("DELETE", "/api/dats/gb", nil)
	f.do("PUT", "/api/preferences", map[string]any{"defaultMode": "full", "modes": map[string]string{}, "regions": []string{"USA"}})
	if _, sys := f.do("GET", "/api/systems/gb", nil); sys["hasDat"] == true {
		t.Fatal("the DAT should be gone")
	}
	if rec, _ := f.do("POST", "/api/backups/"+name+"/restore", nil); rec.Code != 200 {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body)
	}
	_, sys := f.do("GET", "/api/systems/gb", nil)
	if sys["hasDat"] != true || sys["total"] != 2.0 || sys["mode"] != "official" {
		t.Fatalf("after restore: %v", sys)
	}

	// a backup downloaded and uploaded again
	req := httptest.NewRequest("GET", "/api/backups/download", nil)
	req.RemoteAddr, req.Host = "127.0.0.1:1", "localhost"
	dl := httptest.NewRecorder()
	f.handler.ServeHTTP(dl, req)
	if dl.Code != 200 || dl.Body.Len() == 0 {
		t.Fatalf("download: %d", dl.Code)
	}
	if rec, _ := f.do("POST", "/api/backups/restore", dl.Body.String()); rec.Code != 200 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	// something else is refused
	if rec, _ := f.do("POST", "/api/backups/restore", "not a zip"); rec.Code != 400 {
		t.Fatalf("garbage: %d", rec.Code)
	}
	if rec, _ := f.do("POST", "/api/backups/..%2Fcompletist.db/restore", nil); rec.Code != 404 {
		t.Fatalf("bad name: %d", rec.Code)
	}
}

func TestOldBackupsAreRemoved(t *testing.T) {
	f := newFullFixture(t)
	day := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		info, err := f.backups.Make(context.Background(), day.AddDate(0, 0, 7*i))
		if err != nil {
			t.Fatal(err)
		}
		// the file times decide which ones are the oldest
		when := day.AddDate(0, 0, 7*i)
		os.Chtimes(filepath.Join(f.data, backup.Folder, info.Name), when, when)
	}
	if n := len(f.backups.List()); n != 5 {
		t.Fatalf("kept %d", n)
	}
	if f.backups.Due(day.AddDate(0, 0, 7*6+1)) {
		t.Fatal("a backup was made yesterday")
	}
}

func TestNotifications(t *testing.T) {
	var mutex sync.Mutex
	received := []map[string]any{}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mutex.Lock()
		received = append(received, body)
		mutex.Unlock()
	}))
	defer hook.Close()
	messages := func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		out := []string{}
		for _, m := range received {
			out = append(out, m["message"].(string))
		}
		return out
	}

	f := newFullFixture(t)
	if rec, _ := f.do("PUT", "/api/notifications", map[string]any{"webhookUrl": "not a url"}); rec.Code != 400 {
		t.Fatalf("invalid url: %d", rec.Code)
	}
	if rec, _ := f.do("PUT", "/api/notifications", map[string]any{"webhookUrl": hook.URL, "language": "es", "newGames": true, "completeSets": true, "damaged": true}); rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := f.do("POST", "/api/notifications/test", nil); rec.Code != 200 || len(messages()) != 1 {
		t.Fatalf("test: %d %v", rec.Code, messages())
	}

	a, b := content(21, 64), content(22, 64)
	f.do("POST", "/api/dats/import", gbDat(map[string][]byte{"A (World)": a, "B (World)": b}))
	f.do("PUT", "/api/preferences", map[string]any{"defaultMode": "full", "modes": map[string]string{}, "regions": []string{"World"}})
	f.write("gb/A (World).gb", a)
	f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	f.scanner.Run(context.Background(), "test")
	// the first scan after the DAT reports the game found
	if m := messages(); len(m) != 2 || !strings.Contains(m[1], "Nuevo en tu colección: A (World)") {
		t.Fatalf("first: %v", m)
	}
	// nothing new, nothing sent
	f.scanner.Run(context.Background(), "test")
	if len(messages()) != 2 {
		t.Fatalf("repeat: %v", messages())
	}
	// the last game completes the set, and a damaged file appears
	f.write("gb/B (World).gb", b)
	f.write("gb/B (World) copy/B (World).gb", content(98, 64))
	f.scanner.Run(context.Background(), "test")
	m := messages()
	if len(m) != 3 || !strings.Contains(m[2], "Nuevo en tu colección: B (World)") || !strings.Contains(m[2], "Set completo: Game Boy (2 juegos)") || !strings.Contains(m[2], "Archivo dañado o modificado: B (World).gb") {
		t.Fatalf("third: %v", m)
	}
	// a viewer cannot read the settings with their secrets
	f.do("POST", "/api/users", map[string]any{"name": "admin", "password": "a long secret phrase"})
	if rec, _ := f.do("GET", "/api/notifications", nil); rec.Code != 401 {
		t.Fatalf("without login: %d", rec.Code)
	}
}

func TestWatcherScansChangedFolders(t *testing.T) {
	f := newFullFixture(t)
	f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	f.scanner.Run(context.Background(), "test")
	scans := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher.Start(ctx, watcher.Config{
		DataFolder:   f.data,
		Folders:      func() []string { return []string{f.library} },
		Scan:         func() bool { scans <- struct{}{}; return true },
		PollInterval: time.Hour,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	f.write("gb/new.gb", bytes.Repeat([]byte{1}, 64))
	select {
	case <-scans:
	case <-time.After(30 * time.Second):
		t.Fatal("no scan after a file was added")
	}
}
