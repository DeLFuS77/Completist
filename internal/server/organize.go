package server

import (
	"context"
	"net/http"
	"strconv"
	"sync"

	"github.com/DeLFuS77/completist/internal/activity"
	"github.com/DeLFuS77/completist/internal/auth"
	"github.com/DeLFuS77/completist/internal/organize"
	"github.com/DeLFuS77/completist/internal/tasks"
)

// one change to the files at a time
var organizing sync.Mutex

func (s *Server) organizeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/organize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		kind := q.Get("kind")
		if kind != organize.KindZip && kind != organize.KindCHD {
			kind = organize.KindRename
		}
		plan, err := organize.MakePlan(r.Context(), s.Store, q.Get("system"), kind)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		organize.SortSkipped(plan.Skipped)
		writeJSON(w, http.StatusOK, plan)
	})

	mux.HandleFunc("POST /api/organize", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			System          string   `json:"system"`
			Kind            string   `json:"kind"`
			IDs             []string `json:"ids"`
			RemoveOriginals bool     `json:"removeOriginals"`
		}
		if err := decode(r, &in); err != nil || len(in.IDs) == 0 {
			writeError(w, http.StatusBadRequest, "Choose the games to change.")
			return
		}
		if in.Kind != organize.KindZip && in.Kind != organize.KindCHD {
			in.Kind = organize.KindRename
		}
		if _, err := organize.MakePlan(r.Context(), s.Store, in.System, in.Kind); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !organizing.TryLock() {
			writeError(w, http.StatusConflict, "Files are being changed already.")
			return
		}
		user := userName(r)
		go func() {
			defer organizing.Unlock()
			s.runOrganize(in.System, in.Kind, in.IDs, in.RemoveOriginals, user)
		}()
		w.WriteHeader(http.StatusAccepted)
	})

	mux.HandleFunc("GET /api/organize/history", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.Journal.List())
	})

	mux.HandleFunc("POST /api/organize/history/{id}/undo", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		b, ok := s.Journal.Get(id)
		if err != nil || !ok {
			writeError(w, http.StatusNotFound, "unknown change")
			return
		}
		if b.Undone {
			writeError(w, http.StatusConflict, "This change was undone already.")
			return
		}
		if !organizing.TryLock() {
			writeError(w, http.StatusConflict, "Files are being changed already.")
			return
		}
		user := userName(r)
		go func() {
			defer organizing.Unlock()
			s.runUndo(b, user)
		}()
		w.WriteHeader(http.StatusAccepted)
	})
}

func userName(r *http.Request) string {
	if p := auth.PrincipalFrom(r); p != nil {
		return p.Name
	}
	return ""
}

// runOrganize applies a change as a task, while no scan runs.
func (s *Server) runOrganize(system, kind string, ids []string, removeOriginals bool, user string) {
	id := s.Tasks.Start(tasks.KindOrganize, tasks.TriggerManual)
	var batch organize.Batch
	var failed []organize.Skipped
	var err error
	s.Scanner.Exclusive(func() {
		ctx := context.Background()
		var plan organize.Plan
		plan, err = organize.MakePlan(ctx, s.Store, system, kind)
		if err != nil {
			return
		}
		batch, failed, err = organize.Apply(ctx, s.Store, s.Journal, plan, ids, removeOriginals, user, func(done, total int, name string) {
			s.Tasks.Progress(id, done, total, name)
		})
	})
	if err != nil {
		s.Tasks.Finish(id, &tasks.Note{Text: tasks.NoteOrganize, Detail: err.Error()})
		return
	}
	for _, f := range failed {
		s.Tasks.Warn(id, tasks.NoteSkipped, f.Path+": "+f.Reason)
	}
	s.Tasks.SetResult(id, len(batch.Changes), batch.Games)
	s.Tasks.Finish(id, nil)
	if batch.Games > 0 {
		action := activity.FilesRenamed
		switch kind {
		case organize.KindZip:
			action = activity.FilesPacked
		case organize.KindCHD:
			action = activity.FilesToCHD
		}
		s.recordAs(user, action, systemNames([]string{system}))
	}
	s.afterOrganize(kind)
}

func (s *Server) runUndo(b organize.Batch, user string) {
	id := s.Tasks.Start(tasks.KindOrganize, tasks.TriggerManual)
	var failed []organize.Skipped
	var err error
	s.Scanner.Exclusive(func() {
		failed, err = organize.Undo(context.Background(), s.Store, s.Journal, b.ID)
	})
	if err != nil {
		s.Tasks.Finish(id, &tasks.Note{Text: tasks.NoteOrganize, Detail: err.Error()})
		return
	}
	for _, f := range failed {
		s.Tasks.Warn(id, tasks.NoteSkipped, f.Path+": "+f.Reason)
	}
	s.Tasks.SetResult(id, len(b.Changes), b.Games)
	s.Tasks.Finish(id, nil)
	s.recordAs(user, activity.ChangeUndone, systemNames([]string{b.System}))
	s.afterOrganize(b.Kind)
}

// afterOrganize brings the collection up to date: renamed files were moved in the store, packed
// ones are read again.
func (s *Server) afterOrganize(kind string) {
	s.Store.Rematch(context.Background())
	s.changed()
	if kind == organize.KindZip {
		s.Scanner.Start(tasks.TriggerSettings)
	}
}

func (s *Server) recordAs(user, action, detail string) {
	if s.Activity != nil {
		s.Activity.Add(user, action, detail)
	}
}
