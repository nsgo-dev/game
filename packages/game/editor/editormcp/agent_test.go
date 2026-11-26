package editormcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1037 Acceptance: selecting two objects (slime_01, slime_02), pressing ⌘L, typing a request, and sending it
// delivers the selection's object IDs, open scene (path + YAML), skipPlan=true flag, and rendered viewport PNG
// through MCP (both editor://agent/turn notifications/reads and agent_turn_get), and the turn shows in the Agent panel.
func TestAskInPlaceDeliversSelectionSceneAndViewportImageOverMCP(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), []byte(`version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: actors
    space: world
    objects:
      - id: knight
        name: Knight
        x: 120
        y: 220
      - id: slime_01
        name: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 240
        y: 220
      - id: slime_02
        name: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 320
        y: 220
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	var s *editormcp.Server
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = editormcp.New(ed, editormcp.Options{Run: cx.Post})
		cx.Cleanup(func() { _ = s.Close() })
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	runMCP := func(fn func()) {
		t.Helper()
		done := make(chan struct{})
		go func() {
			defer close(done)
			fn()
		}()
		for {
			app.Settle()
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}

	notices := make(chan *mcp.ResourceUpdatedNotificationParams, 8)
	cs := connect(t, s, &mcp.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
			notices <- req.Params
		},
	})
	if err := cs.Subscribe(context.Background(), &mcp.SubscribeParams{URI: editormcp.AgentTurnURI}); err != nil {
		t.Fatalf("Subscribe(%s): %v", editormcp.AgentTurnURI, err)
	}

	// Select slime_01 and slime_02, press ⌘L, type request, and send.
	app.Post(func() {
		ed.SelectObjects([]string{"slime_01", "slime_02"})
	})
	app.Settle()

	app.Key("meta+l")
	if !ed.AskInPlaceOpen() {
		t.Fatal("meta+l did not open the in-place composer")
	}
	app.Type("id:ask-in-place-input", "make these two hop instead of walk, and a bit faster")
	app.Key("enter")

	// 1. Agent receives MCP resource notification on editor://agent/turn.
	select {
	case n := <-notices:
		if n.URI != editormcp.AgentTurnURI {
			t.Fatalf("notice URI = %q, want %q", n.URI, editormcp.AgentTurnURI)
		}
		if n.Meta[editormcp.MetaBy] != "person" || n.Meta[editormcp.MetaSkipPlan] != true {
			t.Fatalf("notice meta = %+v, want by=person skipPlan=true", n.Meta)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for editor://agent/turn notification")
	}

	// 2. Reading editor://agent/turn yields JSON payload + viewport PNG blob.
	var (
		readRes *mcp.ReadResourceResult
		readErr error
	)
	runMCP(func() {
		readRes, readErr = cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.AgentTurnURI})
	})
	if readErr != nil {
		t.Fatalf("ReadResource(%s): %v", editormcp.AgentTurnURI, readErr)
	}
	if len(readRes.Contents) != 2 {
		t.Fatalf("ReadResource contents = %d, want 2 (JSON + PNG)", len(readRes.Contents))
	}
	var resPayload editormcp.AgentTurnPayload
	if err := json.Unmarshal([]byte(readRes.Contents[0].Text), &resPayload); err != nil {
		t.Fatalf("unmarshal resource JSON: %v", err)
	}
	if !slices.Equal(resPayload.SelectionIDs, []string{"slime_01", "slime_02"}) {
		t.Fatalf("resource SelectionIDs = %v, want [slime_01 slime_02]", resPayload.SelectionIDs)
	}
	if resPayload.ScenePath != "scenes/level_01.scene.yaml" || !strings.Contains(resPayload.SceneYAML, "id: slime_01") || !resPayload.SkipPlan {
		t.Fatalf("resource payload = %+v", resPayload)
	}
	if readRes.Contents[1].MIMEType != "image/png" {
		t.Fatalf("resource blob mime = %q, want image/png", readRes.Contents[1].MIMEType)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(readRes.Contents[1].Blob)); err != nil {
		t.Fatalf("resource PNG invalid: %v", err)
	}

	// 3. Calling agent_turn_get returns structured context + PNG ImageContent.
	var toolRes *mcp.CallToolResult
	runMCP(func() {
		toolRes = call(t, cs, editormcp.ToolAgentTurnGet, map[string]any{})
	})
	outText := mustOK(t, toolRes)
	if !strings.Contains(outText, "slime_01, slime_02") || !strings.Contains(outText, "scenes/level_01.scene.yaml") {
		t.Fatalf("agent_turn_get text:\n%s", outText)
	}
	var got editormcp.AgentTurnPayload
	structured(t, toolRes, &got)
	if got.Prompt != "make these two hop instead of walk, and a bit faster" || !got.SkipPlan || !got.HasViewportImage {
		t.Fatalf("agent_turn_get structured = %+v", got)
	}
	if !slices.Equal(got.SelectionIDs, []string{"slime_01", "slime_02"}) || got.ScenePath != "scenes/level_01.scene.yaml" {
		t.Fatalf("agent_turn_get selection/scene = %+v", got)
	}
	var imgData []byte
	for _, c := range toolRes.Content {
		if ic, ok := c.(*mcp.ImageContent); ok && ic.MIMEType == "image/png" {
			imgData = ic.Data
		}
	}
	if len(imgData) == 0 {
		t.Fatal("agent_turn_get did not attach viewport PNG ImageContent")
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(imgData)); err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		t.Fatalf("agent_turn_get PNG invalid: err=%v cfg=%+v", err, cfg)
	}

	// 4. Turn shows in the Agent panel and updates live when the agent calls agent_turn_reply.
	if !app.Exists("id:agent-panel") || !app.Exists("id:agent-turn-0") {
		t.Fatalf("turn %s not shown in Agent panel", got.ID)
	}
	var replyRes *mcp.CallToolResult
	runMCP(func() {
		replyRes = call(t, cs, editormcp.ToolAgentTurnReply, map[string]any{
			"turnId": got.ID,
			"status": "done",
			"reply":  "Switched slime_01 and slime_02 to hop patrol at speed 42.",
		})
	})
	mustOK(t, replyRes)
	app.Settle()
	if !app.Exists("id:agent-turn-reply-" + got.ID) {
		t.Fatalf("agent reply not rendered in Agent panel for %s", got.ID)
	}

	// 5. agent_turn_reply rejects NUL bytes and replies exceeding 256 KiB.
	var nulRes *mcp.CallToolResult
	runMCP(func() {
		nulRes = call(t, cs, editormcp.ToolAgentTurnReply, map[string]any{
			"turnId": got.ID,
			"reply":  "bad\x00reply",
		})
	})
	if !nulRes.IsError {
		t.Fatal("agent_turn_reply unexpectedly accepted NUL byte")
	}
	var hugeRes *mcp.CallToolResult
	runMCP(func() {
		hugeRes = call(t, cs, editormcp.ToolAgentTurnReply, map[string]any{
			"turnId": got.ID,
			"reply":  strings.Repeat("x", (256<<10)+1),
		})
	})
	if !hugeRes.IsError {
		t.Fatal("agent_turn_reply unexpectedly accepted oversized reply")
	}
}
