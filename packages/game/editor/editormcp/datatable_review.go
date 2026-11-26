package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
)

const (
	maxDataTableReviewFiles     = 64
	maxDataTableReviewChecks    = 64
	maxDataTableReviewYAMLBytes = 4 << 20 // 4 MiB per data table file
)

// The names of the Data Table review tools added to the editor's MCP server.
const (
	// ToolDataTableReviewPropose validates proposed YAML data table changes and opens the row-by-row Data Table review without modifying disk until kept.
	ToolDataTableReviewPropose = "datatable_review_propose"
	// ToolDataTableReviewChecks attaches or refreshes named agent check figures on the active Data Table review.
	ToolDataTableReviewChecks = "datatable_review_checks"
	// ToolDataTableReviewStatus reports the active Data Table review state, per-record kept/reverted/user-edited status, and whether agent checks are out of date.
	ToolDataTableReviewStatus = "datatable_review_status"
)

const dtReviewProposeSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["files"],
  "properties": {
    "prompt": {"type": "string", "description": "Short summary of the request that produced these data table changes, shown in the review header"},
    "files": {
      "type": "array",
      "minItems": 1,
      "description": "One or more YAML data table files in this turn (existing modified tables or new empty/populated tables)",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["file", "yaml"],
        "properties": {
          "file": {"type": "string", "description": "Slash-separated path inside the project root, such as data/items.yaml"},
          "yaml": {"type": "string", "description": "Proposed version-1 YAML data table source validated against datatable.ParseDocument"}
        }
      }
    },
    "checks": {
      "type": "array",
      "description": "Optional named check figures attached by the agent to verify the request goal",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["label", "value"],
        "properties": {
          "label": {"type": "string", "description": "Short label of the metric or check, e.g. 'Over 12 min, floors 1–3'"},
          "value": {"type": "string", "description": "Measured value after the proposed changes, e.g. 'none'"},
          "before": {"type": "string", "description": "Optional value before this request, e.g. '9'"},
          "basis": {"type": "string", "description": "Optional methodology or playtest basis, e.g. 'Coin rate from 20 playtests of floor 3: 30 coins a minute.'"}
        }
      }
    }
  }
}`

const dtReviewChecksSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["checks"],
  "properties": {
    "checks": {
      "type": "array",
      "description": "Refreshed named check figures for the active data table review (clears checksOutOfDate)",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["label", "value"],
        "properties": {
          "label": {"type": "string", "description": "Short label of the metric or check"},
          "value": {"type": "string", "description": "Measured value against the current kept/edited records"},
          "before": {"type": "string", "description": "Optional value before this request"},
          "basis": {"type": "string", "description": "Optional methodology or playtest basis"}
        }
      }
    }
  }
}`

const dtReviewStatusSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "file": {"type": "string", "description": "Optional file path in the active review to focus before reporting status"}
  }
}`

func dataTableReviewTools() []Tool {
	return []Tool{
		{
			Name:        ToolDataTableReviewPropose,
			Title:       "Propose data table changes for review",
			Description: "Validate one or more version-1 YAML data table files against datatable.ParseDocument (and any registered Go row type) and open the row-by-row Data Table review screen with old → new, Δ%, per-record Revert/Restore, and optional named agent check figures, without modifying disk until Keep.",
			InputSchema: []byte(dtReviewProposeSchema),
			Action:      ActionEdit,
			targets:     dtReviewProposeTargets,
			Handler:     dtReviewPropose,
		},
		{
			Name:        ToolDataTableReviewChecks,
			Title:       "Attach or refresh agent check figures",
			Description: "Attach or refresh the agent's named check figures (label, value, before, basis) on the active Data Table review turn and clear the out-of-date flag after the person reverts, edits, or reorders records.",
			InputSchema: []byte(dtReviewChecksSchema),
			Action:      ActionEdit,
			targets:     dtReviewChecksTargets,
			Handler:     dtReviewSetChecks,
		},
		{
			Name:        ToolDataTableReviewStatus,
			Title:       "Inspect active data table review status",
			Description: "Return the active Data Table review's files, kept/reverted/user-edited record counts, current check figures, and whether the agent's checks are out of date.",
			InputSchema: []byte(dtReviewStatusSchema),
			Action:      ActionRead,
			Handler:     dtReviewStatus,
		},
	}
}

func dtReviewProposeTargets(args json.RawMessage, ed *editor.Editor) []editor.PermissionRequest {
	var in struct {
		Files []struct {
			File string `json:"file"`
		} `json:"files"`
	}
	_ = json.Unmarshal(args, &in)
	if len(in.Files) == 0 {
		return nil
	}
	root := ""
	if ed != nil {
		root = projectRoot(ed)
	}
	reqs := make([]editor.PermissionRequest, 0, len(in.Files))
	for _, f := range in.Files {
		raw := strings.TrimSpace(f.File)
		target := raw
		if raw != "" && root != "" {
			if resolved, err := editor.ResolvePermissionFileTarget(root, raw); err == nil && resolved != "" {
				target = resolved
			}
		}
		// datatable_review_propose never writes to disk until the person clicks Keep,
		// so every target evaluates as PermissionActionEdit (matching cutscene_write review mode).
		reqs = append(reqs, editor.PermissionRequest{
			Tool:   ToolDataTableReviewPropose,
			Action: editor.PermissionActionEdit,
			Target: target,
		})
	}
	return reqs
}

func dtReviewChecksTargets(_ json.RawMessage, ed *editor.Editor) []editor.PermissionRequest {
	if ed == nil {
		return nil
	}
	files := ed.DataTableReviewFiles()
	if len(files) == 0 {
		return nil
	}
	reqs := make([]editor.PermissionRequest, 0, len(files))
	for _, f := range files {
		if strings.TrimSpace(f.RelPath) == "" {
			continue
		}
		reqs = append(reqs, editor.PermissionRequest{
			Tool:   ToolDataTableReviewChecks,
			Action: editor.PermissionActionEdit,
			Target: f.RelPath,
		})
	}
	return reqs
}

type dtReviewProposeFileArg struct {
	File string `json:"file"`
	YAML string `json:"yaml"`
}

type dtReviewProposeArgs struct {
	Prompt string                        `json:"prompt"`
	Files  []dtReviewProposeFileArg      `json:"files"`
	Checks []editor.DataTableReviewCheck `json:"checks"`
}

type dtReviewChecksArgs struct {
	Checks []editor.DataTableReviewCheck `json:"checks"`
}

type dtReviewStatusArgs struct {
	File string `json:"file"`
}

// DataTableReviewRecordStatus summarizes one changed, reverted, or user-edited record for MCP callers.
type DataTableReviewRecordStatus struct {
	Key           string `json:"key"`
	DisplayName   string `json:"displayName,omitempty"`
	Changed       bool   `json:"changed,omitempty"`
	Reverted      bool   `json:"reverted,omitempty"`
	UserEdited    bool   `json:"userEdited,omitempty"`
	UserReordered bool   `json:"userReordered,omitempty"`
}

// DataTableReviewStatusResult is returned by datatable_review_propose, datatable_review_checks, and datatable_review_status.
type DataTableReviewStatusResult struct {
	editor.DataTableReviewSnapshot
	ActiveRecords []DataTableReviewRecordStatus `json:"activeRecords,omitempty"`
}

func buildDataTableReviewStatusResult(ed *editor.Editor) DataTableReviewStatusResult {
	snap := ed.DataTableReviewSnapshot()
	res := DataTableReviewStatusResult{DataTableReviewSnapshot: snap}
	if !ed.DataTableReviewPending() {
		return res
	}
	// Report all changed, reverted, or user-edited records in the active file regardless of UI filter/search.
	for _, r := range ed.DataTableReviewAllRecords() {
		if r.Changed || r.Reverted || r.UserEdited || r.UserReordered {
			res.ActiveRecords = append(res.ActiveRecords, DataTableReviewRecordStatus{
				Key:           r.Key,
				DisplayName:   r.DisplayName,
				Changed:       r.Changed,
				Reverted:      r.Reverted,
				UserEdited:    r.UserEdited,
				UserReordered: r.UserReordered,
			})
		}
	}
	return res
}

func dtReviewPathKey(p string) string {
	clean := filepath.Clean(p)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

func dtReviewPropose(ctx context.Context, c *Call) (*Result, error) {
	var args dtReviewProposeArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("%s: %w", ToolDataTableReviewPropose, err)
	}
	if len(args.Files) == 0 {
		return nil, fmt.Errorf("%s: files must contain at least one data table", ToolDataTableReviewPropose)
	}
	if len(args.Files) > maxDataTableReviewFiles {
		return nil, fmt.Errorf("%s: too many files (%d, max %d)", ToolDataTableReviewPropose, len(args.Files), maxDataTableReviewFiles)
	}
	if len(args.Checks) > maxDataTableReviewChecks {
		return nil, fmt.Errorf("%s: too many checks (%d, max %d)", ToolDataTableReviewPropose, len(args.Checks), maxDataTableReviewChecks)
	}

	type parsedProposal struct {
		clean string
		doc   *datatable.Document
	}
	parsed := make([]parsedProposal, 0, len(args.Files))
	seen := make(map[string]struct{}, len(args.Files))
	for _, f := range args.Files {
		clean, err := cleanProjectPath(f.File)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ToolDataTableReviewPropose, err)
		}
		key := dtReviewPathKey(clean)
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s: duplicate file %q in review", ToolDataTableReviewPropose, clean)
		}
		seen[key] = struct{}{}
		if len(f.YAML) > maxDataTableReviewYAMLBytes {
			return nil, fmt.Errorf("%s: %s: yaml is %d bytes (max %d)", ToolDataTableReviewPropose, clean, len(f.YAML), maxDataTableReviewYAMLBytes)
		}
		if strings.TrimSpace(f.YAML) == "" {
			return nil, fmt.Errorf("%s: %s: yaml must not be empty", ToolDataTableReviewPropose, clean)
		}
		doc, err := datatable.ParseDocument(clean, []byte(f.YAML))
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", ToolDataTableReviewPropose, clean, err)
		}
		parsed = append(parsed, parsedProposal{clean: clean, doc: doc})
	}

	var res DataTableReviewStatusResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Playing() {
			return fmt.Errorf("%s: %w", ToolDataTableReviewPropose, editor.ErrPlaying)
		}
		if ed.HistoryPreviewActive() {
			return fmt.Errorf("%s: %w", ToolDataTableReviewPropose, editor.ErrHistoryPreview)
		}
		root := projectRoot(ed)
		if root == "" {
			return fmt.Errorf("%s: no project is open in the editor; open one with go tool game editor <project-dir>", ToolDataTableReviewPropose)
		}

		specs := make([]editor.DataTableReviewFileSpec, 0, len(parsed))
		seenAbs := make(map[string]struct{}, len(parsed))
		for _, p := range parsed {
			abs, exists, err := resolveProjectTargetPath(root, p.clean)
			if err != nil {
				return fmt.Errorf("%s: %w", ToolDataTableReviewPropose, err)
			}
			absKey := dtReviewPathKey(abs)
			if _, dup := seenAbs[absKey]; dup {
				return fmt.Errorf("%s: duplicate file %q in review", ToolDataTableReviewPropose, p.clean)
			}
			seenAbs[absKey] = struct{}{}
			var baseDoc *datatable.Document
			if exists {
				st, serr := os.Stat(abs)
				if serr != nil {
					return fmt.Errorf("%s: %s: %w", ToolDataTableReviewPropose, p.clean, serr)
				}
				if !st.Mode().IsRegular() {
					return fmt.Errorf("%s: %s: not a regular file", ToolDataTableReviewPropose, p.clean)
				}
				if st.Size() > maxDataTableReviewYAMLBytes {
					return fmt.Errorf("%s: %s: file is %d bytes (max %d)", ToolDataTableReviewPropose, p.clean, st.Size(), maxDataTableReviewYAMLBytes)
				}
				raw, rerr := os.ReadFile(abs)
				if rerr != nil {
					return fmt.Errorf("%s: %s: %w", ToolDataTableReviewPropose, p.clean, rerr)
				}
				baseDoc, rerr = datatable.ParseDocument(p.clean, raw)
				if rerr != nil {
					return fmt.Errorf("%s: existing %s: %w", ToolDataTableReviewPropose, p.clean, rerr)
				}
			}
			specs = append(specs, editor.DataTableReviewFileSpec{
				Path:     abs,
				RelPath:  p.clean,
				Base:     baseDoc,
				Proposed: p.doc,
				IsNew:    !exists,
			})
		}

		if err := ed.OpenDataTableReview(editor.DataTableReviewRequest{
			Prompt: args.Prompt,
			Files:  specs,
			Checks: args.Checks,
		}); err != nil {
			return fmt.Errorf("%s: %w", ToolDataTableReviewPropose, err)
		}
		res = buildDataTableReviewStatusResult(ed)
		return nil
	}); err != nil {
		return nil, err
	}

	text := fmt.Sprintf("Opened Data Table review (%d files, %d of %d changed kept, %d checks).",
		len(res.Files), res.KeptCount, res.TotalChanged, len(res.Checks))
	return &Result{Text: text, Structured: res}, nil
}

func dtReviewSetChecks(ctx context.Context, c *Call) (*Result, error) {
	var args dtReviewChecksArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("%s: %w", ToolDataTableReviewChecks, err)
	}
	if len(args.Checks) > maxDataTableReviewChecks {
		return nil, fmt.Errorf("%s: too many checks (%d, max %d)", ToolDataTableReviewChecks, len(args.Checks), maxDataTableReviewChecks)
	}
	var res DataTableReviewStatusResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Playing() {
			return fmt.Errorf("%s: %w", ToolDataTableReviewChecks, editor.ErrPlaying)
		}
		if ed.HistoryPreviewActive() {
			return fmt.Errorf("%s: %w", ToolDataTableReviewChecks, editor.ErrHistoryPreview)
		}
		if !ed.DataTableReviewPending() {
			return errors.New("datatable_review_checks: no data table review is active")
		}
		if err := ed.SetDataTableReviewChecks(args.Checks); err != nil {
			return fmt.Errorf("%s: %w", ToolDataTableReviewChecks, err)
		}
		res = buildDataTableReviewStatusResult(ed)
		return nil
	}); err != nil {
		return nil, err
	}
	text := fmt.Sprintf("Updated %d agent check figure(s) on the active Data Table review (out of date: false).", len(res.Checks))
	return &Result{Text: text, Structured: res}, nil
}

func dtReviewStatus(ctx context.Context, c *Call) (*Result, error) {
	var args dtReviewStatusArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("%s: %w", ToolDataTableReviewStatus, err)
	}
	var res DataTableReviewStatusResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if !ed.DataTableReviewPending() {
			res = DataTableReviewStatusResult{}
			return nil
		}
		if strings.TrimSpace(args.File) != "" {
			if err := ed.SelectDataTableReviewFileByPath(args.File); err != nil {
				return fmt.Errorf("%s: %w", ToolDataTableReviewStatus, err)
			}
		}
		res = buildDataTableReviewStatusResult(ed)
		return nil
	}); err != nil {
		return nil, err
	}
	if !res.Open && !res.Paused {
		return &Result{Text: "No Data Table review is currently open.", Structured: res}, nil
	}
	state := "open"
	if res.Paused {
		state = "paused"
	}
	text := fmt.Sprintf("Data Table review (%s): %d of %d changed kept, %d user edits, checksOutOfDate=%v.",
		state, res.KeptCount, res.TotalChanged, res.UserEditedCount, res.ChecksOutOfDate)
	return &Result{Text: text, Structured: res}, nil
}
