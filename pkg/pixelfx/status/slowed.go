// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/slowed.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package status

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

const (
	slowedDuration = 2.4
	slowedTicks    = 4.0
	slowedStep     = slowedDuration / slowedTicks
	slowedR        = 18.0
)

var (
	slowedS1 = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.3), pixelfx.Lean.Is(3), pixelfx.Crouch.Is(3), pixelfx.FFX.Is(11), pixelfx.FFY.Is(-5), pixelfx.FBX.Is(-9), pixelfx.FBY.Is(-5), pixelfx.ToeB.On(), pixelfx.FX.Is(-5), pixelfx.FY.Is(-19), pixelfx.BX.Is(11), pixelfx.BY.Is(-26))
	slowedS2 = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.3), pixelfx.Lean.Is(3), pixelfx.Crouch.Is(3), pixelfx.FFX.Is(-9), pixelfx.FFY.Is(-5), pixelfx.ToeF.On(), pixelfx.FBX.Is(11), pixelfx.FBY.Is(-5), pixelfx.FX.Is(13), pixelfx.FY.Is(-26), pixelfx.BX.Is(-4), pixelfx.BY.Is(-19))
)

// Slowed is dragged through time: a glassy clock dial behind the head ticks round in heavy steps, flashing with each, while drops sink in slow motion and spread slow rings. It is registered as "status/slowed".
var Slowed = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Slowed",
		Desc:     "Dragged through time: a glassy clock dial behind the head ticks round in heavy steps, flashing with each, while drops sink in slow motion and spread slow rings.",
		Tags:     []string{"status", "slow", "time", "clock", "debuff", "loop"},
		Size:     [2]int{64, 76},
		Origin:   [2]int{32, 68},
		Duration: slowedDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#081a24", "#0e3446", "#165a6e", "#2a8a9a", "#52bcc4", "#9ee6e0", "#e2fff8", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: slowedS1},
			{T: 1.2, Pose: slowedS2},
			{T: 2.4, Pose: slowedS1},
		},
	},
	New: newSlowed,
}

func init() { pixelfx.Register("status/slowed", Slowed) }

type clockHand struct {
	ang, flash, ph float64
}

func slowedHand(t float64) clockHand {
	k := math.Floor(t / slowedStep)
	ph := (t - k*slowedStep) / slowedStep
	base := -pi/2 + (k*tau)/slowedTicks
	q := tau / slowedTicks
	if ph < 0.62 {
		return clockHand{ang: base, flash: 0, ph: ph}
	}
	if ph < 0.72 {
		return clockHand{ang: base - 0.1*math.Sin(((ph-0.62)/0.1)*pi*0.5), flash: 0, ph: ph}
	}
	s := (ph - 0.72) / 0.28
	snap := pixelfx.OutBack(pixelfx.Clamp(s/0.35), 2.2)
	wob := math.Exp(-s*6) * math.Sin(s*22) * 0.05
	return clockHand{ang: base - 0.1*(1-pixelfx.Clamp(s/0.2)) + q*snap + wob, flash: math.Exp(-s * 5), ph: ph}
}

func slowedAfterglow(t float64) float64 {
	s := pixelfx.Wrap(t-slowedStep*0.72, slowedStep)
	return math.Exp(-s * 4)
}

func slowedDial(p *pixelfx.Painter, C []uint32, cx, cy, t float64) {
	h := slowedHand(t)
	fl := math.Max(h.flash, slowedAfterglow(t))
	p.Disc(cx, cy, slowedR-1, C[2], 0.2)
	p.Disc(cx-slowedR*0.25, cy-slowedR*0.3, slowedR*0.5, C[4], 0.08)
	p.Ring(cx, cy, slowedR+1, 1, C[0], 1)
	p.RingFn(cx, cy, slowedR, 2, func(v, ang float64) (uint32, float64) {
		if math.Sin(ang+0.8) < -0.3 {
			if fl > 0.4 {
				return C[7], 1
			}
			return C[6], 1
		}
		if v > 0.5 {
			return C[5], 1
		}
		return C[4], 1
	})
	p.Ring(cx, cy, slowedR-3.5, 1, C[3], 0.6)
	for i := 0; i < 60; i++ {
		if i%5 == 0 {
			continue
		}
		a := (float64(i) / 60) * tau
		p.Dot(cx+math.Cos(a)*(slowedR-2.6), cy+math.Sin(a)*(slowedR-2.6), C[4], 0.8)
	}
	for i := 0; i < 12; i++ {
		a := (float64(i) / 12) * tau
		big := i%3 == 0
		r0 := slowedR - 2.5
		r1 := slowedR - 4.5
		col := C[5]
		if big {
			r1 = slowedR - 6
			col = C[6]
		}
		c, s := math.Cos(a), math.Sin(a)
		p.Line(cx+c*r0, cy+s*r0, cx+c*r1, cy+s*r1, col, 1)
	}
	shud := h.flash * math.Sin(t*60) * 0.06
	ha := -pi/2 + 2.2 + shud
	p.Beam(cx, cy, cx+math.Cos(ha)*slowedR*0.5, cy+math.Sin(ha)*slowedR*0.5, 2.2, C[5], 1)
	tx := cx + math.Cos(h.ang)*(slowedR-4)
	ty := cy + math.Sin(h.ang)*(slowedR-4)
	p.BeamFn(cx, cy, tx, ty, 2.4, func(u, _ float64) (uint32, float64) {
		if u > 0.8 {
			return C[7], 1
		}
		return C[6], 1
	})
	p.Circle(cx, cy, 2, C[6], 1)
	p.Px(cx, cy, C[1])
	p.Glow(cx, cy, slowedR+8, C[4], 0.14+0.2*fl)
	p.GlowRing(cx, cy, slowedR, 3, C[5], 0.12+0.35*fl)
	p.Glow(tx, ty, 5, C[6], 0.3+0.3*h.flash)
	if h.flash > 0.02 {
		s := 1 - h.flash
		p.Ring(cx, cy, slowedR+2+10*s, 1, C[5], h.flash)
		p.GlowRing(cx, cy, slowedR+2+10*s, 3, C[4], 0.3*h.flash)
	}
}

func newSlowed(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			slowedDial(p, C, 0, a.Y+3, t)
			lag := slowedAfterglow(t)
			if lag > 0.05 {
				p.Outline(a.Mask, a.X-1-pixelfx.Round(2*lag), a.Y, C[5], 0.55*lag)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			fl := math.Max(slowedHand(t).flash, slowedAfterglow(t))
			for _, pt := range S.body {
				p.Blend(a.X+float64(pt[0]), a.Y+float64(pt[1]), C[5], 0.08+0.12*fl)
			}
			pixelfx.Cycle(t, slowedDuration, 6, seed+3, func(r *pixelfx.Rng) float64 { return r.Range(1.6, 2.2) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				side := 1.0
				if r.Next() < 0.5 {
					side = -1
				}
				x := side * r.Range(10, 17)
				y0 := a.Y + r.Range(6, 22)
				v0 := r.Range(18, 30)
				k := 1.1
				yAt := func(g float64) float64 { return y0 + (v0*(1-math.Exp(-k*g)))/k + 4*g }
				y := yAt(age)
				u := age / life
				if y < -1 {
					for _, gh := range [2][2]float64{{0.22, 0.35}, {0.11, 0.6}} {
						if age > gh[0] {
							p.Dot(x, yAt(age-gh[0]), C[4], gh[1])
						}
					}
					p.Px(x, y-1, C[5])
					p.Px(x, y, C[6])
					p.Px(x, y+1, C[5])
					p.Px(x-1, y+1, C[4])
					p.Px(x-1, y, C[7])
					p.Glow(x, y, 4, C[5], 0.3*(1-u))
				} else {
					g := age
					for g > 0 && yAt(g) >= -1 {
						g -= 0.02
					}
					s := pixelfx.Clamp((age - g) / 0.9)
					if s < 1 {
						rx := 1 + 7*math.Sqrt(s)
						c := C[5]
						if s < 0.3 {
							c = C[6]
						}
						for _, sd := range [2]float64{-1, 1} {
							p.Line(x+sd*rx, -0.5, x+sd*(rx-1.5), -0.5, c, (1-s)*1.2)
						}
						for dx := -rx + 2; dx <= rx-2; dx++ {
							p.Dot(x+dx, -1.5, C[4], (1-s)*0.5*(1-math.Abs(dx)/rx))
						}
						p.Glow(x, -1, 4+rx, C[4], 0.12*(1-s))
					}
				}
			})
		},
	}
}
