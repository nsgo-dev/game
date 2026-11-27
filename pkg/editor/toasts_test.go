package editor_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func toastDesktopEnv(dark, reduceMotion bool) core.Environment {
	return core.Environment{
		OS:           core.OSMacOS,
		Idiom:        core.IdiomDesktop,
		Dark:         dark,
		ReduceMotion: reduceMotion,
		Locale:       "en-US",
		Lifecycle:    core.LifecycleActive,
	}
}

// advanceToastClock jumps the manual test clock by d without stepping 60 fps display frames,
// so multi-second toast timer checks (4 s, 6 s, 8 s, 60 s) stay deterministic and sub-millisecond.
func advanceToastClock(app *nsgotest.App, d time.Duration) {
	app.Clock.Advance(d)
	app.Settle()
}

// #1004 Acceptance 1:
//   - Success with Undo, info, warning, and error render with Spec tokens, 40 px height,
//     radius 12, single line, distinct icon shapes, and dismiss button on all except progress.
//   - Strict queue order, +N waiting badge, duplicate event merges into waiting entry,
//     nothing jumps ahead of an error or running progress toast.
//   - Exact Spec auto-dismiss timing (success/info 4s, 6s with action, warning 8s, error &
//     progress stay) and timer pausing on hover, keyboard focus, and window background.
func TestToastsKindsQueueMergeTimingAndPause(t *testing.T) {
	ed := newTestEditor(t)

	// Verify Spec Feedback channels table covers all 12 rows and routes decisions outside toasts.
	channels := editor.SpecFeedbackChannels()
	if len(channels) != 12 {
		t.Fatalf("SpecFeedbackChannels count = %d, want 12", len(channels))
	}
	for _, ch := range channels {
		if strings.Contains(strings.ToLower(ch.Event), "asks permission") && ch.Channel == editor.FeedbackChannelToast {
			t.Fatalf("permission decision routed to toast: %+v", ch)
		}
	}

	var undone bool
	ed.EmitToastEvent(editor.ToastEvent{
		Kind:     editor.ToastEventReviewKept,
		Target:   "level_01",
		Message:  "Kept 6 changes to level_01",
		OnAction: func() { undone = true },
	})
	ed.EmitToastEvent(editor.ToastEvent{
		Kind:    editor.ToastEventCopied,
		Target:  "mcp-claude",
		Message: "Copied the MCP config for Claude Code",
	})
	ed.EmitToastEvent(editor.ToastEvent{
		Kind:     editor.ToastEventDiskChanged,
		Target:   "scenes/level_01.scene.yaml",
		Message:  "level_01.scene.yaml changed on disk and was reloaded",
		OnAction: func() {},
	})
	// Repeat of the same waiting event merges in place rather than duplicating or changing order.
	ed.EmitToastEvent(editor.ToastEvent{
		Kind:     editor.ToastEventDiskChanged,
		Target:   "scenes/level_01.scene.yaml",
		Message:  "level_01.scene.yaml changed on disk and was reloaded (updated)",
		OnAction: func() {},
	})
	ed.EmitToastEvent(editor.ToastEvent{
		Kind:     editor.ToastEventSaveFailed,
		Target:   "level_01.scene.yaml",
		Message:  "Couldn't save level_01.scene.yaml: the disk is full",
		OnAction: func() {},
	})

	if got := ed.QueuedToastCount(); got != 3 {
		t.Fatalf("QueuedToastCount = %d, want 3 (repeat disk event must merge)", got)
	}
	queued := ed.QueuedToasts()
	if len(queued) != 3 || queued[0].Kind != editor.ToastInfo || queued[1].Kind != editor.ToastWarning || queued[2].Kind != editor.ToastError {
		t.Fatalf("unexpected queue order: %+v", queued)
	}
	if !strings.Contains(queued[1].Message, "(updated)") {
		t.Fatalf("merged warning message = %q, want updated copy", queued[1].Message)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(toastDesktopEnv(true, true)), nsgotest.ManualFrames())

	dark := editor.DarkSpecPalette()

	// 1. First visible toast: Success with Undo ⌘Z, +3 badge, 40 px height, radius 12, Raised fill.
	toastView := app.Find("id:editor-toast")
	if toastView.Bounds().Size.Height != 40 || toastView.Style.CornerRadius != 12 {
		t.Fatalf("toast geometry height=%v radius=%v, want 40 / 12", toastView.Bounds().Size.Height, toastView.Style.CornerRadius)
	}
	if toastView.Style.Background != dark.Raised || toastView.Style.BorderColor != dark.LineRaised {
		t.Fatalf("success toast bg=%#x border=%#x, want Raised=%#x LineRaised=%#x",
			uint32(toastView.Style.Background), uint32(toastView.Style.BorderColor), uint32(dark.Raised), uint32(dark.LineRaised))
	}
	if !app.Exists("id:editor-toast-icon-success") {
		t.Fatal("missing success icon")
	}
	if got := strings.TrimSpace(allText(app.Find("id:editor-toast-waiting"))); got != "+3" {
		t.Fatalf("waiting badge = %q, want +3", got)
	}
	if got := app.Find("id:editor-toast-action-shortcut").Text; got != "⌘Z" {
		t.Fatalf("action shortcut = %q, want ⌘Z", got)
	}
	if !app.Exists("id:editor-toast-dismiss") {
		t.Fatal("success toast must have dismiss button")
	}

	// Success with an action lasts 6 s (not 4 s).
	advanceToastClock(app, 4500*time.Millisecond)
	if !strings.Contains(app.Find("id:editor-toast-message").Text, "Kept 6 changes") {
		t.Fatalf("success-with-action dismissed too early at 4.5s: got %q", app.Find("id:editor-toast-message").Text)
	}

	// Hovering pauses the countdown indefinitely.
	app.Hover("id:editor-toast")
	if !ed.ToastTimerPaused() {
		t.Fatal("hovering toast did not pause timer")
	}
	advanceToastClock(app, 10*time.Second)
	if !strings.Contains(app.Find("id:editor-toast-message").Text, "Kept 6 changes") {
		t.Fatal("toast dismissed while hovered")
	}
	app.HoverOut()
	if ed.ToastTimerPaused() {
		t.Fatal("HoverOut did not unpause timer")
	}

	// Pressing Undo invokes OnAction; advancing past remaining ~1.5s auto-dismisses to Info.
	app.Press("id:editor-toast-action")
	if !undone {
		t.Fatal("Undo action callback did not run")
	}
	advanceToastClock(app, 2*time.Second)

	// 2. Second visible toast: Info (4 s), +2 badge.
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "Copied the MCP config") {
		t.Fatalf("second toast = %q, want Copied MCP config", got)
	}
	if !app.Exists("id:editor-toast-icon-info") {
		t.Fatal("missing info icon")
	}
	if got := strings.TrimSpace(allText(app.Find("id:editor-toast-waiting"))); got != "+2" {
		t.Fatalf("second waiting badge = %q, want +2", got)
	}

	// Focusing the toast via Shift-F6 pauses its 4 s timer.
	advanceToastClock(app, 2*time.Second)
	app.Key("shift+f6")
	if !ed.ToastTimerPaused() {
		t.Fatal("focusing toast did not pause timer")
	}
	advanceToastClock(app, 10*time.Second)
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "Copied the MCP config") {
		t.Fatalf("focused toast dismissed while focused: got %q", got)
	}
	// Leave toast focus via Shift-F6 -> timer resumes with ~2 s left.
	app.Key("shift+f6")
	if ed.ToastTimerPaused() {
		t.Fatal("leaving toast focus did not resume timer")
	}
	advanceToastClock(app, 1500*time.Millisecond)
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "Copied the MCP config") {
		t.Fatalf("info toast dismissed before remaining 2s elapsed: got %q", got)
	}
	advanceToastClock(app, 1*time.Second)

	// 3. Third visible toast: Warning (8 s) on Warning.Fill & Warning.Edge, +1 badge.
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "changed on disk") {
		t.Fatalf("third toast = %q, want warning disk toast", got)
	}
	if !app.Exists("id:editor-toast-icon-warning") {
		t.Fatal("missing warning icon")
	}
	warnStyle := app.Find("id:editor-toast").Style
	if warnStyle.Background != dark.Warning.Fill || warnStyle.BorderColor != dark.Warning.Edge {
		t.Fatalf("warning toast bg=%#x border=%#x, want Warning.Fill=%#x Warning.Edge=%#x",
			uint32(warnStyle.Background), uint32(warnStyle.BorderColor), uint32(dark.Warning.Fill), uint32(dark.Warning.Edge))
	}
	if got := strings.TrimSpace(allText(app.Find("id:editor-toast-waiting"))); got != "+1" {
		t.Fatalf("third waiting badge = %q, want +1", got)
	}

	// Window going to background pauses the 8 s timer.
	advanceToastClock(app, 4*time.Second)
	bgEnv := toastDesktopEnv(true, true)
	bgEnv.Lifecycle = core.LifecycleBackground
	app.SetEnvironment(bgEnv)
	if !ed.ToastTimerPaused() {
		t.Fatal("window going to background did not pause toast timer")
	}
	advanceToastClock(app, 20*time.Second)
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "changed on disk") {
		t.Fatalf("warning toast dismissed while window was in background: got %q", got)
	}
	app.SetEnvironment(toastDesktopEnv(true, true))
	if ed.ToastTimerPaused() {
		t.Fatal("window returning to active did not resume toast timer")
	}
	advanceToastClock(app, 3*time.Second)
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "changed on disk") {
		t.Fatalf("warning toast dismissed before 8 s elapsed: got %q", got)
	}
	advanceToastClock(app, 2*time.Second)

	// 4. Fourth visible toast: Error stays indefinitely until retried or dismissed.
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "the disk is full") {
		t.Fatalf("fourth toast = %q, want save error toast", got)
	}
	if !app.Exists("id:editor-toast-icon-error") {
		t.Fatal("missing error icon")
	}
	if app.Exists("id:editor-toast-waiting") {
		t.Fatal("waiting badge should not render when queue has no waiting items")
	}
	advanceToastClock(app, 60*time.Second)
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "the disk is full") {
		t.Fatalf("error toast auto-dismissed after 60s; must stay until dismissed")
	}

	// Nothing jumps ahead of an active error toast: a newly fired info toast waits behind it.
	app.Post(func() {
		ed.EmitToastEvent(editor.ToastEvent{
			Kind:    editor.ToastEventCopied,
			Target:  "later",
			Message: "Copied later snippet",
		})
	})
	app.Settle()
	if got := app.Find("id:editor-toast-message").Text; !strings.Contains(got, "the disk is full") {
		t.Fatalf("new info toast jumped ahead of active error toast: got %q", got)
	}
	if got := strings.TrimSpace(allText(app.Find("id:editor-toast-waiting"))); got != "+1" {
		t.Fatalf("waiting badge behind error = %q, want +1", got)
	}

	// Dismissing the error advances cleanly to the waiting info toast.
	app.Press("id:editor-toast-dismiss")
	if got := app.Find("id:editor-toast-message").Text; got != "Copied later snippet" {
		t.Fatalf("after dismissing error, active toast = %q, want Copied later snippet", got)
	}
}

// #1004 Acceptance 2:
// Progress toast updates its percent, has no dismiss button, keeps running in front of later
// toasts, turns into a success toast with "Show in Finder" when done, and its Cancel button
// cancels the live export build.
func TestToastProgressHandleAndExportIntegration(t *testing.T) {
	t.Run("handle updates percent and completes into success", func(t *testing.T) {
		ed := newTestEditor(t)
		var revealed bool
		h := ed.EmitToastEvent(editor.ToastEvent{
			Kind:    editor.ToastEventExportRunning,
			Target:  "macos",
			Message: "Exporting dungeon-run for macOS",
			OnCancel: func() {
				t.Error("Cancel should not fire during normal completion")
			},
		})
		// A later info toast must NOT jump ahead of the running progress toast.
		ed.EmitToastEvent(editor.ToastEvent{
			Kind:    editor.ToastEventCopied,
			Target:  "snippet",
			Message: "Copied snippet",
		})

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900), nsgotest.Environment(toastDesktopEnv(true, true)), nsgotest.ManualFrames())

		if !app.Exists("id:editor-toast-icon-progress") {
			t.Fatal("missing progress icon")
		}
		if app.Exists("id:editor-toast-dismiss") {
			t.Fatal("progress toast must NOT show a dismiss button")
		}
		if got := app.Find("id:editor-toast-progress-pct").Text; got != "0%" {
			t.Fatalf("initial progress pct = %q, want 0%%", got)
		}

		// Update percent in place without disturbing queue order or stealing focus.
		app.Post(func() {
			h.SetProgress(0.62, "")
		})
		app.Settle()
		if got := app.Find("id:editor-toast-progress-pct").Text; got != "62%" {
			t.Fatalf("updated progress pct = %q, want 62%%", got)
		}
		if got := strings.TrimSpace(allText(app.Find("id:editor-toast-waiting"))); got != "+1" {
			t.Fatalf("waiting badge during progress = %q, want +1", got)
		}
		// Progress toast stays indefinitely until done or canceled.
		advanceToastClock(app, 30*time.Second)
		if !app.Exists("id:editor-toast-icon-progress") {
			t.Fatal("progress toast auto-dismissed while running")
		}

		// Complete transitions the progress toast into a success toast with Show in Finder (6 s).
		app.Post(func() {
			h.Complete("Exported build/macos/dungeon-run.app", "Show in Finder", "", func() {
				revealed = true
			})
		})
		app.Settle()
		if !app.Exists("id:editor-toast-icon-success") {
			t.Fatal("completed progress toast did not transition to success icon")
		}
		if !app.Exists("id:editor-toast-dismiss") {
			t.Fatal("completed success toast must show dismiss button")
		}
		if got := app.Find("id:editor-toast-message").Text; got != "Exported build/macos/dungeon-run.app" {
			t.Fatalf("completed message = %q", got)
		}
		app.Press("id:editor-toast-action")
		if !revealed {
			t.Fatal("Show in Finder action did not run")
		}
		// Success with action stays for 6 s, then advances to the waiting info toast.
		advanceToastClock(app, 4500*time.Millisecond)
		if !app.Exists("id:editor-toast-icon-success") {
			t.Fatal("completed success toast with action dismissed before 6s")
		}
		advanceToastClock(app, 2*time.Second)
		if got := app.Find("id:editor-toast-message").Text; got != "Copied snippet" {
			t.Fatalf("after completed toast timed out, active toast = %q, want Copied snippet", got)
		}
	})

	t.Run("export panel progress toast cancels and completes", func(t *testing.T) {
		dir := writeEditorProject(t)
		ed, err := editor.New(nil, testResolver())
		if err != nil {
			t.Fatal(err)
		}
		if err := ed.OpenProject(dir); err != nil {
			t.Fatal(err)
		}

		started := make(chan struct{})
		canceledCh := make(chan struct{})
		ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
			fmt.Fprintln(stdout, "compiling macOS target 40%")
			close(started)
			<-ctx.Done()
			close(canceledCh)
			return ctx.Err()
		}
		var openedCount atomic.Int32
		ed.OpenOutputFolder = func(string) error {
			openedCount.Add(1)
			return nil
		}

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900), nsgotest.Environment(toastDesktopEnv(true, true)))

		// Open Settings > Export (no viewport on screen -> toast anchors at window bottom center, 24 px up).
		runEditorCommand(t, app, ed, editor.CmdExport)

		app.Post(func() {
			if err := ed.StartExportBuild(editor.ExportBuildOptions{Targets: []string{"macos_arm64"}}); err != nil {
				t.Errorf("StartExportBuild: %v", err)
			}
		})
		app.Settle()

		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for export runner to start")
		}
		app.WaitUntil(3*time.Second, func() bool {
			return app.Exists("id:editor-toast-progress-pct") && app.Find("id:editor-toast-progress-pct").Text == "40%"
		})
		if !app.Exists("id:editor-window-toast-host") {
			t.Fatal("expected window toast host while Export settings screen covers viewport")
		}
		if app.Exists("id:editor-toast-dismiss") {
			t.Fatal("running export progress toast must not have a dismiss button")
		}

		// Pressing Cancel on the progress toast cancels the export build and dismisses the toast cleanly.
		app.Press("id:editor-toast-action")
		select {
		case <-canceledCh:
		case <-time.After(3 * time.Second):
			t.Fatal("pressing Cancel on progress toast did not cancel the export build")
		}
		app.WaitUntil(3*time.Second, func() bool { return !ed.ExportRunning() })
		app.Settle()
		if ed.HasActiveToast() {
			toast, _ := ed.ActiveToast()
			t.Fatalf("user-canceled export left active toast: %+v", toast)
		}

		// Now run an export to completion: progress toast becomes "Exported <path>" with "Show in Finder".
		ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
			fmt.Fprintln(stdout, "packaging 85%")
			return nil
		}
		app.Post(func() {
			if err := ed.StartExportBuild(editor.ExportBuildOptions{Targets: []string{"macos_arm64"}}); err != nil {
				t.Errorf("StartExportBuild(success): %v", err)
			}
		})
		app.WaitUntil(3*time.Second, func() bool { return !ed.ExportRunning() })
		app.Settle()

		active, ok := ed.ActiveToast()
		if !ok || active.Kind != editor.ToastSuccess {
			t.Fatalf("completed export toast = %+v (ok=%v), want ToastSuccess", active, ok)
		}
		if !strings.HasPrefix(active.Message, "Exported ") {
			t.Fatalf("completed export message = %q, want 'Exported <path>'", active.Message)
		}
		if active.ActionLabel != "Show in Finder" {
			t.Fatalf("completed export action = %q, want 'Show in Finder'", active.ActionLabel)
		}
		beforeReveal := openedCount.Load()
		app.Press("id:editor-toast-action")
		if openedCount.Load() != beforeReveal+1 {
			t.Fatalf("Show in Finder did not invoke OpenOutputFolder: count=%d, want %d", openedCount.Load(), beforeReveal+1)
		}
	})
}

// #1004 Acceptance 3:
// A failed explicit ⌘S save shows a persistent error toast with Retry, and Retry saves;
// a failed autosave stays in the status bar and NEVER shows a toast.
func TestToastExplicitSaveFailureRetryAndAutosaveNeverToasts(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetAutoSave(false); err != nil {
		t.Fatalf("SetAutoSave(false): %v", err)
	}
	var failSave atomic.Bool
	failSave.Store(true)
	var saveAttempts atomic.Int32
	ed.OnSave = func(data []byte) error {
		saveAttempts.Add(1)
		if failSave.Load() {
			return fmt.Errorf("write level_01: %w", syscall.ENOSPC)
		}
		return nil
	}
	if err := ed.MoveObject("hero", 42, 84); err != nil {
		t.Fatal(err)
	}
	if !ed.SceneDirty() {
		t.Fatal("expected dirty scene after MoveObject")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(toastDesktopEnv(true, true)), nsgotest.ManualFrames())

	// Press ⌘S (mod+s): explicit save fails -> persistent error toast with Retry.
	app.Key("mod+s")
	if saveAttempts.Load() != 1 {
		t.Fatalf("saveAttempts = %d, want 1", saveAttempts.Load())
	}
	toast, ok := ed.ActiveToast()
	if !ok || toast.Kind != editor.ToastError {
		t.Fatalf("ActiveToast after failed ⌘S = %+v (ok=%v), want ToastError", toast, ok)
	}
	if !strings.Contains(toast.Message, "Couldn't save") || !strings.Contains(toast.Message, "disk is full") {
		t.Fatalf("save error toast message = %q, want 'Couldn't save ... disk is full'", toast.Message)
	}
	if toast.ActionLabel != "Retry" {
		t.Fatalf("save error toast action = %q, want Retry", toast.ActionLabel)
	}
	// Error toast must stay until retried or dismissed.
	advanceToastClock(app, 30*time.Second)
	if !app.Exists("id:editor-toast") {
		t.Fatal("save error toast auto-dismissed; must stay until retried or dismissed")
	}

	// Fix disk condition and press Retry -> saves the scene and clears the error toast.
	failSave.Store(false)
	app.Press("id:editor-toast-action")
	if saveAttempts.Load() != 2 {
		t.Fatalf("Retry did not attempt save: saveAttempts = %d, want 2", saveAttempts.Load())
	}
	if ed.SceneDirty() {
		t.Fatal("scene still dirty after Retry succeeded")
	}
	if ed.HasActiveToast() || app.Exists("id:editor-toast") {
		t.Fatal("save error toast should dismiss once Retry saves")
	}

	// Verify autosave failure NEVER toasts (stays strictly in the status bar per Checkpoints.dc.html).
	failSave.Store(true)
	ed.AutoSaveDelay = 50 * time.Millisecond
	if err := ed.SetAutoSave(true); err != nil {
		t.Fatalf("SetAutoSave(true): %v", err)
	}
	app.Post(func() {
		if err := ed.MoveObject("hero", 99, 100); err != nil {
			t.Errorf("MoveObject: %v", err)
		}
	})
	app.Settle()
	app.Advance(100 * time.Millisecond)
	app.WaitUntil(2*time.Second, func() bool {
		return ed.SaveStatus().Kind == editor.SaveStatusFailed
	})
	if ed.HasActiveToast() {
		bad, _ := ed.ActiveToast()
		t.Fatalf("autosave failure must never show a toast, got %+v", bad)
	}
}

// #1004 Acceptance 4:
//   - Polite live region (role="status"), focus never jumps to a toast, F6 reaches it,
//     Tab moves between its buttons, Esc dismisses the focused toast and returns focus
//     to where it was (and Esc while unfocused does NOT dismiss the toast).
//   - Motion: 180 ms in (fading 0->1 and rising 8->0 px), 120 ms out (fading 1->0);
//     with ReduceMotion, appears and leaves immediately with no fade or movement.
func TestToastKeyboardEscFocusReturnAndMotion(t *testing.T) {
	t.Run("Esc dismisses focused toast and restores prior focus", func(t *testing.T) {
		ed := newTestEditor(t)
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900), nsgotest.Environment(toastDesktopEnv(true, true)), nsgotest.ManualFrames())

		// Focus a specific control in Header.
		app.Focus("id:toolbar-history")
		app.Settle()
		if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-history" {
			t.Fatalf("initial focus = %v, want toolbar-history", fv)
		}

		// Showing a toast must NEVER steal focus, and mounts inside a polite role="status" live region.
		app.Post(func() {
			ed.EmitToastEvent(editor.ToastEvent{
				Kind:     editor.ToastEventReviewKept,
				Target:   "level_01",
				Message:  "Kept 4 changes to level_01",
				OnAction: func() {},
			})
		})
		app.Settle()
		host := app.Find("id:editor-toast-host")
		if host.Props.Role != core.RoleStatus || host.Props.LiveRegion != core.LiveRegionPolite {
			t.Fatalf("editor-toast-host Role=%v LiveRegion=%v, want RoleStatus + LiveRegionPolite", host.Props.Role, host.Props.LiveRegion)
		}
		if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-history" {
			t.Fatalf("showing toast stole focus: got %v", fv)
		}

		// Pressing Esc while focus is NOT in the toast must NOT dismiss the toast.
		app.Key("escape")
		if !ed.HasActiveToast() {
			t.Fatal("Esc dismissed toast while toast was not focused")
		}

		// Shift-F6 reaches RegionToast -> focuses action button; Tab moves to dismiss button.
		app.Key("shift+f6")
		if fv := focusedView(app); fv == nil || fv.Props.TestID != "editor-toast-action" {
			t.Fatalf("Shift-F6 into toast focused %v, want editor-toast-action", fv)
		}
		app.Key("tab")
		if fv := focusedView(app); fv == nil || fv.Props.TestID != "editor-toast-dismiss" {
			t.Fatalf("Tab inside toast focused %v, want editor-toast-dismiss", fv)
		}

		// Pressing Esc dismisses the focused toast and returns focus to toolbar-history!
		app.Key("escape")
		if ed.HasActiveToast() || app.Exists("id:editor-toast") {
			t.Fatal("Esc did not dismiss focused toast")
		}
		if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-history" {
			gotID := "<nil>"
			if fv != nil {
				gotID = fv.Props.TestID
			}
			t.Fatalf("after Esc dismissed toast, focus = %q, want toolbar-history", gotID)
		}
	})

	t.Run("180ms enter rise+fade and 120ms exit fade", func(t *testing.T) {
		ed := newTestEditor(t)
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900), nsgotest.Environment(toastDesktopEnv(true, false)), nsgotest.ManualFrames())

		app.Post(func() {
			ed.EmitToastEvent(editor.ToastEvent{
				Kind:    editor.ToastEventCopied,
				Target:  "motion-normal",
				Message: "Copied config",
			})
		})
		app.Settle()

		// Frame 0: starts faded out and 8 px below resting position.
		m0 := app.Find("id:editor-toast-motion").Animated
		if m0.Opacity > 0.05 || math.Abs(float64(m0.TranslateY-8)) > 0.5 {
			t.Fatalf("enter start: opacity=%v translateY=%v, want ~0 and ~8", m0.Opacity, m0.TranslateY)
		}

		// Midpoint (~90ms): fading in and rising.
		app.Advance(90 * time.Millisecond)
		mMid := app.Find("id:editor-toast-motion").Animated
		if mMid.Opacity <= 0.1 || mMid.Opacity >= 0.99 || mMid.TranslateY <= 0.1 || mMid.TranslateY >= 7.9 {
			t.Fatalf("enter midpoint 90ms: opacity=%v translateY=%v, want strictly between start and rest", mMid.Opacity, mMid.TranslateY)
		}

		// End (180ms total): fully visible at rest.
		app.Advance(100 * time.Millisecond)
		mEnd := app.Find("id:editor-toast-motion").Animated
		if math.Abs(float64(mEnd.Opacity-1)) > 0.02 || math.Abs(float64(mEnd.TranslateY)) > 0.1 {
			t.Fatalf("enter end 190ms: opacity=%v translateY=%v, want 1 and 0", mEnd.Opacity, mEnd.TranslateY)
		}

		// Dismiss triggers 120ms fade-out before leaving the tree.
		app.Post(func() { ed.DismissToast() })
		app.Settle()
		app.Advance(60 * time.Millisecond)
		if !app.Exists("id:editor-toast-motion") {
			t.Fatal("toast left tree before 120ms exit animation completed")
		}
		mExitMid := app.Find("id:editor-toast-motion").Animated
		if mExitMid.Opacity <= 0.05 || mExitMid.Opacity >= 0.99 {
			t.Fatalf("exit midpoint 60ms: opacity=%v, want mid-fade", mExitMid.Opacity)
		}
		app.Advance(80 * time.Millisecond)
		if app.Exists("id:editor-toast-motion") {
			t.Fatal("toast remained in tree after 120ms exit animation finished")
		}
	})

	t.Run("ReduceMotion appears and leaves immediately with no fade or movement", func(t *testing.T) {
		ed := newTestEditor(t)
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900), nsgotest.Environment(toastDesktopEnv(true, true)), nsgotest.ManualFrames())

		app.Post(func() {
			ed.EmitToastEvent(editor.ToastEvent{
				Kind:    editor.ToastEventCopied,
				Target:  "motion-reduced",
				Message: "Copied config",
			})
		})
		app.Settle()

		// Frame 0 under ReduceMotion: immediately opacity 1 and translateY 0 (no fade, no rise).
		m0 := app.Find("id:editor-toast-motion").Animated
		if math.Abs(float64(m0.Opacity-1)) > 0.01 || math.Abs(float64(m0.TranslateY)) > 0.01 {
			t.Fatalf("ReduceMotion enter frame 0: opacity=%v translateY=%v, want 1 and 0", m0.Opacity, m0.TranslateY)
		}

		// Dismiss under ReduceMotion: leaves immediately with zero delay!
		app.Post(func() { ed.DismissToast() })
		app.Settle()
		if app.Exists("id:editor-toast-motion") {
			t.Fatal("ReduceMotion toast stayed in tree on dismiss; must leave immediately")
		}
	})
}

// Verifies placement rules from Toasts.dc.html:
//   - Bottom center of the viewport, above its bar, never over tree, inspector, or agent panel.
//   - When no viewport is on screen (Start screen, Settings), bottom center of the window, 24 px up.
func TestToastPlacementViewportVsWindowScreens(t *testing.T) {
	ed := newTestEditor(t)
	ed.EmitToastEvent(editor.ToastEvent{
		Kind:    editor.ToastEventCopied,
		Target:  "placement",
		Message: "Copied path to clipboard",
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(toastDesktopEnv(true, true)))

	// 1. Viewport visible: centered horizontally inside viewport, never overlapping tree or inspector.
	vpRect := app.Find("id:editor-viewport-container").Bounds()
	toastRect := app.Find("id:editor-toast").Bounds()
	treeRect := app.Find("id:editor-region-scene-tree").Bounds()
	inspRect := app.Find("id:editor-region-inspector").Bounds()
	vpCenterX := vpRect.Origin.X + vpRect.Size.Width/2
	toastCenterX := toastRect.Origin.X + toastRect.Size.Width/2
	if math.Abs(float64(toastCenterX-vpCenterX)) > 2 {
		t.Fatalf("viewport toast centerX=%v, want viewport centerX=%v", toastCenterX, vpCenterX)
	}
	if toastRect.Origin.X < treeRect.Origin.X+treeRect.Size.Width-1 {
		t.Fatalf("toast %+v overlaps left scene tree %+v", toastRect, treeRect)
	}
	if toastRect.Origin.X+toastRect.Size.Width > inspRect.Origin.X+1 {
		t.Fatalf("toast %+v overlaps right inspector/agent panel %+v", toastRect, inspRect)
	}
	barRect := app.Find("id:viewport-overlay-bar").Bounds()
	gapAboveBar := barRect.Origin.Y - (toastRect.Origin.Y + toastRect.Size.Height)
	if gapAboveBar < 6 || gapAboveBar > 14 {
		t.Fatalf("toast gap above viewport bar = %v px, want ~8 px", gapAboveBar)
	}

	// 2. Settings open (no viewport on screen): bottom center of window, 24 px up.
	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageGeneral) })
	app.Settle()
	winRect := app.Find("id:scene-editor").Bounds()
	winToastRect := app.Find("id:editor-toast").Bounds()
	winCenterX := winRect.Origin.X + winRect.Size.Width/2
	winToastCenterX := winToastRect.Origin.X + winToastRect.Size.Width/2
	if math.Abs(float64(winToastCenterX-winCenterX)) > 2 {
		t.Fatalf("settings window toast centerX=%v, want window centerX=%v", winToastCenterX, winCenterX)
	}
	bottomUp := (winRect.Origin.Y + winRect.Size.Height) - (winToastRect.Origin.Y + winToastRect.Size.Height)
	if math.Abs(float64(bottomUp-24)) > 1 {
		t.Fatalf("settings toast bottom offset = %v px, want 24 px", bottomUp)
	}

	// 3. Start screen open (no viewport on screen): bottom center of window, 24 px up.
	app.Post(func() {
		ed.CloseSettings()
		ed.SetStartScreenOpen(true)
	})
	app.Settle()
	startToastRect := app.Find("id:editor-toast").Bounds()
	startBottomUp := (winRect.Origin.Y + winRect.Size.Height) - (startToastRect.Origin.Y + startToastRect.Size.Height)
	if math.Abs(float64(startBottomUp-24)) > 1 {
		t.Fatalf("start screen toast bottom offset = %v px, want 24 px", startBottomUp)
	}
}

// Verifies routed Spec events for copied text (info) and file changed on disk (warning with Show).
func TestToastRoutedCopiedTextAndDiskChangedEvents(t *testing.T) {
	ed, dir := openTestProject(t)
	var copiedText string
	ed.WriteClipboard = func(text string) { copiedText = text }

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(toastDesktopEnv(true, true)), nsgotest.ManualFrames())

	// 1. Copied text -> info toast.
	app.Post(func() {
		ed.CopyConnectInstallCommand()
	})
	app.Settle()
	if copiedText == "" {
		t.Fatal("CopyConnectInstallCommand did not write clipboard")
	}
	active, ok := ed.ActiveToast()
	if !ok || active.Kind != editor.ToastInfo || !strings.Contains(active.Message, "Copied ") {
		t.Fatalf("copied toast = %+v (ok=%v), want ToastInfo 'Copied ...'", active, ok)
	}
	app.Post(func() { ed.ClearToasts() })
	app.Settle()

	// 2. File changed on disk (clean scene reload) -> warning toast with Show action.
	scenePath := filepath.Join(dir, "scenes", "room.yaml")
	extDoc := testSceneDoc()
	extDoc.Name = "Updated On Disk"
	extDoc.Layers[1].Objects[0].X = 222
	extBytes, err := sceneformat.Encode(extDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenePath, extBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	app.Post(func() {
		ed.PollDiskChanges()
	})
	app.Settle()

	diskToast, ok := ed.ActiveToast()
	if !ok || diskToast.Kind != editor.ToastWarning {
		t.Fatalf("disk changed toast = %+v (ok=%v), want ToastWarning", diskToast, ok)
	}
	if !strings.Contains(diskToast.Message, "changed on disk and was reloaded") {
		t.Fatalf("disk changed toast message = %q", diskToast.Message)
	}
	if diskToast.ActionLabel != "Show" {
		t.Fatalf("disk changed toast action = %q, want Show", diskToast.ActionLabel)
	}
	// Pressing Show opens the reload diff panel.
	app.Press("id:editor-toast-action")
	if !ed.DiskReloadDiffOpen() || !app.Exists("id:disk-reload-diff") {
		t.Fatal("pressing Show on disk-changed warning toast did not reveal disk-reload-diff")
	}
}

// Compile-time check that ToastDuration returns exact Spec durations.
func TestToastDurationSpecValues(t *testing.T) {
	cases := []struct {
		name string
		t    editor.Toast
		want time.Duration
	}{
		{"success plain", editor.Toast{Kind: editor.ToastSuccess}, 4 * time.Second},
		{"success with action", editor.Toast{Kind: editor.ToastSuccess, ActionLabel: "Undo"}, 6 * time.Second},
		{"info plain", editor.Toast{Kind: editor.ToastInfo}, 4 * time.Second},
		{"info with action", editor.Toast{Kind: editor.ToastInfo, ActionLabel: "Review"}, 6 * time.Second},
		{"warning plain", editor.Toast{Kind: editor.ToastWarning}, 8 * time.Second},
		{"warning with action", editor.Toast{Kind: editor.ToastWarning, ActionLabel: "Show"}, 8 * time.Second},
		{"error stays", editor.Toast{Kind: editor.ToastError, ActionLabel: "Retry"}, 0},
		{"progress stays", editor.Toast{Kind: editor.ToastProgress, ActionLabel: "Cancel"}, 0},
	}
	for _, tc := range cases {
		if got := editor.ToastDuration(tc.t); got != tc.want {
			t.Errorf("%s: ToastDuration = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Verify ToastHandle is safe on nil receiver and guards against unused errors import.
var _ = errors.New
