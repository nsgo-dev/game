package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type testEnemyRow struct {
	ID       string        `table:"enemy_id"`
	Name     string        `table:"display_name"`
	HP       int           `table:"hp"`
	Speed    float64       `table:"move_speed"`
	Boss     bool          `table:"is_boss"`
	Tags     []string      `table:"tags"`
	Cooldown time.Duration `table:"cooldown"`
}

const sampleEnemiesTable = `# Enemy balance table
version: 1
rows:
  - enemy_id: goblin
    display_name: Cave Goblin
    hp: 45 # starting HP
    move_speed: 3.5
    is_boss: false
    tags: [melee, scout]
    cooldown: 500ms
  - enemy_id: dragon
    display_name: Ancient Wyrm
    hp: 1200
    move_speed: 6.25
    is_boss: true
    tags: [flying, fire, boss]
    cooldown: 2s
  - enemy_id: slime
    display_name: Cave Slime
    hp: 20
    move_speed: 1.5
    is_boss: false
    tags: [ooze]
    cooldown: 1s
`

func countChangedLines(before, after string) int {
	a := strings.Split(before, "\n")
	b := strings.Split(after, "\n")
	n := max(len(a), len(b))
	changed := 0
	for i := range n {
		var la, lb string
		if i < len(a) {
			la = a[i]
		}
		if i < len(b) {
			lb = b[i]
		}
		if la != lb {
			changed++
		}
	}
	return changed
}

func TestDataTableOpenAndSaveWithoutEditsIsByteIdentical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "enemies.yaml")
	if err := os.WriteFile(path, []byte(sampleEnemiesTable), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDataTableFile(path); err != nil {
		t.Fatalf("OpenDataTableFile: %v", err)
	}
	if err := ed.SaveDataTableFile(path); err != nil {
		t.Fatalf("SaveDataTableFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(sampleEnemiesTable)) {
		t.Fatalf("saving without edits changed bytes:\ngot:\n%s\nwant:\n%s", got, sampleEnemiesTable)
	}
}

func TestDataTableSingleCellEditChangesOneLineAndUndoRestoresBytes(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDataTableYAML("data/enemies.yaml", []byte(sampleEnemiesTable)); err != nil {
		t.Fatalf("OpenDataTableYAML: %v", err)
	}
	if err := ed.SetDataTableCellText(0, "hp", "60"); err != nil {
		t.Fatalf("SetDataTableCellText: %v", err)
	}
	edited, err := ed.SaveDataTableYAML()
	if err != nil {
		t.Fatalf("SaveDataTableYAML: %v", err)
	}
	if n := countChangedLines(sampleEnemiesTable, string(edited)); n != 1 {
		t.Fatalf("editing one cell changed %d lines, want 1:\n%s", n, edited)
	}
	if !strings.Contains(string(edited), "    hp: 60 # starting HP\n") {
		t.Fatalf("inline comment or updated cell missing:\n%s", edited)
	}

	// Undo restores byte-for-byte original; Redo re-applies the 1-line edit.
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	undone, err := ed.SaveDataTableYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(undone, []byte(sampleEnemiesTable)) {
		t.Fatalf("Undo did not restore exact bytes:\n%s", undone)
	}
	if !ed.Redo() {
		t.Fatal("Redo failed")
	}
	redone, err := ed.SaveDataTableYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(redone, edited) {
		t.Fatalf("Redo did not restore edited bytes")
	}
}

func TestDataTableColumnsRowOpsSortFilterAndKinds(t *testing.T) {
	src := `version: 1
rows:
  - id: b
    hp: 45
    boss: false
    cooldown: 500ms
    tags: [melee]
  - id: a
    hp: 1200
    extra: bonus
    boss: true
    cooldown: 2s
    tags: [fire, boss]
  - id: c
    hp: 10
    boss: false
    cooldown: 250ms
`
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDataTableYAML("items.yaml", []byte(src)); err != nil {
		t.Fatalf("OpenDataTableYAML: %v", err)
	}

	// Union of row keys in first-appearance order with inferred kinds.
	cols := ed.DataTableColumns()
	wantCols := []editor.DataTableColumn{
		{Name: "id", Kind: datatable.KindString},
		{Name: "hp", Kind: datatable.KindNumber},
		{Name: "boss", Kind: datatable.KindBool},
		{Name: "cooldown", Kind: datatable.KindDuration},
		{Name: "tags", Kind: datatable.KindList},
		{Name: "extra", Kind: datatable.KindString},
	}
	if !reflect.DeepEqual(cols, wantCols) {
		t.Fatalf("DataTableColumns = %+v, want %+v", cols, wantCols)
	}

	// Sorting and filtering change the view without mutating the document.
	ed.SetDataTableSort("hp", editor.SortAsc)
	if got := ed.DataTableViewRows(); !reflect.DeepEqual(got, []int{2, 0, 1}) {
		t.Fatalf("SortAsc(hp) view rows = %v, want [2 0 1]", got)
	}
	ed.SetDataTableSort("cooldown", editor.SortDesc)
	if got := ed.DataTableViewRows(); !reflect.DeepEqual(got, []int{1, 0, 2}) {
		t.Fatalf("SortDesc(cooldown) view rows = %v, want [1 0 2]", got)
	}
	ed.SetDataTableFilter("fire")
	if got := ed.DataTableViewRows(); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("Filter(fire) view rows = %v, want [1]", got)
	}
	ed.SetDataTableFilter("")
	ed.SetDataTableSort("", editor.SortNone)

	// Underlying document is still byte-for-byte untouched by sort/filter.
	raw, err := ed.SaveDataTableYAML()
	if err != nil || !bytes.Equal(raw, []byte(src)) {
		t.Fatalf("sort/filter modified document bytes: err=%v", err)
	}

	// Add, reorder, delete rows — all undoable.
	idx, err := ed.AddDataTableRow()
	if err != nil || idx != 3 || ed.DataTable().RowCount() != 4 {
		t.Fatalf("AddDataTableRow = %d, %v", idx, err)
	}
	if err := ed.SetDataTableCellText(3, "id", "d"); err != nil {
		t.Fatalf("SetDataTableCellText id: %v", err)
	}
	if err := ed.MoveDataTableRow(3, 0); err != nil {
		t.Fatalf("MoveDataTableRow: %v", err)
	}
	if v, _ := ed.DataTable().Cell(0, "id"); v.Text != "d" {
		t.Fatalf("row 0 after MoveDataTableRow = %+v", v)
	}
	if err := ed.DeleteDataTableRow(0); err != nil {
		t.Fatalf("DeleteDataTableRow: %v", err)
	}
	if ed.DataTable().RowCount() != 3 {
		t.Fatalf("RowCount after delete = %d", ed.DataTable().RowCount())
	}
	// Undo delete -> d is back at row 0.
	if !ed.Undo() {
		t.Fatal("Undo delete failed")
	}
	if v, _ := ed.DataTable().Cell(0, "id"); v.Text != "d" {
		t.Fatalf("after undo delete, row 0 = %+v", v)
	}
	// Undo move -> d is back at row 3.
	if !ed.Undo() {
		t.Fatal("Undo move failed")
	}
	if v, _ := ed.DataTable().Cell(3, "id"); v.Text != "d" {
		t.Fatalf("after undo move, row 3 = %+v", v)
	}
	// Undo edit id, undo add row -> back to original 3 rows and exact original bytes!
	ed.Undo()
	ed.Undo()
	if raw, err := ed.SaveDataTableYAML(); err != nil || !bytes.Equal(raw, []byte(src)) {
		t.Fatalf("undoing all edits did not restore exact original bytes: err=%v", err)
	}
}

func TestDataTableRegisteredRowTypeRefusesInvalidEditsInGrid(t *testing.T) {
	dir := writeEditorProject(t)
	tableRel := "data/enemies.yaml"
	tablePath := filepath.Join(dir, filepath.FromSlash(tableRel))
	if err := os.MkdirAll(filepath.Dir(tablePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tablePath, []byte(sampleEnemiesTable), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := editor.RegisterDataTableType[testEnemyRow](ed, "data/*.yaml"); err != nil {
		t.Fatalf("RegisterDataTableType: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	if app.Exists("id:datatable-panel") {
		t.Fatal("Data Table panel should not show before opening a table")
	}

	// Open the table from the file browser.
	app.Press("id:files-row-" + tableRel)
	app.Settle()
	app.Press("id:files-open-table")
	app.Settle()

	if !app.Exists("id:datatable-panel") {
		t.Fatalf("Data Table panel did not open; status=%q", ed.Status)
	}
	if rt := ed.DataTableRowType(); rt == nil || rt.Name() != "testEnemyRow" {
		t.Fatalf("DataTableRowType = %v, want testEnemyRow", rt)
	}

	// Invalid int cell ("not-a-number") is refused in the grid and leaves hp at 45.
	app.Clear("id:datatable-cell-0-hp")
	app.Type("id:datatable-cell-0-hp", "not-a-number")
	app.Submit("id:datatable-cell-0-hp")
	app.Settle()

	var ce *datatable.CellError
	if !errors.As(ed.DataTableError(), &ce) || ce.Row != 1 || ce.Field != "hp" {
		t.Fatalf("DataTableError = %v, want CellError at row 1 field hp", ed.DataTableError())
	}
	if !app.Exists("id:datatable-error") {
		t.Fatal("expected datatable-error banner for refused cell value")
	}
	if v, _ := ed.DataTable().Cell(0, "hp"); v.Text != "45" {
		t.Fatalf("refused edit changed hp to %+v", v)
	}

	// Invalid float into int column ("3.5") is also refused (matching LoadYAML).
	if err := ed.SetDataTableCellText(0, "hp", "3.5"); !errors.As(err, &ce) || ce.Field != "hp" {
		t.Fatalf("SetDataTableCellText(hp, 3.5) = %v, want CellError", err)
	}
	// Unknown column is refused with ErrUnknownField.
	if err := ed.SetDataTableCell(0, "hit_points", datatable.IntValue(99)); !errors.Is(err, datatable.ErrUnknownField) {
		t.Fatalf("SetDataTableCell(hit_points) = %v, want ErrUnknownField", err)
	}
	// Invalid duration is refused in the grid.
	app.Clear("id:datatable-cell-0-cooldown")
	app.Type("id:datatable-cell-0-cooldown", "soon")
	app.Submit("id:datatable-cell-0-cooldown")
	app.Settle()
	if !errors.As(ed.DataTableError(), &ce) || ce.Field != "cooldown" {
		t.Fatalf("DataTableError for bad duration = %v", ed.DataTableError())
	}
	if v, _ := ed.DataTable().Cell(0, "cooldown"); v.Text != "500ms" {
		t.Fatalf("refused duration changed cooldown to %+v", v)
	}

	// Valid edit in the grid succeeds, clears the error banner, and changes only one line on Save.
	app.Clear("id:datatable-cell-0-hp")
	app.Type("id:datatable-cell-0-hp", "75")
	app.Submit("id:datatable-cell-0-hp")
	app.Settle()

	if ed.DataTableError() != nil || app.Exists("id:datatable-error") {
		t.Fatalf("valid edit did not clear error: %v", ed.DataTableError())
	}
	if v, _ := ed.DataTable().Cell(0, "hp"); v.Text != "75" {
		t.Fatalf("hp after valid edit = %+v", v)
	}

	// Save Table button writes back to disk with only 1 line changed and valid for LoadYAML.
	app.Press("id:datatable-save")
	app.Settle()
	saved, err := os.ReadFile(tablePath)
	if err != nil {
		t.Fatal(err)
	}
	if n := countChangedLines(sampleEnemiesTable, string(saved)); n != 1 {
		t.Fatalf("saved table changed %d lines, want 1:\n%s", n, saved)
	}
	loaded, err := datatable.LoadYAML[testEnemyRow](tablePath, saved)
	if err != nil {
		t.Fatalf("LoadYAML after panel save: %v", err)
	}
	if len(loaded) != 3 || loaded[0].HP != 75 || loaded[1].HP != 1200 {
		t.Fatalf("loaded enemies = %+v", loaded)
	}

	// Toggle bool cell, sort by column header, filter, add/reorder/delete row, and undo via UI.
	app.Press("id:datatable-cell-0-is_boss")
	app.Settle()
	if v, _ := ed.DataTable().Cell(0, "is_boss"); v.Text != "true" {
		t.Fatalf("is_boss after toggle = %+v", v)
	}
	app.Press("id:toolbar-undo")
	app.Settle()
	if v, _ := ed.DataTable().Cell(0, "is_boss"); v.Text != "false" {
		t.Fatalf("is_boss after undo = %+v", v)
	}

	// Sort by hp ascending then descending via column header button.
	app.Press("id:datatable-col-hp")
	app.Settle()
	if col, order := ed.DataTableSort(); col != "hp" || order != editor.SortAsc {
		t.Fatalf("sort = %q %v, want hp asc", col, order)
	}
	app.Press("id:datatable-col-hp")
	app.Settle()
	if col, order := ed.DataTableSort(); col != "hp" || order != editor.SortDesc {
		t.Fatalf("sort = %q %v, want hp desc", col, order)
	}
	app.Press("id:datatable-col-hp")
	app.Settle()
	if col, order := ed.DataTableSort(); col != "" || order != editor.SortNone {
		t.Fatalf("sort = %q %v, want none", col, order)
	}

	// Filter via filter input.
	app.Type("id:datatable-filter", "dragon")
	app.Settle()
	if got := ed.DataTableViewRows(); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("filtered rows = %v, want [1]", got)
	}
	app.Clear("id:datatable-filter")
	app.Settle()

	// Add row, move row up, delete row via panel buttons.
	app.Press("id:datatable-add-row")
	app.Settle()
	if ed.DataTable().RowCount() != 4 {
		t.Fatalf("RowCount after +Row = %d", ed.DataTable().RowCount())
	}
	app.Press("id:datatable-row-3-up")
	app.Settle()
	if v, _ := ed.DataTable().Cell(2, "enemy_id"); v.Text != "" {
		t.Fatalf("moved new row not at index 2: %+v", v)
	}
	app.Press("id:datatable-row-2-delete")
	app.Settle()
	if ed.DataTable().RowCount() != 3 {
		t.Fatalf("RowCount after delete = %d", ed.DataTable().RowCount())
	}

	// Close table button closes panel, Undo reopens it.
	app.Press("id:datatable-close")
	app.Settle()
	if app.Exists("id:datatable-panel") || ed.DataTable() != nil {
		t.Fatal("Close did not close Data Table panel")
	}
	app.Press("id:toolbar-undo")
	app.Settle()
	if !app.Exists("id:datatable-panel") || ed.DataTable() == nil {
		t.Fatal("Undo did not reopen Data Table panel")
	}
}
