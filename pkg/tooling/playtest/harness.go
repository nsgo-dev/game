package playtest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
)

// Environment variables passed by `go tool game playtest` and `game_playtest` to a game's playtest tests.
const (
	// EnvConfig overrides the path to playtest.yaml loaded by RunTest.
	EnvConfig = "NSGO_PLAYTEST_CONFIG"
	// EnvOut tells RunTest where to write the YAML playtest report.
	EnvOut = "NSGO_PLAYTEST_OUT"
	// EnvRecordings tells RunTest which directory to write per-run .replay.yaml recordings into.
	EnvRecordings = "NSGO_PLAYTEST_RECORDINGS"
	// EnvCount, when set to a positive integer, overrides runs.count in playtest.yaml.
	EnvCount = "NSGO_PLAYTEST_COUNT"
	// EnvSeeds, when set to a comma-separated list of uint64 seeds, overrides runs.seeds in playtest.yaml.
	EnvSeeds = "NSGO_PLAYTEST_SEEDS"
	// EnvNoFail, when set to "1", lets RunTest write its report without failing testing.TB on missed goals, so the caller (such as go tool game playtest) can inspect and report missed goals directly.
	EnvNoFail = "NSGO_PLAYTEST_NO_FAIL"
)

// RunTest loads playtest.yaml, runs the playtest against makeSim, lists the feel targets of the brief.yaml beside playtest.yaml in the report (pending when unmeasured), writes the report and recordings when requested via environment variables (as `go tool game playtest` sets them), and fails t when any metric goal is missed (unless NSGO_PLAYTEST_NO_FAIL=1).
func RunTest(t testing.TB, defaultConfigPath string, makeSim SimFactory, opts ...RunOptions) *RunResult {
	t.Helper()
	cfgPath := os.Getenv(EnvConfig)
	if cfgPath == "" {
		cfgPath = defaultConfigPath
	}
	if cfgPath == "" {
		cfgPath = FileName
	}
	cfg, err := LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("playtest.RunTest: %v", err)
	}
	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("playtest.RunTest: %v", err)
	}

	var opt RunOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if dir := os.Getenv(EnvRecordings); dir != "" {
		opt.RecordingsDir = dir
	}

	res, err := Run(cfg, makeSim, opt)
	if err != nil {
		t.Fatalf("playtest.RunTest: %v", err)
	}
	if err := applyBriefBeside(cfgPath, res.Report); err != nil {
		t.Fatalf("playtest.RunTest: %v", err)
	}

	if outPath := os.Getenv(EnvOut); outPath != "" {
		if err := SaveReportFile(outPath, res.Report); err != nil {
			t.Fatalf("playtest.RunTest: %v", err)
		}
	}

	if !res.Report.Passed && os.Getenv(EnvNoFail) != "1" {
		t.Fatalf("playtest goals missed:\n%s", res.Report.Summary())
	}
	return res
}

// applyBriefBeside lists in rep the feel targets of the brief.yaml in the directory of the playtest config at cfgPath, the project's root, with ApplyBriefTargetStatuses. A project without brief.yaml leaves rep unchanged.
func applyBriefBeside(cfgPath string, rep *Report) error {
	b, err := project.LoadBrief(filepath.Join(filepath.Dir(cfgPath), project.BriefFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	ApplyBriefTargetStatuses(rep, b)
	return ValidateReport(rep)
}

func applyEnvOverrides(cfg *Config) error {
	if raw := strings.TrimSpace(os.Getenv(EnvSeeds)); raw != "" {
		parts := strings.Split(raw, ",")
		seeds := make([]uint64, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			v, err := strconv.ParseUint(p, 10, 64)
			if err != nil {
				return fmt.Errorf("%s: invalid seed %q: %w", EnvSeeds, p, err)
			}
			seeds = append(seeds, v)
		}
		if len(seeds) == 0 {
			return fmt.Errorf("%s: no seeds provided", EnvSeeds)
		}
		cfg.Runs.Seeds = seeds
		cfg.Runs.Count = len(seeds)
	}
	if raw := strings.TrimSpace(os.Getenv(EnvCount)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return fmt.Errorf("%s: invalid count %q (must be positive integer)", EnvCount, raw)
		}
		cfg.Runs.Count = n
	}
	return nil
}
