package editor

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/gpu"
)

func sixtyFourLampCorridorDoc() *sceneformat.SceneDoc {
	palette := []string{"#ff5533", "#ffaa22", "#33dd88", "#33aaff", "#bb66ff", "#ff44aa"}
	objects := []sceneformat.ObjectDoc{
		{
			ID:     "floor",
			X:      0,
			Y:      0,
			PosZ:   -32,
			ScaleX: sceneformat.Float32(20),
			ScaleZ: sceneformat.Float32(72),
			Mesh:   sceneformat.MeshPlane,
		},
		{
			ID:   "cam_main",
			X:    0,
			Y:    5.5,
			PosZ: 6.5,
			RotX: -0.32,
			Camera: &sceneformat.Camera3DDoc{
				Projection: sceneformat.ProjectionPerspective,
				FOV:        sceneformat.Float32(55),
				Near:       sceneformat.Float32(0.1),
				Far:        sceneformat.Float32(120),
				Current:    true,
			},
		},
	}
	// Place 64 point lights in a row from far (-63) to near (0) so document order puts
	// the nearest lamps last (indices 48..63) — exposing the old 16-light file-order truncation.
	for i := range 64 {
		z := float32(-63 + i)
		col := palette[i%len(palette)]
		objects = append(objects,
			sceneformat.ObjectDoc{
				ID:   fmt.Sprintf("lamp_%02d", i),
				X:    0,
				Y:    1.1,
				PosZ: z,
				Light: &sceneformat.LightDoc{
					Kind:      sceneformat.LightPoint,
					Color:     col,
					Intensity: sceneformat.Float32(5),
					Range:     3.0,
				},
			},
			sceneformat.ObjectDoc{
				ID:     fmt.Sprintf("post_%02d", i),
				X:      float32(1.4) * float32(1-2*(i%2)),
				Y:      0.5,
				PosZ:   z,
				ScaleX: sceneformat.Float32(0.3),
				ScaleY: sceneformat.Float32(1.0),
				ScaleZ: sceneformat.Float32(0.3),
				Mesh:   sceneformat.MeshBox,
			},
		)
	}
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "64 Lamp Corridor",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#06080e",
		Environment: &sceneformat.EnvironmentDoc{
			Sky:              &sceneformat.SkyDoc{Kind: sceneformat.SkyColor, Color: "#06080e"},
			AmbientColor:     "#101420",
			AmbientIntensity: sceneformat.Float32(0.02),
			IBLIntensity:     sceneformat.Float32(0),
			Tonemapping:      sceneformat.TonemapACES,
			Exposure:         sceneformat.Float32(1.0),
		},
		Layers: []sceneformat.LayerDoc{{
			Name:    "world",
			Objects: objects,
		}},
	}
}

func TestViewport3D64PointLightsClusteredParity(t *testing.T) {
	doc := sixtyFourLampCorridorDoc()
	raw, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	gameImg := renderGameFirstFrame(t, doc, nil, gpuViewWidth, gpuViewHeight)

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ed.OpenYAML(raw); err != nil {
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
	if !samePixels(gameImg, editorImg) {
		t.Fatal("editor 3D viewport with 64 clustered point lights differs from game first frame")
	}

	cam := ed.EffectiveCamera3D(gpuViewWidth, gpuViewHeight)
	for _, z := range []float32{0, -5, -15, -30} {
		screen, _, vis := cam.WorldToScreen(math3d.Vec3{X: 0, Y: 0, Z: z}, gpuViewWidth, gpuViewHeight)
		if !vis {
			t.Fatalf("corridor floor at Z=%.1f not visible", z)
		}
		c := editorImg.RGBAAt(int(screen.X), int(screen.Y))
		if max(c.R, c.G, c.B) < 35 {
			t.Errorf("editor viewport floor under lamp Z=%.1f at (%d,%d) is dark %v, want visibly lit", z, int(screen.X), int(screen.Y), c)
		}
	}
}

func TestCapture64LampViewportScreenshot(t *testing.T) {
	out := os.Getenv("CAPTURE_64LAMPS_OUT")
	if out == "" {
		t.Skip("set CAPTURE_64LAMPS_OUT to capture")
	}
	doc := sixtyFourLampCorridorDoc()
	raw, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ed.OpenYAML(raw); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	ed.SetViewMode(ViewMode3D)
	if err := ed.SetLookThroughCamera3D(true); err != nil {
		t.Fatalf("SetLookThroughCamera3D: %v", err)
	}
	const w, h = 640, 360
	sr := gpu.NewSoftwareRenderer(w, h, viewport3DDepthFormat)
	defer sr.Close()
	vr, err := newViewport3DRenderer(ed, sr.Device())
	if err != nil {
		t.Fatalf("newViewport3DRenderer: %v", err)
	}
	defer vr.close()
	enc := sr.Device().BeginEncoder()
	if _, err := vr.render(enc, w, h); err != nil {
		t.Fatalf("render: %v", err)
	}
	sr.Device().Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	src := sr.Image()
	img := image.NewRGBA(src.Bounds())
	copy(img.Pix, src.Pix)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gametest.WritePNG(out, img); err != nil {
		t.Fatal(err)
	}
}
