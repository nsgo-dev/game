package scenemodel_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/scenemodel"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func TestBuildGridMapInstancesMatchesBuildInstances(t *testing.T) {
	glb := crateGLB(t)
	gridYAML := `version: 1
name: Warehouse
cellSize: [2, 2, 2]
meshLibrary: models/crate.glb
boxes:
  - min: [0, 0, 0]
    max: [1, 0, 1]
    tile: Crate
cells:
  - at: [0, 1, 0]
    tile: Lid
    orientation: 5
  - at: [1, 1, 1]
    tile: Lid
    orientation: 19
`
	sceneYAML := `version: 2
name: Warehouse Level
layers:
  - name: level
    objects:
      - id: grid
        x: 10
        "y": 2
        posZ: -4
        gridMap: levels/warehouse.gridmap.yaml
`
	fsys := fstest.MapFS{
		"models/crate.glb":              {Data: glb},
		"levels/warehouse.gridmap.yaml": {Data: []byte(gridYAML)},
	}
	doc, err := sceneformat.Parse([]byte(sceneYAML))
	if err != nil {
		t.Fatalf("sceneformat.Parse: %v", err)
	}
	scene := scene3d.NewScene()
	assets := scenemodel.NewAssets(fsys)
	built, err := scenemodel.Build(doc, scene.Root(), assets)
	if err != nil {
		t.Fatalf("scenemodel.Build: %v", err)
	}

	gm := built.GridMaps["grid"]
	if gm == nil {
		t.Fatal("Built.GridMaps[\"grid\"] is nil")
	}
	if gm.Count() != 6 {
		t.Fatalf("gm.Count() = %d, want 6", gm.Count())
	}
	// 4 Crate cells + 2 Lid cells = 6 drawables sharing 2 meshes and 2 materials.
	if len(built.Drawables) != 6 {
		t.Fatalf("len(Drawables) = %d, want 6", len(built.Drawables))
	}
	if built.Meshes != 2 || built.Materials != 2 {
		t.Fatalf("Meshes=%d Materials=%d, want (2, 2)", built.Meshes, built.Materials)
	}

	// Verify every world transform in AppendDrawItems equals parentObject.WorldMatrix() * BuildInstances transform.
	objWorld := built.Objects["grid"].WorldMatrix()
	batches := gm.BuildInstances(nil, nil)
	var wantMats []math3d.Mat4
	for _, b := range batches {
		for _, m := range b.Transforms {
			wantMats = append(wantMats, objWorld.Mul(m))
		}
	}
	items := built.AppendDrawItems(nil)
	if len(items) != len(wantMats) {
		t.Fatalf("AppendDrawItems len = %d, want %d", len(items), len(wantMats))
	}
	matched := make([]bool, len(wantMats))
	for _, it := range items {
		found := false
		for j, wm := range wantMats {
			if !matched[j] && it.Transform.ApproxEqual(wm, 1e-4) {
				matched[j] = true
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("draw item transform %v did not match any BuildInstances transform", it.Transform)
		}
	}
}

func TestBuildGridMapAll24OrientationsExact(t *testing.T) {
	glb := crateGLB(t)
	var b strings.Builder
	b.WriteString("version: 1\ncellSize: [2, 2, 2]\nmeshLibrary: models/crate.glb\ncells:\n")
	for o := range world3d.GridOrientationCount {
		fmt.Fprintf(&b, "  - at: [%d, 0, 0]\n    tile: Crate\n    orientation: %d\n", o, o)
	}
	fsys := fstest.MapFS{
		"models/crate.glb":          {Data: glb},
		"levels/all24.gridmap.yaml": {Data: []byte(b.String())},
	}
	doc := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{{
			Name:    "level",
			Objects: []sceneformat.ObjectDoc{{ID: "g", GridMap: "levels/all24.gridmap.yaml"}},
		}},
	}
	scene := scene3d.NewScene()
	built, err := scenemodel.Build(doc, scene.Root(), scenemodel.NewAssets(fsys))
	if err != nil {
		t.Fatalf("scenemodel.Build: %v", err)
	}
	gm := built.GridMaps["g"]
	if len(built.Drawables) != world3d.GridOrientationCount {
		t.Fatalf("len(Drawables) = %d, want %d", len(built.Drawables), world3d.GridOrientationCount)
	}
	items := built.AppendDrawItems(nil)
	for o := range world3d.GridOrientationCount {
		want := gm.CellTransform(world3d.GridCoord{X: o, Y: 0, Z: 0}, uint8(o))
		found := false
		for _, it := range items {
			if it.Transform.ApproxEqual(want, 1e-4) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("orientation %d transform %v missing from draw items", o, want)
		}
	}
}

func TestBuildGridMapErrorsLeaveParentUnchanged(t *testing.T) {
	glb := crateGLB(t)
	cases := []struct {
		name string
		fsys fstest.MapFS
		want string
	}{
		{
			name: "missing gridmap file",
			fsys: fstest.MapFS{"models/crate.glb": {Data: glb}},
			want: "levels/missing.gridmap.yaml",
		},
		{
			name: "missing tile in library",
			fsys: fstest.MapFS{
				"models/crate.glb":            {Data: glb},
				"levels/missing.gridmap.yaml": {Data: []byte("version: 1\nmeshLibrary: models/crate.glb\ncells:\n  - at: [0, 0, 0]\n    tile: Ghost\n")},
			},
			want: "Ghost",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &sceneformat.SceneDoc{
				Version: 2,
				Layers: []sceneformat.LayerDoc{{
					Name:    "level",
					Objects: []sceneformat.ObjectDoc{{ID: "g", GridMap: "levels/missing.gridmap.yaml"}},
				}},
			}
			scene := scene3d.NewScene()
			_, err := scenemodel.Build(doc, scene.Root(), scenemodel.NewAssets(tc.fsys))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
			if len(scene.Root().Children()) != 0 {
				t.Fatalf("parent modified on error: %d children", len(scene.Root().Children()))
			}
		})
	}
}
