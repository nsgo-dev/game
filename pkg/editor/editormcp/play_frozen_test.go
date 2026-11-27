package editormcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func callWithApp(t *testing.T, app *nsgotest.App, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ch := make(chan *mcp.CallToolResult, 1)
	go func() {
		ch <- call(t, cs, name, args)
	}()
	for {
		select {
		case res := <-ch:
			return res
		default:
			app.Settle()
			time.Sleep(time.Millisecond)
		}
	}
}

// #1042: play_frozen_context exposes the captured frame PNG, question, version 2 replay YAML,
// live entities, and marks; play_replay_step steps frames A..B while keeping Play paused and updating
// the viewport agent pill and Agent tab step log.
func TestPlayFrozenContextAndReplayStepMCP(t *testing.T) {
	f := openFixture(t)
	f.ed.PlayRate = 60
	f.ed.OnPlayUpdate = func(scene *sceneformat.BuiltScene, _ time.Duration) {
		if hero := scene.Object("hero"); hero != nil {
			hero.Doc.X += 1.5
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1120, 680))

	s := editormcp.New(f.ed, editormcp.Options{Run: app.Post})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Start Play, advance 30 frames, add a mark, pause and submit a frozen-frame question.
	app.Post(func() {
		if err := f.ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		f.ed.SetPlayPaused(true)
		f.ed.StepPlay(30)
		if _, err := f.ed.AddPlayMark(); err != nil {
			t.Errorf("AddPlayMark: %v", err)
		}
		if err := f.ed.OpenAskFrozenFrame(); err != nil {
			t.Errorf("OpenAskFrozenFrame: %v", err)
		}
		if _, err := f.ed.SubmitAskFrozenFrame("Hero snags on the ledge"); err != nil {
			t.Errorf("SubmitAskFrozenFrame: %v", err)
		}
	})
	app.Settle()

	// 1) Agent reads frozen frame context via play_frozen_context.
	res := callWithApp(t, app, cs, editormcp.ToolPlayFrozenContext, map[string]any{})
	_ = mustOK(t, res)
	var ctxRes editormcp.PlayFrozenContextResult
	structured(t, res, &ctxRes)

	if !ctxRes.Playing || !ctxRes.Paused {
		t.Fatalf("playing=%v paused=%v, want true, true", ctxRes.Playing, ctxRes.Paused)
	}
	if ctxRes.Frame != 30 {
		t.Fatalf("Frame = %d, want 30", ctxRes.Frame)
	}
	if ctxRes.Prompt != "Hero snags on the ledge" {
		t.Fatalf("Prompt = %q", ctxRes.Prompt)
	}
	if !strings.HasPrefix(ctxRes.ReplayPath, "replays/") {
		t.Fatalf("ReplayPath = %q, want replays/...", ctxRes.ReplayPath)
	}
	if len(ctxRes.Marks) != 1 {
		t.Fatalf("Marks = %d, want 1", len(ctxRes.Marks))
	}
	if len(ctxRes.LiveEntities) == 0 {
		t.Fatal("expected non-empty LiveEntities")
	}
	rec, err := replay.Decode([]byte(ctxRes.ReplayYAML))
	if err != nil {
		t.Fatalf("decode returned ReplayYAML: %v", err)
	}
	if rec.Version != replay.FormatVersion || rec.TotalSteps != 30 {
		t.Fatalf("decoded replay = %+v", rec)
	}
	hasPNG := false
	for _, c := range res.Content {
		if img, ok := c.(*mcp.ImageContent); ok && img.MIMEType == "image/png" && len(img.Data) > 8 {
			hasPNG = true
		}
	}
	if !hasPNG {
		t.Fatal("play_frozen_context did not attach frame PNG ImageContent")
	}

	// 2) Agent steps frames 30..42 via play_replay_step.
	stepRes := callWithApp(t, app, cs, editormcp.ToolPlayReplayStep, map[string]any{
		"from_frame": 30,
		"to_frame":   42,
		"note":       "hero reaches edge",
	})
	_ = mustOK(t, stepRes)
	var sr editormcp.PlayReplayStepResult
	structured(t, stepRes, &sr)

	if sr.CurrentFrame != 42 || !sr.Paused {
		t.Fatalf("step result = %+v, want CurrentFrame=42 Paused=true", sr)
	}
	if sr.Pill != "agent stepping frames 30–42" {
		t.Fatalf("Pill = %q, want %q", sr.Pill, "agent stepping frames 30–42")
	}
	var stillPaused bool
	app.Post(func() {
		stillPaused = f.ed.PlayPaused()
	})
	app.Settle()
	if !stillPaused {
		t.Fatal("editor must stay paused after play_replay_step")
	}
	foundLog := false
	for _, st := range sr.AgentSteps {
		if strings.Contains(st.Text, "Stepped frames 30–42 with game_loop_step; hero reaches edge") {
			foundLog = true
		}
	}
	if !foundLog {
		t.Fatalf("expected step log entry, got %+v", sr.AgentSteps)
	}

	// 3) Security & bounds: huge to_frame, non-.replay.yaml path, and symlink escape must be refused.
	hugeRes := callWithApp(t, app, cs, editormcp.ToolPlayReplayStep, map[string]any{
		"from_frame": 0,
		"to_frame":   100_000_000,
	})
	if !hugeRes.IsError {
		t.Fatal("expected huge to_frame to return a tool error")
	}

	badExtRes := callWithApp(t, app, cs, editormcp.ToolPlayReplayStep, map[string]any{
		"replay_file": "game.yaml",
		"steps":       1,
	})
	if !badExtRes.IsError {
		t.Fatal("expected non-.replay.yaml replay_file to return a tool error")
	}

	outsideDir := t.TempDir()
	outsideReplay := filepath.Join(outsideDir, "outside.replay.yaml")
	if err := os.WriteFile(outsideReplay, []byte("version: 2\nrate: 60\ntotalSteps: 1\n"), 0o644); err != nil {
		t.Fatalf("write outside replay: %v", err)
	}
	replaysDir := filepath.Join(f.dir, "replays")
	if err := os.MkdirAll(replaysDir, 0o755); err != nil {
		t.Fatalf("mkdir replays: %v", err)
	}
	if err := os.Symlink(outsideReplay, filepath.Join(replaysDir, "escape.replay.yaml")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	symRes := callWithApp(t, app, cs, editormcp.ToolPlayReplayStep, map[string]any{
		"replay_file": "replays/escape.replay.yaml",
		"steps":       1,
	})
	if !symRes.IsError {
		t.Fatal("expected symlink replay_file escaping project root to return a tool error")
	}
}
