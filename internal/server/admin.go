package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/DeLFuS77/completist/internal/activity"
	"github.com/DeLFuS77/completist/internal/backup"
	"github.com/DeLFuS77/completist/internal/notify"
)

// adminRoutes are the activity log, the notifications and the backups. Only administrators
// reach them (see adminOnlyPaths in the auth package).
func (s *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/activity", func(w http.ResponseWriter, r *http.Request) {
		entries := []activity.Entry{}
		if s.Activity != nil {
			entries = s.Activity.List()
		}
		writeJSON(w, http.StatusOK, entries)
	})

	// ---- notifications ----
	mux.HandleFunc("GET /api/notifications", func(w http.ResponseWriter, r *http.Request) {
		if s.Notifier == nil {
			writeJSON(w, http.StatusOK, notify.DefaultOptions())
			return
		}
		writeJSON(w, http.StatusOK, s.Notifier.Options())
	})

	mux.HandleFunc("PUT /api/notifications", func(w http.ResponseWriter, r *http.Request) {
		if s.Notifier == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		var o notify.Options
		if err := decode(r, &o); err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		if errs := o.Validate(); len(errs) > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": errs[0].Message, "fields": errs})
			return
		}
		if err := s.Notifier.SaveOptions(o); err != nil {
			s.fail(w, err)
			return
		}
		s.record(r, activity.NotifyChanged, "")
		writeJSON(w, http.StatusOK, s.Notifier.Options())
	})

	mux.HandleFunc("POST /api/notifications/test", func(w http.ResponseWriter, r *http.Request) {
		if s.Notifier == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		o := s.Notifier.Options()
		if !o.Configured() {
			writeError(w, http.StatusBadRequest, "No notification channel is set. Save the settings first.")
			return
		}
		if err := notify.Send(o, []notify.Item{{Kind: "test"}}); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	// ---- backups ----
	mux.HandleFunc("GET /api/backups", func(w http.ResponseWriter, r *http.Request) {
		if s.Backups == nil {
			writeJSON(w, http.StatusOK, []backup.Info{})
			return
		}
		writeJSON(w, http.StatusOK, s.Backups.List())
	})

	mux.HandleFunc("POST /api/backups", func(w http.ResponseWriter, r *http.Request) {
		if s.Backups == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		info, err := s.Backups.Make(r.Context(), time.Now())
		if err != nil {
			s.fail(w, err)
			return
		}
		s.record(r, activity.BackupMade, info.Name)
		writeJSON(w, http.StatusCreated, info)
	})

	// a fresh backup, downloaded at once
	mux.HandleFunc("GET /api/backups/download", func(w http.ResponseWriter, r *http.Request) {
		if s.Backups == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		name := "completist-backup-" + time.Now().Format("2006-01-02") + ".zip"
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Cache-Control", "no-store")
		if err := s.Backups.Write(r.Context(), w); err != nil {
			s.Log.Error("backup failed", "error", err)
		}
	})

	mux.HandleFunc("GET /api/backups/{name}", func(w http.ResponseWriter, r *http.Request) {
		path := ""
		if s.Backups != nil {
			path = s.Backups.Path(r.PathValue("name"))
		}
		if path == "" {
			writeError(w, http.StatusNotFound, backup.ErrNotFound.Error())
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("name")+`"`)
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})

	mux.HandleFunc("POST /api/backups/{name}/restore", func(w http.ResponseWriter, r *http.Request) {
		path := ""
		if s.Backups != nil {
			path = s.Backups.Path(r.PathValue("name"))
		}
		if path == "" {
			writeError(w, http.StatusNotFound, backup.ErrNotFound.Error())
			return
		}
		s.restore(w, r, path, r.PathValue("name"))
	})

	mux.HandleFunc("POST /api/backups/restore", func(w http.ResponseWriter, r *http.Request) {
		if s.Backups == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<30)
		tmp, err := os.CreateTemp(s.Backups.DataFolder, ".upload-*.zip")
		if err != nil {
			s.fail(w, err)
			return
		}
		defer os.Remove(tmp.Name())
		var src io.Reader = r.Body
		name := "upload"
		if file, header, err := r.FormFile("file"); err == nil {
			defer file.Close()
			src, name = file, header.Filename
		}
		if _, err := io.Copy(tmp, src); err != nil {
			tmp.Close()
			writeError(w, http.StatusBadRequest, backup.ErrInvalid.Error())
			return
		}
		tmp.Close()
		s.restore(w, r, tmp.Name(), name)
	})
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request, path, name string) {
	if err := s.Backups.RestoreFile(r.Context(), path); err != nil {
		if backup.Known(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, backup.ErrNotFound.Error())
			return
		}
		s.fail(w, err)
		return
	}
	s.Log.Info("backup restored", "name", name)
	s.record(r, activity.BackupRestored, name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
