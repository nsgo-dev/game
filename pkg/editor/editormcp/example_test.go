package editormcp_test

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
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
	// agent_report_step read
	// agent_turn_get read
	// agent_turn_reply edit
	// brief_edit edit
	// checkpoint_list read
	// checkpoint_restore edit
	// code_review_propose code
	// code_review_run_checks run
	// code_review_status read
	// cutscene_scrub read
	// cutscene_write edit
	// datatable_review_checks edit
	// datatable_review_propose edit
	// datatable_review_status read
	// file_edit edit
	// game_check run
	// game_delete_file delete
	// game_playtest run
	// history_list read
	// merge_abort run
	// merge_finish run
	// merge_resolve edit
	// merge_status read
	// object_copy_yaml read
	// object_duplicate edit
	// object_find_usages read
	// object_group edit
	// object_locate_yaml read
	// object_move_under edit
	// object_paste_yaml edit
	// object_select_same_prefab read
	// object_set_field edit
	// object_visibility edit
	// permissions_draft read
	// plan_propose read
	// plan_step_update read
	// play_frozen_context read
	// play_replay_step run
	// playtest_define edit
	// playtest_report edit
	// problem_propose_fix read
	// project_context read
	// refactor_rename edit
	// refs_find read
	// release_notes_draft edit
	// release_review_uploads run
	// review_notes_get read
	// review_redo edit
	// review_status read
	// rules_edit edit
	// scene_edit edit
	// scene_name read
	// scene_read read
	// selection_get read
	// sketch_read read
	// sketch_reading edit
	// states_test run
	// states_write edit
	// tasks_control edit
	// tasks_list read
	// tasks_submit edit
	// tile_autotile edit
	// tile_erase edit
	// tile_paint edit
	// tile_query read
	// tile_review_keep edit
	// tile_review_restore_region edit
	// tile_review_revert_all edit
	// tile_review_revert_region edit
	// tile_review_status read
	// tileset_edit edit
	// tileset_read read
	// turn_end edit
	// turn_start edit
	// turn_status read
	// viewport_image read
}
