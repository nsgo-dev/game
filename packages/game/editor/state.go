package editor

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/nsgo-dev/game/packages/game/navbake3d"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/game/packages/game/tilemap"
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
	// ToolPickTile takes the tile under the pointer as the active tile (Editor.PickTile), then hands over to ToolPaintTile.
	ToolPickTile
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
	case ToolPickTile:
		return "Pick Tile"
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

// Shading3D selects how the 3D viewport shades the scene's geometry when it draws it on the GPU (Viewport3DOnGPU).
type Shading3D int

const (
	// Shading3DLit lights the geometry with Lights3D, or with DefaultLights3D under a gradient sky when Lights3D is nil.
	Shading3DLit Shading3D = iota
	// Shading3DUnlit draws the geometry at its textured base color plus its emissive color, ignoring the lights.
	Shading3DUnlit
	// Shading3DWireframe lights the geometry as Shading3DLit does and draws the edges of its triangles over it.
	Shading3DWireframe
)

// String returns a human-readable label for the shading mode.
func (s Shading3D) String() string {
	switch s {
	case Shading3DLit:
		return "Lit"
	case Shading3DUnlit:
		return "Unlit"
	case Shading3DWireframe:
		return "Wireframe"
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
	ModelResolver scenemodel.ModelResolver
	Prefabs       map[string]Prefab
	SelectedID    string
	ActiveLayer   string
	Tool          Tool
	ViewMode      ViewMode
	GizmoMode     GizmoMode3D
	Camera3D      OrbitCamera3D
	ActiveTileID  int
	// ActiveTileLayer names the tile layer of a layered grid (sceneformat.TilemapDoc.TileLayers) that PaintTile, EraseTile and FloodFillTile change, empty for its first. A single grid has no tile layers and ignores it.
	ActiveTileLayer string
	// ActiveTileFlip holds the flip bits (tilemap.FlipHorizontal, FlipVertical and FlipDiagonal) that the Paint and Fill tools place tiles of a layered grid with.
	ActiveTileFlip uint32
	SelectedAsset  *AssetEntry
	// SelectedModel is the model the Place tool adds in the 3D viewport (SelectModel), chosen in the asset browser's Models list; nil places nothing in 3D, where a press then selects.
	SelectedModel *ModelEntry
	// PlaceHeight3D is the height of the ground plane, the horizontal plane the Place tool puts a model on in 3D where the pointer's ray hits no geometry (PlacementAt3D).
	PlaceHeight3D float32
	// PlaceSnap3D snaps where the Place tool puts a model on the ground plane in 3D to the 3D grid's spacing along X and Z; a model placed on the scene's geometry stays where the pointer's ray hits it (PlacementAt3D).
	PlaceSnap3D bool
	// PlaceAlign3D turns a model the Place tool puts on the scene's geometry in 3D so that its +Y axis follows the surface's normal.
	PlaceAlign3D bool
	Status       string
	FilePath     string
	// Shading3D is how the 3D viewport shades the scene's geometry on the GPU (SetShading3D).
	Shading3D Shading3D
	// Lights3D are the lights the 3D viewport draws the scene's geometry with, such as the rig the game lights the scene with, over the scene's background color; nil draws it with DefaultLights3D under a gradient sky. Scene files do not hold lights, so an editor built on this package that knows its game's lights sets them with SetLights3D, which redraws the viewport; assigning the field directly does not.
	Lights3D *render3d.LightRig
	// FlySpeed3D is how fast the 3D viewport's camera flies (BeginFly3D), in world units a second; the wheel changes it while flying (AdjustFlySpeed3D), and the first flight sets it from the camera's Distance when it is not positive.
	FlySpeed3D float32
	// Project is the game's project file (game.yaml) OpenProject opened, nil without one; EditProject changes it undoably and SaveProject writes it.
	Project *project.File
	// ProjectPath is the game.yaml path OpenProject opened Project from.
	ProjectPath string
	// ScenePath is the open scene's slash-separated path from the project root, as Resolver names scene files (for example "scenes/level.scene.yaml"), or empty when the scene has no such path. PlaceObject and PlaceScene resolve the edited scene under this path, so they refuse an instance that would lead back to the open scene and make the saved file instance itself. OpenFile, SaveFile and OpenProject set it when FilePath lies in the open project's directory and clear it otherwise; OpenYAML clears it, since the loaded scene may come from another file.
	ScenePath string
	// SelectedFile is the slash-separated path of the file selected in the file browser (FilesView), relative to the open project's root, or empty when none is selected.
	SelectedFile string
	// Library is the pack library the library panel lists and installs from into the open project (InstallPack); nil hides the panel.
	Library *library.Library
	// ProjectAssets makes the editor load its assets from the open project: OpenProject and OpenProjectScene make Resolver a project.Resolver over the project's directory, so the scene draws the project's atlases, the asset browser lists them (Assets), PlaceScene instances its scene files and materials and Tiled maps load from it. Atlases are found nearest the open scene (ScenePath), as `game check` finds them, and listed again whenever the editor changes the project's files. Set it before opening a project; editorcmd sets it.
	ProjectAssets bool
	// AppName is the editor's name, which the window title EditorView sets ends with (WindowTitle); empty means "NSGo Scene Editor". An editor built on this package, such as a game's own, sets it to its own name, since EditorView owns the window title.
	AppName string

	// OnSave is invoked by the UI Save button with the encoded YAML bytes.
	OnSave func(data []byte) error
	// OnRunGame is invoked by the Run Game command (shortcut, menu bar, or command palette).
	// When nil, RunGame starts play-in-editor if a UI scope is attached.
	OnRunGame func() error
	// OnPlayUpdate runs on every fixed simulation step while Play mode is active.
	OnPlayUpdate func(scene *sceneformat.BuiltScene, dt time.Duration)
	// OnPlayDraw runs on every render frame while Play mode is active.
	OnPlayDraw func(scene *sceneformat.BuiltScene, alpha float64)
	// OnPlayTrigger runs whenever a built-in 2D or 3D physics trigger fires enter, stay, or exit during Play mode.
	OnPlayTrigger func(ev sceneformat.PhysicsTriggerEvent)
	// ShowPhysicsDebug controls whether 2D and 3D physics collider and trigger outlines draw in the viewport. New initializes it to true.
	ShowPhysicsDebug bool
	// ShowLightingPreview controls whether 2D lights, occluder shadows, and ambient darkness draw live in the 2D edit viewport. New initializes it to true.
	ShowLightingPreview bool
	// ShowNavMesh3D controls whether the baked 3D navigation mesh overlay draws in the 3D viewport. New initializes it to true.
	ShowNavMesh3D bool
	// OnBakeNavMesh3DGeometry, when set, is called by BakeNavMesh3D after collecting the open scene's static geometry so games can add programmatic terrain, GridMaps, or physics colliders.
	OnBakeNavMesh3DGeometry func(g *navbake3d.Geometry) error
	// NavMesh3DAssetFS is an optional fallback file system used by BakeNavMesh3D and LoadNavMesh3D to resolve glTF models and instanced scenes when no project directory is open.
	NavMesh3DAssetFS fs.FS
	// ShowLODOverlay controls whether per-object active LOD labels and color-coding draw in the 3D viewport. New initializes it to true.
	ShowLODOverlay bool
	// ShowOccluders controls whether 3D occluder volumes draw in the 3D viewport. New initializes it to true.
	ShowOccluders bool
	// ShowRenderStats controls whether the RenderStats HUD overlay shows in the 3D viewport. New initializes it to true.
	ShowRenderStats bool
	// PlayRate is the fixed update rate in Hz during Play mode and clip playback (defaults to 60 when <= 0).
	PlayRate int
	// OnSaveClip is invoked by the animation panel's Save button with the encoded clip YAML; when nil the button writes to ClipPath.
	OnSaveClip func(data []byte) error
	// OnSaveStates is invoked by the state machine panel's Save button with the encoded *.states.yaml bytes; when nil the button writes to StatesPath.
	OnSaveStates func(data []byte) error
	// Clips optionally resolves animation clips for the open sprite state machine when no project directory holds them on disk.
	Clips animclip.ClipResolver
	// OnSaveDataTable is invoked by the Data Table panel's Save button with the encoded table YAML; when nil the button writes to DataTablePath.
	OnSaveDataTable func(data []byte) error
	// OnSaveGridMap is invoked by SaveGridMap (and Save when a GridMap has unsaved edits) with the encoded canonical .gridmap.yaml bytes; when nil SaveGridMap writes to the GridMap's file path.
	OnSaveGridMap func(data []byte) error
	// GridMapFS optionally provides a filesystem for loading GridMap files and their glTF mesh libraries when no project is open on disk.
	GridMapFS fs.FS
	// ExportCommandRunner runs `go` with args for the Export panel's release builds, streaming output to stdout and stderr; when nil the editor invokes `go` via os/exec.
	ExportCommandRunner func(ctx context.Context, args []string, stdout, stderr io.Writer) error
	// OpenOutputFolder opens the release output directory in the OS file manager after a successful non-dry-run build; when nil the editor uses the host platform's default opener.
	OpenOutputFolder func(dir string) error
	// RunGameCommand starts the child game process for RunGame / RunGameWith, streaming output to stdout and stderr; when nil the editor launches `go tool game dev` in the open project directory in its own process group on desktop.
	RunGameCommand func(spec RunGameSpec, stdout, stderr io.Writer) (GameProcess, error)
	// AssetsCommandRunner runs `go tool game assets` for BuildAssets and ReimportAsset, streaming output to stdout and stderr; when nil the editor invokes `go` in the open project directory on desktop.
	AssetsCommandRunner func(ctx context.Context, spec AssetsBuildSpec, stdout, stderr io.Writer) error
	// OnReloadAssets, when set, is called after ReloadAssets reloads changed atlases or textures from disk.
	OnReloadAssets func(result ReloadAssetsResult)
	// OnExportProfilerTrace is invoked by the Profiler panel's Export Trace button with the Chrome/Perfetto Trace Event Format JSON bytes; when nil the button opens a Save File dialog.
	OnExportProfilerTrace func(data []byte) error
	// ProfilerPollInterval configures how often the editor polls game_perf_readout and game_perf_zones while connected to a running game (defaults to 250ms when <= 0).
	ProfilerPollInterval time.Duration
	// RecentProjectsPath is the per-user YAML file where recently opened projects are stored outside any project directory. When empty, recent projects stay in memory without writing to disk.
	RecentProjectsPath string
	// Now returns the current time for recent-project timestamps and relative-time labels; when nil, time.Now is used.
	Now func() time.Time
	// PickProjectFolder prompts the user to choose a project folder (for Open Project and New Project's parent folder). When nil and a UI scope is attached, the editor invokes cx.PickDirectories.
	PickProjectFolder func(title, startDir string, done func(dir string, ok bool, err error))
	// CreateProject runs project creation from a template (such as `game new`), streaming its output to stdout and stderr. When nil on desktop, the editor invokes `go tool game new`.
	CreateProject func(ctx context.Context, req CreateProjectRequest, stdout, stderr io.Writer) error
	// OnOpenDemo overrides the Start Screen's Open Demo action when non-nil; when nil, OpenDemo loads the built-in Demo Room.
	OnOpenDemo func() error

	playing           bool
	playSnapshot      []byte
	playStop          func()
	playScene         *sceneformat.BuiltScene
	playPhysics       *sceneformat.PhysicsScene
	playTriggerEvents []sceneformat.PhysicsTriggerEvent
	playSteps         int
	playFrames        int
	physicsLayers     *sceneformat.PhysicsLayerTable

	// projectSaved is Project's encoding when it was opened or last saved, which ProjectDirty compares against.
	projectSaved []byte
	// projectSavedOnce reports whether SaveProject has written the currently open project at least once.
	projectSavedOnce bool
	// sceneSaved is Doc's encoding when it was opened or last saved, which SceneDirty compares against.
	sceneSaved []byte
	// clipSaved is the open clip's encoding when it was opened or last saved, which ClipDirty compares against.
	clipSaved []byte
	// tableSaved is the open data table's encoding when it was opened or last saved, which DataTableDirty compares against.
	tableSaved []byte

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

	// The sprite state machine editor: open *.states.yaml graph, live preview player and Drive with keys state.
	states statesEditorState

	// The data table editor: the open table state and registered Go row types per file pattern.
	table      *dataTableState
	tableTypes *datatable.Registry

	// l10n holds the open localization string tables edited by the Localization panel, or nil when closed.
	l10n       *localizationState
	components *sceneformat.ComponentRegistry
	// plugins are the registered plugins in registration order, and pluginTool the plugin tool chosen while Tool is ToolPlugin.
	plugins            []*Plugin
	pluginTool         pluginToolRef
	focusedPluginPanel panelRef
	commands           []EditorCommand
	paletteOpen        bool
	drag3D             drag3DState
	// fly3D says the 3D viewport's camera is flying (BeginFly3D).
	fly3D bool
	// fly3DOrtho says the camera was orthographic when the flight began, and fly3DMoved that the flight has turned or moved it, so a flight that did neither puts the projection back.
	fly3DOrtho   bool
	fly3DMoved   bool
	cameraDrag   cameraDragState
	bgPreview    bgPreviewState
	scope        *core.Scope
	revision     uint64
	nextID       int
	nextListener uint64
	listeners    map[uint64]func()
	// askUnsaved asks the author, through the prompt EditorView shows, whether to save or discard unsaved edits before something loses them; nil without an EditorView.
	askUnsaved func(proceed, cancel func())
	// fetch is the library panel's fetch of a pack from a remote library under way, or nil.
	fetch *packFetch
	// hidden and locked are the objects the scene tree hides in the viewports or locks against picking (SetObjectHidden, SetObjectLocked); they belong to the session, not the scene file.
	hidden map[string]bool
	locked map[string]bool
	// layerLocked are the layers the layers panel locks against the viewports (SetLayerLocked); like hidden and locked, they belong to the session.
	layerLocked map[string]bool
	// baseLayers caches the names of the layers of the scene the open scene inherits (inheritedLayers).
	baseLayers baseLayerCache
	// assetsScene is the ScenePath the project.Resolver of ProjectAssets finds atlases nearest, and assetsResolverRev the assetsRev it last read the project's atlases and materials at (syncProjectAssets).
	assetsScene       string
	assetsResolverRev uint64
	// objIndex is where every object of the open scene sits, rebuilt when the revision or the document changes (lookupObject).
	objIndex objectIndex
	// filesRev counts the changes the editor made to which files the open project holds, by opening it, moving files, saving a scene or clip, which may make a new file, and installing or removing packs; the file browser lists the files again when it changes.
	filesRev uint64
	// assetsRev counts the changes the editor made that may change the content of the project's models, material files and images, by opening a project, moving files, installing or removing packs, reloading built assets (ReloadAssets), and rescanning the project from disk (RefreshSearch); the 3D build loads its assets again when it changes. Saving a scene, clip, data table or localization table writes none of them, so it leaves the 3D build's assets alone.
	assetsRev uint64
	// audioMixer holds the Audio Bus Mixer panel state and its live audition sound.Mixer.
	audioMixer audioMixerState
	// projectSettings holds the Project Settings panel visibility and active section.
	projectSettings projectSettingsState
	// inputMap holds the Input Map panel state, key capture mode, and region canvas drag preview.
	inputMap inputMapState
	// selectedFileRev bumps whenever SelectFile or OpenProblem updates SelectedFile so FilesView resets its target path draft.
	selectedFileRev uint64
	// problems holds the asynchronous assetcheck and project.Check state for the open project.
	problems problemState
	// exportPanel holds the Export & Release panel state, credits pre-flight report, and build output stream.
	exportPanel exportPanelState
	// search holds the cached project search index, active search results and Find References state.
	search projectSearchState
	// view2D is the 2D viewport's own view while view2DFree is set, after the author panned or zoomed; until then the view is the scene camera's (View2D).
	view2D     View2D
	view2DFree bool
	// canvasW and canvasH are the viewport canvas's laid-out size (SetCanvasSize), zero until it lays out.
	canvasW, canvasH float32
	// output holds the docked Output / Log panel history and filter state.
	output outputPanelState
	// profiler holds the live remote/play-mode performance telemetry, recorded frame timeline, zones table, and panel visibility.
	profiler profilerState
	// gameRun tracks the live `go tool game dev` child process started by RunGame / RunGameWith.
	gameRun gameRunState
	// drag2D is the 2D viewport's drag of an object or a gizmo handle in progress (BeginDrag2D).
	drag2D drag2DState
	// stroke is the tile stroke under way (BeginTileStroke), or nil.
	stroke *tileStroke
	// hover is where the mouse rests over the viewport (SetTileHover).
	hover tileHover
	// inspectAssetFile prefers the selected asset's import settings in InspectorView over SelectedID after selecting an asset in FilesView.
	inspectAssetFile bool
	// inspectLayer prefers ActiveLayer's parallax/repeat settings in InspectorView over Scene Settings when no object or asset file is selected (set by SetActiveLayer, cleared by SelectObject or opening a scene).
	inspectLayer bool
	// importRev bumps whenever an asset's *.import.yaml sidecar is written, removed, undone, or redone so InspectorView resyncs text drafts.
	importRev uint64
	// assetBuild tracks asynchronous `go tool game assets` builds and last-loaded output hashes for live atlas/texture reloading.
	assetBuild assetBuildState
	// connPick holds an active target-object pick callback for the connections panel (picking from viewport or scene tree without changing SelectedID).
	connPick *connPickState
	// s3d is the open scene built in 3D, which the 3D viewport draws and picks by (Scene3D).
	s3d scene3DState
	// modelFiles caches the project's model files ProjectModels lists, and thumbs the asset browser's model thumbnails (ModelThumbnail).
	modelFiles modelFileCache
	thumbs     modelThumbs
	// gpu3D says the 3D viewport's GPU view drew the scene's geometry the last frame (Viewport3DOnGPU), and gpu3DErr why it could not, when it could not.
	gpu3D    bool
	gpu3DErr error
	// gridMap holds the active 3D GridMap editing session, tool, floor height, orientation, stroke and hover state.
	gridMap gridMapState
	// recentProjects holds recently opened projects in most-recently-opened order.
	recentProjects []RecentProject
	// startScreen holds the Start Screen UI, template list, new-project form, and creation output state.
	startScreen startScreenState
	// unsavedPrompt tracks a pending project switch waiting for user confirmation of unsaved edits.
	unsavedPrompt unsavedPromptState
	// navMesh3D holds the 3D navigation mesh bake settings, panel visibility, and cached preview mesh.
	navMesh3D navMesh3DState
	// vp3d holds cached 3D render3d.Queue/OcclusionBuffer state, active LOD indices and RenderStats for the 3D viewport.
	vp3d viewport3DState
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
		Doc:                 doc,
		History:             NewHistory(),
		Resolver:            resolver,
		Prefabs:             make(map[string]Prefab),
		Tool:                ToolSelect,
		ViewMode:            ViewMode2D,
		GizmoMode:           Gizmo3DTranslate,
		Camera3D:            DefaultOrbitCamera3D(),
		ActiveTileID:        1,
		Status:              "Ready",
		ShowPhysicsDebug:    true,
		ShowLightingPreview: true,
		ShowNavMesh3D:       true,
		navMesh3D:           navMesh3DState{config: DefaultNavMeshBakeConfig()},
		ShowLODOverlay:      true,
		ShowOccluders:       true,
		ShowRenderStats:     true,
		selKey:              clipKeyRef{-1, -1},
		listeners:           make(map[uint64]func()),
		output: outputPanelState{
			open:       false,
			lastStatus: "Ready",
		},
	}
	if len(doc.Layers) > 0 {
		ed.ActiveLayer = doc.Layers[0].Name
	}
	ed.keepTileInPalette()
	// The scene the editor starts on is clean: an error here leaves it dirty, which only asks before losing it.
	ed.sceneSaved, _ = sceneformat.Encode(doc)
	ed.History.onChange = func() {
		ed.afterMutate()
	}
	ed.initBuiltinCommands()
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

// AttachScope binds a core.Scope to the editor so Play() can launch game.Loop from UI controls and background checks or game runs can post updates to the UI goroutine.
func (e *Editor) AttachScope(cx *core.Scope) {
	e.scope = cx
	e.problems.setScope(cx)
	e.gameRun.setScope(cx)
	e.assetBuild.setScope(cx)
	e.profiler.setScope(cx)
	if cx != nil && e.ProfilerConnected() && !e.isPlayProfilerActive() {
		e.startProfilerPoller()
	} else if cx == nil {
		e.stopProfilerPoller()
	}
	e.startScreen.setScope(cx)
}

func (e *Editor) notify() {
	e.captureStatusForOutput()
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
	// Undoing a layer's addition, or redoing its removal, may take the active layer away.
	e.keepActiveLayer()
	if e.connPick != nil && e.SelectedID != e.connPick.sourceID {
		e.connPick = nil
	}
	// An undone or redone project edit may have turned off the plugin whose tool is active.
	e.dropStalePluginTool()
	e.dropStaleFocusedPluginPanel()
	e.syncAudioMixer()
	e.syncExportPanel()
	e.Rebuild()
	e.refreshOpenSceneSearch()
	e.notify()
}

// Rebuild resolves e.Doc against e.Resolver into e.Built. The scene's 3D build (Scene3D) is brought up to date the next time the 3D viewport draws or picks; a 3D gizmo drag in progress changes only the dragged object's transform, which moves its built node in place.
func (e *Editor) Rebuild() {
	defer e.refreshClipPreview()
	defer e.refreshStatesPreview()
	if e.drag3D.kind != drag3DGizmo {
		e.s3d.stale = true
	}
	if e.Doc == nil {
		e.Built = nil
		e.BuildErr = nil
		return
	}
	e.syncProjectAssets()
	built, err := sceneformat.Build(e.Doc, e.Resolver)
	if err != nil {
		e.BuildErr = err
		return
	}
	// The 2D view pans and zooms past the scene camera's frame, so the edit scene draws every tile of its maps.
	built.DrawAllTiles = true
	e.Built = built
	e.BuildErr = nil
}

// ActiveScene returns the live play scene when playing, the scene posed by the open state machine or animation clip when previewing, or the edit BuiltScene otherwise.
func (e *Editor) ActiveScene() *sceneformat.BuiltScene {
	if e.playing && e.playScene != nil {
		return e.playScene
	}
	if e.states.scene != nil {
		return e.states.scene
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
	e.CancelDrag2D()
	e.CancelCameraDrag()
	e.endTileStrokeForChange()
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
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	e.CancelGizmoDrag3D()
	e.EndViewport3DDrag()
	e.endFly3D()
	e.CancelDrag2D()
	e.CancelCameraDrag()
	e.ViewMode = mode
	e.Status = "View: " + mode.String()
	e.notify()
}

// SetShading3D switches how the 3D viewport shades the scene's geometry on the GPU. It is a view setting, not an edit, so it is not undoable and is not saved with the scene; an unknown mode is ignored.
func (e *Editor) SetShading3D(mode Shading3D) {
	if e.Shading3D == mode || mode < Shading3DLit || mode > Shading3DWireframe {
		return
	}
	e.Shading3D = mode
	e.Status = "Shading: " + mode.String()
	e.notify()
}

// SetLights3D sets the lights the 3D viewport draws the scene's geometry with (Lights3D) and redraws it; nil goes back to DefaultLights3D under a gradient sky. Like Shading3D, the lights are a view setting: setting them is not undoable and changes nothing in the scene file.
func (e *Editor) SetLights3D(lights *render3d.LightRig) {
	if e == nil {
		return
	}
	e.Lights3D = lights
	e.notify()
}

// SetGizmoMode3D switches the active 3D transform gizmo mode (Translate, Rotate, or Scale) and leaves any active GridMap tool.
func (e *Editor) SetGizmoMode3D(mode GizmoMode3D) {
	if e.GizmoMode == mode && e.gridMap.tool == GridMapToolNone {
		return
	}
	e.endGridMapStrokeForChange()
	e.CancelGizmoDrag3D()
	e.gridMap.tool = GridMapToolNone
	e.GizmoMode = mode
	e.Status = "Gizmo: " + mode.String()
	e.notify()
}

// SetActiveLayer selects the layer targeted by object placement and tile painting. When the layer's tileset (TilePalette) has no tile with ActiveTileID, its first tile becomes the active tile.
func (e *Editor) SetActiveLayer(name string) bool {
	if e.layerIndex(name) < 0 {
		return false
	}
	if name != e.ActiveLayer {
		e.endTileStrokeForChange()
	}
	e.CancelCameraDrag()
	e.ActiveLayer = name
	e.inspectAssetFile = false
	e.inspectLayer = true
	e.keepTileInPalette()
	e.notify()
	return true
}

// SetActiveTileLayer selects the tile layer of the active layer's layered grid that tile painting changes, as ActiveTileLayer describes; empty selects its first. It reports false, changing nothing, when the active layer's tilemap is not a layered grid with a tile layer of that name.
func (e *Editor) SetActiveTileLayer(name string) bool {
	_, tm, err := e.resolveInlineTilemap(e.ActiveLayer)
	if err != nil || !tm.IsLayered() || (name != "" && tm.TileLayer(name) == nil) {
		return false
	}
	if name != e.ActiveTileLayer {
		e.endTileStrokeForChange()
	}
	e.ActiveTileLayer = name
	e.notify()
	return true
}

// SetActiveTileFlip sets the flip bits the Paint and Fill tools place tiles of a layered grid with (ActiveTileFlip); bits other than tilemap.FlipMask are dropped.
func (e *Editor) SetActiveTileFlip(flip uint32) {
	e.ActiveTileFlip = flip & tilemap.FlipMask
	e.notify()
}

// SetActiveTileID sets the tile GID used by PaintTile and FloodFillTile tools, which the tile palette (TilePaletteView) sets when a tile is pressed.
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
	if e.Project != nil && e.ProjectPath != "" {
		e.CheckProblems()
	}
	return nil
}

// ComponentRegistry returns the registry set by SetComponentRegistry, or nil.
func (e *Editor) ComponentRegistry() *sceneformat.ComponentRegistry {
	return e.components
}

// SetPhysicsLayers configures the named collision layer table used when validating and playing scenes without an open project. When a project with physics settings is open, the project's physics.layers take precedence.
func (e *Editor) SetPhysicsLayers(layers *sceneformat.PhysicsLayerTable) {
	if e == nil {
		return
	}
	e.physicsLayers = layers
	e.notify()
}

// PhysicsLayers returns the active named collision layer table: the open project's physics.layers when present, otherwise the table set by SetPhysicsLayers, or nil.
func (e *Editor) PhysicsLayers() *sceneformat.PhysicsLayerTable {
	if e == nil {
		return nil
	}
	if e.Project != nil && e.Project.Physics != nil {
		return e.Project.PhysicsLayerTable()
	}
	return e.physicsLayers
}

// SetShowPhysicsDebug toggles collider and trigger debug drawing in the 2D and 3D viewports.
func (e *Editor) SetShowPhysicsDebug(show bool) {
	if e == nil || e.ShowPhysicsDebug == show {
		return
	}
	e.ShowPhysicsDebug = show
	e.notify()
}

func (e *Editor) physicsSettings() *sceneformat.PhysicsSettings {
	if e == nil {
		return nil
	}
	if e.Project != nil && e.Project.Physics != nil {
		return e.Project.PhysicsSettings()
	}
	if e.physicsLayers != nil {
		return &sceneformat.PhysicsSettings{Layers: e.physicsLayers}
	}
	return nil
}

// OpenYAML parses data as a sceneformat.SceneDoc, replaces the active document,
// clears undo/redo history, and rebuilds the scene.
func (e *Editor) OpenYAML(data []byte) error {
	return e.openYAMLWithLayers(data, "", e.PhysicsLayers())
}

func (e *Editor) openYAML(data []byte, file string) error {
	return e.openYAMLWithLayers(data, file, e.PhysicsLayers())
}

func (e *Editor) openYAMLWithLayers(data []byte, file string, layers *sceneformat.PhysicsLayerTable) error {
	if e.playing {
		if err := e.Stop(); err != nil {
			return err
		}
	}
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
		File:          file,
		Components:    e.components,
		PhysicsLayers: layers,
	})
	if err != nil {
		return err
	}
	saved, err := sceneformat.Encode(doc)
	if err != nil {
		return err
	}
	// The scene being replaced keeps its view for when it opens again.
	e.saveViewStateQuietly()
	e.drag2D = drag2DState{}
	e.cameraDrag = cameraDragState{}
	e.bgPreview = bgPreviewState{}
	// A stroke under way edits the scene being replaced, whose history goes with it.
	e.stroke = nil
	e.gridMap.stroke = nil
	e.Doc = doc
	e.sceneSaved = saved
	e.SelectedID = ""
	e.inspectAssetFile = false
	e.inspectLayer = false
	e.connPick = nil
	// Another scene opens at its own camera's view.
	e.view2DFree = false
	e.hidden, e.locked, e.layerLocked = nil, nil, nil
	// The loaded scene did not come from the current path, so a later Save must
	// not overwrite the previously opened file with unrelated content.
	e.FilePath = ""
	e.ScenePath = ""
	if len(doc.Layers) > 0 {
		e.ActiveLayer = doc.Layers[0].Name
	} else {
		e.ActiveLayer = ""
	}
	e.keepTileInPalette()
	e.resetNavMeshForScene()
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
	return e.openFileWithLayers(path, e.PhysicsLayers())
}

func (e *Editor) openFileWithLayers(path string, layers *sceneformat.PhysicsLayerTable) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return loaderr.Locate(err, path, nil, "check that the scene exists at this path and can be read")
	}
	if err := e.openYAMLWithLayers(data, path, layers); err != nil {
		return loaderr.WithFile(err, path)
	}
	e.FilePath = path
	e.syncScenePath()
	e.navMesh3D.config.OutputPath = e.derivedNavMeshPath()
	e.tryAutoloadNavMesh3D()
	e.Status = "Opened " + path
	e.restoreViewState()
	e.notify()
	return nil
}

// SaveFile encodes the scene to YAML and writes it to path.
func (e *Editor) SaveFile(path string) error {
	if !e.playing {
		e.CancelCameraDrag()
		e.cancelBackgroundPreview()
		e.endTileStrokeForChange()
		e.endGridMapStrokeForChange()
	}
	data, err := e.SaveYAML()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	e.FilePath = path
	e.sceneSaved = append([]byte(nil), data...)
	e.syncScenePath()
	e.saveViewStateQuietly()
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = "Saved " + path
	e.notify()
	e.CheckProblems()
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
	// A drag in progress is put back first, so the step undone is the one before it and not the half-dragged object.
	e.CancelDrag2D()
	e.CancelCameraDrag()
	e.cancelBackgroundPreview()
	// A stroke under way is recorded first, so Undo takes it back as a whole.
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	ok, err := e.History.undo()
	if err != nil {
		e.reportStatusError("Undo failed: " + err.Error())
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
	e.CancelDrag2D()
	e.CancelCameraDrag()
	e.cancelBackgroundPreview()
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	ok, err := e.History.redo()
	if err != nil {
		e.reportStatusError("Redo failed: " + err.Error())
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
		if e.SelectedID != "" || e.inspectAssetFile || e.inspectLayer || e.connPick != nil {
			e.SelectedID = ""
			e.inspectAssetFile = false
			e.inspectLayer = false
			e.connPick = nil
			e.notify()
		}
		return true
	}
	if obj, _, _ := e.FindObject(id); obj == nil {
		return false
	} else if obj.GridMap != "" && e.gridMap.relPath != obj.GridMap && !e.GridMapDirty() {
		// Selecting an object that names a GridMap loads it automatically when a project or GridMapFS is available.
		e.SelectedID = id
		e.Status = "Selected " + id
		e.autoOpenSceneGridMap()
		e.notify()
		return true
	}
	if e.SelectedID != id || e.inspectAssetFile || e.inspectLayer || e.connPick != nil {
		e.CancelCameraDrag()
		e.SelectedID = id
		e.inspectAssetFile = false
		e.inspectLayer = false
		e.connPick = nil
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

// FindObject locates the object with ID id anywhere in the scene, nested children included, returning it with its layer index and its index among its siblings (the children of its parent, or its layer's top-level objects). It returns nil, -1, -1 when the open scene does not hold id, as for an object only an instanced or inherited scene adds.
func (e *Editor) FindObject(id string) (*sceneformat.ObjectDoc, int, int) {
	loc := locateObject(e.Doc, id)
	return loc.obj, loc.layer, loc.index
}

// objectIDInUse reports whether any object in the scene, nested children included, has ID id. A scene that instances or inherits other scene files also holds the IDs they add, which only its resolved copy in e.Built shows, so that copy is searched too.
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
// Returns the placed object's unique ID. It refuses a tree that nests deeper than sceneformat.MaxObjectDepth levels or holds a sheared sprite (see sceneformat.CheckObjectTree), or that reuses an ID, including one the scene gets from a scene it inherits or instances, and an object whose mesh and model fields sceneformat.CheckObjectModel refuses. Placing an object that draws a glTF model in a version 1 scene raises it to sceneformat.CurrentVersion, which Undo restores. A tree that instances a scene file (sceneformat.ObjectDoc.Scene) needs a Resolver that implements sceneformat.SceneResolver, and is refused unless the scene still resolves with it placed: see checkPlacedInstances.
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

// DeleteObject removes the object with ID id, a top-level object or a nested child, from the scene as an undoable command, together with the scene's connections from or to it, its descendants or the objects of scenes they instance; undoing it puts them back where they were.
func (e *Editor) DeleteObject(id string) error {
	if e.playing {
		return ErrPlaying
	}
	// A drag in progress is put back first, so Undo of the delete restores the object as it was before the drag.
	e.CancelDrag2D()
	e.CancelCameraDrag()
	loc := locateObject(e.Doc, id)
	if loc.obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	cmd := &deleteObjectCmd{
		ed:           e,
		layerName:    e.Doc.Layers[loc.layer].Name,
		index:        loc.index,
		obj:          cloneObjectDoc(*loc.obj),
		wasNilSlice:  false,
		prevSelected: e.SelectedID,
	}
	if loc.parent != nil {
		cmd.parent = loc.parent.ID
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

// RotateObject sets object id's rotation (in radians) as an undoable command. It refuses a rotation under which the object's or a descendant's sprite would be sheared by a non-uniformly scaled ancestor (see sceneformat.CheckObjectTree).
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
	if err := e.checkTransformEdit(id, func(o *sceneformat.ObjectDoc) { o.Rotation = radians }); err != nil {
		return err
	}
	e.History.Execute(&rotateObjectCmd{
		ed:     e,
		id:     id,
		oldRot: obj.Rotation,
		newRot: radians,
	})
	return nil
}

// ScaleObject sets object id's scale to (sx, sy) as an undoable command. It refuses a scale under which the object's or a descendant's sprite would be sheared, taking its ancestors' transforms into account (see sceneformat.CheckObjectTree).
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
	if err := e.checkTransformEdit(id, func(o *sceneformat.ObjectDoc) { o.ScaleX, o.ScaleY = &sx, &sy }); err != nil {
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

// ScaleObject3D sets object id's 3D scale (sx, sy, sz) as an undoable command. It refuses a scale under which the object's or a descendant's sprite would be sheared, taking its ancestors' transforms into account (see sceneformat.CheckObjectTree).
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
	if err := e.checkTransformEdit(id, func(o *sceneformat.ObjectDoc) { applyTransform3D(o, after) }); err != nil {
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

// SetObjectMesh sets the built-in primitive object id draws in 3D, one of sceneformat.MeshPrimitives or "" for none, as an undoable command. It refuses any other name, and a primitive on an object that names a model.
func (e *Editor) SetObjectMesh(id, mesh string) error {
	if e.playing {
		return ErrPlaying
	}
	if mesh != "" && !slices.Contains(sceneformat.MeshPrimitives(), mesh) {
		return fmt.Errorf("editor: mesh %q is not a built-in primitive (want one of %s)", mesh, strings.Join(sceneformat.MeshPrimitives(), ", "))
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Mesh == mesh {
		return nil
	}
	if mesh != "" && obj.Model != "" {
		return fmt.Errorf("editor: object %q draws model %q; it cannot also draw mesh %q", id, obj.Model, mesh)
	}
	if mesh != "" && obj.GridMap != "" {
		return fmt.Errorf("editor: object %q draws gridMap %q; it cannot also draw mesh %q", id, obj.GridMap, mesh)
	}
	if mesh == "" && obj.Model == "" && obj.Occluder != nil && obj.Occluder.Mode == sceneformat.OccluderModel {
		return fmt.Errorf("editor: object %q has a model occluder; clear or switch occluder to box before removing mesh", id)
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
	// prevVersion is the scene's version before the placement, which raises a version 1 scene to sceneformat.CurrentVersion when obj instances another scene or draws a glTF model.
	prevVersion int
}

func (c *placeObjectCmd) Label() string { return "Place object " + c.obj.ID }

func (c *placeObjectCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	c.wasNilSlice = c.ed.Doc.Layers[li].Objects == nil
	if c.ed.Doc.Version < sceneformat.CurrentVersion && (instancesScene(&c.obj) || drawsModel(&c.obj) || hasOccluder(&c.obj)) {
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

// drawsModel reports whether o or any of its descendants names a glTF model or GridMap, which needs a version 2 scene.
func drawsModel(o *sceneformat.ObjectDoc) bool {
	found := false
	sceneformat.WalkObjects([]sceneformat.ObjectDoc{*o}, func(c, _ *sceneformat.ObjectDoc) bool {
		found = c.Model != "" || c.GridMap != ""
		return !found
	})
	return found
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
	ed        *Editor
	layerName string
	// parent is the ID of the object whose child was deleted, or empty for a top-level object.
	parent       string
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
	slot := siblingsAt(c.ed.Doc, c.layerName, c.parent)
	if slot == nil {
		return
	}
	objs := *slot
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
	if len(objs) == 0 && c.parent != "" {
		objs = nil
	}
	*slot = objs
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
	slot := siblingsAt(c.ed.Doc, c.layerName, c.parent)
	if slot == nil {
		return
	}
	objs := *slot
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
	*slot = objs
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
	if dst.Mesh == "" && dst.Model == "" && dst.GridMap == "" && pf.Mesh != "" {
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
	return sceneformat.CheckObjectModel(o)
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
	case []string:
		return slices.Clone(x)
	case [][2]float64:
		return slices.Clone(x)
	default:
		return v
	}
}

func isFiniteFloat32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
