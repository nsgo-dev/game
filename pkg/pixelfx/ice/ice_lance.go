// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/ice-lance.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The projectile recipe (a hot head, a trail that thins behind it, motes shed along the way, light riding the head) is adapted from Sprite Fusion's open-source energy bolt (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// IceLance is a faceted ice spear flying right: a light pulse runs through its core, rings of cold air shear off the tip, frost peels off into a wake and crystals shed behind it. It is registered as "ice/ice-lance".
var IceLance = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ice lance",
		Desc:     "A faceted ice spear flying right: a light pulse runs through its core, rings of cold air shear off the tip, frost peels off into a wake and crystals shed behind it.",
		Tags:     []string{"ice", "lance", "spear", "projectile", "directional", "magic", "loop"},
		Size:     [2]int{176, 64},
		Origin:   [2]int{146, 32},
		Duration: lanceDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "length", Default: pixelfx.Num(40)},
			{Name: "seed", Default: pixelfx.Num(13)},
		},
	},
	New: newIceLance,
}

func init() { pixelfx.Register("ice/ice-lance", IceLance) }

var lanceDuration = 0.8

// lanceSpear draws the spear: a hexagonal shaft seen from the side, row by row (a white top edge, a lit upper facet, a bright ridge, a cool lower facet, a deep keel, a dark rim), a long cutting point, a light pulse running through its ridge to the tip, a glint sliding back along the top.
func lanceSpear(p *pixelfx.Painter, C []uint32, L, pulse, glint float64) {
	T, H := 16.0, 3.5
	half := func(x float64) float64 {
		if x > -T {
			return H*(-x/T) + 0.3
		}
		if x < -L+3 {
			return H * (0.7 + 0.3*((x+L)/3))
		}
		return H
	}
	p.Shade(-L-1, -H-2, 1, H+2, func(x, y float64, o *pixelfx.Out) uint32 {
		if x > 0 || x < -L {
			return 0
		}
		h := half(x)
		if math.Abs(y) > h+0.5 {
			if math.Abs(y) < h+1.5 {
				o.C = C[1]
			}
			return 0
		}
		v := y / math.Max(0.5, h)
		if x > -1.5 {
			o.C = pixelfx.White
			return o.C
		}
		// three faces lit from above: a pale top facet, a mid front facet with a bright ridge, a deep underside
		if v > -0.3 && v < 0.35 && math.Abs(x-pulse) < 2.5 {
			o.C = pixelfx.White
			return o.C
		}
		if v < -0.3 && math.Abs(x-glint) < 1 {
			o.C = pixelfx.White
			return o.C
		}
		switch {
		case v < -0.72:
			o.C = pixelfx.White
		case v < -0.36:
			o.C = C[6]
		case v < -0.18:
			o.C = C[7]
		case v < 0.38:
			o.C = C[4]
		case v < 0.78:
			o.C = C[2]
		default:
			o.C = C[1]
		}
		return 0
	})
}

func newIceLance(o *pixelfx.Options) pixelfx.Instance {
	C, S, L := pixelfx.Fit(o.Palette(), 8), o.Seed(), o.Num("length")
	D := lanceDuration
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		ph := t / D
		// the wake: a ribbon of frost narrowing behind the spear, with cold light in it
		p.Each(-L-100, -12, -L+4, 12, func(x, y float64, sx, sy int) {
			u := (-L + 4 - x) / 104
			w := 4.5*math.Pow(1-u, 0.7) + 0.5 + 1.5*math.Sin(u*9-ph*tau*2)*u
			v := math.Abs(y) / w
			if v >= 1 {
				return
			}
			a := (1 - v*v) * math.Pow(1-u, 1.4)
			if a < 0.04 {
				return
			}
			p.BlendPx(sx, sy, pick(v < 0.25 && u < 0.4, C[6], pick(v < 0.6, C[5], C[4])), a*0.7)
			if a > 0.3 {
				p.Add(sx, sy, C[5], 0.12*a)
			}
		})
		p.GlowLine(-L-60, 0, 0, 0, 14, C[4], 0.22)
		// frost peeling off the shaft into soft puffs left behind
		pixelfx.Cycle(t, D, 14, S, func(g *pixelfx.Rng) float64 { return g.Range(0.35, 0.55) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			x0 := r.Range(-L+2, -8)
			x, y := pixelfx.Fly(x0, r.Range(-2.5, 2.5), -r.Range(150, 230), r.Sign()*r.Range(8, 30), age, 0, 2.2)
			k := age / life
			rad, a := 1.2+5.5*k, 0.5*math.Pow(1-k, 1.3)
			c := pick(k < 0.3, C[6], C[5])
			p.DiscFn(x, y, rad, func(d, _ float64) (uint32, float64) { return c, a * (1 - d*d) })
			p.Glow(x, y, rad*1.6, C[4], 0.1*(1-k))
		})
		// crystals shed along the way: short tumbling shards with a white edge, glinting, falling back
		pixelfx.Cycle(t, D, 9, S+1, func(g *pixelfx.Rng) float64 { return g.Range(0.4, 0.6) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			x, y := pixelfx.Fly(r.Range(-L, -6), r.Range(-3, 3), -r.Range(170, 260), r.Range(-35, 35), age, 60, 1.2)
			k := age / life
			a := r.Range(0, tau) + age*r.Range(10, 18)*r.Sign()
			l := r.Range(1.2, 2.2)
			ca, sa := math.Cos(a), math.Sin(a)
			if k > 0.85 && math.Mod(age*30, 2) < 1 {
				return
			}
			p.Line(x-ca*l, y-sa*l, x+ca*l, y+sa*l, C[5], 1)
			p.Px(x+ca*l, y+sa*l, pixelfx.White)
			if math.Cos(a*2) > 0.9 {
				for _, d := range cross4 {
					p.Px(x+d[0], y+d[1], C[6])
				}
				p.Glow(x, y, 6, C[5], 0.45)
			}
		})
		// speed lines: the air streaming past above and below
		pixelfx.Cycle(t, D, 12, S+2, func(g *pixelfx.Rng) float64 { return g.Range(0.16, 0.26) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			y := r.Sign() * r.Range(4, 13)
			x := r.Range(-20, 0) - age*r.Range(280, 380)
			l, k := r.Range(6, 16), age/life
			p.LineFn(x, y, x+l, y, func(u float64) (uint32, float64) { return pick(u > 0.7, C[6], C[4]), u * (1 - k) * 1.4 })
		})
		// rings of cold air shearing off the point and sliding back along the shaft: the near arc bright, the far dim
		for i := range 3 {
			q := math.Mod(ph*3+float64(i)/3, 1)
			x, ry, rx := -4-q*(L+20), 4.5+q*8, 1.2+q*2.6
			if q > 0.8 {
				continue
			}
			for k := range 40 {
				an := (float64(k) / 40) * tau
				px, py := x+math.Cos(an)*rx, math.Sin(an)*ry
				if math.Abs(py) < 4.6 && px > -L-2 {
					continue
				}
				p.Px(px, py, pick(math.Cos(an) > 0, pick(q < 0.35, pixelfx.White, C[6]), pick(q < 0.4, C[5], C[4])))
			}
			p.GlowRing(x, 0, ry*0.75, 3, C[5], 0.14*(1-q))
		}
		// the ice fletching at the back: three shards fanning out behind the shaft
		for _, f := range [3][3]float64{{-1, 10, 4}, {1, 9, 4}, {-0.2, 6, 3}} {
			dy, l, w := f[0], f[1], f[2]
			blade(p, -L+3, dy*1.5, -L+3-l, dy*(2+l*0.5), w, C, noBlade)
		}
		// the spear itself, a pulse of light running through it, a glint sliding back along the top
		pulse := -L + math.Mod(ph*2, 1)*(L+6)
		glint := -math.Mod(ph*2+0.5, 1) * L * 1.2
		lanceSpear(p, C, L, pulse, glint)
		p.GlowLine(-L, 0, 0, 0, 10, C[4], 0.1)
		p.Glow(pulse, 0, 8, C[6], 0.3)
		p.Glow(0, 0, 8, pixelfx.White, 0.4)
		p.Glow(-L*0.4, 0, 30, C[3], 0.16)
	}}
}
