package pickups_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/pkg/pixelfx/pickups"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"chest-open", pickups.ChestOpen},
	{"coin-spin", pickups.CoinSpin},
	{"collect-burst", pickups.CollectBurst},
	{"crystal-shard", pickups.CrystalShard},
	{"gem-spin", pickups.GemSpin},
	{"heart-pickup", pickups.HeartPickup},
	{"key-glint", pickups.KeyGlint},
	{"loot-beam", pickups.LootBeam},
	{"potion", pickups.Potion},
	{"power-star", pickups.PowerStar},
	{"xp-orb", pickups.XPOrb},
}

func TestPickupsMatchPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestPickupsAreDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkPickups(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) {
			pixelfxtest.BenchmarkFrames(b, tc.effect)
		})
	}
}
