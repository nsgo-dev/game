package fire_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx/fire"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

func TestTorchMatchesPixelfx(t *testing.T) {
	pixelfxtest.CheckEffect(t, fire.Torch, filepath.Join("testdata", "ref", "torch"))
}

func TestTorchIsDeterministic(t *testing.T) { pixelfxtest.CheckDeterministic(t, fire.Torch) }

func BenchmarkTorch(b *testing.B) { pixelfxtest.BenchmarkFrames(b, fire.Torch) }
