// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/pistol-shots.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/impacts"
	"github.com/nsgo-dev/game/pkg/pixelfx/projectiles"
)

// When each shot is fired and how long its bullet flies, in effect time; variables so sums with them round as pixelfx's do.
var (
	pistolShots = []float64{0.14, 0.42, 0.7}
	pistolFly   = 0.12
)

const (
	pistolTX = 180
	pistolBY = -32
	pistolMX = 28
)

var (
	pistolRest = pixelfx.Ready.Hold("pistol").With(pixelfx.SA.Is(0.9), pixelfx.FX.Is(10), pixelfx.FY.Is(-17), pixelfx.BX.Is(7), pixelfx.BY.Is(-16))
	pistolAim  = pistolRest.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(2), pixelfx.FX.Is(18), pixelfx.FY.Is(-28), pixelfx.SA.Is(0), pixelfx.BX.Is(12), pixelfx.BY.Is(-25), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-9))
	pistolKick = pistolAim.With(pixelfx.Lean.Is(0), pixelfx.FX.Is(16), pixelfx.FY.Is(-33), pixelfx.SA.Is(-0.45), pixelfx.BY.Is(-28))
)

// PistolShots is a move: three aimed shots; the gun kicks up each time and settles, tracers cross the floor, and the target staggers under each hit. It chains projectiles.MuzzlePistol, projectiles.BulletTracer and impacts.HitSpark, and is registered as "moves/pistol-shots".
var PistolShots = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Pistol shots",
		Desc:     "Three aimed shots: the gun kicks up each time and settles, tracers cross the floor, and the target staggers under each hit.",
		Tags:     []string{"move", "gun", "pistol", "projectile", "ranged", "one-shot"},
		Size:     [2]int{378, 76},
		Origin:   [2]int{111, 72},
		Duration: 2.6,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{pistolTX},
		Poses:   pistolPoses(),
		Hits:    pistolHits(),
	},
	New: pixelfx.Chain(pistolCues()),
}

// pistolPoses is the act's track: aim, then for each shot the kick and the settle.
func pistolPoses() []pixelfx.Key {
	keys := []pixelfx.Key{{T: -0.36, Pose: pistolRest}, {T: -0.06, Pose: pistolAim}}
	for _, t := range pistolShots {
		keys = append(keys, pixelfx.Key{T: t, Pose: pistolAim}, pixelfx.Key{T: t + 0.001, Pose: pistolKick, How: "snap"}, pixelfx.Key{T: t + 0.14, Pose: pistolAim})
	}
	return append(keys, pixelfx.Key{T: 1.2, Pose: pistolAim.With(pixelfx.Crouch.Is(1))}, pixelfx.Key{T: 1.7, Pose: pistolRest}, pixelfx.Key{T: 2.6, Pose: pistolRest})
}

// pistolHits is a hit per shot as its bullet lands, the last one harder, swaying the target one way and back.
func pistolHits() []pixelfx.Hit {
	hits := make([]pixelfx.Hit, len(pistolShots))
	for i, t := range pistolShots {
		stop, amp, push := 0.04, 1.0, 2.0
		if i == 2 {
			stop, amp = 0.08, 2
		}
		if i%2 == 1 {
			push = -1.8
		}
		hits[i] = pixelfx.Hit{T: t + pistolFly, Stop: stop, Shake: 0.1, Amp: pixelfx.Val(amp), Push: pixelfx.Val(push), Straw: pixelfx.Val(10), Y: pixelfx.Val(-30)}
	}
	return hits
}

// pistolCues is, for each shot, the muzzle flash, the tracer flying to the target and the spark where it lands.
func pistolCues() []pixelfx.Cue {
	cues := make([]pixelfx.Cue, 0, 3*len(pistolShots))
	for _, t := range pistolShots {
		cues = append(cues,
			pixelfx.Cue{FX: projectiles.MuzzlePistol, T: t, X: pistolMX, Options: map[string]pixelfx.Value{"height": pixelfx.Num(-pistolBY)}},
			pixelfx.Cue{FX: projectiles.BulletTracer, T: t, Until: t + pistolFly, XFn: func(u float64) float64 { return pistolMX + 4 + (u-t)*((pistolTX-pistolMX-14)/pistolFly) }, Y: pistolBY},
			pixelfx.Cue{FX: impacts.HitSpark, T: t + pistolFly, X: pistolTX - 10, Options: map[string]pixelfx.Value{"height": pixelfx.Num(28)}},
		)
	}
	return cues
}

func init() { pixelfx.Register("moves/pistol-shots", PistolShots) }
