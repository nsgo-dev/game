package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestMeasureWasmAndBudgetEvaluation(t *testing.T) {
	payload := bytes.Repeat([]byte("wasm-section-payload-0123456789abcdef"), 512)
	m, err := MeasureWasm(payload)
	if err != nil {
		t.Fatalf("MeasureWasm: %v", err)
	}
	if m.RawBytes != int64(len(payload)) {
		t.Fatalf("RawBytes = %d, want %d", m.RawBytes, len(payload))
	}
	if m.GzipBytes <= 0 || m.GzipBytes >= m.RawBytes {
		t.Fatalf("GzipBytes = %d, expected compressed size between 1 and %d", m.GzipBytes, m.RawBytes)
	}

	okBudget := BudgetFile{
		Target:       "./internal/sizeprobe",
		MaxRawBytes:  m.RawBytes + 1024,
		MaxGzipBytes: m.GzipBytes + 256,
	}
	if rep := EvaluateBudget(okBudget, m); !rep.Passed() {
		t.Fatalf("expected pass within budget, got:\n%s", FormatBudgetReport(rep))
	}

	// Exceed raw budget.
	tightRaw := okBudget
	tightRaw.MaxRawBytes = m.RawBytes - 1
	rep := EvaluateBudget(tightRaw, m)
	if rep.Passed() || !strings.Contains(FormatBudgetReport(rep), "raw wasm size") {
		t.Fatalf("expected raw size violation, got:\n%s", FormatBudgetReport(rep))
	}

	// Exceed gzip budget.
	tightGzip := okBudget
	tightGzip.MaxGzipBytes = m.GzipBytes - 1
	rep = EvaluateBudget(tightGzip, m)
	if rep.Passed() || !strings.Contains(FormatBudgetReport(rep), "gzipped wasm size") {
		t.Fatalf("expected gzip size violation, got:\n%s", FormatBudgetReport(rep))
	}
}

func TestLoadBudget(t *testing.T) {
	p := filepath.Join("..", "..", "benchmarks", "web-budget.json")
	b, err := LoadBudget(p)
	if err != nil {
		t.Fatalf("LoadBudget(%s): %v", p, err)
	}
	if b.Target == "" || b.MaxRawBytes <= 0 || b.MaxGzipBytes <= 0 {
		t.Fatalf("unexpected budget file contents: %+v", b)
	}
	// Verify LoadBudget returns an error cleanly for missing files.
	if _, err := LoadBudget(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing budget file")
	}
}
