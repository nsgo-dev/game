package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"go.yaml.in/yaml/v3"
)

// AgentThreadsDir is the project-relative directory where the editor stores one versioned YAML
// file per agent thread. Creating it writes a .gitignore in .nsgo/ so threads stay local and
// ignored by git.
const AgentThreadsDir = ".nsgo/threads"

// AgentThreadFileVersion is the YAML schema version of per-thread files under .nsgo/threads/<id>.yaml.
const AgentThreadFileVersion = 1

// AgentThreadSchemaURL is where the JSON Schema of version 1 agent thread files is published.
const AgentThreadSchemaURL = "https://game.nsgo.dev/schemas/agent-thread.v1.json"

// AgentThreadSchemaComment is the first line EncodeAgentThreadYAML writes: a YAML comment pointing
// a YAML language server at AgentThreadSchemaURL. ParseAgentThreadYAML ignores it like any other comment.
const AgentThreadSchemaComment = "# yaml-language-server: $schema=" + AgentThreadSchemaURL

const (
	maxAgentThreadBytes = 2 << 20   // 2 MiB safety cap per thread file.
	maxBriefYAMLBytes   = 256 << 10 // 256 KiB safety cap when reading brief.yaml.
)

// AgentThreadOutcome records how a thread request's proposed changes were resolved.
type AgentThreadOutcome string

const (
	// AgentThreadOutcomePending marks an in-progress or awaiting-review request.
	AgentThreadOutcomePending AgentThreadOutcome = "pending"
	// AgentThreadOutcomeKept marks a reviewed request whose changes (all or some) were kept.
	AgentThreadOutcomeKept AgentThreadOutcome = "kept"
	// AgentThreadOutcomeReverted marks a reviewed request whose changes were reverted.
	AgentThreadOutcomeReverted AgentThreadOutcome = "reverted"
)

// Valid reports whether o is a recognized AgentThreadOutcome value.
func (o AgentThreadOutcome) Valid() bool {
	switch o {
	case AgentThreadOutcomePending, AgentThreadOutcomeKept, AgentThreadOutcomeReverted:
		return true
	default:
		return false
	}
}

// AgentThreadStepStatus records the execution state of one step inside an agent request.
type AgentThreadStepStatus string

const (
	// AgentThreadStepPending marks a queued step that has not started yet.
	AgentThreadStepPending AgentThreadStepStatus = "pending"
	// AgentThreadStepRunning marks a step currently executing.
	AgentThreadStepRunning AgentThreadStepStatus = "running"
	// AgentThreadStepDone marks a completed step.
	AgentThreadStepDone AgentThreadStepStatus = "done"
	// AgentThreadStepFailed marks a step that failed.
	AgentThreadStepFailed AgentThreadStepStatus = "failed"
)

// Valid reports whether s is a recognized AgentThreadStepStatus value.
func (s AgentThreadStepStatus) Valid() bool {
	switch s {
	case AgentThreadStepPending, AgentThreadStepRunning, AgentThreadStepDone, AgentThreadStepFailed:
		return true
	default:
		return false
	}
}

// AgentThreadStep is one recorded action taken by the agent while handling a request.
type AgentThreadStep struct {
	// ID uniquely identifies this step within its request.
	ID string `yaml:"id" schema:"required"`
	// Label is the short human-readable description shown in the thread and History.
	Label string `yaml:"label" schema:"required"`
	// Status is the step's state ("pending", "running", "done", or "failed").
	Status AgentThreadStepStatus `yaml:"status" schema:"required,enum=pending|running|done|failed"`
	// Detail holds optional extra context for the step (for example changed fields or files).
	Detail string `yaml:"detail,omitempty"`
	// HistoryStep names the corresponding editor History label, when this step edited the scene.
	HistoryStep string `yaml:"historyStep,omitempty"`
	// HistoryIndex records the 1-based History stack index created by this step, when known.
	HistoryIndex int `yaml:"historyIndex,omitempty"`
	// Objects lists the IDs of scene objects modified by this step.
	Objects []string `yaml:"objects,omitempty"`
	// Files lists project-relative slash paths of files touched by this step.
	Files []string `yaml:"files,omitempty"`
}

// AgentThreadRequest is one author request and its streamed steps, changed objects/files, and review outcome.
type AgentThreadRequest struct {
	// ID uniquely identifies this request within its thread.
	ID string `yaml:"id" schema:"required"`
	// Title is the short single-line summary shown when the request is folded or referenced in the Inspector footer.
	Title string `yaml:"title,omitempty"`
	// Prompt is the full request text sent to the agent.
	Prompt string `yaml:"prompt" schema:"required"`
	// CreatedAt is the RFC3339 UTC timestamp when the request was sent.
	CreatedAt string `yaml:"createdAt" schema:"required"`
	// UpdatedAt is the RFC3339 UTC timestamp when the request last changed state.
	UpdatedAt string `yaml:"updatedAt,omitempty"`
	// Reply is the agent's short completion message shown under the steps.
	Reply string `yaml:"reply,omitempty"`
	// Outcome records whether the request is "pending", "kept", or "reverted".
	Outcome AgentThreadOutcome `yaml:"outcome" schema:"required,enum=pending|kept|reverted"`
	// ChangesTotal is the total number of proposed changes in this request.
	ChangesTotal int `yaml:"changesTotal,omitempty"`
	// ChangesKept is the number of proposed changes kept during review (0..ChangesTotal).
	ChangesKept int `yaml:"changesKept,omitempty"`
	// Files lists project-relative slash paths of files changed by this request.
	Files []string `yaml:"files,omitempty"`
	// Objects lists scene object IDs changed by this request (used for Inspector footer attribution).
	Objects []string `yaml:"objects,omitempty"`
	// Detail is a compact human-readable summary of changed fields/assets shown when expanded.
	Detail string `yaml:"detail,omitempty"`
	// HistoryStep names the undo step label in History linked to this request.
	HistoryStep string `yaml:"historyStep,omitempty"`
	// HistoryIndex records the 1-based undo stack index in History linked to this request, when known.
	HistoryIndex int `yaml:"historyIndex,omitempty"`
	// Steps lists the recorded agent steps for this request in execution order.
	Steps []AgentThreadStep `yaml:"steps,omitempty"`
}

// AgentThread is the strict versioned YAML document stored per line of work under .nsgo/threads/<id>.yaml.
// A file looks like this:
//
//	# yaml-language-server: $schema=https://game.nsgo.dev/schemas/agent-thread.v1.json
//	version: 1
//	id: door-lever-pulls
//	title: Door opens after two lever pulls
//	scenePath: scenes/level_01.scene.yaml
//	sessionId: sess_claude_01
//	createdAt: "2026-11-26T13:40:00Z"
//	updatedAt: "2026-11-26T14:18:00Z"
//
// ParseAgentThreadYAML decodes it strictly and EncodeAgentThreadYAML writes it back in canonical form so
// loading and saving a thread file changes nothing.
type AgentThread struct {
	// Version is the schema version; it must equal AgentThreadFileVersion (1).
	Version int `yaml:"version" schema:"required"`
	// ID is the stable filename-safe identifier for this thread (stored as .nsgo/threads/<id>.yaml).
	ID string `yaml:"id" schema:"required"`
	// Title is the thread's display name, automatically set from its first request.
	Title string `yaml:"title,omitempty"`
	// ScenePath is the slash-separated project-relative scene path this thread belongs to, or empty for Project · no scene.
	ScenePath string `yaml:"scenePath,omitempty"`
	// SessionID is the external agent's session identifier so follow-up requests continue the same agent session.
	SessionID string `yaml:"sessionId,omitempty"`
	// CreatedAt is the RFC3339 UTC timestamp when the thread was created.
	CreatedAt string `yaml:"createdAt" schema:"required"`
	// UpdatedAt is the RFC3339 UTC timestamp of the thread's latest activity.
	UpdatedAt string `yaml:"updatedAt" schema:"required"`
	// Requests lists the thread's requests in chronological order (oldest first, latest last).
	Requests []AgentThreadRequest `yaml:"requests,omitempty"`
}

// Clone returns a deep copy of t.
func (t *AgentThread) Clone() *AgentThread {
	if t == nil {
		return nil
	}
	cp := *t
	if len(t.Requests) > 0 {
		cp.Requests = make([]AgentThreadRequest, len(t.Requests))
		for i, r := range t.Requests {
			rc := r
			rc.Files = slices.Clone(r.Files)
			rc.Objects = slices.Clone(r.Objects)
			if len(r.Steps) > 0 {
				rc.Steps = make([]AgentThreadStep, len(r.Steps))
				for j, st := range r.Steps {
					sc := st
					sc.Objects = slices.Clone(st.Objects)
					sc.Files = slices.Clone(st.Files)
					rc.Steps[j] = sc
				}
			}
			cp.Requests[i] = rc
		}
	}
	return &cp
}

// AgentSendRequest describes a request submitted to the active agent thread via SendAgentRequestWith.
type AgentSendRequest struct {
	Prompt       string
	Title        string
	SessionID    string
	Reply        string
	Outcome      AgentThreadOutcome
	ChangesTotal int
	ChangesKept  int
	Files        []string
	Objects      []string
	Detail       string
	HistoryStep  string
	HistoryIndex int
	Steps        []AgentThreadStep
}

// AgentThreadListItem is one selectable row inside an AgentThreadGroup in the thread switcher.
type AgentThreadListItem struct {
	ID        string
	Title     string
	ScenePath string
	SessionID string
	Meta      string
	Current   bool
	UpdatedAt string
}

// AgentThreadGroup is one scene (or Project · no scene) section in the thread switcher.
type AgentThreadGroup struct {
	ScenePath string
	Heading   string
	OpenScene bool
	Project   bool
	Threads   []AgentThreadListItem
}

// AgentContextItem describes one item listed under "The agent reads these with every request:".
type AgentContextItem struct {
	ID      string
	Label   string
	Primary bool
	Locked  bool
}

// AgentObjectAttribution identifies the agent thread request that last changed an object,
// displayed in the Inspector footer ("Changed in ‘…’ · 2 min ago").
type AgentObjectAttribution struct {
	ThreadID     string
	ThreadTitle  string
	RequestID    string
	RequestTitle string
	ScenePath    string
	Timestamp    string
	AgeLabel     string
}

// AgentHistorySelection identifies the step focused when "Open this step in History" is clicked.
type AgentHistorySelection struct {
	ThreadID  string
	RequestID string
	StepID    string
}

// AgentHistoryEntry is one item in the History timeline (agent steps and manual undo commands).
type AgentHistoryEntry struct {
	ID          string
	Kind        string // "agent", "person", "saved"
	Title       string
	Meta        string
	ThreadID    string
	RequestID   string
	StepID      string
	Timestamp   string
	Highlighted bool
}

type agentThreadsState struct {
	threads             []*AgentThread
	activeThreadID      string
	draftThread         *AgentThread // unsaved empty thread for the current scene or project
	switcherOpen        bool
	switcherQuery       string
	expandedRequestID   string
	selectedHistoryStep AgentHistorySelection
	threadSeq           uint64
	reqSeq              uint64
	stepSeq             uint64
	rev                 uint64
}

const (
	fixAgentThread        = "write an agent thread as version: 1 with id, createdAt, updatedAt, and valid requests/steps"
	fixAgentThreadVersion = "set version: 1"
	fixAgentThreadYAMLRef = "write values directly without YAML anchors, aliases or merge keys"
)

// ParseAgentThreadYAML strictly decodes and validates a versioned agent thread YAML document,
// refusing unknown fields, duplicate keys, YAML anchors/aliases/merge keys, trailing YAML
// documents, and unsupported versions.
func ParseAgentThreadYAML(data []byte) (*AgentThread, error) {
	t, err := parseAgentThread(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixAgentThread)
	}
	return t, nil
}

func parseAgentThread(data []byte) (*AgentThread, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("editor: empty agent thread document")
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("editor: agent thread: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("editor: agent thread must be a YAML mapping")
	}
	if err := checkDuplicateYAMLKeys(root.Content[0]); err != nil {
		return nil, fmt.Errorf("editor: agent thread: %w", err)
	}
	if err := checkThreadNoYAMLRefs(root.Content[0]); err != nil {
		return nil, err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var t AgentThread
	if err := dec.Decode(&t); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("editor: empty agent thread document")
		}
		return nil, fmt.Errorf("editor: agent thread: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("editor: agent thread: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("editor: agent thread: %w", err)
	}
	if err := ValidateAgentThread(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func checkThreadNoYAMLRefs(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Anchor != "" {
		return loaderr.Errorf("", fixAgentThreadYAMLRef, "editor: agent thread must not use YAML anchor &%s", n.Anchor)
	}
	if n.Kind == yaml.AliasNode {
		return loaderr.Errorf("", fixAgentThreadYAMLRef, "editor: agent thread must not use YAML alias *%s", n.Value)
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Tag == "!!merge" || k.Value == "<<" {
				return loaderr.Errorf("", fixAgentThreadYAMLRef, "editor: agent thread must not use YAML merge key <<")
			}
		}
	}
	for _, child := range n.Content {
		if err := checkThreadNoYAMLRefs(child); err != nil {
			return err
		}
	}
	return nil
}

func validAgentThreadFileID(id string) bool {
	if id == "" || len(id) > 128 || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func validThreadRelPath(p string) bool {
	if p == "" || strings.ContainsRune(p, '\\') || strings.HasPrefix(p, "/") {
		return false
	}
	if strings.TrimSpace(p) != p || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return false
	}
	clean := path.Clean(p)
	if clean != p || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	return true
}

// ValidateAgentThread checks that t conforms to version 1 of the agent thread schema.
func ValidateAgentThread(t *AgentThread) error {
	if t == nil {
		return errors.New("editor: agent thread: nil document")
	}
	switch t.Version {
	case AgentThreadFileVersion:
	case 0:
		return loaderr.Errorf("version", fixAgentThreadVersion, "editor: agent thread is missing version (want %d)", AgentThreadFileVersion)
	default:
		return loaderr.Errorf("version", fixAgentThreadVersion, "editor: unsupported agent thread version %d (want %d)", t.Version, AgentThreadFileVersion)
	}
	if !validAgentThreadFileID(t.ID) {
		return loaderr.Errorf("id", "use a filename-safe thread id containing letters, digits, '-', '_' or '.'", "editor: invalid agent thread id %q", t.ID)
	}
	if t.ScenePath != "" && !validThreadRelPath(t.ScenePath) {
		return loaderr.Errorf("scenePath", "use a slash-separated project-relative scene path", "editor: invalid thread scenePath %q", t.ScenePath)
	}
	if strings.IndexFunc(t.SessionID, unicode.IsControl) >= 0 {
		return loaderr.Errorf("sessionId", "remove control characters from sessionId", "editor: invalid thread sessionId")
	}
	if _, err := time.Parse(time.RFC3339, t.CreatedAt); err != nil {
		return loaderr.Errorf("createdAt", "format createdAt as RFC3339 UTC timestamp", "editor: invalid thread createdAt %q: %v", t.CreatedAt, err)
	}
	if _, err := time.Parse(time.RFC3339, t.UpdatedAt); err != nil {
		return loaderr.Errorf("updatedAt", "format updatedAt as RFC3339 UTC timestamp", "editor: invalid thread updatedAt %q: %v", t.UpdatedAt, err)
	}
	seenReq := make(map[string]bool, len(t.Requests))
	for i, r := range t.Requests {
		prefix := fmt.Sprintf("requests[%d]", i)
		if !validAgentThreadFileID(r.ID) {
			return loaderr.Errorf(prefix+".id", "use a valid request id", "editor: invalid request id %q", r.ID)
		}
		if seenReq[r.ID] {
			return loaderr.Errorf(prefix+".id", "give every request a unique id", "editor: duplicate request id %q", r.ID)
		}
		seenReq[r.ID] = true
		if strings.TrimSpace(r.Prompt) == "" {
			return loaderr.Errorf(prefix+".prompt", "provide a non-empty prompt", "editor: %s prompt must not be empty", prefix)
		}
		if _, err := time.Parse(time.RFC3339, r.CreatedAt); err != nil {
			return loaderr.Errorf(prefix+".createdAt", "format createdAt as RFC3339 UTC timestamp", "editor: %s invalid createdAt %q: %v", prefix, r.CreatedAt, err)
		}
		if r.UpdatedAt != "" {
			if _, err := time.Parse(time.RFC3339, r.UpdatedAt); err != nil {
				return loaderr.Errorf(prefix+".updatedAt", "format updatedAt as RFC3339 UTC timestamp", "editor: %s invalid updatedAt %q: %v", prefix, r.UpdatedAt, err)
			}
		}
		if !r.Outcome.Valid() {
			return loaderr.Errorf(prefix+".outcome", "set outcome to pending, kept, or reverted", "editor: %s invalid outcome %q", prefix, r.Outcome)
		}
		if r.ChangesTotal < 0 || r.ChangesKept < 0 || r.ChangesKept > r.ChangesTotal {
			return loaderr.Errorf(prefix+".changesKept", "keep 0 <= changesKept <= changesTotal", "editor: %s invalid change counts (%d of %d)", prefix, r.ChangesKept, r.ChangesTotal)
		}
		if r.HistoryIndex < 0 {
			return loaderr.Errorf(prefix+".historyIndex", "use a non-negative historyIndex", "editor: %s invalid historyIndex %d", prefix, r.HistoryIndex)
		}
		for fi, f := range r.Files {
			if !validThreadRelPath(f) {
				return loaderr.Errorf(fmt.Sprintf("%s.files[%d]", prefix, fi), "use a slash-separated project-relative path", "editor: %s invalid file path %q", prefix, f)
			}
		}
		seenStep := make(map[string]bool, len(r.Steps))
		for j, st := range r.Steps {
			sp := fmt.Sprintf("%s.steps[%d]", prefix, j)
			if !validAgentThreadFileID(st.ID) {
				return loaderr.Errorf(sp+".id", "use a valid step id", "editor: invalid step id %q", st.ID)
			}
			if seenStep[st.ID] {
				return loaderr.Errorf(sp+".id", "give every step in a request a unique id", "editor: duplicate step id %q", st.ID)
			}
			seenStep[st.ID] = true
			if strings.TrimSpace(st.Label) == "" {
				return loaderr.Errorf(sp+".label", "provide a non-empty step label", "editor: %s label must not be empty", sp)
			}
			if !st.Status.Valid() {
				return loaderr.Errorf(sp+".status", "set status to pending, running, done, or failed", "editor: %s invalid status %q", sp, st.Status)
			}
			if st.HistoryIndex < 0 {
				return loaderr.Errorf(sp+".historyIndex", "use a non-negative historyIndex", "editor: %s invalid historyIndex %d", sp, st.HistoryIndex)
			}
			for fi, f := range st.Files {
				if !validThreadRelPath(f) {
					return loaderr.Errorf(fmt.Sprintf("%s.files[%d]", sp, fi), "use a slash-separated project-relative path", "editor: %s invalid file path %q", sp, f)
				}
			}
		}
	}
	return nil
}

// EncodeAgentThreadYAML validates t and encodes it as deterministic 2-space YAML prefixed with
// AgentThreadSchemaComment so loading and saving changes nothing.
func EncodeAgentThreadYAML(t *AgentThread) ([]byte, error) {
	if err := ValidateAgentThread(t); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(AgentThreadSchemaComment)
	buf.WriteByte('\n')
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(t); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("editor: encode agent thread: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: encode agent thread: %w", err)
	}
	return buf.Bytes(), nil
}

// LoadAgentThreadFile reads and strictly parses an agent thread YAML file at filePath.
func LoadAgentThreadFile(filePath string) (*AgentThread, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxAgentThreadBytes {
		return nil, fmt.Errorf("editor: agent thread %s exceeds %d byte limit", filePath, maxAgentThreadBytes)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	t, err := ParseAgentThreadYAML(data)
	if err != nil {
		return nil, loaderr.WithFile(err, filePath)
	}
	return t, nil
}

// SaveAgentThreadFile encodes t and atomically writes it to filePath, creating parent directories if needed.
func SaveAgentThreadFile(filePath string, t *AgentThread) error {
	data, err := EncodeAgentThreadYAML(t)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(filePath, data)
}

func (e *Editor) agentThreadsRoot() (string, bool) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return "", false
	}
	return filepath.Dir(e.ProjectPath), true
}

// verifyPathWithinProject ensures candidate (and any existing parent inside root) does not escape root via symlinks.
func verifyPathWithinProject(root, candidate string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	realRoot := absRoot
	if r, err := filepath.EvalSymlinks(absRoot); err == nil {
		realRoot = r
	}
	absCand, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, absCand)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("editor: path %q leaves project root", candidate)
	}
	// Walk upward until we hit an existing directory/file and verify its real path stays inside realRoot.
	cur := absCand
	for {
		if info, err := os.Lstat(cur); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				resolved, rerr := filepath.EvalSymlinks(cur)
				if rerr != nil {
					return fmt.Errorf("editor: unreadable symlink %q: %w", cur, rerr)
				}
				rrel, rerr := filepath.Rel(realRoot, resolved)
				if rerr != nil || rrel == ".." || strings.HasPrefix(rrel, ".."+string(filepath.Separator)) {
					return fmt.Errorf("editor: symlink %q points outside project root", cur)
				}
			}
			resolved, rerr := filepath.EvalSymlinks(cur)
			if rerr == nil {
				rrel, rerr := filepath.Rel(realRoot, resolved)
				if rerr != nil || rrel == ".." || strings.HasPrefix(rrel, ".."+string(filepath.Separator)) {
					return fmt.Errorf("editor: path %q resolves outside project root", cur)
				}
			}
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return nil
}

func (e *Editor) persistAgentThread(t *AgentThread) error {
	root, ok := e.agentThreadsRoot()
	if !ok || t == nil {
		return nil
	}
	nsgoDir := filepath.Join(root, ".nsgo")
	threadsDir := filepath.Join(root, filepath.FromSlash(AgentThreadsDir))
	target := filepath.Join(threadsDir, t.ID+".yaml")
	if err := verifyPathWithinProject(root, target); err != nil {
		return err
	}
	if err := os.MkdirAll(threadsDir, 0o755); err != nil {
		return err
	}
	// Ensure .nsgo/.gitignore ignores threads so .nsgo/threads/ stays out of version control.
	ignore := filepath.Join(nsgoDir, ".gitignore")
	if _, err := os.Lstat(ignore); errors.Is(err, fs.ErrNotExist) {
		if err := writeFileAtomic(ignore, []byte("*\n")); err != nil {
			return err
		}
	}
	return SaveAgentThreadFile(target, t)
}

// loadAgentThreadsForProject loads all valid .nsgo/threads/*.yaml files for the open project
// and selects the open scene's most recent thread (or a fresh draft when the scene has none).
func (e *Editor) loadAgentThreadsForProject() {
	if e == nil {
		return
	}
	e.agentThreads.threads = nil
	e.agentThreads.activeThreadID = ""
	e.agentThreads.draftThread = nil
	e.agentThreads.switcherOpen = false
	e.agentThreads.switcherQuery = ""
	e.agentThreads.expandedRequestID = ""
	e.agentThreads.selectedHistoryStep = AgentHistorySelection{}
	e.agentThreads.rev++

	root, ok := e.agentThreadsRoot()
	if !ok {
		e.syncAgentThreadForOpenScene()
		return
	}
	threadsDir := filepath.Join(root, filepath.FromSlash(AgentThreadsDir))
	if err := verifyPathWithinProject(root, threadsDir); err != nil {
		e.syncAgentThreadForOpenScene()
		return
	}
	entries, err := os.ReadDir(threadsDir)
	if err != nil {
		e.syncAgentThreadForOpenScene()
		return
	}
	loaded := make([]*AgentThread, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		// Skip background task queue files (*.thread.yaml) owned by tasks.go.
		if strings.HasSuffix(name, ".thread.yaml") {
			continue
		}
		full := filepath.Join(threadsDir, name)
		if err := verifyPathWithinProject(root, full); err != nil {
			continue
		}
		t, err := LoadAgentThreadFile(full)
		if err != nil || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		loaded = append(loaded, t)
	}
	sortAgentThreadsRecentFirst(loaded)
	e.agentThreads.threads = loaded
	e.syncAgentThreadForOpenScene()
}

func (e *Editor) resetAgentThreadsForDemo() {
	if e == nil {
		return
	}
	e.agentThreads.threads = nil
	e.agentThreads.activeThreadID = ""
	e.agentThreads.draftThread = nil
	e.agentThreads.switcherOpen = false
	e.agentThreads.switcherQuery = ""
	e.agentThreads.expandedRequestID = ""
	e.agentThreads.selectedHistoryStep = AgentHistorySelection{}
	e.agentThreads.rev++
	e.syncAgentThreadForOpenScene()
}

func (e *Editor) currentThreadScenePath() string {
	if e == nil {
		return ""
	}
	if e.ScenePath != "" {
		return e.ScenePath
	}
	return ""
}

// syncAgentThreadForOpenScene ensures the active thread belongs to the currently open scene:
// opening or switching to a scene enters its most recent thread (or a fresh empty draft thread
// for that scene) and never carries another scene's thread along.
func (e *Editor) syncAgentThreadForOpenScene() {
	if e == nil {
		return
	}
	scenePath := e.currentThreadScenePath()
	e.agentThreads.switcherOpen = false
	e.agentThreads.expandedRequestID = ""

	// Keep the currently active persisted thread only if it already belongs to this scene.
	if cur := e.findPersistedAgentThread(e.agentThreads.activeThreadID); cur != nil && cur.ScenePath == scenePath {
		e.agentThreads.draftThread = nil
		e.agentThreads.rev++
		return
	}
	// Otherwise enter the open scene's most recent thread.
	if latest := e.mostRecentThreadForScene(scenePath); latest != nil {
		e.agentThreads.activeThreadID = latest.ID
		e.agentThreads.draftThread = nil
		e.agentThreads.rev++
		return
	}
	// No thread yet for this scene: start a fresh empty draft scoped to it.
	e.agentThreads.draftThread = e.makeDraftThread(scenePath)
	e.agentThreads.activeThreadID = e.agentThreads.draftThread.ID
	e.agentThreads.rev++
}

func (e *Editor) makeDraftThread(scenePath string) *AgentThread {
	now := e.now().UTC().Format(time.RFC3339)
	id := e.nextAgentThreadID(scenePath)
	return &AgentThread{
		Version:   AgentThreadFileVersion,
		ID:        id,
		ScenePath: scenePath,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (e *Editor) nextAgentThreadID(scenePath string) string {
	prefix := "project"
	if scenePath != "" {
		base := sceneDisplaySlug(scenePath)
		if base != "" {
			prefix = base
		}
	}
	for {
		e.agentThreads.threadSeq++
		cand := fmt.Sprintf("%s-%03d", prefix, e.agentThreads.threadSeq)
		if e.findPersistedAgentThread(cand) == nil {
			if e.agentThreads.draftThread == nil || e.agentThreads.draftThread.ID != cand {
				return cand
			}
		}
	}
}

func (e *Editor) nextAgentRequestID(t *AgentThread) string {
	for {
		e.agentThreads.reqSeq++
		cand := fmt.Sprintf("req-%03d", e.agentThreads.reqSeq)
		dup := false
		if t != nil {
			for _, r := range t.Requests {
				if r.ID == cand {
					dup = true
					break
				}
			}
		}
		if !dup {
			return cand
		}
	}
}

func (e *Editor) nextAgentStepID(r *AgentThreadRequest) string {
	for {
		e.agentThreads.stepSeq++
		cand := fmt.Sprintf("step-%03d", e.agentThreads.stepSeq)
		dup := false
		if r != nil {
			for _, st := range r.Steps {
				if st.ID == cand {
					dup = true
					break
				}
			}
		}
		if !dup {
			return cand
		}
	}
}

func (e *Editor) findPersistedAgentThread(id string) *AgentThread {
	if e == nil || id == "" {
		return nil
	}
	for _, t := range e.agentThreads.threads {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (e *Editor) mostRecentThreadForScene(scenePath string) *AgentThread {
	var best *AgentThread
	for _, t := range e.agentThreads.threads {
		if t.ScenePath != scenePath {
			continue
		}
		if best == nil || compareThreadsRecentFirst(t, best) < 0 {
			best = t
		}
	}
	return best
}

func compareThreadsRecentFirst(a, b *AgentThread) int {
	ta, erra := time.Parse(time.RFC3339, a.UpdatedAt)
	tb, errb := time.Parse(time.RFC3339, b.UpdatedAt)
	if erra == nil && errb == nil {
		if ta.After(tb) {
			return -1
		}
		if ta.Before(tb) {
			return 1
		}
	} else if a.UpdatedAt != b.UpdatedAt {
		return strings.Compare(b.UpdatedAt, a.UpdatedAt)
	}
	return strings.Compare(a.ID, b.ID)
}

func sortAgentThreadsRecentFirst(list []*AgentThread) {
	slices.SortFunc(list, compareThreadsRecentFirst)
}

// SetAgentMCPConnected updates the live MCP connection status shown in the Agent panel header.
func (e *Editor) SetAgentMCPConnected(connected bool, label string) {
	if e == nil {
		return
	}
	name := cmp.Or(strings.TrimSpace(label), "MCP")
	if connected {
		e.NotifyAgentMCPConnected(AgentClientInfo{
			ID:          "mcp-local",
			Name:        name,
			DisplayName: name,
			Transport:   "stdio",
		})
	} else {
		e.NotifyAgentMCPDisconnected("mcp-local", "")
	}
	e.agentThreads.rev++
}

// AgentThreads returns a deep-copied snapshot of all persisted threads, most recently updated first.
func (e *Editor) AgentThreads() []*AgentThread {
	if e == nil || len(e.agentThreads.threads) == 0 {
		return nil
	}
	sorted := slices.Clone(e.agentThreads.threads)
	sortAgentThreadsRecentFirst(sorted)
	out := make([]*AgentThread, len(sorted))
	for i, t := range sorted {
		out[i] = t.Clone()
	}
	return out
}

// AgentThread returns a deep copy of the thread with id (persisted or active draft), or nil.
func (e *Editor) AgentThread(id string) *AgentThread {
	if e == nil || id == "" {
		return nil
	}
	if t := e.findPersistedAgentThread(id); t != nil {
		return t.Clone()
	}
	if e.agentThreads.draftThread != nil && e.agentThreads.draftThread.ID == id {
		return e.agentThreads.draftThread.Clone()
	}
	return nil
}

// ActiveAgentThread returns a deep copy of the currently active thread (creating an empty draft
// for the open scene on demand when none exists yet).
func (e *Editor) ActiveAgentThread() *AgentThread {
	if e == nil {
		return nil
	}
	if t := e.findPersistedAgentThread(e.agentThreads.activeThreadID); t != nil {
		return t.Clone()
	}
	if e.agentThreads.draftThread != nil {
		return e.agentThreads.draftThread.Clone()
	}
	e.agentThreads.draftThread = e.makeDraftThread(e.currentThreadScenePath())
	e.agentThreads.activeThreadID = e.agentThreads.draftThread.ID
	return e.agentThreads.draftThread.Clone()
}

// NewAgentThread starts a new empty thread for the currently open scene (⌘N) and activates it.
func (e *Editor) NewAgentThread() *AgentThread {
	if e == nil {
		return nil
	}
	return e.newAgentThreadForScene(e.currentThreadScenePath())
}

// NewProjectAgentThread starts a new empty project-level thread (Project · no scene) and activates it.
func (e *Editor) NewProjectAgentThread() *AgentThread {
	if e == nil {
		return nil
	}
	return e.newAgentThreadForScene("")
}

func (e *Editor) newAgentThreadForScene(scenePath string) *AgentThread {
	draft := e.makeDraftThread(scenePath)
	e.agentThreads.draftThread = draft
	e.agentThreads.activeThreadID = draft.ID
	e.agentThreads.switcherOpen = false
	e.agentThreads.switcherQuery = ""
	e.agentThreads.expandedRequestID = ""
	e.agentThreads.rev++
	e.notify()
	return draft.Clone()
}

// SelectAgentThread switches to thread id. When id belongs to a different scene in the open
// project, SelectAgentThread opens or activates that scene's tab before entering thread id.
func (e *Editor) SelectAgentThread(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	t := e.findPersistedAgentThread(id)
	if t == nil {
		if e.agentThreads.draftThread != nil && e.agentThreads.draftThread.ID == id {
			e.agentThreads.activeThreadID = id
			e.agentThreads.switcherOpen = false
			e.agentThreads.expandedRequestID = ""
			e.agentThreads.rev++
			e.notify()
			return nil
		}
		return fmt.Errorf("editor: agent thread %q not found", id)
	}
	if t.ScenePath != "" && t.ScenePath != e.ScenePath && e.Project != nil {
		if err := e.OpenScene(t.ScenePath); err != nil {
			return err
		}
	}
	e.agentThreads.activeThreadID = t.ID
	e.agentThreads.draftThread = nil
	e.agentThreads.switcherOpen = false
	e.agentThreads.expandedRequestID = ""
	e.agentThreads.rev++
	e.notify()
	return nil
}

// AgentSwitcherOpen reports whether the thread switcher popover is open.
func (e *Editor) AgentSwitcherOpen() bool {
	return e != nil && e.agentThreads.switcherOpen
}

// SetAgentSwitcherOpen opens or closes the thread switcher popover.
func (e *Editor) SetAgentSwitcherOpen(open bool) {
	if e == nil || e.agentThreads.switcherOpen == open {
		return
	}
	e.agentThreads.switcherOpen = open
	if !open {
		e.agentThreads.switcherQuery = ""
	}
	e.agentThreads.rev++
	e.notify()
}

// ToggleAgentSwitcher toggles the thread switcher popover.
func (e *Editor) ToggleAgentSwitcher() {
	if e == nil {
		return
	}
	e.SetAgentSwitcherOpen(!e.agentThreads.switcherOpen)
}

// AgentSwitcherQuery returns the active search filter inside the thread switcher popover.
func (e *Editor) AgentSwitcherQuery() string {
	if e == nil {
		return ""
	}
	return e.agentThreads.switcherQuery
}

// SetAgentSwitcherQuery updates the thread switcher search filter.
func (e *Editor) SetAgentSwitcherQuery(q string) {
	if e == nil || e.agentThreads.switcherQuery == q {
		return
	}
	e.agentThreads.switcherQuery = q
	e.agentThreads.rev++
	e.notify()
}

// ExpandedAgentRequestID returns the ID of the folded earlier request currently expanded, or "".
func (e *Editor) ExpandedAgentRequestID() string {
	if e == nil {
		return ""
	}
	return e.agentThreads.expandedRequestID
}

// SetExpandedAgentRequestID expands reqID (or collapses all folded requests when empty).
func (e *Editor) SetExpandedAgentRequestID(reqID string) {
	if e == nil || e.agentThreads.expandedRequestID == reqID {
		return
	}
	e.agentThreads.expandedRequestID = reqID
	e.agentThreads.rev++
	e.notify()
}

// ToggleAgentRequestExpanded expands reqID if collapsed, or collapses it if already expanded.
func (e *Editor) ToggleAgentRequestExpanded(reqID string) {
	if e == nil {
		return
	}
	if e.agentThreads.expandedRequestID == reqID {
		e.SetExpandedAgentRequestID("")
	} else {
		e.SetExpandedAgentRequestID(reqID)
	}
}

// JumpToLatestAgentRequest collapses any expanded earlier request and focuses the latest request.
func (e *Editor) JumpToLatestAgentRequest() {
	if e == nil {
		return
	}
	if e.agentThreads.expandedRequestID == "" {
		return
	}
	e.agentThreads.expandedRequestID = ""
	e.agentThreads.rev++
	e.notify()
}

// SendAgentRequest appends a new request to the active thread (naming an unnamed thread after
// its first request), persists the thread to .nsgo/threads/<id>.yaml when a project is open,
// and invokes Editor.OnSendAgentRequest if set.
func (e *Editor) SendAgentRequest(prompt string) (*AgentThreadRequest, error) {
	return e.SendAgentRequestWith(AgentSendRequest{Prompt: prompt})
}

// SendAgentRequestWith appends a configured request to the active thread, persists it to
// .nsgo/threads/<id>.yaml when a project is open, and invokes Editor.OnSendAgentRequest if set.
func (e *Editor) SendAgentRequestWith(in AgentSendRequest) (*AgentThreadRequest, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return nil, errors.New("editor: agent request prompt must not be empty")
	}
	now := e.now().UTC().Format(time.RFC3339)

	t := e.findPersistedAgentThread(e.agentThreads.activeThreadID)
	promoted := false
	if t == nil {
		if e.agentThreads.draftThread != nil {
			t = e.agentThreads.draftThread
		} else {
			t = e.makeDraftThread(e.currentThreadScenePath())
		}
		promoted = true
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = deriveThreadTitle(prompt)
	}
	if t.Title == "" {
		t.Title = title
	}
	if in.SessionID != "" {
		t.SessionID = in.SessionID
	}
	outcome := in.Outcome
	if outcome == "" {
		outcome = AgentThreadOutcomePending
	}
	if !outcome.Valid() {
		return nil, fmt.Errorf("editor: invalid agent request outcome %q", outcome)
	}
	changesTotal := max(in.ChangesTotal, 0)
	changesKept := in.ChangesKept
	if changesKept < 0 {
		changesKept = 0
	}
	if changesKept > changesTotal {
		changesKept = changesTotal
	}
	if outcome == AgentThreadOutcomeKept && changesTotal > 0 && changesKept == 0 {
		changesKept = changesTotal
	}

	req := AgentThreadRequest{
		ID:           e.nextAgentRequestID(t),
		Title:        title,
		Prompt:       prompt,
		CreatedAt:    now,
		UpdatedAt:    now,
		Reply:        strings.TrimSpace(in.Reply),
		Outcome:      outcome,
		ChangesTotal: changesTotal,
		ChangesKept:  changesKept,
		Files:        dedupeSortedStrings(in.Files),
		Objects:      dedupePreserveOrder(in.Objects),
		Detail:       strings.TrimSpace(in.Detail),
		HistoryStep:  strings.TrimSpace(in.HistoryStep),
		HistoryIndex: max(in.HistoryIndex, 0),
	}
	for _, st := range in.Steps {
		sc := st
		if sc.ID == "" {
			sc.ID = e.nextAgentStepID(&req)
		}
		if sc.Status == "" {
			sc.Status = AgentThreadStepDone
		}
		sc.Objects = dedupePreserveOrder(sc.Objects)
		sc.Files = dedupeSortedStrings(sc.Files)
		req.Objects = mergeUniquePreserveOrder(req.Objects, sc.Objects)
		req.Files = dedupeSortedStrings(append(req.Files, sc.Files...))
		req.Steps = append(req.Steps, sc)
	}
	t.Requests = append(t.Requests, req)
	t.UpdatedAt = now
	if promoted {
		e.agentThreads.threads = append(e.agentThreads.threads, t)
		e.agentThreads.draftThread = nil
	}
	sortAgentThreadsRecentFirst(e.agentThreads.threads)
	e.agentThreads.activeThreadID = t.ID
	e.agentThreads.expandedRequestID = ""
	e.composerDraft = ""

	if err := e.persistAgentThread(t); err != nil {
		return nil, err
	}
	e.agentThreads.rev++
	e.Status = "Sent request to agent: " + title
	e.notify()

	if e.OnSendAgentRequest != nil {
		e.OnSendAgentRequest(t.Clone(), req)
	}
	out := req
	return &out, nil
}

// SetAgentThreadSessionID records the external agent's session ID on threadID and persists it,
// so follow-up requests continue the same external agent session.
func (e *Editor) SetAgentThreadSessionID(threadID, sessionID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if strings.IndexFunc(sessionID, unicode.IsControl) >= 0 {
		return errors.New("editor: sessionId must not contain control characters")
	}
	t := e.findPersistedAgentThread(threadID)
	if t == nil {
		if e.agentThreads.draftThread != nil && e.agentThreads.draftThread.ID == threadID {
			e.agentThreads.draftThread.SessionID = sessionID
			e.agentThreads.rev++
			e.notify()
			return nil
		}
		return fmt.Errorf("editor: agent thread %q not found", threadID)
	}
	t.SessionID = sessionID
	if err := e.persistAgentThread(t); err != nil {
		return err
	}
	e.agentThreads.rev++
	e.notify()
	return nil
}

// RecordAgentStep appends or updates a step on requestID in threadID, merges its Objects/Files
// onto the request for Inspector attribution, and persists the thread.
func (e *Editor) RecordAgentStep(threadID, requestID string, step AgentThreadStep) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	t := e.findPersistedAgentThread(threadID)
	if t == nil {
		return fmt.Errorf("editor: agent thread %q not found", threadID)
	}
	reqIdx := -1
	for i := range t.Requests {
		if t.Requests[i].ID == requestID {
			reqIdx = i
			break
		}
	}
	if reqIdx < 0 {
		return fmt.Errorf("editor: request %q not found in thread %q", requestID, threadID)
	}
	r := &t.Requests[reqIdx]
	if strings.TrimSpace(step.Label) == "" {
		return errors.New("editor: step label must not be empty")
	}
	if step.Status == "" {
		step.Status = AgentThreadStepDone
	}
	if !step.Status.Valid() {
		return fmt.Errorf("editor: invalid step status %q", step.Status)
	}
	if step.ID == "" {
		step.ID = e.nextAgentStepID(r)
	}
	step.Objects = dedupePreserveOrder(step.Objects)
	step.Files = dedupeSortedStrings(step.Files)

	updated := false
	for i := range r.Steps {
		if r.Steps[i].ID == step.ID {
			r.Steps[i] = step
			updated = true
			break
		}
	}
	if !updated {
		r.Steps = append(r.Steps, step)
	}
	r.Objects = mergeUniquePreserveOrder(r.Objects, step.Objects)
	r.Files = dedupeSortedStrings(append(r.Files, step.Files...))
	if step.HistoryStep != "" && r.HistoryStep == "" {
		r.HistoryStep = step.HistoryStep
	}
	if step.HistoryIndex > 0 {
		r.HistoryIndex = step.HistoryIndex
	}
	now := e.now().UTC().Format(time.RFC3339)
	r.UpdatedAt = now
	t.UpdatedAt = now
	sortAgentThreadsRecentFirst(e.agentThreads.threads)
	if err := e.persistAgentThread(t); err != nil {
		return err
	}
	e.agentThreads.rev++
	e.notify()
	return nil
}

// UpdateAgentRequestOutcome updates a request's review outcome ("pending", "kept", "reverted"),
// change counts, detail summary, and optional agent reply, and persists the thread.
func (e *Editor) UpdateAgentRequestOutcome(threadID, requestID string, outcome AgentThreadOutcome, kept, total int, detail, reply string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if !outcome.Valid() {
		return fmt.Errorf("editor: invalid outcome %q", outcome)
	}
	t := e.findPersistedAgentThread(threadID)
	if t == nil {
		return fmt.Errorf("editor: agent thread %q not found", threadID)
	}
	for i := range t.Requests {
		if t.Requests[i].ID != requestID {
			continue
		}
		r := &t.Requests[i]
		r.Outcome = outcome
		if total >= 0 {
			r.ChangesTotal = total
		}
		if kept >= 0 {
			r.ChangesKept = min(kept, r.ChangesTotal)
		}
		if detail != "" {
			r.Detail = strings.TrimSpace(detail)
		}
		if reply != "" {
			r.Reply = strings.TrimSpace(reply)
		}
		now := e.now().UTC().Format(time.RFC3339)
		r.UpdatedAt = now
		t.UpdatedAt = now
		sortAgentThreadsRecentFirst(e.agentThreads.threads)
		if err := e.persistAgentThread(t); err != nil {
			return err
		}
		e.agentThreads.rev++
		e.notify()
		return nil
	}
	return fmt.Errorf("editor: request %q not found in thread %q", requestID, threadID)
}

// KeepAgentRequest marks requestID in threadID as kept (keeping all proposed changes).
func (e *Editor) KeepAgentRequest(threadID, requestID string) error {
	t := e.findPersistedAgentThread(threadID)
	if t == nil {
		return fmt.Errorf("editor: agent thread %q not found", threadID)
	}
	for _, r := range t.Requests {
		if r.ID == requestID {
			return e.UpdateAgentRequestOutcome(threadID, requestID, AgentThreadOutcomeKept, r.ChangesTotal, r.ChangesTotal, "", "")
		}
	}
	return fmt.Errorf("editor: request %q not found in thread %q", requestID, threadID)
}

// RevertAgentRequest marks requestID in threadID as reverted (kept 0 changes).
func (e *Editor) RevertAgentRequest(threadID, requestID string) error {
	t := e.findPersistedAgentThread(threadID)
	if t == nil {
		return fmt.Errorf("editor: agent thread %q not found", threadID)
	}
	for _, r := range t.Requests {
		if r.ID == requestID {
			return e.UpdateAgentRequestOutcome(threadID, requestID, AgentThreadOutcomeReverted, 0, r.ChangesTotal, "", "")
		}
	}
	return fmt.Errorf("editor: request %q not found in thread %q", requestID, threadID)
}

// NoteAgentEditedObjects records object IDs and files modified by an agent edit (for example
// through editormcp's scene_edit) on the active thread's pending latest request when present.
func (e *Editor) NoteAgentEditedObjects(objectIDs, files []string, label string) {
	if e == nil || (len(objectIDs) == 0 && len(files) == 0) {
		return
	}
	t := e.findPersistedAgentThread(e.agentThreads.activeThreadID)
	if t == nil || len(t.Requests) == 0 {
		return
	}
	if t.ScenePath != "" && e.ScenePath != "" && t.ScenePath != e.ScenePath {
		return
	}
	r := &t.Requests[len(t.Requests)-1]
	if r.Outcome != AgentThreadOutcomePending {
		return
	}
	cleanObjs := dedupePreserveOrder(objectIDs)
	var cleanFiles []string
	for _, f := range files {
		if validThreadRelPath(f) {
			cleanFiles = append(cleanFiles, f)
		}
	}
	cleanFiles = dedupeSortedStrings(cleanFiles)
	r.Objects = mergeUniquePreserveOrder(r.Objects, cleanObjs)
	r.Files = dedupeSortedStrings(append(r.Files, cleanFiles...))
	if label != "" {
		step := AgentThreadStep{
			ID:           e.nextAgentStepID(r),
			Label:        label,
			Status:       AgentThreadStepDone,
			HistoryStep:  label,
			HistoryIndex: e.History.Index(),
			Objects:      cleanObjs,
			Files:        cleanFiles,
		}
		r.Steps = append(r.Steps, step)
		if r.HistoryStep == "" {
			r.HistoryStep = label
		}
		r.HistoryIndex = e.History.Index()
	}
	r.ChangesTotal += max(len(cleanObjs), 1)
	now := e.now().UTC().Format(time.RFC3339)
	r.UpdatedAt = now
	t.UpdatedAt = now
	sortAgentThreadsRecentFirst(e.agentThreads.threads)
	_ = e.persistAgentThread(t)
	e.agentThreads.rev++
	e.notify()
}

// AgentThreadGroups returns persisted threads grouped by:
//  1. the open scene ("<SCENE> · OPEN SCENE"),
//  2. other scenes ("<SCENE>"), ordered by most recent thread activity,
//  3. project-level threads with no scene ("PROJECT · NO SCENE").
//
// When query is non-empty, only threads matching query (case-insensitive substring over title,
// scene name/path, request titles/prompts, or changed files) are returned, omitting empty groups.
func (e *Editor) AgentThreadGroups(query string) []AgentThreadGroup {
	if e == nil {
		return nil
	}
	q := strings.TrimSpace(strings.ToLower(query))
	openScene := e.currentThreadScenePath()
	now := e.now()

	sorted := slices.Clone(e.agentThreads.threads)
	sortAgentThreadsRecentFirst(sorted)

	byScene := make(map[string][]*AgentThread)
	var otherScenes []string
	for _, t := range sorted {
		if q != "" && !threadMatchesQuery(t, q) {
			continue
		}
		if _, exists := byScene[t.ScenePath]; !exists && t.ScenePath != "" && t.ScenePath != openScene {
			otherScenes = append(otherScenes, t.ScenePath)
		}
		byScene[t.ScenePath] = append(byScene[t.ScenePath], t)
	}

	makeItems := func(list []*AgentThread) []AgentThreadListItem {
		items := make([]AgentThreadListItem, 0, len(list))
		for _, t := range list {
			title := t.Title
			if title == "" {
				title = e.T("threads.newThread")
			}
			items = append(items, AgentThreadListItem{
				ID:        t.ID,
				Title:     title,
				ScenePath: t.ScenePath,
				SessionID: t.SessionID,
				Meta:      e.formatAgentThreadMeta(t, now),
				Current:   t.ID == e.agentThreads.activeThreadID,
				UpdatedAt: t.UpdatedAt,
			})
		}
		return items
	}

	var groups []AgentThreadGroup
	if openScene != "" {
		if list := byScene[openScene]; len(list) > 0 {
			sceneName := strings.ToUpper(e.sceneHeadingName(openScene))
			groups = append(groups, AgentThreadGroup{
				ScenePath: openScene,
				Heading:   e.T("threads.group.openScene", l10n.Param("scene", sceneName)),
				OpenScene: true,
				Threads:   makeItems(list),
			})
		}
	}
	for _, sp := range otherScenes {
		list := byScene[sp]
		if len(list) == 0 {
			continue
		}
		groups = append(groups, AgentThreadGroup{
			ScenePath: sp,
			Heading:   strings.ToUpper(sceneDisplayName(sp)),
			Threads:   makeItems(list),
		})
	}
	if list := byScene[""]; len(list) > 0 {
		groups = append(groups, AgentThreadGroup{
			ScenePath: "",
			Heading:   e.T("threads.group.project"),
			Project:   true,
			Threads:   makeItems(list),
		})
	}
	return groups
}

func threadMatchesQuery(t *AgentThread, q string) bool {
	if strings.Contains(strings.ToLower(t.Title), q) ||
		strings.Contains(strings.ToLower(t.ScenePath), q) ||
		strings.Contains(strings.ToLower(sceneDisplayName(t.ScenePath)), q) ||
		strings.Contains(strings.ToLower(t.ID), q) {
		return true
	}
	for _, r := range t.Requests {
		if strings.Contains(strings.ToLower(r.Title), q) ||
			strings.Contains(strings.ToLower(r.Prompt), q) ||
			strings.Contains(strings.ToLower(r.Detail), q) {
			return true
		}
		for _, f := range r.Files {
			if strings.Contains(strings.ToLower(f), q) {
				return true
			}
		}
		for _, o := range r.Objects {
			if strings.Contains(strings.ToLower(o), q) {
				return true
			}
		}
	}
	return false
}

// LastAgentChangeForObject returns the most recent non-reverted agent request that modified
// objectID in the open scene (or a project-level thread), for the Inspector footer link.
func (e *Editor) LastAgentChangeForObject(objectID string) (AgentObjectAttribution, bool) {
	if e == nil || objectID == "" {
		return AgentObjectAttribution{}, false
	}
	openScene := e.currentThreadScenePath()
	var best AgentObjectAttribution
	var bestTime time.Time
	found := false

	for _, t := range e.agentThreads.threads {
		if openScene != "" && t.ScenePath != "" && t.ScenePath != openScene {
			continue
		}
		for _, r := range t.Requests {
			if r.Outcome == AgentThreadOutcomeReverted {
				continue
			}
			if !requestTouchesObject(r, objectID) {
				continue
			}
			ts := cmp.Or(r.UpdatedAt, r.CreatedAt, t.UpdatedAt)
			parsed, _ := time.Parse(time.RFC3339, ts)
			if !found || parsed.After(bestTime) || (parsed.Equal(bestTime) && ts > best.Timestamp) {
				title := cmp.Or(r.Title, t.Title, deriveThreadTitle(r.Prompt))
				best = AgentObjectAttribution{
					ThreadID:     t.ID,
					ThreadTitle:  cmp.Or(t.Title, title),
					RequestID:    r.ID,
					RequestTitle: title,
					ScenePath:    t.ScenePath,
					Timestamp:    ts,
					AgeLabel:     e.formatRelativeAge(ts, e.now()),
				}
				bestTime = parsed
				found = true
			}
		}
	}
	return best, found
}

func requestTouchesObject(r AgentThreadRequest, objectID string) bool {
	if slices.Contains(r.Objects, objectID) {
		return true
	}
	for _, st := range r.Steps {
		if slices.Contains(st.Objects, objectID) {
			return true
		}
	}
	return false
}

// OpenAgentRequest switches the right rail to the Agent panel, activates threadID (opening its
// scene if needed), and expands requestID when it is folded as an earlier request.
func (e *Editor) OpenAgentRequest(threadID, requestID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if err := e.SelectAgentThread(threadID); err != nil {
		return err
	}
	t := e.findPersistedAgentThread(threadID)
	if e.agent.mode != AgentModeNone {
		e.agent.rightTab = RightTabAgent
	}
	if t != nil && requestID != "" {
		latestID := ""
		if len(t.Requests) > 0 {
			latestID = t.Requests[len(t.Requests)-1].ID
		}
		if requestID != latestID {
			e.agentThreads.expandedRequestID = requestID
		} else {
			e.agentThreads.expandedRequestID = ""
		}
	}
	e.agentThreads.rev++
	e.notify()
	return nil
}

// OpenHistoryStep opens the right-rail History timeline focused on the given thread request/step
// and invokes Editor.OnOpenHistoryStep when configured.
func (e *Editor) OpenHistoryStep(threadID, requestID, stepID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	sel := AgentHistorySelection{
		ThreadID:  threadID,
		RequestID: requestID,
		StepID:    stepID,
	}
	e.agentThreads.selectedHistoryStep = sel
	e.agent.rightTab = RightTabHistory
	e.agentThreads.switcherOpen = false
	e.agentThreads.rev++
	e.Status = e.T("threads.history.openedStatus")
	e.notify()
	if e.OnOpenHistoryStep != nil {
		e.OnOpenHistoryStep(sel)
	}
	return nil
}

// SelectedHistoryStep returns the step currently highlighted in the History view.
func (e *Editor) SelectedHistoryStep() AgentHistorySelection {
	if e == nil {
		return AgentHistorySelection{}
	}
	return e.agentThreads.selectedHistoryStep
}

// AgentHistoryEntries builds the unified timeline for the History panel:
// agent thread steps for the active scene (most recent first), manual undo steps from History,
// and git commit milestones.
func (e *Editor) AgentHistoryEntries() []AgentHistoryEntry {
	if e == nil {
		return nil
	}
	now := e.now()
	sel := e.agentThreads.selectedHistoryStep
	var entries []AgentHistoryEntry

	// Current state marker.
	nowMeta := e.T("threads.history.clean")
	if e.HasUnsavedChanges() {
		nowMeta = e.T("threads.history.unsaved")
	}
	entries = append(entries, AgentHistoryEntry{
		ID:    "now",
		Kind:  "now",
		Title: e.T("threads.history.now"),
		Meta:  nowMeta,
	})

	// Collect agent thread requests/steps for the open scene (and any explicitly selected thread).
	openScene := e.currentThreadScenePath()
	for _, t := range e.agentThreads.threads {
		if openScene != "" && t.ScenePath != "" && t.ScenePath != openScene && t.ID != sel.ThreadID {
			continue
		}
		for i := len(t.Requests) - 1; i >= 0; i-- {
			r := t.Requests[i]
			ts := cmp.Or(r.UpdatedAt, r.CreatedAt)
			age := e.formatRelativeAge(ts, now)
			if len(r.Steps) > 0 {
				for j := len(r.Steps) - 1; j >= 0; j-- {
					st := r.Steps[j]
					hl := sel.ThreadID == t.ID && sel.RequestID == r.ID && (sel.StepID == st.ID || (sel.StepID == "" && j == len(r.Steps)-1))
					meta := e.formatRequestOutcomeShort(r) + " · " + age
					entries = append(entries, AgentHistoryEntry{
						ID:          t.ID + ":" + r.ID + ":" + st.ID,
						Kind:        "agent",
						Title:       e.T("threads.history.agentPrefix", l10n.Param("label", st.Label)),
						Meta:        meta,
						ThreadID:    t.ID,
						RequestID:   r.ID,
						StepID:      st.ID,
						Timestamp:   ts,
						Highlighted: hl,
					})
				}
			} else {
				hl := sel.ThreadID == t.ID && sel.RequestID == r.ID
				entries = append(entries, AgentHistoryEntry{
					ID:          t.ID + ":" + r.ID,
					Kind:        "agent",
					Title:       e.T("threads.history.agentPrefix", l10n.Param("label", cmp.Or(r.Title, deriveThreadTitle(r.Prompt)))),
					Meta:        e.formatRequestOutcomeShort(r) + " · " + age,
					ThreadID:    t.ID,
					RequestID:   r.ID,
					Timestamp:   ts,
					Highlighted: hl,
				})
			}
		}
	}

	// Include live manual undo stack commands (most recent first).
	if e.History != nil {
		for i := e.History.cursor - 1; i >= 0; i-- {
			lbl := e.History.commands[i].Label()
			entries = append(entries, AgentHistoryEntry{
				ID:    fmt.Sprintf("undo-%d", i+1),
				Kind:  "person",
				Title: e.T("threads.history.youPrefix", l10n.Param("label", lbl)),
				Meta:  e.T("threads.history.undoStep", l10n.Param("index", i+1)),
			})
		}
		for i, m := range e.History.Milestones() {
			entries = append(entries, AgentHistoryEntry{
				ID:    fmt.Sprintf("milestone-%d", i),
				Kind:  "saved",
				Title: e.T("threads.history.saved"),
				Meta:  m.Summary,
			})
		}
	}

	return entries
}

// AgentAlwaysReadItems returns the context items shown in the empty-thread view and composer
// ("The agent reads these with every request:").
func (e *Editor) AgentAlwaysReadItems() []AgentContextItem {
	var items []AgentContextItem
	active := e.ActiveAgentThread()
	scenePath := e.currentThreadScenePath()
	if active != nil && active.ScenePath != "" {
		scenePath = active.ScenePath
	}
	if scenePath != "" || (e != nil && e.Doc != nil && e.Doc.Name != "") {
		name := e.sceneHeadingName(scenePath)
		if name == "" && e != nil && e.Doc != nil {
			name = e.Doc.Name
		}
		if name != "" {
			items = append(items, AgentContextItem{
				ID:      "scene",
				Label:   e.T("threads.context.wholeScene", l10n.Param("scene", name)),
				Primary: true,
			})
		}
	}
	items = append(items,
		AgentContextItem{ID: "brief", Label: "brief.yaml", Locked: true},
		AgentContextItem{ID: "rules", Label: "AGENTS.md", Locked: true},
		AgentContextItem{ID: "viewport", Label: e.T("threads.context.viewportImage")},
	)
	return items
}

// AgentBriefSuggestions returns up to three starter prompts for the empty-thread view,
// extracted from the open project's brief.yaml (refusing symlinks outside the project root)
// or falling back to scene-aware starter suggestions.
func (e *Editor) AgentBriefSuggestions() []string {
	if root, ok := e.agentThreadsRoot(); ok {
		if got := loadBriefSuggestionsFromDir(root); len(got) > 0 {
			return got
		}
	}
	scene := "level_01"
	if e != nil {
		if s := e.sceneHeadingName(e.currentThreadScenePath()); s != "" {
			scene = s
		}
	}
	return []string{
		e.T("threads.suggestion.blockOut", l10n.Param("scene", scene)),
		e.T("threads.suggestion.enemyPatrol"),
		e.T("threads.suggestion.hud"),
	}
}

func loadBriefSuggestionsFromDir(root string) []string {
	briefPath := filepath.Join(root, "brief.yaml")
	if err := verifyPathWithinProject(root, briefPath); err != nil {
		return nil
	}
	info, err := os.Stat(briefPath)
	if err != nil || info.IsDir() || info.Size() > maxBriefYAMLBytes {
		return nil
	}
	data, err := os.ReadFile(briefPath)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc) == 0 {
		return nil
	}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || len(out) >= 3 || slices.Contains(out, s) {
			return
		}
		out = append(out, s)
	}
	extractList := func(v any) {
		switch xs := v.(type) {
		case []any:
			for _, elem := range xs {
				switch item := elem.(type) {
				case string:
					add(item)
				case map[string]any:
					for _, k := range []string{"prompt", "text", "summary", "title", "goal", "name"} {
						if s, ok := item[k].(string); ok && strings.TrimSpace(s) != "" {
							add(s)
							break
						}
					}
				}
			}
		case string:
			add(xs)
		}
	}
	for _, key := range []string{"suggestions", "tasks", "next", "goals", "milestones", "features"} {
		if v, ok := doc[key]; ok {
			extractList(v)
		}
	}
	return out
}

func (e *Editor) formatAgentThreadMeta(t *AgentThread, now time.Time) string {
	if t == nil || len(t.Requests) == 0 {
		return e.T("threads.meta.empty")
	}
	last := t.Requests[len(t.Requests)-1]
	age := e.formatRelativeAge(cmp.Or(t.UpdatedAt, last.UpdatedAt, last.CreatedAt), now)

	// Project-wide threads with files and no review counts show file names (matching Threads.dc.html: "Mon · game.yaml, knight.prefab.yaml").
	if t.ScenePath == "" && last.ChangesTotal == 0 && len(last.Files) > 0 && last.Outcome != AgentThreadOutcomeReverted {
		return age + " · " + formatThreadFilesMeta(last.Files)
	}

	switch last.Outcome {
	case AgentThreadOutcomePending:
		if last.ChangesTotal == 1 {
			return age + " · " + e.T("threads.meta.oneChangeToReview")
		}
		if last.ChangesTotal > 1 {
			return age + " · " + e.T("threads.meta.changesToReview", l10n.Param("count", last.ChangesTotal))
		}
		return age + " · " + e.T("threads.meta.inProgress")
	case AgentThreadOutcomeReverted:
		return age + " · " + e.T("threads.meta.reverted")
	default:
		if last.ChangesTotal > 0 {
			return age + " · " + e.T("threads.meta.keptOf", l10n.Param("kept", last.ChangesKept), l10n.Param("total", last.ChangesTotal))
		}
		if len(last.Files) > 0 {
			return age + " · " + formatThreadFilesMeta(last.Files)
		}
		return age + " · " + e.T("threads.meta.kept")
	}
}

func (e *Editor) formatRequestOutcomeShort(r AgentThreadRequest) string {
	switch r.Outcome {
	case AgentThreadOutcomePending:
		if r.ChangesTotal == 1 {
			return e.T("threads.meta.oneChangeToReview")
		}
		if r.ChangesTotal > 1 {
			return e.T("threads.meta.changesToReview", l10n.Param("count", r.ChangesTotal))
		}
		return e.T("threads.meta.inProgress")
	case AgentThreadOutcomeReverted:
		return e.T("threads.meta.reverted")
	default:
		if r.ChangesTotal > 0 {
			return e.T("threads.meta.keptOf", l10n.Param("kept", r.ChangesKept), l10n.Param("total", r.ChangesTotal))
		}
		return e.T("threads.meta.kept")
	}
}

func (e *Editor) formatRelativeAge(rfc3339 string, now time.Time) string {
	ts, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return e.T("threads.age.now")
	}
	d := now.Sub(ts)
	if d < time.Minute {
		return e.T("threads.age.now")
	}
	if d < time.Hour {
		return e.T("threads.age.minutesAgo", l10n.Param("count", int(d/time.Minute)))
	}
	if d < 24*time.Hour {
		return e.T("threads.age.hoursAgo", l10n.Param("count", int(d/time.Hour)))
	}
	if d < 48*time.Hour {
		return e.T("threads.age.yesterday")
	}
	if d < 7*24*time.Hour {
		return ts.UTC().Format("Mon")
	}
	return ts.UTC().Format("Jan 2")
}

func formatRequestClockTime(rfc3339 string) string {
	ts, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return ""
	}
	return ts.UTC().Format("15:04")
}

func formatThreadFilesMeta(files []string) string {
	if len(files) == 0 {
		return ""
	}
	limit := min(len(files), 3)
	parts := make([]string, limit)
	for i := range limit {
		parts[i] = path.Base(files[i])
	}
	return strings.Join(parts, ", ")
}

func (e *Editor) sceneHeadingName(scenePath string) string {
	if scenePath != "" {
		return sceneDisplayName(scenePath)
	}
	if e != nil && e.Doc != nil && strings.TrimSpace(e.Doc.Name) != "" {
		return strings.TrimSpace(e.Doc.Name)
	}
	return ""
}

func sceneDisplayName(scenePath string) string {
	if scenePath == "" {
		return ""
	}
	base := path.Base(scenePath)
	for _, suf := range []string{".scene.yaml", ".scene.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(base, suf) {
			base = strings.TrimSuffix(base, suf)
			break
		}
	}
	return base
}

func sceneDisplaySlug(scenePath string) string {
	name := strings.ToLower(sceneDisplayName(scenePath))
	var b strings.Builder
	dash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func deriveThreadTitle(prompt string) string {
	line := strings.TrimSpace(prompt)
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	line = strings.Join(strings.Fields(line), " ")
	line = strings.TrimRight(line, ".!?")
	const maxRunes = 64
	if utf8.RuneCountInString(line) > maxRunes {
		rs := []rune(line)
		line = strings.TrimSpace(string(rs[:maxRunes-1])) + "…"
	}
	return line
}

func dedupePreserveOrder(xs []string) []string {
	if len(xs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(xs))
	out := make([]string, 0, len(xs))
	for _, s := range xs {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeUniquePreserveOrder(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	out := slices.Clone(a)
	seen := make(map[string]bool, len(a)+len(b))
	for _, s := range out {
		seen[s] = true
	}
	for _, s := range b {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func dedupeSortedStrings(xs []string) []string {
	out := dedupePreserveOrder(xs)
	slices.Sort(out)
	return out
}
