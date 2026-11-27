package refs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	// yardScene names models/crate.glb from an object, a child object and an override, and models/robot.gltf with one of its nodes.
	yardScene = `version: 2
name: Yard
overrides:
  hero:
    model: models/crate.glb
layers:
  - name: level
    objects:
      - id: crate
        model: models/crate.glb
        children:
          - id: lid
            model: "models/crate.glb"
            modelNode: Lid
      - id: robot
        model: models/robot.gltf
        modelMesh: Body
      - id: ball
        mesh: sphere
`
	// penScene instances yardScene and replaces one object's model.
	penScene = `version: 2
name: Pen
layers:
  - name: level
    objects:
      - id: yard
        scene: scenes/yard.scene.yaml
        overrides:
          crate:
            model: models/crate.glb
`
	// robotGLTF keeps its buffer in a file whose name is percent-encoded, one image beside it and another in a data URI, which names no file.
	robotGLTF = `{
  "asset": {"version": "2.0"},
  "buffers": [{"byteLength": 4, "uri": "robot%20data.bin"}],
  "images": [{"uri": "skin.png"}, {"uri": "data:image/png;base64,AAAA"}]
}
`
)

// modelFiles returns a project whose scenes name glTF models.
func modelFiles() fstest.MapFS {
	return fstest.MapFS{
		"game.yaml":              {Data: []byte(gameHead)},
		"scenes/yard.scene.yaml": {Data: []byte(yardScene)},
		"scenes/pen.scene.yaml":  {Data: []byte(penScene)},
		"models/crate.glb":       {Data: []byte("glTF")},
		"models/robot.gltf":      {Data: []byte(robotGLTF)},
		"models/robot data.bin":  {Data: []byte("data")},
		"models/skin.png":        {Data: []byte("skin")},
	}
}

func TestScanFindsModels(t *testing.T) {
	p := scan(t, modelFiles())
	var got []string
	for _, r := range p.Refs {
		got = append(got, fmt.Sprintf("%s:%d:%d %s %q -> %s", r.File, r.Line, r.Column, r.Field, r.Value, r.Target))
	}
	want := []string{
		`models/robot.gltf:3:40 model buffer "robot%20data.bin" -> models/robot data.bin`,
		`models/robot.gltf:4:22 model image "skin.png" -> models/skin.png`,
		`scenes/pen.scene.yaml:7:16 scene "scenes/yard.scene.yaml" -> scenes/yard.scene.yaml`,
		`scenes/pen.scene.yaml:10:20 model "models/crate.glb" -> models/crate.glb`,
		`scenes/yard.scene.yaml:5:12 model "models/crate.glb" -> models/crate.glb`,
		`scenes/yard.scene.yaml:10:16 model "models/crate.glb" -> models/crate.glb`,
		`scenes/yard.scene.yaml:13:20 model "models/crate.glb" -> models/crate.glb`,
		`scenes/yard.scene.yaml:16:16 model "models/robot.gltf" -> models/robot.gltf`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("refs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if probs := p.Check(); len(probs) != 0 {
		t.Fatalf("problems in a project whose models are all there: %v", probs)
	}
}

func TestPlanMoveModel(t *testing.T) {
	dir := dirFS(t, modelFiles())
	pl := move(t, dir, "models/crate.glb", "props/crate.glb")
	if len(pl.Updated) != 4 {
		t.Fatalf("updated %d references, want 4: %+v", len(pl.Updated), pl.Updated)
	}
	if got, want := read(t, dir, "scenes/yard.scene.yaml"), strings.ReplaceAll(yardScene, "models/crate.glb", "props/crate.glb"); got != want {
		t.Fatalf("yard scene:\n%s\nwant:\n%s", got, want)
	}
	if got, want := read(t, dir, "scenes/pen.scene.yaml"), strings.ReplaceAll(penScene, "models/crate.glb", "props/crate.glb"); got != want {
		t.Fatalf("pen scene:\n%s\nwant:\n%s", got, want)
	}
	clean(t, dir)
}

func TestPlanMoveGLTFKeepsItsURIsEncoded(t *testing.T) {
	dir := dirFS(t, modelFiles())
	// Moving the model's directory changes no path relative to the model, so the .gltf file is not rewritten.
	move(t, dir, "models", "art/models")
	if got := read(t, dir, "art/models/robot.gltf"); got != robotGLTF {
		t.Fatalf("robot.gltf changed when its directory moved:\n%s", got)
	}
	if got := read(t, dir, "scenes/yard.scene.yaml"); !strings.Contains(got, "model: art/models/robot.gltf\n") {
		t.Fatalf("yard scene:\n%s", got)
	}
	clean(t, dir)

	// Moving the buffer writes its new name percent-encoded, as glTF requires.
	move(t, dir, "art/models/robot data.bin", "art/models/bin/robot 100%.bin")
	if got, want := read(t, dir, "art/models/robot.gltf"), strings.Replace(robotGLTF, "robot%20data.bin", "bin/robot%20100%25.bin", 1); got != want {
		t.Fatalf("robot.gltf:\n%s\nwant:\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "art", "models", "bin", "robot 100%.bin")); err != nil {
		t.Fatal(err)
	}
	clean(t, dir)
}

func TestModelThatIsGoneIsAProblem(t *testing.T) {
	fsys := modelFiles()
	delete(fsys, "models/robot data.bin")
	var got []string
	for _, pr := range scan(t, fsys).Check() {
		got = append(got, fmt.Sprintf("%s:%d %s", pr.File, pr.Line, pr.Message))
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "models/robot.gltf:3 ") || !strings.Contains(got[0], `model buffer "robot%20data.bin"`) {
		t.Fatalf("problems %q, want one for the missing buffer", got)
	}
}
