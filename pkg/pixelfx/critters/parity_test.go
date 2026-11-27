package critters_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/critters"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"bat", critters.Bat},
	{"bird", critters.Bird},
	{"butterfly", critters.Butterfly},
	{"eyeball", critters.Eyeball},
	{"fish-school", critters.FishSchool},
	{"flying-skull", critters.FlyingSkull},
	{"frog", critters.Frog},
	{"ghost", critters.Ghost},
	{"jellyfish", critters.Jellyfish},
	{"slime", critters.Slime},
	{"snake", critters.Snake},
	{"wisp", critters.Wisp},
}

func TestCrittersAreRegistered(t *testing.T) {
	for _, tc := range allEffects {
		if e, ok := pixelfx.Lookup("critters/" + tc.name); !ok || e != tc.effect {
			t.Errorf("critters/%s is not registered as %s", tc.name, tc.effect.Meta.Name)
		}
	}
	n := 0
	for _, id := range pixelfx.IDs() {
		if strings.HasPrefix(id, "critters/") {
			n++
		}
	}
	if n != len(allEffects) {
		t.Errorf("%d critters effects registered, %d tested", n, len(allEffects))
	}
}

func TestCrittersMatchPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestCrittersAreDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkFlyingSkull(b *testing.B) { pixelfxtest.BenchmarkFrames(b, critters.FlyingSkull) }

func BenchmarkJellyfish(b *testing.B) { pixelfxtest.BenchmarkFrames(b, critters.Jellyfish) }
