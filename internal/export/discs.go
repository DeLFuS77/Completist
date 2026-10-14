package export

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DeLFuS77/completist/internal/systems"
)

var trackSuffix = regexp.MustCompile(`(?i) \(track 0*\d+\)$`)

// gameFile is the file a player opens for a disc: the CUE sheet next to the first track when
// there is one, the CHD, or else the track itself.
func gameFile(system, path string) string {
	sys, ok := systems.ByID(system)
	if !ok || !sys.Disc() || !strings.EqualFold(filepath.Ext(path), ".bin") {
		return path
	}
	dir := filepath.Dir(path)
	// a GD-ROM dumped as a GDI file opens by it
	if gdi, _ := filepath.Glob(filepath.Join(globEscape(dir), "*.gdi")); len(gdi) == 1 {
		return gdi[0]
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	cue := filepath.Join(dir, trackSuffix.ReplaceAllString(base, "")+".cue")
	if _, err := os.Stat(cue); err == nil {
		return cue
	}
	// a single CUE sheet in the folder is the disc's
	matches, _ := filepath.Glob(filepath.Join(globEscape(dir), "*.cue"))
	if len(matches) == 1 {
		return matches[0]
	}
	return path
}

func globEscape(s string) string {
	r := strings.NewReplacer(`[`, `\[`, `]`, `\]`, `*`, `\*`, `?`, `\?`)
	return r.Replace(s)
}
