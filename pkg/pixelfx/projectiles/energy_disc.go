// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/energy-disc.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the tangent spark streaks from Sprite Fusion's open-source energy and impact effects (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// EnergyDisc is a sawblade of crackling energy spinning flat out as it flies: hooked teeth bright on their leading edges, a white hub, spiral arms churning, sparks flung off the teeth. It is registered as "projectiles/energy-disc".
var EnergyDisc = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Energy disc",
		Desc:     "A sawblade of crackling energy spinning flat out as it flies: hooked teeth bright on their leading edges, a white hub, spiral arms churning, sparks flung off the teeth.",
		Tags:     []string{"projectile", "disc", "saw", "energy", "sci-fi", "directional", "loop"},
		Size:     [2]int{120, 52},
		Origin:   [2]int{92, 26},
		Duration: energyDiscDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0a0f2e", "#172a78", "#2550d0", "#3f8cff", "#7cc8ff", "#c8f0ff", "#ffffff")},
			{Name: "size", Default: pixelfx.Num(9)},
			{Name: "teeth", Default: pixelfx.Num(8)},
			{Name: "speed", Default: pixelfx.Num(220)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newEnergyDisc,
}

func init() { pixelfx.Register("projectiles/energy-disc", EnergyDisc) }

const (
	energyDiscDuration = 0.5
	energyDiscOriginX  = 92
)

func newEnergyDisc(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	R, N, V, S, D := o.Num("size"), o.Num("teeth"), o.Num("speed"), o.Seed(), float64(energyDiscDuration)
	left := float64(-energyDiscOriginX + 2)
	seg := tau / N
	SP := [4]uint32{pixelfx.White, C[5], C[4], C[3]}
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		rot := (tau * 3 * t) / D

		// speed lines peeling off the rim, top and bottom, stretching back and cooling
		beat(t, D, 8, S+4, func(r *pixelfx.Rng) float64 { return r.Range(0.12, 0.22) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			k := age / life
			side := pick(r.Chance(0.5), -1.0, 1.0)
			x0 := -2 - age*r.Range(180, 260)
			ln := r.Range(8, 16) * (0.5 + k)
			y := side * (R + r.Range(0.5, 3))
			if x0-ln < left {
				return
			}
			col := C[3]
			if k < 0.35 {
				col = C[5]
			} else if k < 0.7 {
				col = C[4]
			}
			p.LineFn(x0, y, x0-ln, y, func(u float64) (uint32, float64) {
				a := 1.0
				if u >= 0.65 {
					a = (1 - u) / 0.35
				}
				return col, a
			})
		})
		p.GlowLine(-2, 0, -26, 0, 8, C[3], 0.35)

		// sparks flung off the tooth tips along the spin, then left behind in the air
		beat(t, D, 20, S, func(r *pixelfx.Rng) float64 { return r.Range(0.12, 0.3) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			// keeps pixelfx's two range draws for RNG parity
			a := rot - r.Range(0, tau)*0 + r.Range(0, tau)
			tip := R + 3.5
			x0, y0, v := math.Cos(a)*tip, math.Sin(a)*tip, r.Range(120, 230)
			x, y, vx, vy := wake(x0, y0, -math.Sin(a)*v, math.Cos(a)*v, age, V*0.6, 2.4, 60)
			if x > left+8 && math.Abs(y) < 23 {
				spark(p, x, y, vx, vy, 1-age/life, SP, 0.45, 0.014, 7)
			}
		})

		// the disc: a white hub in a dark collar, spiral arms of energy churning, a bright rim; past the rim, hooked teeth and a dim blur ring
		p.Shade(-R-5, -R-5, R+5, R+5, func(x, y float64, _ *pixelfx.Out) uint32 {
			r, th := pixelfx.Hypot(x, y), math.Atan2(y, x)
			if r > R+4.6 {
				return 0
			}
			if r <= R {
				if r < 1.8 {
					return pixelfx.White
				}
				if r < 2.9 {
					return C[1]
				}
				if r > R-1.1 {
					return C[5]
				}
				arm := math.Sin(3*(th-rot*0.5) - r*0.85)
				switch {
				case arm > 0.65:
					return C[5]
				case arm > 0.2:
					return C[4]
				case arm > -0.3:
					return C[3]
				}
				return C[2]
			}
			ph := pixelfx.Fract((th - rot) / seg)
			h := 4.2 * math.Pow(ph, 1.4)
			if r-R < h {
				switch {
				case ph > 0.86:
					return pixelfx.White
				case ph > 0.6:
					return C[6]
				case ph > 0.35:
					return C[5]
				}
				return C[4]
			}
			if r-R < 1.6 {
				return C[2]
			}
			return 0
		})
		// filaments of energy crackling from the hub to the rim, new every frame
		fr := math.Floor(t*30 + 1e-6)
		for j := range 3 {
			g := pixelfx.RngOf(fr, float64(j), S)
			a0 := g.Range(0, tau)
			pts := make([][2]float64, 0, 5)
			for i := 0; i <= 4; i++ {
				rr := 2.5 + ((R-3)*float64(i))/4
				off := 0.0
				if i != 0 {
					off = g.Range(-0.35, 0.35)
				}
				a := a0 + float64(i)*0.25 + off
				pts = append(pts, [2]float64{math.Cos(a) * rr, math.Sin(a) * rr})
			}
			p.Polyline(pts, pick(j != 0, C[6], pixelfx.White), 1)
		}
		p.Ring(0, 0, R+0.5, 1, C[6], 1)
		// light: a soft pool, a hot hub, a glint riding the top of the rim
		p.Glow(0, 0, 30, C[3], 0.4)
		p.Glow(0, 0, 6, C[5], 0.35)
		p.Glow(math.Cos(rot)*R, math.Sin(rot)*R, 5, C[6], 0.4)
	}}
}
