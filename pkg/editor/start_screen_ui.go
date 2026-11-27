package editor

import (
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// templateSwatchColor returns a restrained card preview tint composed strictly from Spec tokens
// for the active appearance (Dark or Light).
func templateSwatchColor(p SpecPalette, name string) core.Color {
	base := p.Raised
	swatch := func(role core.Color, alpha float32) core.Color {
		return CompositeOver(specAlpha(role, alpha), base)
	}
	swatches := map[string]core.Color{
		"starter":        p.LineRaised,
		"platformer2d":   swatch(p.Accent.Fg, 0.24),
		"rpg2d":          swatch(p.Added.Fg, 0.22),
		"shmup2d":        swatch(p.Removed.Fg, 0.22),
		"fighting2d":     swatch(p.Changed.Fg, 0.24),
		"racing3d":       swatch(p.Accent.Fg, 0.20),
		"fps3d":          swatch(p.Agent.Fg, 0.22),
		"action3d":       swatch(p.Added.Fg, 0.18),
		"puzzle2d":       swatch(p.Warning.Fg, 0.20),
		"cards2d":        swatch(p.Removed.Fg, 0.18),
		"rts2d":          swatch(p.Added.Fg, 0.20),
		"roguelike2d":    swatch(p.Agent.Fg, 0.18),
		"towerdefense2d": swatch(p.Warning.Fg, 0.18),
		"idle":           swatch(p.Added.Fg, 0.16),
		"visualnovel":    swatch(p.Agent.Fg, 0.24),
		"rhythm":         swatch(p.Accent.Fg, 0.18),
	}
	if col, ok := swatches[strings.ToLower(name)]; ok {
		return col
	}
	palette := []core.Color{
		p.LineRaised,
		swatch(p.Accent.Fg, 0.24),
		swatch(p.Added.Fg, 0.22),
		swatch(p.Agent.Fg, 0.22),
		swatch(p.Changed.Fg, 0.20),
		swatch(p.Warning.Fg, 0.20),
	}
	sum := 0
	for i := range len(name) {
		sum += int(name[i])
	}
	return palette[sum%len(palette)]
}

// StartScreenView renders the editor's Start Screen matching Start.dc.html, step 1 of FirstRun.dc.html,
// and the Spec.dc.html design tokens (Dark & Light): left sidebar with navigation and conditional RECENT list,
// right pane with "What do you want to make?", dynamically populated template cards with filter chips,
// live-validated New Project bar with a single primary Create button, and streaming `game new` output.
func StartScreenView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	ed.ensureStartScreenInit()
	type startScreenRenderKey struct {
		dark bool
		lang string
	}
	renderKey := core.NewMemo(cx, func() startScreenRenderKey {
		_ = rev.Get()
		return startScreenRenderKey{
			dark: ed.SpecPalette(cx).Dark,
			lang: ed.ResolvedLanguage(),
		}
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := SpecPaletteFor(renderKey.Get().dark)
		return core.View{
			TestID: "editor-start-screen",
			Style: core.Style{
				Direction:  core.Row,
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Background: p.Bg,
			},
			Children: []core.Node{
				startSidebarView(cx, ed, rev),
				startMainPaneView(cx, ed, rev),
			},
		}
	})
}

func startSidebarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	navBtn := func(testID, label string, active bool, icon func() *core.SVG, onPress func()) core.Node {
		var bg core.Color
		fg := p.TextSecondary
		if active {
			bg = p.Accent.Fill
			fg = p.TextPrimary
		}
		return ui.Button{
			TestID:  testID,
			Text:    label,
			Icon:    icon,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color:   func() core.Color { return bg },
			Tint:    func() core.Color { return fg },
			TextStyle: core.Style{
				FontSize:   13,
				FontWeight: 500,
				Color:      fg,
			},
			Style: core.Style{
				Width:          core.Pct(100),
				Height:         core.Pt(32),
				JustifyContent: core.JustifyStart,
				Padding:        core.Symmetric(0, 12),
				CornerRadius:   6,
			},
			OnPress: onPress,
		}
	}

	// Memoize the recent-project list revision so keystrokes and streamed creation output do not re-stat every project folder on every rev bump.
	recentRevMemo := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.recentProjectsRev()
	})
	hasRecentMemo := core.NewMemo(cx, func() bool {
		_ = recentRevMemo.Get()
		return ed.hasRecentProjects()
	})

	return core.View{
		TestID: "start-sidebar",
		Style: core.Style{
			Width:       core.Pt(280),
			Height:      core.Pct(100),
			Background:  p.Panel,
			Borders:     core.Edges{Right: 1},
			BorderColor: p.Line,
			Padding:     core.All(24),
			Gap:         16,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            8,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "start-title",
						Content: ed.T("startScreen.title"),
						Style: core.Style{
							FontSize:   18,
							FontWeight: 600,
							Color:      p.TextPrimary,
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.CanReturnToEditor()
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:  "start-back-to-editor",
								Text:    ed.T("startScreen.back"),
								Label:   ed.T("startScreen.backLabel"),
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return p.LineRaised },
								TextStyle: core.Style{
									FontSize: 12,
									Color:    p.TextPrimary,
								},
								Style: core.Style{
									Height:       core.Pt(28),
									CornerRadius: 6,
									Padding:      core.Symmetric(0, 10),
								},
								OnPress: func() {
									ed.SetStartScreenOpen(false)
								},
							}
						},
					},
				},
			},
			core.View{
				Style: core.Style{Gap: 4},
				Children: []core.Node{
					navBtn("start-nav-new", ed.T("startScreen.newProject"), true, lucide.Plus, func() {
						ed.SetStartScreenOpen(true)
					}),
					navBtn("start-open-folder", ed.T("startScreen.openFolder"), false, lucide.FolderOpen, func() {
						ed.report(ed.BrowseAndOpenProject())
					}),
					navBtn("start-open-demo", ed.T("startScreen.openDemo"), false, lucide.Play, func() {
						ed.report(ed.RequestOpenDemo())
					}),
				},
			},
			// RECENT section appears only once there are recent projects (matching FirstRun.dc.html / Start.dc.html).
			core.Show{
				When: hasRecentMemo.Get,
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "start-recent-section",
						Style: core.Style{
							FlexGrow:  1,
							FlexBasis: core.Pt(0),
							Gap:       12,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Height:     core.Pt(1),
									Background: p.Line,
								},
							},
							core.Text{
								TestID:  "start-recent-heading",
								Content: ed.T("startScreen.recentHeading"),
								Style: core.Style{
									FontSize:      11,
									FontWeight:    600,
									LetterSpacing: 0.7,
									Color:         p.TextMuted,
								},
							},
							ui.ScrollView{
								TestID:       "start-recent-scroll",
								Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
								ContentStyle: core.Style{Gap: 4},
								EdgeColor:    func() core.Color { return p.Panel },
								Children: []core.Node{
									core.Dynamic(func(cx *core.Scope) core.Node {
										_ = recentRevMemo.Get()
										entries := ed.RecentProjects()
										now := ed.now()
										rows := make([]core.Node, 0, len(entries))
										for i, entry := range entries {
											rows = append(rows, startRecentRow(p, ed, i, entry, now))
										}
										return core.View{
											TestID:   "start-recent-list",
											Style:    core.Style{Gap: 4},
											Children: rows,
										}
									}),
								},
							},
						},
					}
				},
			},
		},
	}
}

func startRecentRow(p SpecPalette, ed *Editor, index int, entry RecentProjectEntry, now time.Time) core.Node {
	relTime := ed.localizedRelativeTime(entry.LastOpened, now)
	shortPath := shortenHomePath(entry.Path)
	meta := shortPath + " · " + relTime
	nameColor := p.TextPrimary
	if entry.Missing {
		nameColor = p.TextSecondary
	}
	entryPath := entry.Path
	entryMissing := entry.Missing

	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        4,
		},
		Children: []core.Node{
			core.View{
				TestID: fmt.Sprintf("start-recent-%d", index),
				Role:   core.RoleButton,
				Label:  ed.T("startScreen.openRecentLabel", l10n.Param("name", entry.Name)),
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					Padding:      core.Symmetric(8, 12),
					CornerRadius: 6,
					Gap:          2,
				},
				OnPress: func(core.PressEvent) {
					if entryMissing {
						ed.reportStatusError(ed.T("startScreen.missingProjectError", l10n.Param("path", entryPath)))
						return
					}
					ed.report(ed.RequestOpenProject(entryPath))
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        6,
						},
						Children: []core.Node{
							core.Text{
								Content: entry.Name,
								Style: core.Style{
									FontSize:   13,
									FontWeight: 500,
									Color:      nameColor,
								},
							},
							core.Show{
								When: func() bool { return entryMissing },
								Then: func(cx *core.Scope) core.Node {
									return core.View{
										TestID: fmt.Sprintf("start-recent-missing-%d", index),
										Style: core.Style{
											Background:   p.Warning.Fill,
											BorderWidth:  1,
											BorderColor:  p.Warning.Edge,
											CornerRadius: 4,
											Padding:      core.Symmetric(1, 6),
										},
										Children: []core.Node{
											core.Text{
												Content: ed.T("startScreen.missingBadge"),
												Style: core.Style{
													FontSize:   10,
													FontWeight: 600,
													Color:      p.Warning.Fg,
												},
											},
										},
									}
								},
							},
						},
					},
					core.Text{
						Content: meta,
						Style: core.Style{
							FontSize: 11,
							Color:    p.TextMuted,
						},
					},
				},
			},
			core.Show{
				When: func() bool { return entryMissing },
				Then: func(cx *core.Scope) core.Node {
					return ui.Button{
						TestID:  fmt.Sprintf("start-recent-remove-%d", index),
						Icon:    lucide.X,
						Label:   ed.T("startScreen.removeMissingLabel", l10n.Param("name", entry.Name)),
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Side:    28,
						Tint:    func() core.Color { return p.TextMuted },
						OnPress: func() {
							ed.report(ed.RemoveRecentProject(entryPath))
						},
					}
				},
			},
		},
	}
}

func startMainPaneView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	return ui.ScrollView{
		TestID: "start-main-scroll",
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Height:     core.Pct(100),
			Background: p.Bg,
		},
		ContentStyle: core.Style{
			Padding: core.Symmetric(24, 32),
			Gap:     20,
		},
		EdgeColor: func() core.Color { return p.Bg },
		Children: []core.Node{
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.FirstRunStage() == FirstRunStageConnect
				},
				Then: func(cx *core.Scope) core.Node {
					return firstRunConnectInPlaceView(cx, ed, rev)
				},
				Else: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{Gap: 20},
						Children: []core.Node{
							startGoToolchainNoticeView(cx, ed, rev),
							startIdeaSectionView(cx, ed, rev),
							startTemplatesSectionView(cx, ed, rev),
							startCreateFormView(cx, ed, rev),
							startCreateOutputView(cx, ed, rev),
						},
					}
				},
			},
		},
	}
}

func startGoToolchainNoticeView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return !ed.GoToolchainOK()
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				st := ed.GoToolchainStatus()
				minVer := st.MinVersion
				if minVer == "" {
					minVer = ed.MinGoVersion()
				}
				title := ed.T("start.goToolchain.title", l10n.Param("version", minVer))
				var detail string
				switch {
				case st.TooOld && st.Version != "":
					detail = ed.T("start.goToolchain.tooOldDetail",
						l10n.Param("found", st.Version),
						l10n.Param("path", st.Binary),
					)
				case st.Err != "":
					detail = ed.T("start.goToolchain.errorDetail",
						l10n.Param("path", st.Binary),
						l10n.Param("err", st.Err),
					)
				default:
					detail = ed.T("start.goToolchain.missingDetail")
				}
				return core.View{
					TestID: "start-go-notice",
					Style: core.Style{
						Direction:      core.Row,
						Wrap:           core.Wrap,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            12,
						Background:     p.Warning.Fill,
						BorderWidth:    1,
						BorderColor:    p.Warning.Edge,
						CornerRadius:   10,
						Padding:        core.Symmetric(12, 16),
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        10,
								FlexGrow:   1,
								FlexBasis:  core.Pt(260),
							},
							Children: []core.Node{
								core.Icon{
									SVG: lucide.TriangleAlert(),
									Style: core.Style{
										Width:      core.Pt(18),
										Height:     core.Pt(18),
										Color:      p.Warning.Fg,
										FlexShrink: 0,
									},
								},
								core.View{
									Style: core.Style{
										FlexGrow:  1,
										FlexBasis: core.Pt(0),
										Gap:       2,
									},
									Children: []core.Node{
										core.Text{
											TestID:  "start-go-notice-title",
											Content: title,
											Style: core.Style{
												FontSize:   13,
												FontWeight: 600,
												Color:      p.TextPrimary,
											},
										},
										core.Text{
											TestID:  "start-go-notice-detail",
											Content: detail,
											Style: core.Style{
												FontSize: 12,
												Color:    p.TextSecondary,
											},
										},
									},
								},
							},
						},
						core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        8,
								FlexShrink: 0,
							},
							Children: []core.Node{
								ui.Button{
									TestID:  "start-go-download",
									Text:    ed.T("start.goToolchain.download"),
									Label:   ed.T("start.goToolchain.downloadLabel"),
									Icon:    lucide.ExternalLink,
									Variant: ui.ButtonPrimary,
									Size:    ui.SizeSmall,
									Color:   func() core.Color { return p.TextPrimary },
									Tint:    func() core.Color { return p.Bg },
									TextStyle: core.Style{
										FontSize:   12,
										FontWeight: 600,
										Color:      p.Bg,
									},
									Style: core.Style{
										Height:       core.Pt(28),
										CornerRadius: 6,
										Padding:      core.Symmetric(0, 10),
									},
									OnPress: func() {
										ed.report(ed.OpenGoDownloadPage())
									},
								},
								ui.Button{
									TestID:  "start-go-retry",
									Text:    ed.T("start.goToolchain.retry"),
									Label:   ed.T("start.goToolchain.retryLabel"),
									Icon:    lucide.RefreshCw,
									Variant: ui.ButtonSecondary,
									Size:    ui.SizeSmall,
									Color:   func() core.Color { return p.Raised },
									Tint:    func() core.Color { return p.TextPrimary },
									TextStyle: core.Style{
										FontSize:   12,
										FontWeight: 500,
										Color:      p.TextPrimary,
									},
									Style: core.Style{
										Height:       core.Pt(28),
										CornerRadius: 6,
										Padding:      core.Symmetric(0, 10),
										BorderWidth:  1,
										BorderColor:  p.LineRaised,
									},
									OnPress: func() {
										ed.RetryGoToolchainCheck()
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

func startIdeaSectionView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	return core.View{
		TestID: "start-idea-section",
		Style:  core.Style{Gap: 12},
		Children: []core.Node{
			core.Text{
				Content: ed.T("startScreen.ideaHeading"),
				Style: core.Style{
					FontSize:   20,
					FontWeight: 600,
					Color:      p.TextPrimary,
				},
			},
			core.View{
				Style: core.Style{
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					CornerRadius: 12,
					Padding:      core.All(16),
					Gap:          12,
				},
				Children: []core.Node{
					specInputField(cx, ed, ui.Input{
						TestID:      "start-idea-input",
						Label:       ed.T("startScreen.ideaLabel"),
						Multiline:   true,
						Placeholder: ed.T("startScreen.ideaPlaceholder"),
						Value: func() string {
							_ = rev.Get()
							return ed.StartProjectIdea()
						},
						OnChange: func(v string) {
							ed.SetStartProjectIdea(v)
						},
						Style: core.Style{
							Width:        core.Pct(100),
							Background:   p.Panel,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
							CornerRadius: 6,
							Color:        p.TextPrimary,
							FontSize:     13,
						},
					}),
					core.View{
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            12,
						},
						Children: []core.Node{
							core.Dynamic(func(cx *core.Scope) core.Node {
								_ = rev.Get()
								dotColor := p.TextFaint
								status := ed.T("startScreen.noAgentConnected")
								if ed.FirstRunAgentConnected() {
									_, name, _ := ed.FirstRunConnectedAgent()
									dotColor = p.Added.Fg
									status = ed.T("startScreen.agentConnected", l10n.Param("name", name))
								}
								return core.View{
									TestID: "start-agent-status",
									Style: core.Style{
										Direction:  core.Row,
										AlignItems: core.AlignCenter,
										Gap:        6,
										FlexGrow:   1,
										FlexBasis:  core.Pt(0),
									},
									Children: []core.Node{
										core.View{
											Style: core.Style{
												Width:        core.Pt(7),
												Height:       core.Pt(7),
												CornerRadius: 4,
												Background:   dotColor,
												FlexShrink:   0,
											},
										},
										core.Text{
											Content: status,
											Style: core.Style{
												FontSize: 12,
												Color:    p.TextMuted,
											},
										},
									},
								}
							}),
							// Quiet/accent styling paired with Start screen Create button (Spec tokens, Dark & Light).
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "start-ai-btn",
								Text:    ed.T("startScreen.startWithAI"),
								Icon:    lucide.Sparkles,
								Variant: specBtnAccent,
								Disabled: func() bool {
									_ = rev.Get()
									return ed.CreatingProject() || !ed.StartNameStatus().OK || !ed.GoToolchainOK()
								},
								OnPress: func() {
									ed.report(ed.StartWithAI())
								},
							}),
						},
					},
					startWhatHappensNextView(p, ed),
				},
			},
		},
	}
}

func startTemplatesSectionView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	filterChip := func(id, label string) core.Node {
		return ui.Button{
			TestID:  "start-filter-" + id,
			Text:    label,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color: func() core.Color {
				_ = rev.Get()
				if ed.StartTemplateFilter() == id {
					return p.Accent.Fill
				}
				return p.Raised
			},
			TextStyle: core.Style{
				FontSize:   12,
				FontWeight: 500,
				Color:      p.TextPrimary,
			},
			Style: core.Style{
				Height:       core.Pt(28),
				CornerRadius: 6,
				Padding:      core.Symmetric(0, 12),
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			OnPress: func() {
				ed.SetStartTemplateFilter(id)
			},
		}
	}

	return core.View{
		TestID: "start-templates-section",
		Style:  core.Style{Gap: 12},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignEnd,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            12,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Gap: 4},
						Children: []core.Node{
							core.Text{
								Content: ed.T("startScreen.templatesHeading"),
								Style: core.Style{
									FontSize:   14,
									FontWeight: 600,
									Color:      p.TextPrimary,
								},
							},
							core.Text{
								Content: ed.T("startScreen.templatesSubtitle"),
								Style: core.Style{
									FontSize: 12.5,
									Color:    p.TextMuted,
								},
							},
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 6},
						Children: []core.Node{
							filterChip("all", ed.T("startScreen.filterAll")),
							filterChip("2d", "2D"),
							filterChip("3d", "3D"),
						},
					},
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				tmpls := ed.FilteredProjectTemplates()
				selected := ed.SelectedProjectTemplate()
				if len(tmpls) == 0 {
					return core.View{
						TestID: "start-templates-empty",
						Style: core.Style{
							Background:   p.Raised,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
							CornerRadius: 12,
							Padding:      core.All(16),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          12,
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("startScreen.templatesEmpty"),
								Style: core.Style{
									FontSize: 12.5,
									Color:    p.TextSecondary,
								},
							},
							ui.Button{
								TestID:  "start-templates-show-all",
								Text:    ed.T("startScreen.showAll"),
								Variant: ui.ButtonSecondary,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return p.LineRaised },
								TextStyle: core.Style{
									FontSize: 12,
									Color:    p.TextPrimary,
								},
								Style: core.Style{
									Height:       core.Pt(28),
									CornerRadius: 6,
									Padding:      core.Symmetric(0, 10),
								},
								OnPress: func() {
									ed.SetStartTemplateFilter("all")
								},
							},
						},
					}
				}

				cards := make([]core.Node, 0, len(tmpls))
				for _, t := range tmpls {
					tmpl := t
					isSelected := tmpl.Name == selected
					borderColor := p.LineRaised
					borderWidth := core.Points(1)
					if isSelected {
						borderColor = p.Accent.Edge
						borderWidth = 2
					}
					cards = append(cards, core.View{
						TestID: "start-template-" + tmpl.Name,
						Role:   core.RoleButton,
						Label:  ed.T("startScreen.selectTemplateLabel", l10n.Param("title", tmpl.DisplayTitle())),
						Style: core.Style{
							Width:        core.Pt(220),
							Background:   p.Raised,
							BorderWidth:  borderWidth,
							BorderColor:  borderColor,
							CornerRadius: 12,
							Overflow:     core.Hidden,
						},
						OnPress: func(core.PressEvent) {
							ed.SelectProjectTemplate(tmpl.Name)
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Height:         core.Pt(56),
									Background:     templateSwatchColor(p, tmpl.Name),
									Padding:        core.All(8),
									JustifyContent: core.JustifyEnd,
									AlignItems:     core.AlignStart,
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{
											Background:   p.Panel,
											BorderWidth:  1,
											BorderColor:  p.LineRaised,
											CornerRadius: 4,
											Padding:      core.Symmetric(2, 6),
										},
										Children: []core.Node{
											core.Text{
												Content: strings.ToUpper(tmpl.EffectiveDimension()),
												Style: core.Style{
													FontSize:   10,
													FontWeight: 600,
													Color:      p.TextSecondary,
												},
											},
										},
									},
								},
							},
							core.View{
								Style: core.Style{
									Padding: core.All(12),
									Gap:     4,
								},
								Children: []core.Node{
									core.Text{
										Content: tmpl.DisplayTitle(),
										Style: core.Style{
											FontSize:   13,
											FontWeight: 600,
											Color:      p.TextPrimary,
										},
									},
									core.Text{
										Content: tmpl.Summary,
										Style: core.Style{
											FontSize: 12,
											Color:    p.TextMuted,
										},
									},
								},
							},
						},
					})
				}

				return core.View{
					TestID: "start-templates-grid",
					Style: core.Style{
						Direction: core.Row,
						Wrap:      core.Wrap,
						Gap:       12,
					},
					Children: cards,
				}
			}),
		},
	}
}

func startCreateFormView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	return core.View{
		TestID: "start-create-form",
		Style: core.Style{
			Borders:     core.Edges{Top: 1},
			BorderColor: p.Line,
			Padding:     core.Edges{Top: 16},
			Gap:         12,
		},
		Children: []core.Node{
			// Row 1: Name, Folder (+ Browse), Module path
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					Wrap:       core.Wrap,
					AlignItems: core.AlignEnd,
					Gap:        12,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Width: core.Pt(180), Gap: 4},
						Children: []core.Node{
							core.Text{
								Content: ed.T("startScreen.nameHeading"),
								Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
							},
							specInputField(cx, ed, ui.Input{
								TestID:      "start-new-name",
								Label:       ed.T("startScreen.projectNameLabel"),
								Placeholder: ed.T("startScreen.projectNamePlaceholder"),
								Value: func() string {
									_ = rev.Get()
									return ed.StartProjectName()
								},
								OnChange: func(v string) {
									ed.SetStartProjectName(v)
								},
								Style: core.Style{
									Width:        core.Pct(100),
									Height:       core.Pt(32),
									Background:   p.Panel,
									BorderWidth:  1,
									BorderColor:  p.LineRaised,
									CornerRadius: 6,
									Color:        p.TextPrimary,
								},
							}),
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(200), Gap: 4},
						Children: []core.Node{
							core.Text{
								Content: ed.T("startScreen.folderHeading"),
								Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
							},
							core.View{
								Style: core.Style{Direction: core.Row, Gap: 6, AlignItems: core.AlignCenter},
								Children: []core.Node{
									specInputField(cx, ed, ui.Input{
										TestID:      "start-new-folder",
										Label:       ed.T("startScreen.parentFolderLabel"),
										Placeholder: ed.T("startScreen.parentFolderPlaceholder"),
										Value: func() string {
											_ = rev.Get()
											return ed.StartProjectParentDir()
										},
										OnChange: func(v string) {
											ed.SetStartProjectParentDir(v)
										},
										Style: core.Style{
											FlexGrow:     1,
											FlexBasis:    core.Pt(0),
											Height:       core.Pt(32),
											Background:   p.Panel,
											BorderWidth:  1,
											BorderColor:  p.LineRaised,
											CornerRadius: 6,
											Color:        p.TextPrimary,
										},
									}),
									ui.Button{
										TestID:  "start-new-folder-browse",
										Text:    ed.T("startScreen.browse"),
										Variant: ui.ButtonSecondary,
										Size:    ui.SizeSmall,
										Color:   func() core.Color { return p.LineRaised },
										TextStyle: core.Style{
											FontSize: 12,
											Color:    p.TextPrimary,
										},
										Style: core.Style{
											Height:       core.Pt(32),
											CornerRadius: 6,
											Padding:      core.Symmetric(0, 10),
										},
										OnPress: func() {
											ed.report(ed.BrowseStartProjectParentDir())
										},
									},
								},
							},
						},
					},
					core.View{
						Style: core.Style{Width: core.Pt(220), Gap: 4},
						Children: []core.Node{
							core.Text{
								Content: ed.T("startScreen.moduleHeading"),
								Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
							},
							specInputField(cx, ed, ui.Input{
								TestID:      "start-new-module",
								Label:       ed.T("startScreen.moduleLabel"),
								Placeholder: ed.T("startScreen.modulePlaceholder"),
								Value: func() string {
									_ = rev.Get()
									return ed.StartProjectModule()
								},
								OnChange: func(v string) {
									ed.SetStartProjectModule(v)
								},
								Style: core.Style{
									Width:        core.Pct(100),
									Height:       core.Pt(32),
									Background:   p.Panel,
									BorderWidth:  1,
									BorderColor:  p.LineRaised,
									CornerRadius: 6,
									Color:        p.TextPrimary,
								},
							}),
						},
					},
				},
			},
			// Row 2: Live validation feedback + single primary Create button
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					Wrap:           core.Wrap,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            12,
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = rev.Get()
						st := ed.StartNameStatus()
						msg := ed.localizedStartNameMessage(st)
						if st.OK {
							return core.View{
								TestID: "start-name-status",
								Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
								Children: []core.Node{
									core.Text{
										Content: msg,
										Style: core.Style{
											FontSize: 12,
											Color:    p.Added.Fg,
										},
									},
								},
							}
						}
						children := []core.Node{
							core.Text{
								Content: msg,
								Style: core.Style{
									FontSize: 12,
									Color:    p.Removed.Fg,
								},
							},
						}
						if st.Exists {
							targetDir := st.TargetDir
							suggested := st.SuggestedName
							children = append(children,
								ui.Button{
									TestID:  "start-name-open-existing",
									Text:    ed.T("startScreen.openExisting"),
									Variant: ui.ButtonSecondary,
									Size:    ui.SizeSmall,
									Color:   func() core.Color { return p.LineRaised },
									TextStyle: core.Style{
										FontSize: 12,
										Color:    p.TextPrimary,
									},
									Style: core.Style{
										Height:       core.Pt(28),
										CornerRadius: 6,
										Padding:      core.Symmetric(0, 10),
									},
									OnPress: func() {
										ed.report(ed.RequestOpenProject(targetDir))
									},
								},
								ui.Button{
									TestID:  "start-name-use-suggestion",
									Text:    ed.T("startScreen.useSuggested", l10n.Param("name", suggested)),
									Variant: ui.ButtonSecondary,
									Size:    ui.SizeSmall,
									Color:   func() core.Color { return p.LineRaised },
									TextStyle: core.Style{
										FontSize: 12,
										Color:    p.TextPrimary,
									},
									Style: core.Style{
										Height:       core.Pt(28),
										CornerRadius: 6,
										Padding:      core.Symmetric(0, 10),
									},
									OnPress: func() {
										ed.SetStartProjectName(suggested)
									},
								},
							)
						}
						return core.View{
							TestID: "start-name-status",
							Style: core.Style{
								Direction:  core.Row,
								Wrap:       core.Wrap,
								AlignItems: core.AlignCenter,
								Gap:        8,
							},
							Children: children,
						}
					}),
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        12,
						},
						Children: []core.Node{
							core.Text{
								TestID: "start-picked-template",
								ContentFunc: func() string {
									_ = rev.Get()
									sel := ed.SelectedProjectTemplate()
									for _, t := range ed.ProjectTemplates() {
										if t.Name == sel {
											return ed.T("startScreen.pickedTemplate", l10n.Param("title", t.DisplayTitle()))
										}
									}
									if sel == "" {
										return ed.T("startScreen.pickedTemplateDefault")
									}
									return ed.T("startScreen.pickedTemplate", l10n.Param("title", sel))
								},
								Style: core.Style{
									FontSize: 12.5,
									Color:    p.TextSecondary,
								},
							},
							// Single primary button on the screen: inverted Spec TextPrimary/Bg (AAA in both Dark and Light).
							ui.Button{
								TestID:    "start-create",
								Text:      ed.T("startScreen.create"),
								BusyLabel: ed.T("startScreen.creating"),
								Variant:   ui.ButtonPrimary,
								Size:      ui.SizeSmall,
								Color:     func() core.Color { return p.TextPrimary },
								Tint:      func() core.Color { return p.Bg },
								TextStyle: core.Style{
									FontSize:   13,
									FontWeight: 600,
									Color:      p.Bg,
								},
								Style: core.Style{
									Height:       core.Pt(32),
									CornerRadius: 8,
									Padding:      core.Symmetric(0, 16),
								},
								Busy: func() bool {
									_ = rev.Get()
									return ed.CreatingProject()
								},
								Disabled: func() bool {
									_ = rev.Get()
									return ed.CreatingProject() || !ed.StartNameStatus().OK || !ed.GoToolchainOK()
								},
								OnPress: func() {
									ed.report(ed.CreateProjectFromStartScreen())
								},
							},
						},
					},
				},
			},
		},
	}
}

func startCreateOutputView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.CreatingProject() || len(ed.CreateProjectOutput()) > 0 || ed.CreateProjectErr() != nil
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "start-create-output",
				Style: core.Style{
					Background:   p.Panel,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					CornerRadius: 12,
					Padding:      core.All(12),
					Gap:          8,
				},
				Children: []core.Node{
					core.Text{
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.CreatingProject() {
								return ed.T("startScreen.creatingProjectHeading")
							}
							if ed.CreateProjectErr() != nil {
								return ed.T("startScreen.creationFailedHeading")
							}
							return ed.T("startScreen.creationOutputHeading")
						},
						Style: core.Style{
							FontSize:      11,
							FontWeight:    600,
							LetterSpacing: 0.7,
							Color:         p.TextMuted,
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.CreateProjectErr() != nil
						},
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID: "start-create-error",
								ContentFunc: func() string {
									_ = rev.Get()
									if err := ed.CreateProjectErr(); err != nil {
										return err.Error()
									}
									return ""
								},
								Style: core.Style{
									FontSize: 12,
									Color:    p.Removed.Fg,
								},
							}
						},
					},
					core.Text{
						TestID: "start-create-log",
						ContentFunc: func() string {
							_ = rev.Get()
							return strings.Join(ed.CreateProjectOutput(), "\n")
						},
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextSecondary,
						},
					},
				},
			}
		},
	}
}

func (e *Editor) localizedStartNameMessage(st StartNameValidation) string {
	switch {
	case st.OK && st.DisplayPath != "":
		return e.T("startScreen.validation.creates", l10n.Param("path", st.DisplayPath))
	case st.Message == "Enter a project name.":
		return e.T("startScreen.validation.enterName")
	case st.InvalidChars:
		return e.T("startScreen.validation.invalidChars")
	case st.Exists && st.DisplayPath != "":
		return e.T("startScreen.validation.alreadyExists", l10n.Param("path", st.DisplayPath))
	case st.NotWritable && strings.HasPrefix(st.Message, "No write access to "):
		return e.T("startScreen.validation.noWriteAccess", l10n.Param("path", strings.TrimPrefix(st.Message, "No write access to ")))
	case strings.HasPrefix(st.Message, "Invalid module path: "):
		return e.T("startScreen.validation.invalidModule", l10n.Param("err", strings.TrimPrefix(st.Message, "Invalid module path: ")))
	default:
		return st.Message
	}
}

func (e *Editor) localizedRelativeTime(rfc3339 string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return e.T("startScreen.relTime.justNow")
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return e.T("startScreen.relTime.oneMinAgo")
		}
		return e.T("startScreen.relTime.minsAgo", l10n.Param("count", mins))
	case d < 24*time.Hour:
		hrs := int(d.Hours())
		if hrs == 1 {
			return e.T("startScreen.relTime.oneHourAgo")
		}
		return e.T("startScreen.relTime.hoursAgo", l10n.Param("count", hrs))
	case d < 48*time.Hour:
		return e.T("startScreen.relTime.yesterday")
	case d < 7*24*time.Hour:
		return e.T("startScreen.relTime.daysAgo", l10n.Param("count", int(d.Hours()/24)))
	case d < 14*24*time.Hour:
		return e.T("startScreen.relTime.lastWeek")
	default:
		return t.UTC().Format("2006-01-02")
	}
}
