// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/frost-breath.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// Soft puffs drawn as a body, a denser core and a lit cap, and streaks with a hot head and a cool tail, follow the recipes in Sprite Fusion's open-source pixel art VFX (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// FrostBreath is a cone of icy wind blowing right: frost puffs billow out, swell and sink, wind streaks race through, ice crystals tumble and glint off a white-hot mouth. It is registered as "ice/frost-breath".
var FrostBreath = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Frost breath",
		Desc:     "A cone of icy wind blowing right: frost puffs billow out, swell and sink, wind streaks race through, ice crystals tumble and glint off a white-hot mouth.",
		Tags:     []string{"ice", "breath", "frost", "cone", "wind", "directional", "loop"},
		Size:     [2]int{184, 104},
		Origin:   [2]int{12, 50},
		Duration: breathDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "spread", Default: pixelfx.Num(0.21)},
			{Name: "power", Default: pixelfx.Num(1)},
			{Name: "seed", Default: pixelfx.Num(11)},
		},
	},
	New: newFrostBreath,
}

func init() { pixelfx.Register("ice/frost-breath", FrostBreath) }

var (
	breathDuration = 1.2
	// breathSink is how much the cold air sags as it slows: it is heavy.
	breathSink = 9.0
)

// breathPuff draws a puff of frost: a soft body, a denser core sitting low, a lit cap while it's young, a cold haze of light.
func breathPuff(p *pixelfx.Painter, C []uint32, x, y, r, a, k float64, front bool) {
	body, capc := pick(front, C[5], C[4]), pick(front, C[7], C[6])
	soft := func(c uint32, q float64) func(d, _ float64) (uint32, float64) {
		return func(d, _ float64) (uint32, float64) { return c, q * (1 - d*d) }
	}
	p.DiscFn(x, y, r, soft(body, a*0.6))
	p.DiscFn(x+r*0.06, y+r*0.12, r*0.68, soft(pixelfx.Mix(body, C[3], 0.3), a*0.5))
	if k < 0.55 {
		p.DiscFn(x-r*0.25, y-r*0.3, r*0.5, soft(capc, a*0.7*(1-k/0.55)))
	}
	p.Glow(x, y, r*1.7, C[4], 0.1*a)
}

// breathGust draws the body of the gust: a cone of turbulent, translucent cold air streaming right, brightest at its heart. The noise scrolls exactly one tile per loop, so it never seams.
func breathGust(p *pixelfx.Painter, C []uint32, t, spread, S float64) {
	ph, sp := t/breathDuration, math.Tan(spread)*1.08
	p.Each(2, -46, 170, 46, func(x, y float64, sx, sy int) {
		u := x / 170
		hw, yc := 2.5+x*sp, 7*u*u
		v := (y - yc) / hw
		if v*v >= 1 || u >= 1 {
			return
		}
		n := pixelfx.TileFbm(x*0.055-ph*10, (y-yc)*0.15+7, 10, 0, S, 2)
		edge := (1 - v*v) * pick(u < 0.08, u/0.08, 1) * math.Pow(1-u, 0.8)
		// denser and brighter down its heart near the mouth
		d := pixelfx.Clamp(0.5+n*1.3+(1-math.Abs(v))*0.35*math.Pow(1-u, 1.5)) * edge
		if d < 0.08 {
			return
		}
		var c uint32
		switch {
		case d > 0.72:
			c = C[7]
		case d > 0.52:
			c = C[6]
		case d > 0.32:
			c = C[5]
		default:
			c = C[4]
		}
		p.BlendPx(sx, sy, c, math.Min(0.78, d*0.95)*pixelfx.Clamp((d-0.08)/0.3))
		if d > 0.3 {
			p.Add(sx, sy, pick(d > 0.6, C[6], C[5]), 0.2*d)
		}
	})
}

func newFrostBreath(o *pixelfx.Options) pixelfx.Instance {
	C, S, sp, power := pixelfx.Fit(o.Palette(), 8), o.Seed(), o.Num("spread"), o.Num("power")
	D := breathDuration
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			breathGust(p, C, t, sp, S)
			// the big slow billows behind
			pixelfx.Cycle(t, D, 12, S, func(g *pixelfx.Rng) float64 { return g.Range(1, 1.25) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				th := r.Range(-sp, sp)
				v := r.Range(170, 230) * power
				x, y := pixelfx.Fly(3, r.Range(-1, 1), v*math.Cos(th), v*math.Sin(th), age, breathSink, 1.4)
				k := age / life
				rad := 2 + 12*math.Pow(k, 0.75)
				breathPuff(p, C, x, math.Min(y, 40-rad), rad, 0.75*math.Pow(1-k, 1.2)*math.Min(1, age/0.08), k, false)
			})
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			// light down the cone
			p.GlowLine(4, 0, 120, 6, 24, C[4], 0.26)
			p.GlowLine(2, 0, 50, 1, 9, C[6], 0.3)
			p.Glow(60, 4, 44, C[3], 0.16)
			// brighter puffs in front, smaller and quicker
			pixelfx.Cycle(t, D, 10, S+1, func(g *pixelfx.Rng) float64 { return g.Range(0.7, 0.95) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				th := r.Range(-sp*0.8, sp*0.8)
				v := r.Range(190, 260) * power
				x, y := pixelfx.Fly(3, r.Range(-1, 1), v*math.Cos(th), v*math.Sin(th), age, breathSink, 1.6)
				k := age / life
				rad := 1.5 + 8*math.Pow(k, 0.7)
				breathPuff(p, C, x, math.Min(y, 40-rad), rad, 0.85*math.Pow(1-k, 1.3)*math.Min(1, age/0.06), k, true)
			})
			// snow carried along, swaying
			pixelfx.Cycle(t, D, 18, S+2, func(g *pixelfx.Rng) float64 { return g.Range(0.8, 1.1) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				th := r.Range(-sp*1.1, sp*1.1)
				v := r.Range(120, 190)
				x, y := pixelfx.Fly(4, 0, v*math.Cos(th), v*math.Sin(th), age, breathSink*1.4, 1.5)
				k := age / life
				sw := math.Sin(age*9+r.Range(0, tau)) * 1.5
				if k < 0.85 || math.Mod(age*30, 2) < 1 {
					p.Px(x, y+sw, pick(r.Chance(0.5), pixelfx.White, C[6]))
				}
			})
			// wind streaks: a white head, a pale body, a cool tail
			pixelfx.Cycle(t, D, 26, S+3, func(g *pixelfx.Rng) float64 { return g.Range(0.22, 0.36) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				th := r.Range(-sp*0.9, sp*0.9)
				v := r.Range(280, 380)
				x, y := pixelfx.Fly(6, r.Range(-2, 2), v*math.Cos(th), v*math.Sin(th), age, 0, 0.9)
				vx, vy := pixelfx.FlyV(v*math.Cos(th), v*math.Sin(th), age, 0, 0.9)
				s := pixelfx.Hypot(vx, vy)
				L, ux, uy, k := 4+s*0.03, vx/s, vy/s, age/life
				p.LineFn(x-ux*L, y-uy*L, x, y, func(u float64) (uint32, float64) {
					c := pick(u > 0.8, pixelfx.White, pick(u > 0.4, C[6], C[4]))
					return c, pick(u > 0.35, 1, u/0.35) * pick(k < 0.75, 1, (1-k)/0.25)
				})
				p.Glow(x, y, 3, C[5], 0.3*(1-k))
			})
			// ice crystals tumbling through: a short shard with a white edge, glinting as it turns
			pixelfx.Cycle(t, D, 9, S+4, func(g *pixelfx.Rng) float64 { return g.Range(0.65, 0.9) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				th := r.Range(-sp, sp)
				v := r.Range(150, 230)
				x, y := pixelfx.Fly(5, 0, v*math.Cos(th), v*math.Sin(th), age, breathSink, 1.1)
				k := age / life
				a := r.Range(0, tau) + age*r.Range(9, 15)*r.Sign()
				L := r.Range(1.5, 2.6)
				ca, sa := math.Cos(a), math.Sin(a)
				if k > 0.85 && math.Mod(age*30, 2) < 1 {
					return
				}
				p.Line(x-ca*L, y-sa*L, x+ca*L, y+sa*L, C[5], 1)
				p.Px(x+ca*L, y+sa*L, pixelfx.White)
				if gl := math.Cos(a * 2); gl > 0.92 {
					for _, d := range cross4 {
						p.Px(x+d[0], y+d[1], C[6])
					}
					p.Px(x, y, pixelfx.White)
					p.Glow(x, y, 6, C[5], 0.5)
				} else {
					p.Glow(x, y, 3, C[5], 0.2)
				}
			})
			// the mouth: a white-hot core, a flickering jet of cold
			for i := range 3 {
				fi := float64(i)
				L, dy := 9+6*pixelfx.LoopNoise(ph, 1.2, S+fi*7), (fi-1)*1.4
				p.LineFn(2, dy*0.5, 2+L, dy, func(u float64) (uint32, float64) {
					return pick(u < 0.5, pixelfx.White, C[6]), pick(u < 0.6, 1, 1-(u-0.6)/0.4)
				})
			}
			p.Ellipse(2.5, 0, 4.6, 3, C[6], 1)
			p.Ellipse(2, 0, 3, 1.8, pixelfx.White, 1)
			fl := 0.85 + 0.15*math.Sin(ph*tau*3)
			p.Glow(2, 0, 24, C[5], 0.7*fl)
			p.Glow(2, 0, 9, pixelfx.White, 0.5*fl)
		},
	}
}
