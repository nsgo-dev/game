// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/charmed.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the shaded, outlined hearts from Sprite Fusion's open-source heal (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

const (
	charmedDuration = 2.4
	charmedBeat     = charmedDuration / 2
)

var (
	charmedSwoon = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.1), pixelfx.FX.Is(-2), pixelfx.FY.Is(-28), pixelfx.BX.Is(2), pixelfx.BY.Is(-28), pixelfx.ArmsFront.On(), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	heart5       = bitmap([]string{".#.#.", "#####", "#####", ".###.", "..#.."}, 1, false)
	heart7       = bitmap([]string{".##.##.", "#######", "#######", ".#####.", "..###..", "...#..."}, 1, false)
)

// Charmed is smitten: a big heart beats over the head, throwing a ring and a flush of light with every beat, little hearts float up, the eyes go pink and the cheeks blush. It is registered as "status/charmed".
var Charmed = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Charmed",
		Desc:     "Smitten: a big heart beats over the head, throwing a ring and a flush of light with every beat, little hearts float up, the eyes go pink and the cheeks blush.",
		Tags:     []string{"status", "charm", "love", "hearts", "debuff", "loop"},
		Size:     [2]int{60, 76},
		Origin:   [2]int{30, 68},
		Duration: charmedDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#3a0a1e", "#7a1238", "#c4245a", "#f04e86", "#ff8ab0", "#ffc8da", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: charmedSwoon.With(pixelfx.Lean.Is(-2))},
			{T: 0.6, Pose: charmedSwoon.With(pixelfx.Lean.Is(0), pixelfx.Crouch.Is(1))},
			{T: 1.2, Pose: charmedSwoon.With(pixelfx.Lean.Is(2))},
			{T: 1.8, Pose: charmedSwoon.With(pixelfx.Lean.Is(0), pixelfx.Crouch.Is(1))},
			{T: 2.4, Pose: charmedSwoon.With(pixelfx.Lean.Is(-2))},
		},
	},
	New: newCharmed,
}

func init() { pixelfx.Register("status/charmed", Charmed) }

// charmedThump is a heartbeat: a strong lub and a softer dub.
func charmedThump(ph float64) float64 {
	a := (ph - 0.04) / 0.07
	b := (ph - 0.26) / 0.07
	return math.Exp(-(a * a)) + 0.6*math.Exp(-(b*b))
}

func charmedHearts(p *pixelfx.Painter, C []uint32, a *pixelfx.Actor, t, seed float64, back bool) {
	pixelfx.Cycle(t, charmedDuration, 12, seed+4, func(r *pixelfx.Rng) float64 { return r.Range(1.3, 1.9) }, func(age, life float64, r *pixelfx.Rng, i int) {
		if (i%3 == 0) != back {
			return
		}
		u := age / life
		side := 1.0
		if r.Next() < 0.5 {
			side = -1
		}
		ph := r.Range(0, tau)
		big := r.Next() < 0.3
		x := side*r.Range(4, 15) + math.Sin(age*4+ph)*2.2
		y := a.Y + r.Range(8, 30) - age*r.Range(14, 22)
		k := math.Min(1, u*6)
		if u > 0.88 {
			q := (u - 0.88) / 0.12
			pixelfx.Sparkle(p, x, y, pixelfx.Round(2*(1-q)), C[5], C[4], 0.4*(1-q))
			return
		}
		R := [4]uint32{C[0], C[2], C[3], C[4]}
		if back {
			R = [4]uint32{C[0], C[1], C[2], C[3]}
		}
		h := heart5
		if big {
			h = heart7
		}
		icon(p, x, y, 4, h, R, k, nil)
		if !back {
			ox := 1.0
			if big {
				ox = 2
			}
			p.Px(x-ox, y-1, C[6])
		}
		gk := 0.22
		if back {
			gk = 0.12
		}
		p.Glow(x, y, 6, C[3], gk*k)
	})
}

func newCharmed(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	E := eyesOf(a, S)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := pixelfx.Wrap(t, charmedBeat)
			th := charmedThump(ph)
			p.Glow(0, a.Y-8, 26+8*th, C[3], 0.16+0.22*th)
			charmedHearts(p, C, a, t, seed, true)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := pixelfx.Wrap(t, charmedBeat)
			th := charmedThump(ph)
			for _, e := range E {
				for _, pt := range e.px {
					col := C[2]
					if pt[1] == e.y0 {
						col = C[3]
					}
					p.Px(a.X+float64(pt[0]), a.Y+float64(pt[1]), col)
				}
				p.Px(a.X+float64(e.x0), a.Y+float64(e.y0+1), C[6])
				blush := pixelfx.Mix(e.skin, C[3], 0.55)
				bx := e.x1
				if float64(e.x0) < float64(a.W)/2 {
					bx = e.x0 - 1
				}
				p.Px(a.X+float64(bx), a.Y+float64(e.y1+1), blush)
				p.Px(a.X+float64(bx+1), a.Y+float64(e.y1+1), blush)
			}
			charmedHearts(p, C, a, t, seed, false)
			hx, hy := 0.0, a.Y-10
			s := 5.6 * (1 + 0.24*th)
			for _, ring := range [2][2]float64{{0.04, 1}, {0.26, 0.6}} {
				t0, kk := ring[0], ring[1]
				u := (ph - t0) / 0.5
				if u < 0 || u > 1 {
					continue
				}
				S1 := 5.6 * (1.25 + 1.3*u)
				outer := shaped(heartShape, S1, 0)
				inner := shaped(heartShape, S1-1.3, 0)
				ext := int(math.Ceil(S1*1.2)) + 1
				col := C[4]
				if u < 0.3 {
					col = C[5]
				}
				for dy := -ext; dy <= ext; dy++ {
					for dx := -ext; dx <= ext; dx++ {
						if outer(dx, dy) != 0 && inner(dx, dy) == 0 {
							p.Dot(hx+float64(dx), hy+float64(dy), col, (1-u)*kk*1.3)
						}
					}
				}
			}
			icon(p, hx, hy, 9, shaped(heartShape, s, 0), [4]uint32{C[0], C[2], C[3], C[4]}, 1, nil)
			p.Px(hx-pixelfx.Round(s*0.55), hy-pixelfx.Round(s*0.45), C[6])
			p.Px(hx-pixelfx.Round(s*0.55)+1, hy-pixelfx.Round(s*0.45), C[5])
			p.Px(hx-pixelfx.Round(s*0.55), hy-pixelfx.Round(s*0.45)+1, C[5])
			p.Glow(hx, hy, 12+6*th, C[4], 0.3+0.4*th)
			if th > 0.5 {
				pixelfx.Sparkle(p, hx+s*0.9, hy-s*0.8, pixelfx.Round(3*th), C[5], C[4], 0.4*th)
			}
			pixelfx.Cycle(t, charmedDuration, 8, seed+2, func(r *pixelfx.Rng) float64 { return r.Range(0.3, 0.5) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				an := r.Range(0, tau)
				d := r.Range(8, 15)
				u := age / life
				pixelfx.Sparkle(p, hx+math.Cos(an)*d, hy+math.Sin(an)*d*0.8, pixelfx.Round(1.6*math.Sin(pi*u)), C[5], C[4], 0.3*math.Sin(pi*u))
			})
		},
	}
}
