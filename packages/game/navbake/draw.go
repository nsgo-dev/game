package navbake

import (
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// Portal is the stretch of edge two polygons of a Mesh share, which an agent crosses to go from one to the other.
type Portal struct {
	// A and B are the ends of the shared stretch.
	A, B core.Point
	// From and To are the indices of the two polygons, From < To.
	From, To int
}

// portalEps is how far apart, in world units, two edges may be and still count as one.
const portalEps = 1e-4

// Portals returns the shared edges between the polygons of m, ordered by From then To. It buckets the polygons' bounds on a uniform grid and compares only polygons whose bounds touch, so it takes time near linear in the number of polygons for a baked mesh; a game that draws them every frame should still keep the result.
func (m *Mesh) Portals() []Portal {
	if m == nil || len(m.Polygons) < 2 {
		return nil
	}
	n := len(m.Polygons)
	// Each polygon's bounds grow by half of portalEps on every side, so two polygons are compared when their bounds come within portalEps.
	bs := make([]bounds, n)
	sizes := make([]float64, 0, n)
	for i, p := range m.Polygons {
		b := bounds{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for _, v := range p {
			x, y := float64(v.X), float64(v.Y)
			b = bounds{min(b.x0, x), min(b.y0, y), max(b.x1, x), max(b.y1, y)}
		}
		b = bounds{b.x0 - portalEps/2, b.y0 - portalEps/2, b.x1 + portalEps/2, b.y1 + portalEps/2}
		bs[i] = b
		if s := max(b.x1-b.x0, b.y1-b.y0); s > 0 && !math.IsInf(s, 0) {
			sizes = append(sizes, s)
		}
	}
	// The cell is the median polygon size, so a typical polygon covers a few cells; a polygon covering more than maxPortalCells cells is compared with every other one instead.
	cell := 1.0
	if len(sizes) > 0 {
		slices.Sort(sizes)
		cell = sizes[len(sizes)/2]
	}
	const maxPortalCells = 256
	type key struct{ x, y int64 }
	grid := make(map[key][]int)
	span := make([][4]int64, n)
	var large []int
	isLarge := make([]bool, n)
	for i, b := range bs {
		x0, y0, x1, y1 := math.Floor(b.x0/cell), math.Floor(b.y0/cell), math.Floor(b.x1/cell), math.Floor(b.y1/cell)
		if !(x0 >= -1<<52 && y0 >= -1<<52 && x1 <= 1<<52 && y1 <= 1<<52) || (x1-x0+1)*(y1-y0+1) > maxPortalCells {
			large = append(large, i)
			isLarge[i] = true
			continue
		}
		span[i] = [4]int64{int64(x0), int64(y0), int64(x1), int64(y1)}
		for cy := span[i][1]; cy <= span[i][3]; cy++ {
			for cx := span[i][0]; cx <= span[i][2]; cx++ {
				grid[key{cx, cy}] = append(grid[key{cx, cy}], i)
			}
		}
	}
	var pairs [][2]int
	for k, ids := range grid {
		for a, i := range ids {
			for _, j := range ids[a+1:] {
				if !bs[i].touches(bs[j]) {
					continue
				}
				// Two touching polygons share every cell their overlap covers; only the cell of the overlap's lower corner reports the pair.
				if max(span[i][0], span[j][0]) != k.x || max(span[i][1], span[j][1]) != k.y {
					continue
				}
				pairs = append(pairs, [2]int{min(i, j), max(i, j)})
			}
		}
	}
	for _, i := range large {
		for j := range n {
			if j == i || (isLarge[j] && j < i) || !bs[i].touches(bs[j]) {
				continue
			}
			pairs = append(pairs, [2]int{min(i, j), max(i, j)})
		}
	}
	slices.SortFunc(pairs, func(a, b [2]int) int {
		if a[0] != b[0] {
			return a[0] - b[0]
		}
		return a[1] - b[1]
	})
	var out []Portal
	for _, pr := range pairs {
		if a, b, ok := sharedEdge(m.Polygons[pr[0]], m.Polygons[pr[1]]); ok {
			out = append(out, Portal{A: a, B: b, From: pr[0], To: pr[1]})
		}
	}
	return out
}

// bounds is an axis-aligned box.
type bounds struct{ x0, y0, x1, y1 float64 }

// touches reports whether a and b overlap or touch.
func (a bounds) touches(b bounds) bool {
	return a.x1 >= b.x0 && b.x1 >= a.x0 && a.y1 >= b.y0 && b.y1 >= a.y0
}

// sharedEdge returns the overlap of an edge of p with an edge of q running the other way along the same line.
func sharedEdge(p, q Polygon) (core.Point, core.Point, bool) {
	for i := range p {
		p0, p1 := p[i], p[(i+1)%len(p)]
		ax, ay := float64(p0.X), float64(p0.Y)
		dx, dy := float64(p1.X)-ax, float64(p1.Y)-ay
		l := math.Hypot(dx, dy)
		if l <= portalEps {
			continue
		}
		ux, uy := dx/l, dy/l
		for j := range q {
			q0, q1 := q[j], q[(j+1)%len(q)]
			ex, ey := float64(q1.X)-float64(q0.X), float64(q1.Y)-float64(q0.Y)
			if ex*ux+ey*uy >= 0 {
				continue
			}
			bx, by := float64(q0.X)-ax, float64(q0.Y)-ay
			cx, cy := float64(q1.X)-ax, float64(q1.Y)-ay
			if math.Abs(bx*uy-by*ux) > portalEps || math.Abs(cx*uy-cy*ux) > portalEps {
				continue
			}
			t0, t1 := bx*ux+by*uy, cx*ux+cy*uy
			lo, hi := max(0, min(t0, t1)), min(l, max(t0, t1))
			if hi-lo <= portalEps {
				continue
			}
			at := func(t float64) core.Point {
				return core.Point{X: core.Points(ax + ux*t), Y: core.Points(ay + uy*t)}
			}
			return at(lo), at(hi), true
		}
	}
	return core.Point{}, core.Point{}, false
}

// Draw fills every polygon of m in fill, outlines it in edge and strokes the portals between polygons in portal, at lineWidth world units under whatever camera transform d already has. It is meant for debug overlays; a transparent color skips that layer. It finds the portals with Mesh.Portals on every call.
func Draw(d *core.Drawing, m *Mesh, fill, edge, portal core.Color, lineWidth float32) {
	if d == nil || m == nil || len(m.Polygons) == 0 {
		return
	}
	d.Save()
	path := func() {
		d.BeginPath()
		for _, p := range m.Polygons {
			for k, v := range p {
				if k == 0 {
					d.MoveTo(float32(v.X), float32(v.Y))
				} else {
					d.LineTo(float32(v.X), float32(v.Y))
				}
			}
			d.ClosePath()
		}
	}
	if fill&0xff != 0 {
		path()
		d.SetFillColor(fill).Fill()
	}
	if edge&0xff != 0 {
		path()
		d.SetStrokeColor(edge).SetLineWidth(lineWidth).Stroke()
	}
	if portal&0xff != 0 {
		d.BeginPath()
		for _, p := range m.Portals() {
			d.MoveTo(float32(p.A.X), float32(p.A.Y)).LineTo(float32(p.B.X), float32(p.B.Y))
		}
		d.SetStrokeColor(portal).SetLineWidth(lineWidth).Stroke()
	}
	d.Restore()
}
