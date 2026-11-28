package gamecli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runWire runs `game wire` with args and returns its exit code, standard output and standard error.
func runWire(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	a, stdout, stderr := newTestApp(t)
	code := run(append([]string{"wire"}, args...), a)
	return code, stdout.String(), stderr.String()
}

func TestWireCheckBuiltinSchemas(t *testing.T) {
	wireDir := filepath.Join("..", "..", "pkg", "online", "netplay", "wire")
	if code, _, errOut := runWire(t, "-in", wireDir, "-out", wireDir, "-self", "-check"); code != 0 {
		t.Fatalf("builtin Go wire codecs are stale: exit %d: %s", code, errOut)
	}
	tsOut := filepath.Join(wireDir, "ts")
	if code, _, errOut := runWire(t, "-lang", "ts", "-in", wireDir, "-out", tsOut, "-check"); code != 0 {
		t.Fatalf("builtin TS wire codecs are stale: exit %d: %s", code, errOut)
	}
}

func TestWireGenerateAndCheckTS(t *testing.T) {
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
      - name: bits
        type: u64
      - name: x
        type: qfloat
        qfloat:
          min: -100
          max: 100
          bits: 12
`
	if err := os.WriteFile(schemaPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "tsgen")

	if code, _, errOut := runWire(t, "-lang", "ts", "-in", dir, "-out", outDir, "-check"); code == 0 || !strings.Contains(errOut, "is missing") {
		t.Fatalf("-lang ts -check on empty dir = exit %d, %q, want missing error", code, errOut)
	}

	code, out, errOut := runWire(t, "-lang", "ts", "-in", dir, "-out", outDir)
	if code != 0 {
		t.Fatalf("ts generate: exit %d: %s", code, errOut)
	}
	for _, want := range []string{"runtime.ts", "sample_wire.ts", "index.ts"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want %s", out, want)
		}
	}

	if code, _, errOut := runWire(t, "-lang", "ts", "-in", dir, "-out", outDir, "-check"); code != 0 {
		t.Fatalf("-lang ts -check after generate: exit %d: %s", code, errOut)
	}

	wireTS := filepath.Join(outDir, "sample_wire.ts")
	data, err := os.ReadFile(wireTS)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "bits: bigint;") || !strings.Contains(string(data), "encodeMove(m: Move, w: Writer)") {
		t.Fatalf("unexpected generated TS:\n%s", data)
	}
	if err := os.WriteFile(wireTS, append(data, []byte("\n// stale\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runWire(t, "-lang", "ts", "-in", dir, "-out", outDir, "-check"); code == 0 || !strings.Contains(errOut, "is out of date") {
		t.Fatalf("-lang ts -check on stale file = exit %d, %q, want out of date error", code, errOut)
	}

	// Single-file .ts embeds runtime cleanly.
	singleOut := filepath.Join(dir, "standalone.ts")
	if code, _, errOut := runWire(t, "-lang", "ts", "-in", schemaPath, "-out", singleOut); code != 0 {
		t.Fatalf("single-file ts generate: exit %d: %s", code, errOut)
	}
	singleData, err := os.ReadFile(singleOut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(singleData), "export class Writer") || !strings.Contains(string(singleData), "export function decodeMove") {
		t.Fatalf("single-file ts missing embedded runtime or decoder:\n%s", singleData)
	}
	if code, _, errOut := runWire(t, "-lang", "ts", "-in", schemaPath, "-out", singleOut, "-check"); code != 0 {
		t.Fatalf("single-file ts -check: exit %d: %s", code, errOut)
	}
}

func TestWireGenerateAndCheck(t *testing.T) {
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

	// -check fails when output is missing.
	if code, _, errOut := runWire(t, "-in", dir, "-out", outDir, "-check"); code == 0 || !strings.Contains(errOut, "is missing") {
		t.Fatalf("-check on empty dir = exit %d, %q, want missing error", code, errOut)
	}

	// Generate writes deterministic Go file.
	code, out, errOut := runWire(t, "-in", dir, "-out", outDir)
	if code != 0 {
		t.Fatalf("generate: exit %d: %s", code, errOut)
	}
	outFile := filepath.Join(outDir, "sample_wire.go")
	if !strings.Contains(out, "sample_wire.go") {
		t.Fatalf("stdout = %q, want sample_wire.go", out)
	}

	// -check passes immediately after generate.
	if code, _, errOut := runWire(t, "-in", dir, "-out", outDir, "-check"); code != 0 {
		t.Fatalf("-check after generate: exit %d: %s", code, errOut)
	}

	// Editing the generated file makes -check fail.
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outFile, append(data, []byte("\n// stale\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runWire(t, "-in", dir, "-out", outDir, "-check"); code == 0 || !strings.Contains(errOut, "is out of date") {
		t.Fatalf("-check on stale file = exit %d, %q, want out of date error", code, errOut)
	}

	// Single-file -out and -pkg override.
	singleOut := filepath.Join(dir, "custom.go")
	if code, _, errOut := runWire(t, "-in", schemaPath, "-out", singleOut, "-pkg", "custompkg"); code != 0 {
		t.Fatalf("single file generate: exit %d: %s", code, errOut)
	}
	singleData, err := os.ReadFile(singleOut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(singleData), "package custompkg") {
		t.Fatalf("generated single file missing package override:\n%s", singleData)
	}
	if code, _, errOut := runWire(t, "-in", schemaPath, "-out", singleOut, "-pkg", "custompkg", "-check"); code != 0 {
		t.Fatalf("single file -check: exit %d: %s", code, errOut)
	}

	// An unsupported -lang is a usage error.
	if code, _, errOut := runWire(t, "-in", schemaPath, "-lang", "rust"); code != 2 || !strings.Contains(errOut, "unsupported -lang") {
		t.Fatalf("unsupported -lang = exit %d, %q, want exit 2", code, errOut)
	}
}
