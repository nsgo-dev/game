package editor

import (
	"fmt"
	"path"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

func tileReviewOverlayBg(p SpecPalette) core.Color {
	return specAlpha(p.Raised, 0.92)
}

func tileReviewCardFocusBg(p SpecPalette) core.Color {
	return CompositeOver(specAlpha(p.Accent.Fg, 0.12), p.Raised)
}

func tileReviewFocusEdge(p SpecPalette) core.Color {
	return CompositeOver(specAlpha(p.Accent.Fg, 0.35), p.LineRaised)
}

func tileReviewKeycapBg(p SpecPalette) core.Color {
	return CompositeOver(specAlpha(p.TextFaint, 0.08), p.Raised)
}

func tileReviewRestoreTextColor(p SpecPalette) core.Color {
	return CompositeOver(specAlpha(p.TextPrimary, 0.62), p.Accent.Fg)
}

// viewportTileOverlays returns the 2D viewport floating overlays:
//   - Top-left tile layer dimension caption ("World · 24 × 20 cells · 16 px") while a tile layer is selected in 2D
//   - Top-center Reviewing Before / After segmented toggle + hold B keycap while a tile review is open
//   - Bottom-left painted / replaced / erased line-style legend while a tile review is open in After mode
//   - Key-hold watcher for holding B to peek Before during an active tile review
func viewportTileOverlays(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) []core.Node {
	return []core.Node{
		// Top floating overlay row: layer dimension caption on the left and Reviewing Before / After bar beside it
		// (wrapping cleanly to a second row when the viewport is narrow so the two pills never collide).
		core.View{
			TestID:        "viewport-tile-top-overlays",
			PointerEvents: core.PointerEventsBoxNone,
			Style: core.Style{
				Position:       core.Absolute,
				Top:            core.Pt(10),
				Left:           core.Pt(12),
				Right:          core.Pt(12),
				Direction:      core.Row,
				Wrap:           core.Wrap,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            8,
			},
			Children: []core.Node{
				core.Show{
					When: func() bool {
						_ = rev.Get()
						if ed.Playing() || ed.ViewMode != ViewMode2D {
							return false
						}
						_, _, ok := ed.ActiveTileLayerCaption()
						return ok
					},
					Then: func(cx *core.Scope) core.Node {
						return core.Dynamic(func(cx *core.Scope) core.Node {
							p := ed.SpecPalette(cx)
							return core.View{
								TestID:        "viewport-tile-caption",
								PointerEvents: core.PointerEventsNone,
								Style: core.Style{
									Height:       core.Pt(28),
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          8,
									Padding:      core.Symmetric(0, 12),
									CornerRadius: 8,
									Background:   tileReviewOverlayBg(p),
									BorderWidth:  1,
									BorderColor:  p.LineRaised,
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{
											Width:        core.Pt(7),
											Height:       core.Pt(7),
											CornerRadius: 2,
											Background:   p.Accent.Fg,
										},
									},
									core.Text{
										TestID: "viewport-tile-caption-layer",
										ContentFunc: func() string {
											_ = rev.Get()
											name, _, _ := ed.ActiveTileLayerCaption()
											return name
										},
										Style: core.Style{FontSize: 12, FontWeight: 600, Color: p.TextPrimary},
									},
									core.Text{
										TestID: "viewport-tile-caption-dims",
										ContentFunc: func() string {
											_ = rev.Get()
											_, dims, _ := ed.ActiveTileLayerCaption()
											return dims
										},
										Style: core.Style{FontSize: 11.5, Color: p.TextMuted},
									},
								},
							}
						})
					},
				},
				core.Show{
					When: func() bool {
						_ = rev.Get()
						return !ed.Playing() && ed.ViewMode == ViewMode2D && ed.TileReviewOpen()
					},
					Then: func(cx *core.Scope) core.Node {
						// Track held B key while a tile review is active so holding B anywhere peeks Before.
						cx.Effect(func() {
							held := core.KeyHeld(cx, "b")
							ed.SetTileReviewPeekBefore(held)
						})
						cx.Cleanup(func() {
							ed.SetTileReviewPeekBefore(false)
						})
						return core.Dynamic(func(cx *core.Scope) core.Node {
							p := ed.SpecPalette(cx)
							return core.View{
								TestID: "tile-review-bar",
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          8,
									Padding:      core.Edges{Top: 4, Bottom: 4, Left: 12, Right: 8},
									CornerRadius: 10,
									Background:   tileReviewOverlayBg(p),
									BorderWidth:  1,
									BorderColor:  p.LineRaised,
								},
								Children: []core.Node{
									core.Text{
										Content: ed.T("tileReview.reviewing"),
										Style:   core.Style{FontSize: 12, Color: p.TextMuted},
									},
									core.View{
										Style: core.Style{Direction: core.Row, Gap: 2},
										Children: []core.Node{
											tileReviewSegButton(cx, ed, "tile-review-before", ed.T("tileReview.before"), func() bool {
												_ = rev.Get()
												return ed.TileReviewShowingBefore()
											}, func() {
												ed.SetTileReviewViewMode(TileReviewBefore)
											}),
											tileReviewSegButton(cx, ed, "tile-review-after", ed.T("tileReview.after"), func() bool {
												_ = rev.Get()
												return !ed.TileReviewShowingBefore()
											}, func() {
												ed.SetTileReviewViewMode(TileReviewAfter)
											}),
										},
									},
									core.View{
										TestID: "tile-review-hold-b",
										Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
										Children: []core.Node{
											core.Text{
												Content: ed.T("tileReview.hold"),
												Style:   core.Style{FontSize: 11, Color: p.TextFaint},
											},
											core.View{
												Style: core.Style{
													Height:         core.Pt(18),
													Padding:        core.Symmetric(0, 5),
													CornerRadius:   4,
													Background:     tileReviewKeycapBg(p),
													BorderWidth:    1,
													BorderColor:    p.LineRaised,
													AlignItems:     core.AlignCenter,
													JustifyContent: core.JustifyCenter,
												},
												Children: []core.Node{
													core.Text{
														Content: "B",
														Style:   core.Style{FontSize: 10.5, FontWeight: 500, Color: p.TextSecondary},
													},
												},
											},
										},
									},
								},
							}
						})
					},
				},
			},
		},

		// Bottom-left Spec line-style legend (painted / replaced / erased) while showing After.
		core.Show{
			When: func() bool {
				_ = rev.Get()
				return !ed.Playing() && ed.ViewMode == ViewMode2D && ed.TileReviewOpen() && !ed.TileReviewShowingBefore()
			},
			Then: func(cx *core.Scope) core.Node {
				return core.Dynamic(func(cx *core.Scope) core.Node {
					p := ed.SpecPalette(cx)
					legendRow := func(color core.Color, text string, dashed bool) core.Node {
						return core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
							Children: []core.Node{
								core.Canvas{
									Style: core.Style{Width: core.Pt(12), Height: core.Pt(12)},
									Draw: func(d *core.Drawing, size core.Size) {
										d.Save()
										d.SetStrokeColor(color).SetLineWidth(1.5)
										if dashed {
											d.SetLineDash(3, 2)
										}
										d.StrokeRect(1, 1, float32(size.Width)-2, float32(size.Height)-2)
										d.Restore()
									},
								},
								core.Text{
									Content: text,
									Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
								},
							},
						}
					}
					return core.View{
						TestID:        "tile-review-legend",
						PointerEvents: core.PointerEventsNone,
						Style: core.Style{
							Position:     core.Absolute,
							Left:         core.Pt(12),
							Bottom:       core.Pt(12),
							Gap:          6,
							Padding:      core.Symmetric(8, 12),
							CornerRadius: 8,
							Background:   tileReviewOverlayBg(p),
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
						},
						Children: []core.Node{
							legendRow(p.Added.Fg, ed.T("tileReview.legendPainted"), true),
							legendRow(p.Changed.Fg, ed.T("tileReview.legendReplaced"), true),
							legendRow(p.Removed.Fg, ed.T("tileReview.legendErased"), false),
						},
					}
				})
			},
		},
	}
}

func tileReviewSegButton(cx *core.Scope, ed *Editor, testID, text string, active func() bool, onPress func()) core.Node {
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		Label:  text,
		OnPress: func(core.PressEvent) {
			onPress()
		},
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			var bg core.Color
			if active() {
				bg = p.LineRaised
			}
			return core.Style{
				Height:         core.Pt(26),
				Padding:        core.Symmetric(0, 10),
				CornerRadius:   6,
				Background:     bg,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			}
		},
		Children: []core.Node{
			core.Text{
				Content: text,
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					col := p.TextMuted
					if active() {
						col = p.TextPrimary
					}
					return core.Style{FontSize: 12, FontWeight: 500, Color: col}
				},
			},
		},
	}
}

// TileReviewPanelView renders the region-based tile edit review panel right of the viewport while a tile review is active in 2D,
// styled exclusively with Spec.dc.html tokens and matching design/editor/Tiles.dc.html.
func TileReviewPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	shown := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return !ed.Playing() && ed.ViewMode == ViewMode2D && ed.TileReviewOpen()
	})
	return core.Show{
		When: shown.Get,
		Then: func(cx *core.Scope) core.Node {
			return tileReviewPanel(cx, ed, rev)
		},
	}
}

func tileReviewPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	regionsKey := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.tileReviewUIRev()
	})

	sceneFileLabel := func() string {
		if ed.ScenePath != "" {
			return path.Base(ed.ScenePath)
		}
		if ed.FilePath != "" {
			return path.Base(ed.FilePath)
		}
		layer, _ := ed.TileReviewLayer()
		return layer
	}

	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := ed.SpecPalette(cx)
		return core.View{
			TestID: "tile-review-panel",
			Role:   core.RoleRegion,
			Label:  ed.T("tileReview.panelLabel"),
			OnKeyDown: func(ev core.KeyEvent) bool {
				return ed.TileReviewHandleKeyEvent(ev, true)
			},
			OnKeyUp: func(ev core.KeyEvent) {
				ed.TileReviewHandleKeyEvent(ev, false)
			},
			Style: dockedStyle(cx, core.Style{
				Width:       core.Pt(304),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
				Padding:     core.All(12),
				Gap:         12,
				Overflow:    core.Hidden,
			}),
			Children: []core.Node{
				// Agent step trace
				core.Dynamic(func(cx *core.Scope) core.Node {
					_ = regionsKey.Get()
					curPal := ed.SpecPalette(cx)
					steps := ed.TileReviewSteps()
					if len(steps) == 0 {
						return core.View{}
					}
					rows := make([]core.Node, 0, len(steps))
					for i, st := range steps {
						rows = append(rows, core.View{
							TestID: fmt.Sprintf("tile-review-step-%d", i),
							Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:          core.Pt(14),
										Height:         core.Pt(14),
										FlexShrink:     0,
										CornerRadius:   7,
										Background:     curPal.Added.Fill,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
									},
									Children: []core.Node{
										core.Text{
											Content: "✓",
											Style:   core.Style{FontSize: 9, FontWeight: 700, Color: curPal.Added.Fg},
										},
									},
								},
								core.Text{
									Content: st.Summary,
									Style:   core.Style{FontSize: 11.5, Color: curPal.TextSecondary, FlexGrow: 1, FlexBasis: core.Pt(0)},
								},
								core.Text{
									Content: st.Tool,
									Style:   core.Style{FontSize: 10.5, Color: curPal.TextFaint},
								},
							},
						})
					}
					return core.View{
						TestID:   "tile-review-steps",
						Style:    core.Style{Gap: 6},
						Children: rows,
					}
				}),

				// Region review card
				core.View{
					TestID: "tile-review-card",
					Style: core.Style{
						FlexGrow:     1,
						FlexBasis:    core.Pt(0),
						CornerRadius: 12,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						Overflow:     core.Hidden,
					},
					Children: []core.Node{
						// Card header: "<M> regions · <N> cells" + scene filename
						core.View{
							Style: core.Style{
								Height:         core.Pt(36),
								Direction:      core.Row,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifySpaceBetween,
								Padding:        core.Symmetric(0, 12),
								Borders:        core.Edges{Bottom: 1},
								BorderColor:    p.Line,
							},
							Children: []core.Node{
								core.Text{
									TestID: "tile-review-header",
									ContentFunc: func() string {
										_ = rev.Get()
										_, total := ed.TileReviewKeptCount()
										cells := ed.TileReviewTotalCells()
										return ed.T("tileReview.headerCounts",
											l10n.Param("regions", ed.Plural("tileReview.regionCount", total)),
											l10n.Param("cells", ed.Plural("tileReview.cellCount", cells)),
										)
									},
									Style: core.Style{FontSize: 12.5, FontWeight: 500, Color: p.TextPrimary},
								},
								core.View{
									Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
									Children: []core.Node{
										core.Show{
											When: func() bool {
												_ = rev.Get()
												return ed.SketchMarkCount() > 0
											},
											Then: func(cx *core.Scope) core.Node {
												return core.View{
													TestID: "tile-review-sketch-overlay",
													Role:   core.RoleButton,
													Label:  ed.T("tileReview.sketchOverlayLabel"),
													OnPress: func(core.PressEvent) {
														ed.ToggleSketchReviewOverlay()
													},
													StyleFunc: func() core.Style {
														_ = rev.Get()
														curPal := ed.SpecPalette(cx)
														bg := core.Transparent
														if ed.SketchReviewOverlayVisible() {
															bg = curPal.LineRaised
														}
														return core.Style{
															Height:         core.Pt(24),
															Padding:        core.Symmetric(0, 8),
															CornerRadius:   6,
															Background:     bg,
															AlignItems:     core.AlignCenter,
															JustifyContent: core.JustifyCenter,
															FocusVisible:   curPal.FocusRing(2),
														}
													},
													Children: []core.Node{
														core.Text{
															Content: ed.T("tileReview.sketchOverlay"),
															StyleFunc: func() core.Style {
																_ = rev.Get()
																curPal := ed.SpecPalette(cx)
																col := curPal.TextMuted
																if ed.SketchReviewOverlayVisible() {
																	col = curPal.TextPrimary
																}
																return core.Style{FontSize: 11, FontWeight: 500, Color: col}
															},
														},
													},
												}
											},
										},
										core.Text{
											TestID: "tile-review-file",
											ContentFunc: func() string {
												_ = rev.Get()
												return sceneFileLabel()
											},
											Style: core.Style{FontSize: 11, Color: p.TextFaint},
										},
									},
								},
							},
						},

						// Scrollable region rows
						ui.ScrollView{
							Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
							Children: []core.Node{
								core.Dynamic(func(cx *core.Scope) core.Node {
									_ = regionsKey.Get()
									curPal := ed.SpecPalette(cx)
									regions := ed.TileReviewRegions()
									focusedID := ed.TileReviewFocusedRegion()
									rows := make([]core.Node, 0, len(regions))
									for i, rg := range regions {
										region := rg
										idx := i
										kept := !region.Reverted
										focused := region.ID == focusedID

										var markBg, markCol core.Color
										signText := region.Sign
										switch region.Kind {
										case TileReviewPainted:
											markBg = curPal.Added.Fill
											markCol = curPal.Added.Fg
										case TileReviewReplaced:
											markBg = curPal.Changed.Fill
											markCol = curPal.Changed.Fg
										case TileReviewErased:
											markBg = curPal.Removed.Fill
											markCol = curPal.Removed.Fg
											signText = "−"
										default:
											markBg = curPal.Accent.Fill
											markCol = curPal.Accent.Fg
										}

										var rowBg, rowBorder core.Color
										var rowBW float32
										if focused {
											rowBg = tileReviewCardFocusBg(curPal)
											rowBorder = tileReviewFocusEdge(curPal)
											rowBW = 1
										}
										rowOpacity := core.Alpha(1)
										if !kept {
											rowOpacity = core.Alpha(0.55)
										}
										toggleText := ed.T("tileReview.revert")
										toggleLabel := ed.T("tileReview.revertRegionLabel", l10n.Param("title", region.Title))
										toggleCol := curPal.TextSecondary
										if !kept {
											toggleText = ed.T("tileReview.restore")
											toggleLabel = ed.T("tileReview.restoreRegionLabel", l10n.Param("title", region.Title))
											toggleCol = tileReviewRestoreTextColor(curPal)
										}
										titleLine := ed.T("tileReview.regionTitle",
											l10n.Param("title", region.Title),
											l10n.Param("cells", ed.Plural("tileReview.cellCount", len(region.Cells))),
										)

										rows = append(rows, core.View{
											TestID: fmt.Sprintf("tile-review-region-%d", idx),
											Style: core.Style{
												Direction:    core.Row,
												AlignItems:   core.AlignCenter,
												Gap:          8,
												Padding:      core.All(8),
												CornerRadius: 8,
												Background:   rowBg,
												BorderWidth:  core.Points(rowBW),
												BorderColor:  rowBorder,
												Opacity:      rowOpacity,
											},
											Children: []core.Node{
												core.View{
													TestID: fmt.Sprintf("tile-review-region-focus-%d", idx),
													Role:   core.RoleButton,
													Label:  titleLine,
													OnPress: func(core.PressEvent) {
														ed.FocusTileReviewRegion(region.ID)
													},
													Style: core.Style{
														FlexGrow:     1,
														FlexBasis:    core.Pt(0),
														Direction:    core.Row,
														AlignItems:   core.AlignCenter,
														Gap:          8,
														FocusVisible: curPal.FocusRing(-2),
													},
													Children: []core.Node{
														core.View{
															Style: core.Style{
																Width:          core.Pt(22),
																Height:         core.Pt(22),
																FlexShrink:     0,
																CornerRadius:   6,
																Background:     markBg,
																AlignItems:     core.AlignCenter,
																JustifyContent: core.JustifyCenter,
															},
															Children: []core.Node{
																core.Text{
																	Content: signText,
																	Style:   core.Style{FontSize: 12, FontWeight: 600, Color: markCol},
																},
															},
														},
														core.View{
															Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
															Children: []core.Node{
																core.Text{
																	TestID:  fmt.Sprintf("tile-review-region-title-%d", idx),
																	Content: titleLine,
																	Style:   core.Style{FontSize: 12, FontWeight: 500, Color: curPal.TextPrimary},
																},
																core.Text{
																	TestID:  fmt.Sprintf("tile-review-region-detail-%d", idx),
																	Content: region.Detail,
																	Style:   core.Style{FontSize: 11, Color: curPal.TextFaint},
																},
															},
														},
													},
												},
												core.View{
													TestID: fmt.Sprintf("tile-review-region-toggle-%d", idx),
													Role:   core.RoleButton,
													Label:  toggleLabel,
													OnPress: func(core.PressEvent) {
														ed.report(ed.ToggleTileReviewRegion(region.ID))
													},
													Style: core.Style{
														Height:         core.Pt(26),
														Padding:        core.Symmetric(0, 10),
														CornerRadius:   6,
														Background:     curPal.LineRaised,
														AlignItems:     core.AlignCenter,
														JustifyContent: core.JustifyCenter,
														FocusVisible:   curPal.FocusRing(2),
													},
													Children: []core.Node{
														core.Text{
															TestID:  fmt.Sprintf("tile-review-region-toggle-text-%d", idx),
															Content: toggleText,
															Style:   core.Style{FontSize: 11.5, FontWeight: 500, Color: toggleCol},
														},
													},
												},
											},
										})
									}
									return core.View{
										Style:    core.Style{Padding: core.All(8), Gap: 4},
										Children: rows,
									}
								}),
							},
						},

						// Card footer: Keep N of M + Revert all
						core.View{
							Style: core.Style{
								Direction:   core.Row,
								Gap:         8,
								Padding:     core.All(12),
								Borders:     core.Edges{Top: 1},
								BorderColor: p.Line,
							},
							Children: []core.Node{
								core.View{
									TestID: "tile-review-keep",
									Role:   core.RoleButton,
									Label:  ed.T("tileReview.keepLabel"),
									OnPress: func(core.PressEvent) {
										if _, err := ed.KeepTileReview(); err != nil {
											ed.report(err)
										}
									},
									Style: core.Style{
										FlexGrow:       1,
										Height:         core.Pt(30),
										CornerRadius:   8,
										Background:     p.Accent.Fg,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   p.FocusRing(2),
									},
									Children: []core.Node{
										core.Text{
											TestID: "tile-review-keep-text",
											ContentFunc: func() string {
												_ = rev.Get()
												kept, total := ed.TileReviewKeptCount()
												return ed.T("tileReview.keepCount",
													l10n.Param("kept", kept),
													l10n.Param("total", total),
												)
											},
											Style: core.Style{FontSize: 12, FontWeight: 600, Color: p.Accent.OnFill},
										},
									},
								},
								core.View{
									TestID: "tile-review-revert-all",
									Role:   core.RoleButton,
									Label:  ed.T("tileReview.revertAllLabel"),
									OnPress: func(core.PressEvent) {
										ed.report(ed.RevertAllTileReview())
									},
									Style: core.Style{
										Height:         core.Pt(30),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										Background:     p.LineRaised,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   p.FocusRing(2),
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("tileReview.revertAll"),
											Style:   core.Style{FontSize: 12, FontWeight: 500, Color: p.TextSecondary},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	})
}
