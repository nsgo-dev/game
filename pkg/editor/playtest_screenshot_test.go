package editor_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestPlaytestReportMacOSScreenshots renders the Playtest Report screen on the real native macOS host
// across all 4 chart kinds (heat map, bars per wave against goal line, line per level, win-rate matrix)
// for visual verification beside design/editor/Playtest.dc.html and Spec.dc.html.
func TestPlaytestReportMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	recRel := "recordings/run_007_seed_7.replay.yaml"
	recAbs := filepath.Join(dir, filepath.FromSlash(recRel))
	if err := os.MkdirAll(filepath.Dir(recAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replay.SaveFile(recAbs, &replay.Recording{
		Version:    replay.FormatVersion,
		Seed:       7,
		Rate:       60,
		Actions:    []string{"right", "jump"},
		TotalSteps: 60,
		Events:     []replay.StepEvent{{Step: 0, Press: []int{0}}, {Step: 12, Press: []int{1}}},
	}); err != nil {
		t.Fatal(err)
	}

	platformerRuns := make([]playtest.RunReport, 20)
	for i := range 20 {
		outcome := playtest.OutcomeFinished
		reason := ""
		dur := 4.8
		if i == 6 || i == 9 || i == 11 || i == 14 || i == 16 || i == 18 {
			outcome = playtest.OutcomeFailed
			reason = "slime pit"
			dur = 5.4
		}
		rec := ""
		if i == 6 {
			rec = recRel
		}
		platformerRuns[i] = playtest.RunReport{
			Index:           i + 1,
			Seed:            uint64(i + 1),
			Outcome:         outcome,
			Reason:          reason,
			Steps:           int(dur * 60),
			DurationSeconds: dur,
			Recording:       rec,
			Perf:            &playtest.PerfSummary{SampleCount: 240, FrameMs: 4.1, P99FrameMs: 6.0},
		}
	}

	platformerRep := &playtest.Report{
		Version: playtest.ReportVersion,
		Scope:   playtest.Scope{Project: "dungeon-run", Scene: "level_01", Template: "platformer2d"},
		Bot: playtest.BotConfig{
			Profile:     "platformer-runner",
			Strategy:    "Greedy route to the door, jumps at ledges and gaps, 120–220 ms human reaction jitter, 20 seeds",
			ReactionMin: 120 * time.Millisecond,
			ReactionMax: 220 * time.Millisecond,
		},
		Rate:      60,
		Speed:     1,
		SeedCount: 20,
		Passed:    false,
		Metrics: []playtest.MetricReport{
			{Name: "finished", Label: "Finished", Aggregate: playtest.AggCount, Value: 14, Goal: &playtest.Goal{Summary: "most first-timers clear floor 1", Source: "brief.yaml", Min: playtest.Float64(16)}, Status: playtest.StatusMissed},
			{Name: "median_time", Label: "Median time", Unit: "s", Aggregate: playtest.AggMedian, Value: 48, Min: 39, Max: 71, Goal: &playtest.Goal{Summary: "under a minute", Source: "brief.yaml", Min: playtest.Float64(40), Max: playtest.Float64(60)}, Status: playtest.StatusMet},
			{Name: "frame_time", Label: "Frame time", Unit: "ms", Aggregate: playtest.AggMedian, Value: 4.1, Goal: &playtest.Goal{Summary: "16.7 ms at 60 Hz", Source: "game.yaml", Max: playtest.Float64(16.7)}, Status: playtest.StatusMet},
			{Name: "coins_taken", Label: "Coins taken", Unit: "%", Aggregate: playtest.AggMean, Value: 71, Min: 50, Max: 100, Goal: &playtest.Goal{Summary: "every coin on a route", Source: "playtest.yaml", Min: playtest.Float64(100)}, Status: playtest.StatusMissed},
		},
		HeatMaps: []playtest.HeatMap{
			{
				Metric: "deaths",
				Event:  "death",
				Points: []playtest.HeatPoint{
					{Seed: 7, Step: 140, X: 220, Y: 280, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 10, Step: 145, X: 226, Y: 278, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 12, Step: 148, X: 223, Y: 282, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 15, Step: 152, X: 228, Y: 276, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 17, Step: 155, X: 218, Y: 281, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 19, Step: 210, X: 420, Y: 214, Tag: "plank jump"},
				},
			},
		},
		Runs: platformerRuns,
	}

	shooterRep := &playtest.Report{
		Version: playtest.ReportVersion,
		Scope:   playtest.Scope{Project: "star-lane", Scene: "stage_2", Template: "shmup2d"},
		Bot: playtest.BotConfig{
			Profile:  "shooter",
			Strategy: "Dodges the nearest bullet with 180 ms reaction, fires constantly, takes power-ups within 80 px",
		},
		Rate:      60,
		Speed:     1,
		SeedCount: 1,
		Passed:    false,
		Metrics: []playtest.MetricReport{
			{Name: "cleared", Label: "Cleared the stage", Aggregate: playtest.AggCount, Value: 0, Goal: &playtest.Goal{Summary: "beatable on a second try", Source: "brief.yaml", Min: playtest.Float64(1)}, Status: playtest.StatusMissed},
			{Name: "hits_per_run", Label: "Hits per run", Aggregate: playtest.AggMedian, Value: 4, Goal: &playtest.Goal{Summary: "tense but fair", Source: "brief.yaml", Min: playtest.Float64(3), Max: playtest.Float64(6)}, Status: playtest.StatusMet},
			{Name: "powerups_taken", Label: "Power-ups taken", Unit: "%", Aggregate: playtest.AggMean, Value: 62, Goal: &playtest.Goal{Summary: "most power-ups reachable", Source: "playtest.yaml", Min: playtest.Float64(70)}, Status: playtest.StatusMissed},
			{Name: "frame_time", Label: "Frame time", Unit: "ms", Aggregate: playtest.AggMedian, Value: 5.8, Goal: &playtest.Goal{Summary: "16.7 ms at 60 Hz", Source: "game.yaml", Max: playtest.Float64(16.7)}, Status: playtest.StatusMet},
		},
		Series: []playtest.SeriesChart{
			{
				Name:      "hits_per_wave",
				Label:     "Hits taken per wave",
				Unit:      "hits",
				Aggregate: playtest.AggMedian,
				TargetMax: playtest.Float64(1),
				Entries: []playtest.SeriesEntry{
					{Label: "wave 1", Value: 0.2},
					{Label: "wave 2", Value: 0.4},
					{Label: "wave 3", Value: 0.7},
					{Label: "wave 4", Value: 2.6, Flag: playtest.FlagProblem},
					{Label: "wave 5", Value: 0.6},
					{Label: "wave 6", Value: 0.4},
				},
			},
		},
		Runs: []playtest.RunReport{{Index: 12, Seed: 12, Outcome: playtest.OutcomeFailed, Reason: "wave 4", Steps: 300, DurationSeconds: 270, Recording: recRel, Perf: &playtest.PerfSummary{SampleCount: 300, FrameMs: 5.8, P99FrameMs: 9.1}}},
	}

	puzzleRep := &playtest.Report{
		Version: playtest.ReportVersion,
		Scope:   playtest.Scope{Project: "crate-logic", Scene: "pack_1", Template: "puzzle2d"},
		Bot: playtest.BotConfig{
			Profile:  "puzzle-solver",
			Strategy: "Breadth-first search for the shortest solution, up to 2 million states; a human-like player that undoes after 3 bad moves",
		},
		Rate:      60,
		Speed:     1,
		SeedCount: 1,
		Passed:    false,
		Metrics: []playtest.MetricReport{
			{Name: "solvable", Label: "Solvable", Unit: "%", Aggregate: playtest.AggMin, Value: 100, Goal: &playtest.Goal{Summary: "no dead ends", Source: "playtest.yaml", Min: playtest.Float64(100)}, Status: playtest.StatusMet},
			{Name: "difficulty_order", Label: "Difficulty order", Aggregate: playtest.AggMax, Value: 2, Goal: &playtest.Goal{Summary: "each level harder than the last", Source: "brief.yaml", Equal: playtest.Float64(0)}, Status: playtest.StatusMissed},
			{Name: "moves_over_par", Label: "Moves over par", Aggregate: playtest.AggMedian, Value: 3, Goal: &playtest.Goal{Summary: "clever, not tedious", Source: "brief.yaml", Max: playtest.Float64(5)}, Status: playtest.StatusMet},
			{Name: "shorter_solutions", Label: "Unintended solutions", Aggregate: playtest.AggMax, Value: 1, Goal: &playtest.Goal{Summary: "one intended trick per level", Source: "playtest.yaml", Equal: playtest.Float64(0)}, Status: playtest.StatusMissed},
		},
		Series: []playtest.SeriesChart{
			{
				Name:  "shortest_solution_per_level",
				Label: "Shortest solution per level",
				Unit:  "moves",
				Entries: []playtest.SeriesEntry{
					{Label: "01", Value: 6},
					{Label: "02", Value: 9},
					{Label: "03", Value: 11},
					{Label: "04", Value: 14},
					{Label: "05", Value: 15},
					{Label: "06", Value: 19},
					{Label: "07", Value: 38, Flag: playtest.FlagProblem, Note: "needs 38 moves"},
					{Label: "08", Value: 19},
					{Label: "09", Value: 24},
					{Label: "10", Value: 27},
					{Label: "11", Value: 14, Flag: playtest.FlagWarning, Note: "14-move shortcut"},
					{Label: "12", Value: 31},
				},
			},
		},
		Runs: []playtest.RunReport{{Index: 7, Seed: 7, Outcome: playtest.OutcomeFinished, Steps: 240, DurationSeconds: 125, Recording: recRel}},
	}

	f := playtest.Float64
	cardsRep := &playtest.Report{
		Version: playtest.ReportVersion,
		Scope:   playtest.Scope{Project: "ember-deck", Scene: "mirror matches", Template: "cards2d"},
		Bot: playtest.BotConfig{
			Profile:  "card-ai",
			Strategy: "Two greedy AIs that look one turn ahead play each deck pair 200 times, taking turns to go first",
			Params:   map[string]any{"matchesPerPair": 200},
		},
		Rate:      60,
		Speed:     1,
		SeedCount: 1,
		Passed:    false,
		Metrics: []playtest.MetricReport{
			{Name: "first_player_wins", Label: "First player wins", Unit: "%", Aggregate: playtest.AggMean, Value: 61, Goal: &playtest.Goal{Summary: "going first is a small edge", Source: "brief.yaml", Min: playtest.Float64(45), Max: playtest.Float64(55)}, Status: playtest.StatusMissed},
			{Name: "match_length", Label: "Match length", Unit: "turns", Aggregate: playtest.AggMedian, Value: 9, Min: 6, Max: 15, Goal: &playtest.Goal{Summary: "quick matches", Source: "brief.yaml", Min: playtest.Float64(8), Max: playtest.Float64(12)}, Status: playtest.StatusMet},
			{Name: "most_lopsided_pair", Label: "Most lopsided pair", Unit: "%", Aggregate: playtest.AggMax, Value: 71, Goal: &playtest.Goal{Summary: "no pair above 60%", Source: "playtest.yaml", Max: playtest.Float64(60)}, Status: playtest.StatusMissed},
			{Name: "ai_think_time", Label: "AI think time", Unit: "ms", Aggregate: playtest.AggMedian, Value: 0.4, Goal: &playtest.Goal{Summary: "answers in under 50 ms", Source: "playtest.yaml", Max: playtest.Float64(50)}, Status: playtest.StatusMet},
		},
		Matrices: []playtest.MatrixChart{
			{
				Name:      "deck_win_rates",
				Label:     "Win rate by deck pair",
				Unit:      "%",
				FlagAbove: playtest.Float64(60),
				Labels:    []string{"Ember", "Frost", "Thorn"},
				Cells: [][]*float64{
					{nil, f(71), f(58)},
					{f(29), nil, f(49)},
					{f(42), f(51), nil},
				},
			},
		},
		Runs: []playtest.RunReport{{Index: 38, Seed: 38, Outcome: playtest.OutcomeFinished, Steps: 312, DurationSeconds: 52, Recording: recRel}},
	}

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := ed.OpenPlaytestReport(editor.PlaytestReportOpenConfig{
		Report:        platformerRep,
		BaseDir:       dir,
		RunsSummary:   "20 headless runs by the agent · 1 m 40 s",
		WatchRunIndex: 7,
		AgentRead:     "The pit is too hard: slime_02 and slime_03 meet at x 300 and leave no gap to jump through. coin_03 sits above the plank where no route passes.",
		Suggestions:   []string{"Offset slime_03's patrol by half a cycle", "Move coin_03 down to the jump arc"},
		Proposals: []editor.PlaytestProposal{
			{
				ID:          "playtest-review-1",
				File:        "playtest.yaml",
				MetricName:  "deaths_per_run",
				MetricLabel: "deaths per run",
				Rationale:   `from "dies 2-4 times on floor 3"`,
				Diff:        "--- a/playtest.yaml\n+++ b/playtest.yaml\n@@ -1,1 +1,6 @@\n+  - name: deaths_per_run\n+    label: Deaths per run\n",
				Status:      "pending",
			},
		},
		AdditionalEntries: []editor.PlaytestReportOpenConfig{
			{
				Report:        shooterRep,
				BaseDir:       dir,
				RunsSummary:   "20 headless runs · 4 m 30 s",
				WatchRunIndex: 12,
				AgentRead:     "Wave 4 is a wall: turret_a and turret_b fire aimed spreads on the same beat, and the gaps close faster than a 180 ms reaction.",
				Suggestions:   []string{"Delay turret_b's first volley by 0.6 s", "Spawn shield_02 before wave 4, away from the turrets"},
			},
			{
				Report:        puzzleRep,
				BaseDir:       dir,
				RunsSummary:   "12 levels · solver plus 20 human-like runs each · 2 m 05 s",
				WatchRunIndex: 7,
				WatchLabel:    "Watch level_07 solve",
				AgentRead:     "level_07 needs 38 moves, twice level_08, so the curve dips right after it. level_11 has a 14-move bypass that skips the switch trick.",
				Suggestions:   []string{"Swap level_07 and level_08 in pack_1", "Put a crate at 5, 3 in level_11 to block the short solution"},
			},
			{
				Report:        cardsRep,
				BaseDir:       dir,
				RunsSummary:   "600 matches · 3 decks · 52 s",
				WatchRunIndex: 38,
				WatchLabel:    "Watch match 38",
				AgentRead:     "Going first is worth too much: the first player plays Kindle on turn 1 and is a full card ahead by turn 3. Ember beats Frost 71% of the time.",
				Suggestions:   []string{"Raise Kindle's cost from 1 to 2", "Give the second player a Spark on turn 1"},
			},
		},
	}); err != nil {
		t.Fatalf("OpenPlaytestReport: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Screenshot("playtest-report-heatmap")

	app.Press("id:playtest-profile-1")
	app.Screenshot("playtest-report-bars")

	app.Press("id:playtest-profile-2")
	app.Screenshot("playtest-report-line")

	app.Press("id:playtest-profile-3")
	app.Screenshot("playtest-report-matrix")
}
