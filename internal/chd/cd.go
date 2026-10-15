package chd

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
)

// eccRebuilt counts the sectors whose sync and ECC were rebuilt, for the tests.
var eccRebuilt atomic.Int64

// A CD in a CHD: every frame is a 2352-byte sector and 96 bytes of subcode. Tracks follow
// each other, each padded to a multiple of four frames.
const (
	sectorSize   = 2352
	subcodeSize  = 96
	frameSize    = sectorSize + subcodeSize
	trackPadding = 4
)

var syncHeader = [12]byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00}

// decompressCD decodes a hunk of CD frames: the sectors with one codec, the subcode with
// deflate, and the sync and error correction bytes chdman removed where it could rebuild them.
func (c *File) decompressCD(codec uint32, src, dst []byte) error {
	frames := len(dst) / frameSize
	lengthBytes := 2
	if len(dst) >= 65536 {
		lengthBytes = 3
	}
	eccBytes := (frames + 7) / 8
	headerBytes := eccBytes + lengthBytes
	if len(src) < headerBytes {
		return errDamaged
	}
	baseLength := int(src[eccBytes])<<8 | int(src[eccBytes+1])
	if lengthBytes > 2 {
		baseLength = baseLength<<8 | int(src[eccBytes+2])
	}
	need := frames * (sectorSize + subcodeSize)
	if cap(c.scratch) < need {
		c.scratch = make([]byte, need)
	}
	sectors := c.scratch[:frames*sectorSize]
	subcode := c.scratch[frames*sectorSize : need]

	if codec == codecCDFL {
		// audio: FLAC of 16-bit big-endian stereo samples, then the subcode
		samples := frames * sectorSize / 4
		block := samples
		for block > sectorSize {
			block /= 2
		}
		if cap(c.samples) < samples*2 {
			c.samples = make([]int16, samples*2)
		}
		out := c.samples[:samples*2]
		used, err := flacFrames(src, samples, block, out)
		if err != nil {
			return err
		}
		for i, s := range out {
			binary.BigEndian.PutUint16(sectors[2*i:], uint16(s))
		}
		if err := inflate(src[used:], subcode); err != nil {
			return err
		}
		for f := 0; f < frames; f++ {
			copy(dst[f*frameSize:], sectors[f*sectorSize:(f+1)*sectorSize])
			copy(dst[f*frameSize+sectorSize:], subcode[f*subcodeSize:(f+1)*subcodeSize])
		}
		return nil
	}

	if len(src) < headerBytes+baseLength {
		return errDamaged
	}
	base := src[headerBytes : headerBytes+baseLength]
	switch codec {
	case codecCDLZ:
		if c.lzma == nil {
			c.lzma = newLZMA()
		}
		if err := c.lzma.decode(base, sectors); err != nil {
			return err
		}
	case codecCDZL:
		if err := inflate(base, sectors); err != nil {
			return err
		}
	}
	if err := inflate(src[headerBytes+baseLength:], subcode); err != nil {
		return err
	}
	for f := 0; f < frames; f++ {
		sector := dst[f*frameSize : f*frameSize+sectorSize]
		copy(sector, sectors[f*sectorSize:(f+1)*sectorSize])
		copy(dst[f*frameSize+sectorSize:], subcode[f*subcodeSize:(f+1)*subcodeSize])
		if src[f/8]&(1<<(f%8)) != 0 {
			copy(sector, syncHeader[:])
			eccGenerate(sector)
			eccRebuilt.Add(1)
		}
	}
	return nil
}

// ---- error correction of mode 1 and mode 2 form 1 sectors (ECMA-130) ----

var eccLow, eccHigh = func() (low, high [256]byte) {
	for i := 0; i < 256; i++ {
		v := i << 1
		if i&0x80 != 0 {
			v ^= 0x11d
		}
		low[i] = byte(v)
		high[byte(i)^byte(v)] = byte(i)
	}
	return
}()

// the bytes each P and Q parity byte covers, counted from the header
var pOffsets, qOffsets = func() (p [86][24]uint16, q [52][43]uint16) {
	for i := range p {
		for k := range p[i] {
			p[i][k] = uint16(i + 86*k)
		}
	}
	for i := range q {
		start := (i>>1)*86 + i&1
		for k := range q[i] {
			q[i][k] = uint16((start + 88*k) % 2236)
		}
	}
	return
}()

func eccCompute(data []byte, mode2 bool, offsets []uint16) (byte, byte) {
	var v1, v2 byte
	for _, o := range offsets {
		b := data[o]
		if mode2 && o < 4 {
			b = 0
		}
		v1 = eccLow[v1^b]
		v2 ^= b
	}
	v1 = eccHigh[eccLow[v1]^v2]
	return v1, v2 ^ v1
}

// eccGenerate writes the P and Q parity of a sector.
func eccGenerate(sector []byte) {
	data := sector[12:]
	mode2 := sector[15] == 2
	for i := range pOffsets {
		sector[2076+i], sector[2076+86+i] = eccCompute(data, mode2, pOffsets[i][:])
	}
	for i := range qOffsets {
		sector[2248+i], sector[2248+52+i] = eccCompute(data, mode2, qOffsets[i][:])
	}
}

// ---- tracks ----

// Track is a track of a CD image.
type Track struct {
	Number int
	// AUDIO, MODE1_RAW, MODE2_RAW, MODE1, MODE2...
	Type string
	// frames of the track, with the pregap when the image holds it, as in its BIN file
	Frames int
	// bytes of each frame in its BIN file
	DataSize int
	// a track of a GD-ROM (Dreamcast)
	GDROM bool

	start int64 // first frame in the CHD
	file  *File
}

// Size is the size of the track as a BIN file.
func (t *Track) Size() int64 {
	return int64(t.Frames) * int64(t.DataSize)
}

// Audio reports whether the track is audio.
func (t *Track) Audio() bool {
	return t.Type == "AUDIO"
}

func dataSize(kind string) (int, bool) {
	switch kind {
	case "AUDIO", "MODE1_RAW", "MODE2_RAW":
		return 2352, true
	case "MODE1", "MODE2_FORM1":
		return 2048, true
	case "MODE2", "MODE2_FORM_MIX":
		return 2336, true
	case "MODE2_FORM2":
		return 2324, true
	}
	return 0, false
}

// Tracks reads the table of contents of a CD image.
func (c *File) Tracks() ([]*Track, error) {
	if c.unitBytes != frameSize {
		return nil, fmt.Errorf("not a CD image")
	}
	var entries [][]byte
	for _, tag := range []string{"CHT2", "CHTR", "CHGD"} {
		e, err := c.metadata(tag)
		if err != nil {
			return nil, err
		}
		if len(e) > 0 {
			entries = e
			break
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no track list in the CHD")
	}
	tracks := []*Track{}
	start := int64(0)
	for _, e := range entries {
		fields := map[string]string{}
		for _, part := range strings.Fields(strings.TrimRight(string(e), "\x00")) {
			if k, v, ok := strings.Cut(part, ":"); ok {
				fields[k] = v
			}
		}
		number, err1 := strconv.Atoi(fields["TRACK"])
		frames, err2 := strconv.Atoi(fields["FRAMES"])
		size, ok := dataSize(fields["TYPE"])
		if err1 != nil || err2 != nil || !ok || frames < 0 {
			return nil, fmt.Errorf("unknown track: %q", string(e))
		}
		t := &Track{Number: number, Type: fields["TYPE"], Frames: frames, DataSize: size, start: start, file: c}
		tracks = append(tracks, t)
		// GD-ROM images count the gap up to the next track in the frames; every track is padded
		// to four frames
		if _, gd := fields["PAD"]; gd {
			t.GDROM = true
			pad, _ := strconv.Atoi(fields["PAD"])
			if pad < 0 || pad > frames {
				return nil, fmt.Errorf("unknown track: %q", string(e))
			}
			t.Frames = frames - pad
		}
		start += int64((frames + trackPadding - 1) / trackPadding * trackPadding)
	}
	return tracks, nil
}

// WriteTo writes the track as its BIN file: the sector data of each frame, with the audio in
// little-endian byte order.
func (t *Track) WriteTo(w io.Writer) (written int64, err error) {
	// a damaged file must not stop the app: what the decoders did not catch ends here
	defer func() {
		if r := recover(); r != nil {
			err = errDamaged
		}
	}()
	c := t.file
	buffer := make([]byte, 0, 64*t.DataSize)
	flush := func() error {
		n, err := w.Write(buffer)
		written += int64(n)
		buffer = buffer[:0]
		return err
	}
	framesPerHunk := int64(c.hunkBytes / frameSize)
	for f := int64(0); f < int64(t.Frames); f++ {
		frame := t.start + f
		hunk, err := c.hunk(frame / framesPerHunk)
		if err != nil {
			return written, err
		}
		at := (frame % framesPerHunk) * frameSize
		sector := hunk[at : at+sectorSize]
		start := len(buffer)
		buffer = append(buffer, sector[:t.DataSize]...)
		if t.Audio() {
			b := buffer[start:]
			for i := 0; i+1 < len(b); i += 2 {
				b[i], b[i+1] = b[i+1], b[i]
			}
		}
		if len(buffer)+t.DataSize > cap(buffer) {
			if err := flush(); err != nil {
				return written, err
			}
		}
	}
	err = flush()
	return written, err
}

// The entries a CHD is listed under in a library: the image of a DVD, or a track of a CD.
const ImageEntry = "Image.iso"

func TrackEntry(n int) string {
	return fmt.Sprintf("Track %d.bin", n)
}
