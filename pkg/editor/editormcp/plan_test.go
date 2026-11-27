package editormcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1035 Acceptance: An agent proposes a plan through MCP (plan_propose) with two questions and five steps,
// the person answers one, checks that Approve is refused, answers the other, removes a step and changes a count,
// approves, and checks the decision the tool returns; then step updates complete the plan into the turn's review.
func TestPlanProposeQuestionsEditRemoveApproveAndTurnReviewOverMCP(t *testing.T) {
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
      - id: knight
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
	ed.WaitForProblems()

	var s *editormcp.Server
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = editormcp.New(ed, editormcp.Options{Run: cx.Post})
		cx.Cleanup(func() { _ = s.Close() })
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	cs := connect(t, s, nil)

	// Start plan_propose asynchronously on a goroutine; it blocks until the person approves or stops.
	type callOutcome struct {
		res *mcp.CallToolResult
		err error
	}
	outcomeCh := make(chan callOutcome, 1)
	go func() {
		res, callErr := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name: editormcp.ToolPlanPropose,
			Arguments: map[string]any{
				"prompt": "Build a second room behind the door: a lever that opens a gate, a few slimes and a coin reward.",
				"title":  "Room 2: a lever gate, slimes and a coin reward",
				"estimate": map[string]any{
					"duration": "about 2 min",
					"tokens":   "~35k tokens",
				},
				"files": []map[string]any{
					{"path": "scenes/level_01.scene.yaml", "tag": "edit"},
					{"path": "prefabs/gate.prefab.yaml", "tag": "new"},
					{"path": "logic/lever.go", "tag": "edit"},
				},
				"questions": []map[string]any{
					{
						"id":     "q1",
						"label":  "Gate",
						"prompt": "Where should the gate stand?",
						"options": []map[string]any{
							{
								"id":        "door",
								"label":     "Right after the door",
								"note":      "Suggested",
								"suggested": true,
								"ghostOverrides": []map[string]any{
									{"step": 2, "ghostId": "gate", "x": 432},
								},
							},
							{
								"id":    "mid",
								"label": "Mid-room",
								"note":  "Slimes guard the lever",
								"ghostOverrides": []map[string]any{
									{"step": 2, "ghostId": "gate", "x": 588},
								},
							},
							{
								"id":    "end",
								"label": "At the end",
								"note":  "Guards the coins",
								"ghostOverrides": []map[string]any{
									{"step": 2, "ghostId": "gate", "x": 714},
								},
							},
						},
					},
					{
						"id":     "q2",
						"label":  "Lever",
						"prompt": "What does the lever do to the gate?",
						"options": []map[string]any{
							{"id": "open", "label": "Stays open", "note": "Suggested", "suggested": true},
							{"id": "timed", "label": "Closes after 5 s", "note": "A short race"},
							{"id": "toggle", "label": "Toggles each pull", "note": "Slimes can be shut in"},
						},
					},
				},
				"steps": []map[string]any{
					{
						"number":     1,
						"title":      "Extend the World tiles 640 px past the door",
						"files":      []string{"scenes/level_01.scene.yaml"},
						"filesLabel": "level_01.scene.yaml",
						"adds":       "1 layer · 214 tiles",
						"ghosts": []map[string]any{
							{"id": "room2", "x": 404, "y": 150, "width": 372, "height": 300, "label": "World +640 px"},
						},
					},
					{
						"number":     2,
						"title":      "Add a gate and a lever",
						"files":      []string{"prefabs/gate.prefab.yaml", "scenes/level_01.scene.yaml"},
						"filesLabel": "gate.prefab.yaml · level_01",
						"adds":       "1 new prefab · 2 objects",
						"objects":    2,
						"ghosts": []map[string]any{
							{"id": "gate", "x": 432, "y": 330, "width": 14, "height": 60, "label": "gate · lever"},
						},
					},
					{
						"number":     3,
						"title":      "Place {slimes} slimes and {coins} coins in room 2",
						"files":      []string{"scenes/level_01.scene.yaml"},
						"filesLabel": "level_01.scene.yaml",
						"fields": []map[string]any{
							{"key": "slimes", "label": "Slimes", "value": 3, "min": 1, "max": 6, "countsObjects": true},
							{"key": "coins", "label": "Coins", "value": 5, "min": 0, "max": 9, "countsObjects": true},
						},
						"ghosts": []map[string]any{
							{"id": "slimes", "x": 452, "y": 372, "width": 26, "height": 16, "repeatField": "slimes", "spanWidth": 308},
						},
					},
					{
						"number":           4,
						"title":            "Pulling the lever opens the gate",
						"files":            []string{"logic/lever.go"},
						"filesLabel":       "logic/lever.go",
						"adds":             "1 new function",
						"permission":       "Edit Go",
						"permissionAction": "code",
					},
					{
						"number":           5,
						"title":            "Run check and a 15 s headless playtest",
						"filesLabel":       "no files",
						"adds":             "reports back",
						"permission":       "Run the game",
						"permissionAction": "run",
					},
				},
			},
		})
		outcomeCh <- callOutcome{res: res, err: callErr}
	}()

	// Wait until the proposed plan is staged in Draft phase in the editor.
	app.WaitUntil(3*time.Second, func() bool {
		pl := ed.ActivePlan()
		return pl != nil && pl.Phase == editor.PlanPhaseDraft
	})

	// 1. Answer only question 1 -> Approve MUST be refused.
	app.Post(func() {
		if err := ed.AnswerPlanQuestion("q1", "mid"); err != nil {
			t.Errorf("AnswerPlanQuestion(q1): %v", err)
		}
		if _, err := ed.ApproveAgentPlan(); err == nil {
			t.Errorf("ApproveAgentPlan succeeded with 1 unanswered question; want refusal")
		}
		if ok, _ := ed.CanApprovePlan(); ok {
			t.Errorf("CanApprovePlan = true with unanswered question; want false")
		}
	})
	app.Settle()

	// Verify plan_propose has not returned yet and nothing was written.
	select {
	case out := <-outcomeCh:
		t.Fatalf("plan_propose returned prematurely before approval: %+v", out)
	default:
	}

	// 2. Answer question 2, remove step 4, change slimes count 3 -> 5, add a note, and approve.
	app.Post(func() {
		if err := ed.AnswerPlanQuestion("q2", "toggle"); err != nil {
			t.Errorf("AnswerPlanQuestion(q2): %v", err)
		}
		if err := ed.RemovePlanStep(4); err != nil {
			t.Errorf("RemovePlanStep(4): %v", err)
		}
		if err := ed.SetPlanStepField(3, "slimes", 5); err != nil {
			t.Errorf("SetPlanStepField(3, slimes, 5): %v", err)
		}
		if err := ed.SetPlanStepNote(3, "one slime on the ledge"); err != nil {
			t.Errorf("SetPlanStepNote(3): %v", err)
		}
		if _, err := ed.ApproveAgentPlan(); err != nil {
			t.Errorf("ApproveAgentPlan: %v", err)
		}
	})
	app.Settle()

	// 3. Receive and verify the structured decision returned by plan_propose.
	var out callOutcome
	select {
	case out = <-outcomeCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for plan_propose decision")
	}
	if out.err != nil || out.res == nil || out.res.IsError {
		t.Fatalf("plan_propose failed: err=%v res=%+v", out.err, out.res)
	}

	raw, err := json.Marshal(out.res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured decision: %v", err)
	}
	var dec editor.AgentPlanDecision
	if err := json.Unmarshal(raw, &dec); err != nil {
		t.Fatalf("unmarshal AgentPlanDecision: %v", err)
	}
	if dec.Decision != "approved" {
		t.Fatalf("decision = %q, want approved", dec.Decision)
	}
	if len(dec.Answers) != 2 || dec.Answers[0].OptionID != "mid" || dec.Answers[1].OptionID != "toggle" {
		t.Fatalf("answers = %+v, want q1=mid and q2=toggle", dec.Answers)
	}
	if len(dec.RemovedSteps) != 1 || dec.RemovedSteps[0] != 4 {
		t.Fatalf("removedSteps = %+v, want [4]", dec.RemovedSteps)
	}
	if len(dec.Steps) != 5 {
		t.Fatalf("len(steps) = %d, want 5", len(dec.Steps))
	}
	if dec.Steps[2].Fields["slimes"] != 5 || dec.Steps[2].Note != "one slime on the ledge" {
		t.Fatalf("step 3 decision = %+v, want slimes=5 and note", dec.Steps[2])
	}
	if !dec.Steps[3].Removed {
		t.Fatalf("step 4 decision should be marked Removed")
	}

	// 4. Advance plan steps through plan_step_update and complete -> turn review lists plan steps with done/skipped marks.
	runMCP := func(fn func()) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			fn()
		}()
		for {
			app.Settle()
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	for _, stepNum := range []int{1, 2, 3} {
		n := stepNum
		runMCP(func() {
			r, callErr := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      editormcp.ToolPlanStepUpdate,
				Arguments: map[string]any{"step": n, "status": "done"},
			})
			if callErr != nil || r.IsError {
				t.Errorf("plan_step_update(%d): err=%v res=%+v", n, callErr, r)
			}
		})
	}
	runMCP(func() {
		r, callErr := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      editormcp.ToolPlanStepUpdate,
			Arguments: map[string]any{"step": 5, "status": "done", "complete": true},
		})
		if callErr != nil || r.IsError {
			t.Errorf("plan_step_update(complete): err=%v res=%+v", callErr, r)
		}
	})

	// Check turn's review checklist lists all 5 steps: 1,2,3,5 done, 4 skipped/removed.
	turn, ok := ed.LatestAgentTurn()
	if !ok || turn.Plan == nil {
		t.Fatalf("LatestAgentTurn missing Plan review: ok=%v turn=%+v", ok, turn)
	}
	doneCount, totalCount := turn.Plan.DoneCount()
	if doneCount != 4 || totalCount != 4 {
		t.Fatalf("turn.Plan.DoneCount() = %d/%d, want 4/4", doneCount, totalCount)
	}
	if !turn.Plan.Steps[3].Removed || turn.Plan.Steps[3].Status != editor.PlanStepSkipped {
		t.Fatalf("step 4 in turn review = %+v, want removed+skipped", turn.Plan.Steps[3])
	}
	if !app.Exists("id:agent-turn-plan-review-0") {
		t.Fatal("agent-turn-plan-review-0 did not render in Agent panel")
	}
}

// Security & phase guards: plan_propose refuses You-only files and path traversal,
// and plan_step_update refuses advancing or completing an unapproved draft plan.
func TestPlanProposeYouOnlyAndDraftStepUpdateGuardsOverMCP(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. Refuse You-only target in plan_propose.
	res := call(t, cs, editormcp.ToolPlanPropose, map[string]any{
		"title": "Loosen permissions",
		"wait":  false,
		"steps": []map[string]any{
			{"title": "Edit permissions", "files": []string{"permissions.yaml"}},
		},
	})
	if !res.IsError || !strings.Contains(text(res), "You only") {
		t.Fatalf("plan_propose targeting permissions.yaml: isError=%v text=%q, want You only refusal", res.IsError, text(res))
	}

	// 2. Refuse path traversal in plan_propose.
	res = call(t, cs, editormcp.ToolPlanPropose, map[string]any{
		"title": "Escape project",
		"wait":  false,
		"steps": []map[string]any{
			{"title": "Touch outside", "files": []string{"../secret.yaml"}},
		},
	})
	if !res.IsError || !strings.Contains(text(res), "leaves the project") {
		t.Fatalf("plan_propose targeting ../secret.yaml: isError=%v text=%q, want project root refusal", res.IsError, text(res))
	}

	// 3. Propose valid plan with wait=false -> plan_step_update must refuse while still in Draft.
	res = call(t, cs, editormcp.ToolPlanPropose, map[string]any{
		"title": "Valid room plan",
		"wait":  false,
		"steps": []map[string]any{
			{"title": "Add hero prop", "files": []string{"scenes/room.scene.yaml"}},
		},
	})
	mustOK(t, res)

	res = call(t, cs, editormcp.ToolPlanStepUpdate, map[string]any{
		"step":     1,
		"status":   "done",
		"complete": true,
	})
	if !res.IsError || !strings.Contains(text(res), "not been approved") {
		t.Fatalf("plan_step_update while draft: isError=%v text=%q, want draft refusal", res.IsError, text(res))
	}
}
