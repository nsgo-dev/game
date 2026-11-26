package editorcmd_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestAgentRewindMacOSScreenshots renders the Agent panel's Rewind popover ("Rewind to before this message")
// and post-rewind state (banner with Redo + rewound prompt returned to composer) on native macOS
// in both Dark and Light Spec themes for visual inspection beside docs/design/editor/Checkpoints.dc.html.
func TestAgentRewindMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	proj := &project.File{
		Version:    project.CurrentVersion,
		Name:       "dungeon-run",
		ID:         "dev.nsgo.dungeon-run",
		AppVersion: "0.1.0",
		Window:     project.Window{Width: 1280, Height: 720},
	}
	projData, err := project.Encode(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), projData, 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := sceneformat.Parse([]byte(macosThemePreviewScene))
	if err != nil {
		t.Fatal(err)
	}
	sceneBytes, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	sceneFile := filepath.Join(dir, "scenes", "level_01.scene.yaml")
	if err := os.MkdirAll(filepath.Dir(sceneFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sceneFile, sceneBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	coinFile := filepath.Join(dir, "prefabs", "coin.prefab.yaml")
	if err := os.MkdirAll(filepath.Dir(coinFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coinFile, []byte("version: 1\nname: coin\nspin: false\nbob: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sparkleFile := filepath.Join(dir, "fx", "sparkle.particles.yaml")

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ed.Close()
	if err := ed.OpenProjectScene(dir, sceneFile); err != nil {
		t.Fatal(err)
	}
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	prompt1 := "Put three slimes patrolling the lower floor and a short coin trail toward the door."
	prompt2 := "Make the coins spin and bob, and give each one a sparkle."

	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		switch req.Prompt {
		case prompt1:
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "s1"})
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Done: 3 slimes and 3 coins, kept."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		case prompt2:
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "s2"})
			_ = os.WriteFile(coinFile, []byte("version: 1\nname: coin\nspin: true\nbob: true\n"), 0o644)
			_ = os.MkdirAll(filepath.Dir(sparkleFile), 0o755)
			_ = os.WriteFile(sparkleFile, []byte("version: 1\nname: sparkle\nrate: 24\n"), 0o644)
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Added spin and bob to prefabs/coin.prefab.yaml", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "2", StepTitle: "Wrote fx/sparkle.particles.yaml", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Kept · 2 files. The sparkle is louder than you probably want."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		}
		return nil
	})

	// Run both turns before starting the native macOS window so all setup mutations happen before UI attachment.
	if err := ed.SubmitAgentPrompt(prompt1); err != nil {
		t.Fatal(err)
	}
	waitUntil := func(cond func() bool) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("timed out waiting for agent turn to finish")
	}
	waitUntil(func() bool { return !ed.AgentWorking() && len(ed.AgentTurns()) == 1 })
	if err := ed.SubmitAgentPrompt(prompt2); err != nil {
		t.Fatal(err)
	}
	waitUntil(func() bool { return !ed.AgentWorking() && len(ed.AgentTurns()) == 2 })

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()

	// 1. Dark: hover message 2 + open Rewind popover.
	app.Hover("id:agent-turn-prompt-1")
	app.Settle()
	app.Press("id:agent-rewind-trigger-1")
	app.Settle()
	app.Screenshot("agent-rewind-popover-dark")

	// 2. Dark: after Rewind (Files and conversation) -> banner with Redo + composer holds prompt 2.
	app.Press("id:agent-rewind-confirm")
	app.Settle()
	app.Screenshot("agent-rewind-after-dark")

	// 3. Light: Redo, switch appearance to Light via live environment, open Rewind popover & confirm.
	app.Press("id:agent-rewind-redo")
	app.Settle()
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	app.Hover("id:agent-turn-prompt-1")
	app.Settle()
	app.Press("id:agent-rewind-trigger-1")
	app.Settle()
	app.Screenshot("agent-rewind-popover-light")

	app.Press("id:agent-rewind-confirm")
	app.Settle()
	app.Screenshot("agent-rewind-after-light")
}
