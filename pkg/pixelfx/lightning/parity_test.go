package lightning_test

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/pkg/pixelfx/lightning"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"ball-lightning", lightning.BallLightning},
	{"chain-lightning", lightning.ChainLightning},
	{"charge-up", lightning.ChargeUp},
	{"electrified", lightning.Electrified},
	{"lightning-strike", lightning.LightningStrike},
	{"plasma-ball", lightning.PlasmaBall},
	{"spark-arc", lightning.SparkArc},
	{"storm-cloud", lightning.StormCloud},
	{"tesla-coil", lightning.TeslaCoil},
	{"thunderclap", lightning.Thunderclap},
	{"zap-hit", lightning.ZapHit},
}

func TestLightningMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestLightningIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

// Every effect of the category is registered under its pixelfx id.
func TestLightningIsRegistered(t *testing.T) {
	for _, tc := range allEffects {
		if e, ok := pixelfx.Lookup("lightning/" + tc.name); !ok || e != tc.effect {
			t.Errorf("lightning/%s is not registered as its effect", tc.name)
		}
	}
}

func TestLightningSurvivesEdgeOptions(t *testing.T) {
	maskOnly := pixelfx.StockActor(2)
	maskOnly.Pixels = nil
	for _, tc := range []struct {
		name   string
		effect *pixelfx.Effect
		opts   pixelfx.Options
	}{
		{"charge-up/charge=NaN", lightning.ChargeUp, pixelfx.Options{}.Set("charge", pixelfx.Num(math.NaN()))},
		{"charge-up/charge=-1e18", lightning.ChargeUp, pixelfx.Options{}.Set("charge", pixelfx.Num(-1e18))},
		{"plasma-ball/tendrils=+Inf", lightning.PlasmaBall, pixelfx.Options{}.Set("tendrils", pixelfx.Num(math.Inf(1)))},
		{"plasma-ball/tendrils=1e9", lightning.PlasmaBall, pixelfx.Options{}.Set("tendrils", pixelfx.Num(1e9))},
		{"plasma-ball/tendrils=NaN", lightning.PlasmaBall, pixelfx.Options{}.Set("tendrils", pixelfx.Num(math.NaN()))},
		{"electrified/mask-only-actor", lightning.Electrified, pixelfx.Options{Actor: maskOnly}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if fr := pixelfx.Render(tc.effect, tc.opts, 0); len(fr.Pixels) == 0 {
				t.Fatalf("rendered no frames")
			}
		})
	}
}

func BenchmarkStormCloud(b *testing.B) { pixelfxtest.BenchmarkFrames(b, lightning.StormCloud) }

func BenchmarkLightningStrike(b *testing.B) {
	pixelfxtest.BenchmarkFrames(b, lightning.LightningStrike)
}

func BenchmarkChainLightningAct(b *testing.B) {
	pixelfxtest.BenchmarkActFrames(b, lightning.ChainLightning)
}
