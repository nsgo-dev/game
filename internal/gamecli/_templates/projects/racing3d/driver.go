package main

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/vehicle3d"
)

// driver drives a car along the racing line: it steers toward a point ahead on the line (pure pursuit) and holds the line's planned speed scaled by its pace. The rivals use it, the player's car uses it after the finish, and the tests drive the player's car with it through the keyboard, a gamepad or the touch controls.
type driver struct {
	course *course
	// pace scales the racing line's speed, in (0, 1].
	pace float32
	// lane is how far right of the centerline the driver keeps.
	lane float32
}

// drive returns the input that keeps car on the line this step.
func (d *driver) drive(car *vehicle3d.Vehicle) vehicle3d.Input {
	pos := fromPhys(car.Body.Pos)
	i, _ := d.course.nearest(pos)
	speed := car.Speed()

	// Look further ahead the faster the car goes, which keeps the steering calm on the straights.
	ahead := 5 + 0.45*max(speed, 0)
	s := d.course.at(d.course.samples[i].dist + ahead)
	target := s.pos.Add(s.right.Scale(d.lane))
	fwd := flat(fromPhys(car.Forward())).Norm()
	to := flat(target.Sub(pos)).Norm()
	// A positive angle means the target is to the left of where the car points; vehicle3d.Input steers right for positive values.
	angle := float32(math.Atan2(float64(fwd.Cross(to).Y), float64(fwd.Dot(to))))
	steer := math3d.Clamp(-2.2*angle, -1, 1)

	// Hold the slowest planned speed over the next few meters, so the car is already braking as a corner arrives.
	want := d.course.samples[i].speed
	for k := 1; k <= 3; k++ {
		want = min(want, d.course.samples[d.course.wrap(i+k)].speed)
	}
	want *= d.pace
	return vehicle3d.Input{
		Throttle: math3d.Clamp((want-speed)*0.6, 0, 1),
		Brake:    math3d.Clamp((speed-want-0.5)*0.4, 0, 1),
		Steer:    steer,
	}
}
