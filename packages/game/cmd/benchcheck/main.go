package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	baselinePath := flag.String("baseline", "../../benchmarks/baseline.json", "path to benchmark baseline JSON")
	inputPath := flag.String("input", "-", "path to `go test -bench` output (or '-' for stdin)")
	maxNsPct := flag.Float64("max-ns-pct", 0, "override max allowed ns/op regression percentage")
	maxBytesPct := flag.Float64("max-bytes-pct", 0, "override max allowed B/op regression percentage")
	maxAllocsPct := flag.Float64("max-allocs-pct", 0, "override max allowed allocs/op regression percentage")
	update := flag.Bool("update", false, "update baseline JSON with measured medians")
	flag.Parse()

	var reader io.Reader = os.Stdin
	if *inputPath != "" && *inputPath != "-" {
		f, err := os.Open(*inputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchcheck: open input: %v\n", err)
			os.Exit(2)
		}
		defer f.Close()
		reader = f
	}

	measured, err := ParseBenchmarkOutput(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchcheck: parse input: %v\n", err)
		os.Exit(2)
	}
	if len(measured) == 0 {
		fmt.Fprintln(os.Stderr, "benchcheck: no benchmark results found in input")
		os.Exit(2)
	}

	baseline, err := LoadBaseline(*baselinePath)
	if err != nil {
		if !*update {
			fmt.Fprintf(os.Stderr, "benchcheck: load baseline: %v\n", err)
			os.Exit(2)
		}
		baseline = BaselineFile{
			MaxNsRegressionPct:     25,
			MaxBytesRegressionPct:  25,
			MaxAllocsRegressionPct: 25,
		}
	}

	if *update {
		if baseline.Benchmarks == nil {
			baseline.Benchmarks = make(map[string]BenchMetrics, len(measured))
		}
		for name, metrics := range measured {
			baseline.Benchmarks[name] = metrics
		}
		if err := SaveBaseline(*baselinePath, baseline); err != nil {
			fmt.Fprintf(os.Stderr, "benchcheck: save baseline: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("benchcheck: updated %s with %d benchmarks\n", *baselinePath, len(measured))
		return
	}

	rep := Evaluate(baseline, measured, EvalOptions{
		MaxNsPct:     *maxNsPct,
		MaxBytesPct:  *maxBytesPct,
		MaxAllocsPct: *maxAllocsPct,
	})
	fmt.Print(FormatReport(rep))
	if !rep.Passed() {
		os.Exit(1)
	}
}
