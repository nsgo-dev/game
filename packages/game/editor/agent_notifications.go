package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/core"
)

// AgentWaitingNotifyDelay is how long an unanswered Waiting-for-you permission or question request
// stays open in the foreground before also sending a system notification (docs/design/editor/AgentStates.dc.html).
const AgentWaitingNotifyDelay = 30 * time.Second

const (
	agentTaskNotificationPrefix = "agent-task:"
	maxHeldTaskNotifications    = 64
	maxNotifyHeaderRunes        = 120
	maxNotifyTitleRunes         = 120
	maxNotifyBodyRunes          = 240
	maxNotifyActionRunes        = 48
)

// IsNotifiable reports whether s is one of the three states allowed to reach the person outside
// the editor (waiting for permission, ready to review, or failed). Running, queued, stopped, and
// done never send system notifications and never add to the Dock badge.
func (s TaskState) IsNotifiable() bool {
	switch s {
	case TaskNeedsYou, TaskReadyToReview, TaskFailed:
		return true
	default:
		return false
	}
}

// AgentTaskCounts summarizes active agent tasks by state across both background tasks (#1022)
// and foreground Agent panel turns.
type AgentTaskCounts struct {
	Waiting       int
	ReadyToReview int
	Failed        int
	Running       int
	Queued        int
}

// BadgeCount returns the Dock badge count: waiting + ready to review + failed.
// Running and queued tasks never add to the badge.
func (c AgentTaskCounts) BadgeCount() int {
	return c.Waiting + c.ReadyToReview + c.Failed
}

// PlayTaskDotState describes the single corner dot shown during Play mode
// (docs/design/editor/Tasks.dc.html: "Nothing pops over a running game. One dot in the corner,
// in the color of the most urgent state; the label shows only on hover.").
type PlayTaskDotState struct {
	// Visible reports whether any attention-needing task (failed, waiting, or ready to review) exists.
	Visible bool
	// UrgentState is the highest-urgency active state (TaskFailed > TaskNeedsYou > TaskReadyToReview).
	UrgentState TaskState
	// UrgentCount is the number of tasks in UrgentState.
	UrgentCount int
	// AriaLabel is the accessible label for the corner dot (for example "1 task needs you").
	AriaLabel string
	// HoverLabel is the tooltip pill shown only on hover or keyboard focus
	// (for example "1 task needs you · stop Play to open").
	HoverLabel string
}

// Color returns the Spec token color for the dot's most urgent state (never raw hex).
func (d PlayTaskDotState) Color(p SpecPalette) core.Color {
	switch d.UrgentState {
	case TaskFailed:
		return p.Removed.Fg
	case TaskNeedsYou:
		return p.Warning.Fg
	case TaskReadyToReview:
		return p.Accent.Fg
	default:
		return p.Agent.Fg
	}
}

// AgentTaskNotification is the structured payload built for one of the three allowed
// system notifications (waiting for permission, ready to review, or failed).
type AgentTaskNotification struct {
	ID              string
	TaskID          string
	State           TaskState
	Header          string
	Title           string
	Body            string
	PrimaryAction   string
	SecondaryAction string
	ScenePath       string
}

// CoreNotification converts n into the NSGo core.Notification passed to cx.ShowNotification.
func (n AgentTaskNotification) CoreNotification() core.Notification {
	title := sanitizeNotificationText(n.Title, maxNotifyTitleRunes)
	header := sanitizeNotificationText(n.Header, maxNotifyHeaderRunes)
	if header != "" {
		if title != "" {
			title = header + " · " + title
		} else {
			title = header
		}
	}
	body := sanitizeNotificationText(n.Body, maxNotifyBodyRunes)
	primary := sanitizeNotificationText(n.PrimaryAction, maxNotifyActionRunes)
	if primary != "" {
		if body != "" {
			body += " · " + primary
		} else {
			body = primary
		}
	}
	return core.Notification{
		ID:    n.ID,
		Title: title,
		Body:  body,
	}
}

// sanitizeNotificationText strips ASCII/Unicode control characters (preventing newline or
// control-sequence injection into OS notification banners), collapses runs of whitespace,
// and bounds the rune length with an ellipsis.
func sanitizeNotificationText(s string, maxRunes int) string {
	if s == "" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	collapsed := strings.Join(strings.Fields(cleaned), " ")
	if maxRunes <= 0 {
		return collapsed
	}
	runes := []rune(collapsed)
	if len(runes) <= maxRunes {
		return collapsed
	}
	if maxRunes == 1 {
		return "…"
	}
	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

type turnNotifyEntry struct {
	task     AgentTask
	filePath string
	openedAt time.Time
}

type heldTaskNotification struct {
	taskID string
	state  TaskState
	notif  core.Notification
}

type waitingTimerEntry struct {
	seq    uint64
	cancel func()
}

// agentNotifyState holds notification, Dock badge, and Play-mode dot tracking.
// Lock ordering invariant: never hold agentNotifyState.mu while acquiring e.tasks.mu
// or invoking core.Scope methods, e.notify(), e.scheduleAgentAfter(), or user hooks.
type agentNotifyState struct {
	mu sync.Mutex

	// turnTasks mirrors direct Agent panel turns / waiting requests (agent.go) using the
	// canonical AgentTask struct without persisting ephemeral panel turns into .nsgo/threads/.
	turnTasks     []turnNotifyEntry
	focusedTaskID string
	waitingTaskID string

	lifecycle core.Lifecycle
	gen       uint64

	waitingTimers map[string]waitingTimerEntry
	nextTimerSeq  uint64
	notifiedState map[string]TaskState
	shownIDs      map[string]bool

	heldNotifications        []heldTaskNotification
	pendingPermNotifications []heldTaskNotification

	notifPermRequested bool
	notifPermPending   bool
	notifPerm          core.NotificationPermission

	lastBadge   int
	badgeSynced bool
}

func (n *agentNotifyState) initDefaults() {
	n.mu.Lock()
	n.lifecycle = core.LifecycleActive
	n.notifPerm = core.NotificationsNotAsked
	n.mu.Unlock()
}

func (e *Editor) setWaitingTurnTaskID(id string) {
	if e == nil {
		return
	}
	e.agent.notify.mu.Lock()
	e.agent.notify.waitingTaskID = id
	e.agent.notify.mu.Unlock()
}

func (e *Editor) waitingTurnTaskID() string {
	if e == nil {
		return ""
	}
	e.agent.notify.mu.Lock()
	id := e.agent.notify.waitingTaskID
	e.agent.notify.mu.Unlock()
	return id
}

func (e *Editor) clearWaitingTurnTaskID() string {
	if e == nil {
		return ""
	}
	e.agent.notify.mu.Lock()
	id := e.agent.notify.waitingTaskID
	e.agent.notify.waitingTaskID = ""
	e.agent.notify.mu.Unlock()
	return id
}

func (e *Editor) turnNotificationTaskByID(id string) (AgentTask, string, bool) {
	if e == nil || id == "" {
		return AgentTask{}, "", false
	}
	e.agent.notify.mu.Lock()
	defer e.agent.notify.mu.Unlock()
	for i := range e.agent.notify.turnTasks {
		if e.agent.notify.turnTasks[i].task.ID == id {
			return e.agent.notify.turnTasks[i].task.clone(), e.agent.notify.turnTasks[i].filePath, true
		}
	}
	return AgentTask{}, "", false
}

func (e *Editor) upsertTurnNotificationTask(task AgentTask, filePath string, openedAt time.Time) {
	if e == nil || strings.TrimSpace(task.ID) == "" {
		return
	}
	if task.State == "" {
		task.State = TaskRunning
	}
	now := e.nowTime()
	if task.State == TaskNeedsYou && openedAt.IsZero() {
		openedAt = now
	}
	if task.ScenePath == "" && filePath == "" {
		task.ScenePath = e.ScenePath
		filePath = e.FilePath
	}

	e.agent.notify.mu.Lock()
	idx := -1
	var prevState TaskState
	for i := range e.agent.notify.turnTasks {
		if e.agent.notify.turnTasks[i].task.ID == task.ID {
			idx = i
			prevState = e.agent.notify.turnTasks[i].task.State
			break
		}
	}
	if idx >= 0 {
		prev := e.agent.notify.turnTasks[idx]
		if task.Title == "" {
			task.Title = prev.task.Title
		}
		if task.ScenePath == "" {
			task.ScenePath = prev.task.ScenePath
		}
		if filePath == "" {
			filePath = prev.filePath
		}
		if task.State == TaskNeedsYou && prevState == TaskNeedsYou && !prev.openedAt.IsZero() {
			openedAt = prev.openedAt
		}
		e.agent.notify.turnTasks[idx] = turnNotifyEntry{
			task:     task.clone(),
			filePath: filePath,
			openedAt: openedAt,
		}
	} else {
		e.agent.notify.turnTasks = append(e.agent.notify.turnTasks, turnNotifyEntry{
			task:     task.clone(),
			filePath: filePath,
			openedAt: openedAt,
		})
		if len(e.agent.notify.turnTasks) > maxHeldTaskNotifications {
			e.agent.notify.turnTasks = slices.Delete(e.agent.notify.turnTasks, 0, len(e.agent.notify.turnTasks)-maxHeldTaskNotifications)
		}
	}
	taskCopy := task.clone()
	e.agent.notify.mu.Unlock()

	e.onAgentTaskStateUpdated(taskCopy, prevState, openedAt)
	e.syncDockBadgeIfAttached()
	e.notify()
}

func (e *Editor) removeTurnNotificationTask(id string) {
	if e == nil || id == "" {
		return
	}
	e.clearTaskNotificationForID(id)
	e.agent.notify.mu.Lock()
	e.agent.notify.turnTasks = slices.DeleteFunc(e.agent.notify.turnTasks, func(entry turnNotifyEntry) bool {
		return entry.task.ID == id
	})
	if e.agent.notify.waitingTaskID == id {
		e.agent.notify.waitingTaskID = ""
	}
	e.agent.notify.mu.Unlock()
	e.syncDockBadgeIfAttached()
	e.notify()
}

func (e *Editor) agentProjectDisplayName() string {
	if e == nil {
		return ""
	}
	if e.Project != nil && strings.TrimSpace(e.Project.Name) != "" {
		return strings.TrimSpace(e.Project.Name)
	}
	if e.ProjectPath != "" {
		base := filepath.Base(filepath.Dir(e.ProjectPath))
		if base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return ""
}

func (e *Editor) agentNotificationHeader() string {
	app := strings.TrimSpace(e.AppName)
	if app == "" {
		app = "NSGo Editor"
	}
	if proj := e.agentProjectDisplayName(); proj != "" {
		return sanitizeNotificationText(app+" · "+proj, maxNotifyHeaderRunes)
	}
	return sanitizeNotificationText(app, maxNotifyHeaderRunes)
}

// BuildAgentTaskNotification builds the system notification for task when task.State is one
// of the three notifiable states (TaskNeedsYou, TaskReadyToReview, or TaskFailed).
// It returns (_, false) for running, queued, stopped, done, dismissed, or empty-ID tasks.
func (e *Editor) BuildAgentTaskNotification(task AgentTask) (AgentTaskNotification, bool) {
	if strings.TrimSpace(task.ID) == "" || task.Dismissed || !task.State.IsNotifiable() {
		return AgentTaskNotification{}, false
	}
	header := e.agentNotificationHeader()
	taskTitle := sanitizeNotificationText(task.Title, maxNotifyTitleRunes)

	var title, detail, body, primary, secondary string
	switch task.State {
	case TaskNeedsYou:
		title = sanitizeNotificationText(task.PermissionPrompt, maxNotifyTitleRunes)
		if title == "" {
			title = sanitizeNotificationText(e.T("agent.notify.waitingTitle"), maxNotifyTitleRunes)
		}
		detail = sanitizeNotificationText(task.PermissionDetail, maxNotifyBodyRunes)
		if detail == "" {
			detail = sanitizeNotificationText(task.StepSummary, maxNotifyBodyRunes)
		}
		primary = sanitizeNotificationText(e.T("agent.notify.actionAllowOnce"), maxNotifyActionRunes)
		secondary = sanitizeNotificationText(e.T("agent.notify.actionOpen"), maxNotifyActionRunes)
		switch {
		case taskTitle != "" && detail != "":
			body = sanitizeNotificationText(taskTitle+" · "+detail, maxNotifyBodyRunes)
		case detail != "":
			body = detail
		case taskTitle != "":
			body = taskTitle
		default:
			body = title
		}
	case TaskReadyToReview:
		title = sanitizeNotificationText(e.T("agent.notify.reviewTitle"), maxNotifyTitleRunes)
		if task.Review != nil && strings.TrimSpace(task.Review.Summary) != "" {
			detail = sanitizeNotificationText(task.Review.Summary, maxNotifyBodyRunes)
		} else {
			detail = sanitizeNotificationText(task.StepSummary, maxNotifyBodyRunes)
		}
		primary = sanitizeNotificationText(e.T("agent.notify.actionReview"), maxNotifyActionRunes)
		secondary = sanitizeNotificationText(e.T("agent.notify.actionLater"), maxNotifyActionRunes)
		switch {
		case taskTitle != "" && detail != "":
			body = sanitizeNotificationText(taskTitle+" · "+detail, maxNotifyBodyRunes)
		case taskTitle != "":
			body = taskTitle
		case detail != "":
			body = detail
		default:
			body = title
		}
	case TaskFailed:
		title = sanitizeNotificationText(e.T("agent.notify.failedTitle"), maxNotifyTitleRunes)
		detail = sanitizeNotificationText(task.Error, maxNotifyBodyRunes)
		if detail == "" {
			detail = sanitizeNotificationText(task.StepSummary, maxNotifyBodyRunes)
		}
		primary = sanitizeNotificationText(e.T("agent.notify.actionOpen"), maxNotifyActionRunes)
		secondary = sanitizeNotificationText(e.T("agent.notify.actionRetry"), maxNotifyActionRunes)
		switch {
		case taskTitle != "" && detail != "":
			body = sanitizeNotificationText(taskTitle+" · "+detail, maxNotifyBodyRunes)
		case taskTitle != "":
			body = taskTitle
		case detail != "":
			body = detail
		default:
			body = title
		}
	default:
		return AgentTaskNotification{}, false
	}

	return AgentTaskNotification{
		ID:              agentTaskNotificationPrefix + task.ID,
		TaskID:          task.ID,
		State:           task.State,
		Header:          header,
		Title:           title,
		Body:            body,
		PrimaryAction:   primary,
		SecondaryAction: secondary,
		ScenePath:       task.ScenePath,
	}, true
}

func (e *Editor) windowInBackground() bool {
	if e == nil {
		return false
	}
	if cx := e.agent.getScope(); cx != nil && !cx.Disposed() {
		lc := core.Untracked(cx, func() core.Lifecycle { return cx.Env().Lifecycle() })
		e.agent.notify.mu.Lock()
		e.agent.notify.lifecycle = lc
		e.agent.notify.mu.Unlock()
		return lc != core.LifecycleActive
	}
	e.agent.notify.mu.Lock()
	lc := e.agent.notify.lifecycle
	e.agent.notify.mu.Unlock()
	return lc != core.LifecycleActive
}

func (e *Editor) setWindowLifecycle(lc core.Lifecycle) {
	if e == nil {
		return
	}
	e.agent.notify.mu.Lock()
	e.agent.notify.lifecycle = lc
	e.agent.notify.mu.Unlock()
}

func (e *Editor) cancelTaskWaitingTimer(taskID string) {
	if e == nil || taskID == "" {
		return
	}
	e.agent.notify.mu.Lock()
	entry, ok := e.agent.notify.waitingTimers[taskID]
	if ok {
		delete(e.agent.notify.waitingTimers, taskID)
	}
	e.agent.notify.mu.Unlock()
	if ok && entry.cancel != nil {
		entry.cancel()
	}
}

func (e *Editor) stopAllTaskNotificationTimers() {
	if e == nil {
		return
	}
	e.agent.notify.mu.Lock()
	cancels := make([]func(), 0, len(e.agent.notify.waitingTimers))
	for id, entry := range e.agent.notify.waitingTimers {
		if entry.cancel != nil {
			cancels = append(cancels, entry.cancel)
		}
		delete(e.agent.notify.waitingTimers, id)
	}
	e.agent.notify.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (e *Editor) clearHeldNotification(taskID string) {
	if e == nil || taskID == "" {
		return
	}
	e.agent.notify.mu.Lock()
	e.agent.notify.heldNotifications = slices.DeleteFunc(e.agent.notify.heldNotifications, func(h heldTaskNotification) bool {
		return h.taskID == taskID
	})
	e.agent.notify.pendingPermNotifications = slices.DeleteFunc(e.agent.notify.pendingPermNotifications, func(h heldTaskNotification) bool {
		return h.taskID == taskID
	})
	e.agent.notify.mu.Unlock()
}

func (e *Editor) closeTaskSystemNotification(taskID string) {
	if e == nil || taskID == "" {
		return
	}
	notifID := agentTaskNotificationPrefix + taskID
	e.agent.notify.mu.Lock()
	shown := e.agent.notify.shownIDs != nil && e.agent.notify.shownIDs[notifID]
	if shown {
		delete(e.agent.notify.shownIDs, notifID)
	}
	e.agent.notify.mu.Unlock()
	if !shown {
		return
	}
	if cx := e.agent.getScope(); cx != nil && !cx.Disposed() {
		cx.CloseNotification(notifID)
	}
}

func (e *Editor) clearTaskNotificationForID(taskID string) {
	if e == nil || taskID == "" {
		return
	}
	e.cancelTaskWaitingTimer(taskID)
	e.clearHeldNotification(taskID)
	e.closeTaskSystemNotification(taskID)
	e.agent.notify.mu.Lock()
	if e.agent.notify.notifiedState != nil {
		delete(e.agent.notify.notifiedState, taskID)
	}
	e.agent.notify.mu.Unlock()
}

// resetTaskNotificationTrackingForTasks clears pending timers, held/pending notifications,
// and shown OS notifications when tasks are reset or loaded from disk on project open
// (preventing restored thread tasks from spamming old system notifications while still
// letting them contribute to the Dock badge and Play-mode dot).
func (e *Editor) resetTaskNotificationTrackingForTasks() {
	if e == nil {
		return
	}
	e.stopAllTaskNotificationTimers()
	e.agent.notify.mu.Lock()
	e.agent.notify.gen++
	shown := make([]string, 0, len(e.agent.notify.shownIDs))
	for notifID := range e.agent.notify.shownIDs {
		shown = append(shown, notifID)
	}
	e.agent.notify.notifiedState = nil
	e.agent.notify.shownIDs = nil
	e.agent.notify.heldNotifications = nil
	e.agent.notify.pendingPermNotifications = nil
	e.agent.notify.mu.Unlock()
	if cx := e.agent.getScope(); cx != nil && !cx.Disposed() {
		for _, notifID := range shown {
			cx.CloseNotification(notifID)
		}
	}
}

func (e *Editor) resetTaskNotificationsForProject() {
	if e == nil {
		return
	}
	e.resetTaskNotificationTrackingForTasks()
	e.agent.notify.mu.Lock()
	e.agent.notify.turnTasks = nil
	e.agent.notify.focusedTaskID = ""
	e.agent.notify.waitingTaskID = ""
	e.agent.notify.mu.Unlock()
	e.syncDockBadgeIfAttached()
}

func (e *Editor) syncDockBadgeIfAttached() {
	if e == nil {
		return
	}
	if cx := e.agent.getScope(); cx != nil && !cx.Disposed() {
		e.syncDockBadge(cx)
	}
}

func (e *Editor) syncDockBadge(cx *core.Scope) {
	if e == nil || cx == nil || cx.Disposed() {
		return
	}
	count := e.AgentDockBadgeCount()
	e.agent.notify.mu.Lock()
	if e.agent.notify.badgeSynced && e.agent.notify.lastBadge == count {
		e.agent.notify.mu.Unlock()
		return
	}
	e.agent.notify.badgeSynced = true
	e.agent.notify.lastBadge = count
	e.agent.notify.mu.Unlock()
	cx.SetBadge(count)
}

func (e *Editor) resetDockBadgeOnCleanup(cx *core.Scope) {
	if e == nil {
		return
	}
	e.stopAllTaskNotificationTimers()
	e.agent.notify.mu.Lock()
	e.agent.notify.gen++
	e.agent.notify.heldNotifications = nil
	e.agent.notify.pendingPermNotifications = nil
	if e.agent.notify.notifPermPending {
		// Unblock a future remounted EditorView if the OS permission callback never fires for the disposed scope.
		e.agent.notify.notifPermPending = false
		e.agent.notify.notifPermRequested = false
	}
	clearBadge := e.agent.notify.badgeSynced && e.agent.notify.lastBadge > 0
	e.agent.notify.badgeSynced = false
	e.agent.notify.lastBadge = 0
	e.agent.notify.mu.Unlock()
	if clearBadge && cx != nil && !cx.Disposed() {
		cx.SetBadge(0)
	}
}

// ClearAgentTasks clears ephemeral Agent panel turn notifications, cancels waiting timers,
// closes shown task notifications, and updates the Dock badge.
func (e *Editor) ClearAgentTasks() {
	if e == nil {
		return
	}
	e.resetTaskNotificationsForProject()
	e.notify()
}

// AgentTaskByID looks up one active agent task by ID across both the background task list
// (#1022) and ephemeral Agent panel turns.
func (e *Editor) AgentTaskByID(id string) (AgentTask, bool) {
	if e == nil || id == "" {
		return AgentTask{}, false
	}
	if t, ok := e.Task(id); ok {
		return t, true
	}
	if t, _, ok := e.turnNotificationTaskByID(id); ok {
		return t, true
	}
	return AgentTask{}, false
}

// AgentTaskCounts returns counts of active non-dismissed tasks grouped by state across both
// the background task list (#1022) and ephemeral Agent panel turns (deduplicated by ID).
func (e *Editor) AgentTaskCounts() AgentTaskCounts {
	if e == nil {
		return AgentTaskCounts{}
	}
	var c AgentTaskCounts
	seen := make(map[string]struct{})
	addTask := func(t AgentTask) {
		if t.Dismissed || t.ID == "" {
			return
		}
		if _, dup := seen[t.ID]; dup {
			return
		}
		seen[t.ID] = struct{}{}
		switch t.State {
		case TaskNeedsYou:
			c.Waiting++
		case TaskReadyToReview:
			c.ReadyToReview++
		case TaskFailed:
			c.Failed++
		case TaskRunning:
			c.Running++
		case TaskQueued:
			c.Queued++
		}
	}
	for _, t := range e.Tasks() {
		addTask(t)
	}
	e.agent.notify.mu.Lock()
	turns := make([]AgentTask, len(e.agent.notify.turnTasks))
	for i := range e.agent.notify.turnTasks {
		turns[i] = e.agent.notify.turnTasks[i].task.clone()
	}
	e.agent.notify.mu.Unlock()
	for _, t := range turns {
		addTask(t)
	}
	return c
}

// AgentDockBadgeCount returns the Dock badge count (waiting + ready to review + failed).
// Running and queued tasks never add to the badge.
func (e *Editor) AgentDockBadgeCount() int {
	return e.AgentTaskCounts().BadgeCount()
}

// PlayTaskDot returns the Play-mode corner dot state: visible only when at least one
// failed, waiting, or ready-to-review task exists, colored by the most urgent state
// (TaskFailed > TaskNeedsYou > TaskReadyToReview), with a hover label naming the count
// and reminding the person to stop Play to open it.
func (e *Editor) PlayTaskDot() PlayTaskDotState {
	if e == nil {
		return PlayTaskDotState{}
	}
	c := e.AgentTaskCounts()
	switch {
	case c.Failed > 0:
		aria := e.Plural("agent.playDot.failed", c.Failed)
		return PlayTaskDotState{
			Visible:     true,
			UrgentState: TaskFailed,
			UrgentCount: c.Failed,
			AriaLabel:   aria,
			HoverLabel:  e.T("agent.playDot.hoverFormat", l10n.Param("summary", aria)),
		}
	case c.Waiting > 0:
		aria := e.Plural("agent.playDot.waiting", c.Waiting)
		return PlayTaskDotState{
			Visible:     true,
			UrgentState: TaskNeedsYou,
			UrgentCount: c.Waiting,
			AriaLabel:   aria,
			HoverLabel:  e.T("agent.playDot.hoverFormat", l10n.Param("summary", aria)),
		}
	case c.ReadyToReview > 0:
		aria := e.Plural("agent.playDot.review", c.ReadyToReview)
		return PlayTaskDotState{
			Visible:     true,
			UrgentState: TaskReadyToReview,
			UrgentCount: c.ReadyToReview,
			AriaLabel:   aria,
			HoverLabel:  e.T("agent.playDot.hoverFormat", l10n.Param("summary", aria)),
		}
	default:
		return PlayTaskDotState{}
	}
}

// HeldAgentNotifications returns a snapshot of system notifications currently held until Play stops.
func (e *Editor) HeldAgentNotifications() []core.Notification {
	if e == nil {
		return nil
	}
	e.agent.notify.mu.Lock()
	defer e.agent.notify.mu.Unlock()
	if len(e.agent.notify.heldNotifications) == 0 {
		return nil
	}
	out := make([]core.Notification, len(e.agent.notify.heldNotifications))
	for i, h := range e.agent.notify.heldNotifications {
		out[i] = h.notif
	}
	return out
}

// FocusedAgentTaskID returns the ID of the agent task most recently opened or focused
// (for example by clicking a system notification).
func (e *Editor) FocusedAgentTaskID() string {
	if e == nil {
		return ""
	}
	e.agent.notify.mu.Lock()
	id := e.agent.notify.focusedTaskID
	e.agent.notify.mu.Unlock()
	if id != "" {
		return id
	}
	return e.FocusedTaskID()
}

// HandleNotificationPress handles an NSGo notification press callback ID. It returns true
// when id names an agent task notification and opens that task in its scene.
func (e *Editor) HandleNotificationPress(id string) bool {
	if e == nil || !strings.HasPrefix(id, agentTaskNotificationPrefix) {
		return false
	}
	taskID := strings.TrimPrefix(id, agentTaskNotificationPrefix)
	if taskID == "" {
		return false
	}
	_ = e.OpenAgentTask(taskID)
	return true
}

// OpenAgentTask focuses the editor window, stops Play if running, dismisses obstructing
// overlays/panels (Settings, History, Problem agent panel, Ask-in-place, Assets mode),
// opens the task's scene (strictly confined inside the open project root), switches to
// the Tasks or Agent panel, and focuses the task (and its open request card when waiting).
func (e *Editor) OpenAgentTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	bgTask, isBGTask := e.Task(id)
	var turnTask AgentTask
	var turnFilePath string
	isTurnTask := false
	if !isBGTask {
		turnTask, turnFilePath, isTurnTask = e.turnNotificationTaskByID(id)
	}
	if !isBGTask && !isTurnTask {
		return fmt.Errorf("editor: unknown agent task %q", id)
	}

	if e.OnFocusWindow != nil {
		e.OnFocusWindow()
	}
	// Clear any held notification for the task being opened before stopping Play so Stop does not re-fire it.
	e.clearHeldNotification(id)
	if e.playing {
		if err := e.Stop(); err != nil {
			return err
		}
	}
	e.settingsOpen = false
	e.SetStartScreenOpen(false)
	e.CloseAskInPlace()
	e.SetProblemAgentPanelOpen(false)
	e.SetHistoryPanelOpen(false)
	e.FinishSketch()
	if e.LeftRailMode() == LeftRailAssets {
		e.SetLeftRailMode(LeftRailScene)
	}

	var opened AgentTask
	if isBGTask {
		if err := e.OpenTask(id); err != nil {
			return err
		}
		opened = bgTask
		e.agent.notify.mu.Lock()
		e.agent.notify.focusedTaskID = bgTask.ID
		e.agent.notify.mu.Unlock()
	} else {
		if err := e.openTurnTaskScene(turnTask.ScenePath, turnFilePath); err != nil {
			return err
		}
		e.SetTasksPanelOpen(false)
		e.agent.notify.mu.Lock()
		e.agent.notify.focusedTaskID = turnTask.ID
		e.agent.notify.mu.Unlock()
		if e.agent.mode != AgentModeNone {
			e.agent.rightTab = RightTabAgent
		}
		if turnTask.State == TaskNeedsYou && e.agent.openRequest != nil {
			if e.agent.openRequest.ID == turnTask.ID || e.waitingTurnTaskID() == turnTask.ID {
				e.FocusOpenAgentRequest()
			}
		}
		opened = turnTask
	}

	e.revealPanelIfHiddenOrInactive(PanelInspector)
	e.closeTaskSystemNotification(opened.ID)
	if e.OnOpenAgentTask != nil {
		e.OnOpenAgentTask(opened)
	}
	e.notify()
	return nil
}

func (e *Editor) openTurnTaskScene(scenePath, filePath string) error {
	scenePath = strings.TrimSpace(scenePath)
	filePath = strings.TrimSpace(filePath)
	if scenePath == "" && filePath == "" {
		return nil
	}
	// Already on the target scene?
	if scenePath != "" && e.ScenePath == scenePath {
		return nil
	}
	// When a project is open, strictly confine target scene opening inside the project root
	// (preventing both ".." traversal and symlink escapes).
	if e.ProjectPath != "" {
		root := filepath.Dir(e.ProjectPath)
		if scenePath != "" {
			if idx := e.findTabByPath(scenePath, ""); idx >= 0 {
				return e.SelectTab(idx)
			}
			_, abs, err := resolveProjectFileWithinRoot(root, scenePath, false)
			if err != nil {
				return err
			}
			return e.OpenFile(abs)
		}
		if filePath != "" {
			rel, err := filepath.Rel(root, filePath)
			if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
				return fmt.Errorf("editor: task scene path %q escapes project root", filePath)
			}
			_, abs, err := resolveProjectFileWithinRoot(root, filepath.ToSlash(rel), false)
			if err != nil {
				return err
			}
			return e.OpenFile(abs)
		}
		return nil
	}
	// Standalone scene fallback when no project is open.
	if idx := e.findTabByPath(scenePath, filePath); idx >= 0 {
		return e.SelectTab(idx)
	}
	if filePath != "" {
		if _, err := os.Stat(filePath); err != nil {
			return err
		}
		return e.OpenFile(filePath)
	}
	return nil
}

func (e *Editor) onAgentTaskStateUpdated(task AgentTask, prevState TaskState, openedAt time.Time) {
	if e == nil || strings.TrimSpace(task.ID) == "" {
		return
	}
	// Leaving a notifiable state (or dismissing) cancels timers/held notifications and closes any active system notification.
	if task.Dismissed || !task.State.IsNotifiable() {
		e.clearTaskNotificationForID(task.ID)
		return
	}

	if prevState != task.State {
		e.cancelTaskWaitingTimer(task.ID)
		e.clearHeldNotification(task.ID)
		e.agent.notify.mu.Lock()
		if e.agent.notify.notifiedState != nil && e.agent.notify.notifiedState[task.ID] != task.State {
			delete(e.agent.notify.notifiedState, task.ID)
		}
		e.agent.notify.mu.Unlock()
	}

	// Already notified for this (task, state)?
	e.agent.notify.mu.Lock()
	alreadyNotified := e.agent.notify.notifiedState != nil && e.agent.notify.notifiedState[task.ID] == task.State
	e.agent.notify.mu.Unlock()
	if alreadyNotified {
		return
	}

	// During Play mode: hold all notifiable events until Play stops; never pop anything over the game.
	if e.playing {
		e.holdNotificationForPlay(task)
		return
	}

	// Background window: all three events (waiting, ready to review, failed) notify immediately.
	if e.windowInBackground() {
		e.dispatchTaskNotification(task)
		return
	}

	// Foreground window (not playing):
	// - ReadyToReview and Failed stay in the editor panel / review (no system notification).
	// - Waiting (TaskNeedsYou) schedules the 30 s notification timer per AgentStates.dc.html.
	if task.State == TaskNeedsYou {
		taskID := task.ID
		e.agent.notify.mu.Lock()
		if _, exists := e.agent.notify.waitingTimers[taskID]; exists {
			e.agent.notify.mu.Unlock()
			return
		}
		e.agent.notify.nextTimerSeq++
		seq := e.agent.notify.nextTimerSeq
		gen := e.agent.notify.gen
		if e.agent.notify.waitingTimers == nil {
			e.agent.notify.waitingTimers = make(map[string]waitingTimerEntry)
		}
		// Reserve the slot with seq before scheduling so synchronous test schedulers cannot leave a leaked entry.
		e.agent.notify.waitingTimers[taskID] = waitingTimerEntry{seq: seq}
		e.agent.notify.mu.Unlock()

		delay := AgentWaitingNotifyDelay
		if !openedAt.IsZero() {
			elapsed := e.nowTime().Sub(openedAt)
			if elapsed >= AgentWaitingNotifyDelay {
				delay = 0
			} else if elapsed > 0 {
				delay = AgentWaitingNotifyDelay - elapsed
			}
		}

		cancel := e.scheduleAgentAfter(delay, func() {
			e.agent.notify.mu.Lock()
			entry, exists := e.agent.notify.waitingTimers[taskID]
			if !exists || entry.seq != seq || e.agent.notify.gen != gen {
				e.agent.notify.mu.Unlock()
				return
			}
			delete(e.agent.notify.waitingTimers, taskID)
			already := e.agent.notify.notifiedState != nil && e.agent.notify.notifiedState[taskID] == TaskNeedsYou
			e.agent.notify.mu.Unlock()
			if already {
				return
			}
			cur, ok := e.AgentTaskByID(taskID)
			if !ok || cur.Dismissed || cur.State != TaskNeedsYou {
				return
			}
			if e.playing {
				e.holdNotificationForPlay(cur)
				return
			}
			e.dispatchTaskNotification(cur)
		})

		e.agent.notify.mu.Lock()
		if entry, exists := e.agent.notify.waitingTimers[taskID]; exists && entry.seq == seq {
			entry.cancel = cancel
			e.agent.notify.waitingTimers[taskID] = entry
			cancel = nil
		}
		e.agent.notify.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

func (e *Editor) holdNotificationForPlay(task AgentTask) {
	built, ok := e.BuildAgentTaskNotification(task)
	if !ok {
		return
	}
	item := heldTaskNotification{
		taskID: task.ID,
		state:  task.State,
		notif:  built.CoreNotification(),
	}
	e.agent.notify.mu.Lock()
	defer e.agent.notify.mu.Unlock()
	for i := range e.agent.notify.heldNotifications {
		if e.agent.notify.heldNotifications[i].taskID == task.ID {
			e.agent.notify.heldNotifications[i] = item
			return
		}
	}
	if len(e.agent.notify.heldNotifications) >= maxHeldTaskNotifications {
		e.agent.notify.heldNotifications = slices.Delete(e.agent.notify.heldNotifications, 0, len(e.agent.notify.heldNotifications)-maxHeldTaskNotifications+1)
	}
	e.agent.notify.heldNotifications = append(e.agent.notify.heldNotifications, item)
}

func (e *Editor) flushHeldNotificationsAfterPlay() {
	if e == nil || e.playing {
		return
	}
	e.agent.notify.mu.Lock()
	if len(e.agent.notify.heldNotifications) == 0 {
		e.agent.notify.mu.Unlock()
		return
	}
	held := e.agent.notify.heldNotifications
	e.agent.notify.heldNotifications = nil
	e.agent.notify.mu.Unlock()
	for _, h := range held {
		cur, ok := e.AgentTaskByID(h.taskID)
		if !ok || cur.Dismissed || cur.State != h.state || !cur.State.IsNotifiable() {
			continue
		}
		e.dispatchTaskNotification(cur)
	}
}

func (e *Editor) dispatchTaskNotification(task AgentTask) {
	built, ok := e.BuildAgentTaskNotification(task)
	if !ok {
		return
	}
	cx := e.agent.getScope()
	if cx == nil || cx.Disposed() {
		return
	}
	item := heldTaskNotification{
		taskID: task.ID,
		state:  task.State,
		notif:  built.CoreNotification(),
	}

	e.agent.notify.mu.Lock()
	if e.agent.notify.notifiedState == nil {
		e.agent.notify.notifiedState = make(map[string]TaskState)
	}
	e.agent.notify.notifiedState[task.ID] = task.State

	if !e.agent.notify.notifPermRequested {
		e.agent.notify.notifPermRequested = true
		e.agent.notify.notifPermPending = true
		permGen := e.agent.notify.gen
		e.enqueuePendingPermNotificationLocked(item)
		e.agent.notify.mu.Unlock()

		cx.RequestNotificationPermission(func(p core.NotificationPermission) {
			e.agent.notify.mu.Lock()
			e.agent.notify.notifPerm = p
			e.agent.notify.notifPermPending = false
			pending := e.agent.notify.pendingPermNotifications
			e.agent.notify.pendingPermNotifications = nil
			stale := e.agent.notify.gen != permGen
			e.agent.notify.mu.Unlock()
			if p != core.NotificationsGranted || stale {
				return
			}
			curScope := e.agent.getScope()
			if curScope == nil || curScope != cx || curScope.Disposed() {
				return
			}
			for _, pend := range pending {
				cur, exists := e.AgentTaskByID(pend.taskID)
				if !exists || cur.Dismissed || cur.State != pend.state || !cur.State.IsNotifiable() {
					continue
				}
				if e.playing {
					e.holdNotificationForPlay(cur)
					continue
				}
				e.showCoreNotification(curScope, pend.notif)
			}
		})
		return
	}

	if e.agent.notify.notifPermPending {
		e.enqueuePendingPermNotificationLocked(item)
		e.agent.notify.mu.Unlock()
		return
	}

	perm := e.agent.notify.notifPerm
	e.agent.notify.mu.Unlock()
	if perm != core.NotificationsGranted {
		return
	}
	e.showCoreNotification(cx, item.notif)
}

func (e *Editor) enqueuePendingPermNotificationLocked(item heldTaskNotification) {
	for i := range e.agent.notify.pendingPermNotifications {
		if e.agent.notify.pendingPermNotifications[i].taskID == item.taskID {
			e.agent.notify.pendingPermNotifications[i] = item
			return
		}
	}
	if len(e.agent.notify.pendingPermNotifications) >= maxHeldTaskNotifications {
		e.agent.notify.pendingPermNotifications = slices.Delete(e.agent.notify.pendingPermNotifications, 0, len(e.agent.notify.pendingPermNotifications)-maxHeldTaskNotifications+1)
	}
	e.agent.notify.pendingPermNotifications = append(e.agent.notify.pendingPermNotifications, item)
}

func (e *Editor) showCoreNotification(cx *core.Scope, n core.Notification) {
	if cx == nil || cx.Disposed() {
		return
	}
	if err := cx.ShowNotification(n, func(shown bool) {
		if !shown {
			e.agent.notify.mu.Lock()
			if e.agent.notify.shownIDs != nil {
				delete(e.agent.notify.shownIDs, n.ID)
			}
			e.agent.notify.mu.Unlock()
		}
	}); err == nil {
		e.agent.notify.mu.Lock()
		if e.agent.notify.shownIDs == nil {
			e.agent.notify.shownIDs = make(map[string]bool)
		}
		e.agent.notify.shownIDs[n.ID] = true
		e.agent.notify.mu.Unlock()
	}
}
