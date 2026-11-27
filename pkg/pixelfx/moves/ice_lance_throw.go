// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/ice-lance-throw.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/arcane"
	"github.com/nsgo-dev/game/pkg/pixelfx/ice"
)

// When the ice lance is thrown and how long it flies, in effect time; variables so sums with them round as pixelfx's do.
var (
	iceThrow = 0.8
	iceFly   = 0.34
	iceHit   = iceThrow + iceFly
	iceTX    = 170.0
	iceSpeed = (iceTX - 34) / iceFly
)

var (
	iceHold   = pixelfx.Ready.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(2), pixelfx.FX.Is(18), pixelfx.FY.Is(-22), pixelfx.BX.Is(11), pixelfx.BY.Is(-17), pixelfx.FFX.Is(10), pixelfx.FBX.Is(-8))
	iceThrown = pixelfx.Ready.With(pixelfx.X.Is(9), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(6), pixelfx.FX.Is(19), pixelfx.FY.Is(-24), pixelfx.BX.Is(-6), pixelfx.BY.Is(-24), pixelfx.FFX.Is(13), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
)

// IceLanceThrow is a move: frost gathers into a spear in his hand, he throws it like a javelin, and the target ices over in a burst of snowflakes and shivers. It chains arcane.SpellCharge, ice.IceLance and ice.SnowflakeBurst, and is registered as "moves/ice-lance-throw".
var IceLanceThrow = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ice lance throw",
		Desc:     "Frost gathers into a spear in his hand, he throws it like a javelin, and the target ices over in a burst of snowflakes and shivers.",
		Tags:     []string{"move", "ice", "spell", "projectile", "one-shot"},
		Size:     [2]int{310, 136},
		Origin:   [2]int{86, 134},
		Duration: iceHit + 3.2,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{iceTX},
		Poses: []pixelfx.Key{
			{T: -0.4, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
			{T: 0.06, Pose: iceHold},
			{T: iceThrow - 0.2, Pose: iceHold},
			{T: iceThrow - 0.03, Pose: iceHold.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-2), pixelfx.FX.Is(17), pixelfx.BX.Is(5), pixelfx.BY.Is(-20))},
			{T: iceThrow, Pose: iceThrown, How: "snap"},
			{T: iceHit + 0.3, Pose: iceThrown.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(4))},
			{T: iceHit + 0.9, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
			{T: iceHit + 3.2, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
		},
		Hits: []pixelfx.Hit{
			{T: iceHit, Stop: 0.13, Shake: 0.2, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.4), Straw: pixelfx.Val(12), Skin: "ice", Shiver: 1.4, Y: pixelfx.Val(-26)},
			{T: 2.75, Push: pixelfx.Val(0.3), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
		Dust: []pixelfx.DustKick{{T: iceThrow, X: 13, N: 6, Dir: 1, C: pixelfx.Hex("#d8f2ff")}},
	},
	New: pixelfx.Chain([]pixelfx.Cue{
		{FX: arcane.SpellCharge, T: 0.06, Until: iceThrow, Options: map[string]pixelfx.Value{"palette": pixelfx.RampOf(pixelfx.Ramps["ice"])}},
		{FX: ice.IceLance, T: iceThrow, Until: iceHit, XFn: func(t float64) float64 { return 34 + (t-iceThrow)*iceSpeed }, Y: -24},
		{FX: ice.SnowflakeBurst, T: iceHit, X: iceTX, Y: -26},
	}),
}

func init() { pixelfx.Register("moves/ice-lance-throw", IceLanceThrow) }
