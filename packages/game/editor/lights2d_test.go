package editor_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/lights2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newLightingTestEditor(t *testing.T) *editor.Editor {
	t.Helper()
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Lighting Test",
		VirtualWidth:    320,
		VirtualHeight:   180,
		BackgroundColor: "#141824",
		AmbientColor:    "rgba(12, 16, 30, 0.85)",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "main",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{
						ID: "lamp",
						X:  80,
						Y:  90,
						Components: map[string]any{
							"light2d": map[string]any{
								"version":     1,
								"kind":        "point",
								"color":       "#ffaa44",
								"radius":      100.0,
								"intensity":   1.0,
								"castShadows": true,
								"enabled":     true,
							},
						},
					},
					{
						ID: "spot",
						X:  220,
						Y:  90,
						Components: map[string]any{
							"light2d": map[string]any{
								"version":     1,
								"kind":        "spot",
								"color":       "#66ccff",
								"radius":      90.0,
								"intensity":   1.0,
								"direction":   0.0,
								"angle":       1.0,
								"castShadows": true,
								"enabled":     true,
							},
						},
					},
					{
						ID: "pillar",
						X:  140,
						Y:  90,
						Components: map[string]any{
							"occluder2d": map[string]any{
								"version": 1,
								"shape":   "rect",
								"width":   24.0,
								"height":  40.0,
								"enabled": true,
							},
						},
					},
					{
						ID: "poly_wall",
						X:  160,
						Y:  140,
						Components: map[string]any{
							"occluder2d": map[string]any{
								"version": 1,
								"shape":   "polygon",
								"points":  [][2]float64{{-20, -10}, {20, -10}, {20, 10}, {-20, 10}},
								"enabled": true,
							},
						},
					},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	return ed
}

// Acceptance #1 & #2: A light and an occluder placed in the editor cast the exact same shadow
// in the editor preview and in the game, and the scene file round-trips unchanged.
func TestEditorPlacedLightAndOccluderMatchGameShadowAndRoundTrip(t *testing.T) {
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAmbientColor("rgba(10, 14, 28, 0.85)"); err != nil {
		t.Fatalf("SetAmbientColor: %v", err)
	}
	lightID, err := ed.PlaceObject("main", sceneformat.ObjectDoc{
		ID: "torch",
		X:  100,
		Y:  100,
		Components: map[string]any{
			"light2d": map[string]any{
				"version":     1,
				"kind":        "point",
				"color":       "#ffbb55",
				"radius":      160.0,
				"intensity":   1.0,
				"castShadows": true,
				"enabled":     true,
			},
		},
	})
	if err != nil {
		t.Fatalf("PlaceObject light: %v", err)
	}
	occID, err := ed.PlaceObject("main", sceneformat.ObjectDoc{
		ID: "wall",
		X:  160,
		Y:  100,
		Components: map[string]any{
			"occluder2d": map[string]any{
				"version": 1,
				"shape":   "polygon",
				"points":  [][2]float64{{-10, -25}, {10, -25}, {10, 25}, {-10, 25}},
				"enabled": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("PlaceObject occluder: %v", err)
	}
	if lightID != "torch" || occID != "wall" {
		t.Fatalf("unexpected IDs: %q, %q", lightID, occID)
	}

	// Acceptance #2: round-trip unchanged through SaveYAML -> OpenYAML -> SaveYAML.
	saved1, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML 1: %v", err)
	}
	if err := ed.OpenYAML(saved1); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	saved2, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML 2: %v", err)
	}
	if !bytes.Equal(saved1, saved2) {
		t.Fatalf("scene round-trip differed:\nsaved1:\n%s\nsaved2:\n%s", saved1, saved2)
	}

	// Build the same saved scene as the game does.
	gameDoc, err := sceneformat.Parse(saved1)
	if err != nil {
		t.Fatalf("game Parse: %v", err)
	}
	gameBuilt, err := sceneformat.Build(gameDoc, nil)
	if err != nil {
		t.Fatalf("game Build: %v", err)
	}
	editorBuilt := ed.ActiveScene()
	if editorBuilt == nil || editorBuilt.Lights2D == nil || gameBuilt.Lights2D == nil {
		t.Fatal("expected Lights2D on both editor preview and game scene")
	}

	// Exact same visibility polygon and EvaluateAt illumination across lit and shadowed points.
	edLight := editorBuilt.Lights2D.Lights[0]
	gmLight := gameBuilt.Lights2D.Lights[0]
	edPoly := lights2d.VisibilityPolygon(core.Point{X: core.Points(edLight.X), Y: core.Points(edLight.Y)}, edLight.Radius, editorBuilt.Lights2D.Occluders)
	gmPoly := lights2d.VisibilityPolygon(core.Point{X: core.Points(gmLight.X), Y: core.Points(gmLight.Y)}, gmLight.Radius, gameBuilt.Lights2D.Occluders)
	if len(edPoly) != len(gmPoly) || len(edPoly) < 3 {
		t.Fatalf("visibility polygon vertex count mismatch: editor=%d game=%d", len(edPoly), len(gmPoly))
	}
	for i := range edPoly {
		if edPoly[i] != gmPoly[i] {
			t.Fatalf("visibility polygon vertex %d mismatch: editor=%v game=%v", i, edPoly[i], gmPoly[i])
		}
	}
	// Sample in front of the wall (lit) and behind the wall (shadowed).
	for _, sample := range [][2]float32{{130, 100}, {210, 100}, {100, 60}, {190, 110}} {
		er, eg, eb := editorBuilt.Lights2D.EvaluateAt(sample[0], sample[1])
		gr, gg, gb := gameBuilt.Lights2D.EvaluateAt(sample[0], sample[1])
		if er != gr || eg != gg || eb != gb {
			t.Fatalf("EvaluateAt(%v,%v) mismatch: editor=(%v,%v,%v) game=(%v,%v,%v)", sample[0], sample[1], er, eg, eb, gr, gg, gb)
		}
	}
	litR, _, _ := editorBuilt.Lights2D.EvaluateAt(130, 100)
	shadR, _, _ := editorBuilt.Lights2D.EvaluateAt(210, 100)
	if litR <= shadR+0.2 {
		t.Fatalf("expected wall to cast shadow: lit=%v shadowed=%v", litR, shadR)
	}

	// Verify rendered canvas ops: both the editor viewport (with preview ON) and the game draw
	// the exact same radial light gradient and shadow visibility clip, while toggling preview OFF removes them.
	vw, vh := ed.ViewportSize()
	wPt, hPt := core.Points(vw), core.Points(vh)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "editor-lit", Style: core.Style{Width: core.Pt(wPt), Height: core.Pt(hPt)}, Draw: func(d *core.Drawing, sz core.Size) {
				ed.SetShowLightingPreview(true)
				ed.DrawViewport(d, sz)
			}},
			core.Canvas{TestID: "editor-unlit", Style: core.Style{Width: core.Pt(wPt), Height: core.Pt(hPt)}, Draw: func(d *core.Drawing, sz core.Size) {
				ed.SetShowLightingPreview(false)
				ed.DrawViewport(d, sz)
			}},
			core.Canvas{TestID: "game-lit", Style: core.Style{Width: core.Pt(wPt), Height: core.Pt(hPt)}, Draw: func(d *core.Drawing, _ core.Size) {
				gameBuilt.Draw(d)
			}},
		}}
	})

	countGrad := func(ops []core.DrawOp) int {
		n := 0
		for _, op := range ops {
			if op.Kind == core.DrawFillGradient && op.Gradient != nil && op.Gradient.Radial {
				n++
			}
		}
		return n
	}
	if n := countGrad(app.Find("editor-lit").Ops); n != 1 {
		t.Fatalf("editor-lit radial light gradients = %d, want 1", n)
	}
	if n := countGrad(app.Find("game-lit").Ops); n != 1 {
		t.Fatalf("game-lit radial light gradients = %d, want 1", n)
	}
	if n := countGrad(app.Find("editor-unlit").Ops); n != 0 {
		t.Fatalf("editor-unlit radial light gradients = %d, want 0 when lighting preview is off", n)
	}
}

func TestLightingPreviewToolbarToggleAndCommand(t *testing.T) {
	ed := newLightingTestEditor(t)
	app := mountEditor(t, ed)

	if !ed.ShowLightingPreview {
		t.Fatal("expected ShowLightingPreview to default to true")
	}
	app.Post(func() { ed.SetShowLightingPreview(false) })
	app.Settle()
	if ed.ShowLightingPreview {
		t.Fatal("expected view-lighting-preview button to toggle ShowLightingPreview off")
	}
	if err := ed.ExecuteCommand(editor.CmdToggleLightingPreview); err != nil {
		t.Fatalf("ExecuteCommand(%q): %v", editor.CmdToggleLightingPreview, err)
	}
	if !ed.ShowLightingPreview {
		t.Fatal("expected CmdToggleLightingPreview to toggle ShowLightingPreview back on")
	}
}

func TestLightRadiusAndSpotAngleHandlesUndoRedo(t *testing.T) {
	t.Run("point light radius handle", func(t *testing.T) {
		ed := newLightingTestEditor(t)
		ed.SelectObject("lamp")
		app := mountEditor(t, ed)
		before := saveYAML(t, ed)

		// Lamp is at (80, 90) with radius 100 -> radius handle is at world (180, 90).
		hx, hy := ed.WorldToCanvas(180, 90)
		if h := ed.PickGizmo2D(hx, hy); h != editor.Handle2DLightRadius {
			t.Fatalf("PickGizmo2D at radius handle = %v, want Light Radius", h)
		}
		// Drag +40 points along +X -> new radius 140.
		app.DragAt("id:editor-viewport", pt(hx, hy), pt(40, 0))
		lamp, _, _ := ed.FindObject("lamp")
		gotR, _ := lamp.Components["light2d"].(map[string]any)["radius"].(float64)
		if math.Abs(gotR-140) > 0.5 {
			t.Fatalf("light2d.radius = %v, want ~140", gotR)
		}
		checkOneUndoStep(t, app, ed, before)
	})

	t.Run("spotlight cone angle handle", func(t *testing.T) {
		ed := newLightingTestEditor(t)
		ed.SelectObject("spot")
		app := mountEditor(t, ed)
		before := saveYAML(t, ed)

		// Spot is at (220, 90), radius 90, direction 0, angle 1.0 -> lower cone edge at angle +0.5.
		s, c := math.Sincos(0.5)
		ex, ey := ed.WorldToCanvas(220+90*float32(c), 90+90*float32(s))
		if h := ed.PickGizmo2D(ex, ey); h != editor.Handle2DLightSpotAngle {
			t.Fatalf("PickGizmo2D at spot cone edge = %v, want Spot Angle", h)
		}
		// Drag cone edge down to angle +1.0 (total spread = 2.0 rad).
		s2, c2 := math.Sincos(1.0)
		tx, ty := ed.WorldToCanvas(220+90*float32(c2), 90+90*float32(s2))
		app.DragAt("id:editor-viewport", pt(ex, ey), pt(tx-ex, ty-ey))

		spotObj, _, _ := ed.FindObject("spot")
		gotAngle, _ := spotObj.Components["light2d"].(map[string]any)["angle"].(float64)
		if math.Abs(gotAngle-2.0) > 0.05 {
			t.Fatalf("light2d.angle = %v, want ~2.0", gotAngle)
		}
		checkOneUndoStep(t, app, ed, before)
	})

	t.Run("spotlight direction handle", func(t *testing.T) {
		ed := newLightingTestEditor(t)
		ed.SelectObject("spot")
		app := mountEditor(t, ed)
		before := saveYAML(t, ed)

		// Spot direction handle sits at 0.55 * radius along direction 0 -> world (220 + 49.5, 90).
		dx, dy := ed.WorldToCanvas(220+90*0.55, 90)
		if h := ed.PickGizmo2D(dx, dy); h != editor.Handle2DLightSpotDirection {
			t.Fatalf("PickGizmo2D at spot direction handle = %v, want Spot Direction", h)
		}
		// Drag to straight down (+Y, direction = pi/2).
		tx, ty := ed.WorldToCanvas(220, 90+50)
		app.DragAt("id:editor-viewport", pt(dx, dy), pt(tx-dx, ty-dy))

		spotObj, _, _ := ed.FindObject("spot")
		gotDir, _ := spotObj.Components["light2d"].(map[string]any)["direction"].(float64)
		if math.Abs(gotDir-math.Pi/2) > 0.05 {
			t.Fatalf("light2d.direction = %v, want ~%v", gotDir, math.Pi/2)
		}
		checkOneUndoStep(t, app, ed, before)
	})
}

func TestOccluderShapeHandlesAndPolygonEditingUndoRedo(t *testing.T) {
	t.Run("polygon vertex drag", func(t *testing.T) {
		ed := newLightingTestEditor(t)
		ed.SelectObject("poly_wall")
		app := mountEditor(t, ed)
		before := saveYAML(t, ed)

		// Vertex 1 is local (20, -10) -> world (180, 130).
		vx, vy := ed.WorldToCanvas(180, 130)
		if idx := ed.PickOccluderVertex2D(vx, vy); idx != 1 {
			t.Fatalf("PickOccluderVertex2D = %d, want 1", idx)
		}
		if h := ed.PickGizmo2D(vx, vy); h != editor.Handle2DOccluderVertex {
			t.Fatalf("PickGizmo2D at vertex = %v, want Occluder Vertex", h)
		}
		app.DragAt("id:editor-viewport", pt(vx, vy), pt(15, -10))

		obj, _, _ := ed.FindObject("poly_wall")
		occ, ok, err := sceneformat.ExtractOccluder2D(obj, nil)
		if err != nil || !ok {
			t.Fatalf("ExtractOccluder2D: ok=%v err=%v", ok, err)
		}
		if math.Abs(occ.Points[1][0]-35) > 0.5 || math.Abs(occ.Points[1][1]-(-20)) > 0.5 {
			t.Fatalf("vertex 1 = %v, want [35, -20]", occ.Points[1])
		}
		checkOneUndoStep(t, app, ed, before)
	})

	t.Run("polygon edge midpoint inserts vertex", func(t *testing.T) {
		ed := newLightingTestEditor(t)
		ed.SelectObject("poly_wall")
		app := mountEditor(t, ed)
		before := saveYAML(t, ed)

		// Edge 0 connects (-20, -10) and (20, -10); midpoint is local (0, -10) -> world (160, 130).
		mx, my := ed.WorldToCanvas(160, 130)
		if idx := ed.PickOccluderEdge2D(mx, my); idx != 0 {
			t.Fatalf("PickOccluderEdge2D = %d, want 0", idx)
		}
		app.DragAt("id:editor-viewport", pt(mx, my), pt(0, -15))

		obj, _, _ := ed.FindObject("poly_wall")
		occ, _, _ := sceneformat.ExtractOccluder2D(obj, nil)
		if len(occ.Points) != 5 {
			t.Fatalf("len(points) = %d, want 5 after midpoint drag", len(occ.Points))
		}
		if math.Abs(occ.Points[1][0]-0) > 0.5 || math.Abs(occ.Points[1][1]-(-25)) > 0.5 {
			t.Fatalf("inserted vertex = %v, want [0, -25]", occ.Points[1])
		}
		checkOneUndoStep(t, app, ed, before)
	})

	t.Run("rect occluder edge handle", func(t *testing.T) {
		ed := newLightingTestEditor(t)
		ed.SelectObject("pillar")
		app := mountEditor(t, ed)
		before := saveYAML(t, ed)

		// Pillar is at (140, 90), width 24 -> East handle at world (152, 90).
		ex, ey := ed.WorldToCanvas(152, 90)
		if h := ed.PickGizmo2D(ex, ey); h != editor.Handle2DOccluderRectE {
			t.Fatalf("PickGizmo2D at rect E handle = %v, want Occluder Width E", h)
		}
		app.DragAt("id:editor-viewport", pt(ex, ey), pt(10, 0))
		obj, _, _ := ed.FindObject("pillar")
		gotW, _ := obj.Components["occluder2d"].(map[string]any)["width"].(float64)
		if math.Abs(gotW-44) > 0.5 {
			t.Fatalf("occluder2d.width = %v, want ~44", gotW)
		}
		checkOneUndoStep(t, app, ed, before)
	})

	t.Run("escape cancels lighting drag cleanly", func(t *testing.T) {
		ed := newLightingTestEditor(t)
		ed.SelectObject("lamp")
		_ = mountEditor(t, ed)
		before := saveYAML(t, ed)

		hx, hy := ed.WorldToCanvas(180, 90)
		if h := ed.BeginDrag2D(hx, hy); h != editor.Handle2DLightRadius {
			t.Fatalf("BeginDrag2D = %v, want Light Radius", h)
		}
		ed.UpdateDrag2D(hx+50, hy, false)
		if !ed.CancelDrag2D() {
			t.Fatal("CancelDrag2D returned false")
		}
		if ed.History.Len() != 0 {
			t.Fatalf("History.Len = %d after cancel, want 0", ed.History.Len())
		}
		if got := saveYAML(t, ed); !bytes.Equal(got, before) {
			t.Fatalf("CancelDrag2D did not restore exact YAML:\ngot:\n%s\nwant:\n%s", got, before)
		}
	})
}

func TestAmbientColorAndOccluderHelpersUndoRedo(t *testing.T) {
	ed := newLightingTestEditor(t)
	before := saveYAML(t, ed)

	if err := ed.SetAmbientColor("not-a-color"); err == nil {
		t.Fatal("expected SetAmbientColor to reject invalid color")
	}
	if err := ed.SetAmbientColor("#080c18cc"); err != nil {
		t.Fatalf("SetAmbientColor: %v", err)
	}
	if ed.AmbientColor() != "#080c18cc" {
		t.Fatalf("AmbientColor = %q", ed.AmbientColor())
	}
	ed.Undo()
	if got := saveYAML(t, ed); !bytes.Equal(got, before) {
		t.Fatalf("Undo SetAmbientColor did not restore YAML")
	}

	// Convert rect occluder to polygon, insert vertex, remove vertex, refuse < 3 vertices.
	if err := ed.ConvertOccluderToPolygon("pillar"); err != nil {
		t.Fatalf("ConvertOccluderToPolygon: %v", err)
	}
	if err := ed.InsertOccluderVertex("pillar", 0, 0, -30); err != nil {
		t.Fatalf("InsertOccluderVertex: %v", err)
	}
	if err := ed.RemoveOccluderVertex("pillar", 1); err != nil {
		t.Fatalf("RemoveOccluderVertex: %v", err)
	}
	if err := ed.RemoveOccluderVertex("pillar", 0); err != nil {
		t.Fatalf("RemoveOccluderVertex to 3: %v", err)
	}
	if err := ed.RemoveOccluderVertex("pillar", 0); err == nil {
		t.Fatal("expected RemoveOccluderVertex to refuse going below 3 vertices")
	}
}

func TestLightingHandlesAlignOnParallaxLayer(t *testing.T) {
	half := float32(0.5)
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Parallax Lighting",
		VirtualWidth:  320,
		VirtualHeight: 180,
		AmbientColor:  "rgba(10, 14, 24, 0.8)",
		Layers: []sceneformat.LayerDoc{
			{
				Name:      "bg_lit",
				Space:     sceneformat.SpaceWorld,
				ParallaxX: &half,
				ParallaxY: &half,
				Objects: []sceneformat.ObjectDoc{
					{
						ID: "bg_lamp",
						X:  80,
						Y:  90,
						Components: map[string]any{
							"light2d": map[string]any{
								"version":     1,
								"kind":        "point",
								"color":       "#ffaa44",
								"radius":      100.0,
								"intensity":   1.0,
								"castShadows": true,
								"enabled":     true,
							},
						},
					},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SelectObject("bg_lamp")
	app := mountEditor(t, ed)

	// Pan the 2D view so parallax layer (0.5x) diverges strongly from non-parallax world space (1.0x).
	ed.SetView2D(editor.View2D{X: 260, Y: 150, Zoom: 1})
	app.Settle()
	before := saveYAML(t, ed)

	// With parallax=0.5, world point (wx, wy) maps to canvas center + (wx - v.X*0.5, wy - v.Y*0.5).
	cw, ch := ed.CanvasSize()
	v := ed.View2D()
	handleCanvasX := cw*0.5 + (80+100-v.X*0.5)*v.Zoom
	handleCanvasY := ch*0.5 + (90-v.Y*0.5)*v.Zoom

	// Non-parallax world position must NOT hit the handle.
	nonParallaxX, nonParallaxY := ed.WorldToCanvas(180, 90)
	if h := ed.PickGizmo2D(nonParallaxX, nonParallaxY); h == editor.Handle2DLightRadius {
		t.Fatalf("unexpectedly hit Light Radius at non-parallax coordinates (%v, %v)", nonParallaxX, nonParallaxY)
	}
	if h := ed.PickGizmo2D(handleCanvasX, handleCanvasY); h != editor.Handle2DLightRadius {
		t.Fatalf("PickGizmo2D at parallax handle (%v, %v) = %v, want Light Radius", handleCanvasX, handleCanvasY, h)
	}

	// Drag +30 canvas points along +X -> radius should grow from 100 to 130.
	app.DragAt("id:editor-viewport", pt(handleCanvasX, handleCanvasY), pt(30, 0))
	lamp, _, _ := ed.FindObject("bg_lamp")
	gotR, _ := lamp.Components["light2d"].(map[string]any)["radius"].(float64)
	if math.Abs(gotR-130) > 0.5 {
		t.Fatalf("parallax light2d.radius = %v, want ~130", gotR)
	}
	checkOneUndoStep(t, app, ed, before)
}

func TestSceneInspectorShowsAmbientColorWhenShowingSceneSettings(t *testing.T) {
	ed := newLightingTestEditor(t)
	ed.SelectObject("")
	if !ed.ShowingSceneSettings() {
		t.Fatal("expected ShowingSceneSettings() == true after SelectObject(\"\")")
	}
	app := mountEditor(t, ed)
	if !app.Exists("id:inspector-scene") {
		t.Fatal("expected inspector-scene to be mounted")
	}
	if !app.Exists("id:inspector-scene-ambient-color") {
		t.Fatal("expected inspector-scene-ambient-color inside sceneInspectorView")
	}

	before := saveYAML(t, ed)
	app.Clear("id:inspector-scene-ambient-color")
	app.Type("id:inspector-scene-ambient-color", "#102030aa\n")
	app.Settle()
	if got := ed.AmbientColor(); got != "#102030aa" {
		t.Fatalf("AmbientColor = %q, want #102030aa", got)
	}
	checkOneUndoStep(t, app, ed, before)

	// Clear button removes ambient color undoably.
	app.Press("id:inspector-scene-ambient-clear")
	app.Settle()
	if got := ed.AmbientColor(); got != "" {
		t.Fatalf("AmbientColor after clear = %q, want empty", got)
	}
}

func TestLightingNilReceiverAndPlayModeGuards(t *testing.T) {
	var nilEd *editor.Editor
	if nilEd.AmbientColor() != "" {
		t.Fatal("nil Editor AmbientColor should be empty")
	}
	nilEd.SetShowLightingPreview(false) // must not panic
	if err := nilEd.SetAmbientColor("#112233"); err == nil {
		t.Fatal("expected error from nil SetAmbientColor")
	}
	if err := nilEd.SetOccluderPolygonPoints("p", [][2]float64{{0, 0}, {1, 0}, {0, 1}}); err == nil {
		t.Fatal("expected error from nil SetOccluderPolygonPoints")
	}
	if err := nilEd.InsertOccluderVertex("p", 0, 1, 1); err == nil {
		t.Fatal("expected error from nil InsertOccluderVertex")
	}
	if err := nilEd.RemoveOccluderVertex("p", 0); err == nil {
		t.Fatal("expected error from nil RemoveOccluderVertex")
	}
	if err := nilEd.ConvertOccluderToPolygon("p"); err == nil {
		t.Fatal("expected error from nil ConvertOccluderToPolygon")
	}

	ed := newLightingTestEditor(t)
	_ = mountEditor(t, ed)
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	defer func() { _ = ed.Stop() }()

	if err := ed.SetAmbientColor("#112233"); err != editor.ErrPlaying {
		t.Fatalf("SetAmbientColor while playing = %v, want ErrPlaying", err)
	}
	if err := ed.SetOccluderPolygonPoints("poly_wall", [][2]float64{{0, 0}, {10, 0}, {0, 10}}); err != editor.ErrPlaying {
		t.Fatalf("SetOccluderPolygonPoints while playing = %v, want ErrPlaying", err)
	}
	if err := ed.InsertOccluderVertex("poly_wall", 0, 5, 5); err != editor.ErrPlaying {
		t.Fatalf("InsertOccluderVertex while playing = %v, want ErrPlaying", err)
	}
	if err := ed.RemoveOccluderVertex("poly_wall", 0); err != editor.ErrPlaying {
		t.Fatalf("RemoveOccluderVertex while playing = %v, want ErrPlaying", err)
	}
	if err := ed.ConvertOccluderToPolygon("pillar"); err != editor.ErrPlaying {
		t.Fatalf("ConvertOccluderToPolygon while playing = %v, want ErrPlaying", err)
	}
}
