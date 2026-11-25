// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/electrified.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// Arcs leaping between points of the silhouette's edge, re-struck every other frame, grounding to the floor, and the periodic jolt that throws sparks follow Sprite Fusion's open-source lightning crackle (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	elTense = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Crouch.Is(3), pixelfx.FX.Is(-8), pixelfx.FY.Is(-17), pixelfx.BX.Is(9), pixelfx.BY.Is(-17), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	elJoltA = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.1), pixelfx.Crouch.Is(0), pixelfx.Y.Is(-1), pixelfx.FX.Is(-13), pixelfx.FY.Is(-36), pixelfx.BX.Is(13), pixelfx.BY.Is(-17), pixelfx.FFX.Is(-4), pixelfx.FBX.Is(5))
	elJoltB = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.7), pixelfx.Crouch.Is(1), pixelfx.Y.Is(-1), pixelfx.FX.Is(-13), pixelfx.FY.Is(-16), pixelfx.BX.Is(13), pixelfx.BY.Is(-36), pixelfx.FFX.Is(-4), pixelfx.FBX.Is(5))
)

// Electrified is current running through a character: arcs crawl over and leap across the body, sparks spit off, the body flares to an electric silhouette on each jolt, and the feet ground out. It is registered as "lightning/electrified".
var Electrified = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Electrified",
		Desc:     "Current runs through a character: arcs crawl over and leap across the body, sparks spit off, the body flares to an electric silhouette on each jolt, and the feet ground out.",
		Tags:     []string{"lightning", "electric", "status", "shock", "stun", "actor", "loop"},
		Size:     [2]int{108, 92},
		Origin:   [2]int{54, 76},
		Duration: 2,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  append(voltOptions(), pixelfx.Option{Name: "seed", Default: pixelfx.Num(5)}),
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: elTense},
			{T: 0.1, Pose: elJoltA, How: "snap"},
			{T: 0.17, Pose: elJoltB, How: "snap"},
			{T: 0.3, Pose: elTense, How: "snap"},
			{T: 0.733, Pose: elTense},
			{T: 0.733, Pose: elJoltB, How: "snap"},
			{T: 0.8, Pose: elJoltA, How: "snap"},
			{T: 0.93, Pose: elTense, How: "snap"},
			{T: 1.367, Pose: elTense},
			{T: 1.367, Pose: elJoltA, How: "snap"},
			{T: 1.45, Pose: elJoltB, How: "snap"},
			{T: 1.58, Pose: elTense, How: "snap"},
			{T: 2, Pose: elTense},
		},
	},
	New: newElectrified,
}

func init() { pixelfx.Register("lightning/electrified", Electrified) }

const (
	elFPS = 30
	elN   = 60
	elP   = 2
)

// elJolts are the jolts: the frame each starts and its strength.
var elJolts = [3]struct{ f0, k float64 }{{3, 1}, {22, 0.7}, {41, 0.9}}

var elLift = [3]float64{1.1, 0.6, -0.2}

// elJolt returns the jolt frame f is in: how many frames into it and its strength, and whether it is in one.
func elJolt(f int) (a int, k float64, ok bool) {
	for _, j := range elJolts {
		if ff := float64(f); ff >= j.f0 && ff < j.f0+3 {
			return int(ff - j.f0), j.k, true
		}
	}
	return 0, 0, false
}

func newElectrified(o *pixelfx.Options) pixelfx.Instance {
	C, S, seed, A := pixelfx.Fit(o.Palette(), 8), o.Ramp("sparks"), o.Seed(), o.Actor
	E := actorEdge(A, 0.5)
	ramp := C[1:8]
	pick := func(q *pixelfx.Rng) [2]float64 { return E[int(math.Floor(q.Next()*float64(len(E))))] }
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			f := frameOf(t, elFPS, elN)
			_, jk, jolt := elJolt(f)
			r := pixelfx.RngOf(seed, 4, float64(f))
			// a cold aura: a broken outline that flickers round the body, light pooling at the feet
			c := C[4]
			if !jolt {
				c = C[1]
				if r.Next() < 0.5 {
					c = C[2]
				}
			}
			p.Outline(A.Mask, A.X, A.Y, c, 1)
			k := 0.55 * jk
			if !jolt {
				k = 0.16 + 0.1*r.Next()
			}
			p.Glow(0, -18, 30, C[3], k)
			p.Glow(0, -2, 22, C[4], 0.18)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			f := frameOf(t, elFPS, elN)
			ja, jk, jolt := elJolt(f)
			// the jolt: the body flares to an electric silhouette, then fades back through its own shading
			if jolt {
				lift := elLift[ja] * jk
				if len(A.Pixels) >= A.W*A.H {
					for y := 0; y < A.H; y++ {
						for x := 0; x < A.W; x++ {
							if c := A.Pixels[y*A.W+x]; c != 0 {
								p.Px(A.X+float64(x), A.Y+float64(y), ramp[int(pixelfx.ClampTo(pixelfx.Round(0.4+pixelfx.Luma(c)*6+lift), 0, 6))])
							}
						}
					}
				} else if A.Mask != nil && len(A.Mask.Data) >= A.W*A.H {
					p.Mask(A.Mask, A.X, A.Y, ramp[int(pixelfx.ClampTo(pixelfx.Round(0.4+0.5*6+lift), 0, 6))], 1)
				}
				p.Glow(0, -19, 26, C[5], 0.5*jk*(1-float64(ja)/3))
			}
			// arcs: two or three at a time, re-struck every other frame, leaping between nearby points of the edge
			r := pixelfx.RngOf(seed, 7, float64(f>>1))
			n := 2.0
			if jolt {
				n += 2
			} else {
				n += r.Int(0, 1)
			}
			for i := 0; float64(i) < n; i++ {
				a := pixelfx.Pick(r, E)
				ang, d := r.Range(0, tau), r.Range(6, 14)
				b := [2]float64{a[0] + math.Cos(ang)*d, a[1] + math.Sin(ang)*d}
				for range 10 {
					c := pixelfx.Pick(r, E)
					if pixelfx.Hypot(c[0]-b[0], c[1]-b[1]) < 5 && pixelfx.Hypot(c[0]-a[0], c[1]-a[1]) > 4 {
						b = c
					}
				}
				k := 0.95
				if f&1 != 0 {
					k = 0.75
				}
				k *= r.Range(0.6, 1)
				crackle(p, zigzag(pixelfx.RngOf(seed, 8, float64(f), float64(i)), a[0], a[1], b[0], b[1], 0.42, 0.35), k, C, 0.4, 1)
				// the edge catches the light where an arc touches it
				for _, e := range E {
					if pixelfx.Hypot(e[0]-a[0], e[1]-a[1]) < 3.5 || pixelfx.Hypot(e[0]-b[0], e[1]-b[1]) < 3 {
						p.Px(e[0]-0.5, e[1]-0.5, C[5])
					}
				}
			}
			// the feet ground out to the floor now and then
			chance := 0.35
			if jolt {
				chance = 0.9
			}
			if r.Next() < chance {
				s := r.Sign()
				x0 := s * r.Range(3, 8)
				crackle(p, zigzag(pixelfx.RngOf(seed, 9, float64(f)), x0, -1.5, x0+s*r.Range(5, 12), -0.5, 0.45, 0.3), 0.7, C, 0.35, 1)
				p.Glow(x0, -1, 10, C[5], 0.35)
			}
			// sparks: a spray off the edge with every jolt, and a few spat out between them
			for _, j := range elJolts {
				age := pixelfx.Wrap(t-j.f0/elFPS, elP)
				for i := range 9 {
					q := pixelfx.RngOf(seed, 21, j.f0, float64(i))
					life := q.Range(0.18, 0.4)
					if age >= life {
						continue
					}
					e := pick(q)
					ang, sp := math.Atan2(e[1]+19, e[0])+q.Range(-0.7, 0.7), q.Range(60, 150)*j.k
					x, y, vx, vy := flight(e[0], e[1], math.Cos(ang)*sp, math.Sin(ang)*sp-30, age, 0)
					spark(p, x, y, vx, vy, 1-age/life, S)
				}
			}
			pixelfx.Cycle(t, elP, 8, seed+3, func(g *pixelfx.Rng) float64 { return g.Range(0.15, 0.3) }, func(age, life float64, q *pixelfx.Rng, _ int) {
				e := pick(q)
				ang, sp := math.Atan2(e[1]+19, e[0])+q.Range(-0.6, 0.6), q.Range(50, 110)
				x, y, vx, vy := flight(e[0], e[1], math.Cos(ang)*sp, math.Sin(ang)*sp-20, age, 0)
				spark(p, x, y, vx, vy, 1-age/life, S)
			})
		},
	}
}
