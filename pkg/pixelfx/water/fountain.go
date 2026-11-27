// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/fountain.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the basin's caustic light (two warped sine nets, light where they cross zero) from Sprite Fusion's open-source bubbles (https://www.spritefusion.com/pixel-art-vfx/bubbles), MIT License, Copyright (c) 2026 Hugo Duprez.

package water

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Fountain is a stone basin whose jet rises from its spout and blooms into glassy arcs that break into beads, fall back and plink into the water with rings. It is registered as "water/fountain".
var Fountain = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Fountain",
		Desc:     "A stone basin; a jet rises from its spout and blooms into glassy arcs that break into beads, fall back and plink into the water with rings.",
		Tags:     []string{"water", "fountain", "jet", "basin", "prop", "ambient", "loop"},
		Size:     [2]int{120, 104},
		Origin:   [2]int{60, 96},
		Duration: fountainDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newFountain,
}

func init() { pixelfx.Register("water/fountain", Fountain) }

const (
	fountainDuration = 2
	fountainSY       = 0.34
	fountainG        = 600
	fountainRim      = 10
	fountainRX       = 36
	fountainIn       = 29.5
	fountainJet      = 54
	fountainArcs     = 12
	// the basin stands on the floor: its base ellipse touches y = 0 at the front; heights are measured from that plane
	fountainBY    = -fountainRX * fountainSY
	fountainLevel = fountainRim - 1.2
)

type fountainArc struct {
	a, vr, vh, tl, ca, sa float64
}

func makeFountainArcs(S float64) []fountainArc {
	out := make([]fountainArc, fountainArcs)
	for i := range fountainArcs {
		a := ((float64(i)+0.5)/fountainArcs)*tau + pixelfx.Rand(float64(i), S)*0.25
		vr := 38 + 14*pixelfx.Rand(float64(i), S+1)
		vh := 30 + 26*pixelfx.Rand(float64(i), S+2)
		tl := (vh + math.Sqrt(vh*vh+2*fountainG*(fountainJet-fountainLevel))) / fountainG
		out[i] = fountainArc{a: a, vr: vr, vh: vh, tl: tl, ca: math.Cos(a), sa: math.Sin(a)}
	}
	return out
}

func fountainAlong(q fountainArc, t float64) (float64, float64, float64) {
	r := q.vr * t
	h := fountainJet + q.vh*t - 0.5*fountainG*t*t
	return q.ca * r, fountainBY + q.sa*r*fountainSY - h, h
}

func fountainInEll(x, y, rx, cy float64) bool {
	dx, dy := x/rx, (y-cy)/(rx*fountainSY)
	return dx*dx+dy*dy <= 1
}

// fountainBasin draws the stone basin: a wide rim lit toward the light with a dark line round both edges, a wall of stone blocks below its near side (lit left, shaded right) standing on the floor.
func fountainBasin(p *pixelfx.Painter, ST []uint32, half int) {
	cy, ry := fountainBY-fountainRim, float64(fountainRX)*fountainSY
	p.Shade(-fountainRX-1, cy-ry-1, fountainRX+1, 1, func(x, y float64, out *pixelfx.Out) uint32 {
		if fountainInEll(x, y, fountainRX, cy) {
			if fountainInEll(x, y, fountainIn, cy) {
				return 0
			}
			near := y > cy
			if near != (half > 0) {
				return 0
			}
			outer := !fountainInEll(x, y-1, fountainRX, cy) || !fountainInEll(x, y+1, fountainRX, cy) || !fountainInEll(x-1, y, fountainRX, cy) || !fountainInEll(x+1, y, fountainRX, cy)
			inner := fountainInEll(x, y-1, fountainIn, cy) || fountainInEll(x, y+1, fountainIn, cy) || fountainInEll(x-1, y, fountainIn, cy) || fountainInEll(x+1, y, fountainIn, cy)
			a := math.Atan2((y-cy)/ry, x/fountainRX)
			lit := -math.Cos(a)*0.65 - math.Sin(a)*0.45
			switch {
			case outer:
				out.C = ST[0]
			case inner:
				if y < cy {
					out.C = ST[2]
				} else {
					out.C = ST[1]
				}
			case lit > 0.4:
				out.C = ST[5]
			case lit > -0.05:
				out.C = ST[4]
			case lit > -0.45:
				out.C = ST[3]
			default:
				out.C = ST[2]
			}
			return 0
		}
		// the wall, under the near half of the rim
		if half < 0 || math.Abs(x) >= fountainRX {
			return 0
		}
		dx := x / fountainRX
		top := cy + math.Sqrt(1-dx*dx)*ry
		base := top + fountainRim
		if y < top || y > base {
			return 0
		}
		u := math.Asin(pixelfx.Clamp(dx)) * fountainRX * 0.62
		row := y - top
		course := 0.0
		if row > fountainRim*0.5 {
			course = 1
		}
		joint := pixelfx.Wrap((u+40+course*4.5)/9, 1) < 0.1
		switch {
		case y > base-1 || math.Abs(x) > fountainRX-1 || row < 1:
			out.C = ST[0]
		case joint || math.Abs(row-fountainRim*0.5) < 0.5:
			out.C = ST[1]
		case dx < -0.4:
			out.C = ST[4]
		case dx < 0.3:
			out.C = ST[3]
		default:
			out.C = ST[2]
		}
		if row >= 1 && row < 2 && !joint {
			if dx < 0 {
				out.C = ST[2]
			} else {
				out.C = ST[1]
			}
		}
		return 0
	})
}

// fountainPool draws the water in the basin: dark and see-through, a lit band at the far side, caustic light swimming in it.
func fountainPool(p *pixelfx.Painter, C []uint32, ph float64) {
	rx := fountainIn
	ry, cy, T := rx*fountainSY, fountainBY-fountainLevel, ph*tau
	for Y := math.Floor(cy - ry); Y < cy+ry; Y++ {
		for X := -math.Ceil(rx); X < rx; X++ {
			x, y := X+0.5, Y+0.5
			dx, dy := x/rx, (y-cy)/ry
			d := dx*dx + dy*dy
			if d > 1 {
				continue
			}
			c, a := C[1], 0.85
			if dy < -0.55 {
				c, a = C[3], 0.8
			}
			p.Blend(x, y, c, a)
			u := x*0.25 + 1.6*math.Sin(y*0.6+T)
			w := y*0.7 + 1.6*math.Sin(x*0.14-T*2)
			f := math.Max(0, 1-math.Abs(math.Sin(u)+math.Sin(w))*2.6) * (1 - d)
			if f > 0.35 {
				p.Light(x, y, C[5], 0.3*f)
			}
		}
	}
}

// drawFountainArc draws one arc: a glassy stream that thins as it falls and breaks into beads at the end; beads slide down it.
func drawFountainArc(p *pixelfx.Painter, C []uint32, q fountainArc, ph float64, back bool) {
	hasPrev := false
	var px, py float64
	for tm := 0.0; tm <= q.tl; tm += 0.012 {
		x, y, _ := fountainAlong(q, tm)
		u := tm / q.tl
		if hasPrev && u < 0.66 {
			var c uint32
			if back {
				if u < 0.25 {
					c = C[5]
				} else {
					c = C[4]
				}
			} else if u < 0.2 {
				c = pixelfx.White
			} else if u < 0.45 {
				c = C[6]
			} else {
				c = C[5]
			}
			p.Line(px, py, x, y, c, 1)
		}
		px, py, hasPrev = x, y, true
	}
	// the beads it breaks into, sliding down at the water's own speed
	backRamp := []uint32{C[0], C[1], C[2], C[2], C[3], C[4], C[5], C[5]}
	for k := 0; k < 6; k++ {
		u := math.Mod(ph*2+float64(k)/6, 1)*0.42 + 0.58
		tm := u * q.tl
		x, y, _ := fountainAlong(q, tm)
		vy := (q.vh-fountainG*tm)*-1 + q.sa*q.vr*fountainSY
		s := 1.0
		ramp := C
		if back {
			ramp = backRamp
		} else if k%3 == 0 {
			s = 2
		}
		bead(p, x, y, q.ca*q.vr, vy, s, ramp, 1)
	}
	// and the ones sliding along the stream itself catch the light
	g := math.Mod(ph*4+q.a, 1) * 0.6
	gx, gy, _ := fountainAlong(q, g*q.tl)
	p.Px(gx, gy, pixelfx.White)
	if !back {
		p.Glow(gx, gy, 3, C[5], 0.3)
	}
}

var cross4 = [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

func newFountain(o *pixelfx.Options) pixelfx.Instance {
	C, S, ST := pixelfx.Fit(o.Palette(), 8), o.Seed(), rock()
	D := float64(fountainDuration)
	Q := makeFountainArcs(S)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			fountainBasin(p, ST, -1)
			fountainPool(p, C, ph)
			// the arcs on the far side, dimmer, behind the jet
			for _, q := range Q {
				if q.sa < 0 {
					drawFountainArc(p, C, q, ph, true)
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			T := ph * tau
			// the spout: a little stone stem in the middle of the basin
			p.Shade(-3.5, fountainBY-16, 3.5, fountainBY-fountainLevel+0.5, func(x, y float64, out *pixelfx.Out) uint32 {
				w := 2.2
				if y < fountainBY-14 {
					w = 3.4
				}
				if math.Abs(x) > w {
					return 0
				}
				switch {
				case math.Abs(x) > w-0.8 || y < fountainBY-15.5:
					out.C = ST[0]
				case x < -0.6:
					out.C = ST[4]
				case x < 0.8:
					out.C = ST[3]
				default:
					out.C = ST[2]
				}
				return 0
			})
			// the jet: a glassy column, white down its middle, wobbling a little at the top
			p.Shade(-3, fountainBY-fountainJet-1, 3, fountainBY-16, func(x, y float64, out *pixelfx.Out) uint32 {
				v := (fountainBY - 16 - y) / (fountainJet - 16)
				sway := math.Sin(T*2+v*5) * 0.5 * v
				w := 1.3 + 0.5*v + 0.3*math.Sin(T*3+y*0.4)
				q := (x - sway) / w
				if math.Abs(q) > 1 {
					return 0
				}
				n := pixelfx.TileNoise(x*0.9, y*0.15+ph*16, 0, 16, S)
				switch {
				case q < -0.4:
					out.C = C[6]
				case q < 0.35:
					if n > -0.3 {
						out.C = pixelfx.White
					} else {
						out.C = C[6]
					}
				default:
					out.C = C[5]
				}
				return 0
			})
			// the bloom where it opens out at the top
			p.Circle(0, fountainBY-fountainJet-1, 2.4, C[6], 1)
			p.Circle(-0.4, fountainBY-fountainJet-1.5, 1.4, pixelfx.White, 1)
			p.Glow(0, fountainBY-fountainJet, 22, C[5], 0.46+0.06*math.Sin(T*4))
			p.Glow(0, fountainBY-fountainJet-1, 8, pixelfx.White, 0.3)
			// glints wink along the arcs
			pixelfx.Cycle(t, D, 10, S+30, func(g *pixelfx.Rng) float64 { return g.Range(0.2, 0.35) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				q := Q[int(math.Floor(r.Next()*fountainArcs))]
				x, y, _ := fountainAlong(q, r.Range(0.1, 0.7)*q.tl)
				k := math.Sin((pi * age) / life)
				if q.sa < -0.3 {
					return
				}
				p.Px(x, y, pixelfx.White)
				if k > 0.6 {
					for _, d := range cross4 {
						p.Px(x+d[0], y+d[1], C[6])
					}
				}
				p.Glow(x, y, 5, C[5], 0.4*k)
			})
			// foam where the jet leaves the spout
			for i := 0; i < 4; i++ {
				c := pixelfx.White
				if i%2 != 0 {
					c = C[6]
				}
				p.Px(math.Sin(T*3+float64(i)*1.7)*2.5, fountainBY-16-float64(i%2), c)
			}
			// the arcs on the near side, in front of the jet
			for _, q := range Q {
				if q.sa >= 0 {
					drawFountainArc(p, C, q, ph, false)
				}
			}
			// where they come down: near halves of the rings, flecks and tiny crowns thrown up, light on the water
			for i, q := range Q {
				f := math.Mod(ph*2+pixelfx.Rand(float64(i), S+9), 1)
				x, y, _ := fountainAlong(q, q.tl)
				ring(p, x, y, 2+f*8, fountainSY, 0.8*math.Pow(1-f, 1.3), C, ringOpts{n: 1, half: 1, seed: float64(i)})
				g := math.Mod(ph*6+pixelfx.Rand(float64(i), S+10), 1)
				if g < 0.15 {
					p.Px(x, y-1, pixelfx.White)
				}
				if g < 0.3 && q.sa > -0.2 {
					p.Line(x, y-1, x+(pixelfx.Rand(float64(i), S+11)-0.5)*2, y-1-3*(1-g/0.3), C[6], 1)
				}
				p.Glow(x, y, 6, C[5], 0.18*(1-g))
			}
			fountainBasin(p, ST, 1)
			// a fine mist hangs round the bloom
			pixelfx.Cycle(t, D, 16, S+20, func(g *pixelfx.Rng) float64 { return g.Range(0.8, 1.4) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				a := r.Range(0, tau)
				rr := r.Range(4, 20) + k*6
				x := math.Cos(a) * rr
				y := fountainBY - fountainJet + r.Range(-4, 16) + k*10
				if k > 0.75 && r.Next() < (k-0.75)*4 {
					return
				}
				c := C[6]
				if r.Next() < 0.4 {
					c = pixelfx.White
				}
				p.Px(x, y, c)
			})
			// light: the jet, and the basin water lit from above
			p.GlowLine(0, fountainBY-16, 0, fountainBY-fountainJet, 10, C[5], 0.18)
			p.Glow(0, fountainBY-fountainLevel-2, 46, C[4], 0.26+0.03*math.Sin(T*2))
			p.Glow(0, fountainBY-fountainLevel-20, 30, C[5], 0.12)
		},
	}
}
