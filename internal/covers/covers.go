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
	"io"
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

// Cache downloads images on first use and keeps them.
type Cache struct {
	DataFolder string
	BaseURL    string
	Client     *http.Client
	Log        *slog.Logger

	once     sync.Once
	slots    chan struct{}
	mutex    sync.Mutex
	inFlight map[string]*call
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

// file is where an image is kept: a hash of the name, so any name is a valid file name.
func (c *Cache) file(system, kind, name string) string {
	sum := sha1.Sum([]byte(name))
	return filepath.Join(c.DataFolder, Folder, system, kind, hex.EncodeToString(sum[:])+".png")
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
	if _, err := os.Stat(path); err == nil {
		return path, nil
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
	cl.path, cl.err = c.download(path, libretro, folder, names)
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

func (c *Cache) download(path, libretro, folder string, names []string) (string, error) {
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
		if err := writeFile(path, data); err != nil {
			return "", err
		}
		os.Remove(path + ".missing")
		return path, nil
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

// Usage is how many images are kept and how much space they take.
type Usage struct {
	Images int   `json:"images"`
	Bytes  int64 `json:"bytes"`
}

// Usage walks the folder of images.
func (c *Cache) Usage() Usage {
	var u Usage
	filepath.WalkDir(filepath.Join(c.DataFolder, Folder), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".png") {
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
