package entities

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrEmptySystemName reports a system or ordering constraint with an empty name.
	ErrEmptySystemName = errors.New("entities: empty system name")
	// ErrDuplicateSystem reports registering a system whose name is already present in the Scheduler.
	ErrDuplicateSystem = errors.New("entities: duplicate system name")
	// ErrNilSystem reports registering a system with a nil update function.
	ErrNilSystem = errors.New("entities: nil system function")
	// ErrUnknownSystem reports a Before or After constraint naming a system that was never registered.
	ErrUnknownSystem = errors.New("entities: unknown system dependency")
	// ErrSystemCycle reports a cycle in the system ordering constraints.
	ErrSystemCycle = errors.New("entities: cyclic system dependency")
)

// SystemOption declares an ordering constraint on a system added to a Scheduler.
type SystemOption func(*systemEntry)

// Before constrains the system to run before the system named other.
func Before(other string) SystemOption {
	return func(s *systemEntry) {
		s.before = append(s.before, other)
	}
}

// After constrains the system to run after the system named other.
func After(other string) SystemOption {
	return func(s *systemEntry) {
		s.after = append(s.after, other)
	}
}

// RunIf runs the system only when cond is true for the current world.
func RunIf(cond func(*World) bool) SystemOption { return func(s *systemEntry) { s.cond = cond } }

type systemEntry struct {
	name   string
	fn     func(w *World, dt time.Duration)
	before []string
	after  []string
	cond   func(*World) bool
}

// Scheduler runs named systems over a World in a deterministic topological order derived from Before and After constraints.
//
// Ties between unconstrained systems are broken by registration order. The zero value of Scheduler is ready to use.
type Scheduler struct {
	systems []systemEntry
	byName  map[string]int
	order   []int
	dirty   bool
}

// NewScheduler returns an empty Scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{}
}

// HasSystem reports whether a system named name has been registered on s.
func (s *Scheduler) HasSystem(name string) bool {
	if s == nil || name == "" {
		return false
	}
	_, exists := s.byName[name]
	return exists
}

// AddSystem registers a named system with optional Before/After constraints.
//
// Constraint targets may be registered later; missing targets and dependency cycles are reported by Build and Step. Immediate errors (empty name, duplicate name, nil function, empty target, or self-dependency) are returned right away without modifying s.
func (s *Scheduler) AddSystem(name string, fn func(w *World, dt time.Duration), opts ...SystemOption) error {
	if name == "" {
		return ErrEmptySystemName
	}
	if fn == nil {
		return fmt.Errorf("entities: system %q: %w", name, ErrNilSystem)
	}
	if _, exists := s.byName[name]; exists {
		return fmt.Errorf("entities: system %q: %w", name, ErrDuplicateSystem)
	}
	entry := systemEntry{name: name, fn: fn}
	for _, opt := range opts {
		if opt != nil {
			opt(&entry)
		}
	}
	for _, target := range entry.before {
		if target == "" {
			return fmt.Errorf("entities: system %q Before: %w", name, ErrEmptySystemName)
		}
		if target == name {
			return fmt.Errorf("entities: system %q cannot run before itself: %w", name, ErrSystemCycle)
		}
	}
	for _, target := range entry.after {
		if target == "" {
			return fmt.Errorf("entities: system %q After: %w", name, ErrEmptySystemName)
		}
		if target == name {
			return fmt.Errorf("entities: system %q cannot run after itself: %w", name, ErrSystemCycle)
		}
	}
	if s.byName == nil {
		s.byName = make(map[string]int)
	}
	s.byName[name] = len(s.systems)
	s.systems = append(s.systems, entry)
	s.dirty = true
	return nil
}

// Build validates all Before/After constraints and resolves the deterministic execution order using Kahn's algorithm with registration-order tie-breaking.
func (s *Scheduler) Build() error {
	n := len(s.systems)
	if n == 0 {
		s.order = s.order[:0]
		s.dirty = false
		return nil
	}

	adj := make([][]int, n)
	inDegree := make([]int, n)
	seenEdge := make(map[uint64]struct{})
	addEdge := func(from, to int) {
		key := uint64(from)<<32 | uint64(to)
		if _, dup := seenEdge[key]; dup {
			return
		}
		seenEdge[key] = struct{}{}
		adj[from] = append(adj[from], to)
		inDegree[to]++
	}

	for i, sys := range s.systems {
		for _, target := range sys.before {
			to, ok := s.byName[target]
			if !ok {
				return fmt.Errorf("entities: system %q Before %q: %w", sys.name, target, ErrUnknownSystem)
			}
			addEdge(i, to)
		}
		for _, target := range sys.after {
			from, ok := s.byName[target]
			if !ok {
				return fmt.Errorf("entities: system %q After %q: %w", sys.name, target, ErrUnknownSystem)
			}
			addEdge(from, i)
		}
	}

	var ready intMinHeap
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			ready.push(i)
		}
	}

	order := make([]int, 0, n)
	for len(ready) > 0 {
		u := ready.pop()
		order = append(order, u)
		for _, v := range adj[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				ready.push(v)
			}
		}
	}

	if len(order) != n {
		var stuck []string
		for i := 0; i < n; i++ {
			if inDegree[i] > 0 {
				stuck = append(stuck, s.systems[i].name)
			}
		}
		return fmt.Errorf("entities: systems [%s]: %w", strings.Join(stuck, ", "), ErrSystemCycle)
	}

	s.order = order
	s.dirty = false
	return nil
}

// Order returns the resolved system names in execution order.
// It resolves the schedule lazily if needed and returns nil if the schedule is empty or invalid.
func (s *Scheduler) Order() []string {
	if s.dirty {
		if err := s.Build(); err != nil {
			return nil
		}
	}
	if len(s.order) == 0 {
		return nil
	}
	out := make([]string, len(s.order))
	for i, idx := range s.order {
		out[i] = s.systems[idx].name
	}
	return out
}

// String formats the resolved system order as "a -> b -> c" for debug overlays and logs.
func (s *Scheduler) String() string {
	return strings.Join(s.Order(), " -> ")
}

// Step resolves the schedule if needed and runs every system on w with dt in exact resolved order.
// Once built, Step performs zero heap allocations of its own.
func (s *Scheduler) Step(w *World, dt time.Duration) error {
	if s.dirty {
		if err := s.Build(); err != nil {
			return err
		}
	}
	for _, idx := range s.order {
		if s.systems[idx].cond == nil || s.systems[idx].cond(w) {
			s.systems[idx].fn(w, dt)
		}
	}
	return nil
}

// intMinHeap is a minimal binary min-heap of registration indices for deterministic Kahn tie-breaking.
type intMinHeap []int

func (h *intMinHeap) push(v int) {
	*h = append(*h, v)
	i := len(*h) - 1
	for i > 0 {
		p := (i - 1) / 2
		if (*h)[p] <= (*h)[i] {
			break
		}
		(*h)[p], (*h)[i] = (*h)[i], (*h)[p]
		i = p
	}
}

func (h *intMinHeap) pop() int {
	s := *h
	minVal := s[0]
	last := len(s) - 1
	s[0] = s[last]
	s = s[:last]
	*h = s
	i := 0
	for {
		l := 2*i + 1
		if l >= len(s) {
			break
		}
		smallest := l
		if r := l + 1; r < len(s) && s[r] < s[l] {
			smallest = r
		}
		if s[i] <= s[smallest] {
			break
		}
		s[i], s[smallest] = s[smallest], s[i]
		i = smallest
	}
	return minVal
}
