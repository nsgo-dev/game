package ai

import (
	"strconv"

	"github.com/nsgo-dev/game/packages/game/ai/internal/sense"
)

// StimulusID identifies something an agent can sense: a target it can see or the source of a noise it can hear. A memory is kept per StimulusID, so a target seen and later heard refreshes one memory. IDs are the game's own, such as entity IDs. Zero is a valid target or noise ID, and anonymous noises usually share it, but Perception.Self treats zero as "not a stimulus", so an agent that is also a target or a noise source needs a non-zero ID.
type StimulusID uint64

// Team is a team affiliation, compared by an AttitudeFunc to decide whether an agent treats a stimulus as hostile, neutral or friendly.
type Team uint8

// NoTeam is the team of things that belong to no side; DefaultAttitude treats it as neutral to everyone.
const NoTeam Team = 255

// Attitude is how an observer regards another team.
type Attitude uint8

const (
	// Hostile marks an enemy.
	Hostile Attitude = iota
	// Neutral marks something on no side.
	Neutral
	// Friendly marks an ally.
	Friendly
)

// String returns the attitude's lower-case name.
func (a Attitude) String() string {
	switch a {
	case Hostile:
		return "hostile"
	case Neutral:
		return "neutral"
	case Friendly:
		return "friendly"
	}
	return "attitude(" + strconv.Itoa(int(a)) + ")"
}

// AttitudeMask selects the attitudes a sense detects. A zero AttitudeMask detects hostile stimuli only.
type AttitudeMask uint8

const (
	// DetectHostile detects hostile stimuli.
	DetectHostile AttitudeMask = 1 << Hostile
	// DetectNeutral detects neutral stimuli.
	DetectNeutral AttitudeMask = 1 << Neutral
	// DetectFriendly detects friendly stimuli.
	DetectFriendly AttitudeMask = 1 << Friendly
	// DetectAll detects every attitude.
	DetectAll = DetectHostile | DetectNeutral | DetectFriendly
)

// Has reports whether m detects attitude a, treating a zero mask as DetectHostile.
func (m AttitudeMask) Has(a Attitude) bool {
	return sense.Detects(uint8(m), uint8(a))
}

// AttitudeFunc decides how an observer on team observer regards a stimulus on team other.
type AttitudeFunc func(observer, other Team) Attitude

// DefaultAttitude treats the same team as friendly, NoTeam on either side as neutral, and every other pair of teams as hostile.
func DefaultAttitude(observer, other Team) Attitude {
	switch {
	case observer == other && observer != NoTeam:
		return Friendly
	case observer == NoTeam || other == NoTeam:
		return Neutral
	}
	return Hostile
}

// Sense names the sense that last refreshed a memory.
type Sense uint8

const (
	// SenseSight is a stimulus seen inside a sight cone with a clear line of sight.
	SenseSight Sense = iota + 1
	// SenseHearing is a noise heard within hearing range.
	SenseHearing
)

// String returns the sense's lower-case name.
func (s Sense) String() string {
	switch s {
	case SenseSight:
		return "sight"
	case SenseHearing:
		return "hearing"
	}
	return "sense(" + strconv.Itoa(int(s)) + ")"
}

// EventKind is what happened to a memory during a step.
type EventKind uint8

const (
	// EventGained reports a stimulus sensed with no memory of it before.
	EventGained EventKind = iota + 1
	// EventUpdated reports a remembered stimulus sensed again with something new: seen again after being out of sight, seen moving, or heard.
	EventUpdated
	// EventLost reports a stimulus that was in sight and no longer is. Its memory stays until it is forgotten.
	EventLost
	// EventForgotten reports a memory dropped because nothing sensed it for ForgetAfter.
	EventForgotten
)

// String returns the event kind's lower-case name.
func (k EventKind) String() string {
	switch k {
	case EventGained:
		return "gained"
	case EventUpdated:
		return "updated"
	case EventLost:
		return "lost"
	case EventForgotten:
		return "forgotten"
	}
	return "event(" + strconv.Itoa(int(k)) + ")"
}

// SightConfig describes an agent's vision. The zero SightConfig sees nothing.
//
// A target is in view when it lies within Range of the eye and within HalfAngle of the facing direction, or within PeripheralRange and within PeripheralHalfAngle. Angles are in radians; a half-angle of π or more covers every direction. Ranges are inclusive. A target in view is seen when the line of sight to it is clear.
type SightConfig struct {
	// Range is how far the agent sees inside its cone; +Inf sees any finite distance, and zero, less or NaN disables the cone.
	Range float32
	// HalfAngle is the half-angle of the sight cone around the facing direction; zero, less or NaN disables the cone.
	HalfAngle float32
	// PeripheralRange is a shorter range seen over PeripheralHalfAngle, so a guard notices a target right beside it.
	PeripheralRange float32
	// PeripheralHalfAngle is the half-angle the peripheral range covers; zero means π, all around the agent, and a negative or NaN value disables the peripheral range.
	PeripheralHalfAngle float32
	// Detect selects the attitudes sight detects; zero detects hostile targets only.
	Detect AttitudeMask
}

// HearingConfig describes an agent's hearing. The zero HearingConfig hears nothing.
type HearingConfig struct {
	// Range is how far away a noise of loudness 1 is heard; a noise's loudness scales it. +Inf hears a noise at any finite distance at strength 1, and zero, less or NaN hears nothing.
	Range float32
	// Detect selects the attitudes hearing detects; zero detects hostile noises only.
	Detect AttitudeMask
}

// PerceptionStats reports the line-of-sight work of the last step.
type PerceptionStats struct {
	// Candidates is the number of agent-target pairs in view.
	Candidates int
	// Checks is the number of LineOfSight calls made, never more than a positive Budget and zero when LineOfSight is nil.
	Checks int
	// Deferred is the number of candidates left to later steps by the budget; they keep their previous result meanwhile.
	Deferred int
}

func (c SightConfig) resolve() sense.Sight {
	return sense.NewSight(c.Range, c.HalfAngle, c.PeripheralRange, c.PeripheralHalfAngle, uint8(c.Detect))
}

// codes adapts f, or DefaultAttitude when f is nil, to the engine's numeric codes.
func (f AttitudeFunc) codes() func(observer, other uint8) uint8 {
	if f == nil {
		f = DefaultAttitude
	}
	return func(observer, other uint8) uint8 { return uint8(f(Team(observer), Team(other))) }
}
