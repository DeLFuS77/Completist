// Package discimage reads the compressed disc images of GameCube, Wii and PSP: GCZ and CSO are
// read back as the ISO they came from, so they are hashed like it; RVZ, WIA and WBFS keep only
// what the game needs, so they are identified by the game code in their disc header.
package discimage

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var (
	ErrFormat      = errors.New("not a disc image of this kind")
	ErrUnsupported = errors.New("this kind of image is not supported")
	errDamaged     = errors.New("damaged disc image")
)

// Image is a compressed image read back as its ISO.
type Image interface {
	Size() int64
	WriteTo(w io.Writer) (int64, error)
	Close() error
}

// Open opens a GCZ or CSO image by its extension.
func Open(path string) (Image, error) {
	switch strings.ToLower(path[strings.LastIndexByte(path, '.')+1:]) {
	case "gcz":
		return openGCZ(path)
	case "cso", "ciso":
		return openCSO(path)
	}
	return nil, ErrFormat
}

// Expands reports whether a file is an image Open reads.
func Expands(path string) bool {
	switch strings.ToLower(path[strings.LastIndexByte(path, '.')+1:]) {
	case "gcz", "cso":
		return true
	}
	return false
}

// ---- GCZ: Dolphin's compressed GameCube and Wii images ----

type gcz struct {
	f         *os.File
	dataSize  int64
	blockSize int64
	pointers  []uint64
	dataStart int64
	end       int64
}

func openGCZ(path string) (Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	h := make([]byte, 32)
	if _, err := io.ReadFull(f, h); err != nil || binary.LittleEndian.Uint32(h) != 0xB10BC001 {
		f.Close()
		return nil, ErrFormat
	}
	g := &gcz{f: f}
	compressed := int64(binary.LittleEndian.Uint64(h[8:]))
	g.dataSize = int64(binary.LittleEndian.Uint64(h[16:]))
	g.blockSize = int64(binary.LittleEndian.Uint32(h[24:]))
	blocks := int64(binary.LittleEndian.Uint32(h[28:]))
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	// the table of the blocks must fit in the file
	if 32+12*blocks > info.Size() {
		f.Close()
		return nil, errDamaged
	}
	if g.blockSize <= 0 || g.blockSize > 64<<20 || blocks <= 0 || blocks > 1<<24 || (blocks-1)*g.blockSize >= g.dataSize+g.blockSize {
		f.Close()
		return nil, errDamaged
	}
	table := make([]byte, 8*blocks)
	if _, err := io.ReadFull(f, table); err != nil {
		f.Close()
		return nil, errDamaged
	}
	g.pointers = make([]uint64, blocks)
	for i := range g.pointers {
		g.pointers[i] = binary.LittleEndian.Uint64(table[8*i:])
	}
	g.dataStart = 32 + 12*blocks
	g.end = compressed
	return g, nil
}

func (g *gcz) Size() int64  { return g.dataSize }
func (g *gcz) Close() error { return g.f.Close() }

func (g *gcz) WriteTo(w io.Writer) (int64, error) {
	var written int64
	out := make([]byte, g.blockSize)
	const raw = uint64(1) << 63
	for i, p := range g.pointers {
		start := int64(p &^ raw)
		end := g.end
		if i+1 < len(g.pointers) {
			end = int64(g.pointers[i+1] &^ raw)
		}
		if end < start || end-start > 2*g.blockSize+1024 {
			return written, errDamaged
		}
		block := make([]byte, end-start)
		if _, err := g.f.ReadAt(block, g.dataStart+start); err != nil {
			return written, errDamaged
		}
		want := min(g.blockSize, g.dataSize-int64(i)*g.blockSize)
		if want <= 0 {
			break
		}
		var data []byte
		if p&raw != 0 {
			if int64(len(block)) < want {
				return written, errDamaged
			}
			data = block[:want]
		} else {
			r, err := zlib.NewReader(bytes.NewReader(block))
			if err != nil {
				return written, errDamaged
			}
			n, err := io.ReadFull(r, out[:want])
			if err != nil || int64(n) != want {
				return written, errDamaged
			}
			data = out[:want]
		}
		n, err := w.Write(data)
		written += int64(n)
		if err != nil {
			return written, err
		}
	}
	if written != g.dataSize {
		return written, errDamaged
	}
	return written, nil
}

// ---- CSO: compressed PSP images (version 1, deflate) ----

type cso struct {
	f         *os.File
	size      int64
	blockSize int64
	align     uint
	index     []uint32
}

func openCSO(path string) (Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	h := make([]byte, 24)
	if _, err := io.ReadFull(f, h); err != nil || string(h[:4]) != "CISO" {
		f.Close()
		return nil, ErrFormat
	}
	c := &cso{f: f, size: int64(binary.LittleEndian.Uint64(h[8:])), blockSize: int64(binary.LittleEndian.Uint32(h[16:])), align: uint(h[21])}
	if h[20] > 1 {
		f.Close()
		return nil, fmt.Errorf("%w: CSO version %d", ErrUnsupported, h[20])
	}
	if c.blockSize <= 0 || c.blockSize > 1<<20 || c.size <= 0 || c.align > 16 {
		f.Close()
		return nil, errDamaged
	}
	blocks := (c.size + c.blockSize - 1) / c.blockSize
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if blocks > 1<<24 || 24+4*(blocks+1) > info.Size() {
		f.Close()
		return nil, errDamaged
	}
	table := make([]byte, 4*(blocks+1))
	if _, err := io.ReadFull(f, table); err != nil {
		f.Close()
		return nil, errDamaged
	}
	c.index = make([]uint32, blocks+1)
	for i := range c.index {
		c.index[i] = binary.LittleEndian.Uint32(table[4*i:])
	}
	return c, nil
}

func (c *cso) Size() int64  { return c.size }
func (c *cso) Close() error { return c.f.Close() }

func (c *cso) WriteTo(w io.Writer) (int64, error) {
	var written int64
	out := make([]byte, c.blockSize)
	for i := 0; i+1 < len(c.index); i++ {
		plain := c.index[i]&0x80000000 != 0
		start := int64(c.index[i]&0x7FFFFFFF) << c.align
		end := int64(c.index[i+1]&0x7FFFFFFF) << c.align
		if end < start || end-start > 2*c.blockSize+(1<<c.align) {
			return written, errDamaged
		}
		block := make([]byte, end-start)
		if _, err := c.f.ReadAt(block, start); err != nil && err != io.EOF {
			return written, errDamaged
		}
		want := min(c.blockSize, c.size-int64(i)*c.blockSize)
		var data []byte
		if plain {
			if int64(len(block)) < want {
				return written, errDamaged
			}
			data = block[:want]
		} else {
			r := flate.NewReader(bytes.NewReader(block))
			n, err := io.ReadFull(r, out[:want])
			if err != nil || int64(n) != want {
				return written, errDamaged
			}
			data = out[:want]
		}
		n, err := w.Write(data)
		written += int64(n)
		if err != nil {
			return written, err
		}
	}
	if written != c.size {
		return written, errDamaged
	}
	return written, nil
}

// ---- the disc header of GameCube and Wii images ----

// Header is what the first bytes of a GameCube or Wii disc say.
type Header struct {
	// game code, such as GALE: the game, the region
	Game    string
	Maker   string
	Disc    int
	Version int
}

// Key is how a disc is matched with the DAT: game code, disc and version.
func (h Header) Key() string {
	return fmt.Sprintf("%s:%d:%d", h.Game, h.Disc, h.Version)
}

// ReadHeader reads the disc header of an RVZ, WIA or WBFS image, or of an ISO.
func ReadHeader(path string) (Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	defer f.Close()
	start := make([]byte, 16)
	if _, err := io.ReadFull(f, start); err != nil {
		return Header{}, ErrFormat
	}
	offset := int64(0)
	switch {
	case string(start[:4]) == "RVZ\x01" || string(start[:4]) == "WIA\x01":
		// the file header, then the disc struct: four fields and a copy of the disc header
		offset = 0x48 + 16
	case string(start[:4]) == "WBFS":
		// the disc header follows the first sector of the drive
		offset = int64(1) << start[8]
	}
	h := make([]byte, 0x20)
	if _, err := f.ReadAt(h, offset); err != nil {
		return Header{}, ErrFormat
	}
	return parseHeader(h)
}

func parseHeader(h []byte) (Header, error) {
	for _, c := range h[:6] {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return Header{}, ErrFormat
		}
	}
	// the magic words of GameCube and Wii discs
	gc := binary.BigEndian.Uint32(h[0x1C:]) == 0xC2339F3D
	wii := binary.BigEndian.Uint32(h[0x18:]) == 0x5D1C9EA3
	if !gc && !wii {
		return Header{}, ErrFormat
	}
	return Header{Game: string(h[:4]), Maker: string(h[4:6]), Disc: int(h[6]), Version: int(h[7])}, nil
}

// IdentifiedByHeader reports whether a file is an image that can only be identified by its
// header: its content differs from the original disc.
func IdentifiedByHeader(path string) bool {
	switch strings.ToLower(path[strings.LastIndexByte(path, '.')+1:]) {
	case "rvz", "wia", "wbfs":
		return true
	}
	return false
}
