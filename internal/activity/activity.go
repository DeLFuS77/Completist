// Package activity records who changed what: libraries, DATs, how games are counted, users,
// notifications and backups. Administrators see it in the settings. Only actions that
// succeeded are recorded.
//
// Adapted from Switch Library Manager Web by the same author.
package activity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	Filename   = "activity.json"
	maxEntries = 500
)

// actions; the interface translates them
const (
	LibraryAdded      = "Added a library"
	LibraryChanged    = "Changed a library"
	LibraryRemoved    = "Removed a library"
	DatsDownloaded    = "Downloaded DATs"
	DatImported       = "Imported a DAT"
	DatRemoved        = "Removed a DAT"
	CountingChanged   = "Changed how games are counted"
	NotifyChanged     = "Changed the notifications"
	BackupMade        = "Made a backup"
	BackupRestored    = "Restored a backup"
	UserCreated       = "Created a user"
	UserRoleChanged   = "Changed the role of a user"
	UserPasswordReset = "Changed the password of a user"
	UserDeleted       = "Deleted a user"
	CoversCleared     = "Removed the stored images"
	CoversChanged     = "Changed how box art is kept"
	WishAdded         = "Added to the wishlist"
	WishRemoved       = "Removed from the wishlist"
)

// Entry is an action of a user.
type Entry struct {
	Time   time.Time `json:"time"`
	User   string    `json:"user,omitempty"`
	Action string    `json:"action"`
	Detail string    `json:"detail,omitempty"`
}

// Log keeps the last actions in the data folder.
type Log struct {
	mutex   sync.Mutex
	path    string
	entries []Entry
}

// Load reads the log of the data folder; an empty folder keeps it in memory only.
func Load(dataFolder string) *Log {
	l := &Log{}
	if dataFolder != "" {
		l.path = filepath.Join(dataFolder, Filename)
	}
	l.Reload()
	return l
}

// Reload reads the log again, after a backup was restored.
func (l *Log) Reload() {
	entries := []Entry{}
	if l.path != "" {
		if data, err := os.ReadFile(l.path); err == nil {
			json.Unmarshal(data, &entries)
		}
	}
	l.mutex.Lock()
	l.entries = entries
	l.mutex.Unlock()
}

// Add records an action, newest first.
func (l *Log) Add(user, action, detail string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.entries = append([]Entry{{Time: time.Now().UTC(), User: user, Action: action, Detail: detail}}, l.entries...)
	if len(l.entries) > maxEntries {
		l.entries = l.entries[:maxEntries]
	}
	if l.path == "" {
		return
	}
	if data, err := json.MarshalIndent(l.entries, "", " "); err == nil {
		tmp := l.path + ".tmp"
		if os.WriteFile(tmp, data, 0o600) == nil {
			os.Rename(tmp, l.path)
		}
	}
}

// List returns the actions, newest first.
func (l *Log) List() []Entry {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return append([]Entry{}, l.entries...)
}
