package ai

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// The steering, avoidance and perception code shares these float64 vector helpers with game/pathfind, which keeps its own private copy; neither package exports them.

type vec2 struct {
	x, y float64
}

func toVec2(p core.Point) vec2 { return vec2{float64(p.X), float64(p.Y)} }

func toPoint(v vec2) core.Point { return core.Point{X: core.Points(v.x), Y: core.Points(v.y)} }

func (a vec2) add(b vec2) vec2      { return vec2{a.x + b.x, a.y + b.y} }
func (a vec2) sub(b vec2) vec2      { return vec2{a.x - b.x, a.y - b.y} }
func (a vec2) scale(s float64) vec2 { return vec2{float64(a.x * s), float64(a.y * s)} }
func (a vec2) dot(b vec2) float64   { return float64(a.x*b.x) + float64(a.y*b.y) }
func (a vec2) cross(b vec2) float64 { return float64(a.x*b.y) - float64(a.y*b.x) }
func (a vec2) lenSq() float64       { return float64(a.x*a.x) + float64(a.y*a.y) }
func (a vec2) len() float64         { return math.Hypot(a.x, a.y) }

func dist(a, b vec2) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }
func distSq(a, b vec2) float64 {
	dx, dy := a.x-b.x, a.y-b.y
	return float64(dx*dx) + float64(dy*dy)
}

func closestPointOnSegment(p, a, b vec2) vec2 {
	ab := b.sub(a)
	l2 := ab.lenSq()
	if l2 <= 1e-18 {
		return a
	}
	t := p.sub(a).dot(ab) / l2
	t = min(max(t, 0), 1)
	return a.add(ab.scale(t))
}

func isFinite32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
