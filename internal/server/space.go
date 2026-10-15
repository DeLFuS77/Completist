package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeLFuS77/completist/internal/systems"
)

// Amount is a number of files and the bytes they take on the disk.
type Amount struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

func (a *Amount) add(bytes int64) {
	a.Files++
	a.Bytes += bytes
}

type spaceFormat struct {
	Format string `json:"format"`
	Amount
}

type spaceSystem struct {
	System     string        `json:"system"`
	Total      Amount        `json:"total"`
	Formats    []spaceFormat `json:"formats"`
	Duplicates Amount        `json:"duplicates"`
	Unknown    Amount        `json:"unknown"`
	Bad        Amount        `json:"bad"`
	// verified files that Organize can compress, and into what
	Compressible Amount `json:"compressible"`
	CompressTo   string `json:"compressTo,omitempty"`
}

// compressTo is what Organize makes of a verified loose file of a system, if anything.
func compressTo(sys *systems.System, ext string) string {
	switch {
	case sys == nil:
		return ""
	case !sys.Disc():
		if ext != ".zip" && ext != ".7z" {
			return "zip"
		}
	case sys.ID == "psp":
		if ext == ".iso" {
			return "cso"
		}
	case sys.ID == "gamecube" || sys.ID == "wii":
		if ext == ".iso" || ext == ".gcm" {
			return "gcz"
		}
	default:
		if ext == ".bin" || ext == ".img" || ext == ".iso" {
			return "chd"
		}
	}
	return ""
}

func (s *Server) space(w http.ResponseWriter, r *http.Request) {
	files, err := s.Store.SpaceFiles(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	// the size on the disk, once for an archive or an image with several entries
	diskSize := map[string]int64{}
	sizeOf := func(path string) int64 {
		if n, ok := diskSize[path]; ok {
			return n
		}
		var n int64
		if info, err := os.Stat(path); err == nil {
			n = info.Size()
		}
		diskSize[path] = n
		return n
	}
	bySystem := map[string]*spaceSystem{}
	formats := map[string]map[string]*Amount{}
	counted := map[string]bool{}
	seenRom := map[int64]bool{}
	var total Amount
	for _, f := range files {
		ss := bySystem[f.System]
		if ss == nil {
			ss = &spaceSystem{System: f.System, Formats: []spaceFormat{}}
			bySystem[f.System] = ss
			formats[f.System] = map[string]*Amount{}
		}
		ext := strings.ToLower(filepath.Ext(f.Path))
		// what this file takes: the whole file the first time it is seen
		var bytes int64
		if !counted[f.Path] {
			counted[f.Path] = true
			bytes = sizeOf(f.Path)
			ss.Total.add(bytes)
			total.add(bytes)
			format := strings.TrimPrefix(ext, ".")
			if format == "" {
				format = "?"
			}
			a := formats[f.System][format]
			if a == nil {
				a = &Amount{}
				formats[f.System][format] = a
			}
			a.add(bytes)
		}
		// an entry of an archive counts with its own size
		share := bytes
		if f.Entry != "" {
			share = f.Size
		}
		switch f.Status {
		case "ok":
			if seenRom[f.RomID] {
				ss.Duplicates.add(share)
				continue
			}
			seenRom[f.RomID] = true
			sys, _ := systems.ByID(f.System)
			if f.Entry == "" {
				if to := compressTo(sys, ext); to != "" {
					ss.Compressible.add(share)
					ss.CompressTo = to
				}
			}
		case "bad":
			ss.Bad.add(share)
		case "unknown", "nodat":
			ss.Unknown.add(share)
		}
	}
	out := []*spaceSystem{}
	for id, ss := range bySystem {
		for name, a := range formats[id] {
			ss.Formats = append(ss.Formats, spaceFormat{Format: name, Amount: *a})
		}
		sort.Slice(ss.Formats, func(i, j int) bool { return ss.Formats[i].Bytes > ss.Formats[j].Bytes })
		out = append(out, ss)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Total.Bytes > out[j].Total.Bytes })
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "systems": out})
}
