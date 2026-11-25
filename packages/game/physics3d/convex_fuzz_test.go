package physics3d_test

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

// fuzzPoints decodes up to 40 points with coordinates in [-2, 2] from data.
func fuzzPoints(data []byte) []v3 {
	var pts []v3
	for len(data) >= 6 && len(pts) < 40 {
		c := func(i int) float32 { return float32(int16(binary.LittleEndian.Uint16(data[i:]))) / 16384 }
		pts = append(pts, v3{X: c(0), Y: c(2), Z: c(4)})
		data = data[6:]
	}
	return pts
}

// FuzzNewConvexHull checks that any point cloud either is rejected as degenerate or gives a closed hull with outward faces that contains every input point.
func FuzzNewConvexHull(f *testing.F) {
	f.Add([]byte("0123456789abcdefghijklmnopqrstuvwxyz0123456789"))
	f.Add(make([]byte, 60))
	// Points in line with a hull edge, and a near-duplicate point on a face's plane.
	f.Add([]byte("034567000X0aklmnopqrstuv000700"))
	f.Add([]byte("0123250007000a0a000A0A0X0A0A0X0 0p0A012305"))
	f.Add([]byte("0\xbf000000000 00100 0000A0010000001000"))
	f.Fuzz(func(t *testing.T, data []byte) {
		pts := fuzzPoints(data)
		h, err := physics3d.NewConvexHull(pts, physics3d.MaxHullVertices)
		if err != nil {
			if !errors.Is(err, physics3d.ErrDegenerateHull) {
				t.Fatalf("unexpected error %v", err)
			}
			return
		}
		if h.Volume() <= 0 || h.NumVertices() < 4 {
			t.Fatalf("hull with volume %v and %d vertices", h.Volume(), h.NumVertices())
		}
		verts := h.Vertices()
		normals := h.FaceNormals()
		// Every edge is shared by exactly two faces, in opposite directions.
		edges := map[[2]int]int{}
		for i, f := range h.Faces() {
			d := verts[f[0]].Dot(normals[i])
			for k, vi := range f {
				if math.Abs(float64(verts[vi].Dot(normals[i])-d)) > 1e-3 {
					t.Fatalf("face %d is not planar", i)
				}
				edges[[2]int{vi, f[(k+1)%len(f)]}]++
			}
			for _, p := range pts {
				if p.Dot(normals[i])-d > 1e-3 {
					t.Fatalf("point %v lies %v outside face %d", p, p.Dot(normals[i])-d, i)
				}
			}
		}
		for e, n := range edges {
			if n != 1 || edges[[2]int{e[1], e[0]}] != 1 {
				t.Fatalf("edge %v is not shared by two faces", e)
			}
		}
	})
}

// FuzzConvexContactSeparates checks that for any placement of a hull and a cylinder, a reported contact is consistent: moving the first shape by Normal*Depth leaves at most a sliver of overlap.
func FuzzConvexContactSeparates(f *testing.F) {
	f.Add(float32(0.5), float32(0.3), float32(0.1), float32(0.7), float32(1.2))
	f.Add(float32(0), float32(0), float32(0), float32(0), float32(0))
	f.Add(float32(0.1), float32(1.4), float32(-0.2), float32(3), float32(-0.4))
	hull, err := physics3d.NewConvexHull(rockPoints(7, 40, 1.2), 24)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, x, y, z, angle, tilt float32) {
		for _, v := range []float32{x, y, z, angle, tilt} {
			if math.IsNaN(float64(v)) || math.Abs(float64(v)) > 4 {
				return
			}
		}
		a := physics3d.ConvexHullShape(hull).Transformed(v3{X: x, Y: y, Z: z}, physics3d.QuatFromAxisAngle(v3{X: 1, Y: tilt, Z: 0.3}, angle))
		shapes := []physics3d.Shape{
			physics3d.CylinderShape(physics3d.Cylinder{A: v3{Y: -0.5}, B: v3{X: 0.2, Y: 0.5}, Radius: 0.4}),
			physics3d.ConvexHullShape(hull),
			physics3d.BoxShape(physics3d.Box{HalfExtents: v3{X: 0.6, Y: 0.3, Z: 0.5}}).Transformed(v3{}, physics3d.QuatFromAxisAngle(v3{Y: 1}, tilt)),
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
			physics3d.CapsuleShape(physics3d.Capsule{A: v3{X: -0.5}, B: v3{X: 0.5, Y: 0.2}, Radius: 0.3}),
		}
		for i, b := range shapes {
			c := physics3d.TestShapes(a, b)
			if !c.Hit {
				continue
			}
			if !(c.Depth > 0) || math.Abs(float64(c.Normal.Len()-1)) > 1e-3 {
				t.Fatalf("shape %d: bad contact %+v", i, c)
			}
			moved := physics3d.TestShapes(a.Translated(c.Normal.Scale(c.Depth)), b)
			if moved.Hit && moved.Depth > 0.01 {
				t.Fatalf("shape %d: moving by %v*%v leaves depth %v", i, c.Normal, c.Depth, moved.Depth)
			}
			// The depth is not overstated: moving by most of it along the normal still leaves the shapes overlapping.
			if c.Depth > 0.05 {
				if short := physics3d.TestShapes(a.Translated(c.Normal.Scale(c.Depth*0.9)), b); !short.Hit {
					t.Fatalf("shape %d: moving by %v*0.9*%v already separates the shapes", i, c.Normal, c.Depth)
				}
			}
		}
	})
}
