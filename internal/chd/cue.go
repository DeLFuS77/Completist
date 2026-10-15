package chd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseCue reads a CUE sheet into the tracks to write: where each one starts in its BIN file,
// how many frames it has and how many of them are its pregap.
func ParseCue(path string) ([]TrackSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	type index struct {
		track  TrackSource
		index0 int // frame of INDEX 00 in the file, -1 without one
		index1 int
	}
	var list []*index
	file := ""
	dir := filepath.Dir(path)
	sc := bufio.NewScanner(f)
	for lines := 0; sc.Scan(); lines++ {
		if lines > 10000 {
			return nil, errors.New("CUE sheet too long")
		}
		line := strings.TrimSpace(sc.Text())
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "FILE":
			name := cueFile(line)
			if name == "" {
				return nil, errors.New("CUE sheet: a FILE without a name")
			}
			if !strings.EqualFold(fields[len(fields)-1], "BINARY") {
				return nil, fmt.Errorf("CUE sheet: %s files are not supported", fields[len(fields)-1])
			}
			file = filepath.Join(dir, filepath.Base(name))
		case "TRACK":
			if len(fields) < 3 || file == "" {
				return nil, errors.New("CUE sheet: a TRACK without its FILE")
			}
			n, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, errors.New("CUE sheet: a TRACK without a number")
			}
			kind, ok := map[string]string{"AUDIO": "AUDIO", "MODE1/2352": "MODE1_RAW", "MODE2/2352": "MODE2_RAW",
				"MODE1/2048": "MODE1", "MODE2/2336": "MODE2"}[strings.ToUpper(fields[2])]
			if !ok {
				return nil, fmt.Errorf("CUE sheet: tracks of type %s are not supported", fields[2])
			}
			list = append(list, &index{track: TrackSource{Number: n, Type: kind, File: file}, index0: -1, index1: -1})
		case "INDEX":
			if len(list) == 0 || len(fields) < 3 {
				return nil, errors.New("CUE sheet: an INDEX without its TRACK")
			}
			n, _ := strconv.Atoi(fields[1])
			frame, err := msf(fields[2])
			if err != nil {
				return nil, err
			}
			switch n {
			case 0:
				list[len(list)-1].index0 = frame
			case 1:
				list[len(list)-1].index1 = frame
			}
		case "PREGAP", "POSTGAP":
			return nil, fmt.Errorf("CUE sheet: %s is not supported", fields[0])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("CUE sheet without tracks")
	}
	out := make([]TrackSource, len(list))
	for i, e := range list {
		if e.index1 < 0 {
			return nil, fmt.Errorf("CUE sheet: track %d has no INDEX 01", e.track.Number)
		}
		start := e.index1
		if e.index0 >= 0 {
			start = e.index0
		}
		t := e.track
		size := t.dataSize()
		t.Offset = int64(start) * int64(size)
		t.Pregap = e.index1 - start
		// up to the next track in the same file, or to its end
		end := -1
		if i+1 < len(list) && list[i+1].track.File == t.File {
			next := list[i+1]
			end = next.index1
			if next.index0 >= 0 {
				end = next.index0
			}
		}
		if end < 0 {
			info, err := os.Stat(t.File)
			if err != nil {
				return nil, err
			}
			if (info.Size()-t.Offset)%int64(size) != 0 {
				return nil, fmt.Errorf("%s: its size is not a number of sectors", filepath.Base(t.File))
			}
			end = start + int((info.Size()-t.Offset)/int64(size))
		}
		t.Frames = end - start
		if t.Frames <= 0 || t.Pregap < 0 {
			return nil, fmt.Errorf("CUE sheet: track %d is empty", t.Number)
		}
		out[i] = t
	}
	return out, nil
}

func cueFile(line string) string {
	rest := strings.TrimSpace(line[4:])
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

// msf reads mm:ss:ff as a frame number.
func msf(s string) (int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("CUE sheet: bad time %q", s)
	}
	m, err1 := strconv.Atoi(parts[0])
	sec, err2 := strconv.Atoi(parts[1])
	fr, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || sec >= 60 || fr >= 75 {
		return 0, fmt.Errorf("CUE sheet: bad time %q", s)
	}
	return (m*60+sec)*75 + fr, nil
}
