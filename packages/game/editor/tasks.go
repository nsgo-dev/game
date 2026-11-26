package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// AgentThreadVersion is the schema version of *.thread.yaml files stored under .nsgo/threads/.
const AgentThreadVersion = 1

// DefaultAgentThreadID is the filename stem of the default project thread file (.nsgo/threads/default.thread.yaml).
const DefaultAgentThreadID = "default"

// TaskState identifies where a background agent task sits in its lifecycle.
type TaskState string

const (
	// TaskNeedsYou means the task is paused waiting for a person's permission or input.
	TaskNeedsYou TaskState = "needs_you"
	// TaskRunning means the task is actively running (or holding on an object another task is editing).
	TaskRunning TaskState = "running"
	// TaskReadyToReview means the task finished its turn and waits for Keep, Revert, or Dismiss.
	TaskReadyToReview TaskState = "ready_to_review"
	// TaskQueued means the task waits for the running/unreviewed task ahead of it to be kept or reverted.
	TaskQueued TaskState = "queued"
	// TaskFailed means the task exited with an error and stays visible until opened, retried, or dismissed.
	TaskFailed TaskState = "failed"
	// TaskStopped means the task was stopped/cancelled or reverted.
	TaskStopped TaskState = "stopped"
	// TaskDone means the task's turn was kept and completed.
	TaskDone TaskState = "done"
)

// Valid reports whether s is a known TaskState.
func (s TaskState) Valid() bool {
	switch s {
	case TaskNeedsYou, TaskRunning, TaskReadyToReview, TaskQueued, TaskFailed, TaskStopped, TaskDone:
		return true
	default:
		return false
	}
}

// ActiveGroup reports whether s belongs to one of the five visible groups in the Tasks tab.
func (s TaskState) ActiveGroup() bool {
	switch s {
	case TaskNeedsYou, TaskRunning, TaskReadyToReview, TaskQueued, TaskFailed:
		return true
	default:
		return false
	}
}

// TaskSource identifies what initiated a background task (requests, plan steps, and automations share one list).
type TaskSource string

const (
	TaskSourceUser       TaskSource = "you"
	TaskSourcePlan       TaskSource = "plan"
	TaskSourceAutomation TaskSource = "automation"
)

// Valid reports whether s is a known TaskSource.
func (s TaskSource) Valid() bool {
	switch s {
	case TaskSourceUser, TaskSourcePlan, TaskSourceAutomation:
		return true
	default:
		return false
	}
}

// TaskStartMode chooses whether a new request queues behind the current turn or runs in parallel.
type TaskStartMode string

const (
	// TaskStartQueue starts the request only after the running task is kept or reverted, so it builds on the reviewed scene.
	TaskStartQueue TaskStartMode = "queue"
	// TaskStartParallel starts immediately as a separate agent session with its own numbered overlay.
	TaskStartParallel TaskStartMode = "parallel"
)

// Valid reports whether m is a known TaskStartMode.
func (m TaskStartMode) Valid() bool {
	return m == TaskStartQueue || m == TaskStartParallel
}

// TaskStep is one step entry shown in the focused task detail area.
type TaskStep struct {
	Summary string `yaml:"summary" json:"summary"`
	Done    bool   `yaml:"done,omitempty" json:"done,omitempty"`
	Active  bool   `yaml:"active,omitempty" json:"active,omitempty"`
	Blocked bool   `yaml:"blocked,omitempty" json:"blocked,omitempty"`
}

// TaskReviewChange describes one object or file change belonging to a task's turn review.
type TaskReviewChange struct {
	ObjectID   string `yaml:"objectId,omitempty" json:"objectId,omitempty"`
	File       string `yaml:"file,omitempty" json:"file,omitempty"`
	Kind       string `yaml:"kind,omitempty" json:"kind,omitempty"` // "added", "changed", "removed"
	Summary    string `yaml:"summary,omitempty" json:"summary,omitempty"`
	BeforeYAML string `yaml:"beforeYaml,omitempty" json:"beforeYaml,omitempty"`
	AfterYAML  string `yaml:"afterYaml,omitempty" json:"afterYaml,omitempty"`
	Kept       bool   `yaml:"kept,omitempty" json:"kept,omitempty"`
	Reverted   bool   `yaml:"reverted,omitempty" json:"reverted,omitempty"`
}

// TaskReview records the separate review turn produced by one completed background task.
type TaskReview struct {
	ID              string             `yaml:"id" json:"id"`
	TaskID          string             `yaml:"taskId,omitempty" json:"taskId,omitempty"`
	ScenePath       string             `yaml:"scene,omitempty" json:"scene,omitempty"`
	Summary         string             `yaml:"summary,omitempty" json:"summary,omitempty"`
	ChangeCount     int                `yaml:"changeCount,omitempty" json:"changeCount,omitempty"`
	Changes         []TaskReviewChange `yaml:"changes,omitempty" json:"changes,omitempty"`
	BeforeSceneYAML string             `yaml:"beforeSceneYaml,omitempty" json:"beforeSceneYaml,omitempty"`
	AfterSceneYAML  string             `yaml:"afterSceneYaml,omitempty" json:"afterSceneYaml,omitempty"`
	Kept            bool               `yaml:"kept,omitempty" json:"kept,omitempty"`
	Reverted        bool               `yaml:"reverted,omitempty" json:"reverted,omitempty"`
	Dismissed       bool               `yaml:"dismissed,omitempty" json:"dismissed,omitempty"`
}

// EffectiveChangeCount returns len(Changes) when populated, else ChangeCount.
func (r *TaskReview) EffectiveChangeCount() int {
	if r == nil {
		return 0
	}
	if len(r.Changes) > 0 {
		return len(r.Changes)
	}
	return r.ChangeCount
}

// AgentTask is one background task entry kept in the editor's Tasks tab and persisted in thread YAML files.
type AgentTask struct {
	ID                  string        `yaml:"id" json:"id"`
	Number              int           `yaml:"number,omitempty" json:"number,omitempty"`
	Title               string        `yaml:"title" json:"title"`
	Prompt              string        `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Source              TaskSource    `yaml:"source" json:"source"`
	PlanStep            int           `yaml:"planStep,omitempty" json:"planStep,omitempty"`
	PlanTotal           int           `yaml:"planTotal,omitempty" json:"planTotal,omitempty"`
	StartMode           TaskStartMode `yaml:"startMode" json:"startMode"`
	State               TaskState     `yaml:"state" json:"state"`
	ScenePath           string        `yaml:"scene,omitempty" json:"scene,omitempty"`
	StepSummary         string        `yaml:"stepSummary,omitempty" json:"stepSummary,omitempty"`
	StepIndex           int           `yaml:"stepIndex,omitempty" json:"stepIndex,omitempty"`
	StepTotal           int           `yaml:"stepTotal,omitempty" json:"stepTotal,omitempty"`
	Steps               []TaskStep    `yaml:"steps,omitempty" json:"steps,omitempty"`
	PermissionPrompt    string        `yaml:"permissionPrompt,omitempty" json:"permissionPrompt,omitempty"`
	PermissionDetail    string        `yaml:"permissionDetail,omitempty" json:"permissionDetail,omitempty"`
	Error               string        `yaml:"error,omitempty" json:"error,omitempty"`
	BlockedOn           string        `yaml:"blockedOn,omitempty" json:"blockedOn,omitempty"`
	BlockedByTaskID     string        `yaml:"blockedByTaskId,omitempty" json:"blockedByTaskId,omitempty"`
	BlockedByTaskNumber int           `yaml:"blockedByTaskNumber,omitempty" json:"blockedByTaskNumber,omitempty"`
	EditingObjects      []string      `yaml:"editingObjects,omitempty" json:"editingObjects,omitempty"`
	HoldingObjects      []string      `yaml:"holdingObjects,omitempty" json:"holdingObjects,omitempty"`
	StartsAfterTaskID   string        `yaml:"startsAfterTaskId,omitempty" json:"startsAfterTaskId,omitempty"`
	SessionID           string        `yaml:"sessionId,omitempty" json:"sessionId,omitempty"`
	Review              *TaskReview   `yaml:"review,omitempty" json:"review,omitempty"`
	CreatedAt           string        `yaml:"createdAt,omitempty" json:"createdAt,omitempty"`
	StartedAt           string        `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	FinishedAt          string        `yaml:"finishedAt,omitempty" json:"finishedAt,omitempty"`
	TimeLabel           string        `yaml:"timeLabel,omitempty" json:"timeLabel,omitempty"`
	ElapsedSeconds      int           `yaml:"elapsedSeconds,omitempty" json:"elapsedSeconds,omitempty"`
	Dismissed           bool          `yaml:"dismissed,omitempty" json:"dismissed,omitempty"`
}

func (t AgentTask) clone() AgentTask {
	out := t
	out.Steps = slices.Clone(t.Steps)
	out.EditingObjects = slices.Clone(t.EditingObjects)
	out.HoldingObjects = slices.Clone(t.HoldingObjects)
	if t.Review != nil {
		cp := *t.Review
		cp.Changes = slices.Clone(t.Review.Changes)
		out.Review = &cp
	}
	return out
}

// Holding reports whether t is running in parallel and currently waiting on an object held by another task.
func (t AgentTask) Holding() bool {
	return t.State == TaskRunning && t.BlockedOn != ""
}

// Visible reports whether t should appear in one of the five active groups in the Tasks tab.
func (t AgentTask) Visible() bool {
	return !t.Dismissed && t.State.ActiveGroup()
}

// AgentThreadDoc is the plain-text YAML format persisted under .nsgo/threads/<id>.thread.yaml.
type AgentThreadDoc struct {
	Version int         `yaml:"version"`
	ID      string      `yaml:"id"`
	Title   string      `yaml:"title,omitempty"`
	Scene   string      `yaml:"scene,omitempty"`
	Tasks   []AgentTask `yaml:"tasks,omitempty"`
}

// TaskRequest configures a new background task submitted to Editor.SubmitTask.
type TaskRequest struct {
	ID                string
	Number            int
	Title             string
	Prompt            string
	Source            TaskSource
	PlanStep          int
	PlanTotal         int
	StartMode         TaskStartMode
	ScenePath         string
	StepSummary       string
	StepIndex         int
	StepTotal         int
	Steps             []TaskStep
	PermissionPrompt  string
	PermissionDetail  string
	Error             string
	EditingObjects    []string
	HoldingObjects    []string
	BlockedOn         string
	BlockedByTaskID   string
	StartsAfterTaskID string
	Review            *TaskReview
	TimeLabel         string
	ElapsedSeconds    int
	// InitialState optionally seeds the task directly into a specific state (for automations,
	// plan steps, tests, or restored entries) without automatically launching AgentTaskRunner.
	InitialState TaskState
}

// TaskSummary aggregates active task counts and Dock badge totals for the status bar, Tasks panel,
// and downstream notification/Dock badge consumers (#1023).
type TaskSummary struct {
	NeedsYou      int `json:"needsYou"`
	Running       int `json:"running"`
	Blocked       int `json:"blocked"`
	ReadyToReview int `json:"readyToReview"`
	Queued        int `json:"queued"`
	Failed        int `json:"failed"`
	// DockBadge equals NeedsYou + ReadyToReview + Failed; Running and Queued never add to it.
	DockBadge int `json:"dockBadge"`
	// MostUrgent is the highest-priority active state (Failed > NeedsYou > ReadyToReview > Running > Queued), or empty when idle.
	MostUrgent TaskState `json:"mostUrgent,omitempty"`
}

// TaskStatusPart is one colored dot + label segment rendered in the status bar indicator.
type TaskStatusPart struct {
	State TaskState
	Label string
}

// ObjectTaskOverlay describes a numbered overlay drawn on an object in the viewport and scene tree.
type ObjectTaskOverlay struct {
	TaskID              string
	TaskNumber          int
	ObjectID            string
	Holding             bool
	BlockedOn           string
	BlockedByTaskNumber int
	Label               string
}

type agentTasksState struct {
	mu              sync.Mutex
	scope           *core.Scope
	panelOpen       bool
	listExpanded    bool
	composerMode    TaskStartMode
	composerDraft   string
	focusedID       string
	threadID        string
	tasks           []AgentTask
	selectedDismiss map[string]bool
	nextSeq         int
	nextNumber      int
	cancels         map[string]context.CancelFunc
	onHoldChange    func(taskID string)
}

func (s *agentTasksState) setScope(cx *core.Scope) {
	s.mu.Lock()
	s.scope = cx
	s.mu.Unlock()
}

func (s *agentTasksState) ensureInit() {
	if s.composerMode == "" {
		s.composerMode = TaskStartQueue
	}
	if s.threadID == "" {
		s.threadID = DefaultAgentThreadID
	}
	if s.selectedDismiss == nil {
		s.selectedDismiss = make(map[string]bool)
	}
	if s.cancels == nil {
		s.cancels = make(map[string]context.CancelFunc)
	}
}

// ParseAgentThread decodes a *.thread.yaml document, refusing unknown fields, duplicate keys,
// YAML aliases/merge keys, and invalid versions or enum values.
func ParseAgentThread(data []byte) (*AgentThreadDoc, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("editor: parse thread yaml: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("editor: thread yaml must be a mapping document")
	}
	if err := checkNoThreadYAMLReferences(&root); err != nil {
		return nil, err
	}
	if err := checkDuplicateYAMLKeys(&root); err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc AgentThreadDoc
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("editor: decode thread yaml: %w", err)
	}
	if doc.Version != AgentThreadVersion {
		return nil, fmt.Errorf("editor: thread yaml version = %d, want %d", doc.Version, AgentThreadVersion)
	}
	if strings.TrimSpace(doc.ID) == "" {
		return nil, errors.New("editor: thread yaml missing id")
	}
	seenID := make(map[string]bool, len(doc.Tasks))
	for i := range doc.Tasks {
		t := &doc.Tasks[i]
		if strings.TrimSpace(t.ID) == "" {
			return nil, fmt.Errorf("editor: thread yaml task[%d] missing id", i)
		}
		if seenID[t.ID] {
			return nil, fmt.Errorf("editor: thread yaml duplicate task id %q", t.ID)
		}
		seenID[t.ID] = true
		if strings.TrimSpace(t.Title) == "" {
			return nil, fmt.Errorf("editor: thread yaml task %q missing title", t.ID)
		}
		if !t.Source.Valid() {
			return nil, fmt.Errorf("editor: thread yaml task %q has unknown source %q", t.ID, t.Source)
		}
		if !t.StartMode.Valid() {
			return nil, fmt.Errorf("editor: thread yaml task %q has unknown startMode %q", t.ID, t.StartMode)
		}
		if !t.State.Valid() {
			return nil, fmt.Errorf("editor: thread yaml task %q has unknown state %q", t.ID, t.State)
		}
	}
	return &doc, nil
}

func checkNoThreadYAMLReferences(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		return errors.New("editor: thread yaml must not use YAML aliases")
	}
	if n.Anchor != "" {
		return errors.New("editor: thread yaml must not use YAML anchors")
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Tag == "!!merge" {
				return errors.New("editor: thread yaml must not use YAML merge keys")
			}
		}
	}
	for _, c := range n.Content {
		if err := checkNoThreadYAMLReferences(c); err != nil {
			return err
		}
	}
	return nil
}

// EncodeAgentThread encodes doc as deterministic plain-text YAML with fixed field order and 2-space indent.
func EncodeAgentThread(doc *AgentThreadDoc) ([]byte, error) {
	if doc == nil {
		return nil, errors.New("editor: nil thread doc")
	}
	cp := *doc
	if cp.Version == 0 {
		cp.Version = AgentThreadVersion
	}
	if strings.TrimSpace(cp.ID) == "" {
		cp.ID = DefaultAgentThreadID
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&cp); err != nil {
		return nil, fmt.Errorf("editor: encode thread yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: close thread yaml encoder: %w", err)
	}
	return buf.Bytes(), nil
}

// effectiveThreadsDir returns the directory where thread YAML files are saved, or "" if none is configured.
func (e *Editor) effectiveThreadsDir() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.ThreadsDir) != "" {
		return e.ThreadsDir
	}
	if e.ProjectPath != "" {
		return filepath.Join(filepath.Dir(e.ProjectPath), ".nsgo", "threads")
	}
	return ""
}

// AgentThreadFilePath returns the current thread's YAML file path under .nsgo/threads/, or "" when no project or ThreadsDir is set.
func (e *Editor) AgentThreadFilePath() string {
	dir := e.effectiveThreadsDir()
	if dir == "" {
		return ""
	}
	e.tasks.ensureInit()
	return filepath.Join(dir, e.tasks.threadID+".thread.yaml")
}

// SaveAgentThread writes the current task list to its plain-text YAML thread file when a project or ThreadsDir is set.
func (e *Editor) SaveAgentThread() error {
	if e == nil {
		return nil
	}
	path := e.AgentThreadFilePath()
	if path == "" {
		return nil
	}
	doc := e.AgentThreadDoc()
	data, err := EncodeAgentThread(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// AgentThreadDoc returns a snapshot of the editor's current background tasks as an AgentThreadDoc.
func (e *Editor) AgentThreadDoc() *AgentThreadDoc {
	if e == nil {
		return &AgentThreadDoc{Version: AgentThreadVersion, ID: DefaultAgentThreadID}
	}
	e.tasks.ensureInit()
	tasks := make([]AgentTask, len(e.tasks.tasks))
	for i := range e.tasks.tasks {
		tasks[i] = e.tasks.tasks[i].clone()
	}
	scene := e.ScenePath
	if scene == "" && e.Doc != nil {
		scene = e.Doc.Name
	}
	return &AgentThreadDoc{
		Version: AgentThreadVersion,
		ID:      e.tasks.threadID,
		Scene:   scene,
		Tasks:   tasks,
	}
}

// LoadAgentThread loads tasks from path (a *.thread.yaml file) into the editor.
func (e *Editor) LoadAgentThread(path string) error {
	if e == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, err := ParseAgentThread(data)
	if err != nil {
		return err
	}
	e.stopAllTaskRunners()
	e.tasks.ensureInit()
	e.tasks.threadID = doc.ID
	e.tasks.tasks = make([]AgentTask, len(doc.Tasks))
	maxNum := 0
	for i := range doc.Tasks {
		e.tasks.tasks[i] = doc.Tasks[i].clone()
		if doc.Tasks[i].Number > maxNum {
			maxNum = doc.Tasks[i].Number
		}
	}
	e.tasks.nextSeq = max(e.tasks.nextSeq, len(doc.Tasks))
	e.tasks.nextNumber = max(e.tasks.nextNumber, maxNum)
	e.tasks.selectedDismiss = make(map[string]bool)
	e.ensureFocusedTaskLocked()
	e.notify()
	return nil
}

func (e *Editor) resetTasksForProject() {
	if e == nil {
		return
	}
	e.stopAllTaskRunners()
	e.tasks.tasks = nil
	e.tasks.selectedDismiss = make(map[string]bool)
	e.tasks.focusedID = ""
	e.tasks.nextSeq = 0
	e.tasks.nextNumber = 0
	e.tasks.threadID = DefaultAgentThreadID
	if path := e.AgentThreadFilePath(); path != "" {
		if _, err := os.Stat(path); err == nil {
			_ = e.LoadAgentThread(path)
		}
	}
}

func (e *Editor) stopAllTaskRunners() {
	if e == nil {
		return
	}
	e.tasks.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(e.tasks.cancels))
	for id, cancel := range e.tasks.cancels {
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
		delete(e.tasks.cancels, id)
	}
	e.tasks.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// SetTaskHoldReleaseHook registers a callback invoked whenever a task stops or finishes and
// releases its object holds (used by editormcp to wake waiting parallel sessions).
func (e *Editor) SetTaskHoldReleaseHook(fn func(taskID string)) {
	if e == nil {
		return
	}
	e.tasks.mu.Lock()
	e.tasks.onHoldChange = fn
	e.tasks.mu.Unlock()
}

func (e *Editor) fireHoldRelease(taskID string) {
	if e == nil || taskID == "" {
		return
	}
	e.tasks.mu.Lock()
	fn := e.tasks.onHoldChange
	e.tasks.mu.Unlock()
	if fn != nil {
		fn(taskID)
	}
}

// TasksPanelOpen reports whether the Agent / Background Tasks panel is currently open.
func (e *Editor) TasksPanelOpen() bool {
	return e != nil && e.tasks.panelOpen
}

// SetTasksPanelOpen opens or closes the Agent / Background Tasks panel.
func (e *Editor) SetTasksPanelOpen(open bool) {
	if e == nil {
		return
	}
	e.tasks.ensureInit()
	if e.tasks.panelOpen == open {
		return
	}
	e.tasks.panelOpen = open
	if open && !e.tasks.listExpanded {
		e.tasks.listExpanded = true
	}
	e.ensureFocusedTaskLocked()
	e.notify()
}

// ToggleTasksPanel toggles the Agent / Background Tasks panel open or closed.
func (e *Editor) ToggleTasksPanel() {
	if e == nil {
		return
	}
	e.SetTasksPanelOpen(!e.tasks.panelOpen)
}

// TasksListExpanded reports whether the Tasks section inside the Agent panel is expanded.
func (e *Editor) TasksListExpanded() bool {
	if e == nil {
		return false
	}
	return e.tasks.listExpanded
}

// SetTasksListExpanded expands or collapses the Tasks list inside the Agent panel.
func (e *Editor) SetTasksListExpanded(expanded bool) {
	if e == nil || e.tasks.listExpanded == expanded {
		return
	}
	e.tasks.listExpanded = expanded
	e.notify()
}

// ToggleTasksListExpanded flips the expanded state of the Tasks list inside the Agent panel.
func (e *Editor) ToggleTasksListExpanded() {
	if e == nil {
		return
	}
	e.SetTasksListExpanded(!e.tasks.listExpanded)
}

// TaskComposerMode returns the selected start mode (Queue or Run in parallel) in the Tasks composer.
func (e *Editor) TaskComposerMode() TaskStartMode {
	if e == nil {
		return TaskStartQueue
	}
	e.tasks.ensureInit()
	return e.tasks.composerMode
}

// SetTaskComposerMode sets the selected start mode (Queue or Run in parallel) in the Tasks composer.
func (e *Editor) SetTaskComposerMode(mode TaskStartMode) {
	if e == nil || !mode.Valid() {
		return
	}
	e.tasks.ensureInit()
	if e.tasks.composerMode == mode {
		return
	}
	e.tasks.composerMode = mode
	e.notify()
}

// TaskComposerDraft returns the current prompt draft in the Tasks panel composer.
func (e *Editor) TaskComposerDraft() string {
	if e == nil {
		return ""
	}
	return e.tasks.composerDraft
}

// SetTaskComposerDraft updates the prompt draft in the Tasks panel composer.
func (e *Editor) SetTaskComposerDraft(draft string) {
	if e == nil || e.tasks.composerDraft == draft {
		return
	}
	e.tasks.composerDraft = draft
	e.notify()
}

// SubmitComposerTask submits the current composer draft using TaskComposerMode and clears the draft.
func (e *Editor) SubmitComposerTask() (AgentTask, error) {
	if e == nil {
		return AgentTask{}, errors.New("editor: nil editor")
	}
	prompt := strings.TrimSpace(e.tasks.composerDraft)
	if prompt == "" {
		return AgentTask{}, errors.New("editor: task prompt is empty")
	}
	mode := e.TaskComposerMode()
	e.tasks.composerDraft = ""
	return e.SubmitTask(TaskRequest{
		Title:     firstLineTitle(prompt),
		Prompt:    prompt,
		Source:    TaskSourceUser,
		StartMode: mode,
	})
}

func firstLineTitle(prompt string) string {
	line := strings.TrimSpace(prompt)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if len(line) > 72 {
		line = strings.TrimSpace(line[:69]) + "…"
	}
	return line
}

func (e *Editor) nowTime() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Editor) findTaskIndex(id string) int {
	for i := range e.tasks.tasks {
		if e.tasks.tasks[i].ID == id {
			return i
		}
	}
	return -1
}

// Tasks returns a snapshot copy of all tasks in creation/queue order.
func (e *Editor) Tasks() []AgentTask {
	if e == nil {
		return nil
	}
	out := make([]AgentTask, len(e.tasks.tasks))
	for i := range e.tasks.tasks {
		out[i] = e.tasks.tasks[i].clone()
	}
	return out
}

// VisibleTasks returns all non-dismissed tasks belonging to the five active groups.
func (e *Editor) VisibleTasks() []AgentTask {
	if e == nil {
		return nil
	}
	var out []AgentTask
	for i := range e.tasks.tasks {
		if e.tasks.tasks[i].Visible() {
			out = append(out, e.tasks.tasks[i].clone())
		}
	}
	return out
}

// TasksInState returns all visible (non-dismissed) tasks currently in state.
func (e *Editor) TasksInState(state TaskState) []AgentTask {
	if e == nil {
		return nil
	}
	var out []AgentTask
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == state {
			out = append(out, t.clone())
		}
	}
	return out
}

// Task returns the task with id, if present.
func (e *Editor) Task(id string) (AgentTask, bool) {
	if e == nil || id == "" {
		return AgentTask{}, false
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return AgentTask{}, false
	}
	return e.tasks.tasks[idx].clone(), true
}

// FocusedTask returns the currently focused task in the Tasks panel.
func (e *Editor) FocusedTask() (AgentTask, bool) {
	if e == nil {
		return AgentTask{}, false
	}
	if e.tasks.focusedID != "" {
		if t, ok := e.Task(e.tasks.focusedID); ok && !t.Dismissed {
			return t, true
		}
	}
	for _, st := range []TaskState{TaskRunning, TaskNeedsYou, TaskReadyToReview, TaskQueued, TaskFailed} {
		for i := range e.tasks.tasks {
			if !e.tasks.tasks[i].Dismissed && e.tasks.tasks[i].State == st {
				return e.tasks.tasks[i].clone(), true
			}
		}
	}
	return AgentTask{}, false
}

// FocusTask sets the focused task in the Tasks panel.
func (e *Editor) FocusTask(id string) {
	if e == nil || id == "" {
		return
	}
	if e.findTaskIndex(id) < 0 {
		return
	}
	if e.tasks.focusedID == id {
		return
	}
	e.tasks.focusedID = id
	e.notify()
}

func (e *Editor) ensureFocusedTaskLocked() {
	if e.tasks.focusedID != "" {
		if idx := e.findTaskIndex(e.tasks.focusedID); idx >= 0 && !e.tasks.tasks[idx].Dismissed {
			return
		}
	}
	e.tasks.focusedID = ""
	for _, st := range []TaskState{TaskRunning, TaskNeedsYou, TaskReadyToReview, TaskQueued, TaskFailed} {
		for i := range e.tasks.tasks {
			if !e.tasks.tasks[i].Dismissed && e.tasks.tasks[i].State == st {
				e.tasks.focusedID = e.tasks.tasks[i].ID
				return
			}
		}
	}
}

// OpenTask opens the Tasks panel focused on id and, when possible, switches to or opens its target
// scene and selects its first edited/blocked object.
func (e *Editor) OpenTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	task := e.tasks.tasks[idx]
	e.tasks.panelOpen = true
	e.tasks.listExpanded = true
	e.tasks.focusedID = id

	// Switch to or open the task's scene if it names a project scene file.
	if task.ScenePath != "" && e.ProjectPath != "" && strings.HasSuffix(task.ScenePath, ".scene.yaml") {
		abs := filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(task.ScenePath))
		if _, err := os.Stat(abs); err == nil && filepath.Clean(abs) != filepath.Clean(e.FilePath) {
			if err := e.OpenFile(abs); err != nil {
				return err
			}
		}
	}
	// Select the first relevant object in the open scene when present.
	targetObj := task.BlockedOn
	if targetObj == "" && len(task.EditingObjects) > 0 {
		targetObj = task.EditingObjects[0]
	}
	if targetObj == "" && task.Review != nil {
		for _, ch := range task.Review.Changes {
			if ch.ObjectID != "" {
				targetObj = ch.ObjectID
				break
			}
		}
	}
	if targetObj != "" {
		if obj, _ := e.FindResolvedObject(targetObj); obj != nil {
			e.SelectObject(targetObj)
		}
	}
	e.notify()
	return nil
}

// SubmitTask adds a new background agent request according to req.StartMode:
//   - TaskStartQueue starts immediately only when no blocking turn is running, waiting on you,
//     or unreviewed; otherwise it enters TaskQueued and starts only after the blocking task is
//     kept or reverted.
//   - TaskStartParallel starts immediately as a separate session with its own numbered overlay.
func (e *Editor) SubmitTask(req TaskRequest) (AgentTask, error) {
	if e == nil {
		return AgentTask{}, errors.New("editor: nil editor")
	}
	e.tasks.ensureInit()
	title := strings.TrimSpace(req.Title)
	prompt := strings.TrimSpace(req.Prompt)
	if title == "" {
		title = firstLineTitle(prompt)
	}
	if title == "" {
		return AgentTask{}, errors.New("editor: task requires a title or prompt")
	}
	source := req.Source
	if source == "" {
		source = TaskSourceUser
	}
	if !source.Valid() {
		return AgentTask{}, fmt.Errorf("editor: unknown task source %q", source)
	}
	mode := req.StartMode
	if mode == "" {
		mode = TaskStartQueue
	}
	if !mode.Valid() {
		return AgentTask{}, fmt.Errorf("editor: unknown task start mode %q", mode)
	}

	e.tasks.nextSeq++
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = fmt.Sprintf("task-%d", e.tasks.nextSeq)
	}
	if e.findTaskIndex(id) >= 0 {
		return AgentTask{}, fmt.Errorf("editor: duplicate task id %q", id)
	}
	num := req.Number
	if num <= 0 && (source != TaskSourceAutomation || mode == TaskStartParallel || req.InitialState == TaskRunning) {
		e.tasks.nextNumber++
		num = e.tasks.nextNumber
	} else if num > e.tasks.nextNumber {
		e.tasks.nextNumber = num
	}

	scenePath := req.ScenePath
	if scenePath == "" {
		if e.ScenePath != "" {
			scenePath = e.ScenePath
		} else if e.Doc != nil && e.Doc.Name != "" {
			scenePath = e.Doc.Name
		}
	}

	now := e.nowTime()
	task := AgentTask{
		ID:               id,
		Number:           num,
		Title:            title,
		Prompt:           prompt,
		Source:           source,
		PlanStep:         req.PlanStep,
		PlanTotal:        req.PlanTotal,
		StartMode:        mode,
		ScenePath:        scenePath,
		StepSummary:      req.StepSummary,
		StepIndex:        req.StepIndex,
		StepTotal:        req.StepTotal,
		Steps:            slices.Clone(req.Steps),
		PermissionPrompt: req.PermissionPrompt,
		PermissionDetail: req.PermissionDetail,
		Error:            req.Error,
		EditingObjects:   slices.Clone(req.EditingObjects),
		HoldingObjects:   slices.Clone(req.HoldingObjects),
		BlockedOn:        req.BlockedOn,
		BlockedByTaskID:  req.BlockedByTaskID,
		TimeLabel:        req.TimeLabel,
		ElapsedSeconds:   req.ElapsedSeconds,
		CreatedAt:        now.UTC().Format(time.RFC3339),
	}
	if task.BlockedByTaskID != "" {
		if holderIdx := e.findTaskIndex(task.BlockedByTaskID); holderIdx >= 0 {
			task.BlockedByTaskNumber = e.tasks.tasks[holderIdx].Number
		}
	}
	if req.Review != nil {
		cp := *req.Review
		cp.Changes = slices.Clone(req.Review.Changes)
		if cp.ID == "" {
			cp.ID = "review-" + id
		}
		if cp.TaskID == "" {
			cp.TaskID = id
		}
		task.Review = &cp
	}

	shouldLaunch := false
	if req.InitialState != "" {
		if !req.InitialState.Valid() {
			return AgentTask{}, fmt.Errorf("editor: unknown task state %q", req.InitialState)
		}
		task.State = req.InitialState
		task.StartsAfterTaskID = req.StartsAfterTaskID
		if task.State == TaskRunning {
			task.StartedAt = now.UTC().Format(time.RFC3339)
			task.SessionID = "session-" + id
		}
	} else if mode == TaskStartParallel {
		task.State = TaskRunning
		task.StartedAt = now.UTC().Format(time.RFC3339)
		task.SessionID = "session-" + id
		shouldLaunch = true
	} else {
		blockerID := req.StartsAfterTaskID
		if blockerID == "" {
			blockerID = e.findQueueBlockerID()
		}
		if blockerID != "" {
			task.State = TaskQueued
			task.StartsAfterTaskID = blockerID
		} else {
			task.State = TaskRunning
			task.StartedAt = now.UTC().Format(time.RFC3339)
			task.SessionID = "session-" + id
			shouldLaunch = true
		}
	}

	e.tasks.tasks = append(e.tasks.tasks, task)
	if e.tasks.focusedID == "" || task.State == TaskRunning {
		e.tasks.focusedID = task.ID
	}
	_ = e.SaveAgentThread()
	e.notify()

	if shouldLaunch {
		e.launchTaskRunner(task.ID)
	}
	return task.clone(), nil
}

// findQueueBlockerID returns the task ID that a newly queued request must wait behind:
// the last queued task if any exist, otherwise a running, needs-you, or unreviewed non-automation turn.
func (e *Editor) findQueueBlockerID() string {
	// If there is already a queued chain, append behind the last queued task.
	for i := len(e.tasks.tasks) - 1; i >= 0; i-- {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == TaskQueued {
			return t.ID
		}
	}
	// Otherwise wait for any running task, needs-you task, or unreviewed foreground (non-automation) turn.
	for i := len(e.tasks.tasks) - 1; i >= 0; i-- {
		t := &e.tasks.tasks[i]
		if t.Dismissed {
			continue
		}
		switch t.State {
		case TaskRunning, TaskNeedsYou:
			return t.ID
		case TaskReadyToReview:
			if t.Source != TaskSourceAutomation {
				return t.ID
			}
		}
	}
	return ""
}

func (e *Editor) isTaskStillBlocking(id string) bool {
	if id == "" {
		return false
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return false
	}
	t := &e.tasks.tasks[idx]
	if t.Dismissed {
		return false
	}
	switch t.State {
	case TaskRunning, TaskNeedsYou, TaskQueued, TaskReadyToReview:
		return true
	default:
		return false
	}
}

func (e *Editor) hasAnyForegroundBlocker(excludeID string) bool {
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if t.ID == excludeID || t.Dismissed {
			continue
		}
		switch t.State {
		case TaskRunning, TaskNeedsYou:
			return true
		case TaskReadyToReview:
			if t.Source != TaskSourceAutomation {
				return true
			}
		}
	}
	return false
}

// advanceTaskQueue starts the next eligible queued task once its predecessor has been kept,
// reverted, stopped, or resolved.
func (e *Editor) advanceTaskQueue(resolvedID string) {
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if t.Dismissed || t.State != TaskQueued {
			continue
		}
		if t.StartsAfterTaskID != "" && t.StartsAfterTaskID != resolvedID && e.isTaskStillBlocking(t.StartsAfterTaskID) {
			continue
		}
		if e.hasAnyForegroundBlocker(t.ID) {
			continue
		}
		now := e.nowTime()
		t.State = TaskRunning
		t.StartsAfterTaskID = ""
		t.StartedAt = now.UTC().Format(time.RFC3339)
		if t.SessionID == "" {
			t.SessionID = "session-" + t.ID
		}
		if t.Number <= 0 {
			e.tasks.nextNumber++
			t.Number = e.tasks.nextNumber
		}
		startedID := t.ID
		_ = e.SaveAgentThread()
		e.notify()
		e.launchTaskRunner(startedID)
		return
	}
}

func (e *Editor) launchTaskRunner(taskID string) {
	if e == nil || e.AgentTaskRunner == nil {
		return
	}
	task, ok := e.Task(taskID)
	if !ok {
		return
	}
	runner := e.AgentTaskRunner
	ctx, cancel := context.WithCancel(context.Background())
	e.tasks.mu.Lock()
	if prev := e.tasks.cancels[taskID]; prev != nil {
		prev()
	}
	e.tasks.cancels[taskID] = cancel
	e.tasks.mu.Unlock()

	post := func(fn func()) {
		e.tasks.mu.Lock()
		sc := e.tasks.scope
		e.tasks.mu.Unlock()
		if sc != nil {
			sc.Post(fn)
			return
		}
		fn()
	}

	go func() {
		err := runner(ctx, task)
		e.tasks.mu.Lock()
		delete(e.tasks.cancels, taskID)
		e.tasks.mu.Unlock()
		cancel()

		post(func() {
			cur, ok := e.Task(taskID)
			if !ok {
				return
			}
			// If the runner already transitioned the task (e.g. CompleteTaskTurn, FailTask, StopTask), respect it.
			if cur.State != TaskRunning && cur.State != TaskNeedsYou {
				return
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					_ = e.StopTask(taskID)
					return
				}
				_ = e.FailTask(taskID, err.Error())
				return
			}
			_ = e.CompleteTaskTurn(taskID, TaskReview{
				ID:        "review-" + taskID,
				TaskID:    taskID,
				ScenePath: cur.ScenePath,
				Summary:   cur.StepSummary,
			})
		})
	}()
}

// UpdateTaskStep updates the current step progress text and step numbers on task id.
func (e *Editor) UpdateTaskStep(id string, stepIndex, stepTotal int, summary string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	if stepIndex > 0 {
		t.StepIndex = stepIndex
	}
	if stepTotal > 0 {
		t.StepTotal = stepTotal
	}
	if summary != "" {
		t.StepSummary = summary
	}
	_ = e.SaveAgentThread()
	e.notify()
	return nil
}

// AddTaskStep appends a step entry to task id's detail log (marking previous active steps done when done is false).
func (e *Editor) AddTaskStep(id string, summary string, done bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	if !done {
		for i := range t.Steps {
			if t.Steps[i].Active {
				t.Steps[i].Active = false
				t.Steps[i].Done = true
			}
		}
		t.StepSummary = summary
	}
	t.Steps = append(t.Steps, TaskStep{
		Summary: summary,
		Done:    done,
		Active:  !done,
	})
	_ = e.SaveAgentThread()
	e.notify()
	return nil
}

// SetTaskHoldState updates which objects task id is actively editing or holding/waiting on.
// When blockedOn is non-empty, the task shows "waiting on <blockedOn>" in the task list and viewport overlay.
func (e *Editor) SetTaskHoldState(id string, editing []string, holding []string, blockedOn string, blockedByTaskID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	t.EditingObjects = slices.Clone(editing)
	t.HoldingObjects = slices.Clone(holding)
	t.BlockedOn = blockedOn
	t.BlockedByTaskID = blockedByTaskID
	t.BlockedByTaskNumber = 0
	if blockedByTaskID != "" {
		if holderIdx := e.findTaskIndex(blockedByTaskID); holderIdx >= 0 {
			t.BlockedByTaskNumber = e.tasks.tasks[holderIdx].Number
		}
	}
	if blockedOn != "" {
		waitMsg := "waiting on " + blockedOn
		if t.BlockedByTaskNumber > 0 {
			waitMsg = fmt.Sprintf("waiting on %s (task %d)", blockedOn, t.BlockedByTaskNumber)
		}
		t.StepSummary = "waiting on " + blockedOn
		// Update or append blocked step entry.
		updated := false
		for i := range t.Steps {
			if t.Steps[i].Blocked {
				t.Steps[i].Summary = waitMsg
				updated = true
				break
			}
		}
		if !updated {
			t.Steps = append(t.Steps, TaskStep{Summary: waitMsg, Blocked: true})
		}
	} else {
		// Clear blocked flag from prior waiting steps when hold resumes.
		for i := range t.Steps {
			if t.Steps[i].Blocked {
				t.Steps[i].Blocked = false
				t.Steps[i].Done = true
			}
		}
	}
	_ = e.SaveAgentThread()
	e.notify()
	return nil
}

// ClearTaskHoldState clears editing/holding/blocked state on task id and triggers the hold release hook.
func (e *Editor) ClearTaskHoldState(id string) {
	if e == nil {
		return
	}
	if idx := e.findTaskIndex(id); idx >= 0 {
		t := &e.tasks.tasks[idx]
		t.EditingObjects = nil
		t.HoldingObjects = nil
		t.BlockedOn = ""
		t.BlockedByTaskID = ""
		t.BlockedByTaskNumber = 0
		_ = e.SaveAgentThread()
		e.notify()
	}
	e.fireHoldRelease(id)
}

// RequestTaskPermission transitions task id to TaskNeedsYou with the given permission prompt.
func (e *Editor) RequestTaskPermission(id, prompt, detail string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	t.State = TaskNeedsYou
	t.PermissionPrompt = prompt
	t.PermissionDetail = detail
	if prompt != "" {
		t.StepSummary = prompt
	}
	_ = e.SaveAgentThread()
	e.notify()
	return nil
}

// ResolveTaskPermission resolves a TaskNeedsYou permission ask, resuming the task when allowed
// or recording the refusal and resuming/notifying the optional callback.
func (e *Editor) ResolveTaskPermission(id string, allow bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	if t.State != TaskNeedsYou {
		return fmt.Errorf("editor: task %q is not waiting for permission (state=%s)", id, t.State)
	}
	t.State = TaskRunning
	t.PermissionPrompt = ""
	t.PermissionDetail = ""
	if allow {
		t.Steps = append(t.Steps, TaskStep{Summary: "Permission allowed once", Done: true})
	} else {
		t.Steps = append(t.Steps, TaskStep{Summary: "Permission denied", Done: true})
	}
	_ = e.SaveAgentThread()
	e.notify()
	if e.OnTaskPermissionDecision != nil {
		e.OnTaskPermissionDecision(id, allow)
	}
	return nil
}

// CompleteTaskTurn finishes task id's running turn, releases its object holds, and moves it to
// TaskReadyToReview with its own separate TaskReview. Queued tasks do NOT start yet; they wait
// until this task is kept or reverted.
func (e *Editor) CompleteTaskTurn(id string, review TaskReview) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	now := e.nowTime()
	t.State = TaskReadyToReview
	t.EditingObjects = nil
	t.HoldingObjects = nil
	t.BlockedOn = ""
	t.BlockedByTaskID = ""
	t.BlockedByTaskNumber = 0
	t.FinishedAt = now.UTC().Format(time.RFC3339)
	if t.ElapsedSeconds == 0 && t.StartedAt != "" {
		if st, err := time.Parse(time.RFC3339, t.StartedAt); err == nil {
			secs := int(now.Sub(st).Seconds())
			if secs > 0 {
				t.ElapsedSeconds = secs
			}
		}
	}
	for i := range t.Steps {
		if t.Steps[i].Active || t.Steps[i].Blocked {
			t.Steps[i].Active = false
			t.Steps[i].Blocked = false
			t.Steps[i].Done = true
		}
	}
	if review.ID == "" {
		review.ID = "review-" + id
	}
	if review.TaskID == "" {
		review.TaskID = id
	}
	if review.ScenePath == "" {
		review.ScenePath = t.ScenePath
	}
	cp := review
	cp.Changes = slices.Clone(review.Changes)
	t.Review = &cp

	_ = e.SaveAgentThread()
	e.notify()
	e.fireHoldRelease(id)
	return nil
}

// FailTask moves task id to TaskFailed, releases its holds, and keeps it visible until opened, retried, or dismissed.
func (e *Editor) FailTask(id, errMsg string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	t.State = TaskFailed
	t.Error = errMsg
	t.EditingObjects = nil
	t.HoldingObjects = nil
	t.BlockedOn = ""
	t.BlockedByTaskID = ""
	t.BlockedByTaskNumber = 0
	t.FinishedAt = e.nowTime().UTC().Format(time.RFC3339)
	_ = e.SaveAgentThread()
	e.notify()
	e.fireHoldRelease(id)
	return nil
}

// StopTask cancels a running, waiting, or queued task, releases any object holds, and advances the queue.
func (e *Editor) StopTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	e.tasks.mu.Lock()
	cancel := e.tasks.cancels[id]
	delete(e.tasks.cancels, id)
	e.tasks.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	t := &e.tasks.tasks[idx]
	t.State = TaskStopped
	t.EditingObjects = nil
	t.HoldingObjects = nil
	t.BlockedOn = ""
	t.BlockedByTaskID = ""
	t.BlockedByTaskNumber = 0
	t.FinishedAt = e.nowTime().UTC().Format(time.RFC3339)
	delete(e.tasks.selectedDismiss, id)
	e.ensureFocusedTaskLocked()
	_ = e.SaveAgentThread()
	e.notify()
	e.fireHoldRelease(id)
	e.advanceTaskQueue(id)
	return nil
}

// RetryTask retries a failed task, starting it immediately if unblocked or queuing it cleanly.
func (e *Editor) RetryTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	if t.State != TaskFailed {
		return fmt.Errorf("editor: task %q is not failed (state=%s)", id, t.State)
	}
	t.Error = ""
	t.Dismissed = false
	t.FinishedAt = ""
	t.ElapsedSeconds = 0
	now := e.nowTime()
	shouldLaunch := false
	if t.StartMode == TaskStartParallel || !e.hasAnyForegroundBlocker(t.ID) {
		t.State = TaskRunning
		t.StartedAt = now.UTC().Format(time.RFC3339)
		if t.SessionID == "" {
			t.SessionID = "session-" + t.ID
		}
		if t.Number <= 0 {
			e.tasks.nextNumber++
			t.Number = e.tasks.nextNumber
		}
		shouldLaunch = true
	} else {
		t.State = TaskQueued
		t.StartsAfterTaskID = e.findQueueBlockerID()
	}
	e.tasks.focusedID = t.ID
	_ = e.SaveAgentThread()
	e.notify()
	if shouldLaunch {
		e.launchTaskRunner(t.ID)
	}
	return nil
}

// KeepTask accepts a TaskReadyToReview turn, marks its review kept, transitions it to TaskDone,
// and starts the next queued task waiting for it.
func (e *Editor) KeepTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	if t.State != TaskReadyToReview {
		return fmt.Errorf("editor: task %q is not ready to review (state=%s)", id, t.State)
	}
	if t.Review == nil {
		t.Review = &TaskReview{ID: "review-" + id, TaskID: id, ScenePath: t.ScenePath}
	}
	t.Review.Kept = true
	t.Review.Reverted = false
	for i := range t.Review.Changes {
		if !t.Review.Changes[i].Reverted {
			t.Review.Changes[i].Kept = true
		}
	}
	t.State = TaskDone
	delete(e.tasks.selectedDismiss, id)
	e.ensureFocusedTaskLocked()
	_ = e.SaveAgentThread()
	e.notify()
	e.advanceTaskQueue(id)
	return nil
}

// RevertTask reverts an unkept TaskReadyToReview turn, marks it TaskStopped, and starts the next
// queued task waiting for it. A turn already kept is never reverted by RevertTask.
func (e *Editor) RevertTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := e.findTaskIndex(id)
	if idx < 0 {
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	if t.State != TaskReadyToReview {
		return fmt.Errorf("editor: task %q is not ready to review (state=%s)", id, t.State)
	}
	if t.Review != nil && !t.Review.Kept {
		if err := e.applyTaskReviewRevert(t); err != nil {
			return err
		}
		t.Review.Reverted = true
		for i := range t.Review.Changes {
			if !t.Review.Changes[i].Kept {
				t.Review.Changes[i].Reverted = true
			}
		}
	}
	t.State = TaskStopped
	delete(e.tasks.selectedDismiss, id)
	e.ensureFocusedTaskLocked()
	_ = e.SaveAgentThread()
	e.notify()
	e.advanceTaskQueue(id)
	return nil
}

type taskSceneRevertCmd struct {
	ed     *Editor
	label  string
	before *sceneformat.SceneDoc
	after  *sceneformat.SceneDoc
}

func (c *taskSceneRevertCmd) Label() string { return c.label }
func (c *taskSceneRevertCmd) Do() {
	c.ed.Doc = c.after
	c.ed.Rebuild()
}
func (c *taskSceneRevertCmd) Undo() {
	c.ed.Doc = c.before
	c.ed.Rebuild()
}

func (e *Editor) applyTaskReviewRevert(t *AgentTask) error {
	if e == nil || t == nil || t.Review == nil || t.Review.Kept {
		return nil
	}
	if t.Review.BeforeSceneYAML == "" || e.Doc == nil {
		return nil
	}
	if t.ScenePath != "" && e.ScenePath != "" && t.ScenePath != e.ScenePath {
		return nil
	}
	doc, err := sceneformat.ParseWith([]byte(t.Review.BeforeSceneYAML), sceneformat.ParseOptions{
		Components:    e.components,
		PhysicsLayers: e.physicsLayers,
	})
	if err != nil {
		return err
	}
	e.History.Execute(&taskSceneRevertCmd{
		ed:     e,
		label:  "Revert agent task: " + t.Title,
		before: e.Doc,
		after:  doc,
	})
	return nil
}

// DismissTasks dismisses one or more Ready to review or Failed tasks from the list.
// Dismissing NEVER reverts anything that was kept (nor mutates kept scene edits or undo history).
func (e *Editor) DismissTasks(ids []string) int {
	if e == nil || len(ids) == 0 {
		return 0
	}
	e.tasks.ensureInit()
	dismissed := 0
	var resolved []string
	for _, id := range ids {
		idx := e.findTaskIndex(id)
		if idx < 0 {
			continue
		}
		t := &e.tasks.tasks[idx]
		if t.Dismissed {
			continue
		}
		if t.State != TaskReadyToReview && t.State != TaskFailed {
			continue
		}
		t.Dismissed = true
		if t.Review != nil {
			t.Review.Dismissed = true
		}
		delete(e.tasks.selectedDismiss, id)
		dismissed++
		resolved = append(resolved, id)
	}
	if dismissed > 0 {
		e.ensureFocusedTaskLocked()
		_ = e.SaveAgentThread()
		e.notify()
		for _, id := range resolved {
			e.advanceTaskQueue(id)
		}
	}
	return dismissed
}

// SetTaskDismissSelected sets whether a Ready to review task is checked for bulk dismissal.
func (e *Editor) SetTaskDismissSelected(id string, selected bool) {
	if e == nil || id == "" {
		return
	}
	e.tasks.ensureInit()
	idx := e.findTaskIndex(id)
	if idx < 0 || e.tasks.tasks[idx].State != TaskReadyToReview || e.tasks.tasks[idx].Dismissed {
		return
	}
	if selected {
		e.tasks.selectedDismiss[id] = true
	} else {
		delete(e.tasks.selectedDismiss, id)
	}
	e.notify()
}

// ToggleTaskDismissSelection toggles whether a Ready to review task is checked for bulk dismissal.
func (e *Editor) ToggleTaskDismissSelection(id string) {
	if e == nil || id == "" {
		return
	}
	e.tasks.ensureInit()
	e.SetTaskDismissSelected(id, !e.tasks.selectedDismiss[id])
}

// TaskDismissSelected reports whether task id is currently checked for bulk dismissal.
func (e *Editor) TaskDismissSelected(id string) bool {
	if e == nil || id == "" {
		return false
	}
	return e.tasks.selectedDismiss[id]
}

// SelectedDismissTaskIDs returns the IDs of all Ready to review tasks currently checked for bulk dismissal, in list order.
func (e *Editor) SelectedDismissTaskIDs() []string {
	if e == nil {
		return nil
	}
	var out []string
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == TaskReadyToReview && e.tasks.selectedDismiss[t.ID] {
			out = append(out, t.ID)
		}
	}
	return out
}

// DismissSelectedTasks dismisses every checked Ready to review task together, reverting nothing that was kept.
func (e *Editor) DismissSelectedTasks() int {
	return e.DismissTasks(e.SelectedDismissTaskIDs())
}

// ReorderQueuedTask moves queued task id earlier (delta < 0) or later (delta > 0) among queued tasks.
func (e *Editor) ReorderQueuedTask(id string, delta int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if delta == 0 {
		return nil
	}
	var queuedIndices []int
	pos := -1
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == TaskQueued {
			if t.ID == id {
				pos = len(queuedIndices)
			}
			queuedIndices = append(queuedIndices, i)
		}
	}
	if pos < 0 {
		return fmt.Errorf("editor: task %q is not queued", id)
	}
	target := pos + delta
	if target < 0 {
		target = 0
	}
	if target >= len(queuedIndices) {
		target = len(queuedIndices) - 1
	}
	if target == pos {
		return nil
	}
	a, b := queuedIndices[pos], queuedIndices[target]
	e.tasks.tasks[a], e.tasks.tasks[b] = e.tasks.tasks[b], e.tasks.tasks[a]
	_ = e.SaveAgentThread()
	e.notify()
	return nil
}

// TaskSummary returns the live counts across the five task groups plus the DockBadge count
// (NeedsYou + ReadyToReview + Failed; running and queued never add to the badge) and MostUrgent state.
func (e *Editor) TaskSummary() TaskSummary {
	if e == nil {
		return TaskSummary{}
	}
	var s TaskSummary
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if t.Dismissed {
			continue
		}
		switch t.State {
		case TaskNeedsYou:
			s.NeedsYou++
		case TaskRunning:
			s.Running++
			if t.BlockedOn != "" {
				s.Blocked++
			}
		case TaskReadyToReview:
			s.ReadyToReview++
		case TaskQueued:
			s.Queued++
		case TaskFailed:
			s.Failed++
		}
	}
	s.DockBadge = s.NeedsYou + s.ReadyToReview + s.Failed
	switch {
	case s.Failed > 0:
		s.MostUrgent = TaskFailed
	case s.NeedsYou > 0:
		s.MostUrgent = TaskNeedsYou
	case s.ReadyToReview > 0:
		s.MostUrgent = TaskReadyToReview
	case s.Running > 0:
		s.MostUrgent = TaskRunning
	case s.Queued > 0:
		s.MostUrgent = TaskQueued
	}
	return s
}

// TaskDockBadgeCount returns NeedsYou + ReadyToReview + Failed (running and queued never add to it).
func (e *Editor) TaskDockBadgeCount() int {
	return e.TaskSummary().DockBadge
}

// TaskElapsedString formats a task's elapsed time (e.g. "0:38") or fixed TimeLabel (e.g. "02:00").
func (e *Editor) TaskElapsedString(t AgentTask) string {
	if t.TimeLabel != "" {
		return t.TimeLabel
	}
	if t.State == TaskQueued {
		return ""
	}
	secs := t.ElapsedSeconds
	if secs <= 0 && t.StartedAt != "" {
		if st, err := time.Parse(time.RFC3339, t.StartedAt); err == nil {
			end := e.nowTime()
			if t.FinishedAt != "" {
				if ft, err := time.Parse(time.RFC3339, t.FinishedAt); err == nil {
					end = ft
				}
			}
			d := int(end.Sub(st).Seconds())
			if d >= 0 {
				secs = d
			}
		}
	}
	if secs < 0 {
		secs = 0
	}
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// TaskStatusParts builds the exact status bar indicator segments defined in docs/design/editor/Tasks.dc.html:
//   - Idle: "No tasks"
//   - Single running task (and nothing else active): "1 running · M:SS"
//   - Mixed: non-zero counts in urgency order (Failed, Needs you, Running, Ready to review, Queued); zero counts drop out.
func (e *Editor) TaskStatusParts() []TaskStatusPart {
	s := e.TaskSummary()
	total := s.Failed + s.NeedsYou + s.Running + s.ReadyToReview + s.Queued
	if total == 0 {
		return []TaskStatusPart{{State: "", Label: "No tasks"}}
	}
	if s.Running == 1 && total == 1 {
		for i := range e.tasks.tasks {
			t := e.tasks.tasks[i]
			if !t.Dismissed && t.State == TaskRunning {
				return []TaskStatusPart{{
					State: TaskRunning,
					Label: fmt.Sprintf("1 running · %s", e.TaskElapsedString(t)),
				}}
			}
		}
	}
	var parts []TaskStatusPart
	if s.Failed > 0 {
		parts = append(parts, TaskStatusPart{State: TaskFailed, Label: fmt.Sprintf("%d failed", s.Failed)})
	}
	if s.NeedsYou > 0 {
		parts = append(parts, TaskStatusPart{State: TaskNeedsYou, Label: fmt.Sprintf("%d needs you", s.NeedsYou)})
	}
	if s.Running > 0 {
		parts = append(parts, TaskStatusPart{State: TaskRunning, Label: fmt.Sprintf("%d running", s.Running)})
	}
	if s.ReadyToReview > 0 {
		parts = append(parts, TaskStatusPart{State: TaskReadyToReview, Label: fmt.Sprintf("%d to review", s.ReadyToReview)})
	}
	if s.Queued > 0 && len(parts) == 0 {
		parts = append(parts, TaskStatusPart{State: TaskQueued, Label: fmt.Sprintf("%d queued", s.Queued)})
	}
	return parts
}

// FormatTaskMeta builds the secondary metadata line for a task row matching Tasks.dc.html.
func (e *Editor) FormatTaskMeta(t AgentTask) string {
	var parts []string
	switch t.Source {
	case TaskSourcePlan:
		if t.PlanStep > 0 && t.PlanTotal > 0 {
			parts = append(parts, fmt.Sprintf("Plan step %d of %d", t.PlanStep, t.PlanTotal))
		} else {
			parts = append(parts, "Plan")
		}
	case TaskSourceAutomation:
		parts = append(parts, "Automation")
	default:
		parts = append(parts, "You")
	}

	switch t.State {
	case TaskNeedsYou:
		if t.PermissionPrompt != "" {
			parts = append(parts, t.PermissionPrompt)
		} else if t.StepSummary != "" {
			parts = append(parts, t.StepSummary)
		} else {
			parts = append(parts, "needs your permission")
		}
		if t.ScenePath != "" {
			parts = append(parts, shortSceneLabel(t.ScenePath))
		}
	case TaskRunning:
		if t.StartMode == TaskStartParallel {
			parts = append(parts, "parallel")
		}
		switch {
		case t.BlockedOn != "":
			parts = append(parts, "waiting on "+t.BlockedOn)
		case t.StepIndex > 0 && t.StepTotal > 0:
			parts = append(parts, fmt.Sprintf("step %d of %d", t.StepIndex, t.StepTotal))
		case t.StepSummary != "":
			parts = append(parts, t.StepSummary)
		}
		if t.BlockedOn == "" && t.ScenePath != "" {
			parts = append(parts, shortSceneLabel(t.ScenePath))
		}
	case TaskReadyToReview:
		if n := t.Review.EffectiveChangeCount(); n == 1 {
			parts = append(parts, "1 change")
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d changes", n))
		} else if t.Review != nil && t.Review.Summary != "" {
			parts = append(parts, t.Review.Summary)
		} else {
			parts = append(parts, "ready to review")
		}
		if t.ScenePath != "" {
			parts = append(parts, shortSceneLabel(t.ScenePath))
		}
	case TaskQueued:
		if t.StartsAfterTaskID != "" {
			if prev, ok := e.Task(t.StartsAfterTaskID); ok {
				if prev.Number > 0 {
					parts = append(parts, fmt.Sprintf("starts after task %d", prev.Number))
				} else {
					parts = append(parts, "starts after "+prev.Title)
				}
			} else {
				parts = append(parts, "queued")
			}
		} else {
			parts = append(parts, "queued")
		}
	case TaskFailed:
		if t.Error != "" {
			parts = append(parts, t.Error)
		} else {
			parts = append(parts, "failed")
		}
	}
	return strings.Join(parts, " · ")
}

func shortSceneLabel(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasSuffix(p, ".scene.yaml") {
		base := filepath.Base(p)
		return strings.TrimSuffix(base, ".scene.yaml")
	}
	return p
}

// TaskComposerHint returns the contextual Queue / Run in parallel hint shown under the composer.
func (e *Editor) TaskComposerHint() string {
	if e == nil {
		return ""
	}
	e.tasks.ensureInit()
	s := e.TaskSummary()
	var primary *AgentTask
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == TaskRunning && !t.Holding() {
			primary = t
			break
		}
	}
	if primary == nil {
		for i := range e.tasks.tasks {
			t := &e.tasks.tasks[i]
			if !t.Dismissed && (t.State == TaskRunning || (t.State == TaskReadyToReview && t.Source != TaskSourceAutomation)) {
				primary = t
				break
			}
		}
	}

	if e.tasks.composerMode == TaskStartQueue {
		if primary == nil {
			return "Starts right away; later queued requests wait until it is kept or reverted"
		}
		ref := primary.Title
		if primary.Number > 0 {
			ref = fmt.Sprintf("task %d", primary.Number)
		}
		if s.Queued > 0 {
			return fmt.Sprintf("Starts after %s is kept or reverted, behind %d queued", ref, s.Queued)
		}
		return fmt.Sprintf("Starts after %s is kept or reverted", ref)
	}
	if primary == nil {
		return "Starts now as its own session with a numbered overlay"
	}
	ref := primary.Title
	if primary.Number > 0 {
		ref = fmt.Sprintf("task %d", primary.Number)
	}
	if len(primary.EditingObjects) > 0 {
		return fmt.Sprintf("Starts now beside %s; holds if it needs %s", ref, primary.EditingObjects[0])
	}
	return fmt.Sprintf("Starts now beside %s; holds if it needs an edited object", ref)
}

func (e *Editor) taskMatchesOpenScene(t *AgentTask) bool {
	if e == nil || t == nil {
		return false
	}
	if t.ScenePath == "" {
		return true
	}
	if e.ScenePath != "" && (t.ScenePath == e.ScenePath || shortSceneLabel(t.ScenePath) == shortSceneLabel(e.ScenePath)) {
		return true
	}
	if e.Doc != nil && e.Doc.Name != "" && (t.ScenePath == e.Doc.Name || shortSceneLabel(t.ScenePath) == e.Doc.Name) {
		return true
	}
	return false
}

// ObjectTaskBadge returns the active numbered overlay for objectID in the open scene (editing takes precedence over holding).
func (e *Editor) ObjectTaskBadge(objectID string) (ObjectTaskOverlay, bool) {
	if e == nil || objectID == "" {
		return ObjectTaskOverlay{}, false
	}
	var holding *ObjectTaskOverlay
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if t.Dismissed || t.State != TaskRunning || !e.taskMatchesOpenScene(t) {
			continue
		}
		num := t.Number
		if num <= 0 {
			num = i + 1
		}
		if slices.Contains(t.EditingObjects, objectID) {
			lbl := t.StepSummary
			if lbl == "" {
				lbl = "editing " + objectID
			}
			return ObjectTaskOverlay{
				TaskID:     t.ID,
				TaskNumber: num,
				ObjectID:   objectID,
				Holding:    false,
				Label:      lbl,
			}, true
		}
		if holding == nil && slices.Contains(t.HoldingObjects, objectID) {
			lbl := "holding"
			if t.BlockedOn != "" {
				lbl = "waiting on " + t.BlockedOn
			}
			ov := ObjectTaskOverlay{
				TaskID:              t.ID,
				TaskNumber:          num,
				ObjectID:            objectID,
				Holding:             true,
				BlockedOn:           t.BlockedOn,
				BlockedByTaskNumber: t.BlockedByTaskNumber,
				Label:               lbl,
			}
			holding = &ov
		}
	}
	if holding != nil {
		return *holding, true
	}
	return ObjectTaskOverlay{}, false
}

// SceneTaskOverlays returns all active numbered overlays (editing and holding) for running tasks in the open scene.
func (e *Editor) SceneTaskOverlays() []ObjectTaskOverlay {
	if e == nil {
		return nil
	}
	var out []ObjectTaskOverlay
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if t.Dismissed || t.State != TaskRunning || !e.taskMatchesOpenScene(t) {
			continue
		}
		num := t.Number
		if num <= 0 {
			num = i + 1
		}
		for _, oid := range t.EditingObjects {
			lbl := t.StepSummary
			if lbl == "" {
				lbl = "editing " + oid
			}
			out = append(out, ObjectTaskOverlay{
				TaskID:     t.ID,
				TaskNumber: num,
				ObjectID:   oid,
				Holding:    false,
				Label:      lbl,
			})
		}
		for _, oid := range t.HoldingObjects {
			if slices.Contains(t.EditingObjects, oid) {
				continue
			}
			lbl := "holding"
			if t.BlockedOn != "" {
				lbl = "waiting on " + t.BlockedOn
			}
			out = append(out, ObjectTaskOverlay{
				TaskID:              t.ID,
				TaskNumber:          num,
				ObjectID:            oid,
				Holding:             true,
				BlockedOn:           t.BlockedOn,
				BlockedByTaskNumber: t.BlockedByTaskNumber,
				Label:               lbl,
			})
		}
	}
	return out
}
