package organize

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeLFuS77/completist/internal/discimage"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
)

// Kinds that compress the ISO of a system into the format its emulators read: CSO for PSP,
// GCZ for GameCube and Wii. Both give back the same ISO, bit for bit.
const (
	KindCSO = "cso"
	KindGCZ = "gcz"
)

// ReasonNotISO is why a file in another format is left as it is.
const ReasonNotISO = "Only ISO files are compressed this way."

// compressed images are not compressed again
var alreadyCompressed = map[string]bool{".chd": true, ".cso": true, ".gcz": true, ".rvz": true, ".wia": true, ".wbfs": true, ".zip": true, ".7z": true}

// ImageKindFor is the compressed image a system's ISO files become, if any.
func ImageKindFor(system string) string {
	switch system {
	case "psp":
		return KindCSO
	case "gamecube", "wii":
		return KindGCZ
	}
	return ""
}

func planImage(o store.Owned, kind string, taken map[string]bool) (Action, string, bool) {
	ext := strings.ToLower(filepath.Ext(o.Path))
	if ext == "."+kind || alreadyCompressed[ext] {
		return Action{}, "", true
	}
	if o.Entry != "" {
		return Action{}, ReasonArchive, false
	}
	if ext != ".iso" && ext != ".gcm" {
		return Action{}, ReasonNotISO, false
	}
	if info, err := os.Stat(o.Path); err != nil || info.Size() == 0 || info.Size()%2048 != 0 {
		return Action{}, ReasonLayout, false
	}
	target := filepath.Join(filepath.Dir(o.Path), safeName(o.Game)+"."+kind)
	if exists(target) || taken[target] {
		return Action{}, ReasonTaken, false
	}
	taken[target] = true
	return Action{ID: actionID(o.Path), Game: o.Game, GameID: o.GameID, SHA1: o.SHA1, LibraryID: o.LibraryID, System: o.System,
		Image: kind, Moves: []Move{{From: o.Path, To: target}}}, "", false
}

// packImage writes the compressed image, reads it back whole and compares it with the ISO,
// records it in the store, and only then removes the ISO.
func packImage(ctx context.Context, st *store.Store, a Action, kind string, removeOriginal bool) ([]Change, int64, error) {
	m := a.Moves[0]
	tmp := tempName(m.To) + "." + kind
	var err error
	if kind == KindCSO {
		err = discimage.WriteCSO(tmp, m.From, nil)
	} else {
		err = discimage.WriteGCZ(tmp, m.From, a.System == "wii", nil)
	}
	if err != nil {
		os.Remove(tmp)
		return nil, 0, err
	}
	fail := func(err error) ([]Change, int64, error) {
		os.Remove(tmp)
		return nil, 0, err
	}
	sum, before, err := fileSHA1(m.From)
	if err != nil {
		return fail(err)
	}
	img, err := discimage.Open(tmp)
	if err != nil {
		return fail(err)
	}
	h := newHashes()
	n, err := img.WriteTo(io.MultiWriter(h.s, h.m, h.c))
	img.Close()
	if err != nil {
		return fail(err)
	}
	sums := h.sums(n)
	if sums.SHA1 != sum {
		return fail(errors.New("The compressed image does not give back the ISO."))
	}
	if a.SHA1 != "" && sums.SHA1 != a.SHA1 {
		return fail(errors.New("The compressed image does not match the DAT."))
	}
	if exists(m.To) {
		return fail(errors.New(ReasonTaken))
	}
	if err := os.Rename(tmp, m.To); err != nil {
		return fail(err)
	}
	info, err := os.Stat(m.To)
	if err != nil {
		return nil, 0, err
	}
	f := store.File{LibraryID: a.LibraryID, Path: m.To, Stem: store.Stem(filepath.Base(m.To)), Size: sums.Size,
		Mtime: info.ModTime().Unix(), System: a.System, CRC: sums.CRC, MD5: sums.MD5, SHA1: sums.SHA1, Status: "unknown"}
	if err := st.SaveFiles(ctx, []store.File{f}); err != nil {
		return nil, 0, err
	}
	change := Change{From: m.From, To: m.To, Originals: []Original{{Path: m.From, SHA1: sum}}}
	var saved int64
	if removeOriginal && os.Remove(m.From) == nil {
		change.Originals[0].Removed = true
		change.Removed = true
		st.RemoveFiles(ctx, a.LibraryID, []store.Key{{Path: m.From}})
		saved = before - info.Size()
	}
	return []Change{change}, saved, nil
}

// unpackImage writes the ISO back if it was removed, checks it, and only then removes the
// compressed image.
func unpackImage(ctx context.Context, st *store.Store, c Change) error {
	if len(c.Originals) != 1 {
		return errors.New("Unknown change.")
	}
	o := c.Originals[0]
	if !o.Removed {
		if !sameContent(o.Path, o.SHA1) {
			return errors.New("An original is no longer there as it was: the compressed image is kept.")
		}
	} else {
		if exists(o.Path) {
			return errors.New(ReasonTaken)
		}
		img, err := discimage.Open(c.To)
		if err != nil {
			return err
		}
		tmp := tempName(o.Path)
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			img.Close()
			return err
		}
		h := sha1.New()
		_, err = img.WriteTo(io.MultiWriter(out, h))
		img.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil && hex.EncodeToString(h.Sum(nil)) != o.SHA1 {
			err = errors.New("The compressed image does not give back the ISO.")
		}
		if err == nil {
			err = os.Rename(tmp, o.Path)
		}
		if err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Remove(c.To); err != nil {
		return err
	}
	st.RemovePath(ctx, c.To)
	return nil
}

func imageSystemOK(kind string, sys *systems.System) bool {
	return ImageKindFor(sys.ID) == kind
}

// ValidKind reports whether a kind of change exists.
func ValidKind(kind string) bool {
	switch kind {
	case KindRename, KindZip, KindCHD, KindCSO, KindGCZ:
		return true
	}
	return false
}
