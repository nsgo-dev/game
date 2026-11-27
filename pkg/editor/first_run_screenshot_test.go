package editor_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestFirstRunMacOSScreenshots renders all five stages of Start with AI on the real native macOS host
// driven by a scripted agent over the real editormcp.Server (Start with WHAT HAPPENS NEXT, Connect in place
// with real MCP tool count, First Plan from the agent, Building with live object counts, and First Review
// listing the exact written scene objects with tip 1 of 3).
func TestFirstRunMacOSScreenshots(t *testing.T) {
	parentDir := t.TempDir()
	settingsFile := filepath.Join(t.TempDir(), "editor.yaml")

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.LoadSettings(settingsFile); err != nil {
		t.Fatal(err)
	}
	ed.SetProjectTemplates([]editor.ProjectTemplate{
		{Name: "starter", Title: "Starter", Summary: "An empty scene and a loop", Dimension: "2d", Default: true},
		{Name: "platformer2d", Title: "2D platformer", Summary: "Rooms, checkpoints, coyote time", Dimension: "2d"},
		{Name: "rpg2d", Title: "2D RPG", Summary: "Town, dialogue, quests, saves", Dimension: "2d"},
		{Name: "racing3d", Title: "3D racer", Summary: "Laps, ghosts, AI drivers", Dimension: "3d"},
	})
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("beach-karts")
	ed.SetStartProjectIdea("A kart racer on a sunny beach with boost pads, palm trees and a finish arch.")
	ed.SetStartScreenOpen(true)

	ed.FirstRunDetectAgents = func() []editor.FirstRunAgentOption {
		return []editor.FirstRunAgentOption{
			{Kind: "claude", Name: "Claude Code", Description: "Found on this computer", Detected: true, ExecutablePath: "/usr/local/bin/claude"},
			{Kind: "codex", Name: "Codex", Description: "Not installed", Detected: false},
			{Kind: "apikey", Name: "An API key", Description: "The editor calls the model itself", Detected: true},
		}
	}

	stage2Ready := make(chan struct{})
	releaseBuild := make(chan struct{})
	var app *nsgotest.App

	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		switch {
		case req.ScenePath == "" && req.ProjectRoot == "":
			if req.Task == "" {
				// Connect readiness probe.
				emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "READY"})
				emit(editor.AgentEvent{Kind: editor.AgentEventDone})
				return nil
			}
			// Planning request.
			emit(editor.AgentEvent{
				Kind: editor.AgentEventMessage,
				Text: `{
					"template": "racing3d",
					"sceneName": "beach_circuit",
					"estimatedFiles": 3,
					"estimatedObjects": 4,
					"estimatedMinutes": 2,
					"question": {
						"prompt": "How should beach boost pads work?",
						"options": [
							{"id": "pad", "label": "Speed pads on sand", "note": "Suggested", "suggested": true},
							{"id": "drift", "label": "Drift charge", "note": "Skill-based boost"}
						]
					},
					"steps": [
						{"number": 1, "title": "Create beach-karts from 3D racer", "filesLabel": "game.yaml"},
						{"number": 2, "title": "Build beach_circuit: player kart, 2 palm trees, boost pad and finish arch", "filesLabel": "beach_circuit · 4 objects"},
						{"number": 3, "title": "Run project check", "filesLabel": "no files"}
					],
					"ghosts": [
						{"id": "track-loop", "step": 2, "label": "beach circuit", "kind": "room", "x": 52, "y": 132, "w": 244, "h": 146}
					]
				}`,
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		default:
			// Build request: place first two objects, pause for building screenshot, then finish.
			addObj := func(obj sceneformat.ObjectDoc) {
				done := make(chan struct{})
				app.Post(func() {
					_, _ = ed.PlaceObject("actors", obj)
					close(done)
				})
				<-done
			}
			addObj(sceneformat.ObjectDoc{ID: "kart_player", Name: "Player Kart", X: 96, Y: 220})
			addObj(sceneformat.ObjectDoc{ID: "palm_tree_01", Name: "Palm Tree", X: 220, Y: 140})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepTitle: "Place beach circuit actors", StepStatus: editor.AgentStepRunning})
			close(stage2Ready)
			select {
			case <-releaseBuild:
			case <-ctx.Done():
				return ctx.Err()
			}
			addObj(sceneformat.ObjectDoc{ID: "boost_pad_01", Name: "Boost Pad", X: 360, Y: 220})
			addObj(sceneformat.ObjectDoc{ID: "finish_arch", Name: "Finish Arch", X: 520, Y: 190})
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepTitle: "Place beach circuit actors", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		}
	})

	app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	s := editormcp.New(ed, editormcp.Options{Run: app.Post})
	t.Cleanup(func() { _ = s.Close() })

	// 1. Start screen with Start with AI and WHAT HAPPENS NEXT.
	app.Screenshot("first-run-1-start")

	// 2. Connect in place on Start screen (real MCP tool count).
	app.Press("id:start-ai-btn")
	app.WaitUntil(3*time.Second, func() bool {
		return ed.FirstRunConnectStatus() == editor.FirstRunConnectConnected
	})
	app.Settle()
	app.Screenshot("first-run-2-connect")

	// 3. The first plan (read-only template preview + agent ghosts + clarifying question).
	app.Press("id:first-run-connect-done")
	app.WaitUntil(3*time.Second, func() bool {
		return !ed.FirstRunPlanLoading() && ed.FirstRunPlan() != nil
	})
	app.Settle()
	app.Screenshot("first-run-3-plan")

	// 4. Building (5-stage bar + real object counts + live placing pill).
	app.Press("id:first-run-plan-just-do-it")
	app.WaitUntil(5*time.Second, func() bool {
		select {
		case <-stage2Ready:
			return true
		default:
			return false
		}
	})
	app.Settle()
	app.Screenshot("first-run-4-building")

	// 5. The first review (exact written scene objects + tip 1 of 3).
	close(releaseBuild)
	app.WaitUntil(5*time.Second, func() bool {
		return ed.FirstRunStage() == editor.FirstRunStageReview
	})
	app.Settle()
	app.Screenshot("first-run-5-review")
}
