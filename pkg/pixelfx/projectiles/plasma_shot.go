// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/plasma-shot.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source energy bolt and orb (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez, for the layered head, rim rays and trail light.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// PlasmaShot is a glowing plasma blob: hot knots churning in its core, a corona crackling over its skin, its tail necking into droplets that tear off, cool and wink out behind. It is registered as "projectiles/plasma-shot".
var PlasmaShot = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Plasma shot",
		Desc:     "A glowing plasma blob: hot knots churning in its core, a corona crackling over its skin, its tail necking into droplets that tear off, cool and wink out behind.",
		Tags:     []string{"projectile", "plasma", "energy", "sci-fi", "directional", "loop"},
		Size:     [2]int{144, 60},
		Origin:   [2]int{108, 30},
		Duration: plasmaShotDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#061a1c", "#0b3b34", "#0f6a4a", "#18a05a", "#3fd46a", "#8cf57e", "#d6ffc4", "#ffffff")},
			{Name: "size", Default: pixelfx.Num(6)},
			{Name: "speed", Default: pixelfx.Num(200)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newPlasmaShot,
}

func init() { pixelfx.Register("projectiles/plasma-shot", PlasmaShot) }

const (
	plasmaShotDuration = 0.8
	plasmaShotOriginX  = 108
)

type plasmaBall struct {
	x, y, r, h, core, k float64
}

var plasmaKnotSpeeds = [3]float64{2, -3, 5}

func newPlasmaShot(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R, V, S, D := o.Num("size"), o.Num("speed"), o.Seed(), float64(plasmaShotDuration)
	left := float64(-plasmaShotOriginX + 2)
	SP := [4]uint32{pixelfx.White, C[6], C[5], C[3]}
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		w, fr := (tau*t)/D, math.Floor(t*30+1e-6)

		// an ion streak left in the air behind it, rippling and cooling in steps
		T0 := -R * 3.2
		tail := func(x float64) float64 {
			return math.Sin(x*0.2+w*2) * (0.3 + ((T0-x)/44)*1.8)
		}
		for x := T0; x > T0-44; x -= 1 {
			u, y := (T0-x)/44, tail(x)
			col := C[4]
			if u < 0.2 {
				col = C[6]
			} else if u < 0.45 {
				col = C[5]
			}
			a := 1.0
			if u >= 0.55 {
				a = (1 - u) / 0.45
			}
			p.Dot(x, y, col, a)
			if u < 0.3 {
				p.Px(x, y+1, pick(u < 0.15, C[5], C[4]))
			}
		}

		// the blob as a field of balls: a body, three hot knots churning round inside it, a wagging tail, and droplets tearing off
		all := make([]plasmaBall, 0, 24)
		all = append(all, plasmaBall{x: 0, y: 0, r: R, h: 1, core: 0.8})
		for i := range 3 {
			fi := float64(i)
			a := w*plasmaKnotSpeeds[i] + (fi*tau)/3
			d := R * (0.5 + 0.1*math.Sin(w*3+fi*2))
			all = append(all, plasmaBall{x: math.Cos(a)*d - 0.4, y: math.Sin(a) * d * 0.8, r: R * 0.32, h: 1, core: 1})
		}
		// the tail: thick and hot where it leaves the body, tapering and cooling to a point, its axis still glowing
		for i := 1; i <= 6; i++ {
			fi := float64(i)
			x := -R * (0.5 + fi*0.42)
			wag := math.Sin(w*2-fi*0.9) * (0.3 + fi*0.28)
			r := R * (0.62 - fi*0.08)
			all = append(all, plasmaBall{x: x, y: wag, r: r, h: 0.96 - fi*0.05, core: math.Sqrt((5.2-fi*0.65)*0.3) / r})
		}
		dropStart := len(all)
		pixelfx.Cycle(t, D, 11, S, func(r *pixelfx.Rng) float64 { return r.Range(0.34, 0.5) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			k := age / life
			pull := r.Range(450, 800)
			x := -R*2.9 - 0.5*pull*age*age - r.Range(0, 16)*age
			y := math.Sin(w*2-5.4+age*12)*2 + r.Range(-30, 30)*age*(0.4+age)
			rad := R * r.Range(0.3, 0.48) * (1 - k*0.6)
			all = append(all, plasmaBall{
				x:    x,
				y:    y,
				r:    rad,
				h:    0.97 * (1 - k*0.75),
				core: math.Sqrt(math.Max(0, 1-k*1.3)*7*0.3) / rad,
				k:    k,
			})
		})
		drops := all[dropStart:]

		// the skin comes from a compact field; the hot bands inside come from the body and its churning knots
		p.Shade(-R*2.6-60, -R*2-8, R*1.6, R*2+8, func(x, y float64, _ *pixelfx.Out) uint32 {
			f, heat, hh := 0.0, 0.0, 0.0
			for i := range all {
				b := &all[i]
				dx, dy := x-b.x, y-b.y
				d2 := dx*dx + dy*dy
				q := d2 / (4 * b.r * b.r)
				if q < 1 {
					c := ((1 - q) * (1 - q)) / 0.5625
					f += c
					heat += c * b.h
				}
				if b.core != 0 {
					hh += (b.core * b.core * b.r * b.r) / (d2 + 0.3)
				}
			}
			if f < 1 {
				return 0
			}
			lvl := 3
			switch {
			case hh > 10:
				lvl = 7
			case hh > 4.6:
				lvl = 6
			case hh > 2.2:
				lvl = 5
			case f > 1.4:
				lvl = 4
			}
			capIdx := int(pixelfx.Round(2 + (heat/f)*5.4))
			if x > R*0.3 && lvl < 5 {
				lvl++
			}
			return C[max(2, min(lvl, capIdx))]
		})

		// the corona: jagged arcs leaping off the skin and back, and rays flickering round the rim, new every frame
		for j := range 3 {
			g := pixelfx.RngOf(fr, float64(j), S)
			a0 := g.Range(-2.3, 2.3)
			span := g.Range(0.45, 0.9) * g.Sign()
			const n = 6
			hgt := g.Range(1.5, 3.5)
			pts := make([][2]float64, 0, n+1)
			for i := 0; i <= n; i++ {
				a := a0 + (span*float64(i))/n
				jit := 0.0
				if i%n != 0 {
					jit = g.Range(-0.5, 0.5)
				}
				rr := R + 0.6 + hgt*math.Sin((pi*float64(i))/n) + jit
				pts = append(pts, [2]float64{math.Cos(a)*rr - 0.4, math.Sin(a) * rr * 0.92})
			}
			p.Polyline(pts, pick(j != 0, C[5], C[6]), 1)
			p.Px(pts[3][0], pts[3][1], pixelfx.White)
			p.Glow(pts[3][0], pts[3][1], 6, C[5], 0.4)
		}
		for j := range 4 {
			g := pixelfx.RngOf(fr, float64(j+9), S)
			a, l, r0 := g.Range(-2.2, 2.2), g.Range(1.5, 4), R+1
			x0, y0 := math.Cos(a)*r0-0.4, math.Sin(a)*r0*0.92
			p.Line(x0, y0, x0+math.Cos(a)*l, y0+math.Sin(a)*l, pick(j%2 != 0, C[4], C[5]), 1)
		}
		// motes flung off the corona, streaking back
		beat(t, D, 14, S+5, func(r *pixelfx.Rng) float64 { return r.Range(0.12, 0.3) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			a, sp := r.Range(-2.4, 2.4), r.Range(60, 130)
			x, y, vx, vy := wake(math.Cos(a)*(R+1), math.Sin(a)*(R+1), math.Cos(a)*sp, math.Sin(a)*sp, age, V*0.7, 4, 0)
			if x > left+4 && math.Abs(y) < 26 {
				spark(p, x, y, vx, vy, 1-age/life, SP, 0.4, 0.015, 6)
			}
		})

		// light: a big soft pool, a hot heart that pulses with the churn, a point on every droplet
		pulse := 0.85 + 0.15*math.Sin(w*4)
		p.Glow(-2, 0, 34, C[3], 0.8*pulse)
		p.Glow(0, 0, 16, C[4], 0.35*pulse)
		p.Glow(1, 0, 7, C[6], 0.3*pulse)
		p.GlowLine(-R, 0, -R*4.6, 0, 7, C[3], 0.3)
		for i := range drops {
			d := &drops[i]
			if d.x < left+4 {
				continue
			}
			if d.k < 0.6 && d.x < -R*3.5 {
				p.Px(d.x, d.y, pick(d.k < 0.3, C[7], C[6]))
			}
			p.Glow(d.x, d.y, 3+d.r*2, C[2], 0.6*(1-d.k))
		}
	}}
}
