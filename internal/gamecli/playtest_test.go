package gamecli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

func copyPlaytestFixture(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "pkg", "tooling", "playtest", "testdata", "platformer.playtest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, filepath.Join(dir, playtest.FileName), string(data))
}

func TestPlaytestCheckOnly(t *testing.T) {
	dir := t.TempDir()
	cfgPath := copyPlaytestFixture(t, dir)

	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"playtest", "-check", "-config", cfgPath}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "is valid (20 runs, bot platformer-runner") {
		t.Fatalf("stdout = %q", stdout)
	}

	badPath := writeFile(t, filepath.Join(dir, "bad.yaml"), "version: 1\nunknownKey: true\n")
	a, _, stderr = newTestApp(t)
	if code := run([]string{"playtest", "-check", "-config", badPath}, a); code != 1 {
		t.Fatalf("bad config exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "unknownKey") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestPlaytestRunsTestsAndEvaluatesReport(t *testing.T) {
	dir := t.TempDir()
	cfgPath := copyPlaytestFixture(t, dir)
	outPath := filepath.Join(dir, "playtest.report.yaml")
	recDir := filepath.Join(dir, "recs")

	passingReport := &playtest.Report{
		Version:   playtest.ReportVersion,
		Scope:     playtest.Scope{Project: "dungeon-run", Scene: "level_01"},
		Bot:       playtest.BotConfig{Profile: playtest.ProfilePlatformerRunner},
		Rate:      60,
		Speed:     1,
		SeedCount: 1,
		Passed:    true,
		Metrics: []playtest.MetricReport{
			{
				Name:      "finished",
				Label:     "Finished",
				Aggregate: playtest.AggCount,
				Value:     1,
				Goal:      &playtest.Goal{Source: "brief.yaml", Min: playtest.Float64(1)},
				Status:    playtest.StatusMet,
			},
		},
		Runs: []playtest.RunReport{
			{Index: 1, Seed: 1, Outcome: playtest.OutcomeFinished, Steps: 60, DurationSeconds: 1},
		},
	}

	var gotEnv []string
	var gotArgs []string
	a, stdout, stderr := newTestApp(t)
	a.nsgoEnv = func(env []string, args []string, _, errOut io.Writer) error {
		gotEnv = append([]string(nil), env...)
		gotArgs = append([]string(nil), args...)
		if err := playtest.SaveReportFile(outPath, passingReport); err != nil {
			return err
		}
		_, _ = io.WriteString(errOut, passingTally)
		return nil
	}

	if code := run([]string{"playtest", "-config", cfgPath, "-out", outPath, "-recordings", recDir, "-count", "5", "-seeds", "1,2", "./levels"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if strings.Join(gotArgs, " ") != "test -run Playtest ./levels -count=1" {
		t.Fatalf("nsgo args = %q", gotArgs)
	}
	envJoined := strings.Join(gotEnv, "\n")
	for _, want := range []string{
		playtest.EnvConfig + "=",
		playtest.EnvOut + "=",
		playtest.EnvRecordings + "=",
		playtest.EnvNoFail + "=1",
		playtest.EnvCount + "=5",
		playtest.EnvSeeds + "=1,2",
	} {
		if !strings.Contains(envJoined, want) {
			t.Errorf("env missing %q:\n%s", want, envJoined)
		}
	}
	if !strings.Contains(stdout.String(), "[MET] Finished = 1") || !strings.Contains(stdout.String(), "Result: PASSED") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestPlaytestFailsWhenGoalMissedOrNoTestMatched(t *testing.T) {
	dir := t.TempDir()
	cfgPath := copyPlaytestFixture(t, dir)
	outPath := filepath.Join(dir, "missed.report.yaml")

	missedReport := &playtest.Report{
		Version:   playtest.ReportVersion,
		Scope:     playtest.Scope{Project: "dungeon-run", Scene: "level_01"},
		Bot:       playtest.BotConfig{Profile: playtest.ProfilePlatformerRunner},
		Rate:      60,
		Speed:     1,
		SeedCount: 1,
		Passed:    false,
		Metrics: []playtest.MetricReport{
			{
				Name:      "coins_taken",
				Label:     "Coins taken",
				Unit:      "%",
				Aggregate: playtest.AggMean,
				Value:     71,
				Goal:      &playtest.Goal{Summary: "every coin on a route", Source: "playtest.yaml", Min: playtest.Float64(100)},
				Status:    playtest.StatusMissed,
			},
		},
		Runs: []playtest.RunReport{
			{Index: 1, Seed: 1, Outcome: playtest.OutcomeFinished, Steps: 60, DurationSeconds: 1},
		},
	}

	a, stdout, stderr := newTestApp(t)
	a.nsgoEnv = func(_ []string, _ []string, _, errOut io.Writer) error {
		if err := playtest.SaveReportFile(outPath, missedReport); err != nil {
			return err
		}
		_, _ = io.WriteString(errOut, passingTally)
		return nil
	}
	if code := run([]string{"playtest", "-config", cfgPath, "-out", outPath}, a); code != 1 {
		t.Fatalf("missed goal exit = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "[MISSED] Coins taken = 71%") || !strings.Contains(stderr.String(), "coins_taken=71") {
		t.Fatalf("stdout=%q\nstderr=%q", stdout, stderr)
	}

	// Zero matched tests must fail even when nsgo test returns 0.
	a, _, stderr = newTestApp(t)
	a.nsgoEnv = func(_ []string, _ []string, _, errOut io.Writer) error {
		_, _ = io.WriteString(errOut, "  0 passed, 0 failed, 0 skipped\n")
		return nil
	}
	if code := run([]string{"playtest", "-config", cfgPath, "-out", outPath}, a); code != 1 {
		t.Fatalf("zero tests exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no test") {
		t.Fatalf("stderr = %q", stderr)
	}

	// A pre-existing report file must not be reused when a passing test writes no new report.
	if err := playtest.SaveReportFile(outPath, missedReport); err != nil {
		t.Fatal(err)
	}
	a, _, stderr = newTestApp(t)
	a.nsgoEnv = func(_ []string, _ []string, _, errOut io.Writer) error {
		_, _ = io.WriteString(errOut, passingTally)
		return nil
	}
	if code := run([]string{"playtest", "-config", cfgPath, "-out", outPath}, a); code != 1 {
		t.Fatalf("stale report exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "wrote no valid report") {
		t.Fatalf("stderr = %q, want missing-report error", stderr)
	}
}
