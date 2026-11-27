package editorcmd_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestFirstRunMacOSScreenshots renders all five stages of FirstRun.dc.html on the real native macOS host
// (Start with WHAT HAPPENS NEXT, Connect in place, First Plan with read-only template preview & ghosts,
// Building with 5-stage bar & counts, and First Review with plan deviations & tip callout).
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
	ed.SetStartProjectName("dungeon-run")
	ed.SetStartProjectIdea("A short dungeon platformer: a knight jumps on slimes, collects coins and pulls a lever to open the exit.")
	ed.SetStartScreenOpen(true)

	ed.FirstRunDetectAgents = func() []editor.FirstRunAgentOption {
		return []editor.FirstRunAgentOption{
			{Kind: "claude", Name: "Claude Code", Description: "Found on this computer", Detected: true, ExecutablePath: "/usr/local/bin/claude"},
			{Kind: "codex", Name: "Codex", Description: "Not installed", Detected: false},
			{Kind: "apikey", Name: "An API key", Description: "The editor calls the model itself", Detected: true},
		}
	}
	ed.FirstRunConnectAgent = func(_ context.Context, _, _ string) (int, error) {
		return 18, nil
	}

	stage2Ready := make(chan struct{})
	releaseBuild := make(chan struct{})
	ed.FirstRunBuilder = func(ctx context.Context, req editor.FirstRunBuildRequest, emit func(editor.FirstRunBuildEvent)) (*editor.FirstRunBuildResult, error) {
		emit(editor.FirstRunBuildEvent{
			StageIndex:  1,
			StageStatus: editor.FirstRunBuildStageDone,
			LogTitle:    "Created " + req.Plan.DisplayPath,
			LogSubtitle: "14 files from the template",
		})
		emit(editor.FirstRunBuildEvent{
			StageIndex:   2,
			StageStatus:  editor.FirstRunBuildStageActive,
			LogTitle:     "Building level_01",
			LogSubtitle:  "19 of 31 objects",
			ObjectsDone:  19,
			ObjectsTotal: 31,
			PlacingLabel: "placing slime_03",
			PlacedShapes: []editor.FirstRunPreviewShape{
				{ID: "f", Kind: "floor", X: 16, Y: 248, W: 340, H: 40, Outline: "added"},
				{ID: "k", Kind: "knight", X: 54, Y: 218, W: 16, H: 30, Outline: "added"},
				{ID: "s1", Kind: "slime", X: 94, Y: 234, W: 22, H: 14, Outline: "added"},
				{ID: "s2", Kind: "slime", X: 152, Y: 234, W: 22, H: 14, Outline: "added"},
				{ID: "s3", Kind: "slime", X: 214, Y: 234, W: 22, H: 14, Outline: "agent"},
				{ID: "c1", Kind: "coin", X: 140, Y: 164, W: 9, H: 9, Outline: "added"},
				{ID: "c2", Kind: "coin", X: 156, Y: 164, W: 9, H: 9, Outline: "added"},
			},
		})
		close(stage2Ready)
		select {
		case <-releaseBuild:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &editor.FirstRunBuildResult{
			SceneRelPath:   "scenes/level_01.scene.yaml",
			PlanStepsDone:  3,
			PlanStepsTotal: 3,
			Deviations:     []editor.FirstRunPlanDeviation{{Summary: "11 coins, not 12: one sat inside a wall"}},
			Changes: []editor.FirstRunReviewChange{
				{ID: "tpl", Sign: "+", Title: "Template files", Detail: "game.yaml, prefabs, logic · 14"},
				{ID: "tiles", Sign: "+", Title: "World tiles", Detail: "3 rooms · 612 tiles"},
				{ID: "knight", Sign: "+", Title: "Knight", Detail: "player · spawn x 64"},
				{ID: "slimes", Sign: "+", Title: "slime_01 … 04", Detail: "patrol · stomp to defeat"},
				{ID: "coins", Sign: "+", Title: "coin_01 … 11", Detail: "pickup · value 1"},
				{ID: "lever", Sign: "+", Title: "lever_01, door_exit", Detail: "pull to open the exit"},
			},
			Shapes: []editor.FirstRunPreviewShape{
				{ID: "sh-floor", ChangeID: "tiles", Kind: "floor", X: 16, Y: 248, W: 340, H: 40, Outline: "added"},
				{ID: "sh-plat", ChangeID: "tiles", Kind: "platform", X: 128, Y: 184, W: 76, H: 10},
				{ID: "sh-knight", ChangeID: "knight", Kind: "knight", X: 54, Y: 218, W: 16, H: 30, Outline: "added"},
				{ID: "sh-slime1", ChangeID: "slimes", Kind: "slime", X: 94, Y: 234, W: 22, H: 14, Outline: "added"},
				{ID: "sh-slime2", ChangeID: "slimes", Kind: "slime", X: 152, Y: 234, W: 22, H: 14, Outline: "added"},
				{ID: "sh-slime3", ChangeID: "slimes", Kind: "slime", X: 214, Y: 234, W: 22, H: 14, Outline: "added"},
				{ID: "sh-coin1", ChangeID: "coins", Kind: "coin", X: 140, Y: 164, W: 9, H: 9, Outline: "added"},
				{ID: "sh-coin2", ChangeID: "coins", Kind: "coin", X: 156, Y: 164, W: 9, H: 9, Outline: "added"},
				{ID: "sh-lever", ChangeID: "lever", Kind: "lever", X: 254, Y: 232, W: 6, H: 16, Outline: "added"},
				{ID: "sh-door", ChangeID: "lever", Kind: "door", X: 318, Y: 208, W: 18, H: 40, Outline: "added"},
			},
		}, nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// 1. Start screen with Start with AI and WHAT HAPPENS NEXT.
	app.Screenshot("first-run-1-start")

	// 2. Connect in place on Start screen.
	app.Press("id:start-ai-btn")
	app.WaitUntil(2*time.Second, func() bool {
		return ed.FirstRunConnectStatus() == editor.FirstRunConnectConnected
	})
	app.Settle()
	app.Screenshot("first-run-2-connect")

	// 3. The first plan (read-only template preview + ghosts + clarifying question).
	app.Press("id:first-run-connect-done")
	app.Settle()
	app.Screenshot("first-run-3-plan")

	// 4. Building (5-stage bar + counts + live placing pill).
	app.Press("id:first-run-plan-just-do-it")
	<-stage2Ready
	app.Settle()
	app.Screenshot("first-run-4-building")

	// 5. The first review (plan summary + amber deviation + tip 1 of 3).
	close(releaseBuild)
	app.WaitUntil(3*time.Second, func() bool {
		return ed.FirstRunStage() == editor.FirstRunStageReview
	})
	app.Settle()
	app.Screenshot("first-run-5-review")
}
