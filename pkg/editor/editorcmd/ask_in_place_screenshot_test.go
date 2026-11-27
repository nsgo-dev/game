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

// TestAskInPlaceMacOSScreenshots renders the anchored ⌘L in-place composer with two selected
// slimes (slime_01, slime_02), contextual suggestions, and the Agent panel thread on macOS
// for visual verification beside docs/design/editor/AskInPlace.dc.html and Spec.dc.html.
func TestAskInPlaceMacOSScreenshots(t *testing.T) {
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
backgroundColor: "#181C24"
layers:
  - name: actors
    space: world
    objects:
      - id: knight
        name: Knight
        x: 150
        y: 200
        sprite:
          atlas: actors
          frame: hero
      - id: slime_01
        name: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 260
        y: 205
        sprite:
          atlas: actors
          frame: slime
      - id: slime_02
        name: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 340
        y: 205
        sprite:
          atlas: actors
          frame: slime
`)
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), sceneYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	sheet := image.NewRGBA(image.Rect(0, 0, 48, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			sheet.SetRGBA(x, y, color.RGBA{R: 0xe9, G: 0xa8, B: 0x4a, A: 0xff})
		}
		for x := 24; x < 48; x++ {
			sheet.SetRGBA(x, y, color.RGBA{R: 0x4f, G: 0xd3, B: 0x8a, A: 0xff})
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
	if err := ed.SetLanguage(editor.LanguageEnglish); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAppearance(editor.AppearanceDark); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Post(func() {
		ed.SelectObjects([]string{"slime_01", "slime_02"})
		ed.FrameSelection2D()
	})
	app.Settle()

	// 1. Dark Spec appearance: two slimes selected + anchored ⌘L composer with typed prompt and suggestions.
	app.Key("mod+l")
	if !ed.AskInPlaceOpen() {
		t.Fatal("mod+l did not open in-place composer")
	}
	app.Type("id:ask-in-place-input", "make these two hop instead of walk, and a bit faster")
	app.Screenshot("ask-in-place-dark")

	// 2. Send request -> Agent panel shows the turn with selection, scene, viewport image, and skip-plan badge.
	app.Key("enter")
	if !app.Exists("id:agent-panel") {
		t.Fatal("Agent panel not shown after sending in-place request")
	}
	app.Screenshot("ask-in-place-agent-panel")

	// 3. Light Spec appearance verification.
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
		_ = ed.OpenAskInPlace()
		ed.SetAskInPlaceDraft("make these two hop instead of walk, and a bit faster")
	})
	app.Settle()
	app.Screenshot("ask-in-place-light")
}
