package ai

import (
	"math"
	"time"
)

// BlackboardOp identifies a comparison operator evaluated by [BlackboardCondition].
type BlackboardOp string

const (
	// BBOpEq holds when the blackboard key is set and equals the comparison value.
	BBOpEq BlackboardOp = "eq"
	// BBOpNe holds when the blackboard key is unset or differs from the comparison value.
	BBOpNe BlackboardOp = "ne"
	// BBOpLt holds when the blackboard key is set and strictly less than the comparison value.
	BBOpLt BlackboardOp = "lt"
	// BBOpLte holds when the blackboard key is set and less than or equal to the comparison value.
	BBOpLte BlackboardOp = "lte"
	// BBOpGt holds when the blackboard key is set and strictly greater than the comparison value.
	BBOpGt BlackboardOp = "gt"
	// BBOpGte holds when the blackboard key is set and greater than or equal to the comparison value.
	BBOpGte BlackboardOp = "gte"
	// BBOpSet holds when the blackboard key is present.
	BBOpSet BlackboardOp = "set"
	// BBOpUnset holds when the blackboard key is absent.
	BBOpUnset BlackboardOp = "unset"
)

// Valid reports whether op is a known [BlackboardOp].
func (op BlackboardOp) Valid() bool {
	switch op {
	case BBOpEq, BBOpNe, BBOpLt, BBOpLte, BBOpGt, BBOpGte, BBOpSet, BBOpUnset:
		return true
	default:
		return false
	}
}

// NeedsValue reports whether op compares against an explicit operand value.
func (op BlackboardOp) NeedsValue() bool {
	return op != BBOpSet && op != BBOpUnset
}

// AbortMode controls how a [BlackboardCondition] decorator interrupts running
// branches when its watched blackboard key changes.
type AbortMode string

const (
	// AbortNone checks the condition only on entry into the child.
	AbortNone AbortMode = "none"
	// AbortSelf aborts the decorator's own running child (or enclosing Sequence's
	// subsequent running child when used without a child) when the watched key
	// changes and the condition becomes false.
	AbortSelf AbortMode = "self"
	// AbortLower aborts a running lower-priority sibling in an enclosing Selector
	// on the next tick when the watched key changes and the condition becomes true.
	AbortLower AbortMode = "lower"
	// AbortBoth combines AbortSelf and AbortLower.
	AbortBoth AbortMode = "both"
)

// Valid reports whether m is a recognized [AbortMode] (including empty, which means AbortNone).
func (m AbortMode) Valid() bool {
	switch m {
	case "", AbortNone, AbortSelf, AbortLower, AbortBoth:
		return true
	default:
		return false
	}
}

func (m AbortMode) abortsSelf() bool  { return m == AbortSelf || m == AbortBoth }
func (m AbortMode) abortsLower() bool { return m == AbortLower || m == AbortBoth }

// btObserver is implemented internally by nodes that participate in blackboard
// observer-abort checks within an ancestor Selector.
type btObserver interface {
	hasBBObserver() bool
	bbWantsPreempt() bool
}

// btSelfAbortable is implemented internally by condition nodes that can abort a
// running subsequent sibling in an enclosing Sequence when their key changes.
type btSelfAbortable interface {
	bbSelfAborted() bool
}

func nodeHasBBObserver(n Node) bool {
	if obs, ok := n.(btObserver); ok {
		return obs.hasBBObserver()
	}
	return false
}

func nodeWantsPreempt(n Node) bool {
	if obs, ok := n.(btObserver); ok && obs.hasBBObserver() {
		return obs.bbWantsPreempt()
	}
	return false
}

func nodeHasBBSelfAbort(n Node) bool {
	if bc, ok := unwrapNamed(n).(*blackboardConditionNode); ok {
		return bc.abort.abortsSelf()
	}
	return false
}

// nodeHasNonBBGate reports whether n contains a non-blackboard gating node
// (such as Condition, Guard, or WithAttackToken) whose changes cannot be
// observed through blackboard key revisions alone.
//
// Called only at tree construction time, never during Tick.
func nodeHasNonBBGate(n Node) bool {
	return nodeHasNonBBGateVisited(n, nil)
}

func nodeHasNonBBGateVisited(n Node, visited map[uintptr]bool) bool {
	if isNilNode(n) {
		return false
	}
	switch v := n.(type) {
	case *conditionNode, *guardNode, *attackTokenNode:
		return true
	case *blackboardConditionNode:
		return v.childHasNonBBGate
	case *sequenceNode:
		return v.hasNonBBGate
	case *selectorNode:
		return v.hasNonBBGate
	case *namedNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *BehaviorTree:
		return nodeHasNonBBGateVisited(v.root, visited)
	case *inverterNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *succeederNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *failerNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *repeaterNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *untilFailNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *cooldownNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *timeLimitNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *retryNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *serviceNode:
		return nodeHasNonBBGateVisited(v.child, visited)
	case *parallelNode:
		for _, ch := range v.children {
			if nodeHasNonBBGateVisited(ch, visited) {
				return true
			}
		}
		return false
	case *randomSelectorNode:
		for _, ch := range v.children {
			if nodeHasNonBBGateVisited(ch, visited) {
				return true
			}
		}
		return false
	case *randomSequenceNode:
		for _, ch := range v.children {
			if nodeHasNonBBGateVisited(ch, visited) {
				return true
			}
		}
		return false
	case *weightedSelectorNode:
		for _, opt := range v.options {
			if nodeHasNonBBGateVisited(opt.Node, visited) {
				return true
			}
		}
		return false
	case btUtilityNode:
		for _, ch := range v.btUtilityChildren() {
			if nodeHasNonBBGateVisited(ch, visited) {
				return true
			}
		}
		return false
	default:
		if cn, ok := n.(ChildNodes); ok {
			if ptr, ok := nodePtrKey(n); ok {
				if visited == nil {
					visited = make(map[uintptr]bool)
				}
				if visited[ptr] {
					return false
				}
				visited[ptr] = true
			}
			for _, ch := range cn.Children() {
				if nodeHasNonBBGateVisited(ch, visited) {
					return true
				}
			}
		}
		return false
	}
}

// nodeContainsBBObserver reports whether n is or contains any BlackboardCondition.
// Called only at tree construction time, never during Tick.
func nodeContainsBBObserver(n Node) bool {
	return nodeContainsBBObserverVisited(n, nil)
}

func nodeContainsBBObserverVisited(n Node, visited map[uintptr]bool) bool {
	if isNilNode(n) {
		return false
	}
	switch v := n.(type) {
	case *blackboardConditionNode:
		return true
	case *sequenceNode:
		return v.hasAnyBB
	case *selectorNode:
		return v.hasAnyBB
	case *namedNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *BehaviorTree:
		return nodeContainsBBObserverVisited(v.root, visited)
	case *guardNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *attackTokenNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *inverterNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *succeederNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *failerNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *repeaterNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *untilFailNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *cooldownNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *timeLimitNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *retryNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *serviceNode:
		return nodeContainsBBObserverVisited(v.child, visited)
	case *parallelNode:
		for _, ch := range v.children {
			if nodeContainsBBObserverVisited(ch, visited) {
				return true
			}
		}
		return false
	case *randomSelectorNode:
		for _, ch := range v.children {
			if nodeContainsBBObserverVisited(ch, visited) {
				return true
			}
		}
		return false
	case *randomSequenceNode:
		for _, ch := range v.children {
			if nodeContainsBBObserverVisited(ch, visited) {
				return true
			}
		}
		return false
	case *weightedSelectorNode:
		for _, opt := range v.options {
			if nodeContainsBBObserverVisited(opt.Node, visited) {
				return true
			}
		}
		return false
	case btUtilityNode:
		for _, ch := range v.btUtilityChildren() {
			if nodeContainsBBObserverVisited(ch, visited) {
				return true
			}
		}
		return false
	default:
		if cn, ok := n.(ChildNodes); ok {
			if ptr, ok := nodePtrKey(n); ok {
				if visited == nil {
					visited = make(map[uintptr]bool)
				}
				if visited[ptr] {
					return false
				}
				visited[ptr] = true
			}
			for _, ch := range cn.Children() {
				if nodeContainsBBObserverVisited(ch, visited) {
					return true
				}
			}
		}
		return false
	}
}

// BlackboardCondition evaluates key on bb using op and value, optionally wrapping
// child as a decorator (when child is nil, it acts as a condition leaf returning
// StatusSuccess when the condition holds and StatusFailure otherwise).
//
// When abort is AbortSelf or AbortBoth, a key change that falsifies the condition
// while child is running resets child and returns StatusFailure on the next tick.
// When abort is AbortLower or AbortBoth, a key change that makes the condition
// true while a lower-priority sibling in an enclosing Selector is running aborts
// that lower-priority sibling on the next tick.
func BlackboardCondition(bb *Blackboard, key string, op BlackboardOp, value BlackboardValue, abort AbortMode, child Node) Node {
	if abort == "" {
		abort = AbortNone
	}
	n := &blackboardConditionNode{
		bb:                bb,
		key:               key,
		op:                op,
		value:             value,
		abort:             abort,
		child:             child,
		childObserver:     nodeHasBBObserver(child),
		childHasNonBBGate: nodeHasNonBBGate(child),
	}
	n.clockState.adopt(child)
	return n
}

type blackboardConditionNode struct {
	bb                *Blackboard
	key               string
	op                BlackboardOp
	value             BlackboardValue
	abort             AbortMode
	child             Node
	childObserver     bool
	childHasNonBBGate bool

	running    bool
	seenRev    bool
	lastRev    uint64
	lastResult bool
	clockState btClockState
	trace      btTraceState
}

func (n *blackboardConditionNode) btClockRef() *btClockState { return &n.clockState }

func (n *blackboardConditionNode) keyRev() uint64 {
	if n.bb == nil {
		return 0
	}
	return n.bb.KeyRevision(n.key)
}

func (n *blackboardConditionNode) eval() bool {
	return evalBlackboardOp(n.bb, n.key, n.op, n.value)
}

func (n *blackboardConditionNode) refresh() bool {
	rev := n.keyRev()
	if !n.seenRev || rev != n.lastRev {
		n.lastRev = rev
		n.seenRev = true
		n.lastResult = n.eval()
	}
	return n.lastResult
}

func (n *blackboardConditionNode) Tick(dt time.Duration) Status {
	n.clockState.advanceIfRoot(dt)
	if n.running && n.child != nil {
		if n.abort.abortsSelf() {
			if !n.refresh() {
				n.child.Reset()
				n.running = false
				return n.trace.record(StatusFailure)
			}
		}
		st := n.child.Tick(dt)
		n.running = st == StatusRunning
		return n.trace.record(st)
	}

	// Entry evaluation (or leaf evaluation when child == nil).
	rev := n.keyRev()
	ok := n.eval()
	n.lastRev = rev
	n.seenRev = true
	n.lastResult = ok
	if !ok {
		return n.trace.record(StatusFailure)
	}
	if n.child == nil {
		return n.trace.record(StatusSuccess)
	}
	st := n.child.Tick(dt)
	n.running = st == StatusRunning
	return n.trace.record(st)
}

func (n *blackboardConditionNode) Reset() {
	n.running = false
	n.seenRev = false
	n.lastRev = 0
	n.lastResult = false
	n.trace.resetTrace()
	if n.child != nil {
		n.child.Reset()
	}
}

func (n *blackboardConditionNode) btKind() string               { return string(BTNodeBlackboardCondition) }
func (n *blackboardConditionNode) btLastStatus() (Status, bool) { return n.trace.status() }
func (n *blackboardConditionNode) btChildCount() int {
	if n.child == nil {
		return 0
	}
	return 1
}
func (n *blackboardConditionNode) btChildAt(int) Node { return n.child }
func (n *blackboardConditionNode) btChildActive(i int) bool {
	return i == 0 && n.running && n.trace.ticked && n.trace.lastStatus == StatusRunning
}

func (n *blackboardConditionNode) hasBBObserver() bool { return !n.childHasNonBBGate }

func (n *blackboardConditionNode) bbWantsPreempt() bool {
	if n.childHasNonBBGate {
		return false
	}
	if !n.abort.abortsLower() {
		if n.seenRev && n.lastResult && n.childObserver && n.eval() {
			return nodeWantsPreempt(n.child)
		}
		return false
	}
	rev := n.keyRev()
	if !n.seenRev || rev != n.lastRev {
		ok := n.eval()
		n.lastRev = rev
		n.seenRev = true
		n.lastResult = ok
		return ok
	}
	if n.lastResult && n.childObserver {
		return nodeWantsPreempt(n.child)
	}
	return false
}

func (n *blackboardConditionNode) bbSelfAborted() bool {
	if !n.abort.abortsSelf() {
		return false
	}
	return !n.refresh()
}

// evalBlackboardOp evaluates op against bb[key] and target.
func evalBlackboardOp(bb *Blackboard, key string, op BlackboardOp, target BlackboardValue) bool {
	var cur BlackboardValue
	var present bool
	if bb != nil {
		cur, present = bb.Get(key)
	}
	switch op {
	case BBOpSet:
		return present && cur.Kind != BlackboardNone
	case BBOpUnset:
		return !present || cur.Kind == BlackboardNone
	case BBOpEq:
		if !present {
			return false
		}
		return bbValuesEqual(cur, target)
	case BBOpNe:
		if !present {
			return true
		}
		return !bbValuesEqual(cur, target)
	case BBOpLt, BBOpLte, BBOpGt, BBOpGte:
		if !present {
			return false
		}
		cmp, ok := bbCompare(cur, target)
		if !ok {
			return false
		}
		switch op {
		case BBOpLt:
			return cmp < 0
		case BBOpLte:
			return cmp <= 0
		case BBOpGt:
			return cmp > 0
		default:
			return cmp >= 0
		}
	default:
		return false
	}
}

func bbValuesEqual(a, b BlackboardValue) bool {
	if a.Kind == b.Kind {
		if a.Kind == BlackboardFloat {
			return a.Float == b.Float
		}
		if a.Kind == BlackboardVec2 {
			return a.Vec2.X == b.Vec2.X && a.Vec2.Y == b.Vec2.Y
		}
		if a.Kind == BlackboardVec3 {
			return a.Vec3.X == b.Vec3.X && a.Vec3.Y == b.Vec3.Y && a.Vec3.Z == b.Vec3.Z
		}
		return a.Equal(b)
	}
	// Allow natural numeric cross-comparisons (int <-> float64, entity <-> non-negative int).
	if cmp, ok := bbCompareNumeric(a, b); ok {
		return cmp == 0
	}
	return false
}

func bbCompare(a, b BlackboardValue) (int, bool) {
	if a.Kind == BlackboardString && b.Kind == BlackboardString {
		switch {
		case a.String < b.String:
			return -1, true
		case a.String > b.String:
			return 1, true
		default:
			return 0, true
		}
	}
	return bbCompareNumeric(a, b)
}

func bbCompareNumeric(a, b BlackboardValue) (int, bool) {
	switch a.Kind {
	case BlackboardInt:
		switch b.Kind {
		case BlackboardInt:
			return cmpInt(a.Int, b.Int), true
		case BlackboardFloat:
			return cmpFloat(float64(a.Int), b.Float)
		case BlackboardEntity:
			if a.Int < 0 {
				return -1, true
			}
			return cmpUint64(uint64(a.Int), b.Entity), true
		}
	case BlackboardFloat:
		switch b.Kind {
		case BlackboardFloat:
			return cmpFloat(a.Float, b.Float)
		case BlackboardInt:
			return cmpFloat(a.Float, float64(b.Int))
		}
	case BlackboardEntity:
		switch b.Kind {
		case BlackboardEntity:
			return cmpUint64(a.Entity, b.Entity), true
		case BlackboardInt:
			if b.Int < 0 {
				return 1, true
			}
			return cmpUint64(a.Entity, uint64(b.Int)), true
		}
	}
	return 0, false
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpUint64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpFloat(a, b float64) (int, bool) {
	if math.IsNaN(a) || math.IsNaN(b) {
		return 0, false
	}
	switch {
	case a < b:
		return -1, true
	case a > b:
		return 1, true
	default:
		return 0, true
	}
}

// Observer forwarding on built-in composites and decorators so BlackboardCondition
// decorators work seamlessly even when nested inside another composite or decorator.

func unwrapNamed(n Node) Node {
	for {
		nn, ok := n.(*namedNode)
		if !ok {
			return n
		}
		n = nn.child
	}
}

func (s *sequenceNode) hasBBObserver() bool { return s.observerOnly }

func (s *sequenceNode) bbWantsPreempt() bool {
	if !s.observerOnly {
		return false
	}
	for _, ch := range s.children {
		if ch == nil {
			return false
		}
		obs, ok := ch.(btObserver)
		if !ok || !obs.hasBBObserver() {
			break
		}
		if obs.bbWantsPreempt() {
			return true
		}
		if bc, ok := unwrapNamed(ch).(*blackboardConditionNode); ok && !bc.eval() {
			return false
		}
	}
	return false
}

func (s *selectorNode) hasBBObserver() bool { return s.allObserver }

func (s *selectorNode) bbWantsPreempt() bool {
	if !s.allObserver {
		return false
	}
	for _, ch := range s.children {
		if nodeWantsPreempt(ch) {
			return true
		}
	}
	return false
}

func (n *inverterNode) hasBBObserver() bool  { return nodeHasBBObserver(n.child) }
func (n *inverterNode) bbWantsPreempt() bool { return nodeWantsPreempt(n.child) }

func (n *succeederNode) hasBBObserver() bool  { return nodeHasBBObserver(n.child) }
func (n *succeederNode) bbWantsPreempt() bool { return nodeWantsPreempt(n.child) }

func (n *failerNode) hasBBObserver() bool  { return nodeHasBBObserver(n.child) }
func (n *failerNode) bbWantsPreempt() bool { return nodeWantsPreempt(n.child) }

func (r *repeaterNode) hasBBObserver() bool  { return nodeHasBBObserver(r.child) }
func (r *repeaterNode) bbWantsPreempt() bool { return nodeWantsPreempt(r.child) }

func (u *untilFailNode) hasBBObserver() bool  { return nodeHasBBObserver(u.child) }
func (u *untilFailNode) bbWantsPreempt() bool { return nodeWantsPreempt(u.child) }

func (c *cooldownNode) hasBBObserver() bool { return nodeHasBBObserver(c.child) }
func (c *cooldownNode) bbWantsPreempt() bool {
	if !c.running && c.remainingDuration() > 0 {
		return false
	}
	return nodeWantsPreempt(c.child)
}

func (t *timeLimitNode) hasBBObserver() bool  { return nodeHasBBObserver(t.child) }
func (t *timeLimitNode) bbWantsPreempt() bool { return nodeWantsPreempt(t.child) }

func (r *retryNode) hasBBObserver() bool  { return nodeHasBBObserver(r.child) }
func (r *retryNode) bbWantsPreempt() bool { return nodeWantsPreempt(r.child) }

func (s *serviceNode) hasBBObserver() bool  { return nodeHasBBObserver(s.child) }
func (s *serviceNode) bbWantsPreempt() bool { return nodeWantsPreempt(s.child) }
