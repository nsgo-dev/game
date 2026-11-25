// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/whirlpool.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the swirl's texture (streaks laid along a differentially wound flow, light on the brightest pixels) from Sprite Fusion's open-source black hole (https://www.spritefusion.com/pixel-art-vfx/black-hole), MIT License, Copyright (c) 2026 Hugo Duprez.

package water

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Whirlpool winds dark water into a top-down vortex: foam arms spiral in, darkening as they are drawn down into a deep eye with a lit far rim, flecks of foam circling in after them. It is registered as "water/whirlpool".
var Whirlpool = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Whirlpool",
		Desc:     "Seen from above, dark water winds into a vortex: foam arms spiral in, darkening as they're drawn down into a deep eye with a lit far rim, flecks of foam circling in after them.",
		Tags:     []string{"water", "whirlpool", "vortex", "maelstrom", "swirl", "top-down", "loop"},
		Size:     [2]int{128, 84},
		Origin:   [2]int{64, 42},
		Duration: whirlpoolDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newWhirlpool,
}

func init() { pixelfx.Register("water/whirlpool", Whirlpool) }

const (
	whirlpoolDuration = 3
	whirlpoolR        = 60
	whirlpoolSY       = 0.6
	whirlpoolK        = 2.4
	whirlpoolEye      = 0.16
)

// whirlpoolFoam returns the foam intensity along differentially wound log-spiral arms.
func whirlpoolFoam(rho, th, ph float64) float64 {
	s := math.Log(math.Max(rho, 0.02))
	a := th + whirlpoolK*s + ph*tau
	b := th + whirlpoolK*1.35*s + ph*tau*2
	one := math.Pow(0.5+0.5*math.Cos(7*a+0.9*math.Sin(3*a+s*4)), 26)
	two := math.Pow(0.5+0.5*math.Cos(11*b+1.3), 34) * 0.7
	// finer streaks crowd in near the eye, where the water runs fastest
	three := math.Pow(0.5+0.5*math.Cos(17*(th+whirlpoolK*0.8*s+ph*tau*3)), 30) * 0.8 * math.Max(0, 1-(rho-whirlpoolEye)*2.2)
	dash := 0.55 + 0.45*math.Sin(s*22-ph*tau*4+a*2)
	return (one + two + three) * dash
}

func newWhirlpool(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	D := float64(whirlpoolDuration)
	ry := float64(whirlpoolR) * whirlpoolSY
	return &pixelfx.Funcs{
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			p.Shade(-whirlpoolR, -ry, whirlpoolR, ry, func(x, y float64, out *pixelfx.Out) uint32 {
				dx, dy := x/whirlpoolR, y/ry
				rho := pixelfx.Hypot(dx, dy)
				if rho > 1 {
					return 0
				}
				th := math.Atan2(dy, dx)
				// the eye: a hole down into the dark, its far wall lit, a ring of foam racing round its lip
				if rho < whirlpoolEye {
					far := -dy / rho
					// the far wall of the funnel, lit, falling off into the dark below the lip on the near side
					wall := (rho/whirlpoolEye - 0.35) / 0.65
					if far > 0 {
						switch {
						case wall > 0.8:
							if far > 0.6 {
								out.C = C[5]
							} else {
								out.C = C[4]
							}
						case wall > 0.45:
							out.C = C[3]
						case wall > 0.15:
							out.C = C[2]
						default:
							out.C = C[0]
						}
					} else {
						if wall > 0.85 && far > -0.5 {
							out.C = C[2]
						} else {
							out.C = C[0]
						}
					}
					return 0
				}
				k := (rho - whirlpoolEye) / (1 - whirlpoolEye)
				fade := 1 - pixelfx.Smooth(0.62, 1, k)
				// dark see-through water, darker toward the eye, a faint sheen mid-way
				bc := C[1]
				if k < 0.25 {
					bc = C[0]
				}
				p.Blend(x, y, bc, (0.75-0.25*k)*(1-pixelfx.Smooth(0.8, 1, k)))
				if k > 0.3 && k < 0.8 {
					p.Blend(x, y, C[2], 0.18*math.Sin(pi*(k-0.3)/0.5))
				}
				// the foam ring round the eye
				if k < 0.035 {
					g := 0.5 + 0.5*math.Cos(th*9-ph*tau*6)
					switch {
					case g > 0.7:
						out.C = pixelfx.White
					case g > 0.3:
						out.C = C[6]
					default:
						out.C = C[4]
					}
					return 0
				}
				I := whirlpoolFoam(rho, th, ph) * fade * math.Min(1, (k-0.08)*4) * (0.45 + 0.55*math.Sin(pi*math.Min(1, k*1.25)))
				if I > 0.12 {
					switch {
					case I > 0.75:
						out.C = pixelfx.White
					case I > 0.45:
						out.C = C[6]
					case I > 0.25:
						out.C = C[5]
					default:
						out.C = C[4]
					}
					if I > 0.25 {
						out.L = C[5]
						out.K = 0.08 + 0.14*math.Min(1, I)
					}
				}
				return 0
			})
			// flecks of foam drawn round and in along the arms, each a short streak along its path
			pixelfx.Cycle(t, D, 30, seed, func(g *pixelfx.Rng) float64 { return g.Range(1.2, 2.2) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				u := age / life
				rho := 0.8 - 0.62*u*u
				a0 := r.Range(0, tau)
				th := a0 - whirlpoolK*math.Log(rho)
				ang := th - age*(1.5+2.2/rho)
				x, y := math.Cos(ang)*rho*whirlpoolR, math.Sin(ang)*rho*ry
				ang2 := ang + 0.12/rho
				x2, y2 := math.Cos(ang2)*rho*whirlpoolR, math.Sin(ang2)*rho*ry
				if u > 0.85 && r.Next() < (u-0.85)*6 {
					return
				}
				c := C[6]
				if u < 0.2 {
					c = C[5]
				} else if u < 0.6 {
					c = pixelfx.White
				}
				p.Line(x2, y2, x, y, c, 1)
			})
			// glints winking on the foam
			pixelfx.Cycle(t, D, 8, seed+3, func(g *pixelfx.Rng) float64 { return g.Range(0.25, 0.45) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				rho := r.Range(0.35, 0.8)
				th := r.Range(0, tau) - age*2.5
				k := math.Sin((pi * age) / life)
				x, y := math.Cos(th)*rho*whirlpoolR, math.Sin(th)*rho*ry
				if whirlpoolFoam(rho, th, ph) < 0.35 {
					return
				}
				p.Px(x, y, pixelfx.White)
				if k > 0.55 {
					for _, q := range cross4 {
						p.Px(x+q[0], y+q[1], C[6])
					}
				}
				p.Glow(x, y, 5, C[5], 0.4*k)
			})
			// light: a cool sheen on the swirl, a glow off the eye's lit rim
			p.Glow(0, 0, whirlpoolR*0.9, C[4], 0.2)
			p.Glow(0, 0, whirlpoolR*whirlpoolEye*1.6+6, C[5], 0.28)
			p.Glow(0, -ry*whirlpoolEye*0.8, 14, C[5], 0.32)
		},
	}
}
