package physics3d

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// Convex hulls (#808). A ConvexHull is built once from a point cloud by a deterministic quickhull and is then shared, read-only, by every Shape and Body that uses it, so moving or copying a hull never copies its vertices.

const (
	// DefaultHullVertices is the vertex limit NewConvexHull uses when given a limit of 0 or less.
	DefaultHullVertices = 64
	// MaxHullVertices is the largest vertex limit NewConvexHull accepts; larger limits are lowered to it.
	MaxHullVertices = 256
)

// ErrDegenerateHull is returned by NewConvexHull and ConvexHullFromMesh when the points do not span a volume: fewer than four of them, all on one plane, line or point, or not finite.
var ErrDegenerateHull = errors.New("physics3d: points do not span a convex volume")

// ConvexHull is a convex polyhedron for collision. Build one with NewConvexHull or ConvexHullFromMesh; the zero value is empty and collides with nothing.
//
// The hull's vertices are fixed when it is built and are relative to Center, which Translated and Shape.Transformed move. A Shape that wraps a hull may also set Rot, which turns the hull about Center, as it does for a Box.
type ConvexHull struct {
	// Center is where the hull's local origin sits.
	Center Vec3
	geom   *hullGeom
}

// hullFace is one planar face of a hull: its vertex indices counter-clockwise seen from outside, its outward unit normal and its plane offset (n·v for its vertices).
type hullFace struct {
	idx []int
	n   dv
	d   float64
}

// hullGeom is the immutable geometry of a ConvexHull in its local frame.
type hullGeom struct {
	v     []dv
	faces []hullFace
	edges [][2]int
	// volume, centroid and inertia (per unit mass, about the centroid) are the hull's mass properties for uniform density.
	volume   float64
	centroid dv
	inertia  [3][3]float64
	// radius is the largest distance from the local origin to a vertex; halfWidth is half the hull's smallest width across a face.
	radius    float64
	halfWidth float64
}

// NewConvexHull returns the convex hull of points, keeping at most maxVertices of them as its vertices (DefaultHullVertices when maxVertices <= 0, and never more than MaxHullVertices). A limit of 1 to 3 is raised to 4, the fewest vertices a hull with volume has.
//
// The hull is built by quickhull, which adds the point furthest outside the hull so far until none is left or the limit is reached. When the limit stops it early, the hull is the hull of the points kept: it lies inside the full hull and keeps its most prominent corners. Points within a small tolerance of a face are dropped, and coplanar triangles are merged into polygon faces. The result depends only on the points and their order, never on map order or the platform's scheduling, so the same input always gives the same hull.
//
// It returns ErrDegenerateHull when the points do not span a volume.
func NewConvexHull(points []Vec3, maxVertices int) (ConvexHull, error) {
	pts := make([]dv, len(points))
	for i, p := range points {
		if !isFiniteVec(p) {
			return ConvexHull{}, fmt.Errorf("%w: point %d is not finite", ErrDegenerateHull, i)
		}
		pts[i] = toDV(p)
	}
	if maxVertices <= 0 {
		maxVertices = DefaultHullVertices
	}
	maxVertices = max(min(maxVertices, MaxHullVertices), 4)
	g, err := buildHull(pts, maxVertices)
	if err != nil {
		return ConvexHull{}, err
	}
	return ConvexHull{geom: g}, nil
}

// ConvexHullFromMesh returns the convex hull of a triangle mesh's vertices, such as a glTF primitive's Positions and Indices or a render3d.Mesh's. When indices is not empty only the vertices it references are used; otherwise every position is. maxVertices is as for NewConvexHull.
func ConvexHullFromMesh(positions []math3d.Vec3, indices []uint32, maxVertices int) (ConvexHull, error) {
	var pts []Vec3
	if len(indices) == 0 {
		pts = make([]Vec3, len(positions))
		for i, p := range positions {
			pts[i] = Vec3{X: p.X, Y: p.Y, Z: p.Z}
		}
	} else {
		used := make([]bool, len(positions))
		for i, ix := range indices {
			if int(ix) >= len(positions) {
				return ConvexHull{}, fmt.Errorf("physics3d: index %d (%d) is out of range for %d positions", i, ix, len(positions))
			}
			used[ix] = true
		}
		for i, u := range used {
			if u {
				p := positions[i]
				pts = append(pts, Vec3{X: p.X, Y: p.Y, Z: p.Z})
			}
		}
	}
	return NewConvexHull(pts, maxVertices)
}

// IsEmpty reports whether h has no geometry, as the zero ConvexHull has.
func (h ConvexHull) IsEmpty() bool { return h.geom == nil }

// NumVertices returns how many vertices h has.
func (h ConvexHull) NumVertices() int {
	if h.geom == nil {
		return 0
	}
	return len(h.geom.v)
}

// Vertices returns a copy of h's vertices relative to Center.
func (h ConvexHull) Vertices() []Vec3 {
	if h.geom == nil {
		return nil
	}
	out := make([]Vec3, len(h.geom.v))
	for i, v := range h.geom.v {
		out[i] = v.vec3()
	}
	return out
}

// Faces returns a copy of h's faces, each a list of indices into Vertices in counter-clockwise order seen from outside the hull.
func (h ConvexHull) Faces() [][]int {
	if h.geom == nil {
		return nil
	}
	out := make([][]int, len(h.geom.faces))
	for i, f := range h.geom.faces {
		out[i] = slices.Clone(f.idx)
	}
	return out
}

// FaceNormals returns the outward unit normal of each face, in the order of Faces.
func (h ConvexHull) FaceNormals() []Vec3 {
	if h.geom == nil {
		return nil
	}
	out := make([]Vec3, len(h.geom.faces))
	for i, f := range h.geom.faces {
		out[i] = f.n.vec3()
	}
	return out
}

// Edges returns a copy of h's edges, each a pair of indices into Vertices with the smaller first, in a fixed order.
func (h ConvexHull) Edges() [][2]int {
	if h.geom == nil {
		return nil
	}
	return slices.Clone(h.geom.edges)
}

// Volume returns h's volume.
func (h ConvexHull) Volume() float32 {
	if h.geom == nil {
		return 0
	}
	return float32(h.geom.volume)
}

// Centroid returns h's center of mass for uniform density, Center plus the centroid of its vertices' volume.
func (h ConvexHull) Centroid() Vec3 {
	if h.geom == nil {
		return h.Center
	}
	return h.Center.Add(h.geom.centroid.vec3())
}

// InertiaTensor returns the inertia tensor of h with the given mass and uniform density, about its centroid, in its local axes, as rows.
func (h ConvexHull) InertiaTensor(mass float32) [3]Vec3 {
	if h.geom == nil {
		return [3]Vec3{}
	}
	m := float64(mass)
	var out [3]Vec3
	for i := range 3 {
		out[i] = dv{h.geom.inertia[i][0] * m, h.geom.inertia[i][1] * m, h.geom.inertia[i][2] * m}.vec3()
	}
	return out
}

// Translated returns a copy of h shifted by offset.
func (h ConvexHull) Translated(offset Vec3) ConvexHull {
	h.Center = h.Center.Add(offset)
	return h
}

// Recentered returns h with its vertices moved so that its centroid is at the local origin and Center moved by the same amount, so it covers the same space. A body built from a recentered hull at the hull's position rotates about its center of mass.
func (h ConvexHull) Recentered() ConvexHull {
	if h.geom == nil {
		return h
	}
	c := h.geom.centroid
	g := *h.geom
	g.v = make([]dv, len(h.geom.v))
	g.radius = 0
	for i, v := range h.geom.v {
		g.v[i] = v.sub(c)
		g.radius = max(g.radius, g.v[i].length())
	}
	g.faces = make([]hullFace, len(h.geom.faces))
	for i, f := range h.geom.faces {
		g.faces[i] = hullFace{idx: f.idx, n: f.n, d: f.d - f.n.dot(c)}
	}
	g.centroid = dv{}
	return ConvexHull{Center: h.Center.Add(c.vec3()), geom: &g}
}

// worldVertices returns h's vertices turned by rot about Center.
func (h ConvexHull) worldVertices(rot Quat) []Vec3 {
	if h.geom == nil {
		return nil
	}
	c := cvx{center: toDV(h.Center), axes: rotAxes(rot)}
	out := make([]Vec3, len(h.geom.v))
	for i, v := range h.geom.v {
		out[i] = c.world(v).vec3()
	}
	return out
}

// ConvexHullEdges returns the world-space wireframe segments of h turned by rot about its Center, one per hull edge in the order of Edges.
func ConvexHullEdges(h ConvexHull, rot Quat) [][2]Vec3 {
	if h.geom == nil {
		return nil
	}
	v := h.worldVertices(rot)
	out := make([][2]Vec3, len(h.geom.edges))
	for i, e := range h.geom.edges {
		out[i] = [2]Vec3{v[e[0]], v[e[1]]}
	}
	return out
}

// supportIndex returns the index of the vertex furthest along d, the lowest on ties.
func (g *hullGeom) supportIndex(d dv) int {
	best, bd := 0, g.v[0].dot(d)
	for i := 1; i < len(g.v); i++ {
		if v := g.v[i].dot(d); v > bd {
			best, bd = i, v
		}
	}
	return best
}

// supportFace returns the index of the face whose normal is closest to d, the lowest on ties.
func (g *hullGeom) supportFace(d dv) int {
	best, bd := 0, g.faces[0].n.dot(d)
	for i := 1; i < len(g.faces); i++ {
		if v := g.faces[i].n.dot(d); v > bd {
			best, bd = i, v
		}
	}
	return best
}

// bounds returns the world AABB of the hull at center turned by rot.
func (g *hullGeom) bounds(center Vec3, rot Quat) Box {
	c := cvx{center: toDV(center), axes: rotAxes(rot)}
	lo := dv{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := lo.neg()
	for _, v := range g.v {
		p := c.world(v)
		lo = dv{min(lo.x, p.x), min(lo.y, p.y), min(lo.z, p.z)}
		hi = dv{max(hi.x, p.x), max(hi.y, p.y), max(hi.z, p.z)}
	}
	return BoxFromMinMax(lo.vec3(), hi.vec3())
}

// buildHull runs quickhull over pts, stopping once the hull has maxV vertices.
func buildHull(pts []dv, maxV int) (*hullGeom, error) {
	if len(pts) < 4 {
		return nil, fmt.Errorf("%w: %d points", ErrDegenerateHull, len(pts))
	}
	lo, hi := pts[0], pts[0]
	maxAbs := 0.0
	for _, p := range pts {
		lo = dv{min(lo.x, p.x), min(lo.y, p.y), min(lo.z, p.z)}
		hi = dv{max(hi.x, p.x), max(hi.y, p.y), max(hi.z, p.z)}
		maxAbs = max(maxAbs, math.Abs(p.x), math.Abs(p.y), math.Abs(p.z))
	}
	ext := hi.sub(lo)
	scale := max(ext.x, ext.y, ext.z)
	if scale <= 0 {
		return nil, fmt.Errorf("%w: all points coincide", ErrDegenerateHull)
	}
	// Points come from float32, so anything within a few float32 ulps of the coordinates' magnitude of a face is on it.
	tol := 1e-6 * (scale + maxAbs)

	// Initial tetrahedron: the two most distant axis extremes, the point furthest from their line, then the point furthest from that plane.
	var ext6 [6]int
	for i, p := range pts {
		if p.x < pts[ext6[0]].x {
			ext6[0] = i
		}
		if p.x > pts[ext6[1]].x {
			ext6[1] = i
		}
		if p.y < pts[ext6[2]].y {
			ext6[2] = i
		}
		if p.y > pts[ext6[3]].y {
			ext6[3] = i
		}
		if p.z < pts[ext6[4]].z {
			ext6[4] = i
		}
		if p.z > pts[ext6[5]].z {
			ext6[5] = i
		}
	}
	i0, i1, best := 0, 0, -1.0
	for a := range 6 {
		for b := a + 1; b < 6; b++ {
			if d := pts[ext6[a]].sub(pts[ext6[b]]).lenSq(); d > best {
				i0, i1, best = ext6[a], ext6[b], d
			}
		}
	}
	line := pts[i1].sub(pts[i0]).norm()
	i2, best := -1, tol
	for i, p := range pts {
		if d := line.cross(p.sub(pts[i0])).length(); d > best {
			i2, best = i, d
		}
	}
	if i2 < 0 {
		return nil, fmt.Errorf("%w: the points are collinear", ErrDegenerateHull)
	}
	pn := pts[i1].sub(pts[i0]).cross(pts[i2].sub(pts[i0])).norm()
	i3, best := -1, tol
	for i, p := range pts {
		if d := math.Abs(pn.dot(p.sub(pts[i0]))); d > best {
			i3, best = i, d
		}
	}
	if i3 < 0 {
		return nil, fmt.Errorf("%w: the points are coplanar", ErrDegenerateHull)
	}

	q := &quickhull{pts: pts, tol: tol, edge: make(map[[2]int]int)}
	if pn.dot(pts[i3].sub(pts[i0])) > 0 {
		i1, i2 = i2, i1
	}
	q.addFace(i0, i1, i2)
	q.addFace(i0, i3, i1)
	q.addFace(i1, i3, i2)
	q.addFace(i2, i3, i0)
	nverts := 4
	isVert := map[int]bool{i0: true, i1: true, i2: true, i3: true}
	var free []int
	for i := range pts {
		if !isVert[i] {
			free = append(free, i)
		}
	}
	q.assign(free, 0)

	// A point within the tolerance of every plane can still lie past a knife-thin edge of a flat hull, and such a point was dropped as inside. Once no outside point is left, every point is checked against the finished faces again, and any found outside goes back in, until none is.
	for range 8 {
		for nverts < maxV {
			fi, eye := q.furthest()
			if eye < 0 {
				break
			}
			q.addPoint(fi, eye)
			isVert[eye] = true
			nverts++
		}
		if nverts >= maxV {
			break
		}
		var missed []int
		for i := range pts {
			if !isVert[i] {
				missed = append(missed, i)
			}
		}
		before := q.outsideCount()
		q.assign(missed, 0)
		if q.outsideCount() == before {
			break
		}
	}
	return q.finish()
}

// quickhull is the working state of buildHull: triangles with their outside sets and a map from directed edge to the face that owns it (looked up, never iterated).
type quickhull struct {
	pts   []dv
	tol   float64
	faces []qhFace
	edge  map[[2]int]int
}

type qhFace struct {
	v       [3]int
	n       dv
	d       float64
	outside []int
	alive   bool
}

func (q *quickhull) addFace(a, b, c int) int {
	n := q.pts[b].sub(q.pts[a]).cross(q.pts[c].sub(q.pts[a])).norm()
	fi := len(q.faces)
	q.faces = append(q.faces, qhFace{v: [3]int{a, b, c}, n: n, d: n.dot(q.pts[a]), alive: true})
	q.edge[[2]int{a, b}] = fi
	q.edge[[2]int{b, c}] = fi
	q.edge[[2]int{c, a}] = fi
	return fi
}

func (q *quickhull) dist(fi, p int) float64 {
	f := &q.faces[fi]
	return f.n.dot(q.pts[p]) - f.d
}

// assign gives each point to the first live face it lies outside of, trying the faces from index from on first (the ones just made) and then the older ones; points outside none are inside the hull and dropped. An orphan of a removed face can lie outside only a face that stayed, so the older faces are tried too.
func (q *quickhull) assign(points []int, from int) {
	for _, p := range points {
		for k := range q.faces {
			fi := (from + k) % len(q.faces)
			if q.faces[fi].alive && q.dist(fi, p) > q.tol {
				q.faces[fi].outside = append(q.faces[fi].outside, p)
				break
			}
		}
	}
}

// outsideCount returns how many points are waiting outside a live face.
func (q *quickhull) outsideCount() int {
	n := 0
	for fi := range q.faces {
		if q.faces[fi].alive {
			n += len(q.faces[fi].outside)
		}
	}
	return n
}

// furthest returns the face and point with the largest distance outside the hull, the lowest point index on ties, or -1 when every point is inside.
func (q *quickhull) furthest() (int, int) {
	bf, bp, bd := -1, -1, 0.0
	for fi := range q.faces {
		f := &q.faces[fi]
		if !f.alive {
			continue
		}
		for _, p := range f.outside {
			d := q.dist(fi, p)
			if d > bd || (d == bd && bp >= 0 && p < bp) {
				bf, bp, bd = fi, p, d
			}
		}
	}
	return bf, bp
}

// addPoint adds eye, which lies outside face fi, to the hull: it removes the faces eye sees or lies on, connected to fi, and fans new faces from the horizon to eye.
func (q *quickhull) addPoint(fi, eye int) {
	visible := []int{fi}
	seen := map[int]bool{fi: true}
	var horizon [][2]int
	for i := 0; i < len(visible); i++ {
		f := q.faces[visible[i]]
		for e := range 3 {
			a, b := f.v[e], f.v[(e+1)%3]
			nb := q.edge[[2]int{b, a}]
			if seen[nb] {
				continue
			}
			// A neighbour goes when eye is outside it at all, not only by the tolerance: kept, a face eye is slightly outside of would fold the hull inward at the horizon, which a thin new face can magnify into a large dent. A face whose plane eye lies on, to rounding, goes too, or eye in line with one of its edges would make a face of zero area.
			if q.dist(nb, eye) > -q.tol*1e-6 {
				seen[nb] = true
				visible = append(visible, nb)
				continue
			}
			horizon = append(horizon, [2]int{a, b})
		}
	}
	var orphans []int
	for _, vi := range visible {
		f := &q.faces[vi]
		f.alive = false
		for _, p := range f.outside {
			if p != eye {
				orphans = append(orphans, p)
			}
		}
		f.outside = nil
		for e := range 3 {
			k := [2]int{f.v[e], f.v[(e+1)%3]}
			if q.edge[k] == vi {
				delete(q.edge, k)
			}
		}
	}
	first := len(q.faces)
	// Fan in horizon order so the new faces are created deterministically.
	for _, e := range horizon {
		q.addFace(e[0], e[1], eye)
	}
	q.assign(orphans, first)
}

// finish merges coplanar triangles into polygon faces, renumbers the vertices in input order, and computes the mass properties.
func (q *quickhull) finish() (*hullGeom, error) {
	var tris []int
	for fi := range q.faces {
		if q.faces[fi].alive {
			tris = append(tris, fi)
		}
	}
	group := make(map[int]int, len(tris))
	type poly struct {
		loop []int
		n    dv
	}
	var polys []poly
	for _, seed := range tris {
		if _, ok := group[seed]; ok {
			continue
		}
		gid := len(polys)
		sf := q.faces[seed]
		members := []int{seed}
		group[seed] = gid
		for i := 0; i < len(members); i++ {
			f := q.faces[members[i]]
			for e := range 3 {
				a, b := f.v[e], f.v[(e+1)%3]
				nb, ok := q.edge[[2]int{b, a}]
				if !ok {
					continue
				}
				if _, done := group[nb]; done {
					continue
				}
				o := q.faces[nb]
				coplanar := o.n.dot(sf.n) > 1-1e-6
				for _, v := range o.v {
					coplanar = coplanar && math.Abs(sf.n.dot(q.pts[v])-sf.d) <= q.tol
				}
				if coplanar {
					group[nb] = gid
					members = append(members, nb)
				}
			}
		}
		// The polygon's boundary is every member edge whose twin belongs to another polygon.
		next := make(map[int]int)
		start := -1
		for _, m := range members {
			f := q.faces[m]
			for e := range 3 {
				a, b := f.v[e], f.v[(e+1)%3]
				if g, ok := group[q.edge[[2]int{b, a}]]; ok && g == gid {
					continue
				}
				next[a] = b
				if start < 0 {
					start = a
				}
			}
		}
		loop := []int{start}
		for v := next[start]; v != start && len(loop) <= len(next); v = next[v] {
			loop = append(loop, v)
		}
		if len(loop) != len(next) {
			// Not a simple polygon: keep the triangles apart.
			for _, m := range members[1:] {
				delete(group, m)
			}
			loop = sf.v[:]
		}
		polys = append(polys, poly{loop: slices.Clone(loop), n: sf.n})
	}

	// Renumber the vertices in the order of their input index.
	var used []int
	seen := make(map[int]bool)
	for _, p := range polys {
		for _, v := range p.loop {
			if !seen[v] {
				seen[v] = true
				used = append(used, v)
			}
		}
	}
	slices.Sort(used)
	remap := make(map[int]int, len(used))
	g := &hullGeom{v: make([]dv, len(used))}
	for i, v := range used {
		remap[v] = i
		g.v[i] = q.pts[v]
		g.radius = max(g.radius, q.pts[v].length())
	}
	edgeSeen := make(map[[2]int]bool)
	for _, p := range polys {
		idx := make([]int, len(p.loop))
		for i, v := range p.loop {
			idx[i] = remap[v]
		}
		// Newell's normal of the merged polygon and its mean plane offset.
		var n dv
		for i := range idx {
			a, b := g.v[idx[i]], g.v[idx[(i+1)%len(idx)]]
			n = n.add(dv{float64((a.y - b.y) * (a.z + b.z)), float64((a.z - b.z) * (a.x + b.x)), float64((a.x - b.x) * (a.y + b.y))})
		}
		n = n.norm()
		if n.lenSq() == 0 {
			n = p.n
		}
		d := n.dot(g.v[idx[0]])
		for _, i := range idx[1:] {
			d = max(d, n.dot(g.v[i]))
		}
		g.faces = append(g.faces, hullFace{idx: idx, n: n, d: d})
		for i := range idx {
			a, b := idx[i], idx[(i+1)%len(idx)]
			if a > b {
				a, b = b, a
			}
			if !edgeSeen[[2]int{a, b}] {
				edgeSeen[[2]int{a, b}] = true
				g.edges = append(g.edges, [2]int{a, b})
			}
		}
	}
	slices.SortFunc(g.edges, func(x, y [2]int) int {
		if x[0] != y[0] {
			return x[0] - y[0]
		}
		return x[1] - y[1]
	})
	if err := g.massProperties(); err != nil {
		return nil, err
	}
	g.halfWidth = math.Inf(1)
	for _, f := range g.faces {
		lo := f.d
		for _, v := range g.v {
			lo = min(lo, f.n.dot(v))
		}
		g.halfWidth = min(g.halfWidth, (f.d-lo)/2)
	}
	return g, nil
}

// massProperties computes the volume, centroid and unit-mass inertia tensor of g from the tetrahedra its faces make with a point inside it.
func (g *hullGeom) massProperties() error {
	var ref dv
	for _, v := range g.v {
		ref = ref.add(v)
	}
	ref = ref.scale(1 / float64(len(g.v)))
	var vol float64
	var mom dv
	var cov [3][3]float64
	for _, f := range g.faces {
		a := g.v[f.idx[0]].sub(ref)
		for i := 1; i+1 < len(f.idx); i++ {
			b, c := g.v[f.idx[i]].sub(ref), g.v[f.idx[i+1]].sub(ref)
			det := a.dot(b.cross(c))
			vol += det / 6
			s := a.add(b).add(c)
			mom = mom.add(s.scale(det / 24))
			// Covariance of a tetrahedron with a corner at the origin: det/120 * (a a' + b b' + c c' + s s').
			for _, p := range [4]dv{a, b, c, s} {
				pa := [3]float64{p.x, p.y, p.z}
				for r := range 3 {
					for k := range 3 {
						cov[r][k] += float64(det / 120 * pa[r] * pa[k])
					}
				}
			}
		}
	}
	if !(vol > 0) {
		return fmt.Errorf("%w: zero volume", ErrDegenerateHull)
	}
	cen := mom.scale(1 / vol)
	cc := [3]float64{cen.x, cen.y, cen.z}
	for r := range 3 {
		for k := range 3 {
			cov[r][k] -= float64(vol * cc[r] * cc[k])
		}
	}
	tr := cov[0][0] + cov[1][1] + cov[2][2]
	for r := range 3 {
		for k := range 3 {
			v := -cov[r][k]
			if r == k {
				v += tr
			}
			g.inertia[r][k] = v / vol
		}
	}
	g.volume = vol
	g.centroid = cen.add(ref)
	return nil
}

// raycastHull intersects a ray, in the hull's local frame with a unit direction, with the hull's face planes.
func (g *hullGeom) raycast(o, d dv, maxDist float64) (float64, dv, bool, bool) {
	tEnter, tExit := 0.0, maxDist
	var n dv
	entered := false
	for _, f := range g.faces {
		den := f.n.dot(d)
		dist := f.d - f.n.dot(o)
		if math.Abs(den) < 1e-15 {
			if dist < 0 {
				return 0, dv{}, false, false
			}
			continue
		}
		t := dist / den
		if den < 0 {
			if t > tEnter {
				tEnter, n, entered = t, f.n, true
			}
		} else if t < tExit {
			tExit = t
		}
		if tEnter > tExit {
			return 0, dv{}, false, false
		}
	}
	return tEnter, n, entered, true
}
