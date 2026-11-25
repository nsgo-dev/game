// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/bullet-tracer.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the spark and streak recipes from Sprite Fusion's open-source impact ricochet (https://www.spritefusion.com/pixel-art-vfx/impact), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// BulletTracer is a white-hot slug tearing along, its tracer burning behind it in a long streak that sputters and cools from white through gold to red, shedding sparks and a thread of smoke. It is registered as "projectiles/bullet-tracer".
var BulletTracer = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Bullet tracer",
		Desc:     "A white-hot slug tearing along, its tracer burning behind it in a long streak that sputters and cools from white through gold to red, shedding sparks and a thread of smoke.",
		Tags:     []string{"projectile", "bullet", "tracer", "gun", "directional", "loop"},
		Size:     [2]int{200, 40},
		Origin:   [2]int{184, 18},
		Duration: bulletTracerDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "smoke", Default: pixelfx.Palette("#2e2a36", "#46404f", "#625b6b", "#817a8a", "#a39caa")},
			{Name: "length", Default: pixelfx.Num(120)},
			{Name: "speed", Default: pixelfx.Num(520)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newBulletTracer,
}

func init() { pixelfx.Register("projectiles/bullet-tracer", BulletTracer) }

const (
	bulletTracerDuration = 0.6
	bulletTracerOriginX  = 184
)

var bulletSlug = []string{"..oyyp..", ".ypwwwwp", "ywwwwwww", ".ypwwwwp", "..oyyp.."}

func newBulletTracer(o *pixelfx.Options) pixelfx.Instance {
	F, SM := pixelfx.Fit(o.Palette(), 9), pixelfx.Fit(o.Ramp("smoke"), 5)
	S, D, L, V := o.Seed(), float64(bulletTracerDuration), o.Num("length"), o.Num("speed")
	left := float64(-bulletTracerOriginX + 2)
	SP := [4]uint32{F[7], F[6], F[4], F[3]}
	slugPal := map[rune]uint32{'w': pixelfx.White, 'p': F[7], 'y': F[6], 'o': F[5]}
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		fr, ph := math.Floor(t*30+1e-6), (tau*t)/D

		// a thread of smoke off the cool end of the trace, wavering as it thins
		var puffs []cloudPuff
		beat(t, D, 18, S+7, func(r *pixelfx.Rng) float64 { return r.Range(0.5, 0.7) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			k := age / life
			x := -L*r.Range(0.45, 0.8) - age*90
			y := 0.5 + math.Sin(age*6+r.Range(0, tau))*age*3 - age*age*8
			if x < left+6 {
				return
			}
			puffs = append(puffs, cloudPuff{
				x: x,
				y: y,
				r: 1.2 + age*4,
				a: 0.55 * (1 - k) * math.Min(1, age*8) * pixelfx.Smooth(left, left+30, x),
				b: 0.95 - k*0.4,
				w: 0,
			})
		})
		cloud(p, puffs, SM, SM, cloudOpts{dens: 1.2, max: 0.6})

		// the trace: a tapering body of burning gas near the slug, banded white core, gold, an orange rim; past it a one-pixel line cooling to red
		heat := func(d float64) float64 {
			return 1 - math.Pow(d/L, 0.8) + pixelfx.Noise(d*0.12-t*40, S)*0.12 + 0.08*math.Sin(d*0.35-ph*9)
		}
		yAt := func(d float64) float64 { return 0.5 + math.Sin(d*0.09+ph*2)*(d/L)*0.8 }
		HOT := L * 0.32
		p.Shade(-HOT-1, -4, 1, 5, func(x, y float64, _ *pixelfx.Out) uint32 {
			d := -x
			u := d / HOT
			if u < 0 || u > 1 {
				return 0
			}
			w := 0.7 + 2.3*math.Pow(1-u, 1.4)*(0.9+0.1*math.Sin(d*0.8-ph*12))
			v := math.Abs(y-yAt(d)) / w
			if v > 1 {
				return 0
			}
			h := heat(d)
			switch {
			case v < 0.38:
				return pick(h > 0.8, pixelfx.White, F[7])
			case v < 0.7:
				if h > 0.75 {
					return F[7]
				}
				if h > 0.6 {
					return F[6]
				}
				return F[5]
			default:
				return pick(u < 0.5, F[5], F[4])
			}
		})
		for d := HOT; d <= L; d += 0.5 {
			u, h := d/L, heat(d)
			if u > 0.55 && pixelfx.Rand(math.Floor(d/3), fr, S) < (u-0.55)*1.6 {
				continue
			}
			var c uint32
			switch {
			case h > 0.55:
				c = F[6]
			case h > 0.42:
				c = F[5]
			case h > 0.3:
				c = F[4]
			case h > 0.18:
				c = F[3]
			case h > 0.08:
				c = F[2]
			default:
				c = F[1]
			}
			a := 1.0
			if u >= 0.85 {
				a = (1 - u) / 0.15
			}
			p.Dot(-d, yAt(d), c, a)
		}
		p.GlowLine(-2, 1, -L*0.45, 1, 6, F[4], 0.35)
		p.GlowLine(-2, 1, -L*0.2, 1, 3, F[7], 0.3)

		// sparks of burning compound shed off the trace, dropping and cooling as they fall behind
		beat(t, D, 16, S+2, func(r *pixelfx.Rng) float64 { return r.Range(0.1, 0.28) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			x, y, vx, vy := wake(-r.Range(4, L*0.5), 0.5+r.Range(-0.5, 1), -r.Range(10, 80), r.Range(-60, 50), age, V*0.35, 3, 260)
			if x > left+8 {
				spark(p, x, y, vx, vy, 1-age/life, SP, 0.45, 0.012, 6)
			}
		})

		// the slug: a white-hot pointed round, pale-rimmed, its nose a point of light
		p.Sprite(bulletSlug, slugPal, -1, -1, 1, pixelfx.SpriteOpts{})
		p.Glow(2, 1, 24, F[4], 0.5)
		p.Glow(3, 1, 8, pixelfx.White, 0.55)
	}}
}
