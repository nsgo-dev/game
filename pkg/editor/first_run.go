package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
	"github.com/nsgo-dev/nsgo/core"
)

var errFirstRunPlaytestUnsupported = errors.New("editor: headless playtest requires desktop")

// FirstRunStage identifies the active stage of the Start with AI first-run journey
// (design/editor/FirstRun.dc.html).
type FirstRunStage int

const (
	// FirstRunStageNone means no first-run flow step is active (normal Start Screen or working editor).
	FirstRunStageNone FirstRunStage = iota
	// FirstRunStageConnect shows Connect an agent in place on the Start Screen while preserving idea and name.
	FirstRunStageConnect
	// FirstRunStagePlan shows the read-only template preview with planned ghosts and the agent's first plan before any folder is created.
	FirstRunStagePlan
	// FirstRunStageBuilding shows the working layout with the 5-stage bar, live object counts, Esc/Cancel confirmation, and check-failure fix proposal.
	FirstRunStageBuilding
	// FirstRunStageReview shows the first review with the plan summary, amber deviations, Before/After peek, per-change toggles, and one-time onboarding tips.
	FirstRunStageReview
)

// String returns a human-readable stage name.
func (s FirstRunStage) String() string {
	switch s {
	case FirstRunStageConnect:
		return "connect"
	case FirstRunStagePlan:
		return "plan"
	case FirstRunStageBuilding:
		return "building"
	case FirstRunStageReview:
		return "review"
	default:
		return "none"
	}
}

// Onboarding tip IDs persisted in EditorSettings.SeenTips so each first-review tip shows once per person
// and returns when the user triggers Help › Replay tips.
const (
	FirstRunTipKeepRevert   = "first_review.keep_revert"
	FirstRunTipHoldB        = "first_review.hold_b"
	FirstRunTipAskSelection = "first_review.ask_selection"
)

// FirstRunTipIDs returns the three first-review tip IDs in presentation order.
func FirstRunTipIDs() []string {
	return []string{
		FirstRunTipKeepRevert,
		FirstRunTipHoldB,
		FirstRunTipAskSelection,
	}
}

// FirstRunAgentOption describes one agent choice shown in Connect in place.
type FirstRunAgentOption struct {
	// Kind is "claude", "codex", or "apikey".
	Kind string
	// Name is the display label (such as "Claude Code", "Codex", or "An API key").
	Name string
	// Description is the short subtitle shown beneath Name.
	Description string
	// Detected reports whether a local CLI agent was found on PATH (ignored for "apikey").
	Detected bool
	// ExecutablePath holds the resolved binary path when Detected is true.
	ExecutablePath string
}

// FirstRunConnectStatus describes the current state of the Connect in place card.
type FirstRunConnectStatus string

const (
	FirstRunConnectIdle         FirstRunConnectStatus = "idle"
	FirstRunConnectConnecting   FirstRunConnectStatus = "connecting"
	FirstRunConnectConnected    FirstRunConnectStatus = "connected"
	FirstRunConnectTimeout      FirstRunConnectStatus = "timeout"
	FirstRunConnectKeyIdle      FirstRunConnectStatus = "key_idle"
	FirstRunConnectKeyValid     FirstRunConnectStatus = "key_valid"
	FirstRunConnectKeyInvalid   FirstRunConnectStatus = "key_invalid"
	FirstRunConnectKeyNoNetwork FirstRunConnectStatus = "key_no_network"
	FirstRunConnectKeyNoQuota   FirstRunConnectStatus = "key_no_quota"
)

// FirstRunAPIKeyStatus is returned by Editor.FirstRunValidateAPIKey.
type FirstRunAPIKeyStatus string

const (
	FirstRunAPIKeyEmpty       FirstRunAPIKeyStatus = ""
	FirstRunAPIKeyWorks       FirstRunAPIKeyStatus = "works"
	FirstRunAPIKeyNotAccepted FirstRunAPIKeyStatus = "not_accepted"
	FirstRunAPIKeyNoNetwork   FirstRunAPIKeyStatus = "no_network"
	FirstRunAPIKeyNoQuota     FirstRunAPIKeyStatus = "no_quota"
)

// FirstRunConnectStep is one checklist row inside the Connect in place card.
type FirstRunConnectStep struct {
	Text    string
	Done    bool
	Waiting bool
	Strong  bool
}

// FirstRunPlanRequest carries the Start Screen's idea, validated project name, and available templates
// to Editor.FirstRunPlanner before any project folder is created.
type FirstRunPlanRequest struct {
	Idea        string
	ProjectName string
	ParentDir   string
	TargetDir   string
	DisplayPath string
	Module      string
	Templates   []ProjectTemplate
	Selected    string
}

// FirstRunPlanOption is one answer card for a clarifying question in the first plan.
type FirstRunPlanOption struct {
	ID                 string
	Label              string
	Note               string
	Suggested          bool
	Verb               string
	StepTitleOverrides map[int]string
}

// FirstRunPlanQuestion is an optional clarifying question asked in the first plan before building.
type FirstRunPlanQuestion struct {
	Prompt  string
	Options []FirstRunPlanOption
}

// FirstRunPlanStep is one numbered step in the first plan card.
type FirstRunPlanStep struct {
	Number     int
	Title      string
	FilesLabel string
	Permission string
}

// FirstRunPreviewShape is a schematic 2D preview box rendered in the read-only template preview,
// live building viewport, or first-review viewport.
type FirstRunPreviewShape struct {
	ID       string
	ChangeID string
	Kind     string // "room", "floor", "platform", "divider", "object"
	X, Y     float32
	W, H     float32
	Outline  string // "", "added", "agent", "problem", "planned"
	Badge    string
}

// FirstRunGhost marks a planned addition on the read-only template preview viewport.
type FirstRunGhost struct {
	ID    string
	Step  int
	Label string
	Kind  string // "room", "planned", "object"
	X, Y  float32
	W, H  float32
}

// FirstRunPlan is the agent's proposed plan shown before Approve creates the project folder.
type FirstRunPlan struct {
	ProjectName      string
	TargetDir        string
	DisplayPath      string
	Module           string
	Idea             string
	Template         string
	TemplateTitle    string
	SceneName        string
	EstimatedFiles   int
	EstimatedObjects int
	EstimatedMinutes int
	Question         *FirstRunPlanQuestion
	SelectedAnswer   string
	Steps            []FirstRunPlanStep
	TemplateShapes   []FirstRunPreviewShape
	Ghosts           []FirstRunGhost
	Approved         bool
}

// FirstRunBuildStageStatus describes the status of one stage in the 5-stage progress bar.
type FirstRunBuildStageStatus string

const (
	FirstRunBuildStageTodo   FirstRunBuildStageStatus = "todo"
	FirstRunBuildStageActive FirstRunBuildStageStatus = "active"
	FirstRunBuildStageDone   FirstRunBuildStageStatus = "done"
	FirstRunBuildStageFail   FirstRunBuildStageStatus = "fail"
)

// FirstRunBuildStageNames are the 5 stage labels from FirstRun.dc.html.
var FirstRunBuildStageNames = [5]string{"Template", "Project", "First scene", "Check", "Playtest"}

// FirstRunBuildLogEntry is one item in the Building agent panel log.
type FirstRunBuildLogEntry struct {
	StageIndex int
	Status     FirstRunBuildStageStatus
	Title      string
	Subtitle   string
	DoneCount  int
	TotalCount int
}

// FirstRunBuildFixProposal is shown when the Check stage fails before entering the first review.
type FirstRunBuildFixProposal struct {
	ProblemSummary string
	ObjectID       string
	ObjectBadge    string
	FixDescription string
	ApplyFix       func(ctx context.Context, projectDir string) error
}

// FirstRunBuildEvent is emitted by FirstRunBuilder as stages and object placements advance.
type FirstRunBuildEvent struct {
	StageIndex   int
	StageStatus  FirstRunBuildStageStatus
	LogTitle     string
	LogSubtitle  string
	ObjectsDone  int
	ObjectsTotal int
	PlacingLabel string
	PlacedShapes []FirstRunPreviewShape
}

// FirstRunBuildRequest is passed to Editor.FirstRunBuilder after the target project folder is created.
type FirstRunBuildRequest struct {
	Plan         FirstRunPlan
	ProjectDir   string
	Answer       string
	WaitIfPaused func(context.Context) error
}

// FirstRunPlanDeviation describes an intentional difference between the approved plan and the built result,
// displayed in amber on top of the first review.
type FirstRunPlanDeviation struct {
	Summary string
}

// FirstRunReviewChange is one grouped change entry in the first review's left list.
type FirstRunReviewChange struct {
	ID        string
	Sign      string // "+"
	Title     string
	Detail    string
	ObjectIDs []string
	Reverted  bool
}

// FirstRunBuildResult is returned by Editor.FirstRunBuilder when building finishes or pauses on a check failure.
type FirstRunBuildResult struct {
	SceneRelPath   string
	PlanStepsDone  int
	PlanStepsTotal int
	Deviations     []FirstRunPlanDeviation
	Changes        []FirstRunReviewChange
	Shapes         []FirstRunPreviewShape
	CheckFailure   *FirstRunBuildFixProposal
}

// FirstRunBuildOutcome identifies the current mode of the Building stage.
type FirstRunBuildOutcome string

const (
	FirstRunBuildRunning        FirstRunBuildOutcome = "running"
	FirstRunBuildCancelPrompt   FirstRunBuildOutcome = "cancel_prompt"
	FirstRunBuildCheckFailed    FirstRunBuildOutcome = "check_failed"
	FirstRunBuildStoppedKept    FirstRunBuildOutcome = "stopped_kept"
	FirstRunBuildStoppedDeleted FirstRunBuildOutcome = "stopped_deleted"
)

// FirstRunBuildSnapshot is a read-only copy of the active Building screen state.
type FirstRunBuildSnapshot struct {
	ProjectName       string
	SceneName         string
	DisplayPath       string
	Outcome           FirstRunBuildOutcome
	Stages            [5]FirstRunBuildStageStatus
	Log               []FirstRunBuildLogEntry
	ObjectsDone       int
	ObjectsTotal      int
	PlacingLabel      string
	Shapes            []FirstRunPreviewShape
	Elapsed           time.Duration
	CreatedByFlow     bool
	CanDeleteFolder   bool
	CreatedAgeSeconds int
	CheckFailure      *FirstRunBuildFixProposal
	StatusPill        string
}

// FirstRunReviewSnapshot is a read-only copy of the active First Review screen state.
type FirstRunReviewSnapshot struct {
	ProjectName    string
	ProjectDir     string
	SceneRelPath   string
	PlanStepsDone  int
	PlanStepsTotal int
	Deviations     []FirstRunPlanDeviation
	Changes        []FirstRunReviewChange
	Shapes         []FirstRunPreviewShape
	BeforeMode     bool
	PeekBefore     bool
	ShowingBefore  bool
	KeptCount      int
	TotalCount     int
	ActiveTipIndex int
	ActiveTipID    string
}

type firstRunBuildRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once

	pauseMu sync.Mutex
	paused  bool
	resumeC chan struct{}
}

func newFirstRunBuildRun(cancel context.CancelFunc) *firstRunBuildRun {
	return &firstRunBuildRun{
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

func (r *firstRunBuildRun) finish() {
	r.once.Do(func() {
		r.setPaused(false)
		if r.cancel != nil {
			r.cancel()
		}
		close(r.done)
	})
}

func (r *firstRunBuildRun) setPaused(p bool) {
	r.pauseMu.Lock()
	defer r.pauseMu.Unlock()
	if p {
		if !r.paused {
			r.paused = true
			r.resumeC = make(chan struct{})
		}
		return
	}
	if r.paused {
		r.paused = false
		if r.resumeC != nil {
			close(r.resumeC)
			r.resumeC = nil
		}
	}
}

func (r *firstRunBuildRun) waitIfPaused(ctx context.Context) error {
	for {
		r.pauseMu.Lock()
		if !r.paused {
			r.pauseMu.Unlock()
			return ctx.Err()
		}
		ch := r.resumeC
		r.pauseMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

// First-run API key system keychain service and account identifiers (never written to YAML).
const (
	FirstRunAPIKeyKeychainService = "dev.nsgo.game.agent"
	FirstRunAPIKeyKeychainAccount = "api_key"
)

type firstRunState struct {
	mu    sync.Mutex
	scope *core.Scope

	stage FirstRunStage

	// Connected agent state (survives returning to Start Screen in the same session).
	agentConnected bool
	agentKind      string
	agentName      string
	agentTools     int

	// Connect in place state.
	agentOptions           []FirstRunAgentOption
	selectedAgent          string
	connectStatus          FirstRunConnectStatus
	connectStartedAt       time.Time
	connectErr             string
	diagnosticsCopied      bool
	apiKey                 string
	apiKeySeq              uint64
	apiKeyStoredInKeychain bool
	connectCancel          context.CancelFunc
	connectSeq             uint64
	connectDone            chan struct{}

	// Plan state (no project folder created until Approve).
	plan         *FirstRunPlan
	planning     bool
	planLoading  bool
	planErr      string
	planCancel   context.CancelFunc
	planSeq      uint64
	planDone     chan struct{}
	proposedPlan *AgentPlan

	// Building state.
	buildOutcome      FirstRunBuildOutcome
	buildStages       [5]FirstRunBuildStageStatus
	buildLog          []FirstRunBuildLogEntry
	buildObjectsDone  int
	buildObjectsTotal int
	buildPlacingLabel string
	buildShapes       []FirstRunPreviewShape
	buildStartedAt    time.Time
	buildCheckFailure *FirstRunBuildFixProposal
	buildPendingRes   *FirstRunBuildResult
	buildRun          *firstRunBuildRun
	// Folder ownership safety guard: ONLY set when this flow created a brand-new folder after Approve.
	createdByFlow bool
	createdDir    string
	createdAt     time.Time

	// Review state.
	reviewProjectName string
	reviewProjectDir  string
	reviewScenePath   string
	reviewPlanDone    int
	reviewPlanTotal   int
	reviewDeviations  []FirstRunPlanDeviation
	reviewChanges     []FirstRunReviewChange
	reviewShapes      []FirstRunPreviewShape
	reviewBeforeMode  bool
	reviewPeekBefore  bool
	reviewTipIndex    int // 0..2 or -1 when all seen/skipped
}

// SeenTips returns a sorted copy of the onboarding tip IDs already seen or skipped in EditorSettings.
func (e *Editor) SeenTips() []string {
	if e == nil {
		return nil
	}
	return e.Settings().SeenTips
}

// HasSeenTip reports whether tipID has been seen or skipped in EditorSettings.
func (e *Editor) HasSeenTip(tipID string) bool {
	if e == nil || tipID == "" {
		return false
	}
	return slices.Contains(e.settings.SeenTips, tipID)
}

// MarkTipsSeen records one or more tip IDs in EditorSettings.SeenTips, saving to SettingsPath when set.
func (e *Editor) MarkTipsSeen(ids ...string) error {
	if e == nil || len(ids) == 0 {
		return nil
	}
	cur := e.Settings()
	changed := false
	for _, id := range ids {
		if err := validateTipID(id); err != nil {
			return err
		}
		if !slices.Contains(cur.SeenTips, id) {
			cur.SeenTips = append(cur.SeenTips, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	slices.Sort(cur.SeenTips)
	e.settings = cur
	e.syncFirstRunTipsFromSettings()
	return e.persistSettingsAndNotify()
}

// ReplayTips clears the first-run review tip IDs from EditorSettings.SeenTips so each tip shows again
// (wired to Help › Replay tips and the first review's Replay tips button).
func (e *Editor) ReplayTips() error {
	if e == nil {
		return nil
	}
	firstRunIDs := FirstRunTipIDs()
	cur := e.Settings()
	var kept []string
	changed := false
	for _, id := range cur.SeenTips {
		if slices.Contains(firstRunIDs, id) {
			changed = true
			continue
		}
		kept = append(kept, id)
	}
	if changed {
		cur.SeenTips = kept
		e.settings = cur
	}
	e.firstRun.mu.Lock()
	inReview := e.firstRun.stage == FirstRunStageReview
	e.firstRun.reviewTipIndex = 0
	e.firstRun.mu.Unlock()
	if changed {
		if err := e.persistSettingsAndNotify(); err != nil {
			return err
		}
	} else {
		e.notify()
	}
	if !inReview {
		e.ShowToast(Toast{
			ID:      "first-run-replay-tips",
			Kind:    ToastInfo,
			Message: "Review tips will show again on your next first review.",
		})
	}
	return nil
}

func (e *Editor) syncFirstRunTipsFromSettings() {
	if e == nil {
		return
	}
	nextIdx := e.nextUnseenFirstRunTipIndex()
	e.firstRun.mu.Lock()
	e.firstRun.reviewTipIndex = nextIdx
	e.firstRun.mu.Unlock()
}

func (e *Editor) nextUnseenFirstRunTipIndex() int {
	for i, id := range FirstRunTipIDs() {
		if !e.HasSeenTip(id) {
			return i
		}
	}
	return -1
}

// FirstRunStage returns the active stage of the Start with AI first-run flow.
func (e *Editor) FirstRunStage() FirstRunStage {
	if e == nil {
		return FirstRunStageNone
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.stage
}

// FirstRunFullScreenOpen reports whether a full-window first-run screen (Plan, Building, or Review) is active.
func (e *Editor) FirstRunFullScreenOpen() bool {
	st := e.FirstRunStage()
	return st == FirstRunStagePlan || st == FirstRunStageBuilding || st == FirstRunStageReview
}

func (s *firstRunState) setScope(cx *core.Scope) {
	s.mu.Lock()
	s.scope = cx
	s.mu.Unlock()
}

// FirstRunAPIKeyStoredInKeychain reports whether the validated Start with AI API key was persisted to Editor.Keychain.
func (e *Editor) FirstRunAPIKeyStoredInKeychain() bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.apiKeyStoredInKeychain
}

// FirstRunAgentConnected reports whether an agent is currently connected for Start with AI,
// including an active MCP client or configured API-key mode with a stored/validated API key.
func (e *Editor) FirstRunAgentConnected() bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	connected := e.firstRun.agentConnected
	hasMemKey := strings.TrimSpace(e.firstRun.apiKey) != ""
	e.firstRun.mu.Unlock()
	if connected {
		return true
	}
	if e.AgentStatus() == AgentStatusConnected || len(e.AgentClients()) > 0 {
		return true
	}
	if e.AgentMode() == AgentModeAPIKey {
		return hasMemKey || e.HasAgentAPIKey(DefaultAgentAPIKeyAccount)
	}
	return false
}

// firstRunMCPToolCount queries the editor's MCP server (Editor.AgentTools) for the number of enabled tools.
func (e *Editor) firstRunMCPToolCount(ctx context.Context) (int, error) {
	if e == nil || e.AgentTools == nil {
		return 0, errors.New("editor MCP server is not running")
	}
	specs, err := e.AgentTools.ListTools(ctx)
	if err != nil {
		return 0, fmt.Errorf("editor MCP server failed to list tools: %w", err)
	}
	if len(specs) == 0 {
		return 0, errors.New("editor MCP server reports 0 enabled tools")
	}
	return len(specs), nil
}

// firstRunLiveMCPToolCount returns the live count of enabled MCP tools, or 0 when no MCP server is attached.
func (e *Editor) firstRunLiveMCPToolCount() int {
	if e == nil || e.AgentTools == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, err := e.firstRunMCPToolCount(ctx)
	if err != nil {
		return 0
	}
	return n
}

// FirstRunConnectError returns the last connection error message from Connect in place, if any.
func (e *Editor) FirstRunConnectError() string {
	if e == nil {
		return ""
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.connectErr
}

// FirstRunConnectedAgent returns the connected agent's kind, display name, and tool count,
// reflecting both Start with AI Connect and active #1200 Agent/MCP connections.
func (e *Editor) FirstRunConnectedAgent() (kind, name string, tools int) {
	if e == nil {
		return "", "", 0
	}
	e.firstRun.mu.Lock()
	if e.firstRun.agentConnected {
		kind, name, tools = e.firstRun.agentKind, e.firstRun.agentName, e.firstRun.agentTools
		e.firstRun.mu.Unlock()
		if tools <= 0 {
			tools = e.firstRunLiveMCPToolCount()
		}
		return kind, name, tools
	}
	hasMemKey := strings.TrimSpace(e.firstRun.apiKey) != ""
	e.firstRun.mu.Unlock()

	if e.AgentStatus() == AgentStatusConnected || len(e.AgentClients()) > 0 {
		client, ok := e.AgentPrimaryClient()
		display := "Agent"
		inferredKind := "local"
		if ok {
			if client.DisplayName != "" {
				display = client.DisplayName
			} else if client.Name != "" {
				display = FriendlyAgentName(client.Name)
			}
			lower := strings.ToLower(client.Name + " " + display)
			switch {
			case strings.Contains(lower, "codex"):
				inferredKind = "codex"
			case strings.Contains(lower, "claude"):
				inferredKind = "claude"
			default:
				inferredKind = "local"
			}
		} else if e.AgentMode() == AgentModeAPIKey {
			inferredKind = "apikey"
			display = firstRunAgentDisplayName("apikey")
		}
		return inferredKind, display, e.firstRunLiveMCPToolCount()
	}
	if e.AgentMode() == AgentModeAPIKey && (hasMemKey || e.HasAgentAPIKey(DefaultAgentAPIKeyAccount)) {
		return "apikey", firstRunAgentDisplayName("apikey"), e.firstRunLiveMCPToolCount()
	}
	return "", "", 0
}

// SetFirstRunAgentConnected configures whether an agent is already connected (for example when
// an MCP session is active before pressing Start with AI).
func (e *Editor) SetFirstRunAgentConnected(connected bool, kind, name string, tools int) {
	if e == nil {
		return
	}
	if connected && tools <= 0 {
		tools = e.firstRunLiveMCPToolCount()
	}
	e.firstRun.mu.Lock()
	e.firstRun.agentConnected = connected
	if connected {
		if kind == "" {
			kind = "local"
		}
		if name == "" {
			name = firstRunAgentDisplayName(kind)
		}
		if tools < 0 {
			tools = 0
		}
		e.firstRun.agentKind = kind
		e.firstRun.agentName = name
		e.firstRun.agentTools = tools
	} else {
		e.firstRun.agentKind = ""
		e.firstRun.agentName = ""
		e.firstRun.agentTools = 0
	}
	e.firstRun.mu.Unlock()
	e.notify()
}

func firstRunAgentDisplayName(kind string) string {
	switch kind {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "apikey":
		return "Built-in agent"
	default:
		return "Agent"
	}
}

func detectLocalAgents() []FirstRunAgentOption {
	var claudePath, codexPath string
	if canRunGameDesktop {
		claudePath, _ = resolveLocalAgentBinary("claude")
		codexPath, _ = resolveLocalAgentBinary("codex")
	}
	claudeDesc := "Not installed"
	if claudePath != "" {
		claudeDesc = "Found on this computer"
	}
	codexDesc := "Not installed"
	if codexPath != "" {
		codexDesc = "Found on this computer"
	}
	return []FirstRunAgentOption{
		{
			Kind:           "claude",
			Name:           "Claude Code",
			Description:    claudeDesc,
			Detected:       claudePath != "",
			ExecutablePath: claudePath,
		},
		{
			Kind:           "codex",
			Name:           "Codex",
			Description:    codexDesc,
			Detected:       codexPath != "",
			ExecutablePath: codexPath,
		},
		{
			Kind:        "apikey",
			Name:        "An API key",
			Description: "The editor calls the model itself",
			Detected:    true,
		},
	}
}

// DetectedLocalAgents returns the cached list of local agent detection results (Claude Code, Codex, API key),
// running detection lazily on first call without notifying listeners (safe to read inside render closures).
// Call RefreshDetectedLocalAgents to re-scan the machine and notify UI listeners.
func (e *Editor) DetectedLocalAgents() []FirstRunAgentOption {
	if e == nil {
		return nil
	}
	e.firstRun.mu.Lock()
	if len(e.firstRun.agentOptions) > 0 {
		out := slices.Clone(e.firstRun.agentOptions)
		e.firstRun.mu.Unlock()
		return out
	}
	e.firstRun.mu.Unlock()

	detector := e.FirstRunDetectAgents
	if detector == nil {
		detector = detectLocalAgents
	}
	opts := detector()
	if len(opts) == 0 {
		opts = detectLocalAgents()
	}
	e.firstRun.mu.Lock()
	if len(e.firstRun.agentOptions) == 0 {
		e.firstRun.agentOptions = slices.Clone(opts)
	}
	out := slices.Clone(e.firstRun.agentOptions)
	e.firstRun.mu.Unlock()
	return out
}

// RefreshDetectedLocalAgents re-scans locally installed agent CLIs (respecting FirstRunDetectAgents if set),
// caches the result, and if Step 2 Connect is currently active with "apikey" selected only because no local
// agent was previously found, auto-selects the first newly detected local agent.
func (e *Editor) RefreshDetectedLocalAgents() []FirstRunAgentOption {
	if e == nil {
		return nil
	}
	detector := e.FirstRunDetectAgents
	if detector == nil {
		detector = detectLocalAgents
	}
	opts := detector()
	if len(opts) == 0 {
		opts = detectLocalAgents()
	}

	var autoSelect string
	e.firstRun.mu.Lock()
	e.firstRun.agentOptions = slices.Clone(opts)
	if e.firstRun.stage == FirstRunStageConnect && (e.firstRun.selectedAgent == "" || (e.firstRun.selectedAgent == "apikey" && strings.TrimSpace(e.firstRun.apiKey) == "")) {
		for _, o := range opts {
			if o.Kind != "apikey" && o.Detected {
				autoSelect = o.Kind
				break
			}
		}
	}
	e.firstRun.mu.Unlock()

	if autoSelect != "" {
		_ = e.SelectFirstRunAgent(autoSelect)
	} else {
		e.notify()
	}
	return slices.Clone(opts)
}

// StartWithAI begins the Start with AI journey from the Start Screen.
// When no agent is connected yet, it opens Connect in place on the Start Screen while keeping
// the idea and project name intact; when an agent is already connected, it opens the first plan directly.
// No project folder is created until the author approves the plan.
func (e *Editor) StartWithAI() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.ensureStartScreenInit()
	if err := e.goToolchainError(); err != nil {
		e.reportStatusError(err.Error())
		return err
	}

	name := e.StartProjectName()
	parentDir := e.StartProjectParentDir()
	module := e.StartProjectModule()
	v := ValidateNewProjectInputs(name, parentDir, module)
	e.startScreen.mu.Lock()
	e.startScreen.validation = v
	e.startScreen.aiNotice = ""
	e.startScreen.mu.Unlock()
	if !v.OK {
		e.notify()
		return errors.New(v.Message)
	}

	if e.FirstRunAgentConnected() {
		return e.OpenFirstRunPlan()
	}

	detector := e.FirstRunDetectAgents
	if detector == nil {
		detector = detectLocalAgents
	}
	opts := detector()
	if len(opts) == 0 {
		opts = detectLocalAgents()
	}

	selected := "apikey"
	for _, o := range opts {
		if o.Kind != "apikey" && o.Detected {
			selected = o.Kind
			break
		}
	}

	e.cancelFirstRunConnect()
	var existingKey string
	var keySeq uint64
	e.firstRun.mu.Lock()
	e.firstRun.stage = FirstRunStageConnect
	e.firstRun.agentOptions = slices.Clone(opts)
	e.firstRun.selectedAgent = selected
	e.firstRun.diagnosticsCopied = false
	e.firstRun.connectErr = ""
	if selected == "apikey" {
		existingKey = e.firstRun.apiKey
		e.firstRun.apiKeySeq++
		keySeq = e.firstRun.apiKeySeq
		e.firstRun.connectStatus = FirstRunConnectKeyIdle
	} else {
		e.firstRun.connectStatus = FirstRunConnectConnecting
		e.firstRun.connectStartedAt = e.now()
	}
	e.firstRun.mu.Unlock()

	if selected == "apikey" && existingKey != "" {
		st := e.connectStatusForAPIKey(existingKey)
		e.firstRun.mu.Lock()
		if e.firstRun.selectedAgent == "apikey" && e.firstRun.apiKeySeq == keySeq {
			e.firstRun.connectStatus = st
		}
		e.firstRun.mu.Unlock()
	}

	e.SetStartScreenOpen(true)
	e.notify()
	if selected != "apikey" {
		e.startFirstRunConnectHandshake(selected, v.DisplayPath)
	}
	return nil
}

// FirstRunAgentOptions returns the agent choices displayed in Connect in place.
func (e *Editor) FirstRunAgentOptions() []FirstRunAgentOption {
	if e == nil {
		return nil
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return slices.Clone(e.firstRun.agentOptions)
}

// FirstRunSelectedAgent returns the selected agent kind ("claude", "codex", or "apikey") in Connect in place.
func (e *Editor) FirstRunSelectedAgent() string {
	if e == nil {
		return ""
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.selectedAgent
}

// FirstRunConnectStatus returns the active Connect in place status.
func (e *Editor) FirstRunConnectStatus() FirstRunConnectStatus {
	if e == nil {
		return FirstRunConnectIdle
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.connectStatus
}

// FirstRunConnectSteps returns the progress checklist rows for the Connect in place card.
func (e *Editor) FirstRunConnectSteps() []FirstRunConnectStep {
	if e == nil {
		return nil
	}
	e.firstRun.mu.Lock()
	kind := e.firstRun.selectedAgent
	st := e.firstRun.connectStatus
	tools := e.firstRun.agentTools
	started := e.firstRun.connectStartedAt
	e.firstRun.mu.Unlock()

	if kind == "apikey" || st == FirstRunConnectTimeout {
		return nil
	}
	displayPath := e.StartNameStatus().DisplayPath
	if displayPath == "" {
		displayPath = "~/games/" + e.StartProjectName()
	}
	agentName := firstRunAgentDisplayName(kind)
	elapsed := max(int(e.now().Sub(started).Seconds()), 0)
	step3 := FirstRunConnectStep{
		Text:    fmt.Sprintf("Asking %s to connect · %d s", agentName, elapsed),
		Waiting: true,
	}
	if st == FirstRunConnectConnected {
		step3Text := fmt.Sprintf("%s connected", agentName)
		if tools > 0 {
			step3Text = fmt.Sprintf("%s connected · %d tools", agentName, tools)
		}
		step3 = FirstRunConnectStep{
			Text:   step3Text,
			Done:   true,
			Strong: true,
		}
	}
	mcpReady := e.AgentTools != nil || st == FirstRunConnectConnected
	return []FirstRunConnectStep{
		{Text: "Prepared MCP config for " + displayPath + " (written on Approve)", Done: true},
		{Text: "Started the editor's MCP server", Done: mcpReady, Waiting: !mcpReady},
		step3,
	}
}

// SelectFirstRunAgent selects kind ("claude", "codex", or "apikey") in Connect in place.
func (e *Editor) SelectFirstRunAgent(kind string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.firstRun.mu.Lock()
	var opt *FirstRunAgentOption
	for i := range e.firstRun.agentOptions {
		if e.firstRun.agentOptions[i].Kind == kind {
			o := e.firstRun.agentOptions[i]
			opt = &o
			break
		}
	}
	e.firstRun.mu.Unlock()
	if opt == nil {
		return fmt.Errorf("editor: unknown agent %q", kind)
	}
	if kind != "apikey" && !opt.Detected {
		return fmt.Errorf("editor: agent %s is not installed", opt.Name)
	}

	e.cancelFirstRunConnect()
	e.firstRun.mu.Lock()
	e.firstRun.selectedAgent = kind
	e.firstRun.diagnosticsCopied = false
	e.firstRun.connectErr = ""
	if kind == "apikey" {
		existingKey := e.firstRun.apiKey
		e.firstRun.apiKeySeq++
		keySeq := e.firstRun.apiKeySeq
		e.firstRun.connectStatus = FirstRunConnectKeyIdle
		e.firstRun.mu.Unlock()
		if existingKey != "" {
			st := e.connectStatusForAPIKey(existingKey)
			e.firstRun.mu.Lock()
			if e.firstRun.selectedAgent == "apikey" && e.firstRun.apiKeySeq == keySeq {
				e.firstRun.connectStatus = st
			}
			e.firstRun.mu.Unlock()
		}
		e.notify()
		return nil
	}
	e.firstRun.connectStatus = FirstRunConnectConnecting
	e.firstRun.connectStartedAt = e.now()
	e.firstRun.mu.Unlock()

	e.notify()
	e.startFirstRunConnectHandshake(kind, e.StartNameStatus().DisplayPath)
	return nil
}

func (e *Editor) cancelFirstRunConnect() {
	e.firstRun.mu.Lock()
	cancel := e.firstRun.connectCancel
	e.firstRun.connectCancel = nil
	e.firstRun.connectSeq++
	e.firstRun.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WaitForFirstRunConnect blocks until any active Start with AI connect handshake goroutine completes.
func (e *Editor) WaitForFirstRunConnect() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	done := e.firstRun.connectDone
	e.firstRun.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (e *Editor) startFirstRunConnectHandshake(kind, displayPath string) {
	timeout := e.FirstRunConnectTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	done := make(chan struct{})
	e.firstRun.mu.Lock()
	e.firstRun.connectSeq++
	seq := e.firstRun.connectSeq
	e.firstRun.connectCancel = cancel
	e.firstRun.connectDone = done
	e.firstRun.mu.Unlock()

	connector := e.FirstRunConnectAgent
	if connector == nil {
		connector = e.defaultFirstRunConnectAgent
	}

	go func() {
		defer cancel()
		tools, err := connector(ctx, kind, displayPath)
		apply := func() {
			defer close(done)
			e.firstRun.mu.Lock()
			if e.firstRun.connectSeq != seq || e.firstRun.stage != FirstRunStageConnect {
				e.firstRun.mu.Unlock()
				return
			}
			e.firstRun.connectCancel = nil
			if err != nil {
				if errors.Is(err, context.Canceled) {
					e.firstRun.mu.Unlock()
					return
				}
				e.firstRun.connectStatus = FirstRunConnectTimeout
				e.firstRun.connectErr = err.Error()
				e.firstRun.mu.Unlock()
				e.notify()
				return
			}
			if tools < 0 {
				tools = 0
			}
			e.firstRun.connectStatus = FirstRunConnectConnected
			e.firstRun.agentTools = tools
			e.firstRun.connectErr = ""
			e.firstRun.mu.Unlock()
			e.notify()
		}
		e.firstRunPostOrRun(apply)
	}()
}

// defaultFirstRunConnectAgent verifies the editor's MCP server tool list and starts the chosen CLI agent
// through Editor.AgentRunner (or CLIAgentRunner on desktop) with a read-only readiness probe, failing visibly
// whenever the MCP server or CLI agent cannot answer.
func (e *Editor) defaultFirstRunConnectAgent(ctx context.Context, kind, _ string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	var agentKind ExternalAgentKind
	switch kind {
	case "claude":
		agentKind = ExternalAgentClaudeCode
	case "codex":
		agentKind = ExternalAgentCodex
	default:
		return 0, fmt.Errorf("unsupported local agent kind %q", kind)
	}

	tools, err := e.firstRunMCPToolCount(ctx)
	if err != nil {
		return 0, err
	}

	runner := e.AgentRunner
	if runner == nil {
		runner = defaultCLIAgentRunner()
	}
	var streamErr string
	runErr := runner.Run(ctx, AgentRequest{
		Prompt:    "Connection check for NSGo Game Editor. Reply with READY. Do not modify any files or run commands.",
		AgentKind: agentKind,
		Route:     AgentRouteLocal,
	}, func(ev AgentEvent) {
		if ev.Kind == AgentEventError && strings.TrimSpace(ev.Err) != "" && streamErr == "" {
			streamErr = strings.TrimSpace(ev.Err)
		}
	})
	if runErr != nil {
		return 0, runErr
	}
	if streamErr != "" {
		return 0, errors.New(streamErr)
	}
	return tools, nil
}

// RetryFirstRunConnect retries connecting the currently selected CLI agent.
func (e *Editor) RetryFirstRunConnect() {
	if e == nil {
		return
	}
	kind := e.FirstRunSelectedAgent()
	if kind == "" || kind == "apikey" {
		return
	}
	_ = e.SelectFirstRunAgent(kind)
}

// CopyFirstRunDiagnostics copies a safe diagnostic report (never containing any API key) to the clipboard
// and marks the Copy diagnostics button as Copied in place.
func (e *Editor) CopyFirstRunDiagnostics() string {
	if e == nil {
		return ""
	}
	e.firstRun.mu.Lock()
	kind := e.firstRun.selectedAgent
	errMsg := e.firstRun.connectErr
	e.firstRun.diagnosticsCopied = true
	e.firstRun.mu.Unlock()

	displayPath := e.StartNameStatus().DisplayPath
	if displayPath == "" {
		displayPath = "~/games/" + e.StartProjectName()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "NSGo Game Editor — First Run Connect Diagnostics\n")
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "Agent: %s (%s)\n", firstRunAgentDisplayName(kind), kind)
	fmt.Fprintf(&b, "Target: %s\n", displayPath)
	if errMsg != "" {
		fmt.Fprintf(&b, "Error: %s\n", errMsg)
	}
	text := b.String()
	e.writeClipboardText(text)
	e.EmitToastEvent(ToastEvent{
		Kind:    ToastEventCopied,
		Target:  "first-run-diagnostics",
		Message: "Copied diagnostics to clipboard",
	})
	e.notify()
	return text
}

// FirstRunDiagnosticsCopied reports whether Copy diagnostics has been clicked in the current Connect view.
func (e *Editor) FirstRunDiagnosticsCopied() bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.diagnosticsCopied
}

// FirstRunAPIKey returns the in-memory API key input (never written to disk).
func (e *Editor) FirstRunAPIKey() string {
	if e == nil {
		return ""
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.apiKey
}

// SetFirstRunAPIKey updates the in-memory API key and immediately validates it without holding firstRun.mu.
func (e *Editor) SetFirstRunAPIKey(key string) {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	e.firstRun.apiKey = key
	e.firstRun.apiKeyStoredInKeychain = false
	e.firstRun.apiKeySeq++
	seq := e.firstRun.apiKeySeq
	e.firstRun.mu.Unlock()

	st := e.connectStatusForAPIKey(key)

	e.firstRun.mu.Lock()
	if e.firstRun.apiKeySeq == seq && e.firstRun.selectedAgent == "apikey" {
		e.firstRun.connectStatus = st
	}
	e.firstRun.mu.Unlock()
	e.notify()
}

func (e *Editor) connectStatusForAPIKey(key string) FirstRunConnectStatus {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return FirstRunConnectKeyIdle
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	validator := e.FirstRunValidateAPIKey
	var res FirstRunAPIKeyStatus
	if validator != nil {
		res = validator(ctx, trimmed)
	} else {
		res = e.defaultValidateAPIKey(ctx, trimmed)
	}
	switch res {
	case FirstRunAPIKeyWorks:
		return FirstRunConnectKeyValid
	case FirstRunAPIKeyNoNetwork:
		return FirstRunConnectKeyNoNetwork
	case FirstRunAPIKeyNoQuota:
		return FirstRunConnectKeyNoQuota
	default:
		return FirstRunConnectKeyInvalid
	}
}

// defaultValidateAPIKey verifies key by sending one minimal request (max_tokens: 1 on the fast model)
// to the built-in agent's configured endpoint.
func (e *Editor) defaultValidateAPIKey(ctx context.Context, key string) FirstRunAPIKeyStatus {
	rawEndpoint := strings.TrimSpace(e.BuiltInAgentEndpoint)
	if rawEndpoint == "" {
		rawEndpoint = DefaultBuiltInAgentEndpoint
	}
	endpoint, err := validateAndResolveBuiltInEndpoint(rawEndpoint)
	if err != nil {
		return FirstRunAPIKeyNotAccepted
	}
	client := e.BuiltInHTTPClient
	if client == nil {
		client = &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: builtInCheckRedirect,
		}
	}
	payload := anthropicRequest{
		Model:     DefaultBuiltInFastModel,
		MaxTokens: 1,
		Messages: []anthropicMessage{
			{Role: "user", Content: "ping"},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return FirstRunAPIKeyNotAccepted
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return FirstRunAPIKeyNotAccepted
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := client.Do(req)
	if err != nil {
		return FirstRunAPIKeyNoNetwork
	}
	defer func() { _ = resp.Body.Close() }()
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return FirstRunAPIKeyWorks
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return FirstRunAPIKeyNotAccepted
	case resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusTooManyRequests:
		return FirstRunAPIKeyNoQuota
	case resp.StatusCode == http.StatusBadRequest:
		var env anthropicErrorEnvelope
		_ = json.Unmarshal(respBytes, &env)
		combined := strings.ToLower(env.Error.Type + " " + env.Error.Message)
		if strings.Contains(combined, "credit") || strings.Contains(combined, "quota") ||
			strings.Contains(combined, "billing") || strings.Contains(combined, "balance") {
			return FirstRunAPIKeyNoQuota
		}
		return FirstRunAPIKeyNotAccepted
	case resp.StatusCode >= 500 || resp.StatusCode == 529:
		return FirstRunAPIKeyNoNetwork
	default:
		return FirstRunAPIKeyNotAccepted
	}
}

// FirstRunConnectCanFinish reports whether the Done button on Connect in place is enabled.
func (e *Editor) FirstRunConnectCanFinish() bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.connectStatus == FirstRunConnectConnected || e.firstRun.connectStatus == FirstRunConnectKeyValid
}

// EditFirstRunFromConnect returns from Connect in place back to Step 1 on the Start Screen, keeping idea and name.
func (e *Editor) EditFirstRunFromConnect() {
	if e == nil {
		return
	}
	e.cancelFirstRunConnect()
	e.firstRun.mu.Lock()
	e.firstRun.stage = FirstRunStageNone
	e.firstRun.mu.Unlock()
	e.notify()
}

// DeferFirstRunConnect handles the Later button on Connect in place, returning to Start while keeping idea and name.
func (e *Editor) DeferFirstRunConnect() {
	e.EditFirstRunFromConnect()
}

// FinishFirstRunConnect completes Connect in place once an agent has answered or an API key is verified,
// syncs the #1200 Agent mode (and persists a validated API key via Editor.Keychain / core.Secrets when available),
// and immediately opens Step 3 (The first plan) without creating any project folder on disk.
func (e *Editor) FinishFirstRunConnect() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if !e.FirstRunConnectCanFinish() {
		return errors.New("editor: connect an agent or provide a valid API key first")
	}
	e.firstRun.mu.Lock()
	kind := e.firstRun.selectedAgent
	tools := e.firstRun.agentTools
	apiKey := strings.TrimSpace(e.firstRun.apiKey)
	e.firstRun.mu.Unlock()

	if tools <= 0 {
		tools = e.firstRunLiveMCPToolCount()
	}

	e.firstRun.mu.Lock()
	e.firstRun.agentConnected = true
	e.firstRun.agentKind = kind
	e.firstRun.agentName = firstRunAgentDisplayName(kind)
	e.firstRun.agentTools = tools
	e.firstRun.mu.Unlock()

	if kind == "apikey" {
		e.agent.mode = AgentModeAPIKey
		if e.agent.status == AgentStatusNotConnected || e.agent.status == AgentStatusDisconnected {
			e.agent.status = AgentStatusConnected
		}
		stored := false
		if apiKey != "" {
			if e.Keychain != nil {
				if err := e.Keychain.Set(FirstRunAPIKeyKeychainService, FirstRunAPIKeyKeychainAccount, apiKey); err == nil {
					stored = true
				}
			}
			if e.Keychain != nil || e.AgentSecretStore != nil || e.agentSecretsStore() != nil {
				if err := e.StoreAgentAPIKey(DefaultAgentAPIKeyAccount, apiKey); err == nil {
					stored = true
				}
			}
		}
		e.firstRun.mu.Lock()
		e.firstRun.apiKeyStoredInKeychain = stored
		e.firstRun.mu.Unlock()
	} else {
		e.agent.mode = AgentModeLocal
		if e.agent.status == AgentStatusNotConnected || e.agent.status == AgentStatusDisconnected {
			e.agent.status = AgentStatusConnected
		}
	}

	return e.OpenFirstRunPlan()
}

// IsFirstRunPlanning reports whether Start with AI is currently asking the connected agent to propose a first-run plan.
// Used by editormcp's plan_propose tool to capture the plan without blocking on the working-editor plan dialog.
func (e *Editor) IsFirstRunPlanning() bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.planning
}

// CaptureFirstRunAgentPlan stores an AgentPlan proposed via plan_propose during Start with AI planning
// and clears the working-editor active plan slot so it does not leak into the workspace right rail.
func (e *Editor) CaptureFirstRunAgentPlan(p *AgentPlan) {
	if e == nil || p == nil {
		return
	}
	cp := p.Clone()
	e.firstRun.mu.Lock()
	if e.firstRun.planning {
		e.firstRun.proposedPlan = cp
	}
	e.firstRun.mu.Unlock()

	e.plan.mu.Lock()
	if e.plan.active != nil && e.plan.active.ID == cp.ID {
		e.plan.active = nil
		e.plan.waiters = nil
		e.plan.decided = false
		e.plan.seq++
		e.plan.ghostRev++
	}
	e.plan.mu.Unlock()
}

func (e *Editor) takeCapturedFirstRunAgentPlan() *AgentPlan {
	e.firstRun.mu.Lock()
	p := e.firstRun.proposedPlan
	e.firstRun.proposedPlan = nil
	e.firstRun.mu.Unlock()
	return p
}

// FirstRunPlanLoading reports whether Start with AI is waiting for the connected agent to propose a plan.
func (e *Editor) FirstRunPlanLoading() bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.planLoading
}

// FirstRunPlanError returns the error message from the last failed first-run plan attempt, if any.
func (e *Editor) FirstRunPlanError() string {
	if e == nil {
		return ""
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.planErr
}

// WaitForFirstRunPlan blocks until any in-flight first-run planning request completes.
func (e *Editor) WaitForFirstRunPlan() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	done := e.firstRun.planDone
	e.firstRun.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (e *Editor) cancelFirstRunPlan() {
	e.firstRun.mu.Lock()
	cancel := e.firstRun.planCancel
	e.firstRun.planCancel = nil
	e.firstRun.planSeq++
	e.firstRun.planLoading = false
	e.firstRun.planning = false
	e.firstRun.proposedPlan = nil
	e.firstRun.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (e *Editor) hasFirstRunScope() bool {
	e.firstRun.mu.Lock()
	scope := e.firstRun.scope
	e.firstRun.mu.Unlock()
	if scope != nil {
		return true
	}
	e.startScreen.mu.Lock()
	scope = e.startScreen.scope
	e.startScreen.mu.Unlock()
	return scope != nil
}

// resolveFirstRunRunner returns the active AgentRunner, route, and external CLI kind for Start with AI
// planning and building. It fails when no agent is connected.
func (e *Editor) resolveFirstRunRunner() (AgentRunner, AgentModelRoute, ExternalAgentKind, error) {
	if e == nil {
		return nil, AgentRouteNone, ExternalAgentAuto, errors.New("editor: nil editor")
	}
	kind, _, _ := e.FirstRunConnectedAgent()
	if kind == "apikey" || (kind == "" && e.AgentMode() == AgentModeAPIKey && (e.HasAgentAPIKey(DefaultAgentAPIKeyAccount) || strings.TrimSpace(e.FirstRunAPIKey()) != "")) {
		r, err := e.newBuiltInAgentRunner()
		if err != nil {
			return nil, AgentRouteNone, ExternalAgentAuto, err
		}
		return r, AgentRouteBuiltInDefault, ExternalAgentAuto, nil
	}
	if kind != "" || e.AgentRunner != nil {
		runner := e.AgentRunner
		if runner == nil {
			runner = defaultCLIAgentRunner()
		}
		extKind := ExternalAgentAuto
		switch kind {
		case "claude":
			extKind = ExternalAgentClaudeCode
		case "codex":
			extKind = ExternalAgentCodex
		}
		return runner, AgentRouteLocal, extKind, nil
	}
	return nil, AgentRouteNone, ExternalAgentAuto, errors.New("editor: connect an agent before planning or building")
}

// RetryFirstRunPlan retries asking the connected agent for the first plan after a planning error.
func (e *Editor) RetryFirstRunPlan() error {
	return e.OpenFirstRunPlan()
}

// OpenFirstRunPlan asks the connected agent (or Editor.FirstRunPlanner when set) for the first plan
// for the current Start Screen idea and name, and transitions to Step 3 (The first plan).
// No project folder is created on disk until Approve.
func (e *Editor) OpenFirstRunPlan() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.ensureStartScreenInit()

	name := e.StartProjectName()
	parentDir := e.StartProjectParentDir()
	module := e.StartProjectModule()
	idea := strings.TrimSpace(e.StartProjectIdea())
	v := ValidateNewProjectInputs(name, parentDir, module)
	if !v.OK {
		return errors.New(v.Message)
	}

	req := FirstRunPlanRequest{
		Idea:        idea,
		ProjectName: name,
		ParentDir:   parentDir,
		TargetDir:   v.TargetDir,
		DisplayPath: v.DisplayPath,
		Module:      strings.TrimSpace(module),
		Templates:   e.ProjectTemplates(),
		Selected:    e.SelectedProjectTemplate(),
	}

	finalizePlan := func(plan *FirstRunPlan) (*FirstRunPlan, error) {
		if plan == nil {
			return nil, errors.New("editor: agent did not return a plan")
		}
		cp := cloneFirstRunPlan(plan)
		if cp.ProjectName == "" {
			cp.ProjectName = name
		}
		if cp.TargetDir == "" {
			cp.TargetDir = v.TargetDir
		} else if filepath.Clean(cp.TargetDir) != filepath.Clean(v.TargetDir) {
			return nil, fmt.Errorf("editor: planner target directory %q does not match validated path %q", cp.TargetDir, v.TargetDir)
		}
		if cp.DisplayPath == "" {
			cp.DisplayPath = v.DisplayPath
		}
		if cp.Module == "" {
			cp.Module = strings.TrimSpace(module)
		}
		if cp.Idea == "" {
			cp.Idea = idea
		}
		if cp.Template == "" {
			cp.Template, cp.TemplateTitle = pickTemplateForIdea(req.Idea, req.Templates, req.Selected)
		} else if cp.TemplateTitle == "" {
			cp.TemplateTitle = templateTitleForName(cp.Template, req.Templates)
		}
		if cp.SceneName == "" {
			cp.SceneName = "level_01"
		}
		if err := validateFirstRunSceneName(cp.SceneName); err != nil {
			return nil, err
		}
		if len(cp.Steps) == 0 {
			return nil, errors.New("editor: agent plan has no steps")
		}
		return cp, nil
	}

	e.cancelFirstRunPlan()

	if planner := e.FirstRunPlanner; planner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		plan, err := planner(ctx, req)
		cancel()
		if err != nil {
			return err
		}
		cp, err := finalizePlan(plan)
		if err != nil {
			return err
		}
		e.firstRun.mu.Lock()
		e.firstRun.plan = cp
		e.firstRun.planLoading = false
		e.firstRun.planErr = ""
		e.firstRun.stage = FirstRunStagePlan
		e.firstRun.mu.Unlock()

		e.startScreen.mu.Lock()
		e.startScreen.open = false
		e.startScreen.mu.Unlock()
		e.notify()
		return nil
	}

	// Default path: verify an agent is connected before entering the plan stage.
	if _, _, _, err := e.resolveFirstRunRunner(); err != nil {
		return err
	}

	// When a UI scope is mounted, run the agent off the UI goroutine so editormcp tool calls
	// (plan_propose) can safely post onto the UI goroutine without deadlocking.
	if e.hasFirstRunScope() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		done := make(chan struct{})
		e.firstRun.mu.Lock()
		e.firstRun.planSeq++
		seq := e.firstRun.planSeq
		e.firstRun.planCancel = cancel
		e.firstRun.planDone = done
		e.firstRun.plan = nil
		e.firstRun.planLoading = true
		e.firstRun.planErr = ""
		e.firstRun.stage = FirstRunStagePlan
		e.firstRun.mu.Unlock()

		e.startScreen.mu.Lock()
		e.startScreen.open = false
		e.startScreen.mu.Unlock()
		e.notify()

		go func() {
			defer cancel()
			plan, err := e.defaultFirstRunPlan(ctx, req)
			var cp *FirstRunPlan
			if err == nil {
				cp, err = finalizePlan(plan)
			}
			e.firstRunPostOrRun(func() {
				defer close(done)
				e.firstRun.mu.Lock()
				if e.firstRun.planSeq != seq || e.firstRun.stage != FirstRunStagePlan {
					e.firstRun.mu.Unlock()
					return
				}
				e.firstRun.planCancel = nil
				e.firstRun.planLoading = false
				if err != nil {
					if !errors.Is(err, context.Canceled) {
						e.firstRun.planErr = err.Error()
					}
					e.firstRun.mu.Unlock()
					e.notify()
					return
				}
				e.firstRun.plan = cp
				e.firstRun.planErr = ""
				e.firstRun.mu.Unlock()
				e.notify()
			})
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	plan, err := e.defaultFirstRunPlan(ctx, req)
	if err != nil {
		return err
	}
	cp, err := finalizePlan(plan)
	if err != nil {
		return err
	}
	e.firstRun.mu.Lock()
	e.firstRun.plan = cp
	e.firstRun.planLoading = false
	e.firstRun.planErr = ""
	e.firstRun.stage = FirstRunStagePlan
	e.firstRun.mu.Unlock()

	e.startScreen.mu.Lock()
	e.startScreen.open = false
	e.startScreen.mu.Unlock()
	e.notify()
	return nil
}

// defaultFirstRunPlan asks the connected agent to propose a first-run plan using the editor's
// plan_propose MCP tool (or a structured JSON plan in its reply when MCP tools are not invoked).
func (e *Editor) defaultFirstRunPlan(ctx context.Context, req FirstRunPlanRequest) (*FirstRunPlan, error) {
	runner, route, extKind, err := e.resolveFirstRunRunner()
	if err != nil {
		return nil, err
	}

	e.firstRun.mu.Lock()
	e.firstRun.planning = true
	e.firstRun.proposedPlan = nil
	e.firstRun.mu.Unlock()
	defer func() {
		e.firstRun.mu.Lock()
		e.firstRun.planning = false
		e.firstRun.mu.Unlock()
	}()

	prompt := buildFirstRunPlanPrompt(req)
	var replyBuf strings.Builder
	var streamErr string
	runErr := runner.Run(ctx, AgentRequest{
		Prompt:    prompt,
		AgentKind: extKind,
		Route:     route,
		Task:      AgentTaskSceneEdit,
	}, func(ev AgentEvent) {
		switch ev.Kind {
		case AgentEventMessage:
			if strings.TrimSpace(ev.Text) != "" {
				if replyBuf.Len() > 0 {
					replyBuf.WriteByte('\n')
				}
				replyBuf.WriteString(ev.Text)
			}
		case AgentEventError:
			if strings.TrimSpace(ev.Err) != "" && streamErr == "" {
				streamErr = strings.TrimSpace(ev.Err)
			}
		}
	})
	if runErr != nil {
		return nil, runErr
	}
	if streamErr != "" {
		return nil, errors.New(streamErr)
	}

	if ap := e.takeCapturedFirstRunAgentPlan(); ap != nil {
		return firstRunPlanFromAgentPlan(ap, req), nil
	}
	plan, parseErr := parseFirstRunPlanFromText(replyBuf.String(), req)
	if parseErr != nil {
		return nil, parseErr
	}
	return plan, nil
}

func buildFirstRunPlanPrompt(req FirstRunPlanRequest) string {
	var b strings.Builder
	b.WriteString("You are planning a brand-new NSGo game project before any project folder is created on disk. Do not create or modify any files yet.\n")
	fmt.Fprintf(&b, "Project name: %s\n", req.ProjectName)
	fmt.Fprintf(&b, "Target path: %s\n", req.DisplayPath)
	if strings.TrimSpace(req.Idea) != "" {
		fmt.Fprintf(&b, "Author's idea: %s\n", strings.TrimSpace(req.Idea))
	}
	if len(req.Templates) > 0 {
		b.WriteString("Available templates:\n")
		for _, t := range req.Templates {
			fmt.Fprintf(&b, "- %s (%s): %s\n", t.Name, t.DisplayTitle(), t.Summary)
		}
	}
	b.WriteString("\nPropose a concise first-run plan tailored specifically to the author's idea.\n")
	b.WriteString("If the plan_propose MCP tool is available, call plan_propose with wait=false, concrete steps, optional clarifying question, and optional viewport ghosts.\n")
	b.WriteString("Otherwise, reply with a JSON object: {\"template\": \"...\", \"sceneName\": \"...\", \"estimatedFiles\": N, \"estimatedObjects\": N, \"estimatedMinutes\": N, \"question\": {\"prompt\": \"...\", \"options\": [{\"id\": \"...\", \"label\": \"...\", \"note\": \"...\", \"suggested\": true}]}, \"steps\": [{\"number\": 1, \"title\": \"...\", \"filesLabel\": \"...\", \"permission\": \"...\"}], \"ghosts\": [{\"id\": \"...\", \"step\": 2, \"label\": \"...\", \"x\": 40, \"y\": 120, \"w\": 80, \"h\": 40}]}.\n")
	return b.String()
}

func firstRunPlanFromAgentPlan(ap *AgentPlan, req FirstRunPlanRequest) *FirstRunPlan {
	tmplName, tmplTitle := pickTemplateForIdea(req.Idea, req.Templates, req.Selected)
	sceneName := "level_01"
	for _, f := range ap.Files {
		if sn := extractSceneNameFromPath(f.Path); sn != "" {
			sceneName = sn
			break
		}
	}
	if sceneName == "level_01" {
		for _, st := range ap.Steps {
			for _, f := range st.Files {
				if sn := extractSceneNameFromPath(f); sn != "" {
					sceneName = sn
					break
				}
			}
			if sceneName != "level_01" {
				break
			}
		}
	}

	steps := make([]FirstRunPlanStep, 0, len(ap.Steps))
	var ghosts []FirstRunGhost
	estObjects := 0
	for i, st := range ap.Steps {
		if st.Removed {
			continue
		}
		num := st.Number
		if num <= 0 {
			num = i + 1
		}
		filesLabel := strings.TrimSpace(st.FilesLabel)
		if filesLabel == "" && len(st.Files) > 0 {
			filesLabel = strings.Join(st.Files, ", ")
		}
		if filesLabel == "" && st.Objects > 0 {
			filesLabel = fmt.Sprintf("%d objects", st.Objects)
		}
		steps = append(steps, FirstRunPlanStep{
			Number:     num,
			Title:      st.Title,
			FilesLabel: filesLabel,
			Permission: st.Permission,
		})
		estObjects += max(st.Objects, 0)
		for gi, g := range st.Ghosts {
			gid := g.ID
			if gid == "" {
				gid = fmt.Sprintf("ghost-%d-%d", num, gi+1)
			}
			stepNum := g.Step
			if stepNum <= 0 {
				stepNum = num
			}
			ghosts = append(ghosts, FirstRunGhost{
				ID:    gid,
				Step:  stepNum,
				Label: g.Label,
				Kind:  "planned",
				X:     g.X,
				Y:     g.Y,
				W:     g.Width,
				H:     g.Height,
			})
		}
	}

	var question *FirstRunPlanQuestion
	if len(ap.Questions) > 0 && len(ap.Questions[0].Options) > 0 {
		q := ap.Questions[0]
		opts := make([]FirstRunPlanOption, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, FirstRunPlanOption{
				ID:                 o.ID,
				Label:              o.Label,
				Note:               o.Note,
				Suggested:          o.Suggested,
				StepTitleOverrides: maps.Clone(o.StepTitleOverrides),
			})
		}
		question = &FirstRunPlanQuestion{
			Prompt:  q.Prompt,
			Options: opts,
		}
	}

	estFiles := len(ap.Files)
	if estFiles == 0 {
		seen := make(map[string]struct{})
		for _, st := range ap.Steps {
			for _, f := range st.Files {
				if strings.TrimSpace(f) != "" {
					seen[f] = struct{}{}
				}
			}
		}
		estFiles = len(seen)
	}
	estMinutes := parseDurationMinutes(ap.Estimate.Duration)

	return &FirstRunPlan{
		ProjectName:      req.ProjectName,
		TargetDir:        req.TargetDir,
		DisplayPath:      req.DisplayPath,
		Module:           req.Module,
		Idea:             req.Idea,
		Template:         tmplName,
		TemplateTitle:    tmplTitle,
		SceneName:        sceneName,
		EstimatedFiles:   estFiles,
		EstimatedObjects: estObjects,
		EstimatedMinutes: estMinutes,
		Question:         question,
		Steps:            steps,
		Ghosts:           ghosts,
	}
}

type firstRunJSONPlan struct {
	Template         string `json:"template"`
	TemplateTitle    string `json:"templateTitle"`
	SceneName        string `json:"sceneName"`
	EstimatedFiles   int    `json:"estimatedFiles"`
	EstimatedObjects int    `json:"estimatedObjects"`
	EstimatedMinutes int    `json:"estimatedMinutes"`
	Question         *struct {
		Prompt  string `json:"prompt"`
		Options []struct {
			ID                 string         `json:"id"`
			Label              string         `json:"label"`
			Note               string         `json:"note"`
			Suggested          bool           `json:"suggested"`
			Verb               string         `json:"verb"`
			StepTitleOverrides map[int]string `json:"stepTitleOverrides"`
		} `json:"options"`
	} `json:"question"`
	Steps []struct {
		Number     int    `json:"number"`
		Title      string `json:"title"`
		FilesLabel string `json:"filesLabel"`
		Permission string `json:"permission"`
	} `json:"steps"`
	Ghosts []struct {
		ID     string  `json:"id"`
		Step   int     `json:"step"`
		Label  string  `json:"label"`
		Kind   string  `json:"kind"`
		X      float32 `json:"x"`
		Y      float32 `json:"y"`
		W      float32 `json:"w"`
		Width  float32 `json:"width"`
		H      float32 `json:"h"`
		Height float32 `json:"height"`
	} `json:"ghosts"`
}

func parseFirstRunPlanFromText(raw string, req FirstRunPlanRequest) (*FirstRunPlan, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("editor: agent returned an empty plan response")
	}

	if jsonBlob := extractJSONObject(trimmed); jsonBlob != "" {
		var jp firstRunJSONPlan
		if err := json.Unmarshal([]byte(jsonBlob), &jp); err == nil && len(jp.Steps) > 0 {
			tmplName := strings.TrimSpace(jp.Template)
			tmplTitle := strings.TrimSpace(jp.TemplateTitle)
			if tmplName == "" {
				tmplName, tmplTitle = pickTemplateForIdea(req.Idea, req.Templates, req.Selected)
			} else if tmplTitle == "" {
				tmplTitle = templateTitleForName(tmplName, req.Templates)
			}
			sceneName := strings.TrimSpace(jp.SceneName)
			if sceneName == "" {
				sceneName = "level_01"
			}
			steps := make([]FirstRunPlanStep, 0, len(jp.Steps))
			for i, st := range jp.Steps {
				num := st.Number
				if num <= 0 {
					num = i + 1
				}
				steps = append(steps, FirstRunPlanStep{
					Number:     num,
					Title:      strings.TrimSpace(st.Title),
					FilesLabel: strings.TrimSpace(st.FilesLabel),
					Permission: strings.TrimSpace(st.Permission),
				})
			}
			var q *FirstRunPlanQuestion
			if jp.Question != nil && strings.TrimSpace(jp.Question.Prompt) != "" && len(jp.Question.Options) > 0 {
				opts := make([]FirstRunPlanOption, 0, len(jp.Question.Options))
				for _, o := range jp.Question.Options {
					opts = append(opts, FirstRunPlanOption{
						ID:                 o.ID,
						Label:              o.Label,
						Note:               o.Note,
						Suggested:          o.Suggested,
						Verb:               o.Verb,
						StepTitleOverrides: maps.Clone(o.StepTitleOverrides),
					})
				}
				q = &FirstRunPlanQuestion{
					Prompt:  strings.TrimSpace(jp.Question.Prompt),
					Options: opts,
				}
			}
			var ghosts []FirstRunGhost
			for i, g := range jp.Ghosts {
				gid := strings.TrimSpace(g.ID)
				if gid == "" {
					gid = fmt.Sprintf("ghost-%d", i+1)
				}
				w := g.W
				if w <= 0 {
					w = g.Width
				}
				h := g.H
				if h <= 0 {
					h = g.Height
				}
				kind := strings.TrimSpace(g.Kind)
				if kind == "" {
					kind = "planned"
				}
				ghosts = append(ghosts, FirstRunGhost{
					ID:    gid,
					Step:  max(g.Step, 1),
					Label: g.Label,
					Kind:  kind,
					X:     g.X,
					Y:     g.Y,
					W:     w,
					H:     h,
				})
			}
			return &FirstRunPlan{
				ProjectName:      req.ProjectName,
				TargetDir:        req.TargetDir,
				DisplayPath:      req.DisplayPath,
				Module:           req.Module,
				Idea:             req.Idea,
				Template:         tmplName,
				TemplateTitle:    tmplTitle,
				SceneName:        sceneName,
				EstimatedFiles:   max(jp.EstimatedFiles, 0),
				EstimatedObjects: max(jp.EstimatedObjects, 0),
				EstimatedMinutes: max(jp.EstimatedMinutes, 0),
				Question:         q,
				Steps:            steps,
				Ghosts:           ghosts,
			}, nil
		}
	}

	// Plain-text fallback: extract numbered or bulleted lines directly from the agent's own words.
	var steps []FirstRunPlanStep
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		cleaned := stripListMarker(line)
		if cleaned == "" {
			continue
		}
		steps = append(steps, FirstRunPlanStep{
			Number: len(steps) + 1,
			Title:  cleaned,
		})
	}
	if len(steps) == 0 {
		return nil, errors.New("editor: could not parse a plan from the agent's response")
	}
	tmplName, tmplTitle := pickTemplateForIdea(req.Idea, req.Templates, req.Selected)
	return &FirstRunPlan{
		ProjectName:   req.ProjectName,
		TargetDir:     req.TargetDir,
		DisplayPath:   req.DisplayPath,
		Module:        req.Module,
		Idea:          req.Idea,
		Template:      tmplName,
		TemplateTitle: tmplTitle,
		SceneName:     "level_01",
		Steps:         steps,
	}, nil
}

func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func stripListMarker(line string) string {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return strings.TrimSpace(line[2:])
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i > 0 && i < len(line) && (line[i] == '.' || line[i] == ')') {
		return strings.TrimSpace(line[i+1:])
	}
	return line
}

func extractSceneNameFromPath(p string) string {
	norm := strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	base := filepath.Base(norm)
	for _, suf := range []string{".scene.yaml", ".scene.yml"} {
		if strings.HasSuffix(base, suf) {
			name := strings.TrimSuffix(base, suf)
			if validateFirstRunSceneName(name) == nil {
				return name
			}
		}
	}
	return ""
}

func parseDurationMinutes(raw string) int {
	for _, field := range strings.Fields(raw) {
		field = strings.Trim(field, "~.,()")
		if n, err := strconv.Atoi(field); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func templateTitleForName(name string, tmpls []ProjectTemplate) string {
	for _, t := range tmpls {
		if strings.EqualFold(t.Name, name) {
			return t.DisplayTitle()
		}
	}
	if name == "" {
		return "Starter"
	}
	return name
}

// DefaultFirstRunPlan builds a minimal, neutral template-derived skeleton plan from req without
// any hardcoded game content. OpenFirstRunPlan asks the connected agent instead of calling this.
func DefaultFirstRunPlan(req FirstRunPlanRequest) *FirstRunPlan {
	tmplName, tmplTitle := pickTemplateForIdea(req.Idea, req.Templates, req.Selected)
	buildTitle := "Build level_01"
	if idea := strings.TrimSpace(req.Idea); idea != "" {
		buildTitle = "Build level_01 for: " + truncateRunes(idea, 80)
	}
	steps := []FirstRunPlanStep{
		{Number: 1, Title: "Create project from the " + tmplTitle + " template"},
		{Number: 2, Title: buildTitle, FilesLabel: "scenes/level_01.scene.yaml"},
		{Number: 3, Title: "Run game check", FilesLabel: "no files"},
	}
	return &FirstRunPlan{
		ProjectName:   req.ProjectName,
		TargetDir:     req.TargetDir,
		DisplayPath:   req.DisplayPath,
		Module:        req.Module,
		Idea:          req.Idea,
		Template:      tmplName,
		TemplateTitle: tmplTitle,
		SceneName:     "level_01",
		Steps:         steps,
	}
}

func pickTemplateForIdea(idea string, tmpls []ProjectTemplate, selected string) (name, title string) {
	lower := strings.ToLower(idea)
	keywords := []struct {
		template string
		title    string
		words    []string
	}{
		{"platformer2d", "2D platformer", []string{"platformer", "dungeon", "jump", "knight", "slime", "coin", "lever"}},
		{"rpg2d", "2D RPG", []string{"rpg", "town", "quest", "dialogue", "farming", "mushroom", "cave", "trade"}},
		{"racing3d", "3D racer", []string{"racing", "racer", "kart", "car", "lap", "track"}},
		{"fps3d", "First-person shooter", []string{"fps", "first-person", "shooter"}},
		{"action3d", "3D action adventure", []string{"3d", "courtyard", "orbit"}},
		{"puzzle2d", "Grid puzzle", []string{"puzzle", "sokoban", "grid"}},
		{"cards2d", "Card game", []string{"card", "deck", "hand"}},
		{"roguelike2d", "Roguelike", []string{"roguelike", "loot", "procgen"}},
		{"towerdefense2d", "Tower defense", []string{"tower", "wave", "defense"}},
		{"visualnovel", "Visual novel", []string{"novel", "story", "choice"}},
		{"rhythm", "Rhythm game", []string{"rhythm", "beat", "music"}},
	}
	findRegistered := func(want string) (string, string, bool) {
		for _, t := range tmpls {
			if strings.EqualFold(t.Name, want) {
				return t.Name, t.DisplayTitle(), true
			}
		}
		return "", "", false
	}
	for _, kw := range keywords {
		for _, w := range kw.words {
			if strings.Contains(lower, w) {
				if n, tit, ok := findRegistered(kw.template); ok {
					return n, tit
				}
				return kw.template, kw.title
			}
		}
	}
	if selected != "" {
		if n, tit, ok := findRegistered(selected); ok {
			return n, tit
		}
	}
	if len(tmpls) > 0 {
		return tmpls[0].Name, tmpls[0].DisplayTitle()
	}
	return "platformer2d", "2D platformer"
}

func cloneFirstRunPlan(p *FirstRunPlan) *FirstRunPlan {
	if p == nil {
		return nil
	}
	cp := *p
	if p.Question != nil {
		q := *p.Question
		q.Options = make([]FirstRunPlanOption, len(p.Question.Options))
		for i, opt := range p.Question.Options {
			oc := opt
			if opt.StepTitleOverrides != nil {
				oc.StepTitleOverrides = maps.Clone(opt.StepTitleOverrides)
			}
			q.Options[i] = oc
		}
		cp.Question = &q
	}
	cp.Steps = slices.Clone(p.Steps)
	cp.TemplateShapes = slices.Clone(p.TemplateShapes)
	cp.Ghosts = slices.Clone(p.Ghosts)
	return &cp
}

// FirstRunPlan returns a copy of the active first plan, or nil when none is active.
func (e *Editor) FirstRunPlan() *FirstRunPlan {
	if e == nil {
		return nil
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return cloneFirstRunPlan(e.firstRun.plan)
}

// SelectFirstRunPlanAnswer selects an answer option for the first plan's clarifying question,
// updating the affected step description live.
func (e *Editor) SelectFirstRunPlanAnswer(optionID string) bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	plan := e.firstRun.plan
	if plan == nil || plan.Approved || plan.Question == nil {
		e.firstRun.mu.Unlock()
		return false
	}
	var picked *FirstRunPlanOption
	for i := range plan.Question.Options {
		if plan.Question.Options[i].ID == optionID {
			o := plan.Question.Options[i]
			picked = &o
			break
		}
	}
	if picked == nil {
		e.firstRun.mu.Unlock()
		return false
	}
	plan.SelectedAnswer = picked.ID
	if len(picked.StepTitleOverrides) > 0 {
		for i := range plan.Steps {
			num := plan.Steps[i].Number
			if num <= 0 {
				num = i + 1
			}
			if title, ok := picked.StepTitleOverrides[num]; ok && strings.TrimSpace(title) != "" {
				plan.Steps[i].Title = title
			}
		}
	} else if picked.Verb != "" && len(plan.Steps) >= 2 {
		for _, opt := range plan.Question.Options {
			if opt.Verb != "" && strings.Contains(plan.Steps[1].Title, " to "+opt.Verb+",") {
				plan.Steps[1].Title = strings.Replace(plan.Steps[1].Title, " to "+opt.Verb+",", " to "+picked.Verb+",", 1)
				break
			}
		}
	}
	e.firstRun.mu.Unlock()
	e.notify()
	return true
}

// FirstRunPlanCanApprove reports whether Approve is currently enabled on the first plan.
func (e *Editor) FirstRunPlanCanApprove() bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	if e.firstRun.stage != FirstRunStagePlan || e.firstRun.planLoading || e.firstRun.plan == nil || e.firstRun.plan.Approved {
		return false
	}
	if e.firstRun.plan.Question != nil && len(e.firstRun.plan.Question.Options) > 0 && e.firstRun.plan.SelectedAnswer == "" {
		return false
	}
	return true
}

// BackToStartFromPlan returns from Step 3 (The first plan) back to the Start Screen, preserving idea and name.
func (e *Editor) BackToStartFromPlan() {
	if e == nil {
		return
	}
	e.cancelFirstRunPlan()
	e.firstRun.mu.Lock()
	e.firstRun.stage = FirstRunStageNone
	e.firstRun.plan = nil
	e.firstRun.planErr = ""
	e.firstRun.mu.Unlock()
	e.SetStartScreenOpen(true)
}

// JustDoItFirstRunPlan picks the suggested answer (when a clarifying question is unanswered)
// and immediately approves and starts building.
func (e *Editor) JustDoItFirstRunPlan() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.firstRun.mu.Lock()
	plan := e.firstRun.plan
	var suggested string
	if plan != nil && plan.Question != nil && plan.SelectedAnswer == "" && len(plan.Question.Options) > 0 {
		suggested = plan.Question.Options[0].ID
		for _, o := range plan.Question.Options {
			if o.Suggested {
				suggested = o.ID
				break
			}
		}
	}
	e.firstRun.mu.Unlock()
	if suggested != "" {
		e.SelectFirstRunPlanAnswer(suggested)
	}
	return e.ApproveFirstRunPlan()
}

// ApproveFirstRunPlan approves the first plan, creates the brand-new target project folder on disk
// (recording folder ownership exclusively for this flow), and transitions into Step 4 (Building).
func (e *Editor) ApproveFirstRunPlan() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if !e.FirstRunPlanCanApprove() {
		return errors.New("editor: answer the plan question before approving")
	}

	e.firstRun.mu.Lock()
	plan := cloneFirstRunPlan(e.firstRun.plan)
	e.firstRun.mu.Unlock()
	if plan == nil {
		return errors.New("editor: no first-run plan active")
	}

	if err := validateFirstRunSceneName(plan.SceneName); err != nil {
		return err
	}
	targetDir := filepath.Clean(plan.TargetDir)
	if targetDir == "" || targetDir == "." {
		return errors.New("editor: empty project target directory")
	}
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("editor: resolve project target directory: %w", err)
	}
	if !isSafeOwnedProjectDir(absTarget) {
		return fmt.Errorf("editor: refusing unsafe project target directory %s", shortenHomePath(absTarget))
	}
	if err := os.MkdirAll(filepath.Dir(absTarget), 0o755); err != nil {
		return fmt.Errorf("editor: create parent folder: %w", err)
	}
	// Safety invariant: create the target directory atomically with os.Mkdir so a pre-existing
	// directory or symlink can never be claimed as createdByFlow (eliminates TOCTOU).
	if err := os.Mkdir(absTarget, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("editor: folder %s already exists", shortenHomePath(absTarget))
		}
		return fmt.Errorf("editor: create project folder: %w", err)
	}
	absDir := absTarget
	if realDir, err := filepath.EvalSymlinks(absTarget); err == nil {
		absDir = realDir
	}
	if !isSafeOwnedProjectDir(absDir) {
		_ = os.Remove(absTarget)
		return fmt.Errorf("editor: refusing unsafe project target directory %s", shortenHomePath(absDir))
	}

	now := e.now()
	ctx, cancel := context.WithCancel(context.Background())
	run := newFirstRunBuildRun(cancel)

	initialStages := [5]FirstRunBuildStageStatus{
		FirstRunBuildStageDone,
		FirstRunBuildStageActive,
		FirstRunBuildStageTodo,
		FirstRunBuildStageTodo,
		FirstRunBuildStageTodo,
	}
	stage1Sub := "scaffolding project files"
	if plan.EstimatedFiles > 1 {
		stage1Sub = fmt.Sprintf("%d files from %s", plan.EstimatedFiles-1, plan.TemplateTitle)
	}
	stage2Sub := "waiting for agent"
	if plan.EstimatedObjects > 0 {
		stage2Sub = fmt.Sprintf("0 of %d objects", plan.EstimatedObjects)
	}
	initialLog := []FirstRunBuildLogEntry{
		{StageIndex: 0, Status: FirstRunBuildStageDone, Title: "Picked " + plan.TemplateTitle, Subtitle: "template for your game"},
		{StageIndex: 1, Status: FirstRunBuildStageActive, Title: "Creating " + plan.DisplayPath, Subtitle: stage1Sub},
		{StageIndex: 2, Status: FirstRunBuildStageTodo, Title: "Build " + plan.SceneName, Subtitle: stage2Sub, TotalCount: plan.EstimatedObjects},
		{StageIndex: 3, Status: FirstRunBuildStageTodo, Title: "check", Subtitle: "runs by itself"},
		{StageIndex: 4, Status: FirstRunBuildStageTodo, Title: "Playtest", Subtitle: "runs after check"},
	}

	e.firstRun.mu.Lock()
	if e.firstRun.plan != nil {
		e.firstRun.plan.Approved = true
	}
	e.firstRun.stage = FirstRunStageBuilding
	e.firstRun.buildOutcome = FirstRunBuildRunning
	e.firstRun.buildStages = initialStages
	e.firstRun.buildLog = initialLog
	e.firstRun.buildObjectsDone = 0
	e.firstRun.buildObjectsTotal = plan.EstimatedObjects
	e.firstRun.buildPlacingLabel = ""
	e.firstRun.buildShapes = slices.Clone(plan.TemplateShapes)
	e.firstRun.buildStartedAt = now
	e.firstRun.buildCheckFailure = nil
	e.firstRun.buildPendingRes = nil
	e.firstRun.buildRun = run
	e.firstRun.createdByFlow = true
	e.firstRun.createdDir = absDir
	e.firstRun.createdAt = now
	e.firstRun.mu.Unlock()
	e.notify()

	builder := e.FirstRunBuilder
	if builder == nil {
		builder = e.defaultFirstRunBuilder
	}

	req := FirstRunBuildRequest{
		Plan:         *plan,
		ProjectDir:   absDir,
		Answer:       plan.SelectedAnswer,
		WaitIfPaused: run.waitIfPaused,
	}

	emit := func(ev FirstRunBuildEvent) {
		e.firstRunPostOrRun(func() {
			e.applyFirstRunBuildEvent(run, ev)
		})
	}

	go func() {
		res, buildErr := builder(ctx, req, emit)
		if ctx.Err() != nil {
			run.finish()
			return
		}
		e.firstRunPostOrRun(func() {
			defer run.finish()
			e.finishFirstRunBuild(run, plan, absDir, res, buildErr)
		})
	}()

	return nil
}

func (e *Editor) applyFirstRunBuildEvent(run *firstRunBuildRun, ev FirstRunBuildEvent) {
	e.firstRun.mu.Lock()
	if e.firstRun.buildRun != run || e.firstRun.stage != FirstRunStageBuilding {
		e.firstRun.mu.Unlock()
		return
	}
	if ev.StageIndex >= 0 && ev.StageIndex < len(e.firstRun.buildStages) {
		if ev.StageStatus != "" {
			e.firstRun.buildStages[ev.StageIndex] = ev.StageStatus
		}
		for i := range e.firstRun.buildLog {
			if e.firstRun.buildLog[i].StageIndex == ev.StageIndex {
				if ev.StageStatus != "" {
					e.firstRun.buildLog[i].Status = ev.StageStatus
				}
				if ev.LogTitle != "" {
					e.firstRun.buildLog[i].Title = ev.LogTitle
				}
				if ev.LogSubtitle != "" {
					e.firstRun.buildLog[i].Subtitle = ev.LogSubtitle
				}
				if ev.ObjectsTotal > 0 {
					e.firstRun.buildLog[i].DoneCount = ev.ObjectsDone
					e.firstRun.buildLog[i].TotalCount = ev.ObjectsTotal
				}
				break
			}
		}
	}
	if ev.ObjectsTotal > 0 {
		e.firstRun.buildObjectsDone = ev.ObjectsDone
		e.firstRun.buildObjectsTotal = ev.ObjectsTotal
	}
	if ev.PlacingLabel != "" || ev.StageStatus == FirstRunBuildStageDone {
		e.firstRun.buildPlacingLabel = ev.PlacingLabel
	}
	if len(ev.PlacedShapes) > 0 {
		e.firstRun.buildShapes = slices.Clone(ev.PlacedShapes)
	}
	e.firstRun.mu.Unlock()
	e.notify()
}

func (e *Editor) finishFirstRunBuild(run *firstRunBuildRun, plan *FirstRunPlan, projectDir string, res *FirstRunBuildResult, buildErr error) {
	e.firstRun.mu.Lock()
	if e.firstRun.buildRun != run || e.firstRun.stage != FirstRunStageBuilding {
		e.firstRun.mu.Unlock()
		return
	}
	if e.firstRun.buildOutcome == FirstRunBuildStoppedKept || e.firstRun.buildOutcome == FirstRunBuildStoppedDeleted {
		e.firstRun.mu.Unlock()
		return
	}
	if buildErr != nil {
		e.firstRun.buildOutcome = FirstRunBuildCheckFailed
		e.firstRun.buildStages[3] = FirstRunBuildStageFail
		e.firstRun.buildCheckFailure = &FirstRunBuildFixProposal{
			ProblemSummary: buildErr.Error(),
			FixDescription: "I can retry and fix the project configuration.",
		}
		if len(e.firstRun.buildLog) >= 4 {
			e.firstRun.buildLog[3].Status = FirstRunBuildStageFail
			e.firstRun.buildLog[3].Title = "check: 1 problem"
			e.firstRun.buildLog[3].Subtitle = buildErr.Error()
		}
		e.firstRun.mu.Unlock()
		e.notify()
		return
	}
	if res == nil {
		res = buildFirstRunResultFromProject(projectDir, "scenes/"+plan.SceneName+".scene.yaml", plan, nil, nil, 0, nil)
	}
	if res.CheckFailure != nil {
		e.firstRun.buildOutcome = FirstRunBuildCheckFailed
		e.firstRun.buildStages = [5]FirstRunBuildStageStatus{
			FirstRunBuildStageDone,
			FirstRunBuildStageDone,
			FirstRunBuildStageDone,
			FirstRunBuildStageFail,
			FirstRunBuildStageTodo,
		}
		e.firstRun.buildCheckFailure = res.CheckFailure
		e.firstRun.buildPendingRes = res
		if len(res.Shapes) > 0 {
			e.firstRun.buildShapes = slices.Clone(res.Shapes)
		}
		if len(e.firstRun.buildLog) >= 5 {
			builtCount := max(e.firstRun.buildObjectsDone, countFirstRunReviewObjects(res))
			e.firstRun.buildLog[0].Status = FirstRunBuildStageDone
			e.firstRun.buildLog[1].Status = FirstRunBuildStageDone
			e.firstRun.buildLog[2].Status = FirstRunBuildStageDone
			e.firstRun.buildLog[2].Title = "Built " + plan.SceneName
			e.firstRun.buildLog[2].Subtitle = fmt.Sprintf("%d objects", builtCount)
			e.firstRun.buildLog[3].Status = FirstRunBuildStageFail
			e.firstRun.buildLog[3].Title = "check: 1 problem"
			e.firstRun.buildLog[3].Subtitle = res.CheckFailure.ProblemSummary
			e.firstRun.buildLog[4].Status = FirstRunBuildStageTodo
			e.firstRun.buildLog[4].Subtitle = "waits for check"
		}
		e.firstRun.mu.Unlock()
		e.notify()
		return
	}
	stage4 := e.firstRun.buildStages[4]
	if stage4 != FirstRunBuildStageFail {
		stage4 = FirstRunBuildStageDone
	}
	e.firstRun.buildStages = [5]FirstRunBuildStageStatus{
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
		stage4,
	}
	e.firstRun.mu.Unlock()

	e.enterFirstRunReview(plan, projectDir, res)
}

func (e *Editor) enterFirstRunReview(plan *FirstRunPlan, projectDir string, res *FirstRunBuildResult) {
	if res == nil {
		res = buildFirstRunResultFromProject(projectDir, "scenes/"+plan.SceneName+".scene.yaml", plan, nil, nil, 0, nil)
	}
	// Open the newly created project and built scene cleanly in the editor workspace if game.yaml exists.
	sceneRel := res.SceneRelPath
	if sceneRel == "" {
		sceneRel = "scenes/" + plan.SceneName + ".scene.yaml"
	}
	if _, err := os.Stat(filepath.Join(projectDir, project.FileName)); err == nil {
		absScene, joinErr := safeJoinUnderDir(projectDir, filepath.FromSlash(sceneRel))
		if joinErr == nil {
			if _, stErr := os.Stat(absScene); stErr == nil {
				_ = e.OpenProjectScene(projectDir, absScene)
			} else {
				_ = e.OpenProject(projectDir)
			}
		} else {
			_ = e.OpenProject(projectDir)
		}
	}

	nextTip := e.nextUnseenFirstRunTipIndex()

	e.firstRun.mu.Lock()
	e.firstRun.stage = FirstRunStageReview
	e.firstRun.reviewProjectName = plan.ProjectName
	e.firstRun.reviewProjectDir = projectDir
	e.firstRun.reviewScenePath = sceneRel
	e.firstRun.reviewPlanDone = res.PlanStepsDone
	if e.firstRun.reviewPlanDone <= 0 {
		e.firstRun.reviewPlanDone = len(plan.Steps)
	}
	e.firstRun.reviewPlanTotal = res.PlanStepsTotal
	if e.firstRun.reviewPlanTotal <= 0 {
		e.firstRun.reviewPlanTotal = len(plan.Steps)
	}
	e.firstRun.reviewDeviations = slices.Clone(res.Deviations)
	e.firstRun.reviewChanges = slices.Clone(res.Changes)
	e.firstRun.reviewShapes = slices.Clone(res.Shapes)
	e.firstRun.reviewBeforeMode = false
	e.firstRun.reviewPeekBefore = false
	e.firstRun.reviewTipIndex = nextTip
	e.firstRun.mu.Unlock()
	e.notify()
}

// defaultFirstRunBuilder scaffolds the project folder created after Approve, opens it in the editor,
// asks the connected agent to build the first scene via scene_edit, checks it with project.Check / assetcheck,
// runs game playtest (or skips it honestly when playtest.yaml is absent), and derives the first review
// from the actual scene and files written on disk.
func (e *Editor) defaultFirstRunBuilder(ctx context.Context, req FirstRunBuildRequest, emit func(FirstRunBuildEvent)) (*FirstRunBuildResult, error) {
	if err := req.WaitIfPaused(ctx); err != nil {
		return nil, err
	}
	sceneRel := "scenes/" + req.Plan.SceneName + ".scene.yaml"
	absScene, err := safeJoinUnderDir(req.ProjectDir, filepath.FromSlash(sceneRel))
	if err != nil {
		return nil, err
	}

	// Stage 1: Project scaffold.
	if e.CreateProject != nil {
		createReq := CreateProjectRequest{
			Name:     req.Plan.ProjectName,
			Dir:      req.ProjectDir,
			Module:   req.Plan.Module,
			Template: req.Plan.Template,
		}
		createErr := e.CreateProject(ctx, createReq, io.Discard, io.Discard)
		if createErr != nil && ctx.Err() == nil {
			// ApproveFirstRunPlan atomically reserved req.ProjectDir as an empty directory;
			// if game new refused an existing empty folder, release the empty dir and retry once.
			if entries, readErr := os.ReadDir(req.ProjectDir); readErr == nil && len(entries) == 0 {
				if rmErr := os.Remove(req.ProjectDir); rmErr == nil {
					createErr = e.CreateProject(ctx, createReq, io.Discard, io.Discard)
					if _, statErr := os.Stat(req.ProjectDir); errors.Is(statErr, os.ErrNotExist) {
						_ = os.Mkdir(req.ProjectDir, 0o755)
					}
				}
			}
		}
		if createErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if _, statErr := os.Stat(filepath.Join(req.ProjectDir, project.FileName)); statErr != nil {
				return nil, createErr
			}
		}
	} else {
		if err := scaffoldFirstRunProject(req.ProjectDir, req.Plan); err != nil {
			return nil, err
		}
	}

	// Ensure the target scene file exists before opening it so scene_edit always targets it.
	if _, statErr := os.Stat(absScene); errors.Is(statErr, os.ErrNotExist) {
		if err := writeEmptyFirstRunScene(req.ProjectDir, sceneRel, req.Plan.SceneName); err != nil {
			return nil, err
		}
	}

	scaffoldFiles := countProjectFilesExcluding(req.ProjectDir, sceneRel)

	// Write .mcp.json when SyncMCPConfig is wired so CLI agents can discover the editor's MCP server.
	if e.SyncMCPConfig != nil {
		_ = e.SyncMCPConfig(req.ProjectDir, nil, nil)
	}

	// Open the scaffolded project & target scene in the editor so MCP scene_edit edits it live.
	if err := e.firstRunRunOnEditor(ctx, func() error {
		return e.OpenProjectScene(req.ProjectDir, absScene)
	}); err != nil {
		return nil, err
	}
	baselineDoc := e.firstRunSnapshotDoc(ctx)

	emit(FirstRunBuildEvent{
		StageIndex:  1,
		StageStatus: FirstRunBuildStageDone,
		LogTitle:    "Created " + req.Plan.DisplayPath,
		LogSubtitle: formatFirstRunFileCount(scaffoldFiles, req.Plan.TemplateTitle),
	})

	// Stage 2: Ask the connected agent to build the first scene via MCP tools (scene_edit).
	if err := req.WaitIfPaused(ctx); err != nil {
		return nil, err
	}
	runner, route, extKind, err := e.resolveFirstRunRunner()
	if err != nil {
		return nil, err
	}
	estTotal := max(req.Plan.EstimatedObjects, 0)
	stage2InitSub := "waiting for agent"
	if estTotal > 0 {
		stage2InitSub = fmt.Sprintf("0 of %d objects", estTotal)
	}
	emit(FirstRunBuildEvent{
		StageIndex:   2,
		StageStatus:  FirstRunBuildStageActive,
		LogTitle:     "Building " + req.Plan.SceneName,
		LogSubtitle:  stage2InitSub,
		ObjectsDone:  0,
		ObjectsTotal: estTotal,
	})

	baselineIDs := sceneObjectIDSet(baselineDoc)
	prevAddedCount := 0
	var streamErr string
	buildPrompt := buildFirstRunBuildPrompt(req, sceneRel)
	runErr := runner.Run(ctx, AgentRequest{
		Prompt:      buildPrompt,
		ProjectRoot: req.ProjectDir,
		ScenePath:   sceneRel,
		AgentKind:   extKind,
		Route:       route,
		Task:        AgentTaskSceneEdit,
		Permissions: e.AgentPermissions(),
	}, func(ev AgentEvent) {
		if ev.Kind == AgentEventError && strings.TrimSpace(ev.Err) != "" && streamErr == "" {
			streamErr = strings.TrimSpace(ev.Err)
			return
		}
		if ev.Kind != AgentEventStep {
			return
		}
		if err := req.WaitIfPaused(ctx); err != nil {
			return
		}
		curDoc := e.firstRunSnapshotDoc(ctx)
		addedObjs := diffFirstRunAddedObjects(baselineIDs, curDoc)
		done := len(addedObjs)
		total := max(estTotal, done)
		label := strings.TrimSpace(ev.StepTitle)
		if done > prevAddedCount {
			label = "placed " + addedObjs[done-1].obj.ID
			prevAddedCount = done
		}
		sub := fmt.Sprintf("%d objects", done)
		if total > 0 && done < total {
			sub = fmt.Sprintf("%d of %d objects", done, total)
		}
		shapes := firstRunShapesFromScene(curDoc, addedObjs)
		emit(FirstRunBuildEvent{
			StageIndex:   2,
			StageStatus:  FirstRunBuildStageActive,
			LogTitle:     "Building " + req.Plan.SceneName,
			LogSubtitle:  sub,
			ObjectsDone:  done,
			ObjectsTotal: total,
			PlacingLabel: label,
			PlacedShapes: shapes,
		})
	})
	if runErr != nil {
		return nil, runErr
	}
	if streamErr != "" {
		return nil, errors.New(streamErr)
	}
	if err := req.WaitIfPaused(ctx); err != nil {
		return nil, err
	}

	// Persist in-memory scene_edit changes to disk, or reload disk edits made by a CLI agent.
	var finalDoc *sceneformat.SceneDoc
	if err := e.firstRunRunOnEditor(ctx, func() error {
		if e.Doc != nil && !sceneDocsEqual(baselineDoc, e.Doc) {
			if err := e.SaveFile(absScene); err != nil {
				return err
			}
			finalDoc = e.Doc.Clone()
			return nil
		}
		// Reload from disk in case the agent edited the scene file directly.
		if raw, rerr := os.ReadFile(absScene); rerr == nil {
			if parsed, perr := sceneformat.ParseWith(raw, sceneformat.ParseOptions{
				File:       absScene,
				Components: e.components,
			}); perr == nil {
				finalDoc = parsed
				_ = e.OpenProjectScene(req.ProjectDir, absScene)
				return nil
			}
		}
		if e.Doc != nil {
			finalDoc = e.Doc.Clone()
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var deviations []FirstRunPlanDeviation
	res := buildFirstRunResultFromProject(req.ProjectDir, sceneRel, &req.Plan, baselineDoc, finalDoc, scaffoldFiles, nil)
	builtObjects := countFirstRunReviewObjects(res)
	if req.Plan.EstimatedObjects > 0 && builtObjects != req.Plan.EstimatedObjects {
		deviations = append(deviations, FirstRunPlanDeviation{
			Summary: fmt.Sprintf("%d objects built (plan estimated %d)", builtObjects, req.Plan.EstimatedObjects),
		})
		res.Deviations = append(res.Deviations, deviations...)
	}
	emit(FirstRunBuildEvent{
		StageIndex:   2,
		StageStatus:  FirstRunBuildStageDone,
		LogTitle:     "Built " + req.Plan.SceneName,
		LogSubtitle:  fmt.Sprintf("%d objects", builtObjects),
		ObjectsDone:  builtObjects,
		ObjectsTotal: max(builtObjects, 1),
		PlacedShapes: res.Shapes,
	})

	// Stage 3: Check project.
	if err := req.WaitIfPaused(ctx); err != nil {
		return nil, err
	}
	emit(FirstRunBuildEvent{
		StageIndex:  3,
		StageStatus: FirstRunBuildStageActive,
		LogTitle:    "check",
		LogSubtitle: "checking game.yaml and scenes",
	})
	if _, err := project.Check(os.DirFS(req.ProjectDir)); err != nil {
		res.CheckFailure = &FirstRunBuildFixProposal{
			ProblemSummary: err.Error(),
			FixDescription: "I can repair game.yaml so the project check passes.",
			ApplyFix: func(context.Context, string) error {
				return repairFirstRunProjectFile(req.ProjectDir, req.Plan)
			},
		}
		return res, nil
	}
	if report, err := assetcheck.CheckWith(os.DirFS(req.ProjectDir), assetcheck.Options{Components: e.components}); err == nil && report != nil && !report.OK() && len(report.Problems) > 0 {
		pr := report.Problems[0]
		res.CheckFailure = &FirstRunBuildFixProposal{
			ProblemSummary: pr.Message,
			FixDescription: "Fix " + pr.File + " so check passes.",
		}
		return res, nil
	}
	emit(FirstRunBuildEvent{
		StageIndex:  3,
		StageStatus: FirstRunBuildStageDone,
		LogTitle:    "check",
		LogSubtitle: "0 problems",
	})

	// Stage 4: Headless playtest via `game playtest`, or honest skip when the project has no playtest.yaml.
	if err := req.WaitIfPaused(ctx); err != nil {
		return nil, err
	}
	playtestCfgPath := filepath.Join(req.ProjectDir, playtest.FileName)
	if _, statErr := os.Stat(playtestCfgPath); statErr != nil {
		emit(FirstRunBuildEvent{
			StageIndex:  4,
			StageStatus: FirstRunBuildStageDone,
			LogTitle:    "Playtest",
			LogSubtitle: "Skipped · template has no playtest.yaml",
		})
		return res, nil
	}

	emit(FirstRunBuildEvent{
		StageIndex:  4,
		StageStatus: FirstRunBuildStageActive,
		LogTitle:    "Playtest",
		LogSubtitle: "running game playtest",
	})
	playtestRunner := e.FirstRunPlaytest
	if playtestRunner == nil {
		playtestRunner = runFirstRunPlaytestCommand
	}
	rep, ptErr := playtestRunner(ctx, req.ProjectDir)
	switch {
	case errors.Is(ptErr, errFirstRunPlaytestUnsupported):
		emit(FirstRunBuildEvent{
			StageIndex:  4,
			StageStatus: FirstRunBuildStageDone,
			LogTitle:    "Playtest",
			LogSubtitle: "Skipped · headless playtest requires desktop",
		})
	case ptErr == nil && rep != nil && rep.Passed:
		runs := max(len(rep.Runs), 1)
		emit(FirstRunBuildEvent{
			StageIndex:  4,
			StageStatus: FirstRunBuildStageDone,
			LogTitle:    "Playtest",
			LogSubtitle: fmt.Sprintf("passed · %d runs · 0 missed goals", runs),
		})
	default:
		reason := "playtest failed"
		if rep != nil && !rep.Passed {
			missed := rep.MissedMetrics()
			reason = fmt.Sprintf("failed · %d missed goal(s)", len(missed))
		} else if ptErr != nil {
			reason = truncateRunes(strings.TrimSpace(ptErr.Error()), 120)
		}
		emit(FirstRunBuildEvent{
			StageIndex:  4,
			StageStatus: FirstRunBuildStageFail,
			LogTitle:    "Playtest",
			LogSubtitle: reason,
		})
		res.Deviations = append(res.Deviations, FirstRunPlanDeviation{
			Summary: "Playtest: " + reason,
		})
	}
	return res, nil
}

func buildFirstRunBuildPrompt(req FirstRunBuildRequest, sceneRel string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Build the first scene %s for the new NSGo game project %q.\n", sceneRel, req.Plan.ProjectName)
	if strings.TrimSpace(req.Plan.Idea) != "" {
		fmt.Fprintf(&b, "Author's idea: %s\n", strings.TrimSpace(req.Plan.Idea))
	}
	fmt.Fprintf(&b, "Template: %s (%s)\n", req.Plan.Template, req.Plan.TemplateTitle)
	if req.Plan.Question != nil && req.Answer != "" {
		ansLabel := req.Answer
		for _, opt := range req.Plan.Question.Options {
			if opt.ID == req.Answer {
				ansLabel = opt.Label + " (" + opt.ID + ")"
				break
			}
		}
		fmt.Fprintf(&b, "Author's answer to %q: %s\n", req.Plan.Question.Prompt, ansLabel)
	}
	if len(req.Plan.Steps) > 0 {
		b.WriteString("Approved plan:\n")
		for _, st := range req.Plan.Steps {
			fmt.Fprintf(&b, "%d. %s\n", st.Number, st.Title)
		}
	}
	fmt.Fprintf(&b, "\nUse the editor's scene_edit MCP tool to place all planned objects and components into %s. Do not leave placeholder content from unrelated genres.", sceneRel)
	return b.String()
}

func (e *Editor) firstRunRunOnEditor(ctx context.Context, fn func() error) error {
	if fn == nil {
		return nil
	}
	errCh := make(chan error, 1)
	e.firstRunPostOrRun(func() {
		errCh <- fn()
	})
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Editor) firstRunSnapshotDoc(ctx context.Context) *sceneformat.SceneDoc {
	var snap *sceneformat.SceneDoc
	_ = e.firstRunRunOnEditor(ctx, func() error {
		if e.Doc != nil {
			snap = e.Doc.Clone()
		}
		return nil
	})
	return snap
}

func validateFirstRunSceneName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("editor: invalid first-run scene name %q", name)
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return fmt.Errorf("editor: invalid first-run scene name %q", name)
		}
	}
	return nil
}

func safeJoinUnderDir(baseDir, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("editor: path %q must be relative to project root", rel)
	}
	cleanBase, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", err
	}
	full := filepath.Clean(filepath.Join(cleanBase, rel))
	r, err := filepath.Rel(cleanBase, full)
	if err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", fmt.Errorf("editor: path %q escapes project directory", rel)
	}
	return full, nil
}

func repairFirstRunProjectFile(dir string, plan FirstRunPlan) error {
	idSlug := strings.ReplaceAll(plan.ProjectName, "-", "")
	if idSlug == "" {
		idSlug = "mygame"
	}
	pf := &project.File{
		Version:    project.CurrentVersion,
		Name:       plan.ProjectName,
		ID:         "dev.nsgo." + idSlug,
		AppVersion: "0.1.0",
		Window: project.Window{
			Width:  640,
			Height: 360,
		},
	}
	gameYAML, err := project.Encode(pf)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, project.FileName), gameYAML)
}

func scaffoldFirstRunProject(dir string, plan FirstRunPlan) error {
	if err := validateFirstRunSceneName(plan.SceneName); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		return err
	}
	if err := repairFirstRunProjectFile(dir, plan); err != nil {
		return err
	}
	sceneRel := "scenes/" + plan.SceneName + ".scene.yaml"
	return writeEmptyFirstRunScene(dir, sceneRel, plan.SceneName)
}

func writeEmptyFirstRunScene(dir, sceneRel, sceneName string) error {
	if err := validateFirstRunSceneName(sceneName); err != nil {
		return err
	}
	full, err := safeJoinUnderDir(dir, filepath.FromSlash(sceneRel))
	if err != nil {
		return err
	}
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            sceneName,
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#15181d",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Order: 0,
			},
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 1,
			},
		},
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(full, data)
}

type firstRunSceneEntry struct {
	layer string
	obj   sceneformat.ObjectDoc
}

func flattenFirstRunSceneEntries(doc *sceneformat.SceneDoc) []firstRunSceneEntry {
	if doc == nil {
		return nil
	}
	var out []firstRunSceneEntry
	var walk func(layer string, objs []sceneformat.ObjectDoc)
	walk = func(layer string, objs []sceneformat.ObjectDoc) {
		for _, o := range objs {
			cp := o.Clone()
			cp.Children = nil
			out = append(out, firstRunSceneEntry{layer: layer, obj: cp})
			if len(o.Children) > 0 {
				walk(layer, o.Children)
			}
		}
	}
	for _, l := range doc.Layers {
		walk(l.Name, l.Objects)
	}
	return out
}

func sceneObjectIDSet(doc *sceneformat.SceneDoc) map[string]struct{} {
	entries := flattenFirstRunSceneEntries(doc)
	set := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		set[e.obj.ID] = struct{}{}
	}
	return set
}

func diffFirstRunAddedObjects(baselineIDs map[string]struct{}, doc *sceneformat.SceneDoc) []firstRunSceneEntry {
	all := flattenFirstRunSceneEntries(doc)
	if len(baselineIDs) == 0 {
		return all
	}
	var added []firstRunSceneEntry
	for _, e := range all {
		if _, existed := baselineIDs[e.obj.ID]; !existed {
			added = append(added, e)
		}
	}
	return added
}

func sceneDocsEqual(a, b *sceneformat.SceneDoc) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ba, errA := sceneformat.Encode(a)
	bb, errB := sceneformat.Encode(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ba, bb)
}

func countProjectFilesExcluding(projectDir, excludeRel string) int {
	if projectDir == "" {
		return 0
	}
	normExclude := filepath.ToSlash(filepath.Clean(excludeRel))
	count := 0
	_ = filepath.WalkDir(projectDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".nsgo" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(projectDir, p)
		if rerr != nil {
			return nil
		}
		slash := filepath.ToSlash(filepath.Clean(rel))
		if slash == normExclude || slash == ".mcp.json" {
			return nil
		}
		count++
		return nil
	})
	return count
}

func formatFirstRunFileCount(n int, templateTitle string) string {
	unit := "files"
	if n == 1 {
		unit = "file"
	}
	if strings.TrimSpace(templateTitle) != "" {
		return fmt.Sprintf("%d %s from %s", n, unit, templateTitle)
	}
	return fmt.Sprintf("%d %s", n, unit)
}

func sanitizeFirstRunChangeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "object"
	}
	return b.String()
}

func formatFirstRunObjectDetail(entry firstRunSceneEntry) string {
	var parts []string
	if strings.TrimSpace(entry.obj.Name) != "" && entry.obj.Name != entry.obj.ID {
		parts = append(parts, entry.obj.Name)
	}
	if entry.layer != "" {
		parts = append(parts, entry.layer)
	}
	parts = append(parts, fmt.Sprintf("(%s, %s)", turnFormatFloat(entry.obj.X), turnFormatFloat(entry.obj.Y)))
	if len(entry.obj.Components) > 0 {
		compNames := slices.Sorted(maps.Keys(entry.obj.Components))
		if len(compNames) > 0 {
			parts = append(parts, strings.Join(compNames, ", "))
		}
	}
	return strings.Join(parts, " · ")
}

func firstRunShapesFromScene(doc *sceneformat.SceneDoc, entries []firstRunSceneEntry) []FirstRunPreviewShape {
	if len(entries) == 0 {
		return nil
	}
	vw := float32(640)
	vh := float32(360)
	if doc != nil {
		if doc.VirtualWidth > 0 {
			vw = float32(doc.VirtualWidth)
		}
		if doc.VirtualHeight > 0 {
			vh = float32(doc.VirtualHeight)
		}
	}
	// Expand bounds if objects sit outside the virtual frame so every placed object is visible inside (16, 68, 340, 220).
	minX, minY := float32(0), float32(0)
	maxX, maxY := vw, vh
	for _, e := range entries {
		if e.obj.X < minX {
			minX = e.obj.X
		}
		if e.obj.Y < minY {
			minY = e.obj.Y
		}
		if e.obj.X > maxX {
			maxX = e.obj.X
		}
		if e.obj.Y > maxY {
			maxY = e.obj.Y
		}
	}
	spanX := maxX - minX
	if spanX < 1 {
		spanX = 1
	}
	spanY := maxY - minY
	if spanY < 1 {
		spanY = 1
	}

	const (
		canvasX = float32(28)
		canvasY = float32(80)
		canvasW = float32(316)
		canvasH = float32(196)
		boxW    = float32(16)
		boxH    = float32(16)
	)
	shapes := make([]FirstRunPreviewShape, 0, len(entries))
	for _, e := range entries {
		nx := (e.obj.X - minX) / spanX
		ny := (e.obj.Y - minY) / spanY
		px := canvasX + float32(math.Round(float64(nx*(canvasW-boxW))))
		py := canvasY + float32(math.Round(float64(ny*(canvasH-boxH))))
		shapes = append(shapes, FirstRunPreviewShape{
			ID:       "sh-" + sanitizeFirstRunChangeID(e.obj.ID),
			ChangeID: "obj-" + sanitizeFirstRunChangeID(e.obj.ID),
			Kind:     "object",
			X:        px,
			Y:        py,
			W:        boxW,
			H:        boxH,
			Outline:  "added",
			Badge:    e.obj.ID,
		})
	}
	return shapes
}

func buildFirstRunResultFromProject(projectDir, sceneRel string, plan *FirstRunPlan, baselineDoc, finalDoc *sceneformat.SceneDoc, scaffoldFileCount int, deviations []FirstRunPlanDeviation) *FirstRunBuildResult {
	sceneName := "level_01"
	stepsCount := 0
	tmplTitle := ""
	if plan != nil {
		if plan.SceneName != "" {
			sceneName = plan.SceneName
		}
		stepsCount = len(plan.Steps)
		tmplTitle = plan.TemplateTitle
	}
	if sceneRel == "" {
		sceneRel = "scenes/" + sceneName + ".scene.yaml"
	}
	if finalDoc == nil && projectDir != "" {
		if full, err := safeJoinUnderDir(projectDir, filepath.FromSlash(sceneRel)); err == nil {
			if raw, rerr := os.ReadFile(full); rerr == nil {
				if parsed, perr := sceneformat.Parse(raw); perr == nil {
					finalDoc = parsed
				}
			}
		}
	}
	if scaffoldFileCount <= 0 && projectDir != "" {
		scaffoldFileCount = countProjectFilesExcluding(projectDir, sceneRel)
	}

	var changes []FirstRunReviewChange
	if scaffoldFileCount > 0 {
		changes = append(changes, FirstRunReviewChange{
			ID:     "tpl",
			Sign:   "+",
			Title:  "Template files",
			Detail: formatFirstRunFileCount(scaffoldFileCount, tmplTitle),
		})
	}

	entries := diffFirstRunAddedObjects(sceneObjectIDSet(baselineDoc), finalDoc)
	for _, entry := range entries {
		changes = append(changes, FirstRunReviewChange{
			ID:        "obj-" + sanitizeFirstRunChangeID(entry.obj.ID),
			Sign:      "+",
			Title:     entry.obj.ID,
			Detail:    formatFirstRunObjectDetail(entry),
			ObjectIDs: []string{entry.obj.ID},
		})
	}

	shapes := firstRunShapesFromScene(finalDoc, entries)
	return &FirstRunBuildResult{
		SceneRelPath:   sceneRel,
		PlanStepsDone:  stepsCount,
		PlanStepsTotal: stepsCount,
		Deviations:     slices.Clone(deviations),
		Changes:        changes,
		Shapes:         shapes,
	}
}

func countFirstRunReviewObjects(res *FirstRunBuildResult) int {
	if res == nil {
		return 0
	}
	total := 0
	for _, c := range res.Changes {
		total += len(c.ObjectIDs)
	}
	return total
}

// WaitForFirstRunBuild blocks until any active first-run build goroutine finishes or stops.
func (e *Editor) WaitForFirstRunBuild() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	run := e.firstRun.buildRun
	e.firstRun.mu.Unlock()
	if run != nil {
		<-run.done
	}
}

// FirstRunBuildState returns a snapshot of the active Building screen state.
func (e *Editor) FirstRunBuildState() FirstRunBuildSnapshot {
	if e == nil {
		return FirstRunBuildSnapshot{}
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()

	var projName, sceneName, displayPath string
	if e.firstRun.plan != nil {
		projName = e.firstRun.plan.ProjectName
		sceneName = e.firstRun.plan.SceneName
		displayPath = e.firstRun.plan.DisplayPath
	}
	now := e.now()
	elapsed := max(now.Sub(e.firstRun.buildStartedAt), 0)
	ageSec := max(int(now.Sub(e.firstRun.createdAt).Seconds()), 0)

	activeIdx := 1
	for i, st := range e.firstRun.buildStages {
		if st == FirstRunBuildStageActive {
			activeIdx = i + 1
			break
		}
	}
	pill := fmt.Sprintf("Building %s · %d of 5", sceneName, activeIdx)
	switch e.firstRun.buildOutcome {
	case FirstRunBuildCancelPrompt:
		pill = "Paused · waiting for you"
	case FirstRunBuildCheckFailed:
		pill = "Check found 1 problem"
	case FirstRunBuildStoppedDeleted:
		pill = "Stopped · folder deleted"
	case FirstRunBuildStoppedKept:
		pill = fmt.Sprintf("Stopped · %d objects kept", e.firstRun.buildObjectsDone)
	}

	var cf *FirstRunBuildFixProposal
	if e.firstRun.buildCheckFailure != nil {
		cp := *e.firstRun.buildCheckFailure
		cf = &cp
	}

	return FirstRunBuildSnapshot{
		ProjectName:       projName,
		SceneName:         sceneName,
		DisplayPath:       displayPath,
		Outcome:           e.firstRun.buildOutcome,
		Stages:            e.firstRun.buildStages,
		Log:               slices.Clone(e.firstRun.buildLog),
		ObjectsDone:       e.firstRun.buildObjectsDone,
		ObjectsTotal:      e.firstRun.buildObjectsTotal,
		PlacingLabel:      e.firstRun.buildPlacingLabel,
		Shapes:            slices.Clone(e.firstRun.buildShapes),
		Elapsed:           elapsed,
		CreatedByFlow:     e.firstRun.createdByFlow,
		CanDeleteFolder:   e.canDeleteFirstRunFolderLocked(),
		CreatedAgeSeconds: ageSec,
		CheckFailure:      cf,
		StatusPill:        pill,
	}
}

// CanDeleteFirstRunFolder reports whether "Stop and delete the folder" is permitted right now:
// ONLY true when this flow itself created a brand-new folder after Approve.
func (e *Editor) CanDeleteFirstRunFolder() bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.canDeleteFirstRunFolderLocked()
}

func (e *Editor) canDeleteFirstRunFolderLocked() bool {
	if !e.firstRun.createdByFlow || e.firstRun.createdDir == "" {
		return false
	}
	return isSafeOwnedProjectDir(e.firstRun.createdDir)
}

func isSafeOwnedProjectDir(dir string) bool {
	clean := filepath.Clean(dir)
	if clean == "" || clean == "." || clean == string(filepath.Separator) {
		return false
	}
	if filepath.Dir(clean) == clean {
		return false
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cleanHome := filepath.Clean(home)
		if clean == cleanHome {
			return false
		}
	}
	return filepath.IsAbs(clean)
}

// RequestCancelFirstRunBuild pauses building and opens the "Stop building?" confirmation dialog
// (triggered by Cancel or Esc during Building).
func (e *Editor) RequestCancelFirstRunBuild() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	if e.firstRun.stage != FirstRunStageBuilding || e.firstRun.buildOutcome != FirstRunBuildRunning {
		e.firstRun.mu.Unlock()
		return
	}
	e.firstRun.buildOutcome = FirstRunBuildCancelPrompt
	run := e.firstRun.buildRun
	e.firstRun.mu.Unlock()
	if run != nil {
		run.setPaused(true)
	}
	e.notify()
}

// KeepBuildingFirstRun dismisses the Cancel confirmation dialog and resumes building.
func (e *Editor) KeepBuildingFirstRun() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	if e.firstRun.stage != FirstRunStageBuilding || e.firstRun.buildOutcome != FirstRunBuildCancelPrompt {
		e.firstRun.mu.Unlock()
		return
	}
	e.firstRun.buildOutcome = FirstRunBuildRunning
	run := e.firstRun.buildRun
	e.firstRun.mu.Unlock()
	if run != nil {
		run.setPaused(false)
	}
	e.notify()
}

// StopFirstRunKeepFiles stops building while preserving all files created so far on disk,
// offering "Review them" to inspect what was built.
func (e *Editor) StopFirstRunKeepFiles() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	if e.firstRun.stage != FirstRunStageBuilding {
		e.firstRun.mu.Unlock()
		return
	}
	run := e.firstRun.buildRun
	e.firstRun.buildOutcome = FirstRunBuildStoppedKept
	if len(e.firstRun.buildLog) >= 3 && e.firstRun.buildLog[2].Status == FirstRunBuildStageActive {
		e.firstRun.buildLog[2].Status = FirstRunBuildStageTodo
	}
	e.firstRun.mu.Unlock()
	if run != nil {
		run.finish()
	}
	e.notify()
}

// OpenFirstRunReviewFromStopped opens the First Review for the partial files kept after StopFirstRunKeepFiles.
func (e *Editor) OpenFirstRunReviewFromStopped() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	if e.firstRun.stage != FirstRunStageBuilding || e.firstRun.buildOutcome != FirstRunBuildStoppedKept {
		e.firstRun.mu.Unlock()
		return
	}
	plan := cloneFirstRunPlan(e.firstRun.plan)
	projectDir := e.firstRun.createdDir
	doneObjs := e.firstRun.buildObjectsDone
	shapes := slices.Clone(e.firstRun.buildShapes)
	e.firstRun.mu.Unlock()
	if plan == nil {
		return
	}
	res := buildFirstRunResultFromProject(projectDir, "scenes/"+plan.SceneName+".scene.yaml", plan, nil, nil, 0, []FirstRunPlanDeviation{
		{Summary: fmt.Sprintf("Stopped early: %d objects kept", doneObjs)},
	})
	res.PlanStepsDone = min(2, len(plan.Steps))
	if len(shapes) > 0 && len(res.Shapes) == 0 {
		res.Shapes = shapes
	}
	e.enterFirstRunReview(plan, projectDir, res)
}

// StopFirstRunDeleteFolder stops building and removes ONLY the brand-new folder created by this flow after Approve.
// It refuses to delete any folder that was not created by this flow, and never deletes sibling or parent directories.
func (e *Editor) StopFirstRunDeleteFolder() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.firstRun.mu.Lock()
	if e.firstRun.stage != FirstRunStageBuilding {
		e.firstRun.mu.Unlock()
		return errors.New("editor: no first-run build active")
	}
	if !e.canDeleteFirstRunFolderLocked() {
		e.firstRun.mu.Unlock()
		return errors.New("editor: folder was not created by this Start with AI flow and cannot be deleted")
	}
	dirToDelete := e.firstRun.createdDir
	run := e.firstRun.buildRun
	e.firstRun.buildOutcome = FirstRunBuildStoppedDeleted
	e.firstRun.mu.Unlock()

	if run != nil {
		run.finish()
		select {
		case <-run.done:
		case <-time.After(2 * time.Second):
			e.firstRun.mu.Lock()
			if e.firstRun.buildOutcome == FirstRunBuildStoppedDeleted {
				e.firstRun.buildOutcome = FirstRunBuildStoppedKept
			}
			e.firstRun.mu.Unlock()
			e.notify()
			return errors.New("editor: build did not stop in time; folder was not deleted")
		}
	}

	// Detach project and stop watchers/autosave if the editor had opened it inside dirToDelete.
	if e.ProjectPath != "" {
		projRoot := filepath.Dir(e.ProjectPath)
		if realProj, err := filepath.EvalSymlinks(projRoot); err == nil {
			projRoot = realProj
		}
		if filepath.Clean(projRoot) == filepath.Clean(dirToDelete) {
			e.stopDiskWatcher()
			e.stopAutoSave()
			e.Project = nil
			e.ProjectPath = ""
			e.FilePath = ""
			e.ScenePath = ""
		}
	}

	// Re-verify safety before deleting ONLY dirToDelete.
	if !isSafeOwnedProjectDir(dirToDelete) {
		return errors.New("editor: refusing to delete unsafe directory path")
	}
	if err := os.RemoveAll(dirToDelete); err != nil {
		return fmt.Errorf("editor: delete created folder: %w", err)
	}

	e.firstRun.mu.Lock()
	e.firstRun.createdByFlow = false
	e.firstRun.createdDir = ""
	e.firstRun.mu.Unlock()
	e.refreshStartValidationLocked()
	e.notify()
	return nil
}

// ReturnToStartFromFirstRun returns to the Start Screen after Stop and delete the folder,
// keeping the author's idea and project name intact.
func (e *Editor) ReturnToStartFromFirstRun() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	e.firstRun.stage = FirstRunStageNone
	e.firstRun.plan = nil
	e.firstRun.buildCheckFailure = nil
	e.firstRun.buildPendingRes = nil
	e.firstRun.mu.Unlock()
	e.SetStartScreenOpen(true)
}

// ApplyFirstRunFix applies the agent's proposed fix after a failed Check stage and advances into First Review.
func (e *Editor) ApplyFirstRunFix() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.firstRun.mu.Lock()
	if e.firstRun.stage != FirstRunStageBuilding || e.firstRun.buildOutcome != FirstRunBuildCheckFailed {
		e.firstRun.mu.Unlock()
		return errors.New("editor: no failed check fix to apply")
	}
	fix := e.firstRun.buildCheckFailure
	plan := cloneFirstRunPlan(e.firstRun.plan)
	projectDir := e.firstRun.createdDir
	res := e.firstRun.buildPendingRes
	e.firstRun.mu.Unlock()

	if fix != nil && fix.ApplyFix != nil {
		if err := fix.ApplyFix(context.Background(), projectDir); err != nil {
			return err
		}
	}
	if res == nil {
		res = buildFirstRunResultFromProject(projectDir, "scenes/"+plan.SceneName+".scene.yaml", plan, nil, nil, 0, nil)
	}
	res.CheckFailure = nil

	e.firstRun.mu.Lock()
	e.firstRun.buildCheckFailure = nil
	e.firstRun.buildPendingRes = nil
	e.firstRun.buildOutcome = FirstRunBuildRunning
	e.firstRun.buildStages = [5]FirstRunBuildStageStatus{
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
	}
	e.firstRun.mu.Unlock()

	e.enterFirstRunReview(plan, projectDir, res)
	return nil
}

// ReviewFirstRunAsIs bypasses the failed check fix proposal and enters First Review with the current files.
func (e *Editor) ReviewFirstRunAsIs() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	if e.firstRun.stage != FirstRunStageBuilding || e.firstRun.buildOutcome != FirstRunBuildCheckFailed {
		e.firstRun.mu.Unlock()
		return
	}
	plan := cloneFirstRunPlan(e.firstRun.plan)
	projectDir := e.firstRun.createdDir
	res := e.firstRun.buildPendingRes
	e.firstRun.buildCheckFailure = nil
	e.firstRun.buildPendingRes = nil
	e.firstRun.mu.Unlock()
	if res != nil {
		res.CheckFailure = nil
	}
	e.enterFirstRunReview(plan, projectDir, res)
}

// FirstRunReviewState returns a read-only snapshot of the active First Review screen.
func (e *Editor) FirstRunReviewState() FirstRunReviewSnapshot {
	if e == nil {
		return FirstRunReviewSnapshot{ActiveTipIndex: -1}
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()

	changes := slices.Clone(e.firstRun.reviewChanges)
	kept := 0
	for _, c := range changes {
		if !c.Reverted {
			kept++
		}
	}
	tipIdx := e.firstRun.reviewTipIndex
	tipID := ""
	tipIDs := FirstRunTipIDs()
	if tipIdx >= 0 && tipIdx < len(tipIDs) {
		tipID = tipIDs[tipIdx]
	} else {
		tipIdx = -1
	}

	return FirstRunReviewSnapshot{
		ProjectName:    e.firstRun.reviewProjectName,
		ProjectDir:     e.firstRun.reviewProjectDir,
		SceneRelPath:   e.firstRun.reviewScenePath,
		PlanStepsDone:  e.firstRun.reviewPlanDone,
		PlanStepsTotal: e.firstRun.reviewPlanTotal,
		Deviations:     slices.Clone(e.firstRun.reviewDeviations),
		Changes:        changes,
		Shapes:         slices.Clone(e.firstRun.reviewShapes),
		BeforeMode:     e.firstRun.reviewBeforeMode,
		PeekBefore:     e.firstRun.reviewPeekBefore,
		ShowingBefore:  e.firstRun.reviewBeforeMode || e.firstRun.reviewPeekBefore,
		KeptCount:      kept,
		TotalCount:     len(changes),
		ActiveTipIndex: tipIdx,
		ActiveTipID:    tipID,
	}
}

// ToggleFirstRunReviewChange toggles a single change entry between kept and reverted in the First Review.
func (e *Editor) ToggleFirstRunReviewChange(id string) bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	found := false
	for i := range e.firstRun.reviewChanges {
		if e.firstRun.reviewChanges[i].ID == id {
			e.firstRun.reviewChanges[i].Reverted = !e.firstRun.reviewChanges[i].Reverted
			found = true
			break
		}
	}
	e.firstRun.mu.Unlock()
	if found {
		e.notify()
	}
	return found
}

// RevertAllFirstRunReview marks every change in the First Review as reverted.
func (e *Editor) RevertAllFirstRunReview() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	for i := range e.firstRun.reviewChanges {
		e.firstRun.reviewChanges[i].Reverted = true
	}
	e.firstRun.mu.Unlock()
	e.notify()
}

// RestoreAllFirstRunReview restores every change in the First Review to kept.
func (e *Editor) RestoreAllFirstRunReview() {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	for i := range e.firstRun.reviewChanges {
		e.firstRun.reviewChanges[i].Reverted = false
	}
	e.firstRun.mu.Unlock()
	e.notify()
}

// SetFirstRunReviewBefore toggles the Before/After segmented button in the First Review.
func (e *Editor) SetFirstRunReviewBefore(before bool) {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	changed := e.firstRun.reviewBeforeMode != before
	e.firstRun.reviewBeforeMode = before
	e.firstRun.mu.Unlock()
	if changed {
		e.notify()
	}
}

// SetFirstRunReviewPeekBefore controls the temporary hold-B Before peek in the First Review.
func (e *Editor) SetFirstRunReviewPeekBefore(peek bool) {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	changed := e.firstRun.reviewPeekBefore != peek
	e.firstRun.reviewPeekBefore = peek
	e.firstRun.mu.Unlock()
	if changed {
		e.notify()
	}
}

// NextFirstRunTip marks the active first-review tip as seen in EditorSettings and advances to the next unseen tip.
func (e *Editor) NextFirstRunTip() error {
	if e == nil {
		return nil
	}
	e.firstRun.mu.Lock()
	idx := e.firstRun.reviewTipIndex
	e.firstRun.mu.Unlock()
	tipIDs := FirstRunTipIDs()
	if idx < 0 || idx >= len(tipIDs) {
		return nil
	}
	return e.MarkTipsSeen(tipIDs[idx])
}

// SkipFirstRunTips marks all three first-review tips as seen in EditorSettings and dismisses them.
func (e *Editor) SkipFirstRunTips() error {
	if e == nil {
		return nil
	}
	return e.MarkTipsSeen(FirstRunTipIDs()...)
}

// KeepFirstRunReview finalizes the First Review, applying any reverted object removals to the open scene,
// saving, relinquishing temporary folder-delete ownership, and entering the normal working editor.
func (e *Editor) KeepFirstRunReview() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.firstRun.mu.Lock()
	if e.firstRun.stage != FirstRunStageReview {
		e.firstRun.mu.Unlock()
		return errors.New("editor: first review is not open")
	}
	changes := slices.Clone(e.firstRun.reviewChanges)
	e.firstRun.stage = FirstRunStageNone
	e.firstRun.createdByFlow = false
	e.firstRun.createdDir = ""
	e.firstRun.plan = nil
	e.firstRun.mu.Unlock()

	kept := 0
	var revertedIDs []string
	for _, c := range changes {
		if c.Reverted {
			revertedIDs = append(revertedIDs, c.ObjectIDs...)
		} else {
			kept++
		}
	}
	if len(revertedIDs) > 0 && e.Doc != nil {
		for _, id := range revertedIDs {
			if obj, _, _ := e.FindObject(id); obj != nil {
				_ = e.DeleteObject(id)
			}
		}
		if e.FilePath != "" {
			_ = e.SaveFile(e.FilePath)
		}
	}
	e.setStartScreenEntered()
	e.scheduleAutoSaveIfNeeded()
	e.ShowToast(Toast{
		ID:      "first-run-kept",
		Kind:    ToastSuccess,
		Message: fmt.Sprintf("Kept %d of %d changes from the first review", kept, len(changes)),
	})
	e.notify()
	return nil
}

func (e *Editor) resetFirstRunFlow() {
	if e == nil {
		return
	}
	e.cancelFirstRunOps()
	e.firstRun.mu.Lock()
	e.firstRun.stage = FirstRunStageNone
	e.firstRun.plan = nil
	e.firstRun.buildCheckFailure = nil
	e.firstRun.buildPendingRes = nil
	e.firstRun.reviewPeekBefore = false
	e.firstRun.mu.Unlock()
}

func (e *Editor) cancelFirstRunOps() {
	if e == nil {
		return
	}
	e.cancelFirstRunConnect()
	e.cancelFirstRunPlan()
	e.firstRun.mu.Lock()
	run := e.firstRun.buildRun
	e.firstRun.buildRun = nil
	e.firstRun.mu.Unlock()
	if run != nil {
		run.finish()
	}
}

func (e *Editor) firstRunPostOrRun(fn func()) {
	e.postOrRun(fn)
}

func (e *Editor) writeClipboardText(text string) {
	if e.WriteClipboard != nil {
		e.WriteClipboard(text)
		return
	}
	if e.scope != nil {
		e.scope.SetClipboard(text)
		return
	}
	e.clipboardText = text
}

// BindFirstRunTestScope attaches scope for first-run UI posts if needed (no-op when EditorView is mounted).
func (e *Editor) bindFirstRunKeys(cx *core.Scope) {
	cx.OnKeyWith("mod+enter", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if e.SettingsOpen() || e.ShortcutsSheetOpen() {
			return
		}
		switch e.FirstRunStage() {
		case FirstRunStagePlan:
			if e.FirstRunPlanCanApprove() {
				e.report(e.ApproveFirstRunPlan())
			}
		case FirstRunStageReview:
			e.report(e.KeepFirstRunReview())
		}
	})
}
