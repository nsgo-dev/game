package editor_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/internal/scenemerge"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeMergeDriverFixtureProject(t *testing.T, dir string, withGitAttributes bool) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	gameYAML := "version: 1\nname: dungeon-run\nid: dev.example.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"
	projectPath := filepath.Join(dir, project.FileName)
	if err := os.WriteFile(projectPath, []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), []byte(mergeFixtureBaseScene), 0o644); err != nil {
		t.Fatal(err)
	}
	if withGitAttributes {
		if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(scenemerge.GitAttributesContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return projectPath
}

// TestMergeDriverBannerAndOneClickSetup verifies issue #1532 acceptance:
// opening a Git project whose .gitattributes names nsgo-scene without local Git driver config
// shows a setup banner, one-click setup configures local Git and dismisses the banner,
// and captures before/after screenshots.
func TestMergeDriverBannerAndOneClickSetup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}

	repoDir := t.TempDir()
	runTestGit(t, repoDir, "init", "-b", "main")
	projectPath := writeMergeDriverFixtureProject(t, repoDir, true)

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(projectPath); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if !ed.SceneMergeDriverSetupNeeded() || !ed.SceneMergeDriverBannerVisible() {
		t.Fatalf("after OpenProject: needed=%v visible=%v, want both true", ed.SceneMergeDriverSetupNeeded(), ed.SceneMergeDriverBannerVisible())
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	if !app.Exists("id:merge-driver-banner") {
		t.Fatal("expected merge-driver-banner to render when driver is unconfigured")
	}
	if !app.Exists("id:merge-driver-setup") || !app.Exists("id:merge-driver-dismiss") {
		t.Fatal("expected merge-driver-setup and merge-driver-dismiss buttons")
	}

	// Live problem check completing must not remount the setup button under a press.
	setupID := app.Find("id:merge-driver-setup").ID
	ed.WaitForProblems()
	app.Settle()
	if got := app.Find("id:merge-driver-setup").ID; got != setupID {
		t.Fatalf("problem check remounted merge-driver-setup button: %v -> %v", setupID, got)
	}

	app.Screenshot("merge-driver-banner-before")

	// Click one-click setup -> configures local Git and hides banner.
	app.Press("id:merge-driver-setup")
	app.Settle()

	if app.Exists("id:merge-driver-banner") {
		t.Fatal("merge-driver-banner remained visible after pressing setup")
	}
	if ed.SceneMergeDriverSetupNeeded() || ed.SceneMergeDriverBannerVisible() {
		t.Fatalf("after setup: needed=%v visible=%v, want both false", ed.SceneMergeDriverSetupNeeded(), ed.SceneMergeDriverBannerVisible())
	}
	if got := runTestGit(t, repoDir, "config", "--local", "merge.nsgo-scene.driver"); got != scenemerge.DriverCommand {
		t.Errorf("git merge.nsgo-scene.driver = %q, want %q", got, scenemerge.DriverCommand)
	}
	if got := runTestGit(t, repoDir, "config", "--local", "merge.nsgo-scene.name"); got != scenemerge.DriverTitle {
		t.Errorf("git merge.nsgo-scene.name = %q, want %q", got, scenemerge.DriverTitle)
	}

	app.Screenshot("merge-driver-banner-after")

	// Opening the project again in a fresh editor does not show the banner.
	ed2, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed2.OpenProject(projectPath); err != nil {
		t.Fatalf("second OpenProject: %v", err)
	}
	if ed2.SceneMergeDriverSetupNeeded() || ed2.SceneMergeDriverBannerVisible() {
		t.Fatalf("fresh editor after setup: needed=%v visible=%v, want both false", ed2.SceneMergeDriverSetupNeeded(), ed2.SceneMergeDriverBannerVisible())
	}
}

func TestMergeDriverBannerNestedParentRepoAndDismiss(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}

	repoDir := t.TempDir()
	runTestGit(t, repoDir, "init", "-b", "main")
	nestedDir := filepath.Join(repoDir, "games", "dungeon")
	projectPath := writeMergeDriverFixtureProject(t, nestedDir, true)

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(projectPath); err != nil {
		t.Fatalf("OpenProject nested: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	if !app.Exists("id:merge-driver-banner") {
		t.Fatal("expected banner for nested project inside parent Git repo")
	}

	// Dismiss hides the banner without configuring Git.
	app.Press("id:merge-driver-dismiss")
	app.Settle()
	if app.Exists("id:merge-driver-banner") {
		t.Fatal("banner remained visible after Dismiss")
	}
	if !ed.SceneMergeDriverSetupNeeded() || ed.SceneMergeDriverBannerVisible() {
		t.Fatalf("after dismiss: needed=%v visible=%v, want true/false", ed.SceneMergeDriverSetupNeeded(), ed.SceneMergeDriverBannerVisible())
	}

	// Reopening resets dismissal and one-click setup configures the parent repository.
	app.Post(func() {
		if err := ed.OpenProject(projectPath); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if !app.Exists("id:merge-driver-banner") {
		t.Fatal("expected banner to reappear when reopening unconfigured project")
	}
	app.Press("id:merge-driver-setup")
	app.Settle()
	if app.Exists("id:merge-driver-banner") {
		t.Fatal("banner remained visible after nested setup")
	}
	if got := runTestGit(t, repoDir, "config", "--local", "merge.nsgo-scene.driver"); got != scenemerge.DriverCommand {
		t.Errorf("parent repo driver = %q, want %q", got, scenemerge.DriverCommand)
	}
}

func TestMergeDriverBannerNotShownWhenNotNeededAndShowsErrorOnFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}

	t.Run("outside_git_repo", func(t *testing.T) {
		dir := t.TempDir()
		projectPath := writeMergeDriverFixtureProject(t, dir, true)
		ed, err := editor.New(editor.DefaultSceneDoc(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := ed.OpenProject(projectPath); err != nil {
			t.Fatal(err)
		}
		if ed.SceneMergeDriverSetupNeeded() || ed.SceneMergeDriverBannerVisible() {
			t.Fatalf("outside git: needed=%v visible=%v, want false/false", ed.SceneMergeDriverSetupNeeded(), ed.SceneMergeDriverBannerVisible())
		}
	})

	t.Run("git_repo_without_gitattributes", func(t *testing.T) {
		dir := t.TempDir()
		runTestGit(t, dir, "init", "-b", "main")
		projectPath := writeMergeDriverFixtureProject(t, dir, false)
		ed, err := editor.New(editor.DefaultSceneDoc(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := ed.OpenProject(projectPath); err != nil {
			t.Fatal(err)
		}
		if ed.SceneMergeDriverSetupNeeded() || ed.SceneMergeDriverBannerVisible() {
			t.Fatalf("without .gitattributes: needed=%v visible=%v, want false/false", ed.SceneMergeDriverSetupNeeded(), ed.SceneMergeDriverBannerVisible())
		}
	})

	t.Run("setup_error_stays_visible", func(t *testing.T) {
		dir := t.TempDir()
		projectPath := writeMergeDriverFixtureProject(t, dir, true)
		ed, err := editor.New(editor.DefaultSceneDoc(), nil)
		if err != nil {
			t.Fatal(err)
		}
		ed.GitRunner = func(_ string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--show-toplevel" {
				return dir, nil
			}
			if len(args) >= 2 && args[0] == "config" && args[1] == "--get" {
				return "", errors.New("exit status 1")
			}
			if len(args) >= 1 && args[0] == "config" {
				return "permission denied", errors.New("exit status 128")
			}
			return "", errors.New("unsupported")
		}
		if err := ed.OpenProject(projectPath); err != nil {
			t.Fatal(err)
		}

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1280, 800))

		if !app.Exists("id:merge-driver-banner") {
			t.Fatal("expected banner before failed setup")
		}
		app.Press("id:merge-driver-setup")
		app.Settle()

		if !app.Exists("id:merge-driver-banner") || !app.Exists("id:merge-driver-error") {
			t.Fatal("expected banner and error text to stay visible after failed setup")
		}
		if got := app.Find("id:merge-driver-error").Text; !strings.Contains(got, "permission denied") {
			t.Errorf("error text = %q, want permission denied", got)
		}
	})
}
