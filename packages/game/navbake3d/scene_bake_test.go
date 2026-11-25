package navbake3d_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/navbake3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestSceneFileRampSecondFloorAndWallRadius(t *testing.T) {
	// Ground floor: x in [0, 12], z in [0, 10], top at y = 0.
	// Second floor: x in [8, 16], z in [0, 10], top at y = 2.0.
	// Ramp: connects (x=3.5, y=0) up to (x=8.5, y=2.0), width 4 along Z (z in [3, 7]).
	// Slope = atan2(2.0, 5.0) ≈ 21.8° (< 45° max slope).
	const (
		rampDx    = 5.0
		rampDy    = 2.0
		rampLen   = 5.3851648 // hypot(5, 2)
		rampAngle = 0.3805064 // atan2(2, 5)
	)
	wallFootprint := [4]float32{3.0, 7.0, 4.0, 9.5}

	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Ramp and Second Floor",
		NavMesh: "levels/ramp.navmesh.yaml",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:     "ground",
						X:      7,
						Y:      -0.25,
						PosZ:   6,
						ScaleX: sceneformat.Float32(14),
						ScaleY: sceneformat.Float32(0.5),
						ScaleZ: sceneformat.Float32(12),
						Mesh:   sceneformat.MeshBox,
					},
					{
						ID:     "upper_floor",
						X:      13,
						Y:      1.75,
						PosZ:   6,
						ScaleX: sceneformat.Float32(8),
						ScaleY: sceneformat.Float32(0.5),
						ScaleZ: sceneformat.Float32(12),
						Mesh:   sceneformat.MeshBox,
					},
					{
						ID:     "ramp",
						X:      6.5,
						Y:      0.85,
						PosZ:   4.0,
						RotZ:   rampAngle,
						ScaleX: sceneformat.Float32(rampLen + 0.8),
						ScaleY: sceneformat.Float32(0.3),
						ScaleZ: sceneformat.Float32(4.0),
						Mesh:   sceneformat.MeshBox,
					},
					// Static 3D collider wall (no Mesh/Model, pure collider3d).
					{
						ID:   "wall",
						X:    3.5,
						Y:    1.5,
						PosZ: 8.25,
						Components: map[string]any{
							sceneformat.Body3DComponentName: map[string]any{
								"version": 1,
								"kind":    "static",
							},
							sceneformat.Collider3DComponentName: map[string]any{
								"version": 1,
								"shape":   "box",
								"width":   1.0,
								"height":  3.0,
								"depth":   2.5,
							},
						},
					},
					// Dynamic crate on the ground floor: must NOT block static navmesh.
					{
						ID:     "dynamic_crate",
						X:      2.0,
						Y:      0.5,
						PosZ:   2.0,
						ScaleX: sceneformat.Float32(1.2),
						ScaleY: sceneformat.Float32(1.2),
						ScaleZ: sceneformat.Float32(1.2),
						Mesh:   sceneformat.MeshBox,
						Components: map[string]any{
							sceneformat.Body3DComponentName: map[string]any{
								"version": 1,
								"kind":    "dynamic",
								"mass":    2.0,
							},
							sceneformat.Collider3DComponentName: map[string]any{
								"version": 1,
								"shape":   "box",
								"width":   1.2,
								"height":  1.2,
								"depth":   1.2,
							},
						},
					},
					// Trigger sensor on open floor: must NOT block static navmesh.
					{
						ID:   "zone_trigger",
						X:    1.5,
						Y:    1.0,
						PosZ: 4.5,
						Components: map[string]any{
							sceneformat.Trigger3DComponentName: map[string]any{
								"version": 1,
								"shape":   "box",
								"width":   1.5,
								"height":  2.0,
								"depth":   1.5,
							},
						},
					},
				},
			},
			// Screen-space HUD layer must be ignored by 3D navmesh baking.
			{
				Name:  "hud",
				Space: sceneformat.SpaceScreen,
				Objects: []sceneformat.ObjectDoc{
					{ID: "hud_box", X: 6, Y: 0, PosZ: 5, ScaleX: sceneformat.Float32(10), ScaleY: sceneformat.Float32(10), ScaleZ: sceneformat.Float32(10), Mesh: sceneformat.MeshBox},
				},
			},
		},
	}

	var g navbake3d.Geometry
	if err := g.AddSceneFile(doc, nil); err != nil {
		t.Fatalf("AddSceneFile: %v", err)
	}

	settings := navbake3d.Settings{
		CellSize:               0.2,
		CellHeight:             0.1,
		MaxSimplificationError: 0.2,
	}
	agent := navbake3d.AgentType{
		Name:     "humanoid",
		Radius:   0.5,
		Height:   1.8,
		MaxSlope: 45,
		MaxClimb: 0.4,
	}

	file, err := navbake3d.Bake(&g, settings, []navbake3d.AgentType{agent})
	if err != nil {
		t.Fatalf("Bake: %v", err)
	}
	raw1, err := navbake3d.Encode(file)
	if err != nil {
		t.Fatalf("Encode(1): %v", err)
	}
	parsed, err := navbake3d.Parse(raw1)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	raw2, err := navbake3d.Encode(parsed)
	if err != nil {
		t.Fatalf("Encode(2): %v", err)
	}
	if !bytes.Equal(raw1, raw2) {
		t.Fatalf("navmesh YAML did not round-trip byte for byte")
	}

	mesh, ok := parsed.Mesh("humanoid")
	if !ok {
		t.Fatal("missing humanoid mesh")
	}
	nm := mesh.NavMesh()

	// 1. Path climbs the ramp from the ground floor to the second floor.
	path, ok := nm.FindPath(v3(1.5, 0, 4.0), v3(13.5, 2.0, 4.0))
	if !ok {
		t.Fatal("no path from ground floor up ramp to second floor")
	}
	if end := path[len(path)-1]; end.Y < 1.8 || end.Y > 2.25 {
		t.Fatalf("path ends at Y=%v, want ~2.0 on second floor", end.Y)
	}
	var sawRampHeight bool
	for _, p := range sampleSegments(path, 0.1) {
		if p.Y > 0.4 && p.Y < 1.6 && p.X >= 3.5 && p.X <= 9.5 {
			sawRampHeight = true
		}
	}
	if !sawRampHeight {
		t.Fatalf("path did not traverse the ramp's intermediate heights: %v", path)
	}

	// 2. Agent radius keeps ground-floor paths off the static collider3d wall.
	wallPath, ok := nm.FindPath(v3(1.5, 0, 8.25), v3(5.5, 0, 8.25))
	if !ok {
		t.Fatal("no path around wall")
	}
	tol := settings.CellSize
	for _, p := range sampleSegments(wallPath, 0.05) {
		if d := distXZ(wallFootprint, p.X, p.Z); d < agent.Radius-tol {
			t.Fatalf("path point %v is %v from wall, closer than agent radius %v (tol %v)", p, d, agent.Radius, tol)
		}
	}

	// 3. Dynamic crate and trigger do not block the walkable ground floor.
	if !onFloor(nm, 2.0, 2.0) {
		t.Fatal("floor under dynamic_crate should remain walkable")
	}
	if !onFloor(nm, 1.5, 4.5) {
		t.Fatal("floor inside zone_trigger should remain walkable")
	}
}

func TestSceneFilePrimitivesModelNodeAndModelMesh(t *testing.T) {
	// Verify all 5 built-in primitives work and an unknown primitive returns an error.
	var g navbake3d.Geometry
	g.AddPrimitive(sceneformat.MeshPlane, math3d.TRS(v3(5, 0, 5), math3d.QuatIdentity(), v3(10, 1, 10)), 1)
	if err := g.AddPrimitive(sceneformat.MeshSphere, math3d.TRS(v3(3, 0.5, 3), math3d.QuatIdentity(), v3(1.5, 1.5, 1.5)), 1); err != nil {
		t.Fatalf("AddPrimitive(sphere): %v", err)
	}
	if err := g.AddPrimitive(sceneformat.MeshCylinder, math3d.TRS(v3(7, 1, 3), math3d.QuatIdentity(), v3(1.4, 2, 1.4)), 1); err != nil {
		t.Fatalf("AddPrimitive(cylinder): %v", err)
	}
	if err := g.AddPrimitive(sceneformat.MeshCapsule, math3d.TRS(v3(5, 1, 7), math3d.QuatIdentity(), v3(1.2, 1.2, 1.2)), 1); err != nil {
		t.Fatalf("AddPrimitive(capsule): %v", err)
	}
	if err := g.AddPrimitive("pyramid", math3d.Mat4Identity(), 1); err == nil {
		t.Fatal("expected error for unknown primitive")
	}
	nm := bakeSmall(t, &g)
	if !onFloor(nm, 1.5, 1.5) {
		t.Fatal("plane floor should be walkable")
	}
	for _, blocked := range [][2]float32{{3, 3}, {7, 3}, {5, 7}} {
		if onFloor(nm, blocked[0], blocked[1]) {
			t.Errorf("floor inside primitive at %v should not be walkable", blocked)
		}
	}

	// Build a minimal glTF with two named meshes ("floor_mesh", "pillar_mesh") and nodes ("floor_node", "pillar_node").
	twoMeshGLTF := buildTwoQuadGLTF(t)
	fsys := fstest.MapFS{
		"models/parts.gltf": &fstest.MapFile{Data: twoMeshGLTF},
	}

	// Bake selecting only floor_mesh via ModelMesh.
	docMesh := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{{
				ID:        "only_floor",
				Model:     "models/parts.gltf",
				ModelMesh: "floor_mesh",
			}},
		}},
	}
	var gm navbake3d.Geometry
	if err := gm.AddSceneFile(docMesh, fsys); err != nil {
		t.Fatalf("AddSceneFile(ModelMesh): %v", err)
	}
	if gm.TriangleCount() != 2 {
		t.Fatalf("ModelMesh added %d triangles, want 2", gm.TriangleCount())
	}

	// Bake selecting only pillar_node via ModelNode.
	docNode := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{{
				ID:        "only_pillar",
				Model:     "models/parts.gltf",
				ModelNode: "pillar_node",
			}},
		}},
	}
	var gn navbake3d.Geometry
	if err := gn.AddSceneFile(docNode, fsys); err != nil {
		t.Fatalf("AddSceneFile(ModelNode): %v", err)
	}
	if gn.TriangleCount() != 2 {
		t.Fatalf("ModelNode added %d triangles, want 2", gn.TriangleCount())
	}
}

func TestAddTerrainFileAndGridMapFile(t *testing.T) {
	// Generate a flat 9x9 PNG heightmap in memory.
	img := image.NewGray(image.Rect(0, 0, 9, 9))
	for y := range 9 {
		for x := range 9 {
			img.SetGray(x, y, color.Gray{Y: 128})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}

	terrainYAML := `version: 1
name: flat_hill
heightmap: terrain/height.png
cellSize: 1
heightScale: 2
origin: [0, 0, 0]
layers:
  - name: grass
`
	fsys := fstest.MapFS{
		"terrain/height.png":        &fstest.MapFile{Data: pngBuf.Bytes()},
		"terrain/hill.terrain.yaml": &fstest.MapFile{Data: []byte(terrainYAML)},
	}

	var gt navbake3d.Geometry
	if err := gt.AddTerrainFile(fsys, "terrain/hill.terrain.yaml", 1); err != nil {
		t.Fatalf("AddTerrainFile: %v", err)
	}
	if gt.TriangleCount() == 0 {
		t.Fatal("AddTerrainFile added 0 triangles")
	}
	nm := bakeSmall(t, &gt)
	if _, ok := nm.FindPath(v3(2, 1, 2), v3(6, 1, 6)); !ok {
		t.Fatal("no path across baked terrain file")
	}

	// GridMap YAML referencing a minimal glTF mesh library with a floor tile.
	boxGLTF := buildUnitBoxGLTF(t, "stone_floor")
	gridYAML := `version: 1
name: dungeon
cellSize: [2, 2, 2]
meshLibrary: models/tiles.gltf
boxes:
  - min: [0, 0, 0]
    max: [3, 0, 3]
    tile: stone_floor
`
	gfs := fstest.MapFS{
		"models/tiles.gltf":          &fstest.MapFile{Data: boxGLTF},
		"levels/dungeon.gridmap.yaml": &fstest.MapFile{Data: []byte(gridYAML)},
	}
	var gg navbake3d.Geometry
	if err := gg.AddGridMapFile(gfs, "levels/dungeon.gridmap.yaml", 1); err != nil {
		t.Fatalf("AddGridMapFile: %v", err)
	}
	if gg.TriangleCount() == 0 {
		t.Fatal("AddGridMapFile added 0 triangles")
	}
	gnm := bakeSmall(t, &gg)
	if _, ok := gnm.FindPath(v3(1.5, 2, 1.5), v3(6.5, 2, 6.5)); !ok {
		t.Fatal("no path across baked GridMap file")
	}
}

func encodeFloat32s(vals ...float32) []byte {
	buf := make([]byte, len(vals)*4)
	for i, v := range vals {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func encodeUint16s(vals ...uint16) []byte {
	buf := make([]byte, len(vals)*2)
	for i, v := range vals {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	return buf
}

// buildTwoQuadGLTF builds a valid embedded glTF 2.0 document with two named meshes and two named nodes.
func buildTwoQuadGLTF(t *testing.T) []byte {
	t.Helper()
	pos := encodeFloat32s(
		// floor_mesh (0..3): 8x8 quad at y=0
		0, 0, 0, 0, 0, 8, 8, 0, 8, 8, 0, 0,
		// pillar_mesh (4..7): 2x2 quad at y=2
		3, 2, 3, 3, 2, 5, 5, 2, 5, 5, 2, 3,
	)
	idx := encodeUint16s(
		0, 1, 2, 0, 2, 3,
		0, 1, 2, 0, 2, 3,
	)
	bin := append(pos, idx...)
	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin)
	doc := fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0, 1]}],
  "nodes": [
    {"name": "floor_node", "mesh": 0},
    {"name": "pillar_node", "mesh": 1}
  ],
  "meshes": [
    {"name": "floor_mesh", "primitives": [{"attributes": {"POSITION": 0}, "indices": 2}]},
    {"name": "pillar_mesh", "primitives": [{"attributes": {"POSITION": 1}, "indices": 3}]}
  ],
  "buffers": [{"uri": %q, "byteLength": %d}],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": 48},
    {"buffer": 0, "byteOffset": 48, "byteLength": 48},
    {"buffer": 0, "byteOffset": 96, "byteLength": 12},
    {"buffer": 0, "byteOffset": 108, "byteLength": 12}
  ],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 4, "type": "VEC3", "min": [0,0,0], "max": [8,0,8]},
    {"bufferView": 1, "componentType": 5126, "count": 4, "type": "VEC3", "min": [3,2,3], "max": [5,2,5]},
    {"bufferView": 2, "componentType": 5123, "count": 6, "type": "SCALAR"},
    {"bufferView": 3, "componentType": 5123, "count": 6, "type": "SCALAR"}
  ]
}`, uri, len(bin))
	return []byte(doc)
}

// buildUnitBoxGLTF builds a minimal glTF 2.0 document with one named node whose mesh spans [-1,1]^3.
func buildUnitBoxGLTF(t *testing.T, nodeName string) []byte {
	t.Helper()
	pos := encodeFloat32s(
		-1, -1, -1, 1, -1, -1, 1, 1, -1, -1, 1, -1,
		-1, -1, 1, 1, -1, 1, 1, 1, 1, -1, 1, 1,
	)
	idx := encodeUint16s(
		3, 7, 6, 3, 6, 2, // +Y
		0, 1, 5, 0, 5, 4, // -Y
		1, 2, 6, 1, 6, 5, // +X
		0, 4, 7, 0, 7, 3, // -X
		4, 5, 6, 4, 6, 7, // +Z
		0, 3, 2, 0, 2, 1, // -Z
	)
	bin := append(pos, idx...)
	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin)
	doc := fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0]}],
  "nodes": [{"name": %q, "mesh": 0}],
  "meshes": [{"name": %q, "primitives": [{"attributes": {"POSITION": 0}, "indices": 1}]}],
  "buffers": [{"uri": %q, "byteLength": %d}],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": 96},
    {"buffer": 0, "byteOffset": 96, "byteLength": 72}
  ],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 8, "type": "VEC3", "min": [-1,-1,-1], "max": [1,1,1]},
    {"bufferView": 1, "componentType": 5123, "count": 36, "type": "SCALAR"}
  ]
}`, nodeName, nodeName, uri, len(bin))
	return []byte(doc)
}
