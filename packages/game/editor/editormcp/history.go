package editormcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
)

// ToolHistoryList is the MCP tool name exposing recent History timeline entries to connected agents.
const ToolHistoryList = "history_list"

const historyListSchema = `{"type":"object","additionalProperties":false,"properties":{` +
	`"author":{"type":"string","enum":["all","you","agent"],"description":"Filter entries by who made them: all (default), you, or agent"},` +
	`"limit":{"type":"integer","minimum":1,"maximum":500,"description":"Maximum number of entries to return, newest first; 50 when absent"}` +
	`}}`

// HistoryEntryView is one entry returned by history_list.
type HistoryEntryView struct {
	ID           uint64   `json:"id"`
	Kind         string   `json:"kind"`
	Author       string   `json:"author,omitempty"`
	AuthorLabel  string   `json:"authorLabel,omitempty"`
	Summary      string   `json:"summary"`
	Detail       string   `json:"detail,omitempty"`
	Files        []string `json:"files,omitempty"`
	Timestamp    string   `json:"timestamp"`
	RelativeTime string   `json:"relativeTime"`
	ChangeCount  int      `json:"changeCount,omitempty"`
	KeptCount    int      `json:"keptCount,omitempty"`
	Undone       bool     `json:"undone,omitempty"`
}

// HistoryListResult is the structured payload returned by history_list.
type HistoryListResult struct {
	Scene        string             `json:"scene"`
	Unsaved      bool               `json:"unsaved"`
	PreviewingID uint64             `json:"previewingId,omitempty"`
	UndoIndex    int                `json:"undoIndex"`
	Total        int                `json:"total"`
	Entries      []HistoryEntryView `json:"entries"`
}

func historyTools() []Tool {
	return []Tool{
		{
			Name:        ToolHistoryList,
			Title:       "List recent history",
			Description: "Read the editor's unified History timeline of manual edits, agent turns, and saved points, newest first. Filter by author (all, you, or agent) and optionally limit the number of entries returned.",
			InputSchema: []byte(historyListSchema),
			Action:      ActionRead,
			Handler:     historyList,
		},
	}
}

func historyList(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Author string `json:"author"`
		Limit  int    `json:"limit"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("history_list: %w", err)
	}
	filter := editor.HistoryFilterAll
	switch strings.ToLower(strings.TrimSpace(args.Author)) {
	case "", "all":
		filter = editor.HistoryFilterAll
	case "you":
		filter = editor.HistoryFilterYou
	case "agent":
		filter = editor.HistoryFilterAgent
	default:
		return nil, fmt.Errorf("history_list: unknown author filter %q (use all, you, or agent)", args.Author)
	}
	limit := args.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 500 {
		return nil, fmt.Errorf("history_list: limit %d must be between 1 and 500", args.Limit)
	}

	var res HistoryListResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		root := projectRoot(ed)
		res.Scene = sceneName(ed, root)
		res.Unsaved = ed.HasUnsavedChanges()
		if ed.History != nil {
			res.UndoIndex = ed.History.Index()
		}
		if prev, ok := ed.HistoryPreviewEntry(); ok {
			res.PreviewingID = prev.ID
		}
		all := ed.FilteredHistoryEntries(filter)
		res.Total = len(all)
		if len(all) > limit {
			all = all[:limit]
		}
		res.Entries = make([]HistoryEntryView, 0, len(all))
		for _, entry := range all {
			res.Entries = append(res.Entries, HistoryEntryView{
				ID:           entry.ID,
				Kind:         string(entry.Kind),
				Author:       string(entry.Author),
				AuthorLabel:  entry.Author.Label(),
				Summary:      entry.Summary,
				Detail:       entry.Detail,
				Files:        entry.Files,
				Timestamp:    entry.Timestamp.Format(time.RFC3339),
				RelativeTime: ed.FormatHistoryRelativeTime(entry.Timestamp),
				ChangeCount:  entry.ChangeCount,
				KeptCount:    entry.KeptCount,
				Undone:       entry.Undone,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	status := "saved"
	if res.Unsaved {
		status = "unsaved"
	}
	fmt.Fprintf(&b, "History for %s (%s) · %d entries", res.Scene, status, res.Total)
	if len(res.Entries) < res.Total {
		fmt.Fprintf(&b, " (showing %d)", len(res.Entries))
	}
	if res.PreviewingID > 0 {
		fmt.Fprintf(&b, " · previewing #%d", res.PreviewingID)
	}
	b.WriteString(":\n")
	if len(res.Entries) == 0 {
		b.WriteString("No history entries.\n")
	} else {
		for _, e := range res.Entries {
			headline := e.Summary
			if e.Kind == string(editor.HistoryEntrySaved) {
				headline = "Saved"
			} else if e.AuthorLabel != "" {
				headline = e.AuthorLabel + " · " + e.Summary
			}
			var meta []string
			if e.Detail != "" {
				meta = append(meta, e.Detail)
			}
			if e.RelativeTime != "" {
				meta = append(meta, e.RelativeTime)
			}
			fmt.Fprintf(&b, "- #%d %s", e.ID, headline)
			if len(meta) > 0 {
				fmt.Fprintf(&b, " · %s", strings.Join(meta, " · "))
			}
			if len(e.Files) > 0 {
				fmt.Fprintf(&b, " [%s]", strings.Join(e.Files, ", "))
			}
			if e.Undone {
				b.WriteString(" (undone)")
			}
			b.WriteByte('\n')
		}
	}
	return &Result{Text: b.String(), Structured: res}, nil
}
