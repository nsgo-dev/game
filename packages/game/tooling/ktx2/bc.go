package ktx2

import (
	"encoding/binary"
	"image"
	"math"
)

func rgb888To565(r, g, b uint8) uint16 {
	r5 := uint16((int(r)*31 + 127) / 255)
	g6 := uint16((int(g)*63 + 127) / 255)
	b5 := uint16((int(b)*31 + 127) / 255)
	return (r5 << 11) | (g6 << 5) | b5
}

func rgb565To888(c uint16) (r, g, b uint8) {
	r5 := uint8((c >> 11) & 0x1F)
	g6 := uint8((c >> 5) & 0x3F)
	b5 := uint8(c & 0x1F)
	r = (r5 << 3) | (r5 >> 2)
	g = (g6 << 2) | (g6 >> 4)
	b = (b5 << 3) | (b5 >> 2)
	return
}

func gatherBlock4x4(img *image.NRGBA, bx, by int) (pixels [16][4]uint8) {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	for dy := range 4 {
		sy := min(by*4+dy, h-1)
		rowOff := sy * img.Stride
		for dx := range 4 {
			sx := min(bx*4+dx, w-1)
			off := rowOff + sx*4
			i := dy*4 + dx
			pixels[i][0] = img.Pix[off+0]
			pixels[i][1] = img.Pix[off+1]
			pixels[i][2] = img.Pix[off+2]
			pixels[i][3] = img.Pix[off+3]
		}
	}
	return pixels
}

func encodeBC1(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	out := make([]byte, bw*bh*8)
	for by := range bh {
		for bx := range bw {
			pix := gatherBlock4x4(img, bx, by)
			off := (by*bw + bx) * 8
			encodeBC1Block(out[off:off+8], &pix, true)
		}
	}
	return out
}

func encodeBC3(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	out := make([]byte, bw*bh*16)
	for by := range bh {
		for bx := range bw {
			pix := gatherBlock4x4(img, bx, by)
			off := (by*bw + bx) * 16
			encodeBC3AlphaBlock(out[off:off+8], &pix)
			encodeBC1Block(out[off+8:off+16], &pix, false)
		}
	}
	return out
}

// encodeBC1Block compresses a 4x4 RGBA block into 8 bytes using principal-axis
// endpoint selection followed by least-squares cluster refinement.
func encodeBC1Block(dst []byte, pix *[16][4]uint8, allowAlpha bool) {
	var opaqueCount int
	var hasTransparent bool
	var sumR, sumG, sumB float64

	for i := range 16 {
		if allowAlpha && pix[i][3] < 128 {
			hasTransparent = true
			continue
		}
		opaqueCount++
		sumR += float64(pix[i][0])
		sumG += float64(pix[i][1])
		sumB += float64(pix[i][2])
	}

	if opaqueCount == 0 {
		// All pixels transparent: c0=0 <= c1=0, all indices = 3 (transparent black).
		binary.LittleEndian.PutUint16(dst[0:2], 0)
		binary.LittleEndian.PutUint16(dst[2:4], 0)
		binary.LittleEndian.PutUint32(dst[4:8], 0xFFFFFFFF)
		return
	}

	meanR := sumR / float64(opaqueCount)
	meanG := sumG / float64(opaqueCount)
	meanB := sumB / float64(opaqueCount)

	// Compute 3x3 covariance matrix for opaque pixels to find principal color axis.
	var cxx, cxy, cxz, cyy, cyz, czz float64
	var minR, minG, minB = 255.0, 255.0, 255.0
	var maxR, maxG, maxB = 0.0, 0.0, 0.0
	for i := range 16 {
		if allowAlpha && pix[i][3] < 128 {
			continue
		}
		r := float64(pix[i][0])
		g := float64(pix[i][1])
		b := float64(pix[i][2])
		if r < minR {
			minR = r
		}
		if g < minG {
			minG = g
		}
		if b < minB {
			minB = b
		}
		if r > maxR {
			maxR = r
		}
		if g > maxG {
			maxG = g
		}
		if b > maxB {
			maxB = b
		}
		dr := r - meanR
		dg := g - meanG
		db := b - meanB
		cxx += dr * dr
		cxy += dr * dg
		cxz += dr * db
		cyy += dg * dg
		cyz += dg * db
		czz += db * db
	}

	ax := maxR - minR
	ay := maxG - minG
	az := maxB - minB
	if ax == 0 && ay == 0 && az == 0 {
		// Single solid color block.
		c := rgb888To565(uint8(math.Round(meanR)), uint8(math.Round(meanG)), uint8(math.Round(meanB)))
		if hasTransparent {
			binary.LittleEndian.PutUint16(dst[0:2], c)
			binary.LittleEndian.PutUint16(dst[2:4], c)
			var indices uint32
			for i := range 16 {
				if pix[i][3] < 128 {
					indices |= 3 << (2 * i)
				}
			}
			binary.LittleEndian.PutUint32(dst[4:8], indices)
			return
		}
		// All pixels map to index 0 (which is always opaque c0).
		binary.LittleEndian.PutUint16(dst[0:2], c)
		binary.LittleEndian.PutUint16(dst[2:4], c)
		binary.LittleEndian.PutUint32(dst[4:8], 0)
		return
	}

	// Power iteration (4 steps) to align axis with principal eigenvector.
	for range 4 {
		nx := cxx*ax + cxy*ay + cxz*az
		ny := cxy*ax + cyy*ay + cyz*az
		nz := cxz*ax + cyz*ay + czz*az
		mag := math.Sqrt(nx*nx + ny*ny + nz*nz)
		if mag < 1e-9 {
			break
		}
		ax, ay, az = nx/mag, ny/mag, nz/mag
	}

	// Project opaque pixels onto the principal axis to get initial endpoints.
	minProj := math.Inf(1)
	maxProj := math.Inf(-1)
	var minIdx, maxIdx int
	for i := range 16 {
		if allowAlpha && pix[i][3] < 128 {
			continue
		}
		p := (float64(pix[i][0])-meanR)*ax + (float64(pix[i][1])-meanG)*ay + (float64(pix[i][2])-meanB)*az
		if p < minProj {
			minProj = p
			minIdx = i
		}
		if p > maxProj {
			maxProj = p
			maxIdx = i
		}
	}

	c0 := rgb888To565(pix[maxIdx][0], pix[maxIdx][1], pix[maxIdx][2])
	c1 := rgb888To565(pix[minIdx][0], pix[minIdx][1], pix[minIdx][2])

	if hasTransparent {
		if c0 > c1 {
			c0, c1 = c1, c0
		}
		indices := matchBC1Indices3Color(pix, c0, c1)
		// One least-squares refinement pass for 3-color mode.
		if nc0, nc1, ok := refineBC1Endpoints3Color(pix, indices); ok {
			if nc0 > nc1 {
				nc0, nc1 = nc1, nc0
			}
			nIdx := matchBC1Indices3Color(pix, nc0, nc1)
			if bc1BlockError(pix, nc0, nc1, nIdx, true) < bc1BlockError(pix, c0, c1, indices, true) {
				c0, c1, indices = nc0, nc1, nIdx
			}
		}
		binary.LittleEndian.PutUint16(dst[0:2], c0)
		binary.LittleEndian.PutUint16(dst[2:4], c1)
		binary.LittleEndian.PutUint32(dst[4:8], indices)
		return
	}

	// Opaque 4-color mode: ensure c0 > c1 (unless c0 == c1).
	if c0 < c1 {
		c0, c1 = c1, c0
	}
	indices := matchBC1Indices4Color(pix, c0, c1)
	bestErr := bc1BlockError(pix, c0, c1, indices, false)

	// Least-squares refinement (2 iterations).
	for range 2 {
		nc0, nc1, ok := refineBC1Endpoints4Color(pix, indices)
		if !ok {
			break
		}
		if nc0 < nc1 {
			nc0, nc1 = nc1, nc0
		}
		nIdx := matchBC1Indices4Color(pix, nc0, nc1)
		nErr := bc1BlockError(pix, nc0, nc1, nIdx, false)
		if nErr < bestErr {
			c0, c1, indices, bestErr = nc0, nc1, nIdx, nErr
		} else {
			break
		}
	}

	if c0 == c1 {
		indices = 0
	}
	binary.LittleEndian.PutUint16(dst[0:2], c0)
	binary.LittleEndian.PutUint16(dst[2:4], c1)
	binary.LittleEndian.PutUint32(dst[4:8], indices)
}

func bc1Palette4(c0, c1 uint16) (pal [4][3]int) {
	r0, g0, b0 := rgb565To888(c0)
	r1, g1, b1 := rgb565To888(c1)
	pal[0] = [3]int{int(r0), int(g0), int(b0)}
	pal[1] = [3]int{int(r1), int(g1), int(b1)}
	pal[2] = [3]int{(2*int(r0) + int(r1)) / 3, (2*int(g0) + int(g1)) / 3, (2*int(b0) + int(b1)) / 3}
	pal[3] = [3]int{(int(r0) + 2*int(r1)) / 3, (int(g0) + 2*int(g1)) / 3, (int(b0) + 2*int(b1)) / 3}
	return pal
}

func bc1Palette3(c0, c1 uint16) (pal [3][3]int) {
	r0, g0, b0 := rgb565To888(c0)
	r1, g1, b1 := rgb565To888(c1)
	pal[0] = [3]int{int(r0), int(g0), int(b0)}
	pal[1] = [3]int{int(r1), int(g1), int(b1)}
	pal[2] = [3]int{(int(r0) + int(r1)) / 2, (int(g0) + int(g1)) / 2, (int(b0) + int(b1)) / 2}
	return pal
}

func matchBC1Indices4Color(pix *[16][4]uint8, c0, c1 uint16) uint32 {
	pal := bc1Palette4(c0, c1)
	var indices uint32
	for i := range 16 {
		r, g, b := int(pix[i][0]), int(pix[i][1]), int(pix[i][2])
		bestIdx := uint32(0)
		bestDist := math.MaxInt
		for k := range 4 {
			dr := r - pal[k][0]
			dg := g - pal[k][1]
			db := b - pal[k][2]
			d := dr*dr + dg*dg + db*db
			if d < bestDist {
				bestDist = d
				bestIdx = uint32(k)
			}
		}
		indices |= bestIdx << (2 * i)
	}
	return indices
}

func matchBC1Indices3Color(pix *[16][4]uint8, c0, c1 uint16) uint32 {
	pal := bc1Palette3(c0, c1)
	var indices uint32
	for i := range 16 {
		if pix[i][3] < 128 {
			indices |= 3 << (2 * i)
			continue
		}
		r, g, b := int(pix[i][0]), int(pix[i][1]), int(pix[i][2])
		bestIdx := uint32(0)
		bestDist := math.MaxInt
		for k := range 3 {
			dr := r - pal[k][0]
			dg := g - pal[k][1]
			db := b - pal[k][2]
			d := dr*dr + dg*dg + db*db
			if d < bestDist {
				bestDist = d
				bestIdx = uint32(k)
			}
		}
		indices |= bestIdx << (2 * i)
	}
	return indices
}

func refineBC1Endpoints4Color(pix *[16][4]uint8, indices uint32) (uint16, uint16, bool) {
	// Weights for palette entries 0..3 as linear combinations w0*C0 + w1*C1:
	// 0: (1, 0), 1: (0, 1), 2: (2/3, 1/3), 3: (1/3, 2/3).
	w0 := [4]float64{1, 0, 2.0 / 3.0, 1.0 / 3.0}
	w1 := [4]float64{0, 1, 1.0 / 3.0, 2.0 / 3.0}
	var a00, a01, a11 float64
	var b0, b1 [3]float64
	for i := range 16 {
		idx := (indices >> (2 * i)) & 3
		u, v := w0[idx], w1[idx]
		a00 += u * u
		a01 += u * v
		a11 += v * v
		for ch := range 3 {
			val := float64(pix[i][ch])
			b0[ch] += u * val
			b1[ch] += v * val
		}
	}
	det := a00*a11 - a01*a01
	if math.Abs(det) < 1e-6 {
		return 0, 0, false
	}
	invDet := 1.0 / det
	var e0, e1 [3]uint8
	for ch := range 3 {
		v0 := (a11*b0[ch] - a01*b1[ch]) * invDet
		v1 := (a00*b1[ch] - a01*b0[ch]) * invDet
		e0[ch] = clampByte(int(math.Round(v0)))
		e1[ch] = clampByte(int(math.Round(v1)))
	}
	return rgb888To565(e0[0], e0[1], e0[2]), rgb888To565(e1[0], e1[1], e1[2]), true
}

func refineBC1Endpoints3Color(pix *[16][4]uint8, indices uint32) (uint16, uint16, bool) {
	w0 := [3]float64{1, 0, 0.5}
	w1 := [3]float64{0, 1, 0.5}
	var a00, a01, a11 float64
	var b0, b1 [3]float64
	for i := range 16 {
		idx := (indices >> (2 * i)) & 3
		if idx == 3 {
			continue
		}
		u, v := w0[idx], w1[idx]
		a00 += u * u
		a01 += u * v
		a11 += v * v
		for ch := range 3 {
			val := float64(pix[i][ch])
			b0[ch] += u * val
			b1[ch] += v * val
		}
	}
	det := a00*a11 - a01*a01
	if math.Abs(det) < 1e-6 {
		return 0, 0, false
	}
	invDet := 1.0 / det
	var e0, e1 [3]uint8
	for ch := range 3 {
		v0 := (a11*b0[ch] - a01*b1[ch]) * invDet
		v1 := (a00*b1[ch] - a01*b0[ch]) * invDet
		e0[ch] = clampByte(int(math.Round(v0)))
		e1[ch] = clampByte(int(math.Round(v1)))
	}
	return rgb888To565(e0[0], e0[1], e0[2]), rgb888To565(e1[0], e1[1], e1[2]), true
}

func bc1BlockError(pix *[16][4]uint8, c0, c1 uint16, indices uint32, threeColor bool) int {
	total := 0
	if threeColor {
		pal := bc1Palette3(c0, c1)
		for i := range 16 {
			idx := (indices >> (2 * i)) & 3
			if idx == 3 {
				continue
			}
			dr := int(pix[i][0]) - pal[idx][0]
			dg := int(pix[i][1]) - pal[idx][1]
			db := int(pix[i][2]) - pal[idx][2]
			total += dr*dr + dg*dg + db*db
		}
		return total
	}
	pal := bc1Palette4(c0, c1)
	for i := range 16 {
		idx := (indices >> (2 * i)) & 3
		dr := int(pix[i][0]) - pal[idx][0]
		dg := int(pix[i][1]) - pal[idx][1]
		db := int(pix[i][2]) - pal[idx][2]
		total += dr*dr + dg*dg + db*db
	}
	return total
}

func bc3AlphaPalette(a0, a1 uint8) (pal [8]uint8) {
	pal[0] = a0
	pal[1] = a1
	if a0 > a1 {
		for k := 2; k < 8; k++ {
			pal[k] = uint8(((8-k)*int(a0) + (k-1)*int(a1)) / 7)
		}
	} else {
		for k := 2; k < 6; k++ {
			pal[k] = uint8(((6-k)*int(a0) + (k-1)*int(a1)) / 5)
		}
		pal[6] = 0
		pal[7] = 255
	}
	return pal
}

func encodeBC3AlphaBlock(dst []byte, pix *[16][4]uint8) {
	minAll, maxAll := uint8(255), uint8(0)
	minMid, maxMid := uint8(255), uint8(0)
	hasMid := false
	for i := range 16 {
		a := pix[i][3]
		if a < minAll {
			minAll = a
		}
		if a > maxAll {
			maxAll = a
		}
		if a != 0 && a != 255 {
			hasMid = true
			if a < minMid {
				minMid = a
			}
			if a > maxMid {
				maxMid = a
			}
		}
	}

	eval := func(a0, a1 uint8) (uint64, int) {
		pal := bc3AlphaPalette(a0, a1)
		var bits uint64
		totalErr := 0
		for i := range 16 {
			a := int(pix[i][3])
			bestK := uint64(0)
			bestD := math.MaxInt
			for k := range 8 {
				d := a - int(pal[k])
				d *= d
				if d < bestD {
					bestD = d
					bestK = uint64(k)
				}
			}
			bits |= bestK << (3 * i)
			totalErr += bestD
		}
		return bits, totalErr
	}

	// Mode 1: 8-entry interpolated palette (a0 >= a1; if a0 == a1, all alphas are equal).
	bestA0, bestA1 := maxAll, minAll
	bestBits, bestErr := eval(bestA0, bestA1)

	// Mode 2: 6-entry interpolated palette + explicit 0 and 255 (a0 <= a1).
	if hasMid && (minAll == 0 || maxAll == 255) {
		candA0, candA1 := minMid, maxMid
		if candA0 > candA1 {
			candA0, candA1 = candA1, candA0
		}
		bits2, err2 := eval(candA0, candA1)
		if err2 < bestErr {
			bestA0, bestA1, bestBits, bestErr = candA0, candA1, bits2, err2
		}
	}

	dst[0] = bestA0
	dst[1] = bestA1
	for i := range 6 {
		dst[2+i] = byte(bestBits >> (8 * i))
	}
}

func decodeBC1(raw []byte, width, height int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	bw := (width + 3) / 4
	bh := (height + 3) / 4
	for by := range bh {
		for bx := range bw {
			off := (by*bw + bx) * 8
			if off+8 > len(raw) {
				break
			}
			decodeBC1Block(raw[off:off+8], dst, bx*4, by*4, width, height, false)
		}
	}
	return dst
}

func decodeBC3(raw []byte, width, height int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	bw := (width + 3) / 4
	bh := (height + 3) / 4
	for by := range bh {
		for bx := range bw {
			off := (by*bw + bx) * 16
			if off+16 > len(raw) {
				break
			}
			decodeBC1Block(raw[off+8:off+16], dst, bx*4, by*4, width, height, true)
			decodeBC3AlphaBlock(raw[off:off+8], dst, bx*4, by*4, width, height)
		}
	}
	return dst
}

func decodeBC1Block(src []byte, dst *image.NRGBA, x0, y0, w, h int, forceOpaque bool) {
	c0 := binary.LittleEndian.Uint16(src[0:2])
	c1 := binary.LittleEndian.Uint16(src[2:4])
	indices := binary.LittleEndian.Uint32(src[4:8])

	var pal [4][4]uint8
	r0, g0, b0 := rgb565To888(c0)
	r1, g1, b1 := rgb565To888(c1)
	pal[0] = [4]uint8{r0, g0, b0, 255}
	pal[1] = [4]uint8{r1, g1, b1, 255}

	if forceOpaque || c0 > c1 {
		pal[2] = [4]uint8{
			uint8((2*int(r0) + int(r1)) / 3),
			uint8((2*int(g0) + int(g1)) / 3),
			uint8((2*int(b0) + int(b1)) / 3),
			255,
		}
		pal[3] = [4]uint8{
			uint8((int(r0) + 2*int(r1)) / 3),
			uint8((int(g0) + 2*int(g1)) / 3),
			uint8((int(b0) + 2*int(b1)) / 3),
			255,
		}
	} else {
		pal[2] = [4]uint8{
			uint8((int(r0) + int(r1)) / 2),
			uint8((int(g0) + int(g1)) / 2),
			uint8((int(b0) + int(b1)) / 2),
			255,
		}
		pal[3] = [4]uint8{0, 0, 0, 0}
	}

	for dy := range 4 {
		py := y0 + dy
		if py >= h {
			continue
		}
		rowOff := py * dst.Stride
		for dx := range 4 {
			px := x0 + dx
			if px >= w {
				continue
			}
			i := dy*4 + dx
			c := pal[(indices>>(2*i))&3]
			off := rowOff + px*4
			dst.Pix[off+0] = c[0]
			dst.Pix[off+1] = c[1]
			dst.Pix[off+2] = c[2]
			dst.Pix[off+3] = c[3]
		}
	}
}

func decodeBC3AlphaBlock(src []byte, dst *image.NRGBA, x0, y0, w, h int) {
	pal := bc3AlphaPalette(src[0], src[1])
	var bits uint64
	for i := range 6 {
		bits |= uint64(src[2+i]) << (8 * i)
	}
	for dy := range 4 {
		py := y0 + dy
		if py >= h {
			continue
		}
		rowOff := py * dst.Stride
		for dx := range 4 {
			px := x0 + dx
			if px >= w {
				continue
			}
			i := dy*4 + dx
			dst.Pix[rowOff+px*4+3] = pal[(bits>>(3*i))&7]
		}
	}
}
