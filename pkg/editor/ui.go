package editor

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// EditorView returns the root NSGo UI node for ed, composing:
//   - Top header bar (mainHeaderView: scene breadcrumb & File menu with unsaved marker, Select/Move/Rotate/Scale/Pan tools, 2D/3D switcher, History, Command Palette, Play)
//   - Docked workspace (editorDockView) or Assets mode (assetsWorkspaceRow): left Scene / Assets rail, center interactive 2D/3D Viewport with floating viewport controls, Tile / GridMap palette, right Agent / Property Inspector rail, and bottom panels
//   - Bottom status bar (mainStatusBarView: active layer, pointer/tool readout, save status, tasks, problems pill, Output, Debugger)
//
// Mod+S saves (Editor.Save), and the window title follows the open scene and its unsaved marker (Editor.WindowTitle). Opening a scene from the file browser, the Problems panel or the Search and References panel, New Scene, a user's request to quit and closing the window ask first while the scene or project has unsaved edits (Editor.HasUnsavedChanges): Save saves them and goes on, Discard drops them and goes on, and Cancel keeps them.
func EditorView(cx *core.Scope, ed *Editor) core.Node {
	ed.AttachScope(cx)
	focusCtrl := newFocusRegionController(cx)
	ed.focus.ctrl = focusCtrl
	rev := core.NewSignal(cx, ed.Revision())
	ed.bindToastLifecycle(cx, rev)
	unsub := ed.Subscribe(func() {
		rev.Set(ed.Revision())
	})
	cx.OnKeyWith("f6", core.KeyOptions{When: core.KeyAlways}, func() {
		if ed.shortcutCaptureActive {
			return
		}
		ed.CycleRegion(+1)
	})
	cx.OnKeyWith("shift+f6", core.KeyOptions{When: core.KeyAlways}, func() {
		if ed.shortcutCaptureActive {
			return
		}
		ed.CycleRegion(-1)
	})
	// Spec rule 04: global shortcuts rest behind dialogs except Mod+. to stop the agent.
	cx.OnKeyWith("mod+.", core.KeyOptions{When: core.KeyAlways, OverModals: true}, func() {
		if ed.shortcutCaptureActive {
			return
		}
		if ed.AgentTurnWorking() {
			ed.report(ed.StopAgentTurn())
		}
		ed.StopRunningTasks()
		if ed.IsPlanRunning() {
			_, _ = ed.StopAgentPlan()
		}
		ed.StopAgent()
	})
	cx.OnKeyWith("mod+enter", core.KeyOptions{When: core.KeyNotEditing}, func() {
		ed.handleGlobalModEnter()
	})
	cx.OnKeyWith("shift+f5", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.GameRunning() {
			ed.report(ed.StopGame())
		}
	})
	cx.OnKeyWith("mod+shift+p", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.RemoteConnected() {
			snap := ed.remoteSnapshotShared()
			paused := snap.Loop != nil && snap.Loop.Paused
			ed.dispatchRemoteAction(cx, func() error {
				if paused {
					return ed.RemoteResume()
				}
				return ed.RemotePause()
			})
			return
		}
		if ed.Playing() {
			ed.SetPlayPaused(!ed.PlayPaused())
		}
	})
	cx.OnKeyWith(".", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.RemoteConnected() {
			snap := ed.remoteSnapshotShared()
			if snap.Loop != nil && snap.Loop.Paused {
				ed.dispatchRemoteAction(cx, func() error {
					_, err := ed.RemoteStep(1)
					return err
				})
			}
			return
		}
		if ed.Playing() && ed.PlayPaused() {
			ed.StepPlay(1)
		}
	})
	cx.Cleanup(func() {
		unsub()
		ed.stopClipLoop()
		ed.stopStatesLoop()
		ed.stopParticlePreviewLoop()
		ed.stopSceneEmitterLoop()
		ed.stopRigLoop()
		ed.stopCutsceneLoop()
		ed.closeModelPreview()
		ed.stopTileAnimLoop()
		ed.stopAgentPulseLoop()
		ed.cleanupAudioMixerScope(cx)
		ed.cleanupAssetDrawerScope(cx)
		ed.cleanupChangedAudioScope(cx)
		ed.cancelChangesOps()
		ed.stopAllTaskRunners()
		ed.cancelCodeReviewChecks()
		ed.stopExportBuild()
		ed.stopReleaseBuild()
		ed.StopAndroidDeviceRun()
		_ = ed.stopWebBuildServerInternal(false)
		ed.stopAssetsBuild()
		ed.stopProblemCheckTimer()
		ed.stopDiskWatcher()
		ed.CloseObjectContextMenu()
		ed.stopAgentTimers()
		ed.stopAutoSave()
		ed.CloseAskInPlace()
		ed.resetDockBadgeOnCleanup(cx)
		_ = ed.DisconnectProfiler()
		ed.cancelAssetGenOps()
		ed.DisconnectAllAssetGenerators()
		ed.cancelCreateProject()
		ed.cancelFirstRunOps()
		_ = ed.DisconnectRemote()
		_ = ed.StopGame()
		if ed.Playing() {
			_ = ed.Stop()
		}
		if ed.scope == cx {
			ed.AttachScope(nil)
		}
		if ed.focus.ctrl == focusCtrl {
			ed.focus.ctrl = nil
		}
	})

	cx.SetTheme(SpecTheme())
	colors := ui.ColorsOf(cx)
	appearanceMemo := core.NewMemo(cx, func() core.Appearance {
		_ = rev.Get()
		return ed.Appearance().CoreAppearance()
	})
	cx.Effect(func() {
		cx.SetAppearance(appearanceMemo.Get())
	})
	cx.OnNotificationPress(func(id string) {
		ed.HandleNotificationPress(id)
	})
	cx.Effect(func() {
		ed.setWindowLifecycle(cx.Env().Lifecycle())
	})
	cx.Effect(func() {
		_ = rev.Get()
		ed.syncDockBadge(cx)
	})
	ed.bindShortcuts(cx, rev)
	ed.bindFirstRunKeys(cx)
	bindObjectContextMenuKeys(cx, ed)
	ed.bindApplicationMenu(cx, rev)
	dirty := unsavedMemo(cx, ed, rev)
	prompt := core.NewSignal[*unsavedPrompt](cx, nil)
	pathMode := core.NewSignal(cx, pathNone)
	pathTarget := core.NewSignal(cx, "")
	ed.autosave.openSaveAs = func() {
		if ed.Playing() || ed.Project == nil || ed.ProjectPath == "" {
			return
		}
		if failedID := ed.autosave.failedTabID; failedID != 0 && failedID != ed.activeTabID() {
			_ = ed.SelectTabByID(failedID)
		}
		p := ed.ScenePath
		if p == "" {
			p = "scenes/untitled.scene.yaml"
		}
		pathTarget.Set(p)
		pathMode.Set(pathSaveAs)
	}
	cx.Cleanup(func() {
		ed.autosave.openSaveAs = nil
	})
	bindSceneFileHooks(cx, ed, rev, dirty, prompt)
	dragAreaKey.Provide(cx, true)

	type chromeState struct {
		lang   string
		dark   bool
		uiSize InterfaceSize
	}
	chromeKey := core.NewMemo(cx, func() chromeState {
		_ = rev.Get()
		return chromeState{
			lang:   ed.ResolvedLanguage(),
			dark:   cx.Dark(),
			uiSize: ed.InterfaceSize(),
		}
	})

	fullWindowScreenOpen := func() bool {
		return ed.StartScreenOpen() ||
			ed.FirstRunFullScreenOpen() ||
			ed.ReviewOpen() ||
			ed.MergeConflictOpen() ||
			ed.DataTableReviewOpen() ||
			ed.ChangesOpen() ||
			ed.CodeReviewOpen() ||
			ed.BuildReleaseScreenOpen() ||
			ed.UsagesScreenOpen() ||
			ed.PlaytestReportOpen() ||
			ed.AssetGeneratorOpen() ||
			ed.HUDLayoutOpen() ||
			(ed.CutsceneOpen() && !ed.Playing())
	}
	settingsOpen := func() bool {
		_ = rev.Get()
		return ed.SettingsOpen() && !ed.Playing()
	}
	topLevelSettingsOpen := func() bool {
		_ = rev.Get()
		return ed.SettingsOpen() && !ed.Playing() && fullWindowScreenOpen()
	}

	workspace := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = chromeKey.Get()
		files := sceneFileDialogs(cx, ed, rev, prompt, pathMode, pathTarget)
		return core.View{
			Disabled:                    topLevelSettingsOpen,
			AccessibilityElementsHidden: topLevelSettingsOpen,
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				Width:     core.Pct(100),
				Height:    core.Pct(100),
			},
			StyleFunc: func() core.Style {
				if topLevelSettingsOpen() {
					return core.Style{PointerEvents: core.PointerEventsNone}
				}
				return core.Style{}
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
						return core.Show{
							When: func() bool {
								_ = rev.Get()
								return ed.FirstRunFullScreenOpen()
							},
							Then: func(cx *core.Scope) core.Node {
								return FirstRunScreenView(cx, ed, rev)
							},
							Else: func(cx *core.Scope) core.Node {
								return core.Show{
									When: func() bool {
										_ = rev.Get()
										return ed.ReviewOpen()
									},
									Then: func(cx *core.Scope) core.Node {
										return ReviewScreenView(cx, ed, rev)
									},
									Else: func(cx *core.Scope) core.Node {
										return core.Show{
											When: func() bool {
												_ = rev.Get()
												return ed.MergeConflictOpen()
											},
											Then: func(cx *core.Scope) core.Node {
												return MergeConflictView(cx, ed, rev)
											},
											Else: func(cx *core.Scope) core.Node {
												return core.Show{
													When: func() bool {
														_ = rev.Get()
														return ed.DataTableReviewOpen()
													},
													Then: func(cx *core.Scope) core.Node {
														return DataTableReviewView(cx, ed, rev)
													},
													Else: func(cx *core.Scope) core.Node {
														return core.Show{
															When: func() bool {
																_ = rev.Get()
																return ed.CodeReviewOpen()
															},
															Then: func(cx *core.Scope) core.Node {
																return CodeReviewView(cx, ed, rev)
															},
															Else: func(cx *core.Scope) core.Node {
																return core.Show{
																	When: func() bool {
																		_ = rev.Get()
																		return ed.ChangesOpen()
																	},
																	Then: func(cx *core.Scope) core.Node {
																		return ChangesPanelView(cx, ed, rev)
																	},
																	Else: func(cx *core.Scope) core.Node {
																		return core.Show{
																			When: func() bool {
																				_ = rev.Get()
																				return ed.BuildReleaseScreenOpen()
																			},
																			Then: func(cx *core.Scope) core.Node {
																				return BuildReleaseScreenView(cx, ed, rev)
																			},
																			Else: func(cx *core.Scope) core.Node {
																				return core.Show{
																					When: func() bool {
																						_ = rev.Get()
																						return ed.UsagesScreenOpen()
																					},
																					Then: func(cx *core.Scope) core.Node {
																						return ReferencesScreenView(cx, ed, rev)
																					},
																					Else: func(cx *core.Scope) core.Node {
																						return core.Show{
																							When: func() bool {
																								_ = rev.Get()
																								return ed.PlaytestReportOpen()
																							},
																							Then: func(cx *core.Scope) core.Node {
																								return PlaytestReportView(cx, ed, rev)
																							},
																							Else: func(cx *core.Scope) core.Node {
																								return core.Show{
																									When: func() bool {
																										_ = rev.Get()
																										return ed.AssetGeneratorOpen()
																									},
																									Then: func(cx *core.Scope) core.Node {
																										return AssetGeneratorView(cx, ed, rev)
																									},
																									Else: func(cx *core.Scope) core.Node {
																										return core.Show{
																											When: func() bool {
																												_ = rev.Get()
																												return ed.HUDLayoutOpen()
																											},
																											Then: func(cx *core.Scope) core.Node {
																												return HUDLayoutView(cx, ed, rev)
																											},
																											Else: func(cx *core.Scope) core.Node {
																												return core.View{
																													Style: core.Style{
																														Width:  core.Pct(100),
																														Height: core.Pct(100),
																													},
																													Children: []core.Node{
																														mainHeaderView(cx, ed, rev, dirty, pathMode, pathTarget),
																														remotePlayBarView(cx, ed, rev),
																														playModeBarView(cx, ed, rev),
																														mergeDriverBannerView(cx, ed, rev),
																														diskWatchBannerView(cx, ed, rev),
																														core.Show{
																															When: func() bool {
																																_ = rev.Get()
																																return ed.CutsceneOpen() && !ed.Playing()
																															},
																															Then: func(cx *core.Scope) core.Node {
																																return CutscenePanelView(cx, ed, rev)
																															},
																															Else: func(cx *core.Scope) core.Node {
																																return core.View{
																																	Style: core.Style{
																																		FlexGrow:  1,
																																		FlexBasis: core.Pt(0),
																																		Overflow:  core.Hidden,
																																	},
																																	Children: []core.Node{
																																		core.View{
																																			Disabled:                    settingsOpen,
																																			AccessibilityElementsHidden: settingsOpen,
																																			Style: core.Style{
																																				FlexGrow:  1,
																																				FlexBasis: core.Pt(0),
																																				Overflow:  core.Hidden,
																																			},
																																			StyleFunc: func() core.Style {
																																				if settingsOpen() {
																																					return core.Style{PointerEvents: core.PointerEventsNone}
																																				}
																																				return core.Style{}
																																			},
																																			Children: []core.Node{
																																				core.Show{
																																					When: func() bool {
																																						_ = rev.Get()
																																						return ed.LeftRailMode() == LeftRailAssets
																																					},
																																					Then: func(cx *core.Scope) core.Node {
																																						return core.View{
																																							Style: core.Style{
																																								Direction: core.Row,
																																								FlexGrow:  1,
																																								FlexBasis: core.Pt(0),
																																								Overflow:  core.Hidden,
																																							},
																																							Children: []core.Node{dragdrop.Area{Children: assetsWorkspaceRow(cx, ed, rev)}},
																																						}
																																					},
																																					Else: func(cx *core.Scope) core.Node {
																																						return editorDockView(cx, ed, rev)
																																					},
																																				},
																																			},
																																		},
																																		SettingsView(cx, ed, rev),
																																	},
																																}
																															},
																														},
																														mainStatusBarView(cx, ed, rev),
																													},
																												}
																											},
																										}
																									},
																								}
																							},
																						}
																					},
																				}
																			},
																		}
																	},
																}
															},
														}
													},
												}
											},
										}
									},
								}
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
			return core.Style{Background: colors.Background()}
		},
		OnTouchCapture: func(core.TouchEvent) {
			focusCtrl.clearRegionRingOnPointer()
		},
		Children: []core.Node{
			workspace,
			playAskFrozenFrameDialog(cx, ed, rev),
			core.Show{
				When: topLevelSettingsOpen,
				Then: func(cx *core.Scope) core.Node {
					return SettingsView(cx, ed, rev)
				},
			},
			PermissionsWorkspaceOverlayView(cx, ed, rev),
			PermissionsToastView(cx, ed, rev),
			WindowToastView(cx, ed, rev),
			toastEscapeKeyView(ed, rev),
			AgentReviewPanelView(cx, ed, rev),
			PermissionsDraftCardView(cx, ed, rev),
			ShortcutsSheetView(cx, ed, rev),
			ConnectAgentDialogView(cx, ed, rev),
			dialogFocusTrapView(ed),
		},
	}
}

// assetsWorkspaceRow renders the dedicated Assets mode workspace: the Assets left rail, the compact viewport over the bottom Asset Drawer, and the selected-asset inspector (or History panel).
func assetsWorkspaceRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) []core.Node {
	return []core.Node{
		core.View{
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
				base := core.Style{Direction: core.Row, FlexShrink: 0}
				if ed.focus.ctrl == nil {
					return base
				}
				return ed.focus.ctrl.regionOutlineStyle(RegionSceneTree, base)
			},
			Children: []core.Node{AssetsLeftRailView(cx, ed, rev)},
		},
		core.View{
			TestID: "editor-center-column",
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
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
							Height:     core.Pt(290),
							FlexShrink: 0,
							Overflow:   core.Hidden,
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
				AssetDrawerView(cx, ed, rev),
				ProfilerStripView(cx, ed, rev),
			},
		},
		core.View{
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
				base := core.Style{Direction: core.Row, FlexShrink: 0}
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
						return AssetInspectorView(cx, ed, rev)
					},
				},
			},
		},
	}
}

func playReplayBannerView(ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.Playing() && ed.PlayReplay() != nil
		},
		Then: func(cx *core.Scope) core.Node {
			spec := ed.SpecPalette(cx)
			return core.View{
				TestID: "play-replay-banner",
				Style: core.Style{
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Height:      core.Pt(32),
					FlexShrink:  0,
					Padding:     core.Symmetric(0, 12),
					Gap:         10,
					Background:  spec.Panel,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: spec.Line,
				},
				Children: []core.Node{
					core.Text{
						TestID: "play-replay-info",
						ContentFunc: func() string {
							_ = rev.Get()
							p := ed.PlayReplayPlayer()
							if p == nil {
								return ""
							}
							runIdx := ed.PlayReplayRunIndex()
							if runIdx > 0 {
								return fmt.Sprintf("Watching run %d · step %d / %d · seed %d · %s", runIdx, p.StepIndex(), p.TotalSteps(), p.Seed(), ed.PlayReplayPath())
							}
							return fmt.Sprintf("Watching replay · step %d / %d · seed %d · %s", p.StepIndex(), p.TotalSteps(), p.Seed(), ed.PlayReplayPath())
						},
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      spec.TextMuted,
							FlexGrow:   1,
						},
					},
					ui.Button{
						TestID:  "play-replay-back-report",
						Text:    "Back to Playtest",
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Color:   func() core.Color { return spec.Raised },
						TextStyle: core.Style{
							FontSize:   11.5,
							FontWeight: 500,
							Color:      spec.TextSecondary,
						},
						Style: core.Style{
							Height:       core.Pt(24),
							CornerRadius: 6,
							Padding:      core.Symmetric(0, 10),
							BorderWidth:  1,
							BorderColor:  spec.LineRaised,
							FocusVisible: spec.FocusRing(2),
						},
						OnPress: func() {
							ed.report(ed.Stop())
							ed.SetPlaytestReportOpen(true)
						},
					},
				},
			}
		},
	}
}

func (e *Editor) handleGlobalModEnter() {
	// Trust boundary (#1015): ⌘↵ NEVER applies a You-only permissions draft, and must not fire over full-screen modes, modals, or a focused permission card.
	if e.shortcutCaptureActive || e.CapturingKey() || e.PermissionsDraftOpen() || e.PendingAgentPermissionCardFocused() ||
		e.StartScreenOpen() || e.FirstRunFullScreenOpen() || e.ReviewOpen() || e.CodeReviewOpen() || e.MergeConflictOpen() ||
		e.DataTableReviewOpen() || e.UsagesScreenOpen() || e.SettingsOpen() || e.AssetGeneratorOpen() ||
		e.ConnectDialogOpen() || e.PendingTurnPromptOpen() || e.BuildReleaseScreenOpen() {
		return
	}
	if e.AgentReviewOpen() && !e.Playing() {
		_, err := e.KeepAllReviewChanges()
		e.report(err)
		return
	}
	if e.reviewKeepShortcutAllowed() {
		if err := e.KeepTurnReview(); err != nil && !errors.Is(err, ErrReviewBlocked) {
			e.report(err)
		}
	}
}
