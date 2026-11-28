package playtest_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

// TestApplyBriefTargetStatusesKeepsUnmeasuredPending checks that a brief target without a measured metric stays pending and that a measured one is judged by the brief's own bounds, not the playtest.yaml goal the metric carries.
func TestApplyBriefTargetStatusesKeepsUnmeasuredPending(t *testing.T) {
	b := &project.Brief{Version: project.BriefVersion, Feel: &project.BriefFeel{Targets: []project.BriefPlaytestTarget{
		{Metric: "finished", Scene: "s", Basis: "target", Equal: project.Float64(1)},
		{Metric: "deaths", Scene: "s", Basis: "assumption", Max: project.Float64(3)},
		{Metric: "median_time", Scene: "s", Basis: "locked", Max: project.Float64(30)},
		{Metric: "finished", Scene: "other", Equal: project.Float64(1)},
	}}}
	r := &playtest.Report{Version: playtest.ReportVersion, Scope: playtest.Scope{Scene: "s"}, Metrics: []playtest.MetricReport{
		{Name: "finished", Status: playtest.StatusMet, Aggregate: playtest.AggCount, Value: 1},
		// playtest.yaml's own goal (max 60) passes, but the brief's max 30 does not.
		{Name: "median_time", Status: playtest.StatusMet, Aggregate: playtest.AggMedian, Value: 45, Goal: &playtest.Goal{Max: project.Float64(60), Source: "playtest.yaml"}},
	}}
	playtest.ApplyBriefTargetStatuses(r, b)
	want := []playtest.BriefTargetReport{
		{Metric: "deaths", Scene: "s", Basis: "assumption", Status: playtest.BriefTargetPending},
		{Metric: "finished", Scene: "s", Basis: "target", Status: playtest.BriefTargetMet},
		{Metric: "median_time", Scene: "s", Basis: "locked", Status: playtest.BriefTargetMissed},
	}
	if !slices.Equal(r.BriefTargets, want) {
		t.Fatalf("brief targets = %+v, want %+v", r.BriefTargets, want)
	}
}

// TestRunTestListsBriefTargetsBesidePlaytestConfig checks that RunTest reads the brief.yaml beside playtest.yaml and writes every brief target into the report file, an unmeasured one as pending.
func TestRunTestListsBriefTargetsBesidePlaytestConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := fixturePlatformerConfig()
	cfg.Runs.Count = 2
	cfg.Metrics = cfg.Metrics[:3]
	cfgPath := filepath.Join(dir, playtest.FileName)
	if err := playtest.SaveConfigFile(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	brief, err := project.EncodeBrief(&project.Brief{Version: project.BriefVersion, Feel: &project.BriefFeel{Targets: []project.BriefPlaytestTarget{
		{Metric: "finished", Scene: cfg.Scope.Scene, Basis: "target", Min: project.Float64(0)},
		{Metric: "jumps_per_minute", Scene: cfg.Scope.Scene, Basis: "assumption", Max: project.Float64(90)},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.BriefFileName), brief, 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "report.yaml")
	t.Setenv(playtest.EnvConfig, cfgPath)
	t.Setenv(playtest.EnvOut, outPath)

	playtest.RunTest(t, "", makeFixtureSim)
	loaded, err := playtest.LoadReportFile(outPath)
	if err != nil {
		t.Fatalf("LoadReportFile: %v", err)
	}
	want := []playtest.BriefTargetReport{
		{Metric: "finished", Scene: cfg.Scope.Scene, Basis: "target", Status: playtest.BriefTargetMet},
		{Metric: "jumps_per_minute", Scene: cfg.Scope.Scene, Basis: "assumption", Status: playtest.BriefTargetPending},
	}
	if !slices.Equal(loaded.BriefTargets, want) {
		t.Fatalf("report briefTargets = %+v, want %+v", loaded.BriefTargets, want)
	}
}
