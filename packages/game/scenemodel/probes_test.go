package scenemodel_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/gpu"
)

func TestBakeAndShadeWithReflectionProbeAndLightProbeGrid(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "probes"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Emissive red wall on +X, emissive blue wall on -X, and a glossy chrome sphere at center.
	redMat := "version: 1\ntype: material\nbaseColor: '#ff2020'\nemissive: '#ff1818'\nroughness: 0.9\n"
	blueMat := "version: 1\ntype: material\nbaseColor: '#2040ff'\nemissive: '#1830ff'\nroughness: 0.9\n"
	chromeMat := "version: 1\ntype: material\nbaseColor: '#ffffff'\nmetallic: 0.85\nroughness: 0.15\n"
	for name, body := range map[string]string{
		"red.material.yaml":    redMat,
		"blue.material.yaml":   blueMat,
		"chrome.material.yaml": chromeMat,
	} {
		if err := os.WriteFile(filepath.Join(dir, "materials", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Probe Bake Test",
		Environment: &sceneformat.EnvironmentDoc{
			Sky:              &sceneformat.SkyDoc{Kind: sceneformat.SkyColor, Color: "#08080c"},
			AmbientColor:     "#000000",
			AmbientIntensity: sceneformat.Float32(0),
			Tonemapping:      sceneformat.TonemapACES,
			Exposure:         sceneformat.Float32(1),
		},
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID:       "red_wall",
					X:        3.0,
					Y:        1.0,
					PosZ:     0,
					ScaleX:   sceneformat.Float32(0.4),
					ScaleY:   sceneformat.Float32(4.0),
					ScaleZ:   sceneformat.Float32(6.0),
					Mesh:     sceneformat.MeshBox,
					Material: "materials/red.material.yaml",
				},
				{
					ID:       "blue_wall",
					X:        -3.0,
					Y:        1.0,
					PosZ:     0,
					ScaleX:   sceneformat.Float32(0.4),
					ScaleY:   sceneformat.Float32(4.0),
					ScaleZ:   sceneformat.Float32(6.0),
					Mesh:     sceneformat.MeshBox,
					Material: "materials/blue.material.yaml",
				},
				{
					ID:       "chrome_orb",
					X:        0,
					Y:        1.0,
					PosZ:     0,
					Mesh:     sceneformat.MeshSphere,
					Material: "materials/chrome.material.yaml",
				},
				{
					ID:   "refl_probe",
					X:    0,
					Y:    1.0,
					PosZ: 0,
					ReflectionProbe: &sceneformat.ReflectionProbeDoc{
						Size:          &[3]float32{8, 4, 8},
						BlendDistance: 0.5,
						Intensity:     sceneformat.Float32(1.2),
						BoxProjection: sceneformat.Bool(true),
						Resolution:    32,
					},
				},
				{
					ID:   "light_grid",
					X:    0,
					Y:    1.0,
					PosZ: 0,
					LightProbeGrid: &sceneformat.LightProbeGridDoc{
						Size:      &[3]float32{5, 2, 4},
						Counts:    &[3]int{2, 1, 2},
						Intensity: sceneformat.Float32(1.0),
					},
				},
			},
		}},
	}

	assets := scenemodel.NewAssets(os.DirFS(dir))
	world := scene3d.NewScene()
	built, err := scenemodel.Build(doc, world.Root(), assets)
	if err != nil {
		t.Fatalf("Build unbaked: %v", err)
	}
	if len(built.ReflectionProbes) != 1 || len(built.LightProbeGrids) != 1 {
		t.Fatalf("probes = (%d, %d), want (1, 1)", len(built.ReflectionProbes), len(built.LightProbeGrids))
	}

	// Bake reflection probe cubemap -> HDR file.
	cm, err := scenemodel.BakeReflectionProbe(built, built.ReflectionProbes[0], scenemodel.ProbeBakeOptions{Size: 16})
	if err != nil {
		t.Fatalf("BakeReflectionProbe: %v", err)
	}
	posX := cm.At(gpu.CubeFacePosX, 8, 8)
	negX := cm.At(gpu.CubeFaceNegX, 8, 8)
	if posX[0] <= posX[2]*2 {
		t.Errorf("+X cubemap face = %v, want strong red from red_wall", posX)
	}
	if negX[2] <= negX[0]*2 {
		t.Errorf("-X cubemap face = %v, want strong blue from blue_wall", negX)
	}
	var hdrBuf bytes.Buffer
	if err := render3d.EncodeCubemapHDR(&hdrBuf, cm); err != nil {
		t.Fatalf("EncodeCubemapHDR: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probes", "room.hdr"), hdrBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Bake light probe grid -> YAML file.
	lpg, err := scenemodel.BakeLightProbeGrid(built, built.LightProbeGrids[0], scenemodel.ProbeBakeOptions{Size: 8})
	if err != nil {
		t.Fatalf("BakeLightProbeGrid: %v", err)
	}
	shCenter := lpg.Sample(math3d.Vec3{X: 0, Y: 1, Z: 0})
	irrPosX := shCenter.Evaluate(math3d.Vec3{X: 1, Y: 0, Z: 0})
	irrNegX := shCenter.Evaluate(math3d.Vec3{X: -1, Y: 0, Z: 0})
	if irrPosX[0] <= irrPosX[2] {
		t.Errorf("SH irradiance along +X = %v, want red > blue", irrPosX)
	}
	if irrNegX[2] <= irrNegX[0] {
		t.Errorf("SH irradiance along -X = %v, want blue > red", irrNegX)
	}
	lpgDoc, err := render3d.LightProbeGridToDoc(lpg)
	if err != nil {
		t.Fatalf("LightProbeGridToDoc: %v", err)
	}
	lpgYAML, err := render3d.EncodeLightProbeGrid(lpgDoc)
	if err != nil {
		t.Fatalf("EncodeLightProbeGrid: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probes", "room.probes.yaml"), lpgYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	// Link baked files and rebuild: verify Renderer shades differently with baked probes than without.
	docBaked := doc.Clone()
	docBaked.Layers[0].Objects[3].ReflectionProbe.Cubemap = "probes/room.hdr"
	docBaked.Layers[0].Objects[4].LightProbeGrid.Data = "probes/room.probes.yaml"

	renderOrb := func(scene *sceneformat.SceneDoc) [3]uint8 {
		w := scene3d.NewScene()
		b, err := scenemodel.Build(scene, w.Root(), scenemodel.NewAssets(os.DirFS(dir)))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatDepth24Plus)
		defer sr.Close()
		r, err := scenemodel.NewRenderer(nil, sr.Device(), b, scenemodel.RendererOptions{DepthFormat: gpu.FormatDepth24Plus})
		if err != nil {
			t.Fatalf("NewRenderer: %v", err)
		}
		defer r.Close()
		cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 1.0, Z: 2.5}, math3d.DegToRad(50), 1, 0.1, 50)
		cam.LookAt(math3d.Vec3{X: 0, Y: 1.0, Z: 0}, math3d.Vec3Up())
		if _, err := r.PrepareScene(cam); err != nil {
			t.Fatalf("PrepareScene: %v", err)
		}
		enc := sr.Device().BeginEncoder()
		r.RecordShadows(enc)
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		r.DrawSky(pass)
		r.Draw(pass)
		pass.End()
		sr.Device().Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("GPU error: %v", err)
		}
		c := sr.Image().RGBAAt(32, 32)
		return [3]uint8{c.R, c.G, c.B}
	}

	unbakedPix := renderOrb(doc)
	bakedPix := renderOrb(docBaked)
	if unbakedPix == bakedPix {
		t.Fatalf("baked probes did not affect sphere shading: unbaked=%v baked=%v", unbakedPix, bakedPix)
	}
	if bakedPix[0] == 0 && bakedPix[1] == 0 && bakedPix[2] == 0 {
		t.Fatalf("baked sphere pixel is black: %v", bakedPix)
	}
}
