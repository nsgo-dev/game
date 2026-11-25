package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

	app.Press("id:toolbar-undo")
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
