package chd

import (
	"bytes"
	"compress/flate"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"sync"
)

// Writing CHD files, version 5, as chdman does: CD images with the cdzl codec (deflate, with
// the sync and ECC of data sectors left out where they can be rebuilt) and DVD images with
// zlib. Repeated hunks are stored once. Every program that reads CHD reads these.

// TrackSource is a track of a CD read from a BIN file.
type TrackSource struct {
	Number int
	// AUDIO, MODE1_RAW, MODE2_RAW, MODE1 or MODE2
	Type string
	File string
	// where the track starts in the file, and how many frames it has there, its pregap included
	Offset int64
	Frames int
	// frames of the pregap, in the file before INDEX 01
	Pregap int
}

func (t TrackSource) dataSize() int {
	size, _ := dataSize(t.Type)
	return size
}

// Progress is told how many bytes of the source were written.
type Progress func(done, total int64)

const (
	cdHunkFrames = 8
	dvdHunkBytes = 4096
	metaChecksum = 0x01
)

// WriteCD writes the tracks as a CD image.
func WriteCD(out string, tracks []TrackSource, progress Progress) error {
	if len(tracks) == 0 {
		return errors.New("no tracks")
	}
	meta := [][2]string{}
	var total int64
	frames := int64(0)
	for _, t := range tracks {
		if t.dataSize() == 0 {
			return fmt.Errorf("track %d: type %s not supported", t.Number, t.Type)
		}
		pgtype := "MODE1"
		if t.Pregap > 0 {
			pgtype = "V" + t.Type
		}
		meta = append(meta, [2]string{"CHT2", fmt.Sprintf("TRACK:%d TYPE:%s SUBTYPE:NONE FRAMES:%d PREGAP:%d PGTYPE:%s PGSUB:NONE POSTGAP:0",
			t.Number, t.Type, t.Frames, t.Pregap, pgtype)})
		frames += int64((t.Frames + trackPadding - 1) / trackPadding * trackPadding)
		total += int64(t.Frames) * int64(t.dataSize())
	}
	src := &cdFrames{tracks: tracks, total: total, progress: progress}
	defer src.close()
	return write(out, writeParams{
		codec: codecCDZL, hunkBytes: cdHunkFrames * frameSize, unitBytes: frameSize,
		logical: frames * frameSize, meta: meta, read: src.read, compress: compressCD,
	})
}

// WriteDVD writes an ISO file as a DVD image.
func WriteDVD(out string, iso string, progress Progress) error {
	f, err := os.Open(iso)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size()%2048 != 0 || info.Size() == 0 {
		return errors.New("not an image of 2048-byte sectors")
	}
	var done int64
	read := func(buf []byte) error {
		n, err := io.ReadFull(f, buf)
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			clear(buf[n:])
			err = nil
		}
		done += int64(n)
		if progress != nil {
			progress(done, info.Size())
		}
		return err
	}
	return write(out, writeParams{
		codec: codecZlib, hunkBytes: dvdHunkBytes, unitBytes: 2048, logical: info.Size(),
		meta: [][2]string{{"DVD ", ""}}, read: read, compress: compressZlib,
	})
}

// cdFrames reads the frames of the tracks one hunk at a time.
type cdFrames struct {
	tracks   []TrackSource
	track    int
	frame    int // in the track, padding included
	file     *os.File
	done     int64
	total    int64
	progress Progress
}

func (s *cdFrames) close() {
	if s.file != nil {
		s.file.Close()
	}
}

func (s *cdFrames) read(buf []byte) error {
	clear(buf)
	for at := 0; at+frameSize <= len(buf); at += frameSize {
		for s.track < len(s.tracks) {
			t := s.tracks[s.track]
			padded := (t.Frames + trackPadding - 1) / trackPadding * trackPadding
			if s.frame < padded {
				break
			}
			s.track++
			s.frame = 0
			if s.file != nil {
				s.file.Close()
				s.file = nil
			}
		}
		if s.track >= len(s.tracks) {
			return nil
		}
		t := s.tracks[s.track]
		if s.frame < t.Frames {
			if s.file == nil {
				f, err := os.Open(t.File)
				if err != nil {
					return err
				}
				if _, err := f.Seek(t.Offset, io.SeekStart); err != nil {
					f.Close()
					return err
				}
				s.file = f
			}
			size := t.dataSize()
			sector := buf[at : at+size]
			if _, err := io.ReadFull(s.file, sector); err != nil {
				return fmt.Errorf("track %d: %w", t.Number, err)
			}
			if t.Type == "AUDIO" {
				// stored big-endian, as chdman does
				for i := 0; i+1 < size; i += 2 {
					sector[i], sector[i+1] = sector[i+1], sector[i]
				}
			}
			s.done += int64(size)
			if s.progress != nil && s.frame%256 == 0 {
				s.progress(s.done, s.total)
			}
		}
		s.frame++
	}
	return nil
}

type writeParams struct {
	codec     uint32
	hunkBytes int
	unitBytes int
	logical   int64
	meta      [][2]string
	read      func([]byte) error
	compress  func(hunk []byte, w *flate.Writer, out *bytes.Buffer) []byte
}

type hunkOut struct {
	kind   uint8
	data   []byte
	crc    uint16
	self   uint64
	length uint32
}

func write(out string, p writeParams) (err error) {
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(out)
		}
	}()
	w := &countingWriter{w: f}

	// the header, written again at the end with the offsets and the checksums
	if _, err := w.Write(make([]byte, headerSize)); err != nil {
		return err
	}
	metaOffset := uint64(w.n)
	metaHashes := [][24]byte{}
	for i, m := range p.meta {
		data := append([]byte(m[1]), 0)
		next := uint64(0)
		if i+1 < len(p.meta) {
			next = uint64(w.n) + 16 + uint64(len(data))
		}
		head := make([]byte, 16)
		copy(head, m[0])
		binary.BigEndian.PutUint32(head[4:], metaChecksum<<24|uint32(len(data)))
		binary.BigEndian.PutUint64(head[8:], next)
		if _, err := w.Write(append(head, data...)); err != nil {
			return err
		}
		var h [24]byte
		copy(h[:], m[0])
		sum := sha1.Sum(data)
		copy(h[4:], sum[:])
		metaHashes = append(metaHashes, h)
	}

	hunks := int((p.logical + int64(p.hunkBytes) - 1) / int64(p.hunkBytes))
	entries := make([]hunkOut, 0, hunks)
	firstOffset := uint64(w.n)
	raw := sha1.New()
	seen := map[[20]byte]uint64{}
	workers := min(4, runtime.NumCPU())
	batch := workers * 8
	remaining := p.logical
	for start := 0; start < hunks; start += batch {
		n := min(batch, hunks-start)
		bufs := make([][]byte, n)
		for i := range bufs {
			bufs[i] = make([]byte, p.hunkBytes)
			if err := p.read(bufs[i]); err != nil {
				return err
			}
			part := bufs[i][:min(int64(p.hunkBytes), remaining)]
			raw.Write(part)
			remaining -= int64(len(part))
		}
		results := make([]hunkOut, n)
		var wg sync.WaitGroup
		next := make(chan int)
		for k := 0; k < workers; k++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				fw, _ := flate.NewWriter(nil, flate.DefaultCompression)
				var out bytes.Buffer
				for i := range next {
					hunk := bufs[i]
					r := hunkOut{crc: crc16(hunk)}
					if c := p.compress(hunk, fw, &out); c != nil && len(c) < p.hunkBytes {
						r.kind, r.data = compType0, c
					} else {
						r.kind, r.data = compNone, hunk
					}
					results[i] = r
				}
			}()
		}
		for i := 0; i < n; i++ {
			next <- i
		}
		close(next)
		wg.Wait()
		for i, r := range results {
			sum := sha1.Sum(bufs[i])
			if ref, ok := seen[sum]; ok {
				entries = append(entries, hunkOut{kind: compSelf, self: ref})
				continue
			}
			seen[sum] = uint64(start + i)
			r.length = uint32(len(r.data))
			if _, err := w.Write(r.data); err != nil {
				return err
			}
			r.data = nil
			entries = append(entries, r)
		}
	}

	mapOffset := uint64(w.n)
	mapData, err := encodeMap(entries, firstOffset, uint32(p.hunkBytes))
	if err != nil {
		return err
	}
	if _, err := w.Write(mapData); err != nil {
		return err
	}

	// the checksums: of the data, and of the data with the metadata
	var rawSum [20]byte
	copy(rawSum[:], raw.Sum(nil))
	sort.Slice(metaHashes, func(i, j int) bool { return bytes.Compare(metaHashes[i][:], metaHashes[j][:]) < 0 })
	overall := sha1.New()
	overall.Write(rawSum[:])
	for _, h := range metaHashes {
		overall.Write(h[:])
	}
	h := make([]byte, headerSize)
	copy(h, "MComprHD")
	binary.BigEndian.PutUint32(h[8:], headerSize)
	binary.BigEndian.PutUint32(h[12:], 5)
	binary.BigEndian.PutUint32(h[16:], p.codec)
	binary.BigEndian.PutUint64(h[32:], uint64(p.logical))
	binary.BigEndian.PutUint64(h[40:], mapOffset)
	binary.BigEndian.PutUint64(h[48:], metaOffset)
	binary.BigEndian.PutUint32(h[56:], uint32(p.hunkBytes))
	binary.BigEndian.PutUint32(h[60:], uint32(p.unitBytes))
	copy(h[64:], rawSum[:])
	copy(h[84:], overall.Sum(nil))
	if _, err := f.WriteAt(h, 0); err != nil {
		return err
	}
	return f.Sync()
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func deflate(data []byte, w *flate.Writer, out *bytes.Buffer) []byte {
	out.Reset()
	w.Reset(out)
	w.Write(data)
	w.Close()
	return append([]byte(nil), out.Bytes()...)
}

func compressZlib(hunk []byte, w *flate.Writer, out *bytes.Buffer) []byte {
	return deflate(hunk, w, out)
}

// compressCD writes a hunk of CD frames as cdzl: which sectors lost their sync and ECC, the
// length of the sectors compressed, the sectors, then the subcode.
func compressCD(hunk []byte, w *flate.Writer, out *bytes.Buffer) []byte {
	frames := len(hunk) / frameSize
	eccBytes := (frames + 7) / 8
	sectors := make([]byte, frames*sectorSize)
	subcode := make([]byte, frames*subcodeSize)
	flags := make([]byte, eccBytes)
	check := make([]byte, sectorSize)
	for f := 0; f < frames; f++ {
		sector := sectors[f*sectorSize : (f+1)*sectorSize]
		copy(sector, hunk[f*frameSize:f*frameSize+sectorSize])
		copy(subcode[f*subcodeSize:], hunk[f*frameSize+sectorSize:(f+1)*frameSize])
		if bytes.Equal(sector[:12], syncHeader[:]) && (sector[15] == 1 || sector[15] == 2) {
			copy(check, sector)
			eccGenerate(check)
			if bytes.Equal(check[2076:], sector[2076:]) {
				// rebuilt when read: left out here, so the hunk compresses better
				flags[f/8] |= 1 << (f % 8)
				clear(sector[:12])
				clear(sector[2076:])
			}
		}
	}
	base := deflate(sectors, w, out)
	sub := deflate(subcode, w, out)
	if len(hunk) >= 65536 || len(base) >= 1<<16 {
		return nil
	}
	result := make([]byte, 0, eccBytes+2+len(base)+len(sub))
	result = append(result, flags...)
	result = append(result, byte(len(base)>>8), byte(len(base)))
	result = append(result, base...)
	return append(result, sub...)
}

// ---- the map ----

type bitWriter struct {
	buf   []byte
	cur   byte
	nbits uint
}

func (b *bitWriter) write(v uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		b.cur = b.cur<<1 | byte(v>>uint(i)&1)
		b.nbits++
		if b.nbits == 8 {
			b.buf = append(b.buf, b.cur)
			b.cur, b.nbits = 0, 0
		}
	}
}

func (b *bitWriter) bytes() []byte {
	if b.nbits > 0 {
		return append(b.buf, b.cur<<(8-b.nbits))
	}
	return b.buf
}

func bitsFor(v uint64) int {
	n := 0
	for v > 0 {
		n++
		v >>= 1
	}
	return n
}

// encodeMap writes the compressed map: a Huffman tree where every kind has a 4-bit code, the
// kind of every hunk, then its length and CRC, or the hunk it copies.
func encodeMap(entries []hunkOut, firstOffset uint64, hunkBytes uint32) ([]byte, error) {
	var maxLength, maxSelf uint64
	for _, e := range entries {
		switch e.kind {
		case compType0:
			maxLength = max(maxLength, uint64(e.length))
		case compSelf:
			maxSelf = max(maxSelf, e.self)
		}
	}
	lengthBits, selfBits := bitsFor(maxLength), bitsFor(maxSelf)
	bw := &bitWriter{}
	for i := 0; i < 16; i++ {
		bw.write(4, 4)
	}
	for _, e := range entries {
		bw.write(uint64(e.kind), 4)
	}
	raw := make([]byte, 12*len(entries))
	current := firstOffset
	for i, e := range entries {
		r := raw[12*i:]
		r[0] = e.kind
		var length uint32
		var offset uint64
		var crc uint16
		switch e.kind {
		case compType0:
			bw.write(uint64(e.length), lengthBits)
			bw.write(uint64(e.crc), 16)
			length, offset, crc = e.length, current, e.crc
			current += uint64(e.length)
		case compNone:
			bw.write(uint64(e.crc), 16)
			length, offset, crc = hunkBytes, current, e.crc
			current += uint64(hunkBytes)
		case compSelf:
			bw.write(e.self, selfBits)
			offset = e.self
		}
		r[1], r[2], r[3] = byte(length>>16), byte(length>>8), byte(length)
		r[4], r[5] = byte(offset>>40), byte(offset>>32)
		binary.BigEndian.PutUint32(r[6:], uint32(offset))
		binary.BigEndian.PutUint16(r[10:], crc)
	}
	data := bw.bytes()
	head := make([]byte, 16)
	binary.BigEndian.PutUint32(head[0:], uint32(len(data)))
	binary.BigEndian.PutUint16(head[4:], uint16(firstOffset>>32))
	binary.BigEndian.PutUint32(head[6:], uint32(firstOffset))
	binary.BigEndian.PutUint16(head[10:], crc16(raw))
	head[12], head[13], head[14] = byte(lengthBits), byte(selfBits), 0
	return append(head, data...), nil
}
