package input

import (
	"math"
	"sort"
)

type Context struct {
	Name     string
	Priority int
	Enabled  bool
	Bindings map[string][]Input
}
type Contexts struct {
	contexts map[string]Context
	stack    []string
}

func NewContexts() *Contexts { return &Contexts{contexts: map[string]Context{}} }
func (c *Contexts) Add(ctx Context) {
	if c == nil || ctx.Name == "" {
		return
	}
	if !ctx.Enabled {
		ctx.Enabled = true
	}
	c.contexts[ctx.Name] = ctx
}
func (c *Contexts) Push(name string) bool {
	if c == nil {
		return false
	}
	ctx, ok := c.contexts[name]
	if !ok {
		return false
	}
	ctx.Enabled = true
	c.contexts[name] = ctx
	c.stack = append(c.stack, name)
	sort.SliceStable(c.stack, func(i, j int) bool { return c.contexts[c.stack[i]].Priority > c.contexts[c.stack[j]].Priority })
	return true
}
func (c *Contexts) Pop(name string) bool {
	if c == nil {
		return false
	}
	ctx, ok := c.contexts[name]
	if !ok {
		return false
	}
	ctx.Enabled = false
	c.contexts[name] = ctx
	for i, n := range c.stack {
		if n == name {
			c.stack = append(c.stack[:i], c.stack[i+1:]...)
			break
		}
	}
	return true
}
func (c *Contexts) Binding(name string) []Input {
	if c == nil {
		return nil
	}
	for _, n := range c.stack {
		ctx := c.contexts[n]
		if ctx.Enabled && len(ctx.Bindings[name]) > 0 {
			return append([]Input(nil), ctx.Bindings[name]...)
		}
	}
	return nil
}

type Vector2 struct{ X, Y float64 }

func NormalizeVector(v Vector2) Vector2 {
	l := math.Hypot(v.X, v.Y)
	if l > 1 {
		return Vector2{v.X / l, v.Y / l}
	}
	return v
}
