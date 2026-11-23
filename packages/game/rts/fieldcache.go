package rts

import (
	"slices"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math2d"
)

// DefaultFieldCacheSize is the number of flow fields a FieldCache keeps when NewFieldCache is given a capacity of 0 or less.
const DefaultFieldCacheSize = 16

// FieldCache builds flow fields over one grid and keeps the most recently used ones, so units ordered to the same goals share a single field instead of each building their own.
//
// Fields are keyed by their goal set, compared as a sorted set of distinct cells, so the same goals given in another order or with repeats find the same field. When the cache is full the least recently used field is dropped; a unit still holding it keeps following it, Crowd.Rebuild still rebuilds it after a grid change, and the cache builds a fresh one the next time those goals are asked for. A FieldCache is not safe for concurrent use.
type FieldCache struct {
	grid     ai.Grid
	origin   math2d.Vec2
	cellSize float32
	capacity int
	diagonal bool

	entries []*cacheEntry
	clock   uint64
	builds  uint64
	scratch []ai.Cell
}

type cacheEntry struct {
	key   []ai.Cell
	field *FlowField
	used  uint64
}

// NewFieldCache returns a cache of up to capacity 8-way flow fields over grid, whose cells are cellSize world units wide starting at origin.
func NewFieldCache(grid ai.Grid, origin math2d.Vec2, cellSize float32, capacity int) *FieldCache {
	if capacity <= 0 {
		capacity = DefaultFieldCacheSize
	}
	return &FieldCache{grid: grid, origin: origin, cellSize: validCellSize(cellSize), capacity: capacity, diagonal: true}
}

// Grid returns the grid the cache builds fields over.
func (fc *FieldCache) Grid() ai.Grid { return fc.grid }

// Origin returns the world position of the top-left corner of cell (0, 0).
func (fc *FieldCache) Origin() math2d.Vec2 { return fc.origin }

// CellSize returns the width and height of a cell in world units.
func (fc *FieldCache) CellSize() float32 { return fc.cellSize }

// Len returns the number of fields the cache holds.
func (fc *FieldCache) Len() int { return len(fc.entries) }

// Builds returns how many fields the cache has built or rebuilt since it was made, which tells a test or a profiler whether units are sharing fields.
func (fc *FieldCache) Builds() uint64 { return fc.builds }

// Field returns the flow field toward goals, building it on first use, or nil and false when no goal is an in-bounds walkable cell.
func (fc *FieldCache) Field(goals ...ai.Cell) (*FlowField, bool) {
	key := normalizeGoals(fc.scratch[:0], goals)
	fc.scratch = key
	fc.clock++
	for _, e := range fc.entries {
		if slices.Equal(e.key, key) {
			e.used = fc.clock
			return e.field, true
		}
	}
	f := NewFlowField(fc.origin, fc.cellSize)
	f.Diagonal = fc.diagonal
	fc.builds++
	if !f.Build(fc.grid, key...) {
		return nil, false
	}
	e := &cacheEntry{key: slices.Clone(key), field: f, used: fc.clock}
	if len(fc.entries) < fc.capacity {
		fc.entries = append(fc.entries, e)
		return f, true
	}
	oldest := 0
	for i, x := range fc.entries {
		if x.used < fc.entries[oldest].used {
			oldest = i
		}
	}
	fc.entries[oldest] = e
	return f, true
}

// Rebuild integrates every cached field again over the grid, in place, so units following them see a change to the grid (a wall built, a bridge destroyed) on their next step. A field none of whose goals is walkable any more is dropped from the cache, so the next Field call for those goals reports false. Call it after changing the grid's walkable cells or costs; a Crowd's own Rebuild calls it and also rebuilds the fields its units still follow after the cache dropped them.
func (fc *FieldCache) Rebuild() {
	kept := fc.entries[:0]
	for _, e := range fc.entries {
		fc.builds++
		if e.field.Build(fc.grid, e.key...) {
			kept = append(kept, e)
		}
	}
	clear(fc.entries[len(kept):])
	fc.entries = kept
}

// Clear drops every cached field.
func (fc *FieldCache) Clear() {
	clear(fc.entries)
	fc.entries = fc.entries[:0]
}

// normalizeGoals writes the distinct cells of goals into dst in row-major order.
func normalizeGoals(dst, goals []ai.Cell) []ai.Cell {
	dst = append(dst, goals...)
	slices.SortFunc(dst, func(a, b ai.Cell) int {
		if a.Row != b.Row {
			return a.Row - b.Row
		}
		return a.Col - b.Col
	})
	return slices.Compact(dst)
}
