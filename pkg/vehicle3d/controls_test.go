package vehicle3d_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/vehicle3d"
)

type fixedSource struct {
	axes    map[string]float64
	pressed map[string]bool
}

func (s fixedSource) Pressed(n string) bool    { return s.pressed[n] }
func (s fixedSource) JustPressed(string) bool  { return false }
func (s fixedSource) JustReleased(string) bool { return false }
func (s fixedSource) Axis(n string) float64    { return s.axes[n] }
func (s fixedSource) Step()                    {}

func TestControlsRead(t *testing.T) {
	c := vehicle3d.DefaultControls()
	src := fixedSource{
		axes:    map[string]float64{"throttle": 2, "brake": math.NaN(), "steer": -0.25},
		pressed: map[string]bool{"handbrake": true},
	}
	if got := c.Read(src); got != (vehicle3d.Input{Throttle: 1, Steer: -0.25, Handbrake: true}) {
		t.Fatalf("Read = %+v", got)
	}
	if got := (vehicle3d.Controls{}).Read(src); got != (vehicle3d.Input{}) {
		t.Fatalf("empty Controls read %+v", got)
	}
	if !reflect.DeepEqual(c.Axes(), []string{"throttle", "brake", "steer"}) || !reflect.DeepEqual(c.Actions(), []string{"handbrake"}) {
		t.Fatalf("Axes %v Actions %v", c.Axes(), c.Actions())
	}
	if a := (vehicle3d.Controls{Steer: "x"}).Axes(); !reflect.DeepEqual(a, []string{"x"}) {
		t.Fatalf("Axes skips empty names: %v", a)
	}
	if a := (vehicle3d.Controls{}).Actions(); a != nil {
		t.Fatalf("Actions = %v", a)
	}
}

func TestControlsReadPlayer(t *testing.T) {
	c := vehicle3d.DefaultControls()
	rec := &replay.Recording{
		Version: replay.FormatVersion, Rate: 60, TotalSteps: 2,
		Actions: c.Actions(), Axes: c.Axes(),
		Events: []replay.StepEvent{
			{Step: 0, Axes: []replay.AxisChange{{Index: 0, Value: 0.5}, {Index: 2, Value: 1}}},
			{Step: 1, Press: []int{0}, Axes: []replay.AxisChange{{Index: 1, Value: 1}}},
		},
	}
	p := replay.NewPlayer(rec)
	p.Step()
	if got := c.Read(p); got != (vehicle3d.Input{Throttle: 0.5, Steer: 1}) {
		t.Fatalf("step 0 = %+v", got)
	}
	p.Step()
	if got := c.Read(p); got != (vehicle3d.Input{Throttle: 0.5, Brake: 1, Steer: 1, Handbrake: true}) {
		t.Fatalf("step 1 = %+v", got)
	}
}
