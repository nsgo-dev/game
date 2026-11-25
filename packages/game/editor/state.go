package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
	"github.com/nsgo-dev/game/packages/game/tooling/library"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrPlaying is returned when an edit operation is attempted while Play mode is active.
var ErrPlaying = errors.New("editor: cannot edit scene while playing")

// Tool selects the active viewport interaction mode.
type Tool int

const (
	ToolSelect Tool = iota
	ToolMove
	ToolPlace
	ToolPaintTile
	ToolEraseTile
	ToolFillTile
	// ToolPlugin is a plugin's tool, chosen with Editor.SetPluginTool; SetTool(ToolPlugin) is ignored.
	ToolPlugin
)

// String returns a human-readable label for the tool.
func (t Tool) String() string {
	switch t {
	case ToolSelect:
		return "Select"
	case ToolMove:
		return "Move"
	case ToolPlace:
		return "Place"
	case ToolPaintTile:
		return "Paint Tile"
	case ToolEraseTile:
		return "Erase Tile"
	case ToolFillTile:
		return "Fill Tile"
	case ToolPlugin:
		return "Plugin"
	default:
		return "Unknown"
	}
}

// ViewMode selects whether the editor viewport renders and edits in 2D or 3D.
type ViewMode int

const (
	ViewMode2D ViewMode = iota
	ViewMode3D
)

// String returns a human-readable label for the view mode.
func (m ViewMode) String() string {
	switch m {
	case ViewMode2D:
		return "2D"
	case ViewMode3D:
		return "3D"
	default:
		return "Unknown"
	}
}

// GizmoMode3D selects the active 3D transform gizmo operation.
type GizmoMode3D int

const (
	Gizmo3DTranslate GizmoMode3D = iota
	Gizmo3DRotate
	Gizmo3DScale
)

// String returns a human-readable label for the 3D gizmo mode.
func (m GizmoMode3D) String() string {
	switch m {
	case Gizmo3DTranslate:
		return "Translate"
	case Gizmo3DRotate:
		return "Rotate"
	case Gizmo3DScale:
		return "Scale"
	default:
		return "Unknown"
	}
}

// GizmoAxis3D identifies an axis handle on a 3D transform gizmo.
type GizmoAxis3D int

const (
	Axis3DNone GizmoAxis3D = iota
	Axis3DX
	Axis3DY
	Axis3DZ
)

// String returns a human-readable label for the gizmo axis.
func (a GizmoAxis3D) String() string {
	switch a {
	case Axis3DX:
		return "X"
	case Axis3DY:
		return "Y"
	case Axis3DZ:
		return "Z"
	default:
		return "None"
	}
}

// Prefab is a reusable object template that PlaceObject and PlacePrefab can instantiate.
type Prefab struct {
	Name       string
	Sprite     *sceneformat.SpriteRef
	PosZ       float32
	OriginX    float32
	OriginY    float32
	RotX       float32
	RotY       float32
	RotZ       float32
	ScaleX     *float32
	ScaleY     *float32
	ScaleZ     *float32
	Depth      float32
	Mesh       string
	FlipX      bool
	FlipY      bool
	Alpha      *float32
	Z          int
	Components map[string]any
	Properties map[string]any
}

// Editor owns the editable SceneDoc, its resolved BuiltScene, undo/redo History,
// viewport tool/selection state, asset browser selection, and play-mode lifecycle.
type Editor struct {
	Doc           *sceneformat.SceneDoc
	Built         *sceneformat.BuiltScene
	BuildErr      error
	History       *History
	Resolver      sceneformat.AssetResolver
	Prefabs       map[string]Prefab
	SelectedID    string
	ActiveLayer   string
	Tool          Tool
	ViewMode      ViewMode
	GizmoMode     GizmoMode3D
	Camera3D      OrbitCamera3D
	ActiveTileID  int
	SelectedAsset *AssetEntry
	Status        string
	FilePath      string
	// Project is the game's project file (game.yaml) OpenProject opened, nil without one; EditProject changes it undoably and SaveProject writes it.
	Project *project.File
	// ProjectPath is the game.yaml path OpenProject opened Project from.
	ProjectPath string
	// ScenePath is the open scene's slash-separated path from the project root, as Resolver names scene files (for example "scenes/level.scene.yaml"), or empty when the scene has no such path. PlaceObject and PlaceScene resolve the edited scene under this path, so they refuse an instance that would lead back to the open scene and make the saved file instance itself. OpenYAML clears it, since the loaded scene may come from another file; set it again after OpenFile.
	ScenePath string
	// Library is the pack library the library panel lists and installs from into the open project (InstallPack); nil hides the panel.
	Library *library.Library

	// OnSave is invoked by the UI Save button with the encoded YAML bytes.
	OnSave func(data []byte) error
	// OnPlayUpdate runs on every fixed simulation step while Play mode is active.
	OnPlayUpdate func(scene *sceneformat.BuiltScene, dt time.Duration)
	// OnPlayDraw runs on every render frame while Play mode is active.
	OnPlayDraw func(scene *sceneformat.BuiltScene, alpha float64)
	// PlayRate is the fixed update rate in Hz during Play mode and clip playback (defaults to 60 when <= 0).
	PlayRate int
	// OnSaveClip is invoked by the animation panel's Save button with the encoded clip YAML; when nil the button writes to ClipPath.
	OnSaveClip func(data []byte) error
	// OnSaveDataTable is invoked by the Data Table panel's Save button with the encoded table YAML; when nil the button writes to DataTablePath.
	OnSaveDataTable func(data []byte) error

	playing      bool
	playSnapshot []byte
	playStop     func()
	playScene    *sceneformat.BuiltScene
	playSteps    int
	playFrames   int

	// projectSaved is Project's encoding when it was opened or last saved, which ProjectDirty compares against.
	projectSaved []byte

	// The animation editor: the open clip, its file and target, the playhead, the selected key, and the preview scene posed by the clip.
	clip        *resource.Animation
	clipMeta    clipMeta
	clipTime    time.Duration
	clipPlaying bool
	clipStop    func()
	clipScene   *sceneformat.BuiltScene
	clipPlayer  *animclip.Player
	clipErr     error
	selKey      clipKeyRef

	// The data table editor: the open table state and registered Go row types per file pattern.
	table      *dataTableState
	tableTypes *datatable.Registry

	components *sceneformat.ComponentRegistry
	// plugins are the registered plugins in registration order, and pluginTool the plugin tool chosen while Tool is ToolPlugin.
	plugins      []*Plugin
	pluginTool   pluginToolRef
	drag3D       drag3DState
	scope        *core.Scope
	revision     uint64
	nextID       int
	nextListener uint64
	listeners    map[uint64]func()
	// fetch is the library panel's fetch of a pack from a remote library under way, or nil.
	fetch *packFetch
	// filesRev counts the changes the editor made to which files the open project holds, by opening it, moving files, saving a scene or clip, which may make a new file, and installing or removing packs; the file browser lists the files again when it changes.
	filesRev uint64
	// audioMixer holds the Audio Bus Mixer panel state and its live audition sound.Mixer.
	audioMixer audioMixerState
}

// DefaultSceneDoc returns a valid starter SceneDoc suitable for a new editor session.
func DefaultSceneDoc() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Untitled Scene",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "main",
				Space: sceneformat.SpaceWorld,
				Order: 0,
			},
		},
	}
}

// New constructs an Editor over doc and resolver. If doc is nil, DefaultSceneDoc() is used.
func New(doc *sceneformat.SceneDoc, resolver sceneformat.AssetResolver) (*Editor, error) {
	if doc == nil {
		doc = DefaultSceneDoc()
	}
	if err := sceneformat.Validate(doc); err != nil {
		return nil, err
	}
	ed := &Editor{
		Doc:          doc,
		History:      NewHistory(),
		Resolver:     resolver,
		Prefabs:      make(map[string]Prefab),
		Tool:         ToolSelect,
		ViewMode:     ViewMode2D,
		GizmoMode:    Gizmo3DTranslate,
		Camera3D:     DefaultOrbitCamera3D(),
		ActiveTileID: 1,
		Status:       "Ready",
		selKey:       clipKeyRef{-1, -1},
		listeners:    make(map[uint64]func()),
	}
	if len(doc.Layers) > 0 {
		ed.ActiveLayer = doc.Layers[0].Name
	}
	ed.History.onChange = func() {
		ed.afterMutate()
	}
	ed.Rebuild()
	return ed, nil
}

// Revision returns a monotonically increasing counter bumped on every state change.
func (e *Editor) Revision() uint64 {
	return e.revision
}

// Subscribe registers fn to run whenever editor state changes and returns an unsubscribe function.
func (e *Editor) Subscribe(fn func()) func() {
	if e == nil || fn == nil {
		return func() {}
	}
	e.nextListener++
	id := e.nextListener
	if e.listeners == nil {
		e.listeners = make(map[uint64]func())
	}
	e.listeners[id] = fn
	return func() {
		delete(e.listeners, id)
	}
}

// AttachScope binds a core.Scope to the editor so Play() can launch game.Loop from UI controls.
func (e *Editor) AttachScope(cx *core.Scope) {
	e.scope = cx
}

func (e *Editor) notify() {
	e.revision++
	for _, fn := range e.listeners {
		fn()
	}
}

func (e *Editor) afterMutate() {
	// Ensure SelectedID still points to an existing object.
	if e.SelectedID != "" {
		if obj, _, _ := e.FindObject(e.SelectedID); obj == nil {
			e.SelectedID = ""
		}
	}
	// An undone or redone project edit may have turned off the plugin whose tool is active.
	e.dropStalePluginTool()
	e.syncAudioMixer()
	e.Rebuild()
	e.notify()
}

// Rebuild resolves e.Doc against e.Resolver into e.Built.
func (e *Editor) Rebuild() {
	defer e.refreshClipPreview()
	if e.Doc == nil {
		e.Built = nil
		e.BuildErr = nil
		return
	}
	built, err := sceneformat.Build(e.Doc, e.Resolver)
	if err != nil {
		e.BuildErr = err
		return
	}
	e.Built = built
	e.BuildErr = nil
}

// ActiveScene returns the live play scene when playing, the scene posed by the open animation clip at the playhead when one is open and previews, or the edit BuiltScene otherwise.
func (e *Editor) ActiveScene() *sceneformat.BuiltScene {
	if e.playing && e.playScene != nil {
		return e.playScene
	}
	if e.clipScene != nil {
		return e.clipScene
	}
	return e.Built
}

// SetTool switches the active viewport tool. A plugin's tool is chosen with SetPluginTool instead, so SetTool(ToolPlugin) is ignored.
func (e *Editor) SetTool(t Tool) {
	if e.Tool == t || t == ToolPlugin {
		return
	}
	e.Tool = t
	e.pluginTool = pluginToolRef{}
	e.Status = "Tool: " + t.String()
	e.notify()
}

// SetViewMode switches the editor viewport between 2D and 3D modes.
func (e *Editor) SetViewMode(mode ViewMode) {
	if e.ViewMode == mode {
		return
	}
	e.CancelGizmoDrag3D()
	e.ViewMode = mode
	e.Status = "View: " + mode.String()
	e.notify()
}

// SetGizmoMode3D switches the active 3D transform gizmo mode (Translate, Rotate, or Scale).
func (e *Editor) SetGizmoMode3D(mode GizmoMode3D) {
	if e.GizmoMode == mode {
		return
	}
	e.CancelGizmoDrag3D()
	e.GizmoMode = mode
	e.Status = "Gizmo: " + mode.String()
	e.notify()
}

// SetActiveLayer selects the layer targeted by object placement and tile painting.
func (e *Editor) SetActiveLayer(name string) bool {
	if e.layerIndex(name) < 0 {
		return false
	}
	e.ActiveLayer = name
	e.notify()
	return true
}

// SetActiveTileID sets the tile GID used by PaintTile and FloodFillTile tools.
func (e *Editor) SetActiveTileID(id int) {
	if id < 0 {
		return
	}
	e.ActiveTileID = id
	e.notify()
}

// SetComponentRegistry makes the editor check components against reg: the current document's components and connections must already conform, later opens refuse files whose components or connections do not, SetComponentField and PlaceObject refuse unregistered fields, and the inspector lists every registered field of a component. Nil turns the checks off. Changing the registry clears undo/redo history, since commands recorded under the old registry could bring back components the new one refuses.
func (e *Editor) SetComponentRegistry(reg *sceneformat.ComponentRegistry) error {
	if err := sceneformat.ValidateComponents(e.Doc, reg); err != nil {
		return err
	}
	// OpenFile binds connections against the registry (sceneformat.ParseWith), so the open scene must pass the same check, or a scene the editor lets the user save would not open again. A scene that instances or inherits others is checked once resolved, as ParseWith leaves it.
	if !e.Doc.RefersToScenes() {
		if err := sceneformat.ValidateConnections(e.Doc, reg); err != nil {
			return err
		}
	}
	if reg == e.components {
		return nil
	}
	e.components = reg
	// Clear triggers onChange -> afterMutate -> Rebuild + notify.
	e.History.Clear()
	return nil
}

// ComponentRegistry returns the registry set by SetComponentRegistry, or nil.
func (e *Editor) ComponentRegistry() *sceneformat.ComponentRegistry {
	return e.components
}

// OpenYAML parses data as a sceneformat.SceneDoc, replaces the active document,
// clears undo/redo history, and rebuilds the scene.
func (e *Editor) OpenYAML(data []byte) error {
	return e.openYAML(data, "")
}

func (e *Editor) openYAML(data []byte, file string) error {
	if e.playing {
		if err := e.Stop(); err != nil {
			return err
		}
	}
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: file, Components: e.components})
	if err != nil {
		return err
	}
	e.Doc = doc
	e.SelectedID = ""
	// The loaded scene did not come from the current path, so a later Save must
	// not overwrite the previously opened file with unrelated content.
	e.FilePath = ""
	e.ScenePath = ""
	if len(doc.Layers) > 0 {
		e.ActiveLayer = doc.Layers[0].Name
	} else {
		e.ActiveLayer = ""
	}
	e.Status = "Opened scene"
	// Clear triggers onChange -> afterMutate -> Rebuild + notify.
	e.History.Clear()
	return nil
}

// SaveYAML encodes the current edit SceneDoc to deterministic YAML.
// If Play mode is active, it returns a copy of the pre-play snapshot so runtime
// simulation state never leaks into saved YAML.
func (e *Editor) SaveYAML() ([]byte, error) {
	if e.playing && len(e.playSnapshot) > 0 {
		out := make([]byte, len(e.playSnapshot))
		copy(out, e.playSnapshot)
		return out, nil
	}
	return sceneformat.Encode(e.Doc)
}

// OpenFile reads a YAML scene file from path and loads it into the editor. A scene that does not parse returns sceneformat.Parse's *loaderr.Error, naming path.
func (e *Editor) OpenFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return loaderr.Locate(err, path, nil, "check that the scene exists at this path and can be read")
	}
	if err := e.openYAML(data, path); err != nil {
		return loaderr.WithFile(err, path)
	}
	e.FilePath = path
	e.Status = "Opened " + path
	e.notify()
	return nil
}

// SaveFile encodes the scene to YAML and writes it to path.
func (e *Editor) SaveFile(path string) error {
	data, err := e.SaveYAML()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	e.FilePath = path
	e.filesRev++
	e.Status = "Saved " + path
	e.notify()
	return nil
}

// writeFileAtomic writes data to a temporary file beside path and renames it
// over path, so a crash or a failed write never truncates the previous scene.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".scene-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Undo reverts the last edit in History (no-op while playing). A step that fails to revert, such as a file move (MoveFile) that something now on disk blocks, stays in place, and the status bar says why.
func (e *Editor) Undo() bool {
	if e.playing {
		return false
	}
	ok, err := e.History.undo()
	if err != nil {
		e.Status = "Undo failed: " + err.Error()
		e.notify()
	}
	if ok {
		e.Status = "Undo"
		e.notify()
	}
	return ok
}

// Redo re-applies the last undone edit in History (no-op while playing). A step that fails to re-apply stays undone, and the status bar says why.
func (e *Editor) Redo() bool {
	if e.playing {
		return false
	}
	ok, err := e.History.redo()
	if err != nil {
		e.Status = "Redo failed: " + err.Error()
		e.notify()
	}
	if ok {
		e.Status = "Redo"
		e.notify()
	}
	return ok
}

// SelectObject selects the object with ID id (or clears selection if id == "").
// Returns true if selection succeeded (or cleared).
func (e *Editor) SelectObject(id string) bool {
	if id == "" {
		if e.SelectedID != "" {
			e.SelectedID = ""
			e.notify()
		}
		return true
	}
	if obj, _, _ := e.FindObject(id); obj == nil {
		return false
	}
	if e.SelectedID != id {
		e.SelectedID = id
		e.Status = "Selected " + id
		e.notify()
	}
	return true
}

// SelectedObject returns the currently selected ObjectDoc and its layer name, or nil, "".
func (e *Editor) SelectedObject() (*sceneformat.ObjectDoc, string) {
	if e.SelectedID == "" || e.Doc == nil {
		return nil, ""
	}
	obj, li, _ := e.FindObject(e.SelectedID)
	if obj == nil {
		return nil, ""
	}
	return obj, e.Doc.Layers[li].Name
}

// FindObject locates a top-level object by ID across all layers, returning it with its layer index and its index in that layer's Objects. A nested child is not found; it moves, duplicates and deletes with its top-level ancestor.
func (e *Editor) FindObject(id string) (*sceneformat.ObjectDoc, int, int) {
	if e.Doc == nil || id == "" {
		return nil, -1, -1
	}
	for li := range e.Doc.Layers {
		layer := &e.Doc.Layers[li]
		for oi := range layer.Objects {
			if layer.Objects[oi].ID == id {
				return &layer.Objects[oi], li, oi
			}
		}
	}
	return nil, -1, -1
}

// objectIDInUse reports whether any object in the scene, nested children included, has ID id. FindObject sees only top-level objects, so ID allocation and duplicate checks go through this instead. A scene that instances or inherits other scene files also holds the IDs they add, which only its resolved copy in e.Built shows, so that copy is searched too.
func (e *Editor) objectIDInUse(id string) bool {
	if e.Doc == nil {
		return false
	}
	if docHoldsID(e.Doc, id) {
		return true
	}
	return e.Built != nil && e.Built.Doc != nil && e.Built.Doc != e.Doc && docHoldsID(e.Built.Doc, id)
}

// docHoldsID reports whether any object of doc, nested children included, has ID id.
func docHoldsID(doc *sceneformat.SceneDoc, id string) bool {
	for li := range doc.Layers {
		found := false
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			found = o.ID == id
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func (e *Editor) layerIndex(name string) int {
	if e.Doc == nil {
		return -1
	}
	for i := range e.Doc.Layers {
		if e.Doc.Layers[i].Name == name {
			return i
		}
	}
	return -1
}

func (e *Editor) resolveLayerIndex(layerName string) (int, error) {
	if e.Doc == nil || len(e.Doc.Layers) == 0 {
		return -1, errors.New("editor: scene has no layers")
	}
	if layerName == "" {
		layerName = e.ActiveLayer
	}
	if layerName == "" {
		return 0, nil
	}
	idx := e.layerIndex(layerName)
	if idx < 0 {
		return -1, fmt.Errorf("editor: layer %q not found", layerName)
	}
	return idx, nil
}

func (e *Editor) generateObjectID(base string) string {
	if base == "" {
		base = "obj"
	}
	for {
		e.nextID++
		candidate := fmt.Sprintf("%s_%d", base, e.nextID)
		if !e.objectIDInUse(candidate) {
			return candidate
		}
	}
}

// PlaceObject adds obj to layerName (or ActiveLayer when empty) as an undoable command,
// merging defaults from e.Prefabs when obj.Prefab matches a registered prefab.
// Returns the placed object's unique ID. It refuses a tree that nests deeper than sceneformat.MaxObjectDepth levels or holds a sheared sprite (see sceneformat.CheckObjectTree), or that reuses an ID, including one the scene gets from a scene it inherits or instances. A tree that instances a scene file (sceneformat.ObjectDoc.Scene) needs a Resolver that implements sceneformat.SceneResolver, and is refused unless the scene still resolves with it placed: see checkPlacedInstances.
func (e *Editor) PlaceObject(layerName string, obj sceneformat.ObjectDoc) (string, error) {
	if e.playing {
		return "", ErrPlaying
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return "", err
	}

	placed := cloneObjectDoc(obj)
	if placed.Prefab != "" {
		if pf, ok := e.Prefabs[placed.Prefab]; ok {
			applyPrefabDefaults(&placed, pf)
		}
	}
	if placed.ID == "" {
		base := placed.Name
		if base == "" && placed.Prefab != "" {
			base = placed.Prefab
		}
		if base == "" && placed.Sprite != nil {
			base = placed.Sprite.Frame
		}
		placed.ID = e.generateObjectID(base)
	}
	var invalid error
	sceneformat.WalkObjects([]sceneformat.ObjectDoc{placed}, func(o, _ *sceneformat.ObjectDoc) bool {
		invalid = validateObjectForEditor(o)
		return invalid == nil
	})
	if invalid != nil {
		return "", invalid
	}
	if err := sceneformat.CheckObjectTree(&placed); err != nil {
		return "", err
	}
	if e.components != nil {
		check := sceneformat.SceneDoc{Layers: []sceneformat.LayerDoc{{Objects: []sceneformat.ObjectDoc{placed}}}}
		if err := sceneformat.ValidateComponents(&check, e.components); err != nil {
			return "", err
		}
	}

	// The placed object's children carry IDs too, and every ID in the tree must be new to the scene and unique within it.
	placedIDs := make(map[string]struct{})
	var dupID string
	sceneformat.WalkObjects([]sceneformat.ObjectDoc{placed}, func(o, _ *sceneformat.ObjectDoc) bool {
		if _, seen := placedIDs[o.ID]; seen || e.objectIDInUse(o.ID) {
			dupID = o.ID
			return false
		}
		placedIDs[o.ID] = struct{}{}
		return true
	})
	if dupID != "" {
		return "", fmt.Errorf("editor: duplicate object id %q", dupID)
	}
	if instancesScene(&placed) {
		if err := e.checkPlacedInstances(li, placed); err != nil {
			return "", err
		}
	}

	cmd := &placeObjectCmd{
		ed:           e,
		layerName:    e.Doc.Layers[li].Name,
		obj:          placed,
		prevSelected: e.SelectedID,
		prevVersion:  e.Doc.Version,
	}
	e.History.Execute(cmd)
	return placed.ID, nil
}

// PlaceScene places an instance of the scene file at scenePath, a slash-separated path from the project root such as "scenes/enemy.scene.yaml", at (x, y) on layerName (or ActiveLayer when empty) as an undoable command, and returns the new object's ID, derived from the file's name. The saved scene keeps the reference (sceneformat.ObjectDoc.Scene), not a copy of the instanced objects, and the viewport draws them through sceneformat.Build. The editor's Resolver must implement sceneformat.SceneResolver; PlaceScene, through PlaceObject, refuses a file that fails to load or resolve, and one that leads back to the open scene (ScenePath). Placing an instance in a version 1 scene raises it to sceneformat.CurrentVersion, which Undo restores.
func (e *Editor) PlaceScene(layerName, scenePath string, x, y float32) (string, error) {
	if e.playing {
		return "", ErrPlaying
	}
	if _, ok := e.Resolver.(sceneformat.SceneResolver); !ok {
		return "", errNoSceneResolver
	}
	if !validScenePath(scenePath) {
		return "", fmt.Errorf("editor: scene %q must be a slash-separated path relative to the project root", scenePath)
	}
	base := path.Base(scenePath)
	if i := strings.IndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	return e.PlaceObject(layerName, sceneformat.ObjectDoc{ID: e.generateObjectID(base), Scene: scenePath, X: x, Y: y})
}

var errNoSceneResolver = errors.New("editor: the resolver cannot load scene files (it does not implement sceneformat.SceneResolver)")

// checkPlacedInstances resolves the scene as it would be with placed added to layer li and returns why it would not resolve: a scene file that fails to load or validate, a cycle, an override that names no object or holds a bad value, or IDs that collide once the instances are expanded. When ScenePath names the open scene, the edited scene is resolved under that path and served for it to every scene file that refers to it, so an instance that leads back to the open scene, directly or through other files, is refused as a cycle before the file on disk instances itself.
func (e *Editor) checkPlacedInstances(li int, placed sceneformat.ObjectDoc) error {
	scenes, ok := e.Resolver.(sceneformat.SceneResolver)
	if !ok {
		return errNoSceneResolver
	}
	candidate := e.Doc.Clone()
	if candidate.Version < sceneformat.CurrentVersion {
		candidate.Version = sceneformat.CurrentVersion
	}
	candidate.Layers[li].Objects = append(candidate.Layers[li].Objects, placed)
	if e.ScenePath == "" {
		_, err := sceneformat.Resolve(candidate, scenes)
		return err
	}
	// The probe inherits ScenePath rather than instancing it, which puts that path on Resolve's chain of files without refusing an open scene that holds a tilemap.
	probe := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Inherits: e.ScenePath}
	_, err := sceneformat.Resolve(probe, editedScene{SceneResolver: scenes, path: e.ScenePath, doc: candidate})
	return err
}

// editedScene serves doc, the open scene as it is being edited, for path, and loads every other scene file through SceneResolver.
type editedScene struct {
	sceneformat.SceneResolver
	path string
	doc  *sceneformat.SceneDoc
}

// Scene returns s.doc for s.path and asks the wrapped resolver for any other path.
func (s editedScene) Scene(p string) (*sceneformat.SceneDoc, error) {
	if p == s.path {
		return s.doc, nil
	}
	return s.SceneResolver.Scene(p)
}

// validScenePath reports whether p has the form sceneformat accepts for a scene reference.
func validScenePath(p string) bool {
	return fs.ValidPath(p) && p != "."
}

// PlacePrefab instantiates a registered prefab at (x, y) on layerName as an undoable command.
func (e *Editor) PlacePrefab(layerName, prefabName string, x, y float32) (string, error) {
	pf, ok := e.Prefabs[prefabName]
	if !ok {
		return "", fmt.Errorf("editor: prefab %q not found", prefabName)
	}
	name := pf.Name
	if name == "" {
		name = prefabName
	}
	return e.PlaceObject(layerName, sceneformat.ObjectDoc{
		Name:   name,
		Prefab: prefabName,
		X:      x,
		Y:      y,
	})
}

// DeleteObject removes the object with ID id from the scene as an undoable command, together with the scene's connections from or to it, its descendants or the objects of scenes they instance; undoing it puts them back where they were.
func (e *Editor) DeleteObject(id string) error {
	if e.playing {
		return ErrPlaying
	}
	obj, li, oi := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	cmd := &deleteObjectCmd{
		ed:           e,
		layerName:    e.Doc.Layers[li].Name,
		index:        oi,
		obj:          cloneObjectDoc(*obj),
		wasNilSlice:  false,
		prevSelected: e.SelectedID,
	}
	e.History.Execute(cmd)
	return nil
}

// MoveObject sets object id's position to (x, y) as an undoable command.
func (e *Editor) MoveObject(id string, x, y float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return errors.New("editor: position must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.X == x && obj.Y == y {
		return nil
	}
	e.History.Execute(&moveObjectCmd{
		ed:   e,
		id:   id,
		oldX: obj.X,
		oldY: obj.Y,
		newX: x,
		newY: y,
	})
	return nil
}

// RotateObject sets object id's rotation (in radians) as an undoable command.
func (e *Editor) RotateObject(id string, radians float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(radians) {
		return errors.New("editor: rotation must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Rotation == radians {
		return nil
	}
	e.History.Execute(&rotateObjectCmd{
		ed:     e,
		id:     id,
		oldRot: obj.Rotation,
		newRot: radians,
	})
	return nil
}

// ScaleObject sets object id's scale to (sx, sy) as an undoable command. It refuses a scale under which a descendant's sprite would be sheared (see sceneformat.CheckObjectTree).
func (e *Editor) ScaleObject(id string, sx, sy float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return errors.New("editor: scale must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.ScaleX != nil && *obj.ScaleX == sx && obj.ScaleY != nil && *obj.ScaleY == sy {
		return nil
	}
	changed := *obj
	changed.ScaleX, changed.ScaleY = &sx, &sy
	if err := sceneformat.CheckObjectTree(&changed); err != nil {
		return err
	}
	e.History.Execute(&scaleObjectCmd{
		ed:        e,
		id:        id,
		oldScaleX: cloneFloat32Ptr(obj.ScaleX),
		oldScaleY: cloneFloat32Ptr(obj.ScaleY),
		newScaleX: sceneformat.Float32(sx),
		newScaleY: sceneformat.Float32(sy),
	})
	return nil
}

// SetObjectAlpha sets object id's opacity in [0, 1] as an undoable command.
func (e *Editor) SetObjectAlpha(id string, alpha float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(alpha) || alpha < 0 || alpha > 1 {
		return fmt.Errorf("editor: alpha %v must be in [0, 1]", alpha)
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Alpha != nil && *obj.Alpha == alpha {
		return nil
	}
	e.History.Execute(&alphaObjectCmd{
		ed:       e,
		id:       id,
		oldAlpha: cloneFloat32Ptr(obj.Alpha),
		newAlpha: sceneformat.Float32(alpha),
	})
	return nil
}

// SetObjectZ sets object id's within-layer draw order Z as an undoable command.
func (e *Editor) SetObjectZ(id string, z int) error {
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Z == z {
		return nil
	}
	e.History.Execute(&zObjectCmd{
		ed:   e,
		id:   id,
		oldZ: obj.Z,
		newZ: z,
	})
	return nil
}

// SetObjectName sets object id's display name as an undoable command.
func (e *Editor) SetObjectName(id, name string) error {
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Name == name {
		return nil
	}
	e.History.Execute(&nameObjectCmd{
		ed:      e,
		id:      id,
		oldName: obj.Name,
		newName: name,
	})
	return nil
}

// Transform3D snapshots an object's 3D transform fields (preserving nil Scale pointers
// so Undo restores unset YAML fields byte-for-byte).
type Transform3D struct {
	X, Y, PosZ       float32
	RotX, RotY, RotZ float32
	ScaleX           *float32
	ScaleY           *float32
	ScaleZ           *float32
}

func captureTransform3D(o *sceneformat.ObjectDoc) Transform3D {
	return Transform3D{
		X:      o.X,
		Y:      o.Y,
		PosZ:   o.PosZ,
		RotX:   o.RotX,
		RotY:   o.RotY,
		RotZ:   o.RotZ,
		ScaleX: cloneFloat32Ptr(o.ScaleX),
		ScaleY: cloneFloat32Ptr(o.ScaleY),
		ScaleZ: cloneFloat32Ptr(o.ScaleZ),
	}
}

func applyTransform3D(o *sceneformat.ObjectDoc, t Transform3D) {
	o.X = t.X
	o.Y = t.Y
	o.PosZ = t.PosZ
	o.RotX = t.RotX
	o.RotY = t.RotY
	o.RotZ = t.RotZ
	o.ScaleX = cloneFloat32Ptr(t.ScaleX)
	o.ScaleY = cloneFloat32Ptr(t.ScaleY)
	o.ScaleZ = cloneFloat32Ptr(t.ScaleZ)
}

func equalFloat32Ptr(a, b *float32) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func equalTransform3D(a, b Transform3D) bool {
	return a.X == b.X && a.Y == b.Y && a.PosZ == b.PosZ &&
		a.RotX == b.RotX && a.RotY == b.RotY && a.RotZ == b.RotZ &&
		equalFloat32Ptr(a.ScaleX, b.ScaleX) &&
		equalFloat32Ptr(a.ScaleY, b.ScaleY) &&
		equalFloat32Ptr(a.ScaleZ, b.ScaleZ)
}

// MoveObject3D sets object id's 3D position (x, y, posZ) as an undoable command.
func (e *Editor) MoveObject3D(id string, x, y, posZ float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) || !isFiniteFloat32(posZ) {
		return errors.New("editor: 3D position must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	before := captureTransform3D(obj)
	after := before
	after.X, after.Y, after.PosZ = x, y, posZ
	if equalTransform3D(before, after) {
		return nil
	}
	e.History.Execute(&transform3DCmd{
		ed:     e,
		id:     id,
		label:  "Move 3D " + id,
		before: before,
		after:  after,
	})
	return nil
}

// RotateObject3D sets object id's 3D Euler rotation (rotX, rotY, rotZ) in radians as an undoable command.
func (e *Editor) RotateObject3D(id string, rotX, rotY, rotZ float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(rotX) || !isFiniteFloat32(rotY) || !isFiniteFloat32(rotZ) {
		return errors.New("editor: 3D rotation must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	before := captureTransform3D(obj)
	after := before
	after.RotX, after.RotY, after.RotZ = rotX, rotY, rotZ
	if equalTransform3D(before, after) {
		return nil
	}
	e.History.Execute(&transform3DCmd{
		ed:     e,
		id:     id,
		label:  "Rotate 3D " + id,
		before: before,
		after:  after,
	})
	return nil
}

// ScaleObject3D sets object id's 3D scale (sx, sy, sz) as an undoable command. It refuses a scale under which a descendant's sprite would be sheared (see sceneformat.CheckObjectTree).
func (e *Editor) ScaleObject3D(id string, sx, sy, sz float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(sx) || !isFiniteFloat32(sy) || !isFiniteFloat32(sz) {
		return errors.New("editor: 3D scale must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	before := captureTransform3D(obj)
	after := before
	after.ScaleX = sceneformat.Float32(sx)
	after.ScaleY = sceneformat.Float32(sy)
	after.ScaleZ = sceneformat.Float32(sz)
	if equalTransform3D(before, after) {
		return nil
	}
	changed := *obj
	applyTransform3D(&changed, after)
	if err := sceneformat.CheckObjectTree(&changed); err != nil {
		return err
	}
	e.History.Execute(&transform3DCmd{
		ed:     e,
		id:     id,
		label:  "Scale 3D " + id,
		before: before,
		after:  after,
	})
	return nil
}

// SetObjectDepth sets object id's 3D depth extent as an undoable command.
func (e *Editor) SetObjectDepth(id string, depth float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(depth) || depth < 0 {
		return fmt.Errorf("editor: depth %v must be finite and non-negative", depth)
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Depth == depth {
		return nil
	}
	e.History.Execute(&depthObjectCmd{
		ed:       e,
		id:       id,
		oldDepth: obj.Depth,
		newDepth: depth,
	})
	return nil
}

// SetObjectMesh sets object id's optional 3D mesh name as an undoable command.
func (e *Editor) SetObjectMesh(id, mesh string) error {
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Mesh == mesh {
		return nil
	}
	e.History.Execute(&meshObjectCmd{
		ed:      e,
		id:      id,
		oldMesh: obj.Mesh,
		newMesh: mesh,
	})
	return nil
}

// --- Commands (all look up target on e.Doc dynamically so Stop/snapshot restore never invalidates history) ---

type placeObjectCmd struct {
	ed           *Editor
	layerName    string
	obj          sceneformat.ObjectDoc
	prevSelected string
	wasNilSlice  bool
	// prevVersion is the scene's version before the placement, which raises a version 1 scene to sceneformat.CurrentVersion when obj instances another scene.
	prevVersion int
}

func (c *placeObjectCmd) Label() string { return "Place object " + c.obj.ID }

func (c *placeObjectCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	c.wasNilSlice = c.ed.Doc.Layers[li].Objects == nil
	if c.ed.Doc.Version < sceneformat.CurrentVersion && instancesScene(&c.obj) {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	c.ed.Doc.Layers[li].Objects = append(c.ed.Doc.Layers[li].Objects, cloneObjectDoc(c.obj))
	c.ed.SelectedID = c.obj.ID
	c.ed.Status = "Placed " + c.obj.ID
}

func (c *placeObjectCmd) Undo() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	objs := c.ed.Doc.Layers[li].Objects
	for i := len(objs) - 1; i >= 0; i-- {
		if objs[i].ID == c.obj.ID {
			objs = append(objs[:i], objs[i+1:]...)
			break
		}
	}
	if len(objs) == 0 && c.wasNilSlice {
		objs = nil
	}
	c.ed.Doc.Layers[li].Objects = objs
	c.ed.Doc.Version = c.prevVersion
	c.ed.SelectedID = c.prevSelected
}

// instancesScene reports whether o or any of its descendants instances a scene file.
func instancesScene(o *sceneformat.ObjectDoc) bool {
	found := false
	sceneformat.WalkObjects([]sceneformat.ObjectDoc{*o}, func(c, _ *sceneformat.ObjectDoc) bool {
		found = c.Scene != ""
		return !found
	})
	return found
}

type deleteObjectCmd struct {
	ed           *Editor
	layerName    string
	index        int
	obj          sceneformat.ObjectDoc
	wasNilSlice  bool
	prevSelected string
	// conns are the connections Do removed with the object, each with its index in the scene's connections before the removal, in ascending index order.
	conns []removedConnection
}

// removedConnection is a connection a delete removed and the index it had.
type removedConnection struct {
	index int
	conn  sceneformat.ConnectionDoc
}

// namesSubtree reports whether id is the ID of an object in ids, or of an object that a scene instanced by one of instancing adds ("enemy_1/body"). Only instancing objects get the prefix match, since an object ID may itself contain a slash: deleting "door" must keep a sibling written "door/x".
func namesSubtree(id string, ids, instancing map[string]struct{}) bool {
	if _, ok := ids[id]; ok {
		return true
	}
	for {
		i := strings.LastIndexByte(id, '/')
		if i < 0 {
			return false
		}
		id = id[:i]
		if _, ok := instancing[id]; ok {
			return true
		}
	}
}

func (c *deleteObjectCmd) Label() string { return "Delete object " + c.obj.ID }

func (c *deleteObjectCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	objs := c.ed.Doc.Layers[li].Objects
	if c.index >= 0 && c.index < len(objs) && objs[c.index].ID == c.obj.ID {
		objs = append(objs[:c.index], objs[c.index+1:]...)
	} else {
		for i := range objs {
			if objs[i].ID == c.obj.ID {
				objs = append(objs[:i], objs[i+1:]...)
				break
			}
		}
	}
	c.ed.Doc.Layers[li].Objects = objs
	ids := make(map[string]struct{})
	instancing := make(map[string]struct{})
	sceneformat.WalkObjects([]sceneformat.ObjectDoc{c.obj}, func(o, _ *sceneformat.ObjectDoc) bool {
		ids[o.ID] = struct{}{}
		if o.Scene != "" {
			instancing[o.ID] = struct{}{}
		}
		return true
	})
	c.conns = nil
	var kept []sceneformat.ConnectionDoc
	for i, conn := range c.ed.Doc.Connections {
		if namesSubtree(conn.From, ids, instancing) || namesSubtree(conn.To, ids, instancing) {
			c.conns = append(c.conns, removedConnection{index: i, conn: conn})
			continue
		}
		kept = append(kept, conn)
	}
	c.ed.Doc.Connections = kept
	if c.ed.SelectedID == c.obj.ID {
		c.ed.SelectedID = ""
	}
	c.ed.Status = "Deleted " + c.obj.ID
}

func (c *deleteObjectCmd) Undo() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	objs := c.ed.Doc.Layers[li].Objects
	idx := c.index
	if idx < 0 {
		idx = 0
	}
	if idx > len(objs) {
		idx = len(objs)
	}
	objs = append(objs, sceneformat.ObjectDoc{})
	copy(objs[idx+1:], objs[idx:])
	objs[idx] = cloneObjectDoc(c.obj)
	c.ed.Doc.Layers[li].Objects = objs
	for _, rc := range c.conns {
		at := min(rc.index, len(c.ed.Doc.Connections))
		c.ed.Doc.Connections = slices.Insert(c.ed.Doc.Connections, at, rc.conn)
	}
	c.ed.SelectedID = c.prevSelected
}

type moveObjectCmd struct {
	ed         *Editor
	id         string
	oldX, oldY float32
	newX, newY float32
}

func (c *moveObjectCmd) Label() string { return "Move " + c.id }

func (c *moveObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.X = c.newX
		obj.Y = c.newY
	}
}

func (c *moveObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.X = c.oldX
		obj.Y = c.oldY
	}
}

type rotateObjectCmd struct {
	ed     *Editor
	id     string
	oldRot float32
	newRot float32
}

func (c *rotateObjectCmd) Label() string { return "Rotate " + c.id }

func (c *rotateObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Rotation = c.newRot
	}
}

func (c *rotateObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Rotation = c.oldRot
	}
}

type scaleObjectCmd struct {
	ed                   *Editor
	id                   string
	oldScaleX, oldScaleY *float32
	newScaleX, newScaleY *float32
}

func (c *scaleObjectCmd) Label() string { return "Scale " + c.id }

func (c *scaleObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.ScaleX = cloneFloat32Ptr(c.newScaleX)
		obj.ScaleY = cloneFloat32Ptr(c.newScaleY)
	}
}

func (c *scaleObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.ScaleX = cloneFloat32Ptr(c.oldScaleX)
		obj.ScaleY = cloneFloat32Ptr(c.oldScaleY)
	}
}

type alphaObjectCmd struct {
	ed       *Editor
	id       string
	oldAlpha *float32
	newAlpha *float32
}

func (c *alphaObjectCmd) Label() string { return "Set alpha " + c.id }

func (c *alphaObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Alpha = cloneFloat32Ptr(c.newAlpha)
	}
}

func (c *alphaObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Alpha = cloneFloat32Ptr(c.oldAlpha)
	}
}

type zObjectCmd struct {
	ed   *Editor
	id   string
	oldZ int
	newZ int
}

func (c *zObjectCmd) Label() string { return "Set Z " + c.id }

func (c *zObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Z = c.newZ
	}
}

func (c *zObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Z = c.oldZ
	}
}

type nameObjectCmd struct {
	ed      *Editor
	id      string
	oldName string
	newName string
}

func (c *nameObjectCmd) Label() string { return "Rename " + c.id }

func (c *nameObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Name = c.newName
	}
}

func (c *nameObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Name = c.oldName
	}
}

type transform3DCmd struct {
	ed     *Editor
	id     string
	label  string
	before Transform3D
	after  Transform3D
}

func (c *transform3DCmd) Label() string {
	if c.label != "" {
		return c.label
	}
	return "Transform 3D " + c.id
}

func (c *transform3DCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		applyTransform3D(obj, c.after)
	}
}

func (c *transform3DCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		applyTransform3D(obj, c.before)
	}
}

type depthObjectCmd struct {
	ed       *Editor
	id       string
	oldDepth float32
	newDepth float32
}

func (c *depthObjectCmd) Label() string { return "Set depth " + c.id }

func (c *depthObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Depth = c.newDepth
	}
}

func (c *depthObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Depth = c.oldDepth
	}
}

type meshObjectCmd struct {
	ed      *Editor
	id      string
	oldMesh string
	newMesh string
}

func (c *meshObjectCmd) Label() string { return "Set mesh " + c.id }

func (c *meshObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Mesh = c.newMesh
	}
}

func (c *meshObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Mesh = c.oldMesh
	}
}

// --- Cloning & validation helpers ---

func applyPrefabDefaults(dst *sceneformat.ObjectDoc, pf Prefab) {
	if dst.Name == "" && pf.Name != "" {
		dst.Name = pf.Name
	}
	if dst.Sprite == nil && pf.Sprite != nil {
		sp := *pf.Sprite
		dst.Sprite = &sp
	}
	if dst.PosZ == 0 && pf.PosZ != 0 {
		dst.PosZ = pf.PosZ
	}
	if dst.OriginX == 0 && pf.OriginX != 0 {
		dst.OriginX = pf.OriginX
	}
	if dst.OriginY == 0 && pf.OriginY != 0 {
		dst.OriginY = pf.OriginY
	}
	if dst.RotX == 0 && pf.RotX != 0 {
		dst.RotX = pf.RotX
	}
	if dst.RotY == 0 && pf.RotY != 0 {
		dst.RotY = pf.RotY
	}
	if dst.RotZ == 0 && pf.RotZ != 0 {
		dst.RotZ = pf.RotZ
	}
	if dst.ScaleX == nil && pf.ScaleX != nil {
		dst.ScaleX = cloneFloat32Ptr(pf.ScaleX)
	}
	if dst.ScaleY == nil && pf.ScaleY != nil {
		dst.ScaleY = cloneFloat32Ptr(pf.ScaleY)
	}
	if dst.ScaleZ == nil && pf.ScaleZ != nil {
		dst.ScaleZ = cloneFloat32Ptr(pf.ScaleZ)
	}
	if dst.Depth == 0 && pf.Depth != 0 {
		dst.Depth = pf.Depth
	}
	if dst.Mesh == "" && pf.Mesh != "" {
		dst.Mesh = pf.Mesh
	}
	if !dst.FlipX && pf.FlipX {
		dst.FlipX = true
	}
	if !dst.FlipY && pf.FlipY {
		dst.FlipY = true
	}
	if dst.Alpha == nil && pf.Alpha != nil {
		dst.Alpha = cloneFloat32Ptr(pf.Alpha)
	}
	if dst.Z == 0 && pf.Z != 0 {
		dst.Z = pf.Z
	}
	if len(pf.Components) > 0 {
		if dst.Components == nil {
			dst.Components = make(map[string]any, len(pf.Components))
		}
		for k, v := range pf.Components {
			if _, exists := dst.Components[k]; !exists {
				dst.Components[k] = cloneAny(v)
			}
		}
	}
	if len(pf.Properties) > 0 {
		if dst.Properties == nil {
			dst.Properties = make(map[string]any, len(pf.Properties))
		}
		for k, v := range pf.Properties {
			if _, exists := dst.Properties[k]; !exists {
				dst.Properties[k] = cloneAny(v)
			}
		}
	}
}

func validateObjectForEditor(o *sceneformat.ObjectDoc) error {
	if o.ID == "" {
		return errors.New("editor: object id must not be empty")
	}
	if !isFiniteFloat32(o.X) || !isFiniteFloat32(o.Y) || !isFiniteFloat32(o.PosZ) ||
		!isFiniteFloat32(o.OriginX) || !isFiniteFloat32(o.OriginY) ||
		!isFiniteFloat32(o.Rotation) || !isFiniteFloat32(o.RotX) || !isFiniteFloat32(o.RotY) || !isFiniteFloat32(o.RotZ) {
		return errors.New("editor: object transform must be finite")
	}
	if !isFiniteFloat32(o.Depth) || o.Depth < 0 {
		return fmt.Errorf("editor: object depth %v must be finite and non-negative", o.Depth)
	}
	if o.ScaleX != nil && !isFiniteFloat32(*o.ScaleX) {
		return errors.New("editor: object scaleX must be finite")
	}
	if o.ScaleY != nil && !isFiniteFloat32(*o.ScaleY) {
		return errors.New("editor: object scaleY must be finite")
	}
	if o.ScaleZ != nil && !isFiniteFloat32(*o.ScaleZ) {
		return errors.New("editor: object scaleZ must be finite")
	}
	if o.Alpha != nil && (!isFiniteFloat32(*o.Alpha) || *o.Alpha < 0 || *o.Alpha > 1) {
		return fmt.Errorf("editor: object alpha %v must be in [0, 1]", *o.Alpha)
	}
	if o.Sprite != nil && (o.Sprite.Atlas == "" || o.Sprite.Frame == "") {
		return errors.New("editor: object sprite requires both atlas and frame")
	}
	if o.Scene != "" && !validScenePath(o.Scene) {
		return fmt.Errorf("editor: object scene %q must be a slash-separated path relative to the project root", o.Scene)
	}
	if len(o.Overrides) > 0 && o.Scene == "" {
		return errors.New("editor: object overrides require a scene")
	}
	return nil
}

func cloneObjectDoc(o sceneformat.ObjectDoc) sceneformat.ObjectDoc {
	return o.Clone()
}

func cloneFloat32Ptr(p *float32) *float32 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneAny(v)
	}
	return out
}

func cloneAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = cloneAny(x[i])
		}
		return out
	default:
		return v
	}
}

func isFiniteFloat32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
