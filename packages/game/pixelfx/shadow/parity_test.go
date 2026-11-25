package shadow_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/shadow"
)

var effects = []struct {
	name string
	fx   *pixelfx.Effect
}{
	{"black-hole", shadow.BlackHole},
	{"curse", shadow.Curse},
	{"dark-explosion", shadow.DarkExplosion},
	{"eclipse", shadow.Eclipse},
	{"grasping-hands", shadow.GraspingHands},
	{"hex-sigil", shadow.HexSigil},
	{"life-steal", shadow.LifeSteal},
	{"necro-skulls", shadow.NecroSkulls},
	{"shadow-flames", shadow.ShadowFlames},
	{"shadow-tendrils", shadow.ShadowTendrils},
	{"soul-drain", shadow.SoulDrain},
	{"void-orb", shadow.VoidOrb},
}

func TestShadowMatchesPixelfx(t *testing.T) {
	for _, tc := range effects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.fx, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestShadowIsDeterministic(t *testing.T) {
	for _, tc := range effects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.fx)
		})
	}
}

func BenchmarkShadow(b *testing.B) {
	for _, tc := range effects {
		b.Run(tc.name, func(b *testing.B) {
			pixelfxtest.BenchmarkFrames(b, tc.fx)
		})
	}
}
