package assetbuild_test

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tooling/assetbuild"
	"github.com/nsgo-dev/nsgo/core"
)

func writeSheetPNG(t *testing.T, p string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSidecarCollisionAndPointsBakeIntoAtlas(t *testing.T) {
	root := t.TempDir()
	// 32x16 sheet of two 16x16 cells:
	// cell 0: 8x12 solid box at [4,12) x [2,14)
	// cell 1: L-shape
	sheet := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for y := 2; y < 14; y++ {
		for x := 4; x < 12; x++ {
			sheet.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	for y := 2; y < 14; y++ {
		for x := 18; x < 22; x++ {
			sheet.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	for y := 10; y < 14; y++ {
		for x := 18; x < 30; x++ {
			sheet.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	writeSheetPNG(t, filepath.Join(root, "art", "hero.png"), sheet)
	sidecar := `version: 1
image:
  slice: {width: 16, height: 16}
  collision:
    mode: alpha
    tolerance: 0
    maxVertices: 8
  points:
    feet: [8, 16]
    muzzle: [12, 6]
  cellPoints:
    - cell: 1
      points:
        muzzle: [15, 12]
`
	if err := os.WriteFile(filepath.Join(root, "art", "hero.png.import.yaml"), []byte(sidecar), 0o644); err != nil {
		t.Fatal(err)
	}

	assets := &project.Assets{
		Atlases: []project.AtlasTarget{{In: "art", Out: "assets/atlases", Name: "actors"}},
	}
	b := &assetbuild.Builder{Root: root}
	rep, err := b.Build(context.Background(), assets)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rep.Count(assetbuild.Built) != 1 {
		t.Fatalf("built = %d, want 1", rep.Count(assetbuild.Built))
	}

	jsonBytes, err := os.ReadFile(filepath.Join(root, "assets", "atlases", "actors.json"))
	if err != nil {
		t.Fatal(err)
	}
	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:actors.png"}, jsonBytes)
	if err != nil {
		t.Fatalf("LoadAtlas: %v", err)
	}
	f0 := atlas.Frames["hero_0.png"]
	if len(f0.Shapes) != 1 || len(f0.Shapes[0]) != 4 {
		t.Fatalf("hero_0 shapes = %+v, want 1 rectangle", f0.Shapes)
	}
	if pt, ok := f0.Point("muzzle"); !ok || pt.X != 12 || pt.Y != 6 {
		t.Fatalf("hero_0 muzzle = %+v, %v", pt, ok)
	}
	if pt, ok := f0.Point("feet"); !ok || pt.X != 8 || pt.Y != 16 {
		t.Fatalf("hero_0 feet = %+v, %v", pt, ok)
	}

	f1 := atlas.Frames["hero_1.png"]
	if len(f1.Shapes) != 2 {
		t.Fatalf("hero_1 L-shape got %d convex shapes, want 2: %+v", len(f1.Shapes), f1.Shapes)
	}
	if pt, ok := f1.Point("muzzle"); !ok || pt.X != 15 || pt.Y != 12 {
		t.Fatalf("hero_1 muzzle = %+v, %v; want (15, 12)", pt, ok)
	}
	if pt, ok := f1.Point("feet"); !ok || pt.X != 8 || pt.Y != 16 {
		t.Fatalf("hero_1 inherited feet = %+v, %v; want (8, 16)", pt, ok)
	}

	// Second build is up to date.
	rep2, err := b.Build(context.Background(), assets)
	if err != nil || rep2.Count(assetbuild.UpToDate) != 1 {
		t.Fatalf("second Build: rep=%+v err=%v", rep2, err)
	}
}

func TestTextureTargetRefusesCollisionAndPoints(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sidecar string
		want    string
	}{
		{"collision on texture", "version: 1\nimage:\n  collision: {mode: alpha}\n", "collision"},
		{"points on texture", "version: 1\nimage:\n  points: {muzzle: [4, 4]}\n", "points"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
			img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
			writeSheetPNG(t, filepath.Join(root, "tex", "albedo.png"), img)
			if err := os.WriteFile(filepath.Join(root, "tex", "albedo.png.import.yaml"), []byte(tc.sidecar), 0o644); err != nil {
				t.Fatal(err)
			}
			assets := &project.Assets{
				Textures: []project.TextureTarget{{In: "tex/albedo.png", Out: "assets/albedo.ktx2"}},
			}
			_, err := (&assetbuild.Builder{Root: root}).Build(context.Background(), assets)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}
