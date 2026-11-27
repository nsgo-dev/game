package editorcmd_test

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestObjectContextMenuMacOSScreenshots renders the unified object context menu,
// its Move under submenu, and the Copy as YAML toast on the real native macOS host
// (linking github.com/nsgo-dev/nsgo through editorcmd) for visual verification
// beside docs/design/editor/Inspector.dc.html.
func TestObjectContextMenuMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: dungeon-run
id: com.example.dungeonrun
appVersion: 0.1.0
window:
  title: Dungeon Run
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sceneYAML := []byte(`version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: actors
    space: world
    objects:
      - id: knight
        name: Knight
        x: 160
        y: 200
        sprite:
          atlas: actors
          frame: hero
      - id: slime_01
        name: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 260
        y: 200
        sprite:
          atlas: actors
          frame: slime
      - id: slime_02
        name: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 340
        y: 200
        sprite:
          atlas: actors
          frame: slime
      - id: coins
        name: coins
        x: 420
        y: 200
        children:
          - id: coin_01
            name: coin_01
            x: 0
            y: 0
          - id: coin_02
            name: coin_02
            x: 16
            y: 0
`)
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), sceneYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	sheet := image.NewRGBA(image.Rect(0, 0, 48, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			sheet.SetRGBA(x, y, color.RGBA{R: 0x3b, G: 0x82, B: 0xf6, A: 0xff})
		}
		for x := 24; x < 48; x++ {
			sheet.SetRGBA(x, y, color.RGBA{R: 0x22, G: 0xc5, B: 0x5e, A: 0xff})
		}
	}
	actors := &sprite.Atlas{
		Image: core.ImageSource{Pixels: sheet},
		Frames: map[string]sprite.Frame{
			"hero":  {Name: "hero", X: 0, Y: 0, W: 24, H: 24, Width: 24, Height: 24},
			"slime": {Name: "slime", X: 24, Y: 0, W: 24, H: 24, Width: 24, Height: 24},
		},
	}
	ed, err := editor.New(nil, sceneformat.AtlasMap{"actors": actors})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if err := ed.SetAppearance(editor.AppearanceDark); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	app.Post(func() {
		ed.SelectObject("slime_01")
		ed.FrameSelection2D()
	})
	app.Settle()

	// 1. Open object context menu on slime_01 via secondary click in the 2D viewport.
	app.RightClick("id:editor-viewport")
	if !ed.ObjectContextMenuOpen() || ed.ObjectContextMenuTarget() != "slime_01" {
		t.Fatalf("context menu did not open for slime_01 (open=%v target=%q)", ed.ObjectContextMenuOpen(), ed.ObjectContextMenuTarget())
	}
	app.Screenshot("object-context-menu")

	// 2. Open Move under submenu.
	app.Press("id:context-menu-move-under")
	app.Screenshot("object-context-menu-move-under")

	// 3. Copy as YAML and capture the floating Spec toast.
	app.Press("id:context-menu-copy-yaml")
	if !app.Exists("id:editor-toast") {
		t.Fatal("Copy as YAML toast not shown")
	}
	app.Screenshot("object-context-menu-copy-toast")
}
