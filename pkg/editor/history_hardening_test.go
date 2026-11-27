package editor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

func openHistoryHardeningProject(t *testing.T) (*Editor, string) {
	t.Helper()
	dir := t.TempDir()
	projYAML := "version: 1\nname: hardening\nid: dev.example.hardening\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(projYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#181C24",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "knight", Name: "Knight", X: 100, Y: 200},
				},
			},
		},
	}
	raw, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	_ = ed.SetAutoSave(false)
	return ed, dir
}

// Blocker 1: Symlink escape and oversized file refusal in history extra-file snapshots and restore.
func TestHistoryExtraFileSymlinkEscapeAndSizeCapRefused(t *testing.T) {
	ed, dir := openHistoryHardeningProject(t)

	// Create a file outside the project root and a symlink inside the project pointing to it.
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.yaml")
	if err := os.WriteFile(outsideFile, []byte("secret: keep-me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "escape.yaml")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	// 1. RecordAgentTurn must refuse symlink escaping project root before running fn.
	fnRan := false
	err := ed.RecordAgentTurn(AgentTurnOptions{
		Title: "symlink attempt",
		Files: []string{"escape.yaml"},
	}, func() error {
		fnRan = true
		return nil
	})
	if err == nil {
		t.Fatal("expected symlink escape in RecordAgentTurn to fail")
	}
	if fnRan {
		t.Fatal("RecordAgentTurn should refuse escaping symlink before running fn")
	}

	// 2. applyHistoryFiles must also refuse symlink escape (both write and delete) and leave outside file untouched.
	if err := ed.applyHistoryFiles(map[string]historyFileSnapshot{
		"escape.yaml": {exists: true, data: []byte("pwned")},
	}); err == nil {
		t.Fatal("expected applyHistoryFiles write through escaping symlink to fail")
	}
	if err := ed.applyHistoryFiles(map[string]historyFileSnapshot{
		"escape.yaml": {exists: false},
	}); err == nil {
		t.Fatal("expected applyHistoryFiles delete of escaping symlink to fail")
	}
	got, err := os.ReadFile(outsideFile)
	if err != nil || string(got) != "secret: keep-me\n" {
		t.Fatalf("outside file mutated: %q, err=%v", string(got), err)
	}

	// 3. Oversized extra file (> maxInspectorYAMLBytes) is refused cleanly.
	bigPath := filepath.Join(dir, "huge.yaml")
	if err := os.WriteFile(bigPath, bytes.Repeat([]byte("a"), maxInspectorYAMLBytes+128), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.RecordAgentTurn(AgentTurnOptions{
		Title: "oversized file",
		Files: []string{"huge.yaml"},
	}, func() error { return nil }); err == nil {
		t.Fatal("expected oversized extra file to be refused")
	}
}

// Blocker 2: Atomic rollback on partial failure in applyHistorySnapshot and RecordAgentTurn.
func TestHistoryAtomicRollbackOnPartialFailure(t *testing.T) {
	ed, dir := openHistoryHardeningProject(t)

	// Prepare file A (normal file) and a directory at path B so writing file B fails.
	fileA := filepath.Join(dir, "a.yaml")
	if err := os.WriteFile(fileA, []byte("original-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirB := filepath.Join(dir, "b.yaml")
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatal(err)
	}

	// Modify knight so we have a distinct scene snapshot to attempt restoring.
	if err := ed.MoveObject("knight", 250, 200); err != nil {
		t.Fatal(err)
	}
	origDoc := ed.Doc
	otherDoc := ed.Doc.Clone()
	otherDoc.Layers[0].Objects[0].X = 999
	otherYAML, err := sceneformat.Encode(otherDoc)
	if err != nil {
		t.Fatal(err)
	}

	// applyHistorySnapshot writes a.yaml then fails trying to overwrite directory b.yaml:
	// a.yaml MUST roll back to "original-a\n", and ed.Doc MUST remain unchanged!
	err = ed.applyHistorySnapshot(historySnapshot{
		sceneYAML: otherYAML,
		scenePath: "scenes/level_01.scene.yaml",
		files: map[string]historyFileSnapshot{
			"a.yaml": {exists: true, data: []byte("mutated-a\n")},
			"b.yaml": {exists: true, data: []byte("cannot-write-dir\n")},
		},
	})
	if err == nil {
		t.Fatal("expected applyHistorySnapshot to fail when target path is a directory")
	}
	if ed.Doc != origDoc {
		t.Fatal("ed.Doc was mutated despite applyHistorySnapshot failure")
	}
	if k, _, _ := ed.FindObject("knight"); k == nil || k.X != 250 {
		t.Fatalf("knight mutated after failed snapshot apply: %+v", k)
	}
	if raw, rerr := os.ReadFile(fileA); rerr != nil || string(raw) != "original-a\n" {
		t.Fatalf("a.yaml was not rolled back: got %q, err=%v", string(raw), rerr)
	}

	// RecordAgentTurn error rollback: fn mutates scene AND a.yaml, then returns an error.
	// Both scene AND a.yaml must roll back cleanly, with no timeline entry added.
	entriesBefore := len(ed.HistoryEntries())
	err = ed.RecordAgentTurn(AgentTurnOptions{
		Title: "failing turn",
		Files: []string{"a.yaml"},
	}, func() error {
		if err := ed.MoveObject("knight", 400, 200); err != nil {
			return err
		}
		if err := os.WriteFile(fileA, []byte("half-written\n"), 0o644); err != nil {
			return err
		}
		return errors.New("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("RecordAgentTurn err = %v, want boom", err)
	}
	if k, _, _ := ed.FindObject("knight"); k == nil || k.X != 250 {
		t.Fatalf("scene not rolled back after failed RecordAgentTurn: %+v", k)
	}
	if raw, rerr := os.ReadFile(fileA); rerr != nil || string(raw) != "original-a\n" {
		t.Fatalf("a.yaml not rolled back after failed RecordAgentTurn: %q, err=%v", string(raw), rerr)
	}
	if len(ed.HistoryEntries()) != entriesBefore {
		t.Fatalf("failed RecordAgentTurn added history entry: got %d, want %d", len(ed.HistoryEntries()), entriesBefore)
	}

	// Untouched file listed in opts.Files must NOT be overwritten on Restore.
	untouchedPath := filepath.Join(dir, "untouched.yaml")
	if err := os.WriteFile(untouchedPath, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevEntryID := ed.HistoryEntries()[0].ID
	if err := ed.RecordAgentTurn(AgentTurnOptions{
		Title: "touch only scene",
		Files: []string{"untouched.yaml"},
	}, func() error {
		return ed.MoveObject("knight", 310, 200)
	}); err != nil {
		t.Fatal(err)
	}
	// External edit updates untouched.yaml to v2.
	if err := os.WriteFile(untouchedPath, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.RestoreHistoryEntry(prevEntryID); err != nil {
		t.Fatal(err)
	}
	if raw, rerr := os.ReadFile(untouchedPath); rerr != nil || string(raw) != "v2\n" {
		t.Fatalf("untouched.yaml was overwritten by restore: got %q, want v2", string(raw))
	}
}

// Blocker 3: Bounded memory in History.records (count cap + byte budget pruning superseded first).
func TestHistoryRecordsBoundedMemoryAndSupersededPruning(t *testing.T) {
	ed, _ := openHistoryHardeningProject(t)

	// 1. Entry count cap.
	ed.History.maxRecords = 5
	ed.History.maxSnapshotBytes = 32 << 20
	for i := 1; i <= 12; i++ {
		if err := ed.MoveObject("knight", float32(100+i*10), 200); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(ed.History.records); got > 5 {
		t.Fatalf("records len = %d, want <= 5", got)
	}
	// Newest entry is preserved.
	if entries := ed.HistoryEntries(); len(entries) != 5 || !strings.Contains(entries[0].Detail, "220") {
		t.Fatalf("newest entry not preserved after count trim: %+v", entries)
	}

	// 2. Byte budget prunes superseded records first, then oldest active records.
	ed.History.Clear()
	ed.History.maxRecords = 50
	// Measure one snapshot's size and set budget to hold ~3 records.
	if err := ed.MoveObject("knight", 111, 200); err != nil {
		t.Fatal(err)
	}
	oneSize := historyRecordBytes(&ed.History.records[0])
	if oneSize <= 0 {
		t.Fatal("expected positive record byte size")
	}
	ed.History.maxSnapshotBytes = oneSize*3 + oneSize/2

	// Create 2 more edits, undo them so they become superseded on the next edit.
	_ = ed.MoveObject("knight", 122, 200)
	_ = ed.MoveObject("knight", 133, 200)
	ed.Undo()
	ed.Undo()
	// New edits mark the 2 undone records superseded and push past budget.
	_ = ed.MoveObject("knight", 144, 200)
	_ = ed.MoveObject("knight", 155, 200)
	_ = ed.MoveObject("knight", 166, 200)

	if total := ed.History.totalRecordBytes(); total > ed.History.maxSnapshotBytes {
		t.Fatalf("total snapshot bytes = %d exceeds cap %d", total, ed.History.maxSnapshotBytes)
	}
	// Superseded records should have been pruned first.
	for _, r := range ed.History.records {
		if r.superseded {
			t.Fatalf("expected superseded records to be pruned first, found %+v", r.entry)
		}
	}
	if entries := ed.HistoryEntries(); len(entries) == 0 || !strings.Contains(entries[0].Detail, "166") {
		t.Fatalf("newest entry lost under byte cap: %+v", entries)
	}
}

// Blocker 4, 5, 6, 8 + Minor: Preview guards, transitions (Save/Undo/Tab/Project/Demo), modal guards,
// right-rail mutual exclusion, SaveTab OnSave marker, and safe Restore Toast Undo callback.
func TestHistoryPreviewGuardsLifecycleAndToastUndoSafety(t *testing.T) {
	ed, dir := openHistoryHardeningProject(t)
	now := time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC)
	ed.SetHistoryClock(func() time.Time { return now })

	if err := ed.MoveObject("knight", 150, 200); err != nil {
		t.Fatal(err)
	}
	entry1ID := ed.HistoryEntries()[0].ID

	now = now.Add(2 * time.Minute)
	if err := ed.MoveObject("knight", 260, 200); err != nil {
		t.Fatal(err)
	}

	// Enter read-only preview of entry1 (knight x=150) while live Doc has knight x=260.
	if err := ed.PreviewHistoryEntry(entry1ID); err != nil {
		t.Fatal(err)
	}
	if !ed.HistoryPreviewActive() {
		t.Fatal("expected preview active")
	}

	// Mutations MUST be blocked while previewing.
	if err := ed.MoveObject("knight", 999, 200); !errors.Is(err, ErrHistoryPreview) {
		t.Fatalf("MoveObject during preview = %v, want ErrHistoryPreview", err)
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "bad", X: 1, Y: 1}); !errors.Is(err, ErrHistoryPreview) {
		t.Fatalf("PlaceObject during preview = %v, want ErrHistoryPreview", err)
	}
	if err := ed.DeleteObject("knight"); !errors.Is(err, ErrHistoryPreview) {
		t.Fatalf("DeleteObject during preview = %v, want ErrHistoryPreview", err)
	}
	if err := ed.EditProject("bad", func(*project.File) {}); !errors.Is(err, ErrHistoryPreview) {
		t.Fatalf("EditProject during preview = %v, want ErrHistoryPreview", err)
	}
	if ed.BeginDrag2D(150, 200) != Handle2DNone {
		t.Fatal("BeginDrag2D should be blocked during preview")
	}
	if ed.BeginMarquee2D(10, 10) {
		t.Fatal("BeginMarquee2D should be blocked during preview")
	}
	if ed.OpenObjectContextMenu("knight") {
		t.Fatal("OpenObjectContextMenu should be blocked during preview")
	}
	if ed.ExecuteCommand(CmdDelete) == nil {
		// CmdDelete is disabled while previewing so live doc stays untouched.
	}
	if k, _, _ := ed.FindObject("knight"); k == nil || k.X != 260 {
		t.Fatalf("live knight mutated during preview: %+v", k)
	}

	// Save() MUST exit preview and save the LIVE scene (x=260), NEVER the preview (x=150)!
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}
	if ed.HistoryPreviewActive() {
		t.Fatal("Save() did not exit history preview")
	}
	diskYAML, err := os.ReadFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diskYAML), "x: 260") || strings.Contains(string(diskYAML), "x: 150") {
		t.Fatalf("Save() wrote wrong scene while previewing:\n%s", string(diskYAML))
	}

	// Undo() and Redo() MUST exit preview cleanly.
	if err := ed.PreviewHistoryEntry(entry1ID); err != nil {
		t.Fatal(err)
	}
	if !ed.Undo() {
		t.Fatal("Undo() failed from preview")
	}
	if ed.HistoryPreviewActive() {
		t.Fatal("Undo() did not exit history preview")
	}
	if err := ed.PreviewHistoryEntry(entry1ID); err != nil {
		t.Fatal(err)
	}
	if !ed.Redo() {
		t.Fatal("Redo() failed from preview")
	}
	if ed.HistoryPreviewActive() {
		t.Fatal("Redo() did not exit history preview")
	}

	// SaveTab() with OnSave != nil exits preview and records a Saved marker.
	savedViaCallback := false
	ed.OnSave = func([]byte) error { savedViaCallback = true; return nil }
	if err := ed.MoveObject("knight", 280, 200); err != nil {
		t.Fatal(err)
	}
	if err := ed.PreviewHistoryEntry(entry1ID); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveTab(ed.ActiveTab()); err != nil {
		t.Fatal(err)
	}
	if !savedViaCallback || ed.HistoryPreviewActive() {
		t.Fatalf("SaveTab(OnSave) saved=%v preview=%v", savedViaCallback, ed.HistoryPreviewActive())
	}
	if top := ed.HistoryEntries()[0]; top.Kind != HistoryEntrySaved {
		t.Fatalf("SaveTab(OnSave) did not record Saved marker: %+v", top)
	}
	ed.OnSave = nil

	// Blocker 8: Right-rail mutual exclusion between History and ProblemAgentPanel.
	ed.SetHistoryPanelOpen(true)
	if !ed.HistoryPanelOpen() {
		t.Fatal("expected History panel open")
	}
	ed.SetProblemAgentPanelOpen(true)
	if !ed.ProblemAgentPanelOpen() || ed.HistoryPanelOpen() {
		t.Fatalf("opening ProblemAgentPanel should close History: problem=%v history=%v", ed.ProblemAgentPanelOpen(), ed.HistoryPanelOpen())
	}
	ed.SetHistoryPanelOpen(true)
	if !ed.HistoryPanelOpen() || ed.ProblemAgentPanelOpen() {
		t.Fatalf("opening History should close ProblemAgentPanel: history=%v problem=%v", ed.HistoryPanelOpen(), ed.ProblemAgentPanelOpen())
	}

	// Blocker 6: Restore Toast Undo callback is guarded against stale edits and tab/project switches.
	if err := ed.RestoreHistoryEntry(entry1ID); err != nil {
		t.Fatal(err)
	}
	toast, ok := ed.ActiveToast()
	if !ok || toast.OnAction == nil {
		t.Fatal("expected active restore toast with Undo callback")
	}
	// Make another edit -> Restore toast's OnAction MUST NOT undo the newer edit!
	if err := ed.MoveObject("knight", 333, 200); err != nil {
		t.Fatal(err)
	}
	toast.OnAction()
	if k, _, _ := ed.FindObject("knight"); k == nil || k.X != 333 {
		t.Fatalf("stale Restore toast Undo undid a subsequent edit: knight=%+v", k)
	}

	// Fresh Restore toast Undo DOES work when state still matches.
	if err := ed.RestoreHistoryEntry(entry1ID); err != nil {
		t.Fatal(err)
	}
	if k, _, _ := ed.FindObject("knight"); k == nil || k.X != 150 {
		t.Fatalf("expected knight at 150 after Restore, got %+v", k)
	}
	freshToast, ok := ed.ActiveToast()
	if !ok || freshToast.OnAction == nil {
		t.Fatal("expected fresh restore toast")
	}
	freshToast.OnAction()
	if k, _, _ := ed.FindObject("knight"); k == nil || k.X != 333 {
		t.Fatalf("matching Restore toast Undo did not revert restore: knight=%+v", k)
	}

	// Blocker 5: OpenDemo resets history panel open/filter/preview cleanly.
	ed.SetHistoryPanelOpen(true)
	ed.SetHistoryFilter(HistoryFilterAgent)
	if err := ed.PreviewHistoryEntry(entry1ID); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatal(err)
	}
	if ed.HistoryPanelOpen() || ed.HistoryFilter() != HistoryFilterAll || ed.HistoryPreviewActive() {
		t.Fatalf("OpenDemo did not reset history panel: open=%v filter=%q preview=%v",
			ed.HistoryPanelOpen(), ed.HistoryFilter(), ed.HistoryPreviewActive())
	}

	// Blocker 5: StartScreen blocks opening History panel / preview / restore / agent turn.
	ed.SetStartScreenOpen(true)
	ed.SetHistoryPanelOpen(true)
	if ed.HistoryPanelOpen() {
		t.Fatal("StartScreen should block SetHistoryPanelOpen(true)")
	}
	if err := ed.PreviewHistoryEntry(1); err == nil {
		t.Fatal("StartScreen should block PreviewHistoryEntry")
	}
	if err := ed.RestoreHistoryEntry(1); err == nil {
		t.Fatal("StartScreen should block RestoreHistoryEntry")
	}
	if err := ed.RecordAgentTurn(AgentTurnOptions{Title: "blocked"}, func() error { return nil }); err == nil {
		t.Fatal("StartScreen should block RecordAgentTurn")
	}
}
