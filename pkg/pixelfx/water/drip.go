// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/drip.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source blood (https://www.spritefusion.com/pixel-art-vfx/blood), MIT License, Copyright (c) 2026 Hugo Duprez, for the bead swelling at the tip and the falling drop's tail.

package water

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Drip is a bead swelling under a wet stone tip, pinching off, falling stretched by its speed and plinking into a puddle with a crown, a ring and a tiny rebound. It is registered as "water/drip".
var Drip = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Drip",
		Desc:     "A bead swells under a wet stone tip and catches the light, pinches off, falls stretched by its speed and plinks into a puddle with a crown, a ring and a tiny rebound.",
		Tags:     []string{"water", "drip", "drop", "puddle", "cave", "ambient", "loop"},
		Size:     [2]int{72, 88},
		Origin:   [2]int{36, 78},
		Duration: dripDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "height", Default: pixelfx.Num(62)},
			{Name: "source", Default: pixelfx.Bool(true)},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newDrip,
}

func init() { pixelfx.Register("water/drip", Drip) }

const (
	dripDuration = 1.6
	dripSY       = 0.28
	dripG        = 760
	dripLet      = 0.78
	dripPRX      = 20
	dripRebound  = 0.06
	dripBack     = 0.2
)

var (
	dripWet   = pixelfx.Ramp("#5d6a86", "#9fb6d6")
	dripCrown = [3][2]float64{{-26, 70}, {24, 64}, {-6, 90}}
)

func dripLandAt(H float64) float64 { return dripLet + math.Sqrt((2*H)/dripG) }

// dripstone draws the stone the water gathers on: a lump of ceiling rock tapering to a dripstone point, lit on its left, a dark rim, a wet streak running down to the point.
func dripstone(p *pixelfx.Painter, H float64) {
	ST, top := rock(), -H-14
	p.Shade(-24, top, 22, -H+0.5, func(x, y float64, out *pixelfx.Out) uint32 {
		// a ragged underside of ceiling rock that hangs down into a short dripstone, its point at x = 0
		under := top + 7 + math.Sin(x*0.45)*1.2 + math.Sin(x*1.3+1)*0.6 - math.Abs(x+4)*0.12
		cone := -H - math.Abs(x)*1.25
		bottom := math.Max(under, math.Min(cone, -H))
		span := 22.0
		if x < 0 {
			span = 24
		}
		if y > bottom || math.Abs(x) > span-math.Max(0, top+3-y)*4 {
			return 0
		}
		rim := y > bottom-1 || math.Abs(x) > span-1
		lx, v := (x+24)/46, (y-top)/14
		switch {
		case rim:
			out.C = ST[0]
		case v < 0.25:
			out.C = ST[1]
		case y > under && math.Abs(x) < 6:
			if x < 0 {
				out.C = ST[4]
			} else {
				out.C = ST[2]
			}
		case lx < 0.3:
			out.C = ST[4]
		case lx < 0.62:
			out.C = ST[3]
		default:
			out.C = ST[2]
		}
		if !rim && math.Abs(x-9-(y-top)*0.4) < 0.5 && v > 0.3 && v < 0.7 {
			out.C = ST[0]
		}
		return 0
	})
	// a wet streak down to the point, catching the light
	for y := -H - 7; y < -H; y++ {
		c := dripWet[0]
		if y > -H-3 {
			c = dripWet[1]
		}
		p.Px(-1, y, c)
	}
	p.Px(-1, -H-2, pixelfx.White)
}

// dripPuddle draws the puddle: dark see-through water with a lit far edge, the drop's light in it.
func dripPuddle(p *pixelfx.Painter, C []uint32) {
	ry := float64(dripPRX) * dripSY
	for Y := math.Floor(-ry); Y < ry; Y++ {
		for X := -dripPRX; X < dripPRX; X++ {
			x, y := float64(X)+0.5, Y+0.5
			dx, dy := x/dripPRX, y/ry
			d := dx*dx + dy*dy
			if d > 1 {
				continue
			}
			p.Blend(x, y, C[1], 0.72*math.Min(1, (1-d)*5))
		}
	}
	// the far edge catches the light in one fine line
	for X := -dripPRX + 3; X < dripPRX-3; X++ {
		x := float64(X) + 0.5
		dx := x / dripPRX
		y := -math.Sqrt(1-dx*dx) * ry
		c := C[3]
		if math.Abs(x) < dripPRX*0.5 {
			c = C[4]
		}
		p.Dot(x, y+0.5, c, 0.95)
	}
}

func newDrip(o *pixelfx.Options) pixelfx.Instance {
	C, H, source := pixelfx.Fit(o.Palette(), 8), o.Num("height"), o.Bool("source")
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			dripPuddle(p, C)
			u := t - dripLandAt(H)
			if u >= 0 {
				ring(p, 0, 0, 2+17*pixelfx.Ease.OutCubic(u/0.75), dripSY, 1.1*(1-u/0.75), C, ringOpts{n: 2, gap: 2.5, half: -1, seed: 1, wob: 0.4})
			}
			v := t - dripLandAt(H) - dripRebound - dripBack
			if v >= 0 {
				ring(p, 0, 0, 1.5+9*pixelfx.Ease.OutCubic(v/0.5), dripSY, 0.9*(1-v/0.5), C, ringOpts{n: 1, half: -1, seed: 2})
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			land := dripLandAt(H)
			if source {
				dripstone(p, H)
			}
			// the bead swells at the tip, wobbling, and catches the light; its neck stretches before it lets go
			if t < dripLet {
				k := t / dripLet
				r := 0.7 + 2.3*pixelfx.Ease.InQuad(k)
				wob := 0.15 * math.Sin(t*26) * k
				neck := pixelfx.Smooth(0.82, 1, k) * 2.5
				cy := -H + r + neck
				if neck > 0.2 {
					p.Line(0, -H, 0, cy-r, C[5], 1)
				}
				p.EllipseFn(0, cy, r*(1-wob), r*(1+wob)+0.3*k, func(d, a float64) (uint32, float64) {
					if d > 0.72 {
						if math.Cos(a) < 0 && math.Sin(a) < 0.3 {
							return C[5], 1
						}
						return C[3], 1
					}
					if d < 0.35 && math.Cos(a) > 0 {
						return C[6], 1
					}
					return C[4], 1
				})
				if r > 1 {
					p.Px(-r*0.4, cy-r*0.45, pixelfx.White)
				}
				p.Glow(0, cy, 4+r*2, C[5], 0.14+0.14*k)
			} else {
				// a little of it springs back up the tip after it lets go
				s := (t - dripLet) / 0.15
				if s < 1 {
					p.Ellipse(0, -H+0.8, 1.2*(1-s*0.6), 1+math.Sin(s*pi)*0.8, C[4], 1)
				}
			}
			// the fall, stretched by its speed
			f := t - dripLet
			if f > 0 && t < land {
				y, vy := -H+3+0.5*dripG*f*f, dripG*f
				bead(p, 0, y, 0, vy, 3, C, 1)
				p.Glow(0, y, 6, C[5], 0.2)
			}
			// the plink: a white fleck, a little crown thrown up, the near half of the ring, light
			u := t - land
			if u >= 0 {
				glint := 0.0
				if u < 0.5 {
					glint = 1 - u*2
				}
				ring(p, 0, 0, 2+17*pixelfx.Ease.OutCubic(u/0.75), dripSY, 1.1*(1-u/0.75), C, ringOpts{n: 2, gap: 2.5, half: 1, hot: u < 0.08, seed: 1, wob: 0.4, glint: glint})
				if u < 0.06 {
					p.Px(0, 0, pixelfx.White)
					p.Px(-1, 0, C[6])
					p.Px(1, 0, C[6])
					p.Glow(0, -1, 10, C[5], 0.5*(1-u/0.06))
				}
				// the crown: three flecks thrown out and back
				for _, cr := range dripCrown {
					vx, vh := cr[0], cr[1]
					h := vh*u - 0.5*dripG*u*u
					if h > 0 && u < 0.3 {
						c := C[6]
						if u < 0.1 {
							c = pixelfx.White
						}
						p.Px(vx*u, -h, c)
					}
				}
				// the rebound: a bead pops up out of the middle and drops back
				r := u - dripRebound
				h := 52*r - 0.5*dripG*0.5*r*r*2
				if r > 0 && h > 0 {
					p.Line(0, -1, 0, -h+1, C[5], 1)
					p.Circle(0, -h, 1.1, C[6], 1)
					p.Px(0, -h-1, pixelfx.White)
				}
				v := u - dripRebound - dripBack
				if v >= 0 {
					ring(p, 0, 0, 1.5+9*pixelfx.Ease.OutCubic(v/0.5), dripSY, 0.9*(1-v/0.5), C, ringOpts{n: 1, half: 1, seed: 2, hot: v < 0.06})
				}
				p.Glow(0, 0, 18, C[4], 0.22*(1-pixelfx.Clamp(u/0.6)))
			}
			// the tip and the drop shimmer in the puddle
			sh := 1.0
			if math.Sin(t*20) > 0 {
				sh = 0
			}
			p.Px(-1+sh, 2, C[4])
			if f > 0 && t < land {
				ry := 2 + 0.5*dripG*f*f*0.04
				p.Px(sh, math.Min(4, ry), C[5])
			}
			// the wet light under the tip and in the puddle
			p.Glow(0, -H+2, 8, C[5], 0.16)
			p.Glow(0, 0, 26, C[3], 0.14)
		},
	}
}
