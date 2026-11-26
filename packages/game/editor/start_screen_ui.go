package editor

import (
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Additional Spec.dc.html color tokens used by the Start Screen (shared tokens live in profiler_ui.go).
var (
	specAccentTint = specAlpha(darkSpecPalette.Accent.Fg, 0.18)
	specRemoved    = darkSpecPalette.Removed.Fg
)

// templateSwatchColor returns a restrained, dark-theme card preview tint composed strictly from Spec tokens.
func templateSwatchColor(name string) core.Color {
	base := darkSpecPalette.Raised
	swatch := func(role core.Color, alpha float32) core.Color {
		return CompositeOver(specAlpha(role, alpha), base)
	}
	swatches := map[string]core.Color{
		"starter":        darkSpecPalette.LineRaised,
		"platformer2d":   swatch(darkSpecPalette.Accent.Fg, 0.24),
		"rpg2d":          swatch(darkSpecPalette.Added.Fg, 0.22),
		"shmup2d":        swatch(darkSpecPalette.Removed.Fg, 0.22),
		"fighting2d":     swatch(darkSpecPalette.Changed.Fg, 0.24),
		"racing3d":       swatch(darkSpecPalette.Accent.Fg, 0.20),
		"fps3d":          swatch(darkSpecPalette.Agent.Fg, 0.22),
		"action3d":       swatch(darkSpecPalette.Added.Fg, 0.18),
		"puzzle2d":       swatch(darkSpecPalette.Warning.Fg, 0.20),
		"cards2d":        swatch(darkSpecPalette.Removed.Fg, 0.18),
		"rts2d":          swatch(darkSpecPalette.Added.Fg, 0.20),
		"roguelike2d":    swatch(darkSpecPalette.Agent.Fg, 0.18),
		"towerdefense2d": swatch(darkSpecPalette.Warning.Fg, 0.18),
		"idle":           swatch(darkSpecPalette.Added.Fg, 0.16),
		"visualnovel":    swatch(darkSpecPalette.Agent.Fg, 0.24),
		"rhythm":         swatch(darkSpecPalette.Accent.Fg, 0.18),
	}
	if col, ok := swatches[strings.ToLower(name)]; ok {
		return col
	}
	palette := []core.Color{
		darkSpecPalette.LineRaised,
		swatch(darkSpecPalette.Accent.Fg, 0.24),
		swatch(darkSpecPalette.Added.Fg, 0.22),
		swatch(darkSpecPalette.Agent.Fg, 0.22),
		swatch(darkSpecPalette.Changed.Fg, 0.20),
		swatch(darkSpecPalette.Warning.Fg, 0.20),
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
	p := ed.SpecPalette(cx)

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
						Content: "NSGo Editor",
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
								Text:    "Back",
								Label:   "Return to open editor",
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
					navBtn("start-nav-new", "New project", true, lucide.Plus, func() {
						ed.SetStartScreenOpen(true)
					}),
					navBtn("start-open-folder", "Open folder…", false, lucide.FolderOpen, func() {
						ed.report(ed.BrowseAndOpenProject())
					}),
					navBtn("start-open-demo", "Open demo", false, lucide.Play, func() {
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
								Content: "RECENT",
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
	relTime := formatRelativeTime(entry.LastOpened, now)
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
				Label:  "Open recent project " + entry.Name,
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					Padding:      core.Symmetric(8, 12),
					CornerRadius: 6,
					Gap:          2,
				},
				OnPress: func(core.PressEvent) {
					if entryMissing {
						ed.reportStatusError("Project folder is missing: " + entryPath)
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
												Content: "Missing",
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
						Label:   "Remove missing recent project " + entry.Name,
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

func startIdeaSectionView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	return core.View{
		TestID: "start-idea-section",
		Style:  core.Style{Gap: 12},
		Children: []core.Node{
			core.Text{
				Content: "What do you want to make?",
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
						Label:       "Describe your game",
						Multiline:   true,
						Placeholder: "A cozy farming game where you grow glowing mushrooms in a cave and trade them with moths at night…",
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
								status := "No agent connected yet"
								if ed.FirstRunAgentConnected() {
									_, name, _ := ed.FirstRunConnectedAgent()
									dotColor = p.Added.Fg
									status = name + " connected"
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
								Text:    "Start with AI",
								Icon:    lucide.Sparkles,
								Variant: specBtnAccent,
								Disabled: func() bool {
									_ = rev.Get()
									return ed.CreatingProject() || !ed.StartNameStatus().OK
								},
								OnPress: func() {
									ed.report(ed.StartWithAI())
								},
							}),
						},
					},
					startWhatHappensNextView(p),
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
								Content: "Or start from a template",
								Style: core.Style{
									FontSize:   14,
									FontWeight: 600,
									Color:      p.TextPrimary,
								},
							},
							core.Text{
								Content: "Each one is a small, playable game you grow into your own.",
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
							filterChip("all", "All"),
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
								Content: "No templates match the active filter.",
								Style: core.Style{
									FontSize: 12.5,
									Color:    p.TextSecondary,
								},
							},
							ui.Button{
								TestID:  "start-templates-show-all",
								Text:    "Show all",
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
						Label:  "Select template " + tmpl.DisplayTitle(),
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
									Background:     templateSwatchColor(tmpl.Name),
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
								Content: "Name",
								Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
							},
							specInputField(cx, ed, ui.Input{
								TestID:      "start-new-name",
								Label:       "Project name",
								Placeholder: "my-game",
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
								Content: "Folder",
								Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
							},
							core.View{
								Style: core.Style{Direction: core.Row, Gap: 6, AlignItems: core.AlignCenter},
								Children: []core.Node{
									specInputField(cx, ed, ui.Input{
										TestID:      "start-new-folder",
										Label:       "Parent folder",
										Placeholder: "~/games",
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
										Text:    "Browse…",
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
								Content: "Module path (optional)",
								Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
							},
							specInputField(cx, ed, ui.Input{
								TestID:      "start-new-module",
								Label:       "Go module path",
								Placeholder: "example.com/my-game",
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
						if st.OK {
							return core.View{
								TestID: "start-name-status",
								Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
								Children: []core.Node{
									core.Text{
										Content: st.Message,
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
								Content: st.Message,
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
									Text:    "Open it",
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
									Text:    "Use " + suggested,
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
											return "Template: " + t.DisplayTitle()
										}
									}
									if sel == "" {
										return "Template: Starter"
									}
									return "Template: " + sel
								},
								Style: core.Style{
									FontSize: 12.5,
									Color:    p.TextSecondary,
								},
							},
							// Single primary button on the screen: inverted Spec TextPrimary/Bg (AAA in both Dark and Light).
							ui.Button{
								TestID:    "start-create",
								Text:      "Create",
								BusyLabel: "Creating…",
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
									return ed.CreatingProject() || !ed.StartNameStatus().OK
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
								return "CREATING PROJECT…"
							}
							if ed.CreateProjectErr() != nil {
								return "PROJECT CREATION FAILED"
							}
							return "PROJECT CREATION OUTPUT"
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
