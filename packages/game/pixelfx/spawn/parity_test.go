package spawn_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/spawn"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"coin-drop", spawn.CoinDrop},
	{"disintegrate", spawn.Disintegrate},
	{"dissolve-burn", spawn.DissolveBurn},
	{"egg-hatch", spawn.EggHatch},
	{"poof", spawn.Poof},
	{"respawn-beam", spawn.RespawnBeam},
	{"shatter-death", spawn.ShatterDeath},
	{"soul-rise", spawn.SoulRise},
	{"spawn-portal", spawn.SpawnPortal},
	{"summon-rise", spawn.SummonRise},
}

func TestEffectsMatchPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestEffectsAreDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkSpawnEffects(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) {
			pixelfxtest.BenchmarkFrames(b, tc.effect)
		})
	}
}
