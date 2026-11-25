// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/arrow-shot.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/impacts"
	"github.com/nsgo-dev/game/packages/game/pixelfx/projectiles"
)

// When the arrow is loosed and how long it flies, in effect time; variables so sums with them round as pixelfx's do.
var (
	arrowLoose = 0.56
	arrowFly   = 0.24
	arrowHit   = arrowLoose + arrowFly
	arrowTX    = 180.0
	arrowSpeed = (arrowTX - 30) / arrowFly
)

var (
	arrowRest   = pixelfx.Ready.Hold("bow").With(pixelfx.SA.Is(-0.4), pixelfx.FX.Is(12), pixelfx.FY.Is(-20), pixelfx.BX.Is(6), pixelfx.BY.Is(-17))
	arrowAim    = arrowRest.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(1), pixelfx.FX.Is(18), pixelfx.FY.Is(-28), pixelfx.SA.Is(0), pixelfx.BX.Is(10), pixelfx.BY.Is(-27), pixelfx.Nocked.On(), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-9))
	arrowDrawn  = arrowAim.With(pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-2), pixelfx.FX.Is(18), pixelfx.BX.Is(-3), pixelfx.BY.Is(-29), pixelfx.Pull.On())
	arrowLoosed = arrowAim.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(2), pixelfx.FX.Is(19), pixelfx.FY.Is(-28), pixelfx.BX.Is(-9), pixelfx.BY.Is(-32), pixelfx.Nocked.Is(0), pixelfx.Pull.Is(0))
)

// ArrowShot is a move: he raises the bow, draws to his cheek, holds his breath and looses; the string snaps forward, the arrow crosses the floor and thuds home. It chains projectiles.Arrow and impacts.HitSpark, and is registered as "moves/arrow-shot".
var ArrowShot = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Arrow shot",
		Desc:     "He raises the bow, draws to his cheek, holds his breath and looses: the string snaps forward, the arrow crosses the floor and thuds home.",
		Tags:     []string{"move", "bow", "arrow", "projectile", "ranged", "one-shot"},
		Size:     [2]int{300, 74},
		Origin:   [2]int{38, 66},
		Duration: 1.9,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{arrowTX},
		Poses: []pixelfx.Key{
			{T: -0.44, Pose: arrowRest},
			{T: -0.14, Pose: arrowAim},
			{T: 0.16, Pose: arrowDrawn},
			{T: arrowLoose, Pose: arrowDrawn.With(pixelfx.Crouch.Is(4))},
			{T: arrowLoose + 0.002, Pose: arrowLoosed, How: "snap"},
			{T: 1.1, Pose: arrowLoosed.With(pixelfx.BX.Is(-6), pixelfx.BY.Is(-25))},
			{T: 1.6, Pose: arrowRest},
			{T: 1.9, Pose: arrowRest},
		},
		Hits: []pixelfx.Hit{
			{T: arrowHit, Stop: 0.07, Shake: 0.12, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.2), Straw: pixelfx.Val(14), Y: pixelfx.Val(-28)},
		},
	},
	New: pixelfx.Chain([]pixelfx.Cue{
		{FX: projectiles.Arrow, T: arrowLoose, Until: arrowHit, XFn: func(t float64) float64 { return 30 + (t-arrowLoose)*arrowSpeed }, Y: -28},
		{FX: impacts.HitSpark, T: arrowHit, X: arrowTX - 10, Options: map[string]pixelfx.Value{"height": pixelfx.Num(30)}},
	}),
}

func init() { pixelfx.Register("moves/arrow-shot", ArrowShot) }
