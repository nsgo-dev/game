package rts

import (
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/nsgo/core"
)

// DrawFlowField strokes one short line per reachable cell of f, from the cell's center along its direction, in col and lineWidth world units, under whatever camera transform d already has. Goal cells get a small cross. It is meant for debug overlays and draws the whole field as a single path.
func DrawFlowField(d *core.Drawing, f *FlowField, col core.Color, lineWidth float32) {
	if d == nil || f == nil {
		return
	}
	cols, rows := f.Size()
	if cols == 0 || rows == 0 {
		return
	}
	size := f.cellSize()
	arm := size * 0.4
	d.Save().SetStrokeColor(col).SetLineWidth(lineWidth).BeginPath()
	for row := range rows {
		for c := range cols {
			cost := f.cost[row*cols+c]
			if cost == Unreachable {
				continue
			}
			center := cellCenter(pathfind.Cell{Col: c, Row: row}, f.Origin, size)
			if cost == 0 {
				k := size * 0.2
				d.MoveTo(center.X-k, center.Y-k).LineTo(center.X+k, center.Y+k)
				d.MoveTo(center.X-k, center.Y+k).LineTo(center.X+k, center.Y-k)
				continue
			}
			tip := center.Add(f.vec[row*cols+c].Scale(arm))
			tail := center.Sub(f.vec[row*cols+c].Scale(arm * 0.5))
			d.MoveTo(tail.X, tail.Y).LineTo(tip.X, tip.Y)
		}
	}
	d.Stroke().Restore()
}

// DrawSelectionBox fills r with fill and outlines it in stroke at lineWidth, under d's current transform; pass the rectangle from BoxSelector.Box while a drag is in progress.
func DrawSelectionBox(d *core.Drawing, r math2d.Rect, fill, stroke core.Color, lineWidth float32) {
	if d == nil {
		return
	}
	r = r.Normalized()
	d.Save().
		SetFillColor(fill).
		BeginPath().Rect(r.X, r.Y, r.W, r.H).Fill().
		SetStrokeColor(stroke).SetLineWidth(lineWidth).
		BeginPath().Rect(r.X, r.Y, r.W, r.H).Stroke().
		Restore()
}
