package fire_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/fire"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"brazier", fire.Brazier},
	{"campfire", fire.Campfire},
	{"candle", fire.Candle},
	{"fire-breath", fire.FireBreath},
	{"fire-ring", fire.FireRing},
	{"fire-tornado", fire.FireTornado},
	{"fireball", fire.Fireball},
	{"flame-pillar", fire.FlamePillar},
	{"flamethrower", fire.Flamethrower},
	{"meteor", fire.Meteor},
	{"spirit-flame", fire.SpiritFlame},
	{"torch", fire.Torch},
}

func TestFireMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestFireIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkTorch(b *testing.B) { pixelfxtest.BenchmarkFrames(b, fire.Torch) }

func BenchmarkCampfire(b *testing.B) { pixelfxtest.BenchmarkFrames(b, fire.Campfire) }

func BenchmarkMeteor(b *testing.B) { pixelfxtest.BenchmarkFrames(b, fire.Meteor) }
