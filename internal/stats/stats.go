// Package stats keeps how complete the collection was day by day and adds up the numbers for
// the statistics page.
package stats

import (
	"context"
	"sort"
	"time"

	"github.com/DeLFuS77/completist/internal/collection"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
)

// Days is how far back the history goes.
const Days = 366

// Record saves today's count of every system with a DAT.
func Record(ctx context.Context, s *store.Store, progress *collection.Cache, now time.Time) error {
	all, err := progress.All(ctx)
	if err != nil {
		return err
	}
	rows := []store.HistoryRow{}
	for _, p := range all {
		if !p.HasDat {
			continue
		}
		rows = append(rows, store.HistoryRow{System: p.System.ID, Have: p.Have, Total: p.Total, Files: p.Files.Files, Bytes: p.Files.Bytes})
	}
	return s.SaveHistory(ctx, now.Format("2006-01-02"), rows)
}

// Point is the whole collection on a day.
type Point struct {
	Day     string  `json:"day"`
	Have    int     `json:"have"`
	Total   int     `json:"total"`
	Percent float64 `json:"percent"`
	Bytes   int64   `json:"bytes"`
}

// Group is how complete a set of systems is: a maker, or a region.
type Group struct {
	Name    string  `json:"name"`
	Systems int     `json:"systems,omitempty"`
	Have    int     `json:"have"`
	Total   int     `json:"total"`
	Percent float64 `json:"percent"`
}

// SystemSize is the space a system takes.
type SystemSize struct {
	System string `json:"system"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// Close is a system near completion.
type Close struct {
	System  string  `json:"system"`
	Have    int     `json:"have"`
	Missing int     `json:"missing"`
	Percent float64 `json:"percent"`
}

// Stats is what the statistics page shows.
type Stats struct {
	Summary  collection.Summary `json:"summary"`
	History  []Point            `json:"history"`
	Makers   []Group            `json:"makers"`
	Regions  []Group            `json:"regions"`
	Sizes    []SystemSize       `json:"sizes"`
	Closest  []Close            `json:"closest"`
	Recent   []store.Recent     `json:"recent"`
	Wishlist struct {
		Total int `json:"total"`
		Owned int `json:"owned"`
	} `json:"wishlist"`
}

func percent(have, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(have) * 100 / float64(total)
}

// Build adds up the statistics.
func Build(ctx context.Context, s *store.Store, progress *collection.Cache, now time.Time) (Stats, error) {
	var out Stats
	all, err := progress.All(ctx)
	if err != nil {
		return out, err
	}
	out.Summary = collection.Summarize(all)
	prefs := collection.LoadPreferences(s)

	makers := map[string]*Group{}
	regions := map[string]*Group{}
	out.Sizes = []SystemSize{}
	out.Closest = []Close{}
	for _, p := range all {
		if p.Files.Files > 0 {
			out.Sizes = append(out.Sizes, SystemSize{System: p.System.ID, Files: p.Files.Files, Bytes: p.Files.Bytes})
		}
		if !p.HasDat {
			continue
		}
		m := makers[p.System.Maker]
		if m == nil {
			m = &Group{Name: p.System.Maker}
			makers[p.System.Maker] = m
		}
		m.Systems++
		m.Have += p.Have
		m.Total += p.Total
		if p.Have > 0 && p.Missing > 0 {
			out.Closest = append(out.Closest, Close{System: p.System.ID, Have: p.Have, Missing: p.Missing, Percent: p.Percent})
		}
		entries, err := collection.Entries(ctx, s, p.System.ID, p.Mode, prefs.Regions)
		if err != nil {
			return out, err
		}
		for _, e := range entries {
			for _, r := range e.Regions {
				g := regions[r]
				if g == nil {
					g = &Group{Name: r}
					regions[r] = g
				}
				g.Total++
				if e.Owned {
					g.Have++
				}
			}
		}
	}
	out.Makers = sortedGroups(makers)
	out.Regions = sortedGroups(regions)
	sort.Slice(out.Sizes, func(i, j int) bool { return out.Sizes[i].Bytes > out.Sizes[j].Bytes })
	// the ones with the fewest games left first
	sort.Slice(out.Closest, func(i, j int) bool {
		if out.Closest[i].Missing != out.Closest[j].Missing {
			return out.Closest[i].Missing < out.Closest[j].Missing
		}
		return out.Closest[i].Percent > out.Closest[j].Percent
	})
	if len(out.Closest) > 5 {
		out.Closest = out.Closest[:5]
	}

	if out.History, err = history(ctx, s, now); err != nil {
		return out, err
	}
	if out.Recent, err = s.RecentGames(ctx, 12); err != nil {
		return out, err
	}
	wishes, err := collection.Wishlist(ctx, s)
	if err != nil {
		return out, err
	}
	out.Wishlist.Total = len(wishes)
	for _, w := range wishes {
		if w.Owned {
			out.Wishlist.Owned++
		}
	}
	return out, nil
}

func sortedGroups(m map[string]*Group) []Group {
	out := make([]Group, 0, len(m))
	for _, g := range m {
		g.Percent = percent(g.Have, g.Total)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// history adds up the systems day by day. Each day lists every system counted then, so a
// system whose DAT was removed stops adding up.
func history(ctx context.Context, s *store.Store, now time.Time) ([]Point, error) {
	rows, err := s.History(ctx, now.AddDate(0, 0, -Days).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	out := []Point{}
	for i := 0; i < len(rows); {
		p := Point{Day: rows[i].Day}
		for ; i < len(rows) && rows[i].Day == p.Day; i++ {
			if _, ok := systems.ByID(rows[i].System); !ok {
				continue
			}
			p.Have += rows[i].Have
			p.Total += rows[i].Total
			p.Bytes += rows[i].Bytes
		}
		p.Percent = percent(p.Have, p.Total)
		out = append(out, p)
	}
	return out, nil
}
