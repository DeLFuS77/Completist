package dat

import (
	"reflect"
	"strings"
	"testing"
)

const clrmamepro = `clrmamepro (
	name "Nintendo - Super Nintendo Entertainment System"
	description "Nintendo - Super Nintendo Entertainment System"
	version "2026.10.07"
)

game (
	name "2020 Super Baseball (Japan)"
	region "Japan"
	rom ( name "2020 Super Baseball (Japan).sfc" size 1572864 crc E95A3DD7 md5 C9027B03A719A547CB2D9BCF9A9A6CBB sha1 FE448AE2C065DFF8B0C2AACC35F9D9EE9432B04F )
)
game (
	name "16 BIT XMAS 2011 - Christmas Craze (World)"
	rom ( name "16 BIT XMAS 2011 - Christmas Craze (World) (Aftermarket) (Unl).sfc" size 524288 crc 5F7B6627 sha1 7752CAA676D15D4D0C42F25548C3E86128A416DB )
)
`

const logiqx = `<?xml version="1.0"?>
<!DOCTYPE datafile PUBLIC "-//Logiqx//DTD ROM Management Datafile//EN" "http://www.logiqx.com/Dats/datafile.dtd">
<datafile>
	<header>
		<name>Nintendo - Game Boy</name>
		<description>Nintendo - Game Boy</description>
		<version>20261001-000000</version>
	</header>
	<game name="Tetris (World) (Rev 1)" cloneof="Tetris (World)">
		<description>Tetris (World) (Rev 1)</description>
		<rom name="Tetris (World) (Rev 1).gb" size="32768" crc="46df91ad" md5="084f1e457749cdec86183189bd88ce69" sha1="74591cc9501af93873f9a5d3eb12da12c0723bbc" status="verified"/>
	</game>
</datafile>`

func TestParseClrmamepro(t *testing.T) {
	d, err := Parse(strings.NewReader(clrmamepro))
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Nintendo - Super Nintendo Entertainment System" || d.Version != "2026.10.07" {
		t.Fatalf("header: %+v", d)
	}
	if len(d.Games) != 2 {
		t.Fatalf("games: %d", len(d.Games))
	}
	g := d.Games[0]
	if g.Region != "Japan" || len(g.Roms) != 1 || g.Roms[0].Size != 1572864 || g.Roms[0].CRC != "e95a3dd7" || g.Roms[0].SHA1 != "fe448ae2c065dff8b0c2aacc35f9d9ee9432b04f" {
		t.Fatalf("game: %+v", g)
	}
	if d.Games[1].Roms[0].MD5 != "" {
		t.Fatalf("a missing md5 must stay empty")
	}
}

func TestParseLogiqx(t *testing.T) {
	d, err := Parse(strings.NewReader(logiqx))
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Nintendo - Game Boy" || len(d.Games) != 1 {
		t.Fatalf("dat: %+v", d)
	}
	g := d.Games[0]
	if g.CloneOf != "Tetris (World)" || g.Roms[0].CRC != "46df91ad" || g.Roms[0].Status != "verified" {
		t.Fatalf("game: %+v", g)
	}
}

func TestParseRejectsOtherFiles(t *testing.T) {
	if _, err := Parse(strings.NewReader("hello world")); err == nil {
		t.Fatal("a text file must not be a DAT")
	}
	if _, err := Parse(strings.NewReader("<html><body></body></html>")); err == nil {
		t.Fatal("an HTML page must not be a DAT")
	}
}

func TestParseName(t *testing.T) {
	cases := []struct {
		name     string
		title    string
		regions  []string
		langs    []string
		official bool
		revision int
	}{
		{"Super Mario World (Europe) (En,Fr,De) (Rev 1)", "Super Mario World", []string{"Europe"}, []string{"En", "Fr", "De"}, true, 1},
		{"Chrono Trigger (USA)", "Chrono Trigger", []string{"USA"}, nil, true, 0},
		{"Star Fox (USA, Europe) (Beta)", "Star Fox", []string{"USA", "Europe"}, nil, false, 0},
		{"16 BIT XMAS 2011 - Christmas Craze (World) (Aftermarket) (Unl)", "16 BIT XMAS 2011 - Christmas Craze", []string{"World"}, nil, false, 0},
		{"[BIOS] Super Game Boy (World)", "Super Game Boy", []string{"World"}, nil, false, 0},
		{"Zelda no Densetsu (Japan) (Proto 2)", "Zelda no Densetsu", []string{"Japan"}, nil, false, 0},
		{"Pokemon Stadium (Europe) (Kiosk)", "Pokemon Stadium", []string{"Europe"}, nil, false, 0},
		{"Mega Man X (USA) (Pirate)", "Mega Man X", []string{"USA"}, nil, false, 0},
		{"Tetris (World) (Alt)", "Tetris", []string{"World"}, nil, true, 0},
	}
	for _, c := range cases {
		info := ParseName(c.name)
		if info.Title != c.title {
			t.Errorf("%s: title %q", c.name, info.Title)
		}
		if !reflect.DeepEqual(info.Regions, c.regions) {
			t.Errorf("%s: regions %v", c.name, info.Regions)
		}
		if !reflect.DeepEqual(info.Languages, c.langs) {
			t.Errorf("%s: languages %v", c.name, info.Languages)
		}
		if info.Flags.Official() != c.official {
			t.Errorf("%s: official %v (%v)", c.name, info.Flags.Official(), info.Flags.Names())
		}
		if info.Revision != c.revision {
			t.Errorf("%s: revision %d", c.name, info.Revision)
		}
	}
}
