// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/ground-pound.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/impacts"
)

// land is when he lands, in effect time; a variable so sums with it round as pixelfx's do.
var land = 0.62

const poundTarget = 80

var (
	poundCoil = pixelfx.Ready.With(pixelfx.Crouch.Is(8), pixelfx.Lean.Is(4), pixelfx.FX.Is(-6), pixelfx.FY.Is(-14), pixelfx.BX.Is(3), pixelfx.BY.Is(-14), pixelfx.FFX.Is(8), pixelfx.FBX.Is(-8))
	poundAir  = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.8), pixelfx.Crouch.Is(-1), pixelfx.FX.Is(-9), pixelfx.FY.Is(-44), pixelfx.BX.Is(10), pixelfx.BY.Is(-44), pixelfx.FFX.Is(6), pixelfx.FFY.Is(-8), pixelfx.FBX.Is(-3), pixelfx.FBY.Is(-3), pixelfx.ToeB.On())
	poundTop  = poundAir.With(pixelfx.Y.Is(-62), pixelfx.FX.Is(-10), pixelfx.FY.Is(-20), pixelfx.BX.Is(11), pixelfx.BY.Is(-20), pixelfx.FFX.Is(7), pixelfx.FFY.Is(-10), pixelfx.FBX.Is(-4), pixelfx.FBY.Is(-10))
	poundDown = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.8), pixelfx.Y.Is(-8), pixelfx.Crouch.Is(0), pixelfx.FX.Is(-5), pixelfx.FY.Is(-40), pixelfx.BX.Is(6), pixelfx.BY.Is(-40), pixelfx.FFX.Is(3), pixelfx.FFY.Is(-3), pixelfx.FBX.Is(-3), pixelfx.FBY.Is(-3), pixelfx.Ghost.Is(1))
	poundHero = pixelfx.Pose{}.With(pixelfx.Y.Is(0), pixelfx.Crouch.Is(8), pixelfx.Lean.Is(5), pixelfx.FX.Is(13), pixelfx.FY.Is(-8), pixelfx.BX.Is(-6), pixelfx.BY.Is(-23), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-10))
)

// GroundPound is a move: he coils, leaps out of frame height, hangs, and comes down like a rock; the floor cracks, shock waves race out and throw the target about as they pass. It chains impacts.GroundSlam and is registered as "moves/ground-pound".
var GroundPound = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ground pound",
		Desc:     "He coils, leaps out of frame height, hangs, and comes down like a rock: the floor cracks, shock waves race out and throw the target about as they pass.",
		Tags:     []string{"move", "impact", "jump", "landing", "shockwave", "one-shot"},
		Size:     [2]int{304, 204},
		Origin:   [2]int{152, 168},
		Duration: land + 3.2,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{poundTarget},
		Poses: []pixelfx.Key{
			{T: -0.5, Pose: pixelfx.Ready},
			{T: -0.16, Pose: poundCoil},
			{T: 0, Pose: poundCoil.With(pixelfx.Crouch.Is(7))},
			{T: 0.03, Pose: poundAir.With(pixelfx.Y.Is(-10), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.26, Pose: poundTop.With(pixelfx.Y.Is(-60)), How: "out"},
			{T: 0.42, Pose: poundTop},
			{T: land - 0.005, Pose: poundDown.With(pixelfx.Y.Is(-4)), How: "in"},
			{T: land, Pose: poundHero, How: "snap"},
			{T: land + 0.8, Pose: poundHero.With(pixelfx.Crouch.Is(6))},
			{T: land + 1.3, Pose: poundHero.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(3), pixelfx.FX.Is(13), pixelfx.FY.Is(-15), pixelfx.BX.Is(-5), pixelfx.BY.Is(-20))},
			{T: land + 1.8, Pose: pixelfx.Ready},
			{T: land + 3.2, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: land, Stop: 0.2, Shake: 0.5, Amp: pixelfx.Val(4), NoTarget: true},
			{T: land + 0.14, Stop: 0.04, Push: pixelfx.Val(3), Straw: pixelfx.Val(26), Y: pixelfx.Val(-8)},
			{T: land + 0.44, Push: pixelfx.Val(-1.6), Straw: pixelfx.Val(8), Y: pixelfx.Val(-8), Flash: pixelfx.Val(0)},
		},
		Dust: []pixelfx.DustKick{{T: 0.03, X: 0, N: 8}},
	},
	New: pixelfx.Chain([]pixelfx.Cue{{FX: impacts.GroundSlam, T: land}}),
}

func init() { pixelfx.Register("moves/ground-pound", GroundPound) }
