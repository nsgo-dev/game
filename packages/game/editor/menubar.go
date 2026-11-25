package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// ApplicationMenu builds the desktop application menu bar (File, Edit, View, Project)
// directly from the editor's unified command registry, keeping labels, shortcuts,
// enabled/checked states, and actions identical to keyboard shortcuts and the command palette.
// Every shortcut uses core.MenuNotEditing so text fields never lose editing keys.
func (e *Editor) ApplicationMenu() []desktop.MenuItem {
	e.initBuiltinCommands()
	byID := make(map[string]EditorCommand, len(e.commands)+8)
	for _, cmd := range e.Commands() {
		byID[cmd.ID] = cmd
	}

	cmdItem := func(id, labelOverride string, radio bool) desktop.MenuItem {
		cmd, ok := byID[id]
		if !ok {
			return desktop.MenuItem{Label: labelOverride, Disabled: true}
		}
		label := cmd.Label
		if labelOverride != "" {
			label = labelOverride
		}
		disabled := cmd.Enabled != nil && !cmd.Enabled()
		checkable := cmd.Checked != nil
		checked := checkable && cmd.Checked()
		cmdID := cmd.ID
		return desktop.MenuItem{
			Label:     label,
			Shortcut:  cmd.Shortcut,
			Disabled:  disabled,
			Checkable: checkable,
			Checked:   checked,
			Radio:     radio && checkable,
			When:      core.MenuNotEditing,
			OnSelect: func() {
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
			},
		}
	}

	sep := desktop.MenuItem{Separator: true}

	// File menu
	fileMenu := desktop.MenuItem{
		Label: "File",
		Items: []desktop.MenuItem{
			cmdItem(CmdNewProject, "New Project...", false),
			cmdItem(CmdOpenProject, "Open Project...", false),
			cmdItem(CmdStartScreen, "Start Screen", false),
			sep,
			cmdItem(CmdSave, "Save", false),
			sep,
			cmdItem(CmdExport, "Export & Release...", false),
		},
	}

	// Edit menu
	editMenu := desktop.MenuItem{
		Label: "Edit",
		Items: []desktop.MenuItem{
			cmdItem(CmdUndo, "Undo", false),
			cmdItem(CmdRedo, "Redo", false),
			sep,
			cmdItem(CmdDuplicate, "Duplicate", false),
			cmdItem(CmdDelete, "Delete", false),
		},
	}

	// View > Tools submenu (built-in 2D tools + active plugin tools)
	toolItems := []desktop.MenuItem{
		cmdItem(CmdToolSelect, "Select", true),
		cmdItem(CmdToolMove, "Move", true),
		cmdItem(CmdToolPlace, "Place", true),
		cmdItem(CmdToolPaintTile, "Paint Tile", true),
		cmdItem(CmdToolEraseTile, "Erase Tile", true),
		cmdItem(CmdToolFillTile, "Fill Tile", true),
	}
	var pluginToolItems []desktop.MenuItem
	for _, p := range e.activePlugins() {
		for _, t := range p.Tools {
			pName, tID, tLabel := p.Name, t.ID, t.Label
			pluginToolItems = append(pluginToolItems, desktop.MenuItem{
				Label:     tLabel,
				Disabled:  e.Playing(),
				Checkable: true,
				Checked:   e.Tool == ToolPlugin && e.pluginTool == (pluginToolRef{pName, tID}),
				Radio:     true,
				When:      core.MenuNotEditing,
				OnSelect: func() {
					if e.Playing() {
						return
					}
					e.SetViewMode(ViewMode2D)
					e.report(e.SetPluginTool(pName, tID))
				},
			})
		}
	}
	if len(pluginToolItems) > 0 {
		toolItems = append(toolItems, sep)
		toolItems = append(toolItems, pluginToolItems...)
	}

	// View > 3D Gizmo submenu
	gizmoItems := []desktop.MenuItem{
		cmdItem(CmdGizmoTranslate, "Translate", true),
		cmdItem(CmdGizmoRotate, "Rotate", true),
		cmdItem(CmdGizmoScale, "Scale", true),
	}

	// View > 3D Shading submenu
	shadingItems := []desktop.MenuItem{
		cmdItem(CmdShadingLit, "Lit", true),
		cmdItem(CmdShadingUnlit, "Unlit", true),
		cmdItem(CmdShadingWire, "Wireframe", true),
	}

	// View menu
	viewMenu := desktop.MenuItem{
		Label: "View",
		Items: []desktop.MenuItem{
			cmdItem(CmdCommandPalette, "Command Palette...", false),
			sep,
			cmdItem(CmdView2D, "2D View", true),
			cmdItem(CmdView3D, "3D View", true),
			cmdItem(CmdFrameSelection, "Frame Selection", false),
			cmdItem(CmdToggleLightingPreview, "2D Lighting Preview", false),
			sep,
			cmdItem(CmdNavMeshOverlay, "Show 3D NavMesh", false),
			cmdItem(CmdNavMeshPanel, "3D NavMesh Bake...", false),
			sep,
			cmdItem(CmdToggleAllShapes, "Show All Component Shapes", false),
			cmdItem(CmdToggleSnapGrid, "Snap to Grid", false),
			sep,
			{Label: "Tools", Items: toolItems},
			{Label: "3D Gizmo", Items: gizmoItems},
			{Label: "3D Shading", Items: shadingItems},
		},
	}

	// Project menu
	playLabel := "Play"
	if e.Playing() {
		playLabel = "Stop"
	}
	projectItems := []desktop.MenuItem{
		cmdItem(CmdPlayStop, playLabel, false),
		cmdItem(CmdRunGame, "Run Game", false),
		sep,
		cmdItem(CmdProjectSettings, "Project Settings", false),
		cmdItem(CmdAudioMixer, "Audio Bus Mixer", false),
		cmdItem(CmdLocalization, "Localization Strings", false),
		cmdItem(CmdProfiler, "Profiler", false),
		sep,
		cmdItem(CmdCheckProblems, "Check Problems", false),
	}

	// Expose active plugin commands under Project > Plugins when present.
	var pluginCmdItems []desktop.MenuItem
	for _, p := range e.activePlugins() {
		for _, pc := range p.Commands {
			id := pluginCommandID(p.Name, pc.ID)
			pluginCmdItems = append(pluginCmdItems, cmdItem(id, pc.Label, false))
		}
	}
	if len(pluginCmdItems) > 0 {
		projectItems = append(projectItems, sep, desktop.MenuItem{
			Label: "Plugins",
			Items: pluginCmdItems,
		})
	}

	projectMenu := desktop.MenuItem{
		Label: "Project",
		Items: projectItems,
	}

	return []desktop.MenuItem{fileMenu, editMenu, viewMenu, projectMenu}
}

// bindApplicationMenu keeps the desktop application menu synchronized with editor state
// while skipping updates on high-frequency animation frames that do not alter menu state.
func (e *Editor) bindApplicationMenu(cx *core.Scope, rev *core.Signal[uint64]) {
	sig := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		fmt.Fprintf(&b, "%v|%v|%v|%v|%v|%d|%d|%d|%d|%s|%s|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v",
			e.StartScreenOpen(),
			e.Playing(),
			e.History.CanUndo(),
			e.History.CanRedo(),
			e.SelectedID != "",
			e.ViewMode,
			e.Tool,
			e.GizmoMode,
			e.Shading3D,
			e.pluginTool.plugin,
			e.pluginTool.id,
			e.Project != nil,
			e.ProjectSettingsOpen(),
			e.AudioMixerOpen(),
			e.LocalizationOpen(),
			e.ExportPanelOpen(),
			e.ProfilerOpen(),
			e.ShowLightingPreview,
			e.ShowNavMesh3D,
			e.NavMeshPanelOpen(),
			e.ShowAllShapes,
			e.SnapToGrid,
		)
		for _, p := range e.activePlugins() {
			b.WriteByte('|')
			b.WriteString(p.Name)
			for _, t := range p.Tools {
				b.WriteByte(':')
				b.WriteString(t.ID)
			}
			for _, c := range p.Commands {
				b.WriteByte(';')
				b.WriteString(c.ID)
				b.WriteByte('=')
				b.WriteString(c.Shortcut)
				if c.Enabled != nil {
					if c.Enabled(e) {
						b.WriteString(":1")
					} else {
						b.WriteString(":0")
					}
				}
			}
		}
		return b.String()
	})
	cx.Effect(func() {
		_ = sig.Get()
		desktop.SetApplicationMenu(cx, e.ApplicationMenu())
	})
}
