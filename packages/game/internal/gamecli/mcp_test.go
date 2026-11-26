package gamecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editorcmd"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const mcpFixtureProject = `version: 1
name: Fixture
id: dev.example.fixture
appVersion: 1.0.0
window:
  width: 320
  height: 180
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
`

const mcpFixtureRoom = `version: 2
name: Room
virtualWidth: 320
virtualHeight: 180
backgroundColor: "#141824"
layers:
  - name: actors
    objects:
      - id: hero
        x: 64
        y: 40
        components:
          body2d:
            mass: 2
`

// mcpEndpoints points the editors' endpoints at a fresh directory short enough for a socket path on every platform, for the rest of the test.
func mcpEndpoints(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS != "windows" {
		var err error
		if dir, err = os.MkdirTemp("/tmp", "gmcp"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}
	t.Setenv(editormcp.EndpointDirEnv, dir)
}

// writeMCPFixture writes a project whose main scene holds a hero, as the editor saves it.
func writeMCPFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	doc, err := sceneformat.Parse([]byte(mcpFixtureRoom))
	if err != nil {
		t.Fatal(err)
	}
	room, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"game.yaml":              []byte(mcpFixtureProject),
		"nsgo.yaml":              []byte("name: Fixture\nid: dev.example.fixture\nversion: 1.0.0\n"),
		"scenes/room.scene.yaml": room,
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// startGameMCP runs go tool game mcp in dir as an agent starts it, on pipes, and connects an MCP client to it.
func startGameMCP(t *testing.T, dir string) *mcp.ClientSession {
	t.Helper()
	a, _, stderr := newTestApp(t)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	a.stdin, a.stdout = inR, outW
	done := make(chan int, 1)
	go func() {
		code := run([]string{"mcp", "-project", dir}, a)
		_ = outW.Close()
		done <- code
	}()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "agent"}, nil).Connect(context.Background(), &mcp.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatalf("connect: %v (stderr: %s)", err, stderr)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		_ = inW.Close()
		if code := <-done; code != 0 {
			t.Errorf("game mcp exit %d (stderr: %s)", code, stderr)
		}
	})
	return cs
}

// callIn calls a tool from another goroutine while app settles, as the editor's goroutine must run for the tool to reach the editor.
func callIn(t *testing.T, app *nsgotest.App, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	type answer struct {
		res *mcp.CallToolResult
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		ch <- answer{res, err}
	}()
	var got answer
	app.WaitUntil(30*time.Second, func() bool {
		select {
		case got = <-ch:
			return true
		default:
			return false
		}
	})
	app.Settle()
	if got.err != nil {
		t.Fatalf("%s: %v", name, got.err)
	}
	return got.res
}

// resultText joins a result's text.
func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// resultPNG is a result's image.
func resultPNG(t *testing.T, res *mcp.CallToolResult) []byte {
	t.Helper()
	for _, c := range res.Content {
		if ic, ok := c.(*mcp.ImageContent); ok && ic.MIMEType == "image/png" {
			return ic.Data
		}
	}
	t.Fatalf("no PNG in %s", resultText(res))
	return nil
}

// The issue's acceptance flow: the editor opens a fixture project, an agent connects through go tool game mcp started in a directory of the project, reads the scene, places an object with scene_edit and sees it in the tree, the viewport and as one undo step; the toolbar's Undo removes it.
func TestGameMCPEditsTheOpenEditor(t *testing.T) {
	mcpEndpoints(t)
	dir := writeMCPFixture(t)
	a, _, stderr := newTestApp(t)
	var ed *editor.Editor
	a.launch = func(e *editor.Editor) { ed = e }
	if code := run([]string{"editor", dir}, a); code != 0 {
		t.Fatalf("game editor exit %d (stderr: %s)", code, stderr)
	}
	t.Cleanup(ed.WaitForProblems)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		editorcmd.Serve(cx, ed, io.Discard)
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.WaitUntil(10*time.Second, func() bool { return !ed.CheckingProblems() })

	cs := startGameMCP(t, filepath.Join(dir, "scenes"))
	read := callIn(t, app, cs, editormcp.ToolSceneRead, map[string]any{"file": "scenes/room.scene.yaml"})
	if read.IsError || !strings.Contains(resultText(read), "\t      - id: hero\n") {
		t.Fatalf("scene_read: %s", resultText(read))
	}
	before := resultPNG(t, callIn(t, app, cs, editormcp.ToolViewportImage, nil))
	steps := ed.History.Index()

	placed := callIn(t, app, cs, editormcp.ToolSceneEdit, map[string]any{"edits": []any{
		map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_1", "name": "Slime", "x": 200, "y": 100, "components": map[string]any{"body2d": map[string]any{"mass": 3}}}},
	}})
	if placed.IsError {
		t.Fatalf("scene_edit: %s", resultText(placed))
	}
	if !app.Exists("id:scene-tree-label-slime_1") {
		t.Fatal("the placed object is not in the scene tree")
	}
	if ed.History.Index() != steps+1 || ed.History.UndoLabel() != "Agent: add slime_1" {
		t.Fatalf("history at %d %q, want one step from %d", ed.History.Index(), ed.History.UndoLabel(), steps)
	}
	if ed.Built.Object("slime_1") == nil {
		t.Fatal("the viewport's scene does not hold the placed object")
	}
	after := resultPNG(t, callIn(t, app, cs, editormcp.ToolViewportImage, nil))
	if bytes.Equal(before, after) {
		t.Fatal("the viewport looks the same after placing an object")
	}
	app.Screenshot("editormcp/placed-by-agent")

	app.Post(func() { ed.Undo() })
	app.Settle()
	if app.Exists("id:scene-tree-label-slime_1") {
		t.Fatal("Undo left the object in the scene tree")
	}
	if obj, _, _ := ed.FindObject("slime_1"); obj != nil || ed.History.Index() != steps {
		t.Fatalf("after Undo: %+v at step %d", obj, ed.History.Index())
	}
	if back := resultPNG(t, callIn(t, app, cs, editormcp.ToolViewportImage, nil)); !bytes.Equal(back, before) {
		t.Fatal("the viewport after Undo differs from before the edit")
	}

	// A refused edit names the object and the field and leaves the file on disk as it was.
	scene := filepath.Join(dir, "scenes", "room.scene.yaml")
	onDisk, err := os.ReadFile(scene)
	if err != nil {
		t.Fatal(err)
	}
	refused := callIn(t, app, cs, editormcp.ToolSceneEdit, map[string]any{"edits": []any{
		map[string]any{"op": "set", "id": "hero", "component": "body2d", "fields": map[string]any{"sped": 3}},
	}})
	if msg := resultText(refused); !refused.IsError || !strings.Contains(msg, `object "hero"`) || !strings.Contains(msg, `field "sped"`) || !strings.Contains(msg, "scenes/room.scene.yaml") {
		t.Fatalf("refused edit: %s", msg)
	}
	if now, err := os.ReadFile(scene); err != nil || !bytes.Equal(now, onDisk) {
		t.Fatalf("the refused edit changed %s: %v", scene, err)
	}
}

// go tool game mcp without an editor on the project answers each call with how to open it, and refuses a directory outside any project.
func TestGameMCPWithoutAnEditor(t *testing.T) {
	mcpEndpoints(t)
	dir := writeMCPFixture(t)
	cs := startGameMCP(t, dir)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: editormcp.ToolSelectionGet})
	if err != nil {
		t.Fatal(err)
	}
	if msg := resultText(res); !res.IsError || !strings.Contains(msg, "no editor has the project open") || !strings.Contains(msg, "go tool game editor") {
		t.Fatalf("selection_get without an editor: %s", msg)
	}

	a, _, stderr := newTestApp(t)
	a.stdin = strings.NewReader("")
	if code := run([]string{"mcp", "-project", t.TempDir()}, a); code != 1 || !strings.Contains(stderr.String(), "no game.yaml") {
		t.Fatalf("outside a project: exit %d, %s", code, stderr)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"mcp", "extra"}, a); code != 2 {
		t.Fatalf("an argument: exit %d, %s", code, stderr)
	}
}

// Acceptance tests for #1009:
// 1. `go tool game mcp install` run twice in a fixture project produces identical files, and a client started from that configuration connects to the open editor.
// 2. Connect an MCP client -> Connected, drop the connection -> Reconnecting with retry schedule (1, 2, 4, 8, 16 s), then Disconnected after five failures; Troubleshoot opens Connect at the failed step.
// 3. A request typed in the composer reaches a connected agent and its steps stream into the panel.
func TestGameMCPInstallConnectReconnectAndComposer(t *testing.T) {
	mcpEndpoints(t)
	dir := writeMCPFixture(t)

	// 1. Run `game mcp install` twice and verify byte-identical .mcp.json and .codex/config.toml.
	a1, stdout1, stderr1 := newTestApp(t)
	if code := run([]string{"mcp", "install", "-project", dir}, a1); code != 0 {
		t.Fatalf("first mcp install exit %d (stderr: %s)", code, stderr1)
	}
	if !strings.Contains(stdout1.String(), "Wrote .mcp.json") || !strings.Contains(stdout1.String(), "Wrote .codex/config.toml") {
		t.Fatalf("first mcp install stdout:\n%s", stdout1)
	}
	claudeBytes1, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	codexBytes1, err := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}

	a2, stdout2, stderr2 := newTestApp(t)
	if code := run([]string{"mcp", "install", "-project", dir}, a2); code != 0 {
		t.Fatalf("second mcp install exit %d (stderr: %s)", code, stderr2)
	}
	if !strings.Contains(stdout2.String(), "Unchanged .mcp.json") || !strings.Contains(stdout2.String(), "Unchanged .codex/config.toml") {
		t.Fatalf("second mcp install stdout:\n%s", stdout2)
	}
	claudeBytes2, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	codexBytes2, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if !bytes.Equal(claudeBytes1, claudeBytes2) {
		t.Fatalf(".mcp.json differed on second install")
	}
	if !bytes.Equal(codexBytes1, codexBytes2) {
		t.Fatalf(".codex/config.toml differed on second install")
	}

	// Parse the written .mcp.json configuration and verify the command it specifies.
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(claudeBytes1, &cfg); err != nil {
		t.Fatalf("parse .mcp.json: %v", err)
	}
	entry, ok := cfg.MCPServers[editormcp.ServerName]
	if !ok || entry.Command != "go" || !slices.Equal(entry.Args, []string{"tool", "game", "mcp"}) {
		t.Fatalf("unexpected .mcp.json entry: %+v", entry)
	}

	// Open the editor on the fixture project with an injectable retry scheduler for deterministic backoff testing.
	edApp, _, edErr := newTestApp(t)
	var ed *editor.Editor
	edApp.launch = func(e *editor.Editor) { ed = e }
	if code := run([]string{"editor", dir}, edApp); code != 0 {
		t.Fatalf("game editor exit %d (stderr: %s)", code, edErr)
	}
	t.Cleanup(ed.WaitForProblems)

	type retryItem struct {
		delay    time.Duration
		fn       func()
		canceled bool
	}
	var retries []*retryItem
	ed.AgentScheduleAfter = func(d time.Duration, fn func()) func() {
		it := &retryItem{delay: d, fn: fn}
		retries = append(retries, it)
		return func() { it.canceled = true }
	}
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.OpenConnectDialog()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		editorcmd.Serve(cx, ed, io.Discard)
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.WaitUntil(10*time.Second, func() bool { return !ed.CheckingProblems() })

	// Launch the MCP proxy using the subcommand from the written `.mcp.json` (`args[2:]` = `["mcp"]`) inside dir.
	t.Chdir(dir)
	proxyApp, _, proxyErr := newTestApp(t)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	proxyApp.stdin, proxyApp.stdout = inR, outW
	proxyDone := make(chan int, 1)
	go func() {
		code := run(entry.Args[2:], proxyApp)
		_ = outW.Close()
		proxyDone <- code
	}()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code"}, nil).Connect(context.Background(), &mcp.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatalf("connect from installed config: %v (stderr: %s)", err, proxyErr)
	}

	// Connecting via the installed config transitions the Connect dialog and status chip to Connected.
	// Wait for the MCP initialized handshake to populate the proxy client identity ("Claude Code over stdio"),
	// since serveConn registers the raw unix transport before notifications/initialized arrives.
	app.WaitUntil(10*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusConnected && ed.AgentStatusTooltip() == "Claude Code over stdio"
	})
	if tip := ed.AgentStatusTooltip(); tip != "Claude Code over stdio" {
		t.Fatalf("tooltip = %q, want Claude Code over stdio", tip)
	}
	if got := mcpViewText(t, app, "id:connect-waiting-text"); !strings.Contains(got, "Connected") {
		t.Fatalf("connect waiting indicator = %q, want Connected", got)
	}
	app.Press("id:connect-done")
	app.Settle()

	// 3. A request typed in the composer reaches the connected agent and its steps stream into the panel.
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Read scenes/room.scene.yaml", StepStatus: editor.AgentStepDone, StepIndex: 1, StepTotal: 2})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "2", StepTitle: "Checked prompt: " + req.Prompt, StepStatus: editor.AgentStepDone, StepIndex: 2, StepTotal: 2})
		emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Completed request."})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})
	app.Type("id:agent-composer-input", "Inspect hero position")
	app.Press("id:agent-composer-send")
	app.WaitUntil(5*time.Second, func() bool {
		return app.Exists("id:agent-turn-summary-0") && ed.AgentStatus() == editor.AgentStatusConnected
	})
	if got := mcpViewText(t, app, "id:agent-step-0-1"); !strings.Contains(got, "Inspect hero position") {
		t.Fatalf("streamed step = %q", got)
	}

	// 2. Drop the MCP connection -> Reconnecting with 1, 2, 4, 8, 16 s schedule -> Disconnected after 5 failures -> Troubleshoot opens Connect at failed step.
	ed.AgentReconnectProbe = func(context.Context) (editor.AgentClientInfo, error) {
		return editor.AgentClientInfo{}, errors.New("connection dropped")
	}
	_ = cs.Close()
	_ = inW.Close()
	<-proxyDone

	app.WaitUntil(10*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusReconnecting
	})
	var gotDelays []time.Duration
	for attempt := 1; attempt <= 5; attempt++ {
		if len(retries) == 0 {
			t.Fatalf("attempt %d: no retry scheduled", attempt)
		}
		last := retries[len(retries)-1]
		gotDelays = append(gotDelays, last.delay)
		last.fn()
		app.Settle()
	}
	wantDelays := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	if !slices.Equal(gotDelays, wantDelays) {
		t.Fatalf("reconnect schedule = %v, want %v", gotDelays, wantDelays)
	}
	if ed.AgentStatus() != editor.AgentStatusDisconnected {
		t.Fatalf("status after 5 failures = %q, want disconnected", ed.AgentStatus())
	}
	app.Press("id:agent-status-troubleshoot")
	app.Settle()
	if !ed.ConnectDialogOpen() || ed.ConnectDialogStep() != editor.ConnectStepWaiting {
		t.Fatalf("Troubleshoot opened=%v step=%q, want open at %q", ed.ConnectDialogOpen(), ed.ConnectDialogStep(), editor.ConnectStepWaiting)
	}
}

func mcpViewText(t *testing.T, app *nsgotest.App, sel string) string {
	t.Helper()
	v := app.Find(sel)
	if v == nil {
		t.Fatalf("selector %q not found", sel)
	}
	if v.Text != "" {
		return v.Text
	}
	var parts []string
	var walk func(n *nsgotest.View)
	walk = func(n *nsgotest.View) {
		if n.Text != "" {
			parts = append(parts, n.Text)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(v)
	return strings.Join(parts, " ")
}

// TestEditorAgentConnectScreenshots mounts the real NSGo Game Editor on macOS and captures
// screenshots of the Connect an agent dialog and the Agent panel across its status chip states
// for side-by-side visual review against docs/design/editor/Connect.dc.html and AgentStates.dc.html.
func TestEditorAgentConnectScreenshots(t *testing.T) {
	ed, err := newDemoEditor()
	if err != nil {
		t.Fatalf("newDemoEditor: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	now := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	_ = ed.SetAppearance(editor.AppearanceDark)

	var retries []func()
	ed.AgentScheduleAfter = func(_ time.Duration, fn func()) func() {
		retries = append(retries, fn)
		return func() {}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	post := func(fn func()) {
		done := make(chan struct{})
		ed.Post(func() {
			fn()
			close(done)
		})
		app.WaitUntil(5*time.Second, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		})
		app.Settle()
	}

	// 1. Connect dialog (local choice, Waiting state) beside Connect.dc.html.
	post(func() { ed.OpenConnectDialog() })
	app.Screenshot("agent/connect-waiting")

	// 2. Connect dialog once first MCP client attaches -> green Connected state.
	post(func() {
		ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
			ID:        "c1",
			Name:      "claude-code",
			Transport: "stdio",
		})
	})
	app.Screenshot("agent/connect-connected")

	// 3. Agent panel (Connected · MCP + streamed steps + composer).
	app.Press("id:connect-done")
	app.Settle()
	post(func() {
		ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, _ editor.AgentRequest, emit func(editor.AgentEvent)) error {
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Read level_01.scene.yaml and slime.prefab.yaml", StepStatus: editor.AgentStepDone, StepIndex: 1, StepTotal: 3})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "2", StepTitle: "Placed 3 slimes and 3 coins on layer actors", StepStatus: editor.AgentStepDone, StepIndex: 2, StepTotal: 3})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "3", StepTitle: "Ran game check · no problems", StepStatus: editor.AgentStepDone, StepIndex: 3, StepTotal: 3})
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Done. The slimes patrol the lower floor and the coins lead toward the door."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		})
		_ = ed.SubmitAgentPrompt("Put three slimes patrolling the lower floor and a short coin trail toward the door.")
	})
	app.WaitUntil(5*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusConnected && app.Exists("id:agent-turn-summary-0")
	})
	app.Settle()
	app.Screenshot("agent/panel-connected")

	// 4. Waiting for you · 0:32 state.
	post(func() {
		ed.SetAgentWaitingForYou(editor.AgentOpenRequest{
			ID:       "req-playtest",
			Title:    "Allow the agent to run the game?",
			Body:     "It wants to play level_01 headless for 10 seconds to check the slimes stay on the floor. Nothing is saved.",
			OpenedAt: now.Add(-32 * time.Second),
		})
	})
	app.Screenshot("agent/panel-waiting")

	// 5. Reconnecting · try 3 of 5 state.
	post(func() {
		ed.ClearAgentWaitingForYou()
		ed.AgentReconnectProbe = func(context.Context) (editor.AgentClientInfo, error) {
			return editor.AgentClientInfo{}, errors.New("offline")
		}
		ed.NotifyAgentMCPDisconnected("c1", "Claude Code stopped answering over MCP.")
		for range 2 {
			if len(retries) > 0 {
				retries[len(retries)-1]()
			}
		}
	})
	app.Screenshot("agent/panel-reconnecting")

	// 6. Disconnected with Reconnect and Troubleshoot buttons.
	post(func() {
		for range 3 {
			if len(retries) > 0 {
				retries[len(retries)-1]()
			}
		}
	})
	app.Screenshot("agent/panel-disconnected")
}
