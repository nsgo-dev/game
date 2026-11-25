// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/brazier.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the tongue, lick, flame-puff, smoke, ember and spark recipe from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Brazier is an iron bowl on a tripod heaped with glowing coals and a big roaring fire, sparks and embers streaming up off it, light on the iron and the floor. It is registered as "fire/brazier".
var Brazier = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Brazier",
		Desc:     "An iron bowl on a tripod heaped with glowing coals and a big roaring fire, sparks and embers streaming up off it, light on the iron and the floor.",
		Tags:     []string{"fire", "brazier", "prop", "light", "sparks", "ambient", "loop"},
		Size:     [2]int{112, 148},
		Origin:   [2]int{56, 136},
		Duration: brazierDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "iron", Default: pixelfx.Palette("#0b0910", "#1c1924", "#302c3a", "#4c4658", "#78708a")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newBrazier,
}

func init() { pixelfx.Register("fire/brazier", Brazier) }

const (
	brazierDuration = 3
	brazierRim      = -25
	brazierRX       = 15
	brazierRY       = 3
)

// brazierTongues are the fire: x, y from the rim, height share, half-width, back row.
var brazierTongues = []tongue{
	{-7, -1.5, 0.5, 2.0, true},
	{7.2, -1.5, 0.55, 2.0, true},
	{-3, -2.5, 0.84, 2.2, true},
	{3.3, -2.5, 0.86, 2.2, true},
	{-11.5, 0.5, 0.2, 1.5, false},
	{11.5, 0.5, 0.22, 1.5, false},
	{-8.3, 0, 0.34, 2.0, false},
	{8.5, 0, 0.38, 2.0, false},
	{-4.7, -0.5, 0.6, 2.8, false},
	{4.9, -0.5, 0.64, 2.8, false},
	{0, -1, 1, 3.6, false},
}

// brazierStand draws the tripod: two legs splayed out and one coming at us, curled feet, a ring holding them together.
func brazierStand(p *pixelfx.Painter, I, H []uint32, fl float64) {
	leg := func(x0, y0, x1, y1 float64) {
		p.Line(x0, y0, x1, y1, I[0], 1)
		p.Line(x0+1, y0, x1+1, y1, I[2], 1)
		p.Line(x0+2, y0, x1+2, y1, I[0], 1)
	}
	// the ring: its far half behind the front leg
	p.EllipseFn(0, -9, 9, 1.8, func(d, _ float64) (uint32, float64) {
		if d > 0.6 {
			return I[2], 1
		}
		return 0, 1
	})
	leg(-6, -17, -12, -1)
	leg(4, -17, 10, -1)
	p.Rect(-14, -1, 5, 1, I[2], 1)
	p.Rect(-15, 0, 6, 1, I[0], 1)
	p.Px(-15, -1, I[3])
	p.Rect(9, -1, 5, 1, I[2], 1)
	p.Rect(9, 0, 6, 1, I[0], 1)
	p.Px(13, -1, I[3])
	p.Rect(-1, -16, 3, 15, I[0], 1)
	p.Rect(0, -16, 1, 15, pixelfx.Mix(I[3], H[4], 0.15*fl), 1)
	p.Rect(-2, -1, 5, 1, I[2], 1)
	p.Rect(-2, 0, 5, 1, I[0], 1)
	p.EllipseFn(0, -9, 9, 1.8, func(d, a float64) (uint32, float64) {
		if math.Sin(a) > 0 && d > 0.55 {
			if d > 0.85 {
				return I[0], 1
			}
			return I[3], 1
		}
		return 0, 1
	})
}

// brazierBowl draws the bowl's front half: a band of rivets under a rim lit by the fire, rounding down to a dark belly.
func brazierBowl(p *pixelfx.Painter, I, H []uint32, fl float64) {
	const rim, rx = brazierRim, brazierRX
	p.Shade(-rx-1, rim-1, rx+1, rim+9, func(x, y float64, _ *pixelfx.Out) uint32 {
		v := (y - rim) / 9
		hw := rx * math.Sqrt(math.Max(0, 1-v*v*0.9))
		if v > 0.5 {
			hw -= (v - 0.5) * 6
		}
		if math.Abs(x) > hw+0.5 || y < rim {
			return 0
		}
		if math.Abs(x) > hw-0.5 || v > 0.92 {
			return I[0]
		}
		if y < rim+1 {
			return pixelfx.Mix(I[3], H[4], 0.5*fl)
		}
		if y < rim+2 {
			return I[2]
		}
		if y < rim+3 {
			if math.Mod(pixelfx.Round(x), 4) == 0 {
				return pixelfx.Mix(I[4], H[4], 0.25*fl)
			}
			return I[0]
		}
		switch {
		case x < -rx*0.45:
			return I[2]
		case x < rx*0.25:
			return I[1]
		}
		return I[0]
	})
}

// brazierCoals draws the coals heaped in the bowl, showing over the rim, breathing in the heat.
func brazierCoals(p *pixelfx.Painter, I, H []uint32, t, S float64) {
	const rim, rx, D = brazierRim, brazierRX, brazierDuration
	p.Shade(-rx, rim-6, rx, rim+1, func(x, y float64, out *pixelfx.Out) uint32 {
		// a mound: highest in the middle, standing proud of the rim
		top := rim - 1 - 4*math.Max(0, 1-math.Pow(x/(rx-1), 2))
		if y < top || y > rim+0.5 || math.Abs(x) > rx-0.5 {
			return 0
		}
		lump, edge := pixelfx.Rand(math.Floor((x+math.Floor(y)*1.5)/3), math.Floor(y/2), S+20), y < top+1
		// the gaps between lumps stay dark; the lumps breathe, hottest deep in the heap
		if pixelfx.Rand(math.Floor(x), math.Floor(y), S+21) < 0.16 || (edge && lump < 0.3) {
			return I[0]
		}
		g := 0.5 + 0.5*math.Sin((t/D)*math.Pi*4+lump*40)
		if edge {
			g *= 0.7
		}
		out.K = 0.3 * g
		switch {
		case g > 0.72:
			return H[2]
		case g > 0.5:
			return H[3]
		case g > 0.3:
			return H[4]
		case g > 0.15:
			return H[5]
		}
		return H[6]
	})
}

type brazierSpark struct {
	x, y, vx, vy, a, w, m float64
	big                   bool
}

func newBrazier(o *pixelfx.Options) pixelfx.Instance {
	D := float64(brazierDuration)
	NL := noiseFor(D)
	F := pixelfx.Fit(o.Palette(), 9)
	H, I, S := hotFirst(F), pixelfx.Fit(o.Ramp("iron"), 5), o.Seed()
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		fl, GLOW := 0.74+0.16*NL(t, 7.3, S+1, 0)+0.1*NL(t, 19, S+2, 0), pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25)
		SPARKS, EMB := [4]uint32{H[1], H[2], H[4], H[5]}, [4]uint32{H[1], H[2], H[4], H[6]}
		floorLight(p, 0, 0.5, 40, 7, GLOW, 0.32*fl)
		brazierStand(p, I, H, fl)
		// the back of the rim, behind the fire
		p.EllipseFn(0, brazierRim, brazierRX, brazierRY, func(d, a float64) (uint32, float64) {
			if math.Sin(a) < 0 && d > 0.7 {
				if d > 0.88 {
					return I[0], 1
				}
				return pixelfx.Mix(I[3], H[4], 0.3*fl), 1
			}
			return 0, 1
		})
		burn(p, t, brazierTongues, burnOpts{
			H: H, D: D, NL: NL, S: S, X: 0, Y: brazierRim, Height: 32, Lean: 0.1, Spread: 7,
			Puffs: 8, Licks: 4, Smoke: 14, SmokeSize: 3, SmokeLife: [2]float64{1.6, 2.4}, Embers: 10, GlowR: 58, Glow: 1.4, Wind: 3,
			Mid: func(float64) {
				brazierCoals(p, I, H, t, S)
				brazierBowl(p, I, H, fl)
			},
		})
		// sparks streaming up in the draught: fast streaks that slow, wander and cool to embers high above
		looped(t, D, 60, S+600, func(r *pixelfx.Rng, _ int) (brazierSpark, float64) {
			side := r.Range(-1, 1)
			var q brazierSpark
			q.x = side * 8
			q.y = brazierRim - r.Range(4, 18)
			q.vx = side * r.Range(10, 40)
			q.vy = -r.Range(70, 150)
			life := r.Range(0.7, 1.7)
			q.a = r.Range(0, 6.3)
			q.w = r.Range(1.8, 3.2)
			q.m = r.Range(3, 8)
			q.big = r.Chance(0.12)
			return q, life
		}, func(q *brazierSpark, age, life float64, _ int) {
			m, size := q.m, 1.0
			if q.big {
				size = 2
			}
			x, y, vx, vy := drift(q.x, q.y, q.vx, q.vy, age, 0, -22, 1.9, 1.9)
			k := 1 - age/life
			// they bend and wander as the draught slows them
			wob := m * (math.Sin(age*q.w+q.a) - math.Sin(q.a)) * math.Min(1, age*1.5)
			wv := m * q.w * math.Cos(age*q.w+q.a) * math.Min(1, age*1.5)
			if pixelfx.Hypot(vx+wv, vy) > 40 {
				spark(p, x+wob, y, vx+wv, vy, k, SPARKS)
			} else {
				ember(p, x+wob, y, k, size, EMB, age*40+q.a)
			}
		})
	}}
}
