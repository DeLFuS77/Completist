package chd

import "errors"

// A raw LZMA decoder, enough for the hunks of a CHD: one stream per hunk, no header, no end
// marker, decoded until the hunk is full. Written from the LZMA specification by Igor Pavlov
// (LzmaSpec.cpp, public domain).

var errLZMA = errors.New("chd: damaged LZMA data")

const (
	lzmaBitModelTotal = 1 << 11
	lzmaMoveBits      = 5
	lzmaTopValue      = 1 << 24
)

type rangeDecoder struct {
	in     []byte
	pos    int
	rng    uint32
	code   uint32
	broken bool
}

func (rd *rangeDecoder) next() byte {
	if rd.pos >= len(rd.in) {
		rd.broken = true
		return 0
	}
	b := rd.in[rd.pos]
	rd.pos++
	return b
}

func (rd *rangeDecoder) init(in []byte) {
	rd.in, rd.pos, rd.rng, rd.code, rd.broken = in, 0, 0xFFFFFFFF, 0, false
	if rd.next() != 0 {
		rd.broken = true
	}
	for i := 0; i < 4; i++ {
		rd.code = rd.code<<8 | uint32(rd.next())
	}
	if rd.code == rd.rng {
		rd.broken = true
	}
}

func (rd *rangeDecoder) normalize() {
	if rd.rng < lzmaTopValue {
		rd.rng <<= 8
		rd.code = rd.code<<8 | uint32(rd.next())
	}
}

func (rd *rangeDecoder) directBits(n int) uint32 {
	var res uint32
	for ; n > 0; n-- {
		rd.rng >>= 1
		rd.code -= rd.rng
		t := 0 - (rd.code >> 31)
		rd.code += rd.rng & t
		if rd.code == rd.rng {
			rd.broken = true
		}
		rd.normalize()
		res = res<<1 + t + 1
	}
	return res
}

func (rd *rangeDecoder) bit(p *uint16) uint32 {
	v := uint32(*p)
	bound := (rd.rng >> 11) * v
	var symbol uint32
	if rd.code < bound {
		v += (lzmaBitModelTotal - v) >> lzmaMoveBits
		rd.rng = bound
	} else {
		v -= v >> lzmaMoveBits
		rd.code -= bound
		rd.rng -= bound
		symbol = 1
	}
	*p = uint16(v)
	rd.normalize()
	return symbol
}

func initProbs(p []uint16) {
	for i := range p {
		p[i] = lzmaBitModelTotal / 2
	}
}

func (rd *rangeDecoder) tree(probs []uint16, bits int) uint32 {
	m := uint32(1)
	for i := 0; i < bits; i++ {
		m = m<<1 + rd.bit(&probs[m])
	}
	return m - 1<<bits
}

func (rd *rangeDecoder) reverseTree(probs []uint16, bits int) uint32 {
	m := uint32(1)
	var symbol uint32
	for i := 0; i < bits; i++ {
		b := rd.bit(&probs[m])
		m = m<<1 + b
		symbol |= b << i
	}
	return symbol
}

type lenDecoder struct {
	choice, choice2 uint16
	low, mid        [16][1 << 3]uint16
	high            [1 << 8]uint16
}

func (l *lenDecoder) init() {
	l.choice, l.choice2 = lzmaBitModelTotal/2, lzmaBitModelTotal/2
	initProbs(l.high[:])
	for i := range l.low {
		initProbs(l.low[i][:])
		initProbs(l.mid[i][:])
	}
}

func (l *lenDecoder) decode(rd *rangeDecoder, posState uint32) uint32 {
	if rd.bit(&l.choice) == 0 {
		return rd.tree(l.low[posState][:], 3)
	}
	if rd.bit(&l.choice2) == 0 {
		return 8 + rd.tree(l.mid[posState][:], 3)
	}
	return 16 + rd.tree(l.high[:], 8)
}

// lzmaDecoder keeps its probability tables between hunks, so they are allocated once.
type lzmaDecoder struct {
	lc, lp, pb uint
	literals   []uint16
	posSlot    [4][1 << 6]uint16
	posDecs    [1 + 128 - 14]uint16
	align      [1 << 4]uint16
	isMatch    [12 << 4]uint16
	isRep      [12]uint16
	isRepG0    [12]uint16
	isRepG1    [12]uint16
	isRepG2    [12]uint16
	isRep0Long [12 << 4]uint16
	length     lenDecoder
	repLength  lenDecoder
	rd         rangeDecoder
}

// newLZMA makes a decoder with the properties CHD uses: lc 3, lp 0, pb 2.
func newLZMA() *lzmaDecoder {
	d := &lzmaDecoder{lc: 3, lp: 0, pb: 2}
	d.literals = make([]uint16, 0x300<<(d.lc+d.lp))
	return d
}

func (d *lzmaDecoder) reset() {
	initProbs(d.literals)
	for i := range d.posSlot {
		initProbs(d.posSlot[i][:])
	}
	initProbs(d.posDecs[:])
	initProbs(d.align[:])
	initProbs(d.isMatch[:])
	initProbs(d.isRep[:])
	initProbs(d.isRepG0[:])
	initProbs(d.isRepG1[:])
	initProbs(d.isRepG2[:])
	initProbs(d.isRep0Long[:])
	d.length.init()
	d.repLength.init()
}

// decode fills out from in.
func (d *lzmaDecoder) decode(in, out []byte) error {
	d.reset()
	rd := &d.rd
	rd.init(in)
	var rep0, rep1, rep2, rep3 uint32
	state := uint32(0)
	pos := 0
	size := len(out)
	pbMask := uint32(1)<<d.pb - 1
	lpMask := uint32(1)<<d.lp - 1
	for pos < size {
		if rd.broken {
			return errLZMA
		}
		posState := uint32(pos) & pbMask
		if rd.bit(&d.isMatch[state<<4+posState]) == 0 {
			// a literal
			prev := uint32(0)
			if pos > 0 {
				prev = uint32(out[pos-1])
			}
			litState := (uint32(pos)&lpMask)<<d.lc + prev>>(8-d.lc)
			probs := d.literals[0x300*litState : 0x300*(litState+1)]
			symbol := uint32(1)
			if state >= 7 {
				if int(rep0) >= pos {
					return errLZMA
				}
				matchByte := uint32(out[pos-int(rep0)-1])
				for symbol < 0x100 {
					matchBit := (matchByte >> 7) & 1
					matchByte <<= 1
					b := rd.bit(&probs[(1+matchBit)<<8+symbol])
					symbol = symbol<<1 | b
					if matchBit != b {
						break
					}
				}
			}
			for symbol < 0x100 {
				symbol = symbol<<1 | rd.bit(&probs[symbol])
			}
			out[pos] = byte(symbol)
			pos++
			switch {
			case state < 4:
				state = 0
			case state < 10:
				state -= 3
			default:
				state -= 6
			}
			continue
		}
		var length uint32
		if rd.bit(&d.isRep[state]) != 0 {
			if pos == 0 {
				return errLZMA
			}
			if rd.bit(&d.isRepG0[state]) == 0 {
				if rd.bit(&d.isRep0Long[state<<4+posState]) == 0 {
					// a short rep: one byte
					if state < 7 {
						state = 9
					} else {
						state = 11
					}
					if int(rep0) >= pos {
						return errLZMA
					}
					out[pos] = out[pos-int(rep0)-1]
					pos++
					continue
				}
			} else {
				var dist uint32
				if rd.bit(&d.isRepG1[state]) == 0 {
					dist = rep1
				} else {
					if rd.bit(&d.isRepG2[state]) == 0 {
						dist = rep2
					} else {
						dist = rep3
						rep3 = rep2
					}
					rep2 = rep1
				}
				rep1 = rep0
				rep0 = dist
			}
			length = d.repLength.decode(rd, posState)
			if state < 7 {
				state = 8
			} else {
				state = 11
			}
		} else {
			rep3, rep2, rep1 = rep2, rep1, rep0
			length = d.length.decode(rd, posState)
			if state < 7 {
				state = 7
			} else {
				state = 10
			}
			rep0 = d.distance(length)
			if rep0 == 0xFFFFFFFF {
				// an end marker: the hunk must be full by now
				break
			}
		}
		length += 2
		if int(rep0) >= pos {
			return errLZMA
		}
		n := int(length)
		if n > size-pos {
			return errLZMA
		}
		src := pos - int(rep0) - 1
		for i := 0; i < n; i++ {
			out[pos+i] = out[src+i]
		}
		pos += n
	}
	if pos != size || rd.broken {
		return errLZMA
	}
	return nil
}

func (d *lzmaDecoder) distance(length uint32) uint32 {
	rd := &d.rd
	lenState := min(length, 3)
	posSlot := rd.tree(d.posSlot[lenState][:], 6)
	if posSlot < 4 {
		return posSlot
	}
	numDirect := int(posSlot>>1) - 1
	dist := (2 | posSlot&1) << numDirect
	if posSlot < 14 {
		// the probabilities of this slot start at dist - posSlot; the tree indexes from 1
		dist += rd.reverseTree(d.posDecs[dist-posSlot:], numDirect)
	} else {
		dist += rd.directBits(numDirect-4) << 4
		dist += rd.reverseTree(d.align[:], 4)
	}
	return dist
}
