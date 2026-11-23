package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackWritesAtlas(t *testing.T) {
	inDir := t.TempDir()
	outDir := t.TempDir()

	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	img.SetNRGBA(2, 2, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	f, err := os.Create(filepath.Join(inDir, "dot.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"pack", "-in", inDir, "-out", outDir, "-name", "ui", "-padding", "2"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "ui.png") || !strings.Contains(stdout.String(), "ui.json") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	for _, name := range []string{"ui.png", "ui.json"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestPackErrors(t *testing.T) {
	a, _, stderr := newTestApp(t)
	if code := run([]string{"pack"}, a); code != 2 {
		t.Fatalf("missing -in exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "game pack: -in directory is required") || !strings.Contains(stderr.String(), "usage: game pack") {
		t.Fatalf("stderr = %q, want the error and the usage", stderr)
	}

	a, _, _ = newTestApp(t)
	if code := run([]string{"pack", "-in", t.TempDir(), "extra"}, a); code != 2 {
		t.Fatalf("stray argument exit %d, want 2", code)
	}

	a, _, stderr = newTestApp(t)
	if code := run([]string{"pack", "-in", filepath.Join(t.TempDir(), "missing")}, a); code != 1 {
		t.Fatalf("missing directory exit %d, want 1", code)
	}
	if !strings.HasPrefix(stderr.String(), "game pack: ") {
		t.Fatalf("stderr = %q, want a game pack: prefix", stderr)
	}
}
