// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/implosion.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The gathering streaks, the pulse rings and the release (cross flare, rings, light) are adapted from Sprite Fusion's open-source energy charge (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Implosion sucks everything in: streaks and grit rush inward, rings contract and a dark sphere collapses onto a crushed white point; a beat of stillness, then a sharp pop. It is registered as "explosions/implosion".
var Implosion = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Implosion",
		Desc:     "Everything is sucked in: streaks and grit rush inward, rings contract and a dark sphere collapses onto a crushed white point; a beat of stillness, then a sharp pop.",
		Tags:     []string{"implosion", "energy", "magic", "collapse", "pop", "one-shot"},
		Size:     [2]int{implW, implH},
		Origin:   [2]int{implOX, implOY},
		Duration: 1.65,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0a0614", "#1c1236", "#3a2470", "#6a44c0", "#a080f0", "#d8c8ff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newImplosion,
}

const implW, implH, implOX, implOY = 208, 184, 104, 92

func init() { pixelfx.Register("explosions/implosion", Implosion) }

// variables, so sums of them round as pixelfx's do
var implSuck, implStill = 0.8, 1.02

var implVoid = pixelfx.Hex("#05040a")

type implGrit struct {
	a, d, t0, T, spin float64
	c                 uint32
}

func newImplosion(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 7), pixelfx.NewRng(o.Seed())
	const OX, OY, W, H = implOX, implOY, implW, implH
	ps, lights := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[6], C[5], C[4], C[3]}}), &pixelfx.Lights{}
	// grit pulled in: specks that spiral toward the middle, faster and faster
	grit := make([]implGrit, 70)
	for i := range grit {
		g := &grit[i]
		g.t0 = R.Range(0, 0.4)
		g.a = R.Range(0, tau)
		g.d = R.Range(40, 86)
		g.T = R.Range(0.3, implSuck-0.04-g.t0)
		g.spin = R.Sign() * R.Range(0.6, 1.6)
		g.c = pixelfx.Pick(R, []uint32{C[2], C[3], C[4], pixelfx.Hex("#4a4458"), pixelfx.Hex("#6e6880")})
	}
	rays := make([]float64, 4)
	for i := range rays {
		rays[i] = (float64(i)*tau)/4 + pi/4
	}
	t, popped := 0.0, false
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < implSuck-0.26 {
				// streaks of light rushing in from all round, accelerating, more of them as it goes
				k := t / implSuck
				for n := 0; float64(n) < 1+math.Floor(k*4); n++ {
					if !R.Chance(0.9) {
						continue
					}
					a, D, T := R.Range(0, tau), R.Range(34, 82)*(1-0.3*k), R.Range(0.14, 0.24)
					v := (D * 3) / (math.Exp(3*T) - 1)
					ps.Spawn(pixelfx.Streak, math.Cos(a)*D, math.Sin(a)*D, -math.Cos(a)*v, -math.Sin(a)*v, T, pick(R.Chance(0.5), C[5], C[4]), 1, 0, 0)
				}
				// vapour drawn in along the way
				if R.Chance(14 * dt) {
					a, D := R.Range(0, tau), R.Range(50, 80)
					ps.Spawn(pixelfx.Vapour, math.Cos(a)*D, math.Sin(a)*D, -math.Cos(a)*90, -math.Sin(a)*90, R.Range(0.4, 0.6), C[3], R.Range(3, 5), 0, 0)
				}
			}
			if !popped && t >= implStill {
				popped = true
				lights.Add(0, 0, 90, C[4], 1, 0.4)
				lights.Add(0, 0, 30, pixelfx.White, 0.9, 0.08)
				for range 34 {
					a, sp := R.Range(0, tau), R.Range(120, 330)
					ps.Spawn(pick(R.Chance(0.6), pixelfx.Mote, pixelfx.StarKind), 0, 0, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.2, 0.45), pixelfx.Pick(R, []uint32{C[6], C[5], C[4]}), R.Range(0.8, 1.8), 0, 0)
				}
			}
			ps.Update(dt)
			cull(ps, -OX+3, -OY+3, W-OX-3, H-OY-3)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			k := pixelfx.Clamp(t / implSuck)
			if t < implStill {
				// the void: a dark sphere edged with light, collapsing onto the core
				vr := 64*(1-pixelfx.Ease.InCubic(k)) + 3
				if k < 1 {
					p.Disc(0, 0, vr, implVoid, 0.35+0.35*k)
					p.Ring(0, 0, vr+1, 1, pick(k < 0.5, C[3], C[4]), 0.5+0.5*k)
					p.GlowRing(0, 0, vr, 5, C[4], 0.18+0.3*k)
				}
				// rings pulled in after it, one after another, brighter as they go
				for i := range 4 {
					fi := float64(i)
					u := (t - fi*0.17) / 0.34
					if u <= 0 || u >= 1 {
						continue
					}
					r := (70-fi*8)*(1-pixelfx.Ease.InQuad(u)) + 2
					p.Ring(0, 0, r, pick(u > 0.7, 2.0, 1), pick(u > 0.6, C[5], C[3]), 0.4+0.6*u)
				}
				// the grit, spiralling in
				for _, q := range grit {
					u := (t - q.t0) / q.T
					if u <= 0 || u >= 1 {
						continue
					}
					d, a := q.d*(1-pixelfx.Ease.InCubic(u)), q.a+q.spin*pixelfx.Ease.InQuad(u)*2
					x, y := math.Cos(a)*d, math.Sin(a)*d
					x2, y2 := math.Cos(a-q.spin*0.1)*(d+4*u*u+0.5), math.Sin(a-q.spin*0.1)*(d+4*u*u+0.5)
					dim := pixelfx.Mix(q.c, implVoid, 0.4)
					p.LineFn(x2, y2, x, y, func(v float64) (uint32, float64) { return pick(v < 0.5, dim, q.c), 1 })
				}
				// the core: growing hotter as it's crushed, then a white point trembling in the stillness
				still := t > implSuck
				jit, cr := 0.0, 1+3.5*(1-k)*k*4
				if still {
					jit, cr = -0.5, 1.6
					if odd(math.Floor(t * 40)) {
						jit = 0.5
					}
				}
				p.Disc(jit, 0, cr+3.5, implVoid, 0.85)
				p.Circle(jit, 0, cr+1.2, C[4], 1)
				p.Circle(jit, 0, cr, pixelfx.White, 1)
				p.Glow(0, 0, 10+26*k, C[4], 0.3+0.5*k)
				if still {
					p.Glow(0, 0, 8, pixelfx.White, 0.4+0.2*math.Sin(t*60))
				}
			} else {
				// the pop: a white point bursting, a cross flare, a fast thin ring and a slower one
				q := (t - implStill) / 0.5
				e := pixelfx.Ease.OutCubic(pixelfx.Clamp(q))
				if q < 0.08 {
					p.Circle(0, 0, 6*(1-q/0.08)+1, pixelfx.White, 1)
				}
				if q < 0.35 {
					L := (1-q/0.35)*34 + 3
					for _, a := range rays {
						for d := 0.0; d <= L; d += 0.5 {
							al := 1.0
							if d >= L*0.7 {
								al = 1 - (d/L-0.7)/0.3
							}
							p.Dot(math.Cos(a)*d, math.Sin(a)*d, pick(d < L*0.4, pixelfx.White, C[5]), al)
						}
					}
					for _, a := range rays {
						for d := 0.0; d <= L*0.45; d += 0.5 {
							p.Dot(math.Cos(a+pi/4)*d, math.Sin(a+pi/4)*d, C[5], 1-d/(L*0.45))
						}
					}
				}
				if q < 1 {
					p.Ring(0, 0, 4+74*e, math.Max(1, 2.5*(1-q)), pick(q < 0.2, pixelfx.White, C[5]), 1.15-q)
					p.Ring(0, 0, 2+40*pixelfx.Ease.OutCubic(pixelfx.Clamp(q*1.4)), 1, C[4], 1-q)
				}
				if q < 0.6 {
					pixelfx.Sparkle(p, 0, 0, 5*(1-q/0.6), pixelfx.White, C[5], 0.6)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}

// odd reports whether the whole number x is odd, as JavaScript's x % 2 is truthy.
func odd(x float64) bool { return math.Mod(x, 2) != 0 }
