package server

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DeLFuS77/completist/internal/chd"
)

func makeGCZ(data []byte, block int) []byte {
	blocks := (len(data) + block - 1) / block
	var payload bytes.Buffer
	pointers := make([]uint64, blocks)
	for i := 0; i < blocks; i++ {
		part := data[i*block : min(len(data), (i+1)*block)]
		pointers[i] = uint64(payload.Len())
		if i%3 == 2 {
			// stored as it is
			pointers[i] |= 1 << 63
			payload.Write(part)
			continue
		}
		w := zlib.NewWriter(&payload)
		w.Write(part)
		w.Close()
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint32(0xB10BC001))
	binary.Write(&out, binary.LittleEndian, uint32(0))
	binary.Write(&out, binary.LittleEndian, uint64(payload.Len()))
	binary.Write(&out, binary.LittleEndian, uint64(len(data)))
	binary.Write(&out, binary.LittleEndian, uint32(block))
	binary.Write(&out, binary.LittleEndian, uint32(blocks))
	for _, p := range pointers {
		binary.Write(&out, binary.LittleEndian, p)
	}
	out.Write(make([]byte, 4*blocks))
	out.Write(payload.Bytes())
	return out.Bytes()
}

func makeCSO(data []byte, block int) []byte {
	blocks := (len(data) + block - 1) / block
	header := 24 + 4*(blocks+1)
	var payload bytes.Buffer
	index := make([]uint32, blocks+1)
	for i := 0; i < blocks; i++ {
		part := data[i*block : min(len(data), (i+1)*block)]
		index[i] = uint32(header + payload.Len())
		if i%4 == 3 {
			index[i] |= 0x80000000
			payload.Write(part)
			continue
		}
		w, _ := flate.NewWriter(&payload, 9)
		w.Write(part)
		w.Close()
	}
	index[blocks] = uint32(header + payload.Len())
	var out bytes.Buffer
	out.WriteString("CISO")
	binary.Write(&out, binary.LittleEndian, uint32(24))
	binary.Write(&out, binary.LittleEndian, uint64(len(data)))
	binary.Write(&out, binary.LittleEndian, uint32(block))
	out.Write([]byte{1, 0, 0, 0})
	for _, v := range index {
		binary.Write(&out, binary.LittleEndian, v)
	}
	out.Write(payload.Bytes())
	return out.Bytes()
}

// a GameCube disc header, as an RVZ file keeps a copy of it
func makeRVZ(code string, disc, version byte) []byte {
	b := make([]byte, 0x200)
	copy(b, "RVZ\x01")
	h := b[0x58:]
	copy(h, code+"01")
	h[6], h[7] = disc, version
	binary.BigEndian.PutUint32(h[0x1C:], 0xC2339F3D)
	return b
}

func TestMoreDiscs(t *testing.T) {
	// a GD-ROM in a CHD, and its tracks as files with a GDI
	gd, err := chd.Open(filepath.Join("..", "chd", "testdata", "gd.chd"))
	if err != nil {
		t.Fatal(err)
	}
	tracks, _ := gd.Tracks()
	var t1, t2, t3 bytes.Buffer
	tracks[0].WriteTo(&t1)
	tracks[1].WriteTo(&t2)
	tracks[2].WriteTo(&t3)
	gd.Close()
	// a DVD in a CHD
	dvd, _ := chd.Open(filepath.Join("..", "chd", "testdata", "dvd-default.chd"))
	var iso bytes.Buffer
	dvd.WriteTo(&iso)
	dvd.Close()
	gcISO, pspISO := content(61, 300000), content(62, 200000)

	dc := "clrmamepro (\n\tname \"Sega - Dreamcast\"\n)\n" +
		"game (\n\tname \"Sky Game (Japan)\"\n" + romLine("Sky Game (Japan) (Track 3).bin", t3.Bytes()) + ")\n"
	ps2 := "clrmamepro (\n\tname \"Sony - PlayStation 2\"\n)\n" +
		"game (\n\tname \"Big Game (Europe)\"\n" + romLine("Big Game (Europe).iso", iso.Bytes()) + ")\n"
	psp := "clrmamepro (\n\tname \"Sony - PlayStation Portable\"\n)\n" +
		"game (\n\tname \"Pocket Game (USA)\"\n" + romLine("Pocket Game (USA).iso", pspISO) + ")\n"
	line := func(name string, b []byte, serial string) string {
		return strings.Replace(romLine(name, b), " )\n", " serial \""+serial+"\" )\n", 1)
	}
	gc := "clrmamepro (\n\tname \"Nintendo - GameCube\"\n)\n" +
		"game (\n\tname \"Cube Game (USA)\"\n" + line("Cube Game (USA).iso", gcISO, "DL-DOL-GCGE-USA") + ")\n" +
		"game (\n\tname \"Saga (USA) (Disc 2) (Rev 1)\"\n" + line("Saga (USA) (Disc 2) (Rev 1).iso", content(63, 1000), "DL-DOL-GSAE-USA") + ")\n"

	f := newFullFixture(t)
	for _, d := range []string{dc, ps2, psp, gc} {
		if rec, _ := f.do("POST", "/api/dats/import", d); rec.Code != 200 {
			t.Fatalf("import: %d %s", rec.Code, rec.Body)
		}
	}
	gdData, _ := os.ReadFile(filepath.Join("..", "chd", "testdata", "gd.chd"))
	dvdData, _ := os.ReadFile(filepath.Join("..", "chd", "testdata", "dvd-default.chd"))
	f.write("dreamcast/Sky Game (Japan).chd", gdData)
	f.write("dreamcast/Sky GDI/track01.bin", t1.Bytes())
	f.write("dreamcast/Sky GDI/track02.raw", t2.Bytes())
	f.write("dreamcast/Sky GDI/track03.bin", t3.Bytes())
	f.write("dreamcast/Sky GDI/disc.gdi", []byte("3\n1 0 4 2352 track01.bin 0\n2 210 0 2352 track02.raw 0\n3 45000 4 2352 track03.bin 0\n"))
	f.write("ps2/Big Game (Europe).chd", dvdData)
	f.write("psp/Pocket Game (USA).cso", makeCSO(pspISO, 2048))
	f.write("gc/Cube Game (USA).gcz", makeGCZ(gcISO, 16384))
	f.write("gc/Saga (USA) (Disc 2).rvz", makeRVZ("GSAE", 1, 1))
	f.write("gc/Other.rvz", makeRVZ("GZZE", 0, 0))
	f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	f.do("PUT", "/api/preferences", map[string]any{"defaultMode": "full", "modes": map[string]string{}, "regions": []string{"USA"}})
	if err := f.scanner.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}

	type sys struct {
		Have  int
		Total int
		Files struct{ Files, Ok, Unknown, Duplicates int }
	}
	check := func(id string, have, files, unknown int) {
		t.Helper()
		var s sys
		f.list("/api/systems/"+id, &s)
		if s.Have != have || s.Files.Files != files || s.Files.Unknown != unknown {
			t.Errorf("%s: %+v", id, s)
		}
	}
	// the CHD and the GDI tracks: track 3 of each, the same game twice
	check("dreamcast", 1, 2, 0)
	check("ps2", 1, 1, 0)
	check("psp", 1, 1, 0)
	// the GCZ by its content, the RVZ by its disc header; the other RVZ is not in the DAT
	check("gamecube", 2, 3, 1)

	var game struct {
		Files []struct {
			Path    string
			AltForm string
		}
	}
	var games struct {
		Items []struct {
			ID   int64
			Name string
		}
	}
	f.list("/api/systems/gamecube/games?filter=have", &games)
	for _, g := range games.Items {
		if strings.HasPrefix(g.Name, "Saga") {
			f.list("/api/games/"+strconv.FormatInt(g.ID, 10), &game)
		}
	}
	if len(game.Files) != 1 || game.Files[0].AltForm != "header" {
		t.Fatalf("rvz: %+v", game)
	}

	gl := unzip(t, f.raw("/api/export/esde").Body.Bytes())["gamelists/dreamcast/gamelist.xml"]
	if !strings.Contains(gl, "<path>./Sky GDI/disc.gdi</path>") || !strings.Contains(gl, "<path>./Sky Game (Japan).chd</path>") {
		t.Fatalf("gamelist: %s", gl)
	}
}
