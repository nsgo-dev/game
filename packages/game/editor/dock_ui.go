package editor

import (
	"github.com/nsgo-dev/nsgo/base/dock"
	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// dockedPanelKey reports whether a panel view is rendered inside EditorView's ui.DockArea,
// so panel views fill their dock slot instead of keeping fixed standalone dimensions.
var dockedPanelKey = core.NewKey("editor.dockedPanel", false)

func isDockedPanel(cx *core.Scope) bool {
	return dockedPanelKey.Use(cx)
}

// dockedStyle returns a panel container style that fills its dock slot when rendered inside ui.DockArea,
// or fallback when the panel view is embedded standalone outside EditorView.
func dockedStyle(cx *core.Scope, fallback core.Style) core.Style {
	if !isDockedPanel(cx) {
		return fallback
	}
	s := fallback
	s.Width = core.Length{}
	s.Height = core.Length{}
	s.FlexGrow = 1
	s.FlexShrink = 1
	s.FlexBasis = core.Pt(0)
	s.MinWidth = core.Pt(0)
	s.MinHeight = core.Pt(0)
	s.Overflow = core.Hidden
	s.Borders = core.Edges{}
	return s
}

// editorDockView renders the editor's workspace on NSGo's ui.DockArea, wrapped in a single
// dragdrop.Area so dragging assets and models from docked panels onto the viewport works across regions.
func editorDockView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	dockedPanelKey.Provide(cx, true)
	colors := ui.ColorsOf(cx)
	areaRef := &dock.AreaRef{}
	ed.dock.areaRef = areaRef
	cx.Cleanup(func() {
		if ed.dock.areaRef == areaRef {
			ed.dock.areaRef = nil
		}
	})

	dockSig := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.dockSyncKey()
	})
	layoutGetter := func() dock.Layout {
		_ = dockSig.Get()
		return ed.DockLayout()
	}

	return core.View{
		Style: core.Style{
			Direction: core.Row,
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			MinWidth:  core.Pt(0),
			MinHeight: core.Pt(0),
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			dragdrop.Area{
				Children: []core.Node{
					ui.DockArea{
						Ref:    areaRef,
						TestID: "editor-dock-area",
						Layout: layoutGetter,
						OnLayoutChange: func(next dock.Layout) {
							ed.SetDockLayout(next)
						},
						Panels: map[string]dock.PanelOptions{
							PanelViewport: {NoClose: true},
						},
						HandleLabel: "Resize panel",
						Style: core.Style{
							FlexGrow:  1,
							FlexBasis: core.Pt(0),
							MinWidth:  core.Pt(0),
							MinHeight: core.Pt(0),
						},
						GroupStyle: core.Style{
							FlexBasis: core.Pt(0),
							Overflow:  core.Hidden,
						},
						ContentStyle: core.Style{
							FlexBasis: core.Pt(0),
							Overflow:  core.Hidden,
						},
						TabBarStyle: core.Style{
							MinHeight: core.Pt(24),
							Overflow:  core.Hidden,
						},
						TabStyle: core.Style{
							Padding: core.Symmetric(0, 8),
						},
						RenderTab: func(cx *core.Scope, tab dock.Tab) core.Node {
							title := ed.panelTitle(tab.Key)
							closeLabel := ""
							if tab.Key != PanelViewport {
								closeLabel = "Close " + title
							}
							return core.View{
								TestID: "dock-tab-" + tab.Key,
								Children: []core.Node{
									ui.DockTabLabel{
										Tab:        tab,
										Text:       title,
										CloseLabel: closeLabel,
									},
								},
							}
						},
						RenderDragPreview: func(cx *core.Scope, tab dock.Tab) core.Node {
							return core.View{
								Style: core.Style{
									Padding:      core.Symmetric(4, 10),
									CornerRadius: ui.RadiusSmall,
									BorderWidth:  1,
									BorderColor:  colors.Border(),
									Background:   colors.Raised(),
								},
								Children: []core.Node{
									core.Text{
										Content: ed.panelTitle(tab.Key),
										Style: core.Style{
											FontSize:   12,
											FontWeight: 600,
											Color:      colors.Text(),
										},
									},
								},
							}
						},
						RenderPanel: func(cx *core.Scope, key string) core.Node {
							return core.View{
								Style: core.Style{
									Position: core.Absolute,
									Top:      core.Pt(0),
									Right:    core.Pt(0),
									Bottom:   core.Pt(0),
									Left:     core.Pt(0),
									Overflow: core.Hidden,
								},
								Children: []core.Node{renderDockedPanel(cx, ed, rev, key)},
							}
						},
						RenderEmpty: func(cx *core.Scope) core.Node {
							return core.View{
								Style: core.Style{
									FlexGrow:       1,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
								},
								Children: []core.Node{
									core.Text{
										Content: "No panels open",
										Style:   core.Style{FontSize: 12, Color: colors.Muted()},
									},
								},
							}
						},
					},
				},
			},
		},
	}
}

func renderDockedPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], key string) core.Node {
	switch key {
	case PanelViewport:
		return core.View{
			TestID: "editor-center-column",
			Style: core.Style{
				Direction: core.Row,
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				MinWidth:  core.Pt(0),
				MinHeight: core.Pt(0),
				Overflow:  core.Hidden,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						MinWidth:  core.Pt(0),
						MinHeight: core.Pt(0),
						Overflow:  core.Hidden,
					},
					Children: []core.Node{
						SceneTabsView(cx, ed, rev),
						playReplayBannerView(ed, rev),
						core.View{
							Ref:    ed.focus.ctrl.rootRef(RegionViewport),
							TestID: "editor-region-viewport",
							Role:   core.RoleRegion,
							Label:  ed.T("region.viewport"),
							Focusable: func() bool {
								return ed.focus.ctrl != nil && ed.focus.ctrl.fallbackRegion.Get() == RegionViewport
							},
							OnFocusIn: func() {
								if ed.focus.ctrl != nil {
									ed.focus.ctrl.onRegionFocusIn(RegionViewport)
								}
							},
							OnFocusOut: func() {
								if ed.focus.ctrl != nil {
									ed.focus.ctrl.onRegionFocusOut(RegionViewport)
								}
							},
							StyleFunc: func() core.Style {
								base := core.Style{
									FlexGrow:  1,
									FlexBasis: core.Pt(0),
									MinHeight: core.Pt(88),
									Overflow:  core.Hidden,
								}
								if ed.focus.ctrl == nil {
									return base
								}
								return ed.focus.ctrl.regionOutlineStyle(RegionViewport, base)
							},
							Children: []core.Node{
								ViewportView(cx, ed, rev),
								// Draw the inset F6 region ring over the opaque Canvas child so macOS
								// layer compositing never obscures the Viewport region indicator.
								core.Show{
									When: func() bool {
										return ed.focus.ctrl != nil && ed.focus.ctrl.activeRegionRing.Get() == RegionViewport
									},
									Then: func(cx *core.Scope) core.Node {
										return core.View{
											TestID:        "editor-region-viewport-ring",
											PointerEvents: core.PointerEventsNone,
											StyleFunc: func() core.Style {
												return core.Style{
													Position:    core.Absolute,
													Top:         core.Pt(0),
													Right:       core.Pt(0),
													Bottom:      core.Pt(0),
													Left:        core.Pt(0),
													BorderWidth: 2,
													BorderColor: ed.SpecPalette(cx).Accent.Edge,
												}
											},
										}
									},
								},
							},
						},
						core.Show{
							When: func() bool {
								_ = rev.Get()
								return ed.ViewMode == ViewMode3D && !ed.Playing()
							},
							Then: func(cx *core.Scope) core.Node {
								return Viewport3DModelsStripView(cx, ed, rev)
							},
						},
						ProfilerStripView(cx, ed, rev),
					},
				},
				TilePaletteView(cx, ed, rev),
				GridMapPaletteView(cx, ed, rev),
			},
		}
	case PanelSceneTree:
		return core.View{
			Ref:    ed.focus.ctrl.rootRef(RegionSceneTree),
			TestID: "editor-region-scene-tree",
			Role:   core.RoleRegion,
			Label:  ed.T("region.sceneTree"),
			Focusable: func() bool {
				return ed.focus.ctrl != nil && ed.focus.ctrl.fallbackRegion.Get() == RegionSceneTree
			},
			OnFocusIn: func() {
				if ed.focus.ctrl != nil {
					ed.focus.ctrl.onRegionFocusIn(RegionSceneTree)
				}
			},
			OnFocusOut: func() {
				if ed.focus.ctrl != nil {
					ed.focus.ctrl.onRegionFocusOut(RegionSceneTree)
				}
			},
			StyleFunc: func() core.Style {
				base := core.Style{
					FlexGrow:   1,
					FlexShrink: 1,
					FlexBasis:  core.Pt(0),
					MinWidth:   core.Pt(0),
					MinHeight:  core.Pt(0),
					Overflow:   core.Hidden,
				}
				if ed.focus.ctrl == nil {
					return base
				}
				return ed.focus.ctrl.regionOutlineStyle(RegionSceneTree, base)
			},
			Children: []core.Node{
				LeftRailSwitchView(cx, ed, rev),
				SceneTreeView(cx, ed, rev),
			},
		}
	case PanelLayers:
		return LayersView(cx, ed, rev)
	case PanelAssets:
		return AssetBrowserView(cx, ed, rev)
	case PanelFiles:
		return FilesView(cx, ed, rev)
	case PanelLibrary:
		return LibraryView(cx, ed, rev)
	case PanelInspector:
		return core.View{
			Ref:    ed.focus.ctrl.rootRef(RegionInspector),
			TestID: "editor-region-inspector",
			Role:   core.RoleRegion,
			Label:  ed.T("region.inspector"),
			Focusable: func() bool {
				return ed.focus.ctrl != nil && ed.focus.ctrl.fallbackRegion.Get() == RegionInspector
			},
			OnFocusIn: func() {
				if ed.focus.ctrl != nil {
					ed.focus.ctrl.onRegionFocusIn(RegionInspector)
				}
			},
			OnFocusOut: func() {
				if ed.focus.ctrl != nil {
					ed.focus.ctrl.onRegionFocusOut(RegionInspector)
				}
			},
			StyleFunc: func() core.Style {
				base := core.Style{
					FlexGrow:   1,
					FlexShrink: 1,
					FlexBasis:  core.Pt(0),
					MinWidth:   core.Pt(0),
					MinHeight:  core.Pt(0),
					Overflow:   core.Hidden,
				}
				if ed.focus.ctrl == nil {
					return base
				}
				return ed.focus.ctrl.regionOutlineStyle(RegionInspector, base)
			},
			Children: []core.Node{
				core.Show{
					When: func() bool {
						_ = rev.Get()
						return !ed.Playing() && ed.HistoryPanelOpen()
					},
					Then: func(cx *core.Scope) core.Node {
						return HistoryPanelView(cx, ed, rev)
					},
					Else: func(cx *core.Scope) core.Node {
						return core.Show{
							When: func() bool {
								_ = rev.Get()
								return !ed.Playing() && ed.ProblemAgentPanelOpen()
							},
							Then: func(cx *core.Scope) core.Node {
								return ProblemAgentPanelView(cx, ed, rev)
							},
							Else: func(cx *core.Scope) core.Node {
								return core.Show{
									When: func() bool {
										_ = rev.Get()
										return !ed.Playing() && ed.ViewMode == ViewMode2D && ed.TileReviewOpen()
									},
									Then: func(cx *core.Scope) core.Node {
										return TileReviewPanelView(cx, ed, rev)
									},
									Else: func(cx *core.Scope) core.Node {
										return core.Show{
											When: func() bool {
												_ = rev.Get()
												return ed.SketchPanelOpen()
											},
											Then: func(cx *core.Scope) core.Node {
												return SketchPanelView(cx, ed, rev)
											},
											Else: func(cx *core.Scope) core.Node {
												return RightRailView(cx, ed, rev)
											},
										}
									},
								}
							},
						}
					},
				},
			},
		}
	case PanelProblems:
		return ProblemsView(cx, ed, rev)
	case PanelOutput:
		return OutputPanelView(cx, ed, rev)
	case PanelProfiler:
		return ProfilerPanelView(cx, ed, rev)
	case PanelSearch:
		return SearchPanelView(cx, ed, rev)
	case PanelAnimation:
		return AnimationPanelView(cx, ed, rev)
	case PanelStates:
		return StatesPanelView(cx, ed, rev)
	case PanelTileset:
		return TilesetEditorPanelView(cx, ed, rev)
	case PanelTerrain:
		return TerrainPanelView(cx, ed, rev)
	case PanelRig:
		return RigPanelView(cx, ed, rev)
	case PanelParticles:
		return ParticleEditorPanelView(cx, ed, rev)
	case PanelMaterial:
		return MaterialEditorPanelView(cx, ed, rev)
	case PanelEnvironment:
		return EnvironmentPanelView(cx, ed, rev)
	case PanelDataTable:
		return DataTablePanelView(cx, ed, rev)
	case PanelNavMesh:
		return NavMeshPanelView(cx, ed, rev)
	case PanelLocalization:
		return LocalizationPanelView(cx, ed, rev)
	default:
		if p, pn, ok := ed.registeredPluginPanel(key); ok {
			return pluginPanelView(cx, ed, rev, dockedPanel{plugin: p.Name, owner: p, panel: pn})
		}
		return nil
	}
}
