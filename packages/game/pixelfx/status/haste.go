// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/haste.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the banded after-images, the dotted speed lines with bright heads and the kicked-up dust from Sprite Fusion's open-source movement dash (https://www.spritefusion.com/pixel-art-vfx/movement), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

const hasteDuration = 1.2

var (
	hasteRun = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.25), pixelfx.Lean.Is(4))
	hasteR1  = hasteRun.With(pixelfx.Crouch.Is(3), pixelfx.FFX.Is(12), pixelfx.FFY.Is(-5), pixelfx.FBX.Is(-10), pixelfx.FBY.Is(-8), pixelfx.ToeB.On(), pixelfx.FX.Is(-7), pixelfx.FY.Is(-19), pixelfx.BX.Is(12), pixelfx.BY.Is(-27))
	hasteR2  = hasteRun.With(pixelfx.Crouch.Is(1), pixelfx.FFX.Is(4), pixelfx.FFY.Is(-8), pixelfx.FBX.Is(-1), pixelfx.FBY.Is(-5), pixelfx.FX.Is(3), pixelfx.FY.Is(-18), pixelfx.BX.Is(5), pixelfx.BY.Is(-19))
	hasteR3  = hasteRun.With(pixelfx.Crouch.Is(3), pixelfx.FFX.Is(-10), pixelfx.FFY.Is(-8), pixelfx.ToeF.On(), pixelfx.FBX.Is(12), pixelfx.FBY.Is(-5), pixelfx.FX.Is(14), pixelfx.FY.Is(-27), pixelfx.BX.Is(-5), pixelfx.BY.Is(-19))
	hasteR4  = hasteRun.With(pixelfx.Crouch.Is(1), pixelfx.FFX.Is(-1), pixelfx.FFY.Is(-5), pixelfx.FBX.Is(4), pixelfx.FBY.Is(-8), pixelfx.FX.Is(5), pixelfx.FY.Is(-19), pixelfx.BX.Is(3), pixelfx.BY.Is(-18))
)

// Haste is sped up: striped after-images stream out behind, speed lines whip past, dust kicks off the heels and the leading edge catches the wind. Faces right. It is registered as "status/haste".
var Haste = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Haste",
		Desc:     "Sped up: striped after-images stream out behind, speed lines whip past, dust kicks off the heels and the leading edge catches the wind. Faces right.",
		Tags:     []string{"status", "haste", "speed", "fast", "buff", "directional", "loop"},
		Size:     [2]int{100, 56},
		Origin:   [2]int{64, 48},
		Duration: hasteDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4c25ab", "#773ae4", "#5b6cff", "#5eb0ff", "#a3dcff", "#ccf6ff", "#ffffff")},
			{Name: "dust", Default: pixelfx.Color("#8a8090")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Follow: pixelfx.Val(0),
		Poses: []pixelfx.Key{
			{T: 0, Pose: hasteR1},
			{T: 0.1, Pose: hasteR2, How: "snap"},
			{T: 0.2, Pose: hasteR3, How: "snap"},
			{T: 0.3, Pose: hasteR4, How: "snap"},
			{T: 0.4, Pose: hasteR1, How: "snap"},
			{T: 0.5, Pose: hasteR2, How: "snap"},
			{T: 0.6, Pose: hasteR3, How: "snap"},
			{T: 0.7, Pose: hasteR4, How: "snap"},
			{T: 0.8, Pose: hasteR1, How: "snap"},
			{T: 0.9, Pose: hasteR2, How: "snap"},
			{T: 1, Pose: hasteR3, How: "snap"},
			{T: 1.1, Pose: hasteR4, How: "snap"},
			{T: 1.2, Pose: hasteR1, How: "snap"},
		},
	},
	New: newHaste,
}

func init() { pixelfx.Register("status/haste", Haste) }

type hasteLevel struct {
	x, y, v int
}

func hasteLevelsOf(a *pixelfx.Actor, S *actorShape) []hasteLevel {
	out := make([]hasteLevel, len(S.body))
	for i, pt := range S.body {
		v := pixelfx.Luma(colorAt(a, pt[0], pt[1]))
		lv := 3
		if v < 0.24 {
			lv = 1
		} else if v < 0.66 {
			lv = 2
		}
		out[i] = hasteLevel{pt[0], pt[1], lv}
	}
	return out
}

func hasteLines(p *pixelfx.Painter, C []uint32, a *pixelfx.Actor, t, seed float64, front bool) {
	count := 11
	seedOff := 7.0
	lenMul := 1.0
	alphaMul := 1.1
	if front {
		count = 5
		seedOff = 9
		lenMul = 0.7
		alphaMul = 0.7
	}
	pixelfx.Cycle(t, hasteDuration, count, seed+seedOff, func(r *pixelfx.Rng) float64 { return r.Range(0.12, 0.26) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		u := age / life
		y := a.Y + r.Range(2, float64(a.H)-3)
		ln := r.Range(10, 26) * lenMul
		x := r.Range(-14, 4) - age*r.Range(160, 240)
		for d := 0.0; d < ln; d++ {
			if d > ln*0.45 && int(d)%2 != 0 {
				continue
			}
			col := C[3]
			if d < 2 {
				col = C[6]
			} else if d < ln*0.4 {
				col = C[5]
			}
			p.Dot(x+d, y, col, (1-d/ln)*(1-u)*alphaMul)
		}
		p.Light(x, y, C[4], 0.4*(1-u))
	})
}

func newHaste(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	dust := o.Color("dust")
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	L := hasteLevelsOf(a, S)
	everys := [4]int{0, 3, 4, 0}
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			f := int(math.Floor(t * 30))
			for g := 3; g >= 1; g-- {
				dx := float64(-g*14) - pixelfx.Round(pixelfx.Rand(float64(f>>1), float64(g), 3)*1.6)
				every := everys[g]
				p.OutlineFn(a.Mask, a.X+dx, a.Y, func(mx, my int) (uint32, float64) {
					if g == 3 && (mx+my)%2 != 0 {
						return 0, 1
					}
					return C[5-g], 1
				})
				if every != 0 {
					for _, lv := range L {
						if ((lv.y+f)%every+every)%every != 0 {
							continue
						}
						idx := max(0, min(6, 5-g+lv.v-2))
						p.Px(a.X+float64(lv.x)+dx, a.Y+float64(lv.y), C[idx])
					}
				}
				p.Glow(a.X+float64(a.W)/2+dx, a.Y+float64(a.H)*0.45, 16, C[3], 0.12*(1-float64(g)*0.25))
			}
			for _, s := range S.side {
				x, y, dir := s[0], s[1], s[2]
				if dir > 0 || (y+(f>>1))%3 != 0 {
					continue
				}
				ln := 3 + int(math.Floor(pixelfx.Rand(float64(y), float64(f>>1), 5)*6))
				for d := 1; d <= ln; d++ {
					col := C[4]
					if d < 3 {
						col = C[5]
					}
					p.Dot(a.X+float64(x-d), a.Y+float64(y), col, 1-float64(d)/float64(ln+1))
				}
			}
			hasteLines(p, C, a, t, seed, false)
			pixelfx.Cycle(t, hasteDuration, 12, seed+5, func(r *pixelfx.Rng) float64 { return r.Range(0.45, 0.7) }, func(age, life float64, r *pixelfx.Rng, i int) {
				u := age / life
				foot := 3.0
				if i%2 != 0 {
					foot = -7
				}
				x := foot + travel(-r.Range(50, 100), 0, 3.5, age)
				y := -1.5 - travel(r.Range(6, 18), 0, 3.5, age)
				rad := r.Range(2.6, 4.6) * (0.6 + 1*u)
				k := math.Pow(1-u, 1.2) * math.Min(1, u*10)
				p.Disc(x, y, rad, dust, k*0.5)
				p.Disc(x+rad*0.05, y+rad*0.08, rad*0.74, dust, k*0.62)
				p.Disc(x-rad*0.22, y-rad*0.3, rad*0.5, C[5], k*0.25)
			})
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			f15 := math.Floor(t * 15)
			for _, s := range S.side {
				x, y, dir := s[0], s[1], s[2]
				if dir > 0 && pixelfx.Rand(float64(x), float64(y), f15) < 0.85 {
					col := C[5]
					if pixelfx.Rand(float64(y), f15, 2) < 0.3 {
						col = C[6]
					}
					p.Px(a.X+float64(x), a.Y+float64(y), col)
				}
			}
			hasteLines(p, C, a, t, seed, true)
			p.Glow(-6, a.Y+float64(a.H)*0.5, 22, C[3], 0.16)
			p.Glow(0, a.Y+float64(a.H)*0.45, 30, C[3], 0.16)
			p.Glow(a.X+float64(a.W), a.Y+float64(a.H)*0.5, 10, C[5], 0.12+0.06*math.Sin((t/hasteDuration)*tau*3))
		},
	}
}
