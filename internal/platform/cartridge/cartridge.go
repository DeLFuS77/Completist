// Package cartridge is the plugin for cartridge systems: a game is one file, identified by its
// checksums. Some dumps carry a copier header and Nintendo 64 dumps come in three byte orders,
// so every form a DAT may list is hashed in one read.
package cartridge

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"hash"
	"hash/crc32"
	"io"

	"github.com/DeLFuS77/completist/internal/platform"
	"github.com/DeLFuS77/completist/internal/systems"
)

func init() {
	platform.Register(Plugin{})
}

// Plugin hashes cartridge dumps.
type Plugin struct{}

func (Plugin) ID() string { return "cartridge" }

type hasher struct {
	form string
	crc  hash.Hash32
	md5  hash.Hash
	sha1 hash.Hash
	size int64
}

func newHasher(form string) *hasher {
	return &hasher{form: form, crc: crc32.NewIEEE(), md5: md5.New(), sha1: sha1.New()}
}

func (h *hasher) Write(p []byte) (int, error) {
	h.crc.Write(p)
	h.md5.Write(p)
	h.sha1.Write(p)
	h.size += int64(len(p))
	return len(p), nil
}

func (h *hasher) sum() platform.Hashes {
	return platform.Hashes{Size: h.size, CRC: hex.EncodeToString(h.crc.Sum(nil)), MD5: hex.EncodeToString(h.md5.Sum(nil)),
		SHA1: hex.EncodeToString(h.sha1.Sum(nil)), Form: h.form}
}

const bufferSize = 256 * 1024

// Hash reads the file once and returns its checksums as is, without its header when it has
// one, and in big-endian byte order for Nintendo 64 dumps stored otherwise.
func (Plugin) Hash(system *systems.System, r io.Reader, size int64) ([]platform.Hashes, error) {
	raw := newHasher("raw")
	buffer := make([]byte, bufferSize)
	// the first block decides which other forms are needed
	n, err := io.ReadFull(r, buffer)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	first := buffer[:n]

	var headerless *hasher
	skip := 0
	if h := system.Header; h != nil && hasHeader(h, first, size) {
		headerless = newHasher("headerless")
		skip = h.Size
	}
	var swapped *hasher
	swap := byteOrder(first)
	if system.N64ByteOrder && swap != 0 {
		swapped = newHasher("byteswapped")
	}

	feed := func(block []byte, offset int64) {
		raw.Write(block)
		if headerless != nil {
			start := int64(skip) - offset
			if start < int64(len(block)) {
				if start < 0 {
					start = 0
				}
				headerless.Write(block[start:])
			}
		}
		if swapped != nil {
			swapped.Write(toBigEndian(block, swap))
		}
	}

	offset := int64(0)
	feed(first, offset)
	offset += int64(len(first))
	// a block size that is a multiple of 4 keeps the byte order conversion aligned
	for err == nil {
		n, err = io.ReadFull(r, buffer)
		if n > 0 {
			feed(buffer[:n], offset)
			offset += int64(n)
		}
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			return nil, err
		}
	}

	out := []platform.Hashes{raw.sum()}
	if headerless != nil {
		out = append(out, headerless.sum())
	}
	if swapped != nil {
		out = append(out, swapped.sum())
	}
	return out, nil
}

func hasHeader(h *systems.Header, first []byte, size int64) bool {
	if size <= int64(h.Size) {
		return false
	}
	if len(h.Magic) > 0 {
		return bytes.HasPrefix(first, h.Magic)
	}
	return h.Modulo > 0 && size%int64(h.Modulo) == int64(h.Size)
}

// byte orders of Nintendo 64 dumps, told by their first four bytes
const (
	orderBig    = 0 // .z64: 80 37 12 40
	orderSwap16 = 1 // .v64: 37 80 40 12
	orderLittle = 2 // .n64: 40 12 37 80
)

func byteOrder(first []byte) int {
	switch {
	case bytes.HasPrefix(first, []byte{0x37, 0x80, 0x40, 0x12}):
		return orderSwap16
	case bytes.HasPrefix(first, []byte{0x40, 0x12, 0x37, 0x80}):
		return orderLittle
	}
	return orderBig
}

func toBigEndian(block []byte, order int) []byte {
	out := make([]byte, len(block))
	copy(out, block)
	switch order {
	case orderSwap16:
		for i := 0; i+1 < len(out); i += 2 {
			out[i], out[i+1] = out[i+1], out[i]
		}
	case orderLittle:
		for i := 0; i+3 < len(out); i += 4 {
			out[i], out[i+1], out[i+2], out[i+3] = out[i+3], out[i+2], out[i+1], out[i]
		}
	}
	return out
}
