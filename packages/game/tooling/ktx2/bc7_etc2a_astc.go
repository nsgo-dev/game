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

// --- BC7 (Mode 6) ---

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

func decodeBC7Block(block []byte, out *[16][4]uint8) {
	mode := -1
	for i := range 8 {
		if (block[0]>>i)&1 != 0 {
			mode = i
			break
		}
	}
	if mode == 6 {
		pos := 7
		var e0, e1 [4]uint32
		for ch := range 4 {
			e0[ch] = readBitsLE128(block, pos, 7)
			pos += 7
			e1[ch] = readBitsLE128(block, pos, 7)
			pos += 7
		}
		p0 := readBitsLE128(block, pos, 1)
		pos++
		p1 := readBitsLE128(block, pos, 1)
		pos++
		var c0, c1 [4]int
		for ch := range 4 {
			c0[ch] = int((e0[ch] << 1) | p0)
			c1[ch] = int((e1[ch] << 1) | p1)
		}
		for i := range 16 {
			bits := 4
			if i == 0 {
				bits = 3
			}
			idx := readBitsLE128(block, pos, bits)
			pos += bits
			w := bc7Weights4[idx]
			for ch := range 4 {
				out[i][ch] = uint8(((64-w)*c0[ch] + w*c1[ch] + 32) >> 6)
			}
		}
		return
	}
	if mode < 0 {
		for i := range 16 {
			out[i] = [4]uint8{0, 0, 0, 255}
		}
		return
	}
	r, g, b, a := block[1], block[2], block[3], uint8(255)
	if mode >= 4 {
		a = block[4]
	}
	for i := range 16 {
		out[i] = [4]uint8{r, g, b, a}
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

// --- ASTC 4x4 LDR (Void-Extent + 1-partition CEM 8 / CEM 12) ---

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

func readASTCWeightBits(block []byte, bitIdx, count int) uint32 {
	var v uint32
	for i := range count {
		bp := 127 - (bitIdx + i)
		if (block[bp>>3]>>(bp&7))&1 != 0 {
			v |= 1 << i
		}
	}
	return v
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

func decodeASTC4x4(raw []byte, width, height int, srgb bool) *image.NRGBA {
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
			decodeASTC4x4Block(raw[off:off+16], srgb, &blockPix)
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

func astcBlueContract(r, g, b, a uint8) [4]uint8 {
	return [4]uint8{
		uint8((int(r) + int(b)) >> 1),
		uint8((int(g) + int(b)) >> 1),
		b,
		a,
	}
}

func decodeASTC4x4Block(block []byte, srgb bool, out *[16][4]uint8) {
	// 2D LDR Void-Extent block.
	if block[0] == 0xFC && (block[1]&0x0F) == 0x0D {
		r := uint8(binary.LittleEndian.Uint16(block[8:10]) >> 8)
		g := uint8(binary.LittleEndian.Uint16(block[10:12]) >> 8)
		b := uint8(binary.LittleEndian.Uint16(block[12:14]) >> 8)
		a := uint8(binary.LittleEndian.Uint16(block[14:16]) >> 8)
		for i := range 16 {
			out[i] = [4]uint8{r, g, b, a}
		}
		return
	}

	bm := readBitsLE128(block, 0, 11)
	part := readBitsLE128(block, 11, 2)
	cem := readBitsLE128(block, 13, 4)

	if part == 0 && (bm == 0x053 || bm == 0x042) && (cem == 8 || cem == 12) {
		wbits := uint8(2)
		if bm == 0x053 {
			wbits = 3
		}
		var v [8]uint8
		nEp := 6
		if cem == 12 {
			nEp = 8
		}
		for i := range nEp {
			v[i] = uint8(readBitsLE128(block, 17+i*8, 8))
		}
		var e0, e1 [4]uint8
		a0, a1 := uint8(255), uint8(255)
		if cem == 12 {
			a0, a1 = v[6], v[7]
		}
		if int(v[1])+int(v[3])+int(v[5]) >= int(v[0])+int(v[2])+int(v[4]) {
			e0 = [4]uint8{v[0], v[2], v[4], a0}
			e1 = [4]uint8{v[1], v[3], v[5], a1}
		} else {
			e0 = astcBlueContract(v[1], v[3], v[5], a1)
			e1 = astcBlueContract(v[0], v[2], v[4], a0)
		}
		for i := range 16 {
			rawW := readASTCWeightBits(block, i*int(wbits), int(wbits))
			w := uastcUnquantWeight(rawW, wbits)
			for ch := range 4 {
				out[i][ch] = uastcInterpolate(e0[ch], e1[ch], w, srgb)
			}
		}
		return
	}

	// Deterministic fallback for other ASTC block modes.
	r, g, b, a := block[4], block[5], block[6], uint8(255)
	if block[7] != 0 {
		a = block[7]
	}
	for i := range 16 {
		out[i] = [4]uint8{r, g, b, a}
	}
}
