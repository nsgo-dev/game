package editor

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tilemap"
)

// tileChange is one cell a tile command changes, at index row*width+col of its grid, with the tile value before and after: a single grid's tile ID, or a layered grid's cell as sceneformat.TileLayerDoc.Cell returns it.
type tileChange struct {
	index  int
	oldGID int64
	newGID int64
}

// tileGrid is the grid of an inline tilemap that the tile tools change: a single grid's data, or one tile layer of a layered grid.
type tileGrid struct {
	tm *sceneformat.TilemapDoc
	// layer is the layered grid's tile layer, nil for a single grid.
	layer *sceneformat.TileLayerDoc
}

// get returns the tile value at index i of the grid.
func (g tileGrid) get(i int) int64 {
	if g.layer == nil {
		return int64(g.tm.Data[i])
	}
	// The document validates, so every cell parses.
	v, _ := g.layer.Cell(i%g.tm.Width, i/g.tm.Width)
	return int64(v)
}

// values returns every tile value of the grid, row by row.
func (g tileGrid) values() []int64 {
	out := make([]int64, g.tm.Width*g.tm.Height)
	if g.layer == nil {
		for i, v := range g.tm.Data {
			out[i] = int64(v)
		}
		return out
	}
	cells, _ := g.layer.Cells()
	for i, v := range cells {
		if i < len(out) {
			out[i] = int64(v)
		}
	}
	return out
}

// apply writes each change's new tile value, or its old one when undo is set, into the grid. A layered grid's rows are parsed and written once each, however many of their cells change.
func (g tileGrid) apply(changes []tileChange, undo bool) {
	n := g.tm.Width * g.tm.Height
	var cells []uint32
	var dirty map[int]bool
	if g.layer != nil {
		cells, _ = g.layer.Cells()
		if len(cells) != n {
			return
		}
		dirty = make(map[int]bool)
	}
	for _, ch := range changes {
		if ch.index < 0 || ch.index >= n {
			continue
		}
		v := ch.newGID
		if undo {
			v = ch.oldGID
		}
		if g.layer == nil {
			g.tm.Data[ch.index] = int(v)
			continue
		}
		cells[ch.index] = uint32(v)
		dirty[ch.index/g.tm.Width] = true
	}
	for r := range dirty {
		g.layer.Rows[r] = sceneformat.FormatTileRow(cells[r*g.tm.Width : (r+1)*g.tm.Width])
	}
}

// resolveInlineTilemap finds the inline TilemapDoc for layerName (or ActiveLayer / first tilemap layer when empty).
// When the layer references a top-level inline tilemap via Ref, it resolves to that TilemapDoc.
func (e *Editor) resolveInlineTilemap(layerName string) (string, *sceneformat.TilemapDoc, error) {
	if e.Doc == nil || len(e.Doc.Layers) == 0 {
		return "", nil, fmt.Errorf("editor: scene has no layers")
	}

	targetName := layerName
	if targetName == "" {
		// Prefer ActiveLayer if it has an inline tilemap, otherwise fall back to the first layer with one.
		if li := e.layerIndex(e.ActiveLayer); li >= 0 && e.inlineTilemapForLayer(&e.Doc.Layers[li]) != nil {
			targetName = e.Doc.Layers[li].Name
		} else {
			for i := range e.Doc.Layers {
				if e.inlineTilemapForLayer(&e.Doc.Layers[i]) != nil {
					targetName = e.Doc.Layers[i].Name
					break
				}
			}
		}
	}
	if targetName == "" {
		return "", nil, fmt.Errorf("editor: no layer with an inline tilemap found")
	}
	li := e.layerIndex(targetName)
	if li < 0 {
		return "", nil, fmt.Errorf("editor: layer %q not found", targetName)
	}
	tm := e.inlineTilemapForLayer(&e.Doc.Layers[li])
	if tm == nil {
		return "", nil, fmt.Errorf("editor: layer %q has no inline tilemap", targetName)
	}
	return targetName, tm, nil
}

func (e *Editor) inlineTilemapForLayer(l *sceneformat.LayerDoc) *sceneformat.TilemapDoc {
	if l == nil || l.Tilemap == nil {
		return nil
	}
	tm := l.Tilemap
	if tm.IsRef() {
		tm = e.Doc.Tilemaps[tm.Ref]
	}
	if tm == nil || !tm.IsInline() {
		return nil
	}
	return tm
}

// tileLayerName returns the name of the tile layer of the layered grid tm that the tile tools change: ActiveTileLayer when tm has it, otherwise its first.
func (e *Editor) tileLayerName(tm *sceneformat.TilemapDoc) string {
	if tm.TileLayer(e.ActiveTileLayer) != nil {
		return e.ActiveTileLayer
	}
	if len(tm.TileLayers) > 0 {
		return tm.TileLayers[0].Name
	}
	return ""
}

// gridOf returns the grid of tm that the tile layer named tileLayer is, or tm's data for a single grid, refusing a grid whose size does not match tm's.
func gridOf(tm *sceneformat.TilemapDoc, tileLayer string) (tileGrid, error) {
	if !tm.IsLayered() {
		if len(tm.Data) != tm.Width*tm.Height {
			return tileGrid{}, fmt.Errorf("editor: tilemap data length %d does not match %dx%d", len(tm.Data), tm.Width, tm.Height)
		}
		return tileGrid{tm: tm}, nil
	}
	l := tm.TileLayer(tileLayer)
	if l == nil {
		return tileGrid{}, fmt.Errorf("editor: tilemap has no tile layer %q", tileLayer)
	}
	if len(l.Rows) != tm.Height {
		return tileGrid{}, fmt.Errorf("editor: tile layer %q has %d rows, not %d", tileLayer, len(l.Rows), tm.Height)
	}
	return tileGrid{tm: tm, layer: l}, nil
}

// tileTarget resolves the grid the tile tools change on layer (or the active one): the layer's name, the tile layer's name (empty for a single grid) and the grid, checking that (col, row) is inside it.
func (e *Editor) tileTarget(layer string, col, row int) (string, string, tileGrid, error) {
	layerName, tm, err := e.resolveInlineTilemap(layer)
	if err != nil {
		return "", "", tileGrid{}, err
	}
	if col < 0 || col >= tm.Width || row < 0 || row >= tm.Height {
		return "", "", tileGrid{}, fmt.Errorf("editor: tile (%d, %d) out of bounds (%dx%d)", col, row, tm.Width, tm.Height)
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

// paintValue returns the tile value painting tileID writes into g: the ID itself in a single grid, and in a layered grid the cell of tile tileID-1 with ActiveTileFlip, or an empty cell for 0. It refuses an ID that a layered grid's cells cannot hold.
func (e *Editor) paintValue(g tileGrid, tileID int) (int64, error) {
	if tileID < 0 {
		return 0, fmt.Errorf("editor: tileID %d must be non-negative", tileID)
	}
	if g.layer == nil || tileID == 0 {
		return int64(tileID), nil
	}
	if tileID > int(tilemap.TileIDMask) {
		return 0, fmt.Errorf("editor: tileID %d is above %d", tileID, tilemap.TileIDMask)
	}
	return int64(uint32(tileID) | e.ActiveTileFlip&tilemap.FlipMask), nil
}

// PaintTile sets the tile at (col, row) on layer's inline tilemap to tileID as an undoable command. On a layered grid it paints the tile layer ActiveTileLayer names, and tileID is the tile's local ID in the tileset plus 1, as the tile palette numbers tiles, placed with the flips ActiveTileFlip holds; 0 empties the cell.
func (e *Editor) PaintTile(layer string, col, row, tileID int) error {
	if e.playing {
		return ErrPlaying
	}
	if tileID < 0 {
		return fmt.Errorf("editor: tileID %d must be non-negative", tileID)
	}
	layerName, tileLayer, g, err := e.tileTarget(layer, col, row)
	if err != nil {
		return err
	}
	v, err := e.paintValue(g, tileID)
	if err != nil {
		return err
	}
	e.exitTileReviewBeforePreview()
	idx := row*g.tm.Width + col
	old := g.get(idx)
	if old == v {
		return nil
	}
	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		tileLayer: tileLayer,
		label:     fmt.Sprintf("Paint tile (%d,%d)", col, row),
		changes:   []tileChange{{index: idx, oldGID: old, newGID: v}},
	})
	return nil
}

// EraseTile clears the tile at (col, row) (setting its ID to 0) on layer's inline tilemap as an undoable command, on a layered grid in the tile layer ActiveTileLayer names.
func (e *Editor) EraseTile(layer string, col, row int) error {
	if e.playing {
		return ErrPlaying
	}
	layerName, tileLayer, g, err := e.tileTarget(layer, col, row)
	if err != nil {
		return err
	}
	e.exitTileReviewBeforePreview()
	idx := row*g.tm.Width + col
	old := g.get(idx)
	if old == 0 {
		return nil
	}
	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		tileLayer: tileLayer,
		label:     fmt.Sprintf("Erase tile (%d,%d)", col, row),
		changes:   []tileChange{{index: idx, oldGID: old, newGID: 0}},
	})
	return nil
}

// FloodFillTile runs a 4-connected BFS flood fill starting at (col, row), replacing all contiguous matching tiles with tileID in a single undoable command. On a layered grid it fills the tile layer ActiveTileLayer names, where tiles match when their IDs and flips both do, and paints tileID as PaintTile does.
func (e *Editor) FloodFillTile(layer string, col, row, tileID int) error {
	if e.playing {
		return ErrPlaying
	}
	if tileID < 0 {
		return fmt.Errorf("editor: tileID %d must be non-negative", tileID)
	}
	layerName, tileLayer, g, err := e.tileTarget(layer, col, row)
	if err != nil {
		return err
	}
	v, err := e.paintValue(g, tileID)
	if err != nil {
		return err
	}
	e.exitTileReviewBeforePreview()
	w, h := g.tm.Width, g.tm.Height
	values := g.values()

	startIdx := row*w + col
	target := values[startIdx]
	if target == v {
		return nil
	}

	type cell struct{ c, r int }
	visited := make([]bool, len(values))
	visited[startIdx] = true
	queue := []cell{{col, row}}
	var changes []tileChange

	dirs := [4]cell{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		idx := cur.r*w + cur.c
		changes = append(changes, tileChange{
			index:  idx,
			oldGID: target,
			newGID: v,
		})
		for _, d := range dirs {
			nc, nr := cur.c+d.c, cur.r+d.r
			if nc < 0 || nc >= w || nr < 0 || nr >= h {
				continue
			}
			nidx := nr*w + nc
			if visited[nidx] || values[nidx] != target {
				continue
			}
			visited[nidx] = true
			queue = append(queue, cell{nc, nr})
		}
	}

	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		tileLayer: tileLayer,
		label:     fmt.Sprintf("Flood fill %d tiles", len(changes)),
		changes:   changes,
	})
	return nil
}

// resolveTerrainForLayer resolves the layered grid and runtime *tilemap.Terrain for terrainName
// (or ActiveTerrain / the tileset's first terrain when empty) on layer.
func (e *Editor) resolveTerrainForLayer(layer, terrainName string) (layerName, tileLayer string, g tileGrid, resolvedName string, terrain *tilemap.Terrain, err error) {
	layerName, tm, err := e.resolveInlineTilemap(layer)
	if err != nil {
		return "", "", tileGrid{}, "", nil, err
	}
	if !tm.IsLayered() {
		return "", "", tileGrid{}, "", nil, fmt.Errorf("editor: layer %q has no layered tilemap with a YAML tileset", layerName)
	}
	tileLayer = e.tileLayerName(tm)
	g, err = gridOf(tm, tileLayer)
	if err != nil {
		return "", "", tileGrid{}, "", nil, err
	}
	doc, err := e.loadTilesetDoc(tm.TilesetFile)
	if err != nil {
		return "", "", tileGrid{}, "", nil, fmt.Errorf("editor: tileset %q: %w", tm.TilesetFile, err)
	}
	name := terrainName
	if name == "" {
		name = e.ActiveTerrain
	}
	if name == "" && len(doc.Terrains) > 0 {
		name = doc.Terrains[0].Name
	}
	if name == "" {
		return "", "", tileGrid{}, "", nil, fmt.Errorf("editor: tileset %q defines no terrain sets", tm.TilesetFile)
	}
	t := doc.Terrain(name, 1)
	if t == nil {
		return "", "", tileGrid{}, "", nil, fmt.Errorf("editor: tileset %q has no terrain %q", tm.TilesetFile, name)
	}
	if t.Autotile.Default == 0 && len(t.Autotile.Tiles) == 0 {
		return "", "", tileGrid{}, "", nil, fmt.Errorf("editor: terrain %q has no tiles mapped", name)
	}
	return layerName, tileLayer, g, name, t, nil
}

func cellGID(v int64) uint32 {
	if v <= 0 {
		return 0
	}
	return uint32(v & int64(tilemap.TileIDMask))
}

// applyTerrainCell places (placed=true) or removes (placed=false) terrain t at (col, row) in values (w x h)
// and updates that cell and its 8 surrounding terrain neighbors using the exact same rules as tilemap.Tilemap.SetAutotile.
// It updates values in place and returns only the cells whose tile value changed.
func applyTerrainCell(values []int64, w, h, col, row int, placed bool, t *tilemap.Terrain) []tileChange {
	if t == nil || col < 0 || row < 0 || col >= w || row >= h || len(values) != w*h {
		return nil
	}
	inBounds := func(c, r int) bool {
		return c >= 0 && r >= 0 && c < w && r < h
	}

	// Snapshot the 5x5 neighborhood around (col, row) so the target and its 8 neighbors all evaluate against consistent post-edit membership.
	var grid [5][5]bool
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			nc, nr := col+dx, row+dy
			if dx == 0 && dy == 0 {
				grid[dy+2][dx+2] = placed
				continue
			}
			if inBounds(nc, nr) {
				grid[dy+2][dx+2] = t.Contains(cellGID(values[nr*w+nc]))
			}
		}
	}

	match := func(c, r int) bool {
		dx, dy := c-col, r-row
		if dx >= -2 && dx <= 2 && dy >= -2 && dy <= 2 {
			return grid[dy+2][dx+2]
		}
		if !inBounds(c, r) {
			return false
		}
		return t.Contains(cellGID(values[r*w+c]))
	}

	var changes []tileChange
	setCell := func(c, r int, newGID int64) {
		idx := r*w + c
		oldGID := values[idx]
		if oldGID == newGID {
			return
		}
		values[idx] = newGID
		changes = append(changes, tileChange{index: idx, oldGID: oldGID, newGID: newGID})
	}

	if !placed {
		setCell(col, row, 0)
	} else {
		mask := t.Autotile.ComputeMask(match, col, row)
		setCell(col, row, int64(t.Autotile.Tile(mask)))
	}

	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nc, nr := col+dx, row+dy
			if !inBounds(nc, nr) || !grid[dy+2][dx+2] {
				continue
			}
			mask := t.Autotile.ComputeMask(match, nc, nr)
			setCell(nc, nr, int64(t.Autotile.Tile(mask)))
		}
	}
	return changes
}

// PaintTerrain places a cell of terrainName (or ActiveTerrain / the tileset's first terrain when empty) at (col, row)
// on layer's layered tilemap and updates the cell and its neighbors through the same autotile rules as tilemap.Tilemap.SetAutotile, as one undoable command.
func (e *Editor) PaintTerrain(layer string, col, row int, terrainName string) error {
	return e.editTerrainCell(layer, col, row, terrainName, true)
}

// EraseTerrain removes terrain at (col, row) on layer's layered tilemap and updates its neighbors through the same autotile rules as tilemap.Tilemap.SetAutotile, as one undoable command.
func (e *Editor) EraseTerrain(layer string, col, row int, terrainName string) error {
	return e.editTerrainCell(layer, col, row, terrainName, false)
}

func (e *Editor) editTerrainCell(layer string, col, row int, terrainName string, placed bool) error {
	if e == nil {
		return fmt.Errorf("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	layerName, tileLayer, g, name, t, err := e.resolveTerrainForLayer(layer, terrainName)
	if err != nil {
		return err
	}
	if col < 0 || col >= g.tm.Width || row < 0 || row >= g.tm.Height {
		return fmt.Errorf("editor: tile (%d, %d) out of bounds (%dx%d)", col, row, g.tm.Width, g.tm.Height)
	}
	e.exitTileReviewBeforePreview()
	values := g.values()
	changes := applyTerrainCell(values, g.tm.Width, g.tm.Height, col, row, placed, t)
	if len(changes) == 0 {
		return nil
	}
	verb := "Paint"
	if !placed {
		verb = "Erase"
	}
	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		tileLayer: tileLayer,
		label:     fmt.Sprintf("%s terrain %q (%d,%d)", verb, name, col, row),
		changes:   changes,
	})
	return nil
}

type tileEditCmd struct {
	ed        *Editor
	layerName string
	// tileLayer names the layered grid's tile layer the command changes, empty for a single grid.
	tileLayer string
	label     string
	changes   []tileChange
}

func (c *tileEditCmd) Label() string { return c.label }

// grid returns the grid the command changes, or false when its layer or tile layer is gone.
func (c *tileEditCmd) grid() (tileGrid, bool) {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return tileGrid{}, false
	}
	tm := c.ed.inlineTilemapForLayer(&c.ed.Doc.Layers[li])
	if tm == nil || tm.IsLayered() != (c.tileLayer != "") {
		return tileGrid{}, false
	}
	g, err := gridOf(tm, c.tileLayer)
	return g, err == nil
}

func (c *tileEditCmd) Do() {
	g, ok := c.grid()
	if !ok {
		return
	}
	g.apply(c.changes, false)
	c.ed.Status = c.label
}

func (c *tileEditCmd) Undo() {
	g, ok := c.grid()
	if !ok {
		return
	}
	g.apply(c.changes, true)
}
