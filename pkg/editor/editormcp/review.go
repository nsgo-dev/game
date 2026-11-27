package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

const (
	// ToolReviewStatus reads the active or most recent agent turn review's seen, reverted, conflict, and hand-edited YAML state.
	ToolReviewStatus = "review_status"
	// ToolReviewNotesGet reads the active review's round state, changes, and sent notes waiting for the agent.
	ToolReviewNotesGet = "review_notes_get"
	// ToolReviewRedo redoes only changes that were noted by the author, advancing the review to the next round.
	ToolReviewRedo = "review_redo"
)

const (
	// maxReviewStatusTextChanges caps how many individual change lines review_status renders in its text reply.
	maxReviewStatusTextChanges = 200
	// maxReviewStatusYAMLLines caps YAML diff lines per change in the MCP structured snapshot.
	maxReviewStatusYAMLLines = 60
	// maxReviewStatusYAMLBytes caps HandEditYAML per change in the MCP structured snapshot.
	maxReviewStatusYAMLBytes = 16 << 10
	// maxReviewRedoItems caps how many change entries one review_redo call may submit.
	maxReviewRedoItems = 256
)

const reviewStatusSchema = `{"type":"object","additionalProperties":false,"properties":{}}`

const reviewNotesGetSchema = `{"type":"object","additionalProperties":false,"properties":{}}`

const reviewRedoSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["changes"],
  "properties": {
    "changes": {
      "type": "array",
      "minItems": 1,
      "maxItems": 256,
      "description": "Noted changes to redo for the next round of the active review. Un-noted changes are refused.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["change_id"],
        "properties": {
          "change_id": {"type": "string", "description": "Change ID from review_notes_get.sent_notes"},
          "detail": {"type": "string", "description": "Updated short detail shown on the change card in the next round"},
          "x": {"type": "number", "description": "Updated X coordinate for a 2D scene object"},
          "y": {"type": "number", "description": "Updated Y coordinate for a 2D scene object"},
          "component": {"type": "string", "description": "Optional component name whose fields are updated"},
          "fields": {"type": "object", "description": "Updated object or component fields"},
          "file_content": {"type": "string", "description": "Optional full updated YAML or text file content for this redone change (un-noted items in the same file are never modified)"},
          "old_value": {"type": "string", "description": "Optional old value label for prefab or data changes"},
          "new_value": {"type": "string", "description": "Optional new value label for prefab or data changes"},
          "delta": {"type": "string", "description": "Optional delta label (for example -25%)"},
          "yaml_lines": {
            "type": "array",
            "description": "Optional diff lines shown in the Review screen's right YAML pane",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["line", "text"],
              "properties": {
                "line": {"type": "integer", "minimum": 1},
                "sign": {"type": "string", "enum": ["", "+", "-"]},
                "text": {"type": "string"},
                "tag": {"type": "string"}
              }
            }
          }
        }
      }
    }
  }
}`

func reviewTools() []Tool {
	return []Tool{
		{
			Name:        ToolReviewStatus,
			Title:       "Read agent turn review status",
			Description: "Return the current or most recent agent turn review state: whether it is open or pending, the request, round state, file summaries, every change's seen and reverted flags, overlapping edit conflicts and the person's merge choice (mine, agent, both), in-review YAML hand edits tagged \"you\" with edited fields and lines, active blockers (including YAML parse errors), and the last keep/revert outcome. Unsent draft notes are never exposed, and privacy policies are enforced. Read-only: keeping or reverting remains the person's decision.",
			InputSchema: []byte(reviewStatusSchema),
			Action:      ActionRead,
			Handler:     reviewStatus,
		},
		{
			Name:        ToolReviewNotesGet,
			Title:       "Read review notes and status",
			Description: "Read the active agent review's round state, changes (with seen, reverted, and redone status), and the change-scoped notes the author explicitly sent via 'Send N notes' or 'Revert and redo with this note'. Each sent note names its change_id, file, object, field, kind, and note text. Unsent draft notes and notes removed before sending are never returned.",
			InputSchema: []byte(reviewNotesGetSchema),
			Action:      ActionRead,
			Handler:     reviewNotesGet,
		},
		{
			Name:        ToolReviewRedo,
			Title:       "Redo noted review changes",
			Description: "Redo only changes the author left notes on in the active review, advancing the same turn to the next round (Round 2+). Earlier rounds remain intact and read-only; un-noted changes cannot be modified and are refused. Redone changes appear unseen again with 'Redone from your note: …' as their Why, and Keep still applies the latest round as one undo step.",
			InputSchema: []byte(reviewRedoSchema),
			Action:      ActionEdit,
			targets:     reviewRedoTargets,
			Handler:     reviewRedo,
		},
	}
}

func reviewRedoTargets(raw json.RawMessage, ed *editor.Editor) []editor.PermissionRequest {
	if ed == nil {
		return nil
	}
	snap := ed.ReviewStatus()
	if !snap.Active {
		return nil
	}
	var args reviewRedoArgs
	if err := json.Unmarshal(raw, &args); err != nil || len(args.Changes) == 0 {
		return nil
	}
	fileByID := make(map[string]string, len(snap.Changes))
	for _, ch := range snap.Changes {
		fileByID[ch.ID] = ch.File
	}
	seen := make(map[string]struct{}, len(args.Changes))
	var reqs []editor.PermissionRequest
	for _, item := range args.Changes {
		f := strings.TrimSpace(fileByID[item.ChangeID])
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		reqs = append(reqs, editor.PermissionRequest{
			Action: editor.PermissionActionEdit,
			Target: f,
		})
	}
	return reqs
}

func reviewStatus(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, err
	}
	var (
		snap   editor.ReviewSnapshot
		policy editor.AgentPrivacyPolicy
		root   string
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		snap = ed.ReviewStatus()
		policy = ed.AgentPrivacy()
		root = projectRoot(ed)
		return nil
	}); err != nil {
		return nil, err
	}

	// Never leak unsent draft notes over MCP; also redact diff/hand-edit lines for files blocked by privacy (e.g. GoSource off).
	for i := range snap.Changes {
		snap.Changes[i].Note = ""
		if checkPathPrivacy(policy, root, snap.Changes[i].File) != nil {
			snap.Changes[i].YAMLLines = nil
			snap.Changes[i].HandEditLines = nil
			snap.Changes[i].HandEditYAML = ""
			snap.Changes[i].Conflicts = nil
			for j := range snap.Changes[i].Versions {
				snap.Changes[i].Versions[j].YAMLLines = nil
			}
			continue
		}
		if len(snap.Changes[i].YAMLLines) > maxReviewStatusYAMLLines {
			snap.Changes[i].YAMLLines = snap.Changes[i].YAMLLines[:maxReviewStatusYAMLLines]
		}
		for j := range snap.Changes[i].Versions {
			if len(snap.Changes[i].Versions[j].YAMLLines) > maxReviewStatusYAMLLines {
				snap.Changes[i].Versions[j].YAMLLines = snap.Changes[i].Versions[j].YAMLLines[:maxReviewStatusYAMLLines]
			}
		}
		if len(snap.Changes[i].HandEditLines) > maxReviewStatusYAMLLines {
			snap.Changes[i].HandEditLines = snap.Changes[i].HandEditLines[:maxReviewStatusYAMLLines]
		}
		if len(snap.Changes[i].HandEditYAML) > maxReviewStatusYAMLBytes {
			snap.Changes[i].HandEditYAML = snap.Changes[i].HandEditYAML[:maxReviewStatusYAMLBytes]
		}
	}

	writeChangeSummary := func(b *strings.Builder, changes []editor.ReviewChange) {
		limit := min(len(changes), maxReviewStatusTextChanges)
		for _, ch := range changes[:limit] {
			seenTag := "unseen"
			if ch.Seen {
				seenTag = "seen"
			}
			revTag := "kept"
			if ch.Reverted {
				revTag = "reverted"
			}
			fmt.Fprintf(b, "\n- [%s] %s (%s) %s · %s, %s", ch.Kind, ch.Title, ch.File, ch.Detail, seenTag, revTag)
			if len(ch.Conflicts) > 0 {
				choice := string(ch.ConflictChoice)
				if choice == "" {
					choice = "unresolved"
				}
				fmt.Fprintf(b, ", conflict=%s", choice)
			}
			if ch.HandEdited {
				by := ch.HandEditBy
				if by == "" {
					by = editor.ReviewAuthorYou
				}
				fmt.Fprintf(b, ", hand-edited by %s", by)
				if len(ch.HandEditFields) > 0 {
					fmt.Fprintf(b, " [%s]", strings.Join(ch.HandEditFields, ", "))
				}
			}
			if ch.YAMLError != nil {
				fmt.Fprintf(b, ", yaml error on line %d", ch.YAMLError.Line)
			}
		}
		if len(changes) > limit {
			fmt.Fprintf(b, "\n... and %d more changes", len(changes)-limit)
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
		if snap.Round > 1 {
			fmt.Fprintf(&b, " · round %d (viewing round %d)", snap.Round, snap.ViewRound)
		}
		if snap.Request != "" {
			fmt.Fprintf(&b, "\nRequest: %q", snap.Request)
		}
		if len(snap.Blockers) > 0 {
			fmt.Fprintf(&b, "\nBlockers: %d", len(snap.Blockers))
			for _, bl := range snap.Blockers {
				fmt.Fprintf(&b, "\n  - %s: %s", bl.ChangeID, bl.Reason)
			}
		}
		writeChangeSummary(&b, snap.Changes)
	case editor.ReviewOutcomeKept:
		fmt.Fprintf(&b, "Review %s: %s", snap.Outcome, snap.LastOutcomeMsg)
		writeChangeSummary(&b, snap.Changes)
	case editor.ReviewOutcomeReverted:
		fmt.Fprintf(&b, "Review %s: %s", snap.Outcome, snap.LastOutcomeMsg)
	default:
		b.WriteString("No agent turn review is active.")
	}

	return &Result{
		Text:       b.String(),
		Structured: snap,
	}, nil
}

// ReviewChangeSummary is one change entry reported by review_notes_get.
type ReviewChangeSummary struct {
	ID            string                  `json:"id"`
	File          string                  `json:"file"`
	FileKind      editor.ReviewFileKind   `json:"file_kind"`
	Kind          editor.ReviewChangeKind `json:"kind"`
	Object        string                  `json:"object,omitempty"`
	Field         string                  `json:"field,omitempty"`
	Title         string                  `json:"title"`
	Detail        string                  `json:"detail"`
	Why           string                  `json:"why,omitempty"`
	Seen          bool                    `json:"seen"`
	Reverted      bool                    `json:"reverted"`
	RedoneInRound int                     `json:"redone_in_round,omitempty"`
}

// ReviewNotesStatusResult is the structured result returned by review_notes_get.
type ReviewNotesStatusResult struct {
	Active            bool                     `json:"active"`
	Open              bool                     `json:"open"`
	Paused            bool                     `json:"paused"`
	Request           string                   `json:"request,omitempty"`
	Round             int                      `json:"round"`
	ViewRound         int                      `json:"view_round"`
	DraftNoteCount    int                      `json:"draft_note_count"`
	PendingNotesRound int                      `json:"pending_notes_round,omitempty"`
	SentNotes         []editor.ReviewAgentNote `json:"sent_notes,omitempty"`
	Changes           []ReviewChangeSummary    `json:"changes,omitempty"`
}

// ReviewRedoResult is the structured result returned by review_redo.
type ReviewRedoResult struct {
	Round         int      `json:"round"`
	RedoneIDs     []string `json:"redone_ids"`
	RoundBanner   string   `json:"round_banner"`
	PreservedPrev bool     `json:"preserved_previous_round"`
}

func reviewNotesGet(ctx context.Context, c *Call) (*Result, error) {
	var empty struct{}
	if err := c.Decode(&empty); err != nil {
		return nil, fmt.Errorf("review_notes_get: %w", err)
	}
	var out ReviewNotesStatusResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		snap := ed.ReviewStatus()
		if !snap.Active {
			out = ReviewNotesStatusResult{Active: false}
			return nil
		}
		out = ReviewNotesStatusResult{
			Active:         true,
			Open:           snap.Open,
			Paused:         !snap.Open,
			Request:        snap.Request,
			Round:          snap.Round,
			ViewRound:      snap.ViewRound,
			DraftNoteCount: snap.DraftNoteCount,
			Changes:        make([]ReviewChangeSummary, 0, len(snap.Changes)),
		}
		if snap.PendingBatch != nil {
			out.PendingNotesRound = snap.PendingBatch.Round
			out.SentNotes = snap.PendingBatch.Notes
		}
		for _, ch := range snap.Changes {
			ver := ch.VersionFor(snap.ViewRound)
			obj := ch.ObjectID
			if obj == "" {
				obj = ch.RowID
			}
			if obj == "" {
				obj = ch.Title
			}
			out.Changes = append(out.Changes, ReviewChangeSummary{
				ID:            ch.ID,
				File:          ch.File,
				FileKind:      ch.FileKind,
				Kind:          ch.Kind,
				Object:        obj,
				Field:         ch.Field,
				Title:         ch.Title,
				Detail:        ver.Detail,
				Why:           ver.Why,
				Seen:          ch.Seen,
				Reverted:      ch.Reverted,
				RedoneInRound: ch.RedoneInRound,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("review_notes_get: %w", err)
	}
	if !out.Active {
		return &Result{Text: "No agent review is active.", Structured: out}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Review round %d (viewing round %d) · %d changes · %d sent notes",
		out.Round, out.ViewRound, len(out.Changes), len(out.SentNotes))
	if out.Paused {
		b.WriteString(" · paused")
	}
	for _, n := range out.SentNotes {
		fmt.Fprintf(&b, "\n- [%s] %s :: %s", n.ChangeID, n.File, n.Object)
		if n.Field != "" {
			fmt.Fprintf(&b, ".%s", n.Field)
		}
		if n.RevertFirst {
			b.WriteString(" (revert and redo)")
		}
		fmt.Fprintf(&b, ": %s", n.Note)
	}
	return &Result{Text: b.String(), Structured: out}, nil
}

type reviewRedoArgs struct {
	Changes []reviewRedoItemArgs `json:"changes"`
}

type reviewRedoItemArgs struct {
	ChangeID    string                  `json:"change_id"`
	Detail      string                  `json:"detail"`
	X           *float32                `json:"x"`
	Y           *float32                `json:"y"`
	Component   string                  `json:"component"`
	Fields      map[string]any          `json:"fields"`
	FileContent *string                 `json:"file_content"`
	OldValue    string                  `json:"old_value"`
	NewValue    string                  `json:"new_value"`
	Delta       string                  `json:"delta"`
	YAMLLines   []editor.ReviewYAMLLine `json:"yaml_lines"`
}

func reviewRedo(ctx context.Context, c *Call) (*Result, error) {
	var args reviewRedoArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("review_redo: %w", err)
	}
	if len(args.Changes) == 0 {
		return nil, errors.New("review_redo: changes must contain at least one noted change")
	}
	if len(args.Changes) > maxReviewRedoItems {
		return nil, fmt.Errorf("review_redo: too many changes (%d, max %d)", len(args.Changes), maxReviewRedoItems)
	}

	var res ReviewRedoResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		snap := ed.ReviewStatus()
		if !snap.Active {
			return editor.ErrNoReview
		}
		policy := ed.AgentPrivacy()
		root := projectRoot(ed)
		byID := make(map[string]editor.ReviewChange, len(snap.Changes))
		for _, ch := range snap.Changes {
			byID[ch.ID] = ch
		}

		redos := make([]editor.ReviewRedoChange, 0, len(args.Changes))
		redoneIDs := make([]string, 0, len(args.Changes))
		for _, item := range args.Changes {
			if ch, ok := byID[item.ChangeID]; ok {
				if perr := checkPathPrivacy(policy, root, ch.File); perr != nil {
					return perr
				}
			}
			rc := editor.ReviewRedoChange{
				ChangeID:  item.ChangeID,
				Detail:    item.Detail,
				X:         item.X,
				Y:         item.Y,
				OldValue:  item.OldValue,
				NewValue:  item.NewValue,
				DeltaPct:  item.Delta,
				YAMLLines: item.YAMLLines,
			}
			if item.FileContent != nil {
				rc.FileContent = []byte(*item.FileContent)
			}
			if item.X != nil || item.Y != nil || len(item.Fields) > 0 {
				if obj, layer, ok := ed.ReviewObjectDocForChange(item.ChangeID, snap.Round); ok && obj != nil {
					if item.X != nil {
						obj.X = *item.X
					}
					if item.Y != nil {
						obj.Y = *item.Y
					}
					if len(item.Fields) > 0 {
						applyReviewRedoFields(obj, item.Component, item.Fields)
					}
					rc.ObjectDoc = obj
					rc.LayerName = layer
				}
			}
			redos = append(redos, rc)
			redoneIDs = append(redoneIDs, item.ChangeID)
		}

		if err := ed.ApplyReviewRedo(redos); err != nil {
			return err
		}
		after := ed.ReviewStatus()
		res = ReviewRedoResult{
			Round:         after.Round,
			RedoneIDs:     redoneIDs,
			RoundBanner:   after.RoundBanner,
			PreservedPrev: true,
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("review_redo: %w", err)
	}
	return &Result{
		Text:       fmt.Sprintf("Applied round %d redo for %d noted change(s): %s. %s", res.Round, len(res.RedoneIDs), strings.Join(res.RedoneIDs, ", "), res.RoundBanner),
		Structured: res,
	}, nil
}

func applyReviewRedoFields(obj *sceneformat.ObjectDoc, component string, fields map[string]any) {
	if obj == nil || len(fields) == 0 {
		return
	}
	if strings.TrimSpace(component) != "" {
		if obj.Components == nil {
			obj.Components = map[string]any{}
		}
		compMap, ok := obj.Components[component].(map[string]any)
		if !ok || compMap == nil {
			compMap = map[string]any{}
		}
		maps.Copy(compMap, fields)
		obj.Components[component] = compMap
		return
	}
	for k, v := range fields {
		switch k {
		case "name":
			if s, ok := v.(string); ok {
				obj.Name = s
			}
		case "x":
			if f, ok := toFloat32(v); ok {
				obj.X = f
			}
		case "y":
			if f, ok := toFloat32(v); ok {
				obj.Y = f
			}
		case "z":
			if f, ok := toFloat32(v); ok {
				obj.Z = int(f)
			}
		case "rotation":
			if f, ok := toFloat32(v); ok {
				obj.Rotation = f
			}
		default:
			if obj.Properties == nil {
				obj.Properties = map[string]any{}
			}
			obj.Properties[k] = v
		}
	}
}

func toFloat32(v any) (float32, bool) {
	switch n := v.(type) {
	case float64:
		return float32(n), true
	case float32:
		return n, true
	case int:
		return float32(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return float32(f), true
		}
	}
	return 0, false
}
