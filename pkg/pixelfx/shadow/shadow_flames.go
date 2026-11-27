// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/shadow-flames.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the tongues built from nested layers, each narrower and shorter than the last, from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez. Here the layers run the other way: a pale skin outside, violet within, a black heart at the core.

package shadow

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// ShadowFlames burns inside-out fire on the floor: a black heart wrapped in violet flame with pale skin and tips, shedding embers and dark smoke. It is registered as "shadow/shadow-flames".
var ShadowFlames = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Shadow flames",
		Desc:     "Inside-out fire on the floor: a black heart wrapped in violet flame with pale skin and tips, shedding embers and dark smoke.",
		Tags:     []string{"shadow", "fire", "flame", "ground", "dark", "magic", "loop"},
		Size:     [2]int{104, 112},
		Origin:   [2]int{52, 102},
		Duration: 2.4,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "seed", Default: pixelfx.Num(12)},
		},
	},
	New: newShadowFlames,
}

func init() { pixelfx.Register("shadow/shadow-flames", ShadowFlames) }

const (
	sfD  = 2.4
	sfGX = 22
	sfGY = 60
)

type sfTongue struct {
	dx, h0, w, n float64
}

type sfLayer struct {
	lw, lh float64
	tone   int8
}

var (
	// tongues: offset, base height, width, seed; the middle tallest
	sfTongues = [5]sfTongue{
		{-11, 20, 4.2, 1},
		{11, 22, 4.4, 2},
		{-6, 31, 5.4, 3},
		{6, 29, 5.2, 4},
		{0, 40, 6.6, 5},
	}
	// layers from the outside in: width share, height share, which tone (3 is the black heart)
	sfLayers = [4]sfLayer{
		{1, 1, 0},
		{0.8, 0.88, 1},
		{0.54, 0.68, 2},
		{0.32, 0.48, 3},
	}
	sfOrder = func() [5]int {
		ord := []int{0, 1, 2, 3, 4}
		slices.SortStableFunc(ord, func(a, b int) int {
			return cmp.Compare(math.Abs(sfTongues[b].dx), math.Abs(sfTongues[a].dx))
		})
		return [5]int(ord)
	}()
)

// sfLNoise is noise that comes back to where it started each loop: a circle through 2D noise, offset by v.
func sfLNoise(t, v, seed, speed float64) float64 {
	return pixelfx.Noise2(math.Cos((tau*t)/sfD)*speed+v, math.Sin((tau*t)/sfD)*speed, seed)
}

func newShadowFlames(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	S := o.Seed()
	toneCols := [4]uint32{C[6], C[4], C[2], C[0]}
	smokeBase := pixelfx.Mix(C[3], C[2], 0.5)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			// the scorched floor: a dark burn, violet light pooled on it
			p.EllipseFn(0, 0.5, 22, 3.2, func(d, _ float64) (uint32, float64) {
				a := 1.0
				if d >= 0.85 {
					a = (1 - d) * 6
				}
				return pick(d < 0.6, C[0], C[1]), a
			})
			p.Glow(0, -2, 46, C[4], 0.3)
			// dark smoke climbing off the tips, a lit rim on its underside
			pixelfx.Cycle(t, sfD, 14, S+1, func(r *pixelfx.Rng) float64 { return r.Range(1.2, 1.9) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				u := age / L
				x := r.Range(-6, 6) + math.Sin(u*4+r.Range(0, 6))*5*u
				y := -30 - u*r.Range(36, 52)
				rad := 3 + u*6
				a := math.Min(1, u*6) * (1 - u) * 0.7
				p.Disc(x, y, rad, pixelfx.Mix(smokeBase, C[1], u), a)
				p.Disc(x-rad*0.2, y-rad*0.3, rad*0.5, C[3], a*0.4)
				p.Disc(x, y+rad*0.45, rad*0.55, C[5], a*0.45*(1-u))
			})
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			// rasterize the tongues, outermost first, into a grid of tones
			var grid [sfGX * 2 * sfGY]int8
			for i := range grid {
				grid[i] = -1
			}
			set := func(x, y float64, v int8) {
				i, j := int(pixelfx.Round(x))+sfGX, int(pixelfx.Round(y))+sfGY
				if i >= 0 && j >= 0 && i < sfGX*2 && j < sfGY {
					grid[j*sfGX*2+i] = v
				}
			}
			for _, idx := range sfOrder {
				tg := sfTongues[idx]
				h := tg.h0 * (0.78 + 0.22*sfLNoise(t, tg.n*3, tg.n, 4))
				sway := sfLNoise(t, tg.n*7, tg.n+9, 3) * 3
				for _, l := range sfLayers {
					Hl := h * l.lh
					for r := 0.0; r <= Hl; r++ {
						u := r / math.Max(1, Hl)
						half := tg.w*l.lw*math.Pow(1-u, 0.85)*math.Min(1, (r+1.5)/3.5) + sfLNoise(t, r*0.45+tg.n, tg.n+20, 6)*1.1 + sfLNoise(t, r*1.3+tg.n*5, tg.n+50, 9)*0.6
						cx := tg.dx + sway*u*u*l.lh + sfLNoise(t, r*0.2+tg.n*2, tg.n+30, 5)*u*1.4
						if half < 0.3 {
							continue
						}
						for x, x1 := pixelfx.Round(cx-half), pixelfx.Round(cx+half); x <= x1; x++ {
							// the skin breaks up near the top: pixels flicker out of it
							if l.tone == 0 && u > 0.45 && sfLNoise(t, x*0.9+r*0.7+tg.n, tg.n+60, 8) > 0.35 {
								continue
							}
							set(x, -r, l.tone)
						}
					}
				}
			}
			// draw it: the pale skin whitening to its tips, the black heart pure black
			for j := 0; j < sfGY; j++ {
				for i := 0; i < sfGX*2; i++ {
					v := grid[j*sfGX*2+i]
					if v < 0 {
						continue
					}
					x, y := float64(i-sfGX), float64(j-sfGY)
					c := toneCols[v]
					if v == 0 && y < -24 && (j == 0 || grid[(j-1)*sfGX*2+i] < 0) {
						c = pick(y < -32, pixelfx.White, C[7])
					}
					p.Px(x+0.5, y+0.5, c)
				}
			}
			// licks of pale flame breaking off the tips
			pixelfx.Cycle(t, sfD, 34, S, func(r *pixelfx.Rng) float64 { return r.Range(0.25, 0.5) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				u := age / L
				k := int(r.Int(0, 4))
				tg := sfTongues[k]
				x := tg.dx + r.Range(-4, 4) + u*r.Range(-5, 5)
				y := -tg.h0*r.Range(0.45, 0.85) - u*r.Range(10, 20)
				rad := r.Range(1, 2.2)*(1-u) + 0.3
				p.Circle(x, y, rad+0.6, pick(u < 0.5, C[6], C[4]), 1)
				if rad > 1 {
					p.Circle(x, y+0.4, rad*0.5, pick(u < 0.3, C[2], C[4]), 1)
				}
			})
			// embers drifting up, flickering, cooling to violet
			pixelfx.Cycle(t, sfD, 30, S+2, func(r *pixelfx.Rng) float64 { return r.Range(1, 1.8) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				u := age / L
				x := r.Range(-12, 12) + math.Sin(u*6+r.Range(0, 6))*5*u
				y := -10 - u*r.Range(40, 70)
				if u > 0.7 && math.Sin(age*40+x) < 0 {
					return
				}
				c := C[6]
				if u < 0.3 {
					c = pixelfx.White
				} else if u < 0.6 {
					c = C[7]
				}
				p.Px(x, y, c)
				p.Glow(x, y, 3, C[6], 0.35*(1-u))
			})
			// light: a violet bloom round it, but none on the black heart
			fl := 0.85 + 0.15*sfLNoise(t, 0, 77, 6)
			p.Glow(0, -16, 52, C[4], 0.5*fl)
			p.Glow(0, -12, 26, C[6], 0.34*fl)
			for j := 0; j < sfGY; j++ {
				for i := 0; i < sfGX*2; i++ {
					if grid[j*sfGX*2+i] == 3 {
						x, y := float64(i-sfGX)+0.5, float64(j-sfGY)+0.5
						sx, sy := p.CellX(x, y)-p.OX, p.CellY(x, y)-p.OY
						if sx >= 0 && sy >= 0 && sx < p.W && sy < p.H {
							p.L[sy*p.W+sx] = 0
						}
					}
				}
			}
		},
	}
}
