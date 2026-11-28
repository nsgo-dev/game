package editormcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/project"
)

// The server lists the five tools named as Settings › Tools names them, each with its action, and the two resources of the change stream.
func TestServerListsToolsAndResources(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	var names []string
	actions := map[string]any{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
		actions[tool.Name] = tool.Meta["nsgo.dev/action"]
		if tool.Description == "" || tool.InputSchema == nil {
			t.Errorf("%s has no description or schema", tool.Name)
		}
	}
	slices.Sort(names)
	want := []string{"agent_report_step", "agent_turn_get", "agent_turn_reply", "brief_edit", "checkpoint_list", "checkpoint_restore", "code_review_propose", "code_review_run_checks", "code_review_status", "cutscene_scrub", "cutscene_write", "datatable_review_checks", "datatable_review_propose", "datatable_review_status", "file_edit", "game_check", "game_playtest", "history_list", "merge_abort", "merge_finish", "merge_resolve", "merge_status", "object_copy_yaml", "object_duplicate", "object_find_usages", "object_group", "object_locate_yaml", "object_move_under", "object_paste_yaml", "object_select_same_prefab", "object_set_field", "object_visibility", "permissions_draft", "phone_call_tool", "phone_sessions_list", "plan_propose", "plan_step_update", "play_frozen_context", "play_replay_step", "playtest_define", "playtest_report", "problem_propose_fix", "project_context", "refactor_rename", "refs_find", "release_notes_draft", "release_review_uploads", "review_notes_get", "review_redo", "review_status", "rules_edit", "scene_edit", "scene_read", "selection_get", "sketch_read", "sketch_reading", "skill_get", "states_test", "states_write", "tasks_control", "tasks_list", "tasks_submit", "tile_autotile", "tile_erase", "tile_paint", "tile_query", "tile_review_keep", "tile_review_restore_region", "tile_review_revert_all", "tile_review_revert_region", "tile_review_status", "tileset_edit", "tileset_read", "turn_end", "turn_start", "turn_status", "viewport_image"}
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	if actions["scene_edit"] != "edit" || actions["scene_read"] != "read" || actions["game_check"] != "run" || actions["game_playtest"] != "run" || actions["playtest_define"] != "edit" || actions["agent_turn_get"] != "read" || actions["agent_turn_reply"] != "edit" || actions["project_context"] != "read" || actions["brief_edit"] != "edit" || actions["rules_edit"] != "edit" || actions["review_status"] != "read" || actions["review_notes_get"] != "read" || actions["review_redo"] != "edit" || actions["code_review_propose"] != "code" || actions["code_review_status"] != "read" || actions["code_review_run_checks"] != "run" || actions["plan_propose"] != "read" || actions["plan_step_update"] != "read" {
		t.Fatalf("actions = %v", actions)
	}
	var uris []string
	for r, err := range cs.Resources(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		uris = append(uris, r.URI)
	}
	slices.Sort(uris)
	wantResources := []string{editormcp.AgentTurnURI, editormcp.BriefURI, editormcp.ContextURI, editormcp.RulesURI, editormcp.SceneURI, editormcp.SelectionURI, "skill://nsgo-game/SKILL.md", "skill://nsgo-game/references/cli-and-formats.md", "skill://nsgo-game/references/mcp-tools.md", "skill://nsgo-game/references/packages.md"}
	slices.Sort(wantResources)
	if !slices.Equal(uris, wantResources) {
		t.Fatalf("resources = %v", uris)
	}
}

// mcpAppsClient declares MCP Apps support as a chat client that renders HTML widgets does.
func mcpAppsClient() *mcp.ClientOptions {
	return &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{Extensions: map[string]any{
		"io.modelcontextprotocol/ui": map[string]any{"mimeTypes": []any{"text/html;profile=mcp-app"}},
	}}}
}

// resourceURIs lists the resource URIs a client sees.
func resourceURIs(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	var uris []string
	for r, err := range cs.Resources(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		uris = append(uris, r.URI)
	}
	return uris
}

// widgetURI is the _meta.ui.resourceUri of a tool result, or "" when it has none.
func widgetURI(res *mcp.CallToolResult) string {
	ui, _ := res.Meta["ui"].(map[string]any)
	uri, _ := ui["resourceUri"].(string)
	return uri
}

// A client that declares MCP Apps sees the widget resources, reads self-contained HTML with no network access, and gets the widget URI on viewport_image; one that does not sees neither and cannot read them; a refused viewport_image points at no widget.
func TestMCPAppsWidgetsAreNegotiatedAndGated(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	apps := connect(t, s, mcpAppsClient())
	if uris := resourceURIs(t, apps); !slices.Contains(uris, editormcp.PlaytestWidgetURI) || !slices.Contains(uris, editormcp.ViewportWidgetURI) {
		t.Fatalf("MCP Apps client resources = %v", uris)
	}
	for _, uri := range []string{editormcp.PlaytestWidgetURI, editormcp.ViewportWidgetURI} {
		read, err := apps.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err != nil || len(read.Contents) != 1 {
			t.Fatalf("read %s = %+v, err=%v", uri, read, err)
		}
		html := read.Contents[0].Text
		if read.Contents[0].MIMEType != "text/html;profile=mcp-app" || !strings.Contains(html, `content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'"`) || strings.Contains(html, "http://") || strings.Contains(html, "https://") || strings.Contains(html, "<script") || strings.Contains(html, "connect-src") {
			t.Fatalf("widget %s = %s %q", uri, read.Contents[0].MIMEType, html)
		}
	}
	res := call(t, apps, editormcp.ToolViewportImage, map[string]any{})
	mustOK(t, res)
	if got := widgetURI(res); got != editormcp.ViewportWidgetURI {
		t.Fatalf("viewport_image widget = %q, meta %+v", got, res.Meta)
	}

	plain := connect(t, s, nil)
	if uris := resourceURIs(t, plain); slices.Contains(uris, editormcp.PlaytestWidgetURI) || slices.Contains(uris, editormcp.ViewportWidgetURI) || !slices.Contains(uris, editormcp.SceneURI) {
		t.Fatalf("plain client resources = %v", uris)
	}
	if _, err := plain.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.PlaytestWidgetURI}); err == nil || !strings.Contains(err.Error(), "io.modelcontextprotocol/ui") {
		t.Fatalf("plain client read a widget: err=%v", err)
	}
	res = call(t, plain, editormcp.ToolViewportImage, map[string]any{})
	mustOK(t, res)
	if _, ok := res.Meta["ui"]; ok {
		t.Fatalf("plain client viewport_image meta = %+v", res.Meta)
	}

	if err := f.ed.SetAgentPrivacy(editor.PrivacyViewportImages, false); err != nil {
		t.Fatal(err)
	}
	res = call(t, apps, editormcp.ToolViewportImage, map[string]any{})
	if !res.IsError || widgetURI(res) != "" {
		t.Fatalf("viewport_image with viewport images off: error=%v meta=%+v", res.IsError, res.Meta)
	}
}

func TestSkillGetReturnsEmbeddedSection(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	res := call(t, cs, editormcp.ToolSkillGet, map[string]any{"file": "SKILL.md", "section": "Core rules"})
	if res.IsError || !strings.Contains(text(res), "Core rules") {
		t.Fatalf("skill_get = error:%v text:%q", res.IsError, text(res))
	}
	bad := call(t, cs, editormcp.ToolSkillGet, map[string]any{"file": "SKILL.md", "section": "Missing"})
	if !bad.IsError {
		t.Fatal("unknown skill section accepted")
	}
}

// scene_read returns the open scene with line numbers as the editor holds it, other files from disk, game.yaml as the editor holds it, and refuses paths outside the project.
func TestSceneRead(t *testing.T) {
	f := openFixture(t)
	// The person wrote the scene by hand, with a comment the editor's encoding would drop.
	if err := os.WriteFile(f.scenePath(), []byte("# The room the hero starts in.\n"+fixtureRoom), 0o644); err != nil {
		t.Fatal(err)
	}
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolSceneRead, map[string]any{})
	out := mustOK(t, res)
	var info editormcp.FileText
	structured(t, res, &info)
	// With no unsaved edits the open scene is the file on disk, its comment and line numbers as game_check reports them.
	if info.Source != "disk" || info.Unsaved || !strings.HasPrefix(out, "scenes/room.scene.yaml: lines 1-") || !strings.Contains(out, "\n     1\t# The room the hero starts in.\n") || !strings.Contains(out, "\n     2\tversion: 2\n") || !strings.Contains(out, "\t      - id: hero\n") {
		t.Fatalf("open scene %+v:\n%s", info, out)
	}
	res = call(t, cs, editormcp.ToolSceneRead, map[string]any{"offset": 2, "limit": math.MaxInt})
	out = mustOK(t, res)
	structured(t, res, &info)
	if info.From != 2 || info.To != info.Lines || info.Lines < 10 {
		t.Fatalf("the largest limit %+v:\n%s", info, out)
	}

	if err := f.ed.MoveObject("hero", 70, 40); err != nil {
		t.Fatal(err)
	}
	res = call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "scenes/room.scene.yaml", "offset": 2, "limit": 3})
	out = mustOK(t, res)
	structured(t, res, &info)
	if !info.Unsaved || info.Source != "editor" || info.From != 2 || info.To != 4 || !strings.Contains(out, "with unsaved edits") || !strings.Contains(out, "call again with offset 5") || strings.Contains(out, "\n     1\t") {
		t.Fatalf("moved scene %+v:\n%s", info, out)
	}

	out = mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "data/items.yaml"}))
	if !strings.HasPrefix(out, "data/items.yaml: lines 1-4 of 4\n") || !strings.Contains(out, "     4\t    damage: 3\n") {
		t.Fatalf("data file:\n%s", out)
	}
	out = mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "game.yaml"}))
	if !strings.HasPrefix(out, "game.yaml: lines 1-") || !strings.Contains(out, "\t# The fixture's project file.\n") || !strings.Contains(out, "mainScene: room") {
		t.Fatalf("unedited project file:\n%s", out)
	}
	if err := f.ed.EditProject("Rename", func(p *project.File) { p.Name = "Renamed" }); err != nil {
		t.Fatal(err)
	}
	out = mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "game.yaml"}))
	if !strings.Contains(out, "game.yaml (as the editor holds it, with unsaved edits") || !strings.Contains(out, "name: Renamed") {
		t.Fatalf("edited project file:\n%s", out)
	}

	for file, want := range map[string]string{
		"../outside.yaml":          "leaves the project",
		"/etc/passwd":              "not an absolute path",
		"scenes/missing.yaml":      "no such file",
		"art/actors.png":           "not a text file",
		"scenes":                   "not a file",
		"scenes/../../x.yaml":      "leaves the project",
		"scenes/./room.scene.yaml": "",
	} {
		res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": file})
		switch {
		case want == "" && res.IsError:
			t.Errorf("%s: %s", file, text(res))
		case want != "" && (!res.IsError || !strings.Contains(text(res), want)):
			t.Errorf("%s: error %v %q, want one saying %q", file, res.IsError, text(res), want)
		}
	}
	if res := call(t, cs, editormcp.ToolSceneRead, map[string]any{"fil": "x"}); !res.IsError || !strings.Contains(text(res), "unknown field") {
		t.Errorf("misspelled argument: %v %s", res.IsError, text(res))
	}
}

// The acceptance flow without a window: scene_edit places an object, which the editor holds as one undo step; Undo removes it.
func TestSceneEditPlacesAnObjectAsOneUndoStep(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	before := f.ed.History.Index()

	res := call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{
				"id": "slime_1", "x": 200, "y": 100,
				"sprite":     map[string]any{"atlas": "actors", "frame": "slime"},
				"components": map[string]any{"body": map[string]any{"speed": 12}},
			}},
			map[string]any{"op": "set", "id": "slime_1", "fields": map[string]any{"name": "Slime", "z": 3}},
			map[string]any{"op": "set_property", "id": "slime_1", "key": "hp", "value": 4},
		},
	})
	out := mustOK(t, res)
	var got editormcp.EditResult
	structured(t, res, &got)
	if !slices.Equal(got.Objects, []string{"slime_1"}) || got.Undo != "Agent: 3 scene edits" || !strings.Contains(out, "one undo step") {
		t.Fatalf("result %+v: %s", got, out)
	}
	obj, _, _ := f.ed.FindObject("slime_1")
	if obj == nil || obj.Name != "Slime" || obj.Z != 3 || obj.Properties["hp"] != 4 || fmt.Sprint(obj.Components["body"].(map[string]any)["speed"]) != "12" {
		t.Fatalf("placed %+v", obj)
	}
	if f.ed.History.Index() != before+1 || f.ed.Built.Object("slime_1") == nil {
		t.Fatalf("history index %d (was %d), built %v", f.ed.History.Index(), before, f.ed.Built.Object("slime_1"))
	}
	if !f.ed.Undo() {
		t.Fatal("nothing to undo")
	}
	if obj, _, _ := f.ed.FindObject("slime_1"); obj != nil || f.ed.History.Index() != before {
		t.Fatalf("after undo: %+v at %d", obj, f.ed.History.Index())
	}
	if !f.ed.Redo() {
		t.Fatal("nothing to redo")
	}
	if obj, _, _ := f.ed.FindObject("slime_1"); obj == nil || obj.Name != "Slime" {
		t.Fatalf("after redo: %+v", obj)
	}
}

// scene_read reports a scene version that the selection and the view leave alone, scene_edit refuses a stale baseVersion without changing anything, and the version the conflict reports makes the same write succeed.
func TestSceneEditRefusesStaleBaseVersion(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	read := func() string {
		t.Helper()
		var info editormcp.FileText
		structured(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{}), &info)
		if info.Version == "" {
			t.Fatalf("scene_read reported no version: %+v", info)
		}
		return info.Version
	}
	base := read()
	f.ed.RunHeadless(func() {
		f.ed.SelectObject("hero")
		f.ed.PanView2D(5, 5)
	})
	if v := read(); v != base {
		t.Fatalf("selection and view changed the scene version from %s to %s", base, v)
	}
	var sel editormcp.Selection
	structured(t, call(t, cs, editormcp.ToolSelectionGet, map[string]any{}), &sel)
	if sel.SceneVersion != base {
		t.Fatalf("selection_get sceneVersion = %q, want %q", sel.SceneVersion, base)
	}

	// The person moves the hero between the agent's read and its write.
	f.ed.RunHeadless(func() {
		if err := f.ed.MoveObject("hero", 70, 40); err != nil {
			t.Fatal(err)
		}
	})
	var before []byte
	var index, length int
	f.ed.RunHeadless(func() {
		before, _ = f.ed.SaveYAML()
		index, length = f.ed.History.Index(), f.ed.History.Len()
	})
	onDisk, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	edit := map[string]any{"baseVersion": base, "edits": []any{
		map[string]any{"op": "move", "id": "hero", "x": 110, "y": 20},
		map[string]any{"op": "add", "layer": "props", "object": map[string]any{"id": "crate", "x": 1, "y": 2}},
	}}
	res := call(t, cs, editormcp.ToolSceneEdit, edit)
	if !res.IsError || !strings.Contains(text(res), "changed since you read it") {
		t.Fatalf("stale scene_edit = error:%v text:%q", res.IsError, text(res))
	}
	f.ed.RunHeadless(func() {
		after, _ := f.ed.SaveYAML()
		if !bytes.Equal(after, before) || f.ed.History.Index() != index || f.ed.History.Len() != length {
			t.Errorf("a refused scene_edit changed the scene or its history")
		}
	})
	if now, err := os.ReadFile(f.scenePath()); err != nil || !bytes.Equal(now, onDisk) {
		t.Fatalf("a refused scene_edit changed the file: %v", err)
	}

	edit["baseVersion"] = read()
	res = call(t, cs, editormcp.ToolSceneEdit, edit)
	mustOK(t, res)
	var out editormcp.EditResult
	structured(t, res, &out)
	if out.Version == "" || out.Version != read() {
		t.Fatalf("scene_edit version %q is not the scene's version after the edit", out.Version)
	}
}

// A repeated scene_edit with the same requestId and arguments applies once and returns the first result, even when its baseVersion is stale by then; the same requestId with other arguments is refused.
func TestSceneEditRequestIDAppliesOnce(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	var info editormcp.FileText
	structured(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{}), &info)
	add := map[string]any{"baseVersion": info.Version, "requestId": "add-crate-1", "edits": []any{
		map[string]any{"op": "add", "layer": "props", "object": map[string]any{"id": "crate", "x": 1, "y": 2}},
	}}
	first := call(t, cs, editormcp.ToolSceneEdit, add)
	mustOK(t, first)
	var length int
	f.ed.RunHeadless(func() { length = f.ed.History.Len() })
	again := call(t, cs, editormcp.ToolSceneEdit, add)
	if text(again) != mustOK(t, first) {
		t.Fatalf("retried scene_edit = %q, want the first result %q", text(again), text(first))
	}
	f.ed.RunHeadless(func() {
		if f.ed.History.Len() != length {
			t.Errorf("a retried scene_edit applied again")
		}
		if obj, _, _ := f.ed.FindObject("crate"); obj == nil {
			t.Errorf("the crate is missing after a retried add")
		}
	})
	add["edits"] = []any{map[string]any{"op": "move", "id": "hero", "x": 1, "y": 1}}
	res := call(t, cs, editormcp.ToolSceneEdit, add)
	if !res.IsError || !strings.Contains(text(res), "different arguments") {
		t.Fatalf("reused requestId = error:%v text:%q", res.IsError, text(res))
	}
	// A call that failed leaves its requestId free for the corrected call.
	bad := map[string]any{"requestId": "move-1", "edits": []any{map[string]any{"op": "move", "id": "nobody", "x": 1, "y": 1}}}
	if res := call(t, cs, editormcp.ToolSceneEdit, bad); !res.IsError {
		t.Fatalf("moving a missing object succeeded: %s", text(res))
	}
	if res := call(t, cs, editormcp.ToolSceneEdit, bad); !res.IsError || strings.Contains(text(res), "requestId") {
		t.Fatalf("a failed call's requestId was kept: %s", text(res))
	}
}

func TestSceneEditRecordsAttributionOnActiveAgentThread(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	req, err := f.ed.SendAgentRequest("Move the hero right")
	if err != nil {
		t.Fatalf("SendAgentRequest: %v", err)
	}
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{"op": "move", "id": "hero", "x": 120, "y": 40},
		},
	}))
	attr, ok := f.ed.LastAgentChangeForObject("hero")
	if !ok || attr.RequestID != req.ID || attr.RequestTitle != "Move the hero right" {
		t.Fatalf("LastAgentChangeForObject(hero) = (%+v, %v), want req %q", attr, ok, req.ID)
	}
}

// Each kind of edit reaches the editor's method for it.
func TestSceneEditOps(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	edit := func(edits ...map[string]any) {
		t.Helper()
		list := make([]any, len(edits))
		for i, e := range edits {
			list[i] = e
		}
		mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{"edits": list}))
	}

	edit(map[string]any{"op": "move", "id": "hero", "x": 10, "y": 20})
	edit(map[string]any{"op": "set", "id": "hero", "fields": map[string]any{"rotation": 0.5, "scaleX": 2, "scaleY": 2, "alpha": 0.5, "flipX": true, "originX": 8, "originY": 8}})
	edit(map[string]any{"op": "set", "id": "hero", "component": "body", "fields": map[string]any{"solid": false, "speed": 60}})
	edit(map[string]any{"op": "add", "object": map[string]any{"id": "torch", "x": 1, "y": 2}, "layer": "props"},
		map[string]any{"op": "reparent", "id": "torch", "parent": "hero"})
	hero, _, _ := f.ed.FindObject("hero")
	if hero.X != 10 || hero.Y != 20 || hero.Rotation != 0.5 || *hero.ScaleX != 2 || *hero.ScaleY != 2 || *hero.Alpha != 0.5 || !hero.FlipX || hero.OriginX != 8 {
		t.Fatalf("hero %+v", hero)
	}
	if body := hero.Components["body"].(map[string]any); body["solid"] != false || body["speed"] != 60.0 {
		t.Fatalf("body %+v", body)
	}
	if len(hero.Children) != 1 || hero.Children[0].ID != "torch" {
		t.Fatalf("children %+v", hero.Children)
	}
	edit(map[string]any{"op": "reparent", "id": "torch"}, map[string]any{"op": "move_to_layer", "id": "torch", "layer": "props"})
	edit(map[string]any{"op": "remove_component", "id": "hero", "component": "body"},
		map[string]any{"op": "add_component", "id": "hero", "component": "body", "fields": map[string]any{"speed": 5}})
	edit(map[string]any{"op": "set_property", "id": "hero", "key": "tag", "value": "player"},
		map[string]any{"op": "remove_property", "id": "hero", "key": "tag"})
	edit(map[string]any{"op": "set", "id": "hero", "fields": map[string]any{"id": "player", "sprite": nil}})
	edit(map[string]any{"op": "remove", "id": "torch"})
	player, _, _ := f.ed.FindObject("player")
	if player == nil || player.Sprite != nil || player.Components["body"].(map[string]any)["speed"] != 5.0 || player.Properties["tag"] != nil {
		t.Fatalf("player %+v", player)
	}
	if torch, _, _ := f.ed.FindObject("torch"); torch != nil {
		t.Fatalf("torch still there: %+v", torch)
	}
}

// A refused scene_edit names the file, the object and the field with a fix, applies none of the call's edits, adds no undo step and leaves the file on disk byte for byte unchanged.
func TestSceneEditRefusalChangesNothing(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	onDisk, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	inEditor, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	index, length := f.ed.History.Index(), f.ed.History.Len()

	cases := []struct {
		name  string
		edits []any
		want  []string
		field string
	}{
		{"unknown field", []any{
			map[string]any{"op": "move", "id": "hero", "x": 1, "y": 1},
			map[string]any{"op": "set", "id": "hero", "component": "body", "fields": map[string]any{"sped": 3}},
		}, []string{`scenes/room.scene.yaml: edit 2 (set) on object "hero" component "body" field "sped"`, "Nothing was changed."}, "sped"},
		{"unknown component", []any{
			map[string]any{"op": "add_component", "id": "hero", "component": "bodyy"},
		}, []string{`on object "hero" component "bodyy"`, "body"}, ""},
		{"no such node", []any{
			map[string]any{"op": "move", "id": "nobody", "x": 1, "y": 1},
		}, []string{`on object "nobody"`, "has no object", "scene_read"}, ""},
		{"unknown object field", []any{
			map[string]any{"op": "set", "id": "hero", "fields": map[string]any{"colour": "red"}},
		}, []string{`field "colour"`, "set one of id, name"}, "colour"},
		{"object field the format refuses", []any{
			map[string]any{"op": "add", "object": map[string]any{"id": "x", "colour": "red"}},
		}, []string{`edit 1 (add) on object "x" field "colour"`}, "colour"},
		{"component field of the wrong kind on add", []any{
			map[string]any{"op": "add", "object": map[string]any{"id": "x", "components": map[string]any{"body": map[string]any{"speed": "fast"}}}},
		}, []string{`on object "x" component "body" field "speed"`}, "speed"},
		{"duplicate id", []any{
			map[string]any{"op": "add", "object": map[string]any{"id": "slime"}},
			map[string]any{"op": "add", "object": map[string]any{"id": "hero"}},
		}, []string{"edit 2 (add)", `duplicate object id "hero"`}, ""},
		{"out of range", []any{
			map[string]any{"op": "set", "id": "hero", "component": "body", "fields": map[string]any{"speed": 9000}},
		}, []string{`field "speed"`}, "speed"},
		{"another scene", nil, []string{"scenes/other.scene.yaml", "open scenes/other.scene.yaml in the editor first"}, ""},
		{"unknown op", []any{map[string]any{"op": "explode", "id": "hero"}}, []string{`unknown op "explode"`}, ""},
		{"unknown op without an id", []any{map[string]any{"op": "bogus"}}, []string{`unknown op "bogus"`, "move_to_layer"}, ""},
		{"unknown op on a missing object", []any{map[string]any{"op": "bogus", "id": "nope"}}, []string{`unknown op "bogus"`}, ""},
		{"property out of float range", []any{
			map[string]any{"op": "set_property", "id": "hero", "key": "k", "value": json.RawMessage(`1e400`)},
		}, []string{`field "k"`, "finite number"}, "k"},
		{"component field out of float range", []any{
			map[string]any{"op": "set", "id": "hero", "component": "body", "fields": map[string]any{"speed": json.RawMessage(`1e400`)}},
		}, []string{`component "body" field "speed"`}, "speed"},
	}
	for _, c := range cases {
		args := map[string]any{"edits": c.edits}
		if c.edits == nil {
			args = map[string]any{"file": "scenes/other.scene.yaml", "edits": []any{map[string]any{"op": "remove", "id": "ghost"}}}
		}
		res := call(t, cs, editormcp.ToolSceneEdit, args)
		msg := text(res)
		if !res.IsError {
			t.Errorf("%s: applied: %s", c.name, msg)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%s: %q does not say %q", c.name, msg, w)
			}
		}
		var detail struct {
			Error editormcp.EditError `json:"error"`
		}
		structured(t, res, &detail)
		if detail.Error.File == "" || detail.Error.Problem == "" || detail.Error.Fix == "" || detail.Error.Field != c.field {
			t.Errorf("%s: structured error %+v, want file, problem, fix and field %q", c.name, detail.Error, c.field)
		}
		now, err := f.ed.SaveYAML()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(now, inEditor) || f.ed.History.Index() != index || f.ed.History.Len() != length || f.ed.History.CanRedo() {
			t.Fatalf("%s: the refused call changed the editor (history %d/%d, was %d/%d)", c.name, f.ed.History.Index(), f.ed.History.Len(), index, length)
		}
	}
	after, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, onDisk) {
		t.Fatal("a refused edit changed the file on disk")
	}
}

// selection_get reports the selected object with its YAML lines as scene_read numbers them, and the editor's state around it.
func TestSelectionGet(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	out := mustOK(t, call(t, cs, editormcp.ToolSelectionGet, map[string]any{}))
	if !strings.Contains(out, "Scene scenes/room.scene.yaml, active layer actors, tool Select, 2D view.") || !strings.Contains(out, "No object is selected.") {
		t.Fatalf("nothing selected:\n%s", out)
	}
	f.ed.SelectObject("hero")
	f.ed.SelectFile("data/items.yaml")
	res := call(t, cs, editormcp.ToolSelectionGet, map[string]any{})
	out = mustOK(t, res)
	var sel editormcp.Selection
	structured(t, res, &sel)
	if sel.Object == nil || sel.Object.ID != "hero" || sel.Object.Layer != "actors" || sel.Object.Line != 9 || !slices.Equal(sel.Object.Components, []string{"body"}) || sel.File != "data/items.yaml" {
		t.Fatalf("selection %+v", sel)
	}
	if sel.Object.Edit != (editormcp.ObjectEdit{ID: "hero", Origin: "local"}) {
		t.Fatalf("selection edit metadata %+v", sel.Object.Edit)
	}
	read := mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{}))
	if !strings.Contains(read, sel.Object.YAML) || !strings.HasPrefix(sel.Object.YAML, "     9\t      - id: hero\n") || strings.Contains(sel.Object.YAML, "- name: props") {
		t.Fatalf("object YAML:\n%s\nnot the lines of:\n%s", sel.Object.YAML, read)
	}
	if !strings.Contains(out, "Selected object hero on layer actors, at line 9:") {
		t.Fatalf("selection text:\n%s", out)
	}
}

// viewport_image returns a PNG of the viewport at its size times scale, which shows a placed object.
func TestViewportImage(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	f.ed.SetCanvasSize(320, 180)
	res := call(t, cs, editormcp.ToolViewportImage, map[string]any{})
	mustOK(t, res)
	img := pngOf(t, res)
	if b := img.Bounds(); b.Dx() != 320 || b.Dy() != 180 {
		t.Fatalf("image is %v, want 320×180", b)
	}
	cx, cy := 208, 108
	if _, _, _, a := img.At(cx, cy).RGBA(); a != 0 {
		t.Fatalf("pixel at (%d, %d) is drawn before anything is there", cx, cy)
	}
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{"edits": []any{
		map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_1", "x": 200, "y": 100, "sprite": map[string]any{"atlas": "actors", "frame": "slime"}}},
	}}))
	res = call(t, cs, editormcp.ToolViewportImage, map[string]any{"scale": 2})
	img = pngOf(t, res)
	var info editormcp.ViewportImage
	structured(t, res, &info)
	if b := img.Bounds(); b.Dx() != 640 || b.Dy() != 360 || info.Width != 640 || info.ViewMode != "2D" {
		t.Fatalf("scaled image is %v (%+v), want 640×360", b, info)
	}
	r, g, b, a := img.At(2*cx, 2*cy).RGBA()
	if r>>8 != uint32(heroColor.R) || g>>8 != uint32(heroColor.G) || b>>8 != uint32(heroColor.B) || a>>8 != 0xff {
		t.Fatalf("pixel at the placed slime is %v, want its sprite's %v", img.At(2*cx, 2*cy), heroColor)
	}
	if res := call(t, cs, editormcp.ToolViewportImage, map[string]any{"scale": 9}); !res.IsError {
		t.Fatal("scale 9 accepted")
	}
}

// game_check returns the problems the Problems panel lists, and says when the open scene has edits the check did not see.
func TestGameCheckMatchesTheProblemsPanel(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolGameCheck, map[string]any{})
	out := mustOK(t, res)
	var got editormcp.Check
	structured(t, res, &got)
	panel := f.ed.Problems()
	if len(panel) == 0 || len(got.Problems) != len(panel) {
		t.Fatalf("game_check found %+v, the Problems panel %+v", got.Problems, panel)
	}
	for i, p := range panel {
		if g := got.Problems[i]; g.File != p.File || g.Line != p.Line || g.Column != p.Column || g.Message != p.Message || !strings.Contains(out, p.Location()+": "+p.Message) {
			t.Errorf("problem %d: %+v, panel %+v", i, g, p)
		}
	}
	if !slices.ContainsFunc(got.Problems, func(p editormcp.Problem) bool {
		return p.File == "scenes/other.scene.yaml" && strings.Contains(p.Message, "ghost")
	}) {
		t.Errorf("no problem about the ghost frame: %+v", got.Problems)
	}
	if got.Unsaved != "" {
		t.Errorf("unsaved %q before any edit", got.Unsaved)
	}
	if err := f.ed.MoveObject("hero", 1, 1); err != nil {
		t.Fatal(err)
	}
	res = call(t, cs, editormcp.ToolGameCheck, map[string]any{})
	structured(t, res, &got)
	if got.Unsaved != "scenes/room.scene.yaml" || !strings.Contains(text(res), "has edits that are not saved") {
		t.Fatalf("after an edit: %+v %s", got, text(res))
	}

	bare := newEditor(t)
	s2 := editormcp.New(bare, editormcp.Options{})
	t.Cleanup(func() { _ = s2.Close() })
	if res := call(t, connect(t, s2, nil), editormcp.ToolGameCheck, map[string]any{}); !res.IsError || !strings.Contains(text(res), "no project is open") {
		t.Fatalf("without a project: %s", text(res))
	}
}

// A subscribed client hears of each change to the open scene and the selection, with who made it.
func TestChangeStream(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	got := make(chan *mcp.ResourceUpdatedNotificationParams, 16)
	cs := connect(t, s, &mcp.ClientOptions{ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
		got <- req.Params
	}})
	for _, uri := range []string{editormcp.SceneURI, editormcp.SelectionURI} {
		if err := cs.Subscribe(context.Background(), &mcp.SubscribeParams{URI: uri}); err != nil {
			t.Fatal(err)
		}
	}
	next := func(uri, by string) {
		t.Helper()
		n := <-got
		if n.URI != uri || n.Meta[editormcp.MetaBy] != by || n.Meta[editormcp.MetaRevision] == nil {
			t.Fatalf("notice %+v, want %s by %s", n, uri, by)
		}
	}

	// The person moves the hero: the scene changed.
	if err := f.ed.MoveObject("hero", 5, 5); err != nil {
		t.Fatal(err)
	}
	next(editormcp.SceneURI, "person")
	// The agent places an object, which the editor selects.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{"edits": []any{map[string]any{"op": "add", "object": map[string]any{"id": "slime_1"}}}}))
	seen := map[string]bool{}
	for range 2 {
		n := <-got
		if n.Meta[editormcp.MetaBy] != "agent" {
			t.Fatalf("notice %+v, want by agent", n)
		}
		seen[n.URI] = true
	}
	if !seen[editormcp.SceneURI] || !seen[editormcp.SelectionURI] {
		t.Fatalf("notices %v, want the scene and the selection", seen)
	}
	// Selecting without editing changes the selection only.
	f.ed.SelectObject("hero")
	next(editormcp.SelectionURI, "person")

	read, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.SceneURI})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read.Contents[0].Text, "id: slime_1") {
		t.Fatalf("scene resource:\n%s", read.Contents[0].Text)
	}
	read, err = cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.SelectionURI})
	if err != nil {
		t.Fatal(err)
	}
	var sel editormcp.Selection
	if err := json.Unmarshal([]byte(read.Contents[0].Text), &sel); err != nil || sel.Object == nil || sel.Object.ID != "hero" {
		t.Fatalf("selection resource %s: %v", read.Contents[0].Text, err)
	}
}

// A session that ends without unsubscribing no longer counts as subscribed, so the editor stops encoding the scene and the selection for it on every change.
func TestEndedSessionDropsItsSubscriptions(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	for range 2 {
		st, ct := mcp.NewInMemoryTransports()
		ss, err := s.Connect(context.Background(), st)
		if err != nil {
			t.Fatal(err)
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, &mcp.ClientOptions{ResourceUpdatedHandler: func(context.Context, *mcp.ResourceUpdatedNotificationRequest) {}}).Connect(context.Background(), ct, &mcp.ClientSessionOptions{ProtocolVersion: legacyProtocol})
		if err != nil {
			t.Fatal(err)
		}
		for _, uri := range []string{editormcp.SceneURI, editormcp.SelectionURI} {
			if err := cs.Subscribe(context.Background(), &mcp.SubscribeParams{URI: uri}); err != nil {
				t.Fatal(err)
			}
		}
		if scene, selection := s.Subscribed(); !scene || !selection {
			t.Fatalf("subscribed scene %v, selection %v, want both", scene, selection)
		}
		_ = cs.Close()
		_ = ss.Wait()
		if scene, selection := s.Subscribed(); scene || selection {
			t.Fatalf("after the session ended: subscribed scene %v, selection %v, want neither", scene, selection)
		}
	}
}

// Tools other parts of the editor add join the list, and clients hear the list changed; the server refuses a duplicate or malformed one.
func TestAddTool(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{OnPermissionAsk: allowAsk})
	t.Cleanup(func() { _ = s.Close() })
	changed := make(chan struct{}, 4)
	cs := connect(t, s, &mcp.ClientOptions{ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) { changed <- struct{}{} }})

	err := s.AddTool(editormcp.Tool{
		Name:   "custom_action",
		Action: editormcp.ActionRun,
		Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
			var name string
			if err := c.Editor(ctx, func(ed *editor.Editor) error {
				name = ed.Doc.Name
				return nil
			}); err != nil {
				return nil, err
			}
			return &editormcp.Result{Text: "played " + name}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-changed
	if out := mustOK(t, call(t, cs, "custom_action", nil)); out != "played Room" {
		t.Fatalf("custom_action: %q", out)
	}
	for _, bad := range []editormcp.Tool{
		{Name: "custom_action", Action: editormcp.ActionRead, Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) { return nil, nil }},
		{Name: "bad name", Action: editormcp.ActionRead, Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) { return nil, nil }},
		{Name: "no_handler", Action: editormcp.ActionRead},
		{Name: "no_action", Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) { return nil, nil }},
		{Name: "bad_action", Action: editormcp.Action("exec"), Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) { return nil, nil }},
		{Name: "bad_schema", Action: editormcp.ActionRead, InputSchema: json.RawMessage(`{"type":"array"}`), Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) { return nil, nil }},
	} {
		if err := s.AddTool(bad); err == nil {
			t.Errorf("AddTool(%q) accepted", bad.Name)
		}
	}
	s.RemoveTool("custom_action")
	<-changed
	if res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "custom_action"}); err == nil && !res.IsError {
		t.Fatal("removed tool still answers")
	}
	if names := toolNames(s.Tools()); slices.Contains(names, "custom_action") {
		t.Fatalf("tools %v", names)
	}
}

func toolNames(tools []editormcp.Tool) []string {
	var out []string
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

// A call that panics answers with an error and leaves the server serving.
func TestPanickingToolIsAnError(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	if err := s.AddTool(editormcp.Tool{Name: "boom", Action: editormcp.ActionRead, Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
		return nil, c.Editor(ctx, func(*editor.Editor) error { panic("boom") })
	}}); err != nil {
		t.Fatal(err)
	}
	if res := call(t, cs, "boom", nil); !res.IsError || !strings.Contains(text(res), "boom") {
		t.Fatalf("boom: %s", text(res))
	}
	mustOK(t, call(t, cs, editormcp.ToolSelectionGet, nil))
}

// Server.Editor hands its function to Options.Run and waits for it; a call whose context ends before the editor runs it never runs.
func TestEditorRunsOnTheEditorsGoroutine(t *testing.T) {
	f := openFixture(t)
	queue := make(chan func(), 4)
	s := editormcp.New(f.ed, editormcp.Options{Run: func(fn func()) { queue <- fn }})
	t.Cleanup(func() { _ = s.Close() })

	done := make(chan error, 1)
	go func() {
		done <- s.Editor(context.Background(), func(ed *editor.Editor) error { return ed.MoveObject("hero", 9, 9) })
	}()
	(<-queue)()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if hero, _, _ := f.ed.FindObject("hero"); hero.X != 9 {
		t.Fatalf("hero at %v", hero.X)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ran := false
	go func() {
		done <- s.Editor(ctx, func(*editor.Editor) error { ran = true; return nil })
	}()
	fn := <-queue
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call: %v", err)
	}
	fn()
	if ran {
		t.Fatal("a cancelled call ran")
	}
}

// #921: selection_get reports the editor's active language so connected agents answer in the same language.
func TestSelectionGetReportsEditorLanguage(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolSelectionGet, nil)
	mustOK(t, res)
	var sel editormcp.Selection
	structured(t, res, &sel)
	if sel.Language != "en" {
		t.Fatalf("default selection language = %q, want en", sel.Language)
	}

	if err := f.ed.SetLanguage(editor.LanguageChinese); err != nil {
		t.Fatal(err)
	}
	res = call(t, cs, editormcp.ToolSelectionGet, nil)
	mustOK(t, res)
	sel = editormcp.Selection{}
	structured(t, res, &sel)
	if sel.Language != "zh" {
		t.Fatalf("Chinese selection language = %q, want zh", sel.Language)
	}
}

// #1547: With Options.Run == nil, Server.Editor and session notifications share the Editor's
// single headless owner, so concurrent MCP calls serialize with Editor.RunHeadless and Close
// inside RunHeadless never deadlocks.
func TestHeadlessServerSerializesWithRunHeadlessAndClosesCleanly(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	cs := connect(t, s, nil)

	errCh := make(chan error, 1)
	go func() {
		for i := 0; i < 15; i++ {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: editormcp.ToolSelectionGet})
			if err != nil {
				errCh <- err
				return
			}
			if res.IsError {
				errCh <- errors.New(text(res))
				return
			}
		}
		errCh <- nil
	}()

	for i := 0; i < 15; i++ {
		f.ed.RunHeadless(func() {
			_ = f.ed.MoveObject("hero", float32(10+i), float32(20+i))
		})
	}
	if err := <-errCh; err != nil {
		t.Fatalf("concurrent MCP calls: %v", err)
	}

	// Closing the server inside RunHeadless must cancel any waiting notifyEditor without deadlocking.
	done := make(chan error, 1)
	go func() {
		f.ed.RunHeadless(func() {
			done <- s.Close()
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close inside RunHeadless: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Server.Close inside RunHeadless deadlocked")
	}
}
