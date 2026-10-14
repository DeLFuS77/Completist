// Package dat reads DAT files, the lists of known good dumps published by No-Intro, Redump and
// others. It reads the Logiqx XML format and the older clrmamepro text format.
package dat

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

// Rom is one file of a game, as the DAT describes it.
type Rom struct {
	Name string
	Size int64
	// hex, lower case; empty when the DAT does not give it
	CRC  string
	MD5  string
	SHA1 string
	// "baddump", "nodump" or "verified"; empty when the DAT does not say
	Status string
	Serial string
}

// Game is one entry of a DAT.
type Game struct {
	Name        string
	Description string
	CloneOf     string
	Region      string
	Roms        []Rom
}

// Dat is a parsed DAT file.
type Dat struct {
	Name        string
	Description string
	Version     string
	Homepage    string
	Games       []Game
}

var ErrFormat = errors.New("not a DAT file")

// Parse reads a DAT in either format.
func Parse(r io.Reader) (*Dat, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	head, _ := br.Peek(512)
	trimmed := bytes.TrimLeft(head, "\xef\xbb\xbf \t\r\n")
	switch {
	case bytes.HasPrefix(trimmed, []byte("<")):
		return parseXML(br)
	case bytes.HasPrefix(bytes.ToLower(trimmed), []byte("clrmamepro")) || bytes.HasPrefix(trimmed, []byte("game")):
		return parseClrmamepro(br)
	}
	return nil, ErrFormat
}

// ---- Logiqx XML ----

type xmlRom struct {
	Name   string `xml:"name,attr"`
	Size   string `xml:"size,attr"`
	CRC    string `xml:"crc,attr"`
	MD5    string `xml:"md5,attr"`
	SHA1   string `xml:"sha1,attr"`
	Status string `xml:"status,attr"`
	Serial string `xml:"serial,attr"`
}

type xmlGame struct {
	Name        string   `xml:"name,attr"`
	CloneOf     string   `xml:"cloneof,attr"`
	Description string   `xml:"description"`
	Roms        []xmlRom `xml:"rom"`
	Disks       []xmlRom `xml:"disk"`
}

type xmlHeader struct {
	Name        string `xml:"name"`
	Description string `xml:"description"`
	Version     string `xml:"version"`
	Homepage    string `xml:"homepage"`
}

func parseXML(r io.Reader) (*Dat, error) {
	decoder := xml.NewDecoder(r)
	decoder.Strict = false
	out := &Dat{}
	seenRoot := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("DAT: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "datafile":
			seenRoot = true
		case "header":
			var h xmlHeader
			if err := decoder.DecodeElement(&h, &start); err != nil {
				return nil, fmt.Errorf("DAT header: %w", err)
			}
			out.Name, out.Description, out.Version, out.Homepage = strings.TrimSpace(h.Name), strings.TrimSpace(h.Description), strings.TrimSpace(h.Version), strings.TrimSpace(h.Homepage)
		case "game", "machine":
			var g xmlGame
			if err := decoder.DecodeElement(&g, &start); err != nil {
				return nil, fmt.Errorf("DAT game: %w", err)
			}
			game := Game{Name: g.Name, Description: strings.TrimSpace(g.Description), CloneOf: g.CloneOf}
			for _, rom := range append(g.Roms, g.Disks...) {
				size, _ := strconv.ParseInt(rom.Size, 10, 64)
				game.Roms = append(game.Roms, Rom{Name: rom.Name, Size: size, CRC: hexLower(rom.CRC), MD5: hexLower(rom.MD5),
					SHA1: hexLower(rom.SHA1), Status: strings.ToLower(rom.Status), Serial: rom.Serial})
			}
			out.Games = append(out.Games, game)
		}
	}
	if !seenRoot {
		return nil, ErrFormat
	}
	return out, nil
}

// ---- clrmamepro ----

// tokenizer splits the clrmamepro format into words, quoted strings and parentheses.
type tokenizer struct {
	r *bufio.Reader
}

func (t *tokenizer) next() (string, bool, error) {
	for {
		c, _, err := t.r.ReadRune()
		if err != nil {
			return "", false, err
		}
		if unicode.IsSpace(c) {
			continue
		}
		switch c {
		case '(', ')':
			return string(c), false, nil
		case '"':
			var b strings.Builder
			for {
				c, _, err := t.r.ReadRune()
				if err != nil {
					return "", false, err
				}
				if c == '"' {
					return b.String(), true, nil
				}
				b.WriteRune(c)
			}
		default:
			var b strings.Builder
			b.WriteRune(c)
			for {
				c, _, err := t.r.ReadRune()
				if err != nil {
					return b.String(), false, nil
				}
				if unicode.IsSpace(c) || c == '(' || c == ')' {
					t.r.UnreadRune()
					return b.String(), false, nil
				}
				b.WriteRune(c)
			}
		}
	}
}

// node is a block like `game ( name "x" rom ( ... ) )`: its fields and its child blocks.
type node struct {
	fields   map[string]string
	children []child
}

type child struct {
	name string
	node *node
}

// maxDepth is how deep blocks may nest; real DATs use two levels
const maxDepth = 8

func (t *tokenizer) block(depth int) (*node, error) {
	if depth > maxDepth {
		return nil, errors.New("blocks nested too deeply")
	}
	n := &node{fields: map[string]string{}}
	for {
		key, quoted, err := t.next()
		if err != nil {
			return nil, err
		}
		if key == ")" && !quoted {
			return n, nil
		}
		value, quoted, err := t.next()
		if err != nil {
			return nil, err
		}
		if value == "(" && !quoted {
			sub, err := t.block(depth + 1)
			if err != nil {
				return nil, err
			}
			n.children = append(n.children, child{name: strings.ToLower(key), node: sub})
			continue
		}
		if _, exists := n.fields[strings.ToLower(key)]; !exists {
			n.fields[strings.ToLower(key)] = value
		}
	}
}

func parseClrmamepro(r io.Reader) (*Dat, error) {
	t := &tokenizer{r: bufio.NewReaderSize(r, 64*1024)}
	out := &Dat{}
	found := false
	for {
		word, _, err := t.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("DAT: %w", err)
		}
		open, _, err := t.next()
		if err != nil || open != "(" {
			return nil, fmt.Errorf("DAT: expected ( after %q", word)
		}
		n, err := t.block(1)
		if err != nil {
			return nil, fmt.Errorf("DAT: unexpected end in %q", word)
		}
		switch strings.ToLower(word) {
		case "clrmamepro":
			found = true
			out.Name, out.Description, out.Version, out.Homepage = n.fields["name"], n.fields["description"], n.fields["version"], n.fields["homepage"]
		case "game", "machine", "resource":
			found = true
			game := Game{Name: n.fields["name"], Description: n.fields["description"], CloneOf: n.fields["cloneof"], Region: n.fields["region"]}
			for _, c := range n.children {
				if c.name != "rom" && c.name != "disk" {
					continue
				}
				size, _ := strconv.ParseInt(c.node.fields["size"], 10, 64)
				game.Roms = append(game.Roms, Rom{Name: c.node.fields["name"], Size: size, CRC: hexLower(c.node.fields["crc"]),
					MD5: hexLower(c.node.fields["md5"]), SHA1: hexLower(c.node.fields["sha1"]), Status: strings.ToLower(c.node.fields["flags"]),
					Serial: firstNonEmpty(c.node.fields["serial"], n.fields["serial"])})
			}
			out.Games = append(out.Games, game)
		}
	}
	if !found {
		return nil, ErrFormat
	}
	return out, nil
}

func hexLower(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return ""
		}
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
