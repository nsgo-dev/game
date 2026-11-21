package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// BaselineFile is the JSON schema stored in benchmarks/baseline.json.
type BaselineFile struct {
	MaxNsRegressionPct     float64                 `json:"max_ns_regression_pct"`
	MaxBytesRegressionPct  float64                 `json:"max_bytes_regression_pct"`
	MaxAllocsRegressionPct float64                 `json:"max_allocs_regression_pct"`
	Note                   string                  `json:"note,omitempty"`
	Benchmarks             map[string]BenchMetrics `json:"benchmarks"`
}

// BenchMetrics stores the tracked metrics for one Go benchmark.
type BenchMetrics struct {
	NsPerOp     float64 `json:"ns_per_op"`
	BytesPerOp  float64 `json:"bytes_per_op"`
	AllocsPerOp float64 `json:"allocs_per_op"`
}

// EvalOptions overrides thresholds from BaselineFile when positive.
type EvalOptions struct {
	MaxNsPct     float64
	MaxBytesPct  float64
	MaxAllocsPct float64
}

// BenchResult holds the comparison outcome for one benchmark.
type BenchResult struct {
	Name           string
	Missing        bool
	Baseline       BenchMetrics
	Measured       BenchMetrics
	NsDeltaPct     float64
	BytesDeltaPct  float64
	AllocsDeltaPct float64
	Regressions    []string
}

// Passed reports whether this benchmark met all baseline thresholds.
func (r BenchResult) Passed() bool {
	return !r.Missing && len(r.Regressions) == 0
}

// Report aggregates all benchmark comparison results.
type Report struct {
	MaxNsPct     float64
	MaxBytesPct  float64
	MaxAllocsPct float64
	Results      []BenchResult
}

// Passed reports whether every baseline benchmark passed without regression.
func (r Report) Passed() bool {
	if len(r.Results) == 0 {
		return false
	}
	for _, res := range r.Results {
		if !res.Passed() {
			return false
		}
	}
	return true
}

// ParseBenchmarkOutput parses standard `go test -bench=. -benchmem` output
// and returns median metrics per normalized benchmark name (stripping the
// trailing -GOMAXPROCS suffix).
func ParseBenchmarkOutput(r io.Reader) (map[string]BenchMetrics, error) {
	type series struct {
		ns     []float64
		bytes  []float64
		allocs []float64
	}
	grouped := make(map[string]*series)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := normalizeBenchName(fields[0])
		if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
			continue
		}

		var ns, bytesOp, allocsOp float64
		var hasNs bool
		for i := 2; i+1 < len(fields); i += 2 {
			val, err := strconv.ParseFloat(fields[i], 64)
			if err != nil {
				continue
			}
			switch fields[i+1] {
			case "ns/op":
				ns = val
				hasNs = true
			case "B/op":
				bytesOp = val
			case "allocs/op":
				allocsOp = val
			}
		}
		if !hasNs {
			continue
		}
		s := grouped[name]
		if s == nil {
			s = &series{}
			grouped[name] = s
		}
		s.ns = append(s.ns, ns)
		s.bytes = append(s.bytes, bytesOp)
		s.allocs = append(s.allocs, allocsOp)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	out := make(map[string]BenchMetrics, len(grouped))
	for name, s := range grouped {
		out[name] = BenchMetrics{
			NsPerOp:     median(s.ns),
			BytesPerOp:  median(s.bytes),
			AllocsPerOp: median(s.allocs),
		}
	}
	return out, nil
}

func normalizeBenchName(raw string) string {
	idx := strings.LastIndexByte(raw, '-')
	if idx <= 0 || idx == len(raw)-1 {
		return raw
	}
	for _, r := range raw[idx+1:] {
		if r < '0' || r > '9' {
			return raw
		}
	}
	return raw[:idx]
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := slices.Clone(vals)
	slices.Sort(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

// Evaluate compares measured benchmarks against baseline and returns a Report.
func Evaluate(baseline BaselineFile, measured map[string]BenchMetrics, opts EvalOptions) Report {
	maxNs := baseline.MaxNsRegressionPct
	if maxNs <= 0 {
		maxNs = 25
	}
	if opts.MaxNsPct > 0 {
		maxNs = opts.MaxNsPct
	}
	maxBytes := baseline.MaxBytesRegressionPct
	if maxBytes <= 0 {
		maxBytes = 25
	}
	if opts.MaxBytesPct > 0 {
		maxBytes = opts.MaxBytesPct
	}
	maxAllocs := baseline.MaxAllocsRegressionPct
	if maxAllocs <= 0 {
		maxAllocs = 25
	}
	if opts.MaxAllocsPct > 0 {
		maxAllocs = opts.MaxAllocsPct
	}

	names := make([]string, 0, len(baseline.Benchmarks))
	for name := range baseline.Benchmarks {
		names = append(names, name)
	}
	sort.Strings(names)

	rep := Report{
		MaxNsPct:     maxNs,
		MaxBytesPct:  maxBytes,
		MaxAllocsPct: maxAllocs,
		Results:      make([]BenchResult, 0, len(names)),
	}

	for _, name := range names {
		base := baseline.Benchmarks[name]
		got, ok := measured[name]
		if !ok {
			rep.Results = append(rep.Results, BenchResult{
				Name:        name,
				Missing:     true,
				Baseline:    base,
				Regressions: []string{"missing from benchmark output"},
			})
			continue
		}

		res := BenchResult{
			Name:           name,
			Baseline:       base,
			Measured:       got,
			NsDeltaPct:     deltaPct(base.NsPerOp, got.NsPerOp),
			BytesDeltaPct:  deltaPct(base.BytesPerOp, got.BytesPerOp),
			AllocsDeltaPct: deltaPct(base.AllocsPerOp, got.AllocsPerOp),
		}

		if res.NsDeltaPct > maxNs {
			res.Regressions = append(res.Regressions, fmt.Sprintf("ns/op regressed by %s (got %.1f, baseline %.1f, limit +%.1f%%)",
				formatPct(res.NsDeltaPct), got.NsPerOp, base.NsPerOp, maxNs))
		}
		if res.BytesDeltaPct > maxBytes {
			res.Regressions = append(res.Regressions, fmt.Sprintf("B/op regressed by %s (got %.1f, baseline %.1f, limit +%.1f%%)",
				formatPct(res.BytesDeltaPct), got.BytesPerOp, base.BytesPerOp, maxBytes))
		}
		if res.AllocsDeltaPct > maxAllocs {
			res.Regressions = append(res.Regressions, fmt.Sprintf("allocs/op regressed by %s (got %.1f, baseline %.1f, limit +%.1f%%)",
				formatPct(res.AllocsDeltaPct), got.AllocsPerOp, base.AllocsPerOp, maxAllocs))
		}

		rep.Results = append(rep.Results, res)
	}

	return rep
}

func deltaPct(base, got float64) float64 {
	if base == 0 {
		if got == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return ((got - base) / base) * 100
}

func formatPct(p float64) string {
	if math.IsInf(p, 1) {
		return "+inf%"
	}
	return fmt.Sprintf("%+.1f%%", p)
}

// FormatReport renders a human-readable summary table for rep.
func FormatReport(rep Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Benchmark baseline check (limits: ns/op +%.1f%%, B/op +%.1f%%, allocs/op +%.1f%%)\n",
		rep.MaxNsPct, rep.MaxBytesPct, rep.MaxAllocsPct)
	for _, r := range rep.Results {
		status := "PASS"
		if !r.Passed() {
			status = "FAIL"
		}
		if r.Missing {
			fmt.Fprintf(&b, "  [%s] %s: missing from benchmark output\n", status, r.Name)
			continue
		}
		fmt.Fprintf(&b, "  [%s] %-40s ns/op: %9.1f (base %9.1f, %7s) | B/op: %8.1f (base %8.1f, %7s) | allocs/op: %5.1f (base %5.1f, %7s)\n",
			status, r.Name,
			r.Measured.NsPerOp, r.Baseline.NsPerOp, formatPct(r.NsDeltaPct),
			r.Measured.BytesPerOp, r.Baseline.BytesPerOp, formatPct(r.BytesDeltaPct),
			r.Measured.AllocsPerOp, r.Baseline.AllocsPerOp, formatPct(r.AllocsDeltaPct))
		for _, msg := range r.Regressions {
			fmt.Fprintf(&b, "         -> %s\n", msg)
		}
	}
	return b.String()
}

// LoadBaseline reads and decodes a BaselineFile from path.
func LoadBaseline(path string) (BaselineFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BaselineFile{}, err
	}
	var bf BaselineFile
	if err := json.Unmarshal(data, &bf); err != nil {
		return BaselineFile{}, err
	}
	return bf, nil
}

// SaveBaseline writes bf as indented JSON to path.
func SaveBaseline(path string, bf BaselineFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(bf, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
