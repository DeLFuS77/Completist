// Package covers keeps the box art, screenshots and title screens of the games, downloaded
// once from libretro-thumbnails and then served from the data folder.
package covers

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Folder is where the images are kept, inside the data folder.
const Folder = "covers"

// DefaultBaseURL is where the images come from; COMPLETIST_COVERS_URL replaces it, for a mirror.
const DefaultBaseURL = "https://thumbnails.libretro.com/"

// RetryAfter is how long a game without an image waits before it is looked up again.
const RetryAfter = 7 * 24 * time.Hour

// maxSize keeps a wrong answer from filling the disk; real images are well under 1 MB.
const maxSize = 8 << 20

// Kinds are the images a game can have, with the folder libretro keeps them in.
var Kinds = map[string]string{
	"boxart": "Named_Boxarts",
	"snap":   "Named_Snaps",
	"title":  "Named_Titles",
}

var (
	// ErrNotFound means no image of that kind exists for the game.
	ErrNotFound = errors.New("no image")
	// ErrUnavailable means the image server could not be reached; nothing is remembered.
	ErrUnavailable = errors.New("the image server is not available")
)

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// How box art is kept. Screenshots and title screens are always kept as they come: they are
// small, and pixel art would lose its sharp edges in a JPEG.
const (
	// the PNG as libretro has it
	ModeOriginal = "original"
	// a JPEG of quality 92, with the same pixels: about a quarter of the size, with no
	// difference to see at the size covers are shown
	ModeHigh = "high"
	// a JPEG of quality 80: about a sixth of the size; a close look shows some softness
	ModeSmall = "small"
)

var Modes = []string{ModeOriginal, ModeHigh, ModeSmall}

func ValidMode(m string) bool {
	return m == ModeOriginal || m == ModeHigh || m == ModeSmall
}

func quality(mode string) int {
	switch mode {
	case ModeHigh:
		return 92
	case ModeSmall:
		return 80
	}
	return 0
}

// Cache downloads images on first use and keeps them.
type Cache struct {
	DataFolder string
	BaseURL    string
	Client     *http.Client
	Log        *slog.Logger
	// how box art is kept; nil keeps the original
	Mode func() string

	once       sync.Once
	slots      chan struct{}
	mutex      sync.Mutex
	inFlight   map[string]*call
	converting bool
}

type call struct {
	done chan struct{}
	path string
	err  error
}

func (c *Cache) init() {
	c.once.Do(func() {
		// a page full of covers asks for dozens at once; a few downloads at a time are enough
		c.slots = make(chan struct{}, 6)
		c.inFlight = map[string]*call{}
		if c.BaseURL == "" {
			c.BaseURL = DefaultBaseURL
		}
		if !strings.HasSuffix(c.BaseURL, "/") {
			c.BaseURL += "/"
		}
		if c.Client == nil {
			c.Client = &http.Client{Timeout: 30 * time.Second}
		}
		if c.Log == nil {
			c.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
		}
	})
}

// Safe is the file name libretro gives a game: these characters become an underscore.
func Safe(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("&*/:`<>?\\|\"", r) {
			return '_'
		}
		return r
	}, name)
}

// file is where an image is kept, without its extension: a hash of the name, so any name is a
// valid file name. It is a .png, or a .jpg for box art kept smaller.
func (c *Cache) file(system, kind, name string) string {
	sum := sha1.Sum([]byte(name))
	return filepath.Join(c.DataFolder, Folder, system, kind, hex.EncodeToString(sum[:]))
}

func existing(base string) string {
	for _, ext := range []string{".png", ".jpg"} {
		if _, err := os.Stat(base + ext); err == nil {
			return base + ext
		}
	}
	return ""
}

func (c *Cache) mode() string {
	if c.Mode == nil {
		return ModeOriginal
	}
	return c.Mode()
}

// Get returns the file of an image, downloading it if needed. names are tried in order (the
// game, then its parent); the first that exists is kept under the first name.
func (c *Cache) Get(ctx context.Context, system, libretro, kind string, names []string) (string, error) {
	c.init()
	folder, ok := Kinds[kind]
	if !ok || libretro == "" || len(names) == 0 {
		return "", ErrNotFound
	}
	path := c.file(system, kind, names[0])
	if found := existing(path); found != "" {
		return found, nil
	}
	if info, err := os.Stat(path + ".missing"); err == nil && time.Since(info.ModTime()) < RetryAfter {
		return "", ErrNotFound
	}

	// one download per image, however many ask for it
	c.mutex.Lock()
	if cl, ok := c.inFlight[path]; ok {
		c.mutex.Unlock()
		select {
		case <-cl.done:
			return cl.path, cl.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	cl := &call{done: make(chan struct{})}
	c.inFlight[path] = cl
	c.mutex.Unlock()

	// the download goes on if the page that asked is closed, so the next visit finds it
	cl.path, cl.err = c.download(path, kind, libretro, folder, names)
	c.mutex.Lock()
	delete(c.inFlight, path)
	c.mutex.Unlock()
	close(cl.done)

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		return cl.path, cl.err
	}
}

func (c *Cache) download(path, kind, libretro, folder string, names []string) (string, error) {
	c.slots <- struct{}{}
	defer func() { <-c.slots }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	for _, name := range names {
		u := c.BaseURL + url.PathEscape(libretro) + "/" + folder + "/" + url.PathEscape(Safe(name)) + ".png"
		data, err := c.fetch(ctx, u)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			c.Log.Warn("cover download failed", "url", u, "error", err)
			return "", ErrUnavailable
		}
		ext := ".png"
		if kind == "boxart" {
			if small, ok := shrink(data, c.mode()); ok {
				data, ext = small, ".jpg"
			}
		}
		if err := writeFile(path+ext, data); err != nil {
			return "", err
		}
		os.Remove(path + ".missing")
		return path + ext, nil
	}
	// remembered, so the server is not asked again on every visit
	if err := writeFile(path+".missing", nil); err != nil {
		return "", err
	}
	return "", ErrNotFound
}

func (c *Cache) fetch(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, fmt.Errorf("the image is larger than %d bytes", maxSize)
	}
	// only images are kept and served; a page of an error or a captive portal is not
	if !bytes.HasPrefix(data, pngMagic) {
		return nil, ErrNotFound
	}
	return data, nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// shrink makes a JPEG of a box art. ok is false when it is better kept as it is: the mode
// keeps originals, the image has transparent parts, or the JPEG would not be much smaller.
func shrink(data []byte, mode string) ([]byte, bool) {
	q := quality(mode)
	if q == 0 {
		return nil, false
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	if o, ok := img.(interface{ Opaque() bool }); ok && !o.Opaque() {
		return nil, false
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: q}); err != nil {
		return nil, false
	}
	if out.Len() > len(data)*9/10 {
		return nil, false
	}
	return out.Bytes(), true
}

// Usage is how many images are kept and how much space they take.
type Usage struct {
	Images     int    `json:"images"`
	Bytes      int64  `json:"bytes"`
	Mode       string `json:"mode"`
	Converting bool   `json:"converting"`
}

// Usage walks the folder of images.
func (c *Cache) Usage() Usage {
	c.init()
	u := Usage{Mode: c.mode()}
	c.mutex.Lock()
	u.Converting = c.converting
	c.mutex.Unlock()
	filepath.WalkDir(filepath.Join(c.DataFolder, Folder), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg")) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			u.Images++
			u.Bytes += info.Size()
		}
		return nil
	})
	return u
}

// Clear removes every kept image; they are downloaded again when needed.
func (c *Cache) Clear() error {
	return os.RemoveAll(filepath.Join(c.DataFolder, Folder))
}

// Convert makes the box art already kept smaller, as the mode says, in the background. Images
// kept smaller are not made original again: removing them downloads them again.
func (c *Cache) Convert() {
	c.init()
	mode := c.mode()
	if quality(mode) == 0 {
		return
	}
	c.mutex.Lock()
	if c.converting {
		c.mutex.Unlock()
		return
	}
	c.converting = true
	c.mutex.Unlock()
	go func() {
		defer func() {
			c.mutex.Lock()
			c.converting = false
			c.mutex.Unlock()
		}()
		var before, after int64
		count := 0
		filepath.WalkDir(filepath.Join(c.DataFolder, Folder), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Base(filepath.Dir(path)) != "boxart" || !strings.HasSuffix(path, ".png") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			small, ok := shrink(data, mode)
			if !ok {
				return nil
			}
			base := strings.TrimSuffix(path, ".png")
			if writeFile(base+".jpg", small) == nil {
				os.Remove(path)
				before += int64(len(data))
				after += int64(len(small))
				count++
			}
			return nil
		})
		if count > 0 {
			c.Log.Info("box art made smaller", "images", count, "before", before, "after", after)
		}
	}()
}
