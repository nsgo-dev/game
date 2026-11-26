package editormcp_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

// startProxy runs a proxy for root and connects a client to it in memory, speaking protocol when it is not empty.
func startProxy(t *testing.T, root string, opts *mcp.ClientOptions, protocol string) *mcp.ClientSession {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- editormcp.NewProxy(root).Run(ctx, st) }()
	var session *mcp.ClientSessionOptions
	if protocol != "" {
		session = &mcp.ClientSessionOptions{ProtocolVersion: protocol}
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "agent"}, opts).Connect(context.Background(), ct, session)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		<-done
	})
	return cs
}

// listNames lists the tools a client sees.
func listNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// Without an editor the proxy lists the built-in tools and answers each call with how to open the project; once an editor opens it, calls reach the editor, tools the editor adds appear, and after the editor closes and opens again the proxy reconnects.
func TestProxyFindsTheEditor(t *testing.T) {
	endpointDir(t)
	dir := t.TempDir()
	writeProject(t, dir)
	root := mustRoot(t, dir)
	changed := make(chan struct{}, 8)
	cs := startProxy(t, root, &mcp.ClientOptions{ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
		select {
		case changed <- struct{}{}:
		default:
		}
	}}, "")

	if names := listNames(t, cs); !slices.Equal(names, []string{"agent_turn_get", "agent_turn_reply", "brief_edit", "checkpoint_list", "checkpoint_restore", "cutscene_scrub", "cutscene_write", "datatable_review_checks", "datatable_review_propose", "datatable_review_status", "game_check", "game_playtest", "history_list", "merge_abort", "merge_finish", "merge_resolve", "merge_status", "object_copy_yaml", "object_duplicate", "object_find_usages", "object_group", "object_locate_yaml", "object_move_under", "object_paste_yaml", "object_select_same_prefab", "object_set_field", "object_visibility", "permissions_draft", "play_frozen_context", "play_replay_step", "playtest_define", "playtest_report", "problem_propose_fix", "project_context", "refactor_rename", "refs_find", "release_notes_draft", "release_review_uploads", "review_notes_get", "review_redo", "review_status", "rules_edit", "scene_edit", "scene_read", "selection_get", "sketch_read", "sketch_reading", "states_test", "states_write", "tasks_control", "tasks_list", "tasks_submit", "tile_autotile", "tile_erase", "tile_paint", "tile_query", "tile_review_keep", "tile_review_restore_region", "tile_review_revert_all", "tile_review_revert_region", "tile_review_status", "tileset_edit", "tileset_read", "viewport_image"}) {
		t.Fatalf("tools without an editor: %v", names)
	}
	res := call(t, cs, editormcp.ToolSceneRead, nil)
	if !res.IsError || !strings.Contains(text(res), "no editor has the project open") || !strings.Contains(text(res), "go tool game editor "+root) {
		t.Fatalf("a call without an editor: %s", text(res))
	}

	ed := newEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	s := editormcp.New(ed, editormcp.Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	out := mustOK(t, call(t, cs, editormcp.ToolSceneRead, nil))
	if !strings.Contains(out, "id: hero") {
		t.Fatalf("scene_read through the proxy:\n%s", out)
	}
	if err := s.AddTool(editormcp.Tool{Name: "custom_action", Action: editormcp.ActionRead, Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) {
		return &editormcp.Result{Text: "played"}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	for !slices.Contains(listNames(t, cs), "custom_action") {
		<-changed
	}
	if out := mustOK(t, call(t, cs, "custom_action", nil)); out != "played" {
		t.Fatalf("custom_action through the proxy: %q", out)
	}

	// The editor closes: calls fail with how to open the project again, then reach the new editor.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if res := call(t, cs, editormcp.ToolSelectionGet, nil); !res.IsError || !strings.Contains(text(res), "go tool game editor") {
		t.Fatalf("a call after the editor closed: %s", text(res))
	}
	s2 := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s2.Close() })
	if err := s2.Open(); err != nil {
		t.Fatal(err)
	}
	mustOK(t, call(t, cs, editormcp.ToolSelectionGet, nil))
	for slices.Contains(listNames(t, cs), "custom_action") {
		<-changed
	}
}

// A client subscribed through the proxy hears of the changes in the editor, the subscription made before an editor was open included.
func TestProxyForwardsTheChangeStream(t *testing.T) {
	endpointDir(t)
	dir := t.TempDir()
	writeProject(t, dir)
	root := mustRoot(t, dir)
	got := make(chan *mcp.ResourceUpdatedNotificationParams, 16)
	cs := startProxy(t, root, &mcp.ClientOptions{ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
		got <- req.Params
	}}, legacyProtocol)
	if err := cs.Subscribe(context.Background(), &mcp.SubscribeParams{URI: editormcp.SceneURI}); err != nil {
		t.Fatal(err)
	}

	ed := newEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	// The first call connects, and connecting subscribes.
	mustOK(t, call(t, cs, editormcp.ToolSelectionGet, nil))
	if err := ed.MoveObject("hero", 3, 4); err != nil {
		t.Fatal(err)
	}
	n := <-got
	if n.URI != editormcp.SceneURI || n.Meta[editormcp.MetaBy] != "person" {
		t.Fatalf("notice %+v", n)
	}
	read, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.SceneURI})
	if err != nil || !strings.Contains(read.Contents[0].Text, "x: 3") {
		t.Fatalf("scene through the proxy: %v %+v", err, read)
	}
}
