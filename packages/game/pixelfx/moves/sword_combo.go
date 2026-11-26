// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/moves/sword-combo.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package moves

import (
	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/melee"
)

// When each stroke begins and when the finisher lands, in effect time; variables so sums with them round as pixelfx's do.
var (
	comboA      = 0.0
	comboB      = 0.5
	comboC      = 1.04
	comboFinish = comboC + 0.335
)

const comboStep = 10

var comboStand = pixelfx.Ready.Hold("sword").With(pixelfx.FX.Is(11), pixelfx.FY.Is(-15), pixelfx.SA.Is(0.38))

// SwordCombo is a move: three strokes walking forward; a slash, a crossing double cut, and a two-handed cleave into the floor that throws the target about. It chains melee.SwordSlash, melee.DoubleSlash and melee.HeavyCleave, and is registered as "moves/sword-combo".
var SwordCombo = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Sword combo",
		Desc:     "Three strokes walking forward: a slash, a crossing double cut, and a two-handed cleave into the floor that throws the target about.",
		Tags:     []string{"move", "melee", "sword", "combo", "one-shot"},
		Size:     [2]int{264, 150},
		Origin:   [2]int{72, 130},
		Duration: comboFinish + 1.6,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options:  []pixelfx.Option{{Name: "seed", Default: pixelfx.Num(1)}},
	},
	Act: &pixelfx.Act{
		Targets: []float64{118},
		Poses:   comboPoses(),
		Hits:    comboHits(),
		Dust:    comboDust(),
	},
	New: pixelfx.Chain([]pixelfx.Cue{
		{FX: melee.SwordSlash, T: comboA},
		{FX: melee.DoubleSlash, T: comboB, X: comboStep},
		{FX: melee.HeavyCleave, T: comboC, X: comboStep * 2},
	}),
}

func comboFilterKeys(keys []pixelfx.Key, ok func(float64) bool) []pixelfx.Key {
	var out []pixelfx.Key
	for _, k := range keys {
		if ok(k.T) {
			out = append(out, k)
		}
	}
	return out
}

func comboPoses() []pixelfx.Key {
	var poses []pixelfx.Key
	poses = append(poses, pixelfx.ShiftKeys(comboFilterKeys(melee.SwordSlash.Act.Poses, func(t float64) bool { return t <= 0.3 }), comboA, 0, pixelfx.Pose{})...)
	poses = append(poses, pixelfx.ShiftKeys(comboFilterKeys(melee.DoubleSlash.Act.Poses, func(t float64) bool { return t <= 0.27 && t >= -0.14 }), comboB, comboStep, pixelfx.Pose{})...)
	poses = append(poses, pixelfx.ShiftKeys(comboFilterKeys(melee.HeavyCleave.Act.Poses, func(t float64) bool { return t >= -0.3 }), comboC, comboStep*2, pixelfx.Pose{}.Hold("sword"))...)
	return append(poses, pixelfx.Key{T: comboFinish + 1.6, Pose: comboStand.With(pixelfx.X.Is(comboStep*2 - 4))})
}

func comboHits() []pixelfx.Hit {
	var hits []pixelfx.Hit
	hits = append(hits, pixelfx.ShiftHits(melee.SwordSlash.Act.Hits, comboA)...)
	hits = append(hits, pixelfx.ShiftHits(melee.DoubleSlash.Act.Hits, comboB)...)
	hits = append(hits, pixelfx.ShiftHits(melee.HeavyCleave.Act.Hits, comboC)...)
	return hits
}

func comboDust() []pixelfx.DustKick {
	out := make([]pixelfx.DustKick, 0, len(melee.SwordSlash.Act.Dust)+len(melee.HeavyCleave.Act.Dust))
	for _, d := range melee.SwordSlash.Act.Dust {
		d.T += comboA
		out = append(out, d)
	}
	for _, d := range melee.HeavyCleave.Act.Dust {
		d.T += comboC
		d.X += comboStep * 2
		out = append(out, d)
	}
	return out
}

func init() { pixelfx.Register("moves/sword-combo", SwordCombo) }
