// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/rocket-launcher.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/explosions"
	"github.com/nsgo-dev/game/packages/game/pixelfx/projectiles"
)

// When the rocket is fired and how long it flies, in effect time; variables so sums with them round as pixelfx's do.
var (
	rocketFire = 0.6
	rocketFly  = 0.56
	rocketHit  = rocketFire + rocketFly
)

const (
	rocketTX = 200
	rocketMY = -21
	rocketMX = 34
)

var (
	rocketRest   = pixelfx.Ready.Hold("launcher").With(pixelfx.Two.On(), pixelfx.SA.Is(-1), pixelfx.FX.Is(9), pixelfx.FY.Is(-19))
	rocketKneel  = pixelfx.Ready.Hold("launcher").With(pixelfx.Two.On(), pixelfx.SA.Is(0), pixelfx.FX.Is(9), pixelfx.FY.Is(-17), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(2), pixelfx.FFX.Is(12), pixelfx.FBX.Is(-9))
	rocketKicked = rocketKneel.With(pixelfx.X.Is(-6), pixelfx.SA.Is(-0.12), pixelfx.FX.Is(4), pixelfx.FY.Is(-19), pixelfx.Lean.Is(-4), pixelfx.Crouch.Is(5))
)

// RocketLauncher is a move: he drops to one knee, shoulders the tube and fires; the kick shoves him back along the floor, the rocket crawls out on its jet and the target goes up. It chains projectiles.MuzzleShotgun, projectiles.Rocket and explosions.SmallExplosion, and is registered as "moves/rocket-launcher".
var RocketLauncher = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Rocket launcher",
		Desc:     "He drops to one knee, shoulders the tube and fires: the kick shoves him back along the floor, the rocket crawls out on its jet and the target goes up.",
		Tags:     []string{"move", "gun", "rocket", "projectile", "explosion", "one-shot"},
		Size:     [2]int{480, 244},
		Origin:   [2]int{178, 198},
		Duration: rocketHit + 5.4,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{rocketTX},
		Poses: []pixelfx.Key{
			{T: -0.52, Pose: rocketRest},
			{T: -0.18, Pose: rocketKneel},
			{T: rocketFire - 0.001, Pose: rocketKneel.With(pixelfx.Crouch.Is(7))},
			{T: rocketFire, Pose: rocketKicked, How: "snap"},
			{T: rocketFire + 0.36, Pose: rocketKneel.With(pixelfx.X.Is(-6))},
			{T: rocketHit + 0.9, Pose: rocketKneel.With(pixelfx.X.Is(-6), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(1))},
			{T: rocketHit + 1.5, Pose: rocketRest.With(pixelfx.X.Is(-3))},
			{T: rocketHit + 2.2, Pose: rocketRest.With(pixelfx.X.Is(0))},
			{T: rocketHit + 5.4, Pose: rocketRest},
		},
		Hits: []pixelfx.Hit{
			{T: rocketFire, Stop: 0.06, Shake: 0.22, Amp: pixelfx.Val(2), NoTarget: true},
			{T: rocketHit, Stop: 0.2, Shake: 0.45, Amp: pixelfx.Val(4), Push: pixelfx.Val(3.8), Straw: pixelfx.Val(44), Skin: "char"},
			{T: rocketHit + 4.8, Push: pixelfx.Val(0), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
		Dust: []pixelfx.DustKick{{T: rocketFire, X: -16, N: 10, Dir: -1}},
	},
	New: pixelfx.Chain([]pixelfx.Cue{
		{FX: projectiles.MuzzleShotgun, T: rocketFire, X: rocketMX, Options: map[string]pixelfx.Value{"height": pixelfx.Num(-rocketMY)}},
		{FX: projectiles.Rocket, T: rocketFire, Until: rocketHit, XFn: rocketX, Y: rocketMY},
		{FX: explosions.SmallExplosion, T: rocketHit, X: rocketTX},
	}),
}

// rocketX is where the rocket is at the move's time t: it crawls out of the tube and speeds up as its jet takes hold.
func rocketX(t float64) float64 {
	return rocketMX + 10 + math.Pow(t-rocketFire, 1.5)*((rocketTX-rocketMX-16)/math.Pow(rocketFly, 1.5))
}

func init() { pixelfx.Register("moves/rocket-launcher", RocketLauncher) }
