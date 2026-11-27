package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/animclip"
	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/editor/internal/profiler"
	"github.com/nsgo-dev/game/pkg/editor/keychain"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind3d/navbake3d"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/scenemodel"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/game/pkg/tooling/library"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
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
	// ToolTerrain paints or erases autotiled terrain sets from a layered grid's YAML tileset, updating each cell and its neighbors through the same rules as tilemap.Tilemap.SetAutotile.
	ToolTerrain
	// ToolRectTile fills or erases a dragged rectangle of tiles on the active layer's inline tilemap.
	ToolRectTile
	// ToolLineTile draws or erases a Bresenham line of tiles between two cells.
	ToolLineTile
	// ToolStampTile paints a rectangular TileStamp pattern (selected from the palette or copied/cut from the map).
	ToolStampTile
	// ToolRandomTile paints a deterministic weighted random pick from selected tiles.
	ToolRandomTile
	// ToolNavTestPath sets start and end world points on click to preview the path pathfind.NavMesh.FindPath finds on the built 2D navigation mesh.
	ToolNavTestPath
	// ToolRotate rotates the selected object in 2D (and sets 3D Gizmo3DRotate).
	ToolRotate
	// ToolScale scales the selected object in 2D (and sets 3D Gizmo3DScale).
	ToolScale
	// ToolPan pans the viewport camera on drag.
	ToolPan
)

// String returns a human-readable label for the tool.
func (t Tool) String() string {
	switch t {
	case ToolSelect:
		return "Select"
	case ToolMove:
		return "Move"
	case ToolRotate:
		return "Rotate"
	case ToolScale:
		return "Scale"
	case ToolPan:
		return "Pan"
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
	case ToolTerrain:
		return "Terrain"
	case ToolRectTile:
		return "Rect Tile"
	case ToolLineTile:
		return "Line Tile"
	case ToolStampTile:
		return "Stamp Tile"
	case ToolRandomTile:
		return "Random Tile"
	case ToolNavTestPath:
		return "Test Path"
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
	// Gizmo3DCollider shows draggable size handles on the selected object's 3D collider or trigger.
	Gizmo3DCollider
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
	case Gizmo3DCollider:
		return "Collider"
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

// GizmoAxis3D identifies an axis, plane, or center handle on a 3D transform gizmo.
type GizmoAxis3D int

const (
	Axis3DNone GizmoAxis3D = iota
	Axis3DX
	Axis3DY
	Axis3DZ
	Axis3DXY
	Axis3DXZ
	Axis3DYZ
	Axis3DAll
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
	case Axis3DXY:
		return "XY"
	case Axis3DXZ:
		return "XZ"
	case Axis3DYZ:
		return "YZ"
	case Axis3DAll:
		return "All"
	default:
		return "None"
	}
}

// GizmoSpace3D selects whether the 3D transform gizmo aligns with world axes (Global)
// or the selected object's world orientation (Local).
type GizmoSpace3D int

const (
	// GizmoSpaceGlobal aligns 3D transform gizmo axes with world X, Y, Z.
	GizmoSpaceGlobal GizmoSpace3D = iota
	// GizmoSpaceLocal aligns 3D transform gizmo axes with the selected object's orientation.
	GizmoSpaceLocal
)

// String returns "Global" or "Local".
func (s GizmoSpace3D) String() string {
	switch s {
	case GizmoSpaceLocal:
		return "Local"
	default:
		return "Global"
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
	GizmoSpace3D  GizmoSpace3D
	Camera3D      OrbitCamera3D
	ActiveTileID  int
	// ActiveTileLayer names the tile layer of a layered grid (sceneformat.TilemapDoc.TileLayers) that PaintTile, EraseTile and FloodFillTile change, empty for its first. A single grid has no tile layers and ignores it.
	ActiveTileLayer string
	// ActiveTileFlip holds the flip bits (tilemap.FlipHorizontal, FlipVertical and FlipDiagonal) that the Paint and Fill tools place tiles of a layered grid with.
	ActiveTileFlip uint32
	// ActiveTerrain names the terrain set of a layered grid's YAML tileset that ToolTerrain paints and erases, empty for its first.
	ActiveTerrain string
	SelectedAsset *AssetEntry
	// SelectedPrefab names the registered prefab selected in the asset browser for the Place tool, or empty when none is selected.
	SelectedPrefab string
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
	// SettingsPath is the per-user editor settings YAML file path outside the project (set by LoadSettings or LoadUserSettings). When non-empty, SetLanguage and SetAppearance persist changes to it.
	SettingsPath string

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
	// OnPlayReset, when set, is called when Play mode starts or resets for replay playback with the recording's seed so game state resets deterministically.
	OnPlayReset func(seed uint64) error
	// PlayInputSource supplies live action/axis input sampled into the 60-second rolling replay buffer during Play mode.
	PlayInputSource replay.ActionSource
	// PlayInputActions lists the watched action names recorded in Play mode's rolling replay buffer. When empty and Project defines input actions, those action names are used in sorted order.
	PlayInputActions []string
	// PlayInputAxes lists the watched axis names recorded in Play mode's rolling replay buffer.
	PlayInputAxes []string
	// PlaySeed is the random seed written into Play mode's rolling replay recordings.
	PlaySeed uint64
	// ReplayDir optionally overrides the root directory under which replays/<scene>-<frame>.replay.yaml files are written when no project is open.
	ReplayDir string
	// OnPlayMarksThread, when set, is called when unsent Play mode marks are dispatched as one thread (via SendPlayMarks or automatically when Play stops).
	OnPlayMarksThread func(PlayMarksThread)
	// OnAskFrozenFrame, when set, is called when the author submits a prompt from Play mode's "Ask about frame N" dialog.
	OnAskFrozenFrame func(AskFrozenFrameRequest)
	// OnAskPanicFix, when set, is called when the author clicks "Ask the agent to fix" on a panic card in the Output panel.
	OnAskPanicFix func(PanicReport)
	// ShowPhysicsDebug controls whether 2D and 3D physics collider and trigger outlines draw in the viewport. New initializes it to true.
	ShowPhysicsDebug bool
	// ShowLightingPreview controls whether 2D lights, occluder shadows, and ambient darkness draw live in the 2D edit viewport. New initializes it to true.
	ShowLightingPreview bool
	// ShowNavMesh2D controls whether the built 2D navigation mesh overlay and authored navregion2d outlines draw in the 2D edit viewport. New initializes it to true.
	ShowNavMesh2D bool
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
	// ShowAllShapes controls whether the 2D viewport draws component shape fields of every visible object (true) or only the selected object (false).
	ShowAllShapes bool
	// SnapToGrid snaps object placement, drags, 2D handles, and 3D translate gizmos to EffectiveGridStep increments when true.
	SnapToGrid bool
	// GridSize is an explicit grid spacing in world/layer units used when SnapToGrid is true; 0 follows the active layer's tilemap tile size (or 8 when it has none).
	GridSize float32
	// SnapToPixel snaps positions to whole points (1-unit increments) when SnapToGrid is false.
	SnapToPixel bool
	// SnapRotation snaps 2D and 3D rotation drags to RotationSnapDeg-degree increments.
	SnapRotation bool
	// RotationSnapDeg is the rotation snap step in degrees. New initializes it to 15.
	RotationSnapDeg float32
	// SnapScale snaps 2D and 3D scale drags to ScaleSnapStep increments.
	SnapScale bool
	// ScaleSnapStep is the scale snap increment. New initializes it to 0.25.
	ScaleSnapStep float32
	// ShowGrid draws an optional 2D grid overlay at the active snap step in the 2D edit viewport.
	ShowGrid bool
	// PlayRate is the fixed update rate in Hz during Play mode and clip playback (defaults to 60 when <= 0).
	PlayRate int
	// OnSaveClip is invoked by the animation panel's Save button with the encoded clip YAML; when nil the button writes to ClipPath.
	OnSaveClip func(data []byte) error
	// OnSaveStates is invoked by the state machine panel's Save button with the encoded *.states.yaml bytes; when nil the button writes to StatesPath.
	OnSaveStates func(data []byte) error
	// OnSaveCutscene is invoked by the cutscene timeline panel's Save button with the encoded *.cutscene.yaml bytes; when nil the button writes to CutscenePath.
	OnSaveCutscene func(data []byte) error
	// CutsceneCatalog optionally provides localized strings for cutscene dialogue previews when no localization files are loaded from the project.
	CutsceneCatalog *l10n.Catalog
	// Clips optionally resolves animation clips for the open sprite state machine when no project directory holds them on disk.
	Clips animclip.ClipResolver
	// OnSaveDataTable is invoked by the Data Table panel's Save button with the encoded table YAML; when nil the button writes to DataTablePath.
	OnSaveDataTable func(data []byte) error
	// OnSaveGridMap is invoked by SaveGridMap (and Save when a GridMap has unsaved edits) with the encoded canonical .gridmap.yaml bytes; when nil SaveGridMap writes to the GridMap's file path.
	OnSaveGridMap func(data []byte) error
	// GridMapFS optionally provides a filesystem for loading GridMap files and their glTF mesh libraries when no project is open on disk.
	GridMapFS fs.FS
	// OnSaveTileset is invoked by the Tileset Editor panel's Save button with the encoded tileset YAML; when nil the button writes to TilesetPath.
	OnSaveTileset func(data []byte) error
	// OnSaveParticleEmitter is invoked by the Particle Emitter panel's Save button with the encoded emitter YAML; when nil the button writes to ParticleEmitterPath.
	OnSaveParticleEmitter func(data []byte) error
	// OnSaveMaterial is invoked by the Material Editor panel's Save button with the encoded material YAML (resource.Encode); when nil the button writes to MaterialPath.
	OnSaveMaterial func(data []byte) error
	// OnSaveRooms is invoked by SaveRooms / Save with the encoded *.rooms.yaml bytes; when nil SaveRooms writes to RoomsPath.
	OnSaveRooms func(data []byte) error
	// OnSaveRig is invoked by the Rig Editor panel's Save Rig button with the encoded .skel.yaml bytes; when nil the button writes to RigPath.
	OnSaveRig func(data []byte) error
	// OnAskAgentUpdateRef, when set, is called when the author clicks "Ask the agent to update it" for a non-rewritten reference (such as a Go string constant) in the References / Safe Rename review.
	OnAskAgentUpdateRef func(req SafeRenameAgentRequest) error
	// OnOpenSourceFile, when set, is called when the author clicks "Open <file>" for a non-scene source file (such as a Go file) in the References screen.
	OnOpenSourceFile func(file string, line int) error
	// ExportCommandRunner runs `go` with args for the Export panel's release builds, streaming output to stdout and stderr; when nil the editor invokes `go` via os/exec.
	ExportCommandRunner func(ctx context.Context, args []string, stdout, stderr io.Writer) error
	// ReleaseCheckRunner overrides `nsgo check` for the Build and Release screen's preflight and Check project step; when nil, runProjectCheck runs over the project directory.
	ReleaseCheckRunner func(ctx context.Context, dir string) ([]assetcheck.Problem, time.Duration, error)
	// ReleaseStepRunner optionally overrides execution of each Build and Release step (Check project, Compile Go, Pack assets, Sign, Smoke playtest, Measure).
	ReleaseStepRunner func(ctx context.Context, req ReleaseStepRequest, stdout, stderr io.Writer) error
	// ReleasePlaytestRunner overrides the Smoke playtest step of a Build and Release run; when nil, a default summary is produced.
	ReleasePlaytestRunner func(ctx context.Context, platform string, dir string) (BuildSmokePlaytestResult, error)
	// ReleaseMeasureRunner overrides the Measure step of a Build and Release run; when nil, artifact size and category breakdown are measured from disk and compared against the last release baseline.
	ReleaseMeasureRunner func(ctx context.Context, platform string, plan *releaseplan.Plan) (BuildArtifactResult, error)
	// ReleaseReadinessInspector overrides platform readiness evaluation on the Build and Release screen; when nil, built-in readiness checks (including iOS bundle id, icon, certificate, provisioning profile, privacy manifest, and touch controls) are used.
	ReleaseReadinessInspector func(platform string, dir string, f *project.File) []BuildReadinessItem
	// OnReleaseReadinessPersonAction is called when the author clicks a readiness action that requires the person (such as signing in to Apple for a provisioning profile).
	OnReleaseReadinessPersonAction func(platform string, item BuildReadinessItem) error
	// OnReviewReleaseUploads is called when the author presses "Review uploads..." on the Build and Release screen; nothing is uploaded automatically.
	OnReviewReleaseUploads func(snapshot BuildReleaseSnapshot) error
	// ReleaseNotesDrafter generates player-facing Markdown release notes from commits since the last release,
	// kept reviews, and changelog fragments; when nil, DefaultReleaseNotesDraft is used.
	ReleaseNotesDrafter func(ctx context.Context, req ReleaseNotesDraftRequest) (ReleaseNotesDraft, error)
	// ReleaseUploader uploads a single user-confirmed artifact to a store destination (itch.io, Google Play internal testing, or TestFlight).
	// When nil, the editor invokes `go tool nsgo publish` in the project directory (or `nsgo publish` on PATH when the project has no go.mod nsgo tool),
	// passing the keychain credential solely via NSGo's environment variables. It is never invoked until the person confirms in Review uploads, whatever the permission profile.
	ReleaseUploader ReleaseUploader
	// Keychain stores and retrieves store upload credentials and built-in agent API keys outside project files; when nil, keychain.Default() is used.
	Keychain keychain.Store
	// SyncMCPConfig, when set (automatically wired by editormcp.New), writes configured external MCP servers into the open project's agent MCP configuration (.mcp.json and .codex/config.toml).
	SyncMCPConfig func(root string, servers []AgentMCPServer, removed []string) error
	// OpenOutputFolder opens the release output directory in the OS file manager after a successful non-dry-run build; when nil the editor uses the host platform's default opener.
	OpenOutputFolder func(dir string) error
	// OpenBrowserURL opens a local/LAN web build URL in this computer's browser from the Build and Release screen's RUN ON DEVICE list; when nil the editor uses the host platform's default browser opener.
	OpenBrowserURL func(rawURL string) error
	// LANAddressResolver returns the local/LAN IPv4 address the web build test server binds to and advertises in the QR code; when nil the editor picks a private RFC 1918 LAN interface address (falling back to 127.0.0.1). Non-LAN public IP addresses are always refused.
	LANAddressResolver func() (string, error)
	// WebBuildServePort optionally pins the TCP port used by StartWebBuildServer; 0 (the default) allocates an ephemeral free port on the LAN interface.
	WebBuildServePort int
	// DeviceDiscoverer probes connected USB devices (such as Android phones over adb) for the Build and Release screen's RUN ON DEVICE list; when nil the editor queries `adb devices -l` on desktop.
	DeviceDiscoverer func(ctx context.Context, dir string) ([]ConnectedDevice, error)
	// DeviceCommandRunner executes `go tool nsgo build -target ... -run -device ...` when launching a finished Android build on a connected USB device; when nil the editor invokes `go` via os/exec on desktop.
	DeviceCommandRunner func(ctx context.Context, req DeviceCommandRequest, stdout, stderr io.Writer) error
	// WebBridgeHandler optionally handles JSON-RPC 2.0 requests received on `/__nsgo_mcp/rpc` from a served web build in Play mode; when nil, built-in bridge discovery/status methods are served.
	WebBridgeHandler func(ctx context.Context, method string, params json.RawMessage) (any, error)
	// RunGameCommand starts the child game process for RunGame / RunGameWith, streaming output to stdout and stderr; when nil the editor launches `go tool game dev` in the open project directory in its own process group on desktop.
	RunGameCommand func(spec RunGameSpec, stdout, stderr io.Writer) (GameProcess, error)
	// GoToolchainProbe optionally overrides Go toolchain discovery for Start Screen and Play/Create gating (used in tests).
	GoToolchainProbe func(minVersion string) GoToolchainStatus
	// RemoteTransport, when set, supplies the MCP client transport used to connect to a running game launched with RunGameOptions.Automation; when nil the editor spawns `go tool nsgo mcp` in the project directory on desktop.
	RemoteTransport func(ctx context.Context, spec RunGameSpec) (mcp.Transport, error)
	// RemoteRefreshInterval controls how often the remote scene tree and inspector poll the running game while unpaused (defaults to 250ms when <= 0).
	RemoteRefreshInterval time.Duration
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
	// LayoutPath is the per-user YAML file where the editor's dock panel layout is stored outside any project directory. When empty, layout edits stay in memory without writing to disk.
	LayoutPath string
	// Now returns the current time for recent-project timestamps and relative-time labels; when nil, time.Now is used.
	Now func() time.Time
	// PickProjectFolder prompts the user to choose a project folder (for Open Project and New Project's parent folder). When nil and a UI scope is attached, the editor invokes cx.PickDirectories.
	PickProjectFolder func(title, startDir string, done func(dir string, ok bool, err error))
	// CreateProject runs project creation from a template (such as `game new`), streaming its output to stdout and stderr. When nil on desktop, the editor invokes `go tool game new`.
	CreateProject func(ctx context.Context, req CreateProjectRequest, stdout, stderr io.Writer) error
	// OnOpenDemo overrides the Start Screen's Open Demo action when non-nil; when nil, OpenDemo loads the built-in Demo Room.
	OnOpenDemo func() error
	// OnSaveScreen, when set, is called with the screen path and canonical *.screen.yaml bytes whenever a HUD layout edit writes or reverts a screen.
	OnSaveScreen func(path string, data []byte) error
	// OnHUDPrompt, when set, is called when the author submits a prompt from the HUD layout screen's composer or "Ask the agent to fix N" button.
	OnHUDPrompt func(HUDPromptRequest)
	// OnOpenFileAtLine, when set, is called by OpenSelectedObjectYAML with the file path and 1-based line number of the selected object in the scene YAML.
	OnOpenFileAtLine func(path string, line int)
	// OnComposerPrompt, when set, is called when the inspector (such as Ask to add) sends or stages a prompt for the agent composer.
	OnComposerPrompt func(prompt string)
	// GitRunner invokes git with args in dir for scene merge conflict inspection, commit, and abort; when nil, os/exec runs git.
	GitRunner func(dir string, args ...string) (string, error)
	// MergeSuggester generates the agent's suggested resolution and reason for each conflicted scene item; when nil, DefaultMergeSuggestion is used.
	MergeSuggester func(ctx context.Context, req MergeSuggestRequest) (MergeSuggestion, error)
	// MergeCheckRunner overrides `game check` during FinishMerge; when nil, runProjectCheck runs over the project directory.
	MergeCheckRunner func(dir, scenePath string, doc *sceneformat.SceneDoc) ([]assetcheck.Problem, error)
	// MergePlaytestRunner overrides the short playtest run during FinishMerge; when nil, a deterministic 10 s headless Build/Update/Physics step runs.
	MergePlaytestRunner func(dir, scenePath string, doc *sceneformat.SceneDoc) (MergePlaytestResult, error)
	// WriteClipboard, when set, overrides writing text to the system clipboard (used by Copy and Cut); when nil and a UI scope is attached, core.Scope.SetClipboard is used.
	WriteClipboard func(text string)
	// ReadClipboard, when set, overrides reading text from the system clipboard (used by Paste); when nil and a UI scope is attached, core.Scope.ReadClipboard is used.
	ReadClipboard func(done func(text string))
	// ReadClipboardImage, when set, overrides reading an image from the system clipboard (used by PasteAgentReferenceImage); when nil and a UI scope is attached, core.Scope.ReadClipboardImage is used.
	ReadClipboardImage func(done func(img core.ClipboardImage, ok bool, err error))
	// ReadClipboardFiles, when set, overrides reading copied file URLs from the system clipboard (used by PasteAgentReferenceImage); when nil and a UI scope is attached, core.Scope.ReadClipboardFiles is used.
	ReadClipboardFiles func(done func(uris []string))
	// PickAgentReferenceFiles, when set, overrides the native file picker opened by the Agent composer's Attach an image button.
	PickAgentReferenceFiles func(done func(uris []string))
	// DiskWatchInterval controls how often the editor polls watched open files (scene, clip, game.yaml) and the project tree for external changes while a UI scope is attached (defaults to 250ms when <= 0).
	DiskWatchInterval time.Duration
	// OnPlaytestPrompt, when set, is called when the author presses "Ask the agent to tune it" or clicks an actionable suggestion in the Playtest Report screen.
	OnPlaytestPrompt func(PlaytestPromptRequest)
	// OnOpenPlaytestConfig, when set, overrides the BOT PROFILE card's "Open" action for playtest.yaml; when nil, the editor opens an inline playtest.yaml preview drawer.
	OnOpenPlaytestConfig func(configPath string) error
	// OnPlayReplayStep runs on every fixed simulation step during Play mode when watching a saved replay recording (after player.Step() and before OnPlayUpdate).
	OnPlayReplayStep func(player *replay.Player, scene *sceneformat.BuiltScene, dt time.Duration)
	// OnAssetImport, when set, handles the Asset Drawer's Import action for folder instead of opening the native file picker.
	OnAssetImport func(folder string)
	// OnAssetGenerate, when set, handles the Asset Drawer's Generate action for folder.
	OnAssetGenerate func(folder string)
	// OnAssetReplaceMissing, when set, handles the selected missing asset's "Replace with a file" action.
	OnAssetReplaceMissing func(missingPath string)
	// OnAssetAskAgent, when set, handles "Ask the agent" actions from the Asset Drawer and selected-asset inspector.
	OnAssetAskAgent func(assetPath, prompt string)
	// OnAskAgent3D, when set, handles "Or ask for one" (3D MODELS strip) and "Ask to add a component…" (3D inspector) prompts.
	OnAskAgent3D func(req Viewport3DAgentRequest)
	// ChangesCheckRunner overrides `nsgo check` and `nsgo test` in the Changes panel; when nil, the editor invokes `go tool nsgo check` and `go tool nsgo test`.
	ChangesCheckRunner func(dir string) (ChangesCheckStatus, error)
	// CodeReviewCheckRunner overrides `nsgo check` and `nsgo test` for pending Go code review (receiving the built Go overlay path and reporting live test progress); when nil, the editor runs `go tool nsgo check` and `go tool nsgo test -json` with GOFLAGS=-overlay=... on desktop.
	CodeReviewCheckRunner func(ctx context.Context, req CodeReviewCheckRequest, onProgress func(CodeReviewCheckProgress)) (CodeReviewCheckResult, error)
	// OnAskCodeReviewFix, when set, is called when the author clicks "Ask the agent to fix" on a failed test card in Go code review.
	OnAskCodeReviewFix func(CodeReviewFixRequest)
	// ExternalEditorName optionally names the external IDE shown in Go code review (for example "GoLand"); when empty, a localized default label is shown.
	ExternalEditorName string
	// CommitMessageDrafter generates the commit summary and description for the Changes panel; when nil, DefaultCommitDraft is used.
	CommitMessageDrafter func(ctx context.Context, req CommitDraftRequest) (CommitDraft, error)
	// GHRunner invokes the GitHub CLI (`gh`) with args in dir for OpenPullRequest; when nil, os/exec runs `gh` when installed on PATH.
	GHRunner func(dir string, args ...string) (string, error)
	// ChangesClipboard overrides clipboard writes from CopyPRDescription; when nil, falls back to WriteClipboard or core.Scope.SetClipboard.
	ChangesClipboard func(text string)
	// OnAskAgent, when set, is called when the author triggers "Ask the agent about this" (AskAgentAboutObject / mod+l) on an object.
	OnAskAgent func(AgentPromptRequest)
	// OnStopAgent is invoked by Mod+. (even while a modal dialog is open, per Spec rule 04) to stop an active agent run without disturbing reviewable changes.
	OnStopAgent func()
	// OnStartAgentRequest, when set, is called when the author starts a new agent request via StartAgentRequest.
	OnStartAgentRequest func(prompt string)
	// OnAddToAgentRequest, when set, is called when the author adds follow-up instructions via "Add to this request" while an agent request is running.
	OnAddToAgentRequest func(text string)
	// AgentRunner runs a composer request against an external agent (such as Claude Code or Codex) and streams incremental steps; when nil on desktop, CLIAgentRunner is used.
	AgentRunner AgentRunner
	// AutomationDrafter optionally turns plain words from "+ New automation, in words" into a candidate AutomationRule; when nil, a built-in English/Chinese drafter is used.
	AutomationDrafter func(ctx context.Context, words string) (AutomationRule, error)
	// AgentTools lists and executes the editor's MCP tools for the built-in agent; editormcp.New wires this automatically.
	AgentTools AgentToolExecutor
	// AgentSecretStore optionally overrides secret storage for the built-in agent's API key; when nil and Keychain is nil, cx.Secrets() is used whenever a core.Scope is attached (backed by the OS keychain in production and nsgotest.Secrets in tests) so keys never touch project files.
	AgentSecretStore AgentSecretStore
	// BuiltInAgentEndpoint optionally overrides the HTTP endpoint used by the built-in agent (defaults to DefaultBuiltInAgentEndpoint). Only https:// URLs or loopback http:// URLs are permitted.
	BuiltInAgentEndpoint string
	// BuiltInHTTPClient optionally overrides the *http.Client used by the built-in agent.
	BuiltInHTTPClient *http.Client
	// AgentReconnectProbe checks whether an MCP agent session is reachable during a reconnect attempt; wired automatically by editormcp.Server and replaceable in tests.
	AgentReconnectProbe func(ctx context.Context) (AgentClientInfo, error)
	// AgentScheduleAfter schedules fn to run after d and returns a cancel function; when nil, time.AfterFunc posting onto the editor's UI scope is used. Tests replace this to verify the 1, 2, 4, 8, 16 s reconnect backoff without real sleeps.
	AgentScheduleAfter func(d time.Duration, fn func()) (cancel func())
	// OnFocusAgentRequest, when set, is called when the user clicks the "Waiting for you" status chip to focus the open request.
	OnFocusAgentRequest func(req AgentOpenRequest)
	// OnAgentTurn, when set, is called whenever the author sends a request from the ⌘L in-place composer or Agent panel.
	OnAgentTurn func(AgentTurn)
	// OnSendAgentRequest, when set, is called after SendAgentRequest / SendAgentRequestWith appends and persists a request on the active agent thread.
	OnSendAgentRequest func(thread *AgentThread, req AgentThreadRequest)
	// OnOpenHistoryStep, when set, is called when the author clicks "Open this step in History" on a folded request or step.
	OnOpenHistoryStep func(sel AgentHistorySelection)
	// OnAgentPointPicked, when set, is called when the user clicks a world coordinate in the viewport during Partial "Let me point".
	OnAgentPointPicked func(turnID, target string, worldX, worldY float32)
	// OnOpenAgentReview, when set, is called when the user opens a turn's Review from Partial, Lost, Stopped, or Mismatch cards.
	OnOpenAgentReview func(review AgentTurnReview)
	// OnSendAgentComposerRequest, when set, receives each request assembled from the agent composer's active chips (and nothing else) and returns the agent's reply and optional reported token usage.
	OnSendAgentComposerRequest func(AgentRequest) (AgentResponse, error)
	// OnCompactAgentThread, when set, overrides how CompactAgentThread summarizes earlier turns and updates token usage.
	OnCompactAgentThread func(earlier, kept []AgentThreadTurn) (summary string, usage *AgentTokenUsage, err error)
	// OnStartFreshAgentThread, when set, is called when StartFreshAgentThread starts a new thread seeded with summary.
	OnStartFreshAgentThread func(summary string) error
	// OnPlanDecision, when set, is called whenever the author approves, chooses Just do it on, or stops an agent-proposed plan.
	OnPlanDecision func(AgentPlanDecision)
	// ProblemCheckDebounce controls the debounce delay before live problem checks run after edits (defaults to 80ms when <= 0).
	ProblemCheckDebounce time.Duration
	// OnAskAboutProblem, when set, is called when the author submits a question from "Ask about this problem…" with the problem attached.
	OnAskAboutProblem func(ProblemQuestionRequest)
	// AutoSaveDelay configures the debounce delay after kept edits before automatic saving runs (defaults to DefaultAutoSaveDelay = 2s when <= 0).
	AutoSaveDelay time.Duration
	// SavingIndicatorDelay configures how long an automatic save must stay in flight before the status bar shows "Saving…" (defaults to DefaultSavingIndicatorDelay = 300ms when <= 0).
	SavingIndicatorDelay time.Duration
	// SaveFileWriter optionally overrides the atomic file writer used when saving scenes and project files (defaults to writeFileAtomic when nil), allowing deterministic slow-write or error tests.
	SaveFileWriter func(path string, data []byte) error
	// FreeSpaceBytes optionally overrides available-disk-space lookup for path when formatting disk-full save errors (for example "Couldn't save: disk is full · 12 MB free"). When nil, the OS filesystem free space is queried.
	FreeSpaceBytes func(path string) (uint64, bool)
	// AgentTaskRunner, when set, overrides how background agent tasks started by SubmitTask or the queue scheduler run. When nil, Editor runs each task through the configured agent route (CLI runner or built-in runner) as its own session and turn.
	AgentTaskRunner func(ctx context.Context, task AgentTask) error
	// OnTaskPermissionDecision, when set, is called when the user clicks Allow once or Deny on a TaskNeedsYou task.
	OnTaskPermissionDecision func(taskID string, allow bool)
	// ThreadsDir optionally overrides where *.thread.yaml background task files are persisted; when empty, <projectDir>/.nsgo/threads is used while a project is open.
	ThreadsDir string
	// OnSettingsAgentRequest, when set, is called when the author clicks a suggested ask or submits an agent prompt on a Settings page or in the Settings search bar.
	OnSettingsAgentRequest func(SettingsAgentRequest)
	// OnSaveBrief, when set, overrides writing brief.yaml on disk when the game brief is edited or saved.
	OnSaveBrief func(data []byte) error
	// OnSaveProjectRules, when set, overrides writing AGENTS.md on disk when project rules are edited or saved.
	OnSaveProjectRules func(data []byte) error
	// FirstRunDetectAgents overrides local agent discovery (claude, codex, API key) for the Start with AI Connect step.
	FirstRunDetectAgents func() []FirstRunAgentOption
	// FirstRunConnectAgent overrides the agent handshake during Start with AI Connect (returning connected tool count or an error/timeout).
	FirstRunConnectAgent func(ctx context.Context, kind string, projectDisplayPath string) (tools int, err error)
	// FirstRunValidateAPIKey overrides immediate validation of a pasted API key in Start with AI Connect.
	FirstRunValidateAPIKey func(ctx context.Context, key string) FirstRunAPIKeyStatus
	// FirstRunPlanner overrides generating the first plan from the author's idea and selected project name.
	FirstRunPlanner func(ctx context.Context, req FirstRunPlanRequest) (*FirstRunPlan, error)
	// FirstRunBuilder overrides executing the approved first-run plan after the project folder is created.
	FirstRunBuilder func(ctx context.Context, req FirstRunBuildRequest, emit func(FirstRunBuildEvent)) (*FirstRunBuildResult, error)
	// FirstRunPlaytest overrides running the headless playtest stage during Start with AI Building when the project has playtest.yaml.
	FirstRunPlaytest func(ctx context.Context, projectDir string) (*playtest.Report, error)
	// FirstRunConnectTimeout bounds the Start with AI agent handshake before showing the no-answer checklist (defaults to 30s when <= 0).
	FirstRunConnectTimeout time.Duration
	// OnFocusWindow, when set, is called when clicking a system notification brings the editor window to the front.
	OnFocusWindow func()
	// OnOpenAgentTask, when set, is called after OpenAgentTask opens a task's scene and switches to the Tasks or Agent panel.
	OnOpenAgentTask func(task AgentTask)
	// OnSendSketch, when set, is called when the author sends a viewport sketch to the agent (SendSketch).
	OnSendSketch func(SketchAgentRequest)
	// OnReplySketchReading, when set, is called when the author answers an agent sketch reading card (Go ahead, Correct a mark, or a text reply).
	OnReplySketchReading func(SketchReadingReply)
	// CheckpointMaxBytes caps the total disk size under .nsgo/checkpoints/ before the oldest checkpoints are pruned (defaults to DefaultCheckpointMaxBytes when <= 0).
	CheckpointMaxBytes int64

	playing            bool
	playPaused         bool
	playTimeScale      float64
	playLoop           *game.Loop
	playSnapshot       []byte
	playStop           func()
	playScene          *sceneformat.BuiltScene
	playPhysics        *sceneformat.PhysicsScene
	playTriggerEvents  []sceneformat.PhysicsTriggerEvent
	playSteps          int
	playFrames         int
	playReplay         *replay.Recording
	playReplayPlayer   *replay.Player
	playReplayPath     string
	playReplayRunIndex int
	playRolling        *playRollingRecorder
	playMarks          playMarksState
	physicsLayers      *sceneformat.PhysicsLayerTable

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
	clipLoading bool
	clipErr     error
	selKey      clipKeyRef

	// The sprite state machine editor: open *.states.yaml graph, live preview player and Drive with keys state.
	states statesEditorState

	// The cutscene timeline editor: open *.cutscene.yaml timeline, live preview player, thumbnails and agent review state.
	cutscene cutsceneEditorState

	// The data table editor: the open table state, active agent review, and registered Go row types per file pattern.
	table       *dataTableState
	tableReview *dataTableReviewState
	tableTypes  *datatable.Registry

	// The tileset editor: the open *.tileset.yaml document, selected terrain/tile, collision/animation/data state, and viewport tile animation loop.
	tileset              *tilesetEditorState
	tileAnimStop         func()
	tileAnimRev          uint64
	tileAnimListeners    map[uint64]func()
	nextTileAnimListener uint64

	// The particle emitter editor: the open emitter config, live preview emitter, and edit-viewport scene emitter loop.
	particles        *particleEditorState
	sceneEmitterStop func()
	// rig holds the open 2D skeletal rig edited by the Rig Editor panel, or nil when closed.
	rig *rigState
	// rigStepRev records the revision emitted by a pure StepRig playback tick so unsavedMemo skips full YAML re-encoding at 60 Hz.
	rigStepRev uint64

	// The material editor: the open *.material.yaml document, live preview state, and live reload revision.
	material    *materialEditorState
	materialRev uint64
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
	fly3DOrtho       bool
	fly3DMoved       bool
	agentPrompt3D    viewport3DAgentPromptState
	lastAgentReq3D   *Viewport3DAgentRequest
	modelStats3D     modelStatsCache3D
	cameraDrag       cameraDragState
	cameraBoundsDrag cameraBoundsDragState
	rooms            *roomMapState
	roomDrag         roomDragState
	bgPreview        bgPreviewState
	scope            *core.Scope
	revision         uint64
	nextID           int
	nextListener     uint64
	listeners        map[uint64]func()
	// askUnsaved asks the author, through the prompt EditorView shows, whether to save or discard unsaved edits before something loses them; nil without an EditorView.
	askUnsaved func(proceed, cancel func())
	// askCloseTab asks the author whether to save, discard or cancel closing a dirty scene tab; nil without an EditorView.
	askCloseTab func(tabID uint64, label string)
	// tabs holds all open scene tabs; tabs[activeTab] mirrors the active scene fields on Editor.
	tabs      []*sceneTab
	activeTab int
	tabSeq    uint64
	// scenesRev bumps whenever any scene tab is saved so 3D views of tabs that instance or inherit it rebuild.
	scenesRev uint64
	// fetch is the library panel's fetch of a pack from a remote library under way, or nil.
	fetch *packFetch
	// hidden and locked are the objects the scene tree hides in the viewports or locks against picking (SetObjectHidden, SetObjectLocked); they belong to the session, not the scene file.
	hidden map[string]bool
	locked map[string]bool
	// layerLocked are the layers the layers panel locks against the viewports (SetLayerLocked); like hidden and locked, they belong to the session.
	layerLocked map[string]bool
	// baseLayers caches the names of the layers of the scene the open scene inherits (inheritedLayers).
	baseLayers baseLayerCache
	// baseScenes caches resolved source scenes for override inspection and editing.
	baseScenes baseSceneCache
	// instanceCounts caches per-file scene instance counts across the open project so CountSceneInstances avoids disk walks on UI ticks.
	instanceCounts instanceCountCache
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
	// buildRelease holds the full-window Build and Release screen state, preflight checks, preset build steps, logs, failures, results, and readiness checks.
	buildRelease buildReleaseState
	// agentActorDepth tracks active agent calls (via WithAgentActor / editormcp) to enforce strict human-only boundaries such as store uploads.
	agentActorDepth atomic.Int32
	// search holds the cached project search index, active search results and Find References state.
	search projectSearchState
	// assetDrawer holds the Assets rail/drawer state, filter/sort/selection, preview playback, and drag-to-viewport state.
	assetDrawer assetDrawerState
	// view2D is the 2D viewport's own view while view2DFree is set, after the author panned or zoomed; until then the view is the scene camera's (View2D).
	view2D     View2D
	view2DFree bool
	// canvasW and canvasH are the viewport canvas's laid-out size (SetCanvasSize), zero until it lays out.
	canvasW, canvasH float32
	// output holds the docked Output / Log panel history and filter state.
	output outputPanelState
	// profiler holds the live remote/play-mode performance telemetry, recorded frame timeline, zones table, and panel visibility.
	profiler *profiler.State
	// gameRun tracks the live `go tool game dev` child process started by RunGame / RunGameWith.
	gameRun gameRunState
	// goToolchain caches Go toolchain discovery and version check results for the Start Screen and Play/Create actions.
	goToolchain goToolchainState
	// selectedIDs holds the ordered set of selected object IDs; SelectedID is the primary object within it.
	selectedIDs []string
	// drag2D is the 2D viewport's drag of an object or a gizmo handle in progress (BeginDrag2D).
	drag2D drag2DState
	// marquee is the 2D viewport's marquee box selection drag in progress (BeginMarquee2D).
	marquee marqueeState
	// shapeDrag is the 2D viewport's drag of a component shape handle in progress (BeginShapeDrag2D).
	shapeDrag shapeDragState
	// pluginDrag tracks a 2D viewport drag claimed by the active plugin tool (BeginPluginDrag2D).
	pluginDrag        bool
	pluginDragMoved   bool
	pluginDragRef     pluginToolRef
	pluginDragLast    math2d.Vec2
	pluginHoverActive bool
	pluginHoverRef    pluginToolRef
	pluginHoverPos    math2d.Vec2
	// pluginDrag3D tracks a 3D viewport drag claimed by the active plugin tool (BeginPluginDrag3D).
	pluginDrag3D        bool
	pluginDrag3DMoved   bool
	pluginDrag3DRef     pluginToolRef
	pluginDrag3DLast    math2d.Vec2
	pluginDrag3DW       float32
	pluginDrag3DH       float32
	pluginHover3DActive bool
	pluginHover3DRef    pluginToolRef
	pluginHover3DRay    math3d.Ray
	// compGizmoDrag tracks an interactive 3D component gizmo handle drag (BeginComponentGizmoDrag3D).
	compGizmoDrag componentGizmoDrag3DState
	// stroke is the tile stroke under way (BeginTileStroke), or nil.
	stroke *tileStroke
	// hover is where the mouse rests over the viewport (SetTileHover).
	hover tileHover
	// tileSel is the selected rectangular region on the active layer's inline tilemap, for Copy/Cut.
	tileSel    TileRect
	hasTileSel bool
	// tileClipboard is the last region copied or cut from a tilemap (CopyTileRegion, CutTileRegion).
	tileClipboard TileStamp
	// activeStamp is the rectangular pattern painted by ToolStampTile (SetTileStamp, SelectPaletteStamp, CopyTileRegion).
	activeStamp TileStamp
	// paletteStampAnchor and paletteStampRect track the rectangular selection made inside TilePaletteView.
	paletteStampAnchor  int
	paletteStampRect    TileRect
	hasPaletteStampRect bool
	// randomTiles and randomSeed configure ToolRandomTile's deterministic weighted picks.
	randomTiles []WeightedTile
	randomSeed  uint64
	// tileReview holds the active region-based agent tile edit review for the open scene, or nil.
	tileReview    *tileReviewState
	tileReviewSeq uint64
	// sketch holds the session-only viewport sketch layer, confirmation hold, reading card, and post-sketch review state.
	sketch sketchState
	// paletteImgCache caches FileResolver-loaded tileset images across TilePalette() calls so 60 Hz rev bumps avoid disk reads.
	paletteImgCache paletteFileImageCache
	// inspectAssetFile prefers the selected asset's import settings in InspectorView over SelectedID after selecting an asset in FilesView.
	inspectAssetFile bool
	// inspectLayer prefers ActiveLayer's parallax/repeat settings in InspectorView over Scene Settings when no object or asset file is selected (set by SetActiveLayer, cleared by SelectObject or opening a scene).
	inspectLayer bool
	// importRev bumps whenever an asset's *.import.yaml sidecar is written, removed, undone, or redone so InspectorView resyncs text drafts.
	importRev uint64
	// importPreview caches the selected image asset for the slice-grid & pivot preview canvas and tracks any active pivot drag.
	importPreview imageImportPreviewState
	// modelPreview caches the selected glTF/GLB model, its orbit camera, animation playback state, and software GPU preview renderer.
	modelPreview modelPreviewState
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
	// firstRun holds the Start with AI first-run flow state (Connect in place, First Plan, Building, First Review, and tips).
	firstRun firstRunState
	// unsavedPrompt tracks a pending project switch waiting for user confirmation of unsaved edits.
	unsavedPrompt unsavedPromptState
	// navMesh3D holds the 3D navigation mesh bake settings, panel visibility, and cached preview mesh.
	navMesh3D navMesh3DState
	// nav2D holds the 2D navigation test-path preview and cached portal segments for the built 2D NavMesh overlay.
	nav2D nav2DState
	// vp3d holds cached 3D render3d.Queue/OcclusionBuffer state, active LOD indices and RenderStats for the 3D viewport.
	vp3d viewport3DState
	// remote holds the live remote scene tree, inspector, MCP client session, loop controls, tunables, and console state.
	remote remoteState
	// terrain holds the open 3D terrain editing session (heightmap, layer weights, foliage density maps) and active brush state.
	terrain      *terrainState
	terrainBrush TerrainBrush
	// settings holds the per-user editor language, appearance, and shortcut override preferences and the unified Settings screen state.
	settingsMu            sync.RWMutex
	settings              EditorSettings
	settingsOpen          bool
	settingsPage          SettingsPage
	editorSettingsTab     EditorSettingsTab
	shortcutsSheetOpen    bool
	shortcutCaptureActive bool
	catalog               *l10n.Catalog
	// toasts holds the FIFO queue of transient Spec toast notifications shown over the viewport
	// (or bottom center of the window when no viewport is on screen).
	toasts        []*toastQueueEntry
	toastSeq      uint64
	toastNextInst uint64
	toastRT       *toastRuntime
	// overlayLegendHidden hides the compact Spec overlay legend in the viewport top-left corner when true (shown by default in edit mode).
	overlayLegendHidden bool
	// sceneOverlays holds active live Spec viewport overlays (Agent working, Added, Changed, Removed, Focused change).
	sceneOverlays []SceneOverlayEntry
	// overlayEpoch is the reference start time used for pulsing task overlays.
	overlayEpoch time.Time
	// overlayNowFn optionally overrides time.Now for deterministic overlay animation tests.
	overlayNowFn func() time.Time
	// reduceMotionOverride optionally overrides Scope.Env().ReduceMotion() for tests and headless editors.
	reduceMotionOverride *bool
	// agentLive tracks the pending agent turn, structural diff overlays, active touched objects, and header pill state.
	agentLive agentLiveState
	// agentMutating is > 0 while an agent-originated MCP tool executes through RunAsAgent.
	agentMutating int
	// hud holds the HUD and menu multi-size layout screen state, active *.screen.yaml document, locale tables, overlays, and touch drag session.
	hud hudLayoutState
	// colorVisionPreview selects the active color-vision deficiency simulation mode for the scene viewport and HUD layout preview.
	colorVisionPreview a11y.ColorVision
	// lookThroughCamera3D makes the 3D viewport render and project through the scene's active 3D camera (LookThroughCamera3D) instead of Camera3D.
	lookThroughCamera3D bool
	// envPanelOpen controls whether the Scene Environment panel is shown.
	envPanelOpen bool
	// merge holds the object-by-object Scene Merge Conflict resolution screen state.
	merge mergeConflictState
	// mergeDriver tracks whether the open Git repository needs one-click nsgo-scene merge driver setup.
	mergeDriver mergeDriverState
	// contextMenu holds the unified viewport/keyboard object context menu, inline rename request, toast, and last agent prompt draft.
	contextMenu objectContextMenuState
	// clipboardText holds an in-memory copy of the last scene YAML copied when no UI scope or clipboard hook is attached.
	clipboardText     string
	lastClipboardKind clipboardKind
	lastPasteYAML     string
	pasteStreak       int
	// pointer2D and pointer3D track the last hovered viewport canvas point for Paste at pointer.
	pointer2D    math2d.Vec2
	hasPointer2D bool
	pointer3D    [2]float32
	hasPointer3D bool
	// dock holds the editor's dockable panel layout blueprint, hidden panels, and mounted ui.DockArea ref.
	dock dockLayoutState
	// disk tracks on-disk snapshots, external-change conflicts, save-conflict prompts, and background file watching.
	disk diskWatchState
	// playtest holds the Playtest Report screen state, active report entries, chart view-model, and playtest_define proposal reviews.
	playtest playtestReportState
	// assetGen holds the MCP asset generator client sessions, in-memory candidate previews, style brief checks, and license status.
	assetGen assetGenState
	// snapInvert temporarily inverts snapping while a modifier key (Ctrl/Cmd) is held during a drag or press.
	snapInvert bool
	// snapSettingsPopoverOpen tracks whether the viewport Snap Settings popover is currently open.
	snapSettingsPopoverOpen bool
	// builtinOverlays and customOverlays hold the 2D viewport overlay definitions shown in the Overlays popover and editormcp's viewport_image.
	builtinOverlays     []ViewportOverlay
	customOverlays      []ViewportOverlay
	overlaysPopoverOpen bool
	drawOverlayOverride map[string]bool
	// usages holds the full-window Find Usages and Safe Rename review screen state.
	usages referencesScreenState
	// changes holds the Changes & Commit screen state, working-tree file diffing, and commit milestones.
	changes changesState
	// inspectorFilter narrows visible inspector sections and fields by case-insensitive substring match.
	inspectorFilter string
	// inspectorCollapsed tracks which titled inspector sections are collapsed by section ID.
	inspectorCollapsed map[string]bool
	// agentThreads holds per-line-of-work agent threads grouped by scene, thread switcher UI state, and History step selection.
	agentThreads agentThreadsState
	// inspectorScrub tracks an active live label-drag scrub in the inspector.
	inspectorScrub inspectorScrubState
	// composerDraft holds the current prompt text staged for the agent composer (e.g. via Ask to add).
	composerDraft string
	// inspectorReviewChanges holds active agent review field changes shown in the inspector.
	inspectorReviewChanges []InspectorReviewChange
	// inspectorFieldProblems holds explicit field-level problems shown as problem pills in the inspector.
	inspectorFieldProblems []InspectorFieldProblem
	// focus holds the editor's keyboard region controller, per-region focus memory, dialog focus trap state, and text-input typing state.
	focus focusRegionState
	// agent holds the Connect dialog, status chip, reconnect backoff, permission checkboxes, active thread, and composer state.
	agent agentState
	// automations holds the project's automations.yaml rules, pre-run checkpoints, weekly run counters, pause-for-today state, and background automation runs.
	automations automationRuntimeState
	// agentComposer holds the @-mention picker, context chips, hover preview, and context meter state.
	agentComposer agentComposerState
	// codeReview holds pending Go code changes, companion YAML changes, build-overlay check results, and per-file confirmation state.
	codeReview codeReviewState
	// autosave tracks debounced automatic saving of kept changes, the 300 ms Saving… threshold, and persistent status-bar save errors.
	autosave autoSaveState
	// perms holds the project's MCP permission profile/rules (permissions.yaml), Settings › Permissions state, blocked steps, pending asks, Always saved toast, You-only draft state, tool toggles, and held external loosenings.
	perms permissionsState
	// askInPlace holds the anchored ⌘L in-place composer state, contextual suggestions, and return focus.
	askInPlace askInPlaceState
	// tasks holds the background agent task list, queue/parallel scheduling, and thread persistence state.
	tasks agentTasksState
	// agentDocs holds the open project's brief.yaml, AGENTS.md, pending document reviews, review outcomes, and last Settings agent request.
	agentDocs agentDocsState
	// historyPanel holds the unified History timeline panel, point-in-time read-only preview, and agent-turn recording state.
	historyPanel historyPanelState
	// agentReview holds the Review changes panel state including proposed file deletions that Keep all never applies.
	agentReview agentReviewState
	// review holds the change-by-change agent turn review state, pending overlays, and right-rail Agent card state.
	review turnReviewState
	// checkpoints holds the open project's persistent .nsgo/checkpoints/ state and any active turn.
	checkpoints checkpointState
	// agentTurn holds the active, pending, stopped, kept, or reverted multi-file agent turn.
	agentTurn        *agentTurnState
	agentTurnSeq     uint64
	agentTurnRev     uint64
	lastRevertedTurn *agentTurnState
	// stagedFiles holds in-memory edits to non-active project files (other scenes, prefabs, data tables, clips)
	// touched by an agent turn, so nothing is written to disk until the turn is kept and saved.
	stagedFiles map[string]*stagedFileEntry
	// pendingTurnPrompt tracks a blocked close/scene-switch/project-switch waiting on Keep, Revert, or Cancel.
	pendingTurnPrompt pendingTurnPromptState
	// pointerRev and pointerListeners broadcast viewport pointer coordinate updates to the status bar without bumping full Revision.
	pointerRev          uint64
	nextPointerListener uint64
	pointerListeners    map[uint64]func()
	// canvasSizeRev and canvasSizeListeners broadcast viewport canvas layout resizes (for overlays such as the camera frame caption) without bumping full Revision.
	canvasSizeRev          uint64
	nextCanvasSizeListener uint64
	canvasSizeListeners    map[uint64]func()
	// projectLoading tracks an in-progress project open so every workspace region shows its designed loading state.
	projectLoading     bool
	projectLoadingPath string
	// sceneOpenErr records the last failed scene open (missing file, malformed YAML, etc.) so every workspace region shows its designed error state instead of a blank viewport.
	sceneOpenErr *SceneOpenError
	// layersPanelOpen, fileBrowserOpen, assetBrowserOpen, and libraryPanelOpen toggle auxiliary docked panels moved off the default AI-first chrome.
	layersPanelOpen  bool
	fileBrowserOpen  bool
	assetBrowserOpen bool
	libraryPanelOpen bool
	// sceneTreeFilter and sceneTreeFilterOpen hold the left-rail scene tree quick-filter state.
	sceneTreeFilter     string
	sceneTreeFilterOpen bool
	// paletteRecent holds the most recently executed Command Palette item IDs (newest first).
	paletteRecent []string
	// proposedNewObjects records object IDs proposed by a pending review for the left rail NEW badge.
	proposedNewObjects map[string]bool
	// plan holds the active agent-proposed plan, clarifying question answers, step edits, ghosts, and MCP waiters.
	plan agentPlanState
	// headless serializes all scope-less Editor callbacks and RunHeadless calls and drains on AttachScope.
	headless editorHeadless
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
	isPlaceholder := doc == nil
	if doc == nil {
		doc = DefaultSceneDoc()
	}
	if err := sceneformat.Validate(doc); err != nil {
		return nil, err
	}
	ed := &Editor{
		Doc:                 doc,
		Resolver:            resolver,
		Prefabs:             make(map[string]Prefab),
		Tool:                ToolSelect,
		ViewMode:            ViewMode2D,
		GizmoMode:           Gizmo3DTranslate,
		Camera3D:            DefaultOrbitCamera3D(),
		ActiveTileID:        1,
		randomSeed:          1,
		Status:              "Ready",
		ShowPhysicsDebug:    true,
		ShowLightingPreview: true,
		ShowNavMesh2D:       true,
		ShowNavMesh3D:       true,
		navMesh3D:           navMesh3DState{config: DefaultNavMeshBakeConfig()},
		ShowLODOverlay:      true,
		ShowOccluders:       true,
		ShowRenderStats:     true,
		RotationSnapDeg:     defaultRotationStepDeg,
		ScaleSnapStep:       defaultScaleStep,
		terrainBrush:        DefaultTerrainBrush(),
		selKey:              clipKeyRef{-1, -1},
		cutscene:            cutsceneEditorState{selTrack: -1, selClip: -1},
		listeners:           make(map[uint64]func()),
		output: outputPanelState{
			open:       false,
			lastStatus: "Ready",
		},
		settings:     DefaultEditorSettings(),
		overlayEpoch: time.Now(),
		perms: permissionsState{
			file:          DefaultPermissions(),
			initialized:   true,
			focusedRule:   -1,
			activeSection: "permissions",
		},
		catalog: defaultEditorCatalog,
		hud: hudLayoutState{
			overlays: DefaultHUDOverlays(),
		},
	}
	ed.profiler = profiler.New(ed.newProfilerHost())
	ed.agent.initDefaults()
	ed.automations.initDefaults()
	ed.History = ed.newTabHistory()
	if len(doc.Layers) > 0 {
		ed.ActiveLayer = doc.Layers[0].Name
	}
	ed.keepTileInPalette()
	// The scene the editor starts on is clean: an error here leaves it dirty, which only asks before losing it.
	ed.sceneSaved, _ = sceneformat.Encode(doc)
	ed.initBuiltinOverlays()
	ed.initBuiltinCommands()
	ed.initAgentComposer()
	ed.Rebuild()
	ed.resetTabsToActive(isPlaceholder)
	return ed, nil
}

// Revision returns a monotonically increasing counter bumped on every state change.
func (e *Editor) Revision() uint64 {
	return e.revision
}

// TileAnimRevision returns a monotonically increasing counter bumped on edit-mode tile animation ticks without bumping Revision.
func (e *Editor) TileAnimRevision() uint64 {
	if e == nil {
		return 0
	}
	return e.tileAnimRev
}

// SubscribeTileAnim registers fn to run on edit-mode tile animation ticks and returns an unsubscribe function.
func (e *Editor) SubscribeTileAnim(fn func()) func() {
	if e == nil || fn == nil {
		return func() {}
	}
	e.nextTileAnimListener++
	id := e.nextTileAnimListener
	if e.tileAnimListeners == nil {
		e.tileAnimListeners = make(map[uint64]func())
	}
	e.tileAnimListeners[id] = fn
	return func() {
		delete(e.tileAnimListeners, id)
	}
}

func (e *Editor) notifyTileAnim() {
	e.tileAnimRev++
	for _, fn := range e.tileAnimListeners {
		fn()
	}
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
// When attaching a non-nil scope, AttachScope first publishes cx and drains any in-flight headless work (forwarding queued headless callbacks to cx.Post in FIFO order) before touching UI-owned state.
func (e *Editor) AttachScope(cx *core.Scope) {
	if e == nil {
		return
	}
	if cx != nil {
		e.headless.attachScope(cx)
		e.applyScope(cx)
		return
	}
	e.headless.detachScope(func() {
		e.applyScope(nil)
	})
}

func (e *Editor) applyScope(cx *core.Scope) {
	e.scope = cx
	e.problems.setScope(cx)
	e.gameRun.setScope(cx)
	e.assetBuild.setScope(cx)
	e.profiler.SetScope(cx)
	if cx != nil && e.profiler.Connected() && !e.profiler.PlayActive() {
		e.profiler.StartPoller()
	} else if cx == nil {
		e.profiler.StopPoller()
	}
	e.startScreen.setScope(cx)
	e.firstRun.setScope(cx)
	e.remote.setScope(cx)
	e.settingsMu.Lock()
	e.agent.builtInKeyChecked = false
	e.settingsMu.Unlock()
	e.buildRelease.setScope(cx)
	e.tasks.setScope(cx)
	e.setDiskWatcherScope(cx)
	e.setAutoSaveScope(cx)
	if cx != nil {
		e.startParticlePreviewLoop()
		e.syncSceneEmitterLoop()
		e.syncTileAnimLoop()
		e.startAutomationScheduler()
		e.syncAgentPulseLoop()
		cx.SetAppearance(e.Appearance().CoreAppearance())
	} else {
		e.stopAgentTimers()
		e.stopAllTaskRunners()
		e.stopParticlePreviewLoop()
		e.stopSceneEmitterLoop()
		e.stopModelPreviewLoop()
		e.stopTileAnimLoop()
		e.stopAutomationRunsAndScheduler()
		e.stopAgentPulseLoop()
	}
}

func (e *Editor) notify() {
	e.captureStatusForOutput()
	e.revision++
	for _, fn := range e.listeners {
		fn()
	}
}

func (e *Editor) afterMutate() {
	// Ensure SelectedRoomID still points to an existing room in the open room map.
	if e.rooms != nil && e.rooms.selectedRoomID != "" {
		if e.rooms.doc == nil || e.rooms.doc.Room(e.rooms.selectedRoomID) == nil {
			e.rooms.selectedRoomID = ""
		}
	}
	// Undoing a layer's addition, or redoing its removal, may take the active layer away.
	e.keepActiveLayer()
	// An undone or redone project edit may have turned off the plugin whose tool is active.
	e.dropStalePluginTool()
	e.dropStaleFocusedPluginPanel()
	e.syncAudioMixer()
	e.syncExportPanel()
	e.syncHUDAfterMutate()
	e.syncTileReviewAfterMutate()
	e.syncAgentTurnAfterMutate()
	e.syncReviewAfterMutate()
	e.Rebuild()
	// Ensure SelectedID and selectedIDs still point to existing local, instanced, or inherited objects.
	e.pruneSelection()
	if e.contextMenu.open && e.contextMenu.targetID != "" {
		if obj, _ := e.FindResolvedObject(e.contextMenu.targetID); obj == nil {
			e.contextMenu.open = false
			e.contextMenu.targetID = ""
		}
	}
	if e.connPick != nil && e.SelectedID != e.connPick.sourceID {
		e.connPick = nil
	}
	if e.activeTab >= 0 && e.activeTab < len(e.tabs) {
		if e.History.Len() > 0 || e.SceneDirty() {
			e.tabs[e.activeTab].placeholder = false
		}
		e.syncActiveTab()
	}
	e.refreshOpenSceneSearch()
	e.scheduleLiveProblemCheck()
	e.scheduleAutoSaveIfNeeded()
	e.notify()
}

// Rebuild resolves e.Doc against e.Resolver into e.Built. The scene's 3D build (Scene3D) is brought up to date the next time the 3D viewport draws or picks; a 3D gizmo drag in progress changes only the dragged object's transform, which moves its built node in place.
func (e *Editor) Rebuild() {
	defer e.refreshClipPreview()
	defer e.refreshStatesPreview()
	defer e.refreshCutscenePreview()
	defer e.syncTileAnimLoop()
	e.syncRemoteDocObjectIDs()
	if e.drag3D.kind != drag3DGizmo {
		e.s3d.stale = true
	}
	if e.Doc == nil {
		e.Built = nil
		e.BuildErr = nil
		e.recomputeNavPath2D()
		return
	}
	e.syncProjectAssets()
	resolver := e.liveResolver()
	built, err := sceneformat.Build(e.Doc, resolver)
	if err != nil {
		e.BuildErr = err
		// When a scene fails to build solely because of a broken connection (for example an
		// object renamed in an unsaved base/instanced scene), keep rendering the live scene
		// objects so the viewport draws the Spec Problem overlay directly on them.
		if len(e.Doc.Connections) > 0 {
			noConns := e.Doc.Clone()
			noConns.Connections = nil
			if fallback, ferr := sceneformat.Build(noConns, resolver); ferr == nil {
				fallback.DrawAllTiles = true
				e.Built = fallback
				e.syncSceneEmitterLoop()
				e.recomputeNavPath2D()
			}
		}
		return
	}
	// The 2D view pans and zooms past the scene camera's frame, so the edit scene draws every tile of its maps.
	built.DrawAllTiles = true
	e.Built = built
	e.BuildErr = nil
	e.syncSceneEmitterLoop()
	e.recomputeNavPath2D()
}

// ActiveScene returns the live play scene when playing, the read-only history preview scene when previewing a timeline step, the scene posed by the open state machine or animation clip when previewing, or the edit BuiltScene otherwise.
func (e *Editor) ActiveScene() *sceneformat.BuiltScene {
	if e.playing && e.playScene != nil {
		return e.playScene
	}
	if e.historyPanel.preview != nil && e.historyPanel.preview.scene != nil {
		return e.historyPanel.preview.scene
	}
	if e.cutscene.scene != nil {
		return e.cutscene.scene
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
	if t == ToolPlugin || (e.Tool == t && e.gridMap.tool == GridMapToolNone) {
		return
	}
	e.CancelDrag2D()
	e.CancelMarquee2D()
	e.CancelInspectorScrub()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.ClearPluginHover2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.ClearPluginHover3D()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	e.gridMap.tool = GridMapToolNone
	e.Tool = t
	switch t {
	case ToolMove:
		e.GizmoMode = Gizmo3DTranslate
	case ToolRotate:
		e.GizmoMode = Gizmo3DRotate
	case ToolScale:
		e.GizmoMode = Gizmo3DScale
	}
	if isTileTool(t) {
		e.inspectLayer = false
	}
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
	if mode == ViewMode2D {
		e.resetViewport3DAgentPrompt(false)
	}
	e.endTerrainStrokeForChange()
	e.CancelGizmoDrag3D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.ClearPluginHover3D()
	e.EndViewport3DDrag()
	e.endFly3D()
	e.CancelDrag2D()
	e.CancelMarquee2D()
	e.CancelInspectorScrub()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.ClearPluginHover2D()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.CancelSketchDrag()
	e.overlaysPopoverOpen = false
	e.snapSettingsPopoverOpen = false
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

// SetGizmoSpace3D switches the 3D transform gizmo between Global (world) and Local (object) axes.
// It cancels any active 3D gizmo drag so no drag mixes spaces, is not undoable, and ignores unknown values.
func (e *Editor) SetGizmoSpace3D(space GizmoSpace3D) {
	if e == nil || e.playing || e.GizmoSpace3D == space || (space != GizmoSpaceGlobal && space != GizmoSpaceLocal) {
		return
	}
	e.CancelGizmoDrag3D()
	e.GizmoSpace3D = space
	e.Status = "Space: " + space.String()
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
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.gridMap.tool = GridMapToolNone
	e.GizmoMode = mode
	e.Status = "Gizmo: " + mode.String()
	e.notify()
}

// ToggleGizmoSpace3D switches the 3D transform gizmo between Global (world) and Local (object) axes and returns the active space.
func (e *Editor) ToggleGizmoSpace3D() GizmoSpace3D {
	if e == nil {
		return GizmoSpaceGlobal
	}
	if e.GizmoSpace3D == GizmoSpaceLocal {
		e.SetGizmoSpace3D(GizmoSpaceGlobal)
	} else {
		e.SetGizmoSpace3D(GizmoSpaceLocal)
	}
	return e.GizmoSpace3D
}

// SetActiveLayer selects the layer targeted by object placement and tile painting. When the layer's tileset (TilePalette) has no tile with ActiveTileID, its first tile becomes the active tile.
func (e *Editor) SetActiveLayer(name string) bool {
	if e.layerIndex(name) < 0 && !e.inheritedLayers()[name] {
		return false
	}
	if name != e.ActiveLayer {
		e.endTileStrokeForChange()
		e.tileSel = TileRect{}
		e.hasTileSel = false
		e.paletteStampAnchor = 0
		e.paletteStampRect = TileRect{}
		e.hasPaletteStampRect = false
	}
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	if e.rooms != nil {
		e.rooms.selectedRoomID = ""
	}
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
		e.tileSel = TileRect{}
		e.hasTileSel = false
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
	e.inspectLayer = false
	e.notify()
}

// SetActiveTerrain sets the terrain set name used by ToolTerrain, PaintTerrain and EraseTerrain; choosing a terrain is view state and does not enter undo history.
func (e *Editor) SetActiveTerrain(name string) {
	if e == nil || e.ActiveTerrain == name {
		return
	}
	e.endTileStrokeForChange()
	e.ActiveTerrain = name
	e.notify()
}

// SetComponentRegistry makes the editor check components against reg: every open scene's components and connections must already conform, later opens refuse files whose components or connections do not, SetComponentField and PlaceObject refuse unregistered fields, and the inspector lists every registered field of a component. Nil turns the checks off. Changing the registry clears undo/redo history across open tabs, since commands recorded under the old registry could bring back components the new one refuses.
func (e *Editor) SetComponentRegistry(reg *sceneformat.ComponentRegistry) error {
	e.syncActiveTab()
	for _, t := range e.tabs {
		if t.doc == nil {
			continue
		}
		if err := sceneformat.ValidateComponents(t.doc, reg); err != nil {
			return err
		}
		// OpenFile binds connections against the registry (sceneformat.ParseWith), so each open scene must pass the same check, or a scene the editor lets the user save would not open again. A scene that instances or inherits others is checked once resolved, as ParseWith leaves it.
		if !t.doc.RefersToScenes() {
			if err := sceneformat.ValidateConnections(t.doc, reg); err != nil {
				return err
			}
		}
	}
	if reg == e.components {
		return nil
	}
	e.components = reg
	for i, t := range e.tabs {
		if i != e.activeTab && t.history != nil {
			t.history.commands = nil
			t.history.cursor = 0
			if t.history.group != nil {
				t.history.group.cmds = nil
			}
		}
	}
	// Clear triggers onChange -> afterMutate -> Rebuild + notify.
	e.History.Clear()
	e.rebuildAllInactiveTabs()
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
	e.marquee = marqueeState{}
	e.shapeDrag = shapeDragState{}
	e.CancelPluginDrag2D()
	e.ClearPluginHover2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.ClearPluginHover3D()
	e.cameraDrag = cameraDragState{}
	e.cameraBoundsDrag = cameraBoundsDragState{}
	e.roomDrag = roomDragState{}
	e.rooms = nil
	e.bgPreview = bgPreviewState{}
	// A stroke under way edits the scene being replaced, whose history goes with it.
	e.stroke = nil
	e.gridMap.stroke = nil
	e.tileSel = TileRect{}
	e.hasTileSel = false
	e.paletteStampAnchor = 0
	e.paletteStampRect = TileRect{}
	e.hasPaletteStampRect = false
	e.tileReview = nil
	e.clearAgentLiveForSceneReset()
	e.Doc = doc
	e.sceneSaved = saved
	e.clearSelectionFields()
	e.contextMenu.open = false
	e.contextMenu.targetID = ""
	e.pasteStreak = 0
	e.inspectAssetFile = false
	e.inspectLayer = false
	e.connPick = nil
	// Another scene opens at its own camera's view.
	e.view2DFree = false
	e.lookThroughCamera3D = false
	e.hidden, e.locked, e.layerLocked = nil, nil, nil
	// The loaded scene did not come from the current path, so a later Save must
	// not overwrite the previously opened file with unrelated content.
	e.FilePath = ""
	e.ScenePath = ""
	e.clearSceneDiskSnapshot()
	if len(doc.Layers) > 0 {
		e.ActiveLayer = doc.Layers[0].Name
	} else {
		e.ActiveLayer = ""
	}
	e.keepTileInPalette()
	e.nav2D = nav2DState{}
	e.resetNavMeshForScene()
	if e.activeTab >= 0 && e.activeTab < len(e.tabs) {
		e.tabs[e.activeTab].placeholder = false
	}
	e.Status = "Opened scene"
	// Clear triggers onChange -> afterMutate -> Rebuild + notify.
	e.History.Clear()
	e.syncActiveTab()
	e.syncAgentThreadForOpenScene()
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

// OpenFile reads a YAML scene file from path and opens it in a tab, or switches to its existing tab when that file is already open, preserving every open tab's undo history. A scene that does not parse returns sceneformat.Parse's *loaderr.Error, naming path.
func (e *Editor) OpenFile(path string) error {
	return e.openFileWithLayers(path, e.PhysicsLayers())
}

func (e *Editor) openFileWithLayers(path string, layers *sceneformat.PhysicsLayerTable) error {
	e.CloseAskInPlace()
	e.ExitHistoryPreview()
	rel, _ := e.projectRel(path)
	if idx := e.findTabByPath(rel, path); idx >= 0 {
		if e.playing {
			if err := e.Stop(); err != nil {
				return err
			}
		}
		return e.SelectTab(idx)
	}
	if rel != "" {
		if staged, ok := e.stagedFiles[rel]; ok && !staged.shouldDelete && staged.sceneDoc != nil {
			saved := append([]byte(nil), staged.savedData...)
			if len(saved) == 0 {
				if enc, encErr := sceneformat.Encode(staged.sceneDoc); encErr == nil {
					saved = enc
				}
			}
			docClone := staged.sceneDoc.Clone()
			if e.playing {
				if err := e.Stop(); err != nil {
					return err
				}
			}
			// Promote staged copy into a real open tab and drop the staged entry so saveKeptStagedFiles cannot clobber later tab edits.
			delete(e.stagedFiles, rel)
			e.activateNewTab(docClone, saved, path)
			if diskData, rErr := os.ReadFile(path); rErr == nil {
				e.recordSceneDiskSnapshot(path, diskData)
			}
			e.syncActiveTab()
			e.notify()
			return nil
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		openErr := loaderr.Locate(err, path, nil, "check that the scene exists at this path and can be read")
		e.recordSceneOpenError(path, openErr)
		return openErr
	}
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
		File:          path,
		Components:    e.components,
		PhysicsLayers: layers,
	})
	if err != nil {
		openErr := loaderr.WithFile(err, path)
		e.recordSceneOpenError(path, openErr)
		return openErr
	}
	saved, err := sceneformat.Encode(doc)
	if err != nil {
		openErr := loaderr.WithFile(err, path)
		e.recordSceneOpenError(path, openErr)
		return openErr
	}
	if e.playing {
		if err := e.Stop(); err != nil {
			return err
		}
	}
	e.sceneOpenErr = nil
	e.proposedNewObjects = nil
	e.activateNewTab(doc, saved, path)
	e.recordSceneDiskSnapshot(path, data)
	e.syncActiveTab()
	e.notify()
	return nil
}

// SaveFile encodes the scene to YAML and writes it to path, then rebuilds any other open tabs that instance or inherit scenes.
func (e *Editor) SaveFile(path string) error {
	if !e.playing {
		e.CancelShapeDrag2D()
		e.CancelPluginDrag2D()
		e.CancelComponentGizmoDrag3D()
		e.CancelPluginDrag3D()
		e.CancelCameraDrag()
		e.CancelCameraBoundsDrag()
		e.CancelRoomDrag()
		e.cancelBackgroundPreview()
		e.cancelMaterialColorPreview()
		e.endTileStrokeForChange()
		e.exitTileReviewBeforePreview()
		e.ExitHistoryPreview()
		e.endGridMapStrokeForChange()
	}
	if err := e.checkDiskConflict(DiskFileScene, path, e.disk.sceneDisk); err != nil {
		return err
	}
	data, err := e.SaveYAML()
	if err != nil {
		return err
	}
	if err := e.saveWriter()(path, data); err != nil {
		return err
	}
	e.FilePath = path
	e.sceneSaved = append([]byte(nil), data...)
	if e.activeTab >= 0 && e.activeTab < len(e.tabs) {
		e.tabs[e.activeTab].placeholder = false
	}
	e.recordSceneDiskSnapshot(path, data)
	prevScenePath := e.ScenePath
	e.syncScenePath()
	e.syncActiveTab()
	e.noteSavedHistoryFile(path)
	if e.ScenePath != prevScenePath {
		e.syncAgentThreadForOpenScene()
	}
	e.saveViewStateQuietly()
	e.filesRev++
	e.snapshotProjectTree()
	e.invalidateProjectIndex()
	e.rebuildDependentTabs(e.activeTabID())
	e.recordSaveSucceededFor(e.activeTabID(), false)
	e.Status = "Saved " + path
	e.notify()
	e.CheckProblems()
	e.NotifyAutomationFileChanged(path)
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

func (e *Editor) canUndo() bool {
	return e != nil && (e.canUndoCutsceneReviewEdit() || e.AgentTurnReviewOpen() || e.History.CanUndo())
}

func (e *Editor) canRedo() bool {
	return e != nil && (e.canRedoCutsceneReviewEdit() || e.History.CanRedo())
}

// Undo reverts the last edit in History (or follows Checkpoints.dc.html rules during an active agent turn/review, or the last manual edit joined to an open cutscene review; no-op while playing). A step that fails to revert, such as a file move (MoveFile) that something now on disk blocks, stays in place, and the status bar says why.
func (e *Editor) Undo() bool {
	if e.playing {
		return false
	}
	e.ExitHistoryPreview()
	if e.undoCutsceneReviewEdit() {
		return true
	}
	// A drag in progress is put back first, so the step undone is the one before it and not the half-dragged object.
	e.CancelDrag2D()
	e.CancelMarquee2D()
	if handled, ok := e.undoForAgentTurn(); handled {
		return ok
	}
	e.CancelInspectorScrub()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.CancelRigDrag()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.cancelBackgroundPreview()
	e.cancelMaterialColorPreview()
	// A stroke under way is recorded first, so Undo takes it back as a whole.
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	e.endTerrainStrokeForChange()
	canUndo := e.History != nil && e.History.CanUndo()
	if canUndo {
		e.prepareTileReviewForHistoryStep()
	}
	ok, err := e.History.undo()
	if canUndo {
		e.finishTileReviewHistoryStep(ok)
	}
	if err != nil {
		e.reportStatusError("Undo failed: " + err.Error())
	}
	if ok {
		e.Status = "Undo"
		e.notify()
	}
	return ok
}

// Redo re-applies the last undone edit in History (or the last undone manual edit in an open cutscene review; no-op while playing). A step that fails to re-apply stays undone, and the status bar says why.
func (e *Editor) Redo() bool {
	if e.playing {
		return false
	}
	e.ExitHistoryPreview()
	if e.redoCutsceneReviewEdit() {
		return true
	}
	e.CancelDrag2D()
	e.CancelMarquee2D()
	e.CancelInspectorScrub()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.CancelRigDrag()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.cancelBackgroundPreview()
	e.cancelMaterialColorPreview()
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	e.endTerrainStrokeForChange()
	canRedo := e.History != nil && e.History.CanRedo()
	if canRedo {
		e.prepareTileReviewForHistoryStep()
	}
	ok, err := e.History.redo()
	if canRedo {
		e.finishTileReviewHistoryStep(ok)
	}
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
	if id != e.SelectedID {
		e.CancelInspectorScrub()
		e.CancelComponentGizmoDrag3D()
	}
	if id == "" {
		hadRoom := e.rooms != nil && e.rooms.selectedRoomID != ""
		if e.SelectedID != "" || len(e.selectedIDs) > 0 || e.inspectAssetFile || e.inspectLayer || e.connPick != nil || hadRoom {
			e.CancelRoomDrag()
			e.clearSelectionFields()
			e.contextMenu.open = false
			e.contextMenu.targetID = ""
			e.inspectAssetFile = false
			e.inspectLayer = false
			e.connPick = nil
			if e.rooms != nil {
				e.rooms.selectedRoomID = ""
			}
			e.notify()
		}
		return true
	}
	if obj, _ := e.FindResolvedObject(id); obj == nil {
		return false
	} else if obj.GridMap != "" && e.gridMap.relPath != obj.GridMap && !e.GridMapDirty() {
		// Selecting an object that names a GridMap loads it automatically when a project or GridMapFS is available.
		e.setSelectionQuiet([]string{id}, id)
		e.Status = "Selected " + id
		e.autoOpenSceneGridMap()
		e.notify()
		return true
	}
	hadRoom := e.rooms != nil && e.rooms.selectedRoomID != ""
	if e.SelectedID != id || len(e.selectedIDs) != 1 || e.selectedIDs[0] != id || e.inspectAssetFile || e.inspectLayer || e.connPick != nil || hadRoom {
		e.CancelCameraDrag()
		e.CancelCameraBoundsDrag()
		e.CancelRoomDrag()
		e.setSelectionQuiet([]string{id}, id)
		e.inspectAssetFile = false
		e.inspectLayer = false
		e.connPick = nil
		if e.rooms != nil {
			e.rooms.selectedRoomID = ""
		}
		e.Status = "Selected " + id
		e.notify()
	}
	return true
}

// SelectedObject returns the currently selected ObjectDoc (local, instanced, or inherited) and its layer name, or nil, "".
func (e *Editor) SelectedObject() (*sceneformat.ObjectDoc, string) {
	if e.SelectedID == "" || e.Doc == nil {
		return nil, ""
	}
	return e.FindResolvedObject(e.SelectedID)
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
	if e.HistoryPreviewActive() {
		return "", ErrHistoryPreview
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
		ed:              e,
		layerName:       e.Doc.Layers[li].Name,
		obj:             placed,
		prevSelected:    e.SelectedID,
		prevSelectedIDs: e.SelectedIDs(),
		prevVersion:     e.Doc.Version,
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
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	if err := e.checkAgentTouchLockSubtree(id); err != nil {
		return err
	}
	if e.IsObjectPausedByPlan(id) {
		return fmt.Errorf("editor: manual edits to %q are paused while the agent runs its plan", id)
	}
	// A drag in progress is put back first, so Undo of the delete restores the object as it was before the drag.
	e.CancelDrag2D()
	e.CancelMarquee2D()
	e.CancelInspectorScrub()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	loc := locateObject(e.Doc, id)
	if loc.obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	cmd := &deleteObjectCmd{
		ed:              e,
		layerName:       e.Doc.Layers[loc.layer].Name,
		index:           loc.index,
		obj:             cloneObjectDoc(*loc.obj),
		wasNilSlice:     false,
		prevSelected:    e.SelectedID,
		prevSelectedIDs: e.SelectedIDs(),
	}
	if loc.parent != nil {
		cmd.parent = loc.parent.ID
	}
	e.History.Execute(cmd)
	return nil
}

// MoveObject sets object id's position to (x, y) as an undoable command.
// When id names an instanced or inherited object, it records an override on its instance or inheriting scene.
func (e *Editor) MoveObject(id string, x, y float32) error {
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if e.IsObjectPausedByPlan(id) {
		return fmt.Errorf("editor: manual edits to %q are paused while the agent runs its plan", id)
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return errors.New("editor: position must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Move "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if x != resolved.X {
					ov.X = sceneformat.Float32(x)
				}
				if y != resolved.Y {
					ov.Y = sceneformat.Float32(y)
				}
				return nil
			})
		}
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
// When id names an instanced or inherited object, it records an override on its instance or inheriting scene.
func (e *Editor) RotateObject(id string, radians float32) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if !isFiniteFloat32(radians) {
		return errors.New("editor: rotation must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Rotate "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if resolved.Rotation == radians {
					return nil
				}
				ov.Rotation = sceneformat.Float32(radians)
				return nil
			})
		}
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
// When id names an instanced or inherited object, it records an override on its instance or inheriting scene.
func (e *Editor) ScaleObject(id string, sx, sy float32) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return errors.New("editor: scale must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Scale "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if sx != resolved.EffectiveScaleX() {
					ov.ScaleX = sceneformat.Float32(sx)
				}
				if sy != resolved.EffectiveScaleY() {
					ov.ScaleY = sceneformat.Float32(sy)
				}
				return nil
			})
		}
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
// When id names an instanced or inherited object, it records an override on its instance or inheriting scene.
func (e *Editor) SetObjectAlpha(id string, alpha float32) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if err := e.checkObjectNotPausedByPlan(id); err != nil {
		return err
	}
	if !isFiniteFloat32(alpha) || alpha < 0 || alpha > 1 {
		return fmt.Errorf("editor: alpha %v must be in [0, 1]", alpha)
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Set alpha "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if resolved.EffectiveAlpha() == alpha && ov.Alpha == nil {
					return nil
				}
				ov.Alpha = sceneformat.Float32(alpha)
				return nil
			})
		}
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
// When id names an instanced or inherited object, it records an override on its instance or inheriting scene.
func (e *Editor) SetObjectZ(id string, z int) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if err := e.checkObjectNotPausedByPlan(id); err != nil {
		return err
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Set Z "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if resolved.Z == z {
					return nil
				}
				ov.Z = sceneformat.Int(z)
				return nil
			})
		}
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
// When id names an instanced or inherited object, it records an override on its instance or inheriting scene.
func (e *Editor) SetObjectName(id, name string) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if err := e.checkObjectNotPausedByPlan(id); err != nil {
		return err
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Rename "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if resolved.Name == name {
					return nil
				}
				ov.Name = sceneformat.String(name)
				return nil
			})
		}
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
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) || !isFiniteFloat32(posZ) {
		return errors.New("editor: 3D position must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Move 3D "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if x != resolved.X {
					ov.X = sceneformat.Float32(x)
				}
				if y != resolved.Y {
					ov.Y = sceneformat.Float32(y)
				}
				if posZ != resolved.PosZ {
					ov.PosZ = sceneformat.Float32(posZ)
				}
				return nil
			})
		}
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
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if !isFiniteFloat32(rotX) || !isFiniteFloat32(rotY) || !isFiniteFloat32(rotZ) {
		return errors.New("editor: 3D rotation must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Rotate 3D "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if rotX != resolved.RotX {
					ov.RotX = sceneformat.Float32(rotX)
				}
				if rotY != resolved.RotY {
					ov.RotY = sceneformat.Float32(rotY)
				}
				if rotZ != resolved.RotZ {
					ov.RotZ = sceneformat.Float32(rotZ)
				}
				return nil
			})
		}
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
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if !isFiniteFloat32(sx) || !isFiniteFloat32(sy) || !isFiniteFloat32(sz) {
		return errors.New("editor: 3D scale must be finite")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Scale 3D "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if sx != resolved.EffectiveScaleX() {
					ov.ScaleX = sceneformat.Float32(sx)
				}
				if sy != resolved.EffectiveScaleY() {
					ov.ScaleY = sceneformat.Float32(sy)
				}
				if sz != resolved.EffectiveScaleZ() {
					ov.ScaleZ = sceneformat.Float32(sz)
				}
				return nil
			})
		}
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
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if !isFiniteFloat32(depth) || depth < 0 {
		return fmt.Errorf("editor: depth %v must be finite and non-negative", depth)
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Set depth "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if resolved.Depth == depth {
					return nil
				}
				ov.Depth = sceneformat.Float32(depth)
				return nil
			})
		}
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
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if mesh != "" && !slices.Contains(sceneformat.MeshPrimitives(), mesh) {
		return fmt.Errorf("editor: mesh %q is not a built-in primitive (want one of %s)", mesh, strings.Join(sceneformat.MeshPrimitives(), ", "))
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Set mesh "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if resolved.Mesh == mesh {
					return nil
				}
				if mesh != "" && resolved.Model != "" {
					return fmt.Errorf("editor: object %q draws model %q; it cannot also draw mesh %q", id, resolved.Model, mesh)
				}
				if mesh != "" && resolved.GridMap != "" {
					return fmt.Errorf("editor: object %q draws gridMap %q; it cannot also draw mesh %q", id, resolved.GridMap, mesh)
				}
				if mesh == "" && resolved.Model == "" && resolved.Occluder != nil && resolved.Occluder.Mode == sceneformat.OccluderModel {
					return fmt.Errorf("editor: object %q has a model occluder; clear or switch occluder to box before removing mesh", id)
				}
				ov.Mesh = sceneformat.String(mesh)
				return nil
			})
		}
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
	ed              *Editor
	layerName       string
	obj             sceneformat.ObjectDoc
	prevSelected    string
	prevSelectedIDs []string
	wasNilSlice     bool
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
	if c.ed.Doc.Version < sceneformat.CurrentVersion && (instancesScene(&c.obj) || drawsModel(&c.obj) || hasOccluder(&c.obj) || hasParticles(&c.obj) || usesScene3DFeatures(&c.obj)) {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	c.ed.Doc.Layers[li].Objects = append(c.ed.Doc.Layers[li].Objects, cloneObjectDoc(c.obj))
	c.ed.setSelectionQuiet([]string{c.obj.ID}, c.obj.ID)
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
	c.ed.setSelectionQuiet(c.prevSelectedIDs, c.prevSelected)
}

// usesScene3DFeatures reports whether o or any of its descendants carries a 3D light, 3D camera, reflection probe, or light probe grid, which needs a version 4 scene.
func usesScene3DFeatures(o *sceneformat.ObjectDoc) bool {
	found := false
	sceneformat.WalkObjects([]sceneformat.ObjectDoc{*o}, func(c, _ *sceneformat.ObjectDoc) bool {
		found = c.Light != nil || c.Camera != nil || c.ReflectionProbe != nil || c.LightProbeGrid != nil
		return !found
	})
	return found
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

// hasParticles reports whether o or any of its descendants places a particle emitter, which needs a version 2 scene.
func hasParticles(o *sceneformat.ObjectDoc) bool {
	found := false
	sceneformat.WalkObjects([]sceneformat.ObjectDoc{*o}, func(c, _ *sceneformat.ObjectDoc) bool {
		found = c.Particles != nil
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
	parent          string
	index           int
	obj             sceneformat.ObjectDoc
	wasNilSlice     bool
	prevSelected    string
	prevSelectedIDs []string
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
	remaining := make([]string, 0, len(c.ed.selectedIDs))
	for _, sid := range c.ed.SelectedIDs() {
		if _, removed := ids[sid]; !removed {
			remaining = append(remaining, sid)
		}
	}
	primary := c.ed.SelectedID
	if _, removed := ids[primary]; removed {
		if len(remaining) > 0 {
			primary = remaining[len(remaining)-1]
		} else {
			primary = ""
		}
	}
	c.ed.setSelectionQuiet(remaining, primary)
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
	c.ed.setSelectionQuiet(c.prevSelectedIDs, c.prevSelected)
}

func historyObjectDisplayName(ed *Editor, id string) string {
	if ed != nil && id != "" {
		if obj, _, _ := ed.FindObject(id); obj != nil && obj.Name != "" {
			return obj.Name
		}
	}
	return id
}

func (c *placeObjectCmd) historyMeta() HistoryMeta {
	name := c.obj.Name
	if name == "" {
		name = c.obj.ID
	}
	return HistoryMeta{
		Author:  HistoryAuthorYou,
		Summary: "added " + name,
		Detail:  fmt.Sprintf("on %s (%g, %g)", c.layerName, c.obj.X, c.obj.Y),
	}
}

func (c *deleteObjectCmd) historyMeta() HistoryMeta {
	name := c.obj.Name
	if name == "" {
		name = c.obj.ID
	}
	return HistoryMeta{
		Author:  HistoryAuthorYou,
		Summary: "removed " + name,
		Detail:  "from " + c.layerName,
	}
}

type moveObjectCmd struct {
	ed         *Editor
	id         string
	oldX, oldY float32
	newX, newY float32
}

func (c *moveObjectCmd) Label() string { return "Move " + c.id }

func (c *moveObjectCmd) historyMeta() HistoryMeta {
	name := historyObjectDisplayName(c.ed, c.id)
	var detail string
	switch {
	case c.oldX != c.newX && c.oldY == c.newY:
		detail = fmt.Sprintf("x %g → %g", c.oldX, c.newX)
	case c.oldX == c.newX && c.oldY != c.newY:
		detail = fmt.Sprintf("y %g → %g", c.oldY, c.newY)
	default:
		detail = fmt.Sprintf("x %g → %g, y %g → %g", c.oldX, c.newX, c.oldY, c.newY)
	}
	return HistoryMeta{
		Author:  HistoryAuthorYou,
		Summary: "moved " + name,
		Detail:  detail,
	}
}

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

func (c *rotateObjectCmd) historyMeta() HistoryMeta {
	name := historyObjectDisplayName(c.ed, c.id)
	return HistoryMeta{
		Author:  HistoryAuthorYou,
		Summary: "rotated " + name,
		Detail:  fmt.Sprintf("rotation %g → %g", c.oldRot, c.newRot),
	}
}

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

func (c *scaleObjectCmd) historyMeta() HistoryMeta {
	name := historyObjectDisplayName(c.ed, c.id)
	val := func(p *float32) float32 {
		if p == nil {
			return 1
		}
		return *p
	}
	return HistoryMeta{
		Author:  HistoryAuthorYou,
		Summary: "scaled " + name,
		Detail:  fmt.Sprintf("scale %g×%g → %g×%g", val(c.oldScaleX), val(c.oldScaleY), val(c.newScaleX), val(c.newScaleY)),
	}
}

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

func (c *alphaObjectCmd) historyMeta() HistoryMeta {
	name := historyObjectDisplayName(c.ed, c.id)
	val := func(p *float32) float32 {
		if p == nil {
			return 1
		}
		return *p
	}
	return HistoryMeta{
		Author:  HistoryAuthorYou,
		Summary: "changed alpha of " + name,
		Detail:  fmt.Sprintf("alpha %g → %g", val(c.oldAlpha), val(c.newAlpha)),
	}
}

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

func (c *zObjectCmd) historyMeta() HistoryMeta {
	name := historyObjectDisplayName(c.ed, c.id)
	return HistoryMeta{
		Author:  HistoryAuthorYou,
		Summary: "changed z of " + name,
		Detail:  fmt.Sprintf("z %d → %d", c.oldZ, c.newZ),
	}
}

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

func (c *nameObjectCmd) historyMeta() HistoryMeta {
	oldLabel := c.oldName
	if oldLabel == "" {
		oldLabel = c.id
	}
	newLabel := c.newName
	if newLabel == "" {
		newLabel = c.id
	}
	return HistoryMeta{
		Author:  HistoryAuthorYou,
		Summary: "renamed " + c.id,
		Detail:  fmt.Sprintf("%s → %s", oldLabel, newLabel),
	}
}

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
	if o.Particles != nil && (o.Particles.File == "" || !validScenePath(o.Particles.File)) {
		return fmt.Errorf("editor: object particles file %q must be a slash-separated path relative to the project root", o.Particles.File)
	}
	if o.Scene != "" && !validScenePath(o.Scene) {
		return fmt.Errorf("editor: object scene %q must be a slash-separated path relative to the project root", o.Scene)
	}
	if len(o.Overrides) > 0 && o.Scene == "" {
		return errors.New("editor: object overrides require a scene")
	}
	if err := sceneformat.CheckObjectModel(o); err != nil {
		return err
	}
	if err := sceneformat.CheckObjectLight(o); err != nil {
		return err
	}
	return sceneformat.CheckObjectCamera3D(o)
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
	case []sceneformat.Point:
		return slices.Clone(x)
	default:
		return v
	}
}

func isFiniteFloat32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
