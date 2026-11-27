package ai

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/core"
)

// PathOptions configures bounded and partial pathfinding queries across grid A*
// and 2D/3D navigation meshes.
type PathOptions struct {
	// MaxExpansions caps the number of search nodes expanded before stopping.
	// Zero means unlimited expansions (for AStar, zero falls back to AStar.MaxExpansions when > 0);
	// a negative value always forces unlimited expansions.
	MaxExpansions int
	// AllowPartial returns a path to the explored node closest to the goal when
	// the goal is unreachable or MaxExpansions is reached, flagged with Partial = true.
	AllowPartial bool
}

// SearchStatus reports the lifecycle state of an incremental path search.
type SearchStatus uint8

const (
	// SearchInProgress indicates the search still has work remaining and has not reached the goal or its expansion budget.
	SearchInProgress SearchStatus = iota
	// SearchComplete indicates a complete path to the goal was found.
	SearchComplete
	// SearchPartial indicates a partial path to the closest explored node was produced
	// because the goal was unreachable or MaxExpansions was reached with AllowPartial enabled.
	SearchPartial
	// SearchFailed indicates no path was produced (invalid endpoints, unreachable goal without AllowPartial, or budget exhausted without AllowPartial).
	SearchFailed
)

// Done reports whether the search has reached a terminal state (complete, partial, or failed).
func (s SearchStatus) Done() bool {
	return s != SearchInProgress
}

// String returns a human-readable status name.
func (s SearchStatus) String() string {
	switch s {
	case SearchInProgress:
		return "in_progress"
	case SearchComplete:
		return "complete"
	case SearchPartial:
		return "partial"
	case SearchFailed:
		return "failed"
	default:
		return fmt.Sprintf("ai.SearchStatus(%d)", uint8(s))
	}
}

// IncrementalSearch is a deterministic path search that advances in bounded expansion steps.
//
// *AStarSearch, *NavMeshSearch, and *world3d.NavMeshSearch all implement IncrementalSearch
// so a single PathQueue can service mixed 2D grid, 2D NavMesh, and 3D NavMesh requests
// within one shared per-step expansion budget.
type IncrementalSearch interface {
	// Step advances the search by at most maxExpansions node expansions and returns
	// the updated status along with the exact number of expansions performed.
	// When maxExpansions <= 0 or the search is already done, Step performs 0 expansions.
	Step(maxExpansions int) (status SearchStatus, expansions int)
	// Status returns the search's current status.
	Status() SearchStatus
	// Expansions returns the cumulative number of node expansions performed so far.
	Expansions() int
}

var (
	_ IncrementalSearch = (*AStarSearch)(nil)
	_ IncrementalSearch = (*NavMeshSearch)(nil)
)

// PathRequestID uniquely identifies a request submitted to a PathQueue.
// Zero is never assigned to a valid request.
type PathRequestID uint64

// PathQueuePolicy controls how a PathQueue divides its per-step expansion budget
// among active requests within the same priority tier.
type PathQueuePolicy uint8

const (
	// PathQueueFIFO runs the highest-priority earliest-submitted request until it
	// completes or the step's remaining expansion budget is spent.
	PathQueueFIFO PathQueuePolicy = iota
	// PathQueueRoundRobin shares the step budget fairly across all active requests
	// in the highest pending priority tier in deterministic rotation order.
	PathQueueRoundRobin
)

// PathQueueConfig configures a PathQueue.
type PathQueueConfig struct {
	// DefaultBudget is the per-step expansion budget used by StepDefault.
	DefaultBudget int
	// Policy selects FIFO (default) or fair round-robin scheduling within each priority tier.
	Policy PathQueuePolicy
	// SliceSize caps the expansions granted per turn in PathQueueRoundRobin mode.
	// Zero or negative divides the remaining step budget evenly across requests in the active priority tier.
	SliceSize int
}

type pathQueueEntry struct {
	id       PathRequestID
	priority int
	search   IncrementalSearch
	onDone   func(PathRequestID, SearchStatus)
}

// PathQueue services multiple incremental path searches within a strict per-step
// node-expansion budget.
//
// All scheduling is deterministic, single-threaded, and free of background goroutines.
type PathQueue struct {
	defaultBudget   int
	policy          PathQueuePolicy
	sliceSize       int
	nextID          PathRequestID
	entries         []pathQueueEntry
	totalExpansions int
	completed       int
}

// NewPathQueue returns a new PathQueue with the given configuration.
func NewPathQueue(cfg PathQueueConfig) *PathQueue {
	return &PathQueue{
		defaultBudget: cfg.DefaultBudget,
		policy:        cfg.Policy,
		sliceSize:     cfg.SliceSize,
	}
}

// Submit enqueues search with the given priority (higher values run first; ties break
// in FIFO submission order). When the search reaches a terminal status during Step,
// onDone (if non-nil) is invoked synchronously with the request ID and final status.
//
// Submitting a nil search is ignored and returns 0.
func (q *PathQueue) Submit(search IncrementalSearch, priority int, onDone func(PathRequestID, SearchStatus)) PathRequestID {
	if q == nil || search == nil {
		return 0
	}
	q.nextID++
	id := q.nextID
	entry := pathQueueEntry{
		id:       id,
		priority: priority,
		search:   search,
		onDone:   onDone,
	}
	// Insert after all existing entries with priority >= entry.priority to preserve FIFO order per tier.
	pos := len(q.entries)
	for i := range q.entries {
		if q.entries[i].priority < priority {
			pos = i
			break
		}
	}
	q.entries = append(q.entries, pathQueueEntry{})
	copy(q.entries[pos+1:], q.entries[pos:])
	q.entries[pos] = entry
	return id
}

// SubmitGrid creates an incremental AStarSearch and enqueues it on q, returning the
// request ID (for Cancel) and the search handle (for polling Status/Result).
// If onDone is non-nil, it is called with the completed GridPathResult once the search finishes.
func (q *PathQueue) SubmitGrid(a *AStar, g Grid, start, goal Cell, opts PathOptions, priority int, onDone func(PathRequestID, GridPathResult)) (PathRequestID, *AStarSearch) {
	if q == nil {
		return 0, nil
	}
	search := NewAStarSearch(a, g, start, goal, opts)
	var cb func(PathRequestID, SearchStatus)
	if onDone != nil {
		cb = func(id PathRequestID, _ SearchStatus) {
			onDone(id, search.Result())
		}
	}
	id := q.Submit(search, priority, cb)
	return id, search
}

// SubmitNavMesh creates an incremental NavMeshSearch and enqueues it on q, returning the
// request ID (for Cancel) and the search handle (for polling Status/Result).
// If onDone is non-nil, it is called with the completed NavPathResult once the search finishes.
func (q *PathQueue) SubmitNavMesh(m *NavMesh, start, goal core.Point, opts PathOptions, priority int, onDone func(PathRequestID, NavPathResult)) (PathRequestID, *NavMeshSearch) {
	if q == nil {
		return 0, nil
	}
	search := NewNavMeshSearch(m, start, goal, opts)
	var cb func(PathRequestID, SearchStatus)
	if onDone != nil {
		cb = func(id PathRequestID, _ SearchStatus) {
			onDone(id, search.Result())
		}
	}
	id := q.Submit(search, priority, cb)
	return id, search
}

// Cancel removes a pending request from the queue. It returns true if the request was found
// and removed; canceled requests are not stepped again and their callbacks are not invoked.
func (q *PathQueue) Cancel(id PathRequestID) bool {
	if q == nil || id == 0 {
		return false
	}
	for i := range q.entries {
		if q.entries[i].id == id {
			copy(q.entries[i:], q.entries[i+1:])
			q.entries[len(q.entries)-1] = pathQueueEntry{}
			q.entries = q.entries[:len(q.entries)-1]
			return true
		}
	}
	return false
}

// Clear removes all pending requests without invoking callbacks.
func (q *PathQueue) Clear() {
	if q == nil {
		return
	}
	clear(q.entries)
	q.entries = q.entries[:0]
}

// Pending returns the number of active requests currently waiting in the queue.
func (q *PathQueue) Pending() int {
	if q == nil {
		return 0
	}
	return len(q.entries)
}

// IsPending reports whether id is currently queued and has not finished or been canceled.
func (q *PathQueue) IsPending(id PathRequestID) bool {
	if q == nil || id == 0 {
		return false
	}
	for i := range q.entries {
		if q.entries[i].id == id {
			return true
		}
	}
	return false
}

// TotalExpansions returns the cumulative number of node expansions performed across all Step calls.
func (q *PathQueue) TotalExpansions() int {
	if q == nil {
		return 0
	}
	return q.totalExpansions
}

// Completed returns the cumulative number of requests that finished (complete, partial, or failed).
func (q *PathQueue) Completed() int {
	if q == nil {
		return 0
	}
	return q.completed
}

// StepDefault advances queued requests using the queue's configured DefaultBudget.
func (q *PathQueue) StepDefault() int {
	if q == nil {
		return 0
	}
	return q.Step(q.defaultBudget)
}

// Step advances queued requests using at most budget total node expansions, returning
// the exact number of expansions performed (always in [0, budget] when budget > 0, or 0 when budget <= 0).
func (q *PathQueue) Step(budget int) int {
	if q == nil || budget <= 0 || len(q.entries) == 0 {
		return 0
	}

	remaining := budget
	used := 0
	stalled := 0
	roundLeft := 0
	roundPriority := 0

	for remaining > 0 && len(q.entries) > 0 {
		entry := q.entries[0]

		// Determine how many entries share the highest active priority tier.
		tierLen := 1
		if q.policy == PathQueueRoundRobin {
			for tierLen < len(q.entries) && q.entries[tierLen].priority == entry.priority {
				tierLen++
			}
		}

		// Finish already-terminal entries immediately without consuming budget.
		if st := entry.search.Status(); st.Done() {
			q.removeAt(0)
			q.completed++
			stalled = 0
			if roundLeft > 0 && entry.priority == roundPriority {
				roundLeft--
			}
			if entry.onDone != nil {
				entry.onDone(entry.id, st)
			}
			continue
		}

		slice := remaining
		if q.policy == PathQueueRoundRobin {
			if q.sliceSize > 0 {
				slice = min(q.sliceSize, remaining)
			} else {
				if roundLeft <= 0 || entry.priority != roundPriority {
					roundPriority = entry.priority
					roundLeft = tierLen
				}
				slice = max(1, remaining/roundLeft)
			}
		}

		st, spent := entry.search.Step(slice)
		if spent < 0 {
			spent = 0
		}
		used += spent
		q.totalExpansions += spent
		remaining -= spent
		if remaining < 0 {
			remaining = 0
		}

		if st.Done() {
			q.removeAt(0)
			q.completed++
			stalled = 0
			if roundLeft > 0 && entry.priority == roundPriority {
				roundLeft--
			}
			if entry.onDone != nil {
				entry.onDone(entry.id, st)
			}
			continue
		}

		if spent == 0 {
			stalled++
			if q.policy != PathQueueRoundRobin || stalled >= tierLen {
				break
			}
		} else {
			stalled = 0
		}

		if q.policy == PathQueueRoundRobin {
			if roundLeft > 0 && entry.priority == roundPriority {
				roundLeft--
			}
			if tierLen > 1 {
				copy(q.entries[0:tierLen-1], q.entries[1:tierLen])
				q.entries[tierLen-1] = entry
			}
		}
	}

	return used
}

func (q *PathQueue) removeAt(i int) {
	copy(q.entries[i:], q.entries[i+1:])
	q.entries[len(q.entries)-1] = pathQueueEntry{}
	q.entries = q.entries[:len(q.entries)-1]
}
