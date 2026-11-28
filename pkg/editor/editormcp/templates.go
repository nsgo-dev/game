package editormcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nsgo-dev/game/pkg/editor"
)

const ToolTemplatesList = "templates_list"

func templateTools() []Tool {
	return []Tool{{Name: ToolTemplatesList, Title: "List project templates", Description: "List versioned project templates available to game new and the editor Start Screen, optionally filtered by dimension, genre, orientation, input, or player count.", InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{"dimension":{"type":"string"},"genre":{"type":"string"},"orientation":{"type":"string"},"input":{"type":"string"},"players":{"type":"integer","minimum":1}}}`), Action: ActionRead, Handler: templatesList}}
}

func templatesList(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Dimension, Genre, Orientation, Input string
		Players                              int `json:"players"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("%s: %w", ToolTemplatesList, err)
	}
	var out []editor.ProjectTemplate
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		for _, t := range ed.ProjectTemplates() {
			if args.Dimension != "" && t.EffectiveDimension() != args.Dimension || args.Genre != "" && t.Genre != args.Genre || args.Orientation != "" && t.Orientation != args.Orientation {
				continue
			}
			if args.Input != "" && !hasTemplateInput(t.Inputs, args.Input) || args.Players > 0 && (t.PlayersMax < args.Players || t.PlayersMin > args.Players) {
				continue
			}
			out = append(out, t)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(out)
	return &Result{Text: string(b), Structured: out}, nil
}

func hasTemplateInput(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
