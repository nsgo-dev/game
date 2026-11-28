package editormcp_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

// openOverrideProject opens a project whose open scene inherits a base scene and instances another, so it holds local, instanced and inherited objects.
func openOverrideProject(t *testing.T) *editor.Editor {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"game.yaml": `version: 1
name: Override Demo
id: dev.nsgo.overrides
appVersion: 0.1.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level
  scenes:
    - name: level
      path: scenes/level.scene.yaml
`,
		"scenes/slime.scene.yaml": `version: 2
layers:
  - name: actors
    objects:
      - id: body
        x: 10
        "y": 20
`,
		"scenes/base.scene.yaml": `version: 3
layers:
  - name: actors
    objects:
      - id: hero
        x: 40
        "y": 50
`,
		"scenes/level.scene.yaml": `version: 3
inherits: scenes/base.scene.yaml
layers:
  - name: actors
    objects:
      - id: local_box
        x: 10
        "y": 10
      - id: slime_1
        scene: scenes/slime.scene.yaml
        x: 40
        "y": 20
`,
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	return ed
}

// selection_get and scene_read with objects report each object's origin and the overrides entry an edit to an instanced or inherited object writes, as Editor.ObjectOverrideInfo routes it.
func TestObjectEditOrigins(t *testing.T) {
	ed := openOverrideProject(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	const scene = "scenes/level.scene.yaml"
	want := map[string]editormcp.ObjectEdit{
		"local_box":    {ID: "local_box", Origin: "local"},
		"slime_1":      {ID: "slime_1", Origin: "local", SourceScene: "scenes/slime.scene.yaml"},
		"slime_1/body": {ID: "slime_1/body", Origin: "instanced", SourceScene: "scenes/slime.scene.yaml", RecordsIn: &editormcp.EditRecord{File: scene, Instance: "slime_1", OverridesKey: "body"}},
		"hero":         {ID: "hero", Origin: "inherited", SourceScene: "scenes/base.scene.yaml", RecordsIn: &editormcp.EditRecord{File: scene, OverridesKey: "hero"}},
	}
	same := func(got, want editormcp.ObjectEdit) bool {
		if got.ID != want.ID || got.Origin != want.Origin || got.SourceScene != want.SourceScene || (got.RecordsIn == nil) != (want.RecordsIn == nil) {
			return false
		}
		return got.RecordsIn == nil || *got.RecordsIn == *want.RecordsIn
	}

	for id, w := range want {
		ed.SelectObject(id)
		var sel editormcp.Selection
		structured(t, call(t, cs, editormcp.ToolSelectionGet, map[string]any{}), &sel)
		if sel.Object == nil || !same(sel.Object.Edit, w) {
			t.Fatalf("selection_get on %s: %+v, want %+v", id, sel.Object, w)
		}
	}

	var plain editormcp.FileText
	structured(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{}), &plain)
	if plain.Objects != nil {
		t.Fatalf("scene_read without objects listed %+v", plain.Objects)
	}
	for _, args := range []map[string]any{{"objects": true}, {"file": scene, "objects": true}} {
		var read editormcp.FileText
		structured(t, call(t, cs, editormcp.ToolSceneRead, args), &read)
		var ids []string
		for _, o := range read.Objects {
			ids = append(ids, o.ID)
			if w, ok := want[o.ID]; ok && !same(o, w) {
				t.Fatalf("scene_read %v: %+v, want %+v", args, o, w)
			}
		}
		for id := range want {
			if !slices.Contains(ids, id) {
				t.Fatalf("scene_read %v lists %v, missing %s", args, ids, id)
			}
		}
	}

	res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "scenes/base.scene.yaml", "objects": true})
	if !res.IsError || !strings.Contains(text(res), "scene open in the editor only") {
		t.Fatalf("scene_read objects on a scene that is not open: %s", text(res))
	}
}
