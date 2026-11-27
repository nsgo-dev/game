package gamecli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testBaseline = `version: 1
max_regression_pct: {ns_per_op: 25, bytes_per_op: 25, allocs_per_op: 25}
benchmarks:
  BenchmarkDraw: {ns_per_op: 1000, bytes_per_op: 64, allocs_per_op: 2}
  BenchmarkLoad: {ns_per_op: 500, bytes_per_op: 0, allocs_per_op: 0}
`

// fakeBench replaces a's nsgo with one that prints, on its nth call, nsgo test's line for each benchmark in results with the nth of its ns/op values, and records the calls. Like nsgo test, it prints them on standard error.
func fakeBench(a *app, results map[string][]float64) *[][]string {
	var calls [][]string
	a.nsgo = func(args []string, _, stdout io.Writer) error {
		n := len(calls)
		calls = append(calls, args)
		for _, name := range []string{"BenchmarkDraw", "BenchmarkLoad", "BenchmarkNew"} {
			ns, ok := results[name]
			if !ok {
				continue
			}
			var bytes, allocs int
			if name == "BenchmarkDraw" {
				bytes, allocs = 64, 2
			}
			fmt.Fprintf(stdout, "  ✓ %s-8 1000 %g ns/op %d B/op %d allocs/op\n", name, ns[n%len(ns)], bytes, allocs)
		}
		fmt.Fprintln(stdout, "  2 passed, 0 failed, 0 skipped")
		return nil
	}
	return &calls
}

func TestBenchPassesWithinTheBaseline(t *testing.T) {
	baseline := writeFile(t, filepath.Join(t.TempDir(), "baseline.yaml"), testBaseline)
	a, stdout, stderr := newTestApp(t)
	calls := fakeBench(a, map[string][]float64{"BenchmarkDraw": {1100, 5000, 1200}, "BenchmarkLoad": {400}})
	if code := run([]string{"bench", "-baseline", baseline, "./sprite", "./tilemap"}, a); code != 0 {
		t.Fatalf("exit %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	run := []string{"-benchmem", "-benchtime=200ms", "-count=1"}
	var want [][]string
	for _, dir := range []string{"./sprite", "./tilemap"} {
		for range 3 {
			want = append(want, append([]string{"test", "-bench", ".", dir}, run...))
		}
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("nsgo calls = %q, want %q", *calls, want)
	}
	// The median of each benchmark over every run is compared, so the one slow run of BenchmarkDraw does not fail it.
	out := stdout.String()
	if !strings.Contains(stderr.String(), "✓ BenchmarkDraw-8 1000 5000 ns/op") {
		t.Errorf("nsgo test's output is not passed on:\n%s", stderr)
	}
	for _, line := range []string{"[PASS] BenchmarkDraw", "ns/op:      1200.0", "[PASS] BenchmarkLoad"} {
		if !strings.Contains(out, line) {
			t.Errorf("stdout does not contain %q:\n%s", line, out)
		}
	}
	data, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != testBaseline {
		t.Fatalf("a check changed the baseline:\n%s", data)
	}
}

func TestBenchReadsResultsFromBothStreams(t *testing.T) {
	baseline := writeFile(t, filepath.Join(t.TempDir(), "baseline.yaml"), testBaseline)
	a, stdout, stderr := newTestApp(t)
	a.nsgo = func(_ []string, out, errOut io.Writer) error {
		fmt.Fprintln(out, "BenchmarkDraw-8 1000 1000 ns/op 64 B/op 2 allocs/op")
		fmt.Fprintln(errOut, "  ✓ BenchmarkLoad-8 1000 500 ns/op 0 B/op 0 allocs/op")
		return nil
	}
	if code := run([]string{"bench", "-baseline", baseline, "-count", "1", "./sprite"}, a); code != 0 {
		t.Fatalf("exit %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	for _, line := range []string{"[PASS] BenchmarkDraw", "[PASS] BenchmarkLoad"} {
		if !strings.Contains(stdout.String(), line) {
			t.Errorf("stdout does not contain %q:\n%s", line, stdout)
		}
	}
}

func TestBenchFailsOnARegression(t *testing.T) {
	baseline := writeFile(t, filepath.Join(t.TempDir(), "baseline.yaml"), testBaseline)
	a, stdout, stderr := newTestApp(t)
	calls := fakeBench(a, map[string][]float64{"BenchmarkDraw": {1600}})
	if code := run([]string{"bench", "-baseline", baseline, "-bench", "Draw|Load", "-benchtime", "10x", "-count", "2", "./sprite"}, a); code != 1 {
		t.Fatalf("exit %d, want 1 (stdout: %s)", code, stdout)
	}
	if len(*calls) != 2 || (*calls)[0][2] != "Draw|Load" || (*calls)[0][5] != "-benchtime=10x" {
		t.Fatalf("nsgo calls = %q", *calls)
	}
	for _, line := range []string{"[FAIL] BenchmarkDraw", "ns/op regressed by +60.0%", "[FAIL] BenchmarkLoad: not measured"} {
		if !strings.Contains(stdout.String(), line) {
			t.Errorf("stdout does not contain %q:\n%s", line, stdout)
		}
	}
	if !strings.Contains(stderr.String(), "game bench: 2 of 2 benchmarks regressed or were not measured") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestBenchUpdateWritesAndMergesTheBaseline(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "benchmarks", "baseline.yaml")
	a, stdout, stderr := newTestApp(t)
	fakeBench(a, map[string][]float64{"BenchmarkDraw": {900}, "BenchmarkLoad": {450}})
	if code := run([]string{"bench", "-baseline", baseline, "-count", "1", "-update"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "game bench: updated "+baseline+" with 2 benchmarks") {
		t.Fatalf("stdout = %q", stdout)
	}
	want := `version: 1
max_regression_pct: {ns_per_op: 25, bytes_per_op: 25, allocs_per_op: 25}
benchmarks:
  BenchmarkDraw: {ns_per_op: 900, bytes_per_op: 64, allocs_per_op: 2}
  BenchmarkLoad: {ns_per_op: 450, bytes_per_op: 0, allocs_per_op: 0}
`
	if data, err := os.ReadFile(baseline); err != nil || string(data) != want {
		t.Fatalf("baseline = %q, %v; want %q", data, err, want)
	}

	a, _, stderr = newTestApp(t)
	fakeBench(a, map[string][]float64{"BenchmarkNew": {7}})
	if code := run([]string{"bench", "-baseline", baseline, "-count", "1", "-update", "./new"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	want += "  BenchmarkNew: {ns_per_op: 7, bytes_per_op: 0, allocs_per_op: 0}\n"
	if data, err := os.ReadFile(baseline); err != nil || string(data) != want {
		t.Fatalf("merged baseline = %q, %v; want %q", data, err, want)
	}
}

func TestBenchReadsResultsFromAFile(t *testing.T) {
	dir := t.TempDir()
	baseline := writeFile(t, filepath.Join(dir, "baseline.yaml"), testBaseline)
	results := writeFile(t, filepath.Join(dir, "bench.txt"), "goos: linux\nBenchmarkDraw-8 100 1000 ns/op 64 B/op 2 allocs/op\nBenchmarkLoad-8 100 510 ns/op 0 B/op 0 allocs/op\nPASS\n")
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"bench", "-baseline", baseline, "-in", results}, a); code != 0 {
		t.Fatalf("exit %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stdout.String(), "[PASS] BenchmarkLoad") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestBenchErrors(t *testing.T) {
	dir := t.TempDir()
	baseline := writeFile(t, filepath.Join(dir, "baseline.yaml"), testBaseline)
	empty := writeFile(t, filepath.Join(dir, "empty.yaml"), "version: 1\nmax_regression_pct: {ns_per_op: 25, bytes_per_op: 25, allocs_per_op: 25}\n")
	noResults := writeFile(t, filepath.Join(dir, "none.txt"), "PASS\n")
	results := writeFile(t, filepath.Join(dir, "bench.txt"), "BenchmarkDraw-8 100 1000 ns/op\n")
	missing := filepath.Join(dir, "missing.yaml")
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"bench", "-count", "0"}, 2, "-count must be at least 1, not 0"},
		{[]string{"bench", "-baseline", ""}, 2, "-baseline file is required"},
		{[]string{"bench", "-bench", ""}, 2, "-bench must match the benchmarks to run"},
		{[]string{"bench", "-benchtime", ""}, 2, "-benchtime must not be empty"},
		{[]string{"bench", "-in", results, "./sprite"}, 2, "-in reads results instead of running the benchmarks"},
		{[]string{"bench", "-baseline", baseline, "-in", noResults}, 1, "no benchmark results in " + noResults},
		{[]string{"bench", "-baseline", missing, "-in", results}, 1, "record a baseline with -update"},
		{[]string{"bench", "-baseline", empty, "-in", results}, 1, "holds no benchmarks; record them with -update"},
		{[]string{"bench", "-baseline", results, "-in", results}, 1, results + ":1: "},
		{[]string{"bench", "-baseline", baseline, "-in", missing}, 1, "no such file or directory"},
	} {
		a, _, stderr := newTestApp(t)
		if code := run(tc.args, a); code != tc.code {
			t.Errorf("%q exit %d, want %d", tc.args, code, tc.code)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("%q stderr = %q, want %q", tc.args, stderr, tc.want)
		}
	}

	a, _, stderr := newTestApp(t)
	fakeBench(a, nil)
	if code := run([]string{"bench", "-baseline", baseline, "-bench", "Nothing", "-count", "1"}, a); code != 1 || !strings.Contains(stderr.String(), `-bench "Nothing" matched no benchmark`) {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}

	a, _, stderr = newTestApp(t)
	a.nsgo = func([]string, io.Writer, io.Writer) error { return errors.New("exit status 2") }
	if code := run([]string{"bench", "-baseline", baseline, "./sprite"}, a); code != 1 || !strings.Contains(stderr.String(), "benchmarks in ./sprite failed: exit status 2") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}
