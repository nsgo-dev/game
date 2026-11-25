package editor

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// Built-in editor command IDs registered in the unified command registry.
const (
	CmdNewProject            = "file.newProject"
	CmdOpenProject           = "file.openProject"
	CmdStartScreen           = "file.startScreen"
	CmdSave                  = "file.save"
	CmdUndo                  = "edit.undo"
	CmdRedo                  = "edit.redo"
	CmdDelete                = "edit.delete"
	CmdDuplicate             = "edit.duplicate"
	CmdToolSelect            = "tool.select"
	CmdToolMove              = "tool.move"
	CmdToolPlace             = "tool.place"
	CmdToolPaintTile         = "tool.paint"
	CmdToolEraseTile         = "tool.erase"
	CmdToolFillTile          = "tool.fill"
	CmdToolTerrain           = "tool.terrain"
	CmdGizmoTranslate        = "gizmo.translate"
	CmdGizmoRotate           = "gizmo.rotate"
	CmdGizmoScale            = "gizmo.scale"
	CmdView2D                = "view.mode2d"
	CmdView3D                = "view.mode3d"
	CmdShadingLit            = "view.shading.lit"
	CmdShadingUnlit          = "view.shading.unlit"
	CmdShadingWire           = "view.shading.wireframe"
	CmdNavMeshPanel          = "view.navmesh"
	CmdNavMeshOverlay        = "view.navmesh.overlay"
	CmdBakeNavMesh3D         = "project.bakeNavMesh3D"
	CmdFrameSelection        = "view.frame"
	CmdToggleLightingPreview = "view.lighting"
	CmdToggleAllShapes       = "view.shapes"
	CmdToggleSnapGrid        = "view.snap"
	CmdCommandPalette        = "view.palette"
	CmdPlayStop              = "project.play"
	CmdRunGame               = "project.run"
	CmdProjectSettings       = "project.settings"
	CmdAudioMixer            = "project.mixer"
	CmdLocalization          = "project.l10n"
	CmdParticleEditor        = "project.particles"
	CmdExport                = "project.export"
	CmdProfiler              = "project.profiler"
	CmdCheckProblems         = "project.check"
	// The 3D viewport's axis views and projection: each axis view looks at the camera's Target along a world axis (SetView3D), and the orthographic command switches between perspective and orthographic (SetOrthographic3D).
	CmdView3DFront        = "view.3d.front"
	CmdView3DBack         = "view.3d.back"
	CmdView3DRight        = "view.3d.right"
	CmdView3DLeft         = "view.3d.left"
	CmdView3DTop          = "view.3d.top"
	CmdView3DBottom       = "view.3d.bottom"
	CmdView3DOrthographic = "view.3d.orthographic"
)

var (
	// ErrCommandNotFound is returned by ExecuteCommand when no active command has the requested ID.
	ErrCommandNotFound = errors.New("editor: command not found")
	// ErrCommandDisabled is returned by ExecuteCommand when the target command is currently disabled.
	ErrCommandDisabled = errors.New("editor: command is disabled")
)

// EditorCommand describes an invocable command in the editor's unified command registry,
// shared by keyboard shortcuts, the desktop menu bar, and the command palette.
type EditorCommand struct {
	// ID uniquely identifies the command (for example "file.save" or "plugin.health.heal-all").
	ID string
	// Label is the display title shown in menus and the command palette.
	Label string
	// Description is optional explanatory text shown in the command palette.
	Description string
	// Category groups the command in the command palette (for example "File", "Edit", "Tools", "View", "Project").
	Category string
	// Keywords are extra search terms matched by the command palette.
	Keywords []string
	// Shortcut is the canonical primary NSGo key shortcut (using "mod" for Command on Apple platforms and Ctrl elsewhere), or empty when unbound.
	Shortcut string
	// AlternateShortcuts are optional additional shortcuts that invoke the same command (such as "backspace" alongside "delete").
	AlternateShortcuts []string
	// Enabled reports whether the command can run right now; nil means always enabled.
	Enabled func() bool
	// Checked reports whether the command represents an active mode or open toggle; nil for non-toggle commands.
	Checked func() bool
	// Run executes the command.
	Run func() error
}

// NormalizeShortcut validates and normalizes raw into NSGo's canonical shortcut form
// (modifiers in order mod, ctrl, alt, shift, meta followed by the lowercase key name).
// An empty or whitespace-only string returns "", nil.
func NormalizeShortcut(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if hasControl(trimmed) {
		return "", fmt.Errorf("editor: shortcut %q contains control characters", raw)
	}
	parts := strings.Split(strings.ToLower(trimmed), "+")
	if len(parts) == 0 {
		return "", fmt.Errorf("editor: invalid shortcut %q", raw)
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	if key == "" {
		return "", fmt.Errorf("editor: shortcut %q is missing a key", raw)
	}
	switch key {
	case "return":
		key = "enter"
	case "del":
		key = "delete"
	case "esc":
		key = "escape"
	case "mod", "ctrl", "control", "alt", "option", "shift", "meta", "cmd", "command":
		return "", fmt.Errorf("editor: shortcut %q must end with a non-modifier key", raw)
	}

	alias := map[string]string{
		"mod":     "mod",
		"ctrl":    "ctrl",
		"control": "ctrl",
		"alt":     "alt",
		"option":  "alt",
		"shift":   "shift",
		"meta":    "meta",
		"cmd":     "meta",
		"command": "meta",
	}
	seen := make(map[string]bool, len(parts)-1)
	for _, m := range parts[:len(parts)-1] {
		m = strings.TrimSpace(m)
		canon, ok := alias[m]
		if !ok {
			return "", fmt.Errorf("editor: shortcut %q has unknown modifier %q", raw, m)
		}
		if seen[canon] {
			return "", fmt.Errorf("editor: shortcut %q repeats modifier %q", raw, canon)
		}
		seen[canon] = true
	}

	var out []string
	for _, m := range []string{"mod", "ctrl", "alt", "shift", "meta"} {
		if seen[m] {
			out = append(out, m)
		}
	}
	out = append(out, key)
	return strings.Join(out, "+"), nil
}

// FormatShortcut formats shortcut for display on targetOS using NSGo's platform conventions
// (for example "mod+s" renders as "⌘S" on macOS and "Ctrl+S" on Windows and Linux).
func FormatShortcut(shortcut string, targetOS core.OS) string {
	norm, err := NormalizeShortcut(shortcut)
	if err != nil {
		return shortcut
	}
	if norm == "" {
		return ""
	}
	apple := targetOS == core.OSMacOS || targetOS == core.OSIOS
	return core.KeyLabel(norm, targetOS, apple)
}

// shortcutCollisionKeys returns the platform-resolved shortcut strings that a canonical
// shortcut occupies on Apple platforms (mod -> meta) and non-Apple platforms (mod -> ctrl).
// Two shortcuts clash if they share any collision key.
func shortcutCollisionKeys(canonical string) []string {
	if canonical == "" {
		return nil
	}
	resolve := func(modTarget string) string {
		parts := strings.Split(canonical, "+")
		key := parts[len(parts)-1]
		seen := make(map[string]bool, len(parts)-1)
		for _, m := range parts[:len(parts)-1] {
			if m == "mod" {
				m = modTarget
			}
			seen[m] = true
		}
		var out []string
		for _, m := range []string{"ctrl", "alt", "shift", "meta"} {
			if seen[m] {
				out = append(out, m)
			}
		}
		out = append(out, key)
		return strings.Join(out, "+")
	}
	appleKey := resolve("meta")
	otherKey := resolve("ctrl")
	if appleKey == otherKey {
		return []string{appleKey}
	}
	return []string{appleKey, otherKey}
}

// shortcutsForCommand returns every non-empty canonical shortcut owned by cmd.
func shortcutsForCommand(cmd EditorCommand) []string {
	var out []string
	if cmd.Shortcut != "" {
		out = append(out, cmd.Shortcut)
	}
	for _, alt := range cmd.AlternateShortcuts {
		if alt != "" && !slices.Contains(out, alt) {
			out = append(out, alt)
		}
	}
	return out
}

func validateCommandID(id string) error {
	if id == "" {
		return errors.New("editor: command has no ID")
	}
	if strings.IndexFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("editor: command ID %q must have no spaces or control characters", id)
	}
	return nil
}

// initBuiltinCommands populates the editor's built-in command registry.
func (e *Editor) initBuiltinCommands() {
	if len(e.commands) > 0 {
		return
	}
	notPlaying := func() bool { return !e.StartScreenOpen() && !e.Playing() }
	inEditor := func() bool { return !e.StartScreenOpen() }
	hasProject := func() bool { return !e.StartScreenOpen() && e.Project != nil }

	e.commands = []EditorCommand{
		{
			ID:          CmdNewProject,
			Label:       "New Project...",
			Description: "Open the Start Screen to create a new project from a template",
			Category:    "File",
			Keywords:    []string{"new", "create", "template", "start", "project"},
			Shortcut:    "mod+n",
			Run: func() error {
				e.SetStartScreenOpen(true)
				return nil
			},
		},
		{
			ID:          CmdOpenProject,
			Label:       "Open Project...",
			Description: "Choose a project folder to open in the editor",
			Category:    "File",
			Keywords:    []string{"open", "folder", "project", "switch"},
			Shortcut:    "mod+o",
			Run:         func() error { return e.BrowseAndOpenProject() },
		},
		{
			ID:          CmdStartScreen,
			Label:       "Start Screen",
			Description: "Show the Start Screen with recent projects, templates, and demo",
			Category:    "File",
			Keywords:    []string{"start", "welcome", "recent", "projects", "demo"},
			Run: func() error {
				e.SetStartScreenOpen(true)
				return nil
			},
		},
		{
			ID:          CmdSave,
			Label:       "Save",
			Description: "Save the open scene and project changes",
			Category:    "File",
			Keywords:    []string{"write", "persist", "yaml"},
			Shortcut:    "mod+s",
			Enabled:     notPlaying,
			Run:         func() error { return e.Save() },
		},
		{
			ID:          CmdUndo,
			Label:       "Undo",
			Description: "Undo the last edit",
			Category:    "Edit",
			Keywords:    []string{"revert", "history"},
			Shortcut:    "mod+z",
			Enabled:     func() bool { return !e.StartScreenOpen() && !e.Playing() && e.History.CanUndo() },
			Run: func() error {
				e.Undo()
				return nil
			},
		},
		{
			ID:          CmdRedo,
			Label:       "Redo",
			Description: "Re-apply the last undone edit",
			Category:    "Edit",
			Keywords:    []string{"repeat", "history"},
			Shortcut:    "mod+shift+z",
			Enabled:     func() bool { return !e.StartScreenOpen() && !e.Playing() && e.History.CanRedo() },
			Run: func() error {
				e.Redo()
				return nil
			},
		},
		{
			ID:                 CmdDelete,
			Label:              "Delete Selected Object",
			Description:        "Remove the currently selected object from the scene",
			Category:           "Edit",
			Keywords:           []string{"remove", "object", "erase"},
			Shortcut:           "delete",
			AlternateShortcuts: []string{"backspace"},
			Enabled: func() bool {
				return !e.StartScreenOpen() && !e.Playing() && (e.SelectedID != "" || e.SelectedRoomID() != "")
			},
			Run: func() error {
				if e.SelectedID != "" {
					return e.DeleteSelected()
				}
				if rid := e.SelectedRoomID(); rid != "" {
					return e.DeleteRoom(rid)
				}
				return nil
			},
		},
		{
			ID:          CmdDuplicate,
			Label:       "Duplicate Selected Object",
			Description: "Clone the currently selected object on the same layer",
			Category:    "Edit",
			Keywords:    []string{"clone", "copy", "object"},
			Shortcut:    "mod+d",
			Enabled:     func() bool { return !e.StartScreenOpen() && !e.Playing() && e.SelectedID != "" },
			Run: func() error {
				_, err := e.DuplicateSelected()
				return err
			},
		},
		{
			ID:          CmdToolSelect,
			Label:       "Tool: Select",
			Description: "Switch to the Select viewport tool",
			Category:    "Tools",
			Keywords:    []string{"select", "pointer", "cursor", "2d"},
			Shortcut:    "v",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolSelect },
			Run: func() error {
				e.SetTool(ToolSelect)
				return nil
			},
		},
		{
			ID:          CmdToolMove,
			Label:       "Tool: Move",
			Description: "Switch to the Move viewport tool",
			Category:    "Tools",
			Keywords:    []string{"move", "translate", "drag", "2d"},
			Shortcut:    "m",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolMove },
			Run: func() error {
				e.SetTool(ToolMove)
				return nil
			},
		},
		{
			ID:          CmdToolPlace,
			Label:       "Tool: Place",
			Description: "Switch to the Place viewport tool: an object in 2D, the selected model in 3D",
			Category:    "Tools",
			Keywords:    []string{"place", "spawn", "add", "object", "model", "2d", "3d"},
			Shortcut:    "p",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.Tool == ToolPlace },
			Run: func() error {
				e.SetTool(ToolPlace)
				return nil
			},
		},
		{
			ID:          CmdToolPaintTile,
			Label:       "Tool: Paint Tile",
			Description: "Switch to the Paint Tile brush",
			Category:    "Tools",
			Keywords:    []string{"paint", "brush", "tile", "tilemap", "2d"},
			Shortcut:    "b",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolPaintTile },
			Run: func() error {
				e.SetTool(ToolPaintTile)
				return nil
			},
		},
		{
			ID:          CmdToolEraseTile,
			Label:       "Tool: Erase Tile",
			Description: "Switch to the Erase Tile tool",
			Category:    "Tools",
			Keywords:    []string{"erase", "clear", "tile", "tilemap", "2d"},
			Shortcut:    "e",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolEraseTile },
			Run: func() error {
				e.SetTool(ToolEraseTile)
				return nil
			},
		},
		{
			ID:          CmdToolFillTile,
			Label:       "Tool: Fill Tile",
			Description: "Switch to the Flood Fill Tile tool",
			Category:    "Tools",
			Keywords:    []string{"fill", "bucket", "flood", "tile", "tilemap", "2d"},
			Shortcut:    "g",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolFillTile },
			Run: func() error {
				e.SetTool(ToolFillTile)
				return nil
			},
		},
		{
			ID:          CmdToolTerrain,
			Label:       "Tool: Terrain",
			Description: "Switch to the Terrain autotile brush",
			Category:    "Tools",
			Keywords:    []string{"terrain", "autotile", "blob", "brush", "tile", "tilemap", "2d"},
			Shortcut:    "t",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolTerrain },
			Run: func() error {
				e.SetTool(ToolTerrain)
				return nil
			},
		},
		{
			ID:          CmdGizmoTranslate,
			Label:       "3D Gizmo: Translate",
			Description: "Switch the 3D transform gizmo to Translate mode",
			Category:    "3D Gizmo",
			Keywords:    []string{"gizmo", "translate", "move", "3d"},
			Shortcut:    "w",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.GizmoMode == Gizmo3DTranslate },
			Run: func() error {
				e.SetGizmoMode3D(Gizmo3DTranslate)
				return nil
			},
		},
		{
			ID:          CmdGizmoRotate,
			Label:       "3D Gizmo: Rotate",
			Description: "Switch the 3D transform gizmo to Rotate mode",
			Category:    "3D Gizmo",
			Keywords:    []string{"gizmo", "rotate", "turn", "3d"},
			Shortcut:    "r",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.GizmoMode == Gizmo3DRotate },
			Run: func() error {
				e.SetGizmoMode3D(Gizmo3DRotate)
				return nil
			},
		},
		{
			ID:          CmdGizmoScale,
			Label:       "3D Gizmo: Scale",
			Description: "Switch the 3D transform gizmo to Scale mode",
			Category:    "3D Gizmo",
			Keywords:    []string{"gizmo", "scale", "resize", "3d"},
			Shortcut:    "s",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.GizmoMode == Gizmo3DScale },
			Run: func() error {
				e.SetGizmoMode3D(Gizmo3DScale)
				return nil
			},
		},
		{
			ID:          CmdView2D,
			Label:       "Switch to 2D View",
			Description: "Show the 2D scene viewport",
			Category:    "View",
			Keywords:    []string{"2d", "viewport", "mode"},
			Shortcut:    "alt+2",
			Enabled:     inEditor,
			Checked:     func() bool { return e.ViewMode == ViewMode2D },
			Run: func() error {
				e.SetViewMode(ViewMode2D)
				return nil
			},
		},
		{
			ID:          CmdView3D,
			Label:       "Switch to 3D View",
			Description: "Show the 3D orbit and gizmo viewport",
			Category:    "View",
			Keywords:    []string{"3d", "viewport", "mode"},
			Shortcut:    "alt+3",
			Enabled:     inEditor,
			Checked:     func() bool { return e.ViewMode == ViewMode3D },
			Run: func() error {
				e.SetViewMode(ViewMode3D)
				return nil
			},
		},
		{
			ID:          CmdShadingLit,
			Label:       "3D Shading: Lit",
			Description: "Light the 3D viewport's geometry with the scene's lights",
			Category:    "View",
			Keywords:    []string{"shading", "lit", "lights", "3d"},
			Checked:     func() bool { return e.Shading3D == Shading3DLit },
			Run: func() error {
				e.SetShading3D(Shading3DLit)
				return nil
			},
		},
		{
			ID:          CmdShadingUnlit,
			Label:       "3D Shading: Unlit",
			Description: "Draw the 3D viewport's geometry at its base colors, without lights",
			Category:    "View",
			Keywords:    []string{"shading", "unlit", "albedo", "flat", "3d"},
			Checked:     func() bool { return e.Shading3D == Shading3DUnlit },
			Run: func() error {
				e.SetShading3D(Shading3DUnlit)
				return nil
			},
		},
		{
			ID:          CmdShadingWire,
			Label:       "3D Shading: Wireframe",
			Description: "Light the 3D viewport's geometry and draw its triangle edges over it",
			Category:    "View",
			Keywords:    []string{"shading", "wireframe", "edges", "triangles", "3d"},
			Checked:     func() bool { return e.Shading3D == Shading3DWireframe },
			Run: func() error {
				e.SetShading3D(Shading3DWireframe)
				return nil
			},
		},
		{
			ID:          CmdNavMeshPanel,
			Label:       "3D NavMesh Bake Panel",
			Description: "Toggle the 3D navigation mesh bake settings panel",
			Category:    "View",
			Keywords:    []string{"navmesh", "navigation", "bake", "3d", "panel"},
			Checked:     func() bool { return e.NavMeshPanelOpen() },
			Run: func() error {
				e.SetNavMeshPanelOpen(!e.NavMeshPanelOpen())
				return nil
			},
		},
		{
			ID:          CmdNavMeshOverlay,
			Label:       "Show 3D NavMesh Overlay",
			Description: "Toggle drawing the baked 3D navigation mesh over the 3D viewport",
			Category:    "View",
			Keywords:    []string{"navmesh", "navigation", "overlay", "3d", "debug"},
			Checked:     func() bool { return e.ShowNavMesh3D },
			Run: func() error {
				e.SetShowNavMesh3D(!e.ShowNavMesh3D)
				return nil
			},
		},
		{
			ID:          CmdBakeNavMesh3D,
			Label:       "Bake 3D NavMesh",
			Description: "Bake a 3D navigation mesh from the open scene's static geometry",
			Category:    "Project",
			Keywords:    []string{"navmesh", "navigation", "bake", "3d", "recast"},
			Enabled:     notPlaying,
			Run: func() error {
				_, err := e.BakeNavMesh3D()
				return err
			},
		},
		{
			ID:          CmdFrameSelection,
			Label:       "Frame Selection",
			Description: "Center the viewport camera on the selected object or scene",
			Category:    "View",
			Keywords:    []string{"focus", "camera", "center", "zoom"},
			Shortcut:    "f",
			Enabled:     inEditor,
			Run: func() error {
				e.FrameSelection()
				return nil
			},
		},
		view3DCommand(e, CmdView3DFront, View3DFront, "1", "front"),
		view3DCommand(e, CmdView3DBack, View3DBack, "ctrl+1", "back", "rear"),
		view3DCommand(e, CmdView3DRight, View3DRight, "3", "right", "side"),
		view3DCommand(e, CmdView3DLeft, View3DLeft, "ctrl+3", "left", "side"),
		view3DCommand(e, CmdView3DTop, View3DTop, "7", "top", "plan"),
		view3DCommand(e, CmdView3DBottom, View3DBottom, "ctrl+7", "bottom", "under"),
		{
			ID:          CmdView3DOrthographic,
			Label:       "3D View: Orthographic",
			Description: "Switch the 3D viewport between perspective and orthographic projection",
			Category:    "View",
			Keywords:    []string{"orthographic", "ortho", "perspective", "projection", "3d"},
			Shortcut:    "5",
			Enabled:     func() bool { return e.ViewMode == ViewMode3D },
			Checked:     func() bool { return e.Camera3D.Orthographic },
			Run: func() error {
				e.SetOrthographic3D(!e.Camera3D.Orthographic)
				return nil
			},
		},
		{
			ID:          CmdToggleLightingPreview,
			Label:       "Toggle 2D Lighting Preview",
			Description: "Show or hide live 2D lights, occluder shadows, and ambient darkness in the 2D viewport",
			Category:    "View",
			Keywords:    []string{"light", "lighting", "shadow", "ambient", "occluder", "preview", "2d"},
			Enabled:     func() bool { return !e.Playing() && e.ViewMode == ViewMode2D },
			Checked:     func() bool { return e.ShowLightingPreview },
			Run: func() error {
				e.SetShowLightingPreview(!e.ShowLightingPreview)
				return nil
			},
		},
		{
			ID:          CmdToggleAllShapes,
			Label:       "Show All Component Shapes",
			Description: "Toggle drawing component shape fields for all visible objects in the 2D viewport",
			Category:    "View",
			Keywords:    []string{"shapes", "collider", "trigger", "polygon", "handles", "2d"},
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ShowAllShapes },
			Run: func() error {
				e.SetShowAllShapes(!e.ShowAllShapes)
				return nil
			},
		},
		{
			ID:          CmdToggleSnapGrid,
			Label:       "Snap to Grid",
			Description: "Toggle grid snapping for 2D viewport shape handle edits",
			Category:    "View",
			Keywords:    []string{"snap", "grid", "shapes", "handles", "2d"},
			Enabled:     notPlaying,
			Checked:     func() bool { return e.SnapToGrid },
			Run: func() error {
				e.SetSnapToGrid(!e.SnapToGrid)
				return nil
			},
		},
		{
			ID:          CmdCommandPalette,
			Label:       "Command Palette",
			Description: "Search and run editor commands, plugin tools, panels, and project files",
			Category:    "View",
			Keywords:    []string{"commands", "search", "quick", "open", "palette"},
			Shortcut:    "mod+k",
			Run: func() error {
				e.ToggleCommandPalette()
				return nil
			},
		},
		{
			ID:          CmdPlayStop,
			Label:       "Play / Stop",
			Description: "Toggle play-in-editor simulation",
			Category:    "Project",
			Keywords:    []string{"play", "stop", "preview", "simulate"},
			Shortcut:    "f5",
			Enabled:     inEditor,
			Run:         func() error { return e.TogglePlay() },
		},
		{
			ID:          CmdRunGame,
			Label:       "Run Game",
			Description: "Launch the game using Editor.OnRunGame, go tool game dev, or play-in-editor",
			Category:    "Project",
			Keywords:    []string{"run", "launch", "start", "game"},
			Shortcut:    "mod+r",
			Enabled:     inEditor,
			Run:         func() error { return e.runGameCommand() },
		},
		{
			ID:          CmdProjectSettings,
			Label:       "Project Settings",
			Description: "Toggle the Project Settings panel",
			Category:    "Project",
			Keywords:    []string{"settings", "game.yaml", "config", "panel"},
			Shortcut:    "mod+,",
			Enabled:     hasProject,
			Checked:     func() bool { return e.ProjectSettingsOpen() },
			Run: func() error {
				e.SetProjectSettingsOpen(!e.ProjectSettingsOpen())
				return nil
			},
		},
		{
			ID:          CmdAudioMixer,
			Label:       "Audio Bus Mixer",
			Description: "Toggle the Audio Bus Mixer panel",
			Category:    "Project",
			Keywords:    []string{"audio", "sound", "mixer", "bus", "panel"},
			Enabled:     hasProject,
			Checked:     func() bool { return e.AudioMixerOpen() },
			Run: func() error {
				e.SetAudioMixerOpen(!e.AudioMixerOpen())
				return nil
			},
		},
		{
			ID:          CmdLocalization,
			Label:       "Localization Strings",
			Description: "Toggle the Localization string table panel",
			Category:    "Project",
			Keywords:    []string{"l10n", "strings", "translations", "locales", "panel"},
			Enabled:     notPlaying,
			Checked:     func() bool { return e.LocalizationOpen() },
			Run: func() error {
				if e.LocalizationOpen() {
					return e.CloseLocalizations()
				}
				return e.openDefaultLocalizations()
			},
		},
		{
			ID:          CmdParticleEditor,
			Label:       "Particle Emitter Editor",
			Description: "Toggle the Particle Emitter Editor panel with live preview",
			Category:    "Project",
			Keywords:    []string{"particles", "emitter", "vfx", "fx", "curves", "panel"},
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ParticleEmitterOpen() },
			Run: func() error {
				if e.ParticleEmitterOpen() {
					return e.CloseParticleEmitter()
				}
				return e.NewParticleEmitter()
			},
		},
		{
			ID:          CmdExport,
			Label:       "Export & Release",
			Description: "Toggle the Export & Release Build panel",
			Category:    "Project",
			Keywords:    []string{"export", "release", "build", "package", "panel"},
			Shortcut:    "mod+shift+e",
			Enabled:     hasProject,
			Checked:     func() bool { return e.ExportPanelOpen() },
			Run: func() error {
				e.SetExportPanelOpen(!e.ExportPanelOpen())
				return nil
			},
		},
		{
			ID:          CmdProfiler,
			Label:       "Profiler",
			Description: "Toggle the Profiler panel for the running game",
			Category:    "Project",
			Keywords:    []string{"profiler", "perf", "performance", "fps", "frame", "zones", "trace", "memory", "gc", "panel"},
			Shortcut:    "mod+shift+m",
			Checked:     func() bool { return e.ProfilerOpen() },
			Run: func() error {
				e.SetProfilerOpen(!e.ProfilerOpen())
				return nil
			},
		},
		{
			ID:          CmdCheckProblems,
			Label:       "Check Problems",
			Description: "Re-run asset and project validation checks",
			Category:    "Project",
			Keywords:    []string{"problems", "diagnostics", "validate", "lint"},
			Shortcut:    "mod+shift+b",
			Enabled:     hasProject,
			Run: func() error {
				e.CheckProblems()
				return nil
			},
		},
	}
}

// registeredShortcutOwners returns a map from platform-resolved shortcut key to the command ID
// that owns it across all built-in, custom, and registered plugin commands.
func (e *Editor) registeredShortcutOwners() map[string]string {
	e.initBuiltinCommands()
	owners := make(map[string]string, len(e.commands)*2)
	add := func(id string, shortcuts ...string) {
		for _, sc := range shortcuts {
			for _, ck := range shortcutCollisionKeys(sc) {
				owners[ck] = id
			}
		}
	}
	for _, c := range e.commands {
		add(c.ID, shortcutsForCommand(c)...)
	}
	for _, p := range e.plugins {
		for _, pc := range p.Commands {
			if pc.Shortcut != "" {
				add(pluginCommandID(p.Name, pc.ID), pc.Shortcut)
			}
		}
	}
	return owners
}

func (e *Editor) hasRegisteredCommandID(id string) bool {
	e.initBuiltinCommands()
	for _, c := range e.commands {
		if c.ID == id {
			return true
		}
	}
	for _, p := range e.plugins {
		for _, pc := range p.Commands {
			if pluginCommandID(p.Name, pc.ID) == id {
				return true
			}
		}
	}
	return false
}

// RegisterCommand adds a custom command to the editor's unified command registry.
// It refuses an empty or invalid ID, missing Label or Run, a duplicate ID, an invalid
// shortcut, or any shortcut that clashes with an existing built-in, custom, or plugin shortcut.
func (e *Editor) RegisterCommand(cmd EditorCommand) error {
	e.initBuiltinCommands()
	if err := validateCommandID(cmd.ID); err != nil {
		return err
	}
	if strings.TrimSpace(cmd.Label) == "" {
		return fmt.Errorf("editor: command %q has no label", cmd.ID)
	}
	if cmd.Run == nil {
		return fmt.Errorf("editor: command %q has no Run function", cmd.ID)
	}
	if e.hasRegisteredCommandID(cmd.ID) {
		return fmt.Errorf("editor: command %q is already registered", cmd.ID)
	}

	normPrimary, err := NormalizeShortcut(cmd.Shortcut)
	if err != nil {
		return fmt.Errorf("editor: command %q: %w", cmd.ID, err)
	}
	cmd.Shortcut = normPrimary

	var normAlts []string
	for _, alt := range cmd.AlternateShortcuts {
		n, err := NormalizeShortcut(alt)
		if err != nil {
			return fmt.Errorf("editor: command %q: %w", cmd.ID, err)
		}
		if n != "" {
			normAlts = append(normAlts, n)
		}
	}
	cmd.AlternateShortcuts = normAlts

	owners := e.registeredShortcutOwners()
	local := make(map[string]bool)
	for _, sc := range shortcutsForCommand(cmd) {
		for _, ck := range shortcutCollisionKeys(sc) {
			if existing, clash := owners[ck]; clash {
				return fmt.Errorf("editor: command %q shortcut %q clashes with command %q", cmd.ID, sc, existing)
			}
			if local[ck] {
				return fmt.Errorf("editor: command %q repeats shortcut %q", cmd.ID, sc)
			}
			local[ck] = true
		}
	}

	cmd.Keywords = slices.Clone(cmd.Keywords)
	e.commands = append(e.commands, cmd)
	e.notify()
	return nil
}

func pluginCommandID(pluginName, cmdID string) string {
	return "plugin." + pluginName + "." + cmdID
}

// Commands returns all currently active commands in deterministic order:
// built-in and custom commands followed by the commands of active plugins.
func (e *Editor) Commands() []EditorCommand {
	e.initBuiltinCommands()
	out := make([]EditorCommand, 0, len(e.commands))
	out = append(out, e.commands...)
	for _, p := range e.activePlugins() {
		for _, pc := range p.Commands {
			cmd := pc
			cat := cmd.Category
			if cat == "" {
				cat = p.Name
			}
			var enabled func() bool
			if cmd.Enabled != nil {
				fn := cmd.Enabled
				enabled = func() bool { return fn(e) }
			}
			runFn := cmd.Run
			keywords := append([]string{p.Name, cmd.ID}, cmd.Keywords...)
			out = append(out, EditorCommand{
				ID:          pluginCommandID(p.Name, cmd.ID),
				Label:       cmd.Label,
				Description: cmd.Description,
				Category:    cat,
				Keywords:    keywords,
				Shortcut:    cmd.Shortcut,
				Enabled:     enabled,
				Run:         func() error { return runFn(e) },
			})
		}
	}
	return out
}

// Command returns the active command with ID id, or false if no active command has that ID.
func (e *Editor) Command(id string) (EditorCommand, bool) {
	for _, c := range e.Commands() {
		if c.ID == id {
			return c, true
		}
	}
	return EditorCommand{}, false
}

// ExecuteCommand runs the active command with ID id.
// It returns ErrCommandNotFound if id is not active and ErrCommandDisabled if the command is disabled.
func (e *Editor) ExecuteCommand(id string) error {
	cmd, ok := e.Command(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrCommandNotFound, id)
	}
	if cmd.Enabled != nil && !cmd.Enabled() {
		return fmt.Errorf("%w: %q", ErrCommandDisabled, id)
	}
	err := cmd.Run()
	if err != nil {
		e.report(err)
	}
	return err
}

// TriggerShortcut finds the active command bound to shortcut and runs it if enabled.
// It returns true if a matching enabled command executed.
func (e *Editor) TriggerShortcut(shortcut string) bool {
	norm, err := NormalizeShortcut(shortcut)
	if err != nil || norm == "" {
		return false
	}
	want := shortcutCollisionKeys(norm)
	for _, cmd := range e.Commands() {
		matched := false
		for _, sc := range shortcutsForCommand(cmd) {
			if sc == norm {
				matched = true
				break
			}
			for _, ck := range shortcutCollisionKeys(sc) {
				if slices.Contains(want, ck) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			continue
		}
		if cmd.Enabled != nil && !cmd.Enabled() {
			return false
		}
		if err := cmd.Run(); err != nil {
			e.report(err)
		}
		return true
	}
	return false
}

// DeleteSelected deletes the currently selected object as an undoable command.
// When nothing is selected, it returns nil without modifying history.
func (e *Editor) DeleteSelected() error {
	if e.playing {
		return ErrPlaying
	}
	if e.SelectedID == "" {
		return nil
	}
	return e.DeleteObject(e.SelectedID)
}

// DuplicateObject clones object id on the same layer with fresh unique IDs for it and its descendants,
// selects the duplicate, and returns its new ID as one undoable command.
func (e *Editor) DuplicateObject(id string) (string, error) {
	if e.playing {
		return "", ErrPlaying
	}
	// A drag in progress is put back first, so the copy is of the object as it was before the drag.
	e.CancelDrag2D()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelCameraDrag()
	obj, li, _ := e.FindObject(id)
	if obj == nil {
		return "", fmt.Errorf("editor: object %q not found", id)
	}
	cloned := cloneObjectDoc(*obj)
	allocated := make(map[string]struct{})
	var assignIDs func(o *sceneformat.ObjectDoc)
	assignIDs = func(o *sceneformat.ObjectDoc) {
		base := o.ID
		if base == "" {
			base = o.Name
		}
		for {
			candidate := e.generateObjectID(base)
			if _, taken := allocated[candidate]; !taken {
				o.ID = candidate
				allocated[candidate] = struct{}{}
				break
			}
		}
		for i := range o.Children {
			assignIDs(&o.Children[i])
		}
	}
	assignIDs(&cloned)
	return e.PlaceObject(e.Doc.Layers[li].Name, cloned)
}

// DuplicateSelected duplicates the currently selected object and returns the duplicate's ID.
func (e *Editor) DuplicateSelected() (string, error) {
	if e.playing {
		return "", ErrPlaying
	}
	if e.SelectedID == "" {
		return "", nil
	}
	return e.DuplicateObject(e.SelectedID)
}

// TogglePlay starts play-in-editor if stopped, or stops it and restores the pre-play snapshot if playing.
func (e *Editor) TogglePlay() error {
	if e.Playing() {
		return e.Stop()
	}
	return e.Play()
}

// runGameCommand backs CmdRunGame: it invokes Editor.OnRunGame when configured,
// otherwise launches `go tool game dev` via RunGame when a project is open,
// otherwise starts play-in-editor when a UI scope is attached.
func (e *Editor) runGameCommand() error {
	if e.OnRunGame != nil {
		if err := e.OnRunGame(); err != nil {
			e.report(err)
			return err
		}
		return nil
	}
	if e.Project != nil && e.ProjectPath != "" {
		return e.RunGame()
	}
	if e.scope != nil {
		if !e.Playing() {
			return e.Play()
		}
		return nil
	}
	err := errors.New("editor: no run handler configured (set Editor.OnRunGame or attach a UI scope)")
	e.report(err)
	return err
}

// FrameSelection centers the active 2D or 3D viewport camera on the selected object,
// or on all scene objects (or the origin) when nothing is selected.
// In 2D mode it delegates to FrameSelection2D, moving the independent editor View2D
// without mutating SceneDoc.Camera.
func (e *Editor) FrameSelection() bool {
	if e == nil || e.Doc == nil {
		return false
	}
	if e.ViewMode == ViewMode3D {
		return e.frameSelection3D()
	}
	return e.FrameSelection2D()
}

func (e *Editor) frameSelection3D() bool {
	e.endFly3D()
	e.scene3D()
	if obj, _ := e.SelectedObject(); obj != nil {
		bmin, bmax := e.objectAABB3D(obj)
		e.frameAABB3D(bmin, bmax)
		e.Status = "Framed " + obj.ID
		e.notify()
		return true
	}
	objs := e.Objects3D()
	if len(objs) == 0 {
		e.Camera3D = DefaultOrbitCamera3D()
		e.Status = "Framed scene"
		e.notify()
		return true
	}
	bmin, bmax := e.objectAABB3D(objs[0].Doc)
	for _, ref := range objs[1:] {
		mn, mx := e.objectAABB3D(ref.Doc)
		for axis := 0; axis < 3; axis++ {
			if mn[axis] < bmin[axis] {
				bmin[axis] = mn[axis]
			}
			if mx[axis] > bmax[axis] {
				bmax[axis] = mx[axis]
			}
		}
	}
	e.frameAABB3D(bmin, bmax)
	e.Status = "Framed scene"
	e.notify()
	return true
}

func (e *Editor) frameAABB3D(bmin, bmax [3]float32) {
	center := [3]float32{
		(bmin[0] + bmax[0]) * 0.5,
		(bmin[1] + bmax[1]) * 0.5,
		(bmin[2] + bmax[2]) * 0.5,
	}
	dx := float64(bmax[0] - bmin[0])
	dy := float64(bmax[1] - bmin[1])
	dz := float64(bmax[2] - bmin[2])
	radius := 0.5 * math.Sqrt(dx*dx+dy*dy+dz*dz)
	_, fov, _, _ := e.Camera3D.effectiveParams()
	var dist float32
	if e.Camera3D.Orthographic {
		// The orthographic view is OrthoHeight high whatever the eye's distance, so the distance is the one whose height holds the bounds' sphere, with the same margin, across the narrower side of the view.
		halfH := radius * 1.4
		if w, h := e.CanvasSize(); w > 0 && h > 0 && w < h {
			halfH *= float64(h / w)
		}
		dist = float32(halfH / math.Tan(fov*0.5))
	} else {
		sinHalf := math.Sin(fov * 0.5)
		if sinHalf < 0.1 {
			sinHalf = 0.5
		}
		dist = float32((radius / sinHalf) * 1.4)
	}
	if dist < 60 {
		dist = 60
	}
	if dist > maxOrbitDistance {
		dist = maxOrbitDistance
	}
	e.Camera3D.Target = center
	e.Camera3D.Distance = dist
}

// view3DCommand is the command that turns the 3D viewport to the axis view v, bound to shortcut; it runs only in the 3D view.
func view3DCommand(e *Editor, id string, v View3D, shortcut string, keywords ...string) EditorCommand {
	return EditorCommand{
		ID:          id,
		Label:       "3D View: " + v.String(),
		Description: "Look at the 3D viewport's target from the " + strings.ToLower(v.String()),
		Category:    "View",
		Keywords:    append([]string{"axis", "view", "camera", "3d"}, keywords...),
		Shortcut:    shortcut,
		Enabled:     func() bool { return e.ViewMode == ViewMode3D },
		Checked: func() bool {
			cur, ok := e.Camera3D.AxisView()
			return ok && cur == v
		},
		Run: func() error {
			e.SetView3D(v)
			return nil
		},
	}
}

// bindShortcuts registers NSGo keyboard bindings for every active command in EditorView.
// Every binding uses core.KeyNotEditing so shortcuts are ignored while any text field has focus,
// and re-binds automatically only when the set of active shortcuts changes (such as when a plugin turns on or off).
func (e *Editor) bindShortcuts(cx *core.Scope, rev *core.Signal[uint64]) {
	sig := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		for _, cmd := range e.Commands() {
			for _, sc := range shortcutsForCommand(cmd) {
				b.WriteString(cmd.ID)
				b.WriteByte(0)
				b.WriteString(sc)
				b.WriteByte(0)
			}
		}
		return b.String()
	})
	cx.Effect(func() {
		_ = sig.Get()
		for _, cmd := range e.Commands() {
			cmdID := cmd.ID
			for _, sc := range shortcutsForCommand(cmd) {
				key := sc
				cx.OnKeyWith(key, core.KeyOptions{When: core.KeyNotEditing}, func() {
					if cmdID == CmdPlayStop && e.Project != nil && e.ProjectPath != "" && !e.Playing() {
						if !e.GameRunning() {
							e.report(e.RunGame())
						}
						return
					}
					active, ok := e.Command(cmdID)
					if !ok {
						return
					}
					if active.Enabled != nil && !active.Enabled() {
						return
					}
					if err := active.Run(); err != nil {
						e.report(err)
					}
				})
			}
		}
	})
}

// Ensure time import is used cleanly when needed by future command helpers.
var _ = time.Second
