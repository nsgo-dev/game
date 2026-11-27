package editor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1051 Acceptance 1: With no agent connected, Start with AI opens Connect in place on the Start screen,
// keeps the idea and name through Connect, shows WHAT HAPPENS NEXT, opens a read-only template preview
// with planned ghosts, and creates NO project folder until Approve.
func TestFirstRunNoAgentConnectInPlaceKeepsIdeaAndNameAndNoFolderUntilApprove(t *testing.T) {
	parentDir := t.TempDir()
	targetDir := filepath.Join(parentDir, "dungeon-run")
	idea := "A short dungeon platformer: a knight jumps on slimes, collects coins and pulls a lever to open the exit."

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed.SetProjectTemplates([]editor.ProjectTemplate{
		{Name: "starter", Title: "Starter", Summary: "An empty scene and a loop", Dimension: "2d", Default: true},
		{Name: "platformer2d", Title: "2D platformer", Summary: "Rooms, checkpoints, coyote time", Dimension: "2d"},
	})
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("dungeon-run")
	ed.SetStartProjectIdea(idea)
	ed.SetStartScreenOpen(true)

	var copiedDiag string
	ed.WriteClipboard = func(text string) { copiedDiag = text }

	ed.FirstRunDetectAgents = func() []editor.FirstRunAgentOption {
		return []editor.FirstRunAgentOption{
			{Kind: "claude", Name: "Claude Code", Description: "Found on this computer", Detected: true, ExecutablePath: "/usr/local/bin/claude"},
			{Kind: "codex", Name: "Codex", Description: "Not installed", Detected: false},
			{Kind: "apikey", Name: "An API key", Description: "The editor calls the model itself", Detected: true},
		}
	}

	failFirst := true
	ed.FirstRunConnectAgent = func(_ context.Context, kind, _ string) (int, error) {
		if failFirst {
			failFirst = false
			return 0, errors.New("timed out after 30 s")
		}
		return 18, nil
	}
	ed.FirstRunValidateAPIKey = func(_ context.Context, key string) editor.FirstRunAPIKeyStatus {
		switch key {
		case "sk-good-key-123456":
			return editor.FirstRunAPIKeyWorks
		case "sk-offline":
			return editor.FirstRunAPIKeyNoNetwork
		case "sk-quota":
			return editor.FirstRunAPIKeyNoQuota
		default:
			return editor.FirstRunAPIKeyNotAccepted
		}
	}

	// Hold the builder after Approve so we can assert folder creation timing.
	buildStarted := make(chan struct{})
	releaseBuild := make(chan struct{})
	ed.FirstRunBuilder = func(ctx context.Context, req editor.FirstRunBuildRequest, emit func(editor.FirstRunBuildEvent)) (*editor.FirstRunBuildResult, error) {
		close(buildStarted)
		emit(editor.FirstRunBuildEvent{
			StageIndex:   2,
			StageStatus:  editor.FirstRunBuildStageActive,
			LogTitle:     "Building level_01",
			LogSubtitle:  "19 of 31 objects",
			ObjectsDone:  19,
			ObjectsTotal: 31,
			PlacingLabel: "placing slime_03",
		})
		select {
		case <-releaseBuild:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &editor.FirstRunBuildResult{
			SceneRelPath:   "scenes/level_01.scene.yaml",
			PlanStepsDone:  3,
			PlanStepsTotal: 3,
			Changes: []editor.FirstRunReviewChange{
				{ID: "tpl", Sign: "+", Title: "Template files", Detail: "game.yaml, prefabs, logic · 14"},
			},
		}, nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	// Step 1: WHAT HAPPENS NEXT lists the 3 steps and Start with AI is enabled.
	if !app.Exists("id:start-what-happens-next") {
		t.Fatal("WHAT HAPPENS NEXT section did not render on Start screen")
	}
	nextText := allText(app.Find("id:start-what-happens-next"))
	for _, want := range []string{"WHAT HAPPENS NEXT", "Connect an agent", "Approve its plan", "Review the first scene"} {
		if !strings.Contains(nextText, want) {
			t.Fatalf("WHAT HAPPENS NEXT missing %q: %q", want, nextText)
		}
	}

	// Click Start with AI with no agent connected -> opens Connect in place on Start screen.
	app.Press("id:start-ai-btn")
	app.WaitUntil(2*time.Second, func() bool {
		return ed.FirstRunConnectStatus() == editor.FirstRunConnectTimeout
	})
	app.Settle()

	if !ed.StartScreenOpen() || ed.FirstRunStage() != editor.FirstRunStageConnect {
		t.Fatalf("expected Connect in place on Start screen: startOpen=%v stage=%v", ed.StartScreenOpen(), ed.FirstRunStage())
	}
	if ed.StartProjectIdea() != idea || ed.StartProjectName() != "dungeon-run" {
		t.Fatalf("idea/name lost in Connect: idea=%q name=%q", ed.StartProjectIdea(), ed.StartProjectName())
	}
	if got := app.Find("id:first-run-connect-idea").Text; got != idea {
		t.Fatalf("connect summary idea = %q, want %q", got, idea)
	}
	if got := app.Find("id:first-run-connect-name").Text; got != "dungeon-run" {
		t.Fatalf("connect summary name = %q, want dungeon-run", got)
	}
	// Timeout alert + Copy diagnostics confirms in place.
	if !app.Exists("id:first-run-connect-timeout") {
		t.Fatal("timeout alert did not appear")
	}
	if app.Enabled("id:first-run-connect-done") {
		t.Fatal("Done should be disabled while not connected")
	}
	app.Press("id:first-run-connect-copy-diag")
	if !ed.FirstRunDiagnosticsCopied() || !strings.Contains(copiedDiag, "Claude Code") {
		t.Fatalf("Copy diagnostics failed: copied=%v text=%q", ed.FirstRunDiagnosticsCopied(), copiedDiag)
	}

	// Switch to API key -> test invalid / offline / quota / valid states (and never leak key in diagnostics).
	app.Press("id:first-run-agent-apikey")
	app.Post(func() { ed.SetFirstRunAPIKey("bad") })
	app.Settle()
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyInvalid || app.Enabled("id:first-run-connect-done") {
		t.Fatalf("bad API key status=%v doneEnabled=%v", ed.FirstRunConnectStatus(), app.Enabled("id:first-run-connect-done"))
	}
	app.Post(func() { ed.SetFirstRunAPIKey("sk-offline") })
	app.Settle()
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyNoNetwork || app.Enabled("id:first-run-connect-done") {
		t.Fatalf("offline API key status=%v", ed.FirstRunConnectStatus())
	}
	app.Post(func() { ed.SetFirstRunAPIKey("sk-quota") })
	app.Settle()
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyNoQuota || app.Enabled("id:first-run-connect-done") {
		t.Fatalf("quota API key status=%v", ed.FirstRunConnectStatus())
	}
	app.Post(func() { ed.SetFirstRunAPIKey("sk-good-key-123456") })
	app.Settle()
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyValid || !app.Enabled("id:first-run-connect-done") {
		t.Fatalf("valid API key status=%v doneEnabled=%v", ed.FirstRunConnectStatus(), app.Enabled("id:first-run-connect-done"))
	}

	// Edit returns to Start screen keeping idea and name, then Start with AI reconnects Claude Code.
	app.Press("id:first-run-connect-edit")
	if !ed.StartScreenOpen() || ed.FirstRunStage() != editor.FirstRunStageNone {
		t.Fatalf("Edit did not return to Start screen: startOpen=%v stage=%v", ed.StartScreenOpen(), ed.FirstRunStage())
	}
	if ed.StartProjectIdea() != idea || ed.StartProjectName() != "dungeon-run" {
		t.Fatalf("idea/name lost after Edit: idea=%q name=%q", ed.StartProjectIdea(), ed.StartProjectName())
	}

	app.Press("id:start-ai-btn")
	app.WaitUntil(2*time.Second, func() bool {
		return ed.FirstRunConnectStatus() == editor.FirstRunConnectConnected
	})
	app.Settle()
	if !app.Enabled("id:first-run-connect-done") {
		t.Fatal("Done should be enabled once Claude Code connects")
	}

	// CRITICAL: Folder MUST NOT exist on disk after Connect!
	if _, err := os.Stat(targetDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project folder created prematurely during Connect: stat err=%v", err)
	}

	// Click Done -> Step 3 (The first plan).
	app.Press("id:first-run-connect-done")
	if ed.FirstRunStage() != editor.FirstRunStagePlan || !app.Exists("id:first-run-plan") {
		t.Fatalf("Done did not open First Plan: stage=%v", ed.FirstRunStage())
	}
	if !app.Exists("id:first-run-plan-readonly-pill") || !app.Exists("id:first-run-ghost-ghost-rooms") {
		t.Fatal("Plan view missing read-only template preview pill or planned ghosts")
	}
	// CRITICAL: Folder STILL MUST NOT exist during Plan!
	if _, err := os.Stat(targetDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project folder created prematurely during Plan: stat err=%v", err)
	}
	// Approve disabled until clarifying question is answered.
	if app.Enabled("id:first-run-plan-approve") {
		t.Fatal("Approve should be disabled before answering clarifying question")
	}
	app.Press("id:first-run-plan-answer-sword")
	if !app.Enabled("id:first-run-plan-approve") {
		t.Fatal("Approve should be enabled after picking an answer")
	}
	plan := ed.FirstRunPlan()
	if plan == nil || !strings.Contains(plan.Steps[1].Title, "fight") {
		t.Fatalf("selecting Sword swing did not update step 2: %+v", plan)
	}

	// Approve creates the folder and starts Building!
	app.Press("id:first-run-plan-approve")
	<-buildStarted
	app.Settle()

	if ed.FirstRunStage() != editor.FirstRunStageBuilding || !app.Exists("id:first-run-building") {
		t.Fatalf("Approve did not enter Building: stage=%v", ed.FirstRunStage())
	}
	if info, err := os.Stat(targetDir); err != nil || !info.IsDir() {
		t.Fatalf("Approve did not create target project folder %s: err=%v", targetDir, err)
	}

	close(releaseBuild)
	app.WaitUntil(3*time.Second, func() bool {
		return ed.FirstRunStage() == editor.FirstRunStageReview
	})
	app.Settle()
}

// #1051 Acceptance 2: Building shows 5-stage bar + counts; Cancel/Esc asks first; Stop and delete the folder
// removes ONLY the folder created in this flow (leaving sibling/existing folders untouched); failed Check stops
// before Review with the agent's proposed fix.
func TestFirstRunBuildingCancelDeleteSafetyAndFailedCheckFix(t *testing.T) {
	parentDir := t.TempDir()
	siblingDir := filepath.Join(parentDir, "sibling-project")
	if err := os.MkdirAll(siblingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	siblingSentinel := filepath.Join(siblingDir, "keep-me.txt")
	if err := os.WriteFile(siblingSentinel, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	idea := "A dungeon platformer with a knight, slimes, coins and a lever."
	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed.SetFirstRunAgentConnected(true, "claude", "Claude Code", 18)
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("dungeon-run")
	ed.SetStartProjectIdea(idea)
	ed.SetStartScreenOpen(true)

	inStage2 := make(chan struct{})
	releaseStage2 := make(chan struct{})
	ed.FirstRunBuilder = func(ctx context.Context, req editor.FirstRunBuildRequest, emit func(editor.FirstRunBuildEvent)) (*editor.FirstRunBuildResult, error) {
		// Write a marker file inside the newly created folder.
		_ = os.WriteFile(filepath.Join(req.ProjectDir, "draft.txt"), []byte("agent"), 0o644)
		emit(editor.FirstRunBuildEvent{
			StageIndex:   1,
			StageStatus:  editor.FirstRunBuildStageDone,
			LogTitle:     "Created " + req.Plan.DisplayPath,
			LogSubtitle:  "14 files from the template",
			ObjectsDone:  0,
			ObjectsTotal: 31,
		})
		emit(editor.FirstRunBuildEvent{
			StageIndex:   2,
			StageStatus:  editor.FirstRunBuildStageActive,
			LogTitle:     "Building level_01",
			LogSubtitle:  "19 of 31 objects",
			ObjectsDone:  19,
			ObjectsTotal: 31,
			PlacingLabel: "placing slime_03",
		})
		close(inStage2)
		select {
		case <-releaseStage2:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if err := req.WaitIfPaused(ctx); err != nil {
			return nil, err
		}
		return &editor.FirstRunBuildResult{}, nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	// Already connected -> Start with AI goes straight to Plan; Just do it picks suggested answer and builds.
	app.Press("id:start-ai-btn")
	if ed.FirstRunStage() != editor.FirstRunStagePlan {
		t.Fatalf("expected direct transition to Plan when agent connected, got %v", ed.FirstRunStage())
	}
	app.Press("id:first-run-plan-just-do-it")
	<-inStage2
	app.Settle()

	// Verify 5-stage bar and object counts in Agent panel.
	stageBarText := allText(app.Find("id:first-run-build-stage-bar"))
	for _, stageName := range editor.FirstRunBuildStageNames {
		if !strings.Contains(stageBarText, stageName) {
			t.Fatalf("stage bar missing %q: %q", stageName, stageBarText)
		}
	}
	if sub := app.Find("id:first-run-build-log-sub-2").Text; !strings.Contains(sub, "19 of 31 objects") {
		t.Fatalf("build stage 2 subtitle = %q, want 19 of 31 objects", sub)
	}

	// Esc asks first: Keep building, Stop keep files, Stop and delete the folder.
	app.Key("escape")
	if !app.Exists("id:first-run-cancel-dialog") {
		t.Fatal("Esc during Building did not open Stop building confirmation dialog")
	}
	if !app.Exists("id:first-run-cancel-keep-building") || !app.Exists("id:first-run-cancel-stop-keep") || !app.Exists("id:first-run-cancel-stop-delete") {
		t.Fatal("Cancel dialog missing expected buttons")
	}

	// Keep building resumes cleanly.
	app.Press("id:first-run-cancel-keep-building")
	if app.Exists("id:first-run-cancel-dialog") || ed.FirstRunBuildState().Outcome != editor.FirstRunBuildRunning {
		t.Fatalf("Keep building did not resume: outcome=%v", ed.FirstRunBuildState().Outcome)
	}

	// Cancel button -> Stop and delete the folder removes ONLY dungeon-run, preserving sibling-project.
	app.Press("id:first-run-build-cancel")
	app.Press("id:first-run-cancel-stop-delete")
	targetDir := filepath.Join(parentDir, "dungeon-run")
	if _, err := os.Stat(targetDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stop and delete did not remove %s: err=%v", targetDir, err)
	}
	if data, err := os.ReadFile(siblingSentinel); err != nil || string(data) != "precious" {
		t.Fatalf("sibling folder was disturbed! err=%v data=%q", err, data)
	}
	if !app.Exists("id:first-run-stopped-deleted-card") {
		t.Fatal("stopped-deleted confirmation card did not render")
	}

	// Back to Start preserves idea & name, and name is free again.
	app.Press("id:first-run-stopped-back-start")
	if !ed.StartScreenOpen() || ed.StartProjectIdea() != idea || ed.StartProjectName() != "dungeon-run" || !ed.StartNameStatus().OK {
		t.Fatalf("after Back to Start: open=%v idea=%q name=%q ok=%v", ed.StartScreenOpen(), ed.StartProjectIdea(), ed.StartProjectName(), ed.StartNameStatus().OK)
	}

	// CanDeleteFirstRunFolder must be false when no flow-created folder is owned.
	if ed.CanDeleteFirstRunFolder() {
		t.Fatal("CanDeleteFirstRunFolder should be false after deletion")
	}

	// Now test failed Check stopping before review with proposed fix + Fix it advancing to review!
	fixApplied := false
	ed.FirstRunBuilder = func(_ context.Context, req editor.FirstRunBuildRequest, _ func(editor.FirstRunBuildEvent)) (*editor.FirstRunBuildResult, error) {
		return &editor.FirstRunBuildResult{
			SceneRelPath:   "scenes/level_01.scene.yaml",
			PlanStepsDone:  3,
			PlanStepsTotal: 3,
			Deviations:     []editor.FirstRunPlanDeviation{{Summary: "11 coins, not 12: one sat inside a wall"}},
			Changes: []editor.FirstRunReviewChange{
				{ID: "lever", Sign: "+", Title: "lever_01, door_2", Detail: "pull to open the exit"},
			},
			CheckFailure: &editor.FirstRunBuildFixProposal{
				ProblemSummary: "lever_01.target is door_2, which does not exist",
				ObjectID:       "lever_01",
				ObjectBadge:    "lever_01 · target missing",
				FixDescription: "I'll add door_2 at the end of room 2 so the lever has something to open.",
				ApplyFix: func(_ context.Context, _ string) error {
					fixApplied = true
					return nil
				},
			},
		}, nil
	}

	app.Press("id:start-ai-btn")
	app.Press("id:first-run-plan-just-do-it")
	app.WaitUntil(3*time.Second, func() bool {
		return ed.FirstRunBuildState().Outcome == editor.FirstRunBuildCheckFailed
	})
	app.Settle()

	if ed.FirstRunStage() != editor.FirstRunStageBuilding {
		t.Fatalf("failed Check should stay in Building stage, got %v", ed.FirstRunStage())
	}
	if !app.Exists("id:first-run-check-failed-card") || !app.Exists("id:first-run-build-problem-pill") {
		t.Fatal("failed Check card or viewport problem pill did not render")
	}
	app.Press("id:first-run-check-fix-it")
	if !fixApplied {
		t.Fatal("Fix it did not invoke ApplyFix")
	}
	if ed.FirstRunStage() != editor.FirstRunStageReview {
		t.Fatalf("Fix it did not advance to First Review, stage=%v", ed.FirstRunStage())
	}
	// Clean up created project dir is handled by t.TempDir().
}

// #1051 Acceptance 3: First Review displays plan summary + amber deviations, Before/After + hold B,
// per-change revert/restore, and three tips shown one at a time and once per person (persisted in editor.yaml),
// ending all three on Skip tips and returning via Help › Replay tips.
func TestFirstRunReviewPlanDeviationsBeforeHoldBTipsPersistenceAndHelpReplay(t *testing.T) {
	parentDir := t.TempDir()
	settingsFile := filepath.Join(t.TempDir(), "editor.yaml")

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.LoadSettings(settingsFile); err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	ed.SetFirstRunAgentConnected(true, "claude", "Claude Code", 18)
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("dungeon-run")
	ed.SetStartProjectIdea("A short dungeon platformer: a knight jumps on slimes, collects coins and pulls a lever.")
	ed.SetStartScreenOpen(true)

	ed.FirstRunBuilder = func(_ context.Context, req editor.FirstRunBuildRequest, _ func(editor.FirstRunBuildEvent)) (*editor.FirstRunBuildResult, error) {
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
				{ID: "k", ChangeID: "knight", Kind: "knight", X: 54, Y: 218, W: 16, H: 30, Outline: "added"},
			},
		}, nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	app.Press("id:start-ai-btn")
	app.Press("id:first-run-plan-just-do-it")
	app.WaitUntil(3*time.Second, func() bool {
		return ed.FirstRunStage() == editor.FirstRunStageReview
	})
	app.Settle()

	// Plan summary + amber deviation on top of review.
	if got := app.Find("id:first-run-review-plan-steps").Text; got != "Plan: 3 of 3 steps" {
		t.Fatalf("plan steps = %q", got)
	}
	if got := app.Find("id:first-run-review-deviation-0").Text; !strings.Contains(got, "11 coins, not 12") {
		t.Fatalf("deviation = %q", got)
	}

	// Before/After toggle shows "Before: an empty folder".
	app.Press("id:first-run-review-before")
	if !app.Exists("id:first-run-review-before-pill") {
		t.Fatal("Before pill did not appear")
	}
	app.Press("id:first-run-review-after")
	if app.Exists("id:first-run-review-before-pill") {
		t.Fatal("Before pill stayed visible in After mode")
	}

	// Hold B peek shows Before cleanly too.
	app.Post(func() { ed.SetFirstRunReviewPeekBefore(true) })
	app.Settle()
	if !ed.FirstRunReviewState().ShowingBefore || !app.Exists("id:first-run-review-before-pill") {
		t.Fatal("hold-B peek did not show Before view")
	}
	app.Post(func() { ed.SetFirstRunReviewPeekBefore(false) })
	app.Settle()

	// Per-change revert/restore updates Keep count.
	app.Press("id:first-run-review-toggle-coins")
	if st := ed.FirstRunReviewState(); st.KeptCount != 5 || st.TotalCount != 6 {
		t.Fatalf("after reverting coins: kept=%d/%d, want 5/6", st.KeptCount, st.TotalCount)
	}
	app.Press("id:first-run-review-toggle-coins")
	if st := ed.FirstRunReviewState(); st.KeptCount != 6 {
		t.Fatalf("after restoring coins: kept=%d, want 6", st.KeptCount)
	}

	// Three tips shown ONE AT A TIME.
	if !app.Exists("id:first-run-tip-0") || app.Exists("id:first-run-tip-1") || app.Exists("id:first-run-tip-2") {
		t.Fatal("expected only tip 1 of 3 visible initially")
	}
	if got := app.Find("id:first-run-tip-step").Text; got != "1 of 3" {
		t.Fatalf("tip 0 step = %q, want 1 of 3", got)
	}

	app.Press("id:first-run-tip-next")
	if app.Exists("id:first-run-tip-0") || !app.Exists("id:first-run-tip-1") {
		t.Fatal("Next from tip 0 did not advance to tip 1")
	}
	if got := app.Find("id:first-run-tip-body").Text; !strings.Contains(got, "Hold B") {
		t.Fatalf("tip 1 body = %q", got)
	}

	app.Press("id:first-run-tip-next")
	if app.Exists("id:first-run-tip-1") || !app.Exists("id:first-run-tip-2") {
		t.Fatal("Next from tip 1 did not advance to tip 2")
	}
	if got := app.Find("id:first-run-tip-body").Text; !strings.Contains(got, "⌘L") {
		t.Fatalf("tip 2 body = %q, want ⌘L on macOS", got)
	}

	app.Press("id:first-run-tip-next") // Got it
	if app.Exists("id:first-run-tip-2") || ed.FirstRunReviewState().ActiveTipIndex != -1 {
		t.Fatal("Got it did not dismiss tip 2")
	}

	// Verify seenTips persisted in editor.yaml and round-trips strictly.
	saved, err := editor.LoadEditorSettings(settingsFile)
	if err != nil {
		t.Fatalf("LoadEditorSettings: %v", err)
	}
	wantTips := []string{editor.FirstRunTipAskSelection, editor.FirstRunTipHoldB, editor.FirstRunTipKeepRevert}
	if !reflect.DeepEqual(saved.SeenTips, wantTips) {
		t.Fatalf("persisted SeenTips = %v, want %v", saved.SeenTips, wantTips)
	}
	encoded, err := editor.EncodeEditorSettings(saved)
	if err != nil {
		t.Fatalf("EncodeEditorSettings: %v", err)
	}
	reparsed, err := editor.ParseEditorSettings(encoded)
	if err != nil || !reflect.DeepEqual(reparsed.SeenTips, wantTips) {
		t.Fatalf("settings round-trip failed: err=%v tips=%v", err, reparsed)
	}

	// Help › Replay tips brings all three tips back and clears them in editor.yaml!
	app.ChooseMenu("Help", "Replay tips")
	if !app.Exists("id:first-run-tip-0") || ed.FirstRunReviewState().ActiveTipIndex != 0 {
		t.Fatalf("Help > Replay tips did not restore tip 0: activeTip=%d", ed.FirstRunReviewState().ActiveTipIndex)
	}
	replayed, err := editor.LoadEditorSettings(settingsFile)
	if err != nil || len(replayed.SeenTips) != 0 {
		t.Fatalf("after Help > Replay tips, persisted SeenTips = %v (err=%v), want empty", replayed.SeenTips, err)
	}

	// Skip tips ends all three immediately and persists all three as seen.
	app.Press("id:first-run-tip-skip")
	if app.Exists("id:first-run-tip-0") || app.Exists("id:first-run-tip-1") || app.Exists("id:first-run-tip-2") {
		t.Fatal("Skip tips did not end all three tips")
	}
	afterSkip, err := editor.LoadEditorSettings(settingsFile)
	if err != nil || !reflect.DeepEqual(afterSkip.SeenTips, wantTips) {
		t.Fatalf("after Skip tips, persisted SeenTips = %v (err=%v), want %v", afterSkip.SeenTips, err, wantTips)
	}

	// Restarting the editor with the same settings file does NOT re-show tips on a new review.
	ed2, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	if err := ed2.LoadSettings(settingsFile); err != nil {
		t.Fatal(err)
	}
	ed2.SetFirstRunAgentConnected(true, "claude", "Claude Code", 18)
	ed2.SetStartProjectParentDir(parentDir)
	ed2.SetStartProjectName("dungeon-run-2")
	ed2.SetStartProjectIdea("Second dungeon run")
	ed2.FirstRunBuilder = func(_ context.Context, _ editor.FirstRunBuildRequest, _ func(editor.FirstRunBuildEvent)) (*editor.FirstRunBuildResult, error) {
		return &editor.FirstRunBuildResult{
			SceneRelPath:   "scenes/level_01.scene.yaml",
			PlanStepsDone:  3,
			PlanStepsTotal: 3,
			Changes:        []editor.FirstRunReviewChange{{ID: "tpl", Sign: "+", Title: "Template files", Detail: "14"}},
		}, nil
	}
	if err := ed2.StartWithAI(); err != nil {
		t.Fatal(err)
	}
	if err := ed2.JustDoItFirstRunPlan(); err != nil {
		t.Fatal(err)
	}
	ed2.WaitForFirstRunBuild()
	if got := ed2.FirstRunReviewState().ActiveTipIndex; got != -1 {
		t.Fatalf("restarted editor with persisted seenTips showed tip %d, want -1", got)
	}

	// Keep finishes the first review cleanly into the editor workspace.
	app.Press("id:first-run-review-keep")
	if ed.FirstRunStage() != editor.FirstRunStageNone || ed.StartScreenOpen() {
		t.Fatalf("Keep did not exit review into editor: stage=%v startOpen=%v", ed.FirstRunStage(), ed.StartScreenOpen())
	}
	if toast, ok := ed.ActiveToast(); !ok || !strings.Contains(toast.Message, "Kept 6 of 6") {
		t.Fatalf("expected Kept toast after finishing review, got ok=%v toast=%+v", ok, toast)
	}
}

// Regression tests covering PR #1220 blockers:
// 1. Typing into first-run-api-key-input keeps focus without remounting and persists key to Keychain + #1200 AgentMode.
// 2. Atomic os.Mkdir existing-dir guard, SceneName/TargetDir traversal rejection, and valid project.Check scaffold.
// 3. Handshake bounded timeout under -race, #1200 MCP agent sync, #1206 HasOpenReview autosave pause, and Spec Light Start screen.
func TestFirstRunSecurityFocusConcurrencySpecLightAndIntegrations(t *testing.T) {
	t.Run("api_key_input_typing_focus_and_keychain_sync", func(t *testing.T) {
		parentDir := t.TempDir()
		kc := editor.NewMemoryKeychain()
		ed, err := editor.NewDemo()
		if err != nil {
			t.Fatal(err)
		}
		ed.Keychain = kc
		ed.SetStartProjectParentDir(parentDir)
		ed.SetStartProjectName("key-focus-game")
		ed.SetStartProjectIdea("A puzzle game")
		ed.SetStartScreenOpen(true)
		ed.FirstRunDetectAgents = func() []editor.FirstRunAgentOption {
			return []editor.FirstRunAgentOption{
				{Kind: "claude", Name: "Claude Code", Description: "Not installed", Detected: false},
				{Kind: "apikey", Name: "An API key", Description: "The editor calls the model itself", Detected: true},
			}
		}
		ed.FirstRunValidateAPIKey = func(ctx context.Context, key string) editor.FirstRunAPIKeyStatus {
			// Verify validation does not hold firstRun.mu (re-entrant read must not deadlock) and has a deadline.
			if _, ok := ctx.Deadline(); !ok {
				t.Error("expected bounded context deadline in FirstRunValidateAPIKey")
			}
			_ = ed.FirstRunSelectedAgent()
			if strings.HasPrefix(key, "sk-valid") {
				return editor.FirstRunAPIKeyWorks
			}
			return editor.FirstRunAPIKeyNotAccepted
		}

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1280, 800))

		app.Press("id:start-ai-btn")
		app.Settle()

		// Focus and type character-by-character into the masked API key input; it must never lose focus/remount!
		app.Type("id:first-run-api-key-input", "sk-valid-123456")
		app.Settle()
		if !ed.IsTyping() {
			t.Fatal("expected first-run-api-key-input to remain focused (IsTyping() == true) after typing")
		}

		if got := ed.FirstRunAPIKey(); got != "sk-valid-123456" {
			t.Fatalf("typed API key = %q, want %q (input likely remounted during typing)", got, "sk-valid-123456")
		}
		if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyValid {
			t.Fatalf("connect status = %v, want key_valid", ed.FirstRunConnectStatus())
		}
		if !app.Enabled("id:first-run-connect-done") {
			t.Fatal("Done button should be enabled for valid API key")
		}

		// Finishing Connect syncs #1200 AgentModeAPIKey and saves the key to Editor.Keychain.
		app.Press("id:first-run-connect-done")
		app.Settle()
		if ed.AgentMode() != editor.AgentModeAPIKey {
			t.Fatalf("AgentMode() = %q, want %q", ed.AgentMode(), editor.AgentModeAPIKey)
		}
		stored, err := kc.Get(editor.FirstRunAPIKeyKeychainService, editor.FirstRunAPIKeyKeychainAccount)
		if err != nil || stored != "sk-valid-123456" {
			t.Fatalf("Keychain.Get = (%q, %v), want (%q, nil)", stored, err, "sk-valid-123456")
		}
		if !ed.FirstRunAPIKeyStoredInKeychain() {
			t.Fatal("expected FirstRunAPIKeyStoredInKeychain() = true")
		}
	})

	t.Run("path_traversal_atomic_mkdir_and_default_builder_check", func(t *testing.T) {
		parentDir := t.TempDir()
		ed, err := editor.NewDemo()
		if err != nil {
			t.Fatal(err)
		}
		ed.SetFirstRunAgentConnected(true, "claude", "Claude Code", 18)
		ed.SetStartProjectParentDir(parentDir)
		ed.SetStartProjectName("safe-game")
		ed.SetStartProjectIdea("A dungeon platformer")

		// 1. Planner attempting SceneName traversal must be rejected.
		ed.FirstRunPlanner = func(_ context.Context, req editor.FirstRunPlanRequest) (*editor.FirstRunPlan, error) {
			p := editor.DefaultFirstRunPlan(req)
			p.SceneName = "../../escape"
			return p, nil
		}
		if err := ed.OpenFirstRunPlan(); err == nil {
			t.Fatal("expected OpenFirstRunPlan to reject SceneName path traversal")
		}

		// 2. Planner attempting TargetDir hijack must be rejected.
		ed.FirstRunPlanner = func(_ context.Context, req editor.FirstRunPlanRequest) (*editor.FirstRunPlan, error) {
			p := editor.DefaultFirstRunPlan(req)
			p.TargetDir = filepath.Join(parentDir, "other-dir")
			return p, nil
		}
		if err := ed.OpenFirstRunPlan(); err == nil {
			t.Fatal("expected OpenFirstRunPlan to reject mismatched planner TargetDir")
		}

		// 3. Atomic os.Mkdir refuses a folder created between Plan and Approve without claiming ownership.
		ed.FirstRunPlanner = nil
		if err := ed.OpenFirstRunPlan(); err != nil {
			t.Fatalf("OpenFirstRunPlan: %v", err)
		}
		targetDir := filepath.Join(parentDir, "safe-game")
		if err := os.Mkdir(targetDir, 0o755); err != nil {
			t.Fatal(err)
		}
		sentinel := filepath.Join(targetDir, "preexisting.txt")
		if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ed.JustDoItFirstRunPlan(); err == nil {
			t.Fatal("expected ApproveFirstRunPlan to refuse pre-existing directory")
		}
		if ed.CanDeleteFirstRunFolder() {
			t.Fatal("CanDeleteFirstRunFolder must be false when pre-existing folder was refused")
		}
		if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep" {
			t.Fatalf("pre-existing folder modified: err=%v data=%q", err, got)
		}

		// 4. Default pure-Go builder writes valid project.File game.yaml passing project.Check and enters Review.
		ed.SetStartProjectName("valid-scaffold")
		if err := ed.StartWithAI(); err != nil {
			t.Fatalf("StartWithAI: %v", err)
		}
		if err := ed.JustDoItFirstRunPlan(); err != nil {
			t.Fatalf("JustDoItFirstRunPlan: %v", err)
		}
		ed.WaitForFirstRunBuild()
		if ed.FirstRunStage() != editor.FirstRunStageReview {
			t.Fatalf("defaultFirstRunBuilder stage = %v, want review (outcome=%v)", ed.FirstRunStage(), ed.FirstRunBuildState().Outcome)
		}
		builtDir := filepath.Join(parentDir, "valid-scaffold")
		if _, err := project.Check(os.DirFS(builtDir)); err != nil {
			t.Fatalf("project.Check on defaultFirstRunBuilder output failed: %v", err)
		}
		// #1206 Autosave guard: HasOpenReview() must be true during First Review and false after Keep.
		if !ed.HasOpenReview() {
			t.Fatal("HasOpenReview() = false during FirstRunStageReview, want true")
		}
		if err := ed.KeepFirstRunReview(); err != nil {
			t.Fatalf("KeepFirstRunReview: %v", err)
		}
		if ed.HasOpenReview() {
			t.Fatal("HasOpenReview() = true after KeepFirstRunReview, want false")
		}
	})

	t.Run("mcp_agent_sync_handshake_timeout_and_spec_light_start_screen", func(t *testing.T) {
		parentDir := t.TempDir()
		ed, err := editor.NewDemo()
		if err != nil {
			t.Fatal(err)
		}
		ed.SetStartProjectParentDir(parentDir)
		ed.SetStartProjectName("mcp-sync-game")
		ed.SetStartScreenOpen(true)

		// #1200 MCP client connected -> Start with AI sees it as already connected and skips Connect in place.
		ed.NotifyAgentMCPConnected(editor.AgentClientInfo{Name: "claude-code", DisplayName: "Claude Code"})
		if !ed.FirstRunAgentConnected() {
			t.Fatal("expected FirstRunAgentConnected() = true when #1200 MCP client is connected")
		}
		if kind, name, _ := ed.FirstRunConnectedAgent(); kind != "claude" || name != "Claude Code" {
			t.Fatalf("FirstRunConnectedAgent() = (%q, %q), want (claude, Claude Code)", kind, name)
		}

		// Handshake timeout triggers FirstRunConnectTimeout cleanly without leaking goroutines under -race.
		ed2, err := editor.NewDemo()
		if err != nil {
			t.Fatal(err)
		}
		ed2.SetStartProjectParentDir(parentDir)
		ed2.SetStartProjectName("timeout-game")
		ed2.SetStartScreenOpen(true)
		ed2.FirstRunConnectTimeout = 40 * time.Millisecond
		ed2.FirstRunDetectAgents = func() []editor.FirstRunAgentOption {
			return []editor.FirstRunAgentOption{
				{Kind: "claude", Name: "Claude Code", Detected: true},
				{Kind: "apikey", Name: "An API key", Detected: true},
			}
		}
		ed2.FirstRunConnectAgent = func(ctx context.Context, _, _ string) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		}

		// Mount in Spec Light mode to verify Start Screen uses light SpecPalette tokens everywhere.
		_ = ed2.SetAppearance(editor.AppearanceLight)
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed2)
		}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: false}))

		light := editor.SpecPaletteFor(false)
		if got := app.Find("id:editor-start-screen").Style.Background; got != light.Bg {
			t.Fatalf("Spec Light editor-start-screen background = %v, want %v", got, light.Bg)
		}
		if got := app.Find("id:start-sidebar").Style.Background; got != light.Panel {
			t.Fatalf("Spec Light start-sidebar background = %v, want %v", got, light.Panel)
		}
		if got := app.Find("id:start-templates-empty").Style.Background; got != light.Raised {
			t.Fatalf("Spec Light start-templates-empty background = %v, want %v", got, light.Raised)
		}
		if got := app.Find("id:start-templates-empty").Style.BorderColor; got != light.LineRaised {
			t.Fatalf("Spec Light start-templates-empty border color = %v, want %v", got, light.LineRaised)
		}

		app.Press("id:start-ai-btn")
		app.WaitUntil(2*time.Second, func() bool {
			return ed2.FirstRunConnectStatus() == editor.FirstRunConnectTimeout
		})
		app.Settle()
		if !app.Exists("id:first-run-connect-timeout") {
			t.Fatal("expected timeout alert after FirstRunConnectTimeout elapsed")
		}
	})
}
