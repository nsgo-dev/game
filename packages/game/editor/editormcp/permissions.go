package editormcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
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
	// HeldByDisk reports that an external permissions.yaml loosening was held until confirmed in Settings › Permissions.
	HeldByDisk bool `json:"heldByDisk,omitempty"`
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

func clonePermissionEval(eval editor.PermissionEvaluation) editor.PermissionEvaluation {
	if eval.Rule != nil {
		cp := eval.Rule.Clone()
		eval.Rule = &cp
	}
	return eval
}

// LastPermissionDecision returns a copy of the most recent permission evaluation recorded by the server for a mutating or running tool call.
func (s *Server) LastPermissionDecision() (editor.PermissionEvaluation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasLastPermEval {
		return editor.PermissionEvaluation{}, false
	}
	return clonePermissionEval(s.lastPermEval), true
}

func (s *Server) recordPermissionDecision(eval editor.PermissionEvaluation) {
	stored := clonePermissionEval(eval)
	s.mu.Lock()
	s.lastPermEval = stored
	s.hasLastPermEval = true
	cb := s.onPermDecision
	s.mu.Unlock()
	if cb != nil {
		cb(clonePermissionEval(stored))
	}
}

func permissionDecisionSeverity(d editor.PermissionDecision) int {
	switch d {
	case editor.PermissionAllow:
		return 0
	case editor.PermissionAsk:
		return 1
	case editor.PermissionDeny:
		return 2
	default:
		return 3
	}
}

// checkPermission evaluates t against the editor's permission rules before t.Handler runs.
// Only read-only tools (ActionRead) skip the rule table; empty or unrecognized actions fail closed (PermissionDeny).
func (s *Server) checkPermission(ctx context.Context, t Tool, args json.RawMessage, onEditor bool) error {
	if t.Action == ActionRead {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !t.Action.Valid() {
		eval := editor.PermissionEvaluation{
			Decision:  editor.PermissionDeny,
			RuleIndex: -1,
			Reason:    fmt.Sprintf("Denied: tool %q has invalid action %q.", t.Name, t.Action),
			Link:      "Settings › Permissions",
			Tool:      t.Name,
		}
		s.recordPermissionDecision(eval)
		return permissionErrorFromEval(eval, "")
	}

	var (
		eval       editor.PermissionEvaluation
		summary    string
		reqID      string
		askOnly    bool
		youOnlyErr *YouOnlyError
		skipEval   bool
	)
	runOnEditor := func(fn func(ed *editor.Editor) error) error {
		if onEditor {
			return s.asAgent(fn)
		}
		return s.Editor(ctx, fn)
	}

	// Enforce You-only trust boundary first, then evaluate all targets and atomically record Deny or non-blocking Ask in a single editor-goroutine step.
	if err := runOnEditor(func(ed *editor.Editor) error {
		reqs := permissionRequestsForTool(t, args, ed)
		if yerr := checkYouOnlyTargets(t, args, reqs, ed); yerr != nil {
			youOnlyErr = yerr
			return nil
		}
		// game_delete_file only proposes deletions in Review changes (never deletes directly on disk),
		// so once You-only paths are refused, allow proposing even when Delete ** is Deny / tool is off.
		if t.Name == ToolGameDeleteFile {
			skipEval = true
			return nil
		}
		eval, summary = evaluateAllPermissionRequests(ed, reqs)
		switch eval.Decision {
		case editor.PermissionDeny:
			ed.RecordBlockedPermission(eval, summary)
		case editor.PermissionAsk:
			// Never invoke a potentially blocking OnPermissionAsk callback synchronously on the UI goroutine (onEditor == true).
			if onEditor || s.onPermAsk == nil {
				req := ed.EnqueuePermissionRequest(eval, summary, nil)
				reqID = req.ID
				askOnly = true
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if youOnlyErr != nil {
		return youOnlyErr
	}
	if skipEval {
		return nil
	}

	s.recordPermissionDecision(eval)

	switch eval.Decision {
	case editor.PermissionAllow:
		return nil

	case editor.PermissionDeny:
		return permissionErrorFromEval(eval, "")

	case editor.PermissionAsk:
		if askOnly {
			return permissionErrorFromEval(eval, reqID)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		ans, err := s.onPermAsk(ctx, clonePermissionEval(eval))
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
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
			deniedEval := clonePermissionEval(eval)
			deniedEval.Decision = editor.PermissionDeny
			deniedEval.Reason = "Denied by you: " + eval.Reason
			return permissionErrorFromEval(deniedEval, "")
		default:
			var pendingID string
			if err := runOnEditor(func(ed *editor.Editor) error {
				req := ed.EnqueuePermissionRequest(eval, summary, nil)
				pendingID = req.ID
				return nil
			}); err != nil {
				return err
			}
			return permissionErrorFromEval(eval, pendingID)
		}

	default:
		return permissionErrorFromEval(eval, "")
	}
}

func evaluateAllPermissionRequests(ed *editor.Editor, reqs []editor.PermissionRequest) (editor.PermissionEvaluation, string) {
	root := ""
	if ed != nil {
		root = projectRoot(ed)
	}
	var (
		strictest        editor.PermissionEvaluation
		strictestSummary string
		hasStrictest     bool
	)
	for _, req := range reqs {
		eval := evaluateSinglePermissionRequest(ed, root, req)
		if !hasStrictest || permissionDecisionSeverity(eval.Decision) > permissionDecisionSeverity(strictest.Decision) {
			strictest = eval
			strictestSummary = req.Summary
			hasStrictest = true
		}
		if eval.Decision == editor.PermissionDeny {
			return eval, req.Summary
		}
	}
	return strictest, strictestSummary
}

func evaluateSinglePermissionRequest(ed *editor.Editor, root string, req editor.PermissionRequest) editor.PermissionEvaluation {
	if req.Action != editor.PermissionActionRun && strings.TrimSpace(req.Target) != "" {
		resolved, err := editor.ResolvePermissionFileTarget(root, req.Target)
		if err != nil {
			perms := editor.DefaultPermissions()
			if ed != nil {
				perms = ed.Permissions()
			}
			return editor.PermissionEvaluation{
				Decision:  editor.PermissionDeny,
				Profile:   perms.Profile,
				Modified:  perms.IsModified(),
				RuleIndex: -1,
				RuleTotal: len(perms.Rules),
				Reason:    fmt.Sprintf("Denied: %v", err),
				Link:      "Settings › Permissions",
				Tool:      req.Tool,
				Action:    req.Action,
				Target:    strings.TrimSpace(req.Target),
			}
		}
		req.Target = resolved
	}
	if ed == nil {
		return editor.DefaultPermissions().Evaluate(req)
	}
	return ed.EvaluatePermission(req)
}

func permissionErrorFromEval(eval editor.PermissionEvaluation, reqID string) *PermissionError {
	eval = clonePermissionEval(eval)
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
		HeldByDisk: eval.HeldByDisk,
		RequestID:  reqID,
	}
}

func permissionRequestsForTool(t Tool, args json.RawMessage, ed *editor.Editor) []editor.PermissionRequest {
	if t.targets != nil {
		reqs := t.targets(args, ed)
		if len(reqs) > 0 {
			for i := range reqs {
				if reqs[i].Tool == "" {
					reqs[i].Tool = t.Name
				}
				if reqs[i].Summary == "" {
					reqs[i].Summary = formatToolCallSummary(reqs[i].Action, t.Name, reqs[i].Target)
				}
			}
			return reqs
		}
	}
	return []editor.PermissionRequest{permissionRequestForTool(t, args, ed)}
}

func permissionRequestForTool(t Tool, args json.RawMessage, ed *editor.Editor) editor.PermissionRequest {
	action, ok := mapToolAction(t.Action)
	target := ""
	if t.Target != nil {
		a, tgt := t.Target(args, ed)
		if a.Valid() {
			action = a
			ok = true
		}
		target = tgt
	} else if ok {
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

func mapToolAction(a Action) (editor.PermissionAction, bool) {
	switch a {
	case ActionEdit:
		return editor.PermissionActionEdit, true
	case ActionCreate:
		return editor.PermissionActionCreate, true
	case ActionDelete:
		return editor.PermissionActionDelete, true
	case ActionRun:
		return editor.PermissionActionRun, true
	case ActionCode:
		return editor.PermissionActionCode, true
	default:
		return "", false
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
	if f := strField("file", "path", "from"); f != "" {
		return f
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
	raw := strings.TrimSpace(in.File)
	if raw != "" && ed != nil {
		if root := projectRoot(ed); root != "" {
			if resolved, err := editor.ResolvePermissionFileTarget(root, raw); err == nil && resolved != "" {
				if _, serr := os.Stat(filepath.Join(root, filepath.FromSlash(resolved))); os.IsNotExist(serr) {
					return editor.PermissionActionCreate, resolved
				}
				return editor.PermissionActionEdit, resolved
			}
		}
	}
	return editor.PermissionActionEdit, raw
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
	raw := strings.TrimSpace(in.File)
	if raw != "" && ed != nil {
		if root := projectRoot(ed); root != "" {
			if resolved, err := editor.ResolvePermissionFileTarget(root, raw); err == nil && resolved != "" {
				if in.Review != nil && !*in.Review {
					if _, serr := os.Stat(filepath.Join(root, filepath.FromSlash(resolved))); os.IsNotExist(serr) {
						return editor.PermissionActionCreate, resolved
					}
				}
				return editor.PermissionActionEdit, resolved
			}
		}
	}
	return editor.PermissionActionEdit, raw
}

func tilesetEditTarget(args json.RawMessage, ed *editor.Editor) (editor.PermissionAction, string) {
	var in struct {
		File string `json:"file"`
	}
	_ = json.Unmarshal(args, &in)
	target := strings.TrimSpace(in.File)
	if target == "" && ed != nil {
		if ed.TilesetOpen() {
			target = ed.TilesetPath()
		} else {
			target = ed.ActiveLayerTilesetPath()
		}
	}
	return editor.PermissionActionEdit, target
}

func playtestDefineTarget(args json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	var in struct {
		File string `json:"file"`
	}
	_ = json.Unmarshal(args, &in)
	target := strings.TrimSpace(in.File)
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
	target := strings.TrimSpace(in.File)
	if target == "" {
		target = playtest.ReportFileName
	}
	return editor.PermissionActionEdit, target
}

func splitRenamePathParts(rel string) (folder, suffix string) {
	idx := strings.LastIndexByte(rel, '/')
	base := rel
	if idx >= 0 {
		folder = rel[:idx+1]
		base = rel[idx+1:]
	}
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		suffix = base[dot:]
	}
	return folder, suffix
}

func resolveRenameDestTarget(fromResolved, toRaw string) string {
	raw := strings.TrimSpace(toRaw)
	if raw == "" {
		return ""
	}
	if strings.ContainsAny(raw, `/\:`) || strings.Contains(raw, "..") {
		return raw
	}
	folder, suffix := splitRenamePathParts(fromResolved)
	if suffix != "" && strings.HasSuffix(strings.ToLower(raw), strings.ToLower(suffix)) {
		return path.Clean(raw)
	}
	// If the caller supplied an explicit filename with extension (for example "game.yaml") on a different extension, evaluate it directly too.
	if strings.ContainsRune(raw, '.') {
		return path.Clean(raw)
	}
	if fromResolved != "" {
		return path.Clean(folder + raw + suffix)
	}
	return raw
}

func refactorRenameTargets(args json.RawMessage, ed *editor.Editor) []editor.PermissionRequest {
	var in struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Apply bool   `json:"apply"`
	}
	_ = json.Unmarshal(args, &in)
	fromRaw := strings.TrimSpace(in.From)
	toRaw := strings.TrimSpace(in.To)
	var reqs []editor.PermissionRequest
	reqs = append(reqs, editor.PermissionRequest{
		Tool:   ToolRefactorRename,
		Action: editor.PermissionActionEdit,
		Target: fromRaw,
	})
	fromResolved := fromRaw
	if ed != nil && fromRaw != "" {
		if r, err := editor.ResolvePermissionFileTarget(projectRoot(ed), fromRaw); err == nil && r != "" {
			fromResolved = r
		}
	}
	if toRaw != "" {
		dest := resolveRenameDestTarget(fromResolved, toRaw)
		reqs = append(reqs, editor.PermissionRequest{
			Tool:   ToolRefactorRename,
			Action: editor.PermissionActionEdit,
			Target: dest,
		})
	}
	if in.Apply && ed != nil && fromResolved != "" {
		if rep, err := ed.FindUsages(fromResolved); err == nil && rep != nil {
			seen := map[string]bool{fromResolved: true}
			addFiles := func(groups []editor.UsageFileGroup) {
				for _, g := range groups {
					if g.File == "" || seen[g.File] {
						continue
					}
					seen[g.File] = true
					reqs = append(reqs, editor.PermissionRequest{
						Tool:   ToolRefactorRename,
						Action: editor.PermissionActionEdit,
						Target: g.File,
					})
				}
			}
			addFiles(rep.Scenes)
			addFiles(rep.Variants)
			addFiles(rep.DataTables)
			addFiles(rep.Other)
		}
	}
	return reqs
}

func mergeResolveTarget(_ json.RawMessage, ed *editor.Editor) (editor.PermissionAction, string) {
	if ed != nil {
		if sess := ed.MergeSession(); sess != nil && sess.ScenePath != "" {
			return editor.PermissionActionEdit, sess.ScenePath
		}
	}
	return editor.PermissionActionEdit, openSceneTarget(ed)
}

func mergeFinishTargets(_ json.RawMessage, ed *editor.Editor) []editor.PermissionRequest {
	sceneTarget := openSceneTarget(ed)
	if ed != nil {
		if sess := ed.MergeSession(); sess != nil && sess.ScenePath != "" {
			sceneTarget = sess.ScenePath
		}
	}
	return []editor.PermissionRequest{
		{
			Tool:   ToolMergeFinish,
			Action: editor.PermissionActionEdit,
			Target: sceneTarget,
		},
		{
			Tool:   ToolMergeFinish,
			Action: editor.PermissionActionRun,
			Target: "git merge",
		},
	}
}

func mergeAbortTarget(_ json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	return editor.PermissionActionRun, "merge"
}
