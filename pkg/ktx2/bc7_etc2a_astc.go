package ktx2

import (
	"encoding/binary"
	"image"
	"math"
)

// BC7 4-bit interpolation weight table (Khronos Data Format Spec §19 / DirectX BC7).
var bc7Weights4 = [16]int{0, 4, 9, 13, 17, 21, 26, 30, 34, 38, 43, 47, 51, 55, 60, 64}

// EAC 8-bit alpha modifier table (Khronos Data Format Spec Table C.10 / OpenGL ES 3.0).
var eacModifierTable = [16][8]int{
	{-3, -6, -9, -15, 2, 5, 8, 14},
	{-3, -7, -10, -13, 2, 6, 9, 12},
	{-2, -5, -8, -13, 1, 4, 7, 12},
	{-2, -4, -6, -13, 1, 3, 5, 12},
	{-3, -6, -8, -12, 2, 5, 7, 11},
	{-3, -7, -9, -11, 2, 6, 8, 10},
	{-4, -7, -8, -11, 3, 6, 7, 10},
	{-3, -5, -8, -11, 2, 4, 7, 10},
	{-2, -6, -8, -10, 1, 5, 7, 9},
	{-2, -5, -8, -10, 1, 4, 7, 9},
	{-2, -4, -8, -10, 1, 3, 7, 9},
	{-2, -5, -7, -10, 1, 4, 6, 9},
	{-3, -4, -7, -10, 2, 3, 6, 9},
	{-1, -2, -3, -10, 0, 1, 2, 9},
	{-4, -6, -8, -9, 3, 5, 7, 8},
	{-3, -5, -7, -9, 2, 4, 6, 8},
}

// --- BC7 (Mode 6 encoder, all-mode decoder) ---

func encodeBC7(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	out := make([]byte, bw*bh*16)
	for by := range bh {
		for bx := range bw {
			pix := gatherBlock4x4(img, bx, by)
			off := (by*bw + bx) * 16
			encodeBC7Mode6Block(out[off:off+16], &pix)
		}
	}
	return out
}

func encodeBC7Mode6Block(dst []byte, pix *[16][4]uint8) {
	// Fit initial RGBA endpoints via 4D PCA, then refine with least-squares over bc7Weights4.
	lo, hi := fitBlockEndpoints4D(pix, true)

	// Convert bc7Weights4 to uint32 slice for shared linear refinement.
	var w32 [16]uint32
	for i, w := range bc7Weights4 {
		w32[i] = uint32(w)
	}

	evalQuant := func(c0, c1 [4]uint8, p0, p1 uint32) (e0, e1 [4]uint32, idx [16]uint8, err int) {
		var q0, q1 [4]int
		for ch := range 4 {
			// Quantize 8-bit c to 7-bit e such that (e<<1)|p is closest to c.
			v0 := (int(c0[ch]) - int(p0) + 1) >> 1
			if v0 < 0 {
				v0 = 0
			} else if v0 > 127 {
				v0 = 127
			}
			v1 := (int(c1[ch]) - int(p1) + 1) >> 1
			if v1 < 0 {
				v1 = 0
			} else if v1 > 127 {
				v1 = 127
			}
			e0[ch] = uint32(v0)
			e1[ch] = uint32(v1)
			q0[ch] = (v0 << 1) | int(p0)
			q1[ch] = (v1 << 1) | int(p1)
		}
		var pal [16][4]int
		for k, w := range bc7Weights4 {
			for ch := range 4 {
				pal[k][ch] = ((64-w)*q0[ch] + w*q1[ch] + 32) >> 6
			}
		}
		for i := range 16 {
			bestK := uint8(0)
			bestD := math.MaxInt
			for k := range 16 {
				dr := int(pix[i][0]) - pal[k][0]
				dg := int(pix[i][1]) - pal[k][1]
				db := int(pix[i][2]) - pal[k][2]
				da := int(pix[i][3]) - pal[k][3]
				d := dr*dr + dg*dg + db*db + da*da
				if d < bestD {
					bestD = d
					bestK = uint8(k)
				}
			}
			idx[i] = bestK
			err += bestD
		}
		return e0, e1, idx, err
	}

	pickBestP := func(c0, c1 [4]uint8) (bestE0, bestE1 [4]uint32, bestP0, bestP1 uint32, bestIdx [16]uint8, bestErr int) {
		bestErr = math.MaxInt
		for p0 := uint32(0); p0 <= 1; p0++ {
			for p1 := uint32(0); p1 <= 1; p1++ {
				e0, e1, idx, err := evalQuant(c0, c1, p0, p1)
				if err < bestErr {
					bestE0, bestE1, bestP0, bestP1, bestIdx, bestErr = e0, e1, p0, p1, idx, err
				}
			}
		}
		return
	}

	bestE0, bestE1, bestP0, bestP1, bestIdx, bestErr := pickBestP(lo, hi)

	for range 2 {
		nlo, nhi, ok := refineEndpointsLinear(pix, w32[:], &bestIdx, true)
		if !ok {
			break
		}
		e0, e1, p0, p1, idx, err := pickBestP(nlo, nhi)
		if err < bestErr {
			bestE0, bestE1, bestP0, bestP1, bestIdx, bestErr = e0, e1, p0, p1, idx, err
		} else {
			break
		}
	}

	// Enforce Mode 6 anchor constraint: texel 0 index MSB must be 0 (< 8).
	if bestIdx[0] >= 8 {
		bestE0, bestE1 = bestE1, bestE0
		bestP0, bestP1 = bestP1, bestP0
		for i := range 16 {
			bestIdx[i] = 15 - bestIdx[i]
		}
	}

	clear(dst[:16])
	writeBitsLE128(dst, 6, 1, 1) // Mode 6: 0b01000000
	pos := 7
	for ch := range 4 {
		writeBitsLE128(dst, pos, 7, bestE0[ch])
		pos += 7
		writeBitsLE128(dst, pos, 7, bestE1[ch])
		pos += 7
	}
	writeBitsLE128(dst, pos, 1, bestP0)
	pos++
	writeBitsLE128(dst, pos, 1, bestP1)
	pos++
	writeBitsLE128(dst, pos, 3, uint32(bestIdx[0]))
	pos += 3
	for i := 1; i < 16; i++ {
		writeBitsLE128(dst, pos, 4, uint32(bestIdx[i]))
		pos += 4
	}
}

func decodeBC7(raw []byte, width, height int) *image.NRGBA {
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
			decodeBC7Block(raw[off:off+16], &blockPix)
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
	return dst
}

// bc7ModeInfo describes the fields of one BC7 block mode (BC7 format specification, Mode Information): its subsets, partition, rotation and index-selection bits, color and alpha endpoint bits, per-endpoint or per-subset P-bits, and primary and secondary index bits.
type bc7ModeInfo struct {
	subsets, partitionBits, rotationBits, indexSelBits int
	colorBits, alphaBits                               int
	endpointPBits, sharedPBits                         bool
	indexBits, index2Bits                              int
}

var bc7Modes = [8]bc7ModeInfo{
	{subsets: 3, partitionBits: 4, colorBits: 4, endpointPBits: true, indexBits: 3},
	{subsets: 2, partitionBits: 6, colorBits: 6, sharedPBits: true, indexBits: 3},
	{subsets: 3, partitionBits: 6, colorBits: 5, indexBits: 2},
	{subsets: 2, partitionBits: 6, colorBits: 7, endpointPBits: true, indexBits: 2},
	{subsets: 1, rotationBits: 2, indexSelBits: 1, colorBits: 5, alphaBits: 6, indexBits: 2, index2Bits: 3},
	{subsets: 1, rotationBits: 2, colorBits: 7, alphaBits: 8, indexBits: 2, index2Bits: 2},
	{subsets: 1, colorBits: 7, alphaBits: 7, endpointPBits: true, indexBits: 4},
	{subsets: 2, partitionBits: 6, colorBits: 5, alphaBits: 5, endpointPBits: true, indexBits: 2},
}

var bc7Weights2 = [4]int{0, 21, 43, 64}
var bc7Weights3 = [8]int{0, 9, 18, 27, 37, 46, 55, 64}

func bc7Weight(bits int, idx uint32) int {
	switch bits {
	case 2:
		return bc7Weights2[idx]
	case 3:
		return bc7Weights3[idx]
	default:
		return bc7Weights4[idx]
	}
}

// bc7Dequant expands a bits-wide endpoint component to 8 bits by replicating its high bits.
func bc7Dequant(v uint32, bits int) uint8 {
	v <<= 8 - bits
	return uint8(v | v>>bits)
}

// decodeBC7Block decodes one 16-byte BC7 block in any of its eight modes into 16 RGBA texels in raster order. A block with no mode bit set is reserved and decodes to transparent black, as the specification requires.
func decodeBC7Block(block []byte, out *[16][4]uint8) {
	mode := -1
	for i := range 8 {
		if (block[0]>>i)&1 != 0 {
			mode = i
			break
		}
	}
	if mode < 0 {
		*out = [16][4]uint8{}
		return
	}
	m := bc7Modes[mode]
	pos := mode + 1
	read := func(n int) uint32 {
		v := readBitsLE128(block, pos, n)
		pos += n
		return v
	}
	partition := int(read(m.partitionBits))
	rotation := read(m.rotationBits)
	indexSel := read(m.indexSelBits)

	nEnd := m.subsets * 2
	var ep [6][4]uint32
	for ch := range 3 {
		for e := range nEnd {
			ep[e][ch] = read(m.colorBits)
		}
	}
	if m.alphaBits > 0 {
		for e := range nEnd {
			ep[e][3] = read(m.alphaBits)
		}
	}
	colorBits, alphaBits := m.colorBits, m.alphaBits
	switch {
	case m.endpointPBits:
		for e := range nEnd {
			p := read(1)
			for ch := range 4 {
				ep[e][ch] = ep[e][ch]<<1 | p
			}
		}
		colorBits++
		if alphaBits > 0 {
			alphaBits++
		}
	case m.sharedPBits:
		for s := range m.subsets {
			p := read(1)
			for e := s * 2; e < s*2+2; e++ {
				for ch := range 4 {
					ep[e][ch] = ep[e][ch]<<1 | p
				}
			}
		}
		colorBits++
	}
	var end [6][4]uint8
	for e := range nEnd {
		for ch := range 3 {
			end[e][ch] = bc7Dequant(ep[e][ch], colorBits)
		}
		end[e][3] = 255
		if alphaBits > 0 {
			end[e][3] = bc7Dequant(ep[e][3], alphaBits)
		}
	}

	var subsetOf [16]uint8
	anchors := [3]int{0, -1, -1}
	switch m.subsets {
	case 2:
		subsetOf = bc7Partition2[partition]
		anchors[1] = int(bc7Anchor2Second[partition])
	case 3:
		subsetOf = bc7Partition3[partition]
		anchors[1] = int(bc7Anchor3Second[partition])
		anchors[2] = int(bc7Anchor3Third[partition])
	}
	isAnchor := func(i int) bool { return i == anchors[0] || i == anchors[1] || i == anchors[2] }

	var idx1, idx2 [16]uint32
	for i := range 16 {
		n := m.indexBits
		if isAnchor(i) {
			n--
		}
		idx1[i] = read(n)
	}
	if m.index2Bits > 0 {
		for i := range 16 {
			n := m.index2Bits
			if i == 0 {
				n--
			}
			idx2[i] = read(n)
		}
	}

	for i := range 16 {
		s := int(subsetOf[i])
		e0, e1 := end[s*2], end[s*2+1]
		cIdx, cBits, aIdx, aBits := idx1[i], m.indexBits, idx1[i], m.indexBits
		if m.index2Bits > 0 {
			aIdx, aBits = idx2[i], m.index2Bits
			if indexSel == 1 {
				cIdx, cBits, aIdx, aBits = aIdx, aBits, cIdx, cBits
			}
		}
		cw, aw := bc7Weight(cBits, cIdx), bc7Weight(aBits, aIdx)
		var px [4]uint8
		for ch := range 3 {
			px[ch] = uint8(((64-cw)*int(e0[ch]) + cw*int(e1[ch]) + 32) >> 6)
		}
		px[3] = uint8(((64-aw)*int(e0[3]) + aw*int(e1[3]) + 32) >> 6)
		if rotation > 0 {
			px[3], px[rotation-1] = px[rotation-1], px[3]
		}
		out[i] = px
	}
}

// --- ETC2 RGBA8 (EAC 8-bit Alpha + ETC2 RGB8) ---

func encodeETC2RGBA8(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	out := make([]byte, bw*bh*16)
	for by := range bh {
		for bx := range bw {
			pix := gatherBlock4x4(img, bx, by)
			off := (by*bw + bx) * 16
			encodeEACAlphaBlock(out[off:off+8], &pix)
			rgbWord := encodeETC2Block(&pix)
			binary.BigEndian.PutUint64(out[off+8:off+16], rgbWord)
		}
	}
	return out
}

func encodeEACAlphaBlock(dst []byte, pix *[16][4]uint8) {
	minA, maxA := 255, 0
	for i := range 16 {
		a := int(pix[i][3])
		if a < minA {
			minA = a
		}
		if a > maxA {
			maxA = a
		}
	}
	if minA == maxA {
		clear(dst[:8])
		dst[0] = uint8(minA)
		// multiplier=0 -> constant alpha = base
		return
	}

	bestErr := math.MaxInt
	bestBase := uint8(0)
	bestMul := 0
	bestTbl := 0
	var bestBits uint64

	span := maxA - minA
	for tbl := range 16 {
		mods := eacModifierTable[tbl]
		modSpan := mods[7] - mods[3] // positive max - negative min
		estMul := (span + modSpan/2) / modSpan
		if estMul < 1 {
			estMul = 1
		}
		if estMul > 15 {
			estMul = 15
		}
		for mul := max(1, estMul-1); mul <= min(15, estMul+1); mul++ {
			// Center base so [base + mods[3]*mul, base + mods[7]*mul] straddles [minA, maxA].
			base := ((minA - mods[3]*mul) + (maxA - mods[7]*mul)) / 2
			if base < 0 {
				base = 0
			} else if base > 255 {
				base = 255
			}
			var pal [8]int
			for k := range 8 {
				pal[k] = int(clampByte(base + mods[k]*mul))
			}
			var bits uint64
			curErr := 0
			for y := range 4 {
				for x := range 4 {
					a := int(pix[y*4+x][3])
					bestK := uint64(0)
					bestD := math.MaxInt
					for k := range 8 {
						d := a - pal[k]
						d *= d
						if d < bestD {
							bestD = d
							bestK = uint64(k)
						}
					}
					curErr += bestD
					pi := x*4 + y // column-major per Khronos EAC spec
					shift := uint(45 - 3*pi)
					bits |= bestK << shift
				}
			}
			if curErr < bestErr {
				bestErr = curErr
				bestBase = uint8(base)
				bestMul = mul
				bestTbl = tbl
				bestBits = bits
			}
		}
	}

	dst[0] = bestBase
	dst[1] = uint8((bestMul << 4) | bestTbl)
	dst[2] = uint8(bestBits >> 40)
	dst[3] = uint8(bestBits >> 32)
	dst[4] = uint8(bestBits >> 24)
	dst[5] = uint8(bestBits >> 16)
	dst[6] = uint8(bestBits >> 8)
	dst[7] = uint8(bestBits)
}

func decodeETC2RGBA8(raw []byte, width, height int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	bw := (width + 3) / 4
	bh := (height + 3) / 4
	for by := range bh {
		for bx := range bw {
			off := (by*bw + bx) * 16
			if off+16 > len(raw) {
				break
			}
			rgbWord := binary.BigEndian.Uint64(raw[off+8 : off+16])
			decodeETC2Block(rgbWord, dst, bx*4, by*4, width, height)
			decodeEACAlphaBlock(raw[off:off+8], dst, bx*4, by*4, width, height)
		}
	}
	return dst
}

func decodeEACAlphaBlock(src []byte, dst *image.NRGBA, x0, y0, width, height int) {
	base := int(src[0])
	mul := int(src[1] >> 4)
	tbl := int(src[1] & 0x0F)
	bits := uint64(src[2])<<40 |
		uint64(src[3])<<32 |
		uint64(src[4])<<24 |
		uint64(src[5])<<16 |
		uint64(src[6])<<8 |
		uint64(src[7])

	for y := range 4 {
		py := y0 + y
		if py >= height {
			continue
		}
		rowOff := py * dst.Stride
		for x := range 4 {
			px := x0 + x
			if px >= width {
				continue
			}
			pi := x*4 + y
			shift := uint(45 - 3*pi)
			idx := (bits >> shift) & 7
			mod := eacModifierTable[tbl][idx]
			dst.Pix[rowOff+px*4+3] = clampByte(base + mod*mul)
		}
	}
}
