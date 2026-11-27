package editorcmd_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestSettingsAutomationsMacOSScreenshots renders Settings › Automations (Dark and Light,
// plus Agent panel Ready to review) on the real native macOS host for visual inspection
// beside docs/design/editor/Settings.dc.html and Spec.dc.html.
func TestSettingsAutomationsMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	sceneDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(sceneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sceneDir, "level_01.scene.yaml"), []byte("version: 1\nname: Level 01\nlayers:\n  - name: world\n    space: world\n    order: 0\n"), 0o644); err != nil {
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
	ed.Now = func() time.Time { return time.Date(2026, 11, 26, 14, 30, 0, 0, time.UTC) }
	ed.SetLanguage(editor.LanguageEnglish)
	ed.SetAppearance(editor.AppearanceDark)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s1", StepTitle: "Ran game check · 0 errors", StepStatus: editor.AgentStepDone})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s2", StepTitle: "Ran 5 playtests on level_01", StepStatus: editor.AgentStepDone})
		emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "All 5 playtests cleared floor 1 in 41 s (target < 60 s). Ready for review."})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})
	if err := ed.SetSceneName("Level 01 · Dungeon"); err != nil {
		t.Fatal(err)
	}
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}
	if !ed.WaitAutomations(3 * time.Second) {
		t.Fatal("automation did not finish")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageAutomations) })
	app.Settle()
	app.Screenshot("settings-automations-dark")

	app.Post(func() {
		ed.SetAppearance(editor.AppearanceLight)
	})
	app.Settle()
	app.Screenshot("settings-automations-light")

	app.Post(func() {
		ed.SetAppearance(editor.AppearanceDark)
		ed.CloseSettings()
		ed.SetRightRailTab(editor.RightTabAgent)
	})
	app.Settle()
	app.Screenshot("agent-automation-ready-dark")
}
