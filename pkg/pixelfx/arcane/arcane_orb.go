// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/arcane-orb.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The orb (a glassy bubble that thickens towards its rim, a hot core, a double rim, a highlight arc and spot, motes orbiting in front and behind) is adapted from Sprite Fusion's open-source energy orb (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// the loop, the orb's radius and height; variables so expressions with them round step by step as JavaScript's do
var (
	orbD   = 2.4
	orbRad = 17.0
	orbY0  = -52.0
)

// ArcaneOrb floats an orb of smoky violet glass with a swirl turning round a white-hot heart, two tilted rings of light orbiting behind and in front. It is registered as "arcane/arcane-orb".
var ArcaneOrb = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Arcane orb",
		Desc:     "A floating orb of smoky violet glass with a swirl turning round a white-hot heart, two tilted rings of light orbiting behind and in front.",
		Tags:     []string{"arcane", "orb", "sphere", "magic", "floating", "loop"},
		Size:     [2]int{136, 112},
		Origin:   [2]int{68, 104},
		Duration: orbD,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: func(o *pixelfx.Options) pixelfx.Instance {
		return &pixelfx.Funcs{
			DrawBackFunc: func(p *pixelfx.Painter, t float64) { drawBackArcaneOrb(p, t, o) },
			DrawFunc:     func(p *pixelfx.Painter, t float64) { drawArcaneOrb(p, t, o) },
		}
	},
}

func init() { pixelfx.Register("arcane/arcane-orb", ArcaneOrb) }

// orbRing is one of the two rings, a circle tilted in depth (sy) and rolled in the picture (rot), beads running round it.
type orbRing struct {
	r, sy, rot float64
	beads      int
	turn, c    float64
}

var orbRings = [2]orbRing{
	{r: 31, sy: 0.24, rot: 0.36, beads: 3, turn: 2, c: 0},
	{r: 27, sy: 0.3, rot: -0.55, beads: 2, turn: -3, c: 1},
}

// orbState is where the orb is at a moment: its phase and its centre, bobbing.
type orbState struct {
	ph, cx, cy float64
}

func orbSetup(t float64) orbState {
	ph := t / orbD
	return orbState{ph: ph, cx: 0, cy: orbY0 + math.Sin(ph*tau)*2}
}

// orbRingDraw draws one ring's points on the near (z > 0) or far side; beads brighter, with light.
func orbRingDraw(p *pixelfx.Painter, C []uint32, s orbState, R *orbRing, near bool) {
	rot := R.rot + 0.12*math.Sin(s.ph*tau+R.c)
	cr, sr, n := math.Cos(rot), math.Sin(rot), int(math.Ceil(R.r*tau*1.5))
	pos := func(g float64) (float64, float64) {
		lx, ly := math.Cos(g)*R.r, math.Sin(g)*R.r*R.sy
		return s.cx + lx*cr - ly*sr, s.cy + lx*sr + ly*cr
	}
	for j := range n {
		g := (float64(j) / float64(n)) * tau
		if z := math.Sin(g); z > 0 != near {
			continue
		}
		x, y := pos(g)
		p.Dot(x, y, pick(near, C[5], C[4]), pickF(near, 0.7, 0.4))
	}
	for b := range R.beads {
		// Force float64 rounding of the ring phase before adding the bead offset so arm64 FMA does not fuse it and flip the sign of sin(g) near 2π (frame 12, bead 2 of ring 0 with the defaults).
		g := float64(s.ph*tau*R.turn) + (float64(b)*tau)/float64(R.beads)
		for k := range 7 {
			// a bead and its trail along the ring
			fk := float64(k)
			gg := g - pixelfx.Sign(R.turn)*fk*0.07
			if z := math.Sin(gg); z > 0 != near {
				continue
			}
			x, y := pos(gg)
			c := C[5]
			if k == 0 {
				c = pixelfx.White
			} else if k < 3 {
				c = C[6]
			}
			p.Dot(x, y, c, pickF(k < 4, 1, 1-(fk-3)/4))
			if k == 0 {
				p.Glow(x, y, pickF(near, 7, 5), C[6], pickF(near, 0.5, 0.25))
			}
		}
	}
}

// orbBody draws the orb itself: smoky glass, the swirl, the heart, the rim, the highlights.
func orbBody(p *pixelfx.Painter, C []uint32, s orbState) {
	ph, cx, cy := s.ph, s.cx, s.cy
	// the glass: translucent, thicker towards the rim
	p.DiscFn(cx, cy, orbRad, func(d, _ float64) (uint32, float64) { return pick(d > 0.82, C[2], C[1]), 0.32 + 0.5*d*d })
	// the swirl: two bright arms with a lilac edge, and two faint ones between, turning twice a loop
	for _, arm := range [4]struct {
		s0   float64
		main bool
	}{{0, true}, {pi, true}, {pi / 2, false}, {(3 * pi) / 2, false}} {
		for u := 0.08; u <= 0.85; u += 0.012 {
			a, r := ph*tau*2+arm.s0+u*3.4, u*orbRad*0.92
			x, y := cx+math.Cos(a)*r, cy+math.Sin(a)*r
			fade := 1.0
			if u >= 0.6 {
				fade = 1 - (u-0.6)/0.25
			}
			if !arm.main {
				p.Dot(x, y, C[4], fade*0.8)
				continue
			}
			// the arm's lit edge trails just behind it
			b := a - 0.16
			xb, yb := cx+math.Cos(b)*r, cy+math.Sin(b)*r
			p.Dot(xb, yb, pick(u < 0.4, C[6], C[5]), fade)
			c := C[5]
			if u < 0.3 {
				c = pixelfx.White
			} else if u < 0.55 {
				c = C[6]
			}
			p.Dot(x, y, c, fade)
		}
	}
	// the heart
	pulse := 0.85 + 0.15*math.Sin(ph*tau*3)
	p.Circle(cx, cy, 3.6*pulse+1, C[5], 1)
	p.Circle(cx, cy, 2.8*pulse, C[6], 1)
	p.Circle(cx, cy, 1.7*pulse, pixelfx.White, 1)
	// the rim: a bright line just inside a dark one, a highlight arc and spot top left, a glint bottom right
	p.Ring(cx, cy, orbRad+0.4, 1, C[5], 1)
	p.Ring(cx, cy, orbRad+1.3, 1, C[2], 0.8)
	for a := 3.5; a < 4.7; a += 0.03 {
		p.Dot(cx+math.Cos(a)*(orbRad-2.4), cy+math.Sin(a)*(orbRad-2.4), pick(a > 3.85 && a < 4.35, pixelfx.White, C[6]), 1)
	}
	p.Circle(cx-orbRad*0.42, cy-orbRad*0.5, 1.3, pixelfx.White, 1)
	p.Dot(cx+orbRad*0.52, cy+orbRad*0.55, C[6], 1)
	p.Glow(cx, cy, 20, C[6], 0.26*pulse)
	p.GlowRing(cx, cy, orbRad, 6, C[5], 0.25)
}

// orbMotes draws motes orbiting on tilted paths, and sparks drifting off the orb.
func orbMotes(p *pixelfx.Painter, C []uint32, s orbState, S float64, near bool) {
	ph, cx, cy := s.ph, s.cx, s.cy
	for i := range 8 {
		fi := float64(i)
		dir := 1.0
		if i%2 == 0 {
			dir = -1
		}
		a := math.Mod(fi*2.4+S, tau) + ph*tau*dir*float64(1+i%3)
		rr := orbRad + 5 + math.Mod(fi*0.37, 1)*12
		tilt := (math.Mod(fi*0.71, 1) - 0.5) * 1.4
		x0, z := math.Cos(a)*rr, math.Sin(a)
		if z > 0 != near {
			continue
		}
		x, y := cx+x0*math.Cos(tilt), cy+x0*math.Sin(tilt)+z*rr*0.28
		p.Dot(x, y, pick(near, pixelfx.White, C[5]), 1)
		p.Dot(x-dir, y, pick(near, C[6], C[4]), pickF(near, 1, 0.6))
		if near {
			p.Dot(x-2*dir, y, C[4], 0.6)
			p.Glow(x, y, 5, C[6], 0.35)
		}
	}
	if !near {
		return
	}
	pixelfx.Cycle(ph*orbD, orbD, 10, S*3+1, func(g *pixelfx.Rng) float64 { return g.Range(0.6, 1) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		a, q := r.Range(0, tau), age/life
		d := orbRad + 1 + q*r.Range(8, 18)
		x, y := cx+math.Cos(a)*d, cy+math.Sin(a)*d-q*10
		mote(p, x, y, math.Cos(a)*25, math.Sin(a)*25-12, pick(r.Chance(0.5), C[6], C[5]), 0.6, 1-q, 0.3)
	})
}

func drawBackArcaneOrb(p *pixelfx.Painter, t float64, o *pixelfx.Options) {
	C, s := pixelfx.Fit(o.Palette(), 8), orbSetup(t)
	lift := (s.cy - orbY0) / 2
	// the light it throws on the floor, stronger as it bobs down
	p.Glow(0, -1, 36, C[4], 0.26-0.04*lift)
	for x := -20.0; x <= 20; x++ {
		u := math.Abs(x+0.5) / 20
		p.Dot(x, 0, pick(u < 0.3, C[6], C[5]), (1.1-u)*(0.9-0.15*lift))
	}
	p.Glow(s.cx, s.cy, 60, C[4], 0.36)
	for i := range orbRings {
		orbRingDraw(p, C, s, &orbRings[i], false)
	}
	orbMotes(p, C, s, o.Seed(), false)
}

func drawArcaneOrb(p *pixelfx.Painter, t float64, o *pixelfx.Options) {
	C, s := pixelfx.Fit(o.Palette(), 8), orbSetup(t)
	orbBody(p, C, s)
	for i := range orbRings {
		orbRingDraw(p, C, s, &orbRings[i], true)
	}
	orbMotes(p, C, s, o.Seed(), true)
	pixelfx.Sparkle(p, s.cx-orbRad*0.42, s.cy-orbRad*0.5, 1.5+math.Max(0, math.Sin(s.ph*tau*2))*2, C[6], C[5], 0.3)
}
