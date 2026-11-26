package editor_test

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newTerrainTestEditor(t *testing.T) *editor.Editor {
	t.Helper()
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	err = ed.NewTerrain(editor.NewTerrainOptions{
		Name:        "Valley",
		Cols:        17,
		Rows:        17,
		CellSize:    2,
		HeightScale: 40,
		Layers: []world3d.TerrainLayerDoc{
			{Name: "grass", Color: &[3]float32{0.2, 0.6, 0.2}},
			{Name: "rock", Color: &[3]float32{0.5, 0.5, 0.5}},
			{Name: "snow", Color: &[3]float32{0.95, 0.95, 0.95}},
		},
		Foliage: []world3d.FoliageLayerDoc{
			{Name: "fern", Mesh: "models/fern.glb", Density: 1.5, Seed: 7},
		},
	})
	if err != nil {
		t.Fatalf("NewTerrain: %v", err)
	}
	return ed
}

func TestTerrainSculptBrushesFalloffAndSingleUndoStep(t *testing.T) {
	ed := newTerrainTestEditor(t)
	hm := ed.Terrain()

	// 1. Raise stroke with Smooth falloff across multiple dabs -> exactly ONE undo step.
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:     editor.TerrainBrushRaise,
		Radius:   6,
		Strength: 0.8,
		Falloff:  editor.TerrainFalloffSmooth,
	})
	if !ed.BeginTerrainStroke(16, 16) {
		t.Fatalf("BeginTerrainStroke(raise) returned false")
	}
	ed.ContinueTerrainStroke(18, 16)
	ed.ContinueTerrainStroke(20, 16)
	if !ed.EndTerrainStroke() {
		t.Fatalf("EndTerrainStroke(raise) returned false")
	}
	if ed.History.Len() != 1 || ed.History.Index() != 1 {
		t.Fatalf("History after multi-dab raise stroke = (len=%d, idx=%d), want (1, 1)", ed.History.Len(), ed.History.Index())
	}
	centerY, ok := hm.HeightAt(16, 16)
	if !ok || centerY <= 0 {
		t.Fatalf("center height after raise = (%v, %v), want > 0", centerY, ok)
	}
	edgeY, _ := hm.HeightAt(12, 16)
	if edgeY >= centerY {
		t.Fatalf("smooth falloff edge (%v) should be lower than center (%v)", edgeY, centerY)
	}
	farY, _ := hm.HeightAt(0, 0)
	if farY != 0 {
		t.Fatalf("sample outside radius changed to %v, want 0", farY)
	}

	// Undo restores exact flat ground; Redo restores exact sculpted heights.
	raisedSnap := append([]float32(nil), hm.Heights...)
	if !ed.Undo() {
		t.Fatalf("Undo returned false")
	}
	for i, h := range hm.Heights {
		if h != 0 {
			t.Fatalf("height[%d] after Undo = %v, want 0", i, h)
		}
	}
	if !ed.Redo() {
		t.Fatalf("Redo returned false")
	}
	for i, h := range hm.Heights {
		if h != raisedSnap[i] {
			t.Fatalf("height[%d] after Redo = %v, want %v", i, h, raisedSnap[i])
		}
	}

	// 2. Lower brush decreases height in one undo step.
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:     editor.TerrainBrushLower,
		Radius:   6,
		Strength: 0.5,
		Falloff:  editor.TerrainFalloffLinear,
	})
	beforeLower := hm.SampleHeight(8, 8)
	if !ed.BeginTerrainStroke(16, 16) || !ed.EndTerrainStroke() {
		t.Fatalf("lower stroke failed")
	}
	if got := hm.SampleHeight(8, 8); got >= beforeLower {
		t.Fatalf("height after Lower = %v, want < %v", got, beforeLower)
	}
	if ed.History.Len() != 2 {
		t.Fatalf("History.Len() = %d, want 2", ed.History.Len())
	}

	// 3. Smooth brush reduces a sharp spike and lifts its immediate neighbors.
	hm.Heights[4*hm.Cols+4] = 0.8 // sharp spike at (8, 8)
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:     editor.TerrainBrushSmooth,
		Radius:   4,
		Strength: 1.0,
		Falloff:  editor.TerrainFalloffFlat,
	})
	if !ed.BeginTerrainStroke(8, 8) || !ed.EndTerrainStroke() {
		t.Fatalf("smooth stroke failed")
	}
	if hm.Heights[4*hm.Cols+4] >= 0.8 {
		t.Fatalf("spike was not smoothed down: %v", hm.Heights[4*hm.Cols+4])
	}
	if hm.Heights[4*hm.Cols+5] <= 0 {
		t.Fatalf("neighbor of spike was not lifted by smoothing: %v", hm.Heights[4*hm.Cols+5])
	}

	// 4. SetHeight brush drives heights toward TargetHeight (world Y = 20 -> normalized 0.5).
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:         editor.TerrainBrushSetHeight,
		Radius:       4,
		Strength:     1.0,
		Falloff:      editor.TerrainFalloffFlat,
		TargetHeight: 20,
	})
	if !ed.BeginTerrainStroke(4, 28) || !ed.EndTerrainStroke() {
		t.Fatalf("setHeight stroke failed")
	}
	if got, _ := hm.HeightAt(4, 28); !math3d.ApproxEqual(got, 20, 1e-4) {
		t.Fatalf("HeightAt after SetHeight(20) = %v, want 20", got)
	}

	// 5. Flatten brush samples height at stroke start (20) and pulls adjacent lower cells up to 20.
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:     editor.TerrainBrushFlatten,
		Radius:   6,
		Strength: 1.0,
		Falloff:  editor.TerrainFalloffFlat,
	})
	if !ed.BeginTerrainStroke(4, 28) {
		t.Fatalf("BeginTerrainStroke(flatten) failed")
	}
	ed.ContinueTerrainStroke(10, 28)
	if !ed.EndTerrainStroke() {
		t.Fatalf("EndTerrainStroke(flatten) failed")
	}
	if got, _ := hm.HeightAt(10, 28); !math3d.ApproxEqual(got, 20, 1e-4) {
		t.Fatalf("HeightAt after Flatten = %v, want 20", got)
	}

	// 6. CancelTerrainStroke restores pre-stroke heights and adds no history step.
	histBefore := ed.History.Len()
	snapBefore := append([]float32(nil), hm.Heights...)
	ed.SetTerrainBrush(editor.TerrainBrush{Mode: editor.TerrainBrushRaise, Radius: 6, Strength: 1.0})
	if !ed.BeginTerrainStroke(28, 28) {
		t.Fatalf("BeginTerrainStroke before cancel failed")
	}
	ed.CancelTerrainStroke()
	if ed.History.Len() != histBefore {
		t.Fatalf("CancelTerrainStroke recorded history step: %d vs %d", ed.History.Len(), histBefore)
	}
	for i := range hm.Heights {
		if hm.Heights[i] != snapBefore[i] {
			t.Fatalf("CancelTerrainStroke did not restore height[%d]", i)
		}
	}
}

func TestTerrainLayerAndFoliagePaintingUndoRedo(t *testing.T) {
	ed := newTerrainTestEditor(t)
	hm := ed.Terrain()

	// Paint material layer 1 ("rock") across a drag -> 1 undo step.
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:       editor.TerrainBrushPaintLayer,
		Radius:     6,
		Strength:   1.0,
		Falloff:    editor.TerrainFalloffFlat,
		LayerIndex: 1,
	})
	if !ed.BeginTerrainStroke(8, 8) {
		t.Fatalf("BeginTerrainStroke(paint layer) failed")
	}
	ed.ContinueTerrainStroke(12, 8)
	if !ed.EndTerrainStroke() {
		t.Fatalf("EndTerrainStroke(paint layer) failed")
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() = %d, want 1", ed.History.Len())
	}
	sample, ok := hm.LayerAt(10, 8)
	if !ok || sample.Name != "rock" || sample.Weight(1) < 0.99 {
		t.Fatalf("LayerAt(10,8) after paint = (%+v, %v), want rock ~1.0", sample, ok)
	}
	// Weights always remain normalized.
	sum := sample.Weight(0) + sample.Weight(1) + sample.Weight(2)
	if !math3d.ApproxEqual(sum, 1.0, 1e-4) {
		t.Fatalf("weight sum = %v, want 1.0", sum)
	}

	// Undo restores grass; Redo brings rock back.
	if !ed.Undo() {
		t.Fatalf("Undo layer paint failed")
	}
	sampleAfterUndo, _ := hm.LayerAt(10, 8)
	if sampleAfterUndo.Name != "grass" || sampleAfterUndo.Weight(0) < 0.99 {
		t.Fatalf("LayerAt after Undo = %+v, want grass", sampleAfterUndo)
	}
	if !ed.Redo() {
		t.Fatalf("Redo layer paint failed")
	}
	sampleAfterRedo, _ := hm.LayerAt(10, 8)
	if sampleAfterRedo.Name != "rock" || sampleAfterRedo.Weight(1) < 0.99 {
		t.Fatalf("LayerAt after Redo = %+v, want rock", sampleAfterRedo)
	}

	// Paint foliage density ("fern") -> 1 undo step, then erase with TargetDensity=0.
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:          editor.TerrainBrushPaintFoliage,
		Radius:        6,
		Strength:      1.0,
		Falloff:       editor.TerrainFalloffFlat,
		FoliageIndex:  0,
		TargetDensity: 1.0,
	})
	if !ed.BeginTerrainStroke(24, 24) {
		t.Fatalf("BeginTerrainStroke(foliage) failed")
	}
	ed.ContinueTerrainStroke(26, 24)
	if !ed.EndTerrainStroke() {
		t.Fatalf("EndTerrainStroke(foliage) failed")
	}
	if ed.History.Len() != 2 {
		t.Fatalf("History.Len() = %d, want 2", ed.History.Len())
	}
	dens, ok := hm.FoliageDensityAt("fern", 24, 24)
	if !ok || dens < 0.99 {
		t.Fatalf("FoliageDensityAt(24,24) = (%v, %v), want ~1.0", dens, ok)
	}
	// Undo restores zero foliage density.
	if !ed.Undo() {
		t.Fatalf("Undo foliage failed")
	}
	if d, _ := hm.FoliageDensityAt("fern", 24, 24); d != 0 {
		t.Fatalf("FoliageDensityAt after Undo = %v, want 0", d)
	}
	if !ed.Redo() {
		t.Fatalf("Redo foliage failed")
	}
	if d, _ := hm.FoliageDensityAt("fern", 24, 24); d < 0.99 {
		t.Fatalf("FoliageDensityAt after Redo = %v, want ~1.0", d)
	}
}

func TestTerrainEditorSaveMatchesGameLoadAndRender(t *testing.T) {
	dir := t.TempDir()
	// Create a minimal project structure with material resources and foliage mesh placeholder.
	if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: TerrainDemo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nid: dev.example.terraindemo\nname: TerrainDemo\nappVersion: 0.1.0\nwindow:\n  width: 1280\n  height: 720\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "materials", "grass.material.yaml"), []byte("type: material\nversion: 1\nname: Grass\nbaseColor: \"#33aa44\"\nroughness: 0.85\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "materials", "rock.material.yaml"), []byte("type: material\nversion: 1\nname: Rock\nbaseColor: \"#888888\"\nroughness: 0.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "models", "pine.glb"), []byte("glTF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(dir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	err = ed.NewTerrain(editor.NewTerrainOptions{
		Name:        "Alpine",
		Path:        "terrain/alpine.terrain.yaml",
		Cols:        33,
		Rows:        33,
		CellSize:    2,
		HeightScale: 50,
		Layers: []world3d.TerrainLayerDoc{
			{Name: "grass", Material: "materials/grass.material.yaml"},
			{Name: "rock", Material: "materials/rock.material.yaml"},
		},
		Foliage: []world3d.FoliageLayerDoc{
			{Name: "pine", Mesh: "models/pine.glb", Density: 0.5, Seed: 11},
		},
	})
	if err != nil {
		t.Fatalf("NewTerrain: %v", err)
	}
	if !ed.TerrainDirty() || !ed.HasUnsavedChanges() {
		t.Fatalf("new terrain should report dirty before first save")
	}

	// Sculpt a smooth hill at (32, 32) with sub-8-bit fine steps.
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:     editor.TerrainBrushRaise,
		Radius:   14,
		Strength: 0.25,
		Falloff:  editor.TerrainFalloffSmooth,
	})
	if !ed.BeginTerrainStroke(32, 32) || !ed.EndTerrainStroke() {
		t.Fatalf("sculpt stroke failed")
	}
	// Paint rock layer around (12, 12).
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:       editor.TerrainBrushPaintLayer,
		Radius:     10,
		Strength:   1.0,
		Falloff:    editor.TerrainFalloffFlat,
		LayerIndex: 1,
	})
	if !ed.BeginTerrainStroke(12, 12) || !ed.EndTerrainStroke() {
		t.Fatalf("layer paint stroke failed")
	}
	// Paint pine foliage density around (48, 48).
	ed.SetTerrainBrush(editor.TerrainBrush{
		Mode:          editor.TerrainBrushPaintFoliage,
		Radius:        10,
		Strength:      1.0,
		Falloff:       editor.TerrainFalloffFlat,
		FoliageIndex:  0,
		TargetDensity: 1.0,
	})
	if !ed.BeginTerrainStroke(48, 48) || !ed.EndTerrainStroke() {
		t.Fatalf("foliage paint stroke failed")
	}

	// Save writes 16-bit PNG heightmap, per-layer weight PNGs, foliage density PNG, and .terrain.yaml.
	if err := ed.SaveTerrain(); err != nil {
		t.Fatalf("SaveTerrain: %v", err)
	}
	if ed.TerrainDirty() {
		t.Fatalf("TerrainDirty() = true after SaveTerrain")
	}

	// Verify heightmap PNG on disk is 16-bit grayscale (*image.Gray16).
	hmFile, err := os.Open(filepath.Join(dir, "terrain", "alpine_height.png"))
	if err != nil {
		t.Fatalf("open saved heightmap PNG: %v", err)
	}
	hmDecoded, err := png.Decode(hmFile)
	_ = hmFile.Close()
	if err != nil {
		t.Fatalf("png.Decode saved heightmap: %v", err)
	}
	if _, ok := hmDecoded.(*image.Gray16); !ok {
		t.Fatalf("saved heightmap PNG decoded as %T, want *image.Gray16", hmDecoded)
	}

	// Reload from disk exactly as a game does via world3d.LoadTerrainFS + render3d.TerrainMaterialFromDoc.
	fsys := os.DirFS(dir)
	gameHM, gameDoc, err := world3d.LoadTerrainFS(fsys, "terrain/alpine.terrain.yaml")
	if err != nil {
		t.Fatalf("world3d.LoadTerrainFS: %v", err)
	}

	editorHM := ed.Terrain()
	if gameHM.Cols != editorHM.Cols || gameHM.Rows != editorHM.Rows {
		t.Fatalf("game dims = %dx%d, want %dx%d", gameHM.Cols, gameHM.Rows, editorHM.Cols, editorHM.Rows)
	}
	// Every height sample matches within 16-bit quantization (<= 1/65535 * heightScale).
	maxErr := float64(0)
	for i := range editorHM.Heights {
		diff := math.Abs(float64(gameHM.Heights[i] - editorHM.Heights[i]))
		if diff > maxErr {
			maxErr = diff
		}
	}
	if maxErr > 1.0/65535.0+1e-6 {
		t.Fatalf("heightmap round-trip max normalized error = %v, exceeds 16-bit precision", maxErr)
	}

	// Layer weights match at painted rock spot and unpainted grass spot.
	rockSample, ok := gameHM.LayerAt(12, 12)
	if !ok || rockSample.Name != "rock" || rockSample.Weight(1) < 0.99 {
		t.Fatalf("game LayerAt(12,12) = (%+v, %v), want rock", rockSample, ok)
	}
	grassSample, ok := gameHM.LayerAt(52, 12)
	if !ok || grassSample.Name != "grass" || grassSample.Weight(0) < 0.99 {
		t.Fatalf("game LayerAt(52,12) = (%+v, %v), want grass", grassSample, ok)
	}

	// Foliage density matches and ScatterFoliage spawns trees where painted.
	pineDens, ok := gameHM.FoliageDensityAt("pine", 48, 48)
	if !ok || pineDens < 0.99 {
		t.Fatalf("game FoliageDensityAt(48,48) = (%v, %v), want ~1.0", pineDens, ok)
	}
	emptyDens, ok := gameHM.FoliageDensityAt("pine", 12, 12)
	if !ok || emptyDens > 0.01 {
		t.Fatalf("game FoliageDensityAt(12,12) = (%v, %v), want ~0.0", emptyDens, ok)
	}
	fields, err := gameDoc.ScatterFoliage(gameHM, gameHM.Bounds(), world3d.FoliageBuildOptions{
		ResolveMesh: func(string) (uint32, error) { return 1, nil },
	})
	if err != nil || len(fields) != 1 || fields[0].TotalCount() == 0 {
		t.Fatalf("game ScatterFoliage failed: len=%d err=%v", len(fields), err)
	}

	// render3d builds the blended terrain material directly from the saved YAML + weight PNGs.
	gameMat, err := render3d.TerrainMaterialFromDoc(fsys, gameDoc)
	if err != nil {
		t.Fatalf("render3d.TerrainMaterialFromDoc: %v", err)
	}
	if len(gameMat.Layers) != 2 || gameMat.SplatMap0 == nil {
		t.Fatalf("unexpected game TerrainMaterial: layers=%d splat0=%v", len(gameMat.Layers), gameMat.SplatMap0 != nil)
	}
	// Check SplatMap0 texel at the painted rock spot has G=255 (layer 1).
	nrgba, ok := gameMat.SplatMap0.(*image.NRGBA)
	if !ok {
		t.Fatalf("SplatMap0 type = %T, want *image.NRGBA", gameMat.SplatMap0)
	}
	px := nrgba.NRGBAAt(6, 6) // (12/64)*33 ~= 6
	if px.G < 250 {
		t.Fatalf("SplatMap0 at painted rock spot = %+v, want G~=255", px)
	}

	// assetcheck reports 0 problems on the editor-written project.
	report, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatalf("assetcheck.Check: %v", err)
	}
	if !report.OK() {
		t.Fatalf("assetcheck reported problems on saved terrain: %+v", report.Problems)
	}

	// Reopening the saved file in a fresh editor loads the exact same terrain back.
	ed2, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed2.OpenProject(filepath.Join(dir, "game.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := ed2.OpenTerrainProjectFile("terrain/alpine.terrain.yaml"); err != nil {
		t.Fatalf("OpenTerrainProjectFile: %v", err)
	}
	if ed2.TerrainDirty() {
		t.Fatalf("freshly opened terrain should not be dirty")
	}
	reopenedY, _ := ed2.Terrain().HeightAt(32, 32)
	origY, _ := editorHM.HeightAt(32, 32)
	if !math3d.ApproxEqual(reopenedY, origY, 0.01) {
		t.Fatalf("reopened center height = %v, want %v", reopenedY, origY)
	}
	_ = color.Gray{}
}

func TestTerrainPanelAndViewport3DInteraction(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.Camera3D.Target = [3]float32{16, 0, 16}
	ed.Camera3D.Yaw = 0
	ed.Camera3D.Pitch = 1.2
	ed.Camera3D.Distance = 50

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	})
	app.Resize(1280, 800)

	// Toggle Terrain button in 3D toolbar opens a new terrain and mounts TerrainPanelView.
	runEditorCommand(t, app, ed, editor.CmdTerrainEditor)
	if !ed.TerrainOpen() {
		t.Fatalf("expected TerrainOpen() after pressing toolbar-terrain")
	}
	if app.Find("id:terrain-panel") == nil {
		t.Fatalf("expected terrain-panel to mount")
	}

	// Select Raise brush and Linear falloff via UI controls.
	app.Press("id:terrain-brush-raise")
	if ed.TerrainBrush().Mode != editor.TerrainBrushRaise {
		t.Fatalf("brush mode = %v, want Raise", ed.TerrainBrush().Mode)
	}
	app.Press("id:terrain-falloff-linear")
	if ed.TerrainBrush().Falloff != editor.TerrainFalloffLinear {
		t.Fatalf("brush falloff = %v, want Linear", ed.TerrainBrush().Falloff)
	}

	// Sculpt via 3D viewport raycast drag and verify one undo step + DrawViewport3D.
	const w, h float32 = 640, 360
	sx0, sy0, _, vis0 := ed.Camera3D.Project([3]float32{16, 0, 16}, w, h)
	sx1, sy1, _, vis1 := ed.Camera3D.Project([3]float32{22, 0, 16}, w, h)
	if !vis0 || !vis1 {
		t.Fatalf("expected terrain center to project into 3D viewport")
	}
	ed.SetTerrainHoverAt(sx0, sy0, w, h)
	ed.BeginViewport3DDrag(sx0, sy0, w, h, false)
	ed.UpdateViewport3DDrag(sx1, sy1)
	ed.EndViewport3DDrag()
	if ed.History.Len() != 1 {
		t.Fatalf("3D viewport terrain drag recorded %d history steps, want 1", ed.History.Len())
	}
	if y, ok := ed.Terrain().HeightAt(16, 16); !ok || y <= 0 {
		t.Fatalf("terrain center height after 3D drag = (%v, %v), want > 0", y, ok)
	}

	// Drawing the 3D viewport with open terrain + brush cursor ring succeeds cleanly.
	var drawing core.Drawing
	ed.DrawViewport3D(&drawing, core.Size{Width: 640, Height: 360})
}
