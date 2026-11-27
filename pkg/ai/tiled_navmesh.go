package ai

import (
	"math"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultNavTileSize2D   float32 = 64
	minNavTileCoord2D      int32   = math.MinInt32 + 1
	maxNavTileCoord2D      int32   = math.MaxInt32 - 1
	maxNavObstacleTiles2D  int64   = 65536
	maxOffMeshWatchTiles2D int64   = 256
)

func clampTileCoord2D(v float64) int32 {
	if math.IsNaN(v) {
		return 0
	}
	f := math.Floor(v)
	if f <= float64(minNavTileCoord2D) {
		return minNavTileCoord2D
	}
	if f >= float64(maxNavTileCoord2D) {
		return maxNavTileCoord2D
	}
	return int32(f)
}

func neighborCoord2D(c, dir NavTileCoord) (NavTileCoord, bool) {
	nx := int64(c.X) + int64(dir.X)
	ny := int64(c.Y) + int64(dir.Y)
	if nx < math.MinInt32 || nx > math.MaxInt32 || ny < math.MinInt32 || ny > math.MaxInt32 {
		return NavTileCoord{}, false
	}
	return NavTileCoord{X: int32(nx), Y: int32(ny)}, true
}

// NavTileCoord identifies a fixed-size tile in a 2D TiledNavMesh.
type NavTileCoord struct {
	X, Y int32
}

func compareNavTileCoord2D(a, b NavTileCoord) int {
	if a.X != b.X {
		if a.X < b.X {
			return -1
		}
		return 1
	}
	if a.Y < b.Y {
		return -1
	}
	if a.Y > b.Y {
		return 1
	}
	return 0
}

// NavPolyRef uniquely identifies a walkable convex polygon inside a 2D TiledNavMesh.
// Salt increments whenever the owning tile is replaced, rebuilt, or removed so stale
// references are detected in O(1).
type NavPolyRef struct {
	Tile NavTileCoord
	Poly int32
	Salt uint32
}

// Valid reports whether r holds a non-zero tile salt and non-negative polygon index.
func (r NavPolyRef) Valid() bool {
	return r.Salt != 0 && r.Poly >= 0
}

// NavTileVersion records a tile coordinate together with its revision number when a path was computed.
type NavTileVersion struct {
	Coord   NavTileCoord
	Version uint64
}

// NavTileChangeKind classifies why a tile in a TiledNavMesh changed.
type NavTileChangeKind uint8

const (
	// NavTileAdded indicates a tile was inserted into a previously empty coordinate.
	NavTileAdded NavTileChangeKind = iota
	// NavTileUpdated indicates an existing tile was replaced or rebuilt (for example due to a dynamic obstacle).
	NavTileUpdated
	// NavTileRemoved indicates an existing tile was removed from the mesh.
	NavTileRemoved
)

// NavTileChange describes a single tile mutation event delivered to tile listeners and path watchers.
type NavTileChange struct {
	Coord   NavTileCoord
	Kind    NavTileChangeKind
	Version uint64
}

// NavListenerID identifies a tile-change listener registered on a TiledNavMesh.
type NavListenerID uint64

// NavPathWatchID identifies a path-invalidation watcher registered on a TiledNavMesh.
type NavPathWatchID uint64

// NavObstacleID identifies a dynamic obstacle registered on a TiledNavMesh.
type NavObstacleID uint64

// NavObstacleKind selects the geometric shape of a 2D NavObstacle.
type NavObstacleKind uint8

const (
	// NavObstacleBox is an axis-aligned or rotated 2D box (Center, HalfExtents, Angle).
	NavObstacleBox NavObstacleKind = iota
	// NavObstacleCircle is a 2D circle approximated by a circumscribed regular polygon (Center, Radius, Segments).
	NavObstacleCircle
	// NavObstaclePolygon is an arbitrary simple 2D polygon (Vertices, convex or concave).
	NavObstaclePolygon
)

// NavObstacleMode selects how a dynamic obstacle modifies walkable polygons in touched tiles.
type NavObstacleMode uint8

const (
	// NavObstacleCarve subtracts the obstacle footprint from walkable polygons so paths detour around it.
	NavObstacleCarve NavObstacleMode = iota
	// NavObstacleMark splits walkable polygons along the obstacle footprint and applies Area/Flags
	// (defaulting to NavFlagDisabled when both Area and Flags are zero) to the covered polygons.
	NavObstacleMark
)

// NavObstacle describes a temporary runtime obstacle applied to a 2D TiledNavMesh.
type NavObstacle struct {
	Kind        NavObstacleKind
	Mode        NavObstacleMode
	Center      core.Point   // Box and Circle center
	HalfExtents core.Point   // Box half-width and half-height (must be > 0)
	Angle       float32      // Optional Box rotation in radians (CCW)
	Radius      float32      // Circle radius (must be > 0)
	Segments    int          // Circle approximation segments (default 12, clamped to [6, 64])
	Vertices    []core.Point // Polygon vertices (at least 3; convex or concave, any winding)
	Margin      float32      // Optional outward inflation in world units (e.g. agent radius); >= 0
	Area        NavArea      // Applied in NavObstacleMark mode when non-zero (or when Flags is set)
	Flags       NavFlags     // Applied in NavObstacleMark mode (defaults to NavFlagDisabled when both Area and Flags are 0)
}

// NavBoxObstacle constructs an axis-aligned 2D box obstacle spanning [minPt, maxPt].
func NavBoxObstacle(minPt, maxPt core.Point) NavObstacle {
	loX, hiX := min(minPt.X, maxPt.X), max(minPt.X, maxPt.X)
	loY, hiY := min(minPt.Y, maxPt.Y), max(minPt.Y, maxPt.Y)
	return NavObstacle{
		Kind:        NavObstacleBox,
		Mode:        NavObstacleCarve,
		Center:      core.Point{X: (loX + hiX) * 0.5, Y: (loY + hiY) * 0.5},
		HalfExtents: core.Point{X: (hiX - loX) * 0.5, Y: (hiY - loY) * 0.5},
	}
}

// NavOrientedBoxObstacle constructs a rotated 2D box obstacle centered at center with halfExtents and rotation angleRad.
func NavOrientedBoxObstacle(center, halfExtents core.Point, angleRad float32) NavObstacle {
	return NavObstacle{
		Kind:        NavObstacleBox,
		Mode:        NavObstacleCarve,
		Center:      center,
		HalfExtents: halfExtents,
		Angle:       angleRad,
	}
}

// NavCircleObstacle constructs a 2D circular obstacle centered at center with radius.
func NavCircleObstacle(center core.Point, radius float32) NavObstacle {
	return NavObstacle{
		Kind:   NavObstacleCircle,
		Mode:   NavObstacleCarve,
		Center: center,
		Radius: radius,
	}
}

// NavPolygonObstacle constructs a 2D polygonal obstacle (convex or concave) from verts.
func NavPolygonObstacle(verts []core.Point) NavObstacle {
	cp := make([]core.Point, len(verts))
	copy(cp, verts)
	return NavObstacle{
		Kind:     NavObstaclePolygon,
		Mode:     NavObstacleCarve,
		Vertices: cp,
	}
}

type basePoly2D struct {
	verts []vec2
	area  NavArea
	flags NavFlags
}

// NavTile is an independently baked 2D navigation tile covering a fixed square region
// [minX, maxX] x [minY, maxY]. A NavTile may be baked off the main thread with BakeNavTile
// and installed into a TiledNavMesh with AddTile, ReplaceTile, or SetTile.
type NavTile struct {
	coord    NavTileCoord
	tileSize float64
	minX     float64
	minY     float64
	maxX     float64
	maxY     float64
	base     []basePoly2D
}

// Coord returns the tile coordinate this tile was baked for.
func (t *NavTile) Coord() NavTileCoord {
	if t == nil {
		return NavTileCoord{}
	}
	return t.coord
}

// BasePolygonCount returns the number of base walkable polygons baked into this tile.
func (t *NavTile) BasePolygonCount() int {
	if t == nil {
		return 0
	}
	return len(t.base)
}

// Bounds returns the world-space [min, max] rectangle of this tile.
func (t *NavTile) Bounds() (minPt, maxPt core.Point) {
	if t == nil {
		return core.Point{}, core.Point{}
	}
	return core.Point{X: core.Points(t.minX), Y: core.Points(t.minY)},
		core.Point{X: core.Points(t.maxX), Y: core.Points(t.maxY)}
}

// BakeNavTile bakes an independent 2D NavTile for coord from convex walkable polygons polys,
// clipping every polygon to the tile's exact bounds [origin + coord*tileSize, origin + (coord+1)*tileSize].
// Degenerate polygons or polygons outside the tile bounds are omitted.
// BakeNavTile is pure and safe to call concurrently from worker goroutines.
func BakeNavTile(coord NavTileCoord, tileSize float32, origin core.Point, polys []NavPolygonInput) *NavTile {
	ts := float64(tileSize)
	if !isFinite32(tileSize) || ts <= 0 {
		ts = float64(defaultNavTileSize2D)
	}
	ox, oy := float64(origin.X), float64(origin.Y)
	if math.IsNaN(ox) || math.IsInf(ox, 0) {
		ox = 0
	}
	if math.IsNaN(oy) || math.IsInf(oy, 0) {
		oy = 0
	}
	minX := ox + float64(float64(coord.X)*ts)
	maxX := ox + float64((float64(coord.X)+1)*ts)
	minY := oy + float64(float64(coord.Y)*ts)
	maxY := oy + float64((float64(coord.Y)+1)*ts)

	tile := &NavTile{
		coord:    coord,
		tileSize: ts,
		minX:     minX,
		minY:     minY,
		maxX:     maxX,
		maxY:     maxY,
	}

	for _, in := range polys {
		if len(in.Vertices) < 3 {
			continue
		}
		raw := make([]vec2, len(in.Vertices))
		valid := true
		pMinX, pMinY := math.Inf(1), math.Inf(1)
		pMaxX, pMaxY := math.Inf(-1), math.Inf(-1)
		for i, pt := range in.Vertices {
			v := toVec2(pt)
			if !isFiniteVec2(v) {
				valid = false
				break
			}
			raw[i] = v
			pMinX = min(pMinX, v.x)
			pMinY = min(pMinY, v.y)
			pMaxX = max(pMaxX, v.x)
			pMaxY = max(pMaxY, v.y)
		}
		if !valid || pMaxX <= minX+tiledSplitEps || pMinX >= maxX-tiledSplitEps ||
			pMaxY <= minY+tiledSplitEps || pMinY >= maxY-tiledSplitEps {
			continue
		}
		cleaned := cleanPoly2D(raw)
		if len(cleaned) < 3 {
			continue
		}
		clipped := clipPolyToBox2D(cleaned, minX, minY, maxX, maxY)
		if len(clipped) < 3 {
			continue
		}
		flags := in.Flags
		if flags == 0 {
			flags = NavFlagWalk
		}
		tile.base = append(tile.base, basePoly2D{
			verts: clipped,
			area:  in.Area,
			flags: flags,
		})
	}
	return tile
}

// TiledNavMeshConfig configures a 2D TiledNavMesh.
type TiledNavMeshConfig struct {
	// TileSize is the width and height of each square tile in world units.
	// Defaults to 64 when <= 0.
	TileSize float32
	// Origin is the world-space origin of tile (0, 0). Defaults to (0, 0).
	Origin core.Point
}

// TiledNavPathLink marks a segment in TiledNavPathResult.Path that traverses an off-mesh link.
// Path[StartIndex] is the link's entry point on the mesh and Path[EndIndex] is its exit point
// (with EndIndex == StartIndex + 1).
type TiledNavPathLink struct {
	// StartIndex is the index in TiledNavPathResult.Path where the off-mesh link begins.
	StartIndex int
	// EndIndex is the index in TiledNavPathResult.Path where the off-mesh link ends (StartIndex + 1).
	EndIndex int
	// FromPoly and ToPoly are the source and destination polygon references connected by the link.
	FromPoly NavPolyRef
	ToPoly   NavPolyRef
	// Start and End are the world-space entry and exit positions of the traversed link direction.
	Start core.Point
	End   core.Point
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

// TiledNavPathResult is the outcome of a 2D TiledNavMesh path search, including
// the tile footprint and tile versions crossed so callers can detect invalidation.
type TiledNavPathResult struct {
	Path     []core.Point
	Corridor []NavPolyRef
	Tiles    []NavTileVersion
	// Links lists each segment of Path that traverses an off-mesh link, in path order.
	// For each link, Path[link.StartIndex] is the entry point and Path[link.EndIndex]
	// (with EndIndex == StartIndex + 1) is the exit point.
	Links      []TiledNavPathLink
	Found      bool
	Partial    bool
	Exhausted  bool
	Expansions int
}

// HasLinks reports whether r traverses at least one off-mesh link.
func (r TiledNavPathResult) HasLinks() bool {
	return len(r.Links) > 0
}

// LinkAt returns the off-mesh link traversed by path segment segmentIndex (from Path[segmentIndex]
// to Path[segmentIndex+1]), or (zero, false) when that segment is ordinary walking movement.
func (r TiledNavPathResult) LinkAt(segmentIndex int) (TiledNavPathLink, bool) {
	for _, l := range r.Links {
		if l.StartIndex == segmentIndex {
			return l, true
		}
		if l.StartIndex > segmentIndex {
			break
		}
	}
	return TiledNavPathLink{}, false
}

// CrossesTile reports whether r's corridor passes through coord.
func (r TiledNavPathResult) CrossesTile(coord NavTileCoord) bool {
	for _, tv := range r.Tiles {
		if tv.Coord == coord {
			return true
		}
	}
	return false
}

// TileCoords returns the deduplicated list of tile coordinates crossed by r.
func (r TiledNavPathResult) TileCoords() []NavTileCoord {
	if len(r.Tiles) == 0 {
		return nil
	}
	out := make([]NavTileCoord, len(r.Tiles))
	for i, tv := range r.Tiles {
		out[i] = tv.Coord
	}
	return out
}

// TiledNavRaycastHit describes the result of casting a segment across a 2D TiledNavMesh.
type TiledNavRaycastHit struct {
	Point  core.Point
	Normal core.Point
	Poly   NavPolyRef
	T      float32
	Hit    bool
}

// Cardinal neighbor directions for 2D tiles: 0=-X, 1=+X, 2=-Y, 3=+Y.
var tileDir2D = [4]NavTileCoord{
	{X: -1, Y: 0},
	{X: 1, Y: 0},
	{X: 0, Y: -1},
	{X: 0, Y: 1},
}

var tileOppDir2D = [4]int{1, 0, 3, 2}

type tiledPortal2D struct {
	fromPoly   int32
	toSlot     int32
	toPoly     int32
	left       vec2
	right      vec2
	endPos     vec2
	cost       float64
	flags      uint32
	offMeshIdx int32 // 1-based index into tm.offMeshLinks (0 for shared-edge portals)
	linkID     int
	cross      bool
	offMesh    bool
}

type tiledOffMeshEntry2D struct {
	idx         int32
	rawStart    core.Point
	rawEnd      core.Point
	link        NavOffMeshLink
	filterFlags NavFlags
	watchTiles  []NavTileCoord
	globalWatch bool

	attached  bool
	fromCoord NavTileCoord
	fromSlot  int32
	fromPoly  int32
	toCoord   NavTileCoord
	toSlot    int32
	toPoly    int32
	sVec      vec2
	eVec      vec2
	cost      float64
}

type navTileSlot2D struct {
	slotID    int32
	salt      uint32
	coord     NavTileCoord
	minX      float64
	minY      float64
	maxX      float64
	maxY      float64
	base      []basePoly2D
	polys     []navPoly
	portals   []tiledPortal2D
	bvhNodes  []navBVHNode2D
	bvhOrder  []int
	border    [4][]int32 // polygon indices touching -X, +X, -Y, +Y borders
	polyMinX  float64
	polyMinY  float64
	polyMaxX  float64
	polyMaxY  float64
	hasBounds bool
}

type obstacleEntry2D struct {
	id         NavObstacleID
	obs        NavObstacle
	cutters    [][]vec2
	minX, minY float64
	maxX, maxY float64
	tiles      []NavTileCoord
}

type tileListener2D struct {
	id NavListenerID
	fn func(NavTileChange)
}

type pathWatch2D struct {
	id    NavPathWatchID
	tiles []NavTileCoord
	fn    func(NavTileChange)
}

// TiledNavMesh is a 2D navigation mesh partitioned into fixed-size square tiles
// that can be baked, added, removed, and replaced independently at runtime.
// Polygons are automatically linked across tile borders, dynamic obstacles rebuild
// only the touched tiles, a two-level spatial index avoids whole-mesh scans, and
// per-tile versions + watchers invalidate paths crossing changed tiles.
//
// TiledNavMesh is deterministic and is not safe for concurrent mutation.
type TiledNavMesh struct {
	tileSize float64
	originX  float64
	originY  float64

	tiles     map[NavTileCoord]*navTileSlot2D
	slots     []*navTileSlot2D
	freeSlots []int32
	nextSalt  uint32

	tileVersions map[NavTileCoord]uint64
	revision     uint64
	polyCount    int

	// Coarse tile BVH for fast unbounded NearestPolygon / large-extent queries.
	tileBVHMu    sync.Mutex
	tileBVHDirty atomic.Bool
	tileBVHNodes []navBVHNode2D
	tileBVHOrder []*navTileSlot2D

	// Dynamic obstacles indexed by ID and by touched tile coordinate.
	nextObstacleID NavObstacleID
	obstacles      map[NavObstacleID]*obstacleEntry2D
	tileObstacles  map[NavTileCoord][]NavObstacleID

	// Off-mesh links indexed by insertion order and by candidate endpoint tile coordinates.
	offMeshLinks       []*tiledOffMeshEntry2D
	tileOffMeshLinks   map[NavTileCoord][]int32
	globalOffMeshLinks []int32

	// Tile change listeners & path-invalidation watchers.
	nextListenerID NavListenerID
	listeners      []tileListener2D
	nextWatchID    NavPathWatchID
	watches        map[NavPathWatchID]*pathWatch2D
	tileWatches    map[NavTileCoord][]NavPathWatchID

	// Optional batch update state so multiple edits rebuild each touched tile at most once.
	batchDepth int
	dirtyTiles map[NavTileCoord]NavTileChangeKind
}

// NewTiledNavMesh creates an empty 2D TiledNavMesh with cfg.
func NewTiledNavMesh(cfg TiledNavMeshConfig) *TiledNavMesh {
	ts := float64(cfg.TileSize)
	if !isFinite32(cfg.TileSize) || ts <= 0 {
		ts = float64(defaultNavTileSize2D)
	}
	ox, oy := float64(cfg.Origin.X), float64(cfg.Origin.Y)
	if math.IsNaN(ox) || math.IsInf(ox, 0) {
		ox = 0
	}
	if math.IsNaN(oy) || math.IsInf(oy, 0) {
		oy = 0
	}
	return &TiledNavMesh{
		tileSize:         ts,
		originX:          ox,
		originY:          oy,
		tiles:            make(map[NavTileCoord]*navTileSlot2D),
		tileVersions:     make(map[NavTileCoord]uint64),
		obstacles:        make(map[NavObstacleID]*obstacleEntry2D),
		tileObstacles:    make(map[NavTileCoord][]NavObstacleID),
		tileOffMeshLinks: make(map[NavTileCoord][]int32),
		watches:          make(map[NavPathWatchID]*pathWatch2D),
		tileWatches:      make(map[NavTileCoord][]NavPathWatchID),
		revision:         1,
	}
}

// BuildTiledNavMesh constructs a 2D TiledNavMesh from convex polygons using fixed square tiles of size tileSize.
func BuildTiledNavMesh(polys [][]core.Point, tileSize float32) *TiledNavMesh {
	inputs := make([]NavPolygonInput, len(polys))
	for i, raw := range polys {
		inputs[i] = NavPolygonInput{
			Vertices: raw,
			Area:     NavAreaDefault,
			Flags:    NavFlagWalk,
		}
	}
	return BuildTiledNavMeshWithAreas(inputs, tileSize)
}

// BuildTiledNavMeshWithAreas constructs a 2D TiledNavMesh from convex polygons carrying per-polygon areas and flags.
func BuildTiledNavMeshWithAreas(polys []NavPolygonInput, tileSize float32) *TiledNavMesh {
	tm := NewTiledNavMesh(TiledNavMeshConfig{TileSize: tileSize})
	if len(polys) == 0 {
		return tm
	}
	// Group polygons by overlapped tile coordinates so each tile is baked cleanly once.
	perTile := make(map[NavTileCoord][]NavPolygonInput)
	var coords []NavTileCoord
	for _, in := range polys {
		if len(in.Vertices) < 3 {
			continue
		}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		valid := true
		for _, pt := range in.Vertices {
			v := toVec2(pt)
			if !isFiniteVec2(v) {
				valid = false
				break
			}
			minX = min(minX, v.x)
			minY = min(minY, v.y)
			maxX = max(maxX, v.x)
			maxY = max(maxY, v.y)
		}
		if !valid || maxX <= minX+tiledSplitEps || maxY <= minY+tiledSplitEps {
			continue
		}
		tx0, ty0, tx1, ty1 := tm.tileRangeForAABB(minX, minY, maxX, maxY)
		for tx := int64(tx0); tx <= int64(tx1); tx++ {
			for ty := int64(ty0); ty <= int64(ty1); ty++ {
				c := NavTileCoord{X: int32(tx), Y: int32(ty)}
				if _, exists := perTile[c]; !exists {
					coords = append(coords, c)
				}
				perTile[c] = append(perTile[c], in)
			}
		}
	}
	slices.SortFunc(coords, compareNavTileCoord2D)
	origin := core.Point{X: core.Points(tm.originX), Y: core.Points(tm.originY)}
	for _, c := range coords {
		tile := BakeNavTile(c, float32(tm.tileSize), origin, perTile[c])
		if len(tile.base) > 0 {
			tm.SetTile(c, tile)
		}
	}
	return tm
}

// TileSize returns the configured tile size in world units.
func (tm *TiledNavMesh) TileSize() float32 {
	if tm == nil {
		return 0
	}
	return float32(tm.tileSize)
}

// Origin returns the world-space origin of tile (0, 0).
func (tm *TiledNavMesh) Origin() core.Point {
	if tm == nil {
		return core.Point{}
	}
	return core.Point{X: core.Points(tm.originX), Y: core.Points(tm.originY)}
}

// TileCoordAt returns the tile coordinate containing p.
func (tm *TiledNavMesh) TileCoordAt(p core.Point) NavTileCoord {
	if tm == nil || tm.tileSize <= 0 {
		return NavTileCoord{}
	}
	return NavTileCoord{
		X: clampTileCoord2D((float64(p.X) - tm.originX) / tm.tileSize),
		Y: clampTileCoord2D((float64(p.Y) - tm.originY) / tm.tileSize),
	}
}

// TileBounds returns the exact world-space [min, max] bounds of tile coord.
func (tm *TiledNavMesh) TileBounds(coord NavTileCoord) (minPt, maxPt core.Point) {
	if tm == nil {
		return core.Point{}, core.Point{}
	}
	minX, minY, maxX, maxY := tm.tileBounds64(coord)
	return core.Point{X: core.Points(minX), Y: core.Points(minY)},
		core.Point{X: core.Points(maxX), Y: core.Points(maxY)}
}

func (tm *TiledNavMesh) tileBounds64(coord NavTileCoord) (minX, minY, maxX, maxY float64) {
	minX = tm.originX + float64(float64(coord.X)*tm.tileSize)
	maxX = tm.originX + float64((float64(coord.X)+1)*tm.tileSize)
	minY = tm.originY + float64(float64(coord.Y)*tm.tileSize)
	maxY = tm.originY + float64((float64(coord.Y)+1)*tm.tileSize)
	return
}

func (tm *TiledNavMesh) tileRangeForAABB(minX, minY, maxX, maxY float64) (tx0, ty0, tx1, ty1 int32) {
	tx0 = clampTileCoord2D((minX - tm.originX) / tm.tileSize)
	ty0 = clampTileCoord2D((minY - tm.originY) / tm.tileSize)
	adjMaxX := maxX
	if maxX > minX+tiledSplitEps {
		adjMaxX = maxX - tiledSplitEps
	}
	adjMaxY := maxY
	if maxY > minY+tiledSplitEps {
		adjMaxY = maxY - tiledSplitEps
	}
	tx1 = clampTileCoord2D((adjMaxX - tm.originX) / tm.tileSize)
	ty1 = clampTileCoord2D((adjMaxY - tm.originY) / tm.tileSize)
	if tx1 < tx0 {
		tx1 = tx0
	}
	if ty1 < ty0 {
		ty1 = ty0
	}
	return
}

// Revision returns a monotonically increasing number bumped on any tile or obstacle mutation.
func (tm *TiledNavMesh) Revision() uint64 {
	if tm == nil {
		return 0
	}
	return tm.revision
}

// TileVersion returns the current revision number of tile coord (0 if never modified).
func (tm *TiledNavMesh) TileVersion(coord NavTileCoord) uint64 {
	if tm == nil {
		return 0
	}
	return tm.tileVersions[coord]
}

// TileCount returns the number of currently installed tiles.
func (tm *TiledNavMesh) TileCount() int {
	if tm == nil {
		return 0
	}
	return len(tm.tiles)
}

// HasTile reports whether a tile is currently installed at coord.
func (tm *TiledNavMesh) HasTile(coord NavTileCoord) bool {
	if tm == nil {
		return false
	}
	_, ok := tm.tiles[coord]
	return ok
}

// TileCoords returns all installed tile coordinates in deterministic sorted order.
func (tm *TiledNavMesh) TileCoords() []NavTileCoord {
	if tm == nil || len(tm.tiles) == 0 {
		return nil
	}
	out := make([]NavTileCoord, 0, len(tm.tiles))
	for c := range tm.tiles {
		out = append(out, c)
	}
	slices.SortFunc(out, compareNavTileCoord2D)
	return out
}

// PolygonCount returns the total number of active walkable polygons across all installed tiles.
func (tm *TiledNavMesh) PolygonCount() int {
	if tm == nil {
		return 0
	}
	return tm.polyCount
}

// TilePolygonCount returns the number of active polygons currently inside tile coord.
func (tm *TiledNavMesh) TilePolygonCount(coord NavTileCoord) int {
	if tm == nil {
		return 0
	}
	slot := tm.tiles[coord]
	if slot == nil {
		return 0
	}
	return len(slot.polys)
}

// BakeTile bakes polys into a NavTile for coord using tm's TileSize and Origin and installs it into tm.
func (tm *TiledNavMesh) BakeTile(coord NavTileCoord, polys []NavPolygonInput) *NavTile {
	if tm == nil {
		return nil
	}
	tile := BakeNavTile(coord, float32(tm.tileSize), tm.Origin(), polys)
	tm.SetTile(coord, tile)
	return tile
}

// AddTile installs tile at coord only if no tile currently exists at coord.
// Returns false if tm or tile is nil or coord is already occupied.
func (tm *TiledNavMesh) AddTile(coord NavTileCoord, tile *NavTile) bool {
	if tm == nil || tile == nil {
		return false
	}
	if _, exists := tm.tiles[coord]; exists {
		return false
	}
	tm.installTile(coord, tile, NavTileAdded)
	return true
}

// ReplaceTile replaces the existing tile at coord with tile.
// Returns false if tm or tile is nil or no tile currently exists at coord.
func (tm *TiledNavMesh) ReplaceTile(coord NavTileCoord, tile *NavTile) bool {
	if tm == nil || tile == nil {
		return false
	}
	if _, exists := tm.tiles[coord]; !exists {
		return false
	}
	tm.installTile(coord, tile, NavTileUpdated)
	return true
}

// SetTile installs or replaces tile at coord, applying any active dynamic obstacles touching coord
// and linking polygons across borders with neighboring tiles.
func (tm *TiledNavMesh) SetTile(coord NavTileCoord, tile *NavTile) bool {
	if tm == nil || tile == nil {
		return false
	}
	kind := NavTileAdded
	if _, exists := tm.tiles[coord]; exists {
		kind = NavTileUpdated
	}
	tm.installTile(coord, tile, kind)
	return true
}

// RemoveTile unloads the tile at coord, removes cross-tile border links from neighboring tiles,
// bumps coord's tile version, and notifies listeners / invalidates paths crossing coord.
func (tm *TiledNavMesh) RemoveTile(coord NavTileCoord) bool {
	if tm == nil {
		return false
	}
	slot, exists := tm.tiles[coord]
	if !exists {
		return false
	}
	affectedLinks := tm.detachOffMeshLinksForCoords([]NavTileCoord{coord})
	tm.unlinkNeighborsOf(slot)
	tm.polyCount -= len(slot.polys)
	delete(tm.tiles, coord)
	tm.slots[slot.slotID] = nil
	tm.freeSlots = append(tm.freeSlots, slot.slotID)
	tm.tileBVHDirty.Store(true)
	tm.attachOffMeshLinks(affectedLinks)

	tm.revision++
	tm.tileVersions[coord]++
	ver := tm.tileVersions[coord]
	if tm.batchDepth > 0 {
		tm.markDirtyBatch(coord, NavTileRemoved)
		return true
	}
	tm.notifyChanges([]NavTileChange{{Coord: coord, Kind: NavTileRemoved, Version: ver}})
	return true
}

// RebuildTile rebuilds the existing tile at coord from its base polygons and active dynamic obstacles.
// Returns false if no tile exists at coord.
func (tm *TiledNavMesh) RebuildTile(coord NavTileCoord) bool {
	if tm == nil {
		return false
	}
	slot, exists := tm.tiles[coord]
	if !exists {
		return false
	}
	affectedLinks := tm.detachOffMeshLinksForCoords([]NavTileCoord{coord})
	tm.rebuildSlot(slot)
	tm.attachOffMeshLinks(affectedLinks)
	tm.revision++
	tm.tileVersions[coord]++
	ver := tm.tileVersions[coord]
	if tm.batchDepth > 0 {
		tm.markDirtyBatch(coord, NavTileUpdated)
		return true
	}
	tm.notifyChanges([]NavTileChange{{Coord: coord, Kind: NavTileUpdated, Version: ver}})
	return true
}

func (tm *TiledNavMesh) allocSalt() uint32 {
	tm.nextSalt++
	if tm.nextSalt == 0 {
		tm.nextSalt = 1
	}
	return tm.nextSalt
}

func (tm *TiledNavMesh) installTile(coord NavTileCoord, tile *NavTile, kind NavTileChangeKind) {
	minX, minY, maxX, maxY := tm.tileBounds64(coord)
	baseCopy := make([]basePoly2D, 0, len(tile.base))
	sameBounds := math.Abs(tile.minX-minX) <= tiledSplitEps &&
		math.Abs(tile.minY-minY) <= tiledSplitEps &&
		math.Abs(tile.maxX-maxX) <= tiledSplitEps &&
		math.Abs(tile.maxY-maxY) <= tiledSplitEps
	for _, bp := range tile.base {
		verts := bp.verts
		if !sameBounds {
			verts = clipPolyToBox2D(verts, minX, minY, maxX, maxY)
			if len(verts) < 3 {
				continue
			}
		} else {
			cp := make([]vec2, len(verts))
			copy(cp, verts)
			verts = cp
		}
		baseCopy = append(baseCopy, basePoly2D{
			verts: verts,
			area:  bp.area,
			flags: bp.flags,
		})
	}

	slot, exists := tm.tiles[coord]
	if !exists {
		var slotID int32
		if n := len(tm.freeSlots); n > 0 {
			slotID = tm.freeSlots[n-1]
			tm.freeSlots = tm.freeSlots[:n-1]
		} else {
			slotID = int32(len(tm.slots))
			tm.slots = append(tm.slots, nil)
		}
		slot = &navTileSlot2D{
			slotID: slotID,
			coord:  coord,
			minX:   minX,
			minY:   minY,
			maxX:   maxX,
			maxY:   maxY,
		}
		tm.slots[slotID] = slot
		tm.tiles[coord] = slot
		tm.tileBVHDirty.Store(true)
	}
	slot.base = baseCopy

	if tm.batchDepth > 0 {
		tm.markDirtyBatch(coord, kind)
		return
	}
	affectedLinks := tm.detachOffMeshLinksForCoords([]NavTileCoord{coord})
	tm.rebuildSlot(slot)
	tm.attachOffMeshLinks(affectedLinks)
	tm.revision++
	tm.tileVersions[coord]++
	tm.notifyChanges([]NavTileChange{{Coord: coord, Kind: kind, Version: tm.tileVersions[coord]}})
}

// rebuildSlot rebuilds only slot from slot.base + obstacles touching slot.coord,
// updates slot's local BVH and intra-tile portals, and re-links borders with up to 4 cardinal neighbors.
func (tm *TiledNavMesh) rebuildSlot(slot *navTileSlot2D) {
	tm.unlinkNeighborsOf(slot)
	tm.polyCount -= len(slot.polys)
	slot.salt = tm.allocSalt()

	// 1. Start from base polygons and apply all dynamic obstacles touching this tile in deterministic ID order.
	working := make([]basePoly2D, len(slot.base))
	copy(working, slot.base)

	obsIDs := tm.tileObstacles[slot.coord]
	if len(obsIDs) > 0 && len(working) > 0 {
		var next []basePoly2D
		for _, id := range obsIDs {
			entry := tm.obstacles[id]
			if entry == nil {
				continue
			}
			markFlags := entry.obs.Flags
			markArea := entry.obs.Area
			setArea := entry.obs.Area != 0 || entry.obs.Flags != 0
			if entry.obs.Mode == NavObstacleMark && entry.obs.Flags == 0 && entry.obs.Area == 0 {
				markFlags = NavFlagDisabled
			}
			for _, cutter := range entry.cutters {
				next = next[:0]
				for _, p := range working {
					// Fast AABB reject against the obstacle bounds.
					pMinX, pMinY, pMaxX, pMaxY := polyBounds2D(p.verts)
					if pMaxX <= entry.minX+tiledSplitEps || pMinX >= entry.maxX-tiledSplitEps ||
						pMaxY <= entry.minY+tiledSplitEps || pMinY >= entry.maxY-tiledSplitEps {
						next = append(next, p)
						continue
					}
					outside, inside := carveConvexPoly2D(p.verts, cutter)
					for _, ov := range outside {
						next = append(next, basePoly2D{verts: ov, area: p.area, flags: p.flags})
					}
					if entry.obs.Mode == NavObstacleMark && len(inside) >= 3 {
						area := p.area
						if setArea && entry.obs.Area != 0 {
							area = markArea
						}
						flags := p.flags
						if markFlags != 0 {
							flags = markFlags
						}
						next = append(next, basePoly2D{verts: inside, area: area, flags: flags})
					}
				}
				working, next = next, working
				if len(working) == 0 {
					break
				}
			}
			if len(working) == 0 {
				break
			}
		}
		// Lightly merge co-area convex pieces generated by carving to keep tile polygon counts compact.
		if len(working) > 1 {
			working = mergeTilePolys2D(working)
		}
	}

	// 2. Build navPoly slice and tile polygon bounds.
	slot.polys = make([]navPoly, len(working))
	slot.hasBounds = len(working) > 0
	if slot.hasBounds {
		slot.polyMinX, slot.polyMinY = math.Inf(1), math.Inf(1)
		slot.polyMaxX, slot.polyMaxY = math.Inf(-1), math.Inf(-1)
	}
	for i, wp := range working {
		np := makeNavPoly2D(wp.verts, wp.area, wp.flags)
		slot.polys[i] = np
		if np.minX < slot.polyMinX {
			slot.polyMinX = np.minX
		}
		if np.minY < slot.polyMinY {
			slot.polyMinY = np.minY
		}
		if np.maxX > slot.polyMaxX {
			slot.polyMaxX = np.maxX
		}
		if np.maxY > slot.polyMaxY {
			slot.polyMaxY = np.maxY
		}
	}
	tm.polyCount += len(slot.polys)

	// 3. Build local per-tile BVH.
	slot.bvhNodes, slot.bvhOrder = buildPolyBVH2D(slot.polys)

	// 4. Build intra-tile portals using local BVH in deterministic (i, j) order.
	slot.portals = slot.portals[:0]
	var candidates []int
	for i := range slot.polys {
		pi := &slot.polys[i]
		candidates = queryPolyBVH2DAABB(slot.polys, slot.bvhNodes, slot.bvhOrder,
			pi.minX-portalEps, pi.minY-portalEps, pi.maxX+portalEps, pi.maxY+portalEps, candidates[:0])
		slices.Sort(candidates)
		for _, j := range candidates {
			if j <= i {
				continue
			}
			pj := &slot.polys[j]
			if pi.maxX+portalEps < pj.minX || pj.maxX+portalEps < pi.minX ||
				pi.maxY+portalEps < pj.minY || pj.maxY+portalEps < pi.minY {
				continue
			}
			l, r, ok := sharedEdge(pi.verts, pj.verts)
			if !ok {
				continue
			}
			pidIJ := len(slot.portals)
			slot.portals = append(slot.portals, tiledPortal2D{
				fromPoly: int32(i),
				toSlot:   slot.slotID,
				toPoly:   int32(j),
				left:     l,
				right:    r,
			})
			pi.outPortals = append(pi.outPortals, pidIJ)

			pidJI := len(slot.portals)
			slot.portals = append(slot.portals, tiledPortal2D{
				fromPoly: int32(j),
				toSlot:   slot.slotID,
				toPoly:   int32(i),
				left:     r,
				right:    l,
			})
			pj.outPortals = append(pj.outPortals, pidJI)
		}
	}

	// 5. Classify polygons touching each of the 4 tile borders.
	for d := range 4 {
		slot.border[d] = slot.border[d][:0]
	}
	for i := range slot.polys {
		p := &slot.polys[i]
		if p.minX <= slot.minX+portalEps {
			slot.border[0] = append(slot.border[0], int32(i))
		}
		if p.maxX >= slot.maxX-portalEps {
			slot.border[1] = append(slot.border[1], int32(i))
		}
		if p.minY <= slot.minY+portalEps {
			slot.border[2] = append(slot.border[2], int32(i))
		}
		if p.maxY >= slot.maxY-portalEps {
			slot.border[3] = append(slot.border[3], int32(i))
		}
	}

	// 6. Link across borders with existing cardinal neighbor tiles.
	tm.linkNeighborsOf(slot)
	tm.tileBVHDirty.Store(true)
}

func polyBounds2D(verts []vec2) (minX, minY, maxX, maxY float64) {
	minX, minY = verts[0].x, verts[0].y
	maxX, maxY = verts[0].x, verts[0].y
	for _, v := range verts[1:] {
		minX = min(minX, v.x)
		minY = min(minY, v.y)
		maxX = max(maxX, v.x)
		maxY = max(maxY, v.y)
	}
	return
}

func mergeTilePolys2D(polys []basePoly2D) []basePoly2D {
	if len(polys) <= 1 || len(polys) > 256 {
		return polys
	}
	merged := true
	for merged {
		merged = false
		for i := 0; i < len(polys); i++ {
			for j := i + 1; j < len(polys); j++ {
				if polys[i].area != polys[j].area || polys[i].flags != polys[j].flags {
					continue
				}
				if mv, ok := tryMergeConvex2D(polys[i].verts, polys[j].verts); ok {
					polys[i].verts = mv
					polys = append(polys[:j], polys[j+1:]...)
					merged = true
					break
				}
			}
			if merged {
				break
			}
		}
	}
	return polys
}

// unlinkNeighborsOf removes all cross-tile portals pointing into slot from its 4 cardinal neighbors.
func (tm *TiledNavMesh) unlinkNeighborsOf(slot *navTileSlot2D) {
	for _, dir := range tileDir2D {
		nc, ok := neighborCoord2D(slot.coord, dir)
		if !ok {
			continue
		}
		nb := tm.tiles[nc]
		if nb == nil {
			continue
		}
		nb.removePortalsTo(slot.slotID)
	}
}

func (s *navTileSlot2D) removePortalsTo(targetSlot int32) {
	hasAny := false
	for _, p := range s.portals {
		if p.cross && !p.offMesh && p.toSlot == targetSlot {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return
	}
	for i := range s.polys {
		s.polys[i].outPortals = s.polys[i].outPortals[:0]
	}
	kept := s.portals[:0]
	for _, p := range s.portals {
		if p.cross && !p.offMesh && p.toSlot == targetSlot {
			continue
		}
		pid := len(kept)
		kept = append(kept, p)
		s.polys[p.fromPoly].outPortals = append(s.polys[p.fromPoly].outPortals, pid)
	}
	s.portals = kept
}

func (s *navTileSlot2D) removeOffMeshPortal(offMeshIdx int32) {
	hasAny := false
	for _, p := range s.portals {
		if p.offMesh && p.offMeshIdx == offMeshIdx {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return
	}
	for i := range s.polys {
		s.polys[i].outPortals = s.polys[i].outPortals[:0]
	}
	kept := s.portals[:0]
	for _, p := range s.portals {
		if p.offMesh && p.offMeshIdx == offMeshIdx {
			continue
		}
		pid := len(kept)
		kept = append(kept, p)
		s.polys[p.fromPoly].outPortals = append(s.polys[p.fromPoly].outPortals, pid)
	}
	s.portals = kept
}

func (tm *TiledNavMesh) linkNeighborsOf(slot *navTileSlot2D) {
	for d, dir := range tileDir2D {
		nc, ok := neighborCoord2D(slot.coord, dir)
		if !ok {
			continue
		}
		nb := tm.tiles[nc]
		if nb == nil {
			continue
		}
		myBorder := slot.border[d]
		nbBorder := nb.border[tileOppDir2D[d]]
		if len(myBorder) == 0 || len(nbBorder) == 0 {
			continue
		}
		for _, i := range myBorder {
			pi := &slot.polys[i]
			for _, j := range nbBorder {
				pj := &nb.polys[j]
				if pi.maxX+portalEps < pj.minX || pj.maxX+portalEps < pi.minX ||
					pi.maxY+portalEps < pj.minY || pj.maxY+portalEps < pi.minY {
					continue
				}
				l, r, ok := sharedEdge(pi.verts, pj.verts)
				if !ok {
					continue
				}
				pidAB := len(slot.portals)
				slot.portals = append(slot.portals, tiledPortal2D{
					fromPoly: i,
					toSlot:   nb.slotID,
					toPoly:   j,
					left:     l,
					right:    r,
					cross:    true,
				})
				pi.outPortals = append(pi.outPortals, pidAB)

				pidBA := len(nb.portals)
				nb.portals = append(nb.portals, tiledPortal2D{
					fromPoly: j,
					toSlot:   slot.slotID,
					toPoly:   i,
					left:     r,
					right:    l,
					cross:    true,
				})
				pj.outPortals = append(pj.outPortals, pidBA)
			}
		}
	}
}

// Batch groups multiple tile or obstacle mutations so each touched tile is rebuilt at most once
// in deterministic sorted order when fn returns, and notifications fire once per changed tile.
func (tm *TiledNavMesh) Batch(fn func()) {
	if tm == nil || fn == nil {
		return
	}
	tm.batchDepth++
	defer func() {
		tm.batchDepth--
		if tm.batchDepth == 0 {
			tm.flushBatch()
		}
	}()
	fn()
}

func (tm *TiledNavMesh) markDirtyBatch(coord NavTileCoord, kind NavTileChangeKind) {
	if tm.dirtyTiles == nil {
		tm.dirtyTiles = make(map[NavTileCoord]NavTileChangeKind)
	}
	prev, exists := tm.dirtyTiles[coord]
	if !exists {
		tm.dirtyTiles[coord] = kind
		return
	}
	// Preserve Added if tile was added and then touched by an obstacle in the same batch;
	// Removed overrides Added/Updated, while a subsequent add/update overrides Removed.
	if kind == NavTileRemoved {
		tm.dirtyTiles[coord] = NavTileRemoved
	} else if prev == NavTileRemoved {
		tm.dirtyTiles[coord] = NavTileUpdated
	}
}

func (tm *TiledNavMesh) flushBatch() {
	if len(tm.dirtyTiles) == 0 {
		return
	}
	coords := make([]NavTileCoord, 0, len(tm.dirtyTiles))
	for c := range tm.dirtyTiles {
		coords = append(coords, c)
	}
	slices.SortFunc(coords, compareNavTileCoord2D)

	affectedLinks := tm.detachOffMeshLinksForCoords(coords)
	changes := make([]NavTileChange, 0, len(coords))
	for _, c := range coords {
		kind := tm.dirtyTiles[c]
		if slot, exists := tm.tiles[c]; exists {
			tm.rebuildSlot(slot)
			tm.tileVersions[c]++
			changes = append(changes, NavTileChange{Coord: c, Kind: kind, Version: tm.tileVersions[c]})
		} else if kind == NavTileRemoved {
			changes = append(changes, NavTileChange{Coord: c, Kind: NavTileRemoved, Version: tm.tileVersions[c]})
		}
	}
	tm.attachOffMeshLinks(affectedLinks)
	clear(tm.dirtyTiles)
	if len(changes) > 0 {
		tm.revision++
		tm.notifyChanges(changes)
	}
}

// AddObstacle registers a dynamic obstacle and immediately rebuilds only the touched tiles
// (or defers rebuild until Batch returns if called inside Batch).
// Returns (0, false) if tm is nil or obs has invalid geometry.
func (tm *TiledNavMesh) AddObstacle(obs NavObstacle) (NavObstacleID, bool) {
	if tm == nil {
		return 0, false
	}
	cutters, minX, minY, maxX, maxY, ok := buildObstacleCutters2D(obs)
	if !ok {
		return 0, false
	}
	touched, ok := tm.coordsForObstacleAABB(minX, minY, maxX, maxY)
	if !ok {
		return 0, false
	}
	tm.nextObstacleID++
	id := tm.nextObstacleID
	entry := &obstacleEntry2D{
		id:      id,
		obs:     obs,
		cutters: cutters,
		minX:    minX,
		minY:    minY,
		maxX:    maxX,
		maxY:    maxY,
		tiles:   touched,
	}
	tm.obstacles[id] = entry
	for _, c := range touched {
		tm.addTileObstacleRef(c, id)
	}
	tm.rebuildAffectedTiles(touched)
	return id, true
}

// UpdateObstacle replaces the geometry or mode of an existing dynamic obstacle id,
// rebuilding only tiles touched by either its old or new footprint.
func (tm *TiledNavMesh) UpdateObstacle(id NavObstacleID, obs NavObstacle) bool {
	if tm == nil {
		return false
	}
	old, exists := tm.obstacles[id]
	if !exists {
		return false
	}
	cutters, minX, minY, maxX, maxY, ok := buildObstacleCutters2D(obs)
	if !ok {
		return false
	}
	newTiles, ok := tm.coordsForObstacleAABB(minX, minY, maxX, maxY)
	if !ok {
		return false
	}
	oldTiles := old.tiles
	for _, c := range oldTiles {
		tm.removeTileObstacleRef(c, id)
	}
	old.obs = obs
	old.cutters = cutters
	old.minX, old.minY, old.maxX, old.maxY = minX, minY, maxX, maxY
	old.tiles = newTiles
	for _, c := range newTiles {
		tm.addTileObstacleRef(c, id)
	}

	union := make([]NavTileCoord, 0, len(oldTiles)+len(newTiles))
	union = append(union, oldTiles...)
	union = append(union, newTiles...)
	slices.SortFunc(union, compareNavTileCoord2D)
	union = slices.Compact(union)
	tm.rebuildAffectedTiles(union)
	return true
}

// RemoveObstacle removes dynamic obstacle id and rebuilds only the tiles it touched,
// restoring their uncarved geometry.
func (tm *TiledNavMesh) RemoveObstacle(id NavObstacleID) bool {
	if tm == nil {
		return false
	}
	entry, exists := tm.obstacles[id]
	if !exists {
		return false
	}
	delete(tm.obstacles, id)
	for _, c := range entry.tiles {
		tm.removeTileObstacleRef(c, id)
	}
	tm.rebuildAffectedTiles(entry.tiles)
	return true
}

// ClearObstacles removes all dynamic obstacles and rebuilds only tiles that had obstacles.
func (tm *TiledNavMesh) ClearObstacles() {
	if tm == nil || len(tm.obstacles) == 0 {
		return
	}
	var affected []NavTileCoord
	for c := range tm.tileObstacles {
		affected = append(affected, c)
	}
	slices.SortFunc(affected, compareNavTileCoord2D)
	clear(tm.obstacles)
	clear(tm.tileObstacles)
	tm.rebuildAffectedTiles(affected)
}

// ObstacleCount returns the number of active dynamic obstacles.
func (tm *TiledNavMesh) ObstacleCount() int {
	if tm == nil {
		return 0
	}
	return len(tm.obstacles)
}

func (tm *TiledNavMesh) coordsForObstacleAABB(minX, minY, maxX, maxY float64) ([]NavTileCoord, bool) {
	if math.IsNaN(minX) || math.IsInf(minX, 0) || math.IsNaN(minY) || math.IsInf(minY, 0) ||
		math.IsNaN(maxX) || math.IsInf(maxX, 0) || math.IsNaN(maxY) || math.IsInf(maxY, 0) {
		return nil, false
	}
	fMinX := math.Floor((minX - tm.originX) / tm.tileSize)
	fMinY := math.Floor((minY - tm.originY) / tm.tileSize)
	adjMaxX := maxX
	if maxX > minX+tiledSplitEps {
		adjMaxX = maxX - tiledSplitEps
	}
	adjMaxY := maxY
	if maxY > minY+tiledSplitEps {
		adjMaxY = maxY - tiledSplitEps
	}
	fMaxX := math.Floor((adjMaxX - tm.originX) / tm.tileSize)
	fMaxY := math.Floor((adjMaxY - tm.originY) / tm.tileSize)
	if fMaxX < fMinX {
		fMaxX = fMinX
	}
	if fMaxY < fMinY {
		fMaxY = fMinY
	}
	if fMinX < float64(minNavTileCoord2D) || fMaxX > float64(maxNavTileCoord2D) ||
		fMinY < float64(minNavTileCoord2D) || fMaxY > float64(maxNavTileCoord2D) {
		return nil, false
	}
	spanX := int64(fMaxX) - int64(fMinX) + 1
	spanY := int64(fMaxY) - int64(fMinY) + 1
	if spanX <= 0 || spanY <= 0 || spanX > maxNavObstacleTiles2D || spanY > maxNavObstacleTiles2D || spanX*spanY > maxNavObstacleTiles2D {
		return nil, false
	}
	tx0, ty0 := int64(fMinX), int64(fMinY)
	tx1, ty1 := int64(fMaxX), int64(fMaxY)
	out := make([]NavTileCoord, 0, int(spanX*spanY))
	for tx := tx0; tx <= tx1; tx++ {
		for ty := ty0; ty <= ty1; ty++ {
			out = append(out, NavTileCoord{X: int32(tx), Y: int32(ty)})
		}
	}
	return out, true
}

func (tm *TiledNavMesh) addTileObstacleRef(coord NavTileCoord, id NavObstacleID) {
	ids := tm.tileObstacles[coord]
	// Maintain sorted ascending ID order for deterministic carving.
	idx, found := slices.BinarySearch(ids, id)
	if found {
		return
	}
	ids = slices.Insert(ids, idx, id)
	tm.tileObstacles[coord] = ids
}

func (tm *TiledNavMesh) removeTileObstacleRef(coord NavTileCoord, id NavObstacleID) {
	ids := tm.tileObstacles[coord]
	idx, found := slices.BinarySearch(ids, id)
	if !found {
		return
	}
	ids = slices.Delete(ids, idx, idx+1)
	if len(ids) == 0 {
		delete(tm.tileObstacles, coord)
	} else {
		tm.tileObstacles[coord] = ids
	}
}

func (tm *TiledNavMesh) rebuildAffectedTiles(coords []NavTileCoord) {
	if len(coords) == 0 {
		return
	}
	if tm.batchDepth > 0 {
		for _, c := range coords {
			if _, exists := tm.tiles[c]; exists {
				tm.markDirtyBatch(c, NavTileUpdated)
			}
		}
		return
	}
	affectedLinks := tm.detachOffMeshLinksForCoords(coords)
	var changes []NavTileChange
	for _, c := range coords {
		slot, exists := tm.tiles[c]
		if !exists {
			continue
		}
		tm.rebuildSlot(slot)
		tm.tileVersions[c]++
		changes = append(changes, NavTileChange{Coord: c, Kind: NavTileUpdated, Version: tm.tileVersions[c]})
	}
	tm.attachOffMeshLinks(affectedLinks)
	if len(changes) > 0 {
		tm.revision++
		tm.notifyChanges(changes)
	}
}

// --- Off-mesh links (jumps, drops, ladders, teleports) ---

func (tm *TiledNavMesh) resolveLinkEndpoint(p core.Point, radius float32) (*navTileSlot2D, int32, vec2, bool) {
	v := toVec2(p)
	if !isFiniteVec2(v) {
		return nil, -1, vec2{}, false
	}
	if ref, ok := tm.FindPolygon(p); ok {
		slot := tm.tiles[ref.Tile]
		if slot != nil {
			return slot, ref.Poly, v, true
		}
	}
	if radius <= 0 {
		return nil, -1, vec2{}, false
	}
	ext := core.Point{X: core.Points(radius), Y: core.Points(radius)}
	snapped, ref, ok := tm.NearestPointWithFilter(p, ext, NavFilter{})
	if !ok {
		return nil, -1, vec2{}, false
	}
	sv := toVec2(snapped)
	if dist(v, sv) > float64(radius)+portalEps {
		return nil, -1, vec2{}, false
	}
	slot := tm.tiles[ref.Tile]
	if slot == nil {
		return nil, -1, vec2{}, false
	}
	return slot, ref.Poly, sv, true
}

func (tm *TiledNavMesh) offMeshWatchCoords(start, end core.Point, radius float32) ([]NavTileCoord, bool) {
	r := float64(radius) + portalEps*2
	var coords []NavTileCoord
	for _, p := range [2]core.Point{start, end} {
		v := toVec2(p)
		fMinX := math.Floor((v.x - r - tm.originX) / tm.tileSize)
		fMaxX := math.Floor((v.x + r - tm.originX) / tm.tileSize)
		fMinY := math.Floor((v.y - r - tm.originY) / tm.tileSize)
		fMaxY := math.Floor((v.y + r - tm.originY) / tm.tileSize)
		if fMinX < float64(minNavTileCoord2D) || fMaxX > float64(maxNavTileCoord2D) ||
			fMinY < float64(minNavTileCoord2D) || fMaxY > float64(maxNavTileCoord2D) {
			return nil, true
		}
		spanX := int64(fMaxX) - int64(fMinX) + 1
		spanY := int64(fMaxY) - int64(fMinY) + 1
		if spanX <= 0 || spanY <= 0 || spanX > maxOffMeshWatchTiles2D || spanY > maxOffMeshWatchTiles2D || spanX*spanY > maxOffMeshWatchTiles2D {
			return nil, true
		}
		for tx := int64(fMinX); tx <= int64(fMaxX); tx++ {
			for ty := int64(fMinY); ty <= int64(fMaxY); ty++ {
				coords = append(coords, NavTileCoord{X: int32(tx), Y: int32(ty)})
			}
		}
	}
	slices.SortFunc(coords, compareNavTileCoord2D)
	coords = slices.Compact(coords)
	if int64(len(coords)) > maxOffMeshWatchTiles2D {
		return nil, true
	}
	return coords, false
}

func (tm *TiledNavMesh) addTileOffMeshRef(coord NavTileCoord, idx int32) {
	ids := tm.tileOffMeshLinks[coord]
	pos, found := slices.BinarySearch(ids, idx)
	if found {
		return
	}
	tm.tileOffMeshLinks[coord] = slices.Insert(ids, pos, idx)
}

func (tm *TiledNavMesh) detachOffMeshEntry(e *tiledOffMeshEntry2D) {
	if e == nil || !e.attached {
		return
	}
	if int(e.fromSlot) >= 0 && int(e.fromSlot) < len(tm.slots) {
		if s := tm.slots[e.fromSlot]; s != nil {
			s.removeOffMeshPortal(e.idx)
		}
	}
	if e.link.Bidirectional && e.toSlot != e.fromSlot && int(e.toSlot) >= 0 && int(e.toSlot) < len(tm.slots) {
		if s := tm.slots[e.toSlot]; s != nil {
			s.removeOffMeshPortal(e.idx)
		}
	}
	e.attached = false
}

func (tm *TiledNavMesh) attachOffMeshEntry(e *tiledOffMeshEntry2D) bool {
	if e == nil || len(tm.tiles) == 0 {
		return false
	}
	if e.attached {
		tm.detachOffMeshEntry(e)
	}
	fromSlot, fromPoly, sVec, okFrom := tm.resolveLinkEndpoint(e.rawStart, e.link.Radius)
	toSlot, toPoly, eVec, okTo := tm.resolveLinkEndpoint(e.rawEnd, e.link.Radius)
	if !okFrom || !okTo {
		return false
	}
	if fromSlot.slotID == toSlot.slotID && fromPoly == toPoly && vequal(sVec, eVec) {
		return false
	}
	cost := float64(e.link.Cost)
	if !(cost > 0) || math.IsInf(cost, 0) {
		cost = dist(sVec, eVec)
	}
	pid := len(fromSlot.portals)
	fromSlot.portals = append(fromSlot.portals, tiledPortal2D{
		fromPoly:   fromPoly,
		toSlot:     toSlot.slotID,
		toPoly:     toPoly,
		left:       sVec,
		right:      sVec,
		endPos:     eVec,
		cost:       cost,
		flags:      e.link.Flags,
		offMeshIdx: e.idx,
		linkID:     e.link.ID,
		offMesh:    true,
	})
	fromSlot.polys[fromPoly].outPortals = append(fromSlot.polys[fromPoly].outPortals, pid)

	if e.link.Bidirectional {
		rpid := len(toSlot.portals)
		toSlot.portals = append(toSlot.portals, tiledPortal2D{
			fromPoly:   toPoly,
			toSlot:     fromSlot.slotID,
			toPoly:     fromPoly,
			left:       eVec,
			right:      eVec,
			endPos:     sVec,
			cost:       cost,
			flags:      e.link.Flags,
			offMeshIdx: e.idx,
			linkID:     e.link.ID,
			offMesh:    true,
		})
		toSlot.polys[toPoly].outPortals = append(toSlot.polys[toPoly].outPortals, rpid)
	}

	e.attached = true
	e.fromCoord = fromSlot.coord
	e.fromSlot = fromSlot.slotID
	e.fromPoly = fromPoly
	e.toCoord = toSlot.coord
	e.toSlot = toSlot.slotID
	e.toPoly = toPoly
	e.sVec = sVec
	e.eVec = eVec
	e.cost = cost
	e.link.Start = toPoint(sVec)
	e.link.End = toPoint(eVec)
	return true
}

func (tm *TiledNavMesh) detachOffMeshLinksForCoords(coords []NavTileCoord) []int32 {
	if len(tm.offMeshLinks) == 0 || len(coords) == 0 {
		return nil
	}
	var ids []int32
	for _, c := range coords {
		ids = append(ids, tm.tileOffMeshLinks[c]...)
	}
	if len(tm.globalOffMeshLinks) > 0 {
		ids = append(ids, tm.globalOffMeshLinks...)
	}
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for _, idx := range ids {
		if int(idx) > 0 && int(idx) <= len(tm.offMeshLinks) {
			tm.detachOffMeshEntry(tm.offMeshLinks[idx-1])
		}
	}
	return ids
}

func (tm *TiledNavMesh) attachOffMeshLinks(ids []int32) {
	for _, idx := range ids {
		if int(idx) > 0 && int(idx) <= len(tm.offMeshLinks) {
			tm.attachOffMeshEntry(tm.offMeshLinks[idx-1])
		}
	}
}

// AddOffMeshLink connects the polygons containing (or within link.Radius of) link.Start and link.End
// with a directed (or bidirectional) off-mesh portal and automatically re-attaches it whenever touched
// tiles are replaced, rebuilt, or carved by dynamic obstacles.
// Returns false if tm is nil or empty, link coordinates or Radius are invalid, either endpoint cannot
// attach to the mesh, or both endpoints collapse to the same point on the same polygon.
func (tm *TiledNavMesh) AddOffMeshLink(link NavOffMeshLink) bool {
	if tm == nil || len(tm.tiles) == 0 {
		return false
	}
	if !isFinite32(link.Radius) || link.Radius < 0 {
		return false
	}
	if !isFiniteVec2(toVec2(link.Start)) || !isFiniteVec2(toVec2(link.End)) {
		return false
	}
	filterFlags := offMeshFilterFlags(link.Flags)
	stored := link
	stored.Flags = link.Flags | NavLinkOffMesh
	idx := int32(len(tm.offMeshLinks) + 1)
	entry := &tiledOffMeshEntry2D{
		idx:         idx,
		rawStart:    link.Start,
		rawEnd:      link.End,
		link:        stored,
		filterFlags: filterFlags,
	}
	if !tm.attachOffMeshEntry(entry) {
		return false
	}
	watchTiles, globalWatch := tm.offMeshWatchCoords(link.Start, link.End, link.Radius)
	entry.watchTiles = watchTiles
	entry.globalWatch = globalWatch
	tm.offMeshLinks = append(tm.offMeshLinks, entry)
	if globalWatch {
		tm.globalOffMeshLinks = append(tm.globalOffMeshLinks, idx)
	} else {
		for _, c := range watchTiles {
			tm.addTileOffMeshRef(c, idx)
		}
	}

	touched := []NavTileCoord{entry.fromCoord}
	if entry.toCoord != entry.fromCoord {
		touched = append(touched, entry.toCoord)
		slices.SortFunc(touched, compareNavTileCoord2D)
	}
	if tm.batchDepth > 0 {
		for _, c := range touched {
			tm.markDirtyBatch(c, NavTileUpdated)
		}
		return true
	}
	tm.revision++
	changes := make([]NavTileChange, len(touched))
	for i, c := range touched {
		tm.tileVersions[c]++
		changes[i] = NavTileChange{Coord: c, Kind: NavTileUpdated, Version: tm.tileVersions[c]}
	}
	tm.notifyChanges(changes)
	return true
}

// OffMeshLinks returns a copy of all off-mesh links registered on tm in insertion order.
func (tm *TiledNavMesh) OffMeshLinks() []NavOffMeshLink {
	if tm == nil || len(tm.offMeshLinks) == 0 {
		return nil
	}
	out := make([]NavOffMeshLink, len(tm.offMeshLinks))
	for i, e := range tm.offMeshLinks {
		out[i] = e.link
	}
	return out
}

// ClearOffMeshLinks removes all off-mesh links from tm and bumps the mesh revision when any were present.
func (tm *TiledNavMesh) ClearOffMeshLinks() {
	if tm == nil || len(tm.offMeshLinks) == 0 {
		return
	}
	var touched []NavTileCoord
	for _, e := range tm.offMeshLinks {
		if e.attached {
			touched = append(touched, e.fromCoord)
			if e.toCoord != e.fromCoord {
				touched = append(touched, e.toCoord)
			}
			tm.detachOffMeshEntry(e)
		}
	}
	tm.offMeshLinks = nil
	clear(tm.tileOffMeshLinks)
	tm.globalOffMeshLinks = nil

	slices.SortFunc(touched, compareNavTileCoord2D)
	touched = slices.Compact(touched)
	if tm.batchDepth > 0 {
		if len(touched) == 0 {
			tm.revision++
			return
		}
		for _, c := range touched {
			tm.markDirtyBatch(c, NavTileUpdated)
		}
		return
	}
	tm.revision++
	if len(touched) == 0 {
		return
	}
	changes := make([]NavTileChange, len(touched))
	for i, c := range touched {
		tm.tileVersions[c]++
		changes[i] = NavTileChange{Coord: c, Kind: NavTileUpdated, Version: tm.tileVersions[c]}
	}
	tm.notifyChanges(changes)
}

func (tm *TiledNavMesh) offMeshMeta(p tiledPortal2D) (radius float32, area NavArea, filterFlags NavFlags, userData any) {
	if p.offMeshIdx > 0 && int(p.offMeshIdx) <= len(tm.offMeshLinks) {
		e := tm.offMeshLinks[p.offMeshIdx-1]
		return e.link.Radius, e.link.Area, e.filterFlags, e.link.UserData
	}
	return 0, NavAreaDefault, offMeshFilterFlags(p.flags), nil
}

func (tm *TiledNavMesh) offMeshPortalCost2D(p tiledPortal2D, filter NavFilter) (float64, bool) {
	_, area, filterFlags, _ := tm.offMeshMeta(p)
	if !filter.Allows(area, filterFlags) {
		return 0, false
	}
	mult := filter.AreaCost(area)
	if mult <= 0 || math.IsNaN(mult) || math.IsInf(mult, 0) {
		return 0, false
	}
	c := p.cost * mult
	if math.IsNaN(c) || math.IsInf(c, 0) {
		return 0, false
	}
	return c, true
}

func (tm *TiledNavMesh) buildTiledNavShortcuts2D(gVec vec2, minCost float64, filter NavFilter) ([]navShortcut2D, bool) {
	if len(tm.offMeshLinks) == 0 {
		return nil, false
	}
	var dst []navShortcut2D
	for _, e := range tm.offMeshLinks {
		if !e.attached {
			continue
		}
		if !filter.Allows(e.link.Area, e.filterFlags) {
			continue
		}
		mult := filter.AreaCost(e.link.Area)
		if mult <= 0 || math.IsNaN(mult) || math.IsInf(mult, 0) {
			continue
		}
		effCost := e.cost * mult
		if math.IsNaN(effCost) || math.IsInf(effCost, 0) {
			continue
		}
		if effCost >= float64(dist(e.sVec, e.eVec)*minCost)-1e-9 {
			continue
		}
		if toSlot := tm.slots[e.toSlot]; toSlot != nil && int(e.toPoly) < len(toSlot.polys) {
			tp := &toSlot.polys[e.toPoly]
			if filter.Allows(tp.area, tp.flags) {
				tc := filter.AreaCost(tp.area)
				if tc > 0 && !math.IsNaN(tc) && !math.IsInf(tc, 0) {
					if len(dst) >= 64 {
						return nil, true
					}
					dst = append(dst, navShortcut2D{
						start:  e.sVec,
						end:    e.eVec,
						cost:   effCost,
						toGoal: float64(dist(e.eVec, gVec) * minCost),
					})
				}
			}
		}
		if e.link.Bidirectional {
			if fromSlot := tm.slots[e.fromSlot]; fromSlot != nil && int(e.fromPoly) < len(fromSlot.polys) {
				fp := &fromSlot.polys[e.fromPoly]
				if filter.Allows(fp.area, fp.flags) {
					fc := filter.AreaCost(fp.area)
					if fc > 0 && !math.IsNaN(fc) && !math.IsInf(fc, 0) {
						if len(dst) >= 64 {
							return nil, true
						}
						dst = append(dst, navShortcut2D{
							start:  e.eVec,
							end:    e.sVec,
							cost:   effCost,
							toGoal: float64(dist(e.sVec, gVec) * minCost),
						})
					}
				}
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
				cand := float64(dist(endI, dst[j].start)*minCost) + dst[j].cost + dst[j].toGoal
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

// AddTileListener registers fn to be invoked whenever any tile is added, updated, or removed.
func (tm *TiledNavMesh) AddTileListener(fn func(NavTileChange)) NavListenerID {
	if tm == nil || fn == nil {
		return 0
	}
	tm.nextListenerID++
	id := tm.nextListenerID
	tm.listeners = append(tm.listeners, tileListener2D{id: id, fn: fn})
	return id
}

// RemoveTileListener unregisters a listener previously added with AddTileListener.
func (tm *TiledNavMesh) RemoveTileListener(id NavListenerID) bool {
	if tm == nil || id == 0 {
		return false
	}
	for i, l := range tm.listeners {
		if l.id == id {
			tm.listeners = append(tm.listeners[:i], tm.listeners[i+1:]...)
			return true
		}
	}
	return false
}

// IsPathValid reports whether every tile crossed by res is still present and unchanged
// since res was computed.
func (tm *TiledNavMesh) IsPathValid(res TiledNavPathResult) bool {
	if tm == nil || !res.Found || len(res.Tiles) == 0 {
		return false
	}
	for _, tv := range res.Tiles {
		if _, exists := tm.tiles[tv.Coord]; !exists {
			return false
		}
		if tm.tileVersions[tv.Coord] != tv.Version {
			return false
		}
	}
	return true
}

// WatchPath registers a one-shot invalidation callback invoked when any tile in tiles changes.
// Because watchers are indexed per tile, changing a tile notifies only paths crossing that tile.
// Once triggered, the watch is automatically removed.
func (tm *TiledNavMesh) WatchPath(tiles []NavTileCoord, onInvalidate func(NavTileChange)) NavPathWatchID {
	if tm == nil || len(tiles) == 0 || onInvalidate == nil {
		return 0
	}
	dedup := make([]NavTileCoord, len(tiles))
	copy(dedup, tiles)
	slices.SortFunc(dedup, compareNavTileCoord2D)
	dedup = slices.Compact(dedup)

	tm.nextWatchID++
	id := tm.nextWatchID
	tm.watches[id] = &pathWatch2D{
		id:    id,
		tiles: dedup,
		fn:    onInvalidate,
	}
	for _, c := range dedup {
		tm.tileWatches[c] = append(tm.tileWatches[c], id)
	}
	return id
}

// UnwatchPath cancels a path watcher previously registered with WatchPath.
func (tm *TiledNavMesh) UnwatchPath(id NavPathWatchID) bool {
	if tm == nil || id == 0 {
		return false
	}
	w, exists := tm.watches[id]
	if !exists {
		return false
	}
	delete(tm.watches, id)
	for _, c := range w.tiles {
		ids := tm.tileWatches[c]
		if idx, found := slices.BinarySearch(ids, id); found {
			ids = slices.Delete(ids, idx, idx+1)
			if len(ids) == 0 {
				delete(tm.tileWatches, c)
			} else {
				tm.tileWatches[c] = ids
			}
		}
	}
	return true
}

func (tm *TiledNavMesh) notifyChanges(changes []NavTileChange) {
	if len(changes) == 0 {
		return
	}
	// Fire global tile listeners in deterministic registration order.
	if len(tm.listeners) > 0 {
		listeners := make([]tileListener2D, len(tm.listeners))
		copy(listeners, tm.listeners)
		for _, ch := range changes {
			for _, l := range listeners {
				l.fn(ch)
			}
		}
	}

	// Fire per-tile path watchers at most once per watch, in deterministic watch ID order.
	if len(tm.tileWatches) == 0 {
		return
	}
	type triggered struct {
		id NavPathWatchID
		ch NavTileChange
		fn func(NavTileChange)
	}
	var toFire []triggered
	seen := make(map[NavPathWatchID]bool)
	for _, ch := range changes {
		ids := tm.tileWatches[ch.Coord]
		for _, wid := range ids {
			if seen[wid] {
				continue
			}
			w := tm.watches[wid]
			if w == nil {
				continue
			}
			seen[wid] = true
			toFire = append(toFire, triggered{id: wid, ch: ch, fn: w.fn})
		}
	}
	if len(toFire) == 0 {
		return
	}
	slices.SortFunc(toFire, func(a, b triggered) int {
		if a.id < b.id {
			return -1
		}
		if a.id > b.id {
			return 1
		}
		return 0
	})
	for _, item := range toFire {
		tm.UnwatchPath(item.id)
	}
	for _, item := range toFire {
		item.fn(item.ch)
	}
}

// --- Spatial index lookups (O(1) tile hash + per-tile BVH / coarse tile BVH) ---

func (tm *TiledNavMesh) resolveRef(ref NavPolyRef) (*navTileSlot2D, *navPoly) {
	if tm == nil || !ref.Valid() {
		return nil, nil
	}
	slot := tm.tiles[ref.Tile]
	if slot == nil || slot.salt != ref.Salt || int(ref.Poly) >= len(slot.polys) {
		return nil, nil
	}
	return slot, &slot.polys[ref.Poly]
}

// Polygon returns a copy of the vertices of ref, or nil if ref is stale or invalid.
func (tm *TiledNavMesh) Polygon(ref NavPolyRef) []core.Point {
	_, p := tm.resolveRef(ref)
	if p == nil {
		return nil
	}
	return polyPoints2D(p.verts)
}

// PolygonArea returns the area of ref, or NavAreaDefault if ref is invalid.
func (tm *TiledNavMesh) PolygonArea(ref NavPolyRef) NavArea {
	_, p := tm.resolveRef(ref)
	if p == nil {
		return NavAreaDefault
	}
	return p.area
}

// PolygonFlags returns the runtime flags of ref, or 0 if ref is invalid.
func (tm *TiledNavMesh) PolygonFlags(ref NavPolyRef) NavFlags {
	_, p := tm.resolveRef(ref)
	if p == nil {
		return 0
	}
	return p.flags
}

// Neighbors returns the polygon references sharing an edge (intra-tile or cross-tile) with ref.
func (tm *TiledNavMesh) Neighbors(ref NavPolyRef) []NavPolyRef {
	slot, p := tm.resolveRef(ref)
	if p == nil {
		return nil
	}
	out := make([]NavPolyRef, 0, len(p.outPortals))
	for _, pid := range p.outPortals {
		tp := slot.portals[pid]
		dst := tm.slots[tp.toSlot]
		if dst == nil {
			continue
		}
		out = append(out, NavPolyRef{Tile: dst.coord, Poly: tp.toPoly, Salt: dst.salt})
	}
	return out
}

// candidateTilesAtPoint returns up to 4 tiles whose bounds (expanded by portalEps) contain v,
// sorted deterministically by NavTileCoord.
func (tm *TiledNavMesh) candidateTilesAtPoint(v vec2, buf []NavTileCoord) []NavTileCoord {
	buf = buf[:0]
	tx0 := clampTileCoord2D((v.x - portalEps - tm.originX) / tm.tileSize)
	tx1 := clampTileCoord2D((v.x + portalEps - tm.originX) / tm.tileSize)
	ty0 := clampTileCoord2D((v.y - portalEps - tm.originY) / tm.tileSize)
	ty1 := clampTileCoord2D((v.y + portalEps - tm.originY) / tm.tileSize)
	for tx := int64(tx0); tx <= int64(tx1); tx++ {
		for ty := int64(ty0); ty <= int64(ty1); ty++ {
			buf = append(buf, NavTileCoord{X: int32(tx), Y: int32(ty)})
		}
	}
	return buf
}

// FindPolygon locates a polygon containing p in O(1) tile lookup + O(log P_tile) local BVH.
func (tm *TiledNavMesh) FindPolygon(p core.Point) (NavPolyRef, bool) {
	return tm.FindPolygonWithFilter(p, NavFilter{})
}

// FindPolygonWithFilter locates a polygon passing filter that contains p.
func (tm *TiledNavMesh) FindPolygonWithFilter(p core.Point, filter NavFilter) (NavPolyRef, bool) {
	if tm == nil || len(tm.tiles) == 0 {
		return NavPolyRef{}, false
	}
	v := toVec2(p)
	if !isFiniteVec2(v) {
		return NavPolyRef{}, false
	}
	var coordBuf [4]NavTileCoord
	coords := tm.candidateTilesAtPoint(v, coordBuf[:0])
	for _, c := range coords {
		slot := tm.tiles[c]
		if slot == nil || len(slot.polys) == 0 {
			continue
		}
		if idx := findPolyInBVH2D(slot.polys, slot.bvhNodes, slot.bvhOrder, v, filter, true); idx >= 0 {
			return NavPolyRef{Tile: c, Poly: int32(idx), Salt: slot.salt}, true
		}
	}
	return NavPolyRef{}, false
}

// Contains reports whether p lies inside any polygon of the tiled mesh.
func (tm *TiledNavMesh) Contains(p core.Point) bool {
	_, ok := tm.FindPolygon(p)
	return ok
}

// ContainsWithFilter reports whether p lies inside any polygon passing filter.
func (tm *TiledNavMesh) ContainsWithFilter(p core.Point, filter NavFilter) bool {
	_, ok := tm.FindPolygonWithFilter(p, filter)
	return ok
}

func (tm *TiledNavMesh) ensureTileBVH() {
	if !tm.tileBVHDirty.Load() {
		return
	}
	tm.tileBVHMu.Lock()
	defer tm.tileBVHMu.Unlock()
	if !tm.tileBVHDirty.Load() {
		return
	}
	order := make([]*navTileSlot2D, 0, len(tm.tiles))
	for _, slot := range tm.tiles {
		if slot != nil && slot.hasBounds {
			order = append(order, slot)
		}
	}
	if len(order) == 0 {
		tm.tileBVHOrder = nil
		tm.tileBVHNodes = nil
		tm.tileBVHDirty.Store(false)
		return
	}
	slices.SortFunc(order, func(a, b *navTileSlot2D) int {
		return compareNavTileCoord2D(a.coord, b.coord)
	})
	n := len(order)
	nodes := make([]navBVHNode2D, 0, max(1, (2*n)/navBVHLeafSize+4))
	var build func(start, end, depth int) int32
	build = func(start, end, depth int) int32 {
		nodeIdx := int32(len(nodes))
		nodes = append(nodes, navBVHNode2D{})

		s0 := order[start]
		minX, minY, maxX, maxY := s0.polyMinX, s0.polyMinY, s0.polyMaxX, s0.polyMaxY
		cMinX, cMinY := 0.5*(s0.polyMinX+s0.polyMaxX), 0.5*(s0.polyMinY+s0.polyMaxY)
		cMaxX, cMaxY := cMinX, cMinY
		for i := start + 1; i < end; i++ {
			s := order[i]
			minX = min(minX, s.polyMinX)
			minY = min(minY, s.polyMinY)
			maxX = max(maxX, s.polyMaxX)
			maxY = max(maxY, s.polyMaxY)
			cx := 0.5 * (s.polyMinX + s.polyMaxX)
			cy := 0.5 * (s.polyMinY + s.polyMaxY)
			cMinX = min(cMinX, cx)
			cMinY = min(cMinY, cy)
			cMaxX = max(cMaxX, cx)
			cMaxY = max(cMaxY, cy)
		}

		count := end - start
		if count <= navBVHLeafSize || depth >= 56 {
			nodes[nodeIdx] = navBVHNode2D{
				minX:  minX,
				minY:  minY,
				maxX:  maxX,
				maxY:  maxY,
				left:  int32(start),
				count: int32(count),
			}
			return nodeIdx
		}

		splitY := (cMaxY - cMinY) > (cMaxX - cMinX)
		sub := order[start:end]
		if splitY {
			slices.SortFunc(sub, func(a, b *navTileSlot2D) int {
				ca := 0.5 * (a.polyMinY + a.polyMaxY)
				cb := 0.5 * (b.polyMinY + b.polyMaxY)
				if ca < cb {
					return -1
				}
				if ca > cb {
					return 1
				}
				return compareNavTileCoord2D(a.coord, b.coord)
			})
		} else {
			slices.SortFunc(sub, func(a, b *navTileSlot2D) int {
				ca := 0.5 * (a.polyMinX + a.polyMaxX)
				cb := 0.5 * (b.polyMinX + b.polyMaxX)
				if ca < cb {
					return -1
				}
				if ca > cb {
					return 1
				}
				return compareNavTileCoord2D(a.coord, b.coord)
			})
		}
		mid := start + count/2
		leftIdx := build(start, mid, depth+1)
		rightIdx := build(mid, end, depth+1)
		nodes[nodeIdx] = navBVHNode2D{
			minX:  minX,
			minY:  minY,
			maxX:  maxX,
			maxY:  maxY,
			left:  leftIdx,
			right: rightIdx,
		}
		return nodeIdx
	}
	build(0, n, 0)
	tm.tileBVHOrder = order
	tm.tileBVHNodes = nodes
	tm.tileBVHDirty.Store(false)
}

// NearestPolygon returns the polygon closest to p and the closest point on or inside it,
// using the two-level spatial index (coarse tile BVH + per-tile BVH).
func (tm *TiledNavMesh) NearestPolygon(p core.Point) (NavPolyRef, core.Point, bool) {
	return tm.NearestPolygonWithFilter(p, NavFilter{})
}

// NearestPolygonWithFilter returns the closest polygon passing filter and the closest point on it.
func (tm *TiledNavMesh) NearestPolygonWithFilter(p core.Point, filter NavFilter) (NavPolyRef, core.Point, bool) {
	if tm == nil || len(tm.tiles) == 0 {
		return NavPolyRef{}, core.Point{}, false
	}
	v := toVec2(p)
	if !isFiniteVec2(v) {
		return NavPolyRef{}, core.Point{}, false
	}
	if ref, ok := tm.FindPolygonWithFilter(p, filter); ok {
		return ref, p, true
	}
	ref, pt, ok := tm.nearestPolyInternal(v, vec2{}, false, filter)
	if !ok {
		return NavPolyRef{}, core.Point{}, false
	}
	return ref, toPoint(pt), true
}

// NearestPoint returns the closest point on any polygon allowed by DefaultNavFilter within
// half-extents [p - extent, p + extent], together with its NavPolyRef.
func (tm *TiledNavMesh) NearestPoint(p, extent core.Point) (core.Point, NavPolyRef, bool) {
	return tm.NearestPointWithFilter(p, extent, DefaultNavFilter())
}

// NearestPointWithFilter behaves like NearestPoint restricted to filter.
func (tm *TiledNavMesh) NearestPointWithFilter(p, extent core.Point, filter NavFilter) (core.Point, NavPolyRef, bool) {
	if tm == nil || len(tm.tiles) == 0 {
		return core.Point{}, NavPolyRef{}, false
	}
	v := toVec2(p)
	ext := toVec2(extent)
	if !isFiniteVec2(v) || !isFiniteVec2(ext) || ext.x < 0 || ext.y < 0 {
		return core.Point{}, NavPolyRef{}, false
	}
	if ref, ok := tm.FindPolygonWithFilter(p, filter); ok {
		return p, ref, true
	}
	ref, pt, ok := tm.nearestPolyInternal(v, ext, true, filter)
	if !ok {
		return core.Point{}, NavPolyRef{}, false
	}
	return toPoint(pt), ref, true
}

func (tm *TiledNavMesh) nearestPolyInternal(v, ext vec2, useExtent bool, filter NavFilter) (NavPolyRef, vec2, bool) {
	bMinX, bMinY := v.x-ext.x-portalEps, v.y-ext.y-portalEps
	bMaxX, bMaxY := v.x+ext.x+portalEps, v.y+ext.y+portalEps

	var bestRef NavPolyRef
	bestFound := false
	bestDistSq := math.Inf(1)
	var bestPt vec2

	considerSlot := func(slot *navTileSlot2D) {
		if slot == nil || !slot.hasBounds {
			return
		}
		if useExtent && (slot.polyMaxX < bMinX || slot.polyMinX > bMaxX || slot.polyMaxY < bMinY || slot.polyMinY > bMaxY) {
			return
		}
		if aabbDistSq2D(slot.polyMinX, slot.polyMinY, slot.polyMaxX, slot.polyMaxY, v) > bestDistSq {
			return
		}
		idx, pt, d2, ok := nearestPolyInBVH2D(slot.polys, slot.bvhNodes, slot.bvhOrder, v, ext, useExtent, filter, bestDistSq)
		if !ok {
			return
		}
		candRef := NavPolyRef{Tile: slot.coord, Poly: int32(idx), Salt: slot.salt}
		if !bestFound || d2 < bestDistSq || (d2 == bestDistSq && comparePolyRef2D(candRef, bestRef) < 0) {
			bestFound = true
			bestDistSq = d2
			bestRef = candRef
			bestPt = pt
		}
	}

	// Fast path for bounded queries covering only a small number of grid tiles.
	if useExtent {
		fMinX := math.Floor((bMinX - tm.originX) / tm.tileSize)
		fMaxX := math.Floor((bMaxX - tm.originX) / tm.tileSize)
		fMinY := math.Floor((bMinY - tm.originY) / tm.tileSize)
		fMaxY := math.Floor((bMaxY - tm.originY) / tm.tileSize)
		if fMinX >= float64(minNavTileCoord2D) && fMaxX <= float64(maxNavTileCoord2D) &&
			fMinY >= float64(minNavTileCoord2D) && fMaxY <= float64(maxNavTileCoord2D) {
			tx0, tx1 := int64(fMinX), int64(fMaxX)
			ty0, ty1 := int64(fMinY), int64(fMaxY)
			spanX := tx1 - tx0 + 1
			spanY := ty1 - ty0 + 1
			if spanX > 0 && spanY > 0 && spanX <= 64 && spanY <= 64 && spanX*spanY <= 64 && spanX*spanY <= int64(len(tm.tiles))*2 {
				for tx := tx0; tx <= tx1; tx++ {
					for ty := ty0; ty <= ty1; ty++ {
						considerSlot(tm.tiles[NavTileCoord{X: int32(tx), Y: int32(ty)}])
					}
				}
				return bestRef, bestPt, bestFound
			}
		}
	}

	// General path: branch-and-bound over coarse tile BVH.
	tm.ensureTileBVH()
	if len(tm.tileBVHNodes) == 0 {
		return NavPolyRef{}, vec2{}, false
	}
	root := &tm.tileBVHNodes[0]
	if useExtent && (root.maxX < bMinX || root.minX > bMaxX || root.maxY < bMinY || root.minY > bMaxY) {
		return NavPolyRef{}, vec2{}, false
	}
	var stack [64]bvhDistItem
	stack[0] = bvhDistItem{idx: 0, distSq: aabbDistSq2D(root.minX, root.minY, root.maxX, root.maxY, v)}
	sp := 1
	for sp > 0 {
		sp--
		item := stack[sp]
		if item.distSq > bestDistSq {
			continue
		}
		n := &tm.tileBVHNodes[item.idx]
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				considerSlot(tm.tileBVHOrder[k])
			}
			continue
		}
		lNode := &tm.tileBVHNodes[n.left]
		rNode := &tm.tileBVHNodes[n.right]
		lOk := !useExtent || !(lNode.maxX < bMinX || lNode.minX > bMaxX || lNode.maxY < bMinY || lNode.minY > bMaxY)
		rOk := !useExtent || !(rNode.maxX < bMinX || rNode.minX > bMaxX || rNode.maxY < bMinY || rNode.minY > bMaxY)
		lDist := math.Inf(1)
		if lOk {
			lDist = aabbDistSq2D(lNode.minX, lNode.minY, lNode.maxX, lNode.maxY, v)
		}
		rDist := math.Inf(1)
		if rOk {
			rDist = aabbDistSq2D(rNode.minX, rNode.minY, rNode.maxX, rNode.maxY, v)
		}
		if lDist <= rDist {
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.right, distSq: rDist}
				sp++
			}
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.left, distSq: lDist}
				sp++
			}
		} else {
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.left, distSq: lDist}
				sp++
			}
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.right, distSq: rDist}
				sp++
			}
		}
	}
	return bestRef, bestPt, bestFound
}

func comparePolyRef2D(a, b NavPolyRef) int {
	if c := compareNavTileCoord2D(a.Tile, b.Tile); c != 0 {
		return c
	}
	if a.Poly < b.Poly {
		return -1
	}
	if a.Poly > b.Poly {
		return 1
	}
	return 0
}

// --- Per-tile BVH query helpers ---

func queryPolyBVH2DAABB(polys []navPoly, nodes []navBVHNode2D, order []int, minX, minY, maxX, maxY float64, dst []int) []int {
	if len(nodes) == 0 {
		for i := range polys {
			p := &polys[i]
			if p.maxX >= minX && p.minX <= maxX && p.maxY >= minY && p.minY <= maxY {
				dst = append(dst, i)
			}
		}
		return dst
	}
	var stack [64]int32
	stack[0] = 0
	sp := 1
	for sp > 0 {
		sp--
		n := &nodes[stack[sp]]
		if n.maxX < minX || n.minX > maxX || n.maxY < minY || n.minY > maxY {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				idx := order[k]
				p := &polys[idx]
				if p.maxX >= minX && p.minX <= maxX && p.maxY >= minY && p.minY <= maxY {
					dst = append(dst, idx)
				}
			}
			continue
		}
		stack[sp] = n.right
		sp++
		stack[sp] = n.left
		sp++
	}
	return dst
}

func findPolyInBVH2D(polys []navPoly, nodes []navBVHNode2D, order []int, v vec2, filter NavFilter, useFilter bool) int {
	if len(nodes) == 0 {
		for i := range polys {
			if useFilter && !filter.Allows(polys[i].area, polys[i].flags) {
				continue
			}
			if polyContains(&polys[i], v) {
				return i
			}
		}
		return -1
	}
	best := -1
	var stack [64]int32
	stack[0] = 0
	sp := 1
	for sp > 0 {
		sp--
		n := &nodes[stack[sp]]
		if v.x < n.minX-portalEps || v.x > n.maxX+portalEps || v.y < n.minY-portalEps || v.y > n.maxY+portalEps {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				idx := order[k]
				if best >= 0 && idx >= best {
					continue
				}
				p := &polys[idx]
				if useFilter && !filter.Allows(p.area, p.flags) {
					continue
				}
				if polyContains(p, v) {
					best = idx
				}
			}
			continue
		}
		stack[sp] = n.right
		sp++
		stack[sp] = n.left
		sp++
	}
	return best
}

func findPolysAtInBVH2D(polys []navPoly, nodes []navBVHNode2D, order []int, v vec2, filter NavFilter, requireFiniteCost bool, dst []int) []int {
	dst = dst[:0]
	if len(nodes) == 0 {
		for i := range polys {
			p := &polys[i]
			if !filter.Allows(p.area, p.flags) {
				continue
			}
			if requireFiniteCost {
				c := filter.AreaCost(p.area)
				if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
					continue
				}
			}
			if polyContains(p, v) {
				dst = append(dst, i)
			}
		}
		return dst
	}
	var stack [64]int32
	stack[0] = 0
	sp := 1
	for sp > 0 {
		sp--
		n := &nodes[stack[sp]]
		if v.x < n.minX-portalEps || v.x > n.maxX+portalEps || v.y < n.minY-portalEps || v.y > n.maxY+portalEps {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				idx := order[k]
				p := &polys[idx]
				if !filter.Allows(p.area, p.flags) {
					continue
				}
				if requireFiniteCost {
					c := filter.AreaCost(p.area)
					if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
						continue
					}
				}
				if polyContains(p, v) {
					dst = append(dst, idx)
				}
			}
			continue
		}
		stack[sp] = n.right
		sp++
		stack[sp] = n.left
		sp++
	}
	slices.Sort(dst)
	return dst
}

func nearestPolyInBVH2D(polys []navPoly, nodes []navBVHNode2D, order []int, v, ext vec2, useExtent bool, filter NavFilter, maxDistSq float64) (int, vec2, float64, bool) {
	bMinX, bMinY := v.x-ext.x-portalEps, v.y-ext.y-portalEps
	bMaxX, bMaxY := v.x+ext.x+portalEps, v.y+ext.y+portalEps
	cMinX, cMinY := v.x-ext.x, v.y-ext.y
	cMaxX, cMaxY := v.x+ext.x, v.y+ext.y

	bestIdx := -1
	bestDistSq := maxDistSq
	var bestPt vec2
	var clipA, clipB [16]vec2

	checkPoly := func(idx int) {
		p := &polys[idx]
		if !filter.Allows(p.area, p.flags) {
			return
		}
		if useExtent && (p.maxX < bMinX || p.minX > bMaxX || p.maxY < bMinY || p.minY > bMaxY) {
			return
		}
		if aabbDistSq2D(p.minX, p.minY, p.maxX, p.maxY, v) > bestDistSq {
			return
		}
		cp := closestPointInPoly(p, v)
		if useExtent && (math.Abs(cp.x-v.x) > ext.x+portalEps || math.Abs(cp.y-v.y) > ext.y+portalEps) {
			clipped := clipPolyAABB2DAny(p.verts, cMinX, cMinY, cMaxX, cMaxY, clipA[:0], clipB[:0])
			if len(clipped) == 0 {
				return
			}
			cp = closestPointOnConvex2D(clipped, v)
		}
		d2 := distSq(v, cp)
		if d2 < bestDistSq || (d2 == bestDistSq && (bestIdx < 0 || idx < bestIdx)) {
			bestDistSq = d2
			bestIdx = idx
			bestPt = cp
		}
	}

	if len(nodes) == 0 {
		for i := range polys {
			checkPoly(i)
		}
		return bestIdx, bestPt, bestDistSq, bestIdx >= 0
	}

	var stack [64]bvhDistItem
	root := &nodes[0]
	if useExtent && (root.maxX < bMinX || root.minX > bMaxX || root.maxY < bMinY || root.minY > bMaxY) {
		return -1, vec2{}, bestDistSq, false
	}
	stack[0] = bvhDistItem{idx: 0, distSq: aabbDistSq2D(root.minX, root.minY, root.maxX, root.maxY, v)}
	sp := 1
	for sp > 0 {
		sp--
		item := stack[sp]
		if item.distSq > bestDistSq {
			continue
		}
		n := &nodes[item.idx]
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				checkPoly(order[k])
			}
			continue
		}
		lNode := &nodes[n.left]
		rNode := &nodes[n.right]
		lOk := !useExtent || !(lNode.maxX < bMinX || lNode.minX > bMaxX || lNode.maxY < bMinY || lNode.minY > bMaxY)
		rOk := !useExtent || !(rNode.maxX < bMinX || rNode.minX > bMaxX || rNode.maxY < bMinY || rNode.minY > bMaxY)
		lDist := math.Inf(1)
		if lOk {
			lDist = aabbDistSq2D(lNode.minX, lNode.minY, lNode.maxX, lNode.maxY, v)
		}
		rDist := math.Inf(1)
		if rOk {
			rDist = aabbDistSq2D(rNode.minX, rNode.minY, rNode.maxX, rNode.maxY, v)
		}
		if lDist <= rDist {
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.right, distSq: rDist}
				sp++
			}
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.left, distSq: lDist}
				sp++
			}
		} else {
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.left, distSq: lDist}
				sp++
			}
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem{idx: n.right, distSq: rDist}
				sp++
			}
		}
	}
	return bestIdx, bestPt, bestDistSq, bestIdx >= 0
}

// --- Raycast across tiles ---

// Raycast casts a segment along the 2D TiledNavMesh surface from start toward target using DefaultNavFilter.
func (tm *TiledNavMesh) Raycast(start, target core.Point) (TiledNavRaycastHit, bool) {
	return tm.RaycastWithFilter(start, target, DefaultNavFilter())
}

// RaycastWithFilter behaves like Raycast restricted to polygons allowed by filter.
func (tm *TiledNavMesh) RaycastWithFilter(start, target core.Point, filter NavFilter) (TiledNavRaycastHit, bool) {
	return tm.raycastWithFilterInternal(start, target, filter, 0, nil)
}

func (tm *TiledNavMesh) raycastWithFilterInternal(start, target core.Point, filter NavFilter, maxAreaCost float64, visitedTiles map[NavTileCoord]bool) (TiledNavRaycastHit, bool) {
	sVec := toVec2(start)
	tVec := toVec2(target)
	if tm == nil || len(tm.tiles) == 0 || !isFiniteVec2(sVec) || !isFiniteVec2(tVec) {
		hit := TiledNavRaycastHit{Point: start, T: 0, Hit: true}
		return hit, true
	}
	allowPoly := func(p *navPoly) bool {
		if !filter.Allows(p.area, p.flags) {
			return false
		}
		if maxAreaCost > 0 {
			c := filter.AreaCost(p.area)
			if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) || c > maxAreaCost {
				return false
			}
		}
		return true
	}
	rawStarts := tm.findPolyLocsAt(sVec, filter, maxAreaCost > 0)
	var starts []polyLoc2D
	if maxAreaCost > 0 {
		for _, loc := range rawStarts {
			if allowPoly(&tm.slots[loc.slot].polys[loc.poly]) {
				starts = append(starts, loc)
			}
		}
	} else {
		starts = rawStarts
	}
	if len(starts) == 0 {
		hit := TiledNavRaycastHit{Point: start, T: 0, Hit: true}
		return hit, true
	}

	// Pick the starting polygon that advances farthest toward target (important when starting on a tile border).
	curLoc := starts[0]
	if len(starts) > 1 && !vequal(sVec, tVec) {
		dir := tVec.sub(sVec)
		bestT := -1.0
		for _, loc := range starts {
			poly := &tm.slots[loc.slot].polys[loc.poly]
			if polyContains(poly, tVec) {
				curLoc = loc
				break
			}
			tExit, _, ok := polyRayExit2D(poly, sVec, dir)
			if ok && tExit > bestT {
				bestT = tExit
				curLoc = loc
			}
		}
	}

	curSlot := tm.slots[curLoc.slot]
	if visitedTiles != nil {
		visitedTiles[curSlot.coord] = true
	}
	curRef := NavPolyRef{Tile: curSlot.coord, Poly: curLoc.poly, Salt: curSlot.salt}
	if vequal(sVec, tVec) || polyContains(&curSlot.polys[curLoc.poly], tVec) {
		return TiledNavRaycastHit{Point: target, Poly: curRef, T: 1, Hit: false}, false
	}

	dir := tVec.sub(sVec)
	curT := 0.0
	visited := make(map[polyLoc2D]bool, 16)
	visited[curLoc] = true

	maxSteps := tm.polyCount + 4
	for range maxSteps {
		slot := tm.slots[curLoc.slot]
		if visitedTiles != nil {
			visitedTiles[slot.coord] = true
		}
		poly := &slot.polys[curLoc.poly]
		curRef = NavPolyRef{Tile: slot.coord, Poly: curLoc.poly, Salt: slot.salt}
		if polyContains(poly, tVec) {
			return TiledNavRaycastHit{Point: target, Poly: curRef, T: 1, Hit: false}, false
		}
		tExit, exitNorm, ok := polyRayExit2D(poly, sVec, dir)
		if !ok {
			pt := sVec.add(dir.scale(curT))
			return TiledNavRaycastHit{Point: toPoint(pt), Poly: curRef, T: float32(curT), Hit: true}, true
		}
		if tExit < curT {
			tExit = curT
		}
		if tExit >= 1.0-1e-9 {
			if polyContains(poly, tVec) {
				return TiledNavRaycastHit{Point: target, Poly: curRef, T: 1, Hit: false}, false
			}
			tExit = 1.0
		}
		exitPt := sVec.add(dir.scale(tExit))

		nextLoc := polyLoc2D{slot: -1, poly: -1}
		bestNextT := tExit + 1e-9
		reached := false
		for _, pid := range poly.outPortals {
			tp := slot.portals[pid]
			if tp.offMesh {
				continue
			}
			cand := polyLoc2D{slot: tp.toSlot, poly: tp.toPoly}
			if visited[cand] {
				continue
			}
			ns := tm.slots[tp.toSlot]
			if ns == nil {
				continue
			}
			np := &ns.polys[tp.toPoly]
			if !allowPoly(np) {
				continue
			}
			if !pointOnSegment2D(exitPt, tp.left, tp.right, portalEps) {
				continue
			}
			if polyContains(np, tVec) {
				nextLoc = cand
				reached = true
				break
			}
			nt, _, nok := polyRayExit2D(np, sVec, dir)
			if nok && nt > bestNextT {
				bestNextT = nt
				nextLoc = cand
			}
		}
		if reached || nextLoc.slot >= 0 {
			curLoc = nextLoc
			curT = tExit
			visited[curLoc] = true
			if visitedTiles != nil {
				visitedTiles[tm.slots[curLoc.slot].coord] = true
			}
			if reached {
				ns := tm.slots[curLoc.slot]
				ref := NavPolyRef{Tile: ns.coord, Poly: curLoc.poly, Salt: ns.salt}
				return TiledNavRaycastHit{Point: target, Poly: ref, T: 1, Hit: false}, false
			}
			continue
		}

		// Check corner grazing across neighboring polygons sharing exitPt.
		if bridge, ok := tm.findCornerBridgeTiled2D(curLoc, exitPt, sVec, tVec, dir, tExit, allowPoly, visited); ok {
			curLoc = bridge
			curT = tExit
			visited[curLoc] = true
			if visitedTiles != nil {
				visitedTiles[tm.slots[curLoc.slot].coord] = true
			}
			continue
		}

		return TiledNavRaycastHit{
			Point:  toPoint(exitPt),
			Normal: toPoint(exitNorm),
			Poly:   curRef,
			T:      float32(min(max(tExit, 0), 1)),
			Hit:    true,
		}, true
	}

	pt := sVec.add(dir.scale(curT))
	return TiledNavRaycastHit{Point: toPoint(pt), Poly: curRef, T: float32(min(max(curT, 0), 1)), Hit: true}, true
}

func (tm *TiledNavMesh) findCornerBridgeTiled2D(start polyLoc2D, exitPt, sVec, tVec, dir vec2, tExit float64, allowPoly func(*navPoly) bool, visited map[polyLoc2D]bool) (polyLoc2D, bool) {
	queue := []polyLoc2D{start}
	seen := map[polyLoc2D]bool{start: true}
	bestLoc := polyLoc2D{slot: -1, poly: -1}
	bestT := tExit + 1e-9
	for len(queue) > 0 && len(seen) < 32 {
		cur := queue[0]
		queue = queue[1:]
		slot := tm.slots[cur.slot]
		if slot == nil {
			continue
		}
		poly := &slot.polys[cur.poly]
		for _, pid := range poly.outPortals {
			tp := slot.portals[pid]
			if tp.offMesh {
				continue
			}
			nb := polyLoc2D{slot: tp.toSlot, poly: tp.toPoly}
			if seen[nb] {
				continue
			}
			ns := tm.slots[tp.toSlot]
			if ns == nil {
				continue
			}
			np := &ns.polys[tp.toPoly]
			if !allowPoly(np) {
				continue
			}
			if !pointOnSegment2D(exitPt, tp.left, tp.right, portalEps) {
				continue
			}
			seen[nb] = true
			if !visited[nb] {
				if polyContains(np, tVec) {
					return nb, true
				}
				nt, _, nok := polyRayExit2D(np, sVec, dir)
				if nok && nt > bestT {
					bestT = nt
					bestLoc = nb
				}
			}
			queue = append(queue, nb)
		}
	}
	return bestLoc, bestLoc.slot >= 0
}

// --- Sparse Portal-Graph A* Pathfinding across Tiles ---

type polyLoc2D struct {
	slot int32
	poly int32
}

func (tm *TiledNavMesh) findPolyLocsAt(v vec2, filter NavFilter, requireFiniteCost bool) []polyLoc2D {
	var coordBuf [4]NavTileCoord
	coords := tm.candidateTilesAtPoint(v, coordBuf[:0])
	var out []polyLoc2D
	var idxBuf [16]int
	for _, c := range coords {
		slot := tm.tiles[c]
		if slot == nil || len(slot.polys) == 0 {
			continue
		}
		idxs := findPolysAtInBVH2D(slot.polys, slot.bvhNodes, slot.bvhOrder, v, filter, requireFiniteCost, idxBuf[:0])
		for _, idx := range idxs {
			out = append(out, polyLoc2D{slot: slot.slotID, poly: int32(idx)})
		}
	}
	return out
}

// FindPath finds the shortest path inside tm from start to goal using DefaultNavFilter.
func (tm *TiledNavMesh) FindPath(start, goal core.Point) ([]core.Point, bool) {
	return tm.FindPathWithFilter(start, goal, DefaultNavFilter())
}

// FindPathWithFilter finds the minimum-cost path inside tm restricted to filter.
func (tm *TiledNavMesh) FindPathWithFilter(start, goal core.Point, filter NavFilter) ([]core.Point, bool) {
	res := tm.FindPathWithFilterAndOptions(start, goal, filter, PathOptions{})
	if !res.Found || res.Partial {
		return nil, false
	}
	return res.Path, true
}

// FindPathWithOptions finds a path using DefaultNavFilter and opts, returning the full TiledNavPathResult
// with tile footprint and version stamps for invalidation checking.
func (tm *TiledNavMesh) FindPathWithOptions(start, goal core.Point, opts PathOptions) TiledNavPathResult {
	return tm.FindPathWithFilterAndOptions(start, goal, DefaultNavFilter(), opts)
}

// FindCorridor finds the sequence of NavPolyRefs from start to goal using DefaultNavFilter.
func (tm *TiledNavMesh) FindCorridor(start, goal core.Point) ([]NavPolyRef, bool) {
	return tm.FindCorridorWithFilter(start, goal, DefaultNavFilter())
}

// FindCorridorWithFilter finds the sequence of NavPolyRefs from start to goal restricted to filter.
func (tm *TiledNavMesh) FindCorridorWithFilter(start, goal core.Point, filter NavFilter) ([]NavPolyRef, bool) {
	res := tm.FindPathWithFilterAndOptions(start, goal, filter, PathOptions{})
	if !res.Found || res.Partial {
		return nil, false
	}
	return res.Corridor, true
}

type tiledAStarNode2D struct {
	idx       int
	f         float64
	h         float64
	tieTile   NavTileCoord
	tiePoly   int32
	tieToTile NavTileCoord
	tieToPoly int32
	tieLink   int32
}

func tiledAStarLess2D(a, b tiledAStarNode2D) bool {
	if a.f != b.f {
		return a.f < b.f
	}
	if a.h != b.h {
		return a.h < b.h
	}
	if c := compareNavTileCoord2D(a.tieTile, b.tieTile); c != 0 {
		return c < 0
	}
	if a.tiePoly != b.tiePoly {
		return a.tiePoly < b.tiePoly
	}
	if c := compareNavTileCoord2D(a.tieToTile, b.tieToTile); c != 0 {
		return c < 0
	}
	if a.tieToPoly != b.tieToPoly {
		return a.tieToPoly < b.tieToPoly
	}
	return a.tieLink < b.tieLink
}

func tiledHeapPush2D(open *[]tiledAStarNode2D, heapPos []int, n tiledAStarNode2D) {
	pos := len(*open)
	*open = append(*open, n)
	heapPos[n.idx] = pos
	tiledSiftUp2D(*open, heapPos, pos)
}

func tiledHeapPop2D(open *[]tiledAStarNode2D, heapPos []int) tiledAStarNode2D {
	s := *open
	top := s[0]
	heapPos[top.idx] = -1
	last := len(s) - 1
	if last == 0 {
		*open = s[:0]
		return top
	}
	s[0] = s[last]
	heapPos[s[0].idx] = 0
	s = s[:last]
	*open = s
	tiledSiftDown2D(s, heapPos, 0)
	return top
}

func tiledSiftUp2D(s []tiledAStarNode2D, heapPos []int, i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !tiledAStarLess2D(s[i], s[p]) {
			break
		}
		s[i], s[p] = s[p], s[i]
		heapPos[s[i].idx] = i
		heapPos[s[p].idx] = p
		i = p
	}
}

func tiledSiftDown2D(s []tiledAStarNode2D, heapPos []int, i int) {
	n := len(s)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && tiledAStarLess2D(s[r], s[l]) {
			best = r
		}
		if !tiledAStarLess2D(s[best], s[i]) {
			break
		}
		s[i], s[best] = s[best], s[i]
		heapPos[s[i].idx] = i
		heapPos[s[best].idx] = best
		i = best
	}
}

// FindPathWithFilterAndOptions finds a minimum-cost path across tm restricted to filter and opts.
// Portal A* touches only visited portals (sparse state, independent of total mesh size) and
// runs Simple Stupid Funnel Algorithm string-pulling across tile borders so paths match
// an untiled NavMesh for the same geometry.
func (tm *TiledNavMesh) FindPathWithFilterAndOptions(start, goal core.Point, filter NavFilter, opts PathOptions) TiledNavPathResult {
	var res TiledNavPathResult
	if tm == nil || len(tm.tiles) == 0 {
		return res
	}
	sVec := toVec2(start)
	gVec := toVec2(goal)
	if !isFiniteVec2(sVec) || !isFiniteVec2(gVec) {
		return res
	}

	startLocs := tm.findPolyLocsAt(sVec, filter, true)
	if len(startLocs) == 0 {
		return res
	}
	goalLocs := tm.findPolyLocsAt(gVec, filter, true)
	hasGoal := len(goalLocs) > 0
	if !hasGoal && !opts.AllowPartial {
		return res
	}

	goalSet := make(map[polyLoc2D]bool, len(goalLocs))
	for _, gl := range goalLocs {
		goalSet[gl] = true
	}

	minCost := minFilterCost(filter)
	shortcuts, zeroH := tm.buildTiledNavShortcuts2D(gVec, minCost, filter)

	// Check direct single-polygon path.
	directPossible := false
	directCost := math.Inf(1)
	var directLoc polyLoc2D
	for _, sl := range startLocs {
		if goalSet[sl] {
			if vequal(sVec, gVec) {
				slot := tm.slots[sl.slot]
				ref := NavPolyRef{Tile: slot.coord, Poly: sl.poly, Salt: slot.salt}
				return TiledNavPathResult{
					Path:     []core.Point{start},
					Corridor: []NavPolyRef{ref},
					Tiles:    []NavTileVersion{{Coord: slot.coord, Version: tm.tileVersions[slot.coord]}},
					Found:    true,
				}
			}
			c := filter.AreaCost(tm.slots[sl.slot].polys[sl.poly].area)
			dc := dist(sVec, gVec) * c
			if dc < directCost {
				directCost = dc
				directPossible = true
				directLoc = sl
			}
		}
	}
	if directPossible && directCost <= heuristic2D(sVec, gVec, minCost, shortcuts, zeroH)+1e-9 {
		slot := tm.slots[directLoc.slot]
		ref := NavPolyRef{Tile: slot.coord, Poly: directLoc.poly, Salt: slot.salt}
		return TiledNavPathResult{
			Path:     []core.Point{start, goal},
			Corridor: []NavPolyRef{ref},
			Tiles:    []NavTileVersion{{Coord: slot.coord, Version: tm.tileVersions[slot.coord]}},
			Found:    true,
		}
	}

	// Sparse portal-graph A* state (index 0 is the virtual goal node) with
	// per-node funnel tracking so subdivided tile borders never distort path costs.
	type visitedPortal struct {
		slotID    int32
		portalIdx int32
		parent    int
		gScore    float64
		fScore    float64
		entryPt   vec2
		apex      vec2
		apexG     float64
		fLeft     vec2
		fRight    vec2
		areaCost  float64
		closed    bool
	}
	nodes := []visitedPortal{{slotID: -1, portalIdx: -1, parent: -1, gScore: math.Inf(1), fScore: math.Inf(1)}}
	heapPos := []int{-1}
	portalToIdx := make(map[uint64]int, 64)
	open := make([]tiledAStarNode2D, 0, 64)

	portalKey := func(slotID, pid int32) uint64 {
		return (uint64(uint32(slotID)) << 32) | uint64(uint32(pid))
	}

	pushOrUpdate := func(slotID, pid int32, g, h float64, parent int, pt, apex, fLeft, fRight vec2, apexG, areaCost float64, tieTile NavTileCoord, tiePoly int32, tieToTile NavTileCoord, tieToPoly, tieLink int32) {
		f := g + h
		var idx int
		if slotID < 0 {
			idx = 0
		} else {
			key := portalKey(slotID, pid)
			var exists bool
			idx, exists = portalToIdx[key]
			if !exists {
				idx = len(nodes)
				portalToIdx[key] = idx
				nodes = append(nodes, visitedPortal{
					slotID:    slotID,
					portalIdx: pid,
					parent:    parent,
					gScore:    g,
					fScore:    f,
					entryPt:   pt,
					apex:      apex,
					apexG:     apexG,
					fLeft:     fLeft,
					fRight:    fRight,
					areaCost:  areaCost,
				})
				heapPos = append(heapPos, -1)
				tiledHeapPush2D(&open, heapPos, tiledAStarNode2D{
					idx: idx, f: f, h: h,
					tieTile: tieTile, tiePoly: tiePoly, tieToTile: tieToTile, tieToPoly: tieToPoly, tieLink: tieLink,
				})
				return
			}
		}
		if f >= nodes[idx].fScore-1e-9 {
			return
		}
		nodes[idx].gScore = g
		nodes[idx].fScore = f
		nodes[idx].parent = parent
		nodes[idx].entryPt = pt
		nodes[idx].apex = apex
		nodes[idx].apexG = apexG
		nodes[idx].fLeft = fLeft
		nodes[idx].fRight = fRight
		nodes[idx].areaCost = areaCost
		nodes[idx].closed = false
		pos := heapPos[idx]
		if pos >= 0 {
			open[pos].f = f
			open[pos].h = h
			tiledSiftUp2D(open, heapPos, pos)
			return
		}
		tiledHeapPush2D(&open, heapPos, tiledAStarNode2D{
			idx: idx, f: f, h: h,
			tieTile: tieTile, tiePoly: tiePoly, tieToTile: tieToTile, tieToPoly: tieToPoly, tieLink: tieLink,
		})
	}

	hasBest := false
	bestNodeIdx := -1
	bestStartLoc := startLocs[0]
	var bestPt vec2
	var bestH, bestG float64
	if opts.AllowPartial {
		for _, sl := range startLocs {
			p := &tm.slots[sl.slot].polys[sl.poly]
			cp := closestPointInPoly(p, gVec)
			h := dist(cp, gVec)
			g := dist(sVec, cp) * filter.AreaCost(p.area)
			if !hasBest || h < bestH || (h == bestH && g < bestG) {
				hasBest = true
				bestNodeIdx = -1
				bestStartLoc = sl
				bestPt = cp
				bestH = h
				bestG = g
			}
		}
	}

	maxCoord := NavTileCoord{X: math.MaxInt32, Y: math.MaxInt32}
	if directPossible {
		pushOrUpdate(-1, -1, directCost, 0, -1, gVec, sVec, gVec, gVec, 0, minCost, maxCoord, math.MaxInt32, maxCoord, math.MaxInt32, math.MaxInt32)
	}

	for _, sl := range startLocs {
		slot := tm.slots[sl.slot]
		sp := &slot.polys[sl.poly]
		spCost := filter.AreaCost(sp.area)
		for _, pid := range sp.outPortals {
			tp := slot.portals[pid]
			dst := tm.slots[tp.toSlot]
			if dst == nil {
				continue
			}
			dp := &dst.polys[tp.toPoly]
			if !filter.Allows(dp.area, dp.flags) {
				continue
			}
			dc := filter.AreaCost(dp.area)
			if dc <= 0 || math.IsNaN(dc) || math.IsInf(dc, 0) {
				continue
			}
			var g, h, apexG, areaCost float64
			var exitPt, apex, fLeft, fRight vec2
			if tp.offMesh {
				linkCost, ok := tm.offMeshPortalCost2D(tp, filter)
				if !ok {
					continue
				}
				g = float64(dist(sVec, tp.left)*spCost) + linkCost
				exitPt = tp.endPos
				apex = tp.endPos
				fLeft = tp.endPos
				fRight = tp.endPos
				apexG = g
				areaCost = dc
				h = heuristic2D(exitPt, gVec, minCost, shortcuts, zeroH)
			} else {
				apex = sVec
				fLeft = tp.left
				fRight = tp.right
				apexG = 0
				areaCost = spCost
				exitPt, g, h = bestPortalPointAndHeuristic2D(sVec, tp.left, tp.right, gVec, tp.left, tp.right, false, 0, 0, spCost, minCost, shortcuts, zeroH)
			}
			pushOrUpdate(sl.slot, int32(pid), g, h, -1, exitPt, apex, fLeft, fRight, apexG, areaCost, slot.coord, sl.poly, dst.coord, tp.toPoly, tp.offMeshIdx)
		}
	}

	foundComplete := false
	expansions := 0
	exhausted := false

	for len(open) > 0 {
		if opts.MaxExpansions > 0 && expansions >= opts.MaxExpansions {
			exhausted = true
			break
		}
		cur := tiledHeapPop2D(&open, heapPos)
		expansions++
		if cur.idx == 0 {
			foundComplete = true
			break
		}
		nodes[cur.idx].closed = true
		vp := nodes[cur.idx]
		fromSlot := tm.slots[vp.slotID]
		tp := fromSlot.portals[vp.portalIdx]
		toSlot := tm.slots[tp.toSlot]
		toPoly := &toSlot.polys[tp.toPoly]
		toCost := filter.AreaCost(toPoly.area)

		if opts.AllowPartial {
			cp := closestPointInPoly(toPoly, gVec)
			h := dist(cp, gVec)
			var g float64
			if math.Abs(toCost-vp.areaCost) <= 1e-12 {
				g = funnelPointDist2D(vp.apex, vp.fLeft, vp.fRight, cp, vp.apexG, toCost)
			} else {
				g = vp.gScore + dist(vp.entryPt, cp)*toCost
			}
			if !hasBest || h < bestH || (h == bestH && g < bestG) {
				hasBest = true
				bestNodeIdx = cur.idx
				bestPt = cp
				bestH = h
				bestG = g
			}
		}

		toLoc := polyLoc2D{slot: tp.toSlot, poly: tp.toPoly}
		if goalSet[toLoc] {
			var ng float64
			if math.Abs(toCost-vp.areaCost) <= 1e-12 {
				ng = funnelPointDist2D(vp.apex, vp.fLeft, vp.fRight, gVec, vp.apexG, toCost)
			} else {
				ng = vp.gScore + dist(vp.entryPt, gVec)*toCost
			}
			pushOrUpdate(-1, -1, ng, 0, cur.idx, gVec, vp.apex, vp.fLeft, vp.fRight, vp.apexG, toCost, maxCoord, math.MaxInt32, maxCoord, math.MaxInt32, math.MaxInt32)
		}

		for _, npid := range toPoly.outPortals {
			ntp := toSlot.portals[npid]
			if !ntp.offMesh && ntp.toSlot == vp.slotID && ntp.toPoly == tp.fromPoly {
				continue
			}
			ndst := tm.slots[ntp.toSlot]
			if ndst == nil {
				continue
			}
			ndp := &ndst.polys[ntp.toPoly]
			if !filter.Allows(ndp.area, ndp.flags) {
				continue
			}
			ndc := filter.AreaCost(ndp.area)
			if ndc <= 0 || math.IsNaN(ndc) || math.IsInf(ndc, 0) {
				continue
			}
			var ng, h, newApexG, nextAreaCost float64
			var exitPt, newApex, newLeft, newRight vec2
			if ntp.offMesh {
				linkCost, ok := tm.offMeshPortalCost2D(ntp, filter)
				if !ok {
					continue
				}
				var walkToLink float64
				if math.Abs(toCost-vp.areaCost) <= 1e-12 {
					walkToLink = funnelPointDist2D(vp.apex, vp.fLeft, vp.fRight, ntp.left, vp.apexG, toCost)
				} else {
					walkToLink = vp.gScore + dist(vp.entryPt, ntp.left)*toCost
				}
				ng = walkToLink + linkCost
				exitPt = ntp.endPos
				newApex = ntp.endPos
				newLeft = ntp.endPos
				newRight = ntp.endPos
				newApexG = ng
				nextAreaCost = ndc
				h = heuristic2D(exitPt, gVec, minCost, shortcuts, zeroH)
			} else {
				if math.Abs(toCost-vp.areaCost) <= 1e-12 {
					newApex, newLeft, newRight, newApexG = advanceFunnel2D(vp.apex, vp.fLeft, vp.fRight, ntp.left, ntp.right, vp.apexG, toCost)
					exitPt, ng, h = bestPortalPointAndHeuristic2D(newApex, newLeft, newRight, gVec, ntp.left, ntp.right, true, newApexG, vp.gScore, toCost, minCost, shortcuts, zeroH)
				} else {
					newApex = vp.entryPt
					newApexG = vp.gScore
					newLeft = ntp.left
					newRight = ntp.right
					exitPt, ng, h = bestPortalPointAndHeuristic2D(vp.entryPt, ntp.left, ntp.right, gVec, ntp.left, ntp.right, false, vp.gScore, vp.gScore, toCost, minCost, shortcuts, zeroH)
				}
				nextAreaCost = toCost
			}
			pushOrUpdate(tp.toSlot, int32(npid), ng, h, cur.idx, exitPt, newApex, newLeft, newRight, newApexG, nextAreaCost, toSlot.coord, tp.toPoly, ndst.coord, ntp.toPoly, ntp.offMeshIdx)
		}
	}

	res.Expansions = expansions
	res.Exhausted = exhausted

	if foundComplete {
		res.Found = true
		if nodes[0].parent == -1 {
			slot := tm.slots[directLoc.slot]
			ref := NavPolyRef{Tile: slot.coord, Poly: directLoc.poly, Salt: slot.salt}
			res.Corridor = []NavPolyRef{ref}
			res.Tiles = []NavTileVersion{{Coord: slot.coord, Version: tm.tileVersions[slot.coord]}}
			if vequal(sVec, gVec) {
				res.Path = []core.Point{start}
			} else {
				res.Path = []core.Point{start, goal}
			}
			return res
		}
		var chain []tiledPortal2D
		var corridor []NavPolyRef
		for id := nodes[0].parent; id != -1; id = nodes[id].parent {
			vp := nodes[id]
			chain = append(chain, tm.slots[vp.slotID].portals[vp.portalIdx])
		}
		slices.Reverse(chain)
		// Reconstruct first polygon from the first chain node's owning slot.
		firstNode := nodes[0].parent
		for nodes[firstNode].parent != -1 {
			firstNode = nodes[firstNode].parent
		}
		firstSlot := tm.slots[nodes[firstNode].slotID]
		corridor = make([]NavPolyRef, len(chain)+1)
		corridor[0] = NavPolyRef{Tile: firstSlot.coord, Poly: chain[0].fromPoly, Salt: firstSlot.salt}
		for i, tp := range chain {
			dst := tm.slots[tp.toSlot]
			corridor[i+1] = NavPolyRef{Tile: dst.coord, Poly: tp.toPoly, Salt: dst.salt}
		}
		res.Corridor = corridor
		tightPath, pathLinks, extraTiles := tm.finalizeTiledCorridor2D(sVec, gVec, chain, corridor, filter)
		res.Path = tightPath
		res.Links = pathLinks
		res.Tiles = tm.collectTileVersions(corridor, extraTiles)
		return res
	}

	if opts.AllowPartial && hasBest {
		res.Found = true
		res.Partial = true
		if bestNodeIdx < 0 {
			slot := tm.slots[bestStartLoc.slot]
			ref := NavPolyRef{Tile: slot.coord, Poly: bestStartLoc.poly, Salt: slot.salt}
			res.Corridor = []NavPolyRef{ref}
			res.Tiles = []NavTileVersion{{Coord: slot.coord, Version: tm.tileVersions[slot.coord]}}
			if vequal(sVec, bestPt) {
				res.Path = []core.Point{start}
			} else {
				res.Path = []core.Point{start, toPoint(bestPt)}
			}
			return res
		}
		var chain []tiledPortal2D
		for id := bestNodeIdx; id != -1; id = nodes[id].parent {
			vp := nodes[id]
			chain = append(chain, tm.slots[vp.slotID].portals[vp.portalIdx])
		}
		slices.Reverse(chain)
		firstNode := bestNodeIdx
		for nodes[firstNode].parent != -1 {
			firstNode = nodes[firstNode].parent
		}
		firstSlot := tm.slots[nodes[firstNode].slotID]
		corridor := make([]NavPolyRef, len(chain)+1)
		corridor[0] = NavPolyRef{Tile: firstSlot.coord, Poly: chain[0].fromPoly, Salt: firstSlot.salt}
		for i, tp := range chain {
			dst := tm.slots[tp.toSlot]
			corridor[i+1] = NavPolyRef{Tile: dst.coord, Poly: tp.toPoly, Salt: dst.salt}
		}
		res.Corridor = corridor
		tightPath, pathLinks, extraTiles := tm.finalizeTiledCorridor2D(sVec, bestPt, chain, corridor, filter)
		res.Path = tightPath
		res.Links = pathLinks
		res.Tiles = tm.collectTileVersions(corridor, extraTiles)
		return res
	}

	return res
}

func (tm *TiledNavMesh) finalizeTiledCorridor2D(start, end vec2, chain []tiledPortal2D, corridor []NavPolyRef, filter NavFilter) ([]core.Point, []TiledNavPathLink, []NavTileCoord) {
	hasOffMesh := false
	for _, tp := range chain {
		if tp.offMesh {
			hasOffMesh = true
			break
		}
	}
	if !hasOffMesh {
		tightPath, extraTiles := tm.tightenTiledPath2D(stringPullTiled2D(start, end, chain), corridor, filter)
		return tightPath, nil, extraTiles
	}

	var pts []core.Point
	var links []TiledNavPathLink
	var extraTiles []NavTileCoord
	segStart := start
	segBegin := 0
	for i, tp := range chain {
		if !tp.offMesh {
			continue
		}
		segPts, segExtra := tm.tightenTiledPath2D(stringPullTiled2D(segStart, tp.left, chain[segBegin:i]), corridor[segBegin:i+1], filter)
		extraTiles = append(extraTiles, segExtra...)
		for _, pt := range segPts {
			if len(pts) == 0 || !vequal(toVec2(pts[len(pts)-1]), toVec2(pt)) {
				pts = append(pts, pt)
			}
		}
		linkStartPt := toPoint(tp.left)
		if len(pts) == 0 {
			pts = append(pts, linkStartPt)
		} else if !vequal(toVec2(pts[len(pts)-1]), tp.left) {
			pts = append(pts, linkStartPt)
		} else {
			pts[len(pts)-1] = linkStartPt
		}
		startIdx := len(pts) - 1
		linkEndPt := toPoint(tp.endPos)
		pts = append(pts, linkEndPt)
		endIdx := len(pts) - 1
		radius, area, _, userData := tm.offMeshMeta(tp)
		links = append(links, TiledNavPathLink{
			StartIndex: startIdx,
			EndIndex:   endIdx,
			FromPoly:   corridor[i],
			ToPoly:     corridor[i+1],
			Start:      linkStartPt,
			End:        linkEndPt,
			Radius:     radius,
			Area:       area,
			Flags:      tp.flags,
			LinkID:     tp.linkID,
			UserData:   userData,
		})
		segStart = tp.endPos
		segBegin = i + 1
	}
	tailPts, tailExtra := tm.tightenTiledPath2D(stringPullTiled2D(segStart, end, chain[segBegin:]), corridor[segBegin:], filter)
	extraTiles = append(extraTiles, tailExtra...)
	for _, pt := range tailPts {
		if len(pts) == 0 || !vequal(toVec2(pts[len(pts)-1]), toVec2(pt)) {
			pts = append(pts, pt)
		}
	}
	return pts, links, extraTiles
}

func (tm *TiledNavMesh) collectTileVersions(corridor []NavPolyRef, extra []NavTileCoord) []NavTileVersion {
	if len(corridor) == 0 && len(extra) == 0 {
		return nil
	}
	coords := make([]NavTileCoord, 0, len(corridor)+len(extra))
	for _, ref := range corridor {
		coords = append(coords, ref.Tile)
	}
	coords = append(coords, extra...)
	slices.SortFunc(coords, compareNavTileCoord2D)
	coords = slices.Compact(coords)
	out := make([]NavTileVersion, len(coords))
	for i, c := range coords {
		out[i] = NavTileVersion{Coord: c, Version: tm.tileVersions[c]}
	}
	return out
}

func advanceFunnel2D(apex, fLeft, fRight, nLeft, nRight vec2, apexG, cost float64) (newApex, newLeft, newRight vec2, newApexG float64) {
	newApex, newLeft, newRight, newApexG = apex, fLeft, fRight, apexG
	if triArea2(newApex, newRight, nRight) >= 0 {
		if vequal(newApex, newRight) || vequal(newApex, newLeft) || triArea2(newApex, newLeft, nRight) < 0 {
			newRight = nRight
		} else {
			newApexG += dist(newApex, newLeft) * cost
			newApex = newLeft
			newLeft = nLeft
			newRight = nRight
			return
		}
	}
	if triArea2(newApex, newLeft, nLeft) <= 0 {
		if vequal(newApex, newLeft) || vequal(newApex, newRight) || triArea2(newApex, newRight, nLeft) > 0 {
			newLeft = nLeft
		} else {
			newApexG += dist(newApex, newRight) * cost
			newApex = newRight
			newLeft = nLeft
			newRight = nRight
			return
		}
	}
	return
}

func funnelPortalPoint2D(apex, fLeft, fRight, goal, pLeft, pRight vec2) vec2 {
	if !vequal(apex, fLeft) && triArea2(apex, fLeft, goal) > 0 {
		return bestPortalPoint(fLeft, goal, pLeft, pRight)
	}
	if !vequal(apex, fRight) && triArea2(apex, fRight, goal) < 0 {
		return bestPortalPoint(fRight, goal, pLeft, pRight)
	}
	return bestPortalPoint(apex, goal, pLeft, pRight)
}

func funnelPointDist2D(apex, fLeft, fRight, target vec2, apexG, cost float64) float64 {
	if !vequal(apex, fLeft) && triArea2(apex, fLeft, target) > 0 {
		return apexG + (dist(apex, fLeft)+dist(fLeft, target))*cost
	}
	if !vequal(apex, fRight) && triArea2(apex, fRight, target) < 0 {
		return apexG + (dist(apex, fRight)+dist(fRight, target))*cost
	}
	return apexG + dist(apex, target)*cost
}

// bestPortalPointAndHeuristic2D chooses the crossing point on [pLeft, pRight] that minimizes
// g(pt) + h(pt) across the direct goal and any admissible off-mesh shortcut targets, so crossing
// tile borders on the way to a shortcut link never overestimates f-score.
func bestPortalPointAndHeuristic2D(apex, fLeft, fRight, gVec, pLeft, pRight vec2, useFunnel bool, apexG, baseG, toCost, minCost float64, shortcuts []navShortcut2D, zeroH bool) (exitPt vec2, g, h float64) {
	evalTarget := func(target vec2) (vec2, float64, float64) {
		var pt vec2
		var cg float64
		if useFunnel {
			pt = funnelPortalPoint2D(apex, fLeft, fRight, target, pLeft, pRight)
			cg = funnelPointDist2D(apex, fLeft, fRight, pt, apexG, toCost)
		} else {
			pt = bestPortalPoint(apex, target, pLeft, pRight)
			cg = baseG + dist(apex, pt)*toCost
		}
		ch := heuristic2D(pt, gVec, minCost, shortcuts, zeroH)
		return pt, cg, ch
	}
	exitPt, g, h = evalTarget(gVec)
	if len(shortcuts) == 0 || zeroH {
		return exitPt, g, h
	}
	bestF := g + h
	for i := range shortcuts {
		pt, cg, ch := evalTarget(shortcuts[i].start)
		if cf := cg + ch; cf < bestF-1e-12 {
			bestF = cf
			exitPt, g, h = pt, cg, ch
		}
	}
	return exitPt, g, h
}

// tightenTiledPath2D removes any artificial bends at interior tile-grid vertices on uniform-cost
// corridors by checking mesh-surface line of sight and pulling bends onto true obstacle corners.
// It preserves all NavFilter constraints and never shortcuts across higher-cost areas.
func (tm *TiledNavMesh) tightenTiledPath2D(pts []core.Point, corridor []NavPolyRef, filter NavFilter) ([]core.Point, []NavTileCoord) {
	if len(pts) <= 2 || len(corridor) == 0 {
		return pts, nil
	}
	// Only tighten when the corridor has uniform area cost (otherwise bends may follow cheap roads).
	refCost := filter.AreaCost(tm.PolygonArea(corridor[0]))
	for _, ref := range corridor[1:] {
		if math.Abs(filter.AreaCost(tm.PolygonArea(ref))-refCost) > 1e-9 {
			return pts, nil
		}
	}
	// Restrict raycasts to polygons costing no more than refCost (covering all areas, including implicit cost 1.0 when refCost < 1).
	maxCost := refCost + 1e-9

	out := make([]vec2, len(pts))
	for i, p := range pts {
		out[i] = toVec2(p)
	}

	modified := false
	for pass := 0; pass < 4 && len(out) > 2; pass++ {
		changed := false
		// 1. Greedy LOS shortcutting: skip any intermediate waypoints with unobstructed surface sight.
		i := 0
		for i+2 < len(out) {
			bestJ := i + 1
			for j := len(out) - 1; j > i+1; j-- {
				if _, hit := tm.raycastWithFilterInternal(toPoint(out[i]), toPoint(out[j]), filter, maxCost, nil); !hit {
					bestJ = j
					break
				}
			}
			if bestJ > i+1 {
				out = append(out[:i+1], out[bestJ:]...)
				changed = true
				modified = true
			}
			i++
		}

		// 2. Pull bends stuck on an interior tile-grid corner onto the true obstructing obstacle vertex.
		for k := 1; k+1 < len(out); k++ {
			a, b, c := out[k-1], out[k], out[k+1]
			hit, blocked := tm.raycastWithFilterInternal(toPoint(a), toPoint(c), filter, maxCost, nil)
			if !blocked {
				out = append(out[:k], out[k+1:]...)
				changed = true
				modified = true
				k--
				continue
			}
			_, hitPoly := tm.resolveRef(hit.Poly)
			if hitPoly == nil {
				continue
			}
			curLen := dist(a, b) + dist(b, c)
			hitPt := toVec2(hit.Point)
			bestCand := b
			bestLen := curLen - 1e-5
			vs := hitPoly.verts
			for e := range len(vs) {
				v0, v1 := vs[e], vs[(e+1)%len(vs)]
				if !pointOnSegment2D(hitPt, v0, v1, portalEps*2) {
					continue
				}
				for _, cand := range [2]vec2{v0, v1} {
					candLen := dist(a, cand) + dist(cand, c)
					if candLen >= bestLen {
						continue
					}
					if _, h1 := tm.raycastWithFilterInternal(toPoint(a), toPoint(cand), filter, maxCost, nil); h1 {
						continue
					}
					if _, h2 := tm.raycastWithFilterInternal(toPoint(cand), toPoint(c), filter, maxCost, nil); h2 {
						continue
					}
					bestLen = candLen
					bestCand = cand
				}
			}
			if !vequal(bestCand, b) {
				out[k] = bestCand
				changed = true
				modified = true
			}
		}
		if !changed {
			break
		}
	}

	clean := simplifyCollinear(out)
	res := make([]core.Point, len(clean))
	for i, v := range clean {
		res[i] = toPoint(v)
	}
	var extraTiles []NavTileCoord
	if modified && len(res) >= 2 {
		visited := make(map[NavTileCoord]bool, len(corridor))
		for i := 0; i+1 < len(res); i++ {
			tm.raycastWithFilterInternal(res[i], res[i+1], filter, maxCost, visited)
		}
		if len(visited) > 0 {
			extraTiles = make([]NavTileCoord, 0, len(visited))
			for c := range visited {
				extraTiles = append(extraTiles, c)
			}
		}
	}
	return res, extraTiles
}

func stringPullTiled2D(start, goal vec2, chain []tiledPortal2D) []core.Point {
	type funnelPortal struct {
		left, right vec2
	}
	portals := make([]funnelPortal, 0, len(chain)+2)
	portals = append(portals, funnelPortal{left: start, right: start})
	for _, tp := range chain {
		portals = append(portals, funnelPortal{left: tp.left, right: tp.right})
	}
	portals = append(portals, funnelPortal{left: goal, right: goal})

	path := []vec2{start}
	portalApex := start
	portalLeft := start
	portalRight := start
	apexIndex := 0
	leftIndex := 0
	rightIndex := 0

	for i := 1; i < len(portals); i++ {
		left := portals[i].left
		right := portals[i].right

		if triArea2(portalApex, portalRight, right) >= 0 {
			if vequal(portalApex, portalRight) || vequal(portalApex, portalLeft) || triArea2(portalApex, portalLeft, right) < 0 {
				portalRight = right
				rightIndex = i
			} else {
				if !vequal(path[len(path)-1], portalLeft) {
					path = append(path, portalLeft)
				}
				portalApex = portalLeft
				apexIndex = leftIndex
				portalLeft = portalApex
				portalRight = portalApex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}

		if triArea2(portalApex, portalLeft, left) <= 0 {
			if vequal(portalApex, portalLeft) || vequal(portalApex, portalRight) || triArea2(portalApex, portalRight, left) > 0 {
				portalLeft = left
				leftIndex = i
			} else {
				if !vequal(path[len(path)-1], portalRight) {
					path = append(path, portalRight)
				}
				portalApex = portalRight
				apexIndex = rightIndex
				portalLeft = portalApex
				portalRight = portalApex
				leftIndex = apexIndex
				rightIndex = apexIndex
				i = apexIndex
				continue
			}
		}
	}

	if !vequal(path[len(path)-1], goal) {
		path = append(path, goal)
	}
	clean := simplifyCollinear(path)
	out := make([]core.Point, len(clean))
	for i, v := range clean {
		out[i] = toPoint(v)
	}
	return out
}
