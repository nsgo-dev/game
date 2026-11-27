package scenemodel_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// sixtyFourLampRowDoc returns a 3D scene with 64 identical point lights placed in a row along Z from -63 to 0
// in document order, so the 16 lights nearest the camera (at Z = 6.5) come LAST in the file (lamp_48..lamp_63).
func sixtyFourLampRowDoc() *sceneformat.SceneDoc {
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
	for i := range 64 {
		z := float32(-63 + i)
		objects = append(objects, sceneformat.ObjectDoc{
			ID:   fmt.Sprintf("lamp_%02d", i),
			X:    0,
			Y:    1.1,
			PosZ: z,
			Light: &sceneformat.LightDoc{
				Kind:      sceneformat.LightPoint,
				Color:     "#ffe2b8",
				Intensity: sceneformat.Float32(6),
				Range:     3.2,
			},
		})
	}
	return &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "64 Lamp Row",
		Environment: &sceneformat.EnvironmentDoc{
			Sky:              &sceneformat.SkyDoc{Kind: sceneformat.SkyColor, Color: "#04060a"},
			AmbientColor:     "#080a10",
			AmbientIntensity: sceneformat.Float32(0.01),
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

func TestSceneFile64PointLightsInARowFullAndNoGPUCompute(t *testing.T) {
	// Round-trip through YAML so the test exercises the real scene-file path.
	raw, err := sceneformat.Encode(sixtyFourLampRowDoc())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	const (
		viewW = 128
		viewH = 96
	)

	profiles := []struct {
		name        string
		opts        []nsgotest.Option
		wantCluster bool
	}{
		{name: "full", wantCluster: true},
		{name: "no-gpu-compute", opts: []nsgotest.Option{nsgotest.NoGPUCompute()}, wantCluster: false},
	}

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			world := scene3d.NewScene()
			built, err := scenemodel.Build(doc, world.Root(), nil)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			cam, ok := built.ActiveCamera(float32(viewW) / float32(viewH))
			if !ok {
				t.Fatal("ActiveCamera returned false")
			}

			var r *scenemodel.Renderer
			opts := append([]nsgotest.Option{nsgotest.Size(viewW, viewH), nsgotest.ManualFrames()}, prof.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "lamps-view",
					Style:       core.Style{Width: core.Pt(viewW), Height: core.Pt(viewH)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						r, err = scenemodel.NewRenderer(cx, dev, built, scenemodel.RendererOptions{
							Label:       "lamps-" + prof.name,
							DepthFormat: gpu.FormatDepth24Plus,
						})
						if err != nil {
							t.Fatalf("NewRenderer: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						if r == nil {
							return
						}
						fw, fh := frame.Size()
						r.SetViewport(float32(fw), float32(fh))
						if _, err := r.PrepareScene(cam); err != nil {
							t.Fatalf("PrepareScene: %v", err)
						}
						r.RecordShadows(frame)
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{
							Label: "lamps",
							ColorAttachments: []gpu.ColorAttachment{{
								LoadOp:     gpu.LoadClear,
								StoreOp:    gpu.StoreStore,
								ClearColor: r.ClearColor(core.RGB(0x04060a)),
							}},
							DepthStencil: &gpu.DepthStencilAttachment{
								DepthLoadOp:  gpu.LoadClear,
								DepthStoreOp: gpu.StoreDiscard,
								ClearDepth:   1,
							},
						})
						r.DrawSky(pass)
						r.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)

			sys := r.ClusteredLights()
			if sys == nil {
				t.Fatal("ClusteredLights() is nil")
			}
			if sys.IsClustered() != prof.wantCluster || sys.IsFallback() == prof.wantCluster {
				t.Fatalf("mode mismatch: IsClustered=%v IsFallback=%v, wantCluster=%v", sys.IsClustered(), sys.IsFallback(), prof.wantCluster)
			}

			stats := sys.LastStats()
			if stats.InputLights != 64 {
				t.Fatalf("InputLights = %d, want 64", stats.InputLights)
			}
			if prof.wantCluster {
				if stats.VisibleLights != 64 || stats.TruncatedLights != 0 {
					t.Fatalf("clustered stats = %+v, want all 64 visible and 0 truncated", stats)
				}
			} else {
				if stats.VisibleLights != render3d.DefaultFallbackMaxLights {
					t.Fatalf("fallback VisibleLights = %d, want %d (stats=%+v)", stats.VisibleLights, render3d.DefaultFallbackMaxLights, stats)
				}
				if stats.TruncatedLights != 64-render3d.DefaultFallbackMaxLights {
					t.Fatalf("fallback TruncatedLights = %d, want %d (stats=%+v)", stats.TruncatedLights, 64-render3d.DefaultFallbackMaxLights, stats)
				}
				// Every selected fallback light must be among the 16 lamps nearest the camera (Z in [-15, 0]).
				fb := sys.FallbackLights()
				if len(fb) != render3d.DefaultFallbackMaxLights {
					t.Fatalf("len(FallbackLights) = %d, want %d", len(fb), render3d.DefaultFallbackMaxLights)
				}
				for i, l := range fb {
					if l.Position.Z < -15.5 || l.Position.Z > 0.5 {
						t.Errorf("fallback light[%d] at Z=%.1f is not among the 16 nearest lamps [-15..0]", i, l.Position.Z)
					}
				}
			}

			img, err := app.GPUReadPixels("id:lamps-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}

			// Sample floor points right under lamps near the camera (which were previously dropped!).
			for _, z := range []float32{0, -3, -6, -10} {
				screen, _, vis := cam.WorldToScreen(math3d.Vec3{X: 0, Y: 0, Z: z}, viewW, viewH)
				if !vis {
					t.Fatalf("world point (0,0,%.1f) not visible", z)
				}
				px, py := int(screen.X), int(screen.Y)
				c := img.RGBAAt(px, py)
				peak := max(c.R, c.G, c.B)
				if peak < 40 {
					t.Errorf("%s: floor under near lamp Z=%.1f at (%d,%d) is dark %v, want visibly lit", prof.name, z, px, py, c)
				}
			}

			// On the full clustered profile, mid/far lamps (e.g. Z = -25) are ALSO lit.
			if prof.wantCluster {
				screen, _, vis := cam.WorldToScreen(math3d.Vec3{X: 0, Y: 0, Z: -25}, viewW, viewH)
				if !vis {
					t.Fatal("world point (0,0,-25) not visible")
				}
				c := img.RGBAAt(int(screen.X), int(screen.Y))
				if max(c.R, c.G, c.B) < 30 {
					t.Errorf("full: floor under mid-corridor lamp Z=-25 at (%d,%d) is dark %v, want visibly lit", int(screen.X), int(screen.Y), c)
				}
			}
		})
	}
}

func TestBuiltSceneLightsAndLightRigNearestCamera(t *testing.T) {
	objects := []sceneformat.ObjectDoc{
		{
			ID:   "cam",
			X:    0,
			Y:    2,
			PosZ: 100,
			Camera: &sceneformat.Camera3DDoc{
				Current: true,
			},
		},
		{
			ID:    "sun_shadow",
			RotX:  -0.8,
			Light: &sceneformat.LightDoc{Kind: sceneformat.LightDirectional, Shadows: true},
		},
		{
			ID:    "pt_shadow",
			PosZ:  -50, // Far from camera, but casts shadows so slot 0 must keep it.
			Light: &sceneformat.LightDoc{Kind: sceneformat.LightPoint, Intensity: sceneformat.Float32(4), Range: 10, Shadows: true},
		},
		{
			ID:    "spot_shadow",
			PosZ:  -60,
			RotX:  -1.0,
			Light: &sceneformat.LightDoc{Kind: sceneformat.LightSpot, Intensity: sceneformat.Float32(4), Range: 10, Shadows: true},
		},
	}
	// Add 30 unshadowed point lights from Z = 0 to Z = 87 (step 3) and 14 unshadowed spot lights from Z = 0 to Z = 78 (step 6).
	for i := range 30 {
		objects = append(objects, sceneformat.ObjectDoc{
			ID:    fmt.Sprintf("pt_%02d", i),
			PosZ:  float32(i * 3),
			Light: &sceneformat.LightDoc{Kind: sceneformat.LightPoint, Intensity: sceneformat.Float32(2), Range: 5},
		})
	}
	for i := range 14 {
		objects = append(objects, sceneformat.ObjectDoc{
			ID:    fmt.Sprintf("sp_%02d", i),
			PosZ:  float32(i * 6),
			RotX:  -1.0,
			Light: &sceneformat.LightDoc{Kind: sceneformat.LightSpot, Intensity: sceneformat.Float32(2), Range: 5},
		})
	}
	// Also add an unbounded (Range: 0) point light to verify ClusterLightFromPoint derives a positive range.
	objects = append(objects, sceneformat.ObjectDoc{
		ID:    "pt_unbounded",
		PosZ:  95,
		Light: &sceneformat.LightDoc{Kind: sceneformat.LightPoint, Intensity: sceneformat.Float32(4)},
	})

	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers:  []sceneformat.LayerDoc{{Name: "world", Objects: objects}},
	}
	world := scene3d.NewScene()
	built, err := scenemodel.Build(doc, world.Root(), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// 1. SceneLights keeps directional + slot 0 shadow point/spot in rig, and puts all 30+14+1 = 45 other local lights in ClusterLight.
	rig, local := built.SceneLights()
	if len(rig.Directional) != 1 || len(rig.Point) != 1 || len(rig.Spot) != 1 {
		t.Fatalf("SceneLights rig counts = %d/%d/%d, want 1/1/1", len(rig.Directional), len(rig.Point), len(rig.Spot))
	}
	if rig.Point[0].Position.Z != -50 {
		t.Errorf("rig.Point[0].Position.Z = %v, want -50 (shadow caster)", rig.Point[0].Position.Z)
	}
	if rig.Spot[0].Position.Z != -60 {
		t.Errorf("rig.Spot[0].Position.Z = %v, want -60 (shadow caster)", rig.Spot[0].Position.Z)
	}
	if len(local) != 45 {
		t.Fatalf("len(SceneLights local) = %d, want 45", len(local))
	}
	for i, l := range local {
		if err := l.Validate(); err != nil {
			t.Errorf("local[%d] failed Validate: %v", i, err)
		}
	}

	// 2. LightRig() uses the active camera at Z=100: slot 0 keeps the shadow caster, and slots 1..15 pick the nearest point lights (high Z).
	lr := built.LightRig()
	if len(lr.Point) != render3d.MaxPointLights {
		t.Fatalf("len(LightRig().Point) = %d, want %d", len(lr.Point), render3d.MaxPointLights)
	}
	if lr.Point[0].Position.Z != -50 {
		t.Fatalf("LightRig().Point[0].Position.Z = %v, want -50 (shadow caster)", lr.Point[0].Position.Z)
	}
	if lr.Point[1].Position.Z != 95 {
		t.Errorf("LightRig().Point[1].Position.Z = %v, want 95 (nearest to cam at Z=100)", lr.Point[1].Position.Z)
	}
	for i := 2; i < len(lr.Point); i++ {
		if lr.Point[i].Position.Z > lr.Point[i-1].Position.Z {
			t.Errorf("LightRig().Point[%d].Z=%v is closer to cam(100) than Point[%d].Z=%v", i, lr.Point[i].Position.Z, i-1, lr.Point[i-1].Position.Z)
		}
	}
	if len(lr.Spot) != render3d.MaxSpotLights {
		t.Fatalf("len(LightRig().Spot) = %d, want %d", len(lr.Spot), render3d.MaxSpotLights)
	}
	if lr.Spot[0].Position.Z != -60 {
		t.Fatalf("LightRig().Spot[0].Position.Z = %v, want -60 (shadow caster)", lr.Spot[0].Position.Z)
	}
	if lr.Spot[1].Position.Z != 78 {
		t.Errorf("LightRig().Spot[1].Position.Z = %v, want 78 (nearest spot to cam at Z=100)", lr.Spot[1].Position.Z)
	}

	// 3. LightRigNear at Z=0 picks the low-Z lights instead.
	nearZero := built.LightRigNear(math3d.Vec3{Z: 0})
	if nearZero.Point[0].Position.Z != -50 {
		t.Fatalf("LightRigNear(0).Point[0].Z = %v, want -50 (shadow caster)", nearZero.Point[0].Position.Z)
	}
	if nearZero.Point[1].Position.Z != 0 {
		t.Errorf("LightRigNear(0).Point[1].Z = %v, want 0 (nearest to Z=0)", nearZero.Point[1].Position.Z)
	}
	if nearZero.Spot[1].Position.Z != 0 {
		t.Errorf("LightRigNear(0).Spot[1].Z = %v, want 0 (nearest spot to Z=0)", nearZero.Spot[1].Position.Z)
	}
}
