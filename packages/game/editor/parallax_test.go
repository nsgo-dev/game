package editor_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func parallaxSceneDoc() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Parallax Stage",
		VirtualWidth:    320,
		VirtualHeight:   180,
		BackgroundColor: "#141824",
		Camera:          &sceneformat.CameraDoc{X: 160, Y: 90, Zoom: sceneformat.Float32(1)},
		Layers: []sceneformat.LayerDoc{
			{
				Name:      "sky",
				Space:     sceneformat.SpaceWorld,
				Order:     0,
				ParallaxX: sceneformat.Float32(0.5),
				ParallaxY: sceneformat.Float32(0.25),
				Objects: []sceneformat.ObjectDoc{
					{
						ID:     "cloud",
						Name:   "Cloud",
						X:      100,
						Y:      40,
						Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"},
					},
				},
			},
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:     "hero",
						Name:   "Hero",
						X:      160,
						Y:      90,
						Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
					},
				},
			},
			{
				Name:  "hud",
				Space: sceneformat.SpaceScreen,
				Order: 100,
			},
		},
	}
}

// Acceptance: a layer with parallaxX: 0.5 scrolls at half speed in the editor 2D viewport preview as the editor camera pans.
func TestEditorViewportPreviewsParallaxAtHalfSpeed(t *testing.T) {
	ed, err := editor.New(parallaxSceneDoc(), testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	size := core.Size{Width: 320, Height: 180}

	drawAt := func() (cloudX, cloudY, heroX, heroY float32) {
		t.Helper()
		d := &core.Drawing{}
		ed.DrawViewport(d, size)
		imgs := gametest.ImageDraws(d.Ops)
		if len(imgs) != 2 {
			t.Fatalf("drawn images = %d, want 2 (cloud and hero)", len(imgs))
		}
		return float32(imgs[0].Corners[0].X), float32(imgs[0].Corners[0].Y),
			float32(imgs[1].Corners[0].X), float32(imgs[1].Corners[0].Y)
	}

	cX1, cY1, hX1, hY1 := drawAt()
	// Pan 100 canvas points left (moves view +100 right) and 40 canvas points up (moves view +40 down).
	if !ed.PanView2D(-100, -40) {
		t.Fatal("PanView2D returned false")
	}
	cX2, cY2, hX2, hY2 := drawAt()

	// Normal layer moves by (-100, -40); parallax (0.5, 0.25) layer moves by (-50, -10).
	if !near(hX2-hX1, -100, 1e-3) || !near(hY2-hY1, -40, 1e-3) {
		t.Fatalf("world layer moved (%v, %v), want (-100, -40)", hX2-hX1, hY2-hY1)
	}
	if !near(cX2-cX1, -50, 1e-3) || !near(cY2-cY1, -10, 1e-3) {
		t.Fatalf("parallax layer moved (%v, %v), want (-50, -10)", cX2-cX1, cY2-cY1)
	}

	// HitTest hits the cloud at its parallax-previewed position (cX2+8, cY2+8).
	if hit := ed.HitTest(cX2+8, cY2+8); hit != "cloud" {
		t.Fatalf("HitTest at parallax cloud (%v, %v) = %q, want cloud", cX2+8, cY2+8, hit)
	}
	// Placing on the parallax layer drops the object where the press falls in that layer's space.
	ed.SetActiveLayer("sky")
	ed.SetTool(editor.ToolPlace)
	ed.HandleCanvasPress(cX2+20, cY2+10)
	placed, layer := ed.SelectedObject()
	if placed == nil || layer != "sky" || !near(placed.X, 120, 1e-3) || !near(placed.Y, 50, 1e-3) {
		t.Fatalf("placed on parallax layer = (%+v, %q), want at (120, 50) on sky", placed, layer)
	}
}

func TestEditorViewportPreviewsRepeatingParallaxLayer(t *testing.T) {
	doc := parallaxSceneDoc()
	doc.Layers[0].RepeatX = true
	doc.Layers[0].RepeatWidth = 100
	doc.Layers[1].Visible = sceneformat.Bool(false)

	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	// At View2D (160, 90) on 200x100 canvas: parallaxX=0.5 -> pcamX=80 -> visible [-20, 180].
	// Period 100 -> cols -1, 0, 1 -> 3 drawn copies.
	d := &core.Drawing{}
	ed.DrawViewport(d, core.Size{Width: 200, Height: 100})
	imgs := gametest.ImageDraws(d.Ops)
	if len(imgs) != 3 {
		t.Fatalf("repeating parallax preview drew %d copies, want 3", len(imgs))
	}
}

func TestLayerInspectorEditsParallaxAndRepeatUndoably(t *testing.T) {
	// Start from a version 2 scene with no parallax fields to verify version upgrade & exact Undo.
	doc := testSceneDoc()
	doc.Version = 2
	doc.Layers = append(doc.Layers, sceneformat.LayerDoc{Name: "hud", Space: sceneformat.SpaceScreen, Order: 100})

	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("actors")
	beforeYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML before: %v", err)
	}

	app := startEditorView(t, ed)

	// With no object selected, the inspector shows the active layer ("actors").
	if got := app.Find("id:inspector-layer-name").Text; got != "actors" {
		t.Fatalf("inspector layer name = %q, want actors", got)
	}

	// Click the "floor" layer row in the scene tree: inspector switches to "floor".
	app.Press("id:scene-tree-layer-floor")
	if got := app.Find("id:inspector-layer-name").Text; got != "floor" {
		t.Fatalf("inspector layer name after selecting floor = %q, want floor", got)
	}

	// Edit parallax X via the inspector NumberInput and toggle repeat X via the Checkbox.
	app.Post(func() {
		if err := ed.SetLayerParallax("floor", 0.5, 0.25); err != nil {
			t.Errorf("SetLayerParallax: %v", err)
		}
		if err := ed.SetLayerRepeat("floor", true, false, 128, 0); err != nil {
			t.Errorf("SetLayerRepeat: %v", err)
		}
	})
	app.Settle()

	// Also toggle repeat Y through the mounted UI checkbox.
	app.Press("id:inspector-layer-repeat-y")

	if ed.Doc.Version != sceneformat.CurrentVersion {
		t.Fatalf("Doc.Version = %d, want %d after editing parallax", ed.Doc.Version, sceneformat.CurrentVersion)
	}
	floor := ed.Doc.Layers[0]
	if floor.EffectiveParallaxX() != 0.5 || floor.EffectiveParallaxY() != 0.25 || !floor.RepeatX || !floor.RepeatY || floor.RepeatWidth != 128 {
		t.Fatalf("floor layer = %+v", floor)
	}
	if bl := ed.Built.Layer("floor"); bl == nil || bl.ParallaxX != 0.5 || bl.ParallaxY != 0.25 || !bl.RepeatX || !bl.RepeatY || bl.RepeatWidth != 128 {
		t.Fatalf("built floor layer = %+v", bl)
	}

	// Screen-space layer disables parallax and shows explanatory note.
	app.Press("id:scene-tree-layer-hud")
	if got := app.Find("id:inspector-layer-name").Text; got != "hud" {
		t.Fatalf("inspector layer name = %q, want hud", got)
	}
	app.Find("id:inspector-layer-screen-note")

	// Undo all 3 edits (repeatY toggle, SetLayerRepeat, SetLayerParallax) and verify byte-for-byte restoration of the v2 YAML!
	for range 3 {
		app.Press("id:toolbar-undo")
	}
	if ed.Doc.Version != 2 {
		t.Fatalf("Doc.Version after Undo = %d, want 2", ed.Doc.Version)
	}
	afterUndo, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after Undo: %v", err)
	}
	if !bytes.Equal(afterUndo, beforeYAML) {
		t.Fatalf("YAML after Undo differs!\ngot:\n%s\nwant:\n%s", afterUndo, beforeYAML)
	}

	// Redo brings parallax and version 3 back cleanly.
	for range 3 {
		app.Press("id:toolbar-redo")
	}
	if ed.Doc.Version != sceneformat.CurrentVersion || ed.Doc.Layers[0].EffectiveParallaxX() != 0.5 || !ed.Doc.Layers[0].RepeatY {
		t.Fatalf("after Redo: version=%d layer=%+v", ed.Doc.Version, ed.Doc.Layers[0])
	}

	// Validation errors are refused.
	if err := ed.SetLayerParallax("floor", float32(math.NaN()), 1); err == nil {
		t.Fatal("SetLayerParallax accepted NaN")
	}
	if err := ed.SetLayerRepeat("floor", true, false, -1, 0); err == nil {
		t.Fatal("SetLayerRepeat accepted negative repeatWidth")
	}
	if err := ed.SetLayerParallax("missing_layer", 0.5, 1); err == nil {
		t.Fatal("SetLayerParallax accepted unknown layer")
	}
}
