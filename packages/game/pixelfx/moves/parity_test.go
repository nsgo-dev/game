package moves_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/moves"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"arrow-shot", moves.ArrowShot},
	{"earth-stomp", moves.EarthStomp},
	{"ground-pound", moves.GroundPound},
	{"pistol-shots", moves.PistolShots},
	{"rocket-launcher", moves.RocketLauncher},
}

func TestMovesAreRegistered(t *testing.T) {
	for _, tc := range allEffects {
		if e, ok := pixelfx.Lookup("moves/" + tc.name); !ok || e != tc.effect {
			t.Errorf("moves/%s is not registered as its effect", tc.name)
		}
	}
}

func TestMovesMatchPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestMovesAreDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkMoves(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) { pixelfxtest.BenchmarkFrames(b, tc.effect) })
	}
}

func BenchmarkMovesActed(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) { pixelfxtest.BenchmarkActFrames(b, tc.effect) })
	}
}
