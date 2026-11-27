package weather_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/pkg/pixelfx/weather"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"aurora", weather.Aurora},
	{"blizzard", weather.Blizzard},
	{"cherry-blossoms", weather.CherryBlossoms},
	{"clouds", weather.Clouds},
	{"falling-leaves", weather.FallingLeaves},
	{"fireflies", weather.Fireflies},
	{"fog", weather.Fog},
	{"god-rays", weather.GodRays},
	{"rain", weather.Rain},
	{"shooting-stars", weather.ShootingStars},
	{"snow", weather.Snow},
	{"storm", weather.Storm},
}

func TestWeatherMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect)
		})
	}
}

func TestWeatherIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkRain(b *testing.B) { pixelfxtest.BenchmarkFrames(b, weather.Rain) }

func BenchmarkStorm(b *testing.B) { pixelfxtest.BenchmarkFrames(b, weather.Storm) }
