package world3d

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math3d"
)

// NavArea identifies the surface or terrain category of a 3D navigation polygon.
// It aliases ai.NavArea so the same area IDs and NavFilters work seamlessly across 2D and 3D meshes.
type NavArea = ai.NavArea

// NavFlags is a bitmask of runtime traversal capabilities and states on a 3D navigation polygon.
// It aliases ai.NavFlags so the same flags and NavFilters work seamlessly across 2D and 3D meshes.
type NavFlags = ai.NavFlags

// NavFilter controls which 3D navigation polygons a query may traverse and how
// each polygon's area scales traversal cost.
type NavFilter = ai.NavFilter

const (
	NavAreaDefault  = ai.NavAreaDefault
	NavAreaRoad     = ai.NavAreaRoad
	NavAreaGrass    = ai.NavAreaGrass
	NavAreaMud      = ai.NavAreaMud
	NavAreaWater    = ai.NavAreaWater
	NavAreaDoor     = ai.NavAreaDoor
	NavAreaHazard   = ai.NavAreaHazard
	NavAreaJump     = ai.NavAreaJump
	NavAreaClimb    = ai.NavAreaClimb
	NavAreaTeleport = ai.NavAreaTeleport
)

const (
	NavFlagWalk     = ai.NavFlagWalk
	NavFlagSwim     = ai.NavFlagSwim
	NavFlagDoor     = ai.NavFlagDoor
	NavFlagJump     = ai.NavFlagJump
	NavFlagClimb    = ai.NavFlagClimb
	NavFlagTeleport = ai.NavFlagTeleport
	NavFlagDisabled = ai.NavFlagDisabled
	NavFlagAll      = ai.NavFlagAll
)

// DefaultNavFilter returns the default query filter matching BuildNavMesh defaults:
// every non-zero, non-disabled polygon is traversable at unit cost (1.0).
func DefaultNavFilter() NavFilter {
	return ai.DefaultNavFilter()
}

// NavPolygonInput describes a single convex 3D polygon together with its
// initial area ID and runtime traversal flags for BuildNavMeshWithAreas.
// A zero Flags value defaults to NavFlagWalk.
type NavPolygonInput struct {
	Vertices []math3d.Vec3 `yaml:"vertices"`
	Area     NavArea       `yaml:"area,omitempty"`
	Flags    NavFlags      `yaml:"flags,omitempty"`
}

const (
	defaultVerticalTolerance = 2.0
	sharedEdgeEps            = 1e-3
	minPortalWidth           = 1e-4
)

// NavLinkOffMesh is a link flag marking a portal as an off-mesh connection
// (such as a jump, drop-down, ladder, or teleport) rather than a shared 3D edge.
const NavLinkOffMesh uint32 = 1 << 0

// NavOffMeshLink describes a custom 3D off-mesh connection between two walkable points on a NavMesh
// (such as a jump, drop-down, ladder, door, or teleporter).
type NavOffMeshLink struct {
	// Start and End are the 3D endpoints on (or within Radius of) the mesh.
	Start math3d.Vec3
	End   math3d.Vec3
	// Radius is the optional endpoint snap and agent entry tolerance in world units.
	// When positive, an endpoint outside a polygon snaps to the nearest polygon point within Radius,
	// and NavAgent uses max(entryRadius, Radius) when entering the link.
	// When zero, endpoints must lie on a walkable polygon within VerticalTolerance.
	Radius float32
	// Bidirectional adds links in both Start->End and End->Start directions when true.
	Bidirectional bool
	// Flags stores game-defined link flags; NavLinkOffMesh is always set automatically.
	Flags uint32
	// ID is an optional game-defined link identifier delivered in NavLinkEvent.LinkID and NavPathLink.LinkID.
	ID int
	// Cost overrides the A* traversal cost when positive; <= 0 uses 3D Euclidean distance.
	Cost float32
	// Area assigns a NavArea to the link so NavFilter can exclude it or scale its traversal cost.
	// Defaults to NavAreaDefault (0).
	Area NavArea
	// UserData holds optional game-defined metadata (such as "jump", "ladder", "teleport", or a custom value)
	// carried through to NavPathLink and NavLinkEvent.
	UserData any
}

// NavPathLink marks a segment in NavPathResult.Path that traverses an off-mesh link.
// Path[StartIndex] is the link's 3D entry point on the mesh and Path[EndIndex] is its 3D exit point
// (with EndIndex == StartIndex + 1).
type NavPathLink struct {
	// StartIndex is the index in NavPathResult.Path where the off-mesh link begins.
	StartIndex int
	// EndIndex is the index in NavPathResult.Path where the off-mesh link ends (StartIndex + 1).
	EndIndex int
	// FromPoly and ToPoly are the source and destination polygon indices connected by the link.
	FromPoly int
	ToPoly   int
	// Start and End are the 3D world-space entry and exit positions of the traversed link direction.
	Start math3d.Vec3
	End   math3d.Vec3
	// Radius is the link's configured connection/entry radius.
	Radius float32
	// Area is the link's navigation area.
	Area NavArea
	// Flags are the link's flags (including NavLinkOffMesh).
	Flags uint32
	// LinkID is the link's configured identifier.
	LinkID int
	// UserData is the link's configured custom metadata.
	UserData any
}

// NavLink describes a traversable 3D portal edge (or off-mesh link) from one
// NavPoly to another NavPoly. For shared-edge portals, Left and Right are
// ordered as seen when looking from the source polygon outward across the
// shared edge into Neighbor. For off-mesh links (OffMesh == true), Left and
// Right hold the entry point and End holds the exit point.
type NavLink struct {
	Neighbor   int
	Left       math3d.Vec3
	Right      math3d.Vec3
	Flags      uint32
	offMeshIdx int32 // 1-based index into nm.offMeshLinks (0 for shared-edge portals)
	LinkID     int
	OffMesh    bool
	End        math3d.Vec3
	Cost       float32
}

type navOffMeshEntry struct {
	link        NavOffMeshLink
	filterFlags NavFlags
}

func offMeshFilterFlags(raw uint32) NavFlags {
	if raw == 0 {
		return NavFlagWalk
	}
	return NavFlags(raw)
}

// NavPoly is a single walkable convex 3D polygon in a NavMesh.
// Vertices are normalized to counter-clockwise winding when viewed from +Y so
// Normal always points upward (Normal.Y > 0).
type NavPoly struct {
	Vertices []math3d.Vec3
	Normal   math3d.Vec3
	PlaneD   float32
	Center   math3d.Vec3
	Bounds   math3d.AABB
	Links    []NavLink
	Area     NavArea
	Flags    NavFlags
}

// HeightAt returns the Y coordinate of the polygon's supporting 3D plane at (x, z).
func (p *NavPoly) HeightAt(x, z float32) float32 {
	if p == nil || math.Abs(float64(p.Normal.Y)) < 1e-7 {
		return 0
	}
	return -(float32(p.Normal.X*x) + float32(p.Normal.Z*z) + p.PlaneD) / p.Normal.Y
}

// ContainsXZ reports whether (x, z) lies inside or on the boundary of p's XZ projection.
func (p *NavPoly) ContainsXZ(x, z float32) bool {
	if p == nil || len(p.Vertices) < 3 {
		return false
	}
	const pad = 1e-4
	if x < p.Bounds.Min.X-pad || x > p.Bounds.Max.X+pad || z < p.Bounds.Min.Z-pad || z > p.Bounds.Max.Z+pad {
		return false
	}
	pt := math3d.Vec3{X: x, Z: z}
	n := len(p.Vertices)
	for i := range n {
		a := p.Vertices[i]
		b := p.Vertices[(i+1)%n]
		dx := b.X - a.X
		dz := b.Z - a.Z
		edgeLen := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		// For CCW polygons viewed from +Y, interior points satisfy triArea2(a, b, pt) <= 0.
		if triArea2(a, b, pt) > 1e-4*edgeLen+1e-6 {
			return false
		}
	}
	return true
}

// NavMesh is a 3D navigation mesh built from convex polygons. It supports sloped
// ramps, obstacles, and vertically stacked walkable levels (bridges, overpasses,
// multi-story floors) linked strictly along shared 3D edges.
type NavMesh struct {
	Polys             []NavPoly
	VerticalTolerance float32
	bvhNodes          []navBVHNode3D
	bvhOrder          []int32
	offMeshLinks      []navOffMeshEntry
	offMeshCount      int
	revision          uint64
	defaultQuery      *NavMeshQuery
}

// BuildNavMesh constructs a NavMesh from convex 3D polygons, filtering out
// degenerate polygons and slopes steeper than maxSlopeRad radians from
// horizontal (upward normal angle from +Y). Adjacent walkable polygons are
// linked automatically along shared 3D edges (including collinear T-junction
// overlaps) so vertically separated levels never falsely connect.
//
// Each polygon is assigned NavAreaDefault and NavFlagWalk. If maxSlopeRad < 0,
// a default limit of 45 degrees (pi/4) is used.
func BuildNavMesh(polys [][]math3d.Vec3, maxSlopeRad float32) *NavMesh {
	return buildNavMesh(len(polys), maxSlopeRad, func(i int) ([]math3d.Vec3, NavArea, NavFlags) {
		return polys[i], NavAreaDefault, NavFlagWalk
	})
}

// BuildNavMeshWithAreas constructs a NavMesh from convex 3D polygons carrying
// per-polygon area IDs and runtime traversal flags, filtering out degenerate
// polygons and slopes steeper than maxSlopeRad radians from horizontal.
//
// Polygons with zero Flags default to NavFlagWalk. If maxSlopeRad < 0, a
// default limit of 45 degrees (pi/4) is used.
func BuildNavMeshWithAreas(polys []NavPolygonInput, maxSlopeRad float32) *NavMesh {
	return buildNavMesh(len(polys), maxSlopeRad, func(i int) ([]math3d.Vec3, NavArea, NavFlags) {
		return polys[i].Vertices, polys[i].Area, polys[i].Flags
	})
}

func buildNavMesh(count int, maxSlopeRad float32, get func(i int) ([]math3d.Vec3, NavArea, NavFlags)) *NavMesh {
	if maxSlopeRad < 0 {
		maxSlopeRad = math.Pi / 4
	}
	minNormalY := float32(math.Cos(float64(maxSlopeRad))) - 1e-5

	totalVerts := 0
	for i := range count {
		raw, _, _ := get(i)
		if len(raw) >= 3 {
			totalVerts += len(raw)
		}
	}

	nm := &NavMesh{
		Polys:             make([]NavPoly, 0, count),
		VerticalTolerance: defaultVerticalTolerance,
		revision:          1,
	}
	vertBuf := make([]math3d.Vec3, 0, totalVerts)

	for i := range count {
		raw, area, flags := get(i)
		if len(raw) < 3 {
			continue
		}
		start := len(vertBuf)
		vertBuf = append(vertBuf, raw...)
		verts := vertBuf[start : start+len(raw) : start+len(raw)]

		normal := newellNormal(verts)
		if normal.LenSq() < 1e-10 {
			vertBuf = vertBuf[:start]
			continue
		}
		normal = normal.Norm()

		// Normalize polygon winding to CCW when viewed from +Y.
		if normal.Y < 0 {
			reverseVec3(verts)
			normal = normal.Neg()
		}

		// Filter unwalkable steep surfaces (walls, cliffs, overhangs).
		if normal.Y < minNormalY || normal.Y < 1e-5 {
			vertBuf = vertBuf[:start]
			continue
		}

		bounds := math3d.AABBEmpty()
		var sum math3d.Vec3
		for _, v := range verts {
			bounds = bounds.ExpandPoint(v)
			sum = sum.Add(v)
		}

		if flags == 0 {
			flags = NavFlagWalk
		}

		nm.Polys = append(nm.Polys, NavPoly{
			Vertices: verts,
			Normal:   normal,
			PlaneD:   -normal.Dot(verts[0]),
			Center:   sum.Scale(1 / float32(len(verts))),
			Bounds:   bounds,
			Area:     area,
			Flags:    flags,
		})
	}

	nm.buildBVH()
	nm.linkPolygons()
	return nm
}

// PolygonCount returns the number of walkable convex polygons in the mesh.
func (nm *NavMesh) PolygonCount() int {
	if nm == nil {
		return 0
	}
	return len(nm.Polys)
}

// PolygonArea returns the area ID of polygon index, or NavAreaDefault if index is out of range.
func (nm *NavMesh) PolygonArea(index int) NavArea {
	if nm == nil || index < 0 || index >= len(nm.Polys) {
		return NavAreaDefault
	}
	return nm.Polys[index].Area
}

// SetPolygonArea updates the area ID of polygon index at runtime without rebuilding the mesh.
// Out-of-range indices are ignored.
func (nm *NavMesh) SetPolygonArea(index int, area NavArea) {
	if nm == nil || index < 0 || index >= len(nm.Polys) {
		return
	}
	if nm.Polys[index].Area != area {
		nm.Polys[index].Area = area
		nm.revision++
	}
}

// PolygonFlags returns the runtime traversal flags of polygon index, or 0 if index is out of range.
func (nm *NavMesh) PolygonFlags(index int) NavFlags {
	if nm == nil || index < 0 || index >= len(nm.Polys) {
		return 0
	}
	return nm.Polys[index].Flags
}

// SetPolygonFlags replaces the runtime traversal flags of polygon index without rebuilding the mesh.
// Out-of-range indices are ignored.
func (nm *NavMesh) SetPolygonFlags(index int, flags NavFlags) {
	if nm == nil || index < 0 || index >= len(nm.Polys) {
		return
	}
	if nm.Polys[index].Flags != flags {
		nm.Polys[index].Flags = flags
		nm.revision++
	}
}

// AddPolygonFlags sets the bits in flags on polygon index at runtime without rebuilding the mesh.
// Out-of-range indices are ignored.
func (nm *NavMesh) AddPolygonFlags(index int, flags NavFlags) {
	if nm == nil || index < 0 || index >= len(nm.Polys) {
		return
	}
	next := nm.Polys[index].Flags | flags
	if next != nm.Polys[index].Flags {
		nm.Polys[index].Flags = next
		nm.revision++
	}
}

// ClearPolygonFlags clears the bits in flags on polygon index at runtime without rebuilding the mesh.
// Out-of-range indices are ignored.
func (nm *NavMesh) ClearPolygonFlags(index int, flags NavFlags) {
	if nm == nil || index < 0 || index >= len(nm.Polys) {
		return
	}
	next := nm.Polys[index].Flags &^ flags
	if next != nm.Polys[index].Flags {
		nm.Polys[index].Flags = next
		nm.revision++
	}
}

func (nm *NavMesh) linkPolygons() {
	n := len(nm.Polys)
	pad := math3d.Vec3{X: sharedEdgeEps, Y: sharedEdgeEps, Z: sharedEdgeEps}
	var candBuf [32]int
	candidates := candBuf[:0]

	for i := range n {
		pi := &nm.Polys[i]
		bi := math3d.AABB{Min: pi.Bounds.Min.Sub(pad), Max: pi.Bounds.Max.Add(pad)}
		candidates = nm.bvhQueryAABB(bi, candidates[:0])
		slices.Sort(candidates)
		for _, j := range candidates {
			if j <= i {
				continue
			}
			pj := &nm.Polys[j]
			if !bi.Intersects(pj.Bounds) {
				continue
			}
			left, right, ok := findSharedPortal(pi.Vertices, pj.Vertices)
			if !ok {
				continue
			}
			pi.Links = append(pi.Links, NavLink{
				Neighbor: j,
				Left:     left,
				Right:    right,
			})
			pj.Links = append(pj.Links, NavLink{
				Neighbor: i,
				Left:     right,
				Right:    left,
			})
		}
	}
}

// findSharedPortal checks whether two CCW convex polygons share a 3D edge (or
// collinear overlapping sub-edge) with opposing edge directions. On success,
// left and right are oriented for crossing from vertsA into vertsB.
func findSharedPortal(vertsA, vertsB []math3d.Vec3) (left, right math3d.Vec3, ok bool) {
	na := len(vertsA)
	nb := len(vertsB)
	for ia := range na {
		a0 := vertsA[ia]
		a1 := vertsA[(ia+1)%na]
		dA := a1.Sub(a0)
		lenA := dA.Len()
		if lenA < minPortalWidth {
			continue
		}
		u := dA.Scale(1 / lenA)

		for ib := range nb {
			b0 := vertsB[ib]
			b1 := vertsB[(ib+1)%nb]
			dB := b1.Sub(b0)
			if dB.LenSq() < minPortalWidth*minPortalWidth {
				continue
			}
			// Adjacent CCW polygons always traverse their shared edge in opposite directions.
			if dA.Dot(dB) >= 0 {
				continue
			}

			// Verify both endpoints of B lie on the 3D line of A within sharedEdgeEps.
			v0 := b0.Sub(a0)
			t0 := v0.Dot(u)
			if v0.Sub(u.Scale(t0)).Len() > sharedEdgeEps {
				continue
			}
			v1 := b1.Sub(a0)
			t1 := v1.Dot(u)
			if v1.Sub(u.Scale(t1)).Len() > sharedEdgeEps {
				continue
			}

			lo := max(float32(0), min(t0, t1))
			hi := min(lenA, max(t0, t1))
			if hi-lo <= minPortalWidth {
				continue
			}

			// Looking outward across CCW edge a0 -> a1 from inside A:
			// a0 (smaller parameter) is on the right, a1 (larger parameter) is on the left.
			right = a0.Add(u.Scale(lo))
			left = a0.Add(u.Scale(hi))
			return left, right, true
		}
	}
	return math3d.Vec3{}, math3d.Vec3{}, false
}

// FindPolygon returns the index of the walkable polygon containing (p.X, p.Z)
// whose 3D surface height is closest to p.Y within VerticalTolerance.
//
// Stacked levels (such as a bridge above a ground floor) remain cleanly
// separated because the polygon minimizing |p.Y - poly.HeightAt(p.X, p.Z)| is
// chosen. Returns (-1, false) if no polygon matches.
func (nm *NavMesh) FindPolygon(p math3d.Vec3) (int, bool) {
	return nm.FindPolygonWithFilter(p, NavFilter{})
}

// FindPolygonWithFilter returns the index of the polygon passing filter that
// contains (p.X, p.Z) and whose 3D surface height is closest to p.Y within
// VerticalTolerance. Returns (-1, false) if no matching polygon is found.
func (nm *NavMesh) FindPolygonWithFilter(p math3d.Vec3, filter NavFilter) (int, bool) {
	if nm == nil || len(nm.Polys) == 0 || !isFiniteVec3(p) {
		return -1, false
	}
	tol := nm.VerticalTolerance
	if tol <= 0 {
		tol = defaultVerticalTolerance
	}
	return nm.bvhFindPoly(p, tol, filter)
}

// Contains reports whether p lies on any polygon in the mesh within VerticalTolerance.
func (nm *NavMesh) Contains(p math3d.Vec3) bool {
	_, ok := nm.FindPolygon(p)
	return ok
}

// ContainsWithFilter reports whether p lies on any polygon passing filter within VerticalTolerance.
func (nm *NavMesh) ContainsWithFilter(p math3d.Vec3, filter NavFilter) bool {
	_, ok := nm.FindPolygonWithFilter(p, filter)
	return ok
}

// NearestPolygon returns the polygon closest to p in 3D and the closest point on
// or inside that polygon.
func (nm *NavMesh) NearestPolygon(p math3d.Vec3) (index int, point math3d.Vec3, ok bool) {
	return nm.NearestPolygonWithFilter(p, NavFilter{})
}

// NearestPolygonWithFilter returns the polygon passing filter closest to p in 3D
// and the closest point on or inside that polygon.
func (nm *NavMesh) NearestPolygonWithFilter(p math3d.Vec3, filter NavFilter) (index int, point math3d.Vec3, ok bool) {
	if nm == nil || len(nm.Polys) == 0 || !isFiniteVec3(p) {
		return -1, math3d.Vec3{}, false
	}
	if idx, found := nm.FindPolygonWithFilter(p, filter); found {
		pt := math3d.Vec3{X: p.X, Y: nm.Polys[idx].HeightAt(p.X, p.Z), Z: p.Z}
		return idx, pt, true
	}
	return nm.bvhNearestPoly(p, math3d.Vec3{}, false, filter)
}

func (nm *NavMesh) polyClosestPoint(polyIdx int, p math3d.Vec3) math3d.Vec3 {
	return closestPointOnNavPoly(&nm.Polys[polyIdx], p)
}

func closestPointOnNavPoly(poly *NavPoly, p math3d.Vec3) math3d.Vec3 {
	signedDist := poly.Normal.Dot(p) + poly.PlaneD
	proj := p.Sub(poly.Normal.Scale(signedDist))
	if poly.ContainsXZ(proj.X, proj.Z) {
		proj.Y = poly.HeightAt(proj.X, proj.Z)
		return proj
	}
	vs := poly.Vertices
	n := len(vs)
	bestDistSq := math.Inf(1)
	var bestPt math3d.Vec3
	for k := range n {
		cp := closestPointOnSegment3D(p, vs[k], vs[(k+1)%n])
		d2 := distSq3D(p, cp)
		if d2 < bestDistSq {
			bestDistSq = d2
			bestPt = cp
		}
	}
	return bestPt
}

func closestPointOnSegment3D(p, a, b math3d.Vec3) math3d.Vec3 {
	ab := b.Sub(a)
	l2 := ab.LenSq()
	if l2 <= 1e-12 {
		return a
	}
	t := math3d.Clamp(p.Sub(a).Dot(ab)/l2, 0, 1)
	return a.Add(ab.Scale(t))
}

func distSq3D(a, b math3d.Vec3) float64 {
	dx := float64(a.X - b.X)
	dy := float64(a.Y - b.Y)
	dz := float64(a.Z - b.Z)
	return float64(dx*dx) + float64(dy*dy) + float64(dz*dz)
}

func dist3D(a, b math3d.Vec3) float64 {
	return math.Sqrt(distSq3D(a, b))
}

// NavPathResult is the outcome of a bounded or incremental 3D NavMesh search.
type NavPathResult struct {
	Path []math3d.Vec3
	// Links lists each segment of Path that traverses an off-mesh link, in path order.
	// For each link, Path[link.StartIndex] is the 3D entry point and Path[link.EndIndex]
	// (with EndIndex == StartIndex + 1) is the 3D exit point.
	Links      []NavPathLink
	Found      bool // true when a complete or partial path was produced
	Partial    bool // true when Path ends at the closest reachable 3D point rather than goal
	Exhausted  bool // true when the search stopped because MaxExpansions was reached
	Expansions int  // total number of portal nodes expanded during the search
}

// HasLinks reports whether r traverses at least one off-mesh link.
func (r NavPathResult) HasLinks() bool {
	return len(r.Links) > 0
}

// LinkAt returns the off-mesh link traversed by path segment segmentIndex (from Path[segmentIndex]
// to Path[segmentIndex+1]), or (zero, false) when that segment is ordinary walking movement.
func (r NavPathResult) LinkAt(segmentIndex int) (NavPathLink, bool) {
	for _, l := range r.Links {
		if l.StartIndex == segmentIndex {
			return l, true
		}
		if l.StartIndex > segmentIndex {
			break
		}
	}
	return NavPathLink{}, false
}

// Revision returns a monotonically increasing number bumped whenever the mesh topology, polygon areas/flags, or link flags change.
func (nm *NavMesh) Revision() uint64 {
	if nm == nil {
		return 0
	}
	return nm.revision
}

// BumpRevision increments the mesh revision so NavAgents watching this mesh invalidate cached corridors and replan.
func (nm *NavMesh) BumpRevision() {
	if nm == nil {
		return
	}
	nm.revision++
}

func (nm *NavMesh) resolveLinkEndpoint(p math3d.Vec3, radius float32) (int, math3d.Vec3, bool) {
	if !isFiniteVec3(p) {
		return -1, math3d.Vec3{}, false
	}
	if idx, ok := nm.FindPolygon(p); ok {
		snapped := p
		snapped.Y = nm.Polys[idx].HeightAt(p.X, p.Z)
		return idx, snapped, true
	}
	if radius <= 0 {
		return -1, math3d.Vec3{}, false
	}
	tol := nm.VerticalTolerance
	if tol <= 0 {
		tol = defaultVerticalTolerance
	}
	vExt := max(radius, tol)
	snapped, idx, ok := nm.NearestPointWithFilter(p, math3d.Vec3{X: radius, Y: vExt, Z: radius}, NavFilter{})
	if !ok {
		return -1, math3d.Vec3{}, false
	}
	dx := float64(p.X - snapped.X)
	dz := float64(p.Z - snapped.Z)
	if math.Hypot(dx, dz) > float64(radius)+float64(sharedEdgeEps) {
		return -1, math3d.Vec3{}, false
	}
	if math.Abs(float64(p.Y-snapped.Y)) > float64(vExt)+float64(sharedEdgeEps) {
		return -1, math3d.Vec3{}, false
	}
	return idx, snapped, true
}

// AddOffMeshLink connects the polygons containing (or within link.Radius of) link.Start and link.End
// with a directed (or bidirectional) off-mesh link and bumps the mesh revision.
// Returns false if the mesh is nil, link coordinates or Radius are invalid, either endpoint cannot
// attach to a walkable polygon, or both endpoints collapse to the same point on the same polygon.
func (nm *NavMesh) AddOffMeshLink(link NavOffMeshLink) bool {
	if nm == nil || len(nm.Polys) == 0 {
		return false
	}
	if !isFinite32(link.Radius) || link.Radius < 0 {
		return false
	}
	fromPoly, start, okFrom := nm.resolveLinkEndpoint(link.Start, link.Radius)
	toPoly, end, okTo := nm.resolveLinkEndpoint(link.End, link.Radius)
	if !okFrom || !okTo {
		return false
	}
	if fromPoly == toPoly && start.ApproxEqual(end, 1e-4) {
		return false
	}

	cost := link.Cost
	if !(cost > 0) || math.IsInf(float64(cost), 0) || math.IsNaN(float64(cost)) {
		cost = start.Dist(end)
	}
	filterFlags := offMeshFilterFlags(link.Flags)
	flags := link.Flags | NavLinkOffMesh
	entryIdx := int32(len(nm.offMeshLinks) + 1)

	nm.Polys[fromPoly].Links = append(nm.Polys[fromPoly].Links, NavLink{
		Neighbor:   toPoly,
		Left:       start,
		Right:      start,
		Flags:      flags,
		offMeshIdx: entryIdx,
		LinkID:     link.ID,
		OffMesh:    true,
		End:        end,
		Cost:       cost,
	})
	nm.offMeshCount++
	if link.Bidirectional {
		nm.Polys[toPoly].Links = append(nm.Polys[toPoly].Links, NavLink{
			Neighbor:   fromPoly,
			Left:       end,
			Right:      end,
			Flags:      flags,
			offMeshIdx: entryIdx,
			LinkID:     link.ID,
			OffMesh:    true,
			End:        start,
			Cost:       cost,
		})
		nm.offMeshCount++
	}
	stored := link
	stored.Start = start
	stored.End = end
	stored.Flags = flags
	nm.offMeshLinks = append(nm.offMeshLinks, navOffMeshEntry{
		link:        stored,
		filterFlags: filterFlags,
	})
	nm.revision++
	return true
}

func (nm *NavMesh) offMeshMeta(link NavLink) (radius float32, area NavArea, filterFlags NavFlags, userData any) {
	if link.offMeshIdx > 0 && int(link.offMeshIdx) <= len(nm.offMeshLinks) {
		e := &nm.offMeshLinks[link.offMeshIdx-1]
		return e.link.Radius, e.link.Area, e.filterFlags, e.link.UserData
	}
	return 0, NavAreaDefault, offMeshFilterFlags(link.Flags), nil
}

// OffMeshLinks returns a copy of all off-mesh links registered on nm in insertion order.
func (nm *NavMesh) OffMeshLinks() []NavOffMeshLink {
	if nm == nil || len(nm.offMeshLinks) == 0 {
		return nil
	}
	out := make([]NavOffMeshLink, len(nm.offMeshLinks))
	for i := range nm.offMeshLinks {
		out[i] = nm.offMeshLinks[i].link
	}
	return out
}

// ClearOffMeshLinks removes all off-mesh links from nm and bumps the mesh revision when any were present.
func (nm *NavMesh) ClearOffMeshLinks() {
	if nm == nil || nm.offMeshCount == 0 {
		return
	}
	for i := range nm.Polys {
		links := nm.Polys[i].Links
		kept := links[:0]
		for _, l := range links {
			if !l.OffMesh {
				kept = append(kept, l)
			}
		}
		clear(links[len(kept):])
		nm.Polys[i].Links = kept
	}
	nm.offMeshLinks = nil
	nm.offMeshCount = 0
	nm.revision++
}

// SetEdgeFlags sets custom flags on the shared-edge link from fromPoly to toPoly and bumps the mesh revision.
// Returns false if no shared-edge link connects fromPoly to toPoly.
func (nm *NavMesh) SetEdgeFlags(fromPoly, toPoly int, flags uint32) bool {
	if nm == nil || fromPoly < 0 || fromPoly >= len(nm.Polys) || toPoly < 0 || toPoly >= len(nm.Polys) {
		return false
	}
	changed := false
	links := nm.Polys[fromPoly].Links
	for i := range links {
		if !links[i].OffMesh && links[i].Neighbor == toPoly {
			links[i].Flags = flags
			changed = true
		}
	}
	if changed {
		nm.revision++
	}
	return changed
}

// FindCorridor computes the sequence of walkable polygon indices from start to goal under DefaultNavFilter.
// Returns (nil, false) if either endpoint is off the traversable NavMesh or no connected corridor exists.
func (nm *NavMesh) FindCorridor(start, goal math3d.Vec3) ([]int, bool) {
	return nm.FindCorridorWithFilter(start, goal, DefaultNavFilter())
}

// FindCorridorWithFilter computes the minimum-cost sequence of walkable polygon indices from start to goal under filter.
// Returns (nil, false) if either endpoint is off the traversable NavMesh or no connected corridor exists.
func (nm *NavMesh) FindCorridorWithFilter(start, goal math3d.Vec3, filter NavFilter) ([]int, bool) {
	corridor, _, ok := nm.findCorridorWithFilter(start, goal, filter)
	if !ok {
		return nil, false
	}
	return corridor, true
}

func (nm *NavMesh) findCorridorWithFilter(start, goal math3d.Vec3, filter NavFilter) ([]int, []NavLink, bool) {
	s := NewNavMeshSearchWithFilter(nm, start, goal, filter, ai.PathOptions{})
	s.step(-1)
	if s.status != ai.SearchComplete {
		return nil, nil, false
	}
	if s.samePoly || s.parent[s.goalNode] == -1 {
		dp := s.directPoly
		if dp < 0 {
			var ok bool
			dp, ok = nm.FindPolygonWithFilter(start, filter)
			if !ok {
				return nil, nil, false
			}
		}
		return []int{dp}, nil, true
	}
	corridor, portals := s.reconstructPortalCorridor(s.parent[s.goalNode])
	return corridor, portals, true
}

// FindPath computes a shortest 3D path from start to goal across the NavMesh
// using DefaultNavFilter, 3D portal-graph A*, and XZ-projected funnel smoothing
// with exact 3D height interpolation across sloped ramps and multi-level bridges.
//
// Returns (nil, false) if either endpoint is off the traversable NavMesh or no
// connected corridor exists between them.
func (nm *NavMesh) FindPath(start, goal math3d.Vec3) ([]math3d.Vec3, bool) {
	return nm.FindPathWithFilter(start, goal, DefaultNavFilter())
}

// FindPathWithFilter computes a minimum-cost 3D path from start to goal
// restricted to polygons allowed by filter, weighting traversal through each
// polygon by filter.AreaCost(area) and using an admissible 3D distance heuristic
// scaled by the minimum allowed area cost.
//
// Returns (nil, false) if either endpoint is off the traversable NavMesh or no
// connected corridor exists between them.
func (nm *NavMesh) FindPathWithFilter(start, goal math3d.Vec3, filter NavFilter) ([]math3d.Vec3, bool) {
	res := nm.FindPathWithFilterAndOptions(start, goal, filter, ai.PathOptions{})
	if !res.Found || res.Partial {
		return nil, false
	}
	return res.Path, true
}

// FindPathWithOptions computes a 3D path from start to goal under DefaultNavFilter and opts,
// supporting an expansion budget and optional partial paths ending at the closest reachable
// 3D point on the NavMesh when goal is off-mesh, disconnected, or beyond MaxExpansions.
func (nm *NavMesh) FindPathWithOptions(start, goal math3d.Vec3, opts ai.PathOptions) NavPathResult {
	return nm.FindPathWithFilterAndOptions(start, goal, DefaultNavFilter(), opts)
}

// FindPathWithFilterAndOptions computes a minimum-cost 3D path from start to goal
// restricted to polygons allowed by filter under opts, supporting an expansion budget
// and optional partial paths.
func (nm *NavMesh) FindPathWithFilterAndOptions(start, goal math3d.Vec3, filter NavFilter, opts ai.PathOptions) NavPathResult {
	s := NewNavMeshSearchWithFilter(nm, start, goal, filter, opts)
	s.step(-1)
	return s.Result()
}

// StartSearch begins an incremental 3D NavMesh search from start to goal under DefaultNavFilter and opts.
func (nm *NavMesh) StartSearch(start, goal math3d.Vec3, opts ai.PathOptions) *NavMeshSearch {
	return NewNavMeshSearchWithFilter(nm, start, goal, DefaultNavFilter(), opts)
}

// StartSearchWithFilter begins an incremental 3D NavMesh search from start to goal under filter and opts.
func (nm *NavMesh) StartSearchWithFilter(start, goal math3d.Vec3, filter NavFilter, opts ai.PathOptions) *NavMeshSearch {
	return NewNavMeshSearchWithFilter(nm, start, goal, filter, opts)
}

var _ ai.IncrementalSearch = (*NavMeshSearch)(nil)

type navShortcut3D struct {
	start  math3d.Vec3
	end    math3d.Vec3
	cost   float64
	toGoal float64
}

func (nm *NavMesh) offMeshLinkCost3D(link NavLink, filter NavFilter) (float64, bool) {
	_, area, filterFlags, _ := nm.offMeshMeta(link)
	if !filter.Allows(area, filterFlags) {
		return 0, false
	}
	mult := filter.AreaCost(area)
	if mult <= 0 || math.IsNaN(mult) || math.IsInf(mult, 0) {
		return 0, false
	}
	base := float64(link.Cost)
	if !(base > 0) || math.IsNaN(base) || math.IsInf(base, 0) {
		base = dist3D(link.Left, link.End)
	}
	c := base * mult
	if math.IsNaN(c) || math.IsInf(c, 0) {
		return 0, false
	}
	return c, true
}

func buildNavShortcuts3D(nm *NavMesh, goal math3d.Vec3, minCost float64, filter NavFilter, dst []navShortcut3D) ([]navShortcut3D, bool) {
	dst = dst[:0]
	if nm.offMeshCount == 0 {
		return dst, false
	}
	for i := range nm.Polys {
		for _, link := range nm.Polys[i].Links {
			if !link.OffMesh {
				continue
			}
			tp := &nm.Polys[link.Neighbor]
			if !filter.Allows(tp.Area, tp.Flags) {
				continue
			}
			tc := filter.AreaCost(tp.Area)
			if tc <= 0 || math.IsNaN(tc) || math.IsInf(tc, 0) {
				continue
			}
			effCost, ok := nm.offMeshLinkCost3D(link, filter)
			if !ok {
				continue
			}
			if effCost < float64(dist3D(link.Left, link.End)*minCost)-1e-9 {
				if len(dst) >= 64 {
					return dst[:0], true
				}
				dst = append(dst, navShortcut3D{
					start:  link.Left,
					end:    link.End,
					cost:   effCost,
					toGoal: float64(dist3D(link.End, goal) * minCost),
				})
			}
		}
	}
	k := len(dst)
	if k <= 1 {
		return dst, false
	}
	for pass := 0; pass < k-1; pass++ {
		changed := false
		for i := range k {
			best := dst[i].toGoal
			endI := dst[i].end
			for j := range k {
				if i == j {
					continue
				}
				cand := float64(dist3D(endI, dst[j].start)*minCost) + dst[j].cost + dst[j].toGoal
				if cand < best-1e-12 {
					best = cand
					changed = true
				}
			}
			dst[i].toGoal = best
		}
		if !changed {
			break
		}
	}
	return dst, false
}

func heuristic3D(pt, goal math3d.Vec3, minCost float64, shortcuts []navShortcut3D, zeroH bool) float64 {
	if zeroH {
		return 0
	}
	h := float64(dist3D(pt, goal) * minCost)
	for i := range shortcuts {
		cand := float64(dist3D(pt, shortcuts[i].start)*minCost) + shortcuts[i].cost + shortcuts[i].toGoal
		if cand < h {
			h = cand
		}
	}
	return h
}

// NavMeshSearch runs a deterministic 3D NavMesh portal A* search incrementally across bounded Step calls.
//
// Stepping a search with any positive per-step expansion budget produces the exact same path,
// links, partial flag, and expansion count as NavMesh.FindPathWithOptions / FindPathWithFilterAndOptions.
// *NavMeshSearch implements ai.IncrementalSearch so it can be scheduled directly on ai.PathQueue.
type NavMeshSearch struct {
	nm         *NavMesh
	start      math3d.Vec3
	goal       math3d.Vec3
	filter     NavFilter
	opts       ai.PathOptions
	allowed    []bool
	polyCost   []float64
	minCost    float64
	shortcuts  []navShortcut3D
	zeroH      bool
	isGoalPoly []bool
	linkOffset []int
	portalFrom []int
	portalLink []NavLink
	goalNode   int

	gScore  []float64
	parent  []int
	entryPt []math3d.Vec3
	closed  []bool
	heapPos []int
	open    []navAStarNode

	status       ai.SearchStatus
	exhausted    bool
	expansions   int
	samePoly     bool
	directPoly   int
	hasBest      bool
	bestPortalID int
	bestPt       math3d.Vec3
	bestH        float64
	bestG        float64
}

// NewNavMeshSearch initializes an incremental 3D NavMesh search from start to goal under DefaultNavFilter and opts.
func NewNavMeshSearch(nm *NavMesh, start, goal math3d.Vec3, opts ai.PathOptions) *NavMeshSearch {
	return NewNavMeshSearchWithFilter(nm, start, goal, DefaultNavFilter(), opts)
}

// NewNavMeshSearchWithFilter initializes an incremental 3D NavMesh search from start to goal under filter and opts.
func NewNavMeshSearchWithFilter(nm *NavMesh, start, goal math3d.Vec3, filter NavFilter, opts ai.PathOptions) *NavMeshSearch {
	s := &NavMeshSearch{
		nm:           nm,
		start:        start,
		goal:         goal,
		filter:       filter,
		opts:         opts,
		status:       ai.SearchFailed,
		directPoly:   -1,
		bestPortalID: -1,
	}
	if nm == nil || len(nm.Polys) == 0 {
		return s
	}

	n := len(nm.Polys)
	tol := nm.VerticalTolerance
	if tol <= 0 {
		tol = defaultVerticalTolerance
	}

	allowed := make([]bool, n)
	polyCost := make([]float64, n)
	minCost := math.Inf(1)

	bestStartDY := tol + 1
	bestGoalDY := tol + 1
	startDY := make([]float32, n)
	goalDY := make([]float32, n)
	for i := range n {
		startDY[i] = tol + 1
		goalDY[i] = tol + 1
	}

	for i := range n {
		poly := &nm.Polys[i]
		if !filter.Allows(poly.Area, poly.Flags) {
			continue
		}
		c := filter.AreaCost(poly.Area)
		if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
			continue
		}
		allowed[i] = true
		polyCost[i] = c
		if c < minCost {
			minCost = c
		}

		if poly.ContainsXZ(start.X, start.Z) {
			dy := float32(math.Abs(float64(start.Y - poly.HeightAt(start.X, start.Z))))
			if dy <= tol {
				startDY[i] = dy
				if dy < bestStartDY {
					bestStartDY = dy
				}
			}
		}
		if poly.ContainsXZ(goal.X, goal.Z) {
			dy := float32(math.Abs(float64(goal.Y - poly.HeightAt(goal.X, goal.Z))))
			if dy <= tol {
				goalDY[i] = dy
				if dy < bestGoalDY {
					bestGoalDY = dy
				}
			}
		}
	}

	if bestStartDY > tol {
		return s
	}

	const levelEps = 1e-4
	var startPolys []int
	isGoalPoly := make([]bool, n)
	firstGoalPoly := -1
	for i := range n {
		if !allowed[i] {
			continue
		}
		if startDY[i] <= bestStartDY+levelEps {
			startPolys = append(startPolys, i)
		}
		if bestGoalDY <= tol && goalDY[i] <= bestGoalDY+levelEps {
			isGoalPoly[i] = true
			if firstGoalPoly < 0 {
				firstGoalPoly = i
			}
		}
	}
	if len(startPolys) == 0 {
		return s
	}

	start.Y = nm.Polys[startPolys[0]].HeightAt(start.X, start.Z)
	s.start = start

	if firstGoalPoly >= 0 {
		goal.Y = nm.Polys[firstGoalPoly].HeightAt(goal.X, goal.Z)
		s.goal = goal
	} else if !opts.AllowPartial {
		return s
	}

	s.shortcuts, s.zeroH = buildNavShortcuts3D(nm, s.goal, minCost, filter, nil)

	directPossible := false
	directCost := math.Inf(1)
	if firstGoalPoly >= 0 {
		for _, sp := range startPolys {
			if isGoalPoly[sp] {
				if start.ApproxEqual(goal, 1e-5) {
					s.samePoly = true
					s.directPoly = sp
					s.status = ai.SearchComplete
					return s
				}
				dc := dist3D(start, goal) * polyCost[sp]
				if dc < directCost {
					directCost = dc
					directPossible = true
					s.directPoly = sp
				}
			}
		}
		if directPossible && directCost <= heuristic3D(start, goal, minCost, s.shortcuts, s.zeroH)+1e-9 {
			s.samePoly = true
			s.status = ai.SearchComplete
			return s
		}
	}

	if opts.AllowPartial {
		for _, sp := range startPolys {
			cp := closestPointOnNavPoly(&nm.Polys[sp], s.goal)
			h := dist3D(cp, s.goal)
			g := dist3D(start, cp) * polyCost[sp]
			if !s.hasBest || h < s.bestH || (h == s.bestH && g < s.bestG) {
				s.hasBest = true
				s.bestPortalID = -1
				s.bestPt = cp
				s.bestH = h
				s.bestG = g
			}
		}
	}

	linkOffset := make([]int, n+1)
	for i := range n {
		linkOffset[i+1] = linkOffset[i] + len(nm.Polys[i].Links)
	}
	totalPortals := linkOffset[n]

	portalFrom := make([]int, totalPortals)
	portalLink := make([]NavLink, totalPortals)
	for i := range n {
		base := linkOffset[i]
		for k, link := range nm.Polys[i].Links {
			portalFrom[base+k] = i
			portalLink[base+k] = link
		}
	}

	goalNode := totalPortals
	nodeCount := goalNode + 1

	s.allowed = allowed
	s.polyCost = polyCost
	s.minCost = minCost
	s.isGoalPoly = isGoalPoly
	s.linkOffset = linkOffset
	s.portalFrom = portalFrom
	s.portalLink = portalLink
	s.goalNode = goalNode

	s.gScore = make([]float64, nodeCount)
	s.parent = make([]int, nodeCount)
	s.entryPt = make([]math3d.Vec3, nodeCount)
	s.closed = make([]bool, nodeCount)
	s.heapPos = make([]int, nodeCount)
	for i := range nodeCount {
		s.gScore[i] = math.Inf(1)
		s.parent[i] = -1
		s.heapPos[i] = -1
	}
	s.open = make([]navAStarNode, 0, min(nodeCount, 64))

	if directPossible {
		s.gScore[goalNode] = directCost
		s.parent[goalNode] = -1
		s.pushOrUpdate(goalNode, directCost, 0)
	}

	for _, sp := range startPolys {
		spCost := polyCost[sp]
		base := linkOffset[sp]
		for k, link := range nm.Polys[sp].Links {
			if !allowed[link.Neighbor] {
				continue
			}
			pid := base + k
			var g float64
			var exitPt math3d.Vec3
			if link.OffMesh {
				cost, ok := nm.offMeshLinkCost3D(link, filter)
				if !ok {
					continue
				}
				g = float64(dist3D(start, link.Left)*spCost) + cost
				exitPt = link.End
			} else {
				exitPt = bestPortalPoint3D(start, s.goal, link.Left, link.Right)
				g = dist3D(start, exitPt) * spCost
			}
			if g < s.gScore[pid] {
				s.gScore[pid] = g
				s.entryPt[pid] = exitPt
				s.parent[pid] = -1
				h := heuristic3D(exitPt, s.goal, minCost, s.shortcuts, s.zeroH)
				s.pushOrUpdate(pid, g+h, h)
			}
		}
	}

	if len(s.open) == 0 {
		if opts.AllowPartial && s.hasBest {
			s.status = ai.SearchPartial
		} else {
			s.status = ai.SearchFailed
		}
		return s
	}

	s.status = ai.SearchInProgress
	return s
}

func (s *NavMeshSearch) pushOrUpdate(id int, f, h float64) {
	if pos := s.heapPos[id]; pos >= 0 {
		s.open[pos].f = f
		s.open[pos].h = h
		navSiftUp(s.open, s.heapPos, pos)
		return
	}
	pos := len(s.open)
	s.open = append(s.open, navAStarNode{id: id, f: f, h: h})
	s.heapPos[id] = pos
	navSiftUp(s.open, s.heapPos, pos)
}

// Step advances the search by at most maxExpansions portal node expansions and returns
// the updated status and exact number of expansions performed.
// When maxExpansions <= 0 or the search is already finished, Step performs 0 expansions.
func (s *NavMeshSearch) Step(maxExpansions int) (status ai.SearchStatus, expansions int) {
	if s == nil {
		return ai.SearchFailed, 0
	}
	if maxExpansions <= 0 || s.status.Done() {
		return s.status, 0
	}
	return s.step(maxExpansions)
}

func (s *NavMeshSearch) step(stepLimit int) (ai.SearchStatus, int) {
	if s.status.Done() {
		return s.status, 0
	}

	budget := stepLimit
	if s.opts.MaxExpansions > 0 {
		rem := s.opts.MaxExpansions - s.expansions
		if rem <= 0 {
			s.finishExhausted()
			return s.status, 0
		}
		if budget < 0 || rem < budget {
			budget = rem
		}
	}

	nm := s.nm
	goal := s.goal
	done := 0

	for len(s.open) > 0 && (budget < 0 || done < budget) {
		cur := navHeapPop(&s.open, s.heapPos)
		done++
		s.expansions++

		if cur.id == s.goalNode {
			s.status = ai.SearchComplete
			return s.status, done
		}
		s.closed[cur.id] = true

		fromPoly := s.portalFrom[cur.id]
		link := s.portalLink[cur.id]
		toPoly := link.Neighbor
		curG := s.gScore[cur.id]
		curPt := s.entryPt[cur.id]
		toCost := s.polyCost[toPoly]

		if s.opts.AllowPartial {
			cp := closestPointOnNavPoly(&nm.Polys[toPoly], goal)
			h := dist3D(cp, goal)
			g := curG + dist3D(curPt, cp)*toCost
			if !s.hasBest || h < s.bestH || (h == s.bestH && (g < s.bestG || (g == s.bestG && cur.id < s.bestPortalID))) {
				s.hasBest = true
				s.bestPortalID = cur.id
				s.bestPt = cp
				s.bestH = h
				s.bestG = g
			}
		}

		if s.isGoalPoly[toPoly] {
			ng := curG + dist3D(curPt, goal)*toCost
			if ng < s.gScore[s.goalNode] {
				s.gScore[s.goalNode] = ng
				s.parent[s.goalNode] = cur.id
				s.pushOrUpdate(s.goalNode, ng, 0)
			}
		}

		base := s.linkOffset[toPoly]
		for k, nlink := range nm.Polys[toPoly].Links {
			npid := base + k
			if s.closed[npid] {
				continue
			}
			if (!nlink.OffMesh && nlink.Neighbor == fromPoly) || !s.allowed[nlink.Neighbor] {
				continue
			}
			var ng float64
			var exitPt math3d.Vec3
			if nlink.OffMesh {
				cost, ok := nm.offMeshLinkCost3D(nlink, s.filter)
				if !ok {
					continue
				}
				ng = curG + float64(dist3D(curPt, nlink.Left)*toCost) + cost
				exitPt = nlink.End
			} else {
				exitPt = bestPortalPoint3D(curPt, goal, nlink.Left, nlink.Right)
				ng = curG + dist3D(curPt, exitPt)*toCost
			}
			if ng < s.gScore[npid] {
				s.gScore[npid] = ng
				s.entryPt[npid] = exitPt
				s.parent[npid] = cur.id
				h := heuristic3D(exitPt, goal, s.minCost, s.shortcuts, s.zeroH)
				s.pushOrUpdate(npid, ng+h, h)
			}
		}
	}

	if len(s.open) == 0 {
		s.exhausted = false
		if s.opts.AllowPartial && s.hasBest {
			s.status = ai.SearchPartial
		} else {
			s.status = ai.SearchFailed
		}
	} else if s.opts.MaxExpansions > 0 && s.expansions >= s.opts.MaxExpansions {
		s.finishExhausted()
	}

	return s.status, done
}

func (s *NavMeshSearch) finishExhausted() {
	s.exhausted = true
	if s.opts.AllowPartial && s.hasBest {
		s.status = ai.SearchPartial
	} else {
		s.status = ai.SearchFailed
	}
}

// Status returns the search's current status.
func (s *NavMeshSearch) Status() ai.SearchStatus {
	if s == nil {
		return ai.SearchFailed
	}
	return s.status
}

// Expansions returns the cumulative number of portal nodes expanded so far.
func (s *NavMeshSearch) Expansions() int {
	if s == nil {
		return 0
	}
	return s.expansions
}

// Result returns the search outcome once finished (or a result with Found == false while still in progress).
func (s *NavMeshSearch) Result() NavPathResult {
	if s == nil {
		return NavPathResult{}
	}
	res := NavPathResult{
		Exhausted:  s.exhausted,
		Expansions: s.expansions,
	}
	switch s.status {
	case ai.SearchComplete:
		res.Found = true
		if s.samePoly || s.parent[s.goalNode] == -1 {
			if s.start.ApproxEqual(s.goal, 1e-5) {
				res.Path = []math3d.Vec3{s.start}
			} else {
				res.Path = []math3d.Vec3{s.start, s.goal}
			}
			return res
		}
		corridor, portals := s.reconstructPortalCorridor(s.parent[s.goalNode])
		res.Path, res.Links = s.nm.smoothCorridorWithLinks(corridor, portals, s.start, s.goal)
		return res

	case ai.SearchPartial:
		res.Found = true
		res.Partial = true
		if s.bestPortalID < 0 {
			if s.start.ApproxEqual(s.bestPt, 1e-5) {
				res.Path = []math3d.Vec3{s.start}
			} else {
				res.Path = []math3d.Vec3{s.start, s.bestPt}
			}
			return res
		}
		corridor, portals := s.reconstructPortalCorridor(s.bestPortalID)
		res.Path, res.Links = s.nm.smoothCorridorWithLinks(corridor, portals, s.start, s.bestPt)
		return res

	default:
		return res
	}
}

func (s *NavMeshSearch) reconstructPortalCorridor(lastPortalID int) ([]int, []NavLink) {
	var portalIDs []int
	for id := lastPortalID; id != -1; id = s.parent[id] {
		portalIDs = append(portalIDs, id)
	}
	slices.Reverse(portalIDs)

	corridor := make([]int, len(portalIDs)+1)
	portals := make([]NavLink, len(portalIDs))
	corridor[0] = s.portalFrom[portalIDs[0]]
	for i, pid := range portalIDs {
		l := s.portalLink[pid]
		portals[i] = l
		corridor[i+1] = l.Neighbor
	}
	return corridor, portals
}

type navAStarNode struct {
	id int
	f  float64
	h  float64
}

func navAStarLess(x, y navAStarNode) bool {
	if x.f != y.f {
		return x.f < y.f
	}
	if x.h != y.h {
		return x.h < y.h
	}
	return x.id < y.id
}

func navHeapPop(open *[]navAStarNode, heapPos []int) navAStarNode {
	s := *open
	top := s[0]
	heapPos[top.id] = -1
	last := len(s) - 1
	if last == 0 {
		*open = s[:0]
		return top
	}
	s[0] = s[last]
	heapPos[s[0].id] = 0
	s = s[:last]
	*open = s
	navSiftDown(s, heapPos, 0)
	return top
}

func navSiftUp(s []navAStarNode, heapPos []int, i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !navAStarLess(s[i], s[p]) {
			break
		}
		s[i], s[p] = s[p], s[i]
		heapPos[s[i].id] = i
		heapPos[s[p].id] = p
		i = p
	}
}

func navSiftDown(s []navAStarNode, heapPos []int, i int) {
	n := len(s)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && navAStarLess(s[r], s[l]) {
			best = r
		}
		if !navAStarLess(s[best], s[i]) {
			break
		}
		s[i], s[best] = s[best], s[i]
		heapPos[s[i].id] = i
		heapPos[s[best].id] = best
		i = best
	}
}

// bestPortalPoint3D returns the point p on 3D segment [left, right] minimizing dist(from, p) + dist(p, goal).
func bestPortalPoint3D(from, goal, left, right math3d.Vec3) math3d.Vec3 {
	segX := float64(right.X - left.X)
	segY := float64(right.Y - left.Y)
	segZ := float64(right.Z - left.Z)
	segLen := math.Sqrt(segX*segX + segY*segY + segZ*segZ)
	if segLen <= 1e-12 {
		return left
	}
	invLen := 1.0 / segLen
	ux, uy, uz := segX*invLen, segY*invLen, segZ*invLen

	fX, fY, fZ := float64(from.X-left.X), float64(from.Y-left.Y), float64(from.Z-left.Z)
	gX, gY, gZ := float64(goal.X-left.X), float64(goal.Y-left.Y), float64(goal.Z-left.Z)

	s1 := fX*ux + fY*uy + fZ*uz
	s2 := gX*ux + gY*uy + gZ*uz

	p1X, p1Y, p1Z := fX-s1*ux, fY-s1*uy, fZ-s1*uz
	p2X, p2Y, p2Z := gX-s2*ux, gY-s2*uy, gZ-s2*uz

	a1 := math.Sqrt(p1X*p1X + p1Y*p1Y + p1Z*p1Z)
	a2 := math.Sqrt(p2X*p2X + p2Y*p2Y + p2Z*p2Z)

	var s float64
	if a1+a2 > 1e-12 {
		s = (s1*a2 + s2*a1) / (a1 + a2)
	} else {
		s = 0.5 * (s1 + s2)
	}
	s = min(max(s, 0), segLen)
	t := float32(s / segLen)
	return left.Lerp(right, t)
}

type funnelCorner struct {
	pos       math3d.Vec3
	portalIdx int
}

// smoothCorridorWithLinks runs the Simple Stupid Funnel Algorithm in XZ across the
// ordered 3D portal corridor, splitting cleanly across any off-mesh links, inserts
// exact 3D portal-edge crossings wherever consecutive polygons change plane/slope,
// and records each traversed off-mesh link segment.
func (nm *NavMesh) smoothCorridorWithLinks(corridor []int, portals []NavLink, start, goal math3d.Vec3) ([]math3d.Vec3, []NavPathLink) {
	hasOffMesh := false
	for _, p := range portals {
		if p.OffMesh {
			hasOffMesh = true
			break
		}
	}
	if !hasOffMesh {
		return nm.smoothCorridorSegment(corridor, portals, start, goal), nil
	}

	var out []math3d.Vec3
	var links []NavPathLink
	segStart := start
	segBegin := 0
	for i, link := range portals {
		if !link.OffMesh {
			continue
		}
		seg := nm.smoothCorridorSegment(corridor[segBegin:i+1], portals[segBegin:i], segStart, link.Left)
		for _, pt := range seg {
			if len(out) == 0 || !out[len(out)-1].ApproxEqual(pt, 1e-4) {
				out = append(out, pt)
			}
		}
		if len(out) == 0 {
			out = append(out, link.Left)
		}
		startIdx := len(out) - 1
		out = append(out, link.End)
		endIdx := len(out) - 1
		radius, area, _, userData := nm.offMeshMeta(link)
		links = append(links, NavPathLink{
			StartIndex: startIdx,
			EndIndex:   endIdx,
			FromPoly:   corridor[i],
			ToPoly:     link.Neighbor,
			Start:      link.Left,
			End:        link.End,
			Radius:     radius,
			Area:       area,
			Flags:      link.Flags,
			LinkID:     link.LinkID,
			UserData:   userData,
		})
		segStart = link.End
		segBegin = i + 1
	}
	tail := nm.smoothCorridorSegment(corridor[segBegin:], portals[segBegin:], segStart, goal)
	for _, pt := range tail {
		if len(out) == 0 || !out[len(out)-1].ApproxEqual(pt, 1e-4) {
			out = append(out, pt)
		}
	}
	return out, links
}

func (nm *NavMesh) smoothCorridorSegment(corridor []int, portals []NavLink, start, goal math3d.Vec3) []math3d.Vec3 {
	if len(portals) == 0 {
		if start.ApproxEqual(goal, 1e-4) {
			return []math3d.Vec3{start}
		}
		return []math3d.Vec3{start, goal}
	}
	m := len(portals) + 2
	portalLeft := make([]math3d.Vec3, m)
	portalRight := make([]math3d.Vec3, m)

	portalLeft[0] = start
	portalRight[0] = start
	for i, p := range portals {
		portalLeft[i+1] = p.Left
		portalRight[i+1] = p.Right
	}
	portalLeft[m-1] = goal
	portalRight[m-1] = goal

	corners := []funnelCorner{{pos: start, portalIdx: 0}}

	portalApex := start
	leftPt := portalLeft[0]
	rightPt := portalRight[0]
	apexIdx, leftIdx, rightIdx := 0, 0, 0

	for i := 1; i < m; i++ {
		left := portalLeft[i]
		right := portalRight[i]

		// Update right funnel boundary.
		if triArea2(portalApex, rightPt, right) <= 0 {
			if sameXZ(portalApex, rightPt) || sameXZ(portalApex, leftPt) || triArea2(portalApex, leftPt, right) > 0 {
				rightPt = right
				rightIdx = i
			} else {
				// Right crossed over left: commit leftPt as a corner.
				corners = appendCorner(corners, funnelCorner{pos: leftPt, portalIdx: leftIdx})
				portalApex = leftPt
				apexIdx = leftIdx
				leftPt = portalApex
				rightPt = portalApex
				leftIdx = apexIdx
				rightIdx = apexIdx
				i = apexIdx
				continue
			}
		}

		// Update left funnel boundary.
		if triArea2(portalApex, leftPt, left) >= 0 {
			if sameXZ(portalApex, leftPt) || sameXZ(portalApex, rightPt) || triArea2(portalApex, rightPt, left) < 0 {
				leftPt = left
				leftIdx = i
			} else {
				// Left crossed over right: commit rightPt as a corner.
				corners = appendCorner(corners, funnelCorner{pos: rightPt, portalIdx: rightIdx})
				portalApex = rightPt
				apexIdx = rightIdx
				leftPt = portalApex
				rightPt = portalApex
				leftIdx = apexIdx
				rightIdx = apexIdx
				i = apexIdx
				continue
			}
		}
	}

	corners = appendCorner(corners, funnelCorner{pos: goal, portalIdx: m - 1})

	// Insert exact 3D portal crossings where consecutive polygons have different
	// planes (e.g. floor -> ramp -> bridge) so Y follows the 3D surface exactly.
	out := make([]math3d.Vec3, 0, len(corners)+len(portals))
	out = append(out, corners[0].pos)

	for c := 1; c < len(corners); c++ {
		prev := corners[c-1]
		next := corners[c]
		for j := prev.portalIdx + 1; j < next.portalIdx; j++ {
			pA := &nm.Polys[corridor[j-1]]
			pB := &nm.Polys[corridor[j]]
			if samePlane(pA, pB) {
				continue
			}
			link := portals[j-1]
			s := intersectXZPortalParam(prev.pos, next.pos, link.Left, link.Right)
			cross := link.Left.Lerp(link.Right, s)
			if !out[len(out)-1].ApproxEqual(cross, 1e-4) {
				out = append(out, cross)
			}
		}
		if !out[len(out)-1].ApproxEqual(next.pos, 1e-4) {
			out = append(out, next.pos)
		}
	}

	return out
}

// nextFunnelCorner returns the first visible XZ funnel corner from pos along portals toward goal.
// isGoal is true when the returned corner is the final goal with straight line of sight through the corridor.
// offMeshIdx is the index into portals if the returned corner is an off-mesh link entry point, or -1 otherwise.
func (nm *NavMesh) nextFunnelCorner(pos, goal math3d.Vec3, portals []NavLink) (corner math3d.Vec3, isGoal bool, offMeshIdx int) {
	if len(portals) == 0 {
		return goal, true, -1
	}
	if portals[0].OffMesh {
		return portals[0].Left, false, 0
	}

	endPt := goal
	endIsGoal := true
	endOffMesh := -1
	segLen := len(portals)
	for i, link := range portals {
		if link.OffMesh {
			endPt = link.Left
			endIsGoal = false
			endOffMesh = i
			segLen = i
			break
		}
	}

	m := segLen + 2
	portalLeft := make([]math3d.Vec3, m)
	portalRight := make([]math3d.Vec3, m)
	portalLeft[0] = pos
	portalRight[0] = pos
	for i := range segLen {
		portalLeft[i+1] = portals[i].Left
		portalRight[i+1] = portals[i].Right
	}
	portalLeft[m-1] = endPt
	portalRight[m-1] = endPt

	const cornerEpsSq float32 = 1e-6 // 1e-3 world units on XZ
	portalApex := pos
	leftPt := pos
	rightPt := pos
	apexIdx, leftIdx, rightIdx := 0, 0, 0

	for i := 1; i < m; i++ {
		left := portalLeft[i]
		right := portalRight[i]

		if triArea2(portalApex, rightPt, right) <= 0 {
			if sameXZ(portalApex, rightPt) || sameXZ(portalApex, leftPt) || triArea2(portalApex, leftPt, right) > 0 {
				rightPt = right
				rightIdx = i
			} else {
				if distXZSq(pos, leftPt) > cornerEpsSq {
					return leftPt, false, -1
				}
				portalApex = leftPt
				apexIdx = leftIdx
				leftPt = portalApex
				rightPt = portalApex
				leftIdx = apexIdx
				rightIdx = apexIdx
				i = apexIdx
				continue
			}
		}

		if triArea2(portalApex, leftPt, left) >= 0 {
			if sameXZ(portalApex, leftPt) || sameXZ(portalApex, rightPt) || triArea2(portalApex, rightPt, left) < 0 {
				leftPt = left
				leftIdx = i
			} else {
				if distXZSq(pos, rightPt) > cornerEpsSq {
					return rightPt, false, -1
				}
				portalApex = rightPt
				apexIdx = rightIdx
				leftPt = portalApex
				rightPt = portalApex
				leftIdx = apexIdx
				rightIdx = apexIdx
				i = apexIdx
				continue
			}
		}
	}

	return endPt, endIsGoal, endOffMesh
}

func distXZSq(a, b math3d.Vec3) float32 {
	dx := a.X - b.X
	dz := a.Z - b.Z
	return dx*dx + dz*dz
}

func appendCorner(corners []funnelCorner, c funnelCorner) []funnelCorner {
	last := corners[len(corners)-1]
	if last.pos.ApproxEqual(c.pos, 1e-4) {
		if c.portalIdx > last.portalIdx {
			corners[len(corners)-1].portalIdx = c.portalIdx
		}
		return corners
	}
	return append(corners, c)
}

func samePlane(a, b *NavPoly) bool {
	return a.Normal.Dot(b.Normal) >= 1-1e-4 && math.Abs(float64(a.PlaneD-b.PlaneD)) <= 1e-3
}

// intersectXZPortalParam returns parameter s in [0, 1] where the XZ line
// segment a0->a1 intersects the portal edge b0->b1.
func intersectXZPortalParam(a0, a1, b0, b1 math3d.Vec3) float32 {
	daX, daZ := a1.X-a0.X, a1.Z-a0.Z
	dbX, dbZ := b1.X-b0.X, b1.Z-b0.Z
	denom := daX*dbZ - daZ*dbX
	if math.Abs(float64(denom)) < 1e-7 {
		// Segment is nearly parallel to portal: project midpoint onto portal.
		midX := 0.5 * (a0.X + a1.X)
		midZ := 0.5 * (a0.Z + a1.Z)
		lenSq := dbX*dbX + dbZ*dbZ
		if lenSq < 1e-12 {
			return 0.5
		}
		s := ((midX-b0.X)*dbX + (midZ-b0.Z)*dbZ) / lenSq
		return math3d.Clamp(s, 0, 1)
	}
	num := (b0.X-a0.X)*daZ - (b0.Z-a0.Z)*daX
	return math3d.Clamp(num/denom, 0, 1)
}

// triArea2 returns twice the signed area of triangle (a, b, c) projected onto XZ.
func triArea2(a, b, c math3d.Vec3) float32 {
	return (b.X-a.X)*(c.Z-a.Z) - (b.Z-a.Z)*(c.X-a.X)
}

func sameXZ(a, b math3d.Vec3) bool {
	dx := a.X - b.X
	dz := a.Z - b.Z
	return dx*dx+dz*dz < 1e-10
}

func newellNormal(verts []math3d.Vec3) math3d.Vec3 {
	var n math3d.Vec3
	count := len(verts)
	for i := range count {
		a := verts[i]
		b := verts[(i+1)%count]
		n.X += (a.Y - b.Y) * (a.Z + b.Z)
		n.Y += (a.Z - b.Z) * (a.X + b.X)
		n.Z += (a.X - b.X) * (a.Y + b.Y)
	}
	return n
}

func reverseVec3(s []math3d.Vec3) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
