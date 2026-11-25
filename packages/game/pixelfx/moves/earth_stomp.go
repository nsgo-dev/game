// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/earth-stomp.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/nature"
)

// When the boot lands and when the spike rams the target, in effect time; variables so sums with them round as pixelfx's do.
var (
	stompT   = 0.06
	stompRam = stompT + 0.43
)

const stompTarget = 112

var (
	stompKnee = pixelfx.Ready.With(pixelfx.Crouch.Is(1), pixelfx.Lean.Is(-3), pixelfx.FX.Is(3), pixelfx.FY.Is(-34), pixelfx.BX.Is(11), pixelfx.BY.Is(-30), pixelfx.FFX.Is(9), pixelfx.FFY.Is(-15), pixelfx.FBX.Is(-5))
	stompSlam = pixelfx.Ready.With(pixelfx.Crouch.Is(7), pixelfx.Lean.Is(3), pixelfx.FX.Is(14), pixelfx.FY.Is(-14), pixelfx.BX.Is(-5), pixelfx.BY.Is(-18), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-9))
)

// EarthStomp is a move: a knee comes up, the boot slams down, the floor heaves under the target and a stone spike rams up through it. It chains nature.EarthSpike and is registered as "moves/earth-stomp".
var EarthStomp = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Earth stomp",
		Desc:     "A knee comes up, the boot slams down, the floor heaves under the target and a stone spike rams up through it.",
		Tags:     []string{"move", "earth", "spell", "stomp", "one-shot"},
		Size:     [2]int{290, 142},
		Origin:   [2]int{50, 128},
		Duration: stompT + 3.4,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{stompTarget},
		Poses: []pixelfx.Key{
			{T: -0.5, Pose: pixelfx.Ready},
			{T: -0.14, Pose: stompKnee},
			{T: stompT - 0.005, Pose: stompKnee.With(pixelfx.FFY.Is(-17))},
			{T: stompT, Pose: stompSlam, How: "snap"},
			{T: stompRam + 0.5, Pose: stompSlam.With(pixelfx.Crouch.Is(5))},
			{T: stompRam + 1.1, Pose: pixelfx.Ready},
			{T: stompT + 3.4, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: stompT, Stop: 0.1, Shake: 0.2, Amp: pixelfx.Val(3), NoTarget: true},
			{T: stompRam, Stop: 0.14, Shake: 0.28, Amp: pixelfx.Val(3), Push: pixelfx.Val(-3.2), Straw: pixelfx.Val(30), Y: pixelfx.Val(-10)},
		},
		Dust: []pixelfx.DustKick{{T: stompT, X: 11, N: 9}},
	},
	New: pixelfx.Chain([]pixelfx.Cue{{FX: nature.EarthSpike, T: stompT, X: stompTarget}}),
}

func init() { pixelfx.Register("moves/earth-stomp", EarthStomp) }
