// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/spin-slash.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crescent shading, cooling ramp, dotted trace, glint and spark recipe from Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	spinStance = pixelfx.Ready.With(pixelfx.FX.Is(12), pixelfx.FY.Is(-21), pixelfx.SA.Is(-0.85)).Hold("sword")
	// he really turns: forward, away from us, facing back, towards us, and round to the front
	spinWound = spinStance.With(pixelfx.Crouch.Is(6), pixelfx.Lean.Is(-2), pixelfx.Yaw.Is(1.1), pixelfx.FX.Is(13), pixelfx.FY.Is(-20), pixelfx.SA.Is(0.3), pixelfx.BX.Is(6), pixelfx.BY.Is(-24), pixelfx.FFX.Is(8), pixelfx.FBX.Is(-8))
	spinWide  = spinStance.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(3), pixelfx.FX.Is(21), pixelfx.FY.Is(-21), pixelfx.SA.Is(0.05), pixelfx.BX.Is(-9), pixelfx.BY.Is(-23), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-11))
)

// SpinSlash is a blade that whirls once round the waist on a tilted plane, behind the body and back in front; the closed ring flares white, dissolves into a dotted trace and throws sparks. It is registered as "melee/spin-slash".
var SpinSlash = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Spin slash",
		Desc:     "The blade whirls once round the waist on a tilted plane, behind the body and back in front; the closed ring flares white, dissolves into a dotted trace and throws sparks.",
		Tags:     []string{"melee", "slash", "spin", "sword", "aoe", "combat", "one-shot"},
		Size:     [2]int{208, 104},
		Origin:   [2]int{104, 84},
		Duration: 0.8,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#16224a", "#1f4a86", "#2a7fc0", "#48b8ec", "#9fe2ff", "#e2f7ff", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "radius", Default: pixelfx.Num(46)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{60},
		Poses: []pixelfx.Key{
			{T: -0.46, Pose: spinStance},
			{T: -0.2, Pose: spinWound},
			{T: -0.02, Pose: spinWound.With(pixelfx.Crouch.Is(7), pixelfx.Yaw.Is(1.3))},
			{T: 0, Pose: spinWide.With(pixelfx.SA.Is(-0.1), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.05, Pose: spinWide.With(pixelfx.Yaw.Is(-1.57), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.09, Pose: spinWide.With(pixelfx.Yaw.Is(-3.14), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.13, Pose: spinWide.With(pixelfx.Yaw.Is(-4.71), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.17, Pose: spinWide, How: "snap"},
			{T: 0.56, Pose: spinWide.With(pixelfx.Crouch.Is(4), pixelfx.Lean.Is(2))},
			{T: 0.92, Pose: spinStance},
		},
		Hits: []pixelfx.Hit{{T: 0.17, Stop: 0.12, Shake: 0.16, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.4), Straw: pixelfx.Val(16)}},
		Dust: []pixelfx.DustKick{{T: 0, X: 0, N: 6}, {T: 0.17, X: 12, N: 6, Dir: 1}},
	},
	New: newSpinSlash,
}

func init() { pixelfx.Register("melee/spin-slash", SpinSlash) }

var (
	// spinSW is how long the edge takes to go once round
	spinSW = 0.17
	// spinFlare is how long the closed ring flares and dissolves
	spinFlare = 0.3
)

// spinTurn returns the fraction of the turn the edge has made at t.
func spinTurn(t float64) float64 { return 1 - math.Pow(1-pixelfx.Clamp(t/spinSW), 1.6) }

// spinPassedAt returns when the edge passed fraction f of the turn.
func spinPassedAt(f float64) float64 { return spinSW * (1 - math.Pow(1-pixelfx.Clamp(f), 1/1.6)) }

func newSpinSlash(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	n, R0 := len(C), o.Num("radius")
	Rg, ps, lights := newSim(o.Seed(), o.Ramp("sparks"))
	tau := pixelfx.Tau
	// the spin starts front-right, goes up the right side, behind the body, down the left and back across the front
	A := swing{cx: 0, cy: -17, R: R0, sy: 0.27, rot: -0.1, a0: 0.5, dir: -1, k: 0.15}
	// a thinner echo just inside, a beat behind
	E := A
	E.R, E.k = R0*0.8, 0.1
	W, WE := 16.0, 5.0
	// the far half sits deeper in the ramp
	DIM := []uint32{C[0], C[0], C[1], C[2], C[3], C[4], C[5]}
	t, closed, lastU := 0.0, false, 0.0

	// the smear: thickest just behind the edge, thinning and cooling where the edge passed longer ago
	body := func(p *pixelfx.Painter, s *swing, w0, lag, half float64, rim bool) {
		U, ramp := tau*spinTurn(t-lag), either(half < 0, DIM, C)
		if U < 0.02 || t-lag > spinSW+0.02 {
			return
		}
		smear(p, s, 0, U, w0, func(u, r, _ float64, out *pixelfx.Out) {
			age, lead := t-lag-spinPassedAt(u/tau), U-u
			w := w0 * math.Pow(pixelfx.Clamp(lead/0.85), 0.5) * (0.3 + 0.7*math.Exp(-age/0.075)) * math.Pow(pixelfx.Clamp(u/1.3), 0.7)
			if w < 0.45 {
				return
			}
			q := (s.R - r) / w
			if q < 0 || q > 1 {
				return
			}
			// white rim while fresh, then darker steps inward, with age and towards the tail
			ci := 0.0
			if !(rim && q < 0.26 && age < 0.06) {
				ci = pixelfx.Round(q*float64(n-1)*0.7 + age*13 + either(rim, 0.4, 1.4))
			}
			out.C = ramp[n-1-int(pixelfx.ClampTo(ci, 0, float64(n-1)))]
			out.A = math.Min(1, u/1.1+0.2)
		}, half, 1)
	}

	ring := func(p *pixelfx.Painter, half float64) {
		// the swept smear and its echo
		body(p, &E, WE, 0.025, half, false)
		body(p, &A, W, 0, half, true)
		if t < spinSW+0.04 {
			k0 := either(half < 0, 0.2, 0.3)
			glowAlong(p, &A, A.R-5, 0, tau*spinTurn(t), 14, C[3], func(u float64) float64 { return k0 * math.Exp(-(t-spinPassedAt(u/tau))/0.08) }, half, 0.35)
		}
		if t < spinSW {
			return
		}
		// closed: the ring flares white, swells a little, thins, cools and breaks into dots
		f, ramp := (t-spinSW)/spinFlare, either(half < 0, DIM, C)
		if f < 1 {
			R, th := A.R+6*pixelfx.Ease.OutCubic(f), 9.0
			if f >= 0.13 {
				th = math.Max(1, 6*math.Pow(1-f, 1.5)+0.5)
			}
			S := A
			S.R = R
			step := 3
			switch {
			case f < 0.13:
				step = 6
			case f < 0.3:
				step = 5
			case f < 0.55:
				step = 4
			}
			smear(p, &S, 0, tau, th, func(_, r, _ float64, out *pixelfx.Out) {
				q := (R - r) / th
				if q < 0 || q > 1 {
					return
				}
				switch {
				case f < 0.13:
					out.C = either(q < 0.6, pixelfx.White, ramp[5])
				case q < 0.45:
					out.C = ramp[step]
				default:
					out.C = ramp[step-1]
				}
				out.A = either(f < 0.4, 1, 1.3-f)
			}, half, 1)
			glowAlong(p, &S, R-2, 0, tau, 14, C[4], constK(0.32*(1-f)*(1-f)), half, 0.35)
		}
		// a dotted trace of the blade's path lingers where the ring was, fading
		if k := 1 - pixelfx.Clamp((t-spinSW-0.06)/0.4); k > 0 {
			trace(p, &A, A.R-0.5, 0, tau, ramp[4], func(u float64) float64 {
				return pixelfx.Clamp(k*1.5-pixelfx.Rand(math.Floor(u*30), 5)*0.8) * 0.95
			}, half, 0.6)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			U := tau * spinTurn(t)
			// shavings peel off the edge as it goes round, flung along its path
			if t < spinSW {
				for u := lastU; u < U; u += 0.3 {
					tr := travel(&A, u)
					ox, oy := tr.x-A.cx, tr.y-A.cy
					l := pixelfx.Hypot(ox, oy)
					if l == 0 {
						l = 1
					}
					sp := Rg.Range(40, 120)
					ps.Spawn(pixelfx.Mote, tr.x, tr.y, tr.dx*sp+(ox/l)*sp*0.35, tr.dy*sp*0.6-Rg.Range(5, 30), Rg.Range(0.22, 0.45), C[4+int(Rg.Int(0, 1))], 0.55, 0, backIf(math.Sin(tr.a) < 0))
				}
				lastU = U
			}
			if !closed && t >= spinSW {
				closed = true
				lights.Add(A.cx, A.cy+4, 90, C[3], 0.4, 0.4)
				// sparks off the edge where it closes, and all round the ring along its turn
				end := travel(&A, tau)
				for range 9 {
					a, sp := math.Atan2(end.dy, end.dx)+Rg.Range(-0.45, 0.35), Rg.Range(160, 360)
					ps.Spawn(pixelfx.Spark, end.x, end.y, math.Cos(a)*sp, math.Sin(a)*sp-Rg.Range(20, 70), Rg.Range(0.14, 0.36), pixelfx.White, 1, 0, 0)
				}
				for range 16 {
					u := Rg.Range(0, tau)
					tr, sp := travel(&A, u), Rg.Range(110, 260)
					ps.Spawn(pixelfx.Spark, tr.x, tr.y, tr.dx*sp, tr.dy*sp-Rg.Range(20, 80), Rg.Range(0.12, 0.32), 0, 1, 0, backIf(math.Sin(tr.a) < 0))
				}
				// and twinkles left hanging on the ring
				for range 6 {
					u := Rg.Range(0, tau)
					x, y := onSwing(&A, A.R+Rg.Range(-1, 3), u)
					ps.Spawn(pixelfx.StarKind, x, y, 0, -Rg.Range(2, 8), Rg.Range(0.25, 0.5), either(Rg.Chance(0.5), C[5], pixelfx.White), Rg.Range(1.2, 2.6), Rg.Range(0.04, 0.16), backIf(math.Sin(A.a0+A.dir*u) < 0))
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			ring(p, -1)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			ring(p, 1)
			// the blade catches the light as it sets off, and glints again where the ring closes
			if t < 0.06 {
				x, y := onSwing(&A, A.R-3, 0)
				pixelfx.Sparkle(p, x, y, 7*(1-t/0.06), C[5], C[3], 0.5)
			}
			g := t - spinSW*0.85
			tx, ty := onSwing(&A, A.R-3, tau)
			if g > 0 && g < 0.24 {
				pixelfx.Sparkle(p, tx, ty, 10*pixelfx.Ease.OutBack(math.Min(1, g/0.05))*(1-g/0.24), C[5], C[3], 0.6)
			}
			// light rides the edge
			if t < spinSW+0.05 {
				U := tau * spinTurn(t)
				x, y := onSwing(&A, A.R-5, U)
				far := math.Sin(A.a0+A.dir*U) < 0
				p.Glow(x, y, either(far, 26.0, 38), C[3], either(far, 0.3, 0.5)*(1-pixelfx.Smooth(spinSW-0.04, spinSW+0.05, t)))
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
