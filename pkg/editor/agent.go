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

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

const agentReconnectProbeTimeout = 2 * time.Second

// RightRailTab selects which panel is shown in the right sidebar.
type RightRailTab string

const (
	// RightTabInspector shows the property / scene / layer inspector (default).
	RightTabInspector RightRailTab = "inspector"
	// RightTabAgent shows the Agent panel with status chip, threads, and composer.
	RightTabAgent RightRailTab = "agent"
	// RightTabHistory shows the unified timeline of agent and manual steps.
	RightTabHistory RightRailTab = "history"

	// RightRailInspector, RightRailAgent, and RightRailHistory alias the right-rail tabs.
	RightRailInspector = RightTabInspector
	RightRailAgent     = RightTabAgent
	RightRailHistory   = RightTabHistory
)

// AgentMode selects how the editor connects to an AI agent.
type AgentMode string

const (
	// AgentModeUnconfigured is the initial state before the user completes the Connect dialog.
	AgentModeUnconfigured AgentMode = ""
	// AgentModeLocal connects to an external MCP agent running on this computer (Claude Code, Codex, or any MCP client).
	AgentModeLocal AgentMode = "local"
	// AgentModeAPIKey selects the built-in model path using an API key stored in NSGo's core.Secrets / system keychain.
	AgentModeAPIKey AgentMode = "apikey"
	// AgentModeNone disables agent features and keeps the Agent panel closed.
	AgentModeNone AgentMode = "none"
)

// AgentStatus is the live connection and work state shown by the Agent panel header's status chip.
type AgentStatus string

const (
	// AgentStatusNotConnected indicates no agent has connected yet.
	AgentStatusNotConnected AgentStatus = "not_connected"
	// AgentStatusConnected indicates an MCP client or built-in API key agent is attached and idle.
	AgentStatusConnected AgentStatus = "connected"
	// AgentStatusWorking indicates the agent is actively executing a request.
	AgentStatusWorking AgentStatus = "working"
	// AgentStatusWaitingForYou indicates a permission ask or clarifying question is open.
	AgentStatusWaitingForYou AgentStatus = "waiting"
	// AgentStatusReconnecting indicates the MCP connection dropped and retries are underway.
	AgentStatusReconnecting AgentStatus = "reconnecting"
	// AgentStatusDisconnected indicates all reconnect retries failed.
	AgentStatusDisconnected AgentStatus = "disconnected"
	// AgentStatusLimitReached indicates the weekly request limit set on Usage and privacy has been reached and requests queue until reset or until the person raises it.
	AgentStatusLimitReached AgentStatus = "limit_reached"
	// AgentStatusOffline indicates there is no network connection and requests wait in a queue.
	AgentStatusOffline AgentStatus = "offline"
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
	// AgentStepPlaceholder is the gray placeholder step shown immediately upon starting a request (within 300 ms) before the first real step arrives.
	AgentStepPlaceholder AgentStepStatus = "placeholder"
	AgentStepPending     AgentStepStatus = "pending"
	AgentStepRunning     AgentStepStatus = "running"
	AgentStepDone        AgentStepStatus = "done"
	AgentStepWaiting     AgentStepStatus = "waiting"
	AgentStepWarning     AgentStepStatus = "warning"
	AgentStepInterrupted AgentStepStatus = "interrupted"
	// AgentStepDenied is a step the user denied, shown with "You denied … · continued without it" and an explicit Allow now action.
	AgentStepDenied AgentStepStatus = "denied"
	// AgentStepBlocked is a step stopped by a Deny permission rule without prompting.
	AgentStepBlocked AgentStepStatus = "blocked"
	// AgentStepStopped is the terminal row added when the user stops the agent with Stop or Mod+.
	AgentStepStopped AgentStepStatus = "stopped"
	AgentStepFailed  AgentStepStatus = "failed"
)

// AgentStep is a single visible step in an agent turn's progress list.
type AgentStep struct {
	ID            string
	Title         string
	Detail        string
	ToolName      string
	Objects       []string
	Status        AgentStepStatus
	CanAllowNow   bool
	allowNowFunc  func()
	PermissionKey string
	RuleSummary   string
	RuleIndex     int
	UncheckedNote string
	StartedAt     time.Time
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

// AgentTurn is one user request (in-place ⌘L, Agent panel composer, terminal-initiated MCP turn, or background automation run)
// and its attached context, streamed steps, follow-ups, pending review, and reply.
type AgentTurn struct {
	ID              string                `json:"id"`
	Prompt          string                `json:"prompt"`
	Images          []AgentReferenceImage `json:"-"`                  // reference images sent with this turn (one-shot attachments and pinned brief references)
	Source          string                `json:"source,omitempty"`   // "in_place", "panel", "external", or "automation"
	SkipPlan        bool                  `json:"skipPlan,omitempty"` // true for small in-place ⌘L edits per Plan.dc.html
	Status          string                `json:"status,omitempty"`   // "pending", "running", "done", "stopped"
	External        bool                  `json:"external,omitempty"` // true for turns started by a terminal MCP client rather than the composer
	AgentName       string                `json:"agentName,omitempty"`
	Task            AgentTaskKind         `json:"task,omitempty"`
	Route           AgentModelRoute       `json:"route,omitempty"`
	Model           string                `json:"model,omitempty"`
	Steps           []AgentStep           `json:"steps,omitempty"`
	PlannedSteps    []string              `json:"plannedSteps,omitempty"`
	FollowUps       []AgentFollowUp       `json:"followUps,omitempty"`
	NotChecked      []string              `json:"notChecked,omitempty"`
	StoppedBefore   string                `json:"stoppedBefore,omitempty"`
	PendingReview   *AgentPendingReview   `json:"pendingReview,omitempty"`
	Summary         string                `json:"summary,omitempty"`
	Reply           string                `json:"reply,omitempty"`
	Error           string                `json:"error,omitempty"`
	ModelError      *AgentModelErrorState `json:"modelError,omitempty"`
	Stopped         bool                  `json:"stopped,omitempty"`
	Done            bool                  `json:"done,omitempty"`
	Partial         *AgentPartialState    `json:"partial,omitempty"`
	Stuck           *AgentStuckState      `json:"stuck,omitempty"`
	Lost            *AgentLostState       `json:"lost,omitempty"`
	StoppedState    *AgentStoppedState    `json:"stoppedState,omitempty"`
	Mismatch        *AgentMismatchReport  `json:"mismatch,omitempty"`
	Review          *AgentTurnReview      `json:"review,omitempty"`
	InputTokens     int                   `json:"inputTokens,omitempty"`
	OutputTokens    int                   `json:"outputTokens,omitempty"`
	CreatedAt       time.Time             `json:"createdAt"`
	StartedAt       time.Time             `json:"startedAt"`
	FinishedAt      time.Time             `json:"finishedAt"`
	Context         AgentTurnContext      `json:"context"`
	ChangedFiles    []string              `json:"changedFiles,omitempty"`
	ThreadID        string                `json:"threadId,omitempty"`
	SessionIDBefore string                `json:"sessionIdBefore,omitempty"`
	SessionIDAfter  string                `json:"sessionIdAfter,omitempty"`
	Automation      bool                  `json:"automation,omitempty"`     // true for turns started by an automation trigger
	AutomationID    string                `json:"automationId,omitempty"`   // rule ID from automations.yaml when Automation is true
	AutomationName  string                `json:"automationName,omitempty"` // WHEN summary of the triggering automation rule
	TriggerSummary  string                `json:"triggerSummary,omitempty"` // concrete trigger description, e.g. "File changed · scenes/level_01.scene.yaml"
	CheckpointID    string                `json:"checkpointId,omitempty"`   // checkpoint captured before the automation started
	ReviewState     AgentReviewState      `json:"reviewState,omitempty"`    // lifecycle state for Ready to review / Kept / Dismissed / Failed
	Plan            *AgentPlanReview      `json:"plan,omitempty"`

	turnStartHistoryIdx int
	baselineDoc         *sceneformat.SceneDoc
	baselineYAML        string
	baselineFile        string
	historyStart        int
	lastSeenIDs         map[string]struct{}
	mutations           []agentObjectMutation
	consecutiveFailTool string
	consecutiveFails    []AgentFailedCall
	lastMCPFailTool     string
	lastMCPFailAt       time.Time
}

// AgentOpenRequest represents a pending permission prompt or question waiting on the user.
type AgentOpenRequest struct {
	ID       string
	Title    string
	Body     string
	OpenedAt time.Time
}

// AgentRequest is passed to an AgentRunner or Editor.OnSendAgentComposerRequest when the user submits
// text from the composer or an automation triggers, carrying both runner/automation metadata
// and the exact context assembled from the active composer chips (and nothing else).
type AgentRequest struct {
	ThreadID         string
	SessionID        string
	Prompt           string
	ProjectRoot      string
	ScenePath        string
	SelectedID       string
	AgentKind        ExternalAgentKind
	Permissions      AgentPermissions
	Task             AgentTaskKind
	Route            AgentModelRoute
	Model            string
	PriorTurns       []AgentTurnSummary
	AutomationID     string
	AutomationName   string
	TriggerKind      AutomationTriggerKind
	TriggerDetail    string
	CheckpointID     string
	AttachedImages   []AgentReferenceImage // one-shot reference images dropped or pasted for this request only
	BriefImages      []AgentReferenceImage // project-wide reference images joined through brief.yaml's Always chip
	Images           []AgentReferenceImage // deduplicated union of AttachedImages and BriefImages sent with this request
	ResumeFromStep   int
	CompletedSteps   []string
	RemainingSteps   []string
	Hint             string
	PointedWorld     *[2]float32
	PointedTarget    string
	TryAnotherWay    bool
	SummarySeed      string
	Attachments      []AgentChipPayload
	ViewportImage    []byte
	ViewportWidth    int
	ViewportHeight   int
	HasViewportImg   bool
	AssembledContext string
}

// AgentEventKind classifies one streamed event from an AgentRunner.
type AgentEventKind string

const (
	AgentEventSession        AgentEventKind = "session"
	AgentEventStep           AgentEventKind = "step"
	AgentEventMessage        AgentEventKind = "message"
	AgentEventUsage          AgentEventKind = "usage"
	AgentEventDone           AgentEventKind = "done"
	AgentEventError          AgentEventKind = "error"
	AgentEventModelRecovered AgentEventKind = "model_recovered"
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
	CallSummary  string
	PlannedSteps []string
	Partial      *AgentPartialState
	Lost         *AgentLostState
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
	attachedImages     []AgentReferenceImage
	attachSeq          uint64
	attachImgVer       uint64
	attachErr          string
	composerDragOver   bool
	previewImageID     string
	lastPinNotice      *BriefPinNotice
	lastPinCmd         *pinReferenceImageCmd
	briefRefCache      map[string]cachedBriefRefImage
	briefMeta          cachedBriefMeta
	workingStep        int
	workingTotal       int
	activeRunCancel    context.CancelFunc
	activeRunID        uint64
	activeTurnID       string
	activeRetryNow     chan struct{}
	activeRetryCancel  func()
	queued             []AgentQueuedRequest
	queueSeq           int
	openRequest        *AgentOpenRequest
	focusRequestSeq    uint64
	focusRequestCalled int
	liveAnnouncement   string
	turnSeq            int
	turnChangeSeq      uint64
	// Registered MCP tools published by editormcp.Server and cached keychain presence for built-in API key.
	registeredTools   []AgentToolInfo
	builtInKeyChecked bool
	builtInKeyPresent bool
	// Per-thread bounded message history for the built-in Anthropic API runner (#1742).
	builtInHistoryMu  sync.Mutex
	builtInHistory    map[string]*builtInThreadHistory
	builtInHistorySeq uint64
	notify            agentNotifyState
	// Failure states, offline queue, viewport pointing, and turn review tracking.
	offline              bool
	pointingTurnID       string
	openReviewTurnID     string
	externalBaselineDoc  *sceneformat.SceneDoc
	externalBaselineYAML string
	externalBaselineFile string
	externalHistoryStart int
	externalBaselineIDs  map[string]struct{}
	// Live MCP request streaming, permission card, follow-up, and Stop state (#1012).
	liveRequestID       uint64
	liveRequestActive   bool
	liveStopped         bool
	liveStopCh          chan struct{}
	liveStepSeq         uint64
	liveAskSeq          uint64
	livePendingAsk      *agentPendingAskHandle
	liveDeniedKeys      map[string]bool
	liveQueuedFollowUps []string
	liveActivity        string
	liveFocusCardSeq    uint64
	liveFocusReviewSeq  uint64
	livePermCardFocused bool

	// Per-turn checkpoints and rewind / redo state.
	checkpoints         map[string]*agentTurnCheckpoint
	rewindOpenTurnID    string
	rewindMode          AgentRewindMode
	lastRewind          *agentRewindRecord
	composerHighlighted bool
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
	a.notify.initDefaults()
	a.rewindMode = AgentRewindBoth
}

func (e *Editor) nowTime() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// CancelAgentReconnect stops any pending reconnect backoff timer without changing the current status.
func (e *Editor) CancelAgentReconnect() {
	if e == nil {
		return
	}
	e.cancelAgentReconnect()
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
	e.stopAllTaskNotificationTimers()
	if e.agent.connectCopiedStop != nil {
		e.agent.connectCopiedStop()
		e.agent.connectCopiedStop = nil
	}
	if e.agent.activeRetryCancel != nil {
		e.agent.activeRetryCancel()
		e.agent.activeRetryCancel = nil
	}
	if e.agent.activeRunCancel != nil {
		e.agent.activeRunCancel()
		e.agent.activeRunCancel = nil
	}
	e.stopAutomationRunsAndScheduler()
	e.agent.activeRetryNow = nil
	e.agent.activeTurnID = ""
	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	e.closeLiveStopChLocked()
	e.agent.liveRequestActive = false
	e.agent.connectAPIKey = ""
}

// resetAgentForProject stops active agent runs/timers and clears per-project agent conversation,
// client sessions, open requests, queued requests, composer draft, and draft API key when switching projects or demos.
func (e *Editor) resetAgentForProject() {
	if e == nil {
		return
	}
	e.resetCodeReviewForProject()
	e.stopAgentTimers()
	e.CloseAskInPlace()
	e.resetTurnReviewForProject()
	e.resetTaskNotificationsForProject()
	e.sceneOverlays = nil
	e.resetPlanForProject()
	e.agent.activeRunID++
	e.agent.activeTurnID = ""
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
	e.clearAllBuiltInThreadHistory()
	e.agent.queued = nil
	e.agent.composerText = ""
	e.composerDraft = ""
	e.agent.attachedImages = nil
	e.agent.attachErr = ""
	e.agent.composerDragOver = false
	e.agent.previewImageID = ""
	e.agent.lastPinNotice = nil
	e.agent.lastPinCmd = nil
	e.agent.briefRefCache = nil
	e.agent.briefMeta = cachedBriefMeta{}
	e.resetAgentComposerForProject()
	e.agent.workingStep = 0
	e.agent.workingTotal = 0
	e.agent.openRequest = nil
	e.agent.offline = false
	e.agent.pointingTurnID = ""
	e.agent.openReviewTurnID = ""
	e.agent.externalBaselineDoc = nil
	e.agent.externalBaselineYAML = ""
	e.agent.externalBaselineFile = ""
	e.agent.externalHistoryStart = 0
	e.agent.externalBaselineIDs = nil
	e.agent.liveStopped = false
	e.agent.liveDeniedKeys = nil
	e.agent.liveQueuedFollowUps = nil
	e.agent.liveActivity = ""
	e.agent.livePermCardFocused = false
	e.agent.checkpoints = nil
	e.agent.rewindOpenTurnID = ""
	e.agent.rewindMode = AgentRewindBoth
	e.agent.lastRewind = nil
	e.agent.composerHighlighted = false
	if e.agent.mode == AgentModeAPIKey && e.HasAgentAPIKey(DefaultAgentAPIKeyAccount) {
		if e.AgentWeeklyLimitReached() {
			e.agent.status = AgentStatusLimitReached
		} else {
			e.agent.status = AgentStatusConnected
		}
	} else {
		e.agent.status = AgentStatusNotConnected
	}
	e.updateAgentAnnouncement()
}

// RightRailTab returns the active tab in the right sidebar (RightTabInspector, RightTabAgent, or RightTabHistory).
func (e *Editor) RightRailTab() RightRailTab {
	if e == nil || e.agent.rightTab == "" {
		return RightTabInspector
	}
	if e.agent.mode == AgentModeNone && e.agent.rightTab == RightTabAgent {
		return RightTabInspector
	}
	return e.agent.rightTab
}

// SetRightRailTab switches the right sidebar between Inspector, Agent, and History.
// When AgentModeNone is active, the Agent panel stays closed.
func (e *Editor) SetRightRailTab(tab RightRailTab) {
	if e == nil {
		return
	}
	if tab != RightTabAgent && tab != RightTabHistory {
		tab = RightTabInspector
	}
	if tab == RightTabAgent && e.agent.mode == AgentModeNone {
		tab = RightTabInspector
	}
	if e.agent.rightTab == tab {
		return
	}
	e.agent.rightTab = tab
	if tab != RightTabAgent {
		e.agentThreads.switcherOpen = false
		e.agentComposer.pickerOpen = false
		e.agentComposer.breakdownOpen = false
		e.agentComposer.hoveredChipID = ""
		e.agent.rewindOpenTurnID = ""
	}
	e.agentThreads.rev++
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

// AgentStatusTooltip returns the hover text for the Connected status chip, naming the agent and transport (for example "Claude Code over stdio" or "Anthropic API (built in)").
func (e *Editor) AgentStatusTooltip() string {
	if len(e.agent.clients) > 0 {
		return e.agent.clients[0].HoverLabel()
	}
	if e != nil && (e.agent.mode == AgentModeAPIKey || e.HasAgentAPIKey(DefaultAgentAPIKeyAccount)) {
		return e.BuiltInAgentProvider() + " (built in · key in system keychain)"
	}
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
		if len(e.agent.clients) == 0 && (e.agent.mode == AgentModeAPIKey || e.HasAgentAPIKey(DefaultAgentAPIKeyAccount)) {
			e.agent.liveAnnouncement = fmt.Sprintf("Connected via %s", e.BuiltInAgentProvider())
		} else {
			e.agent.liveAnnouncement = fmt.Sprintf("Connected via MCP: %s", e.AgentStatusTooltip())
		}
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
	case AgentStatusLimitReached:
		e.agent.liveAnnouncement = fmt.Sprintf("Weekly agent limit reached, resets at %s", e.AgentUsageResetLabel())
	case AgentStatusOffline:
		e.agent.liveAnnouncement = "Agent disconnected, offline"
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
	e.RefreshDetectedLocalAgents()
}

// OpenConnectDialogForMode opens the Connect an agent dialog pre-selected on mode (for example AgentModeAPIKey).
func (e *Editor) OpenConnectDialogForMode(mode AgentMode) {
	if e == nil {
		return
	}
	e.OpenConnectDialog()
	e.SetConnectChoice(mode)
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
	apiKeyDraft := strings.TrimSpace(e.agent.connectAPIKey)
	e.agent.mode = choice
	e.agent.permissions = e.agent.connectPermsDraft
	e.agent.connectOpen = false
	e.agent.connectTroubled = false
	e.agent.connectAPIKey = ""

	switch choice {
	case AgentModeNone:
		e.stopAgentTimers()
		e.agent.queued = nil
		e.agent.rightTab = RightTabInspector
		e.agent.rewindOpenTurnID = ""
		e.agent.status = AgentStatusNotConnected
		e.updateAgentAnnouncement()
	case AgentModeLocal:
		e.agent.rightTab = RightTabAgent
		if e.AgentWeeklyLimitReached() {
			e.agent.status = AgentStatusLimitReached
			e.updateAgentAnnouncement()
		} else if len(e.agent.clients) > 0 && e.agent.status == AgentStatusNotConnected {
			e.agent.status = AgentStatusConnected
			e.updateAgentAnnouncement()
		}
	case AgentModeAPIKey:
		e.agent.rightTab = RightTabAgent
		if apiKeyDraft != "" {
			if err := e.StoreAgentAPIKey(DefaultAgentAPIKeyAccount, apiKeyDraft); err != nil {
				e.Status = err.Error()
			}
		}
		if e.HasAgentAPIKey(DefaultAgentAPIKeyAccount) {
			if e.AgentWeeklyLimitReached() {
				e.agent.status = AgentStatusLimitReached
			} else if e.agent.status == AgentStatusNotConnected || e.agent.status == AgentStatusDisconnected {
				e.agent.status = AgentStatusConnected
			}
			e.updateAgentAnnouncement()
		}
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
	if e.agent.offline {
		e.agent.offline = false
	}
	if e.agent.status != AgentStatusWorking && e.agent.status != AgentStatusWaitingForYou {
		e.agent.status = AgentStatusConnected
	}
	e.markLostTurnReconnected()
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
	e.markActiveTurnLost(reason)
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

	taskID := e.agent.activeTurnID
	taskTitle := ""
	if taskID != "" {
		if turn := e.findAgentTurn(taskID); turn != nil {
			taskTitle = turn.Prompt
		}
	} else {
		taskID = cp.ID
	}
	e.setWaitingTurnTaskID(taskID)
	e.upsertTurnNotificationTask(AgentTask{
		ID:               taskID,
		Title:            taskTitle,
		PermissionPrompt: cp.Title,
		PermissionDetail: cp.Body,
		State:            TaskNeedsYou,
		ScenePath:        e.ScenePath,
	}, e.FilePath, cp.OpenedAt)
}

// ClearAgentWaitingForYou clears the open waiting request and restores Working or Connected status.
func (e *Editor) ClearAgentWaitingForYou() {
	if e == nil || e.agent.openRequest == nil {
		return
	}
	waitingTaskID := e.clearWaitingTurnTaskID()
	if waitingTaskID == "" {
		waitingTaskID = e.agent.openRequest.ID
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
	if waitingTaskID != "" {
		if e.agent.activeRunCancel != nil && waitingTaskID == e.agent.activeTurnID {
			if existing, filePath, ok := e.turnNotificationTaskByID(waitingTaskID); ok {
				existing.State = TaskRunning
				existing.PermissionPrompt = ""
				existing.PermissionDetail = ""
				e.upsertTurnNotificationTask(existing, filePath, time.Time{})
				return
			}
		} else {
			e.removeTurnNotificationTask(waitingTaskID)
			return
		}
	}
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

// FocusOpenAgentRequest opens the Agent panel and focuses the open waiting request or permission card.
func (e *Editor) FocusOpenAgentRequest() {
	if e == nil {
		return
	}
	if e.agent.livePendingAsk != nil && !e.agent.livePendingAsk.resolved {
		e.FocusPermissionCard()
		return
	}
	if e.agent.openRequest == nil {
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
	return e.composerDraft
}

// SetAgentComposerText updates the multi-line draft in the Agent composer.
func (e *Editor) SetAgentComposerText(text string) {
	if e == nil || (e.composerDraft == text && !e.agent.composerHighlighted) {
		return
	}
	e.agent.composerText = text
	e.agent.composerHighlighted = false
	e.updateComposerDraftFromInput(text)
}

// InsertAgentComposerNewline appends a newline to the composer draft (used by Shift+Enter).
func (e *Editor) InsertAgentComposerNewline() {
	if e == nil {
		return
	}
	next := e.composerDraft + "\n"
	e.agent.composerText = next
	e.agent.composerHighlighted = false
	e.updateComposerDraftFromInput(next)
}

func cloneAgentTurn(t AgentTurn) AgentTurn {
	if len(t.Steps) > 0 {
		steps := make([]AgentStep, len(t.Steps))
		for i, s := range t.Steps {
			s.Objects = slices.Clone(s.Objects)
			s.allowNowFunc = nil
			steps[i] = s
		}
		t.Steps = steps
	}
	if len(t.Images) > 0 {
		imgs := make([]AgentReferenceImage, len(t.Images))
		for i, img := range t.Images {
			imgs[i] = img.clone()
		}
		t.Images = imgs
	}
	t.PlannedSteps = slices.Clone(t.PlannedSteps)
	t.FollowUps = slices.Clone(t.FollowUps)
	t.NotChecked = slices.Clone(t.NotChecked)
	if t.PendingReview != nil {
		cp := *t.PendingReview
		cp.NotChecked = slices.Clone(t.PendingReview.NotChecked)
		t.PendingReview = &cp
	}
	t.ChangedFiles = slices.Clone(t.ChangedFiles)
	t.Context.SelectionIDs = slices.Clone(t.Context.SelectionIDs)
	t.Context.SelectionNames = slices.Clone(t.Context.SelectionNames)
	t.Context.ViewportPNG = slices.Clone(t.Context.ViewportPNG)
	if t.Partial != nil {
		p := *t.Partial
		t.Partial = &p
	}
	if t.Stuck != nil {
		s := *t.Stuck
		s.Failures = slices.Clone(t.Stuck.Failures)
		t.Stuck = &s
	}
	if t.Lost != nil {
		l := *t.Lost
		t.Lost = &l
	}
	if t.StoppedState != nil {
		st := *t.StoppedState
		t.StoppedState = &st
	}
	if t.Mismatch != nil {
		m := *t.Mismatch
		m.Rows = slices.Clone(t.Mismatch.Rows)
		t.Mismatch = &m
	}
	if t.Review != nil {
		t.Review = t.Review.Clone()
	}
	if t.Plan != nil {
		t.Plan = t.Plan.Clone()
	}
	t.baselineDoc = nil
	t.baselineYAML = ""
	t.baselineFile = ""
	t.lastSeenIDs = nil
	t.mutations = nil
	t.consecutiveFails = nil
	t.lastMCPFailTool = ""
	t.lastMCPFailAt = time.Time{}
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
		e.agent.turns[i].baselineDoc = nil
		e.agent.turns[i].baselineYAML = ""
		e.agent.turns[i].lastSeenIDs = nil
		e.agent.turns[i].mutations = nil
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
		e.refreshTurnReview(turn)
		if mm := CheckAgentSummaryAgainstDiff(turn.Summary, turn.Review, turn.mutations); mm != nil {
			turn.Mismatch = mm
			e.cancelAutoSaveTimer()
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

// AgentWorking reports whether an agent request (composer runner or live MCP session) is currently working or waiting for permission.
func (e *Editor) AgentWorking() bool {
	if e == nil {
		return false
	}
	return e.agent.activeRunCancel != nil || e.agent.liveRequestActive || e.agent.status == AgentStatusWorking || e.agent.status == AgentStatusWaitingForYou
}

// RecordExternalAgentToolCall records a tool call executed by a terminal-started MCP session so its work appears as an agent turn in the panel.
func (e *Editor) RecordExternalAgentToolCall(clientName, toolName, summary string, isErr bool) {
	if e == nil || e.agent.mode == AgentModeNone {
		return
	}
	// If a live MCP turn is active or was just stopped, live step streaming owns the steps.
	if e.agent.liveRequestActive || e.agent.liveStopped {
		return
	}
	e.RecordAgentToolCall(AgentToolCallRecord{
		ClientName:  clientName,
		ToolName:    toolName,
		StepTitle:   summary,
		CallSummary: summary,
		IsError:     isErr,
	})
}

// SubmitAgentPrompt submits prompt (or the current composer text when prompt is empty) to the agent routed for Scene and data edits and streams its steps into the Agent panel.
func (e *Editor) SubmitAgentPrompt(prompt string) error {
	return e.SubmitAgentTaskPrompt(AgentTaskSceneEdit, prompt)
}

// StopAgentRun cancels the currently running agent request or active running plan,
// keeping completed steps visible, recording done and not-done steps for Resume,
// and leaving changes unkept until reviewed.
func (e *Editor) StopAgentRun() {
	if e == nil {
		return
	}
	if e.IsPlanRunning() {
		_, _ = e.StopAgentPlan()
	}
	e.StopAgent()
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
	if len(ev.PlannedSteps) > 0 {
		turn.PlannedSteps = slices.Clone(ev.PlannedSteps)
	}
	if ev.Partial != nil {
		cp := *ev.Partial
		turn.Partial = &cp
	}
	if ev.Lost != nil {
		cp := *ev.Lost
		if cp.LostAt.IsZero() {
			cp.LostAt = e.nowTime()
		}
		turn.Lost = &cp
	}
	switch ev.Kind {
	case AgentEventSession:
		if ev.SessionID != "" {
			e.agent.sessionID = ev.SessionID
		}
	case AgentEventStep:
		dropTurnPlaceholder(turn)
		stepStatus := ev.StepStatus
		if stepStatus == "" {
			stepStatus = AgentStepRunning
		}
		if stepStatus == AgentStepRunning {
			for i := range turn.Steps {
				if turn.Steps[i].Status == AgentStepRunning && (ev.StepID == "" || turn.Steps[i].ID != ev.StepID) {
					turn.Steps[i].Status = AgentStepDone
				}
			}
		}
		updated := false
		resolvedToolName := ev.ToolName
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
					} else if resolvedToolName == "" {
						resolvedToolName = turn.Steps[i].ToolName
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
				ID:        stepID,
				Title:     ev.StepTitle,
				Detail:    ev.StepDetail,
				ToolName:  ev.ToolName,
				Status:    stepStatus,
				StartedAt: e.nowTime(),
			})
		}
		if stepStatus == AgentStepRunning && ev.StepTitle != "" {
			e.agent.liveActivity = fmt.Sprintf("Agent: %s · manual edits stay available", ev.StepTitle)
		}
		if ev.StepIndex > 0 {
			e.agent.workingStep = ev.StepIndex
		} else {
			e.agent.workingStep = max(1, len(turn.Steps))
		}
		if ev.StepTotal > 0 {
			e.agent.workingTotal = ev.StepTotal
		}
		e.observeTurnSceneMutations(turn, max(1, e.agent.workingStep))
		e.refreshTurnReview(turn)
		if stepStatus == AgentStepFailed {
			toolName := normalizeMCPToolName(resolvedToolName)
			if toolName != "" {
				// Avoid double-counting a failed MCP tool call already recorded by editormcp.Server.
				if turn.lastMCPFailTool == toolName && ev.CallSummary == "" && e.nowTime().Sub(turn.lastMCPFailAt) <= 2*time.Second {
					turn.lastMCPFailTool = ""
				} else {
					callSum := ev.CallSummary
					if callSum == "" {
						callSum = ev.StepTitle
					}
					errMsg := ev.Err
					if errMsg == "" {
						errMsg = ev.StepDetail
					}
					if e.recordConsecutiveToolFailure(turn, toolName, callSum, errMsg) {
						return
					}
				}
			}
		} else if stepStatus == AgentStepDone && resolvedToolName != "" {
			turn.consecutiveFailTool = ""
			turn.consecutiveFails = nil
			turn.lastMCPFailTool = ""
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
		if ev.Err != "" && turn.Lost == nil {
			turn.Error = ev.Err
		}
	case AgentEventModelRecovered:
		turn.ModelError = nil
	case AgentEventDone:
		dropTurnPlaceholder(turn)
		if ev.Text != "" && turn.Summary == "" {
			turn.Summary = ev.Text
			turn.Reply = ev.Text
		}
		turn.ModelError = nil
		turn.Done = true
		turn.Status = "done"
		turn.FinishedAt = e.nowTime()
		e.observeTurnSceneMutations(turn, max(1, len(turn.Steps)))
		e.refreshTurnReview(turn)
		if turn.Summary != "" {
			turn.Mismatch = CheckAgentSummaryAgainstDiff(turn.Summary, turn.Review, turn.mutations)
		}
	}
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.notify()
}

func (e *Editor) finishAgentRun(runID uint64, turnID string, err error, canceled bool) {
	if e.agent.activeRunID != runID {
		return
	}
	if e.agent.activeRetryCancel != nil {
		e.agent.activeRetryCancel()
		e.agent.activeRetryCancel = nil
	}
	e.agent.activeRetryNow = nil
	if e.agent.activeRunCancel != nil {
		e.agent.activeRunCancel()
		e.agent.activeRunCancel = nil
	}
	if e.agent.activeTurnID == turnID {
		e.agent.activeTurnID = ""
	}
	e.agent.liveRequestActive = false
	e.agent.liveActivity = ""
	turn := e.findAgentTurn(turnID)
	if turn != nil && !turn.Done {
		dropTurnPlaceholder(turn)
		turn.Done = true
		turn.FinishedAt = e.nowTime()
		if canceled {
			turn.Stopped = true
			turn.Status = "stopped"
			if turn.ModelError != nil {
				turn.ModelError.Retrying = false
			}
			e.populateStoppedTurnState(turn)
		} else {
			turn.Status = "done"
			if err != nil && turn.Error == "" && turn.Lost == nil {
				turn.Error = err.Error()
			}
			if turn.ModelError != nil {
				turn.ModelError.Retrying = false
			}
		}
		for i := range turn.Steps {
			if turn.Steps[i].Status == AgentStepRunning {
				if canceled {
					turn.Steps[i].Status = AgentStepPending
				} else if err != nil {
					turn.Steps[i].Status = AgentStepFailed
				} else {
					turn.Steps[i].Status = AgentStepDone
				}
			}
		}
		e.observeTurnSceneMutations(turn, max(1, len(turn.Steps)))
		e.refreshTurnReview(turn)
		if turn.Summary != "" && turn.Mismatch == nil {
			turn.Mismatch = CheckAgentSummaryAgainstDiff(turn.Summary, turn.Review, turn.mutations)
		}
		if len(turn.NotChecked) > 0 && turn.PendingReview == nil {
			turn.PendingReview = e.buildTurnPendingReviewLocked(turn, turn.Summary, false)
		}
	}
	if e.agent.openRequest != nil && turnID != "" &&
		(e.agent.openRequest.ID == turnID || e.waitingTurnTaskID() == turnID) {
		e.agent.openRequest = nil
		e.clearWaitingTurnTaskID()
	}
	e.agent.turnChangeSeq++
	e.finalizeTurnCheckpoint(turnID)
	e.restoreIdleAgentStatus()
	if !e.HasUnkeptAgentTurnReview() && !e.HasPendingAgentTurnReview() {
		e.scheduleAutoSaveIfNeeded()
	} else {
		e.cancelAutoSaveTimer()
	}
	if turn != nil {
		switch {
		case canceled || turn.Stopped:
			e.removeTurnNotificationTask(turnID)
			return
		case err != nil || turn.Error != "":
			detail := turn.Error
			if detail == "" && err != nil {
				detail = err.Error()
			}
			e.upsertTurnNotificationTask(AgentTask{
				ID:        turnID,
				Title:     turn.Prompt,
				Error:     detail,
				State:     TaskFailed,
				ScenePath: e.ScenePath,
			}, e.FilePath, time.Time{})
			return
		default:
			e.upsertTurnNotificationTask(AgentTask{
				ID:          turnID,
				Title:       turn.Prompt,
				StepSummary: turn.Summary,
				State:       TaskReadyToReview,
				ScenePath:   e.ScenePath,
			}, e.FilePath, time.Time{})
			return
		}
	}
	e.notify()
}

func (e *Editor) restoreIdleAgentStatus() {
	e.agent.workingStep = 0
	e.agent.workingTotal = 0
	if e.agent.offline {
		e.agent.status = AgentStatusOffline
	} else if e.agent.mode != AgentModeNone && e.AgentWeeklyLimitReached() {
		e.agent.status = AgentStatusLimitReached
		e.updateAgentAnnouncement()
		return
	} else if e.agent.openRequest != nil || e.PendingAgentPermissionAsk() != nil {
		e.agent.status = AgentStatusWaitingForYou
	} else if len(e.agent.clients) > 0 || (e.agent.mode == AgentModeAPIKey && e.HasAgentAPIKey(DefaultAgentAPIKeyAccount)) {
		e.agent.status = AgentStatusConnected
	} else if e.agent.status == AgentStatusWorking || e.agent.status == AgentStatusWaitingForYou || e.agent.status == AgentStatusLimitReached {
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
