package resource

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// Gradient is a run of colors keyed over offsets from 0 to 1, such as a particle's color over its life or a sky's color over the day.
type Gradient struct {
	Header `yaml:",inline"`
	// Name is an optional human-readable name.
	Name string `yaml:"name,omitempty"`
	// Stops are the gradient's colors in non-decreasing Offset order; two stops at one offset make a hard edge. There must be at least one.
	Stops []GradientStop `yaml:"stops"`
}

// GradientStop is one color of a Gradient.
type GradientStop struct {
	// Offset is where the color sits, from 0 to 1.
	Offset float32 `yaml:"offset"`
	// Color is a CSS color parsed with core.ParseColor.
	Color string `yaml:"color"`
}

// Kind returns KindGradient.
func (g *Gradient) Kind() Kind { return KindGradient }

// Validate checks the header and that the stops are in order with valid colors.
func (g *Gradient) Validate() error {
	if g == nil {
		return loaderr.Errorf("", "pass a non-nil *resource.Gradient to Validate", "resource: nil gradient")
	}
	if err := g.Header.validate(KindGradient); err != nil {
		return err
	}
	if len(g.Stops) == 0 {
		return loaderr.Errorf("stops", "add at least one color stop under stops", "resource: gradient needs at least one stop")
	}
	for i, s := range g.Stops {
		sPath := fmt.Sprintf("stops[%d]", i)
		if err := checkUnit(sPath+".offset", fmt.Sprintf("gradient stop %d offset", i), s.Offset); err != nil {
			return err
		}
		if i > 0 && s.Offset < g.Stops[i-1].Offset {
			return loaderr.Errorf(sPath+".offset", fmt.Sprintf("order gradient stops in non-decreasing offset order (at or after %v)", g.Stops[i-1].Offset), "resource: gradient stop %d offset %v is before the previous stop's %v", i, s.Offset, g.Stops[i-1].Offset)
		}
		if _, err := core.ParseColor(s.Color); err != nil {
			return loaderr.Errorf(sPath+".color", "set color to a valid CSS color such as #ff8800 or rgba(255, 136, 0, 1)", "resource: gradient stop %d color %q: %w", i, s.Color, err)
		}
	}
	return nil
}

// CoreStops returns the stops as core.GradientStop values for a canvas gradient. A stop whose color does not parse is transparent.
func (g *Gradient) CoreStops() []core.GradientStop {
	if g == nil {
		return nil
	}
	out := make([]core.GradientStop, len(g.Stops))
	for i, s := range g.Stops {
		c, _ := core.ParseColor(s.Color)
		out[i] = core.GradientStop{Offset: s.Offset, Color: c}
	}
	return out
}

// At returns the gradient's color at t, mixing each channel linearly between the stops around it and holding the end colors beyond them.
func (g *Gradient) At(t float32) core.Color {
	if g == nil || len(g.Stops) == 0 {
		return 0
	}
	color := func(i int) core.Color {
		c, _ := core.ParseColor(g.Stops[i].Color)
		return c
	}
	n := len(g.Stops)
	if t <= g.Stops[0].Offset || math.IsNaN(float64(t)) {
		return color(0)
	}
	if t >= g.Stops[n-1].Offset {
		return color(n - 1)
	}
	i := 1
	for i < n && g.Stops[i].Offset <= t {
		i++
	}
	a, b := g.Stops[i-1], g.Stops[i]
	u := (t - a.Offset) / (b.Offset - a.Offset)
	return lerpColor(color(i-1), color(i), u)
}

func lerpColor(a, b core.Color, u float32) core.Color {
	var out core.Color
	for shift := 0; shift < 32; shift += 8 {
		ca := float32(a >> shift & 0xff)
		cb := float32(b >> shift & 0xff)
		v := uint32(ca + (cb-ca)*u + 0.5)
		out |= core.Color(min(v, 255)) << shift
	}
	return out
}
