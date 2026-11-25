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
		Label: e.T("menu.file"),
		Items: []desktop.MenuItem{
			cmdItem(CmdNewProject, e.T("menu.newProject"), false),
			cmdItem(CmdOpenProject, e.T("menu.openProject"), false),
			cmdItem(CmdStartScreen, e.T("menu.startScreen"), false),
			sep,
			cmdItem(CmdSave, e.T("menu.save"), false),
			cmdItem(CmdCloseTab, "Close Scene Tab", false),
			sep,
			cmdItem(CmdOpenInstancedScene, "Open Instanced Scene", false),
			cmdItem(CmdOpenBaseScene, "Open Base Scene", false),
			sep,
			cmdItem(CmdEditorSettings, e.T("menu.editorSettings"), false),
			sep,
			cmdItem(CmdExport, e.T("menu.export"), false),
		},
	}

	// Edit menu
	editMenu := desktop.MenuItem{
		Label: e.T("menu.edit"),
		Items: []desktop.MenuItem{
			cmdItem(CmdUndo, e.T("menu.undo"), false),
			cmdItem(CmdRedo, e.T("menu.redo"), false),
			sep,
			cmdItem(CmdDuplicate, e.T("menu.duplicate"), false),
			cmdItem(CmdDelete, e.T("menu.delete"), false),
			sep,
			cmdItem(CmdCopyTileRegion, "Copy Tile Region", false),
			cmdItem(CmdCutTileRegion, "Cut Tile Region", false),
			cmdItem(CmdPasteTileRegion, "Paste Tile Region", false),
		},
	}

	// View > Tools submenu (built-in 2D tools + active plugin tools)
	toolItems := []desktop.MenuItem{
		cmdItem(CmdToolSelect, e.T("menu.select"), true),
		cmdItem(CmdToolMove, e.T("menu.move"), true),
		cmdItem(CmdToolPlace, e.T("menu.place"), true),
		cmdItem(CmdToolPaintTile, e.T("menu.paintTile"), true),
		cmdItem(CmdToolEraseTile, e.T("menu.eraseTile"), true),
		cmdItem(CmdToolFillTile, e.T("menu.fillTile"), true),
		cmdItem(CmdToolTerrain, e.T("menu.terrain"), true),
		cmdItem(CmdToolRectTile, "Rectangle Tile", true),
		cmdItem(CmdToolLineTile, "Line Tile", true),
		cmdItem(CmdToolStampTile, "Stamp Tile", true),
		cmdItem(CmdToolRandomTile, "Random Tile", true),
	}
	var pluginToolItems []desktop.MenuItem
	for _, p := range e.activePlugins() {
		for _, t := range p.Tools {
			pName, tID := p.Name, t.ID
			tLabel := e.translatePluginText(p, t.LabelKey, t.Label)
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
		cmdItem(CmdGizmoTranslate, e.T("menu.translate"), true),
		cmdItem(CmdGizmoRotate, e.T("menu.rotate"), true),
		cmdItem(CmdGizmoScale, e.T("menu.scale"), true),
		cmdItem(CmdGizmoCollider, "Collider", true),
	}

	// View > 3D Shading submenu
	shadingItems := []desktop.MenuItem{
		cmdItem(CmdShadingLit, e.T("menu.shadingLit"), true),
		cmdItem(CmdShadingUnlit, e.T("menu.shadingUnlit"), true),
		cmdItem(CmdShadingWire, e.T("menu.shadingWireframe"), true),
	}

	// View menu
	viewMenu := desktop.MenuItem{
		Label: e.T("menu.view"),
		Items: []desktop.MenuItem{
			cmdItem(CmdCommandPalette, e.T("menu.commandPalette"), false),
			sep,
			cmdItem(CmdView2D, e.T("menu.view2d"), true),
			cmdItem(CmdView3D, e.T("menu.view3d"), true),
			cmdItem(CmdView3DCamera, "Look Through Scene Camera", false),
			cmdItem(CmdEnvironmentPanel, "Scene Environment", false),
			cmdItem(CmdFrameSelection, e.T("menu.frameSelection"), false),
			cmdItem(CmdToggleLightingPreview, e.T("menu.lightingPreview"), false),
			sep,
			cmdItem(CmdNavMeshOverlay, e.T("menu.navMeshOverlay"), false),
			cmdItem(CmdNavMeshPanel, e.T("menu.navMeshPanel"), false),
			sep,
			cmdItem(CmdToggleAllShapes, e.T("menu.toggleAllShapes"), false),
			cmdItem(CmdToggleSnapGrid, e.T("menu.toggleSnapGrid"), false),
			sep,
			{Label: e.T("menu.tools"), Items: toolItems},
			{Label: e.T("menu.gizmo3d"), Items: gizmoItems},
			{Label: e.T("menu.shading3d"), Items: shadingItems},
		},
	}

	// Project menu
	playLabel := e.T("menu.play")
	if e.Playing() {
		playLabel = e.T("menu.stop")
	}
	projectItems := []desktop.MenuItem{
		cmdItem(CmdPlayStop, playLabel, false),
		cmdItem(CmdRunGame, e.T("menu.runGame"), false),
		sep,
		cmdItem(CmdProjectSettings, e.T("menu.projectSettings"), false),
		cmdItem(CmdAudioMixer, e.T("menu.audioMixer"), false),
		cmdItem(CmdLocalization, e.T("menu.localization"), false),
		cmdItem(CmdHUDLayout, "HUD & Menu Layout", false),
		cmdItem(CmdProfiler, e.T("menu.profiler"), false),
		cmdItem(CmdParticleEditor, e.T("menu.particleEditor"), false),
		sep,
		cmdItem(CmdCheckProblems, e.T("menu.checkProblems"), false),
	}

	// Expose active plugin commands under Project > Plugins when present.
	var pluginCmdItems []desktop.MenuItem
	for _, p := range e.activePlugins() {
		for _, pc := range p.Commands {
			id := pluginCommandID(p.Name, pc.ID)
			pluginCmdItems = append(pluginCmdItems, cmdItem(id, e.translatePluginText(p, pc.LabelKey, pc.Label), false))
		}
	}
	if len(pluginCmdItems) > 0 {
		projectItems = append(projectItems, sep, desktop.MenuItem{
			Label: e.T("menu.plugins"),
			Items: pluginCmdItems,
		})
	}

	projectMenu := desktop.MenuItem{
		Label: e.T("menu.project"),
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
		fmt.Fprintf(&b, "%s|%v|%v|%v|%v|%v|%v|%d|%d|%d|%d|%s|%s|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%d|%d|%v|%v",
			e.ResolvedLanguage(),
			e.EditorSettingsOpen(),
			e.StartScreenOpen(),
			e.Playing(),
			e.canUndo(),
			e.canRedo(),
			e.SelectedID != "" || e.SelectedRoomID() != "",
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
			e.ParticleEmitterOpen(),
			e.HUDLayoutOpen(),
			e.ExportPanelOpen(),
			e.ProfilerOpen(),
			e.ShowLightingPreview,
			e.ShowNavMesh3D,
			e.NavMeshPanelOpen(),
			e.ShowAllShapes,
			e.SnapToGrid,
			e.hasTileSel,
			!e.tileClipboard.Empty(),
			e.LookThroughCamera3D(),
			e.HasSceneCamera3D(),
			e.EnvironmentPanelOpen(),
			e.TabCount(),
			e.ActiveTab(),
			e.Doc != nil && e.Doc.Inherits != "",
			e.selectedObjectInstancesScene(),
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
