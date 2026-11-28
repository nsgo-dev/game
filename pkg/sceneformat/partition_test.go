package sceneformat

import "testing"

func TestPartitionBoundsAndDeterministicCells(t *testing.T) {
	p := Partition{CellSize: 10, LoadRadius: 1, UnloadRadius: 2}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	got := p.Cells(CellObject{X: 10, Y: 10, Width: 12, Height: 2})
	if len(got) != 4 || got[0] != (CellCoord{0, 0}) || got[3] != (CellCoord{1, 1}) {
		t.Fatalf("cells=%v", got)
	}
	n := p.Needed([]StreamingSource{{X: 0, Y: 0}})
	if len(n) != 9 {
		t.Fatalf("needed=%v", n)
	}
}
func TestPartitionRejectsInvalidRadii(t *testing.T) {
	if err := (Partition{CellSize: 1, LoadRadius: 2, UnloadRadius: 1}).Validate(); err == nil {
		t.Fatal("invalid radii accepted")
	}
}
