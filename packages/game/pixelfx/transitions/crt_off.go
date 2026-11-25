// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/crt-off.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source lightning strike (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez, for the glowing line.

package transitions

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// CRTOff is a retro CRT power-off/on screen transition: the picture squeezes into a glowing line and lingering dot, then switches back on with a warm-up glow. It is registered as "transitions/crt-off".
var CRTOff = &pixelfx.Effect{
	Meta: screenMeta(
		"CRT off",
		"The picture squeezes into a white-hot glowing line, the line into a dot that lingers as a fading afterglow; then it switches back on, dot to line to picture, with a warm-up glow.",
		[]string{"crt", "tv", "retro", "switch", "wipe"},
		2.0,
		pixelfx.Option{Name: "palette", Default: pixelfx.Palette("#060a14", "#0e1a33", "#1a3360", "#2a5a9a", "#4a8fd0", "#8cc4f0", "#d4ecff", "#ffffff")},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(1)},
	),
	New: newCRTOff,
}

func init() { pixelfx.Register("transitions/crt-off", CRTOff) }

var (
	crtYC    = float64(H) / 2
	crtXC    = float64(W) / 2
	crtSq0   = 0.06
	crtSq1   = 0.34
	crtLn1   = 0.56
	crtDot1  = 0.86
	crtOn0   = 1.06
	crtOn1   = 1.16
	crtOpen1 = 1.42
)

func crtShape(t float64) (h, w float64) {
	if t < crtSq0 {
		return H, crtXC
	}
	if t < crtSq1 {
		return math.Max(1, H*(1-pixelfx.Ease.InCubic((t-crtSq0)/(crtSq1-crtSq0)))), crtXC
	}
	if t < crtLn1 {
		return 1, crtXC * (1 - pixelfx.Ease.InQuad((t-crtSq1)/(crtLn1-crtSq1)))
	}
	if t < crtOn0 {
		return 0, 0
	}
	if t < crtOn1 {
		return 1, crtXC * pixelfx.Ease.OutExpo((t-crtOn0)/(crtOn1-crtOn0))
	}
	return math.Max(1, H*pixelfx.Ease.OutCubic(pixelfx.Clamp((t-crtOn1)/(crtOpen1-crtOn1)))), crtXC
}

func newCRTOff(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	ink := o.Color("color")
	seed := o.Seed()

	return &pixelfx.Funcs{
		DrawFunc: func(p0 *pixelfx.Painter, t float64) {
			p := clipTo(p0)
			h, w := crtShape(t)
			fl := 0.85 + 0.15*pixelfx.Rand(math.Floor(t*30), 7, seed)
			if t >= crtOpen1+0.02 && t < crtOpen1+0.6 {
				u := (t - crtOpen1) / 0.6
				band := -20 + u*(H+40)
				for y := range H {
					a := 1 - u
					b := math.Max(0, 1-math.Abs(float64(y)-band)/10)
					scan := 0.0
					if y&1 != 0 {
						scan = 0.05 * a
					}
					k := 0.22*a*a + 0.3*b*b*a + scan
					if k > 0.01 {
						for x := range W {
							p0.Light(float64(x), float64(y), C[5], k)
						}
					}
				}
				return
			}
			if h >= H {
				return
			}
			top := int(pixelfx.Round(crtYC - h/2))
			bot := int(pixelfx.Round(crtYC + h/2))
			if top > 0 {
				rect(p0, 0, 0, W, float64(top), ink)
			}
			if bot < H {
				rect(p0, 0, float64(bot), W, float64(H-bot), ink)
			}
			if h > 2 {
				sq := 1 - h/H
				for y := top; y < bot; y++ {
					e := float64(min(y-top, bot-1-y))
					scan := 0.0
					if y&1 != 0 {
						scan = 0.08 * sq
					}
					em := math.Max(0, 1-e/6)
					k := 0.55*sq*sq + scan + 0.35*em*em
					for x := range W {
						p0.Light(float64(x), float64(y), C[5], k*fl)
					}
				}
				col := C[5]
				if sq > 0.6 {
					col = C[6]
				}
				for _, y := range [2]int{top - 1, bot} {
					if y >= 0 && y < H {
						rect(p0, 0, float64(y), W, 1, col)
					}
				}
				return
			}
			dotPhase := t >= crtLn1 && t < crtOn0
			x0, x1 := int(math.Floor(crtXC-w)), int(math.Ceil(crtXC+w))
			fresh := t < crtSq1+0.08 || (t > crtOn0 && t < crtOn1+0.04)
			if h > 0 && w >= 1 {
				fr := math.Floor(t * 30)
				for x := max(0, x0); x < min(W, x1); x++ {
					xf := float64(x)
					u := math.Abs(xf+0.5-crtXC) / w
					y := crtYC + pixelfx.Round(pixelfx.Noise(xf/23+t*9, seed)*(0.7+0.5*u))
					core := C[6]
					if u < 0.85 {
						core = pixelfx.White
					}
					p0.Px(xf, y, core)
					if u < 0.92 {
						p0.Px(xf, y-1, C[6])
						botCol := C[5]
						if u < 0.6 {
							botCol = C[6]
						}
						p0.Px(xf, y+1, botCol)
					}
					if fresh && u < 0.8 {
						p0.Dot(xf, y-2, C[4], 0.5)
						p0.Dot(xf, y+2, C[4], 0.5)
					}
					if pixelfx.Rand(xf, fr, seed+3) < 0.06 {
						sgn := 1.0
						if pixelfx.Rand(xf, fr, seed+4) < 0.5 {
							sgn = -1
						}
						dy := sgn * (2 + math.Floor(pixelfx.Rand(xf, fr, seed+5)*4))
						sc := C[5]
						if pixelfx.Rand(xf, fr, 6) < 0.5 {
							sc = pixelfx.White
						}
						p.Px(xf, y+dy, sc)
					}
					p0.Light(xf, y, C[6], 0.5*fl*(1-u*0.6))
				}
				for x := x0; x <= x1; x += 9 {
					p.Glow(float64(x), crtYC, 10, C[5], 0.35*fl)
				}
				p.Glow(crtXC, crtYC, math.Min(70, 16+w*0.4), C[5], 0.4*fl)
			}
			if dotPhase {
				u := (t - crtLn1) / (crtDot1 - crtLn1)
				if u < 0.35 {
					glint(p, crtXC, crtYC, 10*(1-u/0.35)+3, 0.2+u*2, C[6], C[5], 1)
				}
				if u < 1 {
					c0 := C[4]
					if u < 0.5 {
						c0 = pixelfx.White
					} else if u < 0.75 {
						c0 = C[6]
					}
					p.Px(crtXC, crtYC, c0)
					c1 := C[4]
					if u < 0.4 {
						c1 = C[6]
					}
					p.Px(crtXC-1, crtYC, c1)
					sm := int(pixelfx.Round(9 * (1 - u)))
					for k := 2; k <= sm; k++ {
						al := 1 - float64(k)/float64(sm+1)
						p.Dot(crtXC-float64(k), crtYC, C[4], al)
						p.Dot(crtXC+float64(k)-1, crtYC, C[4], al)
					}
					p.Glow(crtXC, crtYC, 22*(1-u*0.5), C[5], 0.55*math.Pow(1-u, 1.5))
				}
			}
			s := t - crtOn0
			if s >= 0 && s < 0.14 {
				glint(p, crtXC, crtYC, 12*(1-s/0.14)+2, 0.4+s*6, C[6], C[5], 1)
				p.Glow(crtXC, crtYC, 40, pixelfx.White, 0.5*(1-s/0.14))
			}
		},
	}
}
