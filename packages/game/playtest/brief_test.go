package playtest_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/playtest"
	"github.com/nsgo-dev/game/packages/game/project"
)

func TestApplyBriefTargetsAndLoadBriefTargets(t *testing.T) {
	dir := t.TempDir()
	// Missing brief.yaml returns nil, nil.
	goals, err := playtest.LoadBriefTargets(dir, "floor_01")
	if err != nil || len(goals) != 0 {
		t.Fatalf("missing brief: goals=%v err=%v", goals, err)
	}

	b := &project.Brief{
		Version: project.BriefVersion,
		Pitch:   "Dungeon Run",
		Feel: &project.BriefFeel{
			Summary: "Clear floor 1 under a minute; 2-4 deaths on floor 3.",
			Targets: []project.BriefPlaytestTarget{
				{Metric: "median_time", Scene: "floor_01", Summary: "under a minute", Unit: "s", Max: project.Float64(60)},
				{Metric: "deaths", Scene: "floor_03", Summary: "2–4 deaths", Min: project.Float64(2), Max: project.Float64(4)},
			},
		},
	}
	if err := project.SaveBrief(filepath.Join(dir, project.BriefFileName), b); err != nil {
		t.Fatalf("SaveBrief: %v", err)
	}

	goals, err = playtest.LoadBriefTargets(dir, "floor_03")
	if err != nil {
		t.Fatalf("LoadBriefTargets: %v", err)
	}
	deathsGoal := goals["deaths"]
	if deathsGoal == nil || deathsGoal.Source != "brief.yaml" || *deathsGoal.Min != 2 || *deathsGoal.Max != 4 {
		t.Fatalf("deaths goal = %+v", deathsGoal)
	}
	if deathsGoal.Evaluate(3) != playtest.StatusMet || deathsGoal.Evaluate(6.1) != playtest.StatusMissed {
		t.Fatalf("unexpected evaluation for 3 / 6.1")
	}

	cfg := &playtest.Config{
		Version: playtest.CurrentVersion,
		Scope:   playtest.Scope{Scene: "floor_03"},
		Runs:    playtest.Runs{Count: 5},
		Bot:     playtest.BotConfig{Profile: "platformer-runner"},
		Metrics: []playtest.Metric{
			{Name: "deaths", Measure: playtest.Measure{Kind: playtest.MeasureEvent, Key: "death"}},
			{Name: "frame_time", Measure: playtest.Measure{Kind: playtest.MeasurePerf, Key: "p95_frame_ms"}, Goal: &playtest.Goal{Source: "game.yaml", Max: playtest.Float64(16.6)}},
		},
	}
	if n := playtest.ApplyBriefTargets(cfg, b); n != 1 {
		t.Fatalf("ApplyBriefTargets updated %d, want 1", n)
	}
	if cfg.Metrics[0].Goal == nil || cfg.Metrics[0].Goal.Source != "brief.yaml" || *cfg.Metrics[0].Goal.Max != 4 {
		t.Fatalf("deaths metric goal = %+v", cfg.Metrics[0].Goal)
	}
	if cfg.Metrics[1].Goal.Source != "game.yaml" {
		t.Fatalf("game.yaml goal was overwritten: %+v", cfg.Metrics[1].Goal)
	}
}
