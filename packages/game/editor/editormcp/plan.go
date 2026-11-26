package editormcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

// Plan tool names exposed by editormcp for proposing a plan before edits and tracking step completion.
const (
	// ToolPlanPropose proposes a structured multi-step plan with clarifying questions, editable step parameters,
	// planned viewport ghosts, and planned Scene Tree nodes before any file or scene is modified, and waits for
	// the person's decision (approved with answers and edited steps, or stopped).
	ToolPlanPropose = "plan_propose"
	// ToolPlanStepUpdate updates a running plan step's status (running, done, failed, skipped) and optionally
	// completes the plan so the turn's review lists each plan step and marks which were done.
	ToolPlanStepUpdate = "plan_step_update"
)

const planProposeSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "steps"],
  "properties": {
    "id": {"type": "string", "description": "Optional stable plan ID"},
    "turnId": {"type": "string", "description": "Optional agent turn ID to associate with this plan"},
    "prompt": {"type": "string", "description": "Optional user request text shown above the plan card"},
    "title": {"type": "string", "description": "Short plan title, for example 'Room 2: a lever gate, slimes and a coin reward'"},
    "summary": {"type": "string", "description": "Optional custom summary; when omitted, computed dynamically from live steps, files, objects, and estimate"},
    "reason": {"type": "string", "description": "Optional explanation of why a plan was requested; when omitted, derived from permissions.yaml"},
    "wait": {"type": "boolean", "description": "Wait for the person to approve or stop the plan before returning (defaults to true for MCP calls; forced false for synchronous automation calls)"},
    "estimate": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "duration": {"type": "string", "description": "Estimated duration, e.g. 'about 2 min'"},
        "tokens": {"type": "string", "description": "Estimated token budget, e.g. '~35k tokens'"}
      }
    },
    "files": {
      "type": "array",
      "description": "Optional ordered list of files touched by this plan with tags ('edit', 'new', 'delete')",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["path"],
        "properties": {
          "path": {"type": "string"},
          "tag": {"type": "string", "enum": ["edit", "new", "delete"]}
        }
      }
    },
    "questions": {
      "type": "array",
      "description": "Clarifying questions the person must answer (or let Just do it pick the suggested option) before approval",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "prompt", "options"],
        "properties": {
          "id": {"type": "string"},
          "label": {"type": "string", "description": "Short label shown when the answered question collapses (e.g. 'Gate')"},
          "prompt": {"type": "string"},
          "options": {
            "type": "array",
            "minItems": 1,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["id", "label"],
              "properties": {
                "id": {"type": "string"},
                "label": {"type": "string"},
                "note": {"type": "string"},
                "suggested": {"type": "boolean"},
                "preview": {
                  "type": "object",
                  "additionalProperties": false,
                  "properties": {
                    "kind": {"type": "string", "enum": ["room", "strip"]},
                    "roomSplitPercent": {"type": "number"},
                    "gatePercent": {"type": "number"},
                    "leverPercent": {"type": "number"},
                    "segments": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "additionalProperties": false,
                        "properties": {
                          "from": {"type": "number"},
                          "to": {"type": "number"},
                          "active": {"type": "boolean"}
                        }
                      }
                    }
                  }
                },
                "stepTitleOverrides": {
                  "type": "object",
                  "description": "Optional map of 1-based step number string to updated step title when this option is picked"
                },
                "ghostOverrides": {
                  "type": "array",
                  "description": "Optional viewport ghost position/size/label overrides when this option is picked",
                  "items": {
                    "type": "object",
                    "additionalProperties": false,
                    "required": ["step"],
                    "properties": {
                      "step": {"type": "integer", "minimum": 1},
                      "ghostId": {"type": "string"},
                      "x": {"type": "number"},
                      "y": {"type": "number"},
                      "width": {"type": "number"},
                      "height": {"type": "number"},
                      "label": {"type": "string"},
                      "labelX": {"type": "number"},
                      "labelY": {"type": "number"},
                      "replaceWith": {"type": "array"}
                    }
                  }
                }
              }
            }
          }
        }
      }
    },
    "steps": {
      "type": "array",
      "minItems": 1,
      "description": "Numbered plan steps shown in the Agent panel, Scene Tree, and viewport ghosts",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["title"],
        "properties": {
          "number": {"type": "integer", "minimum": 1},
          "id": {"type": "string"},
          "title": {"type": "string"},
          "files": {"type": "array", "items": {"type": "string"}},
          "filesLabel": {"type": "string"},
          "adds": {"type": "string"},
          "objects": {"type": "integer", "minimum": 0},
          "permission": {"type": "string", "description": "Permission badge such as 'Edit Go' or 'Run the game'"},
          "permissionAction": {"type": "string", "enum": ["edit", "create", "delete", "run", "code"]},
          "permissionTarget": {"type": "string"},
          "notePlaceholder": {"type": "string"},
          "fields": {
            "type": "array",
            "description": "Editable numeric fields with steppers and limits",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["key", "label", "value", "min", "max"],
              "properties": {
                "key": {"type": "string"},
                "label": {"type": "string"},
                "value": {"type": "integer"},
                "min": {"type": "integer"},
                "max": {"type": "integer"},
                "step": {"type": "integer", "minimum": 1},
                "hint": {"type": "string"},
                "countsObjects": {"type": "boolean"}
              }
            }
          },
          "ghosts": {
            "type": "array",
            "description": "Planned viewport rectangles or objects drawn in agent violet for this step",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["x", "y", "width", "height"],
              "properties": {
                "id": {"type": "string"},
                "x": {"type": "number"},
                "y": {"type": "number"},
                "width": {"type": "number"},
                "height": {"type": "number"},
                "radius": {"type": "number"},
                "label": {"type": "string"},
                "activeLabel": {"type": "string"},
                "labelX": {"type": "number"},
                "labelY": {"type": "number"},
                "repeatField": {"type": "string"},
                "spanWidth": {"type": "number"},
                "stepX": {"type": "number"},
                "staggerY": {"type": "number"},
                "objectId": {"type": "string"}
              }
            }
          },
          "nodes": {
            "type": "array",
            "description": "Planned Scene Tree nodes (or existing nodes to badge with this step number)",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["name"],
              "properties": {
                "name": {"type": "string"},
                "layer": {"type": "string"},
                "existing": {"type": "boolean"},
                "objectId": {"type": "string"}
              }
            }
          }
        }
      }
    }
  }
}`

const planStepUpdateSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "step": {"type": "integer", "minimum": 1, "description": "1-based plan step number to update"},
    "status": {"type": "string", "enum": ["pending", "running", "done", "failed", "skipped"], "description": "New status for step"},
    "complete": {"type": "boolean", "description": "When true, marks the entire plan finished and attaches the step checklist to the turn's review"}
  }
}`

func planTools() []Tool {
	return []Tool{
		{
			Name:        ToolPlanPropose,
			Title:       "Propose a plan before editing",
			Description: "Propose a structured plan with clarifying questions, numbered editable steps, planned Scene Tree nodes, and agent-violet viewport ghosts before modifying any scene or file. Waits for the person to approve (returning their answers and edited/removed steps) or stop.",
			InputSchema: []byte(planProposeSchema),
			Action:      ActionRead,
			Handler:     planPropose,
		},
		{
			Name:        ToolPlanStepUpdate,
			Title:       "Update active plan step progress",
			Description: "Report progress on an approved plan step (running, done, failed, skipped) or complete the plan so the turn's review lists each plan step and marks which were done. Returns whether the person pressed Stop.",
			InputSchema: []byte(planStepUpdateSchema),
			Action:      ActionRead,
			Handler:     planStepUpdate,
		},
	}
}

type planProposeArgs struct {
	ID        string                `json:"id"`
	TurnID    string                `json:"turnId"`
	Prompt    string                `json:"prompt"`
	Title     string                `json:"title"`
	Summary   string                `json:"summary"`
	Reason    string                `json:"reason"`
	Wait      *bool                 `json:"wait"`
	Estimate  editor.PlanEstimate   `json:"estimate"`
	Files     []editor.PlanFile     `json:"files"`
	Questions []editor.PlanQuestion `json:"questions"`
	Steps     []editor.PlanStep     `json:"steps"`
}

func planPropose(ctx context.Context, c *Call) (*Result, error) {
	var args planProposeArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("plan_propose: %w", err)
	}
	proposal := editor.AgentPlan{
		ID:        args.ID,
		TurnID:    args.TurnID,
		Prompt:    args.Prompt,
		Title:     args.Title,
		Summary:   args.Summary,
		Reason:    args.Reason,
		Estimate:  args.Estimate,
		Files:     args.Files,
		Questions: args.Questions,
		Steps:     args.Steps,
	}

	var (
		staged     *editor.AgentPlan
		decisionCh <-chan editor.AgentPlanDecision
		summary    string
		youOnlyErr *YouOnlyError
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		checkPath := func(raw string) *YouOnlyError {
			p := strings.TrimSpace(raw)
			if p == "" {
				return nil
			}
			if dom, ok := ed.CheckYouOnlyTarget(p); ok {
				return &YouOnlyError{
					Tool:   ToolPlanPropose,
					Domain: dom,
					Path:   p,
					Reason: fmt.Sprintf("plans cannot modify %s files (%s is You only)", dom.DisplayName(), p),
					Hint:   "Submit a draft with permissions_draft instead; only the person can approve You-only changes.",
				}
			}
			return nil
		}
		for _, f := range proposal.Files {
			if yerr := checkPath(f.Path); yerr != nil {
				youOnlyErr = yerr
				return nil
			}
		}
		for _, st := range proposal.Steps {
			for _, f := range st.Files {
				if yerr := checkPath(f); yerr != nil {
					youOnlyErr = yerr
					return nil
				}
			}
			if st.PermissionAction != editor.PermissionActionRun {
				if yerr := checkPath(st.PermissionTarget); yerr != nil {
					youOnlyErr = yerr
					return nil
				}
			}
		}
		var err error
		staged, decisionCh, err = ed.ProposeAgentPlan(proposal)
		if err != nil {
			return err
		}
		summary = ed.PlanSummaryLine()
		return nil
	}); err != nil {
		return nil, fmt.Errorf("plan_propose: %w", err)
	}
	if youOnlyErr != nil {
		return nil, youOnlyErr
	}

	wait := true
	if args.Wait != nil {
		wait = *args.Wait
	}
	if c.onEditor {
		wait = false
	}

	if !wait {
		pending := editor.AgentPlanDecision{
			PlanID:         staged.ID,
			Decision:       "pending",
			NothingWritten: true,
			Summary:        summary,
		}
		return &Result{
			Text:       fmt.Sprintf("Proposed plan %q (%s); waiting for the person to approve or stop.", staged.Title, summary),
			Structured: pending,
		}, nil
	}

	select {
	case dec, ok := <-decisionCh:
		if !ok {
			dec = editor.AgentPlanDecision{
				PlanID:         staged.ID,
				Decision:       "stopped",
				NothingWritten: true,
			}
		}
		return &Result{
			Text:       formatPlanDecisionText(staged.Title, dec),
			Structured: dec,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.s.done:
		return nil, errors.New("plan_propose: editor MCP server closed")
	}
}

func formatPlanDecisionText(title string, dec editor.AgentPlanDecision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan %q (%s): %s", title, dec.PlanID, dec.Decision)
	if dec.PickedForYou {
		b.WriteString(" (Just do it — suggested answers picked)")
	}
	b.WriteByte('\n')
	if len(dec.Answers) > 0 {
		b.WriteString("Answers:\n")
		for _, a := range dec.Answers {
			fmt.Fprintf(&b, "- %s: %s (%s)\n", a.QuestionID, a.OptionLabel, a.OptionID)
		}
	}
	if len(dec.Steps) > 0 {
		b.WriteString("Steps:\n")
		for _, st := range dec.Steps {
			state := "active"
			if st.Removed {
				state = "removed"
			}
			fmt.Fprintf(&b, "- %d. [%s] %s", st.Number, state, st.Title)
			if st.Note != "" {
				fmt.Fprintf(&b, " (note: %s)", st.Note)
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

// PlanStepUpdatePayload is the structured response returned by plan_step_update.
type PlanStepUpdatePayload struct {
	PlanID     string           `json:"planId,omitempty"`
	Phase      editor.PlanPhase `json:"phase"`
	Stopped    bool             `json:"stopped"`
	ActiveStep int              `json:"activeStep,omitempty"`
	DoneSteps  int              `json:"doneSteps"`
	TotalSteps int              `json:"totalSteps"`
}

func planStepUpdate(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Step     int                   `json:"step"`
		Status   editor.PlanStepStatus `json:"status"`
		Complete bool                  `json:"complete"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("plan_step_update: %w", err)
	}
	if args.Step == 0 && !args.Complete {
		return nil, errors.New("plan_step_update: step or complete is required")
	}

	var payload PlanStepUpdatePayload
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		cur := ed.ActivePlan()
		if cur == nil {
			return errors.New("no active plan")
		}
		payload.PlanID = cur.ID
		turnID := cur.TurnID
		if cur.Phase == editor.PlanPhaseStopped {
			payload.Phase = editor.PlanPhaseStopped
			payload.Stopped = true
			for _, s := range cur.Steps {
				if s.Removed {
					continue
				}
				payload.TotalSteps++
				if s.Status == editor.PlanStepDone {
					payload.DoneSteps++
				}
			}
			return nil
		}
		if cur.Phase != editor.PlanPhaseRunning {
			return fmt.Errorf("plan %s is in phase %q and has not been approved to run", cur.ID, cur.Phase)
		}
		if args.Step > 0 {
			st := args.Status
			if st == "" {
				st = editor.PlanStepDone
			}
			if err := ed.UpdateAgentPlanStep(args.Step, st); err != nil {
				return err
			}
		}
		if args.Complete {
			if err := ed.CompleteAgentPlan(); err != nil {
				return err
			}
			payload.Phase = editor.PlanPhaseDone
			found := false
			if turnID != "" {
				for _, t := range ed.AgentTurns() {
					if t.ID == turnID && t.Plan != nil {
						payload.DoneSteps, payload.TotalSteps = t.Plan.DoneCount()
						found = true
						break
					}
				}
			}
			if !found {
				if turn, ok := ed.LatestAgentTurn(); ok && turn.Plan != nil {
					payload.DoneSteps, payload.TotalSteps = turn.Plan.DoneCount()
				}
			}
			return nil
		}
		after := ed.ActivePlan()
		if after != nil {
			payload.Phase = after.Phase
			payload.Stopped = after.Phase == editor.PlanPhaseStopped
			payload.ActiveStep = after.ActiveStep
			for _, s := range after.Steps {
				if s.Removed {
					continue
				}
				payload.TotalSteps++
				if s.Status == editor.PlanStepDone {
					payload.DoneSteps++
				}
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("plan_step_update: %w", err)
	}

	text := fmt.Sprintf("Plan %s: %s (%d of %d steps done).", payload.PlanID, payload.Phase, payload.DoneSteps, payload.TotalSteps)
	if payload.Stopped {
		text = fmt.Sprintf("Plan %s was stopped by the person.", payload.PlanID)
	}
	return &Result{
		Text:       text,
		Structured: payload,
	}, nil
}
