package impacts_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/impacts"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"blunt-hit", impacts.BluntHit},
	{"bullet-impact", impacts.BulletImpact},
	{"crit-hit", impacts.CritHit},
	{"ground-slam", impacts.GroundSlam},
	{"heavy-landing", impacts.HeavyLanding},
	{"hit-flash", impacts.HitFlash},
	{"hit-spark", impacts.HitSpark},
	{"impact-frame", impacts.ImpactFrame},
	{"parry", impacts.Parry},
	{"ricochet", impacts.Ricochet},
	{"shield-block", impacts.ShieldBlock},
}

func TestImpactsEffectsAreRegistered(t *testing.T) {
	for _, tc := range allEffects {
		e, ok := pixelfx.Lookup("impacts/" + tc.name)
		if !ok || e != tc.effect {
			t.Errorf("impacts/%s is not registered as its effect", tc.name)
		}
	}
}

func TestImpactsEffectsMatchPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestImpactsEffectsAreDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkImpacts(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) {
			pixelfxtest.BenchmarkFrames(b, tc.effect)
		})
	}
}
