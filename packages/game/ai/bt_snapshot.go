package ai

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"time"
)

// BTSnapshotVersion is the [BehaviorTreeSnapshot] schema version written and read
// by [SnapshotBehaviorTree] and [RestoreBehaviorTree].
const BTSnapshotVersion = 1

// ErrInvalidBTSnapshot reports a [BehaviorTreeSnapshot] that [RestoreBehaviorTree]
// refuses because its version, tree structure, node kinds, or state values do not
// match the target behavior tree.
var ErrInvalidBTSnapshot = errors.New("ai: invalid behavior tree snapshot")

const maxBTSnapshotDepth = 1024

// SnapshotNode is an optional interface that custom [Node] implementations may
// implement to participate in [SnapshotBehaviorTree] and [RestoreBehaviorTree].
//
// SnapshotKind returns a stable, non-empty identifier (for example "patrol")
// recorded in the snapshot as "custom:<kind>" to verify tree shape on restore.
// SnapshotState returns plain serializable data (scalars, slices, maps, or
// exported structs with json and yaml tags) representing the node's mutable
// runtime state, or nil when stateless. RestoreState validates and applies that
// state; on error it must leave the node unchanged.
type SnapshotNode interface {
	Node
	SnapshotKind() string
	SnapshotState() (any, error)
	RestoreState(state any) error
}

// ChildNodes is an optional interface that custom composite or decorator [Node]
// implementations may implement so [SnapshotBehaviorTree] and [RestoreBehaviorTree]
// traverse and preserve the state of their child nodes.
type ChildNodes interface {
	Node
	Children() []Node
}

// BehaviorTree wraps a root [Node] and provides [BehaviorTree.Snapshot] and
// [BehaviorTree.Restore] methods alongside [Node.Tick] and [Node.Reset].
type BehaviorTree struct {
	root Node
}

// NewBehaviorTree wraps root in a [BehaviorTree].
func NewBehaviorTree(root Node) *BehaviorTree {
	return &BehaviorTree{root: root}
}

// Root returns the tree's root [Node], or nil if bt is nil.
func (bt *BehaviorTree) Root() Node {
	if bt == nil {
		return nil
	}
	return bt.root
}

// Tick advances the root node by dt, returning [StatusFailure] when bt or its root is nil.
func (bt *BehaviorTree) Tick(dt time.Duration) Status {
	if bt == nil || bt.root == nil {
		return StatusFailure
	}
	return bt.root.Tick(dt)
}

// Reset clears any in-progress state on the root node and its descendants.
func (bt *BehaviorTree) Reset() {
	if bt != nil && bt.root != nil {
		bt.root.Reset()
	}
}

func (bt *BehaviorTree) hasBBObserver() bool {
	if bt == nil {
		return false
	}
	return nodeHasBBObserver(bt.root)
}

func (bt *BehaviorTree) bbWantsPreempt() bool {
	if bt == nil {
		return false
	}
	return nodeWantsPreempt(bt.root)
}

// Snapshot captures bt's structural shape and mutable runtime state.
func (bt *BehaviorTree) Snapshot() (BehaviorTreeSnapshot, error) {
	if bt == nil {
		return BehaviorTreeSnapshot{}, fmt.Errorf("%w: nil behavior tree", ErrInvalidBTSnapshot)
	}
	return SnapshotBehaviorTree(bt.root)
}

// Restore validates snap against bt's structure and applies it atomically without
// mutating any node on error.
func (bt *BehaviorTree) Restore(snap BehaviorTreeSnapshot) error {
	if bt == nil {
		return fmt.Errorf("%w: nil behavior tree", ErrInvalidBTSnapshot)
	}
	return RestoreBehaviorTree(bt.root, snap)
}

// BehaviorTreeSnapshot holds the structural shape and mutable runtime state of a
// behavior tree as plain data that marshals deterministically to YAML and JSON.
type BehaviorTreeSnapshot struct {
	// Version is BTSnapshotVersion.
	Version int `json:"version" yaml:"version"`
	// Nodes lists every node in deterministic pre-order traversal, pairing its
	// structural path (such as "0/1/2") and node kind with any active runtime state.
	Nodes []BTNodeSnapshot `json:"nodes" yaml:"nodes"`
}

// BTNodeSnapshot records one node's structural path, kind, and mutable runtime
// state inside a [BehaviorTreeSnapshot].
type BTNodeSnapshot struct {
	// Path is the deterministic structural path from the root ("0", "0/0", "0/1/2", ...).
	Path string `json:"path" yaml:"path"`
	// Kind identifies the node type (such as "sequence", "wait", or "custom:patrol").
	Kind string `json:"kind" yaml:"kind"`

	// Policy records the completion policy for parallel nodes ("require_all" or "require_one").
	Policy BTParallelPolicyName `json:"policy,omitempty" yaml:"policy,omitempty"`

	// Active reports whether a parallel, random_selector, random_sequence, weighted_selector,
	// or service node is currently mid-run.
	Active bool `json:"active,omitempty" yaml:"active,omitempty"`
	// Running reports whether a cooldown, guard, blackboard_condition, or attack_token
	// decorator currently has a running child.
	Running bool `json:"running,omitempty" yaml:"running,omitempty"`

	// Index is the current child index for sequence, random_selector, and random_sequence nodes.
	Index int `json:"index,omitempty" yaml:"index,omitempty"`
	// RunningChild is the currently running child index for selector, weighted_selector,
	// and utility_selector nodes (nil when no child is running).
	RunningChild *int `json:"runningChild,omitempty" yaml:"runningChild,omitempty"`
	// ActiveChild is the currently selected child index for utility_selector nodes
	// (nil when no action is active).
	ActiveChild *int `json:"activeChild,omitempty" yaml:"activeChild,omitempty"`

	// ChildStatuses holds per-child statuses for an active parallel node.
	ChildStatuses []Status `json:"childStatuses,omitempty" yaml:"childStatuses,omitempty"`
	// Order holds the active shuffled child permutation for random_selector and random_sequence nodes.
	Order []int `json:"order,omitempty" yaml:"order,omitempty"`
	// RemainingOptions marks which options remain untried in an active weighted_selector node.
	RemainingOptions []bool `json:"remainingOptions,omitempty" yaml:"remainingOptions,omitempty"`

	// Done is the number of completed iterations in a repeater node.
	Done int `json:"done,omitempty" yaml:"done,omitempty"`
	// Failures is the number of failed attempts recorded in a retry node.
	Failures int `json:"failures,omitempty" yaml:"failures,omitempty"`

	// Elapsed is the accumulated time in a wait or time_limit node.
	Elapsed time.Duration `json:"elapsed,omitempty" yaml:"elapsed,omitempty"`
	// Remaining is the cooldown time left in a cooldown node.
	Remaining time.Duration `json:"remaining,omitempty" yaml:"remaining,omitempty"`
	// Phase is the periodic phase accumulator in an active service node.
	Phase time.Duration `json:"phase,omitempty" yaml:"phase,omitempty"`
	// SinceLast is the elapsed time since the last callback in an active service node.
	SinceLast time.Duration `json:"sinceLast,omitempty" yaml:"sinceLast,omitempty"`

	// SeenRev reports whether a blackboard_condition node has observed a key revision.
	SeenRev bool `json:"seenRev,omitempty" yaml:"seenRev,omitempty"`
	// LastRev is the last blackboard key revision observed by a blackboard_condition node.
	LastRev uint64 `json:"lastRev,omitempty" yaml:"lastRev,omitempty"`
	// LastResult is the cached condition result from the last revision check of a blackboard_condition node.
	LastResult bool `json:"lastResult,omitempty" yaml:"lastResult,omitempty"`

	// State holds custom serializable state returned by a [SnapshotNode].
	State any `json:"state,omitempty" yaml:"state,omitempty"`
}

type btUtilityNode interface {
	btUtilityChildren() []Node
	btUtilityState() (active, running int)
	btRestoreUtilityState(active, running int)
}

type btTreeEntry struct {
	path string
	kind string
	node Node
}

// SnapshotBehaviorTree captures the structural shape and mutable runtime state of
// the behavior tree rooted at root. The returned snapshot contains only plain data
// suitable for deterministic YAML and JSON encoding and shares no mutable slices
// with root.
func SnapshotBehaviorTree(root Node) (BehaviorTreeSnapshot, error) {
	if isNilNode(root) {
		return BehaviorTreeSnapshot{}, fmt.Errorf("%w: nil root node", ErrInvalidBTSnapshot)
	}
	var entries []btTreeEntry
	ancestors := make(map[uintptr]bool)
	if err := walkBTTree(root, "0", 0, ancestors, &entries); err != nil {
		return BehaviorTreeSnapshot{}, err
	}
	nodes := make([]BTNodeSnapshot, len(entries))
	for i, e := range entries {
		rec, err := captureBTNode(e)
		if err != nil {
			return BehaviorTreeSnapshot{}, err
		}
		nodes[i] = rec
	}
	return BehaviorTreeSnapshot{
		Version: BTSnapshotVersion,
		Nodes:   nodes,
	}, nil
}

// RestoreBehaviorTree validates snap against the tree rooted at root and restores
// every node's runtime state atomically.
//
// If snap has an unsupported version, does not match root's exact structural shape
// and node kinds, contains out-of-range values, or a custom [SnapshotNode] refuses
// its state, RestoreBehaviorTree returns an error wrapping [ErrInvalidBTSnapshot]
// naming the mismatch path and leaves root unchanged.
func RestoreBehaviorTree(root Node, snap BehaviorTreeSnapshot) error {
	if isNilNode(root) {
		return fmt.Errorf("%w: nil root node", ErrInvalidBTSnapshot)
	}
	if snap.Version != BTSnapshotVersion {
		return fmt.Errorf("%w: unsupported version %d (want %d)", ErrInvalidBTSnapshot, snap.Version, BTSnapshotVersion)
	}
	var entries []btTreeEntry
	ancestors := make(map[uintptr]bool)
	if err := walkBTTree(root, "0", 0, ancestors, &entries); err != nil {
		return err
	}

	// Phase 1a: strict structural shape and kind check.
	common := min(len(snap.Nodes), len(entries))
	for i := range common {
		got := &snap.Nodes[i]
		want := entries[i]
		if got.Path != want.path {
			return fmt.Errorf("%w: tree shape mismatch at node %d: snapshot path %q, tree path %q", ErrInvalidBTSnapshot, i, got.Path, want.path)
		}
		if got.Kind != want.kind {
			return fmt.Errorf("%w: node kind mismatch at path %q: snapshot has %q, tree has %q", ErrInvalidBTSnapshot, want.path, got.Kind, want.kind)
		}
	}
	if len(snap.Nodes) > len(entries) {
		extra := snap.Nodes[len(entries)]
		return fmt.Errorf("%w: tree shape mismatch: snapshot has extra node at path %q (kind %q)", ErrInvalidBTSnapshot, extra.Path, extra.Kind)
	}
	if len(snap.Nodes) < len(entries) {
		missing := entries[len(snap.Nodes)]
		return fmt.Errorf("%w: tree shape mismatch: snapshot missing node at path %q (kind %q)", ErrInvalidBTSnapshot, missing.path, missing.kind)
	}

	// Phase 1b: validate all built-in node states and stage infallible commit closures.
	type customPending struct {
		path  string
		node  SnapshotNode
		state any
	}
	appliers := make([]func(), 0, len(entries))
	var customNodes []customPending

	for i, e := range entries {
		rec := &snap.Nodes[i]
		apply, custom, err := validateBTNodeRestore(e, rec)
		if err != nil {
			return err
		}
		if apply != nil {
			appliers = append(appliers, apply)
		}
		if custom != nil {
			customNodes = append(customNodes, customPending{
				path:  e.path,
				node:  custom,
				state: rec.State,
			})
		}
	}

	// Phase 2: apply custom SnapshotNodes transactionally with rollback on error.
	if len(customNodes) > 0 {
		saved := make([]any, len(customNodes))
		for i, c := range customNodes {
			prev, err := c.node.SnapshotState()
			if err != nil {
				return fmt.Errorf("%w: path %q: capture rollback state: %w", ErrInvalidBTSnapshot, c.path, err)
			}
			saved[i] = prev
		}
		for i, c := range customNodes {
			if err := c.node.RestoreState(c.state); err != nil {
				for j := i - 1; j >= 0; j-- {
					_ = customNodes[j].node.RestoreState(saved[j])
				}
				return fmt.Errorf("%w: path %q: %w", ErrInvalidBTSnapshot, c.path, err)
			}
		}
	}

	// Phase 3: commit all pre-validated built-in node states (infallible).
	for _, apply := range appliers {
		apply()
	}
	return nil
}

func isNilNode(n Node) bool {
	if n == nil {
		return true
	}
	rv := reflect.ValueOf(n)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func nodePtrKey(n Node) (uintptr, bool) {
	if n == nil {
		return 0, false
	}
	rv := reflect.ValueOf(n)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		return rv.Pointer(), true
	}
	return 0, false
}

func childPath(parent string, idx int) string {
	return parent + "/" + strconv.Itoa(idx)
}

func btParallelPolicyName(p ParallelPolicy) BTParallelPolicyName {
	if p == RequireOne {
		return BTParallelRequireOne
	}
	return BTParallelRequireAll
}

func walkBTTree(n Node, p string, depth int, ancestors map[uintptr]bool, out *[]btTreeEntry) error {
	if depth > maxBTSnapshotDepth {
		return fmt.Errorf("%w: path %q: behavior tree exceeds maximum depth %d", ErrInvalidBTSnapshot, p, maxBTSnapshotDepth)
	}
	if isNilNode(n) {
		*out = append(*out, btTreeEntry{path: p, kind: "nil", node: nil})
		return nil
	}

	ptrKey, hasPtr := nodePtrKey(n)
	if hasPtr {
		if ancestors[ptrKey] {
			return fmt.Errorf("%w: path %q: cycle detected in behavior tree", ErrInvalidBTSnapshot, p)
		}
		ancestors[ptrKey] = true
		defer delete(ancestors, ptrKey)
	}

	kind, err := btNodeKind(n, p)
	if err != nil {
		return err
	}
	*out = append(*out, btTreeEntry{path: p, kind: kind, node: n})

	switch v := n.(type) {
	case *sequenceNode:
		for i, ch := range v.children {
			if err := walkBTTree(ch, childPath(p, i), depth+1, ancestors, out); err != nil {
				return err
			}
		}
	case *selectorNode:
		for i, ch := range v.children {
			if err := walkBTTree(ch, childPath(p, i), depth+1, ancestors, out); err != nil {
				return err
			}
		}
	case *parallelNode:
		for i, ch := range v.children {
			if err := walkBTTree(ch, childPath(p, i), depth+1, ancestors, out); err != nil {
				return err
			}
		}
	case *randomSelectorNode:
		for i, ch := range v.children {
			if err := walkBTTree(ch, childPath(p, i), depth+1, ancestors, out); err != nil {
				return err
			}
		}
	case *randomSequenceNode:
		for i, ch := range v.children {
			if err := walkBTTree(ch, childPath(p, i), depth+1, ancestors, out); err != nil {
				return err
			}
		}
	case *weightedSelectorNode:
		for i, opt := range v.options {
			if err := walkBTTree(opt.Node, childPath(p, i), depth+1, ancestors, out); err != nil {
				return err
			}
		}
	case btUtilityNode:
		for i, ch := range v.btUtilityChildren() {
			if err := walkBTTree(ch, childPath(p, i), depth+1, ancestors, out); err != nil {
				return err
			}
		}
	case *inverterNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *succeederNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *failerNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *repeaterNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *untilFailNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *cooldownNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *guardNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *timeLimitNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *retryNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *serviceNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *blackboardConditionNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *attackTokenNode:
		if !isNilNode(v.child) {
			return walkBTTree(v.child, childPath(p, 0), depth+1, ancestors, out)
		}
	case *BehaviorTree:
		if !isNilNode(v.root) {
			return walkBTTree(v.root, childPath(p, 0), depth+1, ancestors, out)
		}
	case actionNode, conditionNode, *waitNode:
		// Leaves have no children.
	default:
		if cn, ok := n.(ChildNodes); ok {
			for i, ch := range cn.Children() {
				if err := walkBTTree(ch, childPath(p, i), depth+1, ancestors, out); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func btNodeKind(n Node, p string) (string, error) {
	switch v := n.(type) {
	case *sequenceNode:
		return string(BTNodeSequence), nil
	case *selectorNode:
		return string(BTNodeSelector), nil
	case *parallelNode:
		return string(BTNodeParallel), nil
	case *inverterNode:
		return string(BTNodeInverter), nil
	case *succeederNode:
		return string(BTNodeSucceeder), nil
	case *failerNode:
		return string(BTNodeFailer), nil
	case *repeaterNode:
		return string(BTNodeRepeater), nil
	case *untilFailNode:
		return string(BTNodeUntilFail), nil
	case *cooldownNode:
		return string(BTNodeCooldown), nil
	case *guardNode:
		return string(BTNodeGuard), nil
	case actionNode:
		return string(BTNodeAction), nil
	case conditionNode:
		return string(BTNodeCondition), nil
	case *waitNode:
		return string(BTNodeWait), nil
	case *timeLimitNode:
		return string(BTNodeTimeLimit), nil
	case *retryNode:
		return string(BTNodeRetry), nil
	case *randomSelectorNode:
		return string(BTNodeRandomSelector), nil
	case *randomSequenceNode:
		return string(BTNodeRandomSequence), nil
	case *weightedSelectorNode:
		return string(BTNodeWeightedSelector), nil
	case *serviceNode:
		return string(BTNodeService), nil
	case *blackboardConditionNode:
		return string(BTNodeBlackboardCondition), nil
	case *attackTokenNode:
		return "attack_token", nil
	case btUtilityNode:
		return "utility_selector", nil
	case *BehaviorTree:
		return "behavior_tree", nil
	default:
		if sn, ok := v.(SnapshotNode); ok {
			k := sn.SnapshotKind()
			if k == "" {
				return "", fmt.Errorf("%w: path %q: custom SnapshotNode returned empty SnapshotKind", ErrInvalidBTSnapshot, p)
			}
			return "custom:" + k, nil
		}
		return fmt.Sprintf("custom:%T", v), nil
	}
}

func intPtr(v int) *int {
	return &v
}

func captureBTNode(e btTreeEntry) (BTNodeSnapshot, error) {
	rec := BTNodeSnapshot{
		Path: e.path,
		Kind: e.kind,
	}
	if e.node == nil {
		return rec, nil
	}
	switch v := e.node.(type) {
	case *sequenceNode:
		rec.Index = v.cur
	case *selectorNode:
		if v.running >= 0 {
			rec.RunningChild = intPtr(v.running)
		}
	case *parallelNode:
		rec.Policy = btParallelPolicyName(v.policy)
		if v.active {
			rec.Active = true
			rec.ChildStatuses = slices.Clone(v.status)
		}
	case *repeaterNode:
		rec.Done = v.done
	case *cooldownNode:
		rec.Remaining = v.remaining
		rec.Running = v.running
	case *guardNode:
		rec.Running = v.running
	case *waitNode:
		rec.Elapsed = v.elapsed
	case *timeLimitNode:
		rec.Elapsed = v.elapsed
	case *retryNode:
		rec.Failures = v.failures
	case *randomSelectorNode:
		if v.active {
			rec.Active = true
			rec.Index = v.cur
			rec.Order = slices.Clone(v.order)
		}
	case *randomSequenceNode:
		if v.active {
			rec.Active = true
			rec.Index = v.cur
			rec.Order = slices.Clone(v.order)
		}
	case *weightedSelectorNode:
		if v.active {
			rec.Active = true
			if v.running >= 0 {
				rec.RunningChild = intPtr(v.running)
			}
			rem := make([]bool, len(v.remWeights))
			for i, w := range v.remWeights {
				rem[i] = w > 0
			}
			rec.RemainingOptions = rem
		}
	case *serviceNode:
		if v.active {
			rec.Active = true
			rec.Phase = v.phase
			rec.SinceLast = v.sinceLast
		}
	case *blackboardConditionNode:
		rec.Running = v.running
		rec.SeenRev = v.seenRev
		rec.LastRev = v.lastRev
		rec.LastResult = v.lastResult
	case *attackTokenNode:
		rec.Running = v.running
	case btUtilityNode:
		active, running := v.btUtilityState()
		if active >= 0 {
			rec.ActiveChild = intPtr(active)
		}
		if running >= 0 {
			rec.RunningChild = intPtr(running)
		}
	case *inverterNode, *succeederNode, *failerNode, *untilFailNode, actionNode, conditionNode, *BehaviorTree:
		// Stateless built-in nodes.
	default:
		if sn, ok := v.(SnapshotNode); ok {
			st, err := sn.SnapshotState()
			if err != nil {
				return BTNodeSnapshot{}, fmt.Errorf("%w: path %q: %w", ErrInvalidBTSnapshot, e.path, err)
			}
			rec.State = st
		}
	}
	return rec, nil
}

func validateBTNodeRestore(e btTreeEntry, rec *BTNodeSnapshot) (func(), SnapshotNode, error) {
	if e.node == nil {
		return nil, nil, nil
	}
	switch v := e.node.(type) {
	case *sequenceNode:
		if len(v.children) == 0 {
			if rec.Index != 0 {
				return nil, nil, fmt.Errorf("%w: path %q: sequence index %d out of range for empty sequence", ErrInvalidBTSnapshot, e.path, rec.Index)
			}
		} else if rec.Index < 0 || rec.Index >= len(v.children) {
			return nil, nil, fmt.Errorf("%w: path %q: sequence index %d out of range [0, %d)", ErrInvalidBTSnapshot, e.path, rec.Index, len(v.children))
		}
		idx := rec.Index
		return func() { v.cur = idx }, nil, nil

	case *selectorNode:
		running := -1
		if rec.RunningChild != nil {
			r := *rec.RunningChild
			if r < 0 || r >= len(v.children) {
				return nil, nil, fmt.Errorf("%w: path %q: selector runningChild %d out of range [0, %d)", ErrInvalidBTSnapshot, e.path, r, len(v.children))
			}
			running = r
		}
		return func() { v.running = running }, nil, nil

	case *parallelNode:
		wantPolicy := btParallelPolicyName(v.policy)
		if rec.Policy != "" && rec.Policy != wantPolicy {
			return nil, nil, fmt.Errorf("%w: path %q: parallel policy mismatch: snapshot has %q, tree has %q", ErrInvalidBTSnapshot, e.path, rec.Policy, wantPolicy)
		}
		if !rec.Active {
			if len(rec.ChildStatuses) != 0 {
				return nil, nil, fmt.Errorf("%w: path %q: inactive parallel node cannot carry childStatuses", ErrInvalidBTSnapshot, e.path)
			}
			return func() {
				v.active = false
				clear(v.status)
			}, nil, nil
		}
		if len(v.children) == 0 {
			return nil, nil, fmt.Errorf("%w: path %q: empty parallel node cannot be active", ErrInvalidBTSnapshot, e.path)
		}
		if len(rec.ChildStatuses) != len(v.children) {
			return nil, nil, fmt.Errorf("%w: path %q: parallel childStatuses length %d, want %d", ErrInvalidBTSnapshot, e.path, len(rec.ChildStatuses), len(v.children))
		}
		runningCount, successCount, failureCount := 0, 0, 0
		for i, st := range rec.ChildStatuses {
			switch st {
			case StatusSuccess:
				successCount++
			case StatusFailure:
				failureCount++
			case StatusRunning:
				runningCount++
			default:
				return nil, nil, fmt.Errorf("%w: path %q: parallel child %d has invalid status %d", ErrInvalidBTSnapshot, e.path, i, st)
			}
		}
		if runningCount == 0 {
			return nil, nil, fmt.Errorf("%w: path %q: active parallel node must have at least one running child", ErrInvalidBTSnapshot, e.path)
		}
		if v.policy == RequireOne && successCount > 0 {
			return nil, nil, fmt.Errorf("%w: path %q: active require_one parallel node cannot already have a succeeded child", ErrInvalidBTSnapshot, e.path)
		}
		if v.policy == RequireAll && failureCount > 0 {
			return nil, nil, fmt.Errorf("%w: path %q: active require_all parallel node cannot already have a failed child", ErrInvalidBTSnapshot, e.path)
		}
		statuses := slices.Clone(rec.ChildStatuses)
		return func() {
			v.active = true
			if len(v.status) != len(statuses) {
				v.status = make([]Status, len(statuses))
			}
			copy(v.status, statuses)
		}, nil, nil

	case *repeaterNode:
		if v.count <= 0 {
			if rec.Done != 0 {
				return nil, nil, fmt.Errorf("%w: path %q: infinite repeater cannot have done %d", ErrInvalidBTSnapshot, e.path, rec.Done)
			}
		} else if rec.Done < 0 || rec.Done >= v.count {
			return nil, nil, fmt.Errorf("%w: path %q: repeater done %d out of range [0, %d)", ErrInvalidBTSnapshot, e.path, rec.Done, v.count)
		}
		done := rec.Done
		return func() { v.done = done }, nil, nil

	case *cooldownNode:
		if rec.Remaining < 0 {
			return nil, nil, fmt.Errorf("%w: path %q: negative cooldown remaining %v", ErrInvalidBTSnapshot, e.path, rec.Remaining)
		}
		if v.duration <= 0 && rec.Remaining != 0 {
			return nil, nil, fmt.Errorf("%w: path %q: zero-duration cooldown cannot have remaining %v", ErrInvalidBTSnapshot, e.path, rec.Remaining)
		}
		if v.duration > 0 && rec.Remaining > v.duration {
			return nil, nil, fmt.Errorf("%w: path %q: cooldown remaining %v exceeds duration %v", ErrInvalidBTSnapshot, e.path, rec.Remaining, v.duration)
		}
		if rec.Running {
			if v.child == nil {
				return nil, nil, fmt.Errorf("%w: path %q: cooldown with nil child cannot be running", ErrInvalidBTSnapshot, e.path)
			}
			if rec.Remaining > 0 {
				return nil, nil, fmt.Errorf("%w: path %q: cooldown cannot be both running and cooling down (%v)", ErrInvalidBTSnapshot, e.path, rec.Remaining)
			}
		}
		rem, running := rec.Remaining, rec.Running
		return func() {
			v.remaining = rem
			v.running = running
		}, nil, nil

	case *guardNode:
		if rec.Running && v.child == nil {
			return nil, nil, fmt.Errorf("%w: path %q: guard with nil child cannot be running", ErrInvalidBTSnapshot, e.path)
		}
		running := rec.Running
		return func() { v.running = running }, nil, nil

	case *waitNode:
		if rec.Elapsed < 0 {
			return nil, nil, fmt.Errorf("%w: path %q: negative wait elapsed %v", ErrInvalidBTSnapshot, e.path, rec.Elapsed)
		}
		if v.duration <= 0 && rec.Elapsed != 0 {
			return nil, nil, fmt.Errorf("%w: path %q: non-positive wait cannot have elapsed %v", ErrInvalidBTSnapshot, e.path, rec.Elapsed)
		}
		if v.duration > 0 && rec.Elapsed >= v.duration {
			return nil, nil, fmt.Errorf("%w: path %q: wait elapsed %v must be less than duration %v", ErrInvalidBTSnapshot, e.path, rec.Elapsed, v.duration)
		}
		elapsed := rec.Elapsed
		return func() { v.elapsed = elapsed }, nil, nil

	case *timeLimitNode:
		if rec.Elapsed < 0 {
			return nil, nil, fmt.Errorf("%w: path %q: negative time_limit elapsed %v", ErrInvalidBTSnapshot, e.path, rec.Elapsed)
		}
		if v.limit <= 0 && rec.Elapsed != 0 {
			return nil, nil, fmt.Errorf("%w: path %q: non-positive time_limit cannot have elapsed %v", ErrInvalidBTSnapshot, e.path, rec.Elapsed)
		}
		if v.limit > 0 && rec.Elapsed >= v.limit {
			return nil, nil, fmt.Errorf("%w: path %q: time_limit elapsed %v must be less than limit %v", ErrInvalidBTSnapshot, e.path, rec.Elapsed, v.limit)
		}
		if rec.Elapsed > 0 && v.child == nil {
			return nil, nil, fmt.Errorf("%w: path %q: time_limit with nil child cannot have elapsed %v", ErrInvalidBTSnapshot, e.path, rec.Elapsed)
		}
		elapsed := rec.Elapsed
		return func() { v.elapsed = elapsed }, nil, nil

	case *retryNode:
		if v.retries <= 0 {
			if rec.Failures != 0 {
				return nil, nil, fmt.Errorf("%w: path %q: retry with count %d cannot have failures %d", ErrInvalidBTSnapshot, e.path, v.retries, rec.Failures)
			}
		} else if rec.Failures < 0 || rec.Failures > v.retries {
			return nil, nil, fmt.Errorf("%w: path %q: retry failures %d out of range [0, %d]", ErrInvalidBTSnapshot, e.path, rec.Failures, v.retries)
		}
		failures := rec.Failures
		return func() { v.failures = failures }, nil, nil

	case *randomSelectorNode:
		order, cur, active, err := validateRandomOrder(e.path, "random_selector", len(v.children), rec)
		if err != nil {
			return nil, nil, err
		}
		return func() {
			v.active = active
			v.cur = cur
			if active {
				if len(v.order) != len(order) {
					v.order = make([]int, len(order))
				}
				copy(v.order, order)
			} else {
				clear(v.order)
			}
		}, nil, nil

	case *randomSequenceNode:
		order, cur, active, err := validateRandomOrder(e.path, "random_sequence", len(v.children), rec)
		if err != nil {
			return nil, nil, err
		}
		return func() {
			v.active = active
			v.cur = cur
			if active {
				if len(v.order) != len(order) {
					v.order = make([]int, len(order))
				}
				copy(v.order, order)
			} else {
				clear(v.order)
			}
		}, nil, nil

	case *weightedSelectorNode:
		if !rec.Active {
			if rec.RunningChild != nil || len(rec.RemainingOptions) != 0 {
				return nil, nil, fmt.Errorf("%w: path %q: inactive weighted_selector cannot carry runningChild or remainingOptions", ErrInvalidBTSnapshot, e.path)
			}
			return func() {
				v.active = false
				v.running = -1
				clear(v.remWeights)
			}, nil, nil
		}
		if len(v.options) == 0 {
			return nil, nil, fmt.Errorf("%w: path %q: empty weighted_selector cannot be active", ErrInvalidBTSnapshot, e.path)
		}
		if len(rec.RemainingOptions) != len(v.options) {
			return nil, nil, fmt.Errorf("%w: path %q: weighted_selector remainingOptions length %d, want %d", ErrInvalidBTSnapshot, e.path, len(rec.RemainingOptions), len(v.options))
		}
		for i, rem := range rec.RemainingOptions {
			if rem && v.baseWeights[i] <= 0 {
				return nil, nil, fmt.Errorf("%w: path %q: weighted_selector option %d is marked remaining but is ineligible", ErrInvalidBTSnapshot, e.path, i)
			}
		}
		running := -1
		if rec.RunningChild != nil {
			r := *rec.RunningChild
			if r < 0 || r >= len(v.options) {
				return nil, nil, fmt.Errorf("%w: path %q: weighted_selector runningChild %d out of range [0, %d)", ErrInvalidBTSnapshot, e.path, r, len(v.options))
			}
			if v.baseWeights[r] <= 0 || v.options[r].Node == nil {
				return nil, nil, fmt.Errorf("%w: path %q: weighted_selector runningChild %d is ineligible", ErrInvalidBTSnapshot, e.path, r)
			}
			if rec.RemainingOptions[r] {
				return nil, nil, fmt.Errorf("%w: path %q: weighted_selector runningChild %d cannot still be marked remaining", ErrInvalidBTSnapshot, e.path, r)
			}
			running = r
		}
		remMask := slices.Clone(rec.RemainingOptions)
		return func() {
			v.active = true
			v.running = running
			if len(v.remWeights) != len(v.options) {
				v.remWeights = make([]float64, len(v.options))
			}
			for i, rem := range remMask {
				if rem {
					v.remWeights[i] = v.baseWeights[i]
				} else {
					v.remWeights[i] = 0
				}
			}
		}, nil, nil

	case *serviceNode:
		if !rec.Active {
			if rec.Phase != 0 || rec.SinceLast != 0 {
				return nil, nil, fmt.Errorf("%w: path %q: inactive service cannot have phase %v or sinceLast %v", ErrInvalidBTSnapshot, e.path, rec.Phase, rec.SinceLast)
			}
			return func() {
				v.active = false
				v.phase = 0
				v.sinceLast = 0
			}, nil, nil
		}
		if v.child == nil {
			return nil, nil, fmt.Errorf("%w: path %q: service with nil child cannot be active", ErrInvalidBTSnapshot, e.path)
		}
		if rec.Phase < 0 || rec.SinceLast < 0 {
			return nil, nil, fmt.Errorf("%w: path %q: service cannot have negative phase %v or sinceLast %v", ErrInvalidBTSnapshot, e.path, rec.Phase, rec.SinceLast)
		}
		if v.interval <= 0 {
			if rec.Phase != 0 || rec.SinceLast != 0 {
				return nil, nil, fmt.Errorf("%w: path %q: every-tick service cannot have phase %v or sinceLast %v", ErrInvalidBTSnapshot, e.path, rec.Phase, rec.SinceLast)
			}
		} else if rec.Phase >= v.interval {
			return nil, nil, fmt.Errorf("%w: path %q: service phase %v must be less than interval %v", ErrInvalidBTSnapshot, e.path, rec.Phase, v.interval)
		}
		phase, sinceLast := rec.Phase, rec.SinceLast
		return func() {
			v.active = true
			v.phase = phase
			v.sinceLast = sinceLast
		}, nil, nil

	case *blackboardConditionNode:
		if rec.Running && v.child == nil {
			return nil, nil, fmt.Errorf("%w: path %q: leaf blackboard_condition cannot be running", ErrInvalidBTSnapshot, e.path)
		}
		if !rec.SeenRev && (rec.LastRev != 0 || rec.LastResult) {
			return nil, nil, fmt.Errorf("%w: path %q: blackboard_condition with seenRev=false cannot carry lastRev or lastResult", ErrInvalidBTSnapshot, e.path)
		}
		running, seenRev, lastRev, lastResult := rec.Running, rec.SeenRev, rec.LastRev, rec.LastResult
		return func() {
			v.running = running
			v.seenRev = seenRev
			v.lastRev = lastRev
			v.lastResult = lastResult
		}, nil, nil

	case *attackTokenNode:
		if rec.Running && v.child == nil {
			return nil, nil, fmt.Errorf("%w: path %q: attack_token with nil child cannot be running", ErrInvalidBTSnapshot, e.path)
		}
		running := rec.Running
		return func() { v.running = running }, nil, nil

	case btUtilityNode:
		children := v.btUtilityChildren()
		active := -1
		running := -1
		if rec.ActiveChild != nil {
			a := *rec.ActiveChild
			if a < 0 || a >= len(children) {
				return nil, nil, fmt.Errorf("%w: path %q: utility_selector activeChild %d out of range [0, %d)", ErrInvalidBTSnapshot, e.path, a, len(children))
			}
			active = a
		}
		if rec.RunningChild != nil {
			r := *rec.RunningChild
			if r < 0 || r >= len(children) {
				return nil, nil, fmt.Errorf("%w: path %q: utility_selector runningChild %d out of range [0, %d)", ErrInvalidBTSnapshot, e.path, r, len(children))
			}
			if active != r {
				return nil, nil, fmt.Errorf("%w: path %q: utility_selector runningChild %d must match activeChild %d", ErrInvalidBTSnapshot, e.path, r, active)
			}
			running = r
		}
		return func() { v.btRestoreUtilityState(active, running) }, nil, nil

	case *inverterNode, *succeederNode, *failerNode, *untilFailNode, actionNode, conditionNode, *BehaviorTree:
		return nil, nil, nil

	default:
		if sn, ok := v.(SnapshotNode); ok {
			return nil, sn, nil
		}
		if rec.State != nil {
			return nil, nil, fmt.Errorf("%w: path %q: node %q carries state but does not implement SnapshotNode", ErrInvalidBTSnapshot, e.path, e.kind)
		}
		return nil, nil, nil
	}
}

func validateRandomOrder(p, kind string, n int, rec *BTNodeSnapshot) ([]int, int, bool, error) {
	if !rec.Active {
		if rec.Index != 0 || len(rec.Order) != 0 {
			return nil, 0, false, fmt.Errorf("%w: path %q: inactive %s cannot carry index or order", ErrInvalidBTSnapshot, p, kind)
		}
		return nil, 0, false, nil
	}
	if n == 0 {
		return nil, 0, false, fmt.Errorf("%w: path %q: empty %s cannot be active", ErrInvalidBTSnapshot, p, kind)
	}
	if len(rec.Order) != n {
		return nil, 0, false, fmt.Errorf("%w: path %q: %s order length %d, want %d", ErrInvalidBTSnapshot, p, kind, len(rec.Order), n)
	}
	if rec.Index < 0 || rec.Index >= n {
		return nil, 0, false, fmt.Errorf("%w: path %q: %s index %d out of range [0, %d)", ErrInvalidBTSnapshot, p, kind, rec.Index, n)
	}
	seen := make([]bool, n)
	for i, idx := range rec.Order {
		if idx < 0 || idx >= n {
			return nil, 0, false, fmt.Errorf("%w: path %q: %s order[%d]=%d out of range [0, %d)", ErrInvalidBTSnapshot, p, kind, i, idx, n)
		}
		if seen[idx] {
			return nil, 0, false, fmt.Errorf("%w: path %q: %s order contains duplicate index %d", ErrInvalidBTSnapshot, p, kind, idx)
		}
		seen[idx] = true
	}
	return slices.Clone(rec.Order), rec.Index, true, nil
}
