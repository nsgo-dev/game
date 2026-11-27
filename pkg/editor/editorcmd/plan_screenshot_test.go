package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func f32Ptr(v float32) *float32 { return &v }

// TestAgentPlanMacOSScreenshots renders the Plan card, clarifying questions, inline step editor,
// planned agent-violet viewport ghosts, Scene Tree planned nodes + FILES IN THIS PLAN, Running state,
// and Stopped state on macOS in both Dark and Light Spec appearances beside docs/design/editor/Plan.dc.html.
func TestAgentPlanMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: dungeon-run
id: com.example.dungeonrun
appVersion: 0.1.0
window:
  title: Dungeon Run
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sceneYAML := []byte(`version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#181C24"
layers:
  - name: World
    space: world
  - name: Actors
    space: world
    objects:
      - id: knight
        name: Knight
        x: 76
        y: 256
      - id: door
        name: Door
        x: 370
        y: 250
      - id: slime_01
        name: slime_01
        x: 150
        y: 274
      - id: slime_02
        name: slime_02
        x: 252
        y: 206
`)
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), sceneYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if err := ed.SetLanguage(editor.LanguageEnglish); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAppearance(editor.AppearanceDark); err != nil {
		t.Fatal(err)
	}

	plan := editor.AgentPlan{
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
							{Step: 2, GhostID: "gate", X: f32Ptr(432), LabelX: f32Ptr(402)},
							{Step: 2, GhostID: "lever", X: f32Ptr(330)},
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
							{Step: 2, GhostID: "gate", X: f32Ptr(588), LabelX: f32Ptr(558)},
							{Step: 2, GhostID: "lever", X: f32Ptr(520)},
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
							{Step: 2, GhostID: "gate", X: f32Ptr(714), LabelX: f32Ptr(684)},
							{Step: 2, GhostID: "lever", X: f32Ptr(640)},
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
							Kind:     "strip",
							Segments: []editor.PlanStripSegment{{From: 22, To: 100, Active: true}},
						},
					},
					{
						ID:    "timed",
						Label: "Closes after 5 s",
						Note:  "A short race",
						Preview: &editor.PlanOptionPreview{
							Kind:     "strip",
							Segments: []editor.PlanStripSegment{{From: 22, To: 60, Active: true}},
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
					{ID: "room2", X: 404, Y: 50, Width: 372, Height: 260, Label: "World +640 px", ActiveLabel: "extending World tiles", LabelX: f32Ptr(412), LabelY: f32Ptr(58)},
					{ID: "floor2", X: 404, Y: 250, Width: 372, Height: 60},
					{ID: "ledge2", X: 560, Y: 182, Width: 96, Height: 12},
				},
				Nodes: []editor.PlanTreeNode{{Name: "World", Existing: true}},
			},
			{
				Number:     2,
				Title:      "Add a gate right after the door and a lever",
				Files:      []string{"prefabs/gate.prefab.yaml", "scenes/level_01.scene.yaml"},
				FilesLabel: "gate.prefab.yaml · level_01",
				Adds:       "1 new prefab · 2 objects",
				Objects:    2,
				Ghosts: []editor.PlanGhost{
					{ID: "gate", X: 432, Y: 190, Width: 14, Height: 60, Label: "gate · lever", LabelX: f32Ptr(402), LabelY: f32Ptr(160)},
					{ID: "lever", X: 330, Y: 232, Width: 10, Height: 18},
				},
				Nodes: []editor.PlanTreeNode{
					{Name: "gate", Layer: "Actors"},
					{Name: "lever", Layer: "Actors"},
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
					{ID: "slimes", X: 452, Y: 232, Width: 26, Height: 16, RepeatField: "slimes", SpanWidth: 308, Label: "{slimes} slimes · {coins} coins", LabelX: f32Ptr(580), LabelY: f32Ptr(268)},
					{ID: "coins", X: 566, Y: 160, Width: 10, Height: 10, RepeatField: "coins", StepX: 18, StaggerY: -8},
				},
				Nodes: []editor.PlanTreeNode{
					{Name: "slime_03 … slime_{slimes+2:02d}", Layer: "Actors"},
					{Name: "coins_room2 · {coins}", Layer: "Actors"},
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
			},
			{
				Number:           5,
				Title:            "Run check and a 15 s headless playtest",
				FilesLabel:       "no files",
				Adds:             "reports back",
				Permission:       "Run the game",
				PermissionAction: editor.PermissionActionRun,
			},
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Post(func() {
		ed.NotifyAgentMCPConnected(editor.AgentClientInfo{ID: "c1", Name: "claude-code", Transport: "stdio"})
		if _, _, err := ed.ProposeAgentPlan(plan); err != nil {
			t.Errorf("ProposeAgentPlan: %v", err)
		}
		ed.FrameAll2D()
	})
	app.Settle()

	// 1. Draft dark state (questions open, ghosts visible, Approve disabled).
	app.Screenshot("plan-draft-dark")

	// 2. Answer Q1 (Mid-room) so gate ghost moves + open step 3 inline editor.
	app.Press("id:plan-q-q1-opt-mid")
	app.Press("id:plan-step-edit-3")
	app.Press("id:plan-field-slimes-inc")
	app.Screenshot("plan-step-edit-dark")
	app.Press("id:plan-step-edit-done")

	// 3. Answer Q2 + Approve -> Running state (solid step 1 ghost + header pill + Stop).
	app.Press("id:plan-q-q2-opt-open")
	app.Press("id:plan-approve")
	app.Screenshot("plan-running-dark")

	// 4. Stop before step 1 -> Nothing was written + Edit plan & Run again.
	app.Press("id:plan-stop")
	app.Screenshot("plan-stopped-dark")

	// 5. Light appearance parity in Draft state.
	app.Press("id:plan-edit-again")
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
	})
	app.Settle()
	app.Screenshot("plan-draft-light")
}
