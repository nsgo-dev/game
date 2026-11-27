package scenemodel_test

import (
	"errors"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/scenemodel"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// projectDir is the test project whose scene names the crate model, primitives and material files.
var projectDir = filepath.Join("testdata", "project")

// loadLevel parses the test project's level scene.
func loadLevel(t *testing.T) *sceneformat.SceneDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, "scenes", "level.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "scenes/level.scene.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// buildLevel builds the test project's level under a new scene's root.
func buildLevel(t *testing.T) (*scene3d.Scene, *scenemodel.Built) {
	t.Helper()
	sc := scene3d.NewScene()
	built, err := scenemodel.Build(loadLevel(t), sc.Root(), scenemodel.NewAssets(os.DirFS(projectDir)))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return sc, built
}

func approxBounds(got, want math3d.AABB) bool {
	return got.Min.ApproxEqual(want.Min, 1e-5) && got.Max.ApproxEqual(want.Max, 1e-5)
}

func TestLevelFileRoundTrips(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(projectDir, "scenes", "level.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := sceneformat.Encode(loadLevel(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(data) {
		t.Fatalf("level.scene.yaml is not in canonical form; Encode writes:\n%s", out)
	}
}

func TestBuildAttachesModelsAndPrimitives(t *testing.T) {
	_, built := buildLevel(t)

	type drawn struct {
		object, node string
		parts        int
	}
	var got []drawn
	for _, d := range built.Drawables {
		got = append(got, drawn{d.Object, d.Node.Name, len(d.Parts)})
	}
	want := []drawn{
		{"floor", "floor", 1},
		{"crate", "Crate", 1},
		{"crate", "Lid", 1},
		{"lid", "lid", 1},
		{"ball", "ball", 1},
		{"glass", "glass", 1},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("drawables = %v, want %v", got, want)
	}

	// The whole model hangs under the object's node with the glTF nodes' own transforms.
	crate := built.Objects["crate"]
	if kids := crate.Children(); len(kids) != 1 || kids[0].Name != "Crate" {
		t.Fatalf("crate children = %v, want the model's Crate node", kids)
	}
	crateNode := crate.Children()[0]
	lidNode := crateNode.Children()[0]
	if !lidNode.Local.Pos.ApproxEqual(math3d.Vec3{Y: 0.6}, 1e-6) {
		t.Errorf("Lid local position = %v, want (0, 0.6, 0)", lidNode.Local.Pos)
	}
	if !approxBounds(crateNode.LocalBounds, math3d.AABB{Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}) {
		t.Errorf("Crate LocalBounds = %v, want the unit box", crateNode.LocalBounds)
	}
	lidBounds := math3d.AABB{Min: math3d.Vec3{X: -0.55, Y: -0.1, Z: -0.55}, Max: math3d.Vec3{X: 0.55, Y: 0.1, Z: 0.55}}
	if !approxBounds(lidNode.LocalBounds, lidBounds) {
		t.Errorf("Lid LocalBounds = %v, want %v", lidNode.LocalBounds, lidBounds)
	}
	// The crate sits at y 0.5, so the lid's top is at 0.5 + 0.6 + 0.1.
	if top := crate.HierarchyBounds().Max.Y; top < 1.19 || top > 1.21 {
		t.Errorf("crate hierarchy top = %v, want 1.2", top)
	}

	// modelNode draws that node on the object's own node, with the object's transform in place of the node's.
	lid := built.Objects["lid"]
	if len(lid.Children()) != 0 || !approxBounds(lid.LocalBounds, lidBounds) {
		t.Errorf("lid object: children %v, bounds %v; want the Lid mesh on its own node", lid.Children(), lid.LocalBounds)
	}
	if !lid.WorldTransform().Pos.ApproxEqual(math3d.Vec3{X: 1.5, Y: 0.1, Z: -1.2}, 1e-6) {
		t.Errorf("lid world position = %v, want the object's", lid.WorldTransform().Pos)
	}
	if d, ok := scene3d.AttachmentAs[*scenemodel.Drawable](lid, scenemodel.AttachmentKey); !ok || d.Object != "lid" {
		t.Errorf("lid attachment = %v, %v", d, ok)
	}

	// The two Lid meshes share geometry and material; every primitive shape and material has its own ID.
	byNode := map[string]*scenemodel.Part{}
	for _, d := range built.Drawables {
		byNode[d.Object+"/"+d.Node.Name] = &d.Parts[0]
	}
	if a, b := byNode["crate/Lid"], byNode["lid/lid"]; a.MeshID != b.MeshID || a.MaterialID != b.MaterialID {
		t.Errorf("Lid parts: mesh %d/%d, material %d/%d; want them shared", a.MeshID, b.MeshID, a.MaterialID, b.MaterialID)
	}
	if built.Meshes != 5 || built.Materials != 5 {
		t.Errorf("Meshes, Materials = %d, %d; want 5 (plane, crate, lid, sphere, capsule) and 5 (stone, wood, brass, teal, glass)", built.Meshes, built.Materials)
	}

	wood := byNode["crate/Crate"].Material
	if tex := wood.BaseColorTexture; tex == nil || tex.Image.Bounds() != image.Rect(0, 0, 8, 8) || tex.Mipmaps || tex.Sampler.MagFilter != gpu.FilterNearest {
		t.Errorf("Wood texture = %+v, want the 8x8 planks with the model's nearest, unmipmapped sampler", tex)
	}
	if wood.Metallic != 0 || wood.Roughness != 0.8 {
		t.Errorf("Wood metallic, roughness = %v, %v", wood.Metallic, wood.Roughness)
	}
	if glass := byNode["glass/glass"].Material; glass.AlphaMode != render3d.AlphaBlend || glass.BaseColor.W != 0.5 {
		t.Errorf("glass material = %+v, want blended at alpha 0.5", glass)
	}
	floor := byNode["floor/floor"].Material
	if want := render3d.SRGBToLinearChannel(0x8a / 255.0); floor.BaseColor.X < want-1e-4 || floor.BaseColor.X > want+1e-4 || floor.Roughness != 0.9 {
		t.Errorf("floor material = %+v, want the stone file's color in linear light and roughness 0.9", floor)
	}
}

func TestBuildModelMeshAndOverride(t *testing.T) {
	doc := &sceneformat.SceneDoc{Version: 2, Layers: []sceneformat.LayerDoc{{Name: "l", Objects: []sceneformat.ObjectDoc{
		{ID: "body", Model: "models/crate.glb", ModelMesh: "CrateMesh"},
		{ID: "stone_crate", Model: "models/crate.glb", Material: "materials/stone.material.yaml"},
		{ID: "box", Mesh: sceneformat.MeshBox},
	}}}}
	root := scene3d.NewNode("root")
	built, err := scenemodel.Build(doc, root, scenemodel.NewAssets(os.DirFS(projectDir)))
	if err != nil {
		t.Fatal(err)
	}
	body := built.Objects["body"]
	if len(body.Children()) != 0 || len(built.Drawables[0].Parts) != 1 || built.Drawables[0].Node != body {
		t.Fatalf("modelMesh should draw the mesh on the object's node, got %+v", built.Drawables[0])
	}
	// A material file replaces every material of the model, the lid's brass included.
	var stone []uint32
	for _, d := range built.Drawables {
		if d.Object == "stone_crate" {
			for _, p := range d.Parts {
				stone = append(stone, p.MaterialID)
				if p.Material.BaseColorTexture != nil {
					t.Errorf("%s keeps the model's texture under a material file", d.Node.Name)
				}
			}
		}
	}
	if len(stone) != 2 || stone[0] != stone[1] {
		t.Errorf("stone_crate material IDs = %v, want the file's material on both parts", stone)
	}
	box := built.Drawables[len(built.Drawables)-1]
	if box.Object != "box" || box.Parts[0].Material.Metallic != 0 || box.Parts[0].Material.Roughness != 1 || box.Parts[0].Material.BaseColor != (math3d.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
		t.Errorf("box without a material = %+v, want white, non-metallic, fully rough", box.Parts[0].Material)
	}
}

// failingModels is a ModelResolver whose every model is missing.
type failingModels struct{}

func (failingModels) Model(path string) (*gltf.Document, error) {
	return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
}

func TestBuildErrorsLeaveParentUnchanged(t *testing.T) {
	assets := scenemodel.NewAssets(os.DirFS(projectDir))
	cases := []struct {
		name   string
		object sceneformat.ObjectDoc
		models scenemodel.ModelResolver
		want   string
	}{
		{"missing model", sceneformat.ObjectDoc{ID: "o", Model: "models/gone.glb"}, failingModels{}, `model "models/gone.glb"`},
		{"missing node", sceneformat.ObjectDoc{ID: "o", Model: "models/crate.glb", ModelNode: "Door"}, assets, `has no node "Door"`},
		{"missing mesh", sceneformat.ObjectDoc{ID: "o", Model: "models/crate.glb", ModelMesh: "DoorMesh"}, assets, `has no mesh "DoorMesh"`},
		{"no resolver", sceneformat.ObjectDoc{ID: "o", Model: "models/crate.glb"}, nil, "requires a ModelResolver"},
		{"material without resolver", sceneformat.ObjectDoc{ID: "o", Mesh: "box", Material: "materials/stone.material.yaml"}, modelsOnly{assets}, "sceneformat.MaterialResolver"},
		{"missing material", sceneformat.ObjectDoc{ID: "o", Mesh: "box", Material: "materials/gone.material.yaml"}, assets, `material "materials/gone.material.yaml"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &sceneformat.SceneDoc{Version: 2, Layers: []sceneformat.LayerDoc{{Name: "l", Objects: []sceneformat.ObjectDoc{{ID: "first", Mesh: "box"}, tc.object}}}}
			root := scene3d.NewNode("root")
			_, err := scenemodel.Build(doc, root, tc.models)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), `object "o"`) {
				t.Fatalf("Build error = %v, want one about object \"o\" containing %q", err, tc.want)
			}
			if len(root.Children()) != 0 {
				t.Fatalf("Build left %d nodes under the parent after failing", len(root.Children()))
			}
		})
	}
	if _, err := scenemodel.Build(&sceneformat.SceneDoc{Version: 2}, nil, nil); !errors.Is(err, scene3d.ErrNilNode) {
		t.Errorf("Build with a nil parent = %v, want ErrNilNode", err)
	}
}

// modelsOnly is a ModelResolver that is not a MaterialResolver.
type modelsOnly struct{ a *scenemodel.Assets }

func (m modelsOnly) Model(path string) (*gltf.Document, error) { return m.a.Model(path) }

func TestMaterialFileTextureNeedsImageResolver(t *testing.T) {
	fsys := fstest.MapFS{
		"materials/brick.material.yaml": {Data: []byte("type: material\nversion: 1\ntexture: textures/brick.png\n")},
	}
	doc := &sceneformat.SceneDoc{Version: 2, Layers: []sceneformat.LayerDoc{{Name: "l", Objects: []sceneformat.ObjectDoc{{ID: "wall", Mesh: "box", Material: "materials/brick.material.yaml"}}}}}
	_, err := scenemodel.Build(doc, scene3d.NewNode("root"), materialsOnly{resource.NewLibrary(fsys)})
	if err == nil || !strings.Contains(err.Error(), "ImageResolver") {
		t.Fatalf("Build = %v, want an error asking for an ImageResolver", err)
	}
	// Assets decodes it.
	var png strings.Builder
	writePNG(t, &png, crateTexture())
	fsys["textures/brick.png"] = &fstest.MapFile{Data: []byte(png.String())}
	built, err := scenemodel.Build(doc, scene3d.NewNode("root"), scenemodel.NewAssets(fsys))
	if err != nil {
		t.Fatal(err)
	}
	tex := built.Drawables[0].Parts[0].Material.BaseColorTexture
	if tex == nil || tex.Name != "textures/brick.png" || !tex.Mipmaps || tex.Sampler.AddressU != gpu.AddressRepeat {
		t.Fatalf("texture = %+v, want the brick image with linear repeat mipmapped sampling", tex)
	}
}

// materialsOnly resolves material files but neither models nor images.
type materialsOnly struct{ lib *resource.Library }

func (materialsOnly) Model(path string) (*gltf.Document, error) { return nil, errors.New("no models") }
func (m materialsOnly) Material(path string) (*resource.Material, error) {
	return m.lib.Material(path)
}

func TestAppendDrawItemsSkipsHiddenNodes(t *testing.T) {
	_, built := buildLevel(t)
	items := built.AppendDrawItems(nil)
	if len(items) != 6 {
		t.Fatalf("items = %d, want 6", len(items))
	}
	for _, it := range items {
		d, p, ok := built.Part(it.ID)
		if !ok || it.MeshID != p.MeshID || it.MaterialID != p.MaterialID || it.PipelineID != scenemodel.DrawPipeline(p.Material) {
			t.Fatalf("item %+v does not match its part", it)
		}
		if !it.Transform.ApproxEqual(d.Node.WorldMatrix(), 1e-6) || it.WorldBounds.Empty() {
			t.Fatalf("item %d transform or bounds wrong", it.ID)
		}
	}
	if glass := items[5]; glass.Blend != render3d.BlendAlphaBlend || glass.PipelineID != 2 {
		t.Errorf("glass item blend %v pipeline %d, want alpha blend on pipeline 2", glass.Blend, glass.PipelineID)
	}
	// Hiding the crate object hides its model's nodes too.
	built.Objects["crate"].SetVisible(false)
	if n := len(built.AppendDrawItems(nil)); n != 4 {
		t.Errorf("items with the crate hidden = %d, want 4", n)
	}
	if _, _, ok := built.Part(scenemodel.DrawItemID(99, 0)); ok {
		t.Error("Part found a drawable that does not exist")
	}
}

func TestPrimitiveMeshes(t *testing.T) {
	bounds := map[string]math3d.AABB{
		"box":      {Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}},
		"sphere":   {Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}},
		"plane":    {Min: math3d.Vec3{X: -0.5, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Z: 0.5}},
		"cylinder": {Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}},
		"capsule":  {Min: math3d.Vec3{X: -0.5, Y: -1, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 1, Z: 0.5}},
	}
	for _, name := range sceneformat.MeshPrimitives() {
		t.Run(name, func(t *testing.T) {
			m, err := scenemodel.PrimitiveMesh(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Validate(); err != nil {
				t.Fatal(err)
			}
			if b := m.Bounds(); !approxBounds(b, bounds[name]) {
				t.Errorf("bounds = %v, want %v", b, bounds[name])
			}
			for i, n := range m.Normals {
				if l := n.Len(); l < 0.999 || l > 1.001 {
					t.Fatalf("normal %d has length %v", i, l)
				}
				if uv := m.UVs[i]; uv.X < 0 || uv.X > 1 || uv.Y < 0 || uv.Y > 1 {
					t.Fatalf("UV %d = %v is outside [0, 1]", i, uv)
				}
			}
			// Every triangle winds counter-clockwise seen from where its vertices' normals point, and none is degenerate.
			for k := 0; k < len(m.Indices); k += 3 {
				a, b, c := m.Indices[k], m.Indices[k+1], m.Indices[k+2]
				face := m.Positions[b].Sub(m.Positions[a]).Cross(m.Positions[c].Sub(m.Positions[a]))
				if face.Len() < 1e-7 {
					t.Fatalf("triangle %d is degenerate", k/3)
				}
				avg := m.Normals[a].Add(m.Normals[b]).Add(m.Normals[c])
				if face.Dot(avg) <= 0 {
					t.Fatalf("triangle %d winds against its normals", k/3)
				}
			}
		})
	}
	if _, err := scenemodel.PrimitiveMesh("cone"); err == nil {
		t.Error("PrimitiveMesh accepted an unknown shape")
	}
}

func TestMaterialFromFile(t *testing.T) {
	op := float32(0.25)
	rough := float32(0.3)
	m := scenemodel.MaterialFromFile(&resource.Material{BaseColor: "#ffffff", Emissive: "#ff0000", Opacity: &op, Metallic: 0.7, Roughness: &rough, DoubleSided: true, Blend: "multiply", Unlit: true})
	want := scenemodel.Material{BaseColor: math3d.Vec4{X: 1, Y: 1, Z: 1, W: 0.25}, Metallic: 0.7, Roughness: 0.3, Emissive: math3d.Vec3{X: 1}, AlphaMode: render3d.AlphaBlend, AlphaCutoff: 0.5, DoubleSided: true}
	if m != want {
		t.Fatalf("MaterialFromFile = %+v, want %+v", m, want)
	}
	if m := scenemodel.MaterialFromFile(&resource.Material{}); m.AlphaMode != render3d.AlphaOpaque || m.Roughness != 1 || m.BaseColor.W != 1 {
		t.Fatalf("empty material file = %+v, want opaque white, fully rough", m)
	}
}
