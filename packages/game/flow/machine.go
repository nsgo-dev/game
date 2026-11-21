package flow

import "time"

// State holds the optional callbacks for one state of a Machine.
type State struct {
	// Enter runs once when the machine enters the state.
	Enter func()
	// Update runs on each Machine.Update step while the machine is in the state.
	Update func(dt time.Duration)
	// Exit runs once when the machine leaves the state.
	Exit func()
}

type transition[S comparable] struct {
	any  bool
	from S
	to   S
	when func() bool
}

// Machine is a finite state machine keyed by states of type S and stepped by Update on a game.Loop.
type Machine[S comparable] struct {
	states      map[S]State
	transitions []transition[S]
	current     S
	previous    S
	timeInState time.Duration
	started     bool
}

// NewMachine returns a state machine whose initial state is initial. Call Start to run the initial state's Enter callback before the first Update, or let the first Update start it automatically.
func NewMachine[S comparable](initial S) *Machine[S] {
	return &Machine[S]{
		states:  make(map[S]State),
		current: initial,
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
	m.transitions = append(m.transitions, transition[S]{from: from, to: to, when: when})
}

// AddAnyTransition registers a conditional transition to to from any other state. Transitions are checked in declaration order during Update, and a wildcard transition does not re-enter to when already in it.
func (m *Machine[S]) AddAnyTransition(to S, when func() bool) {
	m.transitions = append(m.transitions, transition[S]{any: true, to: to, when: when})
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
			m.previous = m.current
			m.current = to
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
	m.transitionTo(to)
}

func (m *Machine[S]) transitionTo(to S) {
	if s, ok := m.states[m.current]; ok && s.Exit != nil {
		s.Exit()
	}
	m.previous = m.current
	m.current = to
	m.timeInState = 0
	if s, ok := m.states[m.current]; ok && s.Enter != nil {
		s.Enter()
	}
}

// Update advances the machine by dt: it starts the machine if needed, evaluates transitions in declaration order (taking at most one), adds dt to TimeInState and runs the current state's Update callback.
func (m *Machine[S]) Update(dt time.Duration) {
	if !m.started {
		m.Start()
	}
	for _, tr := range m.transitions {
		if tr.any {
			if m.current == tr.to {
				continue
			}
		} else if tr.from != m.current {
			continue
		}
		if tr.when != nil && tr.when() {
			m.transitionTo(tr.to)
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
