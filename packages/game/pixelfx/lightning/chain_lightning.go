// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/chain-lightning.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The hop timing, the link shapes that re-strike every other frame, the glowing nodes and the crawling arcs follow Sprite Fusion's open-source lightning (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	clAim  = pixelfx.Ready.With(pixelfx.X.Is(-6), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(4), pixelfx.FX.Is(16), pixelfx.FY.Is(-16), pixelfx.BX.Is(-6), pixelfx.BY.Is(-23), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-9))
	clBack = pixelfx.Ready.With(pixelfx.X.Is(-6), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-3), pixelfx.FX.Is(-5), pixelfx.FY.Is(-32), pixelfx.BX.Is(8), pixelfx.BY.Is(-24), pixelfx.ArmsFront.Is(1), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-7))
)

// clZap is the hit as a link lands on target at, at t, freezing stop seconds.
func clZap(at, t, stop float64) pixelfx.Hit {
	return pixelfx.Hit{T: t, At: pixelfx.Val(at), Stop: stop, Push: pixelfx.Val(1.2), Straw: pixelfx.Val(6), Skin: "shock", Shiver: 0.7, Y: pixelfx.Val(-22)}
}

// ChainLightning is a bolt leaping from the caster's hand through three targets in turn; each flashes, sparks and grounds itself to the floor while the arcs between them flicker, then the chain snaps. It is registered as "lightning/chain-lightning".
var ChainLightning = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Chain lightning",
		Desc:     "A bolt leaps from the caster's hand through three targets in turn; each flashes, sparks and grounds itself to the floor while the arcs between them flicker, then the chain snaps.",
		Tags:     []string{"lightning", "electric", "chain", "spell", "combat", "directional", "one-shot"},
		Size:     [2]int{248, 104},
		Origin:   [2]int{40, 82},
		Duration: 1.8,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  append(voltOptions(), pixelfx.Option{Name: "seed", Default: pixelfx.Num(3)}),
	},
	Act: &pixelfx.Act{
		Targets: []float64{58, 104, 150},
		Poses: []pixelfx.Key{
			{T: -0.46, Pose: pixelfx.Ready.With(pixelfx.X.Is(-6))},
			{T: -0.22, Pose: clBack},
			{T: 0, Pose: clBack.With(pixelfx.Crouch.Is(5))},
			{T: 0.04, Pose: clAim, How: "snap"},
			{T: 0.7, Pose: clAim.With(pixelfx.Crouch.Is(3))},
			{T: 1.35, Pose: clAim.With(pixelfx.Crouch.Is(3), pixelfx.FY.Is(-18))},
			{T: 1.8, Pose: pixelfx.Ready.With(pixelfx.X.Is(-6))},
		},
		Hits: []pixelfx.Hit{
			clZap(0, 0.05, 0.03),
			clZap(1, 0.14, 0.03),
			func() pixelfx.Hit { h := clZap(2, 0.23, 0.07); h.Shake, h.Amp = 0.14, pixelfx.Val(1); return h }(),
			{T: 1.35, Push: pixelfx.Val(0), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
	},
	New: newChainLightning,
}

func init() { pixelfx.Register("lightning/chain-lightning", ChainLightning) }

// The hand, the targets, and the chain's timing: the wind-up, how long a leap takes, the gap between leaps, when the chain starts to give out and how long that takes. Variables, so sums of them round as JavaScript's do.
var (
	clHand                               = [2]float64{10, -16}
	clHops                               = [3][2]float64{{58, -22}, {104, -30}, {150, -19}}
	clWind, clLeap, clGap, clHold, clBrk = 0.12, 0.05, 0.075, 0.78, 0.16
	clAsh                                = pixelfx.Hex("#5b5566")
	clFirst                              = [3]float64{1, 0.55, 1}
)

// clOn returns when link i lands on its target, clOff when it starts to give out.
func clOn(i int) float64  { return clWind + float64(i)*clGap + clLeap }
func clOff(i int) float64 { return clHold + float64(i)*0.05 }

// clLevel returns link i's strength at t: a hard first flash, a restless hum, then it stutters out.
func clLevel(i int, t float64) float64 {
	a := t - clOn(i)
	if a < 0 {
		return 0
	}
	fi := float64(i)
	hum := 0.78 + 0.22*math.Sin(t*61+fi)*math.Sin(t*23+fi*2)
	if a < 0.1 {
		hum = 1
		if n := int(math.Floor(a * 30)); n >= 0 && n < len(clFirst) {
			hum = clFirst[n]
		}
	}
	b := t - clOff(i)
	switch {
	case b < 0:
		return hum
	case b < clBrk:
		s := 1.0
		if int(math.Floor(b*60))%3 == 1 {
			s = 0.5
		}
		return hum * (1 - b/clBrk) * s
	}
	return 0
}

func newChainLightning(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	R := pixelfx.NewRng(seed)
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: o.Ramp("sparks")})
	hot := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor})
	lights := &pixelfx.Lights{}
	pts := [4][2]float64{clHand, clHops[0], clHops[1], clHops[2]}
	var hit, popped [3]bool
	// where each target grounds itself: a spot on the floor below it
	var ground [3]float64
	for i, h := range clHops {
		ground[i] = h[0] + R.Range(-5, 5)
	}
	t := 0.0
	shape := func(i, f int) bolt {
		r := pixelfx.RngOf(seed, 31, float64(i), float64(f))
		x0, y0, x1, y1 := pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]
		L := pixelfx.Hypot(x1-x0, y1-y0)
		return bowed(r, x0, y0, x1, y1, -L*r.Range(0.05, 0.15), 0.2, 0.08)
	}
	smoke := pixelfx.Mix(clAsh, C[2], 0.35)
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the charge gathers at the hand
			if t < clWind && R.Chance(70*dt) {
				a, d, T := R.Range(0, tau), R.Range(10, 24), R.Range(0.06, 0.1)
				v := (d * 3) / (math.Exp(3*T) - 1)
				c := C[5]
				if R.Chance(0.5) {
					c = C[6]
				}
				ps.Spawn(pixelfx.Streak, clHand[0]+math.Cos(a)*d, clHand[1]+math.Sin(a)*d, -math.Cos(a)*v, -math.Sin(a)*v, T, c, 1, 0, 0)
			}
			for i, h := range clHops {
				x, y := h[0], h[1]
				if !hit[i] && t >= clOn(i) {
					hit[i] = true
					dir := math.Atan2(y-pts[i][1], x-pts[i][0])
					lights.Add(x, y, 56, C[4], 0.85, 0.22)
					lights.Add(x, y, 16, pixelfx.White, 0.7, 0.08)
					if i == 0 {
						lights.Add(clHand[0], clHand[1], 40, C[4], 0.6, 0.15)
					}
					for range 18 {
						var a float64
						if R.Chance(0.6) {
							a = dir + R.Range(-1.1, 1.1)
						} else {
							a = R.Range(0, tau)
						}
						sp := R.Range(70, 230)
						ps.Spawn(pixelfx.Spark, x, y, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.15, 0.4), pixelfx.White, 1, 0, 0)
					}
					for range 5 {
						hot.Spawn(pixelfx.Spark, x+R.Range(-2, 2), y+2, R.Range(-50, 50), R.Range(-60, 20), R.Range(0.5, 0.9), pixelfx.White, 1, 0, 0)
					}
					ps.Spawn(pixelfx.StarKind, x+R.Range(-4, 4), y-R.Range(3, 7), 0, -6, R.Range(0.25, 0.4), C[6], R.Range(1.5, 2.5), 0, 0)
					for range 2 {
						ps.Spawn(pixelfx.Smoke, x+R.Range(-4, 4), y+R.Range(-2, 3), R.Range(-6, 6), -R.Range(8, 16), R.Range(0.7, 1), smoke, R.Range(2, 3.5), R.Range(0.1, 0.25), 0)
					}
				}
				// while the chain holds: sparks shed off the links, spit from the floor where each target grounds
				if hit[i] && t < clOff(i) {
					if R.Chance(9 * dt) {
						u := R.Next()
						ps.Spawn(pixelfx.Spark, pixelfx.Lerp(pts[i][0], x, u), pixelfx.Lerp(pts[i][1], y, u)-2, R.Range(-60, 60), R.Range(-90, 20), R.Range(0.1, 0.25), pixelfx.White, 1, 0, 0)
					}
					if R.Chance(7 * dt) {
						ps.Spawn(pixelfx.Spark, ground[i], -1, R.Range(-70, 70), -R.Range(40, 140), R.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
					}
					if R.Chance(2 * dt) {
						hot.Spawn(pixelfx.Spark, x+R.Range(-3, 3), y+3, R.Range(-30, 30), R.Range(-30, 10), R.Range(0.5, 0.8), pixelfx.White, 1, 0, 0)
					}
				}
				// when it gives out, the target pops once more and smokes
				if !popped[i] && t >= clOff(i)+clBrk {
					popped[i] = true
					for range 7 {
						a, sp := R.Range(0, tau), R.Range(40, 140)
						ps.Spawn(pixelfx.Spark, x, y, math.Cos(a)*sp, math.Sin(a)*sp-30, R.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
					}
					ps.Spawn(pixelfx.Smoke, x, y, R.Range(-4, 4), -R.Range(10, 16), R.Range(0.55, 0.7), smoke, R.Range(2.5, 3.5), 0, 0)
					lights.Add(x, y, 30, C[4], 0.5, 0.12)
				}
			}
			ps.Update(dt)
			hot.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the floor scorches where the targets ground themselves
			for i := range clHops {
				a := t - clOn(i)
				if a < 0 {
					continue
				}
				T0 := math.Min(1, a/0.2) * math.Pow(pixelfx.Clamp(1-(t-clOff(i))/0.7), 1.3) * 0.9
				scorch(p, ground[i], 0, 7, T0, 1-pixelfx.Smooth(1.35, 1.75, t))
			}
			ps.Draw(p, pixelfx.LayerBack)
			hot.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			f := int(math.Floor(t*30 + 1e-4))
			f2 := f >> 1
			H := clHand
			// the hand: a node that swells while the charge gathers and holds the chain
			kh := clLevel(0, t)
			if t < clWind {
				kh = t / clWind
			}
			if kh > 0.02 {
				r := (1.2 + 1.6*kh + math.Sin(t*53)*0.4) * math.Min(1, kh*1.5)
				p.Circle(H[0], H[1], r+1.2, C[4], 1)
				p.Circle(H[0], H[1], r, C[6], 1)
				p.Circle(H[0], H[1], r*0.5, pixelfx.White, 1)
				p.Glow(H[0], H[1], 18, C[4], 0.6*kh)
				if t > clWind*0.5 {
					r2 := pixelfx.RngOf(seed, 5, float64(f))
					a, d := r2.Range(0, tau), r2.Range(4, 8)
					crackle(p, zigzag(r2, H[0], H[1], H[0]+math.Cos(a)*d, H[1]+math.Sin(a)*d, 0.4, 0.3), 0.6*kh, C, 0.3, 1)
				}
			}
			for i, h := range clHops {
				x, y := h[0], h[1]
				fi := float64(i)
				t0 := clWind + fi*clGap
				// the leap: the bolt races out from the last node with a hot head
				if t >= t0 && t < clOn(i) {
					u := pixelfx.Ease.OutQuad((t - t0) / clLeap)
					b := shape(i, -1)
					stroke(p, b[:1], 1, C, 0.75, strokeOpts{upTo: pixelfx.Val(u)})
					tp := b[0].pts
					hp := tp[min(len(tp)-1, int(math.Floor(u*float64(len(tp)-1))))]
					p.Circle(hp[0], hp[1], 2, pixelfx.White, 1)
					p.Glow(hp[0], hp[1], 16, C[5], 0.7)
				}
				k := clLevel(i, t)
				if k > 0 {
					stroke(p, shape(i, f2), k, C, 0.75, strokeOpts{})
				} else if gone := clOff(i) + clBrk; t >= gone && t < gone+0.35 {
					// the spent channel: a dotted trace that cools and goes
					g := 1 - (t-clOff(i)-clBrk)/0.35
					b := shape(i, int(math.Floor(gone*15)))
					c := C[2]
					if g > 0.5 {
						c = C[4]
					}
					traceAll(p, b[0].pts, c, g*0.9)
				}
				// the target: a flash as the bolt lands, then a node with arcs crawling over it and down to the floor
				if a := t - clOn(i); a >= 0 && a < 0.09 {
					q := a / 0.09
					p.Circle(x, y, 6*(1-q)+1.5, C[5], 1)
					p.Circle(x, y, 5*(1-q), pixelfx.White, 1)
					pixelfx.Sparkle(p, x, y, pixelfx.Round(10*(1-q)), C[6], C[4], 0.6)
				}
				if k > 0.05 {
					r := (2.6 + math.Sin(t*47+fi)*0.7) * k
					p.Circle(x, y, r+1.5, C[2], 1)
					p.Circle(x, y, r+0.7, C[5], 1)
					p.Circle(x, y, r*0.6, pixelfx.White, 1)
					p.Glow(x, y, 26, C[4], 0.6*k)
					p.Glow(x, y, 8, C[6], 0.5*k)
					// arcs crawl over the target as if over a body caught in the current
					r2 := pixelfx.RngOf(seed, 11, fi, float64(f))
					n := 1
					if t-clOn(i) < 0.12 {
						n = 2
					}
					for range n {
						a0 := r2.Range(0, tau)
						a1 := a0 + r2.Sign()*r2.Range(0.5, 1)
						d0, d1 := r2.Range(4, 6), r2.Range(5, 8)
						crackle(p, zigzag(r2, x+math.Cos(a0)*d0, y+math.Sin(a0)*d0, x+math.Cos(a1)*d1, y+math.Sin(a1)*d1, 0.35, 0), r2.Range(0.45, 0.7)*k, C, 0.25, 1)
					}
					if r2.Next() < 0.3*k {
						gx := ground[i] + r2.Range(-3, 3)
						crackle(p, zigzag(r2, x+r2.Range(-2, 2), y+3, gx, -0.5, 0.3, 0.35), 0.75*k, C, 0.35, 1)
						p.Glow(gx, -1, 12, C[5], 0.5*k)
						p.Px(gx, -1, pixelfx.White)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			hot.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
