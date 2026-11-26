package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/project"
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
