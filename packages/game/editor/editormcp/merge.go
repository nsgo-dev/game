package editormcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

const (
	// ToolMergeStatus inspects the active scene merge conflict session or detects unmerged scene conflicts in Git.
	ToolMergeStatus = "merge_status"
	// ToolMergeResolve resolves a conflicted scene object (Mine, Theirs, Agent, custom YAML, or all Agent) or updates the agent's suggestion.
	ToolMergeResolve = "merge_resolve"
	// ToolMergeFinish writes the resolved scene, runs game check and a short playtest, and creates the Git merge commit.
	ToolMergeFinish = "merge_finish"
	// ToolMergeAbort aborts the active Git merge and restores the branch as it was.
	ToolMergeAbort = "merge_abort"
)

const mergeStatusSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "open": {"type": "boolean", "description": "When true and no merge session is open yet, open the first conflicted scene detected in Git"},
    "file": {"type": "string", "description": "Optional slash-separated scene path to open when open is true"}
  }
}`

const mergeResolveSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "id": {"type": "string", "description": "Conflict item ID (such as door, slime_02, knight). Required unless allAgent is true"},
    "choice": {"type": "string", "enum": ["agent", "mine", "theirs", "custom", "unresolved"], "description": "Resolution choice to apply to id"},
    "yaml": {"type": "string", "description": "Custom object YAML when choice is custom"},
    "allAgent": {"type": "boolean", "description": "When true, take the agent's suggestion for every conflict"},
    "focus": {"type": "boolean", "description": "When true, focus conflict item id in the editor"},
    "suggest": {
      "type": "object",
      "additionalProperties": false,
      "description": "Optional update to the agent's suggestion for id before resolving",
      "properties": {
        "meta": {"type": "string"},
        "yaml": {"type": "string"},
        "reason": {"type": "string"},
        "deleted": {"type": "boolean"}
      }
    }
  }
}`

const mergeEmptySchema = `{"type":"object","additionalProperties":false,"properties":{}}`

func mergeTools() []Tool {
	return []Tool{
		{
			Name:        ToolMergeStatus,
			Title:       "Inspect scene merge conflicts",
			Description: "Inspect the active object-by-object scene merge conflict session (branches, conflicted objects, Mine/Theirs/Agent options and reasons, MERGED BY ID items, and resolution progress), or detect unmerged scene files in the open project's Git repository.",
			InputSchema: []byte(mergeStatusSchema),
			Action:      ActionRead,
			Handler:     mergeStatus,
		},
		{
			Name:        ToolMergeResolve,
			Title:       "Resolve a scene merge conflict",
			Description: "Focus a conflicted scene object, update the agent's suggestion (YAML and reason), pick Agent/Mine/Theirs, apply custom YAML, or take the agent's suggestion for all conflicts.",
			InputSchema: []byte(mergeResolveSchema),
			Action:      ActionEdit,
			Handler:     mergeResolve,
		},
		{
			Name:        ToolMergeFinish,
			Title:       "Finish scene merge",
			Description: "Write the resolved scene, run game check and a short playtest on the result, and stage and commit the Git merge once every conflict is resolved.",
			InputSchema: []byte(mergeEmptySchema),
			Action:      ActionRun,
			Handler:     mergeFinish,
		},
		{
			Name:        ToolMergeAbort,
			Title:       "Abort scene merge",
			Description: "Abort the active Git merge, restoring the branch and working tree as they were, and close the conflict resolver.",
			InputSchema: []byte(mergeEmptySchema),
			Action:      ActionRun,
			Handler:     mergeAbort,
		},
	}
}

// MergeStatusResult is the structured payload returned by merge_status, merge_resolve, and merge_finish.
type MergeStatusResult struct {
	Open           bool                       `json:"open"`
	ScenePath      string                     `json:"scenePath,omitempty"`
	Mine           editor.MergeSideMeta       `json:"mine,omitempty"`
	Theirs         editor.MergeSideMeta       `json:"theirs,omitempty"`
	FocusedID      string                     `json:"focusedId,omitempty"`
	ResolvedCount  int                        `json:"resolvedCount"`
	TotalConflicts int                        `json:"totalConflicts"`
	Finished       bool                       `json:"finished,omitempty"`
	CommitSHA      string                     `json:"commitSha,omitempty"`
	VerifySummary  string                     `json:"verifySummary,omitempty"`
	VerifyErr      string                     `json:"verifyErr,omitempty"`
	DetectedScenes []string                   `json:"detectedScenes,omitempty"`
	Conflicts      []editor.MergeConflictItem `json:"conflicts,omitempty"`
	AutoMerged     []editor.MergeAutoItem     `json:"autoMerged,omitempty"`
}

func snapshotMergeStatus(ed *editor.Editor) MergeStatusResult {
	res := MergeStatusResult{
		Open:           ed.MergeConflictOpen(),
		DetectedScenes: ed.DetectedMergeConflicts(),
	}
	if sess := ed.MergeSession(); sess != nil && ed.MergeConflictOpen() {
		res.ScenePath = sess.ScenePath
		res.Mine = sess.Mine
		res.Theirs = sess.Theirs
		res.FocusedID = sess.FocusedID
		res.ResolvedCount = sess.ResolvedCount()
		res.TotalConflicts = len(sess.Conflicts)
		res.Finished = sess.Finished
		res.CommitSHA = sess.CommitSHA
		res.VerifySummary = sess.VerifySummary
		res.VerifyErr = sess.VerifyErr
		res.Conflicts = append([]editor.MergeConflictItem(nil), sess.Conflicts...)
		res.AutoMerged = append([]editor.MergeAutoItem(nil), sess.AutoMerged...)
	}
	return res
}

type mergeStatusArgs struct {
	Open bool   `json:"open"`
	File string `json:"file"`
}

func resolveMergeProjectFile(ed *editor.Editor, file string) (string, error) {
	if file == "" {
		return "", nil
	}
	clean, err := cleanProjectPath(file)
	if err != nil {
		return "", err
	}
	root := projectRoot(ed)
	if root == "" {
		return "", errors.New("open a project before passing file to merge_status")
	}
	abs := filepath.Join(root, filepath.FromSlash(clean))
	if real, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%s: the file links to %s, outside the project", file, real)
		}
		return real, nil
	} else if !errors.Is(rerr, fs.ErrNotExist) {
		return "", rerr
	}
	// File may not exist on disk (e.g. deleted on ours); verify nearest existing parent stays inside root.
	probe := filepath.Dir(abs)
	for {
		if realDir, derr := filepath.EvalSymlinks(probe); derr == nil {
			if rel, err := filepath.Rel(root, realDir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("%s: path links outside the project", file)
			}
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	return abs, nil
}

func mergeStatus(ctx context.Context, c *Call) (*Result, error) {
	var args mergeStatusArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("merge_status: %w", err)
	}
	var out MergeStatusResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		if args.Open && !ed.MergeConflictOpen() {
			if ed.Playing() {
				return editor.ErrPlaying
			}
			target, perr := resolveMergeProjectFile(ed, args.File)
			if perr != nil {
				return perr
			}
			if err := ed.OpenMergeConflict(target); err != nil {
				return err
			}
		} else if !ed.MergeConflictOpen() {
			if detected, derr := ed.DetectSceneMergeConflicts(); derr == nil && len(detected) > 0 {
				out.DetectedScenes = detected
			}
		}
		snap := snapshotMergeStatus(ed)
		if len(snap.DetectedScenes) == 0 && len(out.DetectedScenes) > 0 {
			snap.DetectedScenes = out.DetectedScenes
		}
		out = snap
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("merge_status: %w", err)
	}
	var b strings.Builder
	if !out.Open {
		if len(out.DetectedScenes) > 0 {
			fmt.Fprintf(&b, "No conflict screen open; detected unmerged scene(s): %s", strings.Join(out.DetectedScenes, ", "))
		} else {
			b.WriteString("No active scene merge conflicts.")
		}
	} else {
		fmt.Fprintf(&b, "Merging %s into %s (%s): %d of %d conflicts resolved.",
			out.Theirs.Branch, out.Mine.Branch, out.ScenePath, out.ResolvedCount, out.TotalConflicts)
		if out.Finished && out.CommitSHA != "" {
			fmt.Fprintf(&b, " Merged as %s.", out.CommitSHA)
		}
	}
	return &Result{Text: b.String(), Structured: out}, nil
}

type mergeResolveArgs struct {
	ID       string `json:"id"`
	Choice   string `json:"choice"`
	YAML     string `json:"yaml"`
	AllAgent bool   `json:"allAgent"`
	Focus    bool   `json:"focus"`
	Suggest  *struct {
		Meta    string `json:"meta"`
		YAML    string `json:"yaml"`
		Reason  string `json:"reason"`
		Deleted bool   `json:"deleted"`
	} `json:"suggest"`
}

func mergeResolve(ctx context.Context, c *Call) (*Result, error) {
	var args mergeResolveArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("merge_resolve: %w", err)
	}
	if !args.AllAgent && args.ID == "" {
		return nil, errors.New("merge_resolve: provide id or set allAgent: true")
	}
	var out MergeStatusResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		if args.Suggest != nil {
			if args.ID == "" {
				return errors.New("suggest requires id")
			}
			if err := ed.SetMergeAgentSuggestion(args.ID, editor.MergeSuggestion{
				Meta:    args.Suggest.Meta,
				YAML:    args.Suggest.YAML,
				Reason:  args.Suggest.Reason,
				Deleted: args.Suggest.Deleted,
			}); err != nil {
				return err
			}
		}
		if args.Focus && args.ID != "" {
			if err := ed.FocusMergeConflict(args.ID); err != nil {
				return err
			}
		}
		if args.AllAgent {
			if err := ed.TakeAllAgentMergeSuggestions(); err != nil {
				return err
			}
		} else if args.Choice != "" {
			switch args.Choice {
			case "custom":
				if err := ed.ApplyMergeCustomYAML(args.ID, args.YAML); err != nil {
					return err
				}
			case "unresolved":
				if err := ed.ResolveMergeConflict(args.ID, editor.MergeChoiceUnresolved); err != nil {
					return err
				}
			case "agent":
				if err := ed.ResolveMergeConflict(args.ID, editor.MergeChoiceAgent); err != nil {
					return err
				}
			case "mine":
				if err := ed.ResolveMergeConflict(args.ID, editor.MergeChoiceMine); err != nil {
					return err
				}
			case "theirs":
				if err := ed.ResolveMergeConflict(args.ID, editor.MergeChoiceTheirs); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown choice %q", args.Choice)
			}
		}
		out = snapshotMergeStatus(ed)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("merge_resolve: %w", err)
	}
	text := fmt.Sprintf("Resolved %d of %d conflicts in %s.", out.ResolvedCount, out.TotalConflicts, out.ScenePath)
	return &Result{Text: text, Structured: out}, nil
}

func mergeFinish(ctx context.Context, c *Call) (*Result, error) {
	var empty struct{}
	if err := c.Decode(&empty); err != nil {
		return nil, fmt.Errorf("merge_finish: %w", err)
	}
	var out MergeStatusResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		if err := ed.FinishMerge(); err != nil {
			return err
		}
		out = snapshotMergeStatus(ed)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("merge_finish: %w", err)
	}
	msg := "Finished merge."
	if out.CommitSHA != "" {
		msg = fmt.Sprintf("Merged as %s. %s", out.CommitSHA, out.VerifySummary)
	} else if out.VerifySummary != "" {
		msg = out.VerifySummary
	}
	return &Result{Text: msg, Structured: out}, nil
}

func mergeAbort(ctx context.Context, c *Call) (*Result, error) {
	var empty struct{}
	if err := c.Decode(&empty); err != nil {
		return nil, fmt.Errorf("merge_abort: %w", err)
	}
	var out MergeStatusResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		if err := ed.AbortMerge(); err != nil {
			return err
		}
		out = snapshotMergeStatus(ed)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("merge_abort: %w", err)
	}
	return &Result{Text: "Merge aborted and branch restored.", Structured: out}, nil
}
