package melee_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/melee"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"claw-swipe", melee.ClawSwipe},
	{"dagger-flurry", melee.DaggerFlurry},
	{"double-slash", melee.DoubleSlash},
	{"hammer-smash", melee.HammerSmash},
	{"heavy-cleave", melee.HeavyCleave},
	{"scythe-reap", melee.ScytheReap},
	{"spear-sweep", melee.SpearSweep},
	{"spin-slash", melee.SpinSlash},
	{"sword-slash", melee.SwordSlash},
	{"thrust", melee.Thrust},
	{"uppercut", melee.Uppercut},
	{"whip-crack", melee.WhipCrack},
}

func TestMeleeMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestMeleeIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func TestMeleeRegistered(t *testing.T) {
	for _, tc := range allEffects {
		if e, ok := pixelfx.Lookup("melee/" + tc.name); !ok || e != tc.effect {
			t.Errorf("melee/%s is not registered as its effect", tc.name)
		}
	}
}

func BenchmarkSwordSlash(b *testing.B) { pixelfxtest.BenchmarkFrames(b, melee.SwordSlash) }

func BenchmarkSwordSlashActed(b *testing.B) { pixelfxtest.BenchmarkActFrames(b, melee.SwordSlash) }

func BenchmarkHammerSmash(b *testing.B) { pixelfxtest.BenchmarkFrames(b, melee.HammerSmash) }

func BenchmarkScytheReap(b *testing.B) { pixelfxtest.BenchmarkFrames(b, melee.ScytheReap) }
