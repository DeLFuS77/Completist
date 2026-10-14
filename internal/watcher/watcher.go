// Package watcher rescans the libraries when their files change. Local folders report changes
// right away; every folder is also checked every few minutes, which covers network shares
// that do not report them.
//
// Adapted from Switch Library Manager Web by the same author.
package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// network shares do not report changes, so the folders are also checked regularly
	defaultPollInterval = 2 * time.Minute
	// wait for this long without file events before checking, so a copy can finish
	quietPeriod = 10 * time.Second
	// a folder must look the same twice in a row, this far apart, before it is scanned
	stableDelay = 3 * time.Second
	// the fingerprint of the folders at the last scan of the watcher, kept between runs
	fingerprintFile = "folders.fingerprint"
)

// Config is what the watcher needs from the app.
type Config struct {
	DataFolder string
	// the folders of the libraries, read at every check so added libraries are followed
	Folders func() []string
	// starts a scan; false when one could not start (another is running)
	Scan func() bool
	// how often every folder is checked; 0 for the default
	PollInterval time.Duration
	Log          *slog.Logger
}

// Watcher follows the library folders.
type Watcher struct {
	cfg       Config
	tree      *dirTree
	notify    *fsnotify.Watcher
	watched   map[string]struct{}
	scanned   uint64
	lastCheck uint64
	dirs      []string
}

// Start watches the folders until the context ends.
func Start(ctx context.Context, cfg Config) {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	notify, err := fsnotify.NewWatcher()
	if err != nil {
		// polling still works
		cfg.Log.Warn("file change notifications are not available, the folders are only checked regularly", "every", cfg.PollInterval, "error", err)
	}
	w := &Watcher{cfg: cfg, tree: newDirTree(), notify: notify, watched: map[string]struct{}{}}
	// files changed while the app was stopped are picked up by the first check
	w.lastCheck = w.fingerprint()
	// folders that did not change since the last scan of the previous run need no scan
	w.scanned = w.savedFingerprint()
	w.updateWatches()
	go w.run(ctx)
}

func (w *Watcher) fingerprint() uint64 {
	fingerprint, dirs := w.tree.refresh(w.cfg.Folders())
	w.dirs = dirs
	return fingerprint
}

func (w *Watcher) run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	quiet := time.NewTimer(time.Hour)
	quiet.Stop()
	defer func() {
		if w.notify != nil {
			w.notify.Close()
		}
	}()

	var events chan fsnotify.Event
	var errs chan error
	if w.notify != nil {
		events = w.notify.Events
		errs = w.notify.Errors
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if event.Has(fsnotify.Create) {
				// new sub-folders are watched too
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					w.add(event.Name)
				}
			}
			quiet.Reset(quietPeriod)
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			w.cfg.Log.Debug("folder watcher error", "error", err)
		case <-quiet.C:
			w.checkStable()
		case <-ticker.C:
			w.checkPolled()
			w.updateWatches()
		}
	}
}

// checkStable scans if the folders changed and stopped changing.
func (w *Watcher) checkStable() {
	first := w.fingerprint()
	time.Sleep(stableDelay)
	second := w.fingerprint()
	w.lastCheck = second
	if first == second {
		w.scanIfChanged(second)
	}
}

// checkPolled scans if the folders changed since the last scan and look the same as at the
// previous check, so files still being copied are not scanned half-written.
func (w *Watcher) checkPolled() {
	current := w.fingerprint()
	previous := w.lastCheck
	w.lastCheck = current
	if current == previous {
		w.scanIfChanged(current)
	}
}

func (w *Watcher) scanIfChanged(fingerprint uint64) {
	if fingerprint == w.scanned {
		return
	}
	if w.cfg.Scan() {
		w.scanned = fingerprint
		w.saveFingerprint(fingerprint)
		w.cfg.Log.Info("library folders changed, scanning")
	}
	// if a scan is running, the next check tries again
}

// updateWatches follows the folders found by the last check.
func (w *Watcher) updateWatches() {
	if w.notify == nil {
		return
	}
	wanted := map[string]struct{}{}
	for _, dir := range w.dirs {
		wanted[dir] = struct{}{}
	}
	for path := range w.watched {
		if _, ok := wanted[path]; !ok {
			w.notify.Remove(path)
			delete(w.watched, path)
		}
	}
	for path := range wanted {
		w.add(path)
	}
}

func (w *Watcher) add(path string) {
	if _, ok := w.watched[path]; ok {
		return
	}
	if err := w.notify.Add(path); err != nil {
		w.cfg.Log.Debug("not watching a folder", "path", path, "error", err)
		return
	}
	w.watched[path] = struct{}{}
}

func (w *Watcher) savedFingerprint() uint64 {
	data, err := os.ReadFile(filepath.Join(w.cfg.DataFolder, fingerprintFile))
	if err != nil {
		return 0
	}
	fingerprint, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return fingerprint
}

func (w *Watcher) saveFingerprint(fingerprint uint64) {
	path := filepath.Join(w.cfg.DataFolder, fingerprintFile)
	if err := os.WriteFile(path, []byte(strconv.FormatUint(fingerprint, 10)), 0o644); err != nil {
		w.cfg.Log.Debug("the fingerprint of the folders could not be saved", "error", err)
	}
}
