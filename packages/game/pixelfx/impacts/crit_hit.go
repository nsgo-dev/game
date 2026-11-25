// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/crit-hit.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the star, its key frames and the spark spray from Sprite Fusion's open-source impact (https://www.spritefusion.com/pixel-art-vfx/impact), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// CritHit is a critical hit: a jagged starburst slams in white and overshoots, a ring breaks into flying arcs, sparks streak off, shards tumble to the floor under a big flare. It is registered as "impacts/crit-hit".
var CritHit = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Critical hit",
		Desc:     "A critical hit: a jagged starburst slams in white and overshoots, a ring breaks into flying arcs, sparks streak off, shards tumble to the floor under a big flare.",
		Tags:     []string{"impact", "hit", "crit", "critical", "combat", "directional", "one-shot"},
		Size:     [2]int{216, 136},
		Origin:   [2]int{84, 106},
		Duration: 0.9,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a0510", "#5c0a1c", "#a3142a", "#e0302c", "#ff7a2a", "#ffc23d", "#fff09a", "#ffffff")},
			{Name: "height", Default: pixelfx.Num(26)},
			{Name: "aim", Default: pixelfx.Num(0.12)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newCritHit,
}

func init() { pixelfx.Register("impacts/crit-hit", CritHit) }

// critKeys are the star's key frames: from t, its scale, thickness and alpha, and its colors (rim, body, inner, core) as steps of the ramp; the last only ends the one before.
var critKeys = [...]struct {
	t, sc, th, a float64
	cols         [4]int
}{
	{0, 1, 1, 1, [4]int{6, 7, 7, 7}},
	{0.025, 1.3, 1, 1, [4]int{2, 5, 6, 7}},
	{0.058, 1.05, 0.72, 1, [4]int{2, 4, 5, 6}},
	{0.092, 1.22, 0.3, 1, [4]int{1, 2, 3, 4}},
	{t: 0.125},
}

// critFlare draws a lens flare: a long level streak, thick and white at the heart, a shorter upright one, on a bed of light.
func critFlare(p *pixelfx.Painter, x, y, L, k float64, C []uint32) {
	for _, r := range [4][3]float64{{1, 0, L}, {0, 1, L * 0.45}, {0.7, 0.7, L * 0.16}, {0.7, -0.7, L * 0.16}} {
		dx, dy, n := r[0], r[1], r[2]
		for _, s := range [2]float64{-1, 1} {
			for d := 1.0; d <= n; d++ {
				u := d / n
				a := 1.0
				if u >= 0.7 {
					a = k * (1.6 - u)
				}
				c := C[5]
				if u < 0.35 {
					c = pixelfx.White
				} else if u < 0.7 {
					c = C[6]
				}
				p.Dot(x+s*dx*d, y+s*dy*d, c, a)
				if dy == 0 && u < 0.42 {
					for _, e := range [2]float64{-1, 1} {
						ec, ea := C[5], k
						if u < 0.2 {
							ec = C[6]
						}
						if u < 0.3 {
							ea = 1
						}
						p.Dot(x+s*d, y+e, ec, ea)
					}
				}
			}
		}
	}
	p.GlowLine(x-L, y, x+L, y, 6, C[6], 0.5*k)
	p.GlowLine(x, y-L*0.45, x, y+L*0.45, 4, C[6], 0.3*k)
}

func newCritHit(o *pixelfx.Options) pixelfx.Instance {
	C, R, X, Y, A := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), 0.0, -o.Num("height"), o.Num("aim")
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[3]}})
	bits, lights := pixelfx.NewBits(floor), &pixelfx.Lights{}
	sp := spikes(R, 14, A, spikeOpts{len0: 11, len1: 15, w0: 1.8, w1: 2.5, fwd: 1.4, back: 0.6, jag: 0.5, jit: 0.18})
	// the two longest spikes reach further
	order := make([]int, len(sp))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		switch {
		case sp[b].len < sp[a].len:
			return -1
		case sp[b].len > sp[a].len:
			return 1
		}
		return 0
	})
	for _, i := range order[:2] {
		sp[i].len *= 1.3
	}
	segs := make([]ringSeg, 7)
	for i := range segs {
		a := A + (float64(i)/7)*pixelfx.Tau
		segs[i] = ringSeg{a: a, gap: R.Range(0.55, 0.85), dr: R.Range(0.08, 0.4) + 0.15*math.Max(0, math.Cos(a-A)), w: 0.7 + 0.5*math.Max(0, math.Cos(a-A))}
	}

	lights.Add(X, Y, 84, C[5], 0.6, 0.5)
	lights.Add(X, Y, 30, pixelfx.White, 0.7, 0.1)
	lights.Add(X, -4, 44, C[4], 0.3, 0.45)
	// sparks: most sprayed on along the blow, a few kicked back
	for range 36 {
		back := R.Chance(0.22)
		a0, spread, vk := A, 0.8, 1.0
		if back {
			a0, spread, vk = A+pi, 0.6, 0.6
		}
		a := a0 + R.Range(-1, 1)*spread
		v := R.Range(170, 430) * vk
		life := R.Range(0.15, 0.5)
		delay := 0.0
		if R.Chance(0.3) {
			delay = R.Range(0, 0.04)
		}
		ps.Spawn(pixelfx.Spark, X, Y, math.Cos(a)*v, math.Sin(a)*v-50, life, pixelfx.White, 1, delay, 0)
	}
	// shards: bright splinters that tumble down, bounce and settle
	for range 10 {
		a := A + R.Range(-1, 1)*1.1 - 0.35
		v := R.Range(110, 280)
		var b pixelfx.Bit
		b.Kind = pixelfx.Shard
		b.X, b.Y = X, Y
		b.VX, b.VY = math.Cos(a)*v, math.Sin(a)*v-40
		b.G, b.Drag, b.Bounce = 0.8, 0.7, 0.35
		b.Life = R.Range(0.55, 0.85)
		b.A = R.Range(0, pixelfx.Tau)
		b.VA = R.Range(-25, 25)
		b.S = R.Range(2, 4.5)
		b.C = pixelfx.Pick(R, []uint32{C[7], C[6], C[6], C[5]})
		bits.Add(b)
	}
	// embers of light drifting up and off, and glints that pop round the ring after the hit
	for range 12 {
		a := A - pi/2 + R.Range(-1.4, 1.4)
		v := R.Range(35, 110)
		life := R.Range(0.35, 0.7)
		c := pixelfx.Pick(R, []uint32{C[6], C[5], C[5]})
		ps.Spawn(pixelfx.Mote, X+math.Cos(a)*4, Y+math.Sin(a)*4, math.Cos(a)*v, math.Sin(a)*v-15, life, c, 0.6, R.Range(0, 0.05), 0)
	}
	for range 5 {
		a, d := R.Range(0, pixelfx.Tau), R.Range(14, 34)
		life := R.Range(0.22, 0.38)
		c := pixelfx.White
		if R.Chance(0.5) {
			c = C[6]
		}
		size := R.Range(1.6, 3)
		ps.Spawn(pixelfx.StarKind, X+math.Cos(a)*d, Y+math.Sin(a)*d*0.8, 0, -4, life, c, size, R.Range(0.08, 0.32), 0)
	}

	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			ps.Update(dt)
			bits.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the ring: it swells off the star, then breaks into arcs that fly on and cool
			if u := pixelfx.Clamp((t - 0.03) / 0.37); t >= 0.03 && u < 1 {
				r, brk, th := 10+30*pixelfx.Ease.OutCubic(u), pixelfx.Smooth(0.12, 0.6, u), math.Max(1, 3.6*math.Pow(1-u, 1.2))
				var i int
				switch {
				case u < 0.12:
					i = 7
				case u < 0.3:
					i = 6
				case u < 0.5:
					i = 5
				case u < 0.75:
					i = 4
				default:
					i = 3
				}
				k := 1.0
				if u >= 0.6 {
					k = (1 - u) / 0.4
				}
				brokenRing(p, X, Y, r, th, segs, brk, C[i], C[max(2, i-2)], k, 1, 0)
				if u < 0.6 {
					p.GlowRing(X, Y, r, 6, C[5], 0.35*math.Pow(1-u/0.6, 2))
				}
			}
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerBoth)
			// the flare blooms as the star collapses
			if f := (t - 0.04) / 0.28; f > 0 && f < 1 {
				critFlare(p, X, Y, 64*math.Pow(1-f, 1.3), 1-f, C)
				p.Glow(X, Y, 56, C[5], 0.4*(1-f))
			}
			// the starburst slams in: a white frame, an overshoot, a settle, then it breaks up
			for i := 0; i < len(critKeys)-1; i++ {
				if key := critKeys[i]; t >= key.t && t < critKeys[i+1].t {
					star(p, X, Y, sp, key.sc, key.th, [4]uint32{C[key.cols[0]], C[key.cols[1]], C[key.cols[2]], C[key.cols[3]]}, key.a, 2.6)
				}
			}
			if pop := math.Exp(-t / 0.05); pop > 0.02 {
				p.Glow(X, Y, 44, C[5], 0.4*pop)
				p.Glow(X, Y, 12, pixelfx.White, 0.6*pop)
			}
			lights.Draw(p)
		},
	}
}
