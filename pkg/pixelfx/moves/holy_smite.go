// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/holy-smite.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/holy"
)

// smiteBolt is when the bolt drops, in effect time; a variable so sums with it round as pixelfx's do.
var smiteBolt = 0.47

const smiteTX = 118

var (
	smiteUp   = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(-1), pixelfx.FX.Is(-7), pixelfx.FY.Is(-44), pixelfx.BX.Is(8), pixelfx.BY.Is(-44), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	smiteDown = pixelfx.Ready.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(5), pixelfx.FX.Is(17), pixelfx.FY.Is(-29), pixelfx.BX.Is(13), pixelfx.BY.Is(-25), pixelfx.FFX.Is(12), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
)

// HolySmite is a move: both hands go up and a glint marks the target; he brings them down and a bolt of light drops like a hammer, leaving the target blanched. It chains holy.Smite and is registered as "moves/holy-smite".
var HolySmite = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Holy smite",
		Desc:     "Both hands go up and a glint marks the target; he brings them down and a bolt of light drops like a hammer, leaving the target blanched.",
		Tags:     []string{"move", "holy", "spell", "strike", "one-shot"},
		Size:     [2]int{252, 182},
		Origin:   [2]int{42, 151},
		Duration: 2.8,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{smiteTX},
		Poses: []pixelfx.Key{
			{T: -0.42, Pose: pixelfx.Ready},
			{T: -0.04, Pose: smiteUp},
			{T: smiteBolt - 0.01, Pose: smiteUp.With(pixelfx.Y.Is(-1))},
			{T: smiteBolt, Pose: smiteDown, How: "snap"},
			{T: 1.1, Pose: smiteDown.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(3))},
			{T: 1.7, Pose: pixelfx.Ready},
			{T: 2.8, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: smiteBolt, Stop: 0.14, Shake: 0.32, Amp: pixelfx.Val(3), Push: pixelfx.Val(2.8), Straw: pixelfx.Val(24), Skin: "holy", Y: pixelfx.Val(-30)},
			{T: 2.3, Push: pixelfx.Val(0), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
		Dust: []pixelfx.DustKick{{T: smiteBolt, X: 14, N: 6, Dir: 1}},
	},
	New: pixelfx.Chain([]pixelfx.Cue{{FX: holy.Smite, T: 0, X: smiteTX}}),
}

func init() { pixelfx.Register("moves/holy-smite", HolySmite) }
