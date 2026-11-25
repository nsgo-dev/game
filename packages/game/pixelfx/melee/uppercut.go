// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/uppercut.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crescent recipe from Sprite Fusion's open-source slash, and the star from his impact (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	uppercutFists = pixelfx.Ready.With(pixelfx.FX.Is(12), pixelfx.FY.Is(-25), pixelfx.BX.Is(9), pixelfx.BY.Is(-27), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(1))
	uppercutCoil  = uppercutFists.With(pixelfx.Crouch.Is(7), pixelfx.Lean.Is(3), pixelfx.FX.Is(8), pixelfx.FY.Is(-11), pixelfx.BX.Is(8), pixelfx.BY.Is(-27), pixelfx.FFX.Is(10), pixelfx.FBX.Is(-9))
	uppercutTop   = uppercutFists.With(pixelfx.X.Is(1), pixelfx.Y.Is(-11), pixelfx.Crouch.Is(-1), pixelfx.Lean.Is(2), pixelfx.FX.Is(12), pixelfx.FY.Is(-42), pixelfx.BX.Is(-6), pixelfx.BY.Is(-21), pixelfx.FFX.Is(8), pixelfx.FFY.Is(-8), pixelfx.FBX.Is(-3), pixelfx.FBY.Is(-2), pixelfx.ToeB.On(), pixelfx.Ghost.Is(1))
)

// Uppercut is a crescent that rises from the floor past the face, and at the top a comic impact star bursts: bold outline, hot core, burst lines, twinkles and sparks. It is registered as "melee/uppercut".
var Uppercut = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Uppercut",
		Desc:     "A crescent rises from the floor past the face, and at the top a comic impact star bursts: bold outline, hot core, burst lines, twinkles and sparks.",
		Tags:     []string{"melee", "uppercut", "punch", "rising", "launcher", "comic", "combat", "directional", "one-shot"},
		Size:     [2]int{132, 128},
		Origin:   [2]int{48, 110},
		Duration: 0.85,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#3a1408", "#7a2a10", "#c4501a", "#f08a2a", "#ffc94a", "#fff2a8", "#ffffff")},
			{Name: "outline", Default: pixelfx.Color("#2a0a08")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "seed", Default: pixelfx.Num(11)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{44},
		Poses: []pixelfx.Key{
			{T: -0.42, Pose: uppercutFists},
			{T: -0.16, Pose: uppercutCoil},
			{T: 0, Pose: uppercutCoil.With(pixelfx.Crouch.Is(8))},
			{T: 0.03, Pose: uppercutFists.With(pixelfx.Y.Is(-4), pixelfx.Crouch.Is(1), pixelfx.Lean.Is(4), pixelfx.FX.Is(18), pixelfx.FY.Is(-27), pixelfx.BX.Is(-5), pixelfx.BY.Is(-23), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-7), pixelfx.ToeB.On(), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.076, Pose: uppercutTop, How: "snap"},
			// pixelfx's ghost: undefined leaves the key unsaid
			{T: 0.3, Pose: uppercutTop.With(pixelfx.Y.Is(-13)).Without(pixelfx.Ghost)},
			{T: 0.56, Pose: uppercutTop.With(pixelfx.Y.Is(0), pixelfx.Crouch.Is(2), pixelfx.FX.Is(13), pixelfx.FY.Is(-31), pixelfx.FFX.Is(7), pixelfx.FFY.Is(-6), pixelfx.FBX.Is(-5), pixelfx.FBY.Is(-5)).Without(pixelfx.Ghost), How: "in"},
			{T: 0.6, Pose: uppercutFists.With(pixelfx.Y.Is(0), pixelfx.Crouch.Is(7), pixelfx.Lean.Is(3), pixelfx.FX.Is(12), pixelfx.FY.Is(-14), pixelfx.BX.Is(-6), pixelfx.BY.Is(-16), pixelfx.FFX.Is(10), pixelfx.FBX.Is(-9)), How: "snap"},
			{T: 0.98, Pose: uppercutFists},
		},
		Hits: []pixelfx.Hit{{T: 0.076, Stop: 0.13, Shake: 0.18, Amp: pixelfx.Val(2), Push: pixelfx.Val(-2.6), Straw: pixelfx.Val(18), Y: pixelfx.Val(-34)}},
		Dust: []pixelfx.DustKick{{T: 0.03, X: 8, N: 6}, {T: 0.6, X: 0, N: 6}},
	},
	New: newUppercut,
}

func init() { pixelfx.Register("melee/uppercut", Uppercut) }

// uppercutDust is the color of the dust the fist kicks up off the floor.
var uppercutDust = pixelfx.Hex("#6a6080")

// burstLine is a line bursting out round the star: its angle, where it starts and how long it is.
type burstLine struct{ a, r0, L float64 }

// ringCol is the color of a ring k of the way through bursting out: white, then down the ramp.
func ringCol(C []uint32, k float64) uint32 {
	switch {
	case k < 0.25:
		return pixelfx.White
	case k < 0.5:
		return C[5]
	case k < 0.75:
		return C[4]
	}
	return C[3]
}

func newUppercut(o *pixelfx.Options) pixelfx.Instance {
	C, INK := pixelfx.Fit(o.Palette(), 7), o.Color("outline")
	Rg, ps, lights := newSim(o.Seed(), o.Ramp("sparks"))
	tau := pixelfx.Tau
	// up from the floor in front, bowing forward past the face, to the top
	A := swing{cx: 8, cy: -25, R: 27, sy: 1, rot: 0, a0: 1.08, dir: -1, span: 2.5, W: 13, t0: 0.02, sweep: 0.07, hold: 0.05, trail: 0.22, ramp: C}
	top := slashEnd(A)
	SX, SY, HIT := top.x+3, top.y-5, A.t0+A.sweep*0.8
	spikes := make([]spike, 10)
	for i := range spikes {
		fi := float64(i)
		spikes[i].a = (fi/10)*tau + Rg.Range(-0.12, 0.12) - pi/2
		spikes[i].len = either(i%2 != 0, 7.0, 13) + Rg.Range(0, 4)
		spikes[i].w = either(i%2 != 0, 2.2, 3)
	}
	bursts := make([]burstLine, 8)
	for i := range bursts {
		bursts[i].a = (float64(i)/8)*tau + 0.2 + Rg.Range(-0.15, 0.15)
		bursts[i].r0 = Rg.Range(15, 18)
		bursts[i].L = Rg.Range(5, 9)
	}
	turned := make([]spike, len(spikes))
	t, hit, kicked := 0.0, false, false
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !kicked {
				kicked = true
				// the fist comes up off the floor
				for range 4 {
					ps.Spawn(pixelfx.Dust, 18+Rg.Range(-4, 4), -1, Rg.Range(-30, 30), -Rg.Range(5, 20), Rg.Range(0.3, 0.55), uppercutDust, Rg.Range(1.5, 2.6), 0, 0)
				}
			}
			if !hit && t >= HIT {
				hit = true
				lights.Add(SX, SY, 56, C[4], 0.6, 0.4)
				lights.Add(SX, SY, 18, pixelfx.White, 0.7, 0.08)
				for range 16 {
					a, sp := -pi/2+Rg.Range(-1.6, 1.6), Rg.Range(120, 320)
					ps.Spawn(pixelfx.Spark, SX, SY, math.Cos(a)*sp, math.Sin(a)*sp, Rg.Range(0.15, 0.45), pixelfx.White, 1, 0, 0)
				}
				for range 6 {
					a, d := Rg.Range(0, tau), Rg.Range(14, 24)
					ps.Spawn(pixelfx.StarKind, SX+math.Cos(a)*d, SY+math.Sin(a)*d, math.Cos(a)*20, math.Sin(a)*20-10, Rg.Range(0.25, 0.5), either(Rg.Chance(0.5), C[5], pixelfx.White), Rg.Range(1.4, 2.6), Rg.Range(0.04, 0.18), 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			slash(p, &A, t, 0)
			// speed lines rising beside the crescent while it goes up
			u := t - A.t0
			if u > 0 && u < 0.16 {
				for _, sl := range [...][3]float64{{A.cx + A.R + 7, -16, 18}, {A.cx + A.R + 11, -30, 12}, {A.cx - 4, -40, 10}} {
					dx, y0, L := sl[0], sl[1], sl[2]
					k := u / 0.16
					y := y0 - 30*k
					for d := 0.0; d < L; d++ {
						c := C[3]
						if d < 3 {
							c = pixelfx.White
						} else if d < L*0.5 {
							c = C[5]
						}
						p.Dot(dx, y+d, c, (1-d/L)*(1-k))
					}
				}
			}
			if u > 0 && u < A.sweep+0.06 {
				x, y := onSwing(&A, A.R-5, A.span*pixelfx.Ease.OutExpo(pixelfx.Clamp(u/A.sweep)))
				p.Glow(x, y, 34, C[3], 0.45*(1-u/(A.sweep+0.06)))
			}
			// the comic star: pops past full size, holds, then shrinks away turning, with burst lines round it
			q := t - HIT
			if q >= 0 && q < 0.36 {
				pop, out := pixelfx.OutBack(pixelfx.Clamp(q/0.05), 2.6), 1-pixelfx.Smooth(0.16, 0.36, q)
				sc, turn := pop*(0.35+0.65*out), q*1.2
				for i, s := range spikes {
					s.a += turn
					turned[i] = s
				}
				if sc > 0.08 {
					star(p, SX, SY, turned, sc*1.12, 1, INK, INK, INK, INK)
					star(p, SX, SY, turned, sc, 1, either(q < 0.05, pixelfx.White, C[6]), C[4], C[3], C[2])
				}
				p.Glow(SX, SY, 30, C[4], 0.45*out)
			}
			if q >= 0.02 && q < 0.24 {
				// a ring bursts out round the star, white to orange, breaking into dots
				k := (q - 0.02) / 0.22
				r, th, col := 7+17*pixelfx.Ease.OutCubic(k), math.Max(1, 2.2*(1-k)), ringCol(C, k)
				for a := 0.0; a < tau; a += 0.6 / r {
					for o := 0.0; o < th; o += 0.5 {
						p.Dot(SX+math.Cos(a)*(r-o), SY+math.Sin(a)*(r-o), col, 1.15-k)
					}
				}
			}
			if q >= 0.02 && q < 0.2 {
				k := (q - 0.02) / 0.18
				for _, b := range bursts {
					r0, ca, sa := b.r0+10*pixelfx.Ease.OutCubic(k), math.Cos(b.a), math.Sin(b.a)
					p.Line(SX+ca*r0, SY+sa*r0, SX+ca*(r0+b.L*(1-k)), SY+sa*(r0+b.L*(1-k)), either(k < 0.4, pixelfx.White, C[5]), 1.1-k)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
