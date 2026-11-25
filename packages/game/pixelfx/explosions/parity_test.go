package explosions_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx/explosions"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

func TestSmallExplosionMatchesPixelfx(t *testing.T) {
	pixelfxtest.CheckEffect(t, explosions.SmallExplosion, filepath.Join("testdata", "ref", "small-explosion"))
}

func TestSmallExplosionIsDeterministic(t *testing.T) {
	pixelfxtest.CheckDeterministic(t, explosions.SmallExplosion)
}

func BenchmarkSmallExplosion(b *testing.B) { pixelfxtest.BenchmarkFrames(b, explosions.SmallExplosion) }
