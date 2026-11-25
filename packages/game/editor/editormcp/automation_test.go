package editormcp_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// RegisterAutomation offers the server's tools to nsgo mcp: each runs on the editor's goroutine from start to end, a refused edit is the same EditError, and an image comes as base64.
func TestRegisterAutomation(t *testing.T) {
	f := openFixture(t)
	var root *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		root = cx
		s := editormcp.New(f.ed, editormcp.Options{Run: cx.Post})
		if err := s.RegisterAutomation(cx); err != nil {
			t.Error(err)
		}
		cx.Cleanup(func() { _ = s.Close() })
		return core.View{}
	})
	callTool := func(name string, args string) (editormcp.AutomationResult, error) {
		t.Helper()
		var raw json.RawMessage
		var err error
		app.Post(func() { raw, err = automation.Call(root, context.Background(), name, json.RawMessage(args)) })
		app.Settle()
		var out editormcp.AutomationResult
		if err == nil {
			if jerr := json.Unmarshal(raw, &out); jerr != nil {
				t.Fatal(jerr)
			}
		}
		return out, err
	}

	out, err := callTool(editormcp.ToolSceneRead, `{}`)
	if err != nil || !strings.Contains(out.Text, "id: hero") {
		t.Fatalf("scene_read: %v %q", err, out.Text)
	}
	out, err = callTool(editormcp.ToolSceneEdit, `{"edits":[{"op":"move","id":"hero","x":1,"y":2}]}`)
	if err != nil || !strings.Contains(out.Text, "one undo step") {
		t.Fatalf("scene_edit: %v %q", err, out.Text)
	}
	if hero, _, _ := f.ed.FindObject("hero"); hero.X != 1 || hero.Y != 2 {
		t.Fatalf("hero at %v, %v", hero.X, hero.Y)
	}
	_, err = callTool(editormcp.ToolSceneEdit, `{"edits":[{"op":"remove","id":"nobody"}]}`)
	if edit, ok := errors.AsType[*editormcp.EditError](err); !ok || edit.Object != "nobody" {
		t.Fatalf("refused edit: %v", err)
	}
	out, err = callTool(editormcp.ToolViewportImage, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := base64.StdEncoding.DecodeString(out.PNGBase64); err != nil || !strings.HasPrefix(string(data), "\x89PNG") {
		t.Fatalf("viewport_image's PNG: %v", err)
	}
	out, err = callTool(editormcp.ToolGameCheck, `{}`)
	if err != nil || !strings.Contains(out.Text, "ghost") {
		t.Fatalf("game_check: %v %q", err, out.Text)
	}
}
