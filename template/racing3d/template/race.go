package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/vehicle3d"
)

const (
	// stepRate is the fixed simulation rate, in steps per second.
	stepRate = 60
	// stepDur is one fixed step.
	stepDur = time.Second / stepRate
	// countdownSteps is how long the lights take before the start: three seconds.
	countdownSteps = 3 * stepRate
	// ghostInterval is how many steps apart the best-lap ghost samples the player's car.
	ghostInterval = 3
	// grassSpeed is the fastest a car keeps going off the road before the grass slows it, in m/s.
	grassSpeed = 12
	// flipSteps is how long a car may lie on its side or roof before it is put back on the road.
	flipSteps = stepRate
	// fallHeight is how far below the ground a car may drop, off the edge of the ground, before it is put back on the road.
	fallHeight = 5
)

// The physics layers: wheels ride on the ground and cars pass through each other, so a crowded corner never wrecks a race, while the checkpoints see every car.
const (
	layerGround physics3d.LayerMask = 1 << iota
	layerCar
	layerCheckpoint
)

// phase is where a race is.
type phase int

const (
	// phaseCountdown holds the cars on the grid while the lights count down.
	phaseCountdown phase = iota
	// phaseRacing runs the race until the player finishes or every rival does.
	phaseRacing
	// phaseFinished shows the results while the cars cool down.
	phaseFinished
)

// car is one car in the race.
type car struct {
	name  string
	color math3d.Vec3
	v     *vehicle3d.Vehicle
	racer *vehicle3d.Racer
	// ai drives a rival all race, and the player's car after the finish.
	ai *driver
	// prevPos and prevRot are the chassis pose before the last step, which drawing interpolates from.
	prevPos physics3d.Vec3
	prevRot physics3d.Quat
	// flipped counts the steps the car has been upside down or on its side.
	flipped int
}

// Result is one line of the results screen.
type Result struct {
	// Place is the car's 1-based finishing position.
	Place int
	// Name is the driver's name.
	Name string
	// Player marks the player's car.
	Player bool
	// Finished reports whether the car finished every lap before the race ended.
	Finished bool
	// Steps is the car's race time in steps, when Finished.
	Steps int
	// BestLap is the car's fastest lap in steps, or 0 before it completed one.
	BestLap int
}

// Race is one race on a Track: the physics world, the cars, the lap counting, the countdown and the best-lap ghost. It is pure simulation with no drawing, so the tests drive it directly and the game drives it from game.Loop.
type Race struct {
	track  *Track
	course *course
	world  *physics3d.World
	race   *vehicle3d.Race
	// cars holds the rivals in grid order, then the player's car, which is player.
	cars   []*car
	player *car
	phase  phase
	// steps counts every step since the race was made, the countdown included.
	steps   int
	results []Result

	// ghost is the fastest lap the player has driven, played back each lap; it outlives the race through newRace and the game's storage.
	ghost    *vehicle3d.Ghost
	ghostRec vehicle3d.GhostRecorder
	// onBestLap, when set, hears every new fastest lap, so the game can save its ghost.
	onBestLap func(*vehicle3d.Ghost)
}

// newRace builds a race on t with every car tuned as cfg, the player's car at the back of the grid. ghost is the fastest lap so far, or nil.
func newRace(t *Track, cfg vehicle3d.Config, ghost *vehicle3d.Ghost) (*Race, error) {
	r := &Race{track: t, course: newCourse(t, cfg.MaxSpeed), world: physics3d.NewWorld(), ghost: ghost, ghostRec: vehicle3d.GhostRecorder{Interval: ghostInterval}}

	// The ground: one flat box wider than any circuit Validate accepts, its top at y = 0.
	ground := r.world.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: groundHalf, Y: 1, Z: groundHalf}}), physics3d.Vec3{Y: -1}))
	ground.Layer, ground.Mask = layerGround, layerCar
	ground.Friction = 0.8

	// The checkpoints: a sphere across the road at each, which the race needs every car to pass in order.
	cps := make([]*physics3d.Body, t.Checkpoints)
	for i := range cps {
		p := r.course.checkpoint(i)
		cps[i] = vehicle3d.AddCheckpoint(r.world, physics3d.SphereShape(physics3d.Sphere{Radius: t.Width/2 + 1}), physics3d.Vec3{X: p.X, Y: 1, Z: p.Z})
		cps[i].Layer, cps[i].Mask = layerCheckpoint, layerCar
	}
	race, err := vehicle3d.NewRace(t.Laps, cps...)
	if err != nil {
		return nil, err
	}
	r.race = race
	r.world.OnTrigger = race.Observe
	race.OnLap = r.lapDone

	add := func(slot int, name string, color math3d.Vec3, ai *driver) (*car, error) {
		pos, fwd := r.course.slot(slot)
		pos.Y = rideHeight(cfg)
		v, err := vehicle3d.New(r.world, cfg, toPhys(pos), heading(fwd))
		if err != nil {
			return nil, err
		}
		v.Body.Layer, v.Body.Mask = layerCar, layerGround|layerCheckpoint
		v.RayMask = layerGround
		c := &car{name: name, color: color, v: v, racer: race.AddRacer(v.Body, name), ai: ai, prevPos: v.Body.Pos, prevRot: v.Body.Rot}
		r.cars = append(r.cars, c)
		return c, nil
	}
	for i, rv := range t.Rivals {
		if _, err := add(i, rv.Name, math3d.Vec3{X: rv.Color[0], Y: rv.Color[1], Z: rv.Color[2]}, &driver{course: r.course, pace: rv.Pace, lane: rv.Lane}); err != nil {
			return nil, err
		}
	}
	player, err := add(len(t.Rivals), playerName, math3d.Vec3{X: 0.95, Y: 0.78, Z: 0.1}, nil)
	if err != nil {
		return nil, err
	}
	r.player = player
	return r, nil
}

// rideHeight is how high above the ground a car of tuning cfg starts: its wheels just touching, so it settles onto its springs during the countdown.
func rideHeight(cfg vehicle3d.Config) float32 {
	h := float32(0)
	for _, w := range cfg.Wheels {
		h = max(h, w.RestLength+w.Radius-w.Anchor[1])
	}
	return h
}

// Step advances the race by one fixed step. in is the player's input and respawn asks to put the player's car back on the road; both are ignored outside the race itself.
func (r *Race) Step(in vehicle3d.Input, respawn bool) {
	r.steps++
	for _, c := range r.cars {
		c.prevPos, c.prevRot = c.v.Body.Pos, c.v.Body.Rot
	}
	if r.phase == phaseCountdown {
		// The cars sit on the grid with the brakes on while the suspension settles.
		for _, c := range r.cars {
			c.v.SetInput(vehicle3d.Input{Brake: 1})
			c.v.Step(r.world, stepDur)
		}
		r.world.Step(stepDur)
		if r.steps >= countdownSteps {
			r.phase = phaseRacing
			r.ghostRec.Reset()
			r.ghostRec.Record(r.player.v.Body.Pos, r.player.v.Body.Rot)
		}
		return
	}

	r.race.Tick()
	for _, c := range r.cars {
		var ci vehicle3d.Input
		switch {
		case c.ai != nil:
			ci = c.ai.drive(c.v)
		case c.racer.Finished:
			// The player's car crosses the line on its own: the game hands it to a slow driver for the cool-down lap.
			c.ai = &driver{course: r.course, pace: 0.5}
			ci = c.ai.drive(c.v)
		default:
			ci = r.assist(c, in)
			if respawn {
				r.respawn(c)
			}
		}
		r.keepOnRoad(c, &ci)
		c.v.SetInput(ci)
		c.v.Step(r.world, stepDur)
	}
	r.world.Step(stepDur)
	if r.phase == phaseRacing && !r.player.racer.Finished {
		r.ghostRec.Record(r.player.v.Body.Pos, r.player.v.Body.Rot)
	}
	if r.phase == phaseRacing && (r.player.racer.Finished || r.rivalsFinished()) {
		r.phase = phaseFinished
		r.results = r.standings()
	}
}

// assist turns the player's raw input into what the car is told: holding brake at a standstill reverses.
func (r *Race) assist(c *car, in vehicle3d.Input) vehicle3d.Input {
	if in.Brake > 0 && in.Throttle <= 0 && c.v.Speed() < 1 {
		in.Throttle, in.Brake = -in.Brake, 0
	}
	return in
}

// keepOnRoad slows a car on the grass and puts one that has rolled over, or fallen off the ground, back on the road.
func (r *Race) keepOnRoad(c *car, in *vehicle3d.Input) {
	_, side := r.course.nearest(fromPhys(c.v.Body.Pos))
	if abs(side) > r.track.Width/2 && c.v.Speed() > grassSpeed {
		in.Throttle = min(in.Throttle, 0.2)
		in.Brake = max(in.Brake, 0.35)
	}
	if c.v.Up().Y < 0.3 {
		c.flipped++
	} else {
		c.flipped = 0
	}
	if c.flipped > flipSteps || c.v.Body.Pos.Y < -fallHeight {
		r.respawn(c)
	}
}

// respawn puts c back on the centerline where it is, at rest and facing the way the race goes.
func (r *Race) respawn(c *car) {
	i, _ := r.course.nearest(fromPhys(c.v.Body.Pos))
	s := r.course.samples[i]
	pos := s.pos
	pos.Y = rideHeight(c.v.Config())
	c.v.Reset(toPhys(pos), heading(s.forward))
	c.prevPos, c.prevRot = c.v.Body.Pos, c.v.Body.Rot
	c.flipped = 0
}

// lapDone keeps the player's fastest flying lap as the ghost. Lap 1 is timed from the lights with the car standing on the grid behind the line, while every later lap runs from the line at speed, so only laps after the first are compared and saved; a one-lap race never records a ghost.
func (r *Race) lapDone(ev vehicle3d.LapEvent) {
	if ev.Racer != r.player.racer {
		return
	}
	r.ghostRec.Finish(r.player.v.Body.Pos, r.player.v.Body.Rot)
	if ev.Lap > 1 && (r.ghost == nil || ev.Steps < r.ghost.LapSteps) {
		r.ghost = r.ghostRec.Ghost(stepRate, ev.Steps)
		r.ghost.Name = trackID(r.track)
		if r.onBestLap != nil {
			r.onBestLap(r.ghost)
		}
	}
	r.ghostRec.Reset()
}

// rivalsFinished reports whether every rival has finished.
func (r *Race) rivalsFinished() bool {
	for _, c := range r.cars {
		if c != r.player && !c.racer.Finished {
			return false
		}
	}
	return true
}

// order returns the cars from first to last: finished cars by finish time, then the rest by how far round they are.
func (r *Race) order() []*car {
	cars := slices.Clone(r.cars)
	slices.SortStableFunc(cars, func(a, b *car) int {
		if a.racer.Finished || b.racer.Finished {
			switch {
			case !b.racer.Finished:
				return -1
			case !a.racer.Finished:
				return 1
			}
			return a.racer.FinishStep - b.racer.FinishStep
		}
		pa, pb := r.progress(a), r.progress(b)
		switch {
		case pa > pb:
			return -1
		case pa < pb:
			return 1
		}
		return 0
	})
	return cars
}

// progress is how far c has come in meters: the laps it completed and the distance from the start line to its last checkpoint, plus how far past that checkpoint it is.
func (r *Race) progress(c *car) float32 {
	n := r.track.Checkpoints
	passed := (c.racer.Next - 1 + n) % n // the checkpoint c passed last; 0 is the start line
	segment := r.course.length / float32(n)
	i, _ := r.course.nearest(fromPhys(c.v.Body.Pos))
	along := r.course.wrapDist(r.course.samples[i].dist - (r.track.Start + float32(passed)*segment))
	// A car only just past a checkpoint can sit a step's travel behind it; never count that as most of a lap.
	if along > 2*segment {
		along = 0
	}
	return float32(c.racer.Laps)*r.course.length + float32(passed)*segment + along
}

// standings returns the result of every car in order.
func (r *Race) standings() []Result {
	cars := r.order()
	res := make([]Result, len(cars))
	for i, c := range cars {
		res[i] = Result{Place: i + 1, Name: c.name, Player: c == r.player, Finished: c.racer.Finished, Steps: c.racer.FinishStep, BestLap: c.racer.BestLap}
	}
	return res
}

// Place returns the player's current position, 1-based.
func (r *Race) Place() int {
	return slices.Index(r.order(), r.player) + 1
}

// Lap returns the lap the player is on, 1-based and at most the race's laps.
func (r *Race) Lap() int { return min(r.player.racer.Laps+1, r.track.Laps) }

// Countdown returns the whole seconds left before the start, 0 once the race has started.
func (r *Race) Countdown() int {
	if r.phase != phaseCountdown {
		return 0
	}
	return (countdownSteps - r.steps + stepRate - 1) / stepRate
}

// LapSteps returns how long the player has been on the current lap, in steps.
func (r *Race) LapSteps() int {
	if r.phase == phaseCountdown {
		return 0
	}
	if r.player.racer.Finished {
		return r.player.racer.LastLap
	}
	return r.race.Step() - r.player.racer.LapStart
}

// Results returns the final standings once the race is over, or nil before.
func (r *Race) Results() []Result { return r.results }

// Won reports whether the race is over with the player in first place.
func (r *Race) Won() bool { return len(r.results) > 0 && r.results[0].Player }

// Ghost returns the fastest lap the player has driven, or nil.
func (r *Race) Ghost() *vehicle3d.Ghost { return r.ghost }

// ghostPose returns where the ghost is alpha of the way into the next step and whether it shows: only while the player is racing a flying lap, any lap after the first, and a ghost exists. The ghost is a flying lap from the line, so racing it on lap 1, which starts on the grid, would put it ahead from the lights.
func (r *Race) ghostPose(alpha float32) (physics3d.Vec3, physics3d.Quat, bool) {
	if r.ghost == nil || r.phase != phaseRacing || r.player.racer.Finished || r.player.racer.Laps == 0 {
		return physics3d.Vec3{}, physics3d.Quat{}, false
	}
	pos, rot := r.ghost.Pose(float64(r.race.Step()-r.player.racer.LapStart) + float64(alpha))
	return pos, rot, true
}

// carPose returns c's chassis pose alpha of the way from the previous step to the last one.
func carPose(c *car, alpha float32) (math3d.Vec3, math3d.Quat) {
	p := fromPhys(c.prevPos).Lerp(fromPhys(c.v.Body.Pos), alpha)
	q := fromPhysQuat(c.prevRot).Slerp(fromPhysQuat(c.v.Body.Rot), alpha)
	return p, q
}

// formatSteps writes a duration in steps as m:ss.cc.
func formatSteps(steps int) string {
	cs := steps * 100 / stepRate
	return fmt.Sprintf("%d:%02d.%02d", cs/6000, cs/100%60, cs%100)
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
