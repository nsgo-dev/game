// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/dark-explosion.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the pull that swirls motes into the core and the collapse burst from Sprite Fusion's open-source black-hole (https://www.spritefusion.com/pixel-art-vfx/black-hole), and the sparks, rings and smoke from his explosion, MIT License, Copyright (c) 2026 Hugo Duprez.

package shadow

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// DarkExplosion swirls motes into a pulsing core, cuts a black star across a violet-white flash, and burns a black sphere at the rim that throws lit shards and smoke before collapsing. It is registered as "shadow/dark-explosion".
var DarkExplosion = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Dark explosion",
		Desc:     "Motes swirl into a pulsing core, a black star cuts across a violet-white flash, and a black sphere burns at the rim, throws lit shards and smoke, then collapses.",
		Tags:     []string{"shadow", "explosion", "void", "dark", "magic", "impact", "one-shot"},
		Size:     [2]int{280, 248},
		Origin:   [2]int{140, 206},
		Duration: 3.8,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newDarkExplosion,
}

func init() { pixelfx.Register("shadow/dark-explosion", DarkExplosion) }

// the heart of the blast hangs above the floor; the sphere's radius; the beats of the choreography
const (
	deY    = -54.0
	deRS   = 16.0
	deRel  = 0.52
	deBurn = 1.62
	dePop  = 1.84
)

var deSmoke = pixelfx.Hex("#5d5468")

// deSphere draws a black sphere: a void body (a faint crescent of reflected light low on the right, a dull glint high on the left), a white rim, and pointed tongues of violet flame that lean up, flicker and crawl round it.
func deSphere(p *pixelfx.Painter, C []uint32, r, t, heat float64) {
	if r < 0.6 {
		return
	}
	E := r + 12*heat + 2
	p.Shade(-E, deY-E, E, deY+E, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x, y-deY
		d := math.Sqrt(dx*dx + dy*dy)
		if d > E {
			return 0
		}
		if d < r-0.5 {
			e, ang := r-d, math.Atan2(dy, dx)
			if e < 1.4 && math.Cos(ang-0.8) > 0.55 {
				return C[2]
			}
			gx, gy := dx+r*0.45, dy+r*0.45
			if r > 8 && gx*gx+gy*gy < 1.6 {
				return C[3]
			}
			return C[0]
		}
		e := d - (r - 0.5)
		if e < 1 {
			out.C = pick(math.Sin(math.Atan2(dy, dx)*7+t*9) > -0.6, pixelfx.White, C[7])
			out.K = 0.4
			return 0
		}
		// the further out, the lower the angle it's sampled at: tongues bend upward like flame
		ang := math.Atan2(dy+e*0.6, dx)
		top := math.Max(0, -math.Sin(ang))
		a1, a2 := ang-t*0.9, ang+t*1.7
		i1, i2 := pixelfx.Round((a1*9)/tau), pixelfx.Round((a2*15)/tau)
		T1 := math.Pow(0.5+0.5*math.Cos(a1*9), 3) * (0.55 + 0.45*pixelfx.Noise(t*7+i1*3.7, 1))
		T2 := math.Pow(0.5+0.5*math.Cos(a2*15), 4) * (0.5 + 0.5*pixelfx.Noise(t*11+i2*5.1, 2))
		h := heat * (0.3 + 0.95*top) * (1.5 + 12*math.Max(0, T1) + 6*math.Max(0, T2))
		if e > h+1 {
			return 0
		}
		u := (e - 1) / h
		switch {
		case u < 0.18:
			out.C = pixelfx.White
		case u < 0.4:
			out.C = C[7]
		case u < 0.62:
			out.C = C[6]
		case u < 0.84:
			out.C = C[5]
		default:
			out.C = C[4]
		}
		if u < 0.8 {
			out.A = 1
		} else {
			out.A = (1 - u) * 5
		}
		if u < 0.6 && (pixelfx.ToInt32(math.Floor(x)+math.Floor(y))&1) == 0 {
			out.K = 0.25 * (1 - u)
		}
		return 0
	})
}

type deLight struct {
	x, y, r, k, life, max float64
	c                     uint32
}

type deRing struct {
	r0, r1, life, max, th, delay float64
	hot                          bool
}

type deShardState struct {
	flat, wisp bool
}

// deFloorRing draws where the shock meets the floor: its cross-section with the floor plane, a flat ring racing out.
func deFloorRing(p *pixelfx.Painter, C []uint32, r deRing, back bool) {
	if r.delay > 0 || !r.hot || r.th < 3 {
		return
	}
	q := r.life / r.max
	rr := r.r0 + (r.r1-r.r0)*(1-q*q*q)
	if rr <= -deY {
		return
	}
	rf := math.Sqrt(rr*rr - deY*deY)
	pixelfx.Band(p, 0, 0.5, rf, 0.16, math.Max(1, 3*q), math.Min(1, q*1.5), []uint32{C[3], C[4], C[5], C[6]}, func(g float64) bool {
		return (math.Sin(g) < 0) == back
	}, false)
	if !back {
		p.Glow(-rf, -2, 16, C[5], 0.4*q)
		p.Glow(rf, -2, 16, C[5], 0.4*q)
	}
}

func newDarkExplosion(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, C[7], C[6], C[5]}})
	embers := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[4]}})
	pf := newPuffs(puffsOpts{
		rng:   R,
		flame: []uint32{pixelfx.White, C[7], C[6], C[5], C[5], C[4], C[4], C[3], C[2]},
		soot:  []uint32{pixelfx.Mix(pixelfx.Mix(C[3], C[1], 0.3), deSmoke, 0.45), pixelfx.Mix(C[1], deSmoke, 0.3)},
		cap:   pixelfx.Mix(pixelfx.Mix(C[4], C[2], 0.5), deSmoke, 0.4),
		rim:   []uint32{C[6], C[5], C[4]},
		floor: floor,
	})
	bits := pixelfx.NewBits(floor)
	var lights []deLight
	var rings []deRing
	t, acc, dustAcc, flick := 0.0, 0.0, 0.0, 1.0
	released, collapsing, popped := false, false, false
	moteCols := []uint32{pixelfx.White, C[6], C[6], C[5]}

	coreR := func(tt float64) float64 {
		if tt < deRel-0.07 {
			return 1.5 + 5*pixelfx.Ease.InQuad(tt/deRel)
		}
		return 1
	}
	sphereR := func(tt float64) float64 {
		if tt < deRel {
			return 0
		}
		if tt < deBurn {
			return deRS * pixelfx.OutBack(pixelfx.Clamp((tt-deRel)/0.14), 2.4) * (1 + 0.06*pixelfx.Smooth(deRel+0.3, deBurn, tt) + 0.02*math.Sin(tt*11))
		}
		if tt < dePop {
			return deRS * 1.08 * (1 - pixelfx.Ease.InQuad((tt-deBurn)/(dePop-deBurn)))
		}
		return 0
	}
	starL := func(tt float64) float64 {
		if tt < deRel || tt > deRel+0.26 {
			return 0
		}
		return 60 * (1 - pixelfx.Ease.InQuad(pixelfx.Clamp((tt-deRel-0.05)/0.21)))
	}
	starRot := func(tt float64) float64 { return 0.32 + (tt-deRel)*2.2 }
	// how hard the sphere lights what's round it: the rims of the smoke, the edges of the shards
	burning := func(tt float64) float64 {
		switch {
		case tt < deRel:
			return pixelfx.Clamp(tt/deRel) * 0.7
		case tt < deBurn:
			return 1
		case tt < dePop:
			return 1.2
		}
		return math.Max(0, 1-(tt-dePop)/0.35)
	}
	addLight := func(x, y, r float64, c uint32, k, life float64) {
		lights = append(lights, deLight{x: x, y: y, r: r, c: c, k: k, life: life, max: life})
	}
	rim := func(a, r float64) (float64, float64) { return math.Cos(a) * r, deY + math.Sin(a)*r }

	release := func() {
		addLight(0, deY, 100, C[4], 1.25, 0.85)
		addLight(0, deY, 86, pixelfx.Mix(C[6], pixelfx.White, 0.5), 1.3, 0.22)
		rings = append(rings,
			deRing{r0: deRS, r1: 104, life: 0.46, max: 0.46, th: 3.6, hot: true, delay: 0},
			deRing{r0: deRS * 0.8, r1: 62, life: 0.3, max: 0.3, th: 1.6, hot: true, delay: 0.02},
			deRing{r0: deRS, r1: 124, life: 0.62, max: 0.62, th: 1.2, hot: false, delay: 0.07},
		)
		// shards of the void: big slivers tumbling out and raining onto the floor, fine splinters faster
		for i := range 34 {
			big := i < 20
			a := R.Range(0, tau)
			x, y := rim(a, deRS*0.7)
			var sp float64
			if big {
				sp = R.Range(80, 170)
			} else {
				sp = R.Range(130, 220)
			}
			vy := math.Sin(a)*sp - R.Range(40, 110)
			drag := 1.7
			if big {
				drag = 1.3
			}
			life := R.Range(1.6, 2.6)
			ba := R.Range(0, tau)
			va := R.Range(-16, 16)
			var sz float64
			if big {
				sz = R.Range(6, 11)
			} else {
				sz = R.Range(3, 4.5)
			}
			bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: x, Y: y, VX: math.Cos(a) * sp, VY: vy, G: 0.75, Drag: drag, Bounce: 0.35, Life: life, A: ba, VA: va, S: sz, Data: &deShardState{}})
		}
		for range 84 {
			a := R.Range(0, tau)
			x, y := rim(a, deRS*0.8)
			sp := R.Range(120, 235)
			ps.Spawn(pixelfx.Spark, x, y, math.Cos(a)*sp, math.Sin(a)*sp-50, R.Range(0.2, 0.7), pixelfx.White, 1, 0, 0)
		}
		for range 28 {
			a := R.Range(0, tau)
			x, y := rim(a, deRS*0.9)
			sp := R.Range(30, 130)
			embers.Spawn(pixelfx.Ember, x, y, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(1.3, 2.5), pixelfx.White, 1, 0, 0)
		}
		for range 24 {
			a := R.Range(0, tau)
			x, y := rim(a, deRS*0.9)
			sp := R.Range(15, 70)
			pf.spawn(pkSoot, x, y, math.Cos(a)*sp, math.Sin(a)*sp*0.8-14, R.Range(2, 2.8), R.Range(7, 12), R.Range(0.02, 0.3), false)
		}
		// the shock flattens on the floor and shoves smoke out along it
		for i := range 12 {
			s := -1.0
			if i%2 != 0 {
				s = 1
			}
			pf.spawn(pkSoot, s*R.Range(10, 40), -R.Range(2, 5), s*R.Range(50, 120), -R.Range(0, 10), R.Range(1.6, 2.4), R.Range(5, 9), R.Range(0.08, 0.2), false)
		}
		for range 10 {
			a := -pi/2 + R.Range(-1.6, 1.6)
			x, y := rim(a, deRS)
			sp := R.Range(30, 80)
			pf.spawn(pkFlame, x, y, math.Cos(a)*sp, math.Sin(a)*sp-30, R.Range(0.3, 0.6), R.Range(2, 3.6), 0, R.Chance(0.5))
		}
		// what was still falling in is swallowed by the blast
		ps.List = ps.List[:0]
		kept := pf.list[:0]
		for _, q := range pf.list {
			if q.delay > 0 {
				kept = append(kept, q)
			}
		}
		pf.list = kept
	}

	pop := func() {
		addLight(0, deY, 56, pixelfx.White, 0.9, 0.18)
		addLight(0, deY, 120, C[5], 0.8, 0.5)
		rings = append(rings, deRing{r0: 2, r1: 40, life: 0.28, max: 0.28, th: 2, hot: true, delay: 0})
		for range 26 {
			a, sp := R.Range(0, tau), R.Range(120, 250)
			ps.Spawn(pixelfx.Spark, 0, deY, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.2, 0.6), pixelfx.White, 1, 0, 0)
		}
		for range 14 {
			a, sp := R.Range(0, tau), R.Range(20, 90)
			embers.Spawn(pixelfx.Ember, 0, deY, math.Cos(a)*sp, math.Sin(a)*sp-30, R.Range(0.9, 1.6), pixelfx.White, 1, 0, 0)
		}
		for range 10 {
			a, sp := R.Range(0, tau), R.Range(20, 70)
			pf.spawn(pkSoot, math.Cos(a)*3, deY+math.Sin(a)*3, math.Cos(a)*sp, math.Sin(a)*sp-15, R.Range(1.1, 1.7), R.Range(3, 6), R.Range(0, 0.08), false)
		}
		for i := range embers.List {
			q := &embers.List[i]
			dx, dy := q.X, q.Y-deY
			d := pixelfx.Hypot(dx, dy)
			if d == 0 {
				d = 1
			}
			f := 90 * math.Max(0, 1-d/70)
			q.VX += (dx / d) * f
			q.VY += (dy/d)*f - f*0.3
		}
		for i := range pf.list {
			q := &pf.list[i]
			dx, dy := q.x, q.y-deY
			d := pixelfx.Hypot(dx, dy)
			if d == 0 {
				d = 1
			}
			f := 90 * math.Max(0, 1-d/70)
			q.vx += (dx / d) * f
			q.vy += (dy/d)*f - f*0.3
		}
		// what's left of it climbs away as a column of smoke
		for range 12 {
			pf.spawn(pkSoot, R.Range(-6, 6), deY+R.Range(-4, 6), R.Range(-10, 10), -R.Range(20, 55), R.Range(1.3, 1.8), R.Range(7, 12), R.Range(0.02, 0.2), false)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			flick = 0.88 + 0.12*math.Sin(t*37)*math.Sin(t*23)
			// the charge: motes swirl in from all round, streaks shoot straight at the core, darkness gathers
			if t < deRel-0.06 {
				k := t / deRel
				for acc += dt * (90 + 150*k); acc > 1; acc-- {
					a, D := R.Range(0, tau), R.Range(36, 84)
					x, y := math.Cos(a)*D, math.Min(-3, deY+math.Sin(a)*D)
					vt, vi := R.Range(50, 110), R.Range(10, 40)
					ps.Spawn(pixelfx.Mote, x, y, -math.Sin(a)*vt-math.Cos(a)*vi, math.Cos(a)*vt-math.Sin(a)*vi, 0.9, pixelfx.Pick(R, moteCols), R.Range(0.7, 1.2), 0, 0)
				}
				if R.Chance(0.75) {
					a, D, T := R.Range(0, tau), R.Range(28, 70), R.Range(0.18, 0.28)
					v := (D * 3) / (math.Exp(3*T) - 1)
					ps.Spawn(pixelfx.Streak, math.Cos(a)*D, deY+math.Sin(a)*D, -math.Cos(a)*v, -math.Sin(a)*v, T, pick(R.Chance(0.5), C[7], C[6]), 1, 0, 0)
				}
				if t < deRel-0.2 && R.Chance(0.16) {
					a, D := R.Range(0, tau), R.Range(34, 60)
					pf.spawn(pkSoot, math.Cos(a)*D, deY+math.Sin(a)*D, -math.Sin(a)*40, math.Cos(a)*40, R.Range(0.6, 0.9), R.Range(3, 6), 0, false)
				}
				attractParticles(ps, 0, deY, 2.2e6*(0.3+k), 110, 0.55, 2.5, dt, func(q *pixelfx.Particle) float64 {
					if q.K == pixelfx.Mote {
						return 1
					}
					return 0
				}, 420)
				attractPuffs(pf, 0, deY, 1.2e6*(0.3+k), 90, 0.7, 4, dt, func(*puff) float64 { return 1 }, 160)
			}
			if t >= deRel && !released {
				released = true
				release()
			}
			// the sphere burns: flame licks off its rim, the odd spark and ember
			if released && t < deBurn {
				sr := sphereR(t)
				for acc += dt * 34; acc > 1; acc-- {
					a := -pi/2 + R.Gauss()*1.1
					x, y := rim(a, sr+1)
					sp := R.Range(10, 35)
					pf.spawn(pkFlame, x, y, math.Cos(a)*sp, math.Sin(a)*sp-R.Range(10, 30), R.Range(0.25, 0.45), R.Range(1.2, 2.2), 0, R.Chance(0.2))
				}
				if R.Chance(dt * 10) {
					a := -pi/2 + R.Range(-1.4, 1.4)
					x, y := rim(a, sr*0.9)
					pf.spawn(pkSoot, x, y, math.Cos(a)*20, -R.Range(15, 35), R.Range(1.2, 1.7), R.Range(5, 9), 0, false)
				}
				if R.Chance(dt * 8) {
					a := R.Range(0, tau)
					x, y := rim(a, sr)
					sp := R.Range(110, 200)
					ps.Spawn(pixelfx.Spark, x, y, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.2, 0.5), pixelfx.White, 1, 0, 0)
				}
				if R.Chance(dt * 7) {
					a := -pi/2 + R.Range(-1.2, 1.2)
					x, y := rim(a, sr)
					embers.Spawn(pixelfx.Ember, x, y, math.Cos(a)*30, -R.Range(20, 50), R.Range(0.9, 1.6), pixelfx.White, 1, 0, 0)
				}
			}
			// the collapse: the sphere draws its own flame back in, streaks race to the heart
			if t >= deBurn && !collapsing {
				collapsing = true
				for range 14 {
					a, D, T := R.Range(0, tau), R.Range(26, 48), R.Range(0.14, 0.2)
					v := (D * 3) / (math.Exp(3*T) - 1)
					ps.Spawn(pixelfx.Streak, math.Cos(a)*D, deY+math.Sin(a)*D, -math.Cos(a)*v, -math.Sin(a)*v, T, pick(R.Chance(0.5), pixelfx.White, C[6]), 1, 0, 0)
				}
			}
			if collapsing && !popped {
				attractPuffs(pf, 0, deY, 1.4e6, 64, 0.6, 3, dt, func(q *puff) float64 {
					if q.k == pkFlame {
						return 1
					}
					return 0.35
				}, 200)
			}
			if t >= dePop && !popped {
				popped = true
				pop()
			}
			// the shock reaches the floor: dust rolls out under the ring
			for _, r := range rings {
				if r.delay > 0 || !r.hot || r.th < 3 {
					continue
				}
				rr := r.r0 + (r.r1-r.r0)*(1-math.Pow(r.life/r.max, 3))
				rf := 0.0
				if rr > -deY {
					rf = math.Sqrt(rr*rr - deY*deY)
				}
				if rf > 0 {
					for dustAcc += dt * 60; dustAcc > 1; dustAcc-- {
						s := R.Sign()
						ps.Spawn(pixelfx.Dust, s*rf+R.Range(-2, 2), -R.Range(1, 3), s*R.Range(20, 50), -R.Range(4, 14), R.Range(0.4, 0.8), pixelfx.Mix(C[2], C[3], R.Next()), R.Range(1.5, 3), 0, 0)
					}
				}
			}
			for i := len(lights) - 1; i >= 0; i-- {
				lights[i].life -= dt
				if lights[i].life <= 0 {
					lights = slices.Delete(lights, i, i+1)
				}
			}
			for i := len(rings) - 1; i >= 0; i-- {
				if rings[i].delay > 0 {
					rings[i].delay -= dt
				} else if rings[i].life -= dt; rings[i].life <= 0 {
					rings = slices.Delete(rings, i, i+1)
				}
			}
			bits.Update(dt)
			for i := range bits.List {
				b := &bits.List[i]
				st := b.Data.(*deShardState)
				// a shard that comes to rest lies flat on the floor
				if b.Rest && !st.flat {
					st.flat = true
					b.A = pixelfx.Round(b.A/pi)*pi + R.Range(-0.2, 0.2)
					b.Y = -1
				}
				// and once it has lain a while, it dissolves into a wisp
				if b.Life < 0.45 && !st.wisp {
					st.wisp = true
					pf.spawn(pkSoot, b.X, b.Y-1, R.Range(-4, 4), -R.Range(8, 16), R.Range(0.6, 0.9), R.Range(1.4, 2.4), 0, false)
				}
			}
			pf.update(dt)
			ps.Update(dt)
			embers.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			k := burning(t)
			for _, r := range rings {
				deFloorRing(p, C, r, true)
			}
			pf.draw(p, pkSoot, &puffLight{x: 0, y: deY, k: k * flick, reach: 84})
			ps.Draw(p, pixelfx.LayerBack)
			if k > 0 {
				p.Glow(0, -3, 74, C[4], 0.24*math.Min(1, k)*flick)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			sr, k, L := sphereR(t), burning(t), starL(t)
			for _, r := range rings {
				deFloorRing(p, C, r, false)
			}
			// the charge: a contracting ring and a black core that beats
			cr := 0.0
			if t < deRel {
				q := t / deRel
				ring := 3 + 74*(1-pixelfx.Ease.InQuad(q))
				d1, d2 := t-deRel*0.5, t-deRel*0.8
				beat := 1 + 0.3*math.Exp(-(d1*d1)/0.0015) + 0.4*math.Exp(-(d2*d2)/0.0015)
				cr = coreR(t) * beat
				ringFast(p, 0, deY, ring, 1, pick(q > 0.7, C[6], C[5]), 0.2+0.7*q)
				if t > deRel-0.07 {
					p.Circle(0, deY, 1.6, pixelfx.White, 1)
				} else {
					deSphere(p, C, cr, t, 0.3+0.3*q)
				}
				halo(p, 0, deY, cr, cr+14+30*q, C[5], (0.45+0.7*q)*beat)
				p.Glow(0, deY, 60+30*q, C[4], 0.12+0.3*q)
			}
			// the release: a violet-white flash with a black star cut across it
			fu := (t - deRel) / 0.12
			if fu >= 0 && fu < 1 {
				r := 36 * (1 - 0.3*fu)
				p.Circle(0, deY, r+1.5, C[5], 1.2-fu)
				p.Circle(0, deY, r, pick(fu < 0.35, pixelfx.White, C[6]), 1.1-fu)
				if fu < 0.6 {
					p.Circle(0, deY, r*0.62, pixelfx.White, 1)
				}
			}
			// the shock rings in the air
			for _, r := range rings {
				if r.delay > 0 {
					continue
				}
				q := r.life / r.max
				rr := r.r0 + (r.r1-r.r0)*(1-q*q*q)
				th := math.Max(1, r.th*q+0.4)
				// the floor stops it: below the floor line it's the flat ring on the floor instead
				ringFastFn(p, 0, deY, rr, th, func(v, g float64) (uint32, float64) {
					var col uint32
					if r.hot {
						switch {
						case v > 0.6:
							col = pixelfx.White
						case v > 0.3:
							col = C[6]
						default:
							col = C[5]
						}
					} else {
						col = pick(v > 0.5, C[5], C[4])
					}
					al := 0.0
					if deY+math.Sin(g)*rr <= 0.5 {
						al = math.Min(1, q*1.6)
					}
					return col, al
				})
				if r.hot {
					ringLight(p, 0, deY, rr, 4, C[5], 0.4*q, func(_, y float64) bool { return y < 0.5 })
				}
			}
			heat := 1.0
			if t >= deBurn {
				heat = 1 + 0.5*pixelfx.Clamp((t-deBurn)/(dePop-deBurn))
			}
			deSphere(p, C, sr, t, heat)
			// the black star, burning at the edge where its arms leave the flash
			var star *starGrid
			if L > 0 {
				star = blackStar(deY, L, starRot(t), 0.5, 0.4)
				age := t - deRel
				fr := 0.0
				if age < 0.12 {
					fr = 36*(1-0.3*(age/0.12)) + 1.5
				}
				cut := math.Max(fr, sr)
				star.draw(p, C[0], pick(age < 0.1, C[5], C[4]), func(x, y float64) bool {
					dy := y - deY
					return x*x+dy*dy < cut*cut
				})
			}
			pf.draw(p, pkFlame, nil)
			lkShard := math.Min(1, k) * 0.9
			for i := range bits.List {
				shard(p, &bits.List[i], C, 0, deY, lkShard)
			}
			ps.Draw(p, pixelfx.LayerFront)
			embers.Draw(p, pixelfx.LayerFront)
			// the pop as it goes: a glint and a last hard light
			pu := (t - dePop) / 0.2
			if pu >= 0 && pu < 1 {
				pixelfx.Sparkle(p, 0, deY, 12*(1-pu), C[6], C[5], 0.7*(1-pu))
			}
			// light: the blast's flare and the sphere's corona; then black takes its light back
			for _, l := range lights {
				q := l.life / l.max
				p.Glow(l.x, l.y, l.r*(0.55+0.45*q), l.c, l.k*q*q)
			}
			if sr > 1 {
				halo(p, 0, deY, sr-0.5, sr+16, C[6], 0.5*flick*math.Min(1, k))
				p.Glow(0, deY, sr+54, C[4], 0.5*flick*math.Min(1, k))
			}
			// black takes its light back: the sphere and the core, then the star's own cells
			bMax := math.Max(sr, cr)
			B0 := bMax + 1
			if B0 > 2 {
				rLim := bMax - 0.5
				unlight(p, -B0, deY-B0, B0, deY+B0, func(x, y float64) bool {
					dy := y - deY
					return x*x+dy*dy < rLim*rLim
				}, 0)
			}
			if star != nil {
				star.unlight(p)
			}
		},
	}
}
