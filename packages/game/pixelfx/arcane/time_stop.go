// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/time-stop.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The cracks walking out from a point and the burst into tumbling shards, glints and light are adapted from Sprite Fusion's open-source ice shatter (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// the clock's height and radius, the beats; variables so expressions with them round step by step as JavaScript's do
var (
	tsCY      = -42.0
	tsRC      = 26.0
	tsOpen    = 0.3
	tsFreeze  = 1.75
	tsCrack   = 2.2
	tsShatter = 2.42
)

// tsTicks are when the second hand ticks, each wait longer than the last, until time stops.
var tsTicks = [8]float64{0.36, 0.46, 0.57, 0.7, 0.85, 1.03, 1.24, 1.49}

// TimeStop springs a clock face open; it ticks slower and slower, freezes drained of colour, cracks, then shatters into falling shards. It is registered as "arcane/time-stop".
var TimeStop = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Time stop",
		Desc:     "A clock face springs open, ticks slower and slower, freezes drained of colour, cracks, then shatters into falling shards.",
		Tags:     []string{"arcane", "time", "clock", "freeze", "stop", "shatter", "magic", "one-shot"},
		Size:     [2]int{196, 136},
		Origin:   [2]int{98, 118},
		Duration: 3.6,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(14)},
		},
	},
	New: newTimeStop,
}

func init() { pixelfx.Register("arcane/time-stop", TimeStop) }

// tsHand is how far the second hand has gone: a snap with a little overshoot at each tick.
func tsHand(t float64) float64 {
	a := 0.0
	for _, tk := range tsTicks {
		if t > tk {
			a += pixelfx.OutBack(pixelfx.Clamp((t-tk)/0.07), 3)
		}
	}
	return -pi/2 + a*(tau/12)
}

// tsFlow is how fast time flows: full speed, slowing to nothing at the freeze.
func tsFlow(t float64) float64 {
	if t < tsOpen {
		return 1
	}
	return math.Pow(math.Max(0, 1-(t-tsOpen)/(tsFreeze-tsOpen)), 1.6)
}

// tsClockTime is the motes' clock: time itself, slowing down (the flow integrated).
func tsClockTime(t float64) float64 {
	s := 0.0
	for u := 0.0; u < math.Min(t, tsFreeze); u += 1.0 / 120 {
		s += tsFlow(u) / 120
	}
	return s
}

type tsMote struct {
	a, r, w, tilt float64
	c             uint32
}

func newTimeStop(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed())
	// the drained ramp: each step's own lightness, cold grey
	G := make([]uint32, len(C))
	for i, c := range C {
		l := pixelfx.Luma(c) * 255
		G[i] = pixelfx.RGB(l*0.92, l*0.96, math.Min(255, l*1.06))
	}
	whites := make([]uint32, len(G))
	for i := range whites {
		whites[i] = pixelfx.White
	}
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	bits, lights := pixelfx.NewBits(zeroGround), &pixelfx.Lights{}
	motes := make([]tsMote, 14)
	for i := range motes {
		m := tsMote{a: R.Range(0, tau)}
		m.r = tsRC + R.Range(5, 14)
		m.w = R.Range(1.4, 2.6) * pickF(i%2 != 0, 1, -1)
		m.tilt = R.Range(-0.5, 0.5)
		m.c = pixelfx.Pick(R, []uint32{pixelfx.White, C[6], C[5]})
		motes[i] = m
	}
	// cracks: jagged walks out from a point on the face
	ix, iy := R.Range(-6, 6), R.Range(-6, 6)
	var cracks [][][2]float64
	var walk func(x, y, a, n float64, depth int)
	walk = func(x, y, a, n float64, depth int) {
		pts := [][2]float64{{x, y}}
		for s := 0.0; s < n; s++ {
			a += R.Range(-0.55, 0.55)
			x += math.Cos(a) * R.Range(1.6, 2.6)
			y += math.Sin(a) * R.Range(1.6, 2.6)
			if pixelfx.Hypot(x, y) > tsRC-0.5 {
				break
			}
			pts = append(pts, [2]float64{x, y})
			if depth < 1 && len(pts) > 3 && R.Chance(0.14) {
				walk(x, y, a+R.Sign()*R.Range(0.6, 1.1), n/2, depth+1)
			}
		}
		cracks = append(cracks, pts)
	}
	for i := range 7 {
		walk(ix, iy, (float64(i)/7)*tau+R.Range(-0.3, 0.3), 16, 0)
	}
	t, tc, ticked, shattered, frozen := 0.0, 0.0, 0, false, false

	// the face, in ramp K: glass, rims, ticks and dots, hands, hub
	face := func(p *pixelfx.Painter, K []uint32, s, cracked float64) {
		cy, r := tsCY, tsRC*s
		if r < 1 {
			return
		}
		p.DiscFn(0, cy, r, func(d, _ float64) (uint32, float64) { return K[1], 0.3 + 0.35*d*d })
		p.Ring(0, cy, r+0.5, 1, K[6], 1)
		p.Ring(0, cy, r-1.5, 1, K[4], 1)
		p.Ring(0, cy, r+2, 1, K[3], 0.6)
		// minute dots and hour ticks; the quarters long and bright
		for i := range 60 {
			a := (float64(i) / 60) * tau
			if i%5 != 0 {
				p.Dot(math.Cos(a)*(r-3.5), cy+math.Sin(a)*(r-3.5), K[4], 0.8)
				continue
			}
			q := i%15 == 0
			r0 := r - pickF(q, 8, 6.5)
			p.Line(math.Cos(a)*r0, cy+math.Sin(a)*r0, math.Cos(a)*(r-3), cy+math.Sin(a)*(r-3), pick(q, K[7], K[6]), 1)
		}
		p.Ring(0, cy, r*0.3, 1, K[4], 0.8)
		// the hour hand, then the second hand with its tail, then the hub
		ha := -pi/2 + (tsHand(t)+pi/2)/12 + 0.9
		sa := tsHand(t)
		if frozen {
			sa += math.Sin(t*70) * 0.012
		}
		p.BeamFn(0, cy, math.Cos(ha)*r*0.48, cy+math.Sin(ha)*r*0.48, 2.6, func(_, v float64) (uint32, float64) { return pick(v < 0.5, K[6], K[4]), 1 })
		p.LineFn(-math.Cos(sa)*5, cy-math.Sin(sa)*5, math.Cos(sa)*(r-4), cy+math.Sin(sa)*(r-4), func(u float64) (uint32, float64) { return pick(u > 0.75, K[7], K[6]), 1 })
		p.Line(-math.Cos(sa)*5+0.5, cy-math.Sin(sa)*5, -math.Cos(sa)*2, cy-math.Sin(sa)*2, K[5], 1)
		p.Circle(0, cy, 2.2, K[5], 1)
		p.Circle(0, cy, 1.2, K[7], 1)
		if cracked > 0 {
			for _, pts := range cracks {
				n := int(math.Floor(float64(len(pts)) * cracked))
				for i := 0; i < n; i++ {
					x, y := pts[i][0], pts[i][1]
					fresh := i > n-3 && cracked < 1
					p.Px(x, cy+y, pick(fresh, pixelfx.White, K[7]))
					if !fresh {
						p.Px(x, cy+y-1, K[2])
					}
				}
			}
		}
	}

	// a mote circling the clock on a tilted orbit, slowing with time; drained and still once it stops
	drawMote := func(p *pixelfx.Painter, m *tsMote, back bool) {
		a := m.a + m.w*tc
		z := math.Sin(a)
		if z < 0 != back {
			return
		}
		x, y := math.Cos(a)*m.r, tsCY+math.Cos(a)*m.r*m.tilt+z*m.r*0.3
		cold, v := t >= tsFreeze, tsFlow(t)*m.w
		p.Dot(x, y, pick(cold, G[6], m.c), pickF(back, 0.6, 1))
		// a tail while it still moves
		if math.Abs(v) > 0.2 {
			for k := 1; k <= 3; k++ {
				fk := float64(k)
				b := a - pixelfx.Sign(m.w)*fk*0.05*math.Min(1, math.Abs(v))
				p.Dot(math.Cos(b)*m.r, tsCY+math.Cos(b)*m.r*m.tilt+math.Sin(b)*m.r*0.3, pick(k < 2, C[5], C[4]), pickF(back, 0.5, 0.9)*(1-fk/4))
			}
		}
		if !back {
			p.Glow(x, y, 5, pick(cold, G[6], C[6]), 0.3)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			tc = tsClockTime(t)
			// each tick sends a ripple and a spark off the hand's tip
			for ticked < len(tsTicks) && t >= tsTicks[ticked] {
				ticked++
				a := tsHand(t + 0.1)
				lights.Add(math.Cos(a)*(tsRC-4), tsCY+math.Sin(a)*(tsRC-4), 22, C[6], 0.6, 0.12)
				for range 3 {
					vx, vy := math.Cos(a)*30+R.Range(-15, 15), math.Sin(a)*30+R.Range(-15, 15)
					life := R.Range(0.2, 0.4)
					c := pixelfx.Pick(R, []uint32{pixelfx.White, C[6]})
					ps.Spawn(pixelfx.StarKind, math.Cos(a)*(tsRC+1), tsCY+math.Sin(a)*(tsRC+1), vx, vy, life, c, R.Range(0.7, 1.3), 0, 0)
				}
			}
			if !frozen && t >= tsFreeze {
				frozen = true
				lights.Add(0, tsCY, 70, pixelfx.White, 0.7, 0.2)
			}
			if !shattered && t >= tsShatter {
				shattered = true
				lights.Add(0, tsCY, 80, pixelfx.White, 0.8, 0.16)
				lights.Add(0, tsCY, 100, C[5], 0.6, 0.6)
				// the face bursts into shards and chunks, cold grey; colour sparks back into the world around them
				for range 64 {
					a, d := R.Range(0, tau), R.Range(0, tsRC)
					x, y, sp := math.Cos(a)*d, tsCY+math.Sin(a)*d, R.Range(40, 140)
					dx, dy := x-ix, y-tsCY-iy
					l := or1(pixelfx.Hypot(dx, dy))
					vx := (dx/l)*sp + R.Range(-25, 25)
					vy := (dy/l)*sp*0.7 - R.Range(40, 120)
					life := R.Range(0.8, 1.15)
					ang, va, s := R.Range(0, tau), R.Range(-22, 22), R.Range(1.5, 3.6)
					c := pixelfx.Pick(R, []uint32{G[7], G[6], G[6], G[5]})
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: x, Y: y, VX: vx, VY: vy, G: 0.9, Drag: 0.5, Bounce: 0.25, Life: life, A: ang, VA: va, S: s, C: c})
				}
				for range 10 {
					a, sp := R.Range(0, tau), R.Range(40, 110)
					vy := math.Sin(a)*sp*0.6 - R.Range(60, 130)
					life := R.Range(0.9, 1.15)
					ang, va, s := R.Range(0, tau), R.Range(-14, 14), R.Range(1.6, 3)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Chunk, X: math.Cos(a) * tsRC * 0.6, Y: tsCY + math.Sin(a)*tsRC*0.6, VX: math.Cos(a) * sp, VY: vy, G: 1, Drag: 0.4, Bounce: 0.3, Life: life, A: ang, VA: va, S: s, Ramp: []uint32{G[1], G[3], G[4], G[6], G[7]}})
				}
				for i := range 26 {
					a, sp := R.Range(0, tau), R.Range(30, 110)
					k := pixelfx.Mote
					if i%2 != 0 {
						k = pixelfx.StarKind
					}
					life := R.Range(0.4, 0.8)
					c := pixelfx.Pick(R, []uint32{pixelfx.White, C[6], C[5]})
					ps.Spawn(k, 0, tsCY, math.Cos(a)*sp, math.Sin(a)*sp, life, c, R.Range(0.8, 1.8), 0, 0)
				}
				for range 18 {
					a, sp := R.Range(0, tau), R.Range(90, 200)
					ps.Spawn(pixelfx.Spark, 0, tsCY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.2, 0.45), pixelfx.White, 1, 0, 0)
				}
				// the stopped time it held lifts away as a cold vapour
				for range 12 {
					a, d := R.Range(0, tau), R.Range(0, tsRC*0.8)
					vx := math.Cos(a) * R.Range(10, 40)
					vy := math.Sin(a)*R.Range(10, 30) - R.Range(5, 20)
					life := R.Range(0.6, 1)
					c := pick(R.Chance(0.5), G[5], C[4])
					ps.Spawn(pixelfx.Vapour, math.Cos(a)*d, tsCY+math.Sin(a)*d, vx, vy, life, c, R.Range(3, 6), 0, 0)
				}
			}
			bits.Update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the floor lit beneath the clock while it stands
			k, cold := pixelfx.Smooth(0, tsOpen, t)*(1-pixelfx.Smooth(tsShatter, tsShatter+0.5, t)), t >= tsFreeze
			if k > 0 {
				p.Glow(0, -1, 40, pick(cold, G[5], C[4]), 0.25*k)
			}
			// the motes behind the face
			if t < tsShatter {
				for i := range motes {
					drawMote(p, &motes[i], true)
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			cold, s := t >= tsFreeze, 1.0
			if t < tsOpen {
				s = pixelfx.OutBack(t/tsOpen, 2.6)
			}
			if t < tsShatter {
				// white for a beat as it freezes, then cold grey; white again in the moment it bursts
				white := (t >= tsFreeze && t < tsFreeze+0.06) || t >= tsShatter-0.05
				K := C
				if white {
					K = whites
				} else if cold {
					K = G
				}
				face(p, K, s, pixelfx.Clamp((t-tsCrack)/0.18))
				p.Glow(0, tsCY, tsRC*1.9, pick(cold, G[5], C[4]), 0.32*s)
				p.GlowRing(0, tsCY, tsRC, 7, pick(cold, G[6], C[5]), 0.28*s)
				for i := range motes {
					drawMote(p, &motes[i], false)
				}
			}
			// springing open: a ring thrown out and a flare
			if t < 0.35 {
				q := t / 0.35
				p.Ring(0, tsCY, tsRC*(0.4+1.2*pixelfx.Ease.OutCubic(q)), math.Max(1, 3*(1-q)), pick(q < 0.3, pixelfx.White, C[6]), 1.2-q)
				if q < 0.5 {
					pixelfx.Flare(p, 0, tsCY, 26*(1-q*2)+2, 1-q*2, C)
				}
			}
			// a ripple of time off each tick; ripples run on the clock's own slowing time, so they drag to a halt and hang there, drained, until it all breaks
			if t < tsShatter {
				for _, tk := range tsTicks {
					if t < tk {
						continue
					}
					q := (tc - tsClockTime(tk)) / 0.32
					if q >= 1 {
						continue
					}
					p.Ring(0, tsCY, tsRC+3+24*pixelfx.Ease.OutCubic(q), 1, pick(cold, G[5], C[5]), pickF(cold, 0.6, 0.8)*math.Pow(1-q, 1.6))
				}
			}
			if t >= tsShatter && t < tsShatter+0.2 {
				q := (t - tsShatter) / 0.2
				pixelfx.Flare(p, 0, tsCY, 34*(1-q)+2, 1-q, C)
				p.Ring(0, tsCY, tsRC+30*pixelfx.Ease.OutCubic(q), math.Max(1, 3*(1-q)), pixelfx.White, 1.2-q)
			}
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
