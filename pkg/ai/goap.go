package ai

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"slices"
	"time"
)

const (
	// MaxBoolFacts is the maximum number of named boolean facts a FactSchema and WorldState can store.
	MaxBoolFacts = 128
	// MaxIntFacts is the maximum number of named small integer facts a FactSchema and WorldState can store.
	MaxIntFacts = 16
	// DefaultGOAPNodeBudget is the default maximum number of regressive A* search nodes expanded per plan.
	DefaultGOAPNodeBudget = 1024
)

var (
	// ErrNoPlan is returned when no sequence of usable actions can satisfy the target goal from the current WorldState.
	ErrNoPlan = errors.New("ai: no plan found")
	// ErrBudgetExceeded is returned when regressive A* exhausts GOAPPlanner.NodeBudget without finding a plan.
	ErrBudgetExceeded = errors.New("ai: planner node budget exceeded")
)

// BoolFact identifies a boolean fact slot (0..MaxBoolFacts-1) in a FactSchema and WorldState.
type BoolFact uint8

// IntFact identifies a small integer fact slot (0..MaxIntFacts-1) in a FactSchema and WorldState.
type IntFact uint8

// FactSchema maps human-readable boolean and integer fact names to compact fixed-size slots in a WorldState.
type FactSchema struct {
	boolNames []string
	boolIndex map[string]BoolFact
	intNames  []string
	intIndex  map[string]IntFact
}

// NewFactSchema creates an empty FactSchema.
func NewFactSchema() *FactSchema {
	return &FactSchema{
		boolIndex: map[string]BoolFact{},
		intIndex:  map[string]IntFact{},
	}
}

// Bool registers (or returns the existing slot for) a named boolean fact.
func (s *FactSchema) Bool(name string) (BoolFact, error) {
	if s == nil {
		return 0, errors.New("ai: nil fact schema")
	}
	if name == "" {
		return 0, errors.New("ai: empty boolean fact name")
	}
	if s.boolIndex == nil {
		s.boolIndex = map[string]BoolFact{}
	}
	if idx, ok := s.boolIndex[name]; ok {
		return idx, nil
	}
	if _, clash := s.intIndex[name]; clash {
		return 0, fmt.Errorf("ai: fact %q is already registered as an integer fact", name)
	}
	if len(s.boolNames) >= MaxBoolFacts {
		return 0, fmt.Errorf("ai: boolean fact limit %d exceeded registering %q", MaxBoolFacts, name)
	}
	idx := BoolFact(len(s.boolNames))
	s.boolNames = append(s.boolNames, name)
	s.boolIndex[name] = idx
	return idx, nil
}

// MustBool registers a named boolean fact like Bool and panics on error.
func (s *FactSchema) MustBool(name string) BoolFact {
	f, err := s.Bool(name)
	if err != nil {
		panic(err)
	}
	return f
}

// Int registers (or returns the existing slot for) a named small integer fact.
func (s *FactSchema) Int(name string) (IntFact, error) {
	if s == nil {
		return 0, errors.New("ai: nil fact schema")
	}
	if name == "" {
		return 0, errors.New("ai: empty integer fact name")
	}
	if s.intIndex == nil {
		s.intIndex = map[string]IntFact{}
	}
	if idx, ok := s.intIndex[name]; ok {
		return idx, nil
	}
	if _, clash := s.boolIndex[name]; clash {
		return 0, fmt.Errorf("ai: fact %q is already registered as a boolean fact", name)
	}
	if len(s.intNames) >= MaxIntFacts {
		return 0, fmt.Errorf("ai: integer fact limit %d exceeded registering %q", MaxIntFacts, name)
	}
	idx := IntFact(len(s.intNames))
	s.intNames = append(s.intNames, name)
	s.intIndex[name] = idx
	return idx, nil
}

// MustInt registers a named small integer fact like Int and panics on error.
func (s *FactSchema) MustInt(name string) IntFact {
	f, err := s.Int(name)
	if err != nil {
		panic(err)
	}
	return f
}

// LookupBool returns the slot for a registered boolean fact.
func (s *FactSchema) LookupBool(name string) (BoolFact, bool) {
	if s == nil {
		return 0, false
	}
	f, ok := s.boolIndex[name]
	return f, ok
}

// LookupInt returns the slot for a registered integer fact.
func (s *FactSchema) LookupInt(name string) (IntFact, bool) {
	if s == nil {
		return 0, false
	}
	f, ok := s.intIndex[name]
	return f, ok
}

// BoolName returns the registered name for f, or "" if out of range.
func (s *FactSchema) BoolName(f BoolFact) string {
	if s == nil || int(f) >= len(s.boolNames) {
		return ""
	}
	return s.boolNames[f]
}

// IntName returns the registered name for f, or "" if out of range.
func (s *FactSchema) IntName(f IntFact) string {
	if s == nil || int(f) >= len(s.intNames) {
		return ""
	}
	return s.intNames[f]
}

// BoolCount returns the number of registered boolean facts.
func (s *FactSchema) BoolCount() int {
	if s == nil {
		return 0
	}
	return len(s.boolNames)
}

// IntCount returns the number of registered integer facts.
func (s *FactSchema) IntCount() int {
	if s == nil {
		return 0
	}
	return len(s.intNames)
}

// Clone returns an independent copy of s.
func (s *FactSchema) Clone() *FactSchema {
	if s == nil {
		return nil
	}
	out := &FactSchema{
		boolNames: slices.Clone(s.boolNames),
		boolIndex: make(map[string]BoolFact, len(s.boolIndex)),
		intNames:  slices.Clone(s.intNames),
		intIndex:  make(map[string]IntFact, len(s.intIndex)),
	}
	for k, v := range s.boolIndex {
		out.boolIndex[k] = v
	}
	for k, v := range s.intIndex {
		out.intIndex[k] = v
	}
	return out
}

// WorldState is a compact, comparable snapshot of an agent's world facts:
// up to MaxBoolFacts boolean facts packed into a 128-bit bitset and up to MaxIntFacts small int16 facts.
type WorldState struct {
	bools [2]uint64
	ints  [MaxIntFacts]int16
}

// Bool returns the value of boolean fact f, or false if f is out of range.
func (w WorldState) Bool(f BoolFact) bool {
	if f >= MaxBoolFacts {
		return false
	}
	return (w.bools[f>>6] & (uint64(1) << (f & 63))) != 0
}

// SetBool sets boolean fact f to v. Out-of-range facts are ignored.
func (w *WorldState) SetBool(f BoolFact, v bool) {
	if w == nil || f >= MaxBoolFacts {
		return
	}
	mask := uint64(1) << (f & 63)
	if v {
		w.bools[f>>6] |= mask
	} else {
		w.bools[f>>6] &^= mask
	}
}

// WithBool returns a copy of w with boolean fact f set to v.
func (w WorldState) WithBool(f BoolFact, v bool) WorldState {
	w.SetBool(f, v)
	return w
}

// Int returns the value of integer fact f, or 0 if f is out of range.
func (w WorldState) Int(f IntFact) int {
	if f >= MaxIntFacts {
		return 0
	}
	return int(w.ints[f])
}

// SetInt sets integer fact f to v, clamped to the int16 range [-32768, 32767]. Out-of-range facts are ignored.
func (w *WorldState) SetInt(f IntFact, v int) {
	if w == nil || f >= MaxIntFacts {
		return
	}
	w.ints[f] = clampInt16(v)
}

// WithInt returns a copy of w with integer fact f set to v (clamped to int16 range).
func (w WorldState) WithInt(f IntFact, v int) WorldState {
	w.SetInt(f, v)
	return w
}

// Apply returns a copy of w updated with eff.
func (w WorldState) Apply(eff Effects) WorldState {
	return eff.Apply(w)
}

// Satisfies reports whether w satisfies every boolean and integer requirement in cond.
func (w WorldState) Satisfies(cond Conditions) bool {
	return cond.SatisfiedBy(w)
}

func clampInt16(v int) int16 {
	if v < math.MinInt16 {
		return math.MinInt16
	}
	if v > math.MaxInt16 {
		return math.MaxInt16
	}
	return int16(v)
}

type intBounds struct {
	lo int16
	hi int16
}

// Conditions specifies required boolean fact values and optional closed integer bounds [lo, hi].
// The zero Conditions matches every WorldState.
//
// Canonical invariant: bits outside boolMask are always 0 in boolVals, and intRange[i] is zero whenever bit i of intMask is 0.
type Conditions struct {
	boolMask [2]uint64
	boolVals [2]uint64
	intMask  uint16
	intRange [MaxIntFacts]intBounds
}

// Empty reports whether c imposes no boolean or integer constraints.
func (c Conditions) Empty() bool {
	return c.boolMask[0] == 0 && c.boolMask[1] == 0 && c.intMask == 0
}

// RequireBool returns a copy of c requiring boolean fact f to equal v.
func (c Conditions) RequireBool(f BoolFact, v bool) Conditions {
	if f >= MaxBoolFacts {
		return c
	}
	word := f >> 6
	bit := uint64(1) << (f & 63)
	c.boolMask[word] |= bit
	if v {
		c.boolVals[word] |= bit
	} else {
		c.boolVals[word] &^= bit
	}
	return c
}

// RequireIntRange returns a copy of c requiring integer fact f to lie in [lo, hi] inclusive.
// If f is already constrained in c, the bounds are intersected; an empty intersection (lo > hi) makes c unsatisfiable.
func (c Conditions) RequireIntRange(f IntFact, lo, hi int) Conditions {
	if f >= MaxIntFacts {
		return c
	}
	var b intBounds
	if lo > hi || lo > math.MaxInt16 || hi < math.MinInt16 {
		b = intBounds{lo: 1, hi: 0}
	} else {
		b = intBounds{lo: clampInt16(lo), hi: clampInt16(hi)}
	}
	bit := uint16(1) << f
	if c.intMask&bit != 0 {
		prev := c.intRange[f]
		if prev.lo > prev.hi || b.lo > b.hi {
			b = intBounds{lo: 1, hi: 0}
		} else {
			if prev.lo > b.lo {
				b.lo = prev.lo
			}
			if prev.hi < b.hi {
				b.hi = prev.hi
			}
			if b.lo > b.hi {
				b = intBounds{lo: 1, hi: 0}
			}
		}
	}
	if b.lo == math.MinInt16 && b.hi == math.MaxInt16 {
		c.intMask &^= bit
		c.intRange[f] = intBounds{}
		return c
	}
	c.intMask |= bit
	c.intRange[f] = b
	return c
}

// RequireIntEq returns a copy of c requiring integer fact f == v.
func (c Conditions) RequireIntEq(f IntFact, v int) Conditions {
	return c.RequireIntRange(f, v, v)
}

// RequireIntGte returns a copy of c requiring integer fact f >= v.
func (c Conditions) RequireIntGte(f IntFact, v int) Conditions {
	return c.RequireIntRange(f, v, math.MaxInt16)
}

// RequireIntLte returns a copy of c requiring integer fact f <= v.
func (c Conditions) RequireIntLte(f IntFact, v int) Conditions {
	return c.RequireIntRange(f, math.MinInt16, v)
}

// RequireIntGt returns a copy of c requiring integer fact f > v.
func (c Conditions) RequireIntGt(f IntFact, v int) Conditions {
	return c.RequireIntRange(f, v+1, math.MaxInt16)
}

// RequireIntLt returns a copy of c requiring integer fact f < v.
func (c Conditions) RequireIntLt(f IntFact, v int) Conditions {
	return c.RequireIntRange(f, math.MinInt16, v-1)
}

// SatisfiedBy reports whether w satisfies every constraint in c.
func (c Conditions) SatisfiedBy(w WorldState) bool {
	if (w.bools[0]&c.boolMask[0]) != c.boolVals[0] || (w.bools[1]&c.boolMask[1]) != c.boolVals[1] {
		return false
	}
	for m := c.intMask; m != 0; m &= m - 1 {
		i := bits.TrailingZeros16(m)
		b := c.intRange[i]
		v := w.ints[i]
		if v < b.lo || v > b.hi {
			return false
		}
	}
	return true
}

func (c Conditions) isUnsatisfiable() bool {
	for m := c.intMask; m != 0; m &= m - 1 {
		i := bits.TrailingZeros16(m)
		if c.intRange[i].lo > c.intRange[i].hi {
			return true
		}
	}
	return false
}

// IntEffectKind identifies how an action modifies a small integer fact.
type IntEffectKind uint8

const (
	// IntEffectNone leaves the integer fact unchanged.
	IntEffectNone IntEffectKind = iota
	// IntEffectSet assigns a constant value to the integer fact.
	IntEffectSet
	// IntEffectAdd adds a signed delta to the integer fact (clamped to int16 range).
	IntEffectAdd
)

type intEffect struct {
	kind  IntEffectKind
	value int16
}

// Effects describes the boolean and integer fact changes produced when an action completes.
type Effects struct {
	boolMask [2]uint64
	boolVals [2]uint64
	intMask  uint16
	intOps   [MaxIntFacts]intEffect
}

// Empty reports whether e makes no changes to any fact.
func (e Effects) Empty() bool {
	return e.boolMask[0] == 0 && e.boolMask[1] == 0 && e.intMask == 0
}

// SetBool returns a copy of e that sets boolean fact f to v.
func (e Effects) SetBool(f BoolFact, v bool) Effects {
	if f >= MaxBoolFacts {
		return e
	}
	word := f >> 6
	bit := uint64(1) << (f & 63)
	e.boolMask[word] |= bit
	if v {
		e.boolVals[word] |= bit
	} else {
		e.boolVals[word] &^= bit
	}
	return e
}

// SetInt returns a copy of e that sets integer fact f to v (clamped to int16 range).
func (e Effects) SetInt(f IntFact, v int) Effects {
	if f >= MaxIntFacts {
		return e
	}
	e.intMask |= uint16(1) << f
	e.intOps[f] = intEffect{kind: IntEffectSet, value: clampInt16(v)}
	return e
}

// AddInt returns a copy of e that adds delta to integer fact f. A zero delta clears any effect on f.
func (e Effects) AddInt(f IntFact, delta int) Effects {
	if f >= MaxIntFacts {
		return e
	}
	if delta == 0 {
		e.intMask &^= uint16(1) << f
		e.intOps[f] = intEffect{}
		return e
	}
	e.intMask |= uint16(1) << f
	e.intOps[f] = intEffect{kind: IntEffectAdd, value: clampInt16(delta)}
	return e
}

// Apply returns a copy of w with all boolean and integer effects in e applied.
func (e Effects) Apply(w WorldState) WorldState {
	w.bools[0] = (w.bools[0] &^ e.boolMask[0]) | e.boolVals[0]
	w.bools[1] = (w.bools[1] &^ e.boolMask[1]) | e.boolVals[1]
	for m := e.intMask; m != 0; m &= m - 1 {
		i := bits.TrailingZeros16(m)
		op := e.intOps[i]
		switch op.kind {
		case IntEffectSet:
			w.ints[i] = op.value
		case IntEffectAdd:
			w.ints[i] = clampInt16(int(w.ints[i]) + int(op.value))
		}
	}
	return w
}

func (e Effects) count() int {
	return bits.OnesCount64(e.boolMask[0]) + bits.OnesCount64(e.boolMask[1]) + bits.OnesCount16(e.intMask)
}

// GOAPAction is one planning operator in a GOAPPlanner.
type GOAPAction struct {
	// Name uniquely identifies the action within a planner.
	Name string
	// Preconditions must hold before the action can execute.
	Preconditions Conditions
	// Effects are applied to the world state when the action completes.
	Effects Effects
	// Cost is the positive planning cost of this action; 0 defaults to 1.
	Cost float64
	// Check is an optional procedural precondition evaluated against the current WorldState.
	// Returning false excludes the action from the current plan and invalidates an active plan step.
	Check func(WorldState) bool
}

// GOAPGoal is a desired world condition with a planning priority.
type GOAPGoal struct {
	// Name identifies the goal.
	Name string
	// Priority ranks competing goals; higher priority is attempted first, with ties broken in declaration order.
	Priority float64
	// Target is the world condition the planner tries to satisfy.
	Target Conditions
}

type goapSearchNode struct {
	state   Conditions
	g       float64
	h       float64
	f       float64
	parent  int32
	action  int32
	depth   int32
	heapPos int32
	closed  bool
}

type goapHeuristic struct {
	minBoolCost      [MaxBoolFacts][2]float64
	hasIntSet        [MaxIntFacts]bool
	minIntSet        [MaxIntFacts]int16
	maxIntSet        [MaxIntFacts]int16
	minPosUnitCost   [MaxIntFacts]float64
	minPosActionCost [MaxIntFacts]float64
	minNegUnitCost   [MaxIntFacts]float64
	minNegActionCost [MaxIntFacts]float64
	maxEffects       float64
}

var infBoolCosts = func() [MaxBoolFacts][2]float64 {
	var t [MaxBoolFacts][2]float64
	inf := math.Inf(1)
	for i := range t {
		t[i][0] = inf
		t[i][1] = inf
	}
	return t
}()

var infIntCosts = func() [MaxIntFacts]float64 {
	var t [MaxIntFacts]float64
	inf := math.Inf(1)
	for i := range t {
		t[i] = inf
	}
	return t
}()

// GOAPPlanner performs deterministic regressive A* search from a goal condition back to the current WorldState.
//
// Ties break in a fixed order (lowest f, lowest h, fewest steps, lowest discovery index in fixed action declaration order).
// Internal search buffers are reused across calls, making repeated PlanInto calls zero-allocation after warm-up.
// A GOAPPlanner is not safe for concurrent use by multiple goroutines.
type GOAPPlanner struct {
	// NodeBudget caps how many search nodes regressive A* may expand per Plan call.
	// Zero or negative uses DefaultGOAPNodeBudget (1024).
	NodeBudget int

	actions       []GOAPAction
	costs         []float64
	effectCounts  []int
	intSetActions [MaxIntFacts][]int

	// Reusable per-plan buffers.
	usable    []bool
	goalOrder []int
	nodes     []goapSearchNode
	open      []int32
	buckets   []int32
	bucketGen []uint32
	gen       uint32
	heur      goapHeuristic
}

// NewGOAPPlanner validates actions and constructs a GOAPPlanner with defensive copies of actions.
// Each action must have a unique non-empty Name and a finite non-negative Cost (0 defaults to 1).
func NewGOAPPlanner(actions ...GOAPAction) (*GOAPPlanner, error) {
	seen := make(map[string]bool, len(actions))
	cloned := make([]GOAPAction, len(actions))
	costs := make([]float64, len(actions))
	effectCounts := make([]int, len(actions))
	var intSetActions [MaxIntFacts][]int

	for i, a := range actions {
		if a.Name == "" {
			return nil, fmt.Errorf("ai: GOAP action %d has no name", i)
		}
		if seen[a.Name] {
			return nil, fmt.Errorf("ai: GOAP action %q is defined twice", a.Name)
		}
		seen[a.Name] = true
		if !isFiniteFloat64(a.Cost) || a.Cost < 0 {
			return nil, fmt.Errorf("ai: GOAP action %q cost %v must be finite and non-negative", a.Name, a.Cost)
		}
		cost := a.Cost
		if cost == 0 {
			cost = 1
		}
		cloned[i] = a
		costs[i] = cost
		effectCounts[i] = a.Effects.count()
		for m := a.Effects.intMask; m != 0; m &= m - 1 {
			fi := bits.TrailingZeros16(m)
			if a.Effects.intOps[fi].kind == IntEffectSet {
				intSetActions[fi] = append(intSetActions[fi], i)
			}
		}
	}

	const initialBuckets = 128
	return &GOAPPlanner{
		NodeBudget:    DefaultGOAPNodeBudget,
		actions:       cloned,
		costs:         costs,
		effectCounts:  effectCounts,
		intSetActions: intSetActions,
		usable:        make([]bool, len(cloned)),
		nodes:         make([]goapSearchNode, 0, 64),
		open:          make([]int32, 0, 64),
		buckets:       make([]int32, initialBuckets),
		bucketGen:     make([]uint32, initialBuckets),
	}, nil
}

// ActionCount returns the number of actions configured on p.
func (p *GOAPPlanner) ActionCount() int {
	if p == nil {
		return 0
	}
	return len(p.actions)
}

// Action returns a copy of the action at index, or the zero GOAPAction if index is out of bounds.
func (p *GOAPPlanner) Action(index int) GOAPAction {
	if p == nil || index < 0 || index >= len(p.actions) {
		return GOAPAction{}
	}
	return p.actions[index]
}

// ActionName returns the Name of the action at index, or "" if index is out of bounds.
func (p *GOAPPlanner) ActionName(index int) string {
	if p == nil || index < 0 || index >= len(p.actions) {
		return ""
	}
	return p.actions[index].Name
}

// ActionIndex returns the index of the action named name, or -1 if none matches.
func (p *GOAPPlanner) ActionIndex(name string) int {
	if p == nil {
		return -1
	}
	for i := range p.actions {
		if p.actions[i].Name == name {
			return i
		}
	}
	return -1
}

// Plan finds the minimum-cost sequence of action indices that reaches goal from start.
// If start already satisfies goal.Target, Plan returns an empty slice, cost 0, and nil error.
func (p *GOAPPlanner) Plan(start WorldState, goal GOAPGoal) (plan []int, cost float64, err error) {
	return p.PlanInto(nil, start, goal)
}

// PlanInto behaves like Plan but writes the resulting action indices into dst[:0], avoiding allocations when dst has enough capacity.
func (p *GOAPPlanner) PlanInto(dst []int, start WorldState, goal GOAPGoal) (plan []int, cost float64, err error) {
	return p.planWithExcluded(dst, start, goal, nil)
}

// PlanNames is a convenience wrapper around Plan that returns the planned action names in execution order.
func (p *GOAPPlanner) PlanNames(start WorldState, goal GOAPGoal) (names []string, cost float64, err error) {
	indices, cost, err := p.Plan(start, goal)
	if err != nil {
		return nil, 0, err
	}
	names = make([]string, len(indices))
	for i, idx := range indices {
		names[i] = p.actions[idx].Name
	}
	return names, cost, nil
}

// PlanBestGoal tries unsatisfied goals in descending Priority order (ties broken in declaration order)
// and returns the first achievable goal's index, action sequence, and cost.
// If every goal is already satisfied by start, it returns (-1, nil, 0, nil).
func (p *GOAPPlanner) PlanBestGoal(start WorldState, goals []GOAPGoal) (goalIndex int, plan []int, cost float64, err error) {
	return p.PlanBestGoalInto(nil, start, goals)
}

// PlanBestGoalInto behaves like PlanBestGoal while writing the resulting action indices into dst[:0].
func (p *GOAPPlanner) PlanBestGoalInto(dst []int, start WorldState, goals []GOAPGoal) (goalIndex int, plan []int, cost float64, err error) {
	return p.planBestGoalWithExcluded(dst, start, goals, nil)
}

func (p *GOAPPlanner) sortGoalOrder(goals []GOAPGoal) []int {
	n := len(goals)
	if cap(p.goalOrder) < n {
		p.goalOrder = make([]int, n)
	} else {
		p.goalOrder = p.goalOrder[:n]
	}
	for i := range n {
		p.goalOrder[i] = i
	}
	// Insertion sort keeps allocation count at zero and preserves declaration order on equal Priority.
	for i := 1; i < n; i++ {
		idx := p.goalOrder[i]
		prio := goals[idx].Priority
		j := i - 1
		for j >= 0 && goals[p.goalOrder[j]].Priority < prio {
			p.goalOrder[j+1] = p.goalOrder[j]
			j--
		}
		p.goalOrder[j+1] = idx
	}
	return p.goalOrder
}

func (p *GOAPPlanner) planBestGoalWithExcluded(dst []int, start WorldState, goals []GOAPGoal, excluded []bool) (int, []int, float64, error) {
	if p == nil {
		return -1, dst[:0], 0, ErrNoPlan
	}
	if len(goals) == 0 {
		return -1, dst[:0], 0, ErrNoPlan
	}
	order := p.sortGoalOrder(goals)
	unsatisfied := false
	var lastErr error
	for _, gi := range order {
		g := goals[gi]
		if g.Target.SatisfiedBy(start) {
			continue
		}
		unsatisfied = true
		plan, cost, err := p.planWithExcluded(dst, start, g, excluded)
		if err == nil {
			return gi, plan, cost, nil
		}
		if err == ErrBudgetExceeded || lastErr == nil {
			lastErr = err
		}
	}
	if !unsatisfied {
		return -1, dst[:0], 0, nil
	}
	if lastErr == nil {
		lastErr = ErrNoPlan
	}
	return -1, dst[:0], 0, lastErr
}

func (p *GOAPPlanner) planWithExcluded(dst []int, start WorldState, goal GOAPGoal, excluded []bool) ([]int, float64, error) {
	if p == nil {
		return dst[:0], 0, ErrNoPlan
	}
	if goal.Target.isUnsatisfiable() {
		return dst[:0], 0, ErrNoPlan
	}
	if goal.Target.SatisfiedBy(start) {
		return dst[:0], 0, nil
	}
	if len(p.actions) == 0 {
		return dst[:0], 0, ErrNoPlan
	}

	budget := p.NodeBudget
	if budget <= 0 {
		budget = DefaultGOAPNodeBudget
	}

	if !p.prepareUsableAndHeuristic(start, excluded) {
		return dst[:0], 0, ErrNoPlan
	}

	h0, reachable := p.evalHeuristic(goal.Target, start)
	if !reachable {
		return dst[:0], 0, ErrNoPlan
	}

	p.resetSearch()
	rootID := p.addNode(goapSearchNode{
		state:   goal.Target,
		g:       0,
		h:       h0,
		f:       h0,
		parent:  -1,
		action:  -1,
		depth:   0,
		heapPos: -1,
	})
	p.heapPush(rootID)

	expanded := 0
	for len(p.open) > 0 {
		curID := p.heapPop()
		cur := p.nodes[curID]
		if cur.state.SatisfiedBy(start) {
			out := dst[:0]
			for id := curID; p.nodes[id].parent != -1; id = p.nodes[id].parent {
				out = append(out, int(p.nodes[id].action))
			}
			return out, cur.g, nil
		}
		if expanded >= budget {
			return dst[:0], 0, ErrBudgetExceeded
		}
		p.nodes[curID].closed = true
		expanded++

		for ai := range p.actions {
			if !p.usable[ai] {
				continue
			}
			nextState, ok := p.regress(cur.state, &p.actions[ai], start)
			if !ok {
				continue
			}
			ng := cur.g + p.costs[ai]
			ndepth := cur.depth + 1

			if existingID, found := p.lookupState(nextState); found {
				ex := &p.nodes[existingID]
				if ng < ex.g || (ng == ex.g && ndepth < ex.depth) {
					ex.g = ng
					ex.f = ng + ex.h
					ex.parent = curID
					ex.action = int32(ai)
					ex.depth = ndepth
					if ex.heapPos >= 0 {
						p.siftUp(int(ex.heapPos))
					} else {
						ex.closed = false
						p.heapPush(existingID)
					}
				}
				continue
			}

			nh, ok := p.evalHeuristic(nextState, start)
			if !ok {
				continue
			}
			nid := p.addNode(goapSearchNode{
				state:   nextState,
				g:       ng,
				h:       nh,
				f:       ng + nh,
				parent:  curID,
				action:  int32(ai),
				depth:   ndepth,
				heapPos: -1,
			})
			p.heapPush(nid)
		}
	}

	return dst[:0], 0, ErrNoPlan
}

func (p *GOAPPlanner) prepareUsableAndHeuristic(start WorldState, excluded []bool) bool {
	h := &p.heur
	h.minBoolCost = infBoolCosts
	h.hasIntSet = [MaxIntFacts]bool{}
	h.minIntSet = [MaxIntFacts]int16{}
	h.maxIntSet = [MaxIntFacts]int16{}
	h.minPosUnitCost = infIntCosts
	h.minPosActionCost = infIntCosts
	h.minNegUnitCost = infIntCosts
	h.minNegActionCost = infIntCosts
	maxEff := 1
	anyUsable := false

	for i := range p.actions {
		if i < len(excluded) && excluded[i] {
			p.usable[i] = false
			continue
		}
		a := &p.actions[i]
		if a.Effects.Empty() || a.Preconditions.isUnsatisfiable() {
			p.usable[i] = false
			continue
		}
		if a.Check != nil && !a.Check(start) {
			p.usable[i] = false
			continue
		}
		p.usable[i] = true
		anyUsable = true

		cost := p.costs[i]
		if ec := p.effectCounts[i]; ec > maxEff {
			maxEff = ec
		}

		for word := range 2 {
			for m := a.Effects.boolMask[word]; m != 0; m &= m - 1 {
				bit := bits.TrailingZeros64(m)
				idx := word*64 + bit
				val := 0
				if (a.Effects.boolVals[word] & (uint64(1) << bit)) != 0 {
					val = 1
				}
				if cost < h.minBoolCost[idx][val] {
					h.minBoolCost[idx][val] = cost
				}
			}
		}

		for m := a.Effects.intMask; m != 0; m &= m - 1 {
			fi := bits.TrailingZeros16(m)
			op := a.Effects.intOps[fi]
			switch op.kind {
			case IntEffectSet:
				if !h.hasIntSet[fi] {
					h.hasIntSet[fi] = true
					h.minIntSet[fi] = op.value
					h.maxIntSet[fi] = op.value
				} else {
					if op.value < h.minIntSet[fi] {
						h.minIntSet[fi] = op.value
					}
					if op.value > h.maxIntSet[fi] {
						h.maxIntSet[fi] = op.value
					}
				}
			case IntEffectAdd:
				if op.value > 0 {
					unit := cost / float64(op.value)
					if unit < h.minPosUnitCost[fi] {
						h.minPosUnitCost[fi] = unit
					}
					if cost < h.minPosActionCost[fi] {
						h.minPosActionCost[fi] = cost
					}
				} else if op.value < 0 {
					unit := cost / float64(-int(op.value))
					if unit < h.minNegUnitCost[fi] {
						h.minNegUnitCost[fi] = unit
					}
					if cost < h.minNegActionCost[fi] {
						h.minNegActionCost[fi] = cost
					}
				}
			}
		}
	}

	h.maxEffects = float64(maxEff)
	return anyUsable
}

// evalHeuristic computes a provably admissible lower bound on the cost to satisfy req from start,
// returning (_, false) when any unsatisfied requirement has no usable achieving action.
func (p *GOAPPlanner) evalHeuristic(req Conditions, start WorldState) (float64, bool) {
	h := &p.heur
	var maxCost, sumCost float64

	for word := range 2 {
		diff := (start.bools[word] ^ req.boolVals[word]) & req.boolMask[word]
		for m := diff; m != 0; m &= m - 1 {
			bit := bits.TrailingZeros64(m)
			idx := word*64 + bit
			want := 0
			if (req.boolVals[word] & (uint64(1) << bit)) != 0 {
				want = 1
			}
			c := h.minBoolCost[idx][want]
			if math.IsInf(c, 1) {
				return 0, false
			}
			if c > maxCost {
				maxCost = c
			}
			sumCost += c
		}
	}

	for m := req.intMask; m != 0; m &= m - 1 {
		fi := bits.TrailingZeros16(m)
		b := req.intRange[fi]
		if b.lo > b.hi {
			return 0, false
		}
		sv := start.ints[fi]
		if sv >= b.lo && sv <= b.hi {
			continue
		}
		best := math.Inf(1)
		if h.hasIntSet[fi] {
			for _, ai := range p.intSetActions[fi] {
				if !p.usable[ai] {
					continue
				}
				v := p.actions[ai].Effects.intOps[fi].value
				if v >= b.lo && v <= b.hi && p.costs[ai] < best {
					best = p.costs[ai]
				}
			}
		}
		if sv < b.lo {
			if !math.IsInf(h.minPosUnitCost[fi], 1) {
				gap := float64(int(b.lo) - int(sv))
				addCost := max(h.minPosActionCost[fi], gap*h.minPosUnitCost[fi])
				if addCost < best {
					best = addCost
				}
			}
		} else {
			if !math.IsInf(h.minNegUnitCost[fi], 1) {
				gap := float64(int(sv) - int(b.hi))
				addCost := max(h.minNegActionCost[fi], gap*h.minNegUnitCost[fi])
				if addCost < best {
					best = addCost
				}
			}
		}
		if math.IsInf(best, 1) {
			return 0, false
		}
		if best > maxCost {
			maxCost = best
		}
		sumCost += best
	}

	return max(maxCost, sumCost/h.maxEffects), true
}

// regress applies action a backwards from subgoal req.
// It returns (next, true) only if a contributes to at least one requirement in req, does not conflict with any other requirement in req, and its preconditions are consistent with the remaining requirements.
func (p *GOAPPlanner) regress(req Conditions, a *GOAPAction, start WorldState) (Conditions, bool) {
	eff := a.Effects
	pre := a.Preconditions
	relevant := false

	// 1. Boolean conflict check and relevance check.
	var remBoolMask, remBoolVals [2]uint64
	for word := range 2 {
		overlap := req.boolMask[word] & eff.boolMask[word]
		if overlap != 0 {
			if (req.boolVals[word] & overlap) != (eff.boolVals[word] & overlap) {
				return Conditions{}, false
			}
			relevant = true
		}
		remBoolMask[word] = req.boolMask[word] &^ eff.boolMask[word]
		remBoolVals[word] = req.boolVals[word] & remBoolMask[word]
	}

	// 2. Integer conflict, regression, and relevance check.
	var remIntMask uint16
	var remIntRange [MaxIntFacts]intBounds
	for m := req.intMask; m != 0; m &= m - 1 {
		fi := bits.TrailingZeros16(m)
		bit := uint16(1) << fi
		b := req.intRange[fi]
		if eff.intMask&bit == 0 {
			remIntMask |= bit
			remIntRange[fi] = b
			continue
		}
		op := eff.intOps[fi]
		switch op.kind {
		case IntEffectSet:
			if op.value < b.lo || op.value > b.hi {
				return Conditions{}, false
			}
			relevant = true
			// Satisfied completely by SetInt; clear bit from remIntMask.
		case IntEffectAdd:
			delta := int32(op.value)
			var newLo int16
			if b.lo == math.MinInt16 {
				newLo = math.MinInt16
			} else {
				l := int32(b.lo) - delta
				if l > math.MaxInt16 {
					return Conditions{}, false
				}
				if l < math.MinInt16 {
					l = math.MinInt16
				}
				newLo = int16(l)
			}
			var newHi int16
			if b.hi == math.MaxInt16 {
				newHi = math.MaxInt16
			} else {
				h := int32(b.hi) - delta
				if h < math.MinInt16 {
					return Conditions{}, false
				}
				if h > math.MaxInt16 {
					h = math.MaxInt16
				}
				newHi = int16(h)
			}
			if newLo > newHi {
				return Conditions{}, false
			}
			if delta > 0 && b.lo > math.MinInt16 {
				if start.ints[fi] < b.lo || (p.heur.hasIntSet[fi] && p.heur.minIntSet[fi] < b.lo) {
					relevant = true
				}
			} else if delta < 0 && b.hi < math.MaxInt16 {
				if start.ints[fi] > b.hi || (p.heur.hasIntSet[fi] && p.heur.maxIntSet[fi] > b.hi) {
					relevant = true
				}
			}
			if newLo != math.MinInt16 || newHi != math.MaxInt16 {
				remIntMask |= bit
				remIntRange[fi] = intBounds{lo: newLo, hi: newHi}
			}
		default:
			remIntMask |= bit
			remIntRange[fi] = b
		}
	}

	if !relevant {
		return Conditions{}, false
	}

	// 3. Merge boolean preconditions (check consistency).
	var next Conditions
	for word := range 2 {
		both := remBoolMask[word] & pre.boolMask[word]
		if both != 0 && (remBoolVals[word]&both) != (pre.boolVals[word]&both) {
			return Conditions{}, false
		}
		next.boolMask[word] = remBoolMask[word] | pre.boolMask[word]
		next.boolVals[word] = remBoolVals[word] | pre.boolVals[word]
	}

	// 4. Merge integer preconditions (intersect bounds).
	next.intMask = remIntMask
	next.intRange = remIntRange
	for m := pre.intMask; m != 0; m &= m - 1 {
		fi := bits.TrailingZeros16(m)
		bit := uint16(1) << fi
		pb := pre.intRange[fi]
		if pb.lo > pb.hi {
			return Conditions{}, false
		}
		if next.intMask&bit != 0 {
			lo := max(next.intRange[fi].lo, pb.lo)
			hi := min(next.intRange[fi].hi, pb.hi)
			if lo > hi {
				return Conditions{}, false
			}
			next.intRange[fi] = intBounds{lo: lo, hi: hi}
		} else {
			next.intMask |= bit
			next.intRange[fi] = pb
		}
	}

	if next == req {
		return Conditions{}, false
	}
	return next, true
}

func hashConditions(c Conditions) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	mix := func(v uint64) {
		h ^= v
		h *= prime64
	}
	mix(c.boolMask[0])
	mix(c.boolMask[1])
	mix(c.boolVals[0])
	mix(c.boolVals[1])
	mix(uint64(c.intMask))
	for m := c.intMask; m != 0; m &= m - 1 {
		fi := bits.TrailingZeros16(m)
		b := c.intRange[fi]
		mix(uint64(fi) | uint64(uint16(b.lo))<<16 | uint64(uint16(b.hi))<<32)
	}
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	return h
}

func (p *GOAPPlanner) resetSearch() {
	p.nodes = p.nodes[:0]
	p.open = p.open[:0]
	if len(p.buckets) == 0 {
		p.buckets = make([]int32, 128)
		p.bucketGen = make([]uint32, 128)
	}
	p.gen++
	if p.gen == 0 {
		clear(p.bucketGen)
		p.gen = 1
	}
}

func (p *GOAPPlanner) ensureBucketCapacity() {
	if len(p.nodes)*2 < len(p.buckets) {
		return
	}
	newCap := len(p.buckets) * 2
	if newCap < 128 {
		newCap = 128
	}
	p.buckets = make([]int32, newCap)
	p.bucketGen = make([]uint32, newCap)
	mask := uint64(newCap - 1)
	gen := p.gen
	for id := range p.nodes {
		slot := hashConditions(p.nodes[id].state) & mask
		for p.bucketGen[slot] == gen {
			slot = (slot + 1) & mask
		}
		p.bucketGen[slot] = gen
		p.buckets[slot] = int32(id)
	}
}

func (p *GOAPPlanner) lookupState(s Conditions) (int32, bool) {
	mask := uint64(len(p.buckets) - 1)
	gen := p.gen
	slot := hashConditions(s) & mask
	for p.bucketGen[slot] == gen {
		id := p.buckets[slot]
		if p.nodes[id].state == s {
			return id, true
		}
		slot = (slot + 1) & mask
	}
	return -1, false
}

func (p *GOAPPlanner) addNode(n goapSearchNode) int32 {
	p.ensureBucketCapacity()
	id := int32(len(p.nodes))
	p.nodes = append(p.nodes, n)
	mask := uint64(len(p.buckets) - 1)
	gen := p.gen
	slot := hashConditions(n.state) & mask
	for p.bucketGen[slot] == gen {
		slot = (slot + 1) & mask
	}
	p.bucketGen[slot] = gen
	p.buckets[slot] = id
	return id
}

// nodeLess enforces a strict deterministic total order: lowest f, then lowest h, then fewest actions, then lowest creation id.
func (p *GOAPPlanner) nodeLess(a, b int32) bool {
	na, nb := &p.nodes[a], &p.nodes[b]
	if na.f != nb.f {
		return na.f < nb.f
	}
	if na.h != nb.h {
		return na.h < nb.h
	}
	if na.depth != nb.depth {
		return na.depth < nb.depth
	}
	return a < b
}

func (p *GOAPPlanner) heapPush(id int32) {
	idx := len(p.open)
	p.open = append(p.open, id)
	p.nodes[id].heapPos = int32(idx)
	p.siftUp(idx)
}

func (p *GOAPPlanner) heapPop() int32 {
	top := p.open[0]
	p.nodes[top].heapPos = -1
	last := len(p.open) - 1
	if last == 0 {
		p.open = p.open[:0]
		return top
	}
	p.open[0] = p.open[last]
	p.nodes[p.open[0]].heapPos = 0
	p.open = p.open[:last]
	p.siftDown(0)
	return top
}

func (p *GOAPPlanner) siftUp(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if !p.nodeLess(p.open[i], p.open[parent]) {
			break
		}
		p.open[i], p.open[parent] = p.open[parent], p.open[i]
		p.nodes[p.open[i]].heapPos = int32(i)
		p.nodes[p.open[parent]].heapPos = int32(parent)
		i = parent
	}
}

func (p *GOAPPlanner) siftDown(i int) {
	n := len(p.open)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && p.nodeLess(p.open[r], p.open[l]) {
			best = r
		}
		if !p.nodeLess(p.open[best], p.open[i]) {
			break
		}
		p.open[i], p.open[best] = p.open[best], p.open[i]
		p.nodes[p.open[i]].heapPos = int32(i)
		p.nodes[p.open[best]].heapPos = int32(best)
		i = best
	}
}

// PlanRunner plans toward prioritized GOAPGoals and executes the resulting action sequence step by step.
//
// PlanRunner implements Node directly, so it can be ticked on game.Loop or embedded inside a behavior tree.
// When an action returns StatusFailure, or when the current WorldState invalidates the remaining plan or activates a higher-priority achievable goal, PlanRunner resets the running action node and replans automatically.
type PlanRunner struct {
	planner *GOAPPlanner
	world   func() WorldState
	goals   []GOAPGoal
	nodes   []Node

	activeGoal int
	plan       []int
	preemptBuf []int
	step       int
	running    bool
	failed     []bool
	plans      int
	lastErr    error
}

// NewPlanRunner creates a PlanRunner that plans using planner and snapshots from world,
// selects goals in descending Priority order, and executes each action using executors[action.Name].
func NewPlanRunner(planner *GOAPPlanner, world func() WorldState, goals []GOAPGoal, executors map[string]Node) *PlanRunner {
	var resolved []Node
	var failed []bool
	if planner != nil {
		resolved = make([]Node, len(planner.actions))
		failed = make([]bool, len(planner.actions))
		for i := range planner.actions {
			resolved[i] = executors[planner.actions[i].Name]
		}
	}
	return &PlanRunner{
		planner:    planner,
		world:      world,
		goals:      slices.Clone(goals),
		nodes:      resolved,
		activeGoal: -1,
		failed:     failed,
	}
}

// ActiveGoal returns the index into the runner's goals slice currently being pursued, or -1 if none.
func (r *PlanRunner) ActiveGoal() int {
	if r == nil {
		return -1
	}
	return r.activeGoal
}

// Step returns the index of the currently active step in the plan, or -1 if no plan is active.
func (r *PlanRunner) Step() int {
	if r == nil || len(r.plan) == 0 || r.step < 0 || r.step >= len(r.plan) {
		return -1
	}
	return r.step
}

// CurrentAction returns the Name of the action currently being executed, or "" if no plan is active.
func (r *PlanRunner) CurrentAction() string {
	if r == nil || r.planner == nil || len(r.plan) == 0 || r.step < 0 || r.step >= len(r.plan) {
		return ""
	}
	return r.planner.ActionName(r.plan[r.step])
}

// Plans returns the total number of plans computed since creation or the last Reset.
func (r *PlanRunner) Plans() int {
	if r == nil {
		return 0
	}
	return r.plans
}

// LastError returns the error from the most recent failed planning attempt, or nil if the last plan succeeded.
func (r *PlanRunner) LastError() error {
	if r == nil {
		return nil
	}
	return r.lastErr
}

// Reset cancels any in-progress plan, resets any running action node, and clears failure tracking.
func (r *PlanRunner) Reset() {
	if r == nil {
		return
	}
	r.resetRunningNode()
	r.plan = r.plan[:0]
	r.step = 0
	r.activeGoal = -1
	r.plans = 0
	r.lastErr = nil
	clear(r.failed)
}

func (r *PlanRunner) resetRunningNode() {
	if r.running && r.step >= 0 && r.step < len(r.plan) {
		if ch := r.nodes[r.plan[r.step]]; ch != nil {
			ch.Reset()
		}
	}
	r.running = false
}

// Tick advances the current plan by dt, replanning automatically on action failure,
// world-state invalidation, or higher-priority goal preemption.
func (r *PlanRunner) Tick(dt time.Duration) Status {
	if r == nil || r.planner == nil || r.world == nil || len(r.goals) == 0 {
		return StatusFailure
	}

	maxReplans := len(r.planner.actions) + 2
	replans := 0

	for {
		ws := r.world()

		if len(r.plan) > 0 && r.activeGoal >= 0 {
			// Active goal already achieved externally.
			if r.goals[r.activeGoal].Target.SatisfiedBy(ws) {
				r.resetRunningNode()
				r.plan = r.plan[:0]
				r.step = 0
				r.activeGoal = -1
				clear(r.failed)
				r.lastErr = nil
				return StatusSuccess
			}

			// Check if a higher-precedence goal is now unsatisfied and achievable.
			if r.maybePreempt(ws) {
				replans++
				if replans > maxReplans {
					r.resetRunningNode()
					r.plan = r.plan[:0]
					r.step = 0
					r.activeGoal = -1
					clear(r.failed)
					return StatusFailure
				}
			} else if !r.planValid(ws) {
				r.resetRunningNode()
				r.plan = r.plan[:0]
				r.step = 0
				r.activeGoal = -1
				clear(r.failed)
			}
		}

		if len(r.plan) == 0 {
			if replans > maxReplans {
				clear(r.failed)
				return StatusFailure
			}
			gi, plan, _, err := r.planner.planBestGoalWithExcluded(r.plan[:0], ws, r.goals, r.failed)
			if err != nil {
				r.lastErr = err
				clear(r.failed)
				return StatusFailure
			}
			r.lastErr = nil
			if gi < 0 || len(plan) == 0 {
				clear(r.failed)
				return StatusSuccess
			}
			r.activeGoal = gi
			r.plan = plan
			r.step = 0
			r.running = false
			r.plans++
		}

		actIdx := r.plan[r.step]
		node := r.nodes[actIdx]
		if node == nil {
			r.failed[actIdx] = true
			r.plan = r.plan[:0]
			r.step = 0
			r.activeGoal = -1
			replans++
			if replans > maxReplans {
				clear(r.failed)
				r.lastErr = ErrNoPlan
				return StatusFailure
			}
			continue
		}

		st := node.Tick(dt)
		switch st {
		case StatusRunning:
			r.running = true
			return StatusRunning

		case StatusSuccess:
			r.running = false
			clear(r.failed)
			r.step++
			if r.step >= len(r.plan) {
				goalIdx := r.activeGoal
				r.plan = r.plan[:0]
				r.step = 0
				r.activeGoal = -1
				if r.goals[goalIdx].Target.SatisfiedBy(r.world()) {
					return StatusSuccess
				}
				replans++
				if replans > maxReplans {
					r.lastErr = ErrNoPlan
					return StatusFailure
				}
			}

		default: // StatusFailure
			r.running = false
			node.Reset()
			r.failed[actIdx] = true
			r.plan = r.plan[:0]
			r.step = 0
			r.activeGoal = -1
			replans++
			if replans > maxReplans {
				clear(r.failed)
				return StatusFailure
			}
		}
	}
}

func (r *PlanRunner) maybePreempt(ws WorldState) bool {
	order := r.planner.sortGoalOrder(r.goals)
	for _, gi := range order {
		if gi == r.activeGoal {
			return false
		}
		if r.goals[gi].Target.SatisfiedBy(ws) {
			continue
		}
		plan, _, err := r.planner.planWithExcluded(r.preemptBuf[:0], ws, r.goals[gi], nil)
		if err != nil {
			continue
		}
		r.resetRunningNode()
		r.preemptBuf = r.plan[:0]
		r.plan = plan
		r.activeGoal = gi
		r.step = 0
		r.plans++
		r.lastErr = nil
		clear(r.failed)
		return true
	}
	return false
}

func (r *PlanRunner) planValid(ws WorldState) bool {
	if r.step < 0 || r.step >= len(r.plan) {
		return false
	}
	sim := ws
	for k := r.step; k < len(r.plan); k++ {
		idx := r.plan[k]
		if idx < 0 || idx >= len(r.planner.actions) {
			return false
		}
		if r.failed[idx] || r.nodes[idx] == nil {
			return false
		}
		a := &r.planner.actions[idx]
		if !a.Preconditions.SatisfiedBy(sim) {
			return false
		}
		if a.Check != nil && !a.Check(ws) {
			return false
		}
		sim = a.Effects.Apply(sim)
	}
	return r.goals[r.activeGoal].Target.SatisfiedBy(sim)
}
