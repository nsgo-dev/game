package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAutoSaveSettingsRoundTripAndUI(t *testing.T) {
	// Omitting autoSave defaults to enabled and round-trips without injecting the key.
	omitted := []byte("# yaml-language-server: $schema=https://game.nsgo.dev/schemas/editor-settings.v1.json\nversion: 1\nlanguage: system\nappearance: system\n")
	parsed, err := editor.ParseEditorSettings(omitted)
	if err != nil {
		t.Fatalf("ParseEditorSettings(omitted): %v", err)
	}
	if !parsed.AutoSaveEnabled() || parsed.AutoSave != nil {
		t.Fatalf("omitted autoSave = %v (enabled=%v), want nil / enabled", parsed.AutoSave, parsed.AutoSaveEnabled())
	}
	reencoded, err := editor.EncodeEditorSettings(parsed)
	if err != nil {
		t.Fatalf("EncodeEditorSettings(omitted): %v", err)
	}
	if !bytes.Equal(reencoded, omitted) {
		t.Fatalf("omitted round-trip changed:\ngot:\n%s\nwant:\n%s", reencoded, omitted)
	}

	// Explicit false and true values round-trip byte-for-byte.
	for _, want := range []bool{false, true} {
		v := want
		s := editor.DefaultEditorSettings()
		s.AutoSave = &v
		enc, err := editor.EncodeEditorSettings(&s)
		if err != nil {
			t.Fatalf("EncodeEditorSettings(%v): %v", want, err)
		}
		got, err := editor.ParseEditorSettings(enc)
		if err != nil {
			t.Fatalf("ParseEditorSettings(%v): %v", want, err)
		}
		if got.AutoSave == nil || *got.AutoSave != want || got.AutoSaveEnabled() != want {
			t.Fatalf("round-trip(%v) = %v (enabled=%v)", want, got.AutoSave, got.AutoSaveEnabled())
		}
		again, err := editor.EncodeEditorSettings(got)
		if err != nil {
			t.Fatalf("re-encode(%v): %v", want, err)
		}
		if !bytes.Equal(again, enc) {
			t.Fatalf("re-encode(%v) changed:\ngot:\n%s\nwant:\n%s", want, again, enc)
		}
	}

	// Toggling General › Save automatically in EditorSettingsDialogView persists to SettingsPath.
	ed, dir := openTestProject(t)
	settingsPath := filepath.Join(dir, "user-settings.yaml")
	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if !ed.AutoSave() {
		t.Fatal("AutoSave() should default to true")
	}
	ed.SetEditorSettingsOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))
	app.Settle()

	if !app.Exists("id:editor-settings-autosave-switch") {
		t.Fatal("missing editor-settings-autosave-switch in Settings › General")
	}
	app.Press("id:editor-settings-autosave-switch")
	app.Settle()
	if ed.AutoSave() {
		t.Fatal("pressing Save automatically switch did not disable AutoSave")
	}
	disk, err := editor.LoadEditorSettings(settingsPath)
	if err != nil {
		t.Fatalf("LoadEditorSettings: %v", err)
	}
	if disk.AutoSave == nil || *disk.AutoSave {
		t.Fatalf("persisted autoSave = %v, want false", disk.AutoSave)
	}
}

func TestAutoSaveKeptChangeAfterTwoSecondsAndNoSavingFlickerUnder300ms(t *testing.T) {
	if editor.DefaultAutoSaveDelay != 2*time.Second {
		t.Fatalf("DefaultAutoSaveDelay = %v, want 2s", editor.DefaultAutoSaveDelay)
	}
	if editor.DefaultSavingIndicatorDelay != 300*time.Millisecond {
		t.Fatalf("DefaultSavingIndicatorDelay = %v, want 300ms", editor.DefaultSavingIndicatorDelay)
	}

	ed, dir := openTestProject(t)
	now := time.Date(2026, 11, 26, 14, 32, 6, 0, time.UTC)
	ed.Now = func() time.Time { return now }

	var seenKinds []editor.SaveStatusKind
	unsub := ed.Subscribe(func() {
		seenKinds = append(seenKinds, ed.SaveStatus().Kind)
	})
	t.Cleanup(unsub)

	scenePath := filepath.Join(dir, "scenes", "room.yaml")
	beforeDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := ed.MoveObject("hero", 42, 84); err != nil {
		t.Fatal(err)
	}
	if !ed.SceneDirty() || !ed.AutoSavePending() {
		t.Fatalf("after edit: dirty=%v pending=%v, want true/true", ed.SceneDirty(), ed.AutoSavePending())
	}

	// Just before 2 s: must NOT have written to disk yet.
	now = now.Add(1999 * time.Millisecond)
	ed.StepAutoSave()
	stillBefore, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stillBefore, beforeDisk) || !ed.SceneDirty() {
		t.Fatal("autosave wrote before 2 s elapsed")
	}

	// At exact 2 s: autosave runs and completes well under 300 ms.
	now = now.Add(1 * time.Millisecond) // 14:32:08
	ed.StepAutoSave()
	ed.WaitAutoSave()

	if ed.SceneDirty() || ed.HasUnsavedChanges() {
		t.Fatalf("after 2 s autosave: dirty=%v unsaved=%v, want false", ed.SceneDirty(), ed.HasUnsavedChanges())
	}
	afterDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	wantDisk, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterDisk, wantDisk) {
		t.Fatalf("disk content does not match edited scene:\ngot:\n%s\nwant:\n%s", afterDisk, wantDisk)
	}
	st := ed.SaveStatus()
	if st.Kind != editor.SaveStatusSaved || !st.HasSaved || st.LastSavedAt.Format("15:04:05") != "14:32:08" {
		t.Fatalf("SaveStatus = %+v, want Saved at 14:32:08", st)
	}
	if tip := ed.SaveStatusTooltip(); !strings.Contains(tip, "14:32:08") {
		t.Fatalf("SaveStatusTooltip() = %q, want time 14:32:08", tip)
	}
	for _, k := range seenKinds {
		if k == editor.SaveStatusSaving {
			t.Fatalf("quick save under 300 ms flickered to Saving… (observed kinds: %v)", seenKinds)
		}
	}
}

func TestAutoSaveShowsSavingOnlyAfter300ms(t *testing.T) {
	ed, dir := openTestProject(t)
	now := time.Date(2026, 11, 26, 14, 32, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }

	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	var once sync.Once
	ed.SaveFileWriter = func(path string, data []byte) error {
		once.Do(func() { close(writeStarted) })
		<-releaseWrite
		return os.WriteFile(path, data, 0o644)
	}

	if err := ed.MoveObject("hero", 91, 52); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	ed.StepAutoSave()
	<-writeStarted

	// At 299 ms in flight: still must NOT show Saving….
	now = now.Add(299 * time.Millisecond)
	ed.StepAutoSave()
	if got := ed.SaveStatus().Kind; got != editor.SaveStatusSaved {
		t.Fatalf("at 299 ms in flight SaveStatus.Kind = %v, want saved (no flicker)", got)
	}

	// At 300 ms in flight: Saving… appears.
	now = now.Add(1 * time.Millisecond)
	ed.StepAutoSave()
	if got := ed.SaveStatus().Kind; got != editor.SaveStatusSaving {
		t.Fatalf("at 300 ms in flight SaveStatus.Kind = %v, want saving", got)
	}

	// Release write and complete.
	close(releaseWrite)
	ed.WaitAutoSave()
	if got := ed.SaveStatus().Kind; got != editor.SaveStatusSaved {
		t.Fatalf("after completion SaveStatus.Kind = %v, want saved", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "scenes", "room.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "x: 91") {
		t.Fatalf("saved scene missing updated hero x=91:\n%s", data)
	}
}

func TestAutoSaveSkipsEditsInsideOpenReviewUntilKept(t *testing.T) {
	ed, dir := openTestProject(t)
	now := time.Date(2026, 11, 26, 14, 35, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }

	scenePath := filepath.Join(dir, "scenes", "room.yaml")
	beforeDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}

	// Open a review and edit inside it: must NOT autosave while the review is open.
	ed.BeginReview()
	if !ed.HasOpenReview() {
		t.Fatal("HasOpenReview() = false after BeginReview()")
	}
	if err := ed.MoveObject("hero", 123, 234); err != nil {
		t.Fatal(err)
	}
	if ed.AutoSavePending() {
		t.Fatal("autosave timer must not arm while a review is open")
	}
	now = now.Add(10 * time.Second)
	ed.StepAutoSave()
	ed.WaitAutoSave()

	duringDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(duringDisk, beforeDisk) {
		t.Fatal("unkept edits inside an open review were written to disk")
	}

	// Keeping the review arms the 2 s autosave timer and persists after 2 s.
	ed.KeepReview()
	if ed.HasOpenReview() || !ed.AutoSavePending() {
		t.Fatalf("after KeepReview: HasOpenReview=%v AutoSavePending=%v", ed.HasOpenReview(), ed.AutoSavePending())
	}
	now = now.Add(1999 * time.Millisecond)
	ed.StepAutoSave()
	if !ed.SceneDirty() {
		t.Fatal("kept review edits saved before 2 s")
	}
	now = now.Add(1 * time.Millisecond)
	ed.StepAutoSave()
	ed.WaitAutoSave()
	if ed.SceneDirty() {
		t.Fatal("kept review edits were not saved after 2 s")
	}
	afterDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(afterDisk), "x: 123") {
		t.Fatalf("saved scene missing kept hero x=123:\n%s", afterDisk)
	}
}

func TestAutoSaveReadOnlyFolderFailureRedDotQuitGuardAndRetry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores read-only directory permissions")
	}
	ed, dir := openTestProject(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	ed.AutoSaveDelay = 40 * time.Millisecond
	ed.SavingIndicatorDelay = 300 * time.Millisecond

	scenesDir := filepath.Join(dir, "scenes")
	t.Cleanup(func() { _ = os.Chmod(scenesDir, 0o755) })

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	if !app.Exists("id:status-save-saved") {
		t.Fatal("expected status-save-saved initially")
	}

	// Make scenes/ read-only so atomic temp-file creation fails with permission denied.
	if err := os.Chmod(scenesDir, 0o555); err != nil {
		t.Fatalf("chmod read-only: %v", err)
	}

	app.Post(func() {
		if err := ed.MoveObject("hero", 77, 88); err != nil {
			t.Errorf("MoveObject: %v", err)
		}
	})
	app.Settle()

	// Wait for the real 40 ms autosave timer + background write to fail and post back.
	app.WaitUntil(3*time.Second, func() bool {
		return ed.SaveStatus().Kind == editor.SaveStatusFailed
	})
	app.Settle()

	st := ed.SaveStatus()
	if st.Kind != editor.SaveStatusFailed || st.ErrorKind != editor.SaveErrorReadOnly || !st.AutoSaveFailure {
		t.Fatalf("SaveStatus = %+v, want failed read_only autosave", st)
	}
	if st.ErrorMessage != "Couldn't save: folder is read-only" {
		t.Fatalf("ErrorMessage = %q, want %q", st.ErrorMessage, "Couldn't save: folder is read-only")
	}
	if !app.Exists("id:status-save-error") || !app.Exists("id:status-save-retry") || !app.Exists("id:status-save-as") {
		t.Fatal("failed autosave controls missing from status bar")
	}
	if got := app.Find("id:status-save-error-text").Text; got != "Couldn't save: folder is read-only" {
		t.Fatalf("status-save-error-text = %q, want read-only message", got)
	}
	// Unsaved dot beside scene name stays visible in problem-red failed state, and quitting asks first.
	if !app.Exists("id:toolbar-unsaved") {
		t.Fatal("toolbar-unsaved dot missing while save fails")
	}
	dot := app.Find("id:toolbar-unsaved")
	if dot.Props.Label != "Unsaved changes · save failed" {
		t.Fatalf("toolbar-unsaved label = %q, want save-failed label", dot.Props.Label)
	}
	if dot.Style.Background != core.RGB(0xFF6B6B) {
		t.Fatalf("toolbar-unsaved background = %#v, want Spec problem red #FF6B6B", dot.Style.Background)
	}
	if !ed.HasUnsavedChanges() {
		t.Fatal("HasUnsavedChanges() must remain true while saving fails so quitting asks first")
	}

	// Restore folder permissions and click Retry in the status bar.
	if err := os.Chmod(scenesDir, 0o755); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	app.Press("id:status-save-retry")
	app.Settle()

	if ed.SaveStatus().Kind != editor.SaveStatusSaved || ed.SaveFailed() || ed.SceneDirty() || ed.HasUnsavedChanges() {
		t.Fatalf("after Retry: status=%+v failed=%v dirty=%v unsaved=%v", ed.SaveStatus(), ed.SaveFailed(), ed.SceneDirty(), ed.HasUnsavedChanges())
	}
	if app.Exists("id:status-save-error") || !app.Exists("id:status-save-saved") {
		t.Fatal("status bar did not return to Saved after Retry")
	}
	data, err := os.ReadFile(filepath.Join(scenesDir, "room.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "x: 77") {
		t.Fatalf("retried scene file missing hero x=77:\n%s", data)
	}
}

func TestAutoSaveDiskFullFormatsFreeSpaceAndSaveAsRecovers(t *testing.T) {
	ed, dir := openTestProject(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	ed.AutoSaveDelay = 30 * time.Millisecond
	ed.SaveFileWriter = func(path string, _ []byte) error {
		return &os.PathError{Op: "write", Path: path, Err: syscall.ENOSPC}
	}
	ed.FreeSpaceBytes = func(string) (uint64, bool) {
		return 12 * 1024 * 1024, true
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	app.Post(func() {
		if err := ed.MoveObject("hero", 55, 66); err != nil {
			t.Errorf("MoveObject: %v", err)
		}
	})
	app.WaitUntil(3*time.Second, func() bool {
		return ed.SaveStatus().Kind == editor.SaveStatusFailed
	})
	app.Settle()

	wantMsg := "Couldn't save: disk is full · 12 MB free"
	if got := app.Find("id:status-save-error-text").Text; got != wantMsg {
		t.Fatalf("disk full status message = %q, want %q", got, wantMsg)
	}

	// Clicking Save as… opens the Save Scene As dialog and recovers to a writable path.
	app.Press("id:status-save-as")
	app.Settle()
	if !app.Exists("id:scene-path-dialog") {
		t.Fatal("pressing Save as… in status bar did not open scene-path-dialog")
	}
	app.Post(func() {
		ed.SaveFileWriter = nil
	})
	app.Clear("id:scene-path-input")
	app.Type("id:scene-path-input", "scenes/room_copy.scene.yaml")
	app.Settle()
	app.Press("id:scene-path-confirm")
	app.Settle()

	if ed.SaveFailed() || ed.SceneDirty() || ed.ScenePath != "scenes/room_copy.scene.yaml" {
		t.Fatalf("after Save as…: failed=%v dirty=%v scenePath=%q", ed.SaveFailed(), ed.SceneDirty(), ed.ScenePath)
	}
	if _, err := os.Stat(filepath.Join(dir, "scenes", "room_copy.scene.yaml")); err != nil {
		t.Fatalf("saved-as copy missing on disk: %v", err)
	}
}

func TestAutoSaveStatusBarScreenshots(t *testing.T) {
	ed, _ := openTestProject(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	ed.Now = func() time.Time { return time.Date(2026, 11, 26, 14, 32, 8, 0, time.UTC) }
	ed.AutoSaveDelay = 30 * time.Millisecond
	ed.SavingIndicatorDelay = 40 * time.Millisecond

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// 1. Saved state (clean, muted Spec styling).
	if !app.Exists("id:status-save-saved") {
		t.Fatal("expected status-save-saved")
	}
	app.Screenshot("autosave-status-saved")

	// 2. Saving… state (write held open past SavingIndicatorDelay).
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	var once sync.Once
	app.Post(func() {
		ed.SaveFileWriter = func(path string, data []byte) error {
			once.Do(func() { close(writeStarted) })
			<-releaseWrite
			return os.WriteFile(path, data, 0o644)
		}
		_ = ed.MoveObject("hero", 68, 44)
	})
	deadline := time.Now().Add(3 * time.Second)
	started := false
	for time.Now().Before(deadline) && !started {
		app.Settle()
		select {
		case <-writeStarted:
			started = true
		default:
			time.Sleep(15 * time.Millisecond)
		}
	}
	if !started {
		t.Fatal("timed out waiting for slow autosave write to start")
	}
	app.WaitUntil(3*time.Second, func() bool {
		return ed.SaveStatus().Kind == editor.SaveStatusSaving
	})
	app.Settle()
	if !app.Exists("id:status-save-saving") {
		t.Fatal("expected status-save-saving")
	}
	app.Screenshot("autosave-status-saving")

	close(releaseWrite)
	app.WaitUntil(3*time.Second, func() bool {
		return ed.SaveStatus().Kind == editor.SaveStatusSaved && !ed.AutoSaveInFlight()
	})

	// 3. Failed autosave: read-only folder (red dot beside scene name + Retry / Save as…).
	app.Post(func() {
		ed.SaveFileWriter = func(path string, _ []byte) error {
			return &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}
		}
		_ = ed.MoveObject("hero", 74, 48)
	})
	app.WaitUntil(3*time.Second, func() bool {
		return ed.SaveStatus().Kind == editor.SaveStatusFailed
	})
	app.Settle()
	if !app.Exists("id:status-save-error") || !app.Exists("id:toolbar-unsaved") {
		t.Fatal("expected failed save bar and red unsaved dot")
	}
	app.Screenshot("autosave-status-failed-readonly")

	// 4. Failed autosave: disk full with free-space readout.
	app.Post(func() {
		ed.FreeSpaceBytes = func(string) (uint64, bool) { return 12 * 1024 * 1024, true }
		ed.SaveFileWriter = func(path string, _ []byte) error {
			return &os.PathError{Op: "write", Path: path, Err: syscall.ENOSPC}
		}
		_ = ed.RetrySave()
	})
	app.Settle()
	app.Screenshot("autosave-status-failed-diskfull")

	// 5. Settings › General dialog with Save automatically switch.
	app.Post(func() {
		ed.SaveFileWriter = nil
		_ = ed.RetrySave()
		ed.SetEditorSettingsOpen(true)
	})
	app.Settle()
	app.Screenshot("autosave-settings-general")
}

// #1016 regression: Tile Review (including Before preview) must pause autosave until kept.
func TestAutoSaveSkipsTileReviewAndBeforePreviewUntilKept(t *testing.T) {
	ed, dir := openTestProject(t)
	now := time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }

	// Add a small inline tilemap to the main layer and save cleanly first.
	ed.Doc.Layers[0].Tilemap = &sceneformat.TilemapDoc{
		Tileset:    "world",
		TileWidth:  16,
		TileHeight: 16,
		Width:      4,
		Height:     4,
		Data:       make([]int, 16),
	}
	if err := ed.Save(); err != nil {
		t.Fatalf("initial Save: %v", err)
	}
	scenePath := filepath.Join(dir, "scenes", "room.yaml")
	beforeDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}

	// Paint tiles inside a tracked agent tile review and toggle Before peek.
	res, err := ed.AgentPaintTiles(editor.AgentTilePaintRequest{
		Layer:       ed.Doc.Layers[0].Name,
		TileID:      3,
		Cells:       []editor.AgentTileCell{{Col: 1, Row: 1}, {Col: 2, Row: 1}},
		Label:       "Paint floor",
		TrackReview: true,
	})
	if err != nil {
		t.Fatalf("AgentPaintTiles: %v", err)
	}
	if !res.ReviewOpen || !ed.TileReviewOpen() || !ed.HasOpenReview() {
		t.Fatalf("expected open tile review: res=%+v HasOpenReview=%v", res, ed.HasOpenReview())
	}
	ed.SetTileReviewPeekBefore(true)
	if ed.AutoSavePending() {
		t.Fatal("autosave must not arm while tile review is open")
	}
	now = now.Add(10 * time.Second)
	ed.StepAutoSave()
	ed.WaitAutoSave()

	duringDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(duringDisk, beforeDisk) {
		t.Fatal("autosave wrote to disk while tile review / Before preview was open")
	}

	// Keeping the tile review closes the review and arms the 2 s autosave timer.
	if _, err := ed.KeepTileReview(); err != nil {
		t.Fatalf("KeepTileReview: %v", err)
	}
	if ed.HasOpenReview() || !ed.AutoSavePending() {
		t.Fatalf("after KeepTileReview: HasOpenReview=%v AutoSavePending=%v", ed.HasOpenReview(), ed.AutoSavePending())
	}
	now = now.Add(2 * time.Second)
	ed.StepAutoSave()
	ed.WaitAutoSave()

	afterDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(afterDisk, beforeDisk) || !strings.Contains(string(afterDisk), "3") {
		t.Fatalf("kept tile review edits were not persisted after 2 s:\n%s", afterDisk)
	}
}

// #1016 regression: Inspector numeric scrub defers autosave and re-arms pre-existing dirty edits on cancel.
func TestAutoSaveDefersDuringInspectorScrubAndRearmsOnCancel(t *testing.T) {
	ed, dir := openTestProject(t)
	now := time.Date(2026, 11, 26, 15, 5, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }

	scenePath := filepath.Join(dir, "scenes", "room.yaml")
	beforeDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}

	// Make a pre-existing kept edit (arms debounce at +2 s).
	if err := ed.MoveObject("hero", 33, 44); err != nil {
		t.Fatal(err)
	}
	if !ed.AutoSavePending() {
		t.Fatal("expected debounce armed after MoveObject")
	}

	// Start live inspector scrub before 2 s elapses.
	if err := ed.BeginInspectorScrub(editor.InspectorScrubField{ObjectID: "hero", Field: "x", Step: 1}); err != nil {
		t.Fatalf("BeginInspectorScrub: %v", err)
	}
	if _, err := ed.UpdateInspectorScrub(50, false); err != nil {
		t.Fatalf("UpdateInspectorScrub: %v", err)
	}

	// Advance past 2 s while scrubbing: must NOT write to disk mid-scrub.
	now = now.Add(3 * time.Second)
	ed.StepAutoSave()
	ed.WaitAutoSave()
	midDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(midDisk, beforeDisk) {
		t.Fatal("autosave wrote to disk while inspector scrub was active")
	}

	// Cancelling the scrub restores pre-scrub x=33 and re-arms autosave for the pre-existing dirty edit.
	if !ed.CancelInspectorScrub() {
		t.Fatal("CancelInspectorScrub returned false")
	}
	if !ed.AutoSavePending() {
		t.Fatal("CancelInspectorScrub did not re-arm deferred autosave")
	}
	now = now.Add(2 * time.Second)
	ed.StepAutoSave()
	ed.WaitAutoSave()

	afterDisk, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(afterDisk), "x: 33") {
		t.Fatalf("pre-existing dirty edit x=33 not saved after scrub cancel:\n%s", afterDisk)
	}
}

// #1016 + #1171 regression: partial multi-target failure commits succeeded items and suppresses in-flight disk watch false conflicts.
func TestAutoSavePartialMultiTargetFailureCommitsSucceededAndSuppressesDiskWatchSelfConflict(t *testing.T) {
	ed, dir := openTestProject(t)
	now := time.Date(2026, 11, 26, 15, 10, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }

	// Dirty BOTH game.yaml and scenes/room.yaml.
	ed.EditProject("Rename project", func(f *project.File) {
		f.Name = "Updated Project Name"
	})
	if err := ed.MoveObject("hero", 81, 92); err != nil {
		t.Fatal(err)
	}
	if !ed.ProjectDirty() || !ed.SceneDirty() {
		t.Fatalf("expected both project and scene dirty: project=%v scene=%v", ed.ProjectDirty(), ed.SceneDirty())
	}

	gamePath := filepath.Join(dir, project.FileName)
	scenePath := filepath.Join(dir, "scenes", "room.yaml")

	gameWritten := make(chan struct{})
	releaseScene := make(chan struct{})
	ed.SaveFileWriter = func(path string, data []byte) error {
		if filepath.Clean(path) == filepath.Clean(scenePath) {
			close(gameWritten)
			<-releaseScene
			return &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}
		}
		return os.WriteFile(path, data, 0o644)
	}

	now = now.Add(2 * time.Second)
	ed.StepAutoSave()
	<-gameWritten

	// While flight is in progress (game.yaml is already written to disk, scenes/room.yaml is blocked),
	// PollDiskChanges must NOT flag game.yaml as an external disk conflict.
	midPoll := ed.PollDiskChanges()
	if len(midPoll.Conflicts) > 0 || len(ed.DiskConflicts()) > 0 {
		t.Fatalf("PollDiskChanges flagged false external conflict for in-flight autosave: poll=%+v conflicts=%+v", midPoll, ed.DiskConflicts())
	}

	close(releaseScene)
	ed.WaitAutoSave()

	// scenes/room.yaml failed, so SaveFailed() is true; game.yaml succeeded and MUST be committed so ProjectDirty()==false and no false disk conflict occurs.
	if !ed.SaveFailed() {
		t.Fatal("expected SaveFailed() == true after scenes/room.yaml failed")
	}
	if ed.ProjectDirty() {
		t.Fatal("expected game.yaml partial success to commit (ProjectDirty() == false)")
	}
	afterPoll := ed.PollDiskChanges()
	if len(afterPoll.Conflicts) > 0 || len(ed.DiskConflicts()) > 0 {
		t.Fatalf("PollDiskChanges flagged false external conflict after partial autosave failure: poll=%+v conflicts=%+v", afterPoll, ed.DiskConflicts())
	}
	gotGame, err := os.ReadFile(gamePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotGame), "Updated Project Name") {
		t.Fatalf("game.yaml missing partial autosave update:\n%s", gotGame)
	}
}

// #1016 regression: OnSave runs synchronously on editor goroutine (-race safe), and Close()/OpenDemo() clean up timers and failure state.
func TestAutoSaveOnSaveRunsOnEditorGoroutineAndCloseDemoReset(t *testing.T) {
	ed := newTestEditor(t)
	now := time.Date(2026, 11, 26, 15, 15, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }

	// Unsynchronized slice mutated in OnSave AND read immediately after StepAutoSave() — -race catches any off-goroutine invocation.
	var saves [][]byte
	ed.OnSave = func(data []byte) error {
		saves = append(saves, append([]byte(nil), data...))
		return nil
	}
	if err := ed.MoveObject("hero", 19, 29); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	ed.StepAutoSave()
	if len(saves) != 1 || ed.SceneDirty() || ed.AutoSaveInFlight() {
		t.Fatalf("expected synchronous OnSave on editor goroutine: saves=%d dirty=%v inFlight=%v", len(saves), ed.SceneDirty(), ed.AutoSaveInFlight())
	}

	// Editor.Close() stops timers even when called while Play mode is active.
	ed2, _ := openTestProject(t)
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed2)
	}, nsgotest.Size(1280, 800))
	app2.Settle()
	app2.Post(func() {
		if err := ed2.MoveObject("hero", 11, 22); err != nil {
			t.Errorf("MoveObject: %v", err)
			return
		}
		if err := ed2.Play(); err != nil {
			t.Errorf("Play: %v", err)
			return
		}
		if err := ed2.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	app2.Settle()
	if ed2.AutoSavePending() {
		t.Fatal("Close() left autosave debounce timer armed")
	}

	// OpenDemo() resets armed timers and active SaveFailed state cleanly.
	ed3, _ := openTestProject(t)
	now3 := time.Date(2026, 11, 26, 15, 20, 0, 0, time.UTC)
	ed3.Now = func() time.Time { return now3 }
	ed3.SaveFileWriter = func(path string, _ []byte) error {
		return &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}
	}
	if err := ed3.MoveObject("hero", 41, 51); err != nil {
		t.Fatal(err)
	}
	now3 = now3.Add(2 * time.Second)
	ed3.StepAutoSave()
	ed3.WaitAutoSave()
	if !ed3.SaveFailed() {
		t.Fatal("expected SaveFailed() before OpenDemo()")
	}
	if err := ed3.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed3.SaveFailed() || ed3.AutoSavePending() || ed3.AutoSaveInFlight() {
		t.Fatalf("OpenDemo() leaked autosave state: failed=%v pending=%v inFlight=%v", ed3.SaveFailed(), ed3.AutoSavePending(), ed3.AutoSaveInFlight())
	}
}

// #1016 regression: multi-tab save failure isolation, Save as… targeting the failed tab, and CloseTab ghost cleanup.
func TestAutoSaveMultiTabFailureIsolationCloseTabCleanupAndSaveAsTarget(t *testing.T) {
	ed, dir := openTestProject(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	now := time.Date(2026, 11, 26, 15, 25, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }

	// Create and open a second scene tab (scenes/other.yaml).
	otherPath := filepath.Join(dir, "scenes", "other.yaml")
	roomData, err := os.ReadFile(filepath.Join(dir, "scenes", "room.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, roomData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenScene("scenes/other.yaml"); err != nil {
		t.Fatalf("OpenScene other: %v", err)
	}
	if len(ed.Tabs()) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(ed.Tabs()))
	}
	tab0ID := ed.Tabs()[0].ID

	// Fail autosave on tab 0 (scenes/room.yaml), while keeping scenes/other.yaml writable.
	roomPath := filepath.Join(dir, "scenes", "room.yaml")
	ed.SaveFileWriter = func(path string, data []byte) error {
		if filepath.Clean(path) == filepath.Clean(roomPath) {
			return &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}
		}
		return os.WriteFile(path, data, 0o644)
	}
	if err := ed.SelectTab(0); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("hero", 61, 71); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	ed.StepAutoSave()
	ed.WaitAutoSave()
	if !ed.SaveFailed() || !ed.SaveFailedForTab(tab0ID) {
		t.Fatalf("expected failure on tab 0: status=%+v", ed.SaveStatus())
	}

	// Switch to tab 1, edit it, and manually save tab 1: must NOT clear tab 0's failed-save alert!
	if err := ed.SelectTab(1); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveObject("hero", 12, 34); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveTab(1); err != nil {
		t.Fatalf("SaveTab(1): %v", err)
	}
	if !ed.SaveFailed() || !ed.SaveFailedForTab(tab0ID) {
		t.Fatal("saving tab 1 clobbered tab 0's active SaveFailed alert")
	}

	// Mount EditorView while tab 1 is active and press status-save-as: must select tab 0 (scenes/room.yaml)!
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	app.Press("id:status-save-as")
	app.Settle()
	if !app.Exists("id:scene-path-dialog") {
		t.Fatal("expected scene-path-dialog when pressing status-save-as")
	}
	if ed.ScenePath != "scenes/room.yaml" {
		t.Fatalf("Save as… targeted %q, want failed tab scenes/room.yaml", ed.ScenePath)
	}
	app.Press("id:scene-path-cancel")
	app.Settle()

	// Closing the failing tab clears its failure state (no ghost alert, HasUnsavedChanges() == false).
	app.Post(func() {
		if err := ed.CloseTab(0); err != nil {
			t.Errorf("CloseTab(0): %v", err)
		}
	})
	app.Settle()
	if ed.SaveFailed() || ed.HasUnsavedChanges() {
		t.Fatalf("CloseTab(0) left ghost save failure: failed=%v unsaved=%v", ed.SaveFailed(), ed.HasUnsavedChanges())
	}
	if app.Exists("id:status-save-error") || !app.Exists("id:status-save-saved") {
		t.Fatal("status bar did not return to Saved after closing failing tab")
	}
}

// #1016 + #1192 regression: status bar save alert and toolbar unsaved dot follow Spec Light and Dark palettes live.
func TestAutoSaveStatusBarSpecLightAndDarkThemeParity(t *testing.T) {
	ed, _ := openTestProject(t)
	now := time.Date(2026, 11, 26, 15, 30, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	ed.SaveFileWriter = func(path string, _ []byte) error {
		return &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}
	}
	if err := ed.MoveObject("hero", 45, 55); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	ed.StepAutoSave()
	ed.WaitAutoSave()
	if !ed.SaveFailed() {
		t.Fatal("expected SaveFailed() == true")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))
	app.Settle()

	assertTheme := func(label string, want theme.Palette) {
		t.Helper()
		pill := app.Find("id:status-save-error")
		if pill.Style.Background != want.Problem().Fill || pill.Style.BorderColor != want.Problem().Edge {
			t.Fatalf("%s: status-save-error bg=#%08X border=#%08X, want bg=#%08X border=#%08X",
				label, uint32(pill.Style.Background), uint32(pill.Style.BorderColor),
				uint32(want.Problem().Fill), uint32(want.Problem().Edge))
		}
		dot := app.Find("id:toolbar-unsaved")
		if dot.Style.Background != want.Problem().Fg {
			t.Fatalf("%s: toolbar-unsaved bg=#%08X, want #%08X", label, uint32(dot.Style.Background), uint32(want.Problem().Fg))
		}
		b := dot.Bounds()
		if b.Size.Width < 5.5 || b.Size.Height < 5.5 {
			t.Fatalf("%s: toolbar-unsaved bounds = %.1fx%.1f, want 6x6", label, b.Size.Width, b.Size.Height)
		}
	}

	assertTheme("light", theme.Light())

	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	app.Settle()
	assertTheme("dark", theme.Dark())
}
