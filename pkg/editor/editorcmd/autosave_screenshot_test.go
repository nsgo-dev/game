package editorcmd_test

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editorcmd"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAutoSaveStatusBarScreenshots(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"game.yaml":              gameYAML,
		"scenes/room.scene.yaml": "version: 2\nname: level_01\nvirtualWidth: 640\nvirtualHeight: 360\nbackgroundColor: \"#15181d\"\nlayers:\n  - name: Actors\n    space: world\n    objects:\n      - id: knight\n        name: Knight\n        x: 120\n        y: 180\n      - id: slime_01\n        name: slime_01\n        x: 240\n        y: 200\n      - id: door\n        name: Door\n        x: 380\n        y: 170\n",
	})
	ed, err := editorcmd.New(editorcmd.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	if err := editorcmd.Open(ed, []string{filepath.Join(dir, "game.yaml")}); err != nil {
		t.Fatal(err)
	}
	_ = ed.SetLanguage(editor.LanguageEnglish)
	_ = ed.SetAppearance(editor.AppearanceDark)
	ed.Now = func() time.Time { return time.Date(2026, 11, 26, 14, 32, 8, 0, time.UTC) }
	ed.AutoSaveDelay = 30 * time.Millisecond
	ed.SavingIndicatorDelay = 40 * time.Millisecond

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// 1. Saved state (muted Spec styling).
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
		_ = ed.MoveObject("knight", 136, 180)
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
		_ = ed.MoveObject("knight", 152, 180)
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
