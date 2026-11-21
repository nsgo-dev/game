package particles

import (
	"math"
	"math/rand/v2"
)

type shapeKind uint8

const (
	shapePoint shapeKind = iota
	shapeCircle
	shapeRing
	shapeRect
)

// Shape describes where an Emitter places each new particle relative to its X, Y position.
// Its zero value is Point, which emits at X, Y.
type Shape struct {
	kind          shapeKind
	radius        float32
	width, height float32
}

// Point returns a shape that emits particles at the emitter's X, Y position.
func Point() Shape { return Shape{kind: shapePoint} }

// Circle returns a shape that emits particles uniformly over a disk of radius centered on X, Y.
func Circle(radius float32) Shape { return Shape{kind: shapeCircle, radius: radius} }

// Ring returns a shape that emits particles uniformly on the circumference of a circle of radius centered on X, Y.
func Ring(radius float32) Shape { return Shape{kind: shapeRing, radius: radius} }

// Rect returns a shape that emits particles uniformly inside a width by height rectangle centered on X, Y.
func Rect(width, height float32) Shape { return Shape{kind: shapeRect, width: width, height: height} }

func (s Shape) sample(rng *rand.Rand) (dx, dy float32) {
	switch s.kind {
	case shapeCircle:
		if s.radius <= 0 {
			return 0, 0
		}
		angle := rng.Float64() * 2 * math.Pi
		dist := float64(s.radius) * math.Sqrt(rng.Float64())
		sin, cos := math.Sincos(angle)
		return float32(cos * dist), float32(sin * dist)
	case shapeRing:
		if s.radius <= 0 {
			return 0, 0
		}
		angle := rng.Float64() * 2 * math.Pi
		sin, cos := math.Sincos(angle)
		r := float64(s.radius)
		return float32(cos * r), float32(sin * r)
	case shapeRect:
		if s.width > 0 {
			dx = (rng.Float32() - 0.5) * s.width
		}
		if s.height > 0 {
			dy = (rng.Float32() - 0.5) * s.height
		}
		return dx, dy
	default:
		return 0, 0
	}
}
