// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/pixel-dissolve.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source sparkle (https://www.spritefusion.com/pixel-art-vfx/sparkle), MIT License, Copyright (c) 2026 Hugo Duprez, for the glints and the twinkling pixel dust.

package transitions

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// PixelDissolve is a blocky pixel-dissolve screen transition: clumps of blocks wink in with cooling neon outlines and glints, then collapse and sparkle away. It is registered as "transitions/pixel-dissolve".
var PixelDissolve = &pixelfx.Effect{
	Meta: screenMeta(
		"Pixel dissolve",
		"Clumps of blocks wink in, each drawn in a white outline that fills black and cools, with glints; then they wink out, outlines collapsing, the last small ones sparkling away.",
		[]string{"dissolve", "pixel", "blocks", "glint"},
		2.2,
		pixelfx.Option{Name: "palette", Default: pixelfx.RampOf(pixelfx.Ramps["neon"])},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(1)},
	),
	New: newPixelDissolve,
}

func init() { pixelfx.Register("transitions/pixel-dissolve", PixelDissolve) }

const dissolveCell = 20

var (
	dissolveP    = 0.17
	dissolveIn0  = 0.04
	dissolveIn1  = 0.8
	dissolveOut0 = 1.12
	dissolveOut1 = 1.85
)

type dissolveBlock struct {
	x, y, s         float64
	tin, tout       float64
	glint           bool
	gx, gy, spin, L float64
}

func dissolveLayout(seed float64) []dissolveBlock {
	var blocks []dissolveBlock
	for cy := range H / dissolveCell {
		for cx := range W / dissolveCell {
			h := pixelfx.Rand(float64(cx), float64(cy), seed)
			n := 4
			if h < 0.32 {
				n = 1
			} else if h < 0.74 {
				n = 2
			}
			s := float64(dissolveCell) / float64(n)
			for j := range n {
				for i := range n {
					blocks = append(blocks, dissolveBlock{
						x: float64(cx*dissolveCell) + float64(i)*s,
						y: float64(cy*dissolveCell) + float64(j)*s,
						s: s,
					})
				}
			}
		}
	}
	rank := func(key func(b *dissolveBlock) float64) []float32 {
		v := make([]float64, len(blocks))
		idx := make([]int, len(blocks))
		for i := range blocks {
			v[i] = key(&blocks[i])
			idx[i] = i
		}
		slices.SortStableFunc(idx, func(a, b int) int { return cmp.Compare(v[a], v[b]) })
		out := make([]float32, len(blocks))
		den := float64(len(blocks) - 1)
		for k, i := range idx {
			out[i] = float32(float64(k) / den)
		}
		return out
	}
	at := func(b *dissolveBlock, sd float64) float64 {
		return pixelfx.Fbm((b.x+b.s/2)/64, (b.y+b.s/2)/64, sd, 3)
	}
	uin := rank(func(b *dissolveBlock) float64 {
		bias := 0.0
		if b.s == 20 {
			bias = 0.12
		} else if b.s == 5 {
			bias = -0.08
		}
		return at(b, seed) + 0.22*pixelfx.Rand(b.x, b.y, seed+1) + bias
	})
	uout := rank(func(b *dissolveBlock) float64 {
		bias := 0.0
		if b.s == 20 {
			bias = -0.14
		} else if b.s == 5 {
			bias = 0.1
		}
		return at(b, seed+7) + 0.22*pixelfx.Rand(b.x, b.y, seed+2) + bias
	})
	for i := range blocks {
		b := &blocks[i]
		b.tin = dissolveIn0 + float64(uin[i])*(dissolveIn1-dissolveIn0)
		b.tout = dissolveOut0 + float64(uout[i])*(dissolveOut1-dissolveOut0)
		thresh := 0.1
		if b.s >= 10 {
			thresh = 0.32
		}
		b.glint = pixelfx.Rand(b.x, b.y, seed+3) < thresh
		gxOff := b.s
		if pixelfx.Rand(b.x, b.y, seed+4) < 0.5 {
			gxOff = 0
		}
		gyOff := b.s
		if pixelfx.Rand(b.x, b.y, seed+5) < 0.5 {
			gyOff = 0
		}
		b.gx = b.x + gxOff
		b.gy = b.y + gyOff
		b.spin = (pixelfx.Rand(b.x, b.y, seed+6) - 0.5) * 3
		if b.s == 20 && pixelfx.Rand(b.x, b.y, seed+8) < 0.45 {
			b.L = 9 + 6*pixelfx.Rand(b.x, b.y, seed+9)
		} else {
			b.L = 2.5 + b.s*0.32
		}
	}
	return blocks
}

func dissolveMote(p *pixelfx.Painter, x, y float64, c uint32, cross bool) {
	p.Px(x, y, c)
	if cross {
		for _, d := range [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			p.Px(x+d[0], y+d[1], c)
		}
	}
}

func dissolveSquare(p *pixelfx.Painter, x, y, s float64, fill, rim uint32) {
	X, Y := pixelfx.Round(x), pixelfx.Round(y)
	S := math.Max(1, pixelfx.Round(s))
	if fill != 0 {
		rect(p, X, Y, S, S, fill)
	}
	if rim != 0 && S > 2 {
		rect(p, X, Y, S, 1, rim)
		rect(p, X, Y+S-1, S, 1, rim)
		rect(p, X, Y, 1, S, rim)
		rect(p, X+S-1, Y, 1, S, rim)
	}
}

func newPixelDissolve(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	ink := o.Color("color")
	blocks := dissolveLayout(o.Seed())

	type dissolveGlint struct {
		b      *dissolveBlock
		u, dir float64
	}
	var glints []dissolveGlint
	star5 := [5][2]float64{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}}

	return &pixelfx.Funcs{
		DrawFunc: func(p0 *pixelfx.Painter, t float64) {
			p := clipTo(p0)
			glints = glints[:0]
			frame := math.Floor(t * 15)
			for n := range blocks {
				b := &blocks[n]
				ki := (t - b.tin) / dissolveP
				ko := (t - b.tout) / dissolveP
				x, y, s := b.x, b.y, b.s
				cx, cy := x+s/2, y+s/2
				if ki < 0 || ko >= 1.6 {
					continue
				}
				nf := float64(n)
				if ko < 0 {
					if ki >= 1 {
						dissolveSquare(p0, x, y, s, ink, 0)
					} else {
						g := 1.0
						if ki < 0.35 {
							g = 0.35 + 0.65*pixelfx.Ease.OutBack(ki/0.35)
						}
						h := s * g
						var rim uint32
						switch {
						case ki < 0.45:
							rim = pixelfx.White
						case ki < 0.6:
							rim = C[6]
						case ki < 0.75:
							rim = C[5]
						case ki < 0.9:
							rim = C[4]
						default:
							rim = C[3]
						}
						var fill uint32
						if ki > 0.2 {
							fill = ink
						}
						dissolveSquare(p, cx-h/2, cy-h/2, h, fill, rim)
						if ki < 0.3 && s >= 10 {
							for _, d := range star5 {
								col := pixelfx.White
								if d[0] != 0 || d[1] != 0 {
									col = C[6]
								}
								p.Px(cx-0.5+d[0], cy-0.5+d[1], col)
							}
						}
						p.Glow(cx, cy, s*0.8+4, C[5], 0.45*(1-ki))
					}
					if b.glint && ki > 0.15 && ki < 1.6 {
						glints = append(glints, dissolveGlint{b, (ki - 0.15) / 1.45, 1})
					}
					if ki < 1.4 && pixelfx.Rand(frame, nf, 9) < 0.35 {
						mx := x + pixelfx.Rand(frame, nf, 10)*(s+6) - 3
						my := y + pixelfx.Rand(frame, nf, 11)*(s+6) - 3
						mc := C[6]
						if pixelfx.Rand(frame, nf, 12) < 0.5 {
							mc = pixelfx.White
						}
						dissolveMote(p, mx, my, mc, pixelfx.Rand(frame, nf, 17) < 0.25)
					}
				} else {
					if ko < 0.35 {
						q := ko / 0.35
						rim := C[6]
						if q < 0.4 {
							rim = C[4]
						} else if q < 0.75 {
							rim = C[5]
						}
						dissolveSquare(p0, x, y, s, ink, rim)
						p.Glow(cx, cy, s*0.8+3, C[4], 0.35*q)
					} else if ko < 0.75 {
						q := (ko - 0.35) / 0.4
						h := s * (1 - pixelfx.Ease.InQuad(q)*0.85)
						rim := C[6]
						if q < 0.5 {
							rim = pixelfx.White
						}
						dissolveSquare(p, cx-h/2, cy-h/2, h, 0, rim)
						p.Glow(cx, cy, s*0.8+4, C[5], 0.45*(1-q*0.5))
					} else if ko < 1 {
						q := (ko - 0.75) / 0.25
						for _, d := range star5 {
							if (d[0] == 0 && d[1] == 0) || q < 0.5 {
								col := pixelfx.White
								if d[0] != 0 || d[1] != 0 {
									col = C[6]
								}
								p.Px(cx-0.5+d[0], cy-0.5+d[1], col)
							}
						}
						p.Glow(cx, cy, s*0.5+3, C[5], 0.35*(1-q))
					}
					if b.glint && ko > 0.3 {
						glints = append(glints, dissolveGlint{b, (ko - 0.3) / 1.3, -1})
					}
					if ko > 0.3 && ko < 1.5 && pixelfx.Rand(frame, nf, 13) < 0.3 {
						mx := x + pixelfx.Rand(frame, nf, 14)*(s+6) - 3
						my := y + pixelfx.Rand(frame, nf, 15)*(s+6) - 3
						mc := C[6]
						if pixelfx.Rand(frame, nf, 16) < 0.5 {
							mc = pixelfx.White
						}
						dissolveMote(p, mx, my, mc, pixelfx.Rand(frame, nf, 18) < 0.25)
					}
				}
			}
			for _, g := range glints {
				if g.u <= 0 || g.u >= 1 {
					continue
				}
				L := g.b.L * math.Pow(math.Sin(pi*g.u), 0.7)
				glint(p, g.b.gx, g.b.gy, L, g.b.spin*g.u*g.dir+0.4, C[5], C[4], 1)
				if g.b.L > 8 {
					p.Glow(g.b.gx, g.b.gy, 18+L, C[5], 0.25*math.Sin(pi*g.u))
				}
			}
		},
	}
}
