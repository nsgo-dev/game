package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAgentPanelPlaceholderLiveStepsAndInputFocusStability(t *testing.T) {
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.SetRightRailTab(editor.RightTabAgent)

	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, focusDesktopEnv, nsgotest.Size(1440, 900))
	app.Settle()

	// Starting a request immediately shows the gray placeholder step (guaranteed within 300 ms).
	ed.StartAgentRequest("Put three slimes patrolling the lower floor.")
	app.Settle()
	if !app.Exists("id:agent-step-placeholder") {
		t.Fatal("expected gray placeholder step immediately after starting request")
	}
	if !app.Exists("id:agent-composer-stop") || !app.Exists("id:agent-add-input") {
		t.Fatal("expected Add to this request input and Stop button while working")
	}

	// Focus "Add to this request" and verify streaming steps do NOT remount or steal focus from it.
	app.Focus("id:agent-add-input")
	app.Settle()

	s1 := ed.BeginAgentToolStep("scene_read", "Read level_01.scene.yaml", "Agent is reading level_01 · manual edits stay available", nil)
	ed.FinishAgentToolStep(s1, "Read level_01.scene.yaml", "", nil, false)
	s2 := ed.BeginAgentToolStep("scene_edit", "Placing 3 coins toward the door", "Agent is editing level_01 · manual edits stay available", []string{"coin_01", "coin_02", "coin_03"})
	app.Settle()

	if fv := focusedView(app); fv == nil || fv.Props.TestID != "agent-add-input" {
		t.Fatalf("streaming steps stole focus from agent-add-input: got %v", fv)
	}
	if app.Exists("id:agent-step-placeholder") {
		t.Fatal("placeholder step should disappear once real steps arrive")
	}
	snap := ed.AgentSession()
	if len(snap.Steps) != 2 {
		t.Fatalf("len(steps) = %d, want 2", len(snap.Steps))
	}
	if snap.Steps[0].Status != editor.AgentStepDone || snap.Steps[1].Status != editor.AgentStepRunning {
		t.Fatalf("step statuses = [%s, %s], want [done, running]", snap.Steps[0].Status, snap.Steps[1].Status)
	}
	ed.FinishAgentToolStep(s2, "Placed 3 coins toward the door", "", []string{"coin_01", "coin_02", "coin_03"}, false)
	app.Settle()

	// Manual edits stay available while the agent works and status bar shows live activity.
	if err := ed.MoveObject("hero", 200, 190); err != nil {
		t.Fatalf("manual MoveObject failed while agent working: %v", err)
	}
	if !app.Exists("id:status-agent-activity") {
		t.Fatal("expected status bar to report live agent activity while working")
	}
}

func TestPermissionCardFocusDisciplineAndShortcuts(t *testing.T) {
	dir := t.TempDir()
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	permPath := filepath.Join(dir, editor.PermissionsFileName)
	if err := ed.LoadProjectPermissions(permPath); err != nil {
		t.Fatalf("LoadProjectPermissions: %v", err)
	}
	if err := ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatalf("SelectPermissionProfile: %v", err)
	}
	ed.SetRightRailTab(editor.RightTabAgent)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, focusDesktopEnv, nsgotest.Size(1440, 900))
	app.Settle()

	// 1. Focus is outside Agent panel (in toolbar): permission card must NOT steal focus (Spec rule 05).
	app.Focus("id:toolbar-play")
	app.Settle()

	ed.StartAgentRequest("Check the slimes with a playtest.")
	stepID := ed.BeginAgentToolStep("game_playtest", "Play 10 s headless", "Agent is playing level_01 headless · manual edits stay available", nil)
	evalPlaytest := ed.EvaluatePermission(editor.PermissionRequest{
		Tool:   "game_playtest",
		Action: editor.PermissionActionRun,
		Target: editor.PermissionRunPlaytests,
	})
	askCh, _, denied := ed.BeginAgentPermissionAsk(editor.AgentPermissionPrompt{
		StepID:        stepID,
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
	if denied {
		t.Fatal("unexpected alreadyDenied on fresh request")
	}
	app.Settle()

	card := app.Find("id:agent-permission-card")
	if card == nil {
		t.Fatal("permission card not mounted")
	}
	if card.Props.Role != core.RoleAlertDialog {
		t.Fatalf("permission card role = %v, want RoleAlertDialog", card.Props.Role)
	}
	if card.Props.LiveRegion != core.LiveRegionPolite {
		t.Fatalf("permission card liveRegion = %v, want LiveRegionPolite", card.Props.LiveRegion)
	}
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "toolbar-play" {
		t.Fatalf("permission card stole focus from toolbar-play: got %v", fv)
	}

	// Clicking header "Waiting for you" chip focuses the permission card.
	app.Press("id:agent-status-chip")
	app.Settle()
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "agent-perm-allow-once" {
		t.Fatalf("clicking Waiting chip focused %v, want agent-perm-allow-once", fv)
	}

	// Down / Up arrows cycle between permission card buttons.
	app.Key("down")
	app.Settle()
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "agent-perm-allow-always" {
		t.Fatalf("down arrow focused %v, want agent-perm-allow-always", fv)
	}
	app.Key("up")
	app.Settle()
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "agent-perm-allow-once" {
		t.Fatalf("up arrow focused %v, want agent-perm-allow-once", fv)
	}

	// Esc leaves the permission card unanswered and the agent keeps waiting (never denies or stops).
	app.Key("escape")
	app.Settle()
	if snap := ed.AgentSession(); snap.Status != editor.AgentStatusWaitingForYou || snap.PendingAsk == nil {
		t.Fatalf("Esc unexpectedly resolved permission card: status=%s pending=%v", snap.Status, snap.PendingAsk)
	}

	// Refocus card and use Mod+Enter to Allow once.
	app.Focus("id:agent-perm-allow-once")
	app.Settle()
	app.Key("mod+enter")
	app.Settle()
	select {
	case out := <-askCh:
		if out != editor.PermissionOutcomeAllowOnce {
			t.Fatalf("outcome = %q, want allow_once", out)
		}
	default:
		t.Fatal("Mod+Enter did not resolve permission ask")
	}

	// 2. When focus is ALREADY inside Agent panel (on a non-input control, not typing),
	// a new permission card takes focus and Mod+Backspace denies.
	app.Focus("id:agent-composer-stop")
	app.Settle()
	step2 := ed.BeginAgentToolStep("game_check", "Run game check", "", nil)
	evalCheck := ed.EvaluatePermission(editor.PermissionRequest{
		Tool:   "game_check",
		Action: editor.PermissionActionRun,
		Target: editor.PermissionRunGameCheck,
	})
	askCh2, _, _ := ed.BeginAgentPermissionAsk(editor.AgentPermissionPrompt{
		StepID:        step2,
		Title:         "Allow the agent to run game check?",
		ShortAction:   "game check",
		Why:           "It wants to check for broken references.",
		Effect:        "Nothing is saved.",
		RuleSource:    evalCheck.Reason,
		RulePreview:   editor.AlwaysRuleForEvaluation(evalCheck).Summary(),
		AlwaysLabel:   "Always allow game check",
		Tool:          "game_check",
		PermissionKey: "run:game check",
		Evaluation:    evalCheck,
	})
	app.Settle()
	if fv := focusedView(app); fv == nil || fv.Props.TestID != "agent-perm-allow-once" {
		t.Fatalf("when focus already in Agent panel, expected card to take focus, got %v", fv)
	}
	app.Key("mod+backspace")
	app.Settle()
	select {
	case out := <-askCh2:
		if out != editor.PermissionOutcomeDeny {
			t.Fatalf("outcome = %q, want deny", out)
		}
	default:
		t.Fatal("Mod+Backspace did not deny focused permission card")
	}

	// 3. Alt+Enter on a focused card chooses Always, persists permissions.yaml, and shows PermissionToast with Undo.
	app.Focus("id:agent-composer-stop")
	app.Settle()
	step3 := ed.BeginAgentToolStep("game_playtest", "Play 10 s headless", "", nil)
	askCh3, _, _ := ed.BeginAgentPermissionAsk(editor.AgentPermissionPrompt{
		StepID:        step3,
		Title:         "Allow the agent to run the game?",
		ShortAction:   "the playtest",
		Why:           "It wants to play level_01 headless.",
		Effect:        "Nothing is saved.",
		RuleSource:    evalPlaytest.Reason,
		RulePreview:   editor.AlwaysRuleForEvaluation(evalPlaytest).Summary(),
		AlwaysLabel:   "Always allow headless playtests",
		Tool:          "game_playtest",
		PermissionKey: "run:playtests",
		Evaluation:    evalPlaytest,
	})
	app.Settle()
	app.Key("alt+enter")
	app.Settle()
	select {
	case out := <-askCh3:
		if out != editor.PermissionOutcomeAllowAlways {
			t.Fatalf("outcome = %q, want allow_always", out)
		}
	default:
		t.Fatal("Alt+Enter did not approve Always on focused permission card")
	}
	if _, err := os.Stat(permPath); err != nil {
		t.Fatalf("Always did not write permissions.yaml: %v", err)
	}
	toast, ok := ed.PermissionToast()
	if !ok || !strings.Contains(toast.Message, "Always") {
		t.Fatalf("expected Always permission toast, got %+v (ok=%v)", toast, ok)
	}
}

func TestStopFromInsideModalDialogAndEscNeverStops(t *testing.T) {
	ed, err := editor.New(newFocusTestScene(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.SetRightRailTab(editor.RightTabAgent)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, focusDesktopEnv, nsgotest.Size(1440, 900))
	app.Settle()

	ed.StartAgentRequest("Add slimes and playtest.")
	s1 := ed.BeginAgentToolStep("scene_read", "Read level_01.scene.yaml", "", nil)
	ed.FinishAgentToolStep(s1, "Read level_01.scene.yaml", "", nil, false)
	if err := ed.MoveObject("hero", 300, 180); err != nil {
		t.Fatalf("MoveObject: %v", err)
	}
	s2 := ed.BeginAgentToolStep("scene_edit", "Moved Hero", "", []string{"hero"})
	ed.FinishAgentToolStep(s2, "Moved Hero", "", []string{"hero"}, false)
	s3 := ed.BeginAgentToolStep("game_playtest", "the playtest", "", nil)
	_, stopCh, _ := ed.BeginAgentPermissionAsk(editor.AgentPermissionPrompt{
		StepID:      s3,
		Title:       "Allow the agent to run the game?",
		ShortAction: "the playtest",
		Tool:        "game_playtest",
		Evaluation: editor.PermissionEvaluation{
			Action: editor.PermissionActionRun,
			Target: editor.PermissionRunPlaytests,
		},
	})
	app.Settle()

	// Esc outside a dialog MUST NOT stop the agent.
	app.Key("escape")
	app.Settle()
	if !ed.AgentWorking() {
		t.Fatal("Esc stopped the agent; Spec rule 02 says Esc never stops the agent")
	}

	// Open a modal dialog (Shortcuts sheet via Cmd+/) and verify Esc closes dialog without stopping agent.
	app.Key("meta+/")
	app.Settle()
	if !ed.ShortcutsSheetOpen() {
		t.Fatal("expected Shortcuts Sheet modal to open")
	}
	app.Key("escape")
	app.Advance(250 * time.Millisecond)
	app.Settle()
	if ed.ShortcutsSheetOpen() {
		t.Fatal("expected Esc to close Shortcuts Sheet")
	}
	if !ed.AgentWorking() {
		t.Fatal("Esc from modal stopped the agent; Esc must never stop the agent")
	}

	// Open modal dialog again and press Mod+. from INSIDE the dialog -> MUST stop the agent!
	app.Key("meta+/")
	app.Settle()
	if !ed.ShortcutsSheetOpen() {
		t.Fatal("expected Shortcuts Sheet modal to reopen")
	}
	app.Key("mod+.")
	app.Settle()

	select {
	case <-stopCh:
	default:
		t.Fatal("Mod+. from inside modal dialog did not close request stop channel")
	}
	snap := ed.AgentSession()
	if !snap.Stopped {
		t.Fatalf("expected Stopped=true after Mod+., got %+v", snap)
	}
	if snap.Review == nil || !snap.Review.Stopped || snap.Review.StepsDone != 2 {
		t.Fatalf("pending review after Stop = %+v, want Stopped=true StepsDone=2", snap.Review)
	}
	if !ed.HasPendingAgentTurnReview() || !ed.HasOpenReview() {
		t.Fatalf("expected HasPendingAgentTurnReview && HasOpenReview to pause autosave until kept/reverted")
	}
	toast, ok := ed.ActiveToast()
	if !ok || toast.Message != "Agent stopped" || toast.ActionLabel != "Review" {
		t.Fatalf("toast after Stop = %+v (ok=%v), want Agent stopped with Review", toast, ok)
	}
	// Completed edits stay in the scene pending for review.
	hero, _, _ := ed.FindObject("hero")
	if hero == nil || hero.X != 300 {
		t.Fatalf("completed edit lost after Stop: hero=%+v", hero)
	}

	// Keeping review clears HasOpenReview; Reverting restores the scene.
	ed.KeepPendingAgentTurnReview()
	if ed.HasPendingAgentTurnReview() {
		t.Fatal("HasPendingAgentTurnReview should be false after KeepPendingAgentTurnReview")
	}
	ed.RevertPendingAgentTurnReview()
	hero, _, _ = ed.FindObject("hero")
	if hero == nil || hero.X == 300 {
		t.Fatalf("RevertPendingAgentTurnReview did not undo agent edit: hero=%+v", hero)
	}
}
