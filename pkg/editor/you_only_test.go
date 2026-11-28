package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestPermissionsDraftNothingTickedByDefaultAndKeepAllLeavesUnchanged(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	// Even under Autonomous profile, a draft starts unticked and Keep all / ⌘↵ cannot apply it.
	if err := ed.SelectPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}
	beforePerms := ed.Permissions()

	draft, err := ed.SubmitPermissionsDraft(editor.PermissionsDraftRequest{
		Request: "stop asking me before every scene edit",
		Rules: []editor.PermissionsDraftRuleSpec{
			{Action: editor.PermissionActionEdit, Patterns: []string{"game.yaml", "brief.yaml"}, Decision: editor.PermissionAllow},
			{Action: editor.PermissionActionDelete, Patterns: []string{"prefabs/**"}, Decision: editor.PermissionAsk},
			{Action: editor.PermissionActionRun, Patterns: []string{"shell"}, Decision: editor.PermissionAsk},
		},
	})
	if err != nil {
		t.Fatalf("SubmitPermissionsDraft: %v", err)
	}
	if !ed.PermissionsDraftOpen() || len(draft.Rules) != 3 {
		t.Fatalf("draft not opened properly: %+v", draft)
	}
	for i, r := range draft.Rules {
		if r.Ticked {
			t.Fatalf("rule %d must NOT be ticked by default", i)
		}
	}
	if ed.PermissionsDraftTickedCount() != 0 {
		t.Fatalf("ticked count = %d, want 0", ed.PermissionsDraftTickedCount())
	}
	if lines := ed.PermissionsDraftPreviewYAML(); len(lines) != 0 {
		t.Fatalf("preview YAML should be empty when 0 rules ticked, got %+v", lines)
	}

	// Apply with 0 ticked must fail and leave rules unchanged.
	if n, err := ed.ApplyPermissionsDraft(); err != editor.ErrNoDraftRulesTicked || n != 0 {
		t.Fatalf("ApplyPermissionsDraft with 0 ticked = (%d, %v), want (0, ErrNoDraftRulesTicked)", n, err)
	}

	// Keep all while draft is open must leave permissions and draft completely unchanged.
	keepRes, err := ed.KeepAllReviewChanges()
	if err != nil {
		t.Fatalf("KeepAllReviewChanges: %v", err)
	}
	if !keepRes.LeftPermissionsDraft || !ed.PermissionsDraftOpen() {
		t.Fatalf("KeepAll must leave permissions draft open: %+v", keepRes)
	}
	if got := ed.Permissions(); !samePermissions(got, beforePerms) {
		t.Fatalf("KeepAll changed permissions:\ngot  %+v\nwant %+v", got, beforePerms)
	}

	// Switch back to Balanced so rule 0 is Ask -> Allow.
	if err := ed.SelectPermissionProfile(editor.PermissionProfileBalanced); err != nil {
		t.Fatal(err)
	}
	// Re-submit draft under Balanced.
	if _, err := ed.SubmitPermissionsDraft(editor.PermissionsDraftRequest{
		Request: "allow editing game.yaml",
		Rules: []editor.PermissionsDraftRuleSpec{
			{Action: editor.PermissionActionEdit, Patterns: []string{"game.yaml", "brief.yaml"}, Decision: editor.PermissionAllow},
			{Action: editor.PermissionActionDelete, Patterns: []string{"prefabs/**"}, Decision: editor.PermissionAsk},
			{Action: editor.PermissionActionRun, Patterns: []string{"shell"}, Decision: editor.PermissionAsk},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Person ticks ONLY rule 0 (Edit game.yaml brief.yaml -> Allow), leaves rules 1 and 2 unticked.
	ed.TogglePermissionsDraftRule(0)
	if ed.PermissionsDraftTickedCount() != 1 {
		t.Fatalf("ticked count = %d, want 1", ed.PermissionsDraftTickedCount())
	}
	preview := ed.PermissionsDraftPreviewYAML()
	if len(preview) == 0 || !strings.Contains(preview[0].Text, "game.yaml") {
		t.Fatalf("unexpected YAML preview: %+v", preview)
	}

	applied, err := ed.ApplyPermissionsDraft()
	if err != nil || applied != 1 {
		t.Fatalf("ApplyPermissionsDraft = (%d, %v), want (1, nil)", applied, err)
	}
	if ed.PermissionsDraftOpen() {
		t.Fatal("draft should close after Apply")
	}
	// History must record this as the person's action.
	if actor := ed.History.UndoActor(); actor != editor.HistoryActorPerson {
		t.Fatalf("History UndoActor = %q, want %q", actor, editor.HistoryActorPerson)
	}
	if !strings.Contains(strings.ToLower(ed.History.UndoLabel()), "by you") {
		t.Fatalf("History UndoLabel = %q, want mention of 'by you'", ed.History.UndoLabel())
	}
	// Rule 0 applied; unticked rules 1 and 2 remain Deny!
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "game.yaml"}); ev.Decision != editor.PermissionAllow {
		t.Fatalf("ticked rule did not apply: %+v", ev)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionDelete, Target: "prefabs/slime.prefab.yaml"}); ev.Decision != editor.PermissionDeny {
		t.Fatalf("unticked delete rule must remain Deny: %+v", ev)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionRun, Target: "shell"}); ev.Decision != editor.PermissionDeny {
		t.Fatalf("unticked shell rule must remain Deny: %+v", ev)
	}

	// Undo restores the previous permissions.yaml cleanly.
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "game.yaml"}); ev.Decision != editor.PermissionAsk {
		t.Fatalf("after Undo, game.yaml edit should return to Ask, got %+v", ev)
	}
}

func TestPulledPermissionsFileHoldsLooseningAndAppliesTightening(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	// Baseline: Balanced (Edit scenes/** -> Allow, Delete ** -> Deny, Run shell -> Deny).
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "scenes/title.scene.yaml"}); ev.Decision != editor.PermissionAllow {
		t.Fatalf("baseline scenes edit = %v, want Allow", ev.Decision)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionDelete, Target: "prefabs/old.prefab.yaml"}); ev.Decision != editor.PermissionDeny {
		t.Fatalf("baseline delete = %v, want Deny", ev.Decision)
	}

	// Simulate a git pull rewriting permissions.yaml on disk:
	// - Tightens Edit scenes/** from Allow -> Ask (must apply at once!)
	// - Loosens Delete ** from Deny -> Allow (must be HELD until confirmed!)
	// - Loosens Run shell from Deny -> Allow (must be HELD until confirmed!)
	pulled := editor.DefaultPermissions()
	pulled.Rules[0].Decision = editor.PermissionAsk   // tightened: Allow -> Ask
	pulled.Rules[5].Decision = editor.PermissionAllow // loosened: Deny -> Allow
	pulled.Rules[6].Decision = editor.PermissionAllow // loosened: Deny -> Allow
	if err := editor.SavePermissions(editor.PermissionsFilePath(dir), &pulled); err != nil {
		t.Fatal(err)
	}

	res := ed.PollDiskChanges()
	if !containsDiskKind(res.Reloaded, editor.DiskFilePermissions) {
		t.Fatalf("PollDiskChanges did not observe permissions.yaml: %+v", res)
	}

	// Tightened rule applied immediately!
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "scenes/title.scene.yaml"}); ev.Decision != editor.PermissionAsk {
		t.Fatalf("tightened scenes edit should be Ask immediately, got %+v", ev)
	}
	// Loosened rules are HELD at Deny!
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionDelete, Target: "prefabs/old.prefab.yaml"}); ev.Decision != editor.PermissionDeny || !ev.HeldByDisk {
		t.Fatalf("pulled Deny->Allow for delete must be held at Deny, got %+v", ev)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionRun, Target: "shell"}); ev.Decision != editor.PermissionDeny || !ev.HeldByDisk {
		t.Fatalf("pulled Deny->Allow for shell must be held at Deny, got %+v", ev)
	}
	held := ed.HeldPermissionLoosenings()
	if len(held) != 2 {
		t.Fatalf("held loosenings = %d, want 2 (%+v)", len(held), held)
	}

	// Person confirms ONLY the delete loosening in Settings › Permissions.
	if err := ed.ConfirmHeldPermissionLoosening(held[0].ID); err != nil {
		t.Fatalf("ConfirmHeldPermissionLoosening: %v", err)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionDelete, Target: "prefabs/old.prefab.yaml"}); ev.Decision != editor.PermissionAllow {
		t.Fatalf("after confirming delete loosening, decision = %v, want Allow", ev.Decision)
	}
	// The second loosening (Run shell) must STILL be held at Deny!
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionRun, Target: "shell"}); ev.Decision != editor.PermissionDeny || !ev.HeldByDisk {
		t.Fatalf("unconfirmed shell loosening must remain held at Deny, got %+v", ev)
	}

	// Rejecting the remaining loosening keeps Deny and writes it back to disk.
	remHeld := ed.HeldPermissionLoosenings()
	if len(remHeld) != 1 {
		t.Fatalf("remaining held = %d, want 1", len(remHeld))
	}
	if err := ed.RejectHeldPermissionLoosening(remHeld[0].ID); err != nil {
		t.Fatalf("RejectHeldPermissionLoosening: %v", err)
	}
	if len(ed.HeldPermissionLoosenings()) != 0 {
		t.Fatalf("expected 0 held loosenings after reject")
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionRun, Target: "shell"}); ev.Decision != editor.PermissionDeny {
		t.Fatalf("rejected shell loosening must stay Deny, got %+v", ev)
	}
}

func TestClonedProjectWithRemovedDenyRuleIsHeldUntilConfirmed(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	// Cloned repo ships a permissions.yaml that removed the Delete ** Deny rule and replaced it with Allow.
	cloned := editor.DefaultPermissions()
	cloned.Rules = append(cloned.Rules[:5], cloned.Rules[6:]...)
	if err := editor.SavePermissions(editor.PermissionsFilePath(dir), &cloned); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionDelete, Target: "prefabs/old.prefab.yaml"}); ev.Decision != editor.PermissionDeny {
		t.Fatalf("cloned repo removing Delete Deny must remain Deny, got %+v", ev)
	}
}

func TestProposedDeletionKeepAllLeavesFileConfirmDeletesAndUndoRestores(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	oldPrefabPath := filepath.Join(dir, "prefabs", "slime_old.prefab.yaml")
	if err := os.MkdirAll(filepath.Dir(oldPrefabPath), 0o755); err != nil {
		t.Fatal(err)
	}
	origBytes := []byte("id: slime_old\nsprite: slime\nhp: 5\n")
	if err := os.WriteFile(oldPrefabPath, origBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	keptCount := 0
	ed.OpenAgentReview("clean up the old slime", []editor.AgentReviewChange{
		{ID: "c1", Path: "scenes/a.scene.yaml", Title: "a.scene.yaml", OnKeep: func() error { keptCount++; return nil }},
		{ID: "c2", Path: "scenes/b.scene.yaml", Title: "b.scene.yaml", OnKeep: func() error { keptCount++; return nil }},
		{ID: "c3", Path: "prefabs/slime.prefab.yaml", Title: "slime.prefab.yaml", OnKeep: func() error { keptCount++; return nil }},
	})
	pd, err := ed.ProposeFileDeletion("prefabs/slime_old.prefab.yaml", "clean up the old slime")
	if err != nil {
		t.Fatalf("ProposeFileDeletion: %v", err)
	}
	if pd.LineCount != 3 || pd.UsedByAnyScene {
		t.Fatalf("unexpected ProposedDeletion: %+v", pd)
	}

	// Keep all keeps the 3 regular changes and leaves prefabs/slime_old.prefab.yaml on disk!
	kres, err := ed.KeepAllReviewChanges()
	if err != nil {
		t.Fatalf("KeepAllReviewChanges: %v", err)
	}
	if kres.KeptChanges != 3 || keptCount != 3 {
		t.Fatalf("kept changes = %d (callback %d), want 3", kres.KeptChanges, keptCount)
	}
	if len(kres.DeletedFiles) != 0 || len(kres.LeftPendingDeletions) != 1 || !kres.ReviewStillOpen {
		t.Fatalf("KeepAll must leave pending deletion untouched and open: %+v", kres)
	}
	if got, err := os.ReadFile(oldPrefabPath); err != nil || !bytes.Equal(got, origBytes) {
		t.Fatalf("file must remain intact on disk after Keep all: err=%v", err)
	}

	// Person confirms the deletion in place -> deletes the file from disk!
	if err := ed.ConfirmProposedDeletion("prefabs/slime_old.prefab.yaml"); err != nil {
		t.Fatalf("ConfirmProposedDeletion: %v", err)
	}
	if _, err := os.Stat(oldPrefabPath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted after confirmation, stat err=%v", err)
	}
	if ed.History.UndoActor() != editor.HistoryActorPerson {
		t.Fatalf("deletion history actor = %q, want person", ed.History.UndoActor())
	}

	// ⌘Z (Undo) restores the file byte-for-byte from the checkpoint!
	if !ed.Undo() {
		t.Fatal("Undo failed to restore deleted file")
	}
	restored, err := os.ReadFile(oldPrefabPath)
	if err != nil {
		t.Fatalf("file was not restored on Undo: %v", err)
	}
	if !bytes.Equal(restored, origBytes) {
		t.Fatalf("restored bytes differ from original")
	}
}

// A background automation holds the process-wide actor marker, but the person's review action remains available.
func TestPersonConfirmsProposedDeletionWhileAutomationRuns(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	personPath := filepath.Join(dir, "prefabs", "person-delete.prefab.yaml")
	agentPath := filepath.Join(dir, "prefabs", "agent-delete.prefab.yaml")
	if err := os.MkdirAll(filepath.Dir(personPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for path := range map[string]string{
		personPath: "id: person-delete\n",
		agentPath:  "id: agent-delete\n",
	} {
		if err := os.WriteFile(path, []byte("id: "+filepath.Base(path)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	defer ed.Close()

	openDeletion := func(path string) {
		ed.OpenAgentReview("clean up files", []editor.AgentReviewChange{
			{ID: "keep-" + filepath.Base(path), Path: "scenes/keep.scene.yaml", Title: "keep.scene.yaml", OnKeep: func() error { return nil }},
		})
		if _, err := ed.ProposeFileDeletion(filepath.ToSlash(filepath.Join("prefabs", filepath.Base(path))), "clean up files"); err != nil {
			t.Fatal(err)
		}
		if _, err := ed.KeepAllReviewChanges(); err != nil {
			t.Fatal(err)
		}
	}

	openDeletion(personPath)
	release := make(chan struct{})
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- ed.WithAgentActor(func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	if !ed.AgentActorActive() {
		t.Fatal("precondition: automation actor should be active")
	}
	if err := ed.ConfirmProposedDeletion("prefabs/person-delete.prefab.yaml"); err != nil {
		t.Fatalf("person ConfirmProposedDeletion while automation runs: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("automation actor: %v", err)
	}
	if _, err := os.Stat(personPath); !os.IsNotExist(err) {
		t.Fatalf("person deletion did not apply, stat error = %v", err)
	}

	openDeletion(agentPath)
	if err := ed.RunAsAgent(func() error {
		return ed.ConfirmProposedDeletion("prefabs/agent-delete.prefab.yaml")
	}); !errors.Is(err, editor.ErrAutomationYouOnly) {
		t.Fatalf("agent ConfirmProposedDeletion = %v, want ErrAutomationYouOnly", err)
	}
	if _, err := os.Stat(agentPath); err != nil {
		t.Fatalf("agent refusal should leave file on disk: %v", err)
	}
}

func TestPermissionsDraftAndDeletionUIFlowAndModEnterGuard(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	prefabPath := filepath.Join(dir, "prefabs", "slime_old.prefab.yaml")
	if err := os.MkdirAll(filepath.Dir(prefabPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefabPath, []byte("id: slime_old\nsprite: slime\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// 1. Open permissions draft -> nothing ticked -> ⌘↵ does NOT apply it!
	app.Post(func() {
		_, _ = ed.SubmitPermissionsDraft(editor.PermissionsDraftRequest{
			Request: "stop asking me before every scene edit",
			Rules: []editor.PermissionsDraftRuleSpec{
				{Action: editor.PermissionActionEdit, Patterns: []string{"game.yaml", "brief.yaml"}, Decision: editor.PermissionAllow},
				{Action: editor.PermissionActionDelete, Patterns: []string{"prefabs/**"}, Decision: editor.PermissionAsk},
			},
		})
	})
	app.Settle()

	if !app.Exists("id:perm-draft-card") || !app.Exists("id:perm-draft-you-only") {
		t.Fatal("permissions draft card not rendered")
	}
	if !app.Exists("id:perm-draft-yaml-none") {
		t.Fatal("expected 'No changes ticked' initially")
	}
	// Press ⌘↵: must NOT apply the draft!
	app.Key("mod+enter")
	app.Settle()
	if !ed.PermissionsDraftOpen() {
		t.Fatal("⌘↵ must NEVER apply a permissions draft")
	}

	// Tick rule 0 and click Apply.
	app.Press("id:perm-draft-tick-0")
	app.Settle()
	if app.Exists("id:perm-draft-yaml-none") {
		t.Fatal("YAML preview should show diff once rule 0 is ticked")
	}
	app.Press("id:perm-draft-apply")
	app.Settle()
	if ed.PermissionsDraftOpen() {
		t.Fatal("draft should close after clicking Apply")
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "game.yaml"}); ev.Decision != editor.PermissionAllow {
		t.Fatalf("rule 0 should be Allow after clicking Apply, got %+v", ev)
	}

	// 2. Open Settings › Permissions and verify You-only pill and panel render.
	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPagePermissions) })
	app.Settle()
	if !app.Exists("id:permissions-settings-panel") || !app.Exists("id:settings-page-you-only") {
		t.Fatal("Settings › Permissions view missing panel or You-only badge")
	}
	app.Post(func() { ed.CloseSettings() })
	app.Settle()

	// 3. Open Review changes with a regular change + proposed deletion; click Keep -> file stays; click Delete this file -> deleted; Undo -> restored.
	app.Post(func() {
		ed.OpenAgentReview("clean up the old slime", []editor.AgentReviewChange{
			{ID: "c1", Path: "scenes/title.scene.yaml", Title: "title.scene.yaml", Detail: "updated slime ref"},
		})
		_, _ = ed.ProposeFileDeletion("prefabs/slime_old.prefab.yaml", "clean up the old slime")
	})
	app.Settle()
	if !app.Exists("id:agent-review-panel") || !app.Exists("id:agent-review-deletion-0") || !app.Exists("id:agent-review-close") {
		t.Fatal("agent review panel, proposed deletion card, or close button missing")
	}
	app.Press("id:agent-review-keep-all")
	app.Settle()
	if _, err := os.Stat(prefabPath); err != nil {
		t.Fatalf("Keep button must leave proposed deletion file on disk: %v", err)
	}
	app.Press("id:agent-review-del-confirm-0")
	app.Settle()
	if _, err := os.Stat(prefabPath); !os.IsNotExist(err) {
		t.Fatalf("Confirming deletion must delete file on disk, err=%v", err)
	}
	app.Post(func() { ed.Undo() })
	app.Settle()
	if _, err := os.Stat(prefabPath); err != nil {
		t.Fatalf("Undo must restore file on disk: %v", err)
	}
}

// --- Security regression tests for #1015 blockers 1-5 ---

func TestYouOnlySymlinkedNonExistentDirCaseSettingsAndBareStemRename(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	if err := os.MkdirAll(filepath.Join(dir, ".nsgo"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Symlink pointing to .nsgo/ so a non-existent file "alias_cfg/tools.yaml" resolves inside .nsgo/tools.yaml!
	if err := os.Symlink(filepath.Join(dir, ".nsgo"), filepath.Join(dir, "alias_cfg")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	// Root scratch.yaml to test bare-stem rename "scratch.yaml" -> "permissions" => "permissions.yaml".
	if err := os.WriteFile(filepath.Join(dir, "scratch.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settingsDir := t.TempDir()
	settingsFile := filepath.Join(settingsDir, "editor.yaml")
	if err := os.WriteFile(settingsFile, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.SettingsPath = settingsFile
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// 1. Non-existent file under symlinked ancestor directory pointing to .nsgo/tools.yaml MUST match YouOnlyTools.
	if dom, ok := ed.CheckYouOnlyTarget("alias_cfg/tools.yaml"); !ok || dom != editor.YouOnlyTools {
		t.Fatalf("CheckYouOnlyTarget(alias_cfg/tools.yaml) = (%q, %v), want (tools, true)", dom, ok)
	}

	// 2. Case-insensitive comparison against SettingsPath on macOS/Windows.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		upperSettings := filepath.Join(settingsDir, "Editor.YAML")
		if dom, ok := ed.CheckYouOnlyTarget(upperSettings); !ok || dom != editor.YouOnlyPermissions {
			t.Fatalf("CheckYouOnlyTarget(%q) = (%q, %v), want (permissions, true)", upperSettings, dom, ok)
		}
	}

	// 3. Bare-stem PreviewSafeRename("scratch.yaml", "permissions") under WithAgentActor MUST be refused!
	err = ed.RunAsAgent(func() error {
		_, rerr := ed.PreviewSafeRename("scratch.yaml", "permissions")
		return rerr
	})
	if err == nil || !strings.Contains(err.Error(), "You only") {
		t.Fatalf("bare-stem PreviewSafeRename to permissions under agent actor = %v, want You-only refusal", err)
	}
	// MoveFile under WithAgentActor to permissions.yaml MUST also be refused!
	err = ed.WithAgentActor(func() error {
		return ed.MoveFile("scratch.yaml", "permissions.yaml")
	})
	if err == nil || !strings.Contains(err.Error(), "You only") {
		t.Fatalf("MoveFile to permissions.yaml under agent actor = %v, want You-only refusal", err)
	}
}

func TestPulledProfileLooseningHeldAndRuleIndexAdjustment(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Baseline is Balanced: unmatched file "docs/readme.md" defaults to Ask.
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "docs/readme.md"}); ev.Decision != editor.PermissionAsk {
		t.Fatalf("baseline unmatched edit = %+v, want Ask", ev)
	}

	// Simulate pulling a permissions.yaml that loosens Profile from Balanced -> Autonomous (while keeping rules identical)!
	pulled := editor.DefaultPermissions()
	pulled.Profile = editor.PermissionProfileAutonomous
	if err := editor.SavePermissions(editor.PermissionsFilePath(dir), &pulled); err != nil {
		t.Fatal(err)
	}
	ed.PollDiskChanges()

	// Effective profile MUST stay Balanced and unmatched edit MUST stay Ask (held from disk)!
	if got := ed.Permissions().Profile; got != editor.PermissionProfileBalanced {
		t.Fatalf("pulled Balanced->Autonomous leaked profile=%q, want balanced", got)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "docs/readme.md"}); ev.Decision != editor.PermissionAsk || !ev.HeldByDisk {
		t.Fatalf("pulled profile loosening must hold unmatched edit at Ask, got %+v", ev)
	}
	held := ed.HeldPermissionLoosenings()
	if len(held) != 1 || held[0].Kind != editor.HeldLooseningProfileRelaxed {
		t.Fatalf("expected 1 HeldLooseningProfileRelaxed, got %+v", held)
	}

	// Confirming the profile loosening promotes effective profile to Autonomous -> unmatched edit becomes Allow.
	if err := ed.ConfirmHeldPermissionLoosening(held[0].ID); err != nil {
		t.Fatalf("ConfirmHeldPermissionLoosening: %v", err)
	}
	if got := ed.Permissions().Profile; got != editor.PermissionProfileAutonomous {
		t.Fatalf("after confirming profile loosening, profile = %q, want autonomous", got)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "docs/readme.md"}); ev.Decision != editor.PermissionAllow {
		t.Fatalf("after confirming profile loosening, unmatched edit = %+v, want Allow", ev)
	}

	// Also verify RuleIndex adjustment when both a removed guard rule AND a relaxed rule are held:
	// confirming the removed guard rule must keep the relaxed rule's RuleIndex pointing to the exact right rule!
	prev := editor.DefaultPermissions()
	// Add a stricter Deny rule on scenes/locked.scene.yaml ahead of scenes/** -> Allow.
	prev.Rules = append([]editor.PermissionRule{
		{Action: editor.PermissionActionEdit, Patterns: []string{"scenes/locked.scene.yaml"}, Decision: editor.PermissionDeny},
	}, prev.Rules...)
	if err := ed.SetPermissions(prev); err != nil {
		t.Fatal(err)
	}
	// External file removes the scenes/locked.scene.yaml Deny rule AND relaxes Run shell (Deny -> Allow).
	inc := editor.DefaultPermissions()
	inc.Rules[6].Decision = editor.PermissionAllow // Run shell relaxed
	if err := editor.SavePermissions(editor.PermissionsFilePath(dir), &inc); err != nil {
		t.Fatal(err)
	}
	ed.PollDiskChanges()
	held2 := ed.HeldPermissionLoosenings()
	if len(held2) != 2 {
		t.Fatalf("expected 2 held entries (1 relaxed + 1 removed), got %+v", held2)
	}
	var removedID, relaxedID string
	for _, h := range held2 {
		switch h.Kind {
		case editor.HeldLooseningRuleRemoved:
			removedID = h.ID
		case editor.HeldLooseningRuleRelaxed:
			relaxedID = h.ID
		}
	}
	if removedID == "" || relaxedID == "" {
		t.Fatalf("expected both removed and relaxed entries, got %+v", held2)
	}
	// Confirm the removed guard rule first, then confirm the relaxed rule: both scenes/locked.scene.yaml and Run shell must become Allow (proving RuleIndex shifted accurately)!
	if err := ed.ConfirmHeldPermissionLoosening(removedID); err != nil {
		t.Fatalf("confirm removed: %v", err)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionEdit, Target: "scenes/locked.scene.yaml"}); ev.Decision != editor.PermissionAllow {
		t.Fatalf("after confirming removed guard rule, scenes/locked.scene.yaml = %+v, want Allow", ev)
	}
	if err := ed.ConfirmHeldPermissionLoosening(relaxedID); err != nil {
		t.Fatalf("confirm relaxed: %v", err)
	}
	if ev := ed.EvaluatePermission(editor.PermissionRequest{Action: editor.PermissionActionRun, Target: "shell"}); ev.Decision != editor.PermissionAllow {
		t.Fatalf("after confirming removed + relaxed rules, shell = %+v, want Allow", ev)
	}
}

func TestProposedDeletionSecurityTOCTOUSymlinkGitGameYAMLOpenTabAndCap(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	outsideFile := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prefabs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Huge > 1 MiB file.
	hugePath := filepath.Join(dir, "prefabs", "huge.bin")
	if err := os.WriteFile(hugePath, make([]byte, (1<<20)+64), 0o644); err != nil {
		t.Fatal(err)
	}
	// Regular file for TOCTOU symlink swap + undo overwrite tests.
	victimPath := filepath.Join(dir, "prefabs", "victim.prefab.yaml")
	if err := os.WriteFile(victimPath, []byte("id: victim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	symPath := filepath.Join(dir, "prefabs", "link.prefab.yaml")
	hasSymlink := os.Symlink(victimPath, symPath) == nil

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// 1. Refuse .git/config, game.yaml, open scene tab, >1 MiB file, and symlinks on propose.
	if _, err := ed.ProposeFileDeletion(".git/config", "test"); err == nil {
		t.Fatal("ProposeFileDeletion must refuse .git/config")
	}
	if _, err := ed.ProposeFileDeletion("game.yaml", "test"); err == nil {
		t.Fatal("ProposeFileDeletion must refuse game.yaml")
	}
	if ed.ScenePath != "" {
		if _, err := ed.ProposeFileDeletion(ed.ScenePath, "test"); err == nil {
			t.Fatalf("ProposeFileDeletion must refuse open scene tab %q", ed.ScenePath)
		}
	}
	if _, err := ed.ProposeFileDeletion("prefabs/huge.bin", "test"); err == nil {
		t.Fatal("ProposeFileDeletion must refuse files > 1 MiB")
	}
	if hasSymlink {
		if _, err := ed.ProposeFileDeletion("prefabs/link.prefab.yaml", "test"); err == nil {
			t.Fatal("ProposeFileDeletion must refuse symlinks")
		}
	}

	// 2. TOCTOU: propose regular file, swap it to a symlink before confirm -> confirm MUST fail and NOT delete outsideFile or symlink!
	if _, err := ed.ProposeFileDeletion("prefabs/victim.prefab.yaml", "cleanup"); err != nil {
		t.Fatalf("ProposeFileDeletion(victim): %v", err)
	}
	if hasSymlink {
		if err := os.Remove(victimPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsideFile, victimPath); err != nil {
			t.Fatal(err)
		}
		if err := ed.ConfirmProposedDeletion("prefabs/victim.prefab.yaml"); err == nil {
			t.Fatal("ConfirmProposedDeletion must reject TOCTOU symlink swap")
		}
		if _, err := os.Stat(outsideFile); err != nil {
			t.Fatalf("outside file must remain intact: %v", err)
		}
		// Restore regular file for undo-overwrite test.
		_ = os.Remove(victimPath)
		if err := os.WriteFile(victimPath, []byte("id: victim\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 3. Confirm delete succeeds on regular file, then if recreated externally, Undo MUST NOT overwrite the external file!
	if err := ed.ConfirmProposedDeletion("prefabs/victim.prefab.yaml"); err != nil {
		t.Fatalf("ConfirmProposedDeletion(victim regular): %v", err)
	}
	externalBytes := []byte("externally recreated\n")
	if err := os.WriteFile(victimPath, externalBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = ed.Undo()
	got, err := os.ReadFile(victimPath)
	if err != nil || !bytes.Equal(got, externalBytes) {
		t.Fatalf("Undo must refuse overwriting recreated file on disk, got %q (err=%v)", got, err)
	}
}

func TestOpenDemoResetsAgentReviewAndAutoSavePausesDuringReviewOrDraft(t *testing.T) {
	dir := writeProjectSettingsFixture(t)
	prefabPath := filepath.Join(dir, "prefabs", "stay.prefab.yaml")
	if err := os.MkdirAll(filepath.Dir(prefabPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefabPath, []byte("id: stay\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Blocker 5: HasOpenReview() must pause autosave when AgentReviewOpen() or PermissionsDraftOpen().
	if ed.HasOpenReview() {
		t.Fatal("HasOpenReview should be false initially")
	}
	if _, err := ed.ProposeFileDeletion("prefabs/stay.prefab.yaml", "cleanup"); err != nil {
		t.Fatal(err)
	}
	if !ed.HasOpenReview() {
		t.Fatal("HasOpenReview must be true while AgentReviewOpen")
	}

	// Blocker 4: OpenDemo() MUST clear agentReview so Demo cannot delete Project A's file!
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.AgentReviewOpen() || ed.AgentReview() != nil {
		t.Fatal("OpenDemo must reset agentReview")
	}
	if err := ed.ConfirmProposedDeletion("prefabs/stay.prefab.yaml"); err == nil {
		t.Fatal("ConfirmProposedDeletion after OpenDemo must fail")
	}
	if _, err := os.Stat(prefabPath); err != nil {
		t.Fatalf("previous project file must remain untouched after OpenDemo: %v", err)
	}

	// Blocker 5: PermissionsDraftOpen() also pauses autosave via HasOpenReview().
	if _, err := ed.SubmitPermissionsDraft(editor.PermissionsDraftRequest{
		Rules: []editor.PermissionsDraftRuleSpec{
			{Action: editor.PermissionActionEdit, Patterns: []string{"game.yaml"}, Decision: editor.PermissionAllow},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if !ed.HasOpenReview() {
		t.Fatal("HasOpenReview must be true while PermissionsDraftOpen")
	}
	ed.DiscardPermissionsDraft()
	if ed.HasOpenReview() {
		t.Fatal("HasOpenReview should return false after discarding draft")
	}
}

func samePermissions(a, b editor.PermissionsFile) bool {
	ea, errA := editor.EncodePermissions(&a)
	eb, errB := editor.EncodePermissions(&b)
	return errA == nil && errB == nil && bytes.Equal(ea, eb)
}

func containsDiskKind(list []editor.DiskFileKind, want editor.DiskFileKind) bool {
	for _, k := range list {
		if k == want {
			return true
		}
	}
	return false
}
