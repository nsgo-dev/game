package input

import "math"

type InputContext struct {
	Name     string
	Priority int
	Enabled  bool
}
type VectorBinding struct {
	X, Y      string
	Normalize bool
}
type Vector2 struct{ X, Y float64 }

func (a *Actions) PushContext(c InputContext) {
	if a == nil || c.Name == "" {
		return
	}
	if a.contexts == nil {
		a.contexts = map[string]InputContext{}
	}
	c.Enabled = true
	a.contexts[c.Name] = c
}
func (a *Actions) PopContext(n string) {
	if a != nil {
		delete(a.contexts, n)
	}
}
func (a *Actions) SetContextEnabled(n string, e bool) {
	if a == nil {
		return
	}
	if c, ok := a.contexts[n]; ok {
		c.Enabled = e
		a.contexts[n] = c
	}
}
func (a *Actions) Contexts() []InputContext {
	if a == nil {
		return nil
	}
	o := []InputContext{}
	for _, c := range a.contexts {
		if c.Enabled {
			o = append(o, c)
		}
	}
	return o
}
func (a *Actions) BindVector(n string, b VectorBinding) {
	if a == nil || n == "" {
		return
	}
	if a.vectors == nil {
		a.vectors = map[string]VectorBinding{}
	}
	b.Normalize = true
	a.vectors[n] = b
}
func (a *Actions) Vector(n string) Vector2 {
	if a == nil {
		return Vector2{}
	}
	b, ok := a.vectors[n]
	if !ok {
		return Vector2{}
	}
	v := Vector2{a.Axis(b.X), a.Axis(b.Y)}
	if b.Normalize {
		d := math.Hypot(v.X, v.Y)
		if d > 1 {
			v.X /= d
			v.Y /= d
		}
	}
	return v
}
