package sceneformat_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// objectScene returns a scene of the given version with one object "o" whose fields are fields, one "key: value" per line.
func objectScene(version string, fields ...string) string {
	var b strings.Builder
	b.WriteString("version: " + version + "\nlayers:\n  - name: a\n    objects:\n      - id: o\n")
	for _, f := range fields {
		b.WriteString("        " + f + "\n")
	}
	return b.String()
}

func TestModelFieldsAreValidated(t *testing.T) {
	obj := []string{"layers", "0", "objects", "0"}
	cases := []struct {
		name, yaml string
		// field is the last step of the path the error names, and marker a text on the line it is reported at.
		field, marker, msg string
	}{
		{"v1_model", objectScene("1", "model: m.glb"), "model", "model: m.glb", "need version 2"},
		{"v1_model_node_alone", objectScene("1", "modelNode: Lid"), "modelNode", "modelNode: Lid", "need version 2"},
		{"v1_model_mesh_alone", objectScene("1", "modelMesh: LidMesh"), "modelMesh", "modelMesh: LidMesh", "need version 2"},
		{"v1_mesh_not_primitive", objectScene("1", "mesh: models/tree.glb"), "mesh", "mesh: models/tree.glb", "move the scene to version 2 and name the file in model"},
		{"mesh_not_primitive", objectScene("2", "mesh: cube"), "mesh", "mesh: cube", "name a glTF file in model instead"},
		{"mesh_and_model", objectScene("2", "mesh: box", "model: m.glb"), "mesh", "mesh: box", `names both mesh "box" and model "m.glb"`},
		{"node_and_mesh", objectScene("2", "model: m.glb", "modelNode: Lid", "modelMesh: LidMesh"), "modelMesh", "modelMesh: LidMesh", "names both modelNode"},
		{"node_without_model", objectScene("2", "modelNode: Lid"), "modelNode", "modelNode: Lid", `modelNode "Lid" requires model`},
		{"mesh_without_model", objectScene("2", "modelMesh: LidMesh"), "modelMesh", "modelMesh: LidMesh", `modelMesh "LidMesh" requires model`},
		{"model_escapes_root", objectScene("2", "model: ../x.glb"), "model", "model: ../x.glb", "must be a slash-separated path"},
		{"model_absolute", objectScene("2", "model: /x.glb"), "model", "model: /x.glb", "must be a slash-separated path"},
		{"model_not_gltf", objectScene("2", "model: x.obj"), "model", "model: x.obj", "must be a glTF file ending in .gltf or .glb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("Parse error = %v, want %q", err, tc.msg)
			}
			fe, ok := errors.AsType[*sceneformat.FieldError](err)
			if !ok || !slices.Equal(fe.Path, append(slices.Clip(obj), tc.field)) {
				t.Fatalf("Parse error %v, want a *FieldError at %q", err, append(slices.Clip(obj), tc.field))
			}
			le, ok := loaderr.As(err)
			if want := lineOf(tc.yaml, tc.marker); !ok || le.Line != want {
				t.Fatalf("Parse error %v reported at line %d, want %d", err, le.Line, want)
			}
		})
	}
}

// lineOf returns the 1-based line of src holding marker, or 0 when none does.
func lineOf(src, marker string) int {
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, marker) {
			return i + 1
		}
	}
	return 0
}

func TestOverrideSwapsPrimitiveForModel(t *testing.T) {
	scenes := newMemScenes(map[string]string{
		"yard.scene.yaml": objectScene("2", "mesh: box"),
	})
	swap := "version: 2\ninherits: yard.scene.yaml\noverrides:\n  o:\n    mesh: \"\"\n    model: models/crate.glb\n"
	doc, err := sceneformat.Parse([]byte(swap))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolved, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if o := resolved.Layers[0].Objects[0]; o.Mesh != "" || o.Model != "models/crate.glb" {
		t.Fatalf("resolved object mesh %q model %q, want the model alone", o.Mesh, o.Model)
	}
	if out, err := sceneformat.Encode(doc); err != nil || !strings.HasSuffix(string(out), "\n"+swap) {
		t.Fatalf("Encode = %q, %v; want the override kept as written:\n%s", out, err, swap)
	}

	// Without clearing mesh the merged object names both, which Resolve refuses at the override.
	kept, err := sceneformat.Parse([]byte("version: 2\ninherits: yard.scene.yaml\noverrides:\n  o:\n    model: models/crate.glb\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := sceneformat.Resolve(kept, scenes); err == nil || !strings.Contains(err.Error(), "names both mesh") {
		t.Fatalf("Resolve of an override that keeps the primitive = %v, want it refused", err)
	}
}

func TestOccluderFieldsAreValidatedAndRoundTrip(t *testing.T) {
	valid := `version: 2
layers:
  - name: world
    objects:
      - id: wall_mesh
        mesh: box
        occluder:
          mode: model
          doubleSided: true
      - id: wall_box
        occluder:
          mode: box
          center: [0, 1.5, 0]
          size: [6, 3, 0.5]
      - id: unit_box
        occluder:
          mode: box
`
	doc, err := sceneformat.Parse([]byte(valid))
	if err != nil {
		t.Fatalf("Parse valid occluders: %v", err)
	}
	enc, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if want := sceneformat.SchemaCommentV2 + "\n" + valid; string(enc) != want {
		t.Fatalf("Encode mismatch:\n--- got ---\n%s\n--- want ---\n%s", enc, want)
	}
	lo, hi := doc.Layers[0].Objects[1].Occluder.LocalBounds()
	if lo != ([3]float32{-3, 0, -0.25}) || hi != ([3]float32{3, 3, 0.25}) {
		t.Fatalf("LocalBounds = (%v, %v), want ([-3 0 -0.25], [3 3 0.25])", lo, hi)
	}
	if sz := doc.Layers[0].Objects[2].Occluder.EffectiveSize(); sz != ([3]float32{1, 1, 1}) {
		t.Fatalf("default EffectiveSize = %v, want [1 1 1]", sz)
	}

	cases := []struct {
		name, yaml, msg string
	}{
		{"v1_occluder", objectScene("1", "mesh: box", "occluder: {mode: box}"), "needs version 2"},
		{"bad_mode", objectScene("2", "occluder: {mode: sphere}"), `must be "model" or "box"`},
		{"model_without_geom", objectScene("2", "occluder: {mode: model}"), `requires mesh or model`},
		{"model_with_size", objectScene("2", "mesh: box", "occluder: {mode: model, size: [1, 1, 1]}"), `size requires mode "box"`},
		{"model_with_center", objectScene("2", "mesh: box", "occluder: {mode: model, center: [0, 1, 0]}"), `center requires mode "box"`},
		{"box_partial_zero_size", objectScene("2", "occluder: {mode: box, size: [4, 0, 1]}"), "all positive axes or all zero"},
		{"box_negative_size", objectScene("2", "occluder: {mode: box, size: [4, -1, 1]}"), "all positive axes or all zero"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("Parse error = %v, want %q", err, tc.msg)
			}
		})
	}
}
