package editor_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/keychain"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func testScriptedFirstRunPlan(req editor.FirstRunPlanRequest) *editor.FirstRunPlan {
	return &editor.FirstRunPlan{
		ProjectName:      req.ProjectName,
		TargetDir:        req.TargetDir,
		DisplayPath:      req.DisplayPath,
		Module:           req.Module,
		Idea:             req.Idea,
		Template:         "platformer2d",
		TemplateTitle:    "2D platformer",
		SceneName:        "level_01",
		EstimatedFiles:   5,
		EstimatedObjects: 4,
		EstimatedMinutes: 2,
		Question: &editor.FirstRunPlanQuestion{
			Prompt: "How should enemies react to the player?",
			Options: []editor.FirstRunPlanOption{
				{ID: "jump", Label: "Bounce defeat", Note: "Suggested", Suggested: true, Verb: "bounce"},
				{ID: "sword", Label: "Melee attack", Note: "Adds an attack", Verb: "fight"},
				{ID: "avoid", Label: "Avoid them", Note: "No combat", Verb: "dodge"},
			},
		},
		Steps: []editor.FirstRunPlanStep{
			{Number: 1, Title: "Copy the 2D platformer template", FilesLabel: "4 new files"},
			{Number: 2, Title: "Build level_01: starter room, player, enemies to bounce, exit gate", FilesLabel: "level_01 · 4 objects"},
			{Number: 3, Title: "Run check", FilesLabel: "no files"},
		},
		Ghosts: []editor.FirstRunGhost{
			{ID: "ghost-rooms", Step: 2, Label: "starter rooms", Kind: "room", X: 110, Y: 138, W: 196, H: 154},
		},
	}
}

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

	ed.FirstRunPlanner = func(_ context.Context, req editor.FirstRunPlanRequest) (*editor.FirstRunPlan, error) {
		return testScriptedFirstRunPlan(req), nil
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
	ed.FirstRunPlanner = func(_ context.Context, req editor.FirstRunPlanRequest) (*editor.FirstRunPlan, error) {
		return testScriptedFirstRunPlan(req), nil
	}
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
	ed.FirstRunPlanner = func(_ context.Context, req editor.FirstRunPlanRequest) (*editor.FirstRunPlan, error) {
		return testScriptedFirstRunPlan(req), nil
	}
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
	ed2.FirstRunPlanner = func(_ context.Context, req editor.FirstRunPlanRequest) (*editor.FirstRunPlan, error) {
		return testScriptedFirstRunPlan(req), nil
	}
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
		kc := keychain.NewMemory()
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
		ed.FirstRunPlanner = func(_ context.Context, req editor.FirstRunPlanRequest) (*editor.FirstRunPlan, error) {
			return testScriptedFirstRunPlan(req), nil
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
		ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
			if strings.Contains(req.Prompt, "Propose a concise first-run plan") {
				emit(editor.AgentEvent{
					Kind: editor.AgentEventMessage,
					Text: `{"template":"starter","sceneName":"level_01","estimatedObjects":1,"steps":[{"number":1,"title":"Create starter project"},{"number":2,"title":"Place player_spawn"}]}`,
				})
				emit(editor.AgentEvent{Kind: editor.AgentEventDone})
				return nil
			}
			// Build stage: place one real object in the open scene.
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepTitle: "Edit the open scene", StepStatus: editor.AgentStepRunning})
			var placeErr error
			ed.RunHeadless(func() {
				_, placeErr = ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "player_spawn", X: 96, Y: 180})
			})
			if placeErr != nil {
				return placeErr
			}
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepTitle: "Edit the open scene", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		})
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

		// Mount in Spec Light mode to verify Start Screen uses light theme.Palette tokens everywhere.
		_ = ed2.SetAppearance(editor.AppearanceLight)
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed2)
		}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: false}))

		light := theme.PaletteFor(false)
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

type stubFirstRunToolExecutor struct {
	tools []editor.AgentToolSpec
	err   error
}

func (s *stubFirstRunToolExecutor) ListTools(_ context.Context) ([]editor.AgentToolSpec, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.tools, nil
}

func (s *stubFirstRunToolExecutor) CallTool(_ context.Context, _ string, _ json.RawMessage) (editor.AgentToolCallResult, error) {
	return editor.AgentToolCallResult{}, nil
}

// #1505 Acceptance 1: With no agent binary and no accepted API key, Start with AI cannot reach Connected:
// prefix-only keys like "sk-123456789012" are validated against BuiltInAgentEndpoint instead of accepted offline,
// and CLI connect fails visibly when the MCP server or CLI runner cannot answer.
func TestFirstRunNoAgentBinaryAndNoKeyCannotReachConnected(t *testing.T) {
	parentDir := t.TempDir()

	var hits atomic.Int32
	var gotKeys []string
	var respStatus atomic.Int32
	respStatus.Store(http.StatusUnauthorized)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotKeys = append(gotKeys, r.Header.Get("x-api-key"))
		st := int(respStatus.Load())
		w.Header().Set("Content-Type", "application/json")
		if st == http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "msg_ping",
				"model":       editor.DefaultBuiltInFastModel,
				"stop_reason": "end_turn",
				"content":     []map[string]any{{"type": "text", "text": "ok"}},
			})
			return
		}
		w.WriteHeader(st)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "authentication_error",
				"message": "invalid x-api-key",
			},
		})
	}))
	defer srv.Close()

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed.BuiltInAgentEndpoint = srv.URL
	ed.BuiltInHTTPClient = srv.Client()
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("no-agent-game")
	ed.SetStartProjectIdea("a kart racer on a beach")
	ed.SetStartScreenOpen(true)
	ed.FirstRunDetectAgents = func() []editor.FirstRunAgentOption {
		return []editor.FirstRunAgentOption{
			{Kind: "claude", Name: "Claude Code", Description: "Not installed", Detected: false},
			{Kind: "codex", Name: "Codex", Description: "Not installed", Detected: false},
			{Kind: "apikey", Name: "An API key", Description: "The editor calls the model itself", Detected: true},
		}
	}

	if err := ed.StartWithAI(); err != nil {
		t.Fatalf("StartWithAI: %v", err)
	}
	if ed.FirstRunStage() != editor.FirstRunStageConnect {
		t.Fatalf("stage = %v, want connect", ed.FirstRunStage())
	}
	if ed.FirstRunConnectCanFinish() || ed.FirstRunAgentConnected() {
		t.Fatal("cannot be connected with no agent binary and no API key")
	}
	if err := ed.FinishFirstRunConnect(); err == nil {
		t.Fatal("FinishFirstRunConnect succeeded without a connected agent or valid API key")
	}
	if err := ed.SelectFirstRunAgent("claude"); err == nil {
		t.Fatal("SelectFirstRunAgent(claude) succeeded when claude is not installed")
	}
	if err := ed.SelectFirstRunAgent("codex"); err == nil {
		t.Fatal("SelectFirstRunAgent(codex) succeeded when codex is not installed")
	}

	// Previously accepted by prefix check alone ("sk-" + >=12 chars); must now hit provider and fail on 401.
	ed.SetFirstRunAPIKey("sk-123456789012")
	if hits.Load() != 1 {
		t.Fatalf("expected 1 HTTP validation request, got %d", hits.Load())
	}
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyInvalid {
		t.Fatalf("401 status = %v, want key_invalid", ed.FirstRunConnectStatus())
	}
	if ed.FirstRunConnectCanFinish() || ed.FirstRunAgentConnected() {
		t.Fatal("rejected API key must not reach Connected")
	}

	// 429 maps to no_quota and still cannot finish.
	respStatus.Store(http.StatusTooManyRequests)
	ed.SetFirstRunAPIKey("sk-quota-123456")
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyNoQuota || ed.FirstRunConnectCanFinish() {
		t.Fatalf("429 status = %v canFinish=%v", ed.FirstRunConnectStatus(), ed.FirstRunConnectCanFinish())
	}

	// 200 maps to key_valid and enables Done.
	respStatus.Store(http.StatusOK)
	ed.SetFirstRunAPIKey("sk-real-key-999")
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyValid || !ed.FirstRunConnectCanFinish() {
		t.Fatalf("200 status = %v canFinish=%v", ed.FirstRunConnectStatus(), ed.FirstRunConnectCanFinish())
	}

	// Closed/unreachable endpoint maps to no_network.
	srv.Close()
	ed.SetFirstRunAPIKey("sk-offline-12345")
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyNoNetwork || ed.FirstRunConnectCanFinish() {
		t.Fatalf("closed server status = %v canFinish=%v", ed.FirstRunConnectStatus(), ed.FirstRunConnectCanFinish())
	}

	// Default CLI connect must also fail visibly when MCP server is absent or CLI fails.
	ed2, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed2.SetStartProjectParentDir(parentDir)
	ed2.SetStartProjectName("cli-fail-game")
	ed2.FirstRunDetectAgents = func() []editor.FirstRunAgentOption {
		return []editor.FirstRunAgentOption{
			{Kind: "claude", Name: "Claude Code", Description: "Found", Detected: true},
			{Kind: "apikey", Name: "An API key", Detected: true},
		}
	}
	ed2.AgentRunner = editor.AgentRunnerFunc(func(context.Context, editor.AgentRequest, func(editor.AgentEvent)) error {
		return errors.New("claude exited with status 1: not signed in")
	})
	if err := ed2.StartWithAI(); err != nil {
		t.Fatalf("StartWithAI(ed2): %v", err)
	}
	ed2.WaitForFirstRunConnect()
	if ed2.FirstRunConnectStatus() != editor.FirstRunConnectTimeout {
		t.Fatalf("expected connect failure status timeout, got %v", ed2.FirstRunConnectStatus())
	}
	if ed2.FirstRunConnectError() == "" {
		t.Fatal("expected non-empty FirstRunConnectError when MCP server is missing")
	}
	if ed2.FirstRunConnectCanFinish() || ed2.FirstRunAgentConnected() {
		t.Fatal("failed CLI connect must not reach Connected")
	}
}

// #1505 Acceptance 2 & 3: A scripted agent drives Connect (counting real MCP tools, never hardcoded 18),
// Plan (showing the agent's beach kart racer plan), Build (placing real objects and skipping playtest honestly
// when playtest.yaml is absent), and Review (listing exactly the written objects with zero artboard strings).
func TestFirstRunScriptedAgentPlanBuildReviewAndNoArtboardStrings(t *testing.T) {
	parentDir := t.TempDir()
	idea := "a kart racer on a beach"

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed.SetProjectTemplates([]editor.ProjectTemplate{
		{Name: "starter", Title: "Starter", Summary: "Minimal 2D starter", Default: true},
		{Name: "racing3d", Title: "3D racer", Summary: "Laps, ghosts, AI drivers"},
	})
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("beach-karts")
	ed.SetStartProjectIdea(idea)
	ed.SetStartScreenOpen(true)

	// Wire 23 tools (deliberately not 18) to verify the real MCP tool count is reported.
	toolSpecs := make([]editor.AgentToolSpec, 23)
	for i := range toolSpecs {
		toolSpecs[i] = editor.AgentToolSpec{Name: "tool_" + string(rune('a'+i))}
	}
	ed.AgentTools = &stubFirstRunToolExecutor{tools: toolSpecs}

	ed.FirstRunDetectAgents = func() []editor.FirstRunAgentOption {
		return []editor.FirstRunAgentOption{
			{Kind: "claude", Name: "Claude Code", Description: "Found on this computer", Detected: true, ExecutablePath: "/usr/local/bin/claude"},
			{Kind: "apikey", Name: "An API key", Detected: true},
		}
	}

	var sawConnectProbe, sawPlanPrompt, sawBuildPrompt bool
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		switch {
		case strings.Contains(req.Prompt, "Connection check"):
			sawConnectProbe = true
			emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "READY"})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		case strings.Contains(req.Prompt, "Propose a concise first-run plan"):
			sawPlanPrompt = true
			if !strings.Contains(req.Prompt, idea) {
				t.Errorf("plan prompt missing author idea %q: %s", idea, req.Prompt)
			}
			emit(editor.AgentEvent{
				Kind: editor.AgentEventMessage,
				Text: `{
					"template": "racing3d",
					"sceneName": "beach_circuit",
					"estimatedFiles": 3,
					"estimatedObjects": 4,
					"estimatedMinutes": 2,
					"question": {
						"prompt": "How should beach drift boosts trigger?",
						"options": [
							{"id": "drift", "label": "Hold drift", "note": "Suggested", "suggested": true, "stepTitleOverrides": {"2": "Build beach_circuit with drift boost pads"}},
							{"id": "pads", "label": "Track pads only", "note": "Simpler handling", "stepTitleOverrides": {"2": "Build beach_circuit with sand boost pads"}}
						]
					},
					"steps": [
						{"number": 1, "title": "Create beach-karts from 3D racer", "filesLabel": "game.yaml"},
						{"number": 2, "title": "Build beach_circuit with kart, dunes, palms and boost pad", "filesLabel": "scenes/beach_circuit.scene.yaml · 4 objects"},
						{"number": 3, "title": "Run project check", "filesLabel": "no files"}
					],
					"ghosts": [
						{"id": "beach-track", "step": 2, "label": "beach loop", "x": 60, "y": 140, "w": 220, "h": 110}
					]
				}`,
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		default:
			sawBuildPrompt = true
			if !strings.Contains(req.Prompt, "pads") {
				t.Errorf("build prompt missing selected answer 'pads': %s", req.Prompt)
			}
			// Place 4 beach kart objects directly into the open scene (simulating scene_edit).
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepTitle: "Place beach kart objects", StepStatus: editor.AgentStepRunning})
			var placeErr error
			ed.RunHeadless(func() {
				for _, obj := range []sceneformat.ObjectDoc{
					{ID: "kart_player", Name: "Beach Kart", X: 100, Y: 200},
					{ID: "sand_dune_01", X: 260, Y: 180},
					{ID: "palm_tree_01", X: 340, Y: 120},
					{ID: "boost_pad_01", X: 480, Y: 210},
				} {
					if _, err := ed.PlaceObject("actors", obj); err != nil {
						placeErr = err
						return
					}
				}
			})
			if placeErr != nil {
				return placeErr
			}
			emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepTitle: "Place beach kart objects", StepStatus: editor.AgentStepDone})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return nil
		}
	})

	// 1. Connect via real defaultFirstRunConnectAgent.
	if err := ed.StartWithAI(); err != nil {
		t.Fatalf("StartWithAI: %v", err)
	}
	ed.WaitForFirstRunConnect()
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectConnected {
		t.Fatalf("connect status = %v (err=%q), want connected", ed.FirstRunConnectStatus(), ed.FirstRunConnectError())
	}
	if !sawConnectProbe {
		t.Fatal("defaultFirstRunConnectAgent never invoked the agent runner")
	}
	steps := ed.FirstRunConnectSteps()
	if len(steps) < 3 || !strings.Contains(steps[2].Text, "23 tools") {
		t.Fatalf("connect step 3 = %+v, want real count '23 tools' (not 18)", steps)
	}

	// 2. Finish Connect -> Plan via real defaultFirstRunPlan.
	if err := ed.FinishFirstRunConnect(); err != nil {
		t.Fatalf("FinishFirstRunConnect: %v", err)
	}
	if !sawPlanPrompt {
		t.Fatal("OpenFirstRunPlan never invoked the agent runner")
	}
	if _, _, tools := ed.FirstRunConnectedAgent(); tools != 23 {
		t.Fatalf("connected tools = %d, want 23", tools)
	}
	plan := ed.FirstRunPlan()
	if plan == nil || plan.Template != "racing3d" || plan.SceneName != "beach_circuit" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if plan.Question == nil || plan.Question.Prompt != "How should beach drift boosts trigger?" {
		t.Fatalf("unexpected plan question: %+v", plan.Question)
	}
	if !ed.SelectFirstRunPlanAnswer("pads") {
		t.Fatal("SelectFirstRunPlanAnswer(pads) returned false")
	}
	plan = ed.FirstRunPlan()
	if !strings.Contains(plan.Steps[1].Title, "sand boost pads") {
		t.Fatalf("StepTitleOverrides did not update step 2: %+v", plan.Steps)
	}

	// 3. Approve -> Build via real defaultFirstRunBuilder.
	if err := ed.ApproveFirstRunPlan(); err != nil {
		t.Fatalf("ApproveFirstRunPlan: %v", err)
	}
	ed.WaitForFirstRunBuild()
	if !sawBuildPrompt {
		t.Fatal("defaultFirstRunBuilder never invoked the agent runner")
	}
	if ed.FirstRunStage() != editor.FirstRunStageReview {
		t.Fatalf("stage after build = %v, want review (build=%+v)", ed.FirstRunStage(), ed.FirstRunBuildState())
	}

	// Stage 4 must honestly report Skipped because no playtest.yaml exists.
	buildSnap := ed.FirstRunBuildState()
	if len(buildSnap.Log) < 5 || !strings.Contains(buildSnap.Log[4].Subtitle, "Skipped") {
		t.Fatalf("expected honest playtest skip in build log[4], got %+v", buildSnap.Log)
	}

	// 4. Verify written scene on disk matches review objects 1-to-1, with zero artboard strings anywhere.
	scenePath := filepath.Join(parentDir, "beach-karts", "scenes", "beach_circuit.scene.yaml")
	rawScene, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", scenePath, err)
	}
	parsedScene, err := sceneformat.Parse(rawScene)
	if err != nil {
		t.Fatalf("Parse written scene: %v", err)
	}
	var diskIDs []string
	for _, l := range parsedScene.Layers {
		for _, o := range l.Objects {
			diskIDs = append(diskIDs, o.ID)
		}
	}
	wantIDs := []string{"kart_player", "sand_dune_01", "palm_tree_01", "boost_pad_01"}
	if !reflect.DeepEqual(diskIDs, wantIDs) {
		t.Fatalf("written scene object IDs = %v, want %v", diskIDs, wantIDs)
	}

	review := ed.FirstRunReviewState()
	var reviewObjIDs []string
	var combinedReviewText strings.Builder
	for _, c := range review.Changes {
		reviewObjIDs = append(reviewObjIDs, c.ObjectIDs...)
		combinedReviewText.WriteString(c.Title + " " + c.Detail + "\n")
	}
	if !reflect.DeepEqual(reviewObjIDs, wantIDs) {
		t.Fatalf("review object IDs = %v, want exact match with written scene %v", reviewObjIDs, wantIDs)
	}

	// Forbidden design-artboard strings must never appear in plan, build log, scene file, or review.
	var audit strings.Builder
	for _, s := range plan.Steps {
		audit.WriteString(s.Title + " " + s.FilesLabel + "\n")
	}
	for _, l := range buildSnap.Log {
		audit.WriteString(l.Title + " " + l.Subtitle + "\n")
	}
	audit.WriteString(string(rawScene))
	audit.WriteString(combinedReviewText.String())
	lowerAudit := strings.ToLower(audit.String())
	for _, forbidden := range []string{"knight", "slime", "coin_", "lever", "door_exit", "612 tiles", "3 rooms", "10 s playtest", "18 tools"} {
		if strings.Contains(lowerAudit, forbidden) {
			t.Fatalf("forbidden artboard string %q leaked into first-run output:\n%s", forbidden, audit.String())
		}
	}
}

// #1505 Playtest stage: when playtest.yaml exists in the created project, Stage 4 runs FirstRunPlaytest
// (or `game playtest`) and reports pass or missed goals honestly.
func TestFirstRunPlaytestStageRunsWhenPlaytestYAMLExists(t *testing.T) {
	runCase := func(t *testing.T, name string, rep *playtest.Report, ptErr error, wantSub string, wantDeviation string) {
		t.Helper()
		parentDir := t.TempDir()
		ed, err := editor.NewDemo()
		if err != nil {
			t.Fatal(err)
		}
		ed.SetFirstRunAgentConnected(true, "claude", "Claude Code", 12)
		ed.SetStartProjectParentDir(parentDir)
		ed.SetStartProjectName(name)
		ed.SetStartProjectIdea("a card battler")
		ed.FirstRunPlanner = func(_ context.Context, req editor.FirstRunPlanRequest) (*editor.FirstRunPlan, error) {
			return &editor.FirstRunPlan{
				ProjectName:   req.ProjectName,
				TargetDir:     req.TargetDir,
				DisplayPath:   req.DisplayPath,
				Template:      "cards2d",
				TemplateTitle: "Card game",
				SceneName:     "table_01",
				Steps: []editor.FirstRunPlanStep{
					{Number: 1, Title: "Create project"},
					{Number: 2, Title: "Place deck"},
				},
			}, nil
		}
		ed.CreateProject = func(_ context.Context, req editor.CreateProjectRequest, _, _ io.Writer) error {
			dir := req.TargetDir()
			if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
				return err
			}
			pf := &project.File{
				Version:    project.CurrentVersion,
				Name:       req.Name,
				ID:         "dev.nsgo.cardtest",
				AppVersion: "0.1.0",
				Window:     project.Window{Width: 640, Height: 360},
			}
			data, err := project.Encode(pf)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, project.FileName), data, 0o644); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, playtest.FileName), []byte("version: 1\n"), 0o644)
		}
		ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, _ editor.AgentRequest, emit func(editor.AgentEvent)) error {
			var placeErr error
			ed.RunHeadless(func() {
				_, placeErr = ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "deck_pile", X: 120, Y: 180})
			})
			emit(editor.AgentEvent{Kind: editor.AgentEventDone})
			return placeErr
		})
		var playtestCalled bool
		ed.FirstRunPlaytest = func(_ context.Context, projectDir string) (*playtest.Report, error) {
			playtestCalled = true
			if filepath.Base(projectDir) != name {
				t.Errorf("playtest projectDir = %q, want base %q", projectDir, name)
			}
			return rep, ptErr
		}

		if err := ed.StartWithAI(); err != nil {
			t.Fatalf("StartWithAI: %v", err)
		}
		if err := ed.ApproveFirstRunPlan(); err != nil {
			t.Fatalf("ApproveFirstRunPlan: %v", err)
		}
		ed.WaitForFirstRunBuild()

		if !playtestCalled {
			t.Fatal("expected FirstRunPlaytest to be invoked when playtest.yaml exists")
		}
		snap := ed.FirstRunBuildState()
		if len(snap.Log) < 5 || !strings.Contains(snap.Log[4].Subtitle, wantSub) {
			t.Fatalf("stage 4 log = %+v, want subtitle containing %q", snap.Log[4], wantSub)
		}
		rev := ed.FirstRunReviewState()
		if wantDeviation == "" {
			if len(rev.Deviations) != 0 {
				t.Fatalf("unexpected deviations on passing playtest: %+v", rev.Deviations)
			}
		} else {
			found := false
			for _, d := range rev.Deviations {
				if strings.Contains(d.Summary, wantDeviation) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected deviation containing %q, got %+v", wantDeviation, rev.Deviations)
			}
		}
	}

	t.Run("passing_playtest", func(t *testing.T) {
		runCase(t, "cards-pass", &playtest.Report{
			Passed: true,
			Runs:   []playtest.RunReport{{Seed: 1}, {Seed: 2}},
		}, nil, "passed · 2 runs · 0 missed goals", "")
	})
	t.Run("failing_playtest", func(t *testing.T) {
		runCase(t, "cards-fail", &playtest.Report{
			Passed: false,
			Metrics: []playtest.MetricReport{
				{Name: "win_rate", Status: "missed"},
			},
		}, nil, "failed · 1 missed goal(s)", "Playtest: failed · 1 missed goal(s)")
	})
}
