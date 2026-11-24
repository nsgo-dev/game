package ktx2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math/bits"
)

// Pure-Go Khronos ASTC 4x4 LDR encoder (void-extent + single-partition CEM 8/12)
// and full ASTC 4x4 LDR decoder (void-extent, all 2D block modes, BISE trits/quints/bits,
// 1..4 partitions, dual-plane weights, bilinear weight grid infill, and all LDR CEMs).

// ASTC BISE quantization range indices (Khronos ASTC Specification Table C.2.15).
const (
	astcQuant2   = 0
	astcQuant3   = 1
	astcQuant4   = 2
	astcQuant5   = 3
	astcQuant6   = 4
	astcQuant8   = 5
	astcQuant10  = 6
	astcQuant12  = 7
	astcQuant16  = 8
	astcQuant20  = 9
	astcQuant24  = 10
	astcQuant32  = 11
	astcQuant40  = 12
	astcQuant48  = 13
	astcQuant64  = 14
	astcQuant80  = 15
	astcQuant96  = 16
	astcQuant128 = 17
	astcQuant160 = 18
	astcQuant192 = 19
	astcQuant256 = 20
)

type astcQuantInfo struct {
	bits   uint8
	trits  uint8
	quints uint8
}

var astcQuantTable = [21]astcQuantInfo{
	astcQuant2:   {bits: 1},
	astcQuant3:   {trits: 1},
	astcQuant4:   {bits: 2},
	astcQuant5:   {quints: 1},
	astcQuant6:   {bits: 1, trits: 1},
	astcQuant8:   {bits: 3},
	astcQuant10:  {bits: 1, quints: 1},
	astcQuant12:  {bits: 2, trits: 1},
	astcQuant16:  {bits: 4},
	astcQuant20:  {bits: 2, quints: 1},
	astcQuant24:  {bits: 3, trits: 1},
	astcQuant32:  {bits: 5},
	astcQuant40:  {bits: 3, quints: 1},
	astcQuant48:  {bits: 4, trits: 1},
	astcQuant64:  {bits: 6},
	astcQuant80:  {bits: 4, quints: 1},
	astcQuant96:  {bits: 5, trits: 1},
	astcQuant128: {bits: 7},
	astcQuant160: {bits: 5, quints: 1},
	astcQuant192: {bits: 6, trits: 1},
	astcQuant256: {bits: 8},
}

// astcWeightUnquant maps scrambled BISE weight indices directly to unquantized [0..64] weights
// for weight quantization modes 0..11 (QUANT_2 through QUANT_32), per Khronos ASTC Spec §C.2.17.
var astcWeightUnquant = [12][]uint8{
	astcQuant2:  {0, 64},
	astcQuant3:  {0, 32, 64},
	astcQuant4:  {0, 21, 43, 64},
	astcQuant5:  {0, 16, 32, 48, 64},
	astcQuant6:  {0, 64, 12, 52, 25, 39},
	astcQuant8:  {0, 9, 18, 27, 37, 46, 55, 64},
	astcQuant10: {0, 64, 7, 57, 14, 50, 21, 43, 28, 36},
	astcQuant12: {0, 64, 17, 47, 5, 59, 23, 41, 11, 53, 28, 36},
	astcQuant16: {0, 4, 8, 12, 17, 21, 25, 29, 35, 39, 43, 47, 52, 56, 60, 64},
	astcQuant20: {0, 64, 16, 48, 3, 61, 19, 45, 6, 58, 23, 41, 9, 55, 26, 38, 13, 51, 29, 35},
	astcQuant24: {0, 64, 8, 56, 16, 48, 24, 40, 2, 62, 11, 53, 19, 45, 27, 37, 5, 59, 13, 51, 22, 42, 30, 34},
	astcQuant32: {0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28, 30, 34, 36, 38, 40, 42, 44, 46, 48, 50, 52, 54, 56, 58, 60, 62, 64},
}

func astcISEBitCount(count, quant int) int {
	if quant < 0 || quant >= len(astcQuantTable) {
		return 1024
	}
	q := astcQuantTable[quant]
	b := count * int(q.bits)
	if q.trits != 0 {
		b += (count*8 + 4) / 5
	} else if q.quints != 0 {
		b += (count*7 + 2) / 3
	}
	return b
}

// astcUnpackTrits decodes an 8-bit ASTC BISE trit block T into five trits t[0..4] (Khronos ASTC Spec §C.2.12).
func astcUnpackTrits(t uint32) [5]uint8 {
	var out [5]uint8
	var c uint32
	if (t>>2)&7 == 7 {
		c = ((t >> 5) & 7) << 2
		c |= t & 3
		out[4] = 2
		out[3] = 2
	} else {
		c = t & 0x1F
		if (t>>5)&3 == 3 {
			out[4] = 2
			out[3] = uint8((t >> 7) & 1)
		} else {
			out[4] = uint8((t >> 7) & 1)
			out[3] = uint8((t >> 5) & 3)
		}
	}
	if c&3 == 3 {
		out[2] = 2
		out[1] = uint8((c >> 4) & 1)
		out[0] = uint8((c >> 2) & 3)
		if out[0] == 3 {
			out[0] = 2
		}
	} else if (c>>2)&3 == 3 {
		out[2] = 2
		out[1] = 2
		out[0] = uint8(c & 3)
	} else {
		out[2] = uint8((c >> 4) & 1)
		out[1] = uint8((c >> 2) & 3)
		out[0] = uint8(c & 3)
		if out[0] == 3 {
			out[0] = 2
		}
	}
	return out
}

// astcUnpackQuints decodes a 7-bit ASTC BISE quint block Q into three quints q[0..2] (Khronos ASTC Spec §C.2.12).
func astcUnpackQuints(q uint32) [3]uint8 {
	var out [3]uint8
	if (q>>1)&3 == 3 && (q>>5)&3 == 0 {
		out[0] = 4
		out[1] = 4
		if q&1 != 0 {
			out[2] = 4
		} else {
			out[2] = uint8((q >> 3) & 3)
		}
		return out
	}
	var c uint32
	if (q>>1)&3 == 3 {
		out[2] = 4
		c = (((q >> 3) & 3) << 3) | ((^(q >> 5) & 3) << 1) | (q & 1)
	} else {
		out[2] = uint8((q >> 5) & 3)
		c = q & 0x1F
	}
	if c&7 == 5 {
		out[1] = 4
		out[0] = uint8((c >> 3) & 3)
	} else if c&7 == 6 {
		out[1] = 4
		out[0] = 4
		out[2] = uint8((c >> 3) & 3)
	} else if c&7 == 7 {
		out[1] = 4
		out[0] = 4
		out[2] = 4
	} else {
		out[0] = uint8(c & 7)
		if out[0] > 4 {
			out[0] = 4
		}
		out[1] = uint8((c >> 3) & 3)
	}
	return out
}

// astcDecodeISE decodes count BISE-encoded integers from src starting at bitOffset.
func astcDecodeISE(quant, count int, src []byte, bitOffset int, dst []uint8) {
	q := astcQuantTable[quant]
	nbits := int(q.bits)
	pos := bitOffset
	read := func(n int) uint32 {
		if n == 0 {
			return 0
		}
		v := readBitsLE128(src, pos, n)
		pos += n
		return v
	}

	if q.trits != 0 {
		tbits := [5]int{2, 2, 1, 2, 1}
		tshift := [5]uint{0, 2, 4, 5, 7}
		for i := 0; i < count; i += 5 {
			n := min(5, count-i)
			var m [5]uint8
			var t uint32
			for j := range n {
				m[j] = uint8(read(nbits))
				t |= read(tbits[j]) << tshift[j]
			}
			trits := astcUnpackTrits(t)
			for j := range n {
				dst[i+j] = (trits[j] << nbits) | m[j]
			}
		}
		return
	}

	if q.quints != 0 {
		qbits := [3]int{3, 2, 2}
		qshift := [3]uint{0, 3, 5}
		for i := 0; i < count; i += 3 {
			n := min(3, count-i)
			var m [3]uint8
			var qv uint32
			for j := range n {
				m[j] = uint8(read(nbits))
				qv |= read(qbits[j]) << qshift[j]
			}
			quints := astcUnpackQuints(qv)
			for j := range n {
				dst[i+j] = (quints[j] << nbits) | m[j]
			}
		}
		return
	}

	for i := range count {
		dst[i] = uint8(read(nbits))
	}
}

// astcUnquantColor converts a scrambled BISE color endpoint integer v to [0..255]
// for quant in [astcQuant6..astcQuant256] (Khronos ASTC Spec §C.2.13).
func astcUnquantColor(v uint8, quant int) uint8 {
	q := astcQuantTable[quant]
	nbits := q.bits
	if q.trits == 0 && q.quints == 0 {
		switch nbits {
		case 8:
			return v
		case 7:
			return (v << 1) | (v >> 6)
		case 6:
			return (v << 2) | (v >> 4)
		case 5:
			return (v << 3) | (v >> 2)
		case 4:
			return (v << 4) | v
		case 3:
			return (v << 5) | (v << 2) | (v >> 1)
		default:
			return replicateTo8(uint32(v), nbits)
		}
	}

	hi := uint32(v) >> nbits
	lo := uint32(v) & ((1 << nbits) - 1)
	var a uint32
	if lo&1 != 0 {
		a = 0x1FF
	}

	var b, c uint32
	if q.trits != 0 {
		switch nbits {
		case 1:
			c = 204
		case 2:
			c = 93
			b1 := (lo >> 1) & 1
			b = (b1 << 8) | (b1 << 4) | (b1 << 2) | (b1 << 1)
		case 3:
			c = 44
			b2, b1 := (lo>>2)&1, (lo>>1)&1
			b = (b2 << 8) | (b1 << 7) | (b2 << 3) | (b1 << 2) | (b2 << 1) | b1
		case 4:
			c = 22
			m := lo >> 1
			b = (m << 6) | m
		case 5:
			c = 11
			m := lo >> 1
			b = (m << 5) | (m >> 2)
		case 6:
			c = 5
			m := lo >> 1
			b = (m << 4) | (m >> 4)
		}
	} else {
		switch nbits {
		case 1:
			c = 113
		case 2:
			c = 54
			b1 := (lo >> 1) & 1
			b = (b1 << 8) | (b1 << 3) | (b1 << 2)
		case 3:
			c = 26
			b2, b1 := (lo>>2)&1, (lo>>1)&1
			b = (b2 << 8) | (b1 << 7) | (b2 << 2) | (b1 << 1) | b2
		case 4:
			c = 13
			m := lo >> 1
			b = (m << 6) | (m >> 1)
		case 5:
			c = 6
			m := lo >> 1
			b = (m << 5) | (m >> 3)
		}
	}

	t := (hi*c + b) ^ a
	t = (a & 0x80) | (t >> 2)
	return uint8(t)
}

// astcDecodeBlockMode2D parses an 11-bit 2D ASTC block mode for a 4x4 footprint.
func astcDecodeBlockMode2D(bm uint32) (wx, wy int, dualPlane bool, weightQuant, weightBits int, ok bool) {
	baseQuant := (bm >> 4) & 1
	h := (bm >> 9) & 1
	d := (bm >> 10) & 1
	a := (bm >> 5) & 3

	if bm&3 != 0 {
		baseQuant |= (bm & 3) << 1
		b := (bm >> 7) & 3
		switch (bm >> 2) & 3 {
		case 0:
			wx = int(b) + 4
			wy = int(a) + 2
		case 1:
			wx = int(b) + 8
			wy = int(a) + 2
		case 2:
			wx = int(a) + 2
			wy = int(b) + 8
		case 3:
			b &= 1
			if bm&0x100 != 0 {
				wx = int(b) + 2
				wy = int(a) + 2
			} else {
				wx = int(a) + 2
				wy = int(b) + 6
			}
		}
	} else {
		if (bm>>2)&3 == 0 {
			return 0, 0, false, 0, 0, false
		}
		baseQuant |= ((bm >> 2) & 3) << 1
		b := (bm >> 9) & 3
		switch (bm >> 7) & 3 {
		case 0:
			wx = 12
			wy = int(a) + 2
		case 1:
			wx = int(a) + 2
			wy = 12
		case 2:
			wx = int(a) + 6
			wy = int(b) + 6
			d = 0
			h = 0
		case 3:
			switch (bm >> 5) & 3 {
			case 0:
				wx, wy = 6, 10
			case 1:
				wx, wy = 10, 6
			default:
				return 0, 0, false, 0, 0, false
			}
		}
	}

	if baseQuant < 2 || wx > 4 || wy > 4 {
		return 0, 0, false, 0, 0, false
	}
	weightQuant = int(baseQuant-2) + 6*int(h)
	if weightQuant < 0 || weightQuant >= 12 {
		return 0, 0, false, 0, 0, false
	}
	dualPlane = d != 0
	weightCount := wx * wy
	if dualPlane {
		weightCount *= 2
	}
	weightBits = astcISEBitCount(weightCount, weightQuant)
	if weightCount > 64 || weightBits < 24 || weightBits > 96 {
		return 0, 0, false, 0, 0, false
	}
	return wx, wy, dualPlane, weightQuant, weightBits, true
}

func astcHash52(inp uint32) uint32 {
	inp ^= inp >> 15
	inp *= 0xEEDE0891
	inp ^= inp >> 5
	inp += inp << 16
	inp ^= inp >> 7
	inp ^= inp >> 3
	inp ^= inp << 6
	inp ^= inp >> 17
	return inp
}

// astcSelectPartition4x4 computes the partition index (0..partCount-1) for texel (x, y) in a 4x4 block.
func astcSelectPartition4x4(seed, x, y, partCount int) int {
	// 4x4 has 16 < 32 texels, so small_block scaling doubles coordinates per Khronos ASTC Spec §C.2.21.
	x <<= 1
	y <<= 1
	seed += (partCount - 1) * 1024
	rnum := astcHash52(uint32(seed))

	seed1 := uint8(rnum & 0xF)
	seed2 := uint8((rnum >> 4) & 0xF)
	seed3 := uint8((rnum >> 8) & 0xF)
	seed4 := uint8((rnum >> 12) & 0xF)
	seed5 := uint8((rnum >> 16) & 0xF)
	seed6 := uint8((rnum >> 20) & 0xF)
	seed7 := uint8((rnum >> 24) & 0xF)
	seed8 := uint8((rnum >> 28) & 0xF)

	seed1 *= seed1
	seed2 *= seed2
	seed3 *= seed3
	seed4 *= seed4
	seed5 *= seed5
	seed6 *= seed6
	seed7 *= seed7
	seed8 *= seed8

	var sh1, sh2 uint
	if seed&1 != 0 {
		if seed&2 != 0 {
			sh1 = 4
		} else {
			sh1 = 5
		}
		if partCount == 3 {
			sh2 = 6
		} else {
			sh2 = 5
		}
	} else {
		if partCount == 3 {
			sh1 = 6
		} else {
			sh1 = 5
		}
		if seed&2 != 0 {
			sh2 = 4
		} else {
			sh2 = 5
		}
	}

	seed1 >>= sh1
	seed2 >>= sh2
	seed3 >>= sh1
	seed4 >>= sh2
	seed5 >>= sh1
	seed6 >>= sh2
	seed7 >>= sh1
	seed8 >>= sh2

	a := (int(seed1)*x + int(seed2)*y + int(rnum>>14)) & 0x3F
	b := (int(seed3)*x + int(seed4)*y + int(rnum>>10)) & 0x3F
	c := (int(seed5)*x + int(seed6)*y + int(rnum>>6)) & 0x3F
	d := (int(seed7)*x + int(seed8)*y + int(rnum>>2)) & 0x3F

	if partCount <= 3 {
		d = 0
	}
	if partCount <= 2 {
		c = 0
	}
	if partCount <= 1 {
		b = 0
	}

	if a >= b && a >= c && a >= d {
		return 0
	}
	if b >= c && b >= d {
		return 1
	}
	if c >= d {
		return 2
	}
	return 3
}

func astcBitTransferSigned(v1, v0 int) (int, int) {
	out0 := (v0 >> 1) | (v1 & 0x80)
	out1 := int(int32(v1<<25) >> 26)
	return out1, out0
}

func astcUncontractColor(c [4]int) [4]int {
	return [4]int{
		(c[0] + c[2]) >> 1,
		(c[1] + c[2]) >> 1,
		c[2],
		c[3],
	}
}

func astcClampRGBA(c [4]int) [4]uint8 {
	return [4]uint8{
		clampByte(c[0]),
		clampByte(c[1]),
		clampByte(c[2]),
		clampByte(c[3]),
	}
}

func astcUnpackColorEndpointsLDR(cem int, v []uint8) (e0, e1 [4]uint8, err error) {
	switch cem {
	case 0: // LDR Luminance Direct
		return [4]uint8{v[0], v[0], v[0], 255}, [4]uint8{v[1], v[1], v[1], 255}, nil
	case 1: // LDR Luminance Base+Offset
		l0 := (int(v[0]) >> 2) | (int(v[1]) & 0xC0)
		l1 := min(l0+(int(v[1])&0x3F), 255)
		return [4]uint8{uint8(l0), uint8(l0), uint8(l0), 255}, [4]uint8{uint8(l1), uint8(l1), uint8(l1), 255}, nil
	case 4: // LDR Luminance+Alpha Direct
		return [4]uint8{v[0], v[0], v[0], v[2]}, [4]uint8{v[1], v[1], v[1], v[3]}, nil
	case 5: // LDR Luminance+Alpha Base+Offset
		dl, l0 := astcBitTransferSigned(int(v[1]), int(v[0]))
		da, a0 := astcBitTransferSigned(int(v[3]), int(v[2]))
		l1 := clampByte(l0 + dl)
		a1 := clampByte(a0 + da)
		u0 := clampByte(l0)
		ua0 := clampByte(a0)
		return [4]uint8{u0, u0, u0, ua0}, [4]uint8{l1, l1, l1, a1}, nil
	case 6: // LDR RGB Base+Scale
		s := int(v[3])
		return [4]uint8{uint8((int(v[0]) * s) >> 8), uint8((int(v[1]) * s) >> 8), uint8((int(v[2]) * s) >> 8), 255},
			[4]uint8{v[0], v[1], v[2], 255}, nil
	case 8: // LDR RGB Direct
		c0 := [4]int{int(v[0]), int(v[2]), int(v[4]), 255}
		c1 := [4]int{int(v[1]), int(v[3]), int(v[5]), 255}
		if c0[0]+c0[1]+c0[2] > c1[0]+c1[1]+c1[2] {
			c0 = astcUncontractColor(c0)
			c1 = astcUncontractColor(c1)
			c0, c1 = c1, c0
		}
		return astcClampRGBA(c0), astcClampRGBA(c1), nil
	case 9: // LDR RGB Base+Offset
		dr, r0 := astcBitTransferSigned(int(v[1]), int(v[0]))
		dg, g0 := astcBitTransferSigned(int(v[3]), int(v[2]))
		db, b0 := astcBitTransferSigned(int(v[5]), int(v[4]))
		c0 := [4]int{r0, g0, b0, 255}
		c1 := [4]int{r0 + dr, g0 + dg, b0 + db, 255}
		if dr+dg+db < 0 {
			c0 = astcUncontractColor(c0)
			c1 = astcUncontractColor(c1)
			c0, c1 = c1, c0
		}
		c0[3], c1[3] = 255, 255
		return astcClampRGBA(c0), astcClampRGBA(c1), nil
	case 10: // LDR RGB Base+Scale + two A
		s := int(v[3])
		return [4]uint8{uint8((int(v[0]) * s) >> 8), uint8((int(v[1]) * s) >> 8), uint8((int(v[2]) * s) >> 8), v[4]},
			[4]uint8{v[0], v[1], v[2], v[5]}, nil
	case 12: // LDR RGBA Direct
		c0 := [4]int{int(v[0]), int(v[2]), int(v[4]), int(v[6])}
		c1 := [4]int{int(v[1]), int(v[3]), int(v[5]), int(v[7])}
		if c0[0]+c0[1]+c0[2] > c1[0]+c1[1]+c1[2] {
			c0 = astcUncontractColor(c0)
			c1 = astcUncontractColor(c1)
			c0, c1 = c1, c0
		}
		return astcClampRGBA(c0), astcClampRGBA(c1), nil
	case 13: // LDR RGBA Base+Offset
		dr, r0 := astcBitTransferSigned(int(v[1]), int(v[0]))
		dg, g0 := astcBitTransferSigned(int(v[3]), int(v[2]))
		db, b0 := astcBitTransferSigned(int(v[5]), int(v[4]))
		da, a0 := astcBitTransferSigned(int(v[7]), int(v[6]))
		c0 := [4]int{r0, g0, b0, a0}
		c1 := [4]int{r0 + dr, g0 + dg, b0 + db, a0 + da}
		if dr+dg+db < 0 {
			c0 = astcUncontractColor(c0)
			c1 = astcUncontractColor(c1)
			c0, c1 = c1, c0
		}
		return astcClampRGBA(c0), astcClampRGBA(c1), nil
	default:
		return [4]uint8{}, [4]uint8{}, fmt.Errorf("HDR color endpoint mode %d is not supported in LDR ASTC", cem)
	}
}

func astcInfillWeights4x4(grid []uint8, wx, wy int, dst *[16]uint32) {
	if wx == 4 && wy == 4 {
		for i := range 16 {
			dst[i] = uint32(grid[i])
		}
		return
	}
	// For 4x4 footprint: Ds = Dt = floor((1024 + 4/2) / (4 - 1)) = 342.
	const ds = 342
	for y := range 4 {
		ct := (ds*y*(wy-1) + 32) >> 6
		jt := ct >> 4
		ft := ct & 0xF
		for x := range 4 {
			cs := (ds*x*(wx-1) + 32) >> 6
			js := cs >> 4
			fs := cs & 0xF

			w11 := (fs*ft + 8) >> 4
			w10 := fs - w11
			w01 := ft - w11
			w00 := 16 - fs - ft + w11

			idx := jt*wx + js
			sum := 8
			if w00 != 0 {
				sum += int(grid[idx]) * w00
			}
			if w10 != 0 {
				sum += int(grid[idx+1]) * w10
			}
			if w01 != 0 {
				sum += int(grid[idx+wx]) * w01
			}
			if w11 != 0 {
				sum += int(grid[idx+wx+1]) * w11
			}
			dst[y*4+x] = uint32(sum >> 4)
		}
	}
}

// decodeASTC4x4 decodes an ASTC 4x4 LDR image into *image.NRGBA.
func decodeASTC4x4(raw []byte, width, height int, srgb bool) (*image.NRGBA, error) {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	bw := (width + 3) / 4
	bh := (height + 3) / 4
	var blockPix [16][4]uint8
	for by := range bh {
		for bx := range bw {
			off := (by*bw + bx) * 16
			if off+16 > len(raw) {
				break
			}
			if err := decodeASTC4x4Block(raw[off:off+16], srgb, &blockPix); err != nil {
				return nil, fmt.Errorf("ktx2: ASTC block (%d, %d): %w", bx, by, err)
			}
			for dy := range 4 {
				py := by*4 + dy
				if py >= height {
					continue
				}
				rowOff := py * dst.Stride
				for dx := range 4 {
					px := bx*4 + dx
					if px >= width {
						continue
					}
					c := blockPix[dy*4+dx]
					di := rowOff + px*4
					dst.Pix[di+0] = c[0]
					dst.Pix[di+1] = c[1]
					dst.Pix[di+2] = c[2]
					dst.Pix[di+3] = c[3]
				}
			}
		}
	}
	return dst, nil
}

// decodeASTC4x4Block decodes any valid 16-byte Khronos ASTC 4x4 LDR block.
func decodeASTC4x4Block(block []byte, srgb bool, out *[16][4]uint8) error {
	if len(block) < 16 {
		return errors.New("truncated ASTC block")
	}

	bm := readBitsLE128(block, 0, 11)
	// Void-extent block (Khronos ASTC Spec §C.2.23).
	if bm&0x1FF == 0x1FC {
		if bm&0x200 != 0 {
			return errors.New("HDR void-extent block is not supported in LDR ASTC")
		}
		if readBitsLE128(block, 10, 2) != 3 {
			return errors.New("invalid 2D void-extent reserved bits")
		}
		lowS := readBitsLE128(block, 12, 13)
		highS := readBitsLE128(block, 25, 13)
		lowT := readBitsLE128(block, 38, 13)
		highT := readBitsLE128(block, 51, 13)
		allOnes := lowS == 0x1FFF && highS == 0x1FFF && lowT == 0x1FFF && highT == 0x1FFF
		if !allOnes && (lowS >= highS || lowT >= highT) {
			return errors.New("invalid 2D void-extent coordinate range")
		}
		r := uint8(binary.LittleEndian.Uint16(block[8:10]) >> 8)
		g := uint8(binary.LittleEndian.Uint16(block[10:12]) >> 8)
		b := uint8(binary.LittleEndian.Uint16(block[12:14]) >> 8)
		a := uint8(binary.LittleEndian.Uint16(block[14:16]) >> 8)
		for i := range 16 {
			out[i] = [4]uint8{r, g, b, a}
		}
		return nil
	}

	wx, wy, dualPlane, weightQuant, weightBits, ok := astcDecodeBlockMode2D(bm)
	if !ok {
		return fmt.Errorf("invalid 4x4 block mode %#x", bm)
	}

	partCount := int(readBitsLE128(block, 11, 2)) + 1
	if dualPlane && partCount == 4 {
		return errors.New("dual-plane mode is illegal with 4 partitions")
	}

	belowWeightsPos := 128 - weightBits

	var cems [4]int
	var partSeed int
	var colorStartBit int
	if partCount == 1 {
		cems[0] = int(readBitsLE128(block, 13, 4))
		colorStartBit = 17
	} else {
		partSeed = int(readBitsLE128(block, 13, 10))
		cemLo := readBitsLE128(block, 23, 6)
		baseClass := cemLo & 3
		if baseClass == 0 {
			shared := int((cemLo >> 2) & 0xF)
			for p := range partCount {
				cems[p] = shared
			}
		} else {
			extraBits := 3*partCount - 4
			belowWeightsPos -= extraBits
			if belowWeightsPos < 29 {
				return errors.New("ASTC block config overflows weight region")
			}
			cemHi := readBitsLE128(block, belowWeightsPos, extraBits)
			enc := cemLo | (cemHi << 6)
			base := int(baseClass - 1)
			bitPos := uint(2)
			for p := range partCount {
				cems[p] = (int((enc>>bitPos)&1) + base) << 2
				bitPos++
			}
			for p := range partCount {
				cems[p] |= int((enc >> bitPos) & 3)
				bitPos += 2
			}
		}
		colorStartBit = 29
	}

	ccs := -1
	if dualPlane {
		belowWeightsPos -= 2
		if belowWeightsPos < colorStartBit {
			return errors.New("ASTC dual-plane CCS overflows color region")
		}
		ccs = int(readBitsLE128(block, belowWeightsPos, 2))
	}

	colorIntCount := 0
	for p := range partCount {
		colorIntCount += ((cems[p] >> 2) + 1) * 2
	}
	if colorIntCount > 18 {
		return fmt.Errorf("ASTC color endpoint integer count %d exceeds 18", colorIntCount)
	}

	colorBits := belowWeightsPos - colorStartBit
	if colorBits < 0 {
		return errors.New("ASTC color endpoint bit budget negative")
	}
	colorQuant := -1
	for q := astcQuant256; q >= astcQuant6; q-- {
		if astcISEBitCount(colorIntCount, q) <= colorBits {
			colorQuant = q
			break
		}
	}
	if colorQuant < astcQuant6 {
		return fmt.Errorf("ASTC color endpoint bit budget %d too small for %d integers", colorBits, colorIntCount)
	}

	var rawColorVals [18]uint8
	astcDecodeISE(colorQuant, colorIntCount, block, colorStartBit, rawColorVals[:colorIntCount])
	for i := range colorIntCount {
		rawColorVals[i] = astcUnquantColor(rawColorVals[i], colorQuant)
	}

	var ep0, ep1 [4][4]uint8
	cOff := 0
	for p := range partCount {
		n := ((cems[p] >> 2) + 1) * 2
		var err error
		ep0[p], ep1[p], err = astcUnpackColorEndpointsLDR(cems[p], rawColorVals[cOff:cOff+n])
		if err != nil {
			return err
		}
		cOff += n
	}

	// Decode bit-reversed weight BISE stream from bit 127 downwards.
	var bswapped [16]byte
	for i := range 16 {
		bswapped[i] = bits.Reverse8(block[15-i])
	}
	gridCount := wx * wy
	realWeightCount := gridCount
	if dualPlane {
		realWeightCount *= 2
	}
	var rawWeights [64]uint8
	astcDecodeISE(weightQuant, realWeightCount, bswapped[:], 0, rawWeights[:realWeightCount])

	uqTable := astcWeightUnquant[weightQuant]
	var grid0, grid1 [16]uint8
	if dualPlane {
		for i := range gridCount {
			a, b := rawWeights[2*i], rawWeights[2*i+1]
			if int(a) >= len(uqTable) || int(b) >= len(uqTable) {
				return errors.New("ASTC weight index out of range")
			}
			grid0[i] = uqTable[a]
			grid1[i] = uqTable[b]
		}
	} else {
		for i := range gridCount {
			a := rawWeights[i]
			if int(a) >= len(uqTable) {
				return errors.New("ASTC weight index out of range")
			}
			grid0[i] = uqTable[a]
		}
	}

	var plane0, plane1 [16]uint32
	astcInfillWeights4x4(grid0[:gridCount], wx, wy, &plane0)
	if dualPlane {
		astcInfillWeights4x4(grid1[:gridCount], wx, wy, &plane1)
	}

	for y := range 4 {
		for x := range 4 {
			i := y*4 + x
			p := 0
			if partCount > 1 {
				p = astcSelectPartition4x4(partSeed, x, y, partCount)
			}
			for ch := range 4 {
				w := plane0[i]
				if dualPlane && ch == ccs {
					w = plane1[i]
				}
				out[i][ch] = uastcInterpolate(ep0[p][ch], ep1[p][ch], w, srgb)
			}
		}
	}
	return nil
}

// --- ASTC 4x4 LDR encoder (Void-Extent + 1-partition CEM 8 / CEM 12) ---

func encodeASTC4x4(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	out := make([]byte, bw*bh*16)
	for by := range bh {
		for bx := range bw {
			pix := gatherBlock4x4(img, bx, by)
			off := (by*bw + bx) * 16
			encodeASTC4x4Block(out[off:off+16], &pix)
		}
	}
	return out
}

func writeASTCWeightBits(block []byte, bitIdx, count int, v uint32) {
	// ASTC stores weights in reverse bit order starting from bit 127 downwards.
	for i := range count {
		if (v>>i)&1 != 0 {
			bp := 127 - (bitIdx + i)
			block[bp>>3] |= 1 << (bp & 7)
		}
	}
}

func encodeASTC4x4Block(dst []byte, pix *[16][4]uint8) {
	solid := true
	opaque := true
	for i := range 16 {
		if pix[i] != pix[0] {
			solid = false
		}
		if pix[i][3] != 255 {
			opaque = false
		}
	}
	if solid {
		// Khronos ASTC 2D LDR Void-Extent block (exact lossless 8-bit RGBA).
		dst[0] = 0xFC
		dst[1] = 0xFD
		dst[2], dst[3], dst[4], dst[5], dst[6], dst[7] = 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF
		r, g, b, a := pix[0][0], pix[0][1], pix[0][2], pix[0][3]
		binary.LittleEndian.PutUint16(dst[8:10], uint16(r)<<8|uint16(r))
		binary.LittleEndian.PutUint16(dst[10:12], uint16(g)<<8|uint16(g))
		binary.LittleEndian.PutUint16(dst[12:14], uint16(b)<<8|uint16(b))
		binary.LittleEndian.PutUint16(dst[14:16], uint16(a)<<8|uint16(a))
		return
	}

	clear(dst[:16])
	if opaque {
		// 1-partition, CEM 8 (LDR RGB direct), 4x4 weight grid with 3-bit weights (block mode 0x053).
		lo, hi := fitBlockEndpoints4D(pix, false)
		weights := uastcWeights3[:]
		indices, bestErr := matchWeightsUASTC(pix, lo, hi, weights, false, false)
		for range 2 {
			nlo, nhi, ok := refineEndpointsLinear(pix, weights, &indices, false)
			if !ok {
				break
			}
			nIdx, nErr := matchWeightsUASTC(pix, nlo, nhi, weights, false, false)
			if nErr < bestErr {
				lo, hi, indices, bestErr = nlo, nhi, nIdx, nErr
			} else {
				break
			}
		}
		// Keep s1 >= s0 so CEM 8 decodes directly without blue-contraction.
		if int(hi[0])+int(hi[1])+int(hi[2]) < int(lo[0])+int(lo[1])+int(lo[2]) {
			lo, hi = hi, lo
			for i := range 16 {
				indices[i] = 7 - indices[i]
			}
		}
		writeBitsLE128(dst, 0, 11, 0x053) // 4x4 grid, 3-bit weights, single plane
		writeBitsLE128(dst, 11, 2, 0)     // 1 partition
		writeBitsLE128(dst, 13, 4, 8)     // CEM 8: LDR RGB direct
		pos := 17
		for _, v := range [6]uint8{lo[0], hi[0], lo[1], hi[1], lo[2], hi[2]} {
			writeBitsLE128(dst, pos, 8, uint32(v))
			pos += 8
		}
		for i := range 16 {
			writeASTCWeightBits(dst, i*3, 3, uint32(indices[i]))
		}
		return
	}

	// 1-partition, CEM 12 (LDR RGBA direct), 4x4 weight grid with 2-bit weights (block mode 0x042).
	lo, hi := fitBlockEndpoints4D(pix, true)
	weights := uastcWeights2[:]
	indices, bestErr := matchWeightsUASTC(pix, lo, hi, weights, true, false)
	for range 2 {
		nlo, nhi, ok := refineEndpointsLinear(pix, weights, &indices, true)
		if !ok {
			break
		}
		nIdx, nErr := matchWeightsUASTC(pix, nlo, nhi, weights, true, false)
		if nErr < bestErr {
			lo, hi, indices, bestErr = nlo, nhi, nIdx, nErr
		} else {
			break
		}
	}
	if int(hi[0])+int(hi[1])+int(hi[2]) < int(lo[0])+int(lo[1])+int(lo[2]) {
		lo, hi = hi, lo
		for i := range 16 {
			indices[i] = 3 - indices[i]
		}
	}
	writeBitsLE128(dst, 0, 11, 0x042) // 4x4 grid, 2-bit weights, single plane
	writeBitsLE128(dst, 11, 2, 0)     // 1 partition
	writeBitsLE128(dst, 13, 4, 12)    // CEM 12: LDR RGBA direct
	pos := 17
	for _, v := range [8]uint8{lo[0], hi[0], lo[1], hi[1], lo[2], hi[2], lo[3], hi[3]} {
		writeBitsLE128(dst, pos, 8, uint32(v))
		pos += 8
	}
	for i := range 16 {
		writeASTCWeightBits(dst, i*2, 2, uint32(indices[i]))
	}
}
