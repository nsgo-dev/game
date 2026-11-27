package editorcmd_test

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func postOnUI(ctx context.Context, ed *editor.Editor, fn func()) {
	done := make(chan struct{})
	ed.Post(func() {
		fn()
		close(done)
	})
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// TestAgentFailureStatesMacOSScreenshots renders each of the six Agent failure states from
// design/editor/AgentStates.dc.html on the real native macOS host for visual inspection
// beside the artboard: Partial (ask + pointing), Stuck, Lost, Stopped, Mismatch, and Offline.
func TestAgentFailureStatesMacOSScreenshots(t *testing.T) {
	newSceneEditor := func(t *testing.T) *editor.Editor {
		t.Helper()
		ed, err := editor.New(editor.DefaultSceneDoc(), nil)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 11, 26, 14, 2, 0, 0, time.UTC)
		ed.Now = func() time.Time { return now }
		ed.SetAppearance(editor.AppearanceDark)
		ed.SetRightRailTab(editor.RightTabAgent)
		ed.AgentScheduleAfter = func(_ time.Duration, _ func()) func() { return func() {} }
		ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
			ID:        "c1",
			Name:      "claude-code",
			Transport: "stdio",
		})
		return ed
	}

	// 1. Partial (ask + "Let me point" active)
	t.Run("partial", func(t *testing.T) {
		ed := newSceneEditor(t)
		ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, _ editor.AgentRequest, emit func(editor.AgentEvent)) error {
			postOnUI(ctx, ed, func() {
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_01", X: 220, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_02", X: 300, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_01", X: 360, Y: 160})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_02", X: 390, Y: 160})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_03", X: 420, Y: 160})
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Read level_01.scene.yaml and slime.prefab.yaml", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "2", StepTitle: "Placed slime_01 and slime_02 · patrol x 220–340", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "3", StepTitle: "slime_03: no free floor 48 px wide; the widest left is 32 px at x 590", StepStatus: editor.AgentStepWarning})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "4", StepTitle: "Placed coin_01 to coin_03 toward the door", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "5", StepTitle: "Ran game check · no problems", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{
				Kind: editor.AgentEventDone,
				Partial: &editor.AgentPartialState{
					DoneCount:   2,
					TotalCount:  3,
					Target:      "slime_03",
					Reason:      "No floor is wide enough for a third patrol. The plank at x 400–576 has room, but the Knight starts on it.",
					PointPrompt: "Click where slime_03 should patrol",
				},
			})
			return nil
		})
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900))
		app.Post(func() {
			if err := ed.SubmitAgentPrompt("Put three slimes patrolling the lower floor and a short coin trail toward the door."); err != nil {
				t.Errorf("SubmitAgentPrompt: %v", err)
			}
		})
		app.WaitUntil(5*time.Second, func() bool { return app.Exists("id:agent-partial-card-0") })
		app.Screenshot("agent-states-partial")
		app.Press("id:agent-partial-point-0")
		app.Settle()
		app.Screenshot("agent-states-partial-pointing")
	})

	// 2. Stuck
	t.Run("stuck", func(t *testing.T) {
		ed := newSceneEditor(t)
		ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, _ editor.AgentRequest, emit func(editor.AgentEvent)) error {
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Read door.prefab.yaml and lever.prefab.yaml", ToolName: "scene_read", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "f1", StepTitle: "Edit scene", ToolName: "scene_edit", CallSummary: "add Door.lock", StepDetail: `unknown component "lock"`, StepStatus: editor.AgentStepFailed})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "f2", StepTitle: "Edit scene", ToolName: "scene_edit", CallSummary: "set Door.trigger.locked: true", StepDetail: `unknown field "locked"`, StepStatus: editor.AgentStepFailed})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "f3", StepTitle: "Edit scene", ToolName: "scene_edit", CallSummary: "set Door.trigger.requires: lever_01", StepDetail: `no node "lever_01" in level_01`, StepStatus: editor.AgentStepFailed})
			<-ctx.Done()
			return ctx.Err()
		})
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900))
		app.Post(func() {
			if err := ed.SubmitAgentPrompt("Make the door open only after lever_01 is pulled."); err != nil {
				t.Errorf("SubmitAgentPrompt: %v", err)
			}
		})
		app.WaitUntil(5*time.Second, func() bool { return app.Exists("id:agent-stuck-card-0") })
		app.Post(func() {
			ed.SetAgentStuckHint("", "The lever is lever_door, under Actors")
		})
		app.Settle()
		app.Screenshot("agent-states-stuck")
	})

	// 3. Lost (connection lost mid-request)
	t.Run("lost", func(t *testing.T) {
		ed := newSceneEditor(t)
		ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, _ editor.AgentRequest, emit func(editor.AgentEvent)) error {
			postOnUI(ctx, ed, func() {
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_04", X: 200, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_05", X: 230, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_06", X: 260, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_07", X: 290, Y: 180})
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Read level_01.scene.yaml", StepStatus: editor.AgentStepDone, PlannedSteps: []string{"Read level_01.scene.yaml", "Placed coin_04 to coin_07", "Placing coin_08", "Game check"}})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "2", StepTitle: "Placed coin_04 to coin_07", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "3", StepTitle: "Placing coin_08", StepStatus: editor.AgentStepRunning})
			<-ctx.Done()
			return ctx.Err()
		})
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900))
		app.Post(func() {
			if err := ed.SubmitAgentPrompt("Add a coin trail from the lever to the door."); err != nil {
				t.Errorf("SubmitAgentPrompt: %v", err)
			}
		})
		app.WaitUntil(5*time.Second, func() bool { return app.Exists("id:agent-step-0-2") })
		app.Post(func() {
			ed.NotifyAgentMCPDisconnected("c1", "")
		})
		app.WaitUntil(5*time.Second, func() bool { return app.Exists("id:agent-lost-card-0") })
		app.Screenshot("agent-states-lost")
	})

	// 4. Stopped
	t.Run("stopped", func(t *testing.T) {
		ed := newSceneEditor(t)
		ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, _ editor.AgentRequest, emit func(editor.AgentEvent)) error {
			postOnUI(ctx, ed, func() {
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_01", X: 220, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_02", X: 280, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_03", X: 340, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_01", X: 360, Y: 160})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_02", X: 390, Y: 160})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_03", X: 420, Y: 160})
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Read the scene and the slime prefab", StepStatus: editor.AgentStepDone, StepIndex: 1, StepTotal: 5, PlannedSteps: []string{"Read the scene and the slime prefab", "Placed slime_01 to slime_03", "Placed coin_01 to coin_03", "Run game check", "Play 10 s headless"}})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "2", StepTitle: "Placed slime_01 to slime_03", StepStatus: editor.AgentStepDone, StepIndex: 2, StepTotal: 5})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "3", StepTitle: "Placed coin_01 to coin_03", StepStatus: editor.AgentStepDone, StepIndex: 3, StepTotal: 5})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "4", StepTitle: "Run game check", StepStatus: editor.AgentStepRunning, StepIndex: 4, StepTotal: 5})
			<-ctx.Done()
			return ctx.Err()
		})
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900))
		app.Post(func() {
			if err := ed.SubmitAgentPrompt("Put three slimes patrolling the lower floor and a short coin trail toward the door."); err != nil {
				t.Errorf("SubmitAgentPrompt: %v", err)
			}
		})
		app.WaitUntil(5*time.Second, func() bool {
			return app.Exists("id:agent-step-0-3") && app.Exists("id:agent-composer-stop")
		})
		app.Press("id:agent-composer-stop")
		app.WaitUntil(5*time.Second, func() bool { return app.Exists("id:agent-stopped-card-0") })
		app.Screenshot("agent-states-stopped")
	})

	// 5. Mismatch
	t.Run("mismatch", func(t *testing.T) {
		ed := newSceneEditor(t)
		ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, _ editor.AgentRequest, emit func(editor.AgentEvent)) error {
			postOnUI(ctx, ed, func() {
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_01", X: 220, Y: 180})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_02", X: 280, Y: 180})
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Placed slime_01 and slime_02", StepStatus: editor.AgentStepDone, StepIndex: 1})
			postOnUI(ctx, ed, func() {
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "slime_03", X: 340, Y: 180})
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "3", StepTitle: "Placed slime_03", StepStatus: editor.AgentStepDone, StepIndex: 3})
			postOnUI(ctx, ed, func() {
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_01", X: 360, Y: 160})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_02", X: 390, Y: 160})
				_, _ = ed.PlaceObject("", sceneformat.ObjectDoc{ID: "coin_03", X: 420, Y: 160})
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "4", StepTitle: "Placed 3 coins", StepStatus: editor.AgentStepDone, StepIndex: 4})
			postOnUI(ctx, ed, func() {
				_ = ed.DeleteObject("slime_03")
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "5", StepTitle: "Retried placement", StepStatus: editor.AgentStepDone, StepIndex: 5})
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Done. Three slimes patrol the lower floor and 3 coins lead to the door. I left the door untouched."})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		})
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900))
		app.Post(func() {
			if err := ed.SubmitAgentPrompt("Put three slimes patrolling the lower floor and a short coin trail toward the door."); err != nil {
				t.Errorf("SubmitAgentPrompt: %v", err)
			}
		})
		app.WaitUntil(5*time.Second, func() bool { return app.Exists("id:agent-mismatch-card-0") })
		app.Screenshot("agent-states-mismatch")
	})

	// 6. Offline
	t.Run("offline", func(t *testing.T) {
		ed := newSceneEditor(t)
		now := time.Date(2026, 11, 26, 14, 10, 0, 0, time.UTC)
		ed.Now = func() time.Time { return now }
		ed.SetAgentOffline(true)
		ed.QueueAgentRequest("Make the coins spin slowly")
		now = now.Add(2 * time.Minute)
		ed.QueueAgentRequest("Add a lever beside the door")

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900))
		app.Settle()
		app.Screenshot("agent-states-offline")
	})
}
