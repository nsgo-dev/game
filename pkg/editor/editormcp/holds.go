package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
)

const (
	// defaultHoldWaitTimeout bounds how long a parallel task waits on a held object before
	// returning an actionable timeout error rather than blocking indefinitely.
	defaultHoldWaitTimeout = 30 * time.Second
	// holdCleanupEditorTimeout bounds post-cancel/rollback UI state updates so a stopped UI loop
	// can never wedge a tool goroutine.
	holdCleanupEditorTimeout = 2 * time.Second
)

type holdWaiter struct {
	taskID  string
	objects []string
	ch      chan struct{}
}

// objectHoldManager coordinates per-object holds across parallel agent MCP sessions so that
// a task editing an object holds it for its active turn while another task attempting to edit
// the same object waits cleanly (showing "waiting on <object>" in the editor) and automatically
// resumes when the first task releases its hold.
type objectHoldManager struct {
	mu           sync.Mutex
	heldBy       map[string]string
	taskHeld     map[string][]string
	sessionTasks map[*mcp.ServerSession]string
	changes      map[string][]editor.TaskReviewChange
	beforeYAML   map[string]string
	waiters      []*holdWaiter
	waitTimeout  time.Duration
	onBlocked    func(taskID, objectID, holderTaskID string)
}

func newObjectHoldManager() *objectHoldManager {
	return &objectHoldManager{
		heldBy:       make(map[string]string),
		taskHeld:     make(map[string][]string),
		sessionTasks: make(map[*mcp.ServerSession]string),
		changes:      make(map[string][]editor.TaskReviewChange),
		beforeYAML:   make(map[string]string),
		waitTimeout:  defaultHoldWaitTimeout,
	}
}

func normalizeTargetObjects(objs []string) []string {
	if len(objs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(objs))
	out := make([]string, 0, len(objs))
	for _, id := range objs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func targetObjectIDs(edits []editArgs) []string {
	var ids []string
	for _, e := range edits {
		if e.ID != "" {
			ids = append(ids, e.ID)
		}
		if e.Parent != nil && *e.Parent != "" {
			ids = append(ids, *e.Parent)
		}
		if e.Op == "add" && len(e.Object) > 0 {
			var obj struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(e.Object, &obj) == nil && obj.ID != "" {
				ids = append(ids, obj.ID)
			}
		}
	}
	return normalizeTargetObjects(ids)
}

// ConnectTask serves one dedicated MCP session on t bound to background task taskID.
// Edits performed on this session automatically hold their target objects for the duration
// of the task's turn and release those holds when the session closes. Sessions are tracked
// by Server.wg and Server.Clients() alongside ordinary Connect sessions.
func (s *Server) ConnectTask(ctx context.Context, t mcp.Transport, taskID string) (*mcp.ServerSession, error) {
	ss, err := s.sdk.Connect(ctx, t, nil)
	if err != nil {
		return nil, err
	}
	s.BindSessionTask(ss, taskID)
	s.trackSession(ss, "in-memory")
	return ss, nil
}

// BindSessionTask associates an existing MCP ServerSession with background task taskID.
func (s *Server) BindSessionTask(ss *mcp.ServerSession, taskID string) {
	if s == nil || ss == nil || taskID == "" {
		return
	}
	s.holds.mu.Lock()
	s.holds.sessionTasks[ss] = taskID
	s.holds.mu.Unlock()
}

func (s *Server) unbindSessionTask(ss *mcp.ServerSession) {
	if s == nil || ss == nil {
		return
	}
	s.holds.mu.Lock()
	taskID := s.holds.sessionTasks[ss]
	delete(s.holds.sessionTasks, ss)
	s.holds.mu.Unlock()
	if taskID != "" {
		s.ReleaseTaskHolds(taskID)
	}
}

func (s *Server) sessionTaskID(ss *mcp.ServerSession) string {
	if s == nil || ss == nil {
		return ""
	}
	s.holds.mu.Lock()
	id := s.holds.sessionTasks[ss]
	s.holds.mu.Unlock()
	return id
}

// SetHoldBlockedHook registers an optional deterministic callback fired immediately after
// a parallel task enters the holding state waiting on an object held by another task.
func (s *Server) SetHoldBlockedHook(fn func(taskID, objectID, holderTaskID string)) {
	if s == nil {
		return
	}
	s.holds.mu.Lock()
	s.holds.onBlocked = fn
	s.holds.mu.Unlock()
}

// SetHoldWaitTimeout configures how long acquireObjectHolds waits on a held object before
// returning an *EditError timeout. Non-positive values reset to defaultHoldWaitTimeout.
func (s *Server) SetHoldWaitTimeout(d time.Duration) {
	if s == nil {
		return
	}
	if d <= 0 {
		d = defaultHoldWaitTimeout
	}
	s.holds.mu.Lock()
	s.holds.waitTimeout = d
	s.holds.mu.Unlock()
}

// HeldObjects returns a snapshot map of objectID -> holding taskID.
func (s *Server) HeldObjects() map[string]string {
	if s == nil {
		return nil
	}
	s.holds.mu.Lock()
	defer s.holds.mu.Unlock()
	out := make(map[string]string, len(s.holds.heldBy))
	maps.Copy(out, s.holds.heldBy)
	return out
}

// ReleaseTaskHolds releases every object hold owned by taskID, clears its buffered turn
// snapshots/changes, and wakes waiting parallel tasks.
func (s *Server) ReleaseTaskHolds(taskID string) {
	if s == nil || taskID == "" {
		return
	}
	s.holds.mu.Lock()
	objs := s.holds.taskHeld[taskID]
	delete(s.holds.taskHeld, taskID)
	delete(s.holds.changes, taskID)
	delete(s.holds.beforeYAML, taskID)
	for _, id := range objs {
		if s.holds.heldBy[id] == taskID {
			delete(s.holds.heldBy, id)
		}
	}
	s.wakeWaitersLocked()
	s.holds.mu.Unlock()
}

func (s *Server) wakeWaitersLocked() {
	for _, w := range s.holds.waiters {
		select {
		case w.ch <- struct{}{}:
		default:
		}
	}
}

func (s *Server) removeWaiterLocked(target *holdWaiter) {
	for i, w := range s.holds.waiters {
		if w == target {
			s.holds.waiters = append(s.holds.waiters[:i], s.holds.waiters[i+1:]...)
			return
		}
	}
}

// hasWaitCycleLocked reports whether taskID waiting on targets would create a wait-for cycle
// across active waiters and current object holders (e.g. task 1 holds A and waits on B while
// task 2 holds B and waits on A).
func (s *Server) hasWaitCycleLocked(taskID string, targets []string) bool {
	if taskID == "" || len(targets) == 0 {
		return false
	}
	adj := make(map[string][]string)
	addEdge := func(from, to string) {
		if from == "" || to == "" || from == to {
			return
		}
		if !slices.Contains(adj[from], to) {
			adj[from] = append(adj[from], to)
		}
	}
	// Existing waiters' wait-for edges (to current holders and earlier overlapping waiters).
	for i, w := range s.holds.waiters {
		if w.taskID == "" {
			continue
		}
		for _, obj := range w.objects {
			if owner, ok := s.holds.heldBy[obj]; ok && owner != w.taskID {
				addEdge(w.taskID, owner)
			}
			for j := 0; j < i; j++ {
				prev := s.holds.waiters[j]
				if prev.taskID != "" && prev.taskID != w.taskID && slices.Contains(prev.objects, obj) {
					addEdge(w.taskID, prev.taskID)
				}
			}
		}
	}
	// Candidate taskID's direct wait-for targets.
	var queue []string
	visited := make(map[string]bool)
	enqueue := func(targetTask string) {
		if targetTask == "" || targetTask == taskID || visited[targetTask] {
			return
		}
		visited[targetTask] = true
		queue = append(queue, targetTask)
	}
	for _, obj := range targets {
		if owner, ok := s.holds.heldBy[obj]; ok && owner != taskID {
			enqueue(owner)
		}
		for _, w := range s.holds.waiters {
			if w.taskID != "" && w.taskID != taskID && slices.Contains(w.objects, obj) {
				enqueue(w.taskID)
			}
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if next == taskID {
				return true
			}
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

// acquireObjectHolds acquires object holds on objects for taskID (or transiently for an unbound session
// when taskID is empty). It returns a rollback function that releases ONLY the newly acquired holds
// (leaving holds already owned by taskID intact) if the caller's edit fails before committing.
// When another task holds one of objects, the call checks for wait-for cycles (avoiding AB-BA deadlocks),
// updates taskID's holding state in the editor ("waiting on <objectID>"), and waits until the hold ends,
// the hold wait timeout expires, ctx is cancelled, or the server closes.
func (s *Server) acquireObjectHolds(ctx context.Context, taskID string, objects []string) (func(), error) {
	targets := normalizeTargetObjects(objects)
	if s == nil || len(targets) == 0 {
		return func() {}, nil
	}

	s.holds.mu.Lock()
	timeout := s.holds.waitTimeout
	if timeout <= 0 {
		timeout = defaultHoldWaitTimeout
	}
	s.holds.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var waiter *holdWaiter
	defer func() {
		if waiter != nil {
			s.holds.mu.Lock()
			s.removeWaiterLocked(waiter)
			s.holds.mu.Unlock()
		}
	}()

	clearWaitingState := func(existingHeld []string) {
		if taskID == "" {
			return
		}
		cleanCtx, cancel := context.WithTimeout(context.Background(), holdCleanupEditorTimeout)
		defer cancel()
		_ = s.Editor(cleanCtx, func(ed *editor.Editor) error {
			if t, ok := ed.Task(taskID); ok && t.State == editor.TaskRunning {
				return ed.SetTaskHoldState(taskID, existingHeld, nil, "", "")
			}
			return nil
		})
	}

	for {
		s.holds.mu.Lock()
		conflictObj := ""
		holderID := ""
		for _, id := range targets {
			if owner, ok := s.holds.heldBy[id]; ok && owner != taskID {
				conflictObj = id
				holderID = owner
				break
			}
		}

		// Preserve FIFO fairness when an earlier waiter is already waiting on one of our objects.
		if conflictObj == "" && waiter == nil {
			for _, w := range s.holds.waiters {
				if w.taskID == taskID {
					continue
				}
				for _, id := range targets {
					if slices.Contains(w.objects, id) {
						conflictObj = id
						holderID = w.taskID
						break
					}
				}
				if conflictObj != "" {
					break
				}
			}
		} else if conflictObj == "" && waiter != nil {
			for _, w := range s.holds.waiters {
				if w == waiter {
					break
				}
				if w.taskID == taskID {
					continue
				}
				for _, id := range targets {
					if slices.Contains(w.objects, id) {
						conflictObj = id
						holderID = w.taskID
						break
					}
				}
				if conflictObj != "" {
					break
				}
			}
		}

		if conflictObj == "" {
			if waiter != nil {
				s.removeWaiterLocked(waiter)
				waiter = nil
			}
			if taskID == "" {
				// Unbound session: no persistent hold needed once no task holds these objects.
				s.holds.mu.Unlock()
				return func() {}, nil
			}
			cur := slices.Clone(s.holds.taskHeld[taskID])
			var newlyAcquired []string
			for _, id := range targets {
				if s.holds.heldBy[id] != taskID {
					newlyAcquired = append(newlyAcquired, id)
				}
				s.holds.heldBy[id] = taskID
				if !slices.Contains(cur, id) {
					cur = append(cur, id)
				}
			}
			slices.Sort(cur)
			s.holds.taskHeld[taskID] = cur
			s.holds.mu.Unlock()

			// Reflect active editing objects on the editor (clearing any previous waiting state).
			_ = s.Editor(ctx, func(ed *editor.Editor) error {
				return ed.SetTaskHoldState(taskID, cur, nil, "", "")
			})

			rollback := func() {
				if len(newlyAcquired) == 0 {
					return
				}
				s.holds.mu.Lock()
				for _, id := range newlyAcquired {
					if s.holds.heldBy[id] == taskID {
						delete(s.holds.heldBy, id)
					}
				}
				remaining := s.holds.taskHeld[taskID][:0]
				for _, id := range s.holds.taskHeld[taskID] {
					if !slices.Contains(newlyAcquired, id) {
						remaining = append(remaining, id)
					}
				}
				if len(remaining) == 0 {
					delete(s.holds.taskHeld, taskID)
					remaining = nil
				} else {
					s.holds.taskHeld[taskID] = slices.Clone(remaining)
					remaining = s.holds.taskHeld[taskID]
				}
				s.wakeWaitersLocked()
				s.holds.mu.Unlock()
				clearWaitingState(remaining)
			}
			return rollback, nil
		}

		// Prevent AB-BA / multi-task circular wait deadlocks before registering a waiter.
		if waiter == nil && s.hasWaitCycleLocked(taskID, targets) {
			s.holds.mu.Unlock()
			return nil, &EditError{
				File:    "the open scene",
				Object:  conflictObj,
				Problem: fmt.Sprintf("deadlock avoided: task %s and task %s are waiting on each other's held objects (%q)", taskID, holderID, conflictObj),
				Fix:     "complete or release one task's turn before editing objects held by another waiting task",
			}
		}

		// Conflict found: register FIFO waiter and update editor holding state.
		if waiter == nil {
			waiter = &holdWaiter{
				taskID:  taskID,
				objects: targets,
				ch:      make(chan struct{}, 1),
			}
			s.holds.waiters = append(s.holds.waiters, waiter)
		}
		existingHeld := slices.Clone(s.holds.taskHeld[taskID])
		hook := s.holds.onBlocked
		s.holds.mu.Unlock()

		if taskID != "" {
			_ = s.Editor(ctx, func(ed *editor.Editor) error {
				return ed.SetTaskHoldState(taskID, existingHeld, targets, conflictObj, holderID)
			})
		}
		if hook != nil {
			hook(taskID, conflictObj, holderID)
		}

		select {
		case <-waiter.ch:
			// Re-check holds on next iteration.
		case <-timer.C:
			clearWaitingState(existingHeld)
			return nil, &EditError{
				File:    "the open scene",
				Object:  conflictObj,
				Problem: fmt.Sprintf("timed out waiting on %q held by task %s", conflictObj, holderID),
				Fix:     "finish or stop the holding task before editing this object",
			}
		case <-ctx.Done():
			clearWaitingState(existingHeld)
			return nil, ctx.Err()
		case <-s.done:
			return nil, errors.New("editormcp: the editor's server is closed")
		}
	}
}

func (s *Server) captureTaskBeforeYAML(taskID string, ed *editor.Editor) {
	if s == nil || taskID == "" || ed == nil {
		return
	}
	s.holds.mu.Lock()
	_, already := s.holds.beforeYAML[taskID]
	s.holds.mu.Unlock()
	if already {
		return
	}
	data, err := ed.SaveYAML()
	if err != nil {
		return
	}
	s.holds.mu.Lock()
	if _, ok := s.holds.beforeYAML[taskID]; !ok {
		s.holds.beforeYAML[taskID] = string(data)
	}
	s.holds.mu.Unlock()
}

func (s *Server) recordTaskEdits(taskID, sceneFile string, edits []editArgs, addedIDs []string) {
	if s == nil || taskID == "" {
		return
	}
	var added []editor.TaskReviewChange
	addIdx := 0
	for _, e := range edits {
		kind := "changed"
		objID := e.ID
		switch e.Op {
		case "add":
			kind = "added"
			if addIdx < len(addedIDs) {
				objID = addedIDs[addIdx]
				addIdx++
			}
		case "remove":
			kind = "removed"
		}
		summary := fmt.Sprintf("%s %s", e.Op, objID)
		added = append(added, editor.TaskReviewChange{
			ObjectID: objID,
			File:     sceneFile,
			Kind:     kind,
			Summary:  summary,
		})
	}
	s.holds.mu.Lock()
	s.holds.changes[taskID] = append(s.holds.changes[taskID], added...)
	s.holds.mu.Unlock()
}

func (s *Server) recordTaskObjectFieldEdit(taskID, sceneFile, objID, field string) {
	if s == nil || taskID == "" {
		return
	}
	s.holds.mu.Lock()
	s.holds.changes[taskID] = append(s.holds.changes[taskID], editor.TaskReviewChange{
		ObjectID: objID,
		File:     sceneFile,
		Kind:     "changed",
		Summary:  fmt.Sprintf("set %s.%s", objID, field),
	})
	s.holds.mu.Unlock()
}

func (s *Server) recordTaskObjectChange(taskID, sceneFile, objID, kind, summary string) {
	if s == nil || taskID == "" {
		return
	}
	s.holds.mu.Lock()
	s.holds.changes[taskID] = append(s.holds.changes[taskID], editor.TaskReviewChange{
		ObjectID: objID,
		File:     sceneFile,
		Kind:     kind,
		Summary:  summary,
	})
	s.holds.mu.Unlock()
}

// TaskReviewSnapshot returns the captured pre-turn scene YAML and recorded per-object changes
// for taskID on this server, or ok == false when no edits were recorded for taskID.
func (s *Server) TaskReviewSnapshot(taskID string) (beforeYAML string, changes []editor.TaskReviewChange, ok bool) {
	if s == nil || taskID == "" {
		return "", nil, false
	}
	s.holds.mu.Lock()
	beforeYAML = s.holds.beforeYAML[taskID]
	changes = slices.Clone(s.holds.changes[taskID])
	s.holds.mu.Unlock()
	if beforeYAML == "" && len(changes) == 0 {
		return "", nil, false
	}
	return beforeYAML, changes, true
}

// CompleteTaskTurn finishes taskID's active turn, produces its own separate editor.TaskReview
// from the edits recorded on its session, releases all object holds held by taskID (waking
// any parallel task waiting on those objects), and transitions the task to TaskReadyToReview.
// If updating the editor fails, recorded changes and holds are preserved.
func (s *Server) CompleteTaskTurn(ctx context.Context, taskID, summary string) (editor.TaskReview, error) {
	if s == nil {
		return editor.TaskReview{}, errors.New("editormcp: nil server")
	}
	if taskID == "" {
		return editor.TaskReview{}, errors.New("editormcp: task id is required")
	}
	s.holds.mu.Lock()
	changes := slices.Clone(s.holds.changes[taskID])
	beforeYAML := s.holds.beforeYAML[taskID]
	s.holds.mu.Unlock()

	var rev editor.TaskReview
	err := s.Editor(ctx, func(ed *editor.Editor) error {
		afterBytes, _ := ed.SaveYAML()
		rev = editor.TaskReview{
			ID:              "review-" + taskID,
			TaskID:          taskID,
			ScenePath:       sceneName(ed, projectRoot(ed)),
			Summary:         summary,
			ChangeCount:     len(changes),
			Changes:         changes,
			BeforeSceneYAML: beforeYAML,
			AfterSceneYAML:  string(afterBytes),
		}
		return ed.CompleteTaskTurn(taskID, rev)
	})
	if err != nil {
		return editor.TaskReview{}, err
	}
	s.ReleaseTaskHolds(taskID)
	return rev, nil
}
