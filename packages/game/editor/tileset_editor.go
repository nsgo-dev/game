package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/skeletal"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoTileset is returned by tileset editor operations when no YAML tileset is open.
var ErrNoTileset = errors.New("editor: no tileset is open")

// TilesetEditorTab selects which per-tile editing tab is shown in the Tileset Editor panel.
type TilesetEditorTab string

const (
	// TilesetTabTerrain edits 4-bit cardinal and 8-bit blob autotile masks.
	TilesetTabTerrain TilesetEditorTab = "terrain"
	// TilesetTabCollision edits per-tile solid/one-way/custom flags and rect/polygon collision shapes.
	TilesetTabCollision TilesetEditorTab = "collision"
	// TilesetTabAnimation edits per-tile animation frames, durations, and live playback preview.
	TilesetTabAnimation TilesetEditorTab = "animation"
	// TilesetTabData edits per-tile name, variant weight, and typed custom properties.
	TilesetTabData TilesetEditorTab = "data"
)

// TilesetShapeTool selects the interactive collision shape drawing tool on the tile canvas.
type TilesetShapeTool string

const (
	// TilesetShapeToolRect adds or edits rectangular collision shapes over the tile.
	TilesetShapeToolRect TilesetShapeTool = "rect"
	// TilesetShapeToolPolygon clicks vertices over the tile to construct a polygon collision shape.
	TilesetShapeToolPolygon TilesetShapeTool = "polygon"
)

type tilesetEditorState struct {
	doc             *tilemap.TilesetDoc
	saved           []byte
	path            string
	relPath         string
	fromDisk        bool
	tab             TilesetEditorTab
	selectedTerrain string
	selectedTile    int
	editMask        uint8
	conflictTile    int
	selectedShape   int
	shapeTool       TilesetShapeTool
	draftPoly       [][2]float32
	animPlaying     bool
	animElapsed     time.Duration
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

func ensurePathWithinDir(root, target string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot, err = filepath.Abs(root)
		if err != nil {
			return err
		}
	}
	cur := target
	var suffix []string
	for {
		realCur, err := filepath.EvalSymlinks(cur)
		if err == nil {
			realTarget := realCur
			for i := len(suffix) - 1; i >= 0; i-- {
				realTarget = filepath.Join(realTarget, suffix[i])
			}
			rel, relErr := filepath.Rel(realRoot, realTarget)
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
				return fmt.Errorf("editor: path %q resolves outside project root", target)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return err
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
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
		if ensurePathWithinDir(root, filePath) == nil {
			if r, err := filepath.Rel(root, filePath); err == nil && !strings.HasPrefix(r, "..") {
				rel = filepath.ToSlash(r)
			}
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
		if err := ensurePathWithinDir(filepath.Dir(e.ProjectPath), full); err != nil {
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

// OpenActiveLayerTileset opens the active layer's YAML tileset (whether layered or single-grid with TilesetFile) in the Tileset Editor panel.
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
	if !tm.HasTilesetFile() {
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
		doc:           doc,
		saved:         saved,
		path:          filePath,
		relPath:       relPath,
		fromDisk:      fromDisk,
		tab:           TilesetTabTerrain,
		selectedTile:  0,
		conflictTile:  -1,
		selectedShape: -1,
		shapeTool:     TilesetShapeToolRect,
		animPlaying:   true,
	}
	if len(doc.Terrains) > 0 {
		st.selectedTerrain = doc.Terrains[0].Name
		if e.ActiveTerrain != "" && slices.ContainsFunc(doc.Terrains, func(t tilemap.TerrainDoc) bool { return t.Name == e.ActiveTerrain }) {
			st.selectedTerrain = e.ActiveTerrain
		}
	}
	e.tileset = st
	e.syncTilesetMaskView()
	e.syncTileAnimLoop()
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
	e.Rebuild()
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
		if e.Project != nil && e.ProjectPath != "" && e.tileset.relPath != "" {
			if err := ensurePathWithinDir(filepath.Dir(e.ProjectPath), e.tileset.path); err != nil {
				e.tileset.lastErr = err
				e.notify()
				return err
			}
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

// SetTilesetSelectedTile selects local tile tileID in the Tileset Editor and syncs the 3x3 edge/corner view, shape selection, and animation preview.
func (e *Editor) SetTilesetSelectedTile(tileID int) bool {
	if !e.TilesetOpen() || tileID < 0 || tileID >= e.tileset.doc.Count() {
		return false
	}
	e.tileset.selectedTile = tileID
	e.tileset.selectedShape = -1
	e.tileset.draftPoly = nil
	e.tileset.animElapsed = 0
	e.tileset.lastErr = nil
	e.syncTilesetMaskView()
	e.syncTileAnimLoop()
	e.notify()
	return true
}

// syncTilesetMaskView reconciles selectedTerrain, selectedTile, editMask, selectedShape, and conflictTile after loading or mutating the tileset doc.
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
	if tile := findTileDoc(doc, st.selectedTile); tile != nil {
		if st.selectedShape >= len(tile.Shapes) {
			st.selectedShape = len(tile.Shapes) - 1
		}
	} else {
		st.selectedShape = -1
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
	return t.Name == "" && !t.Solid && !t.OneWay && t.Flags == 0 && t.Weight == nil &&
		len(t.Properties) == 0 && len(t.Shapes) == 0 && len(t.Animation) == 0 &&
		t.Terrain == "" && len(t.Neighbors) == 0
}

func findTileDoc(doc *tilemap.TilesetDoc, tileID int) *tilemap.TileDoc {
	if doc == nil {
		return nil
	}
	for i := range doc.Tiles {
		if doc.Tiles[i].ID == tileID {
			return &doc.Tiles[i]
		}
	}
	return nil
}

func ensureTileDoc(doc *tilemap.TilesetDoc, tileID int) *tilemap.TileDoc {
	if t := findTileDoc(doc, tileID); t != nil {
		return t
	}
	doc.Tiles = append(doc.Tiles, tilemap.TileDoc{ID: tileID})
	slices.SortFunc(doc.Tiles, func(a, b tilemap.TileDoc) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return findTileDoc(doc, tileID)
}

func pruneTileDoc(doc *tilemap.TilesetDoc, tileID int) {
	if doc == nil {
		return
	}
	for i := range doc.Tiles {
		if doc.Tiles[i].ID == tileID {
			if tileDocIsEmpty(&doc.Tiles[i]) {
				doc.Tiles = slices.Delete(doc.Tiles, i, i+1)
			}
			return
		}
	}
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
		target: e.tileset,
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
	if e.playing {
		return ErrPlaying
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
	target        *tilesetEditorState
	label         string
	before, after *tilemap.TilesetDoc
}

func (c *tilesetDocCmd) Label() string { return c.label }

func (c *tilesetDocCmd) Do() {
	if c.ed.tileset == nil || c.ed.tileset != c.target {
		return
	}
	c.ed.tileset.doc = c.after.Clone()
	c.ed.syncTilesetMaskView()
	c.ed.keepTileInPalette()
	c.ed.Status = c.label
}

func (c *tilesetDocCmd) Undo() {
	if c.ed.tileset == nil || c.ed.tileset != c.target {
		return
	}
	c.ed.tileset.doc = c.before.Clone()
	c.ed.syncTilesetMaskView()
	c.ed.keepTileInPalette()
}

// TilesetTab returns the active tab of the Tileset Editor panel.
func (e *Editor) TilesetTab() TilesetEditorTab {
	if !e.TilesetOpen() {
		return TilesetTabTerrain
	}
	if e.tileset.tab == "" {
		return TilesetTabTerrain
	}
	return e.tileset.tab
}

// SetTilesetTab switches the active per-tile editing tab in the Tileset Editor panel.
func (e *Editor) SetTilesetTab(tab TilesetEditorTab) {
	if !e.TilesetOpen() {
		return
	}
	switch tab {
	case TilesetTabTerrain, TilesetTabCollision, TilesetTabAnimation, TilesetTabData:
	default:
		return
	}
	if e.tileset.tab == tab {
		return
	}
	e.tileset.tab = tab
	e.tileset.draftPoly = nil
	e.syncTileAnimLoop()
	e.notify()
}

// TilesetShapeTool returns the active collision shape drawing tool (rect or polygon).
func (e *Editor) TilesetShapeTool() TilesetShapeTool {
	if !e.TilesetOpen() || e.tileset.shapeTool == "" {
		return TilesetShapeToolRect
	}
	return e.tileset.shapeTool
}

// SetTilesetShapeTool selects the collision shape drawing tool (rect or polygon).
func (e *Editor) SetTilesetShapeTool(tool TilesetShapeTool) {
	if !e.TilesetOpen() {
		return
	}
	if tool != TilesetShapeToolRect && tool != TilesetShapeToolPolygon {
		return
	}
	e.tileset.shapeTool = tool
	e.tileset.draftPoly = nil
	e.notify()
}

// TilesetSelectedShape returns the index of the selected collision shape on the selected tile, or -1.
func (e *Editor) TilesetSelectedShape() int {
	if !e.TilesetOpen() {
		return -1
	}
	return e.tileset.selectedShape
}

// SetTilesetSelectedShape selects collision shape index on the selected tile (-1 clears selection).
func (e *Editor) SetTilesetSelectedShape(index int) {
	if !e.TilesetOpen() {
		return
	}
	tile := findTileDoc(e.tileset.doc, e.tileset.selectedTile)
	if index < 0 || tile == nil || index >= len(tile.Shapes) {
		e.tileset.selectedShape = -1
	} else {
		e.tileset.selectedShape = index
	}
	e.notify()
}

// TilesetDraftPolygon returns a copy of the polygon vertices currently being drawn over the tile canvas.
func (e *Editor) TilesetDraftPolygon() [][2]float32 {
	if !e.TilesetOpen() || len(e.tileset.draftPoly) == 0 {
		return nil
	}
	return slices.Clone(e.tileset.draftPoly)
}

// AddTilesetDraftPolygonPoint appends a vertex (x, y) in tile coordinates to the draft polygon being drawn.
func (e *Editor) AddTilesetDraftPolygonPoint(x, y float32) {
	if !e.TilesetOpen() || e.playing {
		return
	}
	if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) || math.IsNaN(float64(y)) || math.IsInf(float64(y), 0) {
		return
	}
	e.tileset.draftPoly = append(e.tileset.draftPoly, [2]float32{x, y})
	e.notify()
}

// ClearTilesetDraftPolygon discards any in-progress polygon vertices.
func (e *Editor) ClearTilesetDraftPolygon() {
	if !e.TilesetOpen() || len(e.tileset.draftPoly) == 0 {
		return
	}
	e.tileset.draftPoly = nil
	e.notify()
}

// CommitTilesetDraftPolygon turns the draft polygon (at least 3 points) into a collision shape on the selected tile as one undoable edit.
func (e *Editor) CommitTilesetDraftPolygon() error {
	if !e.TilesetOpen() {
		return ErrNoTileset
	}
	if e.playing {
		return ErrPlaying
	}
	if len(e.tileset.draftPoly) < 3 {
		return errors.New("editor: a polygon collision shape needs at least 3 points")
	}
	pts := make([][]float32, len(e.tileset.draftPoly))
	for i, p := range e.tileset.draftPoly {
		pts[i] = []float32{p[0], p[1]}
	}
	err := e.AddTilesetTileShape(e.tileset.selectedTile, tilemap.TileShapeDoc{
		Kind:   "polygon",
		Points: pts,
	})
	if err == nil {
		e.tileset.draftPoly = nil
		e.notify()
	}
	return err
}

// TilesetTileInfo returns a deep copy of tileID's TileDoc in the open tileset and whether the tileset has explicit data for it.
func (e *Editor) TilesetTileInfo(tileID int) (tilemap.TileDoc, bool) {
	if !e.TilesetOpen() || tileID < 0 || tileID >= e.tileset.doc.Count() {
		return tilemap.TileDoc{}, false
	}
	t := findTileDoc(e.tileset.doc, tileID)
	if t == nil {
		return tilemap.TileDoc{ID: tileID}, false
	}
	cp := *t
	if t.Weight != nil {
		w := *t.Weight
		cp.Weight = &w
	}
	if t.Properties != nil {
		cp.Properties = maps.Clone(t.Properties)
	}
	if t.Shapes != nil {
		cp.Shapes = make([]tilemap.TileShapeDoc, len(t.Shapes))
		for i, s := range t.Shapes {
			if s.Points != nil {
				pts := make([][]float32, len(s.Points))
				for j, pt := range s.Points {
					pts[j] = slices.Clone(pt)
				}
				s.Points = pts
			}
			if s.Properties != nil {
				s.Properties = maps.Clone(s.Properties)
			}
			cp.Shapes[i] = s
		}
	}
	if t.Animation != nil {
		cp.Animation = slices.Clone(t.Animation)
	}
	if t.Neighbors != nil {
		cp.Neighbors = slices.Clone(t.Neighbors)
	}
	return cp, true
}

// TilesetTileDisplayName returns tileID's human-readable name from doc: TileDoc.Name when set, else the atlas frame name, else "tile_<id>".
func TilesetTileDisplayName(doc *tilemap.TilesetDoc, tileID int) string {
	if doc != nil {
		if t := findTileDoc(doc, tileID); t != nil && t.Name != "" {
			return t.Name
		}
		if doc.Atlas != "" && tileID >= 0 && tileID < len(doc.Frames) {
			return doc.Frames[tileID]
		}
	}
	return fmt.Sprintf("tile_%d", tileID)
}

// TilesetTileSource formats where tileID comes from in doc, e.g. "art/tiles/dungeon.png · cell 2, 1" or "atlas: props · frame torch_1".
func TilesetTileSource(doc *tilemap.TilesetDoc, tileID int) string {
	if doc == nil {
		return ""
	}
	if doc.Atlas != "" {
		frame := ""
		if tileID >= 0 && tileID < len(doc.Frames) {
			frame = doc.Frames[tileID]
		}
		return fmt.Sprintf("atlas: %s · frame %s", doc.Atlas, frame)
	}
	cols := max(doc.Columns, 1)
	col, row := tileID%cols, tileID/cols
	return fmt.Sprintf("%s · cell %d, %d", doc.Image, col, row)
}

// SetTilesetTileName sets tileID's optional name as an undoable edit.
func (e *Editor) SetTilesetTileName(tileID int, name string) error {
	name = strings.TrimSpace(name)
	return e.commitTilesetEdit(fmt.Sprintf("Rename tile %d", tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := ensureTileDoc(doc, tileID)
		t.Name = name
		pruneTileDoc(doc, tileID)
		return nil
	})
}

// SetTilesetTileWeight sets (or clears when weight == nil) tileID's variant selection weight as an undoable edit.
func (e *Editor) SetTilesetTileWeight(tileID int, weight *float64) error {
	return e.commitTilesetEdit(fmt.Sprintf("Set tile %d weight", tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		if weight != nil && (math.IsNaN(*weight) || math.IsInf(*weight, 0) || *weight < 0) {
			return fmt.Errorf("editor: tile weight %v must be finite and non-negative", *weight)
		}
		t := ensureTileDoc(doc, tileID)
		if weight == nil {
			t.Weight = nil
		} else {
			w := *weight
			t.Weight = &w
		}
		pruneTileDoc(doc, tileID)
		return nil
	})
}

// SetTilesetTileCollision sets tileID's primary collision mode ("solid", "one-way", or "none") as an undoable edit, preserving any custom flags bits.
func (e *Editor) SetTilesetTileCollision(tileID int, mode string) error {
	return e.commitTilesetEdit(fmt.Sprintf("Set tile %d collision to %s", tileID, mode), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		var solid, oneWay bool
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "solid":
			solid = true
		case "one-way", "oneway":
			oneWay = true
		case "none", "":
		default:
			return fmt.Errorf("editor: collision mode %q must be \"solid\", \"one-way\", or \"none\"", mode)
		}
		t := ensureTileDoc(doc, tileID)
		t.Solid = solid
		t.OneWay = oneWay
		pruneTileDoc(doc, tileID)
		return nil
	})
}

// SetTilesetTileFlags sets tileID's solid, one-way, and custom flag bits in one undoable edit.
func (e *Editor) SetTilesetTileFlags(tileID int, solid, oneWay bool, customFlags tilemap.TileFlags) error {
	return e.commitTilesetEdit(fmt.Sprintf("Set tile %d flags", tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := ensureTileDoc(doc, tileID)
		t.Solid = solid
		t.OneWay = oneWay
		t.Flags = customFlags &^ (tilemap.TileSolid | tilemap.TileOneWay)
		pruneTileDoc(doc, tileID)
		return nil
	})
}

// ToggleTilesetTileFlagBit toggles bit (2..31 for custom bits, 0 for solid, 1 for one-way) on tileID as an undoable edit.
func (e *Editor) ToggleTilesetTileFlagBit(tileID int, bit uint) error {
	if bit > 31 {
		return fmt.Errorf("editor: tile flag bit %d must be in [0, 31]", bit)
	}
	return e.commitTilesetEdit(fmt.Sprintf("Toggle tile %d flag bit %d", tileID, bit), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := ensureTileDoc(doc, tileID)
		switch bit {
		case 0:
			t.Solid = !t.Solid
		case 1:
			t.OneWay = !t.OneWay
		default:
			t.Flags ^= tilemap.TileFlags(1 << bit)
		}
		pruneTileDoc(doc, tileID)
		return nil
	})
}

// AddTilesetTileShape appends a collision shape (rect, ellipse, polygon, or polyline) to tileID as an undoable edit and selects it.
func (e *Editor) AddTilesetTileShape(tileID int, shape tilemap.TileShapeDoc) error {
	var newIdx int
	err := e.commitTilesetEdit(fmt.Sprintf("Add %s shape to tile %d", shape.Kind, tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := ensureTileDoc(doc, tileID)
		t.Shapes = append(t.Shapes, cloneTileShapeDoc(shape))
		newIdx = len(t.Shapes) - 1
		return nil
	})
	if err == nil && e.TilesetOpen() && e.tileset.selectedTile == tileID {
		e.tileset.selectedShape = newIdx
		e.notify()
	}
	return err
}

// UpdateTilesetTileShape replaces collision shape shapeIndex on tileID as an undoable edit.
func (e *Editor) UpdateTilesetTileShape(tileID, shapeIndex int, shape tilemap.TileShapeDoc) error {
	return e.commitTilesetEdit(fmt.Sprintf("Update shape %d on tile %d", shapeIndex, tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := findTileDoc(doc, tileID)
		if t == nil || shapeIndex < 0 || shapeIndex >= len(t.Shapes) {
			return fmt.Errorf("editor: tile %d has no shape %d", tileID, shapeIndex)
		}
		t.Shapes[shapeIndex] = cloneTileShapeDoc(shape)
		return nil
	})
}

// RemoveTilesetTileShape deletes collision shape shapeIndex from tileID as an undoable edit.
func (e *Editor) RemoveTilesetTileShape(tileID, shapeIndex int) error {
	return e.commitTilesetEdit(fmt.Sprintf("Remove shape %d from tile %d", shapeIndex, tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := findTileDoc(doc, tileID)
		if t == nil || shapeIndex < 0 || shapeIndex >= len(t.Shapes) {
			return fmt.Errorf("editor: tile %d has no shape %d", tileID, shapeIndex)
		}
		t.Shapes = slices.Delete(t.Shapes, shapeIndex, shapeIndex+1)
		if len(t.Shapes) == 0 {
			t.Shapes = nil
		}
		pruneTileDoc(doc, tileID)
		return nil
	})
}

func cloneTileShapeDoc(s tilemap.TileShapeDoc) tilemap.TileShapeDoc {
	if s.Points != nil {
		pts := make([][]float32, len(s.Points))
		for i, pt := range s.Points {
			pts[i] = slices.Clone(pt)
		}
		s.Points = pts
	}
	if s.Properties != nil {
		s.Properties = maps.Clone(s.Properties)
	}
	return s
}

// AddTilesetTileAnimFrame appends an animation frame to tileID as an undoable edit.
func (e *Editor) AddTilesetTileAnimFrame(tileID int, frame tilemap.TileFrameDoc) error {
	err := e.commitTilesetEdit(fmt.Sprintf("Add animation frame to tile %d", tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := ensureTileDoc(doc, tileID)
		t.Animation = append(t.Animation, frame)
		return nil
	})
	if err == nil {
		e.syncTileAnimLoop()
	}
	return err
}

// UpdateTilesetTileAnimFrame updates animation frame frameIndex of tileID as an undoable edit.
func (e *Editor) UpdateTilesetTileAnimFrame(tileID, frameIndex int, frame tilemap.TileFrameDoc) error {
	return e.commitTilesetEdit(fmt.Sprintf("Update animation frame %d on tile %d", frameIndex, tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := findTileDoc(doc, tileID)
		if t == nil || frameIndex < 0 || frameIndex >= len(t.Animation) {
			return fmt.Errorf("editor: tile %d has no animation frame %d", tileID, frameIndex)
		}
		t.Animation[frameIndex] = frame
		return nil
	})
}

// RemoveTilesetTileAnimFrame deletes animation frame frameIndex from tileID as an undoable edit.
func (e *Editor) RemoveTilesetTileAnimFrame(tileID, frameIndex int) error {
	err := e.commitTilesetEdit(fmt.Sprintf("Remove animation frame %d from tile %d", frameIndex, tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := findTileDoc(doc, tileID)
		if t == nil || frameIndex < 0 || frameIndex >= len(t.Animation) {
			return fmt.Errorf("editor: tile %d has no animation frame %d", tileID, frameIndex)
		}
		t.Animation = slices.Delete(t.Animation, frameIndex, frameIndex+1)
		if len(t.Animation) == 0 {
			t.Animation = nil
		}
		pruneTileDoc(doc, tileID)
		return nil
	})
	if err == nil {
		e.syncTileAnimLoop()
	}
	return err
}

// SetTilesetTileAnimation replaces tileID's full animation frame sequence as one undoable edit (nil or empty clears animation).
func (e *Editor) SetTilesetTileAnimation(tileID int, frames []tilemap.TileFrameDoc) error {
	err := e.commitTilesetEdit(fmt.Sprintf("Set tile %d animation", tileID), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := ensureTileDoc(doc, tileID)
		if len(frames) == 0 {
			t.Animation = nil
		} else {
			t.Animation = slices.Clone(frames)
		}
		pruneTileDoc(doc, tileID)
		return nil
	})
	if err == nil {
		e.syncTileAnimLoop()
	}
	return err
}

// SetTilesetAnimPlaying toggles live animation preview playback in the Tileset Editor.
func (e *Editor) SetTilesetAnimPlaying(playing bool) {
	if !e.TilesetOpen() {
		return
	}
	e.tileset.animPlaying = playing
	e.syncTileAnimLoop()
	e.notify()
}

// TilesetAnimPlaying reports whether the Tileset Editor's tile animation preview is currently playing.
func (e *Editor) TilesetAnimPlaying() bool {
	return e.TilesetOpen() && e.tileset.animPlaying
}

// TilesetAnimPreviewTile returns the local tile ID currently displayed by the Tileset Editor's animation preview for the selected tile.
func (e *Editor) TilesetAnimPreviewTile() int {
	if !e.TilesetOpen() {
		return -1
	}
	sel := e.tileset.selectedTile
	t := findTileDoc(e.tileset.doc, sel)
	if t == nil || len(t.Animation) == 0 {
		return sel
	}
	var total time.Duration
	for _, f := range t.Animation {
		if f.Duration > 0 {
			total += f.Duration
		}
	}
	if total <= 0 {
		return t.Animation[0].Tile
	}
	elapsed := e.tileset.animElapsed % total
	if elapsed < 0 {
		elapsed += total
	}
	for _, f := range t.Animation {
		if f.Duration <= 0 {
			continue
		}
		if elapsed < f.Duration {
			return f.Tile
		}
		elapsed -= f.Duration
	}
	return t.Animation[0].Tile
}

// SetTilesetTileProperty sets a typed custom property (bool, int, float64, or string) on tileID as an undoable edit.
func (e *Editor) SetTilesetTileProperty(tileID int, key string, value any) error {
	key = strings.TrimSpace(key)
	return e.commitTilesetEdit(fmt.Sprintf("Set tile %d property %q", tileID, key), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		if key == "" {
			return errors.New("editor: property key cannot be empty")
		}
		switch v := value.(type) {
		case bool, int, string:
		case float32:
			f := float64(v)
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return fmt.Errorf("editor: property %q value must be finite", key)
			}
			value = f
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("editor: property %q value must be finite", key)
			}
		default:
			return fmt.Errorf("editor: property %q has unsupported type %T (want bool, int, float64, or string)", key, value)
		}
		t := ensureTileDoc(doc, tileID)
		if t.Properties == nil {
			t.Properties = make(map[string]any)
		}
		t.Properties[key] = value
		return nil
	})
}

// DeleteTilesetTileProperty removes custom property key from tileID as an undoable edit.
func (e *Editor) DeleteTilesetTileProperty(tileID int, key string) error {
	return e.commitTilesetEdit(fmt.Sprintf("Delete tile %d property %q", tileID, key), func(doc *tilemap.TilesetDoc) error {
		if tileID < 0 || tileID >= doc.Count() {
			return fmt.Errorf("editor: tile %d is outside the tileset's %d tiles", tileID, doc.Count())
		}
		t := findTileDoc(doc, tileID)
		if t == nil || len(t.Properties) == 0 {
			return nil
		}
		delete(t.Properties, key)
		if len(t.Properties) == 0 {
			t.Properties = nil
		}
		pruneTileDoc(doc, tileID)
		return nil
	})
}

// hasAnimatedTiles reports whether e.Built holds any tilemap with at least one animated tile.
func (e *Editor) hasAnimatedTiles() bool {
	if e == nil || e.Built == nil {
		return false
	}
	for _, l := range e.Built.Layers {
		if l.Map == nil {
			continue
		}
		for _, ts := range l.Map.Tilesets {
			if ts != nil && len(ts.Animations) > 0 {
				return true
			}
		}
	}
	return false
}

func (e *Editor) tilesetPreviewNeedsLoop() bool {
	if !e.TilesetOpen() || !e.tileset.animPlaying {
		return false
	}
	t := findTileDoc(e.tileset.doc, e.tileset.selectedTile)
	return t != nil && len(t.Animation) > 0
}

// StepTileAnimations advances animated tiles in the edit viewport and the Tileset Editor's animation preview by dt.
func (e *Editor) StepTileAnimations(dt time.Duration) {
	if e == nil || dt <= 0 || e.playing {
		return
	}
	changed := false
	if e.hasAnimatedTiles() {
		e.Built.Update(dt)
		changed = true
	}
	if e.tilesetPreviewNeedsLoop() {
		e.tileset.animElapsed += dt
		changed = true
	}
	if changed {
		e.notifyTileAnim()
	}
}

func (e *Editor) syncTileAnimLoop() {
	if e == nil {
		return
	}
	want := !e.playing && e.scope != nil && (e.hasAnimatedTiles() || e.tilesetPreviewNeedsLoop())
	if !want {
		e.stopTileAnimLoop()
		return
	}
	if e.tileAnimStop != nil {
		return
	}
	rate := e.PlayRate
	if rate <= 0 {
		rate = 60
	}
	loop := &game.Loop{
		Rate:   rate,
		Update: e.StepTileAnimations,
	}
	e.scope.Run(func() {
		e.tileAnimStop = loop.Run(e.scope)
	})
}

func (e *Editor) stopTileAnimLoop() {
	if e == nil || e.tileAnimStop == nil {
		return
	}
	stop := e.tileAnimStop
	e.tileAnimStop = nil
	stop()
}

// inspectedTilesetTile returns the YAML tileset path and 0-based local tile ID when the right-side Inspector should show the Tile Inspector (matching Tiles.dc.html).
func (e *Editor) inspectedTilesetTile() (string, int, bool) {
	if e == nil || e.Doc == nil || e.ViewMode != ViewMode2D || e.SelectedID != "" || e.inspectLayer {
		return "", -1, false
	}
	if rel, _ := e.inspectedImportAsset(); rel != "" {
		return "", -1, false
	}
	if !isTileTool(e.Tool) {
		return "", -1, false
	}
	li := e.layerIndex(e.ActiveLayer)
	if li < 0 {
		return "", -1, false
	}
	tm := e.inlineTilemapForLayer(&e.Doc.Layers[li])
	if tm == nil || !tm.HasTilesetFile() {
		return "", -1, false
	}
	doc, err := e.loadTilesetDoc(tm.TilesetFile)
	if err != nil || doc == nil || doc.Count() == 0 {
		return "", -1, false
	}
	tileID := e.ActiveTileID - 1
	if tileID < 0 || tileID >= doc.Count() {
		tileID = 0
	}
	return tm.TilesetFile, tileID, true
}

// ensureTilesetOpenForPath opens relPath in the Tileset Editor if it is not already the open tileset, so edits made from the right-side Tile Inspector go through the same undoable TilesetDoc commands.
func (e *Editor) ensureTilesetOpenForPath(relPath string) error {
	if e.TilesetOpen() && e.tileset.matchesPath(relPath) {
		return nil
	}
	return e.OpenTilesetProjectFile(relPath)
}

// ActiveLayerTilesetPath returns the active layer's YAML tileset path (TilesetFile), or "" when the active layer does not use a YAML tileset file.
func (e *Editor) ActiveLayerTilesetPath() string {
	if e == nil || e.Doc == nil {
		return ""
	}
	li := e.layerIndex(e.ActiveLayer)
	if li < 0 {
		return ""
	}
	tm := e.inlineTilemapForLayer(&e.Doc.Layers[li])
	if tm == nil || !tm.HasTilesetFile() {
		return ""
	}
	return tm.TilesetFile
}

// liveTilesetYAML returns the encoded YAML of the open Tileset Editor document when its path matches p.
func (e *Editor) liveTilesetYAML(p string) ([]byte, bool, error) {
	if e == nil || !e.TilesetOpen() || !e.tileset.matchesPath(p) {
		return nil, false, nil
	}
	data, err := tilemap.EncodeTileset(e.tileset.doc)
	return data, true, err
}

// liveResolver wraps e.Resolver so sceneformat.Build and connection validation see live unsaved edits
// made in the Tileset Editor or in open scene tabs immediately, while forwarding every optional
// sceneformat resolver interface and preserving FileResolver fallbacks.
func (e *Editor) liveResolver() sceneformat.AssetResolver {
	if e == nil {
		return nil
	}
	hasOpenScene := e.hasOpenProjectSceneTab()
	if e.Resolver == nil && !e.TilesetOpen() && !hasOpenScene {
		return nil
	}
	_, hasFile := e.Resolver.(sceneformat.FileResolver)
	_, hasImage := e.Resolver.(sceneformat.ImageResolver)
	if !e.TilesetOpen() && !hasOpenScene && (!hasFile || hasImage) {
		return e.Resolver
	}
	base := &liveTilesetResolver{ed: e, inner: e.Resolver}
	if tr, ok := e.Resolver.(sceneformat.TilemapResolver); ok {
		return &liveTilesetWithTilemapResolver{liveTilesetResolver: base, tr: tr}
	}
	return base
}

type liveTilesetResolver struct {
	ed    *Editor
	inner sceneformat.AssetResolver
}

func (r *liveTilesetResolver) Atlas(name string) (*sprite.Atlas, error) {
	if r.inner == nil {
		return nil, fmt.Errorf("atlas %q not found", name)
	}
	return r.inner.Atlas(name)
}

func (r *liveTilesetResolver) ReadFile(p string) ([]byte, error) {
	if data, ok, err := r.ed.liveTilesetYAML(p); ok {
		return data, err
	}
	if fr, ok := r.inner.(sceneformat.FileResolver); ok {
		return fr.ReadFile(p)
	}
	return nil, fmt.Errorf("reading %q requires a FileResolver", p)
}

func (r *liveTilesetResolver) Image(p string) (core.ImageSource, error) {
	if ir, ok := r.inner.(sceneformat.ImageResolver); ok {
		return ir.Image(p)
	}
	if fr, ok := r.inner.(sceneformat.FileResolver); ok {
		if data, err := fr.ReadFile(p); err == nil && len(data) > 0 {
			return core.ImageSource{URI: p, Data: data}, nil
		}
	}
	return core.ImageSource{URI: p}, nil
}

func (r *liveTilesetResolver) Scene(p string) (*sceneformat.SceneDoc, error) {
	if doc := r.ed.liveOpenSceneDoc(p); doc != nil {
		return doc, nil
	}
	if sr, ok := r.inner.(sceneformat.SceneResolver); ok {
		return sr.Scene(p)
	}
	return nil, fmt.Errorf("scene %q requires a SceneResolver", p)
}

func (r *liveTilesetResolver) Material(p string) (*resource.Material, error) {
	if mr, ok := r.inner.(sceneformat.MaterialResolver); ok {
		return mr.Material(p)
	}
	return nil, fmt.Errorf("material %q requires a MaterialResolver", p)
}

func (r *liveTilesetResolver) PhysicsMaterial(p string) (*resource.PhysicsMaterial, error) {
	if pmr, ok := r.inner.(sceneformat.PhysicsMaterialResolver); ok {
		return pmr.PhysicsMaterial(p)
	}
	return nil, fmt.Errorf("physics material %q requires a PhysicsMaterialResolver", p)
}

func (r *liveTilesetResolver) Particles(p string) (particles.Config, error) {
	if pr, ok := r.inner.(sceneformat.ParticlesResolver); ok {
		return pr.Particles(p)
	}
	fr, ok := r.inner.(sceneformat.FileResolver)
	if !ok {
		return particles.Config{}, fmt.Errorf("particles %q requires a ParticlesResolver or FileResolver (such as FSAssets)", p)
	}
	data, err := fr.ReadFile(p)
	if err != nil {
		return particles.Config{}, fmt.Errorf("read particles %q: %w", p, err)
	}
	cfg, err := particles.UnmarshalConfigYAML(data)
	if err != nil {
		return particles.Config{}, loaderr.WithFile(err, p)
	}
	return cfg, nil
}

func (r *liveTilesetResolver) Skeleton(p string) (*skeletal.SkeletonData, error) {
	if sr, ok := r.inner.(sceneformat.SkeletonResolver); ok {
		return sr.Skeleton(p)
	}
	fr, ok := r.inner.(sceneformat.FileResolver)
	if !ok {
		return nil, fmt.Errorf("skeletal rig %q requires a SkeletonResolver or FileResolver (such as FSAssets)", p)
	}
	raw, err := fr.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read skeletal rig %q: %w", p, err)
	}
	sd, err := skeletal.Load(raw)
	if err != nil {
		return nil, loaderr.WithFile(err, p)
	}
	return sd, nil
}

func (r *liveTilesetResolver) Model(p string) (*gltf.Document, error) {
	if mr, ok := r.inner.(sceneformat.ModelResolver); ok {
		return mr.Model(p)
	}
	fr, ok := r.inner.(sceneformat.FileResolver)
	if !ok {
		return nil, fmt.Errorf("model %q requires a ModelResolver or FileResolver", p)
	}
	return gltf.LoadFS(editorFileResolverFS{fr: fr}, p)
}

func (r *liveTilesetResolver) Terrain(p string) (*world3d.Heightmap, error) {
	if tr, ok := r.inner.(sceneformat.TerrainResolver); ok {
		return tr.Terrain(p)
	}
	fr, ok := r.inner.(sceneformat.FileResolver)
	if !ok {
		return nil, fmt.Errorf("terrain %q requires a TerrainResolver or FileResolver", p)
	}
	hm, _, err := world3d.LoadTerrainFS(editorFileResolverFS{fr: fr}, p)
	return hm, err
}

type liveTilesetWithTilemapResolver struct {
	*liveTilesetResolver
	tr sceneformat.TilemapResolver
}

func (r *liveTilesetWithTilemapResolver) Tilemap(source string) (*sceneformat.TilemapDoc, error) {
	return r.tr.Tilemap(source)
}
