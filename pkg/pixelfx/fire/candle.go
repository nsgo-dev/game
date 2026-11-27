// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/candle.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the layered flame tongue from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Candle is a wax candle in a brass dish: a calm flame with a blue root that sways and stretches, a halo of light, motes drifting in it, a bead of wax running down a drip. It is registered as "fire/candle".
var Candle = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Candle",
		Desc:     "A wax candle in a brass dish: a calm flame with a blue root that sways and stretches, a halo of light, motes drifting in it, a bead of wax running down a drip.",
		Tags:     []string{"fire", "candle", "prop", "light", "calm", "ambient", "loop"},
		Size:     [2]int{56, 76},
		Origin:   [2]int{28, 64},
		Duration: candleDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "root", Default: pixelfx.Palette("#1b2a8a", "#3a62e0", "#7fb4ff")},
			{Name: "wax", Default: pixelfx.Palette("#2e201e", "#5e4436", "#8e6e58", "#b8957a", "#d8b896", "#f0dcc0")},
			{Name: "brass", Default: pixelfx.Palette("#2a1a10", "#6a4a1e", "#b4862e", "#e8c060", "#fff0b0")},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newCandle,
}

func init() { pixelfx.Register("fire/candle", Candle) }

const (
	candleDuration = 4
	candleTop      = -16
	candleWick     = candleTop - 3
	// the bead runs down this drip, from the rim to its bulb
	candleDripX  = 4
	candleDripY0 = candleTop + 2
	candleDripY1 = -7
)

// candleLayers are a calm flame: an orange rim round gold, yellow, a pale heart and a small white core low down.
var candleLayers = []layer{{1, 1, 5}, {0.8, 0.88, 3}, {0.6, 0.72, 2}, {0.38, 0.52, 1}, {0.18, 0.3, 0}}

// candleDish draws the brass dish.
func candleDish(p *pixelfx.Painter, B, H []uint32, fl float64) {
	p.EllipseFn(0, 1, 10, 2.6, func(d, a float64) (uint32, float64) {
		switch {
		case d > 0.82:
			return B[0], 1
		case math.Sin(a) < -0.2:
			if d > 0.6 {
				return pixelfx.Mix(B[3], H[3], 0.3*fl), 1
			}
			return B[2], 1
		case d > 0.55:
			return B[1], 1
		}
		return B[2], 1
	})
	p.Rect(-8, 2, 17, 1, B[1], 1)
	p.Rect(-7, 3, 15, 1, B[0], 1)
}

// candleBody draws the candle: a wax pillar with a dark outline, a highlight band on the lit side, a shadow side, a pool of melted wax lit by the flame, a long glossy drip down the right ending in a bulb and a short one on the left.
func candleBody(p *pixelfx.Painter, X, H []uint32, fl float64) {
	const L = candleTop
	cols := [9]uint32{X[1], X[4], X[5], X[4], X[4], X[3], X[3], X[2], X[0]}
	for i := range 9 {
		x := float64(i - 4)
		p.Rect(x, L+1, 1, -L-2, cols[i], 1)
		if i == 2 {
			p.Rect(x, -8, 1, 7, X[4], 1)
		}
	}
	p.Rect(-3, -1, 7, 1, X[2], 1)
	p.Px(-4, -1, X[0])
	p.Px(4, -1, X[0])
	// the rim and the pool of melted wax round the wick, lit by the flame
	p.Rect(-3, L, 7, 1, pixelfx.Mix(X[5], H[2], 0.25*fl), 1)
	p.Px(-4, L, X[1])
	p.Px(4, L, X[1])
	p.Rect(-2, L+1, 5, 1, pixelfx.Mix(X[5], H[1], 0.5*fl), 1)
	p.Rect(-1, L+1, 3, 1, pixelfx.Mix(X[4], H[2], 0.45*fl), 1)
	// the long drip: a lighter, glossy run down the right that bulges past the side into a bulb
	const y1 = candleDripY1
	p.Rect(3, L+1, 1, y1-L-1, X[4], 1)
	p.Rect(4, L+2, 1, y1-L-2, X[3], 1)
	p.Px(5, L+3, X[0])
	p.Rect(5, L+4, 1, y1-L-4, X[0], 1)
	p.Rect(3, y1, 2, 2, X[4], 1)
	p.Px(5, y1, X[3])
	p.Px(5, y1+1, X[2])
	p.Px(6, y1, X[0])
	p.Px(6, y1+1, X[0])
	p.Rect(3, y1+2, 3, 1, X[0], 1)
	p.Px(3, L+2, X[5])
	// the short drip on the left
	p.Rect(-4, L+1, 1, 4, X[4], 1)
	p.Px(-5, L+3, X[1])
	p.Px(-5, L+4, X[1])
	p.Px(-4, L+5, X[3])
}

type (
	candleSmoke struct{ vx, vy, s float64 }
	candleMote  struct{ x, y, vx, vy, ph float64 }
)

func newCandle(o *pixelfx.Options) pixelfx.Instance {
	D := float64(candleDuration)
	NL := noiseFor(D)
	F := pixelfx.Fit(o.Palette(), 9)
	H, RT, X, B, S := hotFirst(F), pixelfx.Fit(o.Ramp("root"), 3), pixelfx.Fit(o.Ramp("wax"), 6), pixelfx.Fit(o.Ramp("brass"), 5), o.Seed()
	GLOW := pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.3)
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		// calm: everything moves slowly, the flame now and then stretches tall
		N := func(rate, seed, off float64) float64 { return NL(t, rate*0.35, seed, off) }
		fl := 0.86 + 0.08*NL(t, 2, S+1, 0) + 0.06*NL(t, 7, S+2, 0)
		stretch := math.Max(0, NL(t, 0.75, S+3, 0)-0.3) * 0.6
		h := 12 * (1 + stretch) * (0.94 + 0.06*fl)
		sway := NL(t, 0.5, S+4, 0)*1.6 + NL(t, 1.5, S+5, 0)*0.5
		cy := candleWick - h*0.4

		// a pool of warm light on the floor and the halo behind the flame
		floorLight(p, 0, 1, 26, 5, GLOW, 0.3*fl)
		p.Glow(sway*0.3, cy, 38*fl, GLOW, 0.42*fl)
		p.GlowRing(sway*0.3, cy, 10, 4, H[2], 0.12*fl)
		candleDish(p, B, H, fl)
		candleBody(p, X, H, fl)
		// the bead of wax: it swells at the rim, runs down the drip and sets into its bulb
		run := math.Mod(t, D) / D
		swell, slide, set := pixelfx.Smooth(0, 0.3, run), pixelfx.Smooth(0.3, 0.85, run), pixelfx.Smooth(0.85, 1, run)
		if set < 1 && swell > 0.15 {
			by := pixelfx.Round(candleDripY0 + (candleDripY1-candleDripY0)*slide)
			p.Px(candleDripX, by, pixelfx.Mix(X[5], H[1], 0.4*fl))
			if swell > 0.6 && set < 0.5 {
				p.Px(candleDripX+1, by, X[4])
				p.Px(candleDripX+1, by+1, X[2])
			}
		}
		// the wick, glowing at its tip
		p.Line(0, candleTop, 0, candleWick+1, X[0], 1)
		p.Px(0, candleWick+1, H[4])
		// the flame and its blue root
		flameTongue(p, 0, candleWick+1, h, 2.6, sway, N, S*31, H, candleLayers, 0.25, 0.6)
		for dx := -2.0; dx <= 2; dx++ {
			a := 1 - math.Abs(dx)*0.2
			c := RT[1]
			if math.Abs(dx) == 2 {
				c = RT[0]
			}
			p.Dot(dx, candleWick+1, c, a)
			if math.Abs(dx) <= 1 {
				p.Dot(dx, candleWick+2, RT[0], 0.7)
			}
		}
		p.Px(0, candleWick, RT[2])
		// a thread of smoke now and then off the tip
		looped(t, D, 6, S+300, func(r *pixelfx.Rng, _ int) (candleSmoke, float64) {
			var q candleSmoke
			q.vx = r.Range(-1.5, 1.5)
			q.vy = -r.Range(8, 12)
			life := r.Range(1.4, 2.2)
			q.s = r.Range(0.8, 1.2)
			return q, life
		}, func(q *candleSmoke, age, life float64, _ int) {
			x, y, _, _ := drift(sway, candleWick-h, q.vx, q.vy, age, 1, -6, 1, 0.8)
			smokePuff(p, x+NL(t-age, 0.6, S+60, 0)*(1+age*2), y, 1-age/life, q.s, 0, false, 0.6)
		})
		// motes drifting in the candlelight, catching it as they pass close
		looped(t, D, 10, S+400, func(r *pixelfx.Rng, _ int) (candleMote, float64) {
			var q candleMote
			q.x = r.Range(-20, 20)
			q.y = r.Range(-44, -8)
			q.vx = r.Range(-3, 3)
			q.vy = r.Range(-4, 2)
			life := r.Range(2.5, 4)
			q.ph = r.Range(0, 6.3)
			return q, life
		}, func(q *candleMote, age, life float64, _ int) {
			x, y := q.x+q.vx*age+math.Sin(age*1.3+q.ph)*2, q.y+q.vy*age
			d := pixelfx.Hypot(x, y-cy)
			k := math.Min(math.Min(1, age*2), (life-age)*2) * pixelfx.Clamp(1.2-d/26)
			if k > 0.15 {
				c := H[3]
				if k > 0.6 {
					c = H[1]
				}
				p.Dot(x, y, c, k)
				p.Glow(x, y, 3, H[2], 0.2*k)
			}
		})
		// the hot core of the light
		p.Glow(sway*0.5, candleWick-h*0.3, 10, H[1], 0.35*fl)
	}}
}
