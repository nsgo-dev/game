// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/leaf-storm.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the leaves' tumble (turning as they go, flipping over through edge-on, palettes of autumn and green) from Sprite Fusion's open-source falling leaves (https://www.spritefusion.com/pixel-art-vfx/falling-leaves), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var leafStormUp = pixelfx.Pose{}.With(
	pixelfx.Turn.Is(1.1), pixelfx.Crouch.Is(0),
	pixelfx.FX.Is(-13), pixelfx.FY.Is(-37),
	pixelfx.BX.Is(13), pixelfx.BY.Is(-37),
	pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4),
)

// LeafStorm is a rising spiral of tumbling leaves and wind bands around a character, with swirling litter at their feet. It is registered as "nature/leaf-storm".
var LeafStorm = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Leaf storm",
		Desc:     "Leaves tumble up a rising spiral round the character, passing behind and in front, turning and flipping, wrapped in bright bands of wind; litter swirls at their feet.",
		Tags:     []string{"nature", "wind", "leaves", "buff", "aura", "actor", "loop"},
		Size:     [2]int{112, 104},
		Origin:   [2]int{56, 92},
		Duration: leafStormDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a2420", "#2e4038", "#4a6656", "#7a9a84", "#b4d0b8", "#eef8e8")},
			{Name: "leaves", Default: pixelfx.Palette("#123818", "#2f7a2a", "#6cb83a", "#c8f070", "#4a2a08", "#a8701c", "#e8b42e", "#fff09a", "#3a1010", "#9a2e1c", "#e0642a", "#ffb070")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		Poses: []pixelfx.Key{
			{T: 0, Pose: leafStormUp},
			{T: 0.6, Pose: leafStormUp.With(pixelfx.Lean.Is(2), pixelfx.FY.Is(-40), pixelfx.BY.Is(-33))},
			{T: 1.2, Pose: leafStormUp.With(pixelfx.Crouch.Is(-1))},
			{T: 1.8, Pose: leafStormUp.With(pixelfx.Lean.Is(-2), pixelfx.FY.Is(-33), pixelfx.BY.Is(-40))},
			{T: 2.4, Pose: leafStormUp},
		},
	},
	New: newLeafStorm,
}

func init() { pixelfx.Register("nature/leaf-storm", LeafStorm) }

const (
	leafStormDuration = 2.4
	leafStormSY       = 0.3
	leafStormTop      = 64.0
)

func leafStormRingR(y float64) float64 { return 15 + 9*pixelfx.Clamp(-y/leafStormTop) }

// leafStormLeaves draws leaves riding the spiral: up from the floor, round two turns or so, flung out off the top.
func leafStormLeaves(p *pixelfx.Painter, L []uint32, t, seed float64, back bool) {
	pixelfx.Cycle(t, leafStormDuration, 28, seed, func(g *pixelfx.Rng) float64 { return g.Range(1.5, 2.3) }, func(age, life float64, R *pixelfx.Rng, _ int) {
		k := age / life
		turns := R.Range(1.4, 2.2)
		th := R.Range(0, tau) + turns*tau*k
		sn := math.Sin(th)
		if (sn > 0) == back {
			return
		}
		y := -pixelfx.Lerp(1, leafStormTop, math.Pow(k, 0.9)) - R.Range(0, 6)
		fling := pixelfx.Smooth(0.75, 1, k)
		rr := leafStormRingR(y)*R.Range(0.85, 1.15) + fling*22
		x, yy := math.Cos(th)*rr, y+sn*rr*leafStormSY
		a := math.Min(1, k*7) * (1 - pixelfx.Smooth(0.85, 1, k))
		spin := R.Range(-9, 9)*age + th
		fl := age * R.Range(5, 11)
		o := int(R.Int(0, 2)) * 4
		pal := [4]uint32{L[o], L[o+1], L[o+2], L[o+3]}
		if back {
			pal = [4]uint32{L[o], L[o], L[o+1], L[o+2]}
		}
		length := R.Range(6.5, 9)
		wid := R.Range(2.4, 3.3)
		drawLeaf(p, x, yy, length, wid, spin, fl, pal, a)
		// a glint as it turns face on to the light
		if !back && math.Abs(math.Cos(fl)) > 0.97 {
			p.Px(x, yy, pixelfx.White)
			p.Glow(x, yy, 5, L[o+3], 0.35*a)
		}
	})
}

// leafStormWind draws wind bands: 1 px streaks on tilted rings round the character, bright at the head, fading behind, the far halves dimmer and behind it.
func leafStormWind(p *pixelfx.Painter, C []uint32, t, seed float64, back bool) {
	for j := 0; j < 16; j++ {
		R := pixelfx.RngOf(seed, float64(j))
		y0 := -R.Range(4, leafStormTop-4)
		r := leafStormRingR(y0) * R.Range(1.0, 1.25)
		K := R.Int(2, 3)
		span := R.Range(1.1, 2.4)
		head := R.Range(0, tau) + (tau*K*t)/leafStormDuration
		rise := R.Range(-0.18, 0.18)
		for s := 0.0; s <= span; s += 0.45 / r {
			th := head - s
			sn := math.Sin(th)
			if (sn > 0) == back {
				continue
			}
			k := s / span
			x := math.Cos(th) * r
			y := y0 + sn*r*leafStormSY + (th-head)*rise*4
			var c uint32
			if back {
				if k < 0.3 {
					c = C[3]
				} else {
					c = C[2]
				}
			} else if k < 0.06 && R.Next() < 2 {
				c = pixelfx.White
			} else if k < 0.15 {
				c = C[5]
			} else if k < 0.4 {
				c = C[4]
			} else {
				c = C[3]
			}
			if k < 0.45 {
				p.Dot(x, y, c, 1)
			} else {
				p.Blend(x, y, c, 1.6*(1-k))
			}
		}
		if !back && math.Sin(head) > 0 {
			p.Glow(math.Cos(head)*r, y0+math.Sin(head)*r*leafStormSY, 5, C[4], 0.25)
		}
	}
}

// leafStormSwirl draws litter and dust swept round on the floor at the feet.
func leafStormSwirl(p *pixelfx.Painter, C []uint32, t, seed float64, back bool) {
	pixelfx.Cycle(t, leafStormDuration, 14, seed, func(g *pixelfx.Rng) float64 { return g.Range(0.6, 1.1) }, func(age, life float64, R *pixelfx.Rng, _ int) {
		k := age / life
		th := R.Range(0, tau) + R.Range(6, 9)*age
		sn := math.Sin(th)
		if (sn > 0) == back {
			return
		}
		rr := R.Range(10, 22) + k*8
		x := math.Cos(th) * rr
		y := -1 - k*R.Range(2, 8) + sn*rr*0.25
		s := R.Range(2, 3.5) * (0.6 + k)
		col := C[3]
		if back {
			col = C[2]
		}
		p.Disc(x, y, s, col, (1-k)*math.Min(1, k*6)*0.3)
	})
	for j := 0; j < 6; j++ {
		R := pixelfx.RngOf(seed+9, float64(j))
		rr := R.Range(12, 24)
		th0 := R.Range(0, tau) + (tau*3*t)/leafStormDuration
		span := R.Range(0.5, 1)
		for s := 0.0; s < span; s += 0.6 / rr {
			th := th0 - s
			if (math.Sin(th) > 0) == back {
				continue
			}
			col := C[2]
			if s < span*0.4 {
				col = C[3]
			}
			p.Dot(math.Cos(th)*rr, 0.5+rr*0.2*math.Sin(th), col, 1-s/span)
		}
	}
}

func newLeafStorm(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 6)
	L := pixelfx.Fit(o.Ramp("leaves"), 12)
	seed := o.Seed()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			p.Glow(0, -30, 46, C[4], 0.12)
			p.Glow(0, -2, 26, L[2], 0.12)
			leafStormSwirl(p, C, t, seed*3+1, true)
			leafStormWind(p, C, t, seed*5+2, true)
			leafStormLeaves(p, L, t, seed*7+3, true)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			leafStormSwirl(p, C, t, seed*3+1, false)
			leafStormWind(p, C, t, seed*5+2, false)
			leafStormLeaves(p, L, t, seed*7+3, false)
		},
	}
}
