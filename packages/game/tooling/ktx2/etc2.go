package ktx2

import (
	"encoding/binary"
	"image"
	"math"
)

// Standard Khronos ETC1/ETC2 modifier table (OpenGL ES 3.0 Table C.11).
// Indexed by [tableCodeword][(msb<<1)|lsb].
var etcModifierTable = [8][4]int{
	{2, 8, -2, -8},
	{5, 17, -5, -17},
	{9, 29, -9, -29},
	{13, 42, -13, -42},
	{18, 60, -18, -60},
	{24, 80, -24, -80},
	{33, 106, -33, -106},
	{47, 183, -47, -183},
}

// Khronos ETC2 T/H distance table (OpenGL ES 3.0 Table C.13).
var etcDistTable = [8]int{3, 6, 11, 16, 23, 32, 41, 64}

func expand4(v uint64) int {
	x := int(v & 0xF)
	return (x << 4) | x
}

func expand5(v int) int {
	x := v & 0x1F
	return (x << 3) | (x >> 2)
}

func expand6(v uint64) int {
	x := int(v & 0x3F)
	return (x << 2) | (x >> 4)
}

func expand7(v uint64) int {
	x := int(v & 0x7F)
	return (x << 1) | (x >> 6)
}

func signExt3(v uint64) int {
	x := int(v & 7)
	if x >= 4 {
		return x - 8
	}
	return x
}

func quant4(v float64) int {
	q := int(math.Round(v * 15.0 / 255.0))
	if q < 0 {
		return 0
	}
	if q > 15 {
		return 15
	}
	return q
}

func quant5(v float64) int {
	q := int(math.Round(v * 31.0 / 255.0))
	if q < 0 {
		return 0
	}
	if q > 31 {
		return 31
	}
	return q
}

func quant6(v float64) int {
	q := int(math.Round(v * 63.0 / 255.0))
	if q < 0 {
		return 0
	}
	if q > 63 {
		return 63
	}
	return q
}

func quant7(v float64) int {
	q := int(math.Round(v * 127.0 / 255.0))
	if q < 0 {
		return 0
	}
	if q > 127 {
		return 127
	}
	return q
}

func encodeETC2RGB8(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	out := make([]byte, bw*bh*8)
	for by := range bh {
		for bx := range bw {
			pix := gatherBlock4x4(img, bx, by)
			word := encodeETC2Block(&pix)
			off := (by*bw + bx) * 8
			binary.BigEndian.PutUint64(out[off:off+8], word)
		}
	}
	return out
}

func encodeETC2Block(pix *[16][4]uint8) uint64 {
	bestWord := uint64(0)
	bestErr := math.MaxInt

	for flip := range 2 {
		// Individual mode (diff=0)
		if w, err := encodeETC2SubblockMode(pix, flip, false); err < bestErr {
			bestWord, bestErr = w, err
		}
		// Differential mode (diff=1)
		if w, err := encodeETC2SubblockMode(pix, flip, true); err < bestErr {
			bestWord, bestErr = w, err
		}
	}

	// Planar mode (ideal for smooth color gradients)
	if w, err := encodeETC2Planar(pix); err < bestErr {
		bestWord, bestErr = w, err
	}

	// T-mode and H-mode (ideal for sharp multi-color edges, text, and checkerboards)
	if w, err := encodeETC2TH(pix); err < bestErr {
		bestWord, bestErr = w, err
	}

	return bestWord
}

func subblockCoords(flip, sub int) [8][2]int {
	var coords [8][2]int
	idx := 0
	if flip == 0 {
		xStart := sub * 2
		for x := xStart; x < xStart+2; x++ {
			for y := range 4 {
				coords[idx] = [2]int{x, y}
				idx++
			}
		}
	} else {
		yStart := sub * 2
		for x := range 4 {
			for y := yStart; y < yStart+2; y++ {
				coords[idx] = [2]int{x, y}
				idx++
			}
		}
	}
	return coords
}

func encodeETC2SubblockMode(pix *[16][4]uint8, flip int, diff bool) (uint64, int) {
	c0 := subblockCoords(flip, 0)
	c1 := subblockCoords(flip, 1)

	avg := func(coords [8][2]int) (float64, float64, float64) {
		var r, g, b float64
		for _, pt := range coords {
			p := pix[pt[1]*4+pt[0]]
			r += float64(p[0])
			g += float64(p[1])
			b += float64(p[2])
		}
		return r / 8.0, g / 8.0, b / 8.0
	}

	ar1, ag1, ab1 := avg(c0)
	ar2, ag2, ab2 := avg(c1)

	var header uint64
	var base1, base2 [3]int

	if !diff {
		r1, g1, b1 := quant4(ar1), quant4(ag1), quant4(ab1)
		r2, g2, b2 := quant4(ar2), quant4(ag2), quant4(ab2)
		base1 = [3]int{expand4(uint64(r1)), expand4(uint64(g1)), expand4(uint64(b1))}
		base2 = [3]int{expand4(uint64(r2)), expand4(uint64(g2)), expand4(uint64(b2))}
		header = (uint64(r1) << 60) | (uint64(r2) << 56) |
			(uint64(g1) << 52) | (uint64(g2) << 48) |
			(uint64(b1) << 44) | (uint64(b2) << 40)
	} else {
		r1, g1, b1 := quant5(ar1), quant5(ag1), quant5(ab1)
		r2, g2, b2 := quant5(ar2), quant5(ag2), quant5(ab2)
		clampDelta := func(v1, v2 int) (int, int) {
			d := v2 - v1
			if d < -4 {
				d = -4
			}
			if d > 3 {
				d = 3
			}
			// Keep v1+d strictly in [0..31] so we remain in Differential mode.
			if v1+d < 0 {
				d = -v1
			}
			if v1+d > 31 {
				d = 31 - v1
			}
			return d, v1 + d
		}
		dr, rr2 := clampDelta(r1, r2)
		dg, gg2 := clampDelta(g1, g2)
		db, bb2 := clampDelta(b1, b2)
		base1 = [3]int{expand5(r1), expand5(g1), expand5(b1)}
		base2 = [3]int{expand5(rr2), expand5(gg2), expand5(bb2)}
		header = (uint64(r1) << 59) | (uint64(dr&7) << 56) |
			(uint64(g1) << 51) | (uint64(dg&7) << 48) |
			(uint64(b1) << 43) | (uint64(db&7) << 40) |
			(1 << 33)
	}
	header |= uint64(flip&1) << 32

	fitSub := func(coords [8][2]int, base [3]int) (bestCW int, msbBits, lsbBits uint32, bestErr int) {
		bestErr = math.MaxInt
		for cw := range 8 {
			var curMSB, curLSB uint32
			curErr := 0
			mods := etcModifierTable[cw]
			for _, pt := range coords {
				p := pix[pt[1]*4+pt[0]]
				pr, pg, pb := int(p[0]), int(p[1]), int(p[2])
				bestIdx := 0
				bestD := math.MaxInt
				for k := range 4 {
					m := mods[k]
					dr := pr - int(clampByte(base[0]+m))
					dg := pg - int(clampByte(base[1]+m))
					db := pb - int(clampByte(base[2]+m))
					d := dr*dr + dg*dg + db*db
					if d < bestD {
						bestD = d
						bestIdx = k
					}
				}
				curErr += bestD
				pixelBit := uint(pt[0]*4 + pt[1])
				if bestIdx&1 != 0 {
					curLSB |= 1 << pixelBit
				}
				if bestIdx&2 != 0 {
					curMSB |= 1 << pixelBit
				}
			}
			if curErr < bestErr {
				bestErr = curErr
				bestCW = cw
				msbBits = curMSB
				lsbBits = curLSB
			}
		}
		return bestCW, msbBits, lsbBits, bestErr
	}

	cw1, msb1, lsb1, err1 := fitSub(c0, base1)
	cw2, msb2, lsb2, err2 := fitSub(c1, base2)

	word := header |
		(uint64(cw1) << 37) |
		(uint64(cw2) << 34) |
		(uint64(msb1|msb2) << 16) |
		uint64(lsb1|lsb2)
	return word, err1 + err2
}

// encodeETC2Planar fits a linear RGB plane across the 4x4 block and packs it
// into a spec-compliant Khronos ETC2 Planar block.
func encodeETC2Planar(pix *[16][4]uint8) (uint64, int) {
	// Least-squares fit for f(x,y) = a + b*x + c*y over x,y in {0,1,2,3}:
	// mean(x)=mean(y)=1.5, sum((x-1.5)^2)=sum((y-1.5)^2)=20 across 16 pixels.
	fitChannel := func(ch int) (o, h, v float64) {
		var sum, sumX, sumY float64
		for y := range 4 {
			for x := range 4 {
				val := float64(pix[y*4+x][ch])
				sum += val
				sumX += val * (float64(x) - 1.5)
				sumY += val * (float64(y) - 1.5)
			}
		}
		mean := sum / 16.0
		slopeX := sumX / 20.0
		slopeY := sumY / 20.0
		// Plane value at (0,0), (4,0), (0,4):
		o = mean - 1.5*slopeX - 1.5*slopeY
		h = o + 4.0*slopeX
		v = o + 4.0*slopeY
		return o, h, v
	}

	roF, rhF, rvF := fitChannel(0)
	goF, ghF, gvF := fitChannel(1)
	boF, bhF, bvF := fitChannel(2)

	ro6, rh6, rv6 := uint64(quant6(roF)), uint64(quant6(rhF)), uint64(quant6(rvF))
	go7, gh7, gv7 := uint64(quant7(goF)), uint64(quant7(ghF)), uint64(quant7(gvF))
	bo6, bh6, bv6 := uint64(quant6(boF)), uint64(quant6(bhF)), uint64(quant6(bvF))

	// Pack planar payload bits according to OpenGL ES 3.0 §C.2.4.
	var w uint64
	w |= (ro6 & 0x3F) << 57
	w |= ((go7 >> 6) & 1) << 56
	w |= (go7 & 0x3F) << 49
	w |= ((bo6 >> 5) & 1) << 48
	w |= ((bo6 >> 3) & 3) << 43
	w |= (bo6 & 7) << 39
	w |= ((rh6 >> 1) & 0x1F) << 34
	w |= 1 << 33 // diff = 1
	w |= (rh6 & 1) << 32
	w |= (gh7 & 0x7F) << 25
	w |= (bh6 & 0x3F) << 19
	w |= (rv6 & 0x3F) << 13
	w |= (gv7 & 0x7F) << 6
	w |= bv6 & 0x3F

	// Set non-payload selector bits so R+dR in [0..31], G+dG in [0..31], and B+dB overflows [0..31].
	if (w>>58)&1 == 1 {
		w |= 1 << 63
	}
	if (w>>50)&1 == 1 {
		w |= 1 << 55
	}
	bo43 := (bo6 >> 3) & 3
	bo21 := (bo6 >> 1) & 3
	if bo43+bo21 >= 4 {
		// Force B_check = 28 + bo43, dB = bo21 => sum >= 32 (overflow > 31).
		w |= 7 << 45
	} else {
		// Force B_check = bo43, dB = bo21 - 4 => sum <= -1 (overflow < 0).
		w |= 1 << 42
	}

	// Evaluate exact decoded error for this planar block.
	ro, rh, rv := expand6(ro6), expand6(rh6), expand6(rv6)
	go_, gh, gv := expand7(go7), expand7(gh7), expand7(gv7)
	bo, bh, bv := expand6(bo6), expand6(bh6), expand6(bv6)

	totalErr := 0
	for y := range 4 {
		for x := range 4 {
			r := int(clampByte((x*(rh-ro) + y*(rv-ro) + 4*ro + 2) >> 2))
			g := int(clampByte((x*(gh-go_) + y*(gv-go_) + 4*go_ + 2) >> 2))
			b := int(clampByte((x*(bh-bo) + y*(bv-bo) + 4*bo + 2) >> 2))
			p := pix[y*4+x]
			dr := int(p[0]) - r
			dg := int(p[1]) - g
			db := int(p[2]) - b
			totalErr += dr*dr + dg*dg + db*db
		}
	}
	return w, totalErr
}

// encodeETC2TH clusters the 4x4 block into two representative RGB444 colors and
// evaluates both Khronos ETC2 T-mode and H-mode, returning the lower-error block.
func encodeETC2TH(pix *[16][4]uint8) (uint64, int) {
	// Initialize two centers from the farthest pixel from block mean and the farthest from it.
	var mean [3]float64
	for i := range 16 {
		mean[0] += float64(pix[i][0])
		mean[1] += float64(pix[i][1])
		mean[2] += float64(pix[i][2])
	}
	mean[0] /= 16
	mean[1] /= 16
	mean[2] /= 16

	farthest := func(ref [3]float64) [3]float64 {
		bestD := -1.0
		var best [3]float64
		for i := range 16 {
			dr := float64(pix[i][0]) - ref[0]
			dg := float64(pix[i][1]) - ref[1]
			db := float64(pix[i][2]) - ref[2]
			d := dr*dr + dg*dg + db*db
			if d > bestD {
				bestD = d
				best = [3]float64{float64(pix[i][0]), float64(pix[i][1]), float64(pix[i][2])}
			}
		}
		return best
	}

	cA := farthest(mean)
	cB := farthest(cA)

	// 2 iterations of 2-means clustering.
	for range 2 {
		var sumA, sumB [3]float64
		var cntA, cntB int
		for i := range 16 {
			pr, pg, pb := float64(pix[i][0]), float64(pix[i][1]), float64(pix[i][2])
			dA := (pr-cA[0])*(pr-cA[0]) + (pg-cA[1])*(pg-cA[1]) + (pb-cA[2])*(pb-cA[2])
			dB := (pr-cB[0])*(pr-cB[0]) + (pg-cB[1])*(pg-cB[1]) + (pb-cB[2])*(pb-cB[2])
			if dA <= dB {
				sumA[0] += pr
				sumA[1] += pg
				sumA[2] += pb
				cntA++
			} else {
				sumB[0] += pr
				sumB[1] += pg
				sumB[2] += pb
				cntB++
			}
		}
		if cntA > 0 {
			cA = [3]float64{sumA[0] / float64(cntA), sumA[1] / float64(cntA), sumA[2] / float64(cntA)}
		}
		if cntB > 0 {
			cB = [3]float64{sumB[0] / float64(cntB), sumB[1] / float64(cntB), sumB[2] / float64(cntB)}
		}
	}

	qA := [3]uint64{uint64(quant4(cA[0])), uint64(quant4(cA[1])), uint64(quant4(cA[2]))}
	qB := [3]uint64{uint64(quant4(cB[0])), uint64(quant4(cB[1])), uint64(quant4(cB[2]))}

	bestWord := uint64(0)
	bestErr := math.MaxInt

	matchPal := func(pal [4][3]int) (msb, lsb uint32, totalErr int) {
		for x := range 4 {
			for y := range 4 {
				p := pix[y*4+x]
				pr, pg, pb := int(p[0]), int(p[1]), int(p[2])
				bestK := 0
				bestD := math.MaxInt
				for k := range 4 {
					dr := pr - pal[k][0]
					dg := pg - pal[k][1]
					db := pb - pal[k][2]
					d := dr*dr + dg*dg + db*db
					if d < bestD {
						bestD = d
						bestK = k
					}
				}
				totalErr += bestD
				bit := uint(x*4 + y)
				if bestK&1 != 0 {
					lsb |= 1 << bit
				}
				if bestK&2 != 0 {
					msb |= 1 << bit
				}
			}
		}
		return msb, lsb, totalErr
	}

	// Evaluate T-mode for both (qA, qB) and (qB, qA).
	pairs := [2][2][3]uint64{{qA, qB}, {qB, qA}}
	for _, pair := range pairs {
		c1, c2 := pair[0], pair[1]
		r1, g1, b1 := expand4(c1[0]), expand4(c1[1]), expand4(c1[2])
		r2, g2, b2 := expand4(c2[0]), expand4(c2[1]), expand4(c2[2])
		for dIdx := range 8 {
			d := etcDistTable[dIdx]
			pal := [4][3]int{
				{r1, g1, b1},
				{int(clampByte(r2 + d)), int(clampByte(g2 + d)), int(clampByte(b2 + d))},
				{r2, g2, b2},
				{int(clampByte(r2 - d)), int(clampByte(g2 - d)), int(clampByte(b2 - d))},
			}
			msb, lsb, err := matchPal(pal)
			if err < bestErr {
				bestErr = err
				var w uint64
				r1Hi := (c1[0] >> 2) & 3
				r1Lo := c1[0] & 3
				if r1Hi+r1Lo >= 4 {
					w |= 7 << 61
				} else {
					w |= 1 << 58
				}
				w |= r1Hi << 59
				w |= r1Lo << 56
				w |= c1[1] << 52
				w |= c1[2] << 48
				w |= c2[0] << 44
				w |= c2[1] << 40
				w |= c2[2] << 36
				w |= uint64((dIdx>>1)&3) << 34
				w |= 1 << 33
				w |= uint64(dIdx&1) << 32
				w |= uint64(msb) << 16
				w |= uint64(lsb)
				bestWord = w
			}
		}
	}

	// Evaluate H-mode for both (qA, qB) and (qB, qA).
	for _, pair := range pairs {
		c1, c2 := pair[0], pair[1]
		r1, g1, b1 := expand4(c1[0]), expand4(c1[1]), expand4(c1[2])
		r2, g2, b2 := expand4(c2[0]), expand4(c2[1]), expand4(c2[2])
		c1Val := (r1 << 16) | (g1 << 8) | b1
		c2Val := (r2 << 16) | (g2 << 8) | b2
		lsbD := 0
		if c1Val >= c2Val {
			lsbD = 1
		}
		for hi := range 4 {
			dIdx := (hi << 1) | lsbD
			d := etcDistTable[dIdx]
			pal := [4][3]int{
				{int(clampByte(r1 + d)), int(clampByte(g1 + d)), int(clampByte(b1 + d))},
				{int(clampByte(r1 - d)), int(clampByte(g1 - d)), int(clampByte(b1 - d))},
				{int(clampByte(r2 + d)), int(clampByte(g2 + d)), int(clampByte(b2 + d))},
				{int(clampByte(r2 - d)), int(clampByte(g2 - d)), int(clampByte(b2 - d))},
			}
			msb, lsb, err := matchPal(pal)
			if err < bestErr {
				bestErr = err
				var w uint64
				w |= c1[0] << 59
				w |= ((c1[1] >> 1) & 7) << 56
				w |= (c1[1] & 1) << 52
				w |= ((c1[2] >> 3) & 1) << 51
				w |= (c1[2] & 7) << 47
				w |= c2[0] << 43
				w |= c2[1] << 39
				w |= c2[2] << 35
				w |= uint64((hi>>1)&1) << 34
				w |= 1 << 33
				w |= uint64(hi&1) << 32
				w |= uint64(msb) << 16
				w |= uint64(lsb)

				// Keep R+dR in [0..31].
				if (w>>58)&1 == 1 {
					w |= 1 << 63
				}
				// Force G+dG to overflow [0..31].
				gCheckLo := ((c1[1] & 1) << 1) | ((c1[2] >> 3) & 1)
				dgLo := (c1[2] >> 1) & 3
				if gCheckLo+dgLo >= 4 {
					w |= 7 << 53
				} else {
					w |= 1 << 50
				}
				bestWord = w
			}
		}
	}

	return bestWord, bestErr
}

func decodeETC2RGB8(raw []byte, width, height int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	bw := (width + 3) / 4
	bh := (height + 3) / 4
	for by := range bh {
		for bx := range bw {
			off := (by*bw + bx) * 8
			if off+8 > len(raw) {
				break
			}
			w := binary.BigEndian.Uint64(raw[off : off+8])
			decodeETC2Block(w, dst, bx*4, by*4, width, height)
		}
	}
	return dst
}

func decodeETC2Block(w uint64, dst *image.NRGBA, x0, y0, width, height int) {
	diff := (w >> 33) & 1
	flip := (w >> 32) & 1

	writePixel := func(dx, dy int, r, g, b uint8) {
		px, py := x0+dx, y0+dy
		if px >= width || py >= height {
			return
		}
		off := py*dst.Stride + px*4
		dst.Pix[off+0] = r
		dst.Pix[off+1] = g
		dst.Pix[off+2] = b
		dst.Pix[off+3] = 255
	}

	if diff == 0 {
		// Individual mode
		base1 := [3]int{expand4((w >> 60) & 0xF), expand4((w >> 52) & 0xF), expand4((w >> 44) & 0xF)}
		base2 := [3]int{expand4((w >> 56) & 0xF), expand4((w >> 48) & 0xF), expand4((w >> 40) & 0xF)}
		cw1 := int((w >> 37) & 7)
		cw2 := int((w >> 34) & 7)
		decodeETC2Subblocks(w, flip, base1, base2, cw1, cw2, writePixel)
		return
	}

	r := int((w >> 59) & 0x1F)
	dr := signExt3((w >> 56) & 7)
	g := int((w >> 51) & 0x1F)
	dg := signExt3((w >> 48) & 7)
	b := int((w >> 43) & 0x1F)
	db := signExt3((w >> 40) & 7)

	if r+dr < 0 || r+dr > 31 {
		// T-mode
		r1 := expand4((((w >> 59) & 3) << 2) | ((w >> 56) & 3))
		g1 := expand4((w >> 52) & 0xF)
		b1 := expand4((w >> 48) & 0xF)
		r2 := expand4((w >> 44) & 0xF)
		g2 := expand4((w >> 40) & 0xF)
		b2 := expand4((w >> 36) & 0xF)
		d := etcDistTable[(((w>>34)&3)<<1)|((w>>32)&1)]
		pal := [4][3]uint8{
			{uint8(r1), uint8(g1), uint8(b1)},
			{clampByte(r2 + d), clampByte(g2 + d), clampByte(b2 + d)},
			{uint8(r2), uint8(g2), uint8(b2)},
			{clampByte(r2 - d), clampByte(g2 - d), clampByte(b2 - d)},
		}
		for x := range 4 {
			for y := range 4 {
				bit := uint(x*4 + y)
				idx := (((w >> (bit + 16)) & 1) << 1) | ((w >> bit) & 1)
				c := pal[idx]
				writePixel(x, y, c[0], c[1], c[2])
			}
		}
		return
	}

	if g+dg < 0 || g+dg > 31 {
		// H-mode
		r1 := expand4((w >> 59) & 0xF)
		g1 := expand4((((w >> 56) & 7) << 1) | ((w >> 52) & 1))
		b1 := expand4((((w >> 51) & 1) << 3) | ((w >> 47) & 7))
		r2 := expand4((w >> 43) & 0xF)
		g2 := expand4((w >> 39) & 0xF)
		b2 := expand4((w >> 35) & 0xF)
		da := (w >> 34) & 1
		dbBit := (w >> 32) & 1
		c1Val := (r1 << 16) | (g1 << 8) | b1
		c2Val := (r2 << 16) | (g2 << 8) | b2
		dIdx := (da << 2) | (dbBit << 1)
		if c1Val >= c2Val {
			dIdx |= 1
		}
		d := etcDistTable[dIdx]
		pal := [4][3]uint8{
			{clampByte(r1 + d), clampByte(g1 + d), clampByte(b1 + d)},
			{clampByte(r1 - d), clampByte(g1 - d), clampByte(b1 - d)},
			{clampByte(r2 + d), clampByte(g2 + d), clampByte(b2 + d)},
			{clampByte(r2 - d), clampByte(g2 - d), clampByte(b2 - d)},
		}
		for x := range 4 {
			for y := range 4 {
				bit := uint(x*4 + y)
				idx := (((w >> (bit + 16)) & 1) << 1) | ((w >> bit) & 1)
				c := pal[idx]
				writePixel(x, y, c[0], c[1], c[2])
			}
		}
		return
	}

	if b+db < 0 || b+db > 31 {
		// Planar mode
		ro := expand6((w >> 57) & 0x3F)
		go_ := expand7((((w >> 56) & 1) << 6) | ((w >> 49) & 0x3F))
		bo := expand6((((w >> 48) & 1) << 5) | (((w >> 43) & 3) << 3) | ((w >> 39) & 7))
		rh := expand6((((w >> 34) & 0x1F) << 1) | ((w >> 32) & 1))
		gh := expand7((w >> 25) & 0x7F)
		bh := expand6((w >> 19) & 0x3F)
		rv := expand6((w >> 13) & 0x3F)
		gv := expand7((w >> 6) & 0x7F)
		bv := expand6(w & 0x3F)

		for y := range 4 {
			for x := range 4 {
				pr := clampByte((x*(rh-ro) + y*(rv-ro) + 4*ro + 2) >> 2)
				pg := clampByte((x*(gh-go_) + y*(gv-go_) + 4*go_ + 2) >> 2)
				pb := clampByte((x*(bh-bo) + y*(bv-bo) + 4*bo + 2) >> 2)
				writePixel(x, y, pr, pg, pb)
			}
		}
		return
	}

	// Differential mode
	base1 := [3]int{expand5(r), expand5(g), expand5(b)}
	base2 := [3]int{expand5(r + dr), expand5(g + dg), expand5(b + db)}
	cw1 := int((w >> 37) & 7)
	cw2 := int((w >> 34) & 7)
	decodeETC2Subblocks(w, flip, base1, base2, cw1, cw2, writePixel)
}

func decodeETC2Subblocks(w, flip uint64, base1, base2 [3]int, cw1, cw2 int, writePixel func(dx, dy int, r, g, b uint8)) {
	for x := range 4 {
		for y := range 4 {
			inSub1 := (flip == 0 && x < 2) || (flip == 1 && y < 2)
			base := base2
			cw := cw2
			if inSub1 {
				base = base1
				cw = cw1
			}
			bit := uint(x*4 + y)
			idx := (((w >> (bit + 16)) & 1) << 1) | ((w >> bit) & 1)
			m := etcModifierTable[cw][idx]
			writePixel(x, y, clampByte(base[0]+m), clampByte(base[1]+m), clampByte(base[2]+m))
		}
	}
}
