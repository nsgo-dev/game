// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/heart-pickup.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the hover, floor shadow and glints from Sprite Fusion's open-source pickup (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// HeartPickup is a plump glossy heart hovering over its shadow that beats lub-dub: a big squash and stretch, a smaller echo, light and a thin ring pulsing out, tiny hearts drifting off. It is registered as "pickups/heart-pickup".
var HeartPickup = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Heart pickup",
		Desc:     "A plump glossy heart hovering over its shadow beats lub-dub: a big squash and stretch, a smaller echo, light and a thin ring pulsing out, tiny hearts drifting off.",
		Tags:     []string{"pickup", "heart", "health", "life", "loop"},
		Size:     [2]int{56, 60},
		Origin:   [2]int{28, 52},
		Duration: heartPickupDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0408", "#4a0a14", "#8a1222", "#d02436", "#ff4a56", "#ff8a8e", "#ffd0cc", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newHeartPickup,
}

func init() { pixelfx.Register("pickups/heart-pickup", HeartPickup) }

const (
	heartPickupDuration = 1.2
	heartHover          = 18.0
	heartW              = 7.2
	heartLub            = 0.04
	heartDub            = 0.24
)

var (
	heartInk  = pixelfx.Hex("#12060c")
	heartMini = []string{".a.a.", "abbba", ".aba.", "..a.."}
)

func heartBeat(ph, at, s float64) float64 {
	u := ph - at
	if u < 0 || u > 0.3 {
		return 0
	}
	return s * math.Exp(-u*16) * math.Cos(u*34)
}

func heartPulse(ph float64) float64 {
	return heartBeat(ph, heartLub, 1) + heartBeat(ph, heartDub, 0.55) + heartBeat(ph+1, heartLub, 1)
}

func newHeartPickup(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	D := float64(heartPickupDuration)
	L := newLayer(40, 40)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			y := -heartHover - 1.2*math.Sin(ph*tau)
			blobShadow(p, 0, 8+1.5*heartPulse(ph), 0.8, 0)
			pixelfx.Cycle(t, D, 4, seed, pixelfx.Fixed(0.95), func(age, life float64, r *pixelfx.Rng, i int) {
				side := pick(i%2 != 0, 1.0, -1.0)
				k := age / life
				x := side*(r.Range(2, 5)+age*r.Range(9, 15)) + math.Sin(age*7+r.Range(0, tau))*1.5
				yy := y - 5 - age*r.Range(14, 19)
				a := math.Min(1, age/0.12) * (1 - k*k)
				pal := map[rune]uint32{
					'a': pick(k < 0.5, C[5], C[4]),
					'b': pick(k < 0.3, pixelfx.White, C[6]),
				}
				p.Sprite(heartMini, pal, pixelfx.Round(x-2.5), pixelfx.Round(yy-2), a*1.3, pixelfx.SpriteOpts{})
				p.Glow(x, yy, 5, C[4], 0.3*a)
			})
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			y := -heartHover - 1.2*math.Sin(ph*tau)
			b := heartPulse(ph)
			sx, sy, cy := 1+0.16*b, 1-0.13*b, pixelfx.Round(y)
			p.Glow(0, cy, 22+6*math.Max(0, b), C[4], 0.26+0.3*math.Max(0, b))
			L.begin(-20, cy-20)
			pillow(L, -12, cy-11, 12, cy+11, func(x, yy float64) float64 {
				return heartSdf(x, yy, 0, cy, heartW*sx, heartW*sy)
			}, func(lt, e, _, _ float64) uint32 {
				if lt > 0.92 && e > 1.2 {
					return pixelfx.White
				}
				switch {
				case lt > 0.8:
					return C[6]
				case lt > 0.6:
					return C[5]
				case lt > 0.38:
					return C[4]
				case lt > 0.12:
					return C[3]
				default:
					return C[2]
				}
			}, 3.6)
			L.outline(heartInk).draw(p, 0, 0, 1)
			u := pixelfx.Wrap(ph-heartLub, 1)
			if u < 0.28 {
				k := u / 0.28
				r := 9 + 8*(1-math.Pow(1-k, 3))
				p.Ring(0, cy, r, 1, pick(k < 0.2, C[6], C[5]), 0.85*(1-k))
				p.GlowRing(0, cy, r, 4, C[4], 0.25*(1-k))
			}
			g := pixelfx.Wrap(ph-heartLub-0.08, 0.5)
			if g < 0.12 {
				glint(p, -3.5, cy-4, 2.6*math.Sin((g/0.12)*pi), pixelfx.White, 0.45)
			}
		},
	}
}
