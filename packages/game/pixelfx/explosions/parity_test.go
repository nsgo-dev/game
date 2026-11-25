package explosions_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/explosions"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"barrel-explosion", explosions.BarrelExplosion},
	{"big-explosion", explosions.BigExplosion},
	{"cartoon-pow", explosions.CartoonPow},
	{"dirt-explosion", explosions.DirtExplosion},
	{"firework-burst", explosions.FireworkBurst},
	{"firework-rocket", explosions.FireworkRocket},
	{"grenade", explosions.Grenade},
	{"implosion", explosions.Implosion},
	{"nuke", explosions.Nuke},
	{"plasma-explosion", explosions.PlasmaExplosion},
	{"small-explosion", explosions.SmallExplosion},
	{"smoke-bomb", explosions.SmokeBomb},
}

func TestExplosionsMatchPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestExplosionsAreDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkSmallExplosion(b *testing.B) { pixelfxtest.BenchmarkFrames(b, explosions.SmallExplosion) }

func BenchmarkBigExplosion(b *testing.B) { pixelfxtest.BenchmarkFrames(b, explosions.BigExplosion) }

func BenchmarkNuke(b *testing.B) { pixelfxtest.BenchmarkFrames(b, explosions.Nuke) }
