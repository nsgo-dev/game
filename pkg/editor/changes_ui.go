package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived alpha tints for diff lines and status badges from spec_theme.yaml.
var (
	specDiffAddBg  = specAlpha(darkSpecPalette.Added.Fg, 0.08)
	specDiffDelBg  = specAlpha(darkSpecPalette.Removed.Fg, 0.08)
	specBadgeAddBg = specAlpha(darkSpecPalette.Added.Fg, 0.14)
	specBadgeModBg = specAlpha(darkSpecPalette.Changed.Fg, 0.16)
	specBadgeDelBg = specAlpha(darkSpecPalette.Removed.Fg, 0.14)
)

// ChangesPanelView renders the full-window Changes & Commit screen matching
// docs/design/editor/VersionControl.dc.html (screen 1) using only Spec.dc.html tokens.
func ChangesPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	keyOpt := core.KeyOptions{When: core.KeyNotEditing}
	cx.OnKeyWith("escape", keyOpt, func() {
		if ed.ChangesOpen() {
			ed.CloseChangesPanel()
		}
	})

	changesRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.changesStateRev()
	})
	draftRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.changesDraftRev()
	})

	return core.View{
		TestID: "changes-panel",
		Role:   core.RoleRegion,
		Label:  "Changes and commit",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			changesHeaderView(cx, ed, changesRev),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					changesSidebarView(cx, ed, changesRev),
					changesCenterView(cx, ed, rev, changesRev),
					changesCommitPaneView(cx, ed, changesRev, draftRev),
				},
			},
		},
	}
}

func changesHeaderView(cx *core.Scope, ed *Editor, changesRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = changesRev.Get()
		sess := ed.ChangesSession()
		if sess == nil {
			return core.View{}
		}

		aheadParts := make([]string, 0, 2)
		if sess.Ahead > 0 {
			aheadParts = append(aheadParts, fmt.Sprintf("ahead %d", sess.Ahead))
		}
		if sess.Behind > 0 {
			aheadParts = append(aheadParts, fmt.Sprintf("behind %d", sess.Behind))
		}
		aheadLabel := strings.Join(aheadParts, " · ")

		branchItems := make([]core.Node, 0, max(1, len(sess.Branches)))
		for i, br := range sess.Branches {
			branchName := br
			label := branchName
			if branchName == sess.Branch {
				label = "✓ " + branchName
			}
			branchItems = append(branchItems, ui.MenuItem{
				TestID: fmt.Sprintf("changes-branch-item-%d", i),
				Text:   label,
				OnSelect: func() {
					ed.report(ed.GitSwitchBranch(branchName))
				},
			})
		}
		if len(branchItems) == 0 {
			branchItems = append(branchItems, ui.MenuItem{
				TestID:   "changes-branch-item-0",
				Text:     sess.Branch,
				Disabled: func() bool { return true },
			})
		}

		branchChildren := []core.Node{
			core.Icon{
				SVG: lucide.GitBranch(),
				Style: core.Style{
					Width:  core.Pt(14),
					Height: core.Pt(14),
					Color:  specText,
				},
			},
			core.Text{
				TestID:  "changes-branch-name",
				Content: sess.Branch,
				Style: core.Style{
					FontFamily: "monospace",
					FontSize:   11.5,
					Color:      specText,
				},
			},
		}
		if aheadLabel != "" {
			branchChildren = append(branchChildren, core.Text{
				TestID:  "changes-ahead-label",
				Content: aheadLabel,
				Style: core.Style{
					FontFamily: "monospace",
					FontSize:   11,
					Color:      specTextMuted,
				},
			})
		}
		branchChildren = append(branchChildren, core.Icon{
			SVG: lucide.ChevronDown(),
			Style: core.Style{
				Width:  core.Pt(12),
				Height: core.Pt(12),
				Color:  specTextMuted,
			},
		})

		branchFace := core.View{
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Height:       core.Pt(30),
				Padding:      core.Symmetric(0, 10),
				Gap:          8,
				CornerRadius: 8,
				Background:   specRaised,
				BorderWidth:  1,
				BorderColor:  specLineRaised,
			},
			Children: branchChildren,
		}

		return core.View{
			TestID: "changes-header",
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Height:         core.Pt(44),
				FlexShrink:     0,
				Padding:        core.Symmetric(0, 12),
				Gap:            12,
				Background:     specBg,
				Borders:        core.Edges{Bottom: 1},
				BorderColor:    specLine,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        8,
					},
					Children: []core.Node{
						ui.Button{
							TestID:  "changes-back-to-editor",
							Icon:    lucide.ArrowLeft,
							Label:   "Back to scene editor",
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Tint:    func() core.Color { return specTextSecondary },
							Style: core.Style{
								Height:       core.Pt(28),
								CornerRadius: 6,
								Padding:      core.Symmetric(0, 6),
							},
							OnPress: func() {
								ed.CloseChangesPanel()
							},
						},
						core.View{
							Style: core.Style{
								Width:        core.Pt(22),
								Height:       core.Pt(22),
								CornerRadius: 6,
								Background:   specAccent,
							},
						},
						core.Text{
							Content: sess.ProjectName,
							Style: core.Style{
								FontSize:   12.5,
								FontWeight: 600,
								Color:      specTextStrong,
							},
						},
						core.Text{
							Content: "/",
							Style: core.Style{
								FontSize: 12.5,
								Color:    specTextMuted,
							},
						},
						core.Text{
							Content: sess.SceneName,
							Style: core.Style{
								FontSize: 12.5,
								Color:    specTextSecondary,
							},
						},
						core.View{Style: core.Style{Width: core.Pt(8)}},
						ui.Menu{
							TestID:   "changes-branch-menu",
							Label:    "Switch branch, current " + sess.Branch,
							Face:     branchFace,
							Children: branchItems,
						},
					},
				},
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        8,
					},
					Children: []core.Node{
						ui.Button{
							TestID:  "changes-refresh",
							Icon:    lucide.RefreshCw,
							Label:   "Refresh changes",
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return specRaised },
							Tint:    func() core.Color { return specTextSecondary },
							Style: core.Style{
								Height:       core.Pt(30),
								CornerRadius: 8,
								Padding:      core.Symmetric(0, 8),
								BorderWidth:  1,
								BorderColor:  specLineRaised,
							},
							OnPress: func() {
								ed.report(ed.RefreshChanges())
							},
						},
						ui.Button{
							TestID:  "changes-pull",
							Text:    "Pull",
							Icon:    lucide.Download,
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Disabled: func() bool {
								return ed.ChangesBusy()
							},
							Color: func() core.Color { return specLineRaised },
							Tint:  func() core.Color { return specText },
							TextStyle: core.Style{
								FontSize:   12,
								FontWeight: 500,
								Color:      specText,
							},
							Style: core.Style{
								Height:       core.Pt(30),
								CornerRadius: 8,
								Padding:      core.Symmetric(0, 12),
							},
							OnPress: func() {
								ed.report(ed.GitPullAsync(cx))
							},
						},
						ui.Button{
							TestID:  "changes-play",
							Text:    "Play",
							Icon:    lucide.Play,
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return specTextStrong },
							Tint:    func() core.Color { return specBg },
							TextStyle: core.Style{
								FontSize:   12,
								FontWeight: 600,
								Color:      specBg,
							},
							Style: core.Style{
								Height:       core.Pt(30),
								CornerRadius: 8,
								Padding:      core.Symmetric(0, 14),
							},
							OnPress: func() {
								ed.CloseChangesPanel()
								ed.report(ed.TogglePlay())
							},
						},
					},
				},
			},
		}
	})
}

func changesSidebarView(cx *core.Scope, ed *Editor, changesRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = changesRev.Get()
		sess := ed.ChangesSession()
		if sess == nil {
			return core.View{}
		}

		// Group files by ChangesCategory in fixed order: SCENES, DATA, CODE, ASSETS.
		type indexedFile struct {
			idx  int
			file ChangedFile
		}
		grouped := make(map[ChangesCategory][]indexedFile, len(changesCategoryOrder))
		for i, f := range sess.Files {
			grouped[f.Category] = append(grouped[f.Category], indexedFile{idx: i, file: f})
		}

		listNodes := make([]core.Node, 0, len(sess.Files)+len(changesCategoryOrder))
		for _, cat := range changesCategoryOrder {
			items := grouped[cat]
			if len(items) == 0 {
				continue
			}
			listNodes = append(listNodes, core.View{
				Style: core.Style{
					Height:         core.Pt(28),
					JustifyContent: core.JustifyEnd,
					Padding:        core.Edges{Left: 8, Right: 8, Bottom: 4},
				},
				Children: []core.Node{
					core.Text{
						TestID:  "changes-group-" + strings.ToLower(string(cat)),
						Content: string(cat),
						Style: core.Style{
							FontSize:      11,
							FontWeight:    600,
							LetterSpacing: 0.7,
							Color:         specTextMuted,
						},
					},
				},
			})

			for _, it := range items {
				f := it.file
				idx := it.idx
				filePath := f.Path
				selected := sess.SelectedPath == filePath

				var rowBg core.Color
				var leftBorder core.Points
				if selected {
					rowBg = specAccentTint
					leftBorder = 2
				}

				var boxBg core.Color
				boxBorder := specTextMuted
				var boxBorderW core.Points = 1.5
				var checkChildren []core.Node
				included := f.Included
				if included {
					boxBg = specAccent
					boxBorderW = 0
					checkChildren = []core.Node{
						core.Icon{
							SVG: lucide.Check(),
							Style: core.Style{
								Width:  core.Pt(10),
								Height: core.Pt(10),
								Color:  specTextStrong,
							},
						},
					}
				}

				badgeBg := specBadgeModBg
				badgeFg := specChanged
				switch f.StatusCode {
				case "A":
					badgeBg = specBadgeAddBg
					badgeFg = specAdded
				case "D":
					badgeBg = specBadgeDelBg
					badgeFg = specProblem
				}

				listNodes = append(listNodes, core.View{
					TestID: fmt.Sprintf("changes-file-%d", idx),
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Padding:      core.Symmetric(2, 8),
						Gap:          10,
						CornerRadius: 6,
						Background:   rowBg,
						Borders:      core.Edges{Left: leftBorder},
						BorderColor:  specAccent,
					},
					Children: []core.Node{
						core.View{
							TestID:  fmt.Sprintf("changes-check-%d", idx),
							Role:    core.RoleCheckbox,
							Checked: func() bool { return included },
							Label:   "Include " + f.DisplayName + " in the commit",
							Style: core.Style{
								Width:          core.Pt(16),
								Height:         core.Pt(16),
								FlexShrink:     0,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								CornerRadius:   4,
								Background:     boxBg,
								BorderWidth:    boxBorderW,
								BorderColor:    boxBorder,
								FocusVisible:   specFocusRingStyle,
							},
							OnPress: func(core.PressEvent) {
								ed.report(ed.ToggleChangeIncluded(filePath))
							},
							Children: checkChildren,
						},
						core.View{
							TestID: fmt.Sprintf("changes-pick-%d", idx),
							Role:   core.RoleButton,
							Label:  f.DisplayName,
							Style: core.Style{
								FlexGrow:     1,
								FlexBasis:    core.Pt(0),
								Padding:      core.Symmetric(5, 0),
								Gap:          2,
								FocusVisible: specRowFocusRingStyle,
							},
							OnPress: func(core.PressEvent) {
								ed.report(ed.SelectChangedFile(filePath))
							},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Direction:  core.Row,
										AlignItems: core.AlignCenter,
										Gap:        8,
									},
									Children: []core.Node{
										core.Text{
											Content: f.DisplayName,
											Style: core.Style{
												FlexGrow:   1,
												FlexBasis:  core.Pt(0),
												FontFamily: "monospace",
												FontSize:   11.5,
												Color:      specTextStrong,
											},
										},
										core.View{
											Style: core.Style{
												Width:          core.Pt(18),
												Height:         core.Pt(18),
												FlexShrink:     0,
												CornerRadius:   6,
												AlignItems:     core.AlignCenter,
												JustifyContent: core.JustifyCenter,
												Background:     badgeBg,
											},
											Children: []core.Node{
												core.Text{
													Content: f.StatusCode,
													Style: core.Style{
														FontFamily: "monospace",
														FontSize:   10.5,
														FontWeight: 500,
														Color:      badgeFg,
													},
												},
											},
										},
									},
								},
								core.Text{
									Content: f.Detail,
									Style: core.Style{
										FontSize: 11.5,
										Color:    specTextMuted,
									},
								},
							},
						},
					},
				})
			}
		}

		if len(listNodes) == 0 {
			listNodes = append(listNodes, core.View{
				Style: core.Style{Padding: core.All(12)},
				Children: []core.Node{
					core.Text{
						TestID:  "changes-empty",
						Content: "Working tree clean — no changes since " + sess.HeadSHA,
						Style: core.Style{
							FontSize: 12,
							Color:    specTextMuted,
						},
					},
				},
			})
		}

		return core.View{
			TestID: "changes-sidebar",
			Style: core.Style{
				Width:       core.Pt(300),
				FlexShrink:  0,
				Height:      core.Pct(100),
				Background:  specBg,
				Borders:     core.Edges{Right: 1},
				BorderColor: specLine,
				Padding:     core.All(8),
				Gap:         2,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Height:         core.Pt(28),
						Padding:        core.Symmetric(0, 8),
					},
					Children: []core.Node{
						core.Text{
							Content: "CHANGES",
							Style: core.Style{
								FontSize:      11,
								FontWeight:    600,
								LetterSpacing: 0.7,
								Color:         specTextMuted,
							},
						},
						core.Text{
							TestID:  "changes-since-head",
							Content: "since " + sess.HeadSHA,
							Style: core.Style{
								FontFamily: "monospace",
								FontSize:   11,
								FontWeight: 500,
								Color:      specTextMuted,
							},
						},
					},
				},
				ui.ScrollView{
					TestID:       "changes-file-list",
					Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
					ContentStyle: core.Style{Gap: 2},
					EdgeColor:    func() core.Color { return specBg },
					Children:     listNodes,
				},
			},
		}
	})
}

func changesCenterView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], changesRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = changesRev.Get()
		sess := ed.ChangesSession()
		if sess == nil {
			return core.View{}
		}
		cur := sess.SelectedChangedFile()

		// Center sub-header.
		subLeft := []core.Node{}
		subRight := []core.Node{}
		if cur != nil {
			subLeft = append(subLeft,
				core.Text{
					TestID:  "changes-selected-name",
					Content: cur.DisplayName,
					Style: core.Style{
						FontFamily: "monospace",
						FontSize:   11.5,
						Color:      specTextStrong,
					},
				},
				core.Text{
					Content: "compared with HEAD",
					Style: core.Style{
						FontSize: 12,
						Color:    specTextMuted,
					},
				},
			)
			if cur.Category == ChangesCategoryScenes {
				segBtn := func(testID, label string, mode ChangesSceneViewMode) core.Node {
					active := sess.SceneView == mode
					var bg core.Color
					fg := specTextMuted
					if active {
						bg = specLineRaised
						fg = specTextStrong
					}
					return ui.Button{
						TestID:  testID,
						Text:    label,
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Color:   func() core.Color { return bg },
						TextStyle: core.Style{
							FontSize:   11.5,
							FontWeight: 500,
							Color:      fg,
						},
						Style: core.Style{
							Height:       core.Pt(26),
							CornerRadius: 6,
							Padding:      core.Symmetric(0, 10),
						},
						OnPress: func() {
							ed.SetChangesSceneView(mode)
						},
					}
				}
				subRight = append(subRight, core.View{
					Style: core.Style{
						Direction:    core.Row,
						Gap:          2,
						Padding:      core.All(3),
						CornerRadius: 9,
						Background:   specRaised,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
					},
					Children: []core.Node{
						segBtn("changes-scene-mode-side", "Side by side", ChangesSceneViewSideBySide),
						segBtn("changes-scene-mode-head", "HEAD", ChangesSceneViewHead),
						segBtn("changes-scene-mode-now", "Now", ChangesSceneViewNow),
					},
				})
			}
			if cur.Category == ChangesCategoryCode || cur.Category == ChangesCategoryData || cur.Category == ChangesCategoryScenes {
				filePath := cur.Path
				subRight = append(subRight, ui.Button{
					TestID:  "changes-open-in-editor",
					Text:    "Open in editor",
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return specLineRaised },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      specText,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 12),
					},
					OnPress: func() {
						ed.report(ed.OpenChangedFileInEditor(filePath))
					},
				})
			}
		} else {
			subLeft = append(subLeft, core.Text{
				Content: "No file selected",
				Style: core.Style{
					FontSize: 12,
					Color:    specTextMuted,
				},
			})
		}

		// Main comparison body.
		var body core.Node
		switch {
		case cur == nil:
			body = core.View{
				Style: core.Style{
					FlexGrow:       1,
					FlexBasis:      core.Pt(0),
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Background:     specPanel,
				},
				Children: []core.Node{
					core.Text{
						Content: "All changes are committed.",
						Style:   core.Style{FontSize: 13, Color: specTextMuted},
					},
				},
			}
		case cur.Category == ChangesCategoryScenes && cur.Scene != nil:
			body = changesSceneCompareView(sess, cur)
		case cur.Category == ChangesCategoryAssets && cur.Audio != nil:
			body = changesAudioCompareView(ed, cur)
		case cur.Category == ChangesCategoryAssets:
			body = changesGenericAssetView(cur)
		default:
			body = changesTextDiffView(cx, ed, sess, cur)
		}

		// Bottom bar inside Changes screen matching VersionControl.dc.html.
		changesBtnLabel := "Changes"
		if len(sess.Files) > 0 {
			changesBtnLabel = fmt.Sprintf("Changes · %d", len(sess.Files))
		}
		bottomBar := core.View{
			TestID: "changes-bottom-bar",
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Height:         core.Pt(32),
				FlexShrink:     0,
				Padding:        core.Symmetric(0, 14),
				Gap:            16,
				Background:     specBg,
				Borders:        core.Edges{Top: 1},
				BorderColor:    specLine,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 16},
					Children: []core.Node{
						core.Text{
							Content: "Layer " + ed.activeLayerReadout(),
							Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
						},
						core.Text{
							Content: ed.Tool.String(),
							Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
						},
					},
				},
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
					Children: []core.Node{
						core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:        core.Pt(6),
										Height:       core.Pt(6),
										CornerRadius: 3,
										Background:   specAdded,
									},
								},
								core.Text{
									ContentFunc: func() string {
										_ = rev.Get()
										if ed.Project == nil || ed.ProjectPath == "" {
											return "No problems"
										}
										return ed.problemsStatusText()
									},
									Style: core.Style{FontSize: 11.5, Color: specTextMuted},
								},
							},
						},
						ui.Button{
							TestID:  "changes-status-close",
							Text:    changesBtnLabel,
							Icon:    lucide.GitBranch,
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return specAccentTint },
							Tint:    func() core.Color { return specTextStrong },
							TextStyle: core.Style{
								FontSize:   11.5,
								FontWeight: 500,
								Color:      specTextStrong,
							},
							Style: core.Style{
								Height:       core.Pt(24),
								CornerRadius: 6,
								Padding:      core.Symmetric(0, 8),
								BorderWidth:  1,
								BorderColor:  specAccentEdge,
							},
							OnPress: func() {
								ed.CloseChangesPanel()
							},
						},
					},
				},
			},
		}

		return core.View{
			TestID: "changes-center",
			Style: core.Style{
				FlexGrow:   1,
				FlexBasis:  core.Pt(0),
				Height:     core.Pct(100),
				Background: specBg,
				Overflow:   core.Hidden,
			},
			Children: []core.Node{
				core.View{
					TestID: "changes-center-header",
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Height:         core.Pt(44),
						FlexShrink:     0,
						Padding:        core.Symmetric(0, 16),
						Gap:            12,
						Borders:        core.Edges{Bottom: 1},
						BorderColor:    specLine,
					},
					Children: []core.Node{
						core.View{
							Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10},
							Children: subLeft,
						},
						core.View{
							Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
							Children: subRight,
						},
					},
				},
				body,
				bottomBar,
			},
		}
	})
}

func changesSceneCompareView(sess *ChangesSession, f *ChangedFile) core.Node {
	sc := f.Scene

	sceneCard := func(testID, title, badge string, doc *sceneformat.SceneDoc, built *sceneformat.BuiltScene, isHead bool) core.Node {
		return core.View{
			Style: core.Style{
				FlexGrow:     1,
				FlexBasis:    core.Pt(0),
				Height:       core.Pct(100),
				Padding:      core.All(12),
				Gap:          8,
				CornerRadius: 12,
				Background:   specPanel,
				BorderWidth:  1,
				BorderColor:  specLine,
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
							Content: title,
							Style: core.Style{
								FontSize:      11,
								FontWeight:    600,
								LetterSpacing: 0.7,
								Color:         specTextStrong,
							},
						},
						core.Text{
							Content: badge,
							Style: core.Style{
								FontFamily: "monospace",
								FontSize:   11,
								Color:      specTextMuted,
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						FlexGrow:     1,
						FlexBasis:    core.Pt(0),
						Width:        core.Pct(100),
						CornerRadius: 6,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
						Background:   specBg,
						Overflow:     core.Hidden,
					},
					Children: []core.Node{
						core.Canvas{
							TestID: testID,
							Label:  title + " scene preview",
							Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
							Draw: func(d *core.Drawing, size core.Size) {
								drawChangesScenePreview(d, size, sc, doc, built, isHead)
							},
						},
					},
				},
			},
		}
	}

	var previewNode core.Node
	switch sess.SceneView {
	case ChangesSceneViewHead:
		previewNode = sceneCard("changes-canvas-head", "HEAD", trimSceneFilename(f.DisplayName)+" at "+sess.HeadSHA, sc.HeadDoc, sc.HeadBuilt, true)
	case ChangesSceneViewNow:
		previewNode = sceneCard("changes-canvas-now", "NOW", f.DisplayName, sc.NowDoc, sc.NowBuilt, false)
	default:
		previewNode = core.View{
			Style: core.Style{
				Direction: core.Row,
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				Gap:       12,
			},
			Children: []core.Node{
				sceneCard("changes-canvas-head", "HEAD", sess.HeadSHA, sc.HeadDoc, sc.HeadBuilt, true),
				sceneCard("changes-canvas-now", "NOW", "working tree", sc.NowDoc, sc.NowBuilt, false),
			},
		}
	}

	legendItems := make([]core.Node, 0, 3)
	if len(sc.Added) > 0 {
		legendItems = append(legendItems, changesLegendChip("changes-legend-added", specAdded, "added · "+joinObjectNames(sc.Added, 5)))
	}
	if len(sc.Changed) > 0 {
		legendItems = append(legendItems, changesLegendChip("changes-legend-changed", specChanged, "changed · "+joinObjectNames(sc.Changed, 5)))
	}
	if len(sc.Removed) > 0 {
		legendItems = append(legendItems, changesLegendChip("changes-legend-removed", specProblem, "removed · "+joinObjectNames(sc.Removed, 5)))
	}
	if len(legendItems) == 0 {
		legendItems = append(legendItems, core.Text{
			Content: "No object-level differences against HEAD",
			Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
		})
	}

	// Per-object detail chips for changed/added/removed objects.
	badgeNodes := make([]core.Node, 0, len(sc.Added)+len(sc.Changed)+len(sc.Removed))
	for _, o := range sc.Added {
		badgeNodes = append(badgeNodes, changesObjectBadge("added · "+o.Name, specAddedFill, specAddedEdge, specAdded))
	}
	for _, o := range sc.Changed {
		label := "changed · " + o.Name
		if o.Summary != "" && o.Summary != "changed" {
			label += " (" + o.Summary + ")"
		}
		badgeNodes = append(badgeNodes, changesObjectBadge(label, specBadgeModBg, specChanged, specChanged))
	}
	for _, o := range sc.Removed {
		badgeNodes = append(badgeNodes, changesObjectBadge("removed · "+o.Name, specBadgeDelBg, specProblemEdge, specProblem))
	}

	children := []core.Node{
		previewNode,
		core.View{
			TestID: "changes-scene-legend",
			Style: core.Style{
				Direction:  core.Row,
				Wrap:       core.Wrap,
				AlignItems: core.AlignCenter,
				Gap:        16,
				FlexShrink: 0,
			},
			Children: legendItems,
		},
	}
	if len(badgeNodes) > 0 {
		children = append(children, core.View{
			TestID: "changes-scene-badges",
			Style: core.Style{
				Direction:  core.Row,
				Wrap:       core.Wrap,
				Gap:        6,
				FlexShrink: 0,
			},
			Children: badgeNodes,
		})
	}

	return core.View{
		TestID: "changes-scene-compare",
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Padding:    core.All(16),
			Gap:        12,
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: children,
	}
}

func changesLegendChip(testID string, color core.Color, text string) core.Node {
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        6,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:        core.Pt(10),
					Height:       core.Pt(10),
					CornerRadius: 2,
					BorderWidth:  1.5,
					BorderColor:  color,
				},
			},
			core.Text{
				TestID:  testID,
				Content: text,
				Style: core.Style{
					FontSize: 11.5,
					Color:    specTextSecondary,
				},
			},
		},
	}
}

func changesObjectBadge(label string, bg, border, fg core.Color) core.Node {
	return core.View{
		Style: core.Style{
			Height:         core.Pt(24),
			Padding:        core.Symmetric(0, 8),
			CornerRadius:   6,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     bg,
			BorderWidth:    1,
			BorderColor:    border,
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style: core.Style{
					FontFamily: "monospace",
					FontSize:   11,
					Color:      fg,
				},
			},
		},
	}
}

func changesTextDiffView(cx *core.Scope, ed *Editor, sess *ChangesSession, f *ChangedFile) core.Node {
	// Check status bar (nsgo check & nsgo test + timestamp).
	checkIconColor := specTextMuted
	checkText := "nsgo check · not run"
	testIconColor := specTextMuted
	testText := "nsgo test · not run"
	timeText := ""
	if sess.Checks.Ran {
		if sess.Checks.CheckPassed {
			checkIconColor = specAdded
			checkText = nonEmpty(sess.Checks.CheckSummary, "nsgo check")
		} else {
			checkIconColor = specProblem
			checkText = nonEmpty(sess.Checks.CheckSummary, "nsgo check failed")
		}
		if sess.Checks.TestPassed {
			testIconColor = specAdded
			if sess.Checks.TestSummary != "" {
				testText = sess.Checks.TestSummary
			} else if sess.Checks.TestCount > 0 {
				testText = fmt.Sprintf("nsgo test · %d passed", sess.Checks.TestCount)
			} else {
				testText = "nsgo test · passed"
			}
		} else {
			testIconColor = specProblem
			testText = nonEmpty(sess.Checks.TestSummary, "nsgo test · failed")
		}
		timeText = "ran " + formatCheckRelativeTime(sess.Checks.RanAt, ed.now())
	}

	checksBar := core.View{
		TestID: "changes-checks-bar",
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Height:         core.Pt(36),
			FlexShrink:     0,
			Padding:        core.Symmetric(0, 16),
			Gap:            12,
			Background:     specPanel,
			Borders:        core.Edges{Bottom: 1},
			BorderColor:    specLine,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 16},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
						Children: []core.Node{
							core.Icon{
								SVG:   lucide.Check(),
								Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: checkIconColor},
							},
							core.Text{
								TestID:  "changes-check-status",
								Content: checkText,
								Style:   core.Style{FontSize: 12, Color: specTextSecondary},
							},
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
						Children: []core.Node{
							core.Icon{
								SVG:   lucide.Check(),
								Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: testIconColor},
							},
							core.Text{
								TestID:  "changes-test-status",
								Content: testText,
								Style:   core.Style{FontSize: 12, Color: specTextSecondary},
							},
						},
					},
					core.Text{
						TestID:  "changes-checks-time",
						Content: timeText,
						Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
					},
				},
			},
			ui.Button{
				TestID:  "changes-run-checks",
				Text:    "Run checks",
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				Disabled: func() bool {
					return ed.ChangesBusy()
				},
				Color: func() core.Color { return specRaised },
				TextStyle: core.Style{
					FontSize: 11.5,
					Color:    specText,
				},
				Style: core.Style{
					Height:       core.Pt(24),
					CornerRadius: 6,
					Padding:      core.Symmetric(0, 10),
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				OnPress: func() {
					ed.report(ed.RunChangesChecksAsync(cx))
				},
			},
		},
	}

	diffRows := make([]core.Node, 0, len(f.Diff))
	for i, l := range f.Diff {
		var rowBg core.Color
		fg := specTextSecondary
		lineNo := ""
		switch l.Kind {
		case DiffLineAdd:
			rowBg = specDiffAddBg
			fg = specAdded
			if l.NewLine > 0 {
				lineNo = fmt.Sprintf("%d", l.NewLine)
			}
		case DiffLineDel:
			rowBg = specDiffDelBg
			fg = specProblem
			if l.OldLine > 0 {
				lineNo = fmt.Sprintf("%d", l.OldLine)
			}
		case DiffLineHunk:
			fg = specAccent
		case DiffLineMeta:
			fg = specTextMuted
		default:
			if l.NewLine > 0 {
				lineNo = fmt.Sprintf("%d", l.NewLine)
			} else if l.OldLine > 0 {
				lineNo = fmt.Sprintf("%d", l.OldLine)
			}
		}

		diffRows = append(diffRows, core.View{
			TestID: fmt.Sprintf("changes-diff-line-%d", i),
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Padding:    core.Symmetric(1, 16),
				Gap:        8,
				Background: rowBg,
			},
			Children: []core.Node{
				core.Text{
					Content: lineNo,
					Style: core.Style{
						Width:      core.Pt(36),
						FlexShrink: 0,
						FontFamily: "monospace",
						FontSize:   11.5,
						Color:      specTextMuted,
					},
				},
				core.Text{
					Content: l.Sign,
					Style: core.Style{
						Width:      core.Pt(14),
						FlexShrink: 0,
						FontFamily: "monospace",
						FontSize:   11.5,
						Color:      fg,
					},
				},
				core.Text{
					Content: l.Text,
					Style: core.Style{
						FlexGrow:   1,
						FlexBasis:  core.Pt(0),
						FontFamily: "monospace",
						FontSize:   11.5,
						Color:      fg,
					},
				},
			},
		})
	}
	if len(diffRows) == 0 {
		diffRows = append(diffRows, core.View{
			Style: core.Style{Padding: core.All(16)},
			Children: []core.Node{
				core.Text{
					Content: "No line-by-line differences to display.",
					Style:   core.Style{FontSize: 12, Color: specTextMuted},
				},
			},
		})
	}

	return core.View{
		TestID: "changes-text-diff",
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Background: specPanel,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			checksBar,
			ui.ScrollView{
				TestID:       "changes-diff-scroll",
				Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				ContentStyle: core.Style{Padding: core.Symmetric(12, 0)},
				EdgeColor:    func() core.Color { return specPanel },
				Children:     diffRows,
			},
		},
	}
}

func changesAudioCompareView(ed *Editor, f *ChangedFile) core.Node {
	info := f.Audio
	playing := ed.ChangesAuditioningPath() == f.Path
	filePath := f.Path

	metaParts := make([]string, 0, 4)
	if f.Status == ChangesStatusAdded || f.Status == ChangesStatusUntracked {
		metaParts = append(metaParts, "new file")
	} else {
		metaParts = append(metaParts, string(f.Status))
	}
	if info.Duration > 0 {
		metaParts = append(metaParts, fmt.Sprintf("%.1f s", info.Duration.Seconds()))
	}
	if info.SampleRate > 0 {
		metaParts = append(metaParts, fmt.Sprintf("%s %.1f kHz", info.Format, float64(info.SampleRate)/1000.0))
	} else if info.Format != "" {
		metaParts = append(metaParts, info.Format)
	}
	if info.SizeBytes > 0 {
		metaParts = append(metaParts, formatHumanBytes(info.SizeBytes))
	}

	playIcon := lucide.Play
	playLabel := "Play " + f.DisplayName
	if playing {
		playIcon = lucide.Square
		playLabel = "Stop " + f.DisplayName
	}

	refNodes := make([]core.Node, 0, max(1, len(info.UsedBy)))
	if len(info.UsedBy) == 0 {
		refNodes = append(refNodes, core.Text{
			TestID:  "changes-audio-refs",
			Content: "Not referenced by any scene or asset",
			Style:   core.Style{FontSize: 12, Color: specTextMuted},
		})
	} else {
		for i, u := range info.UsedBy {
			tid := fmt.Sprintf("changes-audio-ref-%d", i)
			if i == 0 {
				tid = "changes-audio-refs"
			}
			refNodes = append(refNodes, core.Text{
				TestID:  tid,
				Content: u,
				Style:   core.Style{FontSize: 12, Color: specTextSecondary},
			})
		}
	}

	bars := info.Waveform
	return core.View{
		TestID: "changes-audio-view",
		Style: core.Style{
			FlexGrow:       1,
			FlexBasis:      core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     specPanel,
			Padding:        core.All(24),
		},
		Children: []core.Node{
			core.View{
				TestID: "changes-audio-card",
				Style: core.Style{
					Width:        core.Pt(520),
					Padding:      core.All(16),
					Gap:          12,
					CornerRadius: 12,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: append([]core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
						Children: []core.Node{
							ui.Button{
								TestID:  "changes-audio-play",
								Icon:    playIcon,
								Label:   playLabel,
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return specTextStrong },
								Tint:    func() core.Color { return specBg },
								Style: core.Style{
									Width:          core.Pt(32),
									Height:         core.Pt(32),
									CornerRadius:   8,
									JustifyContent: core.JustifyCenter,
								},
								OnPress: func() {
									if ed.ChangesAuditioningPath() == filePath {
										ed.StopChangedAudio()
									} else {
										ed.report(ed.AuditionChangedAudio(filePath))
									}
								},
							},
							core.View{
								Style: core.Style{Gap: 2},
								Children: []core.Node{
									core.Text{
										Content: f.DisplayName,
										Style: core.Style{
											FontSize:   13,
											FontWeight: 500,
											Color:      specTextStrong,
										},
									},
									core.Text{
										TestID:  "changes-audio-meta",
										Content: strings.Join(metaParts, " · "),
										Style: core.Style{
											FontSize: 11.5,
											Color:    specTextMuted,
										},
									},
								},
							},
						},
					},
					core.Canvas{
						TestID: "changes-audio-waveform",
						Label:  "Audio waveform for " + f.DisplayName,
						Style:  core.Style{Width: core.Pct(100), Height: core.Pt(56)},
						Draw: func(d *core.Drawing, size core.Size) {
							drawAudioWaveform(d, size, bars, playing)
						},
					},
				}, refNodes...),
			},
		},
	}
}

func changesGenericAssetView(f *ChangedFile) core.Node {
	return core.View{
		Style: core.Style{
			FlexGrow:       1,
			FlexBasis:      core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     specPanel,
			Padding:        core.All(24),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:        core.Pt(420),
					Padding:      core.All(16),
					Gap:          6,
					CornerRadius: 12,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: f.DisplayName,
						Style:   core.Style{FontFamily: "monospace", FontSize: 12.5, FontWeight: 500, Color: specTextStrong},
					},
					core.Text{
						Content: f.Detail,
						Style:   core.Style{FontSize: 12, Color: specTextMuted},
					},
				},
			},
		},
	}
}

func changesCommitPaneView(cx *core.Scope, ed *Editor, changesRev *core.Memo[uint64], draftRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = changesRev.Get()
		sess := ed.ChangesSession()
		if sess == nil {
			return core.View{}
		}
		busy := ed.ChangesBusy()

		var bodyChildren []core.Node

		if !sess.Committed {
			inc, total, plus, minus := sess.IncludedSummary()
			canCommitNow := func() bool {
				_ = draftRev.Get()
				if ed.ChangesBusy() || inc == 0 {
					return false
				}
				s := ed.ChangesSession()
				return s != nil && strings.TrimSpace(s.DraftSummary) != ""
			}

			provenance := nonEmpty(sess.DraftProvenance, fmt.Sprintf("Drafted from %d changed files", inc))
			bodyChildren = append(bodyChildren,
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            8,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Height:       core.Pt(24),
								Padding:      core.Symmetric(0, 10),
								Gap:          6,
								CornerRadius: 12,
								Background:   specAgentTint,
								BorderWidth:  1,
								BorderColor:  specAgentEdge,
							},
							Children: []core.Node{
								core.Icon{
									SVG:   lucide.Sparkles(),
									Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: specAgent},
								},
								core.Text{
									TestID:  "changes-draft-provenance",
									Content: provenance,
									Style:   core.Style{FontSize: 11.5, Color: specText},
								},
							},
						},
						ui.Button{
							TestID:  "changes-redraft",
							Text:    "Redraft",
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							TextStyle: core.Style{
								FontSize: 12,
								Color:    specTextSecondary,
							},
							Style: core.Style{
								Height:       core.Pt(28),
								CornerRadius: 8,
								Padding:      core.Symmetric(0, 10),
							},
							OnPress: func() {
								ed.report(ed.RedraftCommitMessage())
							},
						},
					},
				},
				core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						core.Text{
							Content: "Summary",
							Style:   core.Style{FontSize: 12, Color: specTextSecondary},
						},
						ui.Input{
							TestID:      "changes-commit-summary",
							Label:       "Commit summary",
							Placeholder: "Describe what changed",
							Value: func() string {
								_ = draftRev.Get()
								if s := ed.ChangesSession(); s != nil {
									return s.DraftSummary
								}
								return ""
							},
							OnChange: func(v string) {
								ed.SetCommitSummary(v)
							},
							Style: core.Style{
								Width:        core.Pct(100),
								Height:       core.Pt(32),
								Background:   specRaised,
								BorderWidth:  1,
								BorderColor:  specLineRaised,
								CornerRadius: 8,
								Color:        specTextStrong,
								FontSize:     12,
							},
						},
					},
				},
				core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						core.Text{
							Content: "Description",
							Style:   core.Style{FontSize: 12, Color: specTextSecondary},
						},
						ui.Input{
							TestID:      "changes-commit-description",
							Label:       "Commit description",
							Multiline:   true,
							Placeholder: "Details, playtest notes, and context",
							Value: func() string {
								_ = draftRev.Get()
								if s := ed.ChangesSession(); s != nil {
									return s.DraftDescription
								}
								return ""
							},
							OnChange: func(v string) {
								ed.SetCommitDescription(v)
							},
							Style: core.Style{
								Width:        core.Pct(100),
								Height:       core.Pt(164),
								Background:   specRaised,
								BorderWidth:  1,
								BorderColor:  specLineRaised,
								CornerRadius: 8,
								Color:        specText,
								FontFamily:   "monospace",
								FontSize:     11.5,
							},
						},
					},
				},
			)

			if sess.DraftAttribution != "" {
				bodyChildren = append(bodyChildren, core.Text{
					TestID:  "changes-draft-attribution",
					Content: sess.DraftAttribution,
					Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
				})
			}

			includedLabel := fmt.Sprintf("%d of %d files included", inc, total)
			if total == 1 {
				includedLabel = fmt.Sprintf("%d of 1 file included", inc)
			}
			bodyChildren = append(bodyChildren, core.View{
				TestID: "changes-included-summary",
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
				},
				Children: []core.Node{
					core.Text{
						Content: includedLabel,
						Style:   core.Style{FontSize: 12, Color: specTextMuted},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 6},
						Children: []core.Node{
							core.Text{
								Content: fmt.Sprintf("+%d", plus),
								Style:   core.Style{FontFamily: "monospace", FontSize: 11, Color: specAdded},
							},
							core.Text{
								Content: fmt.Sprintf("-%d", minus),
								Style:   core.Style{FontFamily: "monospace", FontSize: 11, Color: specProblem},
							},
						},
					},
				},
			})

			commitText := "Commit"
			if inc == 0 {
				commitText = "Select files to commit"
			}
			bodyChildren = append(bodyChildren, core.View{
				Style: core.Style{Direction: core.Row, Gap: 8},
				Children: []core.Node{
					ui.Button{
						TestID:  "changes-commit",
						Text:    commitText,
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Disabled: func() bool {
							return !canCommitNow()
						},
						Color: func() core.Color {
							if canCommitNow() {
								return specAccent
							}
							return specLine
						},
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: 600,
							Color:      specBg, // Spec contrast table: #101216 on #3B7BFF (4.88:1 AA).
						},
						Style: core.Style{
							FlexGrow:       1,
							FlexBasis:      core.Pt(0),
							Height:         core.Pt(32),
							JustifyContent: core.JustifyCenter,
							CornerRadius:   8,
						},
						OnPress: func() {
							_, err := ed.CommitSelectedChanges()
							ed.report(err)
						},
					},
					ui.Button{
						TestID:  "changes-commit-push",
						Text:    "Commit and push",
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Disabled: func() bool {
							return !canCommitNow()
						},
						Color: func() core.Color { return specLineRaised },
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      specText,
						},
						Style: core.Style{
							Height:       core.Pt(32),
							CornerRadius: 8,
							Padding:      core.Symmetric(0, 12),
						},
						OnPress: func() {
							ed.report(ed.CommitAndPushSelectedChangesAsync(cx))
						},
					},
				},
			})

			if sess.Ahead > 0 {
				pushLabel := fmt.Sprintf("Push %d commits", sess.Ahead)
				if sess.Ahead == 1 {
					pushLabel = "Push 1 commit"
				}
				bodyChildren = append(bodyChildren, ui.Button{
					TestID:  "changes-push",
					Text:    pushLabel,
					Icon:    lucide.Upload,
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Disabled: func() bool {
						return busy
					},
					Color: func() core.Color { return specRaised },
					Tint:  func() core.Color { return specAccent },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      specTextStrong,
					},
					Style: core.Style{
						AlignSelf:    core.AlignStart,
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 12),
						BorderWidth:  1,
						BorderColor:  specAccentEdge,
					},
					OnPress: func() {
						ed.report(ed.PushCommitsAsync(cx))
					},
				})
			}

			bodyChildren = append(bodyChildren, changesShareSection(cx, ed, sess))
		} else {
			// Post-commit confirmation card, SHARE, and HISTORY matching VersionControl.dc.html.
			sha := sess.HeadSHA
			summary := sess.DraftSummary
			pushed := false
			if sess.LastCommit != nil {
				sha = sess.LastCommit.SHA
				summary = sess.LastCommit.Summary
				pushed = sess.LastCommit.Pushed
			}
			cardChildren := []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:          core.Pt(18),
								Height:         core.Pt(18),
								CornerRadius:   9,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Background:     specAddedFill,
							},
							Children: []core.Node{
								core.Icon{
									SVG:   lucide.Check(),
									Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: specAdded},
								},
							},
						},
						core.Text{
							TestID:  "changes-committed-title",
							Content: "Committed as " + sha,
							Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: specTextStrong},
						},
					},
				},
				core.Text{
					Content: summary,
					Style:   core.Style{FontSize: 12.5, Color: specText},
				},
				core.Text{
					Content: "A milestone in History. Restore to it any time, or compare any later step with it.",
					Style:   core.Style{FontSize: 12, Color: specTextMuted},
				},
			}
			if !pushed {
				ahead := max(1, sess.Ahead)
				pushLabel := fmt.Sprintf("Push %d commits", ahead)
				if ahead == 1 {
					pushLabel = "Push 1 commit"
				}
				cardChildren = append(cardChildren, ui.Button{
					TestID:  "changes-push",
					Text:    pushLabel,
					Icon:    lucide.Upload,
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Disabled: func() bool {
						return busy
					},
					Color: func() core.Color { return specAccent },
					Tint:  func() core.Color { return specBg },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 600,
						Color:      specBg,
					},
					Style: core.Style{
						AlignSelf:    core.AlignStart,
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 12),
					},
					OnPress: func() {
						ed.report(ed.PushCommitsAsync(cx))
					},
				})
			}

			bodyChildren = append(bodyChildren,
				core.View{
					TestID: "changes-committed-card",
					Style: core.Style{
						Padding:      core.All(14),
						Gap:          8,
						CornerRadius: 12,
						Background:   specRaised,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
					},
					Children: cardChildren,
				},
				changesShareSection(cx, ed, sess),
				changesHistorySection(ed, sess),
				ui.Button{
					TestID:  "changes-back-to-draft",
					Text:    "Back to the draft",
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					TextStyle: core.Style{
						FontSize: 12,
						Color:    specTextSecondary,
					},
					Style: core.Style{
						AlignSelf:    core.AlignStart,
						Height:       core.Pt(28),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 10),
					},
					OnPress: func() {
						ed.BackToCommitDraft()
					},
				},
			)
		}

		if busyLabel := ed.ChangesBusyLabel(); busyLabel != "" {
			bodyChildren = append(bodyChildren, core.View{
				TestID: "changes-busy",
				Style: core.Style{
					Padding:      core.All(10),
					CornerRadius: 8,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specAccentEdge,
				},
				Children: []core.Node{
					core.Text{
						Content: busyLabel,
						Style:   core.Style{FontSize: 11.5, Color: specText},
					},
				},
			})
		} else if sess.LastErr != "" {
			bodyChildren = append(bodyChildren, core.View{
				TestID: "changes-error",
				Style: core.Style{
					Padding:      core.All(10),
					CornerRadius: 8,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specProblemEdge,
				},
				Children: []core.Node{
					core.Text{
						Content: sess.LastErr,
						Style:   core.Style{FontSize: 11.5, Color: specProblem},
					},
				},
			})
		} else if sess.LastNotice != "" {
			bodyChildren = append(bodyChildren, core.View{
				TestID: "changes-notice",
				Style: core.Style{
					Padding:      core.All(10),
					CornerRadius: 8,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specAddedEdge,
				},
				Children: []core.Node{
					core.Text{
						Content: sess.LastNotice,
						Style:   core.Style{FontSize: 11.5, Color: specAdded},
					},
				},
			})
		}

		return core.View{
			TestID: "changes-commit-pane",
			Style: core.Style{
				Width:       core.Pt(400),
				FlexShrink:  0,
				Height:      core.Pct(100),
				Background:  specPanel,
				Borders:     core.Edges{Left: 1},
				BorderColor: specLine,
				Overflow:    core.Hidden,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Height:         core.Pt(44),
						FlexShrink:     0,
						Padding:        core.Symmetric(0, 16),
						Borders:        core.Edges{Bottom: 1},
						BorderColor:    specLine,
					},
					Children: []core.Node{
						core.Text{
							Content: "Commit",
							Style:   core.Style{FontSize: 14, FontWeight: 600, Color: specTextStrong},
						},
						core.Text{
							Content: sess.Branch,
							Style:   core.Style{FontFamily: "monospace", FontSize: 11, Color: specTextMuted},
						},
					},
				},
				ui.ScrollView{
					TestID:       "changes-commit-scroll",
					Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
					ContentStyle: core.Style{Padding: core.All(16), Gap: 12},
					EdgeColor:    func() core.Color { return specPanel },
					Children:     bodyChildren,
				},
			},
		}
	})
}

func changesShareSection(cx *core.Scope, ed *Editor, sess *ChangesSession) core.Node {
	ghAvailable := ed.ChangesGHAvailable()
	busy := ed.ChangesBusy()
	base := nonEmpty(sess.BaseBranch, "main")
	openSubtitle := fmt.Sprintf("%s into %s with gh, using the same description", sess.Branch, base)
	if !ghAvailable {
		openSubtitle = "Install GitHub CLI (gh) to open pull requests directly"
	}

	shareCard := func(testID, title, subtitle string, icon func() *core.SVG, disabled bool, onPress func()) core.Node {
		fg := specTextStrong
		subFg := specTextMuted
		if disabled {
			fg = specTextMuted
		}
		return core.View{
			TestID:   testID,
			Role:     core.RoleButton,
			Label:    title,
			Disabled: func() bool { return disabled },
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignStart,
				Padding:      core.Symmetric(10, 12),
				Gap:          10,
				CornerRadius: 8,
				Background:   specRaised,
				BorderWidth:  1,
				BorderColor:  specLineRaised,
				FocusVisible: specFocusRingStyle,
			},
			OnPress: func(core.PressEvent) {
				if !disabled {
					onPress()
				}
			},
			Children: []core.Node{
				core.Icon{
					SVG:   icon(),
					Style: core.Style{Width: core.Pt(15), Height: core.Pt(15), Color: specTextSecondary},
				},
				core.View{
					Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
					Children: []core.Node{
						core.Text{
							Content: title,
							Style:   core.Style{FontSize: 12.5, Color: fg},
						},
						core.Text{
							Content: subtitle,
							Style:   core.Style{FontSize: 11.5, Color: subFg},
						},
					},
				},
			},
		}
	}

	return core.View{
		TestID: "changes-share-section",
		Style:  core.Style{Gap: 6},
		Children: []core.Node{
			core.Text{
				Content: "SHARE",
				Style: core.Style{
					FontSize:      11,
					FontWeight:    600,
					LetterSpacing: 0.7,
					Color:         specTextMuted,
					Padding:       core.Edges{Left: 4, Bottom: 2},
				},
			},
			shareCard(
				"changes-copy-pr",
				"Copy as PR description",
				"Prompts, the plan, before and after images, playtest numbers",
				lucide.Copy,
				false,
				func() {
					_, err := ed.CopyPRDescription()
					ed.report(err)
				},
			),
			shareCard(
				"changes-open-pr",
				"Open pull request",
				openSubtitle,
				lucide.GitPullRequest,
				!ghAvailable || busy,
				func() {
					ed.report(ed.OpenPullRequestAsync(cx))
				},
			),
		},
	}
}

func changesHistorySection(ed *Editor, sess *ChangesSession) core.Node {
	now := ed.now()
	rows := []core.Node{
		core.Text{
			Content: "HISTORY",
			Style: core.Style{
				FontSize:      11,
				FontWeight:    600,
				LetterSpacing: 0.7,
				Color:         specTextMuted,
				Padding:       core.Edges{Left: 4, Bottom: 2},
			},
		},
	}

	for i, m := range sess.Milestones {
		if i >= 5 {
			break
		}
		var bg core.Color
		fg := specTextSecondary
		if i == 0 {
			bg = specAccentTint
			fg = specTextStrong
		}
		rows = append(rows, core.View{
			TestID: fmt.Sprintf("changes-milestone-%d", i),
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Height:       core.Pt(28),
				Padding:      core.Symmetric(0, 8),
				Gap:          8,
				CornerRadius: 6,
				Background:   bg,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(8),
						Height:       core.Pt(8),
						FlexShrink:   0,
						CornerRadius: 2,
						Background:   specAccent,
					},
				},
				core.Text{
					Content: "Commit " + m.SHA,
					Style:   core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), FontSize: 12, Color: fg},
				},
				core.Text{
					Content: formatCheckRelativeTime(m.Timestamp, now),
					Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
				},
			},
		})
	}

	return core.View{
		TestID:   "changes-history-list",
		Style:    core.Style{Gap: 2},
		Children: rows,
	}
}

// --- Canvas Rendering Helpers (strictly Spec.dc.html tokens) ---

func drawChangesScenePreview(
	d *core.Drawing,
	size core.Size,
	sc *SceneComparison,
	doc *sceneformat.SceneDoc,
	built *sceneformat.BuiltScene,
	isHead bool,
) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 || sc == nil {
		return
	}

	// Dot-grid viewport surround using only Spec tokens.
	d.SetFillColor(specBg)
	d.BeginPath().Rect(0, 0, w, h).Fill()
	d.SetFillColor(specLineRaised)
	const gridStep float32 = 16
	for gx := gridStep; gx < w; gx += gridStep {
		for gy := gridStep; gy < h; gy += gridStep {
			d.BeginPath().Rect(gx, gy, 1.2, 1.2).Fill()
		}
	}

	vw, vh := float32(640), float32(360)
	if doc != nil && doc.VirtualWidth > 0 && doc.VirtualHeight > 0 {
		vw, vh = doc.VirtualWidth, doc.VirtualHeight
	} else if sc.NowDoc != nil && sc.NowDoc.VirtualWidth > 0 && sc.NowDoc.VirtualHeight > 0 {
		vw, vh = sc.NowDoc.VirtualWidth, sc.NowDoc.VirtualHeight
	} else if sc.HeadDoc != nil && sc.HeadDoc.VirtualWidth > 0 && sc.HeadDoc.VirtualHeight > 0 {
		vw, vh = sc.HeadDoc.VirtualWidth, sc.HeadDoc.VirtualHeight
	}

	const pad float32 = 14
	availW := max(w-pad*2, 1)
	availH := max(h-pad*2, 1)
	scale := min(availW/vw, availH/vh)
	frameW := vw * scale
	frameH := vh * scale
	ox := (w - frameW) / 2
	oy := (h - frameH) / 2

	d.Save()
	d.SetFillColor(specPanel)
	d.BeginPath().RoundRect(ox, oy, frameW, frameH, 4).Fill()

	d.Save()
	d.BeginPath().Rect(ox, oy, frameW, frameH).Clip()
	d.Translate(ox, oy)
	d.Scale(scale, scale)

	if built != nil {
		built.SyncMaterials()
		for _, layer := range built.Layers {
			if !layer.Visible {
				continue
			}
			drawBuiltLayer(d, layer)
		}
	}

	addedIDs := make(map[string]bool, len(sc.Added))
	for _, o := range sc.Added {
		addedIDs[o.ID] = true
	}
	changedIDs := make(map[string]bool, len(sc.Changed))
	for _, o := range sc.Changed {
		changedIDs[o.ID] = true
	}
	removedIDs := make(map[string]bool, len(sc.Removed))
	for _, o := range sc.Removed {
		removedIDs[o.ID] = true
	}

	if doc != nil {
		for _, layer := range doc.Layers {
			if !layer.IsVisible() {
				continue
			}
			var walk func(objs []sceneformat.ObjectDoc, px, py float32)
			walk = func(objs []sceneformat.ObjectDoc, px, py float32) {
				for i := range objs {
					o := &objs[i]
					wx, wy := px+o.X, py+o.Y
					hasSprite := built != nil && built.Objects[o.ID] != nil && built.Objects[o.ID].Sprite != nil
					if !hasSprite {
						drawMergeObjectBlock(d, o, wx, wy, false)
					}
					rx, ry, rw, rh := mergeObjectRect(o, wx, wy)
					if isHead {
						if removedIDs[o.ID] {
							drawDashedRect(d, rx-2, ry-2, rw+4, rh+4, specProblem, 1.5/scale)
						}
					} else {
						if addedIDs[o.ID] {
							drawDashedRect(d, rx-2, ry-2, rw+4, rh+4, specAdded, 1.5/scale)
						} else if changedIDs[o.ID] {
							drawDashedRect(d, rx-2, ry-2, rw+4, rh+4, specChanged, 1.5/scale)
						}
					}
					walk(o.Children, wx, wy)
				}
			}
			walk(layer.Objects, 0, 0)
		}
	}

	d.Restore()

	d.SetStrokeColor(specLineRaised).SetLineWidth(1)
	d.BeginPath().RoundRect(ox, oy, frameW, frameH, 4).Stroke()
	d.Restore()
}

func drawAudioWaveform(d *core.Drawing, size core.Size, bars []float32, playing bool) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 || len(bars) == 0 {
		return
	}
	n := len(bars)
	const gap float32 = 2
	barW := (w - gap*float32(n-1)) / float32(n)
	if barW < 1 {
		barW = 1
	}
	activeCount := n / 3
	if playing {
		activeCount = (n * 2) / 3
	}
	for i, v := range bars {
		bh := max(float32(6), v*(h-4))
		bx := float32(i) * (barW + gap)
		by := (h - bh) / 2
		if i < activeCount {
			d.SetFillColor(specTextSecondary)
		} else {
			d.SetFillColor(specLineRaised)
		}
		d.BeginPath().RoundRect(bx, by, barW, bh, 2).Fill()
	}
}
