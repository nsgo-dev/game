package editormcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
)

// AutomationResult is what a tool registered by RegisterAutomation returns to nsgo mcp: app tools return JSON, so a PNG comes as base64 text (nsgo-dev/nsgo#2343).
type AutomationResult struct {
	// Text is the tool's text for the agent.
	Text string `json:"text,omitempty"`
	// Structured is the tool's structured result, or absent.
	Structured any `json:"structured,omitempty"`
	// PNGBase64 is the tool's PNG in base64, or empty.
	PNGBase64 string `json:"pngBase64,omitempty"`
}

// RegisterAutomation registers the server's tools, as they are now, with github.com/nsgo-dev/nsgo/automation on cx, so that nsgo mcp offers them too while the editor runs under go tool nsgo dev -automation or go tool nsgo test, beside the tools that drive its windows. cx must be a scope on the editor's goroutine, the one Options.Run hands functions to; the tools are unregistered when cx is cleaned up. App tools run synchronously on that goroutine (nsgo-dev/nsgo#2342), so each call runs there from start to end: game_check holds the editor until the check ends. In ordinary and release builds automation registers nothing, and the server's own endpoint (Open) is how agents reach the editor.
func (s *Server) RegisterAutomation(cx *core.Scope) error {
	for _, t := range s.Tools() {
		t := t
		err := automation.Register(cx, automation.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
				res, err := s.call(ctx, t, args, true)
				if err != nil {
					var edit *EditError
					if errors.As(err, &edit) {
						return nil, edit
					}
					return nil, err
				}
				out := AutomationResult{Text: res.Text, Structured: res.Structured}
				if res.PNG != nil {
					out.PNGBase64 = base64.StdEncoding.EncodeToString(res.PNG)
				}
				return out, nil
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}
