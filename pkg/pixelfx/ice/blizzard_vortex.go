// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/blizzard-vortex.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// Particles and bands that pass behind and in front of a centre (dim far half drawn back, bright near half drawn front) follow the recipe in Sprite Fusion's open-source black hole (https://www.spritefusion.com/pixel-art-vfx/black-hole), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// BlizzardVortex is a swirling snow tornado: wind bands spin up a leaning funnel, flakes and ice crystals orbit in front and behind, snow boils at its foot and cold light fills its throat. It is registered as "ice/blizzard-vortex".
var BlizzardVortex = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Blizzard vortex",
		Desc:     "A swirling snow tornado: wind bands spin up a leaning funnel, flakes and ice crystals orbit in front and behind, snow boils at its foot and cold light fills its throat.",
		Tags:     []string{"ice", "snow", "blizzard", "tornado", "vortex", "wind", "loop"},
		Size:     [2]int{128, 116},
		Origin:   [2]int{64, 102},
		Duration: vortexDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "height", Default: pixelfx.Num(80)},
			{Name: "seed", Default: pixelfx.Num(17)},
		},
	},
	New: newBlizzardVortex,
}

func init() { pixelfx.Register("ice/blizzard-vortex", BlizzardVortex) }

const vortexDuration = 2

// vortexTilt is how flat the funnel's rings look.
var vortexTilt = 0.24

// vortexRadius is the funnel's radius at height h of H: a narrow foot, a wide mouth.
func vortexRadius(h, H float64) float64 { return 4 + 28*math.Pow(pixelfx.Clamp(h/H), 1.35) }

// vortexAxis is where the funnel's axis is at height h of H: it sways, one full sway a loop.
func vortexAxis(h, H, ph float64) float64 {
	return (5*math.Sin(tau*ph+h*0.05)+1.5*math.Sin(tau*ph*2-h*0.11))*(h/H) - 2*(h/H)
}

// vortexStreaks draws wind streaks: arcs of snow-laden air swirling round the funnel and climbing it, a bright head and a fading tail, faster low down; the far half of each arc goes behind, the near half in front.
func vortexStreaks(p *pixelfx.Painter, C []uint32, t, H, seed float64, front bool) {
	ph := t / vortexDuration
	pixelfx.Cycle(t, vortexDuration, 58, seed+9, func(g *pixelfx.Rng) float64 { return g.Range(0.5, 0.9) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		h0 := r.Range(2, H)
		w := r.Range(8, 12) * (1.45 - h0/H)
		th := r.Range(0, tau) + age*w
		k := age / life
		fade := math.Min(1, age/0.08) * pick(k > 0.7, (1-k)/0.3, 1)
		L := r.Range(0.7, 1.5)
		m := math.Ceil(L / 0.07)
		for j := 0.0; j <= m; j++ {
			a := th - j*0.07
			s := math.Sin(a)
			if (s >= 0) != front {
				continue
			}
			h := math.Min(H+4, h0+age*16-j*0.25)
			R := vortexRadius(math.Min(h, H), H) * (1 + math.Min(0.3, math.Max(0, h-H*0.84)*0.03))
			u := j / m
			x, y := vortexAxis(math.Min(h, H), H, ph)+math.Cos(a)*R, -h+s*R*vortexTilt
			var c uint32
			if front {
				c = pick(u < 0.15, pixelfx.White, pick(u < 0.5, C[6], C[5]))
			} else {
				c = pick(u < 0.3, C[5], pick(u < 0.6, C[4], C[3]))
			}
			p.Dot(x, y, c, fade*pick(u < 0.6, 1, 1.5-u*1.2)*pick(front, 1, 0.85))
		}
	})
}

// vortexBody draws the body: a translucent column of snow-filled air, densest at the edges where you look through more of it.
func vortexBody(p *pixelfx.Painter, C []uint32, ph, H float64) {
	p.Each(-44, -H-6, 44, 2, func(x, y float64, sx, sy int) {
		h := -y
		if h < 0 || h > H+4 {
			return
		}
		R := vortexRadius(h, H)
		dx := x - vortexAxis(h, H, ph)
		u := dx / R
		if u*u >= 1 {
			return
		}
		k, rim := h/H, math.Pow(math.Abs(u), 3)
		swirl := math.Sin(math.Asin(u)*3-tau*ph*2+h*0.2)*0.5 + 0.5
		a := (0.07 + 0.24*rim + 0.12*swirl) * pixelfx.Clamp((1-math.Abs(u))/0.18) * pick(k > 0.7, math.Max(0, (1-k)/0.3), 1) * pick(h < 4, h/4, 1)
		if a < 0.03 {
			return
		}
		p.BlendPx(sx, sy, pick(rim > 0.5, C[6], pick(swirl > 0.6, C[5], C[4])), a)
		if rim > 0.3 || swirl > 0.75 {
			p.Add(sx, sy, C[5], 0.16*math.Max(rim, swirl-0.5)*(1-k*0.5))
		}
	})
}

// vortexOrbiters draws the snow and ice carried round: rising on a spiral, faster low down, flung out at the top.
func vortexOrbiters(p *pixelfx.Painter, C []uint32, t, H, seed float64, front bool) {
	ph := t / vortexDuration
	pixelfx.Cycle(t, vortexDuration, 76, seed, func(g *pixelfx.Rng) float64 { return g.Range(0.9, 1.6) }, func(age, life float64, r *pixelfx.Rng, i int) {
		h0 := r.Range(0, H*0.6)
		h := h0 + age*r.Range(14, 30)
		k := age / life
		if h > H+8 {
			return
		}
		w := r.Range(7, 11) * (1.5 - h/H)
		th := r.Range(0, tau) + age*w
		out := 1 + r.Range(0.05, 0.3) + math.Min(0.35, math.Max(0, h-H*0.82)*0.04)
		R, s := vortexRadius(math.Min(h, H), H)*out, math.Sin(th)
		if (s >= 0) != front {
			return
		}
		x, y, big := vortexAxis(math.Min(h, H), H, ph)+math.Cos(th)*R, -h+s*R*vortexTilt, i%5 == 0
		fade := math.Min(1, age/0.1) * pick(k > 0.8, (1-k)/0.2, 1)
		if fade <= 0 {
			return
		}
		// a streak back along its path, a bright head
		th2 := th - 0.25
		x2, y2 := vortexAxis(math.Min(h, H), H, ph)+math.Cos(th2)*R, -h+2+math.Sin(th2)*R*vortexTilt
		if front {
			p.LineFn(x2, y2, x, y, func(u float64) (uint32, float64) { return pick(u > 0.6, C[6], C[4]), u * fade })
			c := pixelfx.White
			if !big && !r.Chance(0.5) {
				c = C[6]
			}
			p.Dot(x, y, c, fade)
			if big {
				for _, d := range cross4 {
					p.Dot(x+d[0], y+d[1], C[6], fade)
				}
				p.Glow(x, y, 5, C[5], 0.35*fade)
			}
		} else {
			p.LineFn(x2, y2, x, y, func(u float64) (uint32, float64) { return C[3], u * fade * 0.8 })
			p.Dot(x, y, pick(big, C[5], C[4]), fade)
		}
	})
}

// vortexFoot draws the snow boiling at the foot: soft puffs spinning round the base, lit on top.
func vortexFoot(p *pixelfx.Painter, C []uint32, t, seed float64, front bool) {
	ph := t / vortexDuration
	pixelfx.Cycle(t, vortexDuration, 12, seed+5, func(g *pixelfx.Rng) float64 { return g.Range(0.8, 1.2) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		th := r.Range(0, tau) + age*r.Range(4, 6)
		R := 9 + age*r.Range(8, 16)
		s, k := math.Sin(th), age/life
		if (s >= 0) != front {
			return
		}
		x := vortexAxis(0, 1, ph) + math.Cos(th)*R
		y := -2 - age*r.Range(4, 10) + s*R*0.2
		rad, a := 2+4*k, 0.5*math.Pow(1-k, 1.2)*math.Min(1, age/0.1)
		c := pick(front, C[6], C[5])
		p.DiscFn(x, y, rad, func(d, _ float64) (uint32, float64) { return c, a * (1 - d*d) })
		p.DiscFn(x-rad*0.25, y-rad*0.3, rad*0.5, func(d, _ float64) (uint32, float64) { return pixelfx.White, a * 0.5 * (1 - d*d) * (1 - k) })
	})
}

func newBlizzardVortex(o *pixelfx.Options) pixelfx.Instance {
	C, H, seed := pixelfx.Fit(o.Palette(), 8), o.Num("height"), o.Seed()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			// a snow drift on the floor, swept round
			for x := -30.0; x <= 30; x++ {
				d := math.Abs(x) / 30
				hgt := pixelfx.Round((1 - d*d) * (1.5 + pixelfx.Rand(x, 3)*1.5))
				for j := 0.0; j < hgt; j++ {
					p.Px(x, -j, pick(j == hgt-1, pixelfx.White, C[5]))
				}
				p.Dot(x, 0, C[6], 1-d*0.5)
				p.Dot(x, 1, C[4], 0.8-d*0.6)
			}
			p.Glow(0, -2, 40, C[4], 0.22)
			vortexFoot(p, C, t, seed, false)
			vortexStreaks(p, C, t, H, seed, false)
			vortexOrbiters(p, C, t, H, seed, false)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / vortexDuration
			vortexBody(p, C, ph, H)
			// cold light in its throat
			p.Glow(vortexAxis(H*0.45, H, ph), -H*0.45, 40, C[4], 0.3)
			p.Glow(vortexAxis(H*0.15, H, ph), -H*0.15, 18, C[5], 0.4+0.1*math.Sin(tau*ph*4))
			p.Glow(vortexAxis(H*0.05, H, ph), -3, 10, pixelfx.White, 0.3)
			vortexStreaks(p, C, t, H, seed, true)
			vortexOrbiters(p, C, t, H, seed, true)
			vortexFoot(p, C, t, seed, true)
			// snow thrown off the mouth, spreading into a thin cloud
			pixelfx.Cycle(t, vortexDuration, 10, seed+3, func(g *pixelfx.Rng) float64 { return g.Range(0.9, 1.3) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				th := r.Range(0, tau)
				R, k := vortexRadius(H, H)*(0.6+age*0.9), age/life
				rad, Rc := 3+6*k, math.Min(R, 36)
				x, y := vortexAxis(H, H, ph)+math.Cos(th)*Rc, -H-2-age*4+math.Sin(th)*Rc*vortexTilt
				a := 0.24 * (1 - k) * math.Min(1, age/0.15)
				p.DiscFn(x, y, rad*0.8, func(d, _ float64) (uint32, float64) { return C[6], a * (1 - d*d) })
				p.Glow(x, y, rad*1.5, C[4], 0.08*(1-k))
			})
		},
	}
}
