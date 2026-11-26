package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoGridMap is returned when a GridMap operation is called while no GridMap is open in the editor.
var ErrNoGridMap = errors.New("editor: no grid map is open")

// ErrNoGridMapFile is returned by SaveGridMap while the open GridMap has no file path and no OnSaveGridMap callback.
var ErrNoGridMapFile = errors.New("editor: the grid map has no file; save it under a path first")

// GridMapTool selects the active 3D GridMap editing tool in the 3D viewport.
type GridMapTool int

const (
	// GridMapToolNone leaves the 3D viewport in normal object selection and transform gizmo mode.
	GridMapToolNone GridMapTool = iota
	// GridMapToolPaint paints the active mesh library item at the active orientation onto cells on the current floor height.
	GridMapToolPaint
	// GridMapToolErase clears cells on the current floor height.
	GridMapToolErase
	// GridMapToolPick picks the tile item and orientation from the cell under the pointer and switches to Paint.
	GridMapToolPick
	// GridMapToolBoxFill drags a box on the current floor height and fills it with the active item and orientation.
	GridMapToolBoxFill
)

// String returns a human-readable label for the GridMap tool.
func (t GridMapTool) String() string {
	switch t {
	case GridMapToolNone:
		return "None"
	case GridMapToolPaint:
		return "Paint"
	case GridMapToolErase:
		return "Erase"
	case GridMapToolPick:
		return "Pick"
	case GridMapToolBoxFill:
		return "Box Fill"
	default:
		return "Unknown"
	}
}

type gridMapCellChange struct {
	coord   world3d.GridCoord
	hadOld  bool
	oldCell world3d.GridCell
	hasNew  bool
	newCell world3d.GridCell
}

type gridMapStroke struct {
	tool      GridMapTool
	floor     int
	tileID    uint16
	orient    uint8
	vpW       float32
	vpH       float32
	lastCoord world3d.GridCoord
	hasLast   bool
	boxStart  world3d.GridCoord
	boxEnd    world3d.GridCoord
	hasBox    bool
	changes   []gridMapCellChange
	changed   map[world3d.GridCoord]bool
	picked    bool
	pickedAt  world3d.GridCoord
	hasPicked bool
}

type gridMapHover struct {
	on      bool
	sx, sy  float32
	w, h    float32
	picking bool
}

type gridMapState struct {
	grid         *world3d.GridMap
	docName      string
	meshLibrary  string
	gltfDoc      *gltf.Document
	filePath     string
	relPath      string
	objectID     string
	saved        []byte
	tool         GridMapTool
	floor        int
	activeTileID uint16
	orientation  uint8
	stroke       *gridMapStroke
	hover        gridMapHover
	loadErr      error
}

// GridMap returns the active editable *world3d.GridMap, or nil when none is open.
func (e *Editor) GridMap() *world3d.GridMap {
	if e == nil {
		return nil
	}
	return e.gridMap.grid
}

// GridMapPath returns the project-relative path (or OS file path) of the open GridMap, or empty when none is open.
func (e *Editor) GridMapPath() string {
	if e == nil {
		return ""
	}
	if e.gridMap.relPath != "" {
		return e.gridMap.relPath
	}
	return e.gridMap.filePath
}

// GridMapMeshLibrary returns the mesh library path of the open GridMap, or empty when none is open.
func (e *Editor) GridMapMeshLibrary() string {
	if e == nil {
		return ""
	}
	return e.gridMap.meshLibrary
}

// GridMapTool returns the active 3D GridMap editing tool.
func (e *Editor) GridMapTool() GridMapTool {
	if e == nil {
		return GridMapToolNone
	}
	return e.gridMap.tool
}

// GridMapFloor returns the active integer floor height (Y coordinate) edited by GridMap tools.
func (e *Editor) GridMapFloor() int {
	if e == nil {
		return 0
	}
	return e.gridMap.floor
}

// ActiveGridMapTile returns the currently selected MeshLibrary tile ID for GridMap painting, or 0 when none is selected.
func (e *Editor) ActiveGridMapTile() uint16 {
	if e == nil {
		return 0
	}
	return e.gridMap.activeTileID
}

// GridMapOrientation returns the currently active 3D orientation index in 0..23 for GridMap painting.
func (e *Editor) GridMapOrientation() uint8 {
	if e == nil {
		return 0
	}
	return e.gridMap.orientation % world3d.GridOrientationCount
}

// SetGridMapTool switches the active 3D GridMap tool. Choosing a paint, erase, pick, or box-fill tool
// finishes any active stroke, cancels any 3D gizmo drag, and automatically opens the selected object's
// GridMap (or the first GridMap object in the scene) when no GridMap is open yet.
func (e *Editor) SetGridMapTool(tool GridMapTool) {
	if e == nil || tool < GridMapToolNone || tool > GridMapToolBoxFill {
		return
	}
	if e.gridMap.tool == tool {
		return
	}
	e.endGridMapStrokeForChange()
	e.CancelGizmoDrag3D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.gridMap.tool = tool
	if tool != GridMapToolNone && e.gridMap.grid == nil {
		e.autoOpenSceneGridMap()
	}
	if tool == GridMapToolNone {
		e.Status = "Gizmo: " + e.GizmoMode.String()
	} else {
		e.Status = "GridMap: " + tool.String()
	}
	e.notify()
}

// SetGridMapFloor sets the current floor height (integer Y coordinate) used by GridMap painting, erasing, picking, and box fill.
func (e *Editor) SetGridMapFloor(y int) {
	if e == nil || e.gridMap.floor == y {
		return
	}
	e.endGridMapStrokeForChange()
	e.gridMap.floor = y
	e.Status = fmt.Sprintf("GridMap floor Y = %d", y)
	e.notify()
}

// StepGridMapFloor raises or lowers the active GridMap floor height by delta.
func (e *Editor) StepGridMapFloor(delta int) {
	if e == nil || delta == 0 {
		return
	}
	e.SetGridMapFloor(e.gridMap.floor + delta)
}

// SetActiveGridMapTile selects the MeshLibrary tile ID used by GridMap Paint and Box Fill tools.
func (e *Editor) SetActiveGridMapTile(tileID uint16) {
	if e == nil || tileID == 0 {
		return
	}
	if e.gridMap.grid != nil && e.gridMap.grid.Library != nil {
		if _, ok := e.gridMap.grid.Library.Get(tileID); !ok {
			return
		}
	}
	if e.gridMap.activeTileID == tileID {
		return
	}
	e.gridMap.activeTileID = tileID
	if e.gridMap.grid != nil && e.gridMap.grid.Library != nil {
		if def, ok := e.gridMap.grid.Library.Get(tileID); ok && def.Name != "" {
			e.Status = "GridMap item: " + def.Name
		}
	}
	e.notify()
}

// SetActiveGridMapTileByName selects the MeshLibrary item named name and reports whether it was found.
func (e *Editor) SetActiveGridMapTileByName(name string) bool {
	if e == nil || e.gridMap.grid == nil || e.gridMap.grid.Library == nil {
		return false
	}
	def, ok := e.gridMap.grid.Library.ByName(name)
	if !ok {
		return false
	}
	e.SetActiveGridMapTile(def.ID)
	return true
}

// SetGridMapOrientation sets the active GridMap item orientation (0..23) as an undoable command.
func (e *Editor) SetGridMapOrientation(orient uint8) error {
	if e == nil {
		return ErrNoGridMap
	}
	if e.playing {
		return ErrPlaying
	}
	orient %= world3d.GridOrientationCount
	if e.gridMap.orientation == orient {
		return nil
	}
	e.History.Execute(&gridMapOrientCmd{
		ed:        e,
		oldOrient: e.gridMap.orientation,
		newOrient: orient,
		label:     fmt.Sprintf("Rotate GridMap item (%d)", orient),
	})
	return nil
}

// RotateGridMapOrientation rotates the active GridMap item orientation by quarterTurns 90-degree steps
// around axis (world X, Y, or Z) through all 24 cube orientations, recorded as an undoable command.
func (e *Editor) RotateGridMapOrientation(axis world3d.GridAxis, quarterTurns int) error {
	if e == nil {
		return ErrNoGridMap
	}
	if e.playing {
		return ErrPlaying
	}
	next := world3d.RotateGridOrientation(e.gridMap.orientation, axis, quarterTurns)
	if next == e.gridMap.orientation {
		return nil
	}
	axisName := "Y"
	switch axis {
	case world3d.GridAxisX:
		axisName = "X"
	case world3d.GridAxisZ:
		axisName = "Z"
	}
	e.History.Execute(&gridMapOrientCmd{
		ed:        e,
		oldOrient: e.gridMap.orientation,
		newOrient: next,
		label:     fmt.Sprintf("Rotate GridMap item %s (%d)", axisName, next),
	})
	return nil
}

// StepGridMapOrientation advances the active GridMap item orientation modulo 24 by delta as an undoable command.
func (e *Editor) StepGridMapOrientation(delta int) error {
	if e == nil {
		return ErrNoGridMap
	}
	if e.playing {
		return ErrPlaying
	}
	n := world3d.GridOrientationCount
	next := uint8(((int(e.gridMap.orientation)+delta)%n + n) % n)
	return e.SetGridMapOrientation(next)
}

// OpenGridMap opens a *.gridmap.yaml file by its slash-separated path from the open project root
// (or from GridMapFS, or an OS path on disk when no project is open), loads its glTF mesh library,
// and prepares it for painting in the 3D viewport.
func (e *Editor) OpenGridMap(relOrPath string) error {
	if e == nil {
		return ErrNoGridMap
	}
	e.endGridMapStrokeForChange()
	var (
		fsys     fs.FS
		fsName   string
		filePath string
		relPath  string
	)
	switch {
	case e.Project != nil && e.ProjectPath != "" && validScenePath(relOrPath):
		root := filepath.Dir(e.ProjectPath)
		fsys = os.DirFS(root)
		fsName = relOrPath
		relPath = relOrPath
		filePath = filepath.Join(root, filepath.FromSlash(relOrPath))
	case e.GridMapFS != nil && validScenePath(relOrPath):
		fsys = e.GridMapFS
		fsName = relOrPath
		relPath = relOrPath
	default:
		abs, err := filepath.Abs(relOrPath)
		if err != nil {
			return err
		}
		filePath = abs
		if rel, ok := e.projectRel(abs); ok {
			fsys = os.DirFS(filepath.Dir(e.ProjectPath))
			fsName = rel
			relPath = rel
		} else {
			fsys = os.DirFS(filepath.Dir(abs))
			fsName = filepath.Base(abs)
		}
	}
	return e.openGridMapFromFS(fsys, fsName, filePath, relPath, "")
}

// OpenGridMapFS loads the GridMap at name from fsys and makes it the active editable GridMap.
func (e *Editor) OpenGridMapFS(fsys fs.FS, name string) error {
	if e == nil {
		return ErrNoGridMap
	}
	if fsys == nil {
		return errors.New("editor: nil filesystem")
	}
	e.endGridMapStrokeForChange()
	e.GridMapFS = fsys
	return e.openGridMapFromFS(fsys, name, "", name, "")
}

// OpenGridMapDoc opens doc with lib as the active editable GridMap in memory, optionally associated with filePath for saving.
func (e *Editor) OpenGridMapDoc(doc *world3d.GridMapDoc, lib *world3d.MeshLibrary, filePath string) error {
	if e == nil {
		return ErrNoGridMap
	}
	if doc == nil {
		return errors.New("editor: nil GridMapDoc")
	}
	e.endGridMapStrokeForChange()
	if lib == nil {
		lib = world3d.NewMeshLibrary()
	}
	gm, err := doc.Build(lib)
	if err != nil {
		return err
	}
	relPath := ""
	if r, ok := e.projectRel(filePath); ok {
		relPath = r
	} else if validScenePath(filePath) {
		relPath = filePath
	}
	saved, err := encodeGridMapFromState(gm, doc.Name, doc.MeshLibrary)
	if err != nil {
		return err
	}
	e.gridMap.grid = gm
	e.gridMap.docName = doc.Name
	e.gridMap.meshLibrary = doc.MeshLibrary
	e.gridMap.gltfDoc = nil
	e.gridMap.filePath = filePath
	e.gridMap.relPath = relPath
	e.gridMap.objectID = e.findObjectForGridMap(relPath)
	e.gridMap.saved = saved
	e.gridMap.loadErr = nil
	e.keepGridMapTileInLibrary()
	e.Status = "Opened grid map"
	if e.GridMapPath() != "" {
		e.Status = "Opened " + e.GridMapPath()
	}
	e.notify()
	return nil
}

// NewGridMap creates a new empty GridMap using meshLibrary and cellSize, ready to paint and save to relPath.
func (e *Editor) NewGridMap(relPath, meshLibrary string, cellSize math3d.Vec3) error {
	if e == nil {
		return ErrNoGridMap
	}
	if cellSize.X <= 0 {
		cellSize.X = 1
	}
	if cellSize.Y <= 0 {
		cellSize.Y = 1
	}
	if cellSize.Z <= 0 {
		cellSize.Z = 1
	}
	e.endGridMapStrokeForChange()
	var (
		fsys     fs.FS
		filePath string
	)
	if e.Project != nil && e.ProjectPath != "" {
		root := filepath.Dir(e.ProjectPath)
		fsys = os.DirFS(root)
		if validScenePath(relPath) {
			filePath = filepath.Join(root, filepath.FromSlash(relPath))
		}
	} else if e.GridMapFS != nil {
		fsys = e.GridMapFS
	}
	var lib *world3d.MeshLibrary
	var gdoc *gltf.Document
	if meshLibrary != "" {
		if fsys == nil {
			return fmt.Errorf("editor: cannot load mesh library %q without a project or GridMapFS", meshLibrary)
		}
		var err error
		lib, gdoc, err = loadGridMapLibrary(fsys, meshLibrary)
		if err != nil {
			return err
		}
	} else {
		lib = world3d.NewMeshLibrary()
	}
	cs := [3]float32{cellSize.X, cellSize.Y, cellSize.Z}
	doc := &world3d.GridMapDoc{
		Version:     world3d.GridMapFormatVersion,
		CellSize:    &cs,
		MeshLibrary: meshLibrary,
	}
	gm, err := doc.Build(lib)
	if err != nil {
		return err
	}
	saved, err := encodeGridMapFromState(gm, "", meshLibrary)
	if err != nil {
		return err
	}
	e.gridMap.grid = gm
	e.gridMap.docName = ""
	e.gridMap.meshLibrary = meshLibrary
	e.gridMap.gltfDoc = gdoc
	e.gridMap.filePath = filePath
	e.gridMap.relPath = relPath
	e.gridMap.objectID = e.findObjectForGridMap(relPath)
	e.gridMap.saved = saved
	e.gridMap.loadErr = nil
	e.keepGridMapTileInLibrary()
	e.Status = "New grid map " + relPath
	e.notify()
	return nil
}

// CloseGridMap closes the active GridMap editing session.
func (e *Editor) CloseGridMap() {
	if e == nil {
		return
	}
	e.endGridMapStrokeForChange()
	e.gridMap = gridMapState{}
	e.notify()
}

func (e *Editor) openGridMapFromFS(fsys fs.FS, name, filePath, relPath, objectID string) error {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		e.gridMap.loadErr = err
		return fmt.Errorf("editor: read grid map %q: %w", name, err)
	}
	doc, err := world3d.ParseGridMapWith(data, name)
	if err != nil {
		e.gridMap.loadErr = err
		return err
	}
	var lib *world3d.MeshLibrary
	var gdoc *gltf.Document
	if doc.MeshLibrary != "" {
		lib, gdoc, err = loadGridMapLibrary(fsys, doc.MeshLibrary)
		if err != nil {
			e.gridMap.loadErr = err
			return err
		}
	} else {
		lib = world3d.NewMeshLibrary()
	}
	gm, err := doc.Build(lib)
	if err != nil {
		e.gridMap.loadErr = err
		return err
	}
	saved, err := encodeGridMapFromState(gm, doc.Name, doc.MeshLibrary)
	if err != nil {
		e.gridMap.loadErr = err
		return err
	}
	if objectID == "" {
		objectID = e.findObjectForGridMap(relPath)
	}
	e.gridMap.grid = gm
	e.gridMap.docName = doc.Name
	e.gridMap.meshLibrary = doc.MeshLibrary
	e.gridMap.gltfDoc = gdoc
	e.gridMap.filePath = filePath
	e.gridMap.relPath = relPath
	e.gridMap.objectID = objectID
	e.gridMap.saved = saved
	e.gridMap.loadErr = nil
	e.keepGridMapTileInLibrary()
	e.Status = "Opened " + name
	e.notify()
	return nil
}

func loadGridMapLibrary(fsys fs.FS, libPath string) (*world3d.MeshLibrary, *gltf.Document, error) {
	gltfBytes, err := fs.ReadFile(fsys, libPath)
	if err != nil {
		return nil, nil, fmt.Errorf("editor: read mesh library %q: %w", libPath, err)
	}
	lib, err := world3d.LoadMeshLibraryFromGLTF(gltfBytes)
	if err != nil {
		return nil, nil, err
	}
	// Full gltf.LoadFS gives real triangle geometry for 3D palette thumbnails when buffers are present;
	// JSON-only .gltf libraries without external .bin buffers fall back cleanly to LocalBounds thumbnails.
	gdoc, _ := gltf.LoadFS(fsys, libPath)
	return lib, gdoc, nil
}

func (e *Editor) keepGridMapTileInLibrary() {
	if e.gridMap.grid == nil || e.gridMap.grid.Library == nil {
		e.gridMap.activeTileID = 0
		return
	}
	items := e.gridMap.grid.Library.Items()
	if len(items) == 0 {
		e.gridMap.activeTileID = 0
		return
	}
	if _, ok := e.gridMap.grid.Library.Get(e.gridMap.activeTileID); ok {
		return
	}
	e.gridMap.activeTileID = items[0].ID
}

func (e *Editor) findObjectForGridMap(rel string) string {
	if e.Doc == nil || rel == "" {
		return ""
	}
	if obj, _ := e.SelectedObject(); obj != nil && obj.GridMap == rel {
		return obj.ID
	}
	var found string
	for li := range e.Doc.Layers {
		sceneformat.WalkObjects(e.Doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if o.GridMap == rel {
				found = o.ID
				return false
			}
			return true
		})
		if found != "" {
			return found
		}
	}
	return ""
}

// autoOpenSceneGridMap opens the GridMap named by the selected object (or the first GridMap object in the scene) when available.
func (e *Editor) autoOpenSceneGridMap() {
	if e.Doc == nil {
		return
	}
	var target *sceneformat.ObjectDoc
	if obj, _ := e.SelectedObject(); obj != nil && obj.GridMap != "" {
		target = obj
	}
	if target == nil {
		for li := range e.Doc.Layers {
			sceneformat.WalkObjects(e.Doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
				if o.GridMap != "" {
					target = o
					return false
				}
				return true
			})
			if target != nil {
				break
			}
		}
	}
	if target == nil {
		return
	}
	if e.gridMap.grid != nil && e.gridMap.relPath == target.GridMap {
		e.gridMap.objectID = target.ID
		return
	}
	if e.gridMap.grid != nil && e.GridMapDirty() {
		return
	}
	var fsys fs.FS
	var filePath string
	switch {
	case e.Project != nil && e.ProjectPath != "":
		root := filepath.Dir(e.ProjectPath)
		fsys = os.DirFS(root)
		filePath = filepath.Join(root, filepath.FromSlash(target.GridMap))
	case e.GridMapFS != nil:
		fsys = e.GridMapFS
	default:
		return
	}
	_ = e.openGridMapFromFS(fsys, target.GridMap, filePath, target.GridMap, target.ID)
}

func encodeGridMapFromState(gm *world3d.GridMap, name, meshLibrary string) ([]byte, error) {
	doc, err := gm.ToDoc(name, meshLibrary)
	if err != nil {
		return nil, err
	}
	return world3d.EncodeGridMap(doc)
}

// GridMapDoc exports the active editable GridMap as a canonical *world3d.GridMapDoc.
func (e *Editor) GridMapDoc() (*world3d.GridMapDoc, error) {
	if e == nil || e.gridMap.grid == nil {
		return nil, ErrNoGridMap
	}
	return e.gridMap.grid.ToDoc(e.gridMap.docName, e.gridMap.meshLibrary)
}

// GridMapYAML encodes the active editable GridMap as deterministic canonical YAML that round-trips byte-for-byte.
func (e *Editor) GridMapYAML() ([]byte, error) {
	if e == nil || e.gridMap.grid == nil {
		return nil, ErrNoGridMap
	}
	return encodeGridMapFromState(e.gridMap.grid, e.gridMap.docName, e.gridMap.meshLibrary)
}

// GridMapDirty reports whether the open GridMap differs from what it was opened from or last saved to.
func (e *Editor) GridMapDirty() bool {
	if e == nil || e.gridMap.grid == nil {
		return false
	}
	data, err := e.GridMapYAML()
	return err != nil || !bytes.Equal(data, e.gridMap.saved)
}

// SaveGridMap encodes the open GridMap as canonical YAML and writes it through OnSaveGridMap when set,
// or atomically to its file path otherwise.
func (e *Editor) SaveGridMap() error {
	if e == nil || e.gridMap.grid == nil {
		return ErrNoGridMap
	}
	if e.playing {
		return ErrPlaying
	}
	e.endGridMapStrokeForChange()
	data, err := e.GridMapYAML()
	if err != nil {
		return err
	}
	switch {
	case e.OnSaveGridMap != nil:
		if err := e.OnSaveGridMap(data); err != nil {
			return err
		}
		e.gridMap.saved = data
		e.invalidateProjectIndex()
		e.Status = "Saved grid map"
		e.notify()
		e.CheckProblems()
		return nil
	case e.gridMap.filePath != "":
		return e.SaveGridMapFile(e.gridMap.filePath)
	default:
		return ErrNoGridMapFile
	}
}

// SaveGridMapFile encodes the open GridMap as canonical YAML and writes it atomically to p.
func (e *Editor) SaveGridMapFile(p string) error {
	if e == nil || e.gridMap.grid == nil {
		return ErrNoGridMap
	}
	if e.playing {
		return ErrPlaying
	}
	e.endGridMapStrokeForChange()
	data, err := e.GridMapYAML()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(p, data); err != nil {
		return err
	}
	e.gridMap.filePath = p
	if rel, ok := e.projectRel(p); ok {
		e.gridMap.relPath = rel
	}
	e.gridMap.saved = data
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = "Saved " + e.GridMapPath()
	e.notify()
	e.CheckProblems()
	return nil
}

// --- Direct undoable GridMap cell edits ---

// PaintGridMapCell sets cell c in the open GridMap to tileID with orientation orient (0..23) as an undoable command.
func (e *Editor) PaintGridMapCell(c world3d.GridCoord, tileID uint16, orient uint8) error {
	if e == nil || e.gridMap.grid == nil {
		return ErrNoGridMap
	}
	if e.playing {
		return ErrPlaying
	}
	if tileID == 0 {
		return e.EraseGridMapCell(c)
	}
	if e.gridMap.grid.Library == nil {
		return errors.New("editor: grid map has no mesh library")
	}
	if _, ok := e.gridMap.grid.Library.Get(tileID); !ok {
		return fmt.Errorf("editor: grid map mesh library has no tile ID %d", tileID)
	}
	orient %= world3d.GridOrientationCount
	oldCell, hadOld := e.gridMap.grid.Cell(c)
	newCell := world3d.GridCell{TileID: tileID, Orientation: orient}
	if hadOld && oldCell == newCell {
		return nil
	}
	e.History.Execute(&gridMapEditCmd{
		ed:    e,
		label: fmt.Sprintf("Paint GridMap (%d,%d,%d)", c.X, c.Y, c.Z),
		changes: []gridMapCellChange{{
			coord:   c,
			hadOld:  hadOld,
			oldCell: oldCell,
			hasNew:  true,
			newCell: newCell,
		}},
	})
	return nil
}

// EraseGridMapCell clears cell c in the open GridMap as an undoable command.
func (e *Editor) EraseGridMapCell(c world3d.GridCoord) error {
	if e == nil || e.gridMap.grid == nil {
		return ErrNoGridMap
	}
	if e.playing {
		return ErrPlaying
	}
	oldCell, hadOld := e.gridMap.grid.Cell(c)
	if !hadOld {
		return nil
	}
	e.History.Execute(&gridMapEditCmd{
		ed:    e,
		label: fmt.Sprintf("Erase GridMap (%d,%d,%d)", c.X, c.Y, c.Z),
		changes: []gridMapCellChange{{
			coord:   c,
			hadOld:  true,
			oldCell: oldCell,
			hasNew:  false,
		}},
	})
	return nil
}

// FillGridMapBox fills every cell in the inclusive box [minC, maxC] with tileID and orient (0..23)
// in a single undoable command. Setting tileID to 0 clears the box.
func (e *Editor) FillGridMapBox(minC, maxC world3d.GridCoord, tileID uint16, orient uint8) error {
	if e == nil || e.gridMap.grid == nil {
		return ErrNoGridMap
	}
	if e.playing {
		return ErrPlaying
	}
	if tileID != 0 {
		if e.gridMap.grid.Library == nil {
			return errors.New("editor: grid map has no mesh library")
		}
		if _, ok := e.gridMap.grid.Library.Get(tileID); !ok {
			return fmt.Errorf("editor: grid map mesh library has no tile ID %d", tileID)
		}
	}
	orient %= world3d.GridOrientationCount
	x0, x1 := min(minC.X, maxC.X), max(minC.X, maxC.X)
	y0, y1 := min(minC.Y, maxC.Y), max(minC.Y, maxC.Y)
	z0, z1 := min(minC.Z, maxC.Z), max(minC.Z, maxC.Z)
	const maxBoxCells = 1 << 16
	if int64(x1-x0+1)*int64(y1-y0+1)*int64(z1-z0+1) > maxBoxCells {
		return fmt.Errorf("editor: grid map box fill exceeds %d cells", maxBoxCells)
	}
	var changes []gridMapCellChange
	for y := y0; y <= y1; y++ {
		for z := z0; z <= z1; z++ {
			for x := x0; x <= x1; x++ {
				c := world3d.GridCoord{X: x, Y: y, Z: z}
				oldCell, hadOld := e.gridMap.grid.Cell(c)
				if tileID == 0 {
					if !hadOld {
						continue
					}
					changes = append(changes, gridMapCellChange{coord: c, hadOld: true, oldCell: oldCell, hasNew: false})
				} else {
					newCell := world3d.GridCell{TileID: tileID, Orientation: orient}
					if hadOld && oldCell == newCell {
						continue
					}
					changes = append(changes, gridMapCellChange{coord: c, hadOld: hadOld, oldCell: oldCell, hasNew: true, newCell: newCell})
				}
			}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	e.History.Execute(&gridMapEditCmd{
		ed:      e,
		label:   fmt.Sprintf("Box fill %d GridMap cells", len(changes)),
		changes: changes,
	})
	return nil
}

// RotateGridMapCell rotates the occupied cell at c by quarterTurns 90-degree steps around axis
// as an undoable command.
func (e *Editor) RotateGridMapCell(c world3d.GridCoord, axis world3d.GridAxis, quarterTurns int) error {
	if e == nil || e.gridMap.grid == nil {
		return ErrNoGridMap
	}
	if e.playing {
		return ErrPlaying
	}
	oldCell, hadOld := e.gridMap.grid.Cell(c)
	if !hadOld {
		return nil
	}
	nextOrient := world3d.RotateGridOrientation(oldCell.Orientation, axis, quarterTurns)
	if nextOrient == oldCell.Orientation {
		return nil
	}
	newCell := world3d.GridCell{TileID: oldCell.TileID, Orientation: nextOrient}
	e.History.Execute(&gridMapEditCmd{
		ed:    e,
		label: fmt.Sprintf("Rotate GridMap cell (%d,%d,%d)", c.X, c.Y, c.Z),
		changes: []gridMapCellChange{{
			coord:   c,
			hadOld:  true,
			oldCell: oldCell,
			hasNew:  true,
			newCell: newCell,
		}},
	})
	return nil
}

// PickGridMapCell makes the item and orientation stored at cell c active, returning true if the cell was occupied.
func (e *Editor) PickGridMapCell(c world3d.GridCoord) bool {
	if e == nil || e.playing || e.gridMap.grid == nil {
		return false
	}
	cell, ok := e.gridMap.grid.Cell(c)
	if !ok || cell.TileID == 0 {
		e.Status = fmt.Sprintf("GridMap (%d,%d,%d) is empty", c.X, c.Y, c.Z)
		e.notify()
		return false
	}
	e.gridMap.activeTileID = cell.TileID
	e.gridMap.orientation = cell.Orientation % world3d.GridOrientationCount
	label := fmt.Sprintf("tile %d", cell.TileID)
	if e.gridMap.grid.Library != nil {
		if def, exists := e.gridMap.grid.Library.Get(cell.TileID); exists && def.Name != "" {
			label = def.Name
		}
	}
	e.Status = fmt.Sprintf("Picked %s (orient %d)", label, e.gridMap.orientation)
	e.notify()
	return true
}

// --- Undo commands ---

type gridMapEditCmd struct {
	ed      *Editor
	label   string
	changes []gridMapCellChange
}

func (c *gridMapEditCmd) Label() string { return c.label }

func (c *gridMapEditCmd) Do() {
	gm := c.ed.gridMap.grid
	if gm == nil {
		return
	}
	for _, ch := range c.changes {
		if ch.hasNew {
			gm.SetCell(ch.coord, ch.newCell.TileID, ch.newCell.Orientation)
		} else {
			gm.ClearCell(ch.coord)
		}
	}
	c.ed.Status = c.label
}

func (c *gridMapEditCmd) Undo() {
	gm := c.ed.gridMap.grid
	if gm == nil {
		return
	}
	for i := len(c.changes) - 1; i >= 0; i-- {
		ch := c.changes[i]
		if ch.hadOld {
			gm.SetCell(ch.coord, ch.oldCell.TileID, ch.oldCell.Orientation)
		} else {
			gm.ClearCell(ch.coord)
		}
	}
}

type gridMapOrientCmd struct {
	ed        *Editor
	oldOrient uint8
	newOrient uint8
	label     string
}

func (c *gridMapOrientCmd) Label() string { return c.label }

func (c *gridMapOrientCmd) Do() {
	c.ed.gridMap.orientation = c.newOrient % world3d.GridOrientationCount
	c.ed.Status = c.label
}

func (c *gridMapOrientCmd) Undo() {
	c.ed.gridMap.orientation = c.oldOrient % world3d.GridOrientationCount
}

// --- 3D Ray-Floor Projection, Strokes, and Hover ---

func (e *Editor) gridMapObjectWorld() aff3 {
	if e == nil || e.gridMap.objectID == "" || e.Doc == nil {
		return ident3
	}
	obj, _, _ := e.FindObject(e.gridMap.objectID)
	if obj == nil {
		return ident3
	}
	return e.parentWorld3D(obj.ID).mul(local3(e.posed3D(obj)))
}

// ScreenToGridMapCell intersects the 3D camera ray at viewport pixel (sx, sy) with the active GridMap's
// current floor plane (Y = Origin.Y + floor*CellSize.Y) and returns the integer GridCoord on that floor.
func (e *Editor) ScreenToGridMapCell(sx, sy, w, h float32) (world3d.GridCoord, bool) {
	if e == nil || e.gridMap.grid == nil || w <= 0 || h <= 0 {
		return world3d.GridCoord{}, false
	}
	gm := e.gridMap.grid
	if gm.CellSize.X <= 0 || gm.CellSize.Y <= 0 || gm.CellSize.Z <= 0 {
		return world3d.GridCoord{}, false
	}
	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, w, h)
	orig, dir := vec64(orig32), vec64(dir32)
	if objWorld := e.gridMapObjectWorld(); objWorld != ident3 {
		inv, ok := objWorld.invert()
		if !ok {
			return world3d.GridCoord{}, false
		}
		orig = inv.point(orig)
		dir = normalize64(inv.vector(dir))
		if len64(dir) == 0 {
			return world3d.GridCoord{}, false
		}
	}
	planeY := float64(gm.Origin.Y) + float64(e.gridMap.floor)*float64(gm.CellSize.Y)
	if math.Abs(dir[1]) < 1e-4 {
		return world3d.GridCoord{}, false
	}
	t := (planeY - orig[1]) / dir[1]
	if t <= 0 || math.IsNaN(t) || math.IsInf(t, 0) {
		return world3d.GridCoord{}, false
	}
	hx := orig[0] + dir[0]*t
	hz := orig[2] + dir[2]*t
	cx := int(math.Floor((hx - float64(gm.Origin.X)) / float64(gm.CellSize.X)))
	cz := int(math.Floor((hz - float64(gm.Origin.Z)) / float64(gm.CellSize.Z)))
	return world3d.GridCoord{X: cx, Y: e.gridMap.floor, Z: cz}, true
}

func (e *Editor) raycastGridMapCell(sx, sy, w, h float32) (world3d.GridCoord, bool) {
	if e == nil || e.gridMap.grid == nil || w <= 0 || h <= 0 {
		return world3d.GridCoord{}, false
	}
	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, w, h)
	orig, dir := vec64(orig32), vec64(dir32)
	if objWorld := e.gridMapObjectWorld(); objWorld != ident3 {
		inv, ok := objWorld.invert()
		if !ok {
			return world3d.GridCoord{}, false
		}
		orig = inv.point(orig)
		dir = normalize64(inv.vector(dir))
	}
	ray := math3d.Ray{
		Origin: math3d.Vec3{X: float32(orig[0]), Y: float32(orig[1]), Z: float32(orig[2])},
		Dir:    math3d.Vec3{X: float32(dir[0]), Y: float32(dir[1]), Z: float32(dir[2])},
	}
	_, _, _, far := e.Camera3D.effectiveParams()
	if hit, ok := e.gridMap.grid.Raycast(ray, float32(far)); ok {
		return hit.Coord, true
	}
	return world3d.GridCoord{}, false
}

// SetGridMapHover updates the pointer location used for the 3D GridMap cell preview overlay.
func (e *Editor) SetGridMapHover(sx, sy, w, h float32, picking bool) {
	if e == nil {
		return
	}
	prevC, prevOK := e.HoveredGridMapCell()
	wasPick := e.gridMap.hover.picking
	e.gridMap.hover = gridMapHover{on: true, sx: sx, sy: sy, w: w, h: h, picking: picking}
	curC, curOK := e.HoveredGridMapCell()
	if curC != prevC || curOK != prevOK || wasPick != picking {
		e.notify()
	}
}

// ClearGridMapHover hides the 3D GridMap cell preview overlay when the pointer leaves the viewport.
func (e *Editor) ClearGridMapHover() {
	if e == nil || !e.gridMap.hover.on {
		return
	}
	_, wasOK := e.HoveredGridMapCell()
	e.gridMap.hover = gridMapHover{}
	if wasOK {
		e.notify()
	}
}

// HoveredGridMapCell returns the GridMap cell currently under the pointer on the active floor,
// or false when no GridMap tool is active, in 2D, while playing, or when the ray misses the floor plane.
func (e *Editor) HoveredGridMapCell() (world3d.GridCoord, bool) {
	if e == nil || !e.gridMap.hover.on || e.playing || e.ViewMode != ViewMode3D || e.gridMap.tool == GridMapToolNone || e.gridMap.grid == nil {
		return world3d.GridCoord{}, false
	}
	return e.ScreenToGridMapCell(e.gridMap.hover.sx, e.gridMap.hover.sy, e.gridMap.hover.w, e.gridMap.hover.h)
}

// GridMapStrokeActive reports whether a GridMap pointer stroke is in progress and returns its tool mode.
func (e *Editor) GridMapStrokeActive() (GridMapTool, bool) {
	if e == nil || e.gridMap.stroke == nil {
		return GridMapToolNone, false
	}
	return e.gridMap.stroke.tool, true
}

// GridMapBoxPreview returns the inclusive [minC, maxC] cell bounds of an active Box Fill drag, if any.
func (e *Editor) GridMapBoxPreview() (minC, maxC world3d.GridCoord, ok bool) {
	if e == nil || e.gridMap.stroke == nil || e.gridMap.stroke.tool != GridMapToolBoxFill || !e.gridMap.stroke.hasBox {
		return world3d.GridCoord{}, world3d.GridCoord{}, false
	}
	a, b := e.gridMap.stroke.boxStart, e.gridMap.stroke.boxEnd
	return world3d.GridCoord{X: min(a.X, b.X), Y: min(a.Y, b.Y), Z: min(a.Z, b.Z)},
		world3d.GridCoord{X: max(a.X, b.X), Y: max(a.Y, b.Y), Z: max(a.Z, b.Z)}, true
}

// BeginGridMapStroke starts an interactive GridMap stroke (Paint, Erase, Pick, or Box Fill) at viewport
// pixel (sx, sy) on the current floor height. Call ContinueGridMapStroke as the pointer moves and
// EndGridMapStroke on release to commit the entire stroke as a single undoable History step.
func (e *Editor) BeginGridMapStroke(tool GridMapTool, sx, sy, w, h float32) bool {
	if e == nil || e.playing || e.HistoryPreviewActive() || e.ViewMode != ViewMode3D || e.gridMap.stroke != nil || e.gridMap.grid == nil {
		return false
	}
	if tool <= GridMapToolNone || tool > GridMapToolBoxFill {
		return false
	}
	if tool == GridMapToolPaint || tool == GridMapToolBoxFill {
		if e.gridMap.activeTileID == 0 || e.gridMap.grid.Library == nil {
			return false
		}
		if _, ok := e.gridMap.grid.Library.Get(e.gridMap.activeTileID); !ok {
			return false
		}
	}
	coord, ok := e.ScreenToGridMapCell(sx, sy, w, h)
	if !ok && tool == GridMapToolPick {
		coord, ok = e.raycastGridMapCell(sx, sy, w, h)
	}
	if !ok {
		return false
	}
	st := &gridMapStroke{
		tool:    tool,
		floor:   e.gridMap.floor,
		tileID:  e.gridMap.activeTileID,
		orient:  e.gridMap.orientation % world3d.GridOrientationCount,
		vpW:     w,
		vpH:     h,
		changed: make(map[world3d.GridCoord]bool),
	}
	e.gridMap.stroke = st
	switch tool {
	case GridMapToolPick:
		e.gridMapPickCoord(st, coord, sx, sy, w, h)
	case GridMapToolBoxFill:
		st.boxStart = coord
		st.boxEnd = coord
		st.hasBox = true
		e.notify()
	default:
		st.lastCoord = coord
		st.hasLast = true
		e.gridMapApplyCell(st, coord)
	}
	return true
}

// ContinueGridMapStroke extends an active GridMap stroke to viewport pixel (sx, sy).
func (e *Editor) ContinueGridMapStroke(sx, sy float32) {
	if e == nil || e.gridMap.stroke == nil || e.gridMap.grid == nil {
		return
	}
	st := e.gridMap.stroke
	coord, ok := e.ScreenToGridMapCell(sx, sy, st.vpW, st.vpH)
	if !ok {
		if st.tool == GridMapToolPick {
			if rc, rok := e.raycastGridMapCell(sx, sy, st.vpW, st.vpH); rok {
				e.gridMapPickCoord(st, rc, sx, sy, st.vpW, st.vpH)
			}
		}
		return
	}
	switch st.tool {
	case GridMapToolPick:
		e.gridMapPickCoord(st, coord, sx, sy, st.vpW, st.vpH)
	case GridMapToolBoxFill:
		if !st.hasBox {
			st.boxStart = coord
			st.hasBox = true
		}
		if st.boxEnd != coord {
			st.boxEnd = coord
			e.notify()
		}
	default:
		if !st.hasLast {
			st.lastCoord = coord
			st.hasLast = true
			e.gridMapApplyCell(st, coord)
			return
		}
		if coord == st.lastCoord {
			return
		}
		changed := false
		for _, c := range gridLineXZ(st.lastCoord, coord) {
			if e.gridMapApplyCellSilent(st, c) {
				changed = true
			}
		}
		st.lastCoord = coord
		if changed {
			e.notify()
		}
	}
}

// EndGridMapStroke finishes an active GridMap stroke and commits all modified cells as one undoable History step.
func (e *Editor) EndGridMapStroke() {
	if e == nil || e.gridMap.stroke == nil {
		return
	}
	st := e.gridMap.stroke
	e.gridMap.stroke = nil
	switch st.tool {
	case GridMapToolPick:
		if st.picked && e.gridMap.tool == GridMapToolPick {
			e.SetGridMapTool(GridMapToolPaint)
		}
		return
	case GridMapToolBoxFill:
		if st.hasBox {
			_ = e.FillGridMapBox(st.boxStart, st.boxEnd, st.tileID, st.orient)
		}
		e.notify()
		return
	}
	if len(st.changes) == 0 {
		return
	}
	label := fmt.Sprintf("%s %d GridMap cells", st.tool, len(st.changes))
	if len(st.changes) == 1 {
		c := st.changes[0].coord
		label = fmt.Sprintf("%s GridMap (%d,%d,%d)", st.tool, c.X, c.Y, c.Z)
	}
	// Cells already hold their stroke values; Execute runs Do (idempotent) and records the undo step.
	e.History.Execute(&gridMapEditCmd{ed: e, label: label, changes: st.changes})
}

// CancelGridMapStroke aborts an active GridMap stroke and restores every cell it modified without recording history.
func (e *Editor) CancelGridMapStroke() {
	if e == nil || e.gridMap.stroke == nil {
		return
	}
	st := e.gridMap.stroke
	e.gridMap.stroke = nil
	if gm := e.gridMap.grid; gm != nil && len(st.changes) > 0 {
		for i := len(st.changes) - 1; i >= 0; i-- {
			ch := st.changes[i]
			if ch.hadOld {
				gm.SetCell(ch.coord, ch.oldCell.TileID, ch.oldCell.Orientation)
			} else {
				gm.ClearCell(ch.coord)
			}
		}
	}
	e.notify()
}

func (e *Editor) endGridMapStrokeForChange() {
	if e != nil && e.gridMap.stroke != nil {
		e.EndGridMapStroke()
	}
}

func (e *Editor) gridMapPickCoord(st *gridMapStroke, coord world3d.GridCoord, sx, sy, w, h float32) {
	if _, ok := e.gridMap.grid.Cell(coord); !ok {
		if rc, rok := e.raycastGridMapCell(sx, sy, w, h); rok {
			coord = rc
		}
	}
	if st.hasPicked && st.pickedAt == coord {
		return
	}
	st.pickedAt = coord
	st.hasPicked = true
	if e.PickGridMapCell(coord) {
		st.picked = true
	}
}

func (e *Editor) gridMapApplyCell(st *gridMapStroke, c world3d.GridCoord) {
	if e.gridMapApplyCellSilent(st, c) {
		e.notify()
	}
}

func (e *Editor) gridMapApplyCellSilent(st *gridMapStroke, c world3d.GridCoord) bool {
	gm := e.gridMap.grid
	if gm == nil || st.changed[c] {
		return false
	}
	oldCell, hadOld := gm.Cell(c)
	switch st.tool {
	case GridMapToolPaint:
		newCell := world3d.GridCell{TileID: st.tileID, Orientation: st.orient}
		if hadOld && oldCell == newCell {
			return false
		}
		st.changed[c] = true
		st.changes = append(st.changes, gridMapCellChange{
			coord:   c,
			hadOld:  hadOld,
			oldCell: oldCell,
			hasNew:  true,
			newCell: newCell,
		})
		gm.SetCell(c, newCell.TileID, newCell.Orientation)
		return true
	case GridMapToolErase:
		if !hadOld {
			return false
		}
		st.changed[c] = true
		st.changes = append(st.changes, gridMapCellChange{
			coord:   c,
			hadOld:  true,
			oldCell: oldCell,
			hasNew:  false,
		})
		gm.ClearCell(c)
		return true
	}
	return false
}

// gridLineXZ returns every cell on the XZ plane along the integer Bresenham line from a to b (at a.Y).
func gridLineXZ(a, b world3d.GridCoord) []world3d.GridCoord {
	x0, z0 := a.X, a.Z
	x1, z1 := b.X, b.Z
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dz := z1 - z0
	if dz < 0 {
		dz = -dz
	}
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sz := 1
	if z0 > z1 {
		sz = -1
	}
	err := dx - dz
	out := make([]world3d.GridCoord, 0, max(dx, dz)+1)
	for {
		out = append(out, world3d.GridCoord{X: x0, Y: a.Y, Z: z0})
		if x0 == x1 && z0 == z1 {
			break
		}
		e2 := 2 * err
		if e2 > -dz {
			err -= dz
			x0 += sx
		}
		if e2 < dx {
			err += dx
			z0 += sz
		}
	}
	return out
}

// --- 3D Viewport GridMap Rendering ---

func (e *Editor) drawGridMap3D(d *core.Drawing, w, h float32) {
	gm := e.gridMap.grid
	if gm == nil {
		return
	}
	objWorld := e.gridMapObjectWorld()
	toWorld := func(p math3d.Vec3) [3]float32 {
		v := [3]float64{float64(p.X), float64(p.Y), float64(p.Z)}
		if objWorld != ident3 {
			v = objWorld.point(v)
		}
		return vec32(v)
	}

	// 1. Active floor plane grid when a GridMap tool is active.
	if e.gridMap.tool != GridMapToolNone && !e.playing {
		e.drawGridMapFloor3D(d, gm, toWorld, w, h)
	}

	// 2. Occupied GridMap cells as oriented 3D wireframes with forward-direction ticks.
	type cellDraw struct {
		coord world3d.GridCoord
		cell  world3d.GridCell
		depth float32
	}
	eye := e.Camera3D.eye64()
	_, _, forward := e.Camera3D.basis64()
	var cells []cellDraw
	gm.EachCell(func(c world3d.GridCoord, cell world3d.GridCell) bool {
		xform := gm.CellTransform(c, cell.Orientation)
		center := toWorld(math3d.Vec3{X: xform[12], Y: xform[13], Z: xform[14]})
		depth := float32(dot64(sub64(vec64(center), eye), forward))
		cells = append(cells, cellDraw{coord: c, cell: cell, depth: depth})
		return true
	})
	slices.SortFunc(cells, func(a, b cellDraw) int {
		return cmp.Or(
			cmp.Compare(b.depth, a.depth),
			cmp.Compare(a.coord.Y, b.coord.Y),
			cmp.Compare(a.coord.Z, b.coord.Z),
			cmp.Compare(a.coord.X, b.coord.X),
		)
	})
	for _, item := range cells {
		col := gridMapTileColor(item.cell.TileID, 0.9)
		lw := float32(1.4)
		if e.gridMap.tool != GridMapToolNone && item.coord.Y != e.gridMap.floor {
			col = gridMapTileColor(item.cell.TileID, 0.45)
			lw = 1.0
		}
		e.drawGridMapCellBox3D(d, gm, item.coord, item.cell.TileID, item.cell.Orientation, toWorld, col, lw, w, h)
	}

	if e.playing || e.gridMap.tool == GridMapToolNone {
		return
	}

	// 3. Box Fill drag preview, or hovered cell preview under the pointer.
	if minC, maxC, ok := e.GridMapBoxPreview(); ok {
		bmin := gm.CellBounds(minC).Min
		bmax := gm.CellBounds(maxC).Max
		e.drawAABBWireframe3D(d, bmin, bmax, toWorld, hoverPaintColor, 2.2, w, h)
		e.fillCellFloorQuad3D(d, bmin, bmax, toWorld, core.RGBA(0x38BDF8, 0.22), w, h)
		return
	}
	if hc, ok := e.HoveredGridMapCell(); ok {
		col := hoverPaintColor
		fill := core.RGBA(0x38BDF8, 0.22)
		mode := e.gridMap.tool
		if e.gridMap.hover.picking {
			mode = GridMapToolPick
		}
		switch mode {
		case GridMapToolErase:
			col = hoverEraseColor
			fill = core.RGBA(0xF87171, 0.25)
		case GridMapToolPick:
			col = hoverPickColor
			fill = core.RGBA(0xFBBF24, 0.22)
		}
		cb := gm.CellBounds(hc)
		e.fillCellFloorQuad3D(d, cb.Min, cb.Max, toWorld, fill, w, h)
		if mode == GridMapToolPaint || mode == GridMapToolBoxFill {
			e.drawGridMapCellBox3D(d, gm, hc, e.gridMap.activeTileID, e.gridMap.orientation, toWorld, col, 2.2, w, h)
		} else {
			e.drawAABBWireframe3D(d, cb.Min, cb.Max, toWorld, col, 2.2, w, h)
		}
	}
}

func (e *Editor) drawGridMapFloor3D(d *core.Drawing, gm *world3d.GridMap, toWorld func(math3d.Vec3) [3]float32, w, h float32) {
	y := gm.Origin.Y + float32(e.gridMap.floor)*gm.CellSize.Y
	const half = 8
	centerX, centerZ := 0, 0
	if hc, ok := e.HoveredGridMapCell(); ok {
		centerX, centerZ = hc.X, hc.Z
	}
	xMin := gm.Origin.X + float32(centerX-half)*gm.CellSize.X
	xMax := gm.Origin.X + float32(centerX+half)*gm.CellSize.X
	zMin := gm.Origin.Z + float32(centerZ-half)*gm.CellSize.Z
	zMax := gm.Origin.Z + float32(centerZ+half)*gm.CellSize.Z

	d.Save().SetStrokeColor(core.RGBA(0x38BDF8, 0.32)).SetLineWidth(1).BeginPath()
	hasSeg := false
	for i := centerX - half; i <= centerX+half; i++ {
		x := gm.Origin.X + float32(i)*gm.CellSize.X
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(toWorld(math3d.Vec3{X: x, Y: y, Z: zMin}), toWorld(math3d.Vec3{X: x, Y: y, Z: zMax}), w, h); vis {
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasSeg = true
		}
	}
	for i := centerZ - half; i <= centerZ+half; i++ {
		z := gm.Origin.Z + float32(i)*gm.CellSize.Z
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(toWorld(math3d.Vec3{X: xMin, Y: y, Z: z}), toWorld(math3d.Vec3{X: xMax, Y: y, Z: z}), w, h); vis {
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasSeg = true
		}
	}
	if hasSeg {
		d.Stroke()
	}
	d.Restore()
}

func (e *Editor) drawGridMapCellBox3D(d *core.Drawing, gm *world3d.GridMap, c world3d.GridCoord, tileID uint16, orient uint8, toWorld func(math3d.Vec3) [3]float32, col core.Color, lw, w, h float32) {
	xform := gm.CellTransform(c, orient)
	local := math3d.AABB{
		Min: math3d.Vec3{X: -gm.CellSize.X * 0.5, Y: -gm.CellSize.Y * 0.5, Z: -gm.CellSize.Z * 0.5},
		Max: math3d.Vec3{X: gm.CellSize.X * 0.5, Y: gm.CellSize.Y * 0.5, Z: gm.CellSize.Z * 0.5},
	}
	if gm.Library != nil {
		if def, ok := gm.Library.Get(tileID); ok && !def.LocalBounds.Empty() {
			local = def.LocalBounds
		}
	}
	var corners [8][3]float32
	for i, s := range boxSigns3D {
		lx := local.Min.X
		if s[0] > 0 {
			lx = local.Max.X
		}
		ly := local.Min.Y
		if s[1] > 0 {
			ly = local.Max.Y
		}
		lz := local.Min.Z
		if s[2] > 0 {
			lz = local.Max.Z
		}
		p := xform.TransformPoint(math3d.Vec3{X: lx, Y: ly, Z: lz})
		corners[i] = toWorld(p)
	}
	d.Save().SetStrokeColor(col).SetLineWidth(lw).BeginPath()
	hasEdge := false
	for _, edge := range boxEdges3D {
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(corners[edge[0]], corners[edge[1]], w, h); vis {
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasEdge = true
		}
	}
	// Draw a local +Z orientation tick from cell center toward the cell's front face so the 24 orientations are visually distinguishable.
	center := toWorld(xform.TransformPoint(math3d.Vec3Zero()))
	fwdLen := min(gm.CellSize.X, min(gm.CellSize.Y, gm.CellSize.Z)) * 0.42
	front := toWorld(xform.TransformPoint(math3d.Vec3{Z: fwdLen}))
	if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(center, front, w, h); vis {
		d.MoveTo(x0, y0).LineTo(x1, y1)
		hasEdge = true
	}
	if hasEdge {
		d.Stroke()
	}
	d.Restore()
}

func (e *Editor) drawAABBWireframe3D(d *core.Drawing, bmin, bmax math3d.Vec3, toWorld func(math3d.Vec3) [3]float32, col core.Color, lw, w, h float32) {
	var corners [8][3]float32
	for i, s := range boxSigns3D {
		p := math3d.Vec3{X: bmin.X, Y: bmin.Y, Z: bmin.Z}
		if s[0] > 0 {
			p.X = bmax.X
		}
		if s[1] > 0 {
			p.Y = bmax.Y
		}
		if s[2] > 0 {
			p.Z = bmax.Z
		}
		corners[i] = toWorld(p)
	}
	d.Save().SetStrokeColor(col).SetLineWidth(lw).BeginPath()
	hasEdge := false
	for _, edge := range boxEdges3D {
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(corners[edge[0]], corners[edge[1]], w, h); vis {
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasEdge = true
		}
	}
	if hasEdge {
		d.Stroke()
	}
	d.Restore()
}

func (e *Editor) fillCellFloorQuad3D(d *core.Drawing, bmin, bmax math3d.Vec3, toWorld func(math3d.Vec3) [3]float32, col core.Color, w, h float32) {
	pts := [4]math3d.Vec3{
		{X: bmin.X, Y: bmin.Y, Z: bmin.Z},
		{X: bmax.X, Y: bmin.Y, Z: bmin.Z},
		{X: bmax.X, Y: bmin.Y, Z: bmax.Z},
		{X: bmin.X, Y: bmin.Y, Z: bmax.Z},
	}
	var sx, sy [4]float32
	for i, p := range pts {
		px, py, _, vis := e.Camera3D.Project(toWorld(p), w, h)
		if !vis {
			return
		}
		sx[i], sy[i] = px, py
	}
	d.Save().SetFillColor(col).BeginPath().
		MoveTo(sx[0], sy[0]).
		LineTo(sx[1], sy[1]).
		LineTo(sx[2], sy[2]).
		LineTo(sx[3], sy[3]).
		ClosePath().Fill().Restore()
}

var gridMapPaletteSwatches = []uint32{
	0x60A5FA, // blue
	0x34D399, // emerald
	0xFBBF24, // amber
	0xF472B6, // pink
	0xA78BFA, // violet
	0xFB923C, // orange
	0x2DD4BF, // teal
	0xE879F9, // fuchsia
}

func gridMapTileColor(tileID uint16, alpha float32) core.Color {
	if tileID == 0 {
		return core.RGBA(0x94A3B8, alpha)
	}
	rgb := gridMapPaletteSwatches[int(tileID-1)%len(gridMapPaletteSwatches)]
	return core.RGBA(rgb, alpha)
}

// --- Scene Object GridMap setter ---

// SetObjectGridMap sets the 3D grid map file (*.gridmap.yaml or *.gridmap.yml) object id draws,
// or "" to clear it, as an undoable command. It refuses a path sceneformat refuses, or combining
// a GridMap with Mesh or Model, and raises a version 1 scene to version 2 (restored on Undo).
func (e *Editor) SetObjectGridMap(id, gridMapPath string) error {
	if e.playing {
		return ErrPlaying
	}
	if gridMapPath != "" {
		if !validScenePath(gridMapPath) {
			return fmt.Errorf("editor: gridMap %q must be a slash-separated path relative to the project root", gridMapPath)
		}
		if !sceneformat.IsGridMapPath(gridMapPath) {
			return fmt.Errorf("editor: gridMap %q must end in .gridmap.yaml or .gridmap.yml", gridMapPath)
		}
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Set gridMap on "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if resolved.GridMap == gridMapPath {
					return nil
				}
				if gridMapPath != "" {
					if resolved.Mesh != "" {
						return fmt.Errorf("editor: object %q draws mesh %q; it cannot also draw gridMap %q", id, resolved.Mesh, gridMapPath)
					}
					if resolved.Model != "" {
						return fmt.Errorf("editor: object %q draws model %q; it cannot also draw gridMap %q", id, resolved.Model, gridMapPath)
					}
				}
				ov.GridMap = sceneformat.String(gridMapPath)
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.GridMap == gridMapPath {
		return nil
	}
	if gridMapPath != "" {
		if obj.Mesh != "" {
			return fmt.Errorf("editor: object %q draws mesh %q; it cannot also draw gridMap %q", id, obj.Mesh, gridMapPath)
		}
		if obj.Model != "" {
			return fmt.Errorf("editor: object %q draws model %q; it cannot also draw gridMap %q", id, obj.Model, gridMapPath)
		}
	}
	e.History.Execute(&gridMapObjectCmd{
		ed:          e,
		id:          id,
		oldGridMap:  obj.GridMap,
		newGridMap:  gridMapPath,
		prevVersion: e.Doc.Version,
	})
	return nil
}

type gridMapObjectCmd struct {
	ed          *Editor
	id          string
	oldGridMap  string
	newGridMap  string
	prevVersion int
}

func (c *gridMapObjectCmd) Label() string { return "Set gridMap on " + c.id }

func (c *gridMapObjectCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.id)
	if obj == nil {
		return
	}
	if c.newGridMap != "" && c.ed.Doc.Version < sceneformat.CurrentVersion {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	obj.GridMap = c.newGridMap
	c.ed.Status = "Updated gridMap on " + c.id
}

func (c *gridMapObjectCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.id)
	if obj == nil {
		return
	}
	obj.GridMap = c.oldGridMap
	c.ed.Doc.Version = c.prevVersion
}

// projectGridMaps returns the slash-separated paths of *.gridmap.yaml and *.gridmap.yml files in the open project.
func (e *Editor) projectGridMaps() []string {
	files, err := e.ProjectFiles()
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range files {
		if !f.Dir && sceneformat.IsGridMapPath(f.Path) {
			out = append(out, f.Path)
		}
	}
	slices.Sort(out)
	return out
}

func (e *Editor) followGridMapMove(moveFile func(*string), movePath func(*string)) {
	if e.gridMap.grid == nil {
		return
	}
	moveFile(&e.gridMap.filePath)
	movePath(&e.gridMap.relPath)
	movePath(&e.gridMap.meshLibrary)
	if len(e.gridMap.saved) > 0 {
		if doc, err := world3d.ParseGridMap(e.gridMap.saved); err == nil {
			movePath(&doc.MeshLibrary)
			if data, err := world3d.EncodeGridMap(doc); err == nil {
				e.gridMap.saved = data
			}
		}
	}
}

// isGridMapFile reports whether p names a 3D grid map file (*.gridmap.yaml or *.gridmap.yml).
func isGridMapFile(p string) bool {
	return sceneformat.IsGridMapPath(strings.TrimSpace(p))
}

// gridMapFileName returns the base name of the open GridMap or its mesh library for compact UI labels.
func (e *Editor) gridMapLabel() string {
	if p := e.GridMapPath(); p != "" {
		return path.Base(p)
	}
	if e.gridMap.meshLibrary != "" {
		return path.Base(e.gridMap.meshLibrary)
	}
	return "GridMap"
}
