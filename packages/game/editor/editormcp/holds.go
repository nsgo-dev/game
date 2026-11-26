package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
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
	onBlocked    func(taskID, objectID, holderTaskID string)
}

func newObjectHoldManager() *objectHoldManager {
	return &objectHoldManager{
		heldBy:       make(map[string]string),
		taskHeld:     make(map[string][]string),
		sessionTasks: make(map[*mcp.ServerSession]string),
		changes:      make(map[string][]editor.TaskReviewChange),
		beforeYAML:   make(map[string]string),
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
// of the task's turn and release those holds if the session closes.
func (s *Server) ConnectTask(ctx context.Context, t mcp.Transport, taskID string) (*mcp.ServerSession, error) {
	ss, err := s.sdk.Connect(ctx, t, nil)
	if err != nil {
		return nil, err
	}
	s.BindSessionTask(ss, taskID)
	go func() {
		_ = ss.Wait()
		s.unbindSessionTask(ss)
	}()
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

// ReleaseTaskHolds releases every object hold owned by taskID and wakes waiting parallel tasks.
func (s *Server) ReleaseTaskHolds(taskID string) {
	if s == nil || taskID == "" {
		return
	}
	s.holds.mu.Lock()
	objs := s.holds.taskHeld[taskID]
	delete(s.holds.taskHeld, taskID)
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

// acquireObjectHolds acquires object holds on objects for taskID (or transiently for an unbound session
// when taskID is empty). When another task holds one of objects, the call updates taskID's holding state
// in the editor ("waiting on <objectID>") and waits until the hold ends, ctx is cancelled, or the server closes.
func (s *Server) acquireObjectHolds(ctx context.Context, taskID string, objects []string) (func(), error) {
	targets := normalizeTargetObjects(objects)
	if s == nil || len(targets) == 0 {
		return func() {}, nil
	}

	var waiter *holdWaiter
	defer func() {
		if waiter != nil {
			s.holds.mu.Lock()
			s.removeWaiterLocked(waiter)
			s.holds.mu.Unlock()
		}
	}()

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
			for _, id := range targets {
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
			return func() {}, nil
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
		case <-ctx.Done():
			if taskID != "" {
				_ = s.Editor(context.Background(), func(ed *editor.Editor) error {
					if t, ok := ed.Task(taskID); ok && t.State == editor.TaskRunning {
						return ed.SetTaskHoldState(taskID, existingHeld, nil, "", "")
					}
					return nil
				})
			}
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

// CompleteTaskTurn finishes taskID's active turn, produces its own separate editor.TaskReview
// from the edits recorded on its session, releases all object holds held by taskID (waking
// any parallel task waiting on those objects), and transitions the task to TaskReadyToReview.
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
	delete(s.holds.changes, taskID)
	delete(s.holds.beforeYAML, taskID)
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
	s.ReleaseTaskHolds(taskID)
	return rev, err
}
