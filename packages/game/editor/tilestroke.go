package editor

import (
	"fmt"
	"math"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
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
	default:
		return "Unknown"
	}
}

// tileStroke is a tile stroke under way: the layer it edits, the tile layer of a layered grid (empty for a single grid), the tile value it writes, where the pointer last was in the layer's space, and the cells it has changed so far, each once, with the value the cell held before the stroke.
type tileStroke struct {
	mode      TileStrokeMode
	layer     string
	tileLayer string
	value     int64
	lastX     float32
	lastY     float32
	changes   []tileChange
	changed   map[int]bool
	// picked says a pick stroke picked a tile, and pickedAt is the cell it last looked at (index into the map's grid, -1 for none), which it does not look at again until the pointer leaves it.
	picked   bool
	pickedAt int
}

// tileHover is where the mouse rests over the viewport's canvas, and whether it would pick there (Alt held), which the hover outline (HoveredTile) is drawn from.
type tileHover struct {
	on      bool
	x, y    float32
	picking bool
}

// BeginTileStroke starts a stroke of mode on the active layer's inline tilemap at the canvas point (x, y), and applies it to the cell there: a paint stroke writes ActiveTileID, an erase stroke clears the cell and a pick stroke makes the cell's tile the active tile. ContinueTileStroke carries the stroke on to each point the pointer moves to, and EndTileStroke records what it changed as one undo step. A stroke that starts off the map applies nothing until it reaches the map.
//
// It returns false and starts nothing while playing, in 3D, while another stroke is under way, when the active layer has no inline tilemap to edit, for a paint or erase stroke on a locked or hidden layer (reporting why in the status bar), and for a paint stroke whose ActiveTileID is negative.
func (e *Editor) BeginTileStroke(mode TileStrokeMode, x, y float32) bool {
	if e == nil || e.playing || e.ViewMode != ViewMode2D || e.stroke != nil {
		return false
	}
	if mode < TileStrokePaint || mode > TileStrokePick || mode == TileStrokePaint && e.ActiveTileID < 0 {
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
	if mode == TileStrokePaint {
		if val, err = e.paintValue(g, e.ActiveTileID); err != nil {
			return false
		}
	}
	if mode != TileStrokePick {
		if err := e.lockedLayerErr(); err != nil {
			e.report(err)
			return false
		}
		if !e.layerDrawn(layer) {
			e.report(fmt.Errorf("editor: layer %q is hidden", layer))
			return false
		}
	}
	lx, ly := e.layerCoordsForActive(x, y)
	e.stroke = &tileStroke{mode: mode, layer: layer, tileLayer: tileLayer, value: val, lastX: lx, lastY: ly, changed: map[int]bool{}, pickedAt: -1}
	if mode == TileStrokePick {
		e.pickAt(lx, ly)
		return true
	}
	e.strokeSegment(lx, ly, lx, ly)
	return true
}

// ContinueTileStroke carries the stroke under way on from the pointer's last point to the canvas point (x, y): a paint or erase stroke applies to every cell the straight line between them crosses, so a fast move leaves no gaps, and a pick stroke picks the tile in the cell at (x, y). It does nothing when no stroke is under way.
func (e *Editor) ContinueTileStroke(x, y float32) {
	if e == nil || e.stroke == nil {
		return
	}
	s := e.stroke
	lx, ly := e.layerCoordsForActive(x, y)
	if s.mode == TileStrokePick {
		e.pickAt(lx, ly)
	} else {
		e.strokeSegment(s.lastX, s.lastY, lx, ly)
	}
	s.lastX, s.lastY = lx, ly
}

// EndTileStroke ends the stroke under way, recording the cells it changed as one undo step ("Paint 12 tiles"); a stroke that changed nothing records none. A pick stroke made with the Pick tool that picked a tile makes Paint the active tool, so the picked tile paints next. It does nothing when no stroke is under way.
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
	if len(s.changes) == 0 {
		return
	}
	label := fmt.Sprintf("%s %d tiles", s.mode, len(s.changes))
	if len(s.changes) == 1 {
		idx := s.changes[0].index
		if _, tm, err := e.resolveInlineTilemap(s.layer); err == nil && tm.Width > 0 {
			label = fmt.Sprintf("%s tile (%d,%d)", s.mode, idx%tm.Width, idx/tm.Width)
		}
	}
	// The cells already hold the stroke's tiles, so Do writes what is there and History records the step.
	e.History.Execute(&tileEditCmd{ed: e, layerName: s.layer, tileLayer: s.tileLayer, label: label, changes: s.changes})
}

// CancelTileStroke ends the stroke under way without recording it, putting back every cell it changed, as when a second finger turns a one-finger paint into a two-finger pan. It does nothing when no stroke is under way.
func (e *Editor) CancelTileStroke() {
	if e == nil || e.stroke == nil {
		return
	}
	s := e.stroke
	e.stroke = nil
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

// strokeSegment applies the paint or erase stroke under way to every cell of its layer that the segment from layer point (x0, y0) to (x1, y1) crosses, and rebuilds the scene when a cell changed.
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
	val := s.value
	if s.mode == TileStrokeErase {
		val = 0
	}
	values := g.values()
	tw, th := float64(tm.TileWidth), float64(tm.TileHeight)
	var batch []tileChange
	cellsAlong(float64(x0)/tw, float64(y0)/th, float64(x1)/tw, float64(y1)/th, tm.Width, tm.Height, func(col, row int) {
		idx := row*tm.Width + col
		if s.changed[idx] || values[idx] == val {
			return
		}
		s.changed[idx] = true
		ch := tileChange{index: idx, oldGID: values[idx], newGID: val}
		s.changes = append(s.changes, ch)
		batch = append(batch, ch)
	})
	if len(batch) > 0 {
		g.apply(batch, false)
		e.Rebuild()
		e.notify()
	}
}

// tileAt returns the cell of tm at the layer point (lx, ly), and false off the map.
func tileAt(tm *sceneformat.TilemapDoc, lx, ly float32) (col, row int, ok bool) {
	if tm.TileWidth <= 0 || tm.TileHeight <= 0 || lx < 0 || ly < 0 {
		return 0, 0, false
	}
	col, row = int(lx)/tm.TileWidth, int(ly)/tm.TileHeight
	if col >= tm.Width || row >= tm.Height {
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

// HoveredTile returns the cell of the active layer's inline tilemap under the mouse (SetTileHover), which the viewport outlines, snapped to the grid. It reports false while no tile tool (Paint, Erase, Fill or Pick) is active, in 3D, while playing, when the mouse is not over the viewport and when the cell under it is off the map.
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
	}
	return TileStrokePaint
}

// toolStrokeMode returns the stroke a pointer going down starts with the active tool, and false for none: Alt picks with any tile tool, a secondary (right) button erases with Paint and Erase, and otherwise Paint paints, Erase erases and Pick picks. Fill starts no stroke, as it fills on a press.
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
	}
	return 0, false
}

// The hover outline's colors: sky for painting and filling, red for erasing, amber for picking.
var (
	hoverPaintColor = core.RGBA(0x38BDF8, 1)
	hoverEraseColor = core.RGBA(0xF87171, 1)
	hoverPickColor  = core.RGBA(0xFBBF24, 1)
)

// drawTileHoverOverlay outlines the cell under the mouse (HoveredTile) on the active layer: painting and filling show the active tile in it, faded, erasing tints it red and picking outlines it in amber.
func (e *Editor) drawTileHoverOverlay(d *core.Drawing) {
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
	x, y := float32(col*tm.TileWidth), float32(row*tm.TileHeight)
	w, h := float32(tm.TileWidth), float32(tm.TileHeight)
	mode := e.hoverMode()
	d.Save()
	applyAffine(d, m)
	color := hoverPaintColor
	switch mode {
	case TileStrokePaint:
		e.drawGhostTile(d, tm, x, y)
	case TileStrokeErase:
		color = hoverEraseColor
		d.SetFillColor(core.RGBA(0xF87171, 0.25)).FillRect(x, y, w, h)
	case TileStrokePick:
		color = hoverPickColor
	}
	d.SetStrokeColor(color).SetLineWidth(1.5/strokeScale(m)).StrokeRect(x, y, w, h)
	d.Restore()
}

// drawGhostTile draws the active tile faded at layer point (x, y), as the tilemap would draw it there; a tile the tileset lacks draws nothing.
func (e *Editor) drawGhostTile(d *core.Drawing, tm *sceneformat.TilemapDoc, x, y float32) {
	if e.ActiveTileID <= 0 || e.Resolver == nil || (e.Tool != ToolPaintTile && e.Tool != ToolFillTile) {
		return
	}
	if tm.IsLayered() {
		tile, ok := e.TilePalette().Tile(e.ActiveTileID)
		if !ok || tile.Frame.W <= 0 || tile.Frame.H <= 0 {
			return
		}
		f := tile.Frame
		d.Save().SetAlpha(0.5).DrawImageFrom(tile.Image, f.X, f.Y, f.W, f.H, x+f.OffsetX, y+f.OffsetY, f.W, f.H).Restore()
		return
	}
	atlas, err := e.Resolver.Atlas(tm.Tileset)
	if err != nil || atlas == nil {
		return
	}
	f, ok := atlas.Frames[strconv.Itoa(e.ActiveTileID)]
	if !ok {
		return
	}
	sp := sprite.NewSprite(f)
	sp.X, sp.Y, sp.Alpha = x, y, 0.5
	(&sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&sp}}).Draw(d)
}
