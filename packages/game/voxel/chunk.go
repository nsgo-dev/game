package voxel

import (
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

const (
	// DefaultChunkSize is the default cubic chunk edge length (16x16x16 = 4096 voxels).
	DefaultChunkSize = 16
	// MinChunkSize is the smallest supported power-of-two chunk edge length.
	MinChunkSize = 4
	// MaxChunkSize is the largest supported power-of-two chunk edge length.
	MaxChunkSize = 64
	// DefaultVoxelSize is the default world-space size of one voxel edge.
	DefaultVoxelSize float32 = 1
)

// ValidChunkSize reports whether n is a supported power-of-two chunk size.
func ValidChunkSize(n int) bool {
	return n >= MinChunkSize && n <= MaxChunkSize && n&(n-1) == 0
}

// WorldConfig configures a new [World].
type WorldConfig struct {
	// ChunkSize is the power-of-two cubic edge length of each chunk (4..64).
	// Zero uses DefaultChunkSize (16).
	ChunkSize int
	// VoxelSize is the world-space edge length of each voxel.
	// Zero uses DefaultVoxelSize (1.0).
	VoxelSize float32
	// Registry defines block properties. Nil creates a new empty Registry
	// where Air (0) is empty and any non-zero BlockID is opaque and solid.
	Registry *Registry
}

// Chunk stores a dense cubic N x N x N grid of [BlockID] values in (y, z, x)
// order (index = (y*N + z)*N + x), alongside per-voxel sky and block light
// levels packed into one byte per voxel (high nibble: sky 0..15, low nibble:
// block 0..15).
type Chunk struct {
	coord    ChunkCoord
	size     int
	blocks   []BlockID
	light    []uint8
	nonAir   int
	dirty    bool
	revision uint64
}

func newChunk(coord ChunkCoord, size int) *Chunk {
	return &Chunk{
		coord:  coord,
		size:   size,
		blocks: make([]BlockID, size*size*size),
		light:  newLightSlice(size),
	}
}

// Coord returns the chunk's integer coordinate in chunk space.
func (c *Chunk) Coord() ChunkCoord { return c.coord }

// Size returns the chunk's edge length N.
func (c *Chunk) Size() int { return c.size }

// Empty reports whether every voxel in c is [Air].
func (c *Chunk) Empty() bool { return c == nil || c.nonAir == 0 }

// NonAirCount returns the number of non-air voxels in c.
func (c *Chunk) NonAirCount() int {
	if c == nil {
		return 0
	}
	return c.nonAir
}

// Dirty reports whether c has unmeshed edits.
func (c *Chunk) Dirty() bool {
	if c == nil {
		return false
	}
	return c.dirty
}

// Revision returns a monotonically increasing edit counter for c.
func (c *Chunk) Revision() uint64 {
	if c == nil {
		return 0
	}
	return c.revision
}

func (c *Chunk) index(lx, ly, lz int) int {
	return (ly*c.size+lz)*c.size + lx
}

func (c *Chunk) getUnchecked(lx, ly, lz int) BlockID {
	return c.blocks[(ly*c.size+lz)*c.size+lx]
}

// Get returns the block at chunk-local coordinates (lx, ly, lz), or [Air] if
// out of bounds.
func (c *Chunk) Get(lx, ly, lz int) BlockID {
	if c == nil || lx < 0 || ly < 0 || lz < 0 || lx >= c.size || ly >= c.size || lz >= c.size {
		return Air
	}
	return c.blocks[c.index(lx, ly, lz)]
}

// SkyLight returns the stored sky light level (0..15) at chunk-local
// coordinates (lx, ly, lz), or 0 if out of bounds. Use [World.SkyLight] for
// cross-chunk world-coordinate queries.
func (c *Chunk) SkyLight(lx, ly, lz int) uint8 {
	if c == nil || lx < 0 || ly < 0 || lz < 0 || lx >= c.size || ly >= c.size || lz >= c.size {
		return 0
	}
	return unpackSky(c.light[c.index(lx, ly, lz)])
}

// BlockLight returns the stored block light level (0..15) at chunk-local
// coordinates (lx, ly, lz), or 0 if out of bounds. Use [World.BlockLight] for
// cross-chunk world-coordinate queries.
func (c *Chunk) BlockLight(lx, ly, lz int) uint8 {
	if c == nil || lx < 0 || ly < 0 || lz < 0 || lx >= c.size || ly >= c.size || lz >= c.size {
		return 0
	}
	return unpackBlock(c.light[c.index(lx, ly, lz)])
}

// Light returns both the stored sky light and block light levels (0..15 each)
// at chunk-local coordinates (lx, ly, lz), or (0, 0) if out of bounds.
func (c *Chunk) Light(lx, ly, lz int) (sky, block uint8) {
	if c == nil || lx < 0 || ly < 0 || lz < 0 || lx >= c.size || ly >= c.size || lz >= c.size {
		return 0, 0
	}
	b := c.light[c.index(lx, ly, lz)]
	return unpackSky(b), unpackBlock(b)
}

// Blocks returns the chunk's underlying block slice in (y, z, x) order
// (index = (y*N + z)*N + x). Callers should treat it as read-only; edit blocks
// via [World.SetBlock] so dirty tracking stays accurate.
func (c *Chunk) Blocks() []BlockID {
	if c == nil {
		return nil
	}
	return c.blocks
}

// Bounds returns the world-space axis-aligned bounding box of c.
func (c *Chunk) Bounds(voxelSize float32) math3d.AABB {
	if c == nil {
		return math3d.AABBEmpty()
	}
	span := float32(c.size) * voxelSize
	minPt := math3d.Vec3{
		X: float32(c.coord.X) * span,
		Y: float32(c.coord.Y) * span,
		Z: float32(c.coord.Z) * span,
	}
	return math3d.AABB{
		Min: minPt,
		Max: minPt.Add(math3d.Vec3{X: span, Y: span, Z: span}),
	}
}

// World holds a sparse collection of cubic voxel chunks indexed by [ChunkCoord],
// tracks which chunks need remeshing after edits, maintains incremental per-voxel
// sky and block light propagation, and provides raycasting and YAML persistence.
type World struct {
	chunks       map[ChunkCoord]*Chunk
	airLight     map[ChunkCoord]*airLightChunk
	dirty        map[ChunkCoord]struct{}
	chunkSize    int
	shift        uint
	mask         int
	voxelSize    float32
	registry     *Registry
	hasMinChunkY bool
	minChunkY    int

	lightAddQ []BlockCoord
	lightRemQ []lightRemNode
	lightSeed []BlockCoord
	origLight map[BlockCoord]uint8
}

// NewWorld constructs an empty World with cfg.
// Invalid ChunkSize or VoxelSize values return an error.
func NewWorld(cfg WorldConfig) (*World, error) {
	size := cfg.ChunkSize
	if size == 0 {
		size = DefaultChunkSize
	}
	if !ValidChunkSize(size) {
		return nil, fmt.Errorf("voxel: chunk size %d must be a power of two between %d and %d", size, MinChunkSize, MaxChunkSize)
	}
	vs := cfg.VoxelSize
	if vs == 0 {
		vs = DefaultVoxelSize
	}
	if math.IsNaN(float64(vs)) || math.IsInf(float64(vs), 0) || vs <= 0 {
		return nil, fmt.Errorf("voxel: voxel size %v must be finite and positive", vs)
	}
	reg := cfg.Registry
	if reg == nil {
		reg = NewRegistry()
	}
	return &World{
		chunks:    make(map[ChunkCoord]*Chunk),
		airLight:  make(map[ChunkCoord]*airLightChunk),
		dirty:     make(map[ChunkCoord]struct{}),
		chunkSize: size,
		shift:     uint(bits.TrailingZeros(uint(size))),
		mask:      size - 1,
		voxelSize: vs,
		registry:  reg,
	}, nil
}

// ChunkSize returns the edge length N of each cubic chunk.
func (w *World) ChunkSize() int { return w.chunkSize }

// VoxelSize returns the world-space edge length of one voxel.
func (w *World) VoxelSize() float32 { return w.voxelSize }

// Registry returns the world's block [Registry].
func (w *World) Registry() *Registry { return w.registry }

// BlockToChunk splits a world voxel coordinate into its chunk coordinate and
// chunk-local coordinates (lx, ly, lz) in [0, ChunkSize).
func (w *World) BlockToChunk(pos BlockCoord) (cc ChunkCoord, lx, ly, lz int) {
	cc = ChunkCoord{
		X: pos.X >> w.shift,
		Y: pos.Y >> w.shift,
		Z: pos.Z >> w.shift,
	}
	lx = pos.X & w.mask
	ly = pos.Y & w.mask
	lz = pos.Z & w.mask
	return cc, lx, ly, lz
}

// ChunkOrigin returns the world voxel coordinate of local (0, 0, 0) in cc.
func (w *World) ChunkOrigin(cc ChunkCoord) BlockCoord {
	return BlockCoord{
		X: cc.X << w.shift,
		Y: cc.Y << w.shift,
		Z: cc.Z << w.shift,
	}
}

// ChunkWorldOrigin returns the world-space position of local (0, 0, 0) in cc.
func (w *World) ChunkWorldOrigin(cc ChunkCoord) math3d.Vec3 {
	span := float32(w.chunkSize) * w.voxelSize
	return math3d.Vec3{
		X: float32(cc.X) * span,
		Y: float32(cc.Y) * span,
		Z: float32(cc.Z) * span,
	}
}

// WorldToBlock converts a world-space position to the enclosing integer voxel coordinate.
func (w *World) WorldToBlock(p math3d.Vec3) BlockCoord {
	inv := 1.0 / float64(w.voxelSize)
	return BlockCoord{
		X: int(math.Floor(float64(p.X) * inv)),
		Y: int(math.Floor(float64(p.Y) * inv)),
		Z: int(math.Floor(float64(p.Z) * inv)),
	}
}

// BlockBounds returns the world-space axis-aligned bounding box of voxel pos.
func (w *World) BlockBounds(pos BlockCoord) math3d.AABB {
	vs := w.voxelSize
	minPt := math3d.Vec3{
		X: float32(pos.X) * vs,
		Y: float32(pos.Y) * vs,
		Z: float32(pos.Z) * vs,
	}
	return math3d.AABB{
		Min: minPt,
		Max: minPt.Add(math3d.Vec3{X: vs, Y: vs, Z: vs}),
	}
}

// Block returns the [BlockID] at world voxel coordinate pos, or [Air] if the
// chunk is not allocated.
func (w *World) Block(pos BlockCoord) BlockID {
	if w == nil {
		return Air
	}
	cc, lx, ly, lz := w.BlockToChunk(pos)
	ch := w.chunks[cc]
	if ch == nil {
		return Air
	}
	return ch.getUnchecked(lx, ly, lz)
}

// SetBlock sets the voxel at pos to id and returns whether the block changed.
//
// When the block changes, SetBlock updates sky and block light incrementally,
// marks the edited chunk dirty, and marks adjacent existing chunks dirty only
// when face visibility, cross-chunk ambient occlusion, or voxel lighting
// changes across their boundaries.
func (w *World) SetBlock(pos BlockCoord, id BlockID) bool {
	if w == nil {
		return false
	}
	cc, lx, ly, lz := w.BlockToChunk(pos)
	ch := w.chunks[cc]
	if ch == nil {
		if id == Air {
			return false
		}
		ch = newChunk(cc, w.chunkSize)
		if al, ok := w.airLight[cc]; ok {
			ch.light = al.light
			delete(w.airLight, cc)
		}
		w.chunks[cc] = ch
	}
	idx := ch.index(lx, ly, lz)
	oldID := ch.blocks[idx]
	if oldID == id {
		return false
	}
	ch.blocks[idx] = id
	skyMinChanged := false
	if oldID == Air {
		ch.nonAir++
		if ch.nonAir == 1 {
			if !w.hasMinChunkY {
				w.hasMinChunkY = true
				w.minChunkY = cc.Y
			} else if cc.Y < w.minChunkY {
				w.minChunkY = cc.Y
				skyMinChanged = true
			}
		}
	} else if id == Air {
		ch.nonAir--
		if ch.nonAir == 0 && w.hasMinChunkY && cc.Y == w.minChunkY {
			oldMin := w.minChunkY
			w.recomputeMinChunkY()
			if !w.hasMinChunkY || w.minChunkY != oldMin {
				skyMinChanged = true
			}
		}
	}
	ch.revision++
	w.markDirtyChunk(ch)
	w.markAffectedNeighbors(cc, lx, ly, lz, oldID, id)
	w.updateLightOnEdit(pos, oldID, id, skyMinChanged)
	return true
}

// FillBox fills every voxel in the inclusive box [minPos, maxPos] with id and
// returns the number of voxels that changed. Min/max components are ordered
// automatically per axis.
func (w *World) FillBox(a, b BlockCoord, id BlockID) int {
	if w == nil {
		return 0
	}
	minX, maxX := min(a.X, b.X), max(a.X, b.X)
	minY, maxY := min(a.Y, b.Y), max(a.Y, b.Y)
	minZ, maxZ := min(a.Z, b.Z), max(a.Z, b.Z)

	changed := 0
	for y := minY; y <= maxY; y++ {
		for z := minZ; z <= maxZ; z++ {
			for x := minX; x <= maxX; x++ {
				if w.SetBlock(BlockCoord{X: x, Y: y, Z: z}, id) {
					changed++
				}
			}
		}
	}
	return changed
}

func (w *World) faceVisible(block, neighbor BlockID) bool {
	if block == Air {
		return false
	}
	if neighbor == Air {
		return true
	}
	if w.registry.IsOpaque(block) {
		return !w.registry.IsOpaque(neighbor)
	}
	return !w.registry.IsOpaque(neighbor) && neighbor != block
}

func (w *World) markDirtyChunk(ch *Chunk) {
	ch.dirty = true
	w.dirty[ch.coord] = struct{}{}
}

func (w *World) markAffectedNeighbors(cc ChunkCoord, lx, ly, lz int, oldID, newID BlockID) {
	oldOpaque := w.registry.IsOpaque(oldID)
	newOpaque := w.registry.IsOpaque(newID)
	opacityChanged := oldOpaque != newOpaque
	oldTrans := oldID != Air && !oldOpaque
	newTrans := newID != Air && !newOpaque

	// Changing one opaque block into another opaque block cannot alter any
	// neighbor chunk's face visibility or ambient occlusion.
	if !opacityChanged && !oldTrans && !newTrans {
		return
	}

	n := w.chunkSize
	var dx, dy, dz int
	if lx == 0 {
		dx = -1
	} else if lx == n-1 {
		dx = 1
	}
	if ly == 0 {
		dy = -1
	} else if ly == n-1 {
		dy = 1
	}
	if lz == 0 {
		dz = -1
	} else if lz == n-1 {
		dz = 1
	}
	if dx == 0 && dy == 0 && dz == 0 {
		return
	}

	// Face-adjacent neighbor chunks.
	if dx != 0 {
		nlx := 0
		if dx < 0 {
			nlx = n - 1
		}
		w.maybeDirtyFaceNeighbor(cc.Add(ChunkCoord{X: dx}), nlx, ly, lz, 0, oldID, newID, opacityChanged)
	}
	if dy != 0 {
		nly := 0
		if dy < 0 {
			nly = n - 1
		}
		w.maybeDirtyFaceNeighbor(cc.Add(ChunkCoord{Y: dy}), lx, nly, lz, 1, oldID, newID, opacityChanged)
	}
	if dz != 0 {
		nlz := 0
		if dz < 0 {
			nlz = n - 1
		}
		w.maybeDirtyFaceNeighbor(cc.Add(ChunkCoord{Z: dz}), lx, ly, nlz, 2, oldID, newID, opacityChanged)
	}

	// Edge and corner neighbor chunks only matter when opacity changes (for
	// cross-chunk ambient occlusion).
	if !opacityChanged {
		return
	}
	if dx != 0 && dy != 0 {
		nlx, nly := edgeLocal(dx, n), edgeLocal(dy, n)
		w.maybeDirtyEdgeNeighbor(cc.Add(ChunkCoord{X: dx, Y: dy}), nlx, nly, lz, 2)
	}
	if dx != 0 && dz != 0 {
		nlx, nlz := edgeLocal(dx, n), edgeLocal(dz, n)
		w.maybeDirtyEdgeNeighbor(cc.Add(ChunkCoord{X: dx, Z: dz}), nlx, ly, nlz, 1)
	}
	if dy != 0 && dz != 0 {
		nly, nlz := edgeLocal(dy, n), edgeLocal(dz, n)
		w.maybeDirtyEdgeNeighbor(cc.Add(ChunkCoord{Y: dy, Z: dz}), lx, nly, nlz, 0)
	}
	if dx != 0 && dy != 0 && dz != 0 {
		ncc := cc.Add(ChunkCoord{X: dx, Y: dy, Z: dz})
		if nch := w.chunks[ncc]; nch != nil && nch.nonAir > 0 {
			if nch.getUnchecked(edgeLocal(dx, n), edgeLocal(dy, n), edgeLocal(dz, n)) != Air {
				w.markDirtyChunk(nch)
			}
		}
	}
}

func edgeLocal(dir, n int) int {
	if dir < 0 {
		return n - 1
	}
	return 0
}

// maybeDirtyFaceNeighbor checks the directly adjacent block in ncc for face
// visibility changes, and (when opacityChanged) the surrounding 3x3 patch on
// the boundary plane for cross-chunk AO changes.
func (w *World) maybeDirtyFaceNeighbor(ncc ChunkCoord, nlx, nly, nlz, axis int, oldID, newID BlockID, opacityChanged bool) {
	nch := w.chunks[ncc]
	if nch == nil || nch.nonAir == 0 {
		return
	}
	nb := nch.getUnchecked(nlx, nly, nlz)
	if w.faceVisible(nb, oldID) != w.faceVisible(nb, newID) {
		w.markDirtyChunk(nch)
		return
	}
	if !opacityChanged {
		return
	}
	n := w.chunkSize
	for o1 := -1; o1 <= 1; o1++ {
		for o2 := -1; o2 <= 1; o2++ {
			x, y, z := nlx, nly, nlz
			switch axis {
			case 0:
				y += o1
				z += o2
			case 1:
				x += o1
				z += o2
			case 2:
				x += o1
				y += o2
			}
			if x >= 0 && x < n && y >= 0 && y < n && z >= 0 && z < n {
				if nch.getUnchecked(x, y, z) != Air {
					w.markDirtyChunk(nch)
					return
				}
			}
		}
	}
}

func (w *World) maybeDirtyEdgeNeighbor(ncc ChunkCoord, nlx, nly, nlz, freeAxis int) {
	nch := w.chunks[ncc]
	if nch == nil || nch.nonAir == 0 {
		return
	}
	n := w.chunkSize
	for o := -1; o <= 1; o++ {
		x, y, z := nlx, nly, nlz
		switch freeAxis {
		case 0:
			x += o
		case 1:
			y += o
		case 2:
			z += o
		}
		if x >= 0 && x < n && y >= 0 && y < n && z >= 0 && z < n {
			if nch.getUnchecked(x, y, z) != Air {
				w.markDirtyChunk(nch)
				return
			}
		}
	}
}

// Chunk returns the allocated chunk at cc, or nil if none exists.
func (w *World) Chunk(cc ChunkCoord) *Chunk {
	if w == nil {
		return nil
	}
	return w.chunks[cc]
}

// ChunkCount returns the number of currently allocated chunks (including empty
// chunks that have not yet been pruned).
func (w *World) ChunkCount() int {
	if w == nil {
		return 0
	}
	return len(w.chunks)
}

// ChunkCoords returns all allocated chunk coordinates sorted by [CompareChunkCoords].
func (w *World) ChunkCoords() []ChunkCoord {
	if w == nil || len(w.chunks) == 0 {
		return nil
	}
	out := make([]ChunkCoord, 0, len(w.chunks))
	for cc := range w.chunks {
		out = append(out, cc)
	}
	slices.SortFunc(out, CompareChunkCoords)
	return out
}

// DirtyCount returns the number of chunks currently marked dirty.
func (w *World) DirtyCount() int {
	if w == nil {
		return 0
	}
	return len(w.dirty)
}

// IsDirty reports whether cc is currently marked dirty.
func (w *World) IsDirty(cc ChunkCoord) bool {
	if w == nil {
		return false
	}
	_, ok := w.dirty[cc]
	return ok
}

// DirtyChunks returns all dirty chunk coordinates sorted by [CompareChunkCoords].
func (w *World) DirtyChunks() []ChunkCoord {
	if w == nil || len(w.dirty) == 0 {
		return nil
	}
	out := make([]ChunkCoord, 0, len(w.dirty))
	for cc := range w.dirty {
		out = append(out, cc)
	}
	slices.SortFunc(out, CompareChunkCoords)
	return out
}

// MarkDirty marks cc dirty if it exists in w.
func (w *World) MarkDirty(cc ChunkCoord) {
	if w == nil {
		return
	}
	if ch := w.chunks[cc]; ch != nil {
		w.markDirtyChunk(ch)
	}
}

// ClearDirty clears the dirty flag on cc.
func (w *World) ClearDirty(cc ChunkCoord) {
	if w == nil {
		return
	}
	delete(w.dirty, cc)
	if ch := w.chunks[cc]; ch != nil {
		ch.dirty = false
	}
}

// PruneEmptyChunks removes all allocated chunks that contain only [Air] and
// are not dirty, returning the number of chunks freed. Any non-default light
// passing through a pruned air chunk is preserved seamlessly.
func (w *World) PruneEmptyChunks() int {
	if w == nil {
		return 0
	}
	pruned := 0
	for cc, ch := range w.chunks {
		if ch.nonAir == 0 && !ch.dirty {
			if nonDef := countNonDefaultLight(ch.light); nonDef > 0 {
				w.airLight[cc] = &airLightChunk{
					light:      ch.light,
					nonDefault: nonDef,
				}
			}
			delete(w.chunks, cc)
			pruned++
		}
	}
	return pruned
}

// Bounds returns the world-space axis-aligned bounding box enclosing all
// non-empty chunks, or [math3d.AABBEmpty] when the world has no non-air blocks.
func (w *World) Bounds() math3d.AABB {
	if w == nil {
		return math3d.AABBEmpty()
	}
	b := math3d.AABBEmpty()
	for _, ch := range w.chunks {
		if ch.nonAir > 0 {
			b = b.Union(ch.Bounds(w.voxelSize))
		}
	}
	return b
}

// Clear removes all chunks, light data, and dirty flags from w while keeping its
// ChunkSize, VoxelSize, and Registry.
func (w *World) Clear() {
	if w == nil {
		return
	}
	clear(w.chunks)
	clear(w.airLight)
	clear(w.dirty)
	clear(w.origLight)
	w.hasMinChunkY = false
	w.minChunkY = 0
}
