package navbake3d

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// The vector helpers below round every product before adding it, by converting it explicitly, which the Go spec says forbids fusing a multiply and an add into one instruction. arm64 fuses them and amd64 does not, so without the conversions the same level would bake to different bytes on the two. The bake uses these rather than math3d's Cross, LenSq and Norm, which leave fusion to the compiler.

// cross returns the cross product a × b.
func cross(a, b math3d.Vec3) math3d.Vec3 {
	return math3d.Vec3{
		X: float32(a.Y*b.Z) - float32(a.Z*b.Y),
		Y: float32(a.Z*b.X) - float32(a.X*b.Z),
		Z: float32(a.X*b.Y) - float32(a.Y*b.X),
	}
}

// lenSq returns the squared length of v.
func lenSq(v math3d.Vec3) float32 {
	return float32(v.X*v.X) + float32(v.Y*v.Y) + float32(v.Z*v.Z)
}

// norm returns v scaled to unit length, or the zero vector when v is zero.
func norm(v math3d.Vec3) math3d.Vec3 {
	l := float32(math.Sqrt(float64(lenSq(v))))
	if l == 0 {
		return math3d.Vec3{}
	}
	inv := 1 / l
	return math3d.Vec3{X: v.X * inv, Y: v.Y * inv, Z: v.Z * inv}
}

// madd returns a + b*s with the product rounded first.
func madd(a, b math3d.Vec3, s float32) math3d.Vec3 {
	return math3d.Vec3{X: a.X + float32(b.X*s), Y: a.Y + float32(b.Y*s), Z: a.Z + float32(b.Z*s)}
}

// transformPoint returns m applied to the point p, as math3d's Mat4.TransformPoint does, including its divide by w.
func transformPoint(m math3d.Mat4, p math3d.Vec3) math3d.Vec3 {
	row := func(i int) float32 {
		return float32(m[i]*p.X) + float32(m[4+i]*p.Y) + float32(m[8+i]*p.Z) + m[12+i]
	}
	x, y, z, w := row(0), row(1), row(2), row(3)
	if w != 1 && w != 0 {
		invW := 1 / w
		return math3d.Vec3{X: x * invW, Y: y * invW, Z: z * invW}
	}
	return math3d.Vec3{X: x, Y: y, Z: z}
}
