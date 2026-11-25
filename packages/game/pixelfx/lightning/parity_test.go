package lightning_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/lightning"
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

func BenchmarkStormCloud(b *testing.B) { pixelfxtest.BenchmarkFrames(b, lightning.StormCloud) }

func BenchmarkLightningStrike(b *testing.B) {
	pixelfxtest.BenchmarkFrames(b, lightning.LightningStrike)
}

func BenchmarkChainLightningAct(b *testing.B) {
	pixelfxtest.BenchmarkActFrames(b, lightning.ChainLightning)
}
