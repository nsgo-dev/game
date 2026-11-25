package editor_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newLighting3DTestEditor(t *testing.T) *editor.Editor {
	t.Helper()
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Handles",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID:   "sun",
					X:    0,
					Y:    4,
					PosZ: 0,
					RotX: -0.6,
					Light: &sceneformat.LightDoc{
						Kind:      sceneformat.LightDirectional,
						Color:     "#fff5cc",
						Intensity: sceneformat.Float32(3),
						Shadows:   true,
					},
				},
				{
					ID:   "lamp",
					X:    -3,
					Y:    1.5,
					PosZ: 0,
					Light: &sceneformat.LightDoc{
						Kind:      sceneformat.LightPoint,
						Color:     "#ffaa66",
						Intensity: sceneformat.Float32(5),
						Range:     4,
					},
				},
				{
					ID:   "spot",
					X:    3,
					Y:    3,
					PosZ: 0,
					RotX: -0.5,
					Light: &sceneformat.LightDoc{
						Kind:      sceneformat.LightSpot,
						Color:     "#66ccff",
						Intensity: sceneformat.Float32(8),
						Range:     6,
						InnerCone: 12,
						OuterCone: sceneformat.Float32(28),
					},
				},
				{
					ID:   "cam_main",
					X:    0,
					Y:    2,
					PosZ: 8,
					Camera: &sceneformat.Camera3DDoc{
						Projection: sceneformat.ProjectionPerspective,
						FOV:        sceneformat.Float32(60),
						Near:       sceneformat.Float32(0.1),
						Far:        sceneformat.Float32(25),
						Current:    true,
					},
				},
				{
					ID:   "cam_ortho",
					X:    -6,
					Y:    3,
					PosZ: 6,
					Camera: &sceneformat.Camera3DDoc{
						Projection:  sceneformat.ProjectionOrthographic,
						OrthoHeight: sceneformat.Float32(10),
						Near:        sceneformat.Float32(0.1),
						Far:         sceneformat.Float32(40),
					},
				},
			},
		}},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 2, 0},
		Yaw:      0.25,
		Pitch:    0.25,
		Distance: 22,
		FOV:      float32(math.Pi / 3),
		Near:     0.1,
		Far:      500,
	}
	return ed
}

func dragHandle(t *testing.T, ed *editor.Editor, kind editor.Handle3DKind, dx, dy float32) {
	t.Helper()
	const w, h float32 = 800, 600
	hp, ok := ed.HandlePosition3D(kind)
	if !ok {
		t.Fatalf("HandlePosition3D(%v) returned false", kind)
	}
	sx, sy, _, vis := ed.Camera3D.Project(hp, w, h)
	if !vis {
		t.Fatalf("handle %v at %v is not visible", kind, hp)
	}
	if picked := ed.PickHandle3D(sx, sy, w, h); picked != kind {
		t.Fatalf("PickHandle3D(%v, %v) = %v, want %v", sx, sy, picked, kind)
	}
	ed.BeginViewport3DDrag(sx, sy, w, h, false)
	if !ed.DraggingHandle3D() || ed.ActiveHandle3D() != kind {
		t.Fatalf("BeginViewport3DDrag did not start %v handle drag (active=%v)", kind, ed.ActiveHandle3D())
	}
	ed.UpdateViewport3DDrag(sx+dx, sy+dy)
	ed.EndViewport3DDrag()
	if ed.DraggingHandle3D() {
		t.Fatalf("DraggingHandle3D still true after EndViewport3DDrag")
	}
}

func TestLightAndCamera3DHandlesUndoable(t *testing.T) {
	ed := newLighting3DTestEditor(t)

	// 1. Directional light direction handle.
	ed.SelectObject("sun")
	sun, _, _ := ed.FindObject("sun")
	origRX, origRY := sun.RotX, sun.RotY
	dragHandle(t, ed, editor.Handle3DLightDirection, 50, -35)
	if sun.RotX == origRX && sun.RotY == origRY {
		t.Fatalf("directional light rotation unchanged after direction handle drag")
	}
	aimedRX, aimedRY := sun.RotX, sun.RotY
	ed.Undo()
	if sun.RotX != origRX || sun.RotY != origRY {
		t.Fatalf("Undo did not restore directional light rotation: got (%v,%v), want (%v,%v)", sun.RotX, sun.RotY, origRX, origRY)
	}
	ed.Redo()
	if sun.RotX != aimedRX || sun.RotY != aimedRY {
		t.Fatalf("Redo did not reapply directional light rotation")
	}

	// 2. Point light range handle.
	ed.SelectObject("lamp")
	lamp, _, _ := ed.FindObject("lamp")
	origRange := lamp.Light.Range
	dragHandle(t, ed, editor.Handle3DLightRange, 45, 0)
	if lamp.Light.Range <= origRange {
		t.Fatalf("point light range = %v after outward drag, want > %v", lamp.Light.Range, origRange)
	}
	newRange := lamp.Light.Range
	ed.Undo()
	if lamp.Light.Range != origRange {
		t.Fatalf("Undo did not restore point light range: got %v, want %v", lamp.Light.Range, origRange)
	}
	ed.Redo()
	if lamp.Light.Range != newRange {
		t.Fatalf("Redo did not reapply point light range: got %v, want %v", lamp.Light.Range, newRange)
	}

	// 3. Spot light range, outer cone, and inner cone handles.
	ed.SelectObject("spot")
	spot, _, _ := ed.FindObject("spot")
	origSpotRange := spot.Light.Range
	dragHandle(t, ed, editor.Handle3DLightRange, 0, 40)
	if spot.Light.Range == origSpotRange {
		t.Fatalf("spot light range unchanged after drag")
	}
	ed.Undo()
	if spot.Light.Range != origSpotRange {
		t.Fatalf("Undo did not restore spot light range")
	}

	origOuter := spot.Light.EffectiveOuterCone()
	dragHandle(t, ed, editor.Handle3DLightOuterCone, 45, 0)
	if spot.Light.EffectiveOuterCone() <= origOuter {
		t.Fatalf("spot outer cone = %v after outward drag, want > %v", spot.Light.EffectiveOuterCone(), origOuter)
	}
	newOuter := spot.Light.EffectiveOuterCone()
	ed.Undo()
	if spot.Light.EffectiveOuterCone() != origOuter {
		t.Fatalf("Undo did not restore spot outer cone: got %v, want %v", spot.Light.EffectiveOuterCone(), origOuter)
	}
	ed.Redo()
	if spot.Light.EffectiveOuterCone() != newOuter {
		t.Fatalf("Redo did not reapply spot outer cone")
	}

	origInner := spot.Light.InnerCone
	dragHandle(t, ed, editor.Handle3DLightInnerCone, 0, -35)
	if spot.Light.InnerCone == origInner {
		t.Fatalf("spot inner cone unchanged after drag")
	}
	if spot.Light.InnerCone > spot.Light.EffectiveOuterCone() {
		t.Fatalf("spot inner cone %v exceeded outer cone %v", spot.Light.InnerCone, spot.Light.EffectiveOuterCone())
	}
	ed.Undo()
	if spot.Light.InnerCone != origInner {
		t.Fatalf("Undo did not restore spot inner cone")
	}

	// 4. Perspective 3D camera FOV and Far handles.
	ed.SelectObject("cam_main")
	camMain, _, _ := ed.FindObject("cam_main")
	origFOV := camMain.Camera.EffectiveFOV()
	dragHandle(t, ed, editor.Handle3DCameraFOV, 0, -40)
	if camMain.Camera.EffectiveFOV() <= origFOV {
		t.Fatalf("camera FOV = %v after upward drag, want > %v", camMain.Camera.EffectiveFOV(), origFOV)
	}
	ed.Undo()
	if camMain.Camera.EffectiveFOV() != origFOV {
		t.Fatalf("Undo did not restore camera FOV")
	}

	// Frame camera far plane within view before dragging Far handle.
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 2, -10},
		Yaw:      0.9,
		Pitch:    0.2,
		Distance: 35,
		FOV:      float32(math.Pi / 3),
		Near:     0.1,
		Far:      500,
	}
	origFar := camMain.Camera.EffectiveFar()
	dragHandle(t, ed, editor.Handle3DCameraFar, 45, 0)
	if camMain.Camera.EffectiveFar() == origFar {
		t.Fatalf("camera Far unchanged after drag")
	}
	ed.Undo()
	if camMain.Camera.EffectiveFar() != origFar {
		t.Fatalf("Undo did not restore camera Far")
	}

	// 5. Orthographic 3D camera OrthoHeight handle + cancel drag test.
	ed.SelectObject("cam_ortho")
	camOrtho, _, _ := ed.FindObject("cam_ortho")
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{-6, 3, 2},
		Yaw:      0.3,
		Pitch:    0.15,
		Distance: 24,
		FOV:      float32(math.Pi / 3),
		Near:     0.1,
		Far:      500,
	}
	origOrthoH := camOrtho.Camera.EffectiveOrthoHeight()
	dragHandle(t, ed, editor.Handle3DCameraFOV, 0, -35)
	if camOrtho.Camera.EffectiveOrthoHeight() <= origOrthoH {
		t.Fatalf("ortho height = %v after upward drag, want > %v", camOrtho.Camera.EffectiveOrthoHeight(), origOrthoH)
	}
	ed.Undo()
	if camOrtho.Camera.EffectiveOrthoHeight() != origOrthoH {
		t.Fatalf("Undo did not restore ortho height")
	}

	// CancelHandleDrag3D restores value without creating a history entry.
	ed.History.Clear()
	hp, _ := ed.HandlePosition3D(editor.Handle3DCameraFOV)
	sx, sy, _, _ := ed.Camera3D.Project(hp, 800, 600)
	ed.BeginViewport3DDrag(sx, sy, 800, 600, false)
	ed.UpdateViewport3DDrag(sx, sy-50)
	ed.CancelHandleDrag3D()
	if camOrtho.Camera.EffectiveOrthoHeight() != origOrthoH {
		t.Fatalf("CancelHandleDrag3D left orthoHeight=%v, want %v", camOrtho.Camera.EffectiveOrthoHeight(), origOrthoH)
	}
	if ed.History.CanUndo() {
		t.Fatalf("CancelHandleDrag3D pushed an entry onto History")
	}
}

func TestLightAndCameraIconsPickingAndLookThroughToggle(t *testing.T) {
	ed := newLighting3DTestEditor(t)
	const w, h float32 = 800, 600

	// Clicking each light and camera's projected icon selects it.
	for _, id := range []string{"sun", "lamp", "spot", "cam_main"} {
		obj, _, _ := ed.FindObject(id)
		sx, sy, _, vis := ed.Camera3D.Project([3]float32{obj.X, obj.Y, obj.PosZ}, w, h)
		if !vis {
			t.Fatalf("%s not visible", id)
		}
		got, ok := ed.Pick3DObject(sx, sy, w, h)
		if !ok || got != id {
			t.Fatalf("Pick3DObject on %s icon = (%q, %v), want (%q, true)", id, got, ok, id)
		}
	}

	// Drawing the 3D viewport with lights, camera, and handles executes cleanly.
	ed.SelectObject("spot")
	var d core.Drawing
	ed.DrawViewport3D(&d, core.Size{Width: core.Points(w), Height: core.Points(h)})
	ed.SelectObject("cam_main")
	ed.DrawViewport3D(&d, core.Size{Width: core.Points(w), Height: core.Points(h)})

	// Look-through-camera toggle and keyboard shortcut "0".
	if !ed.HasSceneCamera3D() || ed.LookThroughCamera3D() {
		t.Fatalf("initial HasSceneCamera3D=%v LookThroughCamera3D=%v", ed.HasSceneCamera3D(), ed.LookThroughCamera3D())
	}
	if !ed.TriggerShortcut("0") {
		t.Fatal("shortcut '0' did not trigger CmdView3DCamera")
	}
	if !ed.LookThroughCamera3D() {
		t.Fatal("LookThroughCamera3D = false after shortcut '0'")
	}
	if name := ed.ViewName3D(); name != "Camera (cam_main)" {
		t.Fatalf("ViewName3D() = %q, want %q", name, "Camera (cam_main)")
	}
	// Orbiting exits look-through-camera mode.
	ed.OrbitView3D(0.1, 0)
	if ed.LookThroughCamera3D() {
		t.Fatal("OrbitView3D did not exit LookThroughCamera3D")
	}

	// Switching current camera to cam_ortho clears current on cam_main atomically and Undo restores both.
	ortho, _, _ := ed.FindObject("cam_ortho")
	next := ortho.Camera.Clone()
	next.Current = true
	if err := ed.SetObjectCamera3D("cam_ortho", next); err != nil {
		t.Fatalf("SetObjectCamera3D: %v", err)
	}
	camMain, _, _ := ed.FindObject("cam_main")
	if camMain.Camera.Current || !ortho.Camera.Current {
		t.Fatalf("after switch: cam_main.Current=%v cam_ortho.Current=%v", camMain.Camera.Current, ortho.Camera.Current)
	}
	ed.Undo()
	if !camMain.Camera.Current || ortho.Camera.Current {
		t.Fatalf("after Undo: cam_main.Current=%v cam_ortho.Current=%v", camMain.Camera.Current, ortho.Camera.Current)
	}
}

func TestEnvironmentPanelAndVersionUpgradeUndo(t *testing.T) {
	// Start from a version 3 scene without environment or lights.
	v3 := &sceneformat.SceneDoc{
		Version:         3,
		Name:            "Legacy",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Layers:          []sceneformat.LayerDoc{{Name: "world"}},
	}
	ed, err := editor.New(v3, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)

	// Setting environment raises version to 4, and Undo restores version 3 cleanly.
	env := &sceneformat.EnvironmentDoc{
		Sky:         &sceneformat.SkyDoc{Kind: sceneformat.SkyColor, Color: "#224466"},
		Tonemapping: sceneformat.TonemapACES,
		Exposure:    sceneformat.Float32(1.2),
	}
	if err := ed.SetEnvironment(env); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}
	if ed.Doc.Version != sceneformat.CurrentVersion {
		t.Fatalf("Doc.Version = %d, want %d", ed.Doc.Version, sceneformat.CurrentVersion)
	}
	ed.Undo()
	if ed.Doc.Version != 3 || ed.Doc.Environment != nil {
		t.Fatalf("after Undo: version=%d env=%v, want version=3 env=nil", ed.Doc.Version, ed.Doc.Environment)
	}

	// AddLight3D and AddCamera3D work and upgrade version undoably.
	lightID, err := ed.AddLight3D(sceneformat.LightSpot)
	if err != nil {
		t.Fatalf("AddLight3D: %v", err)
	}
	camID, err := ed.AddCamera3D(sceneformat.ProjectionPerspective)
	if err != nil {
		t.Fatalf("AddCamera3D: %v", err)
	}
	if lightID == "" || camID == "" || ed.Doc.Version != sceneformat.CurrentVersion {
		t.Fatalf("AddLight3D/AddCamera3D failed: light=%q cam=%q ver=%d", lightID, camID, ed.Doc.Version)
	}

	// Mount EditorView and open Environment panel via command.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	if err := ed.ExecuteCommand(editor.CmdEnvironmentPanel); err != nil {
		t.Fatalf("ExecuteCommand(CmdEnvironmentPanel): %v", err)
	}
	app.Settle()
	if !ed.EnvironmentPanelOpen() {
		t.Fatal("EnvironmentPanelOpen = false after command")
	}
	if !app.Exists("id:environment-panel") {
		t.Fatal("environment-panel not mounted in EditorView")
	}
	if !app.Exists("id:view3d-camera") {
		t.Fatal("view3d-camera toggle not mounted in 3D viewport bar")
	}
	// Inspector shows LIGHT 3D and CAMERA 3D sections for the selected camera object.
	if !app.Exists("id:inspector-cam3d-fov") {
		t.Fatal("inspector-cam3d-fov not mounted for selected 3D camera")
	}
}

func TestHandle3DSpotConeClampStaysValid(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Spot Clamp",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{{
				ID: "spot",
				Light: &sceneformat.LightDoc{
					Kind:      sceneformat.LightSpot,
					Range:     10,
					InnerCone: 20,
					OuterCone: sceneformat.Float32(30),
				},
			}},
		}},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.Camera3D.Target = [3]float32{0, 0, -5}
	ed.Camera3D.Distance = 12
	ed.Camera3D.Yaw = 0.6
	ed.Camera3D.Pitch = 0.35
	ed.SelectObject("spot")

	const w, h float32 = 800, 600

	// Drag inner cone handle far past outer cone: must clamp strictly below OuterCone and stay valid.
	hp, ok := ed.HandlePosition3D(editor.Handle3DLightInnerCone)
	if !ok {
		t.Fatal("HandlePosition3D(InnerCone) returned false")
	}
	sx, sy, _, vis := ed.Camera3D.Project(hp, w, h)
	if !vis {
		t.Fatal("InnerCone handle not visible")
	}
	if !ed.BeginHandleDragKind3D(editor.Handle3DLightInnerCone, sx, sy, w, h) {
		t.Fatal("BeginHandleDragKind3D(InnerCone) failed")
	}
	ed.UpdateHandleDrag3D(sx, sy-400)
	ed.EndHandleDrag3D()

	spot, _, _ := ed.FindObject("spot")
	if spot.Light.InnerCone >= spot.Light.EffectiveOuterCone() {
		t.Fatalf("InnerCone %v >= OuterCone %v after extreme drag", spot.Light.InnerCone, spot.Light.EffectiveOuterCone())
	}
	if err := sceneformat.Validate(ed.Doc); err != nil {
		t.Fatalf("scene invalid after inner cone drag: %v", err)
	}

	// Drag outer cone handle far inward below InnerCone: must push InnerCone strictly below OuterCone.
	hp, ok = ed.HandlePosition3D(editor.Handle3DLightOuterCone)
	if !ok {
		t.Fatal("HandlePosition3D(OuterCone) returned false")
	}
	sx, sy, _, vis = ed.Camera3D.Project(hp, w, h)
	if !vis {
		t.Fatal("OuterCone handle not visible")
	}
	if !ed.BeginHandleDragKind3D(editor.Handle3DLightOuterCone, sx, sy, w, h) {
		t.Fatal("BeginHandleDragKind3D(OuterCone) failed")
	}
	ed.UpdateHandleDrag3D(sx-400, sy)
	ed.EndHandleDrag3D()

	spot, _, _ = ed.FindObject("spot")
	if spot.Light.InnerCone >= spot.Light.EffectiveOuterCone() {
		t.Fatalf("InnerCone %v >= OuterCone %v after shrinking outer cone", spot.Light.InnerCone, spot.Light.EffectiveOuterCone())
	}
	if err := sceneformat.Validate(ed.Doc); err != nil {
		t.Fatalf("scene invalid after outer cone drag: %v", err)
	}
}

func TestLookThroughOrthoCameraPreservesNegativeNearAndGuardsTools(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Ortho Near",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID:   "cam",
					PosZ: 0,
					Camera: &sceneformat.Camera3DDoc{
						Projection:  sceneformat.ProjectionOrthographic,
						OrthoHeight: sceneformat.Float32(10),
						Near:        sceneformat.Float32(-20),
						Far:         sceneformat.Float32(50),
						Current:     true,
					},
				},
				{
					// Placed at Z = +5 (behind the camera origin, in front of Near = -20):
					// pickIcon3D projects via vc.project, which clips against vc.near.
					ID:    "behind_light",
					PosZ:  5,
					Light: &sceneformat.LightDoc{Kind: sceneformat.LightPoint, Range: 3},
				},
				{
					ID:   "pt",
					X:    4,
					PosZ: -5,
					Light: &sceneformat.LightDoc{
						Kind:  sceneformat.LightPoint,
						Range: 4,
					},
				},
			},
		}},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	if err := ed.SetLookThroughCamera3D(true); err != nil {
		t.Fatalf("SetLookThroughCamera3D: %v", err)
	}

	const w, h float32 = 800, 600
	// Pick3DObject at center of viewport must hit "behind_light" (Z=+5, camera-space Z=-5 > Near=-20).
	// If Near were clamped to 0.1, "behind_light" would be clipped behind the near plane and missed.
	if got, ok := ed.Pick3DObject(w/2, h/2, w, h); !ok || got != "behind_light" {
		t.Fatalf("Pick3DObject through ortho camera with near=-20 = (%q, %v), want (%q, true)", got, ok, "behind_light")
	}

	// PickHandle3D must return Handle3DNone while a terrain brush or GridMap tool is active.
	if err := ed.SetLookThroughCamera3D(false); err != nil {
		t.Fatalf("SetLookThroughCamera3D(false): %v", err)
	}
	ed.Camera3D.Target = [3]float32{4, 0, -5}
	ed.Camera3D.Distance = 12
	ed.SelectObject("pt")
	hp, ok := ed.HandlePosition3D(editor.Handle3DLightRange)
	if !ok {
		t.Fatal("HandlePosition3D(Range) returned false")
	}
	sx, sy, _, vis := ed.Camera3D.Project(hp, w, h)
	if !vis {
		t.Fatal("Range handle not visible")
	}
	if got := ed.PickHandle3D(sx, sy, w, h); got != editor.Handle3DLightRange {
		t.Fatalf("PickHandle3D at rest = %v, want Handle3DLightRange", got)
	}
	if err := ed.NewTerrain(editor.NewTerrainOptions{}); err != nil {
		t.Fatalf("NewTerrain: %v", err)
	}
	b := ed.TerrainBrush()
	b.Mode = editor.TerrainBrushRaise
	ed.SetTerrainBrush(b)
	if got := ed.PickHandle3D(sx, sy, w, h); got != editor.Handle3DNone {
		t.Fatalf("PickHandle3D while terrain brush active = %v, want Handle3DNone", got)
	}
	ed.CloseTerrain()

	// OpenDemo resets lookThroughCamera3D and envPanelOpen.
	_ = ed.SetLookThroughCamera3D(true)
	ed.SetEnvironmentPanelOpen(true)
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.LookThroughCamera3D() {
		t.Error("OpenDemo left LookThroughCamera3D true")
	}
	if ed.EnvironmentPanelOpen() {
		t.Error("OpenDemo left EnvironmentPanelOpen true")
	}
}
