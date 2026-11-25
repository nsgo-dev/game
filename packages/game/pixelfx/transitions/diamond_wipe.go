// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/diamond-wipe.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source ice crystals (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez, for the faceted shading.

package transitions

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// DiamondWipe is a faceted-diamond lattice screen transition: a slanted wave of diamonds swells across the screen, cools to black as they lock, then rises and shrinks away with glints. It is registered as "transitions/diamond-wipe".
var DiamondWipe = &pixelfx.Effect{
	Meta: screenMeta(
		"Diamond wipe",
		"A wave of faceted diamonds swells across the screen, rims bright and ridges lit, cooling to black as they lock; then they rise as gems again and shrink away with glints.",
		[]string{"diamond", "lattice", "geometric", "wipe"},
		2.0,
		pixelfx.Option{Name: "palette", Default: pixelfx.RampOf(pixelfx.Ramps["arcane"])},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(1)},
	),
	New: newDiamondWipe,
}

func init() { pixelfx.Register("transitions/diamond-wipe", DiamondWipe) }

var (
	diamondS    = 32.0
	diamondHD   = diamondS/2 + 1
	diamondGrow = 0.24
	diamondIn0  = 0.04
	diamondIn1  = 0.66
	diamondOut0 = 1.05
	diamondOut1 = 1.56
)

type diamondCell struct {
	cx, cy, tin, tout, spin float64
	glint                   bool
}

func diamondLattice(seed float64) []diamondCell {
	var out []diamondCell
	jMax := int(math.Ceil(H/(diamondS/2))) + 1
	iMax := int(math.Ceil(W/diamondS)) + 1
	for j := -1; j <= jMax; j++ {
		for i := -1; i <= iMax; i++ {
			off := 0.0
			if j&1 != 0 {
				off = diamondS / 2
			}
			cx := float64(i)*diamondS + off
			cy := (float64(j) * diamondS) / 2
			if cx+diamondHD < 0 || cy+diamondHD < 0 || cx-diamondHD > W || cy-diamondHD > H {
				continue
			}
			u := pixelfx.Clamp((cx + 0.45*cy) / (W + 0.45*H))
			jr := pixelfx.Rand(float64(i), float64(j), seed)
			out = append(out, diamondCell{
				cx:    cx,
				cy:    cy,
				tin:   diamondIn0 + (diamondIn1-diamondIn0)*pixelfx.Clamp(u+(jr-0.5)*0.08),
				tout:  diamondOut0 + (diamondOut1-diamondOut0)*pixelfx.Clamp(u+(pixelfx.Rand(float64(i), float64(j), seed+1)-0.5)*0.08),
				glint: jr < 0.3,
				spin:  pixelfx.Rand(float64(i), float64(j), seed+2) * 3,
			})
		}
	}
	return out
}

func newDiamondWipe(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	ink := o.Color("color")
	L := diamondLattice(o.Seed())
	FACE := [4][3]uint32{
		{C[2], C[1], C[0]},
		{C[1], C[0], C[0]},
		{C[1], C[0], C[0]},
		{C[0], C[0], ink},
	}

	type diamondGlint struct {
		d     *diamondCell
		u, hd float64
	}
	var glints []diamondGlint

	return &pixelfx.Funcs{
		DrawFunc: func(p0 *pixelfx.Painter, t float64) {
			p := clipTo(p0)
			glints = glints[:0]
			for di := range L {
				d := &L[di]
				ki := (t - d.tin) / diamondGrow
				ko := (t - d.tout) / diamondGrow
				if d.glint && ko > 0.7 && ko < 1.5 {
					glints = append(glints, diamondGlint{d, (ko - 0.7) / 0.8, 0})
				}
				if ki <= 0 || ko >= 1 {
					continue
				}
				var hd, heat float64
				if ko > 0 {
					hd = diamondHD * (1 - pixelfx.InBack(ko, 2))
					heat = pixelfx.Smooth(0, 0.35, ko)
				} else {
					hd = diamondHD * pixelfx.OutBack(math.Min(1, ki), 2.2)
					heat = 1 - pixelfx.Smooth(0.45, 1.25, ki)
				}
				if hd < 0.5 {
					continue
				}
				settled := heat <= 0
				y0 := int(math.Max(0, math.Floor(d.cy-hd)))
				y1 := int(math.Min(H-1, math.Ceil(d.cy+hd)))
				for y := y0; y <= y1; y++ {
					dy := float64(y) + 0.5 - d.cy
					w := hd - math.Abs(dy)
					if w <= 0 {
						continue
					}
					x0 := int(math.Max(0, math.Ceil(d.cx-w-0.5)))
					x1 := int(math.Min(W-1, math.Floor(d.cx+w-0.5)))
					if x1 < x0 {
						continue
					}
					if settled {
						rect(p0, float64(x0), float64(y), float64(x1-x0+1), 1, ink)
						continue
					}
					st := 2
					if heat > 0.66 {
						st = 0
					} else if heat > 0.33 {
						st = 1
					}
					for x := x0; x <= x1; x++ {
						dx := float64(x) + 0.5 - d.cx
						e := hd - math.Abs(dx) - math.Abs(dy)
						q := 0
						if dy >= 0 {
							q += 2
						}
						if dx >= 0 {
							q += 1
						}
						vr := dx > -1 && dx <= 0
						hr := dy > -1 && dy <= 0
						var c uint32
						if e < 1 {
							var rim [4]uint32
							if heat > 0.5 {
								rim = [4]uint32{pixelfx.White, C[6], C[5], C[4]}
							} else {
								rim = [4]uint32{C[6], C[5], C[4], C[3]}
							}
							if heat > 0.15 {
								c = rim[q]
								qm := 0.6
								if q == 0 {
									qm = 1
								}
								p0.Light(float64(x), float64(y), C[5], 0.4*heat*qm)
							} else {
								c = ink
							}
						} else if e < 2 && q == 0 && heat > 0.4 {
							c = C[5]
						} else if (vr || hr) && heat > 0.3 {
							tip := vr && dy < -hd+4
							sheen := math.Abs(pixelfx.Wrap(float64(x+y)-t*260, 90)-45) < 3
							switch {
							case tip || (sheen && heat > 0.5):
								c = pixelfx.White
							case vr && dy < 0:
								if heat > 0.6 {
									c = C[6]
								} else {
									c = C[5]
								}
							case heat > 0.6:
								c = C[3]
							default:
								c = C[2]
							}
						} else if heat > 0.15 {
							c = FACE[q][st]
						} else {
							c = ink
						}
						p0.Px(float64(x), float64(y), c)
					}
				}
				if !settled {
					p.Glow(d.cx, d.cy, hd+6, C[4], 0.22*heat)
				}
				if ki > 0.6 && ki < 1.8 && d.glint {
					glints = append(glints, diamondGlint{d, (ki - 0.6) / 1.2, hd})
				}
			}
			for _, g := range glints {
				if g.u <= 0 || g.u >= 1 {
					continue
				}
				y := g.d.cy
				if g.hd != 0 {
					y = g.d.cy - g.hd
				}
				glint(p, g.d.cx, y, 9*math.Pow(math.Sin(pi*g.u), 0.7), g.d.spin+g.u*1.5, C[6], C[5], 1)
			}
		},
	}
}
