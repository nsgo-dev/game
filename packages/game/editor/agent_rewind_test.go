package editor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func setupRewindProject(t *testing.T) (dir string, ed *editor.Editor, sceneFile, coinFile, sparkleFile string) {
	t.Helper()
	dir = t.TempDir()
	proj := &project.File{
		Version:    project.CurrentVersion,
		Name:       "dungeon-run",
		ID:         "dev.nsgo.dungeon-run",
		AppVersion: "0.1.0",
		Window:     project.Window{Width: 1280, Height: 720},
	}
	projData, err := project.Encode(proj)
	if err != nil {
		t.Fatalf("encode game.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), projData, 0o644); err != nil {
		t.Fatal(err)
	}

	sceneDoc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "level_01",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{{
			Name: "main",
			Objects: []sceneformat.ObjectDoc{
				{ID: "knight", Name: "Knight", X: 96, Y: 182},
			},
		}},
	}
	sceneBytes, err := sceneformat.Encode(sceneDoc)
	if err != nil {
		t.Fatalf("encode scene: %v", err)
	}
	sceneFile = filepath.Join(dir, "scenes", "level_01.scene.yaml")
	if err := os.MkdirAll(filepath.Dir(sceneFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sceneFile, sceneBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	coinFile = filepath.Join(dir, "prefabs", "coin.prefab.yaml")
	if err := os.MkdirAll(filepath.Dir(coinFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coinFile, []byte("version: 1\nname: coin\nspin: false\nbob: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sparkleFile = filepath.Join(dir, "fx", "sparkle.particles.yaml")

	ed, err = editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProjectScene(dir, sceneFile); err != nil {
		t.Fatalf("OpenProjectScene: %v", err)
	}
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})
	return dir, ed, sceneFile, coinFile, sparkleFile
}

func waitAgentIdleWithTurns(t *testing.T, app *nsgotest.App, ed *editor.Editor, wantTurns int) {
	t.Helper()
	app.WaitUntil(5*time.Second, func() bool {
		return !ed.AgentWorking() && ed.AgentStatus() == editor.AgentStatusConnected && len(ed.AgentTurns()) == wantTurns
	})
	app.Settle()
}

func TestAgentRewindFilesAndConversationAndRedo(t *testing.T) {
	_, ed, sceneFile, coinFile, sparkleFile := setupRewindProject(t)
	defer ed.Close()

	prompt1 := "Put three slimes patrolling the lower floor and a short coin trail toward the door."
	prompt2 := "Make the coins spin and bob, and give each one a sparkle."

	sceneAfterTurn1 := ""
	coinBeforeTurn2 := "version: 1\nname: coin\nspin: false\nbob: false\n"
	coinAfterTurn2 := "version: 1\nname: coin\nspin: true\nbob: true\n"
	sparkleAfterTurn2 := "version: 1\nname: sparkle\nrate: 24\n"

	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		switch req.Prompt {
		case prompt1:
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "sess-turn-1"})
			doc := &sceneformat.SceneDoc{
				Version:       sceneformat.CurrentVersion,
				Name:          "level_01",
				VirtualWidth:  640,
				VirtualHeight: 360,
				Layers: []sceneformat.LayerDoc{{
					Name: "main",
					Objects: []sceneformat.ObjectDoc{
						{ID: "knight", Name: "Knight", X: 96, Y: 182},
						{ID: "slime_01", Name: "Slime 1", X: 200, Y: 200},
						{ID: "coin_01", Name: "Coin 1", X: 260, Y: 180},
					},
				}},
			}
			b, err := sceneformat.Encode(doc)
			if err != nil {
				return err
			}
			sceneAfterTurn1 = string(b)
			if err := os.WriteFile(sceneFile, b, 0o644); err != nil {
				return err
			}
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Done: 3 slimes and 3 coins, kept."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil

		case prompt2:
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "sess-turn-2"})
			if err := os.WriteFile(coinFile, []byte(coinAfterTurn2), 0o644); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(sparkleFile), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(sparkleFile, []byte(sparkleAfterTurn2), 0o644); err != nil {
				return err
			}
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s1", StepTitle: "Added spin and bob to prefabs/coin.prefab.yaml", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "s2", StepTitle: "Wrote fx/sparkle.particles.yaml", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Kept · 2 files. The sparkle is louder than you probably want."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		default:
			return errors.New("unexpected prompt")
		}
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 860))

	// Send first request that edits the scene file.
	if err := ed.SubmitAgentPrompt(prompt1); err != nil {
		t.Fatalf("submit 1: %v", err)
	}
	waitAgentIdleWithTurns(t, app, ed, 1)

	// Send second request that edits coin.prefab.yaml and creates fx/sparkle.particles.yaml.
	if err := ed.SubmitAgentPrompt(prompt2); err != nil {
		t.Fatalf("submit 2: %v", err)
	}
	waitAgentIdleWithTurns(t, app, ed, 2)

	if ed.AgentSessionID() != "sess-turn-2" {
		t.Fatalf("session after turn 2 = %q, want sess-turn-2", ed.AgentSessionID())
	}
	// Before hovering, Rewind to here trigger should not be shown.
	if app.Exists("id:agent-rewind-trigger-1") {
		t.Fatal("rewind trigger should stay hidden until hovered or focused")
	}

	// Hover message 2 -> "Rewind to here" appears.
	app.Hover("id:agent-turn-prompt-1")
	app.Settle()
	if !app.Exists("id:agent-rewind-trigger-1") {
		t.Fatal("hovering sent message did not reveal Rewind to here")
	}
	if got := agentViewText(t, app, "id:agent-rewind-trigger-1"); got != "Rewind to here" {
		t.Fatalf("trigger text = %q, want Rewind to here", got)
	}

	// Open rewind popover and inspect its choices.
	app.Press("id:agent-rewind-trigger-1")
	app.Settle()
	if !app.Exists("id:agent-rewind-popover") {
		t.Fatal("rewind popover did not open")
	}
	if got := agentViewText(t, app, "id:agent-rewind-title"); got != "Rewind to before this message" {
		t.Fatalf("popover title = %q", got)
	}
	if got := agentViewText(t, app, "id:agent-rewind-opt-both-detail"); got != "Undo 2 files, drop the reply" {
		t.Fatalf("both detail = %q, want Undo 2 files, drop the reply", got)
	}

	// Confirm Rewind (default: Files and conversation).
	app.Press("id:agent-rewind-confirm")
	app.Settle()

	// Files are back to exact post-turn-1 state.
	gotScene, err := os.ReadFile(sceneFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotScene) != sceneAfterTurn1 {
		t.Fatalf("scene file after rewind changed unexpectedly:\ngot:\n%s\nwant:\n%s", gotScene, sceneAfterTurn1)
	}
	gotCoin, err := os.ReadFile(coinFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCoin) != coinBeforeTurn2 {
		t.Fatalf("coin.prefab.yaml after rewind = %q, want %q", gotCoin, coinBeforeTurn2)
	}
	if _, err := os.Stat(sparkleFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sparkle.particles.yaml should be removed by rewind, stat err = %v", err)
	}

	// Conversation dropped turn 2 and rolled session back to after turn 1.
	turns := ed.AgentTurns()
	if len(turns) != 1 || turns[0].Prompt != prompt1 {
		t.Fatalf("turns after rewind = %+v, want only first turn", turns)
	}
	if ed.AgentSessionID() != "sess-turn-1" {
		t.Fatalf("session after rewind = %q, want sess-turn-1", ed.AgentSessionID())
	}

	// Post-rewind banner with Redo is visible, and composer holds turn 2's prompt.
	if !app.Exists("id:agent-rewind-banner") {
		t.Fatal("post-rewind banner missing")
	}
	bannerText := agentViewText(t, app, "id:agent-rewind-banner-text")
	if !strings.Contains(bannerText, "Rewound files and conversation to before") {
		t.Fatalf("banner text = %q", bannerText)
	}
	if ed.AgentComposerText() != prompt2 {
		t.Fatalf("composer text after rewind = %q, want %q", ed.AgentComposerText(), prompt2)
	}
	if !ed.AgentComposerRewound() {
		t.Fatal("expected composer rewound ring to be active")
	}

	// Press Redo -> brings both files and conversation back.
	app.Press("id:agent-rewind-redo")
	app.Settle()

	gotCoin, err = os.ReadFile(coinFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCoin) != coinAfterTurn2 {
		t.Fatalf("coin.prefab.yaml after redo = %q, want %q", gotCoin, coinAfterTurn2)
	}
	gotSparkle, err := os.ReadFile(sparkleFile)
	if err != nil {
		t.Fatalf("sparkle.particles.yaml missing after redo: %v", err)
	}
	if string(gotSparkle) != sparkleAfterTurn2 {
		t.Fatalf("sparkle.particles.yaml after redo = %q, want %q", gotSparkle, sparkleAfterTurn2)
	}
	turns = ed.AgentTurns()
	if len(turns) != 2 || turns[1].Prompt != prompt2 {
		t.Fatalf("turns after redo = %+v, want both turns back", turns)
	}
	if ed.AgentSessionID() != "sess-turn-2" {
		t.Fatalf("session after redo = %q, want sess-turn-2", ed.AgentSessionID())
	}
	if app.Exists("id:agent-rewind-banner") {
		t.Fatal("rewind banner should clear after Redo")
	}
	if ed.AgentComposerText() != "" {
		t.Fatalf("composer text after redo = %q, want empty", ed.AgentComposerText())
	}
}

func TestAgentRewindFilesOnlyAndConversationOnlyModes(t *testing.T) {
	_, ed, _, coinFile, sparkleFile := setupRewindProject(t)
	defer ed.Close()

	coinOrig := "version: 1\nname: coin\nspin: false\nbob: false\n"
	coinV1 := "version: 1\nname: coin\nspin: true\nbob: false\n"
	coinV2 := "version: 1\nname: coin\nspin: true\nbob: true\n"
	sparkleV2 := "version: 1\nname: sparkle\nrate: 24\n"

	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		switch req.Prompt {
		case "First tweak":
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "s1"})
			if err := os.WriteFile(coinFile, []byte(coinV1), 0o644); err != nil {
				return err
			}
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Enabled spin."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		case "Second tweak":
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "s2"})
			if err := os.WriteFile(coinFile, []byte(coinV2), 0o644); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(sparkleFile), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(sparkleFile, []byte(sparkleV2), 0o644); err != nil {
				return err
			}
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Enabled bob and sparkle."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		}
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 860))

	if err := ed.SubmitAgentPrompt("First tweak"); err != nil {
		t.Fatal(err)
	}
	waitAgentIdleWithTurns(t, app, ed, 1)
	if err := ed.SubmitAgentPrompt("Second tweak"); err != nil {
		t.Fatal(err)
	}
	waitAgentIdleWithTurns(t, app, ed, 2)

	// 1. Files only: restores files to after turn 1, keeps BOTH turns in thread.
	app.Hover("id:agent-turn-prompt-1")
	app.Settle()
	app.Press("id:agent-rewind-trigger-1")
	app.Settle()
	app.Press("id:agent-rewind-opt-files")
	app.Settle()
	app.Press("id:agent-rewind-confirm")
	app.Settle()

	gotCoin, err := os.ReadFile(coinFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCoin) != coinV1 {
		t.Fatalf("Files only: coin = %q, want %q", gotCoin, coinV1)
	}
	if _, err := os.Stat(sparkleFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Files only: sparkle should be removed, err = %v", err)
	}
	if len(ed.AgentTurns()) != 2 {
		t.Fatalf("Files only: thread len = %d, want 2 (conversation kept)", len(ed.AgentTurns()))
	}
	if ed.AgentSessionID() != "s2" {
		t.Fatalf("Files only: session = %q, want s2", ed.AgentSessionID())
	}
	if !strings.Contains(agentViewText(t, app, "id:agent-rewind-banner-text"), "files only") {
		t.Fatalf("Files only banner = %q", agentViewText(t, app, "id:agent-rewind-banner-text"))
	}

	// Redo brings files back.
	app.Press("id:agent-rewind-redo")
	app.Settle()
	gotCoin, _ = os.ReadFile(coinFile)
	if string(gotCoin) != coinV2 {
		t.Fatalf("Redo after Files only: coin = %q, want %q", gotCoin, coinV2)
	}
	if _, err := os.Stat(sparkleFile); err != nil {
		t.Fatalf("Redo after Files only: sparkle missing: %v", err)
	}

	// 2. Conversation only: drops turn 2 from thread, keeps files at v2.
	app.Hover("id:agent-turn-prompt-1")
	app.Settle()
	app.Press("id:agent-rewind-trigger-1")
	app.Settle()
	app.Press("id:agent-rewind-opt-conv")
	app.Settle()
	app.Press("id:agent-rewind-confirm")
	app.Settle()

	if len(ed.AgentTurns()) != 1 || ed.AgentTurns()[0].Prompt != "First tweak" {
		t.Fatalf("Conversation only: turns = %+v, want only first turn", ed.AgentTurns())
	}
	if ed.AgentSessionID() != "s1" {
		t.Fatalf("Conversation only: session = %q, want s1", ed.AgentSessionID())
	}
	gotCoin, _ = os.ReadFile(coinFile)
	if string(gotCoin) != coinV2 {
		t.Fatalf("Conversation only: coin changed to %q, want untouched %q", gotCoin, coinV2)
	}
	if _, err := os.Stat(sparkleFile); err != nil {
		t.Fatalf("Conversation only: sparkle should remain, err = %v", err)
	}
	if !strings.Contains(agentViewText(t, app, "id:agent-rewind-banner-text"), "conversation only") {
		t.Fatalf("Conversation only banner = %q", agentViewText(t, app, "id:agent-rewind-banner-text"))
	}

	// Redo brings conversation back.
	app.Press("id:agent-rewind-redo")
	app.Settle()
	if len(ed.AgentTurns()) != 2 || ed.AgentSessionID() != "s2" {
		t.Fatalf("Redo after Conversation only: turns=%d session=%q, want 2 / s2", len(ed.AgentTurns()), ed.AgentSessionID())
	}

	// 3. Rewind to before the very first message (turn 0) with Files and conversation restores initial state.
	turn0ID := ed.AgentTurns()[0].ID
	if err := ed.RewindAgentTurn(turn0ID, editor.AgentRewindBoth); err != nil {
		t.Fatalf("RewindAgentTurn(turn0): %v", err)
	}
	gotCoin, _ = os.ReadFile(coinFile)
	if string(gotCoin) != coinOrig {
		t.Fatalf("rewind to before turn 0: coin = %q, want %q", gotCoin, coinOrig)
	}
	if len(ed.AgentTurns()) != 0 || ed.AgentSessionID() != "" {
		t.Fatalf("rewind to before turn 0: turns=%d session=%q, want 0 / empty", len(ed.AgentTurns()), ed.AgentSessionID())
	}
	if err := ed.RedoAgentRewind(); err != nil {
		t.Fatalf("RedoAgentRewind from turn 0: %v", err)
	}
	if len(ed.AgentTurns()) != 2 || ed.AgentSessionID() != "s2" {
		t.Fatalf("redo from turn 0: turns=%d session=%q, want 2 / s2", len(ed.AgentTurns()), ed.AgentSessionID())
	}
}

func TestAgentRewindContinuesSessionRebuildsOpenSceneAndGuards(t *testing.T) {
	_, ed, sceneFile, _, _ := setupRewindProject(t)
	defer ed.Close()

	var received []editor.AgentRequest
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		received = append(received, req)
		switch req.Prompt {
		case "Add slime":
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "sess-1"})
			doc := ed.Doc.Clone()
			doc.Layers[0].Objects = append(doc.Layers[0].Objects, sceneformat.ObjectDoc{ID: "slime_1", Name: "Slime", X: 120, Y: 180})
			b, err := sceneformat.Encode(doc)
			if err != nil {
				return err
			}
			if err := os.WriteFile(sceneFile, b, 0o644); err != nil {
				return err
			}
			ed.Post(func() {
				_ = ed.ReloadFromDisk(editor.DiskFileScene)
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone, Text: "Added slime_1."})
		case "Add boss":
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "sess-2"})
			doc := ed.Doc.Clone()
			doc.Layers[0].Objects = append(doc.Layers[0].Objects, sceneformat.ObjectDoc{ID: "boss_1", Name: "Boss", X: 300, Y: 180})
			b, err := sceneformat.Encode(doc)
			if err != nil {
				return err
			}
			if err := os.WriteFile(sceneFile, b, 0o644); err != nil {
				return err
			}
			ed.Post(func() {
				_ = ed.ReloadFromDisk(editor.DiskFileScene)
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone, Text: "Added boss_1."})
		case "Add gentle slime instead":
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "sess-3"})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone, Text: "Added gentle slime."})
		}
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	if err := ed.SubmitAgentPrompt("Add slime"); err != nil {
		t.Fatal(err)
	}
	waitAgentIdleWithTurns(t, app, ed, 1)
	if err := ed.SubmitAgentPrompt("Add boss"); err != nil {
		t.Fatal(err)
	}
	waitAgentIdleWithTurns(t, app, ed, 2)

	if obj, _, _ := ed.FindObject("boss_1"); obj == nil {
		t.Fatal("boss_1 not present in open scene before rewind")
	}

	// Cancel closes popover without rewinding.
	turn1ID := ed.AgentTurns()[1].ID
	if !ed.OpenAgentRewindPopover(turn1ID) {
		t.Fatal("OpenAgentRewindPopover failed")
	}
	app.Settle()
	app.Press("id:agent-rewind-cancel")
	app.Settle()
	if ed.AgentRewindOpenTurnID() != "" || len(ed.AgentTurns()) != 2 {
		t.Fatal("Cancel did not close popover cleanly")
	}

	// Play mode refuses rewind.
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if err := ed.RewindAgentTurn(turn1ID, editor.AgentRewindBoth); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("RewindAgentTurn during Play = %v, want ErrPlaying", err)
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Rewind to before "Add boss" -> boss_1 disappears from open scene in memory too, slime_1 remains.
	if err := ed.RewindAgentTurn(turn1ID, editor.AgentRewindBoth); err != nil {
		t.Fatalf("RewindAgentTurn: %v", err)
	}
	app.Settle()
	if obj, _, _ := ed.FindObject("boss_1"); obj != nil {
		t.Fatal("boss_1 still in open scene after rewind")
	}
	if obj, _, _ := ed.FindObject("slime_1"); obj == nil {
		t.Fatal("slime_1 lost from open scene after rewind")
	}

	// Edit composer and send a replacement prompt -> continues from session "sess-1" and 1 prior turn!
	ed.SetAgentComposerText("Add gentle slime instead")
	if ed.AgentComposerRewound() {
		t.Fatal("editing composer text should clear rewound highlight ring")
	}
	if err := ed.SubmitAgentPrompt(""); err != nil {
		t.Fatalf("submit replacement: %v", err)
	}
	waitAgentIdleWithTurns(t, app, ed, 2)

	if len(received) != 3 {
		t.Fatalf("received %d requests, want 3", len(received))
	}
	if received[2].SessionID != "sess-1" {
		t.Fatalf("continued SessionID = %q, want sess-1", received[2].SessionID)
	}
	if len(received[2].PriorTurns) != 1 || received[2].PriorTurns[0].Prompt != "Add slime" {
		t.Fatalf("continued PriorTurns = %+v, want [Add slime]", received[2].PriorTurns)
	}
	if ed.CanRedoAgentRewind() {
		t.Fatal("sending a new prompt should clear previous rewind Redo state")
	}
}

// TestAgentRewindScreenshots captures real macOS screenshots of the Rewind popover and post-rewind state
// in both Dark and Light Spec appearances for visual inspection beside docs/design/editor/Checkpoints.dc.html.
func TestAgentRewindScreenshots(t *testing.T) {
	_, ed, sceneFile, coinFile, sparkleFile := setupRewindProject(t)
	defer ed.Close()

	prompt1 := "Put three slimes patrolling the lower floor and a short coin trail toward the door."
	prompt2 := "Make the coins spin and bob, and give each one a sparkle."

	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		switch req.Prompt {
		case prompt1:
			emit(editor.AgentEvent{Kind: editor.AgentEventSession, SessionID: "s1"})
			b, _ := os.ReadFile(sceneFile)
			_ = os.WriteFile(sceneFile, append(b, []byte("\n")...), 0o644)
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

	ed.SetAppearance(editor.AppearanceDark)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	if err := ed.SubmitAgentPrompt(prompt1); err != nil {
		t.Fatal(err)
	}
	waitAgentIdleWithTurns(t, app, ed, 1)
	if err := ed.SubmitAgentPrompt(prompt2); err != nil {
		t.Fatal(err)
	}
	waitAgentIdleWithTurns(t, app, ed, 2)

	// 1. Dark theme: hover second message + open Rewind popover.
	app.Hover("id:agent-turn-prompt-1")
	app.Settle()
	app.Press("id:agent-rewind-trigger-1")
	app.Settle()

	dark := editor.DarkSpecPalette()
	if got := app.Find("id:agent-rewind-popover").Style.Background; got != dark.Raised {
		t.Fatalf("dark rewind popover bg = %#x, want %#x", uint32(got), uint32(dark.Raised))
	}
	if got := app.Find("id:agent-rewind-confirm").Style.Background; got != dark.Accent.Fg {
		t.Fatalf("dark rewind confirm bg = %#x, want %#x", uint32(got), uint32(dark.Accent.Fg))
	}
	app.Screenshot("agent/rewind-popover-dark")

	// 2. Dark theme: after pressing Rewind -> post-rewind banner with Redo + composer holding prompt2.
	app.Press("id:agent-rewind-confirm")
	app.Settle()
	if got := app.Find("id:agent-rewind-banner").Style.Background; got != dark.Raised {
		t.Fatalf("dark rewind banner bg = %#x, want %#x", uint32(got), uint32(dark.Raised))
	}
	if got := app.Find("id:agent-composer-card").Style.BorderColor; got != dark.Accent.Edge {
		t.Fatalf("dark composer rewound ring = %#x, want %#x", uint32(got), uint32(dark.Accent.Edge))
	}
	app.Screenshot("agent/rewind-after-dark")

	// 3. Light theme parity: Redo, switch to Light, verify popover + post-rewind banner tokens and screenshots.
	app.Press("id:agent-rewind-redo")
	app.Settle()
	ed.SetAppearance(editor.AppearanceLight)
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

	light := editor.LightSpecPalette()
	if got := app.Find("id:agent-rewind-popover").Style.Background; got != light.Raised {
		t.Fatalf("light rewind popover bg = %#x, want %#x", uint32(got), uint32(light.Raised))
	}
	app.Screenshot("agent/rewind-popover-light")

	app.Press("id:agent-rewind-confirm")
	app.Settle()
	if got := app.Find("id:agent-rewind-banner").Style.Background; got != light.Raised {
		t.Fatalf("light rewind banner bg = %#x, want %#x", uint32(got), uint32(light.Raised))
	}
	app.Screenshot("agent/rewind-after-light")
}
