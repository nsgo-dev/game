// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/spear-sweep.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crescent, trace and glint recipe from Sprite Fusion's open-source slash, and the rolling dust and grit from his explosion (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	// upright in one hand; two hands take it low across the belt, he winds right round, and it whips through the front
	spearRest = pixelfx.Ready.With(pixelfx.SA.Is(-1.42), pixelfx.FX.Is(11), pixelfx.FY.Is(-21), pixelfx.BX.Is(7), pixelfx.BY.Is(-16)).Hold("spear")
	spearCock = spearRest.With(pixelfx.Two.On(), pixelfx.Yaw.Is(2.5), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(2), pixelfx.FX.Is(15), pixelfx.FY.Is(-17), pixelfx.SA.Is(0.05), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-8))
	spearOut  = spearRest.With(pixelfx.Two.On(), pixelfx.X.Is(2), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(6), pixelfx.FX.Is(20), pixelfx.FY.Is(-15), pixelfx.SA.Is(0.04), pixelfx.FFX.Is(12), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
)

// SpearSweep is a wide low sweep at shin height: a long flat crescent swings from behind the legs round the front, and the wind of it kicks a wake of dust and grit along the floor. It is registered as "melee/spear-sweep".
var SpearSweep = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Spear sweep",
		Desc:     "A wide low sweep at shin height: a long flat crescent swings from behind the legs round the front, and the wind of it kicks a wake of dust and grit along the floor.",
		Tags:     []string{"melee", "sweep", "spear", "polearm", "low", "aoe", "ground", "dust", "combat", "one-shot"},
		Size:     [2]int{216, 84},
		Origin:   [2]int{104, 70},
		Duration: 1.5,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0f2a2a", "#18504a", "#22806e", "#3fb894", "#8fe6c0", "#dcfff0", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "seed", Default: pixelfx.Num(10)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{62},
		Poses: []pixelfx.Key{
			{T: -0.56, Pose: spearRest},
			{T: -0.34, Pose: spearRest.With(pixelfx.Two.On(), pixelfx.Crouch.Is(4), pixelfx.FX.Is(14), pixelfx.FY.Is(-22), pixelfx.SA.Is(-0.7))},
			{T: -0.14, Pose: spearCock},
			{T: -0.02, Pose: spearCock.With(pixelfx.Crouch.Is(7), pixelfx.Yaw.Is(2.7))},
			{T: 0.045, Pose: spearOut.With(pixelfx.X.Is(1), pixelfx.Yaw.Is(1.3), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.09, Pose: spearOut.With(pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.14, Pose: spearOut, How: "snap"},
			{T: 0.62, Pose: spearOut.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(5))},
			{T: 1.12, Pose: spearRest.With(pixelfx.Two.On(), pixelfx.Crouch.Is(4), pixelfx.SA.Is(-0.7), pixelfx.FX.Is(14), pixelfx.FY.Is(-22))},
			{T: 1.5, Pose: spearRest},
		},
		Hits: []pixelfx.Hit{{T: 0.1, Stop: 0.12, Shake: 0.18, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.6), Straw: pixelfx.Val(18), Y: pixelfx.Val(-12)}},
		Dust: []pixelfx.DustKick{{T: 0.09, X: 14, N: 6, Dir: 1}, {T: 0.09, X: -10, N: 5, Dir: -1}},
	},
	New: newSpearSweep,
}

func init() { pixelfx.Register("melee/spear-sweep", SpearSweep) }

func newSpearSweep(o *pixelfx.Options) pixelfx.Instance {
	C, st := pixelfx.Fit(o.Palette(), 7), stone(o.Color("stone"))
	Rg, ps, lights := newSim(o.Seed(), o.Ramp("sparks"))
	// from behind the left leg, round the front, out to the right; the far part is behind the fighter
	A := swing{cx: 2, cy: -12, R: 50, sy: 0.2, k: 0.19, rot: -0.04, a0: pi + 0.55, dir: -1, span: pi + 0.35, W: 19, t0: 0, sweep: 0.13, hold: 0.05, trail: 0.3, ramp: C}
	end := slashEnd(A)
	t, lastU, done := 0.0, 0.0, false
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the wind under the blade kicks dust up off the floor as it passes, rolling on the way it goes
			U := A.span * pixelfx.Ease.OutExpo(pixelfx.Clamp(t/A.sweep))
			for u := lastU; u < U; u += 0.09 {
				tr := travel(&A, u)
				near := math.Sin(tr.a) >= 0
				fy, sp := either(near, -1.0, -3), Rg.Range(30, 90)
				ps.Spawn(pixelfx.Smoke, tr.x, fy-Rg.Range(0, 2), tr.dx*sp, -Rg.Range(4, 16), Rg.Range(0.7, 1.15), st.dust, Rg.Range(3, 5.5), Rg.Range(0, 0.05), 0)
				if Rg.Chance(0.6) {
					ps.Spawn(pixelfx.Grit, tr.x, fy, tr.dx*Rg.Range(40, 120)+Rg.Range(-20, 20), -Rg.Range(40, 130), Rg.Range(0.6, 1.0), pixelfx.Pick(Rg, []uint32{st.light, st.body, st.shade}), either(Rg.Chance(0.3), 2.0, 1), 0, 0)
				}
				if Rg.Chance(0.35) {
					ps.Spawn(pixelfx.Mote, tr.x, tr.y, tr.dx*Rg.Range(40, 100), tr.dy*40-Rg.Range(5, 20), Rg.Range(0.2, 0.4), either(Rg.Chance(0.5), C[4], C[5]), 0.6, 0, backIf(!near))
				}
			}
			lastU = U
			if !done && t >= A.sweep {
				done = true
				lights.Add(end.x, end.y-4, 50, C[3], 0.5, 0.35)
				for range 8 {
					a, sp := math.Atan2(end.dy, end.dx)+Rg.Range(-0.5, 0.3), Rg.Range(120, 260)
					ps.Spawn(pixelfx.Spark, end.x, end.y, math.Cos(a)*sp, math.Sin(a)*sp-40, Rg.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
				}
				// the wake piles up and rolls on past the tip
				for range 6 {
					ps.Spawn(pixelfx.Smoke, end.x+Rg.Range(-6, 4), -Rg.Range(1, 4), Rg.Range(30, 90), -Rg.Range(4, 14), Rg.Range(0.9, 1.25), st.dust, Rg.Range(4.5, 7), Rg.Range(0, 0.08), pixelfx.Big)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			slash(p, &A, t, -1)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			slash(p, &A, t, 1)
			if g := t - A.sweep*0.6; g > 0 && g < 0.24 {
				pixelfx.Sparkle(p, end.x, end.y, 9*pixelfx.Ease.OutBack(math.Min(1, g/0.05))*(1-g/0.24), C[5], C[3], 0.55)
			}
			// light rides the edge
			if t < A.sweep+0.12 {
				x, y := onSwing(&A, A.R-5, A.span*pixelfx.Ease.OutExpo(pixelfx.Clamp(t/A.sweep)))
				p.Glow(x, y, 36, C[3], 0.45*(1-t/(A.sweep+0.12)))
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
