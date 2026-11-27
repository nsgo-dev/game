// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/spell-charge.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The charge recipe (motes spiralling in, pulse rings collapsing onto a layered ball with flickering rays, short crackling sparks) is adapted from Sprite Fusion's open-source energy charge (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// the loop, the hand, the rings and how long each takes to land; variables so expressions with them round step by step as JavaScript's do
var (
	chargeD     = 1.6
	chargeHX    = 15.0
	chargeHY    = -22.0
	chargeRings = 4
	chargeRT    = 0.5
)

var chargeHold = pixelfx.Ready.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(2), pixelfx.FX.Is(18), pixelfx.FY.Is(-22), pixelfx.BX.Is(11), pixelfx.BY.Is(-17), pixelfx.FFX.Is(10), pixelfx.FBX.Is(-8))

// SpellCharge gathers energy in the caster's hand: streaks and motes spiral in, rings collapse into a swelling orb that crackles with arcs, ready to fire. It is registered as "arcane/spell-charge".
var SpellCharge = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Spell charge",
		Desc:     "Energy gathers in the caster's hand: streaks and motes spiral in, rings collapse into a swelling orb that crackles with arcs, ready to fire.",
		Tags:     []string{"arcane", "charge", "spell", "orb", "cast", "magic", "loop"},
		Size:     [2]int{120, 86},
		Origin:   [2]int{44, 80},
		Duration: chargeD,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		Poses: []pixelfx.Key{
			{T: -0.46, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
			{T: -0.12, Pose: chargeHold.With(pixelfx.Crouch.Is(3))},
			{T: 0, Pose: chargeHold},
			{T: 0.4, Pose: chargeHold.With(pixelfx.Crouch.Is(5))},
			{T: 0.8, Pose: chargeHold.With(pixelfx.Lean.Is(3))},
			{T: 1.2, Pose: chargeHold.With(pixelfx.Crouch.Is(5))},
			{T: 1.6, Pose: chargeHold},
		},
	},
	New: func(o *pixelfx.Options) pixelfx.Instance {
		return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) { drawSpellCharge(p, t, o) }}
	},
}

func init() { pixelfx.Register("arcane/spell-charge", SpellCharge) }

// chargeLandings are the times each ring lands (born at k * D / RINGS, landing RT later), wrapped into the loop.
var chargeLandings = func() []float64 {
	out := make([]float64, chargeRings)
	for k := range out {
		out[k] = (float64(k)*chargeD)/float64(chargeRings) + chargeRT
	}
	return out
}()

func chargeSinceLast(t float64) float64 {
	m := math.Inf(1)
	for _, l := range chargeLandings {
		m = math.Min(m, math.Mod(math.Mod(t-l, chargeD)+chargeD, chargeD))
	}
	return m
}

func chargeOrb(p *pixelfx.Painter, C []uint32, x, y, r, t, ph float64) {
	// rays flickering round the ball, turning three steps a loop
	for i := range 8 {
		fi := float64(i)
		a := (fi/8)*tau + ph*(tau/8)*3
		l := (1.5 + 4.5*math.Abs(pixelfx.LoopNoise(ph, 3, fi*7+1))) * pickF(i%2 != 0, 0.7, 1)
		x0, y0 := x+math.Cos(a)*(r+1.2), y+math.Sin(a)*(r+1.2)
		tip := pick(i%2 != 0, C[4], C[5])
		p.LineFn(x0, y0, x0+math.Cos(a)*l, y0+math.Sin(a)*l, func(u float64) (uint32, float64) {
			return pick(u < 0.4, C[6], tip), pickF(u < 0.6, 1, 1.6-u)
		})
	}
	// the ball: a violet rim, a bright body lit from the top left, a big white heart
	p.Circle(x, y, r+1.3, C[4], 1)
	p.Circle(x, y, r+0.3, C[5], 1)
	p.Circle(x-r*0.12, y-r*0.12, r*0.8, C[6], 1)
	p.Circle(x, y, r*0.5, pixelfx.White, 1)
	// filaments crackling out from the heart through the body to the shell, new ones every other frame
	R := pixelfx.RngOf(math.Floor(t*15), 77)
	for range 3 {
		a := R.Range(0, tau)
		pts := arc(R, x+math.Cos(a)*r*0.4, y+math.Sin(a)*r*0.4, x+math.Cos(a+R.Range(-0.6, 0.6))*(r+0.6), y+math.Sin(a+R.Range(-0.6, 0.6))*(r+0.6), arcOpts{jag: 0.45, forks: 0, forkLen: 0.5})[0].pts
		p.PolylineFn(pts, func(u float64) (uint32, float64) { return pick(u < 0.6, pixelfx.White, C[6]), 1 })
	}
	// a glint on the shell
	p.Px(x-r*0.5, y-r*0.62, pixelfx.White)
}

func chargeRingAt(p *pixelfx.Painter, C []uint32, x, y, rr, sy, rot, q float64) {
	n, c, s := int(math.Ceil(rr*tau*1.4)), math.Cos(rot), math.Sin(rot)
	col := C[5]
	if q > 0.85 {
		col = pixelfx.White
	} else if q > 0.55 {
		col = C[6]
	}
	a := math.Min(1, q*3)
	for j := range n {
		g := (float64(j) / float64(n)) * tau
		lx, ly := math.Cos(g)*rr, math.Sin(g)*rr*sy
		px, py := x+lx*c-ly*s, y+lx*s+ly*c
		if py > -0.5 {
			continue // the floor hides the bottom of a big ring
		}
		p.Dot(px, py, col, a)
		if j%10 == 0 {
			p.Glow(px, py, 5, C[5], 0.12*a)
		}
	}
}

// chargeShapes are each ring's tilt in depth and roll in the picture.
var chargeShapes = [4][2]float64{{1, 0}, {0.38, 0.55}, {1, 0}, {0.42, -0.7}}

func drawSpellCharge(p *pixelfx.Painter, t float64, o *pixelfx.Options) {
	C, ph, S, f := pixelfx.Fit(o.Palette(), 8), t/chargeD, o.Seed(), int(math.Floor(t*30))
	since := chargeSinceLast(t)
	s0, s1 := since/0.07, since/0.18
	swell := math.Exp(-(s0 * s0)) + 0.5*math.Exp(-(s1*s1))
	r := 6.1 + 0.5*math.Sin(ph*tau*4) + 1.4*swell
	// unstable: it shivers a pixel as each ring lands
	jit := 0.0
	if swell > 0.4 {
		jit = pickF(f%2 != 0, 1, -1)
	}
	x, y := chargeHX+jit, chargeHY

	// streaks spiralling in from all round, faster as they near the hand
	pixelfx.Cycle(t, chargeD, 110, S*3+1, func(g *pixelfx.Rng) float64 { return g.Range(0.2, 0.34) }, func(age, life float64, R *pixelfx.Rng, _ int) {
		// they come from the left, above and the upper right; the swirl would carry the rest into the floor
		a0, d0, spin := R.Range(2.8, 5.75), R.Range(20, 52), R.Range(0.6, 1.3)
		pos := func(q float64) (float64, float64) {
			a, d := a0+spin*q*q, d0*(1-q*q)+r
			return chargeHX + math.Cos(a)*d, chargeHY + math.Sin(a)*d*0.92
		}
		q := age / life
		px, py := pos(q)
		qx, qy := pos(math.Min(1, q+0.02))
		// their light dies as they reach the orb, so it isn't washed out by them
		streak(p, px, py, (qx-px)/(0.02*life), (qy-py)/(0.02*life), pick(R.Chance(0.55), C[6], C[5]), math.Min(1, q*4)*(1-pixelfx.Smooth(0.9, 1, q)), 7, 0.4*(1-pixelfx.Smooth(0.6, 0.9, q)))
	})
	// slower motes, drifting in on the same swirl
	pixelfx.Cycle(t, chargeD, 56, S*3+2, func(g *pixelfx.Rng) float64 { return g.Range(0.4, 0.7) }, func(age, life float64, R *pixelfx.Rng, _ int) {
		a0, d0, spin, size := R.Range(2.6, 5.2), R.Range(12, 32), R.Range(1, 2.2), R.Range(0.6, 1)
		pos := func(q float64) (float64, float64) {
			a, d := a0+spin*q, d0*(1-pixelfx.Ease.InQuad(q))+r
			return chargeHX + math.Cos(a)*d, chargeHY + math.Sin(a)*d
		}
		q := age / life
		px, py := pos(q)
		qx, qy := pos(math.Min(1, q+0.02))
		mote(p, px, py, (qx-px)/(0.02*life), (qy-py)/(0.02*life), pixelfx.Pick(R, []uint32{C[5], C[6], C[6], pixelfx.White}), size, math.Min(1, q*3)*pickF(q > 0.9, (1-q)/0.1, 1), 0.3*(1-pixelfx.Smooth(0.55, 0.85, q)))
	})

	// rings collapsing onto the orb, each tilted its own way
	for k := range chargeRings {
		for _, off := range [2]float64{-chargeD, 0} {
			q := (t - (float64(k)*chargeD)/float64(chargeRings) - off) / chargeRT
			if q < 0 || q >= 1 {
				continue
			}
			rr := r + 1.5 + 22*(1-pixelfx.Ease.InQuad(q))
			chargeRingAt(p, C, x, y, rr, chargeShapes[k][0], chargeShapes[k][1], q)
		}
	}

	chargeOrb(p, C, x, y, r, t, ph)

	// arcs crackling off it, new ones every other frame
	w := math.Floor(float64(f) / 2)
	Rw := pixelfx.RngOf(S, w, 3)
	n := 0
	if Rw.Next() < 0.6 {
		n++
	}
	if swell > 0.3 {
		n++
	}
	fresh := pickF(f%2 == 0, 0.8, 0.4)
	for range n {
		a := Rw.Range(0, tau)
		L := Rw.Range(5, 10) * (1 + swell*0.4)
		a1 := a + Rw.Range(-0.5, 0.5)
		x0, y0 := x+math.Cos(a)*(r+0.5), y+math.Sin(a)*(r+0.5)
		x1, y1 := x0+math.Cos(a1)*L, math.Min(-2, y0+math.Sin(a1)*L)
		forks := 0
		if Rw.Next() < 0.5 {
			forks = 1
		}
		drawArc(p, arc(Rw, x0, y0, x1, y1, arcOpts{jag: 0.35, forks: forks, forkLen: 0.5}), arcColors{core: pixelfx.White, body: C[5], halo: C[3], glow: C[5]}, fresh, 0.3)
	}
	// glints winking round it
	pixelfx.Cycle(t, chargeD, 7, S*3+4, pixelfx.Fixed(0.22), func(age, life float64, R *pixelfx.Rng, _ int) {
		a, d := R.Range(0, tau), R.Range(r+5, r+15)
		k := math.Sin((age / life) * pi)
		pixelfx.Sparkle(p, x+math.Cos(a)*d, y+math.Sin(a)*d*0.9, 1+2.5*k, C[6], C[4], 0.4*k)
	})
	if since < 0.07 {
		pixelfx.Sparkle(p, x, y, 4+6*(1-since/0.07), C[6], C[5], 0.5)
	}

	// light: a wide pool that lights the caster, a hot heart, the floor beneath the hand
	pulse := 0.85 + 0.15*math.Sin(ph*tau*4) + 0.2*swell
	p.Glow(x, y, 46, C[4], 0.26*pulse)
	p.GlowRing(x, y, r+3, 8, C[5], 0.3*pulse)
	p.Glow(x, y, r, C[6], 0.12)
	p.Glow(chargeHX, -1, 22, C[5], 0.16*pulse)
}
