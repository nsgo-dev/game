package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestHistoryCheckpointsMacOSScreenshots renders the Project History & Checkpoints panel
// (previewing a checkpoint, restored state, and light theme) on the real native macOS host
// (`env -u GOROOT TMPDIR=/tmp go tool nsgo test -target macos -run TestHistoryCheckpointsMacOSScreenshots ./editor/editorcmd`)
// for visual verification beside design/editor/Checkpoints.dc.html and Spec.dc.html.
func TestHistoryCheckpointsMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"scenes", "prefabs", "scripts", "playtests"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scenePath := filepath.Join(dir, "scenes", "level_01.scene.yaml")
	if err := os.WriteFile(scenePath, []byte("version: 2\nname: Level 01\nvirtualWidth: 640\nvirtualHeight: 360\nbackgroundColor: \"#141824\"\nlayers:\n  - name: actors\n    space: world\n    order: 0\n    objects:\n      - id: knight\n        name: Knight\n        x: 110\n        y: 190\n      - id: slime_01\n        name: Slime 01\n        x: 240\n        y: 210\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prefabs", "coin.prefab.yaml"), []byte("version: 1\nname: Coin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "patrol.go"), []byte("package scripts\n\nconst Speed = 24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pf := &project.File{
		Version:    project.CurrentVersion,
		Name:       "Dungeon Run",
		ID:         "dev.nsgo.dungeonrun",
		AppVersion: "0.4.2",
		Window:     project.Window{Width: 640, Height: 360},
		Autoload: &project.Autoload{
			Scenes:    []project.Entry{{Name: "level_01", Path: "scenes/level_01.scene.yaml"}},
			MainScene: "level_01",
		},
	}
	data, err := project.Encode(pf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	ed.SetLanguage(editor.LanguageEnglish)
	ed.SetAppearance(editor.AppearanceDark)

	// Populate realistic multi-day history entries matching Checkpoints.dc.html.
	clock := time.Date(2026, 11, 25, 2, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return clock }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.TakeCheckpointBeforeTask("Nightly 20-run playtest", "playtests/2026-11-24.report.yaml"); err != nil {
		t.Fatal(err)
	}

	clock = time.Date(2026, 11, 26, 14, 5, 0, 0, time.UTC)
	if _, err := ed.RunCheckpointTurn("Slower patrol, documented", func() error {
		if err := ed.WriteAgentFile("scripts/patrol.go", []byte("package scripts\n\nconst Speed = 18\n")); err != nil {
			return err
		}
		return ed.WriteAgentFile("AGENTS.md", []byte("# Rules\n"))
	}); err != nil {
		t.Fatal(err)
	}

	clock = time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	if err := ed.MoveObject("knight", 148, 190); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveFile(scenePath); err != nil {
		t.Fatal(err)
	}

	clock = time.Date(2026, 11, 26, 14, 31, 0, 0, time.UTC)
	cpSlimes, err := ed.RunCheckpointTurn("Slimes and coins · 6 changes", func() error {
		if err := ed.MoveObject("slime_01", 280, 210); err != nil {
			return err
		}
		return ed.SaveFile(scenePath)
	})
	if err != nil {
		t.Fatal(err)
	}

	clock = time.Date(2026, 11, 26, 14, 41, 0, 0, time.UTC)
	if _, err := ed.RunCheckpointTurn("Coin spin, bob and sparkle", func() error {
		if err := ed.WriteAgentFile("prefabs/coin.prefab.yaml", []byte("version: 1\nname: Coin\nspin: true\n")); err != nil {
			return err
		}
		return ed.WriteAgentFile("fx/sparkle.particles.yaml", []byte("version: 1\nname: sparkle\n"))
	}); err != nil {
		t.Fatal(err)
	}

	clock = time.Date(2026, 11, 26, 14, 43, 0, 0, time.UTC)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Post(func() {
		ed.SetHistoryPanelOpen(true)
		_ = ed.PreviewCheckpoint(cpSlimes.ID)
	})
	app.Settle()
	app.Screenshot("history-checkpoints-dark-preview")

	app.Post(func() {
		_ = ed.RestorePreviewedHistory()
	})
	app.Settle()
	app.Screenshot("history-checkpoints-dark-restored")

	app.Post(func() {
		ed.SetAppearance(editor.AppearanceLight)
		_ = ed.PreviewCheckpoint(cpSlimes.ID)
	})
	app.Settle()
	app.Screenshot("history-checkpoints-light-preview")
}
