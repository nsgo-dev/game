package holy_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/holy"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"blessing", holy.Blessing},
	{"divine-shield", holy.DivineShield},
	{"feather-burst", holy.FeatherBurst},
	{"halo", holy.Halo},
	{"heal", holy.Heal},
	{"heal-aoe", holy.HealAoe},
	{"holy-pillar", holy.HolyPillar},
	{"resurrection", holy.Resurrection},
	{"smite", holy.Smite},
	{"sunburst", holy.Sunburst},
}

func TestHolyMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestHolyIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkHolyPillar(b *testing.B) { pixelfxtest.BenchmarkFrames(b, holy.HolyPillar) }

func BenchmarkResurrection(b *testing.B) { pixelfxtest.BenchmarkFrames(b, holy.Resurrection) }

func BenchmarkResurrectionAct(b *testing.B) { pixelfxtest.BenchmarkActFrames(b, holy.Resurrection) }
