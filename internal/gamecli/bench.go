package gamecli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/nsgo-dev/game/pkg/tooling/bench"
)

// benchCommand runs the benchmarks headless and checks them against a stored baseline, or records the baseline.
var benchCommand = &command{
	name:    "bench",
	args:    "[directory ...]",
	summary: "Run the benchmarks headless and fail when one regresses past its stored baseline, or update the baseline",
	define:  defineBench,
	complete: map[string]completion{
		"baseline": filePaths,
		"":         filePaths,
	},
}

func defineBench(a *app, fs *flag.FlagSet) func() error {
	baselinePath := fs.String("baseline", "benchmarks/baseline.yaml", "the baseline `file` to check against, or to write with -update")
	pattern := fs.String("bench", ".", "run the benchmarks matching this `regexp`, as go test's -bench")
	benchtime := fs.String("benchtime", "200ms", "run each benchmark for this `duration`, or Nx times, as go test's -benchtime")
	count := fs.Int("count", 3, "run the benchmarks this many `times` and compare each one's median")
	update := fs.Bool("update", false, "write the measured medians to the baseline instead of checking them")
	strict := fs.Bool("strict", false, "fail when a measured benchmark has no baseline entry")
	inPath := inFlag(fs, "", "read benchmark results from this `file` of go test -bench output instead of running the benchmarks")

	return func() error {
		switch {
		case *baselinePath == "":
			return usagef("-baseline file is required")
		case *inPath != "" && fs.NArg() > 0:
			return usagef("-in reads results instead of running the benchmarks in %q", fs.Args())
		case *inPath == "" && *pattern == "":
			return usagef("-bench must match the benchmarks to run")
		case *inPath == "" && *benchtime == "":
			return usagef("-benchtime must not be empty")
		case *count < 1:
			return usagef("-count must be at least 1, not %d", *count)
		}

		runBenchmarks := func() ([]byte, error) {
			dirs := fs.Args()
			if len(dirs) == 0 {
				dirs = []string{"./..."}
			}
			// nsgo test prints its results, a benchmark's included, on standard error, so both streams are read.
			var out nsgoOutput
			for _, dir := range dirs {
				// nsgo test v0.3.0 prints only the first result of a benchmark run with -count above 1, because go test -json attributes the later results to the package rather than the benchmark, so each run is a process of its own with -count=1 (nsgo-dev/nsgo#2273).
				for range *count {
					args := []string{"test", "-bench", *pattern, dir, "-benchmem", "-benchtime=" + *benchtime, "-count=1"}
					if err := out.run(a, args); err != nil {
						return nil, fmt.Errorf("benchmarks in %s failed: %w", dir, err)
					}
				}
			}
			return out.bytes(), nil
		}

		var results []byte
		if *inPath != "" {
			data, err := os.ReadFile(*inPath)
			if err != nil {
				return err
			}
			results = data
		} else {
			data, err := runBenchmarks()
			if err != nil {
				return err
			}
			results = data
		}

		measured, err := bench.ParseOutput(bytes.NewReader(results))
		if err != nil {
			return err
		}
		if len(measured) == 0 {
			if *inPath != "" {
				return fmt.Errorf("no benchmark results in %s", *inPath)
			}
			return fmt.Errorf("no benchmark results: -bench %q matched no benchmark", *pattern)
		}

		b, err := bench.LoadFile(*baselinePath)
		if *update {
			if errors.Is(err, os.ErrNotExist) {
				b, err = bench.NewBaseline(), nil
			}
			if err != nil {
				return err
			}
			b.Update(measured)
			if err := bench.SaveFile(*baselinePath, b); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "game bench: updated %s with %d benchmarks\n", *baselinePath, len(measured))
			return nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w; record a baseline with -update", err)
		}
		if err != nil {
			return err
		}
		if len(b.Benchmarks) == 0 {
			return fmt.Errorf("%s holds no benchmarks; record them with -update", *baselinePath)
		}
		rep := bench.Evaluate(b, measured)
		if *strict {
			rep = bench.EvaluateStrict(b, measured)
		}
		if *inPath == "" && onlyTimingRegressions(rep) {
			fmt.Fprint(a.stdout, rep)
			fmt.Fprintln(a.stdout, "game bench: retrying ns/op-only regressions once with unchanged B/op and allocs/op")
			results, err := runBenchmarks()
			if err != nil {
				return err
			}
			measured, err = bench.ParseOutput(bytes.NewReader(results))
			if err != nil {
				return err
			}
			if len(measured) == 0 {
				return fmt.Errorf("no benchmark results on retry: -bench %q matched no benchmark", *pattern)
			}
			rep = bench.Evaluate(b, measured)
			if rep.Passed() {
				fmt.Fprintln(a.stdout, "game bench: ns/op-only regression did not reproduce on retry; treating the first result as a warning")
			}
		}
		fmt.Fprint(a.stdout, rep)
		if !rep.Passed() {
			return fmt.Errorf("%d of %d benchmarks regressed or were not measured", rep.Failed(), len(rep.Results))
		}
		return nil
	}
}

// onlyTimingRegressions reports whether every failure is a ns/op regression with byte-for-byte unchanged allocation metrics. Such failures get one independent retry; missing benchmarks and allocation regressions fail immediately.
func onlyTimingRegressions(rep bench.Report) bool {
	found := false
	for _, res := range rep.Results {
		if res.Passed() {
			continue
		}
		if res.Missing || len(res.Regressions) != 1 || res.Delta.NsPerOp <= rep.Limits.NsPerOp || res.Measured.BytesPerOp != res.Baseline.BytesPerOp || res.Measured.AllocsPerOp != res.Baseline.AllocsPerOp {
			return false
		}
		found = true
	}
	return found
}
