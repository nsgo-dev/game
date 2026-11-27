// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/magic-circle-cast.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The column of light (banded across, strands wandering in it, pulses and halos climbing it, stars thrown off, rings shocking out across the floor) is adapted from Sprite Fusion's open-source holy light (https://www.spritefusion.com/pixel-art-vfx/holy-light), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// the circle on the floor (plane px; the floor squashes depth by mcSY), the column, the beats; variables so expressions with them round step by step as JavaScript's do
var (
	mcR     = 40.0
	mcR2    = 35.0
	mcRI    = 24.0
	mcRR    = 30.0
	mcSY    = 0.3
	mcTop   = 192.0
	mcW     = 11.0
	mcNRune = 8

	mcPen   = [2]float64{0.04, 0.42}
	mcPen2  = [2]float64{0.1, 0.48}
	mcInner = [2]float64{0.3, 0.6}
	mcStar  = [2]float64{0.52, 0.88}
	mcRunes = [2]float64{0.9, 1.3}

	mcFlash = 1.52
	mcBreak = 2.42
	mcGone  = 3.45
	mcEnd   = 3.9
)

// MagicCircleCast draws a circle on the floor, lights its runes one by one, flashes, and fires a column of light that breaks apart into glints. It is registered as "arcane/magic-circle-cast".
var MagicCircleCast = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Magic circle cast",
		Desc:     "A circle draws itself on the floor, runes ignite one by one, it flashes, and a column of light fires up and breaks apart into glints.",
		Tags:     []string{"arcane", "magic", "circle", "rune", "cast", "column", "summon", "ground", "one-shot"},
		Size:     [2]int{200, 220},
		Origin:   [2]int{100, 192},
		Duration: mcEnd,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newMagicCircleCast,
}

func init() { pixelfx.Register("arcane/magic-circle-cast", MagicCircleCast) }

func mcWrap(a float64) float64 { return math.Mod(math.Mod(a, tau)+tau, tau) }

func mcFromFront(g float64) float64 { return math.Abs(math.Atan2(math.Sin(g-pi/2), math.Cos(g-pi/2))) }

// mcOuterAt and mcInnerAt are when the pens pass angle g: the outer rings are drawn from the front round both sides to the back, the inner ring from the back all the way round.
func mcOuterAt(g float64, w [2]float64) float64 { return w[0] + (mcFromFront(g)/pi)*(w[1]-w[0]) }

func mcInnerAt(g float64) float64 {
	return mcInner[0] + (mcWrap(g+pi/2)/tau)*(mcInner[1]-mcInner[0])
}

func mcRuneAngle(k int) float64 { return -pi/2 + ((float64(k)+0.5)*tau)/float64(mcNRune) }

func mcRuneLit(k int) float64 {
	return mcRunes[0] + (float64(k)*(mcRunes[1]-mcRunes[0]))/float64(mcNRune-1)
}

func mcRuneOut(k int) float64 { return mcBreak + 0.2 + float64(k)*0.08 }

// mcLevel is how bright the drawn circle burns: settled while it's drawn, swelling as the runes light, white at the flash.
func mcLevel(t float64) float64 {
	switch {
	case t < mcRunes[0]:
		return 0.55
	case t < mcFlash:
		return 0.55 + 0.4*pixelfx.Smooth(mcRunes[0], mcFlash, t)
	case t < mcFlash+0.07:
		return 1.2
	case t < mcBreak:
		return 0.92
	}
	return 0.92 * (1 - pixelfx.Smooth(mcBreak, mcGone, t))
}

type mcStrand struct{ o, w, f float64 }

// mcEdge is one edge of the hexagram, drawn by a pen from p0 to p1 over [t0, t0 + dur].
type mcEdge struct {
	p0, p1  [2]float64
	t0, dur float64
}

func newMagicCircleCast(o *pixelfx.Options) pixelfx.Instance {
	C, S := pixelfx.Fit(o.Palette(), 8), o.Seed()
	Rg := pixelfx.NewRng(S)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: Rg, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	COL := [5]uint32{pixelfx.White, C[6], C[5], C[4], C[3]}
	var strands [9]mcStrand
	for i := range strands {
		s := mcStrand{o: Rg.Range(-0.85, 0.85)}
		s.w = Rg.Range(0.04, 0.1)
		s.f = Rg.Range(1, 3)
		strands[i] = s
	}
	// the hexagram: two triangles inside the inner ring, each drawn edge by edge
	tri := func(a0 float64) [3][2]float64 {
		var out [3][2]float64
		for k := range 3 {
			a := a0 + (float64(k)*tau)/3
			out[k] = [2]float64{math.Cos(a) * mcRI, math.Sin(a) * mcRI}
		}
		return out
	}
	A, Bt := tri(-pi/2), tri(pi/2)
	var edges []mcEdge
	for k := range 3 {
		for _, T := range [2][3][2]float64{A, Bt} {
			edges = append(edges, mcEdge{p0: T[k], p1: T[(k+1)%3], t0: mcStar[0] + (float64(k)*(mcStar[1]-mcStar[0]))/3, dur: (mcStar[1] - mcStar[0]) / 3})
		}
	}
	t, flashed, broke := 0.0, false, false
	var out [8]bool

	lineCol := func(lvl, age float64) uint32 {
		switch {
		case age < 0.05:
			return pixelfx.White
		case age < 0.16:
			return C[6]
		case lvl > 1:
			return pixelfx.White
		case lvl > 0.8:
			return C[6]
		case lvl > 0.45:
			return C[5]
		case lvl > 0.28:
			return C[4]
		}
		return C[3]
	}
	lineA := func(lvl float64) float64 { return pixelfx.Clamp(lvl / 0.3) }

	// a straight line on the floor plane, drawn by a pen from p0 to p1 over [t0, t0 + dur]; only the far (v < 0) or near part
	planeLine := func(p *pixelfx.Painter, p0, p1 [2]float64, t0, dur, lvl float64, far bool) {
		s := pixelfx.Clamp((t - t0) / dur)
		if s <= 0 {
			return
		}
		u0, u1 := 0.0, s
		v0, v1 := p0[1], p1[1]
		if (v0 < 0) != (v1 < 0) {
			uc := v0 / (v0 - v1)
			if (v0 < 0) == far {
				u1 = math.Min(u1, uc)
			} else {
				u0 = uc
			}
		} else if (v0 < 0) != far {
			return
		}
		if u1 <= u0 {
			return
		}
		P := func(u float64) (float64, float64) { return p0[0] + (p1[0]-p0[0])*u, (v0 + (v1-v0)*u) * mcSY }
		xa, ya := P(u0)
		xb, yb := P(u1)
		a := lineA(lvl)
		p.LineFn(xa, ya, xb, yb, func(q float64) (uint32, float64) { return lineCol(lvl, t-(t0+(u0+(u1-u0)*q)*dur)), a })
	}

	circle := func(p *pixelfx.Painter, far bool) {
		if t >= mcGone {
			return
		}
		lvl, a := mcLevel(t), lineA(mcLevel(t))
		keep := func(g float64) bool { return math.Sin(g) >= 0 }
		if far {
			keep = func(g float64) bool { return math.Sin(g) < 0 }
		}
		ring := func(r float64, when func(g float64) float64, chase func(g float64) bool) {
			floorRing(p, 0, 0, r, mcSY, func(g float64) uint32 {
				if chase != nil && chase(g) {
					return pixelfx.White
				}
				return lineCol(lvl, t-when(g))
			}, func(g float64) float64 { return pickF(t < when(g), 0, a) }, keep)
		}
		// from the moment the runes start to light, two sparks of light chase round the outer ring, faster and faster
		var chase func(g float64) bool
		if t > mcRunes[0] && t < mcBreak {
			e := t - mcRunes[0]
			run := e*5 + e*e*4
			chase = func(g float64) bool {
				for _, o := range [2]float64{0, pi} {
					if math.Abs(math.Atan2(math.Sin(g-run-o), math.Cos(g-run-o))) < 0.22 {
						return true
					}
				}
				return false
			}
		}
		ring(mcR, func(g float64) float64 { return mcOuterAt(g, mcPen) }, chase)
		ring(mcR2, func(g float64) float64 { return mcOuterAt(g, mcPen2) }, nil)
		ring(mcRI, mcInnerAt, nil)
		// ticks across the band between the two outer rings
		for j := range 32 {
			g := ((float64(j) + 0.5) * tau) / 32
			if math.Sin(g) < 0 != far {
				continue
			}
			when := mcOuterAt(g, mcPen2) + 0.03
			if t < when {
				continue
			}
			p.Line(math.Cos(g)*mcR2, math.Sin(g)*mcR2*mcSY, math.Cos(g)*mcR, math.Sin(g)*mcR*mcSY, lineCol(lvl, t-when), a*pickF(j%2 != 0, 0.6, 1))
		}
		for _, e := range edges {
			planeLine(p, e.p0, e.p1, e.t0, e.dur, lvl, far)
		}
	}

	// the runes stand on the circle; dim when the inner pen passes, lit one by one, out one by one
	runes := func(p *pixelfx.Painter, far bool) {
		for k := range mcNRune {
			g := mcRuneAngle(k)
			if math.Sin(g) < 0 != far {
				continue
			}
			x, y0 := math.Cos(g)*mcRR, math.Sin(g)*mcRR*mcSY
			lit, gone := t-mcRuneLit(k), pixelfx.Clamp((t-mcRuneOut(k))/0.25)
			if t < mcInnerAt(g) || gone >= 1 {
				continue
			}
			if lit < 0 {
				glyph(p, k, x, y0-1, C[3], 1, 0)
				continue
			}
			rise := 3 * pixelfx.OutBack(pixelfx.Clamp(lit/0.2), 2.4)
			bob := 0.0
			if lit > 0.2 {
				bob = math.Sin(t*5+float64(k)) * 0.5
			}
			y := y0 - 1 - rise + bob
			hot, lvl := lit < 0.07 || (t > mcFlash && t < mcFlash+0.07), mcLevel(t)
			glyph(p, k, x, y, pick(hot || lvl > 0.8, pixelfx.White, C[6]), 1-gone, pick(hot, C[6], C[4]))
			if lit < 0.1 {
				pixelfx.Sparkle(p, x, y-2, 4*(1-lit/0.1)+1, C[6], C[5], 0.5)
			}
			p.Glow(x, y-2, 8, C[5], (0.3+0.3*lvl)*(1-gone))
			p.Glow(x, y0, 6, C[4], 0.2*(1-gone))
		}
	}

	// the column of light: the thread that climbs first, then the beam that fires up, holds and breaks apart
	column := func(p *pixelfx.Painter) {
		if t < mcFlash-0.22 || t > mcBreak+0.6 {
			return
		}
		if t < mcFlash {
			a := pixelfx.Smooth(mcFlash-0.22, mcFlash-0.04, t)
			top := -(mcTop - 10) * pixelfx.Ease.OutCubic(a)
			for y := -1.0; y >= top; y-- {
				p.Dot(0, y, pick(y > top+6, C[6], pixelfx.White), a*(0.35+0.65*(1+y/mcTop)))
			}
			p.Glow(0, top, 9, C[6], 0.5*a)
			p.Glow(0, -6, 24, C[5], 0.4*a)
			return
		}
		u, brk := t-mcFlash, pixelfx.Clamp((t-mcBreak)/0.45)
		top := -mcTop * pixelfx.Ease.OutExpo(pixelfx.Clamp(u/0.09))
		w := mcW * (1 - pixelfx.Ease.OutCubic(brk)*0.85)
		if t < mcBreak {
			w = mcW * (1 + 0.38*math.Exp(-u*9))
		}
		fade, white := 1-pixelfx.Smooth(mcBreak+0.25, mcBreak+0.55, t), u < 0.067
		if fade <= 0 {
			return
		}
		for y := 3.0; y >= math.Ceil(top); y-- {
			h := -y
			ww := w * (1 + 0.5*math.Exp(-math.Max(0, h)/6))
			pulse := 0
			if pixelfx.Fract(h*0.022-u*2.6) < 0.07 {
				pulse = 1
			}
			for x := math.Floor(-ww * 1.3); x <= math.Ceil(ww*1.3); x++ {
				q := math.Abs(x+0.5) / ww
				// as it breaks, holes open and grow and drift upward until only shards are left
				if brk > 0 && pixelfx.Noise2(x*0.32, h*0.11-(t-mcBreak)*6, S)*0.5+0.5 < brk*1.25-0.1 {
					continue
				}
				var lv int
				switch {
				case q < 0.2:
					lv = 0
				case q < 0.42:
					lv = 1
				case q < 0.66:
					lv = 2
				case q < 0.88:
					lv = 3
				default:
					lv = 4
				}
				for _, s := range strands {
					if math.Abs((x+0.5)/ww-s.o-0.05*math.Sin(t*s.f+s.o*9)) < s.w {
						lv = max(0, lv-1)
					}
				}
				if white {
					lv = 0
				} else {
					lv = max(0, lv-pulse)
				}
				a := pickF(q < 0.88, 1, (1.3-q)/0.42) * fade
				if a <= bayer(x, y)*pickF(q < 0.66, 0.2, 1) {
					continue
				}
				p.Px(x, y, COL[lv])
			}
			if pixelfx.ToInt32(h)&7 == 0 && h < mcTop-ww*2.4-8 {
				p.Glow(0, y, ww*2.4+6, C[5], 0.3*fade)
			}
		}
		// halos climbing it
		for i := range 6 {
			k := (t - (mcFlash + 0.08 + float64(i)*0.16)) / 0.8
			if k < 0 || k > 1 {
				continue
			}
			rx := w * 1.4 * (1 + k*0.35)
			ry, yc, n := rx*0.3, -3-pixelfx.Ease.OutQuad(k)*120, int(math.Ceil(rx*7))
			for j := range n {
				g := (float64(j) / float64(n)) * tau
				p.Dot(math.Cos(g)*rx, yc+math.Sin(g)*ry, pick(k < 0.3, pixelfx.White, C[6]), (1-k)*fade*pickF(math.Sin(g) < 0, 0.6, 1))
			}
		}
		// white-hot where it meets the floor
		p.Glow(0, -4, 34, C[6], 0.45*fade)
		p.Glow(0, -60, 70, C[4], 0.25*fade)
	}

	shock := func(p *pixelfx.Painter, far bool) {
		for _, s := range [2][5]float64{{0, mcR * 0.6, mcR * 2, 6, 0.5}, {0.08, mcR * 0.4, mcR * 1.4, 3, 0.4}} {
			d, r0, r1, th, dur := s[0], s[1], s[2], s[3], s[4]
			k := (t - mcFlash - d) / dur
			if k < 0 || k >= 1 {
				continue
			}
			pixelfx.Band(p, 0, 0, r0+(r1-r0)*pixelfx.Ease.OutCubic(k), mcSY, math.Max(1, th*(1-k)), 1.15-k, []uint32{C[3], C[5], C[6], pixelfx.White}, func(g float64) bool { return math.Sin(g) < 0 == far }, k < 0.2)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the pens shed glitter as they write
			if t > mcPen[0] && t < mcStar[1] && Rg.Chance(0.5) {
				var x, y float64
				switch {
				case t < mcPen[1]:
					g := pi/2 + Rg.Sign()*((t-mcPen[0])/(mcPen[1]-mcPen[0]))*pi
					x, y = math.Cos(g)*mcR, math.Sin(g)*mcR*mcSY
				case t < mcInner[1]:
					g := -pi/2 + ((t-mcInner[0])/(mcInner[1]-mcInner[0]))*tau
					x, y = math.Cos(g)*mcRI, math.Sin(g)*mcRI*mcSY
				default:
					var now []mcEdge
					for _, e := range edges {
						if t >= e.t0 && t < e.t0+e.dur {
							now = append(now, e)
						}
					}
					// pixelfx picks from the edges being drawn, or the first edge when none is: a draw either way
					v := Rg.Next()
					e := edges[0]
					if len(now) > 0 {
						e = now[int(math.Floor(v*float64(len(now))))]
					}
					s := pixelfx.Clamp((t - e.t0) / e.dur)
					x, y = e.p0[0]+(e.p1[0]-e.p0[0])*s, (e.p0[1]+(e.p1[1]-e.p0[1])*s)*mcSY
				}
				vx, vy := Rg.Range(-12, 12), Rg.Range(-30, -8)
				ps.Spawn(pixelfx.Ember, x, y-1, vx, vy, Rg.Range(0.25, 0.55), C[6], 1, 0, 0)
			}
			// gathering: light rushes in along the floor to the heart of the circle
			if t > mcRunes[1]-0.1 && t < mcFlash-0.05 && Rg.Chance(0.7) {
				g, D, T := Rg.Range(0, tau), Rg.Range(mcR*0.7, mcR*1.1), Rg.Range(0.16, 0.24)
				v := (D * 3) / (math.Exp(3*T) - 1)
				x, y := math.Cos(g)*D, math.Sin(g)*D*mcSY-1
				l := or1(pixelfx.Hypot(x, y+2))
				ps.Spawn(pixelfx.Streak, x, y, (-x/l)*v, ((-2-y)/l)*v, T, pick(Rg.Chance(0.5), C[6], C[5]), 1, 0, 0)
			}
			if !flashed && t >= mcFlash {
				flashed = true
				lights.Add(0, -56, 104, C[5], 0.9, 0.6)
				lights.Add(0, -5, 36, pixelfx.White, 0.8, 0.14)
				for range 36 {
					s, sp := Rg.Sign(), Rg.Range(80, 175)
					x := s * Rg.Range(0, mcR*0.5)
					vy := -Rg.Range(30, 180)
					ps.Spawn(pixelfx.Spark, x, -1, s*sp, vy, Rg.Range(0.25, 0.7), pixelfx.White, 1, 0, 0)
				}
				for range 26 {
					x, y := Rg.Range(-mcR, mcR), -Rg.Range(1, 6)
					vx, vy := Rg.Range(-20, 20), -Rg.Range(40, 130)
					life := Rg.Range(0.5, 1.1)
					c := pixelfx.Pick(Rg, []uint32{pixelfx.White, C[6], C[5]})
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, Rg.Range(0.8, 2.3), 0, 0)
				}
				// and a spray of twinkles flung out low across the floor
				for range 34 {
					s := Rg.Sign()
					x, y := s*Rg.Range(0, 14), -Rg.Range(1, 4)
					vx, vy := s*Rg.Range(50, 140), -Rg.Range(10, 90)
					life := Rg.Range(0.4, 0.95)
					c := pixelfx.Pick(Rg, []uint32{pixelfx.White, C[6], C[6], C[5]})
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, Rg.Range(0.7, 2.2), 0, 0)
				}
			}
			// glints pop along the burning circle
			if t > mcRunes[0] && t < mcBreak+0.2 && Rg.Chance(0.12) {
				g, r := Rg.Range(0, tau), pixelfx.Pick(Rg, []float64{mcR, mcR2, mcRI})
				vy, life := -Rg.Range(2, 8), Rg.Range(0.25, 0.45)
				c := pixelfx.Pick(Rg, []uint32{pixelfx.White, C[6]})
				ps.Spawn(pixelfx.StarKind, math.Cos(g)*r, math.Sin(g)*r*mcSY-0.5, 0, vy, life, c, Rg.Range(0.8, 2), 0, 0)
			}
			// the column throws off stars, and light streams up inside it
			if t > mcFlash && t < mcBreak {
				if Rg.Chance(0.5) {
					x, y := Rg.Range(-1, 1)*mcW*1.5, -Rg.Range(0, 120)
					vx, vy := Rg.Range(-10, 10), -Rg.Range(30, 85)
					life := Rg.Range(0.5, 1.1)
					c := pixelfx.Pick(Rg, []uint32{pixelfx.White, C[6], C[5]})
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, Rg.Range(0.7, 2), 0, 0)
				}
				if Rg.Chance(0.35) {
					x, y := Rg.Range(-1, 1)*mcW*0.8, -Rg.Range(0, 50)
					vy, life := -Rg.Range(160, 300), Rg.Range(0.2, 0.35)
					ps.Spawn(pixelfx.Mote, x, y, 0, vy, life, pick(Rg.Chance(0.5), pixelfx.White, C[6]), 0.8, 0, 0)
				}
			}
			// it breaks apart into glints that twinkle as they drift up
			if !broke && t >= mcBreak {
				broke = true
				for range 46 {
					x, y := Rg.Range(-1, 1)*mcW*0.9, -Rg.Range(2, mcTop*0.8)
					vx, vy := Rg.Range(-12, 12), -Rg.Range(8, 40)
					life := Rg.Range(0.5, 1.15)
					c := pixelfx.Pick(Rg, []uint32{pixelfx.White, pixelfx.White, C[6], C[5]})
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, Rg.Range(0.6, 2.4), 0, 0)
				}
			}
			if t > mcBreak && t < mcBreak+0.4 && Rg.Chance(0.8) {
				x, y := Rg.Range(-1, 1)*mcW*0.7, -Rg.Range(2, mcTop*0.78)
				vx, vy := Rg.Range(-8, 8), -Rg.Range(10, 50)
				life := Rg.Range(0.4, 0.9)
				c := pixelfx.Pick(Rg, []uint32{pixelfx.White, C[6], C[5]})
				ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, Rg.Range(0.6, 1.8), 0, 0)
				if Rg.Chance(0.4) {
					x, y := Rg.Range(-1, 1)*mcW*0.5, -Rg.Range(2, mcTop*0.7)
					vy, life := -Rg.Range(60, 140), Rg.Range(0.2, 0.4)
					ps.Spawn(pixelfx.Mote, x, y, 0, vy, life, C[6], 0.7, 0, 0)
				}
			}
			// the runes go out one by one, each in a little puff of glints
			for k := range mcNRune {
				if !out[k] && t >= mcRuneOut(k) {
					out[k] = true
					g := mcRuneAngle(k)
					x, y := math.Cos(g)*mcRR, math.Sin(g)*mcRR*mcSY-5
					for range 4 {
						sx, sy := x+Rg.Range(-2, 2), y+Rg.Range(-2, 2)
						vx, vy := Rg.Range(-14, 14), -Rg.Range(10, 40)
						life := Rg.Range(0.3, 0.55)
						c := pixelfx.Pick(Rg, []uint32{pixelfx.White, C[6]})
						ps.Spawn(pixelfx.StarKind, sx, sy, vx, vy, life, c, Rg.Range(0.6, 1.6), 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			lvl := mcLevel(t)
			if t < mcGone {
				k := lvl * pixelfx.Smooth(mcPen[0], mcPen[1]+0.2, t)
				p.Glow(0, 0, mcR*1.3, C[4], 0.28*k)
				for _, s := range [2]float64{-1, 1} {
					p.Glow(s*mcR*0.6, 1, mcR*0.7, C[5], 0.12*k)
				}
			}
			shock(p, true)
			circle(p, true)
			runes(p, true)
			column(p)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			shock(p, false)
			circle(p, false)
			runes(p, false)
			// the pens: white points running round, writing light
			pen := func(x, y float64) {
				pixelfx.Sparkle(p, x, y, 2, C[6], C[5], 0.5)
				p.Glow(x, y, 7, C[6], 0.45)
			}
			if t > mcPen[0] && t < mcPen[1] {
				for _, s := range [2]float64{-1, 1} {
					g := pi/2 + s*((t-mcPen[0])/(mcPen[1]-mcPen[0]))*pi
					pen(math.Cos(g)*mcR, math.Sin(g)*mcR*mcSY)
				}
			}
			if t > mcInner[0] && t < mcInner[1] {
				g := -pi/2 + ((t-mcInner[0])/(mcInner[1]-mcInner[0]))*tau
				pen(math.Cos(g)*mcRI, math.Sin(g)*mcRI*mcSY)
			}
			for _, e := range edges {
				if t > e.t0 && t < e.t0+e.dur {
					s := (t - e.t0) / e.dur
					pen(e.p0[0]+(e.p1[0]-e.p0[0])*s, (e.p0[1]+(e.p1[1]-e.p0[1])*s)*mcSY)
				}
			}
			// the flash
			if t >= mcFlash && t < mcFlash+0.24 {
				a := (t - mcFlash) / 0.24
				pixelfx.Flare(p, 0, -8, 34*math.Pow(1-a, 1.5), 1-a, C)
				p.Glow(0, -26, 56, pixelfx.White, 0.5*(1-a))
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
