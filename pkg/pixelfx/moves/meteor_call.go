// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/meteor-call.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/arcane"
	"github.com/nsgo-dev/game/pkg/pixelfx/fire"
)

// When he points and when the meteor hits, in effect time; variables so sums with them round as pixelfx's do.
var (
	meteorBolt = 1.52
	meteorHit  = 2.05
)

const meteorTX = 170

var (
	meteorChant  = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.1), pixelfx.Crouch.Is(1), pixelfx.FX.Is(-1), pixelfx.FY.Is(-26), pixelfx.BX.Is(2), pixelfx.BY.Is(-26), pixelfx.ArmsFront.On(), pixelfx.HY.Is(1), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	meteorUp     = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(-1), pixelfx.FX.Is(-7), pixelfx.FY.Is(-44), pixelfx.BX.Is(8), pixelfx.BY.Is(-44), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	meteorPoint  = pixelfx.Ready.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(5), pixelfx.FX.Is(17), pixelfx.FY.Is(-37), pixelfx.BX.Is(-6), pixelfx.BY.Is(-24), pixelfx.FFX.Is(12), pixelfx.FBX.Is(-9), pixelfx.ToeB.On())
	meteorFlinch = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.X.Is(-3), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(-4), pixelfx.FX.Is(-4), pixelfx.FY.Is(-36), pixelfx.BX.Is(5), pixelfx.BY.Is(-33), pixelfx.FFX.Is(4), pixelfx.FBX.Is(-4))
)

// MeteorCall is a move: a long chant; a circle draws itself round him, it flares, he points, and a burning rock comes down on the target and flattens the scene. It chains arcane.MagicCircleCast and fire.Meteor, and is registered as "moves/meteor-call".
var MeteorCall = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Meteor call",
		Desc:     "A long chant: a circle draws itself round him, it flares, he points, and a burning rock comes down on the target and flattens the scene.",
		Tags:     []string{"move", "fire", "spell", "meteor", "ultimate", "one-shot"},
		Size:     [2]int{380, 240},
		Origin:   [2]int{92, 212},
		Duration: meteorHit - 0.62 + 5,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{meteorTX},
		Poses: []pixelfx.Key{
			{T: -0.34, Pose: pixelfx.Ready},
			{T: 0, Pose: meteorChant},
			{T: 0.75, Pose: meteorChant.With(pixelfx.Crouch.Is(2))},
			{T: 1.4, Pose: meteorUp},
			{T: meteorBolt - 0.02, Pose: meteorUp.With(pixelfx.Y.Is(-1))},
			{T: meteorBolt, Pose: meteorPoint, How: "snap"},
			{T: meteorHit - 0.02, Pose: meteorPoint.With(pixelfx.Crouch.Is(3))},
			{T: meteorHit, Pose: meteorFlinch, How: "snap"},
			{T: meteorHit + 0.9, Pose: meteorFlinch.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(-2))},
			{T: meteorHit + 1.7, Pose: pixelfx.Ready.With(pixelfx.X.Is(0))},
			{T: meteorHit + 4.3, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: meteorBolt, Stop: 0.06, Shake: 0.16, Amp: pixelfx.Val(2), NoTarget: true},
			{T: meteorHit, Stop: 0.22, Shake: 0.55, Amp: pixelfx.Val(4), Push: pixelfx.Val(3.8), Straw: pixelfx.Val(44), Skin: "char"},
			{T: meteorHit + 3.9, Push: pixelfx.Val(0), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
	},
	New: pixelfx.Chain([]pixelfx.Cue{
		{FX: arcane.MagicCircleCast, T: 0},
		{FX: fire.Meteor, T: meteorHit - 0.62, X: meteorTX},
	}),
}

func init() { pixelfx.Register("moves/meteor-call", MeteorCall) }
