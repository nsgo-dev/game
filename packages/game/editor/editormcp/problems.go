package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

// ToolProblemProposeFix stages an agent-proposed fix for a live problem in the
// editor's Agent card without ever applying it or mutating any scene or file.
const ToolProblemProposeFix = "problem_propose_fix"

const problemProposeFixSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["problem", "explanation", "edit"],
  "properties": {
    "problem": {
      "type": "object",
      "additionalProperties": false,
      "required": ["file"],
      "properties": {
        "file": {"type": "string", "description": "Slash-separated project path of the file with the problem (as game_check reports it)"},
        "line": {"type": "integer", "minimum": 0, "description": "1-based line number of the problem"},
        "column": {"type": "integer", "minimum": 0, "description": "1-based column number of the problem"},
        "message": {"type": "string", "description": "Problem message or substring as game_check reports it"}
      }
    },
    "explanation": {
      "type": "string",
      "description": "Short explanation of why the problem happened and what the proposed fix changes"
    },
    "edit": {
      "type": "object",
      "additionalProperties": false,
      "required": ["kind"],
      "properties": {
        "kind": {
          "type": "string",
          "enum": ["update_connection", "remove_connection", "set_sprite", "set_material", "set_object_id", "set_component_field", "remove_component", "replace_line"],
          "description": "Structured edit kind for the proposed fix"
        },
        "file": {"type": "string", "description": "Optional target scene path; defaults to problem.file"},
        "connectionIndex": {"type": "integer", "minimum": 0, "description": "0-based connection index for update_connection or remove_connection"},
        "from": {"type": "string", "description": "Updated connection source object ID"},
        "event": {"type": "string", "description": "Updated connection event name"},
        "to": {"type": "string", "description": "Updated connection target object ID"},
        "handler": {"type": "string", "description": "Updated connection handler name"},
        "once": {"type": "boolean", "description": "Updated connection once flag"},
        "args": {"type": "object", "description": "Updated connection argument map"},
        "objectId": {"type": "string", "description": "Target object ID for object/component fixes"},
        "newId": {"type": "string", "description": "New object ID for set_object_id"},
        "atlas": {"type": "string", "description": "Atlas name for set_sprite"},
        "frame": {"type": "string", "description": "Frame name for set_sprite"},
        "component": {"type": "string", "description": "Component name for set_component_field or remove_component"},
        "field": {"type": "string", "description": "Component field name for set_component_field"},
        "value": {"description": "Field or material value for set_material or set_component_field"},
        "line": {"type": "integer", "minimum": 1, "description": "1-based line number for replace_line"},
        "oldText": {"type": "string", "description": "Substring on line to replace for replace_line"},
        "replacement": {"type": "string", "description": "Replacement text for replace_line"}
      }
    }
  }
}`

func problemTools() []Tool {
	return []Tool{
		{
			Name:        ToolProblemProposeFix,
			Title:       "Propose a fix for a problem",
			Description: "Propose a fix for a live problem found by game_check, with an explanation and a validated edit. Shows a review card with the YAML diff and Apply fix / Show me / Ignore buttons in the editor's Agent panel. Never applies the fix or edits any scene or file — only the person can apply it.",
			InputSchema: []byte(problemProposeFixSchema),
			Action:      ActionRead,
			Handler:     problemProposeFix,
		},
	}
}

type problemProposeFixArgs struct {
	Problem struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Column  int    `json:"column"`
		Message string `json:"message"`
	} `json:"problem"`
	Explanation string                 `json:"explanation"`
	Edit        problemProposeEditArgs `json:"edit"`
}

type problemProposeEditArgs struct {
	Kind            string          `json:"kind"`
	File            string          `json:"file"`
	ConnectionIndex int             `json:"connectionIndex"`
	From            string          `json:"from"`
	Event           string          `json:"event"`
	To              string          `json:"to"`
	Handler         string          `json:"handler"`
	Once            *bool           `json:"once"`
	Args            map[string]any  `json:"args"`
	ObjectID        string          `json:"objectId"`
	NewID           string          `json:"newId"`
	Atlas           string          `json:"atlas"`
	Frame           string          `json:"frame"`
	Component       string          `json:"component"`
	Field           string          `json:"field"`
	Value           json.RawMessage `json:"value"`
	Line            int             `json:"line"`
	OldText         string          `json:"oldText"`
	Replacement     string          `json:"replacement"`
}

// ProblemProposeFixResult is what problem_propose_fix returns beside its text summary.
// Applied is always false because no MCP tool applies a fix.
type ProblemProposeFixResult struct {
	Status  string                    `json:"status"`
	Applied bool                      `json:"applied"`
	Fix     editor.ProposedProblemFix `json:"fix"`
}

func problemProposeFix(ctx context.Context, c *Call) (*Result, error) {
	var args problemProposeFixArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("problem_propose_fix: %w", err)
	}
	if strings.TrimSpace(args.Problem.File) == "" {
		return nil, errors.New("problem_propose_fix: problem.file is required")
	}
	cleanFile, err := cleanProjectPath(args.Problem.File)
	if err != nil {
		return nil, fmt.Errorf("problem_propose_fix: %w", err)
	}
	var editFile string
	if strings.TrimSpace(args.Edit.File) != "" {
		editFile, err = cleanProjectPath(args.Edit.File)
		if err != nil {
			return nil, fmt.Errorf("problem_propose_fix: %w", err)
		}
	}

	var decodedVal any
	if len(args.Edit.Value) > 0 && string(args.Edit.Value) != "null" {
		v, verr := fieldValue(args.Edit.Value)
		if verr != nil {
			return nil, fmt.Errorf("problem_propose_fix: edit.value: %w", verr)
		}
		decodedVal = v
	}

	prob := assetcheck.Problem{
		File:    cleanFile,
		Line:    args.Problem.Line,
		Column:  args.Problem.Column,
		Message: args.Problem.Message,
	}
	edit := editor.ProblemFixEdit{
		Kind:            args.Edit.Kind,
		File:            editFile,
		ConnectionIndex: args.Edit.ConnectionIndex,
		From:            args.Edit.From,
		Event:           args.Edit.Event,
		To:              args.Edit.To,
		Handler:         args.Edit.Handler,
		Once:            args.Edit.Once,
		Args:            args.Edit.Args,
		ObjectID:        args.Edit.ObjectID,
		NewID:           args.Edit.NewID,
		Atlas:           args.Edit.Atlas,
		Frame:           args.Edit.Frame,
		Component:       args.Edit.Component,
		Field:           args.Edit.Field,
		Value:           decodedVal,
		Line:            args.Edit.Line,
		OldText:         args.Edit.OldText,
		Replacement:     args.Edit.Replacement,
	}

	var fix editor.ProposedProblemFix
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Project == nil {
			return errors.New("problem_propose_fix: no project is open in the editor; open one with go tool game editor <project-dir>")
		}
		var perr error
		fix, perr = ed.ProposeProblemFix(prob, args.Explanation, edit)
		return perr
	}); err != nil {
		return nil, fmt.Errorf("problem_propose_fix: %w", err)
	}

	res := ProblemProposeFixResult{
		Status:  "proposed",
		Applied: false,
		Fix:     fix,
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Proposed fix %s for %s (not applied; awaiting Apply fix in the editor).\n", fix.ID, fix.Problem.Location())
	for _, dl := range fix.Diff {
		switch dl.Kind {
		case "removed":
			b.WriteString("- " + dl.Text + "\n")
		case "added":
			b.WriteString("+ " + dl.Text + "\n")
		default:
			b.WriteString("  " + dl.Text + "\n")
		}
	}
	return &Result{Text: b.String(), Structured: res}, nil
}
