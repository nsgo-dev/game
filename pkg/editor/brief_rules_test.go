package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeMinimalProjectForBrief(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gameYAML := "version: 1\nname: Dungeon Run\nid: dev.nsgo.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// #1026: A project without brief.yaml shows the empty Game brief page; filling it writes a canonical
// brief.yaml that round-trips byte-for-byte unchanged, edits undo cleanly, and missing sections show as gaps.
func TestSettingsGameBriefEmptyFillRoundTripUndoAndGaps(t *testing.T) {
	dir := writeMinimalProjectForBrief(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.WaitForProblems()

	if ed.BriefExists() {
		t.Fatal("expected BriefExists() == false before brief.yaml exists")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	app.Post(func() { ed.OpenBriefSettings() })
	app.Settle()

	// Empty state banner and Empty badge visible.
	if emptyNode := app.Find("id:brief-empty-state"); emptyNode == nil {
		t.Fatal("expected id:brief-empty-state when project has no brief.yaml")
	}
	if meta := app.Find("id:editor-settings-tab-brief-meta").Text; meta != "Empty" {
		t.Fatalf("brief nav meta = %q, want Empty", meta)
	}

	// Fill Pitch, Art direction, Feel and difficulty, Genre, and Platforms (leaving Tone & audio missing -> "1 gap").
	app.Type("id:brief-pitch-input", "A short, tense dungeon platformer.")
	app.Type("id:brief-art-input", "16 px pixel art, 4-colour shading.")
	app.Type("id:brief-feel-input", "Snappy jumps. Clear floor 1 under a minute; 2-4 deaths on floor 3.")
	app.Press("id:brief-save-btn")

	app.Type("id:brief-genre-add-input", "2D platformer")
	app.Press("id:brief-genre-add-btn")
	app.Type("id:brief-platform-add-input", "desktop")
	app.Press("id:brief-platform-add-btn")

	// Add a structured playtest target.
	app.Type("id:brief-target-metric-input", "deaths")
	app.Type("id:brief-target-scene-input", "floor_03")
	app.Type("id:brief-target-min-input", "2")
	app.Type("id:brief-target-max-input", "4")
	app.Press("id:brief-target-add-btn")

	if !ed.BriefExists() {
		t.Fatal("expected BriefExists() == true after filling brief")
	}
	// Missing only Tone and audio -> "1 gap".
	if gaps := ed.BriefGaps(); !slices.Equal(gaps, []project.BriefSection{project.BriefSectionToneAndAudio}) {
		t.Fatalf("BriefGaps = %v, want [toneAndAudio]", gaps)
	}
	if meta := app.Find("id:editor-settings-tab-brief-meta").Text; meta != "1 gap" {
		t.Fatalf("brief nav meta = %q, want \"1 gap\"", meta)
	}

	// File written on disk round-trips byte-for-byte unchanged.
	briefFile := filepath.Join(dir, project.BriefFileName)
	diskBytes, err := os.ReadFile(briefFile)
	if err != nil {
		t.Fatalf("read brief.yaml: %v", err)
	}
	parsed, err := project.ParseBrief(diskBytes)
	if err != nil {
		t.Fatalf("ParseBrief: %v", err)
	}
	reEncoded, err := project.EncodeBrief(parsed)
	if err != nil {
		t.Fatalf("EncodeBrief: %v", err)
	}
	if !bytes.Equal(diskBytes, reEncoded) {
		t.Fatalf("brief.yaml did not round-trip unchanged:\n--- disk ---\n%s\n--- re-encoded ---\n%s", diskBytes, reEncoded)
	}

	// Playtest targets are readable from the editor.
	targets := ed.BriefPlaytestTargets("floor_03")
	if len(targets) != 1 || targets[0].Metric != "deaths" || *targets[0].Min != 2 || *targets[0].Max != 4 {
		t.Fatalf("BriefPlaytestTargets(floor_03) = %+v", targets)
	}

	// Undo removes the playtest target and updates disk; Redo restores it.
	app.Post(func() { _ = ed.Undo() })
	app.Settle()
	if got := ed.BriefPlaytestTargets("floor_03"); len(got) != 0 {
		t.Fatalf("expected 0 targets after undo, got %+v", got)
	}
	app.Post(func() { _ = ed.Redo() })
	app.Settle()
	if got := ed.BriefPlaytestTargets("floor_03"); len(got) != 1 {
		t.Fatalf("expected 1 target after redo, got %+v", got)
	}
}

// #1026: An unknown field in brief.yaml is refused with its line number and surfaced by OpenProject.
func TestBriefUnknownFieldRefusedWithLineNumber(t *testing.T) {
	dir := writeMinimalProjectForBrief(t)
	bad := "version: 1\npitch: Dungeon Run\nunknownSection: oops\n"
	if err := os.WriteFile(filepath.Join(dir, project.BriefFileName), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject should tolerate invalid brief.yaml and surface BriefLoadErr: %v", err)
	}
	ed.WaitForProblems()
	loadErr := ed.BriefLoadErr()
	if loadErr == nil {
		t.Fatal("expected BriefLoadErr() != nil for unknown field")
	}
	le, ok := loaderr.As(loadErr)
	if !ok || le.Line != 3 || !strings.Contains(loadErr.Error(), "unknownSection") {
		t.Fatalf("BriefLoadErr = %v (ok=%v line=%d), want line 3 naming unknownSection", loadErr, ok, le.Line)
	}
}

// #1026: Project rules (AGENTS.md) editing, undo, suggested rules from reverted reviews, and reviewable Keep/Revert.
func TestSettingsProjectRulesEditingUndoAndRevertedReviewSuggestions(t *testing.T) {
	dir := writeMinimalProjectForBrief(t)
	initial := "# AGENTS.md · how to work on Dungeon Run\n- Object ids are snake_case.\n- Never edit files under art/ by hand.\n"
	if err := os.WriteFile(filepath.Join(dir, editor.ProjectRulesFileName), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	// Seed 30 recent reviews where every change that moved the camera bounds was reverted.
	var outcomes []editor.ReviewOutcomeRecord
	for i := range 27 {
		outcomes = append(outcomes, editor.ReviewOutcomeRecord{
			ReviewID: "keep-" + string(rune('a'+i%26)),
			Action:   "placed enemy prefab",
			Kept:     true,
		})
	}
	for i := range 3 {
		outcomes = append(outcomes, editor.ReviewOutcomeRecord{
			ReviewID: "rev-" + string(rune('1'+i)),
			Action:   "moved the camera bounds",
			Kept:     false,
		})
	}
	ed.RecordReviewOutcomes(outcomes...)

	sugs := ed.SuggestedProjectRules()
	if len(sugs) != 1 {
		t.Fatalf("SuggestedProjectRules = %+v, want 1 suggestion", sugs)
	}
	if !strings.Contains(sugs[0].Reason, "last 30 reviews") || !strings.Contains(sugs[0].Reason, "moved the camera bounds") {
		t.Fatalf("unexpected suggestion reason: %q", sugs[0].Reason)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	app.Post(func() { ed.OpenRulesSettings() })
	app.Settle()

	if meta := app.Find("id:editor-settings-tab-rules-meta").Text; meta != "2" {
		t.Fatalf("rules count badge = %q, want 2", meta)
	}

	// Clicking "Add as a rule" creates a reviewable DocReview without modifying AGENTS.md on disk yet.
	app.Press("id:rules-suggestion-add-" + sugs[0].ID)
	pending := ed.PendingDocReviews(editor.DocReviewRules)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending rules review, got %d", len(pending))
	}
	diskBeforeKeep, err := os.ReadFile(filepath.Join(dir, editor.ProjectRulesFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(diskBeforeKeep) != initial {
		t.Fatalf("AGENTS.md modified before Keep:\n%s", diskBeforeKeep)
	}

	// Keeping the review writes the new rule to AGENTS.md as one undoable step.
	app.Press("id:doc-review-keep-" + pending[0].ID)
	diskAfterKeep, err := os.ReadFile(filepath.Join(dir, editor.ProjectRulesFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diskAfterKeep), "camera bounds") {
		t.Fatalf("AGENTS.md missing kept rule:\n%s", diskAfterKeep)
	}
	if ed.ProjectRulesCount() != 3 {
		t.Fatalf("ProjectRulesCount = %d, want 3", ed.ProjectRulesCount())
	}
	// Suggestion disappears once AGENTS.md covers it.
	if got := ed.SuggestedProjectRules(); len(got) != 0 {
		t.Fatalf("expected 0 remaining suggestions after adding rule, got %+v", got)
	}

	// User can undo the kept rule cleanly, restoring AGENTS.md on disk.
	app.Post(func() { _ = ed.Undo() })
	app.Settle()
	diskAfterUndo, err := os.ReadFile(filepath.Join(dir, editor.ProjectRulesFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(diskAfterUndo) != initial {
		t.Fatalf("AGENTS.md after undo = %q, want %q", diskAfterUndo, initial)
	}

	// Manual plain Markdown edit via the code field + Save rules appends a 3rd rule and undoes cleanly.
	app.Type("id:rules-markdown-input", "- Keep floors under 400 objects.\n")
	app.Press("id:rules-save-btn")
	if ed.ProjectRulesCount() != 3 {
		t.Fatalf("after manual edit ProjectRulesCount = %d, want 3", ed.ProjectRulesCount())
	}
	app.Post(func() { _ = ed.Undo() })
	app.Settle()
	if ed.ProjectRulesCount() != 2 {
		t.Fatalf("after undoing manual edit ProjectRulesCount = %d, want 2", ed.ProjectRulesCount())
	}
}

// #1026: "Agent on this page" card suggested asks and Settings search bar dispatch requests with page context.
func TestSettingsAgentOnThisPageAndSearchDispatch(t *testing.T) {
	dir := writeMinimalProjectForBrief(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	var received []editor.SettingsAgentRequest
	ed.OnSettingsAgentRequest = func(req editor.SettingsAgentRequest) {
		received = append(received, req)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// 1. Click a suggested ask on Settings › Game brief.
	app.Post(func() { ed.OpenBriefSettings() })
	app.Settle()
	app.Press("id:settings-agent-suggestion-0")

	if len(received) != 1 {
		t.Fatalf("expected 1 SettingsAgentRequest, got %d", len(received))
	}
	if received[0].Page != "brief" || received[0].File != project.BriefFileName || received[0].Source != "suggestion" {
		t.Fatalf("suggestion request = %+v", received[0])
	}
	if !strings.Contains(ed.ComposerDraft(), "brief.yaml") {
		t.Fatalf("ComposerDraft() = %q, want brief.yaml context", ed.ComposerDraft())
	}

	// 2. Switch to Project rules and type an agent request into the top Settings search bar.
	app.Press("id:editor-settings-tab-rules")
	app.Type("id:settings-search", "make the game feel harder")
	app.Press("id:settings-search-ask")

	if len(received) != 2 {
		t.Fatalf("expected 2 SettingsAgentRequests, got %d", len(received))
	}
	if received[1].Page != "rules" || received[1].File != editor.ProjectRulesFileName || received[1].Source != "search" || received[1].Prompt != "make the game feel harder" {
		t.Fatalf("search request = %+v", received[1])
	}
	if !strings.Contains(ed.ComposerDraft(), "AGENTS.md") || !strings.Contains(ed.ComposerDraft(), "make the game feel harder") {
		t.Fatalf("ComposerDraft() = %q", ed.ComposerDraft())
	}
}

func TestBriefAndRulesSymlinkContainmentAndSizeCaps(t *testing.T) {
	outsideDir := t.TempDir()
	secretBrief := filepath.Join(outsideDir, "secret-brief.yaml")
	secretRules := filepath.Join(outsideDir, "secret-rules.md")
	const secretBriefBody = "version: 1\npitch: TOP_SECRET_OUTSIDE\n"
	const secretRulesBody = "# SECRET\n- Never leak this.\n"
	if err := os.WriteFile(secretBrief, []byte(secretBriefBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretRules, []byte(secretRulesBody), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := writeMinimalProjectForBrief(t)
	briefLink := filepath.Join(dir, project.BriefFileName)
	rulesLink := filepath.Join(dir, editor.ProjectRulesFileName)
	if err := os.Symlink(secretBrief, briefLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := os.Symlink(secretRules, rulesLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	if ed.BriefExists() {
		t.Fatal("symlinked brief.yaml must not be loaded")
	}
	if ed.BriefLoadErr() == nil {
		t.Fatal("expected BriefLoadErr for symlinked brief.yaml")
	}
	if ed.ProjectRulesExists() || ed.ProjectRules() != "" {
		t.Fatalf("symlinked AGENTS.md must not be loaded, got exists=%v content=%q", ed.ProjectRulesExists(), ed.ProjectRules())
	}
	if err := ed.SetBriefPitch("overwrite attempt"); err == nil {
		t.Fatal("EditBrief must refuse symlinked brief.yaml")
	}
	if err := ed.SetProjectRules("overwrite", "- hijack\n"); err == nil {
		t.Fatal("SetProjectRules must refuse symlinked AGENTS.md")
	}
	// External targets must be completely untouched.
	if got, _ := os.ReadFile(secretBrief); string(got) != secretBriefBody {
		t.Fatalf("external secretBrief modified: %q", got)
	}
	if got, _ := os.ReadFile(secretRules); string(got) != secretRulesBody {
		t.Fatalf("external secretRules modified: %q", got)
	}

	// Undo must also refuse to delete/overwrite if brief.yaml was replaced by a symlink after creation.
	dir2 := writeMinimalProjectForBrief(t)
	ed2, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed2.OpenProject(dir2); err != nil {
		t.Fatal(err)
	}
	ed2.WaitForProblems()
	if err := ed2.SetBriefPitch("Legit pitch"); err != nil {
		t.Fatal(err)
	}
	realBriefPath := filepath.Join(dir2, project.BriefFileName)
	if err := os.Remove(realBriefPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretBrief, realBriefPath); err != nil {
		t.Fatal(err)
	}
	ed2.Undo()
	// Symlink must still exist (not unlinked via os.Remove) and secret target untouched.
	if fi, err := os.Lstat(realBriefPath); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Undo should have refused to remove symlinked brief.yaml (err=%v)", err)
	}
	if got, _ := os.ReadFile(secretBrief); string(got) != secretBriefBody {
		t.Fatalf("external secretBrief modified on Undo: %q", got)
	}

	// Oversized (> 1 MiB) files on disk and in-memory mutations are refused.
	dir3 := writeMinimalProjectForBrief(t)
	huge := "version: 1\npitch: " + strings.Repeat("a", (1<<20)+128) + "\n"
	if err := os.WriteFile(filepath.Join(dir3, project.BriefFileName), []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir3, editor.ProjectRulesFileName), []byte(strings.Repeat("x", (1<<20)+128)), 0o644); err != nil {
		t.Fatal(err)
	}
	ed3, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed3.OpenProject(dir3); err != nil {
		t.Fatal(err)
	}
	ed3.WaitForProblems()
	if ed3.BriefExists() || ed3.BriefLoadErr() == nil {
		t.Fatalf("expected oversized brief.yaml to be rejected, exists=%v err=%v", ed3.BriefExists(), ed3.BriefLoadErr())
	}
	if ed3.ProjectRulesExists() || ed3.ProjectRules() != "" {
		t.Fatalf("expected oversized AGENTS.md to be rejected, exists=%v len=%d", ed3.ProjectRulesExists(), len(ed3.ProjectRules()))
	}
	if err := ed3.SetProjectRules("huge", strings.Repeat("r", (1<<20)+64)); err == nil {
		t.Fatal("SetProjectRules accepted > 1 MiB payload")
	}
}

func TestBriefAndRulesOpenDemoResetAndAllSettingsPageContexts(t *testing.T) {
	dirA := writeMinimalProjectForBrief(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dirA); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	if err := ed.SetBriefPitch("Project A pitch"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectRules("init", "- Rule from project A.\n"); err != nil {
		t.Fatal(err)
	}
	ed.RecordReviewOutcome(editor.ReviewOutcomeRecord{Action: "moved the camera bounds", Kept: false})
	if _, err := ed.ProposeProjectRulesReview("- Rule from project A.\n- Pending rule.\n", "add", "", "agent"); err != nil {
		t.Fatal(err)
	}
	ed.OpenBriefSettings()
	ed.AskAgentFromSettings("page_composer", "check pitch")

	// Verify SettingsPageContext covers all 8 unified Settings pages.
	pages := []struct {
		page editor.SettingsPage
		id   string
		file string
	}{
		{editor.SettingsPageProject, "project", project.FileName},
		{editor.SettingsPageInput, "input", project.FileName},
		{editor.SettingsPageAudio, "audio", project.FileName},
		{editor.SettingsPageExport, "export", project.FileName},
		{editor.SettingsPagePermissions, "permissions", editor.PermissionsFileName},
		{editor.SettingsPageBrief, "brief", project.BriefFileName},
		{editor.SettingsPageRules, "rules", editor.ProjectRulesFileName},
		{editor.SettingsPageShortcuts, "shortcuts", ""},
		{editor.SettingsPageGeneral, "general", ""},
	}
	for _, tc := range pages {
		ed.OpenSettingsPage(tc.page)
		gotID, gotTitle, gotScope, gotFile := ed.SettingsPageContext()
		if gotID != tc.id || gotTitle == "" || gotScope == "" || gotFile != tc.file {
			t.Fatalf("SettingsPageContext(%s) = (%q, %q, %q, %q), want id=%q file=%q", tc.page, gotID, gotTitle, gotScope, gotFile, tc.id, tc.file)
		}
	}

	origBriefBytes, err := os.ReadFile(filepath.Join(dirA, project.BriefFileName))
	if err != nil {
		t.Fatal(err)
	}
	origRulesBytes, err := os.ReadFile(filepath.Join(dirA, editor.ProjectRulesFileName))
	if err != nil {
		t.Fatal(err)
	}

	// Switching to OpenDemo must completely reset all agentDocs state and detach Project A paths.
	if err := ed.OpenDemo(); err != nil {
		t.Fatal(err)
	}
	if ed.BriefExists() || ed.BriefPath() != "" {
		t.Fatalf("after OpenDemo: BriefExists=%v BriefPath=%q", ed.BriefExists(), ed.BriefPath())
	}
	if ed.ProjectRulesExists() || ed.ProjectRulesPath() != "" || ed.ProjectRules() != "" {
		t.Fatalf("after OpenDemo: rulesExists=%v path=%q rules=%q", ed.ProjectRulesExists(), ed.ProjectRulesPath(), ed.ProjectRules())
	}
	if len(ed.PendingDocReviews("")) != 0 || len(ed.ReviewOutcomes()) != 0 || ed.LastSettingsAgentRequest().Prompt != "" {
		t.Fatalf("after OpenDemo: stale reviews=%d outcomes=%d lastReq=%+v", len(ed.PendingDocReviews("")), len(ed.ReviewOutcomes()), ed.LastSettingsAgentRequest())
	}

	// Editing brief/rules while in Demo must NEVER write to Project A's directory.
	if err := ed.SetBriefPitch("Demo-only pitch"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectRules("demo", "- Demo rule.\n"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dirA, project.BriefFileName)); !bytes.Equal(got, origBriefBytes) {
		t.Fatalf("Demo edit overwrote Project A brief.yaml:\n%s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dirA, editor.ProjectRulesFileName)); !bytes.Equal(got, origRulesBytes) {
		t.Fatalf("Demo edit overwrote Project A AGENTS.md:\n%s", got)
	}
}

func TestBriefAndRulesPlayModeAndMergeConflictGuards(t *testing.T) {
	dir := writeMinimalProjectForBrief(t)
	ed, err := editor.New(editor.DemoSceneDoc(), editor.DemoAtlasMap())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if err := ed.SetBriefPitch("Guard test pitch"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetProjectRules("init", "- Guard rule.\n"); err != nil {
		t.Fatal(err)
	}
	ed.RecordReviewOutcomes(
		editor.ReviewOutcomeRecord{Action: "moved the camera bounds", Kept: false},
		editor.ReviewOutcomeRecord{Action: "moved the camera bounds", Kept: false},
	)
	sug := ed.SuggestedProjectRules()
	if len(sug) == 0 {
		t.Fatal("expected suggested rule")
	}
	pending, err := ed.ProposeProjectRulesReview("- Guard rule.\n- Proposed.\n", "add", "", "agent")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Play mode guards.
	var rootScope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	var playGuardErr string
	app.Post(func() {
		if err := ed.PlayIn(rootScope); err != nil {
			playGuardErr = "PlayIn: " + err.Error()
			return
		}
		if err := ed.SetBriefPitch("blocked"); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "EditBrief during Play != ErrPlaying"
			return
		}
		if err := ed.SaveBrief(); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "SaveBrief during Play != ErrPlaying"
			return
		}
		if err := ed.ReloadBrief(); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "ReloadBrief during Play != ErrPlaying"
			return
		}
		if err := ed.SetProjectRules("blocked", "x"); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "SetProjectRules during Play != ErrPlaying"
			return
		}
		if err := ed.SaveProjectRules(); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "SaveProjectRules during Play != ErrPlaying"
			return
		}
		if err := ed.ReloadProjectRules(); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "ReloadProjectRules during Play != ErrPlaying"
			return
		}
		if _, err := ed.ProposeBriefReview(&project.Brief{Version: 1, Pitch: "x"}, "", "", ""); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "ProposeBriefReview during Play != ErrPlaying"
			return
		}
		if _, err := ed.ProposeProjectRulesReview("x", "", "", ""); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "ProposeProjectRulesReview during Play != ErrPlaying"
			return
		}
		if _, err := ed.ProposeSuggestedRule(sug[0].ID); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "ProposeSuggestedRule during Play != ErrPlaying"
			return
		}
		if err := ed.KeepDocReview(pending.ID); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "KeepDocReview during Play != ErrPlaying"
			return
		}
		if err := ed.RevertDocReview(pending.ID); !errors.Is(err, editor.ErrPlaying) {
			playGuardErr = "RevertDocReview during Play != ErrPlaying"
			return
		}
		if req := ed.AskAgentFromSettings("search", "blocked"); req.Prompt != "" {
			playGuardErr = "AskAgentFromSettings during Play should return empty"
			return
		}
		ed.DismissSuggestedRule(sug[0].ID)
		if len(ed.SuggestedProjectRules()) == 0 {
			playGuardErr = "DismissSuggestedRule during Play must be a no-op"
			return
		}
		if err := ed.Stop(); err != nil {
			playGuardErr = "Stop: " + err.Error()
		}
	})
	app.Settle()
	if playGuardErr != "" {
		t.Fatal(playGuardErr)
	}

	// 2. Merge conflict guards.
	baseDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "c",
		Layers: []sceneformat.LayerDoc{{
			Name: "main",
			Objects: []sceneformat.ObjectDoc{
				{ID: "hero", Name: "Hero", X: 10, Y: 10},
			},
		}},
	}
	mineDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "c",
		Layers: []sceneformat.LayerDoc{{
			Name: "main",
			Objects: []sceneformat.ObjectDoc{
				{ID: "hero", Name: "Hero", X: 20, Y: 10},
			},
		}},
	}
	theirsDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "c",
		Layers: []sceneformat.LayerDoc{{
			Name: "main",
			Objects: []sceneformat.ObjectDoc{
				{ID: "hero", Name: "Hero", X: 30, Y: 10},
			},
		}},
	}
	var mergeGuardErr string
	app.Post(func() {
		if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
			ScenePath: "scenes/main.scene.yaml",
			BaseDoc:   baseDoc,
			MineDoc:   mineDoc,
			TheirsDoc: theirsDoc,
		}); err != nil {
			mergeGuardErr = "OpenMergeConflictSession: " + err.Error()
			return
		}
		if err := ed.SetBriefPitch("blocked"); !errors.Is(err, editor.ErrMergeConflictInProgress) {
			mergeGuardErr = "EditBrief during merge != ErrMergeConflictInProgress"
			return
		}
		if err := ed.SetProjectRules("blocked", "x"); !errors.Is(err, editor.ErrMergeConflictInProgress) {
			mergeGuardErr = "SetProjectRules during merge != ErrMergeConflictInProgress"
			return
		}
		if _, err := ed.ProposeBriefReview(&project.Brief{Version: 1, Pitch: "x"}, "", "", ""); !errors.Is(err, editor.ErrMergeConflictInProgress) {
			mergeGuardErr = "ProposeBriefReview during merge != ErrMergeConflictInProgress"
			return
		}
		if err := ed.KeepDocReview(pending.ID); !errors.Is(err, editor.ErrMergeConflictInProgress) {
			mergeGuardErr = "KeepDocReview during merge != ErrMergeConflictInProgress"
			return
		}
		if err := ed.RevertDocReview(pending.ID); !errors.Is(err, editor.ErrMergeConflictInProgress) {
			mergeGuardErr = "RevertDocReview during merge != ErrMergeConflictInProgress"
			return
		}
		if req := ed.AskAgentFromSettings("search", "blocked"); req.Prompt != "" {
			mergeGuardErr = "AskAgentFromSettings during merge should return empty"
		}
	})
	app.Settle()
	if mergeGuardErr != "" {
		t.Fatal(mergeGuardErr)
	}
}

func TestBriefAndRulesDraftStabilityAndThemeRepaint(t *testing.T) {
	dir := writeMinimalProjectForBrief(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	ed.RecordReviewOutcomes(
		editor.ReviewOutcomeRecord{Action: "moved the camera bounds", Kept: false},
		editor.ReviewOutcomeRecord{Action: "moved the camera bounds", Kept: false},
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// 1. Unsaved brief draft must survive incoming DocReviews and form validation errors.
	app.Post(func() { ed.OpenBriefSettings() })
	app.Settle()
	app.Type("id:brief-pitch-input", "Unsaved draft pitch")
	app.Post(func() {
		_, _ = ed.ProposeBriefReview(&project.Brief{Version: 1, Pitch: "Agent proposal"}, "agent pitch", "", "agent")
	})
	app.Settle()
	// Trigger target validation error (empty metric).
	app.Press("id:brief-target-add-btn")
	if app.Find("id:brief-form-error") == nil {
		t.Fatal("expected brief-form-error to appear")
	}
	// Pressing Save brief should persist our preserved unsaved draft, not an empty wiped field!
	app.Press("id:brief-save-btn")
	if b := ed.Brief(); b == nil || b.Pitch != "Unsaved draft pitch" {
		t.Fatalf("unsaved brief draft was wiped before save; got brief=%+v", b)
	}

	// 2. Unsaved rules draft must survive incoming DocReviews and dismissing suggestions.
	app.Press("id:editor-settings-tab-rules")
	app.Type("id:rules-markdown-input", "- Unsaved custom rule.\n")
	app.Post(func() {
		_, _ = ed.ProposeProjectRulesReview("- Agent rule.\n", "agent rule", "", "agent")
		for _, s := range ed.SuggestedProjectRules() {
			ed.DismissSuggestedRule(s.ID)
		}
	})
	app.Settle()
	app.Press("id:rules-save-btn")
	if !strings.Contains(ed.ProjectRules(), "Unsaved custom rule.") {
		t.Fatalf("unsaved rules draft was wiped before save; got %q", ed.ProjectRules())
	}

	// 3. Live Dark -> Light Spec appearance switch repaints settings-agent-card.
	cardDark := app.Find("id:settings-agent-card")
	if cardDark == nil {
		t.Fatal("settings-agent-card missing in dark theme")
	}
	darkBG := cardDark.Style.Background
	if darkBG != theme.PaletteFor(true).Panel {
		t.Fatalf("dark settings-agent-card bg = %#x, want %#x", uint32(darkBG), uint32(theme.PaletteFor(true).Panel))
	}
	app.Post(func() { _ = ed.SetAppearance(editor.AppearanceLight) })
	app.Settle()
	cardLight := app.Find("id:settings-agent-card")
	if cardLight == nil {
		t.Fatal("settings-agent-card missing in light theme")
	}
	if cardLight.Style.Background != theme.PaletteFor(false).Panel {
		t.Fatalf("light settings-agent-card bg = %#x, want %#x", uint32(cardLight.Style.Background), uint32(theme.PaletteFor(false).Panel))
	}
}

func TestBriefArtDirectionSyncsAssetGeneratorStyleBrief(t *testing.T) {
	dir := writeMinimalProjectForBrief(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	const art = "16 px pixel art with warm torch light and cool stone shadows."
	if err := ed.SetBriefArtDirection(art); err != nil {
		t.Fatalf("SetBriefArtDirection: %v", err)
	}
	if got := ed.AssetStyleBrief().Summary; got != art {
		t.Fatalf("AssetStyleBrief().Summary = %q, want %q", got, art)
	}
	if !ed.Undo() {
		t.Fatal("expected Undo to succeed")
	}
	if got := ed.AssetStyleBrief().Summary; got != editor.DefaultAssetStyleBrief().Summary {
		t.Fatalf("after Undo AssetStyleBrief().Summary = %q, want %q", got, editor.DefaultAssetStyleBrief().Summary)
	}
}
