package editor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/editor/keychain"
)

const (
	// BuiltInFormatAnthropic selects Anthropic Messages requests.
	BuiltInFormatAnthropic = "anthropic"
	// BuiltInFormatOpenAI selects OpenAI-compatible Chat Completions requests.
	BuiltInFormatOpenAI = "openai"
	// AgentAPIKeySecretAccount is the account name under NSGo's core.Secrets where the built-in agent's API key is stored.
	// It is never written to project files, EditorSettings YAML, or logs.
	AgentAPIKeySecretAccount = "agent.anthropic.api_key"

	// DefaultBuiltInAgentEndpoint is the default Anthropic Messages API endpoint used by the built-in agent.
	DefaultBuiltInAgentEndpoint = "https://api.anthropic.com/v1/messages"

	// DefaultBuiltInModel is the default model used for "Anthropic API · default".
	DefaultBuiltInModel = "claude-sonnet-4-6"

	// DefaultBuiltInFastModel is the default model used for "Anthropic API · fast".
	DefaultBuiltInFastModel = "claude-haiku-4-5"

	builtInMaxToolIterations          = 24
	builtInMaxResponseBytes           = 8 << 20
	builtInMaxToolResultChars         = 256 * 1024
	builtInMaxArchivedToolResultChars = 8 * 1024
	builtInMaxToolImageBytes          = 8 << 20
	builtInMaxHistoryTurns            = 16
	builtInMaxHistoryBytes            = 256 * 1024
	builtInMaxTrackedThreads          = 32
	maxQueuedAgentRequests            = 64
	maxModelErrorMessageRunes         = 240
	anthropicAPIVersion               = "2023-06-01"
)

var (
	// ErrAgentLimitReached is returned when a request cannot run immediately because the weekly request limit has been reached.
	ErrAgentLimitReached = errors.New("editor: weekly agent request limit reached")

	// ErrAgentAPIKeyMissing is returned when the built-in agent has no API key stored in NSGo secrets or the system keychain.
	ErrAgentAPIKeyMissing = errors.New("editor: built-in agent API key is not configured in system keychain")

	// ErrBuiltInEndpointInsecure is returned when BuiltInAgentEndpoint is not an https:// URL (or loopback http:// URL for local testing).
	ErrBuiltInEndpointInsecure = errors.New("editor: built-in agent endpoint must use https:// (or loopback http://)")

	// ErrBuiltInFormatUnsupported is returned when agent.builtInFormat names a protocol the built-in runner cannot speak yet (currently openai); the runner refuses instead of sending Anthropic Messages requests to an OpenAI-compatible server.
	ErrBuiltInFormatUnsupported = errors.New("editor: built-in agent format is not supported yet")

	// errBuiltInNonRetryable marks client/protocol errors (oversized body, invalid JSON) that must not be retried.
	errBuiltInNonRetryable = errors.New("non-retryable model error")
)

// AgentModelRetryDelays is the exponential backoff schedule used when a model endpoint returns a retryable error (429, 5xx, 529 overloaded):
// try 1 = 4 s, try 2 = 8 s, try 3 = 16 s, try 4 = 32 s (matching design/editor/AgentStates.dc.html: "Retrying in 8 s · try 2 of 4").
var AgentModelRetryDelays = []time.Duration{
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	32 * time.Second,
}

// AgentSecretStore matches NSGo's core.Secrets interface (Get/Set/Delete by account) so the editor uses cx.Secrets() directly in apps and nsgotest, or a test store when headless.
type AgentSecretStore interface {
	Get(account string) (string, bool, error)
	Set(account, value string) error
	Delete(account string) error
}

// MemorySecretStore is an in-memory AgentSecretStore for headless tests without a core.Scope.
type MemorySecretStore struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewMemorySecretStore returns an initialized MemorySecretStore optionally seeded with values.
func NewMemorySecretStore(initial map[string]string) *MemorySecretStore {
	m := &MemorySecretStore{values: make(map[string]string, len(initial))}
	for k, v := range initial {
		m.values[k] = v
	}
	return m
}

// Get returns the secret stored under account.
func (m *MemorySecretStore) Get(account string) (string, bool, error) {
	if m == nil {
		return "", false, errors.New("editor: secret store unavailable")
	}
	m.mu.RLock()
	v, ok := m.values[account]
	m.mu.RUnlock()
	return v, ok, nil
}

// Set stores value under account.
func (m *MemorySecretStore) Set(account, value string) error {
	if m == nil {
		return errors.New("editor: secret store unavailable")
	}
	m.mu.Lock()
	if m.values == nil {
		m.values = make(map[string]string)
	}
	m.values[account] = value
	m.mu.Unlock()
	return nil
}

// Delete removes account from the store.
func (m *MemorySecretStore) Delete(account string) error {
	if m == nil {
		return errors.New("editor: secret store unavailable")
	}
	m.mu.Lock()
	delete(m.values, account)
	m.mu.Unlock()
	return nil
}

// String redacts stored secret values so they can never leak into logs or test output.
func (m *MemorySecretStore) String() string {
	if m == nil {
		return "MemorySecretStore(nil)"
	}
	m.mu.RLock()
	n := len(m.values)
	m.mu.RUnlock()
	return fmt.Sprintf("MemorySecretStore{count:%d, values:[REDACTED]}", n)
}

// GoString redacts stored secret values.
func (m *MemorySecretStore) GoString() string { return m.String() }

// AgentToolSpec describes one editor MCP tool offered to the built-in agent model.
type AgentToolSpec struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// AgentToolCallResult holds the text, optional PNG, and error flag returned by executing one editor MCP tool.
type AgentToolCallResult struct {
	Text    string
	PNG     []byte
	IsError bool
}

// AgentToolExecutor lists and invokes the editor's MCP tools for the built-in agent.
// editormcp.Server wires itself onto Editor.AgentTools automatically in editormcp.New.
type AgentToolExecutor interface {
	ListTools(ctx context.Context) ([]AgentToolSpec, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (AgentToolCallResult, error)
}

// AgentToolInstructionsProvider is an optional interface implemented by an AgentToolExecutor
// (such as editormcp.Server's bridge) to supply the editor MCP server's instructions for the
// built-in agent's system prompt.
type AgentToolInstructionsProvider interface {
	Instructions() string
}

// AgentTaskKind identifies one row of the WHO DOES WHAT routing table on Settings › Models and connections.
// Its string values match AgentTaskRoutesDefs keys persisted in EditorSettings (editor.yaml).
type AgentTaskKind string

const (
	// AgentTaskSceneEdit routes general scene and data edit requests.
	AgentTaskSceneEdit AgentTaskKind = "scene_edits"
	// AgentTaskPlaytest routes playtest replay and heatmap analysis requests.
	AgentTaskPlaytest AgentTaskKind = "playtest_analysis"
	// AgentTaskProblems routes Problems panel explanation and fix proposals.
	AgentTaskProblems AgentTaskKind = "problems_fixes"
	// AgentTaskAskInPlace routes quick in-place (⌘L) requests.
	AgentTaskAskInPlace AgentTaskKind = "ask_in_place"
	// AgentTaskArtSound routes sprite, texture, and sound generation requests.
	AgentTaskArtSound AgentTaskKind = "art_and_sound"
)

// AllAgentTaskKinds lists the WHO DOES WHAT rows in the canonical order of design/editor/Settings.dc.html.
var AllAgentTaskKinds = []AgentTaskKind{
	AgentTaskSceneEdit,
	AgentTaskPlaytest,
	AgentTaskProblems,
	AgentTaskAskInPlace,
	AgentTaskArtSound,
}

// Valid reports whether k is a recognized WHO DOES WHAT task kind.
func (k AgentTaskKind) Valid() bool {
	switch k {
	case AgentTaskSceneEdit, AgentTaskPlaytest, AgentTaskProblems, AgentTaskAskInPlace, AgentTaskArtSound:
		return true
	default:
		return false
	}
}

// Label returns the human-readable task title shown in model error cards and errors.
func (k AgentTaskKind) Label() string {
	switch k {
	case AgentTaskSceneEdit:
		return "Scene and data edits"
	case AgentTaskPlaytest:
		return "Playtest analysis"
	case AgentTaskProblems:
		return "Problems and fixes"
	case AgentTaskAskInPlace:
		return "Ask in place (⌘L)"
	case AgentTaskArtSound:
		return "Art and sound"
	default:
		return "Scene and data edits"
	}
}

// AgentModelRoute classifies which execution backend handles a WHO DOES WHAT route label.
type AgentModelRoute string

const (
	// AgentRouteLocal routes work to the connected local MCP agent (for example Claude Code or Codex).
	AgentRouteLocal AgentModelRoute = "local"
	// AgentRouteBuiltInDefault routes work to the built-in Anthropic API default model.
	AgentRouteBuiltInDefault AgentModelRoute = "builtin_default"
	// AgentRouteBuiltInFast routes work to the built-in Anthropic API fast model.
	AgentRouteBuiltInFast AgentModelRoute = "builtin_fast"
	// AgentRouteNone leaves a task kind unassigned (for example Art and sound when no generator server is connected).
	AgentRouteNone AgentModelRoute = "none"
)

// Valid reports whether r is a recognized task route.
func (r AgentModelRoute) Valid() bool {
	switch r {
	case AgentRouteLocal, AgentRouteBuiltInDefault, AgentRouteBuiltInFast, AgentRouteNone:
		return true
	default:
		return false
	}
}

// IsBuiltIn reports whether r uses the built-in API key runner rather than a local MCP CLI agent.
func (r AgentModelRoute) IsBuiltIn() bool {
	return r == AgentRouteBuiltInDefault || r == AgentRouteBuiltInFast
}

// AgentQueuedRequest is a prompt queued while offline or while the weekly request limit is reached (waiting for explicit user send).
type AgentQueuedRequest struct {
	ID       string
	Task     AgentTaskKind
	Prompt   string
	QueuedAt time.Time
	Offline  bool
}

// AgentModelErrorState describes a live model API error card (such as HTTP 529 overloaded) with retry progress and an optional switch to another connected agent.
type AgentModelErrorState struct {
	Provider         string
	StatusCode       int
	Headline         string
	Message          string
	RetryDelay       time.Duration
	RetryAttempt     int
	RetryTotal       int
	Retrying         bool
	Task             AgentTaskKind
	AlternativeAgent string
	AlternativeRoute AgentModelRoute
}

// Body formats the model error card body text matching design/editor/AgentStates.dc.html,
// for example "Retrying in 8 s · try 2 of 4. Playtest analysis can switch to Claude Code for now."
func (m AgentModelErrorState) Body() string {
	var parts []string
	if m.Retrying && m.RetryTotal > 0 {
		secs := int(m.RetryDelay.Round(time.Second) / time.Second)
		if secs < 1 {
			secs = 1
		}
		parts = append(parts, fmt.Sprintf("Retrying in %d s · try %d of %d.", secs, m.RetryAttempt, m.RetryTotal))
	} else if m.RetryTotal > 0 {
		parts = append(parts, fmt.Sprintf("Failed after %d of %d tries.", m.RetryAttempt, m.RetryTotal))
	} else if strings.TrimSpace(m.Message) != "" {
		parts = append(parts, strings.TrimSpace(m.Message))
	}
	if m.AlternativeAgent != "" && m.AlternativeRoute != "" && m.AlternativeRoute != AgentRouteNone {
		taskLabel := m.Task.Label()
		if taskLabel == "" {
			taskLabel = "This work"
		}
		parts = append(parts, fmt.Sprintf("%s can switch to %s for now.", taskLabel, m.AlternativeAgent))
	}
	return strings.Join(parts, " ")
}

// ResolveAgentModelRoute resolves the configured WHO DOES WHAT route label for task into an execution route.
func (e *Editor) ResolveAgentModelRoute(task AgentTaskKind) AgentModelRoute {
	if !task.Valid() {
		task = AgentTaskSceneEdit
	}
	label := strings.TrimSpace(e.AgentTaskRoute(string(task)))
	if label == "" || strings.EqualFold(label, "Not connected") {
		return AgentRouteNone
	}
	provider := DefaultBuiltInProviderName
	if e != nil {
		provider = e.BuiltInAgentProvider()
	}
	lower := strings.ToLower(label)
	provLower := strings.ToLower(provider)
	isBuiltInPrefix := strings.HasPrefix(lower, provLower) || strings.HasPrefix(lower, strings.ToLower(DefaultBuiltInProviderName))
	if isBuiltInPrefix {
		if strings.Contains(lower, "fast") {
			return AgentRouteBuiltInFast
		}
		return AgentRouteBuiltInDefault
	}
	return AgentRouteLocal
}

func (e *Editor) agentRouteDisplayLabel(route AgentModelRoute) string {
	provider := DefaultBuiltInProviderName
	if e != nil {
		provider = e.BuiltInAgentProvider()
	}
	switch route {
	case AgentRouteLocal:
		name := "Claude Code"
		if c, ok := e.AgentPrimaryClient(); ok && strings.TrimSpace(c.DisplayName) != "" {
			name = c.DisplayName
		}
		return name + " · default"
	case AgentRouteBuiltInDefault:
		return provider + " · default"
	case AgentRouteBuiltInFast:
		return provider + " · fast"
	default:
		return "Not connected"
	}
}

// AgentBuiltInModelForRoute returns the model ID for route: agent.builtInModel or agent.builtInFastModel from editor settings when set, otherwise DefaultBuiltInModel or DefaultBuiltInFastModel.
func (e *Editor) AgentBuiltInModelForRoute(route AgentModelRoute) string {
	if e != nil && e.settings.Agent != nil {
		if route == AgentRouteBuiltInFast && strings.TrimSpace(e.settings.Agent.BuiltInFastModel) != "" {
			return strings.TrimSpace(e.settings.Agent.BuiltInFastModel)
		}
		if route != AgentRouteBuiltInFast && strings.TrimSpace(e.settings.Agent.BuiltInModel) != "" {
			return strings.TrimSpace(e.settings.Agent.BuiltInModel)
		}
	}
	if route == AgentRouteBuiltInFast {
		return DefaultBuiltInFastModel
	}
	return DefaultBuiltInModel
}

// AgentBuiltInEndpointForSettings returns agent.builtInEndpoint from editor settings when set, falling back to Editor.BuiltInAgentEndpoint and then DefaultBuiltInAgentEndpoint.
func (e *Editor) AgentBuiltInEndpointForSettings() string {
	if e != nil && e.settings.Agent != nil && strings.TrimSpace(e.settings.Agent.BuiltInEndpoint) != "" {
		return strings.TrimSpace(e.settings.Agent.BuiltInEndpoint)
	}
	if e != nil && strings.TrimSpace(e.BuiltInAgentEndpoint) != "" {
		return strings.TrimSpace(e.BuiltInAgentEndpoint)
	}
	return DefaultBuiltInAgentEndpoint
}

// AgentBuiltInFormatForSettings returns the configured protocol, defaulting to Anthropic Messages.
func (e *Editor) AgentBuiltInFormatForSettings() string {
	if e != nil && e.settings.Agent != nil && strings.TrimSpace(e.settings.Agent.BuiltInFormat) != "" {
		return strings.TrimSpace(e.settings.Agent.BuiltInFormat)
	}
	return BuiltInFormatAnthropic
}

func nextWeeklyReset(now time.Time) time.Time {
	// Next Monday 00:00 in now's location (aligned with weekStartISO in agent_settings.go).
	y, m, d := now.Date()
	loc := now.Location()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, loc)
	daysUntilMonday := (int(time.Monday) - int(now.Weekday()) + 7) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	return midnight.AddDate(0, 0, daysUntilMonday)
}

// AgentUsageResetAt returns the next weekly usage reset boundary (Monday 00:00 in the editor's clock location).
func (e *Editor) AgentUsageResetAt() time.Time {
	if e == nil {
		return nextWeeklyReset(time.Now())
	}
	return nextWeeklyReset(e.nowTime())
}

// AgentUsageResetLabel returns the formatted reset time shown in the Limit reached chip (for example "00:00").
func (e *Editor) AgentUsageResetLabel() string {
	return e.AgentUsageResetAt().Format("15:04")
}

// AgentWeeklyLimitReached reports whether a finite weekly request limit is set and has been reached for the current week.
func (e *Editor) AgentWeeklyLimitReached() bool {
	if e == nil {
		return false
	}
	limit := e.WeeklyRequestLimit()
	if limit <= 0 {
		return false
	}
	return e.AgentUsage().WeeklyRequests >= limit
}

func (e *Editor) refreshAgentLimitStatus() {
	if e == nil || e.agent.mode == AgentModeNone || e.agent.offline {
		return
	}
	if e.AgentWeeklyLimitReached() {
		if e.agent.activeRunCancel == nil {
			e.agent.status = AgentStatusLimitReached
			e.updateAgentAnnouncement()
		}
		return
	}
	if e.agent.status == AgentStatusLimitReached {
		e.restoreIdleAgentStatus()
	}
}

// CheckAgentUsageReset checks whether the weekly reset boundary has passed and clears Limit reached if so.
func (e *Editor) CheckAgentUsageReset() {
	if e == nil {
		return
	}
	prevReached := e.agent.status == AgentStatusLimitReached
	e.refreshAgentLimitStatus()
	if prevReached != (e.agent.status == AgentStatusLimitReached) {
		e.notify()
	}
}

// AgentQueuedRequests returns a copy of requests waiting in the Agent panel queue.
func (e *Editor) AgentQueuedRequests() []AgentQueuedRequest {
	if e == nil || len(e.agent.queued) == 0 {
		return nil
	}
	return slices.Clone(e.agent.queued)
}

// RemoveQueuedAgentRequest removes one queued request by ID.
func (e *Editor) RemoveQueuedAgentRequest(id string) bool {
	if e == nil {
		return false
	}
	for i, q := range e.agent.queued {
		if q.ID == id {
			e.agent.queued = append(e.agent.queued[:i], e.agent.queued[i+1:]...)
			e.notify()
			return true
		}
	}
	return false
}

// SendQueuedAgentRequest dispatches one queued request once back online and below the weekly limit.
// While offline or while the weekly limit is still reached, it returns an error and leaves the item queued.
func (e *Editor) SendQueuedAgentRequest(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentOffline() {
		return errors.New("editor: cannot send queued request while offline")
	}
	e.refreshAgentLimitStatus()
	if e.AgentWeeklyLimitReached() {
		return ErrAgentLimitReached
	}
	idx := -1
	for i, q := range e.agent.queued {
		if q.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("editor: queued request %q not found", id)
	}
	q := e.agent.queued[idx]
	e.agent.queued = append(e.agent.queued[:idx], e.agent.queued[idx+1:]...)
	if err := e.SubmitAgentTaskPrompt(q.Task, q.Prompt); err != nil {
		// Restore on failure so the user does not lose their queued prompt.
		pos := min(idx, len(e.agent.queued))
		e.agent.queued = slices.Insert(e.agent.queued, pos, q)
		e.notify()
		return err
	}
	e.notify()
	return nil
}

// resolveRouteAndRunner picks the effective route, runner, display agent name, and model ID for task.
func (e *Editor) resolveRouteAndRunner(task AgentTaskKind) (route AgentModelRoute, runner AgentRunner, agentName, model string, err error) {
	route = e.ResolveAgentModelRoute(task)
	hasLocal := len(e.agent.clients) > 0 || e.AgentRunner != nil
	hasBuiltIn := e.HasAgentAPIKey(DefaultAgentAPIKeyAccount)

	// If the user explicitly configured API key mode and no external MCP agent is connected,
	// route tasks that default to local seamlessly through the built-in API key agent.
	if route == AgentRouteLocal && !hasLocal && (e.agent.mode == AgentModeAPIKey || hasBuiltIn) {
		route = AgentRouteBuiltInDefault
	}
	if route == AgentRouteNone {
		if e.agent.mode == AgentModeAPIKey && hasBuiltIn {
			route = AgentRouteBuiltInDefault
		} else if hasLocal {
			route = AgentRouteLocal
		} else {
			return AgentRouteNone, nil, "", "", errors.New("editor: no agent or generator is connected for " + task.Label())
		}
	}

	if route.IsBuiltIn() {
		model = e.AgentBuiltInModelForRoute(route)
		builtInRunner, buildErr := e.newBuiltInAgentRunner()
		if buildErr != nil {
			return route, nil, "", "", buildErr
		}
		return route, builtInRunner, e.BuiltInAgentProvider(), model, nil
	}

	// Local MCP / external CLI runner.
	runner = e.AgentRunner
	if runner == nil {
		if len(e.agent.clients) == 0 && e.agent.mode != AgentModeLocal {
			e.OpenConnectDialog()
			return route, nil, "", "", errors.New("editor: no agent connected")
		}
		runner = defaultCLIAgentRunner()
	}
	if c, ok := e.AgentPrimaryClient(); ok {
		agentName = c.DisplayName
	}
	return route, runner, agentName, "", nil
}

// alternativeAgentForRoute returns another connected agent that can take over if the active route fails (e.g. HTTP 529).
func (e *Editor) alternativeAgentForRoute(failedRoute AgentModelRoute) (displayName string, altRoute AgentModelRoute) {
	if e == nil {
		return "", AgentRouteNone
	}
	if failedRoute.IsBuiltIn() {
		if c, ok := e.AgentPrimaryClient(); ok && len(e.agent.clients) > 0 {
			return c.DisplayName, AgentRouteLocal
		}
		if e.AgentRunner != nil {
			if c, ok := e.AgentPrimaryClient(); ok && c.DisplayName != "" {
				return c.DisplayName, AgentRouteLocal
			}
			return "Claude Code", AgentRouteLocal
		}
		return "", AgentRouteNone
	}
	if e.HasAgentAPIKey(DefaultAgentAPIKeyAccount) {
		return e.BuiltInAgentProvider(), AgentRouteBuiltInDefault
	}
	return "", AgentRouteNone
}

// SubmitAgentTaskPrompt submits prompt for task, routing according to WHO DOES WHAT.
// When the weekly request limit is reached, the prompt is queued instead of executed and status shows Limit reached.
func (e *Editor) SubmitAgentTaskPrompt(task AgentTaskKind, prompt string) error {
	return e.submitAgentTaskPromptInternal(task, prompt, false)
}

func (e *Editor) submitAgentTaskPromptInternal(task AgentTaskKind, prompt string, skipUsageIncrement bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		return errors.New("editor: agent is disabled")
	}
	if !task.Valid() {
		task = AgentTaskSceneEdit
	}
	fromComposer := strings.TrimSpace(prompt) == ""
	text := strings.TrimSpace(prompt)
	if fromComposer {
		text = strings.TrimSpace(e.composerDraft)
		if text == "" {
			text = strings.TrimSpace(e.agent.composerText)
		}
	}
	if text == "" {
		return nil
	}
	if e.agent.offline {
		e.queueAgentTaskRequest(task, prompt, true)
		return nil
	}
	if e.agent.activeRunCancel != nil {
		return errors.New("editor: an agent request is already running")
	}

	// Enforce the weekly limit before starting any request; reaching it queues the request instead of running it.
	if !skipUsageIncrement && e.AgentWeeklyLimitReached() {
		if len(e.agent.queued) >= maxQueuedAgentRequests {
			return fmt.Errorf("editor: request queue is full (%d queued requests)", maxQueuedAgentRequests)
		}
		if fromComposer || text == strings.TrimSpace(e.composerDraft) || text == strings.TrimSpace(e.agent.composerText) {
			e.composerDraft = ""
			e.agent.composerText = ""
			e.agent.composerHighlighted = false
			e.agentComposer.pickerOpen = false
			e.agentComposer.pickerOpenedByAt = false
			e.agentComposer.pickerQuery = ""
			e.agentComposer.pickerActiveIndex = 0
			e.agentComposer.hoveredChipID = ""
		}
		e.agent.queueSeq++
		e.agent.queued = append(e.agent.queued, AgentQueuedRequest{
			ID:       fmt.Sprintf("queued-%d", e.agent.queueSeq),
			Task:     task,
			Prompt:   text,
			QueuedAt: e.nowTime(),
		})
		e.agent.status = AgentStatusLimitReached
		e.updateAgentAnnouncement()
		e.notify()
		return nil
	}

	route, runner, clientName, model, err := e.resolveRouteAndRunner(task)
	if err != nil {
		return err
	}

	attachedImgs, briefImgs, allImgs := e.collectRequestReferenceImages()
	req := e.AssembleAgentRequest(text)
	threadID := req.ThreadID
	if threadID == "" {
		threadID = e.agent.threadID
	}
	sessionID := e.agent.sessionID
	if active := e.ActiveAgentThread(); active != nil {
		if active.ID != "" {
			threadID = active.ID
		}
		if sessionID == "" && active.SessionID != "" {
			sessionID = active.SessionID
		}
	}
	req.ThreadID = threadID
	req.SessionID = sessionID
	req.ProjectRoot = e.agentProjectRoot()
	req.ScenePath = e.ScenePath
	req.SelectedID = e.SelectedID
	req.Permissions = e.agent.permissions
	req.Task = task
	req.Route = route
	req.Model = model
	req.PriorTurns = e.agentPriorTurnSummaries()
	req.AttachedImages = attachedImgs
	req.BriefImages = briefImgs
	req.Images = allImgs

	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	e.closeLiveStopChLocked()
	if fromComposer || text == strings.TrimSpace(e.composerDraft) || text == strings.TrimSpace(e.agent.composerText) {
		e.composerDraft = ""
		e.agent.composerText = ""
	}
	e.agent.attachedImages = nil
	e.agent.attachErr = ""
	e.agent.previewImageID = ""
	e.agentComposer.pickerOpen = false
	e.agentComposer.pickerOpenedByAt = false
	e.agentComposer.pickerQuery = ""
	e.agentComposer.pickerActiveIndex = 0
	e.agentComposer.hoveredChipID = ""
	e.agent.composerHighlighted = false
	e.agent.rewindOpenTurnID = ""
	e.agent.lastRewind = nil

	sessionBefore := sessionID
	cp := e.captureTurnStartCheckpoint()
	e.agent.turnSeq++
	e.agent.turnChangeSeq++
	e.agent.liveRequestID++
	e.agent.liveStepSeq++
	turnID := fmt.Sprintf("turn-%d", e.agent.turnSeq)
	if cp != nil {
		if e.agent.checkpoints == nil {
			e.agent.checkpoints = make(map[string]*agentTurnCheckpoint)
		}
		e.agent.checkpoints[turnID] = cp
	}
	agentKind := ExternalAgentAuto
	if !route.IsBuiltIn() {
		if c, ok := e.AgentPrimaryClient(); ok {
			agentKind = DetectExternalAgentKind(c.Name)
		}
	}
	req.AgentKind = agentKind
	reqCopy := cloneAgentRequest(req)
	e.agentComposer.lastRequest = &reqCopy
	now := e.nowTime()
	turnImages := make([]AgentReferenceImage, len(allImgs))
	for i, img := range allImgs {
		cp := img.clone()
		// Historical turns only render thumbnails from Pixels; avoid retaining multi-MiB raw encoded bytes in RAM.
		cp.Data = nil
		turnImages[i] = cp
	}
	startHist := 0
	if e.History != nil {
		startHist = e.History.Index()
	}
	e.agent.turns = append(e.agent.turns, AgentTurn{
		ID:        turnID,
		Prompt:    text,
		Images:    turnImages,
		Source:    "panel",
		Status:    "running",
		AgentName: clientName,
		Task:      task,
		Route:     route,
		Model:     model,
		Steps: []AgentStep{{
			ID:        fmt.Sprintf("step-%d", e.agent.liveStepSeq),
			Title:     "Starting request…",
			Status:    AgentStepPlaceholder,
			StartedAt: now,
		}},
		CreatedAt:           now,
		StartedAt:           now,
		ThreadID:            threadID,
		SessionIDBefore:     sessionBefore,
		Context:             e.captureAgentTurnContext(req.ScenePath),
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

	e.agent.activeRunID++
	runID := e.agent.activeRunID
	ctx, cancel := context.WithCancel(context.Background())
	e.agent.activeRunCancel = cancel
	e.agent.activeTurnID = turnID
	e.agent.activeRetryNow = make(chan struct{}, 1)
	if e.agent.activeRetryCancel != nil {
		e.agent.activeRetryCancel()
		e.agent.activeRetryCancel = nil
	}
	e.agent.status = AgentStatusWorking
	e.agent.workingStep = 1
	e.agent.workingTotal = 0
	if !skipUsageIncrement {
		e.RecordAgentRequest(1)
	}
	e.cancelAutoSaveTimer()
	e.updateAgentAnnouncement()
	e.upsertTurnNotificationTask(AgentTask{
		ID:        turnID,
		Title:     text,
		State:     TaskRunning,
		ScenePath: e.ScenePath,
	}, e.FilePath, time.Time{})

	if builtIn, ok := runner.(*builtInAgentRunner); ok {
		builtIn.runID = runID
		builtIn.turnID = turnID
		builtIn.retryNow = e.agent.activeRetryNow
	}
	req.MCPServers = e.AgentMCPServers()

	go func() {
		runErr := runner.Run(ctx, req, func(ev AgentEvent) {
			e.postOrRun(func() {
				e.applyAgentEventForRun(runID, turnID, ev)
			})
		})
		e.postOrRun(func() {
			e.finishAgentRun(runID, turnID, runErr, ctx.Err() != nil)
		})
	}()
	return nil
}

// RetryAgentTurn immediately retries a turn waiting in model-error backoff (or re-runs a failed turn).
func (e *Editor) RetryAgentTurn(turnID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.agent.activeRunCancel != nil && e.agent.activeTurnID == turnID && e.agent.activeRetryNow != nil {
		if e.agent.activeRetryCancel != nil {
			e.agent.activeRetryCancel()
			e.agent.activeRetryCancel = nil
		}
		select {
		case e.agent.activeRetryNow <- struct{}{}:
		default:
		}
		return nil
	}
	turn := e.findAgentTurn(turnID)
	if turn == nil {
		return fmt.Errorf("editor: agent turn %q not found", turnID)
	}
	if e.agent.activeRunCancel != nil {
		return errors.New("editor: another agent request is already running")
	}
	return e.submitAgentTaskPromptInternal(turn.Task, turn.Prompt, true)
}

// SwitchAgentTurnToAlternative switches the task kind of a turn experiencing a model error to another connected agent
// (updating WHO DOES WHAT in EditorSettings) and immediately runs the request on that agent.
// Refused with ErrAgentSettingsYouOnly if invoked by an agent.
func (e *Editor) SwitchAgentTurnToAlternative(turnID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAgentSettingsYouOnly
	}
	turn := e.findAgentTurn(turnID)
	if turn == nil {
		return fmt.Errorf("editor: agent turn %q not found", turnID)
	}
	if turn.ModelError == nil || turn.ModelError.AlternativeRoute == "" || turn.ModelError.AlternativeRoute == AgentRouteNone {
		return errors.New("editor: no alternative connected agent available")
	}
	task := turn.Task
	if !task.Valid() {
		task = AgentTaskSceneEdit
	}
	prompt := turn.Prompt
	altRoute := turn.ModelError.AlternativeRoute
	altName := turn.ModelError.AlternativeAgent
	altLabel := e.agentRouteDisplayLabel(altRoute)

	// Cancel the retrying/failed built-in run cleanly before switching.
	if e.agent.activeRunCancel != nil {
		cancel := e.agent.activeRunCancel
		e.agent.activeRunCancel = nil
		if e.agent.activeRetryCancel != nil {
			e.agent.activeRetryCancel()
			e.agent.activeRetryCancel = nil
		}
		e.agent.activeRetryNow = nil
		e.agent.activeTurnID = ""
		e.agent.activeRunID++
		cancel()
	}
	e.removeTurnNotificationTask(turnID)
	turn.Done = true
	turn.Status = "done"
	turn.FinishedAt = e.nowTime()
	e.agent.turnChangeSeq++
	e.finalizeTurnCheckpoint(turnID)
	if turn.ModelError != nil {
		turn.ModelError.Retrying = false
	}
	if turn.Summary == "" {
		turn.Summary = fmt.Sprintf("Switched %s to %s.", task.Label(), altName)
	}

	if err := e.SetAgentTaskRoute(string(task), altLabel); err != nil {
		return err
	}
	return e.submitAgentTaskPromptInternal(task, prompt, true)
}

// --- Built-in Anthropic Messages API runner ---

type builtInAgentRunner struct {
	ed       *Editor
	provider string
	tools    AgentToolExecutor
	endpoint string
	client   *http.Client
	readKey  func() (string, error)
	runID    uint64
	turnID   string
	taskID   string
	retryNow chan struct{}
}

func (e *Editor) newBuiltInAgentRunner() (*builtInAgentRunner, error) {
	if format := e.AgentBuiltInFormatForSettings(); format != BuiltInFormatAnthropic {
		return nil, fmt.Errorf("%w: %s", ErrBuiltInFormatUnsupported, format)
	}
	endpoint, err := validateAndResolveBuiltInEndpoint(e.AgentBuiltInEndpointForSettings())
	if err != nil {
		return nil, err
	}
	client := e.BuiltInHTTPClient
	if client == nil {
		client = &http.Client{
			Timeout:       60 * time.Second,
			CheckRedirect: builtInCheckRedirect,
		}
	}
	// Capture the active secret source on the editor goroutine so the runner goroutine never touches core.Scope directly.
	var readKey func() (string, error)
	if store := e.agentSecretsStore(); store != nil {
		readKey = func() (string, error) {
			val, ok, gerr := store.Get(AgentAPIKeySecretAccount)
			if gerr != nil {
				return "", gerr
			}
			if !ok || strings.TrimSpace(val) == "" {
				if mem := strings.TrimSpace(e.FirstRunAPIKey()); mem != "" {
					return mem, nil
				}
				return "", ErrAgentAPIKeyMissing
			}
			return strings.TrimSpace(val), nil
		}
	} else {
		kc := e.activeKeychain()
		readKey = func() (string, error) {
			val, gerr := kc.Get(AgentKeychainServiceName, DefaultAgentAPIKeyAccount)
			if gerr != nil {
				if errors.Is(gerr, keychain.ErrNotFound) {
					if mem := strings.TrimSpace(e.FirstRunAPIKey()); mem != "" {
						return mem, nil
					}
					return "", ErrAgentAPIKeyMissing
				}
				return "", gerr
			}
			if strings.TrimSpace(val) == "" {
				if mem := strings.TrimSpace(e.FirstRunAPIKey()); mem != "" {
					return mem, nil
				}
				return "", ErrAgentAPIKeyMissing
			}
			return strings.TrimSpace(val), nil
		}
	}
	return &builtInAgentRunner{
		ed:       e,
		provider: e.BuiltInAgentProvider(),
		tools:    e.AgentTools,
		endpoint: endpoint,
		client:   client,
		readKey:  readKey,
	}, nil
}

func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(h)
	return err == nil && addr.IsLoopback()
}

func validateAndResolveBuiltInEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return "", fmt.Errorf("%w: invalid URL", ErrBuiltInEndpointInsecure)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: URL must not include embedded credentials", ErrBuiltInEndpointInsecure)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("%w: missing host", ErrBuiltInEndpointInsecure)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		// Allowed for any host.
	case "http":
		if !isLoopbackHost(host) {
			return "", fmt.Errorf("%w: refusing plaintext http:// to non-loopback host %q", ErrBuiltInEndpointInsecure, host)
		}
	default:
		return "", fmt.Errorf("%w: unsupported scheme %q", ErrBuiltInEndpointInsecure, u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/v1/messages"
	}
	return u.String(), nil
}

func builtInCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) >= 3 {
		return errors.New("editor: too many redirects from model endpoint")
	}
	orig := via[0].URL
	next := req.URL
	if orig == nil || next == nil {
		return ErrBuiltInEndpointInsecure
	}
	// Never forward x-api-key across hosts or downgrade https -> http.
	if !strings.EqualFold(orig.Hostname(), next.Hostname()) || orig.Port() != next.Port() {
		return errors.New("editor: refusing cross-host redirect from model endpoint")
	}
	if strings.EqualFold(orig.Scheme, "https") && !strings.EqualFold(next.Scheme, "https") {
		return errors.New("editor: refusing https-to-http downgrade redirect from model endpoint")
	}
	return nil
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Tools     []anthropicToolDef `json:"tools,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Model      string                  `json:"model"`
	StopReason string                  `json:"stop_reason"`
	Content    []anthropicContentBlock `json:"content"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicErrorEnvelope struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type modelHTTPError struct {
	Provider   string
	StatusCode int
	ErrType    string
	Message    string
	Retryable  bool
}

func (e *modelHTTPError) Error() string {
	provider := e.Provider
	if provider == "" {
		provider = DefaultBuiltInProviderName
	}
	return formatModelErrorHeadline(provider, e.StatusCode, e.ErrType, e.Message)
}

func isRetryableModelStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return true
	default:
		return false
	}
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "…"
}

func formatModelErrorHeadline(provider string, statusCode int, errType, msg string) string {
	if provider == "" {
		provider = DefaultBuiltInProviderName
	}
	if statusCode == 529 || strings.Contains(strings.ToLower(errType), "overloaded") {
		return fmt.Sprintf("%s is overloaded (%d)", provider, statusCode)
	}
	if statusCode == http.StatusTooManyRequests {
		return fmt.Sprintf("%s rate limit reached (429)", provider)
	}
	if statusCode == http.StatusUnauthorized {
		return fmt.Sprintf("%s rejected the API key (401)", provider)
	}
	if statusCode >= 500 {
		return fmt.Sprintf("%s temporary error (%d)", provider, statusCode)
	}
	if statusCode > 0 {
		if trimmed := truncateRunes(strings.TrimSpace(msg), maxModelErrorMessageRunes); trimmed != "" {
			return fmt.Sprintf("%s error (%d): %s", provider, statusCode, trimmed)
		}
		return fmt.Sprintf("%s error (%d)", provider, statusCode)
	}
	if trimmed := truncateRunes(strings.TrimSpace(msg), maxModelErrorMessageRunes); trimmed != "" {
		return fmt.Sprintf("%s request failed: %s", provider, trimmed)
	}
	return fmt.Sprintf("%s request failed", provider)
}

func redactSecret(s, secret string) string {
	if secret == "" || s == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[REDACTED]")
}

func (r *builtInAgentRunner) Run(ctx context.Context, req AgentRequest, emit func(AgentEvent)) error {
	if r.readKey == nil {
		r.markAPIKeyFailure("No API key found in the system keychain.")
		return ErrAgentAPIKeyMissing
	}
	apiKey, err := r.readKey()
	if err != nil {
		r.markAPIKeyFailure("No API key found in the system keychain.")
		if errors.Is(err, ErrAgentAPIKeyMissing) {
			return ErrAgentAPIKeyMissing
		}
		return fmt.Errorf("editor: read built-in API key from secrets: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		r.markAPIKeyFailure("No API key found in the system keychain.")
		return ErrAgentAPIKeyMissing
	}

	var toolDefs []anthropicToolDef
	if r.tools != nil {
		specs, listErr := r.tools.ListTools(ctx)
		if listErr != nil {
			return fmt.Errorf("built-in agent: list editor MCP tools: %w", listErr)
		}
		toolDefs = make([]anthropicToolDef, 0, len(specs))
		for _, spec := range specs {
			schema := spec.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			toolDefs = append(toolDefs, anthropicToolDef{
				Name:        spec.Name,
				Description: spec.Description,
				InputSchema: schema,
			})
		}
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = DefaultBuiltInModel
	}

	input := RenderAgentModelInput(req)
	serverInstructions := ""
	if provider, ok := r.tools.(AgentToolInstructionsProvider); ok {
		serverInstructions = strings.TrimSpace(provider.Instructions())
	}
	allowViewportImg := true
	if r.ed != nil {
		allowViewportImg = r.ed.AgentPrivacy().ViewportImages
	}

	threadID := strings.TrimSpace(req.ThreadID)
	if threadID == "" {
		threadID = "thread-1"
	}

	var (
		priorMessages []anthropicMessage
		historyEpoch  uint64
	)
	if r.ed != nil {
		priorMessages, historyEpoch = r.ed.snapshotBuiltInThreadHistory(threadID, req, input.HasCompactedSummary)
	} else if !input.HasCompactedSummary && len(req.PriorTurns) > 0 {
		priorMessages = buildMessagesFromPriorTurns(req.PriorTurns)
	}

	systemPrompt := buildBuiltInSystemPrompt(req, input, serverInstructions)
	messages := make([]anthropicMessage, 0, len(priorMessages)+1)
	messages = append(messages, priorMessages...)
	turnStartIdx := len(messages)
	messages = append(messages, anthropicMessage{
		Role:    "user",
		Content: buildBuiltInUserContent(input, allowViewportImg),
	})

	for iter := 0; iter < builtInMaxToolIterations; iter++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		resp, err := r.callModelWithRetries(ctx, apiKey, req, anthropicRequest{
			Model:     model,
			MaxTokens: 4096,
			System:    systemPrompt,
			Tools:     toolDefs,
			Messages:  messages,
		}, emit)
		if err != nil {
			return errors.New(redactSecret(err.Error(), apiKey))
		}

		if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
			emit(AgentEvent{
				Kind:         AgentEventUsage,
				InputTokens:  resp.Usage.InputTokens,
				OutputTokens: resp.Usage.OutputTokens,
			})
		}

		var toolUses []anthropicContentBlock
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					emit(AgentEvent{Kind: AgentEventMessage, Text: redactSecret(block.Text, apiKey)})
				}
			case "tool_use":
				toolUses = append(toolUses, block)
			}
		}

		if len(toolUses) == 0 {
			if r.ed != nil && ctx.Err() == nil {
				turnMsgs := make([]anthropicMessage, 0, len(messages)-turnStartIdx+1)
				turnMsgs = append(turnMsgs, messages[turnStartIdx:]...)
				turnMsgs = append(turnMsgs, anthropicMessage{
					Role:    "assistant",
					Content: finalAssistantContentForHistory(resp.Content),
				})
				r.ed.commitBuiltInThreadTurn(threadID, historyEpoch, r.turnID, sanitizeBuiltInTurnMessages(turnMsgs, apiKey))
			}
			emit(AgentEvent{Kind: AgentEventDone})
			return nil
		}

		// Record assistant turn containing the tool calls.
		messages = append(messages, anthropicMessage{
			Role:    "assistant",
			Content: resp.Content,
		})

		toolResults := make([]map[string]any, 0, len(toolUses))
		for _, tu := range toolUses {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			stepID := tu.ID
			if stepID == "" {
				stepID = fmt.Sprintf("tool-%d-%s", iter+1, tu.Name)
			}
			emit(AgentEvent{
				Kind:       AgentEventStep,
				StepID:     stepID,
				StepTitle:  formatAgentToolStep(tu.Name),
				ToolName:   tu.Name,
				StepStatus: AgentStepRunning,
			})

			toolCtx := ctx
			if req.TaskID != "" {
				toolCtx = WithTaskToolContext(ctx, req.TaskID)
			}
			res := r.executeToolWithPrivacy(toolCtx, tu.Name, tu.Input)
			stepStatus := AgentStepDone
			if res.IsError {
				stepStatus = AgentStepFailed
			}
			emit(AgentEvent{
				Kind:       AgentEventStep,
				StepID:     stepID,
				StepTitle:  formatAgentToolStep(tu.Name),
				ToolName:   tu.Name,
				StepStatus: stepStatus,
			})

			resultText := truncateRunes(redactSecret(res.Text, apiKey), builtInMaxToolResultChars)
			resultEntry := map[string]any{
				"type":        "tool_result",
				"tool_use_id": tu.ID,
				"is_error":    res.IsError,
			}
			if len(res.PNG) > 0 && len(res.PNG) <= builtInMaxToolImageBytes {
				blocks := []map[string]any{}
				if resultText != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": resultText})
				}
				blocks = append(blocks, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": "image/png",
						"data":       base64.StdEncoding.EncodeToString(res.PNG),
					},
				})
				resultEntry["content"] = blocks
			} else {
				if len(res.PNG) > builtInMaxToolImageBytes {
					if resultText != "" {
						resultText += "\n"
					}
					resultText += "[image omitted: exceeds maximum size]"
				}
				resultEntry["content"] = resultText
			}
			toolResults = append(toolResults, resultEntry)
		}

		messages = append(messages, anthropicMessage{
			Role:    "user",
			Content: toolResults,
		})
	}

	return errors.New("built-in agent exceeded maximum tool iterations")
}

func (r *builtInAgentRunner) markAPIKeyFailure(cause string) {
	if r.ed == nil {
		return
	}
	r.ed.postOrRun(func() {
		r.ed.agent.failedStep = ConnectStepAPIKey
		r.ed.agent.failedCause = cause
		r.ed.settingsMu.Lock()
		r.ed.agent.builtInKeyChecked = true
		r.ed.agent.builtInKeyPresent = false
		r.ed.settingsMu.Unlock()
	})
}

func (r *builtInAgentRunner) executeToolWithPrivacy(ctx context.Context, name string, args json.RawMessage) AgentToolCallResult {
	if r.tools == nil {
		return AgentToolCallResult{Text: "editor MCP tools are not connected", IsError: true}
	}
	// Defense-in-depth enforcement of Settings › Tools and Settings › Usage and privacy
	// in addition to editormcp.Server's symlink-aware privacy and permission checks.
	if r.ed != nil && !r.ed.IsMCPToolEnabled(name) {
		return AgentToolCallResult{
			Text:    fmt.Sprintf("tool %s is turned off in Settings › Tools; ask the person to enable it", name),
			IsError: true,
		}
	}
	privacy := DefaultAgentPrivacyPolicy()
	if r.ed != nil {
		privacy = r.ed.AgentPrivacy()
	}
	if name == "viewport_image" && !privacy.ViewportImages {
		return AgentToolCallResult{
			Text:    PrivacyRefusalError(PrivacyViewportImages, name).Error(),
			IsError: true,
		}
	}
	if name == "scene_read" {
		var readArgs struct {
			File string `json:"file"`
		}
		if json.Unmarshal(args, &readArgs) == nil {
			kind := PrivacySceneAndData
			if strings.TrimSpace(readArgs.File) != "" {
				kind = ClassifyProjectFilePrivacy(readArgs.File)
			}
			if !privacy.Allows(kind) {
				return AgentToolCallResult{
					Text:    PrivacyRefusalError(kind, readArgs.File).Error(),
					IsError: true,
				}
			}
		}
	}
	res, err := r.tools.CallTool(ctx, name, args)
	if err != nil {
		return AgentToolCallResult{Text: err.Error(), IsError: true}
	}
	if len(res.PNG) > 0 && !privacy.ViewportImages {
		res.PNG = nil
	}
	return res
}

func (r *builtInAgentRunner) callModelWithRetries(ctx context.Context, apiKey string, req AgentRequest, payload anthropicRequest, emit func(AgentEvent)) (*anthropicResponse, error) {
	delays := AgentModelRetryDelays
	maxRetries := len(delays)
	hadRetryError := false
	provider := r.provider
	if provider == "" {
		provider = DefaultBuiltInProviderName
	}

	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		resp, err := r.doModelRequest(ctx, apiKey, payload)
		if err == nil {
			if hadRetryError {
				emit(AgentEvent{Kind: AgentEventModelRecovered})
			}
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var httpErr *modelHTTPError
		retryable := false
		statusCode := 0
		errType := ""
		msg := redactSecret(err.Error(), apiKey)
		if errors.As(err, &httpErr) {
			retryable = httpErr.Retryable
			statusCode = httpErr.StatusCode
			errType = httpErr.ErrType
			msg = redactSecret(httpErr.Message, apiKey)
		} else if !errors.Is(err, ErrBuiltInEndpointInsecure) && !errors.Is(err, errBuiltInNonRetryable) {
			// Transient transport / network errors are retryable.
			retryable = true
		}

		headline := formatModelErrorHeadline(provider, statusCode, errType, msg)
		if statusCode == http.StatusUnauthorized {
			r.markAPIKeyFailure(headline)
		}

		if !retryable || attempt >= maxRetries {
			r.postModelError(ctx, AgentModelErrorState{
				Provider:     provider,
				StatusCode:   statusCode,
				Headline:     headline,
				Message:      truncateRunes(msg, maxModelErrorMessageRunes),
				RetryAttempt: attempt,
				RetryTotal:   maxRetries,
				Retrying:     false,
				Task:         req.Task,
			}, 0, nil)
			return nil, errors.New(headline)
		}

		// Drain any stale Retry now signal received before this error card is published.
		if r.retryNow != nil {
			select {
			case <-r.retryNow:
			default:
			}
		}

		hadRetryError = true
		delay := delays[attempt]
		timerCh := make(chan struct{}, 1)
		r.postModelError(ctx, AgentModelErrorState{
			Provider:     provider,
			StatusCode:   statusCode,
			Headline:     headline,
			Message:      truncateRunes(msg, maxModelErrorMessageRunes),
			RetryDelay:   delay,
			RetryAttempt: attempt + 1,
			RetryTotal:   maxRetries,
			Retrying:     true,
			Task:         req.Task,
		}, delay, timerCh)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.retryNow:
		case <-timerCh:
		}
	}
}

// postModelError updates the turn's ModelError state AND schedules its retry timer atomically on the editor goroutine,
// preventing any timing race between observing the error card and firing AgentScheduleAfter in tests while honoring ctx cancellation.
func (r *builtInAgentRunner) postModelError(ctx context.Context, state AgentModelErrorState, delay time.Duration, timerCh chan struct{}) {
	if r.ed == nil {
		return
	}
	done := make(chan struct{})
	r.ed.postOrRun(func() {
		defer close(done)
		if r.taskID != "" {
			turn := r.ed.findAgentTurn(r.turnID)
			if turn == nil || turn.Done {
				return
			}
			altName, altRoute := r.ed.alternativeAgentForRoute(turn.Route)
			state.AlternativeAgent = altName
			state.AlternativeRoute = altRoute
			cp := state
			turn.ModelError = &cp
			r.ed.agent.turnChangeSeq++
			if state.Headline != "" {
				_ = r.ed.UpdateTaskStep(r.taskID, 0, 0, state.Headline)
			}
			if state.Retrying && timerCh != nil {
				r.ed.scheduleAgentAfter(delay, func() {
					select {
					case timerCh <- struct{}{}:
					default:
					}
				})
			}
			r.ed.notify()
			return
		}
		if r.ed.agent.activeRunID != r.runID {
			return
		}
		turn := r.ed.findAgentTurn(r.turnID)
		if turn == nil || turn.Done {
			return
		}
		altName, altRoute := r.ed.alternativeAgentForRoute(turn.Route)
		state.AlternativeAgent = altName
		state.AlternativeRoute = altRoute
		cp := state
		turn.ModelError = &cp
		r.ed.agent.turnChangeSeq++

		if r.ed.agent.activeRetryCancel != nil {
			r.ed.agent.activeRetryCancel()
			r.ed.agent.activeRetryCancel = nil
		}
		if state.Retrying && timerCh != nil {
			runID := r.runID
			r.ed.agent.activeRetryCancel = r.ed.scheduleAgentAfter(delay, func() {
				if r.ed.agent.activeRunID != runID {
					return
				}
				r.ed.agent.activeRetryCancel = nil
				select {
				case timerCh <- struct{}{}:
				default:
				}
			})
		}
		r.ed.updateAgentAnnouncement()
		r.ed.notify()
	})
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (r *builtInAgentRunner) doModelRequest(ctx context.Context, apiKey string, payload anthropicRequest) (*anthropicResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode model request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build model request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, errors.New(redactSecret(err.Error(), apiKey))
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, builtInMaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read model response: %w", err)
	}
	if len(respBytes) > builtInMaxResponseBytes {
		return nil, fmt.Errorf("%w: model response exceeded maximum size (%d bytes)", errBuiltInNonRetryable, builtInMaxResponseBytes)
	}

	if resp.StatusCode >= 400 {
		var env anthropicErrorEnvelope
		_ = json.Unmarshal(respBytes, &env)
		msg := strings.TrimSpace(env.Error.Message)
		if msg == "" {
			msg = strings.TrimSpace(string(respBytes))
		}
		return nil, &modelHTTPError{
			Provider:   r.provider,
			StatusCode: resp.StatusCode,
			ErrType:    env.Error.Type,
			Message:    truncateRunes(redactSecret(msg, apiKey), maxModelErrorMessageRunes),
			Retryable:  isRetryableModelStatus(resp.StatusCode),
		}
	}

	var decoded anthropicResponse
	if err := json.Unmarshal(respBytes, &decoded); err != nil {
		return nil, fmt.Errorf("%w: decode model response: %v", errBuiltInNonRetryable, err)
	}
	return &decoded, nil
}

func buildBuiltInSystemPrompt(req AgentRequest, input AgentModelInput, serverInstructions string) string {
	var sections []string
	var header strings.Builder
	header.WriteString("You are the NSGo Game Editor's built-in agent. Use the editor's MCP tools to read and edit the open game project cleanly as undoable editor commands.")
	header.WriteString("\n" + agentUntrustedContentNotice)
	if req.ScenePath != "" {
		fmt.Fprintf(&header, "\nOpen scene: %s", req.ScenePath)
	}
	if req.SelectedID != "" {
		fmt.Fprintf(&header, "\nSelected object: %s", req.SelectedID)
	}
	sections = append(sections, header.String())
	if instr := strings.TrimSpace(serverInstructions); instr != "" {
		sections = append(sections, instr)
	}
	if input.AlwaysContext != "" {
		sections = append(sections, input.AlwaysContext)
	}
	return strings.Join(sections, "\n\n")
}

func buildBuiltInUserContent(input AgentModelInput, allowViewportImage bool) any {
	text := input.TurnPrompt
	if len(input.Images) == 0 {
		return text
	}
	var blocks []map[string]any
	for _, img := range input.Images {
		if img.ID == viewportImageChipID && !allowViewportImage {
			continue
		}
		mimeType := strings.TrimSpace(img.MIMEType)
		if mimeType == "" {
			mimeType = img.Format.MIMEType()
		}
		if len(img.Data) == 0 || len(img.Data) > builtInMaxToolImageBytes || mimeType == "" {
			continue
		}
		blocks = append(blocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mimeType,
				"data":       base64.StdEncoding.EncodeToString(img.Data),
			},
		})
	}
	if len(blocks) == 0 {
		return text
	}
	if text != "" {
		blocks = append(blocks, map[string]any{
			"type": "text",
			"text": text,
		})
	}
	return blocks
}

// --- Per-thread bounded message history for the built-in runner ---

type builtInTurnRecord struct {
	TurnID   string
	Messages []anthropicMessage
	Bytes    int
}

type builtInThreadHistory struct {
	Epoch     uint64
	LastUsed  uint64
	Compacted bool
	Turns     []builtInTurnRecord
}

func (e *Editor) activeBuiltInThreadIDs() []string {
	if e == nil {
		return nil
	}
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	add(e.agent.threadID)
	add(e.agentComposer.threadID)
	add(e.agentThreads.activeThreadID)
	if e.agentThreads.draftThread != nil {
		add(e.agentThreads.draftThread.ID)
	}
	return ids
}

func (e *Editor) clearBuiltInHistoryForActiveThread() {
	if e == nil {
		return
	}
	e.clearBuiltInThreadHistory(e.activeBuiltInThreadIDs()...)
}

func (e *Editor) clearBuiltInThreadHistory(threadIDs ...string) {
	if e == nil {
		return
	}
	e.agent.builtInHistoryMu.Lock()
	defer e.agent.builtInHistoryMu.Unlock()
	if e.agent.builtInHistory == nil {
		e.agent.builtInHistory = make(map[string]*builtInThreadHistory)
	}
	for _, id := range threadIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		h := e.agent.builtInHistory[id]
		if h == nil {
			h = &builtInThreadHistory{}
			e.agent.builtInHistory[id] = h
		}
		h.Epoch++
		h.Compacted = true
		h.Turns = nil
	}
}

func (e *Editor) clearAllBuiltInThreadHistory() {
	if e == nil {
		return
	}
	e.agent.builtInHistoryMu.Lock()
	for _, h := range e.agent.builtInHistory {
		h.Epoch++
		h.Turns = nil
	}
	e.agent.builtInHistory = nil
	e.agent.builtInHistoryMu.Unlock()
}

func (e *Editor) snapshotBuiltInThreadHistory(threadID string, req AgentRequest, hasSummary bool) ([]anthropicMessage, uint64) {
	if e == nil {
		return nil, 0
	}
	e.agent.builtInHistoryMu.Lock()
	defer e.agent.builtInHistoryMu.Unlock()
	if e.agent.builtInHistory == nil {
		e.agent.builtInHistory = make(map[string]*builtInThreadHistory)
	}
	h := e.agent.builtInHistory[threadID]
	if h == nil {
		h = &builtInThreadHistory{}
		e.agent.builtInHistory[threadID] = h
	}
	e.agent.builtInHistorySeq++
	h.LastUsed = e.agent.builtInHistorySeq

	if len(h.Turns) > 0 {
		var out []anthropicMessage
		for _, rec := range h.Turns {
			out = append(out, cloneAnthropicMessages(rec.Messages)...)
		}
		return out, h.Epoch
	}
	if !h.Compacted && !hasSummary && len(req.PriorTurns) > 0 {
		return buildMessagesFromPriorTurns(req.PriorTurns), h.Epoch
	}
	return nil, h.Epoch
}

func (e *Editor) commitBuiltInThreadTurn(threadID string, expectedEpoch uint64, turnID string, turnMsgs []anthropicMessage) {
	if e == nil || len(turnMsgs) == 0 {
		return
	}
	rec := builtInTurnRecord{
		TurnID:   turnID,
		Messages: turnMsgs,
		Bytes:    estimateAnthropicMessagesBytes(turnMsgs),
	}

	e.agent.builtInHistoryMu.Lock()
	defer e.agent.builtInHistoryMu.Unlock()
	if e.agent.builtInHistory == nil {
		e.agent.builtInHistory = make(map[string]*builtInThreadHistory)
	}
	h := e.agent.builtInHistory[threadID]
	if h == nil {
		if expectedEpoch != 0 {
			return
		}
		h = &builtInThreadHistory{}
		e.agent.builtInHistory[threadID] = h
	}
	if h.Epoch != expectedEpoch {
		return
	}
	e.agent.builtInHistorySeq++
	h.LastUsed = e.agent.builtInHistorySeq
	h.Compacted = false
	h.Turns = append(h.Turns, rec)
	trimBuiltInThreadTurns(h)
	evictOldBuiltInThreadsLocked(e.agent.builtInHistory, threadID)
}

func (e *Editor) rewindBuiltInThreadHistory(threadID string, droppedTurnIDs map[string]bool) []builtInTurnRecord {
	if e == nil || threadID == "" {
		return nil
	}
	e.agent.builtInHistoryMu.Lock()
	defer e.agent.builtInHistoryMu.Unlock()
	h := e.agent.builtInHistory[threadID]
	if h == nil || len(h.Turns) == 0 {
		return nil
	}
	prev := cloneBuiltInTurnRecords(h.Turns)
	kept := make([]builtInTurnRecord, 0, len(h.Turns))
	for _, rec := range h.Turns {
		if droppedTurnIDs[rec.TurnID] {
			continue
		}
		kept = append(kept, rec)
	}
	h.Turns = kept
	h.Epoch++
	return prev
}

func (e *Editor) restoreBuiltInThreadHistory(threadID string, prev []builtInTurnRecord) {
	if e == nil || threadID == "" {
		return
	}
	e.agent.builtInHistoryMu.Lock()
	defer e.agent.builtInHistoryMu.Unlock()
	if e.agent.builtInHistory == nil {
		e.agent.builtInHistory = make(map[string]*builtInThreadHistory)
	}
	h := e.agent.builtInHistory[threadID]
	if h == nil {
		h = &builtInThreadHistory{}
		e.agent.builtInHistory[threadID] = h
	}
	h.Turns = cloneBuiltInTurnRecords(prev)
	h.Compacted = false
	h.Epoch++
	trimBuiltInThreadTurns(h)
}

func trimBuiltInThreadTurns(h *builtInThreadHistory) {
	if h == nil {
		return
	}
	if len(h.Turns) > builtInMaxHistoryTurns {
		h.Turns = slices.Clone(h.Turns[len(h.Turns)-builtInMaxHistoryTurns:])
	}
	total := 0
	for _, t := range h.Turns {
		total += t.Bytes
	}
	for len(h.Turns) > 1 && total > builtInMaxHistoryBytes {
		total -= h.Turns[0].Bytes
		h.Turns = h.Turns[1:]
	}
}

func evictOldBuiltInThreadsLocked(m map[string]*builtInThreadHistory, keepID string) {
	for len(m) > builtInMaxTrackedThreads {
		var oldestID string
		var oldestSeq uint64
		first := true
		for id, h := range m {
			if id == keepID {
				continue
			}
			if first || h.LastUsed < oldestSeq {
				oldestID = id
				oldestSeq = h.LastUsed
				first = false
			}
		}
		if oldestID == "" {
			break
		}
		delete(m, oldestID)
	}
}

func buildMessagesFromPriorTurns(prior []AgentTurnSummary) []anthropicMessage {
	var turns []builtInTurnRecord
	for _, t := range prior {
		prompt := strings.TrimSpace(t.Prompt)
		if prompt == "" {
			continue
		}
		reply := strings.TrimSpace(t.Summary)
		if reply == "" && len(t.ChangedFiles) > 0 {
			reply = "Changed files: " + strings.Join(t.ChangedFiles, ", ")
		}
		if reply == "" {
			continue
		}
		msgs := []anthropicMessage{
			{Role: "user", Content: prompt},
			{Role: "assistant", Content: reply},
		}
		turns = append(turns, builtInTurnRecord{
			TurnID:   t.ID,
			Messages: msgs,
			Bytes:    estimateAnthropicMessagesBytes(msgs),
		})
	}
	h := &builtInThreadHistory{Turns: turns}
	trimBuiltInThreadTurns(h)
	out := make([]anthropicMessage, 0, len(h.Turns)*2)
	for _, rec := range h.Turns {
		out = append(out, rec.Messages...)
	}
	return out
}

func finalAssistantContentForHistory(blocks []anthropicContentBlock) any {
	var kept []anthropicContentBlock
	for _, blk := range blocks {
		if blk.Type == "text" && strings.TrimSpace(blk.Text) == "" {
			continue
		}
		kept = append(kept, blk)
	}
	if len(kept) == 0 {
		return "Done."
	}
	return kept
}

func sanitizeBuiltInTurnMessages(msgs []anthropicMessage, secret string) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, anthropicMessage{
			Role:    m.Role,
			Content: sanitizeBuiltInMessageContent(m.Content, secret),
		})
	}
	return out
}

func sanitizeBuiltInMessageContent(content any, secret string) any {
	switch c := content.(type) {
	case string:
		return redactSecret(c, secret)
	case []anthropicContentBlock:
		out := make([]anthropicContentBlock, len(c))
		for i, blk := range c {
			cp := blk
			if cp.Text != "" {
				cp.Text = redactSecret(cp.Text, secret)
			}
			if len(cp.Input) > 0 {
				cp.Input = slices.Clone(cp.Input)
			}
			out[i] = cp
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(c))
		for _, blk := range c {
			typ, _ := blk["type"].(string)
			switch typ {
			case "image":
				out = append(out, map[string]any{
					"type": "text",
					"text": "[image omitted from earlier turn]",
				})
			case "text":
				txt, _ := blk["text"].(string)
				out = append(out, map[string]any{
					"type": "text",
					"text": redactSecret(txt, secret),
				})
			case "tool_result":
				cp := map[string]any{
					"type":        "tool_result",
					"tool_use_id": blk["tool_use_id"],
					"is_error":    blk["is_error"],
				}
				switch tc := blk["content"].(type) {
				case string:
					cp["content"] = truncateRunes(redactSecret(tc, secret), builtInMaxArchivedToolResultChars)
				case []map[string]any:
					sub := make([]map[string]any, 0, len(tc))
					for _, sb := range tc {
						st, _ := sb["type"].(string)
						if st == "image" {
							sub = append(sub, map[string]any{
								"type": "text",
								"text": "[image omitted from earlier turn]",
							})
							continue
						}
						if st == "text" {
							txt, _ := sb["text"].(string)
							sub = append(sub, map[string]any{
								"type": "text",
								"text": truncateRunes(redactSecret(txt, secret), builtInMaxArchivedToolResultChars),
							})
						}
					}
					cp["content"] = sub
				default:
					cp["content"] = tc
				}
				out = append(out, cp)
			default:
				cp := make(map[string]any, len(blk))
				for k, v := range blk {
					cp[k] = v
				}
				out = append(out, cp)
			}
		}
		return out
	default:
		return content
	}
}

func cloneBuiltInTurnRecords(in []builtInTurnRecord) []builtInTurnRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]builtInTurnRecord, len(in))
	for i, r := range in {
		out[i] = builtInTurnRecord{
			TurnID:   r.TurnID,
			Messages: cloneAnthropicMessages(r.Messages),
			Bytes:    r.Bytes,
		}
	}
	return out
}

func cloneAnthropicMessages(in []anthropicMessage) []anthropicMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]anthropicMessage, len(in))
	for i, m := range in {
		out[i] = anthropicMessage{
			Role:    m.Role,
			Content: sanitizeBuiltInMessageContent(m.Content, ""),
		}
	}
	return out
}

func estimateAnthropicMessagesBytes(msgs []anthropicMessage) int {
	b, err := json.Marshal(msgs)
	if err != nil {
		return 0
	}
	return len(b)
}
