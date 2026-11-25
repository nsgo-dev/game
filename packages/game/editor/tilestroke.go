package editor

import (
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// TileStrokeMode says what a tile stroke (Editor.BeginTileStroke) does to the cells under the pointer.
type TileStrokeMode int

const (
	// TileStrokePaint writes the active tile (ActiveTileID) into every cell the stroke crosses.
	TileStrokePaint TileStrokeMode = iota
	// TileStrokeErase clears every cell the stroke crosses.
	TileStrokeErase
	// TileStrokePick makes the tile in the cell under the pointer the active tile, as the pointer moves, and edits nothing.
	TileStrokePick
	// TileStrokeTerrain places terrain tiles from the active terrain set into every cell the stroke crosses and updates neighbors with SetAutotile rules.
	TileStrokeTerrain
	// TileStrokeEraseTerrain removes terrain from every cell the stroke crosses and updates neighbors with SetAutotile rules.
	TileStrokeEraseTerrain
	// TileStrokeRect fills the dragged rectangle with ActiveTileID, previewed live and committed on EndTileStroke.
	TileStrokeRect
	// TileStrokeRectErase clears every cell in the dragged rectangle, previewed live and committed on EndTileStroke.
	TileStrokeRectErase
	// TileStrokeLine draws a Bresenham line of ActiveTileID between the start and end cells, previewed live and committed on EndTileStroke.
	TileStrokeLine
	// TileStrokeLineErase clears every cell along a Bresenham line between the start and end cells, previewed live and committed on EndTileStroke.
	TileStrokeLineErase
	// TileStrokeStamp paints the active TileStamp pattern along the stroke, pattern-aligned to the start cell, previewed live and committed on EndTileStroke.
	TileStrokeStamp
	// TileStrokeRandom paints deterministic weighted picks from the random brush pool along the stroke, previewed live and committed on EndTileStroke.
	TileStrokeRandom
	// TileStrokeSelectRegion selects a rectangular region of cells on the active tilemap for Copy or Cut.
	TileStrokeSelectRegion
)

// String returns a human-readable label for the mode.
func (m TileStrokeMode) String() string {
	switch m {
	case TileStrokePaint:
		return "Paint"
	case TileStrokeErase:
		return "Erase"
	case TileStrokePick:
		return "Pick"
	case TileStrokeTerrain:
		return "Paint terrain"
	case TileStrokeEraseTerrain:
		return "Erase terrain"
	case TileStrokeRect:
		return "Rect paint"
	case TileStrokeRectErase:
		return "Rect erase"
	case TileStrokeLine:
		return "Line paint"
	case TileStrokeLineErase:
		return "Line erase"
	case TileStrokeStamp:
		return "Stamp"
	case TileStrokeRandom:
		return "Random paint"
	case TileStrokeSelectRegion:
		return "Select region"
	default:
		return "Unknown"
	}
}

// tileStroke is a tile stroke under way: the layer it edits, the tile layer of a layered grid (empty for a single grid), the tile value it writes, where the pointer last was in the layer's space, and either the cells it has changed live (Paint/Erase/Terrain) or its live preview before release (Rect/Line/Stamp/Random/SelectRegion).
type tileStroke struct {
	mode        TileStrokeMode
	layer       string
	tileLayer   string
	value       int64
	terrainName string
	terrain     *tilemap.Terrain
	lastX       float32
	lastY       float32
	changes     []tileChange
	changeIndex map[int]int
	visited     map[int]bool
	// picked says a pick stroke picked a tile, and pickedAt is the cell it last looked at (index into the map's grid, -1 for none), which it does not look at again until the pointer leaves it.
	picked   bool
	pickedAt int
	// Preview brush state (used when isPreviewMode is true).
	activeTileID int
	activeFlip   uint32
	startCol     int
	startRow     int
	endCol       int
	endRow       int
	stamp        TileStamp
	randomPool   []WeightedTile
	randomSeed   uint64
	preview      []TilePreviewCell
	previewMap   map[int]TilePreviewCell
}

func (s *tileStroke) isPreviewMode() bool {
	return s != nil && s.mode >= TileStrokeRect && s.mode <= TileStrokeSelectRegion
}

// recordChange merges a change to cell idx into s.changes, preserving the original pre-stroke oldGID when the cell is updated more than once during the same stroke (as terrain neighbor updates do).
func (s *tileStroke) recordChange(idx int, oldGID, newGID int64) {
	if slot, ok := s.changeIndex[idx]; ok {
		s.changes[slot].newGID = newGID
		return
	}
	s.changeIndex[idx] = len(s.changes)
	s.changes = append(s.changes, tileChange{index: idx, oldGID: oldGID, newGID: newGID})
}

// tileHover is where the mouse rests over the viewport's canvas, and whether it would pick there (Alt held), which the hover outline (HoveredTile) is drawn from.
type tileHover struct {
	on      bool
	x, y    float32
	picking bool
}

// BeginTileStroke starts a stroke of mode on the active layer's inline tilemap at the canvas point (x, y).
// Freehand Paint, Erase, and Terrain strokes apply live; Rect, Line, Stamp, Random and SelectRegion accumulate a live preview
// (TilePreview) without mutating the document until EndTileStroke records every changed cell as one undo step.
// CancelTileStroke discards the preview or restores live-painted cells without recording history.
//
// It returns false and starts nothing while playing, in 3D, while another stroke is under way, for non-finite coordinates, when the active layer has no inline tilemap to edit, for an edit stroke on a locked or hidden layer (reporting why in the status bar), for a terrain stroke without a valid terrain set, and when required tile/stamp/random parameters are invalid.
func (e *Editor) BeginTileStroke(mode TileStrokeMode, x, y float32) bool {
	if e == nil || e.playing || e.ViewMode != ViewMode2D || e.stroke != nil {
		return false
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return false
	}
	if mode < TileStrokePaint || mode > TileStrokeSelectRegion {
		return false
	}
	if (mode == TileStrokePaint || mode == TileStrokeRect || mode == TileStrokeLine) && e.ActiveTileID < 0 {
		return false
	}
	layer, tm, err := e.resolveInlineTilemap(e.ActiveLayer)
	if err != nil || layer != e.ActiveLayer || tm.TileWidth <= 0 || tm.TileHeight <= 0 {
		return false
	}
	tileLayer := ""
	if tm.IsLayered() {
		tileLayer = e.tileLayerName(tm)
	}
	g, err := gridOf(tm, tileLayer)
	if err != nil {
		return false
	}
	var val int64
	var terrainName string
	var terrain *tilemap.Terrain
	if mode == TileStrokePaint || mode == TileStrokeRect || mode == TileStrokeLine {
		if val, err = e.paintValue(g, e.ActiveTileID); err != nil {
			return false
		}
	}
	var stamp TileStamp
	if mode == TileStrokeStamp {
		var ok bool
		stamp, ok = e.effectiveStamp()
		if !ok {
			return false
		}
		stamp = stamp.Clone()
	}
	var pool []WeightedTile
	if mode == TileStrokeRandom {
		pool = slices.Clone(e.effectiveRandomPool())
		if len(pool) == 0 {
			return false
		}
	}
	if mode != TileStrokePick && mode != TileStrokeSelectRegion {
		if err := e.lockedLayerErr(); err != nil {
			e.report(err)
			return false
		}
		if !e.layerDrawn(layer) {
			e.report(fmt.Errorf("editor: layer %q is hidden", layer))
			return false
		}
		e.exitTileReviewBeforePreview()
	}
	if mode == TileStrokeTerrain || mode == TileStrokeEraseTerrain {
		var rErr error
		_, tileLayer, g, terrainName, terrain, rErr = e.resolveTerrainForLayer(layer, e.ActiveTerrain)
		if rErr != nil {
			e.report(rErr)
			return false
		}
	}
	lx, ly := e.layerCoordsForActive(x, y)
	if !isFiniteFloat32(lx) || !isFiniteFloat32(ly) {
		return false
	}
	startCol, startRow, ok := cellFloor(tm, lx, ly)
	if !ok {
		return false
	}
	var activeFlip uint32
	if tm.IsLayered() {
		activeFlip = e.ActiveTileFlip & tilemap.FlipMask
	}
	e.stroke = &tileStroke{
		mode:         mode,
		layer:        layer,
		tileLayer:    tileLayer,
		value:        val,
		terrainName:  terrainName,
		terrain:      terrain,
		lastX:        lx,
		lastY:        ly,
		changeIndex:  map[int]int{},
		visited:      map[int]bool{},
		pickedAt:     -1,
		activeTileID: e.ActiveTileID,
		activeFlip:   activeFlip,
		startCol:     startCol,
		startRow:     startRow,
		endCol:       startCol,
		endRow:       startRow,
		stamp:        stamp,
		randomPool:   pool,
		randomSeed:   e.randomSeed,
	}
	if mode == TileStrokePick {
		e.pickAt(lx, ly)
		return true
	}
	if e.stroke.isPreviewMode() {
		e.stroke.previewMap = map[int]TilePreviewCell{}
		e.updateStrokePreview(lx, ly, true)
		return true
	}
	e.strokeSegment(lx, ly, lx, ly)
	return true
}

// ContinueTileStroke carries the stroke under way on from the pointer's last point to the canvas point (x, y).
// For Paint, Erase and Terrain it edits every cell the straight line crosses; for Rect, Line, Stamp, Random and SelectRegion
// it updates the live preview without mutating the scene document; for Pick it picks the tile at (x, y).
func (e *Editor) ContinueTileStroke(x, y float32) {
	if e == nil || e.stroke == nil || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return
	}
	s := e.stroke
	lx, ly := e.layerCoordsForActive(x, y)
	if !isFiniteFloat32(lx) || !isFiniteFloat32(ly) {
		return
	}
	switch {
	case s.mode == TileStrokePick:
		e.pickAt(lx, ly)
	case s.isPreviewMode():
		e.updateStrokePreview(lx, ly, false)
	default:
		e.strokeSegment(s.lastX, s.lastY, lx, ly)
	}
	s.lastX, s.lastY = lx, ly
}

// EndTileStroke ends the stroke under way, recording every cell it changed as one undo step; a stroke that changed nothing records none.
// A pick stroke made with the Pick tool that picked a tile makes Paint the active tool, so the picked tile paints next.
// A SelectRegion stroke updates TileSelection without recording an undo step.
func (e *Editor) EndTileStroke() {
	if e == nil || e.stroke == nil {
		return
	}
	s := e.stroke
	e.stroke = nil
	if s.mode == TileStrokePick {
		if s.picked && e.Tool == ToolPickTile {
			e.SetTool(ToolPaintTile)
		}
		return
	}
	if s.mode == TileStrokeSelectRegion {
		_, tm, err := e.resolveInlineTilemap(s.layer)
		if err == nil {
			if r, ok := clampTileRect(normalizeTileRect(s.startCol, s.startRow, s.endCol, s.endRow), tm.Width, tm.Height); ok {
				e.tileSel = r
				e.hasTileSel = true
				e.Status = fmt.Sprintf("Selected %dx%d tile region", r.Width(), r.Height())
			}
		}
		e.notify()
		return
	}
	if s.isPreviewMode() {
		e.commitPreviewStroke(s)
		return
	}
	net := s.changes[:0]
	for _, ch := range s.changes {
		if ch.oldGID != ch.newGID {
			net = append(net, ch)
		}
	}
	if len(net) == 0 {
		return
	}
	var label string
	switch s.mode {
	case TileStrokeTerrain, TileStrokeEraseTerrain:
		label = fmt.Sprintf("%s %q (%d tiles)", s.mode, s.terrainName, len(net))
	default:
		label = fmt.Sprintf("%s %d tiles", s.mode, len(net))
		if len(net) == 1 {
			idx := net[0].index
			if _, tm, err := e.resolveInlineTilemap(s.layer); err == nil && tm.Width > 0 {
				label = fmt.Sprintf("%s tile (%d,%d)", s.mode, idx%tm.Width, idx/tm.Width)
			}
		}
	}
	// The cells already hold the stroke's tiles, so Do writes what is there and History records the step.
	e.History.Execute(&tileEditCmd{ed: e, layerName: s.layer, tileLayer: s.tileLayer, label: label, changes: net})
}

func (e *Editor) commitPreviewStroke(s *tileStroke) {
	if len(s.preview) == 0 {
		e.notify()
		return
	}
	_, tm, err := e.resolveInlineTilemap(s.layer)
	if err != nil {
		e.notify()
		return
	}
	g, err := gridOf(tm, s.tileLayer)
	if err != nil {
		e.notify()
		return
	}
	values := g.values()
	var changes []tileChange
	for _, cell := range s.preview {
		if cell.Col < 0 || cell.Col >= tm.Width || cell.Row < 0 || cell.Row >= tm.Height {
			continue
		}
		idx := cell.Row*tm.Width + cell.Col
		var newVal int64
		if !cell.Erase && cell.Tile > 0 {
			if g.layer != nil {
				if cell.Tile > int(tilemap.TileIDMask) {
					continue
				}
				newVal = int64(uint32(cell.Tile) | (cell.Flip & tilemap.FlipMask))
			} else {
				newVal = int64(cell.Tile)
			}
		}
		if values[idx] != newVal {
			changes = append(changes, tileChange{index: idx, oldGID: values[idx], newGID: newVal})
		}
	}
	if len(changes) == 0 {
		e.notify()
		return
	}
	label := fmt.Sprintf("%s %d tiles", s.mode, len(changes))
	if len(changes) == 1 && tm.Width > 0 {
		idx := changes[0].index
		label = fmt.Sprintf("%s tile (%d,%d)", s.mode, idx%tm.Width, idx/tm.Width)
	}
	e.History.Execute(&tileEditCmd{ed: e, layerName: s.layer, tileLayer: s.tileLayer, label: label, changes: changes})
}

// CancelTileStroke ends the stroke under way without recording it, discarding any live preview or putting back every cell a live stroke changed, as on Escape or when a second finger turns a one-finger stroke into a two-finger pan. It does nothing when no stroke is under way.
func (e *Editor) CancelTileStroke() {
	if e == nil || e.stroke == nil {
		return
	}
	s := e.stroke
	e.stroke = nil
	if s.isPreviewMode() {
		e.notify()
		return
	}
	if len(s.changes) == 0 {
		return
	}
	if _, tm, err := e.resolveInlineTilemap(s.layer); err == nil {
		if g, err := gridOf(tm, s.tileLayer); err == nil {
			g.apply(s.changes, true)
		}
	}
	e.Rebuild()
	e.notify()
}

// TileStrokeActive reports whether a tile stroke is under way, and its mode.
func (e *Editor) TileStrokeActive() (TileStrokeMode, bool) {
	if e == nil || e.stroke == nil {
		return 0, false
	}
	return e.stroke.mode, true
}

// PickTile makes the tile in cell (col, row) of layer's inline tilemap (the active layer's when layer is empty) the active tile, as SetActiveTileID does, and reports whether the cell held one; an empty cell, one off the map, or a call while the scene plays leaves the active tile as it was and reports false. On a layered grid it picks from ActiveTileLayer (or the first tile layer) and copies the cell's flips into ActiveTileFlip.
func (e *Editor) PickTile(layer string, col, row int) bool {
	if e == nil || e.playing {
		return false
	}
	_, _, g, err := e.tileTarget(layer, col, row)
	if err != nil {
		return false
	}
	var id, number int
	if g.layer != nil {
		cell, err := g.layer.Cell(col, row)
		if err != nil {
			return false
		}
		id = int(cell & tilemap.TileIDMask)
		if id <= 0 {
			e.Status = fmt.Sprintf("Tile (%d,%d) is empty", col, row)
			e.notify()
			return false
		}
		number = id - 1
		e.ActiveTileFlip = cell & tilemap.FlipMask
	} else {
		id = g.tm.Data[row*g.tm.Width+col]
		if id <= 0 {
			e.Status = fmt.Sprintf("Tile (%d,%d) is empty", col, row)
			e.notify()
			return false
		}
		number = id
	}
	e.Status = fmt.Sprintf("Picked tile %d", number)
	if e.ActiveTileID == id {
		e.notify()
		return true
	}
	e.SetActiveTileID(id)
	return true
}

// endTileStrokeForChange ends a stroke under way before a change that would leave it editing a layer, tool or history it did not start on: choosing a tool or layer, changing layers, switching views, saving, undoing, redoing and playing.
func (e *Editor) endTileStrokeForChange() {
	if e.stroke != nil {
		e.EndTileStroke()
	}
}

func cellFloor(tm *sceneformat.TilemapDoc, lx, ly float32) (int, int, bool) {
	if tm == nil || tm.TileWidth <= 0 || tm.TileHeight <= 0 || !isFiniteFloat32(lx) || !isFiniteFloat32(ly) {
		return 0, 0, false
	}
	cx := math.Floor(float64(lx) / float64(tm.TileWidth))
	cy := math.Floor(float64(ly) / float64(tm.TileHeight))
	const maxCoord = float64(1 << 30)
	if math.IsNaN(cx) || math.IsInf(cx, 0) || math.IsNaN(cy) || math.IsInf(cy, 0) {
		return 0, 0, false
	}
	cx = min(max(cx, -maxCoord), maxCoord)
	cy = min(max(cy, -maxCoord), maxCoord)
	return int(cx), int(cy), true
}

func (e *Editor) updateStrokePreview(lx, ly float32, initial bool) {
	s := e.stroke
	_, tm, err := e.resolveInlineTilemap(s.layer)
	if err != nil {
		return
	}
	activeFlip := s.activeFlip
	switch s.mode {
	case TileStrokeRect, TileStrokeRectErase, TileStrokeSelectRegion:
		ec, er, ok := cellFloor(tm, lx, ly)
		if !ok {
			return
		}
		if !initial && ec == s.endCol && er == s.endRow {
			return
		}
		s.endCol, s.endRow = ec, er
		s.preview = s.preview[:0]
		if r, ok := clampTileRect(normalizeTileRect(s.startCol, s.startRow, ec, er), tm.Width, tm.Height); ok {
			erase := s.mode == TileStrokeRectErase
			tid := s.activeTileID
			if erase || s.mode == TileStrokeSelectRegion {
				tid = 0
			}
			for row := r.Row0; row <= r.Row1; row++ {
				for col := r.Col0; col <= r.Col1; col++ {
					s.preview = append(s.preview, TilePreviewCell{Col: col, Row: row, Tile: tid, Flip: activeFlip, Erase: erase})
				}
			}
		}
		e.notify()

	case TileStrokeLine, TileStrokeLineErase:
		ec, er, ok := cellFloor(tm, lx, ly)
		if !ok {
			return
		}
		if !initial && ec == s.endCol && er == s.endRow {
			return
		}
		s.endCol, s.endRow = ec, er
		s.preview = s.preview[:0]
		erase := s.mode == TileStrokeLineErase
		tid := s.activeTileID
		if erase {
			tid = 0
		}
		seen := make(map[int]bool)
		bresenhamLineClipped(s.startCol, s.startRow, ec, er, tm.Width, tm.Height, func(col, row int) {
			idx := row*tm.Width + col
			if seen[idx] {
				return
			}
			seen[idx] = true
			s.preview = append(s.preview, TilePreviewCell{Col: col, Row: row, Tile: tid, Flip: activeFlip, Erase: erase})
		})
		sortPreviewCells(s.preview)
		e.notify()

	case TileStrokeStamp:
		prevCol, prevRow, okPrev := cellFloor(tm, s.lastX, s.lastY)
		curCol, curRow, okCur := cellFloor(tm, lx, ly)
		if !okCur {
			return
		}
		if initial || !okPrev {
			prevCol, prevRow = curCol, curRow
		}
		s.endCol, s.endRow = curCol, curRow
		hasFlips := len(s.stamp.Flips) == len(s.stamp.Tiles)
		added := false
		// Walk anchors along the drag so fast moves leave no stamp gaps; clamp extreme off-map coordinates.
		minC, maxC := -s.stamp.Width, tm.Width
		minR, maxR := -s.stamp.Height, tm.Height
		c0, r0 := min(max(prevCol, minC), maxC), min(max(prevRow, minR), maxR)
		c1, r1 := min(max(curCol, minC), maxC), min(max(curRow, minR), maxR)
		bresenhamLine(c0, r0, c1, r1, func(ac, ar int) {
			for dy := range s.stamp.Height {
				mr := ar + dy
				if mr < 0 || mr >= tm.Height {
					continue
				}
				for dx := range s.stamp.Width {
					mc := ac + dx
					if mc < 0 || mc >= tm.Width {
						continue
					}
					idx := mr*tm.Width + mc
					if _, exists := s.previewMap[idx]; exists {
						continue
					}
					sx := posMod(mc-s.startCol, s.stamp.Width)
					sy := posMod(mr-s.startRow, s.stamp.Height)
					tid, flip := s.stamp.Cell(sx, sy)
					if !hasFlips {
						flip = activeFlip
					}
					s.previewMap[idx] = TilePreviewCell{Col: mc, Row: mr, Tile: tid, Flip: flip, Erase: tid == 0}
					added = true
				}
			}
		})
		if added || initial {
			rebuildPreviewSlice(s)
			e.notify()
		}

	case TileStrokeRandom:
		tw, th := float64(tm.TileWidth), float64(tm.TileHeight)
		x0, y0 := float64(s.lastX)/tw, float64(s.lastY)/th
		x1, y1 := float64(lx)/tw, float64(ly)/th
		if initial {
			x0, y0 = x1, y1
		}
		added := false
		cellsAlong(x0, y0, x1, y1, tm.Width, tm.Height, func(col, row int) {
			idx := row*tm.Width + col
			if _, exists := s.previewMap[idx]; exists {
				return
			}
			tid := randomPickTile(s.randomSeed, col, row, s.randomPool)
			s.previewMap[idx] = TilePreviewCell{Col: col, Row: row, Tile: tid, Flip: activeFlip}
			added = true
		})
		if added || initial {
			rebuildPreviewSlice(s)
			e.notify()
		}
	}
}

func rebuildPreviewSlice(s *tileStroke) {
	s.preview = s.preview[:0]
	for _, cell := range s.previewMap {
		s.preview = append(s.preview, cell)
	}
	sortPreviewCells(s.preview)
}

func sortPreviewCells(cells []TilePreviewCell) {
	slices.SortFunc(cells, func(a, b TilePreviewCell) int {
		if a.Row != b.Row {
			return a.Row - b.Row
		}
		return a.Col - b.Col
	})
}

// pickAt picks the tile in the stroke layer's cell at the layer point (lx, ly), if the point is on the map.
func (e *Editor) pickAt(lx, ly float32) {
	s := e.stroke
	_, tm, err := e.resolveInlineTilemap(s.layer)
	if err != nil {
		return
	}
	col, row, ok := tileAt(tm, lx, ly)
	if !ok {
		s.pickedAt = -1
		return
	}
	idx := row*tm.Width + col
	if idx == s.pickedAt {
		return
	}
	s.pickedAt = idx
	if e.PickTile(s.layer, col, row) {
		s.picked = true
	}
}

// strokeSegment applies the paint, erase or terrain stroke under way to every cell of its layer that the segment from layer point (x0, y0) to (x1, y1) crosses, and rebuilds the scene when a cell changed.
func (e *Editor) strokeSegment(x0, y0, x1, y1 float32) {
	s := e.stroke
	_, tm, err := e.resolveInlineTilemap(s.layer)
	if err != nil {
		return
	}
	g, err := gridOf(tm, s.tileLayer)
	if err != nil {
		return
	}
	values := g.values()
	tw, th := float64(tm.TileWidth), float64(tm.TileHeight)
	ox, oy := float64(tm.OffsetX), float64(tm.OffsetY)
	var batch []tileChange
	if s.mode == TileStrokeTerrain || s.mode == TileStrokeEraseTerrain {
		placed := s.mode == TileStrokeTerrain
		cellsAlong((float64(x0)-ox)/tw, (float64(y0)-oy)/th, (float64(x1)-ox)/tw, (float64(y1)-oy)/th, tm.Width, tm.Height, func(col, row int) {
			idx := row*tm.Width + col
			if s.visited[idx] {
				return
			}
			s.visited[idx] = true
			step := applyTerrainCell(values, tm.Width, tm.Height, col, row, placed, s.terrain)
			for _, ch := range step {
				s.recordChange(ch.index, ch.oldGID, ch.newGID)
				batch = append(batch, ch)
			}
		})
	} else {
		val := s.value
		if s.mode == TileStrokeErase {
			val = 0
		}
		cellsAlong((float64(x0)-ox)/tw, (float64(y0)-oy)/th, (float64(x1)-ox)/tw, (float64(y1)-oy)/th, tm.Width, tm.Height, func(col, row int) {
			idx := row*tm.Width + col
			if _, seen := s.changeIndex[idx]; seen || values[idx] == val {
				return
			}
			ch := tileChange{index: idx, oldGID: values[idx], newGID: val}
			s.recordChange(idx, ch.oldGID, ch.newGID)
			batch = append(batch, ch)
		})
	}
	if len(batch) > 0 {
		g.apply(batch, false)
		e.Rebuild()
		e.notify()
	}
}

// tileAt returns the cell of tm at the layer point (lx, ly), and false off the map.
func tileAt(tm *sceneformat.TilemapDoc, lx, ly float32) (col, row int, ok bool) {
	if tm == nil || tm.TileWidth <= 0 || tm.TileHeight <= 0 || tm.Width <= 0 || tm.Height <= 0 {
		return 0, 0, false
	}
	rx := float64(lx) - float64(tm.OffsetX)
	ry := float64(ly) - float64(tm.OffsetY)
	if !finite(rx) || !finite(ry) || rx < 0 || ry < 0 {
		return 0, 0, false
	}
	if rx >= float64(tm.Width)*float64(tm.TileWidth) || ry >= float64(tm.Height)*float64(tm.TileHeight) {
		return 0, 0, false
	}
	col = int(math.Floor(rx / float64(tm.TileWidth)))
	row = int(math.Floor(ry / float64(tm.TileHeight)))
	if col < 0 || col >= tm.Width || row < 0 || row >= tm.Height {
		return 0, 0, false
	}
	return col, row, true
}

// cellsAlong calls visit for every cell of a w by h grid of unit cells that the segment from (x0, y0) to (x1, y1), in cell units, crosses, in order from the start, each once: the segment is clipped to the grid, and each cell after the first shares a side with the one before, so a line drawn through a corner takes one of the two cells beside it rather than leaving a diagonal gap.
func cellsAlong(x0, y0, x1, y1 float64, w, h int, visit func(col, row int)) {
	if w <= 0 || h <= 0 || !finite(x0) || !finite(y0) || !finite(x1) || !finite(y1) {
		return
	}
	// Liang-Barsky clipping to [0, w] x [0, h].
	t0, t1 := 0.0, 1.0
	dx, dy := x1-x0, y1-y0
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
	if !clip(-dx, x0) || !clip(dx, float64(w)-x0) || !clip(-dy, y0) || !clip(dy, float64(h)-y0) {
		return
	}
	// An end the clip leaves in place is kept exactly, as x0+1*(x1-x0) need not round back to x1.
	ax, ay, bx, by := x0, y0, x1, y1
	if t0 > 0 {
		ax, ay = x0+t0*dx, y0+t0*dy
	}
	if t1 < 1 {
		bx, by = x0+t1*dx, y0+t1*dy
	}
	cell := func(v float64, n int) int { return min(max(int(math.Floor(v)), 0), n-1) }
	col, row := cell(ax, w), cell(ay, h)
	endCol, endRow := cell(bx, w), cell(by, h)
	visit(col, row)

	// Amanatides-Woo traversal: tMaxX and tMaxY are how far along the clipped segment it next crosses a vertical and a horizontal grid line.
	sdx, sdy := bx-ax, by-ay
	stepX, stepY := sign(endCol-col), sign(endRow-row)
	tMaxX, tDeltaX := math.Inf(1), math.Inf(1)
	if stepX != 0 && sdx != 0 {
		next := float64(col + max(stepX, 0))
		tMaxX, tDeltaX = (next-ax)/sdx, math.Abs(1/sdx)
	}
	tMaxY, tDeltaY := math.Inf(1), math.Inf(1)
	if stepY != 0 && sdy != 0 {
		next := float64(row + max(stepY, 0))
		tMaxY, tDeltaY = (next-ay)/sdy, math.Abs(1/sdy)
	}
	for col != endCol || row != endRow {
		// Once one axis has reached the end cell only the other steps, so rounding never carries the walk past it.
		if row == endRow || col != endCol && tMaxX <= tMaxY {
			col += stepX
			tMaxX += tDeltaX
		} else {
			row += stepY
			tMaxY += tDeltaY
		}
		visit(col, row)
	}
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// SetTileHover records that the mouse rests at the canvas point (x, y), which the hover outline of a tile tool follows (HoveredTile); picking says it would pick rather than paint there, as with Alt held. The viewport redraws only when the cell under the pointer changes.
func (e *Editor) SetTileHover(x, y float32, picking bool) {
	if e == nil {
		return
	}
	col, row, ok := e.HoveredTile()
	was := e.hover.picking
	e.hover = tileHover{on: true, x: x, y: y, picking: picking}
	if c, r, k := e.HoveredTile(); c != col || r != row || k != ok || was != picking {
		e.notify()
	}
}

// ClearTileHover records that the mouse left the viewport, which hides the hover outline.
func (e *Editor) ClearTileHover() {
	if e == nil || !e.hover.on {
		return
	}
	_, _, ok := e.HoveredTile()
	e.hover = tileHover{}
	if ok {
		e.notify()
	}
}

// HoveredTile returns the cell of the active layer's inline tilemap under the mouse (SetTileHover), which the viewport outlines, snapped to the grid. It reports false while no tile tool is active, in 3D, while playing, when the mouse is not over the viewport and when the cell under it is off the map.
func (e *Editor) HoveredTile() (col, row int, ok bool) {
	if e == nil || !e.hover.on || e.playing || e.ViewMode != ViewMode2D || !isTileTool(e.Tool) {
		return 0, 0, false
	}
	return e.CanvasToTile(e.ActiveLayer, e.hover.x, e.hover.y)
}

// hoverMode is the stroke a press would start, or the one under way, which picks the hover outline's look.
func (e *Editor) hoverMode() TileStrokeMode {
	if s := e.stroke; s != nil {
		return s.mode
	}
	if e.hover.picking {
		return TileStrokePick
	}
	switch e.Tool {
	case ToolEraseTile:
		return TileStrokeErase
	case ToolPickTile:
		return TileStrokePick
	case ToolTerrain:
		return TileStrokeTerrain
	}
	return TileStrokePaint
}

// toolStrokeMode returns the stroke a pointer going down starts with the active tool, and false for none:
// Alt picks with any tile tool; a secondary (right) button erases with Paint, Erase, Terrain, Rect, Line and Random,
// and selects a map region with Stamp; primary runs each tool's brush. Fill starts no stroke, as it fills on a press.
func (e *Editor) toolStrokeMode(secondary, alt bool) (TileStrokeMode, bool) {
	if !isTileTool(e.Tool) {
		return 0, false
	}
	if alt {
		return TileStrokePick, true
	}
	switch e.Tool {
	case ToolPaintTile:
		if secondary {
			return TileStrokeErase, true
		}
		return TileStrokePaint, true
	case ToolEraseTile:
		return TileStrokeErase, true
	case ToolPickTile:
		if !secondary {
			return TileStrokePick, true
		}
	case ToolTerrain:
		if secondary {
			return TileStrokeEraseTerrain, true
		}
		return TileStrokeTerrain, true
	case ToolRectTile:
		if secondary {
			return TileStrokeRectErase, true
		}
		return TileStrokeRect, true
	case ToolLineTile:
		if secondary {
			return TileStrokeLineErase, true
		}
		return TileStrokeLine, true
	case ToolStampTile:
		if secondary {
			return TileStrokeSelectRegion, true
		}
		return TileStrokeStamp, true
	case ToolRandomTile:
		if secondary {
			return TileStrokeErase, true
		}
		return TileStrokeRandom, true
	}
	return 0, false
}

// The hover outline's colors: sky for painting and filling, emerald for terrain, red for erasing, amber for picking.
var (
	hoverPaintColor   = core.RGBA(0x38BDF8, 1)
	hoverTerrainColor = core.RGBA(0x34D399, 1)
	hoverEraseColor   = core.RGBA(0xF87171, 1)
	hoverPickColor    = core.RGBA(0xFBBF24, 1)
)

// drawTileHoverOverlay draws the active tile selection marquee, any live brush preview under way,
// or the hover outline for the cell under the mouse (HoveredTile) on the active layer.
func (e *Editor) drawTileHoverOverlay(d *core.Drawing) {
	e.drawTileSelectionOverlay(d)
	if e.drawTilePreviewOverlay(d) {
		return
	}
	col, row, ok := e.HoveredTile()
	if !ok {
		return
	}
	layer, tm, err := e.resolveInlineTilemap(e.ActiveLayer)
	if err != nil {
		return
	}
	space, px, py := e.layerSpaceAndParallax(layer)
	m := e.layerAffineFor(space, px, py)
	x, y := tm.OffsetX+float32(col)*float32(tm.TileWidth), tm.OffsetY+float32(row)*float32(tm.TileHeight)
	w, h := float32(tm.TileWidth), float32(tm.TileHeight)
	lw := 1.5 / strokeScale(m)
	mode := e.hoverMode()
	d.Save()
	applyAffine(d, m)
	if e.Tool == ToolStampTile && mode == TileStrokePaint {
		e.drawStampHoverOverlay(d, tm, col, row, lw)
		d.Restore()
		return
	}
	color := hoverPaintColor
	switch mode {
	case TileStrokePaint:
		if e.Tool == ToolRandomTile {
			if tid := randomPickTile(e.randomSeed, col, row, e.effectiveRandomPool()); tid > 0 {
				e.drawGhostTileID(d, tm, tid, e.ActiveTileFlip, x, y, nil)
			}
		} else {
			e.drawGhostTile(d, tm, x, y)
		}
	case TileStrokeTerrain:
		color = hoverTerrainColor
		e.drawGhostTerrainTile(d, layer, col, row, x, y)
	case TileStrokeErase, TileStrokeEraseTerrain:
		color = hoverEraseColor
		d.SetFillColor(core.RGBA(0xF87171, 0.25)).FillRect(x, y, w, h)
	case TileStrokePick:
		color = hoverPickColor
	}
	d.SetStrokeColor(color).SetLineWidth(lw).StrokeRect(x, y, w, h)
	d.Restore()
}

// drawGhostTerrainTile draws the tile ToolTerrain would place at (col, row) faded at layer point (x, y).
func (e *Editor) drawGhostTerrainTile(d *core.Drawing, layer string, col, row int, x, y float32) {
	_, _, g, _, t, err := e.resolveTerrainForLayer(layer, e.ActiveTerrain)
	if err != nil || t == nil {
		return
	}
	values := g.values()
	w, h := g.tm.Width, g.tm.Height
	if w <= 0 || h <= 0 || len(values) != w*h {
		return
	}
	match := func(c, r int) bool {
		if c == col && r == row {
			return true
		}
		if c < 0 || r < 0 || c >= w || r >= h {
			return false
		}
		return t.Contains(cellGID(values[r*w+c]))
	}
	gid := int(t.Autotile.Tile(t.Autotile.ComputeMask(match, col, row)) & tilemap.TileIDMask)
	if gid <= 0 {
		return
	}
	tile, ok := e.TilePalette().Tile(gid)
	if !ok || tile.Frame.W <= 0 || tile.Frame.H <= 0 {
		return
	}
	f := tile.Frame
	d.Save().SetAlpha(0.5).DrawImageFrom(tile.Image, f.X, f.Y, f.W, f.H, x+f.OffsetX, y+f.OffsetY, f.W, f.H).Restore()
}

// drawGhostTile draws the active tile faded at layer point (x, y), as the tilemap would draw it there; a tile the tileset lacks draws nothing.
func (e *Editor) drawGhostTile(d *core.Drawing, tm *sceneformat.TilemapDoc, x, y float32) {
	if e.ActiveTileID <= 0 || e.Resolver == nil || (e.Tool != ToolPaintTile && e.Tool != ToolFillTile && e.Tool != ToolRectTile && e.Tool != ToolLineTile) {
		return
	}
	e.drawGhostTileID(d, tm, e.ActiveTileID, e.ActiveTileFlip, x, y, nil)
}
