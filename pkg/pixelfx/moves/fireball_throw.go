// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/fireball-throw.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/arcane"
	"github.com/nsgo-dev/game/pkg/pixelfx/explosions"
	"github.com/nsgo-dev/game/pkg/pixelfx/fire"
)

// When the fireball is thrown and how long it flies, in effect time; variables so sums with them round as pixelfx's do.
var (
	fireballThrow = 0.95
	fireballFly   = 0.46
	fireballHit   = fireballThrow + fireballFly
)

const (
	fireballSpeed = 340
	fireballTX    = 190
)

var (
	fireballHold   = pixelfx.Ready.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(2), pixelfx.FX.Is(18), pixelfx.FY.Is(-22), pixelfx.BX.Is(11), pixelfx.BY.Is(-17), pixelfx.FFX.Is(10), pixelfx.FBX.Is(-8))
	fireballThrown = pixelfx.Ready.With(pixelfx.X.Is(9), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(6), pixelfx.FX.Is(19), pixelfx.FY.Is(-22), pixelfx.BX.Is(-6), pixelfx.BY.Is(-24), pixelfx.FFX.Is(13), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
	fireballFlinch = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.X.Is(7), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-3), pixelfx.FX.Is(-4), pixelfx.FY.Is(-36), pixelfx.BX.Is(5), pixelfx.BY.Is(-33), pixelfx.FFX.Is(4), pixelfx.FBX.Is(-4))
)

// FireballThrow is a move: he gathers fire in his hand, leans back and hurls it; the ball flies, the target takes it in a blast and comes out charred. It chains arcane.SpellCharge, fire.Fireball and explosions.SmallExplosion, and is registered as "moves/fireball-throw".
var FireballThrow = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Fireball throw",
		Desc:     "He gathers fire in his hand, leans back and hurls it: the ball flies, the target takes it in a blast and comes out charred.",
		Tags:     []string{"move", "fire", "spell", "projectile", "explosion", "one-shot"},
		Size:     [2]int{356, 244},
		Origin:   [2]int{64, 198},
		Duration: fireballHit + 5.4,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{fireballTX},
		Poses: []pixelfx.Key{
			{T: -0.42, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
			{T: 0.08, Pose: fireballHold},
			{T: 0.5, Pose: fireballHold},
			{T: fireballThrow - 0.03, Pose: fireballHold.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-2), pixelfx.FX.Is(17), pixelfx.BX.Is(5), pixelfx.BY.Is(-20))},
			{T: fireballThrow, Pose: fireballThrown, How: "snap"},
			{T: fireballHit - 0.02, Pose: fireballThrown.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(4))},
			{T: fireballHit, Pose: fireballFlinch, How: "snap"},
			{T: fireballHit + 0.7, Pose: fireballFlinch.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(-2))},
			{T: fireballHit + 1.4, Pose: pixelfx.Ready.With(pixelfx.X.Is(4))},
			{T: fireballHit + 2.1, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
			{T: fireballHit + 5.4, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
		},
		Hits: []pixelfx.Hit{
			{T: fireballHit, Stop: 0.18, Shake: 0.38, Amp: pixelfx.Val(3), Push: pixelfx.Val(3.4), Straw: pixelfx.Val(34), Skin: "char"},
			{T: fireballHit + 4.8, Push: pixelfx.Val(0), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
		Dust: []pixelfx.DustKick{{T: fireballThrow, X: 13, N: 7, Dir: 1}},
	},
	New: pixelfx.Chain([]pixelfx.Cue{
		{FX: arcane.SpellCharge, T: 0.08, Until: fireballThrow, Options: map[string]pixelfx.Value{"palette": pixelfx.RampOf(pixelfx.Ramps["fire"])}},
		{FX: fire.Fireball, T: fireballThrow, Until: fireballHit, XFn: func(t float64) float64 { return 28 + (t-fireballThrow)*fireballSpeed }, Y: -22},
		{FX: explosions.SmallExplosion, T: fireballHit, X: fireballTX},
	}),
}

func init() { pixelfx.Register("moves/fireball-throw", FireballThrow) }
