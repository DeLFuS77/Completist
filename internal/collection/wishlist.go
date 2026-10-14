package collection

import (
	"context"
	"strings"

	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
)

// Wish is a game on the wishlist and whether you have it now.
type Wish struct {
	store.Wish
	GameID  int64    `json:"id"`
	Title   string   `json:"title"`
	Regions []string `json:"regions"`
	// you have it, or, counting one version of each game, another version of it
	Owned bool `json:"owned"`
	// the DAT still lists it
	InDat bool `json:"inDat"`
}

// Wishlist returns the wishlist with what you have of it, counted the way each system is.
func Wishlist(ctx context.Context, s *store.Store) ([]Wish, error) {
	wishes, err := s.Wishes(ctx)
	if err != nil {
		return nil, err
	}
	prefs := LoadPreferences(s)
	type known struct {
		rows    map[string]store.GameRow
		entries map[string]Entry
	}
	bySystem := map[string]*known{}
	out := make([]Wish, 0, len(wishes))
	for _, w := range wishes {
		k, ok := bySystem[w.System]
		if !ok {
			k = &known{rows: map[string]store.GameRow{}, entries: map[string]Entry{}}
			bySystem[w.System] = k
			if _, valid := systems.ByID(w.System); valid {
				rows, err := s.Games(ctx, w.System)
				if err != nil {
					return nil, err
				}
				for _, r := range rows {
					k.rows[r.Name] = r
				}
				entries, err := Entries(ctx, s, w.System, prefs.ModeOf(w.System), prefs.Regions)
				if err != nil {
					return nil, err
				}
				for _, e := range entries {
					for _, name := range e.Names {
						k.entries[name] = e
					}
				}
			}
		}
		item := Wish{Wish: w, Title: w.Name, Regions: []string{}}
		if r, ok := k.rows[w.Name]; ok {
			item.InDat, item.GameID, item.Title, item.Owned = true, r.ID, r.Title, r.Have > 0
			if r.Regions != "" {
				item.Regions = strings.Split(r.Regions, ",")
			}
		}
		// counted as its system counts: all the discs, or any version of the game
		if e, ok := k.entries[w.Name]; ok {
			item.Owned = e.Owned
		}
		out = append(out, item)
	}
	return out, nil
}

// MarkWanted sets Wanted on the entries with a version on the wishlist.
func MarkWanted(ctx context.Context, s *store.Store, system string, entries []Entry) error {
	wishes, err := s.Wishes(ctx)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, w := range wishes {
		if w.System == system {
			wanted[w.Name] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	for i := range entries {
		for _, name := range entries[i].Names {
			if wanted[name] {
				entries[i].Wanted = true
				break
			}
		}
	}
	return nil
}
