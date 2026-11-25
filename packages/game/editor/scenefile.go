package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// ErrNoSceneFile is returned by Save and RevertScene while the open scene has no file to write to or read back: it was never opened from or saved to one, and no OnSave persists it.
var ErrNoSceneFile = errors.New("editor: the scene has no file; save it under a path first")

// ErrFileExists is returned by NewScene and SaveSceneAs for a path where the project already holds a file, which they never overwrite.
var ErrFileExists = errors.New("editor: a file already exists at that path")

// markSceneSaved snapshots the current edit scene document as clean for SceneDirty comparisons.
func (e *Editor) markSceneSaved() {
	if e == nil || e.Doc == nil {
		return
	}
	if data, err := e.SaveYAML(); err == nil {
		e.sceneSaved = data
		e.syncActiveTab()
	}
}

// SceneDirty reports whether the open scene differs from what it was opened from or last saved to. It compares encodings, as ProjectDirty does, so undoing back to the saved state makes the scene clean again. While playing it compares the scene as it was before Play.
func (e *Editor) SceneDirty() bool {
	if e == nil || e.Doc == nil {
		return false
	}
	data, err := e.SaveYAML()
	return err != nil || !bytes.Equal(data, e.sceneSaved)
}

// ClipDirty reports whether the open animation clip differs from when it was opened or last saved.
func (e *Editor) ClipDirty() bool {
	if e == nil || e.clip == nil {
		return false
	}
	if len(e.clipSaved) == 0 {
		return true
	}
	data, err := e.SaveClipYAML()
	return err != nil || !bytes.Equal(data, e.clipSaved)
}

// DataTableDirty reports whether the open data table differs from when it was opened or last saved.
func (e *Editor) DataTableDirty() bool {
	if e == nil || e.table == nil || e.table.doc == nil {
		return false
	}
	if len(e.tableSaved) == 0 {
		return true
	}
	data, err := e.SaveDataTableYAML()
	return err != nil || !bytes.Equal(data, e.tableSaved)
}

// HasUnsavedChanges reports whether any open scene tab, the shared project, GridMap, terrain, room map, tileset, localization workspace, animation clip, cutscene, data table, skeletal rig, or material has edits that are not saved, which switching projects or quitting would lose.
func (e *Editor) HasUnsavedChanges() bool {
	if e == nil {
		return false
	}
	return e.AnySceneDirty() || e.ProjectDirty() || e.GridMapDirty() || e.TerrainDirty() || e.RoomsDirty() || e.TilesetDirty() || e.LocalizationDirty() || e.ClipDirty() || e.CutsceneDirty() || e.DataTableDirty() || e.RigDirty() || e.MaterialDirty()
}

// SaveAll persists all currently dirty editors (project, every dirty open scene tab, GridMap, terrain, room map, localizations, clip, cutscene, data table, skeletal rig, and material) that have a save destination configured.
func (e *Editor) SaveAll() error {
	if e == nil {
		return nil
	}
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkSaveAllConflicts(); err != nil {
		return err
	}
	if e.ProjectDirty() {
		if err := e.SaveProject(); err != nil {
			return err
		}
	}
	if e.GridMapDirty() {
		if err := e.SaveGridMap(); err != nil {
			return err
		}
	}
	if e.TerrainDirty() {
		if err := e.SaveTerrain(); err != nil {
			return err
		}
	}
	if e.RoomsDirty() {
		if err := e.SaveRooms(); err != nil {
			return err
		}
	}
	if e.TilesetDirty() {
		if err := e.SaveTileset(); err != nil {
			return err
		}
	}
	e.syncActiveTab()
	for i := range e.tabs {
		if e.tabDirtyAt(i) {
			if err := e.SaveTab(i); err != nil {
				return err
			}
		}
	}
	if e.LocalizationDirty() {
		if err := e.SaveLocalizations(); err != nil {
			return err
		}
	}
	if e.ClipDirty() {
		switch {
		case e.OnSaveClip != nil:
			data, err := e.SaveClipYAML()
			if err != nil {
				return err
			}
			if err := e.OnSaveClip(data); err != nil {
				return err
			}
			e.clipSaved = append([]byte(nil), data...)
		case e.ClipPath() != "":
			if err := e.SaveClipFile(e.ClipPath()); err != nil {
				return err
			}
		default:
			return errors.New("editor: open animation clip has unsaved changes with no file path")
		}
	}
	if e.CutsceneDirty() {
		if err := e.saveCutsceneFromPanel(); err != nil {
			return err
		}
	}
	if e.DataTableDirty() {
		if err := e.saveDataTableFromPanel(); err != nil {
			return err
		}
	}
	if e.RigDirty() {
		if err := e.saveRigFromPanel(); err != nil {
			return err
		}
	}
	if e.MaterialDirty() {
		if err := e.SaveMaterial(); err != nil {
			return err
		}
	}
	e.RefreshChangesCount()
	return nil
}

// SceneLabel names the open scene as the toolbar and window title show it: its path from the project root (ScenePath) when it has one, else its file's path (FilePath), else "Untitled".
func (e *Editor) SceneLabel() string {
	switch {
	case e.ScenePath != "":
		return e.ScenePath
	case e.FilePath != "":
		return e.FilePath
	default:
		return e.T("scene.untitled")
	}
}

// WindowTitle is the title EditorView gives the window: SceneLabel, followed by "*" while HasUnsavedChanges, then the editor's name (AppName, or the localized default app name while it is empty), as in "scenes/level.scene.yaml* - NSGo Scene Editor".
func (e *Editor) WindowTitle() string {
	return e.windowTitle(e.HasUnsavedChanges())
}

// windowTitle is WindowTitle for a scene or project whose unsaved state is unsaved, which EditorView has already worked out.
func (e *Editor) windowTitle(unsaved bool) string {
	label := e.SceneLabel()
	if unsaved {
		label += "*"
	}
	name := e.AppName
	if name == "" {
		name = e.T("app.defaultName")
	}
	return label + " - " + name
}

// Save saves what has unsaved edits, as the toolbar's Save button and Mod+S do, and records the open scene's view in the project's ViewStateFile (SaveViewState). An edited project file is saved first (SaveProject), whether or not the scene can be; an unedited one is left as written, comments included. An edited scene then goes to OnSave when it is set, else to FilePath (SaveFile), and an unedited one is left as written too, unless FilePath names no file yet. A scene with neither OnSave nor FilePath returns ErrNoSceneFile, unless it is unedited. It refuses while playing (ErrPlaying).
func (e *Editor) Save() error {
	if e.playing {
		return ErrPlaying
	}
	// A drag in progress is put back first, so the file holds the scene as it was before the drag and not a half-dragged object; a tile, GridMap, or terrain stroke under way is recorded first so the file and history agree.
	e.CancelDrag2D()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.cancelBackgroundPreview()
	if err := e.flushMaterialColorPreview(); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	// The view is kept beside the project rather than in the scene, so saving records it whether or not the scene has edits.
	e.saveViewStateQuietly()
	e.endGridMapStrokeForChange()
	e.endTerrainStrokeForChange()
	if err := e.checkSaveConflicts(); err != nil {
		return err
	}
	projectSaved := false
	if e.ProjectDirty() {
		if err := e.SaveProject(); err != nil {
			return err
		}
		projectSaved = true
	}
	gridMapSaved := false
	if e.GridMapDirty() && (e.OnSaveGridMap != nil || e.gridMap.filePath != "") {
		if err := e.SaveGridMap(); err != nil {
			return err
		}
		gridMapSaved = true
	}
	terrainSaved := false
	if e.TerrainDirty() && e.TerrainPath() != "" {
		if err := e.SaveTerrain(); err != nil {
			return err
		}
		terrainSaved = true
	}
	roomsSaved := false
	if e.RoomsDirty() && (e.OnSaveRooms != nil || (e.rooms != nil && e.rooms.path != "")) {
		if err := e.SaveRooms(); err != nil {
			return err
		}
		roomsSaved = true
	}
	materialSaved := false
	if e.MaterialDirty() && (e.OnSaveMaterial != nil || e.MaterialPath() != "") {
		if err := e.SaveMaterial(); err != nil {
			return err
		}
		materialSaved = true
	}
	if !e.SceneDirty() && (e.OnSave != nil || e.FilePath == "" || fileExists(e.FilePath)) {
		// Rewriting an unedited scene would only drop its comments and hand-kept layout.
		if e.RoomsDirty() {
			return ErrNoRoomsFile
		}
		if projectSaved || gridMapSaved || terrainSaved || roomsSaved || materialSaved {
			e.RefreshChangesCount()
		} else {
			e.Status = "No unsaved changes"
			e.notify()
		}
		return nil
	}
	switch {
	case e.OnSave != nil:
		data, err := e.SaveYAML()
		if err != nil {
			return err
		}
		if err := e.OnSave(data); err != nil {
			return err
		}
		e.sceneSaved = data
		e.syncActiveTab()
		e.invalidateProjectIndex()
		e.rebuildDependentTabs(e.activeTabID())
		e.Status = "Saved scene"
		e.notify()
		e.CheckProblems()
		if e.RoomsDirty() {
			return ErrNoRoomsFile
		}
		return nil
	case e.FilePath != "":
		if err := e.SaveFile(e.FilePath); err != nil {
			return err
		}
		e.RefreshChangesCount()
		if e.RoomsDirty() {
			return ErrNoRoomsFile
		}
		return nil
	default:
		return ErrNoSceneFile
	}
}

// fileExists reports whether p names a file or directory that exists.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// OpenScene opens the scene file at rel, a slash-separated path from the root of the open project (the directory of ProjectPath), in a tab with ScenePath set to rel, or switches to its tab when it is already open. Switching or opening tabs never clears any tab's undo history; the newly opened tab starts with OnSave nil while previously open tabs keep theirs. It returns ErrNoProject without a project.
func (e *Editor) OpenScene(rel string) error {
	abs, err := e.projectFile(rel)
	if err != nil {
		return err
	}
	if idx := e.findTabByPath(rel, abs); idx >= 0 {
		if e.playing {
			if err := e.Stop(); err != nil {
				return err
			}
		}
		return e.SelectTab(idx)
	}
	if err := e.OpenFile(abs); err != nil {
		return err
	}
	e.OnSave = nil
	e.syncActiveTab()
	return nil
}

// NewScene writes a new, minimal valid scene to rel, a slash-separated path from the root of the open project, and opens it as OpenScene does. A path without a .yaml or .yml extension gets ".scene.yaml" (one ending in ".scene" gets ".yaml"), the name `game check` and `game index` find scenes by, and NewScene returns the path it wrote. The scene is named after the file and is as large as the project's display, or its window without one, with one world layer, "main". It refuses the names of the project's own files, game.yaml and nsgo.yaml, makes the directories the path needs, never overwrites a file (ErrFileExists), and returns ErrNoProject without a project. Like OpenScene, it loses the open scene's unsaved edits.
func (e *Editor) NewScene(rel string) (string, error) {
	rel = withSceneExt(rel)
	abs, err := e.sceneTarget(rel)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err == nil {
		return "", fmt.Errorf("%w: %s", ErrFileExists, rel)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	data, err := sceneformat.Encode(newSceneDoc(rel, e.Project))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(abs, data); err != nil {
		return "", err
	}
	e.filesRev++
	e.invalidateProjectIndex()
	if err := e.OpenScene(rel); err != nil {
		return "", err
	}
	e.Status = "New scene " + rel
	e.notify()
	return rel, nil
}

// SaveSceneAs writes the open scene to rel, a slash-separated path from the root of the open project, and makes that file the scene's: FilePath and ScenePath name it, Save writes there and OnSave, which persisted the scene before, is cleared. A path without a .yaml or .yml extension gets ".scene.yaml" (one ending in ".scene" gets ".yaml"), and SaveSceneAs returns the path it wrote. It refuses the names of the project's own files, game.yaml and nsgo.yaml, makes the directories the path needs and never overwrites another file (ErrFileExists); saving to the scene's own path saves it. It refuses while playing (ErrPlaying) and returns ErrNoProject without a project.
func (e *Editor) SaveSceneAs(rel string) (string, error) {
	if e.playing {
		return "", ErrPlaying
	}
	// A drag in progress is put back first, so the file holds the scene as it was before the drag; a tile or GridMap stroke under way is recorded first so the file and history agree.
	e.CancelDrag2D()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.cancelBackgroundPreview()
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	rel = withSceneExt(rel)
	abs, err := e.sceneTarget(rel)
	if err != nil {
		return "", err
	}
	if rel != e.ScenePath {
		if _, err := os.Lstat(abs); err == nil {
			return "", fmt.Errorf("%w: %s", ErrFileExists, rel)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := e.SaveFile(abs); err != nil {
		return "", err
	}
	e.OnSave = nil
	e.syncActiveTab()
	return rel, nil
}

// RevertScene puts back the open scene as its file (FilePath) holds it now, as one undoable step, so Undo brings the reverted edits back. The scene is then clean, until Undo. It refuses while playing (ErrPlaying) and returns ErrNoSceneFile for a scene without a file, and the error of a file that no longer reads or parses, changing nothing then.
func (e *Editor) RevertScene() error {
	if e.playing {
		return ErrPlaying
	}
	if e.FilePath == "" {
		return ErrNoSceneFile
	}
	data, err := os.ReadFile(e.FilePath)
	if err != nil {
		return err
	}
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: e.FilePath, Components: e.components})
	if err != nil {
		return err
	}
	saved, err := sceneformat.Encode(doc)
	if err != nil {
		return err
	}
	// A drag or tile/GridMap stroke in progress is put back first, so Undo of the revert brings back the scene as it was before it.
	e.CancelDrag2D()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.cancelBackgroundPreview()
	e.CancelTileStroke()
	e.connPick = nil
	e.CancelGridMapStroke()
	e.sceneSaved = saved
	e.recordSceneDiskSnapshot(e.FilePath, data)
	e.History.Execute(&replaceSceneCmd{ed: e, label: "Revert scene", before: e.Doc.Clone(), after: doc})
	e.syncActiveTab()
	e.Status = "Reverted " + e.SceneLabel()
	e.notify()
	return nil
}

// projectFile returns the OS path of rel, a slash-separated path from the root of the open project, refusing a path outside it.
func (e *Editor) projectFile(rel string) (string, error) {
	if e.Project == nil || e.ProjectPath == "" {
		return "", ErrNoProject
	}
	if !validScenePath(rel) || strings.ContainsRune(rel, '\\') {
		return "", fmt.Errorf("editor: %q is not a slash-separated path inside the project", rel)
	}
	return filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(rel)), nil
}

// sceneTarget returns the OS path of rel, a slash-separated path from the root of the open project that NewScene or SaveSceneAs is to write a scene to, as projectFile does, refusing the names of the project's own files (game.yaml, nsgo.yaml), which the project's tools would read as something else.
func (e *Editor) sceneTarget(rel string) (string, error) {
	abs, err := e.projectFile(rel)
	if err != nil {
		return "", err
	}
	if base := path.Base(rel); base == project.FileName || base == project.NSGoFileName {
		return "", fmt.Errorf("editor: %q is a project file's name, not a scene's", rel)
	}
	return abs, nil
}

// projectRel returns the slash-separated path from the root of the open project of the OS path p, and false when no project is open or p lies outside it.
func (e *Editor) projectRel(p string) (string, bool) {
	if e.Project == nil || e.ProjectPath == "" || p == "" {
		return "", false
	}
	root, err := filepath.Abs(filepath.Dir(e.ProjectPath))
	if err != nil {
		return "", false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if !validScenePath(rel) {
		return "", false
	}
	return rel, true
}

// syncScenePath sets ScenePath to FilePath's path in the open project, or clears it when the file lies outside the project or there is none, and finds the project's atlases nearest it (syncProjectAssets).
func (e *Editor) syncScenePath() {
	e.ScenePath, _ = e.projectRel(e.FilePath)
	if e.syncProjectAssets() {
		e.Rebuild()
	}
}

// withSceneExt adds ".scene.yaml" to a path without a YAML extension, or ".yaml" to one ending in ".scene", in any case. An empty path, or one naming a directory, stays as it is for projectFile to refuse.
func withSceneExt(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.HasSuffix(rel, "/") {
		return rel
	}
	switch strings.ToLower(path.Ext(rel)) {
	case ".yaml", ".yml":
		return rel
	case ".scene":
		return rel + ".yaml"
	}
	return rel + ".scene.yaml"
}

// newSceneDoc returns the minimal scene NewScene writes to rel: named after the file, as large as p's display or window, with one world layer.
func newSceneDoc(rel string, p *project.File) *sceneformat.SceneDoc {
	doc := DefaultSceneDoc()
	name := path.Base(rel)
	for _, ext := range []string{".yaml", ".yml", ".scene"} {
		if strings.HasSuffix(strings.ToLower(name), ext) {
			name = name[:len(name)-len(ext)]
		}
	}
	if name != "" {
		doc.Name = name
	}
	if p != nil {
		switch {
		case p.Display != nil && p.Display.Width > 0 && p.Display.Height > 0:
			doc.VirtualWidth, doc.VirtualHeight = p.Display.Width, p.Display.Height
		case p.Window.Width > 0 && p.Window.Height > 0:
			doc.VirtualWidth, doc.VirtualHeight = p.Window.Width, p.Window.Height
		}
	}
	return doc
}

// replaceSceneCmd replaces the whole scene with after, and back with before, as RevertScene does.
type replaceSceneCmd struct {
	ed            *Editor
	label         string
	before, after *sceneformat.SceneDoc
}

func (c *replaceSceneCmd) Label() string { return c.label }

func (c *replaceSceneCmd) Do() { c.ed.setDoc(c.after.Clone()) }

func (c *replaceSceneCmd) Undo() { c.ed.setDoc(c.before.Clone()) }

// setDoc makes doc the scene, keeping the active layer when doc has it and choosing its first layer otherwise.
func (e *Editor) setDoc(doc *sceneformat.SceneDoc) {
	e.Doc = doc
	if e.layerIndex(e.ActiveLayer) < 0 {
		e.ActiveLayer = ""
		if len(doc.Layers) > 0 {
			e.ActiveLayer = doc.Layers[0].Name
		}
	}
}
