package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const mainLayoutProjectYAML = `version: 1
name: Dungeon
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
`

const mainLayoutPrefabYAML = `version: 2
name: Slime
layers:
  - name: root
    objects:
      - id: body
        x: 0
        y: 0
`

const mainLayoutSceneYAML = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: knight
        name: Knight
        x: 180
        y: 220
      - id: slime_01
        name: Slime 01
        prefab: prefabs/slime.scene.yaml
        x: 340
        y: 240
      - id: door_trigger
        name: Door Trigger
        x: 520
        y: 220
        components:
          trigger: {}
`

// TestMainLayoutMacOSScreenshots renders the AI-first main layout (#1007) at 1440x900
// on the real macOS host in Dark and Light themes plus Inspector, missing-scene error,
// loading, and empty states for visual inspection against Main.dc.html and Spec.dc.html.
func TestMainLayoutMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("game.yaml", mainLayoutProjectYAML)
	mustWrite("prefabs/slime.scene.yaml", mainLayoutPrefabYAML)
	mustWrite("scenes/level_01.scene.yaml", mainLayoutSceneYAML)

	t.Run("agent-and-inspector", func(t *testing.T) {
		ed, err := editor.New(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ed.Close() })
		if err := ed.OpenProject(dir); err != nil {
			t.Fatal(err)
		}
		ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
			ID:        "sess-1",
			Name:      "Claude Code",
			Transport: "stdio",
		})
		ed.SetRightRailTab(editor.RightTabAgent)
		ed.SetProposedNewObjectIDs([]string{"slime_01"})
		ed.SetViewportPointer2D(312, 184)
		ed.SetOverlayLegendOpen(true)

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   true,
			Locale: "en-US",
		}))

		app.Screenshot("main-layout-agent-dark")

		app.SetEnvironment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   false,
			Locale: "en-US",
		})
		app.Screenshot("main-layout-agent-light")

		app.SetEnvironment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   true,
			Locale: "en-US",
		})
		app.Press("id:scene-tree-label-knight")
		app.Settle()
		app.Screenshot("main-layout-inspector-dark")
	})

	t.Run("error-loading-empty", func(t *testing.T) {
		ed, err := editor.New(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ed.Close() })
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   true,
			Locale: "en-US",
		}))

		app.Screenshot("main-layout-empty")

		app.Post(func() {
			ed.SetProjectLoading(true, filepath.Join(dir, "game.yaml"))
		})
		app.Settle()
		app.Screenshot("main-layout-loading")

		app.Post(func() {
			ed.SetProjectLoading(false, "")
			_ = ed.OpenFile(filepath.Join(dir, "scenes", "missing.scene.yaml"))
		})
		app.Settle()
		app.Screenshot("main-layout-error-missing-scene")
	})
}
