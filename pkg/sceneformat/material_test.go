package sceneformat_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// materialResolver resolves atlases from a map and materials from a shared resource.Library.
type materialResolver struct {
	sceneformat.AtlasMap
	*resource.Library
}

const stoneMaterialV1 = "type: material\nversion: 1\nopacity: 0.5\n"

const stoneMaterialV2 = "type: material\nversion: 1\nopacity: 0.25\nblend: multiply\n"

func sceneUsingMaterial(name, objectID string, alpha float32) *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    name,
		Layers: []sceneformat.LayerDoc{{
			Name: "actors",
			Objects: []sceneformat.ObjectDoc{{
				ID:       objectID,
				Alpha:    sceneformat.Float32(alpha),
				Sprite:   &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
				Material: "materials/stone.material.yaml",
			}},
		}},
	}
}

func TestMaterialFileSharedByTwoScenesChangesBothWhenEdited(t *testing.T) {
	lib := resource.NewLibrary(fstest.MapFS{
		"materials/stone.material.yaml": &fstest.MapFile{Data: []byte(stoneMaterialV1)},
	})
	resolver := materialResolver{AtlasMap: caveResolver, Library: lib}

	cave, err := sceneformat.Build(sceneUsingMaterial("Cave", "cave_wall", 1), resolver)
	if err != nil {
		t.Fatalf("Build cave: %v", err)
	}
	keep, err := sceneformat.Build(sceneUsingMaterial("Keep", "keep_wall", 0.8), resolver)
	if err != nil {
		t.Fatalf("Build keep: %v", err)
	}
	caveWall, keepWall := cave.Object("cave_wall"), keep.Object("keep_wall")
	if caveWall.Material == nil || caveWall.Material != keepWall.Material {
		t.Fatal("the two scenes do not share one material value")
	}
	if caveWall.Sprite.Alpha != 0.5 || keepWall.Sprite.Alpha != 0.4 {
		t.Fatalf("alpha before the edit = %v, %v; want 0.5, 0.4", caveWall.Sprite.Alpha, keepWall.Sprite.Alpha)
	}

	if loaded, err := lib.Reload("materials/stone.material.yaml", []byte(stoneMaterialV2)); !loaded || err != nil {
		t.Fatalf("Reload = %v, %v", loaded, err)
	}
	cave.SyncMaterials()
	keep.SyncMaterials()
	if caveWall.Sprite.Alpha != 0.25 || keepWall.Sprite.Alpha != 0.2 {
		t.Fatalf("alpha after the edit = %v, %v; want 0.25, 0.2", caveWall.Sprite.Alpha, keepWall.Sprite.Alpha)
	}
	if caveWall.Sprite.Blend != core.CompositeMultiply || keepWall.Sprite.Blend != core.CompositeMultiply {
		t.Fatalf("blend after the edit = %v, %v; want multiply", caveWall.Sprite.Blend, keepWall.Sprite.Blend)
	}
}

func TestMaterialReferenceValidationAndResolution(t *testing.T) {
	doc := sceneUsingMaterial("Cave", "wall", 1)
	doc.Layers[0].Objects[0].Material = "../stone.material.yaml"
	if _, err := sceneformat.Encode(doc); err == nil || !strings.Contains(err.Error(), "material") {
		t.Fatalf("Encode accepted a material path outside the project: %v", err)
	}

	doc = sceneUsingMaterial("Cave", "wall", 1)
	if _, err := sceneformat.Build(doc, caveResolver); err == nil || !strings.Contains(err.Error(), "MaterialResolver") {
		t.Fatalf("Build without a MaterialResolver: %v", err)
	}
	lib := resource.NewLibrary(fstest.MapFS{})
	if _, err := sceneformat.Build(doc, materialResolver{AtlasMap: caveResolver, Library: lib}); err == nil || !strings.Contains(err.Error(), "stone.material.yaml") {
		t.Fatalf("Build with a missing material file: %v", err)
	}

	raw, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "material: materials/stone.material.yaml\n") {
		t.Fatalf("Encode did not write the material reference:\n%s", raw)
	}
	back, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := sceneformat.Encode(back)
	if err != nil || string(again) != string(raw) {
		t.Fatalf("material reference did not round-trip: %v\n%s", err, again)
	}
}

func TestMaterialOnNestedChildMultipliesItsWorldAlpha(t *testing.T) {
	lib := resource.NewLibrary(fstest.MapFS{
		"materials/stone.material.yaml": &fstest.MapFile{Data: []byte(stoneMaterialV1)},
	})
	resolver := materialResolver{AtlasMap: caveResolver, Library: lib}
	doc := sceneUsingMaterial("Cave", "wall", 0.8)
	wall := &doc.Layers[0].Objects[0]
	wall.Children = []sceneformat.ObjectDoc{{
		ID:       "moss",
		Alpha:    sceneformat.Float32(0.5),
		Sprite:   &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
		Material: "materials/stone.material.yaml",
	}}

	built, err := sceneformat.Build(doc, resolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	moss := built.Object("moss")
	if moss.Material == nil || moss.Material != built.Object("wall").Material {
		t.Fatal("the nested child does not share its material with its parent")
	}
	// 0.8 (wall) * 0.5 (moss) * 0.5 (material opacity).
	if !near(moss.Sprite.Alpha, 0.2) {
		t.Fatalf("moss alpha = %v, want 0.2", moss.Sprite.Alpha)
	}
	if loaded, err := lib.Reload("materials/stone.material.yaml", []byte(stoneMaterialV2)); !loaded || err != nil {
		t.Fatalf("Reload = %v, %v", loaded, err)
	}
	built.SyncMaterials()
	if !near(moss.Sprite.Alpha, 0.1) || moss.Sprite.Blend != core.CompositeMultiply {
		t.Fatalf("moss after the edit = alpha %v blend %v, want 0.1 multiply", moss.Sprite.Alpha, moss.Sprite.Blend)
	}
}
