// Package scanner reads the libraries: it finds the game files, hashes the new and changed
// ones and records them, so the store can tell which game each one is.
package scanner

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeLFuS77/completist/internal/chd"
	"github.com/DeLFuS77/completist/internal/discimage"
	"github.com/DeLFuS77/completist/internal/platform"
	_ "github.com/DeLFuS77/completist/internal/platform/cartridge"
	_ "github.com/DeLFuS77/completist/internal/platform/disc"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
	"github.com/DeLFuS77/completist/internal/tasks"
)

// Scanner scans the libraries, one scan at a time.
type Scanner struct {
	store   *store.Store
	tasks   *tasks.Log
	workers int
	running sync.Mutex
	// a scan was asked for while one was running: it runs once that one ends
	pending atomic.Bool
	// called after every scan, to drop cached results
	OnChange func()
	// stops the scans when the app stops; Background when not set
	Context context.Context
}

func New(s *store.Store, log *tasks.Log, workers int) *Scanner {
	if workers <= 0 {
		workers = min(4, runtime.NumCPU())
	}
	return &Scanner{store: s, tasks: log, workers: workers}
}

var ErrBusy = errors.New("a scan is already running")

// Start scans every library in the background. When a scan is running already, another one
// follows it, so a library added meanwhile is read too; ErrBusy tells it.
func (sc *Scanner) Start(trigger string) error {
	if !sc.running.TryLock() {
		sc.pending.Store(true)
		return ErrBusy
	}
	go func() {
		defer sc.running.Unlock()
		ctx := sc.Context
		if ctx == nil {
			ctx = context.Background()
		}
		for {
			sc.pending.Store(false)
			sc.run(ctx, trigger)
			if !sc.pending.Load() || ctx.Err() != nil {
				return
			}
			trigger = tasks.TriggerSettings
		}
	}()
	return nil
}

// Exclusive runs work while no scan runs, such as renaming files; a scan asked for meanwhile
// runs after it.
func (sc *Scanner) Exclusive(work func()) {
	sc.running.Lock()
	work()
	sc.running.Unlock()
	if sc.pending.Swap(false) {
		sc.Start(tasks.TriggerSettings)
	}
}

// Run scans every library and waits; for tests and the command line.
func (sc *Scanner) Run(ctx context.Context, trigger string) error {
	sc.running.Lock()
	defer sc.running.Unlock()
	return sc.run(ctx, trigger)
}

// candidate is a file to read, or an entry of an archive.
type candidate struct {
	library store.Library
	path    string
	entry   string
	size    int64
	mtime   int64
	system  *systems.System
}

func (sc *Scanner) run(ctx context.Context, trigger string) error {
	id := sc.tasks.Start(tasks.KindScan, trigger)
	libraries, err := sc.store.Libraries()
	if err != nil {
		sc.tasks.Finish(id, &tasks.Note{Text: tasks.NoteScanFailed, Detail: err.Error()})
		return err
	}
	totalFiles, totalHashed := 0, 0
	for _, lib := range libraries {
		files, hashed, err := sc.scanLibrary(ctx, id, lib)
		totalFiles += files
		totalHashed += hashed
		if err != nil {
			sc.tasks.Warn(id, tasks.NoteNoLibrary, fmt.Sprintf("%s: %v", lib.Path, err))
		}
	}
	sc.tasks.Progress(id, -1, -1, "Matching files with the DATs")
	if err := sc.store.Rematch(ctx); err != nil {
		sc.tasks.Finish(id, &tasks.Note{Text: tasks.NoteScanFailed, Detail: err.Error()})
		return err
	}
	if sc.OnChange != nil {
		sc.OnChange()
	}
	sc.tasks.SetResult(id, totalFiles, totalHashed)
	sc.tasks.Finish(id, nil)
	return nil
}

// scanLibrary returns how many game files the library has and how many were read now.
func (sc *Scanner) scanLibrary(ctx context.Context, taskID int64, lib store.Library) (int, int, error) {
	if info, err := os.Stat(lib.Path); err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("not a folder")
		}
		return 0, 0, err
	}
	known, err := sc.store.KnownFiles(ctx, lib.ID)
	if err != nil {
		return 0, 0, err
	}
	sc.tasks.Progress(taskID, -1, -1, "Looking for files in "+lib.Name)

	seen := map[store.Key]bool{}
	todo := []candidate{}
	discSheets := &sheets{}
	// the entries known of each archive: an archive that has not changed since is not opened
	// again, which matters on a slow volume
	knownEntries := map[string][]store.Key{}
	for k := range known {
		if k.Entry != "" {
			knownEntries[k.Path] = append(knownEntries[k.Path], k)
		}
	}
	archives := []archive{}
	found := 0
	reported := time.Now()
	walkErr := filepath.WalkDir(lib.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			sc.tasks.Warn(taskID, tasks.NoteUnreadable, err.Error())
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && path != lib.Path {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		mtime := info.ModTime().Unix()
		system, _ := systems.Detect(lib.Path, path)
		found++
		if time.Since(reported) > time.Second {
			reported = time.Now()
			sc.tasks.Progress(taskID, -1, -1, fmt.Sprintf("Looking for files in %s: %d found", lib.Name, found))
		}
		if strings.EqualFold(filepath.Ext(path), ".zip") || strings.EqualFold(filepath.Ext(path), ".chd") {
			// a zip or a CHD: its entries come from the archive itself, read below, several
			// at a time, as a network share or a Docker volume answers slowly
			if keys := knownEntries[path]; len(keys) > 0 {
				unchanged := true
				for _, k := range keys {
					if c := known[k]; !c.Ok || c.Mtime != mtime {
						unchanged = false
						break
					}
				}
				if unchanged {
					for _, k := range keys {
						seen[k] = true
					}
					return nil
				}
			}
			archives = append(archives, archive{path: path, mtime: mtime, system: system})
			return nil
		}
		if !systems.IsGameFile(path) && !(system != nil && strings.EqualFold(filepath.Ext(path), ".bin")) {
			return nil
		}
		if !discSheets.identifies(path, system) {
			// audio tracks, and tracks the DATs do not list
			return nil
		}
		key := store.Key{Path: path}
		seen[key] = true
		size := info.Size()
		if discimage.Expands(path) {
			// a GCZ or CSO is known by the size of the ISO it holds, which its header says
			if s, err := discimage.ImageSize(path); err == nil {
				size = s
			}
		}
		if c, ok := known[key]; ok && c.Ok && c.Size == size && c.Mtime == mtime {
			return nil
		}
		todo = append(todo, candidate{library: lib, path: path, size: info.Size(), mtime: mtime, system: system})
		return nil
	})
	if walkErr != nil {
		return 0, 0, walkErr
	}
	sc.listArchives(ctx, taskID, lib, archives, func(a archive, entries []zipEntry, err error) {
		if err != nil {
			sc.tasks.Warn(taskID, tasks.NoteUnreadable, fmt.Sprintf("%s: %v", a.path, err))
			return
		}
		key := store.Key{Path: a.path}
		for _, e := range entries {
			key.Entry = e.name
			seen[key] = true
			if c, ok := known[key]; ok && c.Ok && c.Size == int64(e.size) && c.Mtime == a.mtime {
				continue
			}
			todo = append(todo, candidate{library: lib, path: a.path, entry: e.name, size: int64(e.size), mtime: a.mtime, system: e.system})
		}
	})
	if ctx.Err() != nil {
		return len(seen), 0, ctx.Err()
	}

	gone := []store.Key{}
	for k := range known {
		if !seen[k] {
			gone = append(gone, k)
		}
	}
	if err := sc.store.RemoveFiles(ctx, lib.ID, gone); err != nil {
		return len(seen), 0, err
	}
	if err := sc.hashAll(ctx, taskID, lib, todo); err != nil {
		return len(seen), 0, err
	}
	return len(seen), len(todo), nil
}

var trackPattern = regexp.MustCompile(`(?i)\(track 0*(\d+)\)`)

// laterTrack reports whether a file is a track of a disc after the first, such as
// "Game (USA) (Track 2).bin".
func laterTrack(path string) bool {
	m := trackPattern.FindStringSubmatch(filepath.Base(path))
	return m != nil && m[1] != "1"
}

// chdEntries lists what identifies a CHD: the image of a DVD or UMD, the first track of a CD
// when it holds data, the data tracks of the high-density area of a GD-ROM.
func chdEntries(path string) ([]zipEntry, error) {
	c, err := chd.Open(path)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if c.DVD() {
		return []zipEntry{{name: chdImageEntry, size: uint64(c.Size())}}, nil
	}
	tracks, err := c.Tracks()
	if err != nil {
		return nil, err
	}
	out := []zipEntry{}
	for i, t := range tracks {
		if t.Audio() {
			continue
		}
		if t.GDROM && t.Number < 3 {
			continue
		}
		if !t.GDROM && i > 0 {
			break
		}
		out = append(out, zipEntry{name: trackEntry(t.Number), size: uint64(t.Size())})
	}
	return out, nil
}

// the entries of a CHD: its image, or its tracks
const chdImageEntry = chd.ImageEntry

var trackEntry = chd.TrackEntry

type zipEntry struct {
	name   string
	size   uint64
	system *systems.System
}

// zipEntries lists the game files in a zip: the system comes from the folder of the zip, or
// else from the extension of each entry.
func zipEntries(path string, folderSystem *systems.System) ([]zipEntry, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := []zipEntry{}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := filepath.Ext(f.Name)
		switch {
		case systems.IsGameFile(f.Name):
		case folderSystem != nil && strings.EqualFold(ext, ".bin"):
		default:
			// a readme or an image next to the game
			continue
		}
		system := folderSystem
		if s, ok := systems.ByExtension(ext); ok && (system == nil || !system.HasExtension(f.Name)) {
			system = s
		}
		out = append(out, zipEntry{name: f.Name, size: f.UncompressedSize64, system: system})
	}
	return out, nil
}

const batchSize = 200

// hashAll reads the files with a few workers and saves them in batches. The entries of an
// archive go to one worker, which opens the archive once for all of them.
func (sc *Scanner) hashAll(parent context.Context, taskID int64, lib store.Library, todo []candidate) error {
	if len(todo) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	groups := [][]candidate{}
	byArchive := map[string]int{}
	for _, c := range todo {
		if c.entry == "" {
			groups = append(groups, []candidate{c})
			continue
		}
		i, ok := byArchive[c.path]
		if !ok {
			i = len(groups)
			byArchive[c.path] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], c)
	}

	jobs := make(chan []candidate)
	results := make(chan store.File, sc.workers*2)
	var wg sync.WaitGroup
	for i := 0; i < sc.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for group := range jobs {
				for _, f := range sc.hashGroup(group) {
					select {
					case results <- f:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, g := range groups {
			select {
			case jobs <- g:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	// on an error the workers stop and the results left are dropped, so nothing waits forever
	drain := func() {
		cancel()
		for range results {
		}
	}

	var done atomic.Int64
	batch := []store.File{}
	lastProgress := time.Time{}
	for f := range results {
		batch = append(batch, f)
		n := int(done.Add(1))
		if f.Status == "error" {
			sc.tasks.Warn(taskID, tasks.NoteUnreadable, fmt.Sprintf("%s: %s", filepath.Base(f.Path), f.Error))
		}
		if len(batch) >= batchSize {
			if err := sc.store.SaveFiles(ctx, batch); err != nil {
				drain()
				return err
			}
			batch = batch[:0]
		}
		if time.Since(lastProgress) > 200*time.Millisecond || n == len(todo) {
			name := filepath.Base(f.Path)
			if f.Entry != "" {
				name = f.Entry
			}
			sc.tasks.Progress(taskID, n, len(todo), lib.Name+": "+name)
			lastProgress = time.Now()
		}
	}
	if err := parent.Err(); err != nil {
		return err
	}
	return sc.store.SaveFiles(ctx, batch)
}

// hashGroup hashes a plain file, or the entries of one archive opened once.
func (sc *Scanner) hashGroup(group []candidate) []store.File {
	if len(group) == 1 && group[0].entry == "" {
		return []store.File{sc.hashOne(group[0], nil)}
	}
	out := make([]store.File, 0, len(group))
	if strings.EqualFold(filepath.Ext(group[0].path), ".chd") {
		for _, c := range group {
			out = append(out, sc.hashCHD(c))
		}
		return out
	}
	z, err := zip.OpenReader(group[0].path)
	if err != nil {
		for _, c := range group {
			f := sc.newFile(c)
			f.Status, f.Error = "error", err.Error()
			out = append(out, f)
		}
		return out
	}
	defer z.Close()
	entries := map[string]*zip.File{}
	for _, e := range z.File {
		entries[e.Name] = e
	}
	for _, c := range group {
		out = append(out, sc.hashOne(c, entries[c.entry]))
	}
	return out
}

func (sc *Scanner) newFile(c candidate) store.File {
	name := c.path
	if c.entry != "" {
		name = c.entry
	}
	f := store.File{LibraryID: c.library.ID, Path: c.path, Entry: c.entry, Stem: store.Stem(name), Size: c.size, Mtime: c.mtime, Status: "unknown"}
	if c.system != nil {
		f.System = c.system.ID
	}
	return f
}

// hashOne hashes a file, or an entry of an archive already open.
func (sc *Scanner) hashOne(c candidate, entry *zip.File) store.File {
	f := sc.newFile(c)
	system := c.system
	if system == nil {
		// without a system the file is hashed as a plain cartridge dump, so it can still be
		// found in any DAT
		system = &systems.System{ID: "", Plugin: "cartridge"}
	}
	plugin, err := platform.For(system)
	if err != nil {
		f.Status, f.Error = "error", err.Error()
		return f
	}
	if c.entry == "" && discimage.IdentifiedByHeader(c.path) {
		// RVZ, WIA, WBFS: the store matches it by the code in its disc header
		h, err := discimage.ReadHeader(c.path)
		if err != nil {
			f.Status, f.Error = "error", "no GameCube or Wii disc header"
			return f
		}
		f.Ident, f.AltForm = h.Key(), "header"
		return f
	}
	if c.entry == "" && discimage.Expands(c.path) {
		// GCZ, CSO: hashed as the ISO they hold
		img, err := discimage.Open(c.path)
		if err != nil {
			f.Status, f.Error = "error", err.Error()
			return f
		}
		defer img.Close()
		if !system.Disc() {
			system = &systems.System{ID: "", Plugin: "disc"}
			if plugin, err = platform.For(system); err != nil {
				f.Status, f.Error = "error", err.Error()
				return f
			}
		}
		hashes, err := hashStream(plugin, system, img)
		if err != nil {
			f.Status, f.Error = "error", err.Error()
			return f
		}
		f.Size, f.CRC, f.MD5, f.SHA1 = hashes[0].Size, hashes[0].CRC, hashes[0].MD5, hashes[0].SHA1
		return f
	}
	var r io.Reader
	if c.entry == "" {
		file, err := os.Open(c.path)
		if err != nil {
			f.Status, f.Error = "error", err.Error()
			return f
		}
		defer file.Close()
		r = file
	} else {
		if entry == nil {
			f.Status, f.Error = "error", c.entry+" is no longer in the archive"
			return f
		}
		rc, err := entry.Open()
		if err != nil {
			f.Status, f.Error = "error", err.Error()
			return f
		}
		defer rc.Close()
		r = rc
	}
	hashes, err := plugin.Hash(system, r, c.size)
	if err != nil {
		f.Status, f.Error = "error", err.Error()
		return f
	}
	f.Size, f.CRC, f.MD5, f.SHA1 = hashes[0].Size, hashes[0].CRC, hashes[0].MD5, hashes[0].SHA1
	if len(hashes) > 1 {
		f.AltForm, f.AltSize, f.AltCRC, f.AltSHA1 = hashes[1].Form, hashes[1].Size, hashes[1].CRC, hashes[1].SHA1
	}
	return f
}

// hashCHD hashes the data track of a CHD as the BIN file it was made from.
func (sc *Scanner) hashCHD(c candidate) store.File {
	f := sc.newFile(c)
	// compared with the DAT by the name of the image, not of its track
	f.Stem = store.Stem(filepath.Base(c.path))
	fail := func(err error) store.File {
		f.Status, f.Error = "error", err.Error()
		return f
	}
	img, err := chd.Open(c.path)
	if err != nil {
		return fail(err)
	}
	defer img.Close()
	var source interface {
		WriteTo(io.Writer) (int64, error)
		Size() int64
	}
	if c.entry == chdImageEntry && img.DVD() {
		source = img
	} else {
		tracks, err := img.Tracks()
		if err != nil {
			return fail(err)
		}
		for _, t := range tracks {
			if trackEntry(t.Number) == c.entry {
				source = t
			}
		}
	}
	if source == nil {
		return fail(errors.New(c.entry + " is no longer in the image"))
	}
	system := c.system
	if system == nil || !system.Disc() {
		system = &systems.System{ID: "", Plugin: "disc"}
	}
	plugin, err := platform.For(system)
	if err != nil {
		return fail(err)
	}
	hashes, err := hashStream(plugin, system, source)
	if err != nil {
		return fail(err)
	}
	f.Size, f.CRC, f.MD5, f.SHA1 = hashes[0].Size, hashes[0].CRC, hashes[0].MD5, hashes[0].SHA1
	return f
}

// hashStream hashes what a reader of an image writes, as it writes it.
func hashStream(plugin platform.Plugin, system *systems.System, source interface {
	WriteTo(io.Writer) (int64, error)
	Size() int64
}) ([]platform.Hashes, error) {
	pr, pw := io.Pipe()
	go func() {
		_, err := source.WriteTo(pw)
		pw.CloseWithError(err)
	}()
	hashes, err := plugin.Hash(system, pr, source.Size())
	pr.CloseWithError(io.ErrClosedPipe)
	return hashes, err
}

// archive is a zip or a CHD found by a scan, whose entries are listed after the walk.
type archive struct {
	path   string
	mtime  int64
	system *systems.System
}

// listArchives lists the entries of the archives with several readers at once, and hands them
// over one at a time, in order.
func (sc *Scanner) listArchives(ctx context.Context, taskID int64, lib store.Library, archives []archive, use func(archive, []zipEntry, error)) {
	type result struct {
		entries []zipEntry
		err     error
		done    chan struct{}
	}
	results := make([]result, len(archives))
	for i := range results {
		results[i].done = make(chan struct{})
	}
	next := make(chan int)
	go func() {
		defer close(next)
		for i := range archives {
			select {
			case next <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	for range max(8, sc.workers*2) {
		go func() {
			for i := range next {
				a := archives[i]
				if strings.EqualFold(filepath.Ext(a.path), ".chd") {
					results[i].entries, results[i].err = chdEntries(a.path)
					// a disc image: its data track is read like an entry of an archive
					for k := range results[i].entries {
						results[i].entries[k].system = a.system
					}
				} else {
					results[i].entries, results[i].err = zipEntries(a.path, a.system)
				}
				close(results[i].done)
			}
		}()
	}
	reported := time.Now()
	for i := range archives {
		select {
		case <-results[i].done:
		case <-ctx.Done():
			return
		}
		use(archives[i], results[i].entries, results[i].err)
		if time.Since(reported) > time.Second {
			reported = time.Now()
			sc.tasks.Progress(taskID, i+1, len(archives), "Reading the archives in "+lib.Name)
		}
	}
}
