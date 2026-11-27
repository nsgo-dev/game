package editor_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/assetbuild"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// buildAnimatedTestGLB constructs a valid binary glTF 2.0 (.glb) with:
//   - 3 hierarchical nodes: ShipRoot (0) -> HullNode (1, mesh 0) and WingNode (2, mesh 1)
//   - 2 meshes: Hull (2 triangles, 4 vertices) and Wing (1 triangle, 3 vertices)
//   - 2 materials: HullPaint (red metallic) and WingTrim (blue double-sided)
//   - 2 animations:
//     0: "Hover" (1.0s) translating ShipRoot from (0,0,0) to (0,1.5,0)
//     1: "Spin" (0.8s) rotating WingNode 90 deg around Y
func buildAnimatedTestGLB(t *testing.T) []byte {
	t.Helper()

	var bin bytes.Buffer
	writeFloats := func(vals ...float32) (offset, length int) {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		offset = bin.Len()
		for _, v := range vals {
			_ = binary.Write(&bin, binary.LittleEndian, v)
		}
		return offset, bin.Len() - offset
	}
	writeUint16s := func(vals ...uint16) (offset, length int) {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		offset = bin.Len()
		for _, v := range vals {
			_ = binary.Write(&bin, binary.LittleEndian, v)
		}
		return offset, bin.Len() - offset
	}

	// Mesh 0 (Hull): 1x1 quad split into 2 triangles on Z=0
	hullPosOff, hullPosLen := writeFloats(
		-0.5, -0.5, 0,
		0.5, -0.5, 0,
		0.5, 0.5, 0,
		-0.5, 0.5, 0,
	)
	hullNormOff, hullNormLen := writeFloats(
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
	)
	hullIdxOff, hullIdxLen := writeUint16s(0, 1, 2, 0, 2, 3)

	// Mesh 1 (Wing): 1 triangle offset on X=[0.5, 1.5]
	wingPosOff, wingPosLen := writeFloats(
		0.5, 0, 0,
		1.5, 0, 0,
		0.5, 0.8, 0.2,
	)
	wingNormOff, wingNormLen := writeFloats(
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
	)

	// Animation 0 ("Hover"): times [0, 1.0], translations [(0,0,0), (0,1.5,0)]
	hoverTimeOff, hoverTimeLen := writeFloats(0, 1.0)
	hoverValOff, hoverValLen := writeFloats(
		0, 0, 0,
		0, 1.5, 0,
	)

	// Animation 1 ("Spin"): times [0, 0.8], rotations [identity, 90deg about Y]
	spinTimeOff, spinTimeLen := writeFloats(0, 0.8)
	s45 := float32(math.Sin(math.Pi / 4))
	c45 := float32(math.Cos(math.Pi / 4))
	spinValOff, spinValLen := writeFloats(
		0, 0, 0, 1,
		0, s45, 0, c45,
	)

	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	binBytes := bin.Bytes()

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"scene": 0,
		"scenes": []any{
			map[string]any{"name": "Main", "nodes": []any{0}},
		},
		"nodes": []any{
			map[string]any{"name": "ShipRoot", "children": []any{1, 2}},
			map[string]any{"name": "HullNode", "mesh": 0},
			map[string]any{"name": "WingNode", "mesh": 1},
		},
		"materials": []any{
			map[string]any{
				"name": "HullPaint",
				"pbrMetallicRoughness": map[string]any{
					"baseColorFactor": []any{0.85, 0.2, 0.15, 1.0},
					"metallicFactor":  0.6,
					"roughnessFactor": 0.35,
				},
			},
			map[string]any{
				"name":        "WingTrim",
				"doubleSided": true,
				"pbrMetallicRoughness": map[string]any{
					"baseColorFactor": []any{0.2, 0.65, 0.95, 1.0},
					"metallicFactor":  0.2,
					"roughnessFactor": 0.5,
				},
			},
		},
		"meshes": []any{
			map[string]any{
				"name": "Hull",
				"primitives": []any{
					map[string]any{
						"attributes": map[string]any{"POSITION": 0, "NORMAL": 1},
						"indices":    2,
						"material":   0,
					},
				},
			},
			map[string]any{
				"name": "Wing",
				"primitives": []any{
					map[string]any{
						"attributes": map[string]any{"POSITION": 3, "NORMAL": 4},
						"material":   1,
					},
				},
			},
		},
		"animations": []any{
			map[string]any{
				"name": "Hover",
				"samplers": []any{
					map[string]any{"input": 5, "output": 6, "interpolation": "LINEAR"},
				},
				"channels": []any{
					map[string]any{
						"sampler": 0,
						"target":  map[string]any{"node": 0, "path": "translation"},
					},
				},
			},
			map[string]any{
				"name": "Spin",
				"samplers": []any{
					map[string]any{"input": 7, "output": 8, "interpolation": "LINEAR"},
				},
				"channels": []any{
					map[string]any{
						"sampler": 0,
						"target":  map[string]any{"node": 2, "path": "rotation"},
					},
				},
			},
		},
		"buffers": []any{
			map[string]any{"byteLength": len(binBytes)},
		},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": hullPosOff, "byteLength": hullPosLen},
			map[string]any{"buffer": 0, "byteOffset": hullNormOff, "byteLength": hullNormLen},
			map[string]any{"buffer": 0, "byteOffset": hullIdxOff, "byteLength": hullIdxLen},
			map[string]any{"buffer": 0, "byteOffset": wingPosOff, "byteLength": wingPosLen},
			map[string]any{"buffer": 0, "byteOffset": wingNormOff, "byteLength": wingNormLen},
			map[string]any{"buffer": 0, "byteOffset": hoverTimeOff, "byteLength": hoverTimeLen},
			map[string]any{"buffer": 0, "byteOffset": hoverValOff, "byteLength": hoverValLen},
			map[string]any{"buffer": 0, "byteOffset": spinTimeOff, "byteLength": spinTimeLen},
			map[string]any{"buffer": 0, "byteOffset": spinValOff, "byteLength": spinValLen},
		},
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5126, "count": 4, "type": "VEC3", "min": []any{-0.5, -0.5, 0}, "max": []any{0.5, 0.5, 0}},
			map[string]any{"bufferView": 1, "componentType": 5126, "count": 4, "type": "VEC3"},
			map[string]any{"bufferView": 2, "componentType": 5123, "count": 6, "type": "SCALAR"},
			map[string]any{"bufferView": 3, "componentType": 5126, "count": 3, "type": "VEC3", "min": []any{0.5, 0, 0}, "max": []any{1.5, 0.8, 0.2}},
			map[string]any{"bufferView": 4, "componentType": 5126, "count": 3, "type": "VEC3"},
			map[string]any{"bufferView": 5, "componentType": 5126, "count": 2, "type": "SCALAR", "min": []any{0}, "max": []any{1.0}},
			map[string]any{"bufferView": 6, "componentType": 5126, "count": 2, "type": "VEC3"},
			map[string]any{"bufferView": 7, "componentType": 5126, "count": 2, "type": "SCALAR", "min": []any{0}, "max": []any{0.8}},
			map[string]any{"bufferView": 8, "componentType": 5126, "count": 2, "type": "VEC4"},
		},
	}

	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, ' ')
	}
	totalLen := 12 + 8 + len(jsonBytes) + 8 + len(binBytes)
	var glb bytes.Buffer
	_ = binary.Write(&glb, binary.LittleEndian, uint32(0x46546C67)) // "glTF"
	_ = binary.Write(&glb, binary.LittleEndian, uint32(2))          // version 2
	_ = binary.Write(&glb, binary.LittleEndian, uint32(totalLen))
	_ = binary.Write(&glb, binary.LittleEndian, uint32(len(jsonBytes)))
	_ = binary.Write(&glb, binary.LittleEndian, uint32(0x4E4F534A)) // "JSON"
	glb.Write(jsonBytes)
	_ = binary.Write(&glb, binary.LittleEndian, uint32(len(binBytes)))
	_ = binary.Write(&glb, binary.LittleEndian, uint32(0x004E4942)) // "BIN\0"
	glb.Write(binBytes)

	if _, err := gltf.Load(glb.Bytes(), nil); err != nil {
		t.Fatalf("buildAnimatedTestGLB produced invalid GLB: %v", err)
	}
	return glb.Bytes()
}

func countNonBackgroundPixels(img *image.RGBA, bgR, bgG, bgB uint8) int {
	n := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i] != bgR || img.Pix[i+1] != bgG || img.Pix[i+2] != bgB {
			n++
		}
	}
	return n
}

func countDifferingPixels(a, b *image.RGBA) int {
	if a == nil || b == nil || len(a.Pix) != len(b.Pix) {
		return -1
	}
	n := 0
	for i := 0; i+3 < len(a.Pix); i += 4 {
		if a.Pix[i] != b.Pix[i] || a.Pix[i+1] != b.Pix[i+1] || a.Pix[i+2] != b.Pix[i+2] {
			n++
		}
	}
	return n
}

// #897 Acceptance 1: selecting a GLB in the file browser opens the model preview,
// renders the model with an orbit camera, lists its nodes, meshes, materials, and
// animations with bounds and triangle counts, and plays/pauses/scrubs each of its animations.
func TestModelImportPreviewShowsGLBAndPlaysEachAnimation(t *testing.T) {
	dir := t.TempDir()
	shipGLB := buildAnimatedTestGLB(t)

	gameYAML := `version: 1
name: ModelPreviewFixture
id: dev.example.modelpreview
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
assets:
  models:
    - in: models/ship.glb
      out: assets/ship.glb
`
	roomScene := `version: 2
name: Room
layers:
  - name: actors
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": roomScene,
	}, map[string][]byte{
		"models/ship.glb": shipGLB,
	})

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// Select models/ship.glb in the file browser.
	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-models/ship.glb")
	app.Settle()

	if !app.Exists("id:inspector-asset-import") {
		t.Fatal("expected inspector-asset-import when a GLB is selected")
	}
	if !app.Exists("id:import-model-preview-canvas") {
		t.Fatal("expected import-model-preview-canvas in model import preview")
	}
	if got := allText(app.Find("id:import-asset-target")); !strings.Contains(got, "assets/ship.glb") {
		t.Fatalf("import-asset-target = %q, want target assets/ship.glb", got)
	}

	// Verify metadata inspection (nodes, meshes, materials, animations, bounds, triangles).
	info, err := ed.InspectModelImport("models/ship.glb")
	if err != nil {
		t.Fatalf("InspectModelImport: %v", err)
	}
	if len(info.Nodes) != 3 {
		t.Fatalf("Nodes count = %d, want 3", len(info.Nodes))
	}
	if len(info.Meshes) != 2 {
		t.Fatalf("Meshes count = %d, want 2", len(info.Meshes))
	}
	if len(info.Materials) != 2 {
		t.Fatalf("Materials count = %d, want 2", len(info.Materials))
	}
	if len(info.Animations) != 2 {
		t.Fatalf("Animations count = %d, want 2", len(info.Animations))
	}
	if info.TotalTriangles != 3 {
		t.Fatalf("TotalTriangles = %d, want 3 (2 hull + 1 wing)", info.TotalTriangles)
	}
	if info.TotalVertices != 7 {
		t.Fatalf("TotalVertices = %d, want 7 (4 hull + 3 wing)", info.TotalVertices)
	}
	if info.Bounds.Empty() {
		t.Fatal("expected non-empty model bounds")
	}
	ext := info.Bounds.Size()
	if math.Abs(float64(ext.X)-2.0) > 1e-4 || math.Abs(float64(ext.Y)-1.3) > 1e-4 {
		t.Fatalf("Bounds size = %+v, want approx (2.0, 1.3, 0.2)", ext)
	}

	// Verify UI renders nodes, meshes, materials, animations, bounds, and triangle counts.
	if got := allText(app.Find("id:import-model-preview-info")); !strings.Contains(got, "3 tris") {
		t.Fatalf("import-model-preview-info = %q, want triangle count", got)
	}
	if got := allText(app.Find("id:import-model-bounds")); !strings.Contains(got, "Bounds:") {
		t.Fatalf("import-model-bounds = %q, want bounds summary", got)
	}
	nodesText := allText(app.Find("id:import-model-nodes-list"))
	for _, want := range []string{"ShipRoot", "HullNode", "WingNode"} {
		if !strings.Contains(nodesText, want) {
			t.Fatalf("nodes list missing %q:\n%s", want, nodesText)
		}
	}
	meshesText := allText(app.Find("id:import-model-meshes-list"))
	for _, want := range []string{"Hull", "Wing", "2 tris", "1 tris"} {
		if !strings.Contains(meshesText, want) {
			t.Fatalf("meshes list missing %q:\n%s", want, meshesText)
		}
	}
	matsText := allText(app.Find("id:import-model-materials-list"))
	for _, want := range []string{"HullPaint", "WingTrim", "2-sided"} {
		if !strings.Contains(matsText, want) {
			t.Fatalf("materials list missing %q:\n%s", want, matsText)
		}
	}
	animsText := allText(app.Find("id:import-model-animations-list"))
	for _, want := range []string{"Hover", "Spin"} {
		if !strings.Contains(animsText, want) {
			t.Fatalf("animations list missing %q:\n%s", want, animsText)
		}
	}

	// Verify the model renders non-background pixels on the software GPU.
	restImg, err := ed.RenderModelPreview("models/ship.glb", 160, 120)
	if err != nil {
		t.Fatalf("RenderModelPreview: %v", err)
	}
	if lit := countNonBackgroundPixels(restImg, 0x1E, 0x29, 0x3B); lit < 100 {
		t.Fatalf("rendered model pixels = %d, want visible geometry (>= 100 pixels)", lit)
	}

	// Orbiting the camera changes the rendered image, and Reset View restores it.
	origCam := ed.ModelPreviewCamera()
	ed.OrbitModelPreview(0.9, 0.2)
	orbitedImg, err := ed.RenderModelPreview("models/ship.glb", 160, 120)
	if err != nil {
		t.Fatalf("RenderModelPreview after orbit: %v", err)
	}
	if diff := countDifferingPixels(restImg, orbitedImg); diff < 50 {
		t.Fatalf("orbiting camera changed only %d pixels, want visible camera rotation", diff)
	}
	ed.ZoomModelPreview(1.5)
	if !(ed.ModelPreviewCamera().Distance < origCam.Distance) {
		t.Fatalf("ZoomModelPreview(1.5) did not decrease distance: got %v, orig %v", ed.ModelPreviewCamera().Distance, origCam.Distance)
	}
	ed.PanModelPreview(12, -8, 120)
	app.Press("id:import-model-reset-view")
	app.Settle()
	if got := ed.ModelPreviewCamera(); math.Abs(float64(got.Yaw-origCam.Yaw)) > 1e-4 || math.Abs(float64(got.Distance-origCam.Distance)) > 1e-4 {
		t.Fatalf("Reset View camera = %+v, want %+v", got, origCam)
	}

	// Play animation 0 ("Hover") via its UI row Play button, step forward, verify pose & pixels change.
	app.Press("id:import-model-anim-play-0")
	app.Settle()
	if !ed.ModelPreviewPlaying() || ed.ModelPreviewAnimation() != 0 {
		t.Fatalf("after pressing anim 0 play: playing=%v anim=%d, want true, 0", ed.ModelPreviewPlaying(), ed.ModelPreviewAnimation())
	}
	ed.PauseModelPreviewAnimation()
	if err := ed.SeekModelPreviewAnimation("models/ship.glb", 500*time.Millisecond); err != nil {
		t.Fatalf("SeekModelPreviewAnimation(Hover, 0.5s): %v", err)
	}
	rootMat, ok := ed.ModelPreviewNodeWorldTransform(0)
	if !ok {
		t.Fatal("missing world transform for node 0")
	}
	if math.Abs(float64(rootMat[13])-0.75) > 1e-3 {
		t.Fatalf("Hover at 0.5s: ShipRoot Y translation = %v, want 0.75", rootMat[13])
	}
	hoverImg, err := ed.RenderModelPreview("models/ship.glb", 160, 120)
	if err != nil {
		t.Fatal(err)
	}
	if diff := countDifferingPixels(restImg, hoverImg); diff < 50 {
		t.Fatalf("Hover animation at 0.5s changed only %d pixels vs rest pose", diff)
	}

	// Play animation 1 ("Spin") via its UI row Play button, step forward, verify WingNode rotates & pixels differ.
	app.Press("id:import-model-anim-play-1")
	app.Settle()
	if !ed.ModelPreviewPlaying() || ed.ModelPreviewAnimation() != 1 {
		t.Fatalf("after pressing anim 1 play: playing=%v anim=%d, want true, 1", ed.ModelPreviewPlaying(), ed.ModelPreviewAnimation())
	}
	ed.StepModelPreviewAnimation(400 * time.Millisecond)
	ed.PauseModelPreviewAnimation()
	wingMat, ok := ed.ModelPreviewNodeWorldTransform(2)
	if !ok {
		t.Fatal("missing world transform for node 2")
	}
	// At t >= 0.4s of 0.8s (90 deg Y rotation), WingNode's X-axis Z component (col0.Z = -sin(theta)) is non-zero.
	if math.Abs(float64(wingMat[2])) < 0.3 {
		t.Fatalf("Spin animation did not rotate WingNode: world matrix col0.Z = %v", wingMat[2])
	}
	spinImg, err := ed.RenderModelPreview("models/ship.glb", 160, 120)
	if err != nil {
		t.Fatal(err)
	}
	if diff := countDifferingPixels(restImg, spinImg); diff < 20 {
		t.Fatalf("Spin animation changed only %d pixels vs rest pose", diff)
	}

	// Stop restores bind pose (time 0, ShipRoot Y = 0, WingNode unrotated).
	app.Press("id:import-model-anim-stop")
	app.Settle()
	if ed.ModelPreviewPlaying() || ed.ModelPreviewTime() != 0 {
		t.Fatalf("after Stop: playing=%v time=%v, want false, 0", ed.ModelPreviewPlaying(), ed.ModelPreviewTime())
	}
	restoredRoot, _ := ed.ModelPreviewNodeWorldTransform(0)
	restoredWing, _ := ed.ModelPreviewNodeWorldTransform(2)
	if math.Abs(float64(restoredRoot[13])) > 1e-4 || math.Abs(float64(restoredWing[2])) > 1e-4 {
		t.Fatalf("Stop did not restore bind pose: rootY=%v wingRotZ=%v", restoredRoot[13], restoredWing[2])
	}
}

// #897 Acceptance 2: changing scale writes the sidecar in importsettings.Encode's
// deterministic form, and the next `game assets` run rebuilds ONLY that model
// (leaving other models and textures up to date) with the new scale baked in.
func TestModelImportScaleWritesSidecarAndRebuildsOnlyThatModel(t *testing.T) {
	dir := t.TempDir()
	shipGLB := buildAnimatedTestGLB(t)
	crateGLB := buildAnimatedTestGLB(t)

	gameYAML := `version: 1
name: ModelBuildFixture
id: dev.example.modelbuild
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
assets:
  textures:
    - in: art/albedo.png
      out: assets/albedo.ktx2
      format: rgba8
  models:
    - in: models/ship.glb
      out: assets/ship.glb
    - in: models/crate.glb
      out: assets/crate.glb
`
	roomScene := `version: 2
name: Room
layers:
  - name: actors
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": roomScene,
	}, map[string][]byte{
		"art/albedo.png":   encodeTestPNG(t, 16, 16),
		"models/ship.glb":  shipGLB,
		"models/crate.glb": crateGLB,
	})

	// Initial `game assets` run builds all three targets and writes the asset cache.
	pf, err := project.Parse([]byte(gameYAML))
	if err != nil {
		t.Fatal(err)
	}
	b := &assetbuild.Builder{Root: dir}
	rep1, err := b.Build(context.Background(), pf.Assets)
	if err != nil {
		t.Fatalf("initial assetbuild: %v", err)
	}
	if rep1.Count(assetbuild.Built) != 3 {
		t.Fatalf("initial built count = %d, want 3 (%+v)", rep1.Count(assetbuild.Built), rep1.Targets)
	}
	crateOutBefore, err := os.ReadFile(filepath.Join(dir, "assets", "crate.glb"))
	if err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.AssetsCommandRunner = runAssetBuildInTest
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// Select models/ship.glb in the file browser; verify game.yaml target is shown.
	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-models/ship.glb")
	app.Settle()
	if got := allText(app.Find("id:import-asset-target")); !strings.Contains(got, "Model: assets/ship.glb") {
		t.Fatalf("import-asset-target = %q, want Model: assets/ship.glb", got)
	}

	// Change scale to 0.25 and add credit metadata.
	if err := ed.SetModelImportScale("models/ship.glb", 0.25); err != nil {
		t.Fatalf("SetModelImportScale: %v", err)
	}
	if err := ed.SetAssetImportCredit("models/ship.glb", &importsettings.Credit{
		Author:  "Shipwright",
		License: "CC0-1.0",
	}); err != nil {
		t.Fatalf("SetAssetImportCredit: %v", err)
	}
	app.Settle()

	// Preview info reflects scaled bounds immediately.
	info, err := ed.InspectModelImport("models/ship.glb")
	if err != nil {
		t.Fatal(err)
	}
	if info.Scale != 0.25 {
		t.Fatalf("InspectModelImport.Scale = %v, want 0.25", info.Scale)
	}
	if math.Abs(float64(info.ScaledBounds.Size().X)-0.5) > 1e-4 {
		t.Fatalf("ScaledBounds.Size().X = %v, want 0.5 (2.0 * 0.25)", info.ScaledBounds.Size().X)
	}
	if got := allText(app.Find("id:import-model-bounds")); !strings.Contains(got, "scaled") {
		t.Fatalf("import-model-bounds = %q, want scaled bounds shown", got)
	}

	// Sidecar must match importsettings.Encode byte-for-byte.
	sidecarPath := filepath.Join(dir, "models", "ship.glb.import.yaml")
	rawSidecar, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	wantEncoded, err := importsettings.Encode(&importsettings.File{
		Version: importsettings.CurrentVersion,
		Model:   &importsettings.Model{Scale: 0.25},
		Credit:  &importsettings.Credit{Author: "Shipwright", License: "CC0-1.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rawSidecar, wantEncoded) {
		t.Fatalf("sidecar bytes mismatch:\ngot:\n%s\nwant:\n%s", rawSidecar, wantEncoded)
	}

	// Next `game assets` run must rebuild ONLY models/ship.glb (assets/ship.glb).
	rep2, err := b.Build(context.Background(), pf.Assets)
	if err != nil {
		t.Fatalf("second assetbuild: %v", err)
	}
	var builtIDs, upToDateIDs []string
	for _, tgt := range rep2.Targets {
		switch tgt.Status {
		case assetbuild.Built:
			builtIDs = append(builtIDs, tgt.ID)
		case assetbuild.UpToDate:
			upToDateIDs = append(upToDateIDs, tgt.ID)
		}
	}
	if len(builtIDs) != 1 || builtIDs[0] != "model assets/ship.glb" {
		t.Fatalf("built targets = %v, want only [model assets/ship.glb]; all=%+v", builtIDs, rep2.Targets)
	}
	if len(upToDateIDs) != 2 {
		t.Fatalf("up-to-date targets = %v, want 2 (texture + crate model)", upToDateIDs)
	}

	// Verify assets/ship.glb actually has the 0.25 scale wrapper node baked in, while crate.glb is unchanged.
	rebuiltShipBytes, err := os.ReadFile(filepath.Join(dir, "assets", "ship.glb"))
	if err != nil {
		t.Fatal(err)
	}
	rebuiltDoc, err := gltf.Load(rebuiltShipBytes, nil)
	if err != nil {
		t.Fatalf("load rebuilt ship.glb: %v", err)
	}
	foundScaledWrapper := false
	for _, n := range rebuiltDoc.Nodes {
		if math.Abs(float64(n.Scale.X)-0.25) < 1e-5 && math.Abs(float64(n.Scale.Y)-0.25) < 1e-5 && math.Abs(float64(n.Scale.Z)-0.25) < 1e-5 {
			foundScaledWrapper = true
			break
		}
	}
	if !foundScaledWrapper {
		t.Fatalf("rebuilt assets/ship.glb missing 0.25 scale wrapper node: %+v", rebuiltDoc.Nodes)
	}
	crateOutAfter, err := os.ReadFile(filepath.Join(dir, "assets", "crate.glb"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(crateOutBefore, crateOutAfter) {
		t.Fatal("untouched crate.glb output changed unexpectedly")
	}

	// Also verify Reimport button streams `game assets` output when scale changes again (0.25 -> 0.5).
	if err := ed.SetModelImportScale("models/ship.glb", 0.5); err != nil {
		t.Fatal(err)
	}
	app.Press("id:import-reimport")
	app.WaitUntil(5*time.Second, func() bool { return !ed.AssetsBuilding() })
	if err := ed.AssetsBuildLastError(); err != nil {
		t.Fatalf("Reimport failed: %v", err)
	}
	app.Settle()
	outText := allText(app.Find("id:output-panel"))
	if !strings.Contains(outText, "built model assets/ship.glb") || !strings.Contains(outText, "1 built, 2 up to date") {
		t.Fatalf("Output panel did not report rebuilding only ship.glb:\n%s", outText)
	}
}

// Verify real-world Khronos glTF fixtures with external .bin buffers, skeletal skinning
// (RiggedSimple), and morph target animation (AnimatedMorphCube) load, inspect, animate,
// and render distinct frames in the model preview.
func TestModelImportPreviewSkinnedAndMorphGLTF(t *testing.T) {
	copyDir := func(src, dst string) {
		t.Helper()
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", src, err)
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(src, ent.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dst, ent.Name()), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	dir := t.TempDir()
	gameYAML := `version: 1
name: KhronosPreviewFixture
id: dev.example.khronospreview
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": "version: 2\nname: Room\nlayers:\n  - name: actors\n",
	}, nil)

	copyDir(filepath.Join("..", "gltf", "testdata", "khronos", "RiggedSimple", "glTF"), filepath.Join(dir, "models", "rigged"))
	copyDir(filepath.Join("..", "gltf", "testdata", "khronos", "AnimatedMorphCube", "glTF"), filepath.Join(dir, "models", "morph"))

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// 1. Skinned glTF model: RiggedSimple.gltf
	rigPath := "models/rigged/RiggedSimple.gltf"
	rigInfo, err := ed.InspectModelImport(rigPath)
	if err != nil {
		t.Fatalf("InspectModelImport(RiggedSimple): %v", err)
	}
	if len(rigInfo.Animations) == 0 || rigInfo.TotalTriangles == 0 {
		t.Fatalf("RiggedSimple info = %+v, want animations and triangles", rigInfo)
	}
	rigFrame0, err := ed.RenderModelPreview(rigPath, 128, 128)
	if err != nil {
		t.Fatalf("RenderModelPreview(RiggedSimple, rest): %v", err)
	}
	if countNonBackgroundPixels(rigFrame0, 0x1E, 0x29, 0x3B) < 100 {
		t.Fatal("RiggedSimple did not render visible geometry")
	}
	if err := ed.SeekModelPreviewAnimation(rigPath, rigInfo.Animations[0].Duration/2); err != nil {
		t.Fatalf("SeekModelPreviewAnimation(RiggedSimple): %v", err)
	}
	rigFrameMid, err := ed.RenderModelPreview(rigPath, 128, 128)
	if err != nil {
		t.Fatalf("RenderModelPreview(RiggedSimple, mid): %v", err)
	}
	if diff := countDifferingPixels(rigFrame0, rigFrameMid); diff < 20 {
		t.Fatalf("RiggedSimple skinned animation changed only %d pixels at mid-clip", diff)
	}

	// 2. Morph target glTF model: AnimatedMorphCube.gltf
	morphPath := "models/morph/AnimatedMorphCube.gltf"
	morphInfo, err := ed.InspectModelImport(morphPath)
	if err != nil {
		t.Fatalf("InspectModelImport(AnimatedMorphCube): %v", err)
	}
	if len(morphInfo.Animations) == 0 || len(morphInfo.Meshes) == 0 || morphInfo.Meshes[0].MorphTargets == 0 {
		t.Fatalf("AnimatedMorphCube info = %+v, want animations and morph targets", morphInfo)
	}
	morphFrame0, err := ed.RenderModelPreview(morphPath, 128, 128)
	if err != nil {
		t.Fatalf("RenderModelPreview(AnimatedMorphCube, rest): %v", err)
	}
	if err := ed.SeekModelPreviewAnimation(morphPath, morphInfo.Animations[0].Duration/2); err != nil {
		t.Fatalf("SeekModelPreviewAnimation(AnimatedMorphCube): %v", err)
	}
	morphFrameMid, err := ed.RenderModelPreview(morphPath, 128, 128)
	if err != nil {
		t.Fatalf("RenderModelPreview(AnimatedMorphCube, mid): %v", err)
	}
	if diff := countDifferingPixels(morphFrame0, morphFrameMid); diff < 20 {
		t.Fatalf("AnimatedMorphCube morph animation changed only %d pixels at mid-clip", diff)
	}
}

// Security & Play-mode regression: external .bin buffers or textures referenced by a .gltf
// must not escape the project root through symlinks, and PlayModelPreviewAnimation must refuse
// to start while the editor is in Play mode.
func TestModelImportPreviewRefusesSymlinkExternalBufferAndPlayMode(t *testing.T) {
	outside := t.TempDir()
	outsideBin := filepath.Join(outside, "secret.bin")
	// 3 vertices * 3 float32 = 36 bytes
	binBuf := make([]byte, 36)
	if err := os.WriteFile(outsideBin, binBuf, 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	gameYAML := `version: 1
name: SymlinkModelTest
id: dev.example.symlinkmodel
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
`
	gltfDoc := `{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0]}],
  "nodes": [{"mesh": 0}],
  "meshes": [{"primitives": [{"attributes": {"POSITION": 0}}]}],
  "buffers": [{"uri": "escape.bin", "byteLength": 36}],
  "bufferViews": [{"buffer": 0, "byteOffset": 0, "byteLength": 36}],
  "accessors": [{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"}]
}`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": "version: 2\nname: Room\nlayers:\n  - name: actors\n",
		"models/evil.gltf":       gltfDoc,
	}, map[string][]byte{
		"models/ship.glb": buildAnimatedTestGLB(t),
	})

	if err := os.Symlink(outsideBin, filepath.Join(dir, "models", "escape.bin")); err != nil {
		t.Skipf("symlink unsupported in this environment: %v", err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// First load a valid model so we also verify failed loads clear stale preview state.
	if _, err := ed.InspectModelImport("models/ship.glb"); err != nil {
		t.Fatalf("InspectModelImport(ship.glb): %v", err)
	}
	if _, err := ed.InspectModelImport("models/evil.gltf"); err == nil {
		t.Fatal("expected symlinked external .bin buffer to be refused")
	} else if !strings.Contains(err.Error(), "outside the project root") {
		t.Fatalf("unexpected error for symlinked external .bin: %v", err)
	}
	if _, err := ed.RenderModelPreview("models/evil.gltf", 64, 64); err == nil {
		t.Fatal("expected RenderModelPreview to refuse symlinked external .bin buffer")
	}

	// Play-mode guard: PlayModelPreviewAnimation must return ErrPlaying while playing.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(960, 640))
	if err := ed.Play(); err != nil {
		t.Fatalf("Play(): %v", err)
	}
	defer func() { _ = ed.Stop() }()
	app.Settle()
	if err := ed.PlayModelPreviewAnimation("models/ship.glb", 0); err != editor.ErrPlaying {
		t.Fatalf("PlayModelPreviewAnimation during Play = %v, want ErrPlaying", err)
	}
}
