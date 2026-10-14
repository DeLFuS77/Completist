// Package collection works out how complete a collection is: how many games of each system
// you have and how many are missing, counted the way you choose.
package collection

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/DeLFuS77/completist/internal/dat"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
)

// Modes say what a complete set is.
const (
	// every entry of the DAT
	ModeFull = "full"
	// official retail releases: no betas, prototypes, demos, unlicensed games, hacks or BIOS
	ModeOfficial = "official"
	// one version of each official game: any region or revision counts
	Mode1G1R = "1g1r"
)

var Modes = []string{ModeFull, ModeOfficial, Mode1G1R}

func ValidMode(m string) bool {
	for _, v := range Modes {
		if v == m {
			return true
		}
	}
	return false
}

// Preferences are how the user counts.
type Preferences struct {
	// mode used by systems without their own
	DefaultMode string `json:"defaultMode"`
	// mode per system id
	Modes map[string]string `json:"modes"`
	// regions in order of preference, for choosing the version shown in 1G1R
	Regions []string `json:"regions"`
}

func DefaultPreferences() Preferences {
	return Preferences{DefaultMode: Mode1G1R, Modes: map[string]string{}, Regions: []string{"Europe", "World", "USA", "Japan"}}
}

func (p Preferences) ModeOf(system string) string {
	if m, ok := p.Modes[system]; ok && ValidMode(m) {
		return m
	}
	if ValidMode(p.DefaultMode) {
		return p.DefaultMode
	}
	return Mode1G1R
}

const settingsKey = "collection"

// LoadPreferences reads the preferences, or the defaults.
func LoadPreferences(s *store.Store) Preferences {
	p := DefaultPreferences()
	s.GetSetting(settingsKey, &p)
	if p.Modes == nil {
		p.Modes = map[string]string{}
	}
	if len(p.Regions) == 0 {
		p.Regions = DefaultPreferences().Regions
	}
	return p
}

func SavePreferences(s *store.Store, p Preferences) error {
	return s.SetSetting(settingsKey, p)
}

// Entry is one thing to collect: a game of the DAT, or in 1G1R a game with all its versions.
type Entry struct {
	// id of the game shown: the owned version, or else the preferred one
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Regions  []string `json:"regions"`
	Flags    []string `json:"flags"`
	Owned    bool     `json:"owned"`
	Files    int      `json:"files"`
	Versions int      `json:"versions"`
	// on the wishlist: this game or another version of it
	Wanted bool `json:"wanted"`
	// the names of every version counted as this entry
	Names []string `json:"-"`
}

// Progress is how complete a system is.
type Progress struct {
	System  systems.System         `json:"system"`
	Mode    string                 `json:"mode"`
	HasDat  bool                   `json:"hasDat"`
	Total   int                    `json:"total"`
	Have    int                    `json:"have"`
	Missing int                    `json:"missing"`
	Percent float64                `json:"percent"`
	Files   store.SystemFileCounts `json:"files"`
}

// Entries returns what there is to collect for a system, in a mode.
func Entries(ctx context.Context, s *store.Store, system string, mode string, regions []string) ([]Entry, error) {
	rows, err := s.Games(ctx, system)
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	switch mode {
	case ModeFull, ModeOfficial:
		for _, g := range rows {
			if mode == ModeOfficial && !dat.Flags(g.Flags).Official() {
				continue
			}
			entries = append(entries, entry(g, 1))
		}
	default:
		// a game is grouped with its parent when the DAT says (cloneof), else by its title
		titleOf := map[string]string{}
		for _, g := range rows {
			titleOf[g.Name] = g.Title
		}
		groups := map[string][]store.GameRow{}
		order := []string{}
		for _, g := range rows {
			if !dat.Flags(g.Flags).Official() {
				continue
			}
			key := strings.ToLower(g.Title)
			if parent, ok := titleOf[g.CloneOf]; ok && g.CloneOf != "" {
				key = strings.ToLower(parent)
			}
			if _, ok := groups[key]; !ok {
				order = append(order, key)
			}
			groups[key] = append(groups[key], g)
		}
		for _, key := range order {
			group := groups[key]
			// show the version you have, or else the one you would want
			owned := []store.GameRow{}
			files := 0
			for _, g := range group {
				if g.Have > 0 {
					owned = append(owned, g)
					files += g.Have
				}
			}
			var e Entry
			if len(owned) > 0 {
				e = entry(pick(owned, regions), len(group))
				e.Files = files
			} else {
				e = entry(pick(group, regions), len(group))
			}
			e.Names = make([]string, len(group))
			for i, g := range group {
				e.Names[i] = g.Name
			}
			entries = append(entries, e)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func entry(g store.GameRow, versions int) Entry {
	e := Entry{ID: g.ID, Name: g.Name, Title: g.Title, Flags: dat.Flags(g.Flags).Names(), Owned: g.Have > 0, Files: g.Have, Versions: versions, Regions: []string{}, Names: []string{g.Name}}
	if g.Regions != "" {
		e.Regions = strings.Split(g.Regions, ",")
	}
	return e
}

// pick chooses the version to show for a game: the preferred region, then the latest revision.
func pick(group []store.GameRow, regions []string) store.GameRow {
	rank := func(g store.GameRow) int {
		for i, want := range regions {
			for _, r := range strings.Split(g.Regions, ",") {
				if strings.EqualFold(strings.TrimSpace(r), want) {
					return i
				}
			}
		}
		return len(regions)
	}
	best := group[0]
	for _, g := range group[1:] {
		rb, rg := rank(best), rank(g)
		if rg < rb || (rg == rb && g.Revision > best.Revision) {
			best = g
		}
	}
	return best
}

// Cache keeps the progress of every system until the library or a DAT changes.
type Cache struct {
	store *store.Store
	mutex sync.Mutex
	valid bool
	all   []Progress
}

func NewCache(s *store.Store) *Cache {
	return &Cache{store: s}
}

// Invalidate makes the next call compute again.
func (c *Cache) Invalidate() {
	c.mutex.Lock()
	c.valid = false
	c.mutex.Unlock()
}

// All returns the progress of the systems that have a DAT or files.
func (c *Cache) All(ctx context.Context) ([]Progress, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.valid {
		return c.all, nil
	}
	prefs := LoadPreferences(c.store)
	dats, err := c.store.Dats()
	if err != nil {
		return nil, err
	}
	hasDat := map[string]bool{}
	for _, d := range dats {
		hasDat[d.System] = true
	}
	counts, err := c.store.FileCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := []Progress{}
	for _, sys := range systems.All {
		files := counts[sys.ID]
		if !hasDat[sys.ID] && files == nil {
			continue
		}
		p := Progress{System: sys, Mode: prefs.ModeOf(sys.ID), HasDat: hasDat[sys.ID]}
		if files != nil {
			p.Files = *files
		}
		if p.HasDat {
			entries, err := Entries(ctx, c.store, sys.ID, p.Mode, prefs.Regions)
			if err != nil {
				return nil, err
			}
			p.Total = len(entries)
			for _, e := range entries {
				if e.Owned {
					p.Have++
				}
			}
			p.Missing = p.Total - p.Have
			if p.Total > 0 {
				p.Percent = float64(p.Have) * 100 / float64(p.Total)
			}
		}
		out = append(out, p)
	}
	c.all, c.valid = out, true
	return out, nil
}

// Summary adds up the systems that have a DAT.
type Summary struct {
	Systems int     `json:"systems"`
	Total   int     `json:"total"`
	Have    int     `json:"have"`
	Missing int     `json:"missing"`
	Percent float64 `json:"percent"`
	Files   int     `json:"files"`
	Bytes   int64   `json:"bytes"`
	Bad     int     `json:"bad"`
	Unknown int     `json:"unknown"`
	Dups    int     `json:"duplicates"`
}

func Summarize(all []Progress) Summary {
	s := Summary{}
	for _, p := range all {
		s.Files += p.Files.Files
		s.Bytes += p.Files.Bytes
		s.Bad += p.Files.Bad
		s.Unknown += p.Files.Unknown + p.Files.NoDat
		s.Dups += p.Files.Duplicates
		if !p.HasDat {
			continue
		}
		s.Systems++
		s.Total += p.Total
		s.Have += p.Have
	}
	s.Missing = s.Total - s.Have
	if s.Total > 0 {
		s.Percent = float64(s.Have) * 100 / float64(s.Total)
	}
	return s
}
