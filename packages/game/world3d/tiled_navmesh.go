package world3d

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

const defaultNavTileSize3D float32 = 64

// NavTileCoord identifies a fixed-size tile in a 3D TiledNavMesh.
// It aliases ChunkCoord so 3D navmesh tiles align 1:1 with world3d.ChunkStreamer chunks.
type NavTileCoord = ChunkCoord

// NavPolyRef uniquely identifies a walkable convex 3D polygon inside a TiledNavMesh.
// Salt increments whenever the owning tile is replaced, rebuilt, or removed so stale
// references are detected in O(1).
type NavPolyRef struct {
	Tile ChunkCoord
	Poly int32
	Salt uint32
}

// Valid reports whether r holds a non-zero tile salt and non-negative polygon index.
func (r NavPolyRef) Valid() bool {
	return r.Salt != 0 && r.Poly >= 0
}

// NavTileVersion records a tile coordinate together with its revision number when a path was computed.
type NavTileVersion struct {
	Coord   ChunkCoord
	Version uint64
}

// NavTileChangeKind classifies why a tile in a 3D TiledNavMesh changed.
type NavTileChangeKind = ai.NavTileChangeKind

const (
	NavTileAdded   = ai.NavTileAdded
	NavTileUpdated = ai.NavTileUpdated
	NavTileRemoved = ai.NavTileRemoved
)

// NavTileChange describes a single 3D tile mutation event delivered to tile listeners and path watchers.
type NavTileChange struct {
	Coord   ChunkCoord
	Kind    NavTileChangeKind
	Version uint64
}

// NavListenerID identifies a tile-change listener registered on a 3D TiledNavMesh.
type NavListenerID uint64

// NavPathWatchID identifies a path-invalidation watcher registered on a 3D TiledNavMesh.
type NavPathWatchID uint64

// NavObstacleID identifies a dynamic obstacle registered on a 3D TiledNavMesh.
type NavObstacleID uint64

// NavObstacleKind selects the geometric shape of a 3D NavObstacle.
type NavObstacleKind uint8

const (
	// NavObstacleBox is an axis-aligned or yaw-rotated 3D box (Center, HalfExtents, Yaw).
	NavObstacleBox NavObstacleKind = iota
	// NavObstacleCylinder is a vertical 3D cylinder (Center, Radius, Height, Segments).
	// When Height == 0, the cylinder affects all vertical levels.
	NavObstacleCylinder
	// NavObstaclePolygon is a vertically extruded 3D polygon prism (Vertices in XZ, MinY, MaxY).
	// When MinY == MaxY, the polygon affects all vertical levels.
	NavObstaclePolygon
)

// NavObstacleMode selects how a dynamic obstacle modifies walkable polygons in touched tiles.
type NavObstacleMode = ai.NavObstacleMode

const (
	NavObstacleCarve = ai.NavObstacleCarve
	NavObstacleMark  = ai.NavObstacleMark
)

// NavObstacle describes a temporary runtime 3D obstacle applied to a TiledNavMesh.
type NavObstacle struct {
	Kind        NavObstacleKind
	Mode        NavObstacleMode
	Center      math3d.Vec3   // Box and Cylinder center
	HalfExtents math3d.Vec3   // Box half-extents along X, Y, Z (each > 0)
	Yaw         float32       // Optional Box rotation around +Y in radians
	Radius      float32       // Cylinder radius (> 0)
	Height      float32       // Cylinder total height along Y (0 = unbounded vertically)
	Segments    int           // Cylinder approximation segments (default 12, clamped to [6, 64])
	Vertices    []math3d.Vec3 // Polygon footprint vertices in XZ (at least 3; convex or concave)
	MinY        float32       // Polygon vertical minimum (MinY == MaxY means unbounded vertically)
	MaxY        float32       // Polygon vertical maximum
	Margin      float32       // Optional horizontal outward inflation in world units (>= 0)
	Area        NavArea       // Applied in NavObstacleMark mode when non-zero
	Flags       NavFlags      // Applied in NavObstacleMark mode (defaults to NavFlagDisabled when both Area and Flags are 0)
}

// NavBoxObstacle constructs an axis-aligned 3D box obstacle spanning [minPt, maxPt].
func NavBoxObstacle(minPt, maxPt math3d.Vec3) NavObstacle {
	lo := minPt.Min(maxPt)
	hi := minPt.Max(maxPt)
	return NavObstacle{
		Kind:        NavObstacleBox,
		Mode:        NavObstacleCarve,
		Center:      lo.Add(hi).Scale(0.5),
		HalfExtents: hi.Sub(lo).Scale(0.5),
	}
}

// NavOrientedBoxObstacle constructs a yaw-rotated 3D box obstacle centered at center with halfExtents.
func NavOrientedBoxObstacle(center, halfExtents math3d.Vec3, yawRad float32) NavObstacle {
	return NavObstacle{
		Kind:        NavObstacleBox,
		Mode:        NavObstacleCarve,
		Center:      center,
		HalfExtents: halfExtents,
		Yaw:         yawRad,
	}
}

// NavCylinderObstacle constructs a vertical 3D cylinder obstacle centered at center with radius and total height.
// Passing height == 0 makes the cylinder affect all vertical levels.
func NavCylinderObstacle(center math3d.Vec3, radius, height float32) NavObstacle {
	return NavObstacle{
		Kind:   NavObstacleCylinder,
		Mode:   NavObstacleCarve,
		Center: center,
		Radius: radius,
		Height: height,
	}
}

// NavPolygonObstacle constructs a vertically extruded 3D polygon obstacle (convex or concave in XZ)
// spanning [minY, maxY]. Passing minY == maxY makes it affect all vertical levels.
func NavPolygonObstacle(verts []math3d.Vec3, minY, maxY float32) NavObstacle {
	cp := make([]math3d.Vec3, len(verts))
	copy(cp, verts)
	return NavObstacle{
		Kind:     NavObstaclePolygon,
		Mode:     NavObstacleCarve,
		Vertices: cp,
		MinY:     minY,
		MaxY:     maxY,
	}
}

type basePoly3D struct {
	xz    []vec2XZ
	plane plane3D
	minY  float32
	maxY  float32
	area  NavArea
	flags NavFlags
}

// NavTile is an independently baked 3D navigation tile covering a fixed XZ square region
// [minX, maxX] x [minZ, maxZ]. A NavTile may be baked concurrently on worker goroutines
// with BakeNavTile and installed into a TiledNavMesh with AddTile, ReplaceTile, or SetTile.
type NavTile struct {
	coord    ChunkCoord
	tileSize float64
	minX     float64
	minZ     float64
	maxX     float64
	maxZ     float64
	base     []basePoly3D
}

// Coord returns the chunk/tile coordinate this tile was baked for.
func (t *NavTile) Coord() ChunkCoord {
	if t == nil {
		return ChunkCoord{}
	}
	return t.coord
}

// BasePolygonCount returns the number of base walkable 3D polygons baked into this tile.
func (t *NavTile) BasePolygonCount() int {
	if t == nil {
		return 0
	}
	return len(t.base)
}

// BoundsXZ returns the world-space XZ bounds of this tile (with Y spanning the tile's polygons, or 0 if empty).
func (t *NavTile) Bounds() math3d.AABB {
	if t == nil {
		return math3d.AABB{}
	}
	minY, maxY := float32(0), float32(0)
	if len(t.base) > 0 {
		minY, maxY = t.base[0].minY, t.base[0].maxY
		for _, bp := range t.base[1:] {
			minY = min(minY, bp.minY)
			maxY = max(maxY, bp.maxY)
		}
	}
	return math3d.AABB{
		Min: math3d.Vec3{X: float32(t.minX), Y: minY, Z: float32(t.minZ)},
		Max: math3d.Vec3{X: float32(t.maxX), Y: maxY, Z: float32(t.maxZ)},
	}
}

// BakeNavTile bakes an independent 3D NavTile for coord from convex walkable 3D polygons,
// filtering slopes steeper than maxSlopeRad (default pi/4 when < 0) and clipping every polygon
// cleanly to the tile's exact XZ bounds while preserving exact 3D plane heights.
// BakeNavTile is pure and safe to call concurrently from ChunkStreamer worker goroutines.
func BakeNavTile(coord ChunkCoord, tileSize float32, origin math3d.Vec3, polys []NavPolygonInput, maxSlopeRad float32) *NavTile {
	ts := float64(tileSize)
	if !isFinite32(tileSize) || ts <= 0 {
		ts = float64(defaultNavTileSize3D)
	}
	ox, oz := float64(origin.X), float64(origin.Z)
	if math.IsNaN(ox) || math.IsInf(ox, 0) {
		ox = 0
	}
	if math.IsNaN(oz) || math.IsInf(oz, 0) {
		oz = 0
	}
	if maxSlopeRad < 0 {
		maxSlopeRad = math.Pi / 4
	}
	minNormalY := float32(math.Cos(float64(maxSlopeRad))) - 1e-5

	minX := ox + float64(coord.X)*ts
	maxX := ox + float64(coord.X+1)*ts
	minZ := oz + float64(coord.Z)*ts
	maxZ := oz + float64(coord.Z+1)*ts

	tile := &NavTile{
		coord:    coord,
		tileSize: ts,
		minX:     minX,
		minZ:     minZ,
		maxX:     maxX,
		maxZ:     maxZ,
	}

	for _, in := range polys {
		if len(in.Vertices) < 3 {
			continue
		}
		verts := make([]math3d.Vec3, len(in.Vertices))
		valid := true
		pMinX, pMinZ := math.Inf(1), math.Inf(1)
		pMaxX, pMaxZ := math.Inf(-1), math.Inf(-1)
		for i, v := range in.Vertices {
			if !isFiniteVec3(v) {
				valid = false
				break
			}
			verts[i] = v
			pMinX = min(pMinX, float64(v.X))
			pMinZ = min(pMinZ, float64(v.Z))
			pMaxX = max(pMaxX, float64(v.X))
			pMaxZ = max(pMaxZ, float64(v.Z))
		}
		if !valid || pMaxX <= minX+tiledSplitEps3D || pMinX >= maxX-tiledSplitEps3D ||
			pMaxZ <= minZ+tiledSplitEps3D || pMinZ >= maxZ-tiledSplitEps3D {
			continue
		}
		normal := newellNormal(verts)
		if normal.LenSq() < 1e-10 {
			continue
		}
		normal = normal.Norm()
		if normal.Y < 0 {
			reverseVec3(verts)
			normal = normal.Neg()
		}
		if normal.Y < minNormalY || normal.Y < 1e-5 {
			continue
		}
		pl := plane3D{
			normal: normal,
			planeD: -normal.Dot(verts[0]),
		}
		rawXZ := make([]vec2XZ, len(verts))
		for i, v := range verts {
			rawXZ[i] = vec2XZ{x: float64(v.X), z: float64(v.Z)}
		}
		cleaned := cleanPolyXZ(rawXZ)
		if len(cleaned) < 3 {
			continue
		}
		clipped := clipPolyToBoxXZ(cleaned, minX, minZ, maxX, maxZ)
		if len(clipped) < 3 {
			continue
		}
		yMin, yMax := float32(math.Inf(1)), float32(math.Inf(-1))
		for _, p := range clipped {
			y := pl.heightAt(p.x, p.z)
			if y < yMin {
				yMin = y
			}
			if y > yMax {
				yMax = y
			}
		}
		flags := in.Flags
		if flags == 0 {
			flags = NavFlagWalk
		}
		tile.base = append(tile.base, basePoly3D{
			xz:    clipped,
			plane: pl,
			minY:  yMin,
			maxY:  yMax,
			area:  in.Area,
			flags: flags,
		})
	}
	return tile
}

// TiledNavMeshConfig configures a 3D TiledNavMesh.
type TiledNavMeshConfig struct {
	// TileSize is the XZ width and depth of each square tile in world units.
	// Defaults to 64 when <= 0 (matching ChunkStreamerConfig.ChunkSize).
	TileSize float32
	// Origin is the world-space XZ origin of tile (0, 0). Defaults to (0, 0, 0).
	Origin math3d.Vec3
	// MaxSlopeRad is the default slope limit used by BakeTile when building tiles.
	// Defaults to pi/4 (45 degrees) when < 0.
	MaxSlopeRad float32
	// VerticalTolerance is the maximum vertical distance from a query point to a walkable polygon.
	// Defaults to 2.0 when <= 0.
	VerticalTolerance float32
}

// TiledNavPathResult is the outcome of a 3D TiledNavMesh path search, including
// the tile footprint and tile versions crossed so callers can detect invalidation.
type TiledNavPathResult struct {
	Path       []math3d.Vec3
	Corridor   []NavPolyRef
	Tiles      []NavTileVersion
	Found      bool
	Partial    bool
	Exhausted  bool
	Expansions int
}

// CrossesTile reports whether r's corridor passes through coord.
func (r TiledNavPathResult) CrossesTile(coord ChunkCoord) bool {
	for _, tv := range r.Tiles {
		if tv.Coord == coord {
			return true
		}
	}
	return false
}

// TileCoords returns the deduplicated list of chunk/tile coordinates crossed by r.
func (r TiledNavPathResult) TileCoords() []ChunkCoord {
	if len(r.Tiles) == 0 {
		return nil
	}
	out := make([]ChunkCoord, len(r.Tiles))
	for i, tv := range r.Tiles {
		out[i] = tv.Coord
	}
	return out
}

// TiledNavRaycastHit describes the result of casting a segment across a 3D TiledNavMesh.
type TiledNavRaycastHit struct {
	Point  math3d.Vec3
	Normal math3d.Vec3
	Poly   NavPolyRef
	T      float32
	Hit    bool
}

// Cardinal neighbor directions for 3D XZ tiles: 0=-X, 1=+X, 2=-Z, 3=+Z.
var tileDir3D = [4]ChunkCoord{
	{X: -1, Z: 0},
	{X: 1, Z: 0},
	{X: 0, Z: -1},
	{X: 0, Z: 1},
}

var tileOppDir3D = [4]int{1, 0, 3, 2}

type tiledLink3D struct {
	fromPoly int32
	toSlot   int32
	toPoly   int32
	left     math3d.Vec3
	right    math3d.Vec3
	cross    bool
}

type navTileSlot3D struct {
	slotID    int32
	salt      uint32
	coord     ChunkCoord
	minX      float64
	minZ      float64
	maxX      float64
	maxZ      float64
	base      []basePoly3D
	polys     []NavPoly
	links     []tiledLink3D
	polyLinks [][]int // per-poly indices into links
	bvhNodes  []navBVHNode3D
	bvhOrder  []int32
	border    [4][]int32
	bounds    math3d.AABB
	hasBounds bool
}

type obstacleEntry3D struct {
	id         NavObstacleID
	obs        NavObstacle
	cutters    [][]vec2XZ
	minX, minZ float64
	maxX, maxZ float64
	minY, maxY float32
	unboundedY bool
	tiles      []ChunkCoord
}

type tileListener3D struct {
	id NavListenerID
	fn func(NavTileChange)
}

type pathWatch3D struct {
	id    NavPathWatchID
	tiles []ChunkCoord
	fn    func(NavTileChange)
}

// TiledNavMesh is a 3D navigation mesh partitioned into fixed-size XZ tiles that
// can be baked, added, removed, and replaced independently at runtime (including
// via NavTileStreamer / ChunkStreamer). Polygons are automatically linked in full 3D
// across tile borders (preserving multi-level separation), dynamic obstacles rebuild
// only the touched tiles, a two-level 3D spatial index avoids whole-mesh scans, and
// per-tile versions + watchers invalidate paths crossing changed tiles.
//
// TiledNavMesh is deterministic and is not safe for concurrent mutation.
type TiledNavMesh struct {
	tileSize          float64
	originX           float64
	originZ           float64
	maxSlopeRad       float32
	VerticalTolerance float32

	tiles     map[ChunkCoord]*navTileSlot3D
	slots     []*navTileSlot3D
	freeSlots []int32
	nextSalt  uint32

	tileVersions map[ChunkCoord]uint64
	revision     uint64
	polyCount    int

	tileBVHDirty bool
	tileBVHNodes []navBVHNode3D
	tileBVHOrder []*navTileSlot3D

	nextObstacleID NavObstacleID
	obstacles      map[NavObstacleID]*obstacleEntry3D
	tileObstacles  map[ChunkCoord][]NavObstacleID

	nextListenerID NavListenerID
	listeners      []tileListener3D
	nextWatchID    NavPathWatchID
	watches        map[NavPathWatchID]*pathWatch3D
	tileWatches    map[ChunkCoord][]NavPathWatchID

	batchDepth int
	dirtyTiles map[ChunkCoord]NavTileChangeKind
}

// NewTiledNavMesh creates an empty 3D TiledNavMesh with cfg.
func NewTiledNavMesh(cfg TiledNavMeshConfig) *TiledNavMesh {
	ts := float64(cfg.TileSize)
	if !isFinite32(cfg.TileSize) || ts <= 0 {
		ts = float64(defaultNavTileSize3D)
	}
	ox, oz := float64(cfg.Origin.X), float64(cfg.Origin.Z)
	if math.IsNaN(ox) || math.IsInf(ox, 0) {
		ox = 0
	}
	if math.IsNaN(oz) || math.IsInf(oz, 0) {
		oz = 0
	}
	maxSlope := cfg.MaxSlopeRad
	if maxSlope < 0 {
		maxSlope = math.Pi / 4
	}
	vtol := cfg.VerticalTolerance
	if vtol <= 0 {
		vtol = defaultVerticalTolerance
	}
	return &TiledNavMesh{
		tileSize:          ts,
		originX:           ox,
		originZ:           oz,
		maxSlopeRad:       maxSlope,
		VerticalTolerance: vtol,
		tiles:             make(map[ChunkCoord]*navTileSlot3D),
		tileVersions:      make(map[ChunkCoord]uint64),
		obstacles:         make(map[NavObstacleID]*obstacleEntry3D),
		tileObstacles:     make(map[ChunkCoord][]NavObstacleID),
		watches:           make(map[NavPathWatchID]*pathWatch3D),
		tileWatches:       make(map[ChunkCoord][]NavPathWatchID),
		revision:          1,
	}
}

// BuildTiledNavMesh constructs a 3D TiledNavMesh from convex walkable 3D polygons using fixed square XZ tiles of size tileSize.
func BuildTiledNavMesh(polys [][]math3d.Vec3, maxSlopeRad, tileSize float32) *TiledNavMesh {
	inputs := make([]NavPolygonInput, len(polys))
	for i, raw := range polys {
		inputs[i] = NavPolygonInput{
			Vertices: raw,
			Area:     NavAreaDefault,
			Flags:    NavFlagWalk,
		}
	}
	return BuildTiledNavMeshWithAreas(inputs, maxSlopeRad, tileSize)
}

// BuildTiledNavMeshWithAreas constructs a 3D TiledNavMesh from convex 3D polygons carrying per-polygon areas and flags.
func BuildTiledNavMeshWithAreas(polys []NavPolygonInput, maxSlopeRad, tileSize float32) *TiledNavMesh {
	tm := NewTiledNavMesh(TiledNavMeshConfig{
		TileSize:    tileSize,
		MaxSlopeRad: maxSlopeRad,
	})
	if len(polys) == 0 {
		return tm
	}
	perTile := make(map[ChunkCoord][]NavPolygonInput)
	var coords []ChunkCoord
	for _, in := range polys {
		if len(in.Vertices) < 3 {
			continue
		}
		minX, minZ := math.Inf(1), math.Inf(1)
		maxX, maxZ := math.Inf(-1), math.Inf(-1)
		valid := true
		for _, v := range in.Vertices {
			if !isFiniteVec3(v) {
				valid = false
				break
			}
			minX = min(minX, float64(v.X))
			minZ = min(minZ, float64(v.Z))
			maxX = max(maxX, float64(v.X))
			maxZ = max(maxZ, float64(v.Z))
		}
		if !valid || maxX <= minX+tiledSplitEps3D || maxZ <= minZ+tiledSplitEps3D {
			continue
		}
		tx0, tz0, tx1, tz1 := tm.tileRangeForXZ(minX, minZ, maxX, maxZ)
		for tx := tx0; tx <= tx1; tx++ {
			for tz := tz0; tz <= tz1; tz++ {
				c := ChunkCoord{X: tx, Z: tz}
				if _, exists := perTile[c]; !exists {
					coords = append(coords, c)
				}
				perTile[c] = append(perTile[c], in)
			}
		}
	}
	slices.SortFunc(coords, compareChunkCoord)
	origin := tm.Origin()
	for _, c := range coords {
		tile := BakeNavTile(c, float32(tm.tileSize), origin, perTile[c], tm.maxSlopeRad)
		if len(tile.base) > 0 {
			tm.SetTile(c, tile)
		}
	}
	return tm
}

// TileSize returns the configured XZ tile size in world units.
func (tm *TiledNavMesh) TileSize() float32 {
	if tm == nil {
		return 0
	}
	return float32(tm.tileSize)
}

// Origin returns the world-space XZ origin of tile (0, 0).
func (tm *TiledNavMesh) Origin() math3d.Vec3 {
	if tm == nil {
		return math3d.Vec3{}
	}
	return math3d.Vec3{X: float32(tm.originX), Z: float32(tm.originZ)}
}

// TileCoordAt returns the ChunkCoord containing (p.X, p.Z).
func (tm *TiledNavMesh) TileCoordAt(p math3d.Vec3) ChunkCoord {
	if tm == nil || tm.tileSize <= 0 {
		return ChunkCoord{}
	}
	return ChunkCoord{
		X: int32(math.Floor((float64(p.X) - tm.originX) / tm.tileSize)),
		Z: int32(math.Floor((float64(p.Z) - tm.originZ) / tm.tileSize)),
	}
}

// TileBounds returns the world-space XZ bounds of tile coord (with Y spanning [-math.MaxFloat32/4, +math.MaxFloat32/4]
// when empty or the tile's polygon Y bounds when loaded).
func (tm *TiledNavMesh) TileBounds(coord ChunkCoord) math3d.AABB {
	if tm == nil {
		return math3d.AABB{}
	}
	minX, minZ, maxX, maxZ := tm.tileBounds64(coord)
	if slot := tm.tiles[coord]; slot != nil && slot.hasBounds {
		return math3d.AABB{
			Min: math3d.Vec3{X: float32(minX), Y: slot.bounds.Min.Y, Z: float32(minZ)},
			Max: math3d.Vec3{X: float32(maxX), Y: slot.bounds.Max.Y, Z: float32(maxZ)},
		}
	}
	return math3d.AABB{
		Min: math3d.Vec3{X: float32(minX), Y: 0, Z: float32(minZ)},
		Max: math3d.Vec3{X: float32(maxX), Y: 0, Z: float32(maxZ)},
	}
}

func (tm *TiledNavMesh) tileBounds64(coord ChunkCoord) (minX, minZ, maxX, maxZ float64) {
	minX = tm.originX + float64(coord.X)*tm.tileSize
	maxX = tm.originX + float64(coord.X+1)*tm.tileSize
	minZ = tm.originZ + float64(coord.Z)*tm.tileSize
	maxZ = tm.originZ + float64(coord.Z+1)*tm.tileSize
	return
}

func (tm *TiledNavMesh) tileRangeForXZ(minX, minZ, maxX, maxZ float64) (tx0, tz0, tx1, tz1 int32) {
	tx0 = int32(math.Floor((minX - tm.originX) / tm.tileSize))
	tz0 = int32(math.Floor((minZ - tm.originZ) / tm.tileSize))
	adjMaxX := maxX
	if maxX > minX+tiledSplitEps3D {
		adjMaxX = maxX - tiledSplitEps3D
	}
	adjMaxZ := maxZ
	if maxZ > minZ+tiledSplitEps3D {
		adjMaxZ = maxZ - tiledSplitEps3D
	}
	tx1 = int32(math.Floor((adjMaxX - tm.originX) / tm.tileSize))
	tz1 = int32(math.Floor((adjMaxZ - tm.originZ) / tm.tileSize))
	if tx1 < tx0 {
		tx1 = tx0
	}
	if tz1 < tz0 {
		tz1 = tz0
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
func (tm *TiledNavMesh) TileVersion(coord ChunkCoord) uint64 {
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
func (tm *TiledNavMesh) HasTile(coord ChunkCoord) bool {
	if tm == nil {
		return false
	}
	_, ok := tm.tiles[coord]
	return ok
}

// TileCoords returns all installed tile coordinates in deterministic sorted order.
func (tm *TiledNavMesh) TileCoords() []ChunkCoord {
	if tm == nil || len(tm.tiles) == 0 {
		return nil
	}
	out := make([]ChunkCoord, 0, len(tm.tiles))
	for c := range tm.tiles {
		out = append(out, c)
	}
	slices.SortFunc(out, compareChunkCoord)
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
func (tm *TiledNavMesh) TilePolygonCount(coord ChunkCoord) int {
	if tm == nil {
		return 0
	}
	slot := tm.tiles[coord]
	if slot == nil {
		return 0
	}
	return len(slot.polys)
}

// BakeTile bakes polys into a 3D NavTile for coord using tm's TileSize, Origin, and MaxSlopeRad and installs it into tm.
func (tm *TiledNavMesh) BakeTile(coord ChunkCoord, polys []NavPolygonInput) *NavTile {
	if tm == nil {
		return nil
	}
	tile := BakeNavTile(coord, float32(tm.tileSize), tm.Origin(), polys, tm.maxSlopeRad)
	tm.SetTile(coord, tile)
	return tile
}

// AddTile installs tile at coord only if no tile currently exists at coord.
func (tm *TiledNavMesh) AddTile(coord ChunkCoord, tile *NavTile) bool {
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
func (tm *TiledNavMesh) ReplaceTile(coord ChunkCoord, tile *NavTile) bool {
	if tm == nil || tile == nil {
		return false
	}
	if _, exists := tm.tiles[coord]; !exists {
		return false
	}
	tm.installTile(coord, tile, NavTileUpdated)
	return true
}

// SetTile installs or replaces tile at coord, applying active dynamic obstacles touching coord
// and linking polygons in full 3D across tile borders with neighboring tiles.
func (tm *TiledNavMesh) SetTile(coord ChunkCoord, tile *NavTile) bool {
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

// RemoveTile unloads the tile at coord, unlinks cross-tile border links from neighboring tiles,
// bumps coord's tile version, and notifies listeners / invalidates paths crossing coord.
func (tm *TiledNavMesh) RemoveTile(coord ChunkCoord) bool {
	if tm == nil {
		return false
	}
	slot, exists := tm.tiles[coord]
	if !exists {
		return false
	}
	tm.unlinkNeighborsOf(slot)
	tm.polyCount -= len(slot.polys)
	delete(tm.tiles, coord)
	tm.slots[slot.slotID] = nil
	tm.freeSlots = append(tm.freeSlots, slot.slotID)
	tm.tileBVHDirty = true

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
func (tm *TiledNavMesh) RebuildTile(coord ChunkCoord) bool {
	if tm == nil {
		return false
	}
	slot, exists := tm.tiles[coord]
	if !exists {
		return false
	}
	tm.rebuildSlot(slot)
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

func (tm *TiledNavMesh) installTile(coord ChunkCoord, tile *NavTile, kind NavTileChangeKind) {
	minX, minZ, maxX, maxZ := tm.tileBounds64(coord)
	sameBounds := math.Abs(tile.minX-minX) <= tiledSplitEps3D &&
		math.Abs(tile.minZ-minZ) <= tiledSplitEps3D &&
		math.Abs(tile.maxX-maxX) <= tiledSplitEps3D &&
		math.Abs(tile.maxZ-maxZ) <= tiledSplitEps3D

	baseCopy := make([]basePoly3D, 0, len(tile.base))
	for _, bp := range tile.base {
		xz := bp.xz
		yMin, yMax := bp.minY, bp.maxY
		if !sameBounds {
			xz = clipPolyToBoxXZ(xz, minX, minZ, maxX, maxZ)
			if len(xz) < 3 {
				continue
			}
			yMin, yMax = float32(math.Inf(1)), float32(math.Inf(-1))
			for _, p := range xz {
				y := bp.plane.heightAt(p.x, p.z)
				if y < yMin {
					yMin = y
				}
				if y > yMax {
					yMax = y
				}
			}
		} else {
			cp := make([]vec2XZ, len(xz))
			copy(cp, xz)
			xz = cp
		}
		baseCopy = append(baseCopy, basePoly3D{
			xz:    xz,
			plane: bp.plane,
			minY:  yMin,
			maxY:  yMax,
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
		slot = &navTileSlot3D{
			slotID: slotID,
			coord:  coord,
			minX:   minX,
			minZ:   minZ,
			maxX:   maxX,
			maxZ:   maxZ,
		}
		tm.slots[slotID] = slot
		tm.tiles[coord] = slot
		tm.tileBVHDirty = true
	}
	slot.base = baseCopy

	if tm.batchDepth > 0 {
		tm.markDirtyBatch(coord, kind)
		return
	}
	tm.rebuildSlot(slot)
	tm.revision++
	tm.tileVersions[coord]++
	tm.notifyChanges([]NavTileChange{{Coord: coord, Kind: kind, Version: tm.tileVersions[coord]}})
}

func (tm *TiledNavMesh) rebuildSlot(slot *navTileSlot3D) {
	tm.unlinkNeighborsOf(slot)
	tm.polyCount -= len(slot.polys)
	slot.salt = tm.allocSalt()

	working := make([]basePoly3D, len(slot.base))
	copy(working, slot.base)

	obsIDs := tm.tileObstacles[slot.coord]
	if len(obsIDs) > 0 && len(working) > 0 {
		var next []basePoly3D
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
					// Multi-level vertical separation: skip polygons outside the obstacle's Y span.
					if !entry.unboundedY && (p.maxY < entry.minY-sharedEdgeEps || p.minY > entry.maxY+sharedEdgeEps) {
						next = append(next, p)
						continue
					}
					pMinX, pMinZ, pMaxX, pMaxZ := polyBoundsXZ(p.xz)
					if pMaxX <= entry.minX+tiledSplitEps3D || pMinX >= entry.maxX-tiledSplitEps3D ||
						pMaxZ <= entry.minZ+tiledSplitEps3D || pMinZ >= entry.maxZ-tiledSplitEps3D {
						next = append(next, p)
						continue
					}
					outside, inside := carveConvexPolyXZ(p.xz, cutter)
					for _, ov := range outside {
						next = append(next, makeBasePoly3D(ov, p.plane, p.area, p.flags))
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
						next = append(next, makeBasePoly3D(inside, p.plane, area, flags))
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
		if len(working) > 1 {
			working = mergeTilePolys3D(working)
		}
	}

	slot.polys = make([]NavPoly, len(working))
	slot.polyLinks = make([][]int, len(working))
	slot.hasBounds = len(working) > 0
	slot.bounds = math3d.AABBEmpty()
	for i, wp := range working {
		np := liftPolyTo3D(wp.xz, wp.plane, wp.area, wp.flags)
		slot.polys[i] = np
		slot.bounds = slot.bounds.Union(np.Bounds)
	}
	tm.polyCount += len(slot.polys)

	slot.bvhNodes, slot.bvhOrder = buildPolyBVH3D(slot.polys)

	// Build intra-tile 3D links using local BVH in deterministic (i, j) order.
	slot.links = slot.links[:0]
	pad := math3d.Vec3{X: sharedEdgeEps, Y: sharedEdgeEps, Z: sharedEdgeEps}
	var candidates []int
	for i := range slot.polys {
		pi := &slot.polys[i]
		bi := math3d.AABB{Min: pi.Bounds.Min.Sub(pad), Max: pi.Bounds.Max.Add(pad)}
		candidates = queryPolyBVH3DAABB(slot.polys, slot.bvhNodes, slot.bvhOrder, bi, candidates[:0])
		slices.Sort(candidates)
		for _, j := range candidates {
			if j <= i {
				continue
			}
			pj := &slot.polys[j]
			if !bi.Intersects(pj.Bounds) {
				continue
			}
			l, r, ok := findSharedPortal(pi.Vertices, pj.Vertices)
			if !ok {
				continue
			}
			lidIJ := len(slot.links)
			slot.links = append(slot.links, tiledLink3D{
				fromPoly: int32(i),
				toSlot:   slot.slotID,
				toPoly:   int32(j),
				left:     l,
				right:    r,
			})
			slot.polyLinks[i] = append(slot.polyLinks[i], lidIJ)

			lidJI := len(slot.links)
			slot.links = append(slot.links, tiledLink3D{
				fromPoly: int32(j),
				toSlot:   slot.slotID,
				toPoly:   int32(i),
				left:     r,
				right:    l,
			})
			slot.polyLinks[j] = append(slot.polyLinks[j], lidJI)
		}
	}

	// Classify border polygons for the 4 XZ borders (-X, +X, -Z, +Z).
	for d := range 4 {
		slot.border[d] = slot.border[d][:0]
	}
	for i := range slot.polys {
		b := slot.polys[i].Bounds
		if float64(b.Min.X) <= slot.minX+sharedEdgeEps {
			slot.border[0] = append(slot.border[0], int32(i))
		}
		if float64(b.Max.X) >= slot.maxX-sharedEdgeEps {
			slot.border[1] = append(slot.border[1], int32(i))
		}
		if float64(b.Min.Z) <= slot.minZ+sharedEdgeEps {
			slot.border[2] = append(slot.border[2], int32(i))
		}
		if float64(b.Max.Z) >= slot.maxZ-sharedEdgeEps {
			slot.border[3] = append(slot.border[3], int32(i))
		}
	}

	tm.linkNeighborsOf(slot)
	tm.tileBVHDirty = true
}

func makeBasePoly3D(xz []vec2XZ, pl plane3D, area NavArea, flags NavFlags) basePoly3D {
	yMin, yMax := float32(math.Inf(1)), float32(math.Inf(-1))
	for _, p := range xz {
		y := pl.heightAt(p.x, p.z)
		if y < yMin {
			yMin = y
		}
		if y > yMax {
			yMax = y
		}
	}
	return basePoly3D{
		xz:    xz,
		plane: pl,
		minY:  yMin,
		maxY:  yMax,
		area:  area,
		flags: flags,
	}
}

func polyBoundsXZ(verts []vec2XZ) (minX, minZ, maxX, maxZ float64) {
	minX, minZ = verts[0].x, verts[0].z
	maxX, maxZ = verts[0].x, verts[0].z
	for _, v := range verts[1:] {
		minX = min(minX, v.x)
		minZ = min(minZ, v.z)
		maxX = max(maxX, v.x)
		maxZ = max(maxZ, v.z)
	}
	return
}

func mergeTilePolys3D(polys []basePoly3D) []basePoly3D {
	if len(polys) <= 1 || len(polys) > 256 {
		return polys
	}
	merged := true
	for merged {
		merged = false
		for i := 0; i < len(polys); i++ {
			for j := i + 1; j < len(polys); j++ {
				if polys[i].area != polys[j].area || polys[i].flags != polys[j].flags || !samePlane3D(polys[i].plane, polys[j].plane) {
					continue
				}
				if mv, ok := tryMergeConvexXZ(polys[i].xz, polys[j].xz); ok {
					polys[i] = makeBasePoly3D(mv, polys[i].plane, polys[i].area, polys[i].flags)
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

func (tm *TiledNavMesh) unlinkNeighborsOf(slot *navTileSlot3D) {
	for _, dir := range tileDir3D {
		nb := tm.tiles[ChunkCoord{X: slot.coord.X + dir.X, Z: slot.coord.Z + dir.Z}]
		if nb == nil {
			continue
		}
		nb.removeLinksTo(slot.slotID)
	}
}

func (s *navTileSlot3D) removeLinksTo(targetSlot int32) {
	hasAny := false
	for _, l := range s.links {
		if l.cross && l.toSlot == targetSlot {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return
	}
	for i := range s.polyLinks {
		s.polyLinks[i] = s.polyLinks[i][:0]
	}
	kept := s.links[:0]
	for _, l := range s.links {
		if l.cross && l.toSlot == targetSlot {
			continue
		}
		lid := len(kept)
		kept = append(kept, l)
		s.polyLinks[l.fromPoly] = append(s.polyLinks[l.fromPoly], lid)
	}
	s.links = kept
}

func (tm *TiledNavMesh) linkNeighborsOf(slot *navTileSlot3D) {
	pad := math3d.Vec3{X: sharedEdgeEps, Y: sharedEdgeEps, Z: sharedEdgeEps}
	for d, dir := range tileDir3D {
		nb := tm.tiles[ChunkCoord{X: slot.coord.X + dir.X, Z: slot.coord.Z + dir.Z}]
		if nb == nil {
			continue
		}
		myBorder := slot.border[d]
		nbBorder := nb.border[tileOppDir3D[d]]
		if len(myBorder) == 0 || len(nbBorder) == 0 {
			continue
		}
		for _, i := range myBorder {
			pi := &slot.polys[i]
			bi := math3d.AABB{Min: pi.Bounds.Min.Sub(pad), Max: pi.Bounds.Max.Add(pad)}
			for _, j := range nbBorder {
				pj := &nb.polys[j]
				if !bi.Intersects(pj.Bounds) {
					continue
				}
				l, r, ok := findSharedPortal(pi.Vertices, pj.Vertices)
				if !ok {
					continue
				}
				lidAB := len(slot.links)
				slot.links = append(slot.links, tiledLink3D{
					fromPoly: i,
					toSlot:   nb.slotID,
					toPoly:   j,
					left:     l,
					right:    r,
					cross:    true,
				})
				slot.polyLinks[i] = append(slot.polyLinks[i], lidAB)

				lidBA := len(nb.links)
				nb.links = append(nb.links, tiledLink3D{
					fromPoly: j,
					toSlot:   slot.slotID,
					toPoly:   i,
					left:     r,
					right:    l,
					cross:    true,
				})
				nb.polyLinks[j] = append(nb.polyLinks[j], lidBA)
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

func (tm *TiledNavMesh) markDirtyBatch(coord ChunkCoord, kind NavTileChangeKind) {
	if tm.dirtyTiles == nil {
		tm.dirtyTiles = make(map[ChunkCoord]NavTileChangeKind)
	}
	prev, exists := tm.dirtyTiles[coord]
	if !exists {
		tm.dirtyTiles[coord] = kind
		return
	}
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
	coords := make([]ChunkCoord, 0, len(tm.dirtyTiles))
	for c := range tm.dirtyTiles {
		coords = append(coords, c)
	}
	slices.SortFunc(coords, compareChunkCoord)

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
	clear(tm.dirtyTiles)
	if len(changes) > 0 {
		tm.revision++
		tm.notifyChanges(changes)
	}
}

// AddObstacle registers a dynamic 3D obstacle and rebuilds only the touched tiles
// (or defers rebuild until Batch returns if called inside Batch).
func (tm *TiledNavMesh) AddObstacle(obs NavObstacle) (NavObstacleID, bool) {
	if tm == nil {
		return 0, false
	}
	cutters, minX, minZ, maxX, maxZ, minY, maxY, unboundedY, ok := buildObstacleCutters3D(obs)
	if !ok {
		return 0, false
	}
	tm.nextObstacleID++
	id := tm.nextObstacleID
	touched := tm.coordsForObstacleXZ(minX, minZ, maxX, maxZ)
	entry := &obstacleEntry3D{
		id:         id,
		obs:        obs,
		cutters:    cutters,
		minX:       minX,
		minZ:       minZ,
		maxX:       maxX,
		maxZ:       maxZ,
		minY:       minY,
		maxY:       maxY,
		unboundedY: unboundedY,
		tiles:      touched,
	}
	tm.obstacles[id] = entry
	for _, c := range touched {
		tm.addTileObstacleRef(c, id)
	}
	tm.rebuildAffectedTiles(touched)
	return id, true
}

// UpdateObstacle replaces the geometry or mode of dynamic obstacle id, rebuilding only tiles
// touched by either its old or new footprint.
func (tm *TiledNavMesh) UpdateObstacle(id NavObstacleID, obs NavObstacle) bool {
	if tm == nil {
		return false
	}
	old, exists := tm.obstacles[id]
	if !exists {
		return false
	}
	cutters, minX, minZ, maxX, maxZ, minY, maxY, unboundedY, ok := buildObstacleCutters3D(obs)
	if !ok {
		return false
	}
	oldTiles := old.tiles
	for _, c := range oldTiles {
		tm.removeTileObstacleRef(c, id)
	}
	newTiles := tm.coordsForObstacleXZ(minX, minZ, maxX, maxZ)
	old.obs = obs
	old.cutters = cutters
	old.minX, old.minZ, old.maxX, old.maxZ = minX, minZ, maxX, maxZ
	old.minY, old.maxY, old.unboundedY = minY, maxY, unboundedY
	old.tiles = newTiles
	for _, c := range newTiles {
		tm.addTileObstacleRef(c, id)
	}

	union := make([]ChunkCoord, 0, len(oldTiles)+len(newTiles))
	union = append(union, oldTiles...)
	union = append(union, newTiles...)
	slices.SortFunc(union, compareChunkCoord)
	union = slices.Compact(union)
	tm.rebuildAffectedTiles(union)
	return true
}

// RemoveObstacle removes dynamic obstacle id and rebuilds only the tiles it touched.
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
	var affected []ChunkCoord
	for c := range tm.tileObstacles {
		affected = append(affected, c)
	}
	slices.SortFunc(affected, compareChunkCoord)
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

func (tm *TiledNavMesh) coordsForObstacleXZ(minX, minZ, maxX, maxZ float64) []ChunkCoord {
	tx0, tz0, tx1, tz1 := tm.tileRangeForXZ(minX, minZ, maxX, maxZ)
	out := make([]ChunkCoord, 0, int(tx1-tx0+1)*int(tz1-tz0+1))
	for tx := tx0; tx <= tx1; tx++ {
		for tz := tz0; tz <= tz1; tz++ {
			out = append(out, ChunkCoord{X: tx, Z: tz})
		}
	}
	return out
}

func (tm *TiledNavMesh) addTileObstacleRef(coord ChunkCoord, id NavObstacleID) {
	ids := tm.tileObstacles[coord]
	idx, found := slices.BinarySearch(ids, id)
	if found {
		return
	}
	tm.tileObstacles[coord] = slices.Insert(ids, idx, id)
}

func (tm *TiledNavMesh) removeTileObstacleRef(coord ChunkCoord, id NavObstacleID) {
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

func (tm *TiledNavMesh) rebuildAffectedTiles(coords []ChunkCoord) {
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
	if len(changes) > 0 {
		tm.revision++
		tm.notifyChanges(changes)
	}
}

// AddTileListener registers fn to be invoked whenever any tile is added, updated, or removed.
func (tm *TiledNavMesh) AddTileListener(fn func(NavTileChange)) NavListenerID {
	if tm == nil || fn == nil {
		return 0
	}
	tm.nextListenerID++
	id := tm.nextListenerID
	tm.listeners = append(tm.listeners, tileListener3D{id: id, fn: fn})
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

// IsPathValid reports whether every tile crossed by res is still present and unchanged since res was computed.
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
// Once triggered, the watch is automatically removed.
func (tm *TiledNavMesh) WatchPath(tiles []ChunkCoord, onInvalidate func(NavTileChange)) NavPathWatchID {
	if tm == nil || len(tiles) == 0 || onInvalidate == nil {
		return 0
	}
	dedup := make([]ChunkCoord, len(tiles))
	copy(dedup, tiles)
	slices.SortFunc(dedup, compareChunkCoord)
	dedup = slices.Compact(dedup)

	tm.nextWatchID++
	id := tm.nextWatchID
	tm.watches[id] = &pathWatch3D{
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
	if len(tm.listeners) > 0 {
		listeners := make([]tileListener3D, len(tm.listeners))
		copy(listeners, tm.listeners)
		for _, ch := range changes {
			for _, l := range listeners {
				l.fn(ch)
			}
		}
	}
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
		for _, wid := range tm.tileWatches[ch.Coord] {
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

// --- 3D Two-Level Spatial Index Lookups ---

func (tm *TiledNavMesh) verticalTol() float32 {
	if tm != nil && tm.VerticalTolerance > 0 {
		return tm.VerticalTolerance
	}
	return defaultVerticalTolerance
}

func (tm *TiledNavMesh) resolveRef(ref NavPolyRef) (*navTileSlot3D, *NavPoly) {
	if tm == nil || !ref.Valid() {
		return nil, nil
	}
	slot := tm.tiles[ref.Tile]
	if slot == nil || slot.salt != ref.Salt || int(ref.Poly) >= len(slot.polys) {
		return nil, nil
	}
	return slot, &slot.polys[ref.Poly]
}

// Polygon returns a copy of the 3D vertices of ref, or nil if ref is stale or invalid.
func (tm *TiledNavMesh) Polygon(ref NavPolyRef) []math3d.Vec3 {
	_, p := tm.resolveRef(ref)
	if p == nil {
		return nil
	}
	out := make([]math3d.Vec3, len(p.Vertices))
	copy(out, p.Vertices)
	return out
}

// PolygonArea returns the area of ref, or NavAreaDefault if invalid.
func (tm *TiledNavMesh) PolygonArea(ref NavPolyRef) NavArea {
	_, p := tm.resolveRef(ref)
	if p == nil {
		return NavAreaDefault
	}
	return p.Area
}

// PolygonFlags returns the runtime flags of ref, or 0 if invalid.
func (tm *TiledNavMesh) PolygonFlags(ref NavPolyRef) NavFlags {
	_, p := tm.resolveRef(ref)
	if p == nil {
		return 0
	}
	return p.Flags
}

// Neighbors returns the polygon references sharing a 3D edge (intra-tile or cross-tile) with ref.
func (tm *TiledNavMesh) Neighbors(ref NavPolyRef) []NavPolyRef {
	slot, _ := tm.resolveRef(ref)
	if slot == nil {
		return nil
	}
	lids := slot.polyLinks[ref.Poly]
	out := make([]NavPolyRef, 0, len(lids))
	for _, lid := range lids {
		l := slot.links[lid]
		dst := tm.slots[l.toSlot]
		if dst == nil {
			continue
		}
		out = append(out, NavPolyRef{Tile: dst.coord, Poly: l.toPoly, Salt: dst.salt})
	}
	return out
}

func (tm *TiledNavMesh) candidateTilesAtXZ(x, z float64, buf []ChunkCoord) []ChunkCoord {
	buf = buf[:0]
	const eps = float64(sharedEdgeEps)
	tx0 := int32(math.Floor((x - eps - tm.originX) / tm.tileSize))
	tx1 := int32(math.Floor((x + eps - tm.originX) / tm.tileSize))
	tz0 := int32(math.Floor((z - eps - tm.originZ) / tm.tileSize))
	tz1 := int32(math.Floor((z + eps - tm.originZ) / tm.tileSize))
	for tx := tx0; tx <= tx1; tx++ {
		for tz := tz0; tz <= tz1; tz++ {
			buf = append(buf, ChunkCoord{X: tx, Z: tz})
		}
	}
	return buf
}

// FindPolygon locates the walkable polygon containing (p.X, p.Z) whose 3D surface height is
// closest to p.Y within VerticalTolerance, using O(1) tile lookup + O(log P_tile) local 3D BVH.
func (tm *TiledNavMesh) FindPolygon(p math3d.Vec3) (NavPolyRef, bool) {
	return tm.FindPolygonWithFilter(p, NavFilter{})
}

// FindPolygonWithFilter locates the vertically closest matching polygon passing filter at p.
func (tm *TiledNavMesh) FindPolygonWithFilter(p math3d.Vec3, filter NavFilter) (NavPolyRef, bool) {
	if tm == nil || len(tm.tiles) == 0 || !isFiniteVec3(p) {
		return NavPolyRef{}, false
	}
	tol := tm.verticalTol()
	var coordBuf [4]ChunkCoord
	coords := tm.candidateTilesAtXZ(float64(p.X), float64(p.Z), coordBuf[:0])

	var bestRef NavPolyRef
	bestFound := false
	bestDY := tol + 1

	for _, c := range coords {
		slot := tm.tiles[c]
		if slot == nil || len(slot.polys) == 0 {
			continue
		}
		idx, dy, ok := findPolyInBVH3D(slot.polys, slot.bvhNodes, slot.bvhOrder, p, tol, filter)
		if !ok {
			continue
		}
		cand := NavPolyRef{Tile: c, Poly: int32(idx), Salt: slot.salt}
		if !bestFound || dy < bestDY || (dy == bestDY && comparePolyRef3D(cand, bestRef) < 0) {
			bestFound = true
			bestDY = dy
			bestRef = cand
		}
	}
	return bestRef, bestFound
}

// Contains reports whether p lies on any walkable polygon within VerticalTolerance.
func (tm *TiledNavMesh) Contains(p math3d.Vec3) bool {
	_, ok := tm.FindPolygon(p)
	return ok
}

// ContainsWithFilter reports whether p lies on any walkable polygon passing filter within VerticalTolerance.
func (tm *TiledNavMesh) ContainsWithFilter(p math3d.Vec3, filter NavFilter) bool {
	_, ok := tm.FindPolygonWithFilter(p, filter)
	return ok
}

func (tm *TiledNavMesh) ensureTileBVH() {
	if !tm.tileBVHDirty {
		return
	}
	tm.tileBVHDirty = false
	tm.tileBVHOrder = tm.tileBVHOrder[:0]
	for _, slot := range tm.tiles {
		if slot != nil && slot.hasBounds {
			tm.tileBVHOrder = append(tm.tileBVHOrder, slot)
		}
	}
	if len(tm.tileBVHOrder) == 0 {
		tm.tileBVHNodes = tm.tileBVHNodes[:0]
		return
	}
	slices.SortFunc(tm.tileBVHOrder, func(a, b *navTileSlot3D) int {
		return compareChunkCoord(a.coord, b.coord)
	})
	n := len(tm.tileBVHOrder)
	tm.tileBVHNodes = make([]navBVHNode3D, 0, max(1, (2*n)/navBVHLeafSize3D+4))
	var build func(start, end, depth int) int32
	build = func(start, end, depth int) int32 {
		nodeIdx := int32(len(tm.tileBVHNodes))
		tm.tileBVHNodes = append(tm.tileBVHNodes, navBVHNode3D{})

		s0 := tm.tileBVHOrder[start]
		bounds := s0.bounds
		cMin := s0.bounds.Center()
		cMax := cMin
		for i := start + 1; i < end; i++ {
			s := tm.tileBVHOrder[i]
			bounds = bounds.Union(s.bounds)
			c := s.bounds.Center()
			cMin = cMin.Min(c)
			cMax = cMax.Max(c)
		}

		count := end - start
		if count <= navBVHLeafSize3D || depth >= 56 {
			tm.tileBVHNodes[nodeIdx] = navBVHNode3D{
				bounds: bounds,
				left:   int32(start),
				count:  int32(count),
			}
			return nodeIdx
		}

		span := cMax.Sub(cMin)
		axis := 0
		if span.Z > span.X {
			axis = 2
		}
		sub := tm.tileBVHOrder[start:end]
		slices.SortFunc(sub, func(a, b *navTileSlot3D) int {
			var ca, cb float32
			if axis == 2 {
				ca, cb = a.bounds.Center().Z, b.bounds.Center().Z
			} else {
				ca, cb = a.bounds.Center().X, b.bounds.Center().X
			}
			if ca < cb {
				return -1
			}
			if ca > cb {
				return 1
			}
			return compareChunkCoord(a.coord, b.coord)
		})
		mid := start + count/2
		leftIdx := build(start, mid, depth+1)
		rightIdx := build(mid, end, depth+1)
		tm.tileBVHNodes[nodeIdx] = navBVHNode3D{
			bounds: bounds,
			left:   leftIdx,
			right:  rightIdx,
		}
		return nodeIdx
	}
	build(0, n, 0)
}

// NearestPolygon returns the 3D polygon closest to p and the closest 3D point on it.
func (tm *TiledNavMesh) NearestPolygon(p math3d.Vec3) (NavPolyRef, math3d.Vec3, bool) {
	return tm.NearestPolygonWithFilter(p, NavFilter{})
}

// NearestPolygonWithFilter returns the closest 3D polygon passing filter and closest point on it.
func (tm *TiledNavMesh) NearestPolygonWithFilter(p math3d.Vec3, filter NavFilter) (NavPolyRef, math3d.Vec3, bool) {
	if tm == nil || len(tm.tiles) == 0 || !isFiniteVec3(p) {
		return NavPolyRef{}, math3d.Vec3{}, false
	}
	if ref, ok := tm.FindPolygonWithFilter(p, filter); ok {
		_, poly := tm.resolveRef(ref)
		return ref, math3d.Vec3{X: p.X, Y: poly.HeightAt(p.X, p.Z), Z: p.Z}, true
	}
	return tm.nearestPolyInternal(p, math3d.Vec3{}, false, filter)
}

// NearestPoint returns the closest 3D point on any walkable polygon allowed by DefaultNavFilter
// within 3D half-extents [p - extent, p + extent], together with its NavPolyRef.
func (tm *TiledNavMesh) NearestPoint(p, extent math3d.Vec3) (math3d.Vec3, NavPolyRef, bool) {
	return tm.NearestPointWithFilter(p, extent, DefaultNavFilter())
}

// NearestPointWithFilter behaves like NearestPoint restricted to filter.
func (tm *TiledNavMesh) NearestPointWithFilter(p, extent math3d.Vec3, filter NavFilter) (math3d.Vec3, NavPolyRef, bool) {
	if tm == nil || len(tm.tiles) == 0 || !isFiniteVec3(p) || !isFiniteVec3(extent) ||
		extent.X < 0 || extent.Y < 0 || extent.Z < 0 {
		return math3d.Vec3{}, NavPolyRef{}, false
	}
	ref, pt, ok := tm.nearestPolyInternal(p, extent, true, filter)
	if !ok {
		return math3d.Vec3{}, NavPolyRef{}, false
	}
	return pt, ref, true
}

func (tm *TiledNavMesh) nearestPolyInternal(p, ext math3d.Vec3, useExtent bool, filter NavFilter) (NavPolyRef, math3d.Vec3, bool) {
	const eps float32 = 1e-4
	queryBox := math3d.AABB{
		Min: math3d.Vec3{X: p.X - ext.X - eps, Y: p.Y - ext.Y - eps, Z: p.Z - ext.Z - eps},
		Max: math3d.Vec3{X: p.X + ext.X + eps, Y: p.Y + ext.Y + eps, Z: p.Z + ext.Z + eps},
	}

	var bestRef NavPolyRef
	bestFound := false
	bestDistSq := math.Inf(1)
	var bestPt math3d.Vec3

	considerSlot := func(slot *navTileSlot3D) {
		if slot == nil || !slot.hasBounds {
			return
		}
		if useExtent && !slot.bounds.Intersects(queryBox) {
			return
		}
		if aabbDistSq3D(slot.bounds, p) > bestDistSq {
			return
		}
		idx, pt, d2, ok := nearestPolyInBVH3D(slot.polys, slot.bvhNodes, slot.bvhOrder, p, ext, useExtent, filter, bestDistSq)
		if !ok {
			return
		}
		candRef := NavPolyRef{Tile: slot.coord, Poly: int32(idx), Salt: slot.salt}
		if !bestFound || d2 < bestDistSq || (d2 == bestDistSq && comparePolyRef3D(candRef, bestRef) < 0) {
			bestFound = true
			bestDistSq = d2
			bestRef = candRef
			bestPt = pt
		}
	}

	if useExtent {
		tx0 := int32(math.Floor((float64(queryBox.Min.X) - tm.originX) / tm.tileSize))
		tx1 := int32(math.Floor((float64(queryBox.Max.X) - tm.originX) / tm.tileSize))
		tz0 := int32(math.Floor((float64(queryBox.Min.Z) - tm.originZ) / tm.tileSize))
		tz1 := int32(math.Floor((float64(queryBox.Max.Z) - tm.originZ) / tm.tileSize))
		spanX := int64(tx1) - int64(tx0) + 1
		spanZ := int64(tz1) - int64(tz0) + 1
		if spanX > 0 && spanZ > 0 && spanX*spanZ <= 64 && spanX*spanZ <= int64(len(tm.tiles))*2 {
			for tx := tx0; tx <= tx1; tx++ {
				for tz := tz0; tz <= tz1; tz++ {
					considerSlot(tm.tiles[ChunkCoord{X: tx, Z: tz}])
				}
			}
			return bestRef, bestPt, bestFound
		}
	}

	tm.ensureTileBVH()
	if len(tm.tileBVHNodes) == 0 {
		return NavPolyRef{}, math3d.Vec3{}, false
	}
	root := &tm.tileBVHNodes[0]
	if useExtent && !root.bounds.Intersects(queryBox) {
		return NavPolyRef{}, math3d.Vec3{}, false
	}
	var stack [64]bvhDistItem3D
	stack[0] = bvhDistItem3D{idx: 0, distSq: aabbDistSq3D(root.bounds, p)}
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
		lOk := !useExtent || lNode.bounds.Intersects(queryBox)
		rOk := !useExtent || rNode.bounds.Intersects(queryBox)
		lDist := math.Inf(1)
		if lOk {
			lDist = aabbDistSq3D(lNode.bounds, p)
		}
		rDist := math.Inf(1)
		if rOk {
			rDist = aabbDistSq3D(rNode.bounds, p)
		}
		if lDist <= rDist {
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.right, distSq: rDist}
				sp++
			}
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.left, distSq: lDist}
				sp++
			}
		} else {
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.left, distSq: lDist}
				sp++
			}
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.right, distSq: rDist}
				sp++
			}
		}
	}
	return bestRef, bestPt, bestFound
}

func comparePolyRef3D(a, b NavPolyRef) int {
	if c := compareChunkCoord(a.Tile, b.Tile); c != 0 {
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

// --- Per-tile 3D BVH Helpers ---

func queryPolyBVH3DAABB(polys []NavPoly, nodes []navBVHNode3D, order []int32, box math3d.AABB, dst []int) []int {
	if len(nodes) == 0 {
		for i := range polys {
			if polys[i].Bounds.Intersects(box) {
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
		if !n.bounds.Intersects(box) {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				idx := int(order[k])
				if polys[idx].Bounds.Intersects(box) {
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

func findPolyInBVH3D(polys []NavPoly, nodes []navBVHNode3D, order []int32, p math3d.Vec3, tol float32, filter NavFilter) (int, float32, bool) {
	const xzPad float32 = 1e-4
	minX, maxX := p.X-xzPad, p.X+xzPad
	minY, maxY := p.Y-tol-xzPad, p.Y+tol+xzPad
	minZ, maxZ := p.Z-xzPad, p.Z+xzPad

	bestIdx := -1
	bestDY := tol + 1

	checkPoly := func(idx int) {
		poly := &polys[idx]
		if !filter.Allows(poly.Area, poly.Flags) {
			return
		}
		if !poly.ContainsXZ(p.X, p.Z) {
			return
		}
		dy := float32(math.Abs(float64(p.Y - poly.HeightAt(p.X, p.Z))))
		if dy <= tol && (dy < bestDY || (dy == bestDY && (bestIdx < 0 || idx < bestIdx))) {
			bestDY = dy
			bestIdx = idx
		}
	}

	if len(nodes) == 0 {
		for i := range polys {
			checkPoly(i)
		}
		return bestIdx, bestDY, bestIdx >= 0
	}

	var stack [64]int32
	stack[0] = 0
	sp := 1
	for sp > 0 {
		sp--
		n := &nodes[stack[sp]]
		if n.bounds.Max.X < minX || n.bounds.Min.X > maxX ||
			n.bounds.Max.Y < minY || n.bounds.Min.Y > maxY ||
			n.bounds.Max.Z < minZ || n.bounds.Min.Z > maxZ {
			continue
		}
		if n.count > 0 {
			end := int(n.left + n.count)
			for k := int(n.left); k < end; k++ {
				checkPoly(int(order[k]))
			}
			continue
		}
		stack[sp] = n.right
		sp++
		stack[sp] = n.left
		sp++
	}
	return bestIdx, bestDY, bestIdx >= 0
}

func nearestPolyInBVH3D(polys []NavPoly, nodes []navBVHNode3D, order []int32, p, ext math3d.Vec3, useExtent bool, filter NavFilter, maxDistSq float64) (int, math3d.Vec3, float64, bool) {
	const eps float32 = 1e-4
	queryBox := math3d.AABB{
		Min: math3d.Vec3{X: p.X - ext.X - eps, Y: p.Y - ext.Y - eps, Z: p.Z - ext.Z - eps},
		Max: math3d.Vec3{X: p.X + ext.X + eps, Y: p.Y + ext.Y + eps, Z: p.Z + ext.Z + eps},
	}
	pd := toVec3d(p)
	cMinX, cMinY, cMinZ := pd.x-float64(ext.X), pd.y-float64(ext.Y), pd.z-float64(ext.Z)
	cMaxX, cMaxY, cMaxZ := pd.x+float64(ext.X), pd.y+float64(ext.Y), pd.z+float64(ext.Z)

	bestIdx := -1
	bestDistSq := maxDistSq
	var bestPt math3d.Vec3
	var clipA, clipB [24]vec3d

	checkPoly := func(idx int) {
		poly := &polys[idx]
		if !filter.Allows(poly.Area, poly.Flags) {
			return
		}
		if useExtent && !poly.Bounds.Intersects(queryBox) {
			return
		}
		if aabbDistSq3D(poly.Bounds, p) > bestDistSq {
			return
		}
		cp := closestPointOnNavPoly(poly, p)
		if useExtent && (math.Abs(float64(cp.X-p.X)) > float64(ext.X)+float64(eps) ||
			math.Abs(float64(cp.Y-p.Y)) > float64(ext.Y)+float64(eps) ||
			math.Abs(float64(cp.Z-p.Z)) > float64(ext.Z)+float64(eps)) {
			clipped := clipPolyAABB3DAny(poly.Vertices, cMinX, cMinY, cMinZ, cMaxX, cMaxY, cMaxZ, clipA[:0], clipB[:0])
			if len(clipped) == 0 {
				return
			}
			cp = toVec3f(closestPointOnConvex3Dd(clipped, poly, pd))
		}
		d2 := distSq3D(p, cp)
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

	root := &nodes[0]
	if useExtent && !root.bounds.Intersects(queryBox) {
		return -1, math3d.Vec3{}, bestDistSq, false
	}
	var stack [64]bvhDistItem3D
	stack[0] = bvhDistItem3D{idx: 0, distSq: aabbDistSq3D(root.bounds, p)}
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
				checkPoly(int(order[k]))
			}
			continue
		}
		lNode := &nodes[n.left]
		rNode := &nodes[n.right]
		lOk := !useExtent || lNode.bounds.Intersects(queryBox)
		rOk := !useExtent || rNode.bounds.Intersects(queryBox)
		lDist := math.Inf(1)
		if lOk {
			lDist = aabbDistSq3D(lNode.bounds, p)
		}
		rDist := math.Inf(1)
		if rOk {
			rDist = aabbDistSq3D(rNode.bounds, p)
		}
		if lDist <= rDist {
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.right, distSq: rDist}
				sp++
			}
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.left, distSq: lDist}
				sp++
			}
		} else {
			if lOk && lDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.left, distSq: lDist}
				sp++
			}
			if rOk && rDist <= bestDistSq {
				stack[sp] = bvhDistItem3D{idx: n.right, distSq: rDist}
				sp++
			}
		}
	}
	return bestIdx, bestPt, bestDistSq, bestIdx >= 0
}

// --- 3D Raycast across Tiles ---

// Raycast casts a segment along the 3D TiledNavMesh surface from start toward target using DefaultNavFilter.
func (tm *TiledNavMesh) Raycast(start, target math3d.Vec3) (TiledNavRaycastHit, bool) {
	return tm.RaycastWithFilter(start, target, DefaultNavFilter())
}

// RaycastWithFilter behaves like Raycast restricted to polygons allowed by filter.
func (tm *TiledNavMesh) RaycastWithFilter(start, target math3d.Vec3, filter NavFilter) (TiledNavRaycastHit, bool) {
	if tm == nil || len(tm.tiles) == 0 || !isFiniteVec3(start) || !isFiniteVec3(target) {
		hit := TiledNavRaycastHit{Point: start, T: 0, Hit: true}
		return hit, true
	}
	starts := tm.findPolyLocsAt(start, filter, false)
	if len(starts) == 0 {
		hit := TiledNavRaycastHit{Point: start, T: 0, Hit: true}
		return hit, true
	}

	curLoc := starts[0]
	sx, sz := float64(start.X), float64(start.Z)
	tx, tz := float64(target.X), float64(target.Z)
	dx, dz := tx-sx, tz-sz
	if len(starts) > 1 && dx*dx+dz*dz >= 1e-12 {
		bestT := -1.0
		for _, loc := range starts {
			poly := &tm.slots[loc.slot].polys[loc.poly]
			if poly.ContainsXZ(target.X, target.Z) {
				curLoc = loc
				break
			}
			tExit, _, _, ok := polyRayExitXZ(poly, sx, sz, dx, dz)
			if ok && tExit > bestT {
				bestT = tExit
				curLoc = loc
			}
		}
	}

	curSlot := tm.slots[curLoc.slot]
	curRef := NavPolyRef{Tile: curSlot.coord, Poly: curLoc.poly, Salt: curSlot.salt}
	if dx*dx+dz*dz < 1e-12 || curSlot.polys[curLoc.poly].ContainsXZ(target.X, target.Z) {
		pt := math3d.Vec3{X: target.X, Y: curSlot.polys[curLoc.poly].HeightAt(target.X, target.Z), Z: target.Z}
		return TiledNavRaycastHit{Point: pt, Poly: curRef, T: 1, Hit: false}, false
	}

	curT := 0.0
	visited := make(map[polyLoc3D]bool, 16)
	visited[curLoc] = true

	maxSteps := tm.polyCount + 4
	for range maxSteps {
		slot := tm.slots[curLoc.slot]
		poly := &slot.polys[curLoc.poly]
		curRef = NavPolyRef{Tile: slot.coord, Poly: curLoc.poly, Salt: slot.salt}
		if poly.ContainsXZ(target.X, target.Z) {
			pt := math3d.Vec3{X: target.X, Y: poly.HeightAt(target.X, target.Z), Z: target.Z}
			return TiledNavRaycastHit{Point: pt, Poly: curRef, T: 1, Hit: false}, false
		}
		tExit, exitNorm, _, ok := polyRayExitXZ(poly, sx, sz, dx, dz)
		if !ok {
			ex, ez := float32(sx+dx*curT), float32(sz+dz*curT)
			pt := math3d.Vec3{X: ex, Y: poly.HeightAt(ex, ez), Z: ez}
			return TiledNavRaycastHit{Point: pt, Poly: curRef, T: float32(curT), Hit: true}, true
		}
		if tExit < curT {
			tExit = curT
		}
		if tExit >= 1.0-1e-9 {
			if poly.ContainsXZ(target.X, target.Z) {
				pt := math3d.Vec3{X: target.X, Y: poly.HeightAt(target.X, target.Z), Z: target.Z}
				return TiledNavRaycastHit{Point: pt, Poly: curRef, T: 1, Hit: false}, false
			}
			tExit = 1.0
		}
		ex := sx + dx*tExit
		ez := sz + dz*tExit

		nextLoc := polyLoc3D{slot: -1, poly: -1}
		bestNextT := tExit + 1e-9
		reached := false
		for _, lid := range slot.polyLinks[curLoc.poly] {
			tl := slot.links[lid]
			cand := polyLoc3D{slot: tl.toSlot, poly: tl.toPoly}
			if visited[cand] {
				continue
			}
			ns := tm.slots[tl.toSlot]
			if ns == nil {
				continue
			}
			np := &ns.polys[tl.toPoly]
			if !filter.Allows(np.Area, np.Flags) {
				continue
			}
			if !pointOnSegmentXZ(ex, ez, tl.left, tl.right, sharedEdgeEps) {
				continue
			}
			if np.ContainsXZ(target.X, target.Z) {
				nextLoc = cand
				reached = true
				break
			}
			nt, _, _, nok := polyRayExitXZ(np, sx, sz, dx, dz)
			if nok && nt > bestNextT {
				bestNextT = nt
				nextLoc = cand
			}
		}
		if reached || nextLoc.slot >= 0 {
			curLoc = nextLoc
			curT = tExit
			visited[curLoc] = true
			if reached {
				ns := tm.slots[curLoc.slot]
				np := &ns.polys[curLoc.poly]
				ref := NavPolyRef{Tile: ns.coord, Poly: curLoc.poly, Salt: ns.salt}
				pt := math3d.Vec3{X: target.X, Y: np.HeightAt(target.X, target.Z), Z: target.Z}
				return TiledNavRaycastHit{Point: pt, Poly: ref, T: 1, Hit: false}, false
			}
			continue
		}

		hx, hz := float32(ex), float32(ez)
		return TiledNavRaycastHit{
			Point:  math3d.Vec3{X: hx, Y: poly.HeightAt(hx, hz), Z: hz},
			Normal: exitNorm,
			Poly:   curRef,
			T:      float32(min(max(tExit, 0), 1)),
			Hit:    true,
		}, true
	}

	ex, ez := float32(sx+dx*curT), float32(sz+dz*curT)
	poly := &tm.slots[curLoc.slot].polys[curLoc.poly]
	return TiledNavRaycastHit{
		Point: math3d.Vec3{X: ex, Y: poly.HeightAt(ex, ez), Z: ez},
		Poly:  curRef,
		T:     float32(min(max(curT, 0), 1)),
		Hit:   true,
	}, true
}

// --- Sparse 3D Portal-Graph A* Pathfinding across Tiles ---

type polyLoc3D struct {
	slot int32
	poly int32
}

func (tm *TiledNavMesh) findPolyLocsAt(p math3d.Vec3, filter NavFilter, requireFiniteCost bool) []polyLoc3D {
	tol := tm.verticalTol()
	const xzPad float32 = 1e-4
	const levelEps float32 = 1e-4
	minX, maxX := p.X-xzPad, p.X+xzPad
	minY, maxY := p.Y-tol-xzPad, p.Y+tol+xzPad
	minZ, maxZ := p.Z-xzPad, p.Z+xzPad

	var coordBuf [4]ChunkCoord
	coords := tm.candidateTilesAtXZ(float64(p.X), float64(p.Z), coordBuf[:0])

	type cand struct {
		loc polyLoc3D
		dy  float32
	}
	var cands []cand
	bestDY := tol + 1

	for _, c := range coords {
		slot := tm.tiles[c]
		if slot == nil || len(slot.polys) == 0 {
			continue
		}
		consider := func(idx int) {
			poly := &slot.polys[idx]
			if !filter.Allows(poly.Area, poly.Flags) {
				return
			}
			if requireFiniteCost {
				cost := filter.AreaCost(poly.Area)
				if cost <= 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
					return
				}
			}
			if !poly.ContainsXZ(p.X, p.Z) {
				return
			}
			dy := float32(math.Abs(float64(p.Y - poly.HeightAt(p.X, p.Z))))
			if dy <= tol {
				cands = append(cands, cand{loc: polyLoc3D{slot: slot.slotID, poly: int32(idx)}, dy: dy})
				if dy < bestDY {
					bestDY = dy
				}
			}
		}

		if len(slot.bvhNodes) == 0 {
			for i := range slot.polys {
				consider(i)
			}
		} else {
			var stack [64]int32
			stack[0] = 0
			sp := 1
			for sp > 0 {
				sp--
				n := &slot.bvhNodes[stack[sp]]
				if n.bounds.Max.X < minX || n.bounds.Min.X > maxX ||
					n.bounds.Max.Y < minY || n.bounds.Min.Y > maxY ||
					n.bounds.Max.Z < minZ || n.bounds.Min.Z > maxZ {
					continue
				}
				if n.count > 0 {
					end := int(n.left + n.count)
					for k := int(n.left); k < end; k++ {
						consider(int(slot.bvhOrder[k]))
					}
					continue
				}
				stack[sp] = n.right
				sp++
				stack[sp] = n.left
				sp++
			}
		}
	}

	if len(cands) == 0 {
		return nil
	}
	out := make([]polyLoc3D, 0, len(cands))
	for _, item := range cands {
		if item.dy <= bestDY+levelEps {
			out = append(out, item.loc)
		}
	}
	slices.SortFunc(out, func(a, b polyLoc3D) int {
		sa, sb := tm.slots[a.slot], tm.slots[b.slot]
		if c := compareChunkCoord(sa.coord, sb.coord); c != 0 {
			return c
		}
		if a.poly < b.poly {
			return -1
		}
		if a.poly > b.poly {
			return 1
		}
		return 0
	})
	return out
}

// FindPath computes the shortest 3D path across tm from start to goal using DefaultNavFilter.
func (tm *TiledNavMesh) FindPath(start, goal math3d.Vec3) ([]math3d.Vec3, bool) {
	return tm.FindPathWithFilter(start, goal, DefaultNavFilter())
}

// FindPathWithFilter computes the minimum-cost 3D path across tm restricted to filter.
func (tm *TiledNavMesh) FindPathWithFilter(start, goal math3d.Vec3, filter NavFilter) ([]math3d.Vec3, bool) {
	res := tm.FindPathWithFilterAndOptions(start, goal, filter, ai.PathOptions{})
	if !res.Found || res.Partial {
		return nil, false
	}
	return res.Path, true
}

// FindPathWithOptions computes a 3D path under DefaultNavFilter and opts, returning the full
// TiledNavPathResult with crossed tile versions for invalidation checking.
func (tm *TiledNavMesh) FindPathWithOptions(start, goal math3d.Vec3, opts ai.PathOptions) TiledNavPathResult {
	return tm.FindPathWithFilterAndOptions(start, goal, DefaultNavFilter(), opts)
}

// FindCorridor computes the sequence of 3D NavPolyRefs from start to goal under DefaultNavFilter.
func (tm *TiledNavMesh) FindCorridor(start, goal math3d.Vec3) ([]NavPolyRef, bool) {
	return tm.FindCorridorWithFilter(start, goal, DefaultNavFilter())
}

// FindCorridorWithFilter computes the sequence of 3D NavPolyRefs from start to goal restricted to filter.
func (tm *TiledNavMesh) FindCorridorWithFilter(start, goal math3d.Vec3, filter NavFilter) ([]NavPolyRef, bool) {
	res := tm.FindPathWithFilterAndOptions(start, goal, filter, ai.PathOptions{})
	if !res.Found || res.Partial {
		return nil, false
	}
	return res.Corridor, true
}

type tiledAStarNode3D struct {
	idx       int
	f         float64
	h         float64
	tieTile   ChunkCoord
	tiePoly   int32
	tieToTile ChunkCoord
	tieToPoly int32
}

func tiledAStarLess3D(a, b tiledAStarNode3D) bool {
	if a.f != b.f {
		return a.f < b.f
	}
	if a.h != b.h {
		return a.h < b.h
	}
	if c := compareChunkCoord(a.tieTile, b.tieTile); c != 0 {
		return c < 0
	}
	if a.tiePoly != b.tiePoly {
		return a.tiePoly < b.tiePoly
	}
	if c := compareChunkCoord(a.tieToTile, b.tieToTile); c != 0 {
		return c < 0
	}
	return a.tieToPoly < b.tieToPoly
}

func tiledHeapPush3D(open *[]tiledAStarNode3D, heapPos []int, n tiledAStarNode3D) {
	pos := len(*open)
	*open = append(*open, n)
	heapPos[n.idx] = pos
	tiledSiftUp3D(*open, heapPos, pos)
}

func tiledHeapPop3D(open *[]tiledAStarNode3D, heapPos []int) tiledAStarNode3D {
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
	tiledSiftDown3D(s, heapPos, 0)
	return top
}

func tiledSiftUp3D(s []tiledAStarNode3D, heapPos []int, i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !tiledAStarLess3D(s[i], s[p]) {
			break
		}
		s[i], s[p] = s[p], s[i]
		heapPos[s[i].idx] = i
		heapPos[s[p].idx] = p
		i = p
	}
}

func tiledSiftDown3D(s []tiledAStarNode3D, heapPos []int, i int) {
	n := len(s)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && tiledAStarLess3D(s[r], s[l]) {
			best = r
		}
		if !tiledAStarLess3D(s[best], s[i]) {
			break
		}
		s[i], s[best] = s[best], s[i]
		heapPos[s[i].idx] = i
		heapPos[s[best].idx] = best
		i = best
	}
}

// FindPathWithFilterAndOptions finds a minimum-cost 3D path across tm restricted to filter and opts.
// Sparse portal-graph A* touches only visited portals and runs 3D XZ-funnel smoothing with exact
// plane-transition crossings so paths across tile borders match an untiled world3d.NavMesh.
func (tm *TiledNavMesh) FindPathWithFilterAndOptions(start, goal math3d.Vec3, filter NavFilter, opts ai.PathOptions) TiledNavPathResult {
	var res TiledNavPathResult
	if tm == nil || len(tm.tiles) == 0 || !isFiniteVec3(start) || !isFiniteVec3(goal) {
		return res
	}

	startLocs := tm.findPolyLocsAt(start, filter, true)
	if len(startLocs) == 0 {
		return res
	}
	start.Y = tm.slots[startLocs[0].slot].polys[startLocs[0].poly].HeightAt(start.X, start.Z)

	goalLocs := tm.findPolyLocsAt(goal, filter, true)
	hasGoal := len(goalLocs) > 0
	if hasGoal {
		goal.Y = tm.slots[goalLocs[0].slot].polys[goalLocs[0].poly].HeightAt(goal.X, goal.Z)
	} else if !opts.AllowPartial {
		return res
	}

	goalSet := make(map[polyLoc3D]bool, len(goalLocs))
	for _, gl := range goalLocs {
		goalSet[gl] = true
	}

	minCost := minFilterCost3D(filter)

	directPossible := false
	directCost := math.Inf(1)
	var directLoc polyLoc3D
	if hasGoal {
		for _, sl := range startLocs {
			if goalSet[sl] {
				if start.ApproxEqual(goal, 1e-5) {
					slot := tm.slots[sl.slot]
					ref := NavPolyRef{Tile: slot.coord, Poly: sl.poly, Salt: slot.salt}
					return TiledNavPathResult{
						Path:     []math3d.Vec3{start},
						Corridor: []NavPolyRef{ref},
						Tiles:    []NavTileVersion{{Coord: slot.coord, Version: tm.tileVersions[slot.coord]}},
						Found:    true,
					}
				}
				c := filter.AreaCost(tm.slots[sl.slot].polys[sl.poly].Area)
				dc := dist3D(start, goal) * c
				if dc < directCost {
					directCost = dc
					directPossible = true
					directLoc = sl
				}
			}
		}
		if directPossible && directCost <= dist3D(start, goal)*minCost+1e-9 {
			slot := tm.slots[directLoc.slot]
			ref := NavPolyRef{Tile: slot.coord, Poly: directLoc.poly, Salt: slot.salt}
			return TiledNavPathResult{
				Path:     []math3d.Vec3{start, goal},
				Corridor: []NavPolyRef{ref},
				Tiles:    []NavTileVersion{{Coord: slot.coord, Version: tm.tileVersions[slot.coord]}},
				Found:    true,
			}
		}
	}

	type visitedLink struct {
		slotID   int32
		linkIdx  int32
		parent   int
		gScore   float64
		fScore   float64
		entryPt  math3d.Vec3
		apex     math3d.Vec3
		apexG    float64
		fLeft    math3d.Vec3
		fRight   math3d.Vec3
		areaCost float64
		plane    plane3D
		closed   bool
	}
	nodes := []visitedLink{{slotID: -1, linkIdx: -1, parent: -1, gScore: math.Inf(1), fScore: math.Inf(1)}}
	heapPos := []int{-1}
	linkToIdx := make(map[uint64]int, 64)
	open := make([]tiledAStarNode3D, 0, 64)

	linkKey := func(slotID, lid int32) uint64 {
		return (uint64(uint32(slotID)) << 32) | uint64(uint32(lid))
	}

	maxCoord := ChunkCoord{X: math.MaxInt32, Z: math.MaxInt32}
	pushOrUpdate := func(slotID, lid int32, g, h float64, parent int, pt, apex, fLeft, fRight math3d.Vec3, apexG, areaCost float64, pl plane3D, tieTile ChunkCoord, tiePoly int32, tieToTile ChunkCoord, tieToPoly int32) {
		f := g + h
		var idx int
		if slotID < 0 {
			idx = 0
		} else {
			key := linkKey(slotID, lid)
			var exists bool
			idx, exists = linkToIdx[key]
			if !exists {
				idx = len(nodes)
				linkToIdx[key] = idx
				nodes = append(nodes, visitedLink{
					slotID:   slotID,
					linkIdx:  lid,
					parent:   parent,
					gScore:   g,
					fScore:   f,
					entryPt:  pt,
					apex:     apex,
					apexG:    apexG,
					fLeft:    fLeft,
					fRight:   fRight,
					areaCost: areaCost,
					plane:    pl,
				})
				heapPos = append(heapPos, -1)
				tiledHeapPush3D(&open, heapPos, tiledAStarNode3D{
					idx: idx, f: f, h: h,
					tieTile: tieTile, tiePoly: tiePoly, tieToTile: tieToTile, tieToPoly: tieToPoly,
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
		nodes[idx].plane = pl
		nodes[idx].closed = false
		pos := heapPos[idx]
		if pos >= 0 {
			open[pos].f = f
			open[pos].h = h
			tiledSiftUp3D(open, heapPos, pos)
			return
		}
		tiledHeapPush3D(&open, heapPos, tiledAStarNode3D{
			idx: idx, f: f, h: h,
			tieTile: tieTile, tiePoly: tiePoly, tieToTile: tieToTile, tieToPoly: tieToPoly,
		})
	}

	hasBest := false
	bestNodeIdx := -1
	bestStartLoc := startLocs[0]
	var bestPt math3d.Vec3
	var bestH, bestG float64
	if opts.AllowPartial {
		for _, sl := range startLocs {
			poly := &tm.slots[sl.slot].polys[sl.poly]
			cp := closestPointOnNavPoly(poly, goal)
			h := dist3D(cp, goal)
			g := dist3D(start, cp) * filter.AreaCost(poly.Area)
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

	if directPossible {
		pushOrUpdate(-1, -1, directCost, 0, -1, goal, start, goal, goal, 0, minCost, plane3D{}, maxCoord, math.MaxInt32, maxCoord, math.MaxInt32)
	}

	for _, sl := range startLocs {
		slot := tm.slots[sl.slot]
		sp := &slot.polys[sl.poly]
		spCost := filter.AreaCost(sp.Area)
		spPlane := plane3D{normal: sp.Normal, planeD: sp.PlaneD}
		for _, lid := range slot.polyLinks[sl.poly] {
			tl := slot.links[lid]
			dst := tm.slots[tl.toSlot]
			if dst == nil {
				continue
			}
			dp := &dst.polys[tl.toPoly]
			if !filter.Allows(dp.Area, dp.Flags) {
				continue
			}
			dc := filter.AreaCost(dp.Area)
			if dc <= 0 || math.IsNaN(dc) || math.IsInf(dc, 0) {
				continue
			}
			exitPt := bestPortalPoint3D(start, goal, tl.left, tl.right)
			g := dist3D(start, exitPt) * spCost
			h := dist3D(exitPt, goal) * minCost
			pushOrUpdate(sl.slot, int32(lid), g, h, -1, exitPt, start, tl.left, tl.right, 0, spCost, spPlane, slot.coord, sl.poly, dst.coord, tl.toPoly)
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
		cur := tiledHeapPop3D(&open, heapPos)
		expansions++
		if cur.idx == 0 {
			foundComplete = true
			break
		}
		nodes[cur.idx].closed = true
		vl := nodes[cur.idx]
		fromSlot := tm.slots[vl.slotID]
		tl := fromSlot.links[vl.linkIdx]
		toSlot := tm.slots[tl.toSlot]
		toPoly := &toSlot.polys[tl.toPoly]
		toCost := filter.AreaCost(toPoly.Area)
		toPlane := plane3D{normal: toPoly.Normal, planeD: toPoly.PlaneD}
		sameFunnel := math.Abs(toCost-vl.areaCost) <= 1e-12 && samePlane3D(toPlane, vl.plane)

		if opts.AllowPartial {
			cp := closestPointOnNavPoly(toPoly, goal)
			h := dist3D(cp, goal)
			var g float64
			if sameFunnel {
				g = funnelPointDist3D(vl.apex, vl.fLeft, vl.fRight, cp, vl.apexG, toCost)
			} else {
				g = vl.gScore + dist3D(vl.entryPt, cp)*toCost
			}
			if !hasBest || h < bestH || (h == bestH && g < bestG) {
				hasBest = true
				bestNodeIdx = cur.idx
				bestPt = cp
				bestH = h
				bestG = g
			}
		}

		toLoc := polyLoc3D{slot: tl.toSlot, poly: tl.toPoly}
		if goalSet[toLoc] {
			var ng float64
			if sameFunnel {
				ng = funnelPointDist3D(vl.apex, vl.fLeft, vl.fRight, goal, vl.apexG, toCost)
			} else {
				ng = vl.gScore + dist3D(vl.entryPt, goal)*toCost
			}
			pushOrUpdate(-1, -1, ng, 0, cur.idx, goal, vl.apex, vl.fLeft, vl.fRight, vl.apexG, toCost, toPlane, maxCoord, math.MaxInt32, maxCoord, math.MaxInt32)
		}

		for _, nlid := range toSlot.polyLinks[tl.toPoly] {
			ntl := toSlot.links[nlid]
			if ntl.toSlot == vl.slotID && ntl.toPoly == tl.fromPoly {
				continue
			}
			ndst := tm.slots[ntl.toSlot]
			if ndst == nil {
				continue
			}
			ndp := &ndst.polys[ntl.toPoly]
			if !filter.Allows(ndp.Area, ndp.Flags) {
				continue
			}
			ndc := filter.AreaCost(ndp.Area)
			if ndc <= 0 || math.IsNaN(ndc) || math.IsInf(ndc, 0) {
				continue
			}
			var ng, newApexG float64
			var exitPt, newApex, newLeft, newRight math3d.Vec3
			if sameFunnel {
				newApex, newLeft, newRight, newApexG = advanceFunnel3D(vl.apex, vl.fLeft, vl.fRight, ntl.left, ntl.right, vl.apexG, toCost)
				exitPt = funnelPortalPoint3D(newApex, newLeft, newRight, goal, ntl.left, ntl.right)
				ng = funnelPointDist3D(newApex, newLeft, newRight, exitPt, newApexG, toCost)
			} else {
				exitPt = bestPortalPoint3D(vl.entryPt, goal, ntl.left, ntl.right)
				ng = vl.gScore + dist3D(vl.entryPt, exitPt)*toCost
				newApex = vl.entryPt
				newApexG = vl.gScore
				newLeft = ntl.left
				newRight = ntl.right
			}
			h := dist3D(exitPt, goal) * minCost
			pushOrUpdate(tl.toSlot, int32(nlid), ng, h, cur.idx, exitPt, newApex, newLeft, newRight, newApexG, toCost, toPlane, toSlot.coord, tl.toPoly, ndst.coord, ntl.toPoly)
		}
	}

	res.Expansions = expansions
	res.Exhausted = exhausted

	buildChainResult := func(lastIdx int, endPt math3d.Vec3) {
		var chain []tiledLink3D
		for id := lastIdx; id != -1; id = nodes[id].parent {
			vl := nodes[id]
			chain = append(chain, tm.slots[vl.slotID].links[vl.linkIdx])
		}
		slices.Reverse(chain)
		firstNode := lastIdx
		for nodes[firstNode].parent != -1 {
			firstNode = nodes[firstNode].parent
		}
		firstSlot := tm.slots[nodes[firstNode].slotID]
		corridor := make([]NavPolyRef, len(chain)+1)
		polyPtrs := make([]*NavPoly, len(chain)+1)
		corridor[0] = NavPolyRef{Tile: firstSlot.coord, Poly: chain[0].fromPoly, Salt: firstSlot.salt}
		polyPtrs[0] = &firstSlot.polys[chain[0].fromPoly]
		for i, tl := range chain {
			dst := tm.slots[tl.toSlot]
			corridor[i+1] = NavPolyRef{Tile: dst.coord, Poly: tl.toPoly, Salt: dst.salt}
			polyPtrs[i+1] = &dst.polys[tl.toPoly]
		}
		res.Corridor = corridor
		res.Tiles = tm.collectTileVersions(corridor)
		res.Path = tm.tightenTiledPath3D(smoothCorridorTiled3D(polyPtrs, chain, start, endPt), corridor, polyPtrs, filter)
	}

	if foundComplete {
		res.Found = true
		if nodes[0].parent == -1 {
			slot := tm.slots[directLoc.slot]
			ref := NavPolyRef{Tile: slot.coord, Poly: directLoc.poly, Salt: slot.salt}
			res.Corridor = []NavPolyRef{ref}
			res.Tiles = []NavTileVersion{{Coord: slot.coord, Version: tm.tileVersions[slot.coord]}}
			if start.ApproxEqual(goal, 1e-5) {
				res.Path = []math3d.Vec3{start}
			} else {
				res.Path = []math3d.Vec3{start, goal}
			}
			return res
		}
		buildChainResult(nodes[0].parent, goal)
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
			if start.ApproxEqual(bestPt, 1e-5) {
				res.Path = []math3d.Vec3{start}
			} else {
				res.Path = []math3d.Vec3{start, bestPt}
			}
			return res
		}
		buildChainResult(bestNodeIdx, bestPt)
		return res
	}

	return res
}

func (tm *TiledNavMesh) collectTileVersions(corridor []NavPolyRef) []NavTileVersion {
	if len(corridor) == 0 {
		return nil
	}
	coords := make([]ChunkCoord, 0, len(corridor))
	for _, ref := range corridor {
		coords = append(coords, ref.Tile)
	}
	slices.SortFunc(coords, compareChunkCoord)
	coords = slices.Compact(coords)
	out := make([]NavTileVersion, len(coords))
	for i, c := range coords {
		out[i] = NavTileVersion{Coord: c, Version: tm.tileVersions[c]}
	}
	return out
}

func advanceFunnel3D(apex, fLeft, fRight, nLeft, nRight math3d.Vec3, apexG, cost float64) (newApex, newLeft, newRight math3d.Vec3, newApexG float64) {
	newApex, newLeft, newRight, newApexG = apex, fLeft, fRight, apexG
	if triArea2(newApex, newRight, nRight) <= 0 {
		if sameXZ(newApex, newRight) || sameXZ(newApex, newLeft) || triArea2(newApex, newLeft, nRight) > 0 {
			newRight = nRight
		} else {
			newApexG += dist3D(newApex, newLeft) * cost
			newApex = newLeft
			newLeft = nLeft
			newRight = nRight
			return
		}
	}
	if triArea2(newApex, newLeft, nLeft) >= 0 {
		if sameXZ(newApex, newLeft) || sameXZ(newApex, newRight) || triArea2(newApex, newRight, nLeft) < 0 {
			newLeft = nLeft
		} else {
			newApexG += dist3D(newApex, newRight) * cost
			newApex = newRight
			newLeft = nLeft
			newRight = nRight
			return
		}
	}
	return
}

func funnelPortalPoint3D(apex, fLeft, fRight, goal, pLeft, pRight math3d.Vec3) math3d.Vec3 {
	if !sameXZ(apex, fLeft) && triArea2(apex, fLeft, goal) < 0 {
		return bestPortalPoint3D(fLeft, goal, pLeft, pRight)
	}
	if !sameXZ(apex, fRight) && triArea2(apex, fRight, goal) > 0 {
		return bestPortalPoint3D(fRight, goal, pLeft, pRight)
	}
	return bestPortalPoint3D(apex, goal, pLeft, pRight)
}

func funnelPointDist3D(apex, fLeft, fRight, target math3d.Vec3, apexG, cost float64) float64 {
	if !sameXZ(apex, fLeft) && triArea2(apex, fLeft, target) < 0 {
		return apexG + (dist3D(apex, fLeft)+dist3D(fLeft, target))*cost
	}
	if !sameXZ(apex, fRight) && triArea2(apex, fRight, target) > 0 {
		return apexG + (dist3D(apex, fRight)+dist3D(fRight, target))*cost
	}
	return apexG + dist3D(apex, target)*cost
}

// simplifyCollinear3D removes intermediate waypoints lying strictly on the 3D segment between
// their neighbors (so flat coplanar tile-border corners collapse while slope bends remain exact).
func simplifyCollinear3D(pts []math3d.Vec3) []math3d.Vec3 {
	if len(pts) <= 2 {
		return pts
	}
	out := make([]math3d.Vec3, 0, len(pts))
	out = append(out, pts[0])
	for i := 1; i < len(pts)-1; i++ {
		a := out[len(out)-1]
		b := pts[i]
		c := pts[i+1]
		ac := c.Sub(a)
		acLen := float64(ac.Len())
		if acLen > 1e-9 {
			ab := b.Sub(a)
			// Perpendicular 3D distance from b to line a->c.
			crossLen := float64(ab.Cross(ac).Len())
			if crossLen <= 1e-4*acLen*acLen {
				t := float64(ab.Dot(ac)) / (acLen * acLen)
				if t >= -1e-4 && t <= 1+1e-4 {
					continue
				}
			}
		}
		out = append(out, b)
	}
	out = append(out, pts[len(pts)-1])
	return out
}

// tightenTiledPath3D removes artificial bends at interior tile-grid vertices on uniform-cost
// coplanar runs while preserving slope transitions and vertical multi-level separation.
func (tm *TiledNavMesh) tightenTiledPath3D(pts []math3d.Vec3, corridor []NavPolyRef, polys []*NavPoly, filter NavFilter) []math3d.Vec3 {
	pts = simplifyCollinear3D(pts)
	if len(pts) <= 2 || len(corridor) == 0 {
		return pts
	}
	refCost := filter.AreaCost(polys[0].Area)
	for _, p := range polys[1:] {
		if math.Abs(filter.AreaCost(p.Area)-refCost) > 1e-9 {
			return pts
		}
	}

	rayFilter := filter
	if len(filter.AreaCosts) > 0 {
		rayFilter = filter.Clone()
		for area, c := range filter.AreaCosts {
			if c > refCost+1e-9 {
				rayFilter.ExcludeArea(area)
			}
		}
	}

	// Only shortcut a -> c when a, b, c all lie on the same plane (preserving slope hinges).
	samePlaneTriple := func(a, b, c math3d.Vec3) bool {
		ra, oka := tm.FindPolygonWithFilter(a, rayFilter)
		rb, okb := tm.FindPolygonWithFilter(b, rayFilter)
		rc, okc := tm.FindPolygonWithFilter(c, rayFilter)
		if !oka || !okb || !okc {
			return false
		}
		_, pa := tm.resolveRef(ra)
		_, pb := tm.resolveRef(rb)
		_, pc := tm.resolveRef(rc)
		return pa != nil && pb != nil && pc != nil && samePlane(pa, pb) && samePlane(pb, pc)
	}

	out := make([]math3d.Vec3, len(pts))
	copy(out, pts)

	for pass := 0; pass < 4 && len(out) > 2; pass++ {
		changed := false
		for k := 1; k+1 < len(out); k++ {
			a, b, c := out[k-1], out[k], out[k+1]
			if !samePlaneTriple(a, b, c) {
				continue
			}
			hit, blocked := tm.RaycastWithFilter(a, c, rayFilter)
			if !blocked {
				out = append(out[:k], out[k+1:]...)
				changed = true
				k--
				continue
			}
			_, hitPoly := tm.resolveRef(hit.Poly)
			if hitPoly == nil {
				continue
			}
			curLen := dist3D(a, b) + dist3D(b, c)
			bestCand := b
			bestLen := curLen - 1e-5
			vs := hitPoly.Vertices
			for e := range len(vs) {
				v0, v1 := vs[e], vs[(e+1)%len(vs)]
				if !pointOnSegmentXZ(float64(hit.Point.X), float64(hit.Point.Z), v0, v1, sharedEdgeEps*2) {
					continue
				}
				for _, cand := range [2]math3d.Vec3{v0, v1} {
					candLen := dist3D(a, cand) + dist3D(cand, c)
					if candLen >= bestLen {
						continue
					}
					if _, h1 := tm.RaycastWithFilter(a, cand, rayFilter); h1 {
						continue
					}
					if _, h2 := tm.RaycastWithFilter(cand, c, rayFilter); h2 {
						continue
					}
					bestLen = candLen
					bestCand = cand
				}
			}
			if !bestCand.ApproxEqual(b, 1e-4) {
				out[k] = bestCand
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return simplifyCollinear3D(out)
}

func smoothCorridorTiled3D(polys []*NavPoly, portals []tiledLink3D, start, goal math3d.Vec3) []math3d.Vec3 {
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
		portalLeft[i+1] = p.left
		portalRight[i+1] = p.right
	}
	portalLeft[m-1] = goal
	portalRight[m-1] = goal

	corners := []funnelCorner{{pos: start, portalIdx: 0}}
	portalApex := start
	leftPt := start
	rightPt := start
	apexIdx, leftIdx, rightIdx := 0, 0, 0

	for i := 1; i < m; i++ {
		left := portalLeft[i]
		right := portalRight[i]

		if triArea2(portalApex, rightPt, right) <= 0 {
			if sameXZ(portalApex, rightPt) || sameXZ(portalApex, leftPt) || triArea2(portalApex, leftPt, right) > 0 {
				rightPt = right
				rightIdx = i
			} else {
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

		if triArea2(portalApex, leftPt, left) >= 0 {
			if sameXZ(portalApex, leftPt) || sameXZ(portalApex, rightPt) || triArea2(portalApex, rightPt, left) < 0 {
				leftPt = left
				leftIdx = i
			} else {
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

	out := make([]math3d.Vec3, 0, len(corners)+len(portals))
	out = append(out, corners[0].pos)
	for c := 1; c < len(corners); c++ {
		prev := corners[c-1]
		next := corners[c]
		for j := prev.portalIdx + 1; j < next.portalIdx; j++ {
			pA := polys[j-1]
			pB := polys[j]
			if samePlane(pA, pB) {
				continue
			}
			link := portals[j-1]
			s := intersectXZPortalParam(prev.pos, next.pos, link.left, link.right)
			cross := link.left.Lerp(link.right, s)
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
