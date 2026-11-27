// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/thrust.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the speed lines from Sprite Fusion's open-source dash and the ray star from his sparkle (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	thrustStance = pixelfx.Ready.With(pixelfx.X.Is(-14), pixelfx.FX.Is(12), pixelfx.FY.Is(-21), pixelfx.SA.Is(-0.3)).Hold("sword")
	// the hand comes back to the hip with the point forward, then the whole arm goes out along the blade
	thrustBack  = thrustStance.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-3), pixelfx.FX.Is(-3), pixelfx.FY.Is(-19), pixelfx.SA.Is(-0.05), pixelfx.BX.Is(8), pixelfx.BY.Is(-30), pixelfx.ArmsFront.On(), pixelfx.FFX.Is(10), pixelfx.FBX.Is(-8))
	thrustLunge = thrustStance.With(pixelfx.Crouch.Is(6), pixelfx.Lean.Is(6), pixelfx.FX.Is(23), pixelfx.FY.Is(-20), pixelfx.SA.Is(0), pixelfx.BX.Is(-8), pixelfx.BY.Is(-24), pixelfx.FFX.Is(15), pixelfx.FBX.Is(-11), pixelfx.ToeB.On())
)

// Thrust is a piercing stab: a bundle of streaks shoots out from the hand to a point, a long four-point star flares at the tip with a pierce ring, sparks and speed lines. It is registered as "melee/thrust".
var Thrust = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Thrust",
		Desc:     "A piercing stab: a bundle of streaks shoots out from the hand to a point, a long four-point star flares at the tip with a pierce ring, sparks and speed lines.",
		Tags:     []string{"melee", "thrust", "stab", "spear", "rapier", "pierce", "combat", "directional", "one-shot"},
		Size:     [2]int{196, 76},
		Origin:   [2]int{34, 62},
		Duration: 0.75,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1608", "#6b3c10", "#b8701a", "#f0b42e", "#ffe27a", "#fff6cf", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "reach", Default: pixelfx.Num(66)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{80},
		Poses: []pixelfx.Key{
			{T: -0.46, Pose: thrustStance},
			{T: -0.24, Pose: thrustBack},
			{T: -0.05, Pose: thrustBack.With(pixelfx.Crouch.Is(6), pixelfx.FX.Is(-5))},
			{T: 0, Pose: thrustLunge.With(pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.07, Pose: thrustLunge, How: "snap"},
			{T: 0.3, Pose: thrustLunge.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(5))},
			{T: 0.7, Pose: thrustStance},
		},
		Hits: []pixelfx.Hit{{T: 0.07, Stop: 0.12, Shake: 0.14, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.8), Straw: pixelfx.Val(16), Y: pixelfx.Val(-26)}},
		Dust: []pixelfx.DustKick{{T: 0, X: -24, N: 6, Dir: -1}, {T: 0.07, X: 1, N: 6, Dir: 1}},
	},
	New: newThrust,
}

func init() { pixelfx.Register("melee/thrust", Thrust) }

var (
	thrustOut  = 0.07 // the point arrives
	thrustHold = 0.06
	thrustPull = 0.2
)

// longStar draws a long four-point star: the forward and back arms long, up and down shorter, thick at the heart.
func longStar(p *pixelfx.Painter, x, y, L, k float64, C []uint32) {
	if L < 1 {
		return
	}
	for _, arm := range [...][3]float64{{1, 0, L}, {-1, 0, L * 0.7}, {0, -1, L * 0.42}, {0, 1, L * 0.42}} {
		dx, dy, length := arm[0], arm[1], arm[2]
		for d := 1.0; d <= length; d += 0.5 {
			u := d / length
			c := C[4]
			if u < 0.3 {
				c = pixelfx.White
			} else if u < 0.65 {
				c = C[5]
			}
			p.Dot(x+dx*d, y+dy*d, c, either(u < 0.7, 1, 1-(u-0.7)/0.35))
			if u < 0.25 && length > 6 {
				p.Dot(x+dx*d-dy, y+dy*d+dx, C[5], 1)
				p.Dot(x+dx*d+dy, y+dy*d-dx, C[5], 1)
			}
		}
	}
	for _, dg := range [...][2]float64{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}} {
		for d := 1.0; d <= math.Min(3, L*0.14); d++ {
			p.Px(x+dg[0]*d, y+dg[1]*d, either(d < 2, C[5], C[4]))
		}
	}
	p.Px(x, y, pixelfx.White)
	p.Glow(x, y, 6+L*1.3, C[4], 0.35*k)
	p.Glow(x, y, 3+L*0.4, pixelfx.White, 0.3*k)
}

// thrustStreak is one streak of the bundle: its offset from the line, where and when it starts, how thick it is, whether it is the hot middle one, and how much of the way to the point it goes.
type thrustStreak struct {
	off, x0, t0, w float64
	hot            bool
	len            float64
}

// speedLine is a dotted speed line racing beside the bundle: where its head is, how long it is, how fast it goes and when it was born.
type speedLine struct{ x, y, l, v, t float64 }

func newThrust(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	Rg, ps, lights := newSim(o.Seed(), o.Ramp("sparks"))
	reach := sizeOf(o, "reach")
	HX, HY := 9.0, -20.0
	TX, TY := HX+reach, HY
	// the bundle: streaks fanned at the hand, meeting at the point; the middle one hottest
	streaks := make([]thrustStreak, 0, 7)
	for _, off := range []float64{-7, -4, -2, 0, 2, 4, 7} {
		s := thrustStreak{off: off, w: either(off == 0, 2.0, 1), hot: off == 0}
		s.x0 = HX + 1 + Rg.Range(0, 5) + math.Abs(off)*0.6
		s.t0 = Rg.Range(0, 0.02) + math.Abs(off)*0.003
		s.len = Rg.Range(0.75, 1)
		streaks = append(streaks, s)
	}
	var lines []speedLine
	t, struck := 0.0, false
	pull0, pull1 := thrustOut+thrustHold, thrustOut+thrustHold+thrustPull
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// speed lines race forward beside the bundle while it shoots out and just after
			if t < thrustOut+0.08 && Rg.Chance(0.5) {
				s := Rg.Sign()
				l := speedLine{t: t}
				l.x = HX + Rg.Range(0, reach*0.6)
				l.y = HY + s*Rg.Range(7, 17)
				l.l = Rg.Range(18, 42)
				l.v = Rg.Range(200, 340)
				lines = append(lines, l)
			}
			for i := len(lines) - 1; i >= 0; i-- {
				if t-lines[i].t > 0.24 {
					lines = append(lines[:i], lines[i+1:]...)
				}
			}
			for i := range lines {
				lines[i].x += lines[i].v * dt
			}
			if !struck && t >= thrustOut {
				struck = true
				lights.Add(TX, TY, 44, C[4], 0.45, 0.3)
				lights.Add(TX, TY, 14, pixelfx.White, 0.6, 0.06)
				for range 12 {
					a, sp := Rg.Range(-0.55, 0.45), Rg.Range(150, 330)
					ps.Spawn(pixelfx.Spark, TX, TY, math.Cos(a)*sp, math.Sin(a)*sp-30, Rg.Range(0.12, 0.35), pixelfx.White, 1, 0, 0)
				}
				for range 10 {
					a := Rg.Range(-0.9, 0.9) + either(Rg.Chance(0.3), pi, 0)
					sp := Rg.Range(40, 120)
					ps.Spawn(pixelfx.Mote, TX+Rg.Range(-6, 2), TY+Rg.Range(-2, 2), math.Cos(a)*sp, math.Sin(a)*sp, Rg.Range(0.25, 0.5), either(Rg.Chance(0.5), C[5], C[4]), 0.6, 0, 0)
				}
				for range 4 {
					ps.Spawn(pixelfx.StarKind, TX+Rg.Range(-10, 8), TY+Rg.Range(-9, 9), 0, -3, Rg.Range(0.2, 0.4), either(Rg.Chance(0.5), C[5], pixelfx.White), Rg.Range(1, 2), Rg.Range(0.06, 0.2), 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the point catches the light before it goes
			if t < 0.05 {
				pixelfx.Sparkle(p, HX+2, HY, 5*(1-t/0.05)+1, C[5], C[3], 0.4)
			}
			// speed lines: dotted, white at the head, dimming back along their length
			for _, l := range lines {
				k := (t - l.t) / 0.24
				for d := 0.0; d < l.l*(1-k*0.6); d++ {
					c := C[3]
					if d < 3 {
						c = pixelfx.White
					} else if d < l.l*0.4 {
						c = C[5]
					}
					p.Dot(l.x-d, l.y, c, (1-d/l.l)*(1-k))
				}
			}
			// the bundle: each streak shoots from the hand to the point, holds, then is drawn in to the point
			pull := pixelfx.Smooth(pull0, pull1, t)
			for _, s := range streaks {
				u := pixelfx.Clamp((t - s.t0) / thrustOut)
				head := pixelfx.Ease.OutExpo(u)
				if u <= 0 || pull >= 1 {
					continue
				}
				// the outer streaks stop short of the point and trail a little behind, so each reads on its own
				y0, end := HY+s.off, s.x0+(TX-s.x0)*s.len
				x1, xa := s.x0+(end-s.x0)*head, s.x0+(end-s.x0)*math.Max(pull, head*0.35*(1-s.len)*3)
				yAt := func(x float64) float64 { return y0 + (TY-y0)*pixelfx.Clamp((x-s.x0)/(TX-s.x0))*0.85 }
				for x := xa; x <= x1; x += 0.5 {
					v := (x - xa) / math.Max(1, x1-xa)
					var c uint32
					switch {
					case v > 0.88:
						c = pixelfx.White
					case s.hot:
						c = either(v > 0.35, C[6], C[5])
					case v > 0.55:
						c = C[5]
					case v > 0.25:
						c = C[4]
					default:
						c = C[3]
					}
					p.Dot(x, yAt(x), c, either(v < 0.12, v/0.12, 1))
					if s.w > 1 && v > 0.1 {
						p.Dot(x, yAt(x)+1, either(v > 0.6, C[5], C[3]), either(v < 0.2, v/0.2, 1))
					}
				}
			}
			if t < pull1 {
				p.GlowLine(HX+(TX-HX)*pull, HY, HX+(TX-HX)*pixelfx.Ease.OutExpo(pixelfx.Clamp(t/thrustOut)), TY, 6, C[4], 0.22*(1-pull))
			}
			// the long star at the point, and a pierce ring shed from it
			q := t - thrustOut
			if q >= 0 && q < 0.4 {
				g := pixelfx.OutBack(pixelfx.Clamp(q/0.04), 2.4) * (1 - pixelfx.Smooth(0.08, 0.4, q)) * (1 + 0.12*math.Sin(q*70))
				longStar(p, TX, TY, 24*g, 1-q/0.4, C)
			}
			if q >= 0 && q < 0.2 {
				k := q / 0.2
				rx, ry := 2+7*pixelfx.Ease.OutCubic(k), 4+12*pixelfx.Ease.OutCubic(k)
				for a := 0.0; a < pixelfx.Tau; a += 0.05 {
					p.Dot(TX+4+10*k+math.Cos(a)*rx, TY+math.Sin(a)*ry, either(k < 0.3, pixelfx.White, C[5]), 1.15-k)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
