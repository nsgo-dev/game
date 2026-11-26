package editor

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/nsgo-dev/nsgo/core"
)

// Built-in editor command IDs registered in the unified command registry.
const (
	CmdNewProject             = "file.newProject"
	CmdOpenProject            = "file.openProject"
	CmdStartScreen            = "file.startScreen"
	CmdSave                   = "file.save"
	CmdCloseTab               = "file.closeTab"
	CmdNextTab                = "view.nextTab"
	CmdPrevTab                = "view.prevTab"
	CmdOpenInstancedScene     = "scene.openInstanced"
	CmdOpenBaseScene          = "scene.openBase"
	CmdUndo                   = "edit.undo"
	CmdRedo                   = "edit.redo"
	CmdCopy                   = "edit.copy"
	CmdCut                    = "edit.cut"
	CmdPaste                  = "edit.paste"
	CmdPasteInto              = "edit.pasteInto"
	CmdDelete                 = "edit.delete"
	CmdDuplicate              = "edit.duplicate"
	CmdGroup                  = "edit.group"
	CmdUngroup                = "edit.ungroup"
	CmdToolSelect             = "tool.select"
	CmdToolMove               = "tool.move"
	CmdToolPlace              = "tool.place"
	CmdToolPaintTile          = "tool.paint"
	CmdToolEraseTile          = "tool.erase"
	CmdToolFillTile           = "tool.fill"
	CmdToolTerrain            = "tool.terrain"
	CmdToolRectTile           = "tool.rect"
	CmdToolLineTile           = "tool.line"
	CmdToolStampTile          = "tool.stamp"
	CmdToolRandomTile         = "tool.random"
	CmdCopyTileRegion         = "tile.copy"
	CmdCutTileRegion          = "tile.cut"
	CmdPasteTileRegion        = "tile.paste"
	CmdToolNavTestPath        = "tool.navTestPath"
	CmdGizmoTranslate         = "gizmo.translate"
	CmdGizmoRotate            = "gizmo.rotate"
	CmdGizmoScale             = "gizmo.scale"
	CmdGizmoCollider          = "gizmo.collider"
	CmdFitCollider3D          = "edit.fitCollider3d"
	CmdView2D                 = "view.mode2d"
	CmdView3D                 = "view.mode3d"
	CmdShadingLit             = "view.shading.lit"
	CmdShadingUnlit           = "view.shading.unlit"
	CmdShadingWire            = "view.shading.wireframe"
	CmdNavMeshPanel           = "view.navmesh"
	CmdNavMeshOverlay         = "view.navmesh.overlay"
	CmdBakeNavMesh3D          = "project.bakeNavMesh3D"
	CmdFrameSelection         = "view.frame"
	CmdToggleLightingPreview  = "view.lighting"
	CmdToggleAllShapes        = "view.shapes"
	CmdToggleSnapGrid         = "view.snap"
	CmdToggleShowGrid         = "view.grid"
	CmdToggleSnapPixel        = "view.snap.pixel"
	CmdToggleSnapRotation     = "view.snap.rotation"
	CmdToggleSnapScale        = "view.snap.scale"
	CmdToggleNavMesh2D        = "view.navmesh2d"
	CmdViewportOverlays       = "view.overlays"
	CmdCommandPalette         = "view.palette"
	CmdShortcutsSheet         = "view.shortcuts"
	CmdPlayStop               = "project.play"
	CmdRunGame                = "project.run"
	CmdProjectSettings        = "project.settings"
	CmdAudioMixer             = "project.mixer"
	CmdLocalization           = "project.l10n"
	CmdParticleEditor         = "project.particles"
	CmdHUDLayout              = "project.hud"
	CmdMaterialEditor         = "project.material"
	CmdExport                 = "project.export"
	CmdBuildRelease           = "project.buildRelease"
	CmdProfiler               = "project.profiler"
	CmdCheckProblems          = "project.check"
	CmdFindUsages             = "project.findUsages"
	CmdEditorSettings         = "editor.settings"
	CmdShortcutsSettings      = "editor.shortcuts"
	CmdChanges                = "git.changes"
	CmdMergeConflicts         = "git.mergeConflicts"
	CmdPlaytestReport         = "project.playtestReport"
	CmdToggleAssetsMode       = "view.assets"
	CmdMakeInstanceLocal      = "scene.makeLocal"
	CmdApplyOverridesToSource = "scene.applyOverridesToSource"
	CmdRevertOverrides        = "scene.revertOverrides"
	CmdGeneratedAssets        = "project.generatedAssets"
	CmdConnectAgent           = "agent.connect"
	CmdAgentPanel             = "view.agent"
	CmdPlayMark               = "play.mark"
	CmdAskFrozenFrame         = "play.askFrozenFrame"
	CmdPermissionsSettings    = "settings.permissions"
	// The 3D viewport's axis views and projection: each axis view looks at the camera's Target along a world axis (SetView3D), and the orthographic command switches between perspective and orthographic (SetOrthographic3D).
	CmdView3DFront        = "view.3d.front"
	CmdView3DBack         = "view.3d.back"
	CmdView3DRight        = "view.3d.right"
	CmdView3DLeft         = "view.3d.left"
	CmdView3DTop          = "view.3d.top"
	CmdView3DBottom       = "view.3d.bottom"
	CmdView3DOrthographic = "view.3d.orthographic"
	CmdView3DCamera       = "view.3d.camera"
	CmdGizmoSpace3D       = "gizmo.3d.space"
	CmdEnvironmentPanel   = "scene.environment"
	CmdAddReflectionProbe = "scene.addReflectionProbe"
	CmdAddLightProbeGrid  = "scene.addLightProbeGrid"
	CmdBakeProbes3D       = "project.bakeProbes3D"
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
	// Scope identifies the keyboard scope this command belongs to (for example ScopeGlobal, ScopeViewport, ScopeViewport2D, ScopeViewport3D, ScopeTile, ScopeSceneTree, ScopeAnimation, ScopePlay). Empty defaults to ScopeGlobal.
	Scope string
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
	if strings.IndexFunc(key, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("editor: shortcut %q key %q must not contain spaces", raw, key)
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
	notPlaying := func() bool {
		return !e.StartScreenOpen() && !e.MergeConflictOpen() && !e.ChangesOpen() && !e.PlaytestReportOpen() && !e.UsagesScreenOpen() && !e.Playing()
	}
	inEditor := func() bool {
		return !e.StartScreenOpen() && !e.MergeConflictOpen() && !e.ChangesOpen() && !e.PlaytestReportOpen() && !e.UsagesScreenOpen()
	}
	hasProject := func() bool {
		return !e.StartScreenOpen() && !e.MergeConflictOpen() && !e.ChangesOpen() && !e.PlaytestReportOpen() && !e.UsagesScreenOpen() && e.Project != nil
	}

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
			Description: "Save the active scene and project changes",
			Category:    "File",
			Keywords:    []string{"write", "persist", "yaml"},
			Shortcut:    "mod+s",
			Enabled:     notPlaying,
			Run:         func() error { return e.Save() },
		},
		{
			ID:          CmdCloseTab,
			Label:       "Close Scene Tab",
			Description: "Close the active scene tab, asking first if it has unsaved changes",
			Category:    "File",
			Keywords:    []string{"close", "tab", "scene"},
			Shortcut:    "mod+w",
			Enabled:     notPlaying,
			Run:         func() error { return e.RequestCloseTab(e.ActiveTab()) },
		},
		{
			ID:          CmdOpenInstancedScene,
			Label:       "Open Instanced Scene",
			Description: "Open the scene instanced by the selected object in a tab",
			Category:    "File",
			Keywords:    []string{"open", "instance", "prefab", "scene", "tab"},
			Enabled:     func() bool { return hasProject() && !e.Playing() && e.selectedObjectInstancesScene() },
			Run:         func() error { return e.OpenInstancedScene(e.SelectedID) },
		},
		{
			ID:          CmdOpenBaseScene,
			Label:       "Open Base Scene",
			Description: "Open the base scene inherited by the active scene in a tab",
			Category:    "File",
			Keywords:    []string{"open", "base", "inherits", "parent", "scene", "tab"},
			Enabled:     func() bool { return hasProject() && !e.Playing() && e.Doc != nil && e.Doc.Inherits != "" },
			Run:         func() error { return e.OpenBaseScene() },
		},
		{
			ID:          CmdMakeInstanceLocal,
			Label:       "Make Instance Local",
			Description: "Replace the selected scene instance with a local copy of its resolved objects",
			Category:    "Edit",
			Keywords:    []string{"instance", "prefab", "local", "unpack", "detach", "scene"},
			Enabled:     func() bool { return notPlaying() && e.selectedObjectInstancesScene() },
			Run:         func() error { return e.MakeInstanceLocal(e.SelectedID) },
		},
		{
			ID:          CmdApplyOverridesToSource,
			Label:       "Apply Overrides to Source",
			Description: "Write the selected scene instance's overrides back to its source scene file",
			Category:    "Edit",
			Keywords:    []string{"instance", "prefab", "override", "apply", "source", "save"},
			Enabled: func() bool {
				if !hasProject() || !notPlaying() || e.SelectedID == "" {
					return false
				}
				info := e.ObjectOverrideInfo(e.SelectedID)
				if info.Origin == ObjectInstanced {
					return e.InstanceOverrideCount(info.InstanceID) > 0
				}
				return e.selectedObjectInstancesScene() && e.InstanceOverrideCount(e.SelectedID) > 0
			},
			Run: func() error { return e.ApplyOverridesToSource(e.SelectedID) },
		},
		{
			ID:          CmdRevertOverrides,
			Label:       "Revert All Overrides",
			Description: "Clear all overrides on the selected scene instance, instanced object, or inherited object",
			Category:    "Edit",
			Keywords:    []string{"override", "revert", "reset", "instance", "inherit", "prefab"},
			Enabled: func() bool {
				if !notPlaying() || e.SelectedID == "" {
					return false
				}
				return e.ObjectHasAnyOverride(e.SelectedID) || e.InstanceOverrideCount(e.SelectedID) > 0
			},
			Run: func() error { return e.RevertObjectOverrides(e.SelectedID) },
		},
		{
			ID:          CmdNextTab,
			Label:       "Next Scene Tab",
			Description: "Switch to the next open scene tab",
			Category:    "View",
			Keywords:    []string{"tab", "next", "scene", "switch"},
			Enabled:     func() bool { return notPlaying() && e.TabCount() > 1 },
			Run: func() error {
				n := e.TabCount()
				if n <= 1 {
					return nil
				}
				return e.SelectTab((e.ActiveTab() + 1) % n)
			},
		},
		{
			ID:          CmdPrevTab,
			Label:       "Previous Scene Tab",
			Description: "Switch to the previous open scene tab",
			Category:    "View",
			Keywords:    []string{"tab", "previous", "prev", "scene", "switch"},
			Enabled:     func() bool { return notPlaying() && e.TabCount() > 1 },
			Run: func() error {
				n := e.TabCount()
				if n <= 1 {
					return nil
				}
				return e.SelectTab((e.ActiveTab() + n - 1) % n)
			},
		},
		{
			ID:          CmdUndo,
			Label:       "Undo",
			Description: "Undo the last edit",
			Category:    "Edit",
			Keywords:    []string{"revert", "history"},
			Shortcut:    "mod+z",
			Enabled:     func() bool { return !e.StartScreenOpen() && !e.Playing() && e.canUndo() },
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
			Enabled:     func() bool { return !e.StartScreenOpen() && !e.Playing() && e.canRedo() },
			Run: func() error {
				e.Redo()
				return nil
			},
		},
		{
			ID:          CmdCopy,
			Label:       "Copy",
			Description: "Copy the selected objects or tile region to the clipboard",
			Category:    "Edit",
			Keywords:    []string{"copy", "clipboard", "yaml", "object", "tile"},
			Shortcut:    "mod+c",
			Enabled: func() bool {
				if !notPlaying() {
					return false
				}
				return e.allSelectedObjectsAreLocal() || (e.ViewMode == ViewMode2D && e.hasTileSel)
			},
			Run: func() error {
				if e.ViewMode == ViewMode2D && e.hasTileSel && (isTileTool(e.Tool) || !e.allSelectedObjectsAreLocal()) {
					_, err := e.CopySelectedTileRegion()
					return err
				}
				_, err := e.CopySelected()
				return err
			},
		},
		{
			ID:          CmdCut,
			Label:       "Cut",
			Description: "Cut the selected objects or tile region to the clipboard",
			Category:    "Edit",
			Keywords:    []string{"cut", "clipboard", "yaml", "object", "tile"},
			Shortcut:    "mod+x",
			Enabled: func() bool {
				if !notPlaying() {
					return false
				}
				return e.allSelectedObjectsAreLocal() || (e.ViewMode == ViewMode2D && e.hasTileSel)
			},
			Run: func() error {
				if e.ViewMode == ViewMode2D && e.hasTileSel && (isTileTool(e.Tool) || !e.allSelectedObjectsAreLocal()) {
					_, err := e.CutSelectedTileRegion()
					return err
				}
				_, err := e.CutSelected()
				return err
			},
		},
		{
			ID:          CmdPaste,
			Label:       "Paste",
			Description: "Paste objects from the clipboard onto the active layer, or paste a copied tile region",
			Category:    "Edit",
			Keywords:    []string{"paste", "clipboard", "yaml", "object", "tile"},
			Shortcut:    "mod+v",
			Enabled:     notPlaying,
			Run: func() error {
				if e.ViewMode == ViewMode2D && isTileTool(e.Tool) && !e.tileClipboard.Empty() && e.lastClipboardKind == clipboardTile {
					if col, row, ok := e.HoveredTile(); ok {
						return e.PasteTileRegion(e.ActiveLayer, col, row)
					}
					if sel, ok := e.TileSelection(); ok {
						return e.PasteTileRegion(e.ActiveLayer, sel.Col0, sel.Row0)
					}
					e.activeStamp = e.tileClipboard.Clone()
					e.SetTool(ToolStampTile)
					return nil
				}
				_, err := e.Paste()
				return err
			},
		},
		{
			ID:          CmdPasteInto,
			Label:       "Paste as Child",
			Description: "Paste objects from the clipboard as children of the selected object",
			Category:    "Edit",
			Keywords:    []string{"paste", "child", "parent", "clipboard", "yaml", "object"},
			Shortcut:    "mod+shift+v",
			Enabled:     func() bool { return notPlaying() && e.selectedObjectIsLocal() },
			Run: func() error {
				_, err := e.PasteInto(e.SelectedID)
				return err
			},
		},
		{
			ID:                 CmdDelete,
			Label:              "Delete Selected Object",
			Description:        "Remove the currently selected object from the scene",
			Category:           "Edit",
			Keywords:           []string{"remove", "object", "erase"},
			Scope:              ScopeViewport,
			Shortcut:           "delete",
			AlternateShortcuts: []string{"backspace"},
			Enabled: func() bool {
				return notPlaying() && (e.allSelectedObjectsAreLocal() || e.SelectedRoomID() != "")
			},
			Run: func() error {
				if e.allSelectedObjectsAreLocal() {
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
			Scope:       ScopeViewport,
			Shortcut:    "mod+d",
			Enabled:     func() bool { return notPlaying() && e.allSelectedObjectsAreLocal() },
			Run: func() error {
				_, err := e.DuplicateSelected()
				return err
			},
		},
		{
			ID:          CmdGroup,
			Label:       "Group Selected Objects",
			Description: "Wrap the selected objects under a new empty parent at their common centre",
			Category:    "Edit",
			Keywords:    []string{"group", "parent", "container", "hierarchy", "object"},
			Scope:       ScopeViewport,
			Shortcut:    "mod+g",
			Enabled:     func() bool { return notPlaying() && e.allSelectedObjectsAreLocal() },
			Run: func() error {
				_, err := e.GroupSelected()
				return err
			},
		},
		{
			ID:          CmdUngroup,
			Label:       "Ungroup Selected Object",
			Description: "Lift the selected object's children out while keeping their world transforms",
			Category:    "Edit",
			Keywords:    []string{"ungroup", "unparent", "children", "hierarchy", "object"},
			Scope:       ScopeViewport,
			Shortcut:    "mod+shift+g",
			Enabled: func() bool {
				if !notPlaying() || e.SelectedID == "" {
					return false
				}
				o, _, _ := e.FindObject(e.SelectedID)
				return o != nil && len(o.Children) > 0
			},
			Run: func() error {
				_, err := e.UngroupSelected()
				return err
			},
		},
		{
			ID:          CmdToolSelect,
			Label:       "Tool: Select",
			Description: "Switch to the Select viewport tool",
			Category:    "Tools",
			Keywords:    []string{"select", "pointer", "cursor", "2d"},
			Scope:       ScopeViewport,
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
			Scope:       ScopeViewport,
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
			Scope:       ScopeViewport,
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
			Scope:       ScopeTile,
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
			Scope:       ScopeTile,
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
			Scope:       ScopeTile,
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
			Scope:       ScopeTile,
			Shortcut:    "t",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolTerrain },
			Run: func() error {
				e.SetTool(ToolTerrain)
				return nil
			},
		},
		{
			ID:          CmdToolRectTile,
			Label:       "Tool: Rectangle Tile",
			Description: "Switch to the Rectangle Tile brush (drag to fill, right-drag to erase)",
			Category:    "Tools",
			Keywords:    []string{"rect", "rectangle", "box", "tile", "tilemap", "2d"},
			Scope:       ScopeTile,
			Shortcut:    "u",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolRectTile },
			Run: func() error {
				e.SetTool(ToolRectTile)
				return nil
			},
		},
		{
			ID:          CmdToolLineTile,
			Label:       "Tool: Line Tile",
			Description: "Switch to the Line Tile brush (Bresenham line; right-drag to erase)",
			Category:    "Tools",
			Keywords:    []string{"line", "bresenham", "tile", "tilemap", "2d"},
			Scope:       ScopeTile,
			Shortcut:    "l",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolLineTile },
			Run: func() error {
				e.SetTool(ToolLineTile)
				return nil
			},
		},
		{
			ID:          CmdToolStampTile,
			Label:       "Tool: Stamp Tile",
			Description: "Switch to the Stamp Tile brush (paint patterns, right-drag to select region)",
			Category:    "Tools",
			Keywords:    []string{"stamp", "pattern", "paste", "tile", "tilemap", "2d"},
			Scope:       ScopeTile,
			Shortcut:    "c",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolStampTile },
			Run: func() error {
				e.SetTool(ToolStampTile)
				return nil
			},
		},
		{
			ID:          CmdToolRandomTile,
			Label:       "Tool: Random Tile",
			Description: "Switch to the Random Tile brush (deterministic weighted tile pick)",
			Category:    "Tools",
			Keywords:    []string{"random", "scatter", "weighted", "seed", "tile", "tilemap", "2d"},
			Scope:       ScopeTile,
			Shortcut:    "n",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolRandomTile },
			Run: func() error {
				e.SetTool(ToolRandomTile)
				return nil
			},
		},
		{
			ID:          CmdCopyTileRegion,
			Label:       "Copy Tile Region",
			Description: "Copy the selected tilemap region to the tile clipboard",
			Category:    "Edit",
			Keywords:    []string{"copy", "tile", "region", "stamp", "clipboard"},
			Scope:       ScopeTile,
			Enabled:     func() bool { return notPlaying() && e.ViewMode == ViewMode2D && e.hasTileSel },
			Run: func() error {
				_, err := e.CopySelectedTileRegion()
				return err
			},
		},
		{
			ID:          CmdCutTileRegion,
			Label:       "Cut Tile Region",
			Description: "Cut the selected tilemap region to the tile clipboard",
			Category:    "Edit",
			Keywords:    []string{"cut", "tile", "region", "stamp", "clipboard"},
			Scope:       ScopeTile,
			Enabled:     func() bool { return notPlaying() && e.ViewMode == ViewMode2D && e.hasTileSel },
			Run: func() error {
				_, err := e.CutSelectedTileRegion()
				return err
			},
		},
		{
			ID:          CmdPasteTileRegion,
			Label:       "Paste Tile Region",
			Description: "Paste the tile clipboard at the hovered cell or activate it as the Stamp brush",
			Category:    "Edit",
			Keywords:    []string{"paste", "tile", "region", "stamp", "clipboard"},
			Scope:       ScopeTile,
			Enabled:     func() bool { return notPlaying() && e.ViewMode == ViewMode2D && !e.tileClipboard.Empty() },
			Run: func() error {
				if col, row, ok := e.HoveredTile(); ok {
					return e.PasteTileRegion(e.ActiveLayer, col, row)
				}
				if sel, ok := e.TileSelection(); ok {
					return e.PasteTileRegion(e.ActiveLayer, sel.Col0, sel.Row0)
				}
				e.activeStamp = e.tileClipboard.Clone()
				e.SetTool(ToolStampTile)
				return nil
			},
		},
		{
			ID:          CmdToolNavTestPath,
			Label:       "Tool: Test Nav Path",
			Description: "Press start and end points in the 2D viewport to preview the path ai.NavMesh finds",
			Category:    "Tools",
			Keywords:    []string{"nav", "navmesh", "path", "test", "preview", "ai", "2d"},
			Scope:       ScopeViewport2D,
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ViewMode == ViewMode2D && e.Tool == ToolNavTestPath },
			Run: func() error {
				e.SetViewMode(ViewMode2D)
				e.SetTool(ToolNavTestPath)
				return nil
			},
		},
		{
			ID:          CmdGizmoTranslate,
			Label:       "3D Gizmo: Translate",
			Description: "Switch the 3D transform gizmo to Translate mode",
			Category:    "3D Gizmo",
			Keywords:    []string{"gizmo", "translate", "move", "3d"},
			Scope:       ScopeViewport3D,
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
			Scope:       ScopeViewport3D,
			Shortcut:    "e",
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
			Scope:       ScopeViewport3D,
			Shortcut:    "r",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.GizmoMode == Gizmo3DScale },
			Run: func() error {
				e.SetGizmoMode3D(Gizmo3DScale)
				return nil
			},
		},
		{
			ID:          CmdGizmoCollider,
			Label:       "3D Gizmo: Collider",
			Description: "Switch the 3D gizmo to Collider size handles",
			Category:    "3D Gizmo",
			Keywords:    []string{"gizmo", "collider", "physics", "handles", "size", "3d"},
			Scope:       ScopeViewport3D,
			Shortcut:    "shift+c",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.GizmoMode == Gizmo3DCollider },
			Run: func() error {
				e.SetGizmoMode3D(Gizmo3DCollider)
				return nil
			},
		},
		{
			ID:          CmdFitCollider3D,
			Label:       "Fit 3D Collider to Model Bounds",
			Description: "Fit the selected object's 3D collider to its mesh or glTF model bounds",
			Category:    "3D Gizmo",
			Keywords:    []string{"collider", "fit", "bounds", "model", "physics", "3d"},
			Scope:       ScopeViewport3D,
			Enabled: func() bool {
				if !notPlaying() || e.SelectedID == "" {
					return false
				}
				o, _, _ := e.FindObject(e.SelectedID)
				return o != nil && (o.Model != "" || o.Mesh != "")
			},
			Run: func() error {
				return e.FitCollider3DToBounds(e.SelectedID)
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
			Scope:       ScopeViewport3D,
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
			Scope:       ScopeViewport3D,
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
			Scope:       ScopeViewport3D,
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
			Scope:       ScopeViewport3D,
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
			Scope:       ScopeViewport3D,
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
			Scope:       ScopeViewport,
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
			Scope:       ScopeViewport3D,
			Shortcut:    "5",
			Enabled:     func() bool { return e.ViewMode == ViewMode3D },
			Checked:     func() bool { return e.Camera3D.Orthographic },
			Run: func() error {
				e.SetOrthographic3D(!e.Camera3D.Orthographic)
				return nil
			},
		},
		{
			ID:          CmdGizmoSpace3D,
			Label:       "3D Gizmo: Local / Global Space",
			Description: "Toggle the 3D transform gizmo between local object axes and global world axes",
			Category:    "3D Gizmo",
			Keywords:    []string{"local", "global", "world", "space", "gizmo", "axes", "orientation", "3d"},
			Scope:       ScopeViewport3D,
			Shortcut:    "t",
			Enabled:     func() bool { return !e.Playing() && e.ViewMode == ViewMode3D },
			Checked:     func() bool { return e.GizmoSpace3D == GizmoSpaceLocal },
			Run: func() error {
				e.ToggleGizmoSpace3D()
				return nil
			},
		},
		{
			ID:          CmdToggleLightingPreview,
			Label:       "Toggle 2D Lighting Preview",
			Description: "Show or hide live 2D lights, occluder shadows, and ambient darkness in the 2D viewport",
			Category:    "View",
			Keywords:    []string{"light", "lighting", "shadow", "ambient", "occluder", "preview", "2d"},
			Scope:       ScopeViewport2D,
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
			Scope:       ScopeViewport2D,
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
			Description: "Toggle grid snapping for object placement, drags, 2D handles, and 3D translate gizmos",
			Category:    "View",
			Keywords:    []string{"snap", "grid", "align", "handles", "2d", "3d"},
			Scope:       ScopeViewport2D,
			Enabled:     notPlaying,
			Checked:     func() bool { return e.SnapToGrid },
			Run: func() error {
				e.SetSnapToGrid(!e.SnapToGrid)
				return nil
			},
		},
		{
			ID:          CmdToggleShowGrid,
			Label:       "Show Snap Grid",
			Description: "Toggle the 2D snap grid overlay in the 2D viewport",
			Category:    "View",
			Keywords:    []string{"grid", "overlay", "snap", "lines", "2d"},
			Scope:       ScopeViewport2D,
			Enabled:     notPlaying,
			Checked:     func() bool { return e.ShowGrid },
			Run: func() error {
				e.SetShowGrid(!e.ShowGrid)
				return nil
			},
		},
		{
			ID:          CmdToggleSnapPixel,
			Label:       "Snap to Pixels",
			Description: "Toggle whole-point (pixel) snapping for object placement and drags",
			Category:    "View",
			Keywords:    []string{"snap", "pixel", "integer", "whole", "2d"},
			Scope:       ScopeViewport2D,
			Enabled:     notPlaying,
			Checked:     func() bool { return e.SnapToPixel },
			Run: func() error {
				e.SetSnapToPixel(!e.SnapToPixel)
				return nil
			},
		},
		{
			ID:          CmdToggleSnapRotation,
			Label:       "Snap Rotation",
			Description: "Toggle rotation step snapping for 2D and 3D rotation drags",
			Category:    "View",
			Keywords:    []string{"snap", "rotation", "angle", "degrees", "2d", "3d"},
			Scope:       ScopeViewport,
			Enabled:     notPlaying,
			Checked:     func() bool { return e.SnapRotation },
			Run: func() error {
				e.SetSnapRotation(!e.SnapRotation)
				return nil
			},
		},
		{
			ID:          CmdToggleSnapScale,
			Label:       "Snap Scale",
			Description: "Toggle scale step snapping for 2D and 3D scale drags",
			Category:    "View",
			Keywords:    []string{"snap", "scale", "resize", "step", "2d", "3d"},
			Scope:       ScopeViewport,
			Enabled:     notPlaying,
			Checked:     func() bool { return e.SnapScale },
			Run: func() error {
				e.SetSnapScale(!e.SnapScale)
				return nil
			},
		},
		{
			ID:          CmdToggleAssetsMode,
			Label:       "Asset Drawer",
			Description: "Switch the left rail between Scene and Assets drawer mode",
			Category:    "View",
			Keywords:    []string{"assets", "drawer", "sprites", "audio", "fonts", "tiles", "unused", "missing"},
			Scope:       ScopeGlobal,
			Enabled:     inEditor,
			Checked:     func() bool { return e.LeftRailMode() == LeftRailAssets },
			Run: func() error {
				if e.LeftRailMode() == LeftRailAssets {
					e.SetLeftRailMode(LeftRailScene)
				} else {
					e.SetLeftRailMode(LeftRailAssets)
				}
				return nil
			},
		},
		{
			ID:          CmdView3DCamera,
			Label:       "3D View: Scene Camera",
			Description: "Look through the scene's active 3D camera",
			Category:    "View",
			Keywords:    []string{"camera", "scene", "look", "through", "3d", "preview"},
			Scope:       ScopeViewport3D,
			Shortcut:    "0",
			Enabled:     func() bool { return e.ViewMode == ViewMode3D && (e.LookThroughCamera3D() || e.HasSceneCamera3D()) },
			Checked:     func() bool { return e.LookThroughCamera3D() },
			Run: func() error {
				return e.SetLookThroughCamera3D(!e.LookThroughCamera3D())
			},
		},
		{
			ID:          CmdEnvironmentPanel,
			Label:       "Scene Environment",
			Description: "Toggle the Scene Environment settings panel (sky, ambient, fog, tonemapping)",
			Category:    "View",
			Keywords:    []string{"environment", "sky", "fog", "ibl", "tonemapping", "exposure", "lighting", "3d"},
			Enabled:     func() bool { return e.Doc != nil },
			Checked:     func() bool { return e.EnvironmentPanelOpen() },
			Run: func() error {
				e.SetEnvironmentPanelOpen(!e.EnvironmentPanelOpen())
				return nil
			},
		},
		{
			ID:          CmdToggleNavMesh2D,
			Label:       "Toggle 2D NavMesh Overlay",
			Description: "Show or hide the built 2D navigation mesh polygons, portals, and region outlines in the 2D viewport",
			Category:    "View",
			Keywords:    []string{"nav", "navmesh", "navigation", "region", "overlay", "2d"},
			Scope:       ScopeViewport2D,
			Enabled:     func() bool { return !e.Playing() && e.ViewMode == ViewMode2D },
			Checked:     func() bool { return e.ShowNavMesh2D },
			Run: func() error {
				e.SetShowNavMesh2D(!e.ShowNavMesh2D)
				return nil
			},
		},
		{
			ID:          CmdViewportOverlays,
			Label:       "Viewport Overlays",
			Description: "Open or close the 2D viewport Overlays popover (collision shapes, triggers, connections, patrol paths, navigation, camera bounds, labels)",
			Category:    "View",
			Keywords:    []string{"overlays", "collision", "triggers", "connections", "patrol", "navigation", "camera", "bounds", "labels", "2d"},
			Scope:       ScopeViewport2D,
			Enabled:     func() bool { return !e.Playing() && e.ViewMode == ViewMode2D },
			Checked:     func() bool { return e.OverlaysPopoverOpen() },
			Run: func() error {
				e.ToggleOverlaysPopover()
				return nil
			},
		},
		{
			ID:          CmdAddReflectionProbe,
			Label:       "Add Reflection Probe",
			Description: "Place a 3D reflection probe on the active world layer",
			Category:    "Edit",
			Keywords:    []string{"reflection", "probe", "cubemap", "ibl", "3d", "bake"},
			Scope:       ScopeViewport3D,
			Enabled:     func() bool { return notPlaying() && e.Doc != nil },
			Run: func() error {
				_, err := e.AddReflectionProbe3D()
				return err
			},
		},
		{
			ID:          CmdAddLightProbeGrid,
			Label:       "Add Light Probe Grid",
			Description: "Place a 3D light probe grid on the active world layer",
			Category:    "Edit",
			Keywords:    []string{"light", "probe", "grid", "irradiance", "sh", "gi", "3d", "bake"},
			Scope:       ScopeViewport3D,
			Enabled:     func() bool { return notPlaying() && e.Doc != nil },
			Run: func() error {
				_, err := e.AddLightProbeGrid3D()
				return err
			},
		},
		{
			ID:          CmdBakeProbes3D,
			Label:       "Bake Reflection & Light Probes",
			Description: "Bake all reflection probe cubemaps and light probe grids in the scene",
			Category:    "Project",
			Keywords:    []string{"bake", "reflection", "light", "probe", "cubemap", "sh", "hdr", "ibl", "3d"},
			Scope:       ScopeViewport3D,
			Enabled:     func() bool { return notPlaying() && e.HasProbes3D() },
			Run: func() error {
				_, err := e.BakeAllProbes3D()
				return err
			},
		},
		{
			ID:          CmdCommandPalette,
			Label:       "Command Palette",
			Description: "Search and run editor commands, plugin tools, panels, and project files",
			Category:    "View",
			Keywords:    []string{"commands", "search", "quick", "open", "palette"},
			Scope:       ScopeGlobal,
			Shortcut:    "mod+k",
			Run: func() error {
				e.ToggleCommandPalette()
				return nil
			},
		},
		{
			ID:          CmdShortcutsSheet,
			Label:       "Keyboard Shortcuts",
			Description: "Show the keyboard shortcuts reference sheet grouped by scope",
			Category:    "View",
			Keywords:    []string{"shortcuts", "keybindings", "keys", "cheatsheet", "help"},
			Scope:       ScopeGlobal,
			Shortcut:    "mod+/",
			Checked:     func() bool { return e.ShortcutsSheetOpen() },
			Run: func() error {
				e.ToggleShortcutsSheet()
				return nil
			},
		},
		{
			ID:          CmdPlayStop,
			Label:       "Play / Stop",
			Description: "Toggle play-in-editor simulation",
			Category:    "Project",
			Keywords:    []string{"play", "stop", "preview", "simulate"},
			Scope:       ScopePlay,
			Shortcut:    "f5",
			Enabled:     inEditor,
			Run:         func() error { return e.TogglePlay() },
		},
		{
			ID:          CmdPlayMark,
			Label:       "Mark Play Moment",
			Description: "Add a mark at the current Play frame with the rolling 60 s input replay attached",
			Category:    "Project",
			Keywords:    []string{"play", "mark", "flag", "replay", "moment"},
			Shortcut:    "alt+f",
			Enabled:     func() bool { return inEditor() && (e.Playing() || e.RemoteConnected()) },
			Run: func() error {
				_, err := e.AddPlayMark()
				return err
			},
		},
		{
			ID:          CmdAskFrozenFrame,
			Label:       "Something Looks Wrong",
			Description: "Pause Play mode and ask the agent about the frozen frame with replay and live state attached",
			Category:    "Project",
			Keywords:    []string{"play", "ask", "agent", "freeze", "frozen", "frame", "bug", "replay"},
			Shortcut:    "mod+l",
			Enabled:     func() bool { return inEditor() && (e.Playing() || e.RemoteConnected()) },
			Run:         func() error { return e.OpenAskFrozenFrame() },
		},
		{
			ID:          CmdRunGame,
			Label:       "Run Game",
			Description: "Launch the game using Editor.OnRunGame, go tool game dev, or play-in-editor",
			Category:    "Project",
			Keywords:    []string{"run", "launch", "start", "game"},
			Scope:       ScopePlay,
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
			ID:          CmdHUDLayout,
			Label:       "HUD & Menu Layout",
			Description: "Toggle the multi-size, multi-language HUD and menu layout screen",
			Category:    "Project",
			Keywords:    []string{"hud", "menu", "ui", "screen", "layout", "safe area", "anchors", "locale", "overflow"},
			Shortcut:    "mod+shift+u",
			Enabled:     notPlaying,
			Checked:     func() bool { return e.HUDLayoutOpen() },
			Run: func() error {
				e.SetHUDLayoutOpen(!e.HUDLayoutOpen())
				return nil
			},
		},
		{
			ID:          CmdMaterialEditor,
			Label:       "Material Editor",
			Description: "Toggle the Material Editor panel with live 3D and 2D preview",
			Category:    "Project",
			Keywords:    []string{"material", "pbr", "roughness", "metallic", "color", "texture", "surface", "panel"},
			Enabled:     notPlaying,
			Checked:     func() bool { return e.MaterialOpen() },
			Run: func() error {
				if e.MaterialOpen() {
					return e.CloseMaterial()
				}
				if obj, _ := e.SelectedObject(); obj != nil && obj.Material != "" {
					return e.OpenMaterial(obj.Material)
				}
				return e.NewMaterial()
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
			ID:          CmdBuildRelease,
			Label:       "Build & Release Screen",
			Description: "Open the Build and Release screen with readiness, pre-build checks, and release size comparison",
			Category:    "Project",
			Keywords:    []string{"build", "release", "export", "readiness", "signing", "budgets", "presets", "publish"},
			Shortcut:    "mod+shift+r",
			Scope:       ScopeGlobal,
			Enabled: func() bool {
				return !e.StartScreenOpen() && !e.MergeConflictOpen() && !e.Playing() && (e.BuildReleaseScreenOpen() || e.Project != nil)
			},
			Checked: func() bool { return e.BuildReleaseScreenOpen() },
			Run: func() error {
				if e.BuildReleaseScreenOpen() {
					e.CloseBuildReleaseScreen()
					return nil
				}
				return e.OpenBuildReleaseScreen()
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
		{
			ID:          CmdFindUsages,
			Label:       "Find Usages",
			Description: "Find every usage of the selected prefab, scene, or file across scenes, variants, data tables, and Go code, and rename it safely in one review",
			Category:    "Project",
			Keywords:    []string{"usages", "references", "find", "rename", "refactor", "prefab", "scene"},
			Scope:       ScopeGlobal,
			Shortcut:    "shift+f12",
			Enabled: func() bool {
				if e.StartScreenOpen() || e.MergeConflictOpen() || e.PlaytestReportOpen() || e.HUDLayoutOpen() || e.Playing() || e.Project == nil {
					return false
				}
				if e.UsagesScreenOpen() {
					return true
				}
				target, _ := e.findUsagesTarget()
				return target != ""
			},
			Checked: func() bool { return e.UsagesScreenOpen() },
			Run: func() error {
				return e.OpenUsagesScreen("")
			},
		},
		{
			ID:          CmdEditorSettings,
			Label:       "Editor Settings",
			Description: "Configure editor language, appearance, and keyboard shortcuts",
			Category:    "File",
			Keywords:    []string{"preferences", "language", "chinese", "english", "appearance", "theme", "dark", "light", "shortcuts"},
			Scope:       ScopeGlobal,
			Shortcut:    "mod+shift+,",
			Checked:     func() bool { return e.EditorSettingsOpen() },
			Run: func() error {
				e.ToggleEditorSettings()
				return nil
			},
		},
		{
			ID:          CmdShortcutsSettings,
			Label:       "Settings: Keyboard Shortcuts",
			Description: "Search, rebind, swap, and reset keyboard shortcuts per scope",
			Category:    "File",
			Keywords:    []string{"shortcuts", "keybindings", "rebind", "keys", "preferences", "settings"},
			Scope:       ScopeGlobal,
			Checked:     func() bool { return e.EditorSettingsOpen() && e.EditorSettingsTab() == EditorSettingsTabShortcuts },
			Run: func() error {
				e.OpenShortcutsSettings()
				return nil
			},
		},
		{
			ID:          CmdPermissionsSettings,
			Label:       "Permissions",
			Description: "Configure agent MCP permission profiles and first-match-wins rules",
			Category:    "Project",
			Keywords:    []string{"permissions", "rules", "profile", "cautious", "balanced", "autonomous", "mcp", "agent", "settings"},
			Checked:     func() bool { return e.PermissionsSettingsOpen() },
			Run: func() error {
				e.SetPermissionsSettingsOpen(!e.PermissionsSettingsOpen())
				return nil
			},
		},
		{
			ID:          CmdChanges,
			Label:       "Changes & Commit",
			Description: "Compare modified scenes, data, code, and assets against HEAD and commit with a drafted message",
			Category:    "Project",
			Keywords:    []string{"changes", "git", "diff", "commit", "push", "pull", "branch", "pr"},
			Scope:       ScopeGlobal,
			Enabled: func() bool {
				return !e.StartScreenOpen() && !e.MergeConflictOpen() && !e.Playing() && (e.ChangesOpen() || e.Project != nil || e.FilePath != "")
			},
			Checked: func() bool { return e.ChangesOpen() },
			Run: func() error {
				if e.ChangesOpen() {
					e.CloseChangesPanel()
					return nil
				}
				return e.OpenChangesPanel()
			},
		},
		{
			ID:          CmdConnectAgent,
			Label:       "Connect an Agent",
			Description: "Connect Claude Code, Codex, or another MCP client to this project",
			Category:    "Project",
			Keywords:    []string{"agent", "mcp", "claude", "codex", "connect", "install"},
			Scope:       ScopeGlobal,
			Checked:     func() bool { return e.ConnectDialogOpen() },
			Run: func() error {
				if e.ConnectDialogOpen() {
					e.CloseConnectDialog()
					return nil
				}
				e.OpenConnectDialog()
				return nil
			},
		},
		{
			ID:          CmdAgentPanel,
			Label:       "Agent Panel",
			Description: "Toggle the right sidebar between the Agent panel and the Inspector",
			Category:    "View",
			Keywords:    []string{"agent", "chat", "composer", "mcp", "inspector"},
			Scope:       ScopeGlobal,
			Enabled: func() bool {
				return !e.StartScreenOpen() && !e.SettingsOpen() && !e.AssetGeneratorOpen() && !e.MergeConflictOpen() && !e.ChangesOpen() && !e.BuildReleaseScreenOpen() && !e.UsagesScreenOpen() && !e.PlaytestReportOpen() && !e.HUDLayoutOpen() && e.AgentMode() != AgentModeNone
			},
			Checked: func() bool { return e.AgentPanelOpen() },
			Run: func() error {
				e.SetAgentPanelOpen(!e.AgentPanelOpen())
				return nil
			},
		},
		{
			ID:          CmdMergeConflicts,
			Label:       "Resolve Scene Merge Conflicts",
			Description: "Open the object-by-object scene merge conflict resolver for an active Git merge",
			Category:    "Project",
			Keywords:    []string{"merge", "conflict", "git", "resolve", "scene", "agent"},
			Enabled:     func() bool { return !e.Playing() && (e.MergeConflictOpen() || e.Project != nil || e.FilePath != "") },
			Checked:     func() bool { return e.MergeConflictOpen() },
			Run: func() error {
				if e.MergeConflictOpen() {
					e.CloseMergeConflict()
					return nil
				}
				return e.OpenMergeConflict("")
			},
		},
		{
			ID:          CmdPlaytestReport,
			Label:       "Playtest Report",
			Description: "Open the Playtest Report screen with goals met or missed, charts, and the agent's read",
			Category:    "Project",
			Keywords:    []string{"playtest", "report", "bot", "goals", "metrics", "chart", "heatmap", "replay"},
			Scope:       ScopeGlobal,
			Shortcut:    "mod+shift+y",
			Enabled:     func() bool { return !e.StartScreenOpen() && !e.MergeConflictOpen() && !e.Playing() },
			Checked:     func() bool { return e.PlaytestReportOpen() },
			Run: func() error {
				e.SetPlaytestReportOpen(!e.PlaytestReportOpen())
				return nil
			},
		},
		{
			ID:          CmdGeneratedAssets,
			Label:       "Generated Assets",
			Description: "Generate image and sound candidates via MCP generator servers, inspect style and licenses, and pick a candidate",
			Category:    "Project",
			Keywords:    []string{"generate", "assets", "mcp", "candidates", "license", "source", "brief", "sprite", "sound"},
			Enabled:     func() bool { return !e.StartScreenOpen() && !e.MergeConflictOpen() && !e.Playing() },
			Checked:     func() bool { return e.AssetGeneratorOpen() },
			Run: func() error {
				e.SetAssetGeneratorOpen(!e.AssetGeneratorOpen())
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

// registeredShortcutOwnersInScope returns a map from platform-resolved shortcut key to the command ID
// that owns it within scope across all built-in, custom, and registered plugin commands.
func (e *Editor) registeredShortcutOwnersInScope(scope string) map[string]string {
	e.initBuiltinCommands()
	wantScope := normalizeCommandScope(scope)
	owners := make(map[string]string, len(e.commands))
	add := func(id string, shortcuts ...string) {
		for _, sc := range shortcuts {
			for _, ck := range shortcutCollisionKeys(sc) {
				owners[ck] = id
			}
		}
	}
	for _, c := range e.commands {
		if normalizeCommandScope(c.Scope) != wantScope {
			continue
		}
		add(c.ID, shortcutsForCommand(c)...)
	}
	for _, p := range e.plugins {
		for _, pc := range p.Commands {
			if pc.Shortcut != "" && normalizeCommandScope(pc.Scope) == wantScope {
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

	explicitScope := strings.TrimSpace(cmd.Scope) != ""
	cmd.Scope = normalizeCommandScope(cmd.Scope)
	owners := e.registeredShortcutOwners()
	if explicitScope {
		owners = e.registeredShortcutOwnersInScope(cmd.Scope)
	}
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

// builtinCommandKeys maps built-in command IDs to their localization keys for label, description and category.
var builtinCommandKeys = map[string][3]string{
	CmdNewProject:            {"cmd.newProject.label", "cmd.newProject.desc", "cmd.category.file"},
	CmdOpenProject:           {"cmd.openProject.label", "cmd.openProject.desc", "cmd.category.file"},
	CmdStartScreen:           {"cmd.startScreen.label", "cmd.startScreen.desc", "cmd.category.file"},
	CmdSave:                  {"cmd.save.label", "cmd.save.desc", "cmd.category.file"},
	CmdUndo:                  {"cmd.undo.label", "cmd.undo.desc", "cmd.category.edit"},
	CmdRedo:                  {"cmd.redo.label", "cmd.redo.desc", "cmd.category.edit"},
	CmdCopy:                  {"cmd.copy.label", "cmd.copy.desc", "cmd.category.edit"},
	CmdCut:                   {"cmd.cut.label", "cmd.cut.desc", "cmd.category.edit"},
	CmdPaste:                 {"cmd.paste.label", "cmd.paste.desc", "cmd.category.edit"},
	CmdPasteInto:             {"cmd.pasteInto.label", "cmd.pasteInto.desc", "cmd.category.edit"},
	CmdDelete:                {"cmd.delete.label", "cmd.delete.desc", "cmd.category.edit"},
	CmdDuplicate:             {"cmd.duplicate.label", "cmd.duplicate.desc", "cmd.category.edit"},
	CmdGroup:                 {"cmd.group.label", "cmd.group.desc", "cmd.category.edit"},
	CmdUngroup:               {"cmd.ungroup.label", "cmd.ungroup.desc", "cmd.category.edit"},
	CmdToolSelect:            {"cmd.toolSelect.label", "cmd.toolSelect.desc", "cmd.category.tools"},
	CmdToolMove:              {"cmd.toolMove.label", "cmd.toolMove.desc", "cmd.category.tools"},
	CmdToolPlace:             {"cmd.toolPlace.label", "cmd.toolPlace.desc", "cmd.category.tools"},
	CmdToolPaintTile:         {"cmd.toolPaintTile.label", "cmd.toolPaintTile.desc", "cmd.category.tools"},
	CmdToolEraseTile:         {"cmd.toolEraseTile.label", "cmd.toolEraseTile.desc", "cmd.category.tools"},
	CmdToolFillTile:          {"cmd.toolFillTile.label", "cmd.toolFillTile.desc", "cmd.category.tools"},
	CmdToolNavTestPath:       {"cmd.toolNavTestPath.label", "cmd.toolNavTestPath.desc", "cmd.category.tools"},
	CmdGizmoTranslate:        {"cmd.gizmoTranslate.label", "cmd.gizmoTranslate.desc", "cmd.category.gizmo3d"},
	CmdGizmoRotate:           {"cmd.gizmoRotate.label", "cmd.gizmoRotate.desc", "cmd.category.gizmo3d"},
	CmdGizmoScale:            {"cmd.gizmoScale.label", "cmd.gizmoScale.desc", "cmd.category.gizmo3d"},
	CmdGizmoSpace3D:          {"cmd.gizmoSpace3d.label", "cmd.gizmoSpace3d.desc", "cmd.category.gizmo3d"},
	CmdView2D:                {"cmd.view2d.label", "cmd.view2d.desc", "cmd.category.view"},
	CmdView3D:                {"cmd.view3d.label", "cmd.view3d.desc", "cmd.category.view"},
	CmdShadingLit:            {"cmd.shadingLit.label", "cmd.shadingLit.desc", "cmd.category.view"},
	CmdShadingUnlit:          {"cmd.shadingUnlit.label", "cmd.shadingUnlit.desc", "cmd.category.view"},
	CmdShadingWire:           {"cmd.shadingWire.label", "cmd.shadingWire.desc", "cmd.category.view"},
	CmdNavMeshPanel:          {"cmd.navMeshPanel.label", "cmd.navMeshPanel.desc", "cmd.category.view"},
	CmdNavMeshOverlay:        {"cmd.navMeshOverlay.label", "cmd.navMeshOverlay.desc", "cmd.category.view"},
	CmdBakeNavMesh3D:         {"cmd.bakeNavMesh3d.label", "cmd.bakeNavMesh3d.desc", "cmd.category.project"},
	CmdFrameSelection:        {"cmd.frameSelection.label", "cmd.frameSelection.desc", "cmd.category.view"},
	CmdView3DOrthographic:    {"cmd.view3dOrtho.label", "cmd.view3dOrtho.desc", "cmd.category.view"},
	CmdToggleLightingPreview: {"cmd.lightingPreview.label", "cmd.lightingPreview.desc", "cmd.category.view"},
	CmdToggleNavMesh2D:       {"cmd.navMesh2dOverlay.label", "cmd.navMesh2dOverlay.desc", "cmd.category.view"},
	CmdCommandPalette:        {"cmd.commandPalette.label", "cmd.commandPalette.desc", "cmd.category.view"},
	CmdShortcutsSheet:        {"cmd.shortcutsSheet.label", "cmd.shortcutsSheet.desc", "cmd.category.view"},
	CmdPlayStop:              {"cmd.playStop.label", "cmd.playStop.desc", "cmd.category.project"},
	CmdPlayMark:              {"cmd.playMark.label", "cmd.playMark.desc", "cmd.category.project"},
	CmdAskFrozenFrame:        {"cmd.askFrozenFrame.label", "cmd.askFrozenFrame.desc", "cmd.category.project"},
	CmdRunGame:               {"cmd.runGame.label", "cmd.runGame.desc", "cmd.category.project"},
	CmdProjectSettings:       {"cmd.projectSettings.label", "cmd.projectSettings.desc", "cmd.category.project"},
	CmdAudioMixer:            {"cmd.audioMixer.label", "cmd.audioMixer.desc", "cmd.category.project"},
	CmdLocalization:          {"cmd.localization.label", "cmd.localization.desc", "cmd.category.project"},
	CmdMaterialEditor:        {"cmd.materialEditor.label", "cmd.materialEditor.desc", "cmd.category.project"},
	CmdExport:                {"cmd.export.label", "cmd.export.desc", "cmd.category.project"},
	CmdBuildRelease:          {"cmd.buildRelease.label", "cmd.buildRelease.desc", "cmd.category.project"},
	CmdProfiler:              {"cmd.profiler.label", "cmd.profiler.desc", "cmd.category.project"},
	CmdCheckProblems:         {"cmd.checkProblems.label", "cmd.checkProblems.desc", "cmd.category.project"},
	CmdFindUsages:            {"cmd.findUsages.label", "cmd.findUsages.desc", "cmd.category.project"},
	CmdEditorSettings:        {"cmd.editorSettings.label", "cmd.editorSettings.desc", "cmd.category.file"},
	CmdShortcutsSettings:     {"cmd.shortcutsSettings.label", "cmd.shortcutsSettings.desc", "cmd.category.file"},
	CmdConnectAgent:          {"cmd.connectAgent.label", "cmd.connectAgent.desc", "cmd.category.project"},
	CmdAgentPanel:            {"cmd.agentPanel.label", "cmd.agentPanel.desc", "cmd.category.view"},
	CmdPermissionsSettings:   {"cmd.permissionsSettings.label", "cmd.permissionsSettings.desc", "cmd.category.project"},
	CmdMergeConflicts:        {"cmd.mergeConflicts.label", "cmd.mergeConflicts.desc", "cmd.category.project"},
	CmdToggleAssetsMode:      {"cmd.assetDrawer.label", "cmd.assetDrawer.desc", "cmd.category.view"},
}

// Commands returns all currently active commands in deterministic order:
// built-in and custom commands followed by the commands of active plugins,
// with each command's normalized Scope and effective user shortcut applied.
func (e *Editor) Commands() []EditorCommand {
	e.initBuiltinCommands()
	out := make([]EditorCommand, 0, len(e.commands))
	for _, c := range e.commands {
		if keys, ok := builtinCommandKeys[c.ID]; ok {
			c.Label = e.T(keys[0])
			c.Description = e.T(keys[1])
			c.Category = e.T(keys[2])
		}
		c.Scope = normalizeCommandScope(c.Scope)
		if e.settings.Shortcuts != nil {
			if custom, overridden := e.settings.Shortcuts[c.ID]; overridden {
				c.Shortcut = custom
				c.AlternateShortcuts = nil
			}
		}
		out = append(out, c)
	}
	for _, p := range e.activePlugins() {
		for _, pc := range p.Commands {
			cmd := pc
			fullID := pluginCommandID(p.Name, cmd.ID)
			cat := e.translatePluginText(p, cmd.CategoryKey, cmd.Category)
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
			sc := cmd.Shortcut
			if e.settings.Shortcuts != nil {
				if custom, overridden := e.settings.Shortcuts[fullID]; overridden {
					sc = custom
				}
			}
			out = append(out, EditorCommand{
				ID:          fullID,
				Label:       e.translatePluginText(p, cmd.LabelKey, cmd.Label),
				Description: e.translatePluginText(p, cmd.DescriptionKey, cmd.Description),
				Category:    cat,
				Keywords:    keywords,
				Scope:       normalizeCommandScope(cmd.Scope),
				Shortcut:    sc,
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

// TriggerShortcut finds the highest-priority enabled active command bound to shortcut and runs it.
// It returns true if a matching enabled command executed.
func (e *Editor) TriggerShortcut(shortcut string) bool {
	norm, err := NormalizeShortcut(shortcut)
	if err != nil || norm == "" {
		return false
	}
	cmd, ok := e.resolveShortcutCommand(norm)
	if !ok {
		return false
	}
	if err := cmd.Run(); err != nil {
		e.report(err)
	}
	return true
}

// DeleteSelected deletes the currently selected object(s) as one undoable step.
// When nothing is selected, it returns nil without modifying history.
func (e *Editor) DeleteSelected() error {
	if e.playing {
		return ErrPlaying
	}
	ids := e.topSelectedIDs()
	if len(ids) == 0 {
		return nil
	}
	if len(ids) == 1 {
		return e.DeleteObject(ids[0])
	}
	e.cancelActiveObjectDrags()
	roots, err := e.normalizeObjectSelection(ids)
	if err != nil {
		return err
	}
	if len(roots) == 1 {
		return e.DeleteObject(roots[0])
	}
	label := fmt.Sprintf("Delete %d objects", len(roots))
	return e.deleteRootObjects(roots, label, fmt.Sprintf("Deleted %d objects", len(roots)))
}

// DuplicateObject clones object id on the same layer (and under the same parent) next to
// the original with fresh unique IDs for it and its descendants, rewriting internal connections
// and overrides to the new IDs, selects the duplicate, and returns its new ID as one undoable command.
func (e *Editor) DuplicateObject(id string) (string, error) {
	ids, err := e.DuplicateObjects(id)
	if err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0], nil
}

// DuplicateSelected duplicates the currently selected object(s) in one undoable step
// and returns the primary duplicate's ID.
func (e *Editor) DuplicateSelected() (string, error) {
	if e.playing {
		return "", ErrPlaying
	}
	ids := e.topSelectedIDs()
	if len(ids) == 0 {
		return "", nil
	}
	dups, err := e.DuplicateObjects(ids...)
	if err != nil || len(dups) == 0 {
		return "", err
	}
	return dups[len(dups)-1], nil
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
	if sel := e.SelectedObjects(); len(sel) > 0 {
		bmin, bmax := e.objectAABB3D(sel[0])
		for _, o := range sel[1:] {
			mn, mx := e.objectAABB3D(o)
			for axis := range 3 {
				if mn[axis] < bmin[axis] {
					bmin[axis] = mn[axis]
				}
				if mx[axis] > bmax[axis] {
					bmax[axis] = mx[axis]
				}
			}
		}
		e.frameAABB3D(bmin, bmax)
		if len(sel) == 1 {
			e.Status = "Framed " + sel[0].ID
		} else {
			e.Status = fmt.Sprintf("Framed %d objects", len(sel))
		}
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
		Scope:       ScopeViewport3D,
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
// deduplicates keys shared across different scopes with scope-aware priority dispatch,
// and re-binds automatically whenever active plugins or user shortcut overrides change.
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
		seen := make(map[string]struct{})
		for _, cmd := range e.Commands() {
			for _, sc := range shortcutsForCommand(cmd) {
				if _, dup := seen[sc]; dup {
					continue
				}
				seen[sc] = struct{}{}
				key := sc
				cx.OnKeyWith(key, core.KeyOptions{When: core.KeyNotEditing}, func() {
					if e.shortcutCaptureActive {
						return
					}
					active, ok := e.resolveShortcutCommand(key)
					if !ok {
						return
					}
					if active.ID == CmdToolPaintTile && key == "b" && e.TileReviewOpen() && e.ViewMode == ViewMode2D && !e.Playing() {
						return
					}
					if active.ID == CmdPlayStop && e.Project != nil && e.ProjectPath != "" && !e.Playing() {
						if !e.GameRunning() {
							e.report(e.RunGame())
						}
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
