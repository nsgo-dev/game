// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/laser-bolt.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source energy beam (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez, for the banded body, travelling pulses and spiralling sparks.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// LaserBolt is a red laser bolt: a white-hot core in a rounded head, red bands tapering back, pulses racing down it, a tail that shimmers apart into wavering dashes. It is registered as "projectiles/laser-bolt".
var LaserBolt = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Laser bolt",
		Desc:     "A red laser bolt: a white-hot core in a rounded head, red bands tapering back, pulses racing down it, a tail that shimmers apart into wavering dashes.",
		Tags:     []string{"projectile", "laser", "energy", "sci-fi", "directional", "loop"},
		Size:     [2]int{128, 32},
		Origin:   [2]int{108, 16},
		Duration: laserBoltDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a0610", "#5c0d1c", "#9a1428", "#d8243a", "#ff4d5e", "#ff8a94", "#ffd0d4", "#ffffff")},
			{Name: "length", Default: pixelfx.Num(40)},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newLaserBolt,
}

func init() { pixelfx.Register("projectiles/laser-bolt", LaserBolt) }

const (
	laserBoltDuration = 0.5
	laserBoltOriginX  = 108
)

func newLaserBolt(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	L, S, D := o.Num("length"), o.Seed(), float64(laserBoltDuration)
	left := float64(-laserBoltOriginX + 2)
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		ph, fr := t/D, math.Floor(t*30+1e-6)

		// the shimmering tail: dashes shed off the bolt's end, wavering, flickering and cooling as they fall behind
		beat(t, D, 14, S, func(r *pixelfx.Rng) float64 { return r.Range(0.18, 0.3) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			k := age / life
			x0 := -L*0.8 - age*r.Range(150, 230)
			ln := r.Range(3, 7) * (1 - k*0.5)
			y := 0.5 + math.Sin(x0*0.25+ph*tau*2)*(0.5+k*1.5) + r.Range(-0.6, 0.6)*k
			if x0-ln < left || pixelfx.Rand(fr, r.Int(0, 999)) < k*0.5 {
				return
			}
			col := C[3]
			if k < 0.3 {
				col = C[5]
			} else if k < 0.6 {
				col = C[4]
			}
			p.Line(x0, y, x0-ln, y, col, 1)
			if k < 0.4 {
				p.Glow(x0-ln/2, y, 4, C[4], 0.3*(1-k))
			}
		})

		// the body: bands round a white core, thickest just behind the rounded head and tapering to a thread
		p.Shade(-L-1, -5, 4, 6, func(x, y float64, _ *pixelfx.Out) uint32 {
			d := -x + 3
			u := d / (L + 3)
			if u < 0 || u > 1 {
				return 0
			}
			head := 1.0
			if d < 3 {
				q := (3 - d) / 3
				head = math.Sqrt(math.Max(0, 1-q*q))
			}
			w := (3.4*math.Pow(1-u, 0.9) + 0.5) * head * (1 + 0.08*math.Sin(d*0.9-ph*tau*4))
			v := math.Abs(y-0.5) / w
			if v > 1.2 {
				return 0
			}
			if v > 1 && (u > 0.5 || pixelfx.Rand(math.Floor(x), fr) > 0.6) {
				return 0
			}
			pulse := 0
			if pixelfx.Wrap(d*0.05-ph*3, 1) < 0.12 {
				pulse = 1
			}
			var lvl int
			switch {
			case v < 0.22:
				lvl = pick(u < 0.6, 7, 6)
			case v < 0.45:
				lvl = 6 - pick(u > 0.5, 1, 0)
			case v < 0.72:
				lvl = 4
			case v <= 1:
				lvl = 3
			default:
				lvl = 2
			}
			if v < 0.72 {
				lvl += pulse
			}
			return C[min(7, lvl)]
		})
		// sparks spiralling round the core, a bright one on the near side and a dim one behind
		for _, s := range [2]float64{0, pi} {
			for d := 2.0; d < L*0.7; d += 1 {
				a := d*0.45 - ph*tau*3 + s
				y := 0.5 + math.Sin(a)*(2.2*(1-d/L)+0.6)
				if math.Cos(a) > 0.3 {
					p.Dot(-d, y, pick(math.Cos(a) > 0.8, pixelfx.White, C[6]), 1)
				}
			}
		}
		// the head's glint, flickering, and the light
		g := 0.75 + 0.25*math.Sin(ph*tau*2)
		pixelfx.Sparkle(p, 1, 0.5, 3+pixelfx.Round(g*2), C[6], C[4], 0.5)
		p.GlowLine(1, 0.5, -L, 0.5, 9, C[3], 0.4)
		p.Glow(-2, 0.5, 20, C[3], 0.5*g)
		p.Glow(1, 0.5, 6, pixelfx.White, 0.25)
	}}
}
