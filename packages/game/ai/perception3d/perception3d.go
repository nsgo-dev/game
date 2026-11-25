// Package perception3d is ai's perception for agents in a 3D world: sight cones around a forward vector, hearing and per-stimulus memory on math3d.Vec3, with line of sight through physics3d raycasts or a game's own check.
//
// It shares ai's engine, so its sight, hearing, memory, events, teams, line-of-sight budget and determinism behave exactly as ai.PerceptionSystem's do, and it uses ai's StimulusID, Team, SightConfig, HearingConfig and event types.
package perception3d

import (
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/ai/internal/sense"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
)

// LineOfSight reports whether nothing blocks the segment from an agent's eye at from to a target at to. It must be deterministic for perception to be.
type LineOfSight func(from, to math3d.Vec3) bool

// PhysicsLineOfSight returns a LineOfSight that is blocked by the first hit of w.RaycastFirstLayer with layers; zero selects every layer.
//
// A ray starting inside a body hits it, and a ray to a target hits the target's own body, so keep agents' and targets' bodies off the layers that block sight.
func PhysicsLineOfSight(w *physics3d.World, layers physics3d.LayerMask) LineOfSight {
	if layers == 0 {
		layers = physics3d.LayerAll
	}
	return func(from, to math3d.Vec3) bool {
		_, hit := w.RaycastFirstLayer(physics3d.Vec3{X: from.X, Y: from.Y, Z: from.Z}, physics3d.Vec3{X: to.X, Y: to.Y, Z: to.Z}, layers)
		return !hit
	}
}

// Target is a stimulus that agents can see during one step.
type Target struct {
	// ID identifies the target; a later target with the same ID in one step is ignored.
	ID ai.StimulusID
	// Team is the target's team.
	Team ai.Team
	// Pos is the point agents look at.
	Pos math3d.Vec3
	// Vel is the target's velocity, remembered while it is seen.
	Vel math3d.Vec3
}

// Noise is a sound posted to a System and heard on its next Step.
type Noise struct {
	// Source identifies what made the noise; the memory of it is kept under this ID.
	Source ai.StimulusID
	// Team is the source's team.
	Team ai.Team
	// Pos is where the noise was made.
	Pos math3d.Vec3
	// Loudness scales each listener's hearing range; zero or less is inaudible.
	Loudness float32
}

// Memory is what an agent remembers about one stimulus; its fields mean what ai.Memory's do.
type Memory struct {
	// ID is the stimulus's ID.
	ID ai.StimulusID
	// Team is the stimulus's team when it was last sensed.
	Team ai.Team
	// Attitude is how the agent regarded the stimulus when it was last sensed.
	Attitude ai.Attitude
	// Sense is the sense that last refreshed the memory.
	Sense ai.Sense
	// Pos is the last known position.
	Pos math3d.Vec3
	// Vel is the last known velocity: the target's velocity when seen, zero when heard.
	Vel math3d.Vec3
	// Age is the time since the stimulus was last sensed; zero while it is in sight.
	Age time.Duration
	// Strength is 1 when seen and 1 - distance/reach when heard, and falls linearly to 0 over ForgetAfter.
	Strength float32
	// Visible reports whether the stimulus is in sight now.
	Visible bool
}

// Event reports a change to one memory during the last step.
type Event struct {
	// Kind is what happened.
	Kind ai.EventKind
	// Memory is the memory as the event left it; for ai.EventForgotten it is the memory that was dropped.
	Memory Memory
}

// Perception is one 3D agent's senses and memory. Set its exported fields, add it to a System, and step the system on game.Loop's fixed steps; the system reads the fields at the start of every Step.
type Perception struct {
	// Self is the agent's own StimulusID, so the agent never senses itself; zero means the agent is not a stimulus.
	Self ai.StimulusID
	// Team is the agent's team.
	Team ai.Team
	// Pos is the agent's position.
	Pos math3d.Vec3
	// Forward is the direction the sight cone points along; its length does not matter, and zero faces -Z.
	Forward math3d.Vec3
	// EyeOffset is added to Pos, in world space, to give the point both senses measure from and sight looks from, such as head height on +Y.
	EyeOffset math3d.Vec3
	// Sight is the agent's vision; its angles are measured in 3D around Forward.
	Sight ai.SightConfig
	// Hearing is the agent's hearing.
	Hearing ai.HearingConfig
	// ForgetAfter is how long a stimulus is remembered after it was last sensed; zero or less forgets it on the step after.
	ForgetAfter time.Duration

	state  sense.State
	events []Event
}

// Memories returns a new slice of every memory, sorted by ID.
func (p *Perception) Memories() []Memory {
	return p.AppendMemories(nil)
}

// AppendMemories appends every memory, sorted by ID, to dst and returns the extended slice.
func (p *Perception) AppendMemories(dst []Memory) []Memory {
	for i := range p.state.Mems {
		dst = append(dst, p.memory(&p.state.Mems[i]))
	}
	return dst
}

// Memory returns the memory of the stimulus id, if any.
func (p *Perception) Memory(id ai.StimulusID) (Memory, bool) {
	i, ok := p.state.Find(uint64(id))
	if !ok {
		return Memory{}, false
	}
	return p.memory(&p.state.Mems[i]), true
}

// Sees reports whether the stimulus id is in sight now.
func (p *Perception) Sees(id ai.StimulusID) bool {
	i, ok := p.state.Find(uint64(id))
	return ok && p.state.Mems[i].Visible
}

// Events returns the events of the last Step; the slice is reused by the next Step.
func (p *Perception) Events() []Event {
	return p.events
}

// Forget drops the memory of id without an event and reports whether there was one.
func (p *Perception) Forget(id ai.StimulusID) bool {
	return p.state.Forget(uint64(id))
}

// Clear drops every memory and the last step's events.
func (p *Perception) Clear() {
	p.state.Clear()
	p.events = p.events[:0]
}

func (p *Perception) memory(m *sense.Memory) Memory {
	return Memory{
		ID: ai.StimulusID(m.ID), Team: ai.Team(m.Team), Attitude: ai.Attitude(m.Attitude), Sense: ai.Sense(m.Sense),
		Pos: toVec3(m.Pos), Vel: toVec3(m.Vel),
		Age: m.Age, Strength: m.Strength(p.ForgetAfter), Visible: m.Visible,
	}
}

func (p *Perception) input() sense.Agent {
	c := p.Sight
	return sense.Agent{
		State:      &p.state,
		Self:       uint64(p.Self),
		HasSelf:    p.Self != 0,
		Team:       uint8(p.Team),
		Eye:        fromVec3(p.Pos).Add(fromVec3(p.EyeOffset)),
		Fwd:        fromVec3(p.Forward).Norm(sense.Vec{Z: -1}),
		Sight:      sense.NewSight(c.Range, c.HalfAngle, c.PeripheralRange, c.PeripheralHalfAngle, uint8(c.Detect)),
		HearRange:  float64(p.Hearing.Range),
		HearDetect: uint8(p.Hearing.Detect),
		Forget:     p.ForgetAfter,
	}
}

// System steps the Perceptions of many 3D agents together, so their line-of-sight checks share one per-step Budget, as ai.PerceptionSystem does in 2D. The zero System is ready to use, with every line of sight clear.
type System struct {
	// LineOfSight decides whether a target in view is seen; nil means nothing blocks sight.
	LineOfSight LineOfSight
	// Attitude decides how agents regard targets and noises; nil means ai.DefaultAttitude.
	Attitude ai.AttitudeFunc
	// Budget caps the line-of-sight checks of one step; zero or less checks every pair in view. When the cap is reached, the remaining pairs are checked first on the next step, round-robin, and keep their previous result until then.
	Budget int

	agents []*Perception
	eng    sense.Engine
}

// Add adds p to the system, after the agents already in it. Adding p again does nothing; adding a Perception that belongs to another system panics.
func (s *System) Add(p *Perception) {
	if s.eng.Register(&p.state, s) {
		s.agents = append(s.agents, p)
	}
}

// Remove removes p from the system, keeping its memories, and reports whether it was there.
func (s *System) Remove(p *Perception) bool {
	i := slices.Index(s.agents, p)
	if i < 0 {
		return false
	}
	s.agents = slices.Delete(s.agents, i, i+1)
	p.state.Release()
	return true
}

// Len returns the number of agents in the system.
func (s *System) Len() int {
	return len(s.agents)
}

// PostNoise queues n to be heard on the next Step. Noises are heard in the order they were posted.
func (s *System) PostNoise(n Noise) {
	s.eng.PostNoise(sense.Noise{ID: uint64(n.Source), Team: uint8(n.Team), Pos: fromVec3(n.Pos), Loudness: float64(n.Loudness)})
}

// Step advances every agent by dt: memories age, targets in view are checked for line of sight within the budget, posted noises are heard, and memories not sensed for ForgetAfter are forgotten. targets is read only during the call.
func (s *System) Step(dt time.Duration, targets []Target) {
	s.eng.Agents = s.eng.Agents[:0]
	for _, p := range s.agents {
		s.eng.Agents = append(s.eng.Agents, p.input())
	}
	s.eng.SetTargets(len(targets), func(i int) sense.Target {
		t := &targets[i]
		return sense.Target{ID: uint64(t.ID), Team: uint8(t.Team), Pos: fromVec3(t.Pos), Vel: fromVec3(t.Vel)}
	})
	var los func(from, to sense.Vec) bool
	if s.LineOfSight != nil {
		los = func(from, to sense.Vec) bool { return s.LineOfSight(toVec3(from), toVec3(to)) }
	}
	attitude := s.Attitude
	if attitude == nil {
		attitude = ai.DefaultAttitude
	}
	s.eng.Step(dt, s.Budget, func(observer, other uint8) uint8 { return uint8(attitude(ai.Team(observer), ai.Team(other))) }, los)
	for _, p := range s.agents {
		p.events = p.events[:0]
		for i := range p.state.Events {
			ev := &p.state.Events[i]
			p.events = append(p.events, Event{Kind: ai.EventKind(ev.Kind), Memory: p.memory(&ev.Memory)})
		}
	}
	clear(s.eng.Agents)
}

// Stats returns the line-of-sight work of the last Step.
func (s *System) Stats() ai.PerceptionStats {
	return ai.PerceptionStats(s.eng.Stats)
}

func fromVec3(v math3d.Vec3) sense.Vec {
	return sense.Vec{X: float64(v.X), Y: float64(v.Y), Z: float64(v.Z)}
}

func toVec3(v sense.Vec) math3d.Vec3 {
	return math3d.Vec3{X: float32(v.X), Y: float32(v.Y), Z: float32(v.Z)}
}
