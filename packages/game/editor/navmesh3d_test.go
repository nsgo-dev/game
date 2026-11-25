package editor_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/navbake3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const navMeshTestProjectYAML = `version: 1
name: NavMesh 3D Game
id: dev.nsgo.nav3d
appVersion: 0.1.0
window:
  width: 640
  height: 360
autoload:
  mainScene: arena
  scenes:
    - name: arena
      path: scenes/arena.scene.yaml
`

func rampSecondFloorSceneDoc() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Ramp Level",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:     "ground",
						X:      7,
						Y:      -0.25,
						PosZ:   6,
						ScaleX: sceneformat.Float32(14),
						ScaleY: sceneformat.Float32(0.5),
						ScaleZ: sceneformat.Float32(12),
						Mesh:   sceneformat.MeshBox,
					},
					{
						ID:     "second_floor",
						X:      13,
						Y:      1.75,
						PosZ:   6,
						ScaleX: sceneformat.Float32(8),
						ScaleY: sceneformat.Float32(0.5),
						ScaleZ: sceneformat.Float32(12),
						Mesh:   sceneformat.MeshBox,
					},
					{
						ID:     "ramp",
						X:      6.5,
						Y:      0.85,
						PosZ:   4.0,
						RotZ:   0.3805064, // atan2(2, 5) ≈ 21.8°
						ScaleX: sceneformat.Float32(6.2),
						ScaleY: sceneformat.Float32(0.3),
						ScaleZ: sceneformat.Float32(4.0),
						Mesh:   sceneformat.MeshBox,
					},
					{
						ID:   "wall",
						X:    3.5,
						Y:    1.5,
						PosZ: 8.25,
						Components: map[string]any{
							sceneformat.Body3DComponentName: map[string]any{
								"version": 1,
								"kind":    "static",
							},
							sceneformat.Collider3DComponentName: map[string]any{
								"version": 1,
								"shape":   "box",
								"width":   1.0,
								"height":  3.0,
								"depth":   2.5,
							},
						},
					},
				},
			},
		},
	}
}

func setupNavMeshProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(navMeshTestProjectYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	sceneYAML, err := sceneformat.Encode(rampSecondFloorSceneDoc())
	if err != nil {
		t.Fatalf("sceneformat.Encode: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "arena.scene.yaml"), sceneYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEditorBakeNavMesh3DRampSecondFloorRoundTripAndUndo(t *testing.T) {
	dir := setupNavMeshProjectDir(t)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	cfg := editor.NavMeshBakeConfig{
		AgentName:  "humanoid",
		Radius:     0.5,
		Height:     1.8,
		MaxClimb:   0.4,
		MaxSlope:   45,
		CellSize:   0.2,
		CellHeight: 0.1,
		OutputPath: "levels/arena.navmesh.yaml",
	}
	ed.SetNavMeshBakeConfig(cfg)

	file, err := ed.BakeNavMesh3D()
	if err != nil {
		t.Fatalf("BakeNavMesh3D: %v", err)
	}
	if file == nil || len(file.Meshes) == 0 || len(file.Meshes[0].Polygons) == 0 {
		t.Fatalf("expected non-empty baked navmesh, got %+v", file)
	}
	if ed.Doc.NavMesh != "levels/arena.navmesh.yaml" {
		t.Fatalf("Doc.NavMesh = %q, want levels/arena.navmesh.yaml", ed.Doc.NavMesh)
	}

	// File written on disk is deterministic and round-trips byte for byte.
	diskPath := filepath.Join(dir, "levels", "arena.navmesh.yaml")
	raw1, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", diskPath, err)
	}
	parsed, err := navbake3d.Parse(raw1)
	if err != nil {
		t.Fatalf("navbake3d.Parse: %v", err)
	}
	reencoded, err := navbake3d.Encode(parsed)
	if err != nil {
		t.Fatalf("navbake3d.Encode: %v", err)
	}
	if !bytes.Equal(raw1, reencoded) {
		t.Fatal("written .navmesh.yaml did not round-trip byte for byte")
	}

	// Baking a second time produces identical bytes on disk.
	if _, err := ed.BakeNavMesh3D(); err != nil {
		t.Fatalf("second BakeNavMesh3D: %v", err)
	}
	raw2, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("ReadFile(2): %v", err)
	}
	if !bytes.Equal(raw1, raw2) {
		t.Fatal("baking twice wrote different bytes")
	}

	// FindPath climbs the ramp to the second floor and keeps agent radius off the wall.
	mesh, ok := parsed.Mesh("humanoid")
	if !ok {
		t.Fatal("baked file missing humanoid mesh")
	}
	nm := mesh.NavMesh()
	path, ok := nm.FindPath(math3d.Vec3{X: 1.5, Y: 0, Z: 4.0}, math3d.Vec3{X: 13.5, Y: 2, Z: 4.0})
	if !ok {
		t.Fatal("FindPath failed from ground floor up ramp to second floor")
	}
	if end := path[len(path)-1]; end.Y < 1.8 || end.Y > 2.25 {
		t.Fatalf("path ends at Y=%v, want ~2.0", end.Y)
	}
	wallPath, ok := nm.FindPath(math3d.Vec3{X: 1.5, Y: 0, Z: 8.25}, math3d.Vec3{X: 5.5, Y: 0, Z: 8.25})
	if !ok {
		t.Fatal("FindPath failed around wall")
	}
	for _, p := range wallPath {
		dx := max(3.0-p.X, 0, p.X-4.0)
		dz := max(7.0-p.Z, 0, p.Z-9.5)
		dist := float32(math.Hypot(float64(dx), float64(dz)))
		if dist < cfg.Radius-cfg.CellSize {
			t.Fatalf("path point %v is %v from wall, closer than radius %v (cell %v)", p, dist, cfg.Radius, cfg.CellSize)
		}
	}

	// Linking SceneDoc.NavMesh is undoable and redoable.
	if !ed.Undo() {
		t.Fatal("Undo after BakeNavMesh3D returned false")
	}
	if ed.Doc.NavMesh != "" {
		t.Fatalf("Doc.NavMesh after Undo = %q, want empty", ed.Doc.NavMesh)
	}
	if !ed.Redo() {
		t.Fatal("Redo returned false")
	}
	if ed.Doc.NavMesh != "levels/arena.navmesh.yaml" {
		t.Fatalf("Doc.NavMesh after Redo = %q, want levels/arena.navmesh.yaml", ed.Doc.NavMesh)
	}

	// Saving and reopening the project autoloads the baked .navmesh.yaml for preview.
	if err := ed.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ed2, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New(2): %v", err)
	}
	if err := ed2.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject(2): %v", err)
	}
	if ed2.NavMesh3DFile() == nil || ed2.NavMesh3DMesh() == nil || len(ed2.NavMesh3DMesh().Polygons) == 0 {
		t.Fatal("expected reopening project to autoload the scene's baked .navmesh.yaml for preview")
	}
}

func TestEditorNavMesh3DUIPanelBakeAndViewportOverlay(t *testing.T) {
	dir := setupNavMeshProjectDir(t)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	// Position the 3D camera so it frames the ramp and both floors clearly.
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{7, 1, 5},
		Yaw:      0.6,
		Pitch:    0.55,
		Distance: 22,
		FOV:      float32(math.Pi / 3),
		Near:     0.2,
		Far:      500,
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	// Switch to 3D viewport mode.
	app.Press("id:view-mode-3d")
	if ed.ViewMode != editor.ViewMode3D {
		t.Fatalf("ViewMode = %v, want ViewMode3D", ed.ViewMode)
	}

	// Open the 3D NavMesh bake panel via the 3D toolbar button.
	if app.Exists("id:navmesh-panel") {
		t.Fatal("navmesh-panel should start closed")
	}
	app.Press("id:toolbar-navmesh-panel")
	if !ed.NavMeshPanelOpen() || !app.Exists("id:navmesh-panel") {
		t.Fatal("expected navmesh-panel to open after pressing toolbar-navmesh-panel")
	}

	// Verify all bake setting controls are mounted.
	for _, id := range []string{
		"id:navmesh-agent-name",
		"id:navmesh-agent-radius",
		"id:navmesh-agent-height",
		"id:navmesh-agent-max-climb",
		"id:navmesh-agent-max-slope",
		"id:navmesh-cell-size",
		"id:navmesh-cell-height",
		"id:navmesh-output-path",
		"id:navmesh-overlay-toggle",
		"id:navmesh-bake",
	} {
		app.Find(id)
	}

	// Edit bake settings through the UI controls.
	app.Clear("id:navmesh-agent-name")
	app.Type("id:navmesh-agent-name", "runner")
	app.Clear("id:navmesh-agent-radius")
	app.Type("id:navmesh-agent-radius", "0.45")
	app.Submit("id:navmesh-agent-radius")
	app.Clear("id:navmesh-agent-height")
	app.Type("id:navmesh-agent-height", "1.8")
	app.Submit("id:navmesh-agent-height")
	app.Clear("id:navmesh-agent-max-climb")
	app.Type("id:navmesh-agent-max-climb", "0.4")
	app.Submit("id:navmesh-agent-max-climb")
	app.Clear("id:navmesh-agent-max-slope")
	app.Type("id:navmesh-agent-max-slope", "45")
	app.Submit("id:navmesh-agent-max-slope")
	app.Clear("id:navmesh-cell-size")
	app.Type("id:navmesh-cell-size", "0.25")
	app.Submit("id:navmesh-cell-size")
	app.Clear("id:navmesh-cell-height")
	app.Type("id:navmesh-cell-height", "0.1")
	app.Submit("id:navmesh-cell-height")
	app.Clear("id:navmesh-output-path")
	app.Type("id:navmesh-output-path", "levels/ui_baked.navmesh.yaml")

	countOps := func() (fills, strokes, lines int) {
		ops := app.Find("id:editor-viewport").Ops
		for _, op := range ops {
			for _, f := range []float32{op.X, op.Y, op.X1, op.Y1, op.X2, op.Y2, op.W, op.H} {
				if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
					t.Fatalf("non-finite draw coordinate in op %+v", op)
				}
			}
			switch op.Kind {
			case core.DrawFill:
				fills++
			case core.DrawStroke:
				strokes++
			case core.DrawLineTo:
				lines++
			}
		}
		return fills, strokes, lines
	}

	// Capture baseline 3D viewport draw counts before baking.
	baseFills, baseStrokes, baseLines := countOps()

	// Press Bake in the NavMesh panel!
	app.Press("id:navmesh-bake")
	app.Settle()

	if ed.NavMesh3DError() != "" {
		t.Fatalf("unexpected navmesh bake error: %s", ed.NavMesh3DError())
	}
	if ed.Doc.NavMesh != "levels/ui_baked.navmesh.yaml" {
		t.Fatalf("Doc.NavMesh = %q, want levels/ui_baked.navmesh.yaml", ed.Doc.NavMesh)
	}
	summary := app.Find("id:navmesh-summary").Text
	if !strings.Contains(summary, "runner:") || !strings.Contains(summary, "polygons") {
		t.Fatalf("unexpected navmesh-summary = %q", summary)
	}

	// Verify the written file round-trips byte for byte.
	diskData, err := os.ReadFile(filepath.Join(dir, "levels", "ui_baked.navmesh.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(ui_baked.navmesh.yaml): %v", err)
	}
	f, err := navbake3d.Parse(diskData)
	if err != nil {
		t.Fatalf("Parse(ui_baked.navmesh.yaml): %v", err)
	}
	again, err := navbake3d.Encode(f)
	if err != nil {
		t.Fatalf("Encode(ui_baked.navmesh.yaml): %v", err)
	}
	if !bytes.Equal(diskData, again) {
		t.Fatal("UI-baked .navmesh.yaml did not round-trip byte for byte")
	}

	// With overlay ON (default), the 3D viewport must draw polygon fills + outline strokes!
	withFills, withStrokes, withLines := countOps()
	if withFills <= baseFills || withStrokes <= baseStrokes || withLines <= baseLines {
		t.Fatalf("expected navmesh overlay to emit fills/strokes/lines: before=(%d,%d,%d) after=(%d,%d,%d)",
			baseFills, baseStrokes, baseLines, withFills, withStrokes, withLines)
	}

	// Toggle the 3D navmesh overlay OFF via toolbar button and confirm overlay draw ops disappear.
	app.Press("id:toolbar-navmesh-overlay")
	app.Settle()
	if ed.ShowNavMesh3D {
		t.Fatal("expected ShowNavMesh3D false after toggling toolbar-navmesh-overlay")
	}
	offFills, offStrokes, offLines := countOps()
	if offFills != baseFills || offStrokes != baseStrokes || offLines != baseLines {
		t.Fatalf("overlay OFF draw counts = (%d,%d,%d), want baseline (%d,%d,%d)",
			offFills, offStrokes, offLines, baseFills, baseStrokes, baseLines)
	}

	// Toggle overlay back ON via the panel's switch and confirm overlay draws again.
	app.Press("id:navmesh-overlay-toggle")
	app.Settle()
	if !ed.ShowNavMesh3D {
		t.Fatal("expected ShowNavMesh3D true after toggling navmesh-overlay-toggle")
	}
	onFills, onStrokes, onLines := countOps()
	if onFills != withFills || onStrokes != withStrokes || onLines != withLines {
		t.Fatalf("overlay back ON draw counts = (%d,%d,%d), want (%d,%d,%d)",
			onFills, onStrokes, onLines, withFills, withStrokes, withLines)
	}
}

func TestEditorNavMesh3DExtraGeometryAndErrors(t *testing.T) {
	ed := newTest3DEditor(t)

	// Empty scene fails cleanly with ErrNoGeometry and records NavMesh3DError.
	if _, err := ed.BakeNavMesh3D(); !errors.Is(err, navbake3d.ErrNoGeometry) {
		t.Fatalf("BakeNavMesh3D on empty scene = %v, want ErrNoGeometry", err)
	}
	if ed.NavMesh3DError() == "" {
		t.Fatal("expected NavMesh3DError to be populated after failed bake")
	}

	// OnBakeNavMesh3DGeometry lets a game supply terrain heightmaps or GridMaps.
	ed.OnBakeNavMesh3DGeometry = func(g *navbake3d.Geometry) error {
		const cols, rows = 9, 9
		hm := &world3d.Heightmap{
			Cols:        cols,
			Rows:        rows,
			CellSize:    1,
			HeightScale: 1,
			Heights:     make([]float32, cols*rows),
		}
		g.AddHeightmap(hm, 1)
		return nil
	}
	f, err := ed.BakeNavMesh3D()
	if err != nil {
		t.Fatalf("BakeNavMesh3D with OnBakeNavMesh3DGeometry: %v", err)
	}
	if ed.NavMesh3DError() != "" {
		t.Fatalf("expected NavMesh3DError cleared, got %q", ed.NavMesh3DError())
	}
	if m := ed.NavMesh3DMesh(); m == nil || len(m.Polygons) == 0 || len(f.Meshes) != 1 {
		t.Fatalf("expected baked terrain navmesh polygons, got %+v", f)
	}

	// Refused while playing.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ed.AttachScope(cx)
		return core.View{}
	})
	var playErr error
	app.Post(func() { playErr = ed.Play() })
	app.Settle()
	if playErr != nil {
		t.Fatalf("Play: %v", playErr)
	}
	defer func() {
		app.Post(func() { _ = ed.Stop() })
		app.Settle()
	}()
	if _, err := ed.BakeNavMesh3D(); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("BakeNavMesh3D while playing = %v, want ErrPlaying", err)
	}
	if err := ed.SetSceneNavMesh("levels/blocked.navmesh.yaml"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SetSceneNavMesh while playing = %v, want ErrPlaying", err)
	}
}
