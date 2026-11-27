package editor_test

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func fixturePlatformerReport(recordingRel string) *playtest.Report {
	runs := make([]playtest.RunReport, 20)
	for i := range 20 {
		outcome := playtest.OutcomeFinished
		reason := ""
		dur := 4.8
		if i == 6 || i == 9 || i == 11 || i == 14 || i == 16 || i == 18 {
			outcome = playtest.OutcomeFailed
			reason = "slime pit"
			dur = 5.4
		}
		recPath := ""
		if i == 6 {
			recPath = recordingRel
		}
		runs[i] = playtest.RunReport{
			Index:           i + 1,
			Seed:            uint64(i + 1),
			Outcome:         outcome,
			Reason:          reason,
			Steps:           int(dur * 60),
			DurationSeconds: dur,
			Recording:       recPath,
			Perf: &playtest.PerfSummary{
				SampleCount:    120,
				FrameMs:        4.1,
				P99FrameMs:     6.0,
				AllocsPerFrame: 0,
			},
		}
	}
	return &playtest.Report{
		Version: playtest.ReportVersion,
		Scope: playtest.Scope{
			Project:  "dungeon-run",
			Scene:    "level_01",
			Template: "platformer2d",
		},
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
			{
				Name:      "finished",
				Label:     "Finished",
				Aggregate: playtest.AggCount,
				Value:     14,
				Max:       1,
				Goal: &playtest.Goal{
					Summary: "most first-timers clear floor 1",
					Source:  "brief.yaml",
					Min:     playtest.Float64(16),
				},
				Status: playtest.StatusMissed,
			},
			{
				Name:      "median_time",
				Label:     "Median time",
				Unit:      "s",
				Aggregate: playtest.AggMedian,
				Value:     48,
				Min:       39,
				Max:       71,
				Goal: &playtest.Goal{
					Summary: "under a minute",
					Source:  "brief.yaml",
					Min:     playtest.Float64(40),
					Max:     playtest.Float64(60),
				},
				Status: playtest.StatusMet,
			},
			{
				Name:      "frame_time",
				Label:     "Frame time",
				Unit:      "ms",
				Aggregate: playtest.AggMedian,
				Value:     4.1,
				Min:       3.8,
				Max:       6.0,
				Goal: &playtest.Goal{
					Summary: "16.7 ms at 60 Hz",
					Source:  "game.yaml",
					Max:     playtest.Float64(16.7),
				},
				Status: playtest.StatusMet,
			},
			{
				Name:      "coins_taken",
				Label:     "Coins taken",
				Unit:      "%",
				Aggregate: playtest.AggMean,
				Value:     71,
				Min:       50,
				Max:       100,
				Goal: &playtest.Goal{
					Summary: "every coin on a route",
					Source:  "playtest.yaml",
					Min:     playtest.Float64(100),
				},
				Status: playtest.StatusMissed,
			},
		},
		HeatMaps: []playtest.HeatMap{
			{
				Metric: "deaths",
				Event:  "death",
				Points: []playtest.HeatPoint{
					{Seed: 7, Step: 140, X: 298, Y: 270, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 10, Step: 145, X: 302, Y: 272, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 12, Step: 148, X: 300, Y: 268, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 15, Step: 152, X: 305, Y: 274, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 17, Step: 155, X: 296, Y: 271, Tag: "slime_02 + slime_03 overlap"},
					{Seed: 19, Step: 210, X: 460, Y: 210, Tag: "plank gap"},
				},
			},
		},
		Runs: runs,
	}
}

func fixtureShooterBarsReport() *playtest.Report {
	return &playtest.Report{
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
			{
				Name:      "cleared",
				Label:     "Cleared the stage",
				Aggregate: playtest.AggCount,
				Value:     0,
				Goal:      &playtest.Goal{Summary: "beatable on a second try", Source: "brief.yaml", Min: playtest.Float64(1)},
				Status:    playtest.StatusMissed,
			},
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
					{Label: "wave 4", Value: 2.6, Flag: playtest.FlagProblem, Note: "turret overlap"},
					{Label: "wave 5", Value: 0.6},
					{Label: "wave 6", Value: 0.4},
				},
			},
		},
		Runs: []playtest.RunReport{
			{Index: 1, Seed: 1, Outcome: playtest.OutcomeFailed, Steps: 300, DurationSeconds: 5},
		},
	}
}

func fixturePuzzleLineReport() *playtest.Report {
	return &playtest.Report{
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
			{
				Name:      "difficulty_order",
				Label:     "Difficulty order",
				Aggregate: playtest.AggMax,
				Value:     2,
				Goal:      &playtest.Goal{Summary: "each level harder than the last", Source: "brief.yaml", Equal: playtest.Float64(0)},
				Status:    playtest.StatusMissed,
			},
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
					{Label: "07", Value: 38, Flag: playtest.FlagProblem, Note: "spike before level 08"},
					{Label: "08", Value: 19},
					{Label: "09", Value: 24},
					{Label: "10", Value: 27},
					{Label: "11", Value: 14, Flag: playtest.FlagWarning, Note: "short bypass"},
					{Label: "12", Value: 31},
				},
			},
		},
		Runs: []playtest.RunReport{
			{Index: 1, Seed: 1, Outcome: playtest.OutcomeFinished, Steps: 600, DurationSeconds: 10},
		},
	}
}

func fixtureCardMatrixReport() *playtest.Report {
	f := playtest.Float64
	return &playtest.Report{
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
			{
				Name:      "most_lopsided_pair",
				Label:     "Most lopsided pair",
				Unit:      "%",
				Aggregate: playtest.AggMax,
				Value:     71,
				Goal:      &playtest.Goal{Summary: "no pair above 60%", Source: "playtest.yaml", Max: playtest.Float64(60)},
				Status:    playtest.StatusMissed,
			},
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
		Runs: []playtest.RunReport{
			{Index: 1, Seed: 38, Outcome: playtest.OutcomeFinished, Steps: 540, DurationSeconds: 9},
		},
	}
}

// TestPlaytestReportMissedGoalAndWatchRun verifies Acceptance Criterion 1:
// A fixture report with a missed goal displays a Missed card with its goal and source,
// and pressing "Watch run N" opens that run's .replay.yaml recording in Play mode.
func TestPlaytestReportMissedGoalAndWatchRun(t *testing.T) {
	dir := t.TempDir()
	recRel := "recordings/run_007_seed_7.replay.yaml"
	recAbs := filepath.Join(dir, filepath.FromSlash(recRel))
	if err := os.MkdirAll(filepath.Dir(recAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	rec := &replay.Recording{
		Version:    replay.FormatVersion,
		Seed:       7,
		Rate:       60,
		Actions:    []string{"right", "jump"},
		TotalSteps: 24,
		Events: []replay.StepEvent{
			{Step: 0, Press: []int{0}},
			{Step: 5, Press: []int{1}},
			{Step: 10, Release: []int{1}},
		},
	}
	if err := replay.SaveFile(recAbs, rec); err != nil {
		t.Fatalf("replay.SaveFile: %v", err)
	}

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var replayedSteps int
	ed.OnPlayReplayStep = func(p *replay.Player, _ *sceneformat.BuiltScene, _ time.Duration) {
		replayedSteps++
	}

	rep := fixturePlatformerReport(recRel)
	if err := ed.OpenPlaytestReport(editor.PlaytestReportOpenConfig{
		Report:  rep,
		BaseDir: dir,
		Proposals: []editor.PlaytestProposal{
			{
				ID:          "playtest-review-1",
				File:        "playtest.yaml",
				MetricName:  "deaths_per_run",
				MetricLabel: "deaths per run",
				Rationale:   `from "dies 2-4 times on floor 3"`,
				Diff:        "--- a/playtest.yaml\n+++ b/playtest.yaml\n@@ -1,1 +1,2 @@\n+  - name: deaths_per_run\n",
				Status:      "pending",
			},
		},
	}); err != nil {
		t.Fatalf("OpenPlaytestReport: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	if !app.Exists("id:playtest-report-screen") {
		t.Fatal("expected playtest-report-screen to be mounted")
	}
	if got := app.Find("id:playtest-title").Text; got != "Playtest · level_01" {
		t.Fatalf("title = %q, want %q", got, "Playtest · level_01")
	}
	if got := app.Find("id:playtest-subtitle").Text; !strings.Contains(got, "dungeon-run") || !strings.Contains(got, "20 headless runs") {
		t.Fatalf("subtitle = %q", got)
	}
	if got := app.Find("id:playtest-metric-badge-text-finished").Text; got != "Missed" {
		t.Fatalf("finished badge = %q, want Missed", got)
	}
	if got := app.Find("id:playtest-metric-goal-finished").Text; !strings.Contains(got, "Goal · most first-timers clear floor 1") {
		t.Fatalf("finished goal = %q", got)
	}
	if got := app.Find("id:playtest-metric-source-finished").Text; got != "brief.yaml" {
		t.Fatalf("finished source = %q, want brief.yaml", got)
	}
	if got := app.Find("id:playtest-metric-badge-text-median_time").Text; got != "Met" {
		t.Fatalf("median_time badge = %q, want Met", got)
	}
	if !app.Exists("id:playtest-proposal-banner") {
		t.Fatal("expected playtest_define proposal banner to be visible")
	}
	if got := app.Find("id:playtest-footnote").Text; !strings.Contains(got, "replay system") || !strings.Contains(got, "playtest.yaml") {
		t.Fatalf("footnote = %q", got)
	}

	// Clicking Watch run 7 closes the report and starts Play mode with run 7's replay recording.
	if !app.Exists("id:playtest-watch-run") || !app.Exists("Watch run 7") {
		t.Fatal("expected Watch run 7 button to be visible")
	}
	app.Press("id:playtest-watch-run")
	app.Settle()

	if ed.PlaytestReportOpen() {
		t.Fatal("expected PlaytestReportOpen = false after Watch run 7")
	}
	if !ed.Playing() {
		t.Fatal("expected Playing() = true after Watch run 7")
	}
	if ed.PlayReplay() == nil || ed.PlayReplay().Seed != 7 {
		t.Fatalf("PlayReplay = %+v, want seed 7", ed.PlayReplay())
	}
	if ed.PlayReplayRunIndex() != 7 || ed.PlayReplayPath() != recRel {
		t.Fatalf("PlayReplayRunIndex=%d path=%q, want 7 %q", ed.PlayReplayRunIndex(), ed.PlayReplayPath(), recRel)
	}
	if !app.Exists("id:play-replay-banner") {
		t.Fatal("expected play-replay-banner in Play mode")
	}

	// Clicking Back to Playtest stops Play mode and returns to the report screen.
	app.Press("id:play-replay-back-report")
	app.Settle()
	if ed.Playing() {
		t.Fatal("expected Playing() = false after Back to Playtest")
	}
	if !ed.PlaytestReportOpen() {
		t.Fatal("expected PlaytestReportOpen() = true after Back to Playtest")
	}
	_ = replayedSteps
}

// TestPlaytestReportAllFourChartKinds verifies Acceptance Criterion 2:
// Every one of the 4 chart kinds (heat map, bars per wave against goal line, line per level,
// and win-rate matrix with >60% flagged) is chosen and renders non-empty drawing commands from its fixture report.
func TestPlaytestReportAllFourChartKinds(t *testing.T) {
	cases := []struct {
		name      string
		report    *playtest.Report
		wantKind  editor.PlaytestChartKind
		wantTexts []string
	}{
		{
			name:      "heat_map",
			report:    fixturePlatformerReport(""),
			wantKind:  editor.PlaytestChartHeatMap,
			wantTexts: []string{"slime_02 + slime_03 overlap", "Hotspot cluster"},
		},
		{
			name:      "bars_per_wave",
			report:    fixtureShooterBarsReport(),
			wantKind:  editor.PlaytestChartBars,
			wantTexts: []string{"goal <= 1 hits", "wave 4", "2.6 !"},
		},
		{
			name:      "line_per_level",
			report:    fixturePuzzleLineReport(),
			wantKind:  editor.PlaytestChartLine,
			wantTexts: []string{"07", "38 !", "12"},
		},
		{
			name:      "win_rate_matrix",
			report:    fixtureCardMatrixReport(),
			wantKind:  editor.PlaytestChartMatrix,
			wantTexts: []string{"vs Ember", "71%", "above 60%", "balanced", "same deck"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := editor.SelectPlaytestChart(tc.report)
			if spec.Kind != tc.wantKind {
				t.Fatalf("SelectPlaytestChart kind = %q, want %q", spec.Kind, tc.wantKind)
			}

			var d core.Drawing
			editor.DrawPlaytestChart(&d, core.Size{Width: 960, Height: 520}, spec, nil)
			ops := d.Ops
			if len(ops) < 8 {
				t.Fatalf("expected rich drawing ops for %s, got %d", tc.name, len(ops))
			}

			var drawnTexts []string
			for _, op := range ops {
				if op.Kind == core.DrawFillText {
					drawnTexts = append(drawnTexts, op.Text)
				}
			}
			joined := strings.Join(drawnTexts, " | ")
			for _, want := range tc.wantTexts {
				if !strings.Contains(joined, want) {
					t.Errorf("chart %s missing text %q in drawn texts: %s", tc.name, want, joined)
				}
			}

			// Also verify rendering through the live EditorView UI.
			ed, err := editor.New(editor.DefaultSceneDoc(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := ed.OpenPlaytestReport(editor.PlaytestReportOpenConfig{Report: tc.report}); err != nil {
				t.Fatal(err)
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return editor.EditorView(cx, ed)
			}, nsgotest.Size(1440, 900))
			app.Settle()
			if !app.Exists("id:playtest-chart-canvas") {
				t.Fatalf("chart %s did not mount playtest-chart-canvas", tc.name)
			}
			if got := app.Find("id:playtest-chart-title").Text; got == "" {
				t.Fatalf("chart %s has empty title", tc.name)
			}
		})
	}
}

func TestPlaytestReportInteractionsAndProposals(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, playtest.FileName)
	initialCfg := "version: 1\nscope:\n  project: dungeon-run\n  scene: level_01\n"
	if err := os.WriteFile(cfgPath, []byte(initialCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}

	var prompts []editor.PlaytestPromptRequest
	ed.OnPlaytestPrompt = func(req editor.PlaytestPromptRequest) {
		prompts = append(prompts, req)
	}

	applied := false
	if err := ed.OpenPlaytestReport(editor.PlaytestReportOpenConfig{
		Report:  fixturePlatformerReport(""),
		BaseDir: dir,
		Proposals: []editor.PlaytestProposal{
			{
				ID:          "prop-1",
				File:        playtest.FileName,
				MetricName:  "deaths_per_run",
				MetricLabel: "deaths per run",
				Rationale:   "dies 2-4 times on floor 3",
				Diff:        "--- a/playtest.yaml\n+++ b/playtest.yaml\n@@ -1,1 +1,2 @@\n+  - name: deaths_per_run\n",
				Status:      "pending",
				ApplyFunc: func() error {
					applied = true
					return nil
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// Open playtest.yaml preview drawer.
	app.Press("id:playtest-open-config")
	app.Settle()
	if !app.Exists("id:playtest-config-drawer") {
		t.Fatal("expected playtest-config-drawer after pressing Open")
	}
	if got := app.Find("id:playtest-config-content").Text; !strings.Contains(got, "level_01") {
		t.Fatalf("config preview = %q", got)
	}
	app.Press("id:playtest-config-close")
	app.Settle()
	if app.Exists("id:playtest-config-drawer") {
		t.Fatal("expected config drawer to close")
	}

	// Open Review drawer for playtest_define proposal and apply it.
	app.Press("id:playtest-proposal-review")
	app.Settle()
	if !app.Exists("id:playtest-proposal-drawer") {
		t.Fatal("expected playtest-proposal-drawer after pressing Review")
	}
	app.Press("id:playtest-proposal-apply")
	app.Settle()
	if !applied {
		t.Fatal("expected proposal ApplyFunc to run")
	}
	if app.Exists("id:playtest-proposal-banner") {
		t.Fatal("expected proposal banner to disappear once applied")
	}

	// Ask the agent to tune it and click a suggestion card.
	app.Press("id:playtest-ask-tune")
	app.Settle()
	app.Press("id:playtest-suggestion-0")
	app.Settle()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %d, want 2", len(prompts))
	}
	if prompts[0].Kind != "tune" || prompts[1].Kind != "suggestion" {
		t.Fatalf("prompt kinds = %q, %q", prompts[0].Kind, prompts[1].Kind)
	}
}

func TestPlaytestReportPathContainmentAndPlayModeGuards(t *testing.T) {
	ed, projDir := openTestProject(t)
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.yaml")
	if err := os.WriteFile(outsideFile, []byte("untouched\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Report whose run 7 points to an absolute path outside the project and ConfigPath points outside.
	rep := fixturePlatformerReport(outsideFile)
	if err := ed.OpenPlaytestReport(editor.PlaytestReportOpenConfig{
		Report:     rep,
		ReportPath: "playtest.report.yaml",
		ConfigPath: outsideFile,
		BaseDir:    projDir,
		Proposals: []editor.PlaytestProposal{
			{
				ID:         "abs-escape",
				File:       outsideFile,
				MetricName: "evil",
				After:      "overwritten\n",
				Status:     "pending",
			},
			{
				ID:         "dotdot-name",
				File:       "..notes.yaml",
				MetricName: "valid_dotdot",
				After:      "ok: true\n",
				Status:     "pending",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Absolute path outside BaseDir must be rejected for ApplyPlaytestProposal, WatchPlaytestRun, and OpenPlaytestConfigPreview.
	if err := ed.ApplyPlaytestProposal("abs-escape"); err == nil {
		t.Fatal("ApplyPlaytestProposal wrote to absolute path outside project")
	}
	if got, _ := os.ReadFile(outsideFile); string(got) != "untouched\n" {
		t.Fatalf("outside file was modified: %q", string(got))
	}
	if err := ed.WatchPlaytestRun(7); err == nil {
		t.Fatal("WatchPlaytestRun allowed absolute recording path outside project")
	}
	if err := ed.OpenPlaytestConfigPreview(); err != nil {
		t.Fatalf("OpenPlaytestConfigPreview unexpected error: %v", err)
	}
	if sess := ed.PlaytestSession(); sess == nil || sess.ConfigPreviewErr == "" || sess.ConfigPreviewText != "" {
		t.Fatalf("OpenPlaytestConfigPreview leaked outside file: err=%q text=%q", sess.ConfigPreviewErr, sess.ConfigPreviewText)
	}

	// 2. Non-existent target file under a symlinked directory escaping projDir must be rejected.
	subLink := filepath.Join(projDir, "sublink")
	if err := os.Symlink(outsideDir, subLink); err == nil {
		propID := ed.AddPlaytestProposal(editor.PlaytestProposal{
			ID:         "symlink-dir-escape",
			File:       "sublink/new_escape.yaml",
			MetricName: "sym",
			After:      "escaped\n",
			Status:     "pending",
		})
		if err := ed.ApplyPlaytestProposal(propID); err == nil {
			t.Fatal("ApplyPlaytestProposal wrote through symlinked parent dir outside project")
		}
		if _, err := os.Stat(filepath.Join(outsideDir, "new_escape.yaml")); !os.IsNotExist(err) {
			t.Fatalf("symlinked parent dir write created outside file: %v", err)
		}
	}

	// 3. Valid filename starting with ".." inside the project must succeed and write atomically.
	if err := ed.ApplyPlaytestProposal("dotdot-name"); err != nil {
		t.Fatalf("ApplyPlaytestProposal rejected valid ..notes.yaml: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(projDir, "..notes.yaml")); err != nil || string(got) != "ok: true\n" {
		t.Fatalf("..notes.yaml = %q, err=%v", string(got), err)
	}

	// 4. Play-mode guards on ApplyPlaytestProposal and DismissPlaytestProposal.
	playPropID := ed.AddPlaytestProposal(editor.PlaytestProposal{
		ID:         "play-guard",
		File:       "playtest.yaml",
		MetricName: "m",
		After:      "ok: true\n",
		Status:     "pending",
	})
	var rootScope *core.Scope
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(800, 600))
	if err := ed.PlayIn(rootScope); err != nil {
		t.Fatalf("PlayIn: %v", err)
	}
	if err := ed.ApplyPlaytestProposal(playPropID); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("ApplyPlaytestProposal during Play = %v, want ErrPlaying", err)
	}
	if err := ed.DismissPlaytestProposal(playPropID); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("DismissPlaytestProposal during Play = %v, want ErrPlaying", err)
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestPlaytestReportResetsOnProjectAndDemoSwitch(t *testing.T) {
	ed, projDir := openTestProject(t)
	if err := ed.OpenPlaytestReport(editor.PlaytestReportOpenConfig{
		Report:  fixturePlatformerReport(""),
		BaseDir: projDir,
	}); err != nil {
		t.Fatal(err)
	}
	if !ed.PlaytestReportOpen() || ed.PlaytestSession() == nil {
		t.Fatal("expected playtest report open")
	}

	// OpenProject must clear playtest screen and session.
	if err := ed.OpenProject(projDir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if ed.PlaytestReportOpen() || ed.PlaytestSession() != nil {
		t.Fatalf("OpenProject leaked playtest state: open=%v sess=%v", ed.PlaytestReportOpen(), ed.PlaytestSession())
	}

	// Open another report, then switch to Demo via OpenDemo: must also clear playtest state.
	if err := ed.OpenPlaytestReport(editor.PlaytestReportOpenConfig{
		Report:  fixturePlatformerReport(""),
		BaseDir: projDir,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.PlaytestReportOpen() || ed.PlaytestSession() != nil {
		t.Fatalf("OpenDemo leaked playtest state: open=%v sess=%v", ed.PlaytestReportOpen(), ed.PlaytestSession())
	}
}

func TestPlaytestReportChartsHandleNaNInfZeroMaxValAndNarrowBars(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)

	// 1. HeatMap with NaN/Inf points.
	hmRep := &playtest.Report{
		Version:   playtest.ReportVersion,
		SeedCount: 1,
		HeatMaps: []playtest.HeatMap{
			{
				Metric: "deaths",
				Event:  "death",
				Points: []playtest.HeatPoint{
					{X: nan, Y: 10},
					{X: 100, Y: inf},
					{X: 120, Y: 80, Tag: "pit"},
				},
			},
		},
	}
	hmSpec := editor.SelectPlaytestChart(hmRep)
	if hmSpec.HeatMap == nil || len(hmSpec.HeatMap.Points) != 1 {
		t.Fatalf("expected non-finite heat points filtered, got %+v", hmSpec.HeatMap)
	}

	// 2. Narrow bar chart with n=30 bars + NaN/Inf values + direct MaxVal=0 call.
	entries := make([]playtest.SeriesEntry, 30)
	for i := range entries {
		v := float64(i % 5)
		if i == 0 {
			v = nan
		} else if i == 1 {
			v = inf
		}
		entries[i] = playtest.SeriesEntry{Label: fmt.Sprintf("W%d", i+1), Value: v}
	}
	barSpec := editor.PlaytestChartSpec{
		Kind: editor.PlaytestChartBars,
		Bars: &editor.PlaytestSeriesChart{
			Name:    "waves",
			Entries: entries,
			MaxVal:  0, // deliberately invalid MaxVal
		},
	}
	lineSpec := editor.PlaytestChartSpec{
		Kind: editor.PlaytestChartLine,
		Line: &editor.PlaytestSeriesChart{
			Name:    "levels",
			Entries: entries[:5],
			MaxVal:  nan,
		},
	}
	matSpec := editor.PlaytestChartSpec{
		Kind: editor.PlaytestChartMatrix,
		Matrix: &editor.PlaytestMatrixChart{
			Labels:    []string{"A", "B"},
			FlagAbove: 60,
			Unit:      "%",
			Cells:     [][]*float64{{nil, &nan}, {&inf, nil}},
		},
	}

	for _, spec := range []editor.PlaytestChartSpec{hmSpec, barSpec, lineSpec, matSpec} {
		var d core.Drawing
		editor.DrawPlaytestChart(&d, core.Size{Width: 360, Height: 220}, spec, nil)
		if len(d.Ops) == 0 {
			t.Fatalf("chart %s drew 0 ops", spec.Kind)
		}
	}
}
