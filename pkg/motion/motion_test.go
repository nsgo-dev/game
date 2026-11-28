package motion

import (
	"math"
	"testing"
)

func TestBulletIsFixedRateIndependent(t *testing.T) {
	a := Bullet{Speed: 10, Angle: 0}
	b := a
	a.Step(1)
	for range 10 {
		b.Step(.1)
	}
	if math.Abs(a.Position.X-b.Position.X) > 1e-9 {
		t.Fatalf("positions %v %v", a.Position, b.Position)
	}
}
func TestSineGoldenAndTriangle(t *testing.T) {
	s := Sine{Period: 2, Magnitude: 4, Wave: WaveSine}
	s.Step(.5)
	if got := s.Step(0); math.Abs(got-4) > 1e-9 {
		t.Fatalf("sine=%v", got)
	}
	tr := Sine{Period: 4, Magnitude: 2, Wave: WaveTriangle}
	tr.Step(1)
	if got := tr.Step(0); math.Abs(got-2) > 1e-9 {
		t.Fatalf("triangle=%v", got)
	}
}
func TestRotateWraps(t *testing.T) {
	r := Rotate{Angle: 350, Speed: 20}
	if got := r.Step(1); got != 10 {
		t.Fatalf("angle=%v", got)
	}
}
