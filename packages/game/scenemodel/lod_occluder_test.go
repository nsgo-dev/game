package scenemodel_test

import (
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// mapModelResolver serves in-memory glTF documents and optional import sidecars for LOD/occluder tests.
type mapModelResolver struct {
	models   map[string]*gltf.Document
	sidecars map[string]*importsettings.File
}

func (m *mapModelResolver) Model(p string) (*gltf.Document, error) {
	return m.models[p], nil
}

func (m *mapModelResolver) ImportSettings(p string) (*importsettings.File, error) {
	if m.sidecars == nil {
		return nil, nil
	}
	return m.sidecars[p], nil
}

func gltfMeshFromRender3D(name string, m render3d.Mesh) gltf.Mesh {
	return gltf.Mesh{
		Name: name,
		Primitives: []gltf.Primitive{{
			Mode:       gltf.ModeTriangles,
			Positions:  m.Positions,
			Normals:    m.Normals,
			TexCoords0: m.UVs,
			Indices:    m.Indices,
			Material:   -1,
		}},
	}
}

// twoLODModelDoc builds a glTF document with a high-detail sphere (LOD0) and a low-detail box (LOD1)
// using the _LOD1 node-name convention.
func twoLODModelDoc(t *testing.T, baseName string) (*gltf.Document, uint32, uint32) {
	t.Helper()
	high, err := scenemodel.PrimitiveMesh(sceneformat.MeshSphere)
	if err != nil {
		t.Fatal(err)
	}
	low, err := scenemodel.PrimitiveMesh(sceneformat.MeshBox)
	if err != nil {
		t.Fatal(err)
	}
	highTris := uint32(len(high.Indices) / 3)
	lowTris := uint32(len(low.Indices) / 3)
	if highTris <= lowTris {
		t.Fatalf("expected sphere (%d tris) to have more triangles than box (%d tris)", highTris, lowTris)
	}
	doc := &gltf.Document{
		Scene:  0,
		Scenes: []gltf.Scene{{Roots: []int{0, 1}}},
		Nodes: []gltf.Node{
			{Name: baseName, Mesh: 0, Parent: -1, Scale: math3d.Vec3One(), Rotation: math3d.QuatIdentity()},
			{Name: baseName + "_LOD1", Mesh: 1, Parent: -1, Scale: math3d.Vec3One(), Rotation: math3d.QuatIdentity()},
		},
		Meshes: []gltf.Mesh{
			gltfMeshFromRender3D(baseName, high),
			gltfMeshFromRender3D(baseName+"_LOD1", low),
		},
	}
	return doc, highTris, lowTris
}

// TestLODNodeNameConventionAndSidecarThresholds verifies that a model with two LODs
// pairs _LOD1 automatically (without drawing it twice), applies declared screen-ratio
// thresholds from its *.import.yaml sidecar, and switches at the declared ratio in
// both render3d.Queue and scenemodel.Renderer.
func TestLODNodeNameConventionAndSidecarThresholds(t *testing.T) {
	modelDoc, highTris, lowTris := twoLODModelDoc(t, "Statue")
	resolver := &mapModelResolver{
		models: map[string]*gltf.Document{
			"models/statue.glb": modelDoc,
		},
		sidecars: map[string]*importsettings.File{
			"models/statue.glb": {
				Version: importsettings.CurrentVersion,
				Model: &importsettings.Model{
					LODs: []importsettings.ModelLOD{
						{MinScreenRatio: 0.30},
						{MinScreenRatio: 0},
					},
				},
			},
		},
	}

	sceneDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{ID: "statue", Model: "models/statue.glb"},
			},
		}},
	}
	sc := scene3d.NewScene()
	built, err := scenemodel.Build(sceneDoc, sc.Root(), resolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// _LOD1 must be paired onto Statue rather than emitted as a second drawable.
	if len(built.Drawables) != 1 {
		t.Fatalf("len(Drawables) = %d, want 1", len(built.Drawables))
	}
	part := built.Drawables[0].Parts[0]
	if len(part.LODs) != 2 {
		t.Fatalf("len(part.LODs) = %d, want 2", len(part.LODs))
	}
	if part.LODs[0].MinScreenRatio != 0.30 || part.LODs[1].MinScreenRatio != 0 {
		t.Fatalf("LOD ratios = (%v, %v), want (0.30, 0)", part.LODs[0].MinScreenRatio, part.LODs[1].MinScreenRatio)
	}
	if part.LODs[0].Triangles != highTris || part.LODs[1].Triangles != lowTris {
		t.Fatalf("LOD triangles = (%d, %d), want (%d, %d)", part.LODs[0].Triangles, part.LODs[1].Triangles, highTris, lowTris)
	}

	items := built.AppendDrawItems(nil)
	if len(items) != 1 || len(items[0].LODs) != 2 {
		t.Fatalf("AppendDrawItems LODs = %+v, want 2 levels", items)
	}

	// Render with scenemodel.Renderer at a near camera (screenRatio > 0.30 -> LOD0)
	// and a far camera (screenRatio < 0.30 -> LOD1).
	sr := gpu.NewSoftwareRenderer(160, 120, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	r, err := scenemodel.NewRenderer(nil, sr.Device(), built, scenemodel.RendererOptions{Label: "lod"})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	nearCam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(45), 160.0/120.0, 0.1, 100)
	nearCam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	nearRatio := render3d.ProjectScreenRatio(items[0].WorldBounds, nearCam.ViewProj())
	if nearRatio < 0.30 {
		t.Fatalf("test setup: nearRatio = %v, want >= 0.30", nearRatio)
	}
	nearStats, err := r.Prepare(nearCam, levelLights)
	if err != nil {
		t.Fatalf("Prepare near: %v", err)
	}
	nearPlan := r.Plan()
	if len(nearPlan.Opaque) != 1 || nearPlan.Opaque[0].LODIndex != 0 {
		t.Fatalf("near LODIndex = %+v, want 0 (ratio=%v)", nearPlan.Opaque, nearRatio)
	}
	if nearStats.Triangles != uint64(highTris) {
		t.Fatalf("near triangles = %d, want %d", nearStats.Triangles, highTris)
	}

	// Verify Draw runs cleanly with LOD0 mesh uploaded.
	drawPass := func() {
		enc := sr.Device().BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(clearHex)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		r.Draw(pass)
		pass.End()
		sr.Device().Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
	}
	drawPass()

	// Move camera far enough that screenRatio drops below 0.30 -> switches to LOD1.
	farCam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 12}, math3d.DegToRad(45), 160.0/120.0, 0.1, 100)
	farCam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	farRatio := render3d.ProjectScreenRatio(items[0].WorldBounds, farCam.ViewProj())
	if farRatio >= 0.30 {
		t.Fatalf("test setup: farRatio = %v, want < 0.30", farRatio)
	}
	farStats, err := r.Prepare(farCam, levelLights)
	if err != nil {
		t.Fatalf("Prepare far: %v", err)
	}
	farPlan := r.Plan()
	if len(farPlan.Opaque) != 1 || farPlan.Opaque[0].LODIndex != 1 {
		t.Fatalf("far LODIndex = %+v, want 1 (ratio=%v)", farPlan.Opaque, farRatio)
	}
	if farStats.Triangles != uint64(lowTris) {
		t.Fatalf("far triangles = %d, want %d", farStats.Triangles, lowTris)
	}
	drawPass()
}

// TestSidecarExternalModelLODs verifies that a *.import.yaml sidecar can point LOD1
// at a separate glTF model file and load cleanly through scenemodel.Assets.
func TestSidecarExternalModelLODs(t *testing.T) {
	high, err := scenemodel.PrimitiveMesh(sceneformat.MeshSphere)
	if err != nil {
		t.Fatal(err)
	}
	low, err := scenemodel.PrimitiveMesh(sceneformat.MeshBox)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &mapModelResolver{
		models: map[string]*gltf.Document{
			"models/prop.glb": {
				Scene:  0,
				Scenes: []gltf.Scene{{Roots: []int{0}}},
				Nodes:  []gltf.Node{{Name: "Prop", Mesh: 0, Parent: -1, Scale: math3d.Vec3One(), Rotation: math3d.QuatIdentity()}},
				Meshes: []gltf.Mesh{gltfMeshFromRender3D("Prop", high)},
			},
			"models/prop_lod1.glb": {
				Scene:  0,
				Scenes: []gltf.Scene{{Roots: []int{0}}},
				Nodes:  []gltf.Node{{Name: "PropLow", Mesh: 0, Parent: -1, Scale: math3d.Vec3One(), Rotation: math3d.QuatIdentity()}},
				Meshes: []gltf.Mesh{gltfMeshFromRender3D("PropLow", low)},
			},
		},
		sidecars: map[string]*importsettings.File{
			"models/prop.glb": {
				Version: importsettings.CurrentVersion,
				Model: &importsettings.Model{
					LODHysteresis: 0.05,
					LODs: []importsettings.ModelLOD{
						{MinScreenRatio: 0.25},
						{Model: "models/prop_lod1.glb", MinScreenRatio: 0},
					},
				},
			},
		},
	}
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{
			Name:    "world",
			Objects: []sceneformat.ObjectDoc{{ID: "prop", Model: "models/prop.glb"}},
		}},
	}
	sc := scene3d.NewScene()
	built, err := scenemodel.Build(doc, sc.Root(), resolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if built.LODHysteresis != 0.05 {
		t.Fatalf("Built.LODHysteresis = %v, want 0.05", built.LODHysteresis)
	}
	if len(built.Drawables) != 1 || len(built.Drawables[0].Parts[0].LODs) != 2 {
		t.Fatalf("expected 1 drawable with 2 LODs, got %+v", built.Drawables)
	}
	if built.Drawables[0].Parts[0].LODs[0].MeshID == built.Drawables[0].Parts[0].LODs[1].MeshID {
		t.Fatal("LOD0 and LOD1 must have distinct MeshIDs")
	}
}

// TestAssetsImportSettingsReadsSidecarFromFS verifies that scenemodel.Assets reads
// *.import.yaml sidecars from an fs.FS and returns (nil, nil) when absent.
func TestAssetsImportSettingsReadsSidecarFromFS(t *testing.T) {
	fsys := fstest.MapFS{
		"models/tree.glb.import.yaml": {Data: []byte("version: 1\nmodel:\n  lods:\n    - minScreenRatio: 0.4\n    - minScreenRatio: 0\n")},
	}
	a := scenemodel.NewAssets(fsys)
	got, err := a.ImportSettings("models/tree.glb")
	if err != nil || got == nil || got.Model == nil || len(got.Model.LODs) != 2 {
		t.Fatalf("ImportSettings(tree) = (%+v, %v), want 2 LODs", got, err)
	}
	none, err := a.ImportSettings("models/missing.glb")
	if err != nil || none != nil {
		t.Fatalf("ImportSettings(missing) = (%+v, %v), want (nil, nil)", none, err)
	}
}

// TestOccluderWallCullsObjectsBehindIt verifies that both "model" and "box" occluders
// built from a scene file populate render3d.OcclusionBuffer and cull objects hidden
// behind the wall, reported in RenderStats.Occluded.
func TestOccluderWallCullsObjectsBehindIt(t *testing.T) {
	boxSize := [3]float32{12, 8, 1}
	cases := []struct {
		name string
		wall sceneformat.ObjectDoc
	}{
		{
			name: "model_occluder",
			wall: sceneformat.ObjectDoc{
				ID:       "wall",
				PosZ:     -4,
				ScaleX:   sceneformat.Float32(12),
				ScaleY:   sceneformat.Float32(8),
				ScaleZ:   sceneformat.Float32(1),
				Mesh:     sceneformat.MeshBox,
				Occluder: &sceneformat.OccluderDoc{Mode: sceneformat.OccluderModel},
			},
		},
		{
			name: "box_occluder",
			wall: sceneformat.ObjectDoc{
				ID:       "wall",
				PosZ:     -4,
				Occluder: &sceneformat.OccluderDoc{Mode: sceneformat.OccluderBox, Size: &boxSize},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &sceneformat.SceneDoc{
				Version: sceneformat.CurrentVersion,
				Layers: []sceneformat.LayerDoc{{
					Name: "world",
					Objects: []sceneformat.ObjectDoc{
						tc.wall,
						// Hidden directly behind the wall from camera at (0, 0, 5) looking down -Z.
						{ID: "hidden_a", X: 0, Y: 0, PosZ: -10, Mesh: sceneformat.MeshBox},
						{ID: "hidden_b", X: 1.5, Y: 0, PosZ: -12, Mesh: sceneformat.MeshSphere},
						// Clearly visible in front of the wall.
						{ID: "front", X: 0, Y: 0, PosZ: -1, Mesh: sceneformat.MeshBox},
					},
				}},
			}

			sc := scene3d.NewScene()
			built, err := scenemodel.Build(doc, sc.Root(), nil)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if len(built.Occluders) != 1 {
				t.Fatalf("len(built.Occluders) = %d, want 1", len(built.Occluders))
			}

			cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 5}, math3d.DegToRad(60), 160.0/120.0, 0.1, 100)
			cam.LookAt(math3d.Vec3{Z: -10}, math3d.Vec3Up())

			// 1. Direct Queue + RasterizeOccluders path.
			occ := render3d.NewOcclusionBuffer(0, 0)
			if n := built.RasterizeOccluders(occ, cam.ViewProj()); n != 1 {
				t.Fatalf("RasterizeOccluders = %d, want 1", n)
			}
			items := built.AppendDrawItems(nil)
			var q render3d.Queue
			plan := q.BuildWithOcclusion(cam.ViewProj(), cam.Pos, items, occ)
			if plan.Stats.Occluded != 2 {
				t.Fatalf("Queue RenderStats.Occluded = %d, want 2 (stats=%s)", plan.Stats.Occluded, plan.Stats.Summary())
			}

			// 2. scenemodel.Renderer path automatically rasterizes Built.Occluders.
			sr := gpu.NewSoftwareRenderer(160, 120, gpu.FormatDepth24Plus)
			t.Cleanup(sr.Close)
			r, err := scenemodel.NewRenderer(nil, sr.Device(), built, scenemodel.RendererOptions{Label: tc.name})
			if err != nil {
				t.Fatalf("NewRenderer: %v", err)
			}
			t.Cleanup(func() { _ = r.Close() })
			stats, err := r.Prepare(cam, levelLights)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if stats.Occluded != 2 {
				t.Fatalf("Renderer RenderStats.Occluded = %d, want 2 (stats=%s)", stats.Occluded, stats.Summary())
			}

			// Hiding the wall disables its occluder so the hidden objects become visible again.
			built.Objects["wall"].SetVisible(false)
			statsUnoccluded, err := r.Prepare(cam, levelLights)
			if err != nil {
				t.Fatalf("Prepare after hide: %v", err)
			}
			if statsUnoccluded.Occluded != 0 {
				t.Fatalf("Occluded after hiding wall = %d, want 0", statsUnoccluded.Occluded)
			}
		})
	}
}
