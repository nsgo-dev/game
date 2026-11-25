package editor

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// EditorView returns the root NSGo UI node for ed, composing:
//   - Top toolbar (scene title, the open scene's path with a marker while edits are unsaved, New, Save As and Revert, tool selector, Undo/Redo, Run/Stop Game, Play/Stop, Save)
//   - Scene tree panel listing the layers and their nested objects (SceneTreeView), over the layers panel that picks the active layer and adds, removes, renames, reorders, hides and locks layers (LayersView)
//   - Left Asset Browser panel, with the file browser below it while a project is open (FilesView)
//   - Center interactive Canvas viewport
//   - Tile palette right of the viewport while a tile tool is active in 2D (TilePaletteView)
//   - Right Property Inspector panel
//   - The library panel, when Editor.Library is set (LibraryView)
//   - The panels of the plugins that are on (Plugin), docked left or right of the viewport or below it
//   - Animation panel below them while a clip is open (AnimationPanelView)
//   - Project Settings panel below them while open (ProjectSettingsPanelView)
//   - Search & References panel below them when toggled or showing references (SearchPanelView)
//   - Problems panel below them while a project is open (ProblemsView)
//   - Output / Log panel below them (OutputPanelView)
//   - Bottom status bar (including the live problem count while a project is open)
//
// Mod+S saves (Editor.Save), and the window title follows the open scene and its unsaved marker (Editor.WindowTitle). Opening a scene from the file browser, the Problems panel or the Search and References panel, New Scene, a user's request to quit and closing the window ask first while the scene or project has unsaved edits (Editor.HasUnsavedChanges): Save saves them and goes on, Discard drops them and goes on, and Cancel keeps them.
func EditorView(cx *core.Scope, ed *Editor) core.Node {
	ed.AttachScope(cx)
	rev := core.NewSignal(cx, ed.Revision())
	unsub := ed.Subscribe(func() {
		rev.Set(ed.Revision())
	})
	cx.OnKeyWith("shift+f5", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.GameRunning() {
			ed.report(ed.StopGame())
		}
	})
	cx.Cleanup(func() {
		unsub()
		ed.stopClipLoop()
		ed.cleanupAudioMixerScope(cx)
		ed.stopExportBuild()
		ed.stopAssetsBuild()
		_ = ed.DisconnectProfiler()
		_ = ed.StopGame()
		if ed.Playing() {
			_ = ed.Stop()
		}
		if ed.scope == cx {
			ed.AttachScope(nil)
		}
	})

	colors := ui.ColorsOf(cx)
	ed.bindShortcuts(cx, rev)
	ed.bindApplicationMenu(cx, rev)
	dirty := unsavedMemo(cx, ed, rev)
	pathMode := core.NewSignal(cx, pathNone)
	pathTarget := core.NewSignal(cx, "")
	files := sceneFileView(cx, ed, rev, dirty, pathMode, pathTarget)
	dragAreaKey.Provide(cx, true)

	return core.View{
		TestID: "scene-editor",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: colors.Surface(),
		},
		Children: append([]core.Node{
			toolbarView(cx, ed, rev, sceneFileButtons(cx, ed, rev, dirty, pathMode, pathTarget)),
			core.View{
				// A zero basis gives the row the height left after the toolbar, the animation panel and the status bar, rather than its content's.
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				// One drag area spans the row, so a model dragged from the asset browser drops on the viewport.
				Children: []core.Node{dragdrop.Area{Children: mainRow(cx, ed, rev)}},
			},
			pluginDockView(cx, ed, rev, DockBottom),
			DataTablePanelView(cx, ed, rev),
			AnimationPanelView(cx, ed, rev),
			ProjectSettingsPanelView(cx, ed, rev),
			AudioMixerPanelView(cx, ed, rev),
			InputMapPanelView(cx, ed, rev),
			LocalizationPanelView(cx, ed, rev),
			SearchPanelView(cx, ed, rev),
			ProblemsView(cx, ed, rev),
			ExportPanelView(cx, ed, rev),
			ProfilerPanelView(cx, ed, rev),
			OutputPanelView(cx, ed, rev),
			statusBarView(cx, ed, rev),
			CommandPaletteView(cx, ed, rev),
		}, files...),
	}
}

// mainRow is the editor's middle row: the scene tree over the layers panel, the asset browser over the file browser, the library panel when the editor has a library, the left plugin dock, the viewport, the inspector and the right plugin dock.
func mainRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) []core.Node {
	row := []core.Node{core.View{
		TestID:   "editor-scene-column",
		Style:    core.Style{Width: core.Pt(220)},
		Children: []core.Node{SceneTreeView(cx, ed, rev), LayersView(cx, ed, rev)},
	}, core.View{
		TestID: "editor-left",
		Style:  core.Style{Width: core.Pt(224)},
		Children: []core.Node{
			// A row stretches the asset browser to the height it gets, which is all of the column without a project and half of it beside the file browser.
			core.View{
				Style:    core.Style{Direction: core.Row, FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{AssetBrowserView(cx, ed, rev)},
			},
			FilesView(cx, ed, rev),
		},
	}}
	if ed.Library != nil {
		row = append(row, LibraryView(cx, ed, rev))
	}
	return append(row,
		pluginDockView(cx, ed, rev, DockLeft),
		core.View{
			TestID: "editor-center-column",
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				Overflow:  core.Hidden,
			},
			Children: []core.Node{
				ViewportView(cx, ed, rev),
				ProfilerStripView(cx, ed, rev),
			},
		},
		TilePaletteView(cx, ed, rev),
		InspectorView(cx, ed, rev),
		pluginDockView(cx, ed, rev, DockRight),
	)
}

func toolbarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], fileButtons []core.Node) core.Node {
	colors := ui.ColorsOf(cx)
	notPlaying := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	toolBtn := func(testID, label string, tool Tool) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     label,
			Disabled: notPlaying,
			On: func() bool {
				_ = rev.Get()
				return ed.Tool == tool
			},
			OnChange: func(on bool) {
				if on {
					ed.SetTool(tool)
				}
			},
		}
	}

	viewModeBtn := func(testID, label string, mode ViewMode) core.Node {
		return ui.ToggleButton{
			TestID: testID,
			Text:   label,
			On: func() bool {
				_ = rev.Get()
				return ed.ViewMode == mode
			},
			OnChange: func(on bool) {
				if on {
					ed.SetViewMode(mode)
				}
			},
		}
	}

	gizmoBtn := func(testID, label string, mode GizmoMode3D) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     label,
			Disabled: notPlaying,
			On: func() bool {
				_ = rev.Get()
				return ed.GizmoMode == mode
			},
			OnChange: func(on bool) {
				if on {
					ed.SetGizmoMode3D(mode)
				}
			},
		}
	}

	shadingBtn := func(testID, label string, mode Shading3D) core.Node {
		return ui.ToggleButton{
			TestID: testID,
			Text:   label,
			On: func() bool {
				_ = rev.Get()
				return ed.Shading3D == mode
			},
			OnChange: func(on bool) {
				if on {
					ed.SetShading3D(mode)
				}
			},
		}
	}

	// The edit tools and the 2D view group make the bar wider than a narrow window; it wraps, moving the file and play buttons to a second row rather than past the window's edge, where they cannot be pressed.
	return core.View{
		TestID: "editor-toolbar",
		Style: core.Style{
			Direction:      core.Row,
			Wrap:           core.Wrap,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.Symmetric(8, 12),
			Gap:            8,
			Background:     colors.Raised(),
			Borders:        core.Edges{Bottom: 1},
			BorderColor:    colors.Line(),
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.Text{
						TestID: "toolbar-scene-name",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.Doc != nil && ed.Doc.Name != "" {
								return ed.Doc.Name
							}
							return "Untitled Scene"
						},
						Style: core.Style{
							FontSize:   14,
							FontWeight: 700,
							Color:      colors.Text(),
						},
					},
					ui.HStack{Gap: 6, Children: fileButtons},
				},
			},
			ui.Toolbar{
				Label: "Editor Tools",
				Children: []core.Node{
					ui.ToolbarGroup{
						Label: "View Mode",
						Children: []core.Node{
							viewModeBtn("view-mode-2d", "2D", ViewMode2D),
							viewModeBtn("view-mode-3d", "3D", ViewMode3D),
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.ViewMode == ViewMode3D
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.HStack{
								Gap: 6,
								Children: []core.Node{
									place3DToolsView(rev, ed, notPlaying),
									ui.ToolbarGroup{
										Label: "3D Gizmo",
										Children: []core.Node{
											gizmoBtn("gizmo-translate", "Translate", Gizmo3DTranslate),
											gizmoBtn("gizmo-rotate", "Rotate", Gizmo3DRotate),
											gizmoBtn("gizmo-scale", "Scale", Gizmo3DScale),
										},
									},
									ui.ToolbarGroup{
										Label: "3D Shading",
										Children: []core.Node{
											shadingBtn("shading-lit", "Lit", Shading3DLit),
											shadingBtn("shading-unlit", "Unlit", Shading3DUnlit),
											shadingBtn("shading-wireframe", "Wireframe", Shading3DWireframe),
										},
									},
								},
							}
						},
						Else: func(cx *core.Scope) core.Node {
							return ui.ToolbarGroup{
								Label: "Tools",
								Children: []core.Node{
									toolBtn("tool-select", "Select", ToolSelect),
									toolBtn("tool-move", "Move", ToolMove),
									toolBtn("tool-place", "Place", ToolPlace),
									toolBtn("tool-paint", "Paint", ToolPaintTile),
									toolBtn("tool-erase", "Erase", ToolEraseTile),
									toolBtn("tool-fill", "Fill", ToolFillTile),
									toolBtn("tool-pick", "Pick", ToolPickTile),
								},
							}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.ViewMode == ViewMode2D
						},
						Then: func(cx *core.Scope) core.Node {
							return pluginToolsView(cx, ed, rev, notPlaying)
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.ViewMode == ViewMode2D
						},
						Then: func(cx *core.Scope) core.Node {
							return view2DToolsView(cx, ed, notPlaying)
						},
					},
				},
			},
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					ui.Button{
						TestID:  "toolbar-undo",
						Text:    "Undo",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || !ed.History.CanUndo()
						},
						OnPress: func() {
							ed.Undo()
						},
					},
					ui.Button{
						TestID:  "toolbar-redo",
						Text:    "Redo",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || !ed.History.CanRedo()
						},
						OnPress: func() {
							ed.Redo()
						},
					},
					ui.Button{
						TestID:   "toolbar-save",
						Text:     "Save",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonOutline,
						Disabled: notPlaying,
						OnPress:  func() { ed.saveFromUI() },
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.Playing()
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:  "toolbar-play",
								Text:    "Stop",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonPrimary,
								OnPress: func() {
									_ = ed.Stop()
								},
							}
						},
						Else: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:  "toolbar-play",
								Text:    "Play",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonPrimary,
								OnPress: func() {
									_ = ed.Play()
								},
							}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.GameRunning()
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:  "toolbar-stop-game",
								Text:    "Stop Game (Running)",
								Label:   "Stop the running game (Shift+F5)",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonDanger,
								OnPress: func() {
									ed.report(ed.StopGame())
								},
							}
						},
						Else: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:  "toolbar-run",
								Text:    "Run",
								Label:   "Run the game with go tool game dev (F5)",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonPrimary,
								Disabled: func() bool {
									_ = rev.Get()
									return ed.Project == nil
								},
								OnPress: func() {
									ed.report(ed.RunGame())
								},
							}
						},
					},
					ui.Button{
						TestID:  "toolbar-palette",
						Text:    "Commands",
						Label:   "Open the Command Palette",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						OnPress: func() {
							ed.ToggleCommandPalette()
						},
					},
					ui.Button{
						TestID:  "toolbar-animate",
						Text:    "Animate",
						Label:   "Animate the selected object in a new clip",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || ed.SelectedID == "" || ed.clip != nil
						},
						OnPress: func() {
							ed.report(ed.NewClip("", "New clip", time.Second))
						},
					},
					ui.ToggleButton{
						TestID: "toolbar-project-settings",
						Text:   "Settings",
						Label:  "Toggle the Project Settings panel",
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Project == nil
						},
						On: func() bool {
							_ = rev.Get()
							return ed.ProjectSettingsOpen()
						},
						OnChange: func(on bool) {
							ed.SetProjectSettingsOpen(on)
						},
					},
					ui.ToggleButton{
						TestID: "toolbar-audio-mixer",
						Text:   "Mixer",
						Label:  "Toggle the Audio Bus Mixer panel",
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Project == nil
						},
						On: func() bool {
							_ = rev.Get()
							return ed.AudioMixerOpen()
						},
						OnChange: func(on bool) {
							ed.SetAudioMixerOpen(on)
						},
					},
					ui.ToggleButton{
						TestID: "toolbar-input-map",
						Text:   "Input",
						Label:  "Toggle the Input Map panel",
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Project == nil
						},
						On: func() bool {
							_ = rev.Get()
							return ed.InputMapOpen()
						},
						OnChange: func(on bool) {
							ed.SetInputMapOpen(on)
						},
					},
					ui.ToggleButton{
						TestID:   "toolbar-l10n",
						Text:     "Strings",
						Disabled: notPlaying,
						On: func() bool {
							_ = rev.Get()
							return ed.LocalizationOpen()
						},
						OnChange: func(on bool) {
							if on {
								ed.report(ed.openDefaultLocalizations())
							} else {
								ed.report(ed.CloseLocalizations())
							}
						},
					},
					ui.Button{
						TestID:  "toolbar-build-assets",
						Text:    "Build Assets",
						Label:   "Run go tool game assets for the open project and reload changed atlases and textures",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Project == nil || ed.Playing() || ed.AssetsBuilding()
						},
						OnPress: func() {
							ed.report(ed.BuildAssets())
						},
					},
					ui.ToggleButton{
						TestID: "toolbar-export",
						Text:   "Export",
						Label:  "Toggle the Export & Release Build panel",
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Project == nil
						},
						On: func() bool {
							_ = rev.Get()
							return ed.ExportPanelOpen()
						},
						OnChange: func(on bool) {
							ed.SetExportPanelOpen(on)
						},
					},
					ui.ToggleButton{
						TestID: "toolbar-profiler",
						Text:   "Profiler",
						Label:  "Toggle the Profiler panel",
						On: func() bool {
							_ = rev.Get()
							return ed.ProfilerOpen()
						},
						OnChange: func(on bool) {
							ed.SetProfilerOpen(on)
						},
					},
					ui.ToggleButton{
						TestID: "toolbar-output",
						Text:   "Output",
						Label:  "Toggle the Output log panel",
						On: func() bool {
							_ = rev.Get()
							return ed.OutputPanelOpen()
						},
						OnChange: func(on bool) {
							ed.SetOutputPanelOpen(on)
						},
					},
					ui.ToggleButton{
						TestID: "toolbar-run-open-scene",
						Text:   "Current Scene",
						Label:  "Start game at the currently open scene",
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Project == nil || ed.GameRunning()
						},
						On: func() bool {
							_ = rev.Get()
							return ed.RunStartAtOpenScene()
						},
						OnChange: func(on bool) {
							ed.SetRunStartAtOpenScene(on)
						},
					},
				},
			},
		},
	}
}

// view2DToolsView is the tool bar's group of icon buttons that frame the 2D view on the selection or on everything, or return it to the scene camera's view; each label names its shortcut as the platform shows it.
func view2DToolsView(cx *core.Scope, ed *Editor, playing func() bool) core.Node {
	btn := func(testID string, icon func() *core.SVG, label string, fn func()) core.Node {
		return ui.Button{
			TestID:   testID,
			Icon:     icon,
			Label:    label,
			Size:     ui.SizeSmall,
			Variant:  ui.ButtonGhost,
			Disabled: playing,
			OnPress:  fn,
		}
	}
	return ui.ToolbarGroup{
		Label: "2D View",
		Children: []core.Node{
			btn("view-frame-selection", lucide.Focus, "Frame Selection ("+cx.KeyLabel("f")+")", func() { ed.FrameSelection2D() }),
			btn("view-frame-all", lucide.Maximize, "Frame All ("+cx.KeyLabel("home")+")", func() { ed.FrameAll2D() }),
			btn("view-reset", lucide.Video, "Scene Camera View ("+cx.KeyLabel("mod+0")+")", func() { ed.ResetView2D() }),
		},
	}
}

func statusBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "editor-status",
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.Symmetric(4, 12),
			Background:     colors.Raised(),
			Borders:        core.Edges{Top: 1},
			BorderColor:    colors.Line(),
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 12,
				Children: []core.Node{
					core.Text{
						TestID: "editor-status-mode",
						ContentFunc: func() string {
							_ = rev.Get()
							mode := "Edit"
							if ed.Playing() {
								mode = "Play"
							}
							if ed.ViewMode == ViewMode3D {
								return fmt.Sprintf("Mode: %s | View: 3D %s | Gizmo: %s | Layer: %s", mode, ed.viewStatus3D(), ed.GizmoMode.String(), ed.activeLayerReadout())
							}
							status := fmt.Sprintf("Mode: %s | View: 2D %.0f%% | Tool: %s | Layer: %s", mode, ed.View2D().Zoom*100, ed.toolLabel(), ed.activeLayerReadout())
							// Erase writes no tile, and a layer without tiles to pick keeps an ID the palette does not list, so only a tile Paint or Fill would write is shown.
							if ed.Tool == ToolPaintTile || ed.Tool == ToolFillTile {
								if tile, ok := ed.TilePalette().Tile(ed.ActiveTileID); ok {
									status += fmt.Sprintf(" | Tile: %d", tile.Number)
								}
							}
							return status
						},
						Style: core.Style{
							FontSize: 11,
							Color:    colors.Muted(),
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.Project != nil && ed.ProjectPath != ""
						},
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID: "status-problems",
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.problemsStatusText()
								},
								Style: core.Style{
									FontSize: 11,
									Color:    colors.Muted(),
								},
							}
						},
					},
				},
			},
			core.Text{
				ContentFunc: func() string {
					_ = rev.Get()
					if ed.BuildErr != nil {
						return "Build error: " + ed.BuildErr.Error()
					}
					return ed.Status
				},
				Style: core.Style{
					FontSize: 11,
					Color:    colors.Muted(),
				},
			},
		},
	}
}
