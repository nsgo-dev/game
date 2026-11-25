package scenemodel

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// Tessellation of the round primitives: segments around the Y axis and rings from pole to pole.
const (
	roundSegments = 32
	sphereRings   = 16
	capsuleRings  = 8 // rings in each hemisphere of a capsule
)

// PrimitiveMesh returns the geometry of the built-in primitive name, one of sceneformat.MeshPrimitives, in the object's space: indexed triangles wound counter-clockwise from outside, with unit normals and UVs from 0 to 1. Every call returns new slices.
//
// The shapes are sized like Godot's default meshes so that an object's scale reads as meters: a box 1 on each side, a sphere 1 across, a plane 1 by 1 in the XZ plane facing +Y, a cylinder 1 across and 1 tall, and a capsule 1 across and 2 tall, each centered on the origin with its round axis along Y.
func PrimitiveMesh(name string) (render3d.Mesh, error) {
	switch name {
	case sceneformat.MeshBox:
		return boxMesh(), nil
	case sceneformat.MeshSphere:
		return sphereMesh(), nil
	case sceneformat.MeshPlane:
		return planeMesh(), nil
	case sceneformat.MeshCylinder:
		return cylinderMesh(), nil
	case sceneformat.MeshCapsule:
		return capsuleMesh(), nil
	}
	return render3d.Mesh{}, fmt.Errorf("scenemodel: %q is not a built-in primitive (want one of %v)", name, sceneformat.MeshPrimitives())
}

// meshBuilder appends vertices and triangles to a render3d.Mesh.
type meshBuilder struct {
	m render3d.Mesh
}

// vertex appends one vertex and returns its index.
func (b *meshBuilder) vertex(p, n math3d.Vec3, uv math3d.Vec2) uint32 {
	b.m.Positions = append(b.m.Positions, p)
	b.m.Normals = append(b.m.Normals, n)
	b.m.UVs = append(b.m.UVs, uv)
	return uint32(len(b.m.Positions) - 1)
}

// quad appends the two triangles of the quad a, b, c, d, given counter-clockwise from outside.
func (b *meshBuilder) quad(a, bb, c, d uint32) {
	b.m.Indices = append(b.m.Indices, a, bb, c, a, c, d)
}

// boxMesh returns a 1 by 1 by 1 cube with its own four vertices on each face, so each face is flat-shaded and holds the whole texture.
func boxMesh() render3d.Mesh {
	var b meshBuilder
	// Each face: its outward normal and the two axes its UVs run along (u to the right, v down as seen from outside).
	faces := [6]struct{ n, u, v math3d.Vec3 }{
		{math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3{Y: -1}},
		{math3d.Vec3{X: -1}, math3d.Vec3{Z: 1}, math3d.Vec3{Y: -1}},
		{math3d.Vec3{Y: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: 1}},
		{math3d.Vec3{Y: -1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}},
		{math3d.Vec3{Z: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Y: -1}},
		{math3d.Vec3{Z: -1}, math3d.Vec3{X: -1}, math3d.Vec3{Y: -1}},
	}
	for _, f := range faces {
		center := f.n.Scale(0.5)
		corner := func(su, sv float32) uint32 {
			p := center.Add(f.u.Scale(su * 0.5)).Add(f.v.Scale(sv * 0.5))
			return b.vertex(p, f.n, math3d.Vec2{X: (su + 1) / 2, Y: (sv + 1) / 2})
		}
		// Top-left, bottom-left, bottom-right, top-right as seen from outside: counter-clockwise.
		tl, bl, br, tr := corner(-1, -1), corner(-1, 1), corner(1, 1), corner(1, -1)
		b.quad(tl, bl, br, tr)
	}
	return b.m
}

// planeMesh returns a 1 by 1 square in the XZ plane facing +Y, with U along +X and V along +Z.
func planeMesh() render3d.Mesh {
	var b meshBuilder
	up := math3d.Vec3{Y: 1}
	a := b.vertex(math3d.Vec3{X: -0.5, Z: -0.5}, up, math3d.Vec2{})
	c := b.vertex(math3d.Vec3{X: -0.5, Z: 0.5}, up, math3d.Vec2{Y: 1})
	d := b.vertex(math3d.Vec3{X: 0.5, Z: 0.5}, up, math3d.Vec2{X: 1, Y: 1})
	e := b.vertex(math3d.Vec3{X: 0.5, Z: -0.5}, up, math3d.Vec2{X: 1})
	b.quad(a, c, d, e)
	return b.m
}

// ring returns the sine and cosine of segment i of roundSegments around the Y axis: the direction (sin, 0, cos) starts at +Z and turns towards +X as i and U grow.
func ring(i int) (sin, cos float32) {
	a := 2 * math.Pi * float64(i) / roundSegments
	return float32(math.Sin(a)), float32(math.Cos(a))
}

// sphereMesh returns a UV sphere of radius 0.5.
func sphereMesh() render3d.Mesh {
	var b meshBuilder
	latLong(&b, sphereRings, func(r int) (y, radius, ny, nr, v float32) {
		phi := math.Pi * float64(r) / sphereRings
		s, c := float32(math.Sin(phi)), float32(math.Cos(phi))
		return 0.5 * c, 0.5 * s, c, s, float32(r) / sphereRings
	})
	return b.m
}

// capsuleMesh returns a capsule of radius 0.5 and total height 2: a cylinder 1 tall between two hemispheres. V runs from the top pole (0) to the bottom pole (1) in proportion to the distance along the surface's profile.
func capsuleMesh() render3d.Mesh {
	var b meshBuilder
	const (
		radius = 0.5
		half   = 0.5 // half the height of the cylinder between the hemispheres
	)
	arc := math.Pi / 2 * radius
	total := 2*arc + 2*half
	rows := 2*capsuleRings + 1
	latLong(&b, rows, func(r int) (y, rad, ny, nr, v float32) {
		// Rows 0..capsuleRings are the top hemisphere, capsuleRings+1..rows the bottom one, so the cylinder is the band between rows capsuleRings and capsuleRings+1.
		if r <= capsuleRings {
			phi := math.Pi / 2 * float64(r) / capsuleRings
			s, c := math.Sin(phi), math.Cos(phi)
			return float32(half + radius*c), float32(radius * s), float32(c), float32(s), float32(arc * float64(r) / capsuleRings / total)
		}
		k := r - capsuleRings - 1
		phi := math.Pi/2 + math.Pi/2*float64(k)/capsuleRings
		s, c := math.Sin(phi), math.Cos(phi)
		return float32(-half + radius*c), float32(radius * s), float32(c), float32(s), float32((arc + 2*half + arc*float64(k)/capsuleRings) / total)
	})
	return b.m
}

// latLong appends a surface of revolution about the Y axis with rows+1 rings of roundSegments+1 vertices (the seam is duplicated so U runs from 0 to 1), from the top row down: row r is at height y with radius rad, its normal has vertical part ny and horizontal part nr, and its V is v. A row whose radius rounds to 0 is a pole, and the triangles that would collapse there are left out.
func latLong(b *meshBuilder, rows int, row func(r int) (y, rad, ny, nr, v float32)) {
	start := uint32(len(b.m.Positions))
	radii := make([]float32, rows+1)
	for r := 0; r <= rows; r++ {
		y, rad, ny, nr, v := row(r)
		if rad < 1e-6 {
			rad, nr = 0, 0 // sin(pi) is not exactly 0
		}
		radii[r] = rad
		for i := 0; i <= roundSegments; i++ {
			s, c := ring(i)
			if i == roundSegments {
				s, c = 0, 1 // close the seam exactly
			}
			p := math3d.Vec3{X: rad * s, Y: y, Z: rad * c}
			n := math3d.Vec3{X: nr * s, Y: ny, Z: nr * c}
			b.vertex(p, n.Norm(), math3d.Vec2{X: float32(i) / roundSegments, Y: v})
		}
	}
	const stride = roundSegments + 1
	for r := 0; r < rows; r++ {
		for i := 0; i < roundSegments; i++ {
			a := start + uint32(r*stride+i)       // upper row, this segment
			d := start + uint32(r*stride+i+1)     // upper row, next segment
			c := start + uint32((r+1)*stride+i+1) // lower row, next segment
			bl := start + uint32((r+1)*stride+i)  // lower row, this segment
			if radii[r] != 0 {
				b.m.Indices = append(b.m.Indices, a, bl, d)
			}
			if radii[r+1] != 0 {
				b.m.Indices = append(b.m.Indices, d, bl, c)
			}
		}
	}
}

// cylinderMesh returns a cylinder of radius 0.5 and height 1 with flat caps. The side's U runs around it and V from top (0) to bottom (1); each cap maps the unit square onto its disc.
func cylinderMesh() render3d.Mesh {
	var b meshBuilder
	const radius, half = 0.5, 0.5
	latLong(&b, 1, func(r int) (y, rad, ny, nr, v float32) {
		if r == 0 {
			return half, radius, 0, 1, 0
		}
		return -half, radius, 0, 1, 1
	})
	for _, top := range []bool{true, false} {
		y, ny := float32(half), float32(1)
		if !top {
			y, ny = -half, -1
		}
		n := math3d.Vec3{Y: ny}
		center := b.vertex(math3d.Vec3{Y: y}, n, math3d.Vec2{X: 0.5, Y: 0.5})
		first := uint32(len(b.m.Positions))
		for i := 0; i <= roundSegments; i++ {
			s, c := ring(i)
			if i == roundSegments {
				s, c = 0, 1
			}
			b.vertex(math3d.Vec3{X: radius * s, Y: y, Z: radius * c}, n, math3d.Vec2{X: 0.5 + s/2, Y: 0.5 + c*ny/2})
		}
		for i := uint32(0); i < roundSegments; i++ {
			if top {
				b.m.Indices = append(b.m.Indices, center, first+i, first+i+1)
			} else {
				b.m.Indices = append(b.m.Indices, center, first+i+1, first+i)
			}
		}
	}
	return b.m
}
