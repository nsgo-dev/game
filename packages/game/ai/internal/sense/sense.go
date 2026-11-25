// Package sense is the perception engine behind ai.PerceptionSystem and perception3d.System: sight cones, a round-robin line-of-sight budget, hearing, and per-stimulus memory, all on float64 3D vectors so the 2D and 3D front ends share one implementation.
//
// Its numeric codes match the public types in package ai: attitudes are 0 hostile, 1 neutral and 2 friendly; senses are 1 sight and 2 hearing; event kinds are 1 gained, 2 updated, 3 lost and 4 forgotten.
package sense

import (
	"math"
	"slices"
	"sort"
	"time"
)

// Attitude codes.
const (
	Hostile  uint8 = 0
	Neutral  uint8 = 1
	Friendly uint8 = 2
)

// Sense codes.
const (
	BySight   uint8 = 1
	ByHearing uint8 = 2
)

// Event kind codes.
const (
	Gained    uint8 = 1
	Updated   uint8 = 2
	Lost      uint8 = 3
	Forgotten uint8 = 4
)

// Detects reports whether mask detects attitude, treating a zero mask as hostile only.
func Detects(mask, attitude uint8) bool {
	if mask == 0 {
		mask = 1 << Hostile
	}
	return attitude < 8 && mask&(1<<attitude) != 0
}

// Vec is a 3D vector; 2D callers leave Z zero.
type Vec struct{ X, Y, Z float64 }

// Sub returns a - b.
func (a Vec) Sub(b Vec) Vec { return Vec{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// Add returns a + b.
func (a Vec) Add(b Vec) Vec { return Vec{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

func (a Vec) dot(b Vec) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// finite returns a, or the zero vector when any component is NaN or infinite, so a bad velocity never makes a still target look changed on every step.
func (a Vec) finite() Vec {
	if math.IsNaN(a.X+a.Y+a.Z) || math.IsInf(a.X+a.Y+a.Z, 0) {
		return Vec{}
	}
	return a
}

// Norm returns a scaled to unit length, or fallback when a has no finite non-zero length.
func (a Vec) Norm(fallback Vec) Vec {
	l := math.Sqrt(a.dot(a))
	if l == 0 || math.IsNaN(l) || math.IsInf(l, 0) {
		return fallback
	}
	return Vec{a.X / l, a.Y / l, a.Z / l}
}

// Memory is one remembered stimulus.
type Memory struct {
	ID       uint64
	Team     uint8
	Attitude uint8
	Sense    uint8
	Pos, Vel Vec
	Age      time.Duration
	Visible  bool

	base   float64
	sensed bool
}

// Strength is the memory's strength given the agent's forget time: its sensed strength falling linearly to 0 over forget.
func (m *Memory) Strength(forget time.Duration) float32 {
	s := m.base
	if m.Age > 0 {
		if forget <= 0 {
			return 0
		}
		s -= m.Age.Seconds() / forget.Seconds()
	}
	return float32(min(max(s, 0), 1))
}

// Event is a change to one memory during a step, with the memory as the change left it.
type Event struct {
	Kind   uint8
	Memory Memory
}

// State is one agent's memories, sorted by ID, and the events of its last step.
type State struct {
	Mems   []Memory
	Events []Event

	owner any
	seq   uint64
}

// Find returns the index of the memory of id, or where it would be inserted.
func (st *State) Find(id uint64) (int, bool) {
	return slices.BinarySearchFunc(st.Mems, id, func(m Memory, id uint64) int {
		switch {
		case m.ID < id:
			return -1
		case m.ID > id:
			return 1
		}
		return 0
	})
}

// Forget drops the memory of id and reports whether there was one.
func (st *State) Forget(id uint64) bool {
	i, ok := st.Find(id)
	if ok {
		st.Mems = slices.Delete(st.Mems, i, i+1)
	}
	return ok
}

// Clear drops every memory and event.
func (st *State) Clear() {
	st.Mems = st.Mems[:0]
	st.Events = st.Events[:0]
}

// Release detaches st from the system that owned it.
func (st *State) Release() { st.owner = nil }

// Sight is an agent's resolved vision.
type Sight struct {
	rangeSq, periphSq float64
	coneOn, coneFull  bool
	cosHalf           float64
	periphOn          bool
	periphFull        bool
	cosPeriph         float64
	detect            uint8
}

// NewSight resolves a sight configuration: a cone of halfAngle radians out to rng, and a peripheral cone of periphHalf radians (zero meaning all around) out to periphRange.
func NewSight(rng, halfAngle, periphRange, periphHalf float32, detect uint8) Sight {
	p := Sight{detect: detect}
	if r := float64(rng); r > 0 && halfAngle > 0 {
		p.rangeSq = r * r
		p.coneOn = true
		p.coneFull = float64(halfAngle) >= math.Pi
		p.cosHalf = math.Cos(float64(halfAngle))
	}
	if r := float64(periphRange); r > 0 && periphHalf >= 0 {
		p.periphSq = r * r
		p.periphOn = true
		h := float64(periphHalf)
		p.periphFull = h == 0 || h >= math.Pi
		p.cosPeriph = math.Cos(h)
	}
	return p
}

func (p *Sight) enabled() bool { return p.coneOn || p.periphOn }

// inView reports whether a point at offset d from the eye lies in the cone or the peripheral range around the unit direction fwd.
func (p *Sight) inView(fwd, d Vec) bool {
	dsq := d.dot(d)
	if math.IsNaN(dsq) || math.IsInf(dsq, 0) {
		return false
	}
	in := func(on, full bool, rangeSq, cosH float64) bool {
		if !on || dsq > rangeSq {
			return false
		}
		if full || dsq == 0 {
			return true
		}
		return fwd.dot(d) >= cosH*math.Sqrt(dsq)
	}
	return in(p.coneOn, p.coneFull, p.rangeSq, p.cosHalf) || in(p.periphOn, p.periphFull, p.periphSq, p.cosPeriph)
}

// Agent is one agent's input to a step.
type Agent struct {
	State      *State
	Self       uint64
	HasSelf    bool
	Team       uint8
	Eye, Fwd   Vec
	Sight      Sight
	HearRange  float64
	HearDetect uint8
	Forget     time.Duration
}

// Target is a stimulus that can be seen during a step.
type Target struct {
	ID       uint64
	Team     uint8
	Pos, Vel Vec
}

// Noise is a stimulus heard on the next step.
type Noise struct {
	ID       uint64
	Team     uint8
	Pos      Vec
	Loudness float64
}

// Stats is the line-of-sight work of a step.
type Stats struct {
	Candidates, Checks, Deferred int
}

type pairKey struct {
	seq uint64
	id  uint64
}

func (k pairKey) less(o pairKey) bool {
	return k.seq < o.seq || (k.seq == o.seq && k.id < o.id)
}

type candidate struct {
	agent, target int
	attitude      uint8
	visible       bool
}

// Engine steps agents against targets and noises and owns the round-robin line-of-sight cursor. Fill Agents and call SetTargets before each Step.
type Engine struct {
	Agents []Agent
	Stats  Stats

	targets   []Target
	noises    []Noise
	cands     []candidate
	ranges    []int
	cursor    pairKey
	hasCursor bool
	nextSeq   uint64
}

// Register claims st for the system owner and gives it the next place in the agent order. It reports false when owner already has st and panics when another system does.
func (e *Engine) Register(st *State, owner any) bool {
	switch st.owner {
	case owner:
		return false
	case nil:
	default:
		panic("ai: a perception already belongs to another perception system")
	}
	st.owner = owner
	e.nextSeq++
	st.seq = e.nextSeq
	return true
}

// PostNoise queues n for the next Step.
func (e *Engine) PostNoise(n Noise) {
	e.noises = append(e.noises, n)
}

// SetTargets sets the step's targets from n calls to at, sorted by ID with the first of each ID kept.
func (e *Engine) SetTargets(n int, at func(i int) Target) {
	e.targets = e.targets[:0]
	for i := range n {
		e.targets = append(e.targets, at(i))
	}
	slices.SortStableFunc(e.targets, func(a, b Target) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	e.targets = slices.CompactFunc(e.targets, func(a, b Target) bool { return a.ID == b.ID })
}

// Step advances every agent by dt. attitude rates a team as seen from an observer's team; los reports a clear line of sight and may be nil.
func (e *Engine) Step(dt time.Duration, budget int, attitude func(observer, other uint8) uint8, los func(from, to Vec) bool) {
	if dt < 0 {
		dt = 0
	}
	for i := range e.Agents {
		st := e.Agents[i].State
		st.Events = st.Events[:0]
		for j := range st.Mems {
			st.Mems[j].Age += dt
			st.Mems[j].sensed = false
		}
	}

	// Sight: gather the pairs in view, in agent order and then target ID order.
	e.cands = e.cands[:0]
	e.ranges = e.ranges[:0]
	for ai := range e.Agents {
		a := &e.Agents[ai]
		e.ranges = append(e.ranges, len(e.cands))
		if !a.Sight.enabled() {
			continue
		}
		for ti := range e.targets {
			t := &e.targets[ti]
			if a.HasSelf && t.ID == a.Self {
				continue
			}
			att := attitude(a.Team, t.Team)
			if !Detects(a.Sight.detect, att) || !a.Sight.inView(a.Fwd, t.Pos.Sub(a.Eye)) {
				continue
			}
			e.cands = append(e.cands, candidate{agent: ai, target: ti, attitude: att})
		}
	}
	e.ranges = append(e.ranges, len(e.cands))
	e.checkLineOfSight(budget, los)

	for ai := range e.Agents {
		a := &e.Agents[ai]
		for _, c := range e.cands[e.ranges[ai]:e.ranges[ai+1]] {
			if c.visible {
				see(a, &e.targets[c.target], c.attitude)
			}
		}
		for j := range a.State.Mems {
			m := &a.State.Mems[j]
			if m.Visible && !m.sensed {
				m.Visible = false
				a.State.Events = append(a.State.Events, Event{Lost, *m})
			}
		}
	}

	// Hearing, in the order noises were posted.
	for ai := range e.Agents {
		a := &e.Agents[ai]
		if a.HearRange <= 0 {
			continue
		}
		for ni := range e.noises {
			hear(a, &e.noises[ni], attitude)
		}
	}
	e.noises = e.noises[:0]

	// Forgetting.
	for ai := range e.Agents {
		a := &e.Agents[ai]
		a.State.Mems = slices.DeleteFunc(a.State.Mems, func(m Memory) bool {
			if m.sensed || m.Visible || m.Age < a.Forget {
				return false
			}
			a.State.Events = append(a.State.Events, Event{Forgotten, m})
			return true
		})
	}
}

// checkLineOfSight resolves each candidate's visibility. A nil los sees every candidate without a check, so the budget does not apply. When a positive budget cannot cover every candidate, it checks budget of them round-robin, starting from the first pair after the cursor in (agent, target ID) order, and the rest keep the visibility they had; otherwise it checks all of them in order.
func (e *Engine) checkLineOfSight(budget int, los func(from, to Vec) bool) {
	n := len(e.cands)
	e.Stats = Stats{Candidates: n}
	if los == nil {
		for i := range e.cands {
			e.cands[i].visible = true
		}
		return
	}
	limited := budget > 0 && n > budget
	start := 0
	if limited && e.hasCursor {
		// Candidates are sorted by key, so the resume point is a binary search away.
		start = sort.Search(n, func(i int) bool { return e.cursor.less(e.key(i)) }) % n
	}
	for k := range n {
		i := (start + k) % n
		c := &e.cands[i]
		a := &e.Agents[c.agent]
		if limited && e.Stats.Checks == budget {
			if j, ok := a.State.Find(e.targets[c.target].ID); ok {
				c.visible = a.State.Mems[j].Visible
			}
			e.Stats.Deferred++
			continue
		}
		c.visible = los(a.Eye, e.targets[c.target].Pos)
		e.Stats.Checks++
		if limited {
			e.cursor, e.hasCursor = e.key(i), true
		}
	}
}

func (e *Engine) key(i int) pairKey {
	c := e.cands[i]
	return pairKey{e.Agents[c.agent].State.seq, e.targets[c.target].ID}
}

func see(a *Agent, t *Target, att uint8) {
	st := a.State
	vel := t.Vel.finite()
	i, ok := st.Find(t.ID)
	if !ok {
		m := Memory{ID: t.ID, Team: t.Team, Attitude: att, Sense: BySight, Pos: t.Pos, Vel: vel, Visible: true, base: 1, sensed: true}
		st.Mems = slices.Insert(st.Mems, i, m)
		st.Events = append(st.Events, Event{Gained, m})
		return
	}
	m := &st.Mems[i]
	changed := !m.Visible || m.Sense != BySight || m.Pos != t.Pos || m.Vel != vel || m.Team != t.Team || m.Attitude != att
	m.Team, m.Attitude, m.Sense = t.Team, att, BySight
	m.Pos, m.Vel = t.Pos, vel
	m.Age, m.base = 0, 1
	m.Visible, m.sensed = true, true
	if changed {
		st.Events = append(st.Events, Event{Updated, *m})
	}
}

func hear(a *Agent, n *Noise, attitude func(observer, other uint8) uint8) {
	if a.HasSelf && n.ID == a.Self {
		return
	}
	// An infinite reach hears every noise at a finite distance at full strength, as an infinite sight range sees everything.
	reach := a.HearRange * n.Loudness
	if !(reach > 0) {
		return
	}
	att := attitude(a.Team, n.Team)
	if !Detects(a.HearDetect, att) {
		return
	}
	d := n.Pos.Sub(a.Eye)
	dist := math.Sqrt(d.dot(d))
	if math.IsNaN(dist) || math.IsInf(dist, 0) || dist > reach {
		return
	}
	s := 1.0
	if !math.IsInf(reach, 1) {
		s = 1 - dist/reach
	}
	st := a.State
	i, ok := st.Find(n.ID)
	if !ok {
		m := Memory{ID: n.ID, Team: n.Team, Attitude: att, Sense: ByHearing, Pos: n.Pos, base: s, sensed: true}
		st.Mems = slices.Insert(st.Mems, i, m)
		st.Events = append(st.Events, Event{Gained, m})
		return
	}
	m := &st.Mems[i]
	if m.Visible {
		// Sight already places the stimulus exactly.
		return
	}
	m.base = max(float64(m.Strength(a.Forget)), s)
	m.Team, m.Attitude, m.Sense = n.Team, att, ByHearing
	m.Pos, m.Vel = n.Pos, Vec{}
	m.Age, m.sensed = 0, true
	st.Events = append(st.Events, Event{Updated, *m})
}
