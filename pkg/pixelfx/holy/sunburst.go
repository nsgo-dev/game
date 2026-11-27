// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/sunburst.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Sunburst is a white-hot core in a gold corona; long and short rays turn slowly round it, breathing, with glints running out along them that wink at the tips. It is registered as "holy/sunburst".
var Sunburst = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Sunburst",
		Desc:     "A white-hot core in a gold corona; long and short rays turn slowly round it, breathing, with glints running out along them that wink at the tips.",
		Tags:     []string{"holy", "sun", "radiant", "rays", "light", "glow", "loop"},
		Size:     [2]int{128, 128},
		Origin:   [2]int{64, 64},
		Duration: sunburstT,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: goldPalette()},
			{Name: "rays", Default: pixelfx.Num(8)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newSunburst,
}

func init() { pixelfx.Register("holy/sunburst", Sunburst) }

const sunburstT = 4

var sunburstCore, sunburstRing, sunburstR0 = 4.5, 7.5, 9.0

// sunburstRay draws one tapered ray from radius R0 out to R0 + L at angle a: white at the root, cooling to gold, thinning into light.
func sunburstRay(p *pixelfx.Painter, C []uint32, a, L, w0, k float64) {
	R0 := sunburstR0
	ca, sa := math.Cos(a), math.Sin(a)
	x1, y1, pad := ca*(R0+L), sa*(R0+L), w0+1
	p.Shade(math.Min(0, x1)-pad, math.Min(0, y1)-pad, math.Max(0, x1)+pad, math.Max(0, y1)+pad, func(x, y float64, out *pixelfx.Out) uint32 {
		along := x*ca + y*sa - R0
		u := along / L
		if u < 0 || u > 1 {
			return 0
		}
		across, w := math.Abs(-x*sa+y*ca), (w0/2)*(1-u*0.85)+0.25
		if across > w {
			return 0
		}
		spine, rim := across < 0.5, across > w-0.6 && w > 0.9
		switch {
		case spine && u < 0.4:
			out.C = pixelfx.White
		case spine && u < 0.75:
			out.C = C[6]
		case spine:
			out.C = C[5]
		case rim && u > 0.15:
			out.C = C[3]
		case u < 0.3:
			out.C = C[6]
		case u < 0.62:
			out.C = C[5]
		default:
			out.C = C[4]
		}
		if u < 0.72 {
			out.A = k
		} else {
			out.A = k * (1 - (u-0.72)/0.28) * 1.2
		}
		return 0
	})
	p.GlowLine(ca*R0, sa*R0, ca*(R0+L*0.8), sa*(R0+L*0.8), 5, C[5], 0.16*k)
}

func newSunburst(o *pixelfx.Options) pixelfx.Instance {
	C, rays, seed := pixelfx.Fit(o.Palette(), 8), bounded(o.Num("rays"), 1, 64), o.Seed()
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		T := float64(sunburstT)
		N := rays * 2
		rot := (t / T) * ((2 * tau) / N)
		R0 := sunburstR0
		p.Glow(0, 0, 62, C[4], 0.3+0.06*math.Sin((t/T)*tau*2))
		// fine threads of light between the rays
		for i := 0; float64(i) < N; i++ {
			a := rot + ((float64(i)+0.5)*tau)/N
			L := 14 + 6*math.Sin(a*3+(t/T)*tau)
			p.LineFn(math.Cos(a)*(R0+1), math.Sin(a)*(R0+1), math.Cos(a)*(R0+L), math.Sin(a)*(R0+L), func(u float64) (uint32, float64) {
				if u < 0.3 {
					return C[6], 0.7 - u*0.8
				}
				return C[5], 0.7 - u*0.8
			})
		}
		// the rays, long and short by turns, breathing as a wave runs round them
		for i := 0; float64(i) < N; i++ {
			a := rot + (float64(i)*tau)/N
			long := i%2 == 0
			breathe := 0.86 + 0.14*math.Sin(a*3-(t/T)*tau*2)
			L, w0 := 21*breathe, 2.6
			if long {
				L, w0 = 36*breathe, 4
			}
			sunburstRay(p, C, a, L, w0, 1)
			// a glint runs out along each long ray and winks at the tip
			if long {
				g := pixelfx.Fract((t/T)*2 + (a*3)/tau)
				r := R0 + g*L
				if g < 0.9 {
					p.Px(math.Cos(a)*r, math.Sin(a)*r, pixelfx.White)
					p.Glow(math.Cos(a)*r, math.Sin(a)*r, 5, C[6], 0.45*(1-g))
				} else {
					pixelfx.Sparkle(p, math.Cos(a)*(R0+L), math.Sin(a)*(R0+L), 1+3*math.Sin(((g-0.9)/0.1)*pi), pixelfx.White, C[6], 0.5)
				}
			}
		}
		// the corona and the core
		RING, CORE := sunburstRing, sunburstCore
		p.Shade(-RING-2, -RING-2, RING+2, RING+2, func(x, y float64, out *pixelfx.Out) uint32 {
			d := pixelfx.Hypot(x, y)
			if d > RING {
				return 0
			}
			flick := 0.5 + 0.5*math.Sin(math.Atan2(y, x)*5+(t/T)*tau*3)
			switch {
			case d < CORE:
				out.C = pixelfx.White
			case d < CORE+1.5:
				if flick > 0.6 {
					out.C = pixelfx.White
				} else {
					out.C = C[6]
				}
			case flick > 0.5:
				out.C = C[5]
			default:
				out.C = C[4]
			}
			return 0
		})
		p.Glow(0, 0, 16, pixelfx.White, 0.28)
		// twinkles round it
		pixelfx.Cycle(t, T, 10, seed, func(g *pixelfx.Rng) float64 { return g.Range(0.35, 0.7) }, func(age, life float64, R *pixelfx.Rng, _ int) {
			a, d, k := R.Range(0, tau), R.Range(16, 44), math.Sin((age/life)*pi)
			pixelfx.Sparkle(p, math.Cos(a)*d, math.Sin(a)*d, 1+2.4*k*R.Range(0.5, 1), pixelfx.White, C[5], 0.4*k)
		})
	}}
}
