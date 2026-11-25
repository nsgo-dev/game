package ui_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/ui"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"achievement", ui.Achievement},
	{"coin-collect", ui.CoinCollect},
	{"combo-counter", ui.ComboCounter},
	{"confetti", ui.Confetti},
	{"countdown", ui.Countdown},
	{"crit-number", ui.CritNumber},
	{"damage-number", ui.DamageNumber},
	{"exclamation", ui.Exclamation},
	{"health-bar-hit", ui.HealthBarHit},
	{"heart-lost", ui.HeartLost},
	{"level-up", ui.LevelUp},
	{"quest-complete", ui.QuestComplete},
}

func TestRegistered(t *testing.T) {
	for _, tc := range allEffects {
		id := "ui/" + tc.name
		got, ok := pixelfx.Lookup(id)
		if !ok {
			t.Errorf("pixelfx.Lookup(%q) not found", id)
			continue
		}
		if got != tc.effect {
			t.Errorf("pixelfx.Lookup(%q) returned %p, want %p", id, got, tc.effect)
		}
		if got.ID != id {
			t.Errorf("effect.ID = %q, want %q", got.ID, id)
		}
	}
}

func TestMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkLevelUp(b *testing.B) { pixelfxtest.BenchmarkFrames(b, ui.LevelUp) }

func BenchmarkLevelUpAct(b *testing.B) { pixelfxtest.BenchmarkActFrames(b, ui.LevelUp) }
