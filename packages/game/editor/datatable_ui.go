package editor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// DataTablePanelView renders the Data Table editor below the viewport while a YAML data
// table is open: table path and row-type badge, filter input, Add Row / Save Table / Close
// buttons, validation error banner, sortable column headers with inferred kinds, and a
// scrollable grid of rows with move up/down, delete, and kind-aware cell editors.
func DataTablePanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.table != nil
		},
		Then: func(cx *core.Scope) core.Node { return dataTablePanel(cx, ed, rev) },
	}
}

func dataTablePanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	saveShortcut := FormatShortcut(ed.EffectiveShortcut(CmdSave), cx.Env().OS())

	// structure changes when columns, column kinds, visible row order, filter emptiness, loading state, theme appearance, or the open table path change,
	// so editing a cell's value in place does not remount inputs and steal focus.
	structure := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.table == nil || ed.table.doc == nil {
			return ""
		}
		var b strings.Builder
		b.WriteString(ed.table.path)
		if cx.Dark() {
			b.WriteString("|D")
		} else {
			b.WriteString("|L")
		}
		if ed.table.loading {
			b.WriteString("|loading")
		}
		if strings.TrimSpace(ed.DataTableFilter()) != "" {
			b.WriteString("|filtered")
		}
		b.WriteByte('|')
		for _, c := range ed.DataTableColumns() {
			b.WriteString(c.Name)
			b.WriteByte(':')
			b.WriteString(c.Kind.String())
			b.WriteByte(',')
		}
		b.WriteByte('|')
		for _, r := range ed.DataTableViewRows() {
			b.WriteString(strconv.Itoa(r))
			b.WriteByte(',')
		}
		return b.String()
	})

	header := specPanelHeader(p,
		specPanelTitle(p, "", lucide.Table2, "Data Table", nil),
		core.Text{
			TestID: "datatable-path",
			ContentFunc: func() string {
				_ = rev.Get()
				path := ed.DataTablePath()
				if path == "" {
					return "Untitled"
				}
				name := filepath.Base(path)
				if rt := ed.DataTableRowType(); rt != nil {
					return name + " (" + rt.Name() + ")"
				}
				return name
			},
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      p.TextMuted,
			},
		},
		core.View{
			Style: core.Style{Width: core.Pt(188)},
			Children: []core.Node{ui.Input{
				TestID:      "datatable-filter",
				Label:       "Filter rows",
				Placeholder: "Filter rows…",
				Style:       specInputStyle(p),
				Value: func() string {
					_ = rev.Get()
					return ed.DataTableFilter()
				},
				OnChange: func(text string) {
					ed.SetDataTableFilter(text)
				},
			}},
		},
		core.View{Style: core.Style{FlexGrow: 1}},
		specPanelButton(p, specPanelButtonSpec{
			TestID:   "datatable-add-row",
			Text:     "Add Row",
			Icon:     lucide.Plus,
			Label:    "Add Row",
			Tooltip:  "Add a new row to the data table",
			Variant:  specBtnSecondary,
			Compact:  true,
			Disabled: disabled,
			OnPress: func() {
				_, _ = ed.AddDataTableRow()
			},
		}),
		specPanelButton(p, specPanelButtonSpec{
			TestID:   "datatable-save",
			Text:     "Save Table",
			Icon:     lucide.Save,
			Label:    "Save Table",
			Tooltip:  specShortcutTip("Save Table", saveShortcut),
			Variant:  specBtnQuiet,
			Compact:  true,
			Disabled: disabled,
			OnPress: func() {
				ed.report(ed.saveDataTableFromPanel())
			},
		}),
		specPanelButton(p, specPanelButtonSpec{
			TestID:   "datatable-close",
			Icon:     lucide.X,
			Label:    "Close Data Table",
			Tooltip:  specShortcutTip("Close Data Table", "Esc"),
			Variant:  specBtnGhost,
			Compact:  true,
			Disabled: disabled,
			OnPress: func() {
				ed.report(ed.CloseDataTable())
			},
		}),
	)

	errorBanner := core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.DataTableError() != nil
		},
		Then: func(cx *core.Scope) core.Node {
			p := ed.SpecPalette(cx)
			dismissBtn := specPanelButton(p, specPanelButtonSpec{
				TestID:  "datatable-error-dismiss",
				Icon:    lucide.X,
				Label:   "Dismiss data table error",
				Tooltip: "Dismiss error",
				Variant: specBtnGhost,
				Compact: true,
				OnPress: ed.ClearDataTableError,
			})
			return core.View{
				Style: core.Style{Padding: core.Edges{Top: specSpace8, Left: specSpace16, Right: specSpace16}, FlexShrink: 0},
				Children: []core.Node{
					specStatusBanner(p, "datatable-error", false, "", func() string {
						_ = rev.Get()
						if err := ed.DataTableError(); err != nil {
							return err.Error()
						}
						return ""
					}, dismissBtn),
				},
			}
		},
	}

	grid := ui.ScrollView{
		Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
		ContentStyle: core.Style{
			Padding: core.Symmetric(specSpace8, specSpace16),
			Gap:     specSpace8,
		},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			if structure.Get() == "" || ed.table == nil || ed.table.doc == nil {
				return nil
			}
			p := ed.SpecPalette(cx)
			if ed.DataTableLoading() {
				return specLoadingState(p, "datatable-loading", "Loading data table…")
			}
			cols := ed.DataTableColumns()
			viewRows := ed.DataTableViewRows()
			rows := make([]core.Node, 0, len(viewRows)+1)
			rows = append(rows, dataTableHeaderRow(cx, p, ed, rev, cols))
			if len(viewRows) == 0 {
				if strings.TrimSpace(ed.DataTableFilter()) != "" {
					clearBtn := specPanelButton(p, specPanelButtonSpec{
						TestID:     "datatable-clear-filter",
						Text:       "Clear Filter",
						Icon:       lucide.X,
						Label:      "Clear row filter",
						Tooltip:    "Clear row filter",
						Variant:    specBtnQuiet,
						Compact:    true,
						InsetFocus: true,
						OnPress: func() {
							ed.SetDataTableFilter("")
						},
					})
					rows = append(rows, specEmptyState(p, "datatable-empty", lucide.SearchX, p.TextFaint, "No matching rows", "Clear the filter to show all rows.", clearBtn))
				} else {
					addBtn := specPanelButton(p, specPanelButtonSpec{
						TestID:     "datatable-empty-add",
						Text:       "Add Row",
						Icon:       lucide.Plus,
						Label:      "Add first row",
						Tooltip:    "Add a new row to the data table",
						Variant:    specBtnSecondary,
						Compact:    true,
						InsetFocus: true,
						Disabled:   disabled,
						OnPress: func() {
							_, _ = ed.AddDataTableRow()
						},
					})
					rows = append(rows, specEmptyState(p, "datatable-empty", lucide.Table, p.TextFaint, "No rows yet", "Add a row to begin editing this data table.", addBtn))
				}
			} else {
				for _, docRow := range viewRows {
					rows = append(rows, dataTableDataRow(cx, p, ed, rev, docRow, cols, disabled))
				}
			}
			return core.View{TestID: "datatable-grid", Style: core.Style{Gap: specSpace4}, Children: rows}
		})},
	}

	return specPanelContainer(p, "datatable-panel", "Data Table", 256, header, errorBanner, grid)
}

func dataTableHeaderRow(cx *core.Scope, p SpecPalette, ed *Editor, rev *core.Signal[uint64], cols []DataTableColumn) core.Node {
	cells := make([]core.Node, 0, len(cols)+1)
	cells = append(cells, core.View{
		Style: core.Style{Width: core.Pt(120), JustifyContent: core.JustifyCenter},
		Children: []core.Node{
			specSectionLabel(p, "ROW"),
		},
	})
	for _, col := range cols {
		c := col
		kindTag := ""
		if c.Kind != datatable.KindNull {
			kindTag = " [" + c.Kind.String() + "]"
		}
		// Memoize this column's sort order so unrelated rev ticks do not remount its icon.
		colOrder := core.NewMemo(cx, func() SortOrder {
			_ = rev.Get()
			sortCol, order := ed.DataTableSort()
			if sortCol == c.Name {
				return order
			}
			return SortNone
		})
		cells = append(cells, core.View{
			Style: core.Style{Width: core.Pt(144)},
			Children: []core.Node{
				specPanelButton(p, specPanelButtonSpec{
					TestID: "datatable-col-" + c.Name,
					Text:   c.Name + kindTag,
					IconFunc: func() *core.SVG {
						switch colOrder.Get() {
						case SortAsc:
							return lucide.ArrowUp()
						case SortDesc:
							return lucide.ArrowDown()
						default:
							return lucide.ArrowUpDown()
						}
					},
					LabelFunc: func() string {
						if order := colOrder.Get(); order != SortNone {
							return fmt.Sprintf("Sort by %s (currently %s)", c.Name, order.String())
						}
						return "Sort by " + c.Name
					},
					Tooltip:    "Sort by " + c.Name,
					Variant:    specBtnGhost,
					Compact:    true,
					InsetFocus: true,
					Style: core.Style{
						Width:          core.Pct(100),
						JustifyContent: core.JustifyStart,
						Padding:        core.Symmetric(0, specSpace8),
					},
					OnPress: func() {
						sortCol, order := ed.DataTableSort()
						if sortCol != c.Name {
							ed.SetDataTableSort(c.Name, SortAsc)
							return
						}
						switch order {
						case SortAsc:
							ed.SetDataTableSort(c.Name, SortDesc)
						case SortDesc:
							ed.SetDataTableSort("", SortNone)
						default:
							ed.SetDataTableSort(c.Name, SortAsc)
						}
					},
				}),
			},
		})
	}
	return core.View{
		TestID: "datatable-header",
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         specSpace8,
			Padding:     core.Edges{Bottom: specSpace4},
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.Line,
		},
		Children: cells,
	}
}

func dataTableDataRow(cx *core.Scope, p SpecPalette, ed *Editor, rev *core.Signal[uint64], docRow int, cols []DataTableColumn, disabled func() bool) core.Node {
	canReorder := func() bool {
		_ = rev.Get()
		if ed.Playing() || ed.table == nil || ed.table.doc == nil {
			return false
		}
		_, order := ed.DataTableSort()
		return order == SortNone && strings.TrimSpace(ed.DataTableFilter()) == ""
	}

	controls := core.View{
		Style: core.Style{
			Direction:  core.Row,
			Gap:        specSpace4,
			Width:      core.Pt(120),
			AlignItems: core.AlignCenter,
		},
		Children: []core.Node{
			core.Text{
				TestID:  fmt.Sprintf("datatable-row-%d-index", docRow),
				Content: strconv.Itoa(docRow + 1),
				Style: core.Style{
					Width:       core.Pt(24),
					FontFamily:  core.FontMonospace,
					FontSize:    11.5,
					FontVariant: core.FontTabularNums,
					Color:       p.TextFaint,
				},
			},
			specPanelButton(p, specPanelButtonSpec{
				TestID:     fmt.Sprintf("datatable-row-%d-up", docRow),
				Icon:       lucide.ChevronUp,
				Label:      fmt.Sprintf("Move row %d up", docRow+1),
				Tooltip:    fmt.Sprintf("Move row %d up", docRow+1),
				Variant:    specBtnGhost,
				Compact:    true,
				InsetFocus: true,
				Disabled: func() bool {
					return !canReorder() || docRow <= 0
				},
				OnPress: func() {
					_ = ed.MoveDataTableRow(docRow, docRow-1)
				},
			}),
			specPanelButton(p, specPanelButtonSpec{
				TestID:     fmt.Sprintf("datatable-row-%d-down", docRow),
				Icon:       lucide.ChevronDown,
				Label:      fmt.Sprintf("Move row %d down", docRow+1),
				Tooltip:    fmt.Sprintf("Move row %d down", docRow+1),
				Variant:    specBtnGhost,
				Compact:    true,
				InsetFocus: true,
				Disabled: func() bool {
					if !canReorder() || ed.table == nil || ed.table.doc == nil {
						return true
					}
					return docRow >= ed.table.doc.RowCount()-1
				},
				OnPress: func() {
					_ = ed.MoveDataTableRow(docRow, docRow+1)
				},
			}),
			specPanelButton(p, specPanelButtonSpec{
				TestID:     fmt.Sprintf("datatable-row-%d-delete", docRow),
				Icon:       lucide.Trash,
				Label:      fmt.Sprintf("Delete row %d", docRow+1),
				Tooltip:    fmt.Sprintf("Delete row %d", docRow+1),
				Variant:    specBtnGhost,
				Compact:    true,
				InsetFocus: true,
				Disabled:   disabled,
				OnPress: func() {
					_ = ed.DeleteDataTableRow(docRow)
				},
			}),
		},
	}

	cells := make([]core.Node, 0, len(cols)+1)
	cells = append(cells, controls)
	for _, col := range cols {
		cells = append(cells, core.View{
			Style:    core.Style{Width: core.Pt(144)},
			Children: []core.Node{dataTableCellControl(cx, p, ed, rev, docRow, col, disabled)},
		})
	}
	return core.View{
		TestID: fmt.Sprintf("datatable-row-%d", docRow),
		Style: core.Style{
			Direction:  core.Row,
			MinHeight:  core.Pt(specRowHeight),
			Gap:        specSpace8,
			AlignItems: core.AlignCenter,
		},
		Children: cells,
	}
}

func currentCellText(ed *Editor, docRow int, col string) string {
	if ed == nil || ed.table == nil || ed.table.doc == nil {
		return ""
	}
	v, ok := ed.table.doc.Cell(docRow, col)
	if !ok {
		return ""
	}
	return v.Display()
}

func dataTableCellControl(cx *core.Scope, p SpecPalette, ed *Editor, rev *core.Signal[uint64], docRow int, col DataTableColumn, disabled func() bool) core.Node {
	testID := fmt.Sprintf("datatable-cell-%d-%s", docRow, col.Name)
	label := fmt.Sprintf("Row %d %s", docRow+1, col.Name)

	if col.Kind == datatable.KindBool {
		return ui.Checkbox{
			TestID:   testID,
			Label:    label,
			Disabled: disabled,
			Style: core.Style{
				MinHeight:    core.Pt(specRowHeight),
				Color:        p.TextSecondary,
				FocusVisible: p.FocusRing(-2),
			},
			TextFunc: func() string {
				_ = rev.Get()
				if ed.table != nil && ed.table.doc != nil {
					if v, ok := ed.table.doc.Cell(docRow, col.Name); ok {
						if b, ok := v.Bool(); ok {
							return strconv.FormatBool(b)
						}
					}
				}
				return "false"
			},
			Checked: func() bool {
				_ = rev.Get()
				if ed.table != nil && ed.table.doc != nil {
					if v, ok := ed.table.doc.Cell(docRow, col.Name); ok {
						b, _ := v.Bool()
						return b
					}
				}
				return false
			},
			OnChange: func(on bool) {
				_ = ed.SetDataTableCell(docRow, col.Name, datatable.BoolValue(on))
			},
		}
	}

	draft := core.NewSignal(cx, currentCellText(ed, docRow, col.Name))
	editing := core.NewSignal(cx, false)
	invalid := core.NewSignal(cx, false)

	commit := func(text string) {
		editing.Set(false)
		if err := ed.SetDataTableCellText(docRow, col.Name, text); err != nil {
			invalid.Set(true)
			draft.Set(currentCellText(ed, docRow, col.Name))
			return
		}
		invalid.Set(false)
		draft.Set(currentCellText(ed, docRow, col.Name))
	}

	return ui.Input{
		TestID:   testID,
		Label:    label,
		Disabled: disabled,
		Invalid:  invalid.Get,
		StyleFunc: func() core.Style {
			s := specInputStyle(p)
			if invalid.Get() {
				s.BorderColor = p.Problem().Fg
			}
			return s
		},
		Value: func() string {
			_ = rev.Get()
			if editing.Get() {
				return draft.Get()
			}
			return currentCellText(ed, docRow, col.Name)
		},
		OnFocus: func() {
			draft.Set(currentCellText(ed, docRow, col.Name))
			editing.Set(true)
			invalid.Set(false)
		},
		OnChange: func(text string) {
			draft.Set(text)
			editing.Set(true)
		},
		OnSubmit: commit,
		OnBlur: func() {
			if editing.Get() {
				commit(draft.Get())
			}
		},
	}
}
