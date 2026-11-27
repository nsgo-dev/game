package editor

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

const specRefMonoFamily = "JetBrains Mono, Menlo, monospace"

// ReferencesScreenView renders the full-window Find Usages and Safe Rename review screen
// matching design/editor/References.dc.html using only Spec.dc.html tokens.
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
		r := ed.usagesStateRev()
		if ed.ResolvedLanguage() != "en" {
			r ^= 1 << 63
		}
		return r
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := ed.SpecPalette(cx)
		return core.View{
			TestID: "references-screen",
			Role:   core.RoleRegion,
			Label:  ed.T("usages.screenLabel"),
			Style: core.Style{
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Background: p.Bg,
				Overflow:   core.Hidden,
			},
			Children: []core.Node{
				referencesHeaderView(cx, p, ed, refsRev),
				core.View{
					Style: core.Style{
						Direction: core.Row,
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						Overflow:  core.Hidden,
					},
					Children: []core.Node{
						referencesSidebarView(cx, p, ed, refsRev),
						referencesCenterView(cx, p, ed, refsRev),
						referencesCodeView(cx, p, ed, refsRev),
					},
				},
			},
		}
	})
}

func referencesHeaderView(cx *core.Scope, p SpecPalette, ed *Editor, refsRev *core.Memo[uint64]) core.Node {
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
			title = ed.T("usages.titleRename", l10n.Param("name", base))
			subtitle = ed.T("usages.subtitleRename")
		case ReferencesModeDone:
			title = ed.T("usages.titleRenamed", l10n.Param("name", base))
			if len(st.CheckProblems) > 0 {
				p0 := st.CheckProblems[0]
				if len(st.CheckProblems) == 1 {
					subtitle = ed.T("usages.doneOneProblem", l10n.Param("files", st.AppliedFiles), l10n.Param("file", p0.File))
				} else {
					subtitle = ed.T("usages.doneManyProblems", l10n.Param("files", st.AppliedFiles), l10n.Param("problems", len(st.CheckProblems)))
				}
			} else {
				subtitle = ed.T("usages.doneClean", l10n.Param("files", st.AppliedFiles))
			}
		default:
			title = ed.T("usages.titleUsages", l10n.Param("name", base))
			totalU, totalF := 0, 0
			if st.Report != nil {
				totalU = st.Report.TotalUsages
				totalF = st.Report.TotalFiles
			}
			switch {
			case totalU == 1 && totalF == 1:
				subtitle = ed.T("usages.countOneOne")
			case totalU == 1:
				subtitle = ed.T("usages.countOneMany", l10n.Param("files", totalF))
			case totalF == 1:
				subtitle = ed.T("usages.countManyOne", l10n.Param("usages", totalU))
			default:
				subtitle = ed.T("usages.countManyMany", l10n.Param("usages", totalU), l10n.Param("files", totalF))
			}
			if st.SourceObjectID != "" {
				subtitle = ed.T("usages.subtitleWithSource", l10n.Param("summary", subtitle), l10n.Param("id", st.SourceObjectID))
			}
		}

		leftNodes := []core.Node{
			core.View{
				TestID: "refs-back-to-editor",
				Role:   core.RoleButton,
				Label:  ed.T("usages.backLabel"),
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
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: p.TextMuted},
					},
					core.Text{
						Content: ed.T("usages.editor"),
						Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
					},
					refsKeyCap(p, "Esc"),
				},
			},
			core.View{
				Style: core.Style{
					Width:      core.Pt(1),
					Height:     core.Pt(20),
					Background: p.Line,
				},
			},
		}

		if st.Mode == ReferencesModeDone {
			leftNodes = append(leftNodes, core.View{
				Style: core.Style{
					Width:          core.Pt(18),
					Height:         core.Pt(18),
					CornerRadius:   9,
					Background:     specAlpha(p.Added.Fg, 0.12),
					BorderWidth:    1,
					BorderColor:    specAlpha(p.Added.Fg, 0.45),
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.Check(),
						Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: p.Added.Fg},
					},
				},
			})
		}

		leftNodes = append(leftNodes,
			core.Text{
				TestID:  "refs-title",
				Content: title,
				Style:   core.Style{FontSize: 14, FontWeight: 600, Color: p.TextPrimary},
			},
			core.Text{
				TestID:  "refs-subtitle",
				Content: subtitle,
				Style:   core.Style{FontSize: 12, Color: p.TextFaint},
			},
		)

		var rightNodes []core.Node
		switch st.Mode {
		case ReferencesModeUsages:
			rightNodes = append(rightNodes, core.View{
				TestID: "refs-start-rename",
				Role:   core.RoleButton,
				Label:  ed.T("usages.renameShortcutLabel"),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(30),
					Padding:      core.Symmetric(0, 12),
					Gap:          8,
					CornerRadius: 8,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.StartSafeRename())
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.Pencil(),
						Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: p.TextPrimary},
					},
					core.Text{
						Content: ed.T("usages.renameAction"),
						Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: p.TextPrimary},
					},
					core.Text{
						Content: "F2",
						Style:   core.Style{FontSize: 11, FontFamily: specRefMonoFamily, Color: p.TextFaint},
					},
				},
			})
		case ReferencesModeRename:
			valid := st.Plan != nil && st.Plan.Valid
			fileCount := 1
			if st.Plan != nil && st.Plan.TotalTouchedFiles > 0 {
				fileCount = st.Plan.TotalTouchedFiles
			}
			applyLabel := ed.T("usages.applyRenameMany", l10n.Param("count", fileCount))
			if fileCount == 1 {
				applyLabel = ed.T("usages.applyRenameOne")
			}
			applyBg := p.Line
			applyFg := p.TextFaint
			if valid {
				applyBg = p.Accent.Fg
				applyFg = p.Accent.OnFill
			}
			rightNodes = append(rightNodes,
				ui.Button{
					TestID:  "refs-cancel-rename",
					Text:    ed.T("usages.cancel"),
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return p.Raised },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      p.TextSecondary,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 14),
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					OnPress: func() {
						ed.CancelSafeRename()
					},
				},
				ui.Button{
					TestID:   "refs-apply-rename",
					Text:     applyLabel,
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
			backLabel := ed.T("usages.backToScene", l10n.Param("scene", st.ReturnSceneName))
			rightNodes = append(rightNodes,
				ui.Button{
					TestID:  "refs-undo-rename",
					Text:    ed.T("usages.undo"),
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return p.Raised },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      p.TextSecondary,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 14),
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
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
					Color:   func() core.Color { return p.TextPrimary },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 600,
						Color:      p.OnTextPrimary,
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
				Background:     p.Bg,
				Borders:        core.Edges{Bottom: 1},
				BorderColor:    p.Line,
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

func referencesSidebarView(cx *core.Scope, p SpecPalette, ed *Editor, refsRev *core.Memo[uint64]) core.Node {
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
					BorderColor: p.Line,
					FlexShrink:  0,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("usages.newNameHeading"),
						Style:   core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint},
					},
					core.View{
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Padding:      core.Symmetric(2, 8),
							Gap:          2,
							CornerRadius: 8,
							Background:   p.Raised,
							BorderWidth:  1.5,
							BorderColor:  p.Accent.Fg,
						},
						Children: []core.Node{
							core.Show{
								When: func() bool { return renameFolder.Get() != "" },
								Then: func(cx *core.Scope) core.Node {
									return core.Dynamic(func(*core.Scope) core.Node {
										return core.Text{
											Content: renameFolder.Get(),
											Style:   core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: p.TextFaint, FlexShrink: 0},
										}
									})
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(40)},
								Children: []core.Node{
									ui.Input{
										TestID: "refs-new-name-input",
										Label:  ed.T("usages.newFileNameLabel"),
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
											Background:  p.Raised,
											BorderWidth: 0,
											Color:       p.TextPrimary,
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
											Style:   core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: p.TextFaint, FlexShrink: 0},
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
						valColor := p.Added.Fg
						valMsg := ed.T("usages.enterNewName")
						if st != nil && st.Plan != nil && st.Plan.ValidationMsg != "" {
							valMsg = st.Plan.ValidationMsg
						}
						var valBadge core.Node = core.Icon{
							SVG:   lucide.Check(),
							Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: valColor},
						}
						if !valid {
							valColor = p.Warning.Fg
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
									Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
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
		rowActiveBg := specAlpha(p.Accent.Fg, 0.16)

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
					core.Text{Content: label, Style: core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint}},
					core.Text{Content: meta, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
				},
			})
		}

		if st.Mode == ReferencesModeRename && st.Plan != nil && st.Plan.Valid {
			addGroupHeader(ed.T("usages.groupMoved"), "")
			isOpen := st.FocusedFile == movedFocusSentinel
			var rowBg core.Color
			rowText := p.TextSecondary
			if isOpen {
				rowBg = rowActiveBg
				rowText = p.TextPrimary
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
					core.View{Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 2, Background: p.Agent.Fg, FlexShrink: 0}},
					core.Text{
						Content:  st.Plan.From + " → " + st.Plan.NewStem,
						MaxLines: 1,
						Style:    core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: rowText, FlexGrow: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Overflow: core.Hidden},
					},
					core.Text{
						Content: ed.T("usages.movedTag"),
						Style:   core.Style{FontSize: 11, Color: p.TextFaint, FlexShrink: 0},
					},
				},
			})
		}

		addFileRows := func(files []UsageFileGroup) {
			for _, fg := range files {
				filePath := fg.File
				open := st.FocusedFile == filePath
				dotColor := p.TextFaint
				switch fg.Group {
				case UsageGroupScenes:
					dotColor = p.Accent.Fg
				case UsageGroupVariants:
					dotColor = p.Agent.Fg
				case UsageGroupGoCode:
					dotColor = p.Warning.Fg
				}

				var rowBg core.Color
				rowText := p.TextSecondary
				if open {
					rowBg = rowActiveBg
					rowText = p.TextPrimary
				}

				var rightBadge core.Node
				switch {
				case fg.NotRewritten:
					rightBadge = core.View{
						Style: core.Style{
							Height:       core.Pt(20),
							Padding:      core.Symmetric(0, 6),
							CornerRadius: 6,
							Background:   p.Warning.Fill,
							BorderWidth:  1,
							BorderColor:  p.Warning.Edge,
							AlignItems:   core.AlignCenter,
							Direction:    core.Row,
							FlexShrink:   0,
						},
						Children: []core.Node{
							core.Text{Content: ed.T("usages.notRewritten"), Style: core.Style{FontSize: 10.5, Color: p.Warning.Fg}},
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
							core.Text{Content: fmt.Sprintf("−%d", n), Style: core.Style{FontSize: 11, FontFamily: specRefMonoFamily, Color: p.Removed.Fg}},
							core.Text{Content: fmt.Sprintf("+%d", n), Style: core.Style{FontSize: 11, FontFamily: specRefMonoFamily, Color: p.Added.Fg}},
						},
					}
				default:
					rightBadge = core.Text{
						Content: strconv.Itoa(len(fg.Refs)),
						Style:   core.Style{FontSize: 11.5, Color: p.TextFaint, FlexShrink: 0},
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
						core.Icon{SVG: chevIcon, Style: core.Style{Width: core.Pt(11), Height: core.Pt(11), Color: p.TextFaint, FlexShrink: 0}},
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
						itemFg := p.TextMuted
						var leftBorder core.Edges
						if active {
							itemBg = p.Raised
							itemFg = p.TextPrimary
							leftBorder = core.Edges{Left: 2}
						}
						noteText := ""
						noteColor := p.TextFaint
						if uItem.OverrideCount > 0 {
							if uItem.OverrideCount == 1 {
								noteText = ed.T("usages.overrideOne")
							} else {
								noteText = ed.T("usages.overrideMany", l10n.Param("count", uItem.OverrideCount))
							}
							noteColor = p.Changed.Fg
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
								BorderColor:  p.Accent.Fg,
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
									Style:   core.Style{Width: core.Pt(30), FontSize: 11, FontFamily: specRefMonoFamily, Color: p.TextFaint, FlexShrink: 0},
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
			sceneMeta := ed.T("usages.instanceMany", l10n.Param("count", sceneRefs))
			if sceneRefs == 1 {
				sceneMeta = ed.T("usages.instanceOne")
			}
			addGroupHeader(ed.T("usages.groupScenes"), sceneMeta)
			addFileRows(st.Report.Scenes)

			variantCount := len(st.Report.Variants)
			addGroupHeader(ed.T("usages.groupVariants"), ed.T("usages.inheritCount", l10n.Param("count", variantCount)))
			addFileRows(st.Report.Variants)

			dataRows := countGroupRefs(st.Report.DataTables)
			dataMeta := ed.T("usages.rowMany", l10n.Param("count", dataRows))
			if dataRows == 1 {
				dataMeta = ed.T("usages.rowOne")
			}
			addGroupHeader(ed.T("usages.groupDataTables"), dataMeta)
			addFileRows(st.Report.DataTables)

			goRefs := countGroupRefs(st.Report.GoCode)
			goMeta := ed.T("usages.constantMany", l10n.Param("count", goRefs))
			if goRefs == 1 {
				goMeta = ed.T("usages.constantOne")
			}
			addGroupHeader(ed.T("usages.groupGoCode"), goMeta)
			addFileRows(st.Report.GoCode)

			if len(st.Report.Other) > 0 {
				otherRefs := countGroupRefs(st.Report.Other)
				otherMeta := ed.T("usages.refMany", l10n.Param("count", otherRefs))
				if otherRefs == 1 {
					otherMeta = ed.T("usages.refOne")
				}
				addGroupHeader(ed.T("usages.groupOther"), otherMeta)
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
			Background:  p.Bg,
			Borders:     core.Edges{Right: 1},
			BorderColor: p.Line,
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
					BorderColor: p.Line,
					FlexShrink:  0,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("usages.sidebarHint"),
						Style:   core.Style{FontSize: 11, Color: p.TextFaint},
					},
				},
			},
		},
	}
}

func referencesCenterView(cx *core.Scope, p SpecPalette, ed *Editor, refsRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = refsRev.Get()
		st := ed.ReferencesScreen()
		if st == nil {
			return core.View{}
		}

		var sections []core.Node

		// Rename plan summary card during Rename review mode.
		if st.Mode == ReferencesModeRename && st.Plan != nil {
			sections = append(sections, referencesRenamePlanCard(ed, p, st.Plan))
		}

		// Post-rename problems card in Done mode when non-rewritten Go references remain.
		if st.Mode == ReferencesModeDone && len(st.CheckProblems) > 0 {
			sections = append(sections, referencesDoneProblemsCard(p, ed, st.CheckProblems))
		}

		// Scene cards grid header.
		var scenes []UsageFileGroup
		if st.Report != nil {
			scenes = st.Report.Scenes
		}
		sceneRefs := countGroupRefs(scenes)
		var sceneCountMeta string
		switch {
		case sceneRefs == 1 && len(scenes) == 1:
			sceneCountMeta = ed.T("usages.instancesInScenesOneOne")
		case sceneRefs == 1:
			sceneCountMeta = ed.T("usages.instancesInScenesOneMany", l10n.Param("scenes", len(scenes)))
		case len(scenes) == 1:
			sceneCountMeta = ed.T("usages.instancesInScenesManyOne", l10n.Param("instances", sceneRefs))
		default:
			sceneCountMeta = ed.T("usages.instancesInScenesManyMany", l10n.Param("instances", sceneRefs), l10n.Param("scenes", len(scenes)))
		}
		sections = append(sections, core.View{
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
			},
			Children: []core.Node{
				core.Text{Content: ed.T("usages.groupScenes"), Style: core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint}},
				core.Text{
					Content: sceneCountMeta,
					Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
				},
			},
		})

		// Scene cards arranged in rows of 3.
		if len(scenes) == 0 {
			sections = append(sections, core.View{
				Style: core.Style{
					Padding:      core.All(16),
					CornerRadius: 10,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("usages.noScenes"),
						Style:   core.Style{FontSize: 12, Color: p.TextFaint},
					},
				},
			})
		} else {
			for i := 0; i < len(scenes); i += 3 {
				end := min(i+3, len(scenes))
				rowCards := make([]core.Node, 0, 3)
				for j := i; j < end; j++ {
					rowCards = append(rowCards, referencesSceneCard(p, ed, st, scenes[j]))
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

		gridNote := ed.T("usages.gridNoteUsages")
		if st.Mode == ReferencesModeRename {
			gridNote = ed.T("usages.gridNoteRename")
		}
		sections = append(sections, core.Text{
			Content: gridNote,
			Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
		})

		return ui.ScrollView{
			TestID: "refs-center",
			Style: core.Style{
				FlexGrow:   1,
				FlexBasis:  core.Pt(0),
				Background: p.Bg,
				Padding:    core.All(20),
			},
			Children: []core.Node{
				core.View{Style: core.Style{Gap: 14}, Children: sections},
			},
		}
	})
}

func referencesRenamePlanCard(ed *Editor, p SpecPalette, plan *SafeRenamePlan) core.Node {
	type planRow struct {
		kind string // "step", "ok", "warn"
		text string
		meta string
	}
	var items []planRow
	if plan.To != "" {
		items = append(items, planRow{kind: "step", text: ed.T("usages.planMove", l10n.Param("from", plan.From), l10n.Param("to", plan.To)), meta: ed.T("usages.fileOne")})
	} else {
		items = append(items, planRow{kind: "warn", text: plan.ValidationMsg, meta: ""})
	}
	if plan.Valid {
		rewriteText := ed.T("usages.planRewriteMany", l10n.Param("count", plan.RewrittenRefCount))
		if plan.RewrittenRefCount == 1 {
			rewriteText = ed.T("usages.planRewriteOne")
		}
		fileMeta := ed.T("usages.fileMany", l10n.Param("count", plan.RewrittenFileCount))
		if plan.RewrittenFileCount == 1 {
			fileMeta = ed.T("usages.fileOne")
		}
		items = append(items, planRow{
			kind: "step",
			text: rewriteText,
			meta: fileMeta,
		})
		if len(plan.PreservedObjects) > 0 {
			items = append(items, planRow{
				kind: "ok",
				text: ed.T("usages.planOverridesKept", l10n.Param("objects", joinLocalizedList(ed, plan.PreservedObjects))),
			})
		}
	}
	for _, prob := range plan.Problems {
		text := prob.Message
		if prob.AskSent {
			text = ed.T("usages.agentUpdatingGo", l10n.Param("file", prob.File))
		}
		meta := ""
		if prob.Line > 0 {
			meta = ed.T("usages.lineNumber", l10n.Param("line", prob.Line))
		}
		items = append(items, planRow{kind: "warn", text: text, meta: meta})
	}
	if plan.Valid {
		items = append(items, planRow{kind: "ok", text: ed.T("usages.planGameCheck")})
	}

	rowNodes := make([]core.Node, 0, len(items))
	for i, it := range items {
		var iconBg core.Color
		var iconNode core.Node
		switch it.kind {
		case "ok":
			iconBg = specAlpha(p.Added.Fg, 0.12)
			iconNode = core.Icon{SVG: lucide.Check(), Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: p.Added.Fg}}
		case "warn":
			iconBg = p.Warning.Fill
			iconNode = core.Text{Content: "!", Style: core.Style{FontSize: 11, FontWeight: 700, Color: p.Warning.Fg}}
		default:
			iconBg = p.LineRaised
			iconNode = core.View{Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3, Background: p.TextFaint}}
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
				BorderColor: p.Line,
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
					Style:   core.Style{FontSize: 12, Color: p.TextSecondary, FlexGrow: 1, MinWidth: core.Pt(0)},
				},
				core.Text{
					Content: it.meta,
					Style:   core.Style{FontSize: 11, FontFamily: specRefMonoFamily, Color: p.TextFaint, FlexShrink: 0},
				},
			},
		})
	}

	return core.View{
		TestID: "refs-rename-plan",
		Style: core.Style{
			Padding:      core.Symmetric(8, 16),
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
		},
		Children: rowNodes,
	}
}

func referencesDoneProblemsCard(p SpecPalette, ed *Editor, problems []SafeRenameProblem) core.Node {
	rows := make([]core.Node, 0, len(problems))
	for _, item := range problems {
		prob := item
		text := prob.Message
		if prob.AskSent {
			text = ed.T("usages.agentUpdatingGo", l10n.Param("file", prob.File))
		}
		rows = append(rows, core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        10,
				Padding:    core.Symmetric(6, 0),
			},
			Children: []core.Node{
				core.Text{Content: "!", Style: core.Style{FontSize: 12, FontWeight: 700, Color: p.Warning.Fg, FlexShrink: 0}},
				core.Text{Content: text, Style: core.Style{FontSize: 12, Color: p.TextSecondary, FlexGrow: 1, MinWidth: core.Pt(0)}},
				ui.Button{
					Text:    ed.T("usages.inspect"),
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return p.LineRaised },
					TextStyle: core.Style{
						FontSize: 11,
						Color:    p.TextSecondary,
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
			Background:   p.Warning.Fill,
			BorderWidth:  1,
			BorderColor:  p.Warning.Edge,
		},
		Children: rows,
	}
}

func referencesSceneCard(p SpecPalette, ed *Editor, st *ReferencesScreenState, fg UsageFileGroup) core.Node {
	filePath := fg.File
	focused := st.FocusedFile == filePath
	borderCol := p.LineRaised
	borderW := core.Points(1)
	if focused {
		borderCol = p.Accent.Fg
		borderW = 1.5
	}
	meta := ed.T("usages.instanceMany", l10n.Param("count", len(fg.Refs)))
	if len(fg.Refs) == 1 {
		meta = ed.T("usages.instanceOne")
	}
	if st.Mode == ReferencesModeRename {
		meta = ed.T("usages.lineMany", l10n.Param("count", len(fg.Refs)))
		if len(fg.Refs) == 1 {
			meta = ed.T("usages.lineOne")
		}
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
			Background:   p.Raised,
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
					// Scene sky backdrop using Spec tokens.
					d.SetFillColor(p.Panel).FillRect(0, 0, w, h)
					// Ground silhouette at bottom.
					groundH := h * 0.18
					d.SetFillColor(p.LineRaised).FillRect(0, h-groundH, w, groundH)
					d.SetFillColor(p.Line).FillRect(0, h-groundH, w, 1.5)

					// Subtle platform bar.
					d.SetFillColor(p.LineRaised).BeginPath().RoundRect(w*0.42, h*0.56, w*0.28, h*0.05, 1.5).Fill()

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
							d.SetStrokeColor(p.Accent.Fg).SetLineWidth(2).BeginPath().RoundRect(mx-3, my-3, mw+6, mh+6, 4).Stroke()
						} else if focused {
							d.SetStrokeColor(p.Accent.Fg).SetLineWidth(1).BeginPath().RoundRect(mx-1.5, my-1.5, mw+3, mh+3, 3).Stroke()
						}
						d.SetFillColor(p.Added.Fg).BeginPath().RoundRect(mx, my, mw, mh, 3).Fill()
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
					core.Text{Content: fg.Name, Style: core.Style{FontSize: 12, FontWeight: 500, Color: p.TextPrimary}},
					core.Text{Content: meta, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
				},
			},
		},
	}
}

func referencesCodeView(cx *core.Scope, p SpecPalette, ed *Editor, refsRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = refsRev.Get()
		st := ed.ReferencesScreen()
		if st == nil {
			return core.View{}
		}
		codeTitle, codeMeta, lines := ed.buildUsagePreviewLines(st)
		hitBg := specAlpha(p.Accent.Fg, 0.18)
		hitDimBg := specAlpha(p.Accent.Fg, 0.08)
		delBg := specAlpha(p.Removed.Fg, 0.10)
		addBg := specAlpha(p.Added.Fg, 0.10)

		codeNodes := make([]core.Node, 0, len(lines))
		for _, l := range lines {
			var bg core.Color
			fg := p.TextMuted
			switch l.Kind {
			case "del":
				bg = delBg
				fg = p.Removed.Fg
			case "add":
				bg = addBg
				fg = p.Added.Fg
			case "hit":
				if l.Focused {
					bg = hitBg
				} else {
					bg = hitDimBg
				}
				fg = p.TextPrimary
			case "warn":
				bg = p.Warning.Fill
				fg = p.Warning.Fg
			case "gap", "note":
				fg = p.TextFaint
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
						Style:   core.Style{Width: core.Pt(30), FontSize: 11, FontFamily: specRefMonoFamily, Color: p.TextFaint, FlexShrink: 0},
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
					BorderColor:    p.Line,
					FlexShrink:     0,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "refs-code-title",
						Content: codeTitle,
						Style:   core.Style{FontSize: 11.5, FontFamily: specRefMonoFamily, Color: p.TextPrimary, FlexGrow: 1, MinWidth: core.Pt(0)},
					},
					core.Text{
						TestID:  "refs-code-meta",
						Content: codeMeta,
						Style:   core.Style{FontSize: 11.5, Color: p.TextFaint, FlexShrink: 0},
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
			askLabel := ed.T("usages.askAgentUpdate")
			if askSent {
				askLabel = ed.T("usages.askedAgent")
			}
			children = append(children, core.View{
				TestID: "refs-go-warning",
				Style: core.Style{
					Margin:       core.Edges{Left: 14, Right: 14, Bottom: 14},
					Padding:      core.All(12),
					Gap:          8,
					CornerRadius: 12,
					Background:   p.Warning.Fill,
					BorderWidth:  1,
					BorderColor:  p.Warning.Edge,
					FlexShrink:   0,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("usages.goNotRewrittenTitle"),
						Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.Warning.Fg},
					},
					core.Text{
						Content: ed.T("usages.goNotRewrittenBody"),
						Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
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
									Background:   p.TextPrimary,
								},
								OnPress: func(core.PressEvent) {
									ed.report(ed.AskAgentToUpdateUsage(goFile, focusLine))
								},
								Children: []core.Node{
									core.Icon{SVG: lucide.Sparkles(), Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: p.OnTextPrimary}},
									core.Text{Content: askLabel, Style: core.Style{FontSize: 12, FontWeight: 500, Color: p.OnTextPrimary}},
								},
							},
							ui.Button{
								TestID:  "refs-open-go-file",
								Text:    ed.T("usages.openFile", l10n.Param("file", path.Base(goFile))),
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								TextStyle: core.Style{
									FontSize: 12,
									Color:    p.TextSecondary,
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
				BorderColor: p.Line,
				FlexShrink:  0,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("usages.agentFooterNote"),
					Style:   core.Style{FontSize: 11, Color: p.TextFaint},
				},
			},
		})

		return core.View{
			TestID: "refs-code-panel",
			Style: core.Style{
				Width:       core.Pt(420),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
			},
			Children: children,
		}
	})
}

func refsKeyCap(p SpecPalette, text string) core.Node {
	return core.View{
		Style: core.Style{
			Height:         core.Pt(18),
			Padding:        core.Symmetric(0, 5),
			CornerRadius:   4,
			BorderWidth:    1,
			BorderColor:    p.LineRaised,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
		},
		Children: []core.Node{
			core.Text{Content: text, Style: core.Style{FontSize: 10.5, FontFamily: specRefMonoFamily, Color: p.TextFaint}},
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

func joinLocalizedList(ed *Editor, items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return ed.T("usages.listTwo", l10n.Param("a", items[0]), l10n.Param("b", items[1]))
	default:
		return ed.T("usages.listMany", l10n.Param("init", strings.Join(items[:len(items)-1], ed.T("usages.listSep"))), l10n.Param("last", items[len(items)-1]))
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
