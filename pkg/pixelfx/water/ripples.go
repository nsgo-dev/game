// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/ripples.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package water

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Ripples drops plinks on still dark water and spreads rings across it at a low angle: the swell of each ring catches the light, crests glint, and they fade into the calm. It is registered as "water/ripples".
var Ripples = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ripples",
		Desc:     "A drop plinks on still dark water and rings spread out across it at a low angle: the swell of each ring catches the light, crests glint, and they fade into the calm.",
		Tags:     []string{"water", "ripples", "rings", "surface", "pond", "ambient", "loop"},
		Size:     [2]int{136, 52},
		Origin:   [2]int{68, 30},
		Duration: ripplesDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newRipplesEffect,
}

func init() { pixelfx.Register("water/ripples", Ripples) }

const (
	ripplesDuration = 2
	ripplesSY       = 0.26
	ripplesEvery    = 0.5
	ripplesLife     = 2
	ripplesRMax     = 60
	ripplesPatch    = 64
)

func ripplesRadius(a float64) float64 {
	return 2 + (ripplesRMax-2)*pixelfx.Ease.OutCubic(a/ripplesLife)
}

func ripplesPower(a float64) float64 {
	return math.Pow(1-a/ripplesLife, 1.5) * math.Min(1, a/0.08)
}

type aliveRipple struct {
	a, i float64
}

func ripplesAlive(t float64) []aliveRipple {
	var out []aliveRipple
	for k := 0.0; k*ripplesEvery < ripplesLife; k++ {
		a := pixelfx.Wrap(t-k*ripplesEvery, ripplesDuration)
		if a < ripplesLife {
			out = append(out, aliveRipple{a: a, i: pixelfx.Round(pixelfx.Wrap(t-a, ripplesDuration) / ripplesEvery)})
		}
	}
	return out
}

// ripplesSurface draws dark see-through water shading with the slope of the swell each ring carries, so the rings read as waves catching the light from the far left, not just lines.
func ripplesSurface(p *pixelfx.Painter, C []uint32, t float64) {
	all := ripplesAlive(t)
	rs := make([][3]float64, len(all))
	for i, q := range all {
		rs[i] = [3]float64{ripplesRadius(q.a), ripplesPower(q.a), 3 + q.a*2.2}
	}
	limitY := float64(ripplesPatch) * ripplesSY
	for Y := math.Floor(-limitY); Y < limitY; Y++ {
		for X := -ripplesPatch; X < ripplesPatch; X++ {
			x, y := float64(X)+0.5, Y+0.5
			z := y / ripplesSY
			r := pixelfx.Hypot(x, z)
			e := r / ripplesPatch
			if e > 1 {
				continue
			}
			edge := 1 - pixelfx.Smooth(0.6, 1, e)
			p.Blend(x, y, C[1], 0.55*edge)
			slope := 0.0
			for _, rq := range rs {
				R, k, w := rq[0], rq[1], rq[2]
				d := r - R
				if d > 2.2*w || d < -2.2*w {
					continue
				}
				dw := d / w
				slope += k * math.Exp(-(dw * dw)) * -math.Sin(d*1.15) * 1.15
			}
			if slope == 0 {
				continue
			}
			// the side of a swell facing the light (back and left) catches it; the side away from it goes dark
			face := slope * (-(x*-0.55 + z*-0.84) / math.Max(1, r))
			if face > 0.18 {
				c := C[4]
				if face > 0.6 {
					c = C[5]
				}
				p.Blend(x, y, c, math.Min(0.9, (face-0.18)*2.2)*edge)
			} else if face < -0.25 {
				p.Blend(x, y, C[0], math.Min(0.8, (-face-0.25)*2)*edge)
			}
		}
	}
}

func drawRipplesRings(p *pixelfx.Painter, C []uint32, t float64, half int) {
	for _, q := range ripplesAlive(t) {
		R, k := ripplesRadius(q.a), ripplesPower(q.a)
		glint := 0.0
		if half > 0 && q.a > 0.1 {
			glint = k
		}
		ring(p, 0, 0, R, ripplesSY, 0.85*k, C, ringOpts{n: 2, gap: 3 + q.a*1.4, half: half, hot: q.a < 0.14, glint: glint, seed: q.i + 1, wob: 0.4 + q.a*1.1})
	}
}

func newRipplesEffect(o *pixelfx.Options) pixelfx.Instance {
	C, S := pixelfx.Fit(o.Palette(), 8), o.Seed()
	D := float64(ripplesDuration)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ripplesSurface(p, C, t)
			drawRipplesRings(p, C, t, -1)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			drawRipplesRings(p, C, t, 1)
			// each ring starts with a drop: a white fleck, a bead thrown up and back down, a little light
			for _, q := range ripplesAlive(t) {
				a := q.a
				if a < 0.08 {
					p.Px(0, 0, pixelfx.White)
					p.Px(-1, 0, C[6])
					p.Px(1, 0, C[6])
					p.Glow(0, 0, 8, C[5], 0.5*(1-a/0.08))
				}
				b := a - 0.04
				h := 90*b - 0.5*700*b*b
				if b > 0 && h > 0 {
					p.Px(0, -h, pixelfx.White)
					p.Px(0, -h+1, C[5])
				}
			}
			// glints ride the near crests, winking
			pixelfx.Cycle(t, D, 10, S+5, func(g *pixelfx.Rng) float64 { return g.Range(0.25, 0.45) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				all := ripplesAlive(t)
				idx := int(math.Floor(r.Next() * float64(len(all))))
				a := 0.0
				if idx < len(all) {
					a = all[idx].a
				}
				ang := r.Range(0.3, pi-0.3)
				R := ripplesRadius(a)
				k := math.Sin((pi*age)/life) * ripplesPower(a)
				if k < 0.15 || R < 6 {
					return
				}
				x, y := math.Cos(ang)*R, math.Sin(ang)*R*ripplesSY
				p.Px(x, y, pixelfx.White)
				if k > 0.5 {
					for _, d := range cross4 {
						p.Px(x+d[0], y+d[1], C[6])
					}
				}
				p.Glow(x, y, 5, C[5], 0.4*k)
			})
			// a cool light on the water, brightest where the rings start
			p.Glow(0, 0, 40, C[4], 0.14)
			p.Glow(0, 0, 14, C[5], 0.12)
		},
	}
}
