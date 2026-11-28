package datatable

import "testing"

func TestTableViewPinsRowsAndVersionAcrossReload(t *testing.T) {
	table, err := NewTable[int]("values.yaml")
	if err != nil {
		// NewTable reads from disk, so use a table literal for this focused
		// publication test.
		table = &Table[int]{rows: []int{1}, path: "values.yaml"}
		table.version.Store(1)
	}
	old := table.View()
	if old.Version() != 1 || len(old.Rows()) != 1 {
		t.Fatalf("old view = version %d rows %v", old.Version(), old.Rows())
	}
	if err := table.applyReload([]int{2, 3}, nil); err != nil {
		t.Fatal(err)
	}
	next := table.View()
	if next.Version() != 2 || len(next.Rows()) != 2 || old.Version() != 1 || len(old.Rows()) != 1 || old.Rows()[0] != 1 {
		t.Fatalf("old=%d/%v next=%d/%v", old.Version(), old.Rows(), next.Version(), next.Rows())
	}
}
