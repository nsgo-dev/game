// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/big-explosion.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The blast recipe (fire puffs that cool into smoke, sparks, embers, burning brands, grit, rocks, scorch, rings) is adapted from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// BigExplosion is a huge ground blast in three beats: a white dome, a rolling fireball that climbs into a mushrooming smoke column, heavy rocks, and fires left burning. It is registered as "explosions/big-explosion".
var BigExplosion = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Big explosion",
		Desc:     "A huge ground blast in three beats: a white dome, a rolling fireball that climbs into a mushrooming smoke column, heavy rocks, and fires left burning.",
		Tags:     []string{"explosion", "fire", "smoke", "debris", "mushroom", "big", "ground", "one-shot"},
		Size:     [2]int{bigW, bigH},
		Origin:   [2]int{bigOX, bigOY},
		Duration: bigDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "seed", Default: pixelfx.Num(11)},
		},
	},
	New: newBigExplosion,
}

const bigW, bigH, bigOX, bigOY = 360, 300, 180, 262

func init() { pixelfx.Register("explosions/big-explosion", BigExplosion) }

// variables, so products of them round as pixelfx's do
var bigDuration, bigRad, bigPower, bigEarth = 7.2, 30.0, 1.45, 1.6

// bigSmoke is the plume's smoke, lit on top to sooty underneath.
var bigSmoke = pixelfx.Ramp("#83777f", "#6e6470", "#575060", "#3f3947", "#29242f")

var bigBlastSmoke = pixelfx.Hex("#221d26")

type bigBlast struct {
	t, x, y, rad, power float64
	main                bool
}

// three detonations, a beat apart, climbing as they go
var bigBlasts = []bigBlast{
	{t: 0, x: 0, y: -9, rad: bigRad, power: bigPower, main: true},
	{t: 0.13, x: -17, y: -30, rad: 17, power: 0.9},
	{t: 0.25, x: 15, y: -46, rad: 15, power: 0.8},
}

type airFlash struct{ x, y, r, life, max float64 }

type airRing struct {
	x, y, r0, r1, life, max, thick float64
	c                              uint32
}

func newBigExplosion(o *pixelfx.Options) pixelfx.Instance {
	F := pixelfx.Fit(o.Palette(), 8)
	FR := fireRamp(F)
	HR, st, R := heatRamp(FR), stoneOf(o.Color("stone")), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	WARM, SM := pixelfx.Mix(F[3], F[6], 0.45), bigSmoke
	const W, H, OX, OY = bigW, bigH, bigOX, bigOY
	const X0, X1, Y0, Y1 = -OX + 3, W - OX - 3, -OY + 3, H - OY - 3
	RAD, EARTH, D := bigRad, bigEarth, bigDuration
	var blazes *pixelfx.Blazes
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{F[6], F[5], F[3], F[2]}, OnLand: func(x, y float64) {
		blazes.Add(x, y-0.25, R.Range(3, 4.4), R.Range(0.7, 1.25))
	}})
	blazes = pixelfx.NewBlazes(ps)
	rocks, lights := pixelfx.NewBits(floor), &pixelfx.Lights{}
	var flashes []airFlash
	var rings []airRing
	plume := newPlume(R, plumeOpts{
		X: 0, Y: -34, Rise: 118, Tau: 1.25, Drift: 4, Tube: [2]float64{9, 21}, Ring: [2]float64{2, 19}, Grow: 1.0, N: 42, Size: [2]float64{6, 11}, Swell: 0.9,
		Spin: 2.6, SpinTau: 1.7, Heat: 1.2, Cool: 0.42, Under: 0.5, Tilt: 0.22, Squash: 0.72, Fade: [2]float64{4.4, 6.4}, Glow: 1, Spread: 22,
	})
	col := newColumn(R, columnOpts{W: 5, Foot: 18, Speed: 85, Size: [2]float64{7, 11}, N: 28, Sway: 2.5})
	top := func() float64 {
		S := plume.shape(plume.t)
		return S.y + S.tube*0.45
	}
	t, next := 0.0, 0

	blast := func(b bigBlast) {
		x, y, rad, power, main := b.x, b.y, b.rad, b.power, b.main
		// the later beats flash and ring in the air; the first is a dome on the floor (drawn from t)
		if !main {
			flashes = append(flashes, airFlash{x, y, rad * 1.05, 0.06, 0.06})
			rings = append(rings, airRing{x, y, rad * 0.45, rad * 2.7, 0.36, 0.36, 3, F[5]}, airRing{x, y, rad * 0.3, rad * 1.6, 0.24, 0.24, 1.8, F[3]})
		}
		// light: a big warm flare (raised for a ground blast, so it lights the air and the floor gets a pool)
		if main {
			lights.Add(x, y-24, 72, WARM, 0.6*power, 0.8)
			lights.Add(x, y-12, 44, pixelfx.White, 0.6, 0.1)
		} else {
			lights.Add(x, y, rad*3.4, WARM, 0.6*power, 0.8)
			lights.Add(x, y, rad*1.6, pixelfx.White, 0.6, 0.1)
		}
		down := pick(main, 0.25, pi) // a ground blast throws nothing into the floor
		for range 5 {
			ps.Spawn(pixelfx.Fire, x+R.Range(-2.5, 2.5), y+R.Range(-2.5, 2.5), R.Range(-20, 20), R.Range(-40, -10), R.Range(0.18, 0.3), 0, R.Range(rad*0.4, rad*0.55), 0, 0)
		}
		for i := 0; float64(i) < pixelfx.Round(26*power); i++ {
			var a float64
			if main {
				a = R.Range(-pi-down, down)
			} else {
				a = R.Range(0, tau)
			}
			d, sp := R.Range(0, rad*0.55), R.Range(40, 200)*power
			ps.Spawn(pixelfx.Fire, x+math.Cos(a)*d, y+math.Sin(a)*d, math.Cos(a)*sp, math.Sin(a)*sp*0.8-40, R.Range(0.55, 1.15), 0, R.Range(rad*0.22, rad*0.46), 0, chance(R, 0.45, pixelfx.Smokes|pixelfx.Big))
		}
		for range 5 {
			a, d := R.Range(-pi, 0), R.Range(rad*0.1, rad*0.5)
			ps.Spawn(pixelfx.Smoke, x+math.Cos(a)*d, y+math.Sin(a)*d, math.Cos(a)*30, math.Sin(a)*20-30, R.Range(0.9, 1.5), bigBlastSmoke, R.Range(rad*0.15, rad*0.26), R.Range(0.06, 0.2), pixelfx.Big)
		}
		for i := 0; float64(i) < pixelfx.Round(8*power); i++ {
			a, d, sp := R.Range(-pi, 0), R.Range(0, rad*0.7), R.Range(15, 90)
			ps.Spawn(pixelfx.Smoke, x+math.Cos(a)*d, y+math.Sin(a)*d, math.Cos(a)*sp, math.Sin(a)*sp*0.6-35, R.Range(1.8, 2.8), 0, R.Range(rad*0.26, rad*0.4), R.Range(0.12, 0.45), pixelfx.Big)
		}
		for i := 0; float64(i) < pixelfx.Round(80*power); i++ {
			a, sp := R.Range(0, tau), R.Range(170, pick(main, 470.0, 380))
			ps.Spawn(pixelfx.Spark, x, y, math.Cos(a)*sp, math.Sin(a)*sp-70, R.Range(0.2, 0.65), pixelfx.White, 1, 0, 0)
		}
		for i := 0; float64(i) < pixelfx.Round(30*power); i++ {
			a, sp := R.Range(0, tau), R.Range(30, 150)
			ps.Spawn(pixelfx.Ember, x+math.Cos(a)*rad*0.4, y+math.Sin(a)*rad*0.4, math.Cos(a)*sp, math.Sin(a)*sp-75, R.Range(1.4, 3), F[3], 1, 0, 0)
		}
		if !main {
			return
		}
		// burning brands that land and keep the ground on fire
		for range 14 {
			a, sp := -pi/2+R.Range(-1.3, 1.3), R.Range(160, 340)
			ps.Spawn(pixelfx.Brand, x, y, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.8, 1.5), pixelfx.White, 1, 0, 0)
		}
		// the ground's part: dust rolling out along the floor, a dust column, grit and heavy rocks thrown up
		for range 18 {
			s := R.Sign()
			ps.Spawn(pixelfx.Smoke, s*R.Range(0, RAD*0.6), -R.Range(1, 6), s*R.Range(3, 9)*RAD, -R.Range(2, 15), R.Range(1.3, 2.2), st.dust, R.Range(8, 12), R.Range(0, 0.06), pixelfx.Big)
		}
		for i := 0; float64(i) < pixelfx.Round(EARTH*12); i++ {
			ps.Spawn(pixelfx.Smoke, R.Range(-0.3, 0.3)*RAD, -R.Range(2, RAD*0.6), R.Range(-15, 15), -R.Range(60, 190)*EARTH*0.6, R.Range(1.6, 2.6), st.dust, R.Range(8, 12), R.Range(0, 0.15), pixelfx.Big)
		}
		for i := 0; float64(i) < pixelfx.Round(EARTH*RAD*4); i++ {
			u := R.Range(-1, 1)
			a, sp := -pi/2+u*0.55, R.Range(5, 15)*RAD*(1-0.45*math.Abs(u))
			ps.Spawn(pixelfx.Grit, u*0.25*RAD, -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1.4, 2.4), pixelfx.Pick(R, []uint32{st.light, st.body, st.body, st.shade}), pick(R.Chance(0.45), 2.0, 1), 0, 0)
		}
		for i := range 26 {
			big, hot, sx := i < 10, R.Chance(0.45), R.Sign()
			var b pixelfx.Bit
			b.Kind = pixelfx.Chunk
			b.X = R.Range(-0.4, 0.4) * RAD
			b.Y = -4
			b.VX = sx * R.Range(30, pick(big, 120.0, 150))
			b.VY = -R.Range(pick(big, 280.0, 320), pick(big, 480.0, 560))
			b.G, b.Drag, b.Bounce = 1, 0.3, 0.3
			b.Life = R.Range(3.6, 5)
			b.A = R.Range(0, tau)
			b.VA = R.Range(-14, 14)
			if big {
				b.S = R.Range(2.6, 3.8)
			} else {
				b.S = R.Range(1.2, 2.4)
			}
			// a molten rock's heat, in its Data
			if hot {
				b.Data = R.Range(0.6, 1)
			} else {
				b.Data = 0.0
			}
			rocks.Add(b)
		}
	}

	band := func(p *pixelfx.Painter, keep func(float64) bool) {
		if k := t / 0.6; k < 1 {
			pixelfx.Band(p, 0, 0.5, 132*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 6*(1-k)), 1.15-k, []uint32{st.body, st.light, F[3], F[6]}, keep, false)
		}
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for next < len(bigBlasts) && t >= bigBlasts[next].t {
				next++
				blast(bigBlasts[next-1])
			}
			if t > 0.08 {
				plume.update(dt)
			}
			S, k := plume.shape(plume.t), 1-pixelfx.Smooth(2.2, 3.1, t)
			// the column: smoke and fire boil up off the floor into the fireball, dust is drawn in along the ground
			if t > 0.2 && t < 3.1 {
				if R.Chance(8 * k * dt) {
					ps.Spawn(pixelfx.Smoke, R.Range(-10, 10), -R.Range(1, 6), R.Range(-8, 8), -R.Range(40, 80), R.Range(1.5, 2.3), st.dust, R.Range(5, 8), 0, pixelfx.Big)
				}
				if t < 1.5 && R.Chance(16*dt) {
					ps.Spawn(pixelfx.Fire, R.Range(-6, 6), -R.Range(4, 14), R.Range(-10, 10), -R.Range(90, 150), R.Range(0.5, 0.8), 0, R.Range(4, 7.5), 0, chance(R, 0.5, pixelfx.Smokes|pixelfx.Big))
				}
				if t > 0.45 && R.Chance(12*k*dt) {
					s := R.Sign()
					ps.Spawn(pixelfx.Smoke, s*R.Range(40, 120), -R.Range(1, 4), -s*R.Range(10, 30), -R.Range(0, 5), R.Range(1.3, 1.9), st.dust, R.Range(3.5, 5.5), 0, pixelfx.Big)
				}
			}
			// the fireball sheds embers, then ash drifts down out of the cloud
			if t > 0.3 && t < 3 && R.Chance(10*plume.hot(plume.t)*dt) {
				a := R.Range(0, tau)
				ps.Spawn(pixelfx.Ember, S.x+math.Cos(a)*(S.ring+S.tube), S.y+math.Sin(a)*S.tube*0.5, math.Cos(a)*R.Range(40, 90), -R.Range(10, 60), R.Range(1.2, 2.4), F[3], 1, 0, 0)
			}
			if t > 1.4 && t < 4.6 && R.Chance(7*dt) {
				ps.Spawn(pixelfx.Flake, S.x+R.Range(-1, 1)*(S.ring+S.tube), S.y+S.tube*0.6, R.Range(-6, 6), R.Range(0, 10), R.Range(2, 3), pixelfx.Pick(R, []uint32{SM[1], SM[2], F[2]}), R.Range(0.6, 1.2), 0, 0)
			}
			feed := 0.0
			if t > 0.22 && t < 2.5 {
				feed = 1
			}
			col.update(dt, top(), feed)
			draft(ps, dt, draftOpts{X: 0, Top: S.y + S.tube*0.4, W: 9 + 7*math.Min(1, t), Up: 170, Squeeze: 4, Pull: 130, Low: 12, Reach: 130, K: 1 - pixelfx.Smooth(2.5, 3.8, t)})
			for i := len(flashes) - 1; i >= 0; i-- {
				if flashes[i].life -= dt; flashes[i].life <= 0 {
					flashes = slices.Delete(flashes, i, i+1)
				}
			}
			for i := len(rings) - 1; i >= 0; i-- {
				if rings[i].life -= dt; rings[i].life <= 0 {
					rings = slices.Delete(rings, i, i+1)
				}
			}
			// rocks: molten ones cool and trail smoke and flame (Pts holds the trail as x, y, t triples); every hard landing kicks up dust
			for i := range rocks.List {
				rocks.List[i].PVY = rocks.List[i].VY
			}
			rocks.Update(dt)
			for i := len(rocks.List) - 1; i >= 0; i-- {
				b := &rocks.List[i]
				if b.X < X0+4 || b.X > X1-4 {
					rocks.List = slices.Delete(rocks.List, i, i+1)
					continue
				}
				if h := heatOf(b); h > 0 {
					h = math.Max(0, h-dt*0.3)
					b.Data = h
					if !b.Rest && h > 0.25 {
						b.Pts = append(b.Pts, b.X, b.Y, t)
						if R.Chance(14 * dt) {
							ps.Spawn(pixelfx.Fire, b.X, b.Y, b.VX*0.1, b.VY*0.1-8, R.Range(0.16, 0.3), 0, R.Range(1.2, 2.2), 0, 0)
						}
					} else if b.Rest && R.Chance(3*h*dt) {
						ps.Spawn(pixelfx.Smoke, b.X, b.Y-1, R.Range(-3, 3), -R.Range(5, 12), R.Range(0.8, 1.4), 0, R.Range(1.6, 2.6), 0, 0)
					}
				}
				landed := (b.PVY > 0 && b.VY <= 0) || (b.Rest && !b.Dusted)
				if landed && b.PVY > 60 {
					for j := 0; float64(j) < 2+pixelfx.Round(b.S); j++ {
						ps.Spawn(pixelfx.Dust, b.X+R.Range(-1.5, 1.5), -0.5, R.Sign()*R.Range(10, 50), -R.Range(2, 20), R.Range(0.3, 0.7), st.dust, R.Range(1.3, 1+b.S), 0, 0)
					}
					if heatOf(b) > 0.5 && !b.Dusted {
						blazes.Add(b.X, -0.25, R.Range(1.2, 2.2), 0.45)
					}
				}
				if b.Rest {
					b.Dusted = true
				}
				for len(b.Pts) > 0 && t-b.Pts[2] > 0.45 {
					b.Pts = b.Pts[3:]
				}
			}
			blazes.Update(dt)
			ps.Update(dt)
			cull(ps, X0, Y0, X1, Y1)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			scorch(p, HR, t, 52, 3.4, 1-pixelfx.Smooth(D-1.6, D-0.3, t), 3)
			band(p, farHalf)
			drawPs(p, ps, pixelfx.LayerBack, FR)
			col.draw(p, top(), FR, SM, 1-t/1.4, plume.hot(plume.t))
			plume.draw(p, FR, SM)
			blazes.DrawBack(p)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			band(p, nearHalf)
			// the first beat: a white hero frame, a dome of white-hot air that cools into the fire, a flare, a shell of light
			if t < 0.04 {
				dome(p, F, 0, 44, 1)
				p.Glow(0, -30, 68, pixelfx.White, 0.6)
			} else if t < 0.11 {
				dome(p, F, 0, 38-(t-0.04)*120, 1-(t-0.04)/0.07)
			}
			for i := range rocks.List {
				b := &rocks.List[i]
				trail(p, b.Pts, t, FR, SM, 0.45, 0.6+b.S*0.3, heatOf(b))
				rock(p, b, st, FR)
			}
			drawPs(p, ps, pixelfx.LayerFront, FR)
			if t < 0.24 {
				pixelfx.Flare(p, 0, -14, 70*math.Pow(1-t/0.24, 1.4), 1-t/0.24, F)
			}
			if t < 0.5 {
				u := t / 0.5
				r := 34 + 80*pixelfx.Ease.OutCubic(u)
				c := F[3]
				if u < 0.3 {
					c = F[6]
				} else if u < 0.6 {
					c = F[5]
				}
				p.RingFn(0, 0, r, math.Max(1, 3*(1-u)+0.5), func(_, a float64) (uint32, float64) {
					if math.Sin(a) > 0 {
						return c, 0
					}
					return c, math.Min(1, (1-u)*1.6)
				})
				glowDome(p, 0, 0, r, 6, F[4], 0.35*(1-u), noBox)
			}
			for _, f := range flashes {
				k2 := f.life / f.max
				r := f.r * (0.8 + 0.2*k2)
				p.Circle(f.x, f.y, r+1.2, F[2], 1)
				p.Circle(f.x, f.y, r, pick(k2 > 0.45, F[6], F[5]), 1)
				p.Circle(f.x, f.y, r*0.6, pick(k2 > 0.45, pixelfx.White, F[6]), 1)
			}
			for _, r := range rings {
				k2 := r.life / r.max
				e := 1 - math.Pow(k2, 3)
				p.Ring(r.x, r.y, r.r0+(r.r1-r.r0)*e, math.Max(1, r.thick*k2+0.4), r.c, math.Min(1, k2*1.6))
			}
			// the fireball lights the floor under it while it burns
			hot, flash := plume.hot(plume.t), math.Max(0, 1-t/0.9)
			pool(p, 0, 1, 150, 22, F[3], 0.24*hot+0.5*flash*flash)
			lights.Draw(p)
		},
	}
}
