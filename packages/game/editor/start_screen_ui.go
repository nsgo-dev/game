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
	specAccentTint = core.RGBA(0x3B7BFF, 0.18)
	specRemoved    = mustSpecColor("#FF6B6B")
)

func mustSpecColor(hex string) core.Color {
	c, err := core.ParseColor(hex)
	if err != nil {
		panic(err)
	}
	return c
}

// templateSwatchColor returns a restrained, dark-theme card preview tint derived from the template name.
func templateSwatchColor(name string) core.Color {
	swatches := map[string]string{
		"starter":        "#262B35",
		"platformer2d":   "#253856",
		"rpg2d":          "#2D4533",
		"shmup2d":        "#482A3E",
		"fighting2d":     "#543329",
		"racing3d":       "#294156",
		"fps3d":          "#3D334D",
		"action3d":       "#31423F",
		"puzzle2d":       "#4A432D",
		"cards2d":        "#422E2E",
		"rts2d":          "#2B3F34",
		"roguelike2d":    "#342D45",
		"towerdefense2d": "#423F29",
		"idle":           "#284242",
		"visualnovel":    "#4A3342",
		"rhythm":         "#2D384B",
	}
	if hex, ok := swatches[strings.ToLower(name)]; ok {
		return mustSpecColor(hex)
	}
	palette := []string{"#262B35", "#253856", "#2D4533", "#3D334D", "#31423F", "#4A432D"}
	sum := 0
	for i := range len(name) {
		sum += int(name[i])
	}
	return mustSpecColor(palette[sum%len(palette)])
}

// StartScreenView renders the editor's Start Screen matching Start.dc.html, step 1 of FirstRun.dc.html,
// and the Spec.dc.html design tokens: left sidebar with navigation and conditional RECENT list,
// right pane with "What do you want to make?", dynamically populated template cards with filter chips,
// live-validated New Project bar with a single primary Create button, and streaming `game new` output.
func StartScreenView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	ed.ensureStartScreenInit()

	return core.View{
		TestID: "editor-start-screen",
		Style: core.Style{
			Direction:  core.Row,
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: specBg,
		},
		Children: []core.Node{
			startSidebarView(cx, ed, rev),
			startMainPaneView(cx, ed, rev),
		},
	}
}

func startSidebarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	navBtn := func(testID, label string, active bool, icon func() *core.SVG, onPress func()) core.Node {
		var bg core.Color
		fg := specTextSecondary
		if active {
			bg = specAccentTint
			fg = specTextStrong
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
			Background:  specPanel,
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
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
							Color:      specTextStrong,
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
								Color:   func() core.Color { return specLineRaised },
								TextStyle: core.Style{
									FontSize: 12,
									Color:    specText,
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
									Background: specLine,
								},
							},
							core.Text{
								TestID:  "start-recent-heading",
								Content: "RECENT",
								Style: core.Style{
									FontSize:      11,
									FontWeight:    600,
									LetterSpacing: 0.7,
									Color:         specTextMuted,
								},
							},
							ui.ScrollView{
								TestID:       "start-recent-scroll",
								Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
								ContentStyle: core.Style{Gap: 4},
								EdgeColor:    func() core.Color { return specPanel },
								Children: []core.Node{
									core.Dynamic(func(cx *core.Scope) core.Node {
										_ = recentRevMemo.Get()
										entries := ed.RecentProjects()
										now := ed.now()
										rows := make([]core.Node, 0, len(entries))
										for i, entry := range entries {
											rows = append(rows, startRecentRow(ed, i, entry, now))
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

func startRecentRow(ed *Editor, index int, entry RecentProjectEntry, now time.Time) core.Node {
	relTime := formatRelativeTime(entry.LastOpened, now)
	shortPath := shortenHomePath(entry.Path)
	meta := shortPath + " · " + relTime
	nameColor := specTextStrong
	if entry.Missing {
		nameColor = specTextSecondary
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
											Background:   specWarningFill,
											BorderWidth:  1,
											BorderColor:  specWarningEdge,
											CornerRadius: 4,
											Padding:      core.Symmetric(1, 6),
										},
										Children: []core.Node{
											core.Text{
												Content: "Missing",
												Style: core.Style{
													FontSize:   10,
													FontWeight: 600,
													Color:      specWarning,
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
							Color:    specTextMuted,
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
						Tint:    func() core.Color { return specTextMuted },
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
	return ui.ScrollView{
		TestID: "start-main-scroll",
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Height:     core.Pct(100),
			Background: specBg,
		},
		ContentStyle: core.Style{
			Padding: core.Symmetric(24, 32),
			Gap:     20,
		},
		EdgeColor: func() core.Color { return specBg },
		Children: []core.Node{
			startIdeaSectionView(cx, ed, rev),
			startTemplatesSectionView(cx, ed, rev),
			startCreateFormView(cx, ed, rev),
			startCreateOutputView(cx, ed, rev),
		},
	}
}

func startIdeaSectionView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.View{
		TestID: "start-idea-section",
		Style:  core.Style{Gap: 12},
		Children: []core.Node{
			core.Text{
				Content: "What do you want to make?",
				Style: core.Style{
					FontSize:   20,
					FontWeight: 600,
					Color:      specTextStrong,
				},
			},
			core.View{
				Style: core.Style{
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
					CornerRadius: 12,
					Padding:      core.All(16),
					Gap:          12,
				},
				Children: []core.Node{
					ui.Input{
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
							Background:   specPanel,
							BorderWidth:  1,
							BorderColor:  specLineRaised,
							CornerRadius: 6,
							Color:        specText,
							FontSize:     13,
						},
					},
					core.View{
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            12,
						},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									ed.startScreen.mu.Lock()
									notice := ed.startScreen.aiNotice
									ed.startScreen.mu.Unlock()
									if notice != "" {
										return notice
									}
									return "The agent picks the closest template, sets up the project and builds a first playable scene you can review."
								},
								Style: core.Style{
									FlexGrow:  1,
									FlexBasis: core.Pt(0),
									FontSize:  12,
									Color:     specTextMuted,
								},
							},
							// Secondary/quiet styling so Create stays the single primary button on the screen (per FirstRun.dc.html / Spec.dc.html).
							ui.Button{
								TestID:  "start-ai-btn",
								Text:    "Start with AI",
								Icon:    lucide.Sparkles,
								Variant: ui.ButtonSecondary,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return specLineRaised },
								Tint:    func() core.Color { return specText },
								TextStyle: core.Style{
									FontSize:   12.5,
									FontWeight: 500,
									Color:      specText,
								},
								Style: core.Style{
									Height:       core.Pt(32),
									CornerRadius: 8,
									Padding:      core.Symmetric(0, 12),
								},
								OnPress: func() {
									ed.startScreen.mu.Lock()
									ed.startScreen.aiNotice = "Start with AI connects an agent in a separate workflow; pick a template below to create a project now."
									ed.startScreen.mu.Unlock()
									ed.notify()
								},
							},
						},
					},
				},
			},
		},
	}
}

func startTemplatesSectionView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	filterChip := func(id, label string) core.Node {
		return ui.Button{
			TestID:  "start-filter-" + id,
			Text:    label,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color: func() core.Color {
				_ = rev.Get()
				if ed.StartTemplateFilter() == id {
					return specAccentTint
				}
				return specRaised
			},
			TextStyle: core.Style{
				FontSize:   12,
				FontWeight: 500,
				Color:      specText,
			},
			Style: core.Style{
				Height:       core.Pt(28),
				CornerRadius: 6,
				Padding:      core.Symmetric(0, 12),
				BorderWidth:  1,
				BorderColor:  specLineRaised,
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
									Color:      specTextStrong,
								},
							},
							core.Text{
								Content: "Each one is a small, playable game you grow into your own.",
								Style: core.Style{
									FontSize: 12.5,
									Color:    specTextMuted,
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
							Background:   specRaised,
							BorderWidth:  1,
							BorderColor:  specLineRaised,
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
									Color:    specTextSecondary,
								},
							},
							ui.Button{
								TestID:  "start-templates-show-all",
								Text:    "Show all",
								Variant: ui.ButtonSecondary,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return specLineRaised },
								TextStyle: core.Style{
									FontSize: 12,
									Color:    specText,
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
					borderColor := specLineRaised
					borderWidth := core.Points(1)
					if isSelected {
						borderColor = specAccent
						borderWidth = 2
					}
					cards = append(cards, core.View{
						TestID: "start-template-" + tmpl.Name,
						Role:   core.RoleButton,
						Label:  "Select template " + tmpl.DisplayTitle(),
						Style: core.Style{
							Width:        core.Pt(220),
							Background:   specRaised,
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
											Background:   specPanel,
											BorderWidth:  1,
											BorderColor:  specLineRaised,
											CornerRadius: 4,
											Padding:      core.Symmetric(2, 6),
										},
										Children: []core.Node{
											core.Text{
												Content: strings.ToUpper(tmpl.EffectiveDimension()),
												Style: core.Style{
													FontSize:   10,
													FontWeight: 600,
													Color:      specTextSecondary,
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
											Color:      specTextStrong,
										},
									},
									core.Text{
										Content: tmpl.Summary,
										Style: core.Style{
											FontSize: 12,
											Color:    specTextMuted,
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
	return core.View{
		TestID: "start-create-form",
		Style: core.Style{
			Borders:     core.Edges{Top: 1},
			BorderColor: specLine,
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
								Style:   core.Style{FontSize: 12, Color: specTextSecondary},
							},
							ui.Input{
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
									Background:   specPanel,
									BorderWidth:  1,
									BorderColor:  specLineRaised,
									CornerRadius: 6,
									Color:        specText,
								},
							},
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(200), Gap: 4},
						Children: []core.Node{
							core.Text{
								Content: "Folder",
								Style:   core.Style{FontSize: 12, Color: specTextSecondary},
							},
							core.View{
								Style: core.Style{Direction: core.Row, Gap: 6, AlignItems: core.AlignCenter},
								Children: []core.Node{
									ui.Input{
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
											Background:   specPanel,
											BorderWidth:  1,
											BorderColor:  specLineRaised,
											CornerRadius: 6,
											Color:        specText,
										},
									},
									ui.Button{
										TestID:  "start-new-folder-browse",
										Text:    "Browse…",
										Variant: ui.ButtonSecondary,
										Size:    ui.SizeSmall,
										Color:   func() core.Color { return specLineRaised },
										TextStyle: core.Style{
											FontSize: 12,
											Color:    specText,
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
								Style:   core.Style{FontSize: 12, Color: specTextSecondary},
							},
							ui.Input{
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
									Background:   specPanel,
									BorderWidth:  1,
									BorderColor:  specLineRaised,
									CornerRadius: 6,
									Color:        specText,
								},
							},
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
											Color:    specAdded,
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
									Color:    specRemoved,
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
									Color:   func() core.Color { return specLineRaised },
									TextStyle: core.Style{
										FontSize: 12,
										Color:    specTextStrong,
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
									Color:   func() core.Color { return specLineRaised },
									TextStyle: core.Style{
										FontSize: 12,
										Color:    specTextStrong,
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
									Color:    specTextSecondary,
								},
							},
							// Single primary button on the screen: #101216 on #FFFFFF (18.75:1 AAA in Spec.dc.html).
							ui.Button{
								TestID:    "start-create",
								Text:      "Create",
								BusyLabel: "Creating…",
								Variant:   ui.ButtonPrimary,
								Size:      ui.SizeSmall,
								Color:     func() core.Color { return specTextStrong },
								Tint:      func() core.Color { return specBg },
								TextStyle: core.Style{
									FontSize:   13,
									FontWeight: 600,
									Color:      specBg,
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
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.CreatingProject() || len(ed.CreateProjectOutput()) > 0 || ed.CreateProjectErr() != nil
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "start-create-output",
				Style: core.Style{
					Background:   specPanel,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
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
							Color:         specTextMuted,
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
									Color:    specRemoved,
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
							Color:    specTextSecondary,
						},
					},
				},
			}
		},
	}
}
