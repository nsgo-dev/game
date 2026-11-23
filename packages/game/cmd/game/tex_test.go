package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
)

func makeTestImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x * 255) / max(1, w-1)),
				G: uint8((y * 255) / max(1, h-1)),
				B: uint8(((x + y) * 127) / max(1, w+h-2)),
				A: 255,
			})
		}
	}
	return img
}

func TestTexEncodesPNGAndJPEG(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "albedo.png")
	jpgPath := filepath.Join(dir, "photo.jpg")

	img := makeTestImage(32, 16)
	f1, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f1, img); err != nil {
		t.Fatal(err)
	}
	_ = f1.Close()

	f2, err := os.Create(jpgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f2, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	_ = f2.Close()

	cases := []struct {
		name     string
		args     []string
		outPath  string
		wantVk   uint32
		wantMips uint32
	}{
		{
			name:     "bc1 mipmaps srgb from png",
			args:     []string{"-format", "bc1", "-mipmaps", "-srgb", "-o", filepath.Join(dir, "out_bc1.ktx2"), pngPath},
			outPath:  filepath.Join(dir, "out_bc1.ktx2"),
			wantVk:   ktx2.VkFormatBC1RGBASRGBBlock,
			wantMips: 6, // 32->16->8->4->2->1
		},
		{
			name:     "bc3 linear from png",
			args:     []string{"-format", "bc3", "-o", filepath.Join(dir, "out_bc3.ktx2"), pngPath},
			outPath:  filepath.Join(dir, "out_bc3.ktx2"),
			wantVk:   ktx2.VkFormatBC3UnormBlock,
			wantMips: 1,
		},
		{
			name:     "etc2 mipmaps from jpg default -o",
			args:     []string{"-format", "etc2", "-mipmaps", jpgPath},
			outPath:  filepath.Join(dir, "photo.ktx2"),
			wantVk:   ktx2.VkFormatETC2R8G8B8UnormBlk,
			wantMips: 6,
		},
		{
			name:     "rgba8 via -in flag",
			args:     []string{"-format", "rgba8", "-in", pngPath, "-out", filepath.Join(dir, "out_rgba.ktx2")},
			outPath:  filepath.Join(dir, "out_rgba.ktx2"),
			wantVk:   ktx2.VkFormatR8G8B8A8Unorm,
			wantMips: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, stderr := newTestApp(t)
			if code := run(append([]string{"tex"}, tc.args...), a); code != 0 {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}
			raw, err := os.ReadFile(tc.outPath)
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			if err := ktx2.Validate(raw); err != nil {
				t.Fatalf("Validate failed: %v", err)
			}
			h, levels, err := ktx2.DecodeHeader(raw)
			if err != nil {
				t.Fatalf("DecodeHeader failed: %v", err)
			}
			if h.VkFormat != tc.wantVk {
				t.Fatalf("VkFormat = %d, want %d", h.VkFormat, tc.wantVk)
			}
			if h.LevelCount != tc.wantMips || uint32(len(levels)) != tc.wantMips {
				t.Fatalf("LevelCount = %d (%d entries), want %d", h.LevelCount, len(levels), tc.wantMips)
			}
			if h.PixelWidth != 32 || h.PixelHeight != 16 {
				t.Fatalf("dimensions = %dx%d, want 32x16", h.PixelWidth, h.PixelHeight)
			}
		})
	}
}

func TestTexErrorCases(t *testing.T) {
	a, _, _ := newTestApp(t)
	if code := run([]string{"tex"}, a); code != 2 {
		t.Fatalf("missing input exit=%d, want 2", code)
	}
	a, _, _ = newTestApp(t)
	if code := run([]string{"tex", "-in", "a.png", "b.png"}, a); code != 2 {
		t.Fatalf("-in and positional input exit=%d, want 2", code)
	}
	a, _, stderr := newTestApp(t)
	if code := run([]string{"tex", "-format", "astc", "a.png"}, a); code != 2 {
		t.Fatalf("unsupported format exit=%d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unsupported format") {
		t.Fatalf("stderr = %q, want unsupported format error", stderr)
	}
	a, _, _ = newTestApp(t)
	if code := run([]string{"tex", "-format", "bc1", filepath.Join(t.TempDir(), "missing.png")}, a); code != 1 {
		t.Fatalf("missing file exit=%d, want 1", code)
	}
}
