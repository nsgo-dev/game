// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/nuke.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The fire puffs, sparks and embers are adapted from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Nuke is a blinding white frame, then a fireball that lifts on a thin stem into a wide rolling cap glowing from inside; a white skirt flashes and a dust surge sweeps the floor. It is registered as "explosions/nuke".
var Nuke = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Nuke",
		Desc:     "A blinding white frame, then a fireball lifts on a thin stem into a wide rolling cap that glows from inside; a white skirt flashes and a dust surge sweeps the floor.",
		Tags:     []string{"explosion", "nuke", "mushroom", "cloud", "big", "ground", "one-shot"},
		Size:     [2]int{nukeW, nukeH},
		Origin:   [2]int{nukeOX, nukeOY},
		Duration: 9,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "dust", Default: pixelfx.Color("#8a7466")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newNuke,
}

const nukeW, nukeH, nukeOX, nukeOY = 400, 320, 200, 284

func init() { pixelfx.Register("explosions/nuke", Nuke) }

// variables, so sums of them round as pixelfx's do
var nukeFlash, nukeLift, nukeSkirt0, nukeSkirt1 = 0.05, 0.07, 0.55, 1.75

// nukeCap is the cap's smoke: a lit crescent, two mid tones, a dark underside, the darkest hollow.
var nukeCap = pixelfx.Ramp("#a0918c", "#7c6f6e", "#5f5458", "#40373f", "#271f27")

type veilPuff struct{ a, s, w, e, dx, dy float64 }

type skirtPuff struct{ ph, v, s, w, e, dx, dy float64 }

func newNuke(o *pixelfx.Options) pixelfx.Instance {
	F := pixelfx.Fit(o.Palette(), 8)
	FR, R, dust := fireRamp(F), pixelfx.NewRng(o.Seed()), o.Color("dust")
	floor := func(float64) float64 { return 0 }
	DUST := []uint32{pixelfx.Mix(dust, pixelfx.White, 0.22), dust, pixelfx.Mix(dust, pixelfx.Hex("#3a3040"), 0.35), pixelfx.Mix(dust, pixelfx.Hex("#2a2330"), 0.6), pixelfx.Mix(dust, pixelfx.Hex("#1c1720"), 0.8)}
	const W, H, OX, OY = nukeW, nukeH, nukeOX, nukeOY
	const X0, X1, Y0, Y1 = -OX + 3, W - OX - 3, -OY + 3, H - OY - 3
	CAP := nukeCap
	ps, lights := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{F[6], F[5], F[3], F[2]}}), &pixelfx.Lights{}
	// a wide, flat cap rolling like a torus: lobes go up the middle, out over the top and curl under at the rim
	plume := newPlume(R, plumeOpts{
		X: 0, Y: -26, Rise: 152, Tau: 1.7, Drift: 3, Tube: [2]float64{14, 26}, Ring: [2]float64{4, 46}, Grow: 1.6, N: 50, Size: [2]float64{9.5, 14.5}, Swell: 0.45,
		Spin: 2.4, SpinTau: 2.4, Heat: 1.25, Cool: 0.42, Under: 0.6, Tilt: 0.16, Squash: 0.48, Fade: [2]float64{6.0, 8.3}, Glow: 1, Spread: 26,
		Style: 1, Inner: true, Hollow: true, Fray: []float64{4, 1.6, 6.4},
	})
	plume.onFray = func(x, y, r, ux, uy float64) {
		ps.Spawn(pixelfx.Smoke, x, y, ux*R.Range(10, 26), uy*R.Range(4, 12)-R.Range(2, 8), R.Range(1, 1.7), CAP[int(R.Int(0, 1))], r*R.Range(0.3, 0.45), 0, 0)
	}
	// a thin stem that keeps feeding the cap, then breaks up from the bottom
	col := newColumn(R, columnOpts{W: 3.5, Foot: 22, Speed: 125, Size: [2]float64{5.5, 7}, N: 84, Sway: 1.4, Style: 1})
	top := func() float64 {
		S := plume.shape(plume.t)
		return S.y + S.tube*0.3
	}
	// the condensation skirt: a veil arching over the cap and a bell flaring round the stem
	veil := make([]veilPuff, 72)
	for i := range veil {
		q := &veil[i]
		q.a = -pi * ((float64(i) + R.Range(0.2, 0.8)) / 72)
		q.s = R.Range(6, 10)
		q.w = R.Range(0.97, 1.06)
		q.e = R.Range(0.6, 1.3)
		q.dx = R.Range(-1, 1)
		q.dy = R.Range(-1, 0.2)
	}
	skirt := make([]skirtPuff, 110)
	for i := range skirt {
		q := &skirt[i]
		q.ph = R.Range(0, tau)
		q.v = R.Range(0, 1)
		q.s = R.Range(5, 9)
		q.w = R.Range(0.9, 1.06)
		q.e = R.Range(0.6, 1.3)
		q.dx = R.Range(-1, 1)
		q.dy = R.Range(-1, 0.4)
	}
	t, boomed := 0.0, false

	boom := func() {
		lights.Add(0, -60, 150, F[4], 0.7, 1.6)
		// the fireball's first fire, sparks and embers
		for range 26 {
			a, d, sp := R.Range(-pi, 0), R.Range(4, 22), R.Range(40, 160)
			ps.Spawn(pixelfx.Fire, math.Cos(a)*d, -26+math.Sin(a)*d, math.Cos(a)*sp, math.Sin(a)*sp*0.7-30, R.Range(0.6, 1.1), 0, R.Range(7, 12), 0, chance(R, 0.5, pixelfx.Smokes|pixelfx.Big))
		}
		for range 90 {
			a, sp := R.Range(-pi-0.3, 0.3), R.Range(200, 420)
			ps.Spawn(pixelfx.Spark, math.Cos(a)*18, -26+math.Sin(a)*18, math.Cos(a)*sp, math.Sin(a)*sp-60, R.Range(0.3, 0.8), pixelfx.White, 1, 0, 0)
		}
		for range 50 {
			a, sp := R.Range(-pi, 0), R.Range(30, 140)
			ps.Spawn(pixelfx.Ember, math.Cos(a)*20, -26+math.Sin(a)*20, math.Cos(a)*sp, math.Sin(a)*sp-60, R.Range(1.6, 3.4), F[3], 1, 0, 0)
		}
		// the base surge: dust thrown out along the floor in every direction, racing to the edges; a slower ring behind
		for i := range 120 {
			slow, a := i >= 92, R.Range(0, tau)
			c, s := math.Cos(a), math.Sin(a)
			var sp float64
			if slow {
				sp = R.Range(110, 200)
			} else {
				sp = R.Range(240, 440)
			}
			var size float64
			x, y := c*14, s*14*0.16-R.Range(1, 3)
			vx, vy := c*sp, s*sp*pick(s > 0, 0.06, 0.14)-R.Range(0, pick(slow, 6.0, 3))
			life := R.Range(1.8, 3)
			if slow {
				size = R.Range(8, 11)
			} else {
				size = R.Range(5, 9)
			}
			ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, dust, size, R.Range(0.05, 0.2), 0)
		}
		for range 40 {
			u := R.Range(-1, 1)
			a, sp := -pi/2+u*0.7, R.Range(150, 380)*(1-0.4*math.Abs(u))
			ps.Spawn(pixelfx.Grit, u*12, -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1.2, 2), pixelfx.Pick(R, DUST[:3]), pick(R.Chance(0.4), 2.0, 1), 0, 0)
		}
	}

	band := func(p *pixelfx.Painter, keep func(float64) bool) {
		if k := (t - nukeLift) / 0.8; k > 0 && k < 1 {
			pixelfx.Band(p, 0, 0.5, 196*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 5*(1-k)), 1.15-k, []uint32{DUST[2], DUST[0], F[5], F[7]}, keep, false)
		}
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !boomed && t >= nukeLift {
				boomed = true
				boom()
			}
			if t >= nukeLift {
				plume.update(dt)
			}
			S, hot := plume.shape(plume.t), plume.hot(plume.t)
			feed := 0.0
			if t > 0.35 && t < 4.9 {
				feed = 1
			}
			col.update(dt, top(), feed)
			// afterwinds: dust drawn in along the floor and up the stem
			if t > 0.9 && t < 4.6 && R.Chance(9*dt) {
				s := R.Sign()
				ps.Spawn(pixelfx.Vapour, s*R.Range(50, 160), -R.Range(2, 6), -s*R.Range(20, 45), -R.Range(0, 6), R.Range(1.2, 1.8), DUST[1], R.Range(5, 8), 0, 0)
			}
			if t > 0.3 && t < 3.4 && R.Chance(14*hot*dt) {
				a := R.Range(0, tau)
				ps.Spawn(pixelfx.Ember, S.x+math.Cos(a)*(S.ring+S.tube), S.y+math.Sin(a)*S.tube*0.5, math.Cos(a)*R.Range(30, 80), -R.Range(10, 50), R.Range(1.4, 2.6), F[3], 1, 0, 0)
			}
			draft(ps, dt, draftOpts{X: 0, Top: S.y + S.tube*0.3, W: 9, Up: 160, Squeeze: 4, Pull: 120, Low: 14, Reach: 170, K: 1 - pixelfx.Smooth(4.4, 5.4, t)})
			ps.Update(dt)
			cull(ps, X0, Y0, X1, Y1)
			fadeEdges(ps, X0, X1, 34, Y1)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			if t < nukeFlash {
				return
			}
			S := plume.shape(plume.t)
			// the ground shock: a ring racing out along the floor ahead of the dust (far half)
			band(p, farHalf)
			drawPs(p, ps, pixelfx.LayerBack, FR)
			skirtDraw(p, t, S, veil, skirt, true)
			plume.draw(p, FR, CAP)
			// the dark hollow under the cap where the stem goes in, its heart still warm while the inside glows
			E, gone := plume.ember(plume.t), pixelfx.Smooth(plume.o.Fade[0], plume.o.Fade[1]-0.6, t-nukeLift)
			if gone < 1 {
				hy, rx, ry := S.y+S.tube*0.42, S.ring*0.7+5, S.tube*0.28+2
				haze(p, 0, hy, rx, ry, func(d float64) uint32 {
					if d < 0.55*E {
						return pixelfx.Mix(CAP[4], FR[pick(d < 0.3*E, 5, 6)], 0.75*E)
					}
					return CAP[4]
				}, func(d float64) float64 {
					switch {
					case d < 0.6:
						return 0.8 * (1 - gone)
					case d < 0.85:
						return 0.5 * (1 - gone)
					}
					return 0.22 * (1 - gone)
				})
			}
			col.draw(p, top(), FR, DUST, pixelfx.Clamp(1-t/1.8), E*0.8)
			skirtDraw(p, t, S, veil, skirt, false)
			pool(p, 0, 1, 140, 16, F[3], 0.3*plume.hot(plume.t))
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the blinding frame: a white-hot core in a wash of white light that fades out before the canvas edges
			if t < nukeFlash {
				wash(p, OX, OY, W, H, 0, -40, pixelfx.White, 1.6)
				p.Circle(0, -40, 64, pixelfx.White, 1)
				return
			}
			band(p, nearHalf)
			// out of the white: a crisp white-hot sphere, cooling into the fireball
			if f := (t - nukeFlash) / 0.13; f < 1 {
				r := 22 + 8*f
				p.Circle(0, -26, r+1.5, F[2], 1)
				p.Circle(0, -26, r, pick(f < 0.5, F[6], F[5]), 1)
				p.Circle(0, -26, r*0.62, pick(f < 0.5, pixelfx.White, F[6]), 1)
			}
			drawPs(p, ps, pixelfx.LayerFront, FR)
			// the shock in the air: a thin bright shell racing out from the fireball, cut off by the floor and the canvas
			if u := (t - nukeFlash) / 0.7; u < 1 {
				r, cy := 30+250*pixelfx.Ease.OutCubic(u), -26.0
				c := pick(u < 0.3, pixelfx.White, F[6])
				p.RingFn(0, cy, r, math.Max(1, 2.5*(1-u)), func(_, a float64) (uint32, float64) {
					if cy+math.Sin(a)*r > 0 || math.Abs(math.Cos(a)*r) > OX-3 || cy+math.Sin(a)*r < 3-OY {
						return c, 0
					}
					return c, 1.1 - u
				})
				glowDome(p, 0, cy, r, 7, F[5], 0.3*(1-u), [4]float64{3 - OX, 3 - OY, W - OX - 4, 0})
			}
			// the light of it: everything washed white, falling away fast, then the fireball's own warm light
			if w := math.Exp(-(t - nukeFlash) / 0.13); w > 0.02 {
				wash(p, OX, OY, W, H, 0, -40, pixelfx.White, 0.9*w)
			}
			lights.Draw(p)
		},
	}
}

// wash lays light over most of the canvas, brightest near (cx, cy), falling to nothing just inside the canvas edges so it never shows the canvas as a box.
func wash(p *pixelfx.Painter, OX, OY, W, H, cx, cy float64, c uint32, k float64) {
	rx, up, down := math.Min(OX+cx, W-OX-cx)-4, OY+cy-4, H-OY-cy-4
	p.Shade(-OX, -OY, W-OX-1, H-OY-1, func(x, y float64, out *pixelfx.Out) uint32 {
		d := pixelfx.Hypot((x-cx)/rx, (y-cy)/pick(y < cy, up, down))
		out.L = c
		out.K = k * (1 - pixelfx.Smooth(0, 1, d))
		return 0
	})
}

// skirtDraw draws the condensation skirt: a white veil that flashes over the cap and a bell that flares round the stem, edged with a crisp white rim, then evaporates into wisps that drift apart and thin; far half behind the cloud, near in front.
func skirtDraw(p *pixelfx.Painter, t float64, S plumeShape, veil []veilPuff, skirt []skirtPuff, back bool) {
	u := (t - nukeSkirt0) / (nukeSkirt1 - nukeSkirt0)
	if u <= 0 || u >= 1 {
		return
	}
	a := math.Min(1, u/0.06) * math.Pow(1-u, 1.5) * 0.6
	capR, y0, y1 := (S.ring+S.tube)*1.1, S.y+S.tube*0.3, S.y*0.55
	if !back {
		for _, q := range veil {
			ev := u * u * q.e
			R := capR * q.w * (1 + ev*0.2)
			p.Disc(math.Cos(q.a)*R+q.dx*ev*10, S.y+math.Sin(q.a)*(S.tube*1.15+6)*q.w+q.dy*ev*8, q.s*(1-0.5*u), pixelfx.White, a*0.38)
		}
	}
	for _, q := range skirt {
		z := math.Sin(q.ph)
		if (z < 0) != back {
			continue
		}
		R, ev := pixelfx.Lerp(capR*0.82, 13, math.Pow(q.v, 0.6))*q.w, u*u*q.e
		x, y := math.Cos(q.ph)*R*(1+ev*0.25)+q.dx*ev*10, pixelfx.Lerp(y0, y1, q.v)+z*R*0.18+q.dy*ev*8
		p.Disc(x, y, q.s*(1-0.55*u), pixelfx.White, a*pick(back, 0.3, 0.55)*(1-q.v*0.3))
	}
	// the bell's crisp lower edge, dithering away as it evaporates
	if u < 0.7 {
		R, k := 13*1.25, 1-u/0.7
		for i := range 64 {
			g := (float64(i) / 64) * tau
			z := math.Sin(g)
			if (z < 0) != back {
				continue
			}
			p.Dot(math.Cos(g)*R*(1+u*0.6), y1+2+z*R*0.22, pixelfx.White, k*pick(back, 0.6, 1.0))
		}
	}
}
