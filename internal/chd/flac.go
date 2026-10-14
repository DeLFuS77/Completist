package chd

import "errors"

// A FLAC frame decoder, enough for the audio hunks of a CHD: 16-bit stereo frames at 44.1 kHz
// with no stream header in front, as chdman writes them. Written from the FLAC format
// specification.

var errFLAC = errors.New("chd: damaged FLAC data")

// bitReader reads bits from the most significant end; past the end it reads zeros.
type bitReader struct {
	data []byte
	pos  int // in bits
}

func (b *bitReader) bit() uint32 {
	i := b.pos >> 3
	b.pos++
	if i >= len(b.data) {
		return 0
	}
	return uint32(b.data[i]>>(7-uint(b.pos-1)&7)) & 1
}

func (b *bitReader) bits(n int) uint32 {
	var v uint32
	for n > 0 {
		i := b.pos >> 3
		if i >= len(b.data) {
			v <<= uint(n)
			b.pos += n
			return v
		}
		used := b.pos & 7
		avail := 8 - used
		take := min(avail, n)
		chunk := uint32(b.data[i]>>(uint(avail-take))) & (1<<uint(take) - 1)
		v = v<<uint(take) | chunk
		b.pos += take
		n -= take
	}
	return v
}

func (b *bitReader) signed(n int) int32 {
	if n == 0 {
		return 0
	}
	v := b.bits(n)
	return int32(v<<(32-uint(n))) >> (32 - uint(n))
}

func (b *bitReader) unary() uint32 {
	var n uint32
	for b.bit() == 0 {
		n++
		if b.pos>>3 > len(b.data) {
			return n
		}
	}
	return n
}

func (b *bitReader) align() {
	b.pos = (b.pos + 7) &^ 7
}

func (b *bitReader) overflow() bool {
	return b.pos > len(b.data)*8
}

// flacFrames decodes frames until out holds samples interleaved samples per channel, for two
// channels of 16 bits, and returns how many bytes of data they took.
func flacFrames(data []byte, samples int, blockSize int, out []int16) (int, error) {
	br := &bitReader{data: data}
	done := 0
	channels := [2][]int32{make([]int32, blockSize), make([]int32, blockSize)}
	for done < samples {
		n, err := flacFrame(br, &channels, blockSize)
		if err != nil {
			return 0, err
		}
		if done+n > samples {
			return 0, errFLAC
		}
		for i := 0; i < n; i++ {
			out[(done+i)*2] = int16(channels[0][i])
			out[(done+i)*2+1] = int16(channels[1][i])
		}
		done += n
	}
	return br.pos >> 3, nil
}

func flacFrame(br *bitReader, channels *[2][]int32, maxBlock int) (int, error) {
	br.align()
	if br.bits(14) != 0x3FFE {
		return 0, errFLAC
	}
	br.bits(1) // reserved
	br.bits(1) // blocking strategy
	bsCode := br.bits(4)
	rateCode := br.bits(4)
	assignment := br.bits(4)
	sizeCode := br.bits(3)
	br.bits(1)
	// the frame or sample number, coded like UTF-8
	first := br.bits(8)
	extra := 0
	for mask := uint32(0x80); first&mask != 0 && mask > 1; mask >>= 1 {
		extra++
	}
	if extra == 1 || extra > 7 {
		return 0, errFLAC
	}
	if extra > 1 {
		br.bits(8 * (extra - 1))
	}
	var blockSize int
	switch {
	case bsCode == 1:
		blockSize = 192
	case bsCode >= 2 && bsCode <= 5:
		blockSize = 576 << (bsCode - 2)
	case bsCode == 6:
		blockSize = int(br.bits(8)) + 1
	case bsCode == 7:
		blockSize = int(br.bits(16)) + 1
	case bsCode >= 8:
		blockSize = 256 << (bsCode - 8)
	default:
		return 0, errFLAC
	}
	switch rateCode {
	case 12:
		br.bits(8)
	case 13, 14:
		br.bits(16)
	case 15:
		return 0, errFLAC
	}
	bps := 16
	switch sizeCode {
	case 0, 4:
	case 1:
		bps = 8
	case 2:
		bps = 12
	case 5:
		bps = 20
	case 6:
		bps = 24
	default:
		return 0, errFLAC
	}
	br.bits(8) // CRC-8 of the header
	if blockSize > maxBlock || blockSize > len(channels[0]) {
		return 0, errFLAC
	}
	if assignment > 10 || (assignment < 8 && assignment != 1) {
		// only stereo is stored in a CHD
		return 0, errFLAC
	}
	for ch := 0; ch < 2; ch++ {
		chBps := bps
		if (assignment == 8 && ch == 1) || (assignment == 9 && ch == 0) || (assignment == 10 && ch == 1) {
			chBps++ // the side channel has one bit more
		}
		if err := flacSubframe(br, channels[ch][:blockSize], chBps); err != nil {
			return 0, err
		}
	}
	left, right := channels[0][:blockSize], channels[1][:blockSize]
	switch assignment {
	case 8: // left, side
		for i := range right {
			right[i] = left[i] - right[i]
		}
	case 9: // side, right
		for i := range left {
			left[i] += right[i]
		}
	case 10: // mid, side
		for i := range left {
			mid := left[i]<<1 | right[i]&1
			side := right[i]
			left[i] = (mid + side) >> 1
			right[i] = (mid - side) >> 1
		}
	}
	br.align()
	br.bits(16) // CRC-16 of the frame
	if br.overflow() {
		return 0, errFLAC
	}
	return blockSize, nil
}

var fixedCoefficients = [5][]int64{{}, {1}, {2, -1}, {3, -3, 1}, {4, -6, 4, -1}}

func flacSubframe(br *bitReader, out []int32, bps int) error {
	if br.bit() != 0 {
		return errFLAC
	}
	kind := br.bits(6)
	wasted := 0
	if br.bit() == 1 {
		wasted = int(br.unary()) + 1
		bps -= wasted
	}
	if bps <= 0 || bps > 32 {
		return errFLAC
	}
	switch {
	case kind == 0: // constant
		v := br.signed(bps)
		for i := range out {
			out[i] = v
		}
	case kind == 1: // verbatim
		for i := range out {
			out[i] = br.signed(bps)
		}
	case kind >= 8 && kind <= 12: // fixed prediction
		order := int(kind - 8)
		if order > len(out) {
			return errFLAC
		}
		for i := 0; i < order; i++ {
			out[i] = br.signed(bps)
		}
		if err := flacResidual(br, out, order); err != nil {
			return err
		}
		coef := fixedCoefficients[order]
		for i := order; i < len(out); i++ {
			var sum int64
			for j, c := range coef {
				sum += c * int64(out[i-1-j])
			}
			out[i] += int32(sum)
		}
	case kind >= 32: // linear prediction
		order := int(kind-32) + 1
		if order > len(out) {
			return errFLAC
		}
		for i := 0; i < order; i++ {
			out[i] = br.signed(bps)
		}
		precision := int(br.bits(4)) + 1
		if precision == 16 {
			return errFLAC
		}
		shift := br.signed(5)
		if shift < 0 {
			return errFLAC
		}
		coef := make([]int64, order)
		for i := range coef {
			coef[i] = int64(br.signed(precision))
		}
		if err := flacResidual(br, out, order); err != nil {
			return err
		}
		for i := order; i < len(out); i++ {
			var sum int64
			for j, c := range coef {
				sum += c * int64(out[i-1-j])
			}
			out[i] += int32(sum >> uint(shift))
		}
	default:
		return errFLAC
	}
	if wasted > 0 {
		for i := range out {
			out[i] <<= uint(wasted)
		}
	}
	return nil
}

// flacResidual reads the residual into out[order:].
func flacResidual(br *bitReader, out []int32, order int) error {
	method := br.bits(2)
	if method > 1 {
		return errFLAC
	}
	paramBits, escape := 4, uint32(15)
	if method == 1 {
		paramBits, escape = 5, 31
	}
	partitionOrder := br.bits(4)
	partitions := 1 << partitionOrder
	perPartition := len(out) >> partitionOrder
	if perPartition<<partitionOrder != len(out) || perPartition < order {
		return errFLAC
	}
	i := order
	for p := 0; p < partitions; p++ {
		n := perPartition
		if p == 0 {
			n -= order
		}
		param := br.bits(paramBits)
		if param == escape {
			raw := int(br.bits(5))
			for k := 0; k < n; k++ {
				out[i] = br.signed(raw)
				i++
			}
			continue
		}
		for k := 0; k < n; k++ {
			q := br.unary()
			u := q<<param | br.bits(int(param))
			out[i] = int32(u>>1) ^ -int32(u&1)
			i++
		}
		if br.overflow() {
			return errFLAC
		}
	}
	return nil
}
