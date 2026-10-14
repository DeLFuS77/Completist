// Package backup saves and restores the state of Completist: the database (libraries, DATs,
// files, settings), the users and the activity log, in one ZIP file. A copy is also made
// every week in the data folder, and the last ones are kept, so a mistake can be undone
// without having made a backup by hand.
//
// Adapted from Switch Library Manager Web by the same author.
package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/DeLFuS77/completist/internal/activity"
	"github.com/DeLFuS77/completist/internal/auth"
	"github.com/DeLFuS77/completist/internal/store"
)

const (
	Folder    = "backups"
	Every     = 7 * 24 * time.Hour
	kept      = 5
	dbEntry   = "completist.db"
	maxSize   = 1 << 30
	jsonLimit = 16 << 20
)

// names of the copies in the data folder: one per day at most
var namePattern = regexp.MustCompile(`^completist-backup-\d{4}-\d{2}-\d{2}\.zip$`)

// errors shown to the user, translated by the interface
var (
	ErrInvalid  = errors.New("The file is not a backup of Completist.")
	ErrDamaged  = errors.New("The backup contains a damaged file.")
	ErrNewer    = errors.New("The backup was made by a newer version of Completist.")
	ErrNoAdmin  = errors.New("The backup has users but no administrator.")
	ErrNotFound = errors.New("The backup does not exist.")
)

// Known reports whether an error is one of the messages above.
func Known(err error) bool {
	for _, e := range []error{ErrInvalid, ErrDamaged, ErrNewer, ErrNoAdmin, ErrNotFound} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// Manager makes and restores backups.
type Manager struct {
	DataFolder string
	Store      *store.Store
	Auth       *auth.Auth
	Activity   *activity.Log
	// called after a restore, to drop cached results and scan again
	OnRestore func()
}

// Info describes a copy in the data folder.
type Info struct {
	Name string    `json:"name"`
	Size int64     `json:"size"`
	Time time.Time `json:"time"`
}

func (m *Manager) folder() string {
	return filepath.Join(m.DataFolder, Folder)
}

// Write writes a backup to w.
func (m *Manager) Write(ctx context.Context, w io.Writer) error {
	tmp, err := os.MkdirTemp(m.DataFolder, ".backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	snapshot := filepath.Join(tmp, dbEntry)
	if err := m.Store.Snapshot(ctx, snapshot); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	archive := zip.NewWriter(w)
	add := func(name, path string) error {
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer f.Close()
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return err
		}
		_, err = io.Copy(entry, f)
		return err
	}
	if err := add(dbEntry, snapshot); err != nil {
		return err
	}
	// the session key and caches are never included
	for _, name := range []string{auth.UsersFile, activity.Filename} {
		if err := add(name, filepath.Join(m.DataFolder, name)); err != nil {
			return err
		}
	}
	return archive.Close()
}

// List returns the copies in the data folder, newest first.
func (m *Manager) List() []Info {
	out := []Info{}
	entries, err := os.ReadDir(m.folder())
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !namePattern.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Name: e.Name(), Size: info.Size(), Time: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

// Due reports whether the last copy is a week old, or there is none.
func (m *Manager) Due(now time.Time) bool {
	list := m.List()
	return len(list) == 0 || now.Sub(list[0].Time) >= Every
}

// Make saves a copy in the data folder and removes the oldest ones.
func (m *Manager) Make(ctx context.Context, now time.Time) (Info, error) {
	if err := os.MkdirAll(m.folder(), 0o700); err != nil {
		return Info{}, err
	}
	name := "completist-backup-" + now.Format("2006-01-02") + ".zip"
	path := filepath.Join(m.folder(), name)
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return Info{}, err
	}
	if err := m.Write(ctx, f); err != nil {
		f.Close()
		os.Remove(tmp)
		return Info{}, err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return Info{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return Info{}, err
	}
	list := m.List()
	for _, old := range list[min(len(list), kept):] {
		os.Remove(filepath.Join(m.folder(), old.Name))
	}
	info, _ := os.Stat(path)
	return Info{Name: name, Size: info.Size(), Time: info.ModTime()}, nil
}

// Path returns the path of a copy by name, or "" if there is none.
func (m *Manager) Path(name string) string {
	if !namePattern.MatchString(name) {
		return ""
	}
	path := filepath.Join(m.folder(), name)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return ""
	}
	return path
}

// RestoreFile restores a backup file: a copy of the data folder or an uploaded one.
func (m *Manager) RestoreFile(ctx context.Context, path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return ErrInvalid
	}
	defer r.Close()
	tmp, err := os.MkdirTemp(m.DataFolder, ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	files := map[string]string{}
	for _, e := range r.File {
		switch e.Name {
		case dbEntry, auth.UsersFile, activity.Filename:
		default:
			continue
		}
		limit := int64(jsonLimit)
		if e.Name == dbEntry {
			limit = maxSize
		}
		if e.UncompressedSize64 > uint64(limit) {
			return ErrDamaged
		}
		src, err := e.Open()
		if err != nil {
			return ErrDamaged
		}
		dst := filepath.Join(tmp, e.Name)
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			src.Close()
			return err
		}
		n, err := io.Copy(out, io.LimitReader(src, limit+1))
		src.Close()
		out.Close()
		if err != nil || n > limit {
			return ErrDamaged
		}
		files[e.Name] = dst
	}
	db, ok := files[dbEntry]
	if !ok {
		return ErrInvalid
	}
	if err := store.Check(ctx, db); err != nil {
		if err.Error() == "made by a newer version" {
			return ErrNewer
		}
		return ErrDamaged
	}
	// the users must leave someone able to administer the app
	if users, ok := files[auth.UsersFile]; ok {
		data, _ := os.ReadFile(users)
		var list []struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(data, &list) != nil {
			return ErrDamaged
		}
		admin := m.Auth != nil && m.Auth.HasEnvAdmin()
		for _, u := range list {
			if u.Role == "admin" {
				admin = true
			}
		}
		if len(list) > 0 && !admin {
			return ErrNoAdmin
		}
	}
	if act, ok := files[activity.Filename]; ok {
		data, _ := os.ReadFile(act)
		if json.Unmarshal(data, &[]activity.Entry{}) != nil {
			return ErrDamaged
		}
	}

	if err := m.Store.Restore(ctx, db); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	for _, name := range []string{auth.UsersFile, activity.Filename} {
		src, ok := files[name]
		if !ok {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		path := filepath.Join(m.DataFolder, name)
		if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
			return err
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			return err
		}
	}
	if _, ok := files[auth.UsersFile]; ok && m.Auth != nil {
		if err := m.Auth.ReloadUsers(); err != nil {
			return err
		}
	}
	if m.Activity != nil {
		m.Activity.Reload()
	}
	if m.OnRestore != nil {
		m.OnRestore()
	}
	return nil
}
