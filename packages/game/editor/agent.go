package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

const agentReconnectProbeTimeout = 2 * time.Second

// RightRailTab selects which panel is shown in the right sidebar.
type RightRailTab string

const (
	// RightTabInspector shows the property / scene / layer inspector (default).
	RightTabInspector RightRailTab = "inspector"
	// RightTabAgent shows the Agent panel with status chip, thread, and composer.
	RightTabAgent RightRailTab = "agent"
)

// AgentMode selects how the editor connects to an AI agent.
type AgentMode string

const (
	// AgentModeUnconfigured is the initial state before the user completes the Connect dialog.
	AgentModeUnconfigured AgentMode = ""
	// AgentModeLocal connects to an external MCP agent running on this computer (Claude Code, Codex, or any MCP client).
	AgentModeLocal AgentMode = "local"
	// AgentModeAPIKey selects the built-in model path using an API key (implemented in its own issue).
	AgentModeAPIKey AgentMode = "apikey"
	// AgentModeNone disables agent features and keeps the Agent panel closed.
	AgentModeNone AgentMode = "none"
)

// AgentStatus is the live connection and work state shown by the Agent panel header's status chip.
type AgentStatus string

const (
	// AgentStatusNotConnected indicates no agent has connected yet.
	AgentStatusNotConnected AgentStatus = "not_connected"
	// AgentStatusConnected indicates an MCP client is attached and idle.
	AgentStatusConnected AgentStatus = "connected"
	// AgentStatusWorking indicates the agent is actively executing a request.
	AgentStatusWorking AgentStatus = "working"
	// AgentStatusWaitingForYou indicates a permission ask or clarifying question is open.
	AgentStatusWaitingForYou AgentStatus = "waiting"
	// AgentStatusReconnecting indicates the MCP connection dropped and retries are underway.
	AgentStatusReconnecting AgentStatus = "reconnecting"
	// AgentStatusDisconnected indicates all reconnect retries failed.
	AgentStatusDisconnected AgentStatus = "disconnected"
)

// ConnectStep identifies a specific step inside the Connect an agent dialog so Troubleshoot reopens directly at the step that failed.
type ConnectStep string

const (
	// ConnectStepChoice is the initial connection-mode selection step.
	ConnectStepChoice ConnectStep = "choice"
	// ConnectStepInstall is the project MCP install step (`go tool game mcp install`).
	ConnectStepInstall ConnectStep = "install"
	// ConnectStepWaiting is the waiting-for-MCP-connection step.
	ConnectStepWaiting ConnectStep = "waiting"
	// ConnectStepAPIKey is the built-in API key configuration step.
	ConnectStepAPIKey ConnectStep = "apikey"
)

// ExternalAgentKind identifies a supported external agent CLI invoked non-interactively by the composer.
type ExternalAgentKind string

const (
	ExternalAgentAuto       ExternalAgentKind = ""
	ExternalAgentClaudeCode ExternalAgentKind = "claude"
	ExternalAgentCodex      ExternalAgentKind = "codex"
)

// AgentReconnectDelays is the exponential backoff schedule used when an MCP connection drops (1, 2, 4, 8, and 16 seconds).
var AgentReconnectDelays = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
}

// AgentInstallCommand is the one-time command shown in the Connect dialog and copied by its Copy button.
const AgentInstallCommand = "go tool game mcp install"

// AgentClientInfo identifies one connected MCP client session attached to the editor's MCP server.
type AgentClientInfo struct {
	// ID uniquely identifies the session on the server.
	ID string
	// Name is the client's raw MCP implementation name (for example "claude-code" or "codex").
	Name string
	// DisplayName is the friendly display name shown in tooltips and Settings (for example "Claude Code").
	DisplayName string
	// Transport names how the client reaches the server (for example "stdio" or "unix").
	Transport string
}

// HoverLabel formats the status chip hover text, such as "Claude Code over stdio".
func (c AgentClientInfo) HoverLabel() string {
	name := strings.TrimSpace(c.DisplayName)
	if name == "" {
		name = FriendlyAgentName(c.Name)
	}
	if name == "" {
		name = "MCP client"
	}
	transport := strings.TrimSpace(c.Transport)
	if transport == "" {
		transport = "stdio"
	}
	return fmt.Sprintf("%s over %s", name, transport)
}

// FriendlyAgentName turns a raw MCP ClientInfo.Name into a human-friendly agent label.
func FriendlyAgentName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "MCP client"
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, "claude"):
		return "Claude Code"
	case strings.Contains(lower, "codex"):
		return "Codex"
	default:
		return trimmed
	}
}

// DetectExternalAgentKind infers which CLI to invoke from a connected MCP client name.
func DetectExternalAgentKind(clientName string) ExternalAgentKind {
	lower := strings.ToLower(strings.TrimSpace(clientName))
	switch {
	case strings.Contains(lower, "codex"):
		return ExternalAgentCodex
	case strings.Contains(lower, "claude"):
		return ExternalAgentClaudeCode
	default:
		return ExternalAgentAuto
	}
}

// AgentPermissionDecision is the starting decision for an agent permission rule.
type AgentPermissionDecision string

const (
	AgentPermissionAllow AgentPermissionDecision = "allow"
	AgentPermissionAsk   AgentPermissionDecision = "ask"
	AgentPermissionDeny  AgentPermissionDecision = "deny"
)

// AgentPermissionRule is one starting rule produced from the Connect dialog's four checkboxes.
type AgentPermissionRule struct {
	Action   string
	Files    string
	Decision AgentPermissionDecision
}

// AgentPermissions holds the four starting permission toggles in the Connect dialog.
type AgentPermissions struct {
	EditScenesAndData    bool
	RunGameCheck         bool
	RunHeadlessPlaytests bool
	EditGoCode           bool
}

// DefaultAgentPermissions matches Connect.dc.html: scenes/data and game check ticked, playtests and Go code unticked.
func DefaultAgentPermissions() AgentPermissions {
	return AgentPermissions{
		EditScenesAndData:    true,
		RunGameCheck:         true,
		RunHeadlessPlaytests: false,
		EditGoCode:           false,
	}
}

// Rules translates the four checkboxes into starting ordered permission rules (first match wins).
func (p AgentPermissions) Rules() []AgentPermissionRule {
	decide := func(allow bool) AgentPermissionDecision {
		if allow {
			return AgentPermissionAllow
		}
		return AgentPermissionAsk
	}
	return []AgentPermissionRule{
		{Action: "Edit", Files: "scenes/** prefabs/** data/**", Decision: decide(p.EditScenesAndData)},
		{Action: "Run", Files: "game check", Decision: decide(p.RunGameCheck)},
		{Action: "Run", Files: "playtests", Decision: decide(p.RunHeadlessPlaytests)},
		{Action: "Edit", Files: "**/*.go", Decision: decide(p.EditGoCode)},
		{Action: "Delete", Files: "any file", Decision: AgentPermissionDeny},
		{Action: "Run", Files: "shell commands", Decision: AgentPermissionDeny},
	}
}

// AgentStepStatus is the lifecycle state of one step streamed in an AgentTurn.
type AgentStepStatus string

const (
	AgentStepPending AgentStepStatus = "pending"
	AgentStepRunning AgentStepStatus = "running"
	AgentStepDone    AgentStepStatus = "done"
	AgentStepWaiting AgentStepStatus = "waiting"
	AgentStepFailed  AgentStepStatus = "failed"
)

// AgentStep is a single visible step in an agent turn's progress list.
type AgentStep struct {
	ID       string
	Title    string
	Detail   string
	ToolName string
	Status   AgentStepStatus
}

const (
	// maxRetainedTurnAttachments limits how many recent turns retain full ViewportPNG and SceneYAML payloads
	// in memory so long editor sessions do not grow unboundedly.
	maxRetainedTurnAttachments = 8
	// maxAgentTurnReplyBytes caps reply text stored on an agent turn (e.g. via editormcp agent_turn_reply).
	maxAgentTurnReplyBytes = 256 << 10
)

// AgentTurnContext captures the editor context attached to an agent request:
// the selection (object IDs and names), the open scene (path, name, and YAML), and a rendered viewport PNG.
type AgentTurnContext struct {
	ScenePath      string   `json:"scenePath,omitempty"`
	SceneName      string   `json:"sceneName,omitempty"`
	SceneYAML      string   `json:"sceneYaml,omitempty"`
	SelectionIDs   []string `json:"selectionIds,omitempty"`
	SelectionNames []string `json:"selectionNames,omitempty"`
	Layer          string   `json:"layer,omitempty"`
	ViewportPNG    []byte   `json:"-"`
	ViewportWidth  int      `json:"viewportWidth,omitempty"`
	ViewportHeight int      `json:"viewportHeight,omitempty"`
	ViewMode       string   `json:"viewMode,omitempty"`
}

// AgentTurn is one user request (in-place ⌘L, Agent panel composer, or terminal-initiated MCP turn)
// and its attached context, streamed steps, and reply.
type AgentTurn struct {
	ID           string           `json:"id"`
	Prompt       string           `json:"prompt"`
	Source       string           `json:"source,omitempty"`   // "in_place", "panel", or "external"
	SkipPlan     bool             `json:"skipPlan,omitempty"` // true for small in-place ⌘L edits per Plan.dc.html
	Status       string           `json:"status,omitempty"`   // "pending", "running", "done", "stopped"
	External     bool             `json:"external,omitempty"` // true for turns started by a terminal MCP client rather than the composer
	AgentName    string           `json:"agentName,omitempty"`
	Steps        []AgentStep      `json:"steps,omitempty"`
	Summary      string           `json:"summary,omitempty"`
	Reply        string           `json:"reply,omitempty"`
	Error        string           `json:"error,omitempty"`
	Stopped      bool             `json:"stopped,omitempty"`
	Done         bool             `json:"done,omitempty"`
	InputTokens  int              `json:"inputTokens,omitempty"`
	OutputTokens int              `json:"outputTokens,omitempty"`
	CreatedAt    time.Time        `json:"createdAt"`
	StartedAt    time.Time        `json:"startedAt"`
	FinishedAt   time.Time        `json:"finishedAt"`
	Context      AgentTurnContext `json:"context"`
}

// AgentOpenRequest represents a pending permission prompt or question waiting on the user.
type AgentOpenRequest struct {
	ID       string
	Title    string
	Body     string
	OpenedAt time.Time
}

// AgentRequest is passed to an AgentRunner when the user submits text from the composer.
type AgentRequest struct {
	ThreadID    string
	SessionID   string
	Prompt      string
	ProjectRoot string
	ScenePath   string
	SelectedID  string
	AgentKind   ExternalAgentKind
	Permissions AgentPermissions
}

// AgentEventKind classifies one streamed event from an AgentRunner.
type AgentEventKind string

const (
	AgentEventSession AgentEventKind = "session"
	AgentEventStep    AgentEventKind = "step"
	AgentEventMessage AgentEventKind = "message"
	AgentEventUsage   AgentEventKind = "usage"
	AgentEventDone    AgentEventKind = "done"
	AgentEventError   AgentEventKind = "error"
)

// AgentEvent is one incremental update streamed by an AgentRunner.
type AgentEvent struct {
	Kind         AgentEventKind
	SessionID    string
	StepID       string
	StepTitle    string
	StepDetail   string
	StepStatus   AgentStepStatus
	StepIndex    int
	StepTotal    int
	ToolName     string
	Text         string
	InputTokens  int
	OutputTokens int
	Err          string
}

// AgentRunner executes a composer request against an external agent (such as Claude Code or Codex) and streams incremental events to emit until the turn completes or ctx is canceled.
type AgentRunner interface {
	Run(ctx context.Context, req AgentRequest, emit func(AgentEvent)) error
}

// AgentRunnerFunc adapts a function to AgentRunner.
type AgentRunnerFunc func(ctx context.Context, req AgentRequest, emit func(AgentEvent)) error

// Run calls f.
func (f AgentRunnerFunc) Run(ctx context.Context, req AgentRequest, emit func(AgentEvent)) error {
	return f(ctx, req, emit)
}

type agentState struct {
	scopeMu  sync.RWMutex
	scope    *core.Scope
	inlineMu sync.Mutex

	rightTab RightRailTab
	mode     AgentMode
	status   AgentStatus

	// Connected MCP sessions and last known primary client for tooltips and reconnect diagnostics.
	clients     []AgentClientInfo
	lastClient  AgentClientInfo
	hadClient   bool
	failedStep  ConnectStep
	failedCause string

	// Reconnect retry state.
	reconnectAttempt int
	reconnectTotal   int
	reconnectCancel  func()
	probeCancel      context.CancelFunc
	reconnectGen     uint64

	// Connect dialog state.
	connectOpen       bool
	connectChoice     AgentMode
	connectStep       ConnectStep
	connectTroubled   bool
	connectCopied     bool
	connectCopiedStop func()
	connectAPIKey     string
	permissions       AgentPermissions
	connectPermsDraft AgentPermissions

	// Active thread and composer state.
	threadID           string
	sessionID          string
	turns              []AgentTurn
	composerText       string
	workingStep        int
	workingTotal       int
	activeRunCancel    context.CancelFunc
	activeRunID        uint64
	openRequest        *AgentOpenRequest
	focusRequestSeq    uint64
	focusRequestCalled int
	liveAnnouncement   string
	turnSeq            int
	turnChangeSeq      uint64
}

func (a *agentState) setScope(cx *core.Scope) {
	a.scopeMu.Lock()
	a.scope = cx
	a.scopeMu.Unlock()
}

func (a *agentState) getScope() *core.Scope {
	a.scopeMu.RLock()
	cx := a.scope
	a.scopeMu.RUnlock()
	return cx
}

func (a *agentState) initDefaults() {
	perms := DefaultAgentPermissions()
	a.rightTab = RightTabInspector
	a.mode = AgentModeUnconfigured
	a.status = AgentStatusNotConnected
	a.failedStep = ConnectStepChoice
	a.connectChoice = AgentModeLocal
	a.connectStep = ConnectStepChoice
	a.permissions = perms
	a.connectPermsDraft = perms
	a.threadID = "thread-1"
	a.reconnectTotal = len(AgentReconnectDelays)
}

func (e *Editor) nowTime() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// HasScope reports whether a UI core.Scope is currently attached to the editor.
func (e *Editor) HasScope() bool {
	if e == nil {
		return false
	}
	cx := e.agent.getScope()
	return cx != nil && !cx.Disposed()
}

// Post schedules fn onto the editor's UI scope when attached, or invokes fn inline otherwise.
func (e *Editor) Post(fn func()) {
	e.postOrRun(fn)
}

// CancelAgentReconnect stops any pending reconnect backoff timer without changing the current status.
func (e *Editor) CancelAgentReconnect() {
	if e == nil {
		return
	}
	e.cancelAgentReconnect()
}

func (e *Editor) postOrRun(fn func()) {
	if fn == nil {
		return
	}
	if e != nil {
		if cx := e.agent.getScope(); cx != nil && !cx.Disposed() {
			cx.Post(fn)
			return
		}
		e.agent.inlineMu.Lock()
		defer e.agent.inlineMu.Unlock()
	}
	fn()
}

func (e *Editor) scheduleAgentAfter(d time.Duration, fn func()) func() {
	if e != nil && e.AgentScheduleAfter != nil {
		return e.AgentScheduleAfter(d, fn)
	}
	timer := time.AfterFunc(d, func() {
		e.postOrRun(fn)
	})
	return func() {
		timer.Stop()
	}
}

func (e *Editor) cancelAgentReconnect() {
	if e.agent.reconnectCancel != nil {
		e.agent.reconnectCancel()
		e.agent.reconnectCancel = nil
	}
	if e.agent.probeCancel != nil {
		e.agent.probeCancel()
		e.agent.probeCancel = nil
	}
	e.agent.reconnectGen++
}

func (e *Editor) stopAgentTimers() {
	e.cancelAgentReconnect()
	if e.agent.connectCopiedStop != nil {
		e.agent.connectCopiedStop()
		e.agent.connectCopiedStop = nil
	}
	if e.agent.activeRunCancel != nil {
		e.agent.activeRunCancel()
		e.agent.activeRunCancel = nil
	}
	e.agent.connectAPIKey = ""
}

// resetAgentForProject stops active agent runs/timers and clears per-project agent conversation,
// client sessions, open requests, composer draft, and draft API key when switching projects or demos.
func (e *Editor) resetAgentForProject() {
	if e == nil {
		return
	}
	e.stopAgentTimers()
	e.CloseAskInPlace()
	e.agent.activeRunID++
	e.agent.clients = nil
	e.agent.lastClient = AgentClientInfo{}
	e.agent.hadClient = false
	e.agent.failedStep = ConnectStepChoice
	e.agent.failedCause = ""
	e.agent.reconnectAttempt = 0
	e.agent.connectOpen = false
	e.agent.connectTroubled = false
	e.agent.connectCopied = false
	e.agent.connectAPIKey = ""
	e.agent.sessionID = ""
	e.agent.turns = nil
	e.agent.composerText = ""
	e.agent.workingStep = 0
	e.agent.workingTotal = 0
	e.agent.openRequest = nil
	e.agent.status = AgentStatusNotConnected
	e.updateAgentAnnouncement()
}

// RightRailTab returns the active tab in the right sidebar (RightTabInspector or RightTabAgent).
func (e *Editor) RightRailTab() RightRailTab {
	if e == nil || e.agent.rightTab == "" {
		return RightTabInspector
	}
	if e.agent.mode == AgentModeNone {
		return RightTabInspector
	}
	return e.agent.rightTab
}

// SetRightRailTab switches the right sidebar between Inspector and Agent.
// When AgentModeNone is active, the Agent panel stays closed.
func (e *Editor) SetRightRailTab(tab RightRailTab) {
	if e == nil {
		return
	}
	if tab != RightTabAgent {
		tab = RightTabInspector
	}
	if tab == RightTabAgent && e.agent.mode == AgentModeNone {
		tab = RightTabInspector
	}
	if e.agent.rightTab == tab {
		return
	}
	e.agent.rightTab = tab
	e.notify()
}

// AgentPanelOpen reports whether the Agent tab is currently active in the right sidebar.
func (e *Editor) AgentPanelOpen() bool {
	return e.RightRailTab() == RightTabAgent
}

// SetAgentPanelOpen opens or closes the Agent panel tab.
func (e *Editor) SetAgentPanelOpen(open bool) {
	if open {
		e.SetRightRailTab(RightTabAgent)
	} else {
		e.SetRightRailTab(RightTabInspector)
	}
}

// AgentMode returns the configured agent connection mode.
func (e *Editor) AgentMode() AgentMode {
	if e == nil {
		return AgentModeUnconfigured
	}
	return e.agent.mode
}

// AgentStatus returns the current status chip state.
func (e *Editor) AgentStatus() AgentStatus {
	if e == nil || e.agent.status == "" {
		return AgentStatusNotConnected
	}
	return e.agent.status
}

// AgentClients returns a copy of currently connected MCP clients.
func (e *Editor) AgentClients() []AgentClientInfo {
	if e == nil {
		return nil
	}
	return slices.Clone(e.agent.clients)
}

// AgentPrimaryClient returns the primary connected MCP client (or the last known client when reconnecting/disconnected) and whether one is known.
func (e *Editor) AgentPrimaryClient() (AgentClientInfo, bool) {
	if e == nil {
		return AgentClientInfo{}, false
	}
	if len(e.agent.clients) > 0 {
		return e.agent.clients[0], true
	}
	if e.agent.hadClient {
		return e.agent.lastClient, true
	}
	return AgentClientInfo{}, false
}

// AgentStatusTooltip returns the hover text for the Connected status chip, naming the agent and transport (for example "Claude Code over stdio").
func (e *Editor) AgentStatusTooltip() string {
	if c, ok := e.AgentPrimaryClient(); ok {
		return c.HoverLabel()
	}
	return "MCP client over stdio"
}

// AgentWorkingProgress returns the current 1-based step index and total step count (0 if unknown) while working.
func (e *Editor) AgentWorkingProgress() (step, total int) {
	if e == nil {
		return 0, 0
	}
	return e.agent.workingStep, e.agent.workingTotal
}

// AgentReconnectProgress returns the current 1-based retry attempt and total attempts (5) while reconnecting.
func (e *Editor) AgentReconnectProgress() (attempt, total int) {
	if e == nil {
		return 0, len(AgentReconnectDelays)
	}
	total = e.agent.reconnectTotal
	if total <= 0 {
		total = len(AgentReconnectDelays)
	}
	return e.agent.reconnectAttempt, total
}

// AgentFailedStep returns the Connect dialog step that failed when disconnected.
func (e *Editor) AgentFailedStep() ConnectStep {
	if e == nil || e.agent.failedStep == "" {
		return ConnectStepWaiting
	}
	return e.agent.failedStep
}

// AgentFailedCause returns a short explanation of why the connection failed.
func (e *Editor) AgentFailedCause() string {
	if e == nil {
		return ""
	}
	return e.agent.failedCause
}

// AgentPermissions returns the active starting permission checkboxes.
func (e *Editor) AgentPermissions() AgentPermissions {
	if e == nil {
		return DefaultAgentPermissions()
	}
	return e.agent.permissions
}

// AgentPermissionRules returns the ordered starting permission rules derived from AgentPermissions.
func (e *Editor) AgentPermissionRules() []AgentPermissionRule {
	return e.AgentPermissions().Rules()
}

// AgentLiveAnnouncement returns the latest aria-live="polite" status announcement text.
func (e *Editor) AgentLiveAnnouncement() string {
	if e == nil {
		return ""
	}
	return e.agent.liveAnnouncement
}

func (e *Editor) updateAgentAnnouncement() {
	switch e.agent.status {
	case AgentStatusConnected:
		e.agent.liveAnnouncement = fmt.Sprintf("Connected via MCP: %s", e.AgentStatusTooltip())
	case AgentStatusWorking:
		if e.agent.workingTotal > 0 {
			e.agent.liveAnnouncement = fmt.Sprintf("Agent working, step %d of %d", max(1, e.agent.workingStep), e.agent.workingTotal)
		} else {
			e.agent.liveAnnouncement = fmt.Sprintf("Agent working, step %d", max(1, e.agent.workingStep))
		}
	case AgentStatusWaitingForYou:
		title := "request open"
		if e.agent.openRequest != nil && e.agent.openRequest.Title != "" {
			title = e.agent.openRequest.Title
		}
		e.agent.liveAnnouncement = fmt.Sprintf("Agent waiting for you: %s", title)
	case AgentStatusReconnecting:
		attempt, total := e.AgentReconnectProgress()
		e.agent.liveAnnouncement = fmt.Sprintf("Agent reconnecting, try %d of %d", attempt, total)
	case AgentStatusDisconnected:
		e.agent.liveAnnouncement = "Agent disconnected"
	default:
		e.agent.liveAnnouncement = "No agent connected"
	}
}

// ConnectDialogOpen reports whether the Connect an agent dialog is open.
func (e *Editor) ConnectDialogOpen() bool {
	return e != nil && e.agent.connectOpen
}

// ConnectDialogChoice returns the radio choice currently highlighted inside the Connect dialog.
func (e *Editor) ConnectDialogChoice() AgentMode {
	if e == nil || e.agent.connectChoice == "" {
		return AgentModeLocal
	}
	return e.agent.connectChoice
}

// ConnectDialogStep returns the step the Connect dialog is focused on.
func (e *Editor) ConnectDialogStep() ConnectStep {
	if e == nil || e.agent.connectStep == "" {
		return ConnectStepChoice
	}
	return e.agent.connectStep
}

// ConnectDialogTroubleshooting reports whether the Connect dialog was reopened from Troubleshoot at a failed step.
func (e *Editor) ConnectDialogTroubleshooting() bool {
	return e != nil && e.agent.connectTroubled
}

// ConnectDialogPermissions returns the draft checkbox state inside the Connect dialog.
func (e *Editor) ConnectDialogPermissions() AgentPermissions {
	if e == nil {
		return DefaultAgentPermissions()
	}
	return e.agent.connectPermsDraft
}

// ConnectDialogCopied reports whether the install command was recently copied.
func (e *Editor) ConnectDialogCopied() bool {
	return e != nil && e.agent.connectCopied
}

// ConnectDialogAPIKey returns the draft API key entered in the Connect dialog.
func (e *Editor) ConnectDialogAPIKey() string {
	if e == nil {
		return ""
	}
	return e.agent.connectAPIKey
}

// OpenConnectDialog opens the Connect an agent dialog at the starting step.
func (e *Editor) OpenConnectDialog() {
	if e == nil {
		return
	}
	e.CloseAskInPlace()
	choice := e.agent.mode
	if choice == AgentModeUnconfigured {
		choice = AgentModeLocal
	}
	e.agent.connectOpen = true
	e.agent.connectChoice = choice
	e.agent.connectStep = ConnectStepChoice
	e.agent.connectTroubled = false
	e.agent.connectCopied = false
	e.agent.connectPermsDraft = e.agent.permissions
	e.notify()
}

// OpenConnectDialogAt opens the Connect an agent dialog focused directly on step (used by Troubleshoot so it lands on the step that failed, not the start).
func (e *Editor) OpenConnectDialogAt(step ConnectStep) {
	if e == nil {
		return
	}
	e.CloseAskInPlace()
	if step == "" {
		step = ConnectStepWaiting
	}
	e.agent.connectOpen = true
	e.agent.connectStep = step
	e.agent.connectTroubled = step != ConnectStepChoice
	e.agent.connectCopied = false
	e.agent.connectPermsDraft = e.agent.permissions
	switch step {
	case ConnectStepAPIKey:
		e.agent.connectChoice = AgentModeAPIKey
	case ConnectStepInstall, ConnectStepWaiting:
		e.agent.connectChoice = AgentModeLocal
	default:
		if e.agent.mode != AgentModeUnconfigured {
			e.agent.connectChoice = e.agent.mode
		} else {
			e.agent.connectChoice = AgentModeLocal
		}
	}
	e.notify()
}

// TroubleshootAgentConnection reopens the Connect dialog at the step that failed rather than at the start.
func (e *Editor) TroubleshootAgentConnection() {
	if e == nil {
		return
	}
	step := e.AgentFailedStep()
	if step == ConnectStepChoice {
		step = ConnectStepWaiting
	}
	e.OpenConnectDialogAt(step)
}

// CloseConnectDialog closes the Connect dialog without committing draft changes ("Later").
func (e *Editor) CloseConnectDialog() {
	if e == nil || !e.agent.connectOpen {
		return
	}
	e.agent.connectOpen = false
	e.agent.connectTroubled = false
	e.agent.connectAPIKey = ""
	e.notify()
}

// SetConnectChoice selects one of the three choices inside the Connect dialog.
func (e *Editor) SetConnectChoice(mode AgentMode) {
	if e == nil {
		return
	}
	switch mode {
	case AgentModeLocal, AgentModeAPIKey, AgentModeNone:
	default:
		return
	}
	e.agent.connectChoice = mode
	e.agent.connectTroubled = false
	switch mode {
	case AgentModeLocal:
		e.agent.connectStep = ConnectStepChoice
	case AgentModeAPIKey:
		e.agent.connectStep = ConnectStepAPIKey
	case AgentModeNone:
		e.agent.connectStep = ConnectStepChoice
	}
	e.notify()
}

// SetConnectPermissions updates the draft permission checkboxes inside the Connect dialog.
func (e *Editor) SetConnectPermissions(perms AgentPermissions) {
	if e == nil {
		return
	}
	e.agent.connectPermsDraft = perms
	e.notify()
}

// SetConnectAPIKey updates the draft API key in the Connect dialog.
func (e *Editor) SetConnectAPIKey(key string) {
	if e == nil {
		return
	}
	e.agent.connectAPIKey = key
	e.notify()
}

// CopyConnectInstallCommand copies `go tool game mcp install` to the clipboard.
func (e *Editor) CopyConnectInstallCommand() {
	if e == nil {
		return
	}
	e.clipboardText = AgentInstallCommand
	if e.WriteClipboard != nil {
		e.WriteClipboard(AgentInstallCommand)
	} else if e.scope != nil {
		e.scope.SetClipboard(AgentInstallCommand)
	}
	e.agent.connectCopied = true
	if e.agent.connectCopiedStop != nil {
		e.agent.connectCopiedStop()
	}
	e.agent.connectCopiedStop = e.scheduleAgentAfter(2*time.Second, func() {
		e.agent.connectCopied = false
		e.agent.connectCopiedStop = nil
		e.notify()
	})
	e.Status = "Copied " + AgentInstallCommand
	e.EmitToastEvent(ToastEvent{
		Kind:    ToastEventCopied,
		Target:  "agent-install",
		Message: e.Status,
	})
	e.notify()
}

// CompleteConnectDialog commits the Connect dialog choices and starting permission rules ("Done").
// Choosing AgentModeNone keeps the Agent panel closed; otherwise the Agent panel opens.
func (e *Editor) CompleteConnectDialog() {
	if e == nil {
		return
	}
	choice := e.ConnectDialogChoice()
	e.agent.mode = choice
	e.agent.permissions = e.agent.connectPermsDraft
	e.agent.connectOpen = false
	e.agent.connectTroubled = false
	e.agent.connectAPIKey = ""

	switch choice {
	case AgentModeNone:
		e.cancelAgentReconnect()
		e.agent.rightTab = RightTabInspector
		e.agent.status = AgentStatusNotConnected
		e.updateAgentAnnouncement()
	case AgentModeLocal:
		e.agent.rightTab = RightTabAgent
		if len(e.agent.clients) > 0 && e.agent.status == AgentStatusNotConnected {
			e.agent.status = AgentStatusConnected
			e.updateAgentAnnouncement()
		}
	case AgentModeAPIKey:
		e.agent.rightTab = RightTabAgent
	}
	e.notify()
}

// NotifyAgentMCPConnected records that an MCP client session attached to the editor's MCP server.
// Transitions waiting/reconnecting/disconnected states immediately to AgentStatusConnected.
func (e *Editor) NotifyAgentMCPConnected(client AgentClientInfo) {
	if e == nil {
		return
	}
	if client.ID == "" {
		client.ID = fmt.Sprintf("client-%d", len(e.agent.clients)+1)
	}
	if client.DisplayName == "" {
		client.DisplayName = FriendlyAgentName(client.Name)
	}
	if client.Transport == "" {
		client.Transport = "stdio"
	}

	replaced := false
	for i := range e.agent.clients {
		if e.agent.clients[i].ID == client.ID {
			e.agent.clients[i] = client
			replaced = true
			break
		}
	}
	if !replaced {
		e.agent.clients = append(e.agent.clients, client)
	}
	e.agent.lastClient = client
	e.agent.hadClient = true
	e.agent.failedCause = ""
	e.cancelAgentReconnect()
	e.agent.reconnectAttempt = 0

	if e.agent.mode == AgentModeUnconfigured {
		e.agent.mode = AgentModeLocal
	}
	if e.agent.status != AgentStatusWorking && e.agent.status != AgentStatusWaitingForYou {
		e.agent.status = AgentStatusConnected
	}
	e.updateAgentAnnouncement()
	e.notify()
}

// NotifyAgentMCPDisconnected records that an MCP client session closed or dropped.
// When the last connected client drops, the editor enters AgentStatusReconnecting and runs the 1, 2, 4, 8, 16 s retry schedule before transitioning to AgentStatusDisconnected.
func (e *Editor) NotifyAgentMCPDisconnected(clientID, reason string) {
	if e == nil {
		return
	}
	filtered := e.agent.clients[:0]
	for _, c := range e.agent.clients {
		if c.ID != clientID {
			filtered = append(filtered, c)
		}
	}
	e.agent.clients = filtered
	if len(e.agent.clients) > 0 {
		e.updateAgentAnnouncement()
		e.notify()
		return
	}
	if e.agent.mode == AgentModeNone {
		e.agent.status = AgentStatusNotConnected
		e.updateAgentAnnouncement()
		e.notify()
		return
	}

	// Determine which Connect step failed so Troubleshoot opens right there.
	e.agent.failedStep = e.diagnoseFailedConnectStep()
	if strings.TrimSpace(reason) != "" {
		e.agent.failedCause = reason
	} else {
		e.agent.failedCause = fmt.Sprintf("%s stopped answering over MCP.", e.AgentStatusTooltip())
	}
	e.startAgentReconnect()
}

func (e *Editor) diagnoseFailedConnectStep() ConnectStep {
	if e.agent.mode == AgentModeAPIKey {
		return ConnectStepAPIKey
	}
	if root := e.agentProjectRoot(); root != "" {
		_, errClaude := os.Stat(filepath.Join(root, ".mcp.json"))
		_, errCodex := os.Stat(filepath.Join(root, ".codex", "config.toml"))
		if errClaude != nil && errCodex != nil {
			return ConnectStepInstall
		}
	}
	return ConnectStepWaiting
}

func (e *Editor) agentProjectRoot() string {
	if e == nil {
		return ""
	}
	if e.ProjectPath != "" {
		return filepath.Dir(e.ProjectPath)
	}
	if e.FilePath != "" {
		return filepath.Dir(e.FilePath)
	}
	return ""
}

// ReconnectAgent restarts the reconnect sequence from try 1 of 5 (used by the Disconnected chip's Reconnect button).
func (e *Editor) ReconnectAgent() {
	if e == nil {
		return
	}
	if len(e.agent.clients) > 0 {
		e.cancelAgentReconnect()
		e.agent.status = AgentStatusConnected
		e.updateAgentAnnouncement()
		e.notify()
		return
	}
	// Run an immediate bounded probe off the UI thread (or synchronously when a fake test scheduler
	// is installed) so a stalled MCP client can never freeze the UI goroutine.
	probe := e.AgentReconnectProbe
	if probe != nil {
		if e.AgentScheduleAfter != nil {
			ctx, cancel := context.WithTimeout(context.Background(), agentReconnectProbeTimeout)
			info, err := probe(ctx)
			cancel()
			if err == nil {
				e.NotifyAgentMCPConnected(info)
				return
			}
			e.startAgentReconnect()
			return
		}
		e.cancelAgentReconnect()
		e.agent.status = AgentStatusReconnecting
		e.agent.reconnectAttempt = 1
		e.agent.reconnectTotal = len(AgentReconnectDelays)
		gen := e.agent.reconnectGen
		ctx, cancel := context.WithTimeout(context.Background(), agentReconnectProbeTimeout)
		e.agent.probeCancel = cancel
		e.updateAgentAnnouncement()
		e.notify()
		go func() {
			info, err := probe(ctx)
			cancel()
			e.postOrRun(func() {
				if e.agent.reconnectGen != gen || e.agent.status != AgentStatusReconnecting {
					return
				}
				e.agent.probeCancel = nil
				if err == nil {
					e.NotifyAgentMCPConnected(info)
					return
				}
				e.scheduleReconnectAttempt(gen, 1)
			})
		}()
		return
	}
	e.startAgentReconnect()
}

func (e *Editor) startAgentReconnect() {
	e.cancelAgentReconnect()
	e.agent.status = AgentStatusReconnecting
	e.agent.reconnectAttempt = 1
	e.agent.reconnectTotal = len(AgentReconnectDelays)
	gen := e.agent.reconnectGen
	e.updateAgentAnnouncement()
	e.notify()

	e.scheduleReconnectAttempt(gen, 1)
}

func (e *Editor) advanceReconnectAfterFailure(gen uint64, attempt int) {
	if e.agent.reconnectGen != gen || e.agent.status != AgentStatusReconnecting {
		return
	}
	if attempt < len(AgentReconnectDelays) {
		e.agent.reconnectAttempt = attempt + 1
		e.updateAgentAnnouncement()
		e.notify()
		e.scheduleReconnectAttempt(gen, attempt+1)
		return
	}
	// All 5 retries failed -> Disconnected.
	e.agent.status = AgentStatusDisconnected
	e.agent.reconnectAttempt = len(AgentReconnectDelays)
	if e.agent.failedStep == "" || e.agent.failedStep == ConnectStepChoice {
		e.agent.failedStep = e.diagnoseFailedConnectStep()
	}
	e.updateAgentAnnouncement()
	e.notify()
}

func (e *Editor) scheduleReconnectAttempt(gen uint64, attempt int) {
	if attempt < 1 || attempt > len(AgentReconnectDelays) {
		return
	}
	delay := AgentReconnectDelays[attempt-1]
	e.agent.reconnectCancel = e.scheduleAgentAfter(delay, func() {
		if e.agent.reconnectGen != gen || e.agent.status != AgentStatusReconnecting {
			return
		}
		e.agent.reconnectCancel = nil
		if len(e.agent.clients) > 0 {
			e.agent.status = AgentStatusConnected
			e.agent.reconnectAttempt = 0
			e.updateAgentAnnouncement()
			e.notify()
			return
		}
		probe := e.AgentReconnectProbe
		if probe != nil {
			if e.AgentScheduleAfter != nil {
				ctx, cancel := context.WithTimeout(context.Background(), agentReconnectProbeTimeout)
				info, err := probe(ctx)
				cancel()
				if err == nil {
					e.NotifyAgentMCPConnected(info)
					return
				}
				e.advanceReconnectAfterFailure(gen, attempt)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), agentReconnectProbeTimeout)
			e.agent.probeCancel = cancel
			go func() {
				info, err := probe(ctx)
				cancel()
				e.postOrRun(func() {
					if e.agent.reconnectGen != gen || e.agent.status != AgentStatusReconnecting {
						return
					}
					e.agent.probeCancel = nil
					if err == nil {
						e.NotifyAgentMCPConnected(info)
						return
					}
					e.advanceReconnectAfterFailure(gen, attempt)
				})
			}()
			return
		}
		e.advanceReconnectAfterFailure(gen, attempt)
	})
}

// SetAgentWaitingForYou transitions the status chip to "Waiting for you · elapsed" with an open request card.
func (e *Editor) SetAgentWaitingForYou(req AgentOpenRequest) {
	if e == nil {
		return
	}
	if req.OpenedAt.IsZero() {
		req.OpenedAt = e.nowTime()
	}
	if req.ID == "" {
		req.ID = fmt.Sprintf("req-%d", e.agent.turnSeq+1)
	}
	cp := req
	e.agent.openRequest = &cp
	e.agent.status = AgentStatusWaitingForYou
	e.updateAgentAnnouncement()
	e.notify()
}

// ClearAgentWaitingForYou clears the open waiting request and restores Working or Connected status.
func (e *Editor) ClearAgentWaitingForYou() {
	if e == nil || e.agent.openRequest == nil {
		return
	}
	e.agent.openRequest = nil
	if e.agent.activeRunCancel != nil {
		e.agent.status = AgentStatusWorking
	} else if len(e.agent.clients) > 0 {
		e.agent.status = AgentStatusConnected
	} else {
		e.agent.status = AgentStatusNotConnected
	}
	e.updateAgentAnnouncement()
	e.notify()
}

// AgentOpenRequest returns the currently open permission/question request, or nil.
func (e *Editor) AgentOpenRequest() *AgentOpenRequest {
	if e == nil || e.agent.openRequest == nil {
		return nil
	}
	cp := *e.agent.openRequest
	return &cp
}

// AgentWaitingElapsed returns how long the current Waiting-for-you request has been open, formatted as "m:ss".
func (e *Editor) AgentWaitingElapsed() string {
	if e == nil || e.agent.openRequest == nil {
		return "0:00"
	}
	d := e.nowTime().Sub(e.agent.openRequest.OpenedAt)
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// FocusOpenAgentRequest opens the Agent panel and focuses the open waiting request.
func (e *Editor) FocusOpenAgentRequest() {
	if e == nil || e.agent.openRequest == nil {
		return
	}
	if e.agent.mode != AgentModeNone {
		e.agent.rightTab = RightTabAgent
	}
	e.agent.focusRequestSeq++
	e.agent.focusRequestCalled++
	if e.OnFocusAgentRequest != nil {
		e.OnFocusAgentRequest(*e.agent.openRequest)
	}
	e.notify()
}

// AgentFocusRequestCount returns how many times FocusOpenAgentRequest has been triggered (useful for tests).
func (e *Editor) AgentFocusRequestCount() int {
	if e == nil {
		return 0
	}
	return e.agent.focusRequestCalled
}

// AgentComposerText returns the current multi-line draft in the Agent composer.
func (e *Editor) AgentComposerText() string {
	if e == nil {
		return ""
	}
	return e.agent.composerText
}

// SetAgentComposerText updates the multi-line draft in the Agent composer.
func (e *Editor) SetAgentComposerText(text string) {
	if e == nil || e.agent.composerText == text {
		return
	}
	e.agent.composerText = text
	e.notify()
}

// InsertAgentComposerNewline appends a newline to the composer draft (used by Shift+Enter).
func (e *Editor) InsertAgentComposerNewline() {
	if e == nil {
		return
	}
	e.agent.composerText += "\n"
	e.notify()
}

func cloneAgentTurn(t AgentTurn) AgentTurn {
	t.Steps = slices.Clone(t.Steps)
	t.Context.SelectionIDs = slices.Clone(t.Context.SelectionIDs)
	t.Context.SelectionNames = slices.Clone(t.Context.SelectionNames)
	t.Context.ViewportPNG = slices.Clone(t.Context.ViewportPNG)
	if t.Status == "" {
		switch {
		case t.Stopped:
			t.Status = "stopped"
		case t.Done:
			t.Status = "done"
		default:
			t.Status = "pending"
		}
	}
	if t.Reply == "" && t.Summary != "" {
		t.Reply = t.Summary
	}
	return t
}

func (e *Editor) trimAgentTurnAttachments() {
	if e == nil || len(e.agent.turns) <= maxRetainedTurnAttachments {
		return
	}
	cutoff := len(e.agent.turns) - maxRetainedTurnAttachments
	for i := 0; i < cutoff; i++ {
		e.agent.turns[i].Context.ViewportPNG = nil
		e.agent.turns[i].Context.SceneYAML = ""
	}
}

// AgentTurns returns a deep-enough copy of the turns in the active Agent panel thread, oldest first.
func (e *Editor) AgentTurns() []AgentTurn {
	if e == nil || len(e.agent.turns) == 0 {
		return nil
	}
	out := make([]AgentTurn, len(e.agent.turns))
	for i := range e.agent.turns {
		out[i] = cloneAgentTurn(e.agent.turns[i])
	}
	return out
}

// LatestAgentTurn returns the most recent agent turn and true, or zero and false when none exists.
func (e *Editor) LatestAgentTurn() (AgentTurn, bool) {
	if e == nil || len(e.agent.turns) == 0 {
		return AgentTurn{}, false
	}
	return cloneAgentTurn(e.agent.turns[len(e.agent.turns)-1]), true
}

// AgentTurn returns the recorded turn matching id, or false when not found.
func (e *Editor) AgentTurn(id string) (AgentTurn, bool) {
	if e == nil || id == "" {
		return AgentTurn{}, false
	}
	for i := range e.agent.turns {
		if e.agent.turns[i].ID == id {
			return cloneAgentTurn(e.agent.turns[i]), true
		}
	}
	return AgentTurn{}, false
}

// AgentTurnSeq returns a monotonically increasing sequence number bumped whenever an agent turn is added or updated.
func (e *Editor) AgentTurnSeq() uint64 {
	if e == nil {
		return 0
	}
	return e.agent.turnChangeSeq
}

// ReplyAgentTurn records an agent reply and/or status ("pending", "running", "done", "stopped") on turnID
// (defaulting to the latest turn when turnID is empty) so the Agent panel's thread updates live.
func (e *Editor) ReplyAgentTurn(turnID, reply, status string) error {
	if e == nil || len(e.agent.turns) == 0 {
		return errors.New("editor: no agent turn")
	}
	if len(reply) > maxAgentTurnReplyBytes {
		return fmt.Errorf("editor: agent turn reply exceeds maximum size (%d bytes)", maxAgentTurnReplyBytes)
	}
	if strings.IndexByte(reply, 0) >= 0 {
		return errors.New("editor: agent turn reply must not contain NUL bytes")
	}
	idx := len(e.agent.turns) - 1
	if turnID != "" {
		idx = -1
		for i := range e.agent.turns {
			if e.agent.turns[i].ID == turnID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("editor: agent turn %q not found", turnID)
		}
	}
	turn := &e.agent.turns[idx]
	if status != "" {
		switch status {
		case "pending", "running", "done", "stopped":
			turn.Status = status
			turn.Done = status == "done" || status == "stopped"
			turn.Stopped = status == "stopped"
			if turn.Done && turn.FinishedAt.IsZero() {
				turn.FinishedAt = e.nowTime()
			}
		default:
			return fmt.Errorf("editor: invalid agent turn status %q", status)
		}
	}
	if reply != "" {
		turn.Reply = reply
		turn.Summary = reply
		if status == "" && (turn.Status == "" || turn.Status == "pending") {
			turn.Status = "done"
			turn.Done = true
			turn.FinishedAt = e.nowTime()
		}
	}
	e.agent.turnChangeSeq++
	e.notify()
	return nil
}

// AgentSessionID returns the external CLI session ID tracked for the current thread.
func (e *Editor) AgentSessionID() string {
	if e == nil {
		return ""
	}
	return e.agent.sessionID
}

// AgentWorking reports whether a composer request is currently running.
func (e *Editor) AgentWorking() bool {
	return e != nil && e.agent.activeRunCancel != nil
}

// RecordExternalAgentToolCall records a tool call executed by a terminal-started MCP session so its work appears as an agent turn in the panel.
func (e *Editor) RecordExternalAgentToolCall(clientName, toolName, summary string, isErr bool) {
	if e == nil || e.agent.mode == AgentModeNone {
		return
	}
	// If a composer request is actively running, the CLI stream owns that turn's steps.
	if e.agent.activeRunCancel != nil {
		return
	}
	title := summary
	if strings.TrimSpace(title) == "" {
		title = toolName
	}
	status := AgentStepDone
	if isErr {
		status = AgentStepFailed
	}
	now := e.nowTime()
	// Append to the most recent external turn if it's recent (within 60s), otherwise start a new external turn.
	if n := len(e.agent.turns); n > 0 && e.agent.turns[n-1].External && now.Sub(e.agent.turns[n-1].FinishedAt) <= 60*time.Second {
		t := &e.agent.turns[n-1]
		t.Steps = append(t.Steps, AgentStep{
			ID:       fmt.Sprintf("%s-step-%d", t.ID, len(t.Steps)+1),
			Title:    title,
			ToolName: toolName,
			Status:   status,
		})
		t.FinishedAt = now
		e.notify()
		return
	}
	e.agent.turnSeq++
	e.agent.turnChangeSeq++
	id := fmt.Sprintf("turn-%d", e.agent.turnSeq)
	name := FriendlyAgentName(clientName)
	e.agent.turns = append(e.agent.turns, AgentTurn{
		ID:        id,
		Source:    "external",
		Status:    "done",
		External:  true,
		AgentName: name,
		Steps: []AgentStep{{
			ID:       id + "-step-1",
			Title:    title,
			ToolName: toolName,
			Status:   status,
		}},
		Done:       true,
		CreatedAt:  now,
		StartedAt:  now,
		FinishedAt: now,
	})
	e.notify()
}

// SubmitAgentPrompt submits prompt (or the current composer text when prompt is empty) to the connected agent runner and streams its steps into the Agent panel.
func (e *Editor) SubmitAgentPrompt(prompt string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		return errors.New("editor: agent is disabled")
	}
	text := strings.TrimSpace(prompt)
	if text == "" {
		text = strings.TrimSpace(e.agent.composerText)
	}
	if text == "" {
		return nil
	}
	if e.agent.activeRunCancel != nil {
		return errors.New("editor: an agent request is already running")
	}
	runner := e.AgentRunner
	if runner == nil {
		if len(e.agent.clients) == 0 && e.agent.mode != AgentModeLocal {
			e.OpenConnectDialog()
			return errors.New("editor: no agent connected")
		}
		runner = defaultCLIAgentRunner()
	}

	e.agent.composerText = ""
	e.agent.turnSeq++
	e.agent.turnChangeSeq++
	turnID := fmt.Sprintf("turn-%d", e.agent.turnSeq)
	clientName := ""
	agentKind := ExternalAgentAuto
	if c, ok := e.AgentPrimaryClient(); ok {
		clientName = c.DisplayName
		agentKind = DetectExternalAgentKind(c.Name)
	}
	now := e.nowTime()
	e.agent.turns = append(e.agent.turns, AgentTurn{
		ID:        turnID,
		Prompt:    text,
		Source:    "panel",
		Status:    "running",
		AgentName: clientName,
		CreatedAt: now,
		StartedAt: now,
	})

	e.agent.activeRunID++
	runID := e.agent.activeRunID
	ctx, cancel := context.WithCancel(context.Background())
	e.agent.activeRunCancel = cancel
	e.agent.status = AgentStatusWorking
	e.agent.workingStep = 1
	e.agent.workingTotal = 0
	e.updateAgentAnnouncement()
	e.notify()

	req := AgentRequest{
		ThreadID:    e.agent.threadID,
		SessionID:   e.agent.sessionID,
		Prompt:      text,
		ProjectRoot: e.agentProjectRoot(),
		ScenePath:   e.ScenePath,
		SelectedID:  e.SelectedID,
		AgentKind:   agentKind,
		Permissions: e.agent.permissions,
	}

	go func() {
		err := runner.Run(ctx, req, func(ev AgentEvent) {
			e.postOrRun(func() {
				e.applyAgentEventForRun(runID, turnID, ev)
			})
		})
		e.postOrRun(func() {
			e.finishAgentRun(runID, turnID, err, ctx.Err() != nil)
		})
	}()
	return nil
}

// StopAgentRun cancels the currently running composer request, keeping completed steps visible.
func (e *Editor) StopAgentRun() {
	if e == nil || e.agent.activeRunCancel == nil {
		return
	}
	cancel := e.agent.activeRunCancel
	e.agent.activeRunCancel = nil
	cancel()
	if n := len(e.agent.turns); n > 0 && !e.agent.turns[n-1].Done {
		e.agent.turns[n-1].Stopped = true
		e.agent.turns[n-1].Done = true
		e.agent.turns[n-1].FinishedAt = e.nowTime()
	}
	e.restoreIdleAgentStatus()
	e.notify()
}

func (e *Editor) findAgentTurn(turnID string) *AgentTurn {
	for i := len(e.agent.turns) - 1; i >= 0; i-- {
		if e.agent.turns[i].ID == turnID {
			return &e.agent.turns[i]
		}
	}
	return nil
}

func (e *Editor) applyAgentEventForRun(runID uint64, turnID string, ev AgentEvent) {
	if e.agent.activeRunID != runID {
		return
	}
	turn := e.findAgentTurn(turnID)
	if turn == nil || turn.Done {
		return
	}
	switch ev.Kind {
	case AgentEventSession:
		if ev.SessionID != "" {
			e.agent.sessionID = ev.SessionID
		}
	case AgentEventStep:
		stepStatus := ev.StepStatus
		if stepStatus == "" {
			stepStatus = AgentStepRunning
		}
		updated := false
		if ev.StepID != "" {
			for i := range turn.Steps {
				if turn.Steps[i].ID == ev.StepID {
					if ev.StepTitle != "" {
						turn.Steps[i].Title = ev.StepTitle
					}
					if ev.StepDetail != "" {
						turn.Steps[i].Detail = ev.StepDetail
					}
					if ev.ToolName != "" {
						turn.Steps[i].ToolName = ev.ToolName
					}
					turn.Steps[i].Status = stepStatus
					updated = true
					break
				}
			}
		}
		if !updated {
			stepID := ev.StepID
			if stepID == "" {
				stepID = fmt.Sprintf("%s-step-%d", turn.ID, len(turn.Steps)+1)
			}
			turn.Steps = append(turn.Steps, AgentStep{
				ID:       stepID,
				Title:    ev.StepTitle,
				Detail:   ev.StepDetail,
				ToolName: ev.ToolName,
				Status:   stepStatus,
			})
		}
		if ev.StepIndex > 0 {
			e.agent.workingStep = ev.StepIndex
		} else {
			e.agent.workingStep = max(1, len(turn.Steps))
		}
		if ev.StepTotal > 0 {
			e.agent.workingTotal = ev.StepTotal
		}
		if stepStatus == AgentStepWaiting {
			e.SetAgentWaitingForYou(AgentOpenRequest{
				ID:    ev.StepID,
				Title: ev.StepTitle,
				Body:  ev.StepDetail,
			})
			return
		}
		if e.agent.openRequest == nil {
			e.agent.status = AgentStatusWorking
		}
	case AgentEventMessage:
		if ev.Text != "" {
			if turn.Summary != "" && !strings.HasSuffix(turn.Summary, "\n") && !strings.HasPrefix(ev.Text, "\n") {
				turn.Summary += "\n"
			}
			turn.Summary += ev.Text
		}
	case AgentEventUsage:
		turn.InputTokens += ev.InputTokens
		turn.OutputTokens += ev.OutputTokens
	case AgentEventError:
		if ev.Err != "" {
			turn.Error = ev.Err
		}
	case AgentEventDone:
		if ev.Text != "" && turn.Summary == "" {
			turn.Summary = ev.Text
			turn.Reply = ev.Text
		}
		turn.Done = true
		turn.Status = "done"
		turn.FinishedAt = e.nowTime()
	}
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.notify()
}

func (e *Editor) finishAgentRun(runID uint64, turnID string, err error, canceled bool) {
	if e.agent.activeRunID != runID {
		return
	}
	if e.agent.activeRunCancel != nil {
		e.agent.activeRunCancel()
		e.agent.activeRunCancel = nil
	}
	if turn := e.findAgentTurn(turnID); turn != nil && !turn.Done {
		turn.Done = true
		turn.FinishedAt = e.nowTime()
		if canceled {
			turn.Stopped = true
			turn.Status = "stopped"
		} else {
			turn.Status = "done"
			if err != nil && turn.Error == "" {
				turn.Error = err.Error()
			}
		}
		for i := range turn.Steps {
			if turn.Steps[i].Status == AgentStepRunning {
				if canceled || err != nil {
					turn.Steps[i].Status = AgentStepFailed
				} else {
					turn.Steps[i].Status = AgentStepDone
				}
			}
		}
	}
	e.agent.turnChangeSeq++
	e.restoreIdleAgentStatus()
	e.notify()
}

func (e *Editor) restoreIdleAgentStatus() {
	e.agent.workingStep = 0
	e.agent.workingTotal = 0
	if e.agent.openRequest != nil {
		e.agent.status = AgentStatusWaitingForYou
	} else if len(e.agent.clients) > 0 {
		e.agent.status = AgentStatusConnected
	} else if e.agent.status == AgentStatusWorking {
		if e.agent.hadClient {
			e.agent.status = AgentStatusDisconnected
		} else {
			e.agent.status = AgentStatusNotConnected
		}
	}
	e.updateAgentAnnouncement()
}

// ParseClaudeStreamLine decodes one JSONL line from `claude --print --output-format=stream-json --verbose` into incremental AgentEvents.
func ParseClaudeStreamLine(line []byte) ([]AgentEvent, bool) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	var env struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
		Result    string `json:"result"`
		IsError   bool   `json:"is_error"`
		Usage     *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Message *struct {
			Content []struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Name      string `json:"name"`
				Text      string `json:"text"`
				ToolUseID string `json:"tool_use_id"`
				IsError   bool   `json:"is_error"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		return nil, false
	}
	var events []AgentEvent
	if env.SessionID != "" {
		events = append(events, AgentEvent{Kind: AgentEventSession, SessionID: env.SessionID})
	}
	switch env.Type {
	case "assistant":
		if env.Message != nil {
			for _, block := range env.Message.Content {
				switch block.Type {
				case "tool_use":
					events = append(events, AgentEvent{
						Kind:       AgentEventStep,
						StepID:     block.ID,
						StepTitle:  formatAgentToolStep(block.Name),
						ToolName:   block.Name,
						StepStatus: AgentStepRunning,
					})
				case "text":
					if t := strings.TrimSpace(block.Text); t != "" {
						events = append(events, AgentEvent{
							Kind: AgentEventMessage,
							Text: t,
						})
					}
				}
			}
		}
	case "user":
		if env.Message != nil {
			for _, block := range env.Message.Content {
				if block.Type == "tool_result" && block.ToolUseID != "" {
					st := AgentStepDone
					if block.IsError {
						st = AgentStepFailed
					}
					events = append(events, AgentEvent{
						Kind:       AgentEventStep,
						StepID:     block.ToolUseID,
						StepStatus: st,
					})
				}
			}
		}
	case "result":
		if env.Usage != nil {
			events = append(events, AgentEvent{
				Kind:         AgentEventUsage,
				InputTokens:  env.Usage.InputTokens,
				OutputTokens: env.Usage.OutputTokens,
			})
		}
		if env.IsError {
			events = append(events, AgentEvent{Kind: AgentEventError, Err: env.Result})
		} else {
			events = append(events, AgentEvent{Kind: AgentEventDone, Text: strings.TrimSpace(env.Result)})
		}
	}
	return events, len(events) > 0
}

// ParseCodexStreamLine decodes one JSONL line from `codex exec --json` into incremental AgentEvents.
func ParseCodexStreamLine(line []byte) ([]AgentEvent, bool) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	var env struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread_id"`
		Message  string `json:"message"`
		Error    *struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Item *struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Tool    string `json:"tool"`
			Server  string `json:"server"`
			Command string `json:"command"`
			Text    string `json:"text"`
			Status  string `json:"status"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		return nil, false
	}
	var events []AgentEvent
	if env.ThreadID != "" {
		events = append(events, AgentEvent{Kind: AgentEventSession, SessionID: env.ThreadID})
	}
	switch env.Type {
	case "item.started", "item.updated", "item.completed":
		if env.Item != nil {
			if env.Item.Type == "agent_message" {
				if env.Type == "item.completed" && strings.TrimSpace(env.Item.Text) != "" {
					events = append(events, AgentEvent{
						Kind: AgentEventMessage,
						Text: strings.TrimSpace(env.Item.Text),
					})
				}
				break
			}
			st := AgentStepRunning
			if env.Type == "item.completed" {
				st = AgentStepDone
				if env.Item.Status == "failed" {
					st = AgentStepFailed
				}
			}
			title := env.Item.Type
			toolName := env.Item.Tool
			switch {
			case env.Item.Tool != "":
				title = formatAgentToolStep(env.Item.Tool)
			case env.Item.Command != "":
				title = "Run " + env.Item.Command
			case env.Item.Type == "file_change":
				title = "Updated project files"
			}
			events = append(events, AgentEvent{
				Kind:       AgentEventStep,
				StepID:     env.Item.ID,
				StepTitle:  title,
				ToolName:   toolName,
				StepStatus: st,
			})
		}
	case "turn.completed":
		if env.Usage != nil {
			events = append(events, AgentEvent{
				Kind:         AgentEventUsage,
				InputTokens:  env.Usage.InputTokens,
				OutputTokens: env.Usage.OutputTokens,
			})
		}
		events = append(events, AgentEvent{Kind: AgentEventDone})
	case "turn.failed", "error":
		msg := env.Message
		if msg == "" && env.Error != nil {
			msg = env.Error.Message
		}
		if msg == "" {
			msg = "Codex turn failed"
		}
		events = append(events, AgentEvent{Kind: AgentEventError, Err: msg})
	}
	return events, len(events) > 0
}

func formatAgentToolStep(toolName string) string {
	// Strip MCP prefix such as "mcp__nsgo-game-editor__scene_edit".
	name := toolName
	if idx := strings.LastIndex(name, "__"); idx >= 0 && idx+2 < len(name) {
		name = name[idx+2:]
	}
	switch name {
	case "scene_read":
		return "Read scene file"
	case "scene_edit":
		return "Edit scene"
	case "selection_get":
		return "Inspect selection"
	case "viewport_image":
		return "Capture viewport image"
	case "game_check":
		return "Run game check"
	case "game_playtest":
		return "Run headless playtest"
	default:
		return strings.ReplaceAll(name, "_", " ")
	}
}
