// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/xp-orb.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the orb's shading and its glow from Sprite Fusion's open-source pickup orb (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// XPOrb is a small glowing orb racing a figure-eight, a comet tail tapering behind it along its path, flecks shed from the tail winking out, its light following it over the floor. It is registered as "pickups/xp-orb".
var XPOrb = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "XP orb",
		Desc:     "A small glowing orb races a figure-eight, a comet tail tapering behind it along its path, flecks shed from the tail winking out, its light following it over the floor.",
		Tags:     []string{"pickup", "xp", "orb", "experience", "energy", "loop"},
		Size:     [2]int{72, 56},
		Origin:   [2]int{36, 48},
		Duration: xpOrbDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0c1806", "#1e3a0a", "#36620e", "#5c9614", "#8cd024", "#c4f060", "#effcc0", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newXPOrb,
}

func init() { pixelfx.Register("pickups/xp-orb", XPOrb) }

const (
	xpOrbDuration = 1.8
	xpOrbA        = 16.0
	xpOrbB        = 9.0
	xpOrbHover    = 20.0
	xpOrbTail     = 0.48
)

var xpOrbInk = pixelfx.Hex("#0a1406")

func xpOrbAt(t float64) (float64, float64) {
	a := (t / xpOrbDuration) * tau
	return xpOrbA * math.Sin(a), -xpOrbHover + xpOrbB*math.Sin(a)*math.Cos(a) - 1.2*math.Cos(a*2)
}

func newXPOrb(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	D := float64(xpOrbDuration)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			x, _ := xpOrbAt(t)
			blobShadow(p, x, 3.5, 0.55, 0)
			pool(p, x, 0.5, 14, 3, C[5], 0.3)
			pixelfx.Cycle(t, D, 16, seed, func(g *pixelfx.Rng) float64 { return g.Range(0.35, 0.7) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				fx, fy := xpOrbAt(t - age)
				k := 1 - age/life
				tw := math.Sin(age*r.Range(20, 30) + r.Range(0, tau))
				x2 := fx + r.Range(-2, 2) + age*r.Range(-6, 6)
				y2 := fy + r.Range(-2, 2) + age*r.Range(4, 10)
				if tw*k > -0.2 {
					col := C[5]
					if tw > 0.6 && k > 0.5 {
						col = pixelfx.White
					} else if k > 0.5 {
						col = C[6]
					}
					p.Px(x2, y2, col)
				}
				p.Glow(x2+0.5, y2+0.5, 3, C[5], 0.3*k)
			})
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			x, y := xpOrbAt(t)
			beat := 0.5 + 0.5*math.Sin((t/D)*tau*3)
			const n = 60
			for i := n; i >= 1; i-- {
				u := float64(i) / n
				tx, ty := xpOrbAt(t - u*xpOrbTail)
				r := 3.1 * math.Pow(1-u, 1.1)
				col := C[3]
				if u < 0.25 {
					col = C[5]
				} else if u < 0.55 {
					col = C[4]
				}
				if r > 0.6 {
					a := (1 - u) * 3.3
					if u < 0.7 {
						a = 1
					}
					p.Circle(tx, ty, r, col, a)
				} else {
					p.Dot(tx, ty, col, (1-u)*3)
				}
				if i%6 == 0 {
					p.Glow(tx, ty, 7*(1-u)+2, C[5], 0.24*(1-u))
				}
			}
			for i := int(math.Floor(n * 0.6)); i >= 1; i-- {
				u := float64(i) / n
				tx, ty := xpOrbAt(t - u*xpOrbTail)
				p.Dot(tx, ty, pick(u < 0.2, pixelfx.White, C[6]), (0.6-u)*4)
			}
			X, Y := pixelfx.Round(x), pixelfx.Round(y)
			p.Circle(X, Y, 4.4, xpOrbInk, 1)
			p.CircleFn(X, Y, 3.6, func(d, a float64) (uint32, float64) {
				if d > 0.72 {
					return pick(math.Cos(a+2.4) > 0.3, C[5], C[3]), 1
				}
				if d > 0.4 {
					return pick(math.Cos(a+2.4) > -0.2, C[6], C[5]), 1
				}
				return pixelfx.White, 1
			})
			p.Px(X-2, Y-2, pixelfx.White)
			p.Px(X-1, Y-2, C[7])
			p.Glow(x, y, 16+3*beat, C[5], 0.36+0.14*beat)
			g := math.Mod((t/D)*3, 1)
			if g < 0.12 {
				glint(p, X-2, Y-2, 2.4*math.Sin((g/0.12)*pi), pixelfx.White, 0.4)
			}
		},
	}
}
