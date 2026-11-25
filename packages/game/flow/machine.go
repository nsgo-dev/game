package flow

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultHistoryLimit is the default maximum number of transitions retained by a Machine.
const DefaultHistoryLimit = 32

// State holds the optional callbacks for one state of a Machine.
type State struct {
	// Enter runs once when the machine enters the state.
	Enter func()
	// Update runs on each Machine.Update step while the machine is in the state.
	Update func(dt time.Duration)
	// Exit runs once when the machine leaves the state.
	Exit func()
}

// TransitionRecord records one state transition taken by a Machine.
type TransitionRecord[S comparable] struct {
	// From is the state the machine left.
	From S
	// To is the state the machine entered.
	To S
	// Step is the Update step count on which the transition occurred (0 before the first Update).
	Step uint64
	// Transition names the transition that fired: the label passed to AddNamedTransition or
	// AddNamedAnyTransition, "from->to" / "*->to" for unnamed transitions, or "SetState".
	Transition string
}

// TransitionSnapshot is a type-erased TransitionRecord for inspection and debug overlays.
type TransitionSnapshot struct {
	// From is the formatted state the machine left.
	From string
	// To is the formatted state the machine entered.
	To string
	// Step is the Update step count on which the transition occurred.
	Step uint64
	// Transition names the transition that fired.
	Transition string
}

// MachineInspection is a type-erased, read-only snapshot of a Machine's current state and transition history.
type MachineInspection struct {
	// Current is the formatted current state.
	Current string
	// Previous is the formatted previous state (empty when HasPrevious is false).
	Previous string
	// HasPrevious reports whether at least one transition has occurred.
	HasPrevious bool
	// Step is the total number of Update steps executed.
	Step uint64
	// TimeInState is the elapsed game time spent in Current.
	TimeInState time.Duration
	// History lists the retained recent transitions in chronological order (oldest to newest).
	History []TransitionSnapshot
}

// MachineInspector is implemented by every *Machine[S] so tools and debug overlays can inspect
// any Machine regardless of its state type parameter S.
type MachineInspector interface {
	// Inspect returns a type-erased read-only snapshot of the machine's current state and transition history.
	Inspect() MachineInspection
	// FormatState returns a deterministic text summary of the machine's current state and transition history.
	FormatState() string
}

type transition[S comparable] struct {
	any   bool
	from  S
	to    S
	when  func() bool
	label string
}

// Machine is a finite state machine keyed by states of type S and stepped by Update on a game.Loop.
type Machine[S comparable] struct {
	states      map[S]State
	transitions []transition[S]
	initial     S
	hasInitial  bool
	current     S
	previous    S
	hasPrevious bool
	timeInState time.Duration
	started     bool
	step        uint64

	historyCustom bool
	historyLimit  int
	history       []TransitionRecord[S]
	historyHead   int
	historyCount  int
}

// NewMachine returns a state machine whose initial state is initial. Call Start to run the initial state's Enter callback before the first Update, or let the first Update start it automatically.
func NewMachine[S comparable](initial S) *Machine[S] {
	return &Machine[S]{
		states:     make(map[S]State),
		initial:    initial,
		hasInitial: true,
		current:    initial,
	}
}

// AddState registers the callbacks for id, replacing any previously registered for id.
func (m *Machine[S]) AddState(id S, state State) {
	if m.states == nil {
		m.states = make(map[S]State)
	}
	m.states[id] = state
}

// AddTransition registers a conditional transition from from to to. Transitions are checked in declaration order during Update.
func (m *Machine[S]) AddTransition(from, to S, when func() bool) {
	m.AddNamedTransition("", from, to, when)
}

// AddNamedTransition registers a labeled conditional transition from from to to.
// When name is non-empty it is recorded in TransitionRecord.Transition whenever the transition fires;
// when empty, a default "from->to" label is used.
func (m *Machine[S]) AddNamedTransition(name string, from, to S, when func() bool) {
	label := name
	if label == "" {
		label = fmt.Sprintf("%v->%v", from, to)
	}
	m.transitions = append(m.transitions, transition[S]{from: from, to: to, when: when, label: label})
}

// AddAnyTransition registers a conditional transition to to from any other state. Transitions are checked in declaration order during Update, and a wildcard transition does not re-enter to when already in it.
func (m *Machine[S]) AddAnyTransition(to S, when func() bool) {
	m.AddNamedAnyTransition("", to, when)
}

// AddNamedAnyTransition registers a labeled wildcard transition to to from any other state.
// When name is non-empty it is recorded in TransitionRecord.Transition whenever the transition fires;
// when empty, a default "*->to" label is used.
func (m *Machine[S]) AddNamedAnyTransition(name string, to S, when func() bool) {
	label := name
	if label == "" {
		label = fmt.Sprintf("*->%v", to)
	}
	m.transitions = append(m.transitions, transition[S]{any: true, to: to, when: when, label: label})
}

// Start runs the initial state's Enter callback if the machine has not yet started. Subsequent calls are no-ops.
func (m *Machine[S]) Start() {
	if m.started {
		return
	}
	m.started = true
	m.timeInState = 0
	if s, ok := m.states[m.current]; ok && s.Enter != nil {
		s.Enter()
	}
}

// SetState immediately switches to to, running the current state's Exit and to's Enter callbacks. Switching to the current state after Start is a no-op.
func (m *Machine[S]) SetState(to S) {
	if !m.started {
		m.started = true
		if to != m.current {
			from := m.current
			m.previous = from
			m.hasPrevious = true
			m.current = to
			m.recordTransition(from, to, "SetState")
		}
		m.timeInState = 0
		if s, ok := m.states[m.current]; ok && s.Enter != nil {
			s.Enter()
		}
		return
	}
	if m.current == to {
		return
	}
	m.transitionTo(to, "SetState")
}

func (m *Machine[S]) transitionTo(to S, label string) {
	from := m.current
	if s, ok := m.states[from]; ok && s.Exit != nil {
		s.Exit()
	}
	m.previous = from
	m.hasPrevious = true
	m.current = to
	m.timeInState = 0
	m.recordTransition(from, to, label)
	if s, ok := m.states[m.current]; ok && s.Enter != nil {
		s.Enter()
	}
}

func (m *Machine[S]) effectiveHistoryLimit() int {
	if !m.historyCustom {
		return DefaultHistoryLimit
	}
	return m.historyLimit
}

func (m *Machine[S]) recordTransition(from, to S, label string) {
	limit := m.effectiveHistoryLimit()
	if limit <= 0 {
		return
	}
	if len(m.history) != limit {
		m.history = make([]TransitionRecord[S], limit)
		m.historyHead = 0
		m.historyCount = 0
	}
	rec := TransitionRecord[S]{
		From:       from,
		To:         to,
		Step:       m.step,
		Transition: label,
	}
	if m.historyCount < limit {
		idx := (m.historyHead + m.historyCount) % limit
		m.history[idx] = rec
		m.historyCount++
	} else {
		m.history[m.historyHead] = rec
		m.historyHead = (m.historyHead + 1) % limit
	}
}

// Update advances the machine by dt: it starts the machine if needed, increments Step, evaluates transitions in declaration order (taking at most one), adds dt to TimeInState and runs the current state's Update callback.
func (m *Machine[S]) Update(dt time.Duration) {
	if !m.started {
		m.Start()
	}
	m.step++
	for _, tr := range m.transitions {
		if tr.any {
			if m.current == tr.to {
				continue
			}
		} else if tr.from != m.current {
			continue
		}
		if tr.when != nil && tr.when() {
			m.transitionTo(tr.to, tr.label)
			break
		}
	}
	if dt > 0 {
		m.timeInState += dt
	}
	if s, ok := m.states[m.current]; ok && s.Update != nil {
		s.Update(dt)
	}
}

// Current returns the machine's current state.
func (m *Machine[S]) Current() S { return m.current }

// Previous returns the state before the last transition, or the zero value of S before any transition.
func (m *Machine[S]) Previous() S { return m.previous }

// TimeInState returns the game time spent in the current state since entering it.
func (m *Machine[S]) TimeInState() time.Duration { return m.timeInState }

// Step returns the number of Update steps executed by m.
func (m *Machine[S]) Step() uint64 {
	if m == nil {
		return 0
	}
	return m.step
}

// HistoryLimit returns the maximum number of transitions retained in m's ring buffer.
// Before SetHistoryLimit is called, HistoryLimit returns DefaultHistoryLimit.
func (m *Machine[S]) HistoryLimit() int {
	if m == nil {
		return 0
	}
	return m.effectiveHistoryLimit()
}

// SetHistoryLimit sets the maximum number of recent transitions retained by m.
// Passing 0 or a negative value disables transition history and clears any retained entries.
// Shrinking the limit preserves the most recent transitions in chronological order.
func (m *Machine[S]) SetHistoryLimit(limit int) {
	if m == nil {
		return
	}
	if limit < 0 {
		limit = 0
	}
	m.historyCustom = true
	m.historyLimit = limit
	if limit == 0 {
		m.history = nil
		m.historyHead = 0
		m.historyCount = 0
		return
	}
	if m.historyCount == 0 {
		m.history = nil
		m.historyHead = 0
		return
	}
	if len(m.history) == limit {
		return
	}
	keep := min(m.historyCount, limit)
	skip := m.historyCount - keep
	next := make([]TransitionRecord[S], limit)
	oldCap := len(m.history)
	for i := range keep {
		next[i] = m.history[(m.historyHead+skip+i)%oldCap]
	}
	m.history = next
	m.historyHead = 0
	m.historyCount = keep
}

// ClearHistory discards all recorded transitions without changing HistoryLimit or Step.
func (m *Machine[S]) ClearHistory() {
	if m == nil {
		return
	}
	clear(m.history)
	m.historyHead = 0
	m.historyCount = 0
}

// HistoryInto appends m's retained transitions in chronological order (oldest to newest)
// into dst[:0], performing zero heap allocations when cap(dst) is sufficient.
func (m *Machine[S]) HistoryInto(dst []TransitionRecord[S]) []TransitionRecord[S] {
	dst = dst[:0]
	if m == nil || m.historyCount == 0 {
		return dst
	}
	capN := len(m.history)
	for i := range m.historyCount {
		dst = append(dst, m.history[(m.historyHead+i)%capN])
	}
	return dst
}

// History returns m's retained transitions in chronological order (oldest to newest).
func (m *Machine[S]) History() []TransitionRecord[S] {
	if m == nil || m.historyCount == 0 {
		return nil
	}
	return m.HistoryInto(make([]TransitionRecord[S], 0, m.historyCount))
}

// Inspect returns a type-erased read-only snapshot of m's current state and transition history.
func (m *Machine[S]) Inspect() MachineInspection {
	if m == nil {
		return MachineInspection{}
	}
	snap := MachineInspection{
		Current:     fmt.Sprint(m.current),
		HasPrevious: m.hasPrevious,
		Step:        m.step,
		TimeInState: m.timeInState,
	}
	if m.hasPrevious {
		snap.Previous = fmt.Sprint(m.previous)
	}
	if m.historyCount > 0 {
		snap.History = make([]TransitionSnapshot, m.historyCount)
		capN := len(m.history)
		for i := range m.historyCount {
			rec := m.history[(m.historyHead+i)%capN]
			snap.History[i] = TransitionSnapshot{
				From:       fmt.Sprint(rec.From),
				To:         fmt.Sprint(rec.To),
				Step:       rec.Step,
				Transition: rec.Transition,
			}
		}
	}
	return snap
}

// FormatState returns a deterministic, human-readable text summary of m's current state and
// transition history suitable for statedump Values and diffing AI decisions across runs.
func (m *Machine[S]) FormatState() string {
	return FormatMachineInspection(m.Inspect())
}

// FormatMachineInspection renders snap in a deterministic canonical text format suitable for
// statedump Values and golden-file diffs across runs.
func FormatMachineInspection(snap MachineInspection) string {
	var b strings.Builder
	b.WriteString("state=")
	b.WriteString(snap.Current)
	if snap.HasPrevious {
		b.WriteString(" prev=")
		b.WriteString(snap.Previous)
	}
	b.WriteString(" step=")
	b.WriteString(strconv.FormatUint(snap.Step, 10))
	b.WriteString(" time=")
	b.WriteString(snap.TimeInState.String())
	b.WriteString("\nhistory (")
	b.WriteString(strconv.Itoa(len(snap.History)))
	b.WriteByte(')')
	if len(snap.History) > 0 {
		b.WriteByte(':')
		for _, tr := range snap.History {
			b.WriteString("\n  step ")
			b.WriteString(strconv.FormatUint(tr.Step, 10))
			b.WriteString(": ")
			b.WriteString(tr.From)
			b.WriteString(" -> ")
			b.WriteString(tr.To)
			if tr.Transition != "" {
				b.WriteString(" (")
				b.WriteString(tr.Transition)
				b.WriteByte(')')
			}
		}
	}
	return b.String()
}
