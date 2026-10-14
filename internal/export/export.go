// Package export writes the collection for other programs: RetroArch playlists and ES-DE
// gamelists, packed in a zip file.
package export

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"

	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/systems"
)

// ESDE are the folder names ES-DE gives the systems.
var ESDE = map[string]string{
	"nes": "nes", "fds": "fds", "snes": "snes", "n64": "n64", "virtualboy": "virtualboy",
	"gb": "gb", "gbc": "gbc", "gba": "gba", "nds": "nds", "pokemini": "pokemini",
	"sg1000": "sg-1000", "mastersystem": "mastersystem", "megadrive": "megadrive", "sega32x": "sega32x",
	"gamegear": "gamegear", "pcengine": "pcengine", "atari2600": "atari2600", "atari7800": "atari7800",
	"lynx": "atarilynx", "ngp": "ngp", "ngpc": "ngpc", "wonderswan": "wonderswan",
	"wonderswancolor": "wonderswancolor", "colecovision": "colecovision", "intellivision": "intellivision",
	"vectrex": "vectrex",
}

// Paths turns the paths Completist sees into the paths of the device that plays the games:
// From is replaced with To, and the separators follow To.
type Paths struct {
	From string
	To   string
}

func (p Paths) apply(path string) string {
	from := strings.TrimRight(p.From, `/\`)
	if from == "" || p.To == "" {
		return path
	}
	if path != from && !strings.HasPrefix(path, from+"/") && !strings.HasPrefix(path, from+`\`) {
		return path
	}
	to := strings.TrimRight(p.To, `/\`)
	rest := path[len(from):]
	// a Windows destination: C:\Games or a path with backslashes
	if strings.Contains(to, `\`) || (len(to) >= 2 && to[1] == ':') {
		rest = strings.ReplaceAll(rest, "/", `\`)
	} else {
		rest = strings.ReplaceAll(rest, `\`, "/")
	}
	return to + rest
}

type playlist struct {
	Version            string         `json:"version"`
	DefaultCorePath    string         `json:"default_core_path"`
	DefaultCoreName    string         `json:"default_core_name"`
	LabelDisplayMode   int            `json:"label_display_mode"`
	RightThumbnailMode int            `json:"right_thumbnail_mode"`
	LeftThumbnailMode  int            `json:"left_thumbnail_mode"`
	SortMode           int            `json:"sort_mode"`
	Items              []playlistItem `json:"items"`
}

type playlistItem struct {
	Path     string `json:"path"`
	Label    string `json:"label"`
	CorePath string `json:"core_path"`
	CoreName string `json:"core_name"`
	CRC32    string `json:"crc32"`
	DBName   string `json:"db_name"`
}

// RetroArch writes a playlist for each system: named like its database, so RetroArch finds the
// thumbnails, with the DAT names as labels.
func RetroArch(w io.Writer, files []store.Matched, paths Paths) error {
	z := zip.NewWriter(w)
	bySystem := group(files)
	for _, sys := range systems.All {
		list := bySystem[sys.ID]
		if len(list) == 0 {
			continue
		}
		name := sys.LibretroDat + ".lpl"
		pl := playlist{Version: "1.5", Items: []playlistItem{}}
		for _, f := range list {
			path := paths.apply(f.Path)
			if f.Entry != "" {
				path += "#" + f.Entry
			}
			crc := ""
			if f.CRC != "" {
				crc = strings.ToUpper(f.CRC) + "|crc"
			}
			pl.Items = append(pl.Items, playlistItem{Path: path, Label: f.Name, CorePath: "DETECT", CoreName: "DETECT", CRC32: crc, DBName: name})
		}
		out, err := z.Create("playlists/" + name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(pl); err != nil {
			return err
		}
	}
	return z.Close()
}

type gameList struct {
	XMLName xml.Name   `xml:"gameList"`
	Games   []gameItem `xml:"game"`
}

type gameItem struct {
	Path string `xml:"path"`
	Name string `xml:"name"`
}

// ESDEGamelists writes a gamelist.xml for each system, with paths relative to the folder of
// the system: the folder named after it that holds the file, or else the library.
func ESDEGamelists(w io.Writer, files []store.Matched) error {
	z := zip.NewWriter(w)
	bySystem := group(files)
	for _, sys := range systems.All {
		list := bySystem[sys.ID]
		folder := ESDE[sys.ID]
		if len(list) == 0 || folder == "" {
			continue
		}
		gl := gameList{}
		seen := map[string]bool{}
		for _, f := range list {
			rel := relative(f.Library, f.Path, sys.ID)
			// ES-DE opens files, not entries in an archive: one game per file
			if seen[rel] {
				continue
			}
			seen[rel] = true
			gl.Games = append(gl.Games, gameItem{Path: rel, Name: f.Title})
		}
		out, err := z.Create("gamelists/" + folder + "/gamelist.xml")
		if err != nil {
			return err
		}
		io.WriteString(out, xml.Header)
		enc := xml.NewEncoder(out)
		enc.Indent("", "\t")
		if err := enc.Encode(gl); err != nil {
			return err
		}
		io.WriteString(out, "\n")
	}
	return z.Close()
}

// relative is the path of a file from the folder of its system.
func relative(library, path, system string) string {
	root := library
	for dir := filepath.Dir(path); len(dir) > len(library) && strings.HasPrefix(dir, library); dir = filepath.Dir(dir) {
		if s, ok := systems.ByFolder(filepath.Base(dir)); ok && s.ID == system {
			root = dir
			break
		}
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	return "./" + filepath.ToSlash(rel)
}

func group(files []store.Matched) map[string][]store.Matched {
	out := map[string][]store.Matched{}
	for _, f := range files {
		out[f.System] = append(out[f.System], f)
	}
	return out
}
