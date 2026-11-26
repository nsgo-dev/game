package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
)

const (
	maxReportStepTextBytes = 4096
	maxReportStepObjects   = 128
)

// AgentReportStepResult is the structured payload returned by agent_report_step.
type AgentReportStepResult struct {
	StepID    string   `json:"stepId"`
	Text      string   `json:"text"`
	Objects   []string `json:"objects,omitempty"`
	Status    string   `json:"status"`
	FollowUps []string `json:"followUps,omitempty"`
}

// PermissionDeniedResult is returned as non-fatal structured content when a tool call is denied
// by the person on an interactive Agent panel permission card so the agent continues without retrying it.
type PermissionDeniedResult struct {
	Denied             bool   `json:"denied"`
	AlreadyDenied      bool   `json:"alreadyDenied,omitempty"`
	BlockedByRule      bool   `json:"blockedByRule,omitempty"`
	ContinuedWithoutIt bool   `json:"continuedWithoutIt,omitempty"`
	Rule               string `json:"rule,omitempty"`
	Message            string `json:"message"`
}

func agentReportStep(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Text    string   `json:"text"`
		Objects []string `json:"objects"`
		Status  string   `json:"status"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("agent_report_step: %w", err)
	}
	text := strings.TrimSpace(args.Text)
	if text == "" {
		return nil, errors.New("agent_report_step: text must not be empty")
	}
	if strings.IndexByte(text, 0) >= 0 {
		return nil, errors.New("agent_report_step: text must not contain NUL bytes")
	}
	if len(text) > maxReportStepTextBytes {
		return nil, fmt.Errorf("agent_report_step: text exceeds maximum size (%d bytes)", maxReportStepTextBytes)
	}
	if len(args.Objects) > maxReportStepObjects {
		return nil, fmt.Errorf("agent_report_step: objects exceeds maximum count (%d)", maxReportStepObjects)
	}
	for _, obj := range args.Objects {
		if strings.IndexByte(obj, 0) >= 0 {
			return nil, errors.New("agent_report_step: object IDs must not contain NUL bytes")
		}
	}
	running := args.Status == "running"
	if args.Status != "" && args.Status != "running" && args.Status != "done" {
		return nil, fmt.Errorf("agent_report_step: invalid status %q (want running or done)", args.Status)
	}
	var (
		stepID    string
		followUps []string
		stopped   bool
	)
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.AgentStopped() {
			stopped = true
			return nil
		}
		stepID = ed.ReportAgentStep(text, args.Objects, running)
		followUps = ed.DrainAgentFollowUps()
		return nil
	})
	if err != nil {
		return nil, err
	}
	if stopped {
		return nil, errors.New("agent_report_step: the person stopped this request; stop working and leave changes for review")
	}
	status := "done"
	if running {
		status = "running"
	}
	res := AgentReportStepResult{
		StepID:    stepID,
		Text:      text,
		Objects:   args.Objects,
		Status:    status,
		FollowUps: followUps,
	}
	msg := fmt.Sprintf("Recorded step: %s", text)
	if len(followUps) > 0 {
		msg += "\nAdditional instructions from the person: " + strings.Join(followUps, " | ")
	}
	return &Result{Text: msg, Structured: res}, nil
}

type toolStepMeta struct {
	permKey     string
	runningText string
	activity    string
	askTitle    string
	askShort    string
	askWhy      string
	askEffect   string
	alwaysLabel string
}

func describeToolCall(ed *editor.Editor, t Tool, args json.RawMessage, eval editor.PermissionEvaluation) toolStepMeta {
	scene := sceneName(ed, projectRoot(ed))
	if scene == "" {
		scene = "the scene"
	}
	if ed != nil && ed.Doc != nil && ed.Doc.Name != "" && scene == "the open scene" {
		scene = ed.Doc.Name
	}

	var raw map[string]any
	_ = json.Unmarshal(args, &raw)
	strArg := func(key string) string {
		if raw == nil {
			return ""
		}
		s, _ := raw[key].(string)
		return strings.TrimSpace(s)
	}

	target := strings.TrimSpace(eval.Target)
	if target == "" {
		target = t.Name
	}
	runningText := t.Title
	if runningText == "" {
		runningText = t.Name
	}
	activity := fmt.Sprintf("Agent is running %s · manual edits stay available", t.Name)
	askTitle := fmt.Sprintf("Allow the agent to run %s?", t.Name)
	askShort := t.Name
	askWhy := fmt.Sprintf("It wants to call %s on %s.", t.Name, scene)
	askEffect := "Edits stay unsaved until you keep them."
	alwaysLabel := fmt.Sprintf("Always allow %s", t.Name)

	switch t.Name {
	case ToolSceneRead:
		file := strArg("file")
		if file == "" {
			file = scene
		}
		runningText = "Read " + file
		activity = fmt.Sprintf("Agent is reading %s · manual edits stay available", file)
		askTitle = fmt.Sprintf("Allow the agent to read %s?", file)
		askShort = "reading " + file
		askWhy = fmt.Sprintf("It wants to read %s.", file)
		askEffect = "Nothing is changed."
		alwaysLabel = "Always allow reading " + file
	case ToolSceneEdit:
		file := strArg("file")
		if file == "" {
			file = scene
		}
		runningText = "Editing " + file
		activity = fmt.Sprintf("Agent is editing %s · manual edits stay available", file)
		askTitle = fmt.Sprintf("Allow the agent to edit %s?", file)
		askShort = "editing " + file
		askWhy = fmt.Sprintf("It wants to edit %s.", file)
		askEffect = "Edits stay unsaved until you keep them."
		alwaysLabel = "Always allow editing " + file
	case ToolSelectionGet:
		runningText = "Read selection"
		activity = "Agent is checking the selection · manual edits stay available"
		askEffect = "Nothing is changed."
	case ToolViewportImage:
		runningText = "Capture viewport image"
		activity = "Agent is inspecting the viewport · manual edits stay available"
		askEffect = "Nothing is changed."
	case ToolGameCheck:
		runningText = "Run game check"
		activity = "Agent is running game check · manual edits stay available"
		askTitle = "Allow the agent to run game check?"
		askShort = "game check"
		askWhy = "It wants to check the project for broken references and errors."
		askEffect = "Nothing is saved."
		alwaysLabel = "Always allow game check"
	case ToolGamePlaytest:
		runningText = "Play headless"
		activity = fmt.Sprintf("Agent is playing %s headless · manual edits stay available", scene)
		askTitle = "Allow the agent to run the game?"
		askShort = "the playtest"
		askWhy = fmt.Sprintf("It wants to play %s headless to verify its changes.", scene)
		askEffect = "Nothing is saved."
		alwaysLabel = "Always allow headless playtests"
	case ToolStatesTest:
		runningText = "Test state machine"
		activity = "Agent is testing the state machine · manual edits stay available"
		askTitle = "Allow the agent to test the state machine?"
		askShort = "the state machine test"
		askWhy = "It wants to run scripted input against the sprite state machine."
		askEffect = "Nothing is saved."
		alwaysLabel = "Always allow state machine tests"
	default:
		if eval.Action == editor.PermissionActionRun {
			askEffect = "Nothing is saved."
			if target != "" {
				askShort = target
				askTitle = fmt.Sprintf("Allow the agent to run %s?", target)
				alwaysLabel = fmt.Sprintf("Always allow running %s", target)
			}
		} else if eval.Action != "" && target != "" {
			askShort = fmt.Sprintf("%s %s", strings.ToLower(eval.Action.DisplayName()), target)
			askTitle = fmt.Sprintf("Allow the agent to %s %s?", strings.ToLower(eval.Action.DisplayName()), target)
			askWhy = fmt.Sprintf("It wants to %s %s.", strings.ToLower(eval.Action.DisplayName()), target)
			alwaysLabel = fmt.Sprintf("Always allow %s %s", strings.ToLower(eval.Action.DisplayName()), target)
		}
	}

	permKey := ""
	if eval.Action != "" {
		permKey = string(eval.Action) + ":" + strings.ToLower(strings.TrimSpace(target))
	}
	return toolStepMeta{
		permKey:     permKey,
		runningText: runningText,
		activity:    activity,
		askTitle:    askTitle,
		askShort:    askShort,
		askWhy:      askWhy,
		askEffect:   askEffect,
		alwaysLabel: alwaysLabel,
	}
}

func summarizeToolCompletion(t Tool, meta toolStepMeta, res *Result) (text, detail string, objects []string) {
	text = meta.runningText
	switch t.Name {
	case ToolSceneRead:
		text = meta.runningText
	case ToolSceneEdit:
		if res != nil {
			if er, ok := res.Structured.(EditResult); ok {
				objects = er.Objects
				if len(er.Objects) > 0 {
					text = fmt.Sprintf("Edited %s", strings.Join(er.Objects, ", "))
				} else {
					text = "Edited scene"
				}
			}
		}
	case ToolGameCheck:
		text = "Ran game check"
		if res != nil {
			if chk, ok := res.Structured.(Check); ok {
				switch len(chk.Problems) {
				case 0:
					detail = "no problems"
				case 1:
					detail = "1 problem"
				default:
					detail = fmt.Sprintf("%d problems", len(chk.Problems))
				}
			}
		}
	case ToolGamePlaytest:
		text = "Played headless"
		if res != nil {
			if pr, ok := res.Structured.(PlaytestRunResult); ok {
				if pr.Passed {
					detail = "all goals met"
				} else {
					detail = fmt.Sprintf("%d missed", pr.MissedCount)
				}
			}
		}
	}
	return text, detail, objects
}

// invokeHandlerWithCheckpoint wraps mutating MCP tool handlers in a per-turn checkpoint when no
// turn is already open (#1018) and applies post-call privacy checks.
func (s *Server) invokeHandlerWithCheckpoint(ctx context.Context, t Tool, c *Call) (res *Result, err error) {
	startedTurn := false
	if t.Action == ActionEdit &&
		t.Name != ToolCheckpointRestore &&
		t.Name != ToolAgentTurnReply &&
		t.Name != ToolTasksSubmit &&
		t.Name != ToolTasksControl &&
		t.Name != ToolPermissionsDraft &&
		t.Name != ToolGameDeleteFile {
		kind := editor.CheckpointKindTurn
		if c.onEditor {
			kind = editor.CheckpointKindAutomation
		}
		_ = c.Editor(ctx, func(ed *editor.Editor) error {
			if ed.Project != nil && ed.ProjectPath != "" && ed.ActiveTurnID() == "" {
				if _, berr := ed.BeginCheckpointTurnWith(editor.CheckpointOptions{
					Who:   editor.HistoryAuthorAgent,
					Kind:  kind,
					Title: checkpointTitleForTool(t),
				}); berr == nil {
					startedTurn = true
				}
			}
			return nil
		})
	}
	finished := false
	if startedTurn {
		defer func() {
			cleanupCtx := context.WithoutCancel(ctx)
			_ = c.Editor(cleanupCtx, func(ed *editor.Editor) error {
				if !finished || err != nil {
					_, _ = ed.FinishCheckpointTurnOnError("interrupted")
				} else {
					_, _ = ed.EndCheckpointTurn("")
				}
				return nil
			})
		}()
	}
	res, err = t.Handler(ctx, c)
	finished = true
	if err == nil {
		if perr := s.checkPrivacyPost(ctx, t, res, c.onEditor); perr != nil {
			res, err = nil, perr
		}
	}
	return res, err
}

// runManagedToolCall enforces You-only boundaries and permission rules, streams live steps into the Agent panel,
// opens interactive non-blocking permission cards when appropriate, delivers queued follow-ups, and ties tool
// execution to the active request's Stop channel.
func (s *Server) runManagedToolCall(ctx context.Context, t Tool, args json.RawMessage, onEditor bool, session *mcp.ServerSession) (res *Result, managed bool, err error) {
	if onEditor {
		if err := s.checkPermission(ctx, t, args, true); err != nil {
			return nil, false, err
		}
		if err := s.checkPrivacyPre(ctx, t, args, true); err != nil {
			return nil, false, err
		}
		res, err = s.invokeHandlerWithCheckpoint(ctx, t, &Call{
			Args:     args,
			s:        s,
			onEditor: true,
			session:  session,
			taskID:   s.sessionTaskID(session),
		})
		return res, false, err
	}
	if t.Name == ToolAgentReportStep {
		if err := s.checkPrivacyPre(ctx, t, args, false); err != nil {
			return nil, true, err
		}
		res, err = t.Handler(ctx, &Call{
			Args:     args,
			s:        s,
			onEditor: false,
			session:  session,
			taskID:   s.sessionTaskID(session),
		})
		if err == nil {
			if perr := s.checkPrivacyPost(ctx, t, res, false); perr != nil {
				res, err = nil, perr
			}
		}
		return res, true, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if t.Action != ActionRead && !t.Action.Valid() {
		eval := editor.PermissionEvaluation{
			Decision:  editor.PermissionDeny,
			RuleIndex: -1,
			Reason:    fmt.Sprintf("Denied: tool %q has invalid action %q.", t.Name, t.Action),
			Link:      "Settings › Permissions",
			Tool:      t.Name,
		}
		s.recordPermissionDecision(eval)
		return nil, false, permissionErrorFromEval(eval, "")
	}

	var (
		meta           toolStepMeta
		eval           editor.PermissionEvaluation
		evaluated      bool
		summary        string
		reqID          string
		askOnly        bool
		useCallbackAsk bool
		youOnlyErr     *YouOnlyError
		stopped        bool
		liveActive     bool
		stepID         string
		askID          string
		askCh          <-chan editor.AgentPermissionOutcome
		stopCh         <-chan struct{}
		reDenied       bool
	)

	if err := s.Editor(ctx, func(ed *editor.Editor) error {
		if ed.AgentStopped() {
			stopped = true
			return nil
		}
		liveActive = ed.HasActiveLiveAgentRequest()
		interactiveAsk := s.onPermAsk == nil && (liveActive || s.run != nil)

		if t.Action == ActionRead {
			if liveActive {
				_, stopCh = ed.EnsureAgentRequest("")
				meta = describeToolCall(ed, t, args, editor.PermissionEvaluation{})
				stepID = ed.BeginAgentToolStep(t.Name, meta.runningText, meta.activity, nil)
			}
			return nil
		}

		// Enforce You-only trust boundary FIRST before any permission evaluation or live step.
		reqs := permissionRequestsForTool(t, args, ed)
		if yerr := checkYouOnlyTargets(t, args, reqs, ed); yerr != nil {
			youOnlyErr = yerr
			return nil
		}
		// game_delete_file proposes deletions in Review changes (never deletes directly on disk).
		if t.Name == ToolGameDeleteFile {
			if liveActive {
				_, stopCh = ed.EnsureAgentRequest("")
				meta = describeToolCall(ed, t, args, editor.PermissionEvaluation{Action: editor.PermissionActionDelete})
				stepID = ed.BeginAgentToolStep(t.Name, meta.runningText, meta.activity, nil)
			}
			return nil
		}

		eval, summary = evaluateAllPermissionRequests(ed, reqs)
		evaluated = true
		meta = describeToolCall(ed, t, args, eval)

		switch eval.Decision {
		case editor.PermissionDeny:
			ed.RecordBlockedPermission(eval, summary)
			if liveActive {
				ed.RecordBlockedAgentStep(t.Name, "Blocked: "+meta.runningText, eval)
			}
			return nil

		case editor.PermissionAsk:
			if !interactiveAsk {
				if s.onPermAsk == nil {
					req := ed.EnqueuePermissionRequest(eval, summary, nil)
					reqID = req.ID
					askOnly = true
				} else {
					useCallbackAsk = true
					if liveActive {
						_, stopCh = ed.EnsureAgentRequest("")
						stepID = ed.BeginAgentToolStep(t.Name, meta.runningText, meta.activity, nil)
					}
				}
				return nil
			}
			// Check per-request denial memory BEFORE opening a second permission card.
			if meta.permKey != "" && ed.IsPermissionDenied(meta.permKey) {
				reDenied = true
				return nil
			}
			_, stopCh = ed.EnsureAgentRequest("")
			liveActive = true
			stepID = ed.BeginAgentToolStep(t.Name, meta.runningText, meta.activity, nil)
			ask := editor.AgentPermissionPrompt{
				StepID:        stepID,
				Title:         meta.askTitle,
				ShortAction:   meta.askShort,
				Why:           meta.askWhy,
				Effect:        meta.askEffect,
				RuleSource:    eval.Reason,
				RulePreview:   editor.AlwaysRuleForEvaluation(eval).Summary(),
				AlwaysLabel:   meta.alwaysLabel,
				Tool:          t.Name,
				PermissionKey: meta.permKey,
				Evaluation:    clonePermissionEval(eval),
			}
			askCh, stopCh, _ = ed.BeginAgentPermissionAsk(ask)
			if openAsk := ed.PendingAgentPermissionAsk(); openAsk != nil {
				askID = openAsk.ID
			}
			return nil

		default: // PermissionAllow
			if liveActive {
				_, stopCh = ed.EnsureAgentRequest("")
				stepID = ed.BeginAgentToolStep(t.Name, meta.runningText, meta.activity, nil)
			}
			return nil
		}
	}); err != nil {
		return nil, liveActive, err
	}

	if stopped {
		return nil, true, fmt.Errorf("%s: stopped by the person; leave changes for review", t.Name)
	}
	if youOnlyErr != nil {
		return nil, liveActive, youOnlyErr
	}
	if evaluated {
		s.recordPermissionDecision(eval)
	}
	if evaluated && eval.Decision == editor.PermissionDeny {
		return nil, liveActive, permissionErrorFromEval(eval, "")
	}
	if askOnly {
		return nil, liveActive, permissionErrorFromEval(eval, reqID)
	}
	if reDenied {
		msg := fmt.Sprintf("Permission for %s was already denied in this request. Do not retry it automatically; continue without this step.", t.Name)
		return &Result{
			Text: msg,
			Structured: PermissionDeniedResult{
				Denied:             true,
				AlreadyDenied:      true,
				ContinuedWithoutIt: true,
				Message:            msg,
			},
		}, true, nil
	}

	// Non-interactive OnPermissionAsk callback path (#1010).
	if useCallbackAsk {
		if err := ctx.Err(); err != nil {
			return nil, liveActive, err
		}
		ans, askErr := s.onPermAsk(ctx, clonePermissionEval(eval))
		if askErr != nil {
			return nil, liveActive, askErr
		}
		if err := ctx.Err(); err != nil {
			return nil, liveActive, err
		}
		switch ans {
		case PermissionAnswerAllowOnce:
		case PermissionAnswerAllowAlways:
			if err := s.Editor(ctx, func(ed *editor.Editor) error {
				_, err := ed.ApproveAlwaysPermission(eval)
				return err
			}); err != nil {
				return nil, liveActive, err
			}
		case PermissionAnswerDeny:
			deniedEval := clonePermissionEval(eval)
			deniedEval.Decision = editor.PermissionDeny
			deniedEval.Reason = "Denied by you: " + eval.Reason
			return nil, liveActive, permissionErrorFromEval(deniedEval, "")
		default:
			var pendingID string
			if err := s.Editor(ctx, func(ed *editor.Editor) error {
				req := ed.EnqueuePermissionRequest(eval, summary, nil)
				pendingID = req.ID
				return nil
			}); err != nil {
				return nil, liveActive, err
			}
			return nil, liveActive, permissionErrorFromEval(eval, pendingID)
		}
	}

	// Interactive Agent panel permission card wait (runs on MCP goroutine, never blocks UI).
	if askCh != nil {
		var outcome editor.AgentPermissionOutcome
		select {
		case outcome = <-askCh:
		case <-stopCh:
			outcome = editor.PermissionOutcomeStopped
		case <-ctx.Done():
			if askID != "" {
				s.notifyEditor(func(ed *editor.Editor) {
					ed.CancelPendingAgentPermissionAsk(askID)
				})
			}
			return nil, true, ctx.Err()
		case <-s.done:
			return nil, true, errors.New("editormcp: the editor's server is closed")
		}
		switch outcome {
		case editor.PermissionOutcomeDeny:
			s.attachSafeAllowNow(stepID, t, args, meta, session)
			msg := fmt.Sprintf("The person denied %s. Continue without this step and do not retry it unless they send a new request.", meta.askShort)
			return &Result{
				Text: msg,
				Structured: PermissionDeniedResult{
					Denied:             true,
					ContinuedWithoutIt: true,
					Message:            msg,
				},
			}, true, nil
		case editor.PermissionOutcomeStopped:
			return nil, true, fmt.Errorf("%s: stopped by the person; changes so far stay pending for review", t.Name)
		}
	}

	// Tie tool execution to the request's stop channel so Stop / Mod+. cancels in-flight work immediately.
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if stopCh != nil {
		go func() {
			select {
			case <-stopCh:
				cancel()
			case <-callCtx.Done():
			}
		}()
	}

	if err = s.checkPrivacyPre(callCtx, t, args, false); err == nil {
		res, err = s.invokeHandlerWithCheckpoint(callCtx, t, &Call{
			Args:     args,
			s:        s,
			onEditor: false,
			session:  session,
			taskID:   s.sessionTaskID(session),
		})
	}

	if stopCh != nil {
		select {
		case <-stopCh:
			return nil, true, fmt.Errorf("%s: stopped by the person; changes so far stay pending for review", t.Name)
		default:
		}
	}

	if stepID != "" {
		var followUps []string
		doneText, doneDetail, objs := summarizeToolCompletion(t, meta, res)
		if err != nil {
			doneDetail = err.Error()
		}
		_ = s.Editor(context.Background(), func(ed *editor.Editor) error {
			ed.FinishAgentToolStep(stepID, doneText, doneDetail, objs, err != nil)
			followUps = ed.DrainAgentFollowUps()
			return nil
		})
		if err == nil && len(followUps) > 0 && res != nil {
			note := "\n[Additional instructions added to this request: " + strings.Join(followUps, " | ") + "]"
			res.Text += note
		}
	}

	return res, liveActive || stepID != "", err
}

// attachSafeAllowNow binds an on-demand "Allow now" callback on a denied step that:
// - never leaks goroutines past Server.Close (tracked by s.wg and cancelled on s.done),
// - refuses to run after the editor switches projects (projectGen check),
// - re-verifies You-only trust boundaries before invoking t.Handler.
func (s *Server) attachSafeAllowNow(stepID string, t Tool, args json.RawMessage, meta toolStepMeta, session *mcp.ServerSession) {
	if stepID == "" {
		return
	}
	argsCopy := append(json.RawMessage(nil), args...)
	s.mu.Lock()
	capturedGen := s.projectGen
	s.mu.Unlock()
	_ = s.Editor(context.Background(), func(ed *editor.Editor) error {
		ed.AttachDeniedStepAllowNow(stepID, func() {
			s.mu.Lock()
			if s.closed || s.projectGen != capturedGen {
				s.mu.Unlock()
				return
			}
			s.wg.Add(1)
			s.mu.Unlock()

			go func() {
				defer s.wg.Done()
				runCtx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					select {
					case <-s.done:
						cancel()
					case <-runCtx.Done():
					}
				}()

				// Defense in depth: re-check You-only boundaries before executing on demand.
				var yerr *YouOnlyError
				if err := s.Editor(runCtx, func(ed *editor.Editor) error {
					reqs := permissionRequestsForTool(t, argsCopy, ed)
					yerr = checkYouOnlyTargets(t, argsCopy, reqs, ed)
					return nil
				}); err != nil {
					return
				}
				var (
					res    *Result
					runErr error
				)
				if yerr != nil {
					runErr = yerr
				} else if !s.isToolEnabled(t.Name) {
					runErr = toolDisabledError(t.Name)
				} else if perr := s.checkPrivacyPre(runCtx, t, argsCopy, false); perr != nil {
					runErr = perr
				} else {
					res, runErr = s.invokeHandlerWithCheckpoint(runCtx, t, &Call{
						Args:     argsCopy,
						s:        s,
						onEditor: false,
						session:  session,
						taskID:   s.sessionTaskID(session),
					})
				}
				doneText, doneDetail, objs := summarizeToolCompletion(t, meta, res)
				if runErr != nil {
					doneDetail = runErr.Error()
				}
				_ = s.Editor(context.Background(), func(ed *editor.Editor) error {
					ed.FinishAgentToolStep(stepID, doneText, doneDetail, objs, runErr != nil)
					return nil
				})
			}()
		})
		return nil
	})
}
