// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/muzzle-shotgun.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flash lobes, spark, smoke and casing recipes from Sprite Fusion's open-source muzzle-flash (https://www.spritefusion.com/pixel-art-vfx/muzzle-flash), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// MuzzleShotgun is a shotgun blast from a muzzle height px above the floor (the origin): a wide ragged fan of flame, pellet streaks, a heavy rolling smoke cloud, a red shell kicked out on the pump. It is registered as "projectiles/muzzle-shotgun".
var MuzzleShotgun = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Shotgun blast",
		Desc:     "A shotgun blast from a muzzle `height` px above the floor (the origin): a wide ragged fan of flame, pellet streaks, a heavy rolling smoke cloud, a red shell kicked out on the pump.",
		Tags:     []string{"projectile", "muzzle", "gun", "shotgun", "flash", "smoke", "casing", "directional", "one-shot"},
		Size:     [2]int{160, 88},
		Origin:   [2]int{44, 82},
		Duration: muzzleShotgunDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "smoke", Default: pixelfx.Palette("#221e2a", "#36313f", "#4e4858", "#6a6474", "#888192", "#a8a1b1", "#c8c1cf")},
			{Name: "shell", Default: pixelfx.Palette("#4a0f14", "#9a1e22", "#d8423a", "#8c5c1c", "#e9b54a")},
			{Name: "height", Default: pixelfx.Num(26)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newMuzzleShotgun,
}

func init() { pixelfx.Register("projectiles/muzzle-shotgun", MuzzleShotgun) }

const (
	muzzleShotgunDuration = 3.2
	muzzleShotgunSizeX    = 160
	muzzleShotgunOriginX  = 44
)

// shotgunFan builds the shotgun blast's fan: a fat fireball at the muzzle and a spray of ragged tongues across the cone.
func shotgunFan(R *[32]float64, f int) []flashLobe {
	k, w := 0.55, 1.25
	if f == 1 {
		k, w = 1, 1
	} else if f == 2 {
		k = 0.82
	}
	L := make([]flashLobe, 0, 12)
	L = append(L, flashLobe{off: 0.5, a: 0, len: 11 * k, wid: 3.6 * w, core: 0.5})
	core := 0.0
	if f == 1 {
		core = 0.32
	}
	for i := range 9 {
		u := float64(i)/8 - 0.5
		a := u*1.15 + R[i*3]*0.12
		ln := (17 + 11*(1-math.Abs(u)*1.4) + R[i*3+1]*7) * k
		L = append(L, flashLobe{off: 2, a: a, len: ln, wid: (1.7 + R[i*3+2]) * w, core: core})
	}
	if f == 1 {
		for _, s := range [2]float64{-1, 1} {
			idx := 27
			if s > 0 {
				idx = 28
			}
			L = append(L, flashLobe{off: 1, a: s * (1.6 + R[idx]*0.4), len: 5 + R[29]*3, wid: 1.2, core: 0})
		}
	}
	return L
}

type shotgunPellet struct{ a, v, d0 float64 }

func newMuzzleShotgun(o *pixelfx.Options) pixelfx.Instance {
	F, SM, SH := pixelfx.Fit(o.Palette(), 9), pixelfx.Fit(o.Ramp("smoke"), 7), pixelfx.Fit(o.Ramp("shell"), 5)
	H, R := o.Num("height"), pixelfx.NewRng(o.Seed())
	MY := -H
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: []uint32{F[7], F[6], F[4], F[3]}})
	smoke := newSmoke(R)
	right := float64(muzzleShotgunSizeX - muzzleShotgunOriginX - 2)
	var jit [32]float64
	for i := range jit {
		jit[i] = R.Next()
	}
	// pellets: a tight spray, gone off the canvas in a few frames, each dragging a hot streak
	var pellets [10]shotgunPellet
	for i := range pellets {
		pellets[i] = shotgunPellet{a: R.Range(-0.13, 0.13), v: R.Range(900, 1150), d0: R.Range(0, 4)}
	}
	// sparks and burning wad, flung wide
	for range 16 {
		a, sp := R.Range(-0.55, 0.55), R.Range(180, 460)
		ps.Spawn(pixelfx.Spark, 4, MY, math.Cos(a)*sp, math.Sin(a)*sp-20, R.Range(0.08, 0.3), pixelfx.White, 1, 0, 0)
	}
	for range 7 {
		a, sp := R.Range(-0.5, 0.3), R.Range(70, 200)
		ps.Spawn(pixelfx.Ember, 4, MY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.4, 0.9), F[5], 1, 0, 0)
	}
	// the smoke: a heavy shove of gas forward that slows, rolls, swells and lifts; the first of it lit by the blast
	for i := range 26 {
		a := R.Range(-0.45, 0.35)
		sp := R.Range(60, 230) * pick(i < 8, 1.15, 0.8)
		smoke.add(smokeItem{
			x:     4 + R.Range(0, 4),
			y:     MY + R.Range(-1.5, 1.5),
			vx:    math.Cos(a) * sp,
			vy:    math.Sin(a) * sp,
			r0:    R.Range(2.5, 3.5),
			r1:    R.Range(7, 12.5),
			life:  R.Range(2.1, 2.9),
			a:     R.Range(0.55, 0.72),
			b:     R.Range(0.9, 1),
			drag:  R.Range(2.6, 3.6),
			rise:  R.Range(9, 15),
			grow:  R.Range(1.8, 2.8),
			warm:  1,
			delay: R.Range(0, 0.04),
		})
	}
	for i := range 6 {
		smoke.add(smokeItem{
			x:     2,
			y:     MY,
			vx:    R.Range(15, 50),
			vy:    -R.Range(10, 25),
			r0:    1.5,
			r1:    R.Range(4, 6),
			life:  R.Range(1.8, 2.4),
			a:     0.5,
			b:     1,
			drag:  2,
			rise:  10,
			curl:  -R.Range(1.5, 3),
			warm:  0.6,
			delay: 0.08 + float64(i)*0.05,
		})
	}
	// the pump throws the spent shell a beat later
	const shellT0 = 0.42
	shellVX, shellVY, shellVA, shellA0 := -R.Range(30, 55), -R.Range(110, 150), R.Sign()*R.Range(16, 24), R.Range(0, 6.28)
	warmRamp := []uint32{SM[0], pixelfx.Mix(F[1], SM[1], 0.5), pixelfx.Mix(F[2], SM[2], 0.4), F[3], F[4], F[5], F[7]}
	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			ps.Update(dt)
			smoke.update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			pop, list := math.Exp(-(t+0.008)/0.05), smoke.puffs()
			for i := range list {
				list[i].w *= 0.25 + pop*1.2
			}
			cloud(p, list, SM, warmRamp, cloudOpts{dens: 1.4, max: 0.86, lit: 1.4})

			// the shell: red hull, brass head, tumbling up and back, bouncing, lying a moment
			if t > shellT0 && t < muzzleShotgunDuration-0.05 {
				k := casing(-9, MY, shellVX, shellVY, shellVA, t-shellT0, 850, -1, 0.35, shellA0)
				ca, sa := math.Cos(k.a), math.Sin(k.a)
				fade := pixelfx.Clamp((muzzleShotgunDuration - 0.05 - t) / 0.3)
				for d := -1.5; d <= 1.5; d += 0.5 {
					var c uint32
					if d > 0.8 {
						c = pick(d > 1.2, SH[4], SH[3])
					} else if d > -0.6 {
						c = SH[2]
					} else {
						c = SH[1]
					}
					p.Dot(k.x+ca*d, k.y+sa*d-0.5, c, fade)
				}
				for d := -1.0; d <= 1; d += 0.5 {
					p.Dot(k.x+ca*d-sa, k.y+sa*d+ca-0.5, pick(d > 0.6, SH[3], SH[0]), fade)
				}
				if k.hit >= 0 && k.hit < 0.07 {
					p.Glow(k.x, k.y, 5, SH[4], 0.5*(1-k.hit/0.07))
				}
			}

			// the flash: a white-hot hero frame, then the fan cooling and spreading as the gas balloons
			if t < 3*f1 {
				f := 3
				if t < f1 {
					f = 1
				} else if t < 2*f1 {
					f = 2
				}
				var cols [4]uint32
				switch f {
				case 1:
					cols = [4]uint32{pixelfx.White, F[7], F[6], F[3]}
				case 2:
					cols = [4]uint32{F[7], F[6], F[4], F[2]}
				default:
					cols = [4]uint32{F[5], F[4], F[3], F[1]}
				}
				flash(p, 0, MY, 0, shotgunFan(&jit, f), cols, float64(f+9))
			}
			// the pellets: hot heads with tails that cool to orange, clipped at the canvas edge
			if t < 0.2 {
				for _, q := range pellets {
					head := 6 + q.d0 + q.v*t
					tail := math.Max(4, head-26)
					ca, sa := math.Cos(q.a), math.Sin(q.a)
					for d := tail; d <= head; d += 0.5 {
						x := ca * d
						if x > right {
							break
						}
						u := (d - tail) / math.Max(1, head-tail)
						col := F[4]
						if u > 0.85 {
							col = pixelfx.White
						} else if u > 0.5 {
							col = F[6]
						}
						a := 1.0
						if u <= 0.3 {
							a = u / 0.3
						}
						p.Dot(x, MY+sa*d, col, a)
					}
					if ca*head < right {
						p.Glow(ca*head, MY+sa*head, 4, F[6], 0.5)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			// light: a big pop that floods the floor, a white heart
			p.Glow(8, MY, 46, F[4], 0.95*pop)
			p.Glow(4, MY, 12, pixelfx.White, 0.6*pop)
			p.Glow(10, 0, 30, F[4], 0.28*pop)
		},
	}
}
