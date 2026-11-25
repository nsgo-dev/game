package navbake

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// DefaultPrecision is the grid step, in world units, that Options.Precision defaults to.
const DefaultPrecision = 1.0 / 256

// Source is the level geometry a navigation mesh is baked from: the areas agents may walk and the obstacles inside them, as polygons in world units.
//
// Polygons may be concave, may overlap and may be wound either way: a point is walkable when it is inside the outlines and outside the obstacles under the nonzero winding rule. Two overlapping polygons wound in opposite directions cancel where they overlap, which is how a tilemap's walkable pocket inside a solid block is described.
type Source struct {
	// Outlines are the walkable areas, such as a level's outer boundary.
	Outlines [][]core.Point
	// Obstacles are the blocked areas inside the outlines, such as walls, pillars and holes.
	Obstacles [][]core.Point
}

// Agent is a kind of agent a mesh is baked for.
type Agent struct {
	// Name identifies the agent type, such as "small" or "tank"; BakeAll needs it unique and not empty.
	Name string
	// Radius is how far the agent's center keeps from every obstacle and from the outline's edges, in world units.
	Radius float32
}

// Options tunes a bake. The zero value is ready to use.
type Options struct {
	// Precision is the grid step that the bake snaps every point to, in world units; zero means DefaultPrecision. Coordinates, and the obstacles once grown by an agent's radius, must stay within 2^24 steps of the origin (65536 world units at DefaultPrecision), the largest grid that the float32 coordinates of a Mesh hold exactly; a gap or sliver of an obstacle thinner than one step may close or vanish.
	Precision float32
}

// Bake builds the navigation mesh of src for one agent: the walkable area, shrunk by the agent's radius, divided into convex polygons.
//
// Obstacles are inflated by the radius with their convex corners rounded, the result is triangulated with a constrained Delaunay triangulation and the triangles are merged into convex polygons. A path that NavMesh.FindPath finds on the result keeps at least Radius from every obstacle and outline edge. Baking the same input gives the same mesh, so its YAML is byte for byte the same.
func Bake(src Source, agent Agent, opts Options) (*Mesh, error) {
	meshes, err := bake(src, []Agent{agent}, opts)
	if err != nil {
		return nil, err
	}
	return meshes[0], nil
}

// BakeAll bakes one mesh per agent type from the same source, in the order of agents, sharing the work that does not depend on the radius.
func BakeAll(src Source, agents []Agent, opts Options) ([]*Mesh, error) {
	seen := make(map[string]bool, len(agents))
	for i, a := range agents {
		if a.Name == "" {
			return nil, fmt.Errorf("navbake: agent %d has no name", i)
		}
		if seen[a.Name] {
			return nil, fmt.Errorf("navbake: agent %q is listed twice", a.Name)
		}
		seen[a.Name] = true
	}
	return bake(src, agents, opts)
}

// maxGrid bounds the absolute value of every coordinate of a bake's input and output, in grid steps: float32 holds every integer up to 2^24, so a mesh within it keeps every grid vertex exactly when the grid step is a power of two and within half a step otherwise. It is far inside maxCoord, the range the exact predicates support.
const maxGrid = 1 << 24

// minRadiusSteps widens the inflation by this many grid steps, so snapping the offset geometry to the grid never brings it closer than the radius.
const minRadiusSteps = 2

// maxArcStep is the largest angle one segment of a rounded corner spans.
const maxArcStep = math.Pi / 8

func bake(src Source, agents []Agent, opts Options) ([]*Mesh, error) {
	step := float64(opts.Precision)
	if step == 0 {
		step = DefaultPrecision
	}
	if !(step > 0) || math.IsInf(step, 0) {
		return nil, fmt.Errorf("navbake: precision %v is not a positive finite number", opts.Precision)
	}
	for _, a := range agents {
		if !(a.Radius >= 0) || math.IsInf(float64(a.Radius), 0) {
			return nil, fmt.Errorf("navbake: agent %q has radius %v; want a finite number of 0 or more", a.Name, a.Radius)
		}
		if float64(a.Radius)/step > maxGrid {
			return nil, fmt.Errorf("navbake: agent %q has radius %v, too large for precision %v", a.Name, a.Radius, step)
		}
	}
	if len(src.Outlines) == 0 {
		return nil, errors.New("navbake: the source has no outline")
	}

	// Phase one resolves the outlines and obstacles into one blocked region.
	var segs []seg
	for i, poly := range src.Outlines {
		pts, err := snapPolygon(poly, step)
		if err != nil {
			return nil, fmt.Errorf("navbake: outline %d: %w", i, err)
		}
		segs = addLoop(segs, pts, counts{1, 0})
	}
	for i, poly := range src.Obstacles {
		pts, err := snapPolygon(poly, step)
		if err != nil {
			return nil, fmt.Errorf("navbake: obstacle %d: %w", i, err)
		}
		segs = addLoop(segs, pts, counts{0, 1})
	}
	base, err := triangulate(segs, counts{0, 0}, func(c counts) bool { return c[0] != 0 && c[1] == 0 })
	if err != nil {
		return nil, err
	}

	meshes := make([]*Mesh, len(agents))
	var boundary []dedge
	for k, a := range agents {
		r := float64(a.Radius) / step
		var polys [][]ipt
		if r == 0 {
			polys = base.convexPolygons()
		} else {
			if boundary == nil {
				boundary = base.boundary()
			}
			// Phase two inflates the blocked region by the radius.
			segs := inflate(base.cdt.pts, boundary, r+minRadiusSteps)
			if err := checkRange(segs, step); err != nil {
				return nil, err
			}
			grown, err := triangulate(segs, counts{1, 0}, func(c counts) bool { return c[0] == 0 && c[1] == 0 })
			if err != nil {
				return nil, err
			}
			polys = grown.convexPolygons()
		}
		meshes[k] = newMesh(a, polys, step)
	}
	return meshes, nil
}

// snapPolygon snaps the vertices of poly to the grid.
func snapPolygon(poly []core.Point, step float64) ([]ipt, error) {
	pts := make([]ipt, len(poly))
	for i, p := range poly {
		x, y := float64(p.X)/step, float64(p.Y)/step
		if math.IsNaN(x) || math.IsNaN(y) || math.Abs(x) > maxGrid || math.Abs(y) > maxGrid {
			return nil, fmt.Errorf("point %d (%v, %v) is not finite or too far from the origin for precision %v: coordinates must stay within 2^24 grid steps (%v world units), the range float32 holds exactly", i, p.X, p.Y, step, maxGrid*step)
		}
		pts[i] = ipt{int64(math.Round(x)), int64(math.Round(y))}
	}
	return pts, nil
}

// checkRange reports an error when a segment of the inflated geometry leaves the maxGrid range that the mesh's float32 coordinates hold exactly.
func checkRange(segs []seg, step float64) error {
	for _, s := range segs {
		for _, p := range [2]ipt{s.a, s.b} {
			if p.x > maxGrid || p.x < -maxGrid || p.y > maxGrid || p.y < -maxGrid {
				return fmt.Errorf("navbake: the geometry grown by the agent radius is too far from the origin for precision %v: coordinates must stay within 2^24 grid steps (%v world units), the range float32 holds exactly", step, maxGrid*step)
			}
		}
	}
	return nil
}

// region is a triangulation whose triangles are classified as walkable or not.
type region struct {
	cdt  *cdt
	walk []bool
}

// triangulate arranges segs, triangulates them inside a box around them, and classifies every triangle by the counts accumulated from the box's corner, where they are start, with walkable.
func triangulate(segs []seg, start counts, walkable func(counts) bool) (*region, error) {
	segs, err := arrange(segs)
	if err != nil {
		return nil, err
	}
	lo, hi := ipt{-1, -1}, ipt{1, 1}
	if len(segs) > 0 {
		lo, hi = segs[0].a, segs[0].a
	}
	for _, s := range segs {
		for _, p := range [2]ipt{s.a, s.b} {
			lo = ipt{min(lo.x, p.x), min(lo.y, p.y)}
			hi = ipt{max(hi.x, p.x), max(hi.y, p.y)}
		}
	}
	const margin = 64
	lo = ipt{lo.x - margin, lo.y - margin}
	hi = ipt{hi.x + margin, hi.y + margin}

	pts := make([]ipt, 0, 2*len(segs))
	for _, s := range segs {
		pts = append(pts, s.a, s.b)
	}
	slices.SortFunc(pts, func(p, q ipt) int {
		switch {
		case p == q:
			return 0
		case p.less(q):
			return -1
		}
		return 1
	})
	pts = slices.Compact(pts)
	c := newCDT(lo, hi, len(pts))
	ids := make(map[ipt]int32, len(pts))
	for _, p := range pts {
		ids[p] = int32(len(c.pts))
		c.insert(p)
	}
	type key struct{ a, b int32 }
	delta := make(map[key]counts, len(segs))
	for _, s := range segs {
		a, b := ids[s.a], ids[s.b]
		if err := c.insertConstraint(a, b); err != nil {
			return nil, err
		}
		delta[key{a, b}] = s.d
	}

	// Flood the counts from the corner triangle across every edge.
	n := len(c.tris)
	cnt := make([]counts, n)
	seen := make([]bool, n)
	queue := []int32{c.vt[0]}
	seen[c.vt[0]] = true
	cnt[c.vt[0]] = start
	for len(queue) > 0 {
		t := queue[0]
		queue = queue[1:]
		tr := &c.tris[t]
		for i := range 3 {
			s := tr.n[i]
			if s < 0 {
				continue
			}
			u, w := tr.v[next3[i]], tr.v[prev3[i]]
			next := cnt[t]
			if tr.c[i] {
				// t lies left of u->w, s right of it.
				if d, ok := delta[key{u, w}]; ok {
					next = next.sub(d)
				} else {
					next = next.add(delta[key{w, u}])
				}
			}
			if seen[s] {
				if cnt[s] != next {
					return nil, fmt.Errorf("%w: inconsistent region counts", errTriangulation)
				}
				continue
			}
			seen[s] = true
			cnt[s] = next
			queue = append(queue, s)
		}
	}
	walk := make([]bool, n)
	for t := range n {
		walk[t] = seen[t] && walkable(cnt[t])
	}
	return &region{cdt: c, walk: walk}, nil
}

// dedge is a directed edge between two vertices of a triangulation.
type dedge struct {
	a, b int32
}

// boundary returns the edges between walkable and blocked triangles, each directed with the blocked side on its left.
func (r *region) boundary() []dedge {
	var out []dedge
	for t, tr := range r.cdt.tris {
		if r.walk[t] {
			continue
		}
		for i := range 3 {
			if s := tr.n[i]; s >= 0 && r.walk[s] {
				out = append(out, dedge{tr.v[next3[i]], tr.v[prev3[i]]})
			}
		}
	}
	slices.SortFunc(out, func(e, f dedge) int {
		if e.a != f.a {
			return int(e.a - f.a)
		}
		return int(e.b - f.b)
	})
	return out
}

// inflate returns the segments of phase two: the blocked region's boundary edges, counted in the first counter, and, counted in the second, a rectangle of width radius on the free side of every edge and a rounded wedge at every convex corner of the blocked region, which together make the region grown by radius.
func inflate(pts []ipt, edges []dedge, radius float64) []seg {
	outs := make(map[int32][]int, len(edges))
	ins := make(map[int32][]int, len(edges))
	for i, e := range edges {
		outs[e.a] = append(outs[e.a], i)
		ins[e.b] = append(ins[e.b], i)
	}
	// A vertex in the middle of a straight run of boundary is merged away, so each run gets one rectangle.
	straight := func(v int32) bool {
		if len(outs[v]) != 1 || len(ins[v]) != 1 {
			return false
		}
		in, out := edges[ins[v][0]], edges[outs[v][0]]
		pa, pv, pb := pts[in.a], pts[v], pts[out.b]
		return orient(pa, pv, pb) == 0 && (pv.x-pa.x)*(pb.x-pv.x)+(pv.y-pa.y)*(pb.y-pv.y) > 0
	}
	type run struct {
		a, b   int32
		nx, ny float64
	}
	var runs []run
	runIn := make(map[int32]int)
	runOut := make(map[int32]int)
	for _, e := range edges {
		if straight(e.a) {
			continue
		}
		b := e.b
		for straight(b) {
			b = edges[outs[b][0]].b
		}
		pa, pb := pts[e.a], pts[b]
		dx, dy := float64(pb.x-pa.x), float64(pb.y-pa.y)
		l := math.Hypot(dx, dy)
		runOut[e.a] = len(runs)
		runIn[b] = len(runs)
		runs = append(runs, run{a: e.a, b: b, nx: dy / l, ny: -dx / l})
	}
	offset := func(v int32, nx, ny float64) ipt {
		p := pts[v]
		return ipt{int64(math.Round(float64(p.x) + radius*nx)), int64(math.Round(float64(p.y) + radius*ny))}
	}
	var segs []seg
	for _, r := range runs {
		segs = addSeg(segs, pts[r.a], pts[r.b], counts{1, 0})
		segs = addLoop(segs, []ipt{pts[r.a], offset(r.a, r.nx, r.ny), offset(r.b, r.nx, r.ny), pts[r.b]}, counts{0, 1})
	}
	arc := func(v int32, a0, sweep float64, segments int) []ipt {
		delta := sweep / float64(segments)
		// The polygon's edges touch the circle, so it holds the whole disc.
		rr := radius / math.Cos(delta/2)
		p := pts[v]
		out := make([]ipt, 0, segments)
		for m := range segments {
			ang := a0 + (float64(m)+0.5)*delta
			out = append(out, ipt{int64(math.Round(float64(p.x) + rr*math.Cos(ang))), int64(math.Round(float64(p.y) + rr*math.Sin(ang)))})
		}
		return out
	}
	verts := make([]int32, 0, len(outs))
	for v := range outs {
		verts = append(verts, v)
	}
	slices.Sort(verts)
	for _, v := range verts {
		if len(outs[v]) != 1 || len(ins[v]) != 1 {
			// Two blocked corners meet here: a whole disc covers both.
			segs = addLoop(segs, arc(v, 0, 2*math.Pi, 16), counts{0, 1})
			continue
		}
		ri, ok1 := runIn[v]
		ro, ok2 := runOut[v]
		if !ok1 || !ok2 {
			continue
		}
		in, out := runs[ri], runs[ro]
		turn := in.nx*out.ny - in.ny*out.nx
		if turn <= 0 || orient(pts[in.a], pts[v], pts[out.b]) <= 0 {
			// The two rectangles overlap at a concave corner, but where crossings snapped to the grid bend their ends next to v a sliver can open between them; a disc around v, well clear of any bend, closes it.
			if orient(pts[in.a], pts[v], pts[out.b]) < 0 {
				segs = addLoop(segs, arc(v, 0, 2*math.Pi, 16), counts{0, 1})
			}
			continue
		}
		a0 := math.Atan2(in.ny, in.nx)
		sweep := math.Atan2(turn, in.nx*out.nx+in.ny*out.ny)
		segments := max(int(math.Ceil(sweep/maxArcStep-1e-9)), 1)
		wedge := []ipt{pts[v], offset(v, in.nx, in.ny)}
		wedge = append(wedge, arc(v, a0, sweep, segments)...)
		wedge = append(wedge, offset(v, out.nx, out.ny))
		segs = addLoop(segs, wedge, counts{0, 1})
	}
	return segs
}
