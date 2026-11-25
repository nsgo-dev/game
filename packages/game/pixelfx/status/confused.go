// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/confused.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the shaded, outlined question marks from Sprite Fusion's open-source heal (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

const confusedDuration = 2.4

var (
	confusedShrug = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.FX.Is(-13), pixelfx.FY.Is(-26), pixelfx.BX.Is(14), pixelfx.BY.Is(-26), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	confusedQS    = [3]func(dx, dy int) byte{
		bitmap([]string{".###.", "#...#", "....#", "...#.", "..#..", ".....", "..#.."}, 1, false),
		bitmap([]string{".####.", "##..##", "....##", "...##.", "..##..", "..##..", "......", "..##..", "..##.."}, 1, false),
		bitmap([]string{".#####.", "##...##", "##...##", ".....##", "....##.", "...##..", "...##..", ".......", "...##..", "...##.."}, 1, false),
	}
	confusedMarks = [3]struct {
		x, y, t0 float64
		big      float64
	}{
		{-12, -5, 0, 2},
		{12, -8, 0.8, 1},
		{1, -17, 1.6, 2},
	}
)

// Confused is head in a spin: a glowing spiral whirls over the head, question marks pop up round it and bob, the eyes dart left and right. It is registered as "status/confused".
var Confused = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Confused",
		Desc:     "Head in a spin: a glowing spiral whirls over the head, question marks pop up round it and bob, the eyes dart left and right.",
		Tags:     []string{"status", "confused", "confusion", "dizzy", "debuff", "loop"},
		Size:     [2]int{60, 78},
		Origin:   [2]int{30, 70},
		Duration: confusedDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1e0f2e", "#4b2070", "#8a3ab0", "#c66ae0", "#f0a8ff", "#ffffff")},
			{Name: "marks", Default: pixelfx.Palette("#2a1608", "#b0701a", "#ffd84a", "#fff4b8")},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: confusedShrug.With(pixelfx.X.Is(0), pixelfx.Lean.Is(-2))},
			{T: 0.6, Pose: confusedShrug.With(pixelfx.X.Is(1), pixelfx.Lean.Is(1), pixelfx.Crouch.Is(1), pixelfx.FY.Is(-23), pixelfx.BY.Is(-29), pixelfx.FBX.Is(5), pixelfx.FBY.Is(-6))},
			{T: 1.2, Pose: confusedShrug.With(pixelfx.X.Is(2), pixelfx.Lean.Is(2))},
			{T: 1.8, Pose: confusedShrug.With(pixelfx.X.Is(1), pixelfx.Lean.Is(-1), pixelfx.Crouch.Is(1), pixelfx.FY.Is(-29), pixelfx.BY.Is(-23), pixelfx.FFX.Is(-4), pixelfx.FFY.Is(-6))},
			{T: 2.4, Pose: confusedShrug.With(pixelfx.X.Is(0), pixelfx.Lean.Is(-2))},
		},
	},
	New: newConfused,
}

func init() { pixelfx.Register("status/confused", Confused) }

func confusedSpiral(p *pixelfx.Painter, C []uint32, cx, cy, t float64) {
	rot := (t / confusedDuration) * tau * 2
	turns := 1.8
	n := 72
	breathe := 1 + 0.08*math.Sin((t/confusedDuration)*tau*4)
	at := func(u float64) (float64, float64) {
		th := u*turns*tau - rot
		r := (0.8 + 6.6*u) * breathe
		return cx + math.Cos(th)*r, cy + math.Sin(th)*r*0.9
	}
	for i := 1; i <= n; i++ {
		u := float64(i) / float64(n)
		x0, y0 := at(float64(i-1) / float64(n))
		x1, y1 := at(u)
		if u > 0.45 {
			p.Line(x0+0.5, y0+0.7, x1+0.5, y1+0.7, C[1], 1)
		}
	}
	for i := 1; i <= n; i++ {
		u := float64(i) / float64(n)
		x0, y0 := at(float64(i-1) / float64(n))
		x1, y1 := at(u)
		col := C[2]
		if u > 0.88 {
			col = C[5]
		} else if u > 0.62 {
			col = C[4]
		} else if u > 0.3 {
			col = C[3]
		}
		p.Line(x0, y0, x1, y1, col, 1)
		if i%6 == 0 {
			p.Light(x1, y1, C[3], 0.4*u)
		}
	}
	hx, hy := at(1)
	pixelfx.Sparkle(p, hx, hy, 2, C[4], C[3], 0.5)
	p.Glow(cx, cy, 13, C[3], 0.28)
}

func newConfused(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 6)
	M := pixelfx.Fit(o.Ramp("marks"), 4)
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	E := eyesOf(a, S)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			p.Glow(0, a.Y-7, 16, C[3], 0.22)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			look := -1
			if int(math.Floor((t/confusedDuration)*6))%2 != 0 {
				look = 1
			}
			for _, e := range E {
				for _, pt := range e.px {
					p.Px(a.X+float64(pt[0]), a.Y+float64(pt[1]), 0xff17121f)
				}
				ex := e.x1
				if look < 0 {
					ex = e.x0
				}
				p.Px(a.X+float64(ex), a.Y+float64(e.y0), 0xffffffff)
			}
			confusedSpiral(p, C, 0, a.Y-7, t)
			for _, m := range confusedMarks {
				age := pixelfx.Wrap(t-m.t0, confusedDuration)
				life := 1.9
				if age > life {
					continue
				}
				u := age / life
				pop := pixelfx.OutBack(pixelfx.Clamp(age/0.16), 3)
				out := pixelfx.Clamp((u - 0.82) / 0.18)
				bump := 0.0
				if pop > 1.1 {
					bump = 1
				}
				size := int(math.Max(0, pixelfx.Round(m.big*pop*(1-out)+bump)))
				bob := math.Sin(age*5+m.t0*3) * 1.6
				tilt := math.Sin(age*3.3+m.t0) * 0.8
				if pop < 0.3 || (out > 0 && size == 0 && out > 0.6) {
					continue
				}
				x := m.x + tilt
				y := a.Y + m.y + bob - age*2
				if age < 0.2 {
					k := age / 0.2
					for j := 0; j < 8; j++ {
						an := (float64(j)/8)*tau + 0.2
						d0 := 4 + 7*k
						d1 := d0 + 3*(1-k)
						col := M[3]
						if j%2 != 0 {
							col = M[2]
						}
						p.Line(x+math.Cos(an)*d0, y+math.Sin(an)*d0, x+math.Cos(an)*d1, y+math.Sin(an)*d1, col, 1-k)
					}
					p.Glow(x, y, 16, M[2], 0.45*(1-k))
				}
				icon(p, x, y, 7, confusedQS[min(2, size)], [4]uint32{M[0], M[1], M[2], M[3]}, 1, nil)
				p.Glow(x, y, 10+float64(size)*2, M[2], 0.28*(1-out))
			}
			pixelfx.Cycle(t, confusedDuration, 10, seed+2, func(r *pixelfx.Rng) float64 { return r.Range(0.35, 0.6) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				th := r.Range(0, tau)
				u := age / life
				d := 6 + age*r.Range(12, 22)
				x := math.Cos(th) * d
				y := a.Y - 7 + math.Sin(th)*d*0.8
				if math.Sin(pi*u) > 0.6 && r.Next() < 0.5 {
					pixelfx.Sparkle(p, x, y, 1, C[4], C[3], 0.3)
				} else {
					col := C[4]
					if u < 0.5 {
						col = C[5]
					}
					p.Dot(x, y, col, 1.2-u)
				}
			})
		},
	}
}
