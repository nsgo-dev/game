package editormcp_test

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

// An agent connected to the editor's server places an object with scene_edit, which the editor holds as one undo step, and reads the scene back with scene_read.
func Example() {
	ctx := context.Background()
	ed, err := editor.New(nil, nil)
	if err != nil {
		panic(err)
	}
	s := editormcp.New(ed, editormcp.Options{})
	defer s.Close()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, st); err != nil {
		panic(err)
	}
	agent, err := mcp.NewClient(&mcp.Implementation{Name: "agent"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		panic(err)
	}
	defer agent.Close()

	res, err := agent.CallTool(ctx, &mcp.CallToolParams{Name: editormcp.ToolSceneEdit, Arguments: map[string]any{
		"edits": []any{map[string]any{"op": "add", "object": map[string]any{"id": "crate", "x": 32, "y": 16}}},
	}})
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Content[0].(*mcp.TextContent).Text)
	fmt.Println(ed.History.UndoLabel())

	res, err = agent.CallTool(ctx, &mcp.CallToolParams{Name: editormcp.ToolSceneRead, Arguments: map[string]any{"offset": 9}})
	if err != nil {
		panic(err)
	}
	fmt.Print(res.Content[0].(*mcp.TextContent).Text)
	// Output:
	// Applied 1 edit to the open scene as one undo step, "Agent: add crate"; it is not saved. Added crate.
	// Agent: add crate
	// the open scene (as the editor holds it, with unsaved edits): lines 9-13 of 13
	//      9	    space: world
	//     10	    objects:
	//     11	      - id: crate
	//     12	        x: 32
	//     13	        "y": 16
}

// Another part of the editor adds its tool to the server's registry; its handler reaches the editor through Call.Editor.
func ExampleServer_AddTool() {
	ed, err := editor.New(nil, nil)
	if err != nil {
		panic(err)
	}
	s := editormcp.New(ed, editormcp.Options{})
	defer s.Close()
	err = s.AddTool(editormcp.Tool{
		Name:        "scene_name",
		Title:       "Name the scene",
		Description: "The open scene's name.",
		Action:      editormcp.ActionRead,
		Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
			var name string
			err := c.Editor(ctx, func(ed *editor.Editor) error {
				name = ed.Doc.Name
				return nil
			})
			return &editormcp.Result{Text: name}, err
		},
	})
	fmt.Println(err)
	for _, t := range s.Tools() {
		fmt.Println(t.Name, t.Action)
	}
	// Output:
	// <nil>
	// cutscene_scrub read
	// cutscene_write edit
	// game_check run
	// game_playtest run
	// merge_abort run
	// merge_finish run
	// merge_resolve edit
	// merge_status read
	// playtest_define edit
	// playtest_report edit
	// refactor_rename edit
	// refs_find read
	// scene_edit edit
	// scene_name read
	// scene_read read
	// selection_get read
	// states_test run
	// states_write edit
	// tileset_edit edit
	// tileset_read read
	// viewport_image read
}
