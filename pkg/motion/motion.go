// Package motion contains deterministic 2D motion behaviors usable by scene
// components and code-driven objects.
package motion

import "math"

type Vec2 struct{ X, Y float64 }

// Bullet advances a point with speed, acceleration, gravity and optional angle tracking.
type Bullet struct {
	Position                            Vec2
	Speed, Acceleration, Gravity, Angle float64
	SetAngle                            bool
}

func (b *Bullet) Step(dt float64) {
	if b == nil || dt <= 0 {
		return
	}
	b.Speed += b.Acceleration * dt
	vx, vy := math.Cos(b.Angle*math.Pi/180)*b.Speed, math.Sin(b.Angle*math.Pi/180)*b.Speed
	b.Position.X += vx * dt
	b.Position.Y += vy * dt
	b.Position.Y += .5 * b.Gravity * dt * dt
	b.Speed += b.Gravity * math.Sin(b.Angle*math.Pi/180) * dt
	if b.SetAngle && (vx != 0 || vy != 0) {
		b.Angle = math.Atan2(vy, vx) * 180 / math.Pi
	}
}

type Wave string

const (
	WaveSine     Wave = "sine"
	WaveTriangle Wave = "triangle"
)

// Sine applies a periodic offset to an object's base value.
type Sine struct {
	Period, Magnitude, Phase, Time float64
	Wave                           Wave
}

func (s *Sine) Step(dt float64) float64 {
	if s == nil {
		return 0
	}
	if dt > 0 {
		s.Time += dt
	}
	if s.Period <= 0 || s.Magnitude == 0 {
		return 0
	}
	u := 2*math.Pi*(s.Time/s.Period) + s.Phase
	v := math.Sin(u)
	if s.Wave == WaveTriangle {
		v = 2 / math.Pi * math.Asin(math.Sin(u))
	}
	return s.Magnitude * v
}

// Rotate advances an angle in degrees.
type Rotate struct{ Angle, Speed, Acceleration float64 }

func (r *Rotate) Step(dt float64) float64 {
	if r == nil || dt <= 0 {
		return 0
	}
	r.Angle += r.Speed*dt + .5*r.Acceleration*dt*dt
	r.Speed += r.Acceleration * dt
	r.Angle = math.Mod(r.Angle, 360)
	if r.Angle < 0 {
		r.Angle += 360
	}
	return r.Angle
}
