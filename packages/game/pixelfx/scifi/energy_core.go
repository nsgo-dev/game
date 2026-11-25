// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/energy-core.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source energy orb (https://www.spritefusion.com/pixel-art-vfx/energy) for the plasma ball (lit rim, filaments from a white-hot heart, motes, zaps that spark), MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// EnergyCore is a plasma core beating in a steel cage: it swells and throws a shock ring on each beat, two tilted rings orbit it, and arcs jump to the bars and spark. It is registered as "scifi/energy-core".
var EnergyCore = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Energy core",
		Desc:     "A plasma core beating in a steel cage: it swells and throws a shock ring on each beat, two tilted rings orbit it, and arcs jump to the bars and spark.",
		Tags:     []string{"scifi", "reactor", "core", "power", "plasma", "machine", "prop", "ground", "loop"},
		Size:     [2]int{112, 100},
		Origin:   [2]int{56, 88},
		Duration: energyDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0516", "#3d0a33", "#701560", "#b0228f", "#f53cd1", "#ff86e6", "#ffd2f6", "#ffffff")},
			{Name: "metal", Default: pixelfx.Palette("#0e0d14", "#1e1c27", "#312e3e", "#4b4759", "#6f6a82", "#9d98b0")},
			{Name: "seed", Default: pixelfx.Num(35)},
		},
	},
	New: newEnergyCore,
}

func init() { pixelfx.Register("scifi/energy-core", EnergyCore) }

const (
	energyDuration = 2.4
	energyCY       = -36.0
	energyR0       = 11.0
	energyRC       = 24.0
	energyTopY     = -64.0
	energyBotY     = -9.0
)

var (
	energySlot = 0.1
	energyNS   = int(pixelfx.Round(energyDuration / energySlot))
)

type energyBeat struct {
	b, k   float64
	strong bool
}

// two heartbeats a loop, each a strong thump and a softer echo
var energyBeats = []energyBeat{
	{0.05, 1, true},
	{0.25, 0.5, false},
	{1.25, 1, true},
	{1.45, 0.5, false},
}

func energyEnv(a float64) float64 {
	if a < 0.03 {
		return a / 0.03
	}
	return math.Exp(-(a - 0.03) / 0.12)
}

func energyPulseAt(t float64) float64 {
	m := 0.0
	for _, bt := range energyBeats {
		m = math.Max(m, bt.k*energyEnv(since(t, bt.b, energyDuration)))
	}
	return m
}

type energyBar struct{ x, z float64 }

// the cage's bars round the core: x, depth (> 0 in front)
var energyBars = func() []energyBar {
	out := make([]energyBar, 6)
	for i := range 6 {
		a := (float64(i)/6)*tau + pi/6
		out[i] = energyBar{x: energyRC * math.Sin(a), z: math.Cos(a)}
	}
	return out
}()

// energyRingPt returns [x, y, depth] for a point on an orbiting ring, tilted and turning.
func energyRingPt(r, tilt, spin, a float64) (float64, float64, float64) {
	x, z0 := r*math.Cos(a), r*math.Sin(a)
	y, z := -z0*math.Sin(tilt), z0*math.Cos(tilt)
	return x*math.Cos(spin) + z*math.Sin(spin), energyCY - y, -x*math.Sin(spin) + z*math.Cos(spin)
}

type energyRingSpec struct {
	r, tilt float64
	spin    func(t float64) float64
}

var energyRings = []energyRingSpec{
	{r: 17, tilt: 1.15, spin: func(t float64) float64 { return tau * (t / energyDuration) }},
	{r: 19, tilt: -0.9, spin: func(t float64) float64 { return -tau*2*(t/energyDuration) + 1 }},
}

type energyArc struct {
	s    int
	path [][2]float64
	fk   [][][2]float64
}

// energyPlinth draws the plinth on the floor: a steel drum with a lit top and a row of status lights.
func energyPlinth(p *pixelfx.Painter, C, H []uint32, t, pulse float64) {
	p.Shade(-30, -10, 30, 2, func(x, y float64, out *pixelfx.Out) uint32 {
		top := (x/29)*(x/29)+((y+7)/4)*((y+7)/4) <= 1
		side := math.Abs(x) <= 29 && y > -7 && y <= 0
		if !top && !side {
			return 0
		}
		if top {
			switch {
			case (x/29)*(x/29)+((y+7)/4)*((y+7)/4) > 0.82:
				out.C = H[0]
			case y < -8:
				out.C = H[5]
			case x < 0:
				out.C = H[4]
			default:
				out.C = H[3]
			}
		} else {
			switch {
			case math.Abs(x) > 28 || y == 0:
				out.C = H[0]
			case y == -6:
				out.C = H[4]
			case x < -14:
				out.C = H[3]
			case x > 14:
				out.C = H[1]
			default:
				out.C = H[2]
			}
		}
		return 0
	})
	for i := range 7 {
		x := -18 + float64(i)*6
		k := 0.5 + 0.5*math.Cos(tau*(2*(t/energyDuration)-float64(i)/7))
		c := C[2]
		if k > 0.75 {
			c = pixelfx.White
		} else if k > 0.4 {
			c = C[5]
		}
		p.Px(x, -3, c)
		if k > 0.6 {
			p.Glow(x, -3, 4, C[4], 0.3*k)
		}
	}
	p.Glow(0, -8, 26, C[4], 0.25+0.3*pulse)
}

// energyBarDraw draws a cage bar: steel, lit on the face toward the core.
func energyBarDraw(p *pixelfx.Painter, b energyBar, C, H []uint32, pulse float64, back bool) {
	lit := C[5]
	if back {
		lit = C[3]
	} else if pulse > 0.5 {
		lit = C[6]
	}
	inward := -pixelfx.Sign(b.x)
	for y := energyTopY + 2; y < energyBotY; y++ {
		for i := -1.0; i <= 1; i++ {
			face := i == inward
			var c uint32
			if back {
				if face {
					c = lit
				} else {
					c = H[1]
				}
			} else {
				switch {
				case face:
					c = lit
				case i == 0:
					c = H[3]
				default:
					c = H[1]
				}
			}
			p.Px(b.x+i, y, c)
		}
	}
}

// energyHoop draws the cage's rings top and bottom: the far half or the near half of a steel hoop.
func energyHoop(p *pixelfx.Painter, y, rx float64, H, C []uint32, near bool, pulse float64) {
	n := int(math.Ceil(rx * 8))
	for i := range n {
		g := (float64(i) / float64(n)) * tau
		s := math.Sin(g)
		if (s >= 0) != near {
			continue
		}
		x, yy := math.Cos(g)*rx, y+s*4
		c0, c1 := H[1], H[0]
		if near {
			if s > 0.6 {
				c0 = H[4]
			} else {
				c0 = H[3]
			}
			c1 = H[1]
		}
		p.Px(x, yy, c0)
		p.Px(x, yy+1, c1)
		if near && s < 0.3 {
			c2 := C[3]
			if pulse > 0.5 {
				c2 = C[5]
			}
			p.Px(x, yy-1, c2)
		}
	}
}

// energyCoreDraw draws the core: dark plasma held in a hard rim, thin bright bands churning round a white-hot heart, filaments flickering out from it; a thump floods it with light.
func energyCoreDraw(p *pixelfx.Painter, t float64, C []uint32, R, pulse, seed float64) {
	ph := t / energyDuration
	p.Shade(-R-2, energyCY-R-2, R+2, energyCY+R+2, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x, y-energyCY
		d := math.Sqrt(dx*dx+dy*dy) / R
		if d > 1.1 {
			return 0
		}
		if d > 1 {
			out.C = C[2]
			return 0
		}
		if d > 0.9 {
			if dy < -R*0.3 && dx < R*0.3 {
				out.C = C[6]
			} else {
				out.C = C[4]
			}
			return 0
		}
		a := math.Atan2(dy, dx)
		n := 0.5 + 0.5*pixelfx.TileFbm((a/tau)*6+ph*6+d*1.5, d*2.2-ph*4, 6, 4, seed, 2)
		band := math.Abs(n-0.5) < 0.05+0.05*pulse
		heat := (1-d)*(1-d) + pulse*0.45
		if band {
			switch {
			case heat > 0.5:
				out.C = pixelfx.White
			case heat > 0.2:
				out.C = C[6]
			default:
				out.C = C[5]
			}
		} else {
			switch {
			case heat > 0.7:
				out.C = C[6]
			case heat > 0.42:
				out.C = C[5]
			case heat > 0.2:
				out.C = C[3]
			case n > 0.5:
				out.C = C[2]
			default:
				out.C = C[1]
			}
		}
		return 0
	})
	// filaments, redrawn every other frame
	f := math.Floor(t * 15)
	r := pixelfx.RngOf(seed, f, 5)
	for i := range 3 {
		a := (float64(i)/3)*tau + f*0.7 + r.Range(-0.4, 0.4)
		pts := pixelfx.Bolt(r, 0, energyCY, math.Cos(a)*R*0.88, energyCY+math.Sin(a)*R*0.88, 0.32, 3)
		p.PolylineFn(pts, func(u float64) (uint32, float64) {
			if u < 0.4 {
				return pixelfx.White, 1
			}
			return C[6], 1
		})
		end := pts[len(pts)-1]
		p.Px(end[0], end[1], pixelfx.White)
		p.Glow(end[0], end[1], 4, C[5], 0.4)
	}
	p.Circle(0, energyCY, 1.8+pulse*2, pixelfx.White, 1)
	p.CircleFn(0, energyCY, 3+pulse*2, func(d, _ float64) (uint32, float64) {
		if d > 0.6 {
			return C[6], 0.0001
		}
		return pixelfx.White, 0.0001
	})
	// a glassy highlight top left
	p.Px(-R*0.45, energyCY-R*0.55, pixelfx.White)
	p.Px(-R*0.45+1, energyCY-R*0.55, C[6])
}

func energyRingDraw(p *pixelfx.Painter, t float64, rg energyRingSpec, C []uint32, front bool, pulse float64) {
	spin, bead := rg.spin(t), (t/energyDuration)*tau*3
	n := int(math.Ceil(rg.r * 9))
	for i := range n {
		a := (float64(i) / float64(n)) * tau
		x, y, z := energyRingPt(rg.r, rg.tilt, spin, a)
		if (z >= 0) != front {
			continue
		}
		u := pixelfx.Wrap((a-bead)/tau, 1)
		head := u > 0.9
		var c uint32
		switch {
		case head:
			c = pixelfx.White
		case !front:
			c = C[3]
		case z > 0.5*rg.r || pulse > 0.6:
			c = C[6]
		default:
			c = C[5]
		}
		p.Px(x, y, c)
	}
	if front {
		x, y, _ := energyRingPt(rg.r, rg.tilt, spin, bead)
		p.Glow(x, y, 6, C[5], 0.5)
	}
}

func newEnergyCore(o *pixelfx.Options) pixelfx.Instance {
	C, H, seed := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("metal"), 6), o.Seed()
	rn := pixelfx.NewRng(seed)
	g := newGhosts(energyDuration, ghostOpts{
		ground: zeroGround,
		sparks: []uint32{pixelfx.White, C[6], C[5], C[4]},
		haze:   C[5],
		hasBox: true,
		box:    [4]float64{-54, -98, 54, 11},
	})
	var frontBars []energyBar
	for _, b := range energyBars {
		if b.z > -0.2 {
			frontBars = append(frontBars, b)
		}
	}
	// arcs: each slot of the loop may fire one or two, more just after a thump
	var arcs []energyArc
	for s := range energyNS {
		ts := float64(s) * energySlot
		hot := false
		for _, bt := range energyBeats {
			if bt.strong && since(ts, bt.b, energyDuration) < 0.16 {
				hot = true
				break
			}
		}
		count := 1
		if hot {
			count = 2
		}
		for range count {
			if !hot && !rn.Chance(0.4) {
				continue
			}
			bar := pixelfx.Pick(rn, frontBars)
			ty := rn.Range(energyTopY+8, energyBotY-6)
			ang := math.Atan2(ty-energyCY, bar.x)
			sx, sy := math.Cos(ang)*energyR0, energyCY+math.Sin(ang)*energyR0
			path := pixelfx.Bolt(rn, sx, sy, bar.x-pixelfx.Sign(bar.x)*1.5, ty, 0.3, 4)
			fk := pixelfx.Forks(rn, path, pixelfx.ForkOpts{Count: int(rn.Int(0, 1)), Len: 7, Jag: 0.35, Spread: 0.9})
			arcs = append(arcs, energyArc{s: s, path: path, fk: fk})
			for range 5 {
				a := math.Atan2(ty-sy, bar.x-sx) + pi + rn.Range(-1.2, 1.2)
				sp := rn.Range(60, 170)
				g.add(ts, pixelfx.Spark, bar.x, ty, math.Cos(a)*sp, math.Sin(a)*sp-40, rn.Range(0.2, 0.5), pixelfx.White, 1, 0)
			}
		}
	}
	// each thump throws motes off the core
	for _, bt := range energyBeats {
		if !bt.strong {
			continue
		}
		for range 12 {
			a, sp := rn.Range(0, tau), rn.Range(40, 110)
			life := rn.Range(0.25, 0.5)
			col := pixelfx.Pick(rn, []uint32{pixelfx.White, C[6], C[5]})
			g.add(bt.b+0.02, pixelfx.Mote, math.Cos(a)*energyR0, energyCY+math.Sin(a)*energyR0, math.Cos(a)*sp, math.Sin(a)*sp, life, col, 0.7, 0)
		}
	}

	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			pulse := energyPulseAt(t)
			energyHoop(p, energyBotY, energyRC, H, C, false, pulse)
			energyHoop(p, energyTopY, energyRC, H, C, false, pulse)
			for _, b := range energyBars {
				if b.z < 0 {
					energyBarDraw(p, b, C, H, pulse, true)
				}
			}
			for _, rg := range energyRings {
				energyRingDraw(p, t, rg, C, false, pulse)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			pulse := energyPulseAt(t)
			R := energyR0 * (1 + 0.14*pulse)
			energyPlinth(p, C, H, t, pulse)
			energyCoreDraw(p, t, C, R, pulse, seed)
			for _, rg := range energyRings {
				energyRingDraw(p, t, rg, C, true, pulse)
			}
			for _, b := range energyBars {
				if b.z >= 0 {
					energyBarDraw(p, b, C, H, pulse, false)
				}
			}
			energyHoop(p, energyBotY, energyRC, H, C, true, pulse)
			energyHoop(p, energyTopY, energyRC, H, C, true, pulse)
			// the cap: a steel dome with an emitter on top
			p.Shade(-15, energyTopY-7, 15, energyTopY, func(x, y float64, out *pixelfx.Out) uint32 {
				d := (x/14.5)*(x/14.5) + ((y-energyTopY)/6.5)*((y-energyTopY)/6.5)
				if d > 1 {
					return 0
				}
				l := -x*0.05 - (y-energyTopY)*0.12
				switch {
				case d > 0.8:
					out.C = H[0]
				case l > 0.55:
					out.C = H[5]
				case l > 0.3:
					out.C = H[4]
				default:
					out.C = H[3]
				}
				return 0
			})
			topCol := C[5]
			if pulse > 0.4 {
				topCol = pixelfx.White
			}
			p.Px(0, energyTopY-7, topCol)
			p.Glow(0, energyTopY-7, 5, C[4], 0.4+0.4*pulse)
			// the arcs firing now
			s := int(math.Floor(t/energySlot)) % energyNS
			k := 1 - (t-math.Floor(t/energySlot)*energySlot)/energySlot
			for _, a := range arcs {
				if a.s != s {
					continue
				}
				strokeArc(p, a.path, [3]uint32{C[2], C[5], C[4]}, k, 1)
				for _, fk := range a.fk {
					strokeArc(p, fk, [3]uint32{C[2], C[5], C[4]}, k, 0.6)
				}
				end := a.path[len(a.path)-1]
				p.Glow(end[0], end[1], 8, C[5], 0.6*k)
			}
			// each thump: a shock ring rolls out from the core
			for _, bt := range energyBeats {
				a := since(t, bt.b, energyDuration)
				if !bt.strong || a > 0.45 {
					continue
				}
				q := a / 0.45
				r := energyR0 + 34*pixelfx.Ease.OutCubic(q)
				c := C[5]
				if q < 0.25 {
					c = pixelfx.White
				} else if q < 0.5 {
					c = C[6]
				}
				p.Ring(0, energyCY, r, math.Max(1, 3*(1-q)), c, 1.1-q)
				p.GlowRing(0, energyCY, r, 6, C[4], 0.35*(1-q))
			}
			g.at(t).Draw(p, pixelfx.LayerBoth)
			// light: the core's heat, its white heart, the steel lit round it
			p.Glow(0, energyCY, 46, C[4], 0.2+0.3*pulse)
			p.Glow(0, energyCY, 9, pixelfx.White, 0.35+0.4*pulse)
		},
	}
}
