// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/sleep.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the shaded, outlined letters from Sprite Fusion's open-source heal (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

const (
	sleepDuration = 2.4
	sleepNZ       = 3
)

var (
	sleepNod = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Crouch.Is(1), pixelfx.Lean.Is(1), pixelfx.HY.Is(2), pixelfx.FX.Is(-6), pixelfx.FY.Is(-14), pixelfx.BX.Is(7), pixelfx.BY.Is(-14), pixelfx.FFX.Is(-2), pixelfx.FBX.Is(3))
	sleepZS  = [4]func(dx, dy int) byte{
		bitmap([]string{"#####", "...#.", "..#..", ".#...", "#####"}, 1, false),
		bitmap([]string{"#######", ".....#.", "....#..", "...#...", "..#....", ".#.....", "#######"}, 1, false),
		bitmap([]string{".#######.", "#########", "......##.", ".....##..", "....##...", "...##....", "..##.....", "#########", ".#######."}, 1, false),
		bitmap([]string{".#########.", "###########", ".......###.", "......###..", ".....###...", "....###....", "...###.....", "..###......", ".###.......", "###########", ".#########."}, 1, false),
	}
)

// Sleep is fast asleep: eyes shut, soft outlined Zs float up off the head one after another, swaying and growing as they rise, dream motes drift, a slow breathing glow. It is registered as "status/sleep".
var Sleep = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Sleep",
		Desc:     "Fast asleep: eyes shut, soft outlined Zs float up off the head one after another, swaying and growing as they rise, dream motes drift, a slow breathing glow.",
		Tags:     []string{"status", "sleep", "zzz", "rest", "debuff", "loop"},
		Size:     [2]int{60, 82},
		Origin:   [2]int{26, 74},
		Duration: sleepDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a1636", "#3c3a7a", "#6f78c8", "#a9b8f0", "#dfe6ff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: sleepNod},
			{T: 1.2, Pose: sleepNod.With(pixelfx.Crouch.Is(2), pixelfx.Lean.Is(2), pixelfx.HY.Is(3))},
			{T: 2.4, Pose: sleepNod},
		},
	},
	New: newSleep,
}

func init() { pixelfx.Register("status/sleep", Sleep) }

type zPos struct {
	x, y float64
	size int
	k    float64
}

func sleepZAt(a *pixelfx.Actor, i int, u float64) zPos {
	side := 0.6
	if i%2 != 0 {
		side = 1
	}
	x0 := a.X + float64(a.W)*0.78
	y0 := a.Y + 7
	x := x0 + 13*side*u + 2.5*math.Sin(tau*1.1*u+float64(i)*2.1)
	y := y0 - 32*(1-math.Pow(1-u, 1.35))
	size := 3
	if u < 0.18 {
		size = 0
	} else if u < 0.42 {
		size = 1
	} else if u < 0.68 {
		size = 2
	}
	return zPos{x: x, y: y, size: size, k: math.Min(1, u/0.06)}
}

func newSleep(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 6)
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	E := eyesOf(a, S)
	return &pixelfx.Funcs{
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			breath := 0.5 + 0.5*math.Sin((t/sleepDuration)*tau*2)
			for _, e := range E {
				for _, pt := range e.px {
					p.Px(a.X+float64(pt[0]), a.Y+float64(pt[1]), e.skin)
				}
				y := a.Y + float64(e.y1-1)
				for x := e.x0; x <= e.x1; x++ {
					p.Px(a.X+float64(x), y, 0xff1f1726)
				}
				p.Px(a.X+float64(e.x0-1), y-1, 0xff1f1726)
				p.Px(a.X+float64(e.x1+1), y-1, 0xff1f1726)
			}
			p.Glow(0, a.Y+10, 24, C[3], 0.1+0.1*breath)
			pixelfx.Cycle(t, sleepDuration, 5, seed+6, pixelfx.Fixed(sleepDuration), func(age, life float64, r *pixelfx.Rng, _ int) {
				u := age / life
				x := a.X + float64(a.W)*r.Range(0.3, 1.1) + math.Sin(u*tau+r.Next()*tau)*3
				y := a.Y + r.Range(-6, 8) - u*10
				rad := r.Range(3, 5) * (0.8 + 0.4*u)
				k := math.Sin(pi * u)
				p.Glow(x, y, rad*2.4, C[3], 0.1*k)
			})
			pixelfx.Cycle(t, sleepDuration, 14, seed+4, func(r *pixelfx.Rng) float64 { return r.Range(1.2, 2) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				u := age / life
				x := a.X + float64(a.W)*r.Range(0.4, 1.1) + math.Sin(age*2+r.Next()*tau)*2
				y := a.Y + r.Range(0, 10) - age*r.Range(10, 18)
				tw := math.Sin(pi*u) * (0.6 + 0.4*math.Sin(age*9+r.Next()*tau))
				if tw < 0.15 {
					return
				}
				arm := 0.0
				if tw > 0.7 && r.Next() < 0.4 {
					arm = 1
				}
				pixelfx.Sparkle(p, x, y, arm, C[4], C[3], 0.3*tw)
				col := C[4]
				if tw > 0.6 {
					col = C[5]
				}
				p.Dot(x, y, col, tw*1.3)
			})
			type zEntry struct {
				i int
				u float64
			}
			var zs [sleepNZ]zEntry
			for i := 0; i < sleepNZ; i++ {
				zs[i] = zEntry{i: i, u: pixelfx.Wrap(t-(float64(i)*sleepDuration)/sleepNZ, sleepDuration) / sleepDuration}
			}
			slices.SortStableFunc(zs[:], func(p1, p2 zEntry) int { return cmp.Compare(p2.u, p1.u) })
			for _, ze := range zs {
				z := sleepZAt(a, ze.i, ze.u)
				if ze.u > 0.9 {
					k := (ze.u - 0.9) / 0.1
					for j := 0; j < 7; j++ {
						an := (float64(j)/7)*tau + float64(ze.i)
						d := 3 + 6*math.Sqrt(k)
						col := C[5]
						if j%2 != 0 {
							col = C[4]
						}
						p.Dot(z.x+math.Cos(an)*d, z.y+math.Sin(an)*d*0.8, col, 1.2-k)
					}
					p.Glow(z.x, z.y, 10, C[3], 0.25*(1-k))
					continue
				}
				for j := 1; j <= 6; j++ {
					q := sleepZAt(a, ze.i, math.Max(0, ze.u-float64(j)*0.025))
					if j%2 == 0 || ze.u-float64(j)*0.025 < 0.05 {
						continue
					}
					p.Dot(q.x-1, q.y+4+float64(q.size), C[3], (1-float64(j)/7)*0.9)
				}
				old := ze.u > 0.78
				R := [4]uint32{C[1], C[2], C[3], C[4]}
				gk := 0.26
				if old {
					R = [4]uint32{C[0], C[1], C[2], C[3]}
					gk = 0.12
				}
				icon(p, z.x, z.y, 7, sleepZS[z.size], R, z.k, nil)
				if !old {
					p.Px(z.x-2-float64(z.size), z.y-2-float64(z.size)*0.8, C[5])
				}
				p.Glow(z.x, z.y, 7+float64(z.size)*3, C[3], gk*z.k)
			}
		},
	}
}
