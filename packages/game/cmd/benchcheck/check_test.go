package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBenchmarkOutputComputesMedianAcrossRuns(t *testing.T) {
	raw := `
goos: darwin
goarch: arm64
BenchmarkBatchDraw1000-10          10000            110000 ns/op             512 B/op          2 allocs/op
BenchmarkBatchDraw1000-10          10000            100000 ns/op             512 B/op          2 allocs/op
BenchmarkBatchDraw1000-10          10000            130000 ns/op             512 B/op          2 allocs/op
BenchmarkRaycast-10              2000000               420.5 ns/op             0 B/op          0 allocs/op
PASS
`
	got, err := ParseBenchmarkOutput(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseBenchmarkOutput: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d benchmarks, want 2: %+v", len(got), got)
	}
	if b := got["BenchmarkBatchDraw1000"]; b.NsPerOp != 110000 || b.BytesPerOp != 512 || b.AllocsPerOp != 2 {
		t.Fatalf("BenchmarkBatchDraw1000 = %+v, want median 110000 ns/op, 512 B/op, 2 allocs/op", b)
	}
	if b := got["BenchmarkRaycast"]; b.NsPerOp != 420.5 || b.BytesPerOp != 0 || b.AllocsPerOp != 0 {
		t.Fatalf("BenchmarkRaycast = %+v, want 420.5 ns/op, 0 B/op, 0 allocs/op", b)
	}
}

func TestEvaluatePassesWithinThresholdAndFailsOnDeliberateSlowdown(t *testing.T) {
	baseline := BaselineFile{
		MaxNsRegressionPct:     25,
		MaxBytesRegressionPct:  25,
		MaxAllocsRegressionPct: 25,
		Benchmarks: map[string]BenchMetrics{
			"BenchmarkBatchDraw1000": {NsPerOp: 100000, BytesPerOp: 0, AllocsPerOp: 0},
			"BenchmarkRaycast":       {NsPerOp: 500, BytesPerOp: 0, AllocsPerOp: 0},
		},
	}

	// Within 25% threshold: passes.
	okMeasured := map[string]BenchMetrics{
		"BenchmarkBatchDraw1000": {NsPerOp: 120000, BytesPerOp: 0, AllocsPerOp: 0},
		"BenchmarkRaycast":       {NsPerOp: 550, BytesPerOp: 0, AllocsPerOp: 0},
	}
	okRep := Evaluate(baseline, okMeasured, EvalOptions{})
	if !okRep.Passed() {
		t.Fatalf("expected pass within threshold, got:\n%s", FormatReport(okRep))
	}

	// Deliberate 60% slowdown in BenchmarkBatchDraw1000: triggers regression failure cleanly.
	slowMeasured := map[string]BenchMetrics{
		"BenchmarkBatchDraw1000": {NsPerOp: 160000, BytesPerOp: 0, AllocsPerOp: 0},
		"BenchmarkRaycast":       {NsPerOp: 510, BytesPerOp: 0, AllocsPerOp: 0},
	}
	slowRep := Evaluate(baseline, slowMeasured, EvalOptions{})
	if slowRep.Passed() {
		t.Fatal("expected deliberate slowdown to fail baseline evaluation, but it passed")
	}
	summary := FormatReport(slowRep)
	if !strings.Contains(summary, "[FAIL] BenchmarkBatchDraw1000") || !strings.Contains(summary, "ns/op regressed by +60.0%") {
		t.Fatalf("unexpected failure report:\n%s", summary)
	}
}

func TestEvaluateFailsOnAllocRegressionFromZeroAndMissingBenchmark(t *testing.T) {
	baseline := BaselineFile{
		MaxNsRegressionPct:     25,
		MaxBytesRegressionPct:  25,
		MaxAllocsRegressionPct: 25,
		Benchmarks: map[string]BenchMetrics{
			"BenchmarkRaycast":   {NsPerOp: 500, BytesPerOp: 0, AllocsPerOp: 0},
			"BenchmarkLoadTiled": {NsPerOp: 10000, BytesPerOp: 4096, AllocsPerOp: 50},
		},
	}

	// Introduces 1 allocation where baseline is 0, and omits BenchmarkLoadTiled.
	measured := map[string]BenchMetrics{
		"BenchmarkRaycast": {NsPerOp: 500, BytesPerOp: 32, AllocsPerOp: 1},
	}
	rep := Evaluate(baseline, measured, EvalOptions{})
	if rep.Passed() {
		t.Fatal("expected failure for alloc regression and missing benchmark")
	}
	summary := FormatReport(rep)
	if !strings.Contains(summary, "allocs/op regressed by +inf%") {
		t.Fatalf("expected alloc regression message, got:\n%s", summary)
	}
	if !strings.Contains(summary, "[FAIL] BenchmarkLoadTiled: missing") {
		t.Fatalf("expected missing benchmark failure, got:\n%s", summary)
	}
}

func TestBaselineSaveAndLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "baseline.json")
	want := BaselineFile{
		MaxNsRegressionPct:     25,
		MaxBytesRegressionPct:  25,
		MaxAllocsRegressionPct: 25,
		Benchmarks: map[string]BenchMetrics{
			"BenchmarkLoadAtlas": {NsPerOp: 12345, BytesPerOp: 4096, AllocsPerOp: 42},
		},
	}
	if err := SaveBaseline(p, want); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}
	got, err := LoadBaseline(p)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	if got.Benchmarks["BenchmarkLoadAtlas"] != want.Benchmarks["BenchmarkLoadAtlas"] {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, want)
	}

	repoBaseline := filepath.Join("..", "..", "..", "..", "benchmarks", "baseline.json")
	loaded, err := LoadBaseline(repoBaseline)
	if err != nil {
		t.Fatalf("LoadBaseline(%s): %v", repoBaseline, err)
	}
	for _, name := range []string{
		"BenchmarkBatchDraw1000",
		"BenchmarkLoadAtlas",
		"BenchmarkTilemapDrawVisible1000x1000",
		"BenchmarkLoadTiled",
		"BenchmarkRaycast",
	} {
		if _, ok := loaded.Benchmarks[name]; !ok {
			t.Errorf("repo baseline missing %s", name)
		}
	}
}
