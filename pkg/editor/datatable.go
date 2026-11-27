package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/refs"
	"github.com/nsgo-dev/game/pkg/tooling/datatable"
)

// ErrNoDataTable is returned by data table edits when no data table is open.
var ErrNoDataTable = errors.New("editor: no data table is open")

// SortOrder selects how the Data Table panel sorts visible rows.
type SortOrder int

const (
	SortNone SortOrder = iota
	SortAsc
	SortDesc
)

// String returns the human-readable sort direction.
func (s SortOrder) String() string {
	switch s {
	case SortAsc:
		return "asc"
	case SortDesc:
		return "desc"
	default:
		return "none"
	}
}

// DataTableColumn describes a column shown in the Data Table panel grid.
type DataTableColumn struct {
	Name string
	Kind datatable.ValueKind
}

type dataTableState struct {
	doc       *datatable.Document
	path      string
	fromDisk  bool
	rowType   *datatable.RowType
	sortCol   string
	sortOrder SortOrder
	filter    string
	loading   bool
	lastErr   error
}

func (s *dataTableState) clone() *dataTableState {
	if s == nil {
		return nil
	}
	cp := *s
	cp.doc = s.doc.Clone()
	return &cp
}

// RegisterDataTableType registers Go struct row type T for datatable YAML files matching
// pattern (a slash-separated path or glob relative to the project root, or a filename glob
// such as "enemies.yaml" or "data/*.yaml"). While a matching table is open in the Data Table
// panel, cells are checked with datatable.LoadYAML's rules and invalid edits are refused.
func RegisterDataTableType[T any](e *Editor, pattern string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	rt, err := datatable.RowTypeFor[T]()
	if err != nil {
		return err
	}
	return e.RegisterDataTableRowType(pattern, rt)
}

// RegisterDataTableRowType registers rt for datatable YAML files matching pattern.
// If a matching table is already open, it is validated against rt before the registration
// takes effect.
func (e *Editor) RegisterDataTableRowType(pattern string, rt *datatable.RowType) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if rt == nil {
		return errors.New("editor: nil data table row type")
	}
	if e.table != nil {
		target := e.dataTableMatchPath(e.table.path)
		if ok, err := datatable.MatchPattern(pattern, target); err == nil && ok {
			if err := rt.ValidateDocument(e.table.path, e.table.doc); err != nil {
				return err
			}
		}
	}
	if e.tableTypes == nil {
		e.tableTypes = datatable.NewRegistry()
	}
	if err := e.tableTypes.Register(pattern, rt); err != nil {
		return err
	}
	if e.table != nil {
		e.table.rowType = e.DataTableRowTypeFor(e.table.path)
		e.table.lastErr = nil
		e.notify()
	}
	return nil
}

// DataTableRowTypeFor returns the registered RowType matching path, or nil.
func (e *Editor) DataTableRowTypeFor(path string) *datatable.RowType {
	if e == nil || e.tableTypes == nil || path == "" {
		return nil
	}
	return e.tableTypes.Match(e.dataTableMatchPath(path))
}

func (e *Editor) dataTableMatchPath(filePath string) string {
	if e != nil && e.ProjectPath != "" {
		root := filepath.Dir(e.ProjectPath)
		if rel, err := filepath.Rel(root, filePath); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filePath)
}

// DataTable returns a copy of the YAML data table open in the Data Table panel, or nil.
func (e *Editor) DataTable() *datatable.Document {
	if e == nil || e.table == nil || e.table.doc == nil {
		return nil
	}
	return e.table.doc.Clone()
}

// DataTablePath returns the file path or name of the open data table, or "".
func (e *Editor) DataTablePath() string {
	if e == nil || e.table == nil {
		return ""
	}
	return e.table.path
}

// DataTableRowType returns the registered Go row type active for the open data table, or nil.
func (e *Editor) DataTableRowType() *datatable.RowType {
	if e == nil || e.table == nil {
		return nil
	}
	return e.table.rowType
}

// DataTableError returns the most recent validation error from editing the open data table, or nil.
func (e *Editor) DataTableError() error {
	if e == nil || e.table == nil {
		return nil
	}
	return e.table.lastErr
}

// ClearDataTableError clears the open data table's validation error banner.
func (e *Editor) ClearDataTableError() {
	if e == nil || e.table == nil || e.table.lastErr == nil {
		return
	}
	e.table.lastErr = nil
	e.notify()
}

// DataTableLoading reports whether the Data Table panel is currently loading.
func (e *Editor) DataTableLoading() bool {
	return e != nil && e.table != nil && e.table.loading
}

// SetDataTableLoading updates the open data table's loading state and notifies listeners.
func (e *Editor) SetDataTableLoading(loading bool) {
	if e == nil || e.table == nil || e.table.loading == loading {
		return
	}
	e.table.loading = loading
	e.notify()
}

// OpenDataTableFile reads the YAML data table at path and opens it in the Data Table panel
// as an undoable command. If a Go row type is registered for path, the table is checked
// against it before opening.
func (e *Editor) OpenDataTableFile(path string) error {
	if e.playing {
		return ErrPlaying
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return e.openDataTable(path, data, true)
}

// OpenDataTableProjectFile opens relPath (slash-separated from the open project's root)
// in the Data Table panel.
func (e *Editor) OpenDataTableProjectFile(relPath string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	full := filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(relPath))
	return e.OpenDataTableFile(full)
}

// OpenDataTableYAML parses data as a versioned YAML data table named name (used for error
// messages and row-type pattern matching) and opens it in the Data Table panel as an undoable
// command.
func (e *Editor) OpenDataTableYAML(name string, data []byte) error {
	return e.openDataTable(name, data, false)
}

func (e *Editor) openDataTable(path string, data []byte, fromDisk bool) error {
	if e.playing {
		return ErrPlaying
	}
	doc, err := datatable.ParseDocument(path, data)
	if err != nil {
		return err
	}
	rt := e.DataTableRowTypeFor(path)
	if rt != nil {
		if err := rt.ValidateDocument(path, doc); err != nil {
			return err
		}
	}
	next := &dataTableState{
		doc:      doc,
		path:     path,
		fromDisk: fromDisk,
		rowType:  rt,
	}
	label := "Open data table"
	if path != "" {
		label += " " + path
	}
	e.History.Execute(&swapDataTableCmd{
		ed:     e,
		label:  label,
		before: e.table.clone(),
		after:  next,
	})
	if saved, err := doc.Marshal(); err == nil {
		e.tableSaved = saved
	}
	return nil
}

// CloseDataTable closes the open data table as an undoable command.
func (e *Editor) CloseDataTable() error {
	if e.playing {
		return ErrPlaying
	}
	if e.table == nil {
		return nil
	}
	e.History.Execute(&swapDataTableCmd{
		ed:     e,
		label:  "Close data table",
		before: e.table.clone(),
		after:  nil,
	})
	return nil
}

// SaveDataTableYAML encodes the open data table to YAML, preserving exact bytes when unedited
// and changing only edited lines for single-cell updates.
func (e *Editor) SaveDataTableYAML() ([]byte, error) {
	if e == nil || e.table == nil || e.table.doc == nil {
		return nil, ErrNoDataTable
	}
	return e.table.doc.Marshal()
}

// SaveDataTableFile writes the open data table to path atomically and remembers path.
func (e *Editor) SaveDataTableFile(path string) error {
	data, err := e.SaveDataTableYAML()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	e.table.path = path
	e.table.fromDisk = true
	e.table.rowType = e.DataTableRowTypeFor(path)
	e.table.lastErr = nil
	e.tableSaved = append([]byte(nil), data...)
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = "Saved table " + path
	e.notify()
	return nil
}

func (e *Editor) saveDataTableFromPanel() error {
	if e.table == nil {
		return ErrNoDataTable
	}
	if e.OnSaveDataTable != nil {
		data, err := e.SaveDataTableYAML()
		if err != nil {
			return err
		}
		if err := e.OnSaveDataTable(data); err != nil {
			return err
		}
		e.table.lastErr = nil
		e.tableSaved = append([]byte(nil), data...)
		e.Status = "Saved table"
		e.notify()
		return nil
	}
	if !e.table.fromDisk || e.table.path == "" {
		return errors.New("editor: no table path; open the table from a file or set OnSaveDataTable")
	}
	return e.SaveDataTableFile(e.table.path)
}

// DataTableColumns returns the grid columns for the open data table: the union of all rows'
// keys in deterministic first-appearance order, followed by any columns declared by the
// registered Go row type that no row currently sets.
func (e *Editor) DataTableColumns() []DataTableColumn {
	if e == nil || e.table == nil || e.table.doc == nil {
		return nil
	}
	doc := e.table.doc
	rt := e.table.rowType
	keys := doc.Columns()
	cols := make([]DataTableColumn, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		kind := doc.ColumnKind(k)
		if rt != nil {
			if spec, ok := rt.Column(k); ok {
				kind = spec.Kind
				seen[spec.Name] = struct{}{}
			}
		}
		seen[k] = struct{}{}
		cols = append(cols, DataTableColumn{Name: k, Kind: kind})
	}
	if rt != nil {
		for _, spec := range rt.Columns() {
			if _, ok := seen[spec.Name]; ok {
				continue
			}
			cols = append(cols, DataTableColumn{Name: spec.Name, Kind: spec.Kind})
		}
	}
	return cols
}

// SetDataTableCell updates or inserts cell (row, col) with val as an undoable command.
// When a Go row type is registered for the open table, the resulting row is checked with
// LoadYAML's rules and refused on error.
func (e *Editor) SetDataTableCell(row int, col string, val datatable.Value) error {
	if e.playing {
		return ErrPlaying
	}
	if e.table == nil || e.table.doc == nil {
		return ErrNoDataTable
	}
	if row < 0 || row >= e.table.doc.RowCount() {
		err := fmt.Errorf("editor: table row %d out of range [0, %d)", row, e.table.doc.RowCount())
		e.failDataTable(err)
		return err
	}
	next := e.table.doc.Clone()
	if err := next.SetCell(row, col, val); err != nil {
		e.failDataTable(err)
		return err
	}
	if e.table.rowType != nil {
		if err := e.table.rowType.ValidateRow(e.table.path, row, next.Row(row)); err != nil {
			e.failDataTable(err)
			return err
		}
	}
	// If the cell did not change at all, clear any prior error without adding a no-op undo entry.
	beforeBytes, err1 := e.table.doc.Marshal()
	afterBytes, err2 := next.Marshal()
	if err1 == nil && err2 == nil && bytes.Equal(beforeBytes, afterBytes) {
		if e.table.lastErr != nil {
			e.table.lastErr = nil
			e.notify()
		}
		return nil
	}
	e.History.Execute(&dataTableEditCmd{
		ed:     e,
		label:  fmt.Sprintf("Edit row %d %s", row+1, col),
		before: e.table.doc.Clone(),
		after:  next,
	})
	return nil
}

// SetDataTableCellText parses text using the column's registered Go type (if any) or its
// inferred ValueKind (string, number, bool, duration, list), and applies it as an undoable
// cell edit. Invalid values are refused and reported via DataTableError and Status.
func (e *Editor) SetDataTableCellText(row int, col, text string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.table == nil || e.table.doc == nil {
		return ErrNoDataTable
	}
	if row < 0 || row >= e.table.doc.RowCount() {
		err := fmt.Errorf("editor: table row %d out of range [0, %d)", row, e.table.doc.RowCount())
		e.failDataTable(err)
		return err
	}
	if e.table.rowType != nil {
		val, err := e.table.rowType.ParseCellText(e.table.path, row, col, text)
		if err != nil {
			e.failDataTable(err)
			return err
		}
		return e.SetDataTableCell(row, col, val)
	}

	kind := datatable.KindNull
	if existing, ok := e.table.doc.Cell(row, col); ok && existing.Kind != datatable.KindNull {
		kind = existing.Kind
	} else {
		kind = e.table.doc.ColumnKind(col)
	}
	var (
		val datatable.Value
		err error
	)
	if kind == datatable.KindNull {
		val = datatable.InferValue(text)
	} else {
		val, err = datatable.ParseValue(kind, text)
		if err != nil {
			ce := &datatable.CellError{
				File:  e.table.path,
				Row:   row + 1,
				Field: col,
				Value: text,
				Err:   err,
			}
			e.failDataTable(ce)
			return ce
		}
	}
	return e.SetDataTableCell(row, col, val)
}

// AddDataTableRow appends a new row to the open data table as an undoable command and
// returns its 0-based index. When cells is empty, it fills each column with the deterministic
// zero value of its registered or inferred kind.
func (e *Editor) AddDataTableRow(cells ...datatable.Cell) (int, error) {
	if e.playing {
		return -1, ErrPlaying
	}
	if e.table == nil || e.table.doc == nil {
		return -1, ErrNoDataTable
	}
	if len(cells) == 0 {
		cols := e.DataTableColumns()
		cells = make([]datatable.Cell, 0, len(cols))
		for _, c := range cols {
			val := datatable.ZeroValue(c.Kind)
			if e.table.rowType != nil {
				if spec, ok := e.table.rowType.Column(c.Name); ok && spec.Nullable {
					val = datatable.NullValue()
				}
			}
			cells = append(cells, datatable.Cell{Key: c.Name, Value: val})
		}
	}
	next := e.table.doc.Clone()
	idx, err := next.AppendRow(cells...)
	if err != nil {
		e.failDataTable(err)
		return -1, err
	}
	if e.table.rowType != nil {
		if err := e.table.rowType.ValidateRow(e.table.path, idx, next.Row(idx)); err != nil {
			e.failDataTable(err)
			return -1, err
		}
	}
	e.History.Execute(&dataTableEditCmd{
		ed:     e,
		label:  "Add table row",
		before: e.table.doc.Clone(),
		after:  next,
	})
	return idx, nil
}

// DeleteDataTableRow deletes row (0-based) from the open data table as an undoable command.
func (e *Editor) DeleteDataTableRow(row int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.table == nil || e.table.doc == nil {
		return ErrNoDataTable
	}
	next := e.table.doc.Clone()
	if err := next.DeleteRow(row); err != nil {
		e.failDataTable(err)
		return err
	}
	e.History.Execute(&dataTableEditCmd{
		ed:     e,
		label:  fmt.Sprintf("Delete table row %d", row+1),
		before: e.table.doc.Clone(),
		after:  next,
	})
	return nil
}

// MoveDataTableRow moves row from to to (both 0-based) as an undoable command.
func (e *Editor) MoveDataTableRow(from, to int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.table == nil || e.table.doc == nil {
		return ErrNoDataTable
	}
	if from < 0 || from >= e.table.doc.RowCount() {
		err := fmt.Errorf("editor: table row %d out of range [0, %d)", from, e.table.doc.RowCount())
		e.failDataTable(err)
		return err
	}
	if to < 0 || to >= e.table.doc.RowCount() {
		err := fmt.Errorf("editor: target table row %d out of range [0, %d)", to, e.table.doc.RowCount())
		e.failDataTable(err)
		return err
	}
	if from == to {
		return nil
	}
	next := e.table.doc.Clone()
	if err := next.MoveRow(from, to); err != nil {
		e.failDataTable(err)
		return err
	}
	e.History.Execute(&dataTableEditCmd{
		ed:     e,
		label:  fmt.Sprintf("Move table row %d to %d", from+1, to+1),
		before: e.table.doc.Clone(),
		after:  next,
	})
	return nil
}

// SetDataTableSort sets the grid view's sort column and direction without modifying the document.
func (e *Editor) SetDataTableSort(col string, order SortOrder) {
	if e == nil || e.table == nil {
		return
	}
	if order == SortNone || col == "" {
		e.table.sortCol = ""
		e.table.sortOrder = SortNone
	} else {
		e.table.sortCol = col
		e.table.sortOrder = order
	}
	e.notify()
}

// DataTableSort returns the grid view's current sort column and direction.
func (e *Editor) DataTableSort() (string, SortOrder) {
	if e == nil || e.table == nil {
		return "", SortNone
	}
	return e.table.sortCol, e.table.sortOrder
}

// SetDataTableFilter sets the case-insensitive substring filter applied to the grid view.
func (e *Editor) SetDataTableFilter(query string) {
	if e == nil || e.table == nil {
		return
	}
	if e.table.filter == query {
		return
	}
	e.table.filter = query
	e.notify()
}

// DataTableFilter returns the current grid view filter query.
func (e *Editor) DataTableFilter() string {
	if e == nil || e.table == nil {
		return ""
	}
	return e.table.filter
}

// DataTableViewRows returns the 0-based document row indices currently visible after applying
// the active filter and sort without mutating the document's row order.
func (e *Editor) DataTableViewRows() []int {
	if e == nil || e.table == nil || e.table.doc == nil {
		return nil
	}
	doc := e.table.doc
	n := doc.RowCount()
	q := strings.ToLower(strings.TrimSpace(e.table.filter))
	rows := make([]int, 0, n)
	for i := range n {
		if q == "" || rowMatchesFilter(doc.Row(i), q) {
			rows = append(rows, i)
		}
	}
	if e.table.sortOrder != SortNone && e.table.sortCol != "" {
		col := e.table.sortCol
		desc := e.table.sortOrder == SortDesc
		slices.SortStableFunc(rows, func(a, b int) int {
			va, oka := doc.Cell(a, col)
			vb, okb := doc.Cell(b, col)
			nullA := !oka || va.Kind == datatable.KindNull
			nullB := !okb || vb.Kind == datatable.KindNull
			if nullA && nullB {
				return 0
			}
			// Keep missing/null cells at the bottom regardless of sort direction.
			if nullA {
				return 1
			}
			if nullB {
				return -1
			}
			c := compareCellValues(va, vb)
			if desc {
				return -c
			}
			return c
		})
	}
	return rows
}

func rowMatchesFilter(r datatable.Row, lowerQuery string) bool {
	for _, c := range r.Cells {
		if strings.Contains(strings.ToLower(c.Value.Display()), lowerQuery) {
			return true
		}
	}
	return false
}

func compareCellValues(a, b datatable.Value) int {
	if a.Kind == b.Kind {
		switch a.Kind {
		case datatable.KindNumber:
			if ia, okA := a.Int64(); okA {
				if ib, okB := b.Int64(); okB {
					return cmp.Compare(ia, ib)
				}
			}
			if ua, okA := a.Uint64(); okA {
				if ub, okB := b.Uint64(); okB {
					return cmp.Compare(ua, ub)
				}
			}
			if fa, okA := a.Float64(); okA {
				if fb, okB := b.Float64(); okB {
					return cmp.Compare(fa, fb)
				}
			}
		case datatable.KindDuration:
			if da, okA := a.Duration(); okA {
				if db, okB := b.Duration(); okB {
					return cmp.Compare(da, db)
				}
			}
		case datatable.KindBool:
			ba, _ := a.Bool()
			bb, _ := b.Bool()
			if ba == bb {
				return 0
			}
			if !ba {
				return -1
			}
			return 1
		}
	}
	sa, sb := a.Display(), b.Display()
	if c := cmp.Compare(strings.ToLower(sa), strings.ToLower(sb)); c != 0 {
		return c
	}
	return cmp.Compare(sa, sb)
}

func (e *Editor) failDataTable(err error) {
	if e.table != nil {
		e.table.lastErr = err
	}
	e.report(err)
}

func (e *Editor) followDataTableMove(moveFile func(*string), movePath func(*string), written []refs.Write) {
	if e == nil || e.table == nil || !e.table.fromDisk {
		return
	}
	before := e.table.path
	moveFile(&e.table.path)
	if e.table.path != before {
		e.table.rowType = e.DataTableRowTypeFor(e.table.path)
	}
	var rewritten []byte
	if rel, ok := e.projectRel(e.table.path); ok {
		for _, w := range written {
			if w.Path == rel {
				rewritten = w.Data
				break
			}
		}
	}
	if e.DataTableDirty() {
		// Preserve unsaved edits in memory: rewrite matching path cells in place instead of clobbering e.table.doc from disk.
		if e.table.doc != nil && movePath != nil {
			rewriteDataTablePaths(e.table.doc, movePath)
		}
		if len(rewritten) > 0 {
			if doc, err := datatable.ParseDocument(e.table.path, rewritten); err == nil {
				if saved, err := doc.Marshal(); err == nil {
					e.tableSaved = saved
				}
			}
		}
		return
	}
	if len(rewritten) > 0 {
		if doc, err := datatable.ParseDocument(e.table.path, rewritten); err == nil {
			e.table.doc = doc
			e.table.lastErr = nil
			if saved, err := doc.Marshal(); err == nil {
				e.tableSaved = saved
			}
		}
	}
}

func rewriteDataTablePaths(doc *datatable.Document, movePath func(*string)) {
	if doc == nil || movePath == nil {
		return
	}
	cols := doc.Columns()
	for r := 0; r < doc.RowCount(); r++ {
		for _, col := range cols {
			val, ok := doc.Cell(r, col)
			if !ok {
				continue
			}
			if next, changed := rewriteDataTableValue(val, movePath); changed {
				_ = doc.SetCell(r, col, next)
			}
		}
	}
}

func rewriteDataTableValue(v datatable.Value, movePath func(*string)) (datatable.Value, bool) {
	switch v.Kind {
	case datatable.KindString:
		s := v.Text
		movePath(&s)
		if s != v.Text {
			return datatable.StringValue(s), true
		}
		return v, false
	case datatable.KindList:
		changed := false
		out := make([]datatable.Value, len(v.List))
		for i, elem := range v.List {
			nv, c := rewriteDataTableValue(elem, movePath)
			out[i] = nv
			if c {
				changed = true
			}
		}
		if changed {
			return datatable.ListValue(out...), true
		}
		return v, false
	default:
		return v, false
	}
}

type swapDataTableCmd struct {
	ed     *Editor
	label  string
	before *dataTableState
	after  *dataTableState
}

func (c *swapDataTableCmd) Label() string { return c.label }

func (c *swapDataTableCmd) Do() {
	c.ed.table = c.after.clone()
	if c.ed.table != nil {
		c.ed.table.rowType = c.ed.DataTableRowTypeFor(c.ed.table.path)
	}
	c.ed.Status = c.label
}

func (c *swapDataTableCmd) Undo() {
	c.ed.table = c.before.clone()
	if c.ed.table != nil {
		c.ed.table.rowType = c.ed.DataTableRowTypeFor(c.ed.table.path)
	}
	c.ed.Status = "Undo " + c.label
}

type dataTableEditCmd struct {
	ed     *Editor
	label  string
	before *datatable.Document
	after  *datatable.Document
}

func (c *dataTableEditCmd) Label() string { return c.label }

func (c *dataTableEditCmd) Do() {
	if c.ed.table == nil {
		return
	}
	c.ed.table.doc = c.after.Clone()
	c.ed.table.lastErr = nil
	c.ed.Status = c.label
}

func (c *dataTableEditCmd) Undo() {
	if c.ed.table == nil {
		return
	}
	c.ed.table.doc = c.before.Clone()
	c.ed.table.lastErr = nil
	c.ed.Status = "Undo " + c.label
}
