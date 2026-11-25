package ai

import (
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai/internal/sense"
	"github.com/nsgo-dev/nsgo/core"
)

// LineOfSight reports whether nothing blocks the segment from an agent's eye at from to a target at to. PhysicsLineOfSight adapts a physics world and lights2d.LineOfSight adapts lighting occluders; a game may supply its own, and it must be deterministic for perception to be.
type LineOfSight func(from, to core.Point) bool

// Target is a stimulus that agents can see during one step.
type Target struct {
	// ID identifies the target; a later target with the same ID in one step is ignored.
	ID StimulusID
	// Team is the target's team, compared with each agent's team by the system's AttitudeFunc.
	Team Team
	// Pos is the point agents look at.
	Pos core.Point
	// Vel is the target's velocity, remembered while it is seen.
	Vel core.Point
}

// Noise is a sound posted to a PerceptionSystem and heard on its next Step.
type Noise struct {
	// Source identifies what made the noise; the memory of it is kept under this ID.
	Source StimulusID
	// Team is the source's team.
	Team Team
	// Pos is where the noise was made.
	Pos core.Point
	// Loudness scales each listener's hearing range: a noise of loudness 2 is heard twice as far as one of loudness 1. Zero or less is inaudible.
	Loudness float32
}

// Memory is what an agent remembers about one stimulus.
type Memory struct {
	// ID is the stimulus's ID.
	ID StimulusID
	// Team is the stimulus's team when it was last sensed.
	Team Team
	// Attitude is how the agent regarded the stimulus when it was last sensed.
	Attitude Attitude
	// Sense is the sense that last refreshed the memory.
	Sense Sense
	// Pos is the last known position.
	Pos core.Point
	// Vel is the last known velocity: the target's velocity when seen, zero when heard.
	Vel core.Point
	// Age is the time since the stimulus was last sensed; zero while it is in sight.
	Age time.Duration
	// Strength is 1 when seen, and hearing raises it to at least 1 - distance/reach (1 for an infinite reach); it falls linearly to 0 over ForgetAfter; it ranks memories by how fresh and certain they are.
	Strength float32
	// Visible reports whether the stimulus is in sight now.
	Visible bool
}

// Event reports a change to one memory during the last step.
type Event struct {
	// Kind is what happened.
	Kind EventKind
	// Memory is the memory as the event left it; for EventForgotten it is the memory that was dropped.
	Memory Memory
}

// Perception is one agent's senses and memory. Set its exported fields, add it to a PerceptionSystem, and step the system on game.Loop's fixed steps; the system reads the fields at the start of every Step, so a game moves and turns the agent by changing Pos and Facing.
type Perception struct {
	// Self is the agent's own StimulusID, so the agent never senses itself; zero means the agent is not a stimulus, so an agent that is one needs a non-zero ID.
	Self StimulusID
	// Team is the agent's team.
	Team Team
	// Pos is the agent's position.
	Pos core.Point
	// Facing is the direction the sight cone points along; its length does not matter, and zero faces +X.
	Facing core.Point
	// EyeOffset is added to Pos, in world space, to give the point both senses measure from and sight looks from.
	EyeOffset core.Point
	// Sight is the agent's vision.
	Sight SightConfig
	// Hearing is the agent's hearing.
	Hearing HearingConfig
	// ForgetAfter is how long a stimulus is remembered after it was last sensed; zero or less forgets it on the step after.
	ForgetAfter time.Duration

	state  sense.State
	events []Event
}

// Memories returns a new slice of every memory, sorted by ID.
func (p *Perception) Memories() []Memory {
	return p.AppendMemories(nil)
}

// AppendMemories appends every memory, sorted by ID, to dst and returns the extended slice, so a caller can reuse one buffer every step.
func (p *Perception) AppendMemories(dst []Memory) []Memory {
	for i := range p.state.Mems {
		dst = append(dst, p.memory(&p.state.Mems[i]))
	}
	return dst
}

// Memory returns the memory of the stimulus id, if any.
func (p *Perception) Memory(id StimulusID) (Memory, bool) {
	i, ok := p.state.Find(uint64(id))
	if !ok {
		return Memory{}, false
	}
	return p.memory(&p.state.Mems[i]), true
}

// Sees reports whether the stimulus id is in sight now.
func (p *Perception) Sees(id StimulusID) bool {
	i, ok := p.state.Find(uint64(id))
	return ok && p.state.Mems[i].Visible
}

// Events returns the events of the last Step, in order: per memory as sight, hearing and forgetting reached it. The slice is reused by the next Step.
func (p *Perception) Events() []Event {
	return p.events
}

// Forget drops the memory of id without an event and reports whether there was one.
func (p *Perception) Forget(id StimulusID) bool {
	return p.state.Forget(uint64(id))
}

// Clear drops every memory and the last step's events.
func (p *Perception) Clear() {
	p.state.Clear()
	p.events = p.events[:0]
}

func (p *Perception) memory(m *sense.Memory) Memory {
	return Memory{
		ID: StimulusID(m.ID), Team: Team(m.Team), Attitude: Attitude(m.Attitude), Sense: Sense(m.Sense),
		Pos: toPoint2(m.Pos), Vel: toPoint2(m.Vel),
		Age: m.Age, Strength: m.Strength(p.ForgetAfter), Visible: m.Visible,
	}
}

func (p *Perception) input() sense.Agent {
	return sense.Agent{
		State:      &p.state,
		Self:       uint64(p.Self),
		HasSelf:    p.Self != 0,
		Team:       uint8(p.Team),
		Eye:        fromPoint2(p.Pos).Add(fromPoint2(p.EyeOffset)),
		Fwd:        fromPoint2(p.Facing).Norm(sense.Vec{X: 1}),
		Sight:      p.Sight.resolve(),
		HearRange:  float64(p.Hearing.Range),
		HearDetect: uint8(p.Hearing.Detect),
		Forget:     p.ForgetAfter,
	}
}

// PerceptionSystem steps the Perceptions of many agents together, so the line-of-sight checks of all of them share one per-step Budget. The zero PerceptionSystem is ready to use, with every line of sight clear.
//
// Step it once per fixed step of game.Loop. Agents are served in the order they were added and targets in ID order, so a run is identical every time given the same inputs.
type PerceptionSystem struct {
	// LineOfSight decides whether a target in view is seen; nil means nothing blocks sight.
	LineOfSight LineOfSight
	// Attitude decides how agents regard targets and noises; nil means DefaultAttitude.
	Attitude AttitudeFunc
	// Budget caps the LineOfSight calls of one step; zero or less checks every pair in view, and a nil LineOfSight makes no calls, so the budget does not apply and every pair in view is seen at once. When the cap is reached, the remaining pairs are checked first on the next step, round-robin, and keep their previous result until then.
	Budget int

	agents []*Perception
	eng    sense.Engine
}

// Add adds p to the system, after the agents already in it. Adding p again does nothing; adding a Perception that belongs to another system panics.
func (s *PerceptionSystem) Add(p *Perception) {
	if s.eng.Register(&p.state, s) {
		s.agents = append(s.agents, p)
	}
}

// Remove removes p from the system, keeping its memories, and reports whether it was there.
func (s *PerceptionSystem) Remove(p *Perception) bool {
	i := slices.Index(s.agents, p)
	if i < 0 {
		return false
	}
	s.agents = slices.Delete(s.agents, i, i+1)
	p.state.Release()
	return true
}

// Len returns the number of agents in the system.
func (s *PerceptionSystem) Len() int {
	return len(s.agents)
}

// PostNoise queues n to be heard on the next Step. Noises are heard in the order they were posted.
func (s *PerceptionSystem) PostNoise(n Noise) {
	s.eng.PostNoise(sense.Noise{ID: uint64(n.Source), Team: uint8(n.Team), Pos: fromPoint2(n.Pos), Loudness: float64(n.Loudness)})
}

// Step advances every agent by dt: memories age, targets in view are checked for line of sight within the budget, posted noises are heard, and memories not sensed for ForgetAfter are forgotten. Each agent's Events then report what changed. targets is read only during the call.
func (s *PerceptionSystem) Step(dt time.Duration, targets []Target) {
	s.eng.Agents = s.eng.Agents[:0]
	for _, p := range s.agents {
		s.eng.Agents = append(s.eng.Agents, p.input())
	}
	s.eng.SetTargets(len(targets), func(i int) sense.Target {
		t := &targets[i]
		return sense.Target{ID: uint64(t.ID), Team: uint8(t.Team), Pos: fromPoint2(t.Pos), Vel: fromPoint2(t.Vel)}
	})
	var los func(from, to sense.Vec) bool
	if s.LineOfSight != nil {
		los = func(from, to sense.Vec) bool { return s.LineOfSight(toPoint2(from), toPoint2(to)) }
	}
	s.eng.Step(dt, s.Budget, s.Attitude.codes(), los)
	for _, p := range s.agents {
		p.events = p.events[:0]
		for i := range p.state.Events {
			ev := &p.state.Events[i]
			p.events = append(p.events, Event{Kind: EventKind(ev.Kind), Memory: p.memory(&ev.Memory)})
		}
	}
	clear(s.eng.Agents)
}

// Stats returns the line-of-sight work of the last Step.
func (s *PerceptionSystem) Stats() PerceptionStats {
	return PerceptionStats(s.eng.Stats)
}

func fromPoint2(p core.Point) sense.Vec { return sense.Vec{X: float64(p.X), Y: float64(p.Y)} }

func toPoint2(v sense.Vec) core.Point { return core.Point{X: core.Points(v.X), Y: core.Points(v.Y)} }
