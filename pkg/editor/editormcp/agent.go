package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor"
)

// Agent turn tool names exposed by editormcp alongside scene_read, scene_edit, selection_get, and viewport_image.
const (
	// ToolAgentReportStep lets a connected agent report a live progress step in its own plain words
	// along with the scene object IDs the step touches, and receive queued "Add to this request" follow-ups.
	ToolAgentReportStep = "agent_report_step"
	// ToolAgentTurnGet returns the latest (or named) agent turn started from the ⌘L in-place composer or Agent panel,
	// with the user's prompt, skipPlan flag, selection object IDs, open scene path and YAML, and viewport PNG image.
	ToolAgentTurnGet = "agent_turn_get"
	// ToolAgentTurnReply records the agent's reply and status ("running", "done", "stopped") on a turn in the Agent panel.
	ToolAgentTurnReply = "agent_turn_reply"
)

const (
	agentReportStepSchema = `{"type":"object","additionalProperties":false,"required":["text"],"properties":{` +
		`"text":{"type":"string","minLength":1,"description":"Plain-words description of what the agent did or is doing, such as \"Placed 3 coins toward the door\""},` +
		`"objects":{"type":"array","items":{"type":"string"},"description":"Optional scene object IDs this step touches"},` +
		`"status":{"type":"string","enum":["running","done"],"description":"Whether this step is still running (spinner) or finished (check); defaults to done"}}}`

	agentTurnGetSchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"turnId":{"type":"string","description":"Optional turn ID to read; defaults to the latest agent turn"},` +
		`"includeScene":{"type":"boolean","description":"Include the open scene YAML snapshot in structured output (defaults to true)"},` +
		`"includeImage":{"type":"boolean","description":"Attach the captured viewport PNG as image content (defaults to true)"}}}`

	agentTurnReplySchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"turnId":{"type":"string","description":"Optional turn ID to update; defaults to the latest agent turn"},` +
		`"reply":{"type":"string","description":"Agent reply text to show in the Agent panel thread"},` +
		`"status":{"type":"string","enum":["pending","running","done","stopped"],"description":"Optional turn status"}}}`
)

func agentTools() []Tool {
	return []Tool{
		{
			Name:        ToolAgentReportStep,
			Title:       "Report an agent step",
			Description: "Report a live progress step in the Agent panel in plain words, optionally naming the scene object IDs it touches, and receive any follow-up instructions the person added to the running request.",
			InputSchema: []byte(agentReportStepSchema),
			Action:      ActionRead,
			Handler:     agentReportStep,
		},
		{
			Name:        ToolAgentTurnGet,
			Title:       "Get the latest agent turn",
			Description: "Read the latest (or specified) agent turn submitted from the editor's ⌘L in-place composer or Agent panel, including the user's request, skipPlan flag (true for small ⌘L in-place edits), selected object IDs and names, open scene file and YAML, and the captured viewport PNG image.",
			InputSchema: []byte(agentTurnGetSchema),
			Action:      ActionRead,
			Handler:     agentTurnGet,
		},
		{
			Name:        ToolAgentTurnReply,
			Title:       "Reply to an agent turn",
			Description: "Update an agent turn's status or append the agent's reply text in the editor's Agent panel thread.",
			InputSchema: []byte(agentTurnReplySchema),
			Action:      ActionEdit,
			Handler:     agentTurnReply,
		},
	}
}

// AgentTurnPayload is the structured representation of an editor.AgentTurn returned by agent_turn_get
// and the editor://agent/turn resource.
type AgentTurnPayload struct {
	ID               string   `json:"id"`
	Prompt           string   `json:"prompt"`
	Source           string   `json:"source"`
	SkipPlan         bool     `json:"skipPlan"`
	Status           string   `json:"status"`
	ScenePath        string   `json:"scenePath,omitempty"`
	SceneName        string   `json:"sceneName,omitempty"`
	SceneYAML        string   `json:"sceneYaml,omitempty"`
	SelectionIDs     []string `json:"selectionIds,omitempty"`
	SelectionNames   []string `json:"selectionNames,omitempty"`
	Layer            string   `json:"layer,omitempty"`
	ViewportWidth    int      `json:"viewportWidth,omitempty"`
	ViewportHeight   int      `json:"viewportHeight,omitempty"`
	ViewMode         string   `json:"viewMode,omitempty"`
	HasViewportImage bool     `json:"hasViewportImage"`
	Reply            string   `json:"reply,omitempty"`
}

func makeAgentTurnPayload(turn editor.AgentTurn, includeScene bool) AgentTurnPayload {
	p := AgentTurnPayload{
		ID:               turn.ID,
		Prompt:           turn.Prompt,
		Source:           turn.Source,
		SkipPlan:         turn.SkipPlan,
		Status:           turn.Status,
		ScenePath:        turn.Context.ScenePath,
		SceneName:        turn.Context.SceneName,
		SelectionIDs:     turn.Context.SelectionIDs,
		SelectionNames:   turn.Context.SelectionNames,
		Layer:            turn.Context.Layer,
		ViewportWidth:    turn.Context.ViewportWidth,
		ViewportHeight:   turn.Context.ViewportHeight,
		ViewMode:         turn.Context.ViewMode,
		HasViewportImage: len(turn.Context.ViewportPNG) > 0,
		Reply:            turn.Reply,
	}
	if includeScene {
		p.SceneYAML = turn.Context.SceneYAML
	}
	return p
}

func agentTurnResourceJSON(ed *editor.Editor) ([]byte, []byte, string, bool) {
	turn, ok := ed.LatestAgentTurn()
	if !ok {
		data, _ := json.Marshal(map[string]any{"turn": nil})
		return data, nil, "", false
	}
	payload := makeAgentTurnPayload(turn, true)
	data, _ := json.Marshal(payload)
	return data, turn.Context.ViewportPNG, turn.ID, turn.SkipPlan
}

func agentTurnGet(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		TurnID       string `json:"turnId"`
		IncludeScene *bool  `json:"includeScene"`
		IncludeImage *bool  `json:"includeImage"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("agent_turn_get: %w", err)
	}
	includeScene := args.IncludeScene == nil || *args.IncludeScene
	includeImage := args.IncludeImage == nil || *args.IncludeImage

	var (
		turn  editor.AgentTurn
		found bool
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if args.TurnID != "" {
			turn, found = ed.AgentTurn(args.TurnID)
			if !found {
				return fmt.Errorf("agent turn %q not found", args.TurnID)
			}
			return nil
		}
		turn, found = ed.LatestAgentTurn()
		return nil
	}); err != nil {
		return nil, fmt.Errorf("agent_turn_get: %w", err)
	}
	if !found {
		return nil, errors.New("agent_turn_get: no agent turn has been sent yet")
	}

	payload := makeAgentTurnPayload(turn, includeScene)
	var b strings.Builder
	fmt.Fprintf(&b, "Turn %s (%s", payload.ID, payload.Source)
	if payload.SkipPlan {
		b.WriteString(", skip_plan")
	}
	fmt.Fprintf(&b, "): %s\n", payload.Prompt)
	if payload.ScenePath != "" {
		fmt.Fprintf(&b, "Scene: %s", payload.ScenePath)
		if payload.SceneName != "" {
			fmt.Fprintf(&b, " (%s)", payload.SceneName)
		}
		b.WriteByte('\n')
	} else if payload.SceneName != "" {
		fmt.Fprintf(&b, "Scene: %s\n", payload.SceneName)
	}
	if len(payload.SelectionIDs) > 0 {
		fmt.Fprintf(&b, "Selection (%d): %s\n", len(payload.SelectionIDs), strings.Join(payload.SelectionIDs, ", "))
	} else {
		b.WriteString("Selection: open scene (no objects selected)\n")
	}
	if payload.HasViewportImage {
		fmt.Fprintf(&b, "Viewport image: %d×%d PNG (%s)\n", payload.ViewportWidth, payload.ViewportHeight, payload.ViewMode)
	}

	res := &Result{
		Text:       b.String(),
		Structured: payload,
	}
	if includeImage && len(turn.Context.ViewportPNG) > 0 {
		res.PNG = turn.Context.ViewportPNG
	}
	return res, nil
}

func agentTurnReply(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		TurnID string `json:"turnId"`
		Reply  string `json:"reply"`
		Status string `json:"status"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("agent_turn_reply: %w", err)
	}
	if strings.TrimSpace(args.Reply) == "" && strings.TrimSpace(args.Status) == "" {
		return nil, errors.New("agent_turn_reply: reply or status is required")
	}
	var updated editor.AgentTurn
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if err := ed.ReplyAgentTurn(args.TurnID, args.Reply, args.Status); err != nil {
			return err
		}
		if args.TurnID != "" {
			updated, _ = ed.AgentTurn(args.TurnID)
		} else {
			updated, _ = ed.LatestAgentTurn()
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("agent_turn_reply: %w", err)
	}
	payload := makeAgentTurnPayload(updated, false)
	return &Result{
		Text:       fmt.Sprintf("Updated turn %s (status: %s).", payload.ID, payload.Status),
		Structured: payload,
	}, nil
}
