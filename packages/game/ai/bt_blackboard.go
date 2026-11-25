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
	return &blackboardConditionNode{
		bb:            bb,
		key:           key,
		op:            op,
		value:         value,
		abort:         abort,
		child:         child,
		childObserver: nodeHasBBObserver(child),
	}
}

type blackboardConditionNode struct {
	bb            *Blackboard
	key           string
	op            BlackboardOp
	value         BlackboardValue
	abort         AbortMode
	child         Node
	childObserver bool

	running    bool
	seenRev    bool
	lastRev    uint64
	lastResult bool
}

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
	if n.running && n.child != nil {
		if n.abort.abortsSelf() {
			if !n.refresh() {
				n.child.Reset()
				n.running = false
				return StatusFailure
			}
		}
		st := n.child.Tick(dt)
		n.running = st == StatusRunning
		return st
	}

	// Entry evaluation (or leaf evaluation when child == nil).
	rev := n.keyRev()
	ok := n.eval()
	n.lastRev = rev
	n.seenRev = true
	n.lastResult = ok
	if !ok {
		return StatusFailure
	}
	if n.child == nil {
		return StatusSuccess
	}
	st := n.child.Tick(dt)
	n.running = st == StatusRunning
	return st
}

func (n *blackboardConditionNode) Reset() {
	n.running = false
	n.seenRev = false
	n.lastRev = 0
	n.lastResult = false
	if n.child != nil {
		n.child.Reset()
	}
}

func (n *blackboardConditionNode) hasBBObserver() bool { return true }

func (n *blackboardConditionNode) bbWantsPreempt() bool {
	if !n.abort.abortsLower() {
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

func (s *sequenceNode) hasBBObserver() bool { return s.hasObserver }

func (s *sequenceNode) bbWantsPreempt() bool {
	if !s.hasObserver {
		return false
	}
	for _, ch := range s.children {
		if ch == nil {
			return false
		}
		if obs, ok := ch.(btObserver); ok && obs.hasBBObserver() {
			if obs.bbWantsPreempt() {
				return true
			}
			if bc, ok := ch.(*blackboardConditionNode); ok && !bc.eval() {
				return false
			}
		}
	}
	return false
}

func (s *selectorNode) hasBBObserver() bool { return s.hasObserver }

func (s *selectorNode) bbWantsPreempt() bool {
	if !s.hasObserver {
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
	if !c.running && c.remaining > 0 {
		return false
	}
	return nodeWantsPreempt(c.child)
}

func (g *guardNode) hasBBObserver() bool { return nodeHasBBObserver(g.child) }
func (g *guardNode) bbWantsPreempt() bool {
	if g.cond == nil || !g.cond() {
		return false
	}
	return nodeWantsPreempt(g.child)
}

func (t *timeLimitNode) hasBBObserver() bool  { return nodeHasBBObserver(t.child) }
func (t *timeLimitNode) bbWantsPreempt() bool { return nodeWantsPreempt(t.child) }

func (r *retryNode) hasBBObserver() bool  { return nodeHasBBObserver(r.child) }
func (r *retryNode) bbWantsPreempt() bool { return nodeWantsPreempt(r.child) }

func (s *serviceNode) hasBBObserver() bool  { return nodeHasBBObserver(s.child) }
func (s *serviceNode) bbWantsPreempt() bool { return nodeWantsPreempt(s.child) }
