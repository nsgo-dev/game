package editor

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

type tileChange struct {
	index  int
	oldGID int
	newGID int
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

func validateTileCoords(tm *sceneformat.TilemapDoc, col, row int) error {
	if col < 0 || col >= tm.Width || row < 0 || row >= tm.Height {
		return fmt.Errorf("editor: tile (%d, %d) out of bounds (%dx%d)", col, row, tm.Width, tm.Height)
	}
	if len(tm.Data) != tm.Width*tm.Height {
		return fmt.Errorf("editor: tilemap data length %d does not match %dx%d", len(tm.Data), tm.Width, tm.Height)
	}
	return nil
}

// PaintTile sets the tile at (col, row) on layer's inline tilemap to tileID as an undoable command.
func (e *Editor) PaintTile(layer string, col, row, tileID int) error {
	if e.playing {
		return ErrPlaying
	}
	if tileID < 0 {
		return fmt.Errorf("editor: tileID %d must be non-negative", tileID)
	}
	layerName, tm, err := e.resolveInlineTilemap(layer)
	if err != nil {
		return err
	}
	if err := validateTileCoords(tm, col, row); err != nil {
		return err
	}
	idx := row*tm.Width + col
	oldGID := tm.Data[idx]
	if oldGID == tileID {
		return nil
	}
	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		label:     fmt.Sprintf("Paint tile (%d,%d)", col, row),
		changes:   []tileChange{{index: idx, oldGID: oldGID, newGID: tileID}},
	})
	return nil
}

// EraseTile clears the tile at (col, row) (setting its ID to 0) on layer's inline tilemap as an undoable command.
func (e *Editor) EraseTile(layer string, col, row int) error {
	if e.playing {
		return ErrPlaying
	}
	layerName, tm, err := e.resolveInlineTilemap(layer)
	if err != nil {
		return err
	}
	if err := validateTileCoords(tm, col, row); err != nil {
		return err
	}
	idx := row*tm.Width + col
	oldGID := tm.Data[idx]
	if oldGID == 0 {
		return nil
	}
	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		label:     fmt.Sprintf("Erase tile (%d,%d)", col, row),
		changes:   []tileChange{{index: idx, oldGID: oldGID, newGID: 0}},
	})
	return nil
}

// FloodFillTile runs a 4-connected BFS flood fill starting at (col, row), replacing all contiguous
// matching tiles with tileID in a single undoable command.
func (e *Editor) FloodFillTile(layer string, col, row, tileID int) error {
	if e.playing {
		return ErrPlaying
	}
	if tileID < 0 {
		return fmt.Errorf("editor: tileID %d must be non-negative", tileID)
	}
	layerName, tm, err := e.resolveInlineTilemap(layer)
	if err != nil {
		return err
	}
	if err := validateTileCoords(tm, col, row); err != nil {
		return err
	}

	startIdx := row*tm.Width + col
	targetGID := tm.Data[startIdx]
	if targetGID == tileID {
		return nil
	}

	type cell struct{ c, r int }
	visited := make([]bool, len(tm.Data))
	visited[startIdx] = true
	queue := []cell{{col, row}}
	var changes []tileChange

	dirs := [4]cell{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		idx := cur.r*tm.Width + cur.c
		changes = append(changes, tileChange{
			index:  idx,
			oldGID: targetGID,
			newGID: tileID,
		})
		for _, d := range dirs {
			nc, nr := cur.c+d.c, cur.r+d.r
			if nc < 0 || nc >= tm.Width || nr < 0 || nr >= tm.Height {
				continue
			}
			nidx := nr*tm.Width + nc
			if visited[nidx] || tm.Data[nidx] != targetGID {
				continue
			}
			visited[nidx] = true
			queue = append(queue, cell{nc, nr})
		}
	}

	e.History.Execute(&tileEditCmd{
		ed:        e,
		layerName: layerName,
		label:     fmt.Sprintf("Flood fill %d tiles", len(changes)),
		changes:   changes,
	})
	return nil
}

type tileEditCmd struct {
	ed        *Editor
	layerName string
	label     string
	changes   []tileChange
}

func (c *tileEditCmd) Label() string { return c.label }

func (c *tileEditCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	tm := c.ed.inlineTilemapForLayer(&c.ed.Doc.Layers[li])
	if tm == nil {
		return
	}
	for _, ch := range c.changes {
		if ch.index >= 0 && ch.index < len(tm.Data) {
			tm.Data[ch.index] = ch.newGID
		}
	}
	c.ed.Status = c.label
}

func (c *tileEditCmd) Undo() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	tm := c.ed.inlineTilemapForLayer(&c.ed.Doc.Layers[li])
	if tm == nil {
		return
	}
	for _, ch := range c.changes {
		if ch.index >= 0 && ch.index < len(tm.Data) {
			tm.Data[ch.index] = ch.oldGID
		}
	}
}
