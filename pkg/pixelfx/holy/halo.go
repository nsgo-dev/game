// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/halo.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var haloPray = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.1), pixelfx.FX.Is(-1), pixelfx.FY.Is(-26), pixelfx.BX.Is(2), pixelfx.BY.Is(-26), pixelfx.ArmsFront.Is(1), pixelfx.HY.Is(1), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))

// Halo is a ring of gold light floating over the head with a glint running round it; fine dust sifts down from it and the head catches its light. It is registered as "holy/halo".
var Halo = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Halo",
		Desc:     "A ring of gold light floats over the head with a glint running round it; fine dust sifts down from it and the head catches its light.",
		Tags:     []string{"holy", "halo", "saint", "buff", "glow", "character", "loop"},
		Size:     [2]int{56, 80},
		Origin:   [2]int{28, 70},
		Duration: haloT,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: goldPalette()},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: haloPray},
			{T: 0.5, Pose: haloPray.With(pixelfx.Crouch.Is(1))},
			{T: 1, Pose: haloPray.With(pixelfx.Crouch.Is(1), pixelfx.HY.Is(2))},
			{T: 1.5, Pose: haloPray.With(pixelfx.Crouch.Is(1))},
			{T: 2, Pose: haloPray},
		},
	},
	New: newHalo,
}

func init() { pixelfx.Register("holy/halo", Halo) }

const haloT = 2

var haloHY, haloRX, haloRY = -46.0, 9.5, 2.6

func newHalo(o *pixelfx.Options) pixelfx.Instance {
	C, seed, A := pixelfx.Fit(o.Palette(), 8), o.Seed(), o.Actor
	HY, RX, RY := haloHY, haloRX, haloRY
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		T := float64(haloT)
		ph := (t / T) * tau
		y0 := HY
		if math.Sin(ph) > 0.55 {
			y0 = HY - 1
		}
		gl := pi/2 + ph
		// the hair under it catches the light
		if A != nil && len(A.Pixels) > 0 {
			// a warm rim on the top edge of each column of the head, and light falling off below it
			m := A.Mask
			for i := range m.W {
				for j := 0; j < min(m.H, 9); j++ {
					c := A.Pixels[j*m.W+i]
					// skip empty space and the dark outline, light the first pixel of hair under it
					if c == 0 || pixelfx.Luma(c) < 0.15 {
						continue
					}
					x, y := A.X+float64(i), A.Y+float64(j)
					p.Px(x, y, pixelfx.Mix(c, C[6], 0.45))
					if (j+1)*m.W+i < len(A.Pixels) {
						if c2 := A.Pixels[(j+1)*m.W+i]; c2 != 0 && pixelfx.Luma(c2) >= 0.15 {
							p.Px(x, y+1, pixelfx.Mix(c2, C[5], 0.18))
						}
					}
					for d := 0; d <= 4; d++ {
						p.Light(x, y+float64(d), C[4], 0.3*(1-float64(d)/5))
					}
					break
				}
			}
		}
		// the ring: a thick bright near side, a thin far side, a slow shimmer running round, the glint
		const n = 92
		for j := range n {
			g := (float64(j) / n) * tau
			x, y, isNear := math.Cos(g)*RX, y0+math.Sin(g)*RY, math.Sin(g) >= 0
			shimmer, hot := math.Sin(g*3-ph*2) > 0.7, math.Abs(wrapPi(g-gl)) < 0.3
			if isNear {
				c1, c2 := C[6], C[4]
				if hot || shimmer {
					c1 = pixelfx.White
				}
				if hot {
					c2 = C[6]
				}
				p.Px(x, y, c1)
				p.Px(x, y+1, c2)
			} else {
				c := C[5]
				if hot {
					c = pixelfx.White
				} else if shimmer {
					c = C[6]
				}
				p.Dot(x, y, c, 0.9)
			}
		}
		p.Glow(0, y0, 20, C[4], 0.4)
		p.Glow(0, y0+1, 9, pixelfx.White, 0.16)
		gx, gy := math.Cos(gl)*RX, y0+math.Sin(gl)*RY
		if math.Sin(gl) > -0.2 {
			pixelfx.Sparkle(p, gx, gy+0.5, 1.5+2.5*math.Max(0, math.Sin(gl)), pixelfx.White, C[6], 0.45)
		} else {
			p.Glow(gx, gy, 5, C[6], 0.3)
		}
		// light falls from it onto the head
		p.GlowLine(0, y0+2, 0, y0+9, 9, C[4], 0.16)
		// little stars wink on and off about the ring
		pixelfx.Cycle(t, T, 6, seed+3, func(g *pixelfx.Rng) float64 { return g.Range(0.3, 0.5) }, func(age, life float64, R *pixelfx.Rng, _ int) {
			g, d, k := R.Range(0, tau), R.Range(1.1, 1.5), math.Sin((age/life)*pi)
			pixelfx.Sparkle(p, math.Cos(g)*RX*d, y0+math.Sin(g)*RY*d-R.Range(1, 4), 1+2*k, pixelfx.White, C[6], 0.4*k)
		})
		// dust sifting down off the ring
		pixelfx.Cycle(t, T, 16, seed, func(g *pixelfx.Rng) float64 { return g.Range(0.8, 1.5) }, func(age, life float64, R *pixelfx.Rng, _ int) {
			g, u := R.Range(0, tau), age/life
			x := math.Cos(g)*RX*R.Range(0.8, 1) + math.Sin(u*5+g)*1.2
			y := y0 + math.Sin(g)*RY + 1 + u*R.Range(7, 14)
			tw := 0.6 + 0.4*math.Sin(age*18+g*7)
			k := math.Sin(u*pi) * tw
			c := C[6]
			if tw > 0.85 {
				c = pixelfx.White
			}
			p.Dot(x, y, c, math.Min(1, k*1.5))
			p.Light(x, y, C[5], 0.5*k)
		})
	}}
}
