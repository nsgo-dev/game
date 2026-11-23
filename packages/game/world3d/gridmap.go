package world3d

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// GridOrientationCount is the number of distinct proper 3D orthogonal
// 90-degree rotations supported by GridCell.Orientation (the 24 rotational
// symmetries of a cube). Orientations 0..3 are 0°, 90°, 180°, and 270°
// counter-clockwise quarter-turns around the +Y axis.
const GridOrientationCount = 24

const gridChunkDim = 8

// gridOrientations holds exact {-1, 0, 1} right-handed 4x4 rotation matrices
// for all 24 orthogonal cube orientations. Indices 0..3 are the 4 +Y quarter-turns.
var gridOrientations = buildGridOrientations()

func buildGridOrientations() [GridOrientationCount]math3d.Mat4 {
	// Six base orientations placing local +Y along +Y, -Y, +X, -X, +Z, -Z,
	// each a proper right-handed rotation (det = +1).
	bases := [6]math3d.Mat3{
		// 0: +Y up (identity)
		{
			1, 0, 0,
			0, 1, 0,
			0, 0, 1,
		},
		// 1: -Y up (Rx 180°)
		{
			1, 0, 0,
			0, -1, 0,
			0, 0, -1,
		},
		// 2: +X up (Rz -90°: local +Y -> +X, local +X -> -Y)
		{
			0, -1, 0,
			1, 0, 0,
			0, 0, 1,
		},
		// 3: -X up (Rz +90°: local +Y -> -X, local +X -> +Y)
		{
			0, 1, 0,
			-1, 0, 0,
			0, 0, 1,
		},
		// 4: +Z up (Rx +90°: local +Y -> +Z, local +Z -> -Y)
		{
			1, 0, 0,
			0, 0, 1,
			0, -1, 0,
		},
		// 5: -Z up (Rx -90°: local +Y -> -Z, local +Z -> +Y)
		{
			1, 0, 0,
			0, 0, -1,
			0, 1, 0,
		},
	}

	// Four exact right-handed quarter-turns around +Y (0°, 90°, 180°, 270°).
	// Column-major: c0 = (cos, 0, -sin), c1 = (0, 1, 0), c2 = (sin, 0, cos).
	yTurns := [4]math3d.Mat3{
		{
			1, 0, 0,
			0, 1, 0,
			0, 0, 1,
		},
		{
			0, 0, -1,
			0, 1, 0,
			1, 0, 0,
		},
		{
			-1, 0, 0,
			0, 1, 0,
			0, 0, -1,
		},
		{
			0, 0, 1,
			0, 1, 0,
			-1, 0, 0,
		},
	}

	var out [GridOrientationCount]math3d.Mat4
	for b := range 6 {
		for k := range 4 {
			m := bases[b].Mul(yTurns[k])
			out[b*4+k] = math3d.Mat4{
				m[0], m[1], m[2], 0,
				m[3], m[4], m[5], 0,
				m[6], m[7], m[8], 0,
				0, 0, 0, 1,
			}
		}
	}
	return out
}

// GridOrientationMat4 returns the exact orthogonal 4x4 rotation matrix for
// orientation index orient (modulo GridOrientationCount). Orientations 0..3
// correspond to 0°, 90°, 180°, and 270° rotations around the +Y axis.
func GridOrientationMat4(orient uint8) math3d.Mat4 {
	return gridOrientations[orient%GridOrientationCount]
}

// MeshTileDef describes a modular 3D tile stored in a MeshLibrary.
type MeshTileDef struct {
	ID            uint16
	Name          string
	MeshID        uint32
	MaterialID    uint32
	LocalBounds   math3d.AABB
	ColliderBoxes []math3d.AABB
}

// MeshLibrary is a registry of MeshTileDef entries indexed by ID and Name.
// Tile ID 0 is reserved to denote an empty grid cell.
type MeshLibrary struct {
	byID     map[uint16]MeshTileDef
	byName   map[string]uint16
	nextID   uint16
	revision uint64
}

// NewMeshLibrary returns an empty MeshLibrary ready to register tile definitions.
func NewMeshLibrary() *MeshLibrary {
	return &MeshLibrary{
		byID:   make(map[uint16]MeshTileDef),
		byName: make(map[string]uint16),
		nextID: 1,
	}
}

// Add registers def in the library and returns its assigned non-zero tile ID.
// If def.ID is 0, the next available ID starting from 1 is assigned. If def.ID
// already exists in the library, its definition is replaced. When all 65535 IDs are taken, Add registers nothing and returns 0 (#546).
func (l *MeshLibrary) Add(def MeshTileDef) uint16 {
	if l.byID == nil {
		l.byID = make(map[uint16]MeshTileDef)
		l.byName = make(map[string]uint16)
		l.nextID = 1
	}
	if def.ID == 0 {
		if len(l.byID) >= math.MaxUint16 {
			return 0
		}
		for l.nextID == 0 || l.hasID(l.nextID) {
			l.nextID++
		}
		def.ID = l.nextID
		l.nextID++
	} else if def.ID >= l.nextID {
		l.nextID = def.ID + 1
	}

	if old, exists := l.byID[def.ID]; exists && old.Name != "" && old.Name != def.Name {
		if curID, ok := l.byName[old.Name]; ok && curID == def.ID {
			delete(l.byName, old.Name)
		}
	}

	if len(def.ColliderBoxes) > 0 {
		cloned := make([]math3d.AABB, len(def.ColliderBoxes))
		copy(cloned, def.ColliderBoxes)
		def.ColliderBoxes = cloned
	} else {
		def.ColliderBoxes = nil
	}

	l.byID[def.ID] = def
	if def.Name != "" {
		l.byName[def.Name] = def.ID
	}
	l.revision++
	return def.ID
}

func (l *MeshLibrary) hasID(id uint16) bool {
	_, ok := l.byID[id]
	return ok
}

// Get looks up a tile definition by ID.
func (l *MeshLibrary) Get(id uint16) (MeshTileDef, bool) {
	if l == nil {
		return MeshTileDef{}, false
	}
	def, ok := l.byID[id]
	return def, ok
}

// ByName looks up a tile definition by Name.
func (l *MeshLibrary) ByName(name string) (MeshTileDef, bool) {
	if l == nil || name == "" {
		return MeshTileDef{}, false
	}
	id, ok := l.byName[name]
	if !ok {
		return MeshTileDef{}, false
	}
	def, ok := l.byID[id]
	return def, ok
}

// Len returns the number of tile definitions registered in the library.
func (l *MeshLibrary) Len() int {
	if l == nil {
		return 0
	}
	return len(l.byID)
}

// Revision returns a counter that changes whenever Add replaces or registers a
// definition, so caches derived from the library can invalidate.
func (l *MeshLibrary) Revision() uint64 {
	if l == nil {
		return 0
	}
	return l.revision
}

type gltfLibraryDoc struct {
	Asset struct {
		Version string `json:"version"`
	} `json:"asset"`
	Nodes     []gltfNode     `json:"nodes"`
	Meshes    []gltfMesh     `json:"meshes"`
	Accessors []gltfAccessor `json:"accessors"`
}

type gltfNode struct {
	Name string `json:"name"`
	Mesh *int   `json:"mesh"`
}

type gltfMesh struct {
	Name       string          `json:"name"`
	Primitives []gltfPrimitive `json:"primitives"`
}

type gltfPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Material   *int           `json:"material"`
}

type gltfAccessor struct {
	Min []float32 `json:"min"`
	Max []float32 `json:"max"`
}

// LoadMeshLibraryFromGLTF parses a glTF 2.0 JSON document's nodes, meshes, and
// POSITION accessor min/max bounds to build a MeshLibrary without external
// dependencies. Each node referencing a mesh becomes a MeshTileDef whose
// MeshID is the glTF mesh index, MaterialID is the first primitive's material
// index (or 0 if omitted), LocalBounds is the union of all primitive POSITION
// accessor bounds, and ColliderBoxes defaults to LocalBounds when present.
func LoadMeshLibraryFromGLTF(gltfJSON []byte) (*MeshLibrary, error) {
	if len(gltfJSON) == 0 {
		return nil, errors.New("world3d: empty glTF JSON")
	}
	var doc gltfLibraryDoc
	if err := json.Unmarshal(gltfJSON, &doc); err != nil {
		return nil, fmt.Errorf("world3d: parse glTF JSON: %w", err)
	}
	if doc.Asset.Version != "" && !strings.HasPrefix(doc.Asset.Version, "2.") {
		return nil, fmt.Errorf("world3d: unsupported glTF version %q", doc.Asset.Version)
	}

	lib := NewMeshLibrary()
	addMeshTile := func(name string, meshIdx int) error {
		if meshIdx < 0 || meshIdx >= len(doc.Meshes) {
			return fmt.Errorf("world3d: glTF node %q references invalid mesh index %d", name, meshIdx)
		}
		mesh := doc.Meshes[meshIdx]
		if name == "" {
			name = mesh.Name
		}
		if name == "" {
			name = fmt.Sprintf("mesh_%d", meshIdx)
		}

		var matID uint32
		hasMat := false
		bounds := math3d.AABBEmpty()
		for _, prim := range mesh.Primitives {
			if !hasMat && prim.Material != nil && *prim.Material >= 0 {
				matID = uint32(*prim.Material)
				hasMat = true
			}
			posIdx, hasPos := prim.Attributes["POSITION"]
			if !hasPos {
				continue
			}
			if posIdx < 0 || posIdx >= len(doc.Accessors) {
				return fmt.Errorf("world3d: glTF mesh %d references invalid POSITION accessor %d", meshIdx, posIdx)
			}
			acc := doc.Accessors[posIdx]
			if len(acc.Min) >= 3 && len(acc.Max) >= 3 {
				bounds = bounds.Union(math3d.AABB{
					Min: math3d.Vec3{X: acc.Min[0], Y: acc.Min[1], Z: acc.Min[2]},
					Max: math3d.Vec3{X: acc.Max[0], Y: acc.Max[1], Z: acc.Max[2]},
				})
			}
		}

		var colliders []math3d.AABB
		if !bounds.Empty() {
			colliders = []math3d.AABB{bounds}
		}
		lib.Add(MeshTileDef{
			Name:          name,
			MeshID:        uint32(meshIdx),
			MaterialID:    matID,
			LocalBounds:   bounds,
			ColliderBoxes: colliders,
		})
		return nil
	}

	if len(doc.Nodes) > 0 {
		for i, node := range doc.Nodes {
			if node.Mesh == nil {
				continue
			}
			name := node.Name
			if name == "" && *node.Mesh >= 0 && *node.Mesh < len(doc.Meshes) {
				name = doc.Meshes[*node.Mesh].Name
			}
			if name == "" {
				name = fmt.Sprintf("tile_%d", i)
			}
			if err := addMeshTile(name, *node.Mesh); err != nil {
				return nil, err
			}
		}
	} else {
		for meshIdx := range doc.Meshes {
			if err := addMeshTile(doc.Meshes[meshIdx].Name, meshIdx); err != nil {
				return nil, err
			}
		}
	}

	return lib, nil
}

// GridCoord identifies a discrete 3D cell coordinate in a GridMap.
type GridCoord struct {
	X, Y, Z int
}

// GridCell stores the tile ID and orthogonal orientation (0..23) of a GridMap cell.
type GridCell struct {
	TileID      uint16
	Orientation uint8
}

// GridHit describes the closest intersection between a ray and an occupied GridMap cell.
type GridHit struct {
	Coord    GridCoord
	Cell     GridCell
	Point    math3d.Vec3
	Normal   math3d.Vec3
	Distance float32
}

// GridInstanceBatch groups world-space instance transforms sharing the same
// (MeshID, MaterialID) pair for instanced 3D rendering.
type GridInstanceBatch struct {
	MeshID     uint32
	MaterialID uint32
	Transforms []math3d.Mat4
}

type gridChunkKey struct {
	x, y, z int
}

type gridBatchKey struct {
	meshID     uint32
	materialID uint32
}

type gridChunk struct {
	cells       map[GridCoord]GridCell
	localBounds math3d.AABB // bounds relative to GridMap.Origin
	dirtyBounds bool
}

// GridMap is a sparse 3D grid of modular mesh tiles with runtime editing,
// frustum-culled instanced batch generation, AABB collision queries, and raycasting.
//
// Each cell c spans [Origin + c*CellSize, Origin + (c+1)*CellSize] and places
// its tile's local origin at the center of that cell, rotated by
// GridOrientationMat4(cell.Orientation).
type GridMap struct {
	Origin   math3d.Vec3
	CellSize math3d.Vec3
	Library  *MeshLibrary

	chunks     map[gridChunkKey]*gridChunk
	count      int
	batchIndex map[gridBatchKey]int
	// libRevision is the MeshLibrary revision the chunks' bounds were built
	// from; a changed library invalidates every chunk's cached bounds.
	libRevision uint64
}

// NewGridMap creates an empty GridMap with the given cell dimensions and tile
// library. Any non-positive CellSize axis defaults to 1.0.
func NewGridMap(cellSize math3d.Vec3, lib *MeshLibrary) *GridMap {
	if cellSize.X <= 0 {
		cellSize.X = 1
	}
	if cellSize.Y <= 0 {
		cellSize.Y = 1
	}
	if cellSize.Z <= 0 {
		cellSize.Z = 1
	}
	return &GridMap{
		CellSize:   cellSize,
		Library:    lib,
		chunks:     make(map[gridChunkKey]*gridChunk),
		batchIndex: make(map[gridBatchKey]int),
	}
}

func floorDiv(a, b int) int {
	q := a / b
	r := a % b
	if r < 0 {
		q--
	}
	return q
}

func chunkKeyFor(c GridCoord) gridChunkKey {
	return gridChunkKey{
		x: floorDiv(c.X, gridChunkDim),
		y: floorDiv(c.Y, gridChunkDim),
		z: floorDiv(c.Z, gridChunkDim),
	}
}

// SetCell places tileID with orientation orient (0..23) at grid coordinate c.
// Setting tileID to 0 clears the cell.
func (g *GridMap) SetCell(c GridCoord, tileID uint16, orient uint8) {
	if g == nil {
		return
	}
	if tileID == 0 {
		g.ClearCell(c)
		return
	}
	if g.chunks == nil {
		g.chunks = make(map[gridChunkKey]*gridChunk)
	}
	key := chunkKeyFor(c)
	ch := g.chunks[key]
	if ch == nil {
		ch = &gridChunk{
			cells:       make(map[GridCoord]GridCell),
			dirtyBounds: true,
		}
		g.chunks[key] = ch
	}
	if _, existed := ch.cells[c]; !existed {
		g.count++
	}
	ch.cells[c] = GridCell{
		TileID:      tileID,
		Orientation: orient % GridOrientationCount,
	}
	ch.dirtyBounds = true
}

// ClearCell removes any tile stored at grid coordinate c.
func (g *GridMap) ClearCell(c GridCoord) {
	if g == nil || len(g.chunks) == 0 {
		return
	}
	key := chunkKeyFor(c)
	ch := g.chunks[key]
	if ch == nil {
		return
	}
	if _, existed := ch.cells[c]; !existed {
		return
	}
	delete(ch.cells, c)
	g.count--
	if len(ch.cells) == 0 {
		delete(g.chunks, key)
		return
	}
	ch.dirtyBounds = true
}

// Clear removes all cells from the GridMap.
func (g *GridMap) Clear() {
	if g == nil {
		return
	}
	clear(g.chunks)
	g.count = 0
}

// Count returns the number of occupied cells in the GridMap.
func (g *GridMap) Count() int {
	if g == nil {
		return 0
	}
	return g.count
}

// Cell returns the GridCell at coordinate c, or (GridCell{}, false) if empty.
func (g *GridMap) Cell(c GridCoord) (GridCell, bool) {
	if g == nil || len(g.chunks) == 0 {
		return GridCell{}, false
	}
	ch := g.chunks[chunkKeyFor(c)]
	if ch == nil {
		return GridCell{}, false
	}
	cell, ok := ch.cells[c]
	return cell, ok
}

// FillBox sets every cell in the inclusive coordinate box [minC, maxC] to
// tileID with orientation orient. Component order is normalized automatically
// if any minC axis exceeds maxC. Setting tileID to 0 clears the box.
func (g *GridMap) FillBox(minC, maxC GridCoord, tileID uint16, orient uint8) {
	if g == nil {
		return
	}
	x0, x1 := min(minC.X, maxC.X), max(minC.X, maxC.X)
	y0, y1 := min(minC.Y, maxC.Y), max(minC.Y, maxC.Y)
	z0, z1 := min(minC.Z, maxC.Z), max(minC.Z, maxC.Z)
	for y := y0; y <= y1; y++ {
		for z := z0; z <= z1; z++ {
			for x := x0; x <= x1; x++ {
				g.SetCell(GridCoord{X: x, Y: y, Z: z}, tileID, orient)
			}
		}
	}
}

func (g *GridMap) localCellBounds(c GridCoord) math3d.AABB {
	lo := math3d.Vec3{
		X: float32(c.X) * g.CellSize.X,
		Y: float32(c.Y) * g.CellSize.Y,
		Z: float32(c.Z) * g.CellSize.Z,
	}
	return math3d.AABB{
		Min: lo,
		Max: lo.Add(g.CellSize),
	}
}

// CellBounds returns the world-space axis-aligned bounding box of cell c.
func (g *GridMap) CellBounds(c GridCoord) math3d.AABB {
	b := g.localCellBounds(c)
	return math3d.AABB{
		Min: b.Min.Add(g.Origin),
		Max: b.Max.Add(g.Origin),
	}
}

func (g *GridMap) localCellTransform(c GridCoord, orient uint8) math3d.Mat4 {
	m := GridOrientationMat4(orient)
	m[12] = (float32(c.X) + 0.5) * g.CellSize.X
	m[13] = (float32(c.Y) + 0.5) * g.CellSize.Y
	m[14] = (float32(c.Z) + 0.5) * g.CellSize.Z
	return m
}

// CellTransform returns the world-space transform matrix placing a tile at the
// center of cell c rotated by orient (0..23).
func (g *GridMap) CellTransform(c GridCoord, orient uint8) math3d.Mat4 {
	m := g.localCellTransform(c, orient)
	m[12] += g.Origin.X
	m[13] += g.Origin.Y
	m[14] += g.Origin.Z
	return m
}

func (g *GridMap) localTileBounds(c GridCoord, cell GridCell, def MeshTileDef, hasDef bool) math3d.AABB {
	if hasDef {
		xform := g.localCellTransform(c, cell.Orientation)
		b := math3d.AABBEmpty()
		if !def.LocalBounds.Empty() {
			b = def.LocalBounds.Transform(xform)
		}
		for _, cb := range def.ColliderBoxes {
			if !cb.Empty() {
				b = b.Union(cb.Transform(xform))
			}
		}
		if !b.Empty() {
			return b
		}
	}
	return g.localCellBounds(c)
}

func (g *GridMap) chunkWorldBounds(ch *gridChunk) math3d.AABB {
	if rev := g.Library.Revision(); rev != g.libRevision {
		g.libRevision = rev
		for _, other := range g.chunks {
			other.dirtyBounds = true
		}
	}
	if ch.dirtyBounds {
		b := math3d.AABBEmpty()
		for c, cell := range ch.cells {
			def, hasDef := g.Library.Get(cell.TileID)
			b = b.Union(g.localTileBounds(c, cell, def, hasDef))
		}
		ch.localBounds = b
		ch.dirtyBounds = false
	}
	if ch.localBounds.Empty() {
		return ch.localBounds
	}
	return math3d.AABB{
		Min: ch.localBounds.Min.Add(g.Origin),
		Max: ch.localBounds.Max.Add(g.Origin),
	}
}

// Bounds returns the tight world-space AABB enclosing all occupied cells.
func (g *GridMap) Bounds() math3d.AABB {
	if g == nil || g.count == 0 {
		return math3d.AABBEmpty()
	}
	out := math3d.AABBEmpty()
	for _, ch := range g.chunks {
		out = out.Union(g.chunkWorldBounds(ch))
	}
	return out
}

// CollisionBoxesIn returns all world-space collider AABBs from occupied cells
// that intersect region. Tiles with no ColliderBoxes emit no collision boxes.
func (g *GridMap) CollisionBoxesIn(region math3d.AABB) []math3d.AABB {
	if g == nil || g.count == 0 || g.Library == nil || region.Empty() {
		return nil
	}
	var out []math3d.AABB
	for _, ch := range g.chunks {
		if !g.chunkWorldBounds(ch).Intersects(region) {
			continue
		}
		for c, cell := range ch.cells {
			def, ok := g.Library.Get(cell.TileID)
			if !ok || len(def.ColliderBoxes) == 0 {
				continue
			}
			xform := g.CellTransform(c, cell.Orientation)
			for _, localBox := range def.ColliderBoxes {
				if localBox.Empty() {
					continue
				}
				worldBox := localBox.Transform(xform)
				if worldBox.Intersects(region) {
					out = append(out, worldBox)
				}
			}
		}
	}
	return out
}

type chunkRayCandidate struct {
	chunk *gridChunk
	tMin  float32
}

// Raycast intersects ray with occupied cells up to maxDist (in world-space
// Euclidean units; ray.Dir need not be unit-length). For each cell, Raycast
// tests its transformed ColliderBoxes when present, falling back to its
// transformed LocalBounds or CellBounds so non-colliding tiles remain pickable.
func (g *GridMap) Raycast(ray math3d.Ray, maxDist float32) (GridHit, bool) {
	if g == nil || g.count == 0 || maxDist < 0 {
		return GridHit{}, false
	}
	dirLen := ray.Dir.Len()
	if dirLen < 1e-7 {
		return GridHit{}, false
	}
	unitRay := math3d.Ray{
		Origin: ray.Origin,
		Dir:    ray.Dir.Scale(1 / dirLen),
	}

	var candidates [32]chunkRayCandidate
	hits := candidates[:0]
	for _, ch := range g.chunks {
		wb := g.chunkWorldBounds(ch)
		tMin, _, ok := unitRay.IntersectAABB(wb)
		if !ok || tMin > maxDist {
			continue
		}
		hits = append(hits, chunkRayCandidate{chunk: ch, tMin: tMin})
	}
	if len(hits) == 0 {
		return GridHit{}, false
	}
	if len(hits) > 1 {
		slices.SortFunc(hits, func(a, b chunkRayCandidate) int {
			if a.tMin < b.tMin {
				return -1
			}
			if a.tMin > b.tMin {
				return 1
			}
			return 0
		})
	}

	var best GridHit
	bestT := maxDist + 1
	found := false

	testBox := func(c GridCoord, cell GridCell, b math3d.AABB) {
		t, normal, ok := rayIntersectAABBWithNormal(unitRay, b)
		if ok && t <= maxDist && t < bestT {
			bestT = t
			found = true
			best = GridHit{
				Coord:    c,
				Cell:     cell,
				Point:    unitRay.At(t),
				Normal:   normal,
				Distance: t,
			}
		}
	}

	for _, cand := range hits {
		if found && cand.tMin > bestT {
			break
		}
		for c, cell := range cand.chunk.cells {
			def, hasDef := g.Library.Get(cell.TileID)
			if hasDef && len(def.ColliderBoxes) > 0 {
				xform := g.CellTransform(c, cell.Orientation)
				for _, lb := range def.ColliderBoxes {
					if !lb.Empty() {
						testBox(c, cell, lb.Transform(xform))
					}
				}
				continue
			}
			if hasDef && !def.LocalBounds.Empty() {
				xform := g.CellTransform(c, cell.Orientation)
				testBox(c, cell, def.LocalBounds.Transform(xform))
				continue
			}
			testBox(c, cell, g.CellBounds(c))
		}
	}

	return best, found
}

func rayIntersectAABBWithNormal(unitRay math3d.Ray, b math3d.AABB) (float32, math3d.Vec3, bool) {
	if b.Empty() {
		return 0, math3d.Vec3{}, false
	}
	t0 := float32(math.Inf(-1))
	t1 := float32(math.Inf(1))
	var hitAxis int
	var hitSign float32

	checkAxis := func(origin, dir, bMin, bMax float32, axis int) bool {
		if dir == 0 {
			return origin >= bMin && origin <= bMax
		}
		invD := 1 / dir
		near := (bMin - origin) * invD
		far := (bMax - origin) * invD
		sign := float32(-1)
		if near > far {
			near, far = far, near
			sign = 1
		}
		if near > t0 {
			t0 = near
			hitAxis = axis
			hitSign = sign
		}
		if far < t1 {
			t1 = far
		}
		return t0 <= t1
	}

	if !checkAxis(unitRay.Origin.X, unitRay.Dir.X, b.Min.X, b.Max.X, 0) {
		return 0, math3d.Vec3{}, false
	}
	if !checkAxis(unitRay.Origin.Y, unitRay.Dir.Y, b.Min.Y, b.Max.Y, 1) {
		return 0, math3d.Vec3{}, false
	}
	if !checkAxis(unitRay.Origin.Z, unitRay.Dir.Z, b.Min.Z, b.Max.Z, 2) {
		return 0, math3d.Vec3{}, false
	}
	if t1 < 0 {
		return 0, math3d.Vec3{}, false
	}
	if t0 < 0 {
		return 0, unitRay.Dir.Neg(), true
	}
	var normal math3d.Vec3
	switch hitAxis {
	case 0:
		normal.X = hitSign
	case 1:
		normal.Y = hitSign
	case 2:
		normal.Z = hitSign
	}
	return t0, normal, true
}

// BuildInstances gathers world transforms for all visible occupied cells,
// grouped into instanced draw batches by (MeshID, MaterialID).
//
// If frustum is non-nil, chunks and cells outside the view frustum are culled.
// To avoid steady-state heap allocations, callers can pass back the slice
// returned from the previous frame; its batch slots and Transforms backing
// arrays are reused in place.
func (g *GridMap) BuildInstances(frustum *math3d.Frustum, out []GridInstanceBatch) []GridInstanceBatch {
	out = out[:0]
	if g == nil || g.count == 0 || g.Library == nil {
		return out
	}
	if g.batchIndex == nil {
		g.batchIndex = make(map[gridBatchKey]int)
	} else {
		clear(g.batchIndex)
	}

	for _, ch := range g.chunks {
		if frustum != nil && !frustum.IntersectsAABB(g.chunkWorldBounds(ch)) {
			continue
		}
		for c, cell := range ch.cells {
			def, ok := g.Library.Get(cell.TileID)
			if !ok {
				continue
			}
			xform := g.CellTransform(c, cell.Orientation)
			if frustum != nil {
				var wb math3d.AABB
				if !def.LocalBounds.Empty() {
					wb = def.LocalBounds.Transform(xform)
				} else {
					wb = g.CellBounds(c)
				}
				if !frustum.IntersectsAABB(wb) {
					continue
				}
			}

			key := gridBatchKey{meshID: def.MeshID, materialID: def.MaterialID}
			idx, exists := g.batchIndex[key]
			if !exists {
				idx = len(out)
				if idx < cap(out) {
					out = out[:idx+1]
					out[idx].MeshID = def.MeshID
					out[idx].MaterialID = def.MaterialID
					out[idx].Transforms = out[idx].Transforms[:0]
				} else {
					out = append(out, GridInstanceBatch{
						MeshID:     def.MeshID,
						MaterialID: def.MaterialID,
					})
				}
				g.batchIndex[key] = idx
			}
			out[idx].Transforms = append(out[idx].Transforms, xform)
		}
	}

	if len(out) > 1 {
		slices.SortFunc(out, func(a, b GridInstanceBatch) int {
			if a.MeshID != b.MeshID {
				if a.MeshID < b.MeshID {
					return -1
				}
				return 1
			}
			if a.MaterialID < b.MaterialID {
				return -1
			}
			if a.MaterialID > b.MaterialID {
				return 1
			}
			return 0
		})
	}
	return out
}
