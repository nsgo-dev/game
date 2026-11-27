package editor

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
)

// maxTilemapCells matches sceneformat's per-tilemap cell cap so invalid sizes are refused cleanly before allocation.
const maxTilemapCells = 1 << 24

// atlasNames returns all atlas names advertised by the editor's Resolver, sorted and deduplicated.
func (e *Editor) atlasNames() []string {
	if e == nil || e.Resolver == nil {
		return nil
	}
	e.syncProjectAssets()
	var names []string
	switch r := e.Resolver.(type) {
	case sceneformat.AtlasMap:
		names = make([]string, 0, len(r))
		for name := range r {
			names = append(names, name)
		}
	case AtlasLister:
		names = append(names, r.AtlasNames()...)
	default:
		return nil
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// TileAtlasNames returns the names of atlases advertised by the editor's Resolver that contain at least one canonical positive integer tile frame ("1", "2", ...), in sorted order.
func (e *Editor) TileAtlasNames() []string {
	var out []string
	for _, name := range e.atlasNames() {
		atlas, err := e.Resolver.Atlas(name)
		if err != nil || atlas == nil {
			continue
		}
		if atlasHasTileFrame(atlas) {
			out = append(out, name)
		}
	}
	return out
}

func atlasHasTileFrame(atlas *sprite.Atlas) bool {
	if atlas == nil {
		return false
	}
	for name := range atlas.Frames {
		id, err := strconv.Atoi(name)
		if err == nil && id > 0 && strconv.Itoa(id) == name {
			return true
		}
	}
	return false
}

// defaultTileAtlas returns the first tile-capable atlas name, falling back to the first advertised atlas name, or "".
func (e *Editor) defaultTileAtlas() string {
	if tiles := e.TileAtlasNames(); len(tiles) > 0 {
		return tiles[0]
	}
	if all := e.atlasNames(); len(all) > 0 {
		return all[0]
	}
	return ""
}

// newTilemapRefName returns a free key in e.Doc.Tilemaps derived from layerName (layerName if unused, otherwise layerName_2, layerName_3, ...).
func (e *Editor) newTilemapRefName(layerName string) string {
	base := strings.TrimSpace(layerName)
	if base == "" {
		base = "tilemap"
	}
	if e == nil || e.Doc == nil || e.Doc.Tilemaps == nil {
		return base
	}
	if _, exists := e.Doc.Tilemaps[base]; !exists {
		return base
	}
	for n := 2; ; n++ {
		name := fmt.Sprintf("%s_%d", base, n)
		if _, exists := e.Doc.Tilemaps[name]; !exists {
			return name
		}
	}
}

// cloneTilemapDoc returns a deep copy of tm (or nil when tm is nil).
func cloneTilemapDoc(tm *sceneformat.TilemapDoc) *sceneformat.TilemapDoc {
	if tm == nil {
		return nil
	}
	doc := sceneformat.SceneDoc{Tilemaps: map[string]*sceneformat.TilemapDoc{"_": tm}}
	return doc.Clone().Tilemaps["_"]
}

// editableInlineTilemap resolves l's inline tilemap (direct or via Ref into e.Doc.Tilemaps).
// refName is non-empty when the returned TilemapDoc lives in e.Doc.Tilemaps[refName].
func (e *Editor) editableInlineTilemap(l *sceneformat.LayerDoc) (tm *sceneformat.TilemapDoc, refName string, err error) {
	if l == nil || l.Tilemap == nil {
		return nil, "", fmt.Errorf("editor: layer %q has no tilemap", l.Name)
	}
	tm = l.Tilemap
	if tm.IsRef() {
		refName = tm.Ref
		tm = e.Doc.Tilemaps[refName]
		if tm == nil {
			return nil, "", fmt.Errorf("editor: top-level tilemap %q not found", refName)
		}
	}
	if !tm.IsInline() {
		return nil, "", fmt.Errorf("editor: layer %q does not have an inline tilemap", l.Name)
	}
	return tm, refName, nil
}

// validateTilesetForGrid verifies that tileset resolves (when Resolver is set) and holds frames for every non-zero painted ID in data.
func (e *Editor) validateTilesetForGrid(tileset string, data []int) error {
	if tileset == "" {
		return errors.New("editor: tileset atlas name must not be empty")
	}
	if e.Resolver == nil {
		return nil
	}
	e.syncProjectAssets()
	atlas, err := e.Resolver.Atlas(tileset)
	if err != nil {
		return fmt.Errorf("editor: tileset %q cannot be resolved: %w", tileset, err)
	}
	if atlas == nil {
		return fmt.Errorf("editor: tileset %q resolved to nil atlas", tileset)
	}
	var missing []int
	seen := make(map[int]bool)
	for _, gid := range data {
		if gid <= 0 || seen[gid] {
			continue
		}
		seen[gid] = true
		if _, ok := atlas.Frames[strconv.Itoa(gid)]; !ok {
			missing = append(missing, gid)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	parts := make([]string, len(missing))
	for i, id := range missing {
		parts[i] = strconv.Itoa(id)
	}
	if len(parts) == 1 {
		return fmt.Errorf("editor: tileset %q is missing frame %q for painted tile", tileset, parts[0])
	}
	return fmt.Errorf("editor: tileset %q is missing frames %s for painted tiles", tileset, strings.Join(parts, ", "))
}

// AddLayerTilemap attaches a new empty inline single-grid tilemap to layerName (or ActiveLayer when empty) with tileset, tile size (tileWidth x tileHeight), and grid size (width x height) as an undoable command. It refuses a layer that already has a tilemap, an inherited base-owned layer, non-positive or overflowing sizes, an empty tileset name, or a tileset that the Resolver cannot resolve. When the added tilemap is on ActiveLayer, ActiveTileID is kept in the new tileset's palette.
func (e *Editor) AddLayerTilemap(layerName, tileset string, tileWidth, tileHeight, width, height int) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	if e.playing {
		return ErrPlaying
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return err
	}
	l := &e.Doc.Layers[li]
	if e.inheritedLayers()[l.Name] {
		return e.inheritedErr(l.Name, "tilemap")
	}
	if l.Tilemap != nil {
		return fmt.Errorf("editor: layer %q already has a tilemap", l.Name)
	}
	if tileWidth <= 0 || tileHeight <= 0 {
		return fmt.Errorf("editor: tile size (%dx%d) must be positive", tileWidth, tileHeight)
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("editor: grid size (%dx%d) must be positive", width, height)
	}
	if width > maxTilemapCells || height > maxTilemapCells || width > maxTilemapCells/height {
		return fmt.Errorf("editor: grid size %dx%d exceeds %d tiles", width, height, maxTilemapCells)
	}
	if err := e.validateTilesetForGrid(tileset, nil); err != nil {
		return err
	}
	tm := &sceneformat.TilemapDoc{
		TileWidth:  tileWidth,
		TileHeight: tileHeight,
		Width:      width,
		Height:     height,
		Tileset:    tileset,
		Data:       make([]int, width*height),
	}
	candidate := e.Doc.Clone()
	candidate.Layers[li].Tilemap = cloneTilemapDoc(tm)
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	e.History.Execute(&addLayerTilemapCmd{
		ed:        e,
		layerName: l.Name,
		tm:        tm,
		prevTile:  e.ActiveTileID,
	})
	return nil
}

type addLayerTilemapCmd struct {
	ed        *Editor
	layerName string
	tm        *sceneformat.TilemapDoc
	prevTile  int
}

func (c *addLayerTilemapCmd) Label() string { return "Add tilemap to " + c.layerName }

func (c *addLayerTilemapCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	c.ed.Doc.Layers[li].Tilemap = cloneTilemapDoc(c.tm)
	if c.ed.ActiveLayer == c.layerName {
		c.ed.keepTileInPalette()
	}
	c.ed.Status = fmt.Sprintf("Added %dx%d tilemap to layer %s", c.tm.Width, c.tm.Height, c.layerName)
}

func (c *addLayerTilemapCmd) Undo() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	c.ed.Doc.Layers[li].Tilemap = nil
	if c.ed.ActiveLayer == c.layerName {
		c.ed.ActiveTileID = c.prevTile
		c.ed.keepTileInPalette()
	}
}

// RemoveLayerTilemap removes the tilemap attached to layerName (or ActiveLayer when empty) as an undoable command. If the layer referenced a top-level tilemap via Ref, only the layer's reference is removed and the top-level entry in SceneDoc.Tilemaps is kept.
func (e *Editor) RemoveLayerTilemap(layerName string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	if e.playing {
		return ErrPlaying
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return err
	}
	l := &e.Doc.Layers[li]
	if e.inheritedLayers()[l.Name] {
		return e.inheritedErr(l.Name, "tilemap")
	}
	if l.Tilemap == nil {
		return fmt.Errorf("editor: layer %q has no tilemap", l.Name)
	}
	candidate := e.Doc.Clone()
	candidate.Layers[li].Tilemap = nil
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	e.History.Execute(&removeLayerTilemapCmd{
		ed:        e,
		layerName: l.Name,
		old:       cloneTilemapDoc(l.Tilemap),
		prevTile:  e.ActiveTileID,
	})
	return nil
}

type removeLayerTilemapCmd struct {
	ed        *Editor
	layerName string
	old       *sceneformat.TilemapDoc
	prevTile  int
}

func (c *removeLayerTilemapCmd) Label() string { return "Remove tilemap from " + c.layerName }

func (c *removeLayerTilemapCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	c.ed.Doc.Layers[li].Tilemap = nil
	c.ed.Status = "Removed tilemap from layer " + c.layerName
}

func (c *removeLayerTilemapCmd) Undo() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	c.ed.Doc.Layers[li].Tilemap = cloneTilemapDoc(c.old)
	if c.ed.ActiveLayer == c.layerName {
		c.ed.ActiveTileID = c.prevTile
		c.ed.keepTileInPalette()
	}
}

// SetLayerTilemapTileset changes the tileset atlas of layerName's inline single-grid tilemap (following Ref into SceneDoc.Tilemaps when referenced) as an undoable command. It refuses a layer without an inline single-grid tilemap, an inherited base-owned layer, an empty or unresolved tileset atlas, or an atlas missing frames for any painted tile ID in the grid.
func (e *Editor) SetLayerTilemapTileset(layerName, tileset string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	if e.playing {
		return ErrPlaying
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return err
	}
	l := &e.Doc.Layers[li]
	if e.inheritedLayers()[l.Name] {
		return e.inheritedErr(l.Name, "tilemap")
	}
	tm, refName, err := e.editableInlineTilemap(l)
	if err != nil {
		return err
	}
	if tm.IsLayered() {
		return fmt.Errorf("editor: layer %q uses a layered tilesetFile, not a tileset atlas", l.Name)
	}
	if tm.Tileset == tileset {
		return nil
	}
	if err := e.validateTilesetForGrid(tileset, tm.Data); err != nil {
		return err
	}
	candidate := e.Doc.Clone()
	if refName != "" {
		candidate.Tilemaps[refName].Tileset = tileset
	} else {
		candidate.Layers[li].Tilemap.Tileset = tileset
	}
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	e.History.Execute(&setLayerTilemapTilesetCmd{
		ed:         e,
		layerName:  l.Name,
		refName:    refName,
		oldTileset: tm.Tileset,
		newTileset: tileset,
		prevTile:   e.ActiveTileID,
	})
	return nil
}

type setLayerTilemapTilesetCmd struct {
	ed         *Editor
	layerName  string
	refName    string
	oldTileset string
	newTileset string
	prevTile   int
}

func (c *setLayerTilemapTilesetCmd) Label() string {
	return "Set tilemap tileset of " + c.layerName + " to " + c.newTileset
}

func (c *setLayerTilemapTilesetCmd) target() *sceneformat.TilemapDoc {
	if c.refName != "" {
		return c.ed.Doc.Tilemaps[c.refName]
	}
	if li := c.ed.layerIndex(c.layerName); li >= 0 {
		return c.ed.Doc.Layers[li].Tilemap
	}
	return nil
}

func (c *setLayerTilemapTilesetCmd) Do() {
	tm := c.target()
	if tm == nil {
		return
	}
	tm.Tileset = c.newTileset
	c.ed.keepTileInPalette()
	c.ed.Status = fmt.Sprintf("Set layer %s tileset to %s", c.layerName, c.newTileset)
}

func (c *setLayerTilemapTilesetCmd) Undo() {
	tm := c.target()
	if tm == nil {
		return
	}
	tm.Tileset = c.oldTileset
	c.ed.ActiveTileID = c.prevTile
	c.ed.keepTileInPalette()
}

// SetLayerTilemapTileSize changes the tile dimensions (tileWidth x tileHeight in points) of layerName's inline tilemap (following Ref into SceneDoc.Tilemaps when referenced) as an undoable command. It refuses non-positive sizes, layers without an inline tilemap, and inherited base-owned layers.
func (e *Editor) SetLayerTilemapTileSize(layerName string, tileWidth, tileHeight int) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	if e.playing {
		return ErrPlaying
	}
	if tileWidth <= 0 || tileHeight <= 0 {
		return fmt.Errorf("editor: tile size (%dx%d) must be positive", tileWidth, tileHeight)
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return err
	}
	l := &e.Doc.Layers[li]
	if e.inheritedLayers()[l.Name] {
		return e.inheritedErr(l.Name, "tilemap")
	}
	tm, refName, err := e.editableInlineTilemap(l)
	if err != nil {
		return err
	}
	if tm.TileWidth == tileWidth && tm.TileHeight == tileHeight {
		return nil
	}
	candidate := e.Doc.Clone()
	if refName != "" {
		candidate.Tilemaps[refName].TileWidth = tileWidth
		candidate.Tilemaps[refName].TileHeight = tileHeight
	} else {
		candidate.Layers[li].Tilemap.TileWidth = tileWidth
		candidate.Layers[li].Tilemap.TileHeight = tileHeight
	}
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	e.History.Execute(&setLayerTilemapTileSizeCmd{
		ed:        e,
		layerName: l.Name,
		refName:   refName,
		oldW:      tm.TileWidth,
		oldH:      tm.TileHeight,
		newW:      tileWidth,
		newH:      tileHeight,
	})
	return nil
}

type setLayerTilemapTileSizeCmd struct {
	ed         *Editor
	layerName  string
	refName    string
	oldW, oldH int
	newW, newH int
}

func (c *setLayerTilemapTileSizeCmd) Label() string {
	return fmt.Sprintf("Set tile size of %s to %dx%d", c.layerName, c.newW, c.newH)
}

func (c *setLayerTilemapTileSizeCmd) target() *sceneformat.TilemapDoc {
	if c.refName != "" {
		return c.ed.Doc.Tilemaps[c.refName]
	}
	if li := c.ed.layerIndex(c.layerName); li >= 0 {
		return c.ed.Doc.Layers[li].Tilemap
	}
	return nil
}

func (c *setLayerTilemapTileSizeCmd) Do() {
	tm := c.target()
	if tm == nil {
		return
	}
	tm.TileWidth = c.newW
	tm.TileHeight = c.newH
	c.ed.Status = fmt.Sprintf("Set layer %s tile size to %dx%d", c.layerName, c.newW, c.newH)
}

func (c *setLayerTilemapTileSizeCmd) Undo() {
	tm := c.target()
	if tm == nil {
		return
	}
	tm.TileWidth = c.oldW
	tm.TileHeight = c.oldH
}

// ResizeLayerTilemap grows (positive) or shrinks (negative) layerName's inline tilemap (single or layered, direct or referenced by Ref) from its left, right, top and bottom edges as an undoable command.
//
// Painted cells keep their world positions: growing or shrinking from the left or top shifts cells inside the grid and adjusts OffsetX/OffsetY by -left*TileWidth and -top*TileHeight so every remaining painted tile stays at its exact world coordinates. Writing a non-zero offset on a version 1 or version 2 scene raises SceneDoc.Version to sceneformat.CurrentVersion (which Undo restores). It refuses non-positive resulting dimensions, grids exceeding the scene format's cell cap, layers without an inline tilemap, and inherited base-owned layers.
func (e *Editor) ResizeLayerTilemap(layerName string, left, right, top, bottom int) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	if e.playing {
		return ErrPlaying
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return err
	}
	l := &e.Doc.Layers[li]
	if e.inheritedLayers()[l.Name] {
		return e.inheritedErr(l.Name, "tilemap")
	}
	tm, refName, err := e.editableInlineTilemap(l)
	if err != nil {
		return err
	}
	if left == 0 && right == 0 && top == 0 && bottom == 0 {
		return nil
	}
	if left < -maxTilemapCells || left > maxTilemapCells ||
		right < -maxTilemapCells || right > maxTilemapCells ||
		top < -maxTilemapCells || top > maxTilemapCells ||
		bottom < -maxTilemapCells || bottom > maxTilemapCells {
		return fmt.Errorf("editor: tilemap resize delta (%d, %d, %d, %d) exceeds %d cells", left, right, top, bottom, maxTilemapCells)
	}
	newW64 := int64(tm.Width) + int64(left) + int64(right)
	newH64 := int64(tm.Height) + int64(top) + int64(bottom)
	if newW64 <= 0 || newH64 <= 0 {
		return fmt.Errorf("editor: resized tilemap size (%dx%d) must be positive", newW64, newH64)
	}
	layerMult := int64(1)
	if tm.IsLayered() {
		layerMult = max(int64(len(tm.TileLayers)), 1)
	}
	if newW64 > maxTilemapCells || newH64 > maxTilemapCells ||
		newW64 > maxTilemapCells/newH64 || newW64*newH64 > maxTilemapCells/layerMult {
		return fmt.Errorf("editor: resized tilemap size %dx%d exceeds %d tiles", newW64, newH64, maxTilemapCells)
	}
	newOX64 := float64(tm.OffsetX) - float64(left)*float64(tm.TileWidth)
	newOY64 := float64(tm.OffsetY) - float64(top)*float64(tm.TileHeight)
	if math.IsNaN(newOX64) || math.IsInf(newOX64, 0) || math.Abs(newOX64) > math.MaxFloat32 ||
		math.IsNaN(newOY64) || math.IsInf(newOY64, 0) || math.Abs(newOY64) > math.MaxFloat32 {
		return errors.New("editor: resized tilemap offset must be finite")
	}
	newOX := float32(newOX64)
	newOY := float32(newOY64)
	if !isFiniteFloat32(newOX) || !isFiniteFloat32(newOY) {
		return errors.New("editor: resized tilemap offset must be finite")
	}
	// Avoid negative zero in YAML output.
	if newOX == 0 {
		newOX = 0
	}
	if newOY == 0 {
		newOY = 0
	}

	newW, newH := int(newW64), int(newH64)
	next := cloneTilemapDoc(tm)
	next.Width = newW
	next.Height = newH
	next.OffsetX = newOX
	next.OffsetY = newOY

	if tm.IsLayered() {
		next.TileLayers = make([]sceneformat.TileLayerDoc, len(tm.TileLayers))
		for i := range tm.TileLayers {
			src := &tm.TileLayers[i]
			oldCells, err := src.Cells()
			if err != nil {
				return err
			}
			newCells := make([]uint32, newW*newH)
			for oldR := 0; oldR < tm.Height; oldR++ {
				newR := oldR + top
				if newR < 0 || newR >= newH {
					continue
				}
				for oldC := 0; oldC < tm.Width; oldC++ {
					newC := oldC + left
					if newC < 0 || newC >= newW {
						continue
					}
					idx := oldR*tm.Width + oldC
					if idx < len(oldCells) {
						newCells[newR*newW+newC] = oldCells[idx]
					}
				}
			}
			rows := make([]string, newH)
			for r := 0; r < newH; r++ {
				rows[r] = sceneformat.FormatTileRow(newCells[r*newW : (r+1)*newW])
			}
			dst := *src
			dst.Visible = clonePtr(src.Visible)
			dst.Opacity = clonePtr(src.Opacity)
			dst.Rows = rows
			next.TileLayers[i] = dst
		}
	} else {
		newData := make([]int, newW*newH)
		for oldR := 0; oldR < tm.Height; oldR++ {
			newR := oldR + top
			if newR < 0 || newR >= newH {
				continue
			}
			for oldC := 0; oldC < tm.Width; oldC++ {
				newC := oldC + left
				if newC < 0 || newC >= newW {
					continue
				}
				idx := oldR*tm.Width + oldC
				if idx < len(tm.Data) {
					newData[newR*newW+newC] = tm.Data[idx]
				}
			}
		}
		next.Data = newData
	}

	candidate := e.Doc.Clone()
	if (newOX != 0 || newOY != 0) && candidate.Version < sceneformat.CurrentVersion {
		candidate.Version = sceneformat.CurrentVersion
	}
	if refName != "" {
		candidate.Tilemaps[refName] = cloneTilemapDoc(next)
	} else {
		candidate.Layers[li].Tilemap = cloneTilemapDoc(next)
	}
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	e.History.Execute(&resizeLayerTilemapCmd{
		ed:          e,
		layerName:   l.Name,
		refName:     refName,
		old:         cloneTilemapDoc(tm),
		next:        next,
		prevVersion: e.Doc.Version,
		newVersion:  candidate.Version,
	})
	return nil
}

type resizeLayerTilemapCmd struct {
	ed          *Editor
	layerName   string
	refName     string
	old         *sceneformat.TilemapDoc
	next        *sceneformat.TilemapDoc
	prevVersion int
	newVersion  int
}

func (c *resizeLayerTilemapCmd) Label() string {
	return fmt.Sprintf("Resize tilemap of %s to %dx%d", c.layerName, c.next.Width, c.next.Height)
}

func (c *resizeLayerTilemapCmd) apply(tm *sceneformat.TilemapDoc, version int) {
	c.ed.Doc.Version = version
	if c.refName != "" {
		if c.ed.Doc.Tilemaps != nil {
			c.ed.Doc.Tilemaps[c.refName] = cloneTilemapDoc(tm)
		}
		return
	}
	if li := c.ed.layerIndex(c.layerName); li >= 0 {
		c.ed.Doc.Layers[li].Tilemap = cloneTilemapDoc(tm)
	}
}

func (c *resizeLayerTilemapCmd) Do() {
	c.apply(c.next, c.newVersion)
	c.ed.Status = fmt.Sprintf("Resized layer %s tilemap to %dx%d", c.layerName, c.next.Width, c.next.Height)
}

func (c *resizeLayerTilemapCmd) Undo() {
	c.apply(c.old, c.prevVersion)
}

// ExtractLayerTilemap moves layerName's direct tilemap into top-level SceneDoc.Tilemaps[refName] and replaces the layer's tilemap with a Ref pointing to refName, as an undoable command. It refuses a layer without a tilemap, a layer already using Ref, an inherited base-owned layer, an empty or whitespace-padded refName, or a refName already present in SceneDoc.Tilemaps.
func (e *Editor) ExtractLayerTilemap(layerName, refName string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	if e.playing {
		return ErrPlaying
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return err
	}
	l := &e.Doc.Layers[li]
	if e.inheritedLayers()[l.Name] {
		return e.inheritedErr(l.Name, "tilemap")
	}
	if l.Tilemap == nil {
		return fmt.Errorf("editor: layer %q has no tilemap", l.Name)
	}
	if l.Tilemap.IsRef() {
		return fmt.Errorf("editor: layer %q already references top-level tilemap %q", l.Name, l.Tilemap.Ref)
	}
	if refName == "" || strings.TrimSpace(refName) != refName {
		return errors.New("editor: top-level tilemap name must not be empty or have surrounding whitespace")
	}
	if _, exists := e.Doc.Tilemaps[refName]; exists {
		return fmt.Errorf("editor: top-level tilemap %q already exists", refName)
	}
	candidate := e.Doc.Clone()
	if candidate.Tilemaps == nil {
		candidate.Tilemaps = make(map[string]*sceneformat.TilemapDoc)
	}
	candidate.Tilemaps[refName] = cloneTilemapDoc(l.Tilemap)
	candidate.Layers[li].Tilemap = &sceneformat.TilemapDoc{Ref: refName}
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	e.History.Execute(&extractLayerTilemapCmd{
		ed:        e,
		layerName: l.Name,
		refName:   refName,
		tilemap:   cloneTilemapDoc(l.Tilemap),
		wasNilMap: e.Doc.Tilemaps == nil,
	})
	return nil
}

type extractLayerTilemapCmd struct {
	ed        *Editor
	layerName string
	refName   string
	tilemap   *sceneformat.TilemapDoc
	wasNilMap bool
}

func (c *extractLayerTilemapCmd) Label() string {
	return "Move " + c.layerName + " tilemap to tilemaps." + c.refName
}

func (c *extractLayerTilemapCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	if c.ed.Doc.Tilemaps == nil {
		c.ed.Doc.Tilemaps = make(map[string]*sceneformat.TilemapDoc)
	}
	c.ed.Doc.Tilemaps[c.refName] = cloneTilemapDoc(c.tilemap)
	c.ed.Doc.Layers[li].Tilemap = &sceneformat.TilemapDoc{Ref: c.refName}
	c.ed.Status = fmt.Sprintf("Moved layer %s tilemap to tilemaps.%s", c.layerName, c.refName)
}

func (c *extractLayerTilemapCmd) Undo() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	c.ed.Doc.Layers[li].Tilemap = cloneTilemapDoc(c.tilemap)
	delete(c.ed.Doc.Tilemaps, c.refName)
	if c.wasNilMap && len(c.ed.Doc.Tilemaps) == 0 {
		c.ed.Doc.Tilemaps = nil
	}
}

// InlineLayerTilemap moves layerName's Ref tilemap back inline onto the layer as an undoable command. When no other layer or layerOverride in the scene references the same top-level tilemap, the top-level entry in SceneDoc.Tilemaps is removed (and restored by Undo); when another layer still references it, the top-level entry is kept and this layer receives an independent inline copy.
func (e *Editor) InlineLayerTilemap(layerName string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	if e.playing {
		return ErrPlaying
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return err
	}
	l := &e.Doc.Layers[li]
	if e.inheritedLayers()[l.Name] {
		return e.inheritedErr(l.Name, "tilemap")
	}
	if l.Tilemap == nil || !l.Tilemap.IsRef() {
		return fmt.Errorf("editor: layer %q does not reference a top-level tilemap", l.Name)
	}
	refName := l.Tilemap.Ref
	tm := e.Doc.Tilemaps[refName]
	if tm == nil {
		return fmt.Errorf("editor: top-level tilemap %q not found", refName)
	}
	keepTopLevel := false
	for i := range e.Doc.Layers {
		if i != li && e.Doc.Layers[i].Tilemap != nil && e.Doc.Layers[i].Tilemap.Ref == refName {
			keepTopLevel = true
			break
		}
	}
	if !keepTopLevel {
		for _, lo := range e.Doc.LayerOverrides {
			if lo.Tilemap != nil && lo.Tilemap.Ref == refName {
				keepTopLevel = true
				break
			}
		}
	}
	candidate := e.Doc.Clone()
	candidate.Layers[li].Tilemap = cloneTilemapDoc(tm)
	if !keepTopLevel {
		delete(candidate.Tilemaps, refName)
		if len(candidate.Tilemaps) == 0 {
			candidate.Tilemaps = nil
		}
	}
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	e.History.Execute(&inlineLayerTilemapCmd{
		ed:             e,
		layerName:      l.Name,
		refName:        refName,
		tilemap:        cloneTilemapDoc(tm),
		removeTopLevel: !keepTopLevel,
	})
	return nil
}

type inlineLayerTilemapCmd struct {
	ed             *Editor
	layerName      string
	refName        string
	tilemap        *sceneformat.TilemapDoc
	removeTopLevel bool
}

func (c *inlineLayerTilemapCmd) Label() string {
	return "Inline tilemap " + c.refName + " into " + c.layerName
}

func (c *inlineLayerTilemapCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	c.ed.Doc.Layers[li].Tilemap = cloneTilemapDoc(c.tilemap)
	if c.removeTopLevel {
		delete(c.ed.Doc.Tilemaps, c.refName)
		if len(c.ed.Doc.Tilemaps) == 0 {
			c.ed.Doc.Tilemaps = nil
		}
	}
	c.ed.Status = fmt.Sprintf("Moved tilemap %s inline on layer %s", c.refName, c.layerName)
}

func (c *inlineLayerTilemapCmd) Undo() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	if c.removeTopLevel {
		if c.ed.Doc.Tilemaps == nil {
			c.ed.Doc.Tilemaps = make(map[string]*sceneformat.TilemapDoc)
		}
		c.ed.Doc.Tilemaps[c.refName] = cloneTilemapDoc(c.tilemap)
	}
	c.ed.Doc.Layers[li].Tilemap = &sceneformat.TilemapDoc{Ref: c.refName}
}
