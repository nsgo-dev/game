package editor_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeCheckerPNG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			if ((x/4)+(y/4))%2 == 0 {
				img.SetNRGBA(x, y, color.NRGBA{R: 230, G: 190, B: 80, A: 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{R: 40, G: 70, B: 110, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func renderScene3DSoftware(t *testing.T, ed *editor.Editor, w, h int) *image.RGBA {
	t.Helper()
	built, err := ed.Scene3D()
	if err != nil {
		t.Fatalf("Scene3D: %v", err)
	}
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	r, err := scenemodel.NewRenderer(nil, dev, built, scenemodel.RendererOptions{
		Label:       "test.material.viewport",
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer func() { _ = r.Close() }()
	if _, err := r.Prepare(ed.Camera3D.SceneCamera(float32(w), float32(h)), editor.DefaultLights3D()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "test.material.viewport",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGBA(0x141824, 1)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	r.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("SoftwareRenderer: %v", err)
	}
	src := sr.Image()
	out := image.NewRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	return out
}

func writeMaterialProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"game.yaml": "version: 1\nname: MatDemo\nid: dev.example.matdemo\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: main\n  scenes:\n    - name: main\n      path: scenes/main.scene.yaml\n",
		"scenes/main.scene.yaml": `version: 2
name: Main
virtualWidth: 320
virtualHeight: 180
layers:
  - name: main
    objects:
      - id: sphere_a
        x: -1
        y: 0
        mesh: sphere
        material: materials/metal.material.yaml
      - id: box_b
        x: 1
        y: 0
        mesh: box
        material: materials/metal.material.yaml
      - id: hero
        x: 32
        "y": 40
        sprite:
          atlas: actors
          frame: hero
        material: materials/metal.material.yaml
      - id: ally
        x: 80
        "y": 40
        sprite:
          atlas: actors
          frame: hero
        material: materials/metal.material.yaml
`,
		"materials/metal.material.yaml": "type: material\nversion: 1\nname: Metal\nbaseColor: '#80c0e0'\nmetallic: 0.2\nroughness: 0.9\n",
		"materials/alt.material.yaml":   "type: material\nversion: 1\nname: Alt\nbaseColor: '#e07050'\n",
		"actors.json":                   `{"frames":{"hero":{"frame":{"x":0,"y":0,"w":16,"h":16}}},"meta":{"image":"actors.png","size":{"w":16,"h":16}}}`,
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeCheckerPNG(t, filepath.Join(dir, "actors.png"))
	writeCheckerPNG(t, filepath.Join(dir, "textures", "checker.png"))
	return dir
}

func materialRoughnessOfObject(t *testing.T, built *scenemodel.Built, objectID string) float32 {
	t.Helper()
	for _, d := range built.Drawables {
		if d.Object == objectID && len(d.Parts) > 0 {
			return d.Parts[0].Material.Roughness
		}
	}
	t.Fatalf("object %q not found in Scene3D drawables", objectID)
	return -1
}

// TestMaterialRoughnessUpdatesPreviewAndViewportBeforeSavingAndSavesEncodedWithUndo
// verifies both acceptance criteria of issue #896:
//  1. Changing roughness in the Material Editor panel updates the render3d preview and every
//     object using that material in the viewport BEFORE saving (via Library.Reload).
//  2. Saving writes the file in exact resource.Encode form, and Undo restores the previous
//     roughness in the panel, preview, and every viewport object.
func TestMaterialRoughnessUpdatesPreviewAndViewportBeforeSavingAndSavesEncodedWithUndo(t *testing.T) {
	dir := writeMaterialProject(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.3,
		Pitch:    0.35,
		Distance: 4.5,
		FOV:      float32(math.Pi / 4),
		Near:     0.1,
		Far:      30,
	}

	matFile := filepath.Join(dir, "materials", "metal.material.yaml")
	origDisk, err := os.ReadFile(matFile)
	if err != nil {
		t.Fatal(err)
	}

	if err := ed.OpenMaterialProjectFile("materials/metal.material.yaml"); err != nil {
		t.Fatalf("OpenMaterialProjectFile: %v", err)
	}
	if !ed.MaterialOpen() || ed.MaterialDirty() {
		t.Fatalf("after open: open=%v dirty=%v", ed.MaterialOpen(), ed.MaterialDirty())
	}

	// Capture initial 3D preview and 3D viewport pixels & built material roughness.
	previewBefore, err := ed.MaterialPreviewImage(editor.MaterialPreviewSize)
	if err != nil || previewBefore == nil {
		t.Fatalf("MaterialPreviewImage before: %v, %v", previewBefore != nil, err)
	}
	builtBefore, err := ed.Scene3D()
	if err != nil {
		t.Fatalf("Scene3D before: %v", err)
	}
	if rA, rB := materialRoughnessOfObject(t, builtBefore, "sphere_a"), materialRoughnessOfObject(t, builtBefore, "box_b"); rA != 0.9 || rB != 0.9 {
		t.Fatalf("initial roughness = (%v, %v), want (0.9, 0.9)", rA, rB)
	}
	viewportBefore := renderScene3DSoftware(t, ed, 160, 120)

	// Mount UI and change roughness in the Material Editor panel BEFORE saving.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	app.Clear("id:material-roughness")
	app.Type("id:material-roughness", "0.1")
	app.Submit("id:material-roughness")

	if got := ed.Material().EffectiveRoughness(); got != 0.1 {
		t.Fatalf("Material.Roughness after edit = %v, want 0.1", got)
	}
	if !ed.MaterialDirty() || !ed.HasUnsavedChanges() {
		t.Fatal("expected MaterialDirty and HasUnsavedChanges before saving")
	}

	// File on disk must NOT have been written yet.
	diskBeforeSave, err := os.ReadFile(matFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(diskBeforeSave, origDisk) {
		t.Fatalf("material file changed on disk before saving:\n%s", diskBeforeSave)
	}

	// Live preview image must have updated immediately.
	previewAfter, err := ed.MaterialPreviewImage(editor.MaterialPreviewSize)
	if err != nil || previewAfter == nil {
		t.Fatalf("MaterialPreviewImage after: %v, %v", previewAfter != nil, err)
	}
	if bytes.Equal(previewBefore.Pix, previewAfter.Pix) {
		t.Fatal("MaterialPreviewImage pixels did not change when roughness changed from 0.9 to 0.1")
	}

	// Shared resource.Library must hold the live reloaded roughness, and EVERY viewport object
	// referencing that material must update before saving.
	mr := ed.Resolver.(sceneformat.MaterialResolver)
	sharedMat, err := mr.Material("materials/metal.material.yaml")
	if err != nil || sharedMat.EffectiveRoughness() != 0.1 {
		t.Fatalf("shared Library material roughness = %v (err=%v), want 0.1 via Library.Reload", sharedMat.EffectiveRoughness(), err)
	}
	builtAfter, err := ed.Scene3D()
	if err != nil {
		t.Fatalf("Scene3D after: %v", err)
	}
	if rA, rB := materialRoughnessOfObject(t, builtAfter, "sphere_a"), materialRoughnessOfObject(t, builtAfter, "box_b"); rA != 0.1 || rB != 0.1 {
		t.Fatalf("viewport roughness after edit = (%v, %v), want (0.1, 0.1)", rA, rB)
	}
	viewportAfter := renderScene3DSoftware(t, ed, 160, 120)
	if bytes.Equal(viewportBefore.Pix, viewportAfter.Pix) {
		t.Fatal("3D viewport pixels did not update when shared material roughness changed")
	}

	// 2D objects sharing the same material also update live before saving when opacity/blend change.
	if err := ed.SetMaterialOpacity(0.4); err != nil {
		t.Fatalf("SetMaterialOpacity: %v", err)
	}
	if err := ed.SetMaterialBlend("multiply"); err != nil {
		t.Fatalf("SetMaterialBlend: %v", err)
	}
	for _, id := range []string{"hero", "ally"} {
		bo := ed.Built.Object(id)
		if bo == nil || bo.Sprite == nil {
			t.Fatalf("2D BuiltObject %q missing sprite", id)
		}
		if math.Abs(float64(bo.Sprite.Alpha-0.4)) > 1e-5 || bo.Sprite.Blend != core.CompositeMultiply {
			t.Fatalf("2D object %q after live edit: alpha=%v blend=%v, want 0.4 multiply", id, bo.Sprite.Alpha, bo.Sprite.Blend)
		}
	}

	// Undo opacity & blend so only roughness=0.1 remains, then save via the panel Save button.
	app.Post(func() {
		ed.Undo()
		ed.Undo()
	})
	app.Settle()

	app.Press("id:material-save")
	if ed.MaterialDirty() {
		t.Fatal("MaterialDirty still true after Save Material")
	}
	wantEncoded, err := resource.Encode(ed.Material())
	if err != nil {
		t.Fatalf("resource.Encode: %v", err)
	}
	savedDisk, err := os.ReadFile(matFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(savedDisk, wantEncoded) {
		t.Fatalf("saved file differs from resource.Encode:\ngot:\n%s\nwant:\n%s", savedDisk, wantEncoded)
	}

	// Undo must restore the previous roughness (0.9) in the panel, preview, and every viewport object.
	app.Post(func() {
		if !ed.Undo() {
			t.Error("Undo returned false")
		}
	})
	app.Settle()
	if got := ed.Material().EffectiveRoughness(); got != 0.9 {
		t.Fatalf("Material.Roughness after Undo = %v, want 0.9", got)
	}
	previewUndone, err := ed.MaterialPreviewImage(editor.MaterialPreviewSize)
	if err != nil || previewUndone == nil {
		t.Fatalf("MaterialPreviewImage after Undo: %v", err)
	}
	if !bytes.Equal(previewBefore.Pix, previewUndone.Pix) {
		t.Fatal("MaterialPreviewImage after Undo does not match initial preview")
	}
	builtUndone, err := ed.Scene3D()
	if err != nil {
		t.Fatalf("Scene3D after Undo: %v", err)
	}
	if rA, rB := materialRoughnessOfObject(t, builtUndone, "sphere_a"), materialRoughnessOfObject(t, builtUndone, "box_b"); rA != 0.9 || rB != 0.9 {
		t.Fatalf("viewport roughness after Undo = (%v, %v), want (0.9, 0.9)", rA, rB)
	}
	viewportUndone := renderScene3DSoftware(t, ed, 160, 120)
	if !bytes.Equal(viewportBefore.Pix, viewportUndone.Pix) {
		t.Fatal("3D viewport pixels after Undo do not match initial frame")
	}

	// Redo restores 0.1 again.
	app.Post(func() {
		if !ed.Redo() {
			t.Error("Redo returned false")
		}
	})
	app.Settle()
	if got := ed.Material().EffectiveRoughness(); got != 0.1 {
		t.Fatalf("Material.Roughness after Redo = %v, want 0.1", got)
	}
}

// TestMaterialPanelEditsAllFieldsAndPreviewShapes verifies every field of resource.Material
// through the Material Editor panel UI (including color pickers, texture picker, blend select,
// PBR sliders/inputs, doubleSided, unlit, and Sphere/Cube/Sprite preview switching).
func TestMaterialPanelEditsAllFieldsAndPreviewShapes(t *testing.T) {
	dir := writeMaterialProject(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	if err := ed.OpenMaterialProjectFile("materials/metal.material.yaml"); err != nil {
		t.Fatalf("OpenMaterialProjectFile: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	// Name
	app.Clear("id:material-name")
	app.Type("id:material-name", "Polished Brass")
	if got := ed.Material().Name; got != "Polished Brass" {
		t.Fatalf("Name = %q, want Polished Brass", got)
	}

	// BaseColor via ColorWell / ColorPicker popup
	app.Press("id:material-base-color-well")
	if !app.Exists("id:material-base-color-picker") {
		t.Fatal("expected base color picker popup to open")
	}
	app.Clear("Hex color")
	app.Type("Hex color", "cc9933")
	app.Submit("Hex color")
	app.Key("escape")
	if got := ed.Material().BaseColor; got != "#cc9933" {
		t.Fatalf("BaseColor after ColorPicker = %q, want #cc9933", got)
	}

	// Emissive via ColorWell / ColorPicker popup
	app.Press("id:material-emissive-well")
	if !app.Exists("id:material-emissive-picker") {
		t.Fatal("expected emissive color picker popup to open")
	}
	app.Clear("Hex color")
	app.Type("Hex color", "221100")
	app.Submit("Hex color")
	app.Key("escape")
	if got := ed.Material().Emissive; got != "#221100" {
		t.Fatalf("Emissive after ColorPicker = %q, want #221100", got)
	}

	// Texture via picker
	app.Press("id:material-texture")
	app.Press("role:option textures/checker.png")
	if got := ed.Material().Texture; got != "textures/checker.png" {
		t.Fatalf("Texture = %q, want textures/checker.png", got)
	}

	// Metallic, Roughness, Opacity
	app.Clear("id:material-metallic")
	app.Type("id:material-metallic", "0.85")
	app.Submit("id:material-metallic")
	app.Clear("id:material-roughness")
	app.Type("id:material-roughness", "0.25")
	app.Submit("id:material-roughness")
	app.Clear("id:material-opacity")
	app.Type("id:material-opacity", "0.75")
	app.Submit("id:material-opacity")

	// Blend mode
	app.Press("id:material-blend")
	app.Press("role:option screen")

	// DoubleSided & Unlit
	app.Press("id:material-double-sided")
	app.Press("id:material-unlit")

	got := ed.Material()
	if got.Metallic != 0.85 || got.EffectiveRoughness() != 0.25 || got.EffectiveOpacity() != 0.75 || got.Blend != "screen" || !got.DoubleSided || !got.Unlit {
		t.Fatalf("edited material = %+v", got)
	}

	// Preview shape switching: Sphere vs Cube differ, Lit vs Unlit differ, Sprite returns nil 3D image.
	unlitSphere, err := ed.MaterialPreviewImage(editor.MaterialPreviewSize)
	if err != nil || unlitSphere == nil {
		t.Fatalf("unlit sphere preview: %v", err)
	}
	app.Press("id:material-unlit") // back to lit
	litSphere, err := ed.MaterialPreviewImage(editor.MaterialPreviewSize)
	if err != nil || litSphere == nil {
		t.Fatalf("lit sphere preview: %v", err)
	}
	if bytes.Equal(unlitSphere.Pix, litSphere.Pix) {
		t.Fatal("lit and unlit 3D material previews produced identical pixels")
	}

	app.Press("id:material-preview-cube")
	if ed.MaterialPreviewShape() != editor.MaterialPreviewCube {
		t.Fatalf("shape = %v, want Cube", ed.MaterialPreviewShape())
	}
	litCube, err := ed.MaterialPreviewImage(editor.MaterialPreviewSize)
	if err != nil || litCube == nil {
		t.Fatalf("lit cube preview: %v", err)
	}
	if bytes.Equal(litSphere.Pix, litCube.Pix) {
		t.Fatal("sphere and cube material previews produced identical pixels")
	}

	app.Press("id:material-preview-sprite")
	if ed.MaterialPreviewShape() != editor.MaterialPreviewSprite {
		t.Fatalf("shape = %v, want Sprite", ed.MaterialPreviewShape())
	}
	if img, err := ed.MaterialPreviewImage(editor.MaterialPreviewSize); img != nil || err != nil {
		t.Fatalf("Sprite preview returned (%v, %v), want (nil, nil)", img != nil, err)
	}

	// Save writes canonical resource.Encode output.
	app.Press("id:material-save")
	wantBytes, err := resource.Encode(ed.Material())
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, "materials", "metal.material.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, wantBytes) {
		t.Fatalf("saved material mismatch:\ngot:\n%s\nwant:\n%s", onDisk, wantBytes)
	}

	// Close button closes panel undoably.
	app.Press("id:material-close")
	if ed.MaterialOpen() {
		t.Fatal("material panel still open after Close")
	}
	app.Post(func() { ed.Undo() })
	app.Settle()
	if !ed.MaterialOpen() {
		t.Fatal("Undo of Close did not reopen material panel")
	}
}

// TestInspectorCreatesAndOpensMaterialAndIntegratesWithFilesAndPalette tests the Inspector's
// New Material and Edit Material buttons, File Browser opening, Command Palette, MoveFile,
// validation refusals, and Play mode refusal.
func TestInspectorCreatesAndOpensMaterialAndIntegratesWithFilesAndPalette(t *testing.T) {
	dir := writeMaterialProject(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SelectObject("hero")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1200))

	// Edit Material opens the object's currently assigned material.
	app.Press("id:inspector-material-edit")
	if !ed.MaterialOpen() || ed.MaterialRelPath() != "materials/metal.material.yaml" {
		t.Fatalf("after Edit Material: open=%v rel=%q", ed.MaterialOpen(), ed.MaterialRelPath())
	}
	// In 2D mode, preview defaults to Sprite.
	if ed.MaterialPreviewShape() != editor.MaterialPreviewSprite {
		t.Fatalf("default preview shape in 2D = %v, want Sprite", ed.MaterialPreviewShape())
	}

	// New Material creates a new canonical .material.yaml, assigns it to hero, and opens it.
	app.Press("id:inspector-material-new")
	wantNew := "materials/hero.material.yaml"
	hero, _, _ := ed.FindObject("hero")
	if hero.Material != wantNew {
		t.Fatalf("hero.Material after New Material = %q, want %q", hero.Material, wantNew)
	}
	if ed.MaterialRelPath() != wantNew {
		t.Fatalf("MaterialRelPath after New Material = %q, want %q", ed.MaterialRelPath(), wantNew)
	}
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(wantNew)))
	if err != nil {
		t.Fatalf("read created material: %v", err)
	}
	parsed, err := resource.ParseAs[*resource.Material](raw)
	if err != nil {
		t.Fatalf("parse created material: %v", err)
	}
	encoded, err := resource.Encode(parsed)
	if err != nil || !bytes.Equal(raw, encoded) {
		t.Fatalf("created material is not in resource.Encode form:\n%s", raw)
	}

	// Assign texture & verify MoveFile updates open material path and texture path.
	var opErr error
	app.Post(func() {
		if opErr = ed.SetMaterialTexture("textures/checker.png"); opErr != nil {
			return
		}
		if opErr = ed.SaveMaterial(); opErr != nil {
			return
		}
		if opErr = ed.MoveFile(wantNew, "materials/hero_renamed.material.yaml"); opErr != nil {
			return
		}
		opErr = ed.MoveFile("textures/checker.png", "textures/tiles.png")
	})
	app.Settle()
	if opErr != nil {
		t.Fatalf("texture/save/MoveFile: %v", opErr)
	}
	if ed.MaterialRelPath() != "materials/hero_renamed.material.yaml" {
		t.Fatalf("MaterialRelPath after MoveFile = %q", ed.MaterialRelPath())
	}
	if got := ed.Material().Texture; got != "textures/tiles.png" {
		t.Fatalf("Material.Texture after MoveFile = %q, want textures/tiles.png", got)
	}

	// Validation refuses bad values without mutating material or history.
	histLen := ed.History.Len()
	app.Post(func() {
		for name, err := range map[string]error{
			"bad baseColor":    ed.SetMaterialBaseColor("not-a-color"),
			"bad emissive":     ed.SetMaterialEmissive("rgb(999)"),
			"bad roughness >1": ed.SetMaterialRoughness(1.5),
			"bad roughness <0": ed.SetMaterialRoughness(-0.1),
			"bad metallic >1":  ed.SetMaterialMetallic(2.0),
			"bad opacity >1":   ed.SetMaterialOpacity(1.2),
			"bad blend":        ed.SetMaterialBlend("glow"),
			"escaping texture": ed.SetMaterialTexture("../secret.png"),
		} {
			if err == nil {
				t.Errorf("%s: expected error, got nil", name)
			}
		}
	})
	app.Settle()
	if ed.History.Len() != histLen {
		t.Fatalf("invalid edits mutated history: len=%d, want %d", ed.History.Len(), histLen)
	}

	// Command Palette & File Browser opening.
	app.Post(func() {
		opErr = ed.OpenProjectFile("materials/alt.material.yaml")
	})
	app.Settle()
	if opErr != nil {
		t.Fatalf("OpenProjectFile: %v", opErr)
	}
	if ed.MaterialRelPath() != "materials/alt.material.yaml" {
		t.Fatalf("MaterialRelPath after OpenProjectFile = %q", ed.MaterialRelPath())
	}
	app.Post(func() {
		opErr = ed.ExecuteCommand(editor.CmdMaterialEditor)
	})
	app.Settle()
	if opErr != nil {
		t.Fatalf("CmdMaterialEditor close: %v", opErr)
	}
	if ed.MaterialOpen() {
		t.Fatal("CmdMaterialEditor did not close open material panel")
	}

	// File Browser Open button opens a selected material file.
	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-materials/metal.material.yaml")
	app.Press("id:files-open")
	if !ed.MaterialOpen() || ed.MaterialRelPath() != "materials/metal.material.yaml" {
		t.Fatalf("after File Browser Open: open=%v rel=%q", ed.MaterialOpen(), ed.MaterialRelPath())
	}

	// Play mode refuses material edits with ErrPlaying.
	app.Press("id:toolbar-play")
	if !ed.Playing() {
		t.Fatal("expected Play mode active")
	}
	app.Post(func() {
		for name, err := range map[string]error{
			"SetMaterialRoughness": ed.SetMaterialRoughness(0.5),
			"SaveMaterial":         ed.SaveMaterial(),
			"CreateMaterialFile":   func() error { _, e := ed.CreateMaterialFile("materials/x.material.yaml"); return e }(),
			"CloseMaterial":        ed.CloseMaterial(),
		} {
			if !errors.Is(err, editor.ErrPlaying) {
				t.Errorf("%s while playing = %v, want ErrPlaying", name, err)
			}
		}
	})
	app.Settle()
	app.Press("id:toolbar-play")
}

func TestMaterialSymlinkContainmentAndGuards(t *testing.T) {
	// 1. Nil *Editor receiver guards.
	var nilEd *editor.Editor
	if nilEd.MaterialOpen() || nilEd.MaterialDirty() || nilEd.Material() != nil || nilEd.MaterialPath() != "" || nilEd.MaterialRelPath() != "" {
		t.Fatal("nil *Editor material accessors returned non-zero values")
	}
	nilEd.SetMaterialPreviewShape(editor.MaterialPreviewCube)
	if img, err := nilEd.MaterialPreviewImage(64); img != nil || err != nil {
		t.Fatalf("nil MaterialPreviewImage = (%v, %v), want (nil, nil)", img, err)
	}
	if err := nilEd.OpenMaterialFile("x.material.yaml"); err == nil {
		t.Fatal("expected error from nil OpenMaterialFile")
	}
	if err := nilEd.OpenMaterialProjectFile("materials/x.material.yaml"); err == nil {
		t.Fatal("expected error from nil OpenMaterialProjectFile")
	}
	if _, err := nilEd.CreateMaterialFile("materials/x.material.yaml"); err == nil {
		t.Fatal("expected error from nil CreateMaterialFile")
	}
	if err := nilEd.SaveMaterialFile("x.material.yaml"); err == nil {
		t.Fatal("expected error from nil SaveMaterialFile")
	}

	// 2. Symlink escape checks on Open, Create, Save, and Texture loading.
	outside := t.TempDir()
	secretMat := filepath.Join(outside, "secret.material.yaml")
	rawMat, err := resource.Encode(&resource.Material{
		Header: resource.NewHeader(resource.KindMaterial),
		Name:   "Outside",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretMat, rawMat, 0o644); err != nil {
		t.Fatal(err)
	}
	secretPNG := filepath.Join(outside, "secret.png")
	writeCheckerPNG(t, secretPNG)

	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "game.yaml"), []byte("version: 1\nname: SymProj\nid: dev.example.symproj\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: main\n  scenes:\n    - name: main\n      path: scenes/main.scene.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projDir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "scenes", "main.scene.yaml"), []byte("version: 2\nname: Main\nlayers:\n  - name: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projDir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "materials", "local.material.yaml"), rawMat, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretMat, filepath.Join(projDir, "materials", "link.material.yaml")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(projDir, "materials", "escape_dir")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(secretPNG, filepath.Join(projDir, "materials", "escape.png")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(projDir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if err := ed.OpenMaterialProjectFile("materials/link.material.yaml"); err == nil {
		t.Fatal("expected OpenMaterialProjectFile to refuse symlink escaping project root")
	}
	if _, err := ed.CreateMaterialFile("materials/escape_dir/new.material.yaml"); err == nil {
		t.Fatal("expected CreateMaterialFile to refuse symlinked parent directory escaping project root")
	}
	if err := ed.OpenMaterialProjectFile("materials/local.material.yaml"); err != nil {
		t.Fatalf("OpenMaterialProjectFile(local): %v", err)
	}
	if err := ed.SaveMaterialFile(""); err == nil {
		t.Fatal("expected SaveMaterialFile(\"\") to return an error")
	}
	if err := ed.SaveMaterialFile(filepath.Join(projDir, "materials", "link.material.yaml")); err == nil {
		t.Fatal("expected SaveMaterialFile to refuse symlink target escaping project root")
	}
	if err := ed.SaveMaterialFile(filepath.Join(projDir, "materials", "escape_dir", "escaped.material.yaml")); err == nil {
		t.Fatal("expected SaveMaterialFile to refuse symlinked parent directory escaping project root")
	}

	// Symlinked texture pointing outside project must be refused by 3D preview renderer.
	if err := ed.SetMaterialTexture("materials/escape.png"); err != nil {
		t.Fatalf("SetMaterialTexture: %v", err)
	}
	ed.SetMaterialPreviewShape(editor.MaterialPreviewSphere)
	if _, err := ed.MaterialPreviewImage(64); err == nil {
		t.Fatal("expected MaterialPreviewImage to refuse symlinked texture outside project root")
	}

	// 3. NaN / ±Inf / out-of-range PBR params refused cleanly without mutating state.
	for _, bad := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), -0.1, 1.5} {
		if err := ed.SetMaterialMetallic(bad); err == nil {
			t.Errorf("SetMaterialMetallic(%v) succeeded, want error", bad)
		}
		if err := ed.SetMaterialRoughness(bad); err == nil {
			t.Errorf("SetMaterialRoughness(%v) succeeded, want error", bad)
		}
		if err := ed.SetMaterialOpacity(bad); err == nil {
			t.Errorf("SetMaterialOpacity(%v) succeeded, want error", bad)
		}
	}
}

func TestMaterialCrossTabUndoIsolationDemoResetAndPreviewCache(t *testing.T) {
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "game.yaml"), []byte("version: 1\nname: TabMatProj\nid: dev.example.tabmatproj\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: a\n  scenes:\n    - name: a\n      path: scenes/a.scene.yaml\n    - name: b\n      path: scenes/b.scene.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projDir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "scenes", "a.scene.yaml"), []byte("version: 2\nname: A\nlayers:\n  - name: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "scenes", "b.scene.yaml"), []byte("version: 2\nname: B\nlayers:\n  - name: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projDir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	rA := float32(0.8)
	rawA, _ := resource.Encode(&resource.Material{
		Header:    resource.NewHeader(resource.KindMaterial),
		Name:      "MatA",
		Roughness: &rA,
	})
	rB := float32(0.3)
	rawB, _ := resource.Encode(&resource.Material{
		Header:    resource.NewHeader(resource.KindMaterial),
		Name:      "MatB",
		Roughness: &rB,
	})
	if err := os.WriteFile(filepath.Join(projDir, "materials", "a.material.yaml"), rawA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "materials", "b.material.yaml"), rawB, 0o644); err != nil {
		t.Fatal(err)
	}
	writeCheckerPNG(t, filepath.Join(projDir, "textures", "tile.png"))

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(projDir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Open and edit Material A in Tab 0.
	if err := ed.OpenMaterialProjectFile("materials/a.material.yaml"); err != nil {
		t.Fatalf("OpenMaterialProjectFile(a): %v", err)
	}
	if err := ed.SetMaterialRoughness(0.2); err != nil {
		t.Fatalf("SetMaterialRoughness(a): %v", err)
	}

	// Open Tab 1 (scene b) and open Material B in the panel.
	if err := ed.OpenProjectFile("scenes/b.scene.yaml"); err != nil {
		t.Fatalf("OpenProjectFile(b): %v", err)
	}
	if err := ed.OpenMaterialProjectFile("materials/b.material.yaml"); err != nil {
		t.Fatalf("OpenMaterialProjectFile(b): %v", err)
	}
	if err := ed.SetMaterialRoughness(0.6); err != nil {
		t.Fatalf("SetMaterialRoughness(b): %v", err)
	}

	// Switch back to Tab 0 and Undo Material A's edit: Material B (currently open in panel) must NOT be overwritten!
	if err := ed.SelectTab(0); err != nil {
		t.Fatalf("SelectTab(0): %v", err)
	}
	if !ed.Undo() {
		t.Fatal("Undo in Tab 0 failed")
	}
	if ed.MaterialRelPath() != "materials/b.material.yaml" {
		t.Fatalf("expected Material B to stay open, got %q", ed.MaterialRelPath())
	}
	if got := ed.Material().EffectiveRoughness(); math.Abs(float64(got)-0.6) > 1e-4 {
		t.Fatalf("Material B roughness was corrupted by Tab 0 undo: got %v, want 0.6", got)
	}

	// 3D preview caching returns the exact same cached *image.RGBA on repeated calls.
	ed.SetMaterialPreviewShape(editor.MaterialPreviewSphere)
	img1, err := ed.MaterialPreviewImage(64)
	if err != nil || img1 == nil {
		t.Fatalf("MaterialPreviewImage 1: img=%v err=%v", img1, err)
	}
	img2, err := ed.MaterialPreviewImage(64)
	if err != nil || img2 != img1 {
		t.Fatalf("expected second MaterialPreviewImage call to return cached pointer (img1=%p img2=%p err=%v)", img1, img2, err)
	}

	// 2D sprite texture preview caches without re-reading disk on every draw.
	if err := ed.SetMaterialTexture("textures/tile.png"); err != nil {
		t.Fatalf("SetMaterialTexture: %v", err)
	}
	ed.SetMaterialPreviewShape(editor.MaterialPreviewSprite)
	app := mountEditor(t, ed)
	app.Settle()
	// Delete the texture on disk; a subsequent redraw must still work cleanly from cache without crashing.
	if err := os.Remove(filepath.Join(projDir, "textures", "tile.png")); err != nil {
		t.Fatal(err)
	}
	app.Post(func() {
		// Trigger an unrelated revision; preview should reuse cached texture.
		ed.SelectObject("")
	})
	app.Settle()

	// OpenDemo resets e.material cleanly so old project material does not leak into Demo mode.
	app.Post(func() {
		if err := ed.OpenDemo(); err != nil {
			t.Errorf("OpenDemo: %v", err)
		}
	})
	app.Settle()
	if ed.MaterialOpen() || ed.MaterialDirty() {
		t.Fatalf("expected OpenDemo to reset material state: open=%v dirty=%v", ed.MaterialOpen(), ed.MaterialDirty())
	}
}
