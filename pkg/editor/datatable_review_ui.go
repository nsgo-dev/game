package editor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type dtReviewRowDrag struct {
	key string
}

type dtReviewDropHint struct {
	key    string
	before bool
	on     bool
}

// DataTableReviewView renders the full-window row-by-row Data Table review screen matching
// design/editor/ReviewData.dc.html using exclusively theme.Palette tokens (Dark & Light).
func DataTableReviewView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	reviewShortcutsActive := func() bool {
		return ed.DataTableReviewOpen() &&
			!ed.shortcutCaptureActive &&
			!ed.CommandPaletteOpen() &&
			!ed.SettingsOpen() &&
			!ed.PermissionsDraftOpen() &&
			!ed.StartScreenOpen() &&
			!ed.FirstRunFullScreenOpen() &&
			!ed.MergeConflictOpen()
	}
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
		if !reviewShortcutsActive() {
			return
		}
		if _, _, editing := ed.EditingDataTableReviewCell(); editing {
			ed.FinishDataTableReviewCellEdit()
			return
		}
		ed.PauseDataTableReview()
	})
	cx.OnKeyWith("mod+enter", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if reviewShortcutsActive() {
			ed.report(ed.KeepDataTableReview())
		}
	})
	cx.OnKeyWith("mod+backspace", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if reviewShortcutsActive() {
			ed.report(ed.RevertAllDataTableReview())
		}
	})
	cx.OnKeyWith("j", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if reviewShortcutsActive() {
			ed.StepDataTableReviewFocus(+1)
		}
	})
	cx.OnKeyWith("k", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if reviewShortcutsActive() {
			ed.StepDataTableReviewFocus(-1)
		}
	})
	cx.OnKeyWith("x", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if reviewShortcutsActive() {
			if key := ed.DataTableReviewFocusKey(); key != "" {
				ed.report(ed.ToggleDataTableReviewRecord(key))
			}
		}
	})

	reviewRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.dataTableReviewRev()
	})
	type reviewChrome struct {
		dark bool
		lang string
	}
	chromeMemo := core.NewMemo(cx, func() reviewChrome {
		_ = rev.Get()
		return reviewChrome{
			dark: ed.Palette(cx).Dark,
			lang: ed.ResolvedLanguage(),
		}
	})
	dropHint := core.NewSignal(cx, dtReviewDropHint{})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := theme.PaletteFor(chromeMemo.Get().dark)
		return core.View{
			TestID: "dt-review-screen",
			Role:   core.RoleRegion,
			Label:  ed.T("dtReview.title"),
			Style: core.Style{
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Background: p.Bg,
				Overflow:   core.Hidden,
			},
			Children: []core.Node{
				dtReviewHeaderView(cx, p, ed, reviewRev),
				core.View{
					Style: core.Style{
						Direction: core.Row,
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						Overflow:  core.Hidden,
					},
					Children: []core.Node{
						dtReviewLeftSidebar(cx, p, ed, reviewRev),
						dtReviewCenterPane(cx, p, ed, reviewRev, dropHint),
						dtReviewRightYAML(cx, p, ed, reviewRev),
					},
				},
			},
		}
	})
}

func dtReviewHeaderView(cx *core.Scope, p theme.Palette, ed *Editor, reviewRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		kept, total := ed.DataTableReviewKeptCount()
		prompt := ed.DataTableReviewPrompt()

		children := []core.Node{
			base.Button{
				TestID:  "dt-review-back",
				Label:   ed.T("dtReview.backLabel"),
				Tooltip: specTooltipNode(p, ed.T("dtReview.backLabel")),
				Style: core.Style{
					Height:       core.Pt(specRowHeight),
					MinWidth:     core.Pt(specRowHeight),
					FlexShrink:   0,
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          specSpace8,
					Padding:      core.Symmetric(0, specSpace8),
					CornerRadius: specRadiusControl,
					Cursor:       core.CursorPointer,
					Hover:        &core.Style{Background: p.Raised},
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.PauseDataTableReview()
				},
				Children: []core.Node{
					specIcon(lucide.ArrowLeft, 15, p.TextMuted),
					core.Text{
						Content: ed.T("dtReview.back"),
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextMuted,
						},
					},
					core.View{
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
							core.Text{
								Content: "Esc",
								Style: core.Style{
									FontFamily: core.FontMonospace,
									FontSize:   11,
									Color:      p.TextFaint,
								},
							},
						},
					},
				},
			},
			core.View{
				Style: core.Style{
					Width:      core.Pt(1),
					Height:     core.Pt(20),
					Background: p.Line,
					FlexShrink: 0,
				},
			},
			core.Text{
				TestID:  "dt-review-title",
				Content: ed.T("dtReview.title"),
				Style: core.Style{
					FontSize:   14,
					FontWeight: core.FontWeightSemibold,
					Color:      p.TextPrimary,
					FlexShrink: 0,
				},
			},
		}

		if prompt != "" {
			children = append(children, core.Text{
				TestID:   "dt-review-prompt",
				Content:  "“" + prompt + "”",
				MaxLines: 1,
				Style: core.Style{
					FontSize:   12.5,
					Color:      p.TextFaint,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
				},
			})
		}

		keepText := ed.T("dtReview.keepCount", l10n.Param("kept", kept), l10n.Param("total", total))
		children = append(children,
			core.View{Style: core.Style{FlexGrow: 1}},
			specPanelButton(p, specPanelButtonSpec{
				TestID:  "dt-review-revert-all",
				Text:    ed.T("dtReview.revertAll"),
				Label:   ed.T("dtReview.revertAll"),
				Tooltip: specShortcutTip(ed.T("dtReview.revertAll"), FormatShortcut("mod+backspace", cx.Env().OS())),
				Variant: specBtnQuiet,
				Style:   core.Style{Height: core.Pt(32), Padding: core.Symmetric(0, 14)},
				OnPress: func() {
					ed.report(ed.RevertAllDataTableReview())
				},
			}),
			specPanelButton(p, specPanelButtonSpec{
				TestID:  "dt-review-keep",
				Text:    keepText,
				Label:   keepText,
				Tooltip: specShortcutTip(keepText, FormatShortcut("mod+enter", cx.Env().OS())),
				Variant: specBtnAccent,
				Style:   core.Style{Height: core.Pt(32), Padding: core.Symmetric(0, 14)},
				OnPress: func() {
					ed.report(ed.KeepDataTableReview())
				},
			}),
		)

		return core.View{
			TestID: "dt-review-header",
			Style: core.Style{
				Height:      core.Pt(specHeaderHeight),
				FlexShrink:  0,
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         specSpace12,
				Padding:     core.Symmetric(0, specSpace12),
				Background:  p.Bg,
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
			},
			Children: children,
		}
	})
}

func dtReviewLeftSidebar(cx *core.Scope, p theme.Palette, ed *Editor, reviewRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		files := ed.DataTableReviewFiles()
		activeIdx := ed.DataTableReviewActiveFileIndex()
		checks := ed.DataTableReviewChecks()
		stale := ed.DataTableReviewChecksOutOfDate()

		filesHeading := ed.T("dtReview.filesHeading", l10n.Param("count", len(files)))
		children := []core.Node{
			core.View{
				Style: core.Style{
					Height:         core.Pt(specRowHeight),
					Padding:        core.Symmetric(0, specSpace8),
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{specSectionLabel(p, filesHeading)},
			},
		}

		for i, f := range files {
			idx := i
			selected := i == activeIdx
			bg := core.Color(0)
			if selected {
				bg = p.Accent.Fill
			}
			children = append(children, base.Button{
				TestID: fmt.Sprintf("dt-review-file-%d", i),
				Label:  fmt.Sprintf("%s, %s", f.Name, f.Meta),
				Selected: func() bool {
					return selected
				},
				Style: core.Style{
					Height:       core.Pt(32),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          specSpace8,
					Padding:      core.Symmetric(0, specSpace8),
					CornerRadius: specRadiusButton,
					Background:   bg,
					Cursor:       core.CursorPointer,
					Hover:        &core.Style{Background: p.Raised},
					FocusVisible: p.FocusRing(-2),
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.SelectDataTableReviewFile(idx))
				},
				Children: []core.Node{
					specIcon(lucide.Table2, 14, p.TextMuted),
					core.Text{
						Content:  f.Name,
						MaxLines: 1,
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      p.TextSecondary,
						},
					},
					core.Text{
						TestID:  fmt.Sprintf("dt-review-file-meta-%d", i),
						Content: f.Meta,
						Style: core.Style{
							FontSize:   11.5,
							Color:      p.TextFaint,
							FlexShrink: 0,
						},
					},
				},
			})
		}

		if len(checks) > 0 {
			children = append(children, dtReviewChecksCard(p, ed, checks, stale))
		}

		return core.View{
			TestID: "dt-review-sidebar",
			Style: core.Style{
				Width:       core.Pt(280),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Right: 1},
				BorderColor: p.Line,
				Overflow:    core.Hidden,
			},
			Children: []core.Node{
				ui.ScrollView{
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
					},
					ContentStyle: core.Style{
						Padding: core.Symmetric(specSpace12, specSpace8),
						Gap:     specSpace4,
					},
					Children: children,
				},
			},
		}
	})
}

func dtReviewChecksCard(p theme.Palette, ed *Editor, checks []DataTableReviewCheck, stale bool) core.Node {
	headChildren := []core.Node{
		core.Text{
			Content: ed.T("dtReview.agentCheck"),
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: core.FontWeightMedium,
				Color:      p.TextPrimary,
				FlexGrow:   1,
			},
		},
	}
	if stale {
		headChildren = append(headChildren, core.View{
			TestID: "dt-review-checks-stale",
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          4,
				Height:       core.Pt(20),
				Padding:      core.Symmetric(0, 6),
				CornerRadius: 6,
				Background:   p.Warning.Fill,
				BorderWidth:  1,
				BorderColor:  p.Warning.Edge,
				FlexShrink:   0,
			},
			Children: []core.Node{
				specIcon(lucide.TriangleAlert, 11, p.Warning.Fg),
				core.Text{
					Content: ed.T("dtReview.outOfDate"),
					Style: core.Style{
						FontSize:   11,
						FontWeight: core.FontWeightMedium,
						Color:      p.Warning.Fg,
					},
				},
			},
		})
	}

	cardChildren := []core.Node{
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        specSpace8,
			},
			Children: headChildren,
		},
	}

	var bases []string
	seenBasis := make(map[string]struct{})
	for i, ch := range checks {
		valText := ch.Value
		if ch.Before != "" {
			valText = fmt.Sprintf("%s · %s %s", ch.Value, ed.T("dtReview.was"), ch.Before)
		}
		cardChildren = append(cardChildren, core.View{
			TestID: fmt.Sprintf("dt-review-check-%d", i),
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignStart,
				Gap:        specSpace12,
			},
			Children: []core.Node{
				core.Text{
					Content: ch.Label,
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						FontSize:   12.5,
						Color:      p.TextMuted,
					},
				},
				core.Text{
					Content: valText,
					Style: core.Style{
						FontSize:   12.5,
						Color:      p.TextPrimary,
						FlexShrink: 0,
					},
				},
			},
		})
		if ch.Basis != "" {
			if _, dup := seenBasis[ch.Basis]; !dup {
				seenBasis[ch.Basis] = struct{}{}
				bases = append(bases, ch.Basis)
			}
		}
	}

	for i, basis := range bases {
		cardChildren = append(cardChildren, core.Text{
			TestID:  fmt.Sprintf("dt-review-check-basis-%d", i),
			Content: basis,
			Style: core.Style{
				FontSize: 11.5,
				Color:    p.TextFaint,
			},
		})
	}

	if stale {
		cardChildren = append(cardChildren, core.Text{
			TestID:  "dt-review-checks-stale-hint",
			Content: ed.T("dtReview.outOfDateHint"),
			Style: core.Style{
				FontSize: 11.5,
				Color:    p.Warning.Fg,
			},
		})
	}

	borderCol := p.LineRaised
	if stale {
		borderCol = p.Warning.Edge
	}
	return core.View{
		TestID: "dt-review-checks-card",
		Style: core.Style{
			Margin:       core.Edges{Top: specSpace12},
			Padding:      core.All(specSpace12),
			CornerRadius: specRadiusCard,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  borderCol,
			Gap:          specSpace8,
		},
		Children: cardChildren,
	}
}

func dtReviewCenterPane(cx *core.Scope, p theme.Palette, ed *Editor, reviewRev *core.Memo[uint64], dropHint *core.Signal[dtReviewDropHint]) core.Node {
	// Keep the search input mounted outside the table dynamic block so typing never drops focus.
	searchBar := core.View{
		TestID: "dt-review-filter-wrap",
		Style: core.Style{
			Width:        core.Pt(200),
			Height:       core.Pt(specRowHeight),
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          specSpace8,
			Padding:      core.Symmetric(0, specSpace8),
			CornerRadius: specRadiusControl,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
		},
		Children: []core.Node{
			specIcon(lucide.Search, 13, p.TextFaint),
			core.View{
				Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					ui.Input{
						TestID:      "dt-review-filter",
						Label:       ed.T("dtReview.filterPlaceholder"),
						Placeholder: ed.T("dtReview.filterPlaceholder"),
						Style: core.Style{
							Height:           core.Pt(26),
							FontSize:         12.5,
							Color:            p.TextPrimary,
							PlaceholderColor: p.TextFaint,
							Background:       core.Color(0),
							BorderWidth:      0,
							CaretColor:       p.Accent.Edge,
						},
						Value: func() string {
							_ = reviewRev.Get()
							return ed.DataTableReviewSearch()
						},
						OnChange: func(text string) {
							ed.SetDataTableReviewSearch(text)
						},
					},
				},
			},
		},
	}

	hasRecords := core.NewMemo(cx, func() bool {
		_ = reviewRev.Get()
		f := ed.activeDataTableReviewFile()
		return f != nil && len(f.records) > 0
	})

	return core.View{
		TestID: "dt-review-center",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			MinWidth:  core.Pt(0),
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			core.Show{
				When: hasRecords.Get,
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{
							FlexGrow:  1,
							FlexBasis: core.Pt(0),
							Overflow:  core.Hidden,
						},
						Children: []core.Node{
							dtReviewToolbar(cx, p, ed, reviewRev, searchBar),
							dtReviewErrorBanner(p, ed, reviewRev),
							dtReviewTable(cx, p, ed, reviewRev, dropHint),
						},
					}
				},
				Else: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "dt-review-empty",
						Style: core.Style{
							FlexGrow:       1,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Padding:        core.All(specSpace24),
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("dtReview.emptyFile"),
								Style: core.Style{
									FontSize: 12.5,
									Color:    p.TextMuted,
								},
							},
						},
					}
				},
			},
		},
	}
}

func dtReviewToolbar(cx *core.Scope, p theme.Palette, ed *Editor, reviewRev *core.Memo[uint64], searchBar core.Node) core.Node {
	chipsView := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		all, changed, unchanged, reverted, yours := ed.DataTableReviewChipCounts()
		active := ed.DataTableReviewFilter()

		type chipDef struct {
			id    DataTableReviewFilter
			label string
			count int
		}
		defs := []chipDef{
			{DataTableReviewFilterAll, ed.T("dtReview.chip.all"), all},
			{DataTableReviewFilterChanged, ed.T("dtReview.chip.changed"), changed},
			{DataTableReviewFilterUnchanged, ed.T("dtReview.chip.unchanged"), unchanged},
			{DataTableReviewFilterReverted, ed.T("dtReview.chip.reverted"), reverted},
			{DataTableReviewFilterYours, ed.T("dtReview.chip.yours"), yours},
		}
		nodes := make([]core.Node, 0, len(defs))
		for _, c := range defs {
			cd := c
			on := active == cd.id
			bg := core.Color(0)
			border := p.Line
			fg := p.TextMuted
			countFg := p.TextFaint
			if on {
				bg = p.LineRaised
				border = p.LineRaised
				fg = p.TextPrimary
				countFg = p.TextMuted
			}
			nodes = append(nodes, base.Toggle{
				TestID:  "dt-review-chip-" + string(cd.id),
				Label:   fmt.Sprintf("%s %d", cd.label, cd.count),
				Pressed: func() bool { return on },
				Style: core.Style{
					Height:       core.Pt(specRowHeight),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          5,
					Padding:      core.Symmetric(0, 10),
					CornerRadius: specRadiusControl,
					Background:   bg,
					BorderWidth:  1,
					BorderColor:  border,
					Cursor:       core.CursorPointer,
					Hover:        &core.Style{Background: p.Raised},
					FocusVisible: p.FocusRing(-2),
					FlexShrink:   0,
				},
				OnChange: func(bool) {
					ed.SetDataTableReviewFilter(cd.id)
				},
				Children: []core.Node{
					core.Text{
						Content: cd.label,
						Style: core.Style{
							FontSize: 12.5,
							Color:    fg,
						},
					},
					core.Text{
						TestID:  "dt-review-chip-count-" + string(cd.id),
						Content: strconv.Itoa(cd.count),
						Style: core.Style{
							FontSize: 12,
							Color:    countFg,
						},
					},
				},
			})
		}
		return core.View{
			TestID: "dt-review-chips",
			Role:   core.RoleGroup,
			Label:  ed.T("dtReview.showRows"),
			Style: core.Style{
				Direction: core.Row,
				Wrap:      core.Wrap,
				Gap:       specSpace4,
			},
			Children: nodes,
		}
	})

	return core.View{
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         specSpace8,
			Padding:     core.Symmetric(specSpace12, specSpace16),
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.Line,
			FlexShrink:  0,
		},
		Children: []core.Node{
			chipsView,
			core.View{Style: core.Style{FlexGrow: 1}},
			searchBar,
		},
	}
}

func dtReviewErrorBanner(p theme.Palette, ed *Editor, reviewRev *core.Memo[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = reviewRev.Get()
			return ed.DataTableReviewError() != nil
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				Style: core.Style{
					Padding:    core.Edges{Top: specSpace8, Left: specSpace16, Right: specSpace16},
					FlexShrink: 0,
				},
				Children: []core.Node{
					specStatusBanner(p, "dt-review-error", false, "", func() string {
						_ = reviewRev.Get()
						if err := ed.DataTableReviewError(); err != nil {
							return err.Error()
						}
						return ""
					}, nil),
				},
			}
		},
	}
}

func dtReviewColumnWidth(col DataTableReviewColumn, deltaCount int) core.Style {
	if col.IsPrimary {
		return core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(140)}
	}
	if col.HasDelta {
		w := core.Points(132)
		if deltaCount <= 2 {
			w = 152
		}
		if col.Name == "price" {
			w = 156
		}
		return core.Style{Width: core.Pt(w), FlexShrink: 0}
	}
	if col.Kind == datatable.KindNumber {
		return core.Style{Width: core.Pt(56), FlexShrink: 0}
	}
	return core.Style{Width: core.Pt(96), FlexShrink: 0}
}

func dtReviewTable(cx *core.Scope, p theme.Palette, ed *Editor, reviewRev *core.Memo[uint64], dropHint *core.Signal[dtReviewDropHint]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		f := ed.activeDataTableReviewFile()
		if f == nil {
			return core.View{}
		}
		cols := ed.DataTableReviewColumns()
		records := ed.DataTableReviewVisibleRecords()
		focusKey := ed.DataTableReviewFocusKey()
		editKey, editCol, isEditing := ed.EditingDataTableReviewCell()
		_, sortOrder := ed.DataTableReviewSort()
		canDrag := sortOrder == SortNone && strings.TrimSpace(ed.DataTableReviewSearch()) == "" && ed.DataTableReviewFilter() == DataTableReviewFilterAll

		deltaCount := 0
		for _, c := range cols {
			if c.HasDelta {
				deltaCount++
			}
		}

		// Column headers row.
		headCells := make([]core.Node, 0, len(cols)+2)
		headCells = append(headCells, core.View{
			Role:  core.RoleColumnHeader,
			Label: ed.T("dtReview.colOrder"),
			Style: core.Style{Width: core.Pt(20), FlexShrink: 0},
		})
		for _, col := range cols {
			c := col
			ariaSort := ed.DataTableReviewAriaSort(c.Name)
			sorted := ariaSort != "none"
			fg := p.TextFaint
			if sorted {
				fg = p.TextPrimary
			}
			var arrow core.Node
			if ariaSort == "ascending" {
				arrow = specIcon(lucide.ChevronUp, 11, fg)
			} else if ariaSort == "descending" {
				arrow = specIcon(lucide.ChevronDown, 11, fg)
			}
			btnChildren := []core.Node{
				core.Text{
					Content: c.Label,
					Style: core.Style{
						FontSize:      11,
						FontWeight:    core.FontWeightSemibold,
						LetterSpacing: 0.66,
						Color:         fg,
					},
				},
			}
			if arrow != nil {
				btnChildren = append(btnChildren, arrow)
			}
			colStyle := dtReviewColumnWidth(c, deltaCount)
			colStyle.Direction = core.Row
			colStyle.AlignItems = core.AlignCenter
			var sortOrderLabel string
			switch ariaSort {
			case "ascending":
				sortOrderLabel = ed.T("dtReview.sortAsc")
			case "descending":
				sortOrderLabel = ed.T("dtReview.sortDesc")
			default:
				sortOrderLabel = ed.T("dtReview.sortNone")
			}
			headCells = append(headCells, core.View{
				TestID:                   "dt-review-colhead-" + c.Name,
				Role:                     core.RoleColumnHeader,
				Label:                    c.Label,
				AccessibilityDescription: "aria-sort=" + ariaSort,
				Style:                    colStyle,
				Children: []core.Node{
					base.Button{
						TestID: "dt-review-sort-" + c.Name,
						Label:  ed.T("dtReview.colSortLabel", l10n.Param("col", c.Label), l10n.Param("order", sortOrderLabel)),
						Style: core.Style{
							Height:       core.Pt(24),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          4,
							Padding:      core.Symmetric(0, 4),
							CornerRadius: specRadiusControl,
							Cursor:       core.CursorPointer,
							Hover:        &core.Style{Background: p.Raised},
							FocusVisible: p.FocusRing(-2),
						},
						OnPress: func(core.PressEvent) {
							ed.CycleDataTableReviewSort(c.Name)
						},
						Children: btnChildren,
					},
				},
			})
		}
		headCells = append(headCells, core.View{
			Role:  core.RoleColumnHeader,
			Label: ed.T("dtReview.colAction"),
			Style: core.Style{Width: core.Pt(76), FlexShrink: 0},
		})

		headerRow := core.View{
			TestID: "dt-review-table-header",
			Role:   core.RoleRow,
			Style: core.Style{
				Height:      core.Pt(32),
				FlexShrink:  0,
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         specSpace8,
				Padding:     core.Symmetric(0, specSpace16),
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
			},
			Children: headCells,
		}

		// Data rows.
		rowNodes := make([]core.Node, 0, max(1, len(records)))
		if len(records) == 0 {
			rowNodes = append(rowNodes, core.View{
				TestID: "dt-review-no-match",
				Style:  core.Style{Padding: core.Symmetric(specSpace24, specSpace16)},
				Children: []core.Node{
					core.Text{
						Content: ed.T("dtReview.noMatch"),
						Style:   core.Style{FontSize: 12.5, Color: p.TextFaint},
					},
				},
			})
		} else {
			totalRows := len(f.records)
			for _, rec := range records {
				rowNodes = append(rowNodes, dtReviewDataRow(cx, p, ed, rec, cols, deltaCount, focusKey == rec.Key, isEditing && editKey == rec.Key, editCol, canDrag, totalRows, dropHint))
			}
		}

		return core.View{
			TestID: "dt-review-table",
			Role:   core.RoleTable,
			Label:  f.relPath,
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				Overflow:  core.Hidden,
			},
			Children: []core.Node{
				headerRow,
				ui.ScrollView{
					TestID: "dt-review-scroll",
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
					},
					Children: []core.Node{
						dragdrop.Area{
							Children: rowNodes,
						},
					},
				},
			},
		}
	})
}

func dtReviewKindDotColor(p theme.Palette, kind string) core.Color {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "weapon":
		return p.Changed.Fg
	case "armor":
		return p.Accent.Fg
	case "potion":
		return p.Removed.Fg
	case "trinket":
		return p.Agent.Fg
	case "key":
		return p.Warning.Fg
	default:
		return p.TextMuted
	}
}

func dtReviewDataRow(
	cx *core.Scope,
	p theme.Palette,
	ed *Editor,
	rec DataTableReviewRecord,
	cols []DataTableReviewColumn,
	deltaCount int,
	focused bool,
	editingRow bool,
	editCol string,
	canDrag bool,
	totalRows int,
	dropHint *core.Signal[dtReviewDropHint],
) core.Node {
	recKey := rec.Key
	recOrder := rec.FileOrder
	rowH := core.Points(36)

	// Drag grip cell.
	gripBtn := base.Button{
		TestID:  "dt-review-drag-" + recKey,
		Label:   ed.T("dtReview.dragRowLabel", l10n.Param("name", rec.DisplayName), l10n.Param("row", recOrder+1), l10n.Param("total", totalRows)),
		Tooltip: specTooltipNode(p, ed.T("dtReview.dragToReorder")),
		Style: core.Style{
			Width:          core.Pt(20),
			Height:         core.Pt(specRowHeight),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			CornerRadius:   4,
			Cursor:         core.CursorGrab,
			FocusVisible:   p.FocusRing(-2),
		},
		OnPress: func(core.PressEvent) {
			ed.FocusDataTableReviewRecord(recKey)
		},
		Children: []core.Node{
			specIcon(lucide.GripVertical, 12, p.TextFaint),
		},
	}

	gripCell := core.View{
		Role:  core.RoleCell,
		Style: core.Style{Width: core.Pt(20), FlexShrink: 0},
		Children: []core.Node{
			dragdrop.Source{
				View: core.View{Children: []core.Node{gripBtn}},
				Data: func() any {
					if !canDrag {
						return nil
					}
					return dtReviewRowDrag{key: recKey}
				},
				Preview: func(any) core.Node {
					return core.View{
						Style: core.Style{
							Padding:      core.Symmetric(4, 10),
							CornerRadius: specRadiusControl,
							Background:   p.Raised,
							BorderWidth:  1,
							BorderColor:  p.Accent.Edge,
						},
						Children: []core.Node{
							core.Text{
								Content: rec.DisplayName,
								Style:   core.Style{FontSize: 12, Color: p.TextPrimary},
							},
						},
					}
				},
			},
		},
	}

	cells := make([]core.Node, 0, len(cols)+2)
	cells = append(cells, gripCell)

	for _, col := range cols {
		c := col
		diff := rec.Cells[c.Name]
		colStyle := dtReviewColumnWidth(c, deltaCount)
		colStyle.Direction = core.Row
		colStyle.AlignItems = core.AlignCenter

		if c.IsPrimary {
			nameStyle := core.Style{
				FontSize:   12.5,
				Color:      p.TextPrimary,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
			}
			if rec.Changed && rec.Reverted {
				nameStyle.TextDecoration = core.TextDecorationLineThrough
			}
			primaryChildren := make([]core.Node, 0, 3)
			if rec.KindTag != "" {
				primaryChildren = append(primaryChildren, core.View{
					Style: core.Style{
						Width:        core.Pt(7),
						Height:       core.Pt(7),
						CornerRadius: 2,
						Background:   dtReviewKindDotColor(p, rec.KindTag),
						FlexShrink:   0,
					},
				})
			}
			primaryChildren = append(primaryChildren, core.Text{
				TestID:   "dt-review-name-" + recKey,
				Content:  rec.DisplayName,
				MaxLines: 1,
				Style:    nameStyle,
			})
			if rec.KindTag != "" {
				primaryChildren = append(primaryChildren, core.Text{
					Content: rec.KindTag,
					Style: core.Style{
						FontSize:   11.5,
						Color:      p.TextFaint,
						FlexShrink: 0,
					},
				})
			}
			cells = append(cells, core.View{
				Role:  core.RoleCell,
				Style: colStyle,
				Children: []core.Node{
					base.Button{
						TestID: "dt-review-focus-" + recKey,
						Label:  rec.DisplayName,
						Style: core.Style{
							FlexGrow:     1,
							MinWidth:     core.Pt(0),
							Height:       core.Pt(specRowHeight),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          specSpace8,
							CornerRadius: 4,
							Cursor:       core.CursorPointer,
							FocusVisible: p.FocusRing(-2),
						},
						OnPress: func(core.PressEvent) {
							ed.FocusDataTableReviewRecord(recKey)
						},
						Children: primaryChildren,
					},
				},
			})
			continue
		}

		// Editing this specific cell inline.
		if editingRow && editCol == c.Name && !rec.Reverted {
			cells = append(cells, core.View{
				Role:     core.RoleCell,
				Style:    colStyle,
				Children: []core.Node{dtReviewInlineCellEditor(cx, p, ed, recKey, c, diff)},
			})
			continue
		}

		cells = append(cells, core.View{
			Role:     core.RoleCell,
			Style:    colStyle,
			Children: []core.Node{dtReviewValueCell(p, ed, rec, c, diff)},
		})
	}

	// Action cell: Revert / Restore for changed records.
	var actionChild []core.Node
	if rec.Changed {
		toggleText := ed.T("dtReview.revert")
		toggleFg := p.TextSecondary
		if rec.Reverted {
			toggleText = ed.T("dtReview.restore")
			toggleFg = p.Accent.Fg
		}
		actionChild = append(actionChild, base.Button{
			TestID: "dt-review-toggle-" + recKey,
			Label:  toggleText + " " + rec.DisplayName,
			Style: core.Style{
				Height:         core.Pt(26),
				MinHeight:      core.Pt(26),
				Padding:        core.Symmetric(0, specSpace8),
				CornerRadius:   specRadiusControl,
				Background:     p.Raised,
				BorderWidth:    1,
				BorderColor:    p.LineRaised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Cursor:         core.CursorPointer,
				Hover:          &core.Style{Background: p.LineRaised},
				FocusVisible:   p.FocusRing(-2),
			},
			OnPress: func(core.PressEvent) {
				ed.report(ed.ToggleDataTableReviewRecord(recKey))
			},
			Children: []core.Node{
				core.Text{
					TestID:  "dt-review-toggle-text-" + recKey,
					Content: toggleText,
					Style: core.Style{
						FontSize: 11.5,
						Color:    toggleFg,
					},
				},
			},
		})
	}
	cells = append(cells, core.View{
		Role: core.RoleCell,
		Style: core.Style{
			Width:          core.Pt(76),
			FlexShrink:     0,
			Direction:      core.Row,
			JustifyContent: core.JustifyEnd,
			AlignItems:     core.AlignCenter,
		},
		Children: actionChild,
	})

	// Dim reverted data cells without reducing the Restore button's WCAG contrast.
	if rec.Changed && rec.Reverted {
		dataCells := cells[:len(cells)-1]
		actionCell := cells[len(cells)-1]
		cells = []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace8,
					FlexGrow:   1,
					MinWidth:   core.Pt(0),
					Opacity:    core.Alpha(0.55),
				},
				Children: dataCells,
			},
			actionCell,
		}
	}

	rowRef := &core.Ref{}
	rowView := core.View{
		Ref:    rowRef,
		TestID: "dt-review-row-" + recKey,
		Role:   core.RoleRow,
		StyleFunc: func() core.Style {
			h := dropHint.Get()
			bg := core.Color(0)
			leftW := core.Points(0)
			leftCol := p.Line
			if focused {
				bg = p.Raised
				leftW = 2
				leftCol = p.Accent.Fg
			}
			topW := core.Points(0)
			topCol := p.Line
			botW := core.Points(1)
			botCol := p.Line
			if h.on && h.key == recKey {
				if h.before {
					topW = 2
					topCol = p.Accent.Fg
				} else {
					botW = 2
					botCol = p.Accent.Fg
				}
			}
			return core.Style{
				Height:       core.Pt(rowH),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          specSpace8,
				Padding:      core.Symmetric(0, specSpace16),
				Background:   bg,
				Borders:      core.Edges{Top: topW, Bottom: botW, Left: leftW},
				BorderColors: core.EdgeColors{Top: topCol, Bottom: botCol, Left: leftCol},
			}
		},
		Children: cells,
	}

	before := func(ev dragdrop.Event) bool {
		if b, ok := rowRef.Bounds(); ok {
			return ev.Position.Y < b.Size.Height/2
		}
		return ev.Position.Y < rowH/2
	}
	track := func(ev dragdrop.Event) {
		dropHint.Set(dtReviewDropHint{key: recKey, before: before(ev), on: true})
	}
	return dragdrop.Target{
		View: rowView,
		Accept: func(data any) bool {
			if !canDrag {
				return false
			}
			d, ok := data.(dtReviewRowDrag)
			return ok && d.key != recKey
		},
		OnEnter: track,
		OnMove:  track,
		OnLeave: func(dragdrop.Event) { dropHint.Set(dtReviewDropHint{}) },
		OnDrop: func(ev dragdrop.Event) {
			dropHint.Set(dtReviewDropHint{})
			d, ok := ev.Data.(dtReviewRowDrag)
			if !ok {
				return
			}
			target := recOrder
			if !before(ev) {
				target = recOrder + 1
			}
			// Adjust target when dragging from an earlier index.
			f := ed.activeDataTableReviewFile()
			if f != nil {
				for i, r := range f.records {
					if r.key == d.key {
						if i < target {
							target--
						}
						break
					}
				}
			}
			ed.report(ed.ReorderDataTableReviewRecord(d.key, target))
		},
	}
}

func dtReviewValueCell(p theme.Palette, ed *Editor, rec DataTableReviewRecord, col DataTableReviewColumn, diff DataTableReviewCellDiff) core.Node {
	recKey := rec.Key
	colName := col.Name
	showPill := !rec.Reverted && (diff.Changed || diff.UserEdited)
	nowText := "—"
	if diff.HasCurrent {
		nowText = FormatReviewCellValue(diff.Current)
	}

	var children []core.Node
	label := fmt.Sprintf("%s %s %s", rec.DisplayName, colName, nowText)
	if showPill && diff.HasBase && diff.Changed {
		oldText := FormatReviewCellValue(diff.Base)
		deltaText := ""
		if diff.HasDeltaPct {
			deltaText = FormatReviewDeltaPct(diff.DeltaPct)
		}
		label = ed.T("dtReview.cellChangedLabel", l10n.Param("name", rec.DisplayName), l10n.Param("col", colName), l10n.Param("old", oldText), l10n.Param("new", nowText))
		if deltaText != "" {
			label += " (" + deltaText + ")"
		}
		children = append(children,
			core.Text{
				Content: oldText,
				Style: core.Style{
					FontFamily:     core.FontMonospace,
					FontSize:       11.5,
					Color:          p.TextFaint,
					TextDecoration: core.TextDecorationLineThrough,
				},
			},
			core.Text{
				Content: "→",
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      p.TextFaint,
				},
			},
			core.Text{
				TestID:  fmt.Sprintf("dt-review-cell-val-%s-%s", recKey, colName),
				Content: nowText,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      p.TextPrimary,
				},
			},
		)
		if deltaText != "" {
			children = append(children, core.Text{
				TestID:  fmt.Sprintf("dt-review-cell-delta-%s-%s", recKey, colName),
				Content: deltaText,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11,
					Color:      p.Changed.Fg,
				},
			})
		}
	} else {
		fg := p.TextMuted
		if showPill {
			fg = p.TextPrimary
		}
		children = append(children, core.Text{
			TestID:  fmt.Sprintf("dt-review-cell-val-%s-%s", recKey, colName),
			Content: nowText,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      fg,
			},
		})
	}

	style := core.Style{
		Height:       core.Pt(26),
		Direction:    core.Row,
		AlignItems:   core.AlignCenter,
		Gap:          5,
		Padding:      core.Symmetric(0, 7),
		CornerRadius: specRadiusControl,
		Cursor:       core.CursorDefault,
		FocusVisible: p.FocusRing(-2),
	}
	if !rec.Reverted && !rec.Removed {
		style.Cursor = core.CursorText
	}
	if showPill {
		style.Background = p.Changed.Fill
		style.BorderWidth = 1.5
		if rec.Removed {
			style.Background = p.Removed.Fill
			style.BorderColor = p.Removed.Edge
			style.BorderStyle = core.BorderDashed
		} else if diff.UserEdited {
			style.BorderColor = p.Accent.Edge
			style.BorderStyle = core.BorderSolid
		} else {
			style.BorderColor = p.Changed.Edge
			style.BorderStyle = core.BorderDashed
		}
	}

	return base.Button{
		TestID: fmt.Sprintf("dt-review-cell-%s-%s", recKey, colName),
		Label:  label,
		Style:  style,
		OnPress: func(core.PressEvent) {
			if rec.Reverted || rec.Removed {
				ed.FocusDataTableReviewRecord(recKey)
				return
			}
			ed.StartDataTableReviewCellEdit(recKey, colName)
		},
		Children: children,
	}
}

func dtReviewInlineCellEditor(cx *core.Scope, p theme.Palette, ed *Editor, recKey string, col DataTableReviewColumn, diff DataTableReviewCellDiff) core.Node {
	initial := ed.DataTableReviewEditDraft()
	if initial == "" && diff.HasCurrent {
		initial = diff.Current.Display()
	}
	draft := core.NewSignal(cx, initial)
	isNum := col.Kind == datatable.KindNumber
	step := float64(5)
	if isNum && diff.HasCurrent {
		if _, isInt := diff.Current.Int64(); !isInt {
			step = 0.5
		} else if iv, _ := diff.Current.Int64(); iv < 20 {
			step = 1
		}
	}

	commit := func(text string) {
		if err := ed.SetDataTableReviewCellText(recKey, col.Name, text); err == nil {
			ed.FinishDataTableReviewCellEdit()
		}
	}

	var children []core.Node
	if isNum {
		children = append(children, base.Button{
			TestID: "dt-review-edit-dec",
			Label:  ed.T("dtReview.lowerCellLabel", l10n.Param("row", recKey), l10n.Param("col", col.Name)),
			Style: core.Style{
				Width:          core.Pt(22),
				Height:         core.Pt(22),
				CornerRadius:   4,
				Background:     p.Raised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Cursor:         core.CursorPointer,
				FocusVisible:   p.FocusRing(-2),
			},
			OnPress: func(core.PressEvent) {
				if err := ed.StepDataTableReviewCellNumber(recKey, col.Name, -step); err == nil {
					if f := ed.activeDataTableReviewFile(); f != nil {
						if rec, ok := f.recordByKey[recKey]; ok {
							d := f.cellDiff(rec, col.Name)
							if d.HasCurrent {
								next := d.Current.Display()
								ed.SetDataTableReviewEditDraft(next)
								draft.Set(next)
							}
						}
					}
				}
			},
			Children: []core.Node{specIcon(lucide.Minus, 10, p.TextSecondary)},
		})
	}

	children = append(children, core.View{
		Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(32)},
		Children: []core.Node{
			ui.Input{
				TestID: "dt-review-edit-input",
				Label:  ed.T("dtReview.editCellLabel", l10n.Param("row", recKey), l10n.Param("col", col.Name)),
				Style: core.Style{
					Height:      core.Pt(22),
					FontFamily:  core.FontMonospace,
					FontSize:    11.5,
					Color:       p.TextPrimary,
					Background:  core.Color(0),
					BorderWidth: 0,
					CaretColor:  p.Accent.Edge,
				},
				Value: draft.Get,
				OnChange: func(text string) {
					ed.SetDataTableReviewEditDraft(text)
					draft.Set(text)
				},
				OnSubmit: commit,
			},
		},
	})

	if isNum {
		children = append(children, base.Button{
			TestID: "dt-review-edit-inc",
			Label:  ed.T("dtReview.raiseCellLabel", l10n.Param("row", recKey), l10n.Param("col", col.Name)),
			Style: core.Style{
				Width:          core.Pt(22),
				Height:         core.Pt(22),
				CornerRadius:   4,
				Background:     p.Raised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Cursor:         core.CursorPointer,
				FocusVisible:   p.FocusRing(-2),
			},
			OnPress: func(core.PressEvent) {
				if err := ed.StepDataTableReviewCellNumber(recKey, col.Name, step); err == nil {
					if f := ed.activeDataTableReviewFile(); f != nil {
						if rec, ok := f.recordByKey[recKey]; ok {
							d := f.cellDiff(rec, col.Name)
							if d.HasCurrent {
								next := d.Current.Display()
								ed.SetDataTableReviewEditDraft(next)
								draft.Set(next)
							}
						}
					}
				}
			},
			Children: []core.Node{specIcon(lucide.Plus, 10, p.TextSecondary)},
		})
	}

	children = append(children, base.Button{
		TestID: "dt-review-edit-done",
		Label:  ed.T("dtReview.done"),
		Style: core.Style{
			Width:          core.Pt(22),
			Height:         core.Pt(22),
			CornerRadius:   4,
			Background:     p.Accent.Fg,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Cursor:         core.CursorPointer,
			FocusVisible:   p.FocusRing(-2),
		},
		OnPress: func(core.PressEvent) {
			commit(draft.Get())
		},
		Children: []core.Node{specIcon(lucide.Check, 10, p.Accent.OnFill)},
	})

	return core.View{
		TestID: "dt-review-inline-editor",
		Style: core.Style{
			Height:       core.Pt(26),
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          2,
			Padding:      core.Symmetric(0, 2),
			CornerRadius: specRadiusControl,
			Background:   p.Bg,
			BorderWidth:  1.5,
			BorderColor:  p.Accent.Edge,
			FlexGrow:     1,
		},
		Children: children,
	}
}

func dtReviewRightYAML(cx *core.Scope, p theme.Palette, ed *Editor, reviewRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		focusTitle, lines := ed.DataTableReviewFocusedYAML()

		lineNodes := make([]core.Node, 0, len(lines))
		for i, ln := range lines {
			bg := core.Color(0)
			fg := p.TextFaint
			switch ln.Sign {
			case "+":
				bg = p.Added.Fill
				fg = p.Added.Fg
			case "-":
				bg = p.Removed.Fill
				fg = p.Removed.Fg
			}
			rowChildren := []core.Node{
				core.Text{
					Content: strconv.Itoa(ln.Line),
					Style: core.Style{
						Width:       core.Pt(32),
						FlexShrink:  0,
						TextAlign:   core.TextAlignEnd,
						FontFamily:  core.FontMonospace,
						FontSize:    11.5,
						FontVariant: core.FontTabularNums,
						Color:       p.TextFaint,
					},
				},
				core.Text{
					Content: ln.Sign,
					Style: core.Style{
						Width:      core.Pt(14),
						FlexShrink: 0,
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      fg,
					},
				},
				core.Text{
					Content: ln.Text,
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      fg,
					},
				},
			}
			if ln.Tag != "" {
				rowChildren = append(rowChildren, core.Text{
					TestID:  fmt.Sprintf("dt-review-yaml-tag-%d", i),
					Content: ln.Tag,
					Style: core.Style{
						FontSize:   11,
						Color:      p.TextFaint,
						FlexShrink: 0,
					},
				})
			}
			lineNodes = append(lineNodes, core.View{
				TestID: fmt.Sprintf("dt-review-yaml-line-%d", i),
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        6,
					MinHeight:  core.Pt(22),
					Padding:    core.Edges{Left: specSpace8, Right: specSpace16},
					Background: bg,
				},
				Children: rowChildren,
			})
		}

		return core.View{
			TestID: "dt-review-yaml-pane",
			Style: core.Style{
				Width:       core.Pt(360),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
			},
			Children: []core.Node{
				core.View{
					TestID: "dt-review-yaml-header",
					Style: core.Style{
						Height:      core.Pt(specHeaderHeight),
						FlexShrink:  0,
						Direction:   core.Row,
						AlignItems:  core.AlignCenter,
						Gap:         6,
						Padding:     core.Symmetric(0, specSpace16),
						Borders:     core.Edges{Bottom: 1},
						BorderColor: p.Line,
					},
					Children: []core.Node{
						core.Text{
							TestID:  "dt-review-yaml-focus",
							Content: focusTitle,
							Style: core.Style{
								FontSize:   12.5,
								FontWeight: core.FontWeightMedium,
								Color:      p.TextPrimary,
							},
						},
						core.Text{
							Content: "· YAML",
							Style: core.Style{
								FontSize: 12.5,
								Color:    p.TextMuted,
							},
						},
					},
				},
				ui.ScrollView{
					TestID: "dt-review-yaml-lines",
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
					},
					ContentStyle: core.Style{
						Padding: core.Symmetric(specSpace12, 0),
					},
					Children: lineNodes,
				},
				core.View{
					Style: core.Style{
						Padding:     core.Symmetric(specSpace12, specSpace16),
						Borders:     core.Edges{Top: 1},
						BorderColor: p.Line,
						Gap:         specSpace4,
						FlexShrink:  0,
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("dtReview.hintEdit"),
							Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
						},
						core.Text{
							Content: ed.T("dtReview.hintJoin"),
							Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
						},
					},
				},
			},
		}
	})
}

// agentPendingDataTableReviewCard renders the pending Data Table review card inside AgentPanelView
// so the person can resume a paused review from the Agent panel.
func agentPendingDataTableReviewCard(ed *Editor, p theme.Palette) core.Node {
	snap := ed.DataTableReviewSnapshot()
	title := ed.T("dtReview.agentCardTitle",
		l10n.Param("changes", snap.TotalChanged),
		l10n.Param("files", len(snap.Files)),
	)
	var children []core.Node
	children = append(children, core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        specSpace8,
		},
		Children: []core.Node{
			specIcon(lucide.Table2, 14, p.Accent.Fg),
			core.Text{
				TestID:  "agent-dt-review-title",
				Content: title,
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: core.FontWeightSemibold,
					Color:      p.TextPrimary,
					FlexGrow:   1,
				},
			},
		},
	})
	if snap.Prompt != "" {
		children = append(children, core.Text{
			Content:  "“" + snap.Prompt + "”",
			MaxLines: 2,
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextMuted,
			},
		})
	}
	if snap.ChecksOutOfDate {
		children = append(children, core.Text{
			Content: ed.T("dtReview.outOfDateHint"),
			Style: core.Style{
				FontSize: 11.5,
				Color:    p.Warning.Fg,
			},
		})
	}
	children = append(children, core.View{
		Style: core.Style{
			Direction: core.Row,
			Gap:       specSpace8,
		},
		Children: []core.Node{
			specPanelButton(p, specPanelButtonSpec{
				TestID:  "agent-dt-review-resume",
				Text:    ed.T("dtReview.resumeReview"),
				Label:   ed.T("dtReview.resumeReview"),
				Variant: specBtnAccent,
				Compact: true,
				OnPress: func() {
					ed.ResumeDataTableReview()
				},
			}),
		},
	})
	return core.View{
		TestID: "agent-dt-review-card",
		Style: core.Style{
			CornerRadius: specRadiusCard,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(specSpace12),
			Gap:          specSpace8,
		},
		Children: children,
	}
}
