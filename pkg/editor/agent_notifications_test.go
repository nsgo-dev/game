package editor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func setupNotificationProject(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	scenesDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(scenesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScene := func(rel, name string) {
		doc := &sceneformat.SceneDoc{
			Version:         sceneformat.CurrentVersion,
			Name:            name,
			VirtualWidth:    640,
			VirtualHeight:   360,
			BackgroundColor: "#15181D",
			Layers: []sceneformat.LayerDoc{
				{
					Name:  "actors",
					Space: sceneformat.SpaceWorld,
					Order: 0,
					Objects: []sceneformat.ObjectDoc{
						{ID: "player", Name: "Player", X: 120, Y: 220},
					},
				},
			},
		}
		raw, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeScene("scenes/level_01.scene.yaml", "level_01")
	writeScene("scenes/boss_room.scene.yaml", "boss_room")

	proj := &project.File{
		Version:    project.CurrentVersion,
		Name:       "dungeon-run",
		ID:         "dev.nsgo.dungeonrun",
		AppVersion: "0.1.0",
		Window: project.Window{
			Width:  640,
			Height: 360,
		},
		Autoload: &project.Autoload{
			Scenes: []project.Entry{
				{Name: "level_01", Path: "scenes/level_01.scene.yaml"},
				{Name: "boss_room", Path: "scenes/boss_room.scene.yaml"},
			},
			MainScene: "level_01",
		},
	}
	projRaw, err := project.Encode(proj)
	if err != nil {
		t.Fatal(err)
	}
	projPath := filepath.Join(dir, project.FileName)
	if err := os.WriteFile(projPath, projRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.AppName = "NSGo Editor"
	if err := ed.OpenProject(projPath); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	return ed, dir
}

// TestAgentScriptedBackgroundNotificationAndDockBadgeCount verifies issue #1023 acceptance #1:
// a scripted agent in the background notifies for a permission request and NOT for a finished step
// or agent reply, requests permission only when first needed, and counts 1 waiting + 2 ready to
// review + 1 failed as Dock badge 4 (ignoring running and queued tasks across both Agent runs and
// background #1022 tasks).
func TestAgentScriptedBackgroundNotificationAndDockBadgeCount(t *testing.T) {
	ed, _ := setupNotificationProject(t)
	sched := &fakeRetryScheduler{}
	ed.AgentScheduleAfter = sched.schedule
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})

	stepDone := make(chan struct{})
	askPerm := make(chan struct{})
	permHandled := make(chan struct{})
	finishRun := make(chan struct{})

	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		// Finished step -> must NOT notify.
		emit(editor.AgentEvent{
			Kind:       editor.AgentEventStep,
			StepID:     "s1",
			StepTitle:  "Measured the gap: 64 px",
			StepStatus: editor.AgentStepDone,
			StepIndex:  1,
			StepTotal:  3,
		})
		// Agent reply -> must NOT notify.
		emit(editor.AgentEvent{
			Kind: editor.AgentEventMessage,
			Text: "Checked floor geometry; preparing playtest.",
		})
		close(stepDone)

		<-askPerm
		// Permission request -> MUST notify in background!
		emit(editor.AgentEvent{
			Kind:       editor.AgentEventStep,
			StepID:     "perm-playtest",
			StepTitle:  "Allow the agent to run the game?",
			StepDetail: "Torches around the boss arena wants to play boss_room headless for 10 s.",
			StepStatus: editor.AgentStepWaiting,
			StepIndex:  2,
			StepTotal:  3,
		})
		<-permHandled
		<-finishRun
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      true,
		Locale:    "en-US",
		Lifecycle: core.LifecycleBackground,
	}))

	// Permission must NOT be requested before a notification is actually needed.
	if app.NotificationPermission != core.NotificationsNotAsked {
		t.Fatalf("initial NotificationPermission = %v, want NotificationsNotAsked", app.NotificationPermission)
	}

	// Start scripted agent and also add a queued background task -> neither running nor queued notifies or adds to badge.
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "t-queued",
		Title:        "Coin pickup sound a little higher",
		InitialState: editor.TaskQueued,
	}); err != nil {
		t.Fatalf("SubmitTask queued: %v", err)
	}
	if err := ed.SubmitAgentPrompt("Torches around the boss arena"); err != nil {
		t.Fatalf("SubmitAgentPrompt: %v", err)
	}
	<-stepDone
	app.Settle()

	if len(app.Notifications) != 0 {
		t.Fatalf("finished step + agent reply + queued task produced notifications: %+v", app.Notifications)
	}
	if app.NotificationPermission != core.NotificationsNotAsked {
		t.Fatalf("permission asked prematurely before any notifiable event: %v", app.NotificationPermission)
	}
	if app.Badge != 0 {
		t.Fatalf("Dock badge for running + queued = %d, want 0", app.Badge)
	}

	// Trigger permission request while in background -> asks permission once and shows notification.
	close(askPerm)
	app.WaitUntil(3*time.Second, func() bool {
		return len(app.Notifications) == 1
	})
	if app.NotificationPermission != core.NotificationsGranted {
		t.Fatalf("NotificationPermission = %v, want NotificationsGranted", app.NotificationPermission)
	}
	n := app.Notifications[0]
	if !strings.Contains(n.Title, "NSGo Editor · dungeon-run") || !strings.Contains(n.Title, "Allow the agent to run the game?") {
		t.Fatalf("notification Title = %q, want header + permission title", n.Title)
	}
	if !strings.Contains(n.Body, "Torches around the boss arena") || !strings.Contains(n.Body, "Allow once") {
		t.Fatalf("notification Body = %q, want task title and primary action", n.Body)
	}
	if app.Badge != 1 {
		t.Fatalf("Dock badge with 1 waiting = %d, want 1", app.Badge)
	}

	// Add 2 ready-to-review, 1 failed, 1 extra running, 1 extra queued via #1022 SubmitTask -> Dock badge = 1 + 2 + 1 = 4.
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "t-review-1",
		Title:        "Nightly: fix game check warnings",
		Source:       editor.TaskSourceAutomation,
		InitialState: editor.TaskReadyToReview,
		ScenePath:    "scenes/level_01.scene.yaml",
		Review:       &editor.TaskReview{Summary: "3 changes in level_02.scene.yaml."},
	}); err != nil {
		t.Fatalf("SubmitTask review-1: %v", err)
	}
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "t-review-2",
		Title:        "Translate HUD strings to Japanese",
		Source:       editor.TaskSourceAutomation,
		InitialState: editor.TaskReadyToReview,
		ScenePath:    "scenes/level_01.scene.yaml",
		Review:       &editor.TaskReview{Summary: "12 changes in locale/ja.yaml."},
	}); err != nil {
		t.Fatalf("SubmitTask review-2: %v", err)
	}
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "t-failed-1",
		Title:        "Bake normals for the cave tiles",
		Error:        "the asset tool exited with code 1 at step 2 of 3. Nothing was changed.",
		InitialState: editor.TaskFailed,
		ScenePath:    "scenes/boss_room.scene.yaml",
	}); err != nil {
		t.Fatalf("SubmitTask failed-1: %v", err)
	}
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "t-running-extra",
		Title:        "All slimes 20% faster",
		StartMode:    editor.TaskStartParallel,
		InitialState: editor.TaskRunning,
	}); err != nil {
		t.Fatalf("SubmitTask running-extra: %v", err)
	}
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "t-queued-extra",
		Title:        "Add a lever beside the door",
		InitialState: editor.TaskQueued,
	}); err != nil {
		t.Fatalf("SubmitTask queued-extra: %v", err)
	}
	app.Settle()

	if got := ed.AgentDockBadgeCount(); got != 4 {
		t.Fatalf("AgentDockBadgeCount = %d, want 4 (1 waiting + 2 review + 1 failed)", got)
	}
	if app.Badge != 4 {
		t.Fatalf("app.Badge = %d, want 4", app.Badge)
	}
	// All four notifiable tasks (1 waiting + 2 ready to review + 1 failed) notified in background.
	if len(app.Notifications) != 4 {
		t.Fatalf("len(app.Notifications) = %d, want 4: %+v", len(app.Notifications), app.Notifications)
	}

	// Resolve permission, finish run, dismiss/keep background tasks, and clear turn tasks -> badge returns to 0.
	ed.ClearAgentWaitingForYou()
	close(permHandled)
	close(finishRun)
	app.WaitUntil(3*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusConnected
	})
	ed.DismissTasks([]string{"t-review-1", "t-review-2", "t-failed-1"})
	ed.ClearAgentTasks()
	app.Settle()
	if app.Badge != 0 {
		t.Fatalf("after resolving all tasks app.Badge = %d, want 0", app.Badge)
	}
}

// TestBackgroundTaskTransitionsNotifyAndClearBadge verifies #1022 background task methods
// (SubmitTask, RequestTaskPermission, ResolveTaskPermission, CompleteTaskTurn, FailTask,
// KeepTask, RetryTask, DismissTasks) drive system notifications and Dock badges end-to-end.
func TestBackgroundTaskTransitionsNotifyAndClearBadge(t *testing.T) {
	ed, _ := setupNotificationProject(t)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      true,
		Locale:    "en-US",
		Lifecycle: core.LifecycleBackground,
	}))

	// Running task never notifies and never increments Dock badge.
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:        "bg-1",
		Title:     "Torches around the boss arena",
		StartMode: editor.TaskStartParallel,
		ScenePath: "scenes/boss_room.scene.yaml",
	}); err != nil {
		t.Fatalf("SubmitTask bg-1: %v", err)
	}
	if err := ed.UpdateTaskStep("bg-1", 1, 3, "Measured the room"); err != nil {
		t.Fatalf("UpdateTaskStep: %v", err)
	}
	if err := ed.AddTaskStep("bg-1", "Placed 4 wall brackets", true); err != nil {
		t.Fatalf("AddTaskStep: %v", err)
	}
	app.Settle()
	if len(app.Notifications) != 0 || app.Badge != 0 {
		t.Fatalf("running steps should not notify or badge: notifs=%d badge=%d", len(app.Notifications), app.Badge)
	}

	// RequestTaskPermission -> notifies in background and increments badge to 1.
	if err := ed.RequestTaskPermission("bg-1", "Allow the agent to run the game?", "wants to play boss_room headless for 10 s."); err != nil {
		t.Fatalf("RequestTaskPermission: %v", err)
	}
	app.Settle()
	if len(app.Notifications) != 1 || app.Badge != 1 {
		t.Fatalf("after RequestTaskPermission: notifs=%d badge=%d, want 1 and 1", len(app.Notifications), app.Badge)
	}

	// ResolveTaskPermission -> closes system notification and drops badge back to 0.
	if err := ed.ResolveTaskPermission("bg-1", true); err != nil {
		t.Fatalf("ResolveTaskPermission: %v", err)
	}
	app.Settle()
	if len(app.Notifications) != 0 || app.Badge != 0 {
		t.Fatalf("after ResolveTaskPermission: notifs=%d badge=%d, want 0 and 0", len(app.Notifications), app.Badge)
	}

	// CompleteTaskTurn -> notifies Ready to review and increments badge to 1.
	if err := ed.CompleteTaskTurn("bg-1", editor.TaskReview{Summary: "Added 6 torches."}); err != nil {
		t.Fatalf("CompleteTaskTurn: %v", err)
	}
	app.Settle()
	if len(app.Notifications) != 1 || app.Badge != 1 {
		t.Fatalf("after CompleteTaskTurn: notifs=%d badge=%d, want 1 and 1", len(app.Notifications), app.Badge)
	}
	if !strings.Contains(app.Notifications[0].Title, "Ready to review") || !strings.Contains(app.Notifications[0].Body, "Added 6 torches.") {
		t.Fatalf("review notification = %+v", app.Notifications[0])
	}

	// KeepTask -> closes notification and clears badge.
	if err := ed.KeepTask("bg-1"); err != nil {
		t.Fatalf("KeepTask: %v", err)
	}
	app.Settle()
	if len(app.Notifications) != 0 || app.Badge != 0 {
		t.Fatalf("after KeepTask: notifs=%d badge=%d, want 0 and 0", len(app.Notifications), app.Badge)
	}

	// FailTask -> notifies Task failed and increments badge; RetryTask clears both.
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:        "bg-2",
		Title:     "Bake normals",
		StartMode: editor.TaskStartParallel,
		ScenePath: "scenes/boss_room.scene.yaml",
	}); err != nil {
		t.Fatalf("SubmitTask bg-2: %v", err)
	}
	if err := ed.FailTask("bg-2", "asset tool exited with code 1"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	app.Settle()
	if len(app.Notifications) != 1 || app.Badge != 1 {
		t.Fatalf("after FailTask: notifs=%d badge=%d, want 1 and 1", len(app.Notifications), app.Badge)
	}
	if err := ed.RetryTask("bg-2"); err != nil {
		t.Fatalf("RetryTask: %v", err)
	}
	app.Settle()
	if len(app.Notifications) != 0 || app.Badge != 0 {
		t.Fatalf("after RetryTask: notifs=%d badge=%d, want 0 and 0", len(app.Notifications), app.Badge)
	}
}

// TestAgentWaiting30SecondRuleAndForegroundSilence verifies AgentStates.dc.html's 30 s rule:
// in the foreground, Waiting does not notify immediately, cancels cleanly if answered before 30 s,
// and notifies after 30 s if still waiting; ready-to-review and failed stay in the panel in foreground.
func TestAgentWaiting30SecondRuleAndForegroundSilence(t *testing.T) {
	ed, _ := setupNotificationProject(t)
	now := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	sched := &fakeRetryScheduler{}
	ed.AgentScheduleAfter = sched.schedule

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      true,
		Locale:    "en-US",
		Lifecycle: core.LifecycleActive,
	}))

	// Ready to review and failed in foreground stay in the panel (no system notification).
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "fg-review",
		Title:        "Nightly check",
		InitialState: editor.TaskReadyToReview,
	}); err != nil {
		t.Fatalf("SubmitTask fg-review: %v", err)
	}
	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "fg-failed",
		Title:        "Bake normals",
		InitialState: editor.TaskFailed,
	}); err != nil {
		t.Fatalf("SubmitTask fg-failed: %v", err)
	}
	app.Settle()
	if len(app.Notifications) != 0 {
		t.Fatalf("foreground review/failed should not send system notifications: %+v", app.Notifications)
	}
	if app.NotificationPermission != core.NotificationsNotAsked {
		t.Fatalf("foreground badge update should not request notification permission: %v", app.NotificationPermission)
	}
	if app.Badge != 2 {
		t.Fatalf("foreground badge = %d, want 2", app.Badge)
	}

	// Waiting in foreground schedules a 30 s timer; clearing before 30 s sends nothing.
	ed.SetAgentWaitingForYou(editor.AgentOpenRequest{
		ID:       "perm-short",
		Title:    "Allow the agent to run the game?",
		Body:     "Play level_01 headless for 10 s.",
		OpenedAt: now,
	})
	app.Settle()
	if len(app.Notifications) != 0 {
		t.Fatalf("waiting in foreground notified immediately: %+v", app.Notifications)
	}
	if delays := sched.delays(); len(delays) != 1 || delays[0] != 30*time.Second {
		t.Fatalf("scheduled waiting delays = %v, want [30s]", delays)
	}
	ed.ClearAgentWaitingForYou()
	sched.fireLast()
	app.Settle()
	if len(app.Notifications) != 0 {
		t.Fatalf("cleared waiting request still notified: %+v", app.Notifications)
	}

	// Waiting left unanswered for 30 s notifies when the 30 s timer fires.
	ed.SetAgentWaitingForYou(editor.AgentOpenRequest{
		ID:       "perm-30s",
		Title:    "Allow the agent to run the game?",
		Body:     "Play boss_room headless for 10 s.",
		OpenedAt: now,
	})
	app.Settle()
	if len(app.Notifications) != 0 {
		t.Fatalf("waiting notified before 30 s elapsed: %+v", app.Notifications)
	}
	sched.fireLast()
	app.Settle()
	if len(app.Notifications) != 1 {
		t.Fatalf("after 30 s len(Notifications) = %d, want 1", len(app.Notifications))
	}
	if app.Notifications[0].ID != "agent-task:perm-30s" {
		t.Fatalf("notification ID = %q, want agent-task:perm-30s", app.Notifications[0].ID)
	}
}

// TestNotificationPermissionDeniedDoesNotShowOrReprompt verifies denied permission suppresses
// notifications without re-prompting while keeping Dock badge counts intact.
func TestNotificationPermissionDeniedDoesNotShowOrReprompt(t *testing.T) {
	ed, _ := setupNotificationProject(t)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      true,
		Locale:    "en-US",
		Lifecycle: core.LifecycleBackground,
	}))
	app.NotificationAnswer = core.NotificationsDenied

	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "t-denied",
		Title:        "Bake normals",
		InitialState: editor.TaskFailed,
	}); err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	app.Settle()
	if app.NotificationPermission != core.NotificationsDenied {
		t.Fatalf("NotificationPermission = %v, want NotificationsDenied", app.NotificationPermission)
	}
	if len(app.Notifications) != 0 {
		t.Fatalf("denied permission still showed notifications: %+v", app.Notifications)
	}
	if app.Badge != 1 {
		t.Fatalf("Dock badge = %d, want 1", app.Badge)
	}
}

// TestClickingNotificationFocusesWindowAndOpensTaskInItsScene verifies clicking a system
// notification focuses the editor window, dismisses obstructing panels (History, Settings,
// Problem panel, Assets mode), opens the task's scene even when another scene is active,
// opens the Tasks panel, and focuses the task.
func TestClickingNotificationFocusesWindowAndOpensTaskInItsScene(t *testing.T) {
	ed, _ := setupNotificationProject(t)
	if ed.ScenePath != "scenes/level_01.scene.yaml" {
		t.Fatalf("initial ScenePath = %q, want scenes/level_01.scene.yaml", ed.ScenePath)
	}

	windowFocused := 0
	var openedTask editor.AgentTask
	ed.OnFocusWindow = func() { windowFocused++ }
	ed.OnOpenAgentTask = func(task editor.AgentTask) { openedTask = task }

	// Open obstructing panels/modes beforehand.
	ed.SetHistoryPanelOpen(true)
	ed.SetLeftRailMode(editor.LeftRailAssets)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      true,
		Locale:    "en-US",
		Lifecycle: core.LifecycleBackground,
	}))

	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:               "task-boss",
		Title:            "Torches around the boss arena",
		PermissionPrompt: "Allow the agent to run the game?",
		PermissionDetail: "wants to play boss_room headless for 10 s.",
		InitialState:     editor.TaskNeedsYou,
		ScenePath:        "scenes/boss_room.scene.yaml",
	}); err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	app.Settle()
	if len(app.Notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(app.Notifications))
	}

	app.PressNotification(app.Notifications[0].ID)
	app.Settle()

	if windowFocused != 1 {
		t.Fatalf("OnFocusWindow calls = %d, want 1", windowFocused)
	}
	if ed.ScenePath != "scenes/boss_room.scene.yaml" {
		t.Fatalf("ScenePath after notification click = %q, want scenes/boss_room.scene.yaml", ed.ScenePath)
	}
	if ed.HistoryPanelOpen() {
		t.Fatal("History panel stayed open after clicking task notification")
	}
	if ed.LeftRailMode() != editor.LeftRailScene {
		t.Fatalf("LeftRailMode = %v, want LeftRailScene", ed.LeftRailMode())
	}
	if !ed.TasksPanelOpen() {
		t.Fatal("Tasks panel did not open on notification click")
	}
	if ed.FocusedAgentTaskID() != "task-boss" {
		t.Fatalf("FocusedAgentTaskID = %q, want task-boss", ed.FocusedAgentTaskID())
	}
	if openedTask.ID != "task-boss" {
		t.Fatalf("OnOpenAgentTask = %+v, want task-boss", openedTask)
	}
}

// TestPlayModeHoldsNotificationsAndShowsMostUrgentDotOnHover verifies issue #1023 acceptance #2:
// during Play mode, no agent notification pops over the running game, one corner dot takes
// the color of the most urgent state (TaskFailed > TaskNeedsYou > TaskReadyToReview), the hover
// label shows only on hover, the dot node stays stably mounted across urgency escalations without
// losing hover/focus, and held notifications flush once Play stops.
func TestPlayModeHoldsNotificationsAndShowsMostUrgentDotOnHover(t *testing.T) {
	ed, _ := setupNotificationProject(t)
	ed.SetAppearance(editor.AppearanceDark)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      true,
		Locale:    "en-US",
		Lifecycle: core.LifecycleActive,
	}))

	// Start Play mode.
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	app.Settle()

	// No tasks yet -> no corner dot.
	if app.Exists("id:play-task-dot") {
		t.Fatal("play-task-dot should not appear with 0 urgent tasks")
	}

	// Queued / running tasks alone -> still no corner dot and no notifications.
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "run-1", Title: "Patrol", InitialState: editor.TaskRunning})
	_, _ = ed.SubmitTask(editor.TaskRequest{ID: "q-1", Title: "Coins", InitialState: editor.TaskQueued})
	app.Settle()
	if app.Exists("id:play-task-dot") {
		t.Fatal("play-task-dot should not appear for running/queued tasks only")
	}

	dark := editor.DarkSpecPalette()

	// 1. Ready to review -> dot appears in Spec Accent color (#3B7BFF), label hidden until hover, NO notification yet.
	_, _ = ed.SubmitTask(editor.TaskRequest{
		ID:           "rev-1",
		Title:        "Nightly: fix warnings",
		Source:       editor.TaskSourceAutomation,
		InitialState: editor.TaskReadyToReview,
		ScenePath:    "scenes/level_01.scene.yaml",
		Review:       &editor.TaskReview{Summary: "3 changes in level_01.scene.yaml."},
	})
	app.Settle()
	if len(app.Notifications) != 0 {
		t.Fatalf("notification showed during Play: %+v", app.Notifications)
	}
	if len(ed.HeldAgentNotifications()) != 1 {
		t.Fatalf("HeldAgentNotifications = %d, want 1", len(ed.HeldAgentNotifications()))
	}
	dotNode := app.Find("id:play-task-dot")
	dotCore := app.Find("id:play-task-dot-core")
	if dotNode == nil || dotCore == nil {
		t.Fatal("play-task-dot not visible during Play with ready-to-review task")
	}
	if dotCore.Style.Background != dark.Accent.Fg {
		t.Fatalf("review dot color = %#x, want Spec Accent.Fg %#x", uint32(dotCore.Style.Background), uint32(dark.Accent.Fg))
	}
	if app.Exists("id:play-task-dot-label") {
		t.Fatal("play-task-dot-label shown before hover")
	}

	// Hover the dot and keep hovering across urgency escalations -> node must NOT remount!
	app.Hover("id:play-task-dot")
	app.Settle()
	if !app.Exists("id:play-task-dot-label") {
		t.Fatal("play-task-dot-label did not appear on hover")
	}

	// 2. Add Waiting (needs you) -> dot upgrades to Spec Warning color (#F2C14E) WITHOUT remounting or losing hover.
	_, _ = ed.SubmitTask(editor.TaskRequest{
		ID:               "wait-1",
		Title:            "Torches around the boss arena",
		PermissionPrompt: "Allow the agent to run the game?",
		PermissionDetail: "wants to play boss_room headless for 10 s.",
		InitialState:     editor.TaskNeedsYou,
		ScenePath:        "scenes/boss_room.scene.yaml",
	})
	app.Settle()
	if len(app.Notifications) != 0 {
		t.Fatalf("waiting notification showed during Play: %+v", app.Notifications)
	}
	if gotNode := app.Find("id:play-task-dot"); gotNode.ID != dotNode.ID {
		t.Fatalf("play-task-dot remounted on waiting escalation: %v -> %v", dotNode.ID, gotNode.ID)
	}
	dotCore = app.Find("id:play-task-dot-core")
	if dotCore.Style.Background != dark.Warning.Fg {
		t.Fatalf("waiting dot color = %#x, want Spec Warning.Fg %#x", uint32(dotCore.Style.Background), uint32(dark.Warning.Fg))
	}
	if got := agentViewText(t, app, "id:play-task-dot-label-text"); got != "1 task needs you · stop Play to open" {
		t.Fatalf("hover label = %q, want %q", got, "1 task needs you · stop Play to open")
	}

	// 3. Add Failed -> dot upgrades to Spec Removed/Problem color (#FF6B6B) WITHOUT remounting, still 0 notifications while playing.
	_, _ = ed.SubmitTask(editor.TaskRequest{
		ID:           "fail-1",
		Title:        "Bake normals",
		Error:        "asset tool exited with code 1.",
		InitialState: editor.TaskFailed,
	})
	app.Settle()
	if len(app.Notifications) != 0 {
		t.Fatalf("failed notification showed during Play: %+v", app.Notifications)
	}
	if gotNode := app.Find("id:play-task-dot"); gotNode.ID != dotNode.ID {
		t.Fatalf("play-task-dot remounted on failed escalation: %v -> %v", dotNode.ID, gotNode.ID)
	}
	dotCore = app.Find("id:play-task-dot-core")
	if dotCore.Style.Background != dark.Removed.Fg {
		t.Fatalf("failed dot color = %#x, want Spec Removed.Fg %#x", uint32(dotCore.Style.Background), uint32(dark.Removed.Fg))
	}
	if got := agentViewText(t, app, "id:play-task-dot-label-text"); got != "1 task failed · stop Play to open" {
		t.Fatalf("failed hover label = %q, want %q", got, "1 task failed · stop Play to open")
	}
	app.HoverOut()
	app.Settle()
	if app.Exists("id:play-task-dot-label") {
		t.Fatal("play-task-dot-label did not hide after HoverOut")
	}
	if app.Badge != 3 {
		t.Fatalf("Dock badge during Play = %d, want 3", app.Badge)
	}

	// 4. Stop Play -> dot vanishes and all 3 held notifications flush immediately.
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	app.Settle()
	if app.Exists("id:play-task-dot") {
		t.Fatal("play-task-dot stayed visible after Stop")
	}
	if len(ed.HeldAgentNotifications()) != 0 {
		t.Fatalf("HeldAgentNotifications after Stop = %d, want 0", len(ed.HeldAgentNotifications()))
	}
	if len(app.Notifications) != 3 {
		t.Fatalf("after Stop len(app.Notifications) = %d, want 3: %+v", len(app.Notifications), app.Notifications)
	}
}

// TestScriptedAgentFailureNotifiesInBackground verifies a scripted runner returning an error
// notifies in the background and increments the Dock badge.
func TestScriptedAgentFailureNotifiesInBackground(t *testing.T) {
	ed, _ := setupNotificationProject(t)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{ID: "c1", Name: "claude-code", Transport: "stdio"})
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, _ editor.AgentRequest, _ func(editor.AgentEvent)) error {
		return errors.New("asset tool exited with code 1")
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      true,
		Locale:    "en-US",
		Lifecycle: core.LifecycleBackground,
	}))

	if err := ed.SubmitAgentPrompt("Bake normals for the cave tiles"); err != nil {
		t.Fatalf("SubmitAgentPrompt: %v", err)
	}
	app.WaitUntil(3*time.Second, func() bool {
		return len(app.Notifications) == 1
	})
	if app.Badge != 1 {
		t.Fatalf("badge after runner failure = %d, want 1", app.Badge)
	}
	n := app.Notifications[0]
	if !strings.Contains(n.Title, "Task failed") || !strings.Contains(n.Body, "asset tool exited with code 1") {
		t.Fatalf("failed notification = %+v", n)
	}
}

// TestNotificationClickClosesProblemPanelAndLightThemeDotTokens verifies clicking a notification
// dismisses the Problem agent panel so the Agent/Tasks panel is visible, and Light Spec appearance
// uses LightSpecPalette tokens for the Play dot.
func TestNotificationClickClosesProblemPanelAndLightThemeDotTokens(t *testing.T) {
	ed, _ := setupNotificationProject(t)
	ed.SetAppearance(editor.AppearanceLight)
	ed.SetProblemAgentPanelOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      false,
		Locale:    "en-US",
		Lifecycle: core.LifecycleBackground,
	}))

	ed.SetAgentWaitingForYou(editor.AgentOpenRequest{
		ID:    "task-light",
		Title: "Allow the agent to run the game?",
		Body:  "Torches around the boss arena",
	})
	app.Settle()
	if len(app.Notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(app.Notifications))
	}

	// Clicking notification closes ProblemAgentPanelOpen and opens the Agent panel.
	app.PressNotification(app.Notifications[0].ID)
	app.Settle()
	if ed.ProblemAgentPanelOpen() {
		t.Fatal("ProblemAgentPanelOpen remained true after clicking task notification")
	}
	if !ed.AgentPanelOpen() {
		t.Fatal("Agent panel not open after clicking task notification")
	}

	// Verify Play-mode dot in Light Spec theme uses LightSpecPalette().Warning.Fg.
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	app.Settle()
	light := editor.LightSpecPalette()
	dotCore := app.Find("id:play-task-dot-core")
	if dotCore == nil {
		t.Fatal("play-task-dot-core not visible in Light theme Play mode")
	}
	if dotCore.Style.Background != light.Warning.Fg {
		t.Fatalf("light waiting dot color = %#x, want LightSpecPalette().Warning.Fg %#x", uint32(dotCore.Style.Background), uint32(light.Warning.Fg))
	}
	_ = ed.Stop()
	app.Settle()
}

// TestNotificationSanitizesControlCharsAndBoundsLength verifies untrusted agent strings
// have control characters collapsed and excessively long payloads cleanly bounded.
func TestNotificationSanitizesControlCharsAndBoundsLength(t *testing.T) {
	ed, _ := setupNotificationProject(t)
	longErr := "line1\n\r\tline2\x00" + strings.Repeat("x", 600)
	built, ok := ed.BuildAgentTaskNotification(editor.AgentTask{
		ID:    "sanitize-1",
		Title: "Bake\nnormals\r\tfor\x07tiles",
		Error: longErr,
		State: editor.TaskFailed,
	})
	if !ok {
		t.Fatal("BuildAgentTaskNotification returned false")
	}
	cn := built.CoreNotification()
	if strings.ContainsAny(cn.Title, "\r\n\t\x00\x07") {
		t.Fatalf("notification Title kept control chars: %q", cn.Title)
	}
	if strings.ContainsAny(cn.Body, "\r\n\t\x00\x07") {
		t.Fatalf("notification Body kept control chars: %q", cn.Body)
	}
	if len([]rune(cn.Body)) > 300 {
		t.Fatalf("notification Body too long: %d runes", len([]rune(cn.Body)))
	}
	if !strings.Contains(built.Body, "…") {
		t.Fatalf("expected truncated body to end with ellipsis, got %q", built.Body)
	}
}

// TestOpenAgentTaskRefusesSymlinkEscapeAndTraversal verifies clicking a notification for a task
// whose ScenePath points outside the project root (via symlink or traversal) is refused.
func TestOpenAgentTaskRefusesSymlinkEscapeAndTraversal(t *testing.T) {
	ed, dir := setupNotificationProject(t)
	outsideDir := t.TempDir()
	outsideScene := filepath.Join(outsideDir, "secret.scene.yaml")
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "secret",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#000000",
	}
	raw, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideScene, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "scenes", "escape.scene.yaml")
	if err := os.Symlink(outsideScene, linkPath); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if _, err := ed.SubmitTask(editor.TaskRequest{
		ID:           "sym-task",
		Title:        "Symlink escape",
		InitialState: editor.TaskFailed,
		ScenePath:    "scenes/escape.scene.yaml",
	}); err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	if err := ed.OpenAgentTask("sym-task"); err == nil {
		t.Fatal("OpenAgentTask succeeded for symlink escaping project root, want error")
	}
	if ed.ScenePath != "scenes/level_01.scene.yaml" {
		t.Fatalf("ScenePath changed after refused symlink escape: %q", ed.ScenePath)
	}
}

// TestLoadAgentThreadUpdatesBadgeWithoutSpammingNotifications verifies restoring a thread YAML
// on disk reflects pending attention tasks in the Dock badge without emitting OS notifications
// for historical tasks.
func TestLoadAgentThreadUpdatesBadgeWithoutSpammingNotifications(t *testing.T) {
	ed, dir := setupNotificationProject(t)
	threadsDir := filepath.Join(dir, ".nsgo", "threads")
	if err := os.MkdirAll(threadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	threadPath := filepath.Join(threadsDir, "default.thread.yaml")
	doc := &editor.AgentThreadDoc{
		Version: editor.AgentThreadVersion,
		ID:      editor.DefaultAgentThreadID,
		Tasks: []editor.AgentTask{
			{
				ID:        "restored-1",
				Title:     "Nightly check",
				Source:    editor.TaskSourceAutomation,
				StartMode: editor.TaskStartQueue,
				State:     editor.TaskReadyToReview,
			},
			{
				ID:        "restored-2",
				Title:     "Bake normals",
				Source:    editor.TaskSourceUser,
				StartMode: editor.TaskStartQueue,
				State:     editor.TaskFailed,
				Error:     "exit status 1",
			},
		},
	}
	raw, err := editor.EncodeAgentThread(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(threadPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      true,
		Locale:    "en-US",
		Lifecycle: core.LifecycleBackground,
	}))

	if err := ed.LoadAgentThread(threadPath); err != nil {
		t.Fatalf("LoadAgentThread: %v", err)
	}
	app.Settle()

	if len(app.Notifications) != 0 {
		t.Fatalf("restoring thread from disk spammed notifications: %+v", app.Notifications)
	}
	if app.Badge != 2 {
		t.Fatalf("Dock badge after LoadAgentThread = %d, want 2", app.Badge)
	}
}
