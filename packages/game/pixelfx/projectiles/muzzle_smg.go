// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/muzzle-smg.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flash lobes, spark, smoke wisp and casing recipes from Sprite Fusion's open-source muzzle-flash (https://www.spritefusion.com/pixel-art-vfx/muzzle-flash), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// MuzzleSMG is sustained fire at twelve rounds a second: each shot a different spiky flash with a white core, sparks, smoke wisps piling up and drifting, brass casings bouncing on the floor. It is registered as "projectiles/muzzle-smg".
var MuzzleSMG = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "SMG burst",
		Desc:     "Sustained fire at twelve rounds a second: each shot a different spiky flash with a white core, sparks, smoke wisps piling up and drifting, brass casings bouncing on the floor.",
		Tags:     []string{"projectile", "muzzle", "gun", "automatic", "flash", "smoke", "casing", "directional", "loop"},
		Size:     [2]int{112, 72},
		Origin:   [2]int{56, 66},
		Duration: muzzleSMGDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "brass", Default: pixelfx.Palette("#4a2e10", "#8c5c1c", "#d9a441", "#ffe39a")},
			{Name: "smoke", Default: pixelfx.Palette("#26222e", "#3b3644", "#544e5e", "#6f6979", "#8c8596", "#aca5b5", "#cbc4d2")},
			{Name: "height", Default: pixelfx.Num(26)},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newMuzzleSMG,
}

func init() { pixelfx.Register("projectiles/muzzle-smg", MuzzleSMG) }

const (
	muzzleSMGDuration = 1
	muzzleSMGOriginY  = 66
	smgShots          = 12
)

// smgEachShot calls fn(s, age) for every live copy of every shot fired at s * D / smgShots, while age < life.
func smgEachShot(t, D, life float64, fn func(s int, age float64)) {
	for s := range smgShots {
		for age := pixelfx.Wrap(t-(float64(s)*D)/smgShots+1e-7, D); age < life; age += D {
			fn(s, age)
		}
	}
}

func newMuzzleSMG(o *pixelfx.Options) pixelfx.Instance {
	F, B, SM := pixelfx.Fit(o.Palette(), 9), pixelfx.Fit(o.Ramp("brass"), 4), pixelfx.Fit(o.Ramp("smoke"), 7)
	S, D, H := o.Seed(), float64(muzzleSMGDuration), o.Num("height")
	const MX = 0.0
	MY := -H
	SP := [4]uint32{F[7], F[6], F[4], F[3]}
	top := float64(-muzzleSMGOriginY + 1)
	warmRamp := []uint32{SM[0], pixelfx.Mix(F[1], SM[1], 0.5), pixelfx.Mix(F[2], SM[2], 0.4), F[3], F[4], F[5], F[7]}
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		// the latest shot's pop of light, for the smoke it lights up
		pop := 0.0
		smgEachShot(t, D, 0.2, func(_ int, age float64) {
			pop = math.Max(pop, math.Exp(-(age+0.008)/0.035))
		})

		// smoke: every shot leaves a few wisps that push forward, slow, swell and rise, piling up into a drifting haze
		var puffs []cloudPuff
		smgEachShot(t, D, 2.2, func(s int, age float64) {
			for j := range 3 {
				r := pixelfx.RngOf(S, float64(s), float64(j), 3)
				life, dl := r.Range(1.5, 2.2), float64(j)*0.03
				if age < dl || age-dl >= life {
					continue
				}
				a, port := age-dl, j == 2
				k := a / life
				// a push forward that dies fast, then the warm gas lifts it and the air drifts it back over the gun
				var vx, vy float64
				if port {
					vx = r.Range(-12, 6)
					vy = -r.Range(14, 26)
				} else {
					vx = r.Range(45, 100) * (1 - float64(j)*0.4)
					vy = -r.Range(4, 16)
				}
				x := pick(port, -7.0, MX+3) + (vx*(1-math.Exp(-3.2*a)))/3.2 - r.Range(3, 7)*a*a + math.Sin(a*2.1+r.Range(0, tau))*a*2.5
				y := pick(port, MY-2, MY) + (vy*(1-math.Exp(-2.4*a)))/2.4 - 0.5*r.Range(9, 16)*a*a
				rad := pick(port, 1.0, 1.2) + r.Range(2, 4.5)*(1-math.Exp(-1.4*a))
				if y-rad < top+1 {
					continue
				}
				edge := math.Min(1, (y-rad-top)/12)
				puffs = append(puffs, cloudPuff{
					x: x,
					y: y,
					r: rad,
					a: math.Min(1, a*10) * math.Pow(1-k, 1.6) * pick(port, 0.24, 0.34) * edge,
					b: pixelfx.Clamp(0.92 - k*0.6),
					w: pop * math.Exp(-pixelfx.Hypot(x-MX, y-MY)/9),
				})
			}
		})
		cloud(p, puffs, SM, warmRamp, cloudOpts{dens: 1.4, max: 0.7, lit: 1.3})

		// casings: thrown up and back out of the ejection port, tumbling, bouncing on the floor, then lying a moment
		smgEachShot(t, D, D, func(s int, age float64) {
			r := pixelfx.RngOf(S, float64(s), 7)
			k := casing(-7, MY-1, -r.Range(18, 48), -r.Range(85, 135), r.Sign()*r.Range(20, 32), age, 850, -0.5, 0.42, r.Range(0, tau))
			drawCasing(p, k, B, pixelfx.Clamp((D-age)/0.25))
		})

		// the flash: a hero frame with a white core, then a cooler, smaller one; every shot its own shape
		smgEachShot(t, D, 2*f1, func(s int, age float64) {
			r := pixelfx.RngOf(S, float64(s))
			f := 2
			if age < f1 {
				f = 1
			}
			tilt := r.Range(-0.06, 0.06)
			L := flashShape(r, f, flashShapeOpts{size: r.Range(0.95, 1.2), spike: [2]float64{12, 21}, diag: [2]float64{6, 11}, cross: [2]float64{4, 7.5}, back: [2]float64{2.5, 5}})
			cols := [4]uint32{F[7], F[6], F[4], F[2]}
			if f == 1 {
				cols = [4]uint32{pixelfx.White, F[7], F[6], F[3]}
			}
			flash(p, MX, MY, tilt, L, cols, float64(s*7+f))
		})

		// sparks and unburnt grains spat forward with each shot, dropping as they slow
		smgEachShot(t, D, 0.3, func(s int, age float64) {
			for j := range 5 {
				r := pixelfx.RngOf(S, float64(s), float64(j), 11)
				life := r.Range(0.05, 0.16) * pick(j > 2, 1.8, 1.0)
				if age >= life {
					continue
				}
				a, sp := r.Range(-0.3, 0.3), r.Range(170, 380)*pick(j > 2, 0.5, 1.0)
				x, y := pixelfx.Fly(MX+2, MY, math.Cos(a)*sp, math.Sin(a)*sp, age, 420, 2.2)
				vx, vy := pixelfx.FlyV(math.Cos(a)*sp, math.Sin(a)*sp, age, 420, 2.2)
				if j > 2 {
					p.Px(x, y, pick(age/life < 0.5, F[6], F[4]))
				} else {
					spark(p, x, y, vx, vy, 1-age/life, SP, 0.45, 0.011, 9)
				}
			}
		})

		// light: a hot pop at the muzzle on every shot, the floor and smoke catching it
		smgEachShot(t, D, 0.15, func(_ int, age float64) {
			k := math.Exp(-(age + 0.008) / 0.03)
			p.Glow(MX+4, MY, 26, F[4], 0.8*k)
			p.Glow(MX+2, MY, 7, pixelfx.White, 0.45*k)
			p.Glow(MX+2, 0, 12, F[4], 0.16*k)
		})
	}}
}
