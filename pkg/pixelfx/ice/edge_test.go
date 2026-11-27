package ice_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/ice"
)

// Edge option values pixelfx itself may throw on (an empty cluster list, a count of 0) must not panic in the port: every effect renders, plain and acted out, with each of its number options at each edge value.
func TestIceSurvivesEdgeOptions(t *testing.T) {
	edges := []float64{0, 1, 10, -5, math.NaN()}
	for _, tc := range allEffects {
		for _, opt := range tc.effect.Meta.Options {
			if opt.Name == "palette" || opt.Default.Kind() != pixelfx.KindNumber {
				continue
			}
			for _, v := range edges {
				t.Run(fmt.Sprintf("%s/%s=%v", tc.name, opt.Name, v), func(t *testing.T) {
					t.Parallel()
					o := pixelfx.Options{}.Set(opt.Name, pixelfx.Num(v))
					if fr := pixelfx.Render(tc.effect, o, 0); len(fr.Pixels) == 0 {
						t.Fatalf("rendered no frames")
					}
					pixelfx.RenderAct(tc.effect, o, 0)
				})
			}
		}
	}
}

// The two upstream throws the port turns into a quiet skip: ice-spikes with a reach too short for any cluster, and snowflake-burst with no flakes.
func TestIceGuardedDeviations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		effect *pixelfx.Effect
		opt    string
		v      float64
	}{
		{"ice-spikes reach 10", ice.IceSpikes, "reach", 10},
		{"snowflake-burst count 0", ice.SnowflakeBurst, "count", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fr := pixelfx.Render(tc.effect, pixelfx.Options{}.Set(tc.opt, pixelfx.Num(tc.v)), 0)
			if len(fr.Pixels) == 0 {
				t.Fatalf("rendered no frames")
			}
		})
	}
}
