package editor

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/l10n"
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
	cx.OnKeyWith("mod+shift+p", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if !ed.RemoteConnected() {
			return
		}
		snap := ed.remoteSnapshotShared()
		paused := snap.Loop != nil && snap.Loop.Paused
		ed.dispatchRemoteAction(cx, func() error {
			if paused {
				return ed.RemoteResume()
			}
			return ed.RemotePause()
		})
	})
	cx.OnKeyWith(".", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if !ed.RemoteConnected() {
			return
		}
		snap := ed.remoteSnapshotShared()
		if snap.Loop != nil && snap.Loop.Paused {
			ed.dispatchRemoteAction(cx, func() error {
				_, err := ed.RemoteStep(1)
				return err
			})
		}
	})
	cx.Cleanup(func() {
		unsub()
		ed.stopClipLoop()
		ed.stopStatesLoop()
		ed.stopParticlePreviewLoop()
		ed.stopSceneEmitterLoop()
		ed.stopRigLoop()
		ed.cleanupAudioMixerScope(cx)
		ed.stopExportBuild()
		ed.stopAssetsBuild()
		_ = ed.DisconnectProfiler()
		ed.cancelCreateProject()
		_ = ed.DisconnectRemote()
		_ = ed.StopGame()
		if ed.Playing() {
			_ = ed.Stop()
		}
		if ed.scope == cx {
			ed.AttachScope(nil)
		}
	})

	colors := ui.ColorsOf(cx)
	appearanceMemo := core.NewMemo(cx, func() core.Appearance {
		_ = rev.Get()
		return ed.Appearance().CoreAppearance()
	})
	cx.Effect(func() {
		cx.SetAppearance(appearanceMemo.Get())
	})
	ed.bindShortcuts(cx, rev)
	ed.bindApplicationMenu(cx, rev)
	dirty := unsavedMemo(cx, ed, rev)
	prompt := core.NewSignal[*unsavedPrompt](cx, nil)
	pathMode := core.NewSignal(cx, pathNone)
	pathTarget := core.NewSignal(cx, "")
	bindSceneFileHooks(cx, ed, rev, dirty, prompt)
	dragAreaKey.Provide(cx, true)

	type chromeState struct {
		lang string
		dark bool
	}
	chromeKey := core.NewMemo(cx, func() chromeState {
		_ = rev.Get()
		return chromeState{
			lang: ed.ResolvedLanguage(),
			dark: cx.Env().Dark(),
		}
	})

	workspace := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = chromeKey.Get()
		files := sceneFileDialogs(ed, rev, prompt, pathMode, pathTarget)
		return core.View{
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				Width:     core.Pct(100),
				Height:    core.Pct(100),
			},
			Children: append([]core.Node{
				core.Show{
					When: func() bool {
						_ = rev.Get()
						return ed.StartScreenOpen()
					},
					Then: func(cx *core.Scope) core.Node {
						return StartScreenView(cx, ed, rev)
					},
					Else: func(cx *core.Scope) core.Node {
						return core.View{
							Style: core.Style{
								Width:  core.Pct(100),
								Height: core.Pct(100),
							},
							Children: []core.Node{
								toolbarView(cx, ed, rev, sceneFileButtons(cx, ed, rev, dirty, pathMode, pathTarget)),
								remotePlayBarView(cx, ed, rev),
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
								TilesetEditorPanelView(cx, ed, rev),
								TerrainPanelView(cx, ed, rev),
								RigPanelView(cx, ed, rev),
								DataTablePanelView(cx, ed, rev),
								ParticleEditorPanelView(cx, ed, rev),
								AnimationPanelView(cx, ed, rev),
								StatesPanelView(cx, ed, rev),
								NavMeshPanelView(cx, ed, rev),
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
							},
						}
					},
				},
				CommandPaletteView(cx, ed, rev),
			}, files...),
		}
	})

	return core.View{
		TestID: "scene-editor",
		Style: core.Style{
			Width:  core.Pct(100),
			Height: core.Pct(100),
		},
		StyleFunc: func() core.Style {
			_ = chromeKey.Get()
			return core.Style{Background: colors.Surface()}
		},
		Children: []core.Node{
			workspace,
			EditorSettingsDialogView(cx, ed, rev),
		},
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
		GridMapPaletteView(cx, ed, rev),
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
				return ed.GizmoMode == mode && ed.GridMapTool() == GridMapToolNone
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

	gridMapBtn := func(testID, label string, tool GridMapTool) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     label,
			Disabled: notPlaying,
			On: func() bool {
				_ = rev.Get()
				return ed.GridMapTool() == tool
			},
			OnChange: func(on bool) {
				if on {
					ed.SetGridMapTool(tool)
				} else if ed.GridMapTool() == tool {
					ed.SetGridMapTool(GridMapToolNone)
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
			Padding:        core.Symmetric(4, 10),
			Gap:            4,
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
							return ed.T("scene.untitledScene")
						},
						Style: core.Style{
							FontSize:   14,
							FontWeight: 700,
							Color:      colors.Text(),
						},
					},
					ui.HStack{Gap: 6, Children: fileButtons},
					ui.Button{
						TestID:  "toolbar-editor-settings",
						Icon:    lucide.Settings,
						Label:   ed.T("toolbar.editorSettingsLabel"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						OnPress: func() {
							ed.ToggleEditorSettings()
						},
					},
				},
			},
			ui.Toolbar{
				Label: ed.T("toolbar.label"),
				Children: []core.Node{
					ui.ToolbarGroup{
						Label: ed.T("toolbar.group.viewMode"),
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
										Label: ed.T("toolbar.group.gizmo3d"),
										Children: []core.Node{
											gizmoBtn("gizmo-translate", ed.T("toolbar.translate"), Gizmo3DTranslate),
											gizmoBtn("gizmo-rotate", ed.T("toolbar.rotate"), Gizmo3DRotate),
											gizmoBtn("gizmo-scale", ed.T("toolbar.scale"), Gizmo3DScale),
										},
									},
									ui.ToolbarGroup{
										Label: ed.T("toolbar.group.shading3d"),
										Children: []core.Node{
											shadingBtn("shading-lit", ed.T("toolbar.shadingLit"), Shading3DLit),
											shadingBtn("shading-unlit", ed.T("toolbar.shadingUnlit"), Shading3DUnlit),
											shadingBtn("shading-wireframe", ed.T("toolbar.shadingWireframe"), Shading3DWireframe),
										},
									},
									ui.ToolbarGroup{
										Label: "3D Overlays",
										Children: []core.Node{
											ui.ToggleButton{
												TestID: "viewport3d-toggle-lod",
												Text:   "LOD",
												Label:  "Toggle active LOD indicators in the 3D viewport",
												On: func() bool {
													_ = rev.Get()
													return ed.ShowLODOverlay
												},
												OnChange: ed.SetShowLODOverlay,
											},
											ui.ToggleButton{
												TestID: "viewport3d-toggle-occluders",
												Text:   "Occluders",
												Label:  "Toggle occluder volume visualization in the 3D viewport",
												On: func() bool {
													_ = rev.Get()
													return ed.ShowOccluders
												},
												OnChange: ed.SetShowOccluders,
											},
											ui.ToggleButton{
												TestID: "viewport3d-toggle-stats",
												Text:   "Stats",
												Label:  "Toggle RenderStats overlay in the 3D viewport",
												On: func() bool {
													_ = rev.Get()
													return ed.ShowRenderStats
												},
												OnChange: ed.SetShowRenderStats,
											},
										},
									},
									ui.ToggleButton{
										TestID:   "toolbar-terrain",
										Text:     "Terrain",
										Disabled: notPlaying,
										On: func() bool {
											_ = rev.Get()
											return ed.TerrainOpen()
										},
										OnChange: func(on bool) {
											if on && !ed.TerrainOpen() {
												ed.report(ed.NewTerrain(NewTerrainOptions{}))
											} else if !on && ed.TerrainOpen() {
												ed.CloseTerrain()
											}
										},
									},
								},
							}
						},
						Else: func(cx *core.Scope) core.Node {
							return ui.ToolbarGroup{
								Label: ed.T("toolbar.group.tools"),
								Children: []core.Node{
									toolBtn("tool-select", ed.T("toolbar.select"), ToolSelect),
									toolBtn("tool-move", ed.T("toolbar.move"), ToolMove),
									toolBtn("tool-place", ed.T("toolbar.place"), ToolPlace),
									toolBtn("tool-paint", ed.T("toolbar.paint"), ToolPaintTile),
									toolBtn("tool-erase", ed.T("toolbar.erase"), ToolEraseTile),
									toolBtn("tool-fill", ed.T("toolbar.fill"), ToolFillTile),
									toolBtn("tool-pick", ed.T("toolbar.pick"), ToolPickTile),
									toolBtn("tool-terrain", ed.T("toolbar.terrain"), ToolTerrain),
								},
							}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.ViewMode == ViewMode3D
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.ToolbarGroup{
								Label: "GridMap",
								Children: []core.Node{
									gridMapBtn("gridmap-tool-paint", "Paint", GridMapToolPaint),
									gridMapBtn("gridmap-tool-erase", "Erase", GridMapToolErase),
									gridMapBtn("gridmap-tool-pick", "Pick", GridMapToolPick),
									gridMapBtn("gridmap-tool-boxfill", "Box", GridMapToolBoxFill),
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
							return view2DToolsView(cx, ed, rev, notPlaying)
						},
					},
				},
			},
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.ViewMode == ViewMode3D
				},
				Then: func(cx *core.Scope) core.Node {
					return ui.Toolbar{
						Label: "3D NavMesh",
						Children: []core.Node{
							ui.ToolbarGroup{
								Label: "3D NavMesh",
								Children: []core.Node{
									ui.ToggleButton{
										TestID: "toolbar-navmesh-panel",
										Text:   "NavMesh",
										Label:  "Toggle the 3D Navigation Mesh bake panel",
										On: func() bool {
											_ = rev.Get()
											return ed.NavMeshPanelOpen()
										},
										OnChange: func(on bool) {
											ed.SetNavMeshPanelOpen(on)
										},
									},
									ui.ToggleButton{
										TestID: "toolbar-navmesh-overlay",
										Text:   "Nav Overlay",
										Label:  "Toggle the 3D navigation mesh overlay in the viewport",
										On: func() bool {
											_ = rev.Get()
											return ed.ShowNavMesh3D
										},
										OnChange: func(on bool) {
											ed.SetShowNavMesh3D(on)
										},
									},
								},
							},
						},
					}
				},
			},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					Wrap:       core.Wrap,
					AlignItems: core.AlignCenter,
					MaxWidth:   core.Pct(100),
					FlexShrink: 1,
					Gap:        4,
				},
				Children: []core.Node{
					ui.Button{
						TestID:  "toolbar-undo",
						Text:    ed.T("toolbar.undo"),
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
						Text:    ed.T("toolbar.redo"),
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
						Text:     ed.T("toolbar.save"),
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
								Text:    ed.T("toolbar.stop"),
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
								Text:    ed.T("toolbar.play"),
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
								Text:    ed.T("toolbar.stopGame"),
								Label:   ed.T("toolbar.stopGameLabel"),
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
								Text:    ed.T("toolbar.run"),
								Label:   ed.T("toolbar.runLabel"),
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
						Text:    ed.T("toolbar.commands"),
						Label:   ed.T("toolbar.commandsLabel"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						OnPress: func() {
							ed.ToggleCommandPalette()
						},
					},
					ui.Button{
						TestID:  "toolbar-animate",
						Text:    ed.T("toolbar.animate"),
						Label:   ed.T("toolbar.animateLabel"),
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
						TestID:   "toolbar-particles",
						Text:     "Particles",
						Label:    "Toggle the Particle Emitter Editor panel",
						Disabled: notPlaying,
						On: func() bool {
							_ = rev.Get()
							return ed.ParticleEmitterOpen()
						},
						OnChange: func(on bool) {
							if on {
								ed.report(ed.NewParticleEmitter())
							} else {
								ed.report(ed.CloseParticleEmitter())
							}
						},
					},
					ui.ToggleButton{
						TestID: "toolbar-project-settings",
						Text:   ed.T("toolbar.projectSettings"),
						Label:  ed.T("toolbar.projectSettingsLabel"),
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
						Text:   ed.T("toolbar.mixer"),
						Label:  ed.T("toolbar.mixerLabel"),
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
						Text:   ed.T("toolbar.input"),
						Label:  ed.T("toolbar.inputLabel"),
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
						Text:     ed.T("toolbar.strings"),
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
						Text:    ed.T("toolbar.buildAssets"),
						Label:   ed.T("toolbar.buildAssetsLabel"),
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
						Text:   ed.T("toolbar.export"),
						Label:  ed.T("toolbar.exportLabel"),
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
						Text:   ed.T("toolbar.output"),
						Label:  ed.T("toolbar.outputLabel"),
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
						Text:   ed.T("toolbar.currentScene"),
						Label:  ed.T("toolbar.currentSceneLabel"),
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

// view2DToolsView is the tool bar's group of controls for the 2D view: framing, returning to the scene camera, lighting preview, show-all-shapes, and grid snap toggles.
func view2DToolsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], playing func() bool) core.Node {
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
		Label: ed.T("toolbar.group.view2d"),
		Children: []core.Node{
			btn("view-frame-selection", lucide.Focus, ed.T("toolbar.frameSelection", l10n.Param("key", cx.KeyLabel("f"))), func() { ed.FrameSelection2D() }),
			btn("view-frame-all", lucide.Maximize, ed.T("toolbar.frameAll", l10n.Param("key", cx.KeyLabel("home"))), func() { ed.FrameAll2D() }),
			btn("view-reset", lucide.Video, ed.T("toolbar.sceneCameraView", l10n.Param("key", cx.KeyLabel("mod+0"))), func() { ed.ResetView2D() }),
			ui.ToggleButton{
				TestID:   "view-lighting-preview",
				Icon:     lucide.Lightbulb,
				Label:    ed.T("toolbar.lightingPreviewLabel"),
				Disabled: playing,
				On: func() bool {
					_ = rev.Get()
					return ed.ShowLightingPreview
				},
				OnChange: func(on bool) {
					ed.SetShowLightingPreview(on)
				},
			},
			ui.ToggleButton{
				TestID:   "view-show-all-shapes",
				Icon:     lucide.Hexagon,
				Label:    "Show all component shapes",
				Disabled: playing,
				On: func() bool {
					_ = rev.Get()
					return ed.ShowAllShapes
				},
				OnChange: func(on bool) {
					ed.SetShowAllShapes(on)
				},
			},
			ui.ToggleButton{
				TestID:   "view-snap-grid",
				Icon:     lucide.Magnet,
				Label:    "Snap shape handles to grid",
				Disabled: playing,
				On: func() bool {
					_ = rev.Get()
					return ed.SnapToGrid
				},
				OnChange: func(on bool) {
					ed.SetSnapToGrid(on)
				},
			},
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
							mode := ed.T("status.modeEdit")
							if ed.Playing() {
								mode = ed.T("status.modePlay")
							}
							if ed.ViewMode == ViewMode3D {
								if gt := ed.GridMapTool(); gt != GridMapToolNone {
									return ed.T("status.mode3dGridMap",
										l10n.Param("mode", mode),
										l10n.Param("view", ed.viewStatus3D()),
										l10n.Param("tool", gt.String()),
										l10n.Param("floor", ed.GridMapFloor()),
										l10n.Param("orient", ed.GridMapOrientation()),
										l10n.Param("layer", ed.activeLayerReadout()),
									)
								}
								return ed.T("status.mode3d",
									l10n.Param("mode", mode),
									l10n.Param("view", ed.viewStatus3D()),
									l10n.Param("gizmo", ed.localizedGizmoName(ed.GizmoMode)),
									l10n.Param("layer", ed.activeLayerReadout()),
								)
							}
							zoom := int(math.Round(float64(ed.View2D().Zoom * 100)))
							status := ed.T("status.mode2d",
								l10n.Param("mode", mode),
								l10n.Param("zoom", zoom),
								l10n.Param("tool", ed.toolLabel()),
								l10n.Param("layer", ed.activeLayerReadout()),
							)
							// Erase writes no tile, and a layer without tiles to pick keeps an ID the palette does not list, so only a tile Paint, Fill, Rect or Line would write is shown.
							switch ed.Tool {
							case ToolPaintTile, ToolFillTile, ToolRectTile, ToolLineTile:
								if tile, ok := ed.TilePalette().Tile(ed.ActiveTileID); ok {
									status += ed.T("status.tileSuffix", l10n.Param("tile", tile.Number))
								}
							case ToolTerrain:
								if p := ed.TilePalette(); p.ActiveTerrain != "" {
									status += ed.T("status.terrainSuffix", l10n.Param("terrain", p.ActiveTerrain))
								}
							case ToolStampTile:
								if stamp, ok := ed.TileStamp(); ok {
									status += fmt.Sprintf(" | Stamp: %dx%d", stamp.Width, stamp.Height)
								} else if tile, ok := ed.TilePalette().Tile(ed.ActiveTileID); ok {
									status += ed.T("status.tileSuffix", l10n.Param("tile", tile.Number))
								}
							case ToolRandomTile:
								status += fmt.Sprintf(" | Random: %d tiles (seed %d)", len(ed.effectiveRandomPool()), ed.RandomBrushSeed())
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
					remoteStatusView(cx, ed, rev),
				},
			},
			core.Text{
				TestID: "editor-status-message",
				ContentFunc: func() string {
					_ = rev.Get()
					if ed.BuildErr != nil {
						return ed.T("status.buildError", l10n.Param("error", ed.BuildErr.Error()))
					}
					if ed.Status == "Ready" {
						return ed.T("status.ready")
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
