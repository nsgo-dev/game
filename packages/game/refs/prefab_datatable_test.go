package refs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/refs"
)

func TestScanAndPlanMovePrefabVariantsAndDataTable(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"game.yaml": `version: 1
name: Dungeon Run
id: dev.example.dungeon
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`,
		"prefabs/slime.prefab.yaml": `version: 1
name: slime
components:
  health: { max: 2 }
`,
		"prefabs/slime_blue.prefab.yaml": `version: 1
inherits: prefabs/slime.prefab.yaml
name: slime_blue
`,
		"scenes/level_01.scene.yaml": `version: 3
name: level_01
layers:
  - name: actors
    space: world
    objects:
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 100
        y: 200
      - id: bare_prefab
        prefab: hero
        x: 40
        y: 40
`,
		"data/spawn_waves.yaml": `waves:
  - id: wave_02
    prefab: prefabs/slime.prefab.yaml
    count: 4
`,
	}
	for rel, body := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	proj, err := refs.Scan(os.DirFS(dir))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if probs := proj.Check(); len(probs) != 0 {
		t.Fatalf("unexpected Check problems (bare prefab name must be ignored): %v", probs)
	}

	var matched []refs.Ref
	for _, r := range proj.Refs {
		if r.Target == "prefabs/slime.prefab.yaml" {
			matched = append(matched, r)
		}
	}
	if len(matched) != 3 {
		t.Fatalf("matched refs = %d (%+v), want 3 (variant, scene, data table)", len(matched), matched)
	}

	plan, err := refs.PlanMove(os.DirFS(dir), "prefabs/slime.prefab.yaml", "prefabs/slime_green.prefab.yaml")
	if err != nil {
		t.Fatalf("PlanMove: %v", err)
	}
	if err := plan.Apply(dir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, checkPath := range []string{"prefabs/slime_blue.prefab.yaml", "scenes/level_01.scene.yaml", "data/spawn_waves.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(checkPath)))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "prefabs/slime.prefab.yaml") || !strings.Contains(string(data), "prefabs/slime_green.prefab.yaml") {
			t.Fatalf("%s was not rewritten:\n%s", checkPath, data)
		}
	}
}
