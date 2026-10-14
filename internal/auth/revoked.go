package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The session cookies are signed, not stored, so Log out also writes down the signature of the
// session until it would have expired: a copied cookie stops working too.

const REVOKED_SESSIONS_FILENAME = "sessions-ended.json"

type revokedSessions struct {
	mutex sync.Mutex
	path  string
	// signature of the session -> when it expires (Unix seconds)
	ended map[string]int64
}

func loadRevokedSessions(dataFolder string) *revokedSessions {
	r := &revokedSessions{path: filepath.Join(dataFolder, REVOKED_SESSIONS_FILENAME), ended: map[string]int64{}}
	if data, err := os.ReadFile(r.path); err == nil {
		json.Unmarshal(data, &r.ended)
	}
	return r
}

func (r *revokedSessions) has(signature string) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	_, ok := r.ended[signature]
	return ok
}

func (r *revokedSessions) add(signature string, expiry int64) {
	r.mutex.Lock()
	now := time.Now().Unix()
	for key, until := range r.ended {
		if until < now {
			delete(r.ended, key)
		}
	}
	r.ended[signature] = expiry
	defer r.mutex.Unlock()
	data, err := json.Marshal(r.ended)
	if err != nil {
		return
	}
	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, data, 0600) == nil {
		os.Rename(tmp, r.path)
	}
}
