package sceneformat

import (
	"fmt"
	"slices"
)

// Point is a 2D point in an object's local coordinate space, used by FieldPoint and FieldPolygon component fields.
type Point struct {
	// X is the horizontal coordinate in object-local space.
	X float64 `yaml:"x"`
	// Y is the vertical coordinate in object-local space.
	Y float64 `yaml:"y"`
}

// MarshalYAML encodes Point with deterministic key order matching YAML map decoding ("x", "y").
func (p Point) MarshalYAML() (any, error) {
	return map[string]any{
		"x": normZero64(p.X),
		"y": normZero64(p.Y),
	}, nil
}

// Rect is an axis-aligned 2D rectangle in an object's local coordinate space, used by FieldRect component fields.
type Rect struct {
	// X is the left edge coordinate in object-local space.
	X float64 `yaml:"x"`
	// Y is the top edge coordinate in object-local space.
	Y float64 `yaml:"y"`
	// Width is the horizontal extent in object-local space (non-negative).
	Width float64 `yaml:"width" schema:"minimum=0"`
	// Height is the vertical extent in object-local space (non-negative).
	Height float64 `yaml:"height" schema:"minimum=0"`
}

// MarshalYAML encodes Rect with deterministic alphabetical key order matching YAML map decoding ("height", "width", "x", "y").
func (r Rect) MarshalYAML() (any, error) {
	return map[string]any{
		"height": normZero64(r.Height),
		"width":  normZero64(r.Width),
		"x":      normZero64(r.X),
		"y":      normZero64(r.Y),
	}, nil
}

// Circle is a 2D circle in an object's local coordinate space, used by FieldCircle component fields.
type Circle struct {
	// X is the horizontal center coordinate in object-local space.
	X float64 `yaml:"x"`
	// Y is the vertical center coordinate in object-local space.
	Y float64 `yaml:"y"`
	// Radius is the circle's radius in object-local space (non-negative).
	Radius float64 `yaml:"radius" schema:"minimum=0"`
}

// MarshalYAML encodes Circle with deterministic alphabetical key order matching YAML map decoding ("radius", "x", "y").
func (c Circle) MarshalYAML() (any, error) {
	return map[string]any{
		"radius": normZero64(c.Radius),
		"x":      normZero64(c.X),
		"y":      normZero64(c.Y),
	}, nil
}

// normZero64 turns -0.0 into +0.0 so YAML encoding never emits "-0".
func normZero64(v float64) float64 {
	if v == 0 {
		return 0
	}
	return v
}

func checkPoint(v any) (Point, string) {
	switch x := v.(type) {
	case Point:
		if !isFinite64(x.X) || !isFinite64(x.Y) {
			return Point{}, "point coordinates must be finite"
		}
		return Point{X: normZero64(x.X), Y: normZero64(x.Y)}, ""
	case map[string]any:
		var p Point
		for _, k := range sortedKeys(x) {
			switch k {
			case "x":
				n, msg := checkFiniteFloat("x", x[k])
				if msg != "" {
					return Point{}, msg
				}
				p.X = n
			case "y":
				n, msg := checkFiniteFloat("y", x[k])
				if msg != "" {
					return Point{}, msg
				}
				p.Y = n
			default:
				return Point{}, fmt.Sprintf("unknown key %q (expected x, y)", k)
			}
		}
		return p, ""
	default:
		return Point{}, "want point {x, y}, got " + describeValue(v)
	}
}

func checkRect(v any) (Rect, string) {
	switch x := v.(type) {
	case Rect:
		if !isFinite64(x.X) || !isFinite64(x.Y) || !isFinite64(x.Width) || !isFinite64(x.Height) {
			return Rect{}, "rect values must be finite"
		}
		if x.Width < 0 {
			return Rect{}, fmt.Sprintf("width %v must be >= 0", x.Width)
		}
		if x.Height < 0 {
			return Rect{}, fmt.Sprintf("height %v must be >= 0", x.Height)
		}
		return Rect{
			X:      normZero64(x.X),
			Y:      normZero64(x.Y),
			Width:  normZero64(x.Width),
			Height: normZero64(x.Height),
		}, ""
	case map[string]any:
		var r Rect
		for _, k := range sortedKeys(x) {
			switch k {
			case "x":
				n, msg := checkFiniteFloat("x", x[k])
				if msg != "" {
					return Rect{}, msg
				}
				r.X = n
			case "y":
				n, msg := checkFiniteFloat("y", x[k])
				if msg != "" {
					return Rect{}, msg
				}
				r.Y = n
			case "width":
				n, msg := checkFiniteFloat("width", x[k])
				if msg != "" {
					return Rect{}, msg
				}
				if n < 0 {
					return Rect{}, fmt.Sprintf("width %v must be >= 0", n)
				}
				r.Width = n
			case "height":
				n, msg := checkFiniteFloat("height", x[k])
				if msg != "" {
					return Rect{}, msg
				}
				if n < 0 {
					return Rect{}, fmt.Sprintf("height %v must be >= 0", n)
				}
				r.Height = n
			default:
				return Rect{}, fmt.Sprintf("unknown key %q (expected x, y, width, height)", k)
			}
		}
		return r, ""
	default:
		return Rect{}, "want rect {x, y, width, height}, got " + describeValue(v)
	}
}

func checkCircle(v any) (Circle, string) {
	switch x := v.(type) {
	case Circle:
		if !isFinite64(x.X) || !isFinite64(x.Y) || !isFinite64(x.Radius) {
			return Circle{}, "circle values must be finite"
		}
		if x.Radius < 0 {
			return Circle{}, fmt.Sprintf("radius %v must be >= 0", x.Radius)
		}
		return Circle{
			X:      normZero64(x.X),
			Y:      normZero64(x.Y),
			Radius: normZero64(x.Radius),
		}, ""
	case map[string]any:
		var c Circle
		for _, k := range sortedKeys(x) {
			switch k {
			case "x":
				n, msg := checkFiniteFloat("x", x[k])
				if msg != "" {
					return Circle{}, msg
				}
				c.X = n
			case "y":
				n, msg := checkFiniteFloat("y", x[k])
				if msg != "" {
					return Circle{}, msg
				}
				c.Y = n
			case "radius":
				n, msg := checkFiniteFloat("radius", x[k])
				if msg != "" {
					return Circle{}, msg
				}
				if n < 0 {
					return Circle{}, fmt.Sprintf("radius %v must be >= 0", n)
				}
				c.Radius = n
			default:
				return Circle{}, fmt.Sprintf("unknown key %q (expected x, y, radius)", k)
			}
		}
		return c, ""
	default:
		return Circle{}, "want circle {x, y, radius}, got " + describeValue(v)
	}
}

func checkPolygon(v any) ([]Point, string) {
	if v == nil {
		return []Point(nil), ""
	}
	switch xs := v.(type) {
	case []Point:
		if xs == nil {
			return nil, ""
		}
		out := make([]Point, len(xs))
		for i, pt := range xs {
			p, msg := checkPoint(pt)
			if msg != "" {
				return nil, fmt.Sprintf("point %d: %s", i, msg)
			}
			out[i] = p
		}
		return out, ""
	case []any:
		out := make([]Point, len(xs))
		for i, elem := range xs {
			p, msg := checkPoint(elem)
			if msg != "" {
				return nil, fmt.Sprintf("point %d: %s", i, msg)
			}
			out[i] = p
		}
		return out, ""
	default:
		return nil, "want polygon (list of {x, y} points), got " + describeValue(v)
	}
}

func checkFiniteFloat(key string, v any) (float64, string) {
	n, ok := toFloat(v)
	if !ok {
		return 0, fmt.Sprintf("%s: want number, got %s", key, describeValue(v))
	}
	if !isFinite64(n) {
		return 0, fmt.Sprintf("%s: value %v must be finite", key, n)
	}
	return normZero64(n), ""
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// ParsePoint converts a component field value (Point or decoded YAML map) to a validated Point.
func ParsePoint(v any) (Point, error) {
	p, msg := checkPoint(v)
	if msg != "" {
		return Point{}, fmt.Errorf("sceneformat: %s", msg)
	}
	return p, nil
}

// ParseRect converts a component field value (Rect or decoded YAML map) to a validated Rect.
func ParseRect(v any) (Rect, error) {
	r, msg := checkRect(v)
	if msg != "" {
		return Rect{}, fmt.Errorf("sceneformat: %s", msg)
	}
	return r, nil
}

// ParseCircle converts a component field value (Circle or decoded YAML map) to a validated Circle.
func ParseCircle(v any) (Circle, error) {
	c, msg := checkCircle(v)
	if msg != "" {
		return Circle{}, fmt.Errorf("sceneformat: %s", msg)
	}
	return c, nil
}

// ParsePolygon converts a component field value ([]Point or decoded YAML sequence of maps) to a validated []Point.
func ParsePolygon(v any) ([]Point, error) {
	pts, msg := checkPolygon(v)
	if msg != "" {
		return nil, fmt.Errorf("sceneformat: %s", msg)
	}
	return pts, nil
}

func cloneFieldDefault(v any) any {
	switch x := v.(type) {
	case []string:
		return slices.Clone(x)
	case [][2]float64:
		return slices.Clone(x)
	case []Point:
		return slices.Clone(x)
	default:
		return v
	}
}
