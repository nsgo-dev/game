package editor_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
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
		undoInUI(app, ed)
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
		redoInUI(app, ed)
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

func findStrokedRectCanvasPos(ops []core.DrawOp, wantColor core.Color, wantW, wantH float32) (float32, float32, bool) {
	type xf struct{ a, b, c, d, tx, ty float32 }
	mul := func(m, n xf) xf {
		return xf{
			a:  m.a*n.a + m.c*n.b,
			b:  m.b*n.a + m.d*n.b,
			c:  m.a*n.c + m.c*n.d,
			d:  m.b*n.c + m.d*n.d,
			tx: m.a*n.tx + m.c*n.ty + m.tx,
			ty: m.b*n.tx + m.d*n.ty + m.ty,
		}
	}
	cur := xf{a: 1, d: 1}
	var stack []xf
	var strokeCol core.Color
	var hasRect bool
	var rx, ry, rw, rh float32
	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, cur)
		case core.DrawRestore:
			if len(stack) > 0 {
				cur = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case core.DrawTransform:
			cur = mul(cur, xf{a: op.X1, b: op.Y1, c: op.X2, d: op.Y2, tx: op.X, ty: op.Y})
		case core.DrawTranslate:
			cur = mul(cur, xf{a: 1, d: 1, tx: op.X, ty: op.Y})
		case core.DrawStrokeColor:
			strokeCol = op.Color
		case core.DrawBeginPath:
			hasRect = false
		case core.DrawRect:
			hasRect = true
			rx, ry, rw, rh = op.X, op.Y, op.W, op.H
		case core.DrawStroke:
			if hasRect && strokeCol == wantColor && near(rw, wantW, 1e-3) && near(rh, wantH, 1e-3) {
				return cur.a*rx + cur.c*ry + cur.tx, cur.b*rx + cur.d*ry + cur.ty, true
			}
		}
	}
	return 0, 0, false
}

func TestEditorParallaxAlignsGizmoTileHoverAndPhysicsDebug(t *testing.T) {
	doc := parallaxSceneDoc()
	// Add a 2D collider on the parallax "cloud" object to verify physics debug alignment.
	doc.Layers[0].Objects[0].Components = map[string]any{
		"collider2d": map[string]any{
			"version": 1,
			"shape":   "box",
			"width":   16.0,
			"height":  16.0,
		},
	}
	// Add an inline tilemap on the parallax "sky" layer to verify tile hover alignment.
	doc.Layers[0].Tilemap = &sceneformat.TilemapDoc{
		TileWidth:  16,
		TileHeight: 16,
		Width:      4,
		Height:     4,
		Tileset:    "tiles",
		Data:       make([]int, 16),
	}

	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	size := core.Size{Width: 320, Height: 180}
	ed.SetCanvasSize(320, 180)

	findColliderCanvasPos := func() (float32, float32) {
		t.Helper()
		ed.SetShowPhysicsDebug(true)
		defer ed.SetShowPhysicsDebug(false)
		d := &core.Drawing{}
		ed.DrawViewport(d, size)
		x, y, ok := findStrokedRectCanvasPos(d.Ops, ui.Alpha(ed.SpecPalette(nil).Accent.Fg, 0.9), 16, 16)
		if !ok {
			t.Fatal("physics debug StrokeRect not found")
		}
		return x, y
	}

	px1, py1 := findColliderCanvasPos()

	// Pan (-100, -40) so View2D moves (+100, +40); parallax (0.5, 0.25) layer shifts by (-50, -10) on canvas.
	if !ed.PanView2D(-100, -40) {
		t.Fatal("PanView2D returned false")
	}
	px2, py2 := findColliderCanvasPos()
	if !near(px2-px1, -50, 1e-3) || !near(py2-py1, -10, 1e-3) {
		t.Fatalf("physics debug box moved (%v, %v), want (-50, -10)", px2-px1, py2-py1)
	}

	// Verify 2D gizmo picks and drags "cloud" at its parallax-shifted canvas origin:
	// View2D=(260, 130), parallax=(0.5, 0.25) -> pcam=(130, 32.5); cloud=(100, 40) -> canvas=(160+100-130, 90+40-32.5)=(130, 97.5).
	ed.SetTool(editor.ToolSelect)
	ed.SelectObject("cloud")
	if !ed.Gizmo2DShown() {
		t.Fatal("Gizmo2DShown = false, want true")
	}
	if h := ed.PickGizmo2D(130, 97.5); h != editor.Handle2DMove {
		t.Fatalf("PickGizmo2D at parallax cloud origin (130, 97.5) = %v, want Handle2DMove", h)
	}
	// Un-parallaxed 1x position (160+100-260=0, 90+40-130=0) must NOT hit the gizmo.
	if h := ed.PickGizmo2D(0, 0); h != editor.Handle2DNone {
		t.Fatalf("PickGizmo2D at un-parallaxed (0, 0) = %v, want Handle2DNone", h)
	}
	if h := ed.BeginDrag2D(130, 97.5); h != editor.Handle2DMove {
		t.Fatalf("BeginDrag2D = %v, want Handle2DMove", h)
	}
	ed.UpdateDrag2D(150, 107.5, false)
	ed.EndDrag2D()
	cloud, _ := ed.SelectedObject()
	if cloud == nil || !near(cloud.X, 120, 1e-3) || !near(cloud.Y, 50, 1e-3) {
		t.Fatalf("cloud after gizmo drag = (%+v), want (120, 50)", cloud)
	}

	// Verify tile hover overlay draws at the parallax-shifted tile cell coordinates.
	// Tile (1, 1) on "sky" is layer rect (16, 16, 16, 16) -> canvas top-left (160+16-130, 90+16-32.5) = (46, 73.5).
	ed.SetActiveLayer("sky")
	ed.SetTool(editor.ToolPaintTile)
	ed.SetTileHover(54, 81.5, false)
	col, row, ok := ed.HoveredTile()
	if !ok || col != 1 || row != 1 {
		t.Fatalf("HoveredTile = (%d, %d, %v), want (1, 1, true)", col, row, ok)
	}
	d := &core.Drawing{}
	ed.DrawViewport(d, size)
	hx, hy, foundHover := findStrokedRectCanvasPos(d.Ops, ed.SpecPalette(nil).Accent.Fg, 16, 16)
	if !foundHover {
		t.Fatal("tile hover StrokeRect not found")
	}
	if !near(hx, 46, 1e-3) || !near(hy, 73.5, 1e-3) {
		t.Fatalf("tile hover drawn at (%v, %v), want (46, 73.5)", hx, hy)
	}
}

func TestLayerParallaxAndRepeatRefuseInheritedAndFinishTileStroke(t *testing.T) {
	// 1. Inherited layer extending a base scene refuses SetLayerParallax / SetLayerRepeat and disables controls in InspectorView.
	baseYAML := `version: 3
name: Base
virtualWidth: 320
virtualHeight: 180
layers:
  - name: sky
    parallaxX: 0.5
    repeatX: true
    repeatWidth: 320
`
	childYAML := `version: 3
name: Child
inherits: scenes/base.scene.yaml
layers:
  - name: sky
    objects:
      - id: extra
        x: 10
        "y": 10
  - name: own
`
	resolver := sceneFiles{
		AtlasMap: testResolver(),
		files:    map[string]string{"scenes/base.scene.yaml": baseYAML},
	}
	doc, err := sceneformat.Parse([]byte(childYAML))
	if err != nil {
		t.Fatalf("Parse child: %v", err)
	}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New child: %v", err)
	}
	ed.SetActiveLayer("sky")

	if err := ed.SetLayerParallax("sky", 0.25, 1); err != nil {
		t.Fatalf("SetLayerParallax on inherited layer should write layerOverrides: %v", err)
	}
	if err := ed.SetLayerRepeat("sky", false, false, 0, 0); err != nil {
		t.Fatalf("SetLayerRepeat on inherited layer should write layerOverrides: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("BuildErr after inherited layer overrides = %v, want nil", ed.BuildErr)
	}
	if lo, ok := ed.Doc.LayerOverrides["sky"]; !ok || lo.ParallaxX == nil || *lo.ParallaxX != 0.25 || lo.RepeatX == nil || *lo.RepeatX {
		t.Fatalf("LayerOverrides[sky] = %+v, want parallaxX=0.25 repeatX=false", lo)
	}

	app := startEditorView(t, ed)
	app.Find("id:inspector-layer-inherited-note")
	for _, id := range []string{
		"inspector-layer-parallax-x",
		"inspector-layer-parallax-y",
		"inspector-layer-repeat-x",
		"inspector-layer-repeat-y",
		"inspector-layer-repeat-width",
		"inspector-layer-repeat-height",
	} {
		if !app.Enabled("id:" + id) {
			t.Errorf("%s should be enabled on an inherited layer via layerOverrides", id)
		}
	}
	app.Find("id:inspector-layer-override-dot-parallaxX")
	app.Find("id:inspector-layer-revert-parallaxX")
	app.Press("id:inspector-layer-revert-all")
	if len(ed.Doc.LayerOverrides) != 0 {
		t.Fatalf("LayerOverrides after Revert All = %+v, want empty", ed.Doc.LayerOverrides)
	}

	// Own layer in the same inheriting scene remains editable (both in UI and API).
	app.Post(func() { ed.SetActiveLayer("own") })
	app.Settle()
	if !app.Enabled("id:inspector-layer-repeat-x") {
		t.Fatal("inspector-layer-repeat-x is disabled on the scene's own layer")
	}
	if err := ed.SetLayerParallax("own", 0.75, 1); err != nil {
		t.Fatalf("SetLayerParallax on own layer: %v", err)
	}
	if ed.Doc.Layers[1].EffectiveParallaxX() != 0.75 {
		t.Fatalf("own layer parallaxX = %v, want 0.75", ed.Doc.Layers[1].EffectiveParallaxX())
	}

	// 2. Starting a tile stroke and then editing parallax/repeat finishes the stroke cleanly first.
	strokeDoc := testSceneDoc()
	strokeEd, err := editor.New(strokeDoc, testResolver())
	if err != nil {
		t.Fatalf("editor.New strokeDoc: %v", err)
	}
	strokeEd.SetCanvasSize(320, 180)
	strokeEd.SetActiveLayer("floor")
	strokeEd.SetTool(editor.ToolPaintTile)
	strokeEd.ActiveTileID = 1
	cx, cy := strokeEd.WorldToCanvas(8, 8)
	if !strokeEd.BeginTileStroke(editor.TileStrokePaint, cx, cy) {
		t.Fatal("BeginTileStroke returned false")
	}
	if _, active := strokeEd.TileStrokeActive(); !active {
		t.Fatal("expected active tile stroke")
	}
	if err := strokeEd.SetLayerParallax("floor", 0.5, 1); err != nil {
		t.Fatalf("SetLayerParallax during stroke: %v", err)
	}
	if _, active := strokeEd.TileStrokeActive(); active {
		t.Fatal("SetLayerParallax left tile stroke active")
	}
	if !strokeEd.BeginTileStroke(editor.TileStrokePaint, cx, cy) {
		t.Fatal("second BeginTileStroke returned false")
	}
	if err := strokeEd.SetLayerRepeat("floor", true, false, 64, 0); err != nil {
		t.Fatalf("SetLayerRepeat during stroke: %v", err)
	}
	if _, active := strokeEd.TileStrokeActive(); active {
		t.Fatal("SetLayerRepeat left tile stroke active")
	}
}
