// Package datsync downloads DATs from the libretro-database repository, which mirrors the
// No-Intro DATs and is updated regularly, and imports them.
package datsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DeLFuS77/completist/internal/dat"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
	"github.com/DeLFuS77/completist/internal/tasks"
)

// DefaultBaseURL is the metadat folder of libretro-database, where the DATs are downloaded
// from: No-Intro DATs in no-intro, Redump DATs in redump. COMPLETIST_DAT_URL replaces it, for
// a mirror with the same folders.
const DefaultBaseURL = "https://raw.githubusercontent.com/libretro/libretro-database/master/metadat/"

func baseURL() string {
	if u := os.Getenv("COMPLETIST_DAT_URL"); u != "" {
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		return u
	}
	return DefaultBaseURL
}

// URL is the address of the DAT of a system.
func URL(s *systems.System) string {
	folder, name, _ := strings.Cut(s.DatPath(), "/")
	return baseURL() + folder + "/" + url.PathEscape(name)
}

var client = &http.Client{Timeout: 5 * time.Minute}

// Download fetches and parses the DAT of a system.
func Download(ctx context.Context, s *systems.System) (*dat.Dat, error) {
	if s.LibretroDat == "" {
		return nil, fmt.Errorf("no DAT is known for %s", s.Short)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL(s), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Completist (+https://github.com/DeLFuS77/completist)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", URL(s), resp.Status)
	}
	return dat.Parse(io.LimitReader(resp.Body, 512<<20))
}

var running sync.Mutex

// ErrBusy tells that DATs are being downloaded already.
var ErrBusy = errors.New("the DATs are already being downloaded")

// Start runs Sync in the background, unless a download is running already. onDone gets what
// changed, when set.
func Start(ctx context.Context, st *store.Store, log *tasks.Log, trigger string, ids []string, onChange func(), onDone func([]store.DatChange)) error {
	if !running.TryLock() {
		return ErrBusy
	}
	go func() {
		defer running.Unlock()
		changes := syncSystems(ctx, st, log, trigger, ids, onChange)
		if onDone != nil {
			onDone(changes)
		}
	}()
	return nil
}

// Sync downloads and imports the DATs of the given systems, as a task, and waits. It returns
// what changed in each.
func Sync(ctx context.Context, st *store.Store, log *tasks.Log, trigger string, ids []string, onChange func()) []store.DatChange {
	running.Lock()
	defer running.Unlock()
	return syncSystems(ctx, st, log, trigger, ids, onChange)
}

func syncSystems(ctx context.Context, st *store.Store, log *tasks.Log, trigger string, ids []string, onChange func()) []store.DatChange {
	id := log.Start(tasks.KindDatSync, trigger)
	changes := []store.DatChange{}
	games := 0
	for i, sid := range ids {
		s, ok := systems.ByID(sid)
		if !ok {
			continue
		}
		log.Progress(id, i, len(ids), s.Short)
		d, err := Download(ctx, s)
		if err != nil {
			log.Warn(id, tasks.NoteDatFailed, fmt.Sprintf("%s: %v", s.Short, err))
			continue
		}
		change, err := st.ImportDat(ctx, s.ID, d, "libretro-database")
		if err != nil {
			log.Warn(id, tasks.NoteDatFailed, fmt.Sprintf("%s: %v", s.Short, err))
			continue
		}
		games += change.Games
		changes = append(changes, change)
		if onChange != nil {
			onChange()
		}
	}
	log.SetResult(id, len(changes), games)
	log.Finish(id, nil)
	return changes
}
