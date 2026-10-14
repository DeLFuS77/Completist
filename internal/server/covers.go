package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/DeLFuS77/completist/internal/activity"
	"github.com/DeLFuS77/completist/internal/covers"
	"github.com/DeLFuS77/completist/internal/systems"
)

func (s *Server) coverRoutes(mux *http.ServeMux) {
	// an image of a game: /api/covers/snes/boxart/Super Metroid (Japan, USA) (En,Ja).png
	mux.HandleFunc("GET /api/covers/{system}/{kind}/{name}", func(w http.ResponseWriter, r *http.Request) {
		if s.Covers == nil {
			http.NotFound(w, r)
			return
		}
		sys, ok := systems.ByID(r.PathValue("system"))
		name, isPNG := strings.CutSuffix(r.PathValue("name"), ".png")
		if !ok || !isPNG || name == "" {
			http.NotFound(w, r)
			return
		}
		// only games of a DAT, so the server never downloads what a visitor makes up
		exists, parent, err := s.Store.GameExists(r.Context(), sys.ID, name)
		if err != nil {
			s.fail(w, err)
			return
		}
		if !exists {
			http.NotFound(w, r)
			return
		}
		names := []string{name}
		if parent != "" && parent != name {
			names = append(names, parent)
		}
		path, err := s.Covers.Get(r.Context(), sys.ID, sys.LibretroDat, r.PathValue("kind"), names)
		switch {
		case errors.Is(err, covers.ErrNotFound):
			// the browser asks again tomorrow, the server remembers for a week
			w.Header().Set("Cache-Control", "private, max-age=86400")
			http.NotFound(w, r)
			return
		case errors.Is(err, covers.ErrUnavailable):
			w.Header().Set("Cache-Control", "no-store")
			writeError(w, http.StatusBadGateway, "The image server is not available.")
			return
		case err != nil:
			if r.Context().Err() == nil {
				s.fail(w, err)
			}
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=2592000")
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, path)
	})

	mux.HandleFunc("GET /api/covers", func(w http.ResponseWriter, r *http.Request) {
		if s.Covers == nil {
			writeJSON(w, http.StatusOK, covers.Usage{})
			return
		}
		writeJSON(w, http.StatusOK, s.Covers.Usage())
	})
	mux.HandleFunc("DELETE /api/covers", func(w http.ResponseWriter, r *http.Request) {
		if s.Covers == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := s.Covers.Clear(); err != nil {
			s.fail(w, err)
			return
		}
		s.record(r, activity.CoversCleared, "")
		w.WriteHeader(http.StatusNoContent)
	})
}
