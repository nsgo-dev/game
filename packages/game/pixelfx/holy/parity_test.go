package holy_test

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/holy"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"blessing", holy.Blessing},
	{"divine-shield", holy.DivineShield},
	{"feather-burst", holy.FeatherBurst},
	{"halo", holy.Halo},
	{"heal", holy.Heal},
	{"heal-aoe", holy.HealAoe},
	{"holy-pillar", holy.HolyPillar},
	{"resurrection", holy.Resurrection},
	{"smite", holy.Smite},
	{"sunburst", holy.Sunburst},
}

func TestHolyMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestHolyIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkHolyPillar(b *testing.B) { pixelfxtest.BenchmarkFrames(b, holy.HolyPillar) }

func BenchmarkResurrection(b *testing.B) { pixelfxtest.BenchmarkFrames(b, holy.Resurrection) }

func BenchmarkResurrectionAct(b *testing.B) { pixelfxtest.BenchmarkActFrames(b, holy.Resurrection) }

// TestHolyBoundsNumericOptions renders the effects whose loops step by a numeric option with values that would otherwise never finish (a negative or infinite radius, infinite rays or reach, a huge shaft count) and checks every frame still comes out.
func TestHolyBoundsNumericOptions(t *testing.T) {
	inf, nan := math.Inf(1), math.NaN()
	cases := []struct {
		name   string
		effect *pixelfx.Effect
		option string
		values []float64
	}{
		{"divine-shield", holy.DivineShield, "radius", []float64{-1, -5, 0, inf, -inf, nan, 1e12}},
		{"heal-aoe", holy.HealAoe, "reach", []float64{-1, 0, inf, -inf, nan, 1e12}},
		{"sunburst", holy.Sunburst, "rays", []float64{-1, 0, inf, -inf, nan, 1e12}},
		{"holy-pillar", holy.HolyPillar, "shafts", []float64{-1, 0, inf, nan, 1e9}},
	}
	for _, tc := range cases {
		for _, v := range tc.values {
			t.Run(fmt.Sprintf("%s/%s=%v", tc.name, tc.option, v), func(t *testing.T) {
				t.Parallel()
				f := pixelfx.Render(tc.effect, pixelfx.Options{}.Set(tc.option, pixelfx.Num(v)), 0)
				if len(f.Pixels) == 0 {
					t.Fatalf("no frames rendered")
				}
			})
		}
	}
}
