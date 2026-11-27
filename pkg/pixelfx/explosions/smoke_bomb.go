// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/smoke-bomb.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package explosions

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// SmokeBomb is a round canister that clinks down, fuse sparking, and pops: thick grey-violet smoke gushes out, rolls along the floor, billows into a cloud that hides all, then thins to wisps. It is registered as "explosions/smoke-bomb".
var SmokeBomb = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Smoke bomb",
		Desc:     "A round canister clinks down, fuse sparking, and pops: thick grey-violet smoke gushes out, rolls along the floor, billows into a cloud that hides all, then thins to wisps.",
		Tags:     []string{"smoke", "smoke-bomb", "ninja", "vanish", "escape", "ground", "one-shot"},
		Size:     [2]int{smokeW, smokeH},
		Origin:   [2]int{smokeOX, smokeOY},
		Duration: smokeDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0e0b14", "#1e1828", "#30283e", "#463c58", "#62567a", "#867a9e", "#aea4c4")},
			{Name: "canister", Default: pixelfx.Palette("#16121e", "#2a2434", "#443c52", "#686078", "#9c94ac")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newSmokeBomb,
}

const smokeW, smokeH, smokeOX, smokeOY = 260, 176, 130, 156

func init() { pixelfx.Register("explosions/smoke-bomb", SmokeBomb) }

// variables, so sums of them round as pixelfx's do
var smokeDuration, smokePop, smokeR0 = 5.0, 0.95, 4.0

var (
	smokeBand  = []uint32{pixelfx.Hex("#3a1418"), pixelfx.Hex("#7a2a24"), pixelfx.Hex("#b84a34")}
	smokeGrit  = []uint32{pixelfx.Hex("#5a4f6b"), pixelfx.Hex("#3e3650"), pixelfx.Hex("#776c88")}
	smokeFuse  = pixelfx.Hex("#8a6a48")
	smokeSpeed = [4][2]float64{{70, 170}, {0, 25}, {45, 95}, {20, 60}}
	smokeSize  = [4][2]float64{{12, 17}, {16, 22}, {13, 18}, {14, 19}}
	smokeKinds = [6]int{0, 0, 1, 1, 2, 3}
)

// canister is the smoke bomb's flight state.
type canister struct {
	x, y, vx, vy, va, a float64
	rest                bool
}

// step moves the canister on: it arcs down, bounces with a clink, rolls and stops. hit, when not nil, is called on each bounce with where and how fast.
func (c *canister) step(dt float64, hit func(x, speed float64)) {
	if c.rest {
		return
	}
	c.vy += 900 * dt
	c.x += c.vx * dt
	c.y += c.vy * dt
	if c.y >= -smokeR0 && c.vy > 0 {
		c.y = -smokeR0
		if c.vy > 80 {
			if hit != nil {
				hit(c.x, c.vy)
			}
			c.vy = -c.vy * 0.38
			c.vx *= 0.78
		} else {
			c.vy = 0
		}
	}
	if c.vy == 0 && c.y >= -smokeR0-0.01 {
		c.vx -= c.vx * 3 * dt
		if math.Abs(c.vx) < 2 {
			c.vx, c.rest = 0, true
		}
	}
	if c.vy == 0 {
		c.a += c.vx / smokeR0 * dt
	} else {
		c.a += c.va * dt
	}
}

type smokeLobe struct {
	kind                     int
	t0, vx, vh, d, r0, r1, f float64
	wob                      float64
	on                       bool
	x, h, age                float64
}

func newSmokeBomb(o *pixelfx.Options) pixelfx.Instance {
	C, K, R := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("canister"), 5), pixelfx.NewRng(o.Seed())
	sparks := o.Ramp("sparks")
	floor := func(float64) float64 { return 0 }
	SM := []uint32{C[5], C[4], C[3], C[2], C[1]}
	const W, H, OX, OY = smokeW, smokeH, smokeOX, smokeOY
	const X0, X1, Y0, Y1 = -OX + 3, W - OX - 3, -OY + 3, H - OY - 3
	ps, lights := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: sparks}), &pixelfx.Lights{}
	// thrown in from the left so that it comes to rest on the anchor: run its path once to see where it stops
	start := canister{x: -70, y: -60, vx: 125, vy: -80, va: 7}
	sim := start
	for i := 0; i < 2400 && !sim.rest; i++ {
		sim.step(1.0/240, nil)
	}
	can := start
	can.x = start.x - sim.x
	// the smoke, gushing out front-loaded: rollers that slide out low along the floor, a fat core that stays over the canister, a plume driven up, and a low trickle that keeps the base filled; they rise as they billow
	const N = 52
	lobes := make([]smokeLobe, N)
	for i := range lobes {
		kind, side := smokeKinds[i%6], R.Sign()
		var ang float64
		switch {
		case kind == 0 || kind == 3:
			if side > 0 {
				ang = -R.Range(0, 0.25)
			} else {
				ang = pi + R.Range(0, 0.25)
			}
		default:
			ang = -pi/2 + R.Range(-0.6, 0.6)
		}
		// pixelfx draws every kind's speed and size, then keeps this kind's
		var sp, r1 float64
		for j, s := range smokeSpeed {
			if v := R.Range(s[0], s[1]); j == kind {
				sp = v
			}
		}
		for j, s := range smokeSize {
			if v := R.Range(s[0], s[1]); j == kind {
				r1 = v
			}
		}
		var t0 float64
		if kind == 3 {
			t0 = smokePop + R.Range(0.45, 1.5)
		} else {
			t0 = smokePop + 0.6*math.Pow(float64(i)/(N-1), 1.6) + R.Range(0, 0.03)
		}
		b := &lobes[i]
		b.kind, b.t0, b.vx, b.vh = kind, t0, math.Cos(ang)*sp, -math.Sin(ang)*sp
		b.d = R.Range(-1, 1)
		b.r0 = R.Range(5, 8)
		b.r1 = r1
		b.f = R.Range(-0.45, 0.4)
		b.wob = R.Range(0, tau)
	}
	t, popped := 0.0, false
	fuseTip := func() (float64, float64) {
		return can.x + math.Sin(can.a)*(smokeR0+2.5), can.y - math.Cos(can.a)*(smokeR0+2.5)
	}
	thin := func(b *smokeLobe) float64 { return pixelfx.Smooth(2.5+b.f, 3.9+b.f*0.6, t) }
	radius := func(b *smokeLobe) float64 {
		return (b.r0 + (b.r1-b.r0)*(1-math.Exp(-b.age/0.28))) * (1 + 0.05*math.Sin(t*3+b.wob)) * (1 + 0.35*thin(b))
	}
	// far lobes behind, near ones in front; each a cel-shaded ball: a lit crescent on top, a mid tone, a dark underside. The cloud is lit from above, so lobes low in it sit in its shadow a step darker; anything nearing the top of the canvas thins out
	var on []*smokeLobe
	clouds := func(p *pixelfx.Painter, back bool) {
		on = on[:0]
		for i := range lobes {
			if lobes[i].on {
				on = append(on, &lobes[i])
			}
		}
		if len(on) == 0 {
			return
		}
		top := 0.0
		for _, b := range on {
			top = math.Min(top, -(b.h + radius(b)*1.92))
		}
		var side []*smokeLobe
		for _, b := range on {
			if (b.d < -0.35) == back {
				side = append(side, b)
			}
		}
		slices.SortStableFunc(side, func(a, b *smokeLobe) int { return cmp.Compare(a.d, b.d) })
		for _, b := range side {
			r := radius(b)
			y, k := -(b.h+r*0.92)+b.d*2.5, thin(b)
			a := math.Min(1, b.age/0.05) * math.Pow(1-k, 1.4) * 0.97 * (1 - pixelfx.Smooth(-OY+34, -OY+6, y-r))
			if a <= 0.01 {
				continue
			}
			shade := pick((y-top)/math.Max(20, -top) > 0.55, 1, 0)
			lobe(p, b.x, y, r, SM, SM, 0, 0, a, b.d, min(2, shade+pick(k > 0.6, 1, 0)), 1)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			can.step(dt, func(x, _ float64) {
				// a clink: a few bright specks and a puff of dust
				for range 3 {
					ps.Spawn(pixelfx.Spark, x, -1, R.Range(-70, 70), -R.Range(40, 110), R.Range(0.06, 0.14), pixelfx.White, 1, 0, 0)
				}
				for range 3 {
					ps.Spawn(pixelfx.Dust, x+R.Range(-2, 2), -0.5, R.Sign()*R.Range(10, 40), -R.Range(4, 14), R.Range(0.3, 0.55), smokeGrit[2], R.Range(1.4, 2.4), 0, 0)
				}
			})
			if t < smokePop {
				// the fuse: sparks spitting off its tip, quicker as it burns down, and a thread of smoke
				fx, fy := fuseTip()
				hurry := pixelfx.Smooth(smokePop-0.4, smokePop, t)
				if R.Chance((26 + 50*hurry) * dt) {
					ps.Spawn(pixelfx.Spark, fx, fy, R.Range(-50, 50), -R.Range(30, 100), R.Range(0.08, 0.18), pixelfx.White, 1, 0, 0)
				}
				if R.Chance(8 * dt) {
					ps.Spawn(pixelfx.Vapour, fx, fy-1, R.Range(-4, 4), -R.Range(10, 18), R.Range(0.5, 0.8), C[5], R.Range(1, 1.8), 0, 0)
				}
			} else if !popped {
				popped = true
				// the pop: a flash, a soft light that dies fast, sparks and grit, the canister jolted
				lights.Add(can.x, -9, 64, C[6], 0.75, 0.22)
				lights.Add(can.x, -5, 22, pixelfx.White, 0.7, 0.06)
				for range 30 {
					a, sp := R.Range(-pi-0.35, 0.35), R.Range(120, 300)
					ps.Spawn(pixelfx.Spark, can.x, -5, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.15, 0.4), pixelfx.White, 1, 0, 0)
				}
				for range 26 {
					u := R.Range(-1, 1)
					a, sp := -pi/2+u*0.9, R.Range(80, 220)*(1-0.35*math.Abs(u))
					ps.Spawn(pixelfx.Grit, can.x+u*4, -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.9, 1.6), pixelfx.Pick(R, smokeGrit), pick(R.Chance(0.35), 2.0, 1), 0, 0)
				}
				can.vy = -95
				can.vx = R.Range(-25, 25)
				can.va = R.Sign() * 9
				can.rest = false
			}
			// the cloud: lobes slide out low, slow, then rise as they billow; a breeze takes them late on
			wind := 6 * pixelfx.Smooth(1.8, 3.2, t)
			for i := range lobes {
				b := &lobes[i]
				if !b.on {
					if t < b.t0 {
						continue
					}
					b.on, b.x, b.h = true, can.x+R.Range(-2, 2), 1
				}
				b.age += dt
				b.vx -= b.vx * 2.4 * dt
				// rollers hug the floor for a while; everything rises as it billows, gently
				var lift float64
				switch b.kind {
				case 0:
					lift = 14 * pixelfx.Smooth(0.7, 1.6, b.age)
				case 1:
					lift = 3 + 9*pixelfx.Smooth(0.8, 1.8, b.age)
				case 2:
					lift = 6 + 12*pixelfx.Smooth(0.3, 1.4, b.age)
				default:
					lift = 10 * pixelfx.Smooth(0.8, 1.6, b.age)
				}
				b.vh += lift * dt
				b.vh -= b.vh * 1.6 * dt
				b.x += (b.vx + wind) * dt
				b.h = math.Max(0, b.h+b.vh*dt)
				// thinning, it sheds wisps that drift off
				if k := thin(b); k > 0.12 && k < 0.85 && R.Chance(1.8*dt) {
					r := radius(b)
					if life := math.Min(R.Range(1, 1.6), smokeDuration-0.15-t); life > 0.3 {
						ps.Spawn(pixelfx.Vapour, b.x+R.Range(-r, r)*0.5, -(b.h + r*1.4), wind+R.Range(-6, 10), -R.Range(8, 20), life, C[4], r*R.Range(0.3, 0.45), 0, 0)
					}
				}
			}
			// the spent canister keeps smoking a little
			if t > smokePop+0.5 && t < 4.1 && R.Chance(4*dt) {
				ps.Spawn(pixelfx.Vapour, can.x, can.y-3, R.Range(-3, 3), -R.Range(8, 16), R.Range(0.6, 0.9), C[4], R.Range(1.5, 2.5), 0, 0)
			}
			ps.Update(dt)
			cull(ps, X0, Y0, X1, Y1)
			fadeEdges(ps, X0, X1, 20, Y1)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			ps.Draw(p, pixelfx.LayerBack)
			clouds(p, true)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the canister: live with its fuse burning, then spent; it fades as the last smoke clears
			if fade := 1 - pixelfx.Smooth(4.3, 4.8, t); fade > 0 {
				drawCanister(p, K, &can, t < smokePop, fade)
			}
			if t < smokePop {
				fx, fy := fuseTip()
				p.Px(fx, fy, pick(math.Sin(t*50) > 0, pixelfx.White, sparks[1]))
				p.Glow(fx, fy, 6, sparks[2], 0.45+0.2*math.Sin(t*37))
			}
			clouds(p, false)
			ps.Draw(p, pixelfx.LayerFront)
			// the flash of the pop
			u := t - smokePop
			if u >= 0 && u < 0.067 {
				p.Circle(can.x, -5, 6.5, C[5], 1)
				p.Circle(can.x, -5, 4.5, pixelfx.White, 1)
			}
			if u >= 0 && u < 0.16 {
				pixelfx.Flare(p, can.x, -5, 22*(1-u/0.16), 1-u/0.16, []uint32{C[3], C[4], C[5], C[6]})
			}
			if u >= 0 && u < 0.35 {
				pool(p, can.x, 1, 50, 8, C[6], 0.5*(1-u/0.35))
			}
			lights.Draw(p)
		},
	}
}

// drawCanister draws a round canister turned by its roll: dark metal lit from the top left, a red band, an outline, and a fuse on top while it is live (spent, its cap is blown off).
func drawCanister(p *pixelfx.Painter, K []uint32, c *canister, live bool, a float64) {
	ca, sa := math.Cos(c.a), math.Sin(c.a)
	R0 := smokeR0
	p.Shade(c.x-R0-2, c.y-R0-2, c.x+R0+2, c.y+R0+2, func(px, py float64, out *pixelfx.Out) uint32 {
		dx, dy := px-c.x, py-c.y
		d := pixelfx.Hypot(dx, dy)
		if d > R0+1 {
			return 0
		}
		out.A = a
		if d > R0 {
			return outline
		}
		v, lit := -dx*sa+dy*ca, -(dx*0.6+dy*0.8)/R0
		if math.Abs(v) < 0.9 {
			switch {
			case lit > 0.3:
				return smokeBand[2]
			case lit > -0.4:
				return smokeBand[1]
			}
			return smokeBand[0]
		}
		if d < R0*0.5 && lit > 0.45 {
			return K[4]
		}
		switch {
		case lit > 0.5:
			return K[3]
		case lit > 0:
			return K[2]
		case lit > -0.5:
			return K[1]
		}
		return K[0]
	})
	cx, cy := c.x+sa*R0, c.y-ca*R0
	if live {
		p.Line(cx, cy, cx+sa*2.5, cy-ca*2.5, smokeFuse, a)
	} else {
		p.Dot(cx, cy, K[0], a)
	}
}
