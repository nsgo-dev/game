// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/flare.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the spark, star and light recipes from Sprite Fusion's open-source explosion and sparkle (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Flare is a signal flare that streaks up off the ground trailing sparks and smoke, bursts, and hangs as a pulsing red star that drips sparks under its own red-lit smoke, then gutters out. It is registered as "projectiles/flare".
var Flare = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Signal flare",
		Desc:     "A signal flare streaks up off the ground trailing sparks and smoke, bursts, and hangs as a pulsing red star that drips sparks under its own red-lit smoke, then gutters out.",
		Tags:     []string{"projectile", "flare", "signal", "fire", "smoke", "light", "one-shot"},
		Size:     [2]int{152, 232},
		Origin:   [2]int{76, 226},
		Duration: 5,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a0610", "#5c0d1c", "#9a1428", "#d8243a", "#ff4d5e", "#ff8a94", "#ffd0d4", "#ffffff")},
			{Name: "smoke", Default: pixelfx.Palette("#1e1a26", "#2e2938", "#433d4d", "#5b5466", "#77707f", "#958e9c", "#b3acb9")},
			{Name: "height", Default: pixelfx.Num(150)},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newFlare,
}

func init() { pixelfx.Register("projectiles/flare", Flare) }

const (
	flareRise = 0.85
	flareBurn = 3.1
)

func newFlare(o *pixelfx.Options) pixelfx.Instance {
	C, SM := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("smoke"), 7)
	H, R := o.Num("height"), pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: []uint32{pixelfx.White, C[6], C[4], C[2]}})
	smoke, lights := newSmoke(R), &pixelfx.Lights{}
	WR := []uint32{C[0], C[1], pixelfx.Mix(C[2], SM[2], 0.45), pixelfx.Mix(C[3], SM[3], 0.35), C[4], C[5], C[6]}
	// the rise: fast off the ground, slowing to the top; then the star hangs, sways and sinks slowly
	rise := func(tt float64) (float64, float64) {
		return math.Sin(tt*3) * 2 * (tt / flareRise), -H * pixelfx.Ease.OutCubic(pixelfx.Clamp(tt/flareRise))
	}
	star := func(tt float64) (float64, float64) {
		u := tt - flareRise
		rx, _ := rise(flareRise)
		return math.Sin(u*1.6)*3 + rx, -H + u*9 + math.Sin(u*2.3)*1.5
	}
	t, burst, emit, drip := 0.0, false, 0.0, 0.0
	lights.Add(0, -3, 26, C[4], 0.8, 0.25)
	for range 5 {
		smoke.add(smokeItem{
			x:    R.Range(-3, 3),
			y:    -2,
			vx:   R.Range(-25, 25),
			vy:   -R.Range(5, 20),
			r0:   2,
			r1:   R.Range(4, 6),
			life: R.Range(1, 1.6),
			a:    0.5,
			warm: 0.8,
			rise: 6,
		})
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < flareRise {
				x, y := rise(t)
				px, py := rise(math.Max(0, t-dt))
				for emit += pixelfx.Hypot(x-px, y-py) / 1.5; emit > 1; emit-- {
					smoke.add(smokeItem{
						x:    x + R.Range(-0.6, 0.6),
						y:    y + 4,
						vx:   R.Range(-6, 6),
						vy:   R.Range(0, 8),
						r0:   1.3,
						r1:   R.Range(3, 4.8),
						life: R.Range(1.4, 2.1),
						a:    0.5,
						b:    0.95,
						warm: 0.9,
						rise: 3,
						drag: 2,
						grow: 1.4,
					})
				}
				if R.Chance(0.5) {
					ps.Spawn(pixelfx.Spark, x, y+3, R.Range(-40, 40), R.Range(20, 90), R.Range(0.2, 0.45), pixelfx.White, 1, 0, 0)
				}
			} else if !burst {
				burst = true
				x, y := star(t)
				lights.Add(x, y, 64, C[4], 0.9, 0.5)
				lights.Add(x, y, 24, pixelfx.White, 0.8, 0.12)
				for range 30 {
					a, sp := R.Range(0, tau), R.Range(70, 190)
					ps.Spawn(pixelfx.Spark, x, y, math.Cos(a)*sp, math.Sin(a)*sp-30, R.Range(0.25, 0.7), pixelfx.White, 1, 0, 0)
				}
				for range 10 {
					smoke.add(smokeItem{
						x:    x + R.Range(-3, 3),
						y:    y + R.Range(-3, 3),
						vx:   R.Range(-40, 40),
						vy:   R.Range(-40, 10),
						r0:   2.5,
						r1:   R.Range(6, 9),
						life: R.Range(1.6, 2.4),
						a:    0.55,
						warm: 1,
						rise: 10,
						drag: 2.5,
					})
				}
			} else if t < flareRise+flareBurn {
				// the star burns: sparks drip off it, smoke boils up off it into a plume it lights from below
				x, y := star(t)
				k := (t - flareRise) / flareBurn
				power := 1 - pixelfx.Smooth(0.75, 1, k)
				for drip += dt * 26 * power; drip > 1; drip-- {
					ps.Spawn(pixelfx.Spark, x+R.Range(-1.5, 1.5), y+2, R.Range(-30, 30), R.Range(10, 60), R.Range(0.35, 0.8), pixelfx.White, 1, 0, 0)
				}
				if R.Chance(dt * 18 * power) {
					smoke.add(smokeItem{
						x:    x + R.Range(-1.5, 1.5),
						y:    y - 2,
						vx:   R.Range(-8, 8),
						vy:   -R.Range(15, 30),
						r0:   2,
						r1:   R.Range(6, 10),
						life: R.Range(1.6, 2.4) * (1 - 0.4*k),
						a:    0.5,
						b:    0.85,
						warm: 1,
						rise: 14,
						drag: 1.6,
						grow: 1.2,
					})
				}
			}
			ps.Update(dt)
			smoke.update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the smoke lit red by the star, more the closer and the brighter it burns
			list := smoke.puffs()
			lit := 0.0
			if t > flareRise && t < flareRise+flareBurn {
				lit = 1 - pixelfx.Smooth(0.75, 1, (t-flareRise)/flareBurn)
			}
			if lit > 0 {
				sx, sy := star(t)
				for i := range list {
					list[i].w = math.Max(list[i].w, lit*math.Exp(-pixelfx.Hypot(list[i].x-sx, list[i].y-sy)/26))
				}
			}
			cloud(p, list, SM, WR, cloudOpts{dens: 1.4, max: 0.8, lit: 1.3})
			ps.Draw(p, pixelfx.LayerBoth)
			if t < flareRise {
				// the rising flare: a white-hot head, a short red flame under it
				x, y := rise(t)
				p.LineFn(x, y+1, x, y+5, func(u float64) (uint32, float64) {
					if u < 0.3 {
						return C[6], 1
					}
					if u < 0.7 {
						return C[4], 1
					}
					return C[3], 1
				})
				p.Px(x, y, pixelfx.White)
				p.Px(x, y-1, C[6])
				p.Glow(x, y, 14, C[4], 0.6)
				p.Glow(x, y, 4, pixelfx.White, 0.4)
			} else if t < flareRise+flareBurn {
				x, y := star(t)
				u := t - flareRise
				k, fr := u/flareBurn, math.Floor(t*30)
				fl := 0.8 + 0.2*pixelfx.Rand(fr, 3) + 0.08*math.Sin(u*19)
				die := 1 - pixelfx.Smooth(0.78, 1, k)
				// the burst: a flash of white and rays for a moment
				if u < 0.25 {
					e := u / 0.25
					pixelfx.Flare(p, x, y, 22*(1-e)+3, 1-e, C)
					if e < 0.25 {
						p.Circle(x, y, 5*(1-e*3)+1, pixelfx.White, 1)
					}
				}
				// the star: a hot ball with a long cross of light that pulses, guttering at the end
				r := (2 + 0.6*fl) * (0.4 + 0.6*die)
				p.Circle(x, y, r+1.2, C[3], 1)
				p.Circle(x, y, r, pick(die > 0.4, C[5], C[4]), 1)
				if die > 0.25 {
					p.Circle(x-0.3, y-0.3, r*0.55, pick(die > 0.6, pixelfx.White, C[6]), 1)
				}
				if die > 0.15 && pixelfx.Rand(fr, 9) > 0.15 {
					pixelfx.Sparkle(p, x, y, pixelfx.Round((5+3*fl)*die), C[6], C[4], 0.5*die)
				}
				p.Glow(x, y, 70*(0.6+0.4*die), C[3], 0.55*fl*die)
				p.Glow(x, y, 22, C[4], 0.5*fl*die)
				p.Glow(x, y, 6, pixelfx.White, 0.35*die)
			}
			lights.Draw(p)
		},
	}
}
