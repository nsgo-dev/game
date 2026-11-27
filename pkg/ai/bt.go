package ai

import (
	"fmt"
	"slices"
	"time"
)

// Status is the outcome of one Tick of a behavior tree Node.
type Status uint8

const (
	// StatusSuccess reports that a node completed its work successfully.
	StatusSuccess Status = iota
	// StatusFailure reports that a node failed or its condition did not hold.
	StatusFailure
	// StatusRunning reports that a node needs more ticks to finish.
	StatusRunning
)

// String returns a human-readable name for s.
func (s Status) String() string {
	switch s {
	case StatusSuccess:
		return "Success"
	case StatusFailure:
		return "Failure"
	case StatusRunning:
		return "Running"
	default:
		return "Unknown"
	}
}

// MarshalText encodes s as its lowercase name ("success", "failure", or "running")
// so snapshots read clearly in YAML and JSON.
func (s Status) MarshalText() ([]byte, error) {
	switch s {
	case StatusSuccess:
		return []byte("success"), nil
	case StatusFailure:
		return []byte("failure"), nil
	case StatusRunning:
		return []byte("running"), nil
	default:
		return nil, fmt.Errorf("ai: unknown status %d", s)
	}
}

// UnmarshalText decodes a status name written by [Status.MarshalText].
func (s *Status) UnmarshalText(text []byte) error {
	switch string(text) {
	case "success":
		*s = StatusSuccess
	case "failure":
		*s = StatusFailure
	case "running":
		*s = StatusRunning
	default:
		return fmt.Errorf("ai: unknown status %q", string(text))
	}
	return nil
}

// Node is one node in a behavior tree.
type Node interface {
	// Tick advances the node by dt and returns its current status.
	Tick(dt time.Duration) Status
	// Reset clears any in-progress state on this node and its descendants.
	Reset()
}

const maxBTDuration = time.Duration(1<<63 - 1)

func addDurationClamped(base, delta time.Duration) time.Duration {
	if delta <= 0 {
		return base
	}
	if delta > maxBTDuration-base {
		return maxBTDuration
	}
	return base + delta
}

// btClock tracks monotonic elapsed game time for a behavior tree so Cooldown
// decorators advance even on ticks where their branch is not visited.
type btClock struct {
	now time.Duration
}

type btClockState struct {
	clock     *btClock
	hasParent bool
}

func (s *btClockState) advanceIfRoot(dt time.Duration) {
	if !s.hasParent && s.clock != nil && dt > 0 {
		s.clock.now = addDurationClamped(s.clock.now, dt)
	}
}

func (s *btClockState) adopt(children ...Node) {
	target := s.clock
	if target == nil {
		for _, ch := range children {
			if clk := findBTClock(ch, nil); clk != nil {
				target = clk
				break
			}
		}
	}
	if target == nil {
		return
	}
	s.clock = target
	var visited map[uintptr]bool
	for _, ch := range children {
		bindBTClock(ch, target, &visited)
	}
}

// btClockOwner is implemented internally by built-in composite, decorator, and
// wrapper nodes so tree construction binds every descendant Cooldown to a single
// root-owned btClock.
type btClockOwner interface {
	btClockRef() *btClockState
}

func findBTClock(n Node, visited map[uintptr]bool) *btClock {
	if isNilNode(n) {
		return nil
	}
	if owner, ok := n.(btClockOwner); ok {
		return owner.btClockRef().clock
	}
	if cn, ok := n.(ChildNodes); ok {
		if ptr, ok := nodePtrKey(n); ok {
			if visited == nil {
				visited = make(map[uintptr]bool)
			}
			if visited[ptr] {
				return nil
			}
			visited[ptr] = true
		}
		for _, ch := range cn.Children() {
			if clk := findBTClock(ch, visited); clk != nil {
				return clk
			}
		}
	}
	return nil
}

func bindBTClock(n Node, target *btClock, visited *map[uintptr]bool) {
	if isNilNode(n) {
		return
	}
	if owner, ok := n.(btClockOwner); ok {
		ref := owner.btClockRef()
		if ref.clock == nil {
			// Built-in subtree contains no Cooldown descendants.
			return
		}
		ref.hasParent = true
		if ref.clock == target {
			// Already bound to target along with its built-in descendants.
			return
		}
		old := ref.clock
		if cd, isCD := n.(*cooldownNode); isCD && cd.cooling {
			rem := cd.readyAt - old.now
			if rem <= 0 {
				cd.cooling = false
				cd.readyAt = 0
			} else {
				cd.readyAt = addDurationClamped(target.now, rem)
			}
		}
		ref.clock = target
		if ptr, ok := nodePtrKey(n); ok {
			if *visited == nil {
				*visited = make(map[uintptr]bool)
			}
			if (*visited)[ptr] {
				return
			}
			(*visited)[ptr] = true
		}
		visitBTChildren(n, func(ch Node) {
			bindBTClock(ch, target, visited)
		})
		return
	}
	if cn, ok := n.(ChildNodes); ok {
		if ptr, ok := nodePtrKey(n); ok {
			if *visited == nil {
				*visited = make(map[uintptr]bool)
			}
			if (*visited)[ptr] {
				return
			}
			(*visited)[ptr] = true
		}
		for _, ch := range cn.Children() {
			bindBTClock(ch, target, visited)
		}
	}
}

func visitBTChildren(n Node, fn func(Node)) {
	switch v := n.(type) {
	case *namedNode:
		if v.child != nil {
			fn(v.child)
		}
	case *BehaviorTree:
		if v.root != nil {
			fn(v.root)
		}
	case btUtilityNode:
		for _, ch := range v.btUtilityChildren() {
			if ch != nil {
				fn(ch)
			}
		}
	case *PlanRunner:
		for _, ch := range v.nodes {
			if ch != nil {
				fn(ch)
			}
		}
	case btInspectable:
		for i := range v.btChildCount() {
			if ch := v.btChildAt(i); ch != nil {
				fn(ch)
			}
		}
	default:
		if cn, ok := n.(ChildNodes); ok {
			for _, ch := range cn.Children() {
				if ch != nil {
					fn(ch)
				}
			}
		}
	}
}

// ResetCooldowns clears every active [Cooldown] timer in the behavior tree rooted
// at root without interrupting running branches.
//
// Unlike [Node.Reset], which preserves active cooldowns across branch interruptions
// such as [Guard], [Selector] preemption, and [TimeLimit], ResetCooldowns performs
// an explicit cooldown reset (for example when respawning an agent).
func ResetCooldowns(root Node) {
	if isNilNode(root) {
		return
	}
	var visited map[uintptr]bool
	resetCooldowns(root, &visited)
}

func resetCooldowns(n Node, visited *map[uintptr]bool) {
	if isNilNode(n) {
		return
	}
	if ptr, ok := nodePtrKey(n); ok {
		if *visited == nil {
			*visited = make(map[uintptr]bool)
		}
		if (*visited)[ptr] {
			return
		}
		(*visited)[ptr] = true
	}
	if cd, ok := n.(*cooldownNode); ok {
		cd.resetCooldown()
	}
	visitBTChildren(n, func(ch Node) {
		resetCooldowns(ch, visited)
	})
}

// ParallelPolicy chooses when a Parallel composite finishes.
type ParallelPolicy uint8

const (
	// RequireAll succeeds when every child succeeds and fails as soon as any child fails.
	RequireAll ParallelPolicy = iota
	// RequireOne succeeds as soon as any child succeeds and fails when every child fails.
	RequireOne
)

// Sequence runs children in order, succeeding when all succeed and failing on the first failure.
//
// When a child returns StatusRunning, Sequence remembers its position and resumes at that child on the next tick.
func Sequence(children ...Node) Node {
	cloned := slices.Clone(children)
	hasSelfAbort := false
	hasNonBBGate := false
	hasAnyBB := false
	prefixObs := 0
	inPrefix := true
	hasTrailingObs := false
	for _, ch := range cloned {
		if nodeHasBBSelfAbort(ch) {
			hasSelfAbort = true
		}
		if nodeHasNonBBGate(ch) {
			hasNonBBGate = true
		}
		if nodeContainsBBObserver(ch) {
			hasAnyBB = true
		}
		if inPrefix {
			if nodeHasBBObserver(ch) {
				prefixObs++
			} else {
				inPrefix = false
			}
		}
		if !inPrefix && nodeContainsBBObserver(ch) {
			hasTrailingObs = true
		}
	}
	n := &sequenceNode{
		children:     cloned,
		observerOnly: prefixObs > 0 && !hasNonBBGate && !hasTrailingObs,
		hasSelfAbort: hasSelfAbort,
		hasNonBBGate: hasNonBBGate,
		hasAnyBB:     hasAnyBB,
	}
	n.clockState.adopt(cloned...)
	return n
}

type sequenceNode struct {
	children     []Node
	cur          int
	observerOnly bool
	hasSelfAbort bool
	hasNonBBGate bool
	hasAnyBB     bool
	clockState   btClockState
	trace        btTraceState
}

func (s *sequenceNode) btClockRef() *btClockState { return &s.clockState }

func (s *sequenceNode) Tick(dt time.Duration) Status {
	s.clockState.advanceIfRoot(dt)
	if s.hasSelfAbort && s.cur > 0 {
		for i := 0; i < s.cur; i++ {
			if sa, ok := s.children[i].(btSelfAbortable); ok && sa.bbSelfAborted() {
				if running := s.children[s.cur]; running != nil {
					running.Reset()
				}
				s.cur = 0
				return s.trace.record(StatusFailure)
			}
		}
	}
	for s.cur < len(s.children) {
		ch := s.children[s.cur]
		if ch == nil {
			s.cur = 0
			return s.trace.record(StatusFailure)
		}
		switch ch.Tick(dt) {
		case StatusRunning:
			return s.trace.record(StatusRunning)
		case StatusFailure:
			s.cur = 0
			return s.trace.record(StatusFailure)
		case StatusSuccess:
			s.cur++
		}
	}
	s.cur = 0
	return s.trace.record(StatusSuccess)
}

func (s *sequenceNode) Reset() {
	s.cur = 0
	s.trace.resetTrace()
	for _, ch := range s.children {
		if ch != nil {
			ch.Reset()
		}
	}
}

func (s *sequenceNode) btKind() string               { return string(BTNodeSequence) }
func (s *sequenceNode) btLastStatus() (Status, bool) { return s.trace.status() }
func (s *sequenceNode) btChildCount() int            { return len(s.children) }
func (s *sequenceNode) btChildAt(i int) Node         { return s.children[i] }
func (s *sequenceNode) btChildActive(i int) bool {
	return s.trace.ticked && s.trace.lastStatus == StatusRunning && i == s.cur
}

// Selector tries children in priority order, returning on the first child that succeeds or runs.
//
// If a higher-priority child preempts a lower-priority child that was previously running, Selector resets the interrupted child.
// Higher-priority children whose gating is governed solely by [BlackboardCondition] observers only preempt a running lower-priority branch when an observer's abort mode includes [AbortLower] or [AbortBoth] and its watched key changes; branches that mix non-blackboard conditions or guards (such as [Condition], [Guard], or [WithAttackToken]) are re-evaluated on every tick.
func Selector(children ...Node) Node {
	cloned := slices.Clone(children)
	hasObs := false
	hasNonNil := false
	allObs := true
	hasNonBBGate := false
	hasAnyBB := false
	for _, ch := range cloned {
		if ch == nil {
			continue
		}
		hasNonNil = true
		if nodeHasBBObserver(ch) {
			hasObs = true
		} else {
			allObs = false
		}
		if nodeHasNonBBGate(ch) {
			hasNonBBGate = true
		}
		if nodeContainsBBObserver(ch) {
			hasAnyBB = true
		}
	}
	n := &selectorNode{
		children:     cloned,
		running:      -1,
		hasObserver:  hasObs,
		allObserver:  hasNonNil && allObs,
		hasNonBBGate: hasNonBBGate,
		hasAnyBB:     hasAnyBB,
	}
	n.clockState.adopt(cloned...)
	return n
}

type selectorNode struct {
	children     []Node
	running      int
	hasObserver  bool
	allObserver  bool
	hasNonBBGate bool
	hasAnyBB     bool
	clockState   btClockState
	trace        btTraceState
}

func (s *selectorNode) btClockRef() *btClockState { return &s.clockState }

func (s *selectorNode) Tick(dt time.Duration) Status {
	s.clockState.advanceIfRoot(dt)
	start := 0
	if s.hasObserver && s.running >= 0 {
		preempted := false
		for i := 0; i < s.running; i++ {
			ch := s.children[i]
			if ch == nil {
				continue
			}
			if obs, ok := ch.(btObserver); ok && obs.hasBBObserver() {
				if !obs.bbWantsPreempt() {
					continue
				}
				if prev := s.children[s.running]; prev != nil {
					prev.Reset()
				}
				s.running = -1
				start = i
				preempted = true
				break
			}
			st := ch.Tick(dt)
			if st == StatusFailure {
				continue
			}
			if prev := s.children[s.running]; prev != nil {
				prev.Reset()
			}
			if st == StatusRunning {
				s.running = i
				return s.trace.record(StatusRunning)
			}
			s.running = -1
			return s.trace.record(StatusSuccess)
		}
		if !preempted {
			start = s.running
		}
	}
	for i := start; i < len(s.children); i++ {
		ch := s.children[i]
		if ch == nil {
			continue
		}
		st := ch.Tick(dt)
		if st == StatusFailure {
			if s.running == i {
				s.running = -1
			}
			continue
		}
		if s.running >= 0 && s.running != i {
			if prev := s.children[s.running]; prev != nil {
				prev.Reset()
			}
		}
		if st == StatusRunning {
			s.running = i
			return s.trace.record(StatusRunning)
		}
		s.running = -1
		return s.trace.record(StatusSuccess)
	}
	s.running = -1
	return s.trace.record(StatusFailure)
}

func (s *selectorNode) Reset() {
	s.running = -1
	s.trace.resetTrace()
	for _, ch := range s.children {
		if ch != nil {
			ch.Reset()
		}
	}
}

func (s *selectorNode) btKind() string               { return string(BTNodeSelector) }
func (s *selectorNode) btLastStatus() (Status, bool) { return s.trace.status() }
func (s *selectorNode) btChildCount() int            { return len(s.children) }
func (s *selectorNode) btChildAt(i int) Node         { return s.children[i] }
func (s *selectorNode) btChildActive(i int) bool {
	return s.trace.ticked && s.trace.lastStatus == StatusRunning && i == s.running
}

// Parallel ticks all still-running children each tick and finishes according to policy, resetting any remaining running children on completion.
func Parallel(policy ParallelPolicy, children ...Node) Node {
	cloned := slices.Clone(children)
	n := &parallelNode{
		policy:   policy,
		children: cloned,
		status:   make([]Status, len(cloned)),
	}
	n.clockState.adopt(cloned...)
	return n
}

type parallelNode struct {
	policy     ParallelPolicy
	children   []Node
	status     []Status
	active     bool
	clockState btClockState
	trace      btTraceState
}

func (p *parallelNode) btClockRef() *btClockState { return &p.clockState }

func (p *parallelNode) Tick(dt time.Duration) Status {
	p.clockState.advanceIfRoot(dt)
	if len(p.children) == 0 {
		if p.policy == RequireOne {
			return p.trace.record(StatusFailure)
		}
		return p.trace.record(StatusSuccess)
	}
	if !p.active {
		p.active = true
		for i := range p.status {
			p.status[i] = StatusRunning
		}
	}

	successes, failures := 0, 0
	for i, ch := range p.children {
		if p.status[i] == StatusRunning {
			if ch == nil {
				p.status[i] = StatusFailure
			} else {
				p.status[i] = ch.Tick(dt)
			}
		}
		switch p.status[i] {
		case StatusSuccess:
			successes++
		case StatusFailure:
			failures++
		}
	}

	switch p.policy {
	case RequireOne:
		if successes > 0 {
			p.resetRunning()
			p.active = false
			return p.trace.record(StatusSuccess)
		}
		if failures == len(p.children) {
			p.active = false
			return p.trace.record(StatusFailure)
		}
	default:
		if failures > 0 {
			p.resetRunning()
			p.active = false
			return p.trace.record(StatusFailure)
		}
		if successes == len(p.children) {
			p.active = false
			return p.trace.record(StatusSuccess)
		}
	}
	return p.trace.record(StatusRunning)
}

func (p *parallelNode) resetRunning() {
	for i, ch := range p.children {
		if p.status[i] == StatusRunning && ch != nil {
			ch.Reset()
		}
	}
}

func (p *parallelNode) Reset() {
	p.active = false
	p.trace.resetTrace()
	for _, ch := range p.children {
		if ch != nil {
			ch.Reset()
		}
	}
}

func (p *parallelNode) btKind() string               { return string(BTNodeParallel) }
func (p *parallelNode) btLastStatus() (Status, bool) { return p.trace.status() }
func (p *parallelNode) btChildCount() int            { return len(p.children) }
func (p *parallelNode) btChildAt(i int) Node         { return p.children[i] }
func (p *parallelNode) btChildActive(i int) bool {
	return p.trace.ticked && p.trace.lastStatus == StatusRunning && p.active && i >= 0 && i < len(p.status) && p.status[i] == StatusRunning
}

// Inverter flips StatusSuccess and StatusFailure from child while passing StatusRunning through unchanged.
func Inverter(child Node) Node {
	n := &inverterNode{child: child}
	n.clockState.adopt(child)
	return n
}

type inverterNode struct {
	child      Node
	clockState btClockState
	trace      btTraceState
}

func (n *inverterNode) btClockRef() *btClockState { return &n.clockState }

func (n *inverterNode) Tick(dt time.Duration) Status {
	n.clockState.advanceIfRoot(dt)
	if n.child == nil {
		return n.trace.record(StatusFailure)
	}
	switch n.child.Tick(dt) {
	case StatusSuccess:
		return n.trace.record(StatusFailure)
	case StatusFailure:
		return n.trace.record(StatusSuccess)
	default:
		return n.trace.record(StatusRunning)
	}
}

func (n *inverterNode) Reset() {
	n.trace.resetTrace()
	if n.child != nil {
		n.child.Reset()
	}
}

func (n *inverterNode) btKind() string               { return string(BTNodeInverter) }
func (n *inverterNode) btLastStatus() (Status, bool) { return n.trace.status() }
func (n *inverterNode) btChildCount() int {
	if n.child == nil {
		return 0
	}
	return 1
}
func (n *inverterNode) btChildAt(int) Node { return n.child }
func (n *inverterNode) btChildActive(i int) bool {
	return i == 0 && n.trace.ticked && n.trace.lastStatus == StatusRunning
}

// Succeeder turns both StatusSuccess and StatusFailure from child into StatusSuccess while passing StatusRunning through.
func Succeeder(child Node) Node {
	n := &succeederNode{child: child}
	n.clockState.adopt(child)
	return n
}

type succeederNode struct {
	child      Node
	clockState btClockState
	trace      btTraceState
}

func (n *succeederNode) btClockRef() *btClockState { return &n.clockState }

func (n *succeederNode) Tick(dt time.Duration) Status {
	n.clockState.advanceIfRoot(dt)
	if n.child == nil {
		return n.trace.record(StatusSuccess)
	}
	if n.child.Tick(dt) == StatusRunning {
		return n.trace.record(StatusRunning)
	}
	return n.trace.record(StatusSuccess)
}

func (n *succeederNode) Reset() {
	n.trace.resetTrace()
	if n.child != nil {
		n.child.Reset()
	}
}

func (n *succeederNode) btKind() string               { return string(BTNodeSucceeder) }
func (n *succeederNode) btLastStatus() (Status, bool) { return n.trace.status() }
func (n *succeederNode) btChildCount() int {
	if n.child == nil {
		return 0
	}
	return 1
}
func (n *succeederNode) btChildAt(int) Node { return n.child }
func (n *succeederNode) btChildActive(i int) bool {
	return i == 0 && n.trace.ticked && n.trace.lastStatus == StatusRunning
}

// Failer turns both StatusSuccess and StatusFailure from child into StatusFailure while passing StatusRunning through.
func Failer(child Node) Node {
	n := &failerNode{child: child}
	n.clockState.adopt(child)
	return n
}

type failerNode struct {
	child      Node
	clockState btClockState
	trace      btTraceState
}

func (n *failerNode) btClockRef() *btClockState { return &n.clockState }

func (n *failerNode) Tick(dt time.Duration) Status {
	n.clockState.advanceIfRoot(dt)
	if n.child == nil {
		return n.trace.record(StatusFailure)
	}
	if n.child.Tick(dt) == StatusRunning {
		return n.trace.record(StatusRunning)
	}
	return n.trace.record(StatusFailure)
}

func (n *failerNode) Reset() {
	n.trace.resetTrace()
	if n.child != nil {
		n.child.Reset()
	}
}

func (n *failerNode) btKind() string               { return string(BTNodeFailer) }
func (n *failerNode) btLastStatus() (Status, bool) { return n.trace.status() }
func (n *failerNode) btChildCount() int {
	if n.child == nil {
		return 0
	}
	return 1
}
func (n *failerNode) btChildAt(int) Node { return n.child }
func (n *failerNode) btChildActive(i int) bool {
	return i == 0 && n.trace.ticked && n.trace.lastStatus == StatusRunning
}

// Repeater runs child until it succeeds count times, returning StatusRunning between iterations and StatusFailure immediately if child fails.
//
// When count is less than or equal to 0, Repeater repeats indefinitely until child fails.
func Repeater(count int, child Node) Node {
	n := &repeaterNode{count: count, child: child}
	n.clockState.adopt(child)
	return n
}

type repeaterNode struct {
	count      int
	done       int
	child      Node
	clockState btClockState
	trace      btTraceState
}

func (r *repeaterNode) btClockRef() *btClockState { return &r.clockState }

func (r *repeaterNode) Tick(dt time.Duration) Status {
	r.clockState.advanceIfRoot(dt)
	if r.child == nil {
		return r.trace.record(StatusFailure)
	}
	switch r.child.Tick(dt) {
	case StatusRunning:
		return r.trace.record(StatusRunning)
	case StatusFailure:
		r.done = 0
		return r.trace.record(StatusFailure)
	default:
		if r.count > 0 {
			r.done++
			if r.done >= r.count {
				r.done = 0
				return r.trace.record(StatusSuccess)
			}
		}
		return r.trace.record(StatusRunning)
	}
}

func (r *repeaterNode) Reset() {
	r.done = 0
	r.trace.resetTrace()
	if r.child != nil {
		r.child.Reset()
	}
}

func (r *repeaterNode) btKind() string               { return string(BTNodeRepeater) }
func (r *repeaterNode) btLastStatus() (Status, bool) { return r.trace.status() }
func (r *repeaterNode) btChildCount() int {
	if r.child == nil {
		return 0
	}
	return 1
}
func (r *repeaterNode) btChildAt(int) Node { return r.child }
func (r *repeaterNode) btChildActive(i int) bool {
	return i == 0 && r.trace.ticked && r.trace.lastStatus == StatusRunning
}

// UntilFail ticks child once per tick, returning StatusRunning until child returns StatusFailure, at which point UntilFail returns StatusSuccess.
func UntilFail(child Node) Node {
	n := &untilFailNode{child: child}
	n.clockState.adopt(child)
	return n
}

type untilFailNode struct {
	child      Node
	clockState btClockState
	trace      btTraceState
}

func (u *untilFailNode) btClockRef() *btClockState { return &u.clockState }

func (u *untilFailNode) Tick(dt time.Duration) Status {
	u.clockState.advanceIfRoot(dt)
	if u.child == nil {
		return u.trace.record(StatusSuccess)
	}
	if u.child.Tick(dt) == StatusFailure {
		return u.trace.record(StatusSuccess)
	}
	return u.trace.record(StatusRunning)
}

func (u *untilFailNode) Reset() {
	u.trace.resetTrace()
	if u.child != nil {
		u.child.Reset()
	}
}

func (u *untilFailNode) btKind() string               { return string(BTNodeUntilFail) }
func (u *untilFailNode) btLastStatus() (Status, bool) { return u.trace.status() }
func (u *untilFailNode) btChildCount() int {
	if u.child == nil {
		return 0
	}
	return 1
}
func (u *untilFailNode) btChildAt(int) Node { return u.child }
func (u *untilFailNode) btChildActive(i int) bool {
	return i == 0 && u.trace.ticked && u.trace.lastStatus == StatusRunning
}

// Cooldown prevents child from starting again for duration d after it succeeds, returning StatusFailure while cooling down.
//
// Cooldown measures elapsed time against the enclosing tree's monotonic clock, so
// cooldowns continue counting down even on ticks when this branch is not visited.
// Calling [Node.Reset] cancels an in-progress child run without clearing an active
// cooldown; use [ResetCooldowns] or [BehaviorTree.ResetCooldowns] to clear active
// cooldowns explicitly (for example when respawning an agent).
func Cooldown(d time.Duration, child Node) Node {
	n := &cooldownNode{
		duration:   d,
		child:      child,
		clockState: btClockState{clock: &btClock{}},
	}
	n.clockState.adopt(child)
	return n
}

type cooldownNode struct {
	duration   time.Duration
	readyAt    time.Duration
	cooling    bool
	running    bool
	child      Node
	clockState btClockState
	trace      btTraceState
}

func (c *cooldownNode) btClockRef() *btClockState { return &c.clockState }

func (c *cooldownNode) clockNow() time.Duration {
	if c.clockState.clock == nil {
		return 0
	}
	return c.clockState.clock.now
}

func (c *cooldownNode) remainingDuration() time.Duration {
	if !c.cooling || c.clockState.clock == nil {
		return 0
	}
	rem := c.readyAt - c.clockState.clock.now
	if rem <= 0 {
		return 0
	}
	return rem
}

func (c *cooldownNode) resetCooldown() {
	c.cooling = false
	c.readyAt = 0
}

func (c *cooldownNode) Tick(dt time.Duration) Status {
	c.clockState.advanceIfRoot(dt)
	if c.child == nil {
		return c.trace.record(StatusFailure)
	}
	if !c.running && c.cooling {
		if c.remainingDuration() > 0 {
			return c.trace.record(StatusFailure)
		}
		c.cooling = false
		c.readyAt = 0
	}
	st := c.child.Tick(dt)
	switch st {
	case StatusRunning:
		c.running = true
		return c.trace.record(StatusRunning)
	case StatusSuccess:
		c.running = false
		if c.duration > 0 {
			c.cooling = true
			c.readyAt = addDurationClamped(c.clockNow(), c.duration)
		}
		return c.trace.record(StatusSuccess)
	default:
		c.running = false
		return c.trace.record(StatusFailure)
	}
}

func (c *cooldownNode) Reset() {
	c.running = false
	c.trace.resetTrace()
	if c.child != nil {
		c.child.Reset()
	}
}

func (c *cooldownNode) btKind() string               { return string(BTNodeCooldown) }
func (c *cooldownNode) btLastStatus() (Status, bool) { return c.trace.status() }
func (c *cooldownNode) btChildCount() int {
	if c.child == nil {
		return 0
	}
	return 1
}
func (c *cooldownNode) btChildAt(int) Node { return c.child }
func (c *cooldownNode) btChildActive(i int) bool {
	return i == 0 && c.running && c.trace.ticked && c.trace.lastStatus == StatusRunning
}

// Guard ticks child only while cond returns true; if cond becomes false while child is running, Guard resets child and returns StatusFailure.
func Guard(cond func() bool, child Node) Node {
	n := &guardNode{cond: cond, child: child}
	n.clockState.adopt(child)
	return n
}

type guardNode struct {
	cond       func() bool
	child      Node
	running    bool
	clockState btClockState
	trace      btTraceState
}

func (g *guardNode) btClockRef() *btClockState { return &g.clockState }

func (g *guardNode) Tick(dt time.Duration) Status {
	g.clockState.advanceIfRoot(dt)
	if g.cond == nil || !g.cond() {
		if g.running && g.child != nil {
			g.child.Reset()
		}
		g.running = false
		return g.trace.record(StatusFailure)
	}
	if g.child == nil {
		return g.trace.record(StatusFailure)
	}
	st := g.child.Tick(dt)
	g.running = st == StatusRunning
	return g.trace.record(st)
}

func (g *guardNode) Reset() {
	g.running = false
	g.trace.resetTrace()
	if g.child != nil {
		g.child.Reset()
	}
}

func (g *guardNode) btKind() string               { return string(BTNodeGuard) }
func (g *guardNode) btLastStatus() (Status, bool) { return g.trace.status() }
func (g *guardNode) btChildCount() int {
	if g.child == nil {
		return 0
	}
	return 1
}
func (g *guardNode) btChildAt(int) Node { return g.child }
func (g *guardNode) btChildActive(i int) bool {
	return i == 0 && g.running && g.trace.ticked && g.trace.lastStatus == StatusRunning
}

// Action wraps fn as a leaf node.
func Action(fn func(dt time.Duration) Status) Node { return &actionNode{fn: fn} }

type actionNode struct {
	fn    func(dt time.Duration) Status
	trace btTraceState
}

func (a *actionNode) Tick(dt time.Duration) Status {
	if a.fn == nil {
		return a.trace.record(StatusFailure)
	}
	return a.trace.record(a.fn(dt))
}

func (a *actionNode) Reset() { a.trace.resetTrace() }

func (a *actionNode) btKind() string               { return string(BTNodeAction) }
func (a *actionNode) btLastStatus() (Status, bool) { return a.trace.status() }
func (a *actionNode) btChildCount() int            { return 0 }
func (a *actionNode) btChildAt(int) Node           { return nil }
func (a *actionNode) btChildActive(int) bool       { return false }

// Condition wraps predicate fn as a leaf node returning StatusSuccess when true and StatusFailure when false.
func Condition(fn func() bool) Node { return &conditionNode{fn: fn} }

type conditionNode struct {
	fn    func() bool
	trace btTraceState
}

func (c *conditionNode) Tick(time.Duration) Status {
	if c.fn != nil && c.fn() {
		return c.trace.record(StatusSuccess)
	}
	return c.trace.record(StatusFailure)
}

func (c *conditionNode) Reset() { c.trace.resetTrace() }

func (c *conditionNode) btKind() string               { return string(BTNodeCondition) }
func (c *conditionNode) btLastStatus() (Status, bool) { return c.trace.status() }
func (c *conditionNode) btChildCount() int            { return 0 }
func (c *conditionNode) btChildAt(int) Node           { return nil }
func (c *conditionNode) btChildActive(int) bool       { return false }

// Wait returns StatusRunning until the accumulated dt reaches d, then resets and returns StatusSuccess.
func Wait(d time.Duration) Node { return &waitNode{duration: d} }

type waitNode struct {
	duration time.Duration
	elapsed  time.Duration
	trace    btTraceState
}

func (w *waitNode) Tick(dt time.Duration) Status {
	if w.duration <= 0 {
		return w.trace.record(StatusSuccess)
	}
	if dt > 0 {
		w.elapsed += dt
	}
	if w.elapsed >= w.duration {
		w.elapsed = 0
		return w.trace.record(StatusSuccess)
	}
	return w.trace.record(StatusRunning)
}

func (w *waitNode) Reset() {
	w.elapsed = 0
	w.trace.resetTrace()
}

func (w *waitNode) btKind() string               { return string(BTNodeWait) }
func (w *waitNode) btLastStatus() (Status, bool) { return w.trace.status() }
func (w *waitNode) btChildCount() int            { return 0 }
func (w *waitNode) btChildAt(int) Node           { return nil }
func (w *waitNode) btChildActive(int) bool       { return false }
