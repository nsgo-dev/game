// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/wisp.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame tail, puffs and embers from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Wisp is a will-o'-the-wisp looping a figure-eight over the floor: a little soul-flame with a face, its flame tail licking up and streaming behind, shedding puffs and sparks. It is registered as "critters/wisp".
var Wisp = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Wisp",
		Desc:     "A will-o'-the-wisp loops a figure-eight over the floor: a little soul-flame with a face, its flame tail licking up and streaming behind, shedding puffs and sparks.",
		Tags:     []string{"critter", "wisp", "spirit", "flame", "magic", "flying", "loop"},
		Size:     [2]int{128, 104},
		Origin:   [2]int{64, 96},
		Duration: 3.2,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0a1f2e", "#0f3d52", "#14707e", "#1fa8a8", "#4fe0c8", "#a8fff0", "#ffffff")},
			{Name: "hover", Default: pixelfx.Num(36)},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newWisp,
}

func init() { pixelfx.Register("critters/wisp", Wisp) }

var (
	wispD, wispR = 3.2, 7.5
	// wispTails are the three tongues of its tail: offset, height, half-width and flicker seed.
	wispTails = [3][4]float64{{-3.5, 13, 2.5, 3}, {3.5, 14, 2.5, 7}, {0, 20, 3.2, 11}}
)

type wispState struct{ x, y, vx, vy float64 }

func wispAt(t, hover float64) wispState {
	a := (t / wispD) * tau
	return wispState{x: 34 * math.Sin(a), y: -hover - 10*math.Sin(2*a), vx: ((34 * tau) / wispD) * math.Cos(a), vy: ((-20 * tau) / wispD) * math.Cos(2*a)}
}

func newWisp(o *pixelfx.Options) pixelfx.Instance {
	C, hover, seed := pixelfx.Fit(o.Palette(), 7), o.Num("hover"), o.Seed()
	heat := []uint32{C[6], C[6], C[5], C[5], C[4], C[4], C[3], C[3], C[3]}
	sw := newSwarm()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, wispD, seed)
			S := wispAt(t, hover)
			near := pixelfx.Clamp(1 - (-S.y-24)/30)
			// the floor lit cold beneath it, brighter as it dips
			shadow(p, S.x, 0, 7, 1.2, C[0], 0.4)
			for x := -24.0; x <= 24; x += 6 {
				p.Glow(S.x+x, -1, 16, C[4], (0.08+0.1*near)*(1-math.Abs(x)/32))
			}
		},
		DrawFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, wispD, seed)
			S := wispAt(t, hover)
			lean, stretch, X, Y := pixelfx.ClampTo(-S.vx/80, -0.9, 0.9), pixelfx.ClampTo(S.vy/120, -0.3, 0.4), S.x, S.y
			// flame puffs shed along its path, cooling down the ramp as they drift up
			pixelfx.Cycle(t, wispD, 30, seed*7+1, func(r *pixelfx.Rng) float64 { return r.Range(0.3, 0.6) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				S0 := wispAt(pixelfx.Wrap(t-age, wispD), hover)
				x, y := pixelfx.Fly(S0.x+r.Range(-3, 3), S0.y-r.Range(0, 4), r.Range(-8, 8), -r.Range(6, 16), age, -40, 2.5)
				firePuff(p, x, y, 1-age/L, r.Range(1.2, 2.6), r.Next()*100, heat)
			})
			// the tail: three tongues off the top of its head, streaming back as it flies
			for li := 0; li < 4; li++ {
				for _, tl := range wispTails {
					dx, h, w, n := tl[0], tl[1], tl[2], tl[3]
					hh := (h + 0.25*h*lnoise(t, wispD, 9, n, 5)) * (1 + stretch)
					tongue(p, X+dx, Y-2, hh, w, lnoise(t, wispD, 6, n, 40)*1.5+lean*hh*0.9, t, wispD, n, heat, li)
				}
			}
			// the head: a ball of cold fire, white at the heart, its rim flickering
			R := wispR
			p.Shade(X-R-2, Y-R-2, X+R+2, Y+R+2, func(x, y float64, _ *pixelfx.Out) uint32 {
				dx, dy := x-X, y-Y
				rr := R + 0.7*lnoise(t, wispD, 16, math.Atan2(dy, dx)*1.6+10, 3)
				if dy > 0 {
					rr += -0.6
				}
				d := pixelfx.Hypot(dx, dy) / rr
				switch {
				case d > 1:
					return 0
				case d < 0.42:
					return C[6]
				case d < 0.68:
					return C[5]
				case d < 0.88:
					return C[4]
				}
				return C[3]
			})
			// its face: eyes looking where it's going, a blink, a little mouth
			look, w := pixelfx.Round(pixelfx.ClampTo(S.vx/60, -1, 1)), pixelfx.Wrap(t, wispD)
			blink := w > 2.2 && w < 2.28
			rx, ry := pixelfx.Round(X), pixelfx.Round(Y)
			for _, s := range [2]float64{-1, 1} {
				ex := rx + s*2 - 1 + look
				if s > 0 {
					ex = rx + s*2 + look
				}
				ey := ry - 1
				if blink {
					p.Px(ex, ey+1, C[1])
					p.Px(ex+1, ey+1, C[1])
					continue
				}
				for k := 0.0; k < 3; k++ {
					p.Px(ex, ey+k, C[1])
					p.Px(ex+1, ey+k, C[1])
				}
				if look > 0 {
					p.Px(ex+1, ey, C[6])
				} else {
					p.Px(ex, ey, C[6])
				}
			}
			p.Px(rx+look, ry+3, C[2])
			p.Px(rx-1+look, ry+3, C[2])
			// sparks wander up off it
			sw.begin(t)
			sw.ps.Sparks = []uint32{C[6], C[5], C[4], C[3]}
			pixelfx.Cycle(t, wispD, 26, seed*7+2, func(r *pixelfx.Rng) float64 { return r.Range(0.8, 1.6) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				S0, s := wispAt(pixelfx.Wrap(t-age, wispD), hover), r.Next()*100
				x, y := pixelfx.Fly(S0.x+r.Range(-5, 5), S0.y-r.Range(4, 12), r.Range(-20, 20), -r.Range(20, 45), age, -15, 1.6)
				size := 1.0
				if r.Chance(0.3) {
					size = 2
				}
				sw.add(pixelfx.Ember, x+math.Sin(age*2.3+s)*3*math.Min(1, age), y, 0, 0, age, L, C[4], size, 0, s)
			})
			sw.draw(p)
			// light: a cold glow round it, a hot heart
			fl := 0.85 + 0.15*lnoise(t, wispD, 22, 0, 9)
			p.Glow(X+lean*3, Y-6, 46*fl, C[4], 0.38)
			p.Glow(X, Y-1, 13, C[5], 0.35*fl)
		},
	}
}
