// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/hit-flash.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var hitFlashBlown = pixelfx.Pose{}.With(pixelfx.X.Is(-5), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-5), pixelfx.HX.Is(-1), pixelfx.FX.Is(8), pixelfx.FY.Is(-33), pixelfx.BX.Is(-11), pixelfx.BY.Is(-27), pixelfx.FFX.Is(9), pixelfx.FFY.Is(-7), pixelfx.FBX.Is(-6))

// HitFlash is the character taking damage: a white frame, then their own colors flushed red and blinking white, cooling off, while comic hit lines jab in at them. It is registered as "impacts/hit-flash".
var HitFlash = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Hit flash",
		Desc:     "The character takes damage: a white frame, then their own colors flushed red and blinking white, cooling off, while comic hit lines jab in at them.",
		Tags:     []string{"impact", "hit", "damage", "hurt", "flash", "blink", "actor", "comic", "one-shot"},
		Size:     [2]int{80, 64},
		Origin:   [2]int{40, 54},
		Duration: 0.5,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#14040a", "#3a0a14", "#6a101c", "#a01824", "#d42a30", "#ff5a50", "#ff9a8a")},
			{Name: "lines", Default: pixelfx.Num(7)},
			{Name: "side", Default: pixelfx.Num(1)},
			{Name: "seed", Default: pixelfx.Num(17)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: -0.26, Pose: pixelfx.Ready.With(pixelfx.X.Is(0))},
			{T: 0, Pose: hitFlashBlown, How: "snap"},
			{T: 0.16, Pose: hitFlashBlown.With(pixelfx.X.Is(-8), pixelfx.Crouch.Is(5))},
			{T: 0.42, Pose: hitFlashBlown.With(pixelfx.X.Is(-8), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-2), pixelfx.FX.Is(9), pixelfx.FY.Is(-22), pixelfx.BX.Is(6), pixelfx.BY.Is(-20), pixelfx.FFY.Is(-4))},
			{T: 0.86, Pose: pixelfx.Ready.With(pixelfx.X.Is(0))},
		},
		Hits: []pixelfx.Hit{{T: 0, Stop: 0.07, Shake: 0.14, Amp: pixelfx.Val(2)}},
	},
	New: newHitFlash,
}

func init() { pixelfx.Register("impacts/hit-flash", HitFlash) }

// The beats, by frame at 30 fps: white, white, red, red, white rim, red cooling off.
type hitFlashBeat uint8

const (
	beatNone hitFlashBeat = iota
	beatWhite
	beatWhite2
	beatRed
	beatRim
	beatCool
)

var hitFlashBeats = [...]struct {
	end  float64
	beat hitFlashBeat
}{{0.025, beatWhite}, {0.058, beatWhite2}, {0.125, beatRed}, {0.158, beatRim}, {0.36, beatCool}}

type hitLine struct{ a, d, len, t0 float64 }

func newHitFlash(o *pixelfx.Options) pixelfx.Instance {
	C, R, A, S := pixelfx.Fit(o.Palette(), 7), pixelfx.NewRng(o.Seed()), o.Actor, o.Num("side")
	M, PX := A.Mask, A.Pixels
	cx, cy := A.Chest[0], A.Chest[1]
	// where the blow lands: the character's front, chest high
	hx, hy := cx+S*12, cy-5
	lights := &pixelfx.Lights{}
	lights.Add(hx, hy, 34, pixelfx.White, 0.6, 0.1)
	lights.Add(cx, cy, 30, C[4], 0.5, 0.35)
	// the hurt colors: each pixel's own light and shade carried into the red ramp
	reds, tints := make([]uint32, len(PX)), make([]uint32, len(PX))
	for i, c := range PX {
		if c == 0 {
			continue
		}
		l := pixelfx.Luma(c | 0xff000000)
		reds[i] = C[int(pixelfx.ClampTo(pixelfx.Round(2+l*5), 2, 6))]
		tints[i] = pixelfx.Mix(c|0xff000000, C[4], 0.55)
	}
	// hit lines all round the top and sides, jabbing in at the body
	n := int(o.Num("lines"))
	lines := make([]hitLine, max(n, 0))
	for i := range lines {
		a := -pi - 0.3 + ((float64(i)+R.Range(0.25, 0.75))/o.Num("lines"))*(pi+0.6)
		lines[i] = hitLine{a: a, d: R.Range(15, 19), len: R.Range(6, 9), t0: R.Range(0, 0.04)}
	}
	// when the red cools off, as variables so the difference rounds as pixelfx's does
	coolFrom, coolTo := 0.158, 0.36
	t := 0.0
	beat := func() hitFlashBeat {
		for _, b := range hitFlashBeats {
			if t < b.end {
				return b.beat
			}
		}
		return beatNone
	}
	body := func(p *pixelfx.Painter, color func(i int) uint32) {
		p.MaskFn(M, A.X, A.Y, func(mx, my int) (uint32, float64) { return color(my*M.W + mx), 1 })
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			b := beat()
			switch b {
			case beatWhite, beatWhite2:
				body(p, func(int) uint32 { return pixelfx.White })
				c := C[5]
				if b == beatWhite {
					c = C[6]
				}
				p.Outline(M, A.X, A.Y, c, 1)
			case beatRed:
				body(p, func(i int) uint32 { return reds[i] })
				p.Outline(M, A.X, A.Y, C[5], 1)
			case beatRim:
				p.Outline(M, A.X, A.Y, pixelfx.White, 1)
			case beatCool:
				k := 1 - (t-coolFrom)/(coolTo-coolFrom)
				body(p, func(i int) uint32 { return pixelfx.Mix(PX[i]|0xff000000, tints[i], k) })
				if k > 0.5 {
					p.Outline(M, A.X, A.Y, C[4], (k-0.5)*2)
				}
			}
			// the blow: a small hot burst on the side it came from
			if t < 0.07 {
				big, k := 1.3, 1.0
				if t < 0.035 {
					big, k = 1, 0
				}
				for i := range 8 {
					a := (float64(i)/8)*pixelfx.Tau + 0.2
					L := 9 * big
					if i%2 != 0 {
						L = 4 * big
					}
					p.LineFn(hx+math.Cos(a)*L*0.45*k, hy+math.Sin(a)*L*0.45*k, hx+math.Cos(a)*L, hy+math.Sin(a)*L, func(u float64) (uint32, float64) {
						switch {
						case u < 0.45 && k == 0:
							return pixelfx.White, 1
						case u < 0.75:
							return C[6], 1
						}
						return C[4], 1
					})
				}
				if k == 0 {
					for _, d := range [5][2]float64{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						p.Px(hx+d[0], hy+d[1], pixelfx.White)
					}
				}
				p.Glow(hx, hy, 14, C[6], 0.5*(1-t/0.07))
			}
			// comic hit lines jab in toward the body, then are gone
			for _, l := range lines {
				u := (t - l.t0) / 0.16
				if u < 0 || u >= 1 {
					continue
				}
				d0, L, ca, sa := l.d-6*pixelfx.Ease.OutCubic(u), l.len*(1-u), math.Cos(l.a), math.Sin(l.a)*1.15
				if L < 1 {
					continue
				}
				// a wedge, its point at the body and its fat end out, white with a red edge
				w := (2.8 - 1.6*u) / 2
				nx, ny := -sa*w, ca*w
				ex, ey := cx+ca*(d0+L), cy+sa*(d0+L)
				px0, py0 := cx+ca*d0, cy+sa*d0
				p.Poly([][2]float64{{px0 - ca, py0 - sa}, {ex + nx*1.5 + ca, ey + ny*1.5 + sa}, {ex - nx*1.5 + ca, ey - ny*1.5 + sa}}, C[3], 1)
				c := C[6]
				if u < 0.5 {
					c = pixelfx.White
				}
				p.Poly([][2]float64{{px0, py0}, {ex + nx, ey + ny}, {ex - nx, ey - ny}}, c, 1)
			}
			if k := math.Exp(-t / 0.08); k > 0.02 {
				c := C[5]
				if b == beatWhite || b == beatWhite2 {
					c = pixelfx.White
				}
				p.Glow(cx, cy, 26, c, 0.35*k)
			}
			lights.Draw(p)
		},
	}
}
