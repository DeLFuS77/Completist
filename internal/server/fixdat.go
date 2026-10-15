package server

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DeLFuS77/completist/internal/collection"
	"github.com/DeLFuS77/completist/internal/dat"
	"github.com/DeLFuS77/completist/internal/systems"
)

type fixdatFile struct {
	XMLName xml.Name     `xml:"datafile"`
	Header  fixdatHeader `xml:"header"`
	Games   []fixdatGame `xml:"game"`
}

type fixdatHeader struct {
	Name        string `xml:"name"`
	Description string `xml:"description"`
	Version     string `xml:"version"`
	Author      string `xml:"author"`
	Comment     string `xml:"comment"`
}

type fixdatGame struct {
	Name        string      `xml:"name,attr"`
	Description string      `xml:"description"`
	Roms        []fixdatRom `xml:"rom"`
}

type fixdatRom struct {
	Name string `xml:"name,attr"`
	Size int64  `xml:"size,attr"`
	CRC  string `xml:"crc,attr,omitempty"`
	MD5  string `xml:"md5,attr,omitempty"`
	SHA1 string `xml:"sha1,attr,omitempty"`
}

// fixdat answers a DAT of the games missing in a system, counted the way it is counted, for
// other tools to look for them.
func (s *Server) fixdat(w http.ResponseWriter, r *http.Request) {
	sys, ok := systems.ByID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown system")
		return
	}
	prefs := collection.LoadPreferences(s.Store)
	mode := prefs.ModeOf(sys.ID)
	entries, err := collection.Entries(r.Context(), s.Store, sys.ID, mode, prefs.Regions)
	if err != nil {
		s.fail(w, err)
		return
	}
	roms, err := s.Store.DatRoms(r.Context(), sys.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	datName := sys.Name
	if dats, err := s.Store.Dats(); err == nil {
		for _, d := range dats {
			if d.System == sys.ID && d.Name != "" {
				datName = d.Name
			}
		}
	}
	now := time.Now()
	out := fixdatFile{Header: fixdatHeader{
		Name:        "Fix_" + datName,
		Description: datName + " (missing games)",
		Version:     now.Format("2006-01-02"),
		Author:      "Completist",
		Comment:     "Counted as " + mode,
	}}
	for _, e := range entries {
		if e.Owned {
			continue
		}
		// the version shown for the game, with all its discs
		release := dat.Release(e.Name)
		for _, name := range e.Names {
			if dat.Release(name) != release {
				continue
			}
			g := fixdatGame{Name: name, Description: name}
			for _, rom := range roms[name] {
				g.Roms = append(g.Roms, fixdatRom{Name: rom.Name, Size: rom.Size, CRC: strings.ToLower(rom.CRC), MD5: strings.ToLower(rom.MD5), SHA1: strings.ToLower(rom.SHA1)})
			}
			out.Games = append(out.Games, g)
		}
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeFileName("Fix_"+datName+" ("+now.Format("20060102")+").dat")))
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(xml.Header))
	w.Write([]byte(`<!DOCTYPE datafile PUBLIC "-//Logiqx//DTD ROM Management Datafile//EN" "http://www.logiqx.com/Dats/datafile.dtd">` + "\n"))
	enc := xml.NewEncoder(w)
	enc.Indent("", "\t")
	enc.Encode(out)
}

// safeFileName keeps a name a header can carry.
func safeFileName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || strings.ContainsRune(`"\/:*?<>|`, r) {
			return '_'
		}
		return r
	}, name)
}
