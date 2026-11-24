package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// BudgetFile is the JSON schema stored in benchmarks/web-budget.json.
type BudgetFile struct {
	Target       string `json:"target"`
	LDFlags      string `json:"ldflags"`
	MaxRawBytes  int64  `json:"max_raw_bytes"`
	MaxGzipBytes int64  `json:"max_gzip_bytes"`
	Note         string `json:"note,omitempty"`
}

// Measurement stores measured raw and gzip-compressed WebAssembly sizes.
type Measurement struct {
	RawBytes  int64 `json:"raw_bytes"`
	GzipBytes int64 `json:"gzip_bytes"`
}

// BudgetReport holds the result of comparing a Measurement against a BudgetFile.
type BudgetReport struct {
	Budget     BudgetFile  `json:"budget"`
	Measured   Measurement `json:"measured"`
	Violations []string    `json:"violations,omitempty"`
}

// Passed reports whether both raw and gzipped sizes are within budget.
func (r BudgetReport) Passed() bool {
	return len(r.Violations) == 0
}

// MeasureWasm returns the raw and gzipped (BestCompression) byte sizes of wasm.
func MeasureWasm(wasm []byte) (Measurement, error) {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return Measurement{}, err
	}
	if _, err := gz.Write(wasm); err != nil {
		_ = gz.Close()
		return Measurement{}, err
	}
	if err := gz.Close(); err != nil {
		return Measurement{}, err
	}
	return Measurement{
		RawBytes:  int64(len(wasm)),
		GzipBytes: int64(buf.Len()),
	}, nil
}

// EvaluateBudget checks got against budget and returns a BudgetReport.
func EvaluateBudget(budget BudgetFile, got Measurement) BudgetReport {
	rep := BudgetReport{
		Budget:   budget,
		Measured: got,
	}
	if budget.MaxRawBytes > 0 && got.RawBytes > budget.MaxRawBytes {
		rep.Violations = append(rep.Violations, fmt.Sprintf("raw wasm size %s exceeds budget %s (+%s)",
			formatBytes(got.RawBytes), formatBytes(budget.MaxRawBytes), formatBytes(got.RawBytes-budget.MaxRawBytes)))
	}
	if budget.MaxGzipBytes > 0 && got.GzipBytes > budget.MaxGzipBytes {
		rep.Violations = append(rep.Violations, fmt.Sprintf("gzipped wasm size %s exceeds budget %s (+%s)",
			formatBytes(got.GzipBytes), formatBytes(budget.MaxGzipBytes), formatBytes(got.GzipBytes-budget.MaxGzipBytes)))
	}
	return rep
}

// FormatBudgetReport renders a human-readable summary of rep.
func FormatBudgetReport(rep BudgetReport) string {
	var b strings.Builder
	status := "PASS"
	if !rep.Passed() {
		status = "FAIL"
	}
	fmt.Fprintf(&b, "WebAssembly size budget [%s] (target: %s)\n", status, rep.Budget.Target)
	fmt.Fprintf(&b, "  raw:  %s / %s (%.1f%%)\n",
		formatBytes(rep.Measured.RawBytes),
		formatBytes(rep.Budget.MaxRawBytes),
		pctOf(rep.Measured.RawBytes, rep.Budget.MaxRawBytes))
	fmt.Fprintf(&b, "  gzip: %s / %s (%.1f%%)\n",
		formatBytes(rep.Measured.GzipBytes),
		formatBytes(rep.Budget.MaxGzipBytes),
		pctOf(rep.Measured.GzipBytes, rep.Budget.MaxGzipBytes))
	for _, v := range rep.Violations {
		fmt.Fprintf(&b, "  -> %s\n", v)
	}
	return b.String()
}

func formatBytes(n int64) string {
	const mib = 1024 * 1024
	const kib = 1024
	switch {
	case n >= mib:
		return fmt.Sprintf("%.2f MiB (%d B)", float64(n)/mib, n)
	case n >= kib:
		return fmt.Sprintf("%.2f KiB (%d B)", float64(n)/kib, n)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func pctOf(val, limit int64) float64 {
	if limit <= 0 {
		return 0
	}
	return (float64(val) / float64(limit)) * 100
}

// LoadBudget reads and decodes a BudgetFile from path. Its errors are *loaderr.Error values that name path, with the line of the problem and how to fix it.
func LoadBudget(path string) (BudgetFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BudgetFile{}, loaderr.Locate(err, path, nil, "check the budget file path")
	}
	var bf BudgetFile
	if err := json.Unmarshal(data, &bf); err != nil {
		return BudgetFile{}, loaderr.Locate(err, path, data, "correct the budget file; see docs/en/package-size-budgets.md")
	}
	return bf, nil
}
