package editor_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func float32Ptr(v float32) *float32 { return &v }

// sampleRoom2Plan builds the 5-step, 2-question Room 2 plan matching design/editor/Plan.dc.html.
func sampleRoom2Plan() editor.AgentPlan {
	return editor.AgentPlan{
		Prompt: "Build a second room behind the door: a lever that opens a gate, a few slimes and a coin reward.",
		Title:  "Room 2: a lever gate, slimes and a coin reward",
		Estimate: editor.PlanEstimate{
			Duration: "about 2 min",
			Tokens:   "~35k tokens",
		},
		Files: []editor.PlanFile{
			{Path: "scenes/level_01.scene.yaml", Tag: editor.PlanFileEdit},
			{Path: "prefabs/gate.prefab.yaml", Tag: editor.PlanFileNew},
			{Path: "logic/lever.go", Tag: editor.PlanFileEdit},
		},
		Questions: []editor.PlanQuestion{
			{
				ID:     "q1",
				Label:  "Gate",
				Prompt: "Where should the gate stand?",
				Options: []editor.PlanOption{
					{
						ID:        "door",
						Label:     "Right after the door",
						Note:      "Suggested",
						Suggested: true,
						Preview:   &editor.PlanOptionPreview{Kind: "room", GatePercent: 50, LeverPercent: 28},
						StepTitleOverrides: map[int]string{
							2: "Add a gate right after the door and a lever",
						},
						GhostOverrides: []editor.PlanGhostOverride{
							{Step: 2, GhostID: "gate", X: float32Ptr(432), LabelX: float32Ptr(402)},
							{Step: 2, GhostID: "lever", X: float32Ptr(330)},
						},
					},
					{
						ID:      "mid",
						Label:   "Mid-room",
						Note:    "Slimes guard the lever",
						Preview: &editor.PlanOptionPreview{Kind: "room", GatePercent: 70, LeverPercent: 60},
						StepTitleOverrides: map[int]string{
							2: "Add a gate mid-room and a lever",
						},
						GhostOverrides: []editor.PlanGhostOverride{
							{Step: 2, GhostID: "gate", X: float32Ptr(588), LabelX: float32Ptr(558)},
							{Step: 2, GhostID: "lever", X: float32Ptr(520)},
						},
					},
					{
						ID:      "end",
						Label:   "At the end",
						Note:    "Guards the coins",
						Preview: &editor.PlanOptionPreview{Kind: "room", GatePercent: 88, LeverPercent: 78},
						StepTitleOverrides: map[int]string{
							2: "Add a gate at the end of room 2 and a lever",
						},
						GhostOverrides: []editor.PlanGhostOverride{
							{Step: 2, GhostID: "gate", X: float32Ptr(714), LabelX: float32Ptr(684)},
							{Step: 2, GhostID: "lever", X: float32Ptr(640)},
						},
					},
				},
			},
			{
				ID:     "q2",
				Label:  "Lever",
				Prompt: "What does the lever do to the gate?",
				Options: []editor.PlanOption{
					{
						ID:        "open",
						Label:     "Stays open",
						Note:      "Suggested",
						Suggested: true,
						Preview: &editor.PlanOptionPreview{
							Kind: "strip",
							Segments: []editor.PlanStripSegment{
								{From: 22, To: 100, Active: true},
							},
						},
						StepTitleOverrides: map[int]string{
							4: "Pulling the lever stays open the gate",
						},
					},
					{
						ID:    "timed",
						Label: "Closes after 5 s",
						Note:  "A short race",
						Preview: &editor.PlanOptionPreview{
							Kind: "strip",
							Segments: []editor.PlanStripSegment{
								{From: 22, To: 60, Active: true},
							},
						},
						StepTitleOverrides: map[int]string{
							4: "Pulling the lever closes after 5 s the gate",
						},
					},
					{
						ID:    "toggle",
						Label: "Toggles each pull",
						Note:  "Slimes can be shut in",
						Preview: &editor.PlanOptionPreview{
							Kind: "strip",
							Segments: []editor.PlanStripSegment{
								{From: 22, To: 50, Active: true},
								{From: 74, To: 100, Active: true},
							},
						},
						StepTitleOverrides: map[int]string{
							4: "Pulling the lever toggles the gate",
						},
					},
				},
			},
		},
		Steps: []editor.PlanStep{
			{
				Number:     1,
				Title:      "Extend the World tiles 640 px past the door",
				Files:      []string{"scenes/level_01.scene.yaml"},
				FilesLabel: "level_01.scene.yaml",
				Adds:       "1 layer · 214 tiles",
				Ghosts: []editor.PlanGhost{
					{ID: "room2", X: 404, Y: 150, Width: 372, Height: 300, Label: "World +640 px", ActiveLabel: "extending World tiles", LabelX: float32Ptr(412), LabelY: float32Ptr(158)},
					{ID: "floor2", X: 404, Y: 390, Width: 372, Height: 60},
					{ID: "ledge2", X: 560, Y: 322, Width: 96, Height: 12},
				},
				Nodes: []editor.PlanTreeNode{
					{Name: "main", Existing: true},
				},
			},
			{
				Number:     2,
				Title:      "Add a gate right after the door and a lever",
				Files:      []string{"prefabs/gate.prefab.yaml", "scenes/level_01.scene.yaml"},
				FilesLabel: "gate.prefab.yaml · level_01",
				Adds:       "1 new prefab · 2 objects",
				Objects:    2,
				Ghosts: []editor.PlanGhost{
					{ID: "gate", X: 432, Y: 330, Width: 14, Height: 60, Label: "gate · lever", LabelX: float32Ptr(402), LabelY: float32Ptr(300), ObjectID: "hero"},
					{ID: "lever", X: 330, Y: 372, Width: 10, Height: 18},
				},
				Nodes: []editor.PlanTreeNode{
					{Name: "gate", Layer: "main"},
					{Name: "lever", Layer: "main"},
				},
			},
			{
				Number:          3,
				Title:           "Place {slimes} slimes and {coins} coins in room 2",
				Files:           []string{"scenes/level_01.scene.yaml"},
				FilesLabel:      "level_01.scene.yaml",
				NotePlaceholder: "Or describe it, e.g. one slime on the ledge",
				Fields: []editor.PlanStepField{
					{Key: "slimes", Label: "Slimes", Value: 3, Min: 1, Max: 6, Step: 1, Hint: "1 to 6", CountsObjects: true},
					{Key: "coins", Label: "Coins", Value: 5, Min: 0, Max: 9, Step: 1, Hint: "0 to 9", CountsObjects: true},
				},
				Ghosts: []editor.PlanGhost{
					{ID: "slimes", X: 452, Y: 372, Width: 26, Height: 16, RepeatField: "slimes", SpanWidth: 308, Label: "{slimes} slimes · {coins} coins", LabelX: float32Ptr(640), LabelY: float32Ptr(416)},
					{ID: "coins", X: 566, Y: 300, Width: 10, Height: 10, RepeatField: "coins", StepX: 18, StaggerY: -8},
				},
				Nodes: []editor.PlanTreeNode{
					{Name: "slime_03 … slime_{slimes+2:02d}", Layer: "main"},
					{Name: "coins_room2 · {coins}", Layer: "main"},
				},
			},
			{
				Number:           4,
				Title:            "Pulling the lever opens the gate",
				Files:            []string{"logic/lever.go"},
				FilesLabel:       "logic/lever.go",
				Adds:             "1 new function",
				Permission:       "Edit Go",
				PermissionAction: editor.PermissionActionCode,
				PermissionTarget: "logic/lever.go",
			},
			{
				Number:           5,
				Title:            "Run check and a 15 s headless playtest",
				FilesLabel:       "no files",
				Adds:             "reports back",
				Permission:       "Run the game",
				PermissionAction: editor.PermissionActionRun,
				PermissionTarget: editor.PermissionRunPlaytests,
			},
		},
	}
}

// #1035 Acceptance: Ghosts follow an answer change (the gate moves) and hide with the toggle;
// nothing is written before approval.
func TestPlanGhostsFollowAnswerChangeAndToggleNothingWrittenBeforeApproval(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	scenePath := filepath.Join(dir, "scenes", "level_01.scene.yaml")
	initialScene := `version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: main
    space: world
    objects:
      - id: hero
        name: Knight
        x: 76
        y: 356
`
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: dungeon-run\nid: com.example.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenePath, []byte(initialScene), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	beforeYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := ed.ProposeAgentPlan(sampleRoom2Plan()); err != nil {
		t.Fatalf("ProposeAgentPlan: %v", err)
	}

	findGateX := func() (float32, bool) {
		for _, g := range ed.ResolvedPlanGhosts() {
			if g.ID == "gate" {
				return g.X, true
			}
		}
		return 0, false
	}

	// Default suggested gate position is x=432.
	x0, ok := findGateX()
	if !ok || x0 != 432 {
		t.Fatalf("initial gate ghost x = %v (ok=%v), want 432", x0, ok)
	}

	// Answer q1 with "mid" -> gate moves to 588.
	if err := ed.AnswerPlanQuestion("q1", "mid"); err != nil {
		t.Fatalf("AnswerPlanQuestion(q1, mid): %v", err)
	}
	x1, ok := findGateX()
	if !ok || x1 != 588 {
		t.Fatalf("gate ghost x after 'mid' = %v (ok=%v), want 588", x1, ok)
	}

	// Reopen and switch to "end" -> gate moves to 714.
	if err := ed.ReopenPlanQuestion("q1"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AnswerPlanQuestion("q1", "end"); err != nil {
		t.Fatal(err)
	}
	x2, ok := findGateX()
	if !ok || x2 != 714 {
		t.Fatalf("gate ghost x after 'end' = %v (ok=%v), want 714", x2, ok)
	}

	// Changing slimes count from 3 to 5 updates slime ghost count in viewport.
	countSlimeGhosts := func() int {
		n := 0
		for _, g := range ed.ResolvedPlanGhosts() {
			if g.ID == "slimes" {
				n++
			}
		}
		return n
	}
	if got := countSlimeGhosts(); got != 3 {
		t.Fatalf("initial slime ghosts = %d, want 3", got)
	}
	if err := ed.SetPlanStepField(3, "slimes", 5); err != nil {
		t.Fatal(err)
	}
	if got := countSlimeGhosts(); got != 5 {
		t.Fatalf("slime ghosts after edit = %d, want 5", got)
	}

	// Toggle Plan ghosts off -> ResolvedPlanGhosts is empty; toggle back on -> restored.
	ed.TogglePlanGhosts()
	if ed.PlanGhostsVisible() {
		t.Fatal("PlanGhostsVisible should be false after toggle")
	}
	if got := len(ed.ResolvedPlanGhosts()); got != 0 {
		t.Fatalf("ResolvedPlanGhosts when toggled off = %d, want 0", got)
	}
	ed.TogglePlanGhosts()
	if !ed.PlanGhostsVisible() || len(ed.ResolvedPlanGhosts()) == 0 {
		t.Fatal("PlanGhostsVisible should restore ghosts on second toggle")
	}

	// Verify nothing was written to disk or the open scene before approval.
	diskBytes, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(diskBytes) != initialScene {
		t.Fatalf("scene file on disk changed before approval")
	}
	afterYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeYAML, afterYAML) {
		t.Fatalf("open scene YAML changed before approval")
	}
	if _, err := os.Stat(filepath.Join(dir, "prefabs", "gate.prefab.yaml")); !os.IsNotExist(err) {
		t.Fatalf("prefabs/gate.prefab.yaml should not exist before approval")
	}
}

// #1035 Acceptance: Stop before step 1 leaves every file unchanged and offers Edit plan and Run again.
// Also verifies Just do it ("Picked for you: ..."), manual edit pause on planned objects while running,
// permission reason evaluation, and step removal/restore striking out exclusive files.
func TestPlanJustDoItStopBeforeStep1EditAndRunAgain(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	scenePath := filepath.Join(dir, "scenes", "level_01.scene.yaml")
	initialScene := `version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: main
    space: world
    objects:
      - id: hero
        name: Knight
        x: 76
        y: 356
`
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: dungeon-run\nid: com.example.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenePath, []byte(initialScene), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ed.ProposeAgentPlan(sampleRoom2Plan()); err != nil {
		t.Fatal(err)
	}

	// Permission reason matches Balanced profile editing Go + 3 files.
	permSum := ed.EvaluatePlanPermissions(ed.ActivePlan())
	if !permSum.Needed || !permSum.EditsGo || permSum.FileCount != 3 {
		t.Fatalf("EvaluatePlanPermissions = %+v, want Needed=true EditsGo=true FileCount=3", permSum)
	}
	if !strings.Contains(permSum.Reason, "edits Go") || !strings.Contains(permSum.Reason, "3 files") {
		t.Fatalf("permission reason = %q, want edits Go and 3 files", permSum.Reason)
	}

	// Removing step 4 strikes logic/lever.go out of FILES IN THIS PLAN; restoring brings it back.
	if err := ed.RemovePlanStep(4); err != nil {
		t.Fatal(err)
	}
	leverInPlan := func() bool {
		for _, f := range ed.ResolvedPlanFiles() {
			if f.Path == "logic/lever.go" {
				return f.InPlan
			}
		}
		return false
	}
	if leverInPlan() {
		t.Fatal("logic/lever.go should be struck out when step 4 is removed")
	}
	if err := ed.RestorePlanStep(4); err != nil {
		t.Fatal(err)
	}
	if !leverInPlan() {
		t.Fatal("logic/lever.go should be active again after restoring step 4")
	}

	// Just do it picks suggested options and starts Running at step 1 of 5.
	dec, err := ed.JustDoItAgentPlan()
	if err != nil {
		t.Fatalf("JustDoItAgentPlan: %v", err)
	}
	if dec.Decision != "approved" || !dec.PickedForYou {
		t.Fatalf("JustDoIt decision = %+v, want approved + PickedForYou", dec)
	}
	if !strings.HasPrefix(ed.PlanPickedLine(), "Picked for you:") {
		t.Fatalf("PlanPickedLine = %q, want prefix 'Picked for you:'", ed.PlanPickedLine())
	}
	if pill := ed.PlanHeaderPillText(); !strings.Contains(pill, "step 1 of 5") || !strings.Contains(pill, "manual edits to planned objects are paused") {
		t.Fatalf("running header pill = %q", pill)
	}

	// While running, manual edits to planned object "hero" are paused, whereas agent edits are allowed.
	if !ed.IsObjectPausedByPlan("hero") {
		t.Fatal("IsObjectPausedByPlan(hero) = false while plan running")
	}
	if err := ed.MoveObject("hero", 99, 99); err == nil {
		t.Fatal("manual MoveObject on planned object should be paused while running")
	}

	// Active step (step 1) ghosts turn solid (Active == true); later step ghosts stay dashed (Active == false).
	sawActive1, sawInactive2 := false, false
	for _, g := range ed.ResolvedPlanGhosts() {
		if g.Step == 1 && g.Active {
			sawActive1 = true
		}
		if g.Step == 2 && !g.Active {
			sawInactive2 = true
		}
	}
	if !sawActive1 || !sawInactive2 {
		t.Fatalf("ghost active flags: step1=%v step2Inactive=%v", sawActive1, sawInactive2)
	}

	// Stop before step 1 completes -> NothingWritten, every file unchanged, Edit plan & Run again available.
	stopDec, err := ed.StopAgentPlan()
	if err != nil {
		t.Fatalf("StopAgentPlan: %v", err)
	}
	if stopDec.Decision != "stopped" || !stopDec.NothingWritten {
		t.Fatalf("stopDec = %+v, want stopped + NothingWritten", stopDec)
	}
	if cur := ed.ActivePlan(); cur == nil || cur.Phase != editor.PlanPhaseStopped || !cur.NothingWritten {
		t.Fatalf("active plan after stop = %+v", cur)
	}
	diskBytes, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(diskBytes) != initialScene {
		t.Fatal("scene file on disk changed after Stop before step 1")
	}

	// Edit plan returns to draft; Run again restarts Running.
	if err := ed.EditStoppedPlan(); err != nil {
		t.Fatalf("EditStoppedPlan: %v", err)
	}
	if cur := ed.ActivePlan(); cur == nil || cur.Phase != editor.PlanPhaseDraft {
		t.Fatalf("expected Draft after EditStoppedPlan, got %+v", cur)
	}
	if _, err := ed.ApproveAgentPlan(); err != nil {
		t.Fatalf("Approve after EditStoppedPlan: %v", err)
	}
	if _, err := ed.StopAgentPlan(); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.RunStoppedPlanAgain(); err != nil {
		t.Fatalf("RunStoppedPlanAgain: %v", err)
	}
	if !ed.IsPlanRunning() {
		t.Fatal("expected Running after RunStoppedPlanAgain")
	}
}

// #1035 UI & Screenshots: renders the full EditorView with the Plan card, clarifying questions,
// inline step editor, planned ghosts in viewport, planned nodes + FILES IN THIS PLAN in Scene Tree,
// Running state, Stopped state, and Permissions link in Dark and Light appearances.
func TestAgentPlanUIAndScreenshots(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetAppearance(editor.AppearanceDark)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	app.Post(func() {
		if _, _, err := ed.ProposeAgentPlan(sampleRoom2Plan()); err != nil {
			t.Errorf("ProposeAgentPlan: %v", err)
		}
	})
	app.Settle()

	if !app.Exists("id:agent-plan-card") {
		t.Fatal("agent-plan-card did not render")
	}
	if !app.Exists("id:plan-header-pill") {
		t.Fatal("plan-header-pill did not render")
	}
	if !app.Exists("id:status-plan-summary") {
		t.Fatal("status-plan-summary did not render")
	}
	if !app.Exists("id:plan-files-panel") {
		t.Fatal("plan-files-panel did not render")
	}
	if !app.Exists("id:viewport-plan-ghosts-toggle") {
		t.Fatal("viewport-plan-ghosts-toggle did not render")
	}
	if !app.Exists("id:plan-viewport-legend") {
		t.Fatal("plan-viewport-legend did not render")
	}
	if app.Exists("id:viewport-overlay-legend") {
		overlayBounds := app.Find("id:viewport-overlay-legend").Bounds()
		planLegendBounds := app.Find("id:plan-viewport-legend").Bounds()
		if planLegendBounds.Origin.Y < overlayBounds.Origin.Y+overlayBounds.Size.Height {
			t.Fatalf("plan-viewport-legend (%+v) overlaps viewport-overlay-legend (%+v)", planLegendBounds, overlayBounds)
		}
	}

	// 1. Draft state (2 unanswered questions, ghosts visible, Approve disabled).
	app.Screenshot("plan-draft-dark")

	// Answer Q1 (Mid-room) via UI press -> collapses Q1 with Change button.
	app.Press("id:plan-q-q1-opt-mid")
	if !app.Exists("id:plan-q-q1-summary") || !app.Exists("id:plan-q-q1-change") {
		t.Fatal("Q1 did not collapse to summary with Change button")
	}

	// Expand step 3 editor, bump slimes, type a note without losing input focus/mount.
	app.Press("id:plan-step-edit-3")
	if !app.Exists("id:plan-step-editor") {
		t.Fatal("plan-step-editor did not open")
	}
	app.Press("id:plan-field-slimes-inc")
	app.Type("id:plan-step-note-input", "one slime on the ledge")
	app.Screenshot("plan-step-edit-dark")
	app.Press("id:plan-step-edit-done")

	// Verify note persisted on step 3.
	if pl := ed.ActivePlan(); pl == nil || pl.Steps[2].Note != "one slime on the ledge" {
		t.Fatalf("step 3 note = %+v", pl)
	}

	// Answer Q2 and approve via UI button -> Running state with solid step 1 ghost + Stop button.
	app.Press("id:plan-q-q2-opt-open")
	app.Press("id:plan-approve")
	if !ed.IsPlanRunning() {
		t.Fatal("Approve button did not transition plan to Running")
	}
	if !app.Exists("id:plan-stop") || !app.Exists("id:plan-legend-working") {
		t.Fatal("Running UI (Stop button + working legend) did not render")
	}
	app.Screenshot("plan-running-dark")

	// Press Stop before step 1 completes -> Stopped state with Edit plan and Run again.
	app.Press("id:plan-stop")
	if !app.Exists("id:plan-stopped-line") || !app.Exists("id:plan-edit-again") || !app.Exists("id:plan-run-again") {
		t.Fatal("Stopped UI (Edit plan + Run again) did not render")
	}
	app.Screenshot("plan-stopped-dark")

	// Switch to Spec Light and return to Draft to verify Light palette parity and Permissions link.
	app.Press("id:plan-edit-again")
	app.Post(func() {
		ed.SetAppearance(editor.AppearanceLight)
	})
	app.Settle()
	app.Screenshot("plan-draft-light")

	// Clicking Permissions footer link opens Settings › Permissions.
	app.Press("id:plan-permissions-link")
	if !ed.SettingsOpen() || ed.SettingsPage() != editor.SettingsPagePermissions {
		t.Fatalf("SettingsOpen=%v Page=%q, want SettingsPagePermissions", ed.SettingsOpen(), ed.SettingsPage())
	}
}

// Hardening tests: validation bounds, path traversal & You-only refusal, phase guards,
// idempotent WrittenSteps, HasOpenReview autosave pause, manual-edit pause across inspector/hierarchy,
// HistoryPreviewActive hiding ghosts, and concurrent read/update race safety.
func TestPlanHardeningValidationPhasePauseAndConcurrency(t *testing.T) {
	ed := newTestEditor(t)

	// 1. Refuse path traversal, absolute paths, .git metadata, You-only files, non-finite geometry, and oversized step counts.
	badCases := []struct {
		name string
		plan editor.AgentPlan
	}{
		{
			name: "path traversal in step",
			plan: editor.AgentPlan{Title: "Bad", Steps: []editor.PlanStep{{Title: "S1", Files: []string{"../outside.yaml"}}}},
		},
		{
			name: "absolute path in files",
			plan: editor.AgentPlan{Title: "Bad", Files: []editor.PlanFile{{Path: "/etc/passwd"}}, Steps: []editor.PlanStep{{Title: "S1"}}},
		},
		{
			name: "git metadata",
			plan: editor.AgentPlan{Title: "Bad", Steps: []editor.PlanStep{{Title: "S1", Files: []string{".git/config"}}}},
		},
		{
			name: "you-only permissions.yaml",
			plan: editor.AgentPlan{Title: "Bad", Steps: []editor.PlanStep{{Title: "S1", Files: []string{"permissions.yaml"}}}},
		},
		{
			name: "you-only tools.yaml",
			plan: editor.AgentPlan{Title: "Bad", Files: []editor.PlanFile{{Path: ".nsgo/tools.yaml"}}, Steps: []editor.PlanStep{{Title: "S1"}}},
		},
		{
			name: "non-finite ghost x",
			plan: editor.AgentPlan{Title: "Bad", Steps: []editor.PlanStep{{Title: "S1", Ghosts: []editor.PlanGhost{{X: float32(math.NaN()), Y: 0, Width: 10, Height: 10}}}}},
		},
		{
			name: "negative ghost width",
			plan: editor.AgentPlan{Title: "Bad", Steps: []editor.PlanStep{{Title: "S1", Ghosts: []editor.PlanGhost{{X: 0, Y: 0, Width: -5, Height: 10}}}}},
		},
		{
			name: "too many steps",
			plan: func() editor.AgentPlan {
				steps := make([]editor.PlanStep, 65)
				for i := range steps {
					steps[i] = editor.PlanStep{Title: "Step"}
				}
				return editor.AgentPlan{Title: "TooMany", Steps: steps}
			}(),
		},
	}
	for _, tc := range badCases {
		if _, _, err := ed.ProposeAgentPlan(tc.plan); err == nil {
			t.Errorf("%s: ProposeAgentPlan succeeded; want validation error", tc.name)
		}
	}

	// 2. Stage valid plan; verify phase guards before approval and PlannedNewTreeNodeCount excludes Existing nodes.
	pl := sampleRoom2Plan()
	if _, _, err := ed.ProposeAgentPlan(pl); err != nil {
		t.Fatalf("ProposeAgentPlan: %v", err)
	}
	if got := ed.PlannedNewTreeNodeCount(); got != 4 {
		t.Fatalf("PlannedNewTreeNodeCount = %d, want 4 (excluding Existing layer)", got)
	}
	if ed.HasOpenReview() {
		t.Fatal("HasOpenReview should be false while plan is only in Draft")
	}
	if err := ed.UpdateAgentPlanStep(1, editor.PlanStepDone); err == nil {
		t.Fatal("UpdateAgentPlanStep should refuse while plan is still in Draft")
	}
	if err := ed.CompleteAgentPlan(); err == nil {
		t.Fatal("CompleteAgentPlan should refuse while plan is still in Draft")
	}

	// Remove step 4, approve via Just do it.
	if err := ed.RemovePlanStep(4); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.JustDoItAgentPlan(); err != nil {
		t.Fatal(err)
	}
	if !ed.HasOpenReview() {
		t.Fatal("HasOpenReview must be true while plan is Running to pause autosave")
	}

	// 3. Manual edits across inspector/hierarchy on planned object "hero" are paused, while agent edits succeed.
	if err := ed.SetObjectName("hero", "Renamed"); err == nil {
		t.Fatal("manual SetObjectName on planned object should be paused while running")
	}
	if err := ed.SetProperty("hero", "hp", 10); err == nil {
		t.Fatal("manual SetProperty on planned object should be paused while running")
	}
	if err := ed.SetObjectAlpha("hero", 0.5); err == nil {
		t.Fatal("manual SetObjectAlpha on planned object should be paused while running")
	}
	if err := ed.WithAgentActor(func() error { return ed.SetObjectName("hero", "Knight2") }); err != nil {
		t.Fatalf("agent SetObjectName on planned object failed: %v", err)
	}

	// 4. Removed step cannot be marked done; duplicate done does not double-count WrittenSteps.
	if err := ed.UpdateAgentPlanStep(4, editor.PlanStepDone); err == nil {
		t.Fatal("UpdateAgentPlanStep should refuse marking a removed step done")
	}
	if err := ed.UpdateAgentPlanStep(1, editor.PlanStepDone); err != nil {
		t.Fatal(err)
	}
	if err := ed.UpdateAgentPlanStep(1, editor.PlanStepDone); err != nil {
		t.Fatal(err)
	}
	if cur := ed.ActivePlan(); cur == nil || cur.WrittenSteps != 1 {
		t.Fatalf("WrittenSteps after duplicate step 1 done = %+v, want 1", cur)
	}

	// 5. Concurrent readers + step updates under -race must be race-free.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = ed.ActivePlan()
				_ = ed.ResolvedPlanGhosts()
				_ = ed.ResolvedPlanFiles()
				_ = ed.ResolvedPlanTreeNodes()
				_ = ed.PlanSummaryLine()
				_ = ed.PlanHeaderPillText()
				_ = ed.IsObjectPausedByPlan("hero")
			}
		}()
	}
	_ = ed.UpdateAgentPlanStep(2, editor.PlanStepRunning)
	_ = ed.UpdateAgentPlanStep(2, editor.PlanStepDone)
	wg.Wait()

	if err := ed.CompleteAgentPlan(); err != nil {
		t.Fatalf("CompleteAgentPlan: %v", err)
	}
	if ed.HasOpenReview() {
		t.Fatal("HasOpenReview should be false after CompleteAgentPlan")
	}
}

func TestAgentRewindClearsActivePlanOnDroppedTurn(t *testing.T) {
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Turn 1: propose and complete a plan.
	p1 := sampleRoom2Plan()
	p1.Prompt = "First room plan"
	if _, _, err := ed.ProposeAgentPlan(p1); err != nil {
		t.Fatalf("ProposeAgentPlan 1: %v", err)
	}
	if _, err := ed.JustDoItAgentPlan(); err != nil {
		t.Fatalf("JustDoItAgentPlan 1: %v", err)
	}
	// While plan 1 is running, rewind popover and RewindAgentTurn must be refused.
	turns := ed.AgentTurns()
	if len(turns) != 1 {
		t.Fatalf("len(turns) = %d, want 1", len(turns))
	}
	if ed.OpenAgentRewindPopover(turns[0].ID) {
		t.Fatal("OpenAgentRewindPopover should be refused while a plan is running")
	}
	if err := ed.RewindAgentTurn(turns[0].ID, editor.AgentRewindBoth); err == nil {
		t.Fatal("RewindAgentTurn should be refused while a plan is running")
	}
	if err := ed.CompleteAgentPlan(); err != nil {
		t.Fatalf("CompleteAgentPlan 1: %v", err)
	}

	// Turn 2: propose a second plan, approve it, and stop before step 1 so ActivePlan stays in PlanPhaseStopped.
	p2 := sampleRoom2Plan()
	p2.Prompt = "Second room plan"
	if _, _, err := ed.ProposeAgentPlan(p2); err != nil {
		t.Fatalf("ProposeAgentPlan 2: %v", err)
	}
	if _, err := ed.JustDoItAgentPlan(); err != nil {
		t.Fatalf("JustDoItAgentPlan 2: %v", err)
	}
	if _, err := ed.StopAgentPlan(); err != nil {
		t.Fatalf("StopAgentPlan 2: %v", err)
	}
	if ed.ActivePlan() == nil || ed.ActivePlan().Phase != editor.PlanPhaseStopped {
		t.Fatalf("expected stopped active plan on turn 2, got %+v", ed.ActivePlan())
	}
	turns = ed.AgentTurns()
	if len(turns) != 2 {
		t.Fatalf("len(turns) = %d, want 2", len(turns))
	}
	turn2ID := turns[1].ID

	// Rewinding turn 2 drops it and must clear the stale stopped ActivePlan and its ghosts.
	if err := ed.RewindAgentTurn(turn2ID, editor.AgentRewindBoth); err != nil {
		t.Fatalf("RewindAgentTurn(turn2): %v", err)
	}
	if ed.ActivePlan() != nil {
		t.Fatalf("expected ActivePlan nil after rewinding turn 2, got %+v", ed.ActivePlan())
	}
	if ghosts := ed.ResolvedPlanGhosts(); len(ghosts) != 0 {
		t.Fatalf("expected 0 plan ghosts after rewinding turn 2, got %d", len(ghosts))
	}
	// Turn 1's completed plan review stays intact.
	keptTurns := ed.AgentTurns()
	if len(keptTurns) != 1 || keptTurns[0].Plan == nil {
		t.Fatalf("expected turn 1 with completed Plan review intact, got %+v", keptTurns)
	}

	// Redo restores turn 2 with its deep-cloned Plan review.
	if err := ed.RedoAgentRewind(); err != nil {
		t.Fatalf("RedoAgentRewind: %v", err)
	}
	restored := ed.AgentTurns()
	if len(restored) != 2 || restored[1].Plan == nil {
		t.Fatalf("expected turn 2 Plan review restored on Redo, got %+v", restored)
	}
	// Mutating the cloned snapshot returned by AgentTurns must not affect editor state.
	restored[1].Plan.Title = "mutated"
	if ed.AgentTurns()[1].Plan.Title == "mutated" {
		t.Fatal("AgentTurns did not deep-clone AgentTurn.Plan")
	}
}

func TestPlanShortcutsGuardedWhenRewindPopoverOpen(t *testing.T) {
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create turn 1 with a completed plan so there is a turn to open the rewind popover on.
	p1 := sampleRoom2Plan()
	p1.Prompt = "Turn one"
	if _, _, err := ed.ProposeAgentPlan(p1); err != nil {
		t.Fatalf("ProposeAgentPlan 1: %v", err)
	}
	if _, err := ed.JustDoItAgentPlan(); err != nil {
		t.Fatalf("JustDoItAgentPlan 1: %v", err)
	}
	if err := ed.CompleteAgentPlan(); err != nil {
		t.Fatalf("CompleteAgentPlan 1: %v", err)
	}
	turn1ID := ed.AgentTurns()[0].ID

	// Propose a second plan in Draft phase with all questions answered (so Approve would succeed if unguarded),
	// then restore idle status to simulate opening the rewind popover while a draft plan is visible.
	p2 := sampleRoom2Plan()
	p2.Prompt = "Turn two draft"
	if _, _, err := ed.ProposeAgentPlan(p2); err != nil {
		t.Fatalf("ProposeAgentPlan 2: %v", err)
	}
	if err := ed.AnswerPlanQuestion("q1", "door"); err != nil {
		t.Fatalf("AnswerPlanQuestion q1: %v", err)
	}
	if err := ed.AnswerPlanQuestion("q2", "open"); err != nil {
		t.Fatalf("AnswerPlanQuestion q2: %v", err)
	}
	// Stop and EditStoppedPlan -> Draft, verify CanApprovePlan is true.
	if ok, reason := ed.CanApprovePlan(); !ok {
		t.Fatalf("expected draft plan approvable, got reason %q", reason)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))
	app.Settle()

	// While a draft plan is in AgentStatusWaitingForYou, OpenAgentRewindPopover is blocked by AgentWorking;
	// verify that ProposeAgentPlan also clears any previously open rewind popover.
	if ed.OpenAgentRewindPopover(turn1ID) {
		t.Fatal("expected OpenAgentRewindPopover refused while AgentStatusWaitingForYou")
	}
	if _, err := ed.StopAgentPlan(); err != nil {
		t.Fatalf("StopAgentPlan: %v", err)
	}
	if !ed.OpenAgentRewindPopover(turn1ID) {
		t.Fatal("expected OpenAgentRewindPopover to succeed after plan stopped")
	}
	app.Settle()

	// Editing the stopped plan back to Draft must clear the open rewind popover.
	if err := ed.EditStoppedPlan(); err != nil {
		t.Fatalf("EditStoppedPlan: %v", err)
	}
	if ed.AgentRewindOpenTurnID() != "" {
		t.Fatalf("expected EditStoppedPlan to clear rewind popover, still %q", ed.AgentRewindOpenTurnID())
	}
	// And pressing mod+enter when no rewind popover is open approves the draft plan.
	app.Settle()
	app.Key("mod+enter")
	app.Settle()
	if !ed.IsPlanRunning() {
		t.Fatalf("expected mod+enter to approve draft plan when rewind popover is closed, got %+v", ed.ActivePlan())
	}
}
