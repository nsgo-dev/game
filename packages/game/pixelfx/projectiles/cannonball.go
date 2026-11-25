// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/cannonball.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flash, spark and smoke recipes from Sprite Fusion's open-source muzzle-flash and explosion (https://www.spritefusion.com/pixel-art-vfx/muzzle-flash), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Cannonball is a cannon shot fired from a muzzle height px above the floor (the origin): a white flash, a tongue of flame, the ball arcing off trailing smoke, a big gunsmoke cloud rolling out. It is registered as "projectiles/cannonball".
var Cannonball = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Cannon shot",
		Desc:     "A cannon fires from a muzzle `height` px above the floor (the origin): a white flash, a tongue of flame, the ball arcing off trailing smoke, a big gunsmoke cloud rolling out.",
		Tags:     []string{"projectile", "cannon", "cannonball", "smoke", "flash", "directional", "one-shot"},
		Size:     [2]int{208, 116},
		Origin:   [2]int{48, 102},
		Duration: 3.8,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "smoke", Default: pixelfx.Palette("#3a3542", "#55505e", "#736d7c", "#938d9b", "#b3adba", "#d2cdd6", "#ece8ee")},
			{Name: "iron", Default: pixelfx.Palette("#141218", "#2c2a34", "#4c4a58", "#8a8898")},
			{Name: "height", Default: pixelfx.Num(12)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newCannonball,
}

func init() { pixelfx.Register("projectiles/cannonball", Cannonball) }

const (
	cannonballSizeX   = 208
	cannonballOriginX = 48
	cannonballOriginY = 102
)

var (
	cannonAng  = -0.32
	cannonWarm = pixelfx.Hex("#ffb060")
)

func newCannonball(o *pixelfx.Options) pixelfx.Instance {
	F, SM, IR := pixelfx.Fit(o.Palette(), 9), pixelfx.Fit(o.Ramp("smoke"), 7), pixelfx.Fit(o.Ramp("iron"), 4)
	H, R := o.Num("height"), pixelfx.NewRng(o.Seed())
	MY := -H
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: []uint32{F[7], F[6], F[4], F[3]}})
	smoke, lights := newSmoke(R), &pixelfx.Lights{}
	ca, sa := math.Cos(cannonAng), math.Sin(cannonAng)
	right, top := float64(cannonballSizeX-cannonballOriginX-3), float64(-cannonballOriginY+3)
	ballX, ballY, ballVX, ballVY, ballOut := 4.0, MY, ca*330, sa*330, false
	var jit [16]float64
	for i := range jit {
		jit[i] = R.Next()
	}
	lights.Add(12, MY-2, 44, cannonWarm, 1, 0.35)
	lights.Add(6, MY, 18, pixelfx.White, 0.9, 0.08)
	// burning wadding and sparks thrown out with the shot
	for range 22 {
		a, sp := cannonAng+R.Range(-0.45, 0.45), R.Range(140, 380)
		ps.Spawn(pixelfx.Spark, 6, MY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.1, 0.4), pixelfx.White, 1, 0, 0)
	}
	for range 10 {
		a, sp := cannonAng+R.Range(-0.3, 0.3), R.Range(70, 170)
		ps.Spawn(pixelfx.Spark, 6, MY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.5, 0.9), pixelfx.White, 1, 0, 0)
	}
	for range 8 {
		a, sp := cannonAng+R.Range(-0.5, 0.3), R.Range(40, 110)
		ps.Spawn(pixelfx.Ember, 8, MY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.7, 1.4), F[5], 2, 0, 0)
	}
	// the gunsmoke: a great white shove of it out of the bore that slows, rolls over itself, swells and lifts
	for i := range 34 {
		a := cannonAng + R.Range(-0.4, 0.35)
		sp := R.Range(40, 260) * pick(i < 12, 1.1, 0.75)
		smoke.add(smokeItem{
			x:     6 + R.Range(0, 6),
			y:     MY + R.Range(-2, 2),
			vx:    math.Cos(a) * sp,
			vy:    math.Sin(a) * sp,
			r0:    R.Range(3, 4.5),
			r1:    R.Range(9, 16),
			life:  R.Range(2.7, 3.5),
			a:     R.Range(0.6, 0.75),
			b:     R.Range(0.9, 1),
			drag:  R.Range(2.2, 3.2),
			rise:  R.Range(7, 13),
			grow:  R.Range(1.5, 2.4),
			curl:  R.Range(-1.2, 0.4),
			warm:  1,
			delay: R.Range(0, 0.05),
		})
	}
	warmRamp := []uint32{SM[0], pixelfx.Mix(F[1], SM[1], 0.5), pixelfx.Mix(F[2], SM[2], 0.45), F[3], F[4], F[6], F[7]}
	t, wisp := 0.0, 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !ballOut {
				x0, y0 := ballX, ballY
				ballVY += 260 * dt
				ballX += ballVX * dt
				ballY += ballVY * dt
				// the ball leaves a thin wisp of smoke off its hot skin, one puff every two pixels of flight
				for wisp += pixelfx.Hypot(ballX-x0, ballY-y0) / 2; wisp > 1; wisp-- {
					f := R.Next()
					x, y := x0+(ballX-x0)*f, y0+(ballY-y0)*f
					if x < right-8 && x > 24 && t < 0.6 {
						smoke.add(smokeItem{
							x:    x - 3,
							y:    y,
							vx:   -ballVX * 0.04,
							vy:   -ballVY * 0.04,
							r0:   1,
							r1:   R.Range(2.5, 3.6) * (1 - t),
							life: R.Range(0.7, 1.1),
							a:    0.5 * (1 - t),
							b:    0.95,
							drag: 2,
							rise: 5,
							grow: 2,
						})
					}
				}
				if ballX > right || ballY < top {
					ballOut = true
				}
			}
			ps.Update(dt)
			smoke.update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			pop, list := math.Exp(-(t+0.008)/0.06), smoke.puffs()
			for i := range list {
				list[i].w *= 0.2 + pop*1.3
			}
			cloud(p, list, SM, warmRamp, cloudOpts{dens: 1.35, max: 0.86, lit: 1.3})
			ps.Draw(p, pixelfx.LayerBoth)
			// the ball: dark iron, a lit crescent, a hot glint where the flash still burns on it
			if !ballOut {
				x, y := ballX, ballY
				p.Circle(x, y, 3.2, IR[0], 1)
				p.Circle(x, y, 2.6, IR[1], 1)
				p.Circle(x-0.7, y-0.7, 1.8, IR[2], 1)
				p.Px(x-1, y-1.5, IR[3])
				p.Px(x-2, y-1, IR[3])
				p.Px(x+1.5, y+1, pick(t < 0.3, F[5], IR[2]))
				p.Glow(x, y, 6, F[4], 0.45*pixelfx.Clamp(1-t/0.5))
			}
			// the flash: a white ball with a tongue of flame for a frame, then a cooler, bigger burst as the gas spills
			if t < 3*f1 {
				f := 3
				if t < f1 {
					f = 1
				} else if t < 2*f1 {
					f = 2
				}
				k := 0.8 * 1.3
				if f == 1 {
					k = 1 * 1.3
				} else if f == 2 {
					k = 1.15 * 1.3
				}
				L := make([]flashLobe, 0, 8)
				L = append(L,
					flashLobe{off: 0, a: 0, len: 12 * k, wid: 5 * k, core: 0.55},
					flashLobe{off: 2, a: 0, len: (24 + jit[0]*8) * k, wid: 2.2 * k, core: 0.4},
				)
				for i := range 6 {
					s := 1.0
					if i < 3 {
						s = -1
					}
					a := s * (0.4 + float64(i%3)*0.45 + jit[i+1]*0.2)
					down := math.Sin(cannonAng + a)
					ln := (8 + jit[i+7]*7) * k * pick(f == 3, 0.7, 1.0)
					if down > 0.1 {
						ln = math.Min(ln, (H-3)/down)
					}
					L = append(L, flashLobe{off: 2, a: a, len: ln, wid: 1.6 * k, core: 0.25})
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
				flash(p, 4, MY, cannonAng, L, cols, float64(f))
				if f == 1 {
					p.Circle(7, MY-1, 6.5, pixelfx.White, 1)
					p.Ring(7, MY-1, 7.5, 1, F[7], 1)
				}
			}
			lights.Draw(p)
			p.Glow(14, 0, 22, F[4], 0.25*pop)
		},
	}
}
