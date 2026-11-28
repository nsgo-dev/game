package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// AgentThreadVersion is the schema version of *.thread.yaml files stored under .nsgo/threads/.
const AgentThreadVersion = 1

// DefaultAgentThreadID is the filename stem of the default project thread file (.nsgo/threads/default.thread.yaml).
const DefaultAgentThreadID = "default"

const (
	// maxParallelTasks caps how many background agent tasks may run concurrently.
	maxParallelTasks = 4
	// maxTasksPerThread caps total retained tasks per thread YAML document.
	maxTasksPerThread = 64
	// maxThreadFileBytes caps the maximum size of a *.thread.yaml file read from disk.
	maxThreadFileBytes = 2 << 20
	// defaultTaskRunnerTimeout bounds each background AgentTaskRunner execution.
	defaultTaskRunnerTimeout = 10 * time.Minute
)

// ErrTaskDecisionYouOnly is returned when an agent tries to keep a task's review, dismiss a task that is Ready to review, or answer a task's permission prompt: those decisions are the person's.
var ErrTaskDecisionYouOnly = errors.New("editor: keeping a task's review or answering its permission prompt is You only and cannot be done by an agent")

// taskDecisionByAgent reports whether the current call on the editor goroutine is an agent's MCP tool call, which is refused the person-only task decisions. It reads the per-call agentMutating flag set by RunAsAgent rather than AgentActorActive: agentActorDepth is one process-wide counter that a running automation holds for its whole run on a background goroutine, so it would also refuse the person's own clicks while any automation runs.
func (e *Editor) taskDecisionByAgent() bool {
	return e.AgentMutating()
}

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
	TaskKind            AgentTaskKind `yaml:"taskKind,omitempty" json:"taskKind,omitempty"`
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
	TurnID              string        `yaml:"turnId,omitempty" json:"turnId,omitempty"`
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
	TaskKind          AgentTaskKind
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

type taskToolContextKey struct{}

// WithTaskToolContext returns a derived context tagged with taskID so in-process MCP tool calls
// (such as those from the built-in agent runner) participate in editormcp's per-object holds,
// per-task review tracking, and Needs you permission prompts.
func WithTaskToolContext(ctx context.Context, taskID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if taskID == "" {
		return ctx
	}
	return context.WithValue(ctx, taskToolContextKey{}, taskID)
}

// TaskIDFromContext returns the background task ID attached by WithTaskToolContext, or "".
func TaskIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(taskToolContextKey{}).(string)
	return id
}

type agentTasksState struct {
	mu                 sync.Mutex
	scope              *core.Scope
	projectGen         uint64
	panelOpen          bool
	listExpanded       bool
	composerMode       TaskStartMode
	composerDraft      string
	focusedID          string
	threadID           string
	tasks              []AgentTask
	selectedDismiss    map[string]bool
	nextSeq            int
	nextNumber         int
	cancels            map[string]context.CancelFunc
	permWaiters        map[string]chan bool
	onHoldChange       func(taskID string)
	reviewSnapshotHook func(taskID string) (beforeYAML string, changes []TaskReviewChange, ok bool)
}

func (s *agentTasksState) setScope(cx *core.Scope) {
	s.mu.Lock()
	s.scope = cx
	s.mu.Unlock()
}

func (s *agentTasksState) currentGen() uint64 {
	s.mu.Lock()
	g := s.projectGen
	s.mu.Unlock()
	return g
}

func (s *agentTasksState) ensureInitLocked() {
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
	if s.permWaiters == nil {
		s.permWaiters = make(map[string]chan bool)
	}
}

func validAgentThreadID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func validTaskScenePath(p string) bool {
	if p == "" {
		return true
	}
	if strings.ContainsAny(p, "\\:") || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return false
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return false
	}
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return false
	}
	return fs.ValidPath(cleaned)
}

// ParseAgentThread decodes a *.thread.yaml document, refusing unknown fields, duplicate keys,
// YAML aliases/merge keys, and invalid versions or enum values.
func ParseAgentThread(data []byte) (*AgentThreadDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("write a thread document starting with version: %d and id", AgentThreadVersion), "editor: thread yaml is empty"), "", data, "")
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("editor: parse thread yaml: %w", err), "", data, "check the YAML syntax in the thread file")
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("write the thread file as a YAML mapping starting with version: %d", AgentThreadVersion), "editor: thread yaml must be a mapping document"), "", data, "")
	}
	if err := checkNoThreadYAMLReferences(&root); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	if err := checkDuplicateYAMLKeys(&root); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc AgentThreadDoc
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, loaderr.Locate(fmt.Errorf("editor: decode thread yaml: %w", err), "", data, "check the YAML syntax and field names in the thread file")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the thread file and remove any --- separator", "editor: thread yaml has unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("editor: decode thread yaml: %w", err), "", data, "remove trailing YAML content after the thread file")
	}
	if doc.Version != AgentThreadVersion {
		return nil, loaderr.Locate(loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the thread file", AgentThreadVersion), "editor: thread yaml version = %d, want %d", doc.Version, AgentThreadVersion), "", data, "")
	}
	if !validAgentThreadID(doc.ID) {
		return nil, loaderr.Locate(loaderr.Errorf("id", "set id to 1..64 ASCII letters, digits, '-' or '_'", "editor: thread yaml invalid id %q (must match ^[a-zA-Z0-9_-]{1,64}$)", doc.ID), "", data, "")
	}
	if len(doc.Tasks) > maxTasksPerThread {
		return nil, loaderr.Locate(loaderr.Errorf("tasks", fmt.Sprintf("keep at most %d tasks in one thread file", maxTasksPerThread), "editor: thread yaml has %d tasks, exceeds maximum %d", len(doc.Tasks), maxTasksPerThread), "", data, "")
	}
	seenID := make(map[string]bool, len(doc.Tasks))
	for i := range doc.Tasks {
		t := &doc.Tasks[i]
		tPath := fmt.Sprintf("tasks[%d]", i)
		if strings.TrimSpace(t.ID) == "" {
			return nil, loaderr.Locate(loaderr.Errorf(tPath+".id", "give each task a non-empty id", "editor: thread yaml task[%d] missing id", i), "", data, "")
		}
		if seenID[t.ID] {
			return nil, loaderr.Locate(loaderr.Errorf(tPath+".id", "give each task in the thread a unique id", "editor: thread yaml duplicate task id %q", t.ID), "", data, "")
		}
		seenID[t.ID] = true
		if strings.TrimSpace(t.Title) == "" {
			return nil, loaderr.Locate(loaderr.Errorf(tPath+".title", "give each task a non-empty title", "editor: thread yaml task %q missing title", t.ID), "", data, "")
		}
		if !t.Source.Valid() {
			return nil, loaderr.Locate(loaderr.Errorf(tPath+".source", "set source to a valid task source (chat, automation, or manual)", "editor: thread yaml task %q has unknown source %q", t.ID, t.Source), "", data, "")
		}
		if t.TaskKind != "" && !t.TaskKind.Valid() {
			return nil, fmt.Errorf("editor: thread yaml task %q has unknown taskKind %q", t.ID, t.TaskKind)
		}
		if !t.StartMode.Valid() {
			return nil, loaderr.Locate(loaderr.Errorf(tPath+".startMode", "set startMode to a valid task start mode", "editor: thread yaml task %q has unknown startMode %q", t.ID, t.StartMode), "", data, "")
		}
		if !t.State.Valid() {
			return nil, loaderr.Locate(loaderr.Errorf(tPath+".state", "set state to a valid task state", "editor: thread yaml task %q has unknown state %q", t.ID, t.State), "", data, "")
		}
		if t.Number < 0 || t.ElapsedSeconds < 0 || t.StepIndex < 0 || t.StepTotal < 0 || t.PlanStep < 0 || t.PlanTotal < 0 {
			return nil, loaderr.Locate(loaderr.Errorf(tPath, "set numeric task counters and timings to non-negative values", "editor: thread yaml task %q has negative numeric field", t.ID), "", data, "")
		}
		if !validTaskScenePath(t.ScenePath) {
			return nil, loaderr.Locate(loaderr.Errorf(tPath+".scenePath", "set scenePath to a relative slash-separated path inside the project", "editor: thread yaml task %q has invalid scene path %q", t.ID, t.ScenePath), "", data, "")
		}
		if t.Review != nil {
			for j, ch := range t.Review.Changes {
				switch ch.Kind {
				case "", "added", "changed", "removed":
				default:
					return nil, loaderr.Locate(loaderr.Errorf(fmt.Sprintf("%s.review.changes[%d].kind", tPath, j), "set kind to added, changed, or removed (or omit it)", "editor: thread yaml task %q review change[%d] has unknown kind %q", t.ID, j, ch.Kind), "", data, "")
				}
			}
		}
	}
	return &doc, nil
}

func checkNoThreadYAMLReferences(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: "editor: thread yaml must not use YAML aliases",
			Fix:     "expand YAML aliases inline instead of using *",
		}
	}
	if n.Anchor != "" {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: "editor: thread yaml must not use YAML anchors",
			Fix:     "remove YAML anchors (&name) and write values inline",
		}
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Tag == "!!merge" {
				return &loaderr.Error{
					Line:    n.Content[i].Line,
					Column:  n.Content[i].Column,
					Problem: "editor: thread yaml must not use YAML merge keys",
					Fix:     "remove YAML merge keys (<<) and list fields explicitly",
				}
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
	if !validAgentThreadID(cp.ID) {
		return nil, fmt.Errorf("editor: invalid thread id %q", cp.ID)
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
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	id := e.tasks.threadID
	e.tasks.mu.Unlock()
	if !validAgentThreadID(id) {
		return ""
	}
	return filepath.Join(dir, id+".thread.yaml")
}

// SaveAgentThread writes the current task list to its plain-text YAML thread file when a project or ThreadsDir is set.
func (e *Editor) SaveAgentThread() error {
	if e == nil {
		return nil
	}
	dir := e.effectiveThreadsDir()
	if dir == "" {
		return nil
	}
	doc := e.AgentThreadDoc()
	if !validAgentThreadID(doc.ID) {
		return fmt.Errorf("editor: invalid thread id %q", doc.ID)
	}
	data, err := EncodeAgentThread(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_, abs, err := resolveProjectFileWithinRoot(dir, doc.ID+".thread.yaml", true)
	if err != nil {
		return err
	}
	return writeFileAtomic(abs, data)
}

// AgentThreadDoc returns a snapshot of the editor's current background tasks as an AgentThreadDoc.
func (e *Editor) AgentThreadDoc() *AgentThreadDoc {
	if e == nil {
		return &AgentThreadDoc{Version: AgentThreadVersion, ID: DefaultAgentThreadID}
	}
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	id := e.tasks.threadID
	tasks := make([]AgentTask, len(e.tasks.tasks))
	for i := range e.tasks.tasks {
		tasks[i] = e.tasks.tasks[i].clone()
	}
	e.tasks.mu.Unlock()
	scene := e.ScenePath
	if scene == "" && e.Doc != nil {
		scene = e.Doc.Name
	}
	return &AgentThreadDoc{
		Version: AgentThreadVersion,
		ID:      id,
		Scene:   scene,
		Tasks:   tasks,
	}
}

func readBoundedThreadFile(filePath, threadsDir string) ([]byte, error) {
	if threadsDir != "" {
		dirAbs, err := filepath.Abs(threadsDir)
		if err != nil {
			return nil, err
		}
		resolvedDir := dirAbs
		if r, rerr := filepath.EvalSymlinks(dirAbs); rerr == nil {
			resolvedDir = r
		}
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return nil, err
		}
		resolvedPath, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			return nil, err
		}
		if !isSubpath(resolvedDir, resolvedPath) {
			return nil, fmt.Errorf("editor: thread file %q escapes threads directory", filePath)
		}
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxThreadFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxThreadFileBytes {
		return nil, fmt.Errorf("editor: thread file %q exceeds maximum size (%d bytes)", filePath, maxThreadFileBytes)
	}
	return data, nil
}

// LoadAgentThread loads tasks from path (a *.thread.yaml file) into the editor.
func (e *Editor) LoadAgentThread(filePath string) error {
	if e == nil {
		return nil
	}
	return e.loadAgentThreadUnlocked(filePath)
}

func (e *Editor) loadAgentThreadUnlocked(filePath string) error {
	data, err := readBoundedThreadFile(filePath, e.effectiveThreadsDir())
	if err != nil {
		return err
	}
	doc, err := ParseAgentThread(data)
	if err != nil {
		return err
	}
	oldIDs := e.stopAllTaskRunners()
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	e.tasks.projectGen++
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
	e.tasks.mu.Unlock()
	for _, id := range oldIDs {
		e.fireHoldRelease(id)
	}
	e.resetTaskNotificationTrackingForTasks()
	e.syncDockBadgeIfAttached()
	e.notify()
	return nil
}

func (e *Editor) resetTasksForProject() {
	if e == nil {
		return
	}
	e.stopAllTaskRunners()
	e.tasks.mu.Lock()
	oldIDs := make([]string, 0, len(e.tasks.tasks))
	for i := range e.tasks.tasks {
		oldIDs = append(oldIDs, e.tasks.tasks[i].ID)
	}
	e.tasks.projectGen++
	e.tasks.tasks = nil
	e.tasks.selectedDismiss = make(map[string]bool)
	e.tasks.focusedID = ""
	// Never reset nextSeq across project switches so a stale goroutine can never collide on IDs.
	e.tasks.nextNumber = 0
	e.tasks.threadID = DefaultAgentThreadID
	e.tasks.mu.Unlock()
	for _, id := range oldIDs {
		e.fireHoldRelease(id)
	}
	e.resetTaskNotificationTrackingForTasks()
	if threadPath := e.AgentThreadFilePath(); threadPath != "" {
		if _, err := os.Stat(threadPath); err == nil {
			_ = e.loadAgentThreadUnlocked(threadPath)
		}
	}
	e.syncDockBadgeIfAttached()
}

func (e *Editor) stopAllTaskRunners() []string {
	if e == nil {
		return nil
	}
	e.tasks.mu.Lock()
	e.tasks.projectGen++
	cancels := make([]context.CancelFunc, 0, len(e.tasks.cancels))
	ids := make([]string, 0, len(e.tasks.cancels))
	for id, cancel := range e.tasks.cancels {
		ids = append(ids, id)
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
		delete(e.tasks.cancels, id)
	}
	waiters := make([]chan bool, 0, len(e.tasks.permWaiters))
	for id, ch := range e.tasks.permWaiters {
		waiters = append(waiters, ch)
		delete(e.tasks.permWaiters, id)
	}
	e.tasks.mu.Unlock()
	for _, ch := range waiters {
		close(ch)
	}
	for _, cancel := range cancels {
		cancel()
	}
	return ids
}

// StopRunningTasks stops every currently running background task without disturbing reviewable turns.
func (e *Editor) StopRunningTasks() {
	if e == nil {
		return
	}
	e.tasks.mu.Lock()
	var runningIDs []string
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == TaskRunning {
			runningIDs = append(runningIDs, t.ID)
		}
	}
	e.tasks.mu.Unlock()
	for _, id := range runningIDs {
		_ = e.StopTask(id)
	}
}

// stopRunningTasksForPlay cancels any running background task runners and releases their holds
// before Play mode snapshots and simulates the scene.
func (e *Editor) stopRunningTasksForPlay() {
	if e == nil {
		return
	}
	e.StopRunningTasks()
	ids := e.stopAllTaskRunners()
	for _, id := range ids {
		e.fireHoldRelease(id)
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

// SetTaskReviewSnapshotHook registers a callback queried when the default task runner finishes
// a task turn so edits recorded on its editormcp session populate the task's separate TaskReview.
func (e *Editor) SetTaskReviewSnapshotHook(fn func(taskID string) (beforeYAML string, changes []TaskReviewChange, ok bool)) {
	if e == nil {
		return
	}
	e.tasks.mu.Lock()
	e.tasks.reviewSnapshotHook = fn
	e.tasks.mu.Unlock()
}

func (e *Editor) snapshotTaskReviewFromMCP(taskID string) (string, []TaskReviewChange, bool) {
	if e == nil || taskID == "" {
		return "", nil, false
	}
	e.tasks.mu.Lock()
	fn := e.tasks.reviewSnapshotHook
	e.tasks.mu.Unlock()
	if fn == nil {
		return "", nil, false
	}
	return fn(taskID)
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
	if e == nil {
		return false
	}
	e.tasks.mu.Lock()
	open := e.tasks.panelOpen
	e.tasks.mu.Unlock()
	return open
}

// SetTasksPanelOpen opens or closes the Agent / Background Tasks panel.
func (e *Editor) SetTasksPanelOpen(open bool) {
	if e == nil {
		return
	}
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	if e.tasks.panelOpen == open {
		e.tasks.mu.Unlock()
		return
	}
	e.tasks.panelOpen = open
	if open && !e.tasks.listExpanded {
		e.tasks.listExpanded = true
	}
	e.ensureFocusedTaskLocked()
	e.tasks.mu.Unlock()
	e.notify()
}

// ToggleTasksPanel toggles the Agent / Background Tasks panel open or closed.
func (e *Editor) ToggleTasksPanel() {
	if e == nil {
		return
	}
	e.SetTasksPanelOpen(!e.TasksPanelOpen())
}

// TasksListExpanded reports whether the Tasks section inside the Agent panel is expanded.
func (e *Editor) TasksListExpanded() bool {
	if e == nil {
		return false
	}
	e.tasks.mu.Lock()
	exp := e.tasks.listExpanded
	e.tasks.mu.Unlock()
	return exp
}

// SetTasksListExpanded expands or collapses the Tasks list inside the Agent panel.
func (e *Editor) SetTasksListExpanded(expanded bool) {
	if e == nil {
		return
	}
	e.tasks.mu.Lock()
	if e.tasks.listExpanded == expanded {
		e.tasks.mu.Unlock()
		return
	}
	e.tasks.listExpanded = expanded
	e.tasks.mu.Unlock()
	e.notify()
}

// ToggleTasksListExpanded flips the expanded state of the Tasks list inside the Agent panel.
func (e *Editor) ToggleTasksListExpanded() {
	if e == nil {
		return
	}
	e.SetTasksListExpanded(!e.TasksListExpanded())
}

// TaskComposerMode returns the selected start mode (Queue or Run in parallel) in the Tasks composer.
func (e *Editor) TaskComposerMode() TaskStartMode {
	if e == nil {
		return TaskStartQueue
	}
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	mode := e.tasks.composerMode
	e.tasks.mu.Unlock()
	return mode
}

// SetTaskComposerMode sets the selected start mode (Queue or Run in parallel) in the Tasks composer.
func (e *Editor) SetTaskComposerMode(mode TaskStartMode) {
	if e == nil || !mode.Valid() {
		return
	}
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	if e.tasks.composerMode == mode {
		e.tasks.mu.Unlock()
		return
	}
	e.tasks.composerMode = mode
	e.tasks.mu.Unlock()
	e.notify()
}

// TaskComposerDraft returns the current prompt draft in the Tasks panel composer.
func (e *Editor) TaskComposerDraft() string {
	if e == nil {
		return ""
	}
	e.tasks.mu.Lock()
	draft := e.tasks.composerDraft
	e.tasks.mu.Unlock()
	return draft
}

// SetTaskComposerDraft updates the prompt draft in the Tasks panel composer.
func (e *Editor) SetTaskComposerDraft(draft string) {
	if e == nil {
		return
	}
	e.tasks.mu.Lock()
	if e.tasks.composerDraft == draft {
		e.tasks.mu.Unlock()
		return
	}
	e.tasks.composerDraft = draft
	e.tasks.mu.Unlock()
	e.notify()
}

// SubmitComposerTask submits the current composer draft using TaskComposerMode and clears the draft on success.
func (e *Editor) SubmitComposerTask() (AgentTask, error) {
	if e == nil {
		return AgentTask{}, errors.New("editor: nil editor")
	}
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	prompt := strings.TrimSpace(e.tasks.composerDraft)
	mode := e.tasks.composerMode
	e.tasks.mu.Unlock()
	if prompt == "" {
		return AgentTask{}, errors.New("editor: task prompt is empty")
	}
	task, err := e.SubmitTask(TaskRequest{
		Title:     firstLineTitle(prompt),
		Prompt:    prompt,
		Source:    TaskSourceUser,
		StartMode: mode,
	})
	if err != nil {
		return AgentTask{}, err
	}
	e.tasks.mu.Lock()
	if strings.TrimSpace(e.tasks.composerDraft) == prompt {
		e.tasks.composerDraft = ""
	}
	e.tasks.mu.Unlock()
	e.notify()
	return task, nil
}

func firstLineTitle(prompt string) string {
	line := strings.TrimSpace(prompt)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	runes := []rune(line)
	if len(runes) > 72 {
		line = strings.TrimSpace(string(runes[:69])) + "…"
	}
	return line
}

func (e *Editor) findTaskIndexLocked(id string) int {
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
	e.tasks.mu.Lock()
	out := make([]AgentTask, len(e.tasks.tasks))
	for i := range e.tasks.tasks {
		out[i] = e.tasks.tasks[i].clone()
	}
	e.tasks.mu.Unlock()
	return out
}

// VisibleTasks returns all non-dismissed tasks belonging to the five active groups.
func (e *Editor) VisibleTasks() []AgentTask {
	if e == nil {
		return nil
	}
	e.tasks.mu.Lock()
	var out []AgentTask
	for i := range e.tasks.tasks {
		if e.tasks.tasks[i].Visible() {
			out = append(out, e.tasks.tasks[i].clone())
		}
	}
	e.tasks.mu.Unlock()
	return out
}

// TasksInState returns all visible (non-dismissed) tasks currently in state.
func (e *Editor) TasksInState(state TaskState) []AgentTask {
	if e == nil {
		return nil
	}
	e.tasks.mu.Lock()
	var out []AgentTask
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == state {
			out = append(out, t.clone())
		}
	}
	e.tasks.mu.Unlock()
	return out
}

// Task returns the task with id, if present.
func (e *Editor) Task(id string) (AgentTask, bool) {
	if e == nil || id == "" {
		return AgentTask{}, false
	}
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return AgentTask{}, false
	}
	cp := e.tasks.tasks[idx].clone()
	e.tasks.mu.Unlock()
	return cp, true
}

// FocusedTask returns the currently focused task in the Tasks panel.
func (e *Editor) FocusedTask() (AgentTask, bool) {
	if e == nil {
		return AgentTask{}, false
	}
	e.tasks.mu.Lock()
	defer e.tasks.mu.Unlock()
	if e.tasks.focusedID != "" {
		if idx := e.findTaskIndexLocked(e.tasks.focusedID); idx >= 0 && !e.tasks.tasks[idx].Dismissed {
			return e.tasks.tasks[idx].clone(), true
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
	e.tasks.mu.Lock()
	if e.findTaskIndexLocked(id) < 0 || e.tasks.focusedID == id {
		e.tasks.mu.Unlock()
		return
	}
	e.tasks.focusedID = id
	e.tasks.mu.Unlock()
	e.notify()
}

func (e *Editor) ensureFocusedTaskLocked() {
	if e.tasks.focusedID != "" {
		if idx := e.findTaskIndexLocked(e.tasks.focusedID); idx >= 0 && !e.tasks.tasks[idx].Dismissed {
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
// scene (strictly confined inside the open project root) and selects its first edited/blocked object.
func (e *Editor) OpenTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	task := e.tasks.tasks[idx].clone()
	e.tasks.panelOpen = true
	e.tasks.listExpanded = true
	e.tasks.focusedID = id
	e.tasks.mu.Unlock()

	// Switch to or open the task's scene if it names a project scene file (strictly confined to project root).
	if task.ScenePath != "" && e.ProjectPath != "" && strings.HasSuffix(task.ScenePath, ".scene.yaml") {
		_, abs, err := resolveProjectFileWithinRoot(filepath.Dir(e.ProjectPath), task.ScenePath, false)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else if filepath.Clean(abs) != filepath.Clean(e.FilePath) {
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

// FocusedTaskID returns the ID of the currently focused task, or empty string.
func (e *Editor) FocusedTaskID() string {
	if e == nil {
		return ""
	}
	e.tasks.mu.Lock()
	id := e.tasks.focusedID
	e.tasks.mu.Unlock()
	return id
}

// validateTaskAgentRoute checks whether the editor can run a background task through its default
// agent runner (using the same route resolution as the agent composer). When no agent is connected,
// it opens the Connect dialog and returns an error so no Running task is created.
func (e *Editor) validateTaskAgentRoute(taskKind AgentTaskKind) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	return e.validateTaskAgentRouteLocked(taskKind)
}

func (e *Editor) validateTaskAgentRouteLocked(taskKind AgentTaskKind) error {
	if e.agent.mode == AgentModeNone {
		return errors.New("editor: agent is disabled")
	}
	if !taskKind.Valid() {
		taskKind = AgentTaskSceneEdit
	}
	route, _, _, _, err := e.resolveRouteAndRunner(taskKind)
	if err != nil {
		if route == AgentRouteNone && !e.ConnectDialogOpen() {
			e.OpenConnectDialog()
		}
		return err
	}
	if route.IsBuiltIn() && !e.HasAgentAPIKey(DefaultAgentAPIKeyAccount) {
		e.OpenConnectDialogForMode(AgentModeAPIKey)
		return ErrAgentAPIKeyMissing
	}
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
	if req.TaskKind != "" && !req.TaskKind.Valid() {
		return AgentTask{}, fmt.Errorf("editor: unknown task kind %q", req.TaskKind)
	}
	mode := req.StartMode
	if mode == "" {
		mode = TaskStartQueue
	}
	if !mode.Valid() {
		return AgentTask{}, fmt.Errorf("editor: unknown task start mode %q", mode)
	}
	if req.InitialState != "" && !req.InitialState.Valid() {
		return AgentTask{}, fmt.Errorf("editor: unknown task state %q", req.InitialState)
	}
	if !validTaskScenePath(req.ScenePath) {
		return AgentTask{}, fmt.Errorf("editor: invalid task scene path %q", req.ScenePath)
	}

	// Refuse runnable tasks when no agent is connected (showing the Connect dialog just like the agent composer).
	if req.InitialState == "" && e.AgentTaskRunner == nil {
		if err := e.validateTaskAgentRoute(req.TaskKind); err != nil {
			return AgentTask{}, err
		}
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

	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()

	// Enforce per-thread task cap, pruning oldest inactive/dismissed entries first if possible.
	if len(e.tasks.tasks) >= maxTasksPerThread {
		pruned := false
		for i := range e.tasks.tasks {
			t := &e.tasks.tasks[i]
			if t.Dismissed || t.State == TaskDone || t.State == TaskStopped {
				delete(e.tasks.selectedDismiss, t.ID)
				e.tasks.tasks = append(e.tasks.tasks[:i], e.tasks.tasks[i+1:]...)
				pruned = true
				break
			}
		}
		if !pruned {
			e.tasks.mu.Unlock()
			return AgentTask{}, fmt.Errorf("editor: maximum %d tasks per thread reached; dismiss or complete existing tasks first", maxTasksPerThread)
		}
	}

	// Enforce bounded concurrency for parallel tasks.
	if req.InitialState == "" && mode == TaskStartParallel {
		running := e.countRunningTasksLocked()
		if running >= maxParallelTasks {
			e.tasks.mu.Unlock()
			return AgentTask{}, fmt.Errorf("editor: at most %d parallel tasks can run at once; wait for one to finish or queue this request", maxParallelTasks)
		}
	}

	e.tasks.nextSeq++
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = fmt.Sprintf("task-%d", e.tasks.nextSeq)
	}
	if e.findTaskIndexLocked(id) >= 0 {
		e.tasks.mu.Unlock()
		return AgentTask{}, fmt.Errorf("editor: duplicate task id %q", id)
	}
	num := req.Number
	if num <= 0 && (source != TaskSourceAutomation || mode == TaskStartParallel || req.InitialState == TaskRunning) {
		e.tasks.nextNumber++
		num = e.tasks.nextNumber
	} else if num > e.tasks.nextNumber {
		e.tasks.nextNumber = num
	}

	task := AgentTask{
		ID:               id,
		Number:           num,
		Title:            title,
		Prompt:           prompt,
		Source:           source,
		TaskKind:         req.TaskKind,
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
		if holderIdx := e.findTaskIndexLocked(task.BlockedByTaskID); holderIdx >= 0 {
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
		shouldLaunch = !e.playing
	} else {
		blockerID := req.StartsAfterTaskID
		if blockerID == "" {
			blockerID = e.findQueueBlockerIDLocked()
		}
		if blockerID != "" {
			task.State = TaskQueued
			task.StartsAfterTaskID = blockerID
		} else {
			task.State = TaskRunning
			task.StartedAt = now.UTC().Format(time.RFC3339)
			task.SessionID = "session-" + id
			shouldLaunch = !e.playing
		}
	}

	e.tasks.tasks = append(e.tasks.tasks, task)
	if e.tasks.focusedID == "" || task.State == TaskRunning {
		e.tasks.focusedID = task.ID
	}
	ret := task.clone()
	e.tasks.mu.Unlock()

	_ = e.SaveAgentThread()
	if ret.State.IsNotifiable() {
		e.onAgentTaskStateUpdated(ret, "", now)
	}
	e.syncDockBadgeIfAttached()
	e.notify()

	if shouldLaunch {
		e.launchTaskRunner(ret.ID)
		if updated, ok := e.Task(ret.ID); ok {
			ret = updated
		}
	}
	return ret, nil
}

func (e *Editor) countRunningTasksLocked() int {
	n := 0
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == TaskRunning {
			n++
		}
	}
	return n
}

// findQueueBlockerIDLocked returns the task ID that a newly queued request must wait behind:
// the last queued task if any exist, otherwise a running, needs-you, or unreviewed non-automation turn.
func (e *Editor) findQueueBlockerIDLocked() string {
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

func (e *Editor) isTaskStillBlockingLocked(id string) bool {
	if id == "" {
		return false
	}
	idx := e.findTaskIndexLocked(id)
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

func (e *Editor) hasAnyForegroundBlockerLocked(excludeID string) bool {
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
	if e == nil || e.playing {
		return
	}
	now := e.nowTime()
	var startedID string
	e.tasks.mu.Lock()
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if t.Dismissed || t.State != TaskQueued {
			continue
		}
		if t.StartsAfterTaskID != "" && t.StartsAfterTaskID != resolvedID && e.isTaskStillBlockingLocked(t.StartsAfterTaskID) {
			continue
		}
		if e.hasAnyForegroundBlockerLocked(t.ID) {
			continue
		}
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
		startedID = t.ID
		break
	}
	e.tasks.mu.Unlock()
	if startedID == "" {
		return
	}
	_ = e.SaveAgentThread()
	e.notify()
	e.launchTaskRunner(startedID)
}

func (e *Editor) launchTaskRunner(taskID string) {
	if e == nil || e.playing {
		return
	}
	task, ok := e.Task(taskID)
	if !ok {
		return
	}
	if e.AgentTaskRunner == nil {
		e.launchDefaultTaskRunner(task)
		return
	}
	if e.Project != nil && e.ProjectPath != "" && e.ActiveTurnID() == "" {
		if task.Source == TaskSourceAutomation {
			_, _ = e.TakeCheckpointBeforeAutomation(task.Title)
		} else {
			_, _ = e.TakeCheckpointBeforeTask(task.Title)
		}
	}
	runner := e.AgentTaskRunner
	ctx, cancel := context.WithTimeout(context.Background(), defaultTaskRunnerTimeout)
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	gen := e.tasks.projectGen
	if prev := e.tasks.cancels[taskID]; prev != nil {
		prev()
	}
	e.tasks.cancels[taskID] = cancel
	e.tasks.mu.Unlock()

	post := func(fn func()) {
		if e.tasks.currentGen() != gen {
			return
		}
		e.postOrRun(func() {
			if e.tasks.currentGen() != gen {
				return
			}
			fn()
		})
	}

	go func() {
		err := runner(ctx, task)
		e.tasks.mu.Lock()
		delete(e.tasks.cancels, taskID)
		stale := e.tasks.projectGen != gen
		e.tasks.mu.Unlock()
		cancel()
		if stale {
			return
		}

		post(func() {
			if e.tasks.currentGen() != gen {
				return
			}
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

// launchDefaultTaskRunner executes task through the same agent route as the agent composer
// (resolveRouteAndRunner: CLI runner or built-in runner) in its own session and AgentTurn,
// streaming steps, Needs you, Ready to review, and Failed through the existing task transitions.
func (e *Editor) launchDefaultTaskRunner(task AgentTask) {
	if e == nil || e.playing {
		return
	}
	taskKind := task.TaskKind
	if !taskKind.Valid() {
		taskKind = AgentTaskSceneEdit
	}

	if e.Project != nil && e.ProjectPath != "" && e.ActiveTurnID() == "" {
		if task.Source == TaskSourceAutomation {
			_, _ = e.TakeCheckpointBeforeAutomation(task.Title)
		} else {
			_, _ = e.TakeCheckpointBeforeTask(task.Title)
		}
	}

	if e.agent.mode == AgentModeNone {
		_ = e.FailTask(task.ID, "editor: agent is disabled")
		return
	}
	route, runner, clientName, model, err := e.resolveRouteAndRunner(taskKind)
	if err != nil {
		if route == AgentRouteNone && !e.ConnectDialogOpen() {
			e.OpenConnectDialog()
		}
		_ = e.FailTask(task.ID, err.Error())
		return
	}
	if route.IsBuiltIn() && !e.HasAgentAPIKey(DefaultAgentAPIKeyAccount) {
		e.OpenConnectDialogForMode(AgentModeAPIKey)
		_ = e.FailTask(task.ID, ErrAgentAPIKeyMissing.Error())
		return
	}

	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(task.Title)
	}
	attachedImgs, briefImgs, allImgs := e.collectRequestReferenceImages()
	req := e.AssembleAgentRequest(prompt)
	taskThreadID := "task-" + task.ID
	scenePath := task.ScenePath
	if scenePath == "" {
		scenePath = e.ScenePath
	}
	agentKind := ExternalAgentAuto
	if !route.IsBuiltIn() {
		if c, ok := e.AgentPrimaryClient(); ok {
			agentKind = DetectExternalAgentKind(c.Name)
		}
	}
	req.ThreadID = taskThreadID
	// Always start a fresh CLI session for each background/parallel task rather than resuming the foreground thread.
	req.SessionID = ""
	req.TaskID = task.ID
	req.ProjectRoot = e.agentProjectRoot()
	req.ScenePath = scenePath
	req.SelectedID = e.SelectedID
	req.AgentKind = agentKind
	req.Permissions = e.agent.permissions
	req.Task = taskKind
	req.Route = route
	req.Model = model
	req.PriorTurns = nil
	req.AttachedImages = attachedImgs
	req.BriefImages = briefImgs
	req.Images = allImgs

	var beforeSceneYAML string
	if raw, yerr := e.SaveYAML(); yerr == nil {
		beforeSceneYAML = string(raw)
	}

	now := e.nowTime()
	startHist := 0
	if e.History != nil {
		startHist = e.History.Index()
	}
	e.agent.turnSeq++
	e.agent.turnChangeSeq++
	turnID := fmt.Sprintf("turn-%d", e.agent.turnSeq)
	turnImages := make([]AgentReferenceImage, len(allImgs))
	for i, img := range allImgs {
		cp := img.clone()
		cp.Data = nil
		turnImages[i] = cp
	}
	e.agent.turns = append(e.agent.turns, AgentTurn{
		ID:        turnID,
		Prompt:    prompt,
		Images:    turnImages,
		Source:    "task",
		Status:    "running",
		AgentName: clientName,
		Task:      taskKind,
		Route:     route,
		Model:     model,
		Steps: []AgentStep{{
			ID:        fmt.Sprintf("%s-step-start", turnID),
			Title:     "Starting request…",
			Status:    AgentStepPlaceholder,
			StartedAt: now,
		}},
		CreatedAt:           now,
		StartedAt:           now,
		ThreadID:            taskThreadID,
		ReviewState:         AgentReviewRunning,
		turnStartHistoryIdx: startHist,
	})
	e.trimAgentTurnAttachments()
	e.initTurnBaseline(&e.agent.turns[len(e.agent.turns)-1])
	e.RecordAgentRequest(1)

	if builtIn, ok := runner.(*builtInAgentRunner); ok {
		builtIn.turnID = turnID
		builtIn.taskID = task.ID
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTaskRunnerTimeout)
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	gen := e.tasks.projectGen
	if prev := e.tasks.cancels[task.ID]; prev != nil {
		prev()
	}
	e.tasks.cancels[task.ID] = cancel
	if idx := e.findTaskIndexLocked(task.ID); idx >= 0 {
		e.tasks.tasks[idx].TurnID = turnID
	}
	e.tasks.mu.Unlock()
	e.notify()

	req.MCPServers = e.AgentMCPServers()
	go func() {
		runCtx := WithTaskToolContext(ctx, task.ID)
		runErr := runner.Run(runCtx, req, func(ev AgentEvent) {
			e.postOrRun(func() {
				if e.tasks.currentGen() != gen {
					return
				}
				e.applyDefaultTaskEvent(task.ID, turnID, ev)
			})
		})
		wasCanceled := errors.Is(ctx.Err(), context.Canceled)
		e.tasks.mu.Lock()
		delete(e.tasks.cancels, task.ID)
		stale := e.tasks.projectGen != gen
		e.tasks.mu.Unlock()
		cancel()
		if stale {
			return
		}
		e.postOrRun(func() {
			if e.tasks.currentGen() != gen {
				return
			}
			e.finishDefaultTaskRun(task.ID, turnID, beforeSceneYAML, runErr, wasCanceled)
		})
	}()
}

// applyDefaultTaskEvent runs on the UI scope (or on the single headless executor when scope == nil).
func (e *Editor) applyDefaultTaskEvent(taskID, turnID string, ev AgentEvent) {
	resolvedStepTitle := strings.TrimSpace(ev.StepTitle)
	turn := e.findAgentTurn(turnID)
	if turn != nil && !turn.Done {
		switch ev.Kind {
		case AgentEventSession:
			if ev.SessionID != "" {
				turn.SessionIDAfter = ev.SessionID
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
			if ev.StepID != "" {
				for i := range turn.Steps {
					if turn.Steps[i].ID == ev.StepID {
						if ev.StepTitle != "" {
							turn.Steps[i].Title = ev.StepTitle
						} else if resolvedStepTitle == "" {
							resolvedStepTitle = strings.TrimSpace(turn.Steps[i].Title)
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
					ID:        stepID,
					Title:     ev.StepTitle,
					Detail:    ev.StepDetail,
					ToolName:  ev.ToolName,
					Status:    stepStatus,
					StartedAt: e.nowTime(),
				})
			}
			if stepStatus == AgentStepWaiting {
				turn.ReviewState = AgentReviewNeedsYou
			}
			e.observeTurnSceneMutations(turn, max(1, len(turn.Steps)))
			e.refreshTurnReview(turn)
		case AgentEventMessage:
			if ev.Text != "" && len(turn.Summary) < maxAgentTurnReplyBytes {
				chunk := ev.Text
				if turn.Summary != "" && !strings.HasSuffix(turn.Summary, "\n") && !strings.HasPrefix(chunk, "\n") {
					chunk = "\n" + chunk
				}
				rem := maxAgentTurnReplyBytes - len(turn.Summary)
				if len(chunk) > rem {
					chunk = chunk[:rem]
				}
				turn.Summary += chunk
			}
		case AgentEventUsage:
			turn.InputTokens += ev.InputTokens
			turn.OutputTokens += ev.OutputTokens
		case AgentEventError:
			if ev.Err != "" {
				turn.Error = ev.Err
			}
		case AgentEventModelRecovered:
			turn.ModelError = nil
		case AgentEventDone:
			dropTurnPlaceholder(turn)
			if ev.Text != "" && turn.Summary == "" {
				txt := ev.Text
				if len(txt) > maxAgentTurnReplyBytes {
					txt = txt[:maxAgentTurnReplyBytes]
				}
				turn.Summary = txt
				turn.Reply = txt
			}
			turn.ModelError = nil
			turn.Done = true
			turn.Status = "done"
			turn.FinishedAt = e.nowTime()
			e.observeTurnSceneMutations(turn, max(1, len(turn.Steps)))
			e.refreshTurnReview(turn)
		}
		e.agent.turnChangeSeq++
	}

	switch ev.Kind {
	case AgentEventSession:
		if ev.SessionID != "" {
			e.tasks.mu.Lock()
			if idx := e.findTaskIndexLocked(taskID); idx >= 0 {
				e.tasks.tasks[idx].SessionID = ev.SessionID
			}
			e.tasks.mu.Unlock()
		}
	case AgentEventStep:
		stepStatus := ev.StepStatus
		if stepStatus == "" {
			stepStatus = AgentStepRunning
		}
		if stepStatus == AgentStepWaiting {
			_ = e.RequestTaskPermission(taskID, ev.StepTitle, ev.StepDetail)
			return
		}
		title := resolvedStepTitle
		if title == "" {
			title = strings.TrimSpace(ev.ToolName)
		}
		e.applyTaskStepEvent(taskID, ev.StepIndex, ev.StepTotal, title, stepStatus == AgentStepDone || stepStatus == AgentStepFailed)
	case AgentEventMessage:
		if line := firstLineTitle(ev.Text); line != "" {
			if cur, ok := e.Task(taskID); ok && cur.State == TaskRunning && cur.BlockedOn == "" {
				_ = e.UpdateTaskStep(taskID, 0, 0, line)
				return
			}
		}
	}
	e.notify()
}

func (e *Editor) applyTaskStepEvent(taskID string, stepIndex, stepTotal int, summary string, done bool) {
	if e == nil || taskID == "" {
		return
	}
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(taskID)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return
	}
	t := &e.tasks.tasks[idx]
	if stepIndex > 0 {
		t.StepIndex = stepIndex
	}
	if stepTotal > 0 {
		t.StepTotal = stepTotal
	}
	if summary != "" {
		if t.BlockedOn == "" {
			t.StepSummary = summary
		}
		updated := false
		for i := len(t.Steps) - 1; i >= 0; i-- {
			if t.Steps[i].Active && t.Steps[i].Summary == summary {
				t.Steps[i].Active = !done
				t.Steps[i].Done = done
				updated = true
				break
			}
		}
		if !updated {
			if !done {
				for i := range t.Steps {
					if t.Steps[i].Active {
						t.Steps[i].Active = false
						t.Steps[i].Done = true
					}
				}
			}
			t.Steps = append(t.Steps, TaskStep{
				Summary: summary,
				Done:    done,
				Active:  !done,
			})
		}
	}
	e.tasks.mu.Unlock()
	_ = e.SaveAgentThread()
	e.notify()
}

// finishDefaultTaskRun runs on the UI scope (or on the single headless executor when scope == nil).
func (e *Editor) finishDefaultTaskRun(taskID, turnID, beforeSceneYAML string, runErr error, canceled bool) {
	var (
		turnSummary string
		turnErr     string
	)
	if turn := e.findAgentTurn(turnID); turn != nil {
		if !turn.Done {
			dropTurnPlaceholder(turn)
			turn.Done = true
			turn.FinishedAt = e.nowTime()
		}
		if canceled || errors.Is(runErr, context.Canceled) {
			turn.Stopped = true
			turn.Status = "stopped"
			turn.ReviewState = AgentReviewDismissed
		} else if runErr != nil || turn.Error != "" {
			if runErr != nil && turn.Error == "" {
				turn.Error = runErr.Error()
			}
			turn.Status = "done"
			turn.ReviewState = AgentReviewFailed
		} else {
			turn.Status = "done"
			if turn.ReviewState != AgentReviewKept && turn.ReviewState != AgentReviewDismissed {
				turn.ReviewState = AgentReviewReady
			}
		}
		if turn.ModelError != nil {
			turn.ModelError.Retrying = false
		}
		for i := range turn.Steps {
			if turn.Steps[i].Status == AgentStepRunning {
				if canceled || errors.Is(runErr, context.Canceled) {
					turn.Steps[i].Status = AgentStepPending
				} else if runErr != nil || turn.Error != "" {
					turn.Steps[i].Status = AgentStepFailed
				} else {
					turn.Steps[i].Status = AgentStepDone
				}
			}
		}
		e.observeTurnSceneMutations(turn, max(1, len(turn.Steps)))
		e.refreshTurnReview(turn)
		if turn.Reply == "" && turn.Summary != "" {
			turn.Reply = turn.Summary
		}
		turnSummary = turn.Summary
		turnErr = turn.Error
		e.agent.turnChangeSeq++
	}

	cur, ok := e.Task(taskID)
	if !ok {
		e.fireHoldRelease(taskID)
		e.notify()
		return
	}
	// If the task was already transitioned (e.g. StopTask, CompleteTaskTurn, FailTask), respect it.
	if cur.State != TaskRunning && cur.State != TaskNeedsYou {
		e.fireHoldRelease(taskID)
		e.notify()
		return
	}

	if canceled || errors.Is(runErr, context.Canceled) {
		_ = e.StopTask(taskID)
		return
	}
	if runErr != nil || turnErr != "" {
		errMsg := turnErr
		if errMsg == "" && runErr != nil {
			errMsg = runErr.Error()
		}
		_ = e.FailTask(taskID, errMsg)
		return
	}

	summary := strings.TrimSpace(turnSummary)
	if summary == "" {
		summary = cur.StepSummary
	}
	revBefore := beforeSceneYAML
	var changes []TaskReviewChange
	if mcpBefore, mcpChanges, hasMCP := e.snapshotTaskReviewFromMCP(taskID); hasMCP {
		if mcpBefore != "" {
			revBefore = mcpBefore
		}
		changes = mcpChanges
	}
	var afterSceneYAML string
	if raw, yerr := e.SaveYAML(); yerr == nil {
		afterSceneYAML = string(raw)
	}
	if len(changes) == 0 && revBefore != "" && afterSceneYAML != "" && revBefore != afterSceneYAML {
		changes = e.deriveTaskReviewChanges(cur.ScenePath, revBefore, afterSceneYAML)
	}
	_ = e.CompleteTaskTurn(taskID, TaskReview{
		ID:              "review-" + taskID,
		TaskID:          taskID,
		ScenePath:       cur.ScenePath,
		Summary:         summary,
		ChangeCount:     len(changes),
		Changes:         changes,
		BeforeSceneYAML: revBefore,
		AfterSceneYAML:  afterSceneYAML,
	})
}

func (e *Editor) deriveTaskReviewChanges(scenePath, beforeYAML, afterYAML string) []TaskReviewChange {
	if e == nil || beforeYAML == "" || afterYAML == "" || beforeYAML == afterYAML {
		return nil
	}
	parseOpts := sceneformat.ParseOptions{
		Components:    e.components,
		PhysicsLayers: e.physicsLayers,
	}
	beforeDoc, err := sceneformat.ParseWith([]byte(beforeYAML), parseOpts)
	if err != nil {
		return nil
	}
	afterDoc, err := sceneformat.ParseWith([]byte(afterYAML), parseOpts)
	if err != nil {
		return nil
	}
	allIDs := append(collectAllObjectIDs(beforeDoc), collectAllObjectIDs(afterDoc)...)
	seen := make(map[string]bool, len(allIDs))
	var changes []TaskReviewChange
	for _, oid := range allIDs {
		if oid == "" || seen[oid] {
			continue
		}
		seen[oid] = true
		bObj, bLayer, bParent := findObjectDocInScene(beforeDoc, oid)
		aObj, aLayer, aParent := findObjectDocInScene(afterDoc, oid)
		if objectDocsEqual(bObj, aObj) && bLayer == aLayer && bParent == aParent {
			continue
		}
		kind := "changed"
		summary := "changed " + oid
		switch {
		case bObj == nil && aObj != nil:
			kind = "added"
			summary = "added " + oid
		case bObj != nil && aObj == nil:
			kind = "removed"
			summary = "removed " + oid
		}
		changes = append(changes, TaskReviewChange{
			ObjectID: oid,
			File:     scenePath,
			Kind:     kind,
			Summary:  summary,
		})
	}
	return changes
}

func (e *Editor) markTaskTurnReviewState(turnID string, state AgentReviewState) {
	if e == nil || turnID == "" {
		return
	}
	turn := e.findAgentTurn(turnID)
	if turn == nil {
		return
	}
	turn.ReviewState = state
	if turn.Review != nil {
		if state == AgentReviewKept {
			turn.Review.Kept = true
			turn.Review.Discarded = false
		} else if state == AgentReviewDismissed {
			turn.Review.Kept = false
			turn.Review.Discarded = true
		}
	}
	e.agent.turnChangeSeq++
	e.notify()
}

// UpdateTaskStep updates the current step progress text and step numbers on task id.
func (e *Editor) UpdateTaskStep(id string, stepIndex, stepTotal int, summary string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
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
	e.tasks.mu.Unlock()
	_ = e.SaveAgentThread()
	e.notify()
	return nil
}

// AddTaskStep appends a step entry to task id's detail log (marking previous active steps done when done is false).
func (e *Editor) AddTaskStep(id string, summary string, done bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
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
	e.tasks.mu.Unlock()
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
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	t.EditingObjects = slices.Clone(editing)
	t.HoldingObjects = slices.Clone(holding)
	t.BlockedOn = blockedOn
	t.BlockedByTaskID = blockedByTaskID
	t.BlockedByTaskNumber = 0
	if blockedByTaskID != "" {
		if holderIdx := e.findTaskIndexLocked(blockedByTaskID); holderIdx >= 0 {
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
	e.tasks.mu.Unlock()
	_ = e.SaveAgentThread()
	e.notify()
	return nil
}

// ClearTaskHoldState clears editing/holding/blocked state on task id and triggers the hold release hook.
func (e *Editor) ClearTaskHoldState(id string) {
	if e == nil {
		return
	}
	changed := false
	e.tasks.mu.Lock()
	if idx := e.findTaskIndexLocked(id); idx >= 0 {
		t := &e.tasks.tasks[idx]
		t.EditingObjects = nil
		t.HoldingObjects = nil
		t.BlockedOn = ""
		t.BlockedByTaskID = ""
		t.BlockedByTaskNumber = 0
		changed = true
	}
	e.tasks.mu.Unlock()
	if changed {
		_ = e.SaveAgentThread()
		e.notify()
	}
	e.fireHoldRelease(id)
}

// BeginTaskPermissionAsk transitions task id to TaskNeedsYou with the given prompt/detail and
// returns a channel that receives true (Allow once) or false (Deny) when ResolveTaskPermission
// is called, or closes when the task is stopped, failed, or cancelled.
func (e *Editor) BeginTaskPermissionAsk(id, prompt, detail string) (<-chan bool, func(), error) {
	if e == nil {
		return nil, func() {}, errors.New("editor: nil editor")
	}
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return nil, func() {}, fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	prevState := t.State
	t.State = TaskNeedsYou
	t.PermissionPrompt = prompt
	t.PermissionDetail = detail
	if prompt != "" {
		t.StepSummary = prompt
	}
	if prev := e.tasks.permWaiters[id]; prev != nil {
		close(prev)
	}
	ch := make(chan bool, 1)
	e.tasks.permWaiters[id] = ch
	taskCopy := t.clone()
	e.tasks.mu.Unlock()
	_ = e.SaveAgentThread()
	e.onAgentTaskStateUpdated(taskCopy, prevState, e.nowTime())
	e.syncDockBadgeIfAttached()
	e.notify()
	cancel := func() {
		e.tasks.mu.Lock()
		if cur, ok := e.tasks.permWaiters[id]; ok && cur == ch {
			delete(e.tasks.permWaiters, id)
		}
		e.tasks.mu.Unlock()
	}
	return ch, cancel, nil
}

// RequestTaskPermission transitions task id to TaskNeedsYou with the given permission prompt.
func (e *Editor) RequestTaskPermission(id, prompt, detail string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	prevState := t.State
	t.State = TaskNeedsYou
	t.PermissionPrompt = prompt
	t.PermissionDetail = detail
	if prompt != "" {
		t.StepSummary = prompt
	}
	taskCopy := t.clone()
	e.tasks.mu.Unlock()
	_ = e.SaveAgentThread()
	e.onAgentTaskStateUpdated(taskCopy, prevState, e.nowTime())
	e.syncDockBadgeIfAttached()
	e.notify()
	return nil
}

// ResolveTaskPermission resolves a TaskNeedsYou permission ask, resuming the task when allowed or recording the refusal and resuming/notifying the optional callback. Answering the ask is the person's decision: an agent actor gets ErrTaskDecisionYouOnly, for allow and deny alike.
func (e *Editor) ResolveTaskPermission(id string, allow bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.taskDecisionByAgent() {
		return ErrTaskDecisionYouOnly
	}
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	if t.State != TaskNeedsYou {
		st := t.State
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q is not waiting for permission (state=%s)", id, st)
	}
	prevState := t.State
	t.State = TaskRunning
	t.PermissionPrompt = ""
	t.PermissionDetail = ""
	if allow {
		t.Steps = append(t.Steps, TaskStep{Summary: "Permission allowed once", Done: true})
	} else {
		t.Steps = append(t.Steps, TaskStep{Summary: "Permission denied", Done: true})
	}
	waiter := e.tasks.permWaiters[id]
	delete(e.tasks.permWaiters, id)
	taskCopy := t.clone()
	e.tasks.mu.Unlock()
	_ = e.SaveAgentThread()
	e.onAgentTaskStateUpdated(taskCopy, prevState, time.Time{})
	e.syncDockBadgeIfAttached()
	e.notify()
	cb := e.OnTaskPermissionDecision
	if waiter != nil {
		select {
		case waiter <- allow:
		default:
		}
	}
	if cb != nil {
		cb(id, allow)
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
	now := e.nowTime()
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	prevState := t.State
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
	taskCopy := t.clone()
	e.tasks.mu.Unlock()

	if !cp.Kept {
		e.cancelAutoSaveTimer()
	}
	_ = e.SaveAgentThread()
	e.onAgentTaskStateUpdated(taskCopy, prevState, time.Time{})
	e.syncDockBadgeIfAttached()
	e.notify()
	e.fireHoldRelease(id)
	return nil
}

// HasOpenTaskReview reports whether any non-dismissed background agent task is
// waiting in TaskReadyToReview with an unkept review. Autosave pauses while
// this is true so unkept task edits are never flushed to disk prematurely.
func (e *Editor) HasOpenTaskReview() bool {
	if e == nil {
		return false
	}
	e.tasks.mu.Lock()
	defer e.tasks.mu.Unlock()
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == TaskReadyToReview && (t.Review == nil || !t.Review.Kept) {
			return true
		}
	}
	return false
}

// FailTask moves task id to TaskFailed, releases its holds, and keeps it visible until opened, retried, or dismissed.
func (e *Editor) FailTask(id, errMsg string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	now := e.nowTime()
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	prevState := t.State
	t.State = TaskFailed
	t.Error = errMsg
	t.EditingObjects = nil
	t.HoldingObjects = nil
	t.BlockedOn = ""
	t.BlockedByTaskID = ""
	t.BlockedByTaskNumber = 0
	t.FinishedAt = now.UTC().Format(time.RFC3339)
	waiter := e.tasks.permWaiters[id]
	delete(e.tasks.permWaiters, id)
	taskCopy := t.clone()
	e.tasks.mu.Unlock()
	if waiter != nil {
		close(waiter)
	}
	_ = e.SaveAgentThread()
	e.onAgentTaskStateUpdated(taskCopy, prevState, time.Time{})
	e.syncDockBadgeIfAttached()
	e.notify()
	e.fireHoldRelease(id)
	return nil
}

// StopTask cancels a running, waiting, or queued task, releases any object holds, and advances the queue. A task that is Ready to review is refused, for every caller: stopping it would end its review without reverting its edits, which is a keep that bypasses KeepTask; the person keeps or reverts it instead.
func (e *Editor) StopTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	now := e.nowTime()
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	if e.tasks.tasks[idx].State == TaskReadyToReview {
		e.tasks.mu.Unlock()
		if e.taskDecisionByAgent() {
			return fmt.Errorf("editor: task %q is ready to review and cannot be stopped: %w", id, ErrTaskDecisionYouOnly)
		}
		return fmt.Errorf("editor: task %q is ready to review and cannot be stopped; keep or revert it", id)
	}
	cancel := e.tasks.cancels[id]
	delete(e.tasks.cancels, id)
	waiter := e.tasks.permWaiters[id]
	delete(e.tasks.permWaiters, id)
	t := &e.tasks.tasks[idx]
	prevState := t.State
	t.State = TaskStopped
	t.EditingObjects = nil
	t.HoldingObjects = nil
	t.BlockedOn = ""
	t.BlockedByTaskID = ""
	t.BlockedByTaskNumber = 0
	t.FinishedAt = now.UTC().Format(time.RFC3339)
	delete(e.tasks.selectedDismiss, id)
	e.ensureFocusedTaskLocked()
	taskCopy := t.clone()
	e.tasks.mu.Unlock()

	if waiter != nil {
		close(waiter)
	}
	if cancel != nil {
		cancel()
	}
	_ = e.SaveAgentThread()
	e.onAgentTaskStateUpdated(taskCopy, prevState, time.Time{})
	e.syncDockBadgeIfAttached()
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
	if e.AgentTaskRunner == nil {
		if existing, ok := e.Task(id); ok && existing.State == TaskFailed {
			if err := e.validateTaskAgentRoute(existing.TaskKind); err != nil {
				return err
			}
		}
	}
	now := e.nowTime()
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	if t.State != TaskFailed {
		st := t.State
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q is not failed (state=%s)", id, st)
	}
	if t.StartMode == TaskStartParallel && e.countRunningTasksLocked() >= maxParallelTasks {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: at most %d parallel tasks can run at once; wait for one to finish before retrying", maxParallelTasks)
	}
	prevState := t.State
	t.Error = ""
	t.Dismissed = false
	t.FinishedAt = ""
	t.ElapsedSeconds = 0
	shouldLaunch := false
	if t.StartMode == TaskStartParallel || !e.hasAnyForegroundBlockerLocked(t.ID) {
		t.State = TaskRunning
		t.StartedAt = now.UTC().Format(time.RFC3339)
		if t.SessionID == "" {
			t.SessionID = "session-" + t.ID
		}
		if t.Number <= 0 {
			e.tasks.nextNumber++
			t.Number = e.tasks.nextNumber
		}
		shouldLaunch = !e.playing
	} else {
		t.State = TaskQueued
		t.StartsAfterTaskID = e.findQueueBlockerIDLocked()
	}
	e.tasks.focusedID = t.ID
	taskCopy := t.clone()
	taskID := t.ID
	e.tasks.mu.Unlock()
	_ = e.SaveAgentThread()
	e.onAgentTaskStateUpdated(taskCopy, prevState, time.Time{})
	e.syncDockBadgeIfAttached()
	e.notify()
	if shouldLaunch {
		e.launchTaskRunner(taskID)
	}
	return nil
}

// KeepTask accepts a TaskReadyToReview turn, marks its review kept, transitions it to TaskDone, and starts the next queued task waiting for it. Keeping is the person's decision: an agent actor gets ErrAutomationYouOnly for an automation task and ErrTaskDecisionYouOnly for any other.
func (e *Editor) KeepTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	t := &e.tasks.tasks[idx]
	if t.Source == TaskSourceAutomation && e.AgentActorActive() {
		e.tasks.mu.Unlock()
		return ErrAutomationYouOnly
	}
	if e.taskDecisionByAgent() {
		e.tasks.mu.Unlock()
		if t.Source == TaskSourceAutomation {
			return ErrAutomationYouOnly
		}
		return ErrTaskDecisionYouOnly
	}
	if t.State != TaskReadyToReview {
		st := t.State
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q is not ready to review (state=%s)", id, st)
	}
	prevState := t.State
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
	taskCopy := t.clone()
	e.tasks.mu.Unlock()
	_ = e.SaveAgentThread()
	e.scheduleAutoSaveIfNeeded()
	e.onAgentTaskStateUpdated(taskCopy, prevState, time.Time{})
	e.syncDockBadgeIfAttached()
	e.notify()
	if taskCopy.TurnID != "" {
		e.markTaskTurnReviewState(taskCopy.TurnID, AgentReviewKept)
	}
	e.advanceTaskQueue(id)
	return nil
}

// RevertTask reverts an unkept TaskReadyToReview turn (patching only objects touched by this task
// so edits from other parallel tasks or human edits are preserved), marks it TaskStopped, and
// starts the next queued task waiting for it. A turn already kept is never reverted by RevertTask.
func (e *Editor) RevertTask(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.tasks.mu.Lock()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q not found", id)
	}
	tCopy := e.tasks.tasks[idx].clone()
	if tCopy.State != TaskReadyToReview {
		e.tasks.mu.Unlock()
		return fmt.Errorf("editor: task %q is not ready to review (state=%s)", id, tCopy.State)
	}
	keptObjects := e.keptObjectsByOtherTasksLocked(id)
	hasOtherKept := e.hasOtherKeptTaskLocked(id)
	e.tasks.mu.Unlock()

	if tCopy.Review != nil && !tCopy.Review.Kept {
		if err := e.applyTaskReviewRevert(&tCopy, keptObjects, hasOtherKept); err != nil {
			return err
		}
	}

	var updatedTask AgentTask
	hasUpdated := false
	e.tasks.mu.Lock()
	if curIdx := e.findTaskIndexLocked(id); curIdx >= 0 {
		t := &e.tasks.tasks[curIdx]
		if t.Review != nil && !t.Review.Kept {
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
		updatedTask = t.clone()
		hasUpdated = true
	}
	e.tasks.mu.Unlock()
	_ = e.SaveAgentThread()
	e.scheduleAutoSaveIfNeeded()
	if hasUpdated {
		e.onAgentTaskStateUpdated(updatedTask, tCopy.State, time.Time{})
	}
	e.syncDockBadgeIfAttached()
	e.notify()
	if hasUpdated && updatedTask.TurnID != "" {
		e.markTaskTurnReviewState(updatedTask.TurnID, AgentReviewDismissed)
	}
	e.advanceTaskQueue(id)
	return nil
}

func (e *Editor) keptObjectsByOtherTasksLocked(excludeID string) map[string]bool {
	out := make(map[string]bool)
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if t.ID == excludeID || t.Review == nil || !t.Review.Kept {
			continue
		}
		for _, ch := range t.Review.Changes {
			if ch.ObjectID != "" && !ch.Reverted {
				out[ch.ObjectID] = true
			}
		}
	}
	return out
}

func (e *Editor) hasOtherKeptTaskLocked(excludeID string) bool {
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if t.ID != excludeID && t.Review != nil && t.Review.Kept {
			return true
		}
	}
	return false
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

func findObjectDocInSlice(objs []sceneformat.ObjectDoc, id, layerName, parentID string) (*sceneformat.ObjectDoc, string, string) {
	for i := range objs {
		if objs[i].ID == id {
			return &objs[i], layerName, parentID
		}
		if len(objs[i].Children) > 0 {
			if found, l, p := findObjectDocInSlice(objs[i].Children, id, layerName, objs[i].ID); found != nil {
				return found, l, p
			}
		}
	}
	return nil, "", ""
}

func findObjectDocInScene(doc *sceneformat.SceneDoc, id string) (*sceneformat.ObjectDoc, string, string) {
	if doc == nil || id == "" {
		return nil, "", ""
	}
	for i := range doc.Layers {
		if found, l, p := findObjectDocInSlice(doc.Layers[i].Objects, id, doc.Layers[i].Name, ""); found != nil {
			return found, l, p
		}
	}
	return nil, "", ""
}

func removeObjectDocFromSlice(objs []sceneformat.ObjectDoc, id string) ([]sceneformat.ObjectDoc, bool) {
	for i := range objs {
		if objs[i].ID == id {
			return append(objs[:i], objs[i+1:]...), true
		}
		if len(objs[i].Children) > 0 {
			if next, removed := removeObjectDocFromSlice(objs[i].Children, id); removed {
				objs[i].Children = next
				return objs, true
			}
		}
	}
	return objs, false
}

func removeObjectDocFromScene(doc *sceneformat.SceneDoc, id string) bool {
	if doc == nil || id == "" {
		return false
	}
	for i := range doc.Layers {
		if next, removed := removeObjectDocFromSlice(doc.Layers[i].Objects, id); removed {
			doc.Layers[i].Objects = next
			return true
		}
	}
	return false
}

func objectDocsEqual(a, b *sceneformat.ObjectDoc) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ra, errA := yaml.Marshal(a)
	rb, errB := yaml.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ra, rb)
}

func insertObjectDocInScene(doc *sceneformat.SceneDoc, obj sceneformat.ObjectDoc, layerName, parentID string) {
	if doc == nil || len(doc.Layers) == 0 {
		return
	}
	if parentID != "" {
		if parent, _, _ := findObjectDocInScene(doc, parentID); parent != nil {
			parent.Children = append(parent.Children, obj)
			return
		}
	}
	for i := range doc.Layers {
		if doc.Layers[i].Name == layerName {
			doc.Layers[i].Objects = append(doc.Layers[i].Objects, obj)
			return
		}
	}
	doc.Layers[0].Objects = append(doc.Layers[0].Objects, obj)
}

func collectAllObjectIDs(doc *sceneformat.SceneDoc) []string {
	if doc == nil {
		return nil
	}
	var ids []string
	var walk func(objs []sceneformat.ObjectDoc)
	walk = func(objs []sceneformat.ObjectDoc) {
		for i := range objs {
			if objs[i].ID != "" {
				ids = append(ids, objs[i].ID)
			}
			if len(objs[i].Children) > 0 {
				walk(objs[i].Children)
			}
		}
	}
	for i := range doc.Layers {
		walk(doc.Layers[i].Objects)
	}
	return ids
}

func (e *Editor) applyTaskReviewRevert(t *AgentTask, keptObjects map[string]bool, hasOtherKept bool) error {
	if e == nil || t == nil || t.Review == nil || t.Review.Kept {
		return nil
	}
	if t.Review.BeforeSceneYAML == "" || e.Doc == nil {
		return nil
	}
	if t.ScenePath != "" && e.ScenePath != "" && t.ScenePath != e.ScenePath {
		return nil
	}
	parseOpts := sceneformat.ParseOptions{
		Components:    e.components,
		PhysicsLayers: e.physicsLayers,
	}
	beforeDoc, err := sceneformat.ParseWith([]byte(t.Review.BeforeSceneYAML), parseOpts)
	if err != nil {
		return err
	}
	var afterDoc *sceneformat.SceneDoc
	if t.Review.AfterSceneYAML != "" {
		if parsed, perr := sceneformat.ParseWith([]byte(t.Review.AfterSceneYAML), parseOpts); perr == nil {
			afterDoc = parsed
		}
	}

	// Collect the deterministic set of object IDs touched by this task.
	var touched []string
	seen := make(map[string]bool)
	for _, ch := range t.Review.Changes {
		if ch.ObjectID != "" && !ch.Kept && !seen[ch.ObjectID] {
			seen[ch.ObjectID] = true
			touched = append(touched, ch.ObjectID)
		}
	}
	if len(touched) == 0 && afterDoc != nil {
		// Derive touched object IDs by diffing beforeDoc vs afterDoc so we still only patch this task's objects.
		allIDs := append(collectAllObjectIDs(beforeDoc), collectAllObjectIDs(afterDoc)...)
		for _, oid := range allIDs {
			if seen[oid] {
				continue
			}
			bObj, bLayer, bParent := findObjectDocInScene(beforeDoc, oid)
			aObj, aLayer, aParent := findObjectDocInScene(afterDoc, oid)
			if !objectDocsEqual(bObj, aObj) || bLayer != aLayer || bParent != aParent {
				seen[oid] = true
				touched = append(touched, oid)
			}
		}
	}

	if len(touched) == 0 {
		// Fallback only when no per-object metadata exists AND no other kept tasks could be clobbered.
		if hasOtherKept {
			return nil
		}
		e.History.Execute(&taskSceneRevertCmd{
			ed:     e,
			label:  "Revert agent task: " + t.Title,
			before: e.Doc,
			after:  beforeDoc,
		})
		return nil
	}

	// Deep-clone current scene so we patch ONLY this task's untouched-since-turn objects.
	curBytes, err := sceneformat.Encode(e.Doc)
	if err != nil {
		return err
	}
	patchedDoc, err := sceneformat.ParseWith(curBytes, parseOpts)
	if err != nil {
		return err
	}

	applied := 0
	var skipped []string
	for _, oid := range touched {
		beforeObj, beforeLayer, beforeParent := findObjectDocInScene(beforeDoc, oid)
		curObj, curLayer, curParent := findObjectDocInScene(patchedDoc, oid)

		// Protect any object modified later by another task (kept or parallel) or human edit.
		if afterDoc != nil {
			afterObj, afterLayer, afterParent := findObjectDocInScene(afterDoc, oid)
			if !objectDocsEqual(curObj, afterObj) || curLayer != afterLayer || curParent != afterParent {
				skipped = append(skipped, oid)
				continue
			}
		} else if keptObjects[oid] {
			skipped = append(skipped, oid)
			continue
		}

		switch {
		case beforeObj == nil && curObj != nil:
			// Added by this task -> remove it.
			if removeObjectDocFromScene(patchedDoc, oid) {
				applied++
			}
		case beforeObj != nil && curObj != nil:
			// Modified (and possibly reparented/moved across layers) -> restore exact pre-task object.
			restored := beforeObj.Clone()
			if curLayer == beforeLayer && curParent == beforeParent {
				*curObj = restored
			} else {
				removeObjectDocFromScene(patchedDoc, oid)
				insertObjectDocInScene(patchedDoc, restored, beforeLayer, beforeParent)
			}
			applied++
		case beforeObj != nil && curObj == nil:
			// Removed by this task -> re-insert it.
			restored := beforeObj.Clone()
			insertObjectDocInScene(patchedDoc, restored, beforeLayer, beforeParent)
			applied++
		}
	}

	if applied == 0 {
		if len(skipped) > 0 {
			e.Status = fmt.Sprintf("Skipped reverting %s (modified by a newer edit)", strings.Join(skipped, ", "))
		}
		return nil
	}

	e.History.Execute(&taskSceneRevertCmd{
		ed:     e,
		label:  "Revert agent task: " + t.Title,
		before: e.Doc,
		after:  patchedDoc,
	})
	if len(skipped) > 0 {
		e.Status = fmt.Sprintf("Reverted %d object(s); kept newer edits on %s", applied, strings.Join(skipped, ", "))
	}
	return nil
}

// DismissTasks dismisses one or more Ready to review or Failed tasks from the list. Dismissing NEVER reverts anything that was kept (nor mutates kept scene edits or undo history). An agent actor may dismiss only Failed tasks; its Ready to review ids are skipped.
func (e *Editor) DismissTasks(ids []string) int {
	if e == nil || len(ids) == 0 {
		return 0
	}
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	dismissed := 0
	var resolved []string
	for _, id := range ids {
		idx := e.findTaskIndexLocked(id)
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
		// Dismissing a Ready to review task hides its unkept changes from review, so only the person may do it.
		if t.State == TaskReadyToReview && e.taskDecisionByAgent() {
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
	}
	e.tasks.mu.Unlock()
	if dismissed > 0 {
		_ = e.SaveAgentThread()
		for _, id := range resolved {
			e.clearTaskNotificationForID(id)
		}
		e.syncDockBadgeIfAttached()
		e.notify()
	}
	if dismissed > 0 {
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
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	idx := e.findTaskIndexLocked(id)
	if idx < 0 || e.tasks.tasks[idx].State != TaskReadyToReview || e.tasks.tasks[idx].Dismissed {
		e.tasks.mu.Unlock()
		return
	}
	if selected {
		e.tasks.selectedDismiss[id] = true
	} else {
		delete(e.tasks.selectedDismiss, id)
	}
	e.tasks.mu.Unlock()
	e.notify()
}

// ToggleTaskDismissSelection toggles whether a Ready to review task is checked for bulk dismissal.
func (e *Editor) ToggleTaskDismissSelection(id string) {
	if e == nil || id == "" {
		return
	}
	e.SetTaskDismissSelected(id, !e.TaskDismissSelected(id))
}

// TaskDismissSelected reports whether task id is currently checked for bulk dismissal.
func (e *Editor) TaskDismissSelected(id string) bool {
	if e == nil || id == "" {
		return false
	}
	e.tasks.mu.Lock()
	ok := e.tasks.selectedDismiss[id]
	e.tasks.mu.Unlock()
	return ok
}

// SelectedDismissTaskIDs returns the IDs of all Ready to review tasks currently checked for bulk dismissal, in list order.
func (e *Editor) SelectedDismissTaskIDs() []string {
	if e == nil {
		return nil
	}
	e.tasks.mu.Lock()
	var out []string
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == TaskReadyToReview && e.tasks.selectedDismiss[t.ID] {
			out = append(out, t.ID)
		}
	}
	e.tasks.mu.Unlock()
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
	e.tasks.mu.Lock()
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
		e.tasks.mu.Unlock()
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
		e.tasks.mu.Unlock()
		return nil
	}
	a, b := queuedIndices[pos], queuedIndices[target]
	e.tasks.tasks[a], e.tasks.tasks[b] = e.tasks.tasks[b], e.tasks.tasks[a]
	e.tasks.mu.Unlock()
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
	e.tasks.mu.Lock()
	s := e.taskSummaryLocked()
	e.tasks.mu.Unlock()
	return s
}

func (e *Editor) taskSummaryLocked() TaskSummary {
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

// TaskStatusParts builds the exact status bar indicator segments defined in design/editor/Tasks.dc.html:
//   - Idle: "No tasks"
//   - Single running task (and nothing else active): "1 running · M:SS"
//   - Mixed: non-zero counts in urgency order (Failed, Needs you, Running, Ready to review, Queued); zero counts drop out.
func (e *Editor) TaskStatusParts() []TaskStatusPart {
	if e == nil {
		return []TaskStatusPart{{State: "", Label: e.T("tasksExtra.noTasks")}}
	}
	e.tasks.mu.Lock()
	s := e.taskSummaryLocked()
	total := s.Failed + s.NeedsYou + s.Running + s.ReadyToReview + s.Queued
	if total == 0 {
		e.tasks.mu.Unlock()
		return []TaskStatusPart{{State: "", Label: e.T("tasksExtra.noTasks")}}
	}
	if s.Running == 1 && total == 1 {
		for i := range e.tasks.tasks {
			t := e.tasks.tasks[i]
			if !t.Dismissed && t.State == TaskRunning {
				e.tasks.mu.Unlock()
				return []TaskStatusPart{{
					State: TaskRunning,
					Label: e.T("tasksExtra.oneRunningElapsed", l10n.Param("elapsed", e.TaskElapsedString(t))),
				}}
			}
		}
	}
	e.tasks.mu.Unlock()
	var parts []TaskStatusPart
	if s.Failed > 0 {
		parts = append(parts, TaskStatusPart{State: TaskFailed, Label: e.T("tasksExtra.countFailed", l10n.Param("count", s.Failed))})
	}
	if s.NeedsYou > 0 {
		parts = append(parts, TaskStatusPart{State: TaskNeedsYou, Label: e.T("tasksExtra.countNeedsYou", l10n.Param("count", s.NeedsYou))})
	}
	if s.Running > 0 {
		parts = append(parts, TaskStatusPart{State: TaskRunning, Label: e.T("tasksExtra.countRunning", l10n.Param("count", s.Running))})
	}
	if s.ReadyToReview > 0 {
		parts = append(parts, TaskStatusPart{State: TaskReadyToReview, Label: e.T("tasksExtra.countToReview", l10n.Param("count", s.ReadyToReview))})
	}
	if s.Queued > 0 && len(parts) == 0 {
		parts = append(parts, TaskStatusPart{State: TaskQueued, Label: e.T("tasksExtra.countQueued", l10n.Param("count", s.Queued))})
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
	e.tasks.mu.Lock()
	e.tasks.ensureInitLocked()
	s := e.taskSummaryLocked()
	mode := e.tasks.composerMode
	var primary *AgentTask
	for i := range e.tasks.tasks {
		t := &e.tasks.tasks[i]
		if !t.Dismissed && t.State == TaskRunning && !t.Holding() {
			cp := t.clone()
			primary = &cp
			break
		}
	}
	if primary == nil {
		for i := range e.tasks.tasks {
			t := &e.tasks.tasks[i]
			if !t.Dismissed && (t.State == TaskRunning || (t.State == TaskReadyToReview && t.Source != TaskSourceAutomation)) {
				cp := t.clone()
				primary = &cp
				break
			}
		}
	}
	e.tasks.mu.Unlock()

	if mode == TaskStartQueue {
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
	e.tasks.mu.Lock()
	defer e.tasks.mu.Unlock()
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
	e.tasks.mu.Lock()
	defer e.tasks.mu.Unlock()
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
