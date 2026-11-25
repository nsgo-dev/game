// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/grenade.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The blast (fire puffs that cool into smoke, sparks, grit, scorch, rings, lights) is adapted from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Grenade is a grenade lobbed in that bounces, rolls to a stop with its fuse fizzing, shudders, then blows: a white flash, shrapnel streaks, sparks, a small fireball and a scorch. It is registered as "explosions/grenade".
var Grenade = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Grenade",
		Desc:     "A grenade is lobbed in, bounces, rolls to a stop with its fuse fizzing, shudders, then blows: a white flash, shrapnel streaks, sparks, a small fireball and a scorch.",
		Tags:     []string{"explosion", "grenade", "shrapnel", "fire", "smoke", "ground", "one-shot"},
		Size:     [2]int{grenadeW, grenadeH},
		Origin:   [2]int{grenadeOX, grenadeOY},
		Duration: grenadeDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "body", Default: pixelfx.Palette("#141a10", "#2c3a1e", "#4a5c30", "#6e8446", "#9ab06a")},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newGrenade,
}

const grenadeW, grenadeH, grenadeOX, grenadeOY = 300, 200, 160, 170

func init() { pixelfx.Register("explosions/grenade", Grenade) }

// variables, so sums of them round as pixelfx's do
var grenadeDuration, grenadeBlow, grenadeRB = 4.2, 1.62, 6.2

var (
	groundDust  = pixelfx.Hex("#7d7280")
	groundGrit  = []uint32{pixelfx.Hex("#5a4f6b"), pixelfx.Hex("#3e3650"), pixelfx.Hex("#776c88")}
	groundRing  = []uint32{pixelfx.Hex("#4a3f69"), pixelfx.Hex("#6e6390")}
	grenadeFuse = pixelfx.Hex("#8a8494")
	casing      = []uint32{pixelfx.Hex("#3a3c46"), pixelfx.Hex("#6a6e7c"), pixelfx.Hex("#a8acb8")}
	grenadeCap  = []uint32{pixelfx.Hex("#2a2c36"), pixelfx.Hex("#5e6270"), pixelfx.Hex("#9a9eac"), pixelfx.Hex("#d0d4de")}
)

// lobbed is the grenade's flight state.
type lobbed struct {
	x, y, vx, vy, a, va float64
	rest                bool
}

func newGrenade(o *pixelfx.Options) pixelfx.Instance {
	F := pixelfx.Fit(o.Palette(), 8)
	FR := fireRamp(F)
	HR, G, R := heatRamp(FR), pixelfx.Fit(o.Ramp("body"), 5), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	const W, H, OX, OY = grenadeW, grenadeH, grenadeOX, grenadeOY
	const X0, X1, Y0, Y1 = -OX + 3, W - OX - 3, -OY + 3, H - OY - 3
	ps, lights, bits := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{F[6], F[5], F[3], F[2]}}), &pixelfx.Lights{}, pixelfx.NewBits(floor)
	// the grenade: lobbed from the left, it bounces, rolls and settles
	g := lobbed{x: -140, y: -70, vx: 168, vy: -150, va: -9}
	t, blown, bx := 0.0, false, 0.0
	RB := grenadeRB
	ringCols := []uint32{groundRing[0], groundRing[1], F[3], F[6]}

	blast := func() {
		blown = true
		bx = g.x
		y := -5.0
		lights.Add(bx, y-10, 70, pixelfx.Mix(F[3], F[6], 0.45), 0.85, 0.5)
		lights.Add(bx, y, 30, pixelfx.White, 0.7, 0.07)
		// shrapnel: hot streaks flying straight and far, sparks that fall and bounce
		for range 38 {
			a, sp := R.Range(-pi+0.04, -0.04), R.Range(380, 640)
			ps.Spawn(pixelfx.Mote, bx, y, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.12, 0.26), pixelfx.Pick(R, []uint32{F[7], F[6], F[5]}), R.Range(0.6, 0.9), 0, 0)
		}
		for range 60 {
			a, sp := R.Range(0, tau), R.Range(140, 360)
			ps.Spawn(pixelfx.Spark, bx, y, math.Cos(a)*sp, math.Sin(a)*sp-60, R.Range(0.2, 0.55), pixelfx.White, 1, 0, 0)
		}
		// a small fireball that cools into smoke
		for range 4 {
			ps.Spawn(pixelfx.Fire, bx+R.Range(-2, 2), y+R.Range(-2, 2), R.Range(-15, 15), R.Range(-30, -8), R.Range(0.16, 0.26), 0, R.Range(6, 8), 0, 0)
		}
		for range 16 {
			a, d, sp := R.Range(-pi-0.2, 0.2), R.Range(0, 6), R.Range(40, 150)
			ps.Spawn(pixelfx.Fire, bx+math.Cos(a)*d, y+math.Sin(a)*d, math.Cos(a)*sp, math.Sin(a)*sp*0.8-30, R.Range(0.45, 0.9), 0, R.Range(3.5, 7), 0, chance(R, 0.5, pixelfx.Smokes))
		}
		for range 9 {
			a, d := R.Range(-pi, 0), R.Range(2, 10)
			ps.Spawn(pixelfx.Smoke, bx+math.Cos(a)*d, y+math.Sin(a)*d, math.Cos(a)*25, -R.Range(20, 60), R.Range(1.4, 2.1), 0, R.Range(6, 10), R.Range(0.05, 0.3), pixelfx.Big)
		}
		for range 30 {
			a, sp := R.Range(0, tau), R.Range(30, 120)
			ps.Spawn(pixelfx.Ember, bx, y, math.Cos(a)*sp, math.Sin(a)*sp-50, R.Range(0.8, 1.8), F[3], 1, 0, 0)
		}
		// the casing in pieces, and the floor's grit and dust
		for range 12 {
			a, sp := R.Range(-pi+0.2, -0.2), R.Range(150, 330)
			var b pixelfx.Bit
			b.Kind = pixelfx.Shard
			b.X, b.Y = bx, y
			b.VX, b.VY = math.Cos(a)*sp, math.Sin(a)*sp
			b.G, b.Drag, b.Bounce = 1, 0.5, 0.3
			b.Life = R.Range(1.4, 2.1)
			b.A = R.Range(0, tau)
			b.VA = R.Range(-24, 24)
			b.S = R.Range(2, 3.5)
			b.C = pixelfx.Pick(R, casing)
			bits.Add(b)
		}
		for range 70 {
			u := R.Range(-1, 1)
			a, sp := -pi/2+u*0.8, R.Range(100, 300)*(1-0.4*math.Abs(u))
			ps.Spawn(pixelfx.Grit, bx+u*4, -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1, 1.8), pixelfx.Pick(R, groundGrit), pick(R.Chance(0.35), 2.0, 1), 0, 0)
		}
		for range 10 {
			s := R.Sign()
			ps.Spawn(pixelfx.Smoke, bx+s*R.Range(0, 6), -R.Range(1, 4), s*R.Range(60, 180), -R.Range(2, 10), R.Range(1, 1.7), groundDust, R.Range(5, 8), R.Range(0, 0.05), pixelfx.Big)
		}
	}

	band := func(p *pixelfx.Painter, keep func(float64) bool) {
		if k := (t - grenadeBlow) / 0.4; k < 1 {
			pixelfx.Band(p, bx, 0.5, 70*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 3.5*(1-k)), 1.15-k, ringCols, keep, false)
		}
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !blown {
				// the lob, the bounces, the roll
				if !g.rest {
					g.vy += 900 * dt
					g.x += g.vx * dt
					g.y += g.vy * dt
					if g.y >= -RB && g.vy > 0 {
						g.y = -RB
						if g.vy > 90 {
							// a clink, a puff of dust
							for range 4 {
								ps.Spawn(pixelfx.Dust, g.x+R.Range(-2, 2), -0.5, R.Sign()*R.Range(10, 40), -R.Range(4, 16), R.Range(0.3, 0.6), groundDust, R.Range(1.5, 2.6), 0, 0)
							}
							for range 3 {
								ps.Spawn(pixelfx.Spark, g.x, -1, R.Range(-60, 60), -R.Range(40, 120), R.Range(0.08, 0.16), pixelfx.White, 1, 0, 0)
							}
							g.vy = -g.vy * 0.36
							g.vx *= 0.8
						} else {
							g.vy = 0
						}
					}
					if g.y >= -RB-0.01 && g.vy == 0 {
						g.vx -= g.vx * 2.4 * dt
						g.va = -g.vx / RB
						if math.Abs(g.vx) < 3 {
							g.rest, g.vx = true, 0
						}
					}
					g.a += g.va * dt
				}
				// the fuse: sparks spitting off its tip, a thread of smoke, faster as it runs down
				tx, ty := grenadeTip(g.x, g.y, g.a)
				hurry := pixelfx.Smooth(grenadeBlow-0.5, grenadeBlow, t)
				if R.Chance((24 + 50*hurry) * dt) {
					ps.Spawn(pixelfx.Spark, tx, ty, R.Range(-50, 50), -R.Range(30, 110), R.Range(0.08, 0.2), pixelfx.White, 1, 0, 0)
				}
				if R.Chance(9 * dt) {
					ps.Spawn(pixelfx.Vapour, tx, ty-1, R.Range(-4, 4), -R.Range(10, 20), R.Range(0.5, 0.9), grenadeFuse, R.Range(1.2, 2), 0, 0)
				}
				if t >= grenadeBlow {
					blast()
				}
			}
			bits.Update(dt)
			bits.List = slices.DeleteFunc(bits.List, func(b pixelfx.Bit) bool { return b.X < X0+3 || b.X > X1-3 || b.Y < Y0+3 })
			ps.Update(dt)
			cull(ps, X0, Y0, X1, Y1)
			fadeEdges(ps, X0, X1, 24, Y1)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			if blown {
				u := t - grenadeBlow
				scorch(p, HR, u, 22, 1.6, 1-pixelfx.Smooth(grenadeDuration-grenadeBlow-0.8, grenadeDuration-grenadeBlow-0.15, u), 2)
				band(p, farHalf)
			}
			drawPs(p, ps, pixelfx.LayerBack, FR)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if !blown {
				// the grenade, shuddering in its last half second, the fuse glowing
				shake := pixelfx.Smooth(grenadeBlow-0.45, grenadeBlow, t)
				jx := shake * math.Sin(t*90) * 0.8
				drawGrenade(p, G, g.x+jx, g.y, g.a)
				tx, ty := grenadeTip(g.x+jx, g.y, g.a)
				p.Px(tx, ty, pick(math.Sin(t*50) > 0, F[6], F[5]))
				p.Glow(tx, ty, 6+3*shake, F[4], 0.5+0.3*math.Sin(t*37))
			} else {
				band(p, nearHalf)
			}
			bits.Draw(p)
			drawPs(p, ps, pixelfx.LayerFront, FR)
			if blown {
				u := t - grenadeBlow
				// the flash: a hot disc for two frames, a flare, a ring of light
				if u < 0.07 {
					k2 := 1 - u/0.07
					r := 11 * (0.8 + 0.2*k2)
					p.Circle(bx, -6, r+1.2, F[2], 1)
					p.Circle(bx, -6, r, pick(k2 > 0.45, F[6], F[5]), 1)
					p.Circle(bx, -6, r*0.6, pick(k2 > 0.45, pixelfx.White, F[6]), 1)
				}
				if u < 0.2 {
					pixelfx.Flare(p, bx, -6, 40*math.Pow(1-u/0.2, 1.4), 1-u/0.2, F)
				}
				if u < 0.3 {
					k3 := u / 0.3
					e := 1 - math.Pow(1-k3, 3)
					r := 8 + 40*e
					p.RingFn(bx, -6, r, math.Max(1, 2.5*(1-k3)+0.4), func(_, an float64) (uint32, float64) {
						if math.Sin(an)*r > 9 {
							return F[5], 0
						}
						return F[5], math.Min(1, (1-k3)*1.6)
					})
				}
				pool(p, bx, 1, 70, 12, F[3], 0.55*math.Max(0, 1-u/0.6))
			}
			lights.Draw(p)
		},
	}
}

// grenadeTip returns where the fuse's tip is: on top of the grenade at (x, y), turning with it.
func grenadeTip(x, y, a float64) (float64, float64) {
	ca, sa := math.Cos(a), math.Sin(a)
	return x + sa*10.4, y - ca*10.4
}

// drawGrenade draws a pineapple grenade turned by a (about 11 x 15 px): an oval body with grooves, lit from the top left, an outline, a metal cap with a ring pin, a lever down its side.
func drawGrenade(p *pixelfx.Painter, G []uint32, x, y, a float64) {
	ca, sa, M := math.Cos(a), math.Sin(a), grenadeCap
	p.Shade(x-13, y-13, x+13, y+13, func(px, py float64, _ *pixelfx.Out) uint32 {
		dx, dy := px-x, py-y
		u, v := dx*ca+dy*sa, -dx*sa+dy*ca // u across, v along (cap at v < 0)
		// the cap, its ring, and the lever pressed down the side
		if v < -5 && v > -10.2 && math.Abs(u) < 2.4 {
			switch {
			case v < -9.4 || math.Abs(u) > 1.7:
				return M[0]
			case u < -0.6:
				return M[3]
			}
			return M[2]
		}
		if pixelfx.Hypot(u+3.4, v+8) < 1.9 && pixelfx.Hypot(u+3.4, v+8) > 0.9 {
			return M[2]
		}
		if u > 2.2 && u < 3.6 && v > -8.6 && v < 2 {
			return pick(u > 3, M[0], M[1])
		}
		eu, ev := u/5.1, (v-0.6)/6.4
		e := eu*eu + ev*ev
		if e > 1.2 {
			return 0
		}
		if e > 1 {
			return outline
		}
		// grooves cut the body into knobbly segments; light from the top left in world space
		lit := -(dx*0.6 + dy*0.8) / 6
		gv, gu := math.Abs(math.Mod(v+30, 3.1)-1.55) < 0.45, math.Abs(math.Mod(u+30, 3.4)-1.7) < 0.45
		i := math.Max(0, math.Min(4, pixelfx.Round(2+lit*1.7-pick(gv || gu, 1.0, 0)+pick(e < 0.3 && lit > 0.3, 1.0, 0))))
		return G[int(i)]
	})
}
