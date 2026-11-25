package navbake3d

import (
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
)

// DebugStyle sets how DrawDebug draws a mesh.
type DebugStyle struct {
	// PolygonColor strokes the polygons' outlines; 0 uses DefaultDebugStyle's.
	PolygonColor core.Color
	// DetailColor strokes the detail triangles' inner edges; 0 uses DefaultDebugStyle's.
	DetailColor core.Color
	// LineWidth is the stroke width in points; 0 or less uses DefaultDebugStyle's.
	LineWidth float32
	// HideDetail leaves the detail triangles out, drawing only the polygons.
	HideDetail bool
	// Lift raises every line by this many world units so it is not hidden in the ground it lies on.
	Lift float32
}

// DefaultDebugStyle returns a DebugStyle with high-contrast colors for drawing a mesh over a 3D scene.
func DefaultDebugStyle() DebugStyle {
	return DebugStyle{
		PolygonColor: core.RGB(0x22D3EE),
		DetailColor:  core.RGB(0x0E7490),
		LineWidth:    1.5,
		Lift:         0.05,
	}
}

// DebugLines returns the line segments that show m: first every polygon edge once, in polygon order, then, unless polygonsOnly is set, every edge inside a polygon's detail triangulation once. Edges shared by two polygons appear once.
func (m *Mesh) DebugLines(polygonsOnly bool) [][2]math3d.Vec3 {
	var out [][2]math3d.Vec3
	seen := make(map[[2]int]bool)
	for pi := range m.Polygons {
		p := &m.Polygons[pi]
		n := len(p.Vertices)
		for i := range n {
			a, b := p.Vertices[i], p.Vertices[(i+1)%n]
			k := [2]int{min(a, b), max(a, b)}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, [2]math3d.Vec3{m.Vertices[a].Vec3(), m.Vertices[b].Vec3()})
		}
	}
	if polygonsOnly {
		return out
	}
	for pi := range m.Polygons {
		p := &m.Polygons[pi]
		inner := make(map[[2]int]int)
		for _, t := range p.Triangles {
			for k := range 3 {
				a, b := t[k], t[(k+1)%3]
				inner[[2]int{min(a, b), max(a, b)}]++
			}
		}
		for _, t := range p.Triangles {
			v := m.TriangleVertices(p, t)
			for k := range 3 {
				a, b := t[k], t[(k+1)%3]
				key := [2]int{min(a, b), max(a, b)}
				// An edge two triangles share is inside the polygon; draw it from the first of them only.
				if inner[key] != 2 {
					continue
				}
				inner[key] = 0
				out = append(out, [2]math3d.Vec3{v[k], v[(k+1)%3]})
			}
		}
	}
	return out
}
