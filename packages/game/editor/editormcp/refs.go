package editormcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

const (
	// ToolRefsFind finds every usage of a prefab, scene, or project file across scenes, prefab variants, data tables, and Go source code.
	ToolRefsFind = "refs_find"
	// ToolRefactorRename previews a safe YAML-only rename in the editor's References review screen (with exact line diffs and non-rewritten Go warnings), or applies it as one undo step when apply is true.
	ToolRefactorRename = "refactor_rename"
)

const refsFindSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "file": {"type": "string", "description": "Slash-separated project path (such as prefabs/slime.prefab.yaml or scenes/level_01.scene.yaml). When omitted, uses the selected object's prefab/scene, selected file, or open scene."},
    "open": {"type": "boolean", "description": "When true, open the Find Usages screen in the editor for the target file."}
  }
}`

const refactorRenameSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["from", "to"],
  "properties": {
    "from": {"type": "string", "description": "Slash-separated project path of the prefab, scene, or file to rename (such as prefabs/slime.prefab.yaml)."},
    "to": {"type": "string", "description": "New file stem (such as slime_green) or full slash-separated destination path (such as prefabs/slime_green.prefab.yaml)."},
    "apply": {"type": "boolean", "description": "When false or omitted, opens the safe-rename review in the editor with exact line diffs and non-rewritten Go warnings without mutating any file on disk. When true, applies the YAML-only rename through MoveFile as one undoable step."}
  }
}`

func refsTools() []Tool {
	return []Tool{
		{
			Name:        ToolRefsFind,
			Title:       "Find usages of a prefab, scene, or file",
			Description: "Find every usage of a prefab, scene, or project file grouped into scenes (with instance override counts), prefab variants (inherits), data tables (rows), and Go code (string literals/constants via go/parser), and optionally open the References screen in the editor.",
			InputSchema: []byte(refsFindSchema),
			Action:      ActionRead,
			Handler:     refsFind,
		},
		{
			Name:        ToolRefactorRename,
			Title:       "Preview or apply a safe rename",
			Description: "Prepare a safe YAML-only rename review in the editor's References screen showing the moved file, every exact -/+ YAML line diff with instance overrides preserved, and any Go code references that are intentionally not rewritten. Without apply: true, no file is written to disk so the person can review and click Keep; with apply: true, applies the rename through MoveFile as one undoable step.",
			InputSchema: []byte(refactorRenameSchema),
			Action:      ActionEdit,
			targets:     refactorRenameTargets,
			Handler:     refactorRename,
		},
	}
}

// RefsFindResult is the structured payload returned by refs_find.
type RefsFindResult struct {
	Target      string                  `json:"target"`
	Open        bool                    `json:"open"`
	TotalUsages int                     `json:"totalUsages"`
	TotalFiles  int                     `json:"totalFiles"`
	Scenes      []editor.UsageFileGroup `json:"scenes,omitempty"`
	Variants    []editor.UsageFileGroup `json:"variants,omitempty"`
	DataTables  []editor.UsageFileGroup `json:"dataTables,omitempty"`
	GoCode      []editor.UsageFileGroup `json:"goCode,omitempty"`
	Other       []editor.UsageFileGroup `json:"other,omitempty"`
}

// RefactorRenameResult is the structured payload returned by refactor_rename.
type RefactorRenameResult struct {
	From               string                      `json:"from"`
	To                 string                      `json:"to"`
	Valid              bool                        `json:"valid"`
	ValidationMsg      string                      `json:"validationMsg,omitempty"`
	Applied            bool                        `json:"applied"`
	ReviewOpen         bool                        `json:"reviewOpen"`
	RewrittenFileCount int                         `json:"rewrittenFileCount"`
	TotalTouchedFiles  int                         `json:"totalTouchedFiles"`
	RewrittenRefCount  int                         `json:"rewrittenRefCount"`
	PreservedObjects   []string                    `json:"preservedObjects,omitempty"`
	Files              []editor.SafeRenameFileDiff `json:"files,omitempty"`
	Problems           []editor.SafeRenameProblem  `json:"problems,omitempty"`
	CheckProblems      []editor.SafeRenameProblem  `json:"checkProblems,omitempty"`
}

type refsFindArgs struct {
	File string `json:"file"`
	Open bool   `json:"open"`
}

func refsFind(ctx context.Context, c *Call) (*Result, error) {
	var args refsFindArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("refs_find: %w", err)
	}
	var out RefsFindResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		target := strings.TrimSpace(args.File)
		if target != "" {
			clean, cerr := cleanProjectPath(target)
			if cerr != nil {
				return cerr
			}
			target = clean
		}
		if args.Open {
			if err := ed.OpenUsagesScreen(target); err != nil {
				return err
			}
			st := ed.ReferencesScreen()
			if st == nil || st.Report == nil {
				return errors.New("references screen did not open")
			}
			out = refsFindResultFromReport(st.Report, true)
			return nil
		}
		if target == "" {
			if st := ed.ReferencesScreen(); st != nil && st.Target != "" {
				target = st.Target
			} else if ed.SelectedFile != "" {
				target = ed.SelectedFile
			} else if ed.ScenePath != "" {
				target = ed.ScenePath
			}
		}
		if target == "" {
			return errors.New("pass file or select a file/scene in the editor")
		}
		rep, err := ed.FindUsages(target)
		if err != nil {
			return err
		}
		out = refsFindResultFromReport(rep, ed.UsagesScreenOpen())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("refs_find: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Usages of %s: %d in %d files (%d scenes, %d prefab variants, %d data tables, %d Go files).",
		out.Target, out.TotalUsages, out.TotalFiles,
		len(out.Scenes), len(out.Variants), len(out.DataTables), len(out.GoCode))
	for _, gf := range out.GoCode {
		for _, r := range gf.Refs {
			fmt.Fprintf(&b, "\n  [not rewritten] %s:%d %s", r.File, r.Line, r.Label)
		}
	}
	return &Result{Text: b.String(), Structured: out}, nil
}

func refsFindResultFromReport(rep *editor.UsagesReport, open bool) RefsFindResult {
	if rep == nil {
		return RefsFindResult{Open: open}
	}
	return RefsFindResult{
		Target:      rep.Target,
		Open:        open,
		TotalUsages: rep.TotalUsages,
		TotalFiles:  rep.TotalFiles,
		Scenes:      rep.Scenes,
		Variants:    rep.Variants,
		DataTables:  rep.DataTables,
		GoCode:      rep.GoCode,
		Other:       rep.Other,
	}
}

type refactorRenameArgs struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Apply bool   `json:"apply"`
}

func refactorRename(ctx context.Context, c *Call) (*Result, error) {
	var args refactorRenameArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("refactor_rename: %w", err)
	}
	fromClean, err := cleanProjectPath(args.From)
	if err != nil {
		return nil, fmt.Errorf("refactor_rename: from: %w", err)
	}
	toRaw := strings.TrimSpace(args.To)
	if toRaw == "" {
		return nil, errors.New("refactor_rename: to must not be empty")
	}
	if strings.ContainsRune(toRaw, '/') {
		if _, cerr := cleanProjectPath(toRaw); cerr != nil {
			return nil, fmt.Errorf("refactor_rename: to: %w", cerr)
		}
	}

	committed := false
	if !c.onEditor && args.Apply {
		rollback, err := c.s.acquireObjectHolds(ctx, c.taskID, []string{"file:" + fromClean, "file:" + toRaw})
		if err != nil {
			return nil, err
		}
		defer func() {
			if !committed {
				rollback()
			}
		}()
	}

	var out RefactorRenameResult
	err = c.Editor(ctx, func(ed *editor.Editor) error {
		if holdErr := sketchHoldEditError(ed, fromClean); holdErr != nil {
			return holdErr
		}
		plan, perr := ed.PreviewSafeRename(fromClean, toRaw)
		if plan == nil {
			if perr != nil {
				return perr
			}
			return errors.New("could not compute rename plan")
		}
		out = RefactorRenameResult{
			From:               plan.From,
			To:                 plan.To,
			Valid:              plan.Valid,
			ValidationMsg:      plan.ValidationMsg,
			ReviewOpen:         ed.UsagesScreenOpen(),
			RewrittenFileCount: plan.RewrittenFileCount,
			TotalTouchedFiles:  plan.TotalTouchedFiles,
			RewrittenRefCount:  plan.RewrittenRefCount,
			PreservedObjects:   plan.PreservedObjects,
			Files:              plan.Files,
			Problems:           plan.Problems,
		}
		if !plan.Valid {
			if perr != nil {
				return perr
			}
			return errors.New(plan.ValidationMsg)
		}
		if args.Apply {
			if aerr := ed.ApplySafeRename(); aerr != nil {
				return aerr
			}
			out.Applied = true
			if st := ed.ReferencesScreen(); st != nil {
				out.CheckProblems = st.CheckProblems
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("refactor_rename: %w", err)
	}
	committed = true

	var b strings.Builder
	if out.Applied {
		fmt.Fprintf(&b, "Renamed %s -> %s (%d files written as one undo step; %d YAML references rewritten).",
			out.From, out.To, out.TotalTouchedFiles, out.RewrittenRefCount)
	} else {
		fmt.Fprintf(&b, "Prepared safe-rename review %s -> %s (%d files planned, %d YAML references to rewrite; nothing written to disk until Keep).",
			out.From, out.To, out.TotalTouchedFiles, out.RewrittenRefCount)
	}
	for _, p := range out.Problems {
		fmt.Fprintf(&b, "\n  [not rewritten] %s:%d %s", p.File, p.Line, p.Message)
	}
	return &Result{Text: b.String(), Structured: out}, nil
}
