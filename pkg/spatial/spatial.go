package spatial

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultSpatialCellSize = 64
	initialBucketCap       = 8
)

type cellKey struct {
	x, y int32
}

type spatialEntry[ID comparable] struct {
	id         ID
	bounds     core.Bounds
	minX, maxX int32
	minY, maxY int32
	queryVisit uint64
	pairVisit  uint64
	empty      bool
	alive      bool
}

// Hash is a uniform 2D grid broad-phase index for fast area queries,
// point queries, and collision pair generation.
//
// Clear retains all bucket slices and map tables so per-frame rebuilds
// (Clear + Insert + Query) run with 0 allocations in steady state. Query and
// QueryPairs iterate cells in deterministic row-major order without iterating
// over Go maps, deduplicating multi-cell entries via generation counters.
type Hash[ID comparable] struct {
	cellSize     float32
	cellSize64   float64
	cellToBucket map[cellKey]int32
	buckets      [][]int32
	usedBuckets  int

	idToIndex   map[ID]int32
	entries     []spatialEntry[ID]
	freeEntries []int32
	count       int

	queryGen uint64
	pairGen  uint64
}

// NewHash returns a Hash with uniform square cells of cellSize
// points. If cellSize <= 0 or non-finite, it defaults to 64.
func NewHash[ID comparable](cellSize float32) *Hash[ID] {
	cs := float64(cellSize)
	if !(cs > 0) || math.IsInf(cs, 0) {
		cellSize = defaultSpatialCellSize
		cs = defaultSpatialCellSize
	}
	return &Hash[ID]{
		cellSize:     cellSize,
		cellSize64:   cs,
		cellToBucket: make(map[cellKey]int32),
		idToIndex:    make(map[ID]int32),
	}
}

// CellSize returns the grid cell width and height in points.
func (sh *Hash[ID]) CellSize() float32 { return sh.cellSize }

// Len returns the number of items currently stored in the spatial hash.
func (sh *Hash[ID]) Len() int { return sh.count }

// Clear removes all items from the spatial hash while keeping all bucket slice
// capacities and internal map tables for zero-allocation reuse on subsequent
// frames.
func (sh *Hash[ID]) Clear() {
	for i := 0; i < sh.usedBuckets; i++ {
		sh.buckets[i] = sh.buckets[i][:0]
	}
	sh.usedBuckets = 0
	clear(sh.cellToBucket)
	clear(sh.idToIndex)
	clear(sh.entries)
	sh.entries = sh.entries[:0]
	sh.freeEntries = sh.freeEntries[:0]
	sh.count = 0
}

// Insert adds id with bounds to the spatial hash. If id is already present,
// its bounds are updated in place.
func (sh *Hash[ID]) Insert(id ID, bounds core.Bounds) {
	if _, exists := sh.idToIndex[id]; exists {
		sh.Update(id, bounds)
		return
	}

	var eIdx int32
	if n := len(sh.freeEntries); n > 0 {
		eIdx = sh.freeEntries[n-1]
		sh.freeEntries = sh.freeEntries[:n-1]
	} else {
		eIdx = int32(len(sh.entries))
		sh.entries = append(sh.entries, spatialEntry[ID]{})
	}

	minX, maxX, minY, maxY, ok := sh.boundsCells(bounds)
	sh.entries[eIdx] = spatialEntry[ID]{
		id:     id,
		bounds: bounds,
		minX:   minX,
		maxX:   maxX,
		minY:   minY,
		maxY:   maxY,
		empty:  !ok,
		alive:  true,
	}
	sh.idToIndex[id] = eIdx
	sh.count++

	if ok {
		sh.addToCells(eIdx, minX, maxX, minY, maxY)
	}
}

// Remove deletes id from the spatial hash. Removing an absent id is a no-op.
func (sh *Hash[ID]) Remove(id ID) {
	eIdx, exists := sh.idToIndex[id]
	if !exists {
		return
	}
	e := &sh.entries[eIdx]
	if !e.empty {
		sh.removeFromCells(eIdx, e.minX, e.maxX, e.minY, e.maxY)
	}
	var zeroID ID
	*e = spatialEntry[ID]{id: zeroID}
	delete(sh.idToIndex, id)
	sh.freeEntries = append(sh.freeEntries, eIdx)
	sh.count--
}

// Update changes the bounds associated with id. If id is not yet in the
// spatial hash, Update inserts it. When the item stays within the same grid
// cells, Update completes in O(1) without touching any buckets.
func (sh *Hash[ID]) Update(id ID, bounds core.Bounds) {
	eIdx, exists := sh.idToIndex[id]
	if !exists {
		sh.Insert(id, bounds)
		return
	}
	e := &sh.entries[eIdx]
	minX, maxX, minY, maxY, ok := sh.boundsCells(bounds)

	if !e.empty && ok && minX == e.minX && maxX == e.maxX && minY == e.minY && maxY == e.maxY {
		e.bounds = bounds
		return
	}
	if !e.empty {
		sh.removeFromCells(eIdx, e.minX, e.maxX, e.minY, e.maxY)
	}
	e.bounds = bounds
	e.minX, e.maxX, e.minY, e.maxY = minX, maxX, minY, maxY
	e.empty = !ok
	if ok {
		sh.addToCells(eIdx, minX, maxX, minY, maxY)
	}
}

// Query appends all IDs whose bounds intersect area to out and returns the
// extended slice. Multi-cell entries are reported once in deterministic order.
// Pass out[:0] to reuse a buffer across frames with 0 allocations.
func (sh *Hash[ID]) Query(area core.Bounds, out []ID) []ID {
	if sh.count == 0 {
		return out
	}
	minX, maxX, minY, maxY, ok := sh.boundsCells(area)
	if !ok {
		return out
	}
	gen := sh.nextQueryVisit()
	for cy := minY; cy <= maxY; cy++ {
		for cx := minX; cx <= maxX; cx++ {
			bIdx, exists := sh.cellToBucket[cellKey{cx, cy}]
			if !exists {
				continue
			}
			for _, eIdx := range sh.buckets[bIdx] {
				e := &sh.entries[eIdx]
				if e.queryVisit == gen {
					continue
				}
				e.queryVisit = gen
				if Intersects(e.bounds, area) {
					out = append(out, e.id)
				}
			}
		}
	}
	return out
}

// QueryPoint appends all IDs whose bounds contain p to out and returns the
// extended slice.
func (sh *Hash[ID]) QueryPoint(p core.Point, out []ID) []ID {
	if sh.count == 0 || !isFinitePoint(p) {
		return out
	}
	cx := clampInt32(math.Floor(float64(p.X) / sh.cellSize64))
	cy := clampInt32(math.Floor(float64(p.Y) / sh.cellSize64))
	bIdx, exists := sh.cellToBucket[cellKey{cx, cy}]
	if !exists {
		return out
	}
	for _, eIdx := range sh.buckets[bIdx] {
		e := &sh.entries[eIdx]
		if e.bounds.Contains(p) {
			out = append(out, e.id)
		}
	}
	return out
}

// QueryPairs invokes fn once for every unordered pair (a, b) of distinct
// intersecting items in the spatial hash, without duplicates and with zero
// allocations. Callers must not mutate the spatial hash during fn.
func (sh *Hash[ID]) QueryPairs(fn func(a, b ID)) {
	if fn == nil || sh.count < 2 {
		return
	}
	n := int32(len(sh.entries))
	for i := int32(0); i < n; i++ {
		ea := &sh.entries[i]
		if !ea.alive || ea.empty {
			continue
		}
		gen := sh.nextPairVisit()
		for cy := ea.minY; cy <= ea.maxY; cy++ {
			for cx := ea.minX; cx <= ea.maxX; cx++ {
				bIdx, exists := sh.cellToBucket[cellKey{cx, cy}]
				if !exists {
					continue
				}
				for _, j := range sh.buckets[bIdx] {
					if j <= i {
						continue
					}
					eb := &sh.entries[j]
					if eb.pairVisit == gen {
						continue
					}
					eb.pairVisit = gen
					if Intersects(ea.bounds, eb.bounds) {
						fn(ea.id, eb.id)
					}
				}
			}
		}
	}
}

func (sh *Hash[ID]) addToCells(eIdx int32, minX, maxX, minY, maxY int32) {
	for cy := minY; cy <= maxY; cy++ {
		for cx := minX; cx <= maxX; cx++ {
			key := cellKey{cx, cy}
			bIdx, exists := sh.cellToBucket[key]
			if !exists {
				bIdx = int32(sh.usedBuckets)
				if sh.usedBuckets == len(sh.buckets) {
					sh.buckets = append(sh.buckets, make([]int32, 0, initialBucketCap))
				}
				sh.usedBuckets++
				sh.cellToBucket[key] = bIdx
			}
			sh.buckets[bIdx] = append(sh.buckets[bIdx], eIdx)
		}
	}
}

func (sh *Hash[ID]) removeFromCells(eIdx int32, minX, maxX, minY, maxY int32) {
	for cy := minY; cy <= maxY; cy++ {
		for cx := minX; cx <= maxX; cx++ {
			bIdx, exists := sh.cellToBucket[cellKey{cx, cy}]
			if !exists {
				continue
			}
			bucket := sh.buckets[bIdx]
			for i, v := range bucket {
				if v == eIdx {
					last := len(bucket) - 1
					bucket[i] = bucket[last]
					sh.buckets[bIdx] = bucket[:last]
					break
				}
			}
		}
	}
}

func (sh *Hash[ID]) boundsCells(b core.Bounds) (minX, maxX, minY, maxY int32, ok bool) {
	if !validNonEmptyBounds(b) {
		return 0, 0, 0, 0, false
	}
	minX = clampInt32(math.Floor(float64(b.Origin.X) / sh.cellSize64))
	maxX = clampInt32(math.Floor(float64(b.Origin.X+b.Size.Width) / sh.cellSize64))
	minY = clampInt32(math.Floor(float64(b.Origin.Y) / sh.cellSize64))
	maxY = clampInt32(math.Floor(float64(b.Origin.Y+b.Size.Height) / sh.cellSize64))
	return minX, maxX, minY, maxY, true
}

func (sh *Hash[ID]) nextQueryVisit() uint64 {
	sh.queryGen++
	if sh.queryGen == 0 {
		for i := range sh.entries {
			sh.entries[i].queryVisit = 0
		}
		sh.queryGen = 1
	}
	return sh.queryGen
}

func (sh *Hash[ID]) nextPairVisit() uint64 {
	sh.pairGen++
	if sh.pairGen == 0 {
		for i := range sh.entries {
			sh.entries[i].pairVisit = 0
		}
		sh.pairGen = 1
	}
	return sh.pairGen
}

func validNonEmptyBounds(b core.Bounds) bool {
	if !(b.Size.Width > 0 && b.Size.Height > 0) {
		return false
	}
	x0 := float64(b.Origin.X)
	y0 := float64(b.Origin.Y)
	x1 := float64(b.Origin.X + b.Size.Width)
	y1 := float64(b.Origin.Y + b.Size.Height)
	return !math.IsNaN(x0) && !math.IsInf(x0, 0) &&
		!math.IsNaN(y0) && !math.IsInf(y0, 0) &&
		!math.IsNaN(x1) && !math.IsInf(x1, 0) &&
		!math.IsNaN(y1) && !math.IsInf(y1, 0)
}

func isFinitePoint(p core.Point) bool {
	x, y := float64(p.X), float64(p.Y)
	return !math.IsNaN(x) && !math.IsInf(x, 0) && !math.IsNaN(y) && !math.IsInf(y, 0)
}

func clampInt32(v float64) int32 {
	if v < math.MinInt32 {
		return math.MinInt32
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v)
}
