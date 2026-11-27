package editormcp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

func setupRefsProject(t *testing.T) (string, *editor.Editor) {
	t.Helper()
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
  health:
    max: 2
`,
		"prefabs/slime_blue.prefab.yaml": `version: 1
inherits: prefabs/slime.prefab.yaml
name: slime_blue
`,
		"scenes/level_01.scene.yaml": `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 220
        y: 340
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 284
        y: 340
        components:
          patrol:
            speed: 30
`,
		"scenes/level_02.scene.yaml": `version: 3
name: level_02
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: slime_a
        prefab: prefabs/slime.prefab.yaml
        x: 100
        y: 200
`,
		"data/spawn_waves.yaml": `waves:
  - id: wave_02
    prefab: prefabs/slime.prefab.yaml
    count: 4
`,
		"game/spawn.go": `package game

const slimePrefab = "prefabs/slime.prefab.yaml"
`,
	}
	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	return dir, ed
}

func TestMCPRefsFindAndRefactorRenameReview(t *testing.T) {
	dir, ed := setupRefsProject(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. refs_find returns all 4 groups and opens the References screen when requested.
	res := call(t, cs, editormcp.ToolRefsFind, map[string]any{
		"file": "prefabs/slime.prefab.yaml",
		"open": true,
	})
	txt := mustOK(t, res)
	var found editormcp.RefsFindResult
	structured(t, res, &found)
	if !found.Open || !ed.UsagesScreenOpen() {
		t.Fatalf("expected References screen open after refs_find(open=true)")
	}
	if len(found.Scenes) != 2 || len(found.Variants) != 1 || len(found.DataTables) != 1 || len(found.GoCode) != 1 {
		t.Fatalf("unexpected groups: scenes=%d variants=%d data=%d go=%d", len(found.Scenes), len(found.Variants), len(found.DataTables), len(found.GoCode))
	}
	if !strings.Contains(txt, "game/spawn.go") {
		t.Fatalf("expected Go warning in refs_find text: %s", txt)
	}

	// 2. refactor_rename without apply prepares the review WITHOUT writing anything to disk.
	res = call(t, cs, editormcp.ToolRefactorRename, map[string]any{
		"from": "prefabs/slime.prefab.yaml",
		"to":   "slime_green",
	})
	_ = mustOK(t, res)
	var plan editormcp.RefactorRenameResult
	structured(t, res, &plan)
	if plan.Applied || !plan.ReviewOpen || !plan.Valid {
		t.Fatalf("unexpected review plan state: %+v", plan)
	}
	if plan.To != "prefabs/slime_green.prefab.yaml" {
		t.Fatalf("To = %q, want prefabs/slime_green.prefab.yaml", plan.To)
	}
	if plan.RewrittenFileCount != 4 || plan.TotalTouchedFiles != 5 {
		t.Fatalf("RewrittenFileCount=%d TotalTouchedFiles=%d, want 4 and 5", plan.RewrittenFileCount, plan.TotalTouchedFiles)
	}
	if len(plan.Problems) != 1 || !plan.Problems[0].NotRewritten || plan.Problems[0].File != "game/spawn.go" {
		t.Fatalf("expected 1 non-rewritten Go problem, got %+v", plan.Problems)
	}
	// Disk MUST still be completely untouched before Keep/apply!
	if _, err := os.Stat(filepath.Join(dir, "prefabs", "slime.prefab.yaml")); err != nil {
		t.Fatalf("old prefab disappeared before apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prefabs", "slime_green.prefab.yaml")); !os.IsNotExist(err) {
		t.Fatalf("new prefab was created before apply")
	}

	// 3. Applying via editor.ApplySafeRename (Keep) writes YAML only, leaves Go untouched, and 1 Undo restores everything.
	if err := ed.ApplySafeRename(); err != nil {
		t.Fatalf("ApplySafeRename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prefabs", "slime_green.prefab.yaml")); err != nil {
		t.Fatalf("new prefab missing after Keep: %v", err)
	}
	goBytes, err := os.ReadFile(filepath.Join(dir, "game", "spawn.go"))
	if err != nil || !strings.Contains(string(goBytes), `"prefabs/slime.prefab.yaml"`) {
		t.Fatalf("Go file should not have been rewritten: %s", goBytes)
	}
	if !ed.Undo() {
		t.Fatalf("Undo failed after ApplySafeRename")
	}
	if _, err := os.Stat(filepath.Join(dir, "prefabs", "slime.prefab.yaml")); err != nil {
		t.Fatalf("old prefab missing after Undo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prefabs", "slime_green.prefab.yaml")); !os.IsNotExist(err) {
		t.Fatalf("new prefab still exists after Undo")
	}

	// 4. Verify refactor_rename with apply: true works end-to-end as well.
	res = call(t, cs, editormcp.ToolRefactorRename, map[string]any{
		"from":  "prefabs/slime.prefab.yaml",
		"to":    "prefabs/slime_green.prefab.yaml",
		"apply": true,
	})
	_ = mustOK(t, res)
	var applied editormcp.RefactorRenameResult
	structured(t, res, &applied)
	if !applied.Applied || len(applied.CheckProblems) != 1 {
		t.Fatalf("expected applied=true and 1 check problem for game/spawn.go, got %+v", applied)
	}
	_ = context.Background()
}
