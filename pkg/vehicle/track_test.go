package vehicle_test

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/vehicle"
)

const (
	rate = 60
	dt   = time.Second / rate
	// trackRadius is the radius of the stadium track's two hairpins.
	trackRadius = 15
	// trackHalf is half the length of its two straights.
	trackHalf = 20
)

// testTrack is the reference test track: a flat stadium circuit (two 40 m straights joined by 15 m hairpins) with four checkpoints and a speed bump on the back straight.
type testTrack struct {
	world     *physics3d.World
	car       *vehicle.Vehicle
	race      *vehicle.Race
	racer     *vehicle.Racer
	waypoints []physics3d.Vec3
}

func box(hx, hy, hz float32) physics3d.Shape {
	return physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: hx, Y: hy, Z: hz}})
}

// newTestTrack builds the track and places the reference car behind the start line, facing down the first straight (-Z).
func newTestTrack(laps int) *testTrack {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(box(80, 1, 80), physics3d.Vec3{X: trackRadius, Y: -1}))
	// A 6 cm speed bump across the back straight.
	w.AddBody(physics3d.NewStaticBody(box(6, 0.03, 0.4), physics3d.Vec3{X: 2 * trackRadius, Y: 0.03, Z: 5}))

	gate := box(7, 2, 0.5)
	cps := []*physics3d.Body{
		vehicle.AddCheckpoint(w, gate, physics3d.Vec3{Y: 1, Z: 10}),
		vehicle.AddCheckpoint(w, box(0.5, 2, 7), physics3d.Vec3{X: trackRadius, Y: 1, Z: -trackHalf - trackRadius}),
		vehicle.AddCheckpoint(w, gate, physics3d.Vec3{X: 2 * trackRadius, Y: 1}),
		vehicle.AddCheckpoint(w, box(0.5, 2, 7), physics3d.Vec3{X: trackRadius, Y: 1, Z: trackHalf + trackRadius}),
	}
	race, err := vehicle.NewRace(laps, cps...)
	if err != nil {
		panic(err)
	}
	w.OnTrigger = race.Observe

	car, err := vehicle.New(w, vehicle.ReferenceConfig(), physics3d.Vec3{Y: 0.62, Z: 14}, physics3d.QuatIdentity())
	if err != nil {
		panic(err)
	}
	return &testTrack{
		world:     w,
		car:       car,
		race:      race,
		racer:     race.AddRacer(car.Body, "reference"),
		waypoints: stadium(),
	}
}

// stadium returns the racing line's waypoints, about 3 m apart, turning right: down x = 0 toward -Z, around the far hairpin, back up x = 30 and around the near hairpin.
func stadium() []physics3d.Vec3 {
	var pts []physics3d.Vec3
	for z := float32(trackHalf); z > -trackHalf; z -= 3 {
		pts = append(pts, physics3d.Vec3{Z: z})
	}
	arc := func(cz float32, from float64) {
		for i := 0; i < 16; i++ {
			a := from + math.Pi*float64(i)/16
			pts = append(pts, physics3d.Vec3{
				X: trackRadius - trackRadius*float32(math.Cos(a)),
				Z: cz - trackRadius*float32(math.Sin(a)),
			})
		}
	}
	arc(-trackHalf, 0)
	for z := float32(-trackHalf); z < trackHalf; z += 3 {
		pts = append(pts, physics3d.Vec3{X: 2 * trackRadius, Z: z})
	}
	arc(trackHalf, math.Pi)
	return pts
}

// driver is the test's deterministic reference driver: a pure-pursuit steering law toward the next waypoint and a speed target that slows for the hairpins. It is a replay.ActionSource so a replay.Recorder can record it.
type driver struct {
	car       *vehicle.Vehicle
	waypoints []physics3d.Vec3
	next      int
	axes      map[string]float64
}

var _ replay.ActionSource = (*driver)(nil)

func newDriver(car *vehicle.Vehicle, waypoints []physics3d.Vec3) *driver {
	return &driver{car: car, waypoints: waypoints, axes: map[string]float64{}}
}

// Step computes this step's axes from the car's current state.
func (d *driver) Step() {
	pos := d.car.Body.Pos
	flat := func(v physics3d.Vec3) physics3d.Vec3 { return physics3d.Vec3{X: v.X, Z: v.Z} }
	for range d.waypoints {
		if flat(d.waypoints[d.next].Sub(pos)).Len() > 7 {
			break
		}
		d.next = (d.next + 1) % len(d.waypoints)
	}
	to := flat(d.waypoints[d.next].Sub(pos)).Norm()
	fwd := flat(d.car.Forward()).Norm()
	// Positive cross.Y means the target is to the left of forward.
	cross := fwd.Cross(to).Y
	dot := fwd.Dot(to)
	angle := float32(math.Atan2(float64(cross), float64(dot)))
	steer := max(-1, min(1, -angle*2))

	target := float32(22)
	if math.Abs(float64(angle)) > 0.15 {
		target = 13
	}
	speed := d.car.Speed()
	throttle := max(0, min(1, (target-speed)*0.5))
	brake := max(0, min(1, (speed-target-1)*0.3))
	d.axes["throttle"] = float64(throttle)
	d.axes["brake"] = float64(brake)
	d.axes["steer"] = float64(steer)
}

func (d *driver) Pressed(string) bool      { return false }
func (d *driver) JustPressed(string) bool  { return false }
func (d *driver) JustReleased(string) bool { return false }
func (d *driver) Axis(name string) float64 { return d.axes[name] }

// step runs one fixed step in the order a game.Loop Update would: read input, tick the race, step the car, then the world.
func (tt *testTrack) step(src replay.ActionSource) {
	src.Step()
	tt.car.SetInput(vehicle.DefaultControls().Read(src))
	tt.race.Tick()
	tt.car.Step(tt.world, dt)
	tt.world.Step(dt)
}

// pose is a car's end state compared bit for bit between runs.
type pose struct {
	Pos    physics3d.Vec3
	Rot    physics3d.Quat
	Vel    physics3d.Vec3
	AngVel physics3d.Vec3
	Laps   int
	Best   int
	Last   int
	Finish int
}

func (tt *testTrack) pose() pose {
	b := tt.car.Body
	return pose{
		Pos: b.Pos, Rot: b.Rot, Vel: b.Vel, AngVel: b.AngVel,
		Laps: tt.racer.Laps, Best: tt.racer.BestLap, Last: tt.racer.LastLap, Finish: tt.racer.FinishStep,
	}
}
