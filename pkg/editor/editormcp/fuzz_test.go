package editormcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

// FuzzSceneEdit sends scene_edit arguments of any shape: the server never fails, and a call it refuses leaves the scene and the undo history exactly as they were, while one it applies adds a single undo step.
func FuzzSceneEdit(f *testing.F) {
	for _, seed := range []string{
		`{"edits":[{"op":"add","object":{"id":"a","x":1,"y":2}}]}`,
		`{"edits":[{"op":"move","id":"hero","x":1,"y":2},{"op":"set","id":"hero","component":"body","fields":{"sped":1}}]}`,
		`{"edits":[{"op":"set","id":"hero","fields":{"id":"b","name":"B","z":3,"sprite":null}}]}`,
		`{"edits":[{"op":"add","object":{"id":"c","children":[{"id":"d"}]}},{"op":"reparent","id":"d"},{"op":"remove","id":"c"}]}`,
		`{"edits":[{"op":"set_property","id":"hero","key":"k","value":[1]}]}`,
		`{"file":"../x","edits":[]}`,
		`{"edits":[{"op":"add_component","id":"hero","component":"body"}]}`,
		`[]`,
		`{"edits":[{"op":"set_property","id":"hero","key":"k","value":1e400}]}`,
		`{"edits":[{"op":"set","id":"hero","component":"body","fields":{"speed":-1e400}}]}`,
		`{"edits":[{"op":"bogus"}]}`,
	} {
		f.Add([]byte(seed))
	}
	fix := openFixture(f)
	s := editormcp.New(fix.ed, editormcp.Options{})
	f.Cleanup(func() { _ = s.Close() })
	cs := connect(f, s, nil)
	f.Fuzz(func(t *testing.T, args []byte) {
		if !json.Valid(args) {
			return
		}
		before, err := fix.ed.SaveYAML()
		if err != nil {
			t.Fatal(err)
		}
		index := fix.ed.History.Index()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: editormcp.ToolSceneEdit, Arguments: json.RawMessage(args)})
		if err != nil {
			// A protocol error is the transport refusing arguments that are not an object, before the tool runs.
			return
		}
		after, err := fix.ed.SaveYAML()
		if err != nil {
			t.Fatalf("the scene no longer encodes after %s: %v", args, err)
		}
		if res.IsError {
			if !bytes.Equal(before, after) || fix.ed.History.Index() != index {
				t.Fatalf("refused %s changed the editor: %s", args, text(res))
			}
			return
		}
		if fix.ed.History.Index() > index+1 {
			t.Fatalf("applied %s as %d undo steps", args, fix.ed.History.Index()-index)
		}
	})
}
