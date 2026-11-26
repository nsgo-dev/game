package editor_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// floorEditor is the test scene with the floor layer, a 4x4 map of 16-point tiles framed by tile 1, active and the canvas laid out.
func floorEditor(t *testing.T) *editor.Editor {
	t.Helper()
	ed := newTestEditor(t)
	ed.SetActiveLayer("floor")
	ed.SetCanvasSize(640, 480)
	return ed
}

func floorData(ed *editor.Editor) []int { return ed.Doc.Layers[0].Tilemap.Data }

// canvasAt returns the canvas point over world point (wx, wy).
func canvasAt(ed *editor.Editor, wx, wy float32) core.Point {
	x, y := ed.WorldToCanvas(wx, wy)
	return core.Point{X: core.Points(x), Y: core.Points(y)}
}

// The issue's first acceptance item, through the API: a drag across a row paints the whole row, and one Undo removes it.
func TestTileStrokePaintsARowAsOneUndoStep(t *testing.T) {
	ed := floorEditor(t)
	ed.SetTool(editor.ToolPaintTile)
	ed.SetActiveTileID(2)
	before := slices.Clone(floorData(ed))

	x0, y0 := ed.WorldToCanvas(3, 20)
	if !ed.BeginTileStroke(editor.TileStrokePaint, x0, y0) {
		t.Fatal("BeginTileStroke refused a paint stroke on the floor layer")
	}
	// One move across the whole row, as a fast drag reports it: the cells between are painted too.
	x1, y1 := ed.WorldToCanvas(60, 22)
	ed.ContinueTileStroke(x1, y1)
	if got := floorData(ed)[4:8]; !slices.Equal(got, []int{2, 2, 2, 2}) {
		t.Fatalf("row 1 = %v while the stroke is under way, want it painted", got)
	}
	if ed.History.Len() != 0 {
		t.Fatalf("history holds %d steps before the stroke ends, want none", ed.History.Len())
	}
	ed.EndTileStroke()
	if ed.History.Len() != 1 || ed.History.UndoLabel() != "Paint 4 tiles" {
		t.Fatalf("history = %d steps, undo %q, want one step \"Paint 4 tiles\"", ed.History.Len(), ed.History.UndoLabel())
	}
	if !ed.Undo() || !slices.Equal(floorData(ed), before) {
		t.Fatalf("after one Undo the map is %v, want %v", floorData(ed), before)
	}
	if !ed.Redo() || !slices.Equal(floorData(ed)[4:8], []int{2, 2, 2, 2}) {
		t.Fatalf("after Redo row 1 = %v, want it painted", floorData(ed)[4:8])
	}
}

// A diagonal drag paints a 4-connected line across the map, with no cell skipped where the pointer jumped.
func TestTileStrokeLeavesNoGapsOnADiagonal(t *testing.T) {
	ed := floorEditor(t)
	ed.SetTool(editor.ToolPaintTile)
	ed.SetActiveTileID(3)
	x0, y0 := ed.WorldToCanvas(2, 2)
	x1, y1 := ed.WorldToCanvas(62, 62)
	ed.BeginTileStroke(editor.TileStrokePaint, x0, y0)
	ed.ContinueTileStroke(x1, y1)
	ed.EndTileStroke()
	data := floorData(ed)
	for i := range 4 {
		if data[i*4+i] != 3 {
			t.Fatalf("diagonal cell (%d,%d) = %d, want 3; map %v", i, i, data[i*4+i], data)
		}
		if i < 3 && data[i*4+i+1] != 3 && data[(i+1)*4+i] != 3 {
			t.Fatalf("no side neighbor joins (%d,%d) to (%d,%d); map %v", i, i, i+1, i+1, data)
		}
	}
}

// An erase stroke clears what it crosses; a stroke that changes nothing records nothing; a cancelled stroke puts its cells back and records nothing.
func TestTileStrokeEraseNoopAndCancel(t *testing.T) {
	ed := floorEditor(t)
	ed.SetTool(editor.ToolEraseTile)
	x0, y0 := ed.WorldToCanvas(2, 8)
	x1, y1 := ed.WorldToCanvas(62, 8)
	ed.BeginTileStroke(editor.TileStrokeErase, x0, y0)
	ed.ContinueTileStroke(x1, y1)
	ed.EndTileStroke()
	if got := floorData(ed)[0:4]; !slices.Equal(got, []int{0, 0, 0, 0}) || ed.History.UndoLabel() != "Erase 4 tiles" {
		t.Fatalf("row 0 = %v, undo %q, want it erased as \"Erase 4 tiles\"", got, ed.History.UndoLabel())
	}

	// Erasing the erased row again changes nothing and records nothing.
	ed.BeginTileStroke(editor.TileStrokeErase, x0, y0)
	ed.ContinueTileStroke(x1, y1)
	ed.EndTileStroke()
	if ed.History.Len() != 1 {
		t.Fatalf("history holds %d steps after a stroke that changed nothing, want 1", ed.History.Len())
	}

	before := slices.Clone(floorData(ed))
	ed.SetTool(editor.ToolPaintTile)
	ed.BeginTileStroke(editor.TileStrokePaint, x0, y0)
	ed.ContinueTileStroke(x1, y1)
	if mode, ok := ed.TileStrokeActive(); !ok || mode != editor.TileStrokePaint {
		t.Fatalf("TileStrokeActive = (%v, %v), want a paint stroke", mode, ok)
	}
	ed.CancelTileStroke()
	if !slices.Equal(floorData(ed), before) || ed.History.Len() != 1 {
		t.Fatalf("after a cancel the map is %v with %d steps, want %v with 1", floorData(ed), ed.History.Len(), before)
	}
}

// Undo, a tool change and a layer change in the middle of a stroke record it first, so the stroke is never left half applied and unrecorded.
func TestTileStrokeEndsBeforeOtherChanges(t *testing.T) {
	ed := floorEditor(t)
	ed.SetTool(editor.ToolPaintTile)
	ed.SetActiveTileID(2)
	before := slices.Clone(floorData(ed))
	x, y := ed.WorldToCanvas(24, 24)
	ed.BeginTileStroke(editor.TileStrokePaint, x, y)
	if !ed.Undo() {
		t.Fatal("Undo during a stroke did nothing")
	}
	if _, ok := ed.TileStrokeActive(); ok || !slices.Equal(floorData(ed), before) {
		t.Fatalf("after Undo during a stroke the stroke is on or the map is %v, want it ended and %v", floorData(ed), before)
	}

	for name, change := range map[string]func(){
		"tool":  func() { ed.SetTool(editor.ToolSelect) },
		"layer": func() { ed.SetActiveLayer("actors") },
		"view":  func() { ed.SetViewMode(editor.ViewMode3D) },
	} {
		ed.SetViewMode(editor.ViewMode2D)
		ed.SetActiveLayer("floor")
		ed.SetTool(editor.ToolPaintTile)
		steps := ed.History.Index()
		ed.BeginTileStroke(editor.TileStrokePaint, x, y)
		change()
		if _, ok := ed.TileStrokeActive(); ok || ed.History.Index() != steps+1 {
			t.Fatalf("a %s change during a stroke left it on or recorded %d steps, want it ended and one more step", name, ed.History.Index()-steps)
		}
		ed.Undo()
	}
}

func TestBeginTileStrokeRefuses(t *testing.T) {
	ed := floorEditor(t)
	x, y := ed.WorldToCanvas(24, 24)
	ed.SetActiveLayer("actors")
	if ed.BeginTileStroke(editor.TileStrokePaint, x, y) {
		t.Fatal("a stroke began on a layer without a tilemap")
	}
	ed.SetActiveLayer("floor")
	ed.SetViewMode(editor.ViewMode3D)
	if ed.BeginTileStroke(editor.TileStrokePaint, x, y) {
		t.Fatal("a stroke began in 3D")
	}
	ed.SetViewMode(editor.ViewMode2D)
	if !ed.BeginTileStroke(editor.TileStrokePaint, x, y) || ed.BeginTileStroke(editor.TileStrokeErase, x, y) {
		t.Fatal("want the first stroke to begin and a second one beside it refused")
	}
	ed.EndTileStroke()
	// A stroke that starts off the map paints once it reaches it.
	ox, oy := ed.WorldToCanvas(-40, 24)
	ed.SetActiveTileID(3)
	if !ed.BeginTileStroke(editor.TileStrokePaint, ox, oy) {
		t.Fatal("a stroke starting off the map was refused")
	}
	ed.ContinueTileStroke(x, y)
	ed.EndTileStroke()
	if got := floorData(ed)[4:6]; !slices.Equal(got, []int{3, 3}) {
		t.Fatalf("row 1 = %v after a stroke from off the map to (1,1), want 3, 3 in its first cells", floorData(ed)[4:8])
	}
}

// The issue's third acceptance item, through the API: picking a painted cell makes its tile the active tile; an empty cell leaves it.
func TestPickTile(t *testing.T) {
	ed := floorEditor(t)
	ed.SetActiveTileID(3)
	if ed.PickTile("floor", 1, 1) || ed.ActiveTileID != 3 {
		t.Fatalf("picking the empty cell (1,1) changed the active tile to %d", ed.ActiveTileID)
	}
	if !ed.PickTile("floor", 0, 0) || ed.ActiveTileID != 1 {
		t.Fatalf("picking (0,0), which holds tile 1, left the active tile %d", ed.ActiveTileID)
	}
	if ed.PickTile("floor", 9, 0) {
		t.Fatal("picking a cell off the map reported a tile")
	}
	if ed.History.Len() != 0 {
		t.Fatal("picking recorded an edit")
	}
}

// PickTile refuses while the scene plays, as the other tile edits do, and leaves the active tile as it was.
func TestPickTileRefusedWhilePlaying(t *testing.T) {
	ed := floorEditor(t)
	ed.SetActiveTileID(3)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1120, 680))
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		if ed.PickTile("floor", 0, 0) || ed.ActiveTileID != 3 {
			t.Errorf("PickTile while playing picked, ActiveTileID = %d, want 3", ed.ActiveTileID)
		}
		_ = ed.Stop()
	})
	app.Settle()
}

// The stroke API does nothing on a nil editor, as the rest of it does.
func TestTileStrokeOnNilEditor(t *testing.T) {
	var ed *editor.Editor
	ed.ContinueTileStroke(1, 1)
	ed.EndTileStroke()
	ed.CancelTileStroke()
	if ed.BeginTileStroke(editor.TileStrokePaint, 1, 1) || ed.PickTile("", 0, 0) {
		t.Fatal("a nil editor began a stroke or picked a tile")
	}
}

// The issue's second acceptance item, through the API: the hover outline follows the mouse, snapped to the cell under it.
func TestHoveredTileSnapsToCells(t *testing.T) {
	ed := floorEditor(t)
	x, y := ed.WorldToCanvas(37, 21)
	ed.SetTileHover(x, y, false)
	if _, _, ok := ed.HoveredTile(); ok {
		t.Fatal("the Select tool outlines a cell")
	}
	ed.SetTool(editor.ToolPaintTile)
	if col, row, ok := ed.HoveredTile(); !ok || col != 2 || row != 1 {
		t.Fatalf("HoveredTile = (%d, %d, %v), want (2, 1)", col, row, ok)
	}
	rev := ed.Revision()
	x2, y2 := ed.WorldToCanvas(39, 30)
	ed.SetTileHover(x2, y2, false)
	if ed.Revision() != rev {
		t.Fatal("a move inside the hovered cell redrew the viewport")
	}
	x3, y3 := ed.WorldToCanvas(50, 30)
	ed.SetTileHover(x3, y3, false)
	if col, row, ok := ed.HoveredTile(); !ok || col != 3 || row != 1 || ed.Revision() == rev {
		t.Fatalf("after moving into (3,1) HoveredTile = (%d, %d, %v), revision moved %v", col, row, ok, ed.Revision() != rev)
	}
	ox, oy := ed.WorldToCanvas(90, 30)
	ed.SetTileHover(ox, oy, false)
	if _, _, ok := ed.HoveredTile(); ok {
		t.Fatal("a cell off the map is outlined")
	}
	ed.SetTileHover(x, y, false)
	ed.ClearTileHover()
	if _, _, ok := ed.HoveredTile(); ok {
		t.Fatal("a cell is outlined after the mouse left")
	}
}

// hoverRect returns the rectangle the viewport strokes in color, in the layer's space, and whether it strokes one.
func hoverRect(ops []core.DrawOp, color core.Color) (core.DrawOp, bool) {
	stroking := false
	for _, op := range ops {
		switch op.Kind {
		case core.DrawStrokeColor:
			stroking = op.Color == color
		case core.DrawRect:
			if stroking {
				return op, true
			}
		}
	}
	return core.DrawOp{}, false
}

func drawn(ed *editor.Editor) *core.Drawing {
	cw, ch := ed.CanvasSize()
	d := &core.Drawing{}
	ed.DrawViewport(d, core.Size{Width: core.Points(cw), Height: core.Points(ch)})
	return d
}

// The viewport outlines the hovered cell on its grid lines, wherever in the cell the mouse is, in the color of what a press would do, and draws the tile Paint would write faded inside it.
func TestHoverOutlineIsDrawnOnTheCell(t *testing.T) {
	ed := floorEditor(t)
	pal := ed.SpecPalette(nil)
	ed.SetTool(editor.ToolPaintTile)
	ed.SetActiveTileID(2)
	for _, at := range [][2]float32{{33, 17}, {47, 31}} {
		x, y := ed.WorldToCanvas(at[0], at[1])
		ed.SetTileHover(x, y, false)
		r, ok := hoverRect(drawn(ed).Ops, pal.Accent.Fg)
		if !ok || r.X != 32 || r.Y != 16 || r.W != 16 || r.H != 16 {
			t.Fatalf("hover at world %v outlines %+v (%v), want the cell (2,1) at 32,16 16x16", at, r, ok)
		}
	}
	if n := len(atlasDraws(drawn(ed))); n == 0 {
		t.Fatal("no faded tile is drawn in the hovered cell")
	}
	ed.SetTool(editor.ToolEraseTile)
	if _, ok := hoverRect(drawn(ed).Ops, pal.Removed.Fg); !ok {
		t.Fatal("Erase does not outline the hovered cell in red")
	}
	x, y := ed.WorldToCanvas(33, 17)
	ed.SetTileHover(x, y, true)
	if _, ok := hoverRect(drawn(ed).Ops, pal.Warning.Fg); !ok {
		t.Fatal("Alt does not outline the hovered cell in amber")
	}
	ed.ClearTileHover()
	if _, ok := hoverRect(drawn(ed).Ops, pal.Warning.Fg); ok {
		t.Fatal("a cell is outlined after the mouse left")
	}
}

// atlasDraws returns the sprite draws among d's ops whose alpha is below one, as the faded tile is.
func atlasDraws(d *core.Drawing) []core.DrawOp {
	var out []core.DrawOp
	faded := false
	for _, op := range d.Ops {
		switch op.Kind {
		case core.DrawAlpha:
			faded = op.A < 1
		case core.DrawImage, core.DrawAtlas:
			if faded {
				out = append(out, op)
			}
		}
	}
	return out
}

// mountFloor mounts the editor over the test scene with the floor layer active and the given tool chosen, and returns the canvas point over world point (wx, wy) as the tool's palette leaves the viewport.
func mountFloor(t *testing.T, tool string) (*editor.Editor, *nsgotest.App) {
	t.Helper()
	ed := newTestEditor(t)
	ed.SetActiveLayer("floor")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 680))
	switch tool {
	case "tool-paint":
		runEditorCommand(t, app, ed, editor.CmdToolPaintTile)
	case "tool-erase":
		runEditorCommand(t, app, ed, editor.CmdToolEraseTile)
	case "tool-fill":
		runEditorCommand(t, app, ed, editor.CmdToolFillTile)
	case "tool-rect":
		runEditorCommand(t, app, ed, editor.CmdToolRectTile)
	case "tool-line":
		runEditorCommand(t, app, ed, editor.CmdToolLineTile)
	default:
		app.Press("id:" + tool)
	}
	return ed, app
}

// The issue's first acceptance item in the mounted editor: one mouse drag across a row paints the whole row, and one press of Undo removes it.
func TestMouseDragPaintsARowAndOneUndoRemovesIt(t *testing.T) {
	ed, app := mountFloor(t, "tool-paint")
	app.Press("id:tile-palette-3")
	before := slices.Clone(floorData(ed))
	app.MouseDownAt("id:editor-viewport-frame", canvasAt(ed, 3, 24), core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 30, 25))
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 61, 23))
	app.MouseUpAt("id:editor-viewport-frame", canvasAt(ed, 61, 23), core.MouseButtonLeft)
	if got := floorData(ed)[4:8]; !slices.Equal(got, []int{3, 3, 3, 3}) {
		t.Fatalf("row 1 = %v after the drag, want it painted with tile 3", got)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("the drag recorded %d steps, want 1", ed.History.Len())
	}
	undoInUI(app, ed)
	if !slices.Equal(floorData(ed), before) {
		t.Fatalf("after one Undo the map is %v, want %v", floorData(ed), before)
	}
	if ed.History.CanUndo() {
		t.Fatal("more than the stroke was recorded")
	}
}

// A finger's drag paints as the mouse's does, and the press it completes paints nothing more.
func TestFingerDragPaintsARow(t *testing.T) {
	ed, app := mountFloor(t, "tool-paint")
	app.Press("id:tile-palette-2")
	app.DragAt("id:editor-viewport-frame", canvasAt(ed, 3, 40), core.Point{X: core.Points(58 * ed.View2D().Zoom)})
	if got := floorData(ed)[8:12]; !slices.Equal(got, []int{2, 2, 2, 2}) {
		t.Fatalf("row 2 = %v after a finger's drag, want it painted with tile 2", got)
	}
	if ed.History.Len() != 1 {
		t.Fatalf("the drag recorded %d steps, want 1", ed.History.Len())
	}
	// A tap paints its cell once, as one step.
	app.PressAt("id:editor-viewport", canvasAt(ed, 24, 24))
	if got := floorData(ed)[5]; got != 2 || ed.History.Len() != 2 {
		t.Fatalf("after a tap (1,1) = %d with %d steps, want 2 with 2", got, ed.History.Len())
	}
}

// A right-button drag erases while Paint is active.
func TestRightDragErasesWithPaint(t *testing.T) {
	ed, app := mountFloor(t, "tool-paint")
	app.MouseDownAt("id:editor-viewport-frame", canvasAt(ed, 3, 56), core.MouseButtonRight)
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 62, 56))
	app.MouseUpAt("id:editor-viewport-frame", canvasAt(ed, 62, 56), core.MouseButtonRight)
	if got := floorData(ed)[12:16]; !slices.Equal(got, []int{0, 0, 0, 0}) {
		t.Fatalf("row 3 = %v after a right-drag with Paint, want it erased", got)
	}
	if ed.Tool != editor.ToolPaintTile || ed.History.UndoLabel() != "Erase 4 tiles" {
		t.Fatalf("tool %v, undo %q, want Paint still active and \"Erase 4 tiles\"", ed.Tool, ed.History.UndoLabel())
	}
}

// The issue's second acceptance item in the mounted editor: the outline follows the mouse over the viewport and snaps to cells, and leaves with it.
func TestHoverOutlineFollowsTheMouse(t *testing.T) {
	ed, app := mountFloor(t, "tool-paint")
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 20, 40))
	if col, row, ok := ed.HoveredTile(); !ok || col != 1 || row != 2 {
		t.Fatalf("HoveredTile = (%d, %d, %v), want (1, 2)", col, row, ok)
	}
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 52, 8))
	if col, row, ok := ed.HoveredTile(); !ok || col != 3 || row != 0 {
		t.Fatalf("HoveredTile = (%d, %d, %v), want (3, 0)", col, row, ok)
	}
	r, ok := hoverRect(app.Find("id:editor-viewport").Ops, ed.SpecPalette(nil).Accent.Fg)
	if !ok || r.X != 48 || r.Y != 0 || r.W != 16 || r.H != 16 {
		t.Fatalf("the canvas outlines %+v (%v), want the cell (3,0)", r, ok)
	}
	app.HoverOut()
	if _, _, ok := ed.HoveredTile(); ok {
		t.Fatal("a cell is outlined after the mouse left the viewport")
	}
}

// The issue's third acceptance item in the mounted editor: Alt+press on a painted cell makes its tile the active tile in the palette, and the Pick tool does the same and hands over to Paint.
func TestPickingMakesTheTileActiveInThePalette(t *testing.T) {
	ed, app := mountFloor(t, "tool-paint")
	app.Press("id:tile-palette-3")
	at := canvasAt(ed, 8, 8)
	app.MouseDownAtWith("id:editor-viewport-frame", at, core.MouseButtonLeft, core.Modifiers{Alt: true})
	app.MouseUpAtWith("id:editor-viewport-frame", at, core.MouseButtonLeft, core.Modifiers{Alt: true})
	if ed.ActiveTileID != 1 {
		t.Fatalf("ActiveTileID = %d after Alt+press on tile 1, want 1", ed.ActiveTileID)
	}
	if !app.Find("id:tile-palette-1").Props.Checked || app.Find("id:tile-palette-3").Props.Checked {
		t.Fatal("the palette does not show tile 1 as the selected tile")
	}
	if ed.Doc.Layers[0].Tilemap.Data[0] != 1 || ed.History.Len() != 0 {
		t.Fatal("Alt+press edited the map")
	}
	// A tap with Alt held picks as the mouse does: tile 1 again, from (3,0).
	app.Press("id:tile-palette-3")
	app.KeyDown("alt")
	app.PressAt("id:editor-viewport", canvasAt(ed, 56, 8))
	app.KeyUp("alt")
	if ed.ActiveTileID != 1 || ed.History.Len() != 0 {
		t.Fatalf("after an Alt+tap on tile 1 ActiveTileID = %d with %d steps, want 1 with none", ed.ActiveTileID, ed.History.Len())
	}

	// The Pick tool: paint tile 2 into (1,1), choose tile 3, then pick (1,1).
	app.Press("id:tile-palette-2")
	app.PressAt("id:editor-viewport", canvasAt(ed, 24, 24))
	app.Press("id:tile-palette-3")
	app.Post(func() { ed.SetTool(editor.ToolPickTile) })
	app.Settle()
	if !app.Exists("id:tile-palette") {
		t.Fatal("the palette hides with the Pick tool")
	}
	app.PressAt("id:editor-viewport", canvasAt(ed, 24, 24))
	if ed.ActiveTileID != 2 || !app.Find("id:tile-palette-2").Props.Checked {
		t.Fatalf("ActiveTileID = %d after picking tile 2, want 2 and the palette showing it", ed.ActiveTileID)
	}
	if ed.Tool != editor.ToolPaintTile {
		t.Fatalf("Tool = %v after a pick, want Paint", ed.Tool)
	}
	// Picking an empty cell keeps the Pick tool and the active tile.
	app.Post(func() { ed.SetTool(editor.ToolPickTile) })
	app.Settle()
	app.PressAt("id:editor-viewport", canvasAt(ed, 40, 40))
	if ed.ActiveTileID != 2 || ed.Tool != editor.ToolPickTile {
		t.Fatalf("after picking an empty cell ActiveTileID = %d and Tool = %v, want 2 and Pick", ed.ActiveTileID, ed.Tool)
	}
}

// A drag with Space held pans and never paints.
func TestSpaceDragDoesNotPaint(t *testing.T) {
	ed, app := mountFloor(t, "tool-paint")
	before := slices.Clone(floorData(ed))
	app.KeyDown("space")
	app.MouseDownAt("id:editor-viewport-frame", canvasAt(ed, 24, 24), core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 40, 24))
	app.MouseUpAt("id:editor-viewport-frame", canvasAt(ed, 40, 24), core.MouseButtonLeft)
	app.DragAt("id:editor-viewport-frame", canvasAt(ed, 24, 40), core.Point{X: 20})
	app.KeyUp("space")
	if !slices.Equal(floorData(ed), before) || ed.History.Len() != 0 {
		t.Fatalf("a Space-drag painted: map %v, %d steps", floorData(ed), ed.History.Len())
	}
}

// A right-button stroke, which no press follows, leaves the next click to the tool: Fill fills.
func TestClickAfterRightDragReachesTheTool(t *testing.T) {
	ed, app := mountFloor(t, "tool-paint")
	app.MouseDownAt("id:editor-viewport-frame", canvasAt(ed, 3, 56), core.MouseButtonRight)
	app.HoverAt("id:editor-viewport-frame", canvasAt(ed, 30, 56))
	app.MouseUpAt("id:editor-viewport-frame", canvasAt(ed, 30, 56), core.MouseButtonRight)
	runEditorCommand(t, app, ed, editor.CmdToolFillTile)
	app.Press("id:tile-palette-3")
	app.PressAt("id:editor-viewport", canvasAt(ed, 24, 24))
	if got := floorData(ed)[5]; got != 3 {
		t.Fatalf("after a right-drag and a Fill click (1,1) = %d, want 3", got)
	}
}

// A tap that painted a cell leaves the next click to the tool: Select selects the object under it.
func TestClickAfterPaintTapReachesTheTool(t *testing.T) {
	ed, app := mountFloor(t, "tool-paint")
	app.Press("id:tile-palette-2")
	app.PressAt("id:editor-viewport", canvasAt(ed, 24, 24))
	if got := floorData(ed)[5]; got != 2 {
		t.Fatalf("the tap left (1,1) = %d, want 2", got)
	}
	app.Press("id:tool-select")
	app.PressAt("id:editor-viewport", canvasAt(ed, 40, 40))
	if ed.SelectedID != "hero" {
		t.Fatalf("after a paint tap and a Select click SelectedID = %q, want hero", ed.SelectedID)
	}
	// A Fill click after a tap fills too.
	runEditorCommand(t, app, ed, editor.CmdToolPaintTile)
	app.PressAt("id:editor-viewport", canvasAt(ed, 40, 40))
	runEditorCommand(t, app, ed, editor.CmdToolFillTile)
	app.Press("id:tile-palette-3")
	app.PressAt("id:editor-viewport", canvasAt(ed, 24, 24))
	if got := floorData(ed)[5]; got != 3 {
		t.Fatalf("after a paint tap and a Fill click (1,1) = %d, want 3", got)
	}
}

// Painting and erasing strokes refuse a locked or hidden active layer and leave its tiles and history untouched, while picking still reads a locked layer.
func TestTileStrokeRefusesLockedAndHiddenLayers(t *testing.T) {
	ed := floorEditor(t)
	ed.SetActiveTileID(2)
	before := slices.Clone(floorData(ed))
	x, y := ed.WorldToCanvas(24, 24)
	px, py := ed.WorldToCanvas(8, 8) // cell (0,0) holds tile 1

	if err := ed.SetLayerLocked("floor", true); err != nil {
		t.Fatalf("SetLayerLocked: %v", err)
	}
	if ed.BeginTileStroke(editor.TileStrokePaint, x, y) {
		t.Fatal("BeginTileStroke painted on a locked layer")
	}
	if !strings.Contains(ed.Status, `layer "floor" is locked`) {
		t.Fatalf("locked paint status = %q, want locked reason", ed.Status)
	}
	if ed.BeginTileStroke(editor.TileStrokeErase, px, py) {
		t.Fatal("BeginTileStroke erased on a locked layer")
	}
	if !slices.Equal(floorData(ed), before) || ed.History.Len() != 0 {
		t.Fatalf("locked strokes modified the map or history: data=%v steps=%d", floorData(ed), ed.History.Len())
	}
	if !ed.BeginTileStroke(editor.TileStrokePick, px, py) {
		t.Fatal("BeginTileStroke refused to pick on a locked layer")
	}
	ed.EndTileStroke()
	if ed.ActiveTileID != 1 {
		t.Fatalf("picked ActiveTileID = %d, want 1", ed.ActiveTileID)
	}

	if err := ed.SetLayerLocked("floor", false); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	ed.SetActiveTileID(2)
	if err := ed.SetLayerVisible("floor", false); err != nil {
		t.Fatalf("SetLayerVisible(false): %v", err)
	}
	histAfterHide := ed.History.Len()
	if ed.BeginTileStroke(editor.TileStrokePaint, x, y) {
		t.Fatal("BeginTileStroke painted on a hidden layer")
	}
	if !strings.Contains(ed.Status, `layer "floor" is hidden`) {
		t.Fatalf("hidden paint status = %q, want hidden reason", ed.Status)
	}
	if ed.BeginTileStroke(editor.TileStrokeErase, px, py) {
		t.Fatal("BeginTileStroke erased on a hidden layer")
	}
	if !slices.Equal(floorData(ed), before) || ed.History.Len() != histAfterHide {
		t.Fatalf("hidden strokes modified the map or history: data=%v steps=%d", floorData(ed), ed.History.Len())
	}

	if err := ed.SetLayerVisible("floor", true); err != nil {
		t.Fatalf("SetLayerVisible(true): %v", err)
	}
	if !ed.BeginTileStroke(editor.TileStrokePaint, x, y) {
		t.Fatal("BeginTileStroke refused to paint after showing the layer")
	}
	ed.EndTileStroke()
	if floorData(ed)[5] != 2 {
		t.Fatalf("cell (1,1) = %d after painting the visible unlocked layer, want 2", floorData(ed)[5])
	}
}

// Saving mid-stroke records the stroke before writing so the file, SceneDirty and History agree; reverting mid-stroke cancels the in-progress stroke first.
func TestTileStrokeSaveAndRevertMidStroke(t *testing.T) {
	ed := floorEditor(t)
	dir := t.TempDir()
	sceneFile := filepath.Join(dir, "level.scene.yaml")
	if err := ed.SaveFile(sceneFile); err != nil {
		t.Fatalf("SaveFile initial: %v", err)
	}
	if err := ed.OpenFile(sceneFile); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	ed.SetActiveLayer("floor")
	ed.SetCanvasSize(640, 480)
	ed.SetActiveTileID(2)

	// Mid-stroke Save records the stroke to History, writes the painted tiles and marks the scene clean.
	x0, y0 := ed.WorldToCanvas(24, 24)
	x1, y1 := ed.WorldToCanvas(40, 24)
	if !ed.BeginTileStroke(editor.TileStrokePaint, x0, y0) {
		t.Fatal("BeginTileStroke failed")
	}
	ed.ContinueTileStroke(x1, y1)
	if err := ed.Save(); err != nil {
		t.Fatalf("Save mid-stroke: %v", err)
	}
	if _, active := ed.TileStrokeActive(); active {
		t.Fatal("tile stroke remained active after Save")
	}
	if ed.History.Len() != 1 {
		t.Fatalf("History.Len() = %d after Save mid-stroke, want 1", ed.History.Len())
	}
	if ed.SceneDirty() {
		t.Fatal("SceneDirty() is true immediately after Save mid-stroke")
	}
	raw, err := os.ReadFile(sceneFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	diskDoc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse saved scene: %v", err)
	}
	if got := diskDoc.Layers[0].Tilemap.Data; got[5] != 2 || got[6] != 2 {
		t.Fatalf("saved tiles (1,1),(2,1) = (%d,%d), want (2,2)", got[5], got[6])
	}
	if !ed.Undo() {
		t.Fatal("Undo after Save mid-stroke returned false")
	}
	if floorData(ed)[5] != 0 || floorData(ed)[6] != 0 || !ed.SceneDirty() {
		t.Fatalf("Undo did not restore pre-stroke tiles cleanly: data=%v dirty=%v", floorData(ed), ed.SceneDirty())
	}
	ed.Redo()

	// Mid-stroke SaveFile also records the stroke before writing.
	ed.SetActiveTileID(3)
	x2, y2 := ed.WorldToCanvas(24, 40)
	if !ed.BeginTileStroke(editor.TileStrokePaint, x2, y2) {
		t.Fatal("second BeginTileStroke failed")
	}
	if err := ed.SaveFile(sceneFile); err != nil {
		t.Fatalf("SaveFile mid-stroke: %v", err)
	}
	if _, active := ed.TileStrokeActive(); active || ed.History.Len() != 2 || ed.SceneDirty() {
		t.Fatalf("after SaveFile mid-stroke: active=%v hist=%d dirty=%v", active, ed.History.Len(), ed.SceneDirty())
	}

	// Mid-stroke RevertScene cancels the in-progress stroke first and restores the saved scene cleanly.
	ed.SetActiveTileID(1)
	x3, y3 := ed.WorldToCanvas(40, 40)
	if !ed.BeginTileStroke(editor.TileStrokePaint, x3, y3) {
		t.Fatal("third BeginTileStroke failed")
	}
	if floorData(ed)[10] != 1 {
		t.Fatalf("cell (2,2) = %d during stroke, want 1", floorData(ed)[10])
	}
	if err := ed.RevertScene(); err != nil {
		t.Fatalf("RevertScene mid-stroke: %v", err)
	}
	if _, active := ed.TileStrokeActive(); active {
		t.Fatal("tile stroke remained active after RevertScene")
	}
	ed.EndTileStroke()
	if floorData(ed)[10] != 0 || ed.SceneDirty() {
		t.Fatalf("after RevertScene mid-stroke: cell (2,2)=%d dirty=%v, want 0 and clean", floorData(ed)[10], ed.SceneDirty())
	}
}
