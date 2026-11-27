// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/bubbles.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the bubble shading (a lit rim top-left, a shaded rim bottom-right, a see-through body, a highlight and a refraction fleck, 2x2 and 1 px forms when small) and the surface line from Sprite Fusion's open-source bubbles (https://www.spritefusion.com/pixel-art-vfx/bubbles), MIT License, Copyright (c) 2026 Hugo Duprez.

package water

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Bubbles streams bubbles up from a vent of pebbles, wobbling and swelling as they rise, flaring as they cross shafts of light from the surface, then doming up and popping there. It is registered as "water/bubbles".
var Bubbles = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Bubbles",
		Desc:     "Bubbles stream up from a vent of pebbles, wobbling and swelling as they rise, flare as they cross shafts of light from the surface, then dome up and pop there.",
		Tags:     []string{"water", "bubbles", "underwater", "vent", "ambient", "loop"},
		Size:     [2]int{104, 112},
		Origin:   [2]int{52, 104},
		Duration: bubblesDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newBubbles,
}

func init() { pixelfx.Register("water/bubbles", Bubbles) }

const (
	bubblesDuration = 4
	bubblesTop      = -92
	bubblesHalf     = 48
)

var (
	bubblesMouths  = [3]float64{-5, 1, 6}
	bubblesPebbles = [6][3]float64{{-9, -1.5, 2.4}, {-4, -1, 1.8}, {3.5, -1.2, 2.2}, {9, -1.4, 2.6}, {0, -0.6, 1.4}, {-12.5, -0.8, 1.6}}
)

// bubblesSurfaceY is the surface: a wavy lit line, a darker band under it.
func bubblesSurfaceY(x, ph float64) float64 {
	return bubblesTop + math.Sin(x*0.13+ph*tau*2)*1.1 + math.Sin(x*0.051-ph*tau)*1.3
}

// bubblesShaft returns how much of a drifting light shaft from the surface a point is in.
func bubblesShaft(x, y, ph float64) float64 {
	u := x + (y-bubblesTop)*0.32
	s := math.Sin(u*0.16+ph*tau)*0.6 + math.Sin(u*0.07-ph*tau+1)*0.4
	return math.Max(0, s-0.35) / 0.65
}

// drawBubble draws one bubble at (x, y), radius r, squash q: tiny ones a pixel or a shaded 2x2, bigger ones a lit rim round a see-through body with a highlight and a fleck of light refracted through it.
func drawBubble(p *pixelfx.Painter, C []uint32, x, y, r, q, lit float64) {
	if r < 0.9 {
		c := C[6]
		if lit > 0.3 {
			c = pixelfx.White
		} else if r < 0.7 {
			c = C[5]
		}
		p.Px(x, y, c)
		return
	}
	if r < 1.5 {
		X, Y := math.Floor(x), math.Floor(y)
		p.Px(X, Y, pixelfx.White)
		p.Px(X+1, Y, C[6])
		p.Px(X, Y+1, C[5])
		p.Px(X+1, Y+1, C[4])
		p.Glow(x+0.5, y+0.5, 3, C[5], 0.12+0.2*lit)
		return
	}
	rx, ry := r*(1+q), r*(1-q)
	p.Shade(x-rx-1, y-ry-1, x+rx+1, y+ry+1, func(px, py float64, out *pixelfx.Out) uint32 {
		dx, dy := (px-x)/rx, (py-y)/ry
		d := pixelfx.Hypot(dx, dy) * r
		if d > r+0.5 {
			return 0
		}
		l := (-dx - dy) / sqrt2 / math.Max(0.01, d/r)
		if d > r-0.6 {
			if l > 0.45 {
				out.C = C[6]
			} else if l < -0.55 {
				out.C = C[3]
			} else {
				out.C = C[4]
			}
		} else {
			p.Blend(px, py, C[5], 0.16+0.2*lit)
		}
		return 0
	})
	p.Px(pixelfx.Round(x-rx*0.45), pixelfx.Round(y-ry*0.45), pixelfx.White)
	if r >= 2.5 {
		p.Px(pixelfx.Round(x-rx*0.45)+1, pixelfx.Round(y-ry*0.45), C[6])
	}
	if r >= 2 {
		p.Px(pixelfx.Round(x+rx*0.5), pixelfx.Round(y+ry*0.5), C[6])
	}
	p.Glow(x, y, r*2.4+2, C[5], 0.12+0.3*lit)
}

type risingBubble struct {
	x, y, r, q, age, vy float64
	i                   int
}

// eachBubble runs the bubbles from the vent's three mouths: each rises faster the bigger it is, swells as the pressure drops, wobbles side to side, and pops at the surface.
func eachBubble(t, S float64, fn func(b risingBubble)) {
	pixelfx.Cycle(t, bubblesDuration, 46, S, func(g *pixelfx.Rng) float64 { return g.Range(2.2, 3.4) }, func(age, _ float64, r *pixelfx.Rng, i int) {
		m := bubblesMouths[i%3]
		var r0 float64
		if r.Next() < 0.7 {
			r0 = r.Range(0.5, 1.4)
		} else {
			r0 = r.Range(1.6, 2.6)
		}
		vy := 30 + 9*r0 + r.Range(-4, 4)
		wf := r.Range(3, 6)
		wa := 0.8 + r0*1.2
		ph0 := r.Range(0, tau)
		drift := r.Range(-4, 4)
		y := -2 - vy*age
		k := pixelfx.Clamp(-y / -bubblesTop)
		x := m + drift*k + math.Sin(age*wf+ph0)*wa*k
		fn(risingBubble{
			x: x, y: y, r: r0 * (1 + 0.45*k),
			q: 0.12 * math.Sin(age*wf*2+ph0) * math.Min(1, r0/2),
			i: i, age: age, vy: vy,
		})
	})
}

func newBubbles(o *pixelfx.Options) pixelfx.Instance {
	C, S := pixelfx.Fit(o.Palette(), 8), o.Seed()
	D := float64(bubblesDuration)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			// the water: deeper toward the floor, light falling through it in shafts
			for Y := bubblesTop - 2; Y < 0; Y++ {
				for X := -bubblesHalf; X < bubblesHalf; X++ {
					x, y := float64(X)+0.5, float64(Y)+0.5
					sy := bubblesSurfaceY(x, ph)
					if y < sy {
						continue
					}
					e := 1 - pixelfx.Smooth(bubblesHalf-14, bubblesHalf, math.Abs(x))
					depth := (y - bubblesTop) / -bubblesTop
					p.Blend(x, y, C[1], (0.22+0.22*depth)*e)
					s := bubblesShaft(x, y, ph) * (1 - depth*0.7) * e
					if s > 0.05 {
						p.Light(x, y, C[4], 0.2*s)
					}
				}
			}
			// where the shafts reach the floor, caustic light dances on it
			for X := -bubblesHalf; X < bubblesHalf; X++ {
				x := float64(X) + 0.5
				e := 1 - pixelfx.Smooth(bubblesHalf-14, bubblesHalf, math.Abs(x))
				s := bubblesShaft(x, -2, ph)
				c := math.Max(0, 1-math.Abs(math.Sin(x*0.42+math.Sin(x*0.11+ph*tau*2)*2.2))*2.4)
				if s*c*e > 0.05 {
					p.Light(x, -1, C[5], 0.5*s*c*e)
					p.Light(x, -2, C[4], 0.25*s*c*e)
					p.Light(x, 0, C[4], 0.3*s*c*e)
				}
			}
			// the vent: a little heap of pebbles round a dark mouth
			ST := rock()
			for _, pb := range bubblesPebbles {
				x, y, rr := pb[0], pb[1], pb[2]
				p.Shade(x-rr-1, y-rr-1, x+rr+1, y+rr+1, func(px, py float64, out *pixelfx.Out) uint32 {
					dx, dy := px-x, py-y
					d := pixelfx.Hypot(dx*1.1, dy)
					if d > rr+0.7 || py > 0 {
						return 0
					}
					switch {
					case d > rr:
						out.C = ST[0]
					case dx+dy < -rr*0.6:
						out.C = ST[5]
					case dx+dy < rr*0.3:
						out.C = ST[3]
					default:
						out.C = ST[2]
					}
					return 0
				})
			}
			for _, m := range bubblesMouths {
				p.Px(m, -1, ST[0])
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			// specks drifting in the water, flaring in the light
			pixelfx.Cycle(t, D, 26, S+7, pixelfx.Fixed(D), func(age, _ float64, r *pixelfx.Rng, _ int) {
				x := r.Range(-bubblesHalf+6, bubblesHalf-6) + math.Sin((age/D)*tau+r.Range(0, tau))*3
				y := bubblesTop + 6 + math.Mod(r.Range(0, 86)+age*4, 86)
				s := bubblesShaft(x, y, ph)
				tw := math.Sin((age/D)*tau*3 + r.Range(0, tau))
				if tw < -0.3 && s < 0.2 {
					return
				}
				c := C[4]
				if s > 0.4 {
					c = pixelfx.White
				} else if tw > 0.6 {
					c = C[5]
				}
				p.Px(x, y, c)
				if s > 0.4 {
					p.Glow(x, y, 3, C[5], 0.25*s)
				}
			})
			// the bubbles; when one reaches the surface it domes up, then pops with a ring and a couple of flecks
			eachBubble(t, S, func(b risingBubble) {
				sy := bubblesSurfaceY(b.x, ph)
				if b.y > sy+b.r {
					drawBubble(p, C, b.x, b.y, b.r, b.q, bubblesShaft(b.x, b.y, ph))
				} else {
					since := (sy + b.r - b.y) / b.vy
					if since < 0.12 && b.r >= 1.4 {
						for a := pi; a <= tau+0.01; a += 0.4 / b.r {
							p.Px(b.x+math.Cos(a)*b.r, sy+math.Sin(a)*b.r*0.9, C[6])
						}
						p.Px(b.x-b.r*0.4, sy-b.r*0.6, pixelfx.White)
					} else {
						popEnd := 0.2
						if b.r >= 1.4 {
							popEnd = 0.32
						}
						if since < popEnd {
							off := 0.0
							if b.r >= 1.4 {
								off = 0.12
							}
							k := pixelfx.Clamp((since - off) / 0.2)
							w := math.Max(1, b.r+1+k*4)
							c := C[5]
							if k < 0.4 {
								c = pixelfx.White
							}
							for a := 0.0; a < pi; a += 0.5 / w {
								if math.Sin(a) > 0.15 {
									p.Dot(b.x+math.Cos(a)*w, sy+math.Sin(a)*w*0.3, c, 1.2-k)
								}
							}
							if b.r >= 1.4 && k < 0.6 {
								p.Px(b.x-2, sy-2-k*4, C[6])
								p.Px(b.x+2, sy-3-k*5, C[6])
							}
						}
					}
				}
			})
			// the surface: a lit line with glints running along it, a darker band under it
			for X := -bubblesHalf; X < bubblesHalf; X++ {
				x := float64(X) + 0.5
				y := bubblesSurfaceY(x, ph)
				e := 1 - pixelfx.Smooth(bubblesHalf-12, bubblesHalf, math.Abs(x))
				glint := math.Sin(x*0.37-ph*tau*3)*math.Sin(x*0.13+ph*tau) > 0.55
				c := C[5]
				if glint {
					c = pixelfx.White
				}
				p.Dot(float64(X), y, c, e*1.3)
				p.Dot(float64(X), y+1, C[3], e*1.3)
				if X&3 == 0 {
					gk := 0.1
					if glint {
						gk = 0.22
					}
					p.Glow(x, y+1, 7, C[5], gk*e)
				}
			}
		},
	}
}
