package editor_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/internal/scenemerge"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMergeDriverBannerMacOSScreenshots renders the editor's Git scene merge driver setup banner
// on the real native macOS host (linking github.com/nsgo-dev/nsgo through editorcmd) and captures
// before and after screenshots of pressing the one-click "Enable merge driver" button (#1532).
func TestMergeDriverBannerMacOSScreenshots(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := filepath.Join(t.TempDir(), "dungeon-run")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runMacOSTestGit(t, repoDir, "init", "-b", "main")
	writeMacOSFixtureFile(t, filepath.Join(repoDir, "game.yaml"), "version: 1\nname: dungeon-run\nid: dev.example.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n")
	writeMacOSFixtureFile(t, filepath.Join(repoDir, "scenes", "level_01.scene.yaml"), "version: 3\nname: level_01\nvirtualWidth: 640\nvirtualHeight: 360\nlayers:\n  - name: actors\n    space: world\n    order: 0\n    objects:\n      - id: floor\n        x: 0\n        y: 300\n      - id: knight\n        name: Knight\n        x: 320\n        y: 220\n      - id: torch_01\n        name: Torch\n        x: 160\n        y: 240\n")
	writeMacOSFixtureFile(t, filepath.Join(repoDir, ".gitattributes"), scenemerge.GitAttributesContent)

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(repoDir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	if !app.Exists("id:merge-driver-banner") {
		t.Fatal("expected merge-driver-banner before setup")
	}
	app.Screenshot("merge-driver-banner-before")

	app.Press("id:merge-driver-setup")
	app.Settle()

	if app.Exists("id:merge-driver-banner") {
		t.Fatal("expected merge-driver-banner to disappear after setup")
	}
	app.Screenshot("merge-driver-banner-after")
}
