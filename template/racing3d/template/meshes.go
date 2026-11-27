package main

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
)

// The scene is drawn from a few meshes built in code: a unit cube and a unit wheel that the cars and the trackside props scale into place, and the ground, the road and its curbs built once from the course.

// meshBuilder appends flat-shaded quads to a mesh, each wound counter-clockwise seen from the side its normal points to, as render3d's back-face culling expects.
type meshBuilder struct{ m render3d.Mesh }

// quad adds the quad a, b, c, d (in order around its edge) facing n.
func (b *meshBuilder) quad(a, bb, c, d, n math3d.Vec3) {
	if bb.Sub(a).Cross(c.Sub(a)).Dot(n) < 0 {
		bb, d = d, bb
	}
	base := uint32(len(b.m.Positions))
	b.m.Positions = append(b.m.Positions, a, bb, c, d)
	b.m.Normals = append(b.m.Normals, n, n, n, n)
	b.m.Indices = append(b.m.Indices, base, base+1, base+2, base, base+2, base+3)
}

// tri adds the triangle a, b, c facing n.
func (b *meshBuilder) tri(a, bb, c, n math3d.Vec3) {
	if bb.Sub(a).Cross(c.Sub(a)).Dot(n) < 0 {
		bb, c = c, bb
	}
	base := uint32(len(b.m.Positions))
	b.m.Positions = append(b.m.Positions, a, bb, c)
	b.m.Normals = append(b.m.Normals, n, n, n)
	b.m.Indices = append(b.m.Indices, base, base+1, base+2)
}

// cubeMesh is a cube from -0.5 to 0.5 on each axis.
func cubeMesh() render3d.Mesh {
	var b meshBuilder
	v := func(x, y, z float32) math3d.Vec3 { return math3d.Vec3{X: x, Y: y, Z: z} }
	const h = 0.5
	b.quad(v(h, -h, -h), v(h, h, -h), v(h, h, h), v(h, -h, h), v(1, 0, 0))
	b.quad(v(-h, -h, -h), v(-h, -h, h), v(-h, h, h), v(-h, h, -h), v(-1, 0, 0))
	b.quad(v(-h, h, -h), v(-h, h, h), v(h, h, h), v(h, h, -h), v(0, 1, 0))
	b.quad(v(-h, -h, -h), v(h, -h, -h), v(h, -h, h), v(-h, -h, h), v(0, -1, 0))
	b.quad(v(-h, -h, h), v(h, -h, h), v(h, h, h), v(-h, h, h), v(0, 0, 1))
	b.quad(v(-h, -h, -h), v(-h, h, -h), v(h, h, -h), v(h, -h, -h), v(0, 0, -1))
	return b.m
}

// wheelMesh is a wheel of radius 1 and width 1 whose axle is the X axis, as vehicle.Vehicle.WheelTransform expects; its tread has eight flat faces, so its spin shows.
func wheelMesh() render3d.Mesh {
	const sides = 8
	var b meshBuilder
	rim := func(i int, x float32) math3d.Vec3 {
		a := 2 * math.Pi * float64(i) / sides
		return math3d.Vec3{X: x, Y: float32(math.Cos(a)), Z: float32(math.Sin(a))}
	}
	for i := range sides {
		a0, a1 := rim(i, -0.5), rim(i+1, -0.5)
		b0, b1 := rim(i, 0.5), rim(i+1, 0.5)
		mid := 2 * math.Pi * (float64(i) + 0.5) / sides
		b.quad(a0, a1, b1, b0, math3d.Vec3{Y: float32(math.Cos(mid)), Z: float32(math.Sin(mid))})
		b.tri(math3d.Vec3{X: -0.5}, a0, a1, math3d.Vec3{X: -1})
		b.tri(math3d.Vec3{X: 0.5}, b0, b1, math3d.Vec3{X: 1})
	}
	return b.m
}

// groundMesh is a square of the given half size on y = 0, facing up.
func groundMesh(half float32) render3d.Mesh {
	var b meshBuilder
	v := func(x, z float32) math3d.Vec3 { return math3d.Vec3{X: x, Z: z} }
	b.quad(v(-half, -half), v(-half, half), v(half, half), v(half, -half), math3d.Vec3Up())
	return b.m
}

// stripMesh is the bands along the course, each from band[0] to band[1] meters right of the centerline, height above the ground. A parity of 0 or 1 keeps only the pieces from the even or odd samples, which draws the curbs' stripes; -1 keeps every piece.
func stripMesh(c *course, bands [][2]float32, height float32, parity int) render3d.Mesh {
	var b meshBuilder
	up := math3d.Vec3Up()
	lift := up.Scale(height)
	for i := range c.samples {
		if parity >= 0 && i%2 != parity {
			continue
		}
		s0, s1 := c.samples[i], c.samples[c.wrap(i+1)]
		for _, band := range bands {
			b.quad(
				s0.pos.Add(s0.right.Scale(band[0])).Add(lift),
				s1.pos.Add(s1.right.Scale(band[0])).Add(lift),
				s1.pos.Add(s1.right.Scale(band[1])).Add(lift),
				s0.pos.Add(s0.right.Scale(band[1])).Add(lift),
				up,
			)
		}
	}
	return b.m
}
