package sceneformat_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func makeTestGLTFBox(minV, maxV math3d.Vec3) []byte {
	verts := [8][3]float32{
		{minV.X, minV.Y, minV.Z},
		{maxV.X, minV.Y, minV.Z},
		{maxV.X, maxV.Y, minV.Z},
		{minV.X, maxV.Y, minV.Z},
		{minV.X, minV.Y, maxV.Z},
		{maxV.X, minV.Y, maxV.Z},
		{maxV.X, maxV.Y, maxV.Z},
		{minV.X, maxV.Y, maxV.Z},
	}
	indices := []uint16{
		4, 5, 6, 4, 6, 7,
		1, 0, 3, 1, 3, 2,
		5, 1, 2, 5, 2, 6,
		0, 4, 7, 0, 7, 3,
		7, 6, 2, 7, 2, 3,
		0, 1, 5, 0, 5, 4,
	}
	var bin bytes.Buffer
	for _, v := range verts {
		_ = binary.Write(&bin, binary.LittleEndian, v)
	}
	idxOffset := bin.Len()
	for _, ix := range indices {
		_ = binary.Write(&bin, binary.LittleEndian, ix)
	}
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	b64 := base64.StdEncoding.EncodeToString(bin.Bytes())
	jsonStr := fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0]}],
  "nodes": [{"name": "CrateNode", "mesh": 0}],
  "meshes": [{
    "name": "CrateMesh",
    "primitives": [{
      "attributes": {"POSITION": 0},
      "indices": 1
    }]
  }],
  "buffers": [{"uri": "data:application/octet-stream;base64,%s", "byteLength": %d}],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": %d, "target": 34962},
    {"buffer": 0, "byteOffset": %d, "byteLength": %d, "target": 34963}
  ],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 8, "type": "VEC3", "min": [%g,%g,%g], "max": [%g,%g,%g]},
    {"bufferView": 1, "componentType": 5123, "count": %d, "type": "SCALAR"}
  ]
}`, b64, bin.Len(), idxOffset, idxOffset, len(indices)*2,
		minV.X, minV.Y, minV.Z, maxV.X, maxV.Y, maxV.Z, len(indices))
	return []byte(jsonStr)
}

func makeFlatHeightmapPNG(w, h int, gray uint8) []byte {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetGray(x, y, color.Gray{Y: gray})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

type physics3DTestResolver struct {
	sceneformat.FSAssets
	lib *resource.Library
}

func (r physics3DTestResolver) PhysicsMaterial(p string) (*resource.PhysicsMaterial, error) {
	return r.lib.PhysicsMaterial(p)
}

func TestBody3DTriggerLayerMaskAndPhysicsMaterial(t *testing.T) {
	layers := testPhysicsLayers(t)
	reg := sceneformat.PhysicsComponentRegistry()

	const src = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v3.json
version: 3
name: 3D Trigger Body
layers:
  - name: world
    space: world
    objects:
      - id: runner
        "y": 1
        components:
          body3d:
            gravityScale: 0
            kind: dynamic
            layer: player
            mask:
              - coins
              - walls
            mass: 1
            physicsMaterial: materials/bouncy.physics.yaml
            version: 1
            vx: 6
          collider3d:
            radius: 0.5
            shape: sphere
            version: 1
      - id: sensor
        x: 2
        "y": 1
        components:
          body3d:
            kind: trigger
            layer: coins
            mask:
              - player
            version: 1
          collider3d:
            depth: 1
            height: 2
            shape: box
            version: 1
            width: 1
`
	doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{
		File:          "scenes/trigger3d.scene.yaml",
		Components:    reg,
		PhysicsLayers: layers,
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, []byte(src)) {
		t.Fatalf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", out, src)
	}

	fsys := fstest.MapFS{
		"materials/bouncy.physics.yaml": &fstest.MapFile{Data: []byte("type: physicsMaterial\nversion: 1\nfriction: 0.15\nrestitution: 0.9\n")},
	}
	resolver := physics3DTestResolver{
		FSAssets: sceneformat.FSAssets{FS: fsys},
		lib:      resource.NewLibrary(fsys),
	}

	built, err := sceneformat.Build(doc, resolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ps, err := sceneformat.BuildPhysics(built, &sceneformat.PhysicsSettings{Layers: layers}, resolver)
	if err != nil {
		t.Fatalf("BuildPhysics: %v", err)
	}

	runner := ps.Bodies3D["runner"]
	sensor := ps.Bodies3D["sensor"]
	if runner == nil || sensor == nil {
		t.Fatalf("missing 3D bodies: runner=%v sensor=%v", runner, sensor)
	}
	if !sensor.IsTrigger || !sensor.Static {
		t.Fatalf("sensor body3d kind=trigger: Static=%v IsTrigger=%v, want true/true", sensor.Static, sensor.IsTrigger)
	}
	if runner.Restitution != 0.9 || runner.Friction != 0.15 {
		t.Fatalf("runner physicsMaterial not applied: friction=%v restitution=%v", runner.Friction, runner.Restitution)
	}
	if runner.Layer != physics3d.LayerBit(0) || sensor.Layer != physics3d.LayerBit(2) {
		t.Fatalf("body3d layers not applied: runner=%d sensor=%d", runner.Layer, sensor.Layer)
	}

	var entered, exited bool
	ps.OnTrigger = func(ev sceneformat.PhysicsTriggerEvent) {
		if ev.Is3D && ev.TriggerID == "sensor" && ev.BodyID == "runner" {
			if ev.Kind == sceneformat.TriggerEnter {
				entered = true
			}
			if ev.Kind == sceneformat.TriggerExit {
				exited = true
			}
		}
	}
	for range 60 {
		ps.Step(time.Second / 60)
	}
	if !entered || !exited {
		t.Fatalf("expected trigger enter & exit on body3d kind=trigger, got enter=%v exit=%v", entered, exited)
	}
}

func TestCollider3DParseWithValidatesFieldsWithFileAndLine(t *testing.T) {
	reg := sceneformat.PhysicsComponentRegistry()
	layers := testPhysicsLayers(t)

	cases := []struct {
		name     string
		yaml     string
		wantLine int
		wantSub  string
		loadErr  bool
	}{
		{
			name:     "unknown collider3d field",
			yaml:     "version: 3\nlayers:\n  - name: main\n    objects:\n      - id: crate\n        components:\n          collider3d:\n            shape: box\n            widht: 2\n",
			wantLine: 9,
			wantSub:  `field "widht": unknown field`,
		},
		{
			name:     "invalid collider3d shape enum",
			yaml:     "version: 3\nlayers:\n  - name: main\n    objects:\n      - id: crate\n        components:\n          collider3d:\n            shape: pyramid\n",
			wantLine: 8,
			wantSub:  "must be one of box, sphere, capsule, cylinder, convex, trimesh, heightfield",
		},
		{
			name:     "invalid body3d kind enum",
			yaml:     "version: 3\nlayers:\n  - name: main\n    objects:\n      - id: crate\n        components:\n          body3d:\n            kind: floating\n",
			wantLine: 8,
			wantSub:  "must be one of static, kinematic, dynamic, trigger",
		},
		{
			name:     "negative collider3d width",
			yaml:     "version: 3\nlayers:\n  - name: main\n    objects:\n      - id: crate\n        components:\n          collider3d:\n            shape: box\n            width: -1\n",
			wantLine: 9,
			wantSub:  "below the minimum 0",
		},
		{
			name:     "wrong kind for rotY",
			yaml:     "version: 3\nlayers:\n  - name: main\n    objects:\n      - id: crate\n        components:\n          collider3d:\n            shape: box\n            rotY: sideways\n",
			wantLine: 9,
			wantSub:  "want float",
		},
		{
			name:     "unknown body3d layer",
			yaml:     "version: 3\nlayers:\n  - name: main\n    objects:\n      - id: crate\n        components:\n          body3d:\n            kind: static\n            layer: ghost\n",
			wantLine: 9,
			wantSub:  `unknown physics layer "ghost"`,
			loadErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.ParseWith([]byte(tc.yaml), sceneformat.ParseOptions{
				File:          "scenes/level3d.scene.yaml",
				Components:    reg,
				PhysicsLayers: layers,
			})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %q, want substring %q", err.Error(), tc.wantSub)
			}
			if tc.loadErr {
				var le *loaderr.Error
				if !errors.As(err, &le) || le.File != "scenes/level3d.scene.yaml" || le.Line != tc.wantLine {
					t.Fatalf("got loaderr %v, want scenes/level3d.scene.yaml:%d", err, tc.wantLine)
				}
				return
			}
			var ce *sceneformat.ComponentError
			if !errors.As(err, &ce) || ce.File != "scenes/level3d.scene.yaml" || ce.Line != tc.wantLine {
				t.Fatalf("got ComponentError %v, want scenes/level3d.scene.yaml:%d", err, tc.wantLine)
			}
		})
	}
}

func TestBuildPhysics3DBoxAndCylinderOffsetAndRotation(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version: 3,
		Layers: []sceneformat.LayerDoc{{
			Name: "main",
			Objects: []sceneformat.ObjectDoc{
				{
					// Static box rotated 45 degrees around Y and offset by +2 along X.
					ID: "rotated_box", X: 0, Y: 0, PosZ: 0,
					Components: map[string]any{
						"body3d": map[string]any{"version": 1, "kind": "static"},
						"collider3d": map[string]any{
							"version": 1, "shape": "box",
							"width": 4.0, "height": 1.0, "depth": 1.0,
							"offsetX": 2.0, "rotY": math.Pi / 2,
						},
					},
				},
				{
					// Static cylinder standing at X=-4 with Y offset +1.
					ID: "pillar", X: -4, Y: 0, PosZ: 0,
					Components: map[string]any{
						"collider3d": map[string]any{
							"version": 1, "shape": "cylinder",
							"radius": 0.75, "height": 2.0,
							"offsetY": 1.0,
						},
					},
				},
				{
					// Dynamic ball dropped above the rotated box's +Z wing (at X=2, Z=1.5), which only exists because rotY=pi/2!
					ID: "ball_box", X: 2.0, Y: 2.0, PosZ: 1.5,
					Components: map[string]any{
						"body3d":     map[string]any{"version": 1, "kind": "dynamic", "mass": 1.0},
						"collider3d": map[string]any{"version": 1, "shape": "sphere", "radius": 0.4},
					},
				},
				{
					// Dynamic ball dropped onto the top of the cylinder at Y=2 (offsetY=1 + halfHeight=1).
					ID: "ball_cyl", X: -4.0, Y: 3.0, PosZ: 0,
					Components: map[string]any{
						"body3d":     map[string]any{"version": 1, "kind": "dynamic", "mass": 1.0},
						"collider3d": map[string]any{"version": 1, "shape": "sphere", "radius": 0.4},
					},
				},
			},
		}},
	}

	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := sceneformat.BuildPhysics(built, nil, nil)
	if err != nil {
		t.Fatalf("BuildPhysics: %v", err)
	}

	// Verify ExtractPhysicsDebug3D matches built WorldShape for both box and cylinder.
	boxBody := ps.Bodies3D["rotated_box"]
	boxDebug := sceneformat.ExtractPhysicsDebug3D(&doc.Layers[0].Objects[0])
	if len(boxDebug) != 1 || boxDebug[0].Shape.Kind != physics3d.ShapeBox {
		t.Fatalf("unexpected box debug: %+v", boxDebug)
	}
	ws := boxBody.WorldShape()
	if boxDebug[0].Shape.Box != ws.Box || boxDebug[0].Shape.Rot != ws.Rot {
		t.Fatalf("debug box %+v != runtime WorldShape %+v", boxDebug[0].Shape, ws)
	}

	cylBody := ps.Bodies3D["pillar"]
	if cylBody.Shape.Kind != physics3d.ShapeCylinder {
		t.Fatalf("pillar shape kind = %v, want ShapeCylinder", cylBody.Shape.Kind)
	}

	for range 90 {
		ps.Step(time.Second / 60)
	}
	// ball_box must rest on top of rotated_box (top face Y=0.5, radius=0.4 => center Y ~0.9).
	if y := built.Object("ball_box").Doc.Y; y < 0.7 || y > 1.2 {
		t.Fatalf("ball_box Y = %v, expected resting on rotated/offset box (~0.9)", y)
	}
	// ball_cyl must rest on top of pillar (top face Y=2.0, radius=0.4 => center Y ~2.4).
	if y := built.Object("ball_cyl").Doc.Y; y < 2.2 || y > 2.7 {
		t.Fatalf("ball_cyl Y = %v, expected resting on cylinder top (~2.4)", y)
	}
}

func TestBuildPhysics3DConvexTriMeshAndHeightfield(t *testing.T) {
	crateGLTF := makeTestGLTFBox(math3d.Vec3{X: -1, Y: 0, Z: -1}, math3d.Vec3{X: 1, Y: 1, Z: 1})
	hmPNG := makeFlatHeightmapPNG(5, 5, 128) // ~0.502 * heightScale(4) => ~2.008
	terrainYAML := `version: 1
name: Meadow
heightmap: terrain/height.png
cellSize: 2
heightScale: 4
origin: [0, 0, 0]
layers:
  - name: grass
`
	fsys := fstest.MapFS{
		"models/crate.gltf":           &fstest.MapFile{Data: crateGLTF},
		"terrain/height.png":          &fstest.MapFile{Data: hmPNG},
		"terrain/meadow.terrain.yaml": &fstest.MapFile{Data: []byte(terrainYAML)},
	}
	resolver := sceneformat.FSAssets{FS: fsys}

	doc := &sceneformat.SceneDoc{
		Version: 3,
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID: "convex_crate", X: 0, Y: 0, PosZ: 0,
					Model: "models/crate.gltf",
					Components: map[string]any{
						"body3d":     map[string]any{"version": 1, "kind": "static"},
						"collider3d": map[string]any{"version": 1, "shape": "convex", "maxVertices": 32},
					},
				},
				{
					ID: "trimesh_platform", X: 6, Y: 0, PosZ: 0,
					Model: "models/crate.gltf",
					Components: map[string]any{
						"body3d":     map[string]any{"version": 1, "kind": "static"},
						"collider3d": map[string]any{"version": 1, "shape": "trimesh"},
					},
				},
				{
					ID: "ground", X: 12, Y: 0, PosZ: 0,
					Components: map[string]any{
						"collider3d": map[string]any{"version": 1, "shape": "heightfield", "terrain": "terrain/meadow.terrain.yaml"},
					},
				},
				{
					ID: "ball_convex", X: 0, Y: 2.5, PosZ: 0,
					Components: map[string]any{
						"body3d":     map[string]any{"version": 1, "kind": "dynamic", "mass": 1.0},
						"collider3d": map[string]any{"version": 1, "shape": "sphere", "radius": 0.4},
					},
				},
				{
					ID: "ball_trimesh", X: 6, Y: 2.5, PosZ: 0,
					Components: map[string]any{
						"body3d":     map[string]any{"version": 1, "kind": "dynamic", "mass": 1.0},
						"collider3d": map[string]any{"version": 1, "shape": "sphere", "radius": 0.4},
					},
				},
				{
					ID: "ball_terrain", X: 15, Y: 4.0, PosZ: 3,
					Components: map[string]any{
						"body3d":     map[string]any{"version": 1, "kind": "dynamic", "mass": 1.0},
						"collider3d": map[string]any{"version": 1, "shape": "sphere", "radius": 0.4},
					},
				},
			},
		}},
	}

	built, err := sceneformat.Build(doc, resolver)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := sceneformat.BuildPhysics(built, nil, resolver)
	if err != nil {
		t.Fatalf("BuildPhysics: %v", err)
	}

	if b := ps.Bodies3D["convex_crate"]; b == nil || b.Shape.Kind != physics3d.ShapeConvexHull {
		t.Fatalf("convex_crate body = %+v, want ShapeConvexHull", b)
	}
	if tm := ps.TriMeshes3D["trimesh_platform"]; tm == nil || len(tm.Triangles) == 0 {
		t.Fatalf("trimesh_platform = %+v, want non-empty TriMeshCollider", tm)
	}
	if hf := ps.Heightfields3D["ground"]; hf == nil || hf.Cols != 5 || hf.Rows != 5 {
		t.Fatalf("ground heightfield = %+v, want 5x5 HeightfieldCollider", hf)
	}

	for range 120 {
		ps.Step(time.Second / 60)
	}
	// Crate top is at Y=1.0, ball radius=0.4 => center ~1.4.
	if y := built.Object("ball_convex").Doc.Y; y < 1.2 || y > 1.7 {
		t.Fatalf("ball_convex Y = %v, expected resting on convex hull (~1.4)", y)
	}
	if y := built.Object("ball_trimesh").Doc.Y; y < 1.2 || y > 1.7 {
		t.Fatalf("ball_trimesh Y = %v, expected resting on trimesh (~1.4)", y)
	}
	// Terrain height is ~2.008, ball radius=0.4 => center ~2.41.
	if y := built.Object("ball_terrain").Doc.Y; y < 2.2 || y > 2.7 {
		t.Fatalf("ball_terrain Y = %v, expected resting on heightfield (~2.41)", y)
	}

	// Dynamic trimesh and dynamic heightfield must be refused cleanly.
	badTri := &sceneformat.SceneDoc{
		Version: 3,
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{{
				ID: "bad", Mesh: "box",
				Components: map[string]any{
					"body3d":     map[string]any{"version": 1, "kind": "dynamic"},
					"collider3d": map[string]any{"version": 1, "shape": "trimesh"},
				},
			}},
		}},
	}
	builtBad, _ := sceneformat.Build(badTri, resolver)
	if _, err := sceneformat.BuildPhysics(builtBad, nil, resolver); err == nil || !strings.Contains(err.Error(), "static") {
		t.Fatalf("dynamic trimesh err = %v, want static-only error", err)
	}

	// Custom ModelResolver and TerrainResolver interfaces also work directly.
	var customModel *gltf.Document
	customModel, err = gltf.LoadFS(fsys, "models/crate.gltf")
	if err != nil {
		t.Fatal(err)
	}
	hm, _, err := world3d.LoadTerrainFS(fsys, "terrain/meadow.terrain.yaml")
	if err != nil {
		t.Fatal(err)
	}
	customRes := struct {
		sceneformat.AtlasMap
		model   *gltf.Document
		terrain *world3d.Heightmap
	}{model: customModel, terrain: hm}
	_ = customRes
}

type staticModelResolver struct {
	sceneformat.AtlasMap
	models map[string]*gltf.Document
}

func (r staticModelResolver) Model(path string) (*gltf.Document, error) {
	if doc, ok := r.models[path]; ok {
		return doc, nil
	}
	return nil, fmt.Errorf("missing model %q", path)
}

func TestBuildPhysics3DCyclicGLTFNodeGraphDoesNotOverflow(t *testing.T) {
	crateGLTF := makeTestGLTFBox(math3d.Vec3{X: -1, Y: 0, Z: -1}, math3d.Vec3{X: 1, Y: 1, Z: 1})
	docGLTF, err := gltf.LoadFS(fstest.MapFS{"models/cyclic.gltf": &fstest.MapFile{Data: crateGLTF}}, "models/cyclic.gltf")
	if err != nil {
		t.Fatalf("gltf.LoadFS: %v", err)
	}
	// Introduce a 2-node cycle: node 0 -> node 1 -> node 0.
	docGLTF.Nodes = append(docGLTF.Nodes, gltf.Node{Name: "child", Children: []int{0}})
	docGLTF.Nodes[0].Children = append(docGLTF.Nodes[0].Children, 1)

	res := staticModelResolver{models: map[string]*gltf.Document{"models/cyclic.gltf": docGLTF}}
	doc := &sceneformat.SceneDoc{
		Version: 3,
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID: "cyclic_convex", Model: "models/cyclic.gltf",
					Components: map[string]any{
						"body3d":     map[string]any{"version": 1, "kind": "static"},
						"collider3d": map[string]any{"version": 1, "shape": "convex"},
					},
				},
				{
					ID: "cyclic_trimesh", X: 5, Model: "models/cyclic.gltf",
					Components: map[string]any{
						"body3d":     map[string]any{"version": 1, "kind": "static"},
						"collider3d": map[string]any{"version": 1, "shape": "trimesh"},
					},
				},
			},
		}},
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ps, err := sceneformat.BuildPhysics(built, nil, res)
	if err != nil {
		t.Fatalf("BuildPhysics on cyclic glTF: %v", err)
	}
	if b := ps.Bodies3D["cyclic_convex"]; b == nil || b.Shape.Kind != physics3d.ShapeConvexHull {
		t.Fatalf("cyclic_convex = %+v, want ShapeConvexHull", b)
	}
	if tm := ps.TriMeshes3D["cyclic_trimesh"]; tm == nil || len(tm.Triangles) == 0 {
		t.Fatalf("cyclic_trimesh = %+v, want non-empty TriMeshCollider", tm)
	}
}
