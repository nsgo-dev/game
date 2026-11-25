// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/toxic-puddle.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// ToxicPuddle is black tar lit from inside by chartreuse veins and a glowing rim, with an oily sheen, glossy popping bubbles and curling vapour ribbons. It is registered as "nature/toxic-puddle".
var ToxicPuddle = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Toxic puddle",
		Desc:     "Black tar lit from inside by chartreuse veins and a glowing rim, an oily sheen drifting on it; glossy bubbles dome, stretch and pop, vapour curls up in fraying ribbons.",
		Tags:     []string{"nature", "poison", "toxic", "sludge", "tar", "puddle", "hazard", "ground", "loop"},
		Size:     [2]int{176, 100},
		Origin:   [2]int{88, 86},
		Duration: toxicPuddleDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#06070a", "#0c100c", "#141a10", "#1e2814", "#2e3c18", "#5a7a1c", "#a6d42a", "#ecff86")},
			{Name: "sheen", Default: pixelfx.Palette("#2a1648", "#5a2e8e", "#1e7a86", "#6adccc")},
			{Name: "width", Default: pixelfx.Num(42)},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newToxicPuddle,
}

func init() { pixelfx.Register("nature/toxic-puddle", ToxicPuddle) }

const toxicPuddleDuration = 4.0

var toxicVapourGrey = pixelfx.Hex("#b4b8ae")

// puddleSurf returns the surface height above the floor at x: a heavy lens of tar, slow swells rolling through it.
func puddleSurf(x, W, t float64) float64 {
	d := math.Min(1, math.Abs(x)/W)
	ph := (tau * t) / toxicPuddleDuration
	lens := math.Sqrt(1-d*d) * 6.6
	if d > 0.8 {
		lens += 1.6 * pixelfx.Smooth(0.8, 0.9, d)
	}
	swell := math.Pow(math.Max(0, math.Sin(x*0.09-ph)), 6)*1.2 + math.Pow(math.Max(0, math.Sin(x*0.06+ph*2+1)), 8)*0.9
	return -pixelfx.Round(lens + swell*(1-d))
}

// drawPuddlePool draws the oily tar pool, internal glowing veins and chartreuse rim.
func drawPuddlePool(p *pixelfx.Painter, C, Sh []uint32, W, t, seed float64) {
	ph := t / toxicPuddleDuration
	for x := math.Floor(-W - 1); x <= math.Ceil(W+1); x++ {
		d := math.Abs(x+0.5) / W
		if d > 1.03 {
			continue
		}
		top := puddleSurf(x+0.5, W, t)
		oil := 0.6*pixelfx.TileNoise(x*0.08, ph*4, 0, 4, seed) + 0.6*pixelfx.TileNoise(x*0.11-ph*4, 1, 4, 0, seed+1)
		hue := pixelfx.TileNoise(x*0.05, ph*2+7, 0, 2, seed+3)
		for y := top; y <= 2; y++ {
			if (y == 2 && d > 0.75) || (y == 1 && d > 0.9) {
				continue
			}
			var c uint32
			switch {
			case d > 0.93 && y <= 0:
				if y == top {
					c = C[7]
				} else {
					c = C[6]
				}
			case y == top:
				// the skin catches light along its top: a lit edge, the sheen sliding over it, specular glints running along
				glint := math.Abs(math.Mod(math.Mod(x*0.07-ph*3, 1)+1, 1)-0.5) < 0.012 || pixelfx.Rand(x, math.Floor(t*10)) < 0.015
				switch {
				case glint:
					c = pixelfx.White
				case oil > 0.55:
					if hue > 0 {
						c = Sh[3]
					} else {
						c = Sh[2]
					}
				case oil > 0.15:
					if hue > 0 {
						c = Sh[1]
					} else {
						c = C[5]
					}
				default:
					c = C[5]
				}
				if glint {
					p.Light(x, y, C[7], 0.5)
				}
			case y == top+1:
				if oil > 0.4 {
					c = Sh[0]
				} else {
					c = C[3]
				}
			case y >= 1:
				c = C[0]
			default:
				// cracks of light under the crust, writhing slowly and swelling brighter and dimmer in a slow pulse
				pulse := 0.5 + 0.5*math.Sin((tau*t)/toxicPuddleDuration+x*0.03)
				v := math.Abs(pixelfx.TileNoise(x*0.09, y*0.75+ph*4, 0, 4, seed+7) + 0.2*math.Sin((tau*t*2)/toxicPuddleDuration+x*0.15))
				w := 0.6 + 0.6*pulse
				switch {
				case v < 0.035*w:
					c = C[7]
				case v < 0.08*w:
					c = C[6]
				case v < 0.14*w:
					c = C[4]
				default:
					c = C[1]
				}
				if v < 0.08*w {
					p.Light(x, y, C[6], 0.25+0.3*pulse)
				}
			}
			p.Px(x, y, c)
		}
		if d > 0.9 && d <= 1.03 {
			p.Px(x, top-1, C[1])
		}
	}
	// the glow bleeding out onto the floor at both lips
	for _, s := range [2]float64{-1, 1} {
		for k := 1.0; k <= 6; k++ {
			col := C[5]
			if k < 3 {
				col = C[6]
			}
			p.Dot(s*(W+k), 0, col, 1-k/7)
		}
	}
}

// drawPuddleRibbon draws vapour: a thin ribbon that curls up off the tar, then frays into wisps that wander apart and fade.
func drawPuddleRibbon(p *pixelfx.Painter, C []uint32, x0, y0, age, life float64, R *pixelfx.Rng, k float64) {
	u := age / life
	length := R.Range(16, 26)
	rise := R.Range(9, 14)
	amp := R.Range(2, 4.5)
	w := R.Range(0.18, 0.3)
	ph := R.Range(0, tau)
	head := rise * age
	tail := math.Max(0, head-length)
	fray := pixelfx.Smooth(0.35, 0.85, u)
	fade := pixelfx.Smooth(0, 0.12, u) * (1 - pixelfx.Smooth(0.5, 1, u)) * k
	if fade < 0.02 {
		return
	}
	n := 1
	if fray > 0.05 {
		n = 3
	}
	for h := tail; h <= head; h += 0.5 {
		s := (h - tail) / math.Max(1, head-tail)
		cx := x0 + amp*math.Sin(h*w+ph+age*1.6)*(0.3+s)
		y := y0 - h
		a := fade * math.Sin(pi*s) * 0.45
		// lit from below by the pool: green and bright low down, greying as it climbs and frays
		lit := math.Max(0, 1-h/22) * (1 - 0.6*fray)
		VAP := pixelfx.Mix(toxicVapourGrey, C[6], 0.75*lit)
		for q := 0; q < n; q++ {
			// each wisp wanders off on its own slow curl as the ribbon frays
			off := 0.0
			alphaMul := 1.0
			if n != 1 {
				off = fray*s*float64(q-1)*3 + fray*2.5*math.Sin(h*(0.35+float64(q)*0.17)+ph*float64(q+1)+age*2)
				alphaMul = 0.65 - 0.25*fray
			}
			p.Blend(cx+off, y, VAP, a*alphaMul*(0.8+0.4*lit))
		}
	}
}

// drawPuddleBubbles draws fat glossy bubbles that dome, stretch, pop in a ripple ring and spatter tar, releasing a vapour ribbon.
func drawPuddleBubbles(p *pixelfx.Painter, C, Sh []uint32, W, t, seed float64) {
	pixelfx.Cycle(t, toxicPuddleDuration, 8, seed, func(g *pixelfx.Rng) float64 { return g.Range(2.4, 3.2) }, func(age, L float64, R *pixelfx.Rng, _ int) {
		swell := R.Range(1, 1.6)
		x := R.Range(-0.7, 0.7) * W
		rmax := R.Range(4.5, 8)
		sy := puddleSurf(x, W, t) + 0.5
		if age < swell {
			k := age / swell
			st := pixelfx.Smooth(0.65, 1, k)
			gr := pixelfx.Ease.OutCubic(math.Min(1, k*1.3))
			rx := rmax * gr * (1 - 0.18*st)
			ry := rmax * gr * (1 + 0.5*st)
			X, Y := pixelfx.Round(x), pixelfx.Round(sy)
			inner := 0.35 + 0.65*k
			for j := -math.Ceil(ry) - 1; j <= 0; j++ {
				for i := -math.Ceil(rx) - 1; i <= math.Ceil(rx)+1; i++ {
					dd := pixelfx.Hypot(i/math.Max(0.6, rx), j/math.Max(0.6, ry))
					if dd > 1.1 {
						continue
					}
					// a dark glossy skin with chartreuse light glowing up through it, strongest low in the middle
					var c uint32
					switch {
					case dd > 0.9:
						c = C[0]
					case dd < 0.55*inner && j > -ry*0.75:
						if dd < 0.3*inner {
							c = C[6]
						} else {
							c = C[5]
						}
					case dd < 0.8*inner:
						c = C[4]
					default:
						if j < -ry*0.5 {
							c = C[2]
						} else {
							c = C[3]
						}
					}
					p.Px(X+i, Y+j, c)
				}
			}
			// the gloss: a curved band of sheen on its upper left, a bright highlight
			if rx > 1.8 {
				for a := 3.5; a < 4.7; a += 0.16 {
					col := Sh[1]
					if a < 3.9 {
						col = Sh[3]
					} else if a < 4.3 {
						col = C[7]
					}
					p.Px(X+math.Cos(a)*rx*0.68, Y+math.Sin(a)*ry*0.68, col)
				}
			}
			if rx > 1.2 {
				hx, hy := X-pixelfx.Round(rx*0.35), Y-pixelfx.Round(ry*0.62)
				p.Px(hx, hy, pixelfx.White)
				p.Px(hx+1, hy, C[7])
			}
			p.Glow(x, sy-ry*0.5, 7+rx*1.4, C[6], (0.2+0.35*st)*gr)
			return
		}
		e := age - swell
		// the pop: a ring rippling out flat across the tar, a flash of the light inside, spatter
		if e < 0.3 {
			k := e / 0.3
			rr := 2 + 14*pixelfx.Ease.OutCubic(k)
			col := C[6]
			if k < 0.4 {
				col = pixelfx.White
			} else if k < 0.7 {
				col = C[7]
			}
			for a := 0.0; a < tau; a += 0.5 / rr {
				rx2 := x + math.Cos(a)*rr
				ry2 := sy + math.Sin(a)*rr*0.22
				if math.Abs(rx2) < W*0.95 {
					p.Dot(rx2, ry2, col, (1-k)*1.4)
				}
			}
			p.GlowRing(x, sy, rr, 3, C[6], 0.35*(1-k))
			if e < 0.1 {
				p.Glow(x, sy-2, 18, C[6], 0.7*(1-e/0.1))
			}
		}
		for j := 0; j < 7; j++ {
			vx := (pixelfx.Rand(float64(j), seed, pixelfx.Round(x*10)) - 0.5) * 80
			vy := -45 - pixelfx.Rand(float64(j), seed+3, pixelfx.Round(x*10))*70
			dx, dy := pixelfx.Fly(x, sy-2, vx, vy, e, 420, 0)
			if dy < sy && e < 0.6 {
				p.Px(dx, dy, C[1])
				col := C[6]
				if j&1 != 0 {
					col = Sh[3]
				}
				p.Px(dx, dy-1, col)
			}
		}
		drawPuddleRibbon(p, C, x, sy-2, e, L-swell, R, 0.9)
	})
}

func newToxicPuddle(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	Sh := pixelfx.Fit(o.Ramp("sheen"), 4)
	W := o.Num("width")
	seed := o.Seed()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			pulse := 0.85 + 0.15*math.Sin((tau*t*2)/toxicPuddleDuration)
			// the light comes from inside the pool and its rim
			p.Glow(0, -3, W*1.25, C[5], 0.22*pulse)
			p.Glow(0, -10, W*1.1, C[6], 0.1*pulse)
			for _, s := range [2]float64{-1, 1} {
				p.Glow(s*W, -1, 12, C[6], 0.35*pulse)
			}
			drawPuddlePool(p, C, Sh, W, t, seed)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			drawPuddleBubbles(p, C, Sh, W, t, seed*7+1)
			// a steady breath of vapour ribbons off the whole pool
			pixelfx.Cycle(t, toxicPuddleDuration, 9, seed*13+4, func(g *pixelfx.Rng) float64 { return g.Range(2.4, 3.4) }, func(age, L float64, R *pixelfx.Rng, _ int) {
				x := R.Range(-0.85, 0.85) * W
				drawPuddleRibbon(p, C, x, puddleSurf(x, W, t)-1, age, L, R, 0.75)
			})
		},
	}
}
