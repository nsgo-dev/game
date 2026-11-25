package ai

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// InfluenceSourceID identifies a tracked source registered on an InfluenceLayer.
// The zero value is invalid.
type InfluenceSourceID uint64

// FalloffKind selects how a stamped InfluenceSource attenuates with Euclidean cell distance from its center cell.
type FalloffKind uint8

const (
	// FalloffLinear decreases linearly from 1 at distance 0 to 0 at Radius: max(0, 1 - dist/Radius).
	FalloffLinear FalloffKind = iota
	// FalloffExponential decreases as exp(-FalloffRate * dist) for dist <= Radius (FalloffRate defaults to 1 when <= 0).
	FalloffExponential
	// FalloffCustom delegates to InfluenceSource.CustomFalloff(dist, radius).
	FalloffCustom
)

// InfluenceSource describes a point emitter stamped onto an InfluenceLayer.
type InfluenceSource struct {
	// Cell is the grid coordinate at the center of the source.
	Cell Cell
	// Strength is the peak influence value stamped at Cell. It may be positive or negative.
	Strength float32
	// Radius is the maximum stamp distance in grid cells. Values <= 0 stamp only Cell.
	Radius float32
	// Falloff selects the attenuation curve (defaults to FalloffLinear).
	Falloff FalloffKind
	// FalloffRate controls exponential attenuation speed for FalloffExponential; values <= 0 default to 1.
	FalloffRate float32
	// CustomFalloff evaluates the attenuation multiplier for FalloffCustom given cell distance and radius.
	// If nil, FalloffLinear is used. Non-finite return values are treated as 0.
	CustomFalloff func(dist, radius float32) float32
}

// SpreadOptions configures iterative influence propagation around blocked cells on a Grid.
type SpreadOptions struct {
	// Decay is the fraction of influence lost per unit cell distance in [0, 1].
	// Cardinal neighbors retain (1 - Decay); diagonal neighbors retain (1 - Decay)^sqrt(2).
	// Values <= 0 keep full strength (retention 1); values >= 1 or NaN prevent spreading (retention 0).
	Decay float32
	// Momentum blends previous cell values with newly propagated targets in [0, 1]:
	// next = prev*Momentum + target*(1 - Momentum).
	// Values <= 0 or NaN replace immediately; values >= 1 keep previous values unchanged.
	Momentum float32
	// Iterations is the number of propagation passes to run; values <= 0 run 1 pass.
	Iterations int
	// Diagonal allows 8-way propagation with step distance sqrt(2) and no corner-cutting around blocked cells.
	// False restricts propagation to the 4 cardinal directions.
	Diagonal bool
}

// WeightedLayer pairs an InfluenceLayer with a scalar weight for InfluenceLayer.Combine.
type WeightedLayer struct {
	Layer  *InfluenceLayer
	Weight float32
}

// InfluenceQuery configures a spatial extremum search (HighestInRadius / LowestInRadius).
type InfluenceQuery struct {
	// Grid optionally filters candidate cells by walkability (and reachability when Reachable is true).
	// When nil, all in-bounds cells within Radius are considered.
	Grid Grid
	// Reachable, when true and Grid is non-nil, restricts candidates to cells reachable from the center cell
	// by walking through Grid.Walkable cells within Radius.
	Reachable bool
	// Diagonal allows 8-way steps (with no corner-cutting around blocked cells) during reachability search.
	// Ignored when Reachable is false.
	Diagonal bool
	// ExcludeCenter omits the center cell itself from the candidates when true.
	ExcludeCenter bool
}

// HeatmapOptions configures InfluenceLayer.DrawHeatmap.
type HeatmapOptions struct {
	// PositiveColor is the tint for positive cell values; defaults to green (0x22C55E) when 0.
	PositiveColor core.Color
	// NegativeColor is the tint for negative cell values; defaults to red (0xEF4444) when 0.
	NegativeColor core.Color
	// MaxAbs is the reference magnitude mapped to full MaxAlpha.
	// When <= 0, it is computed automatically from the layer's maximum absolute cell value.
	MaxAbs float32
	// MaxAlpha caps the opacity of peak-magnitude cells in (0, 1]; defaults to 0.65 when <= 0.
	MaxAlpha float32
	// MinThreshold skips cells whose absolute value is <= MinThreshold; defaults to 1e-4 when <= 0.
	MinThreshold float32
	// DrawGrid draws subtle cell outlines over the visible grid region when true.
	DrawGrid bool
	// GridColor is the stroke color when DrawGrid is true; defaults to translucent white when 0.
	GridColor core.Color
}

type cellRect struct {
	minCol, minRow int
	maxCol, maxRow int
}

type sourceSlot struct {
	src        InfluenceSource
	stamped    InfluenceSource
	gen        uint32
	active     bool
	hasStamped bool
	dirty      bool
}

type spreadStep struct {
	dc, dr int
	diag   bool
}

// Fixed neighbor order for deterministic propagation and reachability BFS.
var influenceSteps = [8]spreadStep{
	{0, -1, false},
	{1, 0, false},
	{0, 1, false},
	{-1, 0, false},
	{1, -1, true},
	{1, 1, true},
	{-1, 1, true},
	{-1, -1, true},
}

// InfluenceLayer stores a 2D scalar influence grid matching an ai.Grid's dimensions.
//
// Cells map to world coordinates via Origin and CellSize in the same convention as rts.FlowField:
// cell (col, row) covers [Origin.X+col*CellSize, Origin.X+(col+1)*CellSize) horizontally and
// [Origin.Y+row*CellSize, Origin.Y+(row+1)*CellSize) vertically.
//
// Tracked sources registered with AddSource update incrementally on Update or Spread, touching only
// old and new footprints of changed sources while matching a full Rebuild bit-for-bit. After warm-up,
// incremental updates, spreading, layer combinations, and spatial queries perform zero heap allocations.
//
// An InfluenceLayer is not safe for concurrent use by multiple goroutines.
type InfluenceLayer struct {
	// Origin is the world position of the top-left corner of cell (0, 0).
	Origin math2d.Vec2
	// CellSize is the width and height of one cell in world units; values <= 0 or non-finite behave as 1.
	CellSize float32

	cols, rows int
	values     []float32
	base       []float32
	scratch    []float32
	walkable   []uint8

	// Tracked source registry and incremental dirty tracking.
	slots           []sourceSlot
	freeSlots       []uint32
	sourceCount     int
	tracked         bool
	valuesSynced    bool
	dirty           bool
	fullDirty       bool
	dirtySlots      []int
	extraDirtyBoxes []cellRect
	dirtyBoxes      []cellRect
	dirtyGen        []uint32
	curDirtyGen     uint32
	dirtyCells      []int

	// Reusable buffers for zero-allocation reachability BFS queries.
	bfsGen    []uint32
	bfsCurGen uint32
	bfsQueue  []int
}

// NewInfluenceLayer returns a cols-by-rows InfluenceLayer with CellSize 1 and Origin (0, 0).
func NewInfluenceLayer(cols, rows int) *InfluenceLayer {
	l := &InfluenceLayer{CellSize: 1}
	l.Resize(cols, rows)
	return l
}

// NewInfluenceLayerForGrid returns an InfluenceLayer sized to g.GridSize() with the given world origin and cellSize.
// If g is nil, the returned layer has size (0, 0).
func NewInfluenceLayerForGrid(g Grid, origin math2d.Vec2, cellSize float32) *InfluenceLayer {
	var cols, rows int
	if g != nil {
		cols, rows = g.GridSize()
	}
	l := &InfluenceLayer{
		Origin:   origin,
		CellSize: cellSize,
	}
	l.Resize(cols, rows)
	return l
}

// Size returns the layer's width in columns and height in rows.
func (l *InfluenceLayer) Size() (cols, rows int) {
	if l == nil {
		return 0, 0
	}
	return l.cols, l.rows
}

// Resize changes the layer's dimensions to cols-by-rows, clearing its cell values and reusing backing slices when capacity permits.
// Any registered tracked sources are retained and marked for full recomputation on the next Update or Spread.
func (l *InfluenceLayer) Resize(cols, rows int) {
	if cols < 0 {
		cols = 0
	}
	if rows < 0 {
		rows = 0
	}
	l.cols, l.rows = cols, rows
	n := cols * rows
	if cap(l.values) < n {
		l.values = make([]float32, n)
	} else {
		l.values = l.values[:n]
		clear(l.values)
	}
	if cap(l.base) < n {
		l.base = make([]float32, n)
	} else {
		l.base = l.base[:n]
		clear(l.base)
	}
	if cap(l.dirtyGen) < n {
		l.dirtyGen = make([]uint32, n)
		l.curDirtyGen = 0
	} else {
		l.dirtyGen = l.dirtyGen[:n]
	}
	l.dirtySlots = l.dirtySlots[:0]
	l.extraDirtyBoxes = l.extraDirtyBoxes[:0]
	l.dirtyBoxes = l.dirtyBoxes[:0]
	l.dirtyCells = l.dirtyCells[:0]
	for i := range l.slots {
		l.slots[i].hasStamped = false
		l.slots[i].dirty = false
	}
	if l.sourceCount > 0 {
		l.dirty = true
		l.fullDirty = true
	} else {
		l.dirty = false
		l.fullDirty = false
	}
	l.valuesSynced = true
}

// Clear zeroes all cell values in the layer. If tracked sources exist, they are marked for full recomputation on the next Update or Spread.
func (l *InfluenceLayer) Clear() {
	clear(l.values)
	clear(l.base)
	l.dirtySlots = l.dirtySlots[:0]
	l.extraDirtyBoxes = l.extraDirtyBoxes[:0]
	l.dirtyBoxes = l.dirtyBoxes[:0]
	l.dirtyCells = l.dirtyCells[:0]
	for i := range l.slots {
		l.slots[i].hasStamped = false
		l.slots[i].dirty = false
	}
	if l.sourceCount > 0 {
		l.dirty = true
		l.fullDirty = true
	} else {
		l.tracked = false
		l.dirty = false
		l.fullDirty = false
	}
	l.valuesSynced = true
}

// Fill sets every cell in the layer to v (non-finite values are treated as 0).
func (l *InfluenceLayer) Fill(v float32) {
	v = sanitizeFloat32(v)
	for i := range l.values {
		l.values[i] = v
	}
	l.tracked = false
	l.valuesSynced = false
}

// CopyFrom copies src's dimensions, Origin, CellSize, and cell values into l without allocating when l already has sufficient capacity.
func (l *InfluenceLayer) CopyFrom(src *InfluenceLayer) {
	if l == nil || src == nil {
		return
	}
	if l == src {
		return
	}
	l.Origin = src.Origin
	l.CellSize = src.CellSize
	l.ensureValuesSize(src.cols, src.rows)
	copy(l.values, src.values)
	l.tracked = false
	l.valuesSynced = false
}

// Values returns the layer's flat row-major cell slice (index row*cols + col).
// The returned slice aliases the layer's internal storage and changes on subsequent updates.
func (l *InfluenceLayer) Values() []float32 {
	if l == nil {
		return nil
	}
	return l.values
}

// At returns the value at (col, row), or 0 when out of bounds.
func (l *InfluenceLayer) At(col, row int) float32 {
	if l == nil || !l.inBounds(col, row) {
		return 0
	}
	return l.values[row*l.cols+col]
}

// Set writes v into cell (col, row). Out-of-bounds coordinates are ignored and non-finite values are stored as 0.
func (l *InfluenceLayer) Set(col, row int, v float32) {
	if l == nil || !l.inBounds(col, row) {
		return
	}
	l.values[row*l.cols+col] = sanitizeFloat32(v)
	l.valuesSynced = false
}

func (l *InfluenceLayer) inBounds(col, row int) bool {
	return col >= 0 && col < l.cols && row >= 0 && row < l.rows
}

func (l *InfluenceLayer) cellSize() float32 {
	if !(l.CellSize > 0) || math.IsInf(float64(l.CellSize), 0) {
		return 1
	}
	return l.CellSize
}

// CellAt returns the cell containing world position p and whether p lies inside the grid.
func (l *InfluenceLayer) CellAt(p math2d.Vec2) (Cell, bool) {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return Cell{}, false
	}
	size := float64(l.cellSize())
	x := (float64(p.X) - float64(l.Origin.X)) / size
	y := (float64(p.Y) - float64(l.Origin.Y)) / size
	if math.IsNaN(x) || math.IsNaN(y) {
		return Cell{}, false
	}
	fx, fy := math.Floor(x), math.Floor(y)
	if fx < 0 || fy < 0 || fx >= float64(l.cols) || fy >= float64(l.rows) {
		return Cell{
			Col: int(max(min(fx, float64(l.cols)), -1)),
			Row: int(max(min(fy, float64(l.rows)), -1)),
		}, false
	}
	return Cell{Col: int(fx), Row: int(fy)}, true
}

// CellCenter returns the world position at the center of cell c.
func (l *InfluenceLayer) CellCenter(c Cell) math2d.Vec2 {
	size := l.cellSize()
	return math2d.Vec2{
		X: l.Origin.X + (float32(c.Col)+0.5)*size,
		Y: l.Origin.Y + (float32(c.Row)+0.5)*size,
	}
}

// Stamp additively stamps src directly onto the layer's current cell values, skipping blocked cells when g is non-nil.
// Direct stamps are not tracked for incremental moves; use AddSource and Update for dynamic tracked sources.
func (l *InfluenceLayer) Stamp(g Grid, src InfluenceSource) {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return
	}
	strength := sanitizeFloat32(src.Strength)
	if strength == 0 {
		return
	}
	rect, ok := l.footprintRect(src)
	if !ok {
		return
	}
	rad := sanitizeRadius(src.Radius)
	for row := rect.minRow; row <= rect.maxRow; row++ {
		rowOff := row * l.cols
		for col := rect.minCol; col <= rect.maxCol; col++ {
			if g != nil && !g.Walkable(col, row) {
				continue
			}
			f := evalFalloff(col, row, src, rad)
			if f != 0 {
				l.values[rowOff+col] += strength * f
			}
		}
	}
	l.valuesSynced = false
}

// AddSource registers a tracked source and returns its handle for future MoveSource, SetSource, or RemoveSource calls.
// The source takes effect on the next Update, Rebuild, or Spread call.
func (l *InfluenceLayer) AddSource(src InfluenceSource) InfluenceSourceID {
	var idx int
	if n := len(l.freeSlots); n > 0 {
		idx = int(l.freeSlots[n-1])
		l.freeSlots = l.freeSlots[:n-1]
	} else {
		idx = len(l.slots)
		l.slots = append(l.slots, sourceSlot{gen: 1})
	}
	s := &l.slots[idx]
	if s.gen == 0 {
		s.gen = 1
	}
	s.src = src
	s.active = true
	s.hasStamped = false
	if !s.dirty {
		s.dirty = true
		l.dirtySlots = append(l.dirtySlots, idx)
	}
	l.sourceCount++
	l.tracked = true
	l.dirty = true
	return makeSourceID(s.gen, uint32(idx))
}

// SetSource replaces the parameters of the tracked source id, marking its old and new footprints dirty.
// It returns false if id is invalid or was already removed.
func (l *InfluenceLayer) SetSource(id InfluenceSourceID, src InfluenceSource) bool {
	idx, ok := l.lookupSlot(id)
	if !ok {
		return false
	}
	s := &l.slots[idx]
	s.src = src
	if !s.dirty {
		s.dirty = true
		l.dirtySlots = append(l.dirtySlots, idx)
	}
	l.tracked = true
	l.dirty = true
	return true
}

// MoveSource moves the tracked source id to cell, marking its old and new footprints dirty only if cell changed.
// It returns false if id is invalid or was already removed.
func (l *InfluenceLayer) MoveSource(id InfluenceSourceID, cell Cell) bool {
	idx, ok := l.lookupSlot(id)
	if !ok {
		return false
	}
	s := &l.slots[idx]
	if s.src.Cell == cell {
		return true
	}
	s.src.Cell = cell
	if !s.dirty {
		s.dirty = true
		l.dirtySlots = append(l.dirtySlots, idx)
	}
	l.tracked = true
	l.dirty = true
	return true
}

// RemoveSource unregisters the tracked source id and schedules its stamped footprint to be cleared on the next Update or Spread.
// It returns false if id is invalid or was already removed.
func (l *InfluenceLayer) RemoveSource(id InfluenceSourceID) bool {
	idx, ok := l.lookupSlot(id)
	if !ok {
		return false
	}
	s := &l.slots[idx]
	if s.hasStamped {
		if r, valid := l.footprintRect(s.stamped); valid {
			l.extraDirtyBoxes = append(l.extraDirtyBoxes, r)
			l.dirty = true
		}
		s.hasStamped = false
	}
	s.active = false
	s.gen++
	if s.gen == 0 {
		s.gen = 1
	}
	l.sourceCount--
	l.freeSlots = append(l.freeSlots, uint32(idx))
	return true
}

// Source returns the current parameters of tracked source id, or false if id is invalid or removed.
func (l *InfluenceLayer) Source(id InfluenceSourceID) (InfluenceSource, bool) {
	idx, ok := l.lookupSlot(id)
	if !ok {
		return InfluenceSource{}, false
	}
	return l.slots[idx].src, true
}

// SourceCount returns the number of active tracked sources registered on the layer.
func (l *InfluenceLayer) SourceCount() int {
	if l == nil {
		return 0
	}
	return l.sourceCount
}

// ClearSources removes all tracked sources and marks the layer dirty so the next Update or Spread clears their influence.
func (l *InfluenceLayer) ClearSources() {
	if l == nil {
		return
	}
	hadAny := l.sourceCount > 0 || l.dirty
	l.slots = l.slots[:0]
	l.freeSlots = l.freeSlots[:0]
	l.dirtySlots = l.dirtySlots[:0]
	l.extraDirtyBoxes = l.extraDirtyBoxes[:0]
	l.sourceCount = 0
	if hadAny {
		l.dirty = true
		l.fullDirty = true
	}
}

// MarkDirtyCell marks cell c for incremental recomputation on the next Update or Spread,
// useful when a single obstacle on the underlying Grid toggles walkability.
func (l *InfluenceLayer) MarkDirtyCell(c Cell) {
	l.MarkDirtyRect(c.Col, c.Row, c.Col, c.Row)
}

// MarkDirtyRect marks the inclusive cell rectangle [minCol..maxCol] x [minRow..maxRow] for incremental recomputation.
func (l *InfluenceLayer) MarkDirtyRect(minCol, minRow, maxCol, maxRow int) {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return
	}
	if minCol > maxCol || minRow > maxRow {
		return
	}
	if minCol < 0 {
		minCol = 0
	}
	if minRow < 0 {
		minRow = 0
	}
	if maxCol >= l.cols {
		maxCol = l.cols - 1
	}
	if maxRow >= l.rows {
		maxRow = l.rows - 1
	}
	if minCol > maxCol || minRow > maxRow {
		return
	}
	l.extraDirtyBoxes = append(l.extraDirtyBoxes, cellRect{
		minCol: minCol,
		minRow: minRow,
		maxCol: maxCol,
		maxRow: maxRow,
	})
	l.dirty = true
}

// Update incrementally applies pending AddSource, MoveSource, SetSource, RemoveSource, and MarkDirty changes
// to the layer, touching only the dirty cell footprints while matching Rebuild bit-for-bit.
// When g is non-nil, blocked cells (!g.Walkable) receive 0.
func (l *InfluenceLayer) Update(g Grid) {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return
	}
	l.syncBase(g, true)
}

// Rebuild forces a full recomputation of every cell from all active tracked sources,
// skipping blocked cells when g is non-nil. Call Rebuild when terrain walkability changes across the whole Grid.
func (l *InfluenceLayer) Rebuild(g Grid) {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return
	}
	l.dirty = true
	l.fullDirty = true
	l.syncBase(g, true)
}

func (l *InfluenceLayer) syncBase(g Grid, copyToValues bool) {
	if !l.dirty {
		if copyToValues && l.tracked && !l.valuesSynced && len(l.base) == len(l.values) {
			copy(l.values, l.base)
			l.valuesSynced = true
		}
		return
	}
	n := l.cols * l.rows
	if len(l.base) != n {
		l.ensureBaseSize(n)
		l.fullDirty = true
	}

	if l.fullDirty {
		clear(l.base)
		l.dirtySlots = l.dirtySlots[:0]
		l.extraDirtyBoxes = l.extraDirtyBoxes[:0]
		l.dirtyBoxes = l.dirtyBoxes[:0]
		for i := range l.slots {
			s := &l.slots[i]
			s.dirty = false
			if !s.active {
				s.hasStamped = false
				continue
			}
			l.stampIntoBase(g, s.src)
			s.stamped = s.src
			s.hasStamped = true
		}
		if copyToValues {
			copy(l.values, l.base)
			l.valuesSynced = true
		}
		l.dirty = false
		l.fullDirty = false
		return
	}

	// Incremental update: collect old and new bounding boxes of every changed/removed source.
	l.dirtyBoxes = append(l.dirtyBoxes[:0], l.extraDirtyBoxes...)
	l.extraDirtyBoxes = l.extraDirtyBoxes[:0]

	for _, idx := range l.dirtySlots {
		if idx < 0 || idx >= len(l.slots) {
			continue
		}
		s := &l.slots[idx]
		s.dirty = false
		if s.hasStamped {
			if r, ok := l.footprintRect(s.stamped); ok {
				l.dirtyBoxes = append(l.dirtyBoxes, r)
			}
		}
		if s.active {
			if r, ok := l.footprintRect(s.src); ok {
				l.dirtyBoxes = append(l.dirtyBoxes, r)
			}
			s.stamped = s.src
			s.hasStamped = true
		} else {
			s.hasStamped = false
		}
	}
	l.dirtySlots = l.dirtySlots[:0]

	if len(l.dirtyBoxes) == 0 {
		if copyToValues && !l.valuesSynced {
			copy(l.values, l.base)
			l.valuesSynced = true
		}
		l.dirty = false
		return
	}

	if len(l.dirtyGen) < n {
		l.dirtyGen = make([]uint32, n)
		l.curDirtyGen = 0
	} else {
		l.dirtyGen = l.dirtyGen[:n]
	}
	l.curDirtyGen++
	if l.curDirtyGen == 0 {
		clear(l.dirtyGen)
		l.curDirtyGen = 1
	}
	gen := l.curDirtyGen
	l.dirtyCells = l.dirtyCells[:0]

	union := l.dirtyBoxes[0]
	for _, b := range l.dirtyBoxes {
		if b.minCol < union.minCol {
			union.minCol = b.minCol
		}
		if b.minRow < union.minRow {
			union.minRow = b.minRow
		}
		if b.maxCol > union.maxCol {
			union.maxCol = b.maxCol
		}
		if b.maxRow > union.maxRow {
			union.maxRow = b.maxRow
		}
		for row := b.minRow; row <= b.maxRow; row++ {
			rowOff := row * l.cols
			for col := b.minCol; col <= b.maxCol; col++ {
				ci := rowOff + col
				if l.dirtyGen[ci] != gen {
					l.dirtyGen[ci] = gen
					l.base[ci] = 0
					l.dirtyCells = append(l.dirtyCells, ci)
				}
			}
		}
	}

	if len(l.dirtyCells) == 0 {
		if copyToValues && !l.valuesSynced {
			copy(l.values, l.base)
			l.valuesSynced = true
		}
		l.dirty = false
		return
	}

	// Re-stamp only active sources overlapping the dirty region, in fixed slot order
	// so every dirty cell matches Rebuild bit-for-bit with zero floating-point drift.
	checkBoxes := len(l.dirtyBoxes) <= 16
	for i := range l.slots {
		s := &l.slots[i]
		if !s.active {
			continue
		}
		strength := sanitizeFloat32(s.src.Strength)
		if strength == 0 {
			continue
		}
		sr, ok := l.footprintRect(s.src)
		if !ok || !rectsOverlap(sr, union) {
			continue
		}
		if checkBoxes && !overlapsAnyRect(sr, l.dirtyBoxes) {
			continue
		}
		c0 := max(sr.minCol, union.minCol)
		c1 := min(sr.maxCol, union.maxCol)
		r0 := max(sr.minRow, union.minRow)
		r1 := min(sr.maxRow, union.maxRow)
		rad := sanitizeRadius(s.src.Radius)
		for row := r0; row <= r1; row++ {
			rowOff := row * l.cols
			for col := c0; col <= c1; col++ {
				ci := rowOff + col
				if l.dirtyGen[ci] != gen {
					continue
				}
				if g != nil && !g.Walkable(col, row) {
					continue
				}
				f := evalFalloff(col, row, s.src, rad)
				if f != 0 {
					l.base[ci] += strength * f
				}
			}
		}
	}

	if copyToValues {
		if l.valuesSynced {
			for _, ci := range l.dirtyCells {
				l.values[ci] = l.base[ci]
			}
		} else {
			copy(l.values, l.base)
			l.valuesSynced = true
		}
	}
	l.dirty = false
}

func (l *InfluenceLayer) stampIntoBase(g Grid, src InfluenceSource) {
	strength := sanitizeFloat32(src.Strength)
	if strength == 0 {
		return
	}
	rect, ok := l.footprintRect(src)
	if !ok {
		return
	}
	rad := sanitizeRadius(src.Radius)
	for row := rect.minRow; row <= rect.maxRow; row++ {
		rowOff := row * l.cols
		for col := rect.minCol; col <= rect.maxCol; col++ {
			if g != nil && !g.Walkable(col, row) {
				continue
			}
			f := evalFalloff(col, row, src, rad)
			if f != 0 {
				l.base[rowOff+col] += strength * f
			}
		}
	}
}

// Spread propagates influence across walkable cells of g with the given decay and momentum.
//
// When tracked sources are active on the layer, pending source changes are synced incrementally into
// the internal source buffer without overwriting previous values, and each pass propagates from
// walkable neighbors while anchoring on the current sources so moved or removed sources smoothly
// decay according to opts.Momentum. When no tracked sources are active (for example after direct Stamp
// or Set calls), each cell anchors on its own current value so influence spreads outward around obstacles.
// Blocked cells (!g.Walkable) always evaluate to 0 and never transmit influence.
func (l *InfluenceLayer) Spread(g Grid, opts SpreadOptions) {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return
	}
	if l.tracked {
		l.syncBase(g, false)
	}

	iters := opts.Iterations
	if iters <= 0 {
		iters = 1
	}

	decay := float64(opts.Decay)
	if math.IsNaN(decay) || decay >= 1 {
		decay = 1
	} else if decay < 0 {
		decay = 0
	}
	momentum := float64(opts.Momentum)
	if math.IsNaN(momentum) || momentum < 0 {
		momentum = 0
	} else if momentum > 1 {
		momentum = 1
	}

	cardinalFactor := float32(1 - decay)
	var diagFactor float32
	if decay <= 0 {
		diagFactor = 1
	} else if decay < 1 {
		diagFactor = float32(math.Pow(1-decay, math.Sqrt2))
	}
	mom := float32(momentum)
	invMom := float32(1 - momentum)

	n := l.cols * l.rows
	l.ensureScratchSize(n)

	hasGrid := g != nil
	if hasGrid {
		if cap(l.walkable) < n {
			l.walkable = make([]uint8, n)
		} else {
			l.walkable = l.walkable[:n]
		}
		for row := range l.rows {
			rowOff := row * l.cols
			for col := range l.cols {
				if g.Walkable(col, row) {
					l.walkable[rowOff+col] = 1
				} else {
					l.walkable[rowOff+col] = 0
				}
			}
		}
	}

	stepCount := 4
	if opts.Diagonal {
		stepCount = 8
	}
	useBase := l.tracked
	cols, rows := l.cols, l.rows

	for range iters {
		prev := l.values
		next := l.scratch
		for row := range rows {
			rowOff := row * cols
			for col := range cols {
				idx := rowOff + col
				if hasGrid && l.walkable[idx] == 0 {
					next[idx] = 0
					continue
				}
				var anchor float32
				if useBase {
					anchor = l.base[idx]
				} else {
					anchor = prev[idx]
				}
				var bestPos, bestNeg float32
				if anchor > 0 {
					bestPos = anchor
				} else if anchor < 0 {
					bestNeg = anchor
				}
				if cardinalFactor > 0 {
					for si := range stepCount {
						st := influenceSteps[si]
						nc, nr := col+st.dc, row+st.dr
						if nc < 0 || nc >= cols || nr < 0 || nr >= rows {
							continue
						}
						nidx := nr*cols + nc
						if hasGrid {
							if l.walkable[nidx] == 0 {
								continue
							}
							if st.diag && (l.walkable[rowOff+nc] == 0 || l.walkable[nr*cols+col] == 0) {
								continue
							}
						}
						nv := prev[nidx]
						if nv == 0 {
							continue
						}
						factor := cardinalFactor
						if st.diag {
							factor = diagFactor
						}
						cand := nv * factor
						if cand > bestPos {
							bestPos = cand
						} else if cand < bestNeg {
							bestNeg = cand
						}
					}
				}
				target := bestPos + bestNeg
				if mom == 0 {
					next[idx] = target
				} else if mom >= 1 {
					next[idx] = prev[idx]
				} else {
					next[idx] = prev[idx]*mom + target*invMom
				}
			}
		}
		l.values, l.scratch = next, prev
	}
	l.valuesSynced = false
}

// Add sets each cell of l to a + b (useful for tension maps combining friendly and enemy presence).
// Aliasing l with a or b is safe, and reusing a pre-sized l performs zero heap allocations.
func (l *InfluenceLayer) Add(a, b *InfluenceLayer) {
	l.combineBinary(a, b, 0)
}

// Subtract sets each cell of l to a - b (useful for control maps: friendly - enemy).
// Aliasing l with a or b is safe, and reusing a pre-sized l performs zero heap allocations.
func (l *InfluenceLayer) Subtract(a, b *InfluenceLayer) {
	l.combineBinary(a, b, 1)
}

// Multiply sets each cell of l to a * b (useful for terrain masking or contested overlap maps).
// Aliasing l with a or b is safe, and reusing a pre-sized l performs zero heap allocations.
func (l *InfluenceLayer) Multiply(a, b *InfluenceLayer) {
	l.combineBinary(a, b, 2)
}

func (l *InfluenceLayer) combineBinary(a, b *InfluenceLayer, op uint8) {
	if l == nil || a == nil || b == nil {
		return
	}
	cols, rows := a.cols, a.rows
	if cols == 0 || rows == 0 {
		l.ensureValuesSize(0, 0)
		l.tracked = false
		l.valuesSynced = false
		return
	}
	// Fast path when a and b share identical dimensions.
	if a.cols == b.cols && a.rows == b.rows {
		l.ensureValuesSize(cols, rows)
		av, bv, dv := a.values, b.values, l.values
		switch op {
		case 0:
			for i := range dv {
				dv[i] = av[i] + bv[i]
			}
		case 1:
			for i := range dv {
				dv[i] = av[i] - bv[i]
			}
		case 2:
			for i := range dv {
				dv[i] = av[i] * bv[i]
			}
		}
		l.tracked = false
		l.valuesSynced = false
		return
	}

	// Mismatched dimensions: size to a and treat out-of-bounds cells of b as 0.
	n := cols * rows
	useScratch := l == b
	var dst []float32
	if useScratch {
		l.ensureScratchSize(n)
		dst = l.scratch
	} else {
		l.ensureValuesSize(cols, rows)
		dst = l.values
	}
	for row := range rows {
		rowOff := row * cols
		for col := range cols {
			va := a.values[rowOff+col]
			vb := b.At(col, row)
			switch op {
			case 0:
				dst[rowOff+col] = va + vb
			case 1:
				dst[rowOff+col] = va - vb
			case 2:
				dst[rowOff+col] = va * vb
			}
		}
	}
	if useScratch {
		l.cols, l.rows = cols, rows
		l.values, l.scratch = l.scratch, l.values
	}
	l.tracked = false
	l.valuesSynced = false
}

// Combine sets l to the weighted sum of terms (sum of term.Layer * term.Weight).
// Dimensions follow the first non-nil layer in terms. Aliasing l inside terms is safe,
// and reusing a pre-sized l performs zero heap allocations.
func (l *InfluenceLayer) Combine(terms ...WeightedLayer) {
	if l == nil {
		return
	}
	cols, rows := 0, 0
	found := false
	aliased := false
	for _, t := range terms {
		if t.Layer == nil {
			continue
		}
		if !found {
			cols, rows = t.Layer.cols, t.Layer.rows
			found = true
		}
		if t.Layer == l {
			aliased = true
		}
	}
	if !found || cols == 0 || rows == 0 {
		if l.cols != 0 || l.rows != 0 {
			clear(l.values)
		}
		l.tracked = false
		l.valuesSynced = false
		return
	}
	n := cols * rows
	var dst []float32
	if aliased {
		l.ensureScratchSize(n)
		dst = l.scratch
		clear(dst)
	} else {
		l.ensureValuesSize(cols, rows)
		dst = l.values
		clear(dst)
	}

	for _, t := range terms {
		if t.Layer == nil {
			continue
		}
		w := sanitizeFloat32(t.Weight)
		if w == 0 {
			continue
		}
		src := t.Layer
		if src.cols == cols && src.rows == rows {
			sv := src.values
			for i := range dst {
				dst[i] += sv[i] * w
			}
		} else {
			maxR := min(rows, src.rows)
			maxC := min(cols, src.cols)
			for row := range maxR {
				dstOff := row * cols
				srcOff := row * src.cols
				for col := range maxC {
					dst[dstOff+col] += src.values[srcOff+col] * w
				}
			}
		}
	}

	if aliased {
		l.cols, l.rows = cols, rows
		l.values, l.scratch = l.scratch, l.values
	}
	l.tracked = false
	l.valuesSynced = false
}

// AddScaled adds src * weight into l in place without allocating.
// If l and src differ in size, only the overlapping top-left region is updated.
func (l *InfluenceLayer) AddScaled(src *InfluenceLayer, weight float32) {
	if l == nil || src == nil || l.cols == 0 || l.rows == 0 || src.cols == 0 || src.rows == 0 {
		return
	}
	w := sanitizeFloat32(weight)
	if w == 0 {
		return
	}
	if l.cols == src.cols && l.rows == src.rows {
		dv, sv := l.values, src.values
		for i := range dv {
			dv[i] += sv[i] * w
		}
	} else {
		maxR := min(l.rows, src.rows)
		maxC := min(l.cols, src.cols)
		for row := range maxR {
			dstOff := row * l.cols
			srcOff := row * src.cols
			for col := range maxC {
				l.values[dstOff+col] += src.values[srcOff+col] * w
			}
		}
	}
	l.tracked = false
	l.valuesSynced = false
}

// Scale multiplies every cell of l by s in place (non-finite values are treated as 0).
func (l *InfluenceLayer) Scale(s float32) {
	if l == nil {
		return
	}
	s = sanitizeFloat32(s)
	for i := range l.values {
		l.values[i] *= s
	}
	l.tracked = false
	l.valuesSynced = false
}

// Abs replaces every cell value in l with its absolute value in place.
func (l *InfluenceLayer) Abs() {
	if l == nil {
		return
	}
	for i, v := range l.values {
		if v < 0 {
			l.values[i] = -v
		}
	}
	l.tracked = false
	l.valuesSynced = false
}

// Clamp clamps every cell of l into [minVal, maxVal] in place.
func (l *InfluenceLayer) Clamp(minVal, maxVal float32) {
	if l == nil || minVal > maxVal {
		return
	}
	for i, v := range l.values {
		if v < minVal {
			l.values[i] = minVal
		} else if v > maxVal {
			l.values[i] = maxVal
		}
	}
	l.tracked = false
	l.valuesSynced = false
}

// Normalize divides every cell of l by the maximum absolute value across the layer,
// mapping non-negative layers to [0, 1] and signed layers to [-1, 1] while preserving 0 as neutral.
// It is a no-op if every cell is 0.
func (l *InfluenceLayer) Normalize() {
	if l == nil || len(l.values) == 0 {
		return
	}
	var maxAbs float32
	for _, v := range l.values {
		if v < 0 {
			v = -v
		}
		if v > maxAbs {
			maxAbs = v
		}
	}
	if !(maxAbs > 0) || math.IsInf(float64(maxAbs), 0) {
		return
	}
	inv := 1 / maxAbs
	for i := range l.values {
		l.values[i] *= inv
	}
	l.tracked = false
	l.valuesSynced = false
}

// Sample returns the bilinearly interpolated influence value at world position p and whether p is inside the grid.
// At a cell's center (CellCenter(c)), Sample returns At(c.Col, c.Row) exactly.
func (l *InfluenceLayer) Sample(p math2d.Vec2) (float32, bool) {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return 0, false
	}
	size := float64(l.cellSize())
	ux := (float64(p.X) - float64(l.Origin.X)) / size
	uy := (float64(p.Y) - float64(l.Origin.Y)) / size
	if math.IsNaN(ux) || math.IsNaN(uy) || ux < 0 || uy < 0 || ux >= float64(l.cols) || uy >= float64(l.rows) {
		return 0, false
	}
	return l.sampleGridClamped(ux-0.5, uy-0.5), true
}

func (l *InfluenceLayer) sampleGridClamped(gx, gy float64) float32 {
	maxC := float64(l.cols - 1)
	maxR := float64(l.rows - 1)
	if gx < 0 {
		gx = 0
	} else if gx > maxC {
		gx = maxC
	}
	if gy < 0 {
		gy = 0
	} else if gy > maxR {
		gy = maxR
	}
	x0 := int(math.Floor(gx))
	y0 := int(math.Floor(gy))
	x1 := min(x0+1, l.cols-1)
	y1 := min(y0+1, l.rows-1)
	tx := gx - float64(x0)
	ty := gy - float64(y0)

	v00 := float64(l.values[y0*l.cols+x0])
	v10 := float64(l.values[y0*l.cols+x1])
	v01 := float64(l.values[y1*l.cols+x0])
	v11 := float64(l.values[y1*l.cols+x1])

	top := v00 + (v10-v00)*tx
	bot := v01 + (v11-v01)*tx
	return float32(top + (bot-top)*ty)
}

// GradientAtCell returns the 2D gradient vector (d/dx, d/dy in influence per world unit) at (col, row),
// pointing toward higher influence. Boundary cells use one-sided differences; out-of-bounds cells return zero.
func (l *InfluenceLayer) GradientAtCell(col, row int) math2d.Vec2 {
	if l == nil || !l.inBounds(col, row) {
		return math2d.Vec2{}
	}
	size := float64(l.cellSize())
	var dx, dy float64
	if l.cols > 1 {
		lc := max(col-1, 0)
		rc := min(col+1, l.cols-1)
		dx = (float64(l.values[row*l.cols+rc]) - float64(l.values[row*l.cols+lc])) / (float64(rc-lc) * size)
	}
	if l.rows > 1 {
		ur := max(row-1, 0)
		dr := min(row+1, l.rows-1)
		dy = (float64(l.values[dr*l.cols+col]) - float64(l.values[ur*l.cols+col])) / (float64(dr-ur) * size)
	}
	return math2d.Vec2{X: float32(dx), Y: float32(dy)}
}

// Gradient returns the smooth 2D gradient vector (d/dx, d/dy in influence per world unit) at world position p,
// pointing uphill toward higher influence. At CellCenter(c), Gradient agrees with GradientAtCell(c.Col, c.Row).
// Points outside the grid return the zero vector.
func (l *InfluenceLayer) Gradient(p math2d.Vec2) math2d.Vec2 {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return math2d.Vec2{}
	}
	size := float64(l.cellSize())
	ux := (float64(p.X) - float64(l.Origin.X)) / size
	uy := (float64(p.Y) - float64(l.Origin.Y)) / size
	if math.IsNaN(ux) || math.IsNaN(uy) || ux < 0 || uy < 0 || ux >= float64(l.cols) || uy >= float64(l.rows) {
		return math2d.Vec2{}
	}
	gx := ux - 0.5
	gy := uy - 0.5
	maxC := float64(l.cols - 1)
	maxR := float64(l.rows - 1)

	var dx, dy float64
	if l.cols > 1 {
		lx := max(gx-0.5, 0)
		rx := min(gx+0.5, maxC)
		if span := rx - lx; span > 0 {
			vl := float64(l.sampleGridClamped(lx, gy))
			vr := float64(l.sampleGridClamped(rx, gy))
			dx = (vr - vl) / (span * size)
		}
	}
	if l.rows > 1 {
		uy0 := max(gy-0.5, 0)
		dy1 := min(gy+0.5, maxR)
		if span := dy1 - uy0; span > 0 {
			vu := float64(l.sampleGridClamped(gx, uy0))
			vd := float64(l.sampleGridClamped(gx, dy1))
			dy = (vd - vu) / (span * size)
		}
	}
	return math2d.Vec2{X: float32(dx), Y: float32(dy)}
}

// HighestInRadius finds the candidate cell with the highest value within Euclidean cell distance radius of center.
// Ties break deterministically by closer distance to center, then smaller Row, then smaller Col.
func (l *InfluenceLayer) HighestInRadius(center Cell, radius float32, q InfluenceQuery) (Cell, float32, bool) {
	return l.extremumInRadius(center, radius, q, true)
}

// LowestInRadius finds the candidate cell with the lowest value within Euclidean cell distance radius of center.
// Ties break deterministically by closer distance to center, then smaller Row, then smaller Col.
func (l *InfluenceLayer) LowestInRadius(center Cell, radius float32, q InfluenceQuery) (Cell, float32, bool) {
	return l.extremumInRadius(center, radius, q, false)
}

// Highest finds the highest-valued cell across the entire layer (restricted to walkable cells when g is non-nil).
// Ties break deterministically by smaller Row, then smaller Col.
func (l *InfluenceLayer) Highest(g Grid) (Cell, float32, bool) {
	return l.extremumGlobal(g, true)
}

// Lowest finds the lowest-valued cell across the entire layer (restricted to walkable cells when g is non-nil).
// Ties break deterministically by smaller Row, then smaller Col.
func (l *InfluenceLayer) Lowest(g Grid) (Cell, float32, bool) {
	return l.extremumGlobal(g, false)
}

func (l *InfluenceLayer) extremumGlobal(g Grid, wantHigh bool) (Cell, float32, bool) {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return Cell{}, 0, false
	}
	var bestCell Cell
	var bestVal float32
	found := false
	for row := range l.rows {
		rowOff := row * l.cols
		for col := range l.cols {
			if g != nil && !g.Walkable(col, row) {
				continue
			}
			v := l.values[rowOff+col]
			if !found || (wantHigh && v > bestVal) || (!wantHigh && v < bestVal) {
				bestCell = Cell{Col: col, Row: row}
				bestVal = v
				found = true
			}
		}
	}
	return bestCell, bestVal, found
}

func (l *InfluenceLayer) extremumInRadius(center Cell, radius float32, q InfluenceQuery, wantHigh bool) (Cell, float32, bool) {
	if l == nil || l.cols == 0 || l.rows == 0 {
		return Cell{}, 0, false
	}
	rad := float64(radius)
	if math.IsNaN(rad) || rad < 0 {
		rad = 0
	}
	infRad := math.IsInf(rad, 1)
	r2 := rad * rad

	var bestCell Cell
	var bestVal float32
	bestDistSq := 0
	found := false

	consider := func(col, row int) {
		if q.ExcludeCenter && col == center.Col && row == center.Row {
			return
		}
		v := l.values[row*l.cols+col]
		dc := col - center.Col
		dr := row - center.Row
		d2 := dc*dc + dr*dr
		if !found {
			bestCell = Cell{Col: col, Row: row}
			bestVal = v
			bestDistSq = d2
			found = true
			return
		}
		better := false
		if wantHigh {
			if v > bestVal {
				better = true
			} else if v == bestVal {
				better = tieBreakBetter(d2, row, col, bestDistSq, bestCell)
			}
		} else {
			if v < bestVal {
				better = true
			} else if v == bestVal {
				better = tieBreakBetter(d2, row, col, bestDistSq, bestCell)
			}
		}
		if better {
			bestCell = Cell{Col: col, Row: row}
			bestVal = v
			bestDistSq = d2
		}
	}

	if q.Reachable && q.Grid != nil {
		if !l.inBounds(center.Col, center.Row) || !q.Grid.Walkable(center.Col, center.Row) {
			return Cell{}, 0, false
		}
		n := l.cols * l.rows
		if cap(l.bfsGen) < n {
			l.bfsGen = make([]uint32, n)
			l.bfsCurGen = 0
		} else {
			l.bfsGen = l.bfsGen[:n]
		}
		l.bfsCurGen++
		if l.bfsCurGen == 0 {
			clear(l.bfsGen)
			l.bfsCurGen = 1
		}
		gen := l.bfsCurGen
		startID := center.Row*l.cols + center.Col
		l.bfsGen[startID] = gen
		l.bfsQueue = append(l.bfsQueue[:0], startID)

		stepCount := 4
		if q.Diagonal {
			stepCount = 8
		}
		for head := 0; head < len(l.bfsQueue); head++ {
			id := l.bfsQueue[head]
			c, r := id%l.cols, id/l.cols
			consider(c, r)
			for si := range stepCount {
				st := influenceSteps[si]
				nc, nr := c+st.dc, r+st.dr
				if nc < 0 || nc >= l.cols || nr < 0 || nr >= l.rows {
					continue
				}
				nid := nr*l.cols + nc
				if l.bfsGen[nid] == gen {
					continue
				}
				if !infRad {
					dc := float64(nc - center.Col)
					dr := float64(nr - center.Row)
					if dc*dc+dr*dr > r2 {
						continue
					}
				}
				if !q.Grid.Walkable(nc, nr) {
					continue
				}
				if st.diag && (!q.Grid.Walkable(c+st.dc, r) || !q.Grid.Walkable(c, r+st.dr)) {
					continue
				}
				l.bfsGen[nid] = gen
				l.bfsQueue = append(l.bfsQueue, nid)
			}
		}
		return bestCell, bestVal, found
	}

	minCol, maxCol := 0, l.cols-1
	minRow, maxRow := 0, l.rows-1
	if !infRad {
		ri := int(math.Floor(rad))
		minCol = max(0, center.Col-ri)
		maxCol = min(l.cols-1, center.Col+ri)
		minRow = max(0, center.Row-ri)
		maxRow = min(l.rows-1, center.Row+ri)
	}
	if minCol > maxCol || minRow > maxRow {
		return Cell{}, 0, false
	}
	for row := minRow; row <= maxRow; row++ {
		dr := float64(row - center.Row)
		dr2 := dr * dr
		for col := minCol; col <= maxCol; col++ {
			if !infRad {
				dc := float64(col - center.Col)
				if dc*dc+dr2 > r2 {
					continue
				}
			}
			if q.Grid != nil && !q.Grid.Walkable(col, row) {
				continue
			}
			consider(col, row)
		}
	}
	return bestCell, bestVal, found
}

func tieBreakBetter(d2, row, col, bestD2 int, best Cell) bool {
	if d2 != bestD2 {
		return d2 < bestD2
	}
	if row != best.Row {
		return row < best.Row
	}
	return col < best.Col
}

func (l *InfluenceLayer) ensureValuesSize(cols, rows int) {
	if cols < 0 {
		cols = 0
	}
	if rows < 0 {
		rows = 0
	}
	l.cols, l.rows = cols, rows
	n := cols * rows
	if cap(l.values) < n {
		l.values = make([]float32, n)
	} else {
		l.values = l.values[:n]
	}
}

func (l *InfluenceLayer) ensureBaseSize(n int) {
	if cap(l.base) < n {
		l.base = make([]float32, n)
	} else {
		l.base = l.base[:n]
	}
}

func (l *InfluenceLayer) ensureScratchSize(n int) {
	if cap(l.scratch) < n {
		l.scratch = make([]float32, n)
	} else {
		l.scratch = l.scratch[:n]
	}
}

func makeSourceID(gen, idx uint32) InfluenceSourceID {
	return InfluenceSourceID(uint64(gen)<<32 | uint64(idx))
}

func (l *InfluenceLayer) lookupSlot(id InfluenceSourceID) (int, bool) {
	if l == nil || id == 0 {
		return 0, false
	}
	gen := uint32(id >> 32)
	idx := int(uint32(id))
	if gen == 0 || idx < 0 || idx >= len(l.slots) {
		return 0, false
	}
	s := &l.slots[idx]
	if !s.active || s.gen != gen {
		return 0, false
	}
	return idx, true
}

func (l *InfluenceLayer) footprintRect(src InfluenceSource) (cellRect, bool) {
	if l.cols == 0 || l.rows == 0 {
		return cellRect{}, false
	}
	rad := sanitizeRadius(src.Radius)
	ri := int(math.Floor(float64(rad)))
	minCol := max(0, src.Cell.Col-ri)
	maxCol := min(l.cols-1, src.Cell.Col+ri)
	minRow := max(0, src.Cell.Row-ri)
	maxRow := min(l.rows-1, src.Cell.Row+ri)
	if minCol > maxCol || minRow > maxRow {
		return cellRect{}, false
	}
	return cellRect{
		minCol: minCol,
		minRow: minRow,
		maxCol: maxCol,
		maxRow: maxRow,
	}, true
}

func rectsOverlap(a, b cellRect) bool {
	return a.minCol <= b.maxCol && a.maxCol >= b.minCol && a.minRow <= b.maxRow && a.maxRow >= b.minRow
}

func overlapsAnyRect(r cellRect, boxes []cellRect) bool {
	for _, b := range boxes {
		if rectsOverlap(r, b) {
			return true
		}
	}
	return false
}

func sanitizeFloat32(v float32) float32 {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return 0
	}
	return v
}

func sanitizeRadius(r float32) float32 {
	if !(r > 0) || math.IsInf(float64(r), 0) {
		return 0
	}
	return r
}

func evalFalloff(col, row int, src InfluenceSource, radius float32) float32 {
	dc := col - src.Cell.Col
	dr := row - src.Cell.Row
	if radius <= 0 {
		if dc == 0 && dr == 0 {
			if src.Falloff == FalloffCustom && src.CustomFalloff != nil {
				return sanitizeFloat32(src.CustomFalloff(0, 0))
			}
			return 1
		}
		return 0
	}
	d2 := float64(dc*dc + dr*dr)
	r := float64(radius)
	if d2 > r*r {
		return 0
	}
	dist := math.Sqrt(d2)
	switch src.Falloff {
	case FalloffExponential:
		if dist == 0 {
			return 1
		}
		rate := float64(src.FalloffRate)
		if !(rate > 0) || math.IsInf(rate, 0) {
			rate = 1
		}
		return float32(math.Exp(-rate * dist))
	case FalloffCustom:
		if src.CustomFalloff != nil {
			return sanitizeFloat32(src.CustomFalloff(float32(dist), radius))
		}
		// Nil CustomFalloff falls back to linear.
		fallthrough
	default:
		if dist == 0 {
			return 1
		}
		f := 1 - dist/r
		if f <= 0 {
			return 0
		}
		return float32(f)
	}
}
