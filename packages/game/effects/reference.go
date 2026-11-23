package effects

import (
	"fmt"
	"image"
	"maps"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// Input is what an effect's Reference reads: the pass before it, or the frame for the first, as Input and InputSize read them in its source.
type Input struct {
	at   func(x, y float64) [4]float64
	w, h float64
}

// At returns the premultiplied color of the input at (x, y), in the frame's pixels: for the frame, the pixel under the point, transparent outside it.
func (in Input) At(x, y float64) [4]float64 { return in.at(x, y) }

// Size returns the frame's size in pixels.
func (in Input) Size() (w, h float64) { return in.w, in.h }

// Reference draws frame through the chain on the CPU into a picture of width by height pixels, with each effect's Reference, as View draws it on the GPU into a box of that many pixels with the same uniforms: each pixel's center is mapped onto the frame and drawn by the last effect. perEffect[i] is effect i's uniforms, as Chain.Uniforms and View.Uniforms take them. It is the picture a test compares a chain's output with, and a screenshot of it where no GPU draws. Colors are clamped to 0 to 1 and rounded to 8 bits.
//
// It returns an error when an effect has no Reference, when perEffect names a uniform an effect does not declare or has more entries than the chain has effects, and for a uniform set to an AnimatedNode other than a core.Constant, whose value only the platform knows.
func (c *Chain) Reference(frame *image.RGBA, width, height int, perEffect []core.Uniforms) (*image.RGBA, error) {
	for i, e := range c.effects {
		if e.Reference == nil {
			return nil, fmt.Errorf("effects: %s has no Reference", e.label(i))
		}
	}
	if len(perEffect) > len(c.effects) {
		return nil, fmt.Errorf("effects: uniforms for %d effects, but the chain has %d", len(perEffect), len(c.effects))
	}
	for i, u := range perEffect {
		for _, name := range slices.Sorted(maps.Keys(u)) {
			if !c.uniforms[i][name] {
				return nil, fmt.Errorf("effects: %s has no uniform %s", c.effects[i].label(i), name)
			}
			if animated(u[name]) {
				return nil, fmt.Errorf("effects: %s: uniform %s follows an animated node, whose value only the platform knows", c.effects[i].label(i), name)
			}
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, max(width, 0), max(height, 0)))
	if width <= 0 || height <= 0 {
		return out, nil
	}
	var fw, fh float64
	if frame != nil {
		fw, fh = float64(frame.Rect.Dx()), float64(frame.Rect.Dy())
	}
	in := Input{at: framePixel(frame), w: fw, h: fh}
	for i, e := range c.effects {
		var u core.Uniforms
		if i < len(perEffect) {
			u = perEffect[i]
		}
		prev, ref := in, e.Reference
		in = Input{at: func(x, y float64) [4]float64 { return ref(prev, u, x, y) }, w: fw, h: fh}
	}
	sx, sy := fw/float64(width), fh/float64(height)
	for py := range height {
		row := out.Pix[py*out.Stride:]
		for px := range width {
			c := in.at((float64(px)+0.5)*sx, (float64(py)+0.5)*sy)
			for k, v := range c {
				row[px*4+k] = to8(v)
			}
		}
	}
	return out, nil
}

// framePixel reads frame's pixel under a point, premultiplied from 0 to 1, transparent outside it, as image0At reads the frame's texture.
func framePixel(frame *image.RGBA) func(x, y float64) [4]float64 {
	return func(x, y float64) [4]float64 {
		if frame == nil || math.IsNaN(x) || math.IsNaN(y) {
			return [4]float64{}
		}
		fx, fy := math.Floor(x), math.Floor(y)
		if fx < 0 || fy < 0 || fx >= float64(frame.Rect.Dx()) || fy >= float64(frame.Rect.Dy()) {
			return [4]float64{}
		}
		i := frame.PixOffset(frame.Rect.Min.X+int(fx), frame.Rect.Min.Y+int(fy))
		p := frame.Pix[i : i+4 : i+4]
		return [4]float64{float64(p[0]) / 255, float64(p[1]) / 255, float64(p[2]) / 255, float64(p[3]) / 255}
	}
}

// to8 clamps v to 0 to 1 and rounds it to 8 bits.
func to8(v float64) uint8 {
	if !(v > 0) {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(v * 255))
}

// animated reports whether a uniform's value holds an AnimatedNode other than a core.Constant.
func animated(v any) bool {
	switch x := v.(type) {
	case core.Constant:
		return false
	case core.AnimatedNode:
		return true
	case []core.AnimatedNode:
		return slices.ContainsFunc(x, func(n core.AnimatedNode) bool { return animated(n) })
	case []any:
		return slices.ContainsFunc(x, animated)
	}
	return false
}

// Float returns the number uniform u sets name to, for an effect's Reference: any of Go's integer or float kinds, a bool as 0 or 1, or a core.Constant. It is 0 when u leaves name out or sets it to anything else, such as a vector, which a Reference reads from u itself.
func Float(u core.Uniforms, name string) float64 {
	switch x := u[name].(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case uint:
		return float64(x)
	case uint32:
		return float64(x)
	case uint64:
		return float64(x)
	case bool:
		if x {
			return 1
		}
	case core.Constant:
		return float64(x)
	}
	return 0
}
