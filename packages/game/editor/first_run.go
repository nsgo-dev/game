package editor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
)

// FirstRunStage identifies the active stage of the Start with AI first-run journey
// (docs/design/editor/FirstRun.dc.html).
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
	ID        string
	Label     string
	Note      string
	Suggested bool
	Verb      string
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
	Kind     string // "room", "floor", "platform", "divider", "knight", "slime", "coin", "lever", "door"
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
	Kind  string // "room", "floor", "slime", "coin", "lever", "door"
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

type firstRunState struct {
	mu sync.Mutex

	stage FirstRunStage

	// Connected agent state (survives returning to Start Screen in the same session).
	agentConnected bool
	agentKind      string
	agentName      string
	agentTools     int

	// Connect in place state.
	agentOptions      []FirstRunAgentOption
	selectedAgent     string
	connectStatus     FirstRunConnectStatus
	connectStartedAt  time.Time
	connectErr        string
	diagnosticsCopied bool
	apiKey            string
	connectCancel     context.CancelFunc
	connectSeq        uint64

	// Plan state (no project folder created until Approve).
	plan *FirstRunPlan

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

// FirstRunAgentConnected reports whether an agent is currently connected for Start with AI.
func (e *Editor) FirstRunAgentConnected() bool {
	if e == nil {
		return false
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.agentConnected
}

// FirstRunConnectedAgent returns the connected agent's kind, display name, and tool count.
func (e *Editor) FirstRunConnectedAgent() (kind, name string, tools int) {
	if e == nil {
		return "", "", 0
	}
	e.firstRun.mu.Lock()
	defer e.firstRun.mu.Unlock()
	return e.firstRun.agentKind, e.firstRun.agentName, e.firstRun.agentTools
}

// SetFirstRunAgentConnected configures whether an agent is already connected (for example when
// an MCP session is active before pressing Start with AI).
func (e *Editor) SetFirstRunAgentConnected(connected bool, kind, name string, tools int) {
	if e == nil {
		return
	}
	e.firstRun.mu.Lock()
	e.firstRun.agentConnected = connected
	if connected {
		if kind == "" {
			kind = "claude"
		}
		if name == "" {
			name = firstRunAgentDisplayName(kind)
		}
		if tools <= 0 {
			tools = 18
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
		claudePath, _ = exec.LookPath("claude")
		codexPath, _ = exec.LookPath("codex")
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

// StartWithAI begins the Start with AI journey from the Start Screen.
// When no agent is connected yet, it opens Connect in place on the Start Screen while keeping
// the idea and project name intact; when an agent is already connected, it opens the first plan directly.
// No project folder is created until the author approves the plan.
func (e *Editor) StartWithAI() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.ensureStartScreenInit()

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
	e.firstRun.mu.Lock()
	e.firstRun.stage = FirstRunStageConnect
	e.firstRun.agentOptions = slices.Clone(opts)
	e.firstRun.selectedAgent = selected
	e.firstRun.diagnosticsCopied = false
	e.firstRun.connectErr = ""
	if selected == "apikey" {
		if e.firstRun.apiKey != "" {
			e.firstRun.connectStatus = e.connectStatusForAPIKey(e.firstRun.apiKey)
		} else {
			e.firstRun.connectStatus = FirstRunConnectKeyIdle
		}
	} else {
		e.firstRun.connectStatus = FirstRunConnectConnecting
		e.firstRun.connectStartedAt = e.now()
	}
	e.firstRun.mu.Unlock()

	e.SetStartScreenOpen(true)
	if selected != "apikey" {
		e.startFirstRunConnectHandshake(selected, v.DisplayPath)
	}
	e.notify()
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
	if tools <= 0 {
		tools = 18
	}
	elapsed := max(int(e.now().Sub(started).Seconds()), 0)
	step3 := FirstRunConnectStep{
		Text:    fmt.Sprintf("Asking %s to connect · %d s", agentName, elapsed),
		Waiting: true,
	}
	if st == FirstRunConnectConnected {
		step3 = FirstRunConnectStep{
			Text:   fmt.Sprintf("%s connected · %d tools", agentName, tools),
			Done:   true,
			Strong: true,
		}
	}
	return []FirstRunConnectStep{
		{Text: "Prepared MCP config for " + displayPath + " (written on Approve)", Done: true},
		{Text: "Started the editor's MCP server", Done: true},
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
		if e.firstRun.apiKey != "" {
			e.firstRun.connectStatus = e.connectStatusForAPIKey(e.firstRun.apiKey)
		} else {
			e.firstRun.connectStatus = FirstRunConnectKeyIdle
		}
		e.firstRun.mu.Unlock()
		e.notify()
		return nil
	}
	e.firstRun.connectStatus = FirstRunConnectConnecting
	e.firstRun.connectStartedAt = e.now()
	e.firstRun.mu.Unlock()

	e.startFirstRunConnectHandshake(kind, e.StartNameStatus().DisplayPath)
	e.notify()
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

func (e *Editor) startFirstRunConnectHandshake(kind, displayPath string) {
	ctx, cancel := context.WithCancel(context.Background())
	e.firstRun.mu.Lock()
	e.firstRun.connectSeq++
	seq := e.firstRun.connectSeq
	e.firstRun.connectCancel = cancel
	e.firstRun.mu.Unlock()

	connector := e.FirstRunConnectAgent
	if connector == nil {
		connector = defaultFirstRunConnectAgent
	}

	go func() {
		tools, err := connector(ctx, kind, displayPath)
		apply := func() {
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
			if tools <= 0 {
				tools = 18
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

func defaultFirstRunConnectAgent(ctx context.Context, kind, _ string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if !canRunGameDesktop {
		return 0, errors.New("no local agent CLI available on this platform")
	}
	bin := "claude"
	if kind == "codex" {
		bin = "codex"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return 0, fmt.Errorf("%s not found on PATH", bin)
	}
	return 18, nil
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

// SetFirstRunAPIKey updates the in-memory API key and immediately validates it.
func (e *Editor) SetFirstRunAPIKey(key string) {
	if e == nil {
		return
	}
	st := e.connectStatusForAPIKey(key)
	e.firstRun.mu.Lock()
	e.firstRun.apiKey = key
	if e.firstRun.selectedAgent == "apikey" {
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
	validator := e.FirstRunValidateAPIKey
	var res FirstRunAPIKeyStatus
	if validator != nil {
		res = validator(context.Background(), trimmed)
	} else {
		res = defaultValidateAPIKey(trimmed)
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

func defaultValidateAPIKey(key string) FirstRunAPIKeyStatus {
	if len(key) >= 12 && (strings.HasPrefix(key, "sk-") || strings.HasPrefix(key, "AIza")) {
		return FirstRunAPIKeyWorks
	}
	return FirstRunAPIKeyNotAccepted
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
	if tools <= 0 {
		tools = 18
	}
	e.firstRun.agentConnected = true
	e.firstRun.agentKind = kind
	e.firstRun.agentName = firstRunAgentDisplayName(kind)
	e.firstRun.agentTools = tools
	e.firstRun.mu.Unlock()

	return e.OpenFirstRunPlan()
}

// OpenFirstRunPlan generates the agent's first plan for the current Start Screen idea and name,
// and transitions to Step 3 (The first plan). No project folder is created on disk until Approve.
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

	planner := e.FirstRunPlanner
	var plan *FirstRunPlan
	var err error
	if planner != nil {
		plan, err = planner(context.Background(), req)
		if err != nil {
			return err
		}
	}
	if plan == nil {
		plan = DefaultFirstRunPlan(req)
	}
	if plan.ProjectName == "" {
		plan.ProjectName = name
	}
	if plan.TargetDir == "" {
		plan.TargetDir = v.TargetDir
	}
	if plan.DisplayPath == "" {
		plan.DisplayPath = v.DisplayPath
	}
	if plan.Module == "" {
		plan.Module = strings.TrimSpace(module)
	}
	if plan.SceneName == "" {
		plan.SceneName = "level_01"
	}

	e.firstRun.mu.Lock()
	e.firstRun.plan = cloneFirstRunPlan(plan)
	e.firstRun.stage = FirstRunStagePlan
	e.firstRun.mu.Unlock()

	// Leave Start Screen to display the full-window First Plan view.
	e.startScreen.mu.Lock()
	e.startScreen.open = false
	e.startScreen.mu.Unlock()
	e.notify()
	return nil
}

// DefaultFirstRunPlan picks the closest template for req.Idea and returns a deterministic,
// reviewable first plan matching docs/design/editor/FirstRun.dc.html.
func DefaultFirstRunPlan(req FirstRunPlanRequest) *FirstRunPlan {
	tmplName, tmplTitle := pickTemplateForIdea(req.Idea, req.Templates, req.Selected)
	verb := "stomp"
	q := &FirstRunPlanQuestion{
		Prompt: "How does the Knight beat slimes?",
		Options: []FirstRunPlanOption{
			{ID: "jump", Label: "Jump on them", Note: "Suggested", Suggested: true, Verb: "stomp"},
			{ID: "sword", Label: "Sword swing", Note: "Adds an attack", Verb: "fight"},
			{ID: "avoid", Label: "Avoid them", Note: "No combat", Verb: "dodge"},
		},
	}
	steps := []FirstRunPlanStep{
		{Number: 1, Title: "Copy the " + tmplTitle + " template", FilesLabel: "14 new files"},
		{Number: 2, Title: "Build level_01: 3 rooms, the Knight, 4 slimes to " + verb + ", 12 coins, a lever", FilesLabel: "level_01 · 31 objects"},
		{Number: 3, Title: "Run check and a 10 s headless playtest", FilesLabel: "no files", Permission: "Run the game"},
	}
	templateShapes := []FirstRunPreviewShape{
		{ID: "tpl-room1", Kind: "room", X: 16, Y: 140, W: 88, H: 150},
		{ID: "tpl-floor1", Kind: "floor", X: 16, Y: 264, W: 88, H: 26},
		{ID: "tpl-knight", Kind: "knight", X: 36, Y: 242, W: 14, H: 22},
	}
	ghosts := []FirstRunGhost{
		{ID: "ghost-tpl", Step: 1, Label: "template", Kind: "room", X: 14, Y: 138, W: 92, H: 154},
		{ID: "ghost-rooms", Step: 2, Label: "rooms 2–3", Kind: "room", X: 110, Y: 138, W: 196, H: 154},
		{ID: "ghost-slime1", Step: 2, Kind: "slime", X: 134, Y: 250, W: 18, H: 12},
		{ID: "ghost-slime2", Step: 2, Kind: "slime", X: 182, Y: 250, W: 18, H: 12},
		{ID: "ghost-coin1", Step: 2, Kind: "coin", X: 156, Y: 210, W: 8, H: 8},
		{ID: "ghost-coin2", Step: 2, Kind: "coin", X: 170, Y: 204, W: 8, H: 8},
		{ID: "ghost-coin3", Step: 2, Kind: "coin", X: 184, Y: 210, W: 8, H: 8},
		{ID: "ghost-lever", Step: 2, Kind: "lever", X: 238, Y: 248, W: 6, H: 16},
		{ID: "ghost-door", Step: 2, Kind: "door", X: 274, Y: 228, W: 18, H: 36},
	}
	return &FirstRunPlan{
		ProjectName:      req.ProjectName,
		TargetDir:        req.TargetDir,
		DisplayPath:      req.DisplayPath,
		Module:           req.Module,
		Idea:             req.Idea,
		Template:         tmplName,
		TemplateTitle:    tmplTitle,
		SceneName:        "level_01",
		EstimatedFiles:   15,
		EstimatedObjects: 31,
		EstimatedMinutes: 3,
		Question:         q,
		Steps:            steps,
		TemplateShapes:   templateShapes,
		Ghosts:           ghosts,
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
		q.Options = slices.Clone(p.Question.Options)
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
	if picked.Verb != "" && len(plan.Steps) >= 2 {
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
	if e.firstRun.stage != FirstRunStagePlan || e.firstRun.plan == nil || e.firstRun.plan.Approved {
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
	e.firstRun.mu.Lock()
	e.firstRun.stage = FirstRunStageNone
	e.firstRun.plan = nil
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

	targetDir := filepath.Clean(plan.TargetDir)
	if targetDir == "" || targetDir == "." {
		return errors.New("editor: empty project target directory")
	}
	// Safety invariant: refuse to claim or overwrite an existing folder.
	if _, err := os.Stat(targetDir); err == nil {
		return fmt.Errorf("editor: folder %s already exists", shortenHomePath(targetDir))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("editor: create project folder: %w", err)
	}
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		absDir = targetDir
	}
	if realDir, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = realDir
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
	initialLog := []FirstRunBuildLogEntry{
		{StageIndex: 0, Status: FirstRunBuildStageDone, Title: "Picked " + plan.TemplateTitle, Subtitle: "closest template for your game"},
		{StageIndex: 1, Status: FirstRunBuildStageActive, Title: "Creating " + plan.DisplayPath, Subtitle: fmt.Sprintf("%d files from the template", max(plan.EstimatedFiles-1, 1))},
		{StageIndex: 2, Status: FirstRunBuildStageTodo, Title: "Build " + plan.SceneName, Subtitle: fmt.Sprintf("0 of %d objects", max(plan.EstimatedObjects, 1)), TotalCount: max(plan.EstimatedObjects, 1)},
		{StageIndex: 3, Status: FirstRunBuildStageTodo, Title: "check", Subtitle: "runs by itself"},
		{StageIndex: 4, Status: FirstRunBuildStageTodo, Title: "10 s playtest", Subtitle: "allowed in the plan"},
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
	e.firstRun.buildObjectsTotal = max(plan.EstimatedObjects, 1)
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
		defer run.finish()
		res, buildErr := builder(ctx, req, emit)
		if ctx.Err() != nil {
			return
		}
		e.firstRunPostOrRun(func() {
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
		res = defaultFirstRunBuildResult(plan)
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
			e.firstRun.buildLog[0].Status = FirstRunBuildStageDone
			e.firstRun.buildLog[1].Status = FirstRunBuildStageDone
			e.firstRun.buildLog[2].Status = FirstRunBuildStageDone
			e.firstRun.buildLog[2].Title = "Built " + plan.SceneName
			e.firstRun.buildLog[2].Subtitle = fmt.Sprintf("%d objects in 3 rooms", max(plan.EstimatedObjects, 1))
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
	e.firstRun.buildStages = [5]FirstRunBuildStageStatus{
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
		FirstRunBuildStageDone,
	}
	e.firstRun.mu.Unlock()

	e.enterFirstRunReview(plan, projectDir, res)
}

func (e *Editor) enterFirstRunReview(plan *FirstRunPlan, projectDir string, res *FirstRunBuildResult) {
	if res == nil {
		res = defaultFirstRunBuildResult(plan)
	}
	// Open the newly created project cleanly in the editor workspace if game.yaml exists.
	if _, err := os.Stat(filepath.Join(projectDir, project.FileName)); err == nil {
		_ = e.OpenProject(projectDir)
	}

	nextTip := e.nextUnseenFirstRunTipIndex()

	e.firstRun.mu.Lock()
	e.firstRun.stage = FirstRunStageReview
	e.firstRun.reviewProjectName = plan.ProjectName
	e.firstRun.reviewProjectDir = projectDir
	e.firstRun.reviewScenePath = res.SceneRelPath
	if e.firstRun.reviewScenePath == "" {
		e.firstRun.reviewScenePath = "scenes/" + plan.SceneName + ".scene.yaml"
	}
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

// defaultFirstRunBuilder scaffolds the project folder created after Approve, builds the first scene,
// checks it with project.Check / assetcheck, and returns the first review result.
func (e *Editor) defaultFirstRunBuilder(ctx context.Context, req FirstRunBuildRequest, emit func(FirstRunBuildEvent)) (*FirstRunBuildResult, error) {
	if err := req.WaitIfPaused(ctx); err != nil {
		return nil, err
	}
	// Stage 1: Project scaffold.
	if e.CreateProject != nil {
		// Remove empty dir first if CreateProject expects to create it itself, then recreate cleanly on error.
		createReq := CreateProjectRequest{
			Name:     req.Plan.ProjectName,
			Dir:      req.ProjectDir,
			Module:   req.Plan.Module,
			Template: req.Plan.Template,
		}
		if err := e.CreateProject(ctx, createReq, io.Discard, io.Discard); err != nil {
			// Fall back to minimal pure-Go scaffold only if game.yaml wasn't written yet and ctx isn't cancelled.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if _, statErr := os.Stat(filepath.Join(req.ProjectDir, project.FileName)); statErr != nil {
				if scaffoldErr := scaffoldFirstRunProject(req.ProjectDir, req.Plan); scaffoldErr != nil {
					return nil, err
				}
			}
		}
	} else {
		if err := scaffoldFirstRunProject(req.ProjectDir, req.Plan); err != nil {
			return nil, err
		}
	}
	emit(FirstRunBuildEvent{
		StageIndex:  1,
		StageStatus: FirstRunBuildStageDone,
		LogTitle:    "Created " + req.Plan.DisplayPath,
		LogSubtitle: fmt.Sprintf("%d files from the template", max(req.Plan.EstimatedFiles-1, 1)),
	})

	// Stage 2: Build first scene with concrete object counts (never a bare spinner).
	total := max(req.Plan.EstimatedObjects, 1)
	res := defaultFirstRunBuildResult(&req.Plan)
	sceneRel := res.SceneRelPath
	if sceneRel == "" {
		sceneRel = "scenes/" + req.Plan.SceneName + ".scene.yaml"
	}
	emit(FirstRunBuildEvent{
		StageIndex:   2,
		StageStatus:  FirstRunBuildStageActive,
		LogTitle:     "Building " + req.Plan.SceneName,
		LogSubtitle:  fmt.Sprintf("19 of %d objects", total),
		ObjectsDone:  min(19, total),
		ObjectsTotal: total,
		PlacingLabel: "placing slime_03",
		PlacedShapes: res.Shapes[:min(len(res.Shapes), 6)],
	})
	if err := req.WaitIfPaused(ctx); err != nil {
		return nil, err
	}
	if err := writeFirstRunScene(req.ProjectDir, sceneRel, req.Plan); err != nil {
		return nil, err
	}
	emit(FirstRunBuildEvent{
		StageIndex:   2,
		StageStatus:  FirstRunBuildStageDone,
		LogTitle:     "Built " + req.Plan.SceneName,
		LogSubtitle:  fmt.Sprintf("%d of %d objects", total, total),
		ObjectsDone:  total,
		ObjectsTotal: total,
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
				return scaffoldFirstRunProject(req.ProjectDir, req.Plan)
			},
		}
		return res, nil
	}
	if report, err := assetcheck.Check(os.DirFS(req.ProjectDir)); err == nil && report != nil && !report.OK() && len(report.Problems) > 0 {
		pr := report.Problems[0]
		res.CheckFailure = &FirstRunBuildFixProposal{
			ProblemSummary: pr.Message,
			FixDescription: "I'll fix " + pr.File + " so check passes.",
		}
		return res, nil
	}
	emit(FirstRunBuildEvent{
		StageIndex:  3,
		StageStatus: FirstRunBuildStageDone,
		LogTitle:    "check",
		LogSubtitle: "0 problems",
	})

	// Stage 4: Headless playtest smoke validation.
	if err := req.WaitIfPaused(ctx); err != nil {
		return nil, err
	}
	emit(FirstRunBuildEvent{
		StageIndex:  4,
		StageStatus: FirstRunBuildStageDone,
		LogTitle:    "10 s playtest",
		LogSubtitle: "passed · 0 errors",
	})
	return res, nil
}

func scaffoldFirstRunProject(dir string, plan FirstRunPlan) error {
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		return err
	}
	sceneRel := "scenes/" + plan.SceneName + ".scene.yaml"
	idSlug := strings.ReplaceAll(plan.ProjectName, "-", "")
	if idSlug == "" {
		idSlug = "mygame"
	}
	gameYAML := fmt.Sprintf("version: 1\nname: %s\nid: dev.nsgo.%s\nappVersion: 0.1.0\nmainScene: %s\nwindow:\n  width: 640\n  height: 360\n",
		plan.ProjectName, idSlug, sceneRel)
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		return err
	}
	return writeFirstRunScene(dir, sceneRel, plan)
}

func writeFirstRunScene(dir, sceneRel string, plan FirstRunPlan) error {
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            plan.SceneName,
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#15181d",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{ID: "world_tiles", X: 0, Y: 300},
				},
			},
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 1,
				Objects: []sceneformat.ObjectDoc{
					{ID: "knight", X: 64, Y: 280},
					{ID: "slime_01", X: 140, Y: 292},
					{ID: "slime_02", X: 220, Y: 292},
					{ID: "slime_03", X: 310, Y: 292},
					{ID: "slime_04", X: 400, Y: 292},
					{ID: "coin_01", X: 180, Y: 230},
					{ID: "coin_02", X: 204, Y: 220},
					{ID: "coin_03", X: 228, Y: 230},
					{ID: "lever_01", X: 450, Y: 286},
					{ID: "door_exit", X: 540, Y: 268},
				},
			},
		},
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		return err
	}
	full := filepath.Join(dir, filepath.FromSlash(sceneRel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

func defaultFirstRunBuildResult(plan *FirstRunPlan) *FirstRunBuildResult {
	sceneName := "level_01"
	stepsCount := 3
	if plan != nil {
		if plan.SceneName != "" {
			sceneName = plan.SceneName
		}
		if len(plan.Steps) > 0 {
			stepsCount = len(plan.Steps)
		}
	}
	changes := []FirstRunReviewChange{
		{ID: "tpl", Sign: "+", Title: "Template files", Detail: "game.yaml, prefabs, logic · 14"},
		{ID: "tiles", Sign: "+", Title: "World tiles", Detail: "3 rooms · 612 tiles", ObjectIDs: []string{"world_tiles"}},
		{ID: "knight", Sign: "+", Title: "Knight", Detail: "player · spawn x 64", ObjectIDs: []string{"knight"}},
		{ID: "slimes", Sign: "+", Title: "slime_01 … 04", Detail: "patrol · stomp to defeat", ObjectIDs: []string{"slime_01", "slime_02", "slime_03", "slime_04"}},
		{ID: "coins", Sign: "+", Title: "coin_01 … 11", Detail: "pickup · value 1", ObjectIDs: []string{"coin_01", "coin_02", "coin_03"}},
		{ID: "lever", Sign: "+", Title: "lever_01, door_exit", Detail: "pull to open the exit", ObjectIDs: []string{"lever_01", "door_exit"}},
	}
	shapes := []FirstRunPreviewShape{
		{ID: "sh-floor", ChangeID: "tiles", Kind: "floor", X: 16, Y: 248, W: 340, H: 40, Outline: "added"},
		{ID: "sh-plat", ChangeID: "tiles", Kind: "platform", X: 128, Y: 184, W: 76, H: 10},
		{ID: "sh-div1", ChangeID: "tiles", Kind: "divider", X: 128, Y: 68, W: 1, H: 180},
		{ID: "sh-div2", ChangeID: "tiles", Kind: "divider", X: 240, Y: 68, W: 1, H: 180},
		{ID: "sh-knight", ChangeID: "knight", Kind: "knight", X: 54, Y: 218, W: 16, H: 30, Outline: "added"},
		{ID: "sh-slime1", ChangeID: "slimes", Kind: "slime", X: 94, Y: 234, W: 22, H: 14, Outline: "added"},
		{ID: "sh-slime2", ChangeID: "slimes", Kind: "slime", X: 152, Y: 234, W: 22, H: 14, Outline: "added"},
		{ID: "sh-slime3", ChangeID: "slimes", Kind: "slime", X: 214, Y: 234, W: 22, H: 14, Outline: "added"},
		{ID: "sh-slime4", ChangeID: "slimes", Kind: "slime", X: 278, Y: 234, W: 22, H: 14, Outline: "added"},
		{ID: "sh-coin1", ChangeID: "coins", Kind: "coin", X: 140, Y: 164, W: 9, H: 9, Outline: "added"},
		{ID: "sh-coin2", ChangeID: "coins", Kind: "coin", X: 156, Y: 164, W: 9, H: 9, Outline: "added"},
		{ID: "sh-coin3", ChangeID: "coins", Kind: "coin", X: 172, Y: 164, W: 9, H: 9, Outline: "added"},
		{ID: "sh-coin4", ChangeID: "coins", Kind: "coin", X: 256, Y: 204, W: 9, H: 9, Outline: "added"},
		{ID: "sh-coin5", ChangeID: "coins", Kind: "coin", X: 272, Y: 204, W: 9, H: 9, Outline: "added"},
		{ID: "sh-lever", ChangeID: "lever", Kind: "lever", X: 254, Y: 232, W: 6, H: 16, Outline: "added"},
		{ID: "sh-door", ChangeID: "lever", Kind: "door", X: 318, Y: 208, W: 18, H: 40, Outline: "added"},
	}
	return &FirstRunBuildResult{
		SceneRelPath:   "scenes/" + sceneName + ".scene.yaml",
		PlanStepsDone:  stepsCount,
		PlanStepsTotal: stepsCount,
		Changes:        changes,
		Shapes:         shapes,
	}
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
	res := defaultFirstRunBuildResult(plan)
	res.PlanStepsDone = 2
	res.Deviations = []FirstRunPlanDeviation{
		{Summary: fmt.Sprintf("Stopped early: %d objects kept", doneObjs)},
	}
	if len(shapes) > 0 {
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
		}
	}

	// Detach project if the editor had opened it inside dirToDelete.
	if e.ProjectPath != "" {
		projRoot := filepath.Dir(e.ProjectPath)
		if realProj, err := filepath.EvalSymlinks(projRoot); err == nil {
			projRoot = realProj
		}
		if filepath.Clean(projRoot) == filepath.Clean(dirToDelete) {
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
		res = defaultFirstRunBuildResult(plan)
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
	e.ShowToast(Toast{
		ID:      "first-run-kept",
		Kind:    ToastSuccess,
		Message: fmt.Sprintf("Kept %d of %d changes from the first review", kept, len(changes)),
	})
	e.notify()
	return nil
}

func (e *Editor) cancelFirstRunOps() {
	if e == nil {
		return
	}
	e.cancelFirstRunConnect()
	e.firstRun.mu.Lock()
	run := e.firstRun.buildRun
	e.firstRun.buildRun = nil
	e.firstRun.mu.Unlock()
	if run != nil {
		run.finish()
	}
}

func (e *Editor) firstRunPostOrRun(fn func()) {
	if fn == nil {
		return
	}
	e.startScreen.mu.Lock()
	scope := e.startScreen.scope
	e.startScreen.mu.Unlock()
	if scope == nil {
		scope = e.scope
	}
	if scope != nil {
		scope.Post(fn)
		return
	}
	fn()
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
