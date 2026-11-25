package scenemodel_test

import (
	"bytes"
	"image"
	"math"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

func renderSceneWithCamera(t *testing.T, built *scenemodel.Built, w, h int, cam *scene3d.Camera) *image.RGBA {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	r, err := scenemodel.NewRenderer(nil, dev, built, scenemodel.RendererOptions{Label: "env-test"})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if _, err := r.PrepareScene(cam); err != nil {
		t.Fatalf("PrepareScene: %v", err)
	}
	enc := dev.BeginEncoder()
	r.RecordShadows(enc)
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "env-test",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: r.ClearColor(core.RGB(0x10141c))}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	r.DrawSky(pass)
	r.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	src := sr.Image()
	img := image.NewRGBA(src.Bounds())
	copy(img.Pix, src.Pix)
	return img
}

func makeTinyHDRBytes() []byte {
	var buf bytes.Buffer
	buf.WriteString("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 2 +X 4\n")
	// Top row (bright sky blue-white)
	for range 4 {
		buf.Write([]byte{140, 180, 250, 130})
	}
	// Bottom row (warm ground)
	for range 4 {
		buf.Write([]byte{180, 140, 100, 128})
	}
	return buf.Bytes()
}

func TestBuildAndRenderLightsSkyFogTonemapAndCamera(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Lit Scene",
		Environment: &sceneformat.EnvironmentDoc{
			Sky: &sceneformat.SkyDoc{
				Kind:    sceneformat.SkyGradient,
				Zenith:  "#1a4fa0",
				Horizon: "#b8cde0",
				Ground:  "#2b2622",
			},
			AmbientColor:     "#203040",
			AmbientIntensity: sceneformat.Float32(0.2),
			IBLIntensity:     sceneformat.Float32(0.4),
			Fog: &sceneformat.FogDoc{
				Mode:  sceneformat.FogLinear,
				Color: "#d0a080",
				Start: 4,
				End:   12,
			},
			Tonemapping: sceneformat.TonemapACES,
			Exposure:    sceneformat.Float32(1.1),
		},
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID:    "sun",
					RotX:  -0.9,
					RotY:  0.4,
					Light: &sceneformat.LightDoc{Kind: sceneformat.LightDirectional, Color: "#fff4d8", Intensity: sceneformat.Float32(3), Shadows: true},
				},
				{
					ID:    "lamp",
					X:     -1.5,
					Y:     1.2,
					PosZ:  1.2,
					Light: &sceneformat.LightDoc{Kind: sceneformat.LightPoint, Color: "#ff5522", Intensity: sceneformat.Float32(5), Range: 6},
				},
				{
					ID:   "spot",
					X:    1.5,
					Y:    2.5,
					PosZ: 1.0,
					RotX: -1.1,
					Light: &sceneformat.LightDoc{
						Kind:      sceneformat.LightSpot,
						Color:     "#44ccff",
						Intensity: sceneformat.Float32(6),
						Range:     8,
						InnerCone: 12,
						OuterCone: sceneformat.Float32(30),
					},
				},
				{
					ID:     "floor",
					ScaleX: sceneformat.Float32(16),
					ScaleZ: sceneformat.Float32(16),
					Mesh:   sceneformat.MeshPlane,
				},
				{
					ID:     "near_box",
					Y:      0.5,
					PosZ:   0,
					Mesh:   sceneformat.MeshBox,
				},
				{
					ID:     "far_box",
					X:      2.2,
					Y:      0.5,
					PosZ:   -7,
					Mesh:   sceneformat.MeshBox,
				},
				{
					ID:   "cam_ortho",
					Y:    8,
					RotX: -float32(math.Pi / 2),
					Camera: &sceneformat.Camera3DDoc{
						Projection:  sceneformat.ProjectionOrthographic,
						OrthoHeight: sceneformat.Float32(10),
					},
				},
				{
					ID:   "cam_main",
					X:    0,
					Y:    2.2,
					PosZ: 4.5,
					RotX: -0.35,
					Camera: &sceneformat.Camera3DDoc{
						Projection: sceneformat.ProjectionPerspective,
						FOV:        sceneformat.Float32(50),
						Near:       sceneformat.Float32(0.1),
						Far:        sceneformat.Float32(50),
						Current:    true,
					},
				},
			},
		}},
	}

	world := scene3d.NewScene()
	built, err := scenemodel.Build(doc, world.Root(), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !built.HasSceneLighting() {
		t.Fatal("HasSceneLighting = false, want true")
	}
	if len(built.Lights) != 3 || len(built.Cameras) != 2 {
		t.Fatalf("built %d lights and %d cameras, want 3 and 2", len(built.Lights), len(built.Cameras))
	}
	rig := built.LightRig()
	if len(rig.Directional) != 1 || len(rig.Point) != 1 || len(rig.Spot) != 1 {
		t.Fatalf("LightRig counts = %d/%d/%d, want 1/1/1", len(rig.Directional), len(rig.Point), len(rig.Spot))
	}
	camObj, ok := built.ActiveCameraObject()
	if !ok || camObj.Object != "cam_main" {
		t.Fatalf("ActiveCameraObject = %v, %v, want cam_main", camObj, ok)
	}
	const w, h = 128, 96
	cam, ok := built.ActiveCamera(float32(w) / h)
	if !ok || cam == nil {
		t.Fatal("ActiveCamera returned none")
	}

	img := renderSceneWithCamera(t, built, w, h, cam)
	// Top of frame should show blue-tinted gradient sky.
	top := img.RGBAAt(w/2, 2)
	if top.B <= top.R {
		t.Errorf("top sky pixel %v should be bluer than red", top)
	}

	// Render again without fog and verify far_box pixel shifts toward fog color when fog is on.
	docNoFog := doc.Clone()
	docNoFog.Environment.Fog = nil
	worldNoFog := scene3d.NewScene()
	builtNoFog, err := scenemodel.Build(docNoFog, worldNoFog.Root(), nil)
	if err != nil {
		t.Fatalf("Build no fog: %v", err)
	}
	camNoFog, _ := builtNoFog.ActiveCamera(float32(w) / h)
	imgNoFog := renderSceneWithCamera(t, builtNoFog, w, h, camNoFog)

	clip := cam.ViewProj().MulVec4(math3d.Vec3{X: 2.2, Y: 0.5, Z: -6.5}.Vec4(1))
	fx := int((clip.X/clip.W + 1) / 2 * w)
	fy := int((1 - clip.Y/clip.W) / 2 * h)
	withFog := img.RGBAAt(fx, fy)
	withoutFog := imgNoFog.RGBAAt(fx, fy)
	if withFog == withoutFog {
		t.Errorf("far box pixel (%d,%d) identical with and without fog: %v", fx, fy, withFog)
	}
}

func TestBuildAndRenderHDRSkyAndIBL(t *testing.T) {
	fsys := fstest.MapFS{
		"env/studio.hdr": {Data: makeTinyHDRBytes()},
	}
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Environment: &sceneformat.EnvironmentDoc{
			Sky: &sceneformat.SkyDoc{
				Kind: sceneformat.SkyHDR,
				HDR:  "env/studio.hdr",
			},
			AmbientIntensity: sceneformat.Float32(0),
			Tonemapping:      sceneformat.TonemapReinhard,
			Exposure:         sceneformat.Float32(1.0),
		},
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{ID: "sphere", Mesh: sceneformat.MeshSphere},
				{ID: "cam", PosZ: 3, Camera: &sceneformat.Camera3DDoc{Current: true}},
			},
		}},
	}
	world := scene3d.NewScene()
	built, err := scenemodel.Build(doc, world.Root(), scenemodel.NewAssets(fsys))
	if err != nil {
		t.Fatalf("Build HDR: %v", err)
	}
	if built.Environment.SkyCubemap == nil || built.Environment.IBLPrefilter == nil || built.Environment.IBLIntensity != 1 {
		t.Fatalf("HDR sky did not build cubemap and IBL: %+v", built.Environment)
	}
	cam, _ := built.ActiveCamera(4.0 / 3.0)
	img := renderSceneWithCamera(t, built, 64, 48, cam)
	// Center of sphere should be lit by IBL alone (ambient is 0 and there are no punctual lights).
	center := img.RGBAAt(32, 24)
	if center.R == 0 && center.G == 0 && center.B == 0 {
		t.Errorf("sphere center is black %v, expected HDR IBL illumination", center)
	}
}
