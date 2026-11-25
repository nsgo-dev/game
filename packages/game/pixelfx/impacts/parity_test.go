package impacts_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx/impacts"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

func TestGroundSlamMatchesPixelfx(t *testing.T) {
	pixelfxtest.CheckEffect(t, impacts.GroundSlam, filepath.Join("testdata", "ref", "ground-slam"))
}

func TestGroundSlamIsDeterministic(t *testing.T) {
	pixelfxtest.CheckDeterministic(t, impacts.GroundSlam)
}

func BenchmarkGroundSlam(b *testing.B) { pixelfxtest.BenchmarkFrames(b, impacts.GroundSlam) }
