package dat

import (
	"strconv"
	"strings"
)

// Flags mark what kind of release a DAT entry is, read from the tags in its name, such as
// "(Beta)" or "(Unl)".
type Flags uint32

const (
	FlagBios Flags = 1 << iota
	FlagDemo
	FlagBeta
	FlagProto
	FlagSample
	FlagUnlicensed
	FlagPirate
	FlagHack
	FlagAftermarket
	FlagProgram
	FlagAlt
	FlagBadDump
)

// Unofficial are the flags of entries that are not official retail releases.
const Unofficial = FlagBios | FlagDemo | FlagBeta | FlagProto | FlagSample | FlagUnlicensed | FlagPirate | FlagHack | FlagAftermarket | FlagProgram | FlagBadDump

var flagNames = []struct {
	flag Flags
	name string
}{
	{FlagBios, "bios"}, {FlagDemo, "demo"}, {FlagBeta, "beta"}, {FlagProto, "proto"}, {FlagSample, "sample"},
	{FlagUnlicensed, "unlicensed"}, {FlagPirate, "pirate"}, {FlagHack, "hack"}, {FlagAftermarket, "aftermarket"},
	{FlagProgram, "program"}, {FlagAlt, "alt"}, {FlagBadDump, "baddump"},
}

// Names lists the flags by name, for the API.
func (f Flags) Names() []string {
	names := []string{}
	for _, fn := range flagNames {
		if f&fn.flag != 0 {
			names = append(names, fn.name)
		}
	}
	return names
}

// Official reports whether the entry is an official retail release.
func (f Flags) Official() bool {
	return f&Unofficial == 0
}

var knownRegions = map[string]bool{}

func init() {
	for _, r := range []string{"World", "USA", "Europe", "Japan", "Asia", "Australia", "Austria", "Belgium", "Brazil", "Canada",
		"China", "Croatia", "Denmark", "Finland", "France", "Germany", "Greece", "Hong Kong", "India", "Ireland", "Israel",
		"Italy", "Korea", "Latin America", "Mexico", "Netherlands", "New Zealand", "Norway", "Poland", "Portugal", "Russia",
		"Scandinavia", "Singapore", "South Africa", "Spain", "Sweden", "Switzerland", "Taiwan", "Thailand", "Turkey", "UK",
		"United Kingdom", "Argentina", "Chile", "Colombia", "Czech", "Hungary", "Slovakia", "Ukraine", "Unknown"} {
		knownRegions[strings.ToLower(r)] = true
	}
}

// Info is what the name of a DAT entry tells.
type Info struct {
	// the name without its tags, such as "Super Mario World"
	Title     string
	Regions   []string
	Languages []string
	Flags     Flags
	// revision number, 0 for the first release
	Revision int
	// the disc of a game on several, 0 for a game on one
	Disc int
}

// Release is the name of the release a disc belongs to: the name without "(Disc 2)", so the
// discs of a game are counted as one release.
func Release(name string) string {
	i := strings.Index(strings.ToLower(name), " (disc ")
	if i < 0 {
		return name
	}
	end := strings.IndexByte(name[i+2:], ')')
	if end < 0 {
		return name
	}
	return name[:i] + name[i+2+end+1:]
}

// ParseName reads a No-Intro or Redump style name, such as
// "Super Mario World (Europe) (En,Fr,De) (Rev 1)".
func ParseName(name string) Info {
	info := Info{}
	// tags in front of the name, such as "[BIOS] Super Game Boy (World)"
	for strings.HasPrefix(name, "[") {
		end := strings.IndexByte(name, ']')
		if end < 0 {
			break
		}
		info.readBracketTag(name[1:end])
		name = strings.TrimSpace(name[end+1:])
	}
	cut := len(name)
	for _, sep := range []string{" (", " ["} {
		if i := strings.Index(name, sep); i >= 0 && i < cut {
			cut = i
		}
	}
	info.Title = strings.TrimSpace(name[:cut])
	rest := name[cut:]
	for len(rest) > 0 {
		open := strings.IndexAny(rest, "([")
		if open < 0 {
			break
		}
		closer := byte(')')
		if rest[open] == '[' {
			closer = ']'
		}
		end := strings.IndexByte(rest[open+1:], closer)
		if end < 0 {
			break
		}
		tag := rest[open+1 : open+1+end]
		rest = rest[open+1+end+1:]
		if closer == ']' {
			info.readBracketTag(tag)
		} else {
			info.readTag(tag)
		}
	}
	return info
}

func (info *Info) readBracketTag(tag string) {
	lower := strings.ToLower(tag)
	switch {
	case lower == "bios":
		info.Flags |= FlagBios
	case lower == "b" || strings.HasPrefix(lower, "b "):
		info.Flags |= FlagBadDump
	case strings.HasPrefix(lower, "h") && len(lower) <= 4:
		info.Flags |= FlagHack
	case lower == "t" || strings.HasPrefix(lower, "t+") || strings.HasPrefix(lower, "t-"):
		info.Flags |= FlagHack
	}
}

func (info *Info) readTag(tag string) {
	lower := strings.ToLower(strings.TrimSpace(tag))
	parts := strings.Split(tag, ",")
	if allRegions(parts) {
		for _, p := range parts {
			info.Regions = append(info.Regions, strings.TrimSpace(p))
		}
		return
	}
	if allLanguages(parts) {
		for _, p := range parts {
			info.Languages = append(info.Languages, strings.TrimSpace(p))
		}
		return
	}
	switch {
	case strings.HasPrefix(lower, "disc "):
		if n, err := strconv.Atoi(strings.Fields(lower[5:])[0]); err == nil {
			info.Disc = n
		} else {
			info.Disc = 1
		}
	case strings.HasPrefix(lower, "rev "):
		if n, err := strconv.Atoi(strings.TrimSpace(lower[4:])); err == nil {
			info.Revision = n
		} else {
			info.Revision = 1
		}
	case lower == "beta" || strings.HasPrefix(lower, "beta "):
		info.Flags |= FlagBeta
	case lower == "proto" || strings.HasPrefix(lower, "proto "):
		info.Flags |= FlagProto
	case lower == "demo" || strings.HasPrefix(lower, "demo ") || lower == "kiosk" || strings.HasPrefix(lower, "kiosk "):
		info.Flags |= FlagDemo
	case lower == "sample" || strings.HasPrefix(lower, "sample "):
		info.Flags |= FlagSample
	case lower == "unl":
		info.Flags |= FlagUnlicensed
	case lower == "pirate":
		info.Flags |= FlagPirate
	case lower == "hack" || strings.HasSuffix(lower, " hack"):
		info.Flags |= FlagHack
	case lower == "aftermarket" || lower == "homebrew":
		info.Flags |= FlagAftermarket
	case lower == "program" || strings.HasSuffix(lower, " program") || lower == "test program" || lower == "enhancement chip":
		info.Flags |= FlagProgram
	case lower == "alt" || strings.HasPrefix(lower, "alt "):
		info.Flags |= FlagAlt
	case lower == "bios":
		info.Flags |= FlagBios
	}
}

func allRegions(parts []string) bool {
	for _, p := range parts {
		if !knownRegions[strings.ToLower(strings.TrimSpace(p))] {
			return false
		}
	}
	return len(parts) > 0
}

// allLanguages reports whether every part looks like a language code: "En", "Fr", "Zh-Hant", "Pt-BR".
func allLanguages(parts []string) bool {
	for _, p := range parts {
		p = strings.TrimSpace(p)
		base := p
		if i := strings.IndexByte(p, '-'); i > 0 {
			base = p[:i]
		}
		if len(base) != 2 || base[0] < 'A' || base[0] > 'Z' || base[1] < 'a' || base[1] > 'z' {
			return false
		}
	}
	return len(parts) > 0
}
