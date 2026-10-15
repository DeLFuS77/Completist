package discimage

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"hash/adler32"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// an ISO with parts that compress and parts that do not, and a short last GCZ block
func testISO(t *testing.T, sectors int) (string, [20]byte) {
	data := make([]byte, sectors*2048)
	rand.Read(data[:len(data)/3])
	for i := len(data) / 3; i < 2*len(data)/3; i++ {
		data[i] = byte(i / 7)
	}
	copy(data, "GALE01")
	path := filepath.Join(t.TempDir(), "game.iso")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, sha1.Sum(data)
}

func roundTrip(t *testing.T, out string, size int64, want [20]byte) {
	t.Helper()
	img, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if img.Size() != size {
		t.Fatalf("size %d, want %d", img.Size(), size)
	}
	if s, err := ImageSize(out); err != nil || s != size {
		t.Fatalf("header size %d %v", s, err)
	}
	var buf bytes.Buffer
	if _, err := img.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if sha1.Sum(buf.Bytes()) != want {
		t.Fatal("the image does not give back the ISO")
	}
	info, _ := os.Stat(out)
	if info.Size() >= size {
		t.Fatalf("not smaller: %d >= %d", info.Size(), size)
	}
}

func TestWriteGCZ(t *testing.T) {
	iso, sum := testISO(t, 16*20+3)
	out := filepath.Join(t.TempDir(), "game.gcz")
	calls := 0
	if err := WriteGCZ(out, iso, false, func(done, total int64) { calls++ }); err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("no progress")
	}
	roundTrip(t, out, (16*20+3)*2048, sum)
	if err := WriteGCZ(out, iso, false, nil); err == nil {
		t.Fatal("an existing file was overwritten")
	}
}

func TestWriteCSO(t *testing.T) {
	iso, sum := testISO(t, 1500)
	out := filepath.Join(t.TempDir(), "game.cso")
	if err := WriteCSO(out, iso, nil); err != nil {
		t.Fatal(err)
	}
	roundTrip(t, out, 1500*2048, sum)
}

func TestWriteRejectsPartialSectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odd.iso")
	os.WriteFile(path, make([]byte, 3000), 0o644)
	if err := WriteCSO(filepath.Join(t.TempDir(), "x.cso"), path, nil); err == nil {
		t.Fatal("accepted")
	}
}

// TestGCZAsDolphin reads a GCZ the way Dolphin does: every block whole, stored blocks exactly
// one block long, and the Adler-32 of what is stored in the table.
func TestGCZAsDolphin(t *testing.T) {
	iso, _ := testISO(t, 16*6+5)
	out := filepath.Join(t.TempDir(), "game.gcz")
	if err := WriteGCZ(out, iso, true, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	le := binary.LittleEndian
	if le.Uint32(data[4:]) != 1 {
		t.Fatal("not marked as Wii")
	}
	compressed := le.Uint64(data[8:])
	block := uint64(le.Uint32(data[24:]))
	n := uint64(le.Uint32(data[28:]))
	start := 32 + 12*n
	if uint64(len(data)) != start+compressed {
		t.Fatalf("file %d, header says %d", len(data), start+compressed)
	}
	for i := uint64(0); i < n; i++ {
		p := le.Uint64(data[32+8*i:])
		next := compressed
		if i+1 < n {
			next = le.Uint64(data[32+8*(i+1):]) &^ (1 << 63)
		}
		stored := data[start+p&^(1<<63) : start+next]
		if adler32.Checksum(stored) != le.Uint32(data[32+8*n+4*i:]) {
			t.Fatalf("block %d: wrong hash", i)
		}
		if p&(1<<63) != 0 {
			if uint64(len(stored)) != block {
				t.Fatalf("block %d: stored with %d bytes", i, len(stored))
			}
			continue
		}
		r, err := zlib.NewReader(bytes.NewReader(stored))
		if err != nil {
			t.Fatal(err)
		}
		plain, err := io.ReadAll(r)
		if err != nil || uint64(len(plain)) != block {
			t.Fatalf("block %d: inflates to %d bytes (%v)", i, len(plain), err)
		}
	}
}
