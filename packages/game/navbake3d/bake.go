package navbake3d

import (
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

// ErrNoGeometry is returned by Bake and BakeMesh when no triangle of the geometry is on a layer the settings select.
var ErrNoGeometry = errors.New("navbake3d: no level geometry on the selected layers")

// Bake bakes one navigation mesh per agent type from g's triangles on the layers settings.LayerMask selects, in the order agents lists them, and returns them as a File ready to Encode. Zero fields of settings and of each agent type take their defaults, and the File records the values used.
//
// The same geometry, settings and agent types always bake the same File, so encoding it twice writes the same bytes.
func Bake(g *Geometry, settings Settings, agents []AgentType) (*File, error) {
	settings = settings.withDefaults()
	if err := settings.validate("settings"); err != nil {
		return nil, err
	}
	agents, err := validateAgents(agents)
	if err != nil {
		return nil, err
	}
	f := &File{Version: FormatVersion, Settings: settings}
	for _, a := range agents {
		m, err := bakeMesh(g, settings, a)
		if err != nil {
			return nil, fmt.Errorf("navbake3d: agent %q: %w", a.Name, err)
		}
		f.Meshes = append(f.Meshes, *m)
	}
	return f, nil
}

// BakeMesh bakes the navigation mesh of one agent type, as Bake does for each.
func BakeMesh(g *Geometry, settings Settings, agent AgentType) (*Mesh, error) {
	settings = settings.withDefaults()
	if err := settings.validate("settings"); err != nil {
		return nil, err
	}
	agent = agent.withDefaults()
	if err := agent.validate("agent"); err != nil {
		return nil, err
	}
	return bakeMesh(g, settings, agent)
}

// BakeTile bakes one independent world3d.NavTile for chunk coordinate coord from g's triangles
// overlapping that tile (expanded by an erosion border so wall clearance near tile edges matches
// a full-level bake). Resulting walkable triangles are clipped cleanly to the tile's exact XZ
// bounds so neighboring tiles link seamlessly when installed in a world3d.TiledNavMesh.
//
// When no selected geometry overlaps the tile, BakeTile returns an empty *world3d.NavTile and a nil error.
func BakeTile(g *Geometry, settings Settings, agent AgentType, coord world3d.ChunkCoord, tileSize float32, origin math3d.Vec3) (*world3d.NavTile, error) {
	settings = settings.withDefaults()
	if err := settings.validate("settings"); err != nil {
		return nil, err
	}
	agent = agent.withDefaults()
	if err := agent.validate("agent"); err != nil {
		return nil, err
	}
	if !positive(tileSize) {
		return nil, fmt.Errorf("navbake3d: tileSize %v must be a finite positive number", tileSize)
	}
	if !finiteVec(origin) {
		return nil, fmt.Errorf("navbake3d: tile origin %v must be finite", origin)
	}
	if g == nil || len(g.layers) == 0 {
		return world3d.BakeNavTile(coord, tileSize, origin, nil, math.Pi/2), nil
	}

	cs, ch := settings.CellSize, settings.CellHeight
	cs64 := float64(cs)
	border := (math.Ceil(float64(agent.Radius)/cs64) + 3) * cs64

	ts := float64(tileSize)
	tileMinX := float64(origin.X) + float64(coord.X)*ts
	tileMaxX := float64(origin.X) + (float64(coord.X)+1)*ts
	tileMinZ := float64(origin.Z) + float64(coord.Z)*ts
	tileMaxZ := float64(origin.Z) + (float64(coord.Z)+1)*ts

	// Align the padded voxel grid origin to global cs steps so adjacent tiles share identical voxel columns across borders.
	padMinX := float32(math.Floor((tileMinX-border)/cs64) * cs64)
	padMaxX := float32(math.Ceil((tileMaxX+border)/cs64) * cs64)
	padMinZ := float32(math.Floor((tileMinZ-border)/cs64) * cs64)
	padMaxZ := float32(math.Ceil((tileMaxZ+border)/cs64) * cs64)
	if !finiteVec(math3d.Vec3{X: padMinX, Y: 0, Z: padMinZ}) || !finiteVec(math3d.Vec3{X: padMaxX, Y: 0, Z: padMaxZ}) {
		return nil, fmt.Errorf("navbake3d: tile coord %v exceeds finite float32 bounds", coord)
	}

	minY, maxY := float32(math.Inf(1)), float32(math.Inf(-1))
	hits := 0
	for t, l := range g.layers {
		if l&settings.LayerMask == 0 {
			continue
		}
		v0, v1, v2 := g.verts[t*3], g.verts[t*3+1], g.verts[t*3+2]
		tMin := v0.Min(v1).Min(v2)
		tMax := v0.Max(v1).Max(v2)
		if tMax.X < padMinX || tMin.X > padMaxX || tMax.Z < padMinZ || tMin.Z > padMaxZ {
			continue
		}
		minY = min(minY, tMin.Y)
		maxY = max(maxY, tMax.Y)
		hits++
	}
	if hits == 0 {
		return world3d.BakeNavTile(coord, tileSize, origin, nil, math.Pi/2), nil
	}
	if maxY-minY < ch {
		maxY = minY + ch
	}

	b := math3d.AABB{
		Min: math3d.Vec3{X: padMinX, Y: minY, Z: padMinZ},
		Max: math3d.Vec3{X: padMaxX, Y: maxY, Z: padMaxZ},
	}
	m, err := bakeMeshInBounds(g, settings, agent, b)
	if err != nil {
		return nil, err
	}
	var inputs []world3d.NavPolygonInput
	for pi := range m.Polygons {
		p := &m.Polygons[pi]
		for _, tri := range p.Triangles {
			v := m.TriangleVertices(p, tri)
			inputs = append(inputs, world3d.NavPolygonInput{
				Vertices: []math3d.Vec3{v[0], v[1], v[2]},
				Area:     world3d.NavAreaDefault,
				Flags:    world3d.NavFlagWalk,
			})
		}
	}
	return world3d.BakeNavTile(coord, tileSize, origin, inputs, math.Pi/2), nil
}

// bakeMesh runs the pipeline for one agent with validated, defaulted settings.
func bakeMesh(g *Geometry, s Settings, a AgentType) (*Mesh, error) {
	if g == nil {
		return nil, ErrNoGeometry
	}
	b, n := g.bounds(s.LayerMask)
	if n == 0 {
		return nil, ErrNoGeometry
	}
	return bakeMeshInBounds(g, s, a, b)
}

func bakeMeshInBounds(g *Geometry, s Settings, a AgentType, b math3d.AABB) (*Mesh, error) {
	cs, ch := s.CellSize, s.CellHeight
	// Sizes in cells are checked while they are still floats, since converting a float too large for an int gives a different, meaningless value on each architecture.
	cells := func(size, cell float32) float64 { return math.Ceil(float64(size / cell)) }
	wf, df := max(cells(b.Max.X-b.Min.X, cs), 1), max(cells(b.Max.Z-b.Min.Z, cs), 1)
	if wf*df > MaxCells {
		return nil, fmt.Errorf("the level is %v by %v cells, more than %d; use a larger cellSize", wf, df, MaxCells)
	}
	width, depth := int(wf), int(df)
	if (b.Max.Y-b.Min.Y)/ch > spanMaxHeight/2 {
		return nil, fmt.Errorf("the level is too tall for cellHeight %v; use a larger cellHeight", ch)
	}
	for _, c := range []struct {
		name  string
		v     float64
		limit float64
	}{
		{"height", cells(a.Height, ch), spanMaxHeight / 2},
		{"maxClimb", math.Floor(float64(a.MaxClimb / ch)), spanMaxHeight / 2},
		{"radius", cells(a.Radius, cs), MaxCells},
	} {
		if c.v > c.limit {
			return nil, fmt.Errorf("agent %s is %v cells, more than %v; use a smaller %s or larger cells", c.name, c.v, c.limit, c.name)
		}
	}
	walkHeight := int32(cells(a.Height, ch))
	walkClimb := int32(math.Floor(float64(a.MaxClimb / ch)))
	walkRadius := int32(cells(a.Radius, cs))
	minNormalY := float32(math.Cos(float64(a.MaxSlope) * math.Pi / 180))

	hf := newHeightfield(width, depth, b.Min, b.Max, cs, ch)
	buf := newRasterBuffers()
	areaOf := func(t int) uint8 {
		v0, v1, v2 := g.verts[t*3], g.verts[t*3+1], g.verts[t*3+2]
		if norm(cross(v1.Sub(v0), v2.Sub(v0))).Y > minNormalY {
			return areaWalkable
		}
		return areaNull
	}
	solid := make([]bool, len(g.layers))
	var areas []uint8
	for _, r := range g.solids {
		if g.layers[r[0]]&s.LayerMask == 0 {
			continue
		}
		areas = areas[:0]
		for t := r[0]; t < r[1]; t++ {
			solid[t] = true
			areas = append(areas, areaOf(t))
		}
		hf.rasterizeSolid(g.verts[r[0]*3:r[1]*3], areas, walkClimb, buf)
	}
	for t, l := range g.layers {
		if l&s.LayerMask == 0 || solid[t] {
			continue
		}
		area := areaOf(t)
		hf.rasterizeTriangle(g.verts[t*3], g.verts[t*3+1], g.verts[t*3+2], buf, func(x, z int, smin, smax int32) {
			hf.addSpan(x, z, smin, smax, area, walkClimb)
		})
	}
	hf.filterLowHangingObstacles(walkClimb)
	hf.filterLedges(walkHeight, walkClimb)
	hf.filterLowCeilings(walkHeight)

	chf := buildCompact(hf, walkHeight, walkClimb)
	chf.erode(walkRadius)
	// An island needs at most every cell of the grid and an edge spans at most its diagonal, so larger settings are clamped there: every island is dropped, or no edge is split.
	minArea := int(min(math.Ceil(float64(s.MinRegionArea/float32(cs*cs))), MaxCells+1))
	chf.buildRegions(minArea)
	maxEdge := int32(min(float64(s.MaxEdgeLength/cs), 2*MaxCells))
	contours := chf.buildContours(s.MaxSimplificationError/cs, maxEdge)
	pm := buildPolyMesh(contours, s.MaxVertsPerPoly)
	m := chf.meshFromPolys(pm, s, a)
	if len(g.links) > 0 {
		m.Links = make([]OffMeshLink, len(g.links))
		for i, l := range g.links {
			l.Start = point(l.Start.Vec3())
			l.End = point(l.End.Vec3())
			l.Radius = noNegZero(l.Radius)
			l.Cost = noNegZero(l.Cost)
			m.Links[i] = l
		}
	}
	return m, nil
}

// meshFromPolys converts the polygon mesh to world space, adds each polygon's height detail and drops vertices no polygon uses.
func (chf *compactHeightfield) meshFromPolys(pm *polyMesh, s Settings, a AgentType) *Mesh {
	verts := make([]math3d.Vec3, len(pm.verts))
	for i, v := range pm.verts {
		verts[i] = chf.world(v)
	}
	db := &detailBuilder{
		chf:       chf,
		verts:     verts,
		sampleDst: s.DetailSampleDistance,
		maxErr:    s.DetailSampleMaxError,
		maxDy:     a.Height / 2,
		edges:     make(map[[2]int32][]math3d.Vec3),
	}
	m := &Mesh{Agent: a}
	remap := make([]int, len(verts))
	for i := range remap {
		remap[i] = -1
	}
	for pi, poly := range pm.polys {
		d := db.build(poly, pm.regs[pi])
		if len(d.tris) == 0 {
			continue
		}
		p := Polygon{Vertices: make([]int, len(poly))}
		for i, vi := range poly {
			if remap[vi] < 0 {
				remap[vi] = len(m.Vertices)
				m.Vertices = append(m.Vertices, point(verts[vi]))
			}
			p.Vertices[i] = remap[vi]
		}
		for _, e := range d.extra {
			p.Detail = append(p.Detail, point(e))
		}
		p.Triangles = make([]Triangle, len(d.tris))
		for i, t := range d.tris {
			p.Triangles[i] = Triangle{t[0], t[1], t[2]}
		}
		m.Polygons = append(m.Polygons, p)
	}
	return m
}

// point converts v to a Point, writing negative zero as zero so the YAML round trip is exact.
func point(v math3d.Vec3) Point {
	return Point{noNegZero(v.X), noNegZero(v.Y), noNegZero(v.Z)}
}

func noNegZero(f float32) float32 {
	if f == 0 {
		return 0
	}
	return f
}
