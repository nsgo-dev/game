package editormcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

func listClientToolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// #1028 Acceptance 1: Turning a tool off removes it from the server's tool list for the connected
// client, and calling it returns a refusal the agent can read. game_delete_file is off by default.
func TestDisabledToolRemovedFromClientListAndCallRefused(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	changed := make(chan struct{}, 8)
	cs := connect(t, s, &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			select {
			case changed <- struct{}{}:
			default:
			}
		},
	})

	// 1. game_delete_file is registered on Server (visible in Settings › Tools) but OFF by default
	// and NOT offered to connected MCP clients.
	if f.ed.IsMCPToolEnabled(editormcp.ToolGameDeleteFile) {
		t.Fatal("game_delete_file should be off by default")
	}
	en, total := f.ed.EnabledMCPToolCount()
	if total < 45 || en != total-1 {
		t.Fatalf("EnabledMCPToolCount = (%d, %d), want %d/%d with game_delete_file off", en, total, total-1, total)
	}
	if names := listClientToolNames(t, cs); slices.Contains(names, editormcp.ToolGameDeleteFile) {
		t.Fatalf("game_delete_file should not be in client tool list by default: %v", names)
	}
	// Calling game_delete_file while off returns a readable refusal.
	delRes := call(t, cs, editormcp.ToolGameDeleteFile, map[string]any{"file": "data/items.yaml"})
	if !delRes.IsError {
		t.Fatal("calling disabled game_delete_file succeeded; want refusal")
	}
	if msg := text(delRes); !strings.Contains(msg, editormcp.ToolGameDeleteFile) || !strings.Contains(msg, "turned off") {
		t.Fatalf("disabled game_delete_file refusal = %q, want tool name and reason", msg)
	}

	// 2. Turning scene_edit off removes it from the client's tool list and sends tools/list_changed.
	if err := f.ed.SetMCPToolEnabled(editormcp.ToolSceneEdit, false); err != nil {
		t.Fatalf("SetMCPToolEnabled(scene_edit, false): %v", err)
	}
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive tools/list_changed notification after disabling scene_edit")
	}
	if names := listClientToolNames(t, cs); slices.Contains(names, editormcp.ToolSceneEdit) {
		t.Fatalf("scene_edit remained in client tool list after being turned off")
	}
	// Calling scene_edit while turned off returns a refusal the agent can read and makes no edits.
	editRes := call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "move", "id": "hero", "x": 999, "y": 999}},
	})
	if !editRes.IsError {
		t.Fatal("calling disabled scene_edit succeeded; want refusal")
	}
	if msg := text(editRes); !strings.Contains(msg, editormcp.ToolSceneEdit) || !strings.Contains(msg, "Settings › Tools") {
		t.Fatalf("disabled scene_edit refusal = %q, want readable reason", msg)
	}
	if f.ed.History.CanUndo() {
		t.Fatal("disabled scene_edit mutated editor history")
	}

	// 3. Re-enabling scene_edit restores it to the tool list and allows calls again.
	if err := f.ed.SetMCPToolEnabled(editormcp.ToolSceneEdit, true); err != nil {
		t.Fatalf("SetMCPToolEnabled(scene_edit, true): %v", err)
	}
	if names := listClientToolNames(t, cs); !slices.Contains(names, editormcp.ToolSceneEdit) {
		t.Fatalf("scene_edit did not reappear in client tool list after being turned back on")
	}
	okRes := call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "move", "id": "hero", "x": 44, "y": 22}},
	})
	mustOK(t, okRes)

	// 4. Enabling game_delete_file advertises it and allows proposing deletion of a regular scratch file
	// (still blocking game.yaml and You-only files, and leaving the file on disk until the person confirms).
	scratch := filepath.Join(f.dir, "data", "scratch.yaml")
	if err := os.WriteFile(scratch, []byte("note: temp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.SetMCPToolEnabled(editormcp.ToolGameDeleteFile, true); err != nil {
		t.Fatalf("SetMCPToolEnabled(game_delete_file, true): %v", err)
	}
	if names := listClientToolNames(t, cs); !slices.Contains(names, editormcp.ToolGameDeleteFile) {
		t.Fatalf("game_delete_file did not appear after enabling")
	}
	// game.yaml and permissions.yaml are always protected.
	if manifestRes := call(t, cs, editormcp.ToolGameDeleteFile, map[string]any{"file": "game.yaml"}); !manifestRes.IsError {
		t.Fatal("game_delete_file allowed deleting game.yaml")
	}
	if permRes := call(t, cs, editormcp.ToolGameDeleteFile, map[string]any{"file": "permissions.yaml"}); !permRes.IsError {
		t.Fatal("game_delete_file allowed deleting permissions.yaml")
	}
	mustOK(t, call(t, cs, editormcp.ToolGameDeleteFile, map[string]any{"file": "data/scratch.yaml"}))
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("data/scratch.yaml must stay on disk until confirmed: %v", err)
	}
	if err := f.ed.ConfirmProposedDeletion("data/scratch.yaml"); err != nil {
		t.Fatalf("ConfirmProposedDeletion: %v", err)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("data/scratch.yaml still exists after confirming deletion: %v", err)
	}
}

// #1028 Acceptance 2: With Go source switched off, any tool that would return Go code refuses;
// with viewport images switched off, viewport_image refuses for every connected agent.
// Also verifies Scene and data files and Art and audio files switches.
func TestPrivacySwitchesEnforcedForEveryAgent(t *testing.T) {
	f := openFixture(t)
	// Write a Go file, go.mod, and art/audio text metadata files into the fixture project.
	for rel, content := range map[string]string{
		"main.go":               "package main\n\nfunc main() {}\n",
		"go.mod":                "module example.com/demo\n\ngo 1.27\n",
		"art/hero.atlas.yaml":   "version: 1\nimage: hero.png\n",
		"audio/bgm.import.yaml": "version: 1\naudio:\n  loop: true\n",
	} {
		abs := filepath.Join(f.dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	// Connect two distinct MCP agents to verify privacy applies to EVERY agent.
	connectNamed := func(name string) *mcp.ClientSession {
		st, ct := mcp.NewInMemoryTransports()
		if _, err := s.Connect(context.Background(), st); err != nil {
			t.Fatalf("server connect %s: %v", name, err)
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: name}, nil).Connect(context.Background(), ct, nil)
		if err != nil {
			t.Fatalf("client connect %s: %v", name, err)
		}
		t.Cleanup(func() { _ = cs.Close() })
		return cs
	}
	agentA := connectNamed("claude-code")
	agentB := connectNamed("codex")
	agents := []*mcp.ClientSession{agentA, agentB}

	// 1. Default policy: Go source OFF, Art and audio OFF, Scene and data ON, Viewport images ON.
	for i, cs := range agents {
		for _, goFile := range []string{"main.go", "go.mod"} {
			res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": goFile})
			if !res.IsError {
				t.Fatalf("agent %d: scene_read(%s) succeeded with Go source off", i, goFile)
			}
			if msg := text(res); !strings.Contains(msg, "Go source code") || !strings.Contains(msg, "Settings › Usage and privacy") {
				t.Fatalf("agent %d: scene_read(%s) refusal = %q", i, goFile, msg)
			}
		}
		// refs_find targeting a .go file refuses when Go source is off.
		refRes := call(t, cs, editormcp.ToolRefsFind, map[string]any{"file": "main.go"})
		if !refRes.IsError || !strings.Contains(text(refRes), "Go source code") {
			t.Fatalf("agent %d: refs_find(main.go) with Go source off = (isErr=%v, text=%q)", i, refRes.IsError, text(refRes))
		}
		// Art and audio text files refuse when Art and audio is off.
		for _, artFile := range []string{"art/hero.atlas.yaml", "audio/bgm.import.yaml"} {
			res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": artFile})
			if !res.IsError {
				t.Fatalf("agent %d: scene_read(%s) succeeded with Art and audio off", i, artFile)
			}
			if msg := text(res); !strings.Contains(msg, "Art and audio files") {
				t.Fatalf("agent %d: scene_read(%s) refusal = %q", i, artFile, msg)
			}
		}
		// Scene files and viewport_image succeed under default policy.
		mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "scenes/room.scene.yaml"}))
		mustOK(t, call(t, cs, editormcp.ToolViewportImage, map[string]any{}))
	}

	// 2. Switching Viewport images OFF makes viewport_image refuse for EVERY agent.
	if err := f.ed.SetAgentPrivacy(editor.PrivacyViewportImages, false); err != nil {
		t.Fatal(err)
	}
	for i, cs := range agents {
		res := call(t, cs, editormcp.ToolViewportImage, map[string]any{})
		if !res.IsError {
			t.Fatalf("agent %d: viewport_image succeeded while Viewport images switched off", i)
		}
		if msg := text(res); !strings.Contains(msg, "Viewport images") {
			t.Fatalf("agent %d: viewport_image refusal = %q", i, msg)
		}
		// play_frozen_context requesting an image must also refuse.
		fres := call(t, cs, editormcp.ToolPlayFrozenContext, map[string]any{"includeImage": true})
		if !fres.IsError || !strings.Contains(text(fres), "Viewport images") {
			t.Fatalf("agent %d: play_frozen_context(includeImage=true) = (isErr=%v, %q)", i, fres.IsError, text(fres))
		}
	}

	// 3. Switching Go source ON allows reading main.go; switching Art and audio ON allows art/hero.atlas.yaml.
	if err := f.ed.SetAgentPrivacy(editor.PrivacyGoSource, true); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.SetAgentPrivacy(editor.PrivacyArtAndAudio, true); err != nil {
		t.Fatal(err)
	}
	for _, cs := range agents {
		out := mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "main.go"}))
		if !strings.Contains(out, "package main") {
			t.Fatalf("scene_read(main.go) after enabling Go source = %q", out)
		}
		artOut := mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "art/hero.atlas.yaml"}))
		if !strings.Contains(artOut, "image: hero.png") {
			t.Fatalf("scene_read(art/hero.atlas.yaml) after enabling Art and audio = %q", artOut)
		}
	}

	// 4. Switching Scene and data files OFF refuses scene_read, selection_get, and editor://scene resource.
	if err := f.ed.SetAgentPrivacy(editor.PrivacySceneAndData, false); err != nil {
		t.Fatal(err)
	}
	for i, cs := range agents {
		if res := call(t, cs, editormcp.ToolSceneRead, map[string]any{}); !res.IsError || !strings.Contains(text(res), "Scene and data files") {
			t.Fatalf("agent %d: scene_read open scene with SceneAndData off = (isErr=%v, %q)", i, res.IsError, text(res))
		}
		if res := call(t, cs, editormcp.ToolSelectionGet, map[string]any{}); !res.IsError || !strings.Contains(text(res), "Scene and data files") {
			t.Fatalf("agent %d: selection_get with SceneAndData off = (isErr=%v, %q)", i, res.IsError, text(res))
		}
		if _, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.SceneURI}); err == nil || !strings.Contains(err.Error(), "Scene and data files") {
			t.Fatalf("agent %d: ReadResource(editor://scene) with SceneAndData off = %v", i, err)
		}
	}
}

func TestOtherMCPServersWrittenToAgentConfig(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	// Add a custom MCP server through Editor (automatically syncs .mcp.json and .codex/config.toml).
	on := true
	if err := f.ed.AddAgentMCPServer(editor.AgentMCPServer{
		Name:        "sprite-gen",
		Title:       "Sprite generator",
		Description: "generates pixel sprites",
		Command:     "npx",
		Args:        []string{"-y", "@example/sprite-mcp"},
		Enabled:     &on,
	}); err != nil {
		t.Fatalf("AddAgentMCPServer: %v", err)
	}

	claudePath := filepath.Join(f.dir, editormcp.ClaudeConfigFile)
	codexPath := filepath.Join(f.dir, filepath.FromSlash(editormcp.CodexConfigFile))

	claudeBytes, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var parsed struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(claudeBytes, &parsed); err != nil {
		t.Fatalf("unmarshal .mcp.json: %v", err)
	}
	for _, wantName := range []string{editormcp.ServerName, editor.DefaultRunningGameMCPServerName, "sprite-gen"} {
		if _, ok := parsed.MCPServers[wantName]; !ok {
			t.Fatalf(".mcp.json missing server %q: %+v", wantName, parsed.MCPServers)
		}
	}
	if got := parsed.MCPServers[editor.DefaultRunningGameMCPServerName]; got.Command != "go" || strings.Join(got.Args, " ") != "tool nsgo mcp" {
		t.Fatalf("nsgo-game config in .mcp.json = %+v", got)
	}

	codexBytes, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatalf("read .codex/config.toml: %v", err)
	}
	codexStr := string(codexBytes)
	for _, wantSec := range []string{"[mcp_servers.nsgo-game-editor]", "[mcp_servers.nsgo-game]", "[mcp_servers.sprite-gen]"} {
		if !strings.Contains(codexStr, wantSec) {
			t.Fatalf(".codex/config.toml missing %s:\n%s", wantSec, codexStr)
		}
	}

	// Disabling sprite-gen removes it from both agent configs while preserving nsgo-game.
	if err := f.ed.SetAgentMCPServerEnabled("sprite-gen", false); err != nil {
		t.Fatalf("SetAgentMCPServerEnabled: %v", err)
	}
	claudeAfter, _ := os.ReadFile(claudePath)
	codexAfter, _ := os.ReadFile(codexPath)
	if strings.Contains(string(claudeAfter), `"sprite-gen"`) {
		t.Fatalf("disabled sprite-gen still in .mcp.json:\n%s", claudeAfter)
	}
	if strings.Contains(string(codexAfter), `[mcp_servers.sprite-gen]`) {
		t.Fatalf("disabled sprite-gen still in .codex/config.toml:\n%s", codexAfter)
	}
	if !strings.Contains(string(claudeAfter), `"`+editor.DefaultRunningGameMCPServerName+`"`) {
		t.Fatalf("nsgo-game unexpectedly removed from .mcp.json:\n%s", claudeAfter)
	}

	// Reject malformed server names that could inject TOML headers or JSON keys.
	if _, err := editormcp.InstallWithServers(f.dir, []editor.AgentMCPServer{
		{Name: "bad]\n[injected", Command: "echo"},
	}); err == nil {
		t.Fatal("InstallWithServers accepted invalid TOML-injecting server name")
	}
}

// Regression test for symlink privacy bypasses, nested art/audio folders, and newly-landed MCP tools & resources
// (agent_turn_get, editor://agent/turn, project_context, history_list, editor://brief, editor://rules, editor://context).
func TestPrivacyBlocksSymlinksNestedAssetsAndAllResources(t *testing.T) {
	f := openFixture(t)
	for rel, content := range map[string]string{
		"main.go":                "package main\nfunc secret() {}\n",
		"art/hero.atlas.yaml":    "version: 1\nimage: hero.png\n",
		"assets/art/palette.txt": "palette: warm\n",
		"assets/audio/sfx.txt":   "sfx: jump\n",
	} {
		abs := filepath.Join(f.dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Create in-project symlinks disguised with .scene.yaml extensions pointing to main.go and art/hero.atlas.yaml.
	goLink := filepath.Join(f.dir, "scenes", "sneaky_go.scene.yaml")
	if err := os.Symlink(filepath.Join("..", "main.go"), goLink); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	artLink := filepath.Join(f.dir, "scenes", "sneaky_art.scene.yaml")
	if err := os.Symlink(filepath.Join("..", "art", "hero.atlas.yaml"), artLink); err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. Disguised symlink to main.go must be refused with Go source code when GoSource=false.
	if res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "scenes/sneaky_go.scene.yaml"}); !res.IsError || !strings.Contains(text(res), "Go source code") {
		t.Fatalf("symlink to main.go bypassed Go source privacy: err=%v text=%q", res.IsError, text(res))
	}
	// 2. Disguised symlink to art/hero.atlas.yaml must be refused with Art and audio files when ArtAndAudio=false.
	if res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "scenes/sneaky_art.scene.yaml"}); !res.IsError || !strings.Contains(text(res), "Art and audio files") {
		t.Fatalf("symlink to art file bypassed Art and audio privacy: err=%v text=%q", res.IsError, text(res))
	}
	// 3. Nested art/audio directories (assets/art/, assets/audio/) must be classified as Art and audio.
	for _, nested := range []string{"assets/art/palette.txt", "assets/audio/sfx.txt"} {
		if res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": nested}); !res.IsError || !strings.Contains(text(res), "Art and audio files") {
			t.Fatalf("nested asset %s bypassed Art and audio privacy: err=%v text=%q", nested, res.IsError, text(res))
		}
	}

	// 4. Submit an in-place turn with viewport PNG and verify ViewportImages=false refuses both agent_turn_get and editor://agent/turn.
	f.ed.OpenAskInPlace()
	f.ed.SetAskInPlaceDraft("nudge hero right")
	if _, err := f.ed.SubmitAskInPlace(); err != nil {
		t.Fatalf("SubmitAskInPlace: %v", err)
	}
	if err := f.ed.SetAgentPrivacy(editor.PrivacyViewportImages, false); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.AgentTurnURI}); err == nil || !strings.Contains(err.Error(), "Viewport images") {
		t.Fatalf("ReadResource(editor://agent/turn) with ViewportImages=false = %v", err)
	}
	if res := call(t, cs, editormcp.ToolAgentTurnGet, map[string]any{}); !res.IsError || !strings.Contains(text(res), "Viewport images") {
		t.Fatalf("agent_turn_get(default includeImage) with ViewportImages=false = (err=%v, %q)", res.IsError, text(res))
	}

	// 5. SceneAndData=false refuses project_context, history_list, and all context/brief/rules/turn resources.
	if err := f.ed.SetAgentPrivacy(editor.PrivacySceneAndData, false); err != nil {
		t.Fatal(err)
	}
	for _, toolName := range []string{editormcp.ToolProjectContext, editormcp.ToolHistoryList, editormcp.ToolGamePlaytest} {
		if res := call(t, cs, toolName, map[string]any{}); !res.IsError || !strings.Contains(text(res), "Scene and data files") {
			t.Fatalf("%s with SceneAndData=false = (err=%v, %q)", toolName, res.IsError, text(res))
		}
	}
	for _, uri := range []string{editormcp.AgentTurnURI, editormcp.BriefURI, editormcp.RulesURI, editormcp.ContextURI} {
		if _, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri}); err == nil || !strings.Contains(err.Error(), "Scene and data files") {
			t.Fatalf("ReadResource(%s) with SceneAndData=false = %v", uri, err)
		}
	}
}
