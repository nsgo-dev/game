package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoTileset is returned by tileset editor operations when no YAML tileset is open.
var ErrNoTileset = errors.New("editor: no tileset is open")

type tilesetEditorState struct {
	doc             *tilemap.TilesetDoc
	saved           []byte
	path            string
	relPath         string
	fromDisk        bool
	selectedTerrain string
	selectedTile    int
	editMask        uint8
	conflictTile    int
	lastErr         error
}

func isTilesetFile(relPath string) bool {
	lower := strings.ToLower(relPath)
	return strings.HasSuffix(lower, ".tileset.yaml") || strings.HasSuffix(lower, ".tileset.yml")
}

func (s *tilesetEditorState) matchesPath(relPath string) bool {
	if s == nil || relPath == "" {
		return false
	}
	clean := filepath.ToSlash(relPath)
	return s.relPath == clean || filepath.ToSlash(s.path) == clean
}

// loadTilesetDoc returns the parsed YAML tileset for relPath, preferring the live document open in the Tileset Editor when it matches relPath so terrain edits take effect immediately for painting and palette display.
func (e *Editor) loadTilesetDoc(relPath string) (*tilemap.TilesetDoc, error) {
	if e != nil && e.tileset != nil && e.tileset.doc != nil && e.tileset.matchesPath(relPath) {
		return e.tileset.doc, nil
	}
	if e == nil || e.Resolver == nil {
		return nil, fmt.Errorf("the editor has no asset resolver")
	}
	fr, ok := e.Resolver.(sceneformat.FileResolver)
	if !ok {
		return nil, fmt.Errorf("the editor's resolver does not read project files (sceneformat.FileResolver)")
	}
	data, err := fr.ReadFile(relPath)
	if err != nil {
		return nil, err
	}
	return tilemap.ParseTileset(data)
}

// TilesetOpen reports whether the Tileset Editor panel has a YAML tileset open.
func (e *Editor) TilesetOpen() bool {
	return e != nil && e.tileset != nil && e.tileset.doc != nil
}

// TilesetPath returns the file path or name of the open tileset, or "".
func (e *Editor) TilesetPath() string {
	if !e.TilesetOpen() {
		return ""
	}
	if e.tileset.relPath != "" {
		return e.tileset.relPath
	}
	return e.tileset.path
}

// TilesetDoc returns a deep copy of the tileset document open in the Tileset Editor, or nil.
func (e *Editor) TilesetDoc() *tilemap.TilesetDoc {
	if !e.TilesetOpen() {
		return nil
	}
	return e.tileset.doc.Clone()
}

// TilesetDirty reports whether the open tileset differs from what it was opened from or last saved to.
func (e *Editor) TilesetDirty() bool {
	if !e.TilesetOpen() {
		return false
	}
	data, err := tilemap.EncodeTileset(e.tileset.doc)
	return err != nil || !bytes.Equal(data, e.tileset.saved)
}

// TilesetError returns the most recent validation or save error from the Tileset Editor, or nil.
func (e *Editor) TilesetError() error {
	if !e.TilesetOpen() {
		return nil
	}
	return e.tileset.lastErr
}

// TilesetSelectedTerrain returns the name of the terrain set currently selected in the Tileset Editor, or "".
func (e *Editor) TilesetSelectedTerrain() string {
	if !e.TilesetOpen() {
		return ""
	}
	return e.tileset.selectedTerrain
}

// TilesetSelectedTile returns the local tile ID currently selected in the Tileset Editor, or -1 when closed.
func (e *Editor) TilesetSelectedTile() int {
	if !e.TilesetOpen() {
		return -1
	}
	return e.tileset.selectedTile
}

// TilesetEditMask returns the 8-bit neighbor mask currently shown in the Tileset Editor's 3x3 edge/corner editor for the selected tile.
func (e *Editor) TilesetEditMask() uint8 {
	if !e.TilesetOpen() {
		return 0
	}
	return e.tileset.editMask
}

// TilesetMaskConflictTile returns the local tile ID that already owns TilesetEditMask in the selected terrain, or -1 when there is no conflict.
func (e *Editor) TilesetMaskConflictTile() int {
	if !e.TilesetOpen() {
		return -1
	}
	return e.tileset.conflictTile
}

// OpenTilesetFile reads the YAML tileset at filePath and opens it in the Tileset Editor panel.
func (e *Editor) OpenTilesetFile(filePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	rel := ""
	if e.ProjectPath != "" {
		root := filepath.Dir(e.ProjectPath)
		if r, err := filepath.Rel(root, filePath); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		}
	}
	return e.openTileset(filePath, rel, data, true)
}

// OpenTilesetProjectFile opens relPath (slash-separated from the open project's root, or read via Resolver's FileResolver when no project is open) in the Tileset Editor panel.
func (e *Editor) OpenTilesetProjectFile(relPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project != nil && e.ProjectPath != "" {
		full, err := e.projectFile(relPath)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		return e.openTileset(full, filepath.ToSlash(relPath), data, true)
	}
	if e.Resolver != nil {
		if fr, ok := e.Resolver.(sceneformat.FileResolver); ok {
			data, err := fr.ReadFile(relPath)
			if err != nil {
				return err
			}
			return e.openTileset(filepath.ToSlash(relPath), filepath.ToSlash(relPath), data, false)
		}
	}
	return ErrNoProject
}

// OpenTilesetYAML parses data as a version 1 YAML tileset named name and opens it in the Tileset Editor panel.
func (e *Editor) OpenTilesetYAML(name string, data []byte) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	clean := filepath.ToSlash(name)
	return e.openTileset(clean, clean, data, false)
}

// OpenActiveLayerTileset opens the active layer's layered tilemap's YAML tileset in the Tileset Editor panel.
func (e *Editor) OpenActiveLayerTileset() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	layerName, tm, err := e.resolveInlineTilemap(e.ActiveLayer)
	if err != nil {
		return err
	}
	if !tm.IsLayered() {
		return fmt.Errorf("editor: layer %q has no YAML tileset", layerName)
	}
	return e.OpenTilesetProjectFile(tm.TilesetFile)
}

func (e *Editor) openTileset(filePath, relPath string, data []byte, fromDisk bool) error {
	doc, err := tilemap.ParseTileset(data)
	if err != nil {
		return err
	}
	saved, err := tilemap.EncodeTileset(doc)
	if err != nil {
		return err
	}
	e.endTileStrokeForChange()
	st := &tilesetEditorState{
		doc:          doc,
		saved:        saved,
		path:         filePath,
		relPath:      relPath,
		fromDisk:     fromDisk,
		selectedTile: 0,
		conflictTile: -1,
	}
	if len(doc.Terrains) > 0 {
		st.selectedTerrain = doc.Terrains[0].Name
		if e.ActiveTerrain != "" && slices.ContainsFunc(doc.Terrains, func(t tilemap.TerrainDoc) bool { return t.Name == e.ActiveTerrain }) {
			st.selectedTerrain = e.ActiveTerrain
		}
	}
	e.tileset = st
	e.syncTilesetMaskView()
	e.Status = "Opened tileset " + e.TilesetPath()
	e.notify()
	return nil
}

// CloseTileset closes the Tileset Editor panel.
func (e *Editor) CloseTileset() error {
	if e == nil {
		return nil
	}
	if e.playing {
		return ErrPlaying
	}
	if e.tileset == nil {
		return nil
	}
	e.tileset = nil
	e.Status = "Closed tileset editor"
	e.notify()
	return nil
}

// SaveTilesetYAML validates and encodes the open tileset document to deterministic YAML.
func (e *Editor) SaveTilesetYAML() ([]byte, error) {
	if !e.TilesetOpen() {
		return nil, ErrNoTileset
	}
	return tilemap.EncodeTileset(e.tileset.doc)
}

// SaveTileset writes the open tileset via OnSaveTileset if set, or atomically to its file path on disk.
func (e *Editor) SaveTileset() error {
	if !e.TilesetOpen() {
		return ErrNoTileset
	}
	if e.playing {
		return ErrPlaying
	}
	data, err := tilemap.EncodeTileset(e.tileset.doc)
	if err != nil {
		e.tileset.lastErr = err
		e.notify()
		return err
	}
	if e.OnSaveTileset != nil {
		if err := e.OnSaveTileset(data); err != nil {
			e.tileset.lastErr = err
			e.notify()
			return err
		}
	} else {
		if !e.tileset.fromDisk || e.tileset.path == "" {
			return errors.New("editor: open tileset has no file path to save to")
		}
		if err := writeFileAtomic(e.tileset.path, data); err != nil {
			e.tileset.lastErr = err
			e.notify()
			return err
		}
	}
	e.tileset.saved = data
	e.tileset.lastErr = nil
	e.assetsRev++
	e.invalidateProjectIndex()
	e.Rebuild()
	e.Status = "Saved tileset " + e.TilesetPath()
	e.notify()
	e.CheckProblems()
	return nil
}

// SetTilesetSelectedTerrain selects terrain name in the Tileset Editor (and syncs ActiveTerrain for painting).
func (e *Editor) SetTilesetSelectedTerrain(name string) bool {
	if !e.TilesetOpen() {
		return false
	}
	if !slices.ContainsFunc(e.tileset.doc.Terrains, func(t tilemap.TerrainDoc) bool { return t.Name == name }) {
		return false
	}
	e.tileset.selectedTerrain = name
	e.tileset.lastErr = nil
	e.ActiveTerrain = name
	e.syncTilesetMaskView()
	e.notify()
	return true
}

// SetTilesetSelectedTile selects local tile tileID in the Tileset Editor and syncs the 3x3 edge/corner view to its current mask.
func (e *Editor) SetTilesetSelectedTile(tileID int) bool {
	if !e.TilesetOpen() || tileID < 0 || tileID >= e.tileset.doc.Count() {
		return false
	}
	e.tileset.selectedTile = tileID
	e.tileset.lastErr = nil
	e.syncTilesetMaskView()
	e.notify()
	return true
}

// syncTilesetMaskView reconciles selectedTerrain, selectedTile, editMask and conflictTile after loading or mutating the tileset doc.
func (e *Editor) syncTilesetMaskView() {
	if !e.TilesetOpen() {
		return
	}
	st := e.tileset
	doc := st.doc
	if doc.Count() == 0 {
		st.selectedTile = -1
	} else if st.selectedTile < 0 || st.selectedTile >= doc.Count() {
		st.selectedTile = 0
	}
	if st.selectedTerrain == "" || !slices.ContainsFunc(doc.Terrains, func(t tilemap.TerrainDoc) bool { return t.Name == st.selectedTerrain }) {
		if len(doc.Terrains) > 0 {
			st.selectedTerrain = doc.Terrains[0].Name
		} else {
			st.selectedTerrain = ""
		}
	}
	st.conflictTile = -1
	st.editMask = 0
	if st.selectedTerrain != "" && st.selectedTile >= 0 {
		if mask, ok := tileDocMask(doc, st.selectedTile, st.selectedTerrain); ok {
			st.editMask = mask
		}
	}
}

func terrainDocByName(doc *tilemap.TilesetDoc, name string) (int, *tilemap.TerrainDoc) {
	if doc == nil {
		return -1, nil
	}
	for i := range doc.Terrains {
		if doc.Terrains[i].Name == name {
			return i, &doc.Terrains[i]
		}
	}
	return -1, nil
}

func terrainModeForDoc(td *tilemap.TerrainDoc) tilemap.AutotileMode {
	if td != nil && td.Mode == "blob" {
		return tilemap.Autotile8
	}
	return tilemap.Autotile4
}

func tileDocMask(doc *tilemap.TilesetDoc, tileID int, terrainName string) (uint8, bool) {
	if doc == nil || terrainName == "" {
		return 0, false
	}
	for i := range doc.Tiles {
		if doc.Tiles[i].ID == tileID {
			if doc.Tiles[i].Terrain != terrainName {
				return 0, false
			}
			mask, _ := tilemap.TerrainNeighborsMask(doc.Tiles[i].Neighbors)
			return mask, true
		}
	}
	return 0, false
}

// terrainMaskOwner returns the local tile ID owning mask in terrainName, or -1 if none does.
func terrainMaskOwner(doc *tilemap.TilesetDoc, terrainName string, mask uint8) int {
	if doc == nil || terrainName == "" {
		return -1
	}
	for i := range doc.Tiles {
		if doc.Tiles[i].Terrain != terrainName {
			continue
		}
		m, _ := tilemap.TerrainNeighborsMask(doc.Tiles[i].Neighbors)
		if m == mask {
			return doc.Tiles[i].ID
		}
	}
	return -1
}

func tileDocIsEmpty(t *tilemap.TileDoc) bool {
	return !t.Solid && !t.OneWay && t.Flags == 0 && len(t.Properties) == 0 && len(t.Shapes) == 0 && len(t.Animation) == 0 && t.Terrain == "" && len(t.Neighbors) == 0
}

func clearTileTerrainInDoc(doc *tilemap.TilesetDoc, tileID int) {
	for i := range doc.Tiles {
		if doc.Tiles[i].ID == tileID {
			doc.Tiles[i].Terrain = ""
			doc.Tiles[i].Neighbors = nil
			if tileDocIsEmpty(&doc.Tiles[i]) {
				doc.Tiles = slices.Delete(doc.Tiles, i, i+1)
			}
			return
		}
	}
}

func setTileTerrainInDoc(doc *tilemap.TilesetDoc, tileID int, terrainName string, mask uint8) {
	neighbors := tilemap.TerrainMaskNeighbors(mask)
	for i := range doc.Tiles {
		if doc.Tiles[i].ID == tileID {
			doc.Tiles[i].Terrain = terrainName
			doc.Tiles[i].Neighbors = neighbors
			return
		}
	}
	doc.Tiles = append(doc.Tiles, tilemap.TileDoc{
		ID:        tileID,
		Terrain:   terrainName,
		Neighbors: neighbors,
	})
	slices.SortFunc(doc.Tiles, func(a, b tilemap.TileDoc) int {
		return cmp.Compare(a.ID, b.ID)
	})
}

func (e *Editor) commitTilesetEdit(label string, edit func(doc *tilemap.TilesetDoc) error) error {
	if !e.TilesetOpen() {
		return ErrNoTileset
	}
	if e.playing {
		return ErrPlaying
	}
	before := e.tileset.doc.Clone()
	after := e.tileset.doc.Clone()
	if err := edit(after); err != nil {
		e.tileset.lastErr = err
		e.notify()
		return err
	}
	if err := tilemap.ValidateTileset(after); err != nil {
		e.tileset.lastErr = err
		e.notify()
		return err
	}
	e.tileset.lastErr = nil
	e.History.Execute(&tilesetDocCmd{
		ed:     e,
		label:  label,
		before: before,
		after:  after,
	})
	return nil
}

// AddTilesetTerrain adds a new terrain set (mode "cardinal" for 4-bit or "blob" for 8-bit) to the open tileset as an undoable edit and selects it.
func (e *Editor) AddTilesetTerrain(name, mode string) error {
	name = strings.TrimSpace(name)
	if mode == "" {
		mode = "blob"
	}
	err := e.commitTilesetEdit(fmt.Sprintf("Add terrain %q", name), func(doc *tilemap.TilesetDoc) error {
		if name == "" {
			return errors.New("editor: terrain name cannot be empty")
		}
		if _, existing := terrainDocByName(doc, name); existing != nil {
			return fmt.Errorf("editor: terrain %q already exists", name)
		}
		if mode != "cardinal" && mode != "blob" {
			return fmt.Errorf("editor: terrain mode %q must be \"cardinal\" or \"blob\"", mode)
		}
		doc.Terrains = append(doc.Terrains, tilemap.TerrainDoc{Name: name, Mode: mode})
		return nil
	})
	if err != nil {
		return err
	}
	e.tileset.selectedTerrain = name
	e.ActiveTerrain = name
	e.syncTilesetMaskView()
	e.notify()
	return nil
}

// RemoveTilesetTerrain deletes terrain name and clears all tile mask assignments belonging to it as one undoable edit.
func (e *Editor) RemoveTilesetTerrain(name string) error {
	return e.commitTilesetEdit(fmt.Sprintf("Remove terrain %q", name), func(doc *tilemap.TilesetDoc) error {
		idx, _ := terrainDocByName(doc, name)
		if idx < 0 {
			return fmt.Errorf("editor: terrain %q not found", name)
		}
		doc.Terrains = slices.Delete(doc.Terrains, idx, idx+1)
		out := doc.Tiles[:0]
		for _, tile := range doc.Tiles {
			if tile.Terrain == name {
				tile.Terrain = ""
				tile.Neighbors = nil
				if tileDocIsEmpty(&tile) {
					continue
				}
			}
			out = append(out, tile)
		}
		doc.Tiles = out
		return nil
	})
}

// SetTilesetTerrainMode changes terrain name's mode between "cardinal" (4-bit) and "blob" (8-bit) as an undoable edit. Switching from "blob" to "cardinal" strips diagonal bits from tiles of that terrain, refusing if two tiles would then share the same 4-bit mask.
func (e *Editor) SetTilesetTerrainMode(name, mode string) error {
	return e.commitTilesetEdit(fmt.Sprintf("Set terrain %q mode to %s", name, mode), func(doc *tilemap.TilesetDoc) error {
		_, td := terrainDocByName(doc, name)
		if td == nil {
			return fmt.Errorf("editor: terrain %q not found", name)
		}
		if mode != "cardinal" && mode != "blob" {
			return fmt.Errorf("editor: terrain mode %q must be \"cardinal\" or \"blob\"", mode)
		}
		if td.Mode == mode {
			return nil
		}
		td.Mode = mode
		if mode == "cardinal" {
			const cardinal = tilemap.MaskNorth | tilemap.MaskEast | tilemap.MaskSouth | tilemap.MaskWest
			seen := make(map[uint8]int)
			for i := range doc.Tiles {
				if doc.Tiles[i].Terrain != name {
					continue
				}
				m, _ := tilemap.TerrainNeighborsMask(doc.Tiles[i].Neighbors)
				m &= cardinal
				if other, dup := seen[m]; dup {
					return fmt.Errorf("editor: switching %q to cardinal collides tiles %d and %d on the same 4-bit mask", name, other, doc.Tiles[i].ID)
				}
				seen[m] = doc.Tiles[i].ID
				doc.Tiles[i].Neighbors = tilemap.TerrainMaskNeighbors(m)
			}
		}
		return nil
	})
}

// SetTilesetTerrainDefault sets (or clears when tileID == nil) the explicit default local tile ID of terrain name as an undoable edit.
func (e *Editor) SetTilesetTerrainDefault(name string, tileID *int) error {
	label := fmt.Sprintf("Clear default tile for %q", name)
	if tileID != nil {
		label = fmt.Sprintf("Set default tile %d for %q", *tileID, name)
	}
	return e.commitTilesetEdit(label, func(doc *tilemap.TilesetDoc) error {
		_, td := terrainDocByName(doc, name)
		if td == nil {
			return fmt.Errorf("editor: terrain %q not found", name)
		}
		if tileID == nil {
			td.Default = nil
			return nil
		}
		if *tileID < 0 || *tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", *tileID, doc.Count())
		}
		v := *tileID
		td.Default = &v
		return nil
	})
}

// SetTilesetTileMask assigns local tile tileID to terrainName with neighbor bitmask mask as an undoable edit.
// When reassign is true and another tile in terrainName already owns mask, that other tile's terrain assignment is cleared in the same undo step; when reassign is false, a collision returns an error without modifying the tileset.
func (e *Editor) SetTilesetTileMask(tileID int, terrainName string, mask uint8, reassign bool) error {
	return e.commitTilesetEdit(fmt.Sprintf("Assign tile %d mask in %q", tileID, terrainName), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		_, td := terrainDocByName(doc, terrainName)
		if td == nil {
			return fmt.Errorf("editor: terrain %q not found", terrainName)
		}
		mode := terrainModeForDoc(td)
		if !tilemap.ValidTerrainMask(mode, mask) {
			return fmt.Errorf("editor: mask %#x is not valid for %s terrain %q", mask, td.Mode, terrainName)
		}
		if owner := terrainMaskOwner(doc, terrainName, mask); owner >= 0 && owner != tileID {
			if !reassign {
				return fmt.Errorf("editor: mask %v is already assigned to tile %d in terrain %q", tilemap.TerrainMaskNeighbors(mask), owner, terrainName)
			}
			clearTileTerrainInDoc(doc, owner)
		}
		setTileTerrainInDoc(doc, tileID, terrainName, mask)
		return nil
	})
}

// ClearTilesetTileTerrain removes local tile tileID from its terrain set as an undoable edit.
func (e *Editor) ClearTilesetTileTerrain(tileID int) error {
	return e.commitTilesetEdit(fmt.Sprintf("Clear terrain on tile %d", tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		clearTileTerrainInDoc(doc, tileID)
		return nil
	})
}

// ToggleTilesetEdgeOrCorner toggles one edge or corner bit (tilemap.MaskNorth .. MaskNorthWest) on the currently selected tile in the selected terrain, maintaining corner-gating rules.
// If the resulting mask is free (or already owned by the selected tile), it is committed immediately as an undoable edit.
// If another tile in the same terrain already owns the resulting mask, the editor keeps that other tile untouched, records the conflict (TilesetMaskConflictTile), and updates TilesetEditMask so the user can toggle additional edges/corners or call ReassignTilesetEditMask.
func (e *Editor) ToggleTilesetEdgeOrCorner(bit uint8) error {
	if !e.TilesetOpen() {
		return ErrNoTileset
	}
	if e.playing {
		return ErrPlaying
	}
	st := e.tileset
	if st.selectedTerrain == "" {
		return errors.New("editor: no terrain selected in tileset editor")
	}
	if st.selectedTile < 0 || st.selectedTile >= st.doc.Count() {
		return errors.New("editor: no tile selected in tileset editor")
	}
	_, td := terrainDocByName(st.doc, st.selectedTerrain)
	if td == nil {
		return fmt.Errorf("editor: terrain %q not found", st.selectedTerrain)
	}
	mode := terrainModeForDoc(td)
	nextMask := tilemap.ToggleTerrainMask(mode, st.editMask, bit)
	st.editMask = nextMask
	if owner := terrainMaskOwner(st.doc, st.selectedTerrain, nextMask); owner >= 0 && owner != st.selectedTile {
		st.conflictTile = owner
		st.lastErr = nil
		e.notify()
		return nil
	}
	st.conflictTile = -1
	return e.SetTilesetTileMask(st.selectedTile, st.selectedTerrain, nextMask, false)
}

// ToggleTilesetTileCenter toggles whether the currently selected tile belongs to the selected terrain:
// if it does, removes it from the terrain; if it does not, assigns it with mask 0 (or flags a conflict if another tile owns mask 0).
func (e *Editor) ToggleTilesetTileCenter() error {
	if !e.TilesetOpen() {
		return ErrNoTileset
	}
	if e.playing {
		return ErrPlaying
	}
	st := e.tileset
	if st.selectedTerrain == "" {
		return errors.New("editor: no terrain selected in tileset editor")
	}
	if st.selectedTile < 0 || st.selectedTile >= st.doc.Count() {
		return errors.New("editor: no tile selected in tileset editor")
	}
	if _, assigned := tileDocMask(st.doc, st.selectedTile, st.selectedTerrain); assigned {
		st.editMask = 0
		st.conflictTile = -1
		return e.ClearTilesetTileTerrain(st.selectedTile)
	}
	st.editMask = 0
	if owner := terrainMaskOwner(st.doc, st.selectedTerrain, 0); owner >= 0 && owner != st.selectedTile {
		st.conflictTile = owner
		st.lastErr = nil
		e.notify()
		return nil
	}
	st.conflictTile = -1
	return e.SetTilesetTileMask(st.selectedTile, st.selectedTerrain, 0, false)
}

// ReassignTilesetEditMask commits the pending conflicting mask (TilesetEditMask) to the selected tile, clearing the previous owner in the same undo step.
func (e *Editor) ReassignTilesetEditMask() error {
	if !e.TilesetOpen() {
		return ErrNoTileset
	}
	st := e.tileset
	if st.selectedTerrain == "" || st.selectedTile < 0 {
		return errors.New("editor: no tile or terrain selected")
	}
	mask := st.editMask
	st.conflictTile = -1
	return e.SetTilesetTileMask(st.selectedTile, st.selectedTerrain, mask, true)
}

// tilesetTileVisual returns the image and frame for local tile tileID of the open tileset.
func (e *Editor) tilesetTileVisual(tileID int) (core.ImageSource, sprite.Frame, bool) {
	if !e.TilesetOpen() {
		return core.ImageSource{}, sprite.Frame{}, false
	}
	doc := e.tileset.doc
	img := core.ImageSource{URI: doc.Image}
	var atlas *sprite.Atlas
	if doc.Atlas != "" {
		if e.Resolver == nil {
			return core.ImageSource{}, sprite.Frame{}, false
		}
		var err error
		if atlas, err = e.Resolver.Atlas(doc.Atlas); err != nil || atlas == nil {
			return core.ImageSource{}, sprite.Frame{}, false
		}
		img = atlas.Image
	} else if ir, ok := e.Resolver.(sceneformat.ImageResolver); ok {
		if resolved, err := ir.Image(doc.Image); err == nil {
			img = resolved
		}
	}
	f, ok := tilemap.TilesetFrame(doc, atlas, tileID)
	return img, f, ok
}

type tilesetDocCmd struct {
	ed            *Editor
	label         string
	before, after *tilemap.TilesetDoc
}

func (c *tilesetDocCmd) Label() string { return c.label }

func (c *tilesetDocCmd) Do() {
	if c.ed.tileset == nil {
		return
	}
	c.ed.tileset.doc = c.after.Clone()
	c.ed.syncTilesetMaskView()
	c.ed.keepTileInPalette()
	c.ed.Status = c.label
}

func (c *tilesetDocCmd) Undo() {
	if c.ed.tileset == nil {
		return
	}
	c.ed.tileset.doc = c.before.Clone()
	c.ed.syncTilesetMaskView()
	c.ed.keepTileInPalette()
}
