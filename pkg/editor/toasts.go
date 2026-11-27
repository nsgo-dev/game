package editor

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// ToastKind identifies the visual tone and semantic icon of an editor toast (docs/design/editor/Toasts.dc.html).
type ToastKind string

const (
	ToastSuccess  ToastKind = "success"
	ToastInfo     ToastKind = "info"
	ToastWarning  ToastKind = "warning"
	ToastError    ToastKind = "error"
	ToastProgress ToastKind = "progress"
)

// FeedbackChannel identifies the canonical home of an editor feedback event in
// docs/design/editor/Spec.dc.html ("Feedback channels"). Decisions never go in a toast.
type FeedbackChannel string

const (
	FeedbackChannelToast              FeedbackChannel = "toast"
	FeedbackChannelProgressToast      FeedbackChannel = "progress_toast"
	FeedbackChannelAgentCard          FeedbackChannel = "agent_card"
	FeedbackChannelReview             FeedbackChannel = "review"
	FeedbackChannelProblems           FeedbackChannel = "problems"
	FeedbackChannelSystemNotification FeedbackChannel = "system_notification"
)

// FeedbackChannelEntry describes one row of the Spec Feedback channels table
// (docs/design/editor/Spec.dc.html).
type FeedbackChannelEntry struct {
	Event     string
	Channel   FeedbackChannel
	Action    string
	EventKind ToastEventKind
	Stays     bool
}

// SpecFeedbackChannels returns the complete Spec Feedback channels table mapping each
// editor event to its single home.
func SpecFeedbackChannels() []FeedbackChannelEntry {
	return []FeedbackChannelEntry{
		{Event: "Review kept or reverted", Channel: FeedbackChannelToast, Action: "Undo", EventKind: ToastEventReviewKept},
		{Event: "Fix applied from Problems", Channel: FeedbackChannelToast, Action: "Undo", EventKind: ToastEventFixApplied},
		{Event: "Agent stopped with ⌘.", Channel: FeedbackChannelToast, Action: "Review", EventKind: ToastEventAgentStopped},
		{Event: "“Always” permission rule saved", Channel: FeedbackChannelToast, Action: "Show", EventKind: ToastEventPermissionSaved},
		{Event: "Copied a config, path or snippet", Channel: FeedbackChannelToast, EventKind: ToastEventCopied},
		{Event: "A file changed on disk outside the editor", Channel: FeedbackChannelToast, Action: "Show", EventKind: ToastEventDiskChanged},
		{Event: "Save or export failed", Channel: FeedbackChannelToast, Action: "Retry", EventKind: ToastEventSaveFailed, Stays: true},
		{Event: "Export running", Channel: FeedbackChannelProgressToast, Action: "Cancel", EventKind: ToastEventExportRunning, Stays: true},
		{Event: "Agent asks permission or a question", Channel: FeedbackChannelAgentCard},
		{Event: "Agent finished a request", Channel: FeedbackChannelReview},
		{Event: "Check failed, broken reference", Channel: FeedbackChannelProblems},
		{Event: "Agent asks, finishes or stops while the editor is in the background", Channel: FeedbackChannelSystemNotification},
	}
}

// ToastEventKind identifies a canonical passing-feedback event from the Spec's Feedback
// channels table (docs/design/editor/Spec.dc.html). Screens emit events through
// Editor.EmitToastEvent instead of inventing ad-hoc tones, durations, or queue keys.
type ToastEventKind string

const (
	ToastEventReviewKept      ToastEventKind = "review.kept"
	ToastEventReviewReverted  ToastEventKind = "review.reverted"
	ToastEventFixApplied      ToastEventKind = "problem.fix_applied"
	ToastEventAgentStopped    ToastEventKind = "agent.stopped"
	ToastEventPermissionSaved ToastEventKind = "permission.rule_saved"
	ToastEventCopied          ToastEventKind = "clipboard.copied"
	ToastEventDiskChanged     ToastEventKind = "disk.file_changed"
	ToastEventSaveFailed      ToastEventKind = "save.failed"
	ToastEventExportRunning   ToastEventKind = "export.running"
	ToastEventExportFailed    ToastEventKind = "export.failed"
)

// ToastEvent describes a Spec feedback-channel event routed to the editor's toast queue
// via Editor.EmitToastEvent.
type ToastEvent struct {
	// Kind selects canonical Spec defaults (toast tone, action label, shortcut, timeout,
	// and default deduplication key).
	Kind ToastEventKind
	// ID optionally overrides the default deduplication key derived from Kind and Target.
	ID string
	// Target optionally identifies the subject (for example a file path or export preset)
	// used to scope the default deduplication key when ID is empty.
	Target string
	// Message is the single-line feedback text displayed in the toast.
	Message string
	// ActionLabel optionally overrides the canonical verb button label for Kind.
	ActionLabel string
	// ActionShortcut optionally overrides the canonical keyboard shortcut hint for Kind
	// (for example "⌘Z" or "Ctrl+Z").
	ActionShortcut string
	// OnAction runs when the user activates the toast's action button.
	OnAction func()
	// OnCancel runs when the user activates Cancel on a ToastEventExportRunning toast or
	// calls ToastHandle.Cancel. If OnAction is nil on a progress event, Cancel runs OnCancel.
	OnCancel func()
	// Progress is the initial 0..1 completion fraction for ToastEventExportRunning toasts.
	Progress float32
}

// Toast describes a transient feedback message shown at the bottom center of the editor viewport
// (or bottom center of the window when no viewport is on screen, per docs/design/editor/Toasts.dc.html).
// Decisions never go in a toast; at most one action button is shown.
type Toast struct {
	// ID optionally deduplicates repeated identical events in the toast queue.
	ID string
	// Kind selects the icon shape and surface styling (ToastWarning uses Spec Warning.Fill and Warning.Edge).
	Kind ToastKind
	// Message is the single-line feedback text.
	Message string
	// ActionLabel is an optional short verb button label (for example "Undo", "Show", "Retry", or "Cancel").
	ActionLabel string
	// ActionShortcut is an optional keyboard shortcut hint shown next to ActionLabel (for example "⌘Z").
	ActionShortcut string
	// OnAction runs when the user activates the toast's action button.
	OnAction func()
	// Progress is the 0..1 completion fraction for ToastProgress toasts.
	Progress float32
	// Dismissible reports whether a Dismiss button is shown; defaults to true when Kind != ToastProgress.
	Dismissible *bool
}

func (t Toast) canDismiss() bool {
	if t.Dismissible != nil {
		return *t.Dismissible
	}
	return t.Kind != ToastProgress
}

func clampToastProgress(p float32) float32 {
	if math.IsNaN(float64(p)) || math.IsInf(float64(p), 0) || p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

// ToastDuration returns the Spec auto-dismiss timeout for t (docs/design/editor/Toasts.dc.html):
//   - ToastSuccess and ToastInfo: 4 s without an action, 6 s with an action
//   - ToastWarning: 8 s
//   - ToastError and ToastProgress: 0 (stays until retried, dismissed, canceled, or completed)
func ToastDuration(t Toast) time.Duration {
	switch t.Kind {
	case ToastError, ToastProgress:
		return 0
	case ToastWarning:
		return 8 * time.Second
	default: // ToastSuccess, ToastInfo
		if t.ActionLabel != "" {
			return 6 * time.Second
		}
		return 4 * time.Second
	}
}

// toastQueueEntry holds one queued or visible toast together with its stable mount instance ID
// and remaining auto-dismiss duration.
type toastQueueEntry struct {
	toast      Toast
	instanceID uint64
	remaining  time.Duration
	timed      bool
	onCancel   func()
	cancelOnce sync.Once
}

// ToastHandle tracks a toast emitted into an Editor so long-running work (such as an export)
// can update its percent, transition in place into a success or error toast when the work
// finishes, cancel that work, or dismiss the toast. All methods are safe on a nil handle.
type ToastHandle struct {
	ed *Editor
	id string
}

// ID returns the toast's queue/deduplication identifier.
func (h *ToastHandle) ID() string {
	if h == nil {
		return ""
	}
	return h.id
}

// Active reports whether this toast is currently visible or waiting in the editor's queue.
func (h *ToastHandle) Active() bool {
	if h == nil || h.ed == nil || h.id == "" {
		return false
	}
	return h.ed.findToastIndex(h.id) >= 0
}

// SetProgress updates a running progress toast's 0..1 completion fraction and optional message
// in place without disturbing its queue position, motion state, or keyboard focus.
func (h *ToastHandle) SetProgress(progress float32, message string) {
	if h == nil || h.ed == nil || h.id == "" {
		return
	}
	e := h.ed
	idx := e.findToastIndex(h.id)
	if idx < 0 || e.toasts[idx].toast.Kind != ToastProgress {
		return
	}
	clamped := clampToastProgress(progress)
	changed := e.toasts[idx].toast.Progress != clamped
	e.toasts[idx].toast.Progress = clamped
	if message != "" && e.toasts[idx].toast.Message != message {
		e.toasts[idx].toast.Message = message
		changed = true
	}
	if !changed {
		return
	}
	e.toastSeq++
	e.notify()
}

// Complete transitions a running progress (or active) toast in place into a ToastSuccess toast
// (for example "Exported build/macos/dungeon-run.app" with "Show in Finder"), starting its
// Spec auto-dismiss timer (4 s, or 6 s when actionLabel is non-empty).
func (h *ToastHandle) Complete(message, actionLabel, actionShortcut string, onAction func()) {
	if h == nil || h.ed == nil || h.id == "" || message == "" {
		return
	}
	e := h.ed
	next := Toast{
		ID:             h.id,
		Kind:           ToastSuccess,
		Message:        message,
		ActionLabel:    actionLabel,
		ActionShortcut: actionShortcut,
		OnAction:       onAction,
	}
	idx := e.findToastIndex(h.id)
	if idx < 0 {
		e.enqueueToast(next, nil)
		return
	}
	dur := ToastDuration(next)
	e.toasts[idx].toast = next
	e.toasts[idx].remaining = dur
	e.toasts[idx].timed = dur > 0
	e.toasts[idx].onCancel = nil
	e.toastSeq++
	if idx == 0 {
		e.syncToastTimer(true)
		e.announceToast(next.Message)
	}
	e.notify()
}

// Fail transitions a running progress (or active) toast in place into a persistent ToastError
// toast (staying until retried or dismissed) with an optional Retry action.
func (h *ToastHandle) Fail(message, actionLabel string, onRetry func()) {
	if h == nil || h.ed == nil || h.id == "" || message == "" {
		return
	}
	e := h.ed
	if actionLabel == "" && onRetry != nil {
		actionLabel = "Retry"
	}
	next := Toast{
		ID:          h.id,
		Kind:        ToastError,
		Message:     message,
		ActionLabel: actionLabel,
		OnAction:    onRetry,
	}
	idx := e.findToastIndex(h.id)
	if idx < 0 {
		e.enqueueToast(next, nil)
		return
	}
	e.toasts[idx].toast = next
	e.toasts[idx].remaining = 0
	e.toasts[idx].timed = false
	e.toasts[idx].onCancel = nil
	e.toastSeq++
	if idx == 0 {
		e.syncToastTimer(true)
		e.announceToast(next.Message)
	}
	e.notify()
}

// Cancel invokes the toast's cancellation callback (if any) once and dismisses the toast.
func (h *ToastHandle) Cancel() {
	if h == nil || h.ed == nil || h.id == "" {
		return
	}
	e := h.ed
	var fn func()
	if idx := e.findToastIndex(h.id); idx >= 0 {
		entry := e.toasts[idx]
		entry.cancelOnce.Do(func() {
			fn = entry.onCancel
		})
	}
	e.DismissToastByID(h.id)
	if fn != nil {
		fn()
	}
}

// Dismiss removes this specific toast from the visible slot or waiting queue.
func (h *ToastHandle) Dismiss() {
	if h == nil || h.ed == nil || h.id == "" {
		return
	}
	h.ed.DismissToastByID(h.id)
}

// toastRuntime owns the mounted UI scope's Spec toast timer and pause state.
//
// NSGo gap (v0.3.4, tracked in https://github.com/nsgo-dev/nsgo/issues/2507): ui.Toaster and
// base/toast.Manager cannot drive the editor's Spec toast directly for six reasons:
//  1. base/toast.Manager.Visible(1) is LIFO (newest toast replaces older ones instead of FIFO
//     queuing) and advances timeouts of hidden queued toasts behind the limit;
//  2. ui.Toaster has no ordering rule keeping errors and running progress toasts in front;
//  3. ui.Toaster exposes no waiting-queue count ("+2") and moves duplicate IDs to the newest
//     position rather than merging in place into a waiting entry;
//  4. ui.Toaster pauses only on pointer hover/touch (Stack.OnExpandedChange), not on keyboard
//     focus inside a toast or when the window leaves core.LifecycleActive (background/inactive);
//  5. ui.Toasts mounts in a full-window core.Overlay (never viewport-centered above the viewport
//     bar), hardcodes glass styling and 360 pt width, and uses core.RoleAlert rather than a
//     polite core.RoleStatus live region;
//  6. ui.Toasts binds global Escape (dismissing even when the toast is unfocused) and uses
//     400 ms / 200 ms Sonner motion instead of Spec's 180 ms in (fade + 8 px rise) / 120 ms out.
//
// Editor bridges these gaps here with a deterministic single-shot scope timer (cx.AfterFunc +
// cx.Now) and core.PresenceOf + core.Motion in spec_feedback_ui.go.
type toastRuntime struct {
	cx            *core.Scope
	hovered       bool
	touched       bool
	focused       bool
	background    bool
	timerRunning  bool
	timerInstID   uint64
	timerStarted  time.Time
	stopTimer     func() bool
	lastAnnounced string
}

func (r *toastRuntime) paused() bool {
	if r == nil {
		return false
	}
	return r.hovered || r.touched || r.focused || r.background
}

func (e *Editor) bindToastLifecycle(cx *core.Scope, rev *core.Signal[uint64]) {
	if e == nil || cx == nil {
		return
	}
	rt := &toastRuntime{
		cx:         cx,
		background: cx.Env().Lifecycle() != core.LifecycleActive,
	}
	e.toastRT = rt

	// Track window background/inactive lifecycle changes so the toast timer pauses whenever
	// the editor window is not active.
	cx.Effect(func() {
		bg := cx.Env().Lifecycle() != core.LifecycleActive
		cx.Untrack(func() {
			if e.toastRT == rt && rt.background != bg {
				e.setToastPausedFlag(&rt.background, bg)
			}
		})
	})

	// Track keyboard focus inside RegionToast so focus pauses the active toast's timer.
	cx.Effect(func() {
		focused := false
		if e.focus.ctrl != nil && e.focus.ctrl.toastFocused != nil {
			focused = e.focus.ctrl.toastFocused.Get()
		}
		cx.Untrack(func() {
			if e.toastRT == rt && rt.focused != focused {
				e.setToastPausedFlag(&rt.focused, focused)
			}
		})
	})

	// Sync the timer if a toast was queued before EditorView mounted.
	e.syncToastTimer(false)
	if active, ok := e.ActiveToast(); ok {
		e.announceToast(active.Message)
	}

	cx.Cleanup(func() {
		if rt.stopTimer != nil {
			rt.stopTimer()
			rt.stopTimer = nil
		}
		rt.timerRunning = false
		if e.toastRT == rt {
			e.toastRT = nil
		}
	})
}

func (e *Editor) setToastHovered(hovered bool) {
	if e == nil || e.toastRT == nil || e.toastRT.hovered == hovered {
		return
	}
	e.setToastPausedFlag(&e.toastRT.hovered, hovered)
}

func (e *Editor) setToastTouched(touched bool) {
	if e == nil || e.toastRT == nil || e.toastRT.touched == touched {
		return
	}
	e.setToastPausedFlag(&e.toastRT.touched, touched)
}

func (e *Editor) setToastPausedFlag(flag *bool, next bool) {
	rt := e.toastRT
	if rt == nil || *flag == next {
		return
	}
	wasPaused := rt.paused()
	if !wasPaused && rt.timerRunning {
		e.pauseActiveToastTimer()
	}
	*flag = next
	if !rt.paused() {
		e.syncToastTimer(false)
	}
}

func (e *Editor) pauseActiveToastTimer() {
	rt := e.toastRT
	if rt == nil || !rt.timerRunning {
		return
	}
	if rt.stopTimer != nil {
		rt.stopTimer()
		rt.stopTimer = nil
	}
	rt.timerRunning = false
	if len(e.toasts) > 0 && e.toasts[0].instanceID == rt.timerInstID && e.toasts[0].timed {
		elapsed := max(rt.cx.Now().Sub(rt.timerStarted), 0)
		if elapsed >= e.toasts[0].remaining {
			e.toasts[0].remaining = time.Millisecond
		} else {
			e.toasts[0].remaining -= elapsed
		}
	}
}

func (e *Editor) stopToastTimer() {
	rt := e.toastRT
	if rt == nil {
		return
	}
	if rt.stopTimer != nil {
		rt.stopTimer()
		rt.stopTimer = nil
	}
	rt.timerRunning = false
	rt.timerInstID = 0
}

func (e *Editor) syncToastTimer(forceRestart bool) {
	rt := e.toastRT
	if rt == nil || rt.cx == nil {
		return
	}
	if len(e.toasts) == 0 {
		e.stopToastTimer()
		rt.hovered = false
		rt.touched = false
		rt.lastAnnounced = ""
		return
	}
	head := e.toasts[0]
	if !head.timed || head.remaining <= 0 {
		e.stopToastTimer()
		return
	}
	if rt.paused() {
		if rt.timerRunning {
			e.pauseActiveToastTimer()
		}
		return
	}
	if rt.timerRunning && rt.timerInstID == head.instanceID && !forceRestart {
		return
	}
	if rt.stopTimer != nil {
		rt.stopTimer()
		rt.stopTimer = nil
	}
	instID := head.instanceID
	delay := head.remaining
	rt.timerRunning = true
	rt.timerInstID = instID
	rt.timerStarted = rt.cx.Now()
	rt.stopTimer = rt.cx.AfterFunc(delay, func() {
		if e.toastRT != rt {
			return
		}
		rt.timerRunning = false
		rt.stopTimer = nil
		if rt.paused() {
			// Guard against a same-tick pause race.
			return
		}
		if len(e.toasts) == 0 || e.toasts[0].instanceID != instID {
			return
		}
		e.dismissToastAt(0)
	})
}

func (e *Editor) announceToast(msg string) {
	if e == nil || e.toastRT == nil || e.toastRT.cx == nil || msg == "" {
		return
	}
	if e.toastRT.lastAnnounced == msg {
		return
	}
	e.toastRT.lastAnnounced = msg
	e.toastRT.cx.Announce(msg)
}

// ToastTimerPaused reports whether the active toast's auto-dismiss countdown is currently paused
// due to pointer hover, touch, keyboard focus inside the toast, or the window going to the
// background/inactive state.
func (e *Editor) ToastTimerPaused() bool {
	if e == nil || e.toastRT == nil {
		return false
	}
	return e.toastRT.paused()
}

// ToastRemaining returns the active toast's remaining auto-dismiss duration, or 0 if no toast
// is active or the active toast stays until dismissed/retried/completed (error or progress).
func (e *Editor) ToastRemaining() time.Duration {
	if e == nil || len(e.toasts) == 0 || !e.toasts[0].timed {
		return 0
	}
	rem := e.toasts[0].remaining
	if rt := e.toastRT; rt != nil && rt.timerRunning && rt.timerInstID == e.toasts[0].instanceID && !rt.paused() {
		elapsed := max(rt.cx.Now().Sub(rt.timerStarted), 0)
		if elapsed >= rem {
			return 0
		}
		return rem - elapsed
	}
	return rem
}

func (e *Editor) findToastIndex(id string) int {
	if e == nil || id == "" {
		return -1
	}
	for i, entry := range e.toasts {
		if entry.toast.ID == id {
			return i
		}
	}
	return -1
}

// EmitToastEvent routes a canonical event from the Spec's Feedback channels table to the
// editor's toast queue and returns a *ToastHandle for updating progress, transitioning a
// progress toast to success or error, canceling associated work, or dismissing it.
func (e *Editor) EmitToastEvent(ev ToastEvent) *ToastHandle {
	if e == nil || ev.Message == "" {
		return nil
	}
	t := e.toastFromEvent(ev)
	var onCancel func()
	if ev.Kind == ToastEventExportRunning {
		onCancel = ev.OnCancel
		if onCancel == nil {
			onCancel = ev.OnAction
		}
		if onCancel != nil {
			t.OnAction = nil
		}
	}
	return e.enqueueToast(t, onCancel)
}

func (e *Editor) toastFromEvent(ev ToastEvent) Toast {
	var kind ToastKind
	var defAction, defShortcut string
	switch ev.Kind {
	case ToastEventReviewKept, ToastEventReviewReverted, ToastEventFixApplied:
		kind = ToastSuccess
		defAction = "Undo"
		defShortcut = e.toastShortcutHint("mod+z")
	case ToastEventAgentStopped:
		kind = ToastInfo
		defAction = "Review"
	case ToastEventPermissionSaved:
		kind = ToastSuccess
		defAction = "Show"
	case ToastEventCopied:
		kind = ToastInfo
	case ToastEventDiskChanged:
		kind = ToastWarning
		defAction = "Show"
	case ToastEventSaveFailed, ToastEventExportFailed:
		kind = ToastError
		defAction = "Retry"
	case ToastEventExportRunning:
		kind = ToastProgress
		defAction = "Cancel"
	default:
		kind = ToastInfo
	}

	id := ev.ID
	if id == "" {
		if ev.Kind != "" {
			id = "toast:" + string(ev.Kind)
			if ev.Target != "" {
				id += ":" + ev.Target
			}
		}
	}

	actionLabel := ev.ActionLabel
	if actionLabel == "" && (ev.OnAction != nil || (ev.Kind == ToastEventExportRunning && ev.OnCancel != nil)) {
		actionLabel = defAction
	}
	actionShortcut := ev.ActionShortcut
	if actionShortcut == "" && actionLabel == defAction && defShortcut != "" {
		actionShortcut = defShortcut
	}

	return Toast{
		ID:             id,
		Kind:           kind,
		Message:        ev.Message,
		ActionLabel:    actionLabel,
		ActionShortcut: actionShortcut,
		OnAction:       ev.OnAction,
		Progress:       ev.Progress,
	}
}

func (e *Editor) toastShortcutHint(canonical string) string {
	os := core.OSMacOS
	if e != nil && e.scope != nil {
		if envOS := e.scope.Env().OS(); envOS != "" {
			os = envOS
		}
	}
	return FormatShortcut(canonical, os)
}

// ShowToast appends t to the editor's FIFO toast queue, merging duplicates with the same
// non-empty ID into the existing queued entry, and repaints the editor.
func (e *Editor) ShowToast(t Toast) {
	_ = e.ShowToastHandle(t)
}

// ShowToastHandle appends t to the editor's FIFO toast queue and returns a *ToastHandle
// for updating or dismissing it.
func (e *Editor) ShowToastHandle(t Toast) *ToastHandle {
	if e == nil || t.Message == "" {
		return nil
	}
	return e.enqueueToast(t, nil)
}

func (e *Editor) enqueueToast(t Toast, onCancel func()) *ToastHandle {
	if e == nil || t.Message == "" {
		return nil
	}
	if t.Kind == "" {
		t.Kind = ToastInfo
	}
	if t.ID == "" {
		e.toastNextInst++
		t.ID = fmt.Sprintf("toast-%d", e.toastNextInst)
	}
	if t.Kind == ToastProgress {
		t.Progress = clampToastProgress(t.Progress)
	}

	h := &ToastHandle{ed: e, id: t.ID}
	if t.Kind == ToastProgress && onCancel != nil && t.OnAction == nil {
		t.OnAction = func() {
			h.Cancel()
		}
	}

	dur := ToastDuration(t)
	if idx := e.findToastIndex(t.ID); idx >= 0 {
		// Merge repeat of the same event into its existing slot without changing queue order
		// or jumping ahead of an active error or running progress toast.
		entry := e.toasts[idx]
		entry.toast = t
		if t.Kind == ToastProgress {
			entry.remaining = 0
			entry.timed = false
			if onCancel != nil {
				entry.onCancel = onCancel
			}
		} else {
			entry.remaining = dur
			entry.timed = dur > 0
			entry.onCancel = nil
		}
		e.toastSeq++
		if idx == 0 {
			e.syncToastTimer(t.Kind != ToastProgress)
			if t.Kind != ToastProgress {
				e.announceToast(t.Message)
			}
		}
		e.notify()
		return h
	}

	e.toastNextInst++
	entry := &toastQueueEntry{
		toast:      t,
		instanceID: e.toastNextInst,
		remaining:  dur,
		timed:      dur > 0,
		onCancel:   onCancel,
	}
	e.toasts = append(e.toasts, entry)
	e.toastSeq++
	if len(e.toasts) == 1 {
		e.syncToastTimer(true)
		e.announceToast(t.Message)
	}
	e.notify()
	return h
}

// DismissToast removes the currently visible toast and advances the queue.
func (e *Editor) DismissToast() {
	if e == nil || len(e.toasts) == 0 {
		return
	}
	e.dismissToastAt(0)
}

// DismissToastByID removes the toast with id from the visible slot or waiting queue.
func (e *Editor) DismissToastByID(id string) {
	if e == nil || id == "" {
		return
	}
	idx := e.findToastIndex(id)
	if idx < 0 {
		return
	}
	e.dismissToastAt(idx)
}

func (e *Editor) dismissToastAt(idx int) {
	if e == nil || idx < 0 || idx >= len(e.toasts) {
		return
	}
	e.toasts = append(e.toasts[:idx], e.toasts[idx+1:]...)
	e.toastSeq++
	if idx == 0 {
		if e.toastRT != nil {
			e.toastRT.hovered = false
			e.toastRT.touched = false
		}
		e.syncToastTimer(true)
		if len(e.toasts) > 0 {
			e.announceToast(e.toasts[0].toast.Message)
		}
	}
	e.notify()
}

// dismissFocusedToast handles Esc while keyboard focus is inside RegionToast:
// dismisses the focused toast (when dismissible) and returns focus to where it was
// before entering the toast region.
func (e *Editor) dismissFocusedToast() {
	if e == nil {
		return
	}
	if active, ok := e.ActiveToast(); ok && active.canDismiss() {
		e.DismissToast()
	}
	if e.focus.ctrl != nil {
		e.focus.ctrl.restoreFocusFromToast()
	}
}

// ClearToasts removes all active and queued toasts.
func (e *Editor) ClearToasts() {
	if e == nil || len(e.toasts) == 0 {
		return
	}
	e.toasts = nil
	e.toastSeq++
	e.syncToastTimer(true)
	e.notify()
}

// ActiveToast returns the currently visible toast at the head of the queue, if any.
func (e *Editor) ActiveToast() (Toast, bool) {
	if e == nil || len(e.toasts) == 0 {
		return Toast{}, false
	}
	return e.toasts[0].toast, true
}

func (e *Editor) activeToastInstanceID() uint64 {
	if e == nil || len(e.toasts) == 0 {
		return 0
	}
	return e.toasts[0].instanceID
}

// HasActiveToast reports whether a toast is currently shown and therefore part of
// the F6 region cycle (RegionToast).
func (e *Editor) HasActiveToast() bool {
	_, ok := e.ActiveToast()
	return ok
}

// QueuedToastCount returns how many additional toasts are waiting behind the visible toast.
func (e *Editor) QueuedToastCount() int {
	if e == nil || len(e.toasts) <= 1 {
		return 0
	}
	return len(e.toasts) - 1
}

// QueuedToasts returns a snapshot of the waiting toasts behind the active toast, in FIFO order.
func (e *Editor) QueuedToasts() []Toast {
	if e == nil || len(e.toasts) <= 1 {
		return nil
	}
	out := make([]Toast, len(e.toasts)-1)
	for i := 1; i < len(e.toasts); i++ {
		out[i-1] = e.toasts[i].toast
	}
	return out
}

// viewportVisibleOnScreen reports whether the editor's central viewport is currently visible
// on screen. When false (for example on the Start screen, Settings, Export, or other
// full-window screens), toasts anchor at bottom center of the window, 24 px up.
func (e *Editor) viewportVisibleOnScreen() bool {
	if e == nil {
		return false
	}
	if e.StartScreenOpen() ||
		e.FirstRunFullScreenOpen() ||
		e.ReviewOpen() ||
		e.MergeConflictOpen() ||
		e.DataTableReviewOpen() ||
		e.ChangesOpen() ||
		e.CodeReviewOpen() ||
		e.BuildReleaseScreenOpen() ||
		e.UsagesScreenOpen() ||
		e.PlaytestReportOpen() ||
		e.AssetGeneratorOpen() ||
		e.HUDLayoutOpen() ||
		(e.CutsceneOpen() && !e.Playing()) ||
		(e.SettingsOpen() && !e.Playing()) {
		return false
	}
	return true
}

const explicitSaveFailedToastID = "toast:save.failed"

// notifyExplicitSaveFailure routes a failed explicit save (⌘S, File › Save, or toolbar Save)
// to a persistent Spec error toast with a Retry action. Autosave failures stay in the status bar
// per Checkpoints.dc.html and never call this helper; disk conflicts open saveConflictDialog.
func (e *Editor) notifyExplicitSaveFailure(target string, err error, retry func() error) {
	if e == nil || err == nil {
		return
	}
	if errors.Is(err, ErrPlaying) || errors.Is(err, ErrDiskConflict) {
		return
	}
	label := strings.TrimSpace(target)
	if label == "" {
		label = e.SceneLabel()
	}
	if label == "" {
		label = "scene"
	}
	reason := formatToastSaveErrorReason(err)
	msg := fmt.Sprintf("Couldn't save %s: %s", label, reason)
	e.EmitToastEvent(ToastEvent{
		Kind:        ToastEventSaveFailed,
		ID:          explicitSaveFailedToastID,
		Message:     msg,
		ActionLabel: "Retry",
		OnAction: func() {
			if retry == nil {
				return
			}
			if rerr := retry(); rerr == nil {
				e.clearExplicitSaveFailedToast()
			} else {
				e.Status = "Save error: " + rerr.Error()
				e.notifyExplicitSaveFailure(label, rerr, retry)
			}
		},
	})
}

func (e *Editor) clearExplicitSaveFailedToast() {
	if e == nil {
		return
	}
	e.DismissToastByID(explicitSaveFailedToastID)
}

func formatToastSaveErrorReason(err error) string {
	switch classifySaveError(err) {
	case SaveErrorDiskFull:
		return "the disk is full"
	case SaveErrorReadOnly:
		return "folder is read-only"
	default:
		return strings.TrimSpace(err.Error())
	}
}

func (e *Editor) revealInFileManagerLabel() string {
	osName := core.OSMacOS
	if e != nil && e.scope != nil {
		if envOS := e.scope.Env().OS(); envOS != "" {
			osName = envOS
		}
	} else {
		switch runtime.GOOS {
		case "windows":
			osName = core.OSWindows
		case "linux":
			osName = core.OSLinux
		}
	}
	switch osName {
	case core.OSWindows:
		return "Show in Explorer"
	case core.OSLinux:
		return "Show in Files"
	default:
		return "Show in Finder"
	}
}
