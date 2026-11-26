package editormcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
)

// Tool names for agent turns and multi-file YAML editing.
// Note: there is intentionally no MCP tool that keeps or reverts a turn; that remains the person's decision.
const (
	// ToolTurnStart opens a new agent turn that groups subsequent MCP edits across files into one reviewable step.
	ToolTurnStart = "turn_start"
	// ToolTurnEnd finishes the working agent turn so the person can review, revert individual changes, Keep, or Revert.
	ToolTurnEnd = "turn_end"
	// ToolTurnStatus reads the current agent turn's derived changes and which changes the person reverted.
	ToolTurnStatus = "turn_status"
	// ToolFileEdit validates and edits a project YAML file (game.yaml, data table, clip, prefab, or scene) in memory without writing to disk.
	ToolFileEdit = "file_edit"
)

const turnStartSchema = `{"type":"object","additionalProperties":false,"properties":{` +
	`"prompt":{"type":"string","description":"Short request label for the turn, used as the single History undo label when kept"},` +
	`"step":{"type":"string","description":"Optional initial progress step label shown in the Agent panel"}` +
	`}}`

const turnEndSchema = `{"type":"object","additionalProperties":false,"properties":{` +
	`"summary":{"type":"string","description":"Short completion summary shown above the summary card in the Agent panel"},` +
	`"step":{"type":"string","description":"Optional final progress step label shown in the Agent panel"}` +
	`}}`

const turnStatusSchema = `{"type":"object","additionalProperties":false,"properties":{}}`

const fileEditSchema = `{"type":"object","additionalProperties":false,"required":["file","yaml"],"properties":{` +
	`"file":{"type":"string","description":"Slash-separated project path (such as game.yaml, data/enemies.yaml, prefabs/slime.prefab.yaml, clips/idle.anim.yaml, or scenes/level_02.scene.yaml)"},` +
	`"yaml":{"type":"string","description":"Complete valid YAML content for the file; validated strictly before applying in memory and never written to disk until the turn is kept and saved"},` +
	`"why":{"type":"string","description":"Optional reason explaining why this edit was made, shown alongside the derived changes in review"},` +
	`"label":{"type":"string","description":"Optional undo step label when editing game.yaml outside an active turn"}` +
	`}}`

// TurnStatus is the read-only structured result returned by turn_start, turn_end, and turn_status.
type TurnStatus struct {
	HasTurn bool                      `json:"hasTurn"`
	Turn    *editor.AgentTurnSnapshot `json:"turn,omitempty"`
}

// FileEditResult is returned by file_edit after applying a validated in-memory edit.
type FileEditResult struct {
	File     string `json:"file"`
	InTurn   bool   `json:"inTurn"`
	Revision uint64 `json:"revision"`
}

func turnTools() []Tool {
	return []Tool{
		{
			Name:        ToolTurnStart,
			Title:       "Start an agent turn",
			Description: "Open a new pending agent turn labelled with the request so every subsequent MCP edit across scenes, prefabs, data tables, game.yaml, and clips is grouped into one reviewable turn (and one undo step when kept) without writing to disk.",
			InputSchema: []byte(turnStartSchema),
			Action:      ActionEdit,
			Handler:     turnStart,
		},
		{
			Name:        ToolTurnEnd,
			Title:       "Finish an agent turn",
			Description: "Finish the currently working agent turn so it awaits the person's Keep or Revert decision in the Agent panel summary card. Keeping or reverting stays the person's decision.",
			InputSchema: []byte(turnEndSchema),
			Action:      ActionEdit,
			Handler:     turnEnd,
		},
		{
			Name:        ToolTurnStatus,
			Title:       "Read agent turn state",
			Description: "Read the current agent turn's status, touched files, derived structural changes (with kind, title, detail, file, line, why), and which changes the person has reverted. Read-only: the agent cannot keep or revert a turn.",
			InputSchema: []byte(turnStatusSchema),
			Action:      ActionRead,
			Handler:     turnStatus,
		},
		{
			Name:        ToolFileEdit,
			Title:       "Edit a project YAML file in memory",
			Description: "Validate and apply YAML edits to game.yaml, data tables (data/*.yaml), animation clips (*.anim.yaml), prefabs (*.prefab.yaml), or scenes in memory inside the active agent turn without writing to disk until kept and saved.",
			InputSchema: []byte(fileEditSchema),
			Action:      ActionEdit,
			Target:      fileEditTarget,
			Handler:     fileEdit,
		},
	}
}

const maxFileEditBytes = 2 << 20 // 2 MiB max YAML payload accepted by file_edit

func turnStart(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Prompt string `json:"prompt"`
		Step   string `json:"step"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("turn_start: %w", err)
	}
	var out TurnStatus
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if err := ed.StartAgentTurn(args.Prompt); err != nil {
			return err
		}
		if strings.TrimSpace(args.Step) != "" {
			ed.RecordAgentTurnStep(args.Step)
		}
		if snap, ok := ed.CurrentAgentTurn(); ok {
			out.HasTurn = true
			out.Turn = &snap
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("turn_start: %w", err)
	}
	text := "Started agent turn. Subsequent MCP edits stay in memory until the person keeps or reverts the turn."
	if out.Turn != nil {
		text = fmt.Sprintf("Started agent turn #%d (%q). Subsequent MCP edits stay in memory until the person keeps or reverts the turn.", out.Turn.ID, out.Turn.Prompt)
	}
	return &Result{
		Text:       text,
		Structured: out,
	}, nil
}

func turnEnd(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Summary string `json:"summary"`
		Step    string `json:"step"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("turn_end: %w", err)
	}
	var out TurnStatus
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if strings.TrimSpace(args.Step) != "" {
			ed.RecordAgentTurnStep(args.Step)
		}
		if err := ed.EndAgentTurn(args.Summary); err != nil {
			return err
		}
		if snap, ok := ed.CurrentAgentTurn(); ok {
			out.HasTurn = true
			out.Turn = &snap
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("turn_end: %w", err)
	}
	text := formatTurnStatusText(out)
	return &Result{Text: text, Structured: out}, nil
}

func turnStatus(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("turn_status: %w", err)
	}
	var out TurnStatus
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if snap, ok := ed.CurrentAgentTurn(); ok {
			out.HasTurn = true
			out.Turn = &snap
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("turn_status: %w", err)
	}
	return &Result{Text: formatTurnStatusText(out), Structured: out}, nil
}

func formatTurnStatusText(st TurnStatus) string {
	if !st.HasTurn || st.Turn == nil {
		return "No active agent turn."
	}
	t := st.Turn
	var b strings.Builder
	fmt.Fprintf(&b, "Turn #%d (%s): %q — %d changes (%d active, %d reverted) across %d files (+%d -%d).\n",
		t.ID, t.Status, t.Prompt, t.TotalChanges, t.ActiveChanges, t.RevertedCount, len(t.Files), t.AddedLines, t.RemovedLines)
	for _, ch := range t.Changes {
		sign := "+"
		switch ch.Kind {
		case editor.AgentChangeChanged:
			sign = "~"
		case editor.AgentChangeRemoved:
			sign = "-"
		}
		state := ""
		if ch.Reverted {
			state = " [reverted]"
		}
		fmt.Fprintf(&b, "  %s %s", sign, ch.Title)
		if ch.Detail != "" {
			fmt.Fprintf(&b, " (%s)", ch.Detail)
		}
		fmt.Fprintf(&b, " in %s", ch.File)
		if ch.Line > 0 {
			fmt.Fprintf(&b, ":%d", ch.Line)
		}
		if ch.Reason != "" {
			fmt.Fprintf(&b, " — why: %s", ch.Reason)
		}
		b.WriteString(state)
		b.WriteByte('\n')
	}
	return b.String()
}

func fileEdit(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		File  string `json:"file"`
		YAML  string `json:"yaml"`
		Why   string `json:"why"`
		Label string `json:"label"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("file_edit: %w", err)
	}
	clean, err := cleanProjectPath(args.File)
	if err != nil {
		return nil, fmt.Errorf("file_edit: %w", err)
	}
	if strings.TrimSpace(args.YAML) == "" {
		return nil, errors.New("file_edit: yaml must not be empty")
	}
	if len(args.YAML) > maxFileEditBytes {
		return nil, fmt.Errorf("file_edit: yaml exceeds %d bytes limit", maxFileEditBytes)
	}
	var res FileEditResult
	res.File = clean
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Playing() {
			return errors.New("the scene is playing in the editor, which refuses edits")
		}
		if holdErr := sketchHoldEditError(ed, clean); holdErr != nil {
			return holdErr
		}
		if ed.AgentTurnWorking() {
			if err := ed.RecordAgentFileEdit(clean, []byte(args.YAML), args.Why); err != nil {
				return err
			}
			res.InTurn = true
			res.Revision = ed.Revision()
			return nil
		}
		if clean == project.FileName {
			pf, err := project.Parse([]byte(args.YAML))
			if err != nil {
				return err
			}
			if err := project.Validate(pf); err != nil {
				return err
			}
			label := args.Label
			if label == "" {
				label = "Agent: edit game.yaml"
			}
			if err := ed.EditProject(label, func(dst *project.File) {
				*dst = *pf.Clone()
			}); err != nil {
				return err
			}
			res.Revision = ed.Revision()
			return nil
		}
		return fmt.Errorf("start an agent turn with turn_start before editing %s across files", clean)
	}); err != nil {
		return nil, fmt.Errorf("file_edit: %s: %w", clean, err)
	}
	text := fmt.Sprintf("Applied in-memory edit to %s; it stays unsaved until kept and saved.", res.File)
	return &Result{Text: text, Structured: res}, nil
}
