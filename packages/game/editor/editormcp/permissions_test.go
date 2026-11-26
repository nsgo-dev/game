package editormcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

func TestMCPPermissionsProfilesAndDenyNeverCallsTool(t *testing.T) {
	f := openFixture(t)

	var (
		deleteCalls atomic.Int32
		shellCalls  atomic.Int32
		codeCalls   atomic.Int32
		configCalls atomic.Int32
		createCalls atomic.Int32
		decisions   []editor.PermissionEvaluation
	)

	s := editormcp.New(f.ed, editormcp.Options{
		OnPermissionDecision: func(eval editor.PermissionEvaluation) {
			decisions = append(decisions, eval)
		},
	})
	t.Cleanup(func() { _ = s.Close() })

	// Register representative mutating/running tools covering delete, shell run, code edit, config edit, and file creation.
	mustAdd := func(tool editormcp.Tool) {
		t.Helper()
		if err := s.AddTool(tool); err != nil {
			t.Fatalf("AddTool(%s): %v", tool.Name, err)
		}
	}
	mustAdd(editormcp.Tool{
		Name:   "direct_delete_file",
		Action: editormcp.ActionDelete,
		Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
			deleteCalls.Add(1)
			return &editormcp.Result{Text: "deleted"}, nil
		},
	})
	mustAdd(editormcp.Tool{
		Name:   "shell_run",
		Action: editormcp.ActionRun,
		Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
			shellCalls.Add(1)
			return &editormcp.Result{Text: "ran shell"}, nil
		},
	})
	mustAdd(editormcp.Tool{
		Name:   "code_edit",
		Action: editormcp.ActionCode,
		Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
			codeCalls.Add(1)
			return &editormcp.Result{Text: "edited code"}, nil
		},
	})
	mustAdd(editormcp.Tool{
		Name:   "config_edit",
		Action: editormcp.ActionEdit,
		Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
			configCalls.Add(1)
			return &editormcp.Result{Text: "edited config"}, nil
		},
	})
	mustAdd(editormcp.Tool{
		Name:   "file_create",
		Action: editormcp.ActionCreate,
		Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
			createCalls.Add(1)
			return &editormcp.Result{Text: "created"}, nil
		},
	})

	cs := connect(t, s, nil)

	// --- 1. Balanced profile (default) ---
	// Scene edit and game_check are Allowed immediately.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "move", "id": "hero", "x": 72, "y": 40}},
	}))
	if ev, ok := s.LastPermissionDecision(); !ok || ev.Decision != editor.PermissionAllow || ev.RuleNumber != 1 {
		t.Fatalf("balanced scene_edit decision = %+v (%v), want allow by rule 1", ev, ok)
	}
	mustOK(t, call(t, cs, editormcp.ToolGameCheck, map[string]any{}))
	if ev, ok := s.LastPermissionDecision(); !ok || ev.Decision != editor.PermissionAllow || ev.RuleNumber != 2 {
		t.Fatalf("balanced game_check decision = %+v (%v), want allow by rule 2", ev, ok)
	}

	// game.yaml edit, **/*.go code edit, and file creation Ask (and do not invoke handlers when unanswered).
	cfgRes := call(t, cs, "config_edit", map[string]any{"file": "game.yaml"})
	if !cfgRes.IsError || !strings.Contains(text(cfgRes), "permission required") || !strings.Contains(text(cfgRes), "rule 3 of 7") {
		t.Fatalf("balanced game.yaml = err:%v text:%q", cfgRes.IsError, text(cfgRes))
	}
	codeRes := call(t, cs, "code_edit", map[string]any{"file": "scripts/slime.go"})
	if !codeRes.IsError || !strings.Contains(text(codeRes), "permission required") || !strings.Contains(text(codeRes), "rule 4 of 7") {
		t.Fatalf("balanced code_edit = err:%v text:%q", codeRes.IsError, text(codeRes))
	}
	createRes := call(t, cs, "file_create", map[string]any{"file": "scenes/new.scene.yaml"})
	if !createRes.IsError || !strings.Contains(text(createRes), "permission required") || !strings.Contains(text(createRes), "rule 5 of 7") {
		t.Fatalf("balanced file_create = err:%v text:%q", createRes.IsError, text(createRes))
	}
	if configCalls.Load() != 0 || codeCalls.Load() != 0 || createCalls.Load() != 0 {
		t.Fatalf("Ask invoked handlers: config=%d code=%d create=%d", configCalls.Load(), codeCalls.Load(), createCalls.Load())
	}

	// Delete and shell commands are Denied; handlers NEVER run, error names rule & profile, and blocked step is recorded.
	delRes := call(t, cs, "direct_delete_file", map[string]any{"file": "prefabs/slime_old.prefab.yaml"})
	if !delRes.IsError || !strings.Contains(text(delRes), "permission denied") || !strings.Contains(text(delRes), "rule 6 of 7") || !strings.Contains(text(delRes), "Balanced") {
		t.Fatalf("balanced delete = err:%v text:%q", delRes.IsError, text(delRes))
	}
	shellRes := call(t, cs, "shell_run", map[string]any{"command": "go generate ./assets"})
	if !shellRes.IsError || !strings.Contains(text(shellRes), "permission denied") || !strings.Contains(text(shellRes), "rule 7 of 7") || !strings.Contains(text(shellRes), "Settings › Permissions · rule 7 of 7") {
		t.Fatalf("balanced shell = err:%v text:%q", shellRes.IsError, text(shellRes))
	}
	if deleteCalls.Load() != 0 || shellCalls.Load() != 0 {
		t.Fatalf("Deny invoked handlers: delete=%d shell=%d", deleteCalls.Load(), shellCalls.Load())
	}
	// Structured error payload carries ruleIndex and link.
	var payload struct {
		Error editormcp.PermissionError `json:"error"`
	}
	structured(t, shellRes, &payload)
	if payload.Error.Decision != editor.PermissionDeny || payload.Error.RuleIndex != 6 || payload.Error.RuleNumber != 7 || payload.Error.Link != "Settings › Permissions · rule 7 of 7" {
		t.Fatalf("structured shell PermissionError = %+v", payload.Error)
	}
	if blocked := f.ed.BlockedPermissions(); len(blocked) < 2 {
		t.Fatalf("expected blocked steps recorded on Editor, got %d", len(blocked))
	}

	// --- 2. Cautious profile ---
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatal(err)
	}
	// Reads still succeed under Cautious.
	mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{}))
	// Scene edit and game_check now Ask.
	cautiousEdit := call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "move", "id": "hero", "x": 80, "y": 40}},
	})
	if !cautiousEdit.IsError || !strings.Contains(text(cautiousEdit), "Cautious") || !strings.Contains(text(cautiousEdit), "rule 1 of 7") {
		t.Fatalf("cautious scene_edit = err:%v text:%q", cautiousEdit.IsError, text(cautiousEdit))
	}
	cautiousCheck := call(t, cs, editormcp.ToolGameCheck, map[string]any{})
	if !cautiousCheck.IsError || !strings.Contains(text(cautiousCheck), "Cautious") || !strings.Contains(text(cautiousCheck), "rule 2 of 7") {
		t.Fatalf("cautious game_check = err:%v text:%q", cautiousCheck.IsError, text(cautiousCheck))
	}

	// --- 3. Autonomous profile ---
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}
	mustOK(t, call(t, cs, "config_edit", map[string]any{"file": "game.yaml"}))
	mustOK(t, call(t, cs, "code_edit", map[string]any{"file": "scripts/slime.go"}))
	mustOK(t, call(t, cs, "file_create", map[string]any{"file": "scenes/new.scene.yaml"}))
	if configCalls.Load() != 1 || codeCalls.Load() != 1 || createCalls.Load() != 1 {
		t.Fatalf("Autonomous should allow config/code/create: got %d/%d/%d", configCalls.Load(), codeCalls.Load(), createCalls.Load())
	}
	// Delete & shell still denied by rules 6 & 7 unless explicitly relaxed.
	delAuto := call(t, cs, "game_delete_file", map[string]any{"file": "prefabs/old.prefab.yaml"})
	if !delAuto.IsError || deleteCalls.Load() != 0 {
		t.Fatalf("Autonomous delete should still be denied: err=%v calls=%d", delAuto.IsError, deleteCalls.Load())
	}
	if len(decisions) == 0 {
		t.Fatal("OnPermissionDecision callback was not called")
	}
}

func TestMCPPermissionsFirstMatchWinsOrdering(t *testing.T) {
	f := openFixture(t)
	if err := f.ed.SetPermissions(editor.PermissionsFile{
		Version: editor.PermissionsVersion,
		Profile: editor.PermissionProfileBalanced,
		Rules: []editor.PermissionRule{
			{Action: editor.PermissionActionEdit, Patterns: []string{"scenes/**"}, Decision: editor.PermissionAllow},
			{Action: editor.PermissionActionEdit, Patterns: []string{"**"}, Decision: editor.PermissionAsk},
		},
	}); err != nil {
		t.Fatal(err)
	}

	var dataCalls atomic.Int32
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	if err := s.AddTool(editormcp.Tool{
		Name:   "data_edit",
		Action: editormcp.ActionEdit,
		Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
			dataCalls.Add(1)
			return &editormcp.Result{Text: "ok"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, s, nil)

	// Allow on scenes/** before Ask on ** lets scene_edit through.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "move", "id": "hero", "x": 96, "y": 40}},
	}))
	if ev, ok := s.LastPermissionDecision(); !ok || ev.Decision != editor.PermissionAllow || ev.RuleIndex != 0 {
		t.Fatalf("scene_edit decision = %+v (%v), want allow by rule 0", ev, ok)
	}

	// Non-scene edit falls through to rule 2 (Ask on **) and does not run.
	res := call(t, cs, "data_edit", map[string]any{"file": "data/items.yaml"})
	if !res.IsError || dataCalls.Load() != 0 {
		t.Fatalf("data_edit should ask without running: err=%v calls=%d text=%q", res.IsError, dataCalls.Load(), text(res))
	}
}

func TestMCPPermissionsAskOnceAlwaysAndUndo(t *testing.T) {
	f := openFixture(t)
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatal(err)
	}

	var (
		askCount atomic.Int32
		nextAns  atomic.Int32
	)
	nextAns.Store(int32(editormcp.PermissionAnswerAllowOnce))

	s := editormcp.New(f.ed, editormcp.Options{
		OnPermissionAsk: func(ctx context.Context, eval editor.PermissionEvaluation) (editormcp.PermissionPromptAnswer, error) {
			askCount.Add(1)
			return editormcp.PermissionPromptAnswer(nextAns.Load()), nil
		},
	})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. AllowOnce runs game_check once without adding a rule.
	mustOK(t, call(t, cs, editormcp.ToolGameCheck, map[string]any{}))
	if askCount.Load() != 1 || f.ed.Permissions().IsModified() {
		t.Fatalf("AllowOnce: askCount=%d modified=%v", askCount.Load(), f.ed.Permissions().IsModified())
	}

	// 2. AllowAlways inserts an Allow rule, saves permissions.yaml, shows toast, and skips asking on the next call.
	nextAns.Store(int32(editormcp.PermissionAnswerAllowAlways))
	mustOK(t, call(t, cs, editormcp.ToolGameCheck, map[string]any{}))
	if askCount.Load() != 2 {
		t.Fatalf("AllowAlways askCount = %d, want 2", askCount.Load())
	}
	toast, ok := f.ed.PermissionToast()
	if !ok || toast.Message != "Always permission rule saved" {
		t.Fatalf("expected Always saved toast, got %+v (%v)", toast, ok)
	}
	// Verify permissions.yaml on disk parses back cleanly with the new Allow rule.
	diskData, err := os.ReadFile(filepath.Join(f.dir, editor.PermissionsFileName))
	if err != nil {
		t.Fatalf("read permissions.yaml: %v", err)
	}
	parsed, err := editor.ParsePermissions(diskData)
	if err != nil {
		t.Fatalf("parse saved permissions.yaml: %v", err)
	}
	if len(parsed.Rules) != 8 {
		t.Fatalf("saved rules count = %d, want 8", len(parsed.Rules))
	}

	// Next game_check call runs immediately without asking!
	mustOK(t, call(t, cs, editormcp.ToolGameCheck, map[string]any{}))
	if askCount.Load() != 2 {
		t.Fatalf("after Always, game_check should not ask again: askCount=%d", askCount.Load())
	}

	// 3. Undo reverts the saved rule, so game_check asks again (and Deny stops it).
	if err := f.ed.UndoPermissionToast(); err != nil {
		t.Fatalf("UndoPermissionToast: %v", err)
	}
	nextAns.Store(int32(editormcp.PermissionAnswerDeny))
	denied := call(t, cs, editormcp.ToolGameCheck, map[string]any{})
	if !denied.IsError || !strings.Contains(text(denied), "Denied by you") {
		t.Fatalf("after Undo + Deny: err=%v text=%q", denied.IsError, text(denied))
	}
	if askCount.Load() != 3 {
		t.Fatalf("after Undo, game_check should ask again: askCount=%d", askCount.Load())
	}
	// Suppress unused json import check if needed.
	_ = json.RawMessage{}
}

func TestMCPPermissionsSecurityBypassesAndRace(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	var customCalls atomic.Int32
	if err := s.AddTool(editormcp.Tool{
		Name:   "file_writer",
		Action: editormcp.ActionEdit,
		Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) {
			customCalls.Add(1)
			return &editormcp.Result{Text: "written"}, nil
		},
	}); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	// Empty/invalid action must be refused by AddTool (fail-closed).
	if err := s.AddTool(editormcp.Tool{
		Name: "missing_action",
		Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) {
			return &editormcp.Result{Text: "bad"}, nil
		},
	}); err == nil {
		t.Fatal("expected AddTool with empty Action to be rejected")
	}

	cs := connect(t, s, nil)

	// 1. Path traversal, leading slash, and symlink escapes MUST be denied before handler executes.
	outsideFile := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outsideFile, []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "game.yaml"), filepath.Join(f.dir, "scenes", "link_game.scene.yaml")); err == nil {
		// In Balanced profile, scenes/** is Allow, game.yaml is Ask -> symlink inside scenes/ pointing to game.yaml MUST trigger Ask (not Allow)!
		res := call(t, cs, "file_writer", map[string]any{"file": "scenes/link_game.scene.yaml"})
		if !res.IsError {
			t.Fatal("symlink scenes/link_game.scene.yaml -> ../game.yaml bypassed game.yaml Ask rule")
		}
	}
	if err := os.Symlink(outsideFile, filepath.Join(f.dir, "scenes", "escape_link.scene.yaml")); err == nil {
		res := call(t, cs, "file_writer", map[string]any{"file": "scenes/escape_link.scene.yaml"})
		if !res.IsError || !strings.Contains(text(res), "permission denied") {
			t.Fatalf("outside symlink escape was not denied: err=%v text=%q", res.IsError, text(res))
		}
	}
	for _, bad := range []string{"../outside.yaml", "scenes/../../outside.yaml", "/game.yaml"} {
		res := call(t, cs, "file_writer", map[string]any{"file": bad})
		if !res.IsError || !strings.Contains(text(res), "permission denied") {
			t.Fatalf("escaping path %q was not denied: err=%v text=%q", bad, res.IsError, text(res))
		}
	}
	if customCalls.Load() != 0 {
		t.Fatalf("file_writer ran %d times on blocked/asking paths, want 0", customCalls.Load())
	}

	// 2. refactor_rename MUST check destination `to` (moving to ./game.yaml asks, moving to ../escape.yaml denies).
	resToGame := call(t, cs, editormcp.ToolRefactorRename, map[string]any{
		"from": "scenes/room.scene.yaml",
		"to":   "./game.yaml",
	})
	if !resToGame.IsError || !strings.Contains(text(resToGame), "permission required") {
		t.Fatalf("refactor_rename to ./game.yaml should require permission, got err=%v text=%q", resToGame.IsError, text(resToGame))
	}
	resToEscape := call(t, cs, editormcp.ToolRefactorRename, map[string]any{
		"from": "scenes/room.scene.yaml",
		"to":   "../escape.yaml",
	})
	if !resToEscape.IsError || !strings.Contains(text(resToEscape), "permission denied") {
		t.Fatalf("refactor_rename to ../escape.yaml should be denied, got err=%v text=%q", resToEscape.IsError, text(resToEscape))
	}

	// 3. merge_finish MUST NOT be silently allowed as `game check` under Balanced profile.
	resMerge := call(t, cs, editormcp.ToolMergeFinish, map[string]any{})
	if !resMerge.IsError || !strings.Contains(text(resMerge), "permission required") {
		t.Fatalf("merge_finish under Balanced should require permission (git merge), got err=%v text=%q", resMerge.IsError, text(resMerge))
	}

	// 4. Newly landed #1194/#1195/#1189 tools obey scene edit rules (allowed in Balanced, denied when scenes/** is denied).
	if err := f.ed.SetPermissionRuleDecision(0, editor.PermissionDeny); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{editormcp.ToolObjectSetField, map[string]any{"id": "hero", "field": "x", "value": 10}},
		{editormcp.ToolObjectDuplicate, map[string]any{"id": "hero"}},
		{editormcp.ToolTilePaint, map[string]any{"cells": []any{map[string]any{"col": 0, "row": 0}}, "tile": 1}},
	} {
		res := call(t, cs, tc.tool, tc.args)
		if !res.IsError || !strings.Contains(text(res), "permission denied") {
			t.Fatalf("%s with scenes/** denied: err=%v text=%q", tc.tool, res.IsError, text(res))
		}
	}
	// Restore rule 0 to Allow.
	if err := f.ed.SetPermissionRuleDecision(0, editor.PermissionAllow); err != nil {
		t.Fatal(err)
	}

	// 5. Corrupt permissions.yaml fails closed on scene_edit.
	if err := os.WriteFile(filepath.Join(f.dir, editor.PermissionsFileName), []byte("version: 1\nprofile: balanced\nbogus: true\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = f.ed.LoadProjectPermissions(filepath.Join(f.dir, editor.PermissionsFileName))
	resCorrupt := call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "move", "id": "hero", "x": 99, "y": 99}},
	})
	if !resCorrupt.IsError || !strings.Contains(text(resCorrupt), "permission denied") {
		t.Fatalf("corrupt permissions.yaml must deny scene_edit, got err=%v text=%q", resCorrupt.IsError, text(resCorrupt))
	}
	// Restore clean Balanced rules for concurrency test.
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileBalanced); err != nil {
		t.Fatal(err)
	}

	// 6. LastPermissionDecision returns a deep copy and concurrent calls pass -race cleanly.
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range 5 {
				if id%2 == 0 {
					_ = call(t, cs, editormcp.ToolGameCheck, map[string]any{})
				} else {
					_ = call(t, cs, "file_writer", map[string]any{"file": "../escape.yaml"})
				}
				if ev, ok := s.LastPermissionDecision(); ok && ev.Rule != nil && len(ev.Rule.Patterns) > 0 {
					ev.Rule.Patterns[0] = "mutated-copy"
				}
			}
		}(i)
	}
	wg.Wait()
}
