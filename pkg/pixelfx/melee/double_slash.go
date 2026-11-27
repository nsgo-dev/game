// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/double-slash.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crescent, trace, glint and spark recipe from Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	doubleStance = pixelfx.Ready.With(pixelfx.FX.Is(12), pixelfx.FY.Is(-21), pixelfx.SA.Is(-0.85)).Hold("sword")
	doubleHigh   = doubleStance.With(pixelfx.X.Is(-7), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-4), pixelfx.FX.Is(-7), pixelfx.FY.Is(-35), pixelfx.SA.Is(-2.65), pixelfx.BX.Is(9), pixelfx.BY.Is(-25), pixelfx.ArmsFront.On(), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-8))
	doubleLow    = doubleStance.With(pixelfx.X.Is(3), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(6), pixelfx.FX.Is(22), pixelfx.FY.Is(-20), pixelfx.SA.Is(0.62), pixelfx.BX.Is(-8), pixelfx.BY.Is(-24), pixelfx.FFX.Is(13), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
)

// DoubleSlash is a down cut, then an up cut that crosses it: the moment the X closes both strokes flash white, the crossing bursts with light and sparks, and a dotted X lingers. It is registered as "melee/double-slash".
var DoubleSlash = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Double slash",
		Desc:     "A down cut, then an up cut that crosses it: the moment the X closes both strokes flash white, the crossing bursts with light and sparks, and a dotted X lingers.",
		Tags:     []string{"melee", "slash", "combo", "sword", "cross", "combat", "directional", "one-shot"},
		Size:     [2]int{164, 108},
		Origin:   [2]int{44, 92},
		Duration: 0.8,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a0a22", "#5e1238", "#a01f4e", "#e0386a", "#ff86a6", "#ffd2dd", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{62},
		Poses: []pixelfx.Key{
			{T: -0.42, Pose: doubleStance.With(pixelfx.X.Is(-7))},
			{T: -0.18, Pose: doubleHigh},
			{T: -0.04, Pose: doubleHigh.With(pixelfx.Crouch.Is(5))},
			{T: 0, Pose: doubleStance.With(pixelfx.X.Is(0), pixelfx.Crouch.Is(2), pixelfx.Lean.Is(3), pixelfx.FX.Is(16), pixelfx.FY.Is(-37), pixelfx.SA.Is(-1.45), pixelfx.BX.Is(-7), pixelfx.BY.Is(-24), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-9), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.06, Pose: doubleLow, How: "snap"},
			{T: 0.14, Pose: doubleLow.With(pixelfx.X.Is(4), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(5), pixelfx.FX.Is(20), pixelfx.FY.Is(-17), pixelfx.SA.Is(0.85), pixelfx.Ghost.Is(1))},
			{T: 0.17, Pose: doubleLow.With(pixelfx.X.Is(5), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(5), pixelfx.FX.Is(22), pixelfx.FY.Is(-28), pixelfx.SA.Is(0.1), pixelfx.BY.Is(-18), pixelfx.ToeB.On(), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.21, Pose: doubleLow.With(pixelfx.X.Is(6), pixelfx.Crouch.Is(1), pixelfx.Lean.Is(6), pixelfx.FX.Is(19), pixelfx.FY.Is(-37), pixelfx.SA.Is(-0.95), pixelfx.BY.Is(-18), pixelfx.FFX.Is(13), pixelfx.FBY.Is(-6)), How: "snap"},
			{T: 0.5, Pose: doubleLow.With(pixelfx.X.Is(6), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(3), pixelfx.FX.Is(18), pixelfx.FY.Is(-36), pixelfx.SA.Is(-1.05), pixelfx.BY.Is(-19), pixelfx.FFX.Is(12))},
			{T: 0.92, Pose: doubleStance.With(pixelfx.X.Is(-7))},
		},
		Hits: []pixelfx.Hit{
			{T: 0.06, Stop: 0.05, Shake: 0.08, Amp: pixelfx.Val(1), Push: pixelfx.Val(1.4), Straw: pixelfx.Val(8)},
			{T: 0.16, Stop: 0.14, Shake: 0.2, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.8), Straw: pixelfx.Val(18), Y: pixelfx.Val(-28)},
		},
		Dust: []pixelfx.DustKick{{T: 0.06, X: 16, N: 5, Dir: 1}, {T: 0.17, X: -8, N: 4, Dir: -1}},
	},
	New: newDoubleSlash,
}

func init() { pixelfx.Register("melee/double-slash", DoubleSlash) }

var (
	// doubleUp is when the second cut starts
	doubleUp = 0.14
	// doubleR is the radius both cuts swing on
	doubleR = 62.0
)

// doubleCut returns a cut through (x, y) heading th, its point 0.55 of the way along span.
func doubleCut(x, y, th, dir, span float64) swing { return through(x, y, th, dir, doubleR, span, 0.55) }

func newDoubleSlash(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	Rg, ps, lights := newSim(o.Seed(), o.Ramp("sparks"))
	X, Y := 34.0, -23.0
	down := doubleCut(X, Y, 0.72, 1, 0.84)
	down.W, down.t0, down.sweep, down.hold, down.trail, down.ramp, down.lens = 10, 0, 0.06, 0.07, 0.24, C, true
	up := doubleCut(X, Y, -0.66, -1, 0.84)
	up.W, up.t0, up.sweep, up.hold, up.trail, up.ramp, up.lens = 11, doubleUp, 0.06, 0.06, 0.26, C, true
	ends := [2]travelAt{slashEnd(down), slashEnd(up)}
	slivers := newSlivers(Rg, []float64{-2.4, -1.1, 0.2, 1.6}, 5, 8, 70, 120)
	closeAt := doubleUp + 0.012
	t, cut1, cut2, closed := 0.0, false, false, false
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !cut1 && t >= down.sweep {
				cut1 = true
				shavings(ps, Rg, &down, 10, []uint32{C[4], C[5]}, 0.3)
				for range 6 {
					a, sp := math.Atan2(ends[0].dy, ends[0].dx)+Rg.Range(-0.5, 0.5), Rg.Range(120, 300)
					ps.Spawn(pixelfx.Spark, ends[0].x, ends[0].y, math.Cos(a)*sp, math.Sin(a)*sp, Rg.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
				}
			}
			if !closed && t >= closeAt {
				closed = true
				lights.Add(X, Y, 56, C[3], 0.45, 0.4)
				lights.Add(X, Y, 18, pixelfx.White, 0.6, 0.08)
				for range 18 {
					a, sp := Rg.Range(-pi, pi)*0.8, Rg.Range(140, 360)
					ps.Spawn(pixelfx.Spark, X, Y, math.Cos(a)*sp, math.Sin(a)*sp-40, Rg.Range(0.14, 0.4), pixelfx.White, 1, 0, 0)
				}
				for range 5 {
					ps.Spawn(pixelfx.StarKind, X+Rg.Range(-14, 14), Y+Rg.Range(-12, 12), 0, -4, Rg.Range(0.2, 0.4), either(Rg.Chance(0.5), C[5], pixelfx.White), Rg.Range(1, 2.2), Rg.Range(0.05, 0.2), 0)
				}
			}
			if !cut2 && t >= doubleUp+up.sweep {
				cut2 = true
				shavings(ps, Rg, &up, 12, []uint32{C[4], C[5]}, 0.3)
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// both strokes flash white for two frames when the X closes
			flash := either(t >= closeAt && t < closeAt+0.07, 1.0, 0)
			down.hot, up.hot = flash, flash
			slash(p, &down, t, 0)
			slash(p, &up, t, 0)
			// glints at the ends of the strokes
			for i, s := range [2]*swing{&down, &up} {
				if g := t - s.t0 - s.sweep*0.6; g > 0 && g < 0.2 {
					pixelfx.Sparkle(p, ends[i].x, ends[i].y, 7*pixelfx.Ease.OutBack(math.Min(1, g/0.05))*(1-g/0.2), C[5], C[3], 0.5)
				}
			}
			// the crossing: a flare, slivers flying off it, light
			q := t - closeAt
			if q >= 0 && q < 0.26 {
				k := q / 0.26
				pixelfx.Flare(p, X, Y, 22*math.Pow(1-k, 1.4), 1-k, C)
				p.Glow(X, Y, 34, C[4], 0.35*(1-k))
			}
			if q >= 0.03 && q < 0.15 {
				k := (q - 0.03) / 0.12
				for _, s := range slivers {
					sliver(p, X, Y, s.a, s.r+s.v*(q-0.03), s.arc*(1-0.4*k), sliverCol(C, k), 1-k)
				}
			}
			// light rides each edge as it goes
			for _, s := range [2]*swing{&down, &up} {
				u := t - s.t0
				if u <= 0 || u >= 0.25 {
					continue
				}
				h := *s
				h.span = s.span * pixelfx.Ease.OutExpo(pixelfx.Clamp(u/s.sweep))
				e := slashEnd(h)
				p.Glow(e.x, e.y, 40, C[3], 0.5*(1-u/0.25))
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
