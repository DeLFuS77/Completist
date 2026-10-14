package server

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/DeLFuS77/completist/internal/activity"
	"github.com/DeLFuS77/completist/internal/collection"
	"github.com/DeLFuS77/completist/internal/export"
	"github.com/DeLFuS77/completist/internal/stats"
	"github.com/DeLFuS77/completist/internal/systems"
)

// recordHistory saves today's count, for the history of the statistics.
func (s *Server) recordHistory(ctx context.Context) {
	if err := stats.Record(ctx, s.Store, s.Progress, time.Now()); err != nil {
		s.Log.Warn("the history could not be saved", "error", err)
	}
}

func (s *Server) extraRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		out, err := stats.Build(r.Context(), s.Store, s.Progress, time.Now())
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	// ---- wishlist ----
	mux.HandleFunc("GET /api/wishlist", func(w http.ResponseWriter, r *http.Request) {
		list, err := collection.Wishlist(r.Context(), s.Store)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("PUT /api/wishlist/{system}/{name}", func(w http.ResponseWriter, r *http.Request) {
		sys, ok := systems.ByID(r.PathValue("system"))
		if !ok {
			writeError(w, http.StatusNotFound, "unknown system")
			return
		}
		exists, _, err := s.Store.GameExists(r.Context(), sys.ID, r.PathValue("name"))
		if err != nil {
			s.fail(w, err)
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "unknown game")
			return
		}
		if err := s.Store.AddWish(r.Context(), sys.ID, r.PathValue("name")); err != nil {
			s.fail(w, err)
			return
		}
		s.record(r, activity.WishAdded, r.PathValue("name"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/wishlist/{system}/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Store.RemoveWish(r.Context(), r.PathValue("system"), r.PathValue("name")); err != nil {
			s.fail(w, err)
			return
		}
		s.record(r, activity.WishRemoved, r.PathValue("name"))
		w.WriteHeader(http.StatusNoContent)
	})

	// ---- exports ----
	mux.HandleFunc("GET /api/export/{format}", func(w http.ResponseWriter, r *http.Request) {
		format := r.PathValue("format")
		if format != "retroarch" && format != "esde" {
			http.NotFound(w, r)
			return
		}
		files, err := s.Store.MatchedFiles(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		// built in memory first, so an error is an error page and not half a zip
		var buf bytes.Buffer
		if format == "retroarch" {
			err = export.RetroArch(&buf, files, export.Paths{From: r.URL.Query().Get("from"), To: r.URL.Query().Get("to")})
		} else {
			err = export.ESDEGamelists(&buf, files)
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="completist-`+format+`.zip"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(buf.Bytes())
	})
}
