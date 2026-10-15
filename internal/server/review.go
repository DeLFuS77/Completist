package server

import (
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/DeLFuS77/completist/internal/store"
)

// Why a file is to review. The interface explains each reason in words.
const (
	// the name of a game of the DAT, another size: another dump, a trimmed or padded file
	reasonSize = "size"
	// the name and the size of a game of the DAT, other content: another revision, a hack or
	// a damaged file
	reasonContent = "content"
	// betas, prototypes, demos and samples
	reasonPrerelease = "prerelease"
	// homebrew and unlicensed games
	reasonHomebrew = "homebrew"
	reasonPirate   = "pirate"
	// hacks, translations and known bad dumps, as GoodTools names them
	reasonHack = "hack"
	// test programs and other software that is not a game
	reasonProgram = "program"
	// no DAT for its system
	reasonNoDat = "nodat"
	reasonOther = "other"
)

var reviewTags = []struct {
	re     *regexp.Regexp
	reason string
}{
	{regexp.MustCompile(`(?i)\((beta|proto|prototype|alpha|demo|sample|preview|kiosk)\b[^)]*\)`), reasonPrerelease},
	{regexp.MustCompile(`(?i)\(pirate\)`), reasonPirate},
	{regexp.MustCompile(`(?i)\(hack\)|\[(t[+-]|h\d*|b\d*|a\d*|f\d*|o\d*|p\d*)[^\]]*\]|\(translat`), reasonHack},
	{regexp.MustCompile(`(?i)\((aftermarket|homebrew|unl)\)`), reasonHomebrew},
	{regexp.MustCompile(`(?i)\((program|test program|test cartridge)\)`), reasonProgram},
}

// reviewReason tells why a file not matched by its checksums is to review.
func reviewReason(f store.File, expected []store.Expected) string {
	switch f.Status {
	case "nodat":
		return reasonNoDat
	case "bad":
		for _, e := range expected {
			if e.Size == f.Size {
				return reasonContent
			}
		}
		return reasonSize
	}
	name := filepath.Base(f.Path)
	if f.Entry != "" {
		name += " " + f.Entry
	}
	for _, tag := range reviewTags {
		if tag.re.MatchString(name) {
			return tag.reason
		}
	}
	return reasonOther
}

type reviewFile struct {
	store.File
	Reason   string           `json:"reason"`
	Expected []store.Expected `json:"expected,omitempty"`
}

// reviewFiles lists the files to review of a system, each with why, how many there are for
// each reason, and the DAT they were compared with.
func (s *Server) reviewFiles(w http.ResponseWriter, r *http.Request) {
	system := r.PathValue("id")
	status := r.URL.Query().Get("status")
	statuses := []string{"bad"}
	if status == "unknown" {
		statuses = []string{"unknown", "nodat"}
	}
	expected, err := s.Store.ExpectedByStem(r.Context(), system)
	if err != nil {
		s.fail(w, err)
		return
	}
	reason := r.URL.Query().Get("reason")
	reasons := map[string]int{}
	out := []reviewFile{}
	for _, st := range statuses {
		files, err := s.Store.FilesWithStatus(r.Context(), system, st, 1_000_000)
		if err != nil {
			s.fail(w, err)
			return
		}
		for _, f := range files {
			rf := reviewFile{File: f, Expected: expected[f.Stem]}
			rf.Reason = reviewReason(f, rf.Expected)
			reasons[rf.Reason]++
			if (reason == "" || reason == rf.Reason) && len(out) < 1000 {
				out = append(out, rf)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b reviewFile) int { return strings.Compare(a.Path, b.Path) })
	var dat *store.DatInfo
	if dats, err := s.Store.Dats(); err == nil {
		for _, d := range dats {
			if d.System == system {
				dat = &d
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out, "reasons": reasons, "dat": dat})
}
