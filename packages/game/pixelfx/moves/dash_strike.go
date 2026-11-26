// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/dash-strike.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/impacts"
	"github.com/nsgo-dev/game/packages/game/pixelfx/melee"
)

// When he cuts and when the blow lands, in effect time; variables so sums with them round as pixelfx's do.
var (
	dashCut = 0.16
	dashHit = dashCut + 0.06
)

const (
	dashAt = 116
	dashTX = 176
)

var (
	dashGuard = pixelfx.Ready.Hold("sword").With(pixelfx.FX.Is(12), pixelfx.FY.Is(-21), pixelfx.SA.Is(-0.85))
	dashLow   = dashGuard.With(pixelfx.Crouch.Is(6), pixelfx.Lean.Is(6), pixelfx.FX.Is(-3), pixelfx.FY.Is(-17), pixelfx.SA.Is(3.05), pixelfx.BX.Is(9), pixelfx.BY.Is(-23), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-10))
	dashCock  = dashGuard.With(pixelfx.X.Is(dashAt-4), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(-2), pixelfx.FX.Is(-7), pixelfx.FY.Is(-35), pixelfx.SA.Is(-2.65), pixelfx.BX.Is(9), pixelfx.BY.Is(-25), pixelfx.ArmsFront.On(), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-7))
	dashCutP  = dashGuard.With(pixelfx.X.Is(dashAt), pixelfx.Crouch.Is(2), pixelfx.Lean.Is(3), pixelfx.FX.Is(16), pixelfx.FY.Is(-37), pixelfx.SA.Is(-1.45), pixelfx.BX.Is(-7), pixelfx.BY.Is(-24), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-10))
	dashDown  = dashGuard.With(pixelfx.X.Is(dashAt+2), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(5), pixelfx.FX.Is(20), pixelfx.FY.Is(-21), pixelfx.SA.Is(0.62), pixelfx.BX.Is(-9), pixelfx.BY.Is(-24), pixelfx.FFX.Is(13), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
	dashHop   = dashGuard.With(pixelfx.Y.Is(-14), pixelfx.Crouch.Is(0), pixelfx.Lean.Is(-3), pixelfx.FX.Is(13), pixelfx.FY.Is(-31), pixelfx.SA.Is(-0.9), pixelfx.BX.Is(-8), pixelfx.BY.Is(-28), pixelfx.FFX.Is(6), pixelfx.FFY.Is(-8), pixelfx.FBX.Is(-6), pixelfx.FBY.Is(-6), pixelfx.Ghost.Is(1))
)

// DashStrike is a move: a crouch, then he's across the floor in a blur of after-images and already cutting; a critical hit, a beat, and he hops back to where he started. It chains melee.SwordSlash and impacts.CritHit, and is registered as "moves/dash-strike".
var DashStrike = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Dash strike",
		Desc:     "A crouch, then he's across the floor in a blur of after-images and already cutting: a critical hit, a beat, and he hops back to where he started.",
		Tags:     []string{"move", "melee", "sword", "dash", "crit", "one-shot"},
		Size:     [2]int{372, 156},
		Origin:   [2]int{66, 136},
		Duration: 2.3,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{dashTX},
		Poses: []pixelfx.Key{
			{T: -0.5, Pose: dashGuard.With(pixelfx.X.Is(0))},
			{T: -0.16, Pose: dashLow.With(pixelfx.X.Is(-2))},
			{T: -0.01, Pose: dashLow.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(6))},
			{T: 0, Pose: dashLow.With(pixelfx.X.Is(8), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.14, Pose: dashLow.With(pixelfx.X.Is(dashAt-6), pixelfx.Ghost.Is(1)), How: "line"},
			{T: 0.145, Pose: dashCock, How: "snap"},
			{T: dashCut, Pose: dashCutP, How: "snap"},
			{T: dashHit, Pose: dashDown, How: "snap"},
			{T: 0.75, Pose: dashDown.With(pixelfx.Crouch.Is(4), pixelfx.Lean.Is(4))},
			{T: 1.25, Pose: dashGuard.With(pixelfx.X.Is(dashAt + 2))},
			{T: 1.32, Pose: dashGuard.With(pixelfx.X.Is(dashAt+2), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-2)), How: "snap"},
			{T: 1.4, Pose: dashHop.With(pixelfx.X.Is(dashAt - 28)), How: "snap"},
			{T: 1.6, Pose: dashHop.With(pixelfx.X.Is(22), pixelfx.Y.Is(-18)), How: "line"},
			{T: 1.74, Pose: dashGuard.With(pixelfx.X.Is(0), pixelfx.Y.Is(0), pixelfx.Crouch.Is(5)), How: "line"},
			{T: 2.2, Pose: dashGuard.With(pixelfx.X.Is(0))},
		},
		Hits: []pixelfx.Hit{
			{T: dashHit, Stop: 0.17, Shake: 0.26, Amp: pixelfx.Val(3), Push: pixelfx.Val(3.4), Straw: pixelfx.Val(32)},
		},
		Dust: []pixelfx.DustKick{
			{T: 0, X: 0, N: 8, Dir: -1},
			{T: 0.14, X: dashAt - 6, N: 7},
			{T: 1.74, X: 0, N: 6},
		},
	},
	New: pixelfx.Chain([]pixelfx.Cue{
		{FX: melee.SwordSlash, T: dashCut, X: dashAt},
		{FX: impacts.CritHit, T: dashHit, X: dashTX - 10, Options: map[string]pixelfx.Value{"height": pixelfx.Num(28)}},
	}),
}

func init() { pixelfx.Register("moves/dash-strike", DashStrike) }
