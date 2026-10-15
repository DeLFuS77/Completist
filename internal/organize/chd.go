package organize

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeLFuS77/completist/internal/chd"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
)

// KindCHD turns BIN/CUE dumps and ISO files of disc systems into CHD images.
const KindCHD = "chd"

// reasons, translated by the interface
const (
	ReasonDolphin = "Dolphin does not read CHD: RVZ is its compressed format."
	ReasonNoSheet = "No CUE sheet lists it."
	ReasonLayout  = "Its CUE sheet is not supported."
)

// Original is a file a CHD was made from, to make it again when the change is undone.
type Original struct {
	Path string `json:"path"`
	SHA1 string `json:"sha1"`
	// the tracks of the CHD it holds, in order; none for an ISO
	Tracks []int `json:"tracks,omitempty"`
	// removed once the CHD was checked
	Removed bool `json:"removed,omitempty"`
}

func planCHD(o store.Owned, sys *systems.System, taken map[string]bool) (Action, string, bool) {
	dir := filepath.Dir(o.Path)
	ext := strings.ToLower(filepath.Ext(o.Path))
	if sys.ID == "gamecube" || sys.ID == "wii" {
		return Action{}, ReasonDolphin, false
	}
	if ext == ".chd" {
		return Action{}, "", true
	}
	if o.Entry != "" {
		return Action{}, ReasonArchive, false
	}
	target := filepath.Join(dir, safeName(o.Game)+".chd")
	if exists(target) || taken[target] {
		return Action{}, ReasonTaken, false
	}
	a := Action{ID: actionID(o.Path), Game: o.Game, GameID: o.GameID, SHA1: o.SHA1, LibraryID: o.LibraryID, System: o.System}
	switch ext {
	case ".iso":
		if info, err := os.Stat(o.Path); err != nil || info.Size()%2048 != 0 {
			return Action{}, ReasonLayout, false
		}
		a.Image = "dvd"
		a.Moves = []Move{{From: o.Path, To: target}}
	case ".bin", ".img":
		sheet, _ := findSheet(dir, filepath.Base(o.Path))
		if sheet == "" {
			if sheetOf(dir, ".gdi") != "" {
				return Action{}, ReasonGDI, false
			}
			return Action{}, ReasonNoSheet, false
		}
		tracks, err := chd.ParseCue(sheet)
		if err != nil {
			return Action{}, ReasonLayout, false
		}
		// every file must be read whole by its tracks, one after the other, so it can be made again
		files := map[string]bool{}
		for i, t := range tracks {
			continues := i > 0 && tracks[i-1].File == t.File
			if !continues && (files[t.File] || t.Offset != 0) {
				return Action{}, ReasonLayout, false
			}
			files[t.File] = true
			if i == 0 && t.Type == "AUDIO" {
				return Action{}, ReasonLayout, false
			}
		}
		a.Image = "cd"
		a.Moves = []Move{{From: sheet, To: target}}
		for _, t := range tracks {
			if len(a.Sources) == 0 || a.Sources[len(a.Sources)-1] != filepath.Base(t.File) {
				a.Sources = append(a.Sources, filepath.Base(t.File))
			}
		}
	default:
		return Action{}, ReasonLayout, false
	}
	taken[target] = true
	return a, "", false
}

type hasher struct {
	s, m hash.Hash
	c    hash.Hash32
}

func newHashes() hasher {
	return hasher{s: sha1.New(), m: md5.New(), c: crc32.NewIEEE()}
}

func (h hasher) sums(size int64) entrySums {
	return entrySums{Size: size, CRC: crcHex(h.c.Sum32()), MD5: hex.EncodeToString(h.m.Sum(nil)), SHA1: hex.EncodeToString(h.s.Sum(nil))}
}

func fileSHA1(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha1.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// packCHD writes the CHD, reads it back and checks every file it was made from, records it in
// the store with the checksums the DAT knows, and only then removes the originals.
func packCHD(ctx context.Context, st *store.Store, a Action, removeOriginals bool) ([]Change, int64, error) {
	m := a.Moves[0]
	tmp := tempName(m.To)
	originals := []Original{}
	var tracks []chd.TrackSource
	var err error
	if a.Image == "dvd" {
		err = chd.WriteDVD(tmp, m.From, nil)
		originals = append(originals, Original{Path: m.From})
	} else {
		tracks, err = chd.ParseCue(m.From)
		if err == nil {
			err = chd.WriteCD(tmp, tracks, nil)
		}
		for _, t := range tracks {
			if len(originals) == 0 || originals[len(originals)-1].Path != t.File {
				originals = append(originals, Original{Path: t.File})
			}
			originals[len(originals)-1].Tracks = append(originals[len(originals)-1].Tracks, t.Number)
		}
	}
	if err != nil {
		os.Remove(tmp)
		return nil, 0, err
	}
	var before int64
	for i := range originals {
		sum, size, err := fileSHA1(originals[i].Path)
		if err != nil {
			os.Remove(tmp)
			return nil, 0, err
		}
		originals[i].SHA1 = sum
		before += size
	}

	// read back: each original as the CHD gives it, and what identifies the game
	entry, sums, err := verifyCHD(tmp, originals)
	if err != nil {
		os.Remove(tmp)
		return nil, 0, err
	}
	if a.SHA1 != "" && sums.SHA1 != a.SHA1 {
		os.Remove(tmp)
		return nil, 0, errors.New("The CHD does not match the DAT.")
	}
	if exists(m.To) {
		os.Remove(tmp)
		return nil, 0, errors.New(ReasonTaken)
	}
	if err := os.Rename(tmp, m.To); err != nil {
		os.Remove(tmp)
		return nil, 0, err
	}
	info, err := os.Stat(m.To)
	if err != nil {
		return nil, 0, err
	}
	// the store knows the new file at once, so it is not read again
	f := store.File{LibraryID: a.LibraryID, Path: m.To, Entry: entry, Stem: store.Stem(filepath.Base(m.To)), Size: sums.Size,
		Mtime: info.ModTime().Unix(), System: a.System, CRC: sums.CRC, MD5: sums.MD5, SHA1: sums.SHA1, Status: "unknown"}
	if err := st.SaveFiles(ctx, []store.File{f}); err != nil {
		return nil, 0, err
	}
	change := Change{From: m.From, To: m.To, Originals: originals}
	if a.Image == "cd" {
		sheet, err := os.ReadFile(m.From)
		if err != nil {
			return nil, 0, err
		}
		change.CueSheet = string(sheet)
	}
	var saved int64
	if removeOriginals {
		// each removal is recorded, so undoing makes again exactly what is gone
		removed := []store.Key{}
		all := true
		for i := range change.Originals {
			if err := os.Remove(change.Originals[i].Path); err != nil {
				all = false
				continue
			}
			change.Originals[i].Removed = true
			change.Removed = true
			removed = append(removed, store.Key{Path: change.Originals[i].Path})
		}
		if all && a.Image == "cd" {
			change.CueRemoved = os.Remove(m.From) == nil
		}
		st.RemoveFiles(ctx, a.LibraryID, removed)
		if all {
			saved = before - info.Size()
		}
	}
	return []Change{change}, saved, nil
}

type entrySums struct {
	Size           int64
	CRC, MD5, SHA1 string
}

// verifyCHD compares what a CHD gives back with its originals, and returns the entry that
// identifies the game with its checksums.
func verifyCHD(path string, originals []Original) (string, entrySums, error) {
	c, err := chd.Open(path)
	if err != nil {
		return "", entrySums{}, err
	}
	defer c.Close()
	var sums entrySums
	mismatch := errors.New("The CHD does not give back the original files.")
	if c.DVD() {
		h := newHashes()
		n, err := c.WriteTo(io.MultiWriter(h.s, h.m, h.c))
		if err != nil {
			return "", sums, err
		}
		sums = h.sums(n)
		if len(originals) != 1 || sums.SHA1 != originals[0].SHA1 {
			return "", sums, mismatch
		}
		return chd.ImageEntry, sums, nil
	}
	tracks, err := c.Tracks()
	if err != nil {
		return "", sums, err
	}
	byNumber := map[int]*chd.Track{}
	for _, t := range tracks {
		byNumber[t.Number] = t
	}
	first := tracks[0].Number
	for _, o := range originals {
		whole := sha1.New()
		for _, n := range o.Tracks {
			t, ok := byNumber[n]
			if !ok {
				return "", sums, mismatch
			}
			var w io.Writer = whole
			var h hasher
			if n == first {
				h = newHashes()
				w = io.MultiWriter(whole, h.s, h.m, h.c)
			}
			size, err := t.WriteTo(w)
			if err != nil {
				return "", sums, err
			}
			if n == first {
				sums = h.sums(size)
			}
		}
		if hex.EncodeToString(whole.Sum(nil)) != o.SHA1 {
			return "", sums, mismatch
		}
	}
	return chd.TrackEntry(first), sums, nil
}

func crcHex(v uint32) string {
	const digits = "0123456789abcdef"
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = digits[v&15]
		v >>= 4
	}
	return string(b)
}

// unpackCHD makes again the originals that were removed, checks every original, and only then
// removes the CHD.
func unpackCHD(ctx context.Context, st *store.Store, c Change) error {
	img, err := chd.Open(c.To)
	if err != nil {
		return err
	}
	defer img.Close()
	var tracks []*chd.Track
	if !img.DVD() {
		if tracks, err = img.Tracks(); err != nil {
			return err
		}
	}
	written := []string{}
	fail := func(err error) error {
		for _, p := range written {
			os.Remove(p)
		}
		return err
	}
	for _, o := range c.Originals {
		if !o.Removed {
			// kept: it must still be there as it was, or the CHD is its only copy
			if !sameContent(o.Path, o.SHA1) {
				return fail(errors.New("An original is no longer there as it was: the CHD is kept."))
			}
			continue
		}
		if exists(o.Path) {
			return fail(errors.New(ReasonTaken))
		}
		tmp := tempName(o.Path)
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return fail(err)
		}
		h := sha1.New()
		w := io.MultiWriter(out, h)
		if tracks == nil {
			_, err = img.WriteTo(w)
		} else {
			for _, n := range o.Tracks {
				for _, t := range tracks {
					if t.Number == n && err == nil {
						_, err = t.WriteTo(w)
					}
				}
			}
		}
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil && hex.EncodeToString(h.Sum(nil)) != o.SHA1 {
			err = errors.New("The CHD does not give back the original files.")
		}
		if err == nil {
			err = os.Rename(tmp, o.Path)
		}
		if err != nil {
			os.Remove(tmp)
			return fail(err)
		}
		written = append(written, o.Path)
	}
	if c.CueSheet != "" {
		if c.CueRemoved {
			if exists(c.From) {
				return fail(errors.New(ReasonTaken))
			}
			if err := os.WriteFile(c.From, []byte(c.CueSheet), 0o644); err != nil {
				return fail(err)
			}
		} else if !exists(c.From) {
			return fail(errors.New("An original is no longer there as it was: the CHD is kept."))
		}
	}
	img.Close()
	if err := os.Remove(c.To); err != nil {
		return err
	}
	// the files are right whatever the store says: the scan that follows brings it up to date
	st.RemovePath(ctx, c.To)
	return nil
}
