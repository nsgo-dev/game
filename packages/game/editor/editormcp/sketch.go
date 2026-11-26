package editormcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

// The names of the viewport sketch tools added to the editor's MCP server.
const (
	// ToolSketchRead reads the active/sent viewport sketch marks in scene coordinates and optional PNG image.
	ToolSketchRead = "sketch_read"
	// ToolSketchReading posts the agent's restatement of the user's sketch marks and assumptions before editing.
	ToolSketchReading = "sketch_reading"
)

const (
	sketchReadSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "includeImage": {"type": "boolean", "description": "Include a rendered PNG of the viewport with numbered sketch marks overlaid (defaults to true when marks exist)"},
    "scale": {"type": "number", "minimum": 0.25, "maximum": 4, "description": "Pixels per point for the viewport PNG; 1 when absent"}
  }
}`

	sketchReadingSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["items"],
  "properties": {
    "summary": {"type": "string", "description": "Introductory heading shown on the reading card; defaults to \"I read your sketch as:\""},
    "items": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["number", "text"],
        "properties": {
          "number": {"type": "integer", "minimum": 1, "description": "1-based mark badge number from sketch_read"},
          "mark": {"type": "string", "description": "Optional short caption for the mark (for example 'Box · “3 spikes”'); derived from the mark when omitted"},
          "text": {"type": "string", "description": "Agent's restatement of what it will build for this mark"}
        }
      }
    },
    "assumptions": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Explicit assumptions or questions shown in a warning callout (for example where to add a missing lever)"
    }
  }
}`
)

func sketchTools() []Tool {
	return []Tool{
		{
			Name:        ToolSketchRead,
			Title:       "Read the viewport sketch",
			Description: "Read the person's numbered viewport sketch marks (Pen, Arrow, Box, Text) with exact scene coordinates, labels, prompt, hold state, and an optional PNG of the viewport with the numbered marks overlaid.",
			InputSchema: []byte(sketchReadSchema),
			Action:      ActionRead,
			Handler:     sketchRead,
		},
		{
			Name:        ToolSketchReading,
			Title:       "Post a reading of the viewport sketch",
			Description: "Post the agent's restatement of each numbered sketch mark (\"I read your sketch as: …\") and any assumptions to the editor's reading card. While a sent sketch is awaiting confirmation, scene_edit and other mutating scene tools hold until the person chooses Go ahead, Correct a mark, or replies in words.",
			InputSchema: []byte(sketchReadingSchema),
			Action:      ActionEdit,
			Handler:     sketchReading,
		},
	}
}

// SketchReadResult is the structured payload returned by sketch_read.
type SketchReadResult struct {
	Scene        string                `json:"scene"`
	Active       bool                  `json:"active"`
	HoldState    string                `json:"holdState"`
	HoldActive   bool                  `json:"holdActive"`
	ChipLabel    string                `json:"chipLabel,omitempty"`
	Prompt       string                `json:"prompt,omitempty"`
	SelectedMark int                   `json:"selectedMark,omitempty"`
	View         editor.View2D         `json:"view"`
	Marks        []editor.SketchMark   `json:"marks"`
	Reading      *editor.SketchReading `json:"reading,omitempty"`
	Width        int                   `json:"width,omitempty"`
	Height       int                   `json:"height,omitempty"`
}

func sketchRead(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		IncludeImage *bool   `json:"includeImage"`
		Scale        float64 `json:"scale"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("sketch_read: %w", err)
	}
	scale := args.Scale
	if scale == 0 {
		scale = 1
	}
	if !(scale >= 0.25 && scale <= 4) {
		return nil, fmt.Errorf("sketch_read: scale %v must be between 0.25 and 4", args.Scale)
	}

	var (
		out      SketchReadResult
		pngBytes []byte
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		root := projectRoot(ed)
		marks := ed.SketchMarks()
		out = SketchReadResult{
			Scene:        sceneName(ed, root),
			Active:       ed.SketchActive(),
			HoldState:    string(ed.SketchHoldState()),
			HoldActive:   ed.SketchHoldActive(),
			SelectedMark: ed.SelectedSketchMark(),
			View:         ed.View2D(),
			Marks:        marks,
			Reading:      ed.SketchReading(),
		}
		if len(marks) > 0 {
			out.ChipLabel = ed.SketchChipLabel()
		}
		if req, ok := ed.LastSketchRequest(); ok {
			out.Prompt = req.Prompt
		} else if draft := strings.TrimSpace(ed.SketchPromptDraft()); draft != "" {
			out.Prompt = draft
		}
		wantImg := len(marks) > 0
		if args.IncludeImage != nil {
			wantImg = *args.IncludeImage
		}
		if wantImg {
			b, w, h, err := ed.RenderSketchViewportPNG(float32(scale))
			if err != nil {
				return fmt.Errorf("sketch_read: %w", err)
			}
			pngBytes = b
			out.Width, out.Height = w, h
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var b strings.Builder
	if len(out.Marks) == 0 {
		fmt.Fprintf(&b, "No sketch marks on %s (hold: %s).\n", out.Scene, out.HoldState)
	} else {
		fmt.Fprintf(&b, "%s on %s (scene coordinates, hold: %s):\n", out.ChipLabel, out.Scene, out.HoldState)
		if out.Prompt != "" {
			fmt.Fprintf(&b, "Prompt: %s\n", out.Prompt)
		}
		for _, m := range out.Marks {
			fmt.Fprintf(&b, "%d. %s", m.Number, m.GeometrySummary())
			if m.Label != "" {
				fmt.Fprintf(&b, " — %q", m.Label)
			}
			b.WriteByte('\n')
		}
	}
	return &Result{Text: b.String(), Structured: out, PNG: pngBytes}, nil
}

// SketchReadingResult is the structured payload returned by sketch_reading.
type SketchReadingResult struct {
	Scene      string                `json:"scene"`
	HoldState  string                `json:"holdState"`
	HoldActive bool                  `json:"holdActive"`
	Reading    *editor.SketchReading `json:"reading"`
}

func sketchReading(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Summary     string                     `json:"summary"`
		Items       []editor.SketchReadingItem `json:"items"`
		Assumptions []string                   `json:"assumptions"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("sketch_reading: %w", err)
	}
	var out SketchReadingResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if err := ed.PostSketchReading(editor.SketchReading{
			Summary:     args.Summary,
			Items:       args.Items,
			Assumptions: args.Assumptions,
		}); err != nil {
			return err
		}
		out = SketchReadingResult{
			Scene:      sceneName(ed, projectRoot(ed)),
			HoldState:  string(ed.SketchHoldState()),
			HoldActive: ed.SketchHoldActive(),
			Reading:    ed.SketchReading(),
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("sketch_reading: %w", err)
	}
	text := fmt.Sprintf("Posted sketch reading for %d mark(s) on %s. Scene edits remain held until the person chooses Go ahead, Correct a mark, or replies in words.", len(out.Reading.Items), out.Scene)
	return &Result{Text: text, Structured: out}, nil
}

// sketchHoldEditError returns a refusal error when a sent sketch is awaiting user confirmation.
func sketchHoldEditError(ed *editor.Editor, file string) *EditError {
	if ed == nil || !ed.SketchHoldActive() {
		return nil
	}
	return &EditError{
		File:    file,
		Problem: "a sent viewport sketch is waiting for the person to confirm before editing the scene",
		Fix:     "post your restatement with sketch_reading and wait for the person to choose Go ahead or reply before editing",
	}
}
