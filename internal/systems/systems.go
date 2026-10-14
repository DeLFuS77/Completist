// Package systems lists the game systems Completist knows: their names, the DAT files that
// describe them, the folder names and file extensions that point to them, and the plugin
// that reads their files.
package systems

import (
	"path/filepath"
	"sort"
	"strings"
)

// Header describes a copier or emulator header that some dumps carry in front of the data.
// DATs may describe the data with or without it, so both are hashed.
type Header struct {
	// bytes skipped when the header is present
	Size int
	// the header is present when the file size modulo Modulo equals Size (0: always check Magic)
	Modulo int
	// the header starts with these bytes, when set
	Magic []byte
}

// System is one game system.
type System struct {
	// short, stable id used in URLs, settings and the database
	ID string `json:"id"`
	// full name, as the DATs name it
	Name string `json:"name"`
	// short name for the interface
	Short string `json:"short"`
	// maker, for grouping in the interface
	Maker string `json:"maker"`
	// year the system was released, for sorting
	Year int `json:"year"`
	// plugin that reads its files
	Plugin string `json:"plugin"`
	// file name of its DAT in the libretro-database repository, without .dat; also the folder
	// of its covers in libretro-thumbnails
	LibretroDat string `json:"libretro"`
	// folder of its DAT in metadat: no-intro (the default) or redump
	DatFolder string `json:"-"`
	// extensions of its files, lower case, with the dot
	Extensions []string `json:"extensions"`
	// folder names that hold its files, lower case
	Folders []string `json:"-"`
	// optional header in front of the data
	Header *Header `json:"-"`
	// the data may be stored in another byte order (Nintendo 64)
	N64ByteOrder bool `json:"-"`
}

// the files of disc images: tracks as BIN files, or a whole disc in a CHD
var discExtensions = []string{".bin", ".img", ".chd"}

var nesHeader = &Header{Size: 16, Magic: []byte("NES\x1a")}
var fdsHeader = &Header{Size: 16, Magic: []byte("FDS\x1a")}
var snesHeader = &Header{Size: 512, Modulo: 1024}
var lynxHeader = &Header{Size: 64, Magic: []byte("LYNX")}
var a7800Header = &Header{Size: 128, Magic: []byte("\x01ATARI7800")}
var pceHeader = &Header{Size: 512, Modulo: 8192}

// All is every known system, sorted by maker and year.
var All = []System{
	{ID: "nes", Name: "Nintendo - Nintendo Entertainment System", Short: "NES", Maker: "Nintendo", Year: 1983, Plugin: "cartridge",
		LibretroDat: "Nintendo - Nintendo Entertainment System", Extensions: []string{".nes", ".unh", ".unf", ".unif"},
		Folders: []string{"nes", "famicom", "fc", "nintendo entertainment system"}, Header: nesHeader},
	{ID: "fds", Name: "Nintendo - Family Computer Disk System", Short: "Famicom Disk System", Maker: "Nintendo", Year: 1986, Plugin: "cartridge",
		LibretroDat: "Nintendo - Family Computer Disk System", Extensions: []string{".fds"},
		Folders: []string{"fds", "famicom disk system"}, Header: fdsHeader},
	{ID: "snes", Name: "Nintendo - Super Nintendo Entertainment System", Short: "Super Nintendo", Maker: "Nintendo", Year: 1990, Plugin: "cartridge",
		LibretroDat: "Nintendo - Super Nintendo Entertainment System", Extensions: []string{".sfc", ".smc", ".swc", ".fig"},
		Folders: []string{"snes", "sfc", "super nintendo", "super famicom", "super nintendo entertainment system"}, Header: snesHeader},
	{ID: "n64", Name: "Nintendo - Nintendo 64", Short: "Nintendo 64", Maker: "Nintendo", Year: 1996, Plugin: "cartridge",
		LibretroDat: "Nintendo - Nintendo 64", Extensions: []string{".z64", ".n64", ".v64"},
		Folders: []string{"n64", "nintendo 64"}, N64ByteOrder: true},
	{ID: "virtualboy", Name: "Nintendo - Virtual Boy", Short: "Virtual Boy", Maker: "Nintendo", Year: 1995, Plugin: "cartridge",
		LibretroDat: "Nintendo - Virtual Boy", Extensions: []string{".vb", ".vboy"},
		Folders: []string{"virtualboy", "virtual boy", "vb"}},
	{ID: "gb", Name: "Nintendo - Game Boy", Short: "Game Boy", Maker: "Nintendo", Year: 1989, Plugin: "cartridge",
		LibretroDat: "Nintendo - Game Boy", Extensions: []string{".gb"},
		Folders: []string{"gb", "game boy", "gameboy"}},
	{ID: "gbc", Name: "Nintendo - Game Boy Color", Short: "Game Boy Color", Maker: "Nintendo", Year: 1998, Plugin: "cartridge",
		LibretroDat: "Nintendo - Game Boy Color", Extensions: []string{".gbc"},
		Folders: []string{"gbc", "game boy color", "gameboy color"}},
	{ID: "gba", Name: "Nintendo - Game Boy Advance", Short: "Game Boy Advance", Maker: "Nintendo", Year: 2001, Plugin: "cartridge",
		LibretroDat: "Nintendo - Game Boy Advance", Extensions: []string{".gba"},
		Folders: []string{"gba", "game boy advance", "gameboy advance"}},
	{ID: "nds", Name: "Nintendo - Nintendo DS", Short: "Nintendo DS", Maker: "Nintendo", Year: 2004, Plugin: "cartridge",
		LibretroDat: "Nintendo - Nintendo DS", Extensions: []string{".nds"},
		Folders: []string{"nds", "nintendo ds", "ds"}},
	{ID: "pokemini", Name: "Nintendo - Pokemon Mini", Short: "Pokémon Mini", Maker: "Nintendo", Year: 2001, Plugin: "cartridge",
		LibretroDat: "Nintendo - Pokemon Mini", Extensions: []string{".min"},
		Folders: []string{"pokemini", "pokemon mini"}},
	{ID: "sg1000", Name: "Sega - SG-1000", Short: "SG-1000", Maker: "Sega", Year: 1983, Plugin: "cartridge",
		LibretroDat: "Sega - SG-1000", Extensions: []string{".sg"},
		Folders: []string{"sg1000", "sg-1000"}},
	{ID: "mastersystem", Name: "Sega - Master System - Mark III", Short: "Master System", Maker: "Sega", Year: 1985, Plugin: "cartridge",
		LibretroDat: "Sega - Master System - Mark III", Extensions: []string{".sms"},
		Folders: []string{"mastersystem", "master system", "sms"}},
	{ID: "megadrive", Name: "Sega - Mega Drive - Genesis", Short: "Mega Drive", Maker: "Sega", Year: 1988, Plugin: "cartridge",
		LibretroDat: "Sega - Mega Drive - Genesis", Extensions: []string{".md", ".gen", ".smd"},
		Folders: []string{"megadrive", "mega drive", "genesis", "md"}},
	{ID: "segacd", Name: "Sega - Mega-CD - Sega CD", Short: "Mega CD", Maker: "Sega", Year: 1991, Plugin: "disc",
		LibretroDat: "Sega - Mega-CD - Sega CD", DatFolder: "redump", Extensions: discExtensions,
		Folders: []string{"segacd", "megacd", "mega cd", "mega-cd", "sega cd", "sega-cd"}},
	{ID: "saturn", Name: "Sega - Saturn", Short: "Saturn", Maker: "Sega", Year: 1994, Plugin: "disc",
		LibretroDat: "Sega - Saturn", DatFolder: "redump", Extensions: discExtensions,
		Folders: []string{"saturn", "sega saturn"}},
	{ID: "dreamcast", Name: "Sega - Dreamcast", Short: "Dreamcast", Maker: "Sega", Year: 1998, Plugin: "disc",
		LibretroDat: "Sega - Dreamcast", DatFolder: "redump", Extensions: []string{".bin", ".img", ".chd", ".raw"},
		Folders: []string{"dreamcast", "dc", "sega dreamcast"}},
	{ID: "gamecube", Name: "Nintendo - GameCube", Short: "GameCube", Maker: "Nintendo", Year: 2001, Plugin: "disc",
		LibretroDat: "Nintendo - GameCube", DatFolder: "redump", Extensions: []string{".iso", ".gcm", ".gcz", ".rvz", ".wia", ".ciso"},
		Folders: []string{"gamecube", "gc", "ngc", "nintendo gamecube"}},
	{ID: "wii", Name: "Nintendo - Wii", Short: "Wii", Maker: "Nintendo", Year: 2006, Plugin: "disc",
		LibretroDat: "Nintendo - Wii", DatFolder: "redump", Extensions: []string{".iso", ".gcz", ".rvz", ".wia", ".wbfs"},
		Folders: []string{"wii", "nintendo wii"}},
	{ID: "ps2", Name: "Sony - PlayStation 2", Short: "PlayStation 2", Maker: "Sony", Year: 2000, Plugin: "disc",
		LibretroDat: "Sony - PlayStation 2", DatFolder: "redump", Extensions: []string{".iso", ".bin", ".img", ".chd"},
		Folders: []string{"ps2", "playstation 2", "playstation2", "sony playstation 2"}},
	{ID: "psp", Name: "Sony - PlayStation Portable", Short: "PSP", Maker: "Sony", Year: 2004, Plugin: "disc",
		LibretroDat: "Sony - PlayStation Portable", DatFolder: "redump", Extensions: []string{".iso", ".cso", ".chd"},
		Folders: []string{"psp", "playstation portable", "sony playstation portable"}},
	{ID: "psx", Name: "Sony - PlayStation", Short: "PlayStation", Maker: "Sony", Year: 1994, Plugin: "disc",
		LibretroDat: "Sony - PlayStation", DatFolder: "redump", Extensions: discExtensions,
		Folders: []string{"psx", "ps1", "psone", "playstation", "sony playstation", "playstation 1"}},
	{ID: "sega32x", Name: "Sega - 32X", Short: "32X", Maker: "Sega", Year: 1994, Plugin: "cartridge",
		LibretroDat: "Sega - 32X", Extensions: []string{".32x"},
		Folders: []string{"sega32x", "32x"}},
	{ID: "gamegear", Name: "Sega - Game Gear", Short: "Game Gear", Maker: "Sega", Year: 1990, Plugin: "cartridge",
		LibretroDat: "Sega - Game Gear", Extensions: []string{".gg"},
		Folders: []string{"gamegear", "game gear", "gg"}},
	{ID: "pcengine", Name: "NEC - PC Engine - TurboGrafx 16", Short: "PC Engine", Maker: "NEC", Year: 1987, Plugin: "cartridge",
		LibretroDat: "NEC - PC Engine - TurboGrafx 16", Extensions: []string{".pce"},
		Folders: []string{"pcengine", "pc engine", "tg16", "turbografx 16", "turbografx-16"}, Header: pceHeader},
	{ID: "atari2600", Name: "Atari - 2600", Short: "Atari 2600", Maker: "Atari", Year: 1977, Plugin: "cartridge",
		LibretroDat: "Atari - 2600", Extensions: []string{".a26"},
		Folders: []string{"atari2600", "atari 2600", "2600"}},
	{ID: "atari7800", Name: "Atari - 7800", Short: "Atari 7800", Maker: "Atari", Year: 1986, Plugin: "cartridge",
		LibretroDat: "Atari - 7800", Extensions: []string{".a78"},
		Folders: []string{"atari7800", "atari 7800", "7800"}, Header: a7800Header},
	{ID: "lynx", Name: "Atari - Lynx", Short: "Lynx", Maker: "Atari", Year: 1989, Plugin: "cartridge",
		LibretroDat: "Atari - Lynx", Extensions: []string{".lnx", ".lyx"},
		Folders: []string{"lynx", "atari lynx"}, Header: lynxHeader},
	{ID: "ngp", Name: "SNK - Neo Geo Pocket", Short: "Neo Geo Pocket", Maker: "SNK", Year: 1998, Plugin: "cartridge",
		LibretroDat: "SNK - Neo Geo Pocket", Extensions: []string{".ngp"},
		Folders: []string{"ngp", "neo geo pocket"}},
	{ID: "ngpc", Name: "SNK - Neo Geo Pocket Color", Short: "Neo Geo Pocket Color", Maker: "SNK", Year: 1999, Plugin: "cartridge",
		LibretroDat: "SNK - Neo Geo Pocket Color", Extensions: []string{".ngc"},
		Folders: []string{"ngpc", "neo geo pocket color"}},
	{ID: "wonderswan", Name: "Bandai - WonderSwan", Short: "WonderSwan", Maker: "Bandai", Year: 1999, Plugin: "cartridge",
		LibretroDat: "Bandai - WonderSwan", Extensions: []string{".ws"},
		Folders: []string{"wonderswan", "ws"}},
	{ID: "wonderswancolor", Name: "Bandai - WonderSwan Color", Short: "WonderSwan Color", Maker: "Bandai", Year: 2000, Plugin: "cartridge",
		LibretroDat: "Bandai - WonderSwan Color", Extensions: []string{".wsc"},
		Folders: []string{"wonderswancolor", "wonderswan color", "wsc"}},
	{ID: "colecovision", Name: "Coleco - ColecoVision", Short: "ColecoVision", Maker: "Coleco", Year: 1982, Plugin: "cartridge",
		LibretroDat: "Coleco - ColecoVision", Extensions: []string{".col"},
		Folders: []string{"colecovision", "coleco"}},
	{ID: "intellivision", Name: "Mattel - Intellivision", Short: "Intellivision", Maker: "Mattel", Year: 1979, Plugin: "cartridge",
		LibretroDat: "Mattel - Intellivision", Extensions: []string{".int"},
		Folders: []string{"intellivision"}},
	{ID: "vectrex", Name: "GCE - Vectrex", Short: "Vectrex", Maker: "GCE", Year: 1982, Plugin: "cartridge",
		LibretroDat: "GCE - Vectrex", Extensions: []string{".vec"},
		Folders: []string{"vectrex"}},
}

var byID = map[string]*System{}
var byFolder = map[string]*System{}
var byExtension = map[string]*System{}
var byName = map[string]*System{}

func init() {
	sort.SliceStable(All, func(i, j int) bool {
		if All[i].Maker != All[j].Maker {
			return makerOrder(All[i].Maker) < makerOrder(All[j].Maker)
		}
		return All[i].Year < All[j].Year
	})
	shared := map[string]int{}
	for i := range All {
		for _, ext := range All[i].Extensions {
			shared[ext]++
		}
	}
	for i := range All {
		s := &All[i]
		byID[s.ID] = s
		byName[strings.ToLower(s.Name)] = s
		byFolder[s.ID] = s
		for _, f := range s.Folders {
			byFolder[f] = s
		}
		for _, ext := range s.Extensions {
			// an extension used by several systems does not tell which one it is
			if shared[ext] == 1 {
				byExtension[ext] = s
			}
		}
	}
}

func makerOrder(maker string) int {
	for i, m := range []string{"Nintendo", "Sega", "Sony", "NEC", "SNK", "Atari", "Bandai"} {
		if m == maker {
			return i
		}
	}
	return 100
}

// ByID returns the system with this id.
func ByID(id string) (*System, bool) {
	s, ok := byID[id]
	return s, ok
}

// ByDatName returns the system a DAT describes, from the name in its header.
func ByDatName(name string) (*System, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if s, ok := byName[name]; ok {
		return s, true
	}
	// No-Intro adds suffixes like " (Headered)" or " (Parent-Clone)"
	if i := strings.Index(name, " ("); i > 0 {
		if s, ok := byName[name[:i]]; ok {
			return s, true
		}
	}
	return nil, false
}

// ByFolder returns the system a folder name points to, such as "snes" or "Mega Drive".
func ByFolder(name string) (*System, bool) {
	s, ok := byFolder[strings.ToLower(strings.TrimSpace(name))]
	return s, ok
}

// ByExtension returns the only system that uses this extension.
func ByExtension(ext string) (*System, bool) {
	s, ok := byExtension[strings.ToLower(ext)]
	return s, ok
}

// Detect finds the system of a file in a library: from the folders between the library root and
// the file (the nearest wins), then from its extension.
func Detect(root string, path string) (*System, bool) {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err == nil && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := len(parts) - 1; i >= 0; i-- {
			if s, ok := ByFolder(parts[i]); ok {
				return s, true
			}
		}
	}
	return ByExtension(filepath.Ext(path))
}

// HasExtension reports whether a file name has one of the extensions of the system.
func (s *System) HasExtension(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range s.Extensions {
		if e == ext {
			return true
		}
	}
	return false
}

// Disc reports whether the system uses disc images.
func (s *System) Disc() bool {
	return s.Plugin == "disc"
}

// DatPath is the path of its DAT in the libretro-database metadat folder.
func (s *System) DatPath() string {
	folder := s.DatFolder
	if folder == "" {
		folder = "no-intro"
	}
	return folder + "/" + s.LibretroDat + ".dat"
}

// IsGameFile reports whether a file name has the extension of any known system.
func IsGameFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for i := range All {
		for _, e := range All[i].Extensions {
			if e == ext {
				return true
			}
		}
	}
	return false
}
