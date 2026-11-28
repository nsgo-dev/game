package bench_test

import (
	"bytes"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/bench"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const sampleFile = `version: 1
note: Update with go tool game bench -update.
max_regression_pct: {ns_per_op: 25, bytes_per_op: 30, allocs_per_op: 12.5}
benchmarks:
  BenchmarkBatchDraw1000: {ns_per_op: 3500000, bytes_per_op: 80128, allocs_per_op: 1000}
  BenchmarkRaycast: {ns_per_op: 420.5, bytes_per_op: 0, allocs_per_op: 0}
`

func TestDecodeReadsEveryField(t *testing.T) {
	b, err := bench.Decode([]byte(sampleFile))
	if err != nil {
		t.Fatal(err)
	}
	want := &bench.Baseline{
		Version: 1,
		Note:    "Update with go tool game bench -update.",
		Limits:  bench.Limits{NsPerOp: 25, BytesPerOp: 30, AllocsPerOp: 12.5},
		Benchmarks: map[string]bench.Metrics{
			"BenchmarkBatchDraw1000": {NsPerOp: 3500000, BytesPerOp: 80128, AllocsPerOp: 1000},
			"BenchmarkRaycast":       {NsPerOp: 420.5},
		},
	}
	if !reflect.DeepEqual(b, want) {
		t.Fatalf("Decode = %+v, want %+v", b, want)
	}
}

func TestEncodeRoundTripsByteForByte(t *testing.T) {
	b, err := bench.Decode([]byte(sampleFile))
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sampleFile {
		t.Fatalf("Encode changed the file:\n got:\n%s\nwant:\n%s", got, sampleFile)
	}
}

func TestEncodeSortsBenchmarksAndWritesPlainNumbers(t *testing.T) {
	b := bench.NewBaseline()
	b.Update(map[string]bench.Metrics{
		"BenchmarkZ": {NsPerOp: 1e7, BytesPerOp: math.Copysign(0, -1), AllocsPerOp: 2},
		"BenchmarkA": {NsPerOp: 0.25, BytesPerOp: 16, AllocsPerOp: 1},
	})
	first, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := `version: 1
max_regression_pct: {ns_per_op: 25, bytes_per_op: 25, allocs_per_op: 25}
benchmarks:
  BenchmarkA: {ns_per_op: 0.25, bytes_per_op: 16, allocs_per_op: 1}
  BenchmarkZ: {ns_per_op: 10000000, bytes_per_op: 0, allocs_per_op: 2}
`
	if string(first) != want {
		t.Fatalf("Encode:\n%s\nwant:\n%s", first, want)
	}
	for range 5 {
		again, err := b.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(again, first) {
			t.Fatalf("Encode is not deterministic:\n%s\nthen:\n%s", first, again)
		}
	}
}

func TestDecodeRefusesBadFiles(t *testing.T) {
	limits := "max_regression_pct: {ns_per_op: 25, bytes_per_op: 25, allocs_per_op: 25}\n"
	for name, tc := range map[string]struct{ text, want string }{
		"empty":           {"", "empty baseline"},
		"no version":      {limits, "unsupported version 0"},
		"other version":   {"version: 2\n" + limits, "unsupported version 2"},
		"unknown key":     {"version: 1\n" + limits + "extra: 1\n", `unknown field "extra"`},
		"unknown metric":  {"version: 1\n" + limits + "benchmarks:\n  BenchmarkA: {ns_per_op: 1, bytes_per_op: 1, allocs_per_op: 1, mb_per_s: 3}\n", `unknown field "mb_per_s"`},
		"no limits":       {"version: 1\nbenchmarks: {}\n", "max_regression_pct is empty"},
		"missing limit":   {"version: 1\nmax_regression_pct: {ns_per_op: 25, bytes_per_op: 25}\n", "max_regression_pct has no allocs_per_op"},
		"zero limit":      {"version: 1\nmax_regression_pct: {ns_per_op: 0, bytes_per_op: 25, allocs_per_op: 25}\n", "must be a positive number"},
		"missing metric":  {"version: 1\n" + limits + "benchmarks:\n  BenchmarkA: {ns_per_op: 1, allocs_per_op: 1}\n", "BenchmarkA has no bytes_per_op"},
		"negative metric": {"version: 1\n" + limits + "benchmarks:\n  BenchmarkA: {ns_per_op: -1, bytes_per_op: 1, allocs_per_op: 1}\n", "finite number of at least 0"},
		"nan metric":      {"version: 1\n" + limits + "benchmarks:\n  BenchmarkA: {ns_per_op: .nan, bytes_per_op: 1, allocs_per_op: 1}\n", "finite number of at least 0"},
		"bad name":        {"version: 1\n" + limits + "benchmarks:\n  TestA: {ns_per_op: 1, bytes_per_op: 1, allocs_per_op: 1}\n", "not a benchmark name"},
		"trailing doc":    {"version: 1\n" + limits + "---\nversion: 1\n", "trailing YAML document"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := bench.Decode([]byte(tc.text))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Decode error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestSaveFileAndLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "baseline.yaml")
	b, err := bench.Decode([]byte(sampleFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := bench.SaveFile(path, b); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sampleFile {
		t.Fatalf("saved:\n%s\nwant:\n%s", data, sampleFile)
	}
	again, err := bench.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, b) {
		t.Fatalf("LoadFile = %+v, want %+v", again, b)
	}
	if _, err := bench.LoadFile(filepath.Join(t.TempDir(), "missing.yaml")); err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LoadFile of a missing file = %v, want a not-exist error", err)
	}
}

func TestParseOutputTakesMediansFromGoAndNSGoOutput(t *testing.T) {
	raw := `goos: linux
goarch: amd64
BenchmarkBatchDraw1000-10          10000            110000 ns/op             512 B/op          2 allocs/op
BenchmarkBatchDraw1000-10          10000            100000 ns/op             512 B/op          2 allocs/op
  ✓ BenchmarkBatchDraw1000-10 44 130000 ns/op 512 B/op 2 allocs/op
BenchmarkRaycast-10              2000000               420.5 ns/op             0 B/op          0 allocs/op
BenchmarkRaycast-10              2000000               421.5 ns/op             0 B/op          0 allocs/op
BenchmarkNoMem-10                1000                  7 ns/op
BenchmarkTooShort-10 12
BenchmarkNotANumber-10 lots 5 ns/op
BenchmarkNoNs-10 100 5 MB/s
Benchmark/sub-case-10 100 9 ns/op 1 B/op 1 allocs/op
  ✓ 3 passed, 0 failed, 0 skipped
PASS
`
	got, err := bench.ParseOutput(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bench.Metrics{
		"BenchmarkBatchDraw1000": {NsPerOp: 110000, BytesPerOp: 512, AllocsPerOp: 2},
		"BenchmarkRaycast":       {NsPerOp: 421, BytesPerOp: 0, AllocsPerOp: 0},
		"BenchmarkNoMem":         {NsPerOp: 7},
		"Benchmark/sub-case":     {NsPerOp: 9, BytesPerOp: 1, AllocsPerOp: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseOutput = %+v, want %+v", got, want)
	}
}

func TestParseOutputCPUSuffixHandling(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want map[string]bench.Metrics
	}{
		{
			name: "1-CPU numeric sub-benchmarks stay separate",
			raw: `goos: linux
goarch: amd64
pkg: example.com/decode
cpu: Intel(R) Xeon(R) CPU
BenchmarkDecode/size-1024    1000000    100 ns/op    64 B/op    1 allocs/op
BenchmarkDecode/size-4096     200000    900 ns/op   256 B/op    2 allocs/op
BenchmarkDecode/size-1024    1000000    120 ns/op    64 B/op    1 allocs/op
BenchmarkDecode/size-4096     200000    940 ns/op   256 B/op    2 allocs/op
PASS
ok  	example.com/decode	1.234s
`,
			want: map[string]bench.Metrics{
				"BenchmarkDecode/size-1024": {NsPerOp: 110, BytesPerOp: 64, AllocsPerOp: 1},
				"BenchmarkDecode/size-4096": {NsPerOp: 920, BytesPerOp: 256, AllocsPerOp: 2},
			},
		},
		{
			name: "8-CPU numeric sub-benchmarks produce same keys as 1-CPU",
			raw: `goos: linux
goarch: amd64
pkg: example.com/decode
cpu: Intel(R) Xeon(R) CPU
BenchmarkDecode/size-1024-8  1000000    100 ns/op    64 B/op    1 allocs/op
BenchmarkDecode/size-4096-8   200000    900 ns/op   256 B/op    2 allocs/op
BenchmarkDecode/size-1024-8  1000000    120 ns/op    64 B/op    1 allocs/op
BenchmarkDecode/size-4096-8   200000    940 ns/op   256 B/op    2 allocs/op
PASS
ok  	example.com/decode	1.234s
`,
			want: map[string]bench.Metrics{
				"BenchmarkDecode/size-1024": {NsPerOp: 110, BytesPerOp: 64, AllocsPerOp: 1},
				"BenchmarkDecode/size-4096": {NsPerOp: 920, BytesPerOp: 256, AllocsPerOp: 2},
			},
		},
		{
			name: "1-CPU plain benchmark protects single numeric sub-benchmark",
			raw: `BenchmarkRaycast             2000000    420 ns/op     0 B/op    0 allocs/op
BenchmarkFoo/size-64          100000    100 ns/op    64 B/op    1 allocs/op
`,
			want: map[string]bench.Metrics{
				"BenchmarkRaycast":     {NsPerOp: 420},
				"BenchmarkFoo/size-64": {NsPerOp: 100, BytesPerOp: 64, AllocsPerOp: 1},
			},
		},
		{
			name: "1-CPU single sub-benchmark ending in -1 is never stripped",
			raw:  "BenchmarkBar/step-1          100000     50 ns/op     8 B/op    1 allocs/op\n",
			want: map[string]bench.Metrics{
				"BenchmarkBar/step-1": {NsPerOp: 50, BytesPerOp: 8, AllocsPerOp: 1},
			},
		},
		{
			name: "1-CPU single sub-benchmark ending in -0 or leading zero is never stripped",
			raw: `BenchmarkBar/step-0          100000     12 ns/op
BenchmarkBar/step-08         100000     34 ns/op
`,
			want: map[string]bench.Metrics{
				"BenchmarkBar/step-0":  {NsPerOp: 12},
				"BenchmarkBar/step-08": {NsPerOp: 34},
			},
		},
		{
			name: "1-CPU verbose single sub-benchmark ending in -64 keeps suffix",
			raw: `goos: darwin
goarch: arm64
pkg: example.com/foo
cpu: Apple M2 Max
BenchmarkFoo
BenchmarkFoo/size-64
BenchmarkFoo/size-64         100000     88 ns/op    32 B/op    1 allocs/op
PASS
ok  	example.com/foo	0.312s
`,
			want: map[string]bench.Metrics{
				"BenchmarkFoo/size-64": {NsPerOp: 88, BytesPerOp: 32, AllocsPerOp: 1},
			},
		},
		{
			name: "1-CPU === RUN verbose single sub-benchmark ending in -64 keeps suffix",
			raw: `=== RUN   BenchmarkFoo
=== RUN   BenchmarkFoo/size-64
BenchmarkFoo/size-64         100000     88 ns/op
`,
			want: map[string]bench.Metrics{
				"BenchmarkFoo/size-64": {NsPerOp: 88},
			},
		},
		{
			name: "64-CPU verbose single sub-benchmark strips -64",
			raw: `goos: linux
goarch: amd64
pkg: example.com/foo
BenchmarkFoo
BenchmarkFoo/size
BenchmarkFoo/size-64         100000     88 ns/op
PASS
`,
			want: map[string]bench.Metrics{
				"BenchmarkFoo/size": {NsPerOp: 88},
			},
		},
		{
			name: "multi-CPU -4 with BenchmarkFoo-2-4 strips only trailing -4",
			raw: `BenchmarkFoo-2-4             100000     75 ns/op    16 B/op    1 allocs/op
BenchmarkBar/step-1-4        100000     50 ns/op     8 B/op    1 allocs/op
`,
			want: map[string]bench.Metrics{
				"BenchmarkFoo-2":      {NsPerOp: 75, BytesPerOp: 16, AllocsPerOp: 1},
				"BenchmarkBar/step-1": {NsPerOp: 50, BytesPerOp: 8, AllocsPerOp: 1},
			},
		},
		{
			name: "multi-CPU -16 strips uniform -16 across sub-benchmarks",
			raw: `  ✓ BenchmarkFoo/size-64-16 1000 100 ns/op 64 B/op 1 allocs/op
  ✓ BenchmarkBar/step-1-16 1000 50 ns/op 8 B/op 1 allocs/op
  2 passed, 0 failed, 0 skipped
`,
			want: map[string]bench.Metrics{
				"BenchmarkFoo/size-64": {NsPerOp: 100, BytesPerOp: 64, AllocsPerOp: 1},
				"BenchmarkBar/step-1":  {NsPerOp: 50, BytesPerOp: 8, AllocsPerOp: 1},
			},
		},
		{
			name: "multi-cpu list -cpu=1,4 never merges distinct full names",
			raw: `BenchmarkFoo                 100000    100 ns/op
BenchmarkFoo-4               100000     40 ns/op
`,
			want: map[string]bench.Metrics{
				"BenchmarkFoo":   {NsPerOp: 100},
				"BenchmarkFoo-4": {NsPerOp: 40},
			},
		},
		{
			name: "multi-cpu list -cpu=4,8 never merges distinct full names",
			raw: `BenchmarkFoo-4               100000     40 ns/op
BenchmarkFoo-8               100000     25 ns/op
`,
			want: map[string]bench.Metrics{
				"BenchmarkFoo-4": {NsPerOp: 40},
				"BenchmarkFoo-8": {NsPerOp: 25},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bench.ParseOutput(strings.NewReader(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseOutput = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseOutputRealGoTest1CPUAnd8CPUKeysMatch(t *testing.T) {
	oneCPU := "goos: darwin\n" +
		"goarch: arm64\n" +
		"pkg: github.com/nsgo-dev/game/pkg/tooling/bench\n" +
		"cpu: Apple M2 Max\n" +
		"BenchmarkDecode/size-64\t 1000000\t       120.5 ns/op\t      64 B/op\t       1 allocs/op\n" +
		"BenchmarkDecode/size-1024\t  200000\t       480.0 ns/op\t    1024 B/op\t       1 allocs/op\n" +
		"BenchmarkDecode/size-4096\t   50000\t      1920.0 ns/op\t    4096 B/op\t       1 allocs/op\n" +
		"BenchmarkBar/step-1\t 5000000\t        42.0 ns/op\t       0 B/op\t       0 allocs/op\n" +
		"BenchmarkPlain\t 5000000\t        15.0 ns/op\t       0 B/op\t       0 allocs/op\n" +
		"PASS\n" +
		"ok  \tgithub.com/nsgo-dev/game/pkg/tooling/bench\t2.418s\n"

	eightCPU := "goos: darwin\n" +
		"goarch: arm64\n" +
		"pkg: github.com/nsgo-dev/game/pkg/tooling/bench\n" +
		"cpu: Apple M2 Max\n" +
		"BenchmarkDecode/size-64-8\t 1000000\t       120.5 ns/op\t      64 B/op\t       1 allocs/op\n" +
		"BenchmarkDecode/size-1024-8\t  200000\t       480.0 ns/op\t    1024 B/op\t       1 allocs/op\n" +
		"BenchmarkDecode/size-4096-8\t   50000\t      1920.0 ns/op\t    4096 B/op\t       1 allocs/op\n" +
		"BenchmarkBar/step-1-8\t 5000000\t        42.0 ns/op\t       0 B/op\t       0 allocs/op\n" +
		"BenchmarkPlain-8\t 5000000\t        15.0 ns/op\t       0 B/op\t       0 allocs/op\n" +
		"PASS\n" +
		"ok  \tgithub.com/nsgo-dev/game/pkg/tooling/bench\t2.104s\n"

	got1, err := bench.ParseOutput(strings.NewReader(oneCPU))
	if err != nil {
		t.Fatal(err)
	}
	got8, err := bench.ParseOutput(strings.NewReader(eightCPU))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got1, got8) {
		t.Fatalf("1-CPU and 8-CPU parsed outputs differ:\n1-CPU: %+v\n8-CPU: %+v", got1, got8)
	}
	if len(got1) != 5 {
		t.Fatalf("got %d benchmarks, want 5: %+v", len(got1), got1)
	}
	for _, key := range []string{
		"BenchmarkDecode/size-64",
		"BenchmarkDecode/size-1024",
		"BenchmarkDecode/size-4096",
		"BenchmarkBar/step-1",
		"BenchmarkPlain",
	} {
		if _, ok := got1[key]; !ok {
			t.Errorf("missing expected key %q in %+v", key, got1)
		}
	}
}

func TestEvaluatePassesWithinLimitsAndFailsOnRegression(t *testing.T) {
	b := bench.NewBaseline()
	b.Update(map[string]bench.Metrics{
		"BenchmarkDraw":    {NsPerOp: 100000, BytesPerOp: 100, AllocsPerOp: 4},
		"BenchmarkRaycast": {NsPerOp: 500},
	})

	ok := bench.Evaluate(b, map[string]bench.Metrics{
		"BenchmarkDraw":    {NsPerOp: 120000, BytesPerOp: 125, AllocsPerOp: 5},
		"BenchmarkRaycast": {NsPerOp: 400},
		"BenchmarkNew":     {NsPerOp: 1},
	})
	if !ok.Passed() || ok.Failed() != 0 {
		t.Fatalf("expected a pass within the limits:\n%s", ok)
	}
	if !reflect.DeepEqual(ok.New, []string{"BenchmarkNew"}) {
		t.Fatalf("New = %v, want [BenchmarkNew]", ok.New)
	}
	if !strings.Contains(ok.String(), "[NEW ] BenchmarkNew") {
		t.Fatalf("report does not list the new benchmark:\n%s", ok)
	}

	slow := bench.Evaluate(b, map[string]bench.Metrics{
		"BenchmarkDraw":    {NsPerOp: 160000, BytesPerOp: 100, AllocsPerOp: 4},
		"BenchmarkRaycast": {NsPerOp: 500, AllocsPerOp: 1},
	})
	if slow.Passed() || slow.Failed() != 2 {
		t.Fatalf("expected two failures:\n%s", slow)
	}
	draw, raycast := slow.Results[0], slow.Results[1]
	if draw.Name != "BenchmarkDraw" || len(draw.Regressions) != 1 || !strings.Contains(draw.Regressions[0], "ns/op regressed by +60.0%") {
		t.Fatalf("BenchmarkDraw = %+v", draw)
	}
	if raycast.Name != "BenchmarkRaycast" || !math.IsInf(raycast.Delta.AllocsPerOp, 1) || !strings.Contains(raycast.Regressions[0], "allocs/op regressed by +inf%") {
		t.Fatalf("BenchmarkRaycast = %+v", raycast)
	}
	if s := slow.String(); !strings.Contains(s, "[FAIL] BenchmarkDraw") || !strings.Contains(s, "-> ns/op regressed") {
		t.Fatalf("report:\n%s", s)
	}
}

func TestEvaluateFailsOnMissingBenchmarkAndEmptyBaseline(t *testing.T) {
	b := bench.NewBaseline()
	b.Update(map[string]bench.Metrics{"BenchmarkGone": {NsPerOp: 1}})
	rep := bench.Evaluate(b, map[string]bench.Metrics{})
	if rep.Passed() || !rep.Results[0].Missing || !strings.Contains(rep.String(), "[FAIL] BenchmarkGone: not measured") {
		t.Fatalf("missing benchmark passed:\n%s", rep)
	}
	if empty := bench.Evaluate(bench.NewBaseline(), map[string]bench.Metrics{"BenchmarkA": {NsPerOp: 1}}); empty.Passed() {
		t.Fatalf("an empty baseline passed:\n%s", empty)
	}
}

func TestEvaluateStrictFailsOnUnbaselinedBenchmark(t *testing.T) {
	b := bench.NewBaseline()
	b.Update(map[string]bench.Metrics{"BenchmarkKnown": {NsPerOp: 1}})
	rep := bench.EvaluateStrict(b, map[string]bench.Metrics{
		"BenchmarkKnown": {NsPerOp: 1},
		"BenchmarkNew":   {NsPerOp: 1},
	})
	if rep.Passed() || rep.Failed() != 1 || len(rep.Results) != 2 {
		t.Fatalf("strict report = %+v", rep)
	}
}

func TestEvaluateFailsOnMetricsThatAreNotFiniteNumbers(t *testing.T) {
	b := bench.NewBaseline()
	b.Update(map[string]bench.Metrics{"BenchmarkDraw": {NsPerOp: 1000, BytesPerOp: 64, AllocsPerOp: 2}})
	for _, tc := range []struct {
		got  bench.Metrics
		want string
	}{
		{bench.Metrics{NsPerOp: math.NaN(), BytesPerOp: 64, AllocsPerOp: 2}, "ns/op is not a finite, non-negative number (got NaN)"},
		{bench.Metrics{NsPerOp: 1000, BytesPerOp: math.Inf(1), AllocsPerOp: 2}, "B/op is not a finite, non-negative number (got +Inf)"},
		{bench.Metrics{NsPerOp: 1000, BytesPerOp: 64, AllocsPerOp: -1}, "allocs/op is not a finite, non-negative number (got -1)"},
	} {
		rep := bench.Evaluate(b, map[string]bench.Metrics{"BenchmarkDraw": tc.got})
		if rep.Passed() || len(rep.Results[0].Regressions) != 1 || rep.Results[0].Regressions[0] != tc.want {
			t.Errorf("%+v: regressions %q, want [%q]", tc.got, rep.Results[0].Regressions, tc.want)
		}
	}
}

func TestUpdateKeepsUnmeasuredBenchmarks(t *testing.T) {
	b := bench.NewBaseline()
	b.Update(map[string]bench.Metrics{"BenchmarkA": {NsPerOp: 1}, "BenchmarkB": {NsPerOp: 2}})
	b.Update(map[string]bench.Metrics{"BenchmarkB": {NsPerOp: 3}})
	want := map[string]bench.Metrics{"BenchmarkA": {NsPerOp: 1}, "BenchmarkB": {NsPerOp: 3}}
	if !reflect.DeepEqual(b.Benchmarks, want) {
		t.Fatalf("Benchmarks = %+v, want %+v", b.Benchmarks, want)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte(sampleFile))
	f.Add([]byte("version: 1\nmax_regression_pct: {ns_per_op: 1, bytes_per_op: 1, allocs_per_op: 1}\n"))
	f.Add([]byte("version: 1\nmax_regression_pct: {ns_per_op: 1e400, bytes_per_op: 1, allocs_per_op: 1}\n"))
	f.Add([]byte("version: 1\nnote: \"a: b\\n\"\nmax_regression_pct: {ns_per_op: 1, bytes_per_op: 1, allocs_per_op: 1}\nbenchmarks:\n  Benchmark/x-y: {ns_per_op: 1e20, bytes_per_op: -0.0, allocs_per_op: 0x10}\n"))
	f.Add([]byte("{"))
	f.Fuzz(func(t *testing.T, data []byte) {
		b, err := bench.Decode(data)
		if err != nil {
			return
		}
		first, err := b.Encode()
		if err != nil {
			t.Fatalf("Encode of a decoded baseline: %v", err)
		}
		again, err := bench.Decode(first)
		if err != nil {
			t.Fatalf("Decode of encoded text: %v\n%s", err, first)
		}
		if !reflect.DeepEqual(again, b) {
			t.Fatalf("round trip changed the baseline: %+v, then %+v", b, again)
		}
		second, err := again.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("Encode is not stable:\n%s\nthen:\n%s", first, second)
		}
	})
}

func BenchmarkParseOutput(b *testing.B) {
	var raw strings.Builder
	for i := range 200 {
		raw.WriteString("BenchmarkCase")
		raw.WriteByte(byte('A' + i%26))
		raw.WriteString("-8  1000  12345.6 ns/op  512 B/op  3 allocs/op\n")
	}
	text := raw.String()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := bench.ParseOutput(strings.NewReader(text)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRepositoryBaselineRoundTrips(t *testing.T) {
	path := filepath.Join("..", "..", "..", "benchmarks", "baseline.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bench.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Benchmarks) == 0 {
		t.Fatal("the repository baseline holds no benchmarks")
	}
	again, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, data) {
		t.Fatalf("a load and save changes %s:\n%s", path, again)
	}
}
