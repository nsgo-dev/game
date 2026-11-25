package ice_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/ice"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"blizzard-vortex", ice.BlizzardVortex},
	{"freeze", ice.Freeze},
	{"frost-breath", ice.FrostBreath},
	{"frost-nova", ice.FrostNova},
	{"ice-crystal", ice.IceCrystal},
	{"ice-lance", ice.IceLance},
	{"ice-shatter", ice.IceShatter},
	{"ice-spikes", ice.IceSpikes},
	{"icicle-fall", ice.IcicleFall},
	{"snowflake-burst", ice.SnowflakeBurst},
}

func TestIceMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestIceIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func TestIceIsRegistered(t *testing.T) {
	for _, tc := range allEffects {
		if e, ok := pixelfx.Lookup("ice/" + tc.name); !ok || e != tc.effect {
			t.Errorf("ice/%s is not registered as its effect", tc.name)
		}
	}
}

func BenchmarkFreeze(b *testing.B) { pixelfxtest.BenchmarkFrames(b, ice.Freeze) }

func BenchmarkFrostBreath(b *testing.B) { pixelfxtest.BenchmarkFrames(b, ice.FrostBreath) }

func BenchmarkIceSpikes(b *testing.B) { pixelfxtest.BenchmarkFrames(b, ice.IceSpikes) }
