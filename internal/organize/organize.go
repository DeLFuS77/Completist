// Package organize renames verified files to the names of their DATs and packs cartridge dumps
// in zip files, checked against the DAT before an original is removed. Every change is written
// to a journal, so it can be undone.
package organize

import (
	"archive/zip"
	"bufio"
	"compress/flate"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
)

// Kinds of changes.
const (
	KindRename = "rename"
	KindZip    = "zip"
)

const (
	journalFile = "organize.json"
	keepBatches = 50
)

// Move is a file to rename, or to pack.
type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Action is the change of one game: the files it renames, or the zip it makes.
type Action struct {
	ID     string `json:"id"`
	Game   string `json:"game"`
	GameID int64  `json:"gameId"`
	Moves  []Move `json:"moves"`
	// the CUE sheet rewritten with the new names of its tracks
	Sheet *Move `json:"sheet,omitempty"`
	// zip: the name of the file inside the zip, and what it must hash to
	Entry string `json:"entry,omitempty"`
	SHA1  string `json:"-"`
	CRC   string `json:"-"`
	// chd: the image it makes, cd or dvd, and the files it is made from
	Image     string   `json:"image,omitempty"`
	Sources   []string `json:"sources,omitempty"`
	LibraryID int64    `json:"-"`
	System    string   `json:"-"`
}

// Skipped is a file left as it is, and why.
type Skipped struct {
	Path   string `json:"path"`
	Game   string `json:"game"`
	Reason string `json:"reason"`
}

// Plan is what a change would do.
type Plan struct {
	Kind    string    `json:"kind"`
	System  string    `json:"system"`
	Actions []Action  `json:"actions"`
	Skipped []Skipped `json:"skipped"`
	// files already named as the DAT says, or already packed
	Done int `json:"done"`
}

// reasons, translated by the interface
const (
	ReasonReadOnly = "The library is read only."
	ReasonTaken    = "Another file has this name already."
	ReasonArchive  = "It is in an archive with other files."
	ReasonGDI      = "Tracks of a GDI dump keep their names."
	ReasonHeader   = "Recognised by its disc code only."
)

func actionID(paths ...string) string {
	sum := sha1.Sum([]byte(strings.Join(paths, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// safeName makes a DAT name a valid file name everywhere.
func safeName(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
}

var trackNumber = regexp.MustCompile(`(?i)\(track 0*(\d+)\)`)

// MakePlan works out what a change would do for a system.
func MakePlan(ctx context.Context, st *store.Store, system, kind string) (Plan, error) {
	plan := Plan{Kind: kind, System: system, Actions: []Action{}, Skipped: []Skipped{}}
	sys, ok := systems.ByID(system)
	if !ok {
		return plan, errors.New("Unknown system.")
	}
	if kind == KindZip && sys.Disc() {
		return plan, errors.New("Disc images are not packed in zip files.")
	}
	if kind == KindCHD && !sys.Disc() {
		return plan, errors.New("Only disc images become CHD files.")
	}
	owned, err := st.OwnedFiles(ctx, system)
	if err != nil {
		return plan, err
	}
	// names taken in the plan, so two files never get the same one
	taken := map[string]bool{}
	seenPath := map[string]bool{}
	for _, o := range owned {
		if seenPath[o.Path] {
			continue
		}
		seenPath[o.Path] = true
		skip := func(reason string) {
			plan.Skipped = append(plan.Skipped, Skipped{Path: o.Path, Game: o.Game, Reason: reason})
		}
		if o.ReadOnly {
			skip(ReasonReadOnly)
			continue
		}
		if o.AltForm == "header" {
			skip(ReasonHeader)
			continue
		}
		dir := filepath.Dir(o.Path)
		ext := strings.ToLower(filepath.Ext(o.Path))
		switch kind {
		case KindZip:
			if o.Entry != "" || ext == ".zip" {
				plan.Done++
				continue
			}
			target := filepath.Join(dir, safeName(o.Game)+".zip")
			if exists(target) || taken[target] {
				skip(ReasonTaken)
				continue
			}
			taken[target] = true
			entry := filepath.Base(o.Rom)
			if o.AltForm != "" && o.AltForm != "raw" {
				// a dump with a header keeps its own extension
				entry = strings.TrimSuffix(entry, filepath.Ext(entry)) + filepath.Ext(o.Path)
			}
			plan.Actions = append(plan.Actions, Action{ID: actionID(o.Path), Game: o.Game, GameID: o.GameID,
				Moves: []Move{{From: o.Path, To: target}}, Entry: safeName(entry), SHA1: o.SHA1, CRC: o.CRC})
		case KindCHD:
			a, reason, done := planCHD(o, sys, taken)
			switch {
			case done:
				plan.Done++
			case reason != "":
				skip(reason)
			default:
				plan.Actions = append(plan.Actions, a)
			}
		default:
			a, reason, done := renameAction(o, sys, taken)
			switch {
			case done:
				plan.Done++
			case reason != "":
				skip(reason)
			default:
				plan.Actions = append(plan.Actions, a)
			}
		}
	}
	return plan, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// renameAction works out the new names of a file and, for a disc, of its other tracks and its
// CUE sheet.
func renameAction(o store.Owned, sys *systems.System, taken map[string]bool) (Action, string, bool) {
	dir := filepath.Dir(o.Path)
	ext := filepath.Ext(o.Path)
	var target string
	switch {
	case o.Entry != "":
		// an archive or an image named after the game, when it holds only this game
		if o.Siblings > 1 && !strings.EqualFold(ext, ".chd") {
			return Action{}, ReasonArchive, false
		}
		target = filepath.Join(dir, safeName(o.Game)+ext)
	default:
		name := filepath.Base(o.Rom)
		if o.AltForm != "" && o.AltForm != "raw" {
			name = strings.TrimSuffix(name, filepath.Ext(name)) + ext
		}
		target = filepath.Join(dir, safeName(name))
	}
	if sys.Disc() && o.Entry == "" && strings.EqualFold(ext, ".bin") {
		return discAction(o, target, taken)
	}
	if target == o.Path {
		return Action{}, "", true
	}
	if (exists(target) && !sameFile(target, o.Path)) || taken[target] {
		return Action{}, ReasonTaken, false
	}
	taken[target] = true
	return Action{ID: actionID(o.Path), Game: o.Game, GameID: o.GameID, Moves: []Move{{From: o.Path, To: target}}}, "", false
}

// sameFile tells a file from another one whose name differs only in case, on systems that
// ignore case.
func sameFile(a, b string) bool {
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}

// discAction renames the tracks of a disc listed in its CUE sheet, and the sheet.
func discAction(o store.Owned, target string, taken map[string]bool) (Action, string, bool) {
	dir := filepath.Dir(o.Path)
	sheet, files := findSheet(dir, filepath.Base(o.Path))
	if sheet == "" {
		if sheetOf(dir, ".gdi") != "" {
			return Action{}, ReasonGDI, false
		}
		if target == o.Path {
			return Action{}, "", true
		}
		if (exists(target) && !sameFile(target, o.Path)) || taken[target] {
			return Action{}, ReasonTaken, false
		}
		taken[target] = true
		return Action{ID: actionID(o.Path), Game: o.Game, GameID: o.GameID, Moves: []Move{{From: o.Path, To: target}}}, "", false
	}
	// the other tracks follow the name of the first: "Game (USA) (Track 2).bin"
	first := filepath.Base(target)
	a := Action{ID: actionID(o.Path), Game: o.Game, GameID: o.GameID}
	all := true
	for _, f := range files {
		from := filepath.Join(dir, f.name)
		var to string
		if strings.EqualFold(f.name, filepath.Base(o.Path)) {
			to = target
		} else {
			to = filepath.Join(dir, trackName(first, f.track))
		}
		if from != to {
			all = false
			if (exists(to) && !sameFile(to, from)) || taken[to] {
				return Action{}, ReasonTaken, false
			}
		}
		a.Moves = append(a.Moves, Move{From: from, To: to})
	}
	release := trackNumber.ReplaceAllString(strings.TrimSuffix(first, filepath.Ext(first)), "")
	newSheet := filepath.Join(dir, strings.TrimSpace(release)+".cue")
	if all && newSheet == sheet {
		return Action{}, "", true
	}
	if newSheet != sheet && ((exists(newSheet) && !sameFile(newSheet, sheet)) || taken[newSheet]) {
		return Action{}, ReasonTaken, false
	}
	for _, m := range a.Moves {
		taken[m.To] = true
	}
	taken[newSheet] = true
	a.Sheet = &Move{From: sheet, To: newSheet}
	return a, "", false
}

// trackName is the name of track n of a disc whose first track is named first.
func trackName(first string, n int) string {
	m := trackNumber.FindStringSubmatchIndex(first)
	if m == nil {
		base := strings.TrimSuffix(first, filepath.Ext(first))
		return fmt.Sprintf("%s (Track %d)%s", base, n, filepath.Ext(first))
	}
	// keep the zero padding of the DAT: "Track 01" or "Track 1"
	padded := fmt.Sprintf("%0*d", m[3]-m[2], n)
	return first[:m[0]] + "(Track " + padded + ")" + first[m[1]:]
}

type sheetFile struct {
	name  string
	track int
}

func sheetOf(dir, ext string) string {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.EqualFold(filepath.Ext(e.Name()), ext) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// findSheet returns the CUE sheet in a folder that lists a file, and the files it lists with
// the first track of each.
func findSheet(dir, file string) (string, []sheetFile) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.EqualFold(filepath.Ext(e.Name()), ".cue") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		files := readCue(path)
		for _, f := range files {
			if strings.EqualFold(f.name, file) {
				return path, files
			}
		}
	}
	return "", nil
}

func readCue(path string) []sheetFile {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return nil
	}
	out := []sheetFile{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	current := -1
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "FILE "):
			name := cueFileName(line)
			if name == "" || strings.ContainsAny(name, `/\`) {
				// a sheet pointing to another folder is left alone
				return nil
			}
			out = append(out, sheetFile{name: name})
			current = len(out) - 1
		case strings.HasPrefix(upper, "TRACK ") && current >= 0 && out[current].track == 0:
			fmt.Sscanf(upper[6:], "%d", &out[current].track)
		}
	}
	return out
}

func cueFileName(line string) string {
	rest := strings.TrimSpace(line[5:])
	if strings.HasPrefix(rest, `"`) {
		if end := strings.Index(rest[1:], `"`); end >= 0 {
			return rest[1 : 1+end]
		}
		return ""
	}
	if fields := strings.Fields(rest); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// rewriteCue replaces the file names in a CUE sheet.
func rewriteCue(content string, names map[string]string) string {
	lines := strings.SplitAfter(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToUpper(trimmed), "FILE ") {
			continue
		}
		old := cueFileName(trimmed)
		if n, ok := names[strings.ToLower(old)]; ok {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			kind := strings.TrimSpace(trimmed[strings.LastIndex(trimmed, `"`)+1:])
			if !strings.Contains(trimmed, `"`) {
				fields := strings.Fields(trimmed)
				kind = fields[len(fields)-1]
			}
			end := line[len(strings.TrimRight(line, "\r\n")):]
			lines[i] = fmt.Sprintf(`%sFILE "%s" %s%s`, indent, n, kind, end)
		}
	}
	return strings.Join(lines, "")
}

// ---- the journal ----

// Change is one file changed, enough to undo it.
type Change struct {
	From string `json:"from"`
	To   string `json:"to"`
	// zip: the original was removed after the zip was checked
	Removed bool `json:"removed,omitempty"`
	// zip: the file name inside the zip and the SHA-1 of its content
	Entry string `json:"entry,omitempty"`
	SHA1  string `json:"sha1,omitempty"`
	// a CUE sheet: its content before
	Sheet string `json:"sheet,omitempty"`
	// chd: the files it was made from, and the CUE sheet that listed them
	Originals  []Original `json:"originals,omitempty"`
	CueSheet   string     `json:"cueSheet,omitempty"`
	CueRemoved bool       `json:"cueRemoved,omitempty"`
	// the game it belongs to in the batch: when one change of a game cannot be undone, the
	// others of that game are left as they are too
	Group int `json:"group"`
	// undone already, when undoing the batch stopped halfway
	Undone bool `json:"undone,omitempty"`
}

// Batch is a change made at once.
type Batch struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	System  string    `json:"system"`
	User    string    `json:"user,omitempty"`
	Changes []Change  `json:"changes"`
	Games   int       `json:"games"`
	Undone  bool      `json:"undone"`
	// bytes saved by packing
	Saved int64 `json:"saved,omitempty"`
}

// Journal keeps the batches, newest first.
type Journal struct {
	path  string
	mutex sync.Mutex
}

func OpenJournal(dataFolder string) *Journal {
	return &Journal{path: filepath.Join(dataFolder, journalFile)}
}

func (j *Journal) read() ([]Batch, error) {
	out := []Batch{}
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	// a damaged journal is never written over: its changes could no longer be undone
	if err := json.Unmarshal(data, &out); err != nil {
		return []Batch{}, fmt.Errorf("the list of changes (%s) is damaged: %w", journalFile, err)
	}
	return out, nil
}

func (j *Journal) write(batches []Batch) error {
	if len(batches) > keepBatches {
		batches = batches[:keepBatches]
	}
	data, err := json.MarshalIndent(batches, "", " ")
	if err != nil {
		return err
	}
	tmp := j.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, j.path)
}

// List returns the batches, newest first.
func (j *Journal) List() []Batch {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	batches, _ := j.read()
	return batches
}

func (j *Journal) add(b Batch) (Batch, error) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	batches, err := j.read()
	if err != nil {
		return b, err
	}
	b.ID = 1
	for _, old := range batches {
		b.ID = max(b.ID, old.ID+1)
	}
	return b, j.write(append([]Batch{b}, batches...))
}

// update replaces a batch.
func (j *Journal) update(b Batch) error {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	batches, err := j.read()
	if err != nil {
		return err
	}
	for i := range batches {
		if batches[i].ID == b.ID {
			batches[i] = b
		}
	}
	return j.write(batches)
}

// Get returns a batch.
func (j *Journal) Get(id int64) (Batch, bool) {
	for _, b := range j.List() {
		if b.ID == id {
			return b, true
		}
	}
	return Batch{}, false
}

// ---- applying ----

// Progress is told after each game.
type Progress func(done, total int, name string)

// Apply carries out the chosen actions of a plan and records them. Files renamed are moved in
// the store, so they are not read again; zips are checked against the DAT before the original
// is removed.
func Apply(ctx context.Context, st *store.Store, j *Journal, plan Plan, ids []string, removeOriginals bool, user string, progress Progress) (Batch, []Skipped, error) {
	chosen := map[string]bool{}
	for _, id := range ids {
		chosen[id] = true
	}
	batch := Batch{Time: time.Now().UTC(), Kind: plan.Kind, System: plan.System, User: user, Changes: []Change{}}
	failed := []Skipped{}
	actions := []Action{}
	for _, a := range plan.Actions {
		if len(ids) == 0 || chosen[a.ID] {
			actions = append(actions, a)
		}
	}
	for i, a := range actions {
		if ctx.Err() != nil {
			break
		}
		if progress != nil {
			progress(i, len(actions), a.Game)
		}
		var changes []Change
		var err error
		switch plan.Kind {
		case KindZip:
			var saved int64
			changes, saved, err = pack(a, removeOriginals)
			batch.Saved += saved
		case KindCHD:
			var saved int64
			changes, saved, err = packCHD(ctx, st, a, removeOriginals)
			batch.Saved += saved
		default:
			changes, err = rename(ctx, st, a)
		}
		if err != nil {
			path := ""
			if len(a.Moves) > 0 {
				path = a.Moves[0].From
			}
			failed = append(failed, Skipped{Path: path, Game: a.Game, Reason: err.Error()})
			continue
		}
		for k := range changes {
			changes[k].Group = i
		}
		batch.Changes = append(batch.Changes, changes...)
		batch.Games++
	}
	if batch.Games == 0 {
		return batch, failed, nil
	}
	saved, err := j.add(batch)
	return saved, failed, err
}

func rename(ctx context.Context, st *store.Store, a Action) ([]Change, error) {
	changes := []Change{}
	undo := func() {
		for i := len(changes) - 1; i >= 0; i-- {
			c := changes[i]
			if c.Sheet != "" {
				os.WriteFile(c.From, []byte(c.Sheet), 0o644)
				if c.To != c.From {
					os.Remove(c.To)
				}
				continue
			}
			os.Rename(c.To, c.From)
			st.MovePath(ctx, c.To, c.From)
		}
	}
	names := map[string]string{}
	for _, m := range a.Moves {
		names[strings.ToLower(filepath.Base(m.From))] = filepath.Base(m.To)
		if m.From == m.To {
			continue
		}
		if err := move(m.From, m.To); err != nil {
			undo()
			return nil, err
		}
		changes = append(changes, Change{From: m.From, To: m.To})
		if err := st.MovePath(ctx, m.From, m.To); err != nil {
			undo()
			return nil, err
		}
	}
	if a.Sheet != nil {
		old, err := os.ReadFile(a.Sheet.From)
		if err != nil {
			undo()
			return nil, err
		}
		sheetTmp := tempName(a.Sheet.To)
		if err := os.WriteFile(sheetTmp, []byte(rewriteCue(string(old), names)), 0o644); err != nil {
			os.Remove(sheetTmp)
			undo()
			return nil, err
		}
		if a.Sheet.To != a.Sheet.From {
			if err := os.Remove(a.Sheet.From); err != nil {
				os.Remove(sheetTmp)
				undo()
				return nil, err
			}
		}
		if err := os.Rename(sheetTmp, a.Sheet.To); err != nil {
			os.Remove(sheetTmp)
			os.WriteFile(a.Sheet.From, old, 0o644)
			undo()
			return nil, err
		}
		changes = append(changes, Change{From: a.Sheet.From, To: a.Sheet.To, Sheet: string(old)})
	}
	return changes, nil
}

// tempName is a name for a file being written next to path, which no other file has.
func tempName(path string) string {
	b := make([]byte, 6)
	rand.Read(b)
	return filepath.Join(filepath.Dir(path), ".completist-"+hex.EncodeToString(b)+".tmp")
}

// move renames a file, without replacing another one.
func move(from, to string) error {
	if exists(to) && !sameFile(from, to) {
		return errors.New(ReasonTaken)
	}
	if strings.EqualFold(from, to) && from != to {
		// only the case changes: through another name, for systems that ignore case
		tmp := tempName(to)
		if err := os.Rename(from, tmp); err != nil {
			return err
		}
		return os.Rename(tmp, to)
	}
	return os.Rename(from, to)
}

// pack writes the zip of a file, reads it back and checks it against the DAT, and only then
// removes the original.
func pack(a Action, removeOriginal bool) ([]Change, int64, error) {
	m := a.Moves[0]
	info, err := os.Stat(m.From)
	if err != nil {
		return nil, 0, err
	}
	if exists(m.To) {
		return nil, 0, errors.New(ReasonTaken)
	}
	tmp := tempName(m.To)
	if err := writeZip(m.From, tmp, a.Entry, info.ModTime()); err != nil {
		os.Remove(tmp)
		return nil, 0, err
	}
	if err := checkZip(tmp, a.Entry, a.SHA1, a.CRC, info.Size()); err != nil {
		os.Remove(tmp)
		return nil, 0, err
	}
	if err := os.Rename(tmp, m.To); err != nil {
		os.Remove(tmp)
		return nil, 0, err
	}
	var saved int64
	change := Change{From: m.From, To: m.To, Entry: a.Entry, SHA1: a.SHA1}
	if removeOriginal {
		if err := os.Remove(m.From); err == nil {
			change.Removed = true
			if zi, err := os.Stat(m.To); err == nil {
				saved = info.Size() - zi.Size()
			}
		}
	}
	return []Change{change}, saved, nil
}

func writeZip(from, to, entry string, modified time.Time) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	z := zip.NewWriter(out)
	z.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	h := &zip.FileHeader{Name: entry, Method: zip.Deflate, Modified: modified}
	w, err := z.CreateHeader(h)
	if err == nil {
		_, err = io.Copy(w, in)
	}
	if err == nil {
		err = z.Close()
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// checkZip reads the zip back: one entry, with the checksums of the DAT.
func checkZip(path, entry, wantSHA1, wantCRC string, size int64) error {
	z, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer z.Close()
	if len(z.File) != 1 || z.File[0].Name != entry {
		return errors.New("The zip was not written as expected.")
	}
	r, err := z.File[0].Open()
	if err != nil {
		return err
	}
	defer r.Close()
	h := sha1.New()
	c := crc32.NewIEEE()
	n, err := io.Copy(io.MultiWriter(h, c), r)
	if err != nil {
		return err
	}
	// a dump stored with a header matches the DAT without it: then the size and the checksums
	// of the file itself are compared
	if n != size {
		return errors.New("The zip does not hold the whole file.")
	}
	got := hex.EncodeToString(h.Sum(nil))
	if wantSHA1 != "" && got != wantSHA1 {
		return errors.New("The zip does not match the DAT.")
	}
	if wantSHA1 == "" && wantCRC != "" && fmt.Sprintf("%08x", c.Sum32()) != strings.ToLower(wantCRC) {
		return errors.New("The zip does not match the DAT.")
	}
	return nil
}

// ---- undoing ----

// Undo reverses a batch, newest change first. A change that cannot be undone is reported, the
// other changes of its game are left as they are, and the rest are undone; undoing again later
// retries what was left.
func Undo(ctx context.Context, st *store.Store, j *Journal, id int64) ([]Skipped, error) {
	b, ok := j.Get(id)
	if !ok {
		return nil, errors.New("Unknown change.")
	}
	if b.Undone {
		return nil, errors.New("This change was undone already.")
	}
	failed := []Skipped{}
	blocked := map[int]bool{}
	for i := len(b.Changes) - 1; i >= 0; i-- {
		c := &b.Changes[i]
		if c.Undone {
			continue
		}
		if blocked[c.Group] {
			failed = append(failed, Skipped{Path: c.To, Reason: "Left as it is: another file of the game could not be undone."})
			continue
		}
		var err error
		switch {
		case b.Kind == KindCHD:
			err = unpackCHD(ctx, st, *c)
		case c.Sheet != "":
			err = restoreSheet(*c)
		case b.Kind == KindZip:
			err = unpack(*c)
		default:
			if err = move(c.To, c.From); err == nil {
				err = st.MovePath(ctx, c.To, c.From)
			}
		}
		if err != nil {
			blocked[c.Group] = true
			failed = append(failed, Skipped{Path: c.To, Reason: err.Error()})
			continue
		}
		c.Undone = true
	}
	b.Undone = len(failed) == 0
	return failed, j.update(b)
}

// restoreSheet writes a CUE sheet back as it was, under its old name.
func restoreSheet(c Change) error {
	if c.To != c.From && exists(c.From) && !sameFile(c.From, c.To) {
		return errors.New(ReasonTaken)
	}
	tmp := tempName(c.From)
	if err := os.WriteFile(tmp, []byte(c.Sheet), 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if c.To != c.From {
		if err := os.Remove(c.To); err != nil && !errors.Is(err, os.ErrNotExist) {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, c.From); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// sameContent reports whether a file is still the one with this SHA-1.
func sameContent(path, sum string) bool {
	got, _, err := fileSHA1(path)
	return err == nil && got == sum
}

// unpack puts back an original removed after packing, checked, and removes the zip.
func unpack(c Change) error {
	if !c.Removed {
		// the zip goes only while the original is still there, as it was
		if c.SHA1 == "" || !sameContent(c.From, c.SHA1) {
			return errors.New("The original is no longer there as it was: the zip is kept.")
		}
		return os.Remove(c.To)
	}
	if exists(c.From) {
		return errors.New(ReasonTaken)
	}
	z, err := zip.OpenReader(c.To)
	if err != nil {
		return err
	}
	var entry *zip.File
	for _, f := range z.File {
		if f.Name == c.Entry {
			entry = f
		}
	}
	if entry == nil {
		z.Close()
		return errors.New("The zip no longer holds the game.")
	}
	r, err := entry.Open()
	if err != nil {
		z.Close()
		return err
	}
	tmp := tempName(c.From)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		r.Close()
		z.Close()
		return err
	}
	h := sha1.New()
	_, err = io.Copy(io.MultiWriter(out, h), r)
	r.Close()
	z.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && c.SHA1 != "" && hex.EncodeToString(h.Sum(nil)) != c.SHA1 {
		err = errors.New("The zip does not match the DAT.")
	}
	if err == nil {
		os.Chtimes(tmp, entry.Modified, entry.Modified)
		err = os.Rename(tmp, c.From)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Remove(c.To)
}

// SortSkipped orders skipped files by game.
func SortSkipped(s []Skipped) {
	sort.SliceStable(s, func(i, j int) bool { return s[i].Game < s[j].Game })
}
