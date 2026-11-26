package editormcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/playtest"
)

// PermissionPromptAnswer is the person's response when an MCP tool call matches an Ask permission rule.
type PermissionPromptAnswer int

const (
	// PermissionAnswerPending leaves the request pending without running the tool, returning a *PermissionError with Decision == editor.PermissionAsk.
	PermissionAnswerPending PermissionPromptAnswer = iota
	// PermissionAnswerAllowOnce allows this single tool call without changing permissions.yaml.
	PermissionAnswerAllowOnce
	// PermissionAnswerAllowAlways inserts and saves an Allow rule in permissions.yaml, shows the "Always permission rule saved" toast with Show and Undo, and runs the tool.
	PermissionAnswerAllowAlways
	// PermissionAnswerDeny refuses this tool call without changing permissions.yaml.
	PermissionAnswerDeny
)

// PermissionError is returned when the editor's MCP permission rules block a tool call (Deny) or pause it awaiting the person's approval (Ask).
// The underlying tool handler never runs when a PermissionError is returned.
type PermissionError struct {
	// Tool is the MCP tool name that was checked.
	Tool string `json:"tool"`
	// Action is the evaluated permission action ("edit", "create", "delete", "run", or "code").
	Action editor.PermissionAction `json:"action"`
	// Target is the evaluated project-relative file path or run target.
	Target string `json:"target,omitempty"`
	// Decision is editor.PermissionDeny or editor.PermissionAsk.
	Decision editor.PermissionDecision `json:"decision"`
	// Profile is the active base profile ("cautious", "balanced", or "autonomous").
	Profile editor.PermissionProfile `json:"profile"`
	// Modified reports whether the active rules differ from the profile preset.
	Modified bool `json:"modified,omitempty"`
	// RuleIndex is the 0-based index of the first matching rule, or -1 when the profile default decided.
	RuleIndex int `json:"ruleIndex"`
	// RuleNumber is the 1-based rule number (RuleIndex + 1), or 0 when the profile default decided.
	RuleNumber int `json:"ruleNumber,omitempty"`
	// RuleTotal is the total number of rules in the active table.
	RuleTotal int `json:"ruleTotal,omitempty"`
	// Rule is a copy of the matched rule, or nil when the profile default decided.
	Rule *editor.PermissionRule `json:"rule,omitempty"`
	// Reason is a clear, agent-readable explanation naming the rule and profile.
	Reason string `json:"reason"`
	// Link is the Settings › Permissions location of the matching rule, such as "Settings › Permissions · rule 7 of 7".
	Link string `json:"link"`
	// RequestID identifies the pending Ask request queued on the editor, when Decision == editor.PermissionAsk.
	RequestID string `json:"requestId,omitempty"`
}

func (e *PermissionError) Error() string {
	if e == nil {
		return ""
	}
	prefix := "permission denied"
	if e.Decision == editor.PermissionAsk {
		prefix = "permission required"
	}
	if e.Tool != "" {
		return fmt.Sprintf("%s: %s: %s", e.Tool, prefix, e.Reason)
	}
	return fmt.Sprintf("%s: %s", prefix, e.Reason)
}

// LastPermissionDecision returns the most recent permission evaluation recorded by the server for a mutating or running tool call.
func (s *Server) LastPermissionDecision() (editor.PermissionEvaluation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastPermEval, s.hasLastPermEval
}

func (s *Server) recordPermissionDecision(eval editor.PermissionEvaluation) {
	s.mu.Lock()
	s.lastPermEval = eval
	s.hasLastPermEval = true
	cb := s.onPermDecision
	s.mu.Unlock()
	if cb != nil {
		cb(eval)
	}
}

// checkPermission evaluates t against the editor's permission rules before t.Handler runs.
// Read-only tools (ActionRead or empty Action) are always allowed without consulting the rule table.
func (s *Server) checkPermission(ctx context.Context, t Tool, args json.RawMessage, onEditor bool) error {
	if t.Action == "" || t.Action == ActionRead {
		return nil
	}

	var (
		eval    editor.PermissionEvaluation
		summary string
	)
	runOnEditor := func(fn func(ed *editor.Editor) error) error {
		if onEditor {
			return s.asAgent(fn)
		}
		return s.Editor(ctx, fn)
	}

	if err := runOnEditor(func(ed *editor.Editor) error {
		req := permissionRequestForTool(t, args, ed)
		summary = req.Summary
		eval = ed.EvaluatePermission(req)
		return nil
	}); err != nil {
		return err
	}

	s.recordPermissionDecision(eval)

	switch eval.Decision {
	case editor.PermissionAllow:
		return nil

	case editor.PermissionDeny:
		_ = runOnEditor(func(ed *editor.Editor) error {
			ed.RecordBlockedPermission(eval, summary)
			return nil
		})
		return permissionErrorFromEval(eval, "")

	case editor.PermissionAsk:
		if s.onPermAsk != nil {
			ans, err := s.onPermAsk(ctx, eval)
			if err != nil {
				return err
			}
			switch ans {
			case PermissionAnswerAllowOnce:
				return nil
			case PermissionAnswerAllowAlways:
				if err := runOnEditor(func(ed *editor.Editor) error {
					_, err := ed.ApproveAlwaysPermission(eval)
					return err
				}); err != nil {
					return err
				}
				return nil
			case PermissionAnswerDeny:
				deniedEval := eval
				deniedEval.Decision = editor.PermissionDeny
				deniedEval.Reason = "Denied by you: " + eval.Reason
				return permissionErrorFromEval(deniedEval, "")
			}
		}
		var reqID string
		_ = runOnEditor(func(ed *editor.Editor) error {
			req := ed.EnqueuePermissionRequest(eval, summary, nil)
			reqID = req.ID
			return nil
		})
		return permissionErrorFromEval(eval, reqID)

	default:
		return permissionErrorFromEval(eval, "")
	}
}

func permissionErrorFromEval(eval editor.PermissionEvaluation, reqID string) *PermissionError {
	return &PermissionError{
		Tool:       eval.Tool,
		Action:     eval.Action,
		Target:     eval.Target,
		Decision:   eval.Decision,
		Profile:    eval.Profile,
		Modified:   eval.Modified,
		RuleIndex:  eval.RuleIndex,
		RuleNumber: eval.RuleNumber,
		RuleTotal:  eval.RuleTotal,
		Rule:       eval.Rule,
		Reason:     eval.Reason,
		Link:       eval.Link,
		RequestID:  reqID,
	}
}

func permissionRequestForTool(t Tool, args json.RawMessage, ed *editor.Editor) editor.PermissionRequest {
	action := mapToolAction(t.Action)
	target := ""
	if t.Target != nil {
		a, tgt := t.Target(args, ed)
		if a.Valid() {
			action = a
		}
		target = tgt
	} else {
		target = inferToolTarget(action, t.Name, args, ed)
	}
	summary := formatToolCallSummary(action, t.Name, target)
	return editor.PermissionRequest{
		Tool:    t.Name,
		Action:  action,
		Target:  target,
		Summary: summary,
	}
}

func mapToolAction(a Action) editor.PermissionAction {
	switch a {
	case ActionEdit:
		return editor.PermissionActionEdit
	case ActionCreate:
		return editor.PermissionActionCreate
	case ActionDelete:
		return editor.PermissionActionDelete
	case ActionRun:
		return editor.PermissionActionRun
	case ActionCode:
		return editor.PermissionActionCode
	default:
		return editor.PermissionActionEdit
	}
}

func inferToolTarget(action editor.PermissionAction, toolName string, args json.RawMessage, ed *editor.Editor) string {
	var raw map[string]any
	_ = json.Unmarshal(args, &raw)
	strField := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	if action == editor.PermissionActionRun {
		if cmd := strField("command", "cmd"); cmd != "" {
			return editor.PermissionRunShell + ": " + cmd
		}
		if tgt := strField("target", "kind", "run"); tgt != "" {
			return tgt
		}
		return toolName
	}
	if f := strField("file", "path", "from", "target"); f != "" {
		return strings.TrimPrefix(strings.ReplaceAll(f, `\`, "/"), "./")
	}
	if action == editor.PermissionActionEdit && ed != nil {
		return openSceneTarget(ed)
	}
	return ""
}

func openSceneTarget(ed *editor.Editor) string {
	if ed == nil {
		return "scenes/untitled.scene.yaml"
	}
	if root := projectRoot(ed); root != "" {
		if p := scenePath(ed, root); p != "" {
			return p
		}
	}
	if ed.ScenePath != "" {
		return ed.ScenePath
	}
	return "scenes/untitled.scene.yaml"
}

func formatToolCallSummary(action editor.PermissionAction, toolName, target string) string {
	if action == editor.PermissionActionRun {
		if after, ok := strings.CutPrefix(target, editor.PermissionRunShell+":"); ok {
			return "run " + strings.TrimSpace(after)
		}
		if target != "" {
			return "run " + target
		}
		return "run " + toolName
	}
	if target != "" {
		return fmt.Sprintf("%s %s", strings.ToLower(action.DisplayName()), target)
	}
	return toolName
}

// Target extractors for built-in tools.

func sceneEditTarget(_ json.RawMessage, ed *editor.Editor) (editor.PermissionAction, string) {
	return editor.PermissionActionEdit, openSceneTarget(ed)
}

func gameCheckTarget(_ json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	return editor.PermissionActionRun, editor.PermissionRunGameCheck
}

func gamePlaytestTarget(_ json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	return editor.PermissionActionRun, editor.PermissionRunPlaytests
}

func statesWriteTarget(args json.RawMessage, ed *editor.Editor) (editor.PermissionAction, string) {
	var in struct {
		File string `json:"file"`
	}
	_ = json.Unmarshal(args, &in)
	target := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(in.File), `\`, "/"), "./")
	if target != "" && ed != nil {
		if root := projectRoot(ed); root != "" {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(target))); os.IsNotExist(err) {
				return editor.PermissionActionCreate, target
			}
		}
	}
	return editor.PermissionActionEdit, target
}

func statesTestTarget(_ json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	return editor.PermissionActionRun, editor.PermissionRunPlaytests
}

func cutsceneWriteTarget(args json.RawMessage, ed *editor.Editor) (editor.PermissionAction, string) {
	var in struct {
		File   string `json:"file"`
		Review *bool  `json:"review"`
	}
	_ = json.Unmarshal(args, &in)
	target := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(in.File), `\`, "/"), "./")
	if target != "" && ed != nil && in.Review != nil && !*in.Review {
		if root := projectRoot(ed); root != "" {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(target))); os.IsNotExist(err) {
				return editor.PermissionActionCreate, target
			}
		}
	}
	return editor.PermissionActionEdit, target
}

func tilesetEditTarget(args json.RawMessage, ed *editor.Editor) (editor.PermissionAction, string) {
	var in struct {
		File string `json:"file"`
	}
	_ = json.Unmarshal(args, &in)
	target := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(in.File), `\`, "/"), "./")
	if target == "" && ed != nil && ed.TilesetOpen() {
		target = ed.TilesetPath()
	}
	return editor.PermissionActionEdit, target
}

func playtestDefineTarget(args json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	var in struct {
		File string `json:"file"`
	}
	_ = json.Unmarshal(args, &in)
	target := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(in.File), `\`, "/"), "./")
	if target == "" {
		target = playtest.FileName
	}
	return editor.PermissionActionEdit, target
}

func playtestReportTarget(args json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	var in struct {
		File string `json:"file"`
	}
	_ = json.Unmarshal(args, &in)
	target := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(in.File), `\`, "/"), "./")
	if target == "" {
		target = playtest.ReportFileName
	}
	return editor.PermissionActionEdit, target
}

func refactorRenameTarget(args json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	var in struct {
		From string `json:"from"`
	}
	_ = json.Unmarshal(args, &in)
	target := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(in.From), `\`, "/"), "./")
	return editor.PermissionActionEdit, target
}

func mergeResolveTarget(_ json.RawMessage, ed *editor.Editor) (editor.PermissionAction, string) {
	if ed != nil {
		if sess := ed.MergeSession(); sess != nil && sess.ScenePath != "" {
			return editor.PermissionActionEdit, sess.ScenePath
		}
	}
	return editor.PermissionActionEdit, openSceneTarget(ed)
}

func mergeFinishTarget(_ json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	return editor.PermissionActionRun, editor.PermissionRunGameCheck
}

func mergeAbortTarget(_ json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	return editor.PermissionActionRun, "merge"
}
