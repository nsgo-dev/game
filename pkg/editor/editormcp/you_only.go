package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor"
)

const (
	// ToolPermissionsDraft submits a You-only permissions draft card for the person to review and tick rule by rule.
	// It never modifies permissions.yaml or active rules on its own.
	ToolPermissionsDraft = "permissions_draft"
	// ToolGameDeleteFile proposes deleting a project file in the Review changes panel with its line count and scene usages.
	// It never deletes the file directly on disk, refuses You-only files, and Keep all never applies the deletion without per-file confirmation.
	ToolGameDeleteFile = "game_delete_file"
)

// YouOnlyError is returned when an agent attempts to write, rename, or delete a You-only settings file
// (permissions, tools, models, usage, or per-user editor settings) through any MCP tool.
type YouOnlyError struct {
	Tool   string               `json:"tool"`
	Domain editor.YouOnlyDomain `json:"domain"`
	Path   string               `json:"path"`
	Reason string               `json:"reason"`
	Hint   string               `json:"hint"`
}

// Error describes the protected path, its You-only domain, and how the person can make the requested change.
func (e *YouOnlyError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: refused (You only): %s is protected under %s and cannot be modified by the agent. %s",
		e.Tool, e.Path, e.Domain.DisplayName(), e.Hint)
}

const permissionsDraftSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["rules"],
  "properties": {
    "request": {"type": "string", "description": "Short summary of the person's request that prompted this draft, e.g. \"stop asking me before every scene edit\""},
    "rules": {
      "type": "array",
      "minItems": 1,
      "maxItems": 32,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["action", "patterns", "decision"],
        "properties": {
          "action": {"type": "string", "enum": ["edit", "create", "delete", "run", "code"]},
          "patterns": {"type": "array", "minItems": 1, "items": {"type": "string"}},
          "decision": {"type": "string", "enum": ["allow", "ask", "deny"]}
        }
      }
    }
  }
}`

const gameDeleteFileSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["file"],
  "properties": {
    "file": {"type": "string", "description": "Slash-separated path from the project root to propose deleting, such as prefabs/slime_old.prefab.yaml"},
    "reason": {"type": "string", "description": "Short explanation of why this file should be deleted, shown in the review header"}
  }
}`

func youOnlyTools() []Tool {
	return []Tool{
		{
			Name:        ToolPermissionsDraft,
			Title:       "Draft a permissions change",
			Description: "Submit a You-only draft change to permissions.yaml. Opens a draft card with nothing ticked by default; only the person can tick rules and click Apply. Never modifies permissions.yaml directly.",
			InputSchema: []byte(permissionsDraftSchema),
			Action:      ActionRead,
			Handler:     permissionsDraft,
		},
		{
			Name:        ToolGameDeleteFile,
			Title:       "Propose deleting a file",
			Description: "Propose deleting a project file in the Review changes panel, showing its line count and whether any scene uses it. Never deletes the file directly on disk; Keep all leaves the file until the person confirms the deletion in place, and ⌘Z restores it from the checkpoint. Turned off by default in Settings › Tools.",
			InputSchema: []byte(gameDeleteFileSchema),
			Action:      ActionDelete,
			DefaultOff:  true,
			Destructive: true,
			Handler:     gameDeleteFile,
		},
	}
}

// checkYouOnlyTargets enforces the You-only trust boundary across EVERY non-read tool
// (built-in or custom, regardless of active profile including Autonomous).
// It inspects:
//  1. Every explicit path-like string in args (extractCandidatePaths),
//  2. Every derived file target from reqs (covering bare-stem refactor_rename, release_notes_draft's ExportReleaseNotes, tileset_edit fallback, merge_finish, etc.), and
//  3. Implicit open document targets (ed.ScenePath, ed.FilePath, ed.TilesetPath()) unconditionally — never skipped when unrelated args like "path":"hp" are present.
func checkYouOnlyTargets(t Tool, args json.RawMessage, reqs []editor.PermissionRequest, ed *editor.Editor) *YouOnlyError {
	if ed == nil || t.Action == ActionRead || t.Name == ToolPermissionsDraft {
		return nil
	}
	makeErr := func(p string, dom editor.YouOnlyDomain) *YouOnlyError {
		reason := fmt.Sprintf("writes to %s are You only", dom.DisplayName())
		hint := "Submit a draft with permissions_draft instead; only the person can approve You-only changes."
		if t.Action == ActionDelete || t.Name == ToolGameDeleteFile {
			reason = fmt.Sprintf("deleting %s is You only", dom.DisplayName())
			hint = "You-only configuration files cannot be deleted by the agent."
		}
		return &YouOnlyError{
			Tool:   t.Name,
			Domain: dom,
			Path:   p,
			Reason: reason,
			Hint:   hint,
		}
	}

	// 1. Explicit argument paths.
	for _, p := range extractCandidatePaths(args) {
		if dom, ok := ed.CheckYouOnlyTarget(p); ok {
			return makeErr(p, dom)
		}
	}
	// 2. Derived file targets (including bare-stem refactor_rename destination, release_notes_draft file, tileset fallback, etc.).
	for _, req := range reqs {
		if req.Action == editor.PermissionActionRun || strings.TrimSpace(req.Target) == "" {
			continue
		}
		if dom, ok := ed.CheckYouOnlyTarget(req.Target); ok {
			return makeErr(req.Target, dom)
		}
	}
	// 3. Implicit open document targets (checked unconditionally so unrelated arg keys like "path":"hp" can never mask an open You-only file).
	for _, implicit := range []string{ed.ScenePath, ed.FilePath, ed.TilesetPath()} {
		if implicit == "" {
			continue
		}
		if dom, ok := ed.CheckYouOnlyTarget(implicit); ok {
			return makeErr(implicit, dom)
		}
	}
	return nil
}

// extractCandidatePaths walks args JSON and returns all string values under path-bearing keys
// as well as any string value that directly names a You-only file.
func extractCandidatePaths(args json.RawMessage) []string {
	if len(args) == 0 {
		return nil
	}
	var root any
	if err := json.Unmarshal(args, &root); err != nil {
		return nil
	}
	var out []string
	var walk func(key string, v any)
	walk = func(key string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				walk(strings.ToLower(k), val)
			}
		case []any:
			for _, elem := range x {
				walk(key, elem)
			}
		case string:
			s := strings.TrimSpace(x)
			if s == "" {
				return
			}
			switch key {
			case "file", "from", "to", "path", "out", "target", "config", "scene", "recordings", "files", "paths", "destination", "dest", "src", "source":
				out = append(out, s)
			default:
				if _, ok := editor.MatchYouOnlyPath(s); ok {
					out = append(out, s)
				}
			}
		}
	}
	walk("", root)
	return out
}

// PermissionsDraftResult is the structured result returned by permissions_draft.
type PermissionsDraftResult struct {
	Status      string                   `json:"status"`
	DraftID     string                   `json:"draftId"`
	Domain      editor.YouOnlyDomain     `json:"domain"`
	RuleCount   int                      `json:"ruleCount"`
	TickedCount int                      `json:"tickedCount"`
	Draft       *editor.PermissionsDraft `json:"draft"`
}

func permissionsDraft(ctx context.Context, c *Call) (*Result, error) {
	var args editor.PermissionsDraftRequest
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("permissions_draft: %w", err)
	}
	var draft *editor.PermissionsDraft
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		var serr error
		draft, serr = ed.SubmitPermissionsDraft(args)
		return serr
	})
	if err != nil {
		return nil, fmt.Errorf("permissions_draft: %w", err)
	}
	out := PermissionsDraftResult{
		Status:      "draft_opened",
		DraftID:     draft.ID,
		Domain:      draft.Domain,
		RuleCount:   len(draft.Rules),
		TickedCount: 0,
		Draft:       draft,
	}
	text := fmt.Sprintf("Opened You-only permissions draft (%d rule(s), 0 ticked). Nothing was changed; the person must tick each rule and click Apply.", len(draft.Rules))
	return &Result{Text: text, Structured: out}, nil
}

// GameDeleteFileResult is the structured result returned by game_delete_file.
type GameDeleteFileResult struct {
	Status         string   `json:"status"`
	File           string   `json:"file"`
	LineCount      int      `json:"lineCount"`
	UsedByAnyScene bool     `json:"usedByAnyScene"`
	SceneUsages    []string `json:"sceneUsages,omitempty"`
	UsageSummary   string   `json:"usageSummary"`
	ToolEnabled    bool     `json:"toolEnabled"`
	DeletedOnDisk  bool     `json:"deletedOnDisk"`
}

type gameDeleteFileArgs struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

func gameDeleteFile(ctx context.Context, c *Call) (*Result, error) {
	var args gameDeleteFileArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("game_delete_file: %w", err)
	}
	clean, err := cleanProjectPath(args.File)
	if err != nil {
		return nil, fmt.Errorf("game_delete_file: %w", err)
	}
	var (
		pd     *editor.ProposedDeletion
		toolOn bool
		yErr   *YouOnlyError
	)
	err = c.Editor(ctx, func(ed *editor.Editor) error {
		if dom, ok := ed.CheckYouOnlyTarget(clean); ok {
			yErr = &YouOnlyError{
				Tool:   ToolGameDeleteFile,
				Domain: dom,
				Path:   clean,
				Reason: fmt.Sprintf("deleting %s is You only", dom.DisplayName()),
				Hint:   "You-only configuration files cannot be deleted by the agent.",
			}
			return nil
		}
		toolOn = ed.ToolEnabled(ToolGameDeleteFile)
		var perr error
		pd, perr = ed.ProposeFileDeletion(clean, args.Reason)
		return perr
	})
	if err != nil {
		return nil, fmt.Errorf("game_delete_file: %w", err)
	}
	if yErr != nil {
		return nil, yErr
	}
	if pd == nil {
		return nil, errors.New("game_delete_file: failed to propose file deletion")
	}
	out := GameDeleteFileResult{
		Status:         "proposed_deletion",
		File:           pd.Path,
		LineCount:      pd.LineCount,
		UsedByAnyScene: pd.UsedByAnyScene,
		SceneUsages:    pd.SceneUsages,
		UsageSummary:   pd.UsageSummary,
		ToolEnabled:    toolOn,
		DeletedOnDisk:  false,
	}
	text := fmt.Sprintf("Proposed deletion of %s (%s). File left on disk; Keep all will not delete it until the person confirms in place, and ⌘Z restores it from the checkpoint.", pd.Path, pd.Detail)
	return &Result{Text: text, Structured: out}, nil
}
