package editor

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived alpha tints for ReferencesScreenView from spec_theme.yaml.
var (
	specRefRowActiveBg = specAlpha(darkSpecPalette.Accent.Fg, 0.16)
	specRefHitBg       = specAlpha(darkSpecPalette.Accent.Fg, 0.18)
	specRefHitDimBg    = specAlpha(darkSpecPalette.Accent.Fg, 0.08)
	specRefDelBg       = specAlpha(darkSpecPalette.Removed.Fg, 0.10)
	specRefAddBg       = specAlpha(darkSpecPalette.Added.Fg, 0.10)
	specRefMonoFamily  = "JetBrains Mono, Menlo, monospace"
)

// ReferencesScreenView renders the full-window Find Usages and Safe Rename review screen
// matching docs/design/editor/References.dc.html using only Spec.dc.html tokens.
func ReferencesScreenView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	keyOpt := core.KeyOptions{When: core.KeyNotEditing}
	cx.OnKeyWith("escape", keyOpt, func() {
		if !ed.UsagesScreenOpen() {
			return
		}
		if st := ed.ReferencesScreen(); st != nil && st.Mode == ReferencesModeRename {
			ed.CancelSafeRename()
			return
		}
		ed.CloseUsagesScreen()
	})
	cx.OnKeyWith("up", keyOpt, func() {
		if ed.UsagesScreenOpen() {
			ed.StepUsageFocus(-1)
		}
	})
	cx.OnKeyWith("down", keyOpt, func() {
		if ed.UsagesScreenOpen() {
			ed.StepUsageFocus(1)
		}
	})
	cx.OnKeyWith("enter", keyOpt, func() {
		if ed.UsagesScreenOpen() {
			ed.report(ed.OpenFocusedUsage())
		}
	})
	cx.OnKeyWith("f2", keyOpt, func() {
		if st := ed.ReferencesScreen(); st != nil && st.Mode == ReferencesModeUsages {
			ed.report(ed.StartSafeRename())
		}
	})
	cx.OnKeyWith("mod+enter", core.KeyOptions{When: core.KeyAlways}, func() {
		if st := ed.ReferencesScreen(); st != nil && st.Mode == ReferencesModeRename && st.Plan != nil && st.Plan.Valid {
			ed.report(ed.ApplySafeRename())
		}
	})
	cx.OnKeyWith("mod+z", keyOpt, func() {
		if st := ed.ReferencesScreen(); st != nil && st.Mode == ReferencesModeDone {
			ed.UndoSafeRename()
		}
	})

	refsRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.usagesStateRev()
	})

	return core.View{
		TestID: "references-screen",
		Role:   core.RoleRegion,
		Label:  "Find usages and safe rename",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			referencesHeaderView(cx, ed, refsRev),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					referencesSidebarView(cx, ed, refsRev),
					referencesCenterView(cx, ed, refsRev),
					referencesCodeView(cx, ed, refsRev),
				},
			},
		},
	}
}

func referencesHeaderView(cx *core.Scope, ed *Editor, refsRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = refsRev.Get()
		st := ed.ReferencesScreen()
		if st == nil {
			return core.View{}
		}
		base := path.Base(st.Target)
		var title, subtitle string
		switch st.Mode {
		case ReferencesModeRename:
			title = "Rename " + base
			subtitle = "every reference is rewritten in one review and one undo step"
		case ReferencesModeDone:
			title = "Renamed to " + base
			if len(st.CheckProblems) > 0 {
				p0 := st.CheckProblems[0]
				if len(st.CheckProblems) == 1 {
					subtitle = fmt.Sprintf("%d files rewritten · game check: 1 problem in %s", st.AppliedFiles, p0.File)
				} else {
					subtitle = fmt.Sprintf("%d files rewritten · game check: %d problems", st.AppliedFiles, len(st.CheckProblems))
				}
			} else {
				subtitle = fmt.Sprintf("%d files rewritten · game check: clean", st.AppliedFiles)
			}
		default:
			title = "Usages of " + base
			totalU, totalF := 0, 0
			if st.Report != nil {
				totalU = st.Report.TotalUsages
				totalF = st.Report.TotalFiles
			}
			subtitle = fmt.Sprintf("%d %s in %d %s", totalU, pluralWord(totalU, "usage", "usages"), totalF, pluralWord(totalF, "file", "files"))
			if st.SourceObjectID != "" {
				subtitle += " · Find usages on " + st.SourceObjectID
			}
		}

		leftNodes := []core.Node{
			core.View{
				TestID: "refs-back-to-editor",
				Role:   core.RoleButton,
				Label:  "Back to the editor (Esc)",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(28),
					Padding:      core.Symmetric(0, 8),
					Gap:          8,
					CornerRadius: 6,
				},
				OnPress: func(core.PressEvent) {
					ed.CloseUsagesScreen()
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.ArrowLeft(),
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: specTextSecondary},
					},
					core.Text{
						Content: "Editor",
						Style:   core.Style{FontSize: 12.5, Color: specTextSecondary},
					},
					refsKeyCap("Esc"),
				},
			},
			core.View{
				Style: core.Style{
					Width:      core.Pt(1),
					Height:     core.Pt(20),
					Background: specLine,
				},
			},
		}

		if st.Mode == ReferencesModeDone {
			leftNodes = append(leftNodes, core.View{
				Style: core.Style{
					Width:          core.Pt(18),
					Height:         core.Pt(18),
					CornerRadius:   9,
					Background:     specAddedFill,
					BorderWidth:    1,
					BorderColor:    specAddedEdge,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.Check(),
						Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: specAdded},
					},
				},
			})
		}

		leftNodes = append(leftNodes,
			core.Text{
				TestID:  "refs-title",
				Content: title,
				Style:   core.Style{FontSize: 14, FontWeight: 600, Color: specTextStrong},
			},
			core.Text{
				TestID:  "refs-subtitle",
				Content: subtitle,
				Style:   core.Style{FontSize: 12, Color: specTextMuted},
			},
		)

		var rightNodes []core.Node
		switch st.Mode {
		case ReferencesModeUsages:
			rightNodes = append(rightNodes, core.View{
				TestID: "refs-start-rename",
				Role:   core.RoleButton,
				Label:  "Rename · F2",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(30),
					Padding:      core.Symmetric(0, 12),
					Gap:          8,
					CornerRadius: 8,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.StartSafeRename())
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.Pencil(),
						Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: specTextStrong},
					},
					core.Text{
						Content: "Rename…",
						Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: specTextStrong},
					},
					core.Text{
						Content: "F2",
						Style:   core.Style{FontSize: 11, FontFamily: specRefMonoFamily, Color: specTextMuted},
					},
				},
			})
		case ReferencesModeRename:
			valid := st.Plan != nil && st.Plan.Valid
			fileCount := 1
			if st.Plan != nil && st.Plan.TotalTouchedFiles > 0 {
				fileCount = st.Plan.TotalTouchedFiles
			}
			applyLabel := fmt.Sprintf("Keep · Rename %d %s", fileCount, pluralWord(fileCount, "file", "files"))
			applyBg := specLine
			applyFg := specTextMuted
			if valid {
				applyBg = specAccent
				applyFg = specBg // Spec contrast table: #101216 on #3B7BFF (4.88:1 AA).
			}
			rightNodes = append(rightNodes,
				ui.Button{
					TestID:  "refs-cancel-rename",
					Text:    "Cancel",
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return specRaised },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      specText,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 14),
						BorderWidth:  1,
						BorderColor:  specLineRaised,
					},
					OnPress: func() {
						ed.CancelSafeRename()
					},
				},
				ui.Button{
					TestID:   "refs-apply-rename",
					Text:     applyLabel + " ⌘↵",
					Variant:  ui.ButtonGhost,
					Size:     ui.SizeSmall,
					Disabled: func() bool { return !valid },
					Color:    func() core.Color { return applyBg },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 600,
						Color:      applyFg,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 14),
					},
					OnPress: func() {
						if valid {
							ed.report(ed.ApplySafeRename())
						}
					},
				},
			)
		case ReferencesModeDone:
			backLabel := "Back to " + st.ReturnSceneName
			rightNodes = append(rightNodes,
				ui.Button{
					TestID:  "refs-undo-rename",
					Text:    "Undo · ⌘Z",
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return specRaised },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      specText,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 14),
						BorderWidth:  1,
						BorderColor:  specLineRaised,
					},
					OnPress: func() {
						ed.UndoSafeRename()
					},
				},
				ui.Button{
					TestID:  "refs-back-to-scene",
					Text:    backLabel,
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return specTextStrong },
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
						ed.CloseUsagesScreen()
					},
				},
			)
		}

		return core.View{
			TestID: "refs-header",
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
						Gap:        10,
						FlexGrow:   1,
						MinWidth:   core.Pt(0),
					},
					Children: leftNodes,
				},
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        8,
						FlexShrink: 0,
					},
					Children: rightNodes,
				},
			},
		}
	})
}

func referencesSidebarView(cx *core.Scope, ed *Editor, refsRev *core.Memo[uint64]) core.Node {
	inRename := core.NewMemo(cx, func() bool {
		_ = refsRev.Get()
		return ed.usages.open && ed.usages.mode == ReferencesModeRename
	})
	renameFolder := core.NewMemo(cx, func() string {
		_ = refsRev.Get()
		if !ed.usages.open {
			return ""
		}
		return ed.usages.folderPrefix
	})
	renameSuffix := core.NewMemo(cx, func() string {
		_ = refsRev.Get()
		if !ed.usages.open {
			return ""
		}
		return ed.usages.suffix
	})
	renameInput := core.NewMemo(cx, func() string {
		_ = refsRev.Get()
		if !ed.usages.open {
			return ""
		}
		return ed.usages.renameInput
	})

	renameBox := core.Show{
		When: inRename.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "refs-new-name-box",
				Style: core.Style{
					Padding:     core.All(12),
					Gap:         8,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: specLine,
					FlexShrink:  0,
				},
				Children: []core.Node{
					core.Text{
						Content: "NEW NAME",
						Style:   core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
					},
					core.View{
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Padding:      core.Symmetric(2, 8),
							Gap:          2,
							CornerRadius: 8,
							Background:   specRaised,
							BorderWidth:  1.5,
							BorderColor:  specAccent,
						},
						Children: []core.Node{
							core.Show{
								When: func() bool { return renameFolder.Get() != "" },
								Then: func(cx *core.Scope) core.Node {
									return core.Dynamic(func(*core.Scope) core.Node {
										return core.Text{
											Content: renameFolder.Get(),
											Style:   core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: specTextMuted, FlexShrink: 0},
										}
									})
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(40)},
								Children: []core.Node{
									ui.Input{
										TestID: "refs-new-name-input",
										Label:  "New file name",
										Value:  renameInput.Get,
										OnChange: func(v string) {
											_ = ed.SetSafeRenameInput(v)
										},
										OnSubmit: func(string) {
											if cur := ed.ReferencesScreen(); cur != nil && cur.Plan != nil && cur.Plan.Valid {
												ed.report(ed.ApplySafeRename())
											}
										},
										Style: core.Style{
											Width:       core.Pct(100),
											Height:      core.Pt(26),
											Background:  specRaised,
											BorderWidth: 0,
											Color:       specTextStrong,
											FontFamily:  specRefMonoFamily,
											FontSize:    11.5,
											Padding:     core.Symmetric(0, 4),
										},
									},
								},
							},
							core.Show{
								When: func() bool { return renameSuffix.Get() != "" },
								Then: func(cx *core.Scope) core.Node {
									return core.Dynamic(func(*core.Scope) core.Node {
										return core.Text{
											Content: renameSuffix.Get(),
											Style:   core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: specTextMuted, FlexShrink: 0},
										}
									})
								},
							},
						},
					},
					core.Dynamic(func(*core.Scope) core.Node {
						_ = refsRev.Get()
						st := ed.ReferencesScreen()
						valid := st != nil && st.Plan != nil && st.Plan.Valid
						valColor := specAdded
						valMsg := "Enter a new name"
						if st != nil && st.Plan != nil && st.Plan.ValidationMsg != "" {
							valMsg = st.Plan.ValidationMsg
						}
						var valBadge core.Node = core.Icon{
							SVG:   lucide.Check(),
							Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: valColor},
						}
						if !valid {
							valColor = specWarning
							valBadge = core.Text{
								Content: "!",
								Style:   core.Style{FontSize: 12, FontWeight: 700, Color: valColor},
							}
						}
						return core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        6,
							},
							Children: []core.Node{
								valBadge,
								core.Text{
									TestID:  "refs-validation-msg",
									Content: valMsg,
									Style:   core.Style{FontSize: 11.5, Color: specTextSecondary},
								},
							},
						}
					}),
				},
			}
		},
	}

	rowsDynamic := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = refsRev.Get()
		st := ed.ReferencesScreen()
		if st == nil {
			return core.View{}
		}

		// Grouped usage list rows.
		var rows []core.Node
		addGroupHeader := func(label, meta string) {
			rows = append(rows, core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Height:         core.Pt(28),
					Padding:        core.Edges{Top: 8, Left: 8, Right: 8},
				},
				Children: []core.Node{
					core.Text{Content: label, Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted}},
					core.Text{Content: meta, Style: core.Style{FontSize: 11.5, Color: specTextMuted}},
				},
			})
		}

		if st.Mode == ReferencesModeRename && st.Plan != nil && st.Plan.Valid {
			addGroupHeader("MOVED", "")
			isOpen := st.FocusedFile == movedFocusSentinel
			var rowBg core.Color
			rowText := specText
			if isOpen {
				rowBg = specRefRowActiveBg
				rowText = specTextStrong
			}
			rows = append(rows, core.View{
				TestID: "refs-row-moved",
				Role:   core.RoleButton,
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(28),
					Padding:      core.Symmetric(0, 8),
					Gap:          8,
					CornerRadius: 6,
					Background:   rowBg,
				},
				OnPress: func(core.PressEvent) {
					ed.FocusUsage(movedFocusSentinel, 0)
				},
				Children: []core.Node{
					core.View{Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 2, Background: specAgent, FlexShrink: 0}},
					core.Text{
						Content:  st.Plan.From + " → " + st.Plan.NewStem,
						MaxLines: 1,
						Style:    core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: rowText, FlexGrow: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Overflow: core.Hidden},
					},
					core.Text{
						Content: "moved",
						Style:   core.Style{FontSize: 11, Color: specTextMuted, FlexShrink: 0},
					},
				},
			})
		}

		addFileRows := func(files []UsageFileGroup) {
			for _, fg := range files {
				filePath := fg.File
				open := st.FocusedFile == filePath
				dotColor := specTextMuted
				switch fg.Group {
				case UsageGroupScenes:
					dotColor = specAccent
				case UsageGroupVariants:
					dotColor = specAgent
				case UsageGroupGoCode:
					dotColor = specWarning
				}

				var rowBg core.Color
				rowText := specText
				if open {
					rowBg = specRefRowActiveBg
					rowText = specTextStrong
				}

				var rightBadge core.Node
				switch {
				case fg.NotRewritten:
					rightBadge = core.View{
						Style: core.Style{
							Height:       core.Pt(20),
							Padding:      core.Symmetric(0, 6),
							CornerRadius: 6,
							Background:   specWarningFill,
							BorderWidth:  1,
							BorderColor:  specWarningEdge,
							AlignItems:   core.AlignCenter,
							Direction:    core.Row,
							FlexShrink:   0,
						},
						Children: []core.Node{
							core.Text{Content: "not rewritten", Style: core.Style{FontSize: 10.5, Color: specWarning}},
						},
					}
				case st.Mode == ReferencesModeRename && st.Plan != nil && st.Plan.Valid:
					n := len(fg.Refs)
					if fd := st.Plan.FileDiff(filePath); fd != nil {
						n = fd.ChangeCount
					}
					rightBadge = core.View{
						Style: core.Style{Direction: core.Row, Gap: 4, FlexShrink: 0},
						Children: []core.Node{
							core.Text{Content: fmt.Sprintf("−%d", n), Style: core.Style{FontSize: 11, FontFamily: specRefMonoFamily, Color: specProblem}},
							core.Text{Content: fmt.Sprintf("+%d", n), Style: core.Style{FontSize: 11, FontFamily: specRefMonoFamily, Color: specAdded}},
						},
					}
				default:
					rightBadge = core.Text{
						Content: strconv.Itoa(len(fg.Refs)),
						Style:   core.Style{FontSize: 11.5, Color: specTextMuted, FlexShrink: 0},
					}
				}

				chevIcon := lucide.ChevronRight()
				if open {
					chevIcon = lucide.ChevronDown()
				}
				rows = append(rows, core.View{
					TestID: "refs-file-" + filePath,
					Role:   core.RoleButton,
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Height:       core.Pt(28),
						Padding:      core.Symmetric(0, 8),
						Gap:          8,
						CornerRadius: 6,
						Background:   rowBg,
					},
					OnPress: func(core.PressEvent) {
						ed.FocusUsage(filePath, 0)
					},
					Children: []core.Node{
						core.Icon{SVG: chevIcon, Style: core.Style{Width: core.Pt(11), Height: core.Pt(11), Color: specTextMuted, FlexShrink: 0}},
						core.View{Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 2, Background: dotColor, FlexShrink: 0}},
						core.Text{
							Content:  filePath,
							MaxLines: 1,
							Style:    core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: rowText, FlexGrow: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Overflow: core.Hidden},
						},
						rightBadge,
					},
				})

				if open {
					for i, item := range fg.Refs {
						idx := i
						uItem := item
						active := idx == st.FocusedRefIdx
						var itemBg core.Color
						itemFg := specTextSecondary
						var leftBorder core.Edges
						if active {
							itemBg = specRaised
							itemFg = specTextStrong
							leftBorder = core.Edges{Left: 2}
						}
						noteText := ""
						noteColor := specTextMuted
						if uItem.OverrideCount > 0 {
							noteText = fmt.Sprintf("%d %s", uItem.OverrideCount, pluralWord(uItem.OverrideCount, "override", "overrides"))
							noteColor = specChanged
						} else if uItem.HasPos {
							noteText = fmt.Sprintf("%.0f, %.0f", uItem.X, uItem.Y)
						}
						labelFont := ""
						if fg.Group == UsageGroupGoCode || fg.Group == UsageGroupVariants {
							labelFont = specRefMonoFamily
						}
						rows = append(rows, core.View{
							TestID: fmt.Sprintf("refs-item-%s-%d", filePath, idx),
							Role:   core.RoleButton,
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Height:       core.Pt(26),
								Padding:      core.Edges{Left: 18, Right: 8},
								Gap:          8,
								CornerRadius: 6,
								Background:   itemBg,
								Borders:      leftBorder,
								BorderColor:  specAccent,
							},
							OnPress: func(ev core.PressEvent) {
								ed.FocusUsage(filePath, idx)
								if ev.Clicks == 2 {
									ed.report(ed.OpenUsageItem(uItem))
								}
							},
							Children: []core.Node{
								core.Text{
									Content: strconv.Itoa(uItem.Line),
									Style:   core.Style{Width: core.Pt(30), FontSize: 11, FontFamily: specRefMonoFamily, Color: specTextMuted, FlexShrink: 0},
								},
								core.Text{
									Content:  uItem.Label,
									MaxLines: 1,
									Style:    core.Style{FontSize: 11.5, FontFamily: labelFont, Color: itemFg, FlexGrow: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Overflow: core.Hidden},
								},
								core.Text{
									Content: noteText,
									Style:   core.Style{FontSize: 11, Color: noteColor, FlexShrink: 0},
								},
							},
						})
					}
				}
			}
		}

		if st.Report != nil {
			sceneRefs := countGroupRefs(st.Report.Scenes)
			addGroupHeader("SCENES", fmt.Sprintf("%d %s", sceneRefs, pluralWord(sceneRefs, "instance", "instances")))
			addFileRows(st.Report.Scenes)

			variantCount := len(st.Report.Variants)
			addGroupHeader("PREFAB VARIANTS", fmt.Sprintf("%d inherit it", variantCount))
			addFileRows(st.Report.Variants)

			dataRows := countGroupRefs(st.Report.DataTables)
			addGroupHeader("DATA TABLES", fmt.Sprintf("%d %s", dataRows, pluralWord(dataRows, "row", "rows")))
			addFileRows(st.Report.DataTables)

			goRefs := countGroupRefs(st.Report.GoCode)
			addGroupHeader("GO CODE", fmt.Sprintf("%d %s", goRefs, pluralWord(goRefs, "constant", "constants")))
			addFileRows(st.Report.GoCode)

			if len(st.Report.Other) > 0 {
				otherRefs := countGroupRefs(st.Report.Other)
				addGroupHeader("OTHER", fmt.Sprintf("%d %s", otherRefs, pluralWord(otherRefs, "ref", "refs")))
				addFileRows(st.Report.Other)
			}
		}

		return core.View{Style: core.Style{Gap: 2}, Children: rows}
	})

	return core.View{
		TestID: "refs-sidebar",
		Style: core.Style{
			Width:       core.Pt(360),
			FlexShrink:  0,
			Background:  specBg,
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			renameBox,
			ui.ScrollView{
				Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Padding: core.All(8)},
				Children: []core.Node{rowsDynamic},
			},
			core.View{
				Style: core.Style{
					Padding:     core.Symmetric(10, 14),
					Borders:     core.Edges{Top: 1},
					BorderColor: specLine,
					FlexShrink:  0,
				},
				Children: []core.Node{
					core.Text{
						Content: "↑↓ moves through usages · ↵ opens the scene with the object framed",
						Style:   core.Style{FontSize: 11, Color: specTextMuted},
					},
				},
			},
		},
	}
}

func referencesCenterView(cx *core.Scope, ed *Editor, refsRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = refsRev.Get()
		st := ed.ReferencesScreen()
		if st == nil {
			return core.View{}
		}

		var sections []core.Node

		// Rename plan summary card during Rename review mode.
		if st.Mode == ReferencesModeRename && st.Plan != nil {
			sections = append(sections, referencesRenamePlanCard(st.Plan))
		}

		// Post-rename problems card in Done mode when non-rewritten Go references remain.
		if st.Mode == ReferencesModeDone && len(st.CheckProblems) > 0 {
			sections = append(sections, referencesDoneProblemsCard(ed, st.CheckProblems))
		}

		// Scene cards grid header.
		var scenes []UsageFileGroup
		if st.Report != nil {
			scenes = st.Report.Scenes
		}
		sceneRefs := countGroupRefs(scenes)
		sections = append(sections, core.View{
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
			},
			Children: []core.Node{
				core.Text{Content: "SCENES", Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted}},
				core.Text{
					Content: fmt.Sprintf("%d %s in %d %s", sceneRefs, pluralWord(sceneRefs, "instance", "instances"), len(scenes), pluralWord(len(scenes), "scene", "scenes")),
					Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
				},
			},
		})

		// Scene cards arranged in rows of 3.
		if len(scenes) == 0 {
			sections = append(sections, core.View{
				Style: core.Style{
					Padding:      core.All(16),
					CornerRadius: 10,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: "No scene files instance this target.",
						Style:   core.Style{FontSize: 12, Color: specTextMuted},
					},
				},
			})
		} else {
			for i := 0; i < len(scenes); i += 3 {
				end := min(i+3, len(scenes))
				rowCards := make([]core.Node, 0, 3)
				for j := i; j < end; j++ {
					rowCards = append(rowCards, referencesSceneCard(ed, st, scenes[j]))
				}
				for pad := end - i; pad < 3; pad++ {
					rowCards = append(rowCards, core.View{Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)}})
				}
				sections = append(sections, core.View{
					Style:    core.Style{Direction: core.Row, Gap: 12},
					Children: rowCards,
				})
			}
		}

		gridNote := "Click a scene to see its usages; ↵ on a usage opens the scene with that object framed."
		if st.Mode == ReferencesModeRename {
			gridNote = "Each scene keeps its instances and their overrides; only the prefab path in each line changes."
		}
		sections = append(sections, core.Text{
			Content: gridNote,
			Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
		})

		return ui.ScrollView{
			TestID: "refs-center",
			Style: core.Style{
				FlexGrow:   1,
				FlexBasis:  core.Pt(0),
				Background: specBg,
				Padding:    core.All(20),
			},
			Children: []core.Node{
				core.View{Style: core.Style{Gap: 14}, Children: sections},
			},
		}
	})
}

func referencesRenamePlanCard(plan *SafeRenamePlan) core.Node {
	type planRow struct {
		kind string // "step", "ok", "warn"
		text string
		meta string
	}
	var items []planRow
	if plan.To != "" {
		items = append(items, planRow{kind: "step", text: fmt.Sprintf("Move %s to %s", plan.From, plan.To), meta: "1 file"})
	} else {
		items = append(items, planRow{kind: "warn", text: plan.ValidationMsg, meta: ""})
	}
	if plan.Valid {
		items = append(items, planRow{
			kind: "step",
			text: fmt.Sprintf("Rewrite %d %s in scenes, variants and data", plan.RewrittenRefCount, pluralWord(plan.RewrittenRefCount, "reference", "references")),
			meta: fmt.Sprintf("%d %s", plan.RewrittenFileCount, pluralWord(plan.RewrittenFileCount, "file", "files")),
		})
		if len(plan.PreservedObjects) > 0 {
			items = append(items, planRow{
				kind: "ok",
				text: fmt.Sprintf("Overrides on %s are kept as they are", joinEnglishList(plan.PreservedObjects)),
			})
		}
	}
	for _, prob := range plan.Problems {
		text := prob.Message
		if prob.AskSent {
			text = fmt.Sprintf("The agent is updating %s; it will ask before editing Go", prob.File)
		}
		meta := ""
		if prob.Line > 0 {
			meta = fmt.Sprintf("line %d", prob.Line)
		}
		items = append(items, planRow{kind: "warn", text: text, meta: meta})
	}
	if plan.Valid {
		items = append(items, planRow{kind: "ok", text: "Then game check runs; nothing may point at the old name"})
	}

	rowNodes := make([]core.Node, 0, len(items))
	for i, it := range items {
		var iconBg core.Color
		var iconNode core.Node
		switch it.kind {
		case "ok":
			iconBg = specAddedFill
			iconNode = core.Icon{SVG: lucide.Check(), Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: specAdded}}
		case "warn":
			iconBg = specWarningFill
			iconNode = core.Text{Content: "!", Style: core.Style{FontSize: 11, FontWeight: 700, Color: specWarning}}
		default:
			iconBg = specLineRaised
			iconNode = core.View{Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3, Background: specTextMuted}}
		}
		var border core.Edges
		if i < len(items)-1 {
			border = core.Edges{Bottom: 1}
		}
		rowNodes = append(rowNodes, core.View{
			Style: core.Style{
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				MinHeight:   core.Pt(32),
				Gap:         10,
				Padding:     core.Symmetric(4, 0),
				Borders:     border,
				BorderColor: specLine,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:          core.Pt(18),
						Height:         core.Pt(18),
						CornerRadius:   9,
						Background:     iconBg,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
					},
					Children: []core.Node{iconNode},
				},
				core.Text{
					Content: it.text,
					Style:   core.Style{FontSize: 12, Color: specText, FlexGrow: 1, MinWidth: core.Pt(0)},
				},
				core.Text{
					Content: it.meta,
					Style:   core.Style{FontSize: 11, FontFamily: specRefMonoFamily, Color: specTextMuted, FlexShrink: 0},
				},
			},
		})
	}

	return core.View{
		TestID: "refs-rename-plan",
		Style: core.Style{
			Padding:      core.Symmetric(8, 16),
			CornerRadius: 12,
			Background:   specRaised,
			BorderWidth:  1,
			BorderColor:  specLineRaised,
		},
		Children: rowNodes,
	}
}

func referencesDoneProblemsCard(ed *Editor, problems []SafeRenameProblem) core.Node {
	rows := make([]core.Node, 0, len(problems))
	for _, p := range problems {
		prob := p
		text := prob.Message
		if prob.AskSent {
			text = fmt.Sprintf("The agent is updating %s; it will ask before editing Go", prob.File)
		}
		rows = append(rows, core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        10,
				Padding:    core.Symmetric(6, 0),
			},
			Children: []core.Node{
				core.Text{Content: "!", Style: core.Style{FontSize: 12, FontWeight: 700, Color: specWarning, FlexShrink: 0}},
				core.Text{Content: text, Style: core.Style{FontSize: 12, Color: specText, FlexGrow: 1, MinWidth: core.Pt(0)}},
				ui.Button{
					Text:    "Inspect",
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return specLineRaised },
					TextStyle: core.Style{
						FontSize: 11,
						Color:    specText,
					},
					OnPress: func() {
						ed.FocusUsage(prob.File, 0)
					},
				},
			},
		})
	}
	return core.View{
		TestID: "refs-done-problems",
		Style: core.Style{
			Padding:      core.Symmetric(8, 14),
			CornerRadius: 12,
			Background:   specWarningFill,
			BorderWidth:  1,
			BorderColor:  specWarningEdge,
		},
		Children: rows,
	}
}

func referencesSceneCard(ed *Editor, st *ReferencesScreenState, fg UsageFileGroup) core.Node {
	filePath := fg.File
	focused := st.FocusedFile == filePath
	borderCol := specLineRaised
	borderW := core.Points(1)
	if focused {
		borderCol = specAccent
		borderW = 1.5
	}
	meta := fmt.Sprintf("%d %s", len(fg.Refs), pluralWord(len(fg.Refs), "instance", "instances"))
	if st.Mode == ReferencesModeRename {
		meta = fmt.Sprintf("%d %s", len(fg.Refs), pluralWord(len(fg.Refs), "line", "lines"))
	}

	// Capture snapshot data for canvas draw.
	refsCopy := append([]UsageItem(nil), fg.Refs...)
	vw := fg.VirtualWidth
	if vw <= 0 {
		vw = 640
	}
	vh := fg.VirtualHeight
	if vh <= 0 {
		vh = 360
	}
	hotIdx := -1
	if focused {
		hotIdx = st.FocusedRefIdx
	}

	return core.View{
		TestID: "refs-scene-card-" + filePath,
		Role:   core.RoleButton,
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			Padding:      core.All(8),
			Gap:          8,
			CornerRadius: 12,
			Background:   specRaised,
			BorderWidth:  borderW,
			BorderColor:  borderCol,
		},
		OnPress: func(core.PressEvent) {
			ed.FocusUsage(filePath, 0)
		},
		Children: []core.Node{
			core.Canvas{
				Style: core.Style{
					Width:        core.Pct(100),
					Height:       core.Pt(104),
					CornerRadius: 6,
				},
				Draw: func(d *core.Drawing, size core.Size) {
					w, h := float32(size.Width), float32(size.Height)
					if w <= 0 || h <= 0 {
						return
					}
					// Dark scene sky backdrop using Spec tokens.
					d.SetFillColor(specPanel).FillRect(0, 0, w, h)
					// Ground silhouette at bottom.
					groundH := h * 0.18
					d.SetFillColor(specLineRaised).FillRect(0, h-groundH, w, groundH)
					d.SetFillColor(specLine).FillRect(0, h-groundH, w, 1.5)

					// Subtle platform bar.
					d.SetFillColor(specLineRaised).BeginPath().RoundRect(w*0.42, h*0.56, w*0.28, h*0.05, 1.5).Fill()

					// Draw plotted markers for each instanced object.
					for i, r := range refsCopy {
						nx := float32(0.2 + 0.6*float64(i)/math.Max(1, float64(len(refsCopy)-1)))
						ny := float32(0.82)
						if len(refsCopy) == 1 {
							nx = 0.5
						}
						if r.HasPos {
							nx = clampCardCoord(r.X / vw)
							ny = clampCardCoord(r.Y / vh)
							if ny < 0.18 {
								ny = 0.18
							}
							if ny > 0.84 {
								ny = 0.84
							}
						}
						mw := max(w*0.07, 8)
						mh := max(h*0.12, 8)
						mx := nx*w - mw*0.5
						my := ny*h - mh
						if i == hotIdx {
							d.SetStrokeColor(specAccent).SetLineWidth(2).BeginPath().RoundRect(mx-3, my-3, mw+6, mh+6, 4).Stroke()
						} else if focused {
							d.SetStrokeColor(specAccent).SetLineWidth(1).BeginPath().RoundRect(mx-1.5, my-1.5, mw+3, mh+3, 3).Stroke()
						}
						d.SetFillColor(specAdded).BeginPath().RoundRect(mx, my, mw, mh, 3).Fill()
					}
				},
			},
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Symmetric(0, 4),
					Gap:            8,
				},
				Children: []core.Node{
					core.Text{Content: fg.Name, Style: core.Style{FontSize: 12, FontWeight: 500, Color: specTextStrong}},
					core.Text{Content: meta, Style: core.Style{FontSize: 11.5, Color: specTextMuted}},
				},
			},
		},
	}
}

func referencesCodeView(cx *core.Scope, ed *Editor, refsRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = refsRev.Get()
		st := ed.ReferencesScreen()
		if st == nil {
			return core.View{}
		}
		codeTitle, codeMeta, lines := ed.buildUsagePreviewLines(st)

		codeNodes := make([]core.Node, 0, len(lines))
		for _, l := range lines {
			var bg core.Color
			fg := specTextSecondary
			switch l.Kind {
			case "del":
				bg = specRefDelBg
				fg = specProblem
			case "add":
				bg = specRefAddBg
				fg = specAdded
			case "hit":
				if l.Focused {
					bg = specRefHitBg
				} else {
					bg = specRefHitDimBg
				}
				fg = specTextStrong
			case "warn":
				bg = specWarningFill
				fg = specWarning
			case "gap", "note":
				fg = specTextMuted
			}
			lineNo := ""
			if l.LineNumber > 0 {
				lineNo = strconv.Itoa(l.LineNumber)
			}
			codeNodes = append(codeNodes, core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Padding:    core.Symmetric(2, 14),
					Gap:        6,
					Background: bg,
				},
				Children: []core.Node{
					core.Text{
						Content: lineNo,
						Style:   core.Style{Width: core.Pt(30), FontSize: 11, FontFamily: specRefMonoFamily, Color: specTextMuted, FlexShrink: 0},
					},
					core.Text{
						Content: l.Sign,
						Style:   core.Style{Width: core.Pt(12), FontSize: 11.5, FontFamily: specRefMonoFamily, Color: fg, FlexShrink: 0},
					},
					core.Text{
						Content: l.Text,
						Style:   core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: fg, FlexGrow: 1, MinWidth: core.Pt(0)},
					},
				},
			})
		}

		var children []core.Node
		children = append(children,
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Height:         core.Pt(44),
					Padding:        core.Symmetric(0, 14),
					Gap:            8,
					Borders:        core.Edges{Bottom: 1},
					BorderColor:    specLine,
					FlexShrink:     0,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "refs-code-title",
						Content: codeTitle,
						Style:   core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: specTextStrong, FlexGrow: 1, MinWidth: core.Pt(0)},
					},
					core.Text{
						TestID:  "refs-code-meta",
						Content: codeMeta,
						Style:   core.Style{FontSize: 11.5, Color: specTextMuted, FlexShrink: 0},
					},
				},
			},
			ui.ScrollView{
				TestID: "refs-code-preview",
				Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Padding: core.Symmetric(8, 0)},
				Children: []core.Node{
					core.View{Children: codeNodes},
				},
			},
		)

		// Show Go non-rewritten warning box whenever a Go file is focused.
		if fg := st.Report.FileByPath(st.FocusedFile); fg != nil && fg.NotRewritten {
			goFile := fg.File
			focusLine := 0
			var focusItem UsageItem
			if len(fg.Refs) > 0 {
				idx := min(max(st.FocusedRefIdx, 0), len(fg.Refs)-1)
				focusItem = fg.Refs[idx]
				focusLine = focusItem.Line
			}
			askSent := st.AskSentFiles[goFile]
			askLabel := "Ask the agent to update it"
			if askSent {
				askLabel = "Asked · the agent is on it"
			}
			children = append(children, core.View{
				TestID: "refs-go-warning",
				Style: core.Style{
					Margin:       core.Edges{Left: 14, Right: 14, Bottom: 14},
					Padding:      core.All(12),
					Gap:          8,
					CornerRadius: 12,
					Background:   specWarningFill,
					BorderWidth:  1,
					BorderColor:  specWarningEdge,
					FlexShrink:   0,
				},
				Children: []core.Node{
					core.Text{
						Content: "Go code is not rewritten",
						Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: specWarning},
					},
					core.Text{
						Content: "The rename only rewrites YAML. This string constant keeps the old path and must be updated separately.",
						Style:   core.Style{FontSize: 12, Color: specText},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 8},
						Children: []core.Node{
							core.View{
								TestID: "refs-ask-agent",
								Role:   core.RoleButton,
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 12),
									Gap:          6,
									CornerRadius: 8,
									Background:   specTextStrong,
								},
								OnPress: func(core.PressEvent) {
									ed.report(ed.AskAgentToUpdateUsage(goFile, focusLine))
								},
								Children: []core.Node{
									core.Icon{SVG: lucide.Sparkles(), Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: specAgent}},
									core.Text{Content: askLabel, Style: core.Style{FontSize: 12, FontWeight: 500, Color: specBg}},
								},
							},
							ui.Button{
								TestID:  "refs-open-go-file",
								Text:    "Open " + path.Base(goFile),
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								TextStyle: core.Style{
									FontSize: 12,
									Color:    specText,
								},
								Style: core.Style{
									Height:       core.Pt(30),
									CornerRadius: 8,
									Padding:      core.Symmetric(0, 10),
								},
								OnPress: func() {
									ed.report(ed.OpenUsageItem(focusItem))
								},
							},
						},
					},
				},
			})
		}

		children = append(children, core.View{
			Style: core.Style{
				Padding:     core.Symmetric(10, 14),
				Borders:     core.Edges{Top: 1},
				BorderColor: specLine,
				FlexShrink:  0,
			},
			Children: []core.Node{
				core.Text{
					Content: "The agent finds and renames through the same path, refs_find and refactor_rename, so its renames land here as the same review.",
					Style:   core.Style{FontSize: 11, Color: specTextMuted},
				},
			},
		})

		return core.View{
			TestID: "refs-code-panel",
			Style: core.Style{
				Width:       core.Pt(420),
				FlexShrink:  0,
				Background:  specPanel,
				Borders:     core.Edges{Left: 1},
				BorderColor: specLine,
			},
			Children: children,
		}
	})
}

func refsKeyCap(text string) core.Node {
	return core.View{
		Style: core.Style{
			Height:         core.Pt(18),
			Padding:        core.Symmetric(0, 5),
			CornerRadius:   4,
			BorderWidth:    1,
			BorderColor:    specLineRaised,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
		},
		Children: []core.Node{
			core.Text{Content: text, Style: core.Style{FontSize: 10.5, FontFamily: specRefMonoFamily, Color: specTextMuted}},
		},
	}
}

func countGroupRefs(groups []UsageFileGroup) int {
	n := 0
	for _, g := range groups {
		n += len(g.Refs)
	}
	return n
}

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func joinEnglishList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}

func clampCardCoord(v float32) float32 {
	if v < 0.08 {
		return 0.08
	}
	if v > 0.92 {
		return 0.92
	}
	return v
}
