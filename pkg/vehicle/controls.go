package vehicle

import "github.com/nsgo-dev/game/pkg/replay"

// Controls names the input axes and action a car is driven with, so the same driving code reads live input (*input.Actions), a replay.Recorder while recording, or a replay.Player while playing a recording back.
type Controls struct {
	// Throttle is the axis read into Input.Throttle, in [-1, 1].
	Throttle string
	// Brake is the axis read into Input.Brake, in [0, 1].
	Brake string
	// Steer is the axis read into Input.Steer, in [-1, 1] with positive steering right.
	Steer string
	// Handbrake is the action whose pressed state sets Input.Handbrake.
	Handbrake string
}

// DefaultControls returns Controls reading the axes "throttle", "brake" and "steer" and the action "handbrake".
func DefaultControls() Controls {
	return Controls{Throttle: "throttle", Brake: "brake", Steer: "steer", Handbrake: "handbrake"}
}

// Axes returns the axis names in c, in the order Throttle, Brake, Steer, for replay.NewRecorder; empty names are left out.
func (c Controls) Axes() []string {
	out := make([]string, 0, 3)
	for _, n := range []string{c.Throttle, c.Brake, c.Steer} {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// Actions returns the action names in c for replay.NewRecorder; an empty Handbrake name is left out.
func (c Controls) Actions() []string {
	if c.Handbrake == "" {
		return nil
	}
	return []string{c.Handbrake}
}

// Read samples src into an Input, clamping each axis to its range and treating NaN as 0. An empty name reads as zero or released. Read does not call src.Step.
func (c Controls) Read(src replay.ActionSource) Input {
	axis := func(name string) float32 {
		if name == "" {
			return 0
		}
		return float32(src.Axis(name))
	}
	in := Input{
		Throttle: axis(c.Throttle),
		Brake:    axis(c.Brake),
		Steer:    axis(c.Steer),
	}
	if c.Handbrake != "" {
		in.Handbrake = src.Pressed(c.Handbrake)
	}
	return in.clamped()
}
