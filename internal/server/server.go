// Package server is the HTTP API and serves the interface.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DeLFuS77/completist/internal/activity"
	"github.com/DeLFuS77/completist/internal/auth"
	"github.com/DeLFuS77/completist/internal/backup"
	"github.com/DeLFuS77/completist/internal/collection"
	"github.com/DeLFuS77/completist/internal/covers"
	"github.com/DeLFuS77/completist/internal/dat"
	"github.com/DeLFuS77/completist/internal/datsync"
	"github.com/DeLFuS77/completist/internal/notify"
	"github.com/DeLFuS77/completist/internal/scanner"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
	"github.com/DeLFuS77/completist/internal/tasks"
)

// Version is set at build time.
var Version = "dev"

// Server holds what the handlers need.
type Server struct {
	Store   *store.Store
	Tasks   *tasks.Log
	Scanner *scanner.Scanner
	// optional: who changed what, the backups and the notifications
	Activity *activity.Log
	Backups  *backup.Manager
	Notifier *notify.Notifier
	// optional: the images of the games, kept in the data folder
	Covers   *covers.Cache
	Progress *collection.Cache
	Auth     *auth.Auth
	Log      *slog.Logger
	// the built interface; nil serves a short notice
	UI http.FileSystem
	// hashes of the inline scripts of the interface, for the content security policy
	UIScriptHashes []string
}

// Handler returns the whole app: API, interface and the middlewares around them.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
	})
	s.Auth.Routes(mux, s.Log)
	s.routes(mux)
	s.adminRoutes(mux)
	s.coverRoutes(mux)
	s.extraRoutes(mux)
	mux.Handle("/", s.ui())

	var h http.Handler = mux
	h = s.Auth.Middleware(h)
	h = auth.SameOriginOnly(h)
	h = auth.WithSecurityHeaders(h, s.UIScriptHashes)
	// the health check works without login, for Docker and monitoring
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			mux.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// fail logs an unexpected error; the client gets no internal details.
func (s *Server) fail(w http.ResponseWriter, err error) {
	s.Log.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "Something went wrong. The log of the app tells what.")
}

func decode(r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	return json.NewDecoder(r.Body).Decode(value)
}

func (s *Server) changed() {
	s.Progress.Invalidate()
	s.recordHistory(context.Background())
}

// record adds an action to the activity log, with the name of who did it.
func (s *Server) record(r *http.Request, action, detail string) {
	if s.Activity == nil {
		return
	}
	name := ""
	if p := auth.PrincipalFrom(r); p != nil {
		name = p.Name
	}
	s.Activity.Add(name, action, detail)
}

// systemNames are the short names of systems, for the activity log.
func systemNames(ids []string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if sys, ok := systems.ByID(id); ok {
			names = append(names, sys.Short)
		} else {
			names = append(names, id)
		}
	}
	return strings.Join(names, ", ")
}

// datsUpdated tells the notifier that the DATs changed: files that match a new DAT are not
// new in the collection.
func (s *Server) datsUpdated(changes []store.DatChange) {
	s.changed()
	if s.Notifier != nil {
		s.Notifier.AfterDatUpdate(context.Background(), changes)
	}
}

func (s *Server) routes(mux *http.ServeMux) {
	// ---- the collection ----
	mux.HandleFunc("GET /api/overview", func(w http.ResponseWriter, r *http.Request) {
		all, err := s.Progress.All(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		libraries, err := s.Store.Libraries()
		if err != nil {
			s.fail(w, err)
			return
		}
		dats, _ := s.Store.Dats()
		summary := collection.Summarize(all)
		// files of no known system are in no system's progress
		if counts, err := s.Store.FileCounts(r.Context()); err == nil {
			if none := counts[""]; none != nil {
				summary.Files += none.Files
				summary.Bytes += none.Bytes
				summary.Unknown += none.Unknown + none.NoDat
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"summary":   summary,
			"systems":   all,
			"libraries": len(libraries),
			"dats":      len(dats),
			"scanning":  s.Tasks.Running(tasks.KindScan),
			"version":   Version,
		})
	})

	// one bit per game of every system, owned or not, for the mosaic of the home page
	mux.HandleFunc("GET /api/mosaic", func(w http.ResponseWriter, r *http.Request) {
		all, err := s.Progress.All(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		prefs := collection.LoadPreferences(s.Store)
		type row struct {
			ID    string `json:"id"`
			Total int    `json:"total"`
			Bits  []byte `json:"bits"`
		}
		out := []row{}
		for _, p := range all {
			if !p.HasDat {
				continue
			}
			entries, err := collection.Entries(r.Context(), s.Store, p.System.ID, prefs.ModeOf(p.System.ID), prefs.Regions)
			if err != nil {
				s.fail(w, err)
				return
			}
			bits := make([]byte, (len(entries)+7)/8)
			for i, e := range entries {
				if e.Owned {
					bits[i/8] |= 1 << (i % 8)
				}
			}
			out = append(out, row{ID: p.System.ID, Total: len(entries), Bits: bits})
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/systems", func(w http.ResponseWriter, r *http.Request) {
		prefs := collection.LoadPreferences(s.Store)
		dats, err := s.Store.Dats()
		if err != nil {
			s.fail(w, err)
			return
		}
		byID := map[string]store.DatInfo{}
		for _, d := range dats {
			byID[d.System] = d
		}
		type item struct {
			systems.System
			Mode string         `json:"mode"`
			Dat  *store.DatInfo `json:"dat"`
		}
		out := []item{}
		for _, sys := range systems.All {
			it := item{System: sys, Mode: prefs.ModeOf(sys.ID)}
			if d, ok := byID[sys.ID]; ok {
				it.Dat = &d
			}
			out = append(out, it)
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/systems/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		all, err := s.Progress.All(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		for _, p := range all {
			if p.System.ID == id {
				writeJSON(w, http.StatusOK, p)
				return
			}
		}
		sys, ok := systems.ByID(id)
		if !ok {
			writeError(w, http.StatusNotFound, "unknown system")
			return
		}
		writeJSON(w, http.StatusOK, collection.Progress{System: *sys, Mode: collection.LoadPreferences(s.Store).ModeOf(id)})
	})

	mux.HandleFunc("GET /api/systems/{id}/games", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := systems.ByID(id); !ok {
			writeError(w, http.StatusNotFound, "unknown system")
			return
		}
		prefs := collection.LoadPreferences(s.Store)
		mode := prefs.ModeOf(id)
		if m := r.URL.Query().Get("mode"); collection.ValidMode(m) {
			mode = m
		}
		entries, err := collection.Entries(r.Context(), s.Store, id, mode, prefs.Regions)
		if err != nil {
			s.fail(w, err)
			return
		}
		if err := collection.MarkWanted(r.Context(), s.Store, id, entries); err != nil {
			s.fail(w, err)
			return
		}
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		filter := r.URL.Query().Get("filter")
		region := r.URL.Query().Get("region")
		out := make([]collection.Entry, 0, len(entries))
		for _, e := range entries {
			if filter == "have" && !e.Owned || filter == "missing" && e.Owned || filter == "wanted" && !e.Wanted {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(e.Name), q) {
				continue
			}
			if region != "" && !contains(e.Regions, region) {
				continue
			}
			out = append(out, e)
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit <= 0 || limit > 5000 {
			limit = 5000
		}
		total := len(out)
		if offset < 0 {
			offset = 0
		}
		if offset > total {
			offset = total
		}
		end := min(total, offset+limit)
		regions := map[string]int{}
		for _, e := range entries {
			for _, rg := range e.Regions {
				regions[rg]++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "total": total, "offset": offset, "items": out[offset:end], "regions": regions})
	})

	mux.HandleFunc("GET /api/systems/{id}/files", func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "unknown"
		}
		files, err := s.Store.FilesWithStatus(r.Context(), r.PathValue("id"), status, 1000)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, files)
	})

	mux.HandleFunc("GET /api/games/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad id")
			return
		}
		g, system, err := s.Store.Game(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "unknown game")
			return
		}
		files, err := s.Store.FilesOfGame(r.Context(), id)
		if err != nil {
			s.fail(w, err)
			return
		}
		roms, err := s.Store.RomsOfGame(r.Context(), id)
		if err != nil {
			s.fail(w, err)
			return
		}
		// the other versions of the same game: regions, revisions, betas...
		all, err := s.Store.Games(r.Context(), system)
		if err != nil {
			s.fail(w, err)
			return
		}
		type version struct {
			ID      int64    `json:"id"`
			Name    string   `json:"name"`
			Regions []string `json:"regions"`
			Flags   []string `json:"flags"`
			Owned   bool     `json:"owned"`
		}
		versions := []version{}
		for _, other := range all {
			if strings.EqualFold(other.Title, g.Title) {
				v := version{ID: other.ID, Name: other.Name, Flags: dat.Flags(other.Flags).Names(), Owned: other.Have > 0, Regions: []string{}}
				if other.Regions != "" {
					v.Regions = strings.Split(other.Regions, ",")
				}
				versions = append(versions, v)
			}
		}
		sys, _ := systems.ByID(system)
		wanted := false
		if wishes, err := s.Store.Wishes(r.Context()); err == nil {
			for _, wish := range wishes {
				if wish.System == system && wish.Name == g.Name {
					wanted = true
				}
			}
		}
		regions := []string{}
		if g.Regions != "" {
			regions = strings.Split(g.Regions, ",")
		}
		languages := []string{}
		if g.Languages != "" {
			languages = strings.Split(g.Languages, ",")
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": g.ID, "name": g.Name, "title": g.Title, "system": sys, "regions": regions, "languages": languages,
			"flags": dat.Flags(g.Flags).Names(), "revision": g.Revision, "owned": g.Have > 0,
			"files": files, "roms": roms, "versions": versions, "wanted": wanted,
		})
	})

	// ---- libraries ----
	mux.HandleFunc("GET /api/libraries", func(w http.ResponseWriter, r *http.Request) {
		libs, err := s.Store.Libraries()
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, libs)
	})

	mux.HandleFunc("POST /api/libraries", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path     string `json:"path"`
			Name     string `json:"name"`
			ReadOnly bool   `json:"readOnly"`
		}
		if err := decode(r, &body); err != nil || strings.TrimSpace(body.Path) == "" {
			writeError(w, http.StatusBadRequest, "A folder is needed.")
			return
		}
		info, err := os.Stat(body.Path)
		if err != nil || !info.IsDir() {
			writeError(w, http.StatusBadRequest, "The folder does not exist or cannot be opened.")
			return
		}
		lib, err := s.Store.AddLibrary(body.Path, strings.TrimSpace(body.Name), body.ReadOnly)
		if errors.Is(err, store.ErrLibraryExists) {
			writeError(w, http.StatusConflict, "This folder is already a library.")
			return
		}
		if errors.Is(err, store.ErrLibraryNested) {
			writeError(w, http.StatusConflict, "This folder is inside another library, or contains one.")
			return
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		s.Log.Info("library added", "path", lib.Path)
		s.record(r, activity.LibraryAdded, lib.Path)
		s.Scanner.Start(tasks.TriggerSettings)
		writeJSON(w, http.StatusCreated, lib)
	})

	mux.HandleFunc("PUT /api/libraries/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var body struct {
			Name     string `json:"name"`
			ReadOnly bool   `json:"readOnly"`
		}
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		if err := s.Store.UpdateLibrary(id, body.Name, body.ReadOnly); err != nil {
			s.fail(w, err)
			return
		}
		s.record(r, activity.LibraryChanged, body.Name)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("DELETE /api/libraries/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		removed := ""
		if libs, err := s.Store.Libraries(); err == nil {
			for _, l := range libs {
				if l.ID == id {
					removed = l.Path
				}
			}
		}
		if err := s.Store.RemoveLibrary(id); err != nil {
			s.fail(w, err)
			return
		}
		s.record(r, activity.LibraryRemoved, removed)
		// the files of the library are forgotten, never deleted from the disk
		s.Store.Rematch(r.Context())
		s.changed()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	// folders on the server, to pick a library
	mux.HandleFunc("GET /api/fs", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
			if home, err := os.UserHomeDir(); err == nil && os.Getenv("COMPLETIST_IN_DOCKER") == "" {
				path = home
			}
		}
		path = filepath.Clean(path)
		entries, err := os.ReadDir(path)
		if err != nil {
			writeError(w, http.StatusBadRequest, "The folder cannot be opened.")
			return
		}
		dirs := []string{}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				dirs = append(dirs, e.Name())
			}
		}
		sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i]) < strings.ToLower(dirs[j]) })
		parent := filepath.Dir(path)
		writeJSON(w, http.StatusOK, map[string]any{"path": path, "parent": parent, "folders": dirs})
	})

	mux.HandleFunc("POST /api/scan", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Scanner.Start(tasks.TriggerManual); err != nil {
			writeError(w, http.StatusConflict, "A scan is already running.")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
	})

	// ---- DATs ----
	mux.HandleFunc("GET /api/dats", func(w http.ResponseWriter, r *http.Request) {
		dats, err := s.Store.Dats()
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, dats)
	})

	mux.HandleFunc("POST /api/dats/sync", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Systems []string `json:"systems"`
		}
		decode(r, &body)
		ids := body.Systems
		if len(ids) == 0 {
			// the systems with a DAT already and those with files but no DAT
			ids = s.systemsWorthADat(r.Context())
		}
		if len(ids) == 0 {
			writeError(w, http.StatusBadRequest, "Choose the systems whose DATs you want.")
			return
		}
		if err := datsync.Start(context.Background(), s.Store, s.Tasks, tasks.TriggerManual, ids, s.changed, s.datsUpdated); err != nil {
			writeError(w, http.StatusConflict, "The DATs are already being downloaded.")
			return
		}
		s.record(r, activity.DatsDownloaded, systemNames(ids))
		writeJSON(w, http.StatusAccepted, map[string]any{"systems": ids})
	})

	mux.HandleFunc("POST /api/dats/import", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
		var reader io.Reader = r.Body
		name := r.URL.Query().Get("name")
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			file, header, err := r.FormFile("file")
			if err != nil {
				writeError(w, http.StatusBadRequest, "No file was sent.")
				return
			}
			defer file.Close()
			reader, name = file, header.Filename
		}
		d, err := dat.Parse(reader)
		if err != nil {
			writeError(w, http.StatusBadRequest, "This file is not a DAT.")
			return
		}
		systemID := r.URL.Query().Get("system")
		sys, ok := systems.ByID(systemID)
		if !ok {
			sys, ok = systems.ByDatName(d.Name)
		}
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("The system of this DAT (%s) is not known; choose it.", d.Name))
			return
		}
		change, err := s.Store.ImportDat(r.Context(), sys.ID, d, "file: "+filepath.Base(name))
		if err != nil {
			s.fail(w, err)
			return
		}
		s.changed()
		s.Log.Info("DAT imported", "system", sys.ID, "games", change.Games)
		s.record(r, activity.DatImported, sys.Short+": "+filepath.Base(name))
		s.datsUpdated(nil)
		writeJSON(w, http.StatusOK, change)
	})

	mux.HandleFunc("DELETE /api/dats/{system}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Store.RemoveDat(r.Context(), r.PathValue("system")); err != nil {
			s.fail(w, err)
			return
		}
		s.record(r, activity.DatRemoved, systemNames([]string{r.PathValue("system")}))
		s.datsUpdated(nil)
		s.changed()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	// ---- preferences ----
	mux.HandleFunc("GET /api/preferences", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, collection.LoadPreferences(s.Store))
	})

	mux.HandleFunc("PUT /api/preferences", func(w http.ResponseWriter, r *http.Request) {
		var p collection.Preferences
		if err := decode(r, &p); err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		if !collection.ValidMode(p.DefaultMode) {
			writeError(w, http.StatusBadRequest, "unknown mode")
			return
		}
		for id, m := range p.Modes {
			if _, ok := systems.ByID(id); !ok || !collection.ValidMode(m) {
				delete(p.Modes, id)
			}
		}
		if err := collection.SavePreferences(s.Store, p); err != nil {
			s.fail(w, err)
			return
		}
		s.record(r, activity.CountingChanged, p.DefaultMode)
		s.changed()
		writeJSON(w, http.StatusOK, p)
	})

	// ---- tasks ----
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"version": s.Tasks.Version(), "tasks": s.Tasks.Snapshot()})
	})

	mux.HandleFunc("DELETE /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		s.Tasks.Dismiss(id)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("DELETE /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		s.Tasks.ClearFinished()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	// live updates of the tasks, as server-sent events
	mux.HandleFunc("GET /api/tasks/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming is not supported")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		changes, stop := s.Tasks.Subscribe()
		defer stop()
		send := func() {
			data, _ := json.Marshal(map[string]any{"version": s.Tasks.Version(), "tasks": s.Tasks.Snapshot()})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		send()
		keepAlive := time.NewTicker(25 * time.Second)
		defer keepAlive.Stop()
		throttle := time.NewTicker(250 * time.Millisecond)
		defer throttle.Stop()
		pending := false
		for {
			select {
			case <-r.Context().Done():
				return
			case <-changes:
				pending = true
			case <-throttle.C:
				if pending {
					pending = false
					send()
				}
			case <-keepAlive.C:
				fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			}
		}
	})
}

// systemsWorthADat are the systems that have a DAT or files.
func (s *Server) systemsWorthADat(ctx context.Context) []string {
	ids := []string{}
	seen := map[string]bool{}
	dats, _ := s.Store.Dats()
	for _, d := range dats {
		ids = append(ids, d.System)
		seen[d.System] = true
	}
	counts, _ := s.Store.FileCounts(ctx)
	for id := range counts {
		if id != "" && !seen[id] {
			if _, ok := systems.ByID(id); ok {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if strings.EqualFold(v, value) {
			return true
		}
	}
	return false
}

// ui serves the built interface; paths it does not have get its entry page, so the routes of
// the interface work when opened directly.
func (s *Server) ui() http.Handler {
	notice := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "Completist is running, but the interface was not built into this binary: run `make ui` (or `npm install && npm run build` in ui/) and build again.\n")
	})
	if s.UI == nil {
		return notice
	}
	if f, err := s.UI.Open("/index.html"); err != nil {
		return notice
	} else {
		f.Close()
	}
	files := http.FileServer(s.UI)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		path := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+r.URL.Path)), "/")
		if path != "" {
			if f, err := s.UI.Open("/" + path); err == nil {
				f.Close()
				if strings.HasPrefix(path, "_app/immutable/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		f, err := s.UI.Open("/index.html")
		if err != nil {
			notice(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		io.Copy(w, f)
	})
}
