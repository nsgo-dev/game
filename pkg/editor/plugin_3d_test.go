package editor_test

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func volumeComponents(t *testing.T) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := healthComponents(t)
	if err := reg.Register(sceneformat.ComponentType{
		Name: "spawn_volume",
		Doc:  "3D volume where actors can spawn.",
		Fields: []sceneformat.Field{
			{Name: "radius", Kind: sceneformat.FieldFloat, Default: 12.0, Min: sceneformat.Float64(0.5), Max: sceneformat.Float64(500)},
			{Name: "height", Kind: sceneformat.FieldFloat, Default: 20.0, Min: sceneformat.Float64(0.5), Max: sceneformat.Float64(500)},
			{Name: "offsetX", Kind: sceneformat.FieldFloat, Default: 0.0},
		},
	}); err != nil {
		t.Fatalf("Register spawn_volume: %v", err)
	}
	return reg
}

func floatField(ed *editor.Editor, id, comp, field string) float64 {
	v, _ := ed.ComponentField(id, comp, field)
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0
	}
}

func write3DPluginProject(t *testing.T, plugins ...string) string {
	t.Helper()
	dir := writeEditorProject(t)
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "3D Plugin Scene",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:    "spawner",
						Name:  "Spawner",
						X:     0,
						Y:     0,
						PosZ:  0,
						Depth: 16,
						Components: map[string]any{
							"spawn_volume": map[string]any{"radius": 20.0, "height": 24.0, "offsetX": 0.0},
						},
					},
				},
			},
		},
	}
	scene, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "room.yaml"), scene, 0o644); err != nil {
		t.Fatal(err)
	}
	data := projectYAML
	if len(plugins) > 0 {
		data += "editor:\n  plugins: [" + strings.Join(plugins, ", ") + "]\n"
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Acceptance #1: The editor's test plugin draws in the 3D viewport and receives the ray of a press.
func TestPluginTool3DDrawsAndReceivesPressRay(t *testing.T) {
	var drawCount int
	var lastProj editor.Projection3D
	var pressRays []math3d.Ray

	dir := write3DPluginProject(t, "health")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(volumeComponents(t)); err != nil {
		t.Fatal(err)
	}

	p := healthPlugin(nil)
	p.Tools = append(p.Tools, editor.PluginTool{
		ID:    "beacon3d",
		Label: "Beacon 3D",
		Press3D: func(ed *editor.Editor, ray math3d.Ray) {
			pressRays = append(pressRays, ray)
			// Intersect Y=0 ground plane and place a marker object so the press also verifies scene edits.
			if math.Abs(float64(ray.Dir.Y)) > 1e-5 {
				dist := -ray.Origin.Y / ray.Dir.Y
				if dist >= 0 {
					hit := ray.At(dist)
					_, _ = ed.PlaceObject("world", sceneformat.ObjectDoc{
						X:     hit.X,
						Y:     0,
						PosZ:  hit.Z,
						Depth: 8,
					})
				}
			}
		},
		Draw3D: func(_ *editor.Editor, d *core.Drawing, proj editor.Projection3D) {
			drawCount++
			lastProj = proj
			proj.DrawLine(d, math3d.Vec3{X: -10, Y: 0, Z: 0}, math3d.Vec3{X: 10, Y: 0, Z: 0}, core.RGBA(0xEF4444, 1), 2)
			proj.DrawPoint(d, math3d.Vec3{X: 0, Y: 10, Z: 0}, 6, core.RGBA(0x22C55E, 1))
			proj.DrawBox(d, math3d.Vec3{X: 0, Y: 5, Z: 0}, math3d.Vec3{X: 10, Y: 10, Z: 10}, math3d.Mat4Identity(), core.RGBA(0x38BDF8, 1), 1.5)
		},
	})
	if err := ed.RegisterPlugin(p); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.5,
		Pitch:    0.55,
		Distance: 160,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// In 2D mode the 3D-only tool stays out of the 2D toolbar and does not draw.
	if app.Exists("id:tool-health-beacon3d") {
		t.Fatal("3D-only plugin tool appeared in the 2D toolbar")
	}
	if !app.Exists("id:tool-health-spawn") {
		t.Fatal("2D plugin tool missing from the 2D toolbar")
	}

	// Switch to 3D: the 3D tool appears in the 3D toolbar, while the 2D-only tool leaves.
	app.Press("id:view-mode-3d")
	if !app.Exists("id:tool-health-beacon3d") {
		t.Fatal("3D plugin tool did not appear in the 3D toolbar")
	}
	if app.Exists("id:tool-health-spawn") {
		t.Fatal("2D-only plugin tool appeared in the 3D toolbar")
	}
	if drawCount != 0 {
		t.Fatalf("Draw3D ran %d times before tool activation", drawCount)
	}

	// Activate the 3D plugin tool from the toolbar.
	app.Press("id:tool-health-beacon3d")
	if plugin, id, ok := ed.PluginTool(); !ok || plugin != "health" || id != "beacon3d" || ed.Tool != editor.ToolPlugin {
		t.Fatalf("PluginTool = (%q, %q, %v), Tool = %v", plugin, id, ok, ed.Tool)
	}
	if got := allText(app.Find("id:editor-status")); !strings.Contains(got, "Tool: Beacon 3D") {
		t.Fatalf("3D status = %q, want it to name Beacon 3D", got)
	}
	if drawCount == 0 {
		t.Fatal("Draw3D did not run after activating the 3D plugin tool")
	}
	if lastProj.Width <= 0 || lastProj.Height <= 0 || lastProj.ViewProj == (math3d.Mat4{}) {
		t.Fatalf("Draw3D received invalid Projection3D: %+v", lastProj)
	}

	// Draw into a fresh core.Drawing and verify the plugin actually emitted draw ops over the 3D view.
	var withPlugin, withoutPlugin core.Drawing
	cw, ch := ed.CanvasSize()
	ed.DrawViewport(&withPlugin, core.Size{Width: core.Points(cw), Height: core.Points(ch)})
	app.Press("id:tool-select")
	ed.DrawViewport(&withoutPlugin, core.Size{Width: core.Points(cw), Height: core.Points(ch)})
	if len(withPlugin.Ops) <= len(withoutPlugin.Ops) {
		t.Fatalf("expected 3D plugin tool to emit draw ops: with=%d without=%d", len(withPlugin.Ops), len(withoutPlugin.Ops))
	}

	// Re-activate the 3D tool and press the 3D viewport at a projected world point on the XZ plane.
	app.Press("id:tool-health-beacon3d")
	targetPt := math3d.Vec3{X: 18, Y: 0, Z: -12}
	sx, sy, _, vis := ed.Projection3D(cw, ch).Project(targetPt)
	if !vis {
		t.Fatal("target point is not visible in 3D camera")
	}
	wantRay := ed.PickRay3D(sx, sy, cw, ch)
	beforeObjs := len(ed.Objects3D())
	app.PressAt("id:editor-viewport", core.Point{X: core.Points(sx), Y: core.Points(sy)})
	if len(pressRays) != 1 {
		t.Fatalf("Press3D called %d times, want 1", len(pressRays))
	}
	gotRay := pressRays[0]
	assertNear32(t, "ray.Origin.X", gotRay.Origin.X, wantRay.Origin.X, 1e-3)
	assertNear32(t, "ray.Origin.Y", gotRay.Origin.Y, wantRay.Origin.Y, 1e-3)
	assertNear32(t, "ray.Origin.Z", gotRay.Origin.Z, wantRay.Origin.Z, 1e-3)
	assertNear32(t, "ray.Dir.X", gotRay.Dir.X, wantRay.Dir.X, 1e-3)
	assertNear32(t, "ray.Dir.Y", gotRay.Dir.Y, wantRay.Dir.Y, 1e-3)
	assertNear32(t, "ray.Dir.Z", gotRay.Dir.Z, wantRay.Dir.Z, 1e-3)

	// The placed object landed on the XZ ground plane where the pick ray intersected.
	if len(ed.Objects3D()) != beforeObjs+1 {
		t.Fatalf("expected 1 placed object, got %d (was %d)", len(ed.Objects3D()), beforeObjs)
	}
	placed := ed.Objects3D()[len(ed.Objects3D())-1].Doc
	assertNear32(t, "placed.X", placed.X, targetPt.X, 0.5)
	assertNear32(t, "placed.PosZ", placed.PosZ, targetPt.Z, 0.5)

	// While playing, the 3D plugin tool must neither draw nor receive presses.
	app.Press("id:toolbar-play")
	if !ed.Playing() {
		t.Fatal("Play did not start")
	}
	drawCount = 0
	app.Post(func() { ed.HandleViewport3DPress(sx, sy, cw, ch) })
	app.Settle()
	if drawCount != 0 || len(pressRays) != 1 {
		t.Fatalf("while playing: drawCount=%d pressRays=%d, want 0 and 1", drawCount, len(pressRays))
	}
	app.Press("id:toolbar-play")
}

// 3D plugin tool drag and hover lifecycle (OnDragBegin3D, OnDragMove3D, OnDragEnd3D, OnDragCancel3D, OnHover3D).
func TestPluginTool3DDragHoverAndCancel(t *testing.T) {
	type ev struct {
		kind   string
		active bool
		ray    math3d.Ray
	}
	var events []ev

	dir := write3DPluginProject(t, "spline")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(volumeComponents(t)); err != nil {
		t.Fatal(err)
	}
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "spline",
		Tools: []editor.PluginTool{{
			ID:    "draw",
			Label: "Draw Spline",
			OnDragBegin3D: func(_ *editor.Editor, ray math3d.Ray) bool {
				events = append(events, ev{kind: "begin", ray: ray})
				return true
			},
			OnDragMove3D: func(_ *editor.Editor, ray math3d.Ray) {
				events = append(events, ev{kind: "move", ray: ray})
			},
			OnDragEnd3D: func(_ *editor.Editor, ray math3d.Ray) {
				events = append(events, ev{kind: "end", ray: ray})
			},
			OnDragCancel3D: func(_ *editor.Editor) {
				events = append(events, ev{kind: "cancel"})
			},
			OnHover3D: func(_ *editor.Editor, ray math3d.Ray, active bool) {
				events = append(events, ev{kind: "hover", active: active, ray: ray})
			},
		}},
	}); err != nil {
		t.Fatalf("RegisterPlugin 3D drag-only tool: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// Activating a 3D-only tool from the Command Palette switches to 3D mode automatically.
	app.Post(func() {
		if err := ed.ExecutePaletteItem("plugin-tool:spline\x00draw"); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if ed.ViewMode != editor.ViewMode3D {
		t.Fatalf("ViewMode after palette activation of 3D-only tool = %v, want 3D", ed.ViewMode)
	}

	cw, ch := ed.CanvasSize()
	yawBefore := ed.Camera3D.Yaw

	// Hover enters and leaves.
	app.Post(func() {
		ed.SetPluginHover3D(100, 80, cw, ch)
		ed.ClearPluginHover3D()
	})
	app.Settle()

	// Drag through the mounted 3D viewport: claimed by plugin tool so camera does NOT orbit.
	app.DragAt("id:editor-viewport-frame",
		core.Point{X: 140, Y: 100},
		core.Point{X: 25, Y: 10},
		core.Point{X: 50, Y: 20})

	if ed.Camera3D.Yaw != yawBefore {
		t.Fatalf("claimed 3D plugin drag orbited camera from %v to %v", yawBefore, ed.Camera3D.Yaw)
	}
	if len(events) < 5 {
		t.Fatalf("expected hover(true), hover(false), begin, move(s), end; got %+v", events)
	}
	if events[0].kind != "hover" || !events[0].active || events[1].kind != "hover" || events[1].active {
		t.Fatalf("unexpected hover sequence: %+v", events[:2])
	}
	if events[2].kind != "begin" || events[len(events)-1].kind != "end" {
		t.Fatalf("unexpected drag lifecycle: %+v", events[2:])
	}

	// Escape cancels an active 3D plugin drag cleanly.
	events = nil
	app.Post(func() {
		ed.BeginViewport3DDrag(140, 100, cw, ch, false)
		ed.UpdateViewport3DDrag(180, 120)
	})
	app.Settle()
	if !ed.PluginDragging3D() {
		t.Fatal("expected PluginDragging3D() == true before Escape")
	}
	app.Key("escape")
	if ed.PluginDragging3D() {
		t.Fatal("Escape did not cancel 3D plugin drag")
	}
	if len(events) == 0 || events[len(events)-1].kind != "cancel" {
		t.Fatalf("expected OnDragCancel3D on Escape, got %+v", events)
	}
}

// Acceptance #2: Dragging a plugin component's handle in 3D changes the field as one undo step,
// supports points/lines/boxes in object space, and Escape cancels cleanly.
func TestPluginComponentGizmo3DDragCommitsOneUndoStep(t *testing.T) {
	dir := write3DPluginProject(t, "volumes")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(volumeComponents(t)); err != nil {
		t.Fatal(err)
	}
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "volumes",
		Gizmos3D: []editor.ComponentGizmo3D{{
			Component: "spawn_volume",
			Handles: func(ed *editor.Editor, objectID string) []editor.GizmoHandle3D {
				r := float32(floatField(ed, objectID, "spawn_volume", "radius"))
				h := float32(floatField(ed, objectID, "spawn_volume", "height"))
				offX := float32(floatField(ed, objectID, "spawn_volume", "offsetX"))
				center := math3d.Vec3{X: offX, Y: 0, Z: 0}
				return []editor.GizmoHandle3D{
					{
						Kind:        editor.GizmoHandleBox3D,
						Position:    center,
						Size:        math3d.Vec3{X: r * 2, Y: h, Z: r * 2},
						Color:       core.RGBA(0x38BDF8, 0.9),
						DisplayOnly: true,
					},
					{
						Kind:        editor.GizmoHandleLine3D,
						Start:       center,
						End:         center.Add(math3d.Vec3{X: r}),
						Color:       core.RGBA(0x22C55E, 0.9),
						DisplayOnly: true,
					},
					{
						ID:       "radius+x",
						Kind:     editor.GizmoHandlePoint3D,
						Position: center.Add(math3d.Vec3{X: r}),
						Axis:     math3d.Vec3{X: 1},
						Color:    core.RGBA(0x22C55E, 1),
						Label:    "R",
					},
					{
						ID:       "height+y",
						Kind:     editor.GizmoHandlePoint3D,
						Position: center.Add(math3d.Vec3{Y: h * 0.5}),
						Axis:     math3d.Vec3{Y: 1},
						Color:    core.RGBA(0xF59E0B, 1),
						Label:    "H",
					},
				}
			},
			Drag: func(ed *editor.Editor, drag editor.GizmoDrag3D) error {
				delta, ok := drag.AxisDelta(drag.Handle.Axis)
				if !ok {
					return nil
				}
				switch drag.Handle.ID {
				case "radius+x":
					startR := floatField(ed, drag.ObjectID, "spawn_volume", "radius")
					nextR := max(1.0, startR+float64(delta))
					// Also update offsetX so we verify multi-field edits commit as ONE undo step.
					startOff := floatField(ed, drag.ObjectID, "spawn_volume", "offsetX")
					if err := ed.SetComponentField(drag.ObjectID, "spawn_volume", "radius", nextR); err != nil {
						return err
					}
					return ed.SetComponentField(drag.ObjectID, "spawn_volume", "offsetX", startOff+(nextR-startR)*0.25)
				case "height+y":
					startH := floatField(ed, drag.ObjectID, "spawn_volume", "height")
					return ed.SetComponentField(drag.ObjectID, "spawn_volume", "height", max(1.0, startH+float64(delta)*2))
				}
				return nil
			},
		}},
	}); err != nil {
		t.Fatalf("RegisterPlugin volumes: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.SelectObject("spawner")
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.55,
		Pitch:    0.45,
		Distance: 160,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	cw, ch := ed.CanvasSize()
	proj := ed.Projection3D(cw, ch)

	// Project the +X radius handle at object-space (20, 0, 0) and a drag target at (35, 0, 0).
	sx0, sy0, _, vis0 := proj.Project(math3d.Vec3{X: 20, Y: 0, Z: 0})
	sx1, sy1, _, vis1 := proj.Project(math3d.Vec3{X: 35, Y: 0, Z: 0})
	if !vis0 || !vis1 {
		t.Fatal("handle drag endpoints are not visible")
	}

	// PickComponentGizmo3D hits the plugin handle.
	picked, ok := ed.PickComponentGizmo3D(sx0, sy0, cw, ch)
	if !ok || picked.ObjectID != "spawner" || picked.Component != "spawn_volume" || picked.Handle.ID != "radius+x" {
		t.Fatalf("PickComponentGizmo3D = (%+v, %v), want spawner/spawn_volume/radius+x", picked, ok)
	}

	histBefore := ed.History.Index()

	// Drag the handle through the mounted 3D viewport with multiple intermediate moves.
	midX := (sx0 + sx1) * 0.5
	midY := (sy0 + sy1) * 0.5
	app.DragAt("id:editor-viewport-frame",
		core.Point{X: core.Points(sx0), Y: core.Points(sy0)},
		core.Point{X: core.Points(midX - sx0), Y: core.Points(midY - sy0)},
		core.Point{X: core.Points(sx1 - sx0), Y: core.Points(sy1 - sy0)})

	// Both radius and offsetX updated accurately and committed as ONE undo step.
	gotR := floatField(ed, "spawner", "spawn_volume", "radius")
	gotOff := floatField(ed, "spawner", "spawn_volume", "offsetX")
	if math.Abs(gotR-35.0) > 1.0 {
		t.Fatalf("radius after 3D handle drag = %v, want ~35", gotR)
	}
	if math.Abs(gotOff-3.75) > 0.3 {
		t.Fatalf("offsetX after 3D handle drag = %v, want ~3.75", gotOff)
	}
	if ed.History.Index() != histBefore+1 {
		t.Fatalf("History.Index() = %d, want %d (single undo step)", ed.History.Index(), histBefore+1)
	}

	// One Undo restores both fields cleanly; one Redo brings them back.
	if !ed.Undo() {
		t.Fatal("Undo returned false")
	}
	if r := floatField(ed, "spawner", "spawn_volume", "radius"); r != 20.0 {
		t.Fatalf("radius after Undo = %v, want 20", r)
	}
	if off := floatField(ed, "spawner", "spawn_volume", "offsetX"); off != 0.0 {
		t.Fatalf("offsetX after Undo = %v, want 0", off)
	}
	if !ed.Redo() {
		t.Fatal("Redo returned false")
	}
	if r := floatField(ed, "spawner", "spawn_volume", "radius"); math.Abs(r-gotR) > 1e-4 {
		t.Fatalf("radius after Redo = %v, want %v", r, gotR)
	}

	// Escape during an active handle drag rolls back the live preview and adds no history entry.
	ed.Undo() // back to radius=20
	histBeforeCancel := ed.History.Index()
	app.Post(func() {
		if _, ok := ed.BeginComponentGizmoDrag3D(sx0, sy0, cw, ch); !ok {
			t.Error("BeginComponentGizmoDrag3D failed")
		}
		ed.UpdateComponentGizmoDrag3D(sx1, sy1)
	})
	app.Settle()
	if !ed.DraggingComponentGizmo3D() {
		t.Fatal("expected DraggingComponentGizmo3D() == true")
	}
	if liveR := floatField(ed, "spawner", "spawn_volume", "radius"); math.Abs(liveR-35.0) > 1.0 {
		t.Fatalf("live preview radius = %v, want ~35", liveR)
	}
	app.Key("escape")
	if ed.DraggingComponentGizmo3D() {
		t.Fatal("Escape did not cancel component gizmo drag")
	}
	if afterCancel := floatField(ed, "spawner", "spawn_volume", "radius"); afterCancel != 20.0 {
		t.Fatalf("radius after Escape cancel = %v, want 20", afterCancel)
	}
	if ed.History.Index() != histBeforeCancel {
		t.Fatalf("Escape cancel pushed a history entry: index %d != %d", ed.History.Index(), histBeforeCancel)
	}
}

// Built-in 3D component gizmos (collider3d box/sphere/capsule and trigger3d) reuse the exact same
// ComponentGizmo3D API and commit handle drags as one undo step.
func TestBuiltinCollider3DAndTrigger3DComponentGizmos(t *testing.T) {
	const w, h float32 = 640, 360
	ed := newTest3DEditor(t,
		sceneformat.ObjectDoc{
			ID:    "box_col",
			Name:  "Box Collider",
			X:     0,
			Y:     0,
			PosZ:  0,
			Depth: 10,
			Components: map[string]any{
				"collider3d": map[string]any{
					"version": 1,
					"shape":   "box",
					"width":   20.0,
					"height":  16.0,
					"depth":   12.0,
				},
			},
		},
		sceneformat.ObjectDoc{
			ID:    "sphere_trig",
			Name:  "Sphere Trigger",
			X:     0,
			Y:     0,
			PosZ:  0,
			Depth: 10,
			Components: map[string]any{
				"trigger3d": map[string]any{
					"version": 1,
					"shape":   "sphere",
					"radius":  15.0,
				},
			},
		},
	)
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.55,
		Pitch:    0.45,
		Distance: 140,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	proj := ed.Projection3D(w, h)

	// 1. Box collider3d +x face handle at (10, 0, 0) dragged to (20, 0, 0) -> width 30, offsetX 5 in one undo step.
	ed.SelectObject("box_col")
	sx0, sy0, _, vis0 := proj.Project(math3d.Vec3{X: 10, Y: 0, Z: 0})
	sx1, sy1, _, vis1 := proj.Project(math3d.Vec3{X: 20, Y: 0, Z: 0})
	if !vis0 || !vis1 {
		t.Fatal("box collider handle points not visible")
	}
	picked, ok := ed.BeginComponentGizmoDrag3D(sx0, sy0, w, h)
	if !ok || picked.Component != "collider3d" || picked.Handle.ID != "+x" {
		t.Fatalf("BeginComponentGizmoDrag3D = (%+v, %v), want collider3d/+x", picked, ok)
	}
	ed.UpdateComponentGizmoDrag3D(sx1, sy1)
	if !ed.EndComponentGizmoDrag3D() {
		t.Fatal("EndComponentGizmoDrag3D returned false")
	}
	gotW := floatField(ed, "box_col", "collider3d", "width")
	gotOffX := floatField(ed, "box_col", "collider3d", "offsetX")
	if math.Abs(gotW-30.0) > 0.75 || math.Abs(gotOffX-5.0) > 0.5 {
		t.Fatalf("box collider after +x drag: width=%v offsetX=%v, want ~30 and ~5", gotW, gotOffX)
	}
	if ed.History.Index() != 1 {
		t.Fatalf("History.Index() = %d, want 1", ed.History.Index())
	}
	ed.Undo()
	if wAfter := floatField(ed, "box_col", "collider3d", "width"); wAfter != 20.0 {
		t.Fatalf("width after Undo = %v, want 20", wAfter)
	}

	// 2. Sphere trigger3d radius+x handle at (15, 0, 0) dragged to (25, 0, 0) -> radius 25 in one undo step.
	ed.SelectObject("sphere_trig")
	rx0, ry0, _, rvis0 := proj.Project(math3d.Vec3{X: 15, Y: 0, Z: 0})
	rx1, ry1, _, rvis1 := proj.Project(math3d.Vec3{X: 25, Y: 0, Z: 0})
	if !rvis0 || !rvis1 {
		t.Fatal("sphere trigger handle points not visible")
	}
	picked, ok = ed.BeginComponentGizmoDrag3D(rx0, ry0, w, h)
	if !ok || picked.Component != "trigger3d" || picked.Handle.ID != "radius+x" {
		t.Fatalf("BeginComponentGizmoDrag3D = (%+v, %v), want trigger3d/radius+x", picked, ok)
	}
	ed.UpdateComponentGizmoDrag3D(rx1, ry1)
	if !ed.EndComponentGizmoDrag3D() {
		t.Fatal("EndComponentGizmoDrag3D returned false")
	}
	if gotR := floatField(ed, "sphere_trig", "trigger3d", "radius"); math.Abs(gotR-25.0) > 0.75 {
		t.Fatalf("sphere trigger radius = %v, want ~25", gotR)
	}
	if ed.History.Index() != 1 {
		t.Fatalf("History.Index() = %d, want 1", ed.History.Index())
	}
	ed.Undo()
	if rAfter := floatField(ed, "sphere_trig", "trigger3d", "radius"); rAfter != 15.0 {
		t.Fatalf("sphere radius after Undo = %v, want 15", rAfter)
	}
}

// RegisterPlugin validation for 3D component gizmos and 3D-only tools, plus plugin precedence order.
func TestRegisterPlugin3DGizmosValidationAndPrecedence(t *testing.T) {
	handles := func(*editor.Editor, string) []editor.GizmoHandle3D { return nil }
	cases := map[string]editor.Plugin{
		"gizmo no component":      {Name: "p", Gizmos3D: []editor.ComponentGizmo3D{{Handles: handles}}},
		"gizmo control component": {Name: "p", Gizmos3D: []editor.ComponentGizmo3D{{Component: "a\nb", Handles: handles}}},
		"gizmo no handles":        {Name: "p", Gizmos3D: []editor.ComponentGizmo3D{{Component: "spawn_volume"}}},
		"duplicate gizmo":         {Name: "p", Gizmos3D: []editor.ComponentGizmo3D{{Component: "spawn_volume", Handles: handles}, {Component: "spawn_volume", Handles: handles}}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			ed := newTestEditor(t)
			if err := ed.RegisterPlugin(p); err == nil {
				t.Fatal("RegisterPlugin succeeded for invalid 3D gizmo plugin")
			}
		})
	}

	// First active plugin in editor.plugins order wins when two plugins register a 3D gizmo for the same component.
	dir := write3DPluginProject(t, "first", "second")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(volumeComponents(t)); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct {
		name, handleID string
	}{{"first", "h-first"}, {"second", "h-second"}} {
		id := spec.handleID
		if err := ed.RegisterPlugin(editor.Plugin{
			Name: spec.name,
			Gizmos3D: []editor.ComponentGizmo3D{{
				Component: "spawn_volume",
				Handles: func(*editor.Editor, string) []editor.GizmoHandle3D {
					return []editor.GizmoHandle3D{{ID: id, Kind: editor.GizmoHandlePoint3D, Position: math3d.Vec3{X: 5}}}
				},
			}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	got := ed.ComponentGizmoHandles3D("spawner")
	if len(got) != 1 || got[0].Handle.ID != "h-first" {
		t.Fatalf("ComponentGizmoHandles3D = %+v, want [h-first]", got)
	}
	// Turning "first" off makes "second" win.
	if err := ed.SetPluginEnabled("first", false); err != nil {
		t.Fatal(err)
	}
	got = ed.ComponentGizmoHandles3D("spawner")
	if len(got) != 1 || got[0].Handle.ID != "h-second" {
		t.Fatalf("after disabling first, ComponentGizmoHandles3D = %+v, want [h-second]", got)
	}
	if !reflect.DeepEqual(ed.ActivePlugins(), []string{"second"}) {
		t.Fatalf("ActivePlugins = %v", ed.ActivePlugins())
	}
}

// Verify line/box draggable handles, ShowUnselected drawing, PlaneDelta/ViewPlaneDelta helpers,
// capsule built-in collider3d handles, and rotated/translated object-space transforms.
func TestComponentGizmo3DObjectSpaceRotationLineAndBoxHandles(t *testing.T) {
	const w, h float32 = 640, 360
	dir := write3DPluginProject(t, "volumes")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(volumeComponents(t)); err != nil {
		t.Fatal(err)
	}
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "volumes",
		Gizmos3D: []editor.ComponentGizmo3D{{
			Component:      "spawn_volume",
			ShowUnselected: true,
			Handles: func(ed *editor.Editor, objectID string) []editor.GizmoHandle3D {
				r := float32(floatField(ed, objectID, "spawn_volume", "radius"))
				return []editor.GizmoHandle3D{
					{
						ID:    "edge",
						Kind:  editor.GizmoHandleLine3D,
						Start: math3d.Vec3{X: r, Y: -8, Z: 0},
						End:   math3d.Vec3{X: r, Y: 8, Z: 0},
						Axis:  math3d.Vec3{X: 1},
					},
					{
						ID:       "bounds",
						Kind:     editor.GizmoHandleBox3D,
						Position: math3d.Vec3{X: 0, Y: 0, Z: 0},
						Size:     math3d.Vec3{X: r * 2, Y: 16, Z: r * 2},
					},
				}
			},
			Drag: func(ed *editor.Editor, drag editor.GizmoDrag3D) error {
				switch drag.Handle.ID {
				case "edge":
					delta, ok := drag.AxisDelta(drag.Handle.Axis)
					if !ok {
						return nil
					}
					r := floatField(ed, drag.ObjectID, "spawn_volume", "radius")
					return ed.SetComponentField(drag.ObjectID, "spawn_volume", "radius", max(1.0, r+float64(delta)))
				case "bounds":
					delta, ok := drag.PlaneDelta(math3d.Vec3{Y: 1})
					if !ok {
						return nil
					}
					off := floatField(ed, drag.ObjectID, "spawn_volume", "offsetX")
					return ed.SetComponentField(drag.ObjectID, "spawn_volume", "offsetX", off+float64(delta.X))
				}
				return nil
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetViewMode(editor.ViewMode3D)

	// Rotate spawner 90 degrees around Y so object +X points along world -Z, and translate to (10, 0, 5).
	if err := ed.MoveObject3D("spawner", 10, 0, 5); err != nil {
		t.Fatal(err)
	}
	if err := ed.RotateObject3D("spawner", 0, float32(math.Pi/2), 0); err != nil {
		t.Fatal(err)
	}
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{10, 0, 5},
		Yaw:      0.6,
		Pitch:    0.45,
		Distance: 160,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}

	// ShowUnselected draws even when spawner is not selected.
	ed.SelectObject("")
	var unselectedDraw core.Drawing
	ed.DrawViewport(&unselectedDraw, core.Size{Width: core.Points(w), Height: core.Points(h)})
	if len(unselectedDraw.Ops) == 0 {
		t.Fatal("expected unselected ShowUnselected gizmo to draw ops")
	}

	// Select spawner and drag its line handle along rotated object +X (world -Z).
	ed.SelectObject("spawner")
	proj := ed.Projection3D(w, h)
	// Object-space (20, 0, 0) rotated +90deg around Y sits at world (10, 0, 5 - 20) = (10, 0, -15).
	startWorld := math3d.Vec3{X: 10, Y: 0, Z: -15}
	endWorld := math3d.Vec3{X: 10, Y: 0, Z: -25} // +10 in object +X
	sx0, sy0, _, vis0 := proj.Project(startWorld)
	sx1, sy1, _, vis1 := proj.Project(endWorld)
	if !vis0 || !vis1 {
		t.Fatal("rotated line handle endpoints not visible")
	}
	picked, ok := ed.BeginComponentGizmoDrag3D(sx0, sy0, w, h)
	if !ok || picked.Handle.ID != "edge" {
		t.Fatalf("BeginComponentGizmoDrag3D on rotated line = (%+v, %v), want edge", picked, ok)
	}
	ed.UpdateComponentGizmoDrag3D(sx1, sy1)
	if !ed.EndComponentGizmoDrag3D() {
		t.Fatal("EndComponentGizmoDrag3D returned false")
	}
	if gotR := floatField(ed, "spawner", "spawn_volume", "radius"); math.Abs(gotR-30.0) > 1.0 {
		t.Fatalf("radius after rotated line handle drag = %v, want ~30", gotR)
	}

	// Built-in capsule collider3d height handle drag commits as one undo step.
	capEd := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:    "cap",
		Name:  "Capsule",
		Depth: 10,
		Components: map[string]any{
			"collider3d": map[string]any{
				"version": 1,
				"shape":   "capsule",
				"radius":  5.0,
				"height":  20.0,
			},
		},
	})
	capEd.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.5,
		Pitch:    0.3,
		Distance: 120,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	capEd.SelectObject("cap")
	capProj := capEd.Projection3D(w, h)
	hx0, hy0, _, hvis0 := capProj.Project(math3d.Vec3{X: 0, Y: 10, Z: 0})
	hx1, hy1, _, hvis1 := capProj.Project(math3d.Vec3{X: 0, Y: 18, Z: 0})
	if !hvis0 || !hvis1 {
		t.Fatal("capsule height handle points not visible")
	}
	cp, ok := capEd.BeginComponentGizmoDrag3D(hx0, hy0, w, h)
	if !ok || cp.Component != "collider3d" || cp.Handle.ID != "height+y" {
		t.Fatalf("BeginComponentGizmoDrag3D capsule = (%+v, %v), want collider3d/height+y", cp, ok)
	}
	capEd.UpdateComponentGizmoDrag3D(hx1, hy1)
	if !capEd.EndComponentGizmoDrag3D() {
		t.Fatal("EndComponentGizmoDrag3D capsule returned false")
	}
	if gotH := floatField(capEd, "cap", "collider3d", "height"); math.Abs(gotH-28.0) > 0.75 {
		t.Fatalf("capsule height after +y drag = %v, want ~28", gotH)
	}
	if capEd.History.Index() != 1 {
		t.Fatalf("capsule drag History.Index() = %d, want 1", capEd.History.Index())
	}
	capEd.Undo()
	if gotH := floatField(capEd, "cap", "collider3d", "height"); gotH != 20.0 {
		t.Fatalf("capsule height after Undo = %v, want 20", gotH)
	}
}

func TestComponentGizmo3DDefersToColliderGizmoMode(t *testing.T) {
	const w, h float32 = 800, 600
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:    "cyl",
		Name:  "Cylinder",
		Depth: 10,
		Components: map[string]any{
			"collider3d": map[string]any{
				"version": 1,
				"shape":   "cylinder",
				"radius":  6.0,
				"height":  14.0,
			},
		},
	})
	ed.ShowPhysicsDebug = true
	ed.SelectObject("cyl")

	// In Gizmo3DCollider mode, the built-in collider3d/trigger3d component gizmos yield to the dedicated collider gizmo.
	ed.SetGizmoMode3D(editor.Gizmo3DCollider)
	if handles := ed.ComponentGizmoHandles3D("cyl"); len(handles) != 0 {
		t.Fatalf("ComponentGizmoHandles3D in Gizmo3DCollider mode = %d handles, want 0", len(handles))
	}
	proj := ed.Projection3D(w, h)
	hx, hy, _, vis := proj.Project(math3d.Vec3{X: 6, Y: 0, Z: 0})
	if !vis {
		t.Fatal("cylinder collider +X handle not visible")
	}
	ed.BeginViewport3DDrag(hx, hy, w, h, false)
	if ed.DraggingComponentGizmo3D() {
		t.Fatal("BeginViewport3DDrag in Gizmo3DCollider mode hijacked drag via component gizmo")
	}
	if !ed.DraggingGizmo3D() {
		t.Fatal("expected BeginViewport3DDrag in Gizmo3DCollider mode to start dedicated collider gizmo drag")
	}
	ed.CancelViewport3DDrag()

	// Back in Gizmo3DTranslate mode with ShowPhysicsDebug on, built-in box/sphere/capsule handles are available.
	boxEd := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:    "box",
		Name:  "Box",
		Depth: 10,
		Components: map[string]any{
			"trigger3d": map[string]any{
				"version": 1,
				"shape":   "box",
				"width":   10.0,
				"height":  10.0,
				"depth":   10.0,
			},
		},
	})
	boxEd.ShowPhysicsDebug = true
	boxEd.SelectObject("box")
	boxEd.SetGizmoMode3D(editor.Gizmo3DCollider)
	if handles := boxEd.ComponentGizmoHandles3D("box"); len(handles) != 0 {
		t.Fatalf("trigger3d ComponentGizmoHandles3D in Gizmo3DCollider mode = %d, want 0", len(handles))
	}
	boxEd.SetGizmoMode3D(editor.Gizmo3DTranslate)
	if handles := boxEd.ComponentGizmoHandles3D("box"); len(handles) == 0 {
		t.Fatal("expected trigger3d built-in component gizmo handles in Gizmo3DTranslate mode")
	}
}

func TestProjection3DLookThroughCameraAndTerrainBrushBlock(t *testing.T) {
	const w, h float32 = 800, 600
	ed := newTest3DEditor(t,
		sceneformat.ObjectDoc{
			ID:    "cam1",
			Name:  "MainCam",
			X:     0,
			Y:     10,
			PosZ:  40,
			Depth: 4,
			Camera: &sceneformat.Camera3DDoc{
				Current: true,
				FOV:     sceneformat.Float32(60),
				Near:    sceneformat.Float32(0.5),
				Far:     sceneformat.Float32(1000),
			},
		},
		sceneformat.ObjectDoc{
			ID:    "box1",
			Name:  "Box",
			Depth: 10,
			Components: map[string]any{
				"collider3d": map[string]any{
					"version": 1,
					"shape":   "box",
					"width":   20.0,
					"height":  20.0,
					"depth":   20.0,
				},
			},
		},
	)
	ed.ShowPhysicsDebug = true
	// Point orbit camera somewhere completely different from cam1 (at (0,10,40) looking -Z).
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{200, 0, 200},
		Yaw:      2.2,
		Pitch:    0.6,
		Distance: 150,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	orbitRay := ed.PickRay3D(w/2, h/2, w, h)

	if err := ed.SetLookThroughCamera3D(true); err != nil {
		t.Fatalf("SetLookThroughCamera3D(true): %v", err)
	}
	ltProj := ed.Projection3D(w, h)
	ltRay := ed.PickRay3D(w/2, h/2, w, h)
	if math.Abs(float64(ltRay.Origin.X-0)) > 1e-3 || math.Abs(float64(ltRay.Origin.Y-10)) > 1e-3 || math.Abs(float64(ltRay.Origin.Z-40)) > 1e-3 {
		t.Fatalf("look-through PickRay3D origin = %+v, want (0, 10, 40)", ltRay.Origin)
	}
	if math.Abs(float64(ltRay.Dir.Z+1)) > 1e-3 {
		t.Fatalf("look-through PickRay3D dir = %+v, want (0, 0, -1)", ltRay.Dir)
	}
	if ltRay.Origin == orbitRay.Origin {
		t.Fatal("look-through PickRay3D matched orbit camera instead of scene camera")
	}
	// Point directly in front of cam1 at (0, 10, 0) projects to viewport center.
	sx, sy, _, vis := ltProj.Project(math3d.Vec3{X: 0, Y: 10, Z: 0})
	if !vis || math.Abs(float64(sx-w/2)) > 1 || math.Abs(float64(sy-h/2)) > 1 {
		t.Fatalf("look-through Project(0,10,0) = (%v, %v, vis=%v), want center (%v, %v)", sx, sy, vis, w/2, h/2)
	}
	sc, _, ok := ed.ActiveSceneCamera3D(w, h)
	if !ok || sc == nil || ltProj.ViewProj != sc.ViewProj() {
		t.Fatal("look-through Projection3D.ViewProj does not match ActiveSceneCamera3D.ViewProj()")
	}

	// Now verify Terrain brush blocks component gizmos and cancels active 3D drags.
	if err := ed.SetLookThroughCamera3D(false); err != nil {
		t.Fatal(err)
	}
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.5,
		Pitch:    0.35,
		Distance: 120,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	ed.SelectObject("box1")
	proj := ed.Projection3D(w, h)
	hx, hy, _, hvis := proj.Project(math3d.Vec3{X: 10, Y: 0, Z: 0})
	if !hvis {
		t.Fatal("box collider +X handle not visible")
	}
	if _, ok := ed.BeginComponentGizmoDrag3D(hx, hy, w, h); !ok {
		t.Fatal("expected BeginComponentGizmoDrag3D to start before terrain brush")
	}

	// Open a terrain and activate a terrain brush: active drag must cancel and picking must be blocked.
	if err := ed.NewTerrain(editor.NewTerrainOptions{}); err != nil {
		t.Fatalf("NewTerrain: %v", err)
	}
	ed.SetTerrainBrushMode(editor.TerrainBrushRaise)
	if ed.DraggingComponentGizmo3D() {
		t.Fatal("SetTerrainBrushMode(TerrainBrushRaise) did not cancel active component gizmo drag")
	}
	if _, hit := ed.PickComponentGizmo3D(hx, hy, w, h); hit {
		t.Fatal("PickComponentGizmo3D succeeded while 3D terrain brush is active")
	}
}

func TestPlugin3DAndComponentGizmoCancelOnSceneTabs(t *testing.T) {
	const w, h float32 = 800, 600
	dir := t.TempDir()
	sceneA := filepath.Join(dir, "a.scene.yaml")
	sceneB := filepath.Join(dir, "b.scene.yaml")

	docA := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "SceneA",
		VirtualWidth:  800,
		VirtualHeight: 600,
		Layers: []sceneformat.LayerDoc{{
			Name:  "world",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{{
				ID:    "box",
				Name:  "Box",
				Depth: 10,
				Components: map[string]any{
					"collider3d": map[string]any{
						"version": 1,
						"shape":   "box",
						"width":   20.0,
						"height":  20.0,
						"depth":   20.0,
					},
				},
			}},
		}},
	}
	docB := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "SceneB",
		VirtualWidth:  800,
		VirtualHeight: 600,
		Layers:        []sceneformat.LayerDoc{{Name: "world", Space: sceneformat.SpaceWorld}},
	}
	dataA, err := sceneformat.Encode(docA)
	if err != nil {
		t.Fatal(err)
	}
	dataB, err := sceneformat.Encode(docB)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sceneA, dataA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sceneB, dataB, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenFile(sceneA); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenFile(sceneB); err != nil {
		t.Fatal(err)
	}
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.ShowPhysicsDebug = true
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.5,
		Pitch:    0.35,
		Distance: 120,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	ed.SelectObject("box")

	proj := ed.Projection3D(w, h)
	hx0, hy0, _, vis0 := proj.Project(math3d.Vec3{X: 10, Y: 0, Z: 0})
	hx1, hy1, _, vis1 := proj.Project(math3d.Vec3{X: 25, Y: 0, Z: 0})
	if !vis0 || !vis1 {
		t.Fatal("box handle endpoints not visible")
	}

	// 1. SaveTab mid-drag must cancel & roll back preview mutations before writing to disk.
	if _, ok := ed.BeginComponentGizmoDrag3D(hx0, hy0, w, h); !ok {
		t.Fatal("BeginComponentGizmoDrag3D failed")
	}
	ed.UpdateComponentGizmoDrag3D(hx1, hy1)
	if gotW := floatField(ed, "box", "collider3d", "width"); math.Abs(gotW-20.0) < 1.0 {
		t.Fatalf("expected live preview to change width before SaveTab, got %v", gotW)
	}
	if err := ed.SaveTab(0); err != nil {
		t.Fatalf("SaveTab: %v", err)
	}
	if ed.DraggingComponentGizmo3D() {
		t.Fatal("SaveTab did not cancel active component gizmo drag")
	}
	if gotW := floatField(ed, "box", "collider3d", "width"); gotW != 20.0 {
		t.Fatalf("width after SaveTab cancel = %v, want 20", gotW)
	}
	savedBytes, err := os.ReadFile(sceneA)
	if err != nil {
		t.Fatal(err)
	}
	savedDoc, err := sceneformat.Parse(savedBytes)
	if err != nil {
		t.Fatal(err)
	}
	if col, ok := savedDoc.Layers[0].Objects[0].Components["collider3d"].(map[string]any); ok {
		var wVal float64
		switch v := col["width"].(type) {
		case float64:
			wVal = v
		case int:
			wVal = float64(v)
		}
		if wVal != 20.0 {
			t.Fatalf("saved width on disk = %v (%T), want 20 (preview must not be written)", col["width"], col["width"])
		}
	}

	// 2. SelectTab mid-drag must cancel & roll back component gizmo drag.
	if _, ok := ed.BeginComponentGizmoDrag3D(hx0, hy0, w, h); !ok {
		t.Fatal("second BeginComponentGizmoDrag3D failed")
	}
	ed.UpdateComponentGizmoDrag3D(hx1, hy1)
	if err := ed.SelectTab(1); err != nil {
		t.Fatalf("SelectTab(1): %v", err)
	}
	if ed.DraggingComponentGizmo3D() {
		t.Fatal("SelectTab did not cancel active component gizmo drag")
	}
	if err := ed.SelectTab(0); err != nil {
		t.Fatalf("SelectTab(0): %v", err)
	}
	if gotW := floatField(ed, "box", "collider3d", "width"); gotW != 20.0 {
		t.Fatalf("width after SelectTab cancel = %v, want 20", gotW)
	}
}

func TestComponentGizmo3DPreviewGroupReentrantCancelAndNonFinite(t *testing.T) {
	const w, h float32 = 800, 600
	dir := write3DPluginProject(t, "reentrant")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(volumeComponents(t)); err != nil {
		t.Fatal(err)
	}

	dragCancelCount := 0
	endRayFinite := false
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "reentrant",
		Tools: []editor.PluginTool{{
			ID:            "tool3d",
			Label:         "Tool3D",
			OnDragBegin3D: func(_ *editor.Editor, _ math3d.Ray) bool { return true },
			OnDragEnd3D: func(_ *editor.Editor, r math3d.Ray) {
				endRayFinite = !math.IsNaN(float64(r.Origin.X)) && !math.IsInf(float64(r.Origin.X), 0) &&
					!math.IsNaN(float64(r.Dir.X)) && !math.IsInf(float64(r.Dir.X), 0)
			},
			OnDragCancel3D: func(_ *editor.Editor) { dragCancelCount++ },
		}},
		Gizmos3D: []editor.ComponentGizmo3D{{
			Component: "spawn_volume",
			Handles: func(_ *editor.Editor, _ string) []editor.GizmoHandle3D {
				return []editor.GizmoHandle3D{{
					ID:       "self_cancel",
					Kind:     editor.GizmoHandlePoint3D,
					Position: math3d.Vec3{X: 10, Y: 0, Z: 0},
				}}
			},
			Drag: func(ed *editor.Editor, drag editor.GizmoDrag3D) error {
				// Mutate a field and then cancel the drag re-entrantly.
				if err := ed.SetComponentField(drag.ObjectID, "spawn_volume", "radius", 99.0); err != nil {
					return err
				}
				ed.CancelComponentGizmoDrag3D()
				return nil
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.5,
		Pitch:    0.35,
		Distance: 120,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	ed.SelectObject("spawner")

	// 1. Re-entrant cancel inside Drag rolls back its mutation and preserves an outer History.Group.
	proj := ed.Projection3D(w, h)
	hx, hy, _, vis := proj.Project(math3d.Vec3{X: 10, Y: 0, Z: 0})
	if !vis {
		t.Fatal("handle not visible")
	}
	err = ed.History.Group("outer", func() error {
		if err := ed.SetComponentField("spawner", "spawn_volume", "height", 42.0); err != nil {
			return err
		}
		if _, ok := ed.BeginComponentGizmoDrag3D(hx, hy, w, h); !ok {
			t.Fatal("BeginComponentGizmoDrag3D failed")
		}
		ed.UpdateComponentGizmoDrag3D(hx+15, hy)
		if ed.DraggingComponentGizmo3D() {
			t.Fatal("expected re-entrant CancelComponentGizmoDrag3D to end drag")
		}
		if gotR := floatField(ed, "spawner", "spawn_volume", "radius"); gotR != 20.0 {
			t.Fatalf("radius after re-entrant cancel = %v, want 20 (rolled back)", gotR)
		}
		return ed.SetComponentField("spawner", "spawn_volume", "offsetX", 7.0)
	})
	if err != nil {
		t.Fatalf("outer History.Group: %v", err)
	}
	if ed.History.Index() != 1 {
		t.Fatalf("outer History.Group History.Index() = %d, want 1", ed.History.Index())
	}
	ed.Undo()
	if gotH := floatField(ed, "spawner", "spawn_volume", "height"); gotH != 24.0 {
		t.Fatalf("height after undoing outer group = %v, want 24", gotH)
	}
	if gotO := floatField(ed, "spawner", "spawn_volume", "offsetX"); gotO != 0.0 {
		t.Fatalf("offsetX after undoing outer group = %v, want 0", gotO)
	}

	// 2. Non-finite guards on DrawPoint/DrawLine/DrawBox, GizmoDrag3D helpers, EndPluginDrag3D, and nil SetPluginTool.
	var nilEd *editor.Editor
	if err := nilEd.SetPluginTool("reentrant", "tool3d"); err == nil {
		t.Fatal("nil Editor.SetPluginTool returned nil error")
	}
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	var draw core.Drawing
	proj.DrawPoint(&draw, math3d.Vec3{X: nan}, nan, core.RGBA(0xFFFFFF, 1))
	proj.DrawPoint(&draw, math3d.Vec3{X: 0, Y: 0, Z: 0}, nan, core.RGBA(0xFFFFFF, 1))
	proj.DrawLine(&draw, math3d.Vec3{X: inf}, math3d.Vec3{}, core.RGBA(0xFFFFFF, 1), nan)
	proj.DrawBox(&draw, math3d.Vec3{X: nan}, math3d.Vec3{X: 1, Y: 1, Z: 1}, math3d.Mat4Identity(), core.RGBA(0xFFFFFF, 1), inf)

	badDrag := editor.GizmoDrag3D{
		StartPoint:    math3d.Vec3{X: nan},
		ScreenStart:   [2]float32{nan, 0},
		ScreenCurrent: [2]float32{10, inf},
		LocalRay:      math3d.Ray{Origin: math3d.Vec3{X: nan}, Dir: math3d.Vec3{Z: -1}},
		StartLocalRay: math3d.Ray{Origin: math3d.Vec3{X: 0}, Dir: math3d.Vec3{Z: -1}},
		Camera:        ed.Camera3D,
		ViewportWidth: w, ViewportHeight: h,
	}
	if _, ok := badDrag.AxisDelta(math3d.Vec3{X: 1}); ok {
		t.Fatal("AxisDelta succeeded on NaN StartPoint/rays")
	}
	if _, ok := badDrag.PlanePoint(math3d.Vec3{}, math3d.Vec3{Y: 1}); ok {
		t.Fatal("PlanePoint succeeded on NaN LocalRay")
	}
	if _, ok := badDrag.PlaneDelta(math3d.Vec3{Y: 1}); ok {
		t.Fatal("PlaneDelta succeeded on NaN StartPoint/LocalRay")
	}

	if err := ed.SetPluginTool("reentrant", "tool3d"); err != nil {
		t.Fatal(err)
	}
	if !ed.BeginPluginDrag3D(w/2, h/2, w, h) {
		t.Fatal("BeginPluginDrag3D failed")
	}
	if !ed.EndPluginDrag3D(nan, inf) {
		t.Fatal("EndPluginDrag3D with NaN/Inf returned false")
	}
	if !endRayFinite {
		t.Fatal("EndPluginDrag3D with NaN/Inf did not fall back to finite last ray")
	}
	_ = dragCancelCount
}
