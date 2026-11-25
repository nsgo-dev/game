package main

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
)

const (
	// sampleSpacing is the target distance between the course's samples along the centerline, in meters.
	sampleSpacing = 2
	// cornerGrip is the sideways acceleration, in g, the racing line plans corners for: below the tires' friction so a car on the line keeps some grip in hand.
	cornerGrip = 0.85
	// planBrake is the deceleration in m/s² the racing line plans braking for, below the car's full braking.
	planBrake = 7
	// gravity is the acceleration due to gravity, in m/s².
	gravity = 9.81
)

// course is a Track turned into geometry: the closed centerline sampled every couple of meters, with the direction, the right-hand side and the racing line's speed at each sample.
type course struct {
	track   *Track
	curve   *math3d.CatmullRom3D
	length  float32
	spacing float32
	samples []sample
}

// sample is one point of the centerline.
type sample struct {
	// pos is the centerline's point on the ground.
	pos math3d.Vec3
	// forward is the unit direction of travel and right the unit direction to its right, both on the ground.
	forward, right math3d.Vec3
	// dist is how far the point is along the loop from the track's first point.
	dist float32
	// speed is the fastest the racing line takes this point, in m/s, slow enough to brake for every corner ahead.
	speed float32
}

// newCourse samples t's centerline and plans the racing line's speed for a car whose top speed is maxSpeed.
func newCourse(t *Track, maxSpeed float32) *course {
	pts := make([]math3d.Vec3, len(t.Points))
	for i, p := range t.Points {
		pts[i] = math3d.Vec3{X: p.X, Z: p.Z}
	}
	curve := math3d.NewCatmullRom3DAlpha(pts, 0.5, true)
	length := curve.Length()
	n := max(16, int(math.Ceil(float64(length/sampleSpacing))))
	n += n % 2 // an even count keeps the curbs' stripes alternating where the loop closes
	c := &course{track: t, curve: curve, length: length, spacing: length / float32(n), samples: make([]sample, n)}
	up := math3d.Vec3Up()
	for i := range c.samples {
		d := float32(i) * c.spacing
		p := curve.PointAtDistance(d)
		f := flat(curve.TangentAtDistance(d)).Norm()
		c.samples[i] = sample{pos: math3d.Vec3{X: p.X, Z: p.Z}, forward: f, right: f.Cross(up).Norm(), dist: d}
	}
	// The fastest each point can be taken on its own: the speed whose sideways acceleration on the corner's radius is cornerGrip g.
	for i := range c.samples {
		a, b := c.samples[c.wrap(i-2)].forward, c.samples[c.wrap(i+2)].forward
		turn := math.Atan2(float64(a.Cross(b).Y), float64(a.Dot(b)))
		speed := maxSpeed
		if k := math.Abs(turn) / float64(4*c.spacing); k > 1e-4 {
			speed = min(maxSpeed, float32(math.Sqrt(cornerGrip*gravity/k)))
		}
		c.samples[i].speed = speed
	}
	// Then slower where a corner ahead needs braking for: two passes backwards around the loop carry each limit to the points before it.
	for range 2 {
		for i := n - 1; i >= 0; i-- {
			next := c.samples[c.wrap(i+1)].speed
			reach := float32(math.Sqrt(float64(next*next + 2*planBrake*c.spacing)))
			c.samples[i].speed = min(c.samples[i].speed, reach)
		}
	}
	return c
}

// wrap returns sample index i taken around the loop.
func (c *course) wrap(i int) int {
	n := len(c.samples)
	return ((i % n) + n) % n
}

// wrapDist returns distance d taken around the loop, in [0, length).
func (c *course) wrapDist(d float32) float32 {
	d = float32(math.Mod(float64(d), float64(c.length)))
	if d < 0 {
		d += c.length
	}
	return d
}

// at returns the sample nearest distance d along the loop.
func (c *course) at(d float32) sample {
	return c.samples[c.wrap(int(math.Round(float64(c.wrapDist(d)/c.spacing))))]
}

// nearest returns the index of the sample closest to p on the ground and how far right of the centerline p is there.
func (c *course) nearest(p math3d.Vec3) (int, float32) {
	best, bestD := 0, float32(math.MaxFloat32)
	for i := range c.samples {
		if d := flat(c.samples[i].pos.Sub(p)).LenSq(); d < bestD {
			best, bestD = i, d
		}
	}
	s := c.samples[best]
	return best, flat(p.Sub(s.pos)).Dot(s.right)
}

// checkpoint returns the centerline point of checkpoint i, where 0 is the start/finish line.
func (c *course) checkpoint(i int) math3d.Vec3 {
	return c.curve.PointAtDistance(c.wrapDist(c.track.Start + float32(i)*c.length/float32(c.track.Checkpoints)))
}

// slot returns where grid slot i starts and the direction it faces: two abreast behind the start line, the right-hand car of each row half a gap further back.
func (c *course) slot(i int) (math3d.Vec3, math3d.Vec3) {
	row, col := i/2, i%2
	back := c.track.Grid*float32(row+1) + c.track.Grid*0.5*float32(col)
	s := c.at(c.track.Start - back)
	side := c.track.Width / 4
	if col == 0 {
		side = -side
	}
	return s.pos.Add(s.right.Scale(side)), s.forward
}

// flat drops v's height.
func flat(v math3d.Vec3) math3d.Vec3 { return math3d.Vec3{X: v.X, Z: v.Z} }

// toPhys and fromPhys convert positions between math3d, which drawing uses, and physics3d, which the cars use.
func toPhys(v math3d.Vec3) physics3d.Vec3   { return physics3d.Vec3{X: v.X, Y: v.Y, Z: v.Z} }
func fromPhys(v physics3d.Vec3) math3d.Vec3 { return math3d.Vec3{X: v.X, Y: v.Y, Z: v.Z} }

// fromPhysQuat converts an orientation from physics3d to math3d.
func fromPhysQuat(q physics3d.Quat) math3d.Quat { return math3d.Quat{X: q.X, Y: q.Y, Z: q.Z, W: q.W} }

// heading returns the orientation that turns a car, which faces -Z, toward forward on the ground.
func heading(forward math3d.Vec3) physics3d.Quat {
	yaw := float32(math.Atan2(float64(-forward.X), float64(-forward.Z)))
	return physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, yaw)
}
