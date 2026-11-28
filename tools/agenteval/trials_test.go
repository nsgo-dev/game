package main

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAggregateTaskReports(t *testing.T) {
	got := AggregateTaskReports([]TaskReport{
		{ID: "x", Score: 100, Passed: true, DurationMS: 10, AgentTurns: 1, AgentPromptTokens: 5, Criteria: []CriterionResult{{Name: "ok", Passed: true}}},
		{ID: "x", Score: 80, Passed: false, DurationMS: 20, AgentTurns: 2, AgentPromptTokens: 7, AgentTimedOut: true, Criteria: []CriterionResult{{Name: "broken", Passed: false}}},
		{ID: "x", Score: 90, Passed: true, DurationMS: 30, AgentTurns: 3, AgentPromptTokens: 9},
	})
	if got.Trials != 3 || got.PassRate != 2.0/3.0 || got.MeanScore != 90 || got.Score != 90 || math.Abs(got.StdDev-math.Sqrt(200.0/3.0)) > 1e-9 {
		t.Fatalf("aggregate = %+v", got)
	}
	if got.Passed {
		t.Fatal("partial pass rate should not pass")
	}
	if len(got.Criteria) != 1 || got.Criteria[0].Name != "broken" || !got.AgentTimedOut {
		t.Fatalf("criteria should come from the failing trial: %+v", got)
	}
	if got.DurationMS != 60 || got.AgentTurns != 6 || got.AgentPromptTokens != 21 {
		t.Fatalf("duration and agent counters should be summed: %+v", got)
	}
}

func TestAggregateTaskReportsSingleTrial(t *testing.T) {
	got := AggregateTaskReports([]TaskReport{{ID: "x", Score: 75, Passed: true, DurationMS: 4}})
	if got.Trials != 1 || got.PassRate != 1 || got.MeanScore != 75 || got.StdDev != 0 || got.Score != 75 || !got.Passed || got.DurationMS != 4 {
		t.Fatalf("single trial aggregate = %+v", got)
	}
}

func TestRenderMarkdownShowsTrialStatistics(t *testing.T) {
	rep := BuildSuiteReport(ModeReference, []TaskReport{AggregateTaskReports([]TaskReport{{ID: "x", Score: 100, Passed: true}, {ID: "x", Score: 50}})})
	md := rep.RenderMarkdown()
	if !strings.Contains(md, "2 trials: pass rate 50%, mean score 75.0, standard deviation 25.0") {
		t.Fatalf("markdown lacks trial statistics:\n%s", md)
	}
}

// TestCommandModeTrialsRunInFreshWorkspaces fails the second trial if it sees the marker the first trial left, so it passes only when every trial is staged anew.
func TestCommandModeTrialsRunInFreshWorkspaces(t *testing.T) {
	root := testRepoRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	ref := filepath.Join(root, "tools", "agenteval", "testdata", "fixtures", "add-menu", "reference")
	report, err := RunSuite(ctx, RunOptions{
		RepoRoot:         root,
		TasksDir:         filepath.Join(root, "tools", "agenteval", "tasks"),
		Mode:             ModeCommand,
		AgentCmd:         `test ! -e .trial-marker && touch .trial-marker && cp -R '` + ref + `/.' .`,
		TaskFilter:       []string{"add-menu"},
		MinScoreOverride: 100,
		Timeout:          2 * time.Minute,
		Trials:           2,
	})
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	tr := report.Tasks[0]
	if tr.Trials != 2 || tr.PassRate != 1 || !tr.Passed || tr.StdDev != 0 {
		t.Fatalf("trials did not run in fresh workspaces:\n%s", report.RenderMarkdown())
	}
}
