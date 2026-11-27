package flow

import (
	"reflect"
	"slices"
	"time"
)

type hsmTransitionKind uint8

const (
	hsmTransitionPolled hsmTransitionKind = iota
	hsmTransitionEvent
)

type hsmTransition[S comparable] struct {
	kind  hsmTransitionKind
	any   bool
	from  S
	to    S
	when  func() bool
	match func(event any) bool
}

type hsmFrame[S comparable] struct {
	m  *HSM[S]
	id S
}

// HSM is a hierarchical finite state machine keyed by states of type S and stepped by Update on a game.Loop.
//
// Any state may own a nested child *HSM[S] attached via AddChild, with its own initial state and optional shallow history (SetHistory).
// Enter callbacks run outside-in (parent before child), Exit callbacks run inside-out (child before parent), and transitions between branches exit up to the lowest common ancestor before entering down to the target leaf.
// Transitions declared on a parent apply while any descendant is active, and the innermost active state is checked first.
//
// Event-triggered transitions (AddEventTransition, AddAnyEventTransition, OnEvent, OnAnyEvent) work alongside polled conditions (AddTransition, AddAnyTransition). Events sent through Send or forwarded from a Bus via BindBus are queued in FIFO send order and processed once per Update step.
type HSM[S comparable] struct {
	initial     S
	hasInitial  bool
	states      map[S]State
	known       map[S]bool
	stateOrder  []S
	children    map[S]*HSM[S]
	transitions []hsmTransition[S]

	parent      *HSM[S]
	parentState S

	history    bool
	hasHistory bool
	lastChild  S

	// Runtime state owned per machine level.
	current     S
	previous    S
	timeInState time.Duration
	active      bool

	// Root-owned runtime state.
	started       bool
	activePath    []hsmFrame[S]
	events        []any
	eventScratch  []any
	gen           uint64
	transitioning bool
	deferredSet   []hsmFrame[S]
}

// NewHSM returns a hierarchical state machine whose initial state is initial.
// Call Start to run initial Enter callbacks before the first Update, or let the first Update start it automatically.
func NewHSM[S comparable](initial S) *HSM[S] {
	m := &HSM[S]{
		initial:    initial,
		hasInitial: true,
		current:    initial,
	}
	m.markKnown(initial)
	return m
}

func (m *HSM[S]) markKnown(id S) {
	if m.known == nil {
		m.known = make(map[S]bool)
	}
	if !m.known[id] {
		m.known[id] = true
		m.stateOrder = append(m.stateOrder, id)
	}
}

func (m *HSM[S]) hasState(id S) bool {
	return m.known != nil && m.known[id]
}

func (m *HSM[S]) root() *HSM[S] {
	r := m
	for r.parent != nil {
		r = r.parent
	}
	return r
}

// AddState registers the Enter, Update and Exit callbacks for id, replacing any previously registered callbacks for id while preserving any attached child machine.
func (m *HSM[S]) AddState(id S, state State) {
	if m.states == nil {
		m.states = make(map[S]State)
	}
	m.markKnown(id)
	m.states[id] = state
}

// AddChild attaches child as the nested state machine owned by state parent in m.
// Passing nil removes any child attached to parent.
// AddChild panics if attaching child would create a cycle or if child is already attached to a different parent machine.
func (m *HSM[S]) AddChild(parent S, child *HSM[S]) {
	m.markKnown(parent)
	if child == nil {
		if old, ok := m.children[parent]; ok {
			old.parent = nil
			var zero S
			old.parentState = zero
			delete(m.children, parent)
		}
		return
	}
	for anc := m; anc != nil; anc = anc.parent {
		if anc == child {
			panic("flow: HSM child cycle")
		}
	}
	if child.parent != nil && (child.parent != m || child.parentState != parent) {
		panic("flow: HSM child already attached to another parent")
	}
	if m.children == nil {
		m.children = make(map[S]*HSM[S])
	}
	if old, ok := m.children[parent]; ok && old != child {
		old.parent = nil
		var zero S
		old.parentState = zero
	}
	child.parent = m
	child.parentState = parent
	m.children[parent] = child

	// Forward any events queued on child before attachment onto the root queue.
	if len(child.events) > 0 {
		r := m.root()
		r.events = append(r.events, child.events...)
		clear(child.events)
		child.events = child.events[:0]
	}
}

// SetHistory enables or disables shallow history on m.
// When enabled, re-entering m as a child machine resumes its last active direct child state instead of its initial state.
func (m *HSM[S]) SetHistory(enabled bool) {
	m.history = enabled
}

// History reports whether shallow history is enabled on m.
func (m *HSM[S]) History() bool {
	return m.history
}

// ClearHistory forgets any recorded shallow history on m and all of its nested child machines.
func (m *HSM[S]) ClearHistory() {
	m.hasHistory = false
	var zero S
	m.lastChild = zero
	for _, id := range m.stateOrder {
		if ch := m.children[id]; ch != nil {
			ch.ClearHistory()
		}
	}
}

// AddTransition registers a polled conditional transition from from to to.
// Transitions are evaluated in declaration order during Update, with innermost active states checked first.
func (m *HSM[S]) AddTransition(from, to S, when func() bool) {
	m.transitions = append(m.transitions, hsmTransition[S]{
		kind: hsmTransitionPolled,
		from: from,
		to:   to,
		when: when,
	})
}

// AddAnyTransition registers a polled wildcard transition to to from any active state within m.
// Transitions are evaluated in declaration order during Update, and a wildcard transition does not re-enter to when it is already active.
func (m *HSM[S]) AddAnyTransition(to S, when func() bool) {
	m.transitions = append(m.transitions, hsmTransition[S]{
		kind: hsmTransitionPolled,
		any:  true,
		to:   to,
		when: when,
	})
}

// AddEventTransition registers an event-triggered transition from from to to that fires when an event equal to event is processed and all non-nil guards return true.
// A nil event is ignored.
func (m *HSM[S]) AddEventTransition(from, to S, event any, guard ...func() bool) {
	if event == nil {
		return
	}
	guards := compactGuards(guard)
	m.transitions = append(m.transitions, hsmTransition[S]{
		kind: hsmTransitionEvent,
		from: from,
		to:   to,
		match: func(ev any) bool {
			if !hsmEventsEqual(ev, event) {
				return false
			}
			for _, g := range guards {
				if !g() {
					return false
				}
			}
			return true
		},
	})
}

// AddAnyEventTransition registers an event-triggered wildcard transition to to from any active state within m (when to is not already active).
// It fires when an event equal to event is processed and all non-nil guards return true. A nil event is ignored.
func (m *HSM[S]) AddAnyEventTransition(to S, event any, guard ...func() bool) {
	if event == nil {
		return
	}
	guards := compactGuards(guard)
	m.transitions = append(m.transitions, hsmTransition[S]{
		kind: hsmTransitionEvent,
		any:  true,
		to:   to,
		match: func(ev any) bool {
			if !hsmEventsEqual(ev, event) {
				return false
			}
			for _, g := range guards {
				if !g() {
					return false
				}
			}
			return true
		},
	})
}

// OnEvent registers a typed event-triggered transition on m from from to to.
// It matches any queued event of type E (including events forwarded from a Bus via BindBus) when guard is nil or guard(ev) returns true.
func OnEvent[E any, S comparable](m *HSM[S], from, to S, guard func(E) bool) {
	if m == nil {
		return
	}
	m.transitions = append(m.transitions, hsmTransition[S]{
		kind: hsmTransitionEvent,
		from: from,
		to:   to,
		match: func(ev any) bool {
			typed, ok := ev.(E)
			if !ok {
				return false
			}
			return guard == nil || guard(typed)
		},
	})
}

// OnAnyEvent registers a typed wildcard event-triggered transition on m to to from any active state within m (when to is not already active).
// It matches any queued event of type E when guard is nil or guard(ev) returns true.
func OnAnyEvent[E any, S comparable](m *HSM[S], to S, guard func(E) bool) {
	if m == nil {
		return
	}
	m.transitions = append(m.transitions, hsmTransition[S]{
		kind: hsmTransitionEvent,
		any:  true,
		to:   to,
		match: func(ev any) bool {
			typed, ok := ev.(E)
			if !ok {
				return false
			}
			return guard == nil || guard(typed)
		},
	})
}

// BindBus subscribes m to events of type E on b, forwarding each delivered event to m.Send in delivery order so it is processed on the next Update step.
// It returns an idempotent unsubscribe function.
func BindBus[E any, S comparable](m *HSM[S], b *Bus) (unsubscribe func()) {
	if m == nil || b == nil {
		return func() {}
	}
	return Subscribe(b, func(ev E) {
		m.Send(ev)
	})
}

func compactGuards(guards []func() bool) []func() bool {
	var out []func() bool
	for _, g := range guards {
		if g != nil {
			out = append(out, g)
		}
	}
	return out
}

func hsmEventsEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	ta := reflect.TypeOf(a)
	if ta != reflect.TypeOf(b) {
		return false
	}
	if !ta.Comparable() {
		return false
	}
	return a == b
}

// Send queues event onto the root machine in FIFO send order to be processed on the next Update step.
// Events sent while Update is processing transitions or callbacks are deferred to the following step. A nil event is ignored.
func (m *HSM[S]) Send(event any) {
	if m == nil || event == nil {
		return
	}
	r := m.root()
	r.events = append(r.events, event)
}

// Start enters the initial state path from root to leaf if the machine has not yet started. Subsequent calls are no-ops.
func (m *HSM[S]) Start() {
	r := m.root()
	if r.started {
		return
	}
	r.started = true
	r.gen++
	r.activePath = r.activePath[:0]
	r.transitioning = true
	r.enterFrom(r, r.initial, false)
	r.transitioning = false
	r.drainDeferredSet()
}

// SetState immediately switches to to, exiting up to the lowest common ancestor (inside-out) and entering down to the target leaf (outside-in).
// Switching to the already-active leaf state after Start is a no-op.
func (m *HSM[S]) SetState(to S) {
	r := m.root()
	target := r.resolveTarget(m, to)

	if !r.started {
		r.started = true
		r.gen++
		r.activePath = r.activePath[:0]
		prefix := targetPrefix(target)
		if len(prefix) > 0 && prefix[0].id != r.initial {
			r.previous = r.initial
		}
		r.transitioning = true
		r.enterPrefixAndDescend(prefix)
		r.transitioning = false
		r.drainDeferredSet()
		return
	}

	if r.transitioning {
		r.deferredSet = append(r.deferredSet, target)
		return
	}

	if len(r.activePath) > 0 {
		leaf := r.activePath[len(r.activePath)-1]
		if leaf == target {
			return
		}
	}
	r.transitionToTarget(target)
}

func (m *HSM[S]) drainDeferredSet() {
	for len(m.deferredSet) > 0 {
		target := m.deferredSet[0]
		m.deferredSet = m.deferredSet[1:]
		if len(m.activePath) > 0 && m.activePath[len(m.activePath)-1] == target {
			continue
		}
		m.transitionToTarget(target)
	}
}

// Update advances the root machine by dt:
//  1. Starts the machine if needed.
//  2. Processes queued events (sent before this Update via Send or BindBus) once in FIFO send order, checking active states from innermost to outermost.
//  3. If no event triggered a transition, evaluates polled transitions from innermost to outermost in declaration order, taking at most one.
//  4. Advances TimeInState for every active state (when dt > 0) and runs each active state's Update callback from outside in.
//
// Calling Update on an attached child machine is a no-op; drive the hierarchy from the root machine.
func (m *HSM[S]) Update(dt time.Duration) {
	if m.parent != nil {
		return
	}
	if !m.started {
		m.Start()
	}

	transitioned := false
	if len(m.events) > 0 {
		batch := append(m.eventScratch[:0], m.events...)
		clear(m.events)
		m.events = m.events[:0]
		for _, ev := range batch {
			if m.processEvent(ev) {
				transitioned = true
			}
		}
		clear(batch)
		m.eventScratch = batch[:0]
	}

	if !transitioned {
		m.processPolled()
	}

	if dt > 0 {
		for _, f := range m.activePath {
			f.m.timeInState += dt
		}
	}

	startGen := m.gen
	n := len(m.activePath)
	for i := 0; i < n; i++ {
		if m.gen != startGen {
			break
		}
		f := m.activePath[i]
		if s, ok := f.m.states[f.id]; ok && s.Update != nil {
			s.Update(dt)
		}
	}
}

func (m *HSM[S]) processEvent(ev any) bool {
	for level := len(m.activePath) - 1; level >= 0; level-- {
		f := m.activePath[level]
		if target, ok := m.matchTransitions(level, f, hsmTransitionEvent, ev); ok {
			m.transitionToTarget(target)
			return true
		}
	}
	return false
}

func (m *HSM[S]) processPolled() bool {
	for level := len(m.activePath) - 1; level >= 0; level-- {
		f := m.activePath[level]
		if target, ok := m.matchTransitions(level, f, hsmTransitionPolled, nil); ok {
			m.transitionToTarget(target)
			return true
		}
	}
	return false
}

func (m *HSM[S]) matchTransitions(level int, active hsmFrame[S], kind hsmTransitionKind, ev any) (hsmFrame[S], bool) {
	// Check transitions declared directly on active.m in declaration order.
	if target, ok := m.matchOnMachine(active.m, active.id, true, kind, ev); ok {
		return target, true
	}
	// Also allow non-wildcard transitions declared on ancestors for a descendant state ID not owned directly by that ancestor.
	for anc := level - 1; anc >= 0; anc-- {
		am := m.activePath[anc].m
		if am.hasState(active.id) {
			continue
		}
		if target, ok := m.matchOnMachine(am, active.id, false, kind, ev); ok {
			return target, true
		}
	}
	return hsmFrame[S]{}, false
}

func (m *HSM[S]) matchOnMachine(src *HSM[S], stateID S, allowAny bool, kind hsmTransitionKind, ev any) (hsmFrame[S], bool) {
	for _, tr := range src.transitions {
		if tr.kind != kind {
			continue
		}
		if tr.any {
			if !allowAny {
				continue
			}
			target := m.resolveTarget(src, tr.to)
			if m.isFrameActive(target) {
				continue
			}
			if kind == hsmTransitionPolled {
				if tr.when != nil && tr.when() {
					return target, true
				}
			} else if tr.match != nil && tr.match(ev) {
				return target, true
			}
			continue
		}
		if tr.from != stateID {
			continue
		}
		if kind == hsmTransitionPolled {
			if tr.when != nil && tr.when() {
				return m.resolveTarget(src, tr.to), true
			}
		} else if tr.match != nil && tr.match(ev) {
			return m.resolveTarget(src, tr.to), true
		}
	}
	return hsmFrame[S]{}, false
}

func (m *HSM[S]) isFrameActive(f hsmFrame[S]) bool {
	return slices.Contains(m.activePath, f)
}

func (m *HSM[S]) resolveTarget(src *HSM[S], to S) hsmFrame[S] {
	if src.hasState(to) {
		return hsmFrame[S]{m: src, id: to}
	}
	if found, ok := m.findState(to); ok {
		return found
	}
	return hsmFrame[S]{m: src, id: to}
}

func (m *HSM[S]) findState(id S) (hsmFrame[S], bool) {
	if m.hasState(id) {
		return hsmFrame[S]{m: m, id: id}, true
	}
	for _, stateID := range m.stateOrder {
		if ch := m.children[stateID]; ch != nil {
			if found, ok := ch.findState(id); ok {
				return found, true
			}
		}
	}
	return hsmFrame[S]{}, false
}

func targetPrefix[S comparable](target hsmFrame[S]) []hsmFrame[S] {
	prefix := []hsmFrame[S]{target}
	cur := target.m
	for cur.parent != nil {
		prefix = append(prefix, hsmFrame[S]{m: cur.parent, id: cur.parentState})
		cur = cur.parent
	}
	slices.Reverse(prefix)
	return prefix
}

func (m *HSM[S]) transitionToTarget(target hsmFrame[S]) {
	prefix := targetPrefix(target)

	lca := 0
	for lca < len(m.activePath) && lca < len(prefix) && m.activePath[lca] == prefix[lca] {
		lca++
	}
	// If the target is already on the active path (explicit self-transition or child-to-ancestor transition),
	// exit down to and including the target state so it cleanly re-enters.
	if lca == len(prefix) && lca > 0 {
		lca--
	}

	m.gen++
	m.transitioning = true

	// Exit active states from leaf up to lca (inside-out).
	for i := len(m.activePath) - 1; i >= lca; i-- {
		f := m.activePath[i]
		if s, ok := f.m.states[f.id]; ok && s.Exit != nil {
			s.Exit()
		}
		f.m.lastChild = f.id
		f.m.hasHistory = true
		f.m.previous = f.id
		f.m.active = false
		m.activePath = m.activePath[:i]
	}

	// Enter target prefix from lca down to target (outside-in), then descend into any child machine.
	for i := lca; i < len(prefix); i++ {
		f := prefix[i]
		f.m.current = f.id
		f.m.timeInState = 0
		f.m.active = true
		m.activePath = append(m.activePath, f)
		if s, ok := f.m.states[f.id]; ok && s.Enter != nil {
			s.Enter()
		}
	}

	last := prefix[len(prefix)-1]
	if ch := last.m.children[last.id]; ch != nil {
		m.descendInto(ch)
	}

	m.transitioning = false
	m.drainDeferredSet()
}

func (m *HSM[S]) enterPrefixAndDescend(prefix []hsmFrame[S]) {
	for _, f := range prefix {
		f.m.current = f.id
		f.m.timeInState = 0
		f.m.active = true
		m.activePath = append(m.activePath, f)
		if s, ok := f.m.states[f.id]; ok && s.Enter != nil {
			s.Enter()
		}
	}
	if len(prefix) > 0 {
		last := prefix[len(prefix)-1]
		if ch := last.m.children[last.id]; ch != nil {
			m.descendInto(ch)
		}
	}
}

func (m *HSM[S]) enterFrom(node *HSM[S], id S, useHistory bool) {
	chosen := id
	if useHistory && node.history && node.hasHistory {
		chosen = node.lastChild
	}
	node.current = chosen
	node.timeInState = 0
	node.active = true
	m.activePath = append(m.activePath, hsmFrame[S]{m: node, id: chosen})
	if s, ok := node.states[chosen]; ok && s.Enter != nil {
		s.Enter()
	}
	if ch := node.children[chosen]; ch != nil {
		m.descendInto(ch)
	}
}

func (m *HSM[S]) descendInto(ch *HSM[S]) {
	m.enterFrom(ch, ch.initial, true)
}

// Current returns the active state path from m down to the active leaf state.
// Before Start is called, Current returns the initial path from m to its initial leaf.
// For an attached child machine that is not currently active, Current returns nil.
func (m *HSM[S]) Current() []S {
	r := m.root()
	if !r.started {
		var path []S
		cur := m
		for cur != nil {
			path = append(path, cur.initial)
			cur = cur.children[cur.initial]
		}
		return path
	}
	for i, f := range r.activePath {
		if f.m == m {
			out := make([]S, len(r.activePath)-i)
			for j := i; j < len(r.activePath); j++ {
				out[j-i] = r.activePath[j].id
			}
			return out
		}
	}
	return nil
}

// Leaf returns the innermost active leaf state reachable from m, or the zero value of S if m is an inactive child machine.
func (m *HSM[S]) Leaf() S {
	path := m.Current()
	if len(path) == 0 {
		var zero S
		return zero
	}
	return path[len(path)-1]
}

// In reports whether state is currently active anywhere along m's active path.
func (m *HSM[S]) In(state S) bool {
	return slices.Contains(m.Current(), state)
}

// Previous returns the state before the last transition at m's level, or the zero value of S before any transition.
func (m *HSM[S]) Previous() S { return m.previous }

// TimeInState returns the game time spent in m's current state since entering it.
func (m *HSM[S]) TimeInState() time.Duration { return m.timeInState }

// TimeIn returns the game time spent in state if state is currently active along m's active path, or 0 otherwise.
func (m *HSM[S]) TimeIn(state S) time.Duration {
	r := m.root()
	if !r.started {
		return 0
	}
	start := -1
	for i, f := range r.activePath {
		if f.m == m {
			start = i
			break
		}
	}
	if start < 0 {
		return 0
	}
	for i := start; i < len(r.activePath); i++ {
		if r.activePath[i].id == state {
			return r.activePath[i].m.timeInState
		}
	}
	return 0
}
