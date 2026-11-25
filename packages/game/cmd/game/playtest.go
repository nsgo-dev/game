package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/playtest"
)

// playtestCommand validates playtest.yaml, runs the playtest headless through its tests, records per-seed input recordings and writes a YAML report.
var playtestCommand = &command{
	name:    "playtest",
	args:    "[directory ...]",
	summary: "Validate playtest.yaml, run headless bot playtests with replays, write a YAML report and fail on missed goals",
	define:  definePlaytest,
	complete: map[string]completion{
		"config":     filePaths,
		"recordings": filePaths,
		"":           filePaths,
	},
}

func definePlaytest(a *app, fs *flag.FlagSet) func() error {
	cfgPath := fs.String("config", playtest.FileName, "the playtest configuration `file` to validate and run")
	outPath := outFlag(fs, playtest.ReportFileName, "write the YAML playtest report to this `file`")
	recDir := fs.String("recordings", "playtest-recordings", "write per-seed .replay.yaml recordings into this `directory` (empty skips writing files)")
	pattern := fs.String("run", "Playtest", "run the tests matching this `regexp`, as go test's -run: the tests that call playtest.RunTest")
	count := fs.Int("count", 0, "override runs.count in playtest.yaml when positive")
	seeds := fs.String("seeds", "", "override runs.seeds with a comma-separated list of uint64 `seeds`")
	checkOnly := fs.Bool("check", false, "validate playtest.yaml without running any tests")

	return func() error {
		switch {
		case *cfgPath == "":
			return usagef("-config file is required")
		case *count < 0:
			return usagef("-count must be 0 or more, not %d", *count)
		case !*checkOnly && *outPath == "":
			return usagef("-out report file is required")
		case !*checkOnly && *pattern == "":
			return usagef("-run must name the tests that run the playtest")
		}
		if *seeds != "" {
			for p := range strings.SplitSeq(*seeds, ",") {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if _, err := strconv.ParseUint(p, 10, 64); err != nil {
					return usagef("-seeds contains invalid seed %q", p)
				}
			}
		}

		cfg, err := playtest.LoadConfigFile(*cfgPath)
		if err != nil {
			return err
		}
		if *checkOnly {
			fmt.Fprintf(a.stdout, "game playtest: %s is valid (%d runs, bot %s, %d metrics)\n", *cfgPath, len(cfg.Runs.ResolveSeeds()), cfg.Bot.Profile, len(cfg.Metrics))
			return nil
		}

		absCfg, err := filepath.Abs(*cfgPath)
		if err != nil {
			return err
		}
		absOut, err := filepath.Abs(*outPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil {
			return err
		}
		// Remove any pre-existing report so a passing test that does not call playtest.RunTest cannot silently reuse a stale report.
		if err := os.Remove(absOut); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		env := []string{
			playtest.EnvConfig + "=" + absCfg,
			playtest.EnvOut + "=" + absOut,
			playtest.EnvNoFail + "=1",
		}
		if *recDir != "" {
			absRec, err := filepath.Abs(*recDir)
			if err != nil {
				return err
			}
			env = append(env, playtest.EnvRecordings+"="+absRec)
		}
		if *count > 0 {
			env = append(env, fmt.Sprintf("%s=%d", playtest.EnvCount, *count))
		}
		if *seeds != "" {
			env = append(env, playtest.EnvSeeds+"="+*seeds)
		}

		dirs := fs.Args()
		if len(dirs) == 0 {
			dirs = []string{"./..."}
		}

		fmt.Fprintf(a.stdout, "game playtest: running %s (%s) through tests matching %q\n", *cfgPath, cfg.Bot.Profile, *pattern)
		for _, dir := range dirs {
			var out nsgoOutput
			if err := out.runEnv(a, env, []string{"test", "-run", *pattern, dir, "-count=1"}); err != nil {
				return fmt.Errorf("playtest tests in %s failed: %w", dir, err)
			}
			passed, ok := passedTests(out.bytes())
			switch {
			case !ok:
				return fmt.Errorf("playtest tests in %s printed no tally of passed tests", dir)
			case passed == 0:
				return fmt.Errorf("no test in %s matching -run %q passed, so no playtest ran", dir, *pattern)
			}
		}

		rep, err := playtest.LoadReportFile(absOut)
		if err != nil {
			return fmt.Errorf("playtest tests ran but wrote no valid report to %s: %w", *outPath, err)
		}
		fmt.Fprint(a.stdout, rep.Summary())
		if !rep.Passed {
			missed := rep.MissedMetrics()
			parts := make([]string, 0, len(missed))
			for _, m := range missed {
				parts = append(parts, fmt.Sprintf("%s=%g (goal %s, %s)", m.Name, m.Value, m.Goal.Describe(m.Unit), m.Goal.Source))
			}
			return fmt.Errorf("%d playtest goal(s) missed: %s", len(missed), strings.Join(parts, "; "))
		}
		return nil
	}
}
