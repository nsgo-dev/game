package tilemap_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/tilemap"
)

func TestTileEditLogSetTileAndFillRect(t *testing.T) {
	m := tilemap.New(64, 64, 16, 16)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 64, 64, nil))

	if rev := m.TileRevision(); rev != 0 {
		t.Fatalf("initial TileRevision = %d, want 0", rev)
	}
	if cur, ok := m.TileChangesSince(0, func(int, int) { t.Fatal("unexpected callback") }); cur != 0 || !ok {
		t.Fatalf("TileChangesSince(0) = (%d, %v), want (0, true)", cur, ok)
	}

	// Out-of-bounds and no-op edits do not advance TileRevision.
	m.SetTile(0, -1, 0, 1)
	m.SetTile(0, 0, 0, 0)
	if rev := m.TileRevision(); rev != 0 {
		t.Fatalf("TileRevision after no-op = %d, want 0", rev)
	}

	// Single SetTile records exact cell.
	m.SetTile(0, 3, 5, 2)
	m.SetTile(0, 7, 9, 4)
	var got [][2]int
	cur, ok := m.TileChangesSince(0, func(col, row int) {
		got = append(got, [2]int{col, row})
	})
	if !ok || cur != 2 {
		t.Fatalf("TileChangesSince(0) = (%d, %v), want (2, true)", cur, ok)
	}
	want := [][2]int{{3, 5}, {7, 9}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("changed cells = %v, want %v", got, want)
	}

	// Querying at current revision yields 0 callbacks.
	if cur2, ok2 := m.TileChangesSince(cur, func(int, int) { t.Fatal("unexpected callback at current rev") }); !ok2 || cur2 != cur {
		t.Fatalf("TileChangesSince(%d) = (%d, %v), want (%d, true)", cur, cur2, ok2, cur)
	}

	// Small FillRect records individual cells.
	m.FillRect(0, 10, 10, 2, 2, 5)
	got = got[:0]
	cur, ok = m.TileChangesSince(cur, func(col, row int) {
		got = append(got, [2]int{col, row})
	})
	if !ok || len(got) != 4 {
		t.Fatalf("after 2x2 FillRect: ok=%v, cells=%v, want ok=true with 4 cells", ok, got)
	}

	// Bulk FillRect larger than ring capacity signals full rebuild (ok=false).
	m.FillRect(0, 0, 0, 64, 64, 9)
	cur2, ok2 := m.TileChangesSince(cur, func(int, int) { t.Fatal("bulk overflow should not replay cells") })
	if ok2 || cur2 <= cur {
		t.Fatalf("after bulk FillRect: (%d, %v), want (rev>%d, false)", cur2, ok2, cur)
	}

	// Ring overflow from >1024 individual SetTile calls also signals ok=false.
	startRev := cur2
	for i := range 1100 {
		m.SetTile(0, i%64, (i/64)%64, uint32(10+i%5))
	}
	cur3, ok3 := m.TileChangesSince(startRev, nil)
	if ok3 || cur3 <= startRev {
		t.Fatalf("after ring overflow: (%d, %v), want (rev>%d, false)", cur3, ok3, startRev)
	}
}
