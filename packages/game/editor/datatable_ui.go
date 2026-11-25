package editor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
	"github.com/nsgo-dev/nsgo/core"
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
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	// structure changes when columns, column kinds, visible row order, or the open table path change,
	// so editing a cell's value in place does not remount inputs and steal focus.
	structure := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.table == nil || ed.table.doc == nil {
			return ""
		}
		var b strings.Builder
		b.WriteString(ed.table.path)
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

	header := ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Text{
				Content: "Data Table",
				Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
			},
			core.Text{
				TestID: "datatable-path",
				ContentFunc: func() string {
					_ = rev.Get()
					p := ed.DataTablePath()
					if p == "" {
						return "Untitled"
					}
					name := filepath.Base(p)
					if rt := ed.DataTableRowType(); rt != nil {
						return name + " (" + rt.Name() + ")"
					}
					return name
				},
				Style: core.Style{FontSize: 12, Color: colors.Muted()},
			},
			core.View{
				Style: core.Style{Width: core.Pt(180)},
				Children: []core.Node{ui.Input{
					TestID:      "datatable-filter",
					Label:       "Filter rows",
					Placeholder: "Filter rows...",
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
			ui.Button{
				TestID:   "datatable-add-row",
				Text:     "+ Row",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonSecondary,
				Disabled: disabled,
				OnPress: func() {
					_, _ = ed.AddDataTableRow()
				},
			},
			ui.Button{
				TestID:   "datatable-save",
				Text:     "Save Table",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress: func() {
					ed.report(ed.saveDataTableFromPanel())
				},
			},
			ui.Button{
				TestID:   "datatable-close",
				Text:     "Close",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonGhost,
				Disabled: disabled,
				OnPress: func() {
					ed.report(ed.CloseDataTable())
				},
			},
		},
	}

	grid := ui.ScrollView{
		Style:        core.Style{FlexGrow: 1},
		ContentStyle: core.Style{Gap: 4},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			if structure.Get() == "" || ed.table == nil || ed.table.doc == nil {
				return nil
			}
			cols := ed.DataTableColumns()
			viewRows := ed.DataTableViewRows()
			rows := make([]core.Node, 0, len(viewRows)+1)
			rows = append(rows, dataTableHeaderRow(cx, ed, rev, cols))
			if len(viewRows) == 0 {
				msg := "No rows"
				if strings.TrimSpace(ed.DataTableFilter()) != "" {
					msg = "No matching rows"
				}
				rows = append(rows, core.Text{
					TestID:  "datatable-empty",
					Content: msg,
					Style:   core.Style{FontSize: 12, Color: colors.Muted(), Padding: core.Symmetric(4, 2)},
				})
			} else {
				for _, docRow := range viewRows {
					rows = append(rows, dataTableDataRow(cx, ed, rev, docRow, cols, disabled))
				}
			}
			return core.View{TestID: "datatable-grid", Style: core.Style{Gap: 4}, Children: rows}
		})},
	}

	return core.View{
		TestID: "datatable-panel",
		Style: core.Style{
			Height:      core.Pt(240),
			Padding:     core.Symmetric(6, 12),
			Gap:         6,
			Background:  colors.Inset(),
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
		},
		Children: []core.Node{
			header,
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.DataTableError() != nil
				},
				Then: func(cx *core.Scope) core.Node {
					return core.Text{
						TestID: "datatable-error",
						ContentFunc: func() string {
							_ = rev.Get()
							if err := ed.DataTableError(); err != nil {
								return err.Error()
							}
							return ""
						},
						Style: core.Style{FontSize: 11, Color: colors.Danger()},
					}
				},
			},
			grid,
		},
	}
}

func dataTableHeaderRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], cols []DataTableColumn) core.Node {
	colors := ui.ColorsOf(cx)
	cells := make([]core.Node, 0, len(cols)+1)
	cells = append(cells, core.View{
		Style: core.Style{Width: core.Pt(104), JustifyContent: core.JustifyCenter},
		Children: []core.Node{core.Text{
			Content: "Row",
			Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
		}},
	})
	for _, col := range cols {
		c := col
		cells = append(cells, core.View{
			Style: core.Style{Width: core.Pt(132)},
			Children: []core.Node{ui.Button{
				TestID:  "datatable-col-" + c.Name,
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				TextFunc: func() string {
					_ = rev.Get()
					sortCol, order := ed.DataTableSort()
					suffix := ""
					if sortCol == c.Name {
						switch order {
						case SortAsc:
							suffix = " ^"
						case SortDesc:
							suffix = " v"
						}
					}
					kindTag := ""
					if c.Kind != datatable.KindNull {
						kindTag = " [" + c.Kind.String() + "]"
					}
					return c.Name + kindTag + suffix
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
			}},
		})
	}
	return core.View{
		TestID: "datatable-header",
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         6,
			Padding:     core.Edges{Bottom: 4},
			Borders:     core.Edges{Bottom: 1},
			BorderColor: colors.Line(),
		},
		Children: cells,
	}
}

func dataTableDataRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], docRow int, cols []DataTableColumn, disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
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
			Gap:        2,
			Width:      core.Pt(104),
			AlignItems: core.AlignCenter,
		},
		Children: []core.Node{
			core.Text{
				TestID:  fmt.Sprintf("datatable-row-%d-index", docRow),
				Content: strconv.Itoa(docRow + 1),
				Style:   core.Style{Width: core.Pt(20), FontSize: 11, Color: colors.Muted()},
			},
			ui.Button{
				TestID:  fmt.Sprintf("datatable-row-%d-up", docRow),
				Text:    "^",
				Label:   fmt.Sprintf("Move row %d up", docRow+1),
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				Disabled: func() bool {
					return !canReorder() || docRow <= 0
				},
				OnPress: func() {
					_ = ed.MoveDataTableRow(docRow, docRow-1)
				},
			},
			ui.Button{
				TestID:  fmt.Sprintf("datatable-row-%d-down", docRow),
				Text:    "v",
				Label:   fmt.Sprintf("Move row %d down", docRow+1),
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				Disabled: func() bool {
					if !canReorder() || ed.table == nil || ed.table.doc == nil {
						return true
					}
					return docRow >= ed.table.doc.RowCount()-1
				},
				OnPress: func() {
					_ = ed.MoveDataTableRow(docRow, docRow+1)
				},
			},
			ui.Button{
				TestID:   fmt.Sprintf("datatable-row-%d-delete", docRow),
				Text:     "x",
				Label:    fmt.Sprintf("Delete row %d", docRow+1),
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonGhost,
				Disabled: disabled,
				OnPress: func() {
					_ = ed.DeleteDataTableRow(docRow)
				},
			},
		},
	}

	cells := make([]core.Node, 0, len(cols)+1)
	cells = append(cells, controls)
	for _, col := range cols {
		cells = append(cells, core.View{
			Style:    core.Style{Width: core.Pt(132)},
			Children: []core.Node{dataTableCellControl(cx, ed, rev, docRow, col, disabled)},
		})
	}
	return core.View{
		TestID: fmt.Sprintf("datatable-row-%d", docRow),
		Style: core.Style{
			Direction:  core.Row,
			Gap:        6,
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

func dataTableCellControl(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], docRow int, col DataTableColumn, disabled func() bool) core.Node {
	testID := fmt.Sprintf("datatable-cell-%d-%s", docRow, col.Name)
	label := fmt.Sprintf("Row %d %s", docRow+1, col.Name)

	if col.Kind == datatable.KindBool {
		return ui.Checkbox{
			TestID:   testID,
			Label:    label,
			Disabled: disabled,
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
