// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/summon-rise.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the rubble, dust, grit, embers and smoke recipe from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// SummonRise burns a rune circle into the floor, bursts the ground, and heaves a horned brute up out of it through dust and rubble, lit from below, until its eyes flare. It is registered as "spawn/summon-rise".
var SummonRise = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Summon rise",
		Desc:     "A rune circle burns into the floor, the ground bursts, and a horned brute heaves up out of it through dust and rubble, lit from below, until its eyes flare.",
		Tags:     []string{"spawn", "summon", "demon", "rise", "runes", "dust", "rubble", "ground", "one-shot"},
		Size:     [2]int{176, 116},
		Origin:   [2]int{88, 94},
		Duration: summonDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0608", "#3d0c0c", "#7a1a10", "#c0381a", "#f06a1e", "#ffae3a", "#ffe08a", "#ffffff")},
			{Name: "body", Default: pixelfx.Palette("#060409", "#0e0913", "#170f1e", "#211628", "#2d2036")},
			{Name: "horn", Default: pixelfx.Palette("#0c080c", "#201820", "#352a32", "#4e3f48", "#6e5c64")},
			{Name: "stone", Default: pixelfx.Palette("#1a1424", "#2b2639", "#4a4263", "#6c6488", "#9a91b4")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newSummonRise,
}

func init() { pixelfx.Register("spawn/summon-rise", SummonRise) }

var (
	summonDuration = 4.6
	summonSoot     = pixelfx.Hex("#2a1e2a")
	summonDustC    = pixelfx.Hex("#7a6c78")
	summonIgn      = 0.5
	summonRunes    = 0.78
	summonBreak    = 1.05
	summonEyes     = 2.3
	summonFade     = 3.4
	summonSY       = 0.3
	summonR0       = 36.0
	summonR1       = 27.0
	summonBeast    = buildSummonBeast()
	summonGlyphs   = [][][][2]float64{
		{{{0, -2}, {0, 2}}, {{-1.5, -1}, {1.5, 1}}},
		{{{-1.5, 2}, {0, -2}, {1.5, 2}}, {{-0.8, 0.4}, {0.8, 0.4}}},
		{{{-1.5, -2}, {-1.5, 2}, {1.5, 2}}, {{0, -2}, {1.5, -0.5}}},
		{{{0, -2}, {0, 2}}, {{0, -2}, {1.6, -0.6}, {0, 0.6}}},
		{{{-1.5, -2}, {1.5, 2}}, {{1.5, -2}, {-1.5, 2}}, {{-1.5, 0}, {1.5, 0}}},
		{{{-1.4, -2}, {1.4, -2}, {-1.4, 2}, {1.4, 2}}},
	}
)

type summonBeastData struct {
	w, h    int
	part    []uint8
	amb     []float32
	down    []float32
	rim     []float32
	crease  []uint8
	outline [][2]int
	eyes    [][2]int
	tusks   [][2]int
	glints  [2][2]float64
}

// buildSummonBeast builds the brute part by part so overlapping forms keep their own edges. Parts front to back: 1 head, 2 horns, 3 arms, 4 shoulders, 5 chest, 6 waist, 7 legs, 8 tail. Each pixel keeps the frontmost part over it, that part's surface direction (from its own blurred shape), and whether it sits on a crease over a part behind.
func buildSummonBeast() summonBeastData {
	const W, H = 46, 46
	const N = W * H
	const C = 22.5
	var parts [8][]uint8
	for i := range parts {
		parts[i] = make([]uint8, N)
	}
	var cur []uint8
	put := func(x, y float64) {
		ix, iy := int(pixelfx.Round(x)), int(pixelfx.Round(y))
		if ix >= 0 && iy >= 0 && ix < W && iy < H {
			cur[iy*W+ix] = 1
		}
	}
	ell := func(cx, cy, rx, ry float64) {
		for y := math.Floor(cy - ry); y <= math.Ceil(cy+ry); y++ {
			for x := math.Floor(cx - rx); x <= math.Ceil(cx+rx); x++ {
				dx, dy := (x+0.5-cx)/rx, (y+0.5-cy)/ry
				if dx*dx+dy*dy <= 1 {
					put(x, y)
				}
			}
		}
	}
	stroke := func(pts [][2]float64, r0, r1 float64) {
		L := make([]float64, len(pts)-1)
		total := 0.0
		for i := 1; i < len(pts); i++ {
			d := pixelfx.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
			L[i-1] = d
			total += d
		}
		acc := 0.0
		for i := 1; i < len(pts); i++ {
			n := int(math.Ceil(L[i-1]*3)) + 1
			for j := 0; j <= n; j++ {
				u := float64(j) / float64(n)
				r := r0 + (r1-r0)*((acc+L[i-1]*u)/total)
				ell(pts[i-1][0]+(pts[i][0]-pts[i-1][0])*u, pts[i-1][1]+(pts[i][1]-pts[i-1][1])*u, r, r)
			}
			acc += L[i-1]
		}
	}
	HEAD, HORNS, ARMS, SHOULDERS, CHEST, WAIST, LEGS, TAIL := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6], parts[7]
	for _, s := range [2]float64{-1, 1} {
		X := func(d float64) float64 { return C + s*d }
		cur = HORNS
		stroke([][2]float64{{X(4), 11}, {X(8), 8}, {X(12), 6.5}, {X(15), 4}, {X(16), 1.5}, {X(15.2), 0}}, 2.3, 0.45)
		cur = SHOULDERS
		ell(X(10), 16, 7, 5)
		ell(X(8.5), 19.5, 5.5, 4.2)
		cur = ARMS
		stroke([][2]float64{{X(14.5), 16.5}, {X(17.5), 22}, {X(18.5), 28}}, 4, 3.2)
		stroke([][2]float64{{X(18.5), 28}, {X(19), 33}, {X(19.2), 36.5}}, 3.4, 2.8)
		ell(X(19.2), 37.8, 3, 2.3)
		stroke([][2]float64{{X(17.2), 39}, {X(16.6), 41.8}}, 0.9, 0.4)
		stroke([][2]float64{{X(19.2), 39.5}, {X(19.3), 42.6}}, 0.9, 0.4)
		stroke([][2]float64{{X(21.2), 38.8}, {X(22), 41.4}}, 0.9, 0.4)
		cur = LEGS
		stroke([][2]float64{{X(5.5), 31}, {X(8), 37}, {X(7.5), 42}}, 4, 3)
		ell(X(8.5), 44.3, 4.2, 1.7)
	}
	cur = TAIL
	stroke([][2]float64{{C, 30}, {C + 0.5, 36}, {C + 2.5, 41.5}, {C + 7, 44.6}, {C + 12.5, 44.2}, {C + 14.5, 42.5}}, 2.3, 0.6)
	cur = CHEST
	ell(C, 22.5, 8.2, 7.5)
	cur = WAIST
	ell(C, 29.5, 6.2, 4.5)
	cur = HEAD
	ell(C, 12.5, 5.2, 4.6)
	ell(C, 16, 4.2, 2.8)

	part := make([]uint8, N)
	for i := range N {
		for k := range parts {
			if parts[k][i] != 0 {
				part[i] = uint8(k + 1)
				break
			}
		}
	}
	on := func(x, y int) bool { return x >= 0 && y >= 0 && x < W && y < H && part[y*W+x] > 0 }

	// surface direction per part: the gradient of the part blurred on its own
	blur := func(src []uint8) []float32 {
		a, b := make([]float32, N), make([]float32, N)
		for i, v := range src {
			a[i] = float32(v)
		}
		for range 2 {
			for y := 0; y < H; y++ {
				for x := 0; x < W; x++ {
					s := 0.0
					for d := -2; d <= 2; d++ {
						if x+d >= 0 && x+d < W {
							s += float64(a[y*W+x+d])
						}
					}
					b[y*W+x] = float32(s / 5)
				}
			}
			for y := 0; y < H; y++ {
				for x := 0; x < W; x++ {
					s := 0.0
					for d := -2; d <= 2; d++ {
						if y+d >= 0 && y+d < H {
							s += float64(b[(y+d)*W+x])
						}
					}
					a[y*W+x] = float32(s / 5)
				}
			}
		}
		return a
	}

	amb, down, rim, crease := make([]float32, N), make([]float32, N), make([]float32, N), make([]uint8, N)
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for k, pm := range parts {
		B := blur(pm)
		at := func(x, y int) float64 {
			if x >= 0 && y >= 0 && x < W && y < H {
				return float64(B[y*W+x])
			}
			return 0
		}
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				i := y*W + x
				if part[i] != uint8(k+1) {
					continue
				}
				gx, gy := at(x+1, y)-at(x-1, y), at(x, y+1)-at(x, y-1)
				g := pixelfx.Hypot(gx, gy)
				if g == 0 {
					g = 1e-6
				}
				ox, oy, e := -gx/g, -gy/g, pixelfx.Clamp(g/0.28)
				// a dim cool light from above and the left rounds each form; undersides fall into shadow
				amb[i] = float32(0.42 + e*(0.4*math.Max(0, -oy)+0.14*math.Max(0, -ox)-0.3*math.Max(0, oy)-0.08*math.Max(0, ox)))
				// less light reaches up into the gap between the legs, so it doesn't read as a lit doorway
				gapMul := 1.0
				if math.Abs(float64(x)+0.5-C) < 7 && y > 27 {
					gapMul = 0.3
				}
				down[i] = float32(e * math.Max(0, oy) * gapMul)
				// a crease where this part's edge lies over a part behind it
				for _, d := range dirs {
					nx, ny := x+d[0], y+d[1]
					if on(nx, ny) && part[ny*W+nx] > uint8(k+1) {
						crease[i] = 1
					}
				}
			}
		}
	}
	// the underlight's rim: the bottom of the whole silhouette, two pixels deep; less in the gap between the legs
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			if !on(x, y) {
				continue
			}
			k := 0.0
			if !on(x, y+1) {
				k = 1
			} else if !on(x, y+2) {
				k = 0.5
			}
			if math.Abs(float64(x)+0.5-C) < 7 && y > 27 {
				k *= 0.3
			}
			rim[y*W+x] = float32(k)
		}
	}
	var outline [][2]int
	for y := -1; y <= H; y++ {
		for x := -1; x <= W; x++ {
			if !on(x, y) && (on(x-1, y) || on(x+1, y) || on(x, y-1) || on(x, y+1)) {
				outline = append(outline, [2]int{x, y})
			}
		}
	}
	var eyes, tusks [][2]int
	for _, s := range [2]float64{-1, 1} {
		eyes = append(eyes, [2]int{int(pixelfx.Round(C + s*2.2 - 0.5)), 12}, [2]int{int(pixelfx.Round(C + s*3.2 - 0.5)), 12})
		tusks = append(tusks, [2]int{int(pixelfx.Round(C + s*3.4 - 0.5)), 16}, [2]int{int(pixelfx.Round(C + s*3.6 - 0.5)), 15})
	}
	return summonBeastData{
		w: W, h: H, part: part, amb: amb, down: down, rim: rim, crease: crease, outline: outline, eyes: eyes, tusks: tusks,
		glints: [2][2]float64{{pixelfx.Round(C-2.7-0.5) - W/2, 12 - H}, {pixelfx.Round(C+2.7-0.5) - W/2, 12 - H}},
	}
}

type summonRune struct {
	a float64
	g [][][2]float64
}

func newSummonRise(o *pixelfx.Options) pixelfx.Instance {
	C, SH, HR, ST := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("body"), 5), pixelfx.Fit(o.Ramp("horn"), 5), pixelfx.Fit(o.Ramp("stone"), 5)
	DUST := pixelfx.Mix(ST[2], summonDustC, 0.45)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[6], C[5], C[4], C[3]}})
	// rubble: the floor's own stone, its faces lit warm by the fire underneath
	RUB := []uint32{ST[0], ST[1], pixelfx.Mix(ST[2], C[2], 0.3), pixelfx.Mix(ST[3], C[4], 0.3), pixelfx.Mix(ST[4], C[5], 0.35)}
	rocks, lights := pixelfx.NewBits(floor), &pixelfx.Lights{}
	var runes [12]summonRune
	for i := range runes {
		runes[i] = summonRune{a: (float64(i)/12)*tau + 0.13, g: summonGlyphs[(i*5+2)%len(summonGlyphs)]}
	}
	// cracks radiating from the middle of the circle, on the floor plane
	cracks := make([][][2]float64, 0, 7)
	for i := range 7 {
		a, r := (float64(i)/7)*tau+R.Range(-0.25, 0.25), 3.0
		pts := [][2]float64{{0, 0}}
		length := R.Range(summonR1*0.6, summonR1*1.05)
		for r < length {
			r += R.Range(2, 4)
			a += R.Range(-0.3, 0.3)
			pts = append(pts, [2]float64{math.Cos(a) * r, math.Sin(a) * r * summonSY})
		}
		cracks = append(cracks, pts)
	}
	// the rise: two heaves with a sag between, a little overshoot at the top
	rise := func(tt float64) float64 {
		if tt < summonBreak {
			return 0
		}
		a := pixelfx.Ease.OutCubic(pixelfx.Clamp((tt-summonBreak)/0.4)) * 0.56
		sag := 0.04 * math.Sin(pi*pixelfx.Clamp((tt-summonBreak-0.42)/0.3))
		b := pixelfx.OutBack(pixelfx.Clamp((tt-summonBreak-0.62)/0.45), 1.4) * 0.44
		return a - sag + b
	}
	// the circle's fire: lights up, flares at the break and when the eyes open, dies in the fade
	heat := func(tt float64) float64 {
		db, de := tt-summonBreak, tt-summonEyes
		return pixelfx.Smooth(0, summonIgn, tt) * (1 - pixelfx.Smooth(summonFade, summonFade+0.8, tt)) * (1 - 0.25*pixelfx.Smooth(summonEyes+0.2, summonEyes+0.9, tt)) * (0.85 + 0.15*pixelfx.Noise(tt*9, 3) + 0.5*math.Exp(-(db*db)/0.02) + 0.3*math.Exp(-(de*de)/0.01))
	}
	t := 0.0
	dur := summonDuration
	done := map[string]bool{}
	// a life that ends before the effect does
	left := func(a, b float64) float64 { return math.Max(0.05, math.Min(R.Range(a, b), dur-0.1-t)) }
	once := func(k string, at float64) bool {
		if !done[k] && t >= at {
			done[k] = true
			return true
		}
		return false
	}
	topEdge := func() [][2]float64 {
		off := float64(summonBeast.h) * (1 - rise(t))
		var pts [][2]float64
		for x := 0; x < summonBeast.w; x++ {
			for y := 0; y < summonBeast.h; y++ {
				if summonBeast.part[y*summonBeast.w+x] != 0 {
					ly := float64(y-summonBeast.h) + off
					if ly < 0 {
						pts = append(pts, [2]float64{float64(x) - float64(summonBeast.w)/2, ly})
					}
					break
				}
			}
		}
		return pts
	}
	emberRadii := []float64{summonR0, summonR1, summonR0}
	gritCols1 := []uint32{ST[2], ST[3], ST[4]}
	gritCols2 := []uint32{ST[2], ST[3], ST[3], ST[4]}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			h := heat(t)
			// the rings smoulder: embers and thin smoke off the lines, sparks at the burning front
			if t < summonIgn {
				ph := (t / summonIgn) * pi
				for _, s := range [2]float64{-1, 1} {
					a := pi/2 + s*ph
					y := math.Sin(a) * summonR0 * summonSY
					if R.Chance(0.5) {
						if y < -0.5 {
							vx, vy, life := R.Range(-40, 40), -R.Range(40, 130), R.Range(0.2, 0.45)
							ps.Spawn(pixelfx.Spark, math.Cos(a)*summonR0, y, vx, vy, life, pixelfx.White, 1, 0, 0)
						} else {
							vx, vy, life := R.Range(-20, 20), -R.Range(30, 70), R.Range(0.3, 0.6)
							ps.Spawn(pixelfx.Ember, math.Cos(a)*summonR0, y, vx, vy, life, C[6], 1.4, 0, 0)
						}
					}
				}
			}
			if t > 0.2 && t < summonFade+0.6 && R.Chance(0.2*h) {
				a, r := R.Range(0, tau), pixelfx.Pick(R, emberRadii)
				vx, vy, life, size := R.Range(-10, 10), -R.Range(20, 50), left(0.8, 1.6), R.Range(1, 1.8)
				ps.Spawn(pixelfx.Ember, math.Cos(a)*r, math.Sin(a)*r*summonSY, vx, vy, life, C[5], size, 0, 0)
			}
			if t > 0.15 && R.Chance(0.4*math.Min(1, h)) {
				a := R.Range(0, tau)
				r := summonR1
				if R.Chance(0.7) {
					r = summonR0
				}
				if t >= summonIgn || math.Abs(math.Mod(a-pi/2+pi*3, tau)-pi) < (t/summonIgn)*pi {
					vx, vy, life, size := R.Range(-4, 4), -R.Range(6, 16), R.Range(0.18, 0.4), R.Range(0.8, 1.6)*math.Min(1, h)
					var fl pixelfx.Flag
					if R.Chance(0.25) {
						fl = pixelfx.Smokes
					}
					ps.Spawn(pixelfx.Fire, math.Cos(a)*r, math.Sin(a)*r*summonSY-0.5, vx, vy, life, 0, size, 0, fl)
				}
			}
			if t > 0.3 && t < summonFade+0.4 && R.Chance(0.05*h) {
				a := R.Range(0, tau)
				vx, vy, life, size := R.Range(-4, 4), -R.Range(12, 24), left(1.4, 2.2), R.Range(2.2, 3.4)
				ps.Spawn(pixelfx.Smoke, math.Cos(a)*summonR0*0.9, math.Sin(a)*summonR0*summonSY-1, vx, vy, life, 0, size, 0, 0)
			}
			if once("ign", summonIgn) {
				lights.Add(0, -4, 60, C[4], 0.55, 0.5)
			}
			if once("runes", summonRunes) {
				lights.Add(0, -4, 50, C[5], 0.5, 0.35)
			}
			// the rumble: grit jumps on the floor, dust stirs
			if t > summonRunes && t < summonBreak && R.Chance(0.35) {
				x, vx, vy, life, col := R.Range(-summonR1, summonR1), R.Range(-20, 20), -R.Range(40, 120), R.Range(0.5, 0.9), pixelfx.Pick(R, gritCols1)
				ps.Spawn(pixelfx.Grit, x, -0.5, vx, vy, life, col, 1, 0, 0)
			}
			if t > summonRunes && t < summonBreak && R.Chance(0.1) {
				x, vx, vy, life, size := R.Range(-summonR1, summonR1), R.Range(-12, 12), -R.Range(4, 12), R.Range(0.4, 0.7), R.Range(1.5, 2.5)
				ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, DUST, size, 0, 0)
			}
			// the break: the floor bursts open
			if once("break", summonBreak) {
				lights.Add(0, -18, 64, C[4], 0.9, 0.6)
				lights.Add(0, -8, 26, C[6], 0.8, 0.14)
				for range 18 {
					x, vx, vy := R.Range(-14, 14), R.Range(-75, 75), -R.Range(170, 320)
					life, ang, va, s := R.Range(1.6, 2.4), R.Range(0, tau), R.Range(-14, 14), R.Range(1.3, 3)
					rocks.Add(pixelfx.Bit{Kind: pixelfx.Chunk, X: x, Y: -2, VX: vx, VY: vy, G: 1, Drag: 0.7, Bounce: 0.32, Life: life, A: ang, VA: va, S: s, Ramp: RUB})
				}
				for range 60 {
					u := R.Range(-1, 1)
					a := -pi/2 + u*0.7
					sp := R.Range(80, 230) * (1 - 0.4*math.Abs(u))
					life, col := R.Range(1, 1.8), pixelfx.Pick(R, gritCols2)
					size := 1.0
					if R.Chance(0.4) {
						size = 2
					}
					ps.Spawn(pixelfx.Grit, u*10, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, col, size, 0, 0)
				}
				for range 12 {
					s := R.Sign()
					x, y, vx, vy, life, size, delay := s*R.Range(2, 14), -R.Range(1, 5), s*R.Range(35, 100), -R.Range(4, 24), R.Range(1.2, 2), R.Range(3.5, 6), R.Range(0, 0.1)
					ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, DUST, size, delay, pixelfx.Big)
				}
				for range 4 {
					s := R.Sign()
					x, y, vx, vy, life, size, delay := s*R.Range(10, 18), -R.Range(2, 8), s*R.Range(10, 30), -R.Range(40, 90), R.Range(1.4, 2.2), R.Range(4, 6.5), R.Range(0, 0.12)
					ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, DUST, size, delay, pixelfx.Big)
				}
				for range 24 {
					a, sp := -pi/2+R.Range(-1.1, 1.1), R.Range(150, 360)
					x, life := R.Range(-6, 6), R.Range(0.25, 0.65)
					ps.Spawn(pixelfx.Spark, x, -2, math.Cos(a)*sp, math.Sin(a)*sp, life, pixelfx.White, 1, 0, 0)
				}
			}
			// the heave: earth slides off its shoulders, dust rolls out round its feet
			rv := rise(t) - rise(t-dt)
			if t > summonBreak && t < summonBreak+1.2 && rv > 0 {
				if R.Chance(math.Min(0.9, rv*140)) {
					e := topEdge()
					if len(e) > 0 {
						pt := pixelfx.Pick(R, e)
						vx, vy, life, col := R.Range(-25, 25), -R.Range(10, 60), R.Range(0.8, 1.4), pixelfx.Pick(R, gritCols1)
						size := 1.0
						if R.Chance(0.3) {
							size = 2
						}
						ps.Spawn(pixelfx.Grit, pt[0], pt[1], vx, vy, life, col, size, 0, 0)
					}
				}
				if R.Chance(math.Min(0.8, rv*70)) {
					s := R.Sign()
					x, y, vx, vy, life, size := s*R.Range(10, 22), -R.Range(1, 4), s*R.Range(30, 80), -R.Range(2, 12), R.Range(1, 1.8), R.Range(3, 5.5)
					ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, DUST, size, 0, pixelfx.Big)
				}
			}
			if once("heave2", summonBreak+0.64) {
				lights.Add(0, -10, 56, C[4], 0.6, 0.4)
				for range 8 {
					x := R.Sign() * R.Range(10, 22)
					vx := R.Sign() * R.Range(25, 70)
					vy := -R.Range(90, 180)
					life, ang, va, s := R.Range(1.2, 1.8), R.Range(0, tau), R.Range(-12, 12), R.Range(1.2, 2.4)
					rocks.Add(pixelfx.Bit{Kind: pixelfx.Chunk, X: x, Y: -2, VX: vx, VY: vy, G: 1, Drag: 0.7, Bounce: 0.3, Life: life, A: ang, VA: va, S: s, Ramp: RUB})
				}
			}
			// the eyes flare
			if once("eyes", summonEyes) {
				for _, gl := range summonBeast.glints {
					ex, ey := gl[0], gl[1]
					lights.Add(ex, ey, 14, C[6], 0.7, 0.3)
					for range 5 {
						vx, vy, life := R.Range(-90, 90), R.Range(-90, 20), R.Range(0.15, 0.35)
						ps.Spawn(pixelfx.Spark, ex, ey, vx, vy, life, pixelfx.White, 1, 0, 0)
					}
				}
				for range 10 {
					s := R.Sign()
					x, vx, vy, life, size := s*R.Range(8, 20), s*R.Range(30, 80), -R.Range(3, 10), R.Range(0.4, 0.8), R.Range(1.5, 3)
					ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, DUST, size, 0, 0)
				}
			}
			if t > summonFade+0.05 && t < summonFade+0.75 && R.Chance(0.45) {
				x, y := int(R.Int(4, float64(summonBeast.w-5))), int(R.Int(2, float64(summonBeast.h-4)))
				if summonBeast.part[y*summonBeast.w+x] != 0 {
					lx, ly := float64(x)-float64(summonBeast.w)/2, float64(y-summonBeast.h)
					vx, vy, life, size := R.Range(-6, 6), -R.Range(10, 26), left(0.9, 1.5), R.Range(2, 3.4)
					ps.Spawn(pixelfx.Smoke, lx, ly, vx, vy, life, summonSoot, size, 0, 0)
					if R.Chance(0.3) {
						vx, vy, life := R.Range(-10, 10), -R.Range(15, 40), left(0.6, 1.2)
						ps.Spawn(pixelfx.Ember, lx, ly, vx, vy, life, C[4], 1, 0, 0)
					}
				}
			}
			rocks.Update(dt)
			for i := range rocks.List {
				b := &rocks.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 2 {
						dx, vx, vy, life, size := R.Range(-1.5, 1.5), R.Sign()*R.Range(6, 30), -R.Range(2, 12), R.Range(0.3, 0.6), R.Range(1.2, 2.4)
						ps.Spawn(pixelfx.Dust, b.X+dx, -0.5, vx, vy, life, DUST, size, 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			h := heat(t)
			if h <= 0.01 {
				return
			}
			// the warm pool on the floor
			p.Glow(0, -2, 52, C[4], 0.28*h)
			p.Glow(0, -22, 72, C[4], 0.1*h)
			p.Glow(0, -1, 22, C[5], 0.2*h)
			// the cracks open under it and glow
			ck := pixelfx.Smooth(summonRunes, summonBreak, t) * (1 - pixelfx.Smooth(summonFade, summonFade+0.7, t))
			if ck > 0 {
				cool := pixelfx.Smooth(summonBreak+0.1, summonBreak+1.1, t)
				for _, pts := range cracks {
					for i := 1; i < len(pts); i++ {
						u := float64(i) / float64(len(pts))
						if u > ck*1.1 {
							break
						}
						ci := int(pixelfx.ClampTo(pixelfx.Round(6-u*2-cool*2.6), 2, 6))
						p.Line(pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1], C[ci], 1)
					}
				}
			}
			// the rings: burnt in from the front round to the back, flickering
			const n = 220
			prog := pixelfx.Clamp(t/summonIgn) * pi
			for j := range n {
				a := (float64(j) / n) * tau
				d := math.Abs(math.Mod(a-pi/2+pi*3, tau) - pi)
				if d > prog {
					continue
				}
				fresh := prog-d < 0.25 && t < summonIgn+0.1
				f := pixelfx.Noise(a*6+t*5, 4)*0.5 + 0.5
				k := h * (0.7 + 0.5*f)
				ca, sa := math.Cos(a), math.Sin(a)
				var c0 uint32
				switch {
				case fresh:
					c0 = pixelfx.White
				case k > 0.95:
					c0 = C[6]
				case k > 0.6:
					c0 = C[5]
				case k > 0.35:
					c0 = C[4]
				default:
					c0 = C[3]
				}
				p.Dot(ca*summonR0, sa*summonR0*summonSY, c0, 1)
				c1 := C[3]
				if k > 0.8 {
					c1 = C[4]
				}
				p.Dot(ca*(summonR0+2.2), sa*(summonR0+2.2)*summonSY, c1, 0.9)
				if t > summonRunes-0.15 {
					c2 := C[3]
					if k > 0.9 {
						c2 = C[5]
					} else if k > 0.5 {
						c2 = C[4]
					}
					p.Dot(ca*summonR1, sa*summonR1*summonSY, c2, pixelfx.Smooth(summonRunes-0.15, summonRunes+0.05, t))
				}
				if j%11 == 0 {
					p.Glow(ca*summonR0, sa*summonR0*summonSY, 9, C[4], 0.22*k)
				}
			}
			// the runes between the rings
			rk := pixelfx.Smooth(summonRunes-0.1, summonRunes+0.15, t)
			if rk > 0 {
				mid := (summonR0+summonR1)/2 + 0.5
				for _, r := range runes {
					k := h * rk * (0.75 + 0.35*pixelfx.Noise(t*4+r.a*5, 8))
					ta0, ta1 := -math.Sin(r.a), math.Cos(r.a)
					na0, na1 := math.Cos(r.a), math.Sin(r.a)
					proj := func(pt [2]float64) (float64, float64) {
						u, v := pt[0], pt[1]
						return na0*mid + ta0*u*1.1 + na0*v, (na1*mid + ta1*u*1.1 + na1*v) * summonSY
					}
					col := C[4]
					if k > 0.9 {
						col = C[6]
					} else if k > 0.55 {
						col = C[5]
					}
					al := math.Min(1, k*1.5)
					for _, st := range r.g {
						for i := 1; i < len(st); i++ {
							x0, y0 := proj(st[i-1])
							x1, y1 := proj(st[i])
							p.Line(x0, y0, x1, y1, col, al)
						}
					}
				}
			}
			// the hole the brute climbs out of: a glowing gash
			hole := pixelfx.Smooth(summonBreak-0.05, summonBreak+0.15, t) * (1 - pixelfx.Smooth(summonBreak+0.95, summonBreak+1.5, t))
			if hole > 0 {
				hw := 15 * hole
				p.Shade(-hw-1, -1, hw+1, 4, func(x, y float64, out *pixelfx.Out) uint32 {
					d := pixelfx.Hypot(x/hw, (y-0.5)/3.2)
					if d > 1 {
						return 0
					}
					switch {
					case d > 0.86:
						out.C = C[4]
					case d > 0.62:
						out.C = C[1]
					default:
						out.C = C[0]
					}
					return 0
				})
				p.Glow(0, 0, 24, C[5], 0.3*hole*h)
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			r := rise(t)
			off := float64(summonBeast.h) * (1 - r)
			h := heat(t)
			fade := pixelfx.Smooth(summonFade+0.1, summonFade+0.8, t)
			if r > 0 {
				W, H := summonBeast.w, summonBeast.h
				x0, oy, k := -float64(W)/2, off-float64(H), math.Min(1, h)
				gone := func(x, y int) bool {
					return fade > 0 && fade > 0.06+0.86*(0.55*(1-float64(y)/float64(H))+0.45*pixelfx.Rand(float64(x), float64(y), 3))
				}
				// a dark edge all round, so the bulk reads against the night
				for _, pt := range summonBeast.outline {
					x, y := pt[0], pt[1]
					if float64(y)+oy < 0 && !gone(x, min(H-1, max(0, y))) {
						p.Px(x0+float64(x), float64(y)+oy, SH[0])
					}
				}
				for y := 0; y < H; y++ {
					ly := float64(y) + oy
					if ly >= 0 {
						break
					}
					// the fire below lights the undersides: rims strongest by the floor, broad planes higher up
					upRim, upPlane := math.Exp(ly/17)*k, math.Exp(ly/19)*k
					for x := 0; x < W; x++ {
						i := y*W + x
						pt := summonBeast.part[i]
						if pt == 0 || gone(x, y) {
							continue
						}
						ramp := SH
						if pt == 2 {
							ramp = HR
						}
						a := float64(summonBeast.amb[i])
						var si int
						switch {
						case summonBeast.crease[i] != 0:
							si = 0
						case a > 0.74:
							si = 4
						case a > 0.56:
							si = 3
						case a > 0.36:
							si = 2
						default:
							si = 1
						}
						if pt == 2 && si > 0 {
							add := 1
							if y < 3 {
								add = 2
							}
							si = min(4, si+add) // horns are paler, tips palest
						}
						// rims can run hot; the broad planes only ever warm up to deep red
						Lr := float64(summonBeast.rim[i]) * upRim
						Lp := float64(summonBeast.down[i]) * upPlane * 0.6
						c := ramp[si]
						switch {
						case Lr > 0.7:
							c = C[5]
						case Lr > 0.48:
							c = C[4]
						case Lr > 0.3 || Lp > 0.42:
							c = C[3]
						case Lr > 0.17 || Lp > 0.26:
							c = pixelfx.Mix(C[2], ramp[si], 0.3)
						case Lr > 0.08 || Lp > 0.12:
							c = pixelfx.Mix(ramp[si], C[1], 0.6)
						}
						p.Px(x0+float64(x), ly, c)
					}
				}
				// tusks catch the light; the eye sockets smoulder until the eyes open
				tuskCol := pixelfx.Mix(HR[4], C[4], 0.35*k)
				for _, pt := range summonBeast.tusks {
					x, y := pt[0], pt[1]
					if float64(y)+oy < 0 && !gone(x, y) {
						p.Px(x0+float64(x), float64(y)+oy, tuskCol)
					}
				}
				eyeCol := SH[0]
				if t > summonEyes-0.05 {
					if t < summonEyes+0.1 {
						eyeCol = pixelfx.White
					} else {
						eyeCol = C[6]
					}
				} else if t > summonBreak+0.5 {
					eyeCol = C[2]
				}
				for _, pt := range summonBeast.eyes {
					x, y := pt[0], pt[1]
					if float64(y)+oy < 0 && !gone(x, y) {
						p.Px(x0+float64(x), float64(y)+oy, eyeCol)
					}
				}
				// the eyes: a flare as they open, then a steady burn that outlasts the body
				if t > summonEyes-0.05 {
					ek := (1 - pixelfx.Smooth(summonFade+0.6, summonFade+1.0, t)) * (0.85 + 0.15*math.Sin(t*13))
					for _, gl := range summonBeast.glints {
						ex, ey := gl[0], gl[1]+off
						if ey >= 0 {
							continue
						}
						p.Glow(ex+0.5, ey, 6, C[5], 0.5*ek)
						g := (t - summonEyes) / 0.3
						if g > 0 && g < 1 {
							L := 9 * math.Pow(1-g, 1.5)
							p.LineFn(ex-L, ey, ex+L, ey, func(u float64) (uint32, float64) {
								d := math.Abs(u - 0.5)
								if d < 0.18 {
									return pixelfx.White, 1
								}
								if d < 0.35 {
									return C[6], 1
								}
								return C[5], 1
							})
							if g < 0.4 {
								p.Line(ex, ey-2*(1-g), ex, ey+2*(1-g), C[6], 1)
							}
						} else if fade > 0.6 && ek > 0.05 {
							p.Px(ex, ey, C[6])
						}
					}
				}
				// the light that rakes up its front
				p.Glow(0, -6, 30, C[4], 0.25*h*r)
			}
			rocks.Draw(p)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
