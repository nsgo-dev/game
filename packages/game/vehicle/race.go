package vehicle

import (
	"cmp"
	"errors"
	"slices"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

// AddCheckpoint adds a static trigger body with shape at pos to world and returns it, ready to pass to NewRace.
func AddCheckpoint(world *physics3d.World, shape physics3d.Shape, pos physics3d.Vec3) *physics3d.Body {
	b := physics3d.NewStaticBody(shape, pos)
	b.IsTrigger = true
	b.Tag = "checkpoint"
	return world.AddBody(b)
}

// Racer is one body's progress through a Race. Times are counted in fixed steps; multiply by the step duration for wall time.
type Racer struct {
	// Body is the racer's chassis, such as Vehicle.Body.
	Body *physics3d.Body
	// Name optionally labels the racer.
	Name string
	// Laps is the number of laps completed.
	Laps int
	// Next is the index of the checkpoint the racer must reach next; 0 is the start/finish line.
	Next int
	// LapStart is the step the current lap began on.
	LapStart int
	// LastLap is the duration of the last completed lap in steps, or 0 before the first.
	LastLap int
	// BestLap is the duration of the fastest completed lap in steps, or 0 before the first.
	BestLap int
	// Finished reports whether the racer completed every lap.
	Finished bool
	// FinishStep is the step the racer finished on, when Finished.
	FinishStep int

	order int
}

// LapEvent reports a completed lap to Race.OnLap.
type LapEvent struct {
	// Racer is the racer that completed the lap.
	Racer *Racer
	// Lap is the 1-based number of the lap just completed.
	Lap int
	// Steps is the lap's duration in steps.
	Steps int
	// Best reports whether the lap is the racer's fastest so far.
	Best bool
	// Finished reports whether this was the racer's final lap.
	Finished bool
}

// Race tracks racers through an ordered list of checkpoint triggers for a fixed number of laps.
//
// Checkpoint 0 is the start/finish line. Racers start a lap on the step they are added and must enter checkpoints 1, 2, ... in order and then checkpoint 0 to complete it; entering any other checkpoint is ignored, so cutting the track does not count.
type Race struct {
	// Checkpoints are the trigger bodies in track order; index 0 is the start/finish line.
	Checkpoints []*physics3d.Body
	// Laps is the number of laps that finishes the race.
	Laps int
	// OnLap, when set, is called for every completed lap, in the order Observe sees them.
	OnLap func(LapEvent)

	racers []*Racer
	step   int
}

// NewRace returns a Race over checkpoints for laps laps. It needs at least two checkpoints, each a trigger body, and at least one lap.
func NewRace(laps int, checkpoints ...*physics3d.Body) (*Race, error) {
	if laps < 1 {
		return nil, errors.New("vehicle: race needs at least one lap")
	}
	if len(checkpoints) < 2 {
		return nil, errors.New("vehicle: race needs at least two checkpoints")
	}
	for _, cp := range checkpoints {
		if cp == nil || !cp.IsTrigger {
			return nil, errors.New("vehicle: every checkpoint must be a trigger body")
		}
	}
	return &Race{Checkpoints: slices.Clone(checkpoints), Laps: laps}, nil
}

// AddRacer adds body as a racer whose first lap starts at the current step, heading for checkpoint 1.
func (r *Race) AddRacer(body *physics3d.Body, name string) *Racer {
	rc := &Racer{Body: body, Name: name, Next: 1, LapStart: r.step, order: len(r.racers)}
	r.racers = append(r.racers, rc)
	return rc
}

// Racers returns the racers in the order they were added.
func (r *Race) Racers() []*Racer { return r.racers }

// Step returns the current step: the number of Tick calls so far.
func (r *Race) Step() int { return r.step }

// Tick advances the race clock by one fixed step. Call it once per step, before World.Step, so a checkpoint entered during that World.Step is timed at the new step.
func (r *Race) Tick() { r.step++ }

// Observe handles a trigger event from World.OnTrigger, advancing a racer whose body entered its next checkpoint. Other events are ignored, so Observe can sit behind other trigger handlers.
func (r *Race) Observe(ev physics3d.ContactEvent) {
	if ev.Kind != physics3d.ContactEnter || !ev.IsTrigger {
		return
	}
	r.enter(ev.BodyA, ev.BodyB)
	r.enter(ev.BodyB, ev.BodyA)
}

func (r *Race) enter(body, trigger *physics3d.Body) {
	cp := slices.Index(r.Checkpoints, trigger)
	if cp < 0 {
		return
	}
	for _, rc := range r.racers {
		if rc.Body != body || rc.Finished || rc.Next != cp {
			continue
		}
		if cp != 0 {
			rc.Next = (cp + 1) % len(r.Checkpoints)
			continue
		}
		steps := r.step - rc.LapStart
		rc.Laps++
		rc.LastLap = steps
		best := rc.BestLap == 0 || steps < rc.BestLap
		if best {
			rc.BestLap = steps
		}
		rc.LapStart = r.step
		rc.Next = 1
		if rc.Laps >= r.Laps {
			rc.Finished = true
			rc.FinishStep = r.step
		}
		if r.OnLap != nil {
			r.OnLap(LapEvent{Racer: rc, Lap: rc.Laps, Steps: steps, Best: best, Finished: rc.Finished})
		}
	}
}

// Standings returns the racers from first to last: finished racers by finish step, then the rest by laps completed and checkpoints passed, with ties kept in the order racers were added.
func (r *Race) Standings() []*Racer {
	out := slices.Clone(r.racers)
	n := len(r.Checkpoints)
	progress := func(rc *Racer) int {
		// Next == 0 means every checkpoint of the lap but the finish line was passed.
		passed := rc.Next - 1
		if rc.Next == 0 {
			passed = n - 1
		}
		return rc.Laps*n + passed
	}
	slices.SortStableFunc(out, func(a, b *Racer) int {
		if a.Finished != b.Finished {
			if a.Finished {
				return -1
			}
			return 1
		}
		if a.Finished {
			if c := cmp.Compare(a.FinishStep, b.FinishStep); c != 0 {
				return c
			}
		} else if c := cmp.Compare(progress(b), progress(a)); c != 0 {
			return c
		}
		return cmp.Compare(a.order, b.order)
	})
	return out
}
