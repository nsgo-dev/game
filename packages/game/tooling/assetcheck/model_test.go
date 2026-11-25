package assetcheck_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
	"testing/fstest"
)

// triangleGLTF returns a glTF model in JSON form with one triangle held in a data URI: node "Body" draws mesh "BodyMesh", and its child node "Hat" draws it too.
func triangleGLTF() string {
	var bin bytes.Buffer
	for _, f := range []float32{0, 0, 0, 1, 0, 0, 0, 1, 0} {
		_ = binary.Write(&bin, binary.LittleEndian, math.Float32bits(f))
	}
	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin.Bytes())
	return fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0]}],
  "nodes": [
    {"name": "Body", "mesh": 0, "children": [1]},
    {"name": "Hat", "mesh": 0, "translation": [0, 1, 0]}
  ],
  "meshes": [{"name": "BodyMesh", "primitives": [{"attributes": {"POSITION": 0}}]}],
  "accessors": [{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3", "min": [0, 0, 0], "max": [1, 1, 0]}],
  "bufferViews": [{"buffer": 0, "byteLength": 36}],
  "buffers": [{"byteLength": 36, "uri": %q}]
}
`, uri)
}

// modelScene is a version 2 scene whose objects draw the model, one node and one mesh of it, and a primitive.
const modelScene = `version: 2
name: Yard
layers:
  - name: level
    objects:
      - id: whole
        model: models/robot.gltf
      - id: body
        model: models/robot.gltf
        modelNode: Body
      - id: shape
        model: models/robot.gltf
        modelMesh: BodyMesh
      - id: crate
        mesh: box
        material: materials/stone.material.yaml
`

// modelProject returns the project of project with modelScene and the model it names.
func modelProject(t testing.TB) fstest.MapFS {
	fsys := project(t)
	fsys["scenes/yard.scene.yaml"] = &fstest.MapFile{Data: []byte(modelScene)}
	fsys["models/robot.gltf"] = &fstest.MapFile{Data: []byte(triangleGLTF())}
	return fsys
}

func TestModelsInScenes(t *testing.T) {
	r := mustCheck(t, modelProject(t))
	if !r.OK() {
		t.Fatalf("problems in a project whose models resolve:\n%s", problems(r))
	}
	cases := []struct {
		name string
		edit func(fsys fstest.MapFS)
		// file and marker give where the first problem is reported, the line holding marker, and msg what every problem's message contains.
		file, marker, msg string
		// count is how many problems there are, one per object that names what is wrong.
		count int
	}{
		{"missing model", func(fsys fstest.MapFS) { delete(fsys, "models/robot.gltf") }, "scenes/yard.scene.yaml", "model: models/robot.gltf", `model "models/robot.gltf": not found`, 3},
		{"missing node", func(fsys fstest.MapFS) {
			replace(t, fsys, "scenes/yard.scene.yaml", "modelNode: Body", "modelNode: Tail")
		}, "scenes/yard.scene.yaml", "modelNode: Tail", `modelNode "Tail" is not a node of model "models/robot.gltf"`, 1},
		{"missing mesh", func(fsys fstest.MapFS) {
			replace(t, fsys, "scenes/yard.scene.yaml", "modelMesh: BodyMesh", "modelMesh: TailMesh")
		}, "scenes/yard.scene.yaml", "modelMesh: TailMesh", `modelMesh "TailMesh" is not a mesh of model "models/robot.gltf"`, 1},
		{"model does not load", func(fsys fstest.MapFS) { replace(t, fsys, "models/robot.gltf", `"count": 3`, `"count": 30`) }, "models/robot.gltf", `"meshes"`, "accessor", 1},
		{"model not glTF", func(fsys fstest.MapFS) {
			replace(t, fsys, "scenes/yard.scene.yaml", "        model: models/robot.gltf\n        modelMesh", "        model: models/robot.obj\n        modelMesh")
		}, "scenes/yard.scene.yaml", "model: models/robot.obj", "must be a glTF file ending in .gltf or .glb", 1},
		{"unknown primitive", func(fsys fstest.MapFS) { replace(t, fsys, "scenes/yard.scene.yaml", "mesh: box", "mesh: cube") }, "scenes/yard.scene.yaml", "mesh: cube", `mesh "cube" is not a built-in primitive`, 1},
		{"override names a missing model", func(fsys fstest.MapFS) {
			fsys["scenes/pen.scene.yaml"] = &fstest.MapFile{Data: []byte("version: 2\nname: Pen\nlayers:\n  - name: level\n    objects:\n      - id: yard\n        scene: scenes/yard.scene.yaml\n        overrides:\n          whole:\n            model: models/gone.glb\n")}
		}, "scenes/pen.scene.yaml", "model: models/gone.glb", `model "models/gone.glb": not found`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := modelProject(t)
			tc.edit(fsys)
			r := mustCheck(t, fsys)
			count := tc.count
			if len(r.Problems) != count {
				t.Fatalf("%d problems, want %d:\n%s", len(r.Problems), count, problems(r))
			}
			want := lineOf(t, string(fsys[tc.file].Data), tc.marker)
			if p := r.Problems[0]; p.File != tc.file || p.Line != want {
				t.Fatalf("problem %q, want it at %s:%d", p, tc.file, want)
			}
			for _, p := range r.Problems {
				if p.File != tc.file || !strings.Contains(p.Message, tc.msg) {
					t.Fatalf("problem %q, want one in %s with %q", p, tc.file, tc.msg)
				}
			}
		})
	}
}

func TestModelBufferIsAReference(t *testing.T) {
	fsys := modelProject(t)
	gltf := string(fsys["models/robot.gltf"].Data)
	start := strings.Index(gltf, `"data:`)
	end := strings.Index(gltf[start+1:], `"`) + start + 2
	fsys["models/robot.gltf"] = &fstest.MapFile{Data: []byte(gltf[:start] + `"robot%20data.bin"` + gltf[end:])}
	r := mustCheck(t, fsys)
	var found bool
	for _, p := range r.Problems {
		if p.File == "models/robot.gltf" && p.Line == lineOf(t, string(fsys["models/robot.gltf"].Data), "robot%20data.bin") && strings.Contains(p.Message, `model buffer "robot%20data.bin": not found`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no problem for the missing buffer file:\n%s", problems(r))
	}
}

// replace replaces the first old in fsys's file name with new, failing when it is not there.
func replace(t testing.TB, fsys fstest.MapFS, name, old, new string) {
	t.Helper()
	text := string(fsys[name].Data)
	if !strings.Contains(text, old) {
		t.Fatalf("%s does not contain %q", name, old)
	}
	fsys[name] = &fstest.MapFile{Data: []byte(strings.Replace(text, old, new, 1))}
}
