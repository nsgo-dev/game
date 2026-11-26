package editormcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

const (
	// ToolCodeReviewPropose stages pending Go files (and optional companion YAML files) in the editor's file-by-file Go review without writing them to disk, and optionally runs nsgo check and nsgo test via a Go build overlay.
	ToolCodeReviewPropose = "code_review_propose"
	// ToolCodeReviewStatus reports the active Go code review's round, files, confirmation status, check/test outcomes, and any waiting unconfirmed Go files.
	ToolCodeReviewStatus = "code_review_status"
	// ToolCodeReviewRunChecks reruns nsgo check and nsgo test against the pending Go files via a Go build overlay without touching disk.
	ToolCodeReviewRunChecks = "code_review_run_checks"
)

const codeReviewProposeSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["files"],
  "properties": {
    "prompt": {"type": "string", "description": "The user's request shown in the review header when starting a review; omit when submitting a Round 2 fix inside the same turn"},
    "round": {"type": "integer", "minimum": 1, "description": "Optional explicit round number; when omitted on an update to an active review, increments automatically"},
    "summary": {"type": "string", "description": "Short summary of what this round changed (for example the Round 2 fix note)"},
    "failure_explanation": {"type": "string", "description": "Plain-language explanation of why a test failed, shown in the failed test card"},
    "suspect_file": {"type": "string", "description": "Optional slash-separated project-relative Go file whose line caused the failure"},
    "suspect_line": {"type": "integer", "minimum": 1, "description": "Optional 1-based line number in suspect_file tagged as fails test"},
    "play_impact": {"type": "string", "description": "Short explanation of what leaving Go unconfirmed means in Play (shown in the Agent panel waiting card)"},
    "open": {"type": "boolean", "description": "Open the Go code review screen in the editor (default true)"},
    "run_checks": {"type": "boolean", "description": "Run nsgo check and nsgo test against the pending Go build overlay immediately (default true)"},
    "files": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["path"],
        "properties": {
          "path": {"type": "string", "description": "Slash-separated path from the project root, such as gameplay/patrol.go or prefabs/slime.prefab.yaml"},
          "content": {"type": "string", "description": "Full pending file contents; held in memory until confirmed and kept"},
          "delete": {"type": "boolean", "description": "Mark the file for pending deletion instead of writing content"},
          "note": {"type": "string", "description": "Optional per-file footer note (for example which prefab variants inherit a change)"}
        }
      }
    }
  }
}`

const codeReviewStatusSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`

const codeReviewRunChecksSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "failure_explanation": {"type": "string", "description": "Optional updated explanation of why a failing test failed"},
    "suspect_file": {"type": "string", "description": "Optional slash-separated path of the source file line that caused the failure"},
    "suspect_line": {"type": "integer", "minimum": 1, "description": "Optional 1-based line number in suspect_file to tag as fails test"}
  }
}`

func codeReviewTools() []Tool {
	return []Tool{
		{
			Name:        ToolCodeReviewPropose,
			Title:       "Propose Go code changes for review",
			Description: "Stage or update pending Go files (and optional companion YAML/prefab files) in the editor's file-by-file Go review without touching disk, so Play keeps running the old code until each Go file passes nsgo check and nsgo test and is confirmed by the author. Calling this while a review is active updates the same turn as round 2.",
			InputSchema: []byte(codeReviewProposeSchema),
			Action:      ActionEdit,
			Handler:     codeReviewPropose,
		},
		{
			Name:        ToolCodeReviewStatus,
			Title:       "Inspect Go code review status",
			Description: "Read the active Go code review state: round number, pending YAML and Go files with line deltas and confirmation badges, nsgo check and nsgo test outcomes, failing test details, and any unconfirmed Go files waiting after Keep.",
			InputSchema: []byte(codeReviewStatusSchema),
			Action:      ActionRead,
			Handler:     codeReviewStatus,
		},
		{
			Name:        ToolCodeReviewRunChecks,
			Title:       "Run checks on pending Go changes",
			Description: "Run nsgo check and nsgo test against the editor's pending Go files via a Go build overlay without modifying files on disk, unlocking per-file confirmation only when all checks pass.",
			InputSchema: []byte(codeReviewRunChecksSchema),
			Action:      ActionRun,
			Handler:     codeReviewRunChecks,
		},
	}
}

// CodeReviewFileSummary is one file entry in CodeReviewStatusResult.
type CodeReviewFileSummary struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	AddedLines    int    `json:"addedLines"`
	RemovedLines  int    `json:"removedLines"`
	Confirmed     bool   `json:"confirmed"`
	Applied       bool   `json:"applied"`
	EditedInRound int    `json:"editedInRound"`
}

// CodeReviewChecksSummary is the structured check/test status returned by code_review_* tools.
type CodeReviewChecksSummary struct {
	Phase       string                         `json:"phase"`
	CheckPassed bool                           `json:"checkPassed"`
	CheckError  string                         `json:"checkError,omitempty"`
	TestPassed  bool                           `json:"testPassed"`
	PassedCount int                            `json:"passedCount"`
	FailedCount int                            `json:"failedCount"`
	TotalCount  int                            `json:"totalCount"`
	DurationMs  int64                          `json:"durationMs,omitempty"`
	Packages    []string                       `json:"packages,omitempty"`
	Failures    []editor.CodeReviewTestFailure `json:"failures,omitempty"`
}

// CodeReviewStatusResult is the structured result returned by code_review_propose, code_review_status, and code_review_run_checks.
type CodeReviewStatusResult struct {
	Active         bool                    `json:"active"`
	Open           bool                    `json:"open"`
	Paused         bool                    `json:"paused"`
	KeptYAML       bool                    `json:"keptYaml"`
	FixRequested   bool                    `json:"fixRequested"`
	TurnID         string                  `json:"turnId,omitempty"`
	Prompt         string                  `json:"prompt,omitempty"`
	Round          int                     `json:"round,omitempty"`
	RoundSummary   string                  `json:"roundSummary,omitempty"`
	SelectedPath   string                  `json:"selectedPath,omitempty"`
	Layout         string                  `json:"layout,omitempty"`
	Files          []CodeReviewFileSummary `json:"files,omitempty"`
	WaitingGoFiles []string                `json:"waitingGoFiles,omitempty"`
	Checks         CodeReviewChecksSummary `json:"checks"`
}

type codeReviewProposeArgs struct {
	Prompt             string                          `json:"prompt"`
	Round              int                             `json:"round"`
	Summary            string                          `json:"summary"`
	FailureExplanation string                          `json:"failure_explanation"`
	SuspectFile        string                          `json:"suspect_file"`
	SuspectLine        int                             `json:"suspect_line"`
	PlayImpact         string                          `json:"play_impact"`
	Open               *bool                           `json:"open"`
	RunChecks          *bool                           `json:"run_checks"`
	Files              []editor.CodeReviewProposedFile `json:"files"`
}

type codeReviewRunChecksArgs struct {
	FailureExplanation string `json:"failure_explanation"`
	SuspectFile        string `json:"suspect_file"`
	SuspectLine        int    `json:"suspect_line"`
}

func snapshotCodeReviewStatus(ed *editor.Editor) CodeReviewStatusResult {
	sess := ed.CodeReviewSession()
	if sess == nil {
		return CodeReviewStatusResult{Active: false}
	}
	files := make([]CodeReviewFileSummary, 0, len(sess.Files))
	for _, f := range sess.Files {
		files = append(files, CodeReviewFileSummary{
			Path:          f.Path,
			Kind:          string(f.Kind),
			AddedLines:    f.AddedLines,
			RemovedLines:  f.RemovedLines,
			Confirmed:     f.Confirmed,
			Applied:       f.Applied,
			EditedInRound: f.EditedInRound,
		})
	}
	return CodeReviewStatusResult{
		Active:         true,
		Open:           sess.Open,
		Paused:         sess.Paused,
		KeptYAML:       sess.KeptYAML,
		FixRequested:   sess.FixRequested,
		TurnID:         sess.TurnID,
		Prompt:         sess.Prompt,
		Round:          sess.Round,
		RoundSummary:   sess.RoundSummary,
		SelectedPath:   sess.SelectedPath,
		Layout:         string(sess.Layout),
		Files:          files,
		WaitingGoFiles: ed.CodeReviewWaitingGoFiles(),
		Checks: CodeReviewChecksSummary{
			Phase:       string(sess.Checks.Phase),
			CheckPassed: sess.Checks.CheckPassed,
			CheckError:  sess.Checks.CheckError,
			TestPassed:  sess.Checks.TestPassed,
			PassedCount: sess.Checks.PassedCount,
			FailedCount: sess.Checks.FailedCount,
			TotalCount:  sess.Checks.TotalCount,
			DurationMs:  sess.Checks.Duration.Milliseconds(),
			Packages:    sess.Checks.Packages,
			Failures:    sess.Checks.Failures,
		},
	}
}

func formatCodeReviewStatusText(st CodeReviewStatusResult) string {
	if !st.Active {
		return "No active Go code review."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Go code review (round %d, open=%v): %d file(s)", st.Round, st.Open, len(st.Files))
	if len(st.WaitingGoFiles) > 0 {
		fmt.Fprintf(&b, ", %d Go file(s) waiting for OK (%s)", len(st.WaitingGoFiles), strings.Join(st.WaitingGoFiles, ", "))
	}
	b.WriteString(".")
	switch st.Checks.Phase {
	case string(editor.CodeReviewChecksPassed):
		fmt.Fprintf(&b, "\nChecks: nsgo check passed · nsgo test %d passed.", st.Checks.PassedCount)
	case string(editor.CodeReviewChecksFailed):
		if !st.Checks.CheckPassed && st.Checks.CheckError != "" {
			fmt.Fprintf(&b, "\nChecks: nsgo check failed: %s", st.Checks.CheckError)
		} else {
			fmt.Fprintf(&b, "\nChecks: nsgo test %d failed, %d passed.", st.Checks.FailedCount, st.Checks.PassedCount)
		}
		for _, f := range st.Checks.Failures {
			fmt.Fprintf(&b, "\n- %s: %s", f.TestName, f.Message)
		}
	case string(editor.CodeReviewChecksRunning), string(editor.CodeReviewChecksFixing):
		fmt.Fprintf(&b, "\nChecks: %s.", st.Checks.Phase)
	default:
		b.WriteString("\nChecks: not run yet.")
	}
	return b.String()
}

func codeReviewPropose(ctx context.Context, c *Call) (*Result, error) {
	var args codeReviewProposeArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("code_review_propose: %w", err)
	}
	if len(args.Files) == 0 {
		return nil, errors.New("code_review_propose: files must contain at least one file")
	}
	open := true
	if args.Open != nil {
		open = *args.Open
	}
	runChecks := true
	if args.RunChecks != nil {
		runChecks = *args.RunChecks
	}

	var (
		run *editor.CodeReviewCheckRun
		ed  *editor.Editor
		st  CodeReviewStatusResult
	)
	if err := c.Editor(ctx, func(e *editor.Editor) error {
		ed = e
		if err := e.ProposeCodeReviewFiles(editor.CodeReviewProposeRequest{
			Prompt:             args.Prompt,
			Round:              args.Round,
			RoundSummary:       args.Summary,
			FailureExplanation: args.FailureExplanation,
			SuspectFile:        args.SuspectFile,
			SuspectLine:        args.SuspectLine,
			PlayImpact:         args.PlayImpact,
			Files:              args.Files,
			Open:               open,
			PreserveTurn:       args.Prompt == "",
		}); err != nil {
			return err
		}
		if runChecks && len(e.CodeReviewSession().GoFiles()) > 0 {
			var err error
			run, err = e.PrepareCodeReviewCheckRun()
			if err != nil {
				return err
			}
		} else {
			st = snapshotCodeReviewStatus(e)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("code_review_propose: %w", err)
	}

	if run != nil {
		res, runErr := ed.ExecuteCodeReviewCheckRun(ctx, run)
		if err := c.Editor(ctx, func(e *editor.Editor) error {
			_ = e.ApplyCodeReviewCheckRun(run, res, runErr)
			st = snapshotCodeReviewStatus(e)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("code_review_propose: %w", err)
		}
	}

	return &Result{
		Text:       formatCodeReviewStatusText(st),
		Structured: st,
	}, nil
}

func codeReviewStatus(ctx context.Context, c *Call) (*Result, error) {
	var st CodeReviewStatusResult
	if err := c.Editor(ctx, func(e *editor.Editor) error {
		st = snapshotCodeReviewStatus(e)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("code_review_status: %w", err)
	}
	return &Result{
		Text:       formatCodeReviewStatusText(st),
		Structured: st,
	}, nil
}

func codeReviewRunChecks(ctx context.Context, c *Call) (*Result, error) {
	var args codeReviewRunChecksArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("code_review_run_checks: %w", err)
	}

	var (
		run *editor.CodeReviewCheckRun
		ed  *editor.Editor
	)
	if err := c.Editor(ctx, func(e *editor.Editor) error {
		ed = e
		if args.FailureExplanation != "" || args.SuspectFile != "" || args.SuspectLine > 0 {
			e.SetCodeReviewFailureExplanation(args.FailureExplanation, args.SuspectFile, args.SuspectLine)
		}
		var err error
		run, err = e.PrepareCodeReviewCheckRun()
		return err
	}); err != nil {
		return nil, fmt.Errorf("code_review_run_checks: %w", err)
	}

	res, runErr := ed.ExecuteCodeReviewCheckRun(ctx, run)
	var st CodeReviewStatusResult
	if err := c.Editor(ctx, func(e *editor.Editor) error {
		_ = e.ApplyCodeReviewCheckRun(run, res, runErr)
		st = snapshotCodeReviewStatus(e)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("code_review_run_checks: %w", err)
	}

	return &Result{
		Text:       formatCodeReviewStatusText(st),
		Structured: st,
	}, nil
}
