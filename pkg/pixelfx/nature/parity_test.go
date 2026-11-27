package nature_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/pkg/pixelfx/nature"
)

var allEffects = []struct {
	name string
	fx   *pixelfx.Effect
}{
	{"acid-splash", nature.AcidSplash},
	{"boulder", nature.Boulder},
	{"earth-spike", nature.EarthSpike},
	{"healing-bloom", nature.HealingBloom},
	{"leaf-storm", nature.LeafStorm},
	{"poison-cloud", nature.PoisonCloud},
	{"quake", nature.Quake},
	{"root-snare", nature.RootSnare},
	{"spore-burst", nature.SporeBurst},
	{"thorns", nature.Thorns},
	{"tornado", nature.Tornado},
	{"toxic-puddle", nature.ToxicPuddle},
	{"vine-whip", nature.VineWhip},
}

func TestNatureMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.fx)
		})
	}
}

func TestNatureIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.fx)
		})
	}
}

func BenchmarkNature(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) {
			pixelfxtest.BenchmarkFrames(b, tc.fx)
		})
	}
}
