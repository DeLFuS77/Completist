package server

import (
	"testing"

	"github.com/DeLFuS77/completist/internal/store"
)

func TestReviewReason(t *testing.T) {
	exp := []store.Expected{{Name: "Game (Japan).sfc", Size: 2097152, CRC: "0d8837c1"}}
	for _, c := range []struct {
		f    store.File
		exp  []store.Expected
		want string
	}{
		{store.File{Status: "bad", Size: 1572864}, exp, reasonSize},
		{store.File{Status: "bad", Size: 2097152}, exp, reasonContent},
		{store.File{Status: "nodat", Path: "x.sfc"}, nil, reasonNoDat},
		{store.File{Status: "unknown", Path: "/roms/snes/Aladdin (USA) (Beta).zip"}, nil, reasonPrerelease},
		{store.File{Status: "unknown", Path: "/roms/snes/Alien 3 (USA) (Beta 2).zip"}, nil, reasonPrerelease},
		{store.File{Status: "unknown", Path: "/roms/nes/0-to-X (World) (Demo) (Aftermarket) (Unl).zip"}, nil, reasonPrerelease},
		{store.File{Status: "unknown", Path: "/roms/nes/Starfight (World) (Aftermarket) (Unl).zip"}, nil, reasonHomebrew},
		{store.File{Status: "unknown", Path: "/roms/nes/Power Rangers 2 (Asia) (Ja) (Pirate).zip"}, nil, reasonPirate},
		{store.File{Status: "unknown", Path: "/roms/nes/Zelda (U) [T+Spa].nes"}, nil, reasonHack},
		{store.File{Status: "unknown", Path: "/roms/nes/240p Test Suite (World) (v0.01) (Program) (Aftermarket) (Unl).zip"}, nil, reasonHomebrew},
		{store.File{Status: "unknown", Path: "/roms/nes/Test (World) (Program).zip"}, nil, reasonProgram},
		{store.File{Status: "unknown", Path: "/roms/nes/Some Game (USA).zip"}, nil, reasonOther},
	} {
		if got := reviewReason(c.f, c.exp); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.f.Status, c.f.Path, got, c.want)
		}
	}
}
