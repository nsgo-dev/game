package editor

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// TileRect is an inclusive cell rectangle on an inline tilemap, with Col0 <= Col1 and Row0 <= Row1.
type TileRect struct {
	Col0, Row0 int
	Col1, Row1 int
}

// Width returns the number of columns in r (0 when invalid).
func (r TileRect) Width() int {
	if r.Col1 < r.Col0 {
		return 0
	}
	return r.Col1 - r.Col0 + 1
}

// Height returns the number of rows in r (0 when invalid).
func (r TileRect) Height() int {
	if r.Row1 < r.Row0 {
		return 0
	}
	return r.Row1 - r.Row0 + 1
}

func normalizeTileRect(col0, row0, col1, row1 int) TileRect {
	if col0 > col1 {
		col0, col1 = col1, col0
	}
	if row0 > row1 {
		row0, row1 = row1, row0
	}
	return TileRect{Col0: col0, Row0: row0, Col1: col1, Row1: row1}
}

// TileStamp is a rectangular pattern of tiles painted by the Stamp brush or held by the tile region clipboard.
// Tiles is row-major of length Width*Height: 0 is an empty cell, and a positive integer is a tile ID (as PaintTile takes it).
// Flips, when non-empty (length Width*Height), carries per-cell flip bits (tilemap.FlipHorizontal, FlipVertical, FlipDiagonal) for layered tilemaps.
type TileStamp struct {
	Width  int
	Height int
	Tiles  []int
	Flips  []uint32
}

// Empty reports whether s holds no cells.
func (s TileStamp) Empty() bool {
	return s.Width <= 0 || s.Height <= 0 || len(s.Tiles) == 0 || len(s.Tiles)%s.Width != 0 || len(s.Tiles)/s.Width != s.Height
}

// Clone returns an independent copy of s.
func (s TileStamp) Clone() TileStamp {
	if s.Empty() {
		return TileStamp{}
	}
	return TileStamp{
		Width:  s.Width,
		Height: s.Height,
		Tiles:  slices.Clone(s.Tiles),
		Flips:  slices.Clone(s.Flips),
	}
}

// Cell returns the tile ID and flip bits at (col, row) within s, or (0, 0) out of bounds.
func (s TileStamp) Cell(col, row int) (int, uint32) {
	if s.Empty() || col < 0 || col >= s.Width || row < 0 || row >= s.Height {
		return 0, 0
	}
	idx := row*s.Width + col
	var flip uint32
	if len(s.Flips) == len(s.Tiles) {
		flip = s.Flips[idx] & tilemap.FlipMask
	}
	return s.Tiles[idx], flip
}

func validateTileStamp(s TileStamp) error {
	if s.Width <= 0 || s.Height <= 0 {
		return fmt.Errorf("editor: tile stamp dimensions %dx%d must be positive", s.Width, s.Height)
	}
	if len(s.Tiles) == 0 || len(s.Tiles)%s.Width != 0 || len(s.Tiles)/s.Width != s.Height {
		return fmt.Errorf("editor: tile stamp has %d cells for %dx%d dimensions", len(s.Tiles), s.Width, s.Height)
	}
	if len(s.Flips) != 0 && len(s.Flips) != len(s.Tiles) {
		return fmt.Errorf("editor: tile stamp has %d flips, want 0 or %d", len(s.Flips), len(s.Tiles))
	}
	for i, id := range s.Tiles {
		if id < 0 {
			return fmt.Errorf("editor: tile stamp cell %d has negative tile ID %d", i, id)
		}
	}
	return nil
}

// WeightedTile is one weighted entry in the Random brush's tile pool.
type WeightedTile struct {
	TileID int
	Weight float64
}

// TilePreviewCell is one cell in an in-progress brush stroke's live preview before release.
type TilePreviewCell struct {
	Col   int
	Row   int
	Tile  int
	Flip  uint32
	Erase bool
}

// SetTileStamp sets the active stamp pattern painted by ToolStampTile.
func (e *Editor) SetTileStamp(stamp TileStamp) error {
	if e == nil {
		return nil
	}
	if err := validateTileStamp(stamp); err != nil {
		return err
	}
	e.activeStamp = stamp.Clone()
	e.Status = fmt.Sprintf("Stamp: %dx%d", stamp.Width, stamp.Height)
	e.notify()
	return nil
}

// TileStamp returns a copy of the active stamp pattern, and whether one is set.
func (e *Editor) TileStamp() (TileStamp, bool) {
	if e == nil || e.activeStamp.Empty() {
		return TileStamp{}, false
	}
	return e.activeStamp.Clone(), true
}

// ClearTileStamp clears the active stamp pattern.
func (e *Editor) ClearTileStamp() {
	if e == nil || e.activeStamp.Empty() {
		return
	}
	e.activeStamp = TileStamp{}
	e.paletteStampAnchor = 0
	e.paletteStampRect = TileRect{}
	e.hasPaletteStampRect = false
	e.notify()
}

// effectiveStamp returns the active stamp when set, or a 1x1 stamp of ActiveTileID when valid.
func (e *Editor) effectiveStamp() (TileStamp, bool) {
	if !e.activeStamp.Empty() {
		return e.activeStamp, true
	}
	if e.ActiveTileID > 0 {
		return TileStamp{Width: 1, Height: 1, Tiles: []int{e.ActiveTileID}}, true
	}
	return TileStamp{}, false
}

// TileClipboard returns a copy of the tile region clipboard (set by CopyTileRegion or CutTileRegion), and whether it is non-empty.
func (e *Editor) TileClipboard() (TileStamp, bool) {
	if e == nil || e.tileClipboard.Empty() {
		return TileStamp{}, false
	}
	return e.tileClipboard.Clone(), true
}

// SetTileSelection selects the inclusive cell rectangle on the active layer's inline tilemap for Copy/Cut.
func (e *Editor) SetTileSelection(col0, row0, col1, row1 int) bool {
	if e == nil {
		return false
	}
	_, tm, err := e.resolveInlineTilemap(e.ActiveLayer)
	if err != nil {
		return false
	}
	r, ok := clampTileRect(normalizeTileRect(col0, row0, col1, row1), tm.Width, tm.Height)
	if !ok {
		return false
	}
	e.tileSel = r
	e.hasTileSel = true
	e.Status = fmt.Sprintf("Selected %dx%d tile region", r.Width(), r.Height())
	e.notify()
	return true
}

// TileSelection returns the currently selected tile region on the active layer, and whether one is active.
func (e *Editor) TileSelection() (TileRect, bool) {
	if e == nil || !e.hasTileSel {
		return TileRect{}, false
	}
	return e.tileSel, true
}

// ClearTileSelection clears the active map tile region selection.
func (e *Editor) ClearTileSelection() {
	if e == nil || !e.hasTileSel {
		return
	}
	e.tileSel = TileRect{}
	e.hasTileSel = false
	e.notify()
}

func (e *Editor) clearTileBrushTransient() {
	if e == nil {
		return
	}
	e.tileSel = TileRect{}
	e.hasTileSel = false
	e.paletteStampAnchor = 0
	e.paletteStampRect = TileRect{}
	e.hasPaletteStampRect = false
}

// SelectPaletteStamp selects an inclusive rectangle of tiles from the active layer's tile palette grid
// (using TilePalette().Columns columns per row), sets it as the active stamp pattern, and returns it.
func (e *Editor) SelectPaletteStamp(col0, row0, col1, row1 int) (TileStamp, error) {
	if e == nil {
		return TileStamp{}, fmt.Errorf("editor: nil editor")
	}
	p := e.TilePalette()
	if len(p.Tiles) == 0 || p.Columns <= 0 {
		return TileStamp{}, fmt.Errorf("editor: active layer has no tile palette")
	}
	rows := (len(p.Tiles) + p.Columns - 1) / p.Columns
	r, ok := clampTileRect(normalizeTileRect(col0, row0, col1, row1), p.Columns, rows)
	if !ok {
		return TileStamp{}, fmt.Errorf("editor: palette rectangle (%d,%d)-(%d,%d) is out of bounds (%dx%d)", col0, row0, col1, row1, p.Columns, rows)
	}
	w, h := r.Width(), r.Height()
	tiles := make([]int, w*h)
	hasAny := false
	for dy := range h {
		for dx := range w {
			idx := (r.Row0+dy)*p.Columns + (r.Col0 + dx)
			if idx >= 0 && idx < len(p.Tiles) {
				tiles[dy*w+dx] = p.Tiles[idx].ID
				hasAny = true
			}
		}
	}
	if !hasAny {
		return TileStamp{}, fmt.Errorf("editor: palette selection contains no tiles")
	}
	stamp := TileStamp{Width: w, Height: h, Tiles: tiles}
	e.activeStamp = stamp.Clone()
	e.paletteStampRect = r
	e.hasPaletteStampRect = true
	if anchorIdx := r.Row0*p.Columns + r.Col0; anchorIdx < len(p.Tiles) {
		e.paletteStampAnchor = p.Tiles[anchorIdx].ID
	}
	e.Status = fmt.Sprintf("Selected %dx%d palette stamp", w, h)
	e.notify()
	return stamp, nil
}

// paletteTileCoord returns the (col, row) grid position of tileID in p.
func paletteTileCoord(p TilePalette, tileID int) (col, row int, ok bool) {
	if p.Columns <= 0 {
		return 0, 0, false
	}
	i := slices.IndexFunc(p.Tiles, func(t PaletteTile) bool { return t.ID == tileID })
	if i < 0 {
		return 0, 0, false
	}
	return i % p.Columns, i / p.Columns, true
}

// SelectPaletteStampRange selects the rectangle in the active tile palette spanning fromTileID to toTileID
// as the active stamp pattern.
func (e *Editor) SelectPaletteStampRange(fromTileID, toTileID int) (TileStamp, error) {
	if e == nil {
		return TileStamp{}, fmt.Errorf("editor: nil editor")
	}
	p := e.TilePalette()
	c0, r0, ok0 := paletteTileCoord(p, fromTileID)
	c1, r1, ok1 := paletteTileCoord(p, toTileID)
	if !ok0 || !ok1 {
		return TileStamp{}, fmt.Errorf("editor: tile ID not in active palette")
	}
	return e.SelectPaletteStamp(c0, r0, c1, r1)
}

// RandomBrushSeed returns the fixed seed used by ToolRandomTile.
func (e *Editor) RandomBrushSeed() uint64 {
	if e == nil {
		return 0
	}
	return e.randomSeed
}

// SetRandomBrushSeed sets the deterministic seed used by ToolRandomTile.
func (e *Editor) SetRandomBrushSeed(seed uint64) {
	if e == nil || e.randomSeed == seed {
		return
	}
	e.randomSeed = seed
	e.notify()
}

// RandomBrushTiles returns a copy of the weighted tiles used by ToolRandomTile.
func (e *Editor) RandomBrushTiles() []WeightedTile {
	if e == nil || len(e.randomTiles) == 0 {
		return nil
	}
	return slices.Clone(e.randomTiles)
}

// SetRandomBrushTiles configures the weighted tile pool for ToolRandomTile.
// Each entry must have a positive TileID and a positive finite Weight; duplicate tile IDs and overflowing weight sums are refused.
func (e *Editor) SetRandomBrushTiles(entries []WeightedTile) error {
	if e == nil {
		return nil
	}
	seen := make(map[int]bool, len(entries))
	clean := make([]WeightedTile, 0, len(entries))
	total := 0.0
	for _, entry := range entries {
		if entry.TileID <= 0 {
			return fmt.Errorf("editor: random brush tile ID %d must be positive", entry.TileID)
		}
		if math.IsNaN(entry.Weight) || math.IsInf(entry.Weight, 0) || entry.Weight <= 0 {
			return fmt.Errorf("editor: random brush weight %v for tile %d must be positive and finite", entry.Weight, entry.TileID)
		}
		if seen[entry.TileID] {
			return fmt.Errorf("editor: duplicate tile ID %d in random brush pool", entry.TileID)
		}
		total += entry.Weight
		if math.IsInf(total, 0) || math.IsNaN(total) {
			return fmt.Errorf("editor: random brush total weight overflows float64")
		}
		seen[entry.TileID] = true
		clean = append(clean, entry)
	}
	e.randomTiles = clean
	e.notify()
	return nil
}

// SetRandomBrushTileWeight adds tileID to the random brush pool with weight, or updates its weight when already present.
func (e *Editor) SetRandomBrushTileWeight(tileID int, weight float64) error {
	if e == nil {
		return nil
	}
	if tileID <= 0 {
		return fmt.Errorf("editor: random brush tile ID %d must be positive", tileID)
	}
	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
		return fmt.Errorf("editor: random brush weight %v for tile %d must be positive and finite", weight, tileID)
	}
	total := weight
	idx := -1
	for i := range e.randomTiles {
		if e.randomTiles[i].TileID == tileID {
			idx = i
			continue
		}
		total += e.randomTiles[i].Weight
	}
	if math.IsInf(total, 0) || math.IsNaN(total) {
		return fmt.Errorf("editor: random brush total weight overflows float64")
	}
	if idx >= 0 {
		e.randomTiles[idx].Weight = weight
		e.notify()
		return nil
	}
	e.randomTiles = append(e.randomTiles, WeightedTile{TileID: tileID, Weight: weight})
	e.notify()
	return nil
}

// ToggleRandomBrushTile adds tileID (with weight 1) to the random brush pool if absent, or removes it if present.
func (e *Editor) ToggleRandomBrushTile(tileID int) {
	if e == nil || tileID <= 0 {
		return
	}
	for i, entry := range e.randomTiles {
		if entry.TileID == tileID {
			e.randomTiles = slices.Delete(e.randomTiles, i, i+1)
			e.notify()
			return
		}
	}
	e.randomTiles = append(e.randomTiles, WeightedTile{TileID: tileID, Weight: 1})
	e.notify()
}

// effectiveRandomPool returns the random brush pool, falling back to ActiveTileID (weight 1) when empty.
func (e *Editor) effectiveRandomPool() []WeightedTile {
	if len(e.randomTiles) > 0 {
		return e.randomTiles
	}
	if e.ActiveTileID > 0 {
		return []WeightedTile{{TileID: e.ActiveTileID, Weight: 1}}
	}
	return nil
}

// splitmix64 is a deterministic 64-bit mixing step used to hash cell coordinates with a seed.
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// randomPickTile deterministically picks a tile ID from pool for (seed, col, row) using weighted selection.
func randomPickTile(seed uint64, col, row int, pool []WeightedTile) int {
	if len(pool) == 0 {
		return 0
	}
	if len(pool) == 1 {
		return pool[0].TileID
	}
	total := 0.0
	for _, entry := range pool {
		total += entry.Weight
	}
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return pool[0].TileID
	}
	h := splitmix64(seed ^ 0x243f6a8885a308d3)
	h = splitmix64(h ^ uint64(int64(col))*0x9e3779b97f4a7c15)
	h = splitmix64(h ^ uint64(int64(row))*0xbf58476d1ce4e5b9)
	u := float64(h>>11) / (1 << 53)
	target := u * total
	acc := 0.0
	for _, entry := range pool {
		acc += entry.Weight
		if target < acc {
			return entry.TileID
		}
	}
	return pool[len(pool)-1].TileID
}

// clampTileRect intersects r with [0, w-1] x [0, h-1].
func clampTileRect(r TileRect, w, h int) (TileRect, bool) {
	if w <= 0 || h <= 0 || r.Col1 < 0 || r.Row1 < 0 || r.Col0 >= w || r.Row0 >= h {
		return TileRect{}, false
	}
	r.Col0 = max(r.Col0, 0)
	r.Row0 = max(r.Row0, 0)
	r.Col1 = min(r.Col1, w-1)
	r.Row1 = min(r.Row1, h-1)
	if r.Col0 > r.Col1 || r.Row0 > r.Row1 {
		return TileRect{}, false
	}
	return r, true
}

// bresenhamLine visits every integer grid cell along the Bresenham line from (x0, y0) to (x1, y1), inclusive.
func bresenhamLine(x0, y0, x1, y1 int, visit func(col, row int)) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	x, y := x0, y0
	for {
		visit(x, y)
		if x == x1 && y == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
	}
}

// bresenhamLineClipped runs bresenhamLine between (x0, y0) and (x1, y1), visiting only cells inside [0, w-1] x [0, h-1].
// When either endpoint lies far outside the map, the segment is first clipped to [-1, w] x [-1, h] so the walk stays O(max(w, h)).
func bresenhamLineClipped(x0, y0, x1, y1, w, h int, visit func(col, row int)) {
	if w <= 0 || h <= 0 {
		return
	}
	minX, maxX := min(x0, x1), max(x0, x1)
	minY, maxY := min(y0, y1), max(y0, y1)
	if maxX < 0 || maxY < 0 || minX >= w || minY >= h {
		return
	}
	if x0 < -1 || x0 > w || y0 < -1 || y0 > h || x1 < -1 || x1 > w || y1 < -1 || y1 > h {
		// Clip segment in cell-center coordinates to [-1, w] x [-1, h].
		t0, t1 := 0.0, 1.0
		fx0, fy0 := float64(x0)+0.5, float64(y0)+0.5
		fx1, fy1 := float64(x1)+0.5, float64(y1)+0.5
		dx, dy := fx1-fx0, fy1-fy0
		clip := func(p, q float64) bool {
			if p == 0 {
				return q >= 0
			}
			r := q / p
			if p < 0 {
				if r > t1 {
					return false
				}
				t0 = max(t0, r)
			} else {
				if r < t0 {
					return false
				}
				t1 = min(t1, r)
			}
			return true
		}
		if !clip(-dx, fx0+1) || !clip(dx, float64(w+1)-fx0) || !clip(-dy, fy0+1) || !clip(dy, float64(h+1)-fy0) {
			return
		}
		if t0 > 0 {
			x0 = int(math.Floor(fx0 + t0*dx))
			y0 = int(math.Floor(fy0 + t0*dy))
		}
		if t1 < 1 {
			x1 = int(math.Floor(fx0 + t1*dx))
			y1 = int(math.Floor(fy0 + t1*dy))
		}
	}
	bresenhamLine(x0, y0, x1, y1, func(c, r int) {
		if c >= 0 && c < w && r >= 0 && r < h {
			visit(c, r)
		}
	})
}

func posMod(a, m int) int {
	r := a % m
	if r < 0 {
		r += m
	}
	return r
}

// tileTargetGrid resolves the inline tilemap grid for layer (without checking a single cell coordinate).
func (e *Editor) tileTargetGrid(layer string) (string, string, tileGrid, error) {
	layerName, tm, err := e.resolveInlineTilemap(layer)
	if err != nil {
		return "", "", tileGrid{}, err
	}
	tileLayer := ""
	if tm.IsLayered() {
		tileLayer = e.tileLayerName(tm)
	}
	g, err := gridOf(tm, tileLayer)
	if err != nil {
		return "", "", tileGrid{}, err
	}
	return layerName, tileLayer, g, nil
}

// checkLayerEditableForTiles verifies the layer is not playing, locked, or hidden.
func (e *Editor) checkLayerEditableForTiles(layerName string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.layerLocked[layerName] {
		return fmt.Errorf("editor: layer %q is locked", layerName)
	}
	if !e.layerDrawn(layerName) {
		return fmt.Errorf("editor: layer %q is hidden", layerName)
	}
	return nil
}

// stampValue converts a stamp cell (tileID, flip) into the tileGrid value for g.
func (e *Editor) stampValue(g tileGrid, tileID int, flip uint32, hasFlips bool) (int64, error) {
	if tileID == 0 {
		return 0, nil
	}
	if g.layer == nil {
		return int64(tileID), nil
	}
	if tileID > int(tilemap.TileIDMask) {
		return 0, fmt.Errorf("editor: tileID %d is above %d", tileID, tilemap.TileIDMask)
	}
	f := flip & tilemap.FlipMask
	if !hasFlips {
		f = e.ActiveTileFlip & tilemap.FlipMask
	}
	return int64(uint32(tileID) | f), nil
}

// FillTileRect fills the inclusive rectangle from (col0, row0) to (col1, row1) on layer's inline tilemap
// with tileID (0 erases) as one undoable command.
func (e *Editor) FillTileRect(layer string, col0, row0, col1, row1, tileID int) error {
	if e == nil {
		return fmt.Errorf("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if tileID < 0 {
		return fmt.Errorf("editor: tileID %d must be non-negative", tileID)
	}
	e.endTileStrokeForChange()
	layerName, tileLayer, g, err := e.tileTargetGrid(layer)
	if err != nil {
		return err
	}
	if err := e.checkLayerEditableForTiles(layerName); err != nil {
		return err
	}
	r, ok := clampTileRect(normalizeTileRect(col0, row0, col1, row1), g.tm.Width, g.tm.Height)
	if !ok {
		return fmt.Errorf("editor: tile rectangle (%d,%d)-(%d,%d) is out of bounds (%dx%d)", col0, row0, col1, row1, g.tm.Width, g.tm.Height)
	}
	val, err := e.paintValue(g, tileID)
	if err != nil {
		return err
	}
	values := g.values()
	var changes []tileChange
	for row := r.Row0; row <= r.Row1; row++ {
		for col := r.Col0; col <= r.Col1; col++ {
			idx := row*g.tm.Width + col
			if values[idx] != val {
				changes = append(changes, tileChange{index: idx, oldGID: values[idx], newGID: val})
			}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	verb := "Rect paint"
	if tileID == 0 {
		verb = "Rect erase"
	}
	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		tileLayer: tileLayer,
		label:     fmt.Sprintf("%s %d tiles", verb, len(changes)),
		changes:   changes,
	})
	return nil
}

// EraseTileRect erases the inclusive rectangle from (col0, row0) to (col1, row1) as one undoable command.
func (e *Editor) EraseTileRect(layer string, col0, row0, col1, row1 int) error {
	return e.FillTileRect(layer, col0, row0, col1, row1, 0)
}

// DrawTileLine draws a Bresenham line from (col0, row0) to (col1, row1) on layer's inline tilemap
// with tileID (0 erases) as one undoable command.
func (e *Editor) DrawTileLine(layer string, col0, row0, col1, row1, tileID int) error {
	if e == nil {
		return fmt.Errorf("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if tileID < 0 {
		return fmt.Errorf("editor: tileID %d must be non-negative", tileID)
	}
	e.endTileStrokeForChange()
	layerName, tileLayer, g, err := e.tileTargetGrid(layer)
	if err != nil {
		return err
	}
	if err := e.checkLayerEditableForTiles(layerName); err != nil {
		return err
	}
	val, err := e.paintValue(g, tileID)
	if err != nil {
		return err
	}
	values := g.values()
	seen := make(map[int]bool)
	var changes []tileChange
	bresenhamLineClipped(col0, row0, col1, row1, g.tm.Width, g.tm.Height, func(col, row int) {
		idx := row*g.tm.Width + col
		if seen[idx] {
			return
		}
		seen[idx] = true
		if values[idx] != val {
			changes = append(changes, tileChange{index: idx, oldGID: values[idx], newGID: val})
		}
	})
	if len(changes) == 0 {
		return nil
	}
	verb := "Line paint"
	if tileID == 0 {
		verb = "Line erase"
	}
	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		tileLayer: tileLayer,
		label:     fmt.Sprintf("%s %d tiles", verb, len(changes)),
		changes:   changes,
	})
	return nil
}

// EraseTileLine erases a Bresenham line from (col0, row0) to (col1, row1) as one undoable command.
func (e *Editor) EraseTileLine(layer string, col0, row0, col1, row1 int) error {
	return e.DrawTileLine(layer, col0, row0, col1, row1, 0)
}

// StampTiles places stamp with its top-left corner at (col, row) on layer's inline tilemap as one undoable command.
// Cells falling outside the map are clipped.
func (e *Editor) StampTiles(layer string, col, row int, stamp TileStamp) error {
	if e == nil {
		return fmt.Errorf("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if err := validateTileStamp(stamp); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	layerName, tileLayer, g, err := e.tileTargetGrid(layer)
	if err != nil {
		return err
	}
	if err := e.checkLayerEditableForTiles(layerName); err != nil {
		return err
	}
	return e.applyStampAt(layerName, tileLayer, g, col, row, stamp, "Stamp")
}

func (e *Editor) applyStampAt(layerName, tileLayer string, g tileGrid, col, row int, stamp TileStamp, verb string) error {
	values := g.values()
	hasFlips := len(stamp.Flips) == len(stamp.Tiles)
	var changes []tileChange
	for dy := range stamp.Height {
		mr := row + dy
		if mr < 0 || mr >= g.tm.Height {
			continue
		}
		for dx := range stamp.Width {
			mc := col + dx
			if mc < 0 || mc >= g.tm.Width {
				continue
			}
			tid, flip := stamp.Cell(dx, dy)
			val, err := e.stampValue(g, tid, flip, hasFlips)
			if err != nil {
				return err
			}
			idx := mr*g.tm.Width + mc
			if values[idx] != val {
				changes = append(changes, tileChange{index: idx, oldGID: values[idx], newGID: val})
			}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		tileLayer: tileLayer,
		label:     fmt.Sprintf("%s %d tiles", verb, len(changes)),
		changes:   changes,
	})
	return nil
}

// CopyTileRegion copies the inclusive rectangle from (col0, row0) to (col1, row1) on layer's inline tilemap
// into the editor's tile clipboard and active stamp, returning a copy of the captured TileStamp.
func (e *Editor) CopyTileRegion(layer string, col0, row0, col1, row1 int) (TileStamp, error) {
	if e == nil {
		return TileStamp{}, fmt.Errorf("editor: nil editor")
	}
	if e.playing {
		return TileStamp{}, ErrPlaying
	}
	_, _, g, err := e.tileTargetGrid(layer)
	if err != nil {
		return TileStamp{}, err
	}
	r, ok := clampTileRect(normalizeTileRect(col0, row0, col1, row1), g.tm.Width, g.tm.Height)
	if !ok {
		return TileStamp{}, fmt.Errorf("editor: tile rectangle (%d,%d)-(%d,%d) is out of bounds (%dx%d)", col0, row0, col1, row1, g.tm.Width, g.tm.Height)
	}
	stamp := readTileStamp(g, r)
	e.tileClipboard = stamp.Clone()
	e.activeStamp = stamp.Clone()
	e.paletteStampRect = TileRect{}
	e.hasPaletteStampRect = false
	e.Status = fmt.Sprintf("Copied %dx%d tile region", stamp.Width, stamp.Height)
	e.notify()
	return stamp, nil
}

// CutTileRegion copies the inclusive rectangle from (col0, row0) to (col1, row1) into the tile clipboard
// and active stamp, and clears those cells on layer's inline tilemap in one undoable command.
func (e *Editor) CutTileRegion(layer string, col0, row0, col1, row1 int) (TileStamp, error) {
	if e == nil {
		return TileStamp{}, fmt.Errorf("editor: nil editor")
	}
	if e.playing {
		return TileStamp{}, ErrPlaying
	}
	e.endTileStrokeForChange()
	layerName, tileLayer, g, err := e.tileTargetGrid(layer)
	if err != nil {
		return TileStamp{}, err
	}
	if err := e.checkLayerEditableForTiles(layerName); err != nil {
		return TileStamp{}, err
	}
	r, ok := clampTileRect(normalizeTileRect(col0, row0, col1, row1), g.tm.Width, g.tm.Height)
	if !ok {
		return TileStamp{}, fmt.Errorf("editor: tile rectangle (%d,%d)-(%d,%d) is out of bounds (%dx%d)", col0, row0, col1, row1, g.tm.Width, g.tm.Height)
	}
	stamp := readTileStamp(g, r)
	e.tileClipboard = stamp.Clone()
	e.activeStamp = stamp.Clone()
	e.paletteStampRect = TileRect{}
	e.hasPaletteStampRect = false

	values := g.values()
	var changes []tileChange
	for row := r.Row0; row <= r.Row1; row++ {
		for col := r.Col0; col <= r.Col1; col++ {
			idx := row*g.tm.Width + col
			if values[idx] != 0 {
				changes = append(changes, tileChange{index: idx, oldGID: values[idx], newGID: 0})
			}
		}
	}
	if len(changes) > 0 {
		e.History.Execute(&tileEditCmd{
			ed:        e,
			layerName: layerName,
			tileLayer: tileLayer,
			label:     fmt.Sprintf("Cut %d tiles", len(changes)),
			changes:   changes,
		})
	} else {
		e.Status = fmt.Sprintf("Cut %dx%d tile region", stamp.Width, stamp.Height)
		e.notify()
	}
	return stamp, nil
}

// PasteTileRegion pastes the tile clipboard with its top-left corner at (col, row) on layer's inline tilemap
// in one undoable command.
func (e *Editor) PasteTileRegion(layer string, col, row int) error {
	if e == nil {
		return fmt.Errorf("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.tileClipboard.Empty() {
		return fmt.Errorf("editor: tile clipboard is empty")
	}
	e.endTileStrokeForChange()
	layerName, tileLayer, g, err := e.tileTargetGrid(layer)
	if err != nil {
		return err
	}
	if err := e.checkLayerEditableForTiles(layerName); err != nil {
		return err
	}
	return e.applyStampAt(layerName, tileLayer, g, col, row, e.tileClipboard, "Paste")
}

// CopySelectedTileRegion copies the active TileSelection on ActiveLayer into the tile clipboard and active stamp.
func (e *Editor) CopySelectedTileRegion() (TileStamp, error) {
	if e == nil || !e.hasTileSel {
		return TileStamp{}, fmt.Errorf("editor: no tile region selected")
	}
	return e.CopyTileRegion(e.ActiveLayer, e.tileSel.Col0, e.tileSel.Row0, e.tileSel.Col1, e.tileSel.Row1)
}

// CutSelectedTileRegion cuts the active TileSelection on ActiveLayer into the tile clipboard and active stamp as one undoable step.
func (e *Editor) CutSelectedTileRegion() (TileStamp, error) {
	if e == nil || !e.hasTileSel {
		return TileStamp{}, fmt.Errorf("editor: no tile region selected")
	}
	return e.CutTileRegion(e.ActiveLayer, e.tileSel.Col0, e.tileSel.Row0, e.tileSel.Col1, e.tileSel.Row1)
}

func readTileStamp(g tileGrid, r TileRect) TileStamp {
	w, h := r.Width(), r.Height()
	values := g.values()
	tiles := make([]int, w*h)
	var flips []uint32
	if g.layer != nil {
		flips = make([]uint32, w*h)
	}
	for dy := range h {
		for dx := range w {
			v := values[(r.Row0+dy)*g.tm.Width+(r.Col0+dx)]
			i := dy*w + dx
			if g.layer != nil {
				var u uint32
				if v >= 0 && v <= math.MaxUint32 {
					u = uint32(v)
				}
				tiles[i] = int(u & tilemap.TileIDMask)
				flips[i] = u & tilemap.FlipMask
			} else {
				tiles[i] = int(v)
			}
		}
	}
	return TileStamp{Width: w, Height: h, Tiles: tiles, Flips: flips}
}

// TilePreview returns the cells currently previewed by an in-progress brush stroke before release, in deterministic row-major order.
func (e *Editor) TilePreview() []TilePreviewCell {
	if e == nil || e.stroke == nil || len(e.stroke.preview) == 0 {
		return nil
	}
	out := make([]TilePreviewCell, len(e.stroke.preview))
	copy(out, e.stroke.preview)
	return out
}

// Spec token colors for brush preview and selection overlays.
var (
	previewAccentColor = core.RGBA(0x3B7BFF, 1)
	previewEraseColor  = core.RGBA(0xFF6B6B, 1)
)

// drawTileSelectionOverlay draws a dashed marquee around the active TileSelection (or in-progress region select stroke).
func (e *Editor) drawTileSelectionOverlay(d *core.Drawing) {
	var rect TileRect
	var has bool
	targetLayer := e.ActiveLayer
	if e.stroke != nil && e.stroke.mode == TileStrokeSelectRegion {
		targetLayer = e.stroke.layer
		_, tm, err := e.resolveInlineTilemap(targetLayer)
		if err != nil {
			return
		}
		if r, ok := clampTileRect(normalizeTileRect(e.stroke.startCol, e.stroke.startRow, e.stroke.endCol, e.stroke.endRow), tm.Width, tm.Height); ok {
			rect, has = r, true
		}
	} else if e.hasTileSel && isTileTool(e.Tool) {
		rect, has = e.tileSel, true
	}
	if !has {
		return
	}
	layer, tm, err := e.resolveInlineTilemap(targetLayer)
	if err != nil || !e.layerDrawn(layer) {
		return
	}
	space, px, py := e.layerSpaceAndParallax(layer)
	m := e.layerAffineFor(space, px, py)
	x := float32(rect.Col0 * tm.TileWidth)
	y := float32(rect.Row0 * tm.TileHeight)
	w := float32(rect.Width() * tm.TileWidth)
	h := float32(rect.Height() * tm.TileHeight)
	d.Save()
	applyAffine(d, m)
	d.SetFillColor(core.RGBA(0x3B7BFF, 0.14)).FillRect(x, y, w, h)
	d.SetStrokeColor(previewAccentColor).SetLineWidth(1.5/strokeScale(m)).SetLineDash(4/strokeScale(m), 3/strokeScale(m)).StrokeRect(x, y, w, h)
	d.Restore()
}

// drawTilePreviewOverlay draws the live preview of an in-progress deferred brush stroke (Rectangle, Line, Stamp, Random).
func (e *Editor) drawTilePreviewOverlay(d *core.Drawing) bool {
	s := e.stroke
	if s == nil || !s.isPreviewMode() {
		return false
	}
	if s.mode == TileStrokeSelectRegion {
		return true
	}
	if len(s.preview) == 0 {
		return true
	}
	layer, tm, err := e.resolveInlineTilemap(s.layer)
	if err != nil {
		return true
	}
	space, px, py := e.layerSpaceAndParallax(layer)
	m := e.layerAffineFor(space, px, py)
	tw, th := float32(tm.TileWidth), float32(tm.TileHeight)
	lw := 1.5 / strokeScale(m)
	var pal *TilePalette
	if tm.IsLayered() {
		p := e.TilePalette()
		pal = &p
	}
	d.Save()
	applyAffine(d, m)
	for _, cell := range s.preview {
		x, y := float32(cell.Col)*tw, float32(cell.Row)*th
		if cell.Erase || cell.Tile == 0 {
			d.SetFillColor(core.RGBA(0xFF6B6B, 0.25)).FillRect(x, y, tw, th)
			d.SetStrokeColor(previewEraseColor).SetLineWidth(lw).StrokeRect(x, y, tw, th)
		} else {
			e.drawGhostTileID(d, tm, cell.Tile, cell.Flip, x, y, pal)
			d.SetStrokeColor(previewAccentColor).SetLineWidth(lw).StrokeRect(x, y, tw, th)
		}
	}
	d.Restore()
	return true
}

// drawStampHoverOverlay draws the full active stamp footprint anchored at (col, row) when hovering with ToolStampTile.
func (e *Editor) drawStampHoverOverlay(d *core.Drawing, tm *sceneformat.TilemapDoc, col, row int, lw float32) {
	stamp, ok := e.effectiveStamp()
	if !ok {
		return
	}
	tw, th := float32(tm.TileWidth), float32(tm.TileHeight)
	var pal *TilePalette
	if tm.IsLayered() {
		p := e.TilePalette()
		pal = &p
	}
	hasFlips := len(stamp.Flips) == len(stamp.Tiles)
	for dy := range stamp.Height {
		mr := row + dy
		if mr < 0 || mr >= tm.Height {
			continue
		}
		for dx := range stamp.Width {
			mc := col + dx
			if mc < 0 || mc >= tm.Width {
				continue
			}
			tid, flip := stamp.Cell(dx, dy)
			if !hasFlips {
				flip = e.ActiveTileFlip & tilemap.FlipMask
			}
			x, y := float32(mc)*tw, float32(mr)*th
			if tid == 0 {
				d.SetFillColor(core.RGBA(0xFF6B6B, 0.18)).FillRect(x, y, tw, th)
			} else {
				e.drawGhostTileID(d, tm, tid, flip, x, y, pal)
			}
		}
	}
	// Outline the clamped stamp footprint.
	if r, ok := clampTileRect(TileRect{Col0: col, Row0: row, Col1: col + stamp.Width - 1, Row1: row + stamp.Height - 1}, tm.Width, tm.Height); ok {
		d.SetStrokeColor(hoverPaintColor).SetLineWidth(lw).StrokeRect(
			float32(r.Col0)*tw, float32(r.Row0)*th,
			float32(r.Width())*tw, float32(r.Height())*th,
		)
	}
}

// drawGhostTileID draws tileID (with optional layered flip bits) faded at layer point (x, y).
func (e *Editor) drawGhostTileID(d *core.Drawing, tm *sceneformat.TilemapDoc, tileID int, flip uint32, x, y float32, pal *TilePalette) {
	if tileID <= 0 || e.Resolver == nil {
		return
	}
	if tm.IsLayered() {
		var p TilePalette
		if pal != nil {
			p = *pal
		} else {
			p = e.TilePalette()
		}
		tile, ok := p.Tile(tileID)
		if !ok || tile.Frame.W <= 0 || tile.Frame.H <= 0 {
			return
		}
		f := tile.Frame
		d.Save().SetAlpha(0.5)
		if flip&tilemap.FlipMask != 0 {
			cx := x + float32(tm.TileWidth)/2
			cy := y + float32(tm.TileHeight)/2
			d.Translate(cx, cy)
			if flip&tilemap.FlipDiagonal != 0 {
				d.Transform(0, 1, 1, 0, 0, 0)
			}
			sx, sy := float32(1), float32(1)
			if flip&tilemap.FlipHorizontal != 0 {
				sx = -1
			}
			if flip&tilemap.FlipVertical != 0 {
				sy = -1
			}
			d.Scale(sx, sy)
			d.Translate(-cx, -cy)
		}
		d.DrawImageFrom(tile.Image, f.X, f.Y, f.W, f.H, x+f.OffsetX, y+f.OffsetY, f.W, f.H)
		d.Restore()
		return
	}
	atlas, err := e.Resolver.Atlas(tm.Tileset)
	if err != nil || atlas == nil {
		return
	}
	f, ok := atlas.Frames[strconv.Itoa(tileID)]
	if !ok {
		return
	}
	sp := sprite.NewSprite(f)
	sp.X, sp.Y, sp.Alpha = x, y, 0.5
	(&sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&sp}}).Draw(d)
}
