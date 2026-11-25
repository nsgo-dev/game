// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/potion.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the bubbles (lit and shaded rims, a highlight, a dotted pop) from Sprite Fusion's open-source bubbles (https://www.spritefusion.com/pixel-art-vfx/bubbles), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Potion is a round glass flask of glowing brew rocking in the air: the brew sloshes behind the rock, bubbles rise and pop, light glows through the glass, wisps curl off the cork. It is registered as "pickups/potion".
var Potion = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Potion",
		Desc:     "A round glass flask of glowing brew rocks in the air: the brew sloshes behind the rock, bubbles rise and pop, light glows through the glass, wisps curl off the cork.",
		Tags:     []string{"pickup", "potion", "flask", "brew", "magic", "loop"},
		Size:     [2]int{56, 72},
		Origin:   [2]int{28, 64},
		Duration: potionDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0516", "#3d0a33", "#701560", "#b0228f", "#f53cd1", "#ff86e6", "#ffd2f6", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newPotion,
}

func init() { pixelfx.Register("pickups/potion", Potion) }

const (
	potionDuration = 2.4
	potionRB       = 7.0
	potionHover    = 17.0
	potionNeck     = 2.0
	potionLip      = -12.0
	potionCorkTop  = -16.0
)

var (
	potionInk   = pixelfx.Hex("#120d1a")
	potionGlass = pixelfx.Hex("#2a2236")
	potionCork  = pixelfx.Ramp("#2a160c", "#4e2c16", "#7a4a26", "#a8703a", "#d09a5a")
)

func potionPose(ph float64) (y, tilt float64) {
	return -potionHover - 1.4*math.Sin(ph*tau), 0.13 * math.Sin(ph*tau+0.4)
}

func potionSurface(x, ph float64) float64 {
	return -0.5 + 0.17*math.Sin(ph*tau-0.9)*x + 0.45*math.Sin(x*0.85+ph*tau*3)
}

func drawPotionFlask(L *layer, p *pixelfx.Painter, C []uint32, t, D, ph, cy, tilt, seed float64) {
	ct, st := math.Cos(tilt), math.Sin(tilt)
	L.begin(-20, math.Floor(cy)-22)
	L.fill(-20, math.Floor(cy)-22, 19, math.Floor(cy)+12, func(px, py float64) uint32 {
		wx, wy := px, py-cy
		fx, fy := wx*ct+wy*st, -wx*st+wy*ct
		d := pixelfx.Hypot(fx, fy)
		if fy < potionLip-0.5 && fy >= potionCorkTop && math.Abs(fx) <= potionNeck+0.2 {
			switch {
			case fy < potionCorkTop+1:
				return potionCork[4]
			case fx < -0.6:
				return potionCork[3]
			case fx > 1:
				return potionCork[1]
			default:
				return potionCork[2]
			}
		}
		if fy >= potionLip-0.5 && fy < potionLip+1 && math.Abs(fx) <= potionNeck+1.4 {
			if fx < 0 {
				return pixelfx.Mix(pixelfx.White, C[6], 0.4)
			}
			return pixelfx.Mix(potionGlass, C[5], 0.45)
		}
		inNeck := fy >= potionLip+1 && fy < -potionRB+2 && math.Abs(fx) <= potionNeck+0.6
		if d > potionRB && !inNeck {
			return 0
		}
		wall := d > potionRB-1
		if inNeck {
			wall = math.Abs(fx) > potionNeck-0.5
		}
		s := potionSurface(wx, ph)
		depth := wy - s
		if !wall && !inNeck && depth > 0 {
			core := 1 - pixelfx.Hypot(wx*0.85, wy-1.8)/potionRB
			if depth < 0.9 {
				return pick(wx < -1.5 && wx > -4.5, pixelfx.White, C[6])
			}
			var k int
			switch {
			case core > 0.62:
				k = 5
			case core > 0.38:
				k = 4
			case core > 0.18:
				k = 3
			default:
				k = 2
			}
			if depth < 1.9 && k < 5 {
				k++
			}
			if wx > 1.5 && wy > 2 {
				k--
			}
			return C[max(2, min(6, k))]
		}
		if wall {
			lit := (-fx*0.75 - fy*0.66) / math.Max(1, d)
			if inNeck {
				if fx < 0 {
					return pixelfx.Mix(potionGlass, C[6], 0.55)
				}
				return pixelfx.Mix(potionGlass, C[5], 0.3)
			}
			if lit > 0.82 {
				return pixelfx.Mix(pixelfx.White, C[6], 0.3)
			}
			if lit > 0.3 {
				return pixelfx.Mix(potionGlass, C[6], 0.5)
			}
			if depth > 0 {
				return C[3]
			}
			return pixelfx.Mix(potionGlass, C[5], 0.28)
		}
		return potionGlass
	})
	L.outline(potionInk).draw(p, 0, 0, 1)
	for a := 3.45; a < 4.35; a += 0.04 {
		lx, ly := math.Cos(a+tilt)*(potionRB-2.2), math.Sin(a+tilt)*(potionRB-2.2)
		col := pixelfx.Mix(pixelfx.White, C[6], 0.5)
		if a > 3.7 && a < 4.1 {
			col = pixelfx.White
		}
		p.Px(lx, cy+ly, col)
	}
	p.Px(pixelfx.Round(2*math.Cos(tilt)+3*math.Sin(tilt)), pixelfx.Round(cy-3), pixelfx.White)
	pixelfx.Cycle(t, D, 10, seed, func(g *pixelfx.Rng) float64 { return g.Range(0.7, 1.3) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		x0, r0, vy := r.Range(-4, 4), r.Range(0.6, 1.7), r.Range(7, 13)
		x := x0 + math.Sin(age*r.Range(5, 9)+r.Range(0, tau))*0.7
		y := cy + 5.5 - vy*age
		ph2 := (t + age) / D
		top := cy + potionSurface(x, ph2)
		if y > top+0.8 {
			rr := r0 * (0.7 + 0.5*(age/life))
			if pixelfx.Hypot(x, y-cy) > potionRB-1.5 {
				return
			}
			if rr < 0.95 {
				p.Px(x, y, C[6])
			} else {
				p.Px(x, y, C[3])
				p.Px(x+1, y, C[6])
				p.Px(x, y-1, pixelfx.White)
				p.Px(x+1, y-1, C[6])
				if rr > 1.4 {
					p.Px(x-1, y, C[5])
					p.Px(x, y+1, C[4])
				}
			}
			p.Glow(x+0.5, y, 3, C[6], 0.15)
		} else {
			k := pixelfx.Clamp((top + 0.8 - y) / 3)
			if k >= 1 {
				return
			}
			for a := pi; a < tau; a += 0.6 {
				p.Dot(x+math.Cos(a)*(1+k*2), top-0.5+math.Sin(a)*(1+k*2)*0.5, pick(k < 0.5, pixelfx.White, C[6]), 1.2-k)
			}
			p.Dot(x-1, top-1.5-k*3, C[6], 1-k)
			p.Dot(x+1.5, top-2-k*4, pixelfx.White, 1-k)
		}
	})
}

func newPotion(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	D := float64(potionDuration)
	L := newLayer(40, 44)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			y, _ := potionPose(ph)
			blobShadow(p, 0, 7.5, 0.8-(-y-potionHover+1.4)*0.06, 0)
			pool(p, 0, 0.5, 18, 3, C[5], 0.3)
			pixelfx.Cycle(t, D, 7, seed+1, func(g *pixelfx.Rng) float64 { return g.Range(1.2, 1.8) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				side := r.Sign()
				f := r.Range(3, 4.5)
				ph0 := r.Range(0, tau)
				vy := r.Range(11, 16)
				x0 := r.Range(-1, 1)
				at := func(a float64) (float64, float64) {
					return x0 + math.Sin(a*f+ph0)*(0.8+a*3.2)*side, y + potionCorkTop - 1 - a*vy
				}
				for i := range 9 {
					a := age - float64(i)*0.04
					if a < 0 {
						break
					}
					x, yy := at(a)
					k := (1 - a/life) * math.Min(1, a/0.15) * (1 - float64(i)/9)
					col := C[5]
					if i == 0 {
						col = pixelfx.White
					} else if i < 3 {
						col = C[6]
					}
					p.Dot(x, yy, col, k*1.4)
					if i == 0 {
						p.Glow(x, yy, 4, C[5], 0.3*k)
					}
				}
			})
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			y, tilt := potionPose(ph)
			p.Glow(0, y+1.5, 26, C[4], 0.22)
			drawPotionFlask(L, p, C, t, D, ph, y, tilt, seed)
			p.Glow(0, y+2, 10, C[5], 0.38+0.08*math.Sin(ph*tau*2))
			g := math.Mod(ph+0.2, 0.5) / 0.5
			if g < 0.16 {
				glint(p, -4, y-4, 2.6*math.Sin((g/0.16)*pi), pixelfx.White, 0.4)
			}
		},
	}
}
