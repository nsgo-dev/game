// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/parry.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the star's key frames and the spark spray from Sprite Fusion's open-source impact (https://www.spritefusion.com/pixel-art-vfx/impact), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Parry is two blades clashing: their edges flash in an X, a white four-point star rings out, a ring breaks into arcs and a fan of gold sparks sprays up and rains down. It is registered as "impacts/parry".
var Parry = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Parry",
		Desc:     "Two blades clash: their edges flash in an X, a white four-point star rings out, a ring breaks into arcs and a fan of gold sparks sprays up and rains down.",
		Tags:     []string{"impact", "parry", "block", "clash", "sword", "metal", "sparks", "one-shot"},
		Size:     [2]int{200, 112},
		Origin:   [2]int{100, 96},
		Duration: 1,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1a08", "#6a4410", "#b07a1a", "#e8b030", "#ffe066", "#fff6c4", "#ffffff")},
			{Name: "height", Default: pixelfx.Num(30)},
			{Name: "seed", Default: pixelfx.Num(11)},
		},
	},
	New: newParry,
}

func init() { pixelfx.Register("impacts/parry", Parry) }

func newParry(o *pixelfx.Options) pixelfx.Instance {
	C, R, X, Y := pixelfx.Fit(o.Palette(), 7), pixelfx.NewRng(o.Seed()), 0.0, -o.Num("height")
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[4], C[4], C[3], C[2]}})
	lights := &pixelfx.Lights{}
	// a hard four-point star: long arms on the axes, short ones between
	sp := make([]spike, 8)
	for i := range sp {
		a := (float64(i)*pi)/4 + R.Range(-0.04, 0.04)
		var L float64
		switch {
		case i%2 != 0:
			L = R.Range(6, 8)
		case i%4 == 0:
			L = R.Range(19, 22)
		default:
			L = R.Range(14, 17)
		}
		w := 2.2
		if i%2 != 0 {
			w = 1.2
		}
		sp[i] = spike{a: a, len: L, w: w}
	}
	segs := make([]ringSeg, 6)
	for i := range segs {
		a := (float64(i)/6)*pixelfx.Tau + R.Range(-0.1, 0.1)
		gap := R.Range(0.55, 0.8)
		segs[i] = ringSeg{a: a, gap: gap, dr: R.Range(0.1, 0.35), w: 1}
	}
	// the two edges that met: lines crossing in an X
	e0 := -0.62 + R.Range(-0.08, 0.08)
	edges := [2]float64{e0, pi + 0.62 + R.Range(-0.08, 0.08)}

	lights.Add(X, Y, 60, C[4], 0.65, 0.4)
	lights.Add(X, Y, 22, pixelfx.White, 0.8, 0.08)
	// the fan: sparks thrown up and out to both sides, arcing over and raining down to bounce on the floor
	for i := range 24 {
		s := -1.0
		if i%2 != 0 {
			s = 1
		}
		a := -pi/2 + s*R.Range(0.3, 1.3)
		v := R.Range(130, 290)
		x := X + s*R.Range(1, 3)
		life := R.Range(0.45, 0.85)
		delay := 0.0
		if i > 17 {
			delay = R.Range(0.02, 0.07)
		}
		ps.Spawn(pixelfx.Spark, x, Y, math.Cos(a)*v, math.Sin(a)*v, life, pixelfx.White, 1, delay, 0)
	}
	for range 8 {
		a, v := R.Range(0, pixelfx.Tau), R.Range(40, 110)
		life := R.Range(0.3, 0.55)
		c := pixelfx.Pick(R, []uint32{C[5], C[6]})
		ps.Spawn(pixelfx.Mote, X, Y, math.Cos(a)*v, math.Sin(a)*v-20, life, c, 0.6, R.Range(0, 0.04), 0)
	}
	for range 3 {
		a, d := R.Range(0, pixelfx.Tau), R.Range(12, 26)
		life := R.Range(0.22, 0.34)
		size := R.Range(1.6, 2.6)
		ps.Spawn(pixelfx.StarKind, X+math.Cos(a)*d, Y+math.Sin(a)*d, 0, -3, life, pixelfx.White, size, R.Range(0.1, 0.3), 0)
	}

	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the ring: rings out off the star and breaks into arcs that fly on and cool
			if u := (t - 0.03) / 0.36; u >= 0 && u < 1 {
				r, brk := 8+24*pixelfx.Ease.OutCubic(u), pixelfx.Smooth(0.15, 0.6, u)
				var i int
				switch {
				case u < 0.15:
					i = 6
				case u < 0.35:
					i = 5
				case u < 0.6:
					i = 4
				default:
					i = 3
				}
				k := 1.0
				if u >= 0.6 {
					k = (1 - u) / 0.4
				}
				brokenRing(p, X, Y, r, math.Max(1, 2.6*math.Pow(1-u, 1.2)), segs, brk, C[i], C[max(2, i-2)], k, 1, 0)
				if u < 0.5 {
					p.GlowRing(X, Y, r, 5, C[4], 0.3*(1-u/0.5))
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			// the edges flash where they met
			if t < 0.058 {
				for _, a := range edges {
					ca, sa := math.Cos(a), math.Sin(a)
					L, k := 32.0, 0.6
					if t < 0.025 {
						L, k = 26, 1
					}
					for d := -L; d <= L; d += 0.5 {
						q := math.Abs(d) / L
						c := C[4]
						if q < 0.4 {
							c = pixelfx.White
						} else if q < 0.75 {
							c = C[5]
						}
						al := 1.0
						if q >= 0.6 {
							al = k * (1.3 - q)
						}
						p.Dot(X+ca*d, Y+sa*d, c, al)
					}
					p.GlowLine(X-ca*L, Y-sa*L, X+ca*L, Y+sa*L, 4, C[5], 0.35*k)
				}
			}
			// the star: white, then bigger and gold as it thins, then just a glint
			switch {
			case t < 0.025:
				star(p, X, Y, sp, 1, 1, [4]uint32{C[4], pixelfx.White, pixelfx.White, pixelfx.White}, 1, 2.6)
			case t < 0.058:
				star(p, X, Y, sp, 1.25, 0.65, [4]uint32{C[2], C[5], C[6], pixelfx.White}, 1, 2.6)
			case t < 0.092:
				star(p, X, Y, sp, 1.4, 0.3, [4]uint32{C[2], C[4], C[5], C[6]}, 1, 2)
			}
			if pop := math.Exp(-t / 0.05); pop > 0.02 {
				p.Glow(X, Y, 30, C[4], 0.5*pop)
				p.Glow(X, Y, 10, pixelfx.White, 0.4*pop)
			}
			lights.Draw(p)
		},
	}
}
