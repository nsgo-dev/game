// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/lightning-call.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/lightning"
)

// lightningBolt is when the bolt comes down, in effect time; a variable so sums with it round as pixelfx's do.
var lightningBolt = 0.76

const lightningTX = 130

var (
	lightningUp   = pixelfx.Ready.With(pixelfx.Crouch.Is(1), pixelfx.Lean.Is(-2), pixelfx.FX.Is(2), pixelfx.FY.Is(-44), pixelfx.BX.Is(8), pixelfx.BY.Is(-19), pixelfx.FFX.Is(7), pixelfx.FBX.Is(-5))
	lightningDown = pixelfx.Ready.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(5), pixelfx.FX.Is(18), pixelfx.FY.Is(-27), pixelfx.BX.Is(-7), pixelfx.BY.Is(-24), pixelfx.FFX.Is(12), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
)

// LightningCall is a move: an arm goes up, a sigil lights under the target, and on the downswing a thick bolt comes down on it; the target jolts through the aftershocks. It chains lightning.LightningStrike and is registered as "moves/lightning-call".
var LightningCall = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Lightning call",
		Desc:     "An arm goes up, a sigil lights under the target, and on the downswing a thick bolt comes down on it; the target jolts through the aftershocks.",
		Tags:     []string{"move", "lightning", "spell", "strike", "one-shot"},
		Size:     [2]int{290, 266},
		Origin:   [2]int{42, 231},
		Duration: 3,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{lightningTX},
		Poses: []pixelfx.Key{
			{T: 0, Pose: pixelfx.Ready},
			{T: 0.26, Pose: lightningUp},
			{T: lightningBolt - 0.04, Pose: lightningUp.With(pixelfx.Y.Is(-1), pixelfx.Crouch.Is(-1))},
			{T: lightningBolt, Pose: lightningDown, How: "snap"},
			{T: 1.4, Pose: lightningDown.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(4))},
			{T: 2, Pose: pixelfx.Ready},
			{T: 3, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: lightningBolt, Stop: 0.14, Shake: 0.32, Amp: pixelfx.Val(3), Push: pixelfx.Val(2.8), Straw: pixelfx.Val(24), Skin: "shock", Shiver: 1.2},
			{T: 1.04, Stop: 0.03, Push: pixelfx.Val(1.2), Straw: pixelfx.Val(8)},
			{T: 1.26, Stop: 0.03, Push: pixelfx.Val(-1), Straw: pixelfx.Val(6)},
			{T: 2.5, Push: pixelfx.Val(0), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
		Dust: []pixelfx.DustKick{{T: lightningBolt, X: 14, N: 6, Dir: 1}},
	},
	New: pixelfx.Chain([]pixelfx.Cue{{FX: lightning.LightningStrike, T: 0, X: lightningTX}}),
}

func init() { pixelfx.Register("moves/lightning-call", LightningCall) }
