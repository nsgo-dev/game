package editor_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func agentScreenshotScene() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#15181D",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{ID: "knight", Name: "Knight", X: 360, Y: 220},
					{ID: "slime_01", Name: "slime_01", X: 220, Y: 280},
					{ID: "slime_02", Name: "slime_02", X: 280, Y: 280},
					{ID: "slime_03", Name: "slime_03", X: 340, Y: 280},
				},
			},
		},
	}
}

// TestAgentWorkingScreenshots captures real macOS native screenshots of the Agent panel
// matching design/editor/Working.dc.html and Permissions.dc.html (Dark and Light themes).
func TestAgentWorkingScreenshots(t *testing.T) {
	ed, err := editor.New(agentScreenshotScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	_ = ed.SetLanguage(editor.LanguageEnglish)
	_ = ed.SetAppearance(editor.AppearanceDark)
	_ = ed.SelectPermissionProfile(editor.PermissionProfileCautious)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:          "c1",
		Name:        "claude-code",
		DisplayName: "Claude Code",
		Transport:   "stdio",
	})
	ed.SetRightRailTab(editor.RightTabAgent)

	base := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	now := base
	ed.Now = func() time.Time { return now }

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	evalPlaytest := ed.EvaluatePermission(editor.PermissionRequest{
		Tool:   "game_playtest",
		Action: editor.PermissionActionRun,
		Target: editor.PermissionRunPlaytests,
	})

	// 1. Working + Permission Card (matching Working.dc.html).
	app.Post(func() {
		ed.StartAgentRequest("Put three slimes patrolling the lower floor and a short coin trail toward the door.")
		s1 := ed.BeginAgentToolStep("scene_read", "Read level_01.scene.yaml, slime.prefab.yaml", "", nil)
		ed.FinishAgentToolStep(s1, "Read level_01.scene.yaml, slime.prefab.yaml", "", nil, false)
		s2 := ed.BeginAgentToolStep("scene_edit", "Placed 3 slimes on the 120 px floor span, x 220–340", "", []string{"slime_01", "slime_02", "slime_03"})
		ed.FinishAgentToolStep(s2, "Placed 3 slimes on the 120 px floor span, x 220–340", "", []string{"slime_01", "slime_02", "slime_03"}, false)
		s3 := ed.BeginAgentToolStep("scene_edit", "Placed 3 coins toward the door", "", nil)
		ed.FinishAgentToolStep(s3, "Placed 3 coins toward the door", "", nil, false)
		s4 := ed.BeginAgentToolStep("game_check", "Ran game check", "", nil)
		ed.FinishAgentToolStep(s4, "Ran game check", "no problems", nil, false)
		s5 := ed.BeginAgentToolStep("game_playtest", "Play 10 s headless", "Agent is playing level_01 headless · manual edits stay available", nil)
		ed.BeginAgentPermissionAsk(editor.AgentPermissionPrompt{
			StepID:        s5,
			Title:         "Allow the agent to run the game?",
			ShortAction:   "the playtest",
			Why:           "It wants to play level_01 headless for 10 seconds to check the slimes stay on the floor.",
			Effect:        "Nothing is saved.",
			RuleSource:    evalPlaytest.Reason,
			RulePreview:   editor.AlwaysRuleForEvaluation(evalPlaytest).Summary(),
			AlwaysLabel:   "Always allow headless playtests",
			Tool:          "game_playtest",
			PermissionKey: "run:playtests",
			Evaluation:    evalPlaytest,
		})
		now = base.Add(32 * time.Second)
	})
	app.Settle()
	app.Screenshot("agent-working-permission-card")

	// 2. Denied + Not playtested summary (matching Permissions.dc.html Denied).
	app.Post(func() {
		ed.DenyPermission()
		ed.CompleteAgentRequest("Done, but not playtested: couldn't confirm the slimes stay on the floor. Play it yourself with ⌘P, or allow the playtest above.")
	})
	app.Settle()
	app.Screenshot("agent-denied-summary")

	// 3. Stopped + "Agent stopped" toast with Review.
	app.Post(func() {
		ed.StartAgentRequest("Put three slimes patrolling the lower floor and a short coin trail toward the door.")
		s1 := ed.BeginAgentToolStep("scene_read", "Read level_01.scene.yaml, slime.prefab.yaml", "", nil)
		ed.FinishAgentToolStep(s1, "Read level_01.scene.yaml, slime.prefab.yaml", "", nil, false)
		s2 := ed.BeginAgentToolStep("scene_edit", "Placed 3 slimes on the lower floor", "", []string{"slime_01", "slime_02", "slime_03"})
		ed.FinishAgentToolStep(s2, "Placed 3 slimes on the lower floor", "", []string{"slime_01", "slime_02", "slime_03"}, false)
		s3 := ed.BeginAgentToolStep("game_check", "Ran game check", "", nil)
		ed.FinishAgentToolStep(s3, "Ran game check", "no problems", nil, false)
		s4 := ed.BeginAgentToolStep("game_playtest", "the playtest", "", nil)
		ed.BeginAgentPermissionAsk(editor.AgentPermissionPrompt{
			StepID:      s4,
			Title:       "Allow the agent to run the game?",
			ShortAction: "the playtest",
			Tool:        "game_playtest",
			Evaluation:  evalPlaytest,
		})
		ed.StopAgent()
	})
	app.Settle()
	app.Screenshot("agent-stopped-toast")

	// 4. Light theme parity check for Working + Permission Card.
	app.Post(func() {
		ed.DismissToast()
		_ = ed.SetAppearance(editor.AppearanceLight)
		ed.StartAgentRequest("Put three slimes patrolling the lower floor.")
		s1 := ed.BeginAgentToolStep("scene_read", "Read level_01.scene.yaml", "", nil)
		ed.FinishAgentToolStep(s1, "Read level_01.scene.yaml", "", nil, false)
		s2 := ed.BeginAgentToolStep("game_playtest", "Play 10 s headless", "", nil)
		ed.BeginAgentPermissionAsk(editor.AgentPermissionPrompt{
			StepID:        s2,
			Title:         "Allow the agent to run the game?",
			ShortAction:   "the playtest",
			Why:           "It wants to play level_01 headless for 10 seconds.",
			Effect:        "Nothing is saved.",
			RuleSource:    evalPlaytest.Reason,
			RulePreview:   editor.AlwaysRuleForEvaluation(evalPlaytest).Summary(),
			AlwaysLabel:   "Always allow headless playtests",
			Tool:          "game_playtest",
			PermissionKey: "run:playtests",
			Evaluation:    evalPlaytest,
		})
	})
	app.Settle()
	app.Screenshot("agent-working-light")
}
