package editormcp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/playtest"
)

const fixturePlaytestYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/playtest.v1.json
version: 1
scope:
  project: fixture
  scene: room
runs:
  count: 4
  seedStart: 1
  rate: 60
  speed: 1
  maxDuration: 30s
bot:
  profile: platformer-runner
  reactionMin: 120ms
  reactionMax: 220ms
metrics:
  - name: finished
    label: Finished
    measure:
      kind: outcome
      key: finished
      aggregate: count
    goal:
      summary: clear the room
      source: brief.yaml
      min: 3
`

func TestGamePlaytestTool(t *testing.T) {
	f := openFixture(t)
	if err := os.WriteFile(filepath.Join(f.dir, playtest.FileName), []byte(fixturePlaytestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotReq editormcp.PlaytestRunRequest
	s := editormcp.New(f.ed, editormcp.Options{
		RunPlaytest: func(_ context.Context, req editormcp.PlaytestRunRequest) (*playtest.Report, error) {
			gotReq = req
			return &playtest.Report{
				Version:   playtest.ReportVersion,
				Scope:     req.Config.Scope,
				Bot:       req.Config.Bot,
				Rate:      60,
				Speed:     1,
				SeedCount: 2,
				Passed:    false,
				Metrics: []playtest.MetricReport{
					{
						Name:      "finished",
						Label:     "Finished",
						Aggregate: playtest.AggCount,
						Value:     1,
						Goal:      &playtest.Goal{Summary: "clear the room", Source: "brief.yaml", Min: playtest.Float64(3)},
						Status:    playtest.StatusMissed,
					},
				},
				Runs: []playtest.RunReport{
					{Index: 1, Seed: 10, Outcome: playtest.OutcomeFinished, Steps: 60, DurationSeconds: 1},
					{Index: 2, Seed: 20, Outcome: playtest.OutcomeFailed, Steps: 30, DurationSeconds: 0.5},
				},
			}, nil
		},
	})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolGamePlaytest, map[string]any{
		"seeds": []int{10, 20},
		"out":   "reports/room.report.yaml",
	})
	out := mustOK(t, res)
	var got editormcp.PlaytestRunResult
	structured(t, res, &got)

	if got.File != "playtest.yaml" || got.ReportFile != "reports/room.report.yaml" || got.Passed || got.MissedCount != 1 {
		t.Fatalf("PlaytestRunResult = %+v\n%s", got, out)
	}
	if !strings.Contains(out, "[MISSED] Finished = 1") || !strings.Contains(out, "brief.yaml") {
		t.Fatalf("text output missing missed metric:\n%s", out)
	}
	if gotReq.ConfigFile != "playtest.yaml" || len(gotReq.Config.Runs.Seeds) != 2 || gotReq.Config.Runs.Seeds[0] != 10 {
		t.Fatalf("unexpected PlaytestRunRequest: %+v", gotReq)
	}
	// Report must be written to disk in canonical YAML.
	if _, err := playtest.LoadReportFile(filepath.Join(f.dir, "reports", "room.report.yaml")); err != nil {
		t.Fatalf("saved report did not parse: %v", err)
	}

	// Unknown fields and paths escaping project must be refused.
	if bad := call(t, cs, editormcp.ToolGamePlaytest, map[string]any{"bogus": 1}); !bad.IsError || !strings.Contains(text(bad), "unknown field") {
		t.Fatalf("expected unknown field error, got %v %q", bad.IsError, text(bad))
	}
	if bad := call(t, cs, editormcp.ToolGamePlaytest, map[string]any{"file": "../escape.yaml"}); !bad.IsError || !strings.Contains(text(bad), "leaves the project") {
		t.Fatalf("expected escape error, got %v %q", bad.IsError, text(bad))
	}
}

func TestPlaytestDefineArrivesAsReview(t *testing.T) {
	f := openFixture(t)
	ptPath := filepath.Join(f.dir, playtest.FileName)
	if err := os.WriteFile(ptPath, []byte(fixturePlaytestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolPlaytestDefine, map[string]any{
		"rationale": `from "dies 2-4 times on floor 3"`,
		"metric": map[string]any{
			"name":  "deaths_per_run",
			"label": "Deaths per run",
			"measure": map[string]any{
				"kind":      "event",
				"key":       "death",
				"aggregate": "mean",
				"heatMap":   true,
			},
			"goal": map[string]any{
				"summary": "dies 2-4 times on floor 3",
				"source":  "brief.yaml",
				"min":     2,
				"max":     4,
			},
		},
	})
	out := mustOK(t, res)
	var got editormcp.PlaytestDefineResult
	structured(t, res, &got)

	if got.Status != "pending_review" || got.Review.ID == "" || got.Review.Status != "pending" || got.Review.File != "playtest.yaml" {
		t.Fatalf("PlaytestDefineResult = %+v\n%s", got, out)
	}
	if !strings.Contains(got.Review.Diff, "+  - name: deaths_per_run") || !strings.Contains(out, "pending review") {
		t.Fatalf("diff/text missing proposed metric:\n%s", out)
	}

	// Disk MUST remain unchanged until the person accepts the review.
	onDisk, err := os.ReadFile(ptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != fixturePlaytestYAML {
		t.Fatalf("playtest_define modified playtest.yaml on disk before review acceptance")
	}

	// Duplicate metric without replace: true is refused.
	if dup := call(t, cs, editormcp.ToolPlaytestDefine, map[string]any{
		"metric": map[string]any{
			"name":    "finished",
			"measure": map[string]any{"kind": "outcome", "key": "finished"},
		},
	}); !dup.IsError || !strings.Contains(text(dup), "already defines metric") {
		t.Fatalf("duplicate metric should fail: %v %q", dup.IsError, text(dup))
	}

	// Applying the review writes canonical YAML that parses cleanly with the new metric.
	if err := s.ApplyPlaytestReview(got.Review.ID); err != nil {
		t.Fatalf("ApplyPlaytestReview: %v", err)
	}
	cfg, err := playtest.LoadConfigFile(ptPath)
	if err != nil {
		t.Fatalf("LoadConfigFile after apply: %v", err)
	}
	if len(cfg.Metrics) != 2 || cfg.Metrics[1].Name != "deaths_per_run" || !cfg.Metrics[1].Measure.HeatMap {
		t.Fatalf("applied metrics = %+v", cfg.Metrics)
	}

	// Applying the same review twice fails.
	if err := s.ApplyPlaytestReview(got.Review.ID); err == nil {
		t.Fatal("applying an already-applied review succeeded")
	}

	// Proposing another review and modifying the file externally causes ApplyPlaytestReview to refuse stale apply.
	res2 := call(t, cs, editormcp.ToolPlaytestDefine, map[string]any{
		"metric": map[string]any{
			"name":    "frame_time",
			"unit":    "ms",
			"measure": map[string]any{"kind": "perf", "key": "frame_ms"},
			"goal":    map[string]any{"source": "game.yaml", "max": 16.7},
		},
	})
	mustOK(t, res2)
	var got2 editormcp.PlaytestDefineResult
	structured(t, res2, &got2)

	if err := os.WriteFile(ptPath, []byte(fixturePlaytestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyPlaytestReview(got2.Review.ID); err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("expected stale disk error, got %v", err)
	}
	if err := s.DismissPlaytestReview(got2.Review.ID); err != nil {
		t.Fatalf("DismissPlaytestReview: %v", err)
	}
	reviews := s.PlaytestReviews()
	if len(reviews) != 2 || reviews[0].Status != "applied" || reviews[1].Status != "dismissed" {
		t.Fatalf("PlaytestReviews = %+v", reviews)
	}
}
