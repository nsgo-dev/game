// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/muzzle-pistol.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flash lobes, spark, smoke and casing recipes from Sprite Fusion's open-source muzzle-flash (https://www.spritefusion.com/pixel-art-vfx/muzzle-flash), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// MuzzlePistol is one pistol shot from a muzzle height px above the floor (the origin): a spiky white-cored flash, sparks, a curl of smoke hooking up and back, a casing that bounces and settles. It is registered as "projectiles/muzzle-pistol".
var MuzzlePistol = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Pistol shot",
		Desc:     "One pistol shot from a muzzle `height` px above the floor (the origin): a spiky white-cored flash, sparks, a curl of smoke hooking up and back, a casing that bounces and settles.",
		Tags:     []string{"projectile", "muzzle", "gun", "pistol", "flash", "smoke", "casing", "directional", "one-shot"},
		Size:     [2]int{96, 72},
		Origin:   [2]int{44, 66},
		Duration: muzzlePistolDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "brass", Default: pixelfx.Palette("#4a2e10", "#8c5c1c", "#d9a441", "#ffe39a")},
			{Name: "smoke", Default: pixelfx.Palette("#26222e", "#3b3644", "#544e5e", "#6f6979", "#8c8596", "#aca5b5", "#cbc4d2")},
			{Name: "height", Default: pixelfx.Num(26)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newMuzzlePistol,
}

func init() { pixelfx.Register("projectiles/muzzle-pistol", MuzzlePistol) }

const muzzlePistolDuration = 1.8

func newMuzzlePistol(o *pixelfx.Options) pixelfx.Instance {
	F, B, SM := pixelfx.Fit(o.Palette(), 9), pixelfx.Fit(o.Ramp("brass"), 4), pixelfx.Fit(o.Ramp("smoke"), 7)
	H, seed := o.Num("height"), o.Seed()
	MY, R := -H, pixelfx.NewRng(seed)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: []uint32{F[7], F[6], F[4], F[3]}})
	smoke := newSmoke(R)
	// the flash's shape, fixed for this shot
	shapeSeed, tilt := R.Int(0, 1e6), R.Range(-0.05, 0.05)
	// sparks spat forward, a few grains of unburnt powder that drop
	for range 6 {
		a, sp := R.Range(-0.3, 0.3), R.Range(180, 400)
		ps.Spawn(pixelfx.Spark, 3, MY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.06, 0.16), pixelfx.White, 1, 0, 0)
	}
	for range 5 {
		a, sp := R.Range(-0.35, 0.25), R.Range(60, 160)
		ps.Spawn(pixelfx.Ember, 3, MY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.25, 0.5), F[5], 1, 0, 0)
	}
	// a puff of gas forward, then a chain of wisps that push out, slow and curl up and back over the muzzle
	for i := range 3 {
		smoke.add(smokeItem{
			x:     3,
			y:     MY,
			vx:    R.Range(70, 120),
			vy:    R.Range(-12, 6),
			r0:    1.6,
			r1:    R.Range(4.5, 6),
			life:  R.Range(0.8, 1.05),
			a:     0.55,
			drag:  3.5,
			rise:  10,
			warm:  0.9,
			delay: float64(i) * 0.012,
		})
	}
	for i := range 14 {
		k := float64(i) / 13
		smoke.add(smokeItem{
			x:     2,
			y:     MY,
			vx:    115 - 75*k + R.Range(-12, 12),
			vy:    -6 - 10*k + R.Range(-5, 5),
			r0:    1.4,
			r1:    (3.6 + k*2.6) * R.Range(0.75, 1.25),
			life:  1.15 + k*0.3,
			a:     0.62,
			b:     R.Range(0.85, 1),
			drag:  2.2,
			rise:  6,
			curl:  -4.4 + k*1.8 + R.Range(-0.5, 0.5),
			grow:  1.6,
			warm:  0.7,
			delay: 0.02 + float64(i)*0.022,
		})
	}
	smoke.add(smokeItem{x: -7, y: MY - 2, vx: -6, vy: -18, r0: 1.2, r1: 4, life: 1.05, a: 0.4, rise: 8, delay: 0.03})
	shellVX, shellVY, shellVA, shellA0 := -R.Range(25, 45), -R.Range(95, 130), R.Sign()*R.Range(22, 32), R.Range(0, 6.28)
	warmRamp := []uint32{SM[0], pixelfx.Mix(F[1], SM[1], 0.5), pixelfx.Mix(F[2], SM[2], 0.4), F[3], F[4], F[5], F[7]}
	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			ps.Update(dt)
			smoke.update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			pop := math.Exp(-(t + 0.008) / 0.035)
			list := smoke.puffs()
			for i := range list {
				list[i].w *= 0.4 + pop
			}
			cloud(p, list, SM, warmRamp, cloudOpts{dens: 1.5, max: 0.8, lit: 1.4})
			// the casing: out of the port, up and back, tumbling, a bounce or two, then it lies there and fades
			if t < muzzlePistolDuration {
				drawCasing(p, casing(-7, MY-1, shellVX, shellVY, shellVA, t, 850, -0.5, 0.42, shellA0), B, math.Min(1, (muzzlePistolDuration-0.05-t)/0.3))
			}
			// the flash: the hero frame with a white core, then one cooler, smaller frame
			if t < 2*f1 {
				f := 2
				if t < f1 {
					f = 1
				}
				L := flashShape(pixelfx.RngOf(shapeSeed), f, flashShapeOpts{size: 1.15, spike: [2]float64{15, 22}, diag: [2]float64{7, 12}, cross: [2]float64{4.5, 8}, back: [2]float64{3, 5.5}})
				cols := [4]uint32{F[7], F[6], F[4], F[2]}
				if f == 1 {
					cols = [4]uint32{pixelfx.White, F[7], F[6], F[3]}
				}
				flash(p, 0, MY, tilt, L, cols, float64(f)+seed)
			}
			ps.Draw(p, pixelfx.LayerBoth)
			// light: the pop, its warm pool, a touch on the floor below
			p.Glow(4, MY, 30, F[4], 0.85*pop)
			p.Glow(2, MY, 8, pixelfx.White, 0.5*pop)
			p.Glow(4, 0, 16, F[4], 0.2*pop)
		},
	}
}
