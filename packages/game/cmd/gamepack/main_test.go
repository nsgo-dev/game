package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGamepackCLI(t *testing.T) {
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

	var stdout, stderr bytes.Buffer
	if err := run([]string{"-in", inDir, "-out", outDir, "-name", "ui", "-padding", "2"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ui.png") || !strings.Contains(stdout.String(), "ui.json") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "ui.png")); err != nil {
		t.Fatalf("missing ui.png: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "ui.json")); err != nil {
		t.Fatalf("missing ui.json: %v", err)
	}

	if err := run(nil, &stdout, &stderr); err == nil {
		t.Fatal("expected error when -in is omitted")
	}
}
