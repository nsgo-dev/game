package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGamewireCheckBuiltinSchemas(t *testing.T) {
	wireDir := filepath.Join("..", "..", "pkg", "online", "netplay", "wire")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-in", wireDir, "-out", wireDir, "-self", "-check"}, &stdout, &stderr); err != nil {
		t.Fatalf("builtin wire codecs are stale: %v", err)
	}
}

func TestGamewireGenerateAndCheck(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "sample.wire.yaml")
	const doc = `version: 1
package: sample
order: le
messages:
  - name: Move
    fields:
      - name: tick
        type: uvarint
        bits: 32
      - name: x
        type: qfloat
        qfloat:
          min: -100
          max: 100
          bits: 12
      - name: y
        type: qfloat
        qfloat:
          min: -100
          max: 100
          bits: 12
`
	if err := os.WriteFile(schemaPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "gen")
	var stdout, stderr bytes.Buffer

	// -check fails when output is missing.
	if err := run([]string{"-in", dir, "-out", outDir, "-check"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("-check on empty dir = %v, want missing error", err)
	}

	// Generate writes deterministic Go file.
	stdout.Reset()
	if err := run([]string{"-in", dir, "-out", outDir}, &stdout, &stderr); err != nil {
		t.Fatalf("generate: %v", err)
	}
	outFile := filepath.Join(outDir, "sample_wire.go")
	if !strings.Contains(stdout.String(), "sample_wire.go") {
		t.Fatalf("stdout = %q, want sample_wire.go", stdout.String())
	}

	// -check passes immediately after generate.
	if err := run([]string{"-in", dir, "-out", outDir, "-check"}, &stdout, &stderr); err != nil {
		t.Fatalf("-check after generate: %v", err)
	}

	// Editing the generated file makes -check fail.
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outFile, append(data, []byte("\n// stale\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-in", dir, "-out", outDir, "-check"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "is out of date") {
		t.Fatalf("-check on stale file = %v, want out of date error", err)
	}

	// Single-file -out and -pkg override.
	singleOut := filepath.Join(dir, "custom.go")
	if err := run([]string{"-in", schemaPath, "-out", singleOut, "-pkg", "custompkg"}, &stdout, &stderr); err != nil {
		t.Fatalf("single file generate: %v", err)
	}
	singleData, err := os.ReadFile(singleOut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(singleData), "package custompkg") {
		t.Fatalf("generated single file missing package override:\n%s", singleData)
	}
	if err := run([]string{"-in", schemaPath, "-out", singleOut, "-pkg", "custompkg", "-check"}, &stdout, &stderr); err != nil {
		t.Fatalf("single file -check: %v", err)
	}

	// Unsupported -lang fails cleanly.
	if err := run([]string{"-in", schemaPath, "-lang", "rust"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "unsupported -lang") {
		t.Fatalf("unsupported -lang = %v", err)
	}
}
