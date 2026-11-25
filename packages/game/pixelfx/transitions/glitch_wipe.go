// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/glitch-wipe.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source blink (https://www.spritefusion.com/pixel-art-vfx/movement), MIT License, Copyright (c) 2026 Hugo Duprez, for the specks of white, cyan and magenta.

package transitions

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// GlitchWipe is a digital glitch screen transition: torn bands of black stutter across the screen with RGB ghosts, data slivers and corrupted blocks, then tear on through. It is registered as "transitions/glitch-wipe".
var GlitchWipe = &pixelfx.Effect{
	Meta: screenMeta(
		"Glitch wipe",
		"Torn bands of black stutter across the screen, freezing and jerking back, their ragged edges split into magenta and cyan ghosts spitting data slivers; then they tear on through.",
		[]string{"glitch", "digital", "rgb", "bands", "wipe"},
		1.8,
		pixelfx.Option{Name: "palette", Default: pixelfx.RampOf(pixelfx.Ramps["cyber"])},
		pixelfx.Option{Name: "split", Default: pixelfx.Color("#ff2d6f")},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(1)},
	),
	New: newGlitchWipe,
}

func init() { pixelfx.Register("transitions/glitch-wipe", GlitchWipe) }

var (
	glitchTick = 1.0 / 18
	glitchIn0  = 0.03
	glitchIn1  = 0.78
	glitchOut0 = 0.98
	glitchOut1 = 1.66
)

type glitchBand struct {
	i, y, h, dir        int
	off, t0, t1, o0, o1 float64
}

func glitchBands(seed float64) []glitchBand {
	var out []glitchBand
	for y, i := 0, 0; y < H; i++ {
		fi := float64(i)
		r := pixelfx.Rand(fi, seed)
		var bh int
		switch {
		case r < 0.4:
			bh = 5 + int(math.Floor(pixelfx.Rand(fi, seed+1)*6))
		case r < 0.85:
			bh = 10 + int(math.Floor(pixelfx.Rand(fi, seed+1)*10))
		default:
			bh = 20 + int(math.Floor(pixelfx.Rand(fi, seed+1)*12))
		}
		h := min(H-y, bh)
		a := pixelfx.Clamp(0.5 + 0.55*pixelfx.Noise(float64(y)/40, seed+9) + (pixelfx.Rand(fi, seed+2)-0.5)*0.25)
		b := pixelfx.Clamp(0.5 + 0.55*pixelfx.Noise(float64(y)/40, seed+19) + (pixelfx.Rand(fi, seed+3)-0.5)*0.25)
		span := 0.32 + pixelfx.Rand(fi, seed+6)*0.12
		dir := -1
		if pixelfx.Rand(fi, seed+4) < 0.82 {
			dir = 1
		}
		t0 := glitchIn0 + a*(glitchIn1-glitchIn0-span)
		o0 := glitchOut0 + b*(glitchOut1-glitchOut0-span)
		out = append(out, glitchBand{
			i: i, y: y, h: h, dir: dir,
			off: pixelfx.Rand(fi, seed+5),
			t0:  t0, t1: t0 + span,
			o0: o0, o1: o0 + span,
		})
		y += h
	}
	return out
}

func glitchTear(b *glitchBand, t, t0, t1, salt float64) float64 {
	if t <= t0 {
		return 0
	}
	if t >= t1 {
		return 1
	}
	n := int(math.Floor((t + b.off*glitchTick) / glitchTick))
	for n > 0 && pixelfx.Rand(float64(n), float64(b.i), salt) < 0.22 {
		n--
	}
	tq := float64(n)*glitchTick - b.off*glitchTick
	u := pixelfx.Clamp((tq - t0) / (t1 - t0))
	return pixelfx.Clamp(pixelfx.Ease.InOutQuad(u) + (pixelfx.Rand(float64(n), float64(b.i), salt+1)-0.6)*0.12*math.Sin(pi*u))
}

func newGlitchWipe(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	ink := o.Color("color")
	RED := o.Color("split")
	B := glitchBands(o.Seed())
	pal4 := [4]uint32{pixelfx.White, C[6], C[5], RED}

	return &pixelfx.Funcs{
		DrawFunc: func(p0 *pixelfx.Painter, t float64) {
			p := clipTo(p0)
			fr := int(math.Floor(t / glitchTick))
			frf := float64(fr)
			out := t >= glitchOut0
			for bi := range B {
				b := &B[bi]
				bif := float64(b.i)
				var k float64
				if out {
					k = glitchTear(b, t, b.o0, b.o1, 7)
				} else {
					k = glitchTear(b, t, b.t0, b.t1, 3)
				}
				if (!out && k <= 0) || (out && k >= 1) {
					continue
				}
				moving := k > 0 && k < 1
				slip := 0.0
				if moving && pixelfx.Rand(frf, bif, 11) < 0.12 {
					slip = pixelfx.Round((pixelfx.Rand(frf, bif, 12) - 0.5) * 18)
				}
				fwd := b.dir
				var e float64
				if b.dir > 0 {
					e = k*W + slip
				} else {
					e = W - k*W + slip
				}
				behind := (fwd > 0) != out
				for y := b.y; y < b.y+b.h; y++ {
					jag := 0.0
					if moving {
						jag = pixelfx.Round((pixelfx.Rand(frf, float64(y), 14)-0.5)*6 + (pixelfx.Rand(float64(fr>>1), bif, 15)-0.5)*6)
					}
					ex := pixelfx.ClampTo(pixelfx.Round(e+jag), 0, W)
					yf := float64(y)
					if behind {
						if ex > 0 {
							rect(p0, 0, yf, ex, 1, ink)
						}
					} else if ex < W {
						rect(p0, ex, yf, W-ex, 1, ink)
					}
					if !moving {
						continue
					}
					e1 := ex
					side := -1.0
					if behind {
						e1 = ex - 1
						side = 1
					}
					sp := float64(2 + (b.i+fr)%3)
					p.Px(e1, yf, pixelfx.White)
					p.Px(e1+side*sp, yf, RED)
					p.Px(e1+side*(sp+1), yf, RED)
					p.Px(e1-side*(sp+1), yf, C[5])
					p.Px(e1-side*(sp+2), yf, C[4])
					p.Light(e1, yf, pixelfx.White, 0.45)
					p.Light(e1+side*(sp+1), yf, RED, 0.5)
					p.Light(e1+side*(sp+3), yf, RED, 0.22)
					p.Light(e1-side*(sp+1), yf, C[5], 0.45)
					p.Light(e1-side*(sp+3), yf, C[5], 0.22)
				}
				if !moving {
					continue
				}
				ex := pixelfx.ClampTo(pixelfx.Round(e), 0, W)
				open := -1.0
				if behind {
					open = 1
				}
				gc := C[5]
				if b.i&1 != 0 {
					gc = RED
				}
				p.Glow(ex, float64(b.y)+float64(b.h)/2, float64(b.h)*0.7+7, gc, 0.28)
				for s := range b.h {
					sf := float64(s)
					if pixelfx.Rand(frf, bif, 100+sf) > 0.55 {
						continue
					}
					r200 := pixelfx.Rand(frf, bif, 200+sf)
					x := ex + open*pixelfx.Round(r200*r200*22-4)
					y := float64(b.y) + math.Floor(pixelfx.Rand(frf, bif, 300+sf)*float64(b.h))
					p.Px(x, y, pal4[int(math.Floor(pixelfx.Rand(frf, bif, 400+sf)*4))])
				}
				for s := range 4 {
					sf := float64(s)
					if pixelfx.Rand(frf, bif, 20+sf) > 0.5 {
						continue
					}
					r30 := pixelfx.Rand(frf, bif, 30+sf)
					length := 2 + math.Floor(r30*r30*60)
					ahead := pixelfx.Rand(frf, bif, 40+sf) < 0.6
					d := math.Floor(math.Pow(pixelfx.Rand(frf, bif, 50+sf), 1.5)*70) + 4
					y := float64(b.y) + math.Floor(pixelfx.Rand(frf, bif, 60+sf)*float64(b.h))
					var x float64
					if ahead {
						if open > 0 {
							x = ex + d
						} else {
							x = ex - d - length
						}
					} else {
						if open > 0 {
							x = ex - d - length
						} else {
							x = ex + d
						}
					}
					c := pal4[int(math.Floor(pixelfx.Rand(frf, bif, 70+sf)*4))]
					p.Rect(x, y, length, 1, c, 1)
					for q := 0.0; q < length; q += 3 {
						p.Light(x+q, y, c, 0.35)
					}
				}
				if b.h >= 8 && pixelfx.Rand(frf, bif, 80) < 0.35 {
					bw := 6 + math.Floor(pixelfx.Rand(frf, bif, 81)*10)
					bh := 3 + math.Floor(pixelfx.Rand(frf, bif, 82)*math.Min(6, float64(b.h-3)))
					x := ex + open*math.Floor(pixelfx.Rand(frf, bif, 83)*24-4)
					if open < 0 {
						x -= bw
					}
					y := float64(b.y) + math.Floor(pixelfx.Rand(frf, bif, 84)*(float64(b.h)-bh))
					c := RED
					if pixelfx.Rand(frf, bif, 85) < 0.5 {
						c = C[5]
					}
					p.Rect(x, y, bw, 1, c, 1)
					p.Rect(x, y+bh-1, bw, 1, c, 1)
					p.Rect(x, y, 1, bh, c, 1)
					p.Rect(x+bw-1, y, 1, bh, c, 1)
					for q := 0.0; q < bw; q += 2 {
						p.Light(x+q, y, c, 0.25)
					}
				}
			}
			if t > glitchIn1 && t < glitchOut0+0.05 {
				for s := range 3 {
					sf := float64(s)
					if pixelfx.Rand(frf, sf, 90) > 0.4 {
						continue
					}
					y := math.Floor(pixelfx.Rand(frf, sf, 91) * H)
					x := math.Floor(pixelfx.Rand(frf, sf, 92) * W)
					r93 := pixelfx.Rand(frf, sf, 93)
					length := 3 + math.Floor(r93*r93*40)
					col := RED
					if s == 0 {
						col = C[4]
					} else if s == 1 {
						col = C[3]
					}
					p.Rect(x, y, length, 1, col, 1)
				}
			}
		},
	}
}
