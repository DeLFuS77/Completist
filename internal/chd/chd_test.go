package chd

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The fixture in testdata is a small CD image made with chdman from the BIN files makeDisc
// writes: a data track with sectors chdman can rebuild (sync and ECC removed), and two audio
// tracks, one with its pregap in the file. Its BIN hashes are in testdata/disc.json.

func toBCD(n int) byte { return byte(n/10<<4 | n%10) }

// makeDisc writes a three-track image in the Redump layout and returns the files by track.
func makeDisc(t *testing.T, dir string) map[int]string {
	t.Helper()
	r := rand.New(rand.NewPCG(5, 9))
	// track 1: mode 2 form 1 sectors with valid ECC, a few with damaged ECC that must stay as
	// they are, and readable text so the image compresses
	var t1 bytes.Buffer
	for s := 0; s < 120; s++ {
		sector := make([]byte, sectorSize)
		copy(sector, syncHeader[:])
		lba := s + 150
		sector[12], sector[13], sector[14], sector[15] = toBCD(lba/75/60), toBCD(lba/75%60), toBCD(lba%75), 2
		for i := 16; i < 24; i++ {
			sector[i] = byte(s % 3)
		}
		text := []byte(fmt.Sprintf("SECTOR %05d COMPLETIST TEST DISC ", s))
		for i := 24; i < 2072; i++ {
			sector[i] = text[i%len(text)]
			if s%7 == 0 {
				sector[i] = byte(r.UintN(256))
			}
		}
		eccGenerate(sector)
		if s%40 == 39 {
			sector[2100] ^= 0xff
		}
		t1.Write(sector)
	}
	// audio: stereo sine waves, little-endian as in a BIN file
	audio := func(frames int, freq float64) []byte {
		var b bytes.Buffer
		for i := 0; i < frames*sectorSize/4; i++ {
			l := int16(9000 * math.Sin(float64(i)*freq))
			rr := int16(7000*math.Sin(float64(i)*freq*1.5) + float64(r.IntN(40)))
			binary.Write(&b, binary.LittleEndian, l)
			binary.Write(&b, binary.LittleEndian, rr)
		}
		return b.Bytes()
	}
	files := map[int]string{1: "disc (Track 1).bin", 2: "disc (Track 2).bin", 3: "disc (Track 3).bin"}
	os.WriteFile(filepath.Join(dir, files[1]), t1.Bytes(), 0o644)
	// track 2 has a 2-second pregap of silence inside its file, track 3 none
	t2 := append(make([]byte, 150*sectorSize), audio(30, 0.031)...)
	os.WriteFile(filepath.Join(dir, files[2]), t2, 0o644)
	os.WriteFile(filepath.Join(dir, files[3]), audio(20, 0.017), 0o644)
	cue := `FILE "disc (Track 1).bin" BINARY
  TRACK 01 MODE2/2352
    INDEX 01 00:00:00
FILE "disc (Track 2).bin" BINARY
  TRACK 02 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:02:00
FILE "disc (Track 3).bin" BINARY
  TRACK 03 AUDIO
    INDEX 01 00:00:00
`
	os.WriteFile(filepath.Join(dir, "disc.cue"), []byte(cue), 0o644)
	return files
}

func sha1File(t *testing.T, path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

func trackHashes(t *testing.T, path string) map[int]string {
	t.Helper()
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tracks, err := c.Tracks()
	if err != nil {
		t.Fatal(err)
	}
	out := map[int]string{}
	for _, tr := range tracks {
		h := sha1.New()
		n, err := tr.WriteTo(h)
		if err != nil {
			t.Fatalf("track %d: %v", tr.Number, err)
		}
		if n != tr.Size() {
			t.Fatalf("track %d: %d bytes, size %d", tr.Number, n, tr.Size())
		}
		out[tr.Number] = hex.EncodeToString(h.Sum(nil))
	}
	return out
}

// The fixture reads back as the BIN files it was made from.
func TestFixture(t *testing.T) {
	var want map[string]string
	data, err := os.ReadFile("testdata/disc.json")
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(data, &want)
	before := eccRebuilt.Load()
	for _, name := range []string{"disc-default.chd", "disc-zlib.chd"} {
		got := trackHashes(t, filepath.Join("testdata", name))
		for n := 1; n <= 3; n++ {
			if got[n] != want[fmt.Sprint(n)] {
				t.Errorf("%s track %d: %s, want %s", name, n, got[n], want[fmt.Sprint(n)])
			}
		}
	}
	// the sectors chdman stripped were rebuilt
	if eccRebuilt.Load() == before {
		t.Fatal("no sector had its ECC rebuilt")
	}
}

func TestNotCHD(t *testing.T) {
	if _, err := Open("testdata/disc.json"); err != ErrNotCHD {
		t.Fatalf("%v", err)
	}
}

// With CHDMAN set to a chdman binary, the fixtures are made again and compared with the BIN
// files: go test ./internal/chd -run Make -chdman ... writes them to testdata.
func TestMakeFixtures(t *testing.T) {
	chdman := os.Getenv("CHDMAN")
	if chdman == "" {
		t.Skip("CHDMAN not set")
	}
	dir := t.TempDir()
	files := makeDisc(t, dir)
	want := map[string]string{}
	for n, f := range files {
		want[fmt.Sprint(n)] = sha1File(t, filepath.Join(dir, f))
	}
	variants := map[string][]string{
		"disc-default.chd": nil,
		"disc-zlib.chd":    {"-c", "cdzl"},
		"disc-lzma.chd":    {"-c", "cdlz"},
		"disc-flac.chd":    {"-c", "cdfl"},
	}
	for name, extra := range variants {
		out := filepath.Join(dir, name)
		args := append([]string{"createcd", "-i", filepath.Join(dir, "disc.cue"), "-o", out, "-f"}, extra...)
		if b, err := exec.Command(chdman, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, b)
		}
		got := trackHashes(t, out)
		for n := 1; n <= 3; n++ {
			if got[n] != want[fmt.Sprint(n)] {
				t.Errorf("%s track %d: %s, want %s", name, n, got[n], want[fmt.Sprint(n)])
			}
		}
		if d := os.Getenv("KEEP_DIR"); d != "" {
			b, _ := os.ReadFile(out)
			os.WriteFile(filepath.Join(d, name), b, 0o644)
		}
		if os.Getenv("WRITE_FIXTURES") != "" && (name == "disc-default.chd" || name == "disc-zlib.chd") {
			b, _ := os.ReadFile(out)
			os.WriteFile(filepath.Join("testdata", name), b, 0o644)
		}
	}
	if os.Getenv("WRITE_FIXTURES") != "" {
		b, _ := json.MarshalIndent(want, "", "  ")
		os.WriteFile("testdata/disc.json", b, 0o644)
	}
}

// A damaged file is an error, never a crash.
func TestDamaged(t *testing.T) {
	data, err := os.ReadFile("testdata/disc-default.chd")
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(3, 3))
	dir := t.TempDir()
	for round := 0; round < 200; round++ {
		bad := append([]byte(nil), data...)
		for i := 0; i < 1+int(r.UintN(20)); i++ {
			bad[r.UintN(uint(len(bad)))] ^= byte(1 + r.UintN(255))
		}
		path := filepath.Join(dir, "bad.chd")
		os.WriteFile(path, bad, 0o644)
		c, err := Open(path)
		if err != nil {
			continue
		}
		if tracks, err := c.Tracks(); err == nil {
			for _, tr := range tracks {
				tr.WriteTo(io.Discard)
			}
		}
		c.Close()
	}
}
