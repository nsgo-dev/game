package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestReferencesSafeRenameMacOSScreenshots renders the Find Usages and Safe Rename review screen
// on the real native macOS host (linking github.com/nsgo-dev/nsgo through editorcmd) and captures
// usages, rename-review, go-warning, and done screenshots for visual review beside References.dc.html.
func TestReferencesSafeRenameMacOSScreenshots(t *testing.T) {
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
  body: { type: kinematic, layer: enemies }
  patrol: { from: 0, to: 0, speed: 24, wait: 0.5 }
  health: { max: 2, invulnerable: 0.6 }
  hurts_on_touch: { damage: 1, knockback: 120 }
`,
		"prefabs/slime_blue.prefab.yaml": `version: 1
inherits: prefabs/slime.prefab.yaml
name: slime_blue
components:
  patrol: { speed: 32 }
`,
		"prefabs/slime_king.prefab.yaml": `version: 1
inherits: prefabs/slime.prefab.yaml
name: slime_king
components:
  health: { max: 12 }
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
        x: 190
        y: 300
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 264
        y: 300
        components:
          patrol: { speed: 30 }
          health: { max: 3 }
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 420
        y: 210
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
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 96
        y: 300
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 180
        y: 180
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 370
        y: 300
      - id: slime_04
        prefab: prefabs/slime.prefab.yaml
        x: 520
        y: 300
        properties:
          tint: "#9FD8B4"
`,
		"scenes/level_03.scene.yaml": `version: 3
name: level_03
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: slime_pit_a
        prefab: prefabs/slime.prefab.yaml
        x: 140
        y: 300
      - id: slime_pit_b
        prefab: prefabs/slime.prefab.yaml
        x: 430
        y: 150
`,
		"scenes/boss_room.scene.yaml": `version: 3
name: boss_room
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: minion_left
        prefab: prefabs/slime.prefab.yaml
        x: 140
        y: 300
      - id: minion_right
        prefab: prefabs/slime.prefab.yaml
        x: 490
        y: 300
`,
		"scenes/sewer_01.scene.yaml": `version: 3
name: sewer_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 110
        y: 300
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 290
        y: 180
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 530
        y: 300
`,
		"data/spawn_waves.yaml": `waves:
  - id: wave_02
    prefab: prefabs/slime.prefab.yaml
    count: 4
    every: 2.5
  - id: wave_05
    prefab: prefabs/slime.prefab.yaml
    count: 8
    every: 1.5
`,
		"game/spawn.go": `package game

// slimePrefab is dropped into every open pit.
const slimePrefab = "prefabs/slime.prefab.yaml"

func spawnPitSlimes() string {
	return slimePrefab
}
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
	ed.SelectObject("slime_02")
	if err := ed.OpenUsagesScreen(""); err != nil {
		t.Fatalf("OpenUsagesScreen: %v", err)
	}
	ed.FocusUsage("scenes/level_01.scene.yaml", 1)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Screenshot("references-usages")

	app.Post(func() {
		_, _ = ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "slime_green")
		ed.FocusUsage("scenes/level_01.scene.yaml", 1)
	})
	app.Settle()
	app.Screenshot("references-rename-review")

	app.Press("id:refs-file-game/spawn.go")
	app.Screenshot("references-go-warning")

	app.Press("id:refs-apply-rename")
	app.Screenshot("references-done")
}
