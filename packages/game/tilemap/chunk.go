package tilemap

import (
	"cmp"
	"fmt"
	"math"
	"math/bits"
	"slices"
)

const (
	// DefaultChunkSize is the default power-of-two edge length (in tiles) for sparse chunked tile layers.
	DefaultChunkSize = 16
	// MinChunkSize is the smallest supported power-of-two edge length (in tiles) for a chunked tile layer.
	MinChunkSize = 2
	// MaxChunkSize is the largest supported power-of-two edge length (in tiles) for a chunked tile layer.
	MaxChunkSize = 256
)

// ValidChunkSize reports whether n is a supported power-of-two chunk edge length in [MinChunkSize, MaxChunkSize].
func ValidChunkSize(n int) bool {
	return n >= MinChunkSize && n <= MaxChunkSize && n&(n-1) == 0
}

func normalizeChunkSize(n int) int {
	if n <= 0 {
		return DefaultChunkSize
	}
	if n < MinChunkSize {
		return MinChunkSize
	}
	if n > MaxChunkSize {
		return MaxChunkSize
	}
	if n&(n-1) == 0 {
		return n
	}
	// Round up to the next power of two within [MinChunkSize, MaxChunkSize].
	p := 1 << bits.Len(uint(n-1))
	if p > MaxChunkSize {
		return MaxChunkSize
	}
	return p
}

// ChunkCoord identifies one sparse chunk in chunk-grid coordinates. Both X and Y may be negative.
type ChunkCoord struct {
	X, Y int
}

func compareChunkCoord(a, b ChunkCoord) int {
	if c := cmp.Compare(a.Y, b.Y); c != 0 {
		return c
	}
	return cmp.Compare(a.X, b.X)
}

// TileToChunk converts a world tile coordinate (col, row) into its chunk coordinate and chunk-local (lx, ly) offset
// for a power-of-two chunkSize (invalid chunkSize falls back to DefaultChunkSize).
//
// Negative tile coordinates floor toward negative infinity, so local coordinates are always in [0, chunkSize).
func TileToChunk(col, row, chunkSize int) (coord ChunkCoord, lx, ly int) {
	size := normalizeChunkSize(chunkSize)
	shift := uint(bits.TrailingZeros(uint(size)))
	mask := size - 1
	return ChunkCoord{X: col >> shift, Y: row >> shift}, col & mask, row & mask
}

// Chunk holds one fixed power-of-two square (Size × Size, row-major) of raw tile GIDs inside a chunked TileLayer.
type Chunk struct {
	Coord ChunkCoord
	Size  int
	Tiles []uint32
	// Dirty reports whether this chunk's tiles changed since its static cache was last baked.
	Dirty bool
	// Modified reports whether this chunk's tiles were edited after it was generated or loaded.
	Modified bool

	nonEmpty int
	cache    *LayerCache
}

// NewChunk allocates an empty Size × Size chunk at coord. Non-positive or non-power-of-two sizes are normalized into [MinChunkSize, MaxChunkSize] (defaulting to DefaultChunkSize).
func NewChunk(coord ChunkCoord, size int) *Chunk {
	sz := normalizeChunkSize(size)
	return &Chunk{
		Coord: coord,
		Size:  sz,
		Tiles: make([]uint32, sz*sz),
		Dirty: true,
	}
}

// TileAt returns the raw GID (including flip flags) at chunk-local (lx, ly), or 0 if out of range.
func (c *Chunk) TileAt(lx, ly int) uint32 {
	if c == nil || lx < 0 || ly < 0 || lx >= c.Size || ly >= c.Size {
		return 0
	}
	idx := ly*c.Size + lx
	if idx >= len(c.Tiles) {
		return 0
	}
	return c.Tiles[idx]
}

// SetTile writes gid at chunk-local (lx, ly) and reports whether the cell changed.
// Changing a cell marks the chunk both Dirty and Modified and invalidates its static cache.
func (c *Chunk) SetTile(lx, ly int, gid uint32) bool {
	if c == nil || lx < 0 || ly < 0 || lx >= c.Size || ly >= c.Size {
		return false
	}
	if len(c.Tiles) < c.Size*c.Size {
		grown := make([]uint32, c.Size*c.Size)
		copy(grown, c.Tiles)
		c.Tiles = grown
	}
	idx := ly*c.Size + lx
	prev := c.Tiles[idx]
	if prev == gid {
		return false
	}
	c.Tiles[idx] = gid
	if prev == 0 && gid != 0 {
		c.nonEmpty++
	} else if prev != 0 && gid == 0 && c.nonEmpty > 0 {
		c.nonEmpty--
	}
	c.Dirty = true
	c.Modified = true
	if c.cache != nil {
		c.cache.Dirty = true
	}
	return true
}

// Empty reports whether every cell in c has GID 0.
func (c *Chunk) Empty() bool {
	if c == nil {
		return true
	}
	for _, gid := range c.Tiles {
		if gid != 0 {
			return false
		}
	}
	return true
}

// Cache returns this chunk's static LayerCache, or nil if the chunk has not been baked.
func (c *Chunk) Cache() *LayerCache {
	if c == nil {
		return nil
	}
	return c.cache
}

// Clone returns a deep copy of c's tile data and metadata without copying its static GPU/RGBA cache.
func (c *Chunk) Clone() *Chunk {
	if c == nil {
		return nil
	}
	cp := &Chunk{
		Coord:    c.Coord,
		Size:     c.Size,
		Tiles:    slices.Clone(c.Tiles),
		Dirty:    c.Dirty,
		Modified: c.Modified,
		nonEmpty: c.nonEmpty,
	}
	return cp
}

func (c *Chunk) recountNonEmpty() {
	n := 0
	for _, gid := range c.Tiles {
		if gid != 0 {
			n++
		}
	}
	c.nonEmpty = n
}

type chunkGrid struct {
	size   int
	shift  uint
	mask   int
	chunks map[ChunkCoord]*Chunk
}

func newChunkGrid(chunkSize int) *chunkGrid {
	sz := normalizeChunkSize(chunkSize)
	return &chunkGrid{
		size:   sz,
		shift:  uint(bits.TrailingZeros(uint(sz))),
		mask:   sz - 1,
		chunks: make(map[ChunkCoord]*Chunk),
	}
}

func (g *chunkGrid) tileToChunk(col, row int) (ChunkCoord, int, int) {
	return ChunkCoord{X: col >> g.shift, Y: row >> g.shift}, col & g.mask, row & g.mask
}

// NewChunkedTileLayer returns a visible sparse chunked TileLayer at full opacity using a power-of-two chunkSize
// (defaulting to DefaultChunkSize when chunkSize <= 0).
func NewChunkedTileLayer(name string, chunkSize int) TileLayer {
	sz := normalizeChunkSize(chunkSize)
	return TileLayer{
		Name:      name,
		ChunkSize: sz,
		Opacity:   1,
		Visible:   true,
		chunks:    newChunkGrid(sz),
	}
}

// NewInfinite constructs an unbounded Tilemap with Infinite set to true.
// Add sparse layers with NewChunkedTileLayer.
func NewInfinite(tileWidth, tileHeight float32, tilesets ...*Tileset) *Tilemap {
	return &Tilemap{
		TileWidth:  tileWidth,
		TileHeight: tileHeight,
		Infinite:   true,
		Tilesets:   tilesets,
	}
}

// IsChunked reports whether l uses sparse chunked storage rather than a dense Data slice.
func (l *TileLayer) IsChunked() bool {
	return l != nil && (l.chunks != nil || l.ChunkSize > 0)
}

// EffectiveChunkSize returns the power-of-two chunk size for a chunked layer, or 0 for a dense layer.
func (l *TileLayer) EffectiveChunkSize() int {
	if !l.IsChunked() {
		return 0
	}
	if l.chunks != nil {
		return l.chunks.size
	}
	return normalizeChunkSize(l.ChunkSize)
}

func (l *TileLayer) ensureChunks() *chunkGrid {
	if l.chunks == nil {
		sz := normalizeChunkSize(l.ChunkSize)
		l.ChunkSize = sz
		l.chunks = newChunkGrid(sz)
	}
	return l.chunks
}

// ChunkCount returns the number of currently loaded chunks in l, or 0 for dense or empty layers.
func (l *TileLayer) ChunkCount() int {
	if l == nil || l.chunks == nil {
		return 0
	}
	return len(l.chunks.chunks)
}

// Chunk returns the loaded chunk at coord, or nil if coord is not loaded.
func (l *TileLayer) Chunk(coord ChunkCoord) *Chunk {
	if l == nil || l.chunks == nil {
		return nil
	}
	return l.chunks.chunks[coord]
}

// ChunkCoords returns the coordinates of all currently loaded chunks in deterministic row-major (Y, X) order.
func (l *TileLayer) ChunkCoords() []ChunkCoord {
	if l == nil || l.chunks == nil || len(l.chunks.chunks) == 0 {
		return nil
	}
	out := make([]ChunkCoord, 0, len(l.chunks.chunks))
	for coord := range l.chunks.chunks {
		out = append(out, coord)
	}
	slices.SortFunc(out, compareChunkCoord)
	return out
}

// DirtyChunks returns the coordinates of all loaded chunks whose Dirty flag is set, sorted in deterministic (Y, X) order.
func (l *TileLayer) DirtyChunks() []ChunkCoord {
	if l == nil || l.chunks == nil || len(l.chunks.chunks) == 0 {
		return nil
	}
	var out []ChunkCoord
	for coord, ch := range l.chunks.chunks {
		if ch != nil && (ch.Dirty || (ch.cache != nil && ch.cache.Dirty)) {
			out = append(out, coord)
		}
	}
	slices.SortFunc(out, compareChunkCoord)
	return out
}

// ModifiedChunks returns the coordinates of all loaded chunks whose Modified flag is set, sorted in deterministic (Y, X) order.
func (l *TileLayer) ModifiedChunks() []ChunkCoord {
	if l == nil || l.chunks == nil || len(l.chunks.chunks) == 0 {
		return nil
	}
	var out []ChunkCoord
	for coord, ch := range l.chunks.chunks {
		if ch != nil && ch.Modified {
			out = append(out, coord)
		}
	}
	slices.SortFunc(out, compareChunkCoord)
	return out
}

// ChunkTileBounds returns the inclusive tile-coordinate bounding box [minCol, maxCol] × [minRow, maxRow]
// spanning all currently loaded chunks in l, or ok=false if no chunks are loaded.
func (l *TileLayer) ChunkTileBounds() (minCol, minRow, maxCol, maxRow int, ok bool) {
	if l == nil || l.chunks == nil || len(l.chunks.chunks) == 0 {
		return 0, 0, 0, 0, false
	}
	minCX, minCY := math.MaxInt, math.MaxInt
	maxCX, maxCY := math.MinInt, math.MinInt
	for coord := range l.chunks.chunks {
		if coord.X < minCX {
			minCX = coord.X
		}
		if coord.X > maxCX {
			maxCX = coord.X
		}
		if coord.Y < minCY {
			minCY = coord.Y
		}
		if coord.Y > maxCY {
			maxCY = coord.Y
		}
	}
	sz := l.chunks.size
	return minCX * sz, minCY * sz, maxCX*sz + sz - 1, maxCY*sz + sz - 1, true
}

func (l *TileLayer) tileAtChunked(col, row int) uint32 {
	if l.chunks == nil {
		return 0
	}
	coord, lx, ly := l.chunks.tileToChunk(col, row)
	ch := l.chunks.chunks[coord]
	if ch == nil {
		return 0
	}
	idx := ly*l.chunks.size + lx
	if idx >= len(ch.Tiles) {
		return 0
	}
	return ch.Tiles[idx]
}

// setTileChunked writes gid into a chunked layer at (col, row), returning whether the tile changed.
// When markModified is true, the chunk's Modified flag is set (gameplay edit); when false (initial load), Modified is left unchanged.
func (l *TileLayer) setTileChunked(col, row int, gid uint32, markModified bool) bool {
	g := l.ensureChunks()
	coord, lx, ly := g.tileToChunk(col, row)
	ch := g.chunks[coord]
	if ch == nil {
		if gid == 0 {
			return false
		}
		ch = NewChunk(coord, g.size)
		ch.Modified = markModified
		g.chunks[coord] = ch
	}
	idx := ly*g.size + lx
	if len(ch.Tiles) < g.size*g.size {
		grown := make([]uint32, g.size*g.size)
		copy(grown, ch.Tiles)
		ch.Tiles = grown
	}
	prev := ch.Tiles[idx]
	if prev == gid {
		return false
	}
	ch.Tiles[idx] = gid
	if prev == 0 && gid != 0 {
		ch.nonEmpty++
	} else if prev != 0 && gid == 0 && ch.nonEmpty > 0 {
		ch.nonEmpty--
	}
	ch.Dirty = true
	if markModified {
		ch.Modified = true
	}
	if ch.cache != nil {
		ch.cache.Dirty = true
	}
	return true
}

// hasChunkedLayer reports whether m is unbounded (Infinite is true or at least one tile layer is chunked).
func (m *Tilemap) hasChunkedLayer() bool {
	if m == nil {
		return false
	}
	if m.Infinite {
		return true
	}
	for i := range m.Layers {
		if m.Layers[i].IsChunked() {
			return true
		}
	}
	return false
}

// activeTileBounds returns the inclusive tile-coordinate bounding box covering every loaded chunk,
// dense tile layer, and cell flag override in m.
func (m *Tilemap) activeTileBounds() (minCol, minRow, maxCol, maxRow int, ok bool) {
	minCol, minRow = math.MaxInt, math.MaxInt
	maxCol, maxRow = math.MinInt, math.MinInt
	expand := func(c0, r0, c1, r1 int) {
		if c0 > c1 || r0 > r1 {
			return
		}
		if c0 < minCol {
			minCol = c0
		}
		if r0 < minRow {
			minRow = r0
		}
		if c1 > maxCol {
			maxCol = c1
		}
		if r1 > maxRow {
			maxRow = r1
		}
		ok = true
	}
	for i := range m.Layers {
		l := &m.Layers[i]
		if l.IsChunked() {
			if c0, r0, c1, r1, has := l.ChunkTileBounds(); has {
				expand(c0, r0, c1, r1)
			}
			continue
		}
		lw, lh := m.layerSize(l)
		if lw > 0 && lh > 0 && len(l.Data) > 0 {
			expand(0, 0, lw-1, lh-1)
		}
	}
	for k := range m.cellFlags {
		expand(k.col, k.row, k.col, k.row)
	}
	return minCol, minRow, maxCol, maxRow, ok
}

// ChunkAt returns the loaded chunk at coord in layerIndex, or nil if layerIndex is out of range,
// dense, or does not currently have coord loaded.
func (m *Tilemap) ChunkAt(layerIndex int, coord ChunkCoord) *Chunk {
	if m == nil || layerIndex < 0 || layerIndex >= len(m.Layers) {
		return nil
	}
	return m.Layers[layerIndex].Chunk(coord)
}

// ChunkCoordForTile converts tile (col, row) to the ChunkCoord owning it in layerIndex.
// Returns ok=false if layerIndex is out of range or not a chunked layer.
func (m *Tilemap) ChunkCoordForTile(layerIndex, col, row int) (ChunkCoord, bool) {
	if m == nil || layerIndex < 0 || layerIndex >= len(m.Layers) {
		return ChunkCoord{}, false
	}
	l := &m.Layers[layerIndex]
	if !l.IsChunked() {
		return ChunkCoord{}, false
	}
	g := l.ensureChunks()
	coord, _, _ := g.tileToChunk(col, row)
	return coord, true
}

// PutChunk installs chunk into the chunked layer at layerIndex, replacing any existing chunk at chunk.Coord.
//
// chunk.Size must match the layer's EffectiveChunkSize() and len(chunk.Tiles) must equal chunk.Size*chunk.Size.
// The installed chunk's Dirty flag is set to true so any static chunk cache is rebuilt, while chunk.Modified is preserved as passed.
func (m *Tilemap) PutChunk(layerIndex int, chunk *Chunk) error {
	if m == nil {
		return fmt.Errorf("tilemap: put chunk: nil tilemap")
	}
	if layerIndex < 0 || layerIndex >= len(m.Layers) {
		return fmt.Errorf("tilemap: put chunk: layer index %d out of range [0, %d)", layerIndex, len(m.Layers))
	}
	if chunk == nil {
		return fmt.Errorf("tilemap: put chunk: nil chunk")
	}
	l := &m.Layers[layerIndex]
	if !l.IsChunked() {
		return fmt.Errorf("tilemap: put chunk: layer %d (%q) is not chunked", layerIndex, l.Name)
	}
	g := l.ensureChunks()
	if chunk.Size != g.size {
		return fmt.Errorf("tilemap: put chunk: chunk size %d does not match layer chunk size %d", chunk.Size, g.size)
	}
	wantLen := g.size * g.size
	if len(chunk.Tiles) != wantLen {
		return fmt.Errorf("tilemap: put chunk: chunk has %d tiles, want %d (%dx%d)", len(chunk.Tiles), wantLen, g.size, g.size)
	}
	chunk.recountNonEmpty()
	chunk.Dirty = true
	chunk.cache = nil
	g.chunks[chunk.Coord] = chunk
	m.recordBulkTileEdit()
	return nil
}

// RemoveChunk unloads and returns the chunk at coord from layerIndex (clearing its static cache),
// or returns nil if layerIndex is out of range, dense, or coord is not loaded.
func (m *Tilemap) RemoveChunk(layerIndex int, coord ChunkCoord) *Chunk {
	if m == nil || layerIndex < 0 || layerIndex >= len(m.Layers) {
		return nil
	}
	l := &m.Layers[layerIndex]
	if l.chunks == nil {
		return nil
	}
	ch, ok := l.chunks.chunks[coord]
	if !ok {
		return nil
	}
	ch.cache = nil
	delete(l.chunks.chunks, coord)
	m.recordBulkTileEdit()
	return ch
}
