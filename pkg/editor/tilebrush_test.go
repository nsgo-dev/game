package editor_test

import (
	"math"
	"math/bits"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// brushEditor builds a 6x6 inline tilemap scene with 6 numbered tiles (1..6) for brush tests.
func brushEditor(t *testing.T) *editor.Editor {
	t.Helper()
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Brush Test",
		VirtualWidth:    320,
		VirtualHeight:   180,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "floor",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:  16,
					TileHeight: 16,
					Width:      6,
					Height:     6,
					Tileset:    "tiles",
					Data:       make([]int, 36),
				},
			},
		},
	}
	frames := map[string]sprite.Frame{}
	for id := 1; id <= 6; id++ {
		name := strconv.Itoa(id)
		frames[name] = sprite.Frame{
			Name: name, X: float32((id - 1) * 16), Y: 0, W: 16, H: 16, Width: 16, Height: 16,
		}
	}
	resolver := sceneformat.AtlasMap{
		"tiles": &sprite.Atlas{
			Image:  core.ImageSource{URI: "asset:tiles.png"},
			Frames: frames,
		},
	}
	ed, err := editor.New(doc, resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("floor")
	ed.SetCanvasSize(640, 480)
	return ed
}

func gridData(ed *editor.Editor) []int {
	return ed.Doc.Layers[0].Tilemap.Data
}

// Acceptance #1 (Rectangle fill & erase): dragged rectangle shows a live preview without mutating the map before release,
// commits all cells on release in one Undo step, and right-drag erases a rectangle in one Undo step.
func TestTileBrushRectangleFillErasePreviewAndUndo(t *testing.T) {
	ed := brushEditor(t)
	ed.SetTool(editor.ToolRectTile)
	ed.SetActiveTileID(2)
	before := slices.Clone(gridData(ed))

	// Drag from cell (1,1) to (3,2) -> 3x2 = 6 cells.
	x0, y0 := ed.WorldToCanvas(24, 24)
	x1, y1 := ed.WorldToCanvas(56, 40)
	if !ed.BeginTileStroke(editor.TileStrokeRect, x0, y0) {
		t.Fatal("BeginTileStroke(TileStrokeRect) returned false")
	}
	ed.ContinueTileStroke(x1, y1)

	// Before release: preview holds 6 cells, viewport draws preview ops, and Doc is still unchanged!
	preview := ed.TilePreview()
	if len(preview) != 6 {
		t.Fatalf("rect preview count = %d, want 6", len(preview))
	}
	if !slices.Equal(gridData(ed), before) || ed.History.Len() != 0 {
		t.Fatalf("rect preview mutated map or history before release: hist=%d", ed.History.Len())
	}
	if _, ok := hoverRect(drawn(ed).Ops, ed.SpecPalette(nil).Accent.Edge); !ok {
		t.Fatal("rect preview did not draw accent outline in viewport")
	}

	// Release commits all 6 cells as one undo step.
	ed.EndTileStroke()
	if len(ed.TilePreview()) != 0 {
		t.Fatal("preview remained after EndTileStroke")
	}
	if ed.History.Len() != 1 {
		t.Fatalf("history len = %d after rect fill, want 1", ed.History.Len())
	}
	data := gridData(ed)
	for r := range 6 {
		for c := range 6 {
			want := 0
			if c >= 1 && c <= 3 && r >= 1 && r <= 2 {
				want = 2
			}
			if got := data[r*6+c]; got != want {
				t.Fatalf("cell (%d,%d) = %d after rect fill, want %d", c, r, got, want)
			}
		}
	}

	// Drag a rectangle erase over (2,1)-(3,2) -> 4 cells cleared in one undo step.
	ex0, ey0 := ed.WorldToCanvas(40, 24)
	ex1, ey1 := ed.WorldToCanvas(56, 40)
	if !ed.BeginTileStroke(editor.TileStrokeRectErase, ex0, ey0) {
		t.Fatal("BeginTileStroke(TileStrokeRectErase) returned false")
	}
	ed.ContinueTileStroke(ex1, ey1)
	if got := ed.TilePreview(); len(got) != 4 || !got[0].Erase {
		t.Fatalf("rect erase preview = %+v, want 4 erase cells", got)
	}
	if _, ok := hoverRect(drawn(ed).Ops, ed.SpecPalette(nil).Removed.Edge); !ok {
		t.Fatal("rect erase preview did not draw red outline in viewport")
	}
	ed.EndTileStroke()
	if ed.History.Len() != 2 {
		t.Fatalf("history len = %d after rect erase, want 2", ed.History.Len())
	}
	if data[1*6+1] != 2 || data[2*6+1] != 2 || data[1*6+2] != 0 || data[2*6+3] != 0 {
		t.Fatalf("unexpected map after rect erase: %v", data)
	}

	// One Undo reverts the whole erase, a second Undo reverts the whole fill.
	if !ed.Undo() {
		t.Fatal("Undo rect erase failed")
	}
	if data[1*6+2] != 2 || data[2*6+3] != 2 {
		t.Fatalf("Undo did not restore rect erase: %v", data)
	}
	if !ed.Undo() || !slices.Equal(gridData(ed), before) {
		t.Fatalf("Undo did not revert whole rect fill: %v", gridData(ed))
	}
	if ed.History.CanUndo() {
		t.Fatal("unexpected extra undo steps")
	}
}

// Acceptance #1 (Line brush): Bresenham line paints & erases expected cells, previews live, and reverts in one Undo.
func TestTileBrushLinePreviewAndUndo(t *testing.T) {
	ed := brushEditor(t)
	ed.SetTool(editor.ToolLineTile)
	ed.SetActiveTileID(3)
	before := slices.Clone(gridData(ed))

	// Diagonal Bresenham line from (0,0) to (4,2): (0,0), (1,0), (2,1), (3,1), (4,2) -> wait: dx=4,dy=2 -> (0,0),(1,1),(2,1),(3,2),(4,2) or standard Bresenham:
	// Let's check (0,0) -> (4,4) (exact 5 diagonal cells) and (0,0) -> (4,1) (shallow line).
	x0, y0 := ed.WorldToCanvas(8, 8)
	x1, y1 := ed.WorldToCanvas(72, 72)
	if !ed.BeginTileStroke(editor.TileStrokeLine, x0, y0) {
		t.Fatal("BeginTileStroke(TileStrokeLine) returned false")
	}
	ed.ContinueTileStroke(x1, y1)
	if got := ed.TilePreview(); len(got) != 5 {
		t.Fatalf("diagonal line preview len = %d, want 5", len(got))
	}
	if !slices.Equal(gridData(ed), before) {
		t.Fatal("line preview mutated map before release")
	}
	ed.EndTileStroke()
	if ed.History.Len() != 1 {
		t.Fatalf("history len = %d, want 1", ed.History.Len())
	}
	data := gridData(ed)
	for r := range 6 {
		for c := range 6 {
			want := 0
			if c == r && c <= 4 {
				want = 3
			}
			if got := data[r*6+c]; got != want {
				t.Fatalf("cell (%d,%d) = %d after diagonal line, want %d", c, r, got, want)
			}
		}
	}

	// Erase part of the line with TileStrokeLineErase from (1,1) to (3,3).
	ex0, ey0 := ed.WorldToCanvas(24, 24)
	ex1, ey1 := ed.WorldToCanvas(56, 56)
	ed.BeginTileStroke(editor.TileStrokeLineErase, ex0, ey0)
	ed.ContinueTileStroke(ex1, ey1)
	ed.EndTileStroke()
	if data[0] != 3 || data[1*6+1] != 0 || data[2*6+2] != 0 || data[3*6+3] != 0 || data[4*6+4] != 3 {
		t.Fatalf("after line erase diagonal = [%d %d %d %d %d], want [3 0 0 0 3]",
			data[0], data[1*6+1], data[2*6+2], data[3*6+3], data[4*6+4])
	}

	// Undo erase, then Undo line paint.
	ed.Undo()
	if data[2*6+2] != 3 {
		t.Fatal("Undo did not restore line erase")
	}
	ed.Undo()
	if !slices.Equal(gridData(ed), before) {
		t.Fatalf("Undo did not revert whole line paint: %v", gridData(ed))
	}
}

// Acceptance #1 & #2 (Stamp brush & region clipboard):
// 1) Select a rectangle of tiles from the palette and stamp it onto the map (one undo step, with live preview).
// 2) Copy a region of the map (including empty cells) and paste it elsewhere with the exact same tile IDs.
// 3) Cut a region of the map (clears source as one undo step) and paste it elsewhere.
func TestTileBrushStampPaletteAndRegionCopyCutPaste(t *testing.T) {
	ed := brushEditor(t)
	ed.SetTool(editor.ToolStampTile)

	// Palette has 6 tiles (1..6), 16x16 in a 224pt panel -> 6 tiles in row 0.
	// Select a 3x1 rectangle (cols 0..2, row 0) -> tiles [1, 2, 3].
	stamp, err := ed.SelectPaletteStamp(0, 0, 2, 0)
	if err != nil {
		t.Fatalf("SelectPaletteStamp: %v", err)
	}
	if stamp.Width != 3 || stamp.Height != 1 || !slices.Equal(stamp.Tiles, []int{1, 2, 3}) {
		t.Fatalf("palette stamp = %+v, want 3x1 [1 2 3]", stamp)
	}

	// Stamp at (0,0) via stroke and verify live preview + single undo step.
	before := slices.Clone(gridData(ed))
	sx, sy := ed.WorldToCanvas(8, 8)
	if !ed.BeginTileStroke(editor.TileStrokeStamp, sx, sy) {
		t.Fatal("BeginTileStroke(TileStrokeStamp) returned false")
	}
	if got := ed.TilePreview(); len(got) != 3 {
		t.Fatalf("stamp preview len = %d, want 3", len(got))
	}
	if !slices.Equal(gridData(ed), before) {
		t.Fatal("stamp preview mutated map before release")
	}
	ed.EndTileStroke()
	if got := gridData(ed)[0:3]; !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("row 0 after stamp = %v, want [1 2 3]", got)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("history len = %d after stamp, want 1", ed.History.Len())
	}
	ed.Undo()
	if !slices.Equal(gridData(ed), before) {
		t.Fatalf("Undo did not revert stamp: %v", gridData(ed))
	}
	ed.Redo()

	// Add a second row to make a 2x2 asymmetric region with an empty cell:
	// (0,0)=1, (1,0)=2, (0,1)=0, (1,1)=5.
	if err := ed.PaintTile("floor", 1, 1, 5); err != nil {
		t.Fatalf("PaintTile: %v", err)
	}
	// Pre-fill target area (3,3)-(4,4) with tile 6 so pasting must overwrite empty cell (3,4) to 0 too!
	if err := ed.FillTileRect("floor", 3, 3, 4, 4, 6); err != nil {
		t.Fatalf("FillTileRect: %v", err)
	}

	// Copy region (0,0)-(1,1) and paste at (3,3).
	copied, err := ed.CopyTileRegion("floor", 0, 0, 1, 1)
	if err != nil {
		t.Fatalf("CopyTileRegion: %v", err)
	}
	if copied.Width != 2 || copied.Height != 2 || !slices.Equal(copied.Tiles, []int{1, 2, 0, 5}) {
		t.Fatalf("copied stamp = %+v, want 2x2 [1 2 0 5]", copied)
	}
	if err := ed.PasteTileRegion("floor", 3, 3); err != nil {
		t.Fatalf("PasteTileRegion: %v", err)
	}
	data := gridData(ed)
	gotPasted := []int{data[3*6+3], data[3*6+4], data[4*6+3], data[4*6+4]}
	if !slices.Equal(gotPasted, []int{1, 2, 0, 5}) {
		t.Fatalf("pasted region at (3,3) = %v, want [1 2 0 5]", gotPasted)
	}

	// One Undo reverts the whole paste back to [6 6 6 6].
	ed.Undo()
	gotAfterUndo := []int{data[3*6+3], data[3*6+4], data[4*6+3], data[4*6+4]}
	if !slices.Equal(gotAfterUndo, []int{6, 6, 6, 6}) {
		t.Fatalf("after Undo of paste = %v, want [6 6 6 6]", gotAfterUndo)
	}

	// Cut region (0,0)-(1,1): source cells cleared in one undo step, clipboard holds [1 2 0 5].
	histBeforeCut := ed.History.Index()
	cut, err := ed.CutTileRegion("floor", 0, 0, 1, 1)
	if err != nil {
		t.Fatalf("CutTileRegion: %v", err)
	}
	if !slices.Equal(cut.Tiles, []int{1, 2, 0, 5}) {
		t.Fatalf("cut stamp = %+v, want [1 2 0 5]", cut)
	}
	if ed.History.Index() != histBeforeCut+1 {
		t.Fatalf("CutTileRegion recorded %d steps, want 1", ed.History.Index()-histBeforeCut)
	}
	if data[0] != 0 || data[1] != 0 || data[1*6+1] != 0 {
		t.Fatalf("source region not cleared after cut: %v", data)
	}
	// Paste cut region at (4,0) — clips to map width 6 (cols 4..5, rows 0..1).
	if err := ed.PasteTileRegion("floor", 4, 0); err != nil {
		t.Fatalf("PasteTileRegion after cut: %v", err)
	}
	gotCutPaste := []int{data[0*6+4], data[0*6+5], data[1*6+4], data[1*6+5]}
	if !slices.Equal(gotCutPaste, []int{1, 2, 0, 5}) {
		t.Fatalf("pasted cut region at (4,0) = %v, want [1 2 0 5]", gotCutPaste)
	}
	// Undo paste, then Undo cut restores source region.
	ed.Undo()
	ed.Undo()
	gotRestored := []int{data[0*6+0], data[0*6+1], data[1*6+0], data[1*6+1]}
	if !slices.Equal(gotRestored, []int{1, 2, 0, 5}) {
		t.Fatalf("after undoing cut source = %v, want [1 2 0 5]", gotRestored)
	}
}

// Dragging a Stamp brush tiles the stamp pattern aligned to the stroke's anchor cell as one undo step.
func TestTileBrushStampDragTilesPatternAligned(t *testing.T) {
	ed := brushEditor(t)
	ed.SetTool(editor.ToolStampTile)
	if err := ed.SetTileStamp(editor.TileStamp{Width: 2, Height: 1, Tiles: []int{1, 2}}); err != nil {
		t.Fatalf("SetTileStamp: %v", err)
	}
	x0, y0 := ed.WorldToCanvas(8, 8)
	x1, y1 := ed.WorldToCanvas(56, 8) // col 0 to col 3 -> covers cols 0..4 with 2x1 stamp
	ed.BeginTileStroke(editor.TileStrokeStamp, x0, y0)
	ed.ContinueTileStroke(x1, y1)
	ed.EndTileStroke()
	if got := gridData(ed)[0:5]; !slices.Equal(got, []int{1, 2, 1, 2, 1}) {
		t.Fatalf("tiled stamp row = %v, want [1 2 1 2 1]", got)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("history len = %d, want 1", ed.History.Len())
	}
	ed.Undo()
	if !slices.Equal(gridData(ed)[0:5], []int{0, 0, 0, 0, 0}) {
		t.Fatalf("Undo did not revert tiled stamp drag: %v", gridData(ed)[0:5])
	}
}

// Acceptance #1 & #3 (Random brush): paints deterministic weighted picks for a fixed seed,
// previews live before release, and reverts in one Undo step.
func TestTileBrushRandomBrushFixedSeedDeterminismAndUndo(t *testing.T) {
	pool := []editor.WeightedTile{
		{TileID: 1, Weight: 3},
		{TileID: 2, Weight: 2},
		{TileID: 4, Weight: 1},
	}

	runStroke := func(seed uint64) []int {
		ed := brushEditor(t)
		ed.SetTool(editor.ToolRandomTile)
		if err := ed.SetRandomBrushTiles(pool); err != nil {
			t.Fatalf("SetRandomBrushTiles: %v", err)
		}
		ed.SetRandomBrushSeed(seed)
		before := slices.Clone(gridData(ed))

		// Paint rows 0..3 by dragging back and forth in one stroke.
		x0, y0 := ed.WorldToCanvas(8, 8)
		x1, y1 := ed.WorldToCanvas(88, 8)
		x2, y2 := ed.WorldToCanvas(88, 24)
		x3, y3 := ed.WorldToCanvas(8, 24)
		if !ed.BeginTileStroke(editor.TileStrokeRandom, x0, y0) {
			t.Fatal("BeginTileStroke(TileStrokeRandom) returned false")
		}
		ed.ContinueTileStroke(x1, y1)
		ed.ContinueTileStroke(x2, y2)
		ed.ContinueTileStroke(x3, y3)

		// Live preview must show all 12 cells without mutating Doc before release.
		preview := ed.TilePreview()
		if len(preview) != 12 {
			t.Fatalf("random preview len = %d, want 12", len(preview))
		}
		if !slices.Equal(gridData(ed), before) || ed.History.Len() != 0 {
			t.Fatal("random preview mutated map or history before release")
		}

		ed.EndTileStroke()
		if ed.History.Len() != 1 {
			t.Fatalf("random stroke recorded %d undo steps, want 1", ed.History.Len())
		}
		result := slices.Clone(gridData(ed))

		// Every painted cell must come from the configured weighted pool.
		for i := range 12 {
			v := result[i]
			if v != 1 && v != 2 && v != 4 {
				t.Fatalf("painted cell %d = %d, want one of {1, 2, 4}", i, v)
			}
		}
		// One Undo reverts the whole stroke.
		if !ed.Undo() || !slices.Equal(gridData(ed), before) {
			t.Fatalf("Undo did not revert random stroke: %v", gridData(ed))
		}
		return result
	}

	first := runStroke(42)
	second := runStroke(42)
	if !slices.Equal(first, second) {
		t.Fatalf("fixed seed 42 produced different maps:\nfirst:  %v\nsecond: %v", first, second)
	}
	other := runStroke(999)
	if slices.Equal(first, other) {
		t.Fatalf("different seeds 42 and 999 produced identical maps: %v", first)
	}
}

// Mounted UI end-to-end test: toolbar buttons, shortcuts (u/l/t/n, mod+c/x/v), Escape cancel mid-drag,
// palette rectangle selection, and random seed/weight controls.
func TestTileBrushesMountedUIEscapeCancelAndClipboard(t *testing.T) {
	ed := brushEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// 1. Shortcuts switch between all four new tools.
	for _, tc := range []struct {
		key  string
		want editor.Tool
	}{
		{"u", editor.ToolRectTile},
		{"l", editor.ToolLineTile},
		{"c", editor.ToolStampTile},
		{"n", editor.ToolRandomTile},
	} {
		app.Key(tc.key)
		if ed.Tool != tc.want {
			t.Fatalf("shortcut %q: Tool = %v, want %v", tc.key, ed.Tool, tc.want)
		}
	}

	// 2. Rect drag cancelled with Escape before MouseUp leaves map & history untouched.
	runEditorCommand(t, app, ed, editor.CmdToolRectTile)
	app.Press("id:tile-palette-4")
	before := slices.Clone(gridData(ed))
	app.MouseDownAt("id:editor-viewport-frame", canvasAt(ed, 8, 8), core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 40, 40))
	if len(ed.TilePreview()) != 9 {
		t.Fatalf("mid-drag rect preview = %d cells, want 9", len(ed.TilePreview()))
	}
	app.Key("escape")
	if _, active := ed.TileStrokeActive(); active || len(ed.TilePreview()) != 0 {
		t.Fatal("Escape did not cancel active rect stroke")
	}
	app.MouseUpAt("id:editor-viewport-frame", canvasAt(ed, 40, 40), core.MouseButtonLeft)
	if !slices.Equal(gridData(ed), before) || ed.History.Len() != 0 {
		t.Fatalf("Escape-cancelled rect drag edited map or history: hist=%d data=%v", ed.History.Len(), gridData(ed))
	}

	// 3. Completed Rect mouse drag paints 2x2 and one Undo reverts it.
	app.MouseDownAt("id:editor-viewport-frame", canvasAt(ed, 8, 8), core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 24, 24))
	app.MouseUpAt("id:editor-viewport-frame", canvasAt(ed, 24, 24), core.MouseButtonLeft)
	data := gridData(ed)
	if data[0] != 4 || data[1] != 4 || data[6] != 4 || data[7] != 4 || ed.History.Len() != 1 {
		t.Fatalf("rect drag result: (0,0..1,1)=[%d %d %d %d], hist=%d", data[0], data[1], data[6], data[7], ed.History.Len())
	}

	// 4. Stamp tool: right-drag selects a 2x2 region on the map, Cmd+C copies, hover at (3,3) and Cmd+V pastes!
	runEditorCommand(t, app, ed, editor.CmdToolStampTile)
	if !app.Exists("id:tile-stamp-controls") {
		t.Fatal("stamp controls missing when ToolStampTile is active")
	}
	app.MouseDownAt("id:editor-viewport-frame", canvasAt(ed, 8, 8), core.MouseButtonRight)
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 24, 24))
	app.MouseUpAt("id:editor-viewport-frame", canvasAt(ed, 24, 24), core.MouseButtonRight)
	sel, ok := ed.TileSelection()
	if !ok || sel != (editor.TileRect{Col0: 0, Row0: 0, Col1: 1, Row1: 1}) {
		t.Fatalf("right-drag selection = (%+v, %v), want (0,0)-(1,1)", sel, ok)
	}
	app.Key("meta+c")
	clip, ok := ed.TileClipboard()
	if !ok || !slices.Equal(clip.Tiles, []int{4, 4, 4, 4}) {
		t.Fatalf("clipboard after Cmd+C = (%+v, %v), want [4 4 4 4]", clip, ok)
	}
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 56, 56)) // cell (3,3)
	app.Key("meta+v")
	if data[3*6+3] != 4 || data[3*6+4] != 4 || data[4*6+3] != 4 || data[4*6+4] != 4 {
		t.Fatalf("Cmd+V did not paste at hovered cell (3,3): %v", data)
	}

	// Palette rectangle selection via Rect toggle button: click tile 1, enable Rect, click tile 3 -> 3x1 stamp [1 2 3].
	app.Press("id:tile-palette-1")
	app.Press("id:tile-stamp-range")
	app.Press("id:tile-palette-3")
	st, ok := ed.TileStamp()
	if !ok || st.Width != 3 || st.Height != 1 || !slices.Equal(st.Tiles, []int{1, 2, 3}) {
		t.Fatalf("UI palette range stamp = (%+v, %v), want 3x1 [1 2 3]", st, ok)
	}

	// 5. Random tool UI: toggle tiles 1 and 2, adjust weight, set seed via text field.
	runEditorCommand(t, app, ed, editor.CmdToolRandomTile)
	if !app.Exists("id:tile-random-controls") {
		t.Fatal("random controls missing when ToolRandomTile is active")
	}
	app.Press("id:tile-palette-1")
	app.Press("id:tile-palette-2")
	app.Press("id:tile-random-inc-1")
	pool := ed.RandomBrushTiles()
	if len(pool) != 2 || pool[0].TileID != 1 || pool[0].Weight != 1.5 || pool[1].TileID != 2 || pool[1].Weight != 1 {
		t.Fatalf("random pool after UI toggles = %+v", pool)
	}
	app.Focus("id:tile-random-seed")
	app.Post(func() { ed.SetRandomBrushSeed(1337) })
	app.Settle()
	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "seed 1337") {
		t.Fatalf("status bar = %q, want seed 1337", got)
	}
}

// Layered tilemap support: Rect, Line, Copy/Cut/Paste (preserving per-cell flips), Random, and deterministic YAML round-trip.
func TestTileBrushesOnLayeredTilemapPreservesFlipsAndRoundTrips(t *testing.T) {
	ed := layeredEditor(t)
	deco := ed.Doc.Layers[0].Tilemap.TileLayer("deco")
	ed.SetActiveTileLayer("deco")
	ed.SetActiveTileFlip(tilemap.FlipHorizontal)

	// Fill 2x2 rect on deco with tile 2 (local ID 1h).
	if err := ed.FillTileRect("ground", 0, 0, 1, 1, 2); err != nil {
		t.Fatalf("FillTileRect layered: %v", err)
	}
	if !slices.Equal(deco.Rows, []string{"1h 1h .", "1h 1h ."}) {
		t.Fatalf("deco after FillTileRect = %q", deco.Rows)
	}

	// Draw vertical line on col 2 with tile 3 (local ID 2v).
	ed.SetActiveTileFlip(tilemap.FlipVertical)
	if err := ed.DrawTileLine("ground", 2, 0, 2, 1, 3); err != nil {
		t.Fatalf("DrawTileLine layered: %v", err)
	}
	if !slices.Equal(deco.Rows, []string{"1h 1h 2v", "1h 1h 2v"}) {
		t.Fatalf("deco after DrawTileLine = %q", deco.Rows)
	}

	// Copy 2x1 region (1,0)-(2,0) -> holds [1h, 2v], change ActiveTileFlip to 0, paste at (0,1) -> preserves exact flips!
	if _, err := ed.CopyTileRegion("ground", 1, 0, 2, 0); err != nil {
		t.Fatalf("CopyTileRegion layered: %v", err)
	}
	ed.SetActiveTileFlip(0)
	if err := ed.PasteTileRegion("ground", 0, 1); err != nil {
		t.Fatalf("PasteTileRegion layered: %v", err)
	}
	if deco.Rows[1] != "1h 2v 2v" {
		t.Fatalf("deco row 1 after paste = %q, want %q", deco.Rows[1], "1h 2v 2v")
	}

	// Deterministic YAML round-trip check.
	encoded, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	reparsed, err := sceneformat.Parse(encoded)
	if err != nil {
		t.Fatalf("sceneformat.Parse: %v", err)
	}
	reencoded, err := sceneformat.Encode(reparsed)
	if err != nil {
		t.Fatalf("sceneformat.Encode: %v", err)
	}
	if string(encoded) != string(reencoded) {
		t.Fatal("YAML round-trip was not deterministic")
	}
}

// Validation and guard rails for random brush, stamps, locked/hidden layers, and playing mode.
func TestTileBrushValidationAndGuards(t *testing.T) {
	ed := brushEditor(t)

	if err := ed.SetTileStamp(editor.TileStamp{Width: 0, Height: 1}); err == nil {
		t.Fatal("SetTileStamp accepted zero width")
	}
	if err := ed.SetTileStamp(editor.TileStamp{Width: 2, Height: 2, Tiles: []int{1, 2}}); err == nil {
		t.Fatal("SetTileStamp accepted mismatched tile slice length")
	}
	if err := ed.SetRandomBrushTiles([]editor.WeightedTile{{TileID: 1, Weight: 0}}); err == nil {
		t.Fatal("SetRandomBrushTiles accepted zero weight")
	}
	if err := ed.SetRandomBrushTiles([]editor.WeightedTile{{TileID: 1, Weight: 1}, {TileID: 1, Weight: 2}}); err == nil {
		t.Fatal("SetRandomBrushTiles accepted duplicate tile ID")
	}
	if err := ed.PasteTileRegion("floor", 0, 0); err == nil {
		t.Fatal("PasteTileRegion succeeded with empty clipboard")
	}

	// Locked layer refuses edit strokes & Cut, while allowing Copy and SelectRegion.
	if err := ed.SetLayerLocked("floor", true); err != nil {
		t.Fatalf("SetLayerLocked: %v", err)
	}
	x, y := ed.WorldToCanvas(8, 8)
	if ed.BeginTileStroke(editor.TileStrokeRect, x, y) {
		t.Fatal("BeginTileStroke(TileStrokeRect) allowed on locked layer")
	}
	if err := ed.FillTileRect("floor", 0, 0, 1, 1, 1); err == nil {
		t.Fatal("FillTileRect allowed on locked layer")
	}
	if _, err := ed.CutTileRegion("floor", 0, 0, 1, 1); err == nil {
		t.Fatal("CutTileRegion allowed on locked layer")
	}
	if _, err := ed.CopyTileRegion("floor", 0, 0, 1, 1); err != nil {
		t.Fatalf("CopyTileRegion refused locked layer: %v", err)
	}
	if !ed.BeginTileStroke(editor.TileStrokeSelectRegion, x, y) {
		t.Fatal("BeginTileStroke(TileStrokeSelectRegion) refused locked layer")
	}
	ed.EndTileStroke()
}

func TestTileBrushHardeningGuards(t *testing.T) {
	ed := brushEditor(t)

	// 1. TileStamp multiplication overflow (Width*Height wraps to 0) must be rejected.
	bigDim := 1 << (bits.UintSize / 2)
	overflowStamp := editor.TileStamp{Width: bigDim, Height: bigDim, Tiles: []int{}}
	if !overflowStamp.Empty() {
		t.Fatal("overflow TileStamp reported Empty() == false")
	}
	if err := ed.SetTileStamp(overflowStamp); err == nil {
		t.Fatal("SetTileStamp accepted overflowing dimensions")
	}
	if err := ed.StampTiles("floor", 0, 0, overflowStamp); err == nil {
		t.Fatal("StampTiles accepted overflowing dimensions")
	}

	// 2. Non-finite coordinates in BeginTileStroke / ContinueTileStroke must be ignored without corrupting the map.
	ed.SetTool(editor.ToolRectTile)
	ed.SetActiveTileID(2)
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	if ed.BeginTileStroke(editor.TileStrokeRect, nan, 10) {
		t.Fatal("BeginTileStroke accepted NaN")
	}
	if ed.BeginTileStroke(editor.TileStrokeRect, inf, 10) {
		t.Fatal("BeginTileStroke accepted +Inf")
	}
	x0, y0 := ed.WorldToCanvas(8, 8)
	if !ed.BeginTileStroke(editor.TileStrokeRect, x0, y0) {
		t.Fatal("BeginTileStroke valid rect failed")
	}
	ed.ContinueTileStroke(nan, nan)
	ed.ContinueTileStroke(inf, -inf)
	if got := ed.TilePreview(); len(got) != 1 {
		t.Fatalf("non-finite ContinueTileStroke expanded rect preview to %d cells, want 1", len(got))
	}
	// 3. Changing ActiveTileID mid-stroke must not alter the captured tile ID of an in-progress Rect stroke.
	ed.SetActiveTileID(5)
	x1, y1 := ed.WorldToCanvas(24, 8)
	ed.ContinueTileStroke(x1, y1)
	for _, cell := range ed.TilePreview() {
		if cell.Tile != 2 {
			t.Fatalf("mid-stroke ActiveTileID change mutated preview tile to %d, want captured 2", cell.Tile)
		}
	}
	ed.EndTileStroke()
	if got := gridData(ed)[0:2]; !slices.Equal(got, []int{2, 2}) {
		t.Fatalf("committed rect after mid-stroke ActiveTileID change = %v, want [2 2]", got)
	}

	// 4. Nil receiver guards on FillTileRect, DrawTileLine, StampTiles.
	var nilEd *editor.Editor
	if err := nilEd.FillTileRect("floor", 0, 0, 1, 1, 1); err == nil {
		t.Fatal("nil Editor FillTileRect succeeded")
	}
	if err := nilEd.DrawTileLine("floor", 0, 0, 1, 1, 1); err == nil {
		t.Fatal("nil Editor DrawTileLine succeeded")
	}
	if err := nilEd.StampTiles("floor", 0, 0, editor.TileStamp{Width: 1, Height: 1, Tiles: []int{1}}); err == nil {
		t.Fatal("nil Editor StampTiles succeeded")
	}

	// 5. Overflowing float64 total weight in random brush pool must be refused.
	if err := ed.SetRandomBrushTiles([]editor.WeightedTile{
		{TileID: 1, Weight: math.MaxFloat64},
		{TileID: 2, Weight: math.MaxFloat64},
	}); err == nil {
		t.Fatal("SetRandomBrushTiles accepted +Inf weight sum")
	}
	if err := ed.SetRandomBrushTiles([]editor.WeightedTile{{TileID: 1, Weight: math.MaxFloat64}}); err != nil {
		t.Fatalf("SetRandomBrushTiles single MaxFloat64 failed: %v", err)
	}
	if err := ed.SetRandomBrushTileWeight(2, math.MaxFloat64); err == nil {
		t.Fatal("SetRandomBrushTileWeight accepted +Inf weight sum")
	}

	// 6. Escape must not swallow/clear a hidden tile selection while a non-tile tool is active.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	app.Post(func() {
		ed.SetTileSelection(0, 0, 1, 1)
		ed.SetTool(editor.ToolSelect)
	})
	app.Settle()
	app.Key("escape")
	if _, ok := ed.TileSelection(); !ok {
		t.Fatal("Escape cleared tile selection while ToolSelect (non-tile tool) was active")
	}
}
