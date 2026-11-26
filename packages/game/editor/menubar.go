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
			cmdItem(CmdCloseTab, e.T("menu.closeTab"), false),
			sep,
			cmdItem(CmdOpenInstancedScene, e.T("menu.openInstancedScene"), false),
			cmdItem(CmdOpenBaseScene, e.T("menu.openBaseScene"), false),
			sep,
			cmdItem(CmdEditorSettings, e.T("menu.editorSettings"), false),
			cmdItem(CmdShortcutsSettings, e.T("menu.shortcutsSettings"), false),
			cmdItem(CmdPermissionsSettings, e.T("menu.permissions"), false),
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
			cmdItem(CmdCut, e.T("menu.cut"), false),
			cmdItem(CmdCopy, e.T("menu.copy"), false),
			cmdItem(CmdPaste, e.T("menu.paste"), false),
			cmdItem(CmdPasteInto, e.T("menu.pasteInto"), false),
			cmdItem(CmdDuplicate, e.T("menu.duplicate"), false),
			cmdItem(CmdDelete, e.T("menu.delete"), false),
			sep,
			cmdItem(CmdGroup, e.T("menu.group"), false),
			cmdItem(CmdUngroup, e.T("menu.ungroup"), false),
			sep,
			cmdItem(CmdRevertOverrides, e.T("menu.revertOverrides"), false),
			cmdItem(CmdApplyOverridesToSource, e.T("menu.applyOverridesToSource"), false),
			cmdItem(CmdMakeInstanceLocal, e.T("menu.makeInstanceLocal"), false),
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
		cmdItem(CmdToolNavTestPath, e.T("menu.navTestPath"), true),
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
					e.ensureViewModeForPluginTool(pName, tID)
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
		sep,
		cmdItem(CmdGizmoSpace3D, e.T("cmd.gizmoSpace3d.label"), false),
	}

	// View > 3D Shading submenu
	shadingItems := []desktop.MenuItem{
		cmdItem(CmdShadingLit, e.T("menu.shadingLit"), true),
		cmdItem(CmdShadingUnlit, e.T("menu.shadingUnlit"), true),
		cmdItem(CmdShadingWire, e.T("menu.shadingWireframe"), true),
	}

	// View > Overlays submenu
	var overlayMenuItems []desktop.MenuItem
	for _, ov := range e.ViewportOverlays() {
		ovID := ov.ID
		overlayMenuItems = append(overlayMenuItems, desktop.MenuItem{
			Label:     e.overlayDisplayName(ov),
			Disabled:  e.Playing() || e.ViewMode != ViewMode2D,
			Checkable: true,
			Checked:   e.ViewportOverlayEnabled(ovID),
			When:      core.MenuNotEditing,
			OnSelect: func() {
				if e.Playing() || e.ViewMode != ViewMode2D {
					return
				}
				e.report(e.ToggleViewportOverlay(ovID))
			},
		})
	}

	// View menu
	viewMenu := desktop.MenuItem{
		Label: e.T("menu.view"),
		Items: []desktop.MenuItem{
			cmdItem(CmdAskAgent, e.T("menu.askAgent"), false),
			cmdItem(CmdCommandPalette, e.T("menu.commandPalette"), false),
			cmdItem(CmdShortcutsSheet, e.T("menu.shortcutsSheet"), false),
			cmdItem(CmdToggleTasks, e.T("menu.tasks"), true),
			sep,
			cmdItem(CmdAgentTab, e.T("menu.agentPanel"), false),
			cmdItem(CmdInspectorTab, e.T("menu.inspectorPanel"), false),
			sep,
			cmdItem(CmdView2D, e.T("menu.view2d"), true),
			cmdItem(CmdView3D, e.T("menu.view3d"), true),
			cmdItem(CmdView3DCamera, "Look Through Scene Camera", false),
			cmdItem(CmdEnvironmentPanel, "Scene Environment", false),
			cmdItem(CmdFrameSelection, e.T("menu.frameSelection"), false),
			cmdItem(CmdToggleLightingPreview, e.T("menu.lightingPreview"), false),
			cmdItem(CmdToggleNavMesh2D, e.T("menu.navMesh2dOverlay"), false),
			{Label: e.T("menu.overlays"), Items: overlayMenuItems},
			sep,
			cmdItem(CmdNavMeshOverlay, e.T("menu.navMeshOverlay"), false),
			cmdItem(CmdNavMeshPanel, e.T("menu.navMeshPanel"), false),
			sep,
			cmdItem(CmdToggleAllShapes, e.T("menu.toggleAllShapes"), false),
			cmdItem(CmdToggleShowGrid, e.T("menu.toggleShowGrid"), false),
			cmdItem(CmdToggleSnapGrid, e.T("menu.toggleSnapGrid"), false),
			cmdItem(CmdToggleSnapPixel, e.T("menu.toggleSnapPixel"), false),
			cmdItem(CmdToggleSnapRotation, e.T("menu.toggleSnapRotation"), false),
			cmdItem(CmdToggleSnapScale, e.T("menu.toggleSnapScale"), false),
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
		cmdItem(CmdPlayMark, e.T("menu.playMark"), false),
		cmdItem(CmdAskFrozenFrame, e.T("menu.askFrozenFrame"), false),
		cmdItem(CmdRunGame, e.T("menu.runGame"), false),
		sep,
		cmdItem(CmdProjectSettings, e.T("menu.projectSettings"), false),
		cmdItem(CmdAudioMixer, e.T("menu.audioMixer"), false),
		cmdItem(CmdLocalization, e.T("menu.localization"), false),
		cmdItem(CmdHUDLayout, "HUD & Menu Layout", false),
		cmdItem(CmdProfiler, e.T("menu.profiler"), false),
		cmdItem(CmdParticleEditor, e.T("menu.particleEditor"), false),
		cmdItem(CmdMaterialEditor, e.T("menu.materialEditor"), false),
		cmdItem(CmdBuildRelease, e.T("menu.buildRelease"), false),
		sep,
		cmdItem(CmdFindUsages, e.T("menu.findUsages"), false),
		cmdItem(CmdCheckProblems, e.T("menu.checkProblems"), false),
		cmdItem(CmdGeneratedAssets, e.T("menu.generatedAssets"), false),
		cmdItem(CmdPlaytestReport, e.T("menu.playtestReport"), false),
		cmdItem(CmdChanges, e.T("menu.changes"), false),
		cmdItem(CmdConnectAgent, e.T("menu.connectAgent"), false),
		cmdItem(CmdMergeConflicts, e.T("menu.mergeConflicts"), false),
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
		canUngroup := false
		if e.SelectedID != "" {
			if o, _, _ := e.FindObject(e.SelectedID); o != nil && len(o.Children) > 0 {
				canUngroup = true
			}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s|%v|%s|%v|%d|%v|%v|%v|%v|%v|%v|%v|%s|%v|%v|%v|%v|%v|%v|%d|%v|%v|%v|%d|%d|%d|%d|%d|%s|%s|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%d|%d|%v|%v|%v|%d|%s|%v|%v|%v|%s|%v|%v",
			e.ResolvedLanguage(),
			e.SettingsOpen(),
			e.SettingsPage(),
			e.EditorSettingsOpen(),
			e.EditorSettingsTab(),
			e.ShortcutsSheetOpen(),
			e.PermissionsSettingsOpen(),
			e.StartScreenOpen(),
			e.ChangesOpen(),
			e.MergeConflictOpen(),
			e.PlaytestReportOpen(),
			e.UsagesScreenOpen(),
			e.SelectedFile,
			e.AssetGeneratorOpen(),
			e.Playing(),
			e.RemoteConnected(),
			e.canUndo(),
			e.canRedo(),
			e.SelectedID != "" || e.SelectedRoomID() != "",
			len(e.SelectedIDs()),
			e.allSelectedObjectsAreLocal(),
			e.selectedObjectIsLocal(),
			canUngroup,
			e.ViewMode,
			e.Tool,
			e.GizmoMode,
			e.GizmoSpace3D,
			e.Shading3D,
			e.pluginTool.plugin,
			e.pluginTool.id,
			e.Project != nil,
			e.ProjectSettingsOpen(),
			e.AudioMixerOpen(),
			e.LocalizationOpen(),
			e.ParticleEmitterOpen(),
			e.HUDLayoutOpen(),
			e.MaterialOpen(),
			e.ExportPanelOpen(),
			e.ProfilerOpen(),
			e.ShowLightingPreview,
			e.ShowNavMesh2D,
			e.ShowNavMesh3D,
			e.NavMeshPanelOpen(),
			e.ShowAllShapes,
			e.ShowGrid,
			e.SnapToGrid,
			e.SnapToPixel,
			e.SnapRotation,
			e.SnapScale,
			e.hasTileSel,
			!e.tileClipboard.Empty(),
			e.LookThroughCamera3D(),
			e.HasSceneCamera3D(),
			e.EnvironmentPanelOpen(),
			e.TabCount(),
			e.ActiveTab(),
			e.Doc != nil && e.Doc.Inherits != "",
			e.selectedObjectInstancesScene(),
			e.ObjectHasAnyOverride(e.SelectedID),
			e.InstanceOverrideCount(e.SelectedID),
			e.shortcutsDigest(),
			e.BuildReleaseScreenOpen(),
			e.ConnectDialogOpen(),
			e.AgentPanelOpen(),
			e.AgentMode(),
			e.AskInPlaceOpen(),
			e.TasksPanelOpen(),
		)
		for _, ov := range e.ViewportOverlays() {
			b.WriteByte('|')
			b.WriteString(ov.ID)
			if e.ViewportOverlayEnabled(ov.ID) {
				b.WriteString(":1")
			} else {
				b.WriteString(":0")
			}
		}
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
