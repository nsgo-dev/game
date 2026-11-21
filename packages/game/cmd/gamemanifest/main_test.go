package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGamemanifestCLI(t *testing.T) {
	inDir := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "manifest_gen.go")

	if err := os.WriteFile(filepath.Join(inDir, "hero.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stdout mode.
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-in", inDir, "-pkg", "assets"}, &stdout, &stderr); err != nil {
		t.Fatalf("run stdout: %v (stderr: %s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ImageHero") {
		t.Fatalf("expected ImageHero in stdout:\n%s", stdout.String())
	}

	// Write file mode + -check pass.
	stdout.Reset()
	if err := run([]string{"-in", inDir, "-out", outFile, "-pkg", "assets"}, &stdout, &stderr); err != nil {
		t.Fatalf("run write: %v", err)
	}
	if err := run([]string{"-in", inDir, "-out", outFile, "-pkg", "assets", "-check"}, &stdout, &stderr); err != nil {
		t.Fatalf("run -check on up-to-date manifest: %v", err)
	}

	// Adding an asset causes -check to fail.
	if err := os.WriteFile(filepath.Join(inDir, "jump.wav"), []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-in", inDir, "-out", outFile, "-pkg", "assets", "-check"}, &stdout, &stderr); err == nil {
		t.Fatal("expected -check to fail after adding jump.wav")
	}
}
