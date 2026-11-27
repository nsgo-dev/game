// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/arcane-beam.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The beam (banded across, pulses running down it, sine strands that brighten in front, a source flare with spinning rays, an end flare spraying rays and sparks) is adapted from Sprite Fusion's open-source energy beam (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// the loop, the hand, the beam's length and half width; variables so expressions with them round step by step as JavaScript's do
var (
	beamD  = 1.2
	beamHX = 15.0
	beamHY = -24.0
	beamL  = 168.0
	beamW  = 6.0
)

var (
	beamAim  = pixelfx.Ready.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(4), pixelfx.FX.Is(18), pixelfx.FY.Is(-24), pixelfx.BX.Is(13), pixelfx.BY.Is(-20), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-9), pixelfx.ToeB.On())
	beamPull = pixelfx.Ready.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-3), pixelfx.FX.Is(1), pixelfx.FY.Is(-20), pixelfx.BX.Is(4), pixelfx.BY.Is(-22), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-7))
)

// ArcaneBeam fires a beam right from the hand: a white core, strands twisting round it, pulses running down it, a flaring source and an end that sprays sparks. It is registered as "arcane/arcane-beam".
var ArcaneBeam = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Arcane beam",
		Desc:     "A beam fires right from the hand: a white core, strands twisting round it, pulses running down it, a flaring source and an end that sprays sparks.",
		Tags:     []string{"arcane", "beam", "laser", "ray", "magic", "directional", "loop"},
		Size:     [2]int{264, 100},
		Origin:   [2]int{26, 84},
		Duration: beamD,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(10)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{186},
		Poses: []pixelfx.Key{
			{T: -0.62, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
			{T: -0.3, Pose: beamPull},
			{T: -0.04, Pose: beamPull.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-4))},
			{T: 0, Pose: beamAim.With(pixelfx.Crouch.Is(3)), How: "snap"},
			{T: 0.3, Pose: beamAim},
			{T: 0.6, Pose: beamAim.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(3))},
			{T: 0.9, Pose: beamAim},
			{T: 1.2, Pose: beamAim.With(pixelfx.Crouch.Is(3))},
		},
		Hits: []pixelfx.Hit{
			{T: 0.1, Push: pixelfx.Val(0.5), Straw: pixelfx.Val(3), Flash: pixelfx.Val(0.02), Y: pixelfx.Val(-24)},
			{T: 0.7, Push: pixelfx.Val(0.5), Straw: pixelfx.Val(3), Flash: pixelfx.Val(0.02), Y: pixelfx.Val(-24)},
		},
		Dust: []pixelfx.DustKick{{T: 0, X: -10, N: 5, Dir: -1}},
	},
	New: func(o *pixelfx.Options) pixelfx.Instance {
		return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) { drawArcaneBeam(p, t, o) }}
	},
}

func init() { pixelfx.Register("arcane/arcane-beam", ArcaneBeam) }

func drawArcaneBeam(p *pixelfx.Painter, t float64, o *pixelfx.Options) {
	C, ph, S, EX := pixelfx.Fit(o.Palette(), 8), t/beamD, o.Seed(), beamHX+beamL
	// the body, built across from a white core out to a dithered violet edge, wobbling and pulsing as it goes
	LV := [6]uint32{pixelfx.White, C[6], C[5], C[4], C[3], C[2]}
	for d := 0.0; d <= beamL; d += 0.5 {
		x, w := beamHX+d, beamW*(1+0.12*math.Sin(d*0.35-ph*tau*8))*math.Min(1, (d+3)/7)
		pulse := 0
		if pixelfx.Fract(d*0.025-ph*4) < 0.07 {
			pulse = 1
		}
		for q0 := -w * 1.3; q0 <= w*1.3; q0 += 0.5 {
			q, y := math.Abs(q0)/w, beamHY+q0
			var lv int
			switch {
			case q < 0.2:
				lv = 0
			case q < 0.4:
				lv = 1
			case q < 0.62:
				lv = 2
			case q < 0.85:
				lv = 3
			case q < 1:
				lv = 4
			default:
				lv = 5
			}
			lv = max(0, lv-pulse)
			if lv == 5 && bayer(x, y) > (1.3-q)/0.3 {
				continue
			}
			p.Px(x, y, LV[lv])
		}
		// three strands twisting round it: bright on the near side, gone behind
		for s := range 3 {
			a := d*0.17 - ph*tau*6 + (float64(s)*tau)/3
			off := math.Sin(a) * w * 1.05
			if math.Cos(a) > -0.15 {
				p.Dot(x, beamHY+off, pick(math.Cos(a) > 0.55, pixelfx.White, C[6]), 1)
			}
		}
		if pixelfx.ToInt32(pixelfx.Round(d*2))&11 == 0 {
			p.Glow(x, beamHY, w*2.2+4, C[5], 0.2)
		}
	}

	// the source: a flickering flare with spinning rays at the hand
	fr := 4.2 + math.Sin(ph*tau*10)*0.6
	for i := range 4 {
		fi := float64(i)
		a, l := ph*tau*2.5+(fi*tau)/4, 7+3*math.Sin(ph*tau*6+fi)
		p.LineFn(beamHX, beamHY, beamHX+math.Cos(a)*l, beamHY+math.Sin(a)*l, func(u float64) (uint32, float64) {
			return pick(u < 0.5, pixelfx.White, C[6]), pickF(u < 0.6, 1, 1.6-u)
		})
	}
	p.Circle(beamHX, beamHY, fr+1.5, C[4], 1)
	p.Circle(beamHX, beamHY, fr, C[6], 1)
	p.Circle(beamHX, beamHY, fr*0.6, pixelfx.White, 1)
	p.Glow(beamHX, beamHY, 32, C[5], 0.8)
	// energy drawn into the hand
	pixelfx.Cycle(t, beamD, 10, S*3+1, pixelfx.Fixed(0.18), func(age, life float64, r *pixelfx.Rng, _ int) {
		a := r.Range(0, tau)
		dd := 16*(1-age/life) + 3
		x, y := beamHX+math.Cos(a)*dd, beamHY+math.Sin(a)*dd
		p.Dot(x, y, pick(age/life > 0.6, pixelfx.White, C[6]), 1)
	})

	// the end: a hot flare spraying rays, rings shocking out, sparks thrown back and falling
	ir := 5 + math.Sin(ph*tau*12)*0.8
	for i := range 7 {
		a, l := pi+float64(i-3)*0.36, 8+8*math.Abs(pixelfx.LoopNoise(ph, 4, S*7+float64(i)))
		body := pick(i%2 != 0, C[5], C[6])
		p.LineFn(EX, beamHY, EX+math.Cos(a)*l, beamHY+math.Sin(a)*l, func(u float64) (uint32, float64) {
			return pick(u < 0.4, pixelfx.White, body), pickF(u < 0.6, 1, 1.6-u)
		})
	}
	for k := range 3 {
		q := pixelfx.Fract(ph*3 + float64(k)/3)
		p.Ring(EX, beamHY, 3+15*pixelfx.Ease.OutCubic(q), math.Max(1, 2*(1-q)), pick(q < 0.25, pixelfx.White, C[5]), 1.1-q)
	}
	p.Circle(EX, beamHY, ir+1.5, C[4], 1)
	p.Circle(EX, beamHY, ir, C[6], 1)
	p.Circle(EX, beamHY, ir*0.55, pixelfx.White, 1)
	p.Glow(EX, beamHY, 40, C[5], 0.9)
	p.Glow(EX, beamHY, 12, pixelfx.White, 0.6)
	// a fountain of sparks thrown up and back off the end, arcing down
	pixelfx.Cycle(t, beamD, 220, S*3+2, func(g *pixelfx.Rng) float64 { return g.Range(0.18, 0.5) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		a := pi*1.5 + r.Range(-1.7, 0.45)
		sp := r.Range(70, 260)
		vx, vy := math.Cos(a)*sp, math.Sin(a)*sp-30
		x, y := pixelfx.Fly(EX, beamHY, vx, vy, age, 480, 2.2)
		ux, uy := pixelfx.FlyV(vx, vy, age, 480, 2.2)
		k := 1 - age/life
		if y > -1 {
			return
		}
		head := C[5]
		if k > 0.6 {
			head = pixelfx.White
		} else if k > 0.3 {
			head = C[6]
		}
		sp2 := or1(pixelfx.Hypot(ux, uy))
		l := math.Min(8, 1+sp2*0.022)
		p.LineFn(x-(ux/sp2)*l, y-(uy/sp2)*l, x, y, func(u float64) (uint32, float64) { return pick(u > 0.5, head, C[4]), 1 })
		p.Glow(x, y, 3, head, 0.45*k)
	})
	pixelfx.Cycle(t, beamD, 6, S*3+3, pixelfx.Fixed(0.3), func(age, life float64, r *pixelfx.Rng, _ int) {
		k := math.Sin((age / life) * pi)
		x := EX + r.Range(-12, 4)
		y := beamHY + r.Range(-12, 12)
		pixelfx.Sparkle(p, x, y, 1+2.5*k, C[6], C[5], 0.4*k)
	})
	// violet vapour lifting off the end
	pixelfx.Cycle(t, beamD, 7, S*3+5, func(g *pixelfx.Rng) float64 { return g.Range(0.8, 1.2) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		q := age / life
		x := EX + r.Range(-8, 4) - q*6
		y := beamHY - 4 - q*r.Range(14, 24)
		rad := r.Range(3, 5) * (0.7 + 0.8*q)
		p.DiscFn(x, y, rad, func(dd, _ float64) (uint32, float64) { return C[4], 0.28 * math.Sin(q*pi) * (1 - dd*dd) })
	})
	// motes shed along the beam
	pixelfx.Cycle(t, beamD, 16, S*3+4, func(g *pixelfx.Rng) float64 { return g.Range(0.3, 0.6) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		q := age / life
		x := beamHX + r.Range(10, beamL-10) + q*20
		y := beamHY + r.Sign()*(beamW+1+q*r.Range(4, 10))
		mote(p, x, y, 40, (y-beamHY)*1.5, pick(r.Chance(0.5), C[6], C[5]), 0.6, 1-q, 0.3)
	})
	// the floor beneath catches the light
	p.Glow(EX, -1, 30, C[5], 0.25)
	p.Glow(beamHX+beamL/2, -1, 60, C[4], 0.12)
}
