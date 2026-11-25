// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/mana-burst.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The release (a white disc, a cross, two rings, a big light and a spray of motes and stars) is adapted from Sprite Fusion's open-source energy charge (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// the burst's height, how long it gathers, how many runes it flings; variables so expressions with them round step by step as JavaScript's do
var (
	burstCY     = -28.0
	burstGather = 0.22
	burstN      = 9
)

// ManaBurst rushes energy into a point that blows out in a white flash and rings, flinging glowing runes that flare and pop into sparks one after another. It is registered as "arcane/mana-burst".
var ManaBurst = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Mana burst",
		Desc:     "Energy rushes into a point and blows out in a white flash and rings, flinging glowing runes that flare and pop into sparks one after another.",
		Tags:     []string{"arcane", "burst", "mana", "rune", "nova", "magic", "ground", "one-shot"},
		Size:     [2]int{208, 150},
		Origin:   [2]int{104, 124},
		Duration: 1.8,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newManaBurst,
}

func init() { pixelfx.Register("arcane/mana-burst", ManaBurst) }

type burstRune struct {
	g                     int
	x, y, vx, vy, pop, ph float64
	popped                bool
}

// burstPop is where and when a rune popped.
type burstPop struct{ t, x, y float64 }

func newManaBurst(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	// the runes: flung out round the burst, slowing hard, bobbing while they burn, each popping in its turn
	runes := make([]burstRune, burstN)
	for i := range runes {
		a := (float64(i)/float64(burstN))*tau + R.Range(-0.25, 0.25) - pi/2
		sp := R.Range(150, 210)
		// thrown less far downward, so the low ones stay clear of the floor
		r := burstRune{g: i, x: 0, y: burstCY, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp * pickF(math.Sin(a) > 0, 0.42, 0.85)}
		r.pop = burstGather + 0.42 + float64(i)*0.065 + R.Range(0, 0.05)
		r.ph = R.Range(0, tau)
		runes[i] = r
	}
	var pops []burstPop
	t, released := 0.0, false

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < burstGather-0.03 {
				// the gather: streaks rush in from all round
				for range 2 {
					a, D, T := R.Range(0, tau), R.Range(24, 52), R.Range(0.12, 0.18)
					v := (D * 3) / (math.Exp(3*T) - 1)
					x, y := math.Cos(a)*D, math.Min(-2, burstCY+math.Sin(a)*D*0.85)
					l := or1(pixelfx.Hypot(x, y-burstCY))
					ps.Spawn(pixelfx.Streak, x, y, (-x/l)*v, ((burstCY-y)/l)*v, T, pick(R.Chance(0.5), C[6], C[5]), 1, 0, 0)
				}
			}
			if t >= burstGather && !released {
				released = true
				lights.Add(0, burstCY-12, 96, C[5], 1, 0.5)
				lights.Add(0, burstCY, 40, pixelfx.White, 0.9, 0.1)
				// a spray of motes and stars, thrown less hard at the floor
				for range 96 {
					a, sp := R.Range(0, tau), R.Range(100, 250)
					dn := pickF(math.Sin(a) > 0, 0.45, 1)
					k := pixelfx.StarKind
					if R.Chance(0.65) {
						k = pixelfx.Mote
					}
					life := R.Range(0.25, 0.55)
					c := pixelfx.Pick(R, []uint32{pixelfx.White, C[6], C[5]})
					ps.Spawn(k, 0, burstCY, math.Cos(a)*sp, math.Sin(a)*sp*dn, life, c, R.Range(0.8, 1.8), 0, 0)
				}
				for range 20 {
					a, sp := R.Range(0, tau), R.Range(130, 280)
					ps.Spawn(pixelfx.Spark, 0, burstCY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.2, 0.45), pixelfx.White, 1, 0, 0)
				}
			}
			if released {
				for i := range runes {
					r := &runes[i]
					if r.popped {
						continue
					}
					r.vx -= r.vx * 4.2 * dt
					r.vy -= r.vy*4.2*dt + 8*dt
					r.x += r.vx * dt
					r.y += r.vy * dt
					if R.Chance(0.35) {
						x := r.x + R.Range(-1.5, 1.5)
						vx := r.vx*0.2 + R.Range(-10, 10)
						vy := r.vy*0.2 - R.Range(5, 25)
						ps.Spawn(pixelfx.Mote, x, r.y, vx, vy, R.Range(0.2, 0.4), C[5], 0.5, 0, 0)
					}
					if t >= r.pop {
						r.popped = true
						pops = append(pops, burstPop{t, r.x, r.y})
						lights.Add(r.x, r.y, 34, C[5], 0.9, 0.22)
						lights.Add(r.x, r.y, 12, pixelfx.White, 0.8, 0.06)
						for range 9 {
							a, sp := R.Range(0, tau), R.Range(70, 200)
							ps.Spawn(pixelfx.Spark, r.x, r.y, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.15, 0.4), pixelfx.White, 1, 0, 0)
						}
						for j := range 5 {
							a, sp := R.Range(0, tau), R.Range(20, 80)
							k := pixelfx.Mote
							if j%2 != 0 {
								k = pixelfx.StarKind
							}
							life := R.Range(0.3, 0.6)
							c := pixelfx.Pick(R, []uint32{pixelfx.White, C[6]})
							ps.Spawn(k, r.x, r.y, math.Cos(a)*sp, math.Sin(a)*sp-20, life, c, R.Range(0.8, 1.6), 0, 0)
						}
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the floor beneath catches the burst
			k := math.Max(0, 1-(t-burstGather)/1.1)
			if t < burstGather {
				k = 0.4 * (t / burstGather)
			}
			if k > 0 {
				p.Glow(0, -1, 50, C[4], 0.3*k)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < burstGather {
				// the point it gathers into: a swelling ball with a white heart
				k := t / burstGather
				r := 1 + 3.5*k*k
				p.Circle(0, burstCY, r+1.2, C[4], 1)
				p.Circle(0, burstCY, r, C[6], 1)
				p.Circle(0, burstCY, r*0.5, pixelfx.White, 1)
				p.Glow(0, burstCY, 10+20*k, C[5], 0.4+0.4*k)
				q := pixelfx.Clamp((t - burstGather*0.35) / (burstGather * 0.65))
				if q > 0 && q < 1 {
					p.Ring(0, burstCY, r+2+16*(1-pixelfx.Ease.OutCubic(q)), 1, pick(q > 0.6, pixelfx.White, C[6]), 1-q*0.5)
				}
			} else if q := (t - burstGather) / 0.6; q < 1 {
				e := pixelfx.Ease.OutCubic(q)
				if q < 0.12 {
					p.Circle(0, burstCY, 12*(1-q/0.12)+2, C[5], 1)
					p.Circle(0, burstCY, 12*(1-q/0.12), pixelfx.White, 1)
				}
				if q < 0.32 {
					pixelfx.Flare(p, 0, burstCY, 30*(1-q/0.32)+4, 1-q/0.32, C)
				}
				// the rings, cut off where they meet the floor
				c := pick(q < 0.2, pixelfx.White, C[6])
				p.RingFn(0, burstCY, 4+62*e, math.Max(1, 5*(1-q)), func(_, ang float64) (uint32, float64) {
					return c, pickF(burstCY+math.Sin(ang)*(4+62*e) > 0, 0, 1.2-q)
				})
				p.RingFn(0, burstCY, 2+38*pixelfx.Ease.OutCubic(math.Min(1, q*1.4)), math.Max(1, 2*(1-q)), func(_, ang float64) (uint32, float64) {
					return C[5], pickF(burstCY+math.Sin(ang)*40 > 0, 0, 1-q)
				})
				p.Glow(0, burstCY, 60*(0.4+0.6*e), C[5], 0.9*(1-q))
			}
			// the runes: white as they fly, settling to lilac, flaring white just before they pop
			for i := range runes {
				r := &runes[i]
				if !released || r.popped {
					continue
				}
				left := r.pop - t
				hot, bob := t-burstGather < 0.08 || left < 0.06, math.Sin(t*9+r.ph)*0.6
				sigil(p, r.g, r.x, r.y+bob, pick(hot, pixelfx.White, C[6]), 1, pick(hot, C[4], C[3]))
				p.Glow(r.x, r.y+bob, 10, C[5], pickF(hot, 0.7, 0.35))
				if left < 0.06 {
					pixelfx.Sparkle(p, r.x, r.y+bob, 3+4*(1-left/0.06), C[6], C[5], 0.5)
				}
			}
			// each pop: a small flare and a ring
			for _, q := range pops {
				k := (t - q.t) / 0.32
				if k >= 1 {
					continue
				}
				if k < 0.35 {
					pixelfx.Flare(p, q.x, q.y, 11*(1-k/0.35)+2, 1-k/0.35, C)
				}
				p.Ring(q.x, q.y, 2+11*pixelfx.Ease.OutCubic(k), math.Max(1, 2*(1-k)), pick(k < 0.3, pixelfx.White, C[6]), 1.15-k)
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
