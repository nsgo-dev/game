package water_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/pkg/pixelfx/water"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"bubbles", water.Bubbles},
	{"drip", water.Drip},
	{"fountain", water.Fountain},
	{"geyser", water.Geyser},
	{"rain-splash", water.RainSplash},
	{"ripples", water.Ripples},
	{"splash", water.Splash},
	{"water-jet", water.WaterJet},
	{"water-orb", water.WaterOrb},
	{"waterfall", water.Waterfall},
	{"wave-crash", water.WaveCrash},
	{"whirlpool", water.Whirlpool},
}

func TestWaterEffectsMatchPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestWaterEffectsAreDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkWater(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) {
			pixelfxtest.BenchmarkFrames(b, tc.effect)
		})
	}
}
