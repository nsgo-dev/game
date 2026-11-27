// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/homing-missile.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the blast's particle recipe (fire that cools, sparks, embers, grit, rocks, scorch, floor ring, lights) from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// HomingMissile is a missile that kicks off, arcs up, rolls over and dives onto its target (the origin), its smoke trail left hanging in the air, and hits in a fiery blast of flame, sparks and rocks. It is registered as "projectiles/homing-missile".
var HomingMissile = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Homing missile",
		Desc:     "A missile kicks off, arcs up, rolls over and dives onto its target (the origin), its smoke trail left hanging in the air, and hits in a fiery blast of flame, sparks and rocks.",
		Tags:     []string{"projectile", "missile", "rocket", "homing", "smoke", "explosion", "one-shot"},
		Size:     [2]int{248, 140},
		Origin:   [2]int{160, 120},
		Duration: homingMissileDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "metal", Default: pixelfx.Palette("#25243a", "#3e4058", "#61667f", "#8e97b0", "#c4cce0")},
			{Name: "smoke", Default: pixelfx.Palette("#26222e", "#3b3644", "#544e5e", "#6f6979", "#8c8596", "#aca5b5", "#cbc4d2")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newHomingMissile,
}

func init() { pixelfx.Register("projectiles/homing-missile", HomingMissile) }

const (
	homingMissileDuration = 3.8
	homingMissileFly      = 1.15
)

var (
	// homingPath: up out of the launcher, over the top, and down onto the target, faster all the way
	homingPath    = [4][2]float64{{-130, -22}, {-114, -110}, {-30, -120}, {0, 0}}
	homingMissile = []string{"ff..........", ".fhhhhhhhre.", "nmmmmmmmmrrw", ".fsssssssrd.", "ff.........."}
	homingDustMix = pixelfx.Hex("#968c96")
	homingSoot1   = pixelfx.Hex("#0d0a12")
	homingSoot2   = pixelfx.Hex("#17121d")
	homingOutline = pixelfx.Hex("#1a1422")
)

func homingAt(u float64) (float64, float64) {
	v := 1 - u
	a, b, c, d := v*v*v, 3*v*v*u, 3*v*u*u, u*u*u
	return a*homingPath[0][0] + b*homingPath[1][0] + c*homingPath[2][0] + d*homingPath[3][0],
		a*homingPath[0][1] + b*homingPath[1][1] + c*homingPath[2][1] + d*homingPath[3][1]
}

func homingHeading(u float64) float64 {
	v := 1 - u
	deriv := func(i int) float64 {
		return 3*v*v*(homingPath[1][i]-homingPath[0][i]) + 6*v*u*(homingPath[2][i]-homingPath[1][i]) + 3*u*u*(homingPath[3][i]-homingPath[2][i])
	}
	return math.Atan2(deriv(1), deriv(0))
}

func homingProgress(t float64) float64 { return math.Pow(pixelfx.Clamp(t/homingMissileFly), 1.45) }

func newHomingMissile(o *pixelfx.Options) pixelfx.Instance {
	F, M, SM := pixelfx.Fit(o.Palette(), 9), pixelfx.Fit(o.Ramp("metal"), 5), pixelfx.Fit(o.Ramp("smoke"), 7)
	R, floor := pixelfx.NewRng(o.Seed()), func(float64) float64 { return 0 }
	st := o.Color("stone")
	stoneLight := pixelfx.Mix(pixelfx.Scale(st, 1.35), pixelfx.White, 0.08)
	stoneBody := st
	stoneShade := pixelfx.Scale(st, 0.64)
	stoneDust := pixelfx.Mix(pixelfx.Scale(st, 1.2), homingDustMix, 0.3)
	gritCols := []uint32{stoneLight, stoneBody, stoneBody, stoneShade}
	bandCols := []uint32{stoneBody, stoneLight, F[4], F[7]}
	warmRamp := []uint32{SM[0], pixelfx.Mix(F[1], SM[1], 0.5), pixelfx.Mix(F[2], SM[2], 0.4), F[3], F[4], F[6], F[7]}

	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{F[7], F[6], F[4], F[3]}})
	smoke, rocks, lights := newSmoke(R), pixelfx.NewBits(floor), &pixelfx.Lights{}
	pal := map[rune]uint32{'f': M[1], 'h': M[4], 'm': M[3], 's': M[1], 'n': M[0], 'r': F[2], 'e': F[3], 'd': F[1], 'w': pixelfx.White}
	t, emit, hit := 0.0, 0.0, false
	lights.Add(homingPath[0][0], homingPath[0][1], 30, F[4], 0.7, 0.25)
	for range 6 {
		smoke.add(smokeItem{
			x:    homingPath[0][0] + 2,
			y:    homingPath[0][1] + 2,
			vx:   R.Range(-30, 20),
			vy:   R.Range(-10, 15),
			r0:   2,
			r1:   R.Range(5, 7.5),
			life: R.Range(1.2, 1.8),
			a:    0.6,
			warm: 1,
			rise: 8,
		})
	}

	blast := func() {
		const Y = -6.0
		lights.Add(0, Y-2, 52, cannonWarm, 0.8, 0.8)
		lights.Add(0, Y-2, 30, pixelfx.White, 0.75, 0.1)
		for range 26 {
			a, d, sp := R.Range(0, tau), R.Range(0, 6), R.Range(30, 170)
			ps.Spawn(pixelfx.Fire, math.Cos(a)*d, Y+math.Sin(a)*d, math.Cos(a)*sp, math.Sin(a)*sp*0.75-40, R.Range(0.45, 0.95), 0, R.Range(3.2, 6.5), 0, 0)
		}
		for range 5 {
			ps.Spawn(pixelfx.Fire, R.Range(-3, 3), Y, R.Range(-10, 10), -R.Range(90, 150), R.Range(0.7, 1.1), 0, R.Range(4, 5.5), R.Range(0, 0.08), 0)
		}
		for range 70 {
			a, sp := R.Range(pi+0.12, tau-0.12), R.Range(110, 280)
			ps.Spawn(pixelfx.Spark, 0, Y, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.2, 0.65), pixelfx.White, 1, 0, 0)
		}
		for range 22 {
			a, sp := R.Range(pi, tau), R.Range(30, 110)
			ps.Spawn(pixelfx.Ember, math.Cos(a)*4, Y+math.Sin(a)*4, math.Cos(a)*sp, math.Sin(a)*sp-50, R.Range(0.9, 1.6), F[4], 1, 0, 0)
		}
		for range 16 {
			var b pixelfx.Bit
			b.Kind = pixelfx.Chunk
			b.X = R.Range(-5, 5)
			b.Y = -2
			b.VX = R.Range(-110, 110)
			b.VY = -R.Range(140, 290)
			b.G, b.Drag, b.Bounce = 1, 0.4, 0.3
			b.Life = R.Range(1.5, 2.2)
			b.A = R.Range(0, tau)
			b.VA = R.Range(-16, 16)
			b.S = R.Int(1, 2)
			rocks.Add(b)
		}
		for range 60 {
			u := R.Range(-1, 1)
			a, sp := -pi/2+u*0.55, R.Range(80, 220)*(1-0.4*math.Abs(u))
			ps.Spawn(pixelfx.Grit, u*4, -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1, 1.8), pixelfx.Pick(R, gritCols), pick(R.Chance(0.4), 2.0, 1.0), 0, 0)
		}
		// the smoke: a column off the fire and dust rolling out along the floor, lit by the blast at first
		for i := range 18 {
			a, sp := R.Range(pi*1.15, pi*1.85), R.Range(20, 90)
			smoke.add(smokeItem{
				x:     math.Cos(a) * 4,
				y:     Y + math.Sin(a)*4,
				vx:    math.Cos(a) * sp * 0.7,
				vy:    math.Sin(a)*sp - 45*(float64(i)/18),
				r0:    3,
				r1:    R.Range(8, 14),
				life:  R.Range(1.7, 2.2),
				a:     0.62,
				b:     0.88,
				warm:  1,
				rise:  30 + float64(i)*2,
				drag:  1.8,
				delay: R.Range(0.05, 0.35),
			})
		}
		for range 12 {
			s := R.Sign()
			smoke.add(smokeItem{
				x:    s * R.Range(0, 6),
				y:    -3,
				vx:   s * R.Range(60, 150),
				vy:   -R.Range(3, 12),
				r0:   2.5,
				r1:   R.Range(6, 9),
				life: R.Range(1.1, 1.7),
				a:    0.5,
				b:    0.75,
				warm: 0.3,
				rise: 6,
				drag: 3,
			})
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < homingMissileFly {
				// the trail: puffs left behind along the path, hanging where they're dropped, swelling and thinning
				u := homingProgress(t)
				x, y := homingAt(u)
				h := homingHeading(u)
				cx, cy := math.Cos(h), math.Sin(h)
				px, py := homingAt(homingProgress(math.Max(0, t-dt)))
				// one puff every pixel and a half of flight, wherever along this step it fell, so the trail has no beads
				for emit += pixelfx.Hypot(x-px, y-py) / 1.5; emit > 1; emit-- {
					f := (emit - 1) / math.Max(1e-6, pixelfx.Hypot(x-px, y-py)/1.5)
					ex, ey := x+(px-x)*f, y+(py-y)*f
					smoke.add(smokeItem{
						x:    ex - cx*7 + R.Range(-0.6, 0.6),
						y:    ey - cy*7 + R.Range(-0.6, 0.6),
						vx:   -cx*R.Range(10, 30) + R.Range(-6, 6),
						vy:   -cy*R.Range(10, 30) + R.Range(-6, 6),
						r0:   1.4,
						r1:   R.Range(3.5, 6),
						life: R.Range(1.5, 2.3),
						a:    0.5,
						b:    0.9,
						warm: 0.8,
						rise: 4,
						drag: 3,
						grow: 1.2,
					})
				}
				if R.Chance(0.35) {
					a, sp := h+pi+R.Range(-0.5, 0.5), R.Range(40, 140)
					ps.Spawn(pixelfx.Spark, x-cx*7, y-cy*7, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.15, 0.35), pixelfx.White, 1, 0, 0)
				}
			} else if !hit {
				hit = true
				blast()
			}
			for i := range rocks.List {
				b := &rocks.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 2 {
						x := b.X + R.Range(-1, 1)
						vx := R.Sign() * R.Range(8, 30)
						ps.Spawn(pixelfx.Dust, x, -0.5, vx, -R.Range(2, 14), R.Range(0.3, 0.6), stoneDust, R.Range(1.2, 2.2), 0, 0)
					}
				}
			}
			ps.Update(dt)
			smoke.update(dt)
			rocks.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the scorch: white-hot where it hit, cooling through the fire to soot, fading late
			if hit {
				k := t - homingMissileFly
				fade := 1 - pixelfx.Smooth(homingMissileDuration-1, homingMissileDuration-0.2, t)
				rx := 16 * math.Min(1, k/0.06)
				for y := 0.0; y <= 2; y++ {
					for x := math.Floor(-rx); x <= math.Ceil(rx); x++ {
						d := pixelfx.Hypot((x+0.5)/rx, (y+0.5)/4)
						if d > 1 {
							continue
						}
						T := math.Pow(pixelfx.Clamp(1-k/1.4), 1.4)*(1.25-d) - pixelfx.Rand(x*31+y, 3)*0.35
						var c uint32
						if T > 0.06 {
							c = F[int(math.Max(1, math.Min(8, pixelfx.Round(T*8))))]
						} else if d < 0.5 {
							c = homingSoot1
						} else {
							c = homingSoot2
						}
						p.Dot(x, y, c, fade)
					}
				}
				q := k / 0.45
				if q < 1 {
					pixelfx.Band(p, 0, 0.5, 60*(1-math.Pow(1-q, 3)), 0.16, math.Max(1, 4*(1-q)), 1.15-q, bandCols, func(g float64) bool { return math.Sin(g) < 0 }, false)
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			list := smoke.puffs()
			cloud(p, list, SM, warmRamp, cloudOpts{dens: 1.4, max: 0.82, lit: 1.4})
			if hit {
				q := (t - homingMissileFly) / 0.45
				if q < 1 {
					pixelfx.Band(p, 0, 0.5, 60*(1-math.Pow(1-q, 3)), 0.16, math.Max(1, 4*(1-q)), 1.15-q, bandCols, func(g float64) bool { return math.Sin(g) >= 0 }, false)
				}
			}
			for i := range rocks.List {
				b := &rocks.List[i]
				r, fade := 0.6+b.S*0.75, math.Min(1, b.Life/0.5)
				lx, ly := math.Cos(b.A)*r*0.4, math.Sin(b.A)*r*0.4
				p.Shade(b.X-r-1, b.Y-r-1, b.X+r+1, b.Y+r+1, func(x, y float64, out *pixelfx.Out) uint32 {
					dx, dy := x-b.X, y-b.Y
					d := pixelfx.Hypot(dx*1.15, dy)
					if d > r+0.9 {
						return 0
					}
					lit := dx*lx + dy*ly
					switch {
					case d > r:
						out.C = homingOutline
					case lit < -0.4:
						out.C = stoneLight
					case lit > 0.6:
						out.C = stoneShade
					default:
						out.C = stoneBody
					}
					out.A = fade
					return 0
				})
			}
			ps.Draw(p, pixelfx.LayerBoth)
			if t < homingMissileFly {
				u := homingProgress(t)
				x, y := homingAt(u)
				h := homingHeading(u)
				cx, cy, fr := math.Cos(h), math.Sin(h), math.Floor(t*30)
				// the motor: a short white-hot jet that flickers, the missile riding its heading
				J := 5 + 3*pixelfx.Rand(fr, 7)
				for d := 0.0; d <= J; d += 0.5 {
					v := d / J
					c := F[4]
					if v < 0.3 {
						c = pixelfx.White
					} else if v < 0.6 {
						c = F[6]
					}
					p.Dot(x-cx*(6.5+d), y-cy*(6.5+d), c, 1)
					if v < 0.5 {
						p.Dot(x-cx*(6.5+d)-cy*0.8, y-cy*(6.5+d)+cx*0.8, F[5], 1)
					}
				}
				turnSprite(p, homingMissile, pal, x, y, h, 6, 2.5, 1)
				p.Glow(x-cx*8, y-cy*8, 18, F[4], 0.5)
				p.Glow(x-cx*7, y-cy*7, 6, F[7], 0.5)
			}
			// the hit's flash: a white ball for two frames and a ring of flame
			k := t - homingMissileFly
			if k >= 0 && k < 0.07 {
				r := 14 * (0.8 + 0.2*(1-k/0.07))
				p.Circle(0, -6, r+1.2, F[3], 1)
				p.Circle(0, -6, r, pick(k < 0.035, F[7], F[6]), 1)
				p.Circle(0, -6, r*0.6, pick(k < 0.035, pixelfx.White, F[7]), 1)
			}
			if k >= 0 && k < 0.3 {
				rr := 8 + 30*(1-math.Pow(1-k/0.3, 3))
				p.RingFn(0, -6, rr, math.Max(1, 2.5*(1-k/0.3)+0.4), func(_, ang float64) (uint32, float64) {
					if -6+math.Sin(ang)*rr > 0 {
						return F[5], 0
					}
					return F[5], math.Min(1, (1-k/0.3)*1.6)
				})
			}
			lights.Draw(p)
		},
	}
}
