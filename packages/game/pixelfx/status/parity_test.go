package status_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/status"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"bleeding", status.Bleeding},
	{"burning", status.Burning},
	{"charmed", status.Charmed},
	{"confused", status.Confused},
	{"cursed", status.Cursed},
	{"frozen", status.Frozen},
	{"haste", status.Haste},
	{"petrified", status.Petrified},
	{"poisoned", status.Poisoned},
	{"rage", status.Rage},
	{"sleep", status.Sleep},
	{"slowed", status.Slowed},
	{"stunned", status.Stunned},
}

func TestStatusMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestStatusIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkStatus(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) {
			pixelfxtest.BenchmarkFrames(b, tc.effect)
		})
	}
}
