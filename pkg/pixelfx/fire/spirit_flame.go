// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/spirit-flame.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the layered tongue, its noise-torn edges and the shed puffs from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// SpiritFlame is a cold blue-teal ghost fire floating in the air: a bright rim round a hollow dark heart, a wave climbing its wispy tail, curls breaking off, cold sparks rising. It is registered as "fire/spirit-flame".
var SpiritFlame = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Spirit flame",
		Desc:     "A cold blue-teal ghost fire floating in the air: a bright rim round a hollow dark heart, a wave climbing its wispy tail, curls breaking off, cold sparks rising.",
		Tags:     []string{"fire", "spirit", "ghost", "magic", "cold", "ambient", "loop"},
		Size:     [2]int{80, 124},
		Origin:   [2]int{40, 94},
		Duration: spiritDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#040a16", "#08203a", "#0b3a52", "#0f5e6e", "#16908e", "#2cc4b4", "#7ef0dc", "#c8fff2", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	New: newSpiritFlame,
}

func init() { pixelfx.Register("fire/spirit-flame", SpiritFlame) }

const spiritDuration = 3

// ghostLayers are inverted layers: the rim burns brightest, a teal band inside it, a dark hollow heart, a pale core low down.
var ghostLayers = []layer{{1, 1, 1}, {0.8, 0.92, 3}, {0.6, 0.78, 6}, {0.3, 0.42, 2}, {0.14, 0.22, 0}}

// ghostTongue draws a tongue whose centre line carries a wave climbing it, row by row in layers like a fire tongue: centre(v, r) is how far row r (v of the way up) sits from x.
func ghostTongue(p *pixelfx.Painter, x, y, h, w float64, centre func(v, r float64) float64, N func(rate, seed, off float64) float64, n float64, H []uint32, ls []layer, jit float64) {
	for _, l := range ls {
		L, c := h*l.h, H[l.ri]
		for r := 0.0; r <= L; r++ {
			u, v := r/math.Max(1, L), r/h
			// a teardrop: round at the bottom, swelling, then drawn out thin
			half := w*l.w*math.Pow(1-u, 0.75)*math.Min(1, (r+1.5)/4) + N(6, n, r*0.4)*jit*u
			if half < 0.3 {
				continue
			}
			cx, Y := x+centre(v, r), pixelfx.Round(y-r)
			for px, x1 := pixelfx.Round(cx-half), pixelfx.Round(cx+half); px <= x1; px++ {
				p.Px(px, Y, c)
			}
		}
	}
}

type (
	spiritCurl  struct{ x, y, vx, vy, s, a, w float64 }
	spiritSpark struct{ x, y, vx, vy, a float64 }
)

func newSpiritFlame(o *pixelfx.Options) pixelfx.Instance {
	D := float64(spiritDuration)
	NL := noiseFor(D)
	F := pixelfx.Fit(o.Palette(), 9)
	H, S := hotFirst(F), o.Seed()
	COLD := pixelfx.Mix(H[3], pixelfx.White, 0.15)
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		ph := t / D
		N := func(rate, seed, off float64) float64 { return NL(t, rate, seed, off) }
		bob := math.Sin(ph*tau) * 1.5
		// the wave: it climbs the flame twice a loop, growing toward the tip, and the whole flame leans with a slow drift
		lean := N(0.67, S+2, 0) * 3
		wave := func(v, r, k float64) float64 {
			return (math.Sin(r*0.24-ph*tau*2+k)*2.2+math.Sin(r*0.11-ph*tau+k*0.5)*3.2)*v*v + lean*v*v
		}
		H0, y0 := 44*(0.92+0.08*N(1.3, S+3, 0)), bob

		// the glow it casts, cold and slow
		br := 0.9 + 0.1*N(2, S+4, 0)
		p.Glow(0, y0-16, 60, COLD, 0.36*br)
		p.Glow(wave(0.5, 20, 0)*0.5, y0-20, 26, H[2], 0.2*br)
		// curls breaking off the tips and drifting up, thinning out
		looped(t, D, 40, S+300, func(r *pixelfx.Rng, _ int) (spiritCurl, float64) {
			var q spiritCurl
			q.x = r.Range(-4, 4)
			q.y = -H0 * r.Range(0.55, 0.95)
			q.vx = r.Range(-7, 7)
			q.vy = -r.Range(10, 22)
			life := r.Range(1, 1.9)
			q.s = r.Range(1.4, 2.8)
			q.a = r.Range(0, tau)
			q.w = r.Range(2, 3.5)
			return q, life
		}, func(q *spiritCurl, age, life float64, _ int) {
			x, y, _, _ := drift(q.x+wave(0.9, 26, 0)*0.5, q.y+y0, q.vx, q.vy, age, 0, -6, 1.2, 0.6)
			k := 1 - age/life
			cx, a := x+math.Sin(age*q.w+q.a)*3*(age+0.2), math.Min(1, age*5)*k
			soft(p, cx, y, q.s*(0.8+age*0.8), H[4], 0.35*a)
			soft(p, cx-0.4, y-0.6, q.s*0.5*(0.8+age*0.5), H[2], 0.4*a*k)
			p.Glow(cx, y, 6, H[3], 0.15*a)
		})
		// side wisps that flicker out and curl, now long, now gone; then the body
		wl, wr := math.Max(0, 0.25+0.55*N(1.7, S+6, 0)), math.Max(0, 0.25+0.55*N(1.7, S+7, 0))
		if wl > 0.12 {
			ghostTongue(p, -4, y0-4, H0*wl, 2.4, func(v, r float64) float64 {
				return wave(v, r, 2.1) - 6*v*v + 3*math.Sin(v*5-ph*tau*2)*v
			}, N, S*11+1, H, ghostLayers, 0.5)
		}
		if wr > 0.12 {
			ghostTongue(p, 4.2, y0-4, H0*wr, 2.4, func(v, r float64) float64 {
				return wave(v, r, -1.8) + 6*v*v - 3*math.Sin(v*5-ph*tau*2+1)*v
			}, N, S*11+2, H, ghostLayers, 0.5)
		}
		ghostTongue(p, 0, y0, H0, 8.6, func(v, r float64) float64 { return wave(v, r, 0) }, N, S*11, H, ghostLayers, 0.6)
		// bright strands drifting up through the hollow heart
		for k := range 2 {
			kf := float64(k)
			c := H[1]
			if k != 0 {
				c = H[2]
			}
			for r := 4.0; r < H0*0.72; r++ {
				v := r / H0
				x := wave(v, r, 0) + math.Sin(r*0.35-ph*tau*3+kf*math.Pi)*2.2*(1-v)
				if math.Sin(r*0.2-ph*tau*4+kf*2) > 0.2 {
					p.Px(x, y0-r, c)
				}
			}
		}
		// light along the body
		for _, v := range [3]float64{0.2, 0.45, 0.7} {
			p.Glow(wave(v, H0*v, 0), y0-H0*v, 12, H[2], 0.16*br)
		}
		// the pale heart breathing low in the flame
		p.Glow(0, y0-4, 9, H[1], 0.22+0.06*N(3, S+5, 0))
		// cold sparks rising off it, twinkling
		looped(t, D, 34, S+400, func(r *pixelfx.Rng, _ int) (spiritSpark, float64) {
			var q spiritSpark
			q.x = r.Range(-7, 7)
			q.y = -r.Range(4, 28)
			q.vx = r.Range(-12, 12)
			q.vy = -r.Range(14, 34)
			life := r.Range(0.9, 1.8)
			q.a = r.Range(0, tau)
			return q, life
		}, func(q *spiritSpark, age, life float64, _ int) {
			x, y, _, _ := drift(q.x, q.y+y0, q.vx, q.vy, age, 0, -10, 1.2, 1.2)
			k, tw := 1-age/life, 0.5+0.5*math.Sin(age*14+q.a)
			sx := x + math.Sin(age*3+q.a)*3
			if k*(0.4+0.6*tw) < 0.2 {
				return
			}
			c := H[3]
			switch {
			case tw > 0.7 && k > 0.5:
				c = H[0]
			case k > 0.5:
				c = H[1]
			}
			p.Px(sx, y, c)
			if tw > 0.85 && k > 0.6 {
				p.Px(sx-1, y, H[3])
				p.Px(sx+1, y, H[3])
				p.Px(sx, y-1, H[3])
				p.Px(sx, y+1, H[3])
			}
			p.Glow(sx, y, 4, H[2], 0.3*k)
		})
	}}
}
