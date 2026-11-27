package editormcp_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

// #1015 Acceptance 1: An agent attempting to write the permissions file (and Tools, Models, Usage)
// through EVERY mutating/deleting tool gets a You-only refusal every time — even under Autonomous profile.
func TestAgentCannotWriteYouOnlyFilesThroughAnyTool(t *testing.T) {
	f := openFixture(t)
	// Switch to the loosest profile (Autonomous) and enable game_delete_file to prove no profile or tool switch allows writing/deleting You-only files.
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.SetMCPToolEnabled(editormcp.ToolGameDeleteFile, true); err != nil {
		t.Fatal(err)
	}
	permPath := editor.PermissionsFilePath(f.dir)
	origPermBytes, err := os.ReadFile(permPath)
	if err != nil {
		t.Fatalf("read permissions.yaml: %v", err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	// Also register custom edit and delete tools to verify Server.call enforces the You-only rule globally.
	customInvoked := 0
	if err := s.AddTool(editormcp.Tool{
		Name:   "custom_write_file",
		Title:  "Custom write",
		Action: editormcp.ActionEdit,
		Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) {
			customInvoked++
			return &editormcp.Result{Text: "wrote"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTool(editormcp.Tool{
		Name:   "custom_delete_file",
		Title:  "Custom delete",
		Action: editormcp.ActionDelete,
		Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) {
			customInvoked++
			return &editormcp.Result{Text: "deleted"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	cs := connect(t, s, nil)

	youOnlyTargets := []string{
		"permissions.yaml",
		".nsgo/permissions.yaml",
		"tools.yaml",
		".nsgo/tools.yaml",
		"models.yaml",
		".nsgo/models.yaml",
		"usage.yaml",
		".nsgo/usage.yaml",
		"automations.yaml",
		".nsgo/automations-state.yaml",
	}

	// Attempt to write permissions.yaml through every write/edit/rename/delete tool on the server.
	attempts := []struct {
		tool string
		args map[string]any
	}{
		{editormcp.ToolSceneEdit, map[string]any{"file": "permissions.yaml", "edits": []any{map[string]any{"op": "move", "id": "hero", "x": 1, "y": 2}}}},
		{editormcp.ToolStatesWrite, map[string]any{"file": "permissions.yaml", "yaml": "version: 1\n"}},
		{editormcp.ToolCutsceneWrite, map[string]any{"file": "permissions.yaml", "yaml": "version: 1\n"}},
		{editormcp.ToolTilesetEdit, map[string]any{"file": "permissions.yaml", "ops": []any{map[string]any{"op": "set_tile", "id": 1}}}},
		{editormcp.ToolPlaytestDefine, map[string]any{"file": "permissions.yaml", "metric": map[string]any{"name": "x", "measure": "deaths"}}},
		{editormcp.ToolRefactorRename, map[string]any{"from": "permissions.yaml", "to": "scenes/p.yaml", "apply": true}},
		{editormcp.ToolRefactorRename, map[string]any{"from": "scenes/room.scene.yaml", "to": "permissions.yaml", "apply": true}},
		{editormcp.ToolGameDeleteFile, map[string]any{"file": "permissions.yaml", "reason": "cleanup"}},
		{editormcp.ToolGameDeleteFile, map[string]any{"file": ".nsgo/permissions.yaml", "reason": "cleanup"}},
		{editormcp.ToolMergeResolve, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolMergeFinish, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolMergeAbort, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolTilePaint, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolTileAutotile, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolTileErase, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolObjectSetField, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolObjectPasteYAML, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolObjectDuplicate, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolObjectGroup, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolObjectMoveUnder, map[string]any{"file": "permissions.yaml"}},
		{editormcp.ToolObjectVisibility, map[string]any{"file": "permissions.yaml"}},
		{"custom_write_file", map[string]any{"path": "permissions.yaml", "content": "hack"}},
		{"custom_delete_file", map[string]any{"file": "permissions.yaml"}},
	}

	for _, a := range attempts {
		res := call(t, cs, a.tool, a.args)
		if !res.IsError || !strings.Contains(text(res), "You only") {
			t.Errorf("%s targeting permissions.yaml: got error=%v text=%q, want You-only refusal", a.tool, res.IsError, text(res))
		}
	}

	// Verify all four You-only domains (permissions, tools, models, usage) are refused via custom_write_file and game_delete_file.
	for _, target := range youOnlyTargets {
		for _, tool := range []string{"custom_write_file", editormcp.ToolGameDeleteFile} {
			res := call(t, cs, tool, map[string]any{"file": target})
			if !res.IsError || !strings.Contains(text(res), "You only") {
				t.Errorf("%s targeting %s: got error=%v text=%q, want You-only refusal", tool, target, res.IsError, text(res))
			}
		}
	}

	if customInvoked != 0 {
		t.Fatalf("custom write/delete handler ran %d times on You-only targets, want 0", customInvoked)
	}
	afterPermBytes, err := os.ReadFile(permPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(origPermBytes, afterPermBytes) {
		t.Fatal("permissions.yaml on disk was modified despite You-only refusals")
	}
}

// #1015 Acceptance 1 (draft flow): permissions_draft opens a draft with 0 ticked;
// Keep all leaves rules unchanged; ticking a single rule and applying applies ONLY that rule and records it as the person's.
func TestMCPPermissionsDraftFlow(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolPermissionsDraft, map[string]any{
		"request": "stop asking me before every scene edit",
		"rules": []any{
			map[string]any{"action": "edit", "patterns": []any{"game.yaml", "brief.yaml"}, "decision": "allow"},
			map[string]any{"action": "delete", "patterns": []any{"prefabs/**"}, "decision": "ask"},
		},
	})
	mustOK(t, res)
	var out editormcp.PermissionsDraftResult
	structured(t, res, &out)
	if out.Status != "draft_opened" || out.RuleCount != 2 || out.TickedCount != 0 {
		t.Fatalf("unexpected draft result: %+v", out)
	}

	// Keep all with draft open leaves permissions unchanged.
	if _, err := f.ed.KeepAllReviewChanges(); err != nil {
		t.Fatal(err)
	}
	if ev := f.ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "game.yaml"}); ev.Decision != editor.PermissionAsk {
		t.Fatalf("Keep all must not apply draft: %+v", ev)
	}

	// Person ticks ONLY rule 0 and applies.
	f.ed.SetPermissionsDraftRuleTicked(0, true)
	n, err := f.ed.ApplyPermissionsDraft()
	if err != nil || n != 1 {
		t.Fatalf("ApplyPermissionsDraft = (%d, %v), want (1, nil)", n, err)
	}
	if ev := f.ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "game.yaml"}); ev.Decision != editor.PermissionAllow {
		t.Fatalf("ticked rule did not apply: %+v", ev)
	}
	if ev := f.ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionDelete, Target: "prefabs/old.prefab.yaml"}); ev.Decision != editor.PermissionDeny {
		t.Fatalf("unticked rule must stay Deny: %+v", ev)
	}
	if f.ed.History.UndoActor() != editor.HistoryActorPerson {
		t.Fatalf("applied draft must be attributed to person in History, got %q", f.ed.History.UndoActor())
	}
}

// #1015 Acceptance 2 (MCP enforcement): A pulled permissions.yaml that turns Deny -> Allow
// is held until confirmed, so MCP tool calls matching that rule remain Denied.
func TestMCPPulledLooseningHeldUntilConfirmed(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })

	shellRuns := 0
	if err := s.AddTool(editormcp.Tool{
		Name:   "run_shell",
		Title:  "Run shell command",
		Action: editormcp.ActionRun,
		Handler: func(context.Context, *editormcp.Call) (*editormcp.Result, error) {
			shellRuns++
			return &editormcp.Result{Text: "ok"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, s, nil)

	// Baseline: shell is Denied.
	if res := call(t, cs, "run_shell", map[string]any{"command": "echo hi"}); !res.IsError || !strings.Contains(text(res), "permission denied") {
		t.Fatalf("baseline run_shell should be denied, got err=%v text=%q", res.IsError, text(res))
	}

	// Simulate git pull turning Run shell from Deny -> Allow on disk.
	pulled := editor.DefaultPermissions()
	pulled.Rules[6].Decision = editor.PermissionAllow
	if err := editor.SavePermissions(editor.PermissionsFilePath(f.dir), &pulled); err != nil {
		t.Fatal(err)
	}
	f.ed.PollDiskChanges()

	// Must STILL be denied until confirmed!
	if res := call(t, cs, "run_shell", map[string]any{"command": "echo hi"}); !res.IsError || !strings.Contains(text(res), "permission denied") {
		t.Fatalf("pulled Deny->Allow must be held at Deny, got err=%v text=%q", res.IsError, text(res))
	}
	if shellRuns != 0 {
		t.Fatalf("run_shell handler ran %d times while held, want 0", shellRuns)
	}

	// Once person confirms in Settings › Permissions, it runs.
	held := f.ed.HeldPermissionLoosenings()
	if len(held) != 1 {
		t.Fatalf("expected 1 held loosening, got %+v", held)
	}
	if err := f.ed.ConfirmHeldPermissionLoosening(held[0].ID); err != nil {
		t.Fatal(err)
	}
	mustOK(t, call(t, cs, "run_shell", map[string]any{"command": "echo hi"}))
	if shellRuns != 1 {
		t.Fatalf("run_shell runs = %d, want 1", shellRuns)
	}
}

// #1015 Acceptance 3 (MCP game_delete_file): proposes deletion with line count and scene usages,
// Keep all leaves the file, confirming deletes it, and Undo restores it.
func TestMCPGameDeleteFileProposesAndKeepAllLeavesFile(t *testing.T) {
	f := openFixture(t)
	prefabsDir := filepath.Join(f.dir, "prefabs")
	if err := os.MkdirAll(prefabsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unusedFile := filepath.Join(prefabsDir, "slime_old.prefab.yaml")
	origContent := []byte("id: slime_old\nsprite: actors/slime\ncomponents:\n  body:\n    speed: 24\n")
	if err := os.WriteFile(unusedFile, origContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := f.ed.SetMCPToolEnabled(editormcp.ToolGameDeleteFile, true); err != nil {
		t.Fatal(err)
	}
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Add a regular review item alongside the proposed deletion.
	otherKept := false
	f.ed.OpenAgentReview("clean up the old slime", []editor.AgentReviewChange{
		{ID: "c1", Path: "scenes/room.scene.yaml", Title: "room.scene.yaml", Detail: "switched to new slime", OnKeep: func() error { otherKept = true; return nil }},
	})

	res := call(t, cs, editormcp.ToolGameDeleteFile, map[string]any{
		"file":   "prefabs/slime_old.prefab.yaml",
		"reason": "clean up the old slime",
	})
	mustOK(t, res)
	var out editormcp.GameDeleteFileResult
	structured(t, res, &out)
	if out.Status != "proposed_deletion" || out.LineCount != 5 || out.UsedByAnyScene || out.DeletedOnDisk || !out.ToolEnabled {
		t.Fatalf("unexpected game_delete_file result: %+v", out)
	}
	if _, err := os.Stat(unusedFile); err != nil {
		t.Fatalf("game_delete_file must NOT delete the file on disk: %v", err)
	}

	// Keep all keeps the other change and leaves prefabs/slime_old.prefab.yaml on disk.
	kres, err := f.ed.KeepAllReviewChanges()
	if err != nil {
		t.Fatal(err)
	}
	if !otherKept || kres.KeptChanges != 1 || len(kres.DeletedFiles) != 0 || len(kres.LeftPendingDeletions) != 1 {
		t.Fatalf("KeepAll result = %+v, otherKept=%v", kres, otherKept)
	}
	if _, err := os.Stat(unusedFile); err != nil {
		t.Fatalf("file must remain on disk after Keep all: %v", err)
	}

	// Confirming deletes it; Undo (⌘Z) restores it byte-for-byte.
	if err := f.ed.ConfirmProposedDeletion("prefabs/slime_old.prefab.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unusedFile); !os.IsNotExist(err) {
		t.Fatalf("expected file deleted after confirm, err=%v", err)
	}
	if !f.ed.Undo() {
		t.Fatal("Undo did not restore deleted file")
	}
	restored, err := os.ReadFile(unusedFile)
	if err != nil || !bytes.Equal(restored, origContent) {
		t.Fatalf("restored file mismatch: err=%v got=%q", err, restored)
	}
}

// Security regression test covering #1015 Blockers 1 & 2 over MCP:
//   - bare-stem refactor_rename ("from": "scratch.yaml", "to": "permissions") -> refused as You-only!
//   - release_notes_draft when ExportReleaseNotes() points to permissions.yaml -> refused as You-only!
//   - open-document mutating tool with unrelated "path": "hp" when FilePath is permissions.yaml -> refused as You-only!
//   - pulled Ask -> Allow loosening is held at Ask and routed through the real Ask approval flow (never silently allowed!).
func TestMCPYouOnlyBareStemRenameReleaseNotesOpenDocAndHeldAskFlow(t *testing.T) {
	f := openFixture(t)
	if err := os.WriteFile(filepath.Join(f.dir, "scratch.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}

	var askCount atomic.Int32
	s := editormcp.New(f.ed, editormcp.Options{
		OnPermissionAsk: func(_ context.Context, eval editor.PermissionEvaluation) (editormcp.PermissionPromptAnswer, error) {
			askCount.Add(1)
			return editormcp.PermissionAnswerDeny, nil
		},
	})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. Bare-stem refactor_rename ("scratch.yaml" -> "permissions" => "permissions.yaml") MUST be refused as You-only!
	resBare := call(t, cs, editormcp.ToolRefactorRename, map[string]any{
		"from":  "scratch.yaml",
		"to":    "permissions",
		"apply": true,
	})
	if !resBare.IsError || !strings.Contains(text(resBare), "You only") {
		t.Fatalf("bare-stem refactor_rename to permissions: got err=%v text=%q, want You-only refusal", resBare.IsError, text(resBare))
	}

	// 2. release_notes_draft targeting permissions.yaml via ExportReleaseNotes MUST be refused as You-only!
	origNotes := f.ed.ExportReleaseNotes()
	f.ed.SetExportReleaseNotes("permissions.yaml")
	resNotes := call(t, cs, editormcp.ToolReleaseNotesDraft, map[string]any{
		"markdown": "# Hijacked release notes\n",
	})
	f.ed.SetExportReleaseNotes(origNotes)
	if !resNotes.IsError || !strings.Contains(text(resNotes), "You only") {
		t.Fatalf("release_notes_draft targeting permissions.yaml: got err=%v text=%q, want You-only refusal", resNotes.IsError, text(resNotes))
	}

	// 3. Open-doc mutating tool with unrelated arg key "path": "hp" while FilePath is permissions.yaml MUST STILL be refused as You-only!
	origFile := f.ed.FilePath
	f.ed.FilePath = filepath.Join(f.dir, "permissions.yaml")
	resObj := call(t, cs, editormcp.ToolObjectSetField, map[string]any{
		"id":    "hero",
		"path":  "hp",
		"value": 99,
	})
	f.ed.FilePath = origFile
	if !resObj.IsError || !strings.Contains(text(resObj), "You only") {
		t.Fatalf("object_set_field with path=hp while FilePath=permissions.yaml: got err=%v text=%q, want You-only refusal", resObj.IsError, text(resObj))
	}

	// 4. Held Ask -> Allow loosening MUST route through OnPermissionAsk (never silently allowed!).
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatal(err)
	}
	pulled := editor.PermissionPreset(editor.PermissionProfileCautious)
	pulled.Rules[1].Decision = editor.PermissionAllow // Run game check: Ask -> Allow (must be held at Ask!)
	if err := editor.SavePermissions(editor.PermissionsFilePath(f.dir), &pulled); err != nil {
		t.Fatal(err)
	}
	f.ed.PollDiskChanges()

	resAsk := call(t, cs, editormcp.ToolGameCheck, map[string]any{})
	if !resAsk.IsError || askCount.Load() != 1 {
		t.Fatalf("held Ask->Allow loosening must route through OnPermissionAsk and not run silently: err=%v asks=%d text=%q", resAsk.IsError, askCount.Load(), text(resAsk))
	}
}
