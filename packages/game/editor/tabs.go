package editor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// SceneTab describes one open scene tab in the editor's tab bar.
type SceneTab struct {
	// ID is a stable identifier unique within this Editor session.
	ID uint64
	// Index is the tab's 0-based position in Tabs().
	Index int
	// Label is the display name shown in the tab bar (the file's base name, disambiguated when two open tabs share a base name, or "Untitled").
	Label string
	// Path is the scene's slash-separated path from the project root (ScenePath), or FilePath when outside a project, or empty for an untitled scene.
	Path string
	// FilePath is the OS path of the scene file, or empty.
	FilePath string
	// ScenePath is the slash-separated project-relative path, or empty.
	ScenePath string
	// Name is SceneDoc.Name.
	Name string
	// Dirty reports whether this tab's scene differs from what it was opened from or last saved to.
	Dirty bool
	// Active reports whether this tab is the currently active tab.
	Active bool
}

// sceneTab holds the per-scene state preserved when switching between open tabs.
type sceneTab struct {
	id                  uint64
	doc                 *sceneformat.SceneDoc
	built               *sceneformat.BuiltScene
	buildErr            error
	history             *History
	filePath            string
	scenePath           string
	onSave              func([]byte) error
	sceneSaved          []byte
	sceneDisk           diskSnapshot
	selectedID          string
	selectedIDs         []string
	activeLayer         string
	activeTileID        int
	activeTileLayer     string
	activeTileFlip      uint32
	tileSel             TileRect
	hasTileSel          bool
	view2D              View2D
	view2DFree          bool
	camera3D            OrbitCamera3D
	lookThroughCamera3D bool
	hidden              map[string]bool
	locked              map[string]bool
	layerLocked         map[string]bool
	inspectLayer        bool
	// placeholder is true only for a pristine untitled tab created automatically by New(nil, ...) or by closing the last open tab.
	placeholder bool
}

func (e *Editor) nextTabID() uint64 {
	e.tabSeq++
	return e.tabSeq
}

func (e *Editor) newTabHistory() *History {
	h := NewHistory()
	h.onChange = func() {
		e.afterMutate()
	}
	return h
}

func (e *Editor) activeTabID() uint64 {
	if e == nil || e.activeTab < 0 || e.activeTab >= len(e.tabs) {
		return 0
	}
	return e.tabs[e.activeTab].id
}

// ensureTabs initializes the tab slice lazily if needed.
func (e *Editor) ensureTabs() {
	if e == nil {
		return
	}
	if len(e.tabs) == 0 {
		t := &sceneTab{id: e.nextTabID()}
		e.tabs = []*sceneTab{t}
		e.activeTab = 0
		e.syncActiveTab()
	}
	if e.activeTab < 0 || e.activeTab >= len(e.tabs) {
		e.activeTab = 0
	}
}

// syncActiveTab copies the live active-scene fields on Editor back into e.tabs[e.activeTab].
func (e *Editor) syncActiveTab() {
	if e == nil {
		return
	}
	if len(e.tabs) == 0 {
		t := &sceneTab{id: e.nextTabID()}
		e.tabs = []*sceneTab{t}
		e.activeTab = 0
	}
	if e.activeTab < 0 || e.activeTab >= len(e.tabs) {
		e.activeTab = 0
	}
	t := e.tabs[e.activeTab]
	t.doc = e.Doc
	t.built = e.Built
	t.buildErr = e.BuildErr
	t.history = e.History
	t.filePath = e.FilePath
	t.scenePath = e.ScenePath
	t.onSave = e.OnSave
	t.sceneSaved = e.sceneSaved
	t.sceneDisk = e.disk.sceneDisk
	t.selectedID = e.SelectedID
	t.selectedIDs = e.SelectedIDs()
	t.activeLayer = e.ActiveLayer
	t.activeTileID = e.ActiveTileID
	t.activeTileLayer = e.ActiveTileLayer
	t.activeTileFlip = e.ActiveTileFlip
	t.tileSel = e.tileSel
	t.hasTileSel = e.hasTileSel
	t.view2D = e.view2D
	t.view2DFree = e.view2DFree
	t.camera3D = e.Camera3D
	t.lookThroughCamera3D = e.lookThroughCamera3D
	t.hidden = e.hidden
	t.locked = e.locked
	t.layerLocked = e.layerLocked
	t.inspectLayer = e.inspectLayer
}

// loadActiveTab copies e.tabs[e.activeTab] into the live active-scene fields on Editor.
func (e *Editor) loadActiveTab() {
	if e == nil || len(e.tabs) == 0 {
		return
	}
	if e.activeTab < 0 || e.activeTab >= len(e.tabs) {
		e.activeTab = 0
	}
	t := e.tabs[e.activeTab]
	e.Doc = t.doc
	e.Built = t.built
	e.BuildErr = t.buildErr
	e.History = t.history
	e.FilePath = t.filePath
	e.ScenePath = t.scenePath
	e.OnSave = t.onSave
	e.sceneSaved = t.sceneSaved
	e.disk.sceneDisk = t.sceneDisk
	e.onActiveSceneTabChanged()
	e.setSelectionQuiet(t.selectedIDs, t.selectedID)
	e.pasteStreak = 0
	e.ActiveLayer = t.activeLayer
	e.ActiveTileID = t.activeTileID
	e.ActiveTileLayer = t.activeTileLayer
	e.ActiveTileFlip = t.activeTileFlip
	e.tileSel = t.tileSel
	e.hasTileSel = t.hasTileSel
	e.view2D = t.view2D
	e.view2DFree = t.view2DFree
	e.Camera3D = t.camera3D
	e.lookThroughCamera3D = t.lookThroughCamera3D
	e.hidden = t.hidden
	e.locked = t.locked
	e.layerLocked = t.layerLocked
	e.inspectLayer = t.inspectLayer
}

// resetTabsToActive discards all other tabs and makes the current Editor fields the sole open tab.
func (e *Editor) resetTabsToActive(placeholder bool) {
	if e == nil {
		return
	}
	t := &sceneTab{id: e.nextTabID(), placeholder: placeholder}
	e.tabs = []*sceneTab{t}
	e.activeTab = 0
	e.syncActiveTab()
}

// canReplacePlaceholderTab reports whether the editor holds only a single pristine untitled tab that opening a scene should replace instead of leaving beside it.
func (e *Editor) canReplacePlaceholderTab() bool {
	if e == nil || len(e.tabs) != 1 || e.activeTab != 0 {
		return false
	}
	t := e.tabs[0]
	return t.placeholder &&
		e.FilePath == "" &&
		e.ScenePath == "" &&
		e.OnSave == nil &&
		!e.History.CanUndo() &&
		!e.History.CanRedo() &&
		!e.SceneDirty()
}

// cancelTransientInteraction cancels any viewport drag or preview and commits any in-progress tile, GridMap or terrain stroke before changing or closing a tab.
func (e *Editor) cancelTransientInteraction() {
	e.CancelDrag2D()
	e.CancelMarquee2D()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelAssetDrag()
	e.ClearPluginHover2D()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.CancelRigDrag()
	e.CancelHUDTouchDrag()
	e.cancelBackgroundPreview()
	e.CancelGizmoDrag3D()
	e.CancelHandleDrag3D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.ClearPluginHover3D()
	e.EndViewport3DDrag()
	e.endFly3D()
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	e.endTerrainStrokeForChange()
	e.connPick = nil
}

// canonicalTabFilePath resolves filePath to a cleaned absolute path and follows symlinks when the path exists on disk so symlinked paths (including macOS /var vs /private/var) match the existing tab.
func canonicalTabFilePath(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(abs)
}

// findTabByPath returns the index of the open tab matching scenePath or filePath, or -1 if none matches.
func (e *Editor) findTabByPath(scenePath, filePath string) int {
	if e == nil {
		return -1
	}
	e.syncActiveTab()
	absTarget := canonicalTabFilePath(filePath)
	for i, t := range e.tabs {
		if scenePath != "" && t.scenePath == scenePath {
			return i
		}
		if absTarget != "" && t.filePath != "" {
			if canonicalTabFilePath(t.filePath) == absTarget {
				return i
			}
		}
	}
	return -1
}

func (e *Editor) findTabByID(id uint64) int {
	if e == nil || id == 0 {
		return -1
	}
	for i, t := range e.tabs {
		if t.id == id {
			return i
		}
	}
	return -1
}

func (e *Editor) tabDirtyAt(index int) bool {
	if e == nil || index < 0 || index >= len(e.tabs) {
		return false
	}
	if index == e.activeTab {
		return e.SceneDirty()
	}
	t := e.tabs[index]
	if t.doc == nil {
		return false
	}
	data, err := sceneformat.Encode(t.doc)
	return err != nil || !bytes.Equal(data, t.sceneSaved)
}

func tabFullPath(t *sceneTab) string {
	switch {
	case t.scenePath != "":
		return t.scenePath
	case t.filePath != "":
		return t.filePath
	default:
		return ""
	}
}

func tabBaseLabel(t *sceneTab) string {
	switch {
	case t.scenePath != "":
		return path.Base(t.scenePath)
	case t.filePath != "":
		return filepath.Base(t.filePath)
	default:
		return "Untitled"
	}
}

func (e *Editor) tabPromptLabel(index int) string {
	if e == nil || index < 0 || index >= len(e.tabs) {
		return "Untitled"
	}
	e.syncActiveTab()
	if p := tabFullPath(e.tabs[index]); p != "" {
		return p
	}
	return "Untitled"
}

// Tabs returns a snapshot of all open scene tabs in display order.
func (e *Editor) Tabs() []SceneTab {
	if e == nil {
		return nil
	}
	e.syncActiveTab()
	counts := make(map[string]int, len(e.tabs))
	for _, t := range e.tabs {
		counts[tabBaseLabel(t)]++
	}
	out := make([]SceneTab, len(e.tabs))
	for i, t := range e.tabs {
		base := tabBaseLabel(t)
		full := tabFullPath(t)
		label := base
		if counts[base] > 1 && full != "" {
			label = full
		}
		var name string
		if t.doc != nil {
			name = t.doc.Name
		}
		out[i] = SceneTab{
			ID:        t.id,
			Index:     i,
			Label:     label,
			Path:      full,
			FilePath:  t.filePath,
			ScenePath: t.scenePath,
			Name:      name,
			Dirty:     e.tabDirtyAt(i),
			Active:    i == e.activeTab,
		}
	}
	return out
}

// ActiveTab returns the 0-based index of the active scene tab.
func (e *Editor) ActiveTab() int {
	if e == nil {
		return 0
	}
	e.ensureTabs()
	return e.activeTab
}

// TabCount returns the number of open scene tabs.
func (e *Editor) TabCount() int {
	if e == nil {
		return 0
	}
	e.ensureTabs()
	return len(e.tabs)
}

// TabDirty reports whether the scene in tab index has unsaved edits.
func (e *Editor) TabDirty(index int) bool {
	if e == nil {
		return false
	}
	e.syncActiveTab()
	return e.tabDirtyAt(index)
}

// AnySceneDirty reports whether any open scene tab has unsaved edits.
func (e *Editor) AnySceneDirty() bool {
	if e == nil {
		return false
	}
	e.syncActiveTab()
	for i := range e.tabs {
		if e.tabDirtyAt(i) {
			return true
		}
	}
	return false
}

// SelectTab activates the open scene tab at index without clearing any tab's undo history.
// It returns ErrPlaying while Play mode is active.
func (e *Editor) SelectTab(index int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	e.ensureTabs()
	if index < 0 || index >= len(e.tabs) {
		return fmt.Errorf("editor: tab index %d out of range", index)
	}
	if index == e.activeTab {
		return nil
	}
	e.saveViewStateQuietly()
	e.cancelTransientInteraction()
	e.syncActiveTab()
	e.activeTab = index
	e.loadActiveTab()
	e.inspectAssetFile = false
	e.baseLayers = baseLayerCache{}
	e.s3d.stale = true
	if e.syncProjectAssets() || e.Built == nil {
		e.Rebuild()
	} else {
		e.refreshClipPreview()
		e.refreshStatesPreview()
		e.syncRemoteDocObjectIDs()
		e.syncSceneEmitterLoop()
	}
	e.resetNavMeshForScene()
	e.tryAutoloadNavMesh3D()
	e.refreshOpenSceneSearch()
	e.Status = "Switched to " + e.SceneLabel()
	e.notify()
	return nil
}

// SelectTabByID activates the open scene tab with ID id.
func (e *Editor) SelectTabByID(id uint64) error {
	idx := e.findTabByID(id)
	if idx < 0 {
		return fmt.Errorf("editor: tab %d not found", id)
	}
	return e.SelectTab(idx)
}

// SaveTab saves the scene in tab index to its OnSave callback or FilePath, leaving other tabs and the project file unchanged, and rebuilds any other open tabs that instance or inherit scenes.
func (e *Editor) SaveTab(index int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	e.syncActiveTab()
	if index < 0 || index >= len(e.tabs) {
		return fmt.Errorf("editor: tab index %d out of range", index)
	}
	if index == e.activeTab {
		e.CancelDrag2D()
		e.CancelShapeDrag2D()
		e.CancelPluginDrag2D()
		e.CancelCameraDrag()
		e.CancelCameraBoundsDrag()
		e.CancelRoomDrag()
		e.CancelRigDrag()
		e.CancelHUDTouchDrag()
		e.CancelGizmoDrag3D()
		e.CancelHandleDrag3D()
		e.CancelComponentGizmoDrag3D()
		e.CancelPluginDrag3D()
		e.ClearPluginHover3D()
		e.cancelBackgroundPreview()
		e.endTileStrokeForChange()
		e.saveViewStateQuietly()
		e.endGridMapStrokeForChange()
		e.endTerrainStrokeForChange()
		if !e.SceneDirty() && (e.OnSave != nil || e.FilePath == "" || fileExists(e.FilePath)) {
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
			e.sceneSaved = append([]byte(nil), data...)
			e.tabs[e.activeTab].placeholder = false
			e.syncActiveTab()
			e.invalidateProjectIndex()
			e.rebuildDependentTabs(e.activeTabID())
			e.Status = "Saved scene"
			e.notify()
			e.CheckProblems()
			return nil
		case e.FilePath != "":
			return e.SaveFile(e.FilePath)
		default:
			return ErrNoSceneFile
		}
	}

	t := e.tabs[index]
	if !e.tabDirtyAt(index) && (t.onSave != nil || t.filePath == "" || fileExists(t.filePath)) {
		return nil
	}
	data, err := sceneformat.Encode(t.doc)
	if err != nil {
		return err
	}
	switch {
	case t.onSave != nil:
		if err := t.onSave(data); err != nil {
			return err
		}
		t.sceneSaved = append([]byte(nil), data...)
		t.placeholder = false
		e.invalidateProjectIndex()
		e.rebuildDependentTabs(t.id)
		e.Status = "Saved scene"
		e.notify()
		e.CheckProblems()
		return nil
	case t.filePath != "":
		if err := e.checkTabDiskConflict(t); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(t.filePath), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(t.filePath, data); err != nil {
			return err
		}
		t.sceneSaved = append([]byte(nil), data...)
		t.sceneDisk = makeDiskSnapshot(t.filePath, data)
		t.placeholder = false
		if e.disk.saveConflict != nil && e.disk.saveConflictTabID == t.id {
			e.disk.saveConflict = nil
			e.disk.saveConflictTabID = 0
		}
		e.filesRev++
		e.snapshotProjectTree()
		e.invalidateProjectIndex()
		e.rebuildDependentTabs(t.id)
		e.Status = "Saved " + t.filePath
		e.notify()
		e.CheckProblems()
		return nil
	default:
		return ErrNoSceneFile
	}
}

// CloseTab closes the scene tab at index immediately without prompting.
// Closing the last remaining tab replaces it with a fresh clean untitled scene tab.
// It returns ErrPlaying while Play mode is active.
func (e *Editor) CloseTab(index int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	e.syncActiveTab()
	if index < 0 || index >= len(e.tabs) {
		return fmt.Errorf("editor: tab index %d out of range", index)
	}
	closedLabel := e.tabPromptLabel(index)

	if len(e.tabs) == 1 {
		e.saveViewStateQuietly()
		e.cancelTransientInteraction()
		doc := DefaultSceneDoc()
		saved, _ := sceneformat.Encode(doc)
		activeLayer := ""
		if len(doc.Layers) > 0 {
			activeLayer = doc.Layers[0].Name
		}
		t := &sceneTab{
			id:           e.nextTabID(),
			doc:          doc,
			history:      e.newTabHistory(),
			sceneSaved:   saved,
			activeLayer:  activeLayer,
			activeTileID: 1,
			camera3D:     DefaultOrbitCamera3D(),
			placeholder:  true,
		}
		e.tabs = []*sceneTab{t}
		e.activeTab = 0
		e.loadActiveTab()
		e.inspectAssetFile = false
		e.baseLayers = baseLayerCache{}
		e.s3d.stale = true
		e.keepTileInPalette()
		e.Rebuild()
		e.resetNavMeshForScene()
		e.refreshOpenSceneSearch()
		e.Status = "Closed " + closedLabel
		e.notify()
		return nil
	}

	if index != e.activeTab {
		closedID := e.tabs[index].id
		e.tabs = append(e.tabs[:index], e.tabs[index+1:]...)
		if index < e.activeTab {
			e.activeTab--
		}
		if e.disk.saveConflict != nil && e.disk.saveConflictTabID == closedID {
			e.disk.saveConflict = nil
			e.disk.saveConflictTabID = 0
		}
		e.refreshOpenSceneSearch()
		e.Status = "Closed " + closedLabel
		e.notify()
		return nil
	}

	e.saveViewStateQuietly()
	e.cancelTransientInteraction()
	e.tabs = append(e.tabs[:index], e.tabs[index+1:]...)
	if e.activeTab >= len(e.tabs) {
		e.activeTab = len(e.tabs) - 1
	}
	e.loadActiveTab()
	e.inspectAssetFile = false
	e.baseLayers = baseLayerCache{}
	e.s3d.stale = true
	if e.syncProjectAssets() || e.Built == nil {
		e.Rebuild()
	} else {
		e.refreshClipPreview()
		e.refreshStatesPreview()
		e.syncRemoteDocObjectIDs()
		e.syncSceneEmitterLoop()
	}
	e.resetNavMeshForScene()
	e.tryAutoloadNavMesh3D()
	e.refreshOpenSceneSearch()
	e.Status = "Closed " + closedLabel
	e.notify()
	return nil
}

// RequestCloseTab closes tab index immediately when clean; when the tab has unsaved edits, it prompts in EditorView to save, discard or cancel (leaving the shared project file untouched), or reports errUnsaved when no EditorView is mounted.
func (e *Editor) RequestCloseTab(index int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	e.syncActiveTab()
	if index < 0 || index >= len(e.tabs) {
		return fmt.Errorf("editor: tab index %d out of range", index)
	}
	if !e.tabDirtyAt(index) {
		return e.CloseTab(index)
	}
	if e.askCloseTab != nil {
		e.askCloseTab(e.tabs[index].id, e.tabPromptLabel(index))
		return nil
	}
	e.report(errUnsaved)
	return errUnsaved
}

// OpenInstancedScene opens the scene file instanced by objectID (or SelectedID when empty) in a tab, switching to its tab if it is already open.
func (e *Editor) OpenInstancedScene(objectID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	id := objectID
	if id == "" {
		id = e.SelectedID
	}
	if id == "" {
		return errors.New("editor: no object selected")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Scene == "" {
		return fmt.Errorf("editor: object %q does not instance a scene", id)
	}
	return e.OpenScene(obj.Scene)
}

// OpenBaseScene opens the base scene that the active scene inherits (SceneDoc.Inherits) in a tab, switching to its tab if it is already open.
func (e *Editor) OpenBaseScene() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.Doc == nil || e.Doc.Inherits == "" {
		return errors.New("editor: the active scene does not inherit a base scene")
	}
	return e.OpenScene(e.Doc.Inherits)
}

// resolverForScene returns the AssetResolver configured for scenePath without mutating e.Resolver.
func (e *Editor) resolverForScene(scenePath string) sceneformat.AssetResolver {
	if e.ProjectAssets {
		if r, ok := e.Resolver.(*project.Resolver); ok {
			return r.ForScene(scenePath)
		}
	}
	return e.Resolver
}

// rebuildDependentTabs rebuilds every open tab other than savedTabID that instances or inherits other scenes, so saving an instanced or base scene updates those tabs' BuiltScene and 3D build immediately without reopening them.
func (e *Editor) rebuildDependentTabs(savedTabID uint64) {
	if e == nil {
		return
	}
	e.syncActiveTab()
	e.scenesRev++
	e.baseLayers = baseLayerCache{}
	for i, t := range e.tabs {
		if t.id == savedTabID || t.doc == nil || !t.doc.RefersToScenes() {
			continue
		}
		built, err := sceneformat.Build(t.doc, e.resolverForScene(t.scenePath))
		if err != nil {
			t.buildErr = err
		} else {
			built.DrawAllTiles = true
			t.built = built
			t.buildErr = nil
		}
		if i == e.activeTab {
			e.Built = t.built
			e.BuildErr = t.buildErr
			e.s3d.stale = true
			e.refreshClipPreview()
			e.refreshStatesPreview()
		}
	}
}

// rebuildAllInactiveTabs rebuilds all non-active open tabs (for example after MoveFile, SetComponentRegistry, or pack changes).
func (e *Editor) rebuildAllInactiveTabs() {
	if e == nil {
		return
	}
	e.syncActiveTab()
	for i, t := range e.tabs {
		if i == e.activeTab || t.doc == nil {
			continue
		}
		built, err := sceneformat.Build(t.doc, e.resolverForScene(t.scenePath))
		if err != nil {
			t.buildErr = err
		} else {
			built.DrawAllTiles = true
			t.built = built
			t.buildErr = nil
		}
	}
}

// parseSceneFile reads and parses path without mutating editor state.
func (e *Editor) parseSceneFile(filePath string, layers *sceneformat.PhysicsLayerTable) (*sceneformat.SceneDoc, []byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
		File:          filePath,
		Components:    e.components,
		PhysicsLayers: layers,
	})
	if err != nil {
		return nil, nil, err
	}
	saved, err := sceneformat.Encode(doc)
	if err != nil {
		return nil, nil, err
	}
	return doc, saved, nil
}

// activateNewTab inserts a new scene tab (or replaces the single pristine placeholder tab), activates it, and initializes its viewport and build state.
func (e *Editor) activateNewTab(doc *sceneformat.SceneDoc, saved []byte, filePath string) {
	e.saveViewStateQuietly()
	e.cancelTransientInteraction()
	e.syncActiveTab()

	activeLayer := ""
	if len(doc.Layers) > 0 {
		activeLayer = doc.Layers[0].Name
	}
	t := &sceneTab{
		id:           e.nextTabID(),
		doc:          doc,
		history:      e.newTabHistory(),
		filePath:     filePath,
		sceneSaved:   saved,
		activeLayer:  activeLayer,
		activeTileID: 1,
		camera3D:     DefaultOrbitCamera3D(),
	}
	if e.canReplacePlaceholderTab() {
		e.tabs[0] = t
		e.activeTab = 0
	} else {
		e.tabs = append(e.tabs, t)
		e.activeTab = len(e.tabs) - 1
	}
	e.loadActiveTab()
	e.inspectAssetFile = false
	e.baseLayers = baseLayerCache{}
	e.s3d.stale = true
	e.keepTileInPalette()
	e.syncScenePath()
	if e.Built == nil && e.BuildErr == nil {
		e.Rebuild()
	}
	e.resetNavMeshForScene()
	e.navMesh3D.config.OutputPath = e.derivedNavMeshPath()
	e.tryAutoloadNavMesh3D()
	e.Status = "Opened " + filePath
	e.restoreViewState()
	e.syncActiveTab()
	e.refreshOpenSceneSearch()
}

// selectedObjectInstancesScene reports whether the active scene's selected object instances another scene file.
func (e *Editor) selectedObjectInstancesScene() bool {
	obj, _ := e.SelectedObject()
	return obj != nil && obj.Scene != ""
}
