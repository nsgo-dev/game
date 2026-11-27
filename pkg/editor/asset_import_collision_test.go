package editor_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func setupImageOverlayProject(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "art"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: sprite-test
id: com.example.spritetest
appVersion: 0.1.0
window:
  title: Sprite Test
  width: 640
  height: 360
assets:
  atlases:
    - in: art
      out: assets/atlases
      name: actors
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// 32x32 L-shaped character sprite.
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 4; y < 28; y++ {
		for x := 4; x < 14; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 120, G: 180, B: 240, A: 255})
		}
	}
	for y := 18; y < 28; y++ {
		for x := 4; x < 28; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 120, G: 180, B: 240, A: 255})
		}
	}
	f, err := os.Create(filepath.Join(dir, "art", "hero.png"))
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
	ed, err := editor.New(&sceneformat.SceneDoc{Version: 2}, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(filepath.Join(dir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	return ed, "art/hero.png"
}

func TestImageImportCollisionAndPointDragOverlay(t *testing.T) {
	ed, rel := setupImageOverlayProject(t)

	// Render BEFORE overlay (no collision, no points).
	const cw, ch float32 = 240, 180
	var beforeDraw core.Drawing
	ed.DrawImageImportPreview(&beforeDraw, core.Size{Width: core.Points(cw), Height: core.Points(ch)}, rel)
	beforeImg := gametest.RenderOps(int(cw), int(ch), beforeDraw.Ops)

	// Enable alpha collision tracing and add named points.
	if err := ed.SetImageImportDefaultCollision(rel, "alpha", 0, 8, nil); err != nil {
		t.Fatalf("SetImageImportDefaultCollision: %v", err)
	}
	muzzle := importsettings.Point2D{26, 22}
	feet := importsettings.Point2D{10, 28}
	if err := ed.SetImageImportPoint(rel, "muzzle", &muzzle); err != nil {
		t.Fatalf("SetImageImportPoint muzzle: %v", err)
	}
	if err := ed.SetImageImportPoint(rel, "feet", &feet); err != nil {
		t.Fatalf("SetImageImportPoint feet: %v", err)
	}

	polys, err := ed.PreviewImageCollisionPolygons(rel, -1)
	if err != nil {
		t.Fatalf("PreviewImageCollisionPolygons: %v", err)
	}
	if len(polys) != 2 {
		t.Fatalf("traced L-shape polygons = %d, want 2: %+v", len(polys), polys)
	}

	// Render AFTER overlay and verify visible pixels changed (green traced outline + point markers).
	var afterDraw core.Drawing
	ed.DrawImageImportPreview(&afterDraw, core.Size{Width: core.Points(cw), Height: core.Points(ch)}, rel)
	afterImg := gametest.RenderOps(int(cw), int(ch), afterDraw.Ops)
	diffPixels := 0
	for i := range beforeImg.Pix {
		if beforeImg.Pix[i] != afterImg.Pix[i] {
			diffPixels++
		}
	}
	if diffPixels < 100 {
		t.Fatalf("expected visible overlay diff in preview, got only %d changed channel bytes", diffPixels)
	}

	// Drag the "muzzle" point marker interactively on the preview canvas.
	mx, my, ok := ed.ImageImportPreviewPointPos(rel, -1, muzzle, cw, ch)
	if !ok {
		t.Fatal("ImageImportPreviewPointPos returned false")
	}
	if !ed.BeginImagePivotDrag(rel, mx, my, cw, ch) {
		t.Fatal("BeginImagePivotDrag did not pick up muzzle marker")
	}
	if !ed.ImagePointDragging() {
		t.Fatal("expected ImagePointDragging() == true when grabbing named point marker")
	}
	targetPt := importsettings.Point2D{20, 12}
	tx, ty, ok := ed.ImageImportPreviewPointPos(rel, -1, targetPt, cw, ch)
	if !ok {
		t.Fatal("target ImageImportPreviewPointPos failed")
	}
	ed.UpdateImagePivotDrag(tx, ty)
	if err := ed.EndImagePivotDrag(); err != nil {
		t.Fatalf("EndImagePivotDrag: %v", err)
	}
	if ed.ImagePointDragging() {
		t.Fatal("expected ImagePointDragging() == false after release")
	}

	info, err := ed.InspectAssetImport(rel)
	if err != nil {
		t.Fatal(err)
	}
	pts, _ := info.EffectiveImagePoints()
	if got := pts["muzzle"]; got != targetPt {
		t.Fatalf("dragged muzzle = %+v, want %+v", got, targetPt)
	}

	// Undo restores the original muzzle coordinate in one History step.
	ed.History.Undo()
	info, _ = ed.InspectAssetImport(rel)
	pts, _ = info.EffectiveImagePoints()
	if got := pts["muzzle"]; got != muzzle {
		t.Fatalf("after Undo muzzle = %+v, want %+v", got, muzzle)
	}
}
