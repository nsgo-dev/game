package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
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

func TestPackAppliesSidecarSliceAndPivot(t *testing.T) {
	inDir := t.TempDir()
	outDir := t.TempDir()

	sheet := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for y := range 16 {
		for x := range 32 {
			sheet.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	f, err := os.Create(filepath.Join(inDir, "hero.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, sheet); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	sidecar := "version: 1\nimage:\n  slice: {width: 16, height: 16}\n  pivot:\n    default: [0.5, 1]\n    cells:\n      - cell: 1\n        pivot: [0.25, 0.5]\n"
	if err := os.WriteFile(filepath.Join(inDir, "hero.png.import.yaml"), []byte(sidecar), 0o644); err != nil {
		t.Fatal(err)
	}

	a, _, stderr := newTestApp(t)
	if code := run([]string{"pack", "-in", inDir, "-out", outDir, "-name", "actors"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "actors.json"))
	if err != nil {
		t.Fatal(err)
	}
	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:actors.png"}, data)
	if err != nil {
		t.Fatalf("sprite.LoadAtlas: %v", err)
	}
	if fr := atlas.Frames["hero_0.png"]; fr.PivotX != 8 || fr.PivotY != 16 {
		t.Fatalf("hero_0.png pivot = (%v, %v), want (8, 16)", fr.PivotX, fr.PivotY)
	}
	if fr := atlas.Frames["hero_1.png"]; fr.PivotX != 4 || fr.PivotY != 8 {
		t.Fatalf("hero_1.png pivot = (%v, %v), want (4, 8)", fr.PivotX, fr.PivotY)
	}
}
