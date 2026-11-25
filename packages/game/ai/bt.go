package ai

import (
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

// Node is one node in a behavior tree.
type Node interface {
	// Tick advances the node by dt and returns its current status.
	Tick(dt time.Duration) Status
	// Reset clears any in-progress state on this node and its descendants.
	Reset()
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
	hasObs := false
	for _, ch := range cloned {
		if nodeHasBBObserver(ch) {
			hasObs = true
			break
		}
	}
	return &sequenceNode{children: cloned, hasObserver: hasObs}
}

type sequenceNode struct {
	children    []Node
	cur         int
	hasObserver bool
}

func (s *sequenceNode) Tick(dt time.Duration) Status {
	if s.hasObserver && s.cur > 0 {
		for i := 0; i < s.cur; i++ {
			if sa, ok := s.children[i].(btSelfAbortable); ok && sa.bbSelfAborted() {
				if running := s.children[s.cur]; running != nil {
					running.Reset()
				}
				s.cur = 0
				return StatusFailure
			}
		}
	}
	for s.cur < len(s.children) {
		ch := s.children[s.cur]
		if ch == nil {
			s.cur = 0
			return StatusFailure
		}
		switch ch.Tick(dt) {
		case StatusRunning:
			return StatusRunning
		case StatusFailure:
			s.cur = 0
			return StatusFailure
		case StatusSuccess:
			s.cur++
		}
	}
	s.cur = 0
	return StatusSuccess
}

func (s *sequenceNode) Reset() {
	s.cur = 0
	for _, ch := range s.children {
		if ch != nil {
			ch.Reset()
		}
	}
}

// Selector tries children in priority order, returning on the first child that succeeds or runs.
//
// If a higher-priority child preempts a lower-priority child that was previously running, Selector resets the interrupted child.
// Higher-priority children guarded by a [BlackboardCondition] only preempt a running lower-priority branch when their abort mode includes [AbortLower] or [AbortBoth] and their watched key changes.
func Selector(children ...Node) Node {
	cloned := slices.Clone(children)
	hasObs := false
	for _, ch := range cloned {
		if nodeHasBBObserver(ch) {
			hasObs = true
			break
		}
	}
	return &selectorNode{children: cloned, running: -1, hasObserver: hasObs}
}

type selectorNode struct {
	children    []Node
	running     int
	hasObserver bool
}

func (s *selectorNode) Tick(dt time.Duration) Status {
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
				return StatusRunning
			}
			s.running = -1
			return StatusSuccess
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
			return StatusRunning
		}
		s.running = -1
		return StatusSuccess
	}
	s.running = -1
	return StatusFailure
}

func (s *selectorNode) Reset() {
	s.running = -1
	for _, ch := range s.children {
		if ch != nil {
			ch.Reset()
		}
	}
}

// Parallel ticks all still-running children each tick and finishes according to policy, resetting any remaining running children on completion.
func Parallel(policy ParallelPolicy, children ...Node) Node {
	cloned := slices.Clone(children)
	return &parallelNode{
		policy:   policy,
		children: cloned,
		status:   make([]Status, len(cloned)),
	}
}

type parallelNode struct {
	policy   ParallelPolicy
	children []Node
	status   []Status
	active   bool
}

func (p *parallelNode) Tick(dt time.Duration) Status {
	if len(p.children) == 0 {
		if p.policy == RequireOne {
			return StatusFailure
		}
		return StatusSuccess
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
			return StatusSuccess
		}
		if failures == len(p.children) {
			p.active = false
			return StatusFailure
		}
	default:
		if failures > 0 {
			p.resetRunning()
			p.active = false
			return StatusFailure
		}
		if successes == len(p.children) {
			p.active = false
			return StatusSuccess
		}
	}
	return StatusRunning
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
	for _, ch := range p.children {
		if ch != nil {
			ch.Reset()
		}
	}
}

// Inverter flips StatusSuccess and StatusFailure from child while passing StatusRunning through unchanged.
func Inverter(child Node) Node { return &inverterNode{child: child} }

type inverterNode struct{ child Node }

func (n *inverterNode) Tick(dt time.Duration) Status {
	if n.child == nil {
		return StatusFailure
	}
	switch n.child.Tick(dt) {
	case StatusSuccess:
		return StatusFailure
	case StatusFailure:
		return StatusSuccess
	default:
		return StatusRunning
	}
}

func (n *inverterNode) Reset() {
	if n.child != nil {
		n.child.Reset()
	}
}

// Succeeder turns both StatusSuccess and StatusFailure from child into StatusSuccess while passing StatusRunning through.
func Succeeder(child Node) Node { return &succeederNode{child: child} }

type succeederNode struct{ child Node }

func (n *succeederNode) Tick(dt time.Duration) Status {
	if n.child == nil {
		return StatusSuccess
	}
	if n.child.Tick(dt) == StatusRunning {
		return StatusRunning
	}
	return StatusSuccess
}

func (n *succeederNode) Reset() {
	if n.child != nil {
		n.child.Reset()
	}
}

// Failer turns both StatusSuccess and StatusFailure from child into StatusFailure while passing StatusRunning through.
func Failer(child Node) Node { return &failerNode{child: child} }

type failerNode struct{ child Node }

func (n *failerNode) Tick(dt time.Duration) Status {
	if n.child == nil {
		return StatusFailure
	}
	if n.child.Tick(dt) == StatusRunning {
		return StatusRunning
	}
	return StatusFailure
}

func (n *failerNode) Reset() {
	if n.child != nil {
		n.child.Reset()
	}
}

// Repeater runs child until it succeeds count times, returning StatusRunning between iterations and StatusFailure immediately if child fails.
//
// When count is less than or equal to 0, Repeater repeats indefinitely until child fails.
func Repeater(count int, child Node) Node {
	return &repeaterNode{count: count, child: child}
}

type repeaterNode struct {
	count int
	done  int
	child Node
}

func (r *repeaterNode) Tick(dt time.Duration) Status {
	if r.child == nil {
		return StatusFailure
	}
	switch r.child.Tick(dt) {
	case StatusRunning:
		return StatusRunning
	case StatusFailure:
		r.done = 0
		return StatusFailure
	default:
		if r.count > 0 {
			r.done++
			if r.done >= r.count {
				r.done = 0
				return StatusSuccess
			}
		}
		return StatusRunning
	}
}

func (r *repeaterNode) Reset() {
	r.done = 0
	if r.child != nil {
		r.child.Reset()
	}
}

// UntilFail ticks child once per tick, returning StatusRunning until child returns StatusFailure, at which point UntilFail returns StatusSuccess.
func UntilFail(child Node) Node { return &untilFailNode{child: child} }

type untilFailNode struct{ child Node }

func (u *untilFailNode) Tick(dt time.Duration) Status {
	if u.child == nil {
		return StatusSuccess
	}
	if u.child.Tick(dt) == StatusFailure {
		return StatusSuccess
	}
	return StatusRunning
}

func (u *untilFailNode) Reset() {
	if u.child != nil {
		u.child.Reset()
	}
}

// Cooldown prevents child from starting again for duration d after it succeeds, returning StatusFailure while cooling down.
func Cooldown(d time.Duration, child Node) Node {
	return &cooldownNode{duration: d, child: child}
}

type cooldownNode struct {
	duration  time.Duration
	remaining time.Duration
	running   bool
	child     Node
}

func (c *cooldownNode) Tick(dt time.Duration) Status {
	if c.child == nil {
		return StatusFailure
	}
	if !c.running && c.remaining > 0 {
		if dt > 0 {
			c.remaining -= dt
		}
		if c.remaining > 0 {
			return StatusFailure
		}
		c.remaining = 0
	}
	st := c.child.Tick(dt)
	switch st {
	case StatusRunning:
		c.running = true
		return StatusRunning
	case StatusSuccess:
		c.running = false
		if c.duration > 0 {
			c.remaining = c.duration
		}
		return StatusSuccess
	default:
		c.running = false
		return StatusFailure
	}
}

func (c *cooldownNode) Reset() {
	c.remaining = 0
	c.running = false
	if c.child != nil {
		c.child.Reset()
	}
}

// Guard ticks child only while cond returns true; if cond becomes false while child is running, Guard resets child and returns StatusFailure.
func Guard(cond func() bool, child Node) Node {
	return &guardNode{cond: cond, child: child}
}

type guardNode struct {
	cond    func() bool
	child   Node
	running bool
}

func (g *guardNode) Tick(dt time.Duration) Status {
	if g.cond == nil || !g.cond() {
		if g.running && g.child != nil {
			g.child.Reset()
		}
		g.running = false
		return StatusFailure
	}
	if g.child == nil {
		return StatusFailure
	}
	st := g.child.Tick(dt)
	g.running = st == StatusRunning
	return st
}

func (g *guardNode) Reset() {
	g.running = false
	if g.child != nil {
		g.child.Reset()
	}
}

// Action wraps fn as a leaf node.
func Action(fn func(dt time.Duration) Status) Node { return actionNode{fn: fn} }

type actionNode struct {
	fn func(dt time.Duration) Status
}

func (a actionNode) Tick(dt time.Duration) Status {
	if a.fn == nil {
		return StatusFailure
	}
	return a.fn(dt)
}

func (actionNode) Reset() {}

// Condition wraps predicate fn as a leaf node returning StatusSuccess when true and StatusFailure when false.
func Condition(fn func() bool) Node { return conditionNode{fn: fn} }

type conditionNode struct {
	fn func() bool
}

func (c conditionNode) Tick(time.Duration) Status {
	if c.fn != nil && c.fn() {
		return StatusSuccess
	}
	return StatusFailure
}

func (conditionNode) Reset() {}

// Wait returns StatusRunning until the accumulated dt reaches d, then resets and returns StatusSuccess.
func Wait(d time.Duration) Node { return &waitNode{duration: d} }

type waitNode struct {
	duration time.Duration
	elapsed  time.Duration
}

func (w *waitNode) Tick(dt time.Duration) Status {
	if w.duration <= 0 {
		return StatusSuccess
	}
	if dt > 0 {
		w.elapsed += dt
	}
	if w.elapsed >= w.duration {
		w.elapsed = 0
		return StatusSuccess
	}
	return StatusRunning
}

func (w *waitNode) Reset() {
	w.elapsed = 0
}
