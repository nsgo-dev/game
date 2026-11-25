package editor

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

func lightingParitySceneDoc() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Lighting Parity",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Environment: &sceneformat.EnvironmentDoc{
			Sky: &sceneformat.SkyDoc{
				Kind:    sceneformat.SkyGradient,
				Zenith:  "#2b5899",
				Horizon: "#b8d0e8",
				Ground:  "#302a24",
			},
			AmbientColor:     "#c8d8f0",
			AmbientIntensity: sceneformat.Float32(0.28),
			IBLIntensity:     sceneformat.Float32(0.4),
			Fog: &sceneformat.FogDoc{
				Mode:       sceneformat.FogLinear,
				Color:      "#b8d0e8",
				Start:      4,
				End:        14,
				MaxOpacity: sceneformat.Float32(0.85),
			},
			Tonemapping: sceneformat.TonemapACES,
			Exposure:    sceneformat.Float32(1.1),
		},
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID:   "cam_main",
					Name: "Main Camera",
					X:    0,
					Y:    2.2,
					PosZ: 6.2,
					RotX: -0.22,
					Camera: &sceneformat.Camera3DDoc{
						Projection: sceneformat.ProjectionPerspective,
						FOV:        sceneformat.Float32(55),
						Near:       sceneformat.Float32(0.1),
						Far:        sceneformat.Float32(60),
						Current:    true,
					},
				},
				{
					ID:   "sun",
					Name: "Sun",
					Y:    6,
					RotX: -0.95,
					RotY: 0.45,
					Light: &sceneformat.LightDoc{
						Kind:      sceneformat.LightDirectional,
						Color:     "#fff5e0",
						Intensity: sceneformat.Float32(3.2),
						Shadows:   true,
					},
				},
				{
					ID:   "warm_lamp",
					Name: "Warm Lamp",
					X:    -2.2,
					Y:    1.6,
					PosZ: 1.5,
					Light: &sceneformat.LightDoc{
						Kind:      sceneformat.LightPoint,
						Color:     "#ff8844",
						Intensity: sceneformat.Float32(8),
						Range:     8,
					},
				},
				{
					ID:   "cool_spot",
					Name: "Cool Spot",
					X:    2.2,
					Y:    3.2,
					PosZ: 2.0,
					RotX: -0.95,
					RotY: 0.35,
					Light: &sceneformat.LightDoc{
						Kind:      sceneformat.LightSpot,
						Color:     "#44ccff",
						Intensity: sceneformat.Float32(10),
						Range:     12,
						InnerCone: 14,
						OuterCone: sceneformat.Float32(32),
					},
				},
				{
					ID:     "floor",
					ScaleX: sceneformat.Float32(20),
					ScaleZ: sceneformat.Float32(20),
					Mesh:   sceneformat.MeshPlane,
				},
				{
					ID:   "near_box",
					X:    -0.9,
					Y:    0.55,
					PosZ: 0.4,
					Mesh: sceneformat.MeshBox,
				},
				{
					ID:   "mid_sphere",
					X:    1.1,
					Y:    0.6,
					PosZ: -1.0,
					Mesh: sceneformat.MeshSphere,
				},
				{
					ID:   "far_pillar",
					X:    0.2,
					Y:    1.1,
					PosZ: -6.5,
					Mesh: sceneformat.MeshCylinder,
				},
			},
		}},
	}
}

func renderGameFirstFrame(t *testing.T, doc *sceneformat.SceneDoc, assets scenemodel.ModelResolver, w, h int) *image.RGBA {
	t.Helper()
	world := scene3d.NewScene()
	built, err := scenemodel.Build(doc, world.Root(), assets)
	if err != nil {
		t.Fatalf("scenemodel.Build: %v", err)
	}
	cam, ok := built.ActiveCamera(float32(w) / float32(h))
	if !ok {
		t.Fatal("scene has no active 3D camera")
	}
	sr := gpu.NewSoftwareRenderer(w, h, viewport3DDepthFormat)
	defer sr.Close()
	r, err := scenemodel.NewRenderer(nil, sr.Device(), built, scenemodel.RendererOptions{
		Label:       "game.first_frame",
		DepthFormat: viewport3DDepthFormat,
	})
	if err != nil {
		t.Fatalf("scenemodel.NewRenderer: %v", err)
	}
	defer r.Close()
	if _, err := r.PrepareScene(cam); err != nil {
		t.Fatalf("PrepareScene: %v", err)
	}
	enc := sr.Device().BeginEncoder()
	r.RecordShadows(enc)
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "game.first_frame",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: r.ClearColor(edFallbackBG(doc))}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	r.DrawSky(pass)
	r.Draw(pass)
	pass.End()
	sr.Device().Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	src := sr.Image()
	out := image.NewRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	return out
}

func edFallbackBG(doc *sceneformat.SceneDoc) core.Color {
	ed := &Editor{Doc: doc}
	return ed.background3D()
}

// TestViewport3DSceneLightingParityAndLookThroughCamera proves that a scene's lights, sky, fog,
// tonemapping, and exposure render identically in the editor 3D viewport and in scenemodel/render3d,
// and that looking through the scene's 3D camera matches the game's first frame pixel for pixel.
func TestViewport3DSceneLightingParityAndLookThroughCamera(t *testing.T) {
	doc := lightingParitySceneDoc()
	yamlBytes, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Render the game's first frame directly through scenemodel + render3d.
	gameImg := renderGameFirstFrame(t, doc, nil, gpuViewWidth, gpuViewHeight)

	// Open the same scene in the editor and look through the scene's 3D camera.
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ed.OpenYAML(yamlBytes); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	ed.SetViewMode(ViewMode3D)
	if err := ed.SetLookThroughCamera3D(true); err != nil {
		t.Fatalf("SetLookThroughCamera3D: %v", err)
	}

	v := newSoftwareViewport(t, ed)
	drew, editorImg := v.frame()
	if !drew {
		t.Fatal("editor viewport recorded no frame")
	}

	// 1. Exact pixel-for-pixel parity between editor viewport (looking through scene camera) and game's first frame.
	if !samePixels(gameImg, editorImg) {
		t.Fatal("editor 3D viewport looking through scene camera does not match the game's first frame")
	}

	// 2. Compare against golden reference (UPDATE_GOLDEN=1 writes it).
	assertViewportReference(t, gametest.GoldenPath("", "viewport3d_scene_lighting"), editorImg)

	// 3. Sky gradient shows blue at the top.
	if top := editorImg.RGBAAt(gpuViewWidth/2, 2); top.B <= top.R {
		t.Errorf("top sky pixel = %v, want blue zenith", top)
	}

	// 4. Editing a light or environment updates pixels immediately without breaking parity, and Undo restores exact pixels.
	sun, _, _ := ed.FindObject("sun")
	dimSun := sun.Light.Clone()
	dimSun.Intensity = sceneformat.Float32(0.1)
	if err := ed.SetObjectLight("sun", dimSun); err != nil {
		t.Fatalf("SetObjectLight: %v", err)
	}
	_, dimmedImg := v.frame()
	if samePixels(editorImg, dimmedImg) {
		t.Fatal("dimming directional light did not change viewport pixels")
	}
	ed.Undo()
	_, restoredImg := v.frame()
	if !samePixels(editorImg, restoredImg) {
		t.Fatal("Undo after SetObjectLight did not restore original pixels")
	}

	// 5. Editing environment exposure updates pixels and Undo restores them.
	brightEnv := ed.Doc.Environment.Clone()
	brightEnv.Exposure = sceneformat.Float32(2.4)
	if err := ed.SetEnvironment(brightEnv); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}
	_, brightImg := v.frame()
	if samePixels(editorImg, brightImg) {
		t.Fatal("changing exposure did not change viewport pixels")
	}
	ed.Undo()
	_, afterEnvUndo := v.frame()
	if !samePixels(editorImg, afterEnvUndo) {
		t.Fatal("Undo after SetEnvironment did not restore original pixels")
	}
}

// TestViewport3DLoadsHDRSkyFromProject proves the editor resolves HDR sky files from the open project
// and renders the same frame as scenemodel + render3d.
func TestViewport3DLoadsHDRSkyFromProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "env"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	hdr := &render3d.HDRImage{
		Width:  4,
		Height: 2,
		RGB: [][3]float32{
			{0.2, 0.5, 1.4}, {0.2, 0.5, 1.4}, {0.2, 0.5, 1.4}, {0.2, 0.5, 1.4},
			{0.9, 0.6, 0.3}, {0.9, 0.6, 0.3}, {0.9, 0.6, 0.3}, {0.9, 0.6, 0.3},
		},
	}
	var hdrBuf bytes.Buffer
	if err := render3d.EncodeHDR(&hdrBuf, hdr); err != nil {
		t.Fatalf("EncodeHDR: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "env", "sky.hdr"), hdrBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "HDR Studio",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#000000",
		Environment: &sceneformat.EnvironmentDoc{
			Sky:         &sceneformat.SkyDoc{Kind: sceneformat.SkyHDR, HDR: "env/sky.hdr"},
			Tonemapping: sceneformat.TonemapACES,
			Exposure:    sceneformat.Float32(1.0),
		},
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID:   "cam",
					Y:    0.5,
					PosZ: 3.5,
					Camera: &sceneformat.Camera3DDoc{
						Projection: sceneformat.ProjectionPerspective,
						FOV:        sceneformat.Float32(55),
						Current:    true,
					},
				},
				{
					ID:   "orb",
					Mesh: sceneformat.MeshSphere,
				},
			},
		}},
	}
	sceneBytes, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "hdr.scene.yaml"), sceneBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	gameYAML := "version: 1\nname: HDRTest\nid: dev.example.hdrtest\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: hdr\n  scenes:\n    - name: hdr\n      path: scenes/hdr.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if hdrs := ed.ProjectHDRs(); len(hdrs) != 1 || hdrs[0] != "env/sky.hdr" {
		t.Fatalf("ProjectHDRs() = %v, want [env/sky.hdr]", hdrs)
	}
	ed.SetViewMode(ViewMode3D)
	if err := ed.SetLookThroughCamera3D(true); err != nil {
		t.Fatalf("SetLookThroughCamera3D: %v", err)
	}

	assets := scenemodel.NewAssets(os.DirFS(dir))
	gameImg := renderGameFirstFrame(t, doc, assets, gpuViewWidth, gpuViewHeight)
	v := newSoftwareViewport(t, ed)
	_, editorImg := v.frame()
	if !samePixels(gameImg, editorImg) {
		t.Fatal("HDR sky editor viewport frame differs from game first frame")
	}
}
