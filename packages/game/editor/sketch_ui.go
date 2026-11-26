package editor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// viewportSketchOverlayView renders the floating top-center Sketch toolbar, tool hint, and inline
// selected-mark label editor over the 2D viewport while Sketch mode is active, using exclusively Spec tokens.
func viewportSketchOverlayView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	active := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SketchActive() && !ed.sketchBlockedByScreen()
	})
	type overlayThemeKey struct {
		dark bool
		lang string
	}
	themeMemo := core.NewMemo(cx, func() overlayThemeKey {
		_ = rev.Get()
		return overlayThemeKey{
			dark: ed.SpecPalette(cx).Dark,
			lang: ed.ResolvedLanguage(),
		}
	})
	return core.Show{
		When: active.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				p := SpecPaletteFor(themeMemo.Get().dark)
				divider := func() core.Node {
					return core.View{
						Style: core.Style{
							Width:      core.Pt(1),
							Height:     core.Pt(16),
							Background: p.LineRaised,
							Margin:     core.Symmetric(0, 4),
						},
					}
				}

				toolBtn := func(testID string, tool SketchTool, label string, key string, icon func() *core.SVG, textFallback string) core.Node {
					t := tool
					btn := ui.Button{
						TestID:  testID,
						Label:   fmt.Sprintf("%s · %s", label, key),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Color: func() core.Color {
							_ = rev.Get()
							if ed.SketchTool() == t {
								return p.LineRaised
							}
							return core.Transparent
						},
						Tint: func() core.Color {
							_ = rev.Get()
							if ed.SketchTool() == t {
								return p.TextPrimary
							}
							return p.TextMuted
						},
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: 600,
							Color:      p.TextSecondary,
						},
						Style: core.Style{
							Width:        core.Pt(32),
							Height:       core.Pt(28),
							CornerRadius: 6,
							FocusVisible: p.FocusRing(2),
						},
						OnPress: func() {
							ed.SetSketchTool(t)
						},
					}
					if icon != nil {
						btn.Icon = icon
						btn.IconSize = 15
					} else {
						btn.Text = textFallback
					}
					return btn
				}

				selectedMarkKey := core.NewMemo(cx, func() int {
					_ = rev.Get()
					return ed.SelectedSketchMark()
				})

				return core.View{
					TestID:        "viewport-sketch-overlay-wrap",
					PointerEvents: core.PointerEventsBoxNone,
					Style: core.Style{
						Position:   core.Absolute,
						Left:       core.Pt(0),
						Right:      core.Pt(0),
						Top:        core.Pt(14),
						AlignItems: core.AlignCenter,
						Gap:        8,
					},
					Children: []core.Node{
						// Main floating sketch toolbar matching Sketch.dc.html.
						core.View{
							TestID: "viewport-sketch-toolbar",
							Role:   core.RoleToolbar,
							Label:  ed.T("sketch.toolbarLabel"),
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          2,
								Padding:      core.All(4),
								CornerRadius: 12,
								Background:   ui.Alpha(p.Raised, 0.96),
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
							},
							Children: []core.Node{
								toolBtn("sketch-tool-pen", SketchToolPen, ed.T("sketch.tool.pen"), "P", lucide.Pencil, ""),
								toolBtn("sketch-tool-arrow", SketchToolArrow, ed.T("sketch.tool.arrow"), "A", lucide.ArrowUpRight, ""),
								toolBtn("sketch-tool-box", SketchToolBox, ed.T("sketch.tool.box"), "B", lucide.Square, ""),
								toolBtn("sketch-tool-text", SketchToolText, ed.T("sketch.tool.text"), "T", nil, "T"),
								divider(),
								ui.Button{
									TestID:   "sketch-undo-mark",
									Icon:     lucide.RotateCcw,
									IconSize: 15,
									Label:    ed.T("sketch.undoMark"),
									Size:     ui.SizeSmall,
									Variant:  ui.ButtonGhost,
									Side:     28,
									Disabled: func() bool {
										_ = rev.Get()
										return !ed.CanUndoSketchMark()
									},
									Tint: func() core.Color { return p.TextMuted },
									Style: core.Style{
										Width:        core.Pt(32),
										Height:       core.Pt(28),
										CornerRadius: 6,
										FocusVisible: p.FocusRing(2),
									},
									OnPress: func() {
										ed.UndoLastSketchMark()
									},
								},
								ui.Button{
									TestID:   "sketch-toggle-marks",
									Icon:     lucide.Eye,
									IconSize: 15,
									LabelFunc: func() string {
										_ = rev.Get()
										if ed.SketchMarksHidden() {
											return ed.T("sketch.showMarks")
										}
										return ed.T("sketch.hideMarks")
									},
									Size:    ui.SizeSmall,
									Variant: ui.ButtonGhost,
									Color: func() core.Color {
										_ = rev.Get()
										if ed.SketchMarksHidden() {
											return p.LineRaised
										}
										return core.Transparent
									},
									Tint: func() core.Color {
										_ = rev.Get()
										if ed.SketchMarksHidden() {
											return p.TextPrimary
										}
										return p.TextMuted
									},
									Style: core.Style{
										Width:        core.Pt(32),
										Height:       core.Pt(28),
										CornerRadius: 6,
										FocusVisible: p.FocusRing(2),
									},
									OnPress: func() {
										ed.ToggleSketchMarksHidden()
									},
								},
								core.Text{
									TestID: "sketch-bar-count",
									ContentFunc: func() string {
										_ = rev.Get()
										return ed.SketchBarCountLabel()
									},
									Style: core.Style{
										FontSize: 11.5,
										Color:    p.TextFaint,
										Padding:  core.Symmetric(0, 6),
									},
								},
								ui.Button{
									TestID:  "sketch-clear",
									Text:    ed.T("sketch.clear"),
									Label:   ed.T("sketch.clear"),
									Size:    ui.SizeSmall,
									Variant: ui.ButtonGhost,
									Disabled: func() bool {
										_ = rev.Get()
										return ed.SketchMarkCount() == 0
									},
									TextStyle: core.Style{
										FontSize:   12,
										FontWeight: 500,
										Color:      p.TextMuted,
									},
									Style: core.Style{
										Height:       core.Pt(28),
										Padding:      core.Symmetric(0, 8),
										CornerRadius: 6,
										FocusVisible: p.FocusRing(2),
									},
									OnPress: func() {
										ed.ClearSketch()
									},
								},
								divider(),
								ui.Button{
									TestID:  "sketch-done",
									Text:    ed.T("sketch.done"),
									Label:   ed.T("sketch.done"),
									Size:    ui.SizeSmall,
									Variant: ui.ButtonGhost,
									Color:   func() core.Color { return p.TextPrimary },
									TextStyle: core.Style{
										FontSize:   12,
										FontWeight: 600,
										Color:      p.OnTextPrimary,
									},
									Style: core.Style{
										Height:       core.Pt(28),
										Padding:      core.Symmetric(0, 12),
										CornerRadius: 6,
										FocusVisible: p.FocusRing(2),
									},
									OnPress: func() {
										ed.FinishSketch()
									},
								},
							},
						},

						// Active tool hint pill.
						core.View{
							TestID:        "sketch-tool-hint-pill",
							PointerEvents: core.PointerEventsNone,
							Style: core.Style{
								Height:         core.Pt(22),
								Padding:        core.Symmetric(0, 8),
								CornerRadius:   6,
								Background:     ui.Alpha(p.PillBg, 0.92),
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							},
							Children: []core.Node{
								core.Text{
									TestID: "sketch-tool-hint",
									ContentFunc: func() string {
										_ = rev.Get()
										return ed.SketchTool().Hint()
									},
									Style: core.Style{
										FontSize: 11.5,
										Color:    p.TextMuted,
									},
								},
							},
						},

						// Selected mark inline label editor (shown whenever a mark is selected, including after Correct a mark).
						core.Show{
							When: func() bool {
								return selectedMarkKey.Get() > 0
							},
							Then: func(cx *core.Scope) core.Node {
								return core.Dynamic(func(cx *core.Scope) core.Node {
									num := selectedMarkKey.Get()
									m, ok := ed.SketchMarkByNumber(num)
									if !ok {
										return core.View{}
									}
									pal := ed.SpecPalette(cx)
									return core.View{
										TestID: "sketch-mark-label-bar",
										Style: core.Style{
											Direction:    core.Row,
											AlignItems:   core.AlignCenter,
											Gap:          8,
											Padding:      core.Edges{Top: 4, Bottom: 4, Left: 8, Right: 6},
											CornerRadius: 10,
											Background:   ui.Alpha(pal.Raised, 0.96),
											BorderWidth:  1,
											BorderColor:  pal.Accent.Edge,
										},
										Children: []core.Node{
											core.View{
												TestID: "sketch-selected-mark-badge",
												Style: core.Style{
													Width:          core.Pt(18),
													Height:         core.Pt(18),
													CornerRadius:   9,
													Background:     pal.Accent.Fg,
													AlignItems:     core.AlignCenter,
													JustifyContent: core.JustifyCenter,
												},
												Children: []core.Node{
													core.Text{
														Content: strconv.Itoa(m.Number),
														Style: core.Style{
															FontSize:   11,
															FontWeight: 600,
															Color:      pal.Accent.OnFill,
														},
													},
												},
											},
											specInputField(cx, ed, ui.Input{
												TestID:      "sketch-mark-label-input",
												Label:       fmt.Sprintf("Label for mark %d", m.Number),
												Placeholder: "Add label (e.g. 3 spikes)...",
												Value: func() string {
													_ = rev.Get()
													if cur, ok := ed.SketchMarkByNumber(num); ok {
														return cur.Label
													}
													return ""
												},
												OnChange: func(v string) {
													ed.SetSketchMarkLabel(num, v)
												},
												Style: core.Style{
													Width: core.Pt(200),
												},
											}),
											ui.Button{
												TestID:   "sketch-mark-delete",
												Icon:     lucide.Trash,
												IconSize: 13,
												Label:    fmt.Sprintf("Delete mark %d", m.Number),
												Size:     ui.SizeSmall,
												Variant:  ui.ButtonGhost,
												Side:     26,
												Tint:     func() core.Color { return pal.TextMuted },
												Style: core.Style{
													CornerRadius: 6,
													FocusVisible: pal.FocusRing(2),
												},
												OnPress: func() {
													ed.DeleteSketchMark(num)
												},
											},
										},
									}
								})
							},
						},
					},
				}
			})
		},
	}
}

// sceneTreeSketchSectionView renders the compact "SKETCH LAYER · N marks · not saved" row at the bottom
// of the Scene Tree panel when sketch mode is active or marks exist, matching docs/design/editor/Sketch.dc.html.
func sceneTreeSketchSectionView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	shown := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return !ed.Playing() && (ed.SketchActive() || ed.SketchMarkCount() > 0)
	})
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})
	return core.Show{
		When: shown.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				p := SpecPaletteFor(darkMemo.Get())
				return core.View{
					TestID: "scene-tree-sketch-section",
					Style: core.Style{
						Padding:     core.Edges{Top: 8, Bottom: 4, Left: 4, Right: 4},
						Borders:     core.Edges{Top: 1},
						BorderColor: p.Line,
						Gap:         4,
					},
					Children: []core.Node{
						core.Text{
							Content: "SKETCH LAYER",
							Style: core.Style{
								FontSize:      11,
								FontWeight:    600,
								LetterSpacing: 0.6,
								Color:         p.TextFaint,
								Padding:       core.Symmetric(0, 4),
							},
						},
						core.View{
							TestID: "scene-tree-sketch-layer",
							Role:   core.RoleButton,
							Label:  "Sketch layer",
							OnPress: func(core.PressEvent) {
								ed.StartSketch()
							},
							Style: core.Style{
								Height:       core.Pt(28),
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          8,
								Padding:      core.Symmetric(0, 6),
								CornerRadius: 6,
								FocusVisible: p.FocusRing(-2),
							},
							Children: []core.Node{
								core.Text{
									TestID: "scene-tree-sketch-count",
									ContentFunc: func() string {
										_ = rev.Get()
										n := ed.SketchMarkCount()
										if n == 1 {
											return "1 mark"
										}
										return fmt.Sprintf("%d marks", n)
									},
									Style: core.Style{
										FlexGrow: 1,
										FontSize: 12,
										Color:    p.TextMuted,
									},
								},
								core.Text{
									Content: "not saved",
									Style: core.Style{
										FontSize: 11,
										Color:    p.TextFaint,
									},
								},
							},
						},
					},
				}
			})
		},
	}
}

// SketchPanelView renders the right-side Agent / Sketch panel matching docs/design/editor/Sketch.dc.html
// using exclusively Spec tokens: Agent header, post-sketch Review card (with Sketch overlay toggle, Keep, Revert),
// Agent Reading card ("I read your sketch as:" with numbered items, assumptions warning box, Go ahead, Correct a mark),
// "Sent with this request" scene-coordinate preview, and composer with the "Sketch · N marks" chip.
func SketchPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type panelKey struct {
		dark          bool
		lang          string
		markCount     int
		selectedMark  int
		hold          SketchHoldState
		hasReading    bool
		readingItems  int
		readingAssump int
		reviewOpen    bool
		reviewChanges int
		reviewOverlay bool
		previewOpen   bool
		attached      bool
		marksDigest   string
	}
	stateKey := core.NewMemo(cx, func() panelKey {
		_ = rev.Get()
		var b strings.Builder
		for _, m := range ed.SketchMarks() {
			fmt.Fprintf(&b, "%d:%s:%s:%s;", m.Number, m.Kind, m.GeometrySummary(), m.Label)
		}
		var rItems, rAssump int
		if rd := ed.SketchReading(); rd != nil {
			rItems = len(rd.Items)
			rAssump = len(rd.Assumptions)
			b.WriteString("|R:")
			b.WriteString(rd.Summary)
			for _, it := range rd.Items {
				fmt.Fprintf(&b, "%d:%s:%s;", it.Number, it.Mark, it.Text)
			}
			for _, a := range rd.Assumptions {
				b.WriteString(a)
				b.WriteByte(';')
			}
		}
		return panelKey{
			dark:          ed.SpecPalette(cx).Dark,
			lang:          ed.ResolvedLanguage(),
			markCount:     ed.SketchMarkCount(),
			selectedMark:  ed.SelectedSketchMark(),
			hold:          ed.SketchHoldState(),
			hasReading:    ed.SketchReading() != nil,
			readingItems:  rItems,
			readingAssump: rAssump,
			reviewOpen:    ed.SketchReviewOpen(),
			reviewChanges: len(ed.SketchReviewChanges()),
			reviewOverlay: ed.SketchReviewOverlayVisible(),
			previewOpen:   ed.SketchPreviewOpen(),
			attached:      ed.SketchAttached(),
			marksDigest:   b.String(),
		}
	})

	type panelThemeKey struct {
		dark bool
		lang string
	}
	darkMemo := core.NewMemo(cx, func() panelThemeKey {
		_ = rev.Get()
		return panelThemeKey{
			dark: ed.SpecPalette(cx).Dark,
			lang: ed.ResolvedLanguage(),
		}
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := SpecPaletteFor(darkMemo.Get().dark)
		return core.View{
			TestID: "sketch-agent-panel",
			Role:   core.RoleRegion,
			Label:  "Agent Sketch",
			Style: core.Style{
				Width:       core.Pt(360),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
				Overflow:    core.Hidden,
			},
			Children: []core.Node{
				// Panel Header ("Agent" + MCP dot + Inspector toggle).
				core.View{
					Style: core.Style{
						Height:      core.Pt(44),
						FlexShrink:  0,
						Direction:   core.Row,
						AlignItems:  core.AlignCenter,
						Gap:         8,
						Padding:     core.Edges{Left: 16, Right: 8},
						Borders:     core.Edges{Bottom: 1},
						BorderColor: p.Line,
					},
					Children: []core.Node{
						core.Text{
							Content: "Agent",
							Style: core.Style{
								FontSize:   13,
								FontWeight: 600,
								Color:      p.TextPrimary,
								FlexGrow:   1,
							},
						},
						core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        6,
								Padding:    core.Symmetric(0, 6),
							},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:        core.Pt(6),
										Height:       core.Pt(6),
										CornerRadius: 3,
										Background:   p.Added.Fg,
									},
								},
								core.Text{
									Content: "MCP",
									Style: core.Style{
										FontSize: 11.5,
										Color:    p.TextFaint,
									},
								},
							},
						},
						ui.Button{
							TestID:  "sketch-panel-close",
							Icon:    lucide.X,
							Label:   "Show Inspector",
							Size:    ui.SizeSmall,
							Variant: ui.ButtonGhost,
							Side:    28,
							Tint:    func() core.Color { return p.TextMuted },
							Style: core.Style{
								CornerRadius: 6,
								FocusVisible: p.FocusRing(2),
							},
							OnPress: func() {
								ed.SetSketchPanelOpen(false)
							},
						},
					},
				},

				// Scrollable body: Review card / Reading card / Marks list.
				ui.ScrollView{
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
					},
					Children: []core.Node{
						core.Dynamic(func(cx *core.Scope) core.Node {
							_ = stateKey.Get()
							pal := ed.SpecPalette(cx)
							var items []core.Node

							// 1. Post-sketch Review card (Sketch overlay toggle + Keep / Revert).
							if ed.SketchReviewOpen() {
								items = append(items, sketchReviewCardView(ed, pal))
							}

							// 2. Agent Reading card ("I read your sketch as:" + assumptions + Go ahead / Correct a mark).
							if rd := ed.SketchReading(); rd != nil {
								items = append(items, sketchReadingCardView(ed, pal, rd))
							} else if ed.SketchHoldState() == SketchHoldPending {
								items = append(items, core.View{
									TestID: "sketch-waiting-reading",
									Style: core.Style{
										CornerRadius: 12,
										Background:   pal.Warning.Fill,
										BorderWidth:  1,
										BorderColor:  pal.Warning.Edge,
										Padding:      core.Symmetric(10, 12),
										Gap:          4,
									},
									Children: []core.Node{
										core.Text{
											Content: "Waiting for the agent to read your sketch",
											Style:   core.Style{FontSize: 12, FontWeight: 600, Color: pal.Warning.Fg},
										},
										core.Text{
											Content: "Scene edits are held until the agent restates each mark and you choose Go ahead.",
											Style:   core.Style{FontSize: 11.5, Color: pal.TextSecondary},
										},
									},
								})
							}

							// 3. Active marks list when not in review and no reading card.
							marks := ed.SketchMarks()
							if len(marks) > 0 && !ed.SketchReviewOpen() && ed.SketchReading() == nil {
								sel := ed.SelectedSketchMark()
								rows := make([]core.Node, 0, len(marks))
								for _, mk := range marks {
									m := mk
									isSel := m.Number == sel
									rowBg := pal.Raised
									rowBorder := pal.LineRaised
									if isSel {
										rowBorder = pal.Accent.Edge
									}
									rows = append(rows, core.View{
										TestID: fmt.Sprintf("sketch-panel-mark-%d", m.Number),
										Role:   core.RoleButton,
										Label:  fmt.Sprintf("Mark %d %s", m.Number, m.Kind.String()),
										OnPress: func(core.PressEvent) {
											ed.SelectSketchMark(m.Number)
											if !ed.SketchActive() {
												ed.StartSketch()
											}
										},
										Style: core.Style{
											Direction:    core.Row,
											AlignItems:   core.AlignCenter,
											Gap:          8,
											Padding:      core.Symmetric(8, 10),
											CornerRadius: 8,
											Background:   rowBg,
											BorderWidth:  1,
											BorderColor:  rowBorder,
											FocusVisible: pal.FocusRing(-2),
										},
										Children: []core.Node{
											core.View{
												Style: core.Style{
													Width:          core.Pt(18),
													Height:         core.Pt(18),
													FlexShrink:     0,
													CornerRadius:   9,
													Background:     pal.TextPrimary,
													AlignItems:     core.AlignCenter,
													JustifyContent: core.JustifyCenter,
												},
												Children: []core.Node{
													core.Text{
														Content: strconv.Itoa(m.Number),
														Style:   core.Style{FontSize: 11, FontWeight: 600, Color: pal.OnTextPrimary},
													},
												},
											},
											core.View{
												Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
												Children: []core.Node{
													core.Text{
														Content: m.GeometrySummary(),
														Style: core.Style{
															FontFamily: core.FontMonospace,
															FontSize:   11.5,
															Color:      pal.TextMuted,
														},
													},
													core.Show{
														When: func() bool { return m.Label != "" },
														Then: func(cx *core.Scope) core.Node {
															return core.Text{
																Content: fmt.Sprintf("“%s”", m.Label),
																Style:   core.Style{FontSize: 11.5, Color: pal.TextSecondary},
															}
														},
													},
												},
											},
										},
									})
								}
								items = append(items, core.View{
									TestID:   "sketch-panel-marks-list",
									Style:    core.Style{Gap: 6},
									Children: rows,
								})
							}

							// 4. "Sent with this request" scene-coordinates preview card.
							if ed.SketchPreviewOpen() {
								items = append(items, sketchPreviewCardView(ed, pal))
							}

							if len(items) == 0 {
								items = append(items, core.Text{
									TestID:  "sketch-panel-empty",
									Content: "Draw on the viewport with Pen, Arrow, Box or Text. Marks stay in scene coordinates and are never written to the scene file.",
									Style: core.Style{
										FontSize:   12,
										LineHeight: 18,
										Color:      pal.TextFaint,
									},
								})
							}

							return core.View{
								Style: core.Style{
									Padding: core.All(12),
									Gap:     12,
								},
								Children: items,
							}
						}),
					},
				},

				// Composer footer with "Sketch · N marks" chip and Send button.
				sketchComposerView(cx, ed, rev, p),
			},
		}
	})
}

func sketchReadingCardView(ed *Editor, p SpecPalette, rd *SketchReading) core.Node {
	var children []core.Node

	if req, ok := ed.LastSketchRequest(); ok && req.Prompt != "" {
		children = append(children, core.View{
			TestID: "sketch-reading-prompt",
			Style: core.Style{
				Padding:      core.Symmetric(8, 10),
				CornerRadius: 10,
				Background:   p.Inset,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Gap:          4,
			},
			Children: []core.Node{
				core.Text{
					Content: req.ChipLabel,
					Style:   core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint},
				},
				core.Text{
					Content: req.Prompt,
					Style:   core.Style{FontSize: 12, Color: p.TextPrimary},
				},
			},
		})
	}

	children = append(children, core.Text{
		TestID:  "sketch-reading-heading",
		Content: rd.Summary,
		Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: p.TextSecondary},
	})

	for _, item := range rd.Items {
		it := item
		children = append(children, core.View{
			TestID: fmt.Sprintf("sketch-reading-item-%d", it.Number),
			Role:   core.RoleButton,
			Label:  fmt.Sprintf("Correct mark %d: %s", it.Number, it.Mark),
			OnPress: func(core.PressEvent) {
				ed.report(ed.CorrectSketchMark(it.Number))
			},
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignStart,
				Gap:          8,
				Padding:      core.Symmetric(4, 4),
				CornerRadius: 8,
				FocusVisible: p.FocusRing(-2),
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:          core.Pt(18),
						Height:         core.Pt(18),
						FlexShrink:     0,
						CornerRadius:   9,
						Background:     p.TextPrimary,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.Text{
							Content: strconv.Itoa(it.Number),
							Style:   core.Style{FontSize: 11, FontWeight: 600, Color: p.OnTextPrimary},
						},
					},
				},
				core.View{
					Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
					Children: []core.Node{
						core.Text{
							Content: it.Mark,
							Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
						},
						core.Text{
							Content: it.Text,
							Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
						},
					},
				},
			},
		})
	}

	if len(rd.Assumptions) > 0 {
		assumpNodes := make([]core.Node, 0, len(rd.Assumptions))
		for i, a := range rd.Assumptions {
			assumpNodes = append(assumpNodes, core.Text{
				TestID:  fmt.Sprintf("sketch-reading-assumption-%d", i),
				Content: a,
				Style:   core.Style{FontSize: 12, Color: p.Warning.Fg},
			})
		}
		children = append(children, core.View{
			TestID: "sketch-reading-assumptions",
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Warning.Fill,
				BorderWidth:  1,
				BorderColor:  p.Warning.Edge,
				Padding:      core.Symmetric(8, 12),
				Gap:          4,
			},
			Children: assumpNodes,
		})
	}

	if ed.SketchHoldActive() {
		correctTarget := ed.SelectedSketchMark()
		if correctTarget <= 0 && len(rd.Items) > 0 {
			correctTarget = rd.Items[0].Number
		}
		if correctTarget <= 0 {
			correctTarget = 1
		}
		children = append(children, core.View{
			Style: core.Style{
				Direction: core.Row,
				Gap:       8,
			},
			Children: []core.Node{
				ui.Button{
					TestID:  "sketch-reading-go-ahead",
					Text:    "Go ahead",
					Label:   "Go ahead",
					Size:    ui.SizeSmall,
					Variant: ui.ButtonGhost,
					Color:   func() core.Color { return p.TextPrimary },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 600,
						Color:      p.OnTextPrimary,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						Padding:      core.Symmetric(0, 12),
						CornerRadius: 8,
						FocusVisible: p.FocusRing(2),
					},
					OnPress: func() {
						ed.report(ed.ApproveSketchReading())
					},
				},
				ui.Button{
					TestID:  "sketch-reading-correct",
					Text:    "Correct a mark",
					Label:   "Correct a mark",
					Size:    ui.SizeSmall,
					Variant: ui.ButtonGhost,
					Color:   func() core.Color { return p.LineRaised },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      p.TextPrimary,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						Padding:      core.Symmetric(0, 12),
						CornerRadius: 8,
						FocusVisible: p.FocusRing(2),
					},
					OnPress: func() {
						ed.report(ed.CorrectSketchMark(correctTarget))
					},
				},
			},
		})
	} else if ed.SketchApproved() {
		children = append(children, core.Text{
			TestID:  "sketch-reading-approved-note",
			Content: "Approved · agent may edit the scene.",
			Style:   core.Style{FontSize: 11.5, Color: p.Added.Fg},
		})
	}

	return core.View{
		TestID: "sketch-reading-card",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(12),
			Gap:          10,
		},
		Children: children,
	}
}

func sketchReviewCardView(ed *Editor, p SpecPalette) core.Node {
	changes := ed.SketchReviewChanges()
	countLabel := fmt.Sprintf("%d changes", len(changes))
	if len(changes) == 1 {
		countLabel = "1 change"
	}
	overlayOn := ed.SketchReviewOverlayVisible()
	overlayBg := core.Transparent
	overlayFg := p.TextMuted
	if overlayOn {
		overlayBg = p.LineRaised
		overlayFg = p.TextPrimary
	}

	var rows []core.Node
	for i, ch := range changes {
		swatchCol := p.Changed.Fg
		prefix := "changed"
		if ch.Kind == SketchChangeAdded {
			swatchCol = p.Added.Fg
			prefix = "added"
		} else if ch.Kind == SketchChangeRemoved {
			swatchCol = p.Removed.Fg
			prefix = "removed"
		}
		rows = append(rows, core.View{
			TestID: fmt.Sprintf("sketch-review-change-%d", i),
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(10),
						Height:       core.Pt(10),
						FlexShrink:   0,
						CornerRadius: 2,
						BorderWidth:  1.5,
						BorderColor:  swatchCol,
					},
				},
				core.Text{
					Content: fmt.Sprintf("%s · %s", prefix, ch.ID),
					Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
				},
			},
		})
	}
	if ed.SketchMarkCount() > 0 {
		rows = append(rows, core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:      core.Pt(10),
						Height:     core.Pt(2),
						FlexShrink: 0,
						Background: ui.Alpha(p.TextPrimary, 0.6),
					},
				},
				core.Text{
					Content: "your sketch",
					Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
				},
			},
		})
	}

	return core.View{
		TestID: "sketch-review-card",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Overflow:     core.Hidden,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Height:      core.Pt(40),
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         8,
					Padding:     core.Edges{Left: 12, Right: 8},
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.Line,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "sketch-review-count",
						Content: countLabel,
						Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.TextPrimary, FlexGrow: 1},
					},
					ui.Button{
						TestID:   "sketch-review-overlay-toggle",
						Icon:     lucide.Pencil,
						IconSize: 13,
						Text:     "Sketch overlay",
						Label:    "Toggle sketch overlay",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonGhost,
						Color:    func() core.Color { return overlayBg },
						Tint:     func() core.Color { return overlayFg },
						TextStyle: core.Style{
							FontSize:   11.5,
							FontWeight: 500,
							Color:      overlayFg,
						},
						Style: core.Style{
							Height:       core.Pt(28),
							Padding:      core.Symmetric(0, 8),
							CornerRadius: 6,
							FocusVisible: p.FocusRing(2),
						},
						OnPress: func() {
							ed.ToggleSketchReviewOverlay()
						},
					},
				},
			},
			core.View{
				Style:    core.Style{Padding: core.All(12), Gap: 6},
				Children: rows,
			},
			core.View{
				Style: core.Style{
					Direction:   core.Row,
					Gap:         8,
					Padding:     core.All(12),
					Borders:     core.Edges{Top: 1},
					BorderColor: p.Line,
				},
				Children: []core.Node{
					ui.Button{
						TestID:  "sketch-review-keep",
						Text:    "Keep",
						Label:   "Keep changes and clear sketch",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Color:   func() core.Color { return p.Accent.Fg },
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: 600,
							Color:      p.Accent.OnFill,
						},
						Style: core.Style{
							FlexGrow:     1,
							Height:       core.Pt(30),
							CornerRadius: 8,
							FocusVisible: p.FocusRing(2),
						},
						OnPress: func() {
							ed.report(ed.KeepSketchReview())
						},
					},
					ui.Button{
						TestID:  "sketch-review-revert",
						Text:    "Revert",
						Label:   "Revert changes and keep sketch",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Color:   func() core.Color { return p.LineRaised },
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      p.TextSecondary,
						},
						Style: core.Style{
							Height:       core.Pt(30),
							Padding:      core.Symmetric(0, 12),
							CornerRadius: 8,
							FocusVisible: p.FocusRing(2),
						},
						OnPress: func() {
							ed.report(ed.RevertSketchReview())
						},
					},
				},
			},
		},
	}
}

func sketchPreviewCardView(ed *Editor, p SpecPalette) core.Node {
	marks := ed.SketchMarks()
	rows := make([]core.Node, 0, len(marks))
	for _, m := range marks {
		var labelNode core.Node
		if m.Label != "" {
			labelNode = core.Text{
				Content: fmt.Sprintf("“%s”", m.Label),
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      p.TextFaint,
					Padding:    core.Edges{Left: 20},
				},
			}
		}
		children := []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, Gap: 8},
				Children: []core.Node{
					core.Text{
						Content: strconv.Itoa(m.Number),
						Style: core.Style{
							Width:      core.Pt(12),
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      p.TextPrimary,
						},
					},
					core.Text{
						Content: m.GeometrySummary(),
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      p.TextMuted,
						},
					},
				},
			},
		}
		if labelNode != nil {
			children = append(children, labelNode)
		}
		rows = append(rows, core.View{
			TestID:   fmt.Sprintf("sketch-preview-mark-%d", m.Number),
			Style:    core.Style{Gap: 2},
			Children: children,
		})
	}
	return core.View{
		TestID: "sketch-sent-preview",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(12),
			Gap:          8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween},
				Children: []core.Node{
					core.Text{
						Content: "Sent with this request",
						Style:   core.Style{FontSize: 13, FontWeight: 600, Color: p.TextPrimary},
					},
					core.Text{
						Content: "scene coordinates",
						Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
					},
				},
			},
			core.View{
				Style:    core.Style{Gap: 6},
				Children: rows,
			},
		},
	}
}

func sketchComposerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p SpecPalette) core.Node {
	submit := func() {
		text := strings.TrimSpace(ed.SketchPromptDraft())
		if ed.SketchHoldState() == SketchHoldReading && text != "" {
			ed.report(ed.ReplyToSketchReading(text))
			return
		}
		if ed.SketchAttached() && ed.SketchMarkCount() > 0 {
			if _, err := ed.SendSketch(text); err != nil {
				ed.report(err)
			}
		}
	}

	return core.View{
		TestID: "sketch-composer",
		Style: core.Style{
			FlexShrink:  0,
			Padding:     core.All(12),
			Borders:     core.Edges{Top: 1},
			BorderColor: p.Line,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					CornerRadius: 12,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Padding:      core.Symmetric(8, 10),
					Gap:          8,
				},
				Children: []core.Node{
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.SketchAttached()
						},
						Then: func(cx *core.Scope) core.Node {
							pal := ed.SpecPalette(cx)
							return core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
								Children: []core.Node{
									core.View{
										StyleFunc: func() core.Style {
											_ = rev.Get()
											border := pal.LineRaised
											if ed.SketchPreviewOpen() {
												border = pal.Accent.Edge
											}
											return core.Style{
												Height:       core.Pt(28),
												Direction:    core.Row,
												AlignItems:   core.AlignCenter,
												Gap:          4,
												Padding:      core.Edges{Left: 8, Right: 2},
												CornerRadius: 6,
												Background:   pal.LineRaised,
												BorderWidth:  1,
												BorderColor:  border,
											}
										},
										Children: []core.Node{
											ui.Button{
												TestID:  "sketch-context-chip",
												Icon:    lucide.Pencil,
												Variant: ui.ButtonGhost,
												Size:    ui.SizeSmall,
												TextFunc: func() string {
													_ = rev.Get()
													return ed.SketchChipLabel()
												},
												LabelFunc: func() string {
													_ = rev.Get()
													return ed.SketchChipLabel() + ". Show what is sent"
												},
												IconSize: 12,
												Tint:     func() core.Color { return pal.TextPrimary },
												TextStyle: core.Style{
													FontSize:   11.5,
													FontWeight: 500,
													Color:      pal.TextPrimary,
												},
												Style: core.Style{
													Height:       core.Pt(24),
													Padding:      core.Symmetric(0, 4),
													CornerRadius: 4,
													FocusVisible: pal.FocusRing(2),
												},
												OnPress: func() {
													ed.ToggleSketchPreview()
												},
											},
											ui.Button{
												TestID:   "sketch-chip-remove",
												Icon:     lucide.X,
												IconSize: 11,
												Label:    "Remove the sketch from the request",
												Size:     ui.SizeSmall,
												Variant:  ui.ButtonGhost,
												Side:     20,
												Tint:     func() core.Color { return pal.TextMuted },
												Style: core.Style{
													CornerRadius: 4,
													FocusVisible: pal.FocusRing(2),
												},
												OnPress: func() {
													ed.SetSketchAttached(false)
												},
											},
										},
									},
								},
							}
						},
					},
					specInputField(cx, ed, ui.Input{
						TestID:      "sketch-composer-input",
						Label:       "Ask the agent",
						Placeholder: "Build what I sketched...",
						Value: func() string {
							_ = rev.Get()
							return ed.SketchPromptDraft()
						},
						OnChange: func(v string) {
							ed.SetSketchPromptDraft(v)
						},
						OnSubmit: func(string) {
							submit()
						},
					}),
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        4,
						},
						Children: []core.Node{
							ui.Button{
								TestID:   "sketch-composer-sketch-btn",
								Icon:     lucide.Pencil,
								IconSize: 14,
								Label:    "Sketch on the viewport · D",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonGhost,
								Side:     28,
								Color: func() core.Color {
									_ = rev.Get()
									if ed.SketchActive() {
										return p.LineRaised
									}
									return core.Transparent
								},
								Tint: func() core.Color {
									_ = rev.Get()
									if ed.SketchActive() {
										return p.TextPrimary
									}
									return p.TextMuted
								},
								Style: core.Style{
									CornerRadius: 6,
									FocusVisible: p.FocusRing(2),
								},
								OnPress: func() {
									ed.ToggleSketch()
								},
							},
							core.View{Style: core.Style{FlexGrow: 1}},
							ui.Button{
								TestID:   "sketch-composer-send",
								Icon:     lucide.ArrowUp,
								IconSize: 14,
								Label:    "Send sketch to agent",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonGhost,
								Side:     30,
								Disabled: func() bool {
									_ = rev.Get()
									if ed.SketchHoldState() == SketchHoldReading {
										return strings.TrimSpace(ed.SketchPromptDraft()) == ""
									}
									return !ed.SketchAttached() || ed.SketchMarkCount() == 0
								},
								Color: func() core.Color { return p.TextPrimary },
								Tint:  func() core.Color { return p.OnTextPrimary },
								Style: core.Style{
									CornerRadius: 8,
									FocusVisible: p.FocusRing(2),
								},
								OnPress: submit,
							},
						},
					},
				},
			},
		},
	}
}
