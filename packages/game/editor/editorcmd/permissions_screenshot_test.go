package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestPermissionsSettingsMacOSScreenshots renders Settings › Permissions and the blocked-step / Always-toast
// flows on the real native macOS host beside Settings.dc.html and Permissions.dc.html.
func TestPermissionsSettingsMacOSScreenshots(t *testing.T) {
	projDir := filepath.Join(t.TempDir(), "dungeon-run")
	if err := os.MkdirAll(filepath.Join(projDir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMacOSFixtureFile(t, filepath.Join(projDir, "game.yaml"), "version: 1\nname: dungeon-run\nid: dev.example.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n")
	writeMacOSFixtureFile(t, filepath.Join(projDir, "scenes", "level_01.scene.yaml"), macosMergeBaseScene)

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetLanguage(editor.LanguageEnglish); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(projDir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Record a blocked shell command (rule 7 of 7) before starting the UI.
	shellEval := ed.EvaluatePermission(editor.PermissionRequest{
		Tool:   "shell_run",
		Action: editor.PermissionActionRun,
		Target: "shell: go generate ./assets",
	})
	ed.RecordBlockedPermission(shellEval, "run go generate ./assets")

	var scope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		scope = cx
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	postUI := func(fn func()) {
		scope.Post(fn)
		app.Settle()
	}

	// 1. Click blocked step link from the main workspace -> opens Settings › Permissions with rule 7 of 7 focused.
	app.Press("id:workspace-blocked-step-link")
	app.Screenshot("permissions-settings-blocked-rule7")

	// 2. Switch to Cautious, enqueue playtest Ask card, screenshot Ask card on Settings › Permissions.
	app.Press("id:perm-profile-cautious")
	postUI(func() {
		playEval := ed.EvaluatePermission(editor.PermissionRequest{
			Tool:   "game_playtest",
			Action: editor.PermissionActionRun,
			Target: editor.PermissionRunPlaytests,
		})
		ed.EnqueuePermissionRequest(playEval, "run playtests", nil)
	})
	app.Screenshot("permissions-settings-cautious-ask")

	// 3. Approve Always -> saves rule, marks profile Changed, shows "Always permission rule saved" toast with Show and Undo.
	app.Press("id:perm-rail-ask-always")
	app.Screenshot("permissions-settings-always-toast")
}
