// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/feather-burst.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// FeatherBurst is a white puff that pops and throws feathers that brake hard, then rock from side to side as they sink and fade, with down drifting after them. It is registered as "holy/feather-burst".
var FeatherBurst = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Feather burst",
		Desc:     "A white puff pops and throws feathers that brake hard, then rock from side to side as they sink and fade, with down drifting after them.",
		Tags:     []string{"holy", "feather", "burst", "poof", "angel", "bird", "one-shot"},
		Size:     [2]int{160, 160},
		Origin:   [2]int{80, 60},
		Duration: 3,
		Loop:     false,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a4560", "#7a7494", "#aaa5c2", "#d6d3e6", "#f4f3fa", "#ffffff")},
			{Name: "glow", Default: pixelfx.Color("#fff0c0")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newFeatherBurst,
}

func init() { pixelfx.Register("holy/feather-burst", FeatherBurst) }

type featherLobe struct {
	x, y, r0, r1, life float64
}

func newFeatherBurst(o *pixelfx.Options) pixelfx.Instance {
	F, G, R := pixelfx.Fit(o.Palette(), 6), o.Color("glow"), pixelfx.NewRng(o.Seed())
	W := pixelfx.White
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return math.Inf(1) }, Sparks: []uint32{W, F[4], F[3], F[2]}})
	lights, loose := &pixelfx.Lights{}, newFeathers(R, 1e6)
	// the puff: soft lobes thrown out from the pop, swelling and thinning
	lobes := make([]featherLobe, 11)
	for i := range lobes {
		a := (float64(i)/11)*tau + R.Range(-0.25, 0.25)
		d := R.Range(5, 14)
		r0, r1, life := R.Range(2.5, 4), R.Range(6, 9.5), R.Range(0.45, 0.8)
		lobes[i] = featherLobe{x: math.Cos(a) * d, y: math.Sin(a) * d * 0.8, r0: r0, r1: r1, life: life}
	}
	for i := range 20 {
		a := (float64(i)/20)*tau + R.Range(-0.15, 0.15)
		sp := R.Range(110, 230)
		up := 0.9
		if math.Sin(a) < 0 {
			up = 0.7
		}
		fa, fl, fw, life, delay, sink, amp := a+R.Range(-0.5, 0.5), R.Range(10, 14), R.Range(2.6, 3.4), R.Range(1.9, 2.6), R.Range(0, 0.03), R.Range(26, 38), R.Range(8, 13)
		loose.add(looseFeather{x: math.Cos(a) * 3, y: math.Sin(a) * 3, vx: math.Cos(a) * sp, vy: math.Sin(a)*sp*up - 20, a: fa, len: fl, wid: fw, life: life, delay: delay, sink: sink, amp: amp})
	}
	// small feathers and down, slower
	for range 10 {
		a, sp := R.Range(0, tau), R.Range(50, 130)
		fa, fl, fw, life, sink := R.Range(0, tau), R.Range(6, 8), R.Range(1.8, 2.2), R.Range(1.6, 2.4), R.Range(18, 28)
		loose.add(looseFeather{x: 0, y: 0, vx: math.Cos(a) * sp, vy: math.Sin(a)*sp - 20, a: fa, len: fl, wid: fw, life: life, back: true, sink: sink})
	}
	for range 26 {
		a, sp := R.Range(0, tau), R.Range(30, 140)
		x, y := R.Range(-3, 3), R.Range(-3, 3)
		life, col, size := R.Range(1.4, 2.6), chance(R, 0.6, W, F[4]), R.Range(0.6, 1.6)
		ps.Spawn(pixelfx.Flake, x, y, math.Cos(a)*sp, math.Sin(a)*sp-15, life, col, size, 0, 0)
	}
	for range 10 {
		a, sp := R.Range(0, tau), R.Range(60, 160)
		life, col, size := R.Range(0.3, 0.6), chance(R, 0.5, W, G), R.Range(0.8, 2.2)
		ps.Spawn(pixelfx.StarKind, 0, 0, math.Cos(a)*sp, math.Sin(a)*sp, life, col, size, 0, 0)
	}
	lights.Add(0, 0, 50, G, 0.75, 0.45)
	lights.Add(0, 0, 20, W, 0.7, 0.1)
	t := 0.0

	puff := func(p *pixelfx.Painter) {
		for _, l := range lobes {
			u := t / l.life
			if u >= 1 {
				continue
			}
			e := pixelfx.Ease.OutCubic(u)
			x, y := l.x*(0.4+e*1.3), l.y*(0.4+e*1.3)-u*5
			r := (l.r0 + (l.r1-l.r0)*pixelfx.Ease.OutCubic(math.Min(1, u*3))) * (1 - pixelfx.Ease.InQuad(u))
			if r < 0.6 {
				continue
			}
			p.Circle(x, y, r+0.8, F[2], 1)
			p.CircleFn(x, y, r, func(d, a float64) (uint32, float64) {
				switch {
				case d > 0.55 && math.Sin(a+0.8) > 0.3:
					return F[3], 1
				case d < 0.45 && math.Sin(a+0.8) < -0.2:
					return W, 1
				}
				return F[4], 1
			})
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// feathers catch the light as they turn
			if t > 0.3 && t < 2.2 && loose.count() > 0 && R.Chance(0.14) {
				q, _ := pick(R, loose.list)
				if q.delay <= 0 {
					x, y := q.x+R.Range(-2, 2), q.y+R.Range(-1, 1)
					life, col, size := R.Range(0.2, 0.4), chance(R, 0.5, W, G), R.Range(1, 2.2)
					ps.Spawn(pixelfx.StarKind, x, y, 0, 0, life, col, size, 0, 0)
				}
			}
			ps.Update(dt)
			loose.update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			loose.draw(p, F, pixelfx.LayerBack)
			ps.Draw(p, pixelfx.LayerBack)
			puff(p)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the pop: a white flash and a ring of air
			if t < 0.07 {
				p.Circle(0, 0, 9*(1-t/0.14), W, 1)
			}
			if t < 0.25 {
				u := t / 0.25
				rim := pixelfx.Mix(F[4], G, 0.4)
				p.RingFn(0, 0, 6+24*pixelfx.Ease.OutCubic(u), math.Max(1, 2.5*(1-u)), func(v, _ float64) (uint32, float64) {
					if v > 0.5 {
						return W, 1.1 - u
					}
					return rim, 1.1 - u
				})
			}
			if t < 0.2 {
				pixelfx.Sparkle(p, 0, 0, 10*(1-t/0.2), W, G, 0.6)
			}
			// the burst's warmth lingers a moment
			p.Glow(0, 4, 46, G, 0.28*(1-pixelfx.Smooth(0, 1.3, t)))
			loose.draw(p, F, pixelfx.LayerFront)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
