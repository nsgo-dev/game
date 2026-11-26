package editormcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

// ToolReviewStatus reads the active or most recent agent turn review's seen and reverted change state.
const ToolReviewStatus = "review_status"

const (
	// maxReviewStatusTextChanges caps how many individual change lines review_status renders in its text reply.
	maxReviewStatusTextChanges = 200
	// maxReviewStatusYAMLLines caps YAML diff lines per change in the MCP structured snapshot.
	maxReviewStatusYAMLLines = 60
)

const reviewStatusSchema = `{"type":"object","additionalProperties":false,"properties":{}}`

func reviewTools() []Tool {
	return []Tool{
		{
			Name:        ToolReviewStatus,
			Title:       "Read agent turn review status",
			Description: "Return the current agent turn review state: whether it is open or pending, the request, file summaries, every change's seen and reverted flags, active blockers, and the last keep/revert outcome. Read-only: keeping or reverting remains the person's decision.",
			InputSchema: []byte(reviewStatusSchema),
			Action:      ActionRead,
			Handler:     reviewStatus,
		},
	}
}

func reviewStatus(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, err
	}
	var snap editor.ReviewSnapshot
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		snap = ed.ReviewStatus()
		return nil
	}); err != nil {
		return nil, err
	}

	for i := range snap.Changes {
		if len(snap.Changes[i].YAMLLines) > maxReviewStatusYAMLLines {
			snap.Changes[i].YAMLLines = snap.Changes[i].YAMLLines[:maxReviewStatusYAMLLines]
		}
	}

	var b strings.Builder
	switch snap.Outcome {
	case editor.ReviewOutcomePending:
		state := "pending (editor)"
		if snap.Open {
			state = "open"
		}
		fmt.Fprintf(&b, "Review %s: %d changes across %d files (%d seen, %d kept, %d reverted)",
			state, snap.TotalChanges, snap.FileCount, snap.SeenCount, snap.KeptCount, snap.RevertedCount)
		if snap.Request != "" {
			fmt.Fprintf(&b, "\nRequest: %q", snap.Request)
		}
		if len(snap.Blockers) > 0 {
			fmt.Fprintf(&b, "\nBlockers: %d", len(snap.Blockers))
			for _, bl := range snap.Blockers {
				fmt.Fprintf(&b, "\n  - %s: %s", bl.ChangeID, bl.Reason)
			}
		}
		limit := min(len(snap.Changes), maxReviewStatusTextChanges)
		for _, ch := range snap.Changes[:limit] {
			seenTag := "unseen"
			if ch.Seen {
				seenTag = "seen"
			}
			revTag := "kept"
			if ch.Reverted {
				revTag = "reverted"
			}
			fmt.Fprintf(&b, "\n- [%s] %s (%s) %s · %s, %s", ch.Kind, ch.Title, ch.File, ch.Detail, seenTag, revTag)
		}
		if len(snap.Changes) > limit {
			fmt.Fprintf(&b, "\n... and %d more changes", len(snap.Changes)-limit)
		}
	case editor.ReviewOutcomeKept, editor.ReviewOutcomeReverted:
		fmt.Fprintf(&b, "Review %s: %s", snap.Outcome, snap.LastOutcomeMsg)
	default:
		b.WriteString("No agent turn review is active.")
	}

	return &Result{
		Text:       b.String(),
		Structured: snap,
	}, nil
}
