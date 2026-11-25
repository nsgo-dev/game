// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/rune-circle.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The floor glow and the specks rising off it follow Sprite Fusion's open-source mana heal (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// turning a quarter a loop keeps every part seamless: 16 ticks, a square, and 12 runes in a pattern of three; variables so expressions with them round step by step as JavaScript's do
var (
	rcD  = 4.0
	rcR  = 46.0
	rcRR = 39.0
	rcRI = 23.0
	rcRC = 7.0
	rcSY = 0.3
	rcNR = 12
)

// RuneCircle turns a rune circle on the floor in perspective: a carousel of runes lit by chasing light, a turning square, ticks, motes rising off it. It is registered as "arcane/rune-circle".
var RuneCircle = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Rune circle",
		Desc:     "A rune circle turns on the floor in perspective: a carousel of runes lit by chasing light, a turning square, ticks, motes rising off it.",
		Tags:     []string{"arcane", "rune", "circle", "magic", "aura", "ground", "loop"},
		Size:     [2]int{136, 100},
		Origin:   [2]int{68, 80},
		Duration: rcD,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: func(o *pixelfx.Options) pixelfx.Instance {
		return &pixelfx.Funcs{
			DrawBackFunc: func(p *pixelfx.Painter, t float64) { drawBackRuneCircle(p, t, o) },
			DrawFunc:     func(p *pixelfx.Painter, t float64) { drawRuneCircle(p, t, o) },
		}
	},
}

func init() { pixelfx.Register("arcane/rune-circle", RuneCircle) }

func rcAt(g, r float64) (float64, float64) { return math.Cos(g) * r, math.Sin(g) * r * rcSY }

func rcGap(a, b float64) float64 { return math.Abs(math.Atan2(math.Sin(a-b), math.Cos(a-b))) }

func rcCircle(p *pixelfx.Painter, C []uint32, t float64, far bool) {
	ph := t / rcD
	spin := (ph * tau) / 4
	keep := func(g float64) bool { return math.Sin(g) >= 0 }
	if far {
		keep = func(g float64) bool { return math.Sin(g) < 0 }
	}
	// two sparks of light chase round the outer ring, twice a loop
	chase := ph * tau * 2
	lit := func(g, w float64) bool { return rcGap(g, chase) < w || rcGap(g, chase+pi) < w }
	tone := func(g float64, hi, lo uint32) uint32 { return pick(math.Sin(g) < 0, lo, hi) }
	floorRing(p, 0, 0, rcR, rcSY, func(g float64) uint32 {
		if lit(g, 0.22) {
			return pixelfx.White
		}
		if lit(g, 0.5) {
			return C[6]
		}
		return tone(g, C[6], C[5])
	}, alpha(1), keep)
	floorRing(p, 0, 0, rcRI, rcSY, func(g float64) uint32 { return tone(g, C[5], C[4]) }, alpha(1), keep)
	floorRing(p, 0, 0, rcRC, rcSY, func(g float64) uint32 { return tone(g, C[6], C[5]) }, alpha(1), keep)
	// studs set round the outside of the ring, turning with it
	for j := range 16 {
		g := spin + ((float64(j)+0.5)*tau)/16
		if math.Sin(g) < 0 != far {
			continue
		}
		x, y := rcAt(g, rcR+3.5)
		hot := lit(g, 0.3)
		p.Px(x, y, pick(hot, pixelfx.White, tone(g, pick(j%2 != 0, C[5], C[6]), C[4])))
		if j%2 == 0 {
			for _, dx := range [2]float64{1, -1} {
				p.Px(x+dx, y, pick(hot, C[6], tone(g, C[4], C[3])))
			}
		}
	}
	// a square turning the other way inside the inner ring, its corners touching it
	var pts [4][2]float64
	for k := range 4 {
		pts[k][0], pts[k][1] = rcAt(-spin+(float64(k)*tau)/4+pi/4, rcRI)
	}
	for k := range 4 {
		ax, ay := pts[k][0], pts[k][1]
		bx, by := pts[(k+1)%4][0], pts[(k+1)%4][1]
		if (ay < 0) != (by < 0) {
			u := ay / (ay - by)
			mx := ax + (bx-ax)*u
			if (ay < 0) == far {
				bx, by = mx, 0
			} else {
				ax, ay = mx, 0
			}
		} else if (ay < 0) != far {
			continue
		}
		p.Line(ax, ay, bx, by, pick(far, C[4], C[5]), 1)
	}
	// the runes stand in the band, riding round; bright at the front, dim behind, flaring as the light passes
	type rcRune struct {
		k int
		g float64
	}
	var gs []rcRune
	for k := range rcNR {
		g := spin + ((float64(k)+0.5)*tau)/float64(rcNR)
		if math.Sin(g) < 0 == far {
			gs = append(gs, rcRune{k, g})
		}
	}
	slices.SortStableFunc(gs, func(a, b rcRune) int {
		d := math.Sin(a.g) - math.Sin(b.g)
		switch {
		case d < 0:
			return -1
		case d > 0:
			return 1
		}
		return 0
	})
	for _, r := range gs {
		k, g := r.k, r.g
		x, y := rcAt(g, rcRR)
		hot, bob := lit(g, 0.2), math.Sin(ph*tau*4+4*g)*0.5
		under := C[3]
		if hot {
			under = C[5]
		} else if far {
			under = C[2]
		}
		glyph(p, [3]int{0, 3, 5}[k%3], x, y-1+bob, pick(hot, pixelfx.White, pick(far, C[5], C[6])), 1, under)
		k2 := 0.22
		if hot {
			k2 = 0.5
		} else if far {
			k2 = 0.12
		}
		p.Glow(x, y-3, pickF(hot, 10, 6), C[5], k2)
		p.Glow(x, y, 5, C[4], 0.15)
	}
}

// rcMotes draws motes rising off the circle on a slow helix round its axis, swaying in, and twinkles hanging over it.
func rcMotes(p *pixelfx.Painter, C []uint32, t, S float64, far bool) {
	pixelfx.Cycle(t, rcD, 76, S*5+1, func(g *pixelfx.Rng) float64 { return g.Range(1.1, 1.9) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		g0 := r.Range(0, tau)
		r0 := pixelfx.Pick(r, []float64{rcR, rcR, rcRR, rcRR, rcRI})
		q := age / life
		g := g0 + q*r.Range(0.6, 1.4)
		if math.Sin(g) < 0 != far {
			return
		}
		rr := r0 * (1 - 0.35*q)
		h := 2 + pixelfx.Ease.OutQuad(q)*r.Range(26, 52)
		x, y := math.Cos(g)*rr, math.Sin(g)*rr*rcSY-h
		a, big := math.Min(1, q*5)*(1-pixelfx.Smooth(0.65, 1, q)), r.Chance(0.2)
		var c uint32
		if r.Chance(0.3) {
			c = pixelfx.White
		} else {
			c = pick(r.Chance(0.6), C[6], C[5])
		}
		size := 1.1
		if !big {
			size = r.Range(0.55, 0.9)
		}
		mote(p, x, y, -math.Sin(g)*rr*0.8, -36*(1-q)-10, c, size, a, pickF(big, 0.5, 0.3))
	})
	pixelfx.Cycle(t, rcD, 14, S*5+2, pixelfx.Fixed(0.45), func(age, life float64, r *pixelfx.Rng, _ int) {
		g := r.Range(0, tau)
		if math.Sin(g) < 0 != far {
			return
		}
		rr := r.Range(8, rcR)
		x := math.Cos(g) * rr
		y := math.Sin(g)*rr*rcSY - r.Range(6, 34)
		k := math.Sin((age / life) * pi)
		pixelfx.Sparkle(p, x, y, 1+2.4*k, C[6], C[5], 0.35*k)
	})
}

func drawBackRuneCircle(p *pixelfx.Painter, t float64, o *pixelfx.Options) {
	C, pulse := pixelfx.Fit(o.Palette(), 8), 0.85+0.15*math.Sin((t/rcD)*tau*4)
	// the floor glows under the circle, and a soft light stands over it
	p.Glow(0, 0, rcR*1.4, C[4], 0.24*pulse)
	for _, s := range [2]float64{-1, 1} {
		p.Glow(s*rcR*0.55, 1, rcR*0.7, C[5], 0.12*pulse)
	}
	// a soft column of light standing over it, fading as it rises
	for h := 0.0; h < 66; h += 6 {
		p.Glow(0, -h, 26, C[4], 0.11*pulse*(1-h/80))
	}
	rcCircle(p, C, t, true)
	rcMotes(p, C, t, o.Seed(), true)
}

func drawRuneCircle(p *pixelfx.Painter, t float64, o *pixelfx.Options) {
	C, chase := pixelfx.Fit(o.Palette(), 8), (t/rcD)*tau*2
	rcCircle(p, C, t, false)
	rcMotes(p, C, t, o.Seed(), false)
	// the two sparks of light running round the ring
	for _, g := range [2]float64{chase, chase + pi} {
		x, y := rcAt(g, rcR)
		pixelfx.Sparkle(p, x, y, 2.5+math.Sin(t*30)*0.8, C[6], C[5], 0.5)
		p.Glow(x, y, 14, C[5], 0.4)
	}
}
