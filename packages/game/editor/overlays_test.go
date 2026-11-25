package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newOverlaysTestEditor(t *testing.T) *editor.Editor {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := sceneformat.RegisterNavRegion2DComponent(reg); err != nil {
		t.Fatalf("register navregion2d: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name: "body",
		Fields: []sceneformat.Field{
			{Name: "shape", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{X: -12, Y: -12, Width: 24, Height: 24}},
		},
	}); err != nil {
		t.Fatalf("register body: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name: "trigger",
		Fields: []sceneformat.Field{
			{Name: "zone", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{X: -20, Y: -20, Width: 40, Height: 40}},
		},
	}); err != nil {
		t.Fatalf("register trigger: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name: "lever",
		Events: []sceneformat.Event{
			{Name: "pulled"},
		},
	}); err != nil {
		t.Fatalf("register lever: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name: "door",
		Handlers: []sceneformat.Handler{
			{Name: "open"},
		},
	}); err != nil {
		t.Fatalf("register door: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name: "patrol",
		Fields: []sceneformat.Field{
			{Name: "from", Kind: sceneformat.FieldFloat, Default: 0.0},
			{Name: "to", Kind: sceneformat.FieldFloat, Default: 0.0},
			{Name: "waypoints", Kind: sceneformat.FieldPolygon, Default: []sceneformat.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}}},
		},
	}); err != nil {
		t.Fatalf("register patrol: %v", err)
	}

	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#15181D",
		Camera: &sceneformat.CameraDoc{
			X: 320,
			Y: 180,
			Bounds: &sceneformat.BoundsDoc{
				X:      0,
				Y:      0,
				Width:  1280,
				Height: 720,
			},
		},
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "Actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:   "knight",
						Name: "Knight",
						X:    110,
						Y:    260,
						Components: map[string]any{
							"body": map[string]any{
								"shape": sceneformat.Rect{X: -10, Y: -16, Width: 20, Height: 32},
							},
						},
					},
					{
						ID:   "lever_01",
						Name: "lever_01",
						X:    180,
						Y:    260,
						Components: map[string]any{
							"lever": map[string]any{},
							"trigger": map[string]any{
								"zone": sceneformat.Rect{X: -18, Y: -18, Width: 36, Height: 36},
							},
						},
					},
					{
						ID:   "door_01",
						Name: "door_01",
						X:    490,
						Y:    252,
						Components: map[string]any{
							"door": map[string]any{},
							"trigger": map[string]any{
								"zone": sceneformat.Rect{X: -24, Y: -24, Width: 48, Height: 48},
							},
						},
					},
					{
						ID:   "slime_01",
						Name: "slime_01",
						X:    270,
						Y:    268,
						Components: map[string]any{
							"body": map[string]any{
								"shape": sceneformat.Rect{X: -13, Y: -8, Width: 26, Height: 16},
							},
							"patrol": map[string]any{
								"from": 220.0,
								"to":   350.0,
							},
						},
					},
					{
						ID:   "slime_02",
						Name: "slime_02",
						X:    390,
						Y:    210,
						Components: map[string]any{
							"patrol": map[string]any{
								"waypoints": []sceneformat.Point{{X: -30, Y: 0}, {X: 0, Y: -16}, {X: 40, Y: 0}},
							},
						},
					},
					{
						ID:   "nav_floor",
						Name: "nav_floor",
						X:    320,
						Y:    280,
						Components: map[string]any{
							sceneformat.NavRegion2DComponentName: map[string]any{
								"version": sceneformat.NavRegion2DComponentVersion,
								"mode":    "walkable",
								"points":  [][2]float64{{-260, -16}, {260, -16}, {260, 16}, {-260, 16}},
							},
						},
					},
				},
			},
		},
		Connections: []sceneformat.ConnectionDoc{
			{From: "lever_01", Event: "pulled", To: "door_01", Handler: "open"},
		},
	}

	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	ed.SetCanvasSize(640, 360)
	return ed
}

func drawOpsWithOverlays(t *testing.T, ed *editor.Editor, overlayIDs []string) []core.DrawOp {
	t.Helper()
	var d core.Drawing
	if err := ed.DrawViewportWithOverlays(&d, core.Size{Width: 640, Height: 360}, overlayIDs); err != nil {
		t.Fatalf("DrawViewportWithOverlays(%v): %v", overlayIDs, err)
	}
	return d.Ops
}

func hasOverlayFillText(ops []core.DrawOp, want string) bool {
	for _, op := range ops {
		if op.Kind == core.DrawFillText && op.Text == want {
			return true
		}
	}
	return false
}

func hasCubicOp(ops []core.DrawOp) bool {
	for _, op := range ops {
		if op.Kind == core.DrawCubicTo {
			return true
		}
	}
	return false
}

func TestViewportOverlaysAllSevenBuiltinsAndCounts(t *testing.T) {
	ed := newOverlaysTestEditor(t)

	// Verify all 7 built-in overlays are registered in canonical display order.
	wantIDs := []string{
		editor.OverlayCollisionShapes,
		editor.OverlayTriggers,
		editor.OverlayConnections,
		editor.OverlayPatrolPaths,
		editor.OverlayNavigation,
		editor.OverlayCameraBounds,
		editor.OverlayLabels,
	}
	ovs := ed.ViewportOverlays()
	gotIDs := make([]string, len(ovs))
	for i, o := range ovs {
		gotIDs[i] = o.ID
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("ViewportOverlays IDs = %v, want %v", gotIDs, wantIDs)
	}

	// Verify live scene counts for every built-in overlay.
	wantCounts := map[string]int{
		editor.OverlayCollisionShapes: 2, // knight.body.shape + slime_01.body.shape
		editor.OverlayTriggers:        2, // lever_01.trigger.zone + door_01.trigger.zone
		editor.OverlayConnections:     1, // lever_01.pulled -> door_01.open
		editor.OverlayPatrolPaths:     2, // slime_01 from/to + slime_02 waypoints
		editor.OverlayNavigation:      1, // nav_floor
		editor.OverlayCameraBounds:    1, // 1280x720 bounds
		editor.OverlayLabels:          6, // 6 visible objects
	}
	for id, want := range wantCounts {
		if got := ed.ViewportOverlayCount(id); got != want {
			t.Errorf("ViewportOverlayCount(%q) = %d, want %d", id, got, want)
		}
	}

	// Default active overlays: camera_bounds, collision_shapes, navigation, triggers (count 4).
	if got := ed.ActiveViewportOverlays(); !slices.Equal(got, editor.DefaultViewportOverlays()) {
		t.Fatalf("default ActiveViewportOverlays = %v, want %v", got, editor.DefaultViewportOverlays())
	}
	if ed.ActiveViewportOverlayCount() != 4 {
		t.Fatalf("ActiveViewportOverlayCount = %d, want 4", ed.ActiveViewportOverlayCount())
	}

	// Connections is off by default: no "pulled → open" arrow until turned on.
	var before core.Drawing
	ed.DrawViewport(&before, core.Size{Width: 640, Height: 360})
	if hasOverlayFillText(before.Ops, "pulled → open") || hasCubicOp(before.Ops) {
		t.Fatal("connections arrow drew while connections overlay was off")
	}

	// Turning on Connections draws the curved arrow and "pulled → open" label.
	if err := ed.SetViewportOverlay(editor.OverlayConnections, true); err != nil {
		t.Fatalf("SetViewportOverlay(connections, true): %v", err)
	}
	var after core.Drawing
	ed.DrawViewport(&after, core.Size{Width: 640, Height: 360})
	if !hasOverlayFillText(after.Ops, "pulled → open") {
		t.Fatal("turning on connections did not draw \"pulled → open\" label")
	}
	if !hasCubicOp(after.Ops) {
		t.Fatal("turning on connections did not draw curved Bezier arrow")
	}

	// Each individual overlay adds ops relative to the empty overlay set.
	baseOps := drawOpsWithOverlays(t, ed, []string{})
	for _, id := range wantIDs {
		ops := drawOpsWithOverlays(t, ed, []string{id})
		if len(ops) <= len(baseOps) {
			t.Errorf("overlay %q produced %d ops, expected more than base %d ops", id, len(ops), len(baseOps))
		}
	}

	// Verify specific text captions for camera_bounds and labels.
	camOps := drawOpsWithOverlays(t, ed, []string{editor.OverlayCameraBounds})
	if !hasOverlayFillText(camOps, "bounds 1280 × 720") {
		t.Fatal("camera_bounds overlay did not draw \"bounds 1280 × 720\" caption")
	}
	labelOps := drawOpsWithOverlays(t, ed, []string{editor.OverlayLabels})
	for _, name := range []string{"Knight", "lever_01", "door_01", "slime_01"} {
		if !hasOverlayFillText(labelOps, name) {
			t.Errorf("labels overlay did not draw label %q", name)
		}
	}

	// Unknown overlay ID is refused without mutating editor state.
	var unused core.Drawing
	if err := ed.DrawViewportWithOverlays(&unused, core.Size{Width: 640, Height: 360}, []string{"does_not_exist"}); err == nil {
		t.Fatal("expected error for unknown overlay ID in DrawViewportWithOverlays")
	}
	if !ed.ViewportOverlayEnabled(editor.OverlayConnections) {
		t.Fatal("DrawViewportWithOverlays mutated editor's persisted overlay state")
	}
}

func TestViewportOverlaysPluginAndCustomRegistration(t *testing.T) {
	dir := writePluginProject(t, "ai_debug")
	ed := newPluginEditor(t, dir)
	ed.SetCanvasSize(320, 180)

	drawnCount := 0
	if err := ed.RegisterPlugin(editor.Plugin{
		Name: "ai_debug",
		Overlays: []editor.ViewportOverlay{
			{
				ID:     "sight_cones",
				Name:   "Sight cones",
				Color:  core.RGB(0x4FD38A),
				Swatch: editor.OverlaySwatchDashedLine,
				Count:  func(*editor.Editor) int { return 4 },
				Draw: func(ed *editor.Editor, d *core.Drawing, _ *sceneformat.BuiltScene) {
					drawnCount++
					d.SetStrokeColor(core.RGB(0x4FD38A)).BeginPath().MoveTo(10, 10).LineTo(80, 40).Stroke()
				},
			},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin with overlay: %v", err)
	}

	if ed.ViewportOverlayCount("sight_cones") != 4 {
		t.Fatalf("plugin overlay count = %d, want 4", ed.ViewportOverlayCount("sight_cones"))
	}
	if err := ed.SetViewportOverlay("sight_cones", true); err != nil {
		t.Fatalf("SetViewportOverlay(sight_cones, true): %v", err)
	}
	var d core.Drawing
	ed.DrawViewport(&d, core.Size{Width: 320, Height: 180})
	if drawnCount != 1 {
		t.Fatalf("plugin overlay Draw called %d times, want 1", drawnCount)
	}

	// Direct RegisterViewportOverlay works and rejects duplicates or invalid IDs.
	directDrawn := 0
	if err := ed.RegisterViewportOverlay(editor.ViewportOverlay{
		ID:   "spawn_zones",
		Name: "Spawn zones",
		Draw: func(_ *editor.Editor, d *core.Drawing, _ *sceneformat.BuiltScene) {
			directDrawn++
			d.SetStrokeColor(core.RGB(0x3B7BFF)).StrokeRect(20, 20, 40, 40)
		},
	}); err != nil {
		t.Fatalf("RegisterViewportOverlay: %v", err)
	}
	if err := ed.RegisterViewportOverlay(editor.ViewportOverlay{
		ID:   "spawn_zones",
		Name: "Duplicate",
		Draw: func(*editor.Editor, *core.Drawing, *sceneformat.BuiltScene) {},
	}); err == nil {
		t.Fatal("expected duplicate overlay registration to fail")
	}
	if err := ed.RegisterViewportOverlay(editor.ViewportOverlay{
		ID:   "bad id",
		Name: "Has space",
		Draw: func(*editor.Editor, *core.Drawing, *sceneformat.BuiltScene) {},
	}); err == nil {
		t.Fatal("expected overlay ID with spaces to fail")
	}
	if err := ed.DrawViewportWithOverlays(&core.Drawing{}, core.Size{Width: 320, Height: 180}, []string{"spawn_zones"}); err != nil {
		t.Fatalf("DrawViewportWithOverlays(spawn_zones): %v", err)
	}
	if directDrawn != 1 {
		t.Fatalf("direct custom overlay Draw called %d times, want 1", directDrawn)
	}
}

func TestViewportOverlaysPersistPerUserOutsideProject(t *testing.T) {
	ed, projDir := openTestProject(t)
	settingsPath := filepath.Join(t.TempDir(), "user-config", "editor.yaml")

	gameYAMLPath := filepath.Join(projDir, "game.yaml")
	sceneYAMLPath := filepath.Join(projDir, "scenes", "room.yaml")
	gameBefore, err := os.ReadFile(gameYAMLPath)
	if err != nil {
		t.Fatalf("read game.yaml: %v", err)
	}
	sceneBefore, err := os.ReadFile(sceneYAMLPath)
	if err != nil {
		t.Fatalf("read room.yaml: %v", err)
	}

	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if err := ed.SetViewportOverlay(editor.OverlayConnections, true); err != nil {
		t.Fatalf("enable connections: %v", err)
	}
	if err := ed.SetViewportOverlay(editor.OverlayLabels, true); err != nil {
		t.Fatalf("enable labels: %v", err)
	}
	if err := ed.SetViewportOverlay(editor.OverlayNavigation, false); err != nil {
		t.Fatalf("disable navigation: %v", err)
	}

	// Project files must remain completely untouched and scene/project must remain clean.
	if ed.HasUnsavedChanges() {
		t.Fatal("toggling viewport overlays marked scene or project dirty")
	}
	gameAfter, _ := os.ReadFile(gameYAMLPath)
	sceneAfter, _ := os.ReadFile(sceneYAMLPath)
	if !bytes.Equal(gameBefore, gameAfter) {
		t.Fatal("toggling viewport overlays modified game.yaml")
	}
	if !bytes.Equal(sceneBefore, sceneAfter) {
		t.Fatal("toggling viewport overlays modified scene file")
	}

	// Settings YAML on disk must round-trip cleanly and survive a simulated restart.
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read persisted settings: %v", err)
	}
	if !strings.Contains(string(raw), "viewportOverlays:") {
		t.Fatalf("persisted settings missing viewportOverlays:\n%s", raw)
	}

	restarted, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New (restarted): %v", err)
	}
	if err := restarted.LoadSettings(settingsPath); err != nil {
		t.Fatalf("restarted.LoadSettings: %v", err)
	}
	want := []string{
		editor.OverlayCameraBounds,
		editor.OverlayCollisionShapes,
		editor.OverlayConnections,
		editor.OverlayLabels,
		editor.OverlayTriggers,
	}
	if got := restarted.ActiveViewportOverlays(); !slices.Equal(got, want) {
		t.Fatalf("restarted ActiveViewportOverlays = %v, want %v", got, want)
	}
	if restarted.ShowNavMesh2D {
		t.Fatal("restarted.ShowNavMesh2D = true, want false after disabling navigation overlay")
	}

	// Disabling all overlays writes an explicit empty list [] and survives restart without reverting to defaults.
	if err := restarted.SetViewportOverlays([]string{}); err != nil {
		t.Fatalf("SetViewportOverlays([]): %v", err)
	}
	emptyRaw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read empty settings: %v", err)
	}
	if !strings.Contains(string(emptyRaw), "viewportOverlays: []") {
		t.Fatalf("expected explicit viewportOverlays: [] in settings YAML, got:\n%s", emptyRaw)
	}
	restartedEmpty, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New (restartedEmpty): %v", err)
	}
	if err := restartedEmpty.LoadSettings(settingsPath); err != nil {
		t.Fatalf("restartedEmpty.LoadSettings: %v", err)
	}
	if n := restartedEmpty.ActiveViewportOverlayCount(); n != 0 {
		t.Fatalf("restartedEmpty ActiveViewportOverlayCount = %d, want 0", n)
	}
}

func TestViewportOverlaysPopoverUIAndScreenshot(t *testing.T) {
	ed := newOverlaysTestEditor(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	ed.SelectObject("slime_01")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// Verify the Overlays control lives in the viewport bar and keeps editor-toolbar compact at 1440x900 (<= 2 rows).
	tb := app.Find("id:editor-toolbar")
	if tb.Bounds().Size.Height > 104 {
		t.Fatalf("2D editor-toolbar height = %v at 1440x900, want compact <= 104pt (<= 2 rows)", tb.Bounds().Size.Height)
	}

	// Overlays button starts showing active count (4).
	if !app.Exists("Overlays 4") {
		t.Fatalf("expected Overlays button to show %q", "Overlays 4")
	}
	app.Screenshot("viewport-overlays-before")

	// Open the Overlays popover.
	app.Press("id:viewport-overlays-button")
	app.Settle()
	if !ed.OverlaysPopoverOpen() || !app.Exists("id:viewport-overlays-popover") {
		t.Fatal("pressing Overlays button did not open SHOW IN VIEWPORT popover")
	}
	if got := app.Find("id:viewport-overlays-title").Text; got != "SHOW IN VIEWPORT" {
		t.Fatalf("popover title = %q, want SHOW IN VIEWPORT", got)
	}
	if !app.Exists("id:viewport-overlays-footer") {
		t.Fatal("popover missing agent screenshot footer note")
	}
	if got := app.Find("id:viewport-overlay-count-collision_shapes").Text; got != "2 shapes" {
		t.Fatalf("collision_shapes count = %q, want %q", got, "2 shapes")
	}
	if got := app.Find("id:viewport-overlay-count-connections").Text; got != "1" {
		t.Fatalf("connections count = %q, want %q", got, "1")
	}

	// Turn on Connections and Patrol paths via the popover switches.
	app.Press("id:viewport-overlay-switch-connections")
	app.Settle()
	app.Press("id:viewport-overlay-switch-patrol_paths")
	app.Settle()

	if !ed.ViewportOverlayEnabled(editor.OverlayConnections) || !ed.ViewportOverlayEnabled(editor.OverlayPatrolPaths) {
		t.Fatal("popover switches did not enable connections and patrol_paths")
	}
	if !app.Exists("Overlays 6") {
		t.Fatalf("expected Overlays button to show %q after enabling 2 overlays", "Overlays 6")
	}

	// Verify the live viewport canvas now draws the "pulled → open" connection arrow.
	vp := app.Find("id:editor-viewport")
	if !hasOverlayFillText(vp.Ops, "pulled → open") || !hasCubicOp(vp.Ops) {
		t.Fatal("live 2D viewport canvas missing \"pulled → open\" connection arrow after toggling switch")
	}

	// Capture real nsgotest screenshot on macOS showing the open Overlays popover and drawn overlays.
	app.Screenshot("viewport-overlays-popover")
}
