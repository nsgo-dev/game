// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/dagger-flurry.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crescent, trace, glint and spark recipe from Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var daggerBase = pixelfx.Ready.With(pixelfx.SA.Is(-0.5), pixelfx.SB.Is(-0.9), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(3), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-9), pixelfx.FX.Is(12), pixelfx.FY.Is(-24), pixelfx.BX.Is(9), pixelfx.BY.Is(-24)).Hold("dagger").HoldOff("dagger")

// daggerNear is a stab with the near hand while the far one comes back to the chest; the shoulders turn into it.
func daggerNear(fx, fy, sa float64, more ...pixelfx.PoseValue) pixelfx.Pose {
	return daggerBase.With(pixelfx.FX.Is(fx), pixelfx.FY.Is(fy), pixelfx.SA.Is(sa), pixelfx.BX.Is(4), pixelfx.BY.Is(-24), pixelfx.SB.Is(-1)).With(more...)
}

// daggerFar is a stab with the far hand while the near one comes back to the chest.
func daggerFar(bx, by, sb float64, more ...pixelfx.PoseValue) pixelfx.Pose {
	return daggerBase.With(pixelfx.BX.Is(bx), pixelfx.BY.Is(by), pixelfx.SB.Is(sb), pixelfx.FX.Is(1), pixelfx.FY.Is(-23), pixelfx.SA.Is(-1)).With(more...)
}

// DaggerFlurry is a flurry of small quick crescents at every angle in front of the fighter, each with a flick of sparks, finished by one big cut that glints and bursts. It is registered as "melee/dagger-flurry".
var DaggerFlurry = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Dagger flurry",
		Desc:     "A flurry of small quick crescents at every angle in front of the fighter, each with a flick of sparks, finished by one big cut that glints and bursts.",
		Tags:     []string{"melee", "slash", "dagger", "combo", "flurry", "fast", "combat", "directional", "one-shot"},
		Size:     [2]int{148, 104},
		Origin:   [2]int{36, 86},
		Duration: 0.95,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1b1630", "#3a2f63", "#6150a3", "#9480d8", "#c7b8f5", "#efe9ff", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#ffffff", "#efe9ff", "#c7b8f5", "#9480d8")},
			{Name: "seed", Default: pixelfx.Num(12)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{60},
		Poses: []pixelfx.Key{
			{T: -0.4, Pose: daggerBase.With(pixelfx.X.Is(-4))},
			{T: -0.14, Pose: daggerBase.With(pixelfx.X.Is(-6), pixelfx.Lean.Is(-2), pixelfx.Crouch.Is(5), pixelfx.FX.Is(2), pixelfx.FY.Is(-22), pixelfx.SA.Is(-1.2), pixelfx.BX.Is(2), pixelfx.BY.Is(-26), pixelfx.SB.Is(-1.5))},
			{T: -0.03, Pose: daggerBase.With(pixelfx.X.Is(-6), pixelfx.Lean.Is(-3), pixelfx.Crouch.Is(6), pixelfx.FX.Is(1), pixelfx.FY.Is(-21), pixelfx.SA.Is(-1.2), pixelfx.BX.Is(1), pixelfx.BY.Is(-25), pixelfx.SB.Is(-1.5))},
			{T: 0, Pose: daggerNear(20, -31, -0.5, pixelfx.X.Is(2), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.055, Pose: daggerFar(21, -23, 0.5, pixelfx.X.Is(3)), How: "snap"},
			{T: 0.11, Pose: daggerNear(18, -36, -1, pixelfx.X.Is(4)), How: "snap"},
			{T: 0.165, Pose: daggerFar(22, -31, -0.6, pixelfx.X.Is(5)), How: "snap"},
			{T: 0.22, Pose: daggerNear(20, -22, 0.2, pixelfx.X.Is(5)), How: "snap"},
			{T: 0.275, Pose: daggerFar(21, -27, 0.05, pixelfx.X.Is(6)), How: "snap"},
			{T: 0.33, Pose: daggerBase.With(pixelfx.X.Is(4), pixelfx.Lean.Is(-3), pixelfx.Crouch.Is(5), pixelfx.FX.Is(-1), pixelfx.FY.Is(-33), pixelfx.SA.Is(-2.5), pixelfx.BX.Is(2), pixelfx.BY.Is(-23), pixelfx.SB.Is(-1.2)), How: "snap"},
			{T: 0.37, Pose: daggerBase.With(pixelfx.X.Is(7), pixelfx.Lean.Is(2), pixelfx.Crouch.Is(4), pixelfx.FX.Is(16), pixelfx.FY.Is(-34), pixelfx.SA.Is(-1), pixelfx.BX.Is(-7), pixelfx.BY.Is(-24), pixelfx.SB.Is(-2.4), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.43, Pose: daggerBase.With(pixelfx.X.Is(9), pixelfx.Lean.Is(6), pixelfx.Crouch.Is(6), pixelfx.FX.Is(22), pixelfx.FY.Is(-23), pixelfx.SA.Is(0.15), pixelfx.BX.Is(-9), pixelfx.BY.Is(-24), pixelfx.SB.Is(-2.6), pixelfx.FFX.Is(13), pixelfx.FBX.Is(-10), pixelfx.ToeB.On()), How: "snap"},
			{T: 0.68, Pose: daggerBase.With(pixelfx.X.Is(9), pixelfx.Lean.Is(5), pixelfx.Crouch.Is(5), pixelfx.FX.Is(21), pixelfx.FY.Is(-22), pixelfx.SA.Is(0.2), pixelfx.BX.Is(-8), pixelfx.BY.Is(-24), pixelfx.SB.Is(-2.6))},
			{T: 1.05, Pose: daggerBase.With(pixelfx.X.Is(-4))},
		},
		Hits: []pixelfx.Hit{
			{T: 0.05, Stop: 0.015, Push: pixelfx.Val(0.5), Straw: pixelfx.Val(3), Flash: pixelfx.Val(0.02)},
			{T: 0.105, Stop: 0.015, Push: pixelfx.Val(-0.3), Straw: pixelfx.Val(3), Flash: pixelfx.Val(0.02)},
			{T: 0.16, Stop: 0.015, Push: pixelfx.Val(0.5), Straw: pixelfx.Val(3), Flash: pixelfx.Val(0.02)},
			{T: 0.215, Stop: 0.015, Push: pixelfx.Val(-0.3), Straw: pixelfx.Val(3), Flash: pixelfx.Val(0.02)},
			{T: 0.27, Stop: 0.015, Push: pixelfx.Val(0.5), Straw: pixelfx.Val(3), Flash: pixelfx.Val(0.02)},
			{T: 0.325, Stop: 0.015, Push: pixelfx.Val(-0.3), Straw: pixelfx.Val(3), Flash: pixelfx.Val(0.02)},
			{T: 0.42, Stop: 0.12, Shake: 0.18, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.6), Straw: pixelfx.Val(16)},
		},
		Dust: []pixelfx.DustKick{{T: 0.43, X: 20, N: 7, Dir: 1}, {T: 0, X: -6, N: 3, Dir: -1}},
	},
	New: newDaggerFlurry,
}

func init() { pixelfx.Register("melee/dagger-flurry", DaggerFlurry) }

// daggerEvery is the gap between the small cuts.
var daggerEvery = 0.055

// daggerCuts are the small cuts: where, how they're turned, which way they go.
var daggerCuts = [...][4]float64{
	{21, -27, -0.5, 1}, {33, -16, 0.9, -1}, {26, -38, 2.4, 1}, {37, -30, -1.8, -1}, {19, -17, 0.2, 1}, {30, -24, 1.4, -1},
}

// flyingSliver is a sliver thrown off a cut: its angle, starting radius, speed and arc.
type flyingSliver struct{ a, r, v, arc float64 }

// newSlivers returns slivers flying off at each of angles, jittered by R.
func newSlivers(R *pixelfx.Rng, angles []float64, r0, r1, v0, v1 float64) []flyingSliver {
	out := make([]flyingSliver, len(angles))
	for i, a := range angles {
		out[i].a = a + R.Range(-0.2, 0.2)
		out[i].r = R.Range(r0, r1)
		out[i].v = R.Range(v0, v1)
		out[i].arc = R.Range(0.4, 0.7)
	}
	return out
}

// sliverCol is the color of a sliver k of the way through its flight.
func sliverCol(C []uint32, k float64) uint32 {
	switch {
	case k < 0.4:
		return C[6]
	case k < 0.7:
		return C[5]
	}
	return C[4]
}

func newDaggerFlurry(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	Rg, ps, lights := newSim(o.Seed(), o.Ramp("sparks"))
	all := make([]swing, 0, len(daggerCuts)+1)
	for i, c := range daggerCuts {
		s := swing{cx: c[0], cy: c[1], rot: c[2], dir: c[3]}
		s.R = Rg.Range(12, 15)
		s.sy = Rg.Range(0.55, 0.75)
		s.a0 = Rg.Range(-2.8, -2.2)
		s.span = Rg.Range(2.3, 2.8)
		s.W = Rg.Range(5.2, 6.4)
		s.t0, s.sweep, s.hold, s.trail, s.ramp = float64(i)*daggerEvery, 0.035, 0.03, 0.13, C
		all = append(all, s)
	}
	// the finisher: one wide flat cut right across the front, left to right
	all = append(all, swing{cx: 24, cy: -26, R: 30, sy: 0.36, k: 0.24, rot: 0.12, a0: 3.0, dir: -1, span: 2.85, W: 11, t0: float64(len(daggerCuts))*daggerEvery + 0.04, sweep: 0.055, hold: 0.06, trail: 0.24, ramp: C})
	last := len(all) - 1
	big := &all[last]
	ends, done := make([]travelAt, len(all)), make([]bool, len(all))
	for i := range all {
		ends[i] = slashEnd(all[i])
	}
	slivers := newSlivers(Rg, []float64{-2.6, -1.5, -0.4, 0.7}, 4, 7, 70, 110)
	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := range all {
				s := &all[i]
				if done[i] || t < s.t0+s.sweep {
					continue
				}
				done[i] = true
				e, fin := ends[i], i == last
				lights.Add(e.x, e.y, either(fin, 56.0, 26), C[3], either(fin, 0.6, 0.4), either(fin, 0.4, 0.14))
				if fin {
					lights.Add(e.x, e.y, 16, pixelfx.White, 0.7, 0.08)
				}
				for range either(fin, 12, 3) {
					a, sp := math.Atan2(e.dy, e.dx)+Rg.Range(-0.5, 0.5), Rg.Range(100, either(fin, 340.0, 220))
					ps.Spawn(pixelfx.Spark, e.x, e.y, math.Cos(a)*sp, math.Sin(a)*sp, Rg.Range(0.1, either(fin, 0.35, 0.22)), pixelfx.White, 1, 0, 0)
				}
				shavings(ps, Rg, s, either(fin, 14, 4), []uint32{C[4], C[5]}, 0.3)
				if fin {
					for range 5 {
						ps.Spawn(pixelfx.StarKind, s.cx+Rg.Range(-22, 22), s.cy+Rg.Range(-14, 14), 0, -4, Rg.Range(0.2, 0.45), either(Rg.Chance(0.5), C[5], pixelfx.White), Rg.Range(1, 2.2), Rg.Range(0.05, 0.25), 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			for i := range all {
				slash(p, &all[i], t, 0)
			}
			for i := range all {
				s := &all[i]
				e, fin := ends[i], i == last
				g, life := t-s.t0-s.sweep*0.6, either(fin, 0.24, 0.12)
				if g > 0 && g < life {
					pixelfx.Sparkle(p, e.x, e.y, either(fin, 10.0, 4)*pixelfx.Ease.OutBack(math.Min(1, g/0.04))*(1-g/life), C[5], C[3], either(fin, 0.6, 0.35))
				}
				// light rides each edge while it's moving
				if u := t - s.t0; u > 0 && u < s.sweep+0.1 {
					h := *s
					h.span = s.span * pixelfx.Ease.OutExpo(pixelfx.Clamp(u/s.sweep))
					he := slashEnd(h)
					p.Glow(he.x, he.y, either(fin, 40.0, 22), C[3], either(fin, 0.5, 0.35)*(1-u/(s.sweep+0.1)))
				}
			}
			// the finisher throws slivers off its end
			if q := t - big.t0 - big.sweep; q > 0.02 && q < 0.14 {
				k, e := (q-0.02)/0.12, ends[last]
				for _, s := range slivers {
					sliver(p, e.x, e.y, s.a+math.Atan2(e.dy, e.dx), s.r+s.v*(q-0.02), s.arc*(1-0.4*k), sliverCol(C, k), 1-k)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
