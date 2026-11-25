package editor

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

type cell struct{ c, r int }

func collectCells(x0, y0, x1, y1 float64, w, h int) []cell {
	var out []cell
	cellsAlong(x0, y0, x1, y1, w, h, func(c, r int) { out = append(out, cell{c, r}) })
	return out
}

func TestCellsAlongCrossesEveryCell(t *testing.T) {
	for _, tc := range []struct {
		name           string
		x0, y0, x1, y1 float64
		want           []cell
	}{
		{"a point", 1.5, 2.5, 1.5, 2.5, []cell{{1, 2}}},
		{"a row", 0.5, 1.5, 3.5, 1.5, []cell{{0, 1}, {1, 1}, {2, 1}, {3, 1}}},
		{"a row right to left", 3.5, 1.5, 0.5, 1.5, []cell{{3, 1}, {2, 1}, {1, 1}, {0, 1}}},
		{"a column", 2.5, 0.2, 2.5, 3.9, []cell{{2, 0}, {2, 1}, {2, 2}, {2, 3}}},
		// A shallow line crosses into the next row partway along.
		{"a shallow line", 0.5, 0.7, 3.5, 1.7, []cell{{0, 0}, {1, 0}, {1, 1}, {2, 1}, {3, 1}}},
		// A diagonal through the corners takes a side neighbor at each, leaving no diagonal gap.
		{"a diagonal through corners", 0.5, 0.5, 2.5, 2.5, []cell{{0, 0}, {1, 0}, {1, 1}, {2, 1}, {2, 2}}},
		{"clipped at both ends", -5, 1.5, 9, 1.5, []cell{{0, 1}, {1, 1}, {2, 1}, {3, 1}}},
		{"entering from above", 1.5, -3, 1.5, 0.5, []cell{{1, 0}}},
		{"along the far edge", 0.5, 4, 2.5, 4, []cell{{0, 3}, {1, 3}, {2, 3}}},
		{"wholly outside", -3, -3, -1, 9, nil},
		{"not a number", math.NaN(), 0, 1, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := collectCells(tc.x0, tc.y0, tc.x1, tc.y1, 4, 4)
			if len(got) != len(tc.want) {
				t.Fatalf("cells = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cells = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// FuzzCellsAlong checks what every walk keeps to: it stays on the grid, visits no cell twice, steps to a side neighbor each time, and starts and ends in the cells of segment ends that lie on the grid.
func FuzzCellsAlong(f *testing.F) {
	f.Add(0.5, 0.5, 3.5, 1.5, 4, 4)
	f.Add(-10.0, 2.0, 30.0, 2.0, 8, 3)
	f.Add(0.0, 0.0, 7.0, 7.0, 7, 7)
	f.Add(3.999999, 0.000001, 0.000001, 3.999999, 4, 4)
	f.Add(1e9, -1e9, -1e9, 1e9, 5, 5)
	f.Fuzz(func(t *testing.T, x0, y0, x1, y1 float64, w, h int) {
		w, h = w%64, h%64
		if w <= 0 || h <= 0 {
			return
		}
		got := collectCells(x0, y0, x1, y1, w, h)
		seen := map[cell]bool{}
		for i, c := range got {
			if c.c < 0 || c.c >= w || c.r < 0 || c.r >= h {
				t.Fatalf("cell %v off the %dx%d grid in %v", c, w, h, got)
			}
			if seen[c] {
				t.Fatalf("cell %v visited twice in %v", c, got)
			}
			seen[c] = true
			if i > 0 {
				p := got[i-1]
				if d := abs(c.c-p.c) + abs(c.r-p.r); d != 1 {
					t.Fatalf("step from %v to %v is not to a side neighbor in %v", p, c, got)
				}
			}
		}
		inside := func(x, y float64) bool { return x >= 0 && y >= 0 && x < float64(w) && y < float64(h) }
		if inside(x0, y0) {
			if len(got) == 0 || got[0] != (cell{int(x0), int(y0)}) {
				t.Fatalf("walk from (%v, %v) starts at %v", x0, y0, got)
			}
		}
		if inside(x1, y1) {
			if len(got) == 0 || got[len(got)-1] != (cell{int(x1), int(y1)}) {
				t.Fatalf("walk to (%v, %v) ends at %v", x1, y1, got)
			}
		}
	})
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func BenchmarkCellsAlong(b *testing.B) {
	n := 0
	for b.Loop() {
		cellsAlong(0.5, 0.5, 255.5, 97.25, 256, 256, func(int, int) { n++ })
	}
	_ = n
}

func TestToolStrokeMode(t *testing.T) {
	type want struct {
		mode TileStrokeMode
		ok   bool
	}
	for _, tc := range []struct {
		tool                  Tool
		primary, right, alted want
	}{
		{ToolPaintTile, want{TileStrokePaint, true}, want{TileStrokeErase, true}, want{TileStrokePick, true}},
		{ToolEraseTile, want{TileStrokeErase, true}, want{TileStrokeErase, true}, want{TileStrokePick, true}},
		{ToolFillTile, want{}, want{}, want{TileStrokePick, true}},
		{ToolPickTile, want{TileStrokePick, true}, want{}, want{TileStrokePick, true}},
		{ToolTerrain, want{TileStrokeTerrain, true}, want{TileStrokeEraseTerrain, true}, want{TileStrokePick, true}},
		{ToolSelect, want{}, want{}, want{}},
		{ToolPlace, want{}, want{}, want{}},
	} {
		e := &Editor{Tool: tc.tool}
		for _, c := range []struct {
			name           string
			secondary, alt bool
			want           want
		}{{"primary", false, false, tc.primary}, {"right", true, false, tc.right}, {"alt", false, true, tc.alted}} {
			mode, ok := e.toolStrokeMode(c.secondary, c.alt)
			if ok != c.want.ok || ok && mode != c.want.mode {
				t.Errorf("%v %s: (%v, %v), want (%v, %v)", tc.tool, c.name, mode, ok, c.want.mode, c.want.ok)
			}
		}
	}
}

// One finger draws a stroke; a second finger cancels it and puts its cells back, so a two-finger pan that begins with one finger leaves the map as it was.
func TestTouchStrokeCancelsOnASecondFinger(t *testing.T) {
	doc := DefaultSceneDoc()
	doc.Layers[0].Tilemap = &sceneformat.TilemapDoc{TileWidth: 8, TileHeight: 8, Width: 4, Height: 4, Tileset: "tiles", Data: make([]int, 16)}
	frames := map[string]sprite.Frame{}
	for _, id := range []string{"1", "7"} {
		frames[id] = sprite.Frame{Name: id, W: 8, H: 8, Width: 8, Height: 8}
	}
	ed, err := New(doc, sceneformat.AtlasMap{"tiles": &sprite.Atlas{Image: core.ImageSource{URI: "asset:tiles.png"}, Frames: frames}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.SetCanvasSize(640, 480)
	ed.SetTool(ToolPaintTile)
	ed.ActiveTileID = 7
	begin := func(at core.Point, secondary, alt bool) {
		if mode, ok := ed.toolStrokeMode(secondary, alt); ok {
			ed.BeginTileStroke(mode, float32(at.X), float32(at.Y))
		}
	}
	end := func(at core.Point) {
		ed.ContinueTileStroke(float32(at.X), float32(at.Y))
		ed.EndTileStroke()
	}
	x0, y0 := ed.WorldToCanvas(4, 4)
	x1, y1 := ed.WorldToCanvas(20, 4)
	one := func(phase core.TouchPhase, x, y float32) core.TouchEvent {
		return core.TouchEvent{Phase: phase, Touches: []core.Touch{{ID: 1, Position: core.Point{X: core.Points(x), Y: core.Points(y)}, Changed: true}}}
	}
	touchStroke(one(core.TouchStart, x0, y0), begin, end, ed)
	touchStroke(one(core.TouchMove, x1, y1), begin, end, ed)
	data := ed.Doc.Layers[0].Tilemap.Data
	if data[0] != 7 || data[1] != 7 || data[2] != 7 {
		t.Fatalf("row 0 = %v after one finger moved across it, want 7s", data[:4])
	}
	touchStroke(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{
		{ID: 1, Position: core.Point{X: core.Points(x1), Y: core.Points(y1)}},
		{ID: 2, Position: core.Point{X: 300, Y: 300}, Changed: true},
	}}, begin, end, ed)
	if _, ok := ed.TileStrokeActive(); ok {
		t.Fatal("the stroke is still on after a second finger went down")
	}
	if data[0] != 0 || data[1] != 0 || data[2] != 0 {
		t.Fatalf("row 0 = %v after the stroke was cancelled, want it empty again", data[:4])
	}
	if ed.History.Len() != 0 {
		t.Fatalf("history holds %d steps after a cancelled stroke, want none", ed.History.Len())
	}

	// A TouchCancel with an empty Touches slice still cancels an active stroke and restores its cells.
	touchStroke(one(core.TouchStart, x0, y0), begin, end, ed)
	touchStroke(one(core.TouchMove, x1, y1), begin, end, ed)
	touchStroke(core.TouchEvent{Phase: core.TouchCancel}, begin, end, ed)
	if _, ok := ed.TileStrokeActive(); ok {
		t.Fatal("the stroke stayed active after TouchCancel with no touches")
	}
	if data[0] != 0 || data[1] != 0 || data[2] != 0 || ed.History.Len() != 0 {
		t.Fatalf("row 0 = %v, history = %d after empty TouchCancel, want empty and 0", data[:4], ed.History.Len())
	}

	// A lifted finger ends a stroke and records it.
	touchStroke(one(core.TouchStart, x0, y0), begin, end, ed)
	touchStroke(one(core.TouchEnd, x1, y1), begin, end, ed)
	if data[0] != 7 || data[2] != 7 || ed.History.Len() != 1 {
		t.Fatalf("row 0 = %v and %d history steps after a one-finger stroke, want 7s and one step", data[:4], ed.History.Len())
	}
}
