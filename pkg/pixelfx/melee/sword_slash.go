// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/sword-slash.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crescent, echo, trail, glint and spark recipe from Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	swordGuard = pixelfx.Ready.With(pixelfx.FX.Is(12), pixelfx.FY.Is(-21), pixelfx.SA.Is(-0.85)).Hold("sword")
	// the blade goes back past his head, then lags behind the hand on the way through, so the wrist breaks and it whips
	swordWind = swordGuard.With(pixelfx.X.Is(-8), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-4), pixelfx.FX.Is(-7), pixelfx.FY.Is(-35), pixelfx.SA.Is(-2.65), pixelfx.BX.Is(9), pixelfx.BY.Is(-25), pixelfx.ArmsFront.On(), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-8))
	swordCut  = swordGuard.With(pixelfx.X.Is(0), pixelfx.Crouch.Is(2), pixelfx.Lean.Is(3), pixelfx.FX.Is(16), pixelfx.FY.Is(-37), pixelfx.SA.Is(-1.45), pixelfx.BX.Is(-7), pixelfx.BY.Is(-24), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-10), pixelfx.Ghost.Is(1))
	swordDown = swordGuard.With(pixelfx.X.Is(2), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(5), pixelfx.FX.Is(20), pixelfx.FY.Is(-21), pixelfx.SA.Is(0.62), pixelfx.BX.Is(-9), pixelfx.BY.Is(-24), pixelfx.FFX.Is(13), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
)

// SwordSlash is a tilted crescent that whips round in two frames, cools from white to deep blue as it trails off, glints at the tip and throws sparks, acted out as a sword cut. It is registered as "melee/sword-slash".
var SwordSlash = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Sword slash",
		Desc:     "A tilted crescent whips round in two frames, cools from white to deep blue as it trails off, glints at the tip and throws sparks.",
		Tags:     []string{"melee", "slash", "sword", "combat", "directional", "one-shot"},
		Size:     [2]int{136, 104},
		Origin:   [2]int{44, 92},
		Duration: 0.75,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#212f6b", "#2c58aa", "#3a8bd8", "#62c8f4", "#b8c9e8", "#e8f1ff", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "radius", Default: pixelfx.Num(42)},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{58},
		Poses: []pixelfx.Key{
			{T: -0.55, Pose: swordGuard.With(pixelfx.X.Is(-8))},
			{T: -0.3, Pose: swordWind},
			{T: -0.08, Pose: swordWind.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-5))},
			{T: 0, Pose: swordCut, How: "snap"},
			{T: 0.06, Pose: swordDown, How: "snap"},
			{T: 0.3, Pose: swordDown.With(pixelfx.X.Is(3), pixelfx.Crouch.Is(6))},
			{T: 0.58, Pose: swordGuard.With(pixelfx.X.Is(1), pixelfx.Crouch.Is(3), pixelfx.FX.Is(15), pixelfx.FY.Is(-21), pixelfx.SA.Is(0.1), pixelfx.BX.Is(-5), pixelfx.BY.Is(-21), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-7))},
			{T: 0.86, Pose: swordGuard.With(pixelfx.X.Is(-8))},
		},
		Hits: []pixelfx.Hit{{T: 0.06, Stop: 0.14, Shake: 0.18, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.8), Straw: pixelfx.Val(18)}},
		Dust: []pixelfx.DustKick{{T: 0.06, X: 16, N: 7, Dir: 1}, {T: 0.06, X: -11, N: 4, Dir: -1}},
	},
	New: newSwordSlash,
}

func init() { pixelfx.Register("melee/sword-slash", SwordSlash) }

// swordArc is one crescent of the sword slash: an ellipse of radius R round (cx, cy) squashed to sy and turned by rot, cut from angle a0 to a1, W thick, whipping out over sweep seconds from t0, holding, then trailing off, in ramp.
type swordArc struct {
	cx, cy, R, sy, rot, a0, a1, W, t0, sweep, hold, trail float64
	ramp                                                  []uint32
}

// onArc returns a point on the slash's tilted ellipse.
func (s *swordArc) onArc(r, a float64) (float64, float64) {
	lx, ly := math.Cos(a)*r, math.Sin(a)*r*s.sy
	c, n := math.Cos(s.rot), math.Sin(s.rot)
	return s.cx + lx*c - ly*n, s.cy + lx*n + ly*c
}

// crescent draws the smear: thickest just behind the leading edge, tapering to a hair at the tail, white on the outer rim, cooling down the ramp with age and towards the tail; the tail dissolves through the dither.
func (s *swordArc) crescent(p *pixelfx.Painter, ta, L, W, age float64) {
	if math.Abs(L) < 0.02 || W < 0.3 {
		return
	}
	R, n := s.ramp, len(s.ramp)
	dir, span := pixelfx.Sign(L), math.Abs(L)
	c, sn, B := math.Cos(s.rot), math.Sin(s.rot), s.R+2
	p.Shade(s.cx-B, s.cy-B, s.cx+B, s.cy+B, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-s.cx, y-s.cy
		lx, ly := dx*c+dy*sn, (-dx*sn+dy*c)/s.sy
		r := pixelfx.Hypot(lx, ly)
		if r > s.R+0.5 || r < s.R-W-1 {
			return 0
		}
		u := math.Mod(dir*(math.Atan2(ly, lx)-ta), pixelfx.Tau)
		if u < 0 {
			u += pixelfx.Tau
		}
		if u > span {
			return 0
		}
		v := u / span
		w := W * math.Pow(math.Sin(pi*math.Pow(v, 2.2)), 0.6)
		if w < 0.35 {
			return 0
		}
		q := (s.R - r) / w
		if q < 0 || q > 1 {
			return 0
		}
		// brightest step at the rim while it's young, then darker steps inward, with age and towards the tail
		ci := 0.0
		if !(q < 0.22 && age < 0.5) {
			ci = pixelfx.Round(q*float64(n-1)*0.75 + age*2.4 - v*0.8 + 0.4)
		}
		out.C = R[n-1-int(pixelfx.ClampTo(ci, 0, float64(n-1)))]
		out.A = math.Min(1, v/0.4)
		return 0
	})
	for u := 0.0; u <= span; u += 0.3 {
		x, y := s.onArc(s.R-W*0.4, ta+dir*u)
		p.Glow(x, y, 5+W, R[n-3], 0.22*(1-age)*(u/span))
	}
}

// draw draws the crescent at time t, and the dotted trace of the blade's path that lingers where the smear has already gone.
func (s *swordArc) draw(p *pixelfx.Painter, t float64) {
	u := t - s.t0
	if u < 0 {
		return
	}
	span := s.a1 - s.a0
	head, tail := pixelfx.Ease.OutExpo(pixelfx.Clamp(u/s.sweep)), pixelfx.Smooth(s.sweep+s.hold, s.sweep+s.hold+s.trail, u)
	age := pixelfx.Clamp(u / (s.sweep + s.hold + s.trail))
	if tail < 1 {
		s.crescent(p, s.a0+span*tail, span*(head-tail), s.W*(1-tail*0.55), age)
	}
	th := 0.7 * (1 - pixelfx.Clamp((u-s.sweep-s.hold)/(s.trail+0.2)))
	if tail > 0.02 && th > 0 {
		for k := 0.0; k <= tail; k += 0.6 / (math.Abs(span) * s.R) {
			x, y := s.onArc(s.R-0.5, s.a0+span*k)
			p.Dot(x, y, s.ramp[1], th*(0.4+0.6*k))
		}
	}
}

// tip returns where the edge stops and which way it is going there.
func (s *swordArc) tip() travelAt {
	x, y := s.onArc(s.R-s.W*0.3, s.a1)
	x2, y2 := s.onArc(s.R-s.W*0.3, s.a1+pixelfx.Sign(s.a1-s.a0)*0.05)
	l := pixelfx.Hypot(x2-x, y2-y)
	if l == 0 {
		l = 1
	}
	return travelAt{x: x, y: y, dx: (x2 - x) / l, dy: (y2 - y) / l}
}

func newSwordSlash(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	Rg, ps, _ := newSim(o.Seed(), o.Ramp("sparks"))
	R0, a0, a1 := sizeOf(o, "radius"), -2.55, 0.95
	// the main smear, and a thinner echo just inside it a beat behind
	A := swordArc{cx: 6, cy: -26, R: R0, sy: 0.6, rot: -0.38, a0: a0, a1: a1, W: 12, t0: 0, sweep: 0.06, hold: 0.06, trail: 0.24, ramp: C}
	E := A
	E.R, E.W, E.a0, E.a1 = R0*(33.0/42), R0*(4.5/42), a0+(a1-a0)*0.1, a1-(a1-a0)*(0.15/3.5)
	E.t0, E.hold, E.trail, E.ramp = 0.017, 0.03, 0.2, C[:6]
	tip := A.tip()
	t, landed := 0.0, false
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !landed && t >= A.sweep {
				landed = true
				// sparks off the edge where it stops, and bright shavings flung off the arc
				for range 10 {
					a, sp := math.Atan2(tip.dy, tip.dx)+Rg.Range(-0.5, 0.5), Rg.Range(150, 400)
					ps.Spawn(pixelfx.Spark, tip.x, tip.y, math.Cos(a)*sp, math.Sin(a)*sp, Rg.Range(0.12, 0.35), pixelfx.White, 1, 0, 0)
				}
				for range 16 {
					a := a0 + (a1-a0)*Rg.Range(0.3, 1)
					x, y := A.onArc(A.R, a)
					ox, oy := x-A.cx, y-A.cy
					l := pixelfx.Hypot(ox, oy)
					if l == 0 {
						l = 1
					}
					sp := Rg.Range(30, 100)
					ps.Spawn(pixelfx.Mote, x, y, (ox/l)*sp, (oy/l)*sp-10, Rg.Range(0.25, 0.55), C[4+int(Rg.Int(0, 1))], 0.6, 0, 0)
				}
			}
			ps.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			E.draw(p, t)
			A.draw(p, t)
			// the tip glints as the edge arrives
			if g := t - A.sweep*0.6; g > 0 && g < 0.22 {
				pixelfx.Sparkle(p, tip.x, tip.y, 9*pixelfx.Ease.OutBack(math.Min(1, g/0.05))*(1-g/0.22), C[5], C[3], 0.6)
			}
			// light rides the head of the swing
			if t < 0.3 {
				x, y := A.onArc(A.R-4, A.a0+(A.a1-A.a0)*pixelfx.Ease.OutExpo(pixelfx.Clamp(t/A.sweep)))
				p.Glow(x, y, 40, C[3], 0.5*(1-t/0.3))
			}
			ps.Draw(p, pixelfx.LayerBoth)
		},
	}
}
