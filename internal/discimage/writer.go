package discimage

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/adler32"
	"io"
	"os"
	"runtime"
)

// Progress is told how many bytes of the ISO were compressed, out of all of them.
type Progress func(done, total int64)

const (
	// GCZBlockSize is the block size of the GCZ images made here, one Dolphin uses as well.
	GCZBlockSize = 32 << 10
	// CSOBlockSize is the sector size of a UMD, the block size every PSP tool uses.
	CSOBlockSize = 2048
)

// a batch of blocks compressed by one worker
const batchBytes = 1 << 20

type block struct {
	data []byte
	raw  bool
}

type compressor interface {
	Reset(w io.Writer)
	Write(p []byte) (int, error)
	Close() error
}

// compressBlocks reads the ISO block by block, compresses the blocks on every core and hands
// them over in order. A block that does not get smaller by at least minGain bytes is kept as
// it is. With fullBlocks, a short last block is filled with zeros.
func compressBlocks(iso *os.File, size, blockSize int64, minGain int, fullBlocks bool, newCompressor func() compressor, emit func(block) error, progress Progress) error {
	perBatch := max(1, batchBytes/blockSize)
	blocks := (size + blockSize - 1) / blockSize
	type job struct {
		first int64
		out   chan []block
	}
	jobs := make(chan job)
	order := make(chan chan []block, runtime.NumCPU()*2)
	stop := make(chan struct{})
	errOnce := make(chan error, 1)

	workers := runtime.NumCPU()
	for range workers {
		go func() {
			c := newCompressor()
			var buf bytes.Buffer
			in := make([]byte, perBatch*blockSize)
			for j := range jobs {
				start := j.first * blockSize
				n := min(perBatch*blockSize, size-start)
				clear(in)
				if _, err := iso.ReadAt(in[:n], start); err != nil && !(errors.Is(err, io.EOF)) {
					select {
					case errOnce <- err:
					default:
					}
					j.out <- nil
					continue
				}
				result := make([]block, 0, perBatch)
				for off := int64(0); off < n; off += blockSize {
					end := min(off+blockSize, n)
					if fullBlocks {
						end = off + blockSize
					}
					plain := in[off:end]
					buf.Reset()
					c.Reset(&buf)
					c.Write(plain)
					c.Close()
					if buf.Len() > len(plain)-minGain {
						result = append(result, block{data: append([]byte(nil), plain...), raw: true})
					} else {
						result = append(result, block{data: append([]byte(nil), buf.Bytes()...)})
					}
				}
				j.out <- result
			}
		}()
	}
	go func() {
		defer close(jobs)
		defer close(order)
		for first := int64(0); first < blocks; first += perBatch {
			// the job is handed out before its result is waited for, so a result that is
			// waited for always comes
			out := make(chan []block, 1)
			select {
			case jobs <- job{first: first, out: out}:
			case <-stop:
				return
			}
			select {
			case order <- out:
			case <-stop:
				return
			}
		}
	}()
	defer func() {
		close(stop)
		// let the workers finish what they hold
		for out := range order {
			<-out
		}
	}()
	var done int64
	for out := range order {
		result := <-out
		if result == nil {
			select {
			case err := <-errOnce:
				return err
			default:
				return errors.New("the ISO could not be read")
			}
		}
		for _, b := range result {
			if err := emit(b); err != nil {
				return err
			}
		}
		done = min(size, done+int64(len(result))*blockSize)
		if progress != nil {
			progress(done, size)
		}
	}
	return nil
}

type zlibCompressor struct{ *zlib.Writer }

func (z zlibCompressor) Reset(w io.Writer) { z.Writer.Reset(w) }

type flateCompressor struct{ *flate.Writer }

func (f flateCompressor) Reset(w io.Writer) { f.Writer.Reset(w) }

func openISO(path string, blockAlign int64) (*os.File, int64, error) {
	iso, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	info, err := iso.Stat()
	if err != nil {
		iso.Close()
		return nil, 0, err
	}
	if info.Size() == 0 || info.Size()%blockAlign != 0 {
		iso.Close()
		return nil, 0, errors.New("the ISO is not made of whole sectors")
	}
	return iso, info.Size(), nil
}

// WriteGCZ makes a GCZ image of a GameCube or Wii ISO, the compressed format Dolphin reads
// with no loss: every block is deflated, and kept as it is when that does not make it smaller.
func WriteGCZ(out, isoPath string, wii bool, progress Progress) (err error) {
	iso, size, err := openISO(isoPath, 2048)
	if err != nil {
		return err
	}
	defer iso.Close()
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	blocks := (size + GCZBlockSize - 1) / GCZBlockSize
	pointers := make([]byte, 8*blocks)
	hashes := make([]byte, 4*blocks)
	dataStart := 32 + 12*blocks
	var position int64
	i := 0
	// Dolphin wants every block whole, and keeps a block as it is unless deflating it saves
	// at least 10 bytes
	err = compressBlocks(iso, size, GCZBlockSize, 10, true, func() compressor {
		w, _ := zlib.NewWriterLevel(io.Discard, zlib.BestCompression)
		return zlibCompressor{w}
	}, func(b block) error {
		p := uint64(position)
		if b.raw {
			p |= 1 << 63
		}
		binary.LittleEndian.PutUint64(pointers[8*i:], p)
		binary.LittleEndian.PutUint32(hashes[4*i:], adler32.Checksum(b.data))
		if _, err := f.WriteAt(b.data, dataStart+position); err != nil {
			return err
		}
		position += int64(len(b.data))
		i++
		return nil
	}, progress)
	if err != nil {
		return err
	}
	header := make([]byte, 32)
	binary.LittleEndian.PutUint32(header, 0xB10BC001)
	if wii {
		binary.LittleEndian.PutUint32(header[4:], 1)
	}
	binary.LittleEndian.PutUint64(header[8:], uint64(position))
	binary.LittleEndian.PutUint64(header[16:], uint64(size))
	binary.LittleEndian.PutUint32(header[24:], GCZBlockSize)
	binary.LittleEndian.PutUint32(header[28:], uint32(blocks))
	if _, err := f.WriteAt(append(append(header, pointers...), hashes...), 0); err != nil {
		return err
	}
	return nil
}

// WriteCSO makes a CSO image (version 1, deflate) of a PSP ISO, as PPSSPP and the PSP itself
// read it, with no loss.
func WriteCSO(out, isoPath string, progress Progress) (err error) {
	iso, size, err := openISO(isoPath, CSOBlockSize)
	if err != nil {
		return err
	}
	defer iso.Close()
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	blocks := size / CSOBlockSize
	start := 24 + 4*(blocks+1)
	// positions are kept in 31 bits, shifted by align: big images need it
	align := uint(0)
	for (start+blocks*(CSOBlockSize+int64(1)<<align))>>align >= 1<<31 {
		align++
	}
	unit := int64(1) << align
	pad := func(p int64) int64 { return (p + unit - 1) / unit * unit }
	index := make([]byte, 4*(blocks+1))
	position := pad(start)
	i := 0
	err = compressBlocks(iso, size, CSOBlockSize, 1, false, func() compressor {
		w, _ := flate.NewWriter(io.Discard, flate.BestCompression)
		return flateCompressor{w}
	}, func(b block) error {
		v := uint32(position >> align)
		if b.raw {
			v |= 0x80000000
		}
		binary.LittleEndian.PutUint32(index[4*i:], v)
		if _, err := f.WriteAt(b.data, position); err != nil {
			return err
		}
		position = pad(position + int64(len(b.data)))
		i++
		return nil
	}, progress)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(index[4*i:], uint32(position>>align))
	header := make([]byte, 24)
	copy(header, "CISO")
	binary.LittleEndian.PutUint32(header[4:], 24)
	binary.LittleEndian.PutUint64(header[8:], uint64(size))
	binary.LittleEndian.PutUint32(header[16:], CSOBlockSize)
	header[20] = 1
	header[21] = byte(align)
	if _, err := f.WriteAt(append(header, index...), 0); err != nil {
		return err
	}
	// the last block may end in padding that was never written
	return f.Truncate(position)
}

// ImageSize reads the size of the ISO a GCZ or CSO image holds from its header alone.
func ImageSize(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	h := make([]byte, 24)
	if _, err := io.ReadFull(f, h); err != nil {
		return 0, ErrFormat
	}
	switch {
	case binary.LittleEndian.Uint32(h) == 0xB10BC001:
		return int64(binary.LittleEndian.Uint64(h[16:])), nil
	case string(h[:4]) == "CISO":
		return int64(binary.LittleEndian.Uint64(h[8:])), nil
	}
	return 0, ErrFormat
}
