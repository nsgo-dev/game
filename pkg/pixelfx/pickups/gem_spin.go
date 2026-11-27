// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/gem-spin.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the hover, floor shadow and twinkle glints from Sprite Fusion's open-source pickup and sparkle (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// GemSpin is a brilliant-cut ruby turning on its axis: real facets catch the light in turn, flashes roll over the crown, refracted light slides across the floor, sparkles wink round it. It is registered as "pickups/gem-spin".
var GemSpin = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Gem spin",
		Desc:     "A brilliant-cut ruby turning on its axis: real facets catch the light in turn, flashes roll over the crown, refracted light slides across the floor, sparkles wink round it.",
		Tags:     []string{"pickup", "gem", "jewel", "treasure", "ruby", "loop"},
		Size:     [2]int{64, 64},
		Origin:   [2]int{32, 56},
		Duration: gemSpinDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#16040e", "#3e0a1e", "#760e2e", "#b8163c", "#ec3250", "#ff7482", "#ffc4c8", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newGemSpin,
}

func init() { pixelfx.Register("pickups/gem-spin", GemSpin) }

const (
	gemSpinDuration = 2.4
	gemSpinHover    = 19.0
)

var gemSpinSolid = brilliant(7.5, 3.2, 0.5, 9.5, 0.9, 8)

func gemSpinPose(t, T float64) (y, spin float64) {
	return -gemSpinHover - 1.8*math.Sin((t/T)*tau), (t / T) * pi
}

func newGemSpin(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	T := float64(gemSpinDuration)
	L := newLayer(32, 32)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			y, spin := gemSpinPose(t, T)
			lift := (-y - gemSpinHover + 2) / 4
			blobShadow(p, 0, 8-lift*1.5, 0.75-lift*0.2, 0)
			for k := range 3 {
				a := spin*4 + (float64(k)*tau)/3
				x, k2 := math.Sin(a)*9, 0.5+0.5*math.Cos(a)
				if k2 < 0.15 {
					continue
				}
				p.Dot(x, 0, pick(k2 > 0.7, C[6], C[5]), k2*1.2)
				p.Dot(x+1, 0, C[4], k2*0.8)
				p.Dot(x-1, 0, C[4], k2*0.6)
				p.Glow(x+0.5, 0, 6, C[5], 0.3*k2)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			y, spin := gemSpinPose(t, T)
			p.GlowRing(0, y-1, 5, 15, C[4], 0.34)
			p.Glow(0, y, 26, C[3], 0.14)
			drawGem(L, p, gemSpinSolid, 0, y, spin, C, drawGemOpts{tilt: 0.22, a: 1, edges: true, gain: 1})
			f := math.Mod((spin*4)/tau, 1)
			fk := math.Max(0, 1-math.Abs(f-0.5)/0.08)
			if fk > 0 {
				glint(p, -3, y-3, 3.4*pixelfx.Ease.OutQuad(fk), C[6], 0.45*fk)
			}
			for i := range 8 {
				fi := float64(i)
				life := 0.4
				age := pixelfx.Wrap(t-(fi/8)*T-pixelfx.Rand(fi, seed, 3)*0.1, T)
				if age > life {
					continue
				}
				a := pixelfx.Rand(fi, seed, 1)*tau + fi*2.4
				d := 10 + pixelfx.Rand(fi, seed, 2)*7
				k := math.Sin((age / life) * pi)
				glint(p, math.Cos(a)*d, y+math.Sin(a)*d*0.8-age*6, pixelfx.Round(2.4*k*(0.6+0.4*pixelfx.Rand(fi, 9))), pick(i%3 != 0, C[6], pixelfx.White), 0.4*k)
			}
		},
	}
}
