package transitions_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/pkg/pixelfx/transitions"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"blinds", transitions.Blinds},
	{"burn-away", transitions.BurnAway},
	{"crt-off", transitions.CRTOff},
	{"diamond-wipe", transitions.DiamondWipe},
	{"glitch-wipe", transitions.GlitchWipe},
	{"iris", transitions.Iris},
	{"pixel-dissolve", transitions.PixelDissolve},
	{"shatter-wipe", transitions.ShatterWipe},
	{"slash-wipe", transitions.SlashWipe},
	{"star-iris", transitions.StarIris},
}

func TestTransitionsMatchPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.effect)
		})
	}
}

func TestTransitionsAreDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkTransitions(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) {
			pixelfxtest.BenchmarkFrames(b, tc.effect)
		})
	}
}
