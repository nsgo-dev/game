package moves_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/moves"
)

func TestGroundPoundMatchesPixelfx(t *testing.T) {
	pixelfxtest.CheckEffect(t, moves.GroundPound, filepath.Join("testdata", "ref", "ground-pound"))
}

func TestGroundPoundIsDeterministic(t *testing.T) {
	pixelfxtest.CheckDeterministic(t, moves.GroundPound)
}

func BenchmarkGroundPound(b *testing.B) { pixelfxtest.BenchmarkFrames(b, moves.GroundPound) }

func BenchmarkGroundPoundActed(b *testing.B) { pixelfxtest.BenchmarkActFrames(b, moves.GroundPound) }
