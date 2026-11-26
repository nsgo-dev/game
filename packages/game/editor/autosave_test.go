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

	"github.com/nsgo-dev/game/packages/game/editor"
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
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Settle()
		if ed.SaveStatus().Kind == editor.SaveStatusFailed {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
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
	ed.SaveFileWriter = func(string, []byte) error {
		return &os.PathError{Op: "write", Path: ed.FilePath, Err: syscall.ENOSPC}
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
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Settle()
		if ed.SaveStatus().Kind == editor.SaveStatusFailed {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
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
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Settle()
		if ed.SaveStatus().Kind == editor.SaveStatusSaving {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	app.Settle()
	if !app.Exists("id:status-save-saving") {
		t.Fatal("expected status-save-saving")
	}
	app.Screenshot("autosave-status-saving")

	close(releaseWrite)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Settle()
		if ed.SaveStatus().Kind == editor.SaveStatusSaved && !ed.AutoSaveInFlight() {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	// 3. Failed autosave: read-only folder (red dot beside scene name + Retry / Save as…).
	app.Post(func() {
		ed.SaveFileWriter = func(path string, _ []byte) error {
			return &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}
		}
		_ = ed.MoveObject("hero", 74, 48)
	})
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Settle()
		if ed.SaveStatus().Kind == editor.SaveStatusFailed {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
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
