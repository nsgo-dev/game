package editor

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const (
	maxLiveAgentStepsPerTurn = 512
	maxLiveAgentFollowUps    = 64
	maxAgentStepTextBytes    = 4096
	maxAgentFollowUpBytes    = 32 << 10
	maxAgentStepObjects      = 128
)

// AgentPermissionOutcome is the person's answer to an interactive Agent panel permission card (or Stop interrupting it).
type AgentPermissionOutcome string

const (
	PermissionOutcomeAllowOnce   AgentPermissionOutcome = "allow_once"
	PermissionOutcomeAllowAlways AgentPermissionOutcome = "allow_always"
	PermissionOutcomeDeny        AgentPermissionOutcome = "deny"
	PermissionOutcomeStopped     AgentPermissionOutcome = "stopped"
)

// AgentPermissionPrompt describes an open interactive permission card rendered inside the Agent panel.
type AgentPermissionPrompt struct {
	ID            string
	StepID        string
	Title         string
	ShortAction   string
	Why           string
	Effect        string
	RuleSource    string
	RulePreview   string
	AlwaysLabel   string
	Tool          string
	PermissionKey string
	Evaluation    PermissionEvaluation
	AskedAt       time.Time
}

// AgentFollowUp is an additional instruction sent via "Add to this request" while an agent request is running.
type AgentFollowUp struct {
	Text   string    `json:"text"`
	SentAt time.Time `json:"sentAt"`
}

// AgentPendingReview summarizes completed steps and edits left pending for review after a turn finishes or is stopped.
type AgentPendingReview struct {
	StepsDone     int      `json:"stepsDone"`
	ChangesCount  int      `json:"changesCount"`
	Summary       string   `json:"summary,omitempty"`
	NotChecked    []string `json:"notChecked,omitempty"`
	Stopped       bool     `json:"stopped,omitempty"`
	StoppedBefore string   `json:"stoppedBefore,omitempty"`
	Kept          bool     `json:"kept,omitempty"`
	Reverted      bool     `json:"reverted,omitempty"`
}

// AgentSessionSnapshot is an immutable snapshot of the active or most recent Agent panel turn and live permission card.
type AgentSessionSnapshot struct {
	RequestID       uint64
	TurnID          string
	Status          AgentStatus
	Stopped         bool
	Prompt          string
	Steps           []AgentStep
	PendingAsk      *AgentPermissionPrompt
	FollowUps       []AgentFollowUp
	Review          *AgentPendingReview
	Activity        string
	StartedAt       time.Time
	Elapsed         time.Duration
	WaitingElapsed  time.Duration
	DeniedKeysCount int
}

type agentPendingAskHandle struct {
	ask      AgentPermissionPrompt
	respCh   chan AgentPermissionOutcome
	resolved bool
}

func cloneAgentPermissionPrompt(ask AgentPermissionPrompt) AgentPermissionPrompt {
	if ask.Evaluation.Rule != nil {
		cp := ask.Evaluation.Rule.Clone()
		ask.Evaluation.Rule = &cp
	}
	return ask
}

func dropTurnPlaceholder(turn *AgentTurn) {
	if turn == nil || len(turn.Steps) == 0 {
		return
	}
	out := turn.Steps[:0]
	for _, s := range turn.Steps {
		if s.Status != AgentStepPlaceholder {
			out = append(out, s)
		}
	}
	turn.Steps = out
}

func completedTurnStepCount(turn *AgentTurn) int {
	if turn == nil {
		return 0
	}
	n := 0
	for _, s := range turn.Steps {
		if s.Status == AgentStepDone {
			n++
		}
	}
	return n
}

func (e *Editor) latestAgentTurnPtr() *AgentTurn {
	if e == nil || len(e.agent.turns) == 0 {
		return nil
	}
	return &e.agent.turns[len(e.agent.turns)-1]
}

func (e *Editor) closeLiveStopChLocked() {
	if e.agent.liveStopCh == nil {
		return
	}
	select {
	case <-e.agent.liveStopCh:
	default:
		close(e.agent.liveStopCh)
	}
}

func (e *Editor) resolvePendingLiveAskLocked(outcome AgentPermissionOutcome) {
	if e.agent.livePendingAsk == nil || e.agent.livePendingAsk.resolved {
		return
	}
	e.agent.livePendingAsk.resolved = true
	select {
	case e.agent.livePendingAsk.respCh <- outcome:
	default:
	}
	e.agent.livePendingAsk = nil
	e.agent.livePermCardFocused = false
	if e.agent.openRequest != nil && strings.HasPrefix(e.agent.openRequest.ID, "ask-") {
		e.agent.openRequest = nil
	}
}

// HasActiveLiveAgentRequest reports whether an interactive agent request (started via StartAgentRequest or SubmitAgentPrompt) is currently active.
func (e *Editor) HasActiveLiveAgentRequest() bool {
	if e == nil {
		return false
	}
	return e.agent.liveRequestActive || e.agent.activeRunCancel != nil
}

// AgentStopped reports whether the most recent live request was stopped by the user and has not yet been superseded by a new request.
func (e *Editor) AgentStopped() bool {
	if e == nil {
		return false
	}
	return e.agent.liveStopped && !e.agent.liveRequestActive && e.agent.activeRunCancel == nil
}

// PendingAgentPermissionAsk returns a copy of the currently open interactive permission card, or nil.
func (e *Editor) PendingAgentPermissionAsk() *AgentPermissionPrompt {
	if e == nil || e.agent.livePendingAsk == nil || e.agent.livePendingAsk.resolved {
		return nil
	}
	cp := cloneAgentPermissionPrompt(e.agent.livePendingAsk.ask)
	return &cp
}

// AgentStatusBarText returns the bottom status-bar message describing what the agent is currently doing
// while reminding the person that manual edits stay available, or empty when the agent is idle.
func (e *Editor) AgentStatusBarText() string {
	if e == nil {
		return ""
	}
	switch e.agent.status {
	case AgentStatusWaitingForYou:
		if e.agent.livePendingAsk != nil && !e.agent.livePendingAsk.resolved && e.agent.livePendingAsk.ask.ShortAction != "" {
			return fmt.Sprintf("Waiting for you · %s · manual edits stay available", e.agent.livePendingAsk.ask.ShortAction)
		}
		if e.agent.openRequest != nil && e.agent.openRequest.Title != "" {
			return fmt.Sprintf("Waiting for you · %s · manual edits stay available", e.agent.openRequest.Title)
		}
		return "Waiting for you · manual edits stay available"
	case AgentStatusWorking:
		if e.agent.liveActivity != "" {
			return e.agent.liveActivity
		}
		return "Agent is working · manual edits stay available"
	default:
		return ""
	}
}

// AgentSession returns an immutable snapshot of the active or latest live Agent panel turn and permission state.
func (e *Editor) AgentSession() AgentSessionSnapshot {
	if e == nil {
		return AgentSessionSnapshot{Status: AgentStatusNotConnected}
	}
	now := e.nowTime()
	snap := AgentSessionSnapshot{
		RequestID:       e.agent.liveRequestID,
		Status:          e.AgentStatus(),
		Stopped:         e.AgentStopped(),
		Activity:        e.agent.liveActivity,
		DeniedKeysCount: len(e.agent.liveDeniedKeys),
	}
	if turn := e.latestAgentTurnPtr(); turn != nil {
		cp := cloneAgentTurn(*turn)
		snap.TurnID = cp.ID
		snap.Prompt = cp.Prompt
		snap.Steps = cp.Steps
		snap.FollowUps = cp.FollowUps
		snap.Review = cp.PendingReview
		snap.StartedAt = cp.StartedAt
		if !cp.StartedAt.IsZero() && (snap.Status == AgentStatusWorking || snap.Status == AgentStatusWaitingForYou) {
			snap.Elapsed = max(0, now.Sub(cp.StartedAt))
		}
	}
	if ask := e.PendingAgentPermissionAsk(); ask != nil {
		snap.PendingAsk = ask
		if !ask.AskedAt.IsZero() {
			snap.WaitingElapsed = max(0, now.Sub(ask.AskedAt))
		}
	}
	return snap
}

// StartAgentRequest begins a new live agent request in the Agent panel, clears prior per-request denials
// so the agent may ask again on a new request, and immediately displays a gray placeholder step (within 300 ms).
func (e *Editor) StartAgentRequest(prompt string) uint64 {
	if e == nil {
		return 0
	}
	prompt = strings.TrimSpace(prompt)
	if len(prompt) > maxAgentFollowUpBytes {
		prompt = prompt[:maxAgentFollowUpBytes]
	}
	if e.agent.activeRunCancel != nil {
		e.agent.activeRunID++
		cancel := e.agent.activeRunCancel
		e.agent.activeRunCancel = nil
		cancel()
	}
	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	e.closeLiveStopChLocked()

	now := e.nowTime()
	e.agent.liveRequestID++
	e.agent.turnSeq++
	e.agent.turnChangeSeq++
	e.agent.liveStepSeq++
	turnID := fmt.Sprintf("turn-%d", e.agent.turnSeq)
	startHist := 0
	if e.History != nil {
		startHist = e.History.Index()
	}
	clientName := ""
	if c, ok := e.AgentPrimaryClient(); ok {
		clientName = c.DisplayName
	}
	e.agent.turns = append(e.agent.turns, AgentTurn{
		ID:        turnID,
		Prompt:    prompt,
		Source:    "panel",
		Status:    "running",
		AgentName: clientName,
		Steps: []AgentStep{{
			ID:        fmt.Sprintf("step-%d", e.agent.liveStepSeq),
			Title:     "Starting request…",
			Status:    AgentStepPlaceholder,
			StartedAt: now,
		}},
		CreatedAt:           now,
		StartedAt:           now,
		turnStartHistoryIdx: startHist,
	})
	e.trimAgentTurnAttachments()
	e.initTurnBaseline(&e.agent.turns[len(e.agent.turns)-1])
	e.agent.liveRequestActive = true
	e.agent.liveStopped = false
	e.agent.liveStopCh = make(chan struct{})
	e.agent.liveDeniedKeys = make(map[string]bool)
	e.agent.liveQueuedFollowUps = nil
	e.agent.liveActivity = "Agent is working · manual edits stay available"
	e.agent.rewindOpenTurnID = ""
	e.agent.lastRewind = nil
	e.agent.composerHighlighted = false
	e.agent.status = AgentStatusWorking
	e.agent.workingStep = 1
	e.agent.workingTotal = 0
	e.updateAgentAnnouncement()
	e.notify()
	if e.OnStartAgentRequest != nil && prompt != "" {
		e.OnStartAgentRequest(prompt)
	}
	return e.agent.liveRequestID
}

// EnsureAgentRequest returns the active request ID and stop channel, or starts an implicit live request
// when an external MCP client begins calling tools while the editor is idle.
func (e *Editor) EnsureAgentRequest(prompt string) (uint64, <-chan struct{}) {
	if e == nil {
		return 0, nil
	}
	if e.agent.liveRequestActive || e.agent.activeRunCancel != nil {
		if e.agent.liveStopCh == nil {
			e.agent.liveStopCh = make(chan struct{})
		}
		return e.agent.liveRequestID, e.agent.liveStopCh
	}
	if e.AgentStopped() {
		return e.agent.liveRequestID, e.agent.liveStopCh
	}
	id := e.StartAgentRequest(prompt)
	return id, e.agent.liveStopCh
}

func (e *Editor) ensureLiveTurnLocked() *AgentTurn {
	turn := e.latestAgentTurnPtr()
	if turn == nil || turn.Done || turn.Stopped {
		e.StartAgentRequest("")
		turn = e.latestAgentTurnPtr()
	}
	return turn
}

// BeginAgentToolStep appends a new active running step (with spinner) for a tool call,
// removing the initial placeholder step and enforcing at most one running spinner at a time.
func (e *Editor) BeginAgentToolStep(tool, text, activity string, objects []string) string {
	if e == nil {
		return ""
	}
	turn := e.ensureLiveTurnLocked()
	if turn == nil {
		return ""
	}
	dropTurnPlaceholder(turn)
	// Reuse an already-running step for the same tool (e.g., emitted by builtInAgentRunner
	// immediately before invoking the MCP tool) instead of duplicating it.
	if tool != "" && len(turn.Steps) > 0 {
		last := &turn.Steps[len(turn.Steps)-1]
		if last.Status == AgentStepRunning && last.ToolName == tool {
			if t := strings.TrimSpace(text); t != "" {
				last.Title = t
			}
			if cleanObjs := sanitizeStepObjects(objects); len(cleanObjs) > 0 {
				last.Objects = cleanObjs
			}
			e.agent.liveRequestActive = true
			e.agent.status = AgentStatusWorking
			e.agent.workingStep = max(1, len(turn.Steps))
			if activity != "" {
				e.agent.liveActivity = activity
			} else if text != "" {
				e.agent.liveActivity = fmt.Sprintf("Agent: %s · manual edits stay available", text)
			}
			e.agent.turnChangeSeq++
			e.updateAgentAnnouncement()
			e.notify()
			return last.ID
		}
	}
	for i := range turn.Steps {
		if turn.Steps[i].Status == AgentStepRunning {
			turn.Steps[i].Status = AgentStepDone
		}
	}
	e.agent.liveStepSeq++
	id := fmt.Sprintf("step-%d", e.agent.liveStepSeq)
	if len(turn.Steps) >= maxLiveAgentStepsPerTurn {
		turn.Steps = turn.Steps[len(turn.Steps)-maxLiveAgentStepsPerTurn+1:]
	}
	turn.Steps = append(turn.Steps, AgentStep{
		ID:        id,
		Title:     strings.TrimSpace(text),
		ToolName:  tool,
		Objects:   sanitizeStepObjects(objects),
		Status:    AgentStepRunning,
		StartedAt: e.nowTime(),
	})
	e.agent.liveRequestActive = true
	e.agent.status = AgentStatusWorking
	e.agent.workingStep = max(1, len(turn.Steps))
	if activity != "" {
		e.agent.liveActivity = activity
	} else if text != "" {
		e.agent.liveActivity = fmt.Sprintf("Agent: %s · manual edits stay available", text)
	}
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.notify()
	return id
}

// FinishAgentToolStep marks stepID done (or failed) with its plain-words result summary.
func (e *Editor) FinishAgentToolStep(stepID, text, detail string, objects []string, failed bool) {
	if e == nil || stepID == "" {
		return
	}
	turn := e.latestAgentTurnPtr()
	if turn == nil {
		return
	}
	for i := range turn.Steps {
		if turn.Steps[i].ID != stepID {
			continue
		}
		if turn.Steps[i].Status == AgentStepStopped || turn.Steps[i].Status == AgentStepDenied || turn.Steps[i].Status == AgentStepBlocked {
			return
		}
		if strings.TrimSpace(text) != "" {
			turn.Steps[i].Title = strings.TrimSpace(text)
		}
		if detail != "" {
			turn.Steps[i].Detail = detail
		}
		if cleanObjs := sanitizeStepObjects(objects); len(cleanObjs) > 0 {
			turn.Steps[i].Objects = cleanObjs
		}
		if failed {
			turn.Steps[i].Status = AgentStepFailed
		} else {
			turn.Steps[i].Status = AgentStepDone
		}
		break
	}
	if e.agent.status == AgentStatusWorking {
		e.agent.liveActivity = "Agent is working · manual edits stay available"
	}
	e.agent.turnChangeSeq++
	e.notify()
}

// ReportAgentStep records a step reported directly by the agent in its own words (via agent_report_step),
// along with the scene object IDs it touches.
func (e *Editor) ReportAgentStep(text string, objects []string, running bool) string {
	if e == nil {
		return ""
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.IndexByte(text, 0) >= 0 {
		return ""
	}
	if len(text) > maxAgentStepTextBytes {
		text = text[:maxAgentStepTextBytes]
	}
	turn := e.ensureLiveTurnLocked()
	if turn == nil {
		return ""
	}
	dropTurnPlaceholder(turn)
	if running {
		for i := range turn.Steps {
			if turn.Steps[i].Status == AgentStepRunning {
				turn.Steps[i].Status = AgentStepDone
			}
		}
	}
	e.agent.liveStepSeq++
	id := fmt.Sprintf("step-%d", e.agent.liveStepSeq)
	st := AgentStepDone
	if running {
		st = AgentStepRunning
		e.agent.liveActivity = fmt.Sprintf("Agent: %s · manual edits stay available", text)
	}
	if len(turn.Steps) >= maxLiveAgentStepsPerTurn {
		turn.Steps = turn.Steps[len(turn.Steps)-maxLiveAgentStepsPerTurn+1:]
	}
	turn.Steps = append(turn.Steps, AgentStep{
		ID:        id,
		Title:     text,
		ToolName:  "agent_report_step",
		Objects:   sanitizeStepObjects(objects),
		Status:    st,
		StartedAt: e.nowTime(),
	})
	e.agent.workingStep = max(1, len(turn.Steps))
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.notify()
	return id
}

// RecordBlockedAgentStep records a step stopped by a Deny permission rule in the live Agent panel thread,
// keeping its RuleIndex so clicking it opens Settings › Permissions at the exact matching rule.
func (e *Editor) RecordBlockedAgentStep(tool, text string, eval PermissionEvaluation) string {
	if e == nil {
		return ""
	}
	turn := e.ensureLiveTurnLocked()
	if turn == nil {
		return ""
	}
	dropTurnPlaceholder(turn)
	e.agent.liveStepSeq++
	id := fmt.Sprintf("step-%d", e.agent.liveStepSeq)
	ruleSummary := eval.Reason
	if eval.Rule != nil {
		ruleSummary = eval.Rule.Summary()
	}
	if len(turn.Steps) >= maxLiveAgentStepsPerTurn {
		turn.Steps = turn.Steps[len(turn.Steps)-maxLiveAgentStepsPerTurn+1:]
	}
	turn.Steps = append(turn.Steps, AgentStep{
		ID:          id,
		Title:       strings.TrimSpace(text),
		ToolName:    tool,
		RuleSummary: ruleSummary,
		RuleIndex:   eval.RuleIndex,
		Status:      AgentStepBlocked,
		StartedAt:   e.nowTime(),
	})
	e.agent.turnChangeSeq++
	e.notify()
	return id
}

// BeginAgentPermissionAsk installs an open permission card inside the Agent panel for ask.
// If ask.PermissionKey was already denied in the active request, it returns (nil, stopCh, true) immediately
// without showing a card so denials are never retried automatically.
func (e *Editor) BeginAgentPermissionAsk(ask AgentPermissionPrompt) (respCh <-chan AgentPermissionOutcome, stopCh <-chan struct{}, alreadyDenied bool) {
	if e == nil {
		return nil, nil, false
	}
	if e.AgentStopped() {
		ch := make(chan AgentPermissionOutcome, 1)
		ch <- PermissionOutcomeStopped
		return ch, e.agent.liveStopCh, false
	}
	turn := e.ensureLiveTurnLocked()
	if e.agent.liveStopCh == nil {
		e.agent.liveStopCh = make(chan struct{})
	}
	if ask.PermissionKey != "" && e.agent.liveDeniedKeys[ask.PermissionKey] {
		return nil, e.agent.liveStopCh, true
	}
	// If another ask is somehow still open, resolve it cleanly before replacing.
	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)

	dropTurnPlaceholder(turn)
	e.agent.liveAskSeq++
	if ask.ID == "" {
		ask.ID = fmt.Sprintf("ask-%d", e.agent.liveAskSeq)
	}
	if ask.AskedAt.IsZero() {
		ask.AskedAt = e.nowTime()
	}
	if ask.RulePreview == "" {
		if rule := AlwaysRuleForEvaluation(ask.Evaluation); len(rule.Patterns) > 0 {
			ask.RulePreview = rule.Summary()
		}
	}
	if ask.RuleSource == "" {
		ask.RuleSource = ask.Evaluation.Reason
	}
	if turn != nil && ask.StepID != "" {
		for i := range turn.Steps {
			if turn.Steps[i].ID == ask.StepID {
				turn.Steps[i].Status = AgentStepWaiting
				turn.Steps[i].Detail = "waiting for you"
				turn.Steps[i].PermissionKey = ask.PermissionKey
				break
			}
		}
	}
	ch := make(chan AgentPermissionOutcome, 1)
	stored := cloneAgentPermissionPrompt(ask)
	e.agent.livePendingAsk = &agentPendingAskHandle{
		ask:    stored,
		respCh: ch,
	}
	// Sync AgentOpenRequest so the header status chip renders "Waiting for you · M:SS".
	e.agent.openRequest = &AgentOpenRequest{
		ID:       stored.ID,
		Title:    stored.Title,
		Body:     stored.Why,
		OpenedAt: stored.AskedAt,
	}
	e.agent.status = AgentStatusWaitingForYou
	// Spec rule 05: only auto-focus the permission card when focus is already inside the Agent panel
	// and neither a drag nor text input is active.
	if e.ShouldFocusAgentPrompt() {
		e.agent.liveFocusCardSeq++
	}
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.notify()
	return ch, e.agent.liveStopCh, false
}

// CancelPendingAgentPermissionAsk dismisses the open permission card if its ID still matches askID
// (used when an MCP client cancels or disconnects while waiting on a permission card).
func (e *Editor) CancelPendingAgentPermissionAsk(askID string) {
	if e == nil || askID == "" || e.agent.livePendingAsk == nil || e.agent.livePendingAsk.resolved {
		return
	}
	if e.agent.livePendingAsk.ask.ID != askID {
		return
	}
	stepID := e.agent.livePendingAsk.ask.StepID
	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	if turn := e.latestAgentTurnPtr(); turn != nil && stepID != "" {
		for i := range turn.Steps {
			if turn.Steps[i].ID == stepID && turn.Steps[i].Status == AgentStepWaiting {
				turn.Steps[i].Status = AgentStepFailed
				turn.Steps[i].Detail = "canceled"
				break
			}
		}
	}
	if e.agent.liveRequestActive || e.agent.activeRunCancel != nil {
		e.agent.status = AgentStatusWorking
	} else {
		e.restoreIdleAgentStatus()
	}
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.notify()
}

// FocusPermissionCard opens the Agent panel and requests keyboard focus for the active permission card
// (for example when clicking the header "Waiting for you" chip).
func (e *Editor) FocusPermissionCard() {
	if e == nil || e.agent.livePendingAsk == nil || e.agent.livePendingAsk.resolved {
		return
	}
	if e.agent.mode != AgentModeNone {
		e.agent.rightTab = RightTabAgent
	}
	e.agent.liveFocusCardSeq++
	e.agent.focusRequestCalled++
	if e.OnFocusAgentRequest != nil && e.agent.openRequest != nil {
		e.OnFocusAgentRequest(*e.agent.openRequest)
	}
	e.notify()
}

// AllowPermissionOnce approves the open permission card for this single call only.
func (e *Editor) AllowPermissionOnce() bool {
	if e == nil || e.agent.livePendingAsk == nil || e.agent.livePendingAsk.resolved {
		return false
	}
	ask := cloneAgentPermissionPrompt(e.agent.livePendingAsk.ask)
	e.resolvePendingLiveAskLocked(PermissionOutcomeAllowOnce)
	e.updateLiveAskStepRunningLocked(ask, "allowed once")
	e.agent.status = AgentStatusWorking
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.notify()
	return true
}

// AllowPermissionAlways inserts and persists the concrete Allow rule into permissions.yaml via ApproveAlwaysPermission
// (which also displays the "Always permission rule saved" toast with Show and Undo) and approves the waiting call.
func (e *Editor) AllowPermissionAlways() bool {
	if e == nil || e.agent.livePendingAsk == nil || e.agent.livePendingAsk.resolved {
		return false
	}
	ask := cloneAgentPermissionPrompt(e.agent.livePendingAsk.ask)
	if _, err := e.ApproveAlwaysPermission(ask.Evaluation); err != nil {
		e.report(err)
		return false
	}
	e.resolvePendingLiveAskLocked(PermissionOutcomeAllowAlways)
	e.updateLiveAskStepRunningLocked(ask, "allowed by your new rule")
	e.agent.status = AgentStatusWorking
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.notify()
	return true
}

// DenyPermission denies the open permission card, records the denial for this request so the agent
// will never retry it automatically, marks the step with "Allow now", and lets the agent continue.
func (e *Editor) DenyPermission() bool {
	if e == nil || e.agent.livePendingAsk == nil || e.agent.livePendingAsk.resolved {
		return false
	}
	ask := cloneAgentPermissionPrompt(e.agent.livePendingAsk.ask)
	e.resolvePendingLiveAskLocked(PermissionOutcomeDeny)
	if e.agent.liveDeniedKeys == nil {
		e.agent.liveDeniedKeys = make(map[string]bool)
	}
	if ask.PermissionKey != "" {
		e.agent.liveDeniedKeys[ask.PermissionKey] = true
	}
	deniedLabel, unchecked := deniedStepSummary(ask)
	if turn := e.latestAgentTurnPtr(); turn != nil {
		if unchecked != "" && !slices.Contains(turn.NotChecked, unchecked) {
			turn.NotChecked = append(turn.NotChecked, unchecked)
		}
		for i := range turn.Steps {
			if turn.Steps[i].ID == ask.StepID {
				turn.Steps[i].Status = AgentStepDenied
				turn.Steps[i].Title = deniedLabel
				turn.Steps[i].Detail = "continued without it"
				turn.Steps[i].CanAllowNow = true
				turn.Steps[i].PermissionKey = ask.PermissionKey
				turn.Steps[i].UncheckedNote = unchecked
				break
			}
		}
	}
	e.agent.status = AgentStatusWorking
	e.agent.liveActivity = "Agent is continuing without it · manual edits stay available"
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.notify()
	return true
}

// AttachDeniedStepAllowNow binds the on-demand "Allow now" callback for a denied step.
func (e *Editor) AttachDeniedStepAllowNow(stepID string, fn func()) {
	if e == nil || stepID == "" {
		return
	}
	for ti := len(e.agent.turns) - 1; ti >= 0; ti-- {
		turn := &e.agent.turns[ti]
		for si := range turn.Steps {
			if turn.Steps[si].ID == stepID {
				turn.Steps[si].allowNowFunc = fn
				turn.Steps[si].CanAllowNow = true
				return
			}
		}
	}
}

// AllowDeniedStepNow lets the person explicitly approve and run a previously denied step from its step row.
func (e *Editor) AllowDeniedStepNow(stepID string) bool {
	if e == nil || stepID == "" {
		return false
	}
	for ti := len(e.agent.turns) - 1; ti >= 0; ti-- {
		turn := &e.agent.turns[ti]
		for si := range turn.Steps {
			if turn.Steps[si].ID != stepID || turn.Steps[si].Status != AgentStepDenied {
				continue
			}
			if key := turn.Steps[si].PermissionKey; key != "" {
				delete(e.agent.liveDeniedKeys, key)
			}
			if note := turn.Steps[si].UncheckedNote; note != "" {
				turn.NotChecked = slices.DeleteFunc(turn.NotChecked, func(s string) bool { return s == note })
				if turn.PendingReview != nil {
					turn.PendingReview.NotChecked = slices.DeleteFunc(turn.PendingReview.NotChecked, func(s string) bool { return s == note })
				}
			}
			fn := turn.Steps[si].allowNowFunc
			turn.Steps[si].Status = AgentStepRunning
			turn.Steps[si].Detail = "allowed once, just now"
			turn.Steps[si].CanAllowNow = false
			e.agent.liveActivity = "Agent is running allowed step · manual edits stay available"
			e.agent.turnChangeSeq++
			e.notify()
			if fn != nil {
				fn()
			}
			return true
		}
	}
	return false
}

// AddToAgentRequest appends additional instructions to the running or waiting request
// and queues them for the MCP client.
func (e *Editor) AddToAgentRequest(text string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.IndexByte(text, 0) >= 0 {
		return errors.New("editor: follow-up instruction must not contain NUL bytes")
	}
	if len(text) > maxAgentFollowUpBytes {
		return fmt.Errorf("editor: follow-up instruction exceeds maximum size (%d bytes)", maxAgentFollowUpBytes)
	}
	if !e.AgentWorking() {
		return errors.New("editor: no active agent request")
	}
	if turn := e.latestAgentTurnPtr(); turn != nil {
		if len(turn.FollowUps) < maxLiveAgentFollowUps {
			turn.FollowUps = append(turn.FollowUps, AgentFollowUp{
				Text:   text,
				SentAt: e.nowTime(),
			})
		}
	}
	if len(e.agent.liveQueuedFollowUps) < maxLiveAgentFollowUps {
		e.agent.liveQueuedFollowUps = append(e.agent.liveQueuedFollowUps, text)
	}
	e.agent.turnChangeSeq++
	e.notify()
	if e.OnAddToAgentRequest != nil {
		e.OnAddToAgentRequest(text)
	}
	return nil
}

// DrainAgentFollowUps returns and clears any instructions added via "Add to this request" since the last drain.
func (e *Editor) DrainAgentFollowUps() []string {
	if e == nil || len(e.agent.liveQueuedFollowUps) == 0 {
		return nil
	}
	out := slices.Clone(e.agent.liveQueuedFollowUps)
	e.agent.liveQueuedFollowUps = nil
	return out
}

// CompleteAgentRequest finishes the active live request and prepares the summary/review card,
// explicitly noting anything that was skipped because the user denied it.
func (e *Editor) CompleteAgentRequest(summary string) {
	if e == nil {
		return
	}
	if e.AgentStopped() {
		return
	}
	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	e.agent.liveRequestActive = false
	e.agent.liveActivity = ""
	if turn := e.latestAgentTurnPtr(); turn != nil && !turn.Stopped {
		dropTurnPlaceholder(turn)
		for i := range turn.Steps {
			if turn.Steps[i].Status == AgentStepRunning {
				turn.Steps[i].Status = AgentStepDone
			}
		}
		turn.Done = true
		turn.Status = "done"
		turn.FinishedAt = e.nowTime()
		if summary != "" {
			turn.Summary = summary
			turn.Reply = summary
		}
		e.observeTurnSceneMutations(turn, max(1, len(turn.Steps)))
		e.refreshTurnReview(turn)
		if turn.Summary != "" && turn.Mismatch == nil {
			turn.Mismatch = CheckAgentSummaryAgainstDiff(turn.Summary, turn.Review, turn.mutations)
		}
		turn.PendingReview = e.buildTurnPendingReviewLocked(turn, summary, false)
		e.finalizeTurnCheckpoint(turn.ID)
	}
	e.agent.turnChangeSeq++
	e.restoreIdleAgentStatus()
	e.notify()
}

// StopAgent stops the running or waiting agent request (triggered by the Stop button or Mod+. from anywhere,
// including inside modal dialogs). Completed steps and edits stay pending for review, and the
// "Agent stopped" toast offers Review. Esc never calls StopAgent.
func (e *Editor) StopAgent() bool {
	if e == nil {
		return false
	}
	wasActive := e.AgentWorking()
	if !wasActive {
		if e.OnStopAgent != nil {
			e.OnStopAgent()
		}
		return false
	}

	stoppedBefore := ""
	if e.agent.livePendingAsk != nil && !e.agent.livePendingAsk.resolved {
		if e.agent.livePendingAsk.ask.ShortAction != "" {
			stoppedBefore = e.agent.livePendingAsk.ask.ShortAction
		} else {
			stoppedBefore = e.agent.livePendingAsk.ask.Title
		}
	}

	stoppedTurnID := e.agent.activeTurnID
	e.agent.activeTurnID = ""
	waitingTaskID := e.clearWaitingTurnTaskID()
	if e.agent.activeRetryCancel != nil {
		e.agent.activeRetryCancel()
		e.agent.activeRetryCancel = nil
	}
	e.agent.activeRetryNow = nil
	hadActiveRun := e.agent.activeRunCancel != nil
	if e.agent.activeRunCancel != nil {
		e.agent.activeRunID++
		cancel := e.agent.activeRunCancel
		e.agent.activeRunCancel = nil
		cancel()
	}
	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	e.agent.openRequest = nil
	e.closeLiveStopChLocked()
	e.agent.liveRequestActive = false
	e.agent.liveStopped = true
	e.agent.liveActivity = ""

	if turn := e.latestAgentTurnPtr(); turn != nil && !turn.Done {
		if stoppedTurnID == "" {
			stoppedTurnID = turn.ID
		}
		dropTurnPlaceholder(turn)
		if hadActiveRun {
			for i := range turn.Steps {
				if turn.Steps[i].Status == AgentStepRunning || turn.Steps[i].Status == AgentStepWaiting {
					if stoppedBefore == "" {
						stoppedBefore = turn.Steps[i].Title
					}
					if turn.Steps[i].Status == AgentStepWaiting {
						turn.Steps[i].Status = AgentStepPending
					}
					break
				}
			}
			turn.Stopped = true
			turn.StoppedBefore = stoppedBefore
			turn.Done = true
			turn.Status = "stopped"
			turn.FinishedAt = e.nowTime()
			if turn.ModelError != nil {
				turn.ModelError.Retrying = false
			}
			e.populateStoppedTurnState(turn)
			doneCount := completedTurnStepCount(turn)
			stepLabel := "You stopped the agent"
			if stoppedBefore != "" {
				stepLabel = "You stopped before " + stoppedBefore
			}
			keptDetail := fmt.Sprintf("%d steps kept for review", doneCount)
			if doneCount == 1 {
				keptDetail = "1 step kept for review"
			}
			turn.PendingReview = e.buildTurnPendingReviewLocked(turn, fmt.Sprintf("%s · %s.", stepLabel, keptDetail), true)
		} else {
			for i := range turn.Steps {
				if turn.Steps[i].Status == AgentStepRunning || turn.Steps[i].Status == AgentStepWaiting {
					if stoppedBefore == "" {
						stoppedBefore = turn.Steps[i].Title
					}
					turn.Steps = append(turn.Steps[:i], turn.Steps[i+1:]...)
					break
				}
			}
			doneCount := completedTurnStepCount(turn)
			stepLabel := "You stopped the agent"
			if stoppedBefore != "" {
				stepLabel = "You stopped before " + stoppedBefore
			}
			keptDetail := fmt.Sprintf("%d steps kept for review", doneCount)
			if doneCount == 1 {
				keptDetail = "1 step kept for review"
			}
			e.agent.liveStepSeq++
			turn.Steps = append(turn.Steps, AgentStep{
				ID:        fmt.Sprintf("step-%d", e.agent.liveStepSeq),
				Title:     stepLabel,
				Detail:    keptDetail,
				Status:    AgentStepStopped,
				StartedAt: e.nowTime(),
			})
			turn.Stopped = true
			turn.StoppedBefore = stoppedBefore
			turn.Done = true
			turn.Status = "stopped"
			turn.FinishedAt = e.nowTime()
			if turn.ModelError != nil {
				turn.ModelError.Retrying = false
			}
			e.observeTurnSceneMutations(turn, max(1, len(turn.Steps)))
			e.refreshTurnReview(turn)
			turn.PendingReview = e.buildTurnPendingReviewLocked(turn, fmt.Sprintf("%s · %s.", stepLabel, keptDetail), true)
		}
	}
	if stoppedTurnID != "" {
		e.finalizeTurnCheckpoint(stoppedTurnID)
	}

	e.ShowToast(Toast{
		ID:          "agent-stopped",
		Kind:        ToastInfo,
		Message:     e.T("agent.stoppedToast"),
		ActionLabel: e.T("agent.review"),
		OnAction: func() {
			e.FocusAgentTurnReview()
		},
	})
	e.agent.turnChangeSeq++
	e.restoreIdleAgentStatus()
	if stoppedTurnID != "" {
		e.removeTurnNotificationTask(stoppedTurnID)
	}
	if waitingTaskID != "" && waitingTaskID != stoppedTurnID {
		e.removeTurnNotificationTask(waitingTaskID)
	}
	if stoppedTurnID == "" && waitingTaskID == "" {
		e.notify()
	}
	if e.OnStopAgent != nil {
		e.OnStopAgent()
	}
	return true
}

// FocusAgentTurnReview opens the Agent panel and focuses the pending review card.
func (e *Editor) FocusAgentTurnReview() {
	if e == nil {
		return
	}
	if e.agent.mode != AgentModeNone {
		e.agent.rightTab = RightTabAgent
	}
	e.agent.liveFocusReviewSeq++
	e.notify()
}

// KeepPendingAgentTurnReview keeps the latest turn's pending agent changes, resumes autosave, and shows a confirmation toast with Undo.
func (e *Editor) KeepPendingAgentTurnReview() {
	if e == nil || e.AgentActorActive() {
		return
	}
	turn := e.latestAgentTurnPtr()
	if turn == nil || turn.PendingReview == nil || turn.PendingReview.Kept || turn.PendingReview.Reverted {
		return
	}
	turn.PendingReview.Kept = true
	if turn.Review != nil {
		turn.Review.Kept = true
		turn.Review.Discarded = false
	}
	steps := turn.PendingReview.StepsDone
	msg := fmt.Sprintf("Kept %d steps", steps)
	if steps == 1 {
		msg = "Kept 1 step"
	}
	e.ShowToast(Toast{
		ID:          "agent-review-kept",
		Kind:        ToastSuccess,
		Message:     msg,
		ActionLabel: e.T("permissions.toastUndo"),
		OnAction: func() {
			e.RevertPendingAgentTurnReview()
			e.DismissToast()
		},
	})
	e.agent.turnChangeSeq++
	e.scheduleAutoSaveIfNeeded()
	e.notify()
	e.triggerReviewKeptAutomations(e.ScenePath, "")
}

// RevertPendingAgentTurnReview undoes scene edits made during the latest agent turn back to its starting history index.
func (e *Editor) RevertPendingAgentTurnReview() {
	turn := e.latestAgentTurnPtr()
	if turn == nil || turn.PendingReview == nil || turn.PendingReview.Reverted {
		return
	}
	if e.History != nil {
		target := turn.turnStartHistoryIdx
		for e.History.Index() > target && e.Undo() {
		}
	}
	turn.PendingReview.Kept = false
	turn.PendingReview.Reverted = true
	if turn.Review != nil {
		turn.Review.Kept = false
		turn.Review.Discarded = true
	}
	e.agent.turnChangeSeq++
	e.scheduleAutoSaveIfNeeded()
	e.notify()
}

// HasPendingAgentTurnReview reports whether the latest agent turn left unkept, unreverted scene edits pending review.
func (e *Editor) HasPendingAgentTurnReview() bool {
	turn := e.latestAgentTurnPtr()
	if turn == nil || turn.PendingReview == nil {
		return false
	}
	if turn.Review != nil && (turn.Review.Kept || turn.Review.Discarded) {
		return false
	}
	return !turn.PendingReview.Kept && !turn.PendingReview.Reverted && turn.PendingReview.ChangesCount > 0
}

// IsPermissionDenied reports whether permissionKey was already denied in the current active request.
func (e *Editor) IsPermissionDenied(permissionKey string) bool {
	if e == nil || permissionKey == "" {
		return false
	}
	return e.agent.liveDeniedKeys[permissionKey]
}

// DeniedPermissionKeys returns a sorted copy of permission keys denied in the current request.
func (e *Editor) DeniedPermissionKeys() []string {
	if e == nil || len(e.agent.liveDeniedKeys) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(e.agent.liveDeniedKeys))
}

func (e *Editor) setLivePermissionCardFocused(focused bool) {
	if e == nil {
		return
	}
	e.agent.livePermCardFocused = focused
}

// PendingAgentPermissionCardFocused reports whether keyboard focus is currently inside the Agent panel's live permission card.
func (e *Editor) PendingAgentPermissionCardFocused() bool {
	if e == nil || e.agent.livePendingAsk == nil || e.agent.livePendingAsk.resolved {
		return false
	}
	return e.agent.livePermCardFocused
}

func (e *Editor) updateLiveAskStepRunningLocked(ask AgentPermissionPrompt, detail string) {
	if turn := e.latestAgentTurnPtr(); turn != nil {
		for i := range turn.Steps {
			if turn.Steps[i].ID == ask.StepID {
				turn.Steps[i].Status = AgentStepRunning
				turn.Steps[i].Detail = detail
				break
			}
		}
	}
	if ask.Evaluation.Action == PermissionActionRun && (ask.Evaluation.Target == PermissionRunPlaytests || ask.Tool == "game_playtest") {
		scene := "the scene"
		if e.Doc != nil && e.Doc.Name != "" {
			scene = e.Doc.Name
		}
		e.agent.liveActivity = fmt.Sprintf("Agent is playing %s headless · manual edits stay available", scene)
	} else {
		e.agent.liveActivity = "Agent is working · manual edits stay available"
	}
}

func (e *Editor) buildTurnPendingReviewLocked(turn *AgentTurn, summary string, stopped bool) *AgentPendingReview {
	doneCount := completedTurnStepCount(turn)
	changes := 0
	if e.History != nil {
		if diff := e.History.Index() - turn.turnStartHistoryIdx; diff > 0 {
			changes = diff
		}
	}
	unchecked := slices.Clone(turn.NotChecked)
	if summary == "" && len(unchecked) > 0 {
		summary = fmt.Sprintf("Done, but not checked: %s.", strings.Join(unchecked, ", "))
	}
	return &AgentPendingReview{
		StepsDone:     doneCount,
		ChangesCount:  changes,
		Summary:       summary,
		NotChecked:    unchecked,
		Stopped:       stopped,
		StoppedBefore: turn.StoppedBefore,
	}
}

func deniedStepSummary(ask AgentPermissionPrompt) (label, unchecked string) {
	if ask.Evaluation.Action == PermissionActionRun && (ask.Evaluation.Target == PermissionRunPlaytests || ask.Tool == "game_playtest") {
		return "You denied the playtest", "Not playtested"
	}
	if ask.Evaluation.Action == PermissionActionRun && (ask.Evaluation.Target == PermissionRunGameCheck || ask.Tool == "game_check") {
		return "You denied game check", "Not checked"
	}
	if ask.ShortAction != "" {
		return "You denied " + ask.ShortAction, "Skipped " + ask.ShortAction
	}
	return "You denied " + ask.Tool, "Skipped " + ask.Tool
}

func sanitizeStepObjects(objects []string) []string {
	if len(objects) == 0 {
		return nil
	}
	limit := min(len(objects), maxAgentStepObjects)
	out := make([]string, 0, limit)
	seen := make(map[string]bool, limit)
	for _, raw := range objects {
		id := strings.TrimSpace(raw)
		if id == "" || strings.IndexByte(id, 0) >= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= limit {
			break
		}
	}
	return out
}
