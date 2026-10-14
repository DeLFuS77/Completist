package scanner

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/DeLFuS77/completist/internal/systems"
)

// A disc dumped as tracks has a sheet that lists them: a CUE sheet, or a GDI file for the
// Dreamcast. It tells which tracks are audio, and which one identifies the disc.

type sheetTrack struct {
	number int
	audio  bool
	// from a GDI file: a GD-ROM, identified by its tracks in the high-density area
	gdrom bool
}

type sheets struct {
	mutex sync.Mutex
	dirs  map[string]map[string]sheetTrack
}

// lookup returns what the sheets in the folder of a file say about it.
func (s *sheets) lookup(path string) (sheetTrack, bool) {
	dir := filepath.Dir(path)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.dirs == nil {
		s.dirs = map[string]map[string]sheetTrack{}
	}
	tracks, ok := s.dirs[dir]
	if !ok {
		tracks = readSheets(dir)
		s.dirs[dir] = tracks
	}
	t, ok := tracks[strings.ToLower(filepath.Base(path))]
	return t, ok
}

func readSheets(dir string) map[string]sheetTrack {
	out := map[string]sheetTrack{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		switch {
		case strings.HasSuffix(name, ".cue"):
			readCue(filepath.Join(dir, e.Name()), out)
		case strings.HasSuffix(name, ".gdi"):
			readGDI(filepath.Join(dir, e.Name()), out)
		}
	}
	return out
}

func readCue(path string, out map[string]sheetTrack) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	file := ""
	sc := bufio.NewScanner(f)
	for lines := 0; sc.Scan() && lines < 10000; lines++ {
		line := strings.TrimSpace(sc.Text())
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "FILE "):
			rest := strings.TrimSpace(line[5:])
			if strings.HasPrefix(rest, `"`) {
				if end := strings.Index(rest[1:], `"`); end >= 0 {
					file = rest[1 : 1+end]
				}
			} else if fields := strings.Fields(rest); len(fields) > 0 {
				file = fields[0]
			}
		case strings.HasPrefix(upper, "TRACK ") && file != "":
			fields := strings.Fields(upper)
			if len(fields) < 3 {
				continue
			}
			n, _ := strconv.Atoi(fields[1])
			key := strings.ToLower(filepath.Base(file))
			// the first track of a file is the one its data starts with
			if _, seen := out[key]; !seen {
				out[key] = sheetTrack{number: n, audio: fields[2] == "AUDIO"}
			}
		}
	}
}

func readGDI(path string, out map[string]sheetTrack) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for lines := 0; sc.Scan() && lines < 1000; lines++ {
		line := strings.TrimSpace(sc.Text())
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		n, err1 := strconv.Atoi(fields[0])
		kind, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil {
			continue
		}
		// the file name may be quoted and hold spaces: what is between the size and the offset
		file := strings.Join(fields[4:len(fields)-1], " ")
		file = strings.Trim(file, `"`)
		out[strings.ToLower(filepath.Base(file))] = sheetTrack{number: n, audio: kind == 0, gdrom: true}
	}
}

// identifies reports whether a track file identifies its disc and is worth reading: the DATs
// list the first data track of a CD, and the data tracks of the high-density area of a GD-ROM.
func (s *sheets) identifies(path string, system *systems.System) bool {
	dreamcast := system != nil && system.ID == "dreamcast"
	if t, ok := s.lookup(path); ok {
		if t.audio {
			return false
		}
		if (t.gdrom || dreamcast) && t.number < 3 {
			return false
		}
		return true
	}
	if dreamcast {
		return true
	}
	return !laterTrack(path)
}
