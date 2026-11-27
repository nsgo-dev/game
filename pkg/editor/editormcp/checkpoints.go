package editormcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
)

const (
	// ToolCheckpointList lists persistent turn checkpoints and session history entries in the open project.
	ToolCheckpointList = "checkpoint_list"
	// ToolCheckpointRestore restores a persistent checkpoint back to its pre-turn files as one undoable step.
	ToolCheckpointRestore = "checkpoint_restore"
)

const checkpointListSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "filter": {"type": "string", "enum": ["all", "you", "agent"], "description": "Filter entries by actor; all when absent"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Maximum number of entries to return; 50 when absent"}
  }
}`

const checkpointRestoreSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["id"],
  "properties": {
    "id": {"type": "string", "minLength": 1, "description": "The checkpoint ID to restore (from checkpoint_list)"}
  }
}`

// CheckpointListItem is one entry returned by checkpoint_list.
type CheckpointListItem struct {
	ID           string   `json:"id"`
	CheckpointID string   `json:"checkpointId,omitempty"`
	Time         string   `json:"time,omitempty"`
	Who          string   `json:"who"`
	Kind         string   `json:"kind,omitempty"`
	Title        string   `json:"title"`
	Detail       string   `json:"detail,omitempty"`
	Files        []string `json:"files,omitempty"`
	Persistent   bool     `json:"persistent"`
	Restorable   bool     `json:"restorable"`
}

// CheckpointListResult is the structured payload returned by checkpoint_list.
type CheckpointListResult struct {
	Filter  string               `json:"filter"`
	Total   int                  `json:"total"`
	Entries []CheckpointListItem `json:"entries"`
}

// CheckpointRestoreResult is the structured payload returned by checkpoint_restore.
type CheckpointRestoreResult struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Files    []string `json:"files,omitempty"`
	Revision uint64   `json:"revision"`
}

func checkpointTools() []Tool {
	return []Tool{
		{
			Name:        ToolCheckpointList,
			Title:       "List history and checkpoints",
			Description: "List recent project history and persistent turn checkpoints stored in .nsgo/checkpoints, newest first, optionally filtered by who made them (all, you, or agent).",
			InputSchema: []byte(checkpointListSchema),
			Action:      ActionRead,
			Handler:     checkpointList,
		},
		{
			Name:        ToolCheckpointRestore,
			Title:       "Restore a checkpoint",
			Description: "Restore every file recorded in a persistent checkpoint back to its pre-turn bytes as one undoable step; Undo puts the turn back.",
			InputSchema: []byte(checkpointRestoreSchema),
			Action:      ActionEdit,
			targets:     checkpointRestoreTargets,
			Handler:     checkpointRestore,
		},
	}
}

// checkpointRestoreTargets expands a checkpoint_restore call into one PermissionRequest per file
// recorded in the checkpoint manifest so You-only boundaries and file-scoped edit/code/delete rules
// are evaluated against every file that restoring would modify or remove.
func checkpointRestoreTargets(args json.RawMessage, ed *editor.Editor) []editor.PermissionRequest {
	if ed == nil {
		return nil
	}
	var parsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return nil
	}
	id := strings.TrimPrefix(strings.TrimSpace(parsed.ID), "cp:")
	if id == "" {
		return nil
	}
	m, err := ed.CheckpointManifest(id)
	if err != nil || len(m.Files) == 0 {
		return nil
	}
	reqs := make([]editor.PermissionRequest, 0, len(m.Files))
	for _, f := range m.Files {
		action := editor.PermissionActionEdit
		if !f.Existed {
			action = editor.PermissionActionDelete
		} else if strings.HasSuffix(strings.ToLower(f.Path), ".go") {
			action = editor.PermissionActionCode
		}
		reqs = append(reqs, editor.PermissionRequest{
			Tool:   ToolCheckpointRestore,
			Action: action,
			Target: f.Path,
		})
	}
	return reqs
}

func checkpointList(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Filter string `json:"filter"`
		Limit  int    `json:"limit"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("checkpoint_list: %w", err)
	}
	filter := editor.HistoryFilterAll
	switch args.Filter {
	case "", "all":
		filter = editor.HistoryFilterAll
	case "you":
		filter = editor.HistoryFilterYou
	case "agent":
		filter = editor.HistoryFilterAgent
	default:
		return nil, fmt.Errorf("checkpoint_list: unknown filter %q (want all, you, or agent)", args.Filter)
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var res CheckpointListResult
	res.Filter = string(filter)
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Project != nil && ed.ProjectPath != "" {
			_, _ = ed.LoadCheckpoints()
		}
		entries := ed.UnifiedHistoryEntries(filter)
		res.Total = len(entries)
		if len(entries) > limit {
			entries = entries[:limit]
		}
		res.Entries = make([]CheckpointListItem, 0, len(entries))
		for _, e := range entries {
			ts := ""
			if !e.Timestamp.IsZero() {
				ts = e.Timestamp.UTC().Format(time.RFC3339)
			}
			id := fmt.Sprintf("history:%d", e.ID)
			kind := string(e.Kind)
			restorable := e.ID > 0
			if e.Kind == editor.HistoryEntryCheckpoint && e.CheckpointID != "" {
				id = "cp:" + e.CheckpointID
				kind = string(e.CheckpointKind)
				restorable = len(e.Files) > 0
			}
			res.Entries = append(res.Entries, CheckpointListItem{
				ID:           id,
				CheckpointID: e.CheckpointID,
				Time:         ts,
				Who:          string(e.Author),
				Kind:         kind,
				Title:        e.Summary,
				Detail:       e.Detail,
				Files:        e.Files,
				Persistent:   e.Persistent,
				Restorable:   restorable,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	if len(res.Entries) == 0 {
		b.WriteString("No history entries or checkpoints.\n")
	} else {
		fmt.Fprintf(&b, "%d history entries (showing %d):\n", res.Total, len(res.Entries))
		for _, item := range res.Entries {
			tag := item.Who
			if tag == "" {
				tag = item.Kind
			}
			if item.CheckpointID != "" {
				tag += " · checkpoint " + item.CheckpointID
			}
			fmt.Fprintf(&b, "- [%s] %s", tag, item.Title)
			if len(item.Files) > 0 {
				fmt.Fprintf(&b, " (%s)", strings.Join(item.Files, ", "))
			}
			b.WriteByte('\n')
		}
	}
	return &Result{Text: b.String(), Structured: res}, nil
}

func checkpointRestore(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("checkpoint_restore: %w", err)
	}
	id := strings.TrimPrefix(strings.TrimSpace(args.ID), "cp:")
	if id == "" {
		return nil, fmt.Errorf("checkpoint_restore: id is required")
	}

	var res CheckpointRestoreResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		m, err := ed.CheckpointManifest(id)
		if err != nil {
			return err
		}
		if err := ed.RestoreCheckpoint(id); err != nil {
			return err
		}
		res.ID = m.ID
		res.Title = m.Title
		res.Files = make([]string, len(m.Files))
		for i, f := range m.Files {
			res.Files[i] = f.Path
		}
		res.Revision = ed.Revision()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("checkpoint_restore: %w", err)
	}

	text := fmt.Sprintf("Restored checkpoint %s (%q) across %d file(s) as one undoable step.", res.ID, res.Title, len(res.Files))
	return &Result{Text: text, Structured: res}, nil
}

func checkpointTitleForTool(t Tool) string {
	if t.Title != "" {
		return t.Title
	}
	return t.Name
}
