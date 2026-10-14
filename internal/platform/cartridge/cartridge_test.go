package cartridge

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"math/rand/v2"
	"testing"

	"github.com/DeLFuS77/completist/internal/platform"
	"github.com/DeLFuS77/completist/internal/systems"
)

func sha(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

func data(n int, seed uint64) []byte {
	r := rand.New(rand.NewPCG(seed, seed))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.UintN(256))
	}
	return b
}

func hashOf(t *testing.T, id string, content []byte) []platform.Hashes {
	t.Helper()
	sys, ok := systems.ByID(id)
	if !ok {
		t.Fatalf("no system %s", id)
	}
	h, err := Plugin{}.Hash(sys, bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestPlainFile(t *testing.T) {
	rom := data(32768, 1)
	h := hashOf(t, "gb", rom)
	if len(h) != 1 || h[0].SHA1 != sha(rom) || h[0].Size != 32768 || h[0].Form != "raw" {
		t.Fatalf("%+v", h)
	}
}

func TestNESHeader(t *testing.T) {
	rom := data(40960, 2)
	file := append([]byte("NES\x1a\x02\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), rom...)
	h := hashOf(t, "nes", file)
	if len(h) != 2 || h[0].SHA1 != sha(file) || h[1].SHA1 != sha(rom) || h[1].Size != 40960 || h[1].Form != "headerless" {
		t.Fatalf("%+v", h)
	}
}

func TestSNESCopierHeader(t *testing.T) {
	// larger than one read buffer, so the header is cut across reads correctly
	rom := data(1024*1024, 3)
	file := append(make([]byte, 512), rom...)
	h := hashOf(t, "snes", file)
	if len(h) != 2 || h[1].SHA1 != sha(rom) {
		t.Fatalf("%+v", h)
	}
	// without a header, only one form
	if h := hashOf(t, "snes", rom); len(h) != 1 {
		t.Fatalf("headerless file: %+v", h)
	}
}

func TestN64ByteOrders(t *testing.T) {
	z64 := append([]byte{0x80, 0x37, 0x12, 0x40}, data(1024*1024-4, 4)...)
	v64 := make([]byte, len(z64))
	n64 := make([]byte, len(z64))
	for i := 0; i < len(z64); i += 2 {
		v64[i], v64[i+1] = z64[i+1], z64[i]
	}
	for i := 0; i < len(z64); i += 4 {
		n64[i], n64[i+1], n64[i+2], n64[i+3] = z64[i+3], z64[i+2], z64[i+1], z64[i]
	}
	if h := hashOf(t, "n64", z64); len(h) != 1 || h[0].SHA1 != sha(z64) {
		t.Fatalf("z64: %+v", h)
	}
	for name, file := range map[string][]byte{"v64": v64, "n64": n64} {
		h := hashOf(t, "n64", file)
		if len(h) != 2 || h[1].SHA1 != sha(z64) || h[1].Form != "byteswapped" {
			t.Fatalf("%s: %+v", name, h)
		}
	}
}
