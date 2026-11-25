package editor

import (
	"fmt"
	"math"
	"path"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/cutscene"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	cutsceneGutterW  float32 = 112
	cutsceneLaneW    float32 = 624
	cutsceneTrackH   float32 = 36
	cutscenePreviewW float32 = 576
	cutscenePreviewH float32 = 324
)

// Spec-derived translucent fills using exclusively Spec.dc.html token hues.
var (
	specChangedFill  = core.RGBA(0xE9A84A, 0.14)
	specRemovedFill  = core.RGBA(0xFF6B6B, 0.14)
	specAccentFill   = core.RGBA(0x3B7BFF, 0.16)
	specDialogueFill = core.RGBA(0x3B7BFF, 0.10)
	specAudioFill    = core.RGBA(0x4FD38A, 0.10)
	specSubtitleBg   = core.RGBA(0x101216, 0.88)
)

// CutscenePanelView renders the full-workspace cutscene timeline and agent review screen (matching docs/design/editor/Animation.dc.html Screen 2 and Spec.dc.html tokens) whenever a cutscene is open and Play mode is not active.
func CutscenePanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.CutsceneOpen() && !ed.Playing()
		},
		Then: func(cx *core.Scope) core.Node { return cutscenePanel(cx, ed, rev) },
	}
}

func cutscenePanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	topBar := cutsceneTopBar(cx, ed, rev)
	leftRail := cutsceneLeftRail(cx, ed, rev)
	centerCol := cutsceneCenterColumn(cx, ed, rev)
	rightRail := cutsceneRightRail(cx, ed, rev)

	return core.View{
		TestID: "cutscene-panel",
		OnKeyDown: func(ev core.KeyEvent) bool {
			return ed.CutsceneHandleKeyEvent(ev, true)
		},
		OnKeyUp: func(ev core.KeyEvent) {
			ed.CutsceneHandleKeyEvent(ev, false)
		},
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			topBar,
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.CutsceneError() != nil
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{
							Padding:     core.Symmetric(6, 12),
							Background:  specPanel,
							Borders:     core.Edges{Bottom: 1},
							BorderColor: specLine,
						},
						Children: []core.Node{
							core.Text{
								TestID: "cutscene-error",
								ContentFunc: func() string {
									_ = rev.Get()
									if err := ed.CutsceneError(); err != nil {
										return "Preview: " + err.Error()
									}
									return ""
								},
								Style: core.Style{FontSize: 11.5, Color: specProblem},
							},
						},
					}
				},
			},
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					leftRail,
					centerCol,
					rightRail,
				},
			},
		},
	}
}

func cutsceneTopBar(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.View{
		TestID: "cutscene-topbar",
		Style: core.Style{
			Height:         core.Pt(44),
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.Symmetric(0, 12),
			Gap:            12,
			Background:     specBg,
			Borders:        core.Edges{Bottom: 1},
			BorderColor:    specLine,
		},
		Children: []core.Node{
			// Left breadcrumb: project / cutscenes / file.cutscene.yaml
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(22),
							Height:         core.Pt(22),
							CornerRadius:   6,
							Background:     specAccent,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Text{
								Content: "A",
								Style:   core.Style{FontSize: 11, FontWeight: 700, Color: specBg},
							},
						},
					},
					core.Text{
						TestID: "cutscene-breadcrumb-project",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.Project != nil && ed.Project.Name != "" {
								return ed.Project.Name
							}
							if ed.Doc != nil && ed.Doc.Name != "" {
								return ed.Doc.Name
							}
							return "project"
						},
						Style: core.Style{FontSize: 12.5, FontWeight: 600, Color: specTextStrong},
					},
					core.Text{Content: "/", Style: core.Style{FontSize: 12.5, Color: specTextMuted}},
					core.Text{Content: "cutscenes", Style: core.Style{FontSize: 12.5, Color: specTextSecondary}},
					core.Text{Content: "/", Style: core.Style{FontSize: 12.5, Color: specTextMuted}},
					core.Text{
						TestID: "cutscene-breadcrumb-file",
						ContentFunc: func() string {
							_ = rev.Get()
							return cutsceneFileBaseName(ed)
						},
						Style: core.Style{FontSize: 12.5, Color: specTextSecondary},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.CutsceneDirty()
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID: "cutscene-dirty-dot",
								Style: core.Style{
									Width:        core.Pt(6),
									Height:       core.Pt(6),
									CornerRadius: 3,
									Background:   specChanged,
								},
							}
						},
					},
				},
			},
			// Right review controls or Save/Close buttons
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.CutsceneReviewOpen()
				},
				Then: func(cx *core.Scope) core.Node {
					return ui.HStack{
						Gap: 10,
						Children: []core.Node{
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.CutsceneReviewPrompt() != ""
								},
								Then: func(cx *core.Scope) core.Node {
									return core.Text{
										TestID: "cutscene-review-prompt",
										ContentFunc: func() string {
											_ = rev.Get()
											return "from “" + ed.CutsceneReviewPrompt() + "”"
										},
										Style: core.Style{FontSize: 12, Color: specTextMuted},
									}
								},
							},
							// Before / After segmented toggle
							core.View{
								TestID: "cutscene-review-toggle",
								Style: core.Style{
									Direction:    core.Row,
									Gap:          2,
									Padding:      core.All(3),
									CornerRadius: 8,
									Background:   specRaised,
									BorderWidth:  1,
									BorderColor:  specLineRaised,
								},
								Children: []core.Node{
									cutsceneSegButton("cutscene-review-before", "Before", func() bool {
										_ = rev.Get()
										return ed.CutsceneReviewShowingBefore()
									}, func() {
										ed.SetCutsceneReviewViewMode(CutsceneReviewBefore)
									}),
									cutsceneSegButton("cutscene-review-after", "After", func() bool {
										_ = rev.Get()
										return !ed.CutsceneReviewShowingBefore()
									}, func() {
										ed.SetCutsceneReviewViewMode(CutsceneReviewAfter)
									}),
								},
							},
							core.Text{
								TestID:  "cutscene-peek-hint",
								Content: "hold B to peek",
								Style:   core.Style{FontSize: 11, Color: specTextMuted},
							},
							core.View{
								Style: core.Style{
									Width:      core.Pt(1),
									Height:     core.Pt(22),
									Background: specLineRaised,
								},
							},
							cutsceneActionButton("cutscene-review-revert-all", func() string { return "Revert all" }, false, func() bool {
								_ = rev.Get()
								return ed.Playing()
							}, func() {
								ed.report(ed.RevertAllCutsceneReview())
							}),
							cutsceneActionButton("cutscene-review-keep", func() string {
								_ = rev.Get()
								kept, total := ed.CutsceneReviewKeptCount()
								return fmt.Sprintf("Keep %d of %d", kept, total)
							}, true, func() bool {
								_ = rev.Get()
								return ed.Playing()
							}, func() {
								ed.report(ed.KeepCutsceneReview())
							}),
						},
					}
				},
				Else: func(cx *core.Scope) core.Node {
					return ui.HStack{
						Gap: 8,
						Children: []core.Node{
							cutsceneActionButton("cutscene-save", func() string { return "Save" }, false, func() bool {
								_ = rev.Get()
								return ed.Playing()
							}, func() {
								ed.report(ed.saveCutsceneFromPanel())
							}),
							cutsceneActionButton("cutscene-close", func() string { return "Close" }, false, func() bool {
								_ = rev.Get()
								return ed.Playing()
							}, func() {
								ed.report(ed.CloseCutscene())
							}),
						},
					}
				},
			},
		},
	}
}

func cutsceneLeftRail(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type railEpoch struct {
		projectPath string
		filesRev    uint64
		structRev   uint64
	}
	epoch := core.NewMemo(cx, func() railEpoch {
		_ = rev.Get()
		return railEpoch{ed.ProjectPath, ed.filesRev, ed.cutscene.structRev}
	})
	railKey := core.NewMemo(cx, func() string {
		_ = epoch.Get()
		var b strings.Builder
		for _, c := range ed.ProjectCutscenes() {
			fmt.Fprintf(&b, "c:%s:%s:%v:%v|", c.Path, c.Kind, c.Length, c.InReview)
		}
		for _, ln := range ed.CutsceneLines() {
			fmt.Fprintf(&b, "l:%s:%s:%s:%v|", ln.ID, ln.En, ln.Zh, ln.Added)
		}
		fmt.Fprintf(&b, "open:%s|before:%v", ed.CutscenePath(), ed.CutsceneReviewShowingBefore())
		return b.String()
	})

	return core.View{
		TestID: "cutscene-left-rail",
		Style: core.Style{
			Width:       core.Pt(232),
			FlexShrink:  0,
			Background:  specBg,
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
			Overflow:    core.Hidden,
		},
		Children: []core.Node{
			ui.ScrollView{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = railKey.Get()
						entries := ed.ProjectCutscenes()
						lines := ed.CutsceneLines()
						enPath, _ := ed.CutsceneLocPaths()

						openName := cutsceneDisplayName(cutsceneFileBaseName(ed))
						cutRows := make([]core.Node, 0, len(entries))
						for i, entry := range entries {
							ent := entry
							active := ent.Name == openName
							meta := formatCutsceneSec(ent.Length)
							if ent.Kind == cutscene.KindVideo {
								if ent.Length > 0 {
									meta = fmt.Sprintf("video · %s", formatCutsceneSecCompact(ent.Length))
								} else {
									meta = "video"
								}
							}
							var bg core.Color
							nameCol := specTextSecondary
							if active {
								bg = specActiveNode
								nameCol = specTextStrong
							}
							cutRows = append(cutRows, core.View{
								TestID: fmt.Sprintf("cutscene-list-%s", ent.Name),
								Role:   core.RoleButton,
								Label:  fmt.Sprintf("Cutscene %s (%s)", ent.Name, meta),
								OnPress: func(core.PressEvent) {
									if ent.Path != "" && ed.Project != nil && !active {
										ed.report(ed.OpenCutsceneProjectFile(ent.Path))
									}
								},
								Style: core.Style{
									Height:         core.Pt(28),
									Direction:      core.Row,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifySpaceBetween,
									Padding:        core.Symmetric(0, 8),
									Gap:            8,
									CornerRadius:   6,
									Background:     bg,
									FocusVisible:   specRowFocusRingStyle,
								},
								Children: []core.Node{
									core.Text{
										Content: ent.Name,
										Style:   core.Style{FontSize: 12.5, Color: nameCol, FlexGrow: 1},
									},
									core.Text{
										TestID:  fmt.Sprintf("cutscene-list-meta-%d", i),
										Content: meta,
										Style:   core.Style{FontSize: 11, Color: specTextMuted},
									},
								},
							})
						}

						lineNodes := make([]core.Node, 0, len(lines))
						for _, ln := range lines {
							idCol := specTextMuted
							if ln.Added {
								idCol = specAdded
							}
							enText := ln.En
							if enText == "" {
								enText = "(missing English text)"
							}
							zhText := ln.Zh
							if zhText == "" {
								zhText = "(missing Chinese text)"
							}
							lineNodes = append(lineNodes, core.View{
								TestID: "cutscene-rail-line-" + ln.ID,
								Style:  core.Style{Gap: 2},
								Children: []core.Node{
									core.Text{
										Content: ln.ID,
										Style:   core.Style{FontSize: 11.5, Color: idCol},
									},
									core.Text{
										Content: enText,
										Style:   core.Style{FontSize: 12, Color: specText},
									},
									core.Text{
										Content: zhText,
										Style:   core.Style{FontSize: 12, Color: specTextMuted},
									},
								},
							})
						}
						if len(lineNodes) == 0 {
							lineNodes = append(lineNodes, core.Text{
								Content: "No dialogue lines in this cutscene",
								Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
							})
						}

						return core.View{
							Style: core.Style{Padding: core.Edges{Bottom: 12}},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Height:         core.Pt(36),
										Padding:        core.Symmetric(0, 14),
										JustifyContent: core.JustifyCenter,
									},
									Children: []core.Node{
										core.Text{
											Content: "CUTSCENES",
											Style:   core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
										},
									},
								},
								core.View{
									Style:    core.Style{Padding: core.Symmetric(0, 6), Gap: 2},
									Children: cutRows,
								},
								core.View{
									Style: core.Style{Padding: core.Edges{Top: 16, Left: 14, Right: 14, Bottom: 6}},
									Children: []core.Node{
										core.Text{
											TestID:  "cutscene-lines-header",
											Content: "LINES · " + enPath,
											Style:   core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
										},
									},
								},
								core.View{
									Style:    core.Style{Padding: core.Symmetric(0, 14), Gap: 12},
									Children: lineNodes,
								},
							},
						}
					}),
				},
			},
		},
	}
}

func cutsceneCenterColumn(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.View{
		TestID: "cutscene-center",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			cutscenePreviewStage(cx, ed, rev),
			cutsceneTimelineDrawer(cx, ed, rev),
		},
	}
}

func cutscenePreviewStage(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.View{
		TestID: "cutscene-preview-stage",
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Position:   core.Relative,
			Background: specViewportBG,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			// Dot-grid viewport background matching Spec .vp
			core.Canvas{
				Style: core.Style{
					Position: core.Absolute,
					Top:      core.Pt(0),
					Left:     core.Pt(0),
					Right:    core.Pt(0),
					Bottom:   core.Pt(0),
				},
				Draw: func(d *core.Drawing, size core.Size) {
					drawDotGrid(d, size)
				},
			},
			// Foreground preview frame + camera label + subtitle + review legend
			core.View{
				Style: core.Style{
					Position:   core.Absolute,
					Top:        core.Pt(10),
					Left:       core.Pt(24),
					Right:      core.Pt(16),
					Bottom:     core.Pt(10),
					Direction:  core.Row,
					AlignItems: core.AlignStart,
					Gap:        16,
				},
				Children: []core.Node{
					// Camera caption + 16:9 preview box
					core.View{
						Style: core.Style{Gap: 6},
						Children: []core.Node{
							core.Text{
								TestID: "cutscene-camera-label",
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.CutsceneCameraLabel()
								},
								Style: core.Style{FontSize: 11, Color: specTextMuted},
							},
							core.View{
								TestID: "cutscene-preview-frame",
								Style: core.Style{
									Width:        core.Pt(core.Points(cutscenePreviewW)),
									Height:       core.Pt(core.Points(cutscenePreviewH)),
									Position:     core.Relative,
									CornerRadius: 4,
									Background:   specPanel,
									BorderWidth:  1,
									BorderColor:  specLineRaised,
									Overflow:     core.Hidden,
								},
								Children: []core.Node{
									core.Canvas{
										TestID: "cutscene-preview-canvas",
										Style: core.Style{
											Width:  core.Pct(100),
											Height: core.Pct(100),
										},
										Draw: func(d *core.Drawing, size core.Size) {
											_ = rev.Get()
											drawCutscenePreview(d, size, ed)
										},
									},
									// Before-mode indicator pill
									core.Show{
										When: func() bool {
											_ = rev.Get()
											return ed.CutsceneReviewShowingBefore()
										},
										Then: func(cx *core.Scope) core.Node {
											return core.View{
												TestID: "cutscene-before-banner",
												Style: core.Style{
													Position:       core.Absolute,
													Top:            core.Pt(12),
													Left:           core.Pt(138),
													Height:         core.Pt(26),
													Padding:        core.Symmetric(0, 12),
													CornerRadius:   13,
													AlignItems:     core.AlignCenter,
													JustifyContent: core.JustifyCenter,
													Background:     specBgOverlay,
													BorderWidth:    1,
													BorderColor:    specLineRaised,
												},
												Children: []core.Node{
													core.Text{
														Content: "Showing the cutscene before this request",
														Style:   core.Style{FontSize: 11.5, Color: specTextSecondary},
													},
												},
											}
										},
									},
									// Subtitle bar
									core.Show{
										When: func() bool {
											_ = rev.Get()
											return ed.CutsceneActiveDialogue().Active
										},
										Then: func(cx *core.Scope) core.Node {
											return core.View{
												TestID: "cutscene-subtitle-wrap",
												Style: core.Style{
													Position:       core.Absolute,
													Left:           core.Pt(32),
													Right:          core.Pt(32),
													Bottom:         core.Pt(16),
													AlignItems:     core.AlignCenter,
													JustifyContent: core.JustifyCenter,
												},
												Children: []core.Node{
													core.View{
														Style: core.Style{
															Padding:      core.Symmetric(8, 16),
															CornerRadius: 8,
															Background:   specSubtitleBg,
														},
														Children: []core.Node{
															core.Text{
																TestID: "cutscene-subtitle",
																ContentFunc: func() string {
																	_ = rev.Get()
																	return ed.CutsceneActiveDialogue().Text
																},
																Style: core.Style{FontSize: 14, Color: specTextStrong},
															},
														},
													},
												},
											}
										},
									},
								},
							},
						},
					},
					// Right-side review line-style legend (matching Animation.dc.html lines 354-358)
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.CutsceneReviewOpen()
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID: "cutscene-review-legend",
								Style: core.Style{
									Padding: core.Edges{Top: 22},
									Gap:     8,
								},
								Children: []core.Node{
									cutsceneLegendRow("added", specAdded, true),
									cutsceneLegendRow("changed", specChanged, true),
									cutsceneLegendRow("selected", specAccent, false),
								},
							}
						},
					},
				},
			},
		},
	}
}

func cutsceneLegendRow(label string, col core.Color, dashed bool) core.Node {
	return ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Canvas{
				Style: core.Style{Width: core.Pt(12), Height: core.Pt(12)},
				Draw: func(d *core.Drawing, size core.Size) {
					d.Save()
					d.SetStrokeColor(col).SetLineWidth(1.5)
					if dashed {
						d.SetLineDash(3, 2)
					}
					d.BeginPath().RoundRect(1, 1, float32(size.Width)-2, float32(size.Height)-2, 2).Stroke()
					d.Restore()
				},
			},
			core.Text{
				Content: label,
				Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
			},
		},
	}
}

func cutsceneTimelineDrawer(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	structEpoch := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.cutscene.structRev
	})
	timelineStructure := core.NewMemo(cx, func() string {
		_ = structEpoch.Get()
		doc := ed.effectiveCutsceneDoc()
		if doc == nil {
			return ""
		}
		var b strings.Builder
		fmt.Fprintf(&b, "k=%s|len=%v|before=%v|", doc.EffectiveKind(), doc.Length, ed.CutsceneReviewShowingBefore())
		for ti, tr := range doc.Tracks {
			fmt.Fprintf(&b, "t%d:%s:%s|", ti, tr.Kind, tr.Name)
			for ci, cl := range tr.Clips {
				fmt.Fprintf(&b, "c%d:%s:%v:%v:%d|", ci, cl.ID, cl.Start, cl.Duration, ed.CutsceneClipReviewStatus(ti, ci))
			}
		}
		return b.String()
	})

	transport := core.View{
		TestID: "cutscene-transport",
		Style: core.Style{
			Height:         core.Pt(40),
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.Symmetric(0, 12),
			Gap:            12,
			Background:     specPanel,
			Borders:        core.Edges{Bottom: 1},
			BorderColor:    specLine,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 10,
				Children: []core.Node{
					core.View{
						TestID: "cutscene-play",
						Role:   core.RoleButton,
						Label:  "Play or Pause cutscene",
						OnPress: func(core.PressEvent) {
							if ed.Playing() {
								return
							}
							if ed.CutscenePlaying() {
								ed.PauseCutscene()
							} else {
								ed.report(ed.PlayCutscene())
							}
						},
						Style: core.Style{
							Height:         core.Pt(28),
							MinWidth:       core.Pt(56),
							Padding:        core.Symmetric(0, 10),
							CornerRadius:   8,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Background:     specLineRaised,
							FocusVisible:   specFocusRingStyle,
						},
						Children: []core.Node{
							core.Text{
								TestID: "cutscene-play-label",
								ContentFunc: func() string {
									_ = rev.Get()
									if ed.CutscenePlaying() {
										return "Pause"
									}
									return "Play"
								},
								Style: core.Style{FontSize: 12, FontWeight: 500, Color: specTextStrong},
							},
						},
					},
					core.Text{
						TestID: "cutscene-time",
						ContentFunc: func() string {
							_ = rev.Get()
							return formatCutsceneSec(ed.CutsceneTime())
						},
						Style: core.Style{FontSize: 11.5, FontWeight: 500, Color: specTextStrong},
					},
					core.Text{
						TestID: "cutscene-length",
						ContentFunc: func() string {
							_ = rev.Get()
							return "/ " + formatCutsceneSec(ed.CutsceneLength())
						},
						Style: core.Style{FontSize: 11.5, Color: specTextMuted},
					},
				},
			},
			core.Text{
				Content: "Click the strip to scrub",
				Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
			},
		},
	}

	body := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = timelineStructure.Get()
		doc := ed.effectiveCutsceneDoc()
		if doc == nil {
			return nil
		}
		if doc.EffectiveKind() == cutscene.KindVideo {
			return core.View{
				TestID: "cutscene-video-info",
				Style:  core.Style{Padding: core.All(16), Gap: 6},
				Children: []core.Node{
					core.Text{
						Content: "Video cutscene · " + doc.Source,
						Style:   core.Style{FontSize: 13, FontWeight: 600, Color: specTextStrong},
					},
					core.Text{
						Content: fmt.Sprintf("Fit: %s · Skippable: %v", effectiveFitLabel(doc.Fit), doc.Skippable),
						Style:   core.Style{FontSize: 12, Color: specTextMuted},
					},
				},
			}
		}

		lengthSec := time.Duration(doc.Length).Seconds()
		if lengthSec <= 0 {
			lengthSec = 1
		}
		pxPerSec := cutsceneLaneW / float32(lengthSec)

		// Thumbnail strip (24 clickable thumbnails).
		thumbs := ed.CutsceneThumbs()
		thumbW := (cutsceneLaneW - float32(cutsceneThumbCount-1)*2) / float32(cutsceneThumbCount)
		thumbNodes := make([]core.Node, cutsceneThumbCount)
		for i := range cutsceneThumbCount {
			idx := i
			tt := time.Duration(float64(i) / float64(cutsceneThumbCount) * float64(doc.Length))
			var sample *CutsceneThumbSample
			if idx < len(thumbs) {
				s := thumbs[idx]
				sample = &s
			}
			thumbNodes[i] = core.View{
				TestID: fmt.Sprintf("cutscene-thumb-%d", idx),
				Role:   core.RoleButton,
				Label:  fmt.Sprintf("Scrub to %s", formatCutsceneSec(tt)),
				OnPress: func(core.PressEvent) {
					ed.SeekCutscene(tt)
				},
				StyleFunc: func() core.Style {
					_ = rev.Get()
					step := time.Duration(doc.Length) / cutsceneThumbCount
					diff := ed.CutsceneTime() - tt
					if diff < 0 {
						diff = -diff
					}
					border := specLineRaised
					bw := core.Points(1)
					if diff <= step/2 {
						border = specAccent
						bw = 1.5
					}
					return core.Style{
						Width:        core.Pt(core.Points(thumbW)),
						Height:       core.Pt(38),
						CornerRadius: 4,
						Background:   specViewportBG,
						BorderWidth:  bw,
						BorderColor:  border,
						Overflow:     core.Hidden,
						FocusVisible: specFocusRingStyle,
					}
				},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, size core.Size) {
							drawCutsceneThumb(d, size, sample)
						},
					},
				},
			}
		}

		thumbRow := core.View{
			TestID: "cutscene-thumb-strip",
			Style: core.Style{
				Height:    core.Pt(38),
				Direction: core.Row,
			},
			Children: []core.Node{
				core.View{Style: core.Style{Width: core.Pt(core.Points(cutsceneGutterW))}},
				core.View{
					Style:    core.Style{Direction: core.Row, Gap: 2},
					Children: thumbNodes,
				},
			},
		}

		// Seconds ruler
		maxTick := int(math.Ceil(lengthSec))
		stepSec := 1
		if maxTick > 20 {
			stepSec = max(1, maxTick/12)
		}
		tickNodes := make([]core.Node, 0, maxTick+1)
		for s := 0; s <= maxTick; s += stepSec {
			x := float32(s) * pxPerSec
			if x > cutsceneLaneW-12 {
				x = cutsceneLaneW - 12
			}
			tickNodes = append(tickNodes, core.View{
				Style: core.Style{
					Position:    core.Absolute,
					Left:        core.Pt(core.Points(x)),
					Bottom:      core.Pt(2),
					Padding:     core.Edges{Left: 4},
					Borders:     core.Edges{Left: 1},
					BorderColor: specLineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: fmt.Sprintf("%d", s),
						Style:   core.Style{FontSize: 10.5, Color: specTextMuted},
					},
				},
			})
		}
		rulerRow := core.View{
			TestID: "cutscene-ruler",
			Style: core.Style{
				Height:    core.Pt(20),
				Direction: core.Row,
			},
			Children: []core.Node{
				core.View{Style: core.Style{Width: core.Pt(core.Points(cutsceneGutterW))}},
				core.View{
					Style: core.Style{
						Position: core.Relative,
						Width:    core.Pt(core.Points(cutsceneLaneW)),
						Height:   core.Pt(20),
					},
					Children: tickNodes,
				},
			},
		}

		// Track rows with positioned clips
		trackRows := make([]core.Node, 0, len(doc.Tracks))
		for ti, tr := range doc.Tracks {
			trackIdx := ti
			track := tr
			clipNodes := make([]core.Node, 0, len(track.Clips))
			for ci, cl := range track.Clips {
				clipIdx := ci
				clip := cl
				startSec := time.Duration(clip.Start).Seconds()
				durSec := time.Duration(clip.Duration).Seconds()
				cxPos := float32(startSec) * pxPerSec
				cw := max(float32(20), float32(durSec)*pxPerSec-3)
				clipLabel := cutsceneClipDisplayLabel(track.Kind, clip)
				testID := fmt.Sprintf("cutscene-clip-%d-%d", trackIdx, clipIdx)
				if clip.ID != "" {
					testID = "cutscene-clip-" + clip.ID
				}
				clipNodes = append(clipNodes, core.View{
					TestID: testID,
					Role:   core.RoleButton,
					Label:  fmt.Sprintf("%s, %s to %s", clipLabel, formatCutsceneSec(time.Duration(clip.Start)), formatCutsceneSec(time.Duration(clip.Start)+time.Duration(clip.Duration))),
					OnPress: func(core.PressEvent) {
						ed.SelectCutsceneClip(trackIdx, clipIdx)
					},
					Style: core.Style{
						Position:     core.Absolute,
						Left:         core.Pt(core.Points(cxPos)),
						Top:          core.Pt(4),
						Width:        core.Pt(core.Points(cw)),
						Height:       core.Pt(28),
						CornerRadius: 6,
						Overflow:     core.Hidden,
						FocusVisible: specFocusRingStyle,
					},
					Children: []core.Node{
						// Canvas background with dashed (added/changed) or solid (selected/normal) border per Spec legend.
						core.Canvas{
							Style: core.Style{
								Position: core.Absolute,
								Top:      core.Pt(0),
								Left:     core.Pt(0),
								Right:    core.Pt(0),
								Bottom:   core.Pt(0),
							},
							Draw: func(d *core.Drawing, size core.Size) {
								_ = rev.Get()
								selT, selC, _, _, hasSel := ed.SelectedCutsceneClip()
								selected := hasSel && selT == trackIdx && selC == clipIdx
								status := ed.CutsceneClipReviewStatus(trackIdx, clipIdx)
								drawCutsceneClipBox(d, size, track.Kind, status, selected)
							},
						},
						core.View{
							Style: core.Style{
								Width:          core.Pct(100),
								Height:         core.Pct(100),
								Padding:        core.Symmetric(0, 7),
								JustifyContent: core.JustifyCenter,
							},
							Children: []core.Node{
								core.Text{
									Content: clipLabel,
									Style:   core.Style{FontSize: 11, Color: specText},
								},
							},
						},
					},
				})
			}

			trackRows = append(trackRows, core.View{
				TestID: fmt.Sprintf("cutscene-track-%d", trackIdx),
				Style: core.Style{
					Height:      core.Pt(core.Points(cutsceneTrackH)),
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: specLine,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(core.Points(cutsceneGutterW)),
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Text{
								Content: trackDisplayName(track),
								Style:   core.Style{FontSize: 12, Color: specTextSecondary},
							},
						},
					},
					core.View{
						Style: core.Style{
							Position: core.Relative,
							Width:    core.Pt(core.Points(cutsceneLaneW)),
							Height:   core.Pt(core.Points(cutsceneTrackH)),
						},
						Children: clipNodes,
					},
				},
			})
		}

		// Playhead vertical line spanning thumbs + ruler + tracks
		playhead := core.View{
			TestID: "cutscene-playhead",
			StyleFunc: func() core.Style {
				_ = rev.Get()
				x := cutsceneGutterW
				if l := ed.CutsceneLength(); l > 0 {
					frac := float32(ed.CutsceneTime()) / float32(l)
					if frac < 0 {
						frac = 0
					}
					if frac > 1 {
						frac = 1
					}
					x += frac * cutsceneLaneW
				}
				return core.Style{
					Position:   core.Absolute,
					Left:       core.Pt(core.Points(x)),
					Top:        core.Pt(0),
					Bottom:     core.Pt(0),
					Width:      core.Pt(1.5),
					Background: specAccent,
				}
			},
		}

		tracksStack := append([]core.Node{thumbRow, rulerRow}, trackRows...)
		return core.View{
			Style: core.Style{
				Padding: core.Symmetric(8, 12),
			},
			Children: []core.Node{
				core.View{
					Style:    core.Style{Position: core.Relative},
					Children: append(tracksStack, playhead),
				},
			},
		}
	})

	return core.View{
		TestID: "cutscene-timeline-drawer",
		Style: core.Style{
			Height:      core.Pt(364),
			FlexShrink:  0,
			Background:  specPanel,
			Borders:     core.Edges{Top: 1},
			BorderColor: specLine,
			Overflow:    core.Hidden,
		},
		Children: []core.Node{
			transport,
			ui.ScrollView{
				Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{body},
			},
		},
	}
}

func cutsceneRightRail(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	reviewEpoch := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.cutscene.structRev
	})
	reviewKey := core.NewMemo(cx, func() string {
		_ = reviewEpoch.Get()
		if !ed.CutsceneReviewOpen() {
			return "closed"
		}
		var b strings.Builder
		for i, ch := range ed.CutsceneReviewChanges() {
			fmt.Fprintf(&b, "%d:%s:%v:%v|", i, ch.ID, ch.Reverted, ed.CutsceneChangeFocused(i))
		}
		return b.String()
	})

	return core.View{
		TestID: "cutscene-right-rail",
		Style: core.Style{
			Width:       core.Pt(380),
			FlexShrink:  0,
			Background:  specPanel,
			Borders:     core.Edges{Left: 1},
			BorderColor: specLine,
			Overflow:    core.Hidden,
		},
		Children: []core.Node{
			// Top section: N CHANGES list while a review is open
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.CutsceneReviewOpen()
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "cutscene-review-section",
						Style: core.Style{
							Borders:     core.Edges{Bottom: 1},
							BorderColor: specLine,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Height:         core.Pt(40),
									Padding:        core.Symmetric(0, 16),
									JustifyContent: core.JustifyCenter,
									Borders:        core.Edges{Bottom: 1},
									BorderColor:    specLine,
								},
								Children: []core.Node{
									core.Text{
										TestID: "cutscene-review-header",
										ContentFunc: func() string {
											_ = rev.Get()
											_, total := ed.CutsceneReviewKeptCount()
											return fmt.Sprintf("%d CHANGES · %s", total, cutsceneFileBaseName(ed))
										},
										Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
									},
								},
							},
							core.View{
								Style: core.Style{Padding: core.All(10), Gap: 6},
								Children: []core.Node{
									core.Dynamic(func(cx *core.Scope) core.Node {
										_ = reviewKey.Get()
										changes := ed.CutsceneReviewChanges()
										cards := make([]core.Node, 0, len(changes))
										for i, ch := range changes {
											idx := i
											c := ch
											focused := ed.CutsceneChangeFocused(idx)
											kept := !c.Reverted

											markCol := specChanged
											markBg := specChangedFill
											switch c.Kind {
											case CutsceneChangeAdded:
												markCol = specAdded
												markBg = specAddedFill
											case CutsceneChangeRemoved:
												markCol = specProblem
												markBg = specRemovedFill
											}
											cardBg := specRaised
											cardBorder := specLineRaised
											bw := core.Points(1)
											if focused {
												cardBg = specActiveNode
												cardBorder = specAccent
												bw = 1.5
											}
											var cardOp core.Opacity
											if !kept {
												cardOp = core.Alpha(0.55)
											}
											toggleText := "Revert"
											toggleCol := specText
											if !kept {
												toggleText = "Restore"
												toggleCol = specAccentText
											}
											cards = append(cards, core.View{
												TestID: fmt.Sprintf("cutscene-change-%d", idx),
												Style: core.Style{
													Direction:    core.Row,
													AlignItems:   core.AlignCenter,
													Gap:          8,
													Padding:      core.Edges{Top: 6, Bottom: 6, Left: 10, Right: 8},
													CornerRadius: 12,
													Background:   cardBg,
													BorderWidth:  bw,
													BorderColor:  cardBorder,
													Opacity:      cardOp,
												},
												Children: []core.Node{
													core.View{
														TestID: fmt.Sprintf("cutscene-change-focus-%d", idx),
														Role:   core.RoleButton,
														Label:  c.Title,
														OnPress: func(core.PressEvent) {
															ed.FocusCutsceneReviewChange(idx)
														},
														Style: core.Style{
															FlexGrow:     1,
															FlexBasis:    core.Pt(0),
															Direction:    core.Row,
															AlignItems:   core.AlignCenter,
															Gap:          10,
															FocusVisible: specRowFocusRingStyle,
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
																		Content: c.Sign,
																		Style:   core.Style{FontSize: 12, FontWeight: 600, Color: markCol},
																	},
																},
															},
															core.View{
																Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 1},
																Children: []core.Node{
																	core.Text{
																		Content: c.Title,
																		Style:   core.Style{FontSize: 12, FontWeight: 500, Color: specTextStrong},
																	},
																	core.Text{
																		Content: c.Detail,
																		Style:   core.Style{FontSize: 11, Color: specTextMuted},
																	},
																},
															},
														},
													},
													core.View{
														TestID: fmt.Sprintf("cutscene-change-toggle-%d", idx),
														Role:   core.RoleButton,
														Label:  toggleText + ": " + c.Title,
														OnPress: func(core.PressEvent) {
															ed.report(ed.ToggleCutsceneReviewChange(idx))
														},
														Style: core.Style{
															Height:         core.Pt(28),
															Padding:        core.Symmetric(0, 10),
															CornerRadius:   6,
															AlignItems:     core.AlignCenter,
															JustifyContent: core.JustifyCenter,
															Background:     specLineRaised,
															FocusVisible:   specFocusRingStyle,
														},
														Children: []core.Node{
															core.Text{
																Content: toggleText,
																Style:   core.Style{FontSize: 11.5, FontWeight: 500, Color: toggleCol},
															},
														},
													},
												},
											})
										}
										return core.View{Style: core.Style{Gap: 6}, Children: cards}
									}),
								},
							},
						},
					}
				},
			},
			// Bottom section: Selected Clip Inspector
			ui.ScrollView{
				Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{cutsceneClipInspector(cx, ed, rev)},
			},
		},
	}
}

func cutsceneClipInspector(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	easeOpts := []struct {
		easing cutscene.Easing
		label  string
	}{
		{cutscene.EaseLinear, "Linear"},
		{cutscene.EaseIn, "In"},
		{cutscene.EaseOut, "Out"},
		{cutscene.EaseInOut, "In-out"},
		{cutscene.EaseHold, "Hold"},
	}

	easeButtons := make([]core.Node, len(easeOpts))
	for i, opt := range easeOpts {
		eVal := opt.easing
		eLbl := opt.label
		easeButtons[i] = cutsceneSegButton("cutscene-ease-"+string(eVal), eLbl, func() bool {
			_ = rev.Get()
			_, _, cl, _, ok := ed.SelectedCutsceneClip()
			if !ok {
				return false
			}
			cur := cl.Easing
			if cur == "" {
				cur = cutscene.EaseLinear
			}
			return cur == eVal
		}, func() {
			ti, ci, _, _, ok := ed.SelectedCutsceneClip()
			if ok {
				ed.report(ed.SetCutsceneClipEasing(ti, ci, eVal))
			}
		})
	}

	return core.Show{
		When: func() bool {
			_ = rev.Get()
			_, _, _, _, ok := ed.SelectedCutsceneClip()
			return ok
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "cutscene-clip-inspector",
				Style:  core.Style{Padding: core.All(14), Gap: 10},
				Children: []core.Node{
					// Selected clip title + track name
					ui.HStack{
						Gap: 8,
						Children: []core.Node{
							core.Text{
								TestID: "cutscene-sel-title",
								ContentFunc: func() string {
									_ = rev.Get()
									_, _, cl, tr, ok := ed.SelectedCutsceneClip()
									if !ok {
										return ""
									}
									return cutsceneClipDisplayLabel(tr.Kind, cl)
								},
								Style: core.Style{FontSize: 14, FontWeight: 600, Color: specTextStrong},
							},
							core.Text{
								TestID: "cutscene-sel-track",
								ContentFunc: func() string {
									_ = rev.Get()
									_, _, _, tr, ok := ed.SelectedCutsceneClip()
									if !ok {
										return ""
									}
									return trackDisplayName(tr)
								},
								Style: core.Style{FontSize: 12, Color: specTextMuted},
							},
						},
					},
					// Start ±0.1 s row
					cutsceneStepperRow("Start", "cutscene-start-down", "cutscene-start-value", "cutscene-start-up",
						"Start 0.1 s earlier", "Start 0.1 s later",
						func() string {
							_ = rev.Get()
							_, _, cl, _, ok := ed.SelectedCutsceneClip()
							if !ok {
								return "0.0 s"
							}
							return formatCutsceneSec(time.Duration(cl.Start))
						},
						func() { ed.report(ed.AdjustCutsceneClipStart(-100 * time.Millisecond)) },
						func() { ed.report(ed.AdjustCutsceneClipStart(100 * time.Millisecond)) },
					),
					// Duration ±0.1 s row
					cutsceneStepperRow("Duration", "cutscene-dur-down", "cutscene-dur-value", "cutscene-dur-up",
						"0.1 s shorter", "0.1 s longer",
						func() string {
							_ = rev.Get()
							_, _, cl, _, ok := ed.SelectedCutsceneClip()
							if !ok {
								return "0.0 s"
							}
							return formatCutsceneSec(time.Duration(cl.Duration))
						},
						func() { ed.report(ed.AdjustCutsceneClipDuration(-100 * time.Millisecond)) },
						func() { ed.report(ed.AdjustCutsceneClipDuration(100 * time.Millisecond)) },
					),
					// Easing row (Camera and Actor tracks)
					core.Show{
						When: func() bool {
							_ = rev.Get()
							_, _, _, tr, ok := ed.SelectedCutsceneClip()
							return ok && (tr.Kind == cutscene.TrackCamera || tr.Kind == cutscene.TrackActor)
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID: "cutscene-easing-row",
								Style: core.Style{
									Direction:  core.Row,
									AlignItems: core.AlignCenter,
									Gap:        12,
								},
								Children: []core.Node{
									core.Text{
										Content: "Easing",
										Style:   core.Style{Width: core.Pt(72), FontSize: 12, Color: specTextSecondary},
									},
									core.View{
										Style: core.Style{
											Direction:    core.Row,
											Gap:          2,
											Padding:      core.All(3),
											CornerRadius: 8,
											Background:   specRaised,
											BorderWidth:  1,
											BorderColor:  specLineRaised,
										},
										Children: easeButtons,
									},
								},
							}
						},
					},
					// Dialogue line card (Dialogue tracks only)
					core.Show{
						When: func() bool {
							_ = rev.Get()
							_, _, _, tr, ok := ed.SelectedCutsceneClip()
							return ok && tr.Kind == cutscene.TrackDialogue
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID: "cutscene-line-card",
								Style: core.Style{
									Padding:      core.All(12),
									Gap:          6,
									CornerRadius: 12,
									Background:   specRaised,
									BorderWidth:  1,
									BorderColor:  specLineRaised,
								},
								Children: []core.Node{
									core.Text{
										TestID: "cutscene-line-id",
										ContentFunc: func() string {
											_ = rev.Get()
											_, _, cl, _, _ := ed.SelectedCutsceneClip()
											return cl.Line
										},
										Style: core.Style{FontSize: 11.5, Color: specTextMuted},
									},
									core.Text{
										TestID: "cutscene-line-en",
										ContentFunc: func() string {
											_ = rev.Get()
											_, _, cl, _, _ := ed.SelectedCutsceneClip()
											en, _ := ed.CutsceneLineText(cl.Line)
											if en == "" {
												return "(missing English text)"
											}
											return en
										},
										Style: core.Style{FontSize: 12.5, Color: specTextStrong},
									},
									core.Text{
										TestID: "cutscene-line-zh",
										ContentFunc: func() string {
											_ = rev.Get()
											_, _, cl, _, _ := ed.SelectedCutsceneClip()
											_, zh := ed.CutsceneLineText(cl.Line)
											if zh == "" {
												return "(missing Chinese text)"
											}
											return zh
										},
										Style: core.Style{FontSize: 12.5, Color: specTextSecondary},
									},
									core.Text{
										TestID: "cutscene-line-note",
										ContentFunc: func() string {
											_ = rev.Get()
											enPath, zhPath := ed.CutsceneLocPaths()
											return fmt.Sprintf("Text lives in %s and %s; the cutscene stores only the id.", enPath, zhPath)
										},
										Style: core.Style{FontSize: 11, Color: specTextMuted},
									},
								},
							}
						},
					},
					// Review-joined edit hint
					core.Show{
						When: func() bool {
							_ = rev.Get()
							if !ed.CutsceneReviewOpen() {
								return false
							}
							ti, ci, _, _, ok := ed.SelectedCutsceneClip()
							return (ok && ed.CutsceneClipManuallyEdited(ti, ci)) || ed.CutsceneReviewHasManualEdits()
						},
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID:  "cutscene-review-edited-hint",
								Content: "Your edit joins this review · ⌘Z undoes it",
								Style:   core.Style{FontSize: 11.5, Color: specChanged},
							}
						},
					},
				},
			}
		},
		Else: func(cx *core.Scope) core.Node {
			return core.View{
				Style: core.Style{Padding: core.All(16)},
				Children: []core.Node{
					core.Text{
						Content: "Select a clip on the timeline to inspect its timing and easing.",
						Style:   core.Style{FontSize: 12, Color: specTextMuted},
					},
				},
			}
		},
	}
}

func cutsceneStepperRow(label, downID, valID, upID, downLabel, upLabel string, valFunc func() string, onDown, onUp func()) core.Node {
	btn := func(id, sym, aria string, onPress func()) core.Node {
		return core.View{
			TestID:  id,
			Role:    core.RoleButton,
			Label:   aria,
			OnPress: func(core.PressEvent) { onPress() },
			Style: core.Style{
				Width:          core.Pt(28),
				Height:         core.Pt(28),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   specFocusRingStyle,
			},
			Children: []core.Node{
				core.Text{
					Content: sym,
					Style:   core.Style{FontSize: 13, FontWeight: 600, Color: specText},
				},
			},
		}
	}
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        12,
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style:   core.Style{Width: core.Pt(72), FontSize: 12, Color: specTextSecondary},
			},
			core.View{
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					CornerRadius: 8,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: []core.Node{
					btn(downID, "−", downLabel, onDown),
					core.View{
						Style: core.Style{
							Width:          core.Pt(56),
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Text{
								TestID:      valID,
								ContentFunc: valFunc,
								Style:       core.Style{FontSize: 11.5, FontWeight: 500, Color: specTextStrong},
							},
						},
					},
					btn(upID, "+", upLabel, onUp),
				},
			},
		},
	}
}

func cutsceneSegButton(testID, label string, active func() bool, onPress func()) core.Node {
	return core.View{
		TestID:  testID,
		Role:    core.RoleButton,
		Label:   label,
		OnPress: func(core.PressEvent) { onPress() },
		StyleFunc: func() core.Style {
			var bg core.Color
			if active() {
				bg = specLineRaised
			}
			return core.Style{
				Height:         core.Pt(24),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     bg,
				FocusVisible:   specFocusRingStyle,
			}
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				StyleFunc: func() core.Style {
					col := specTextMuted
					if active() {
						col = specTextStrong
					}
					return core.Style{FontSize: 11.5, FontWeight: 500, Color: col}
				},
			},
		},
	}
}

// cutsceneActionButton uses Spec.dc.html AAA/AA contrast pairs: #101216 (specBg) on #3B7BFF (specAccent) for primary Keep buttons, and specText on specLineRaised for secondary buttons.
func cutsceneActionButton(testID string, textFunc func() string, primary bool, disabled func() bool, onPress func()) core.Node {
	bg := specLineRaised
	fg := specText
	weight := core.FontWeight(500)
	if primary {
		bg = specAccent
		fg = specBg // Spec.dc.html contrast table: #101216 on accent (4.88:1 AA), never #FFFFFF on accent.
		weight = 600
	}
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		OnPress: func(core.PressEvent) {
			if disabled != nil && disabled() {
				return
			}
			onPress()
		},
		StyleFunc: func() core.Style {
			var op core.Opacity
			if disabled != nil && disabled() {
				op = core.Alpha(0.45)
			}
			return core.Style{
				Height:         core.Pt(30),
				Padding:        core.Symmetric(0, 12),
				CornerRadius:   8,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     bg,
				Opacity:        op,
				FocusVisible:   specFocusRingStyle,
			}
		},
		Children: []core.Node{
			core.Text{
				TestID:      testID + "-text",
				ContentFunc: textFunc,
				Style:       core.Style{FontSize: 12, FontWeight: weight, Color: fg},
			},
		},
	}
}

func drawDotGrid(d *core.Drawing, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	d.SetFillColor(specViewportBG).FillRect(0, 0, w, h)
	d.SetFillColor(specViewportDot)
	for gy := float32(8); gy < h; gy += 16 {
		for gx := float32(8); gx < w; gx += 16 {
			d.FillRect(gx, gy, 1.5, 1.5)
		}
	}
}

func drawCutscenePreview(d *core.Drawing, size core.Size, ed *Editor) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	d.SetFillColor(specPanel).FillRect(0, 0, w, h)
	scene := ed.CutscenePreviewScene()
	if scene == nil {
		// Subtle floor silhouette when no scene preview is built (e.g. video cutscene).
		d.SetFillColor(specFloorFill).FillRect(0, h*0.82, w, h*0.18)
		return
	}
	vw, vh := float32(640), float32(360)
	if scene.Doc != nil && scene.Doc.VirtualWidth > 0 && scene.Doc.VirtualHeight > 0 {
		vw, vh = scene.Doc.VirtualWidth, scene.Doc.VirtualHeight
	}
	scale := min(w/vw, h/vh)
	offX := (w - vw*scale) / 2
	offY := (h - vh*scale) / 2

	d.Save()
	d.Translate(offX, offY).Scale(scale, scale)
	d.BeginPath().Rect(0, 0, vw, vh).Clip()
	if !scene.HasBackground {
		d.SetFillColor(specViewportBG).FillRect(0, 0, vw, vh)
	}
	scene.Draw(d)

	// Draw subtle Spec-colored placeholders for non-sprite marker objects so scenes with marker actors/doors/levers stay legible in the preview.
	camX, camY, zoom := float32(0), float32(0), float32(1)
	if scene.Camera != nil {
		camX = scene.Camera.X
		camY = scene.Camera.Y
		zoom = scene.Camera.EffectiveZoom()
	}
	d.Save()
	if scene.Camera != nil {
		d.Translate(vw/2, vh/2).Scale(zoom, zoom).Translate(-camX, -camY)
	}
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		for _, bo := range layer.Objects {
			drawCutsceneMarkerObject(d, bo)
		}
	}
	d.Restore()
	d.Restore()
}

func drawCutsceneMarkerObject(d *core.Drawing, bo *sceneformat.BuiltObject) {
	if bo == nil || bo.Doc == nil {
		return
	}
	if bo.Sprite == nil {
		col := specLineRaised
		idLower := strings.ToLower(bo.Doc.ID)
		switch {
		case strings.Contains(idLower, "slime"):
			col = specAdded
		case strings.Contains(idLower, "knight"), strings.Contains(idLower, "hero"), strings.Contains(idLower, "player"):
			col = specAccent
		case strings.Contains(idLower, "lever"), strings.Contains(idLower, "torch"):
			col = specChanged
		}
		d.SetFillColor(col).BeginPath().RoundRect(bo.Doc.X-14, bo.Doc.Y-18, 28, 36, 4).Fill()
	}
	for _, child := range bo.Children {
		drawCutsceneMarkerObject(d, child)
	}
}

func drawCutsceneThumb(d *core.Drawing, size core.Size, sample *CutsceneThumbSample) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	d.SetFillColor(specViewportBG).FillRect(0, 0, w, h)
	floorY := h * 0.80
	d.SetFillColor(specFloorFill).FillRect(0, floorY, w, h-floorY)
	if sample == nil {
		return
	}
	scaleX := w / 640
	scaleY := h / 360
	for _, a := range sample.Actors {
		ax := (a.X - sample.CamX + 320) * scaleX
		ay := (a.Y - sample.CamY + 180) * scaleY
		if ax < -4 || ax > w+4 {
			continue
		}
		if ay < 2 {
			ay = 2
		}
		if ay > floorY-4 {
			ay = floorY - 4
		}
		col := specText
		if strings.Contains(strings.ToLower(a.ID), "slime") {
			col = specAdded
		}
		d.SetFillColor(col).BeginPath().RoundRect(ax-2, ay-4, 4, 6, 1).Fill()
	}
}

func drawCutsceneClipBox(d *core.Drawing, size core.Size, kind cutscene.TrackKind, status CutsceneChangeKind, selected bool) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	fill := specRaised
	switch kind {
	case cutscene.TrackDialogue:
		fill = specDialogueFill
	case cutscene.TrackSound, cutscene.TrackMusic:
		fill = specAudioFill
	}
	if status == CutsceneChangeAdded {
		fill = specAddedFill
	}
	d.Save()
	d.SetFillColor(fill).BeginPath().RoundRect(0, 0, w, h, 6).Fill()
	switch {
	case selected:
		d.SetStrokeColor(specAccent).SetLineWidth(1.5).BeginPath().RoundRect(0.75, 0.75, w-1.5, h-1.5, 6).Stroke()
	case status == CutsceneChangeAdded:
		d.SetStrokeColor(specAdded).SetLineWidth(1.5).SetLineDash(4, 3).BeginPath().RoundRect(0.75, 0.75, w-1.5, h-1.5, 6).Stroke()
	case status == CutsceneChangeChanged:
		d.SetStrokeColor(specChanged).SetLineWidth(1.5).SetLineDash(4, 3).BeginPath().RoundRect(0.75, 0.75, w-1.5, h-1.5, 6).Stroke()
	default:
		d.SetStrokeColor(specLineRaised).SetLineWidth(1).BeginPath().RoundRect(0.5, 0.5, w-1, h-1, 6).Stroke()
	}
	d.Restore()
}

func cutsceneFileBaseName(ed *Editor) string {
	if p := ed.CutscenePath(); p != "" {
		return path.Base(strings.ReplaceAll(p, `\`, "/"))
	}
	return "untitled.cutscene.yaml"
}

func effectiveFitLabel(fit cutscene.Fit) string {
	if fit == "" {
		return string(cutscene.FitContain)
	}
	return string(fit)
}
