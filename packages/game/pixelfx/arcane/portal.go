// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/portal.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The dithered light ramp and the streaked, knotted disc texture are adapted from Sprite Fusion's open-source black hole (https://www.spritefusion.com/pixel-art-vfx/black-hole), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var portalD = 2.4

// Portal opens an upright rift: fibrous spiral arms wind down into the dark, a lit rim wobbles, motes and streaks are pulled in, the floor glows. Its width and height options are the rift's half width and half height. It is registered as "arcane/portal".
var Portal = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Portal",
		Desc:     "An upright rift: fibrous spiral arms wind down into the dark, a lit rim wobbles, motes and streaks are pulled in, the floor glows.",
		Tags:     []string{"arcane", "portal", "rift", "teleport", "magic", "ground", "loop"},
		Size:     [2]int{152, 116},
		Origin:   [2]int{76, 106},
		Duration: portalD,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "width", Default: pixelfx.Num(17)},
			{Name: "height", Default: pixelfx.Num(29)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: func(o *pixelfx.Options) pixelfx.Instance {
		return &pixelfx.Funcs{
			DrawBackFunc: func(p *pixelfx.Painter, t float64) { portalFloor(p, portalSetup(t, o)) },
			DrawFunc:     func(p *pixelfx.Painter, t float64) { drawPortal(p, t, o) },
		}
	},
}

func init() { pixelfx.Register("arcane/portal", Portal) }

func portalWrapPi(a float64) float64 { return a - tau*math.Floor((a+pi)/tau) }

// portalState is the rift at a moment: its colors, half size, phase, seed and centre, and the two hot spots chasing round its rim.
type portalState struct {
	C                 []uint32
	rx, ry, ph, S, CY float64
	spots             [2]float64
}

func portalSetup(t float64, o *pixelfx.Options) *portalState {
	ph := t / portalD
	ry := o.Num("height")
	// two hot spots chase round the rim with the spin, and a key light sits at the top left
	return &portalState{C: pixelfx.Fit(o.Palette(), 8), rx: o.Num("width"), ry: ry, ph: ph, S: o.Seed(), CY: -(ry + 5), spots: [2]float64{ph*tau*2 - 0.6, ph*tau*2 + pi - 0.6}}
}

// rim is the rim's radius (1 = the oval) at angle th: slow noise rolling round it, plus a three-lobed breath.
func (s *portalState) rim(th float64) float64 {
	return 1 + 0.05*pixelfx.TileNoise((th/tau)*10+s.ph*10, s.ph*4, 10, 4, s.S) + 0.018*math.Sin(th*3-s.ph*tau*2)
}

// at is where a point at angle th and radius rho (1 = the rim) sits; outside, the field widens so motes come from round about.
func (s *portalState) at(th, rho float64) (float64, float64) {
	out, k := math.Max(0, rho-1), math.Min(1, rho)
	return math.Cos(th) * (k*s.rx + out*21), math.Min(-1, s.CY+math.Sin(th)*(k*s.ry+out*16))
}

func (s *portalState) hot(th float64) float64 {
	w0, w1 := portalWrapPi(th-s.spots[0]), portalWrapPi(th-s.spots[1])
	return math.Max(math.Exp(-(w0*w0)/0.12), 0.85*math.Exp(-(w1*w1)/0.08)) + 0.35*math.Max(0, math.Cos(th+2.3))
}

// portalRift draws the rift itself: the dark well, the fibrous arms winding down into it, the lit rim.
func portalRift(p *pixelfx.Painter, s *portalState) {
	C, rx, ry, ph, CY := s.C, s.rx, s.ry, s.ph, s.CY
	if !(rx > 0 && ry > 0) {
		return
	}
	p.Shade(-rx-4, CY-ry-4, rx+4, CY+ry+4, func(x, y float64, out *pixelfx.Out) uint32 {
		u, v := x/rx, (y-CY)/ry
		r := pixelfx.Hypot(u, v)
		if r == 0 || math.IsNaN(r) {
			r = 1e-6
		}
		th := math.Atan2(v, u)
		R0 := s.rim(th)
		g := pixelfx.Hypot(u/rx, v/ry) / r
		e, h := (r-R0)/g, s.hot(th)
		if e > 2.5 {
			return 0
		}
		// outside the rim: a coloured line where it's lit, a dark one elsewhere, a faint halo past it
		if e > 1.5 {
			if h > 0.6 {
				out.C, out.A = C[3], 0.5
			}
			return 0
		}
		if e > 0.5 {
			out.C = pick(h > 0.5, C[4], C[2])
			return 0
		}
		if e > -0.5 {
			out.C = pick(h > 0.42, pixelfx.White, C[6])
			out.K = 0.25 + 0.4*h
			return 0
		}
		// inside, in ramp steps: clean depth bands falling away to a dark well; three thin bright arms and three faint ones on a log spiral, flowing inward a turn a loop, with knots of light running down them; the light that spills in round the rim
		d := r / R0
		z := -math.Log(math.Max(0.02, d))
		phi := 3*th + 6.5*z - tau*3*ph
		c1, c2 := 0.5+0.5*math.Cos(phi), 0.5+0.5*math.Cos(phi+pi+0.6*z)
		lit, sink := math.Pow(d, 1.5), pixelfx.Smooth(0.2, 0.62, d)
		knot := math.Pow(math.Max(0, math.Cos(4*z-phi/3+tau*5*ph)), 6)
		st := pixelfx.Smooth(0.4, 0.7, d) + 0.6*pixelfx.Smooth(0.8, 0.95, d)
		crest := math.Pow(c1, 16) * (0.6 + 5.6*lit) * (1 + 0.5*knot) * sink
		st += crest + math.Pow(c1, 5)*1.1*lit
		st += math.Pow(c2, 18) * (0.3 + 1.8*lit) * sink
		// rings of the tunnel shrinking away down the well, two a loop
		st += math.Pow(0.5+0.5*math.Cos(tau*(z*1.5-ph*2)), 20) * 1.3 * sink * (1 - 0.5*c1)
		// fine strands between the arms, a step brighter than the dark they run through
		st += math.Pow(0.5+0.5*math.Cos(3*phi+2*z+1.3), 10) * 1.1 * lit * sink
		st += 2.2 * math.Pow(pixelfx.Smooth(0.8, 1, d), 2) * (0.75 + 0.45*h)
		out.C = tone(C, st/7, x, y, 0, 0.65)
		if crest > 2.2 {
			out.K = 0.12 + 0.05*crest
		}
		return 0
	})
}

// portalWell draws specks of light deep in the well, swept round with the arms and sinking towards the far end.
func portalWell(p *pixelfx.Painter, s *portalState) {
	C, rx, ry, ph, CY := s.C, s.rx, s.ry, s.ph, s.CY
	pixelfx.Cycle(ph*portalD, portalD, 18, s.S*7+5, func(g *pixelfx.Rng) float64 { return g.Range(1.1, 1.8) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		q := age / life
		th := r.Range(0, tau) + q*2.6
		rho := 0.92 * (1 - pixelfx.Ease.InQuad(q)*0.9)
		R0 := s.rim(th)
		x, y := math.Cos(th)*rho*R0*rx, CY+math.Sin(th)*rho*R0*ry
		tw := math.Sin(ph*tau*r.Int(14, 22) + r.Range(0, tau))
		a := math.Min(1, q*6) * pickF(rho > 0.25, 1, rho/0.25)
		if tw > -0.3 {
			c := C[5]
			if rho > 0.6 && tw > 0.6 {
				c = pixelfx.White
			} else if rho > 0.45 {
				c = C[6]
			}
			p.Dot(x, y, c, a)
		}
	})
}

// portalOrbit draws motes riding round just outside the rim, sinking in as they go.
func portalOrbit(p *pixelfx.Painter, s *portalState) {
	C, rx, ry, ph, CY := s.C, s.rx, s.ry, s.ph, s.CY
	for i := range 7 {
		fi := float64(i)
		k := math.Mod(ph*2+fi/7, 1)
		th := math.Mod(fi*2.39+s.S, tau) + ph*tau*2 + k*2.5
		rho := 1.2 + 0.35*math.Mod(fi*0.618, 1) - 0.3*pixelfx.Ease.InQuad(k)
		x, y := math.Cos(th)*rho*rx, CY+math.Sin(th)*rho*ry
		a := math.Min(1, math.Sin(k*pi)*2.2)
		mote(p, x, y, -math.Sin(th)*rx*10, math.Cos(th)*ry*10, pick(i%3 != 0, C[6], pixelfx.White), 0.6, a, 0.3)
	}
}

// portalInfall draws motes and streaks pulled in from round about, spinning faster as they fall; inside they sink into the dark.
func portalInfall(p *pixelfx.Painter, s *portalState) {
	C, ph := s.C, s.ph
	t := ph * portalD
	pixelfx.Cycle(t, portalD, 60, s.S*7+1, func(g *pixelfx.Rng) float64 { return g.Range(0.3, 0.5) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		a0, d0 := r.Range(0, tau), r.Range(1.8, 3)
		spin := r.Range(0.8, 1.8) * pickF(r.Chance(0.8), 1, 0.35)
		size, hue := r.Range(0.8, 1.5), r.Next()
		pos := func(q float64) (float64, float64) { return s.at(a0+spin*q*q, d0*(1-math.Pow(q, 1.4))) }
		q := age / life
		x, y := pos(q)
		x2, y2 := pos(math.Min(1, q+0.015))
		vx, vy := (x2-x)/(0.015*life), (y2-y)/(0.015*life)
		rho, R0, fade := d0*(1-math.Pow(q, 1.4)), s.rim(a0+spin*q*q), math.Min(1, q*5)
		if rho > R0+0.06 {
			c := C[4]
			switch {
			case hue < 0.25:
				c = pixelfx.White
			case hue < 0.55:
				c = C[6]
			case hue < 0.8:
				c = C[5]
			}
			mote(p, x, y, vx, vy, c, pickF(hue < 0.8, size, size*1.4), fade, pickF(hue < 0.8, 0.4, 0.15))
		} else if rho > 0.3 {
			p.Dot(x, y, pick(rho > 0.65, C[6], C[5]), rho)
		}
	})
	pixelfx.Cycle(t, portalD, 12, s.S*7+2, func(g *pixelfx.Rng) float64 { return g.Range(0.36, 0.5) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		a0, d0, spin := r.Range(0, tau), r.Range(2.3, 3), r.Range(0.2, 0.5)
		qc, q := math.Sqrt(1-1.05/d0), age/life
		pos := func(q float64) (float64, float64) { return s.at(a0+spin*q*q, d0*(1-q*q)) }
		if q < qc {
			x, y := pos(q)
			x2, y2 := pos(q + 0.015)
			streak(p, x, y, (x2-x)/(0.015*life), (y2-y)/(0.015*life), C[6], math.Min(1, q*5), 7, 0.4)
			return
		}
		// it hits the rim: a pinprick of white
		k := 1 - (q-qc)/(1-qc)
		x, y := pos(qc)
		if k > 0.55 {
			p.Px(x, y, pixelfx.White)
		}
		p.Glow(x, y, 7, C[6], 0.6*k*k)
	})
}

// portalSkaters draws grit and sparks skating in along the floor, lifting into the foot of the rift.
func portalSkaters(p *pixelfx.Painter, s *portalState) {
	C, ph := s.C, s.ph
	t, foot := ph*portalD, s.CY+s.ry
	pixelfx.Cycle(t, portalD, 14, s.S*7+3, func(g *pixelfx.Rng) float64 { return g.Range(0.7, 1.1) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		side := r.Sign()
		x0 := side * r.Range(22, 50)
		y0 := -1 - r.Range(0, 1.5)
		q := age / life
		e := pixelfx.Ease.InCubic(q)
		x, y := x0*(1-e), y0+(foot+2)*pixelfx.Smooth(0.55, 1, q)
		xb, yb := x0*(1-pixelfx.Ease.InCubic(math.Max(0, q-0.04))), y0+(foot+2)*pixelfx.Smooth(0.55, 1, q-0.04)
		big := r.Chance(0.3)
		mote(p, x, y, (x-xb)*25, (y-yb)*25, pick(big, C[6], C[5]), pickF(big, 0.9, 0.6), math.Min(1, q*4)*(1-pixelfx.Smooth(0.85, 1, q)), 0.3)
	})
}

func portalFloor(p *pixelfx.Painter, s *portalState) {
	C, pulse := s.C, 0.85+0.15*math.Sin(s.ph*tau*2)
	for x := -36.0; x <= 36; x++ {
		u := math.Abs(x+0.5) / 36
		a := pickF(u < 0.55, 1, 1-(u-0.55)/0.45) * (0.8 + 0.2*pulse)
		c := C[5]
		if u < 0.2 {
			c = pixelfx.White
		} else if u < 0.5 {
			c = C[6]
		}
		p.Dot(x, 0, c, a)
		if u < 0.3 {
			p.Dot(x, 1, pick(u < 0.15, C[5], C[4]), pickF(u < 0.15, 1, 0.5))
		}
	}
}

func drawPortal(p *pixelfx.Painter, t float64, o *pixelfx.Options) {
	s := portalSetup(t, o)
	C, CY, rx, ry := s.C, s.CY, s.rx, s.ry
	pulse := 0.85 + 0.15*math.Sin(s.ph*tau*2)
	portalRift(p, s)
	portalWell(p, s)
	portalOrbit(p, s)
	portalInfall(p, s)
	portalSkaters(p, s)
	// light comes off the rim, not the void: a big soft pool, a halo hugging the rim, hot spots that shine, the floor lit
	p.Glow(0, CY, ry*5, C[4], 0.17*pulse)
	p.Glow(0, CY, ry*2.6, C[4], 0.1*pulse)
	for i := range 16 {
		th := (float64(i) / 16) * tau
		R0 := s.rim(th) * 1.08
		p.Glow(math.Cos(th)*R0*rx, CY+math.Sin(th)*R0*ry, 15, C[4], 0.15*pulse)
	}
	for _, th := range s.spots {
		p.Glow(math.Cos(th)*s.rim(th)*rx, CY+math.Sin(th)*s.rim(th)*ry, 16, C[6], 0.42)
	}
	for i := -3; i <= 3; i++ {
		p.Glow(float64(i*7), -1, 16, C[5], 0.12*pulse)
	}
}
