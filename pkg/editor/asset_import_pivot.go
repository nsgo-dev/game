package editor

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	importPreviewPadding   float32 = 8
	importPivotHitRadius   float32 = 10
	importPivotMarkRadius  float32 = 4.5
	importPivotCrossRadius float32 = 7
	// maxImportPreviewCells caps full per-cell grid/marker rendering and Select option
	// materialization so fine slice grids on large images (such as 1x1 on 1024x1024)
	// stay instant and bounded in memory.
	maxImportPreviewCells = 1024
)

type imageImportPreviewState struct {
	cachedPath    string
	cachedModTime time.Time
	cachedSize    int64
	cachedRGBA    *image.RGBA
	cachedW       int
	cachedH       int

	selectedCell    int
	hasSelectedCell bool

	drag imagePivotDragState
}

type imagePivotDragState struct {
	active    bool
	relPath   string
	cell      int // -1 for unsliced default pivot, >= 0 for sliced cell override
	origPivot importsettings.PivotPoint
	livePivot importsettings.PivotPoint
	moved     bool
	canvasW   float32
	canvasH   float32
	imgW      int
	imgH      int
	cellRect  image.Rectangle
}

func (s *imageImportPreviewState) resetInteraction() {
	s.selectedCell = 0
	s.hasSelectedCell = false
	s.drag = imagePivotDragState{}
}

// loadImportPreviewImage reads and decodes the image asset at relPath in the open project, caching the RGBA pixels until the file's size or modtime changes.
func (e *Editor) loadImportPreviewImage(relPath string) (*image.RGBA, int, int, error) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil, 0, 0, ErrNoProject
	}
	clean, err := normalizeProjectAssetPath(relPath)
	if err != nil {
		return nil, 0, 0, err
	}
	rootDir := filepath.Dir(e.ProjectPath)
	abs := filepath.Join(rootDir, filepath.FromSlash(clean))
	if err := verifyFileWithinRoot(rootDir, abs, relPath); err != nil {
		return nil, 0, 0, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, 0, 0, err
	}
	p := &e.importPreview
	if p.cachedRGBA != nil && p.cachedPath == clean && p.cachedSize == info.Size() && p.cachedModTime.Equal(info.ModTime()) {
		return p.cachedRGBA, p.cachedW, p.cachedH, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, 0, 0, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)

	p.cachedPath = clean
	p.cachedModTime = info.ModTime()
	p.cachedSize = info.Size()
	p.cachedRGBA = rgba
	p.cachedW = w
	p.cachedH = h
	return rgba, w, h, nil
}

func verifyFileWithinRoot(rootDir, absPath, label string) error {
	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootReal = rootAbs
	}
	targetReal, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootReal, targetReal)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("editor: asset path %q resolves outside the project root", label)
	}
	return nil
}

// SelectedImportSliceCell returns the 0-based slice cell currently selected in the image import preview (0 by default).
func (e *Editor) SelectedImportSliceCell() int {
	if e == nil {
		return 0
	}
	if !e.importPreview.hasSelectedCell {
		return 0
	}
	return e.importPreview.selectedCell
}

// SelectImportSliceCell selects the 0-based slice cell in the image import preview for per-cell pivot inspection and editing.
func (e *Editor) SelectImportSliceCell(cell int) {
	if e == nil || cell < 0 {
		return
	}
	if e.importPreview.hasSelectedCell && e.importPreview.selectedCell == cell {
		return
	}
	e.importPreview.selectedCell = cell
	e.importPreview.hasSelectedCell = true
	e.notify()
}

// ImagePivotDragging reports whether an interactive pivot drag on the image import preview is currently active.
func (e *Editor) ImagePivotDragging() bool {
	return e != nil && e.importPreview.drag.active
}

// imagePreviewLayout computes the aspect-preserving origin and pixel-to-point scale fitting imgW x imgH inside canvasW x canvasH.
func imagePreviewLayout(canvasW, canvasH float32, imgW, imgH int) (ox, oy, scale float32, ok bool) {
	if imgW <= 0 || imgH <= 0 {
		return 0, 0, 0, false
	}
	availW := canvasW - 2*importPreviewPadding
	availH := canvasH - 2*importPreviewPadding
	if availW <= 0 || availH <= 0 {
		return 0, 0, 0, false
	}
	scale = min(availW/float32(imgW), availH/float32(imgH))
	if scale <= 0 {
		return 0, 0, 0, false
	}
	ox = (canvasW - float32(imgW)*scale) / 2
	oy = (canvasH - float32(imgH)*scale) / 2
	return ox, oy, scale, true
}

func previewPivotCanvasPos(ox, oy, scale float32, cell image.Rectangle, pt importsettings.PivotPoint) (float32, float32) {
	px := float64(cell.Min.X) + pt[0]*float64(cell.Dx())
	py := float64(cell.Min.Y) + pt[1]*float64(cell.Dy())
	return ox + float32(px)*scale, oy + float32(py)*scale
}

func previewCanvasToPivot(ox, oy, scale float32, cell image.Rectangle, cx, cy float32) importsettings.PivotPoint {
	if cell.Dx() <= 0 || cell.Dy() <= 0 || scale <= 0 {
		return importsettings.PivotPoint{0, 0}
	}
	imgX := float64(cx-ox) / float64(scale)
	imgY := float64(cy-oy) / float64(scale)
	u := (imgX - float64(cell.Min.X)) / float64(cell.Dx())
	v := (imgY - float64(cell.Min.Y)) / float64(cell.Dy())
	u = roundPivotCoord(clamp01(u))
	v = roundPivotCoord(clamp01(v))
	return importsettings.PivotPoint{u, v}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

func roundPivotCoord(v float64) float64 {
	r := math.Round(v*10000) / 10000
	if r == 0 {
		return 0
	}
	return r
}

// ImageImportPreviewCellPoint maps a normalized pivot pt inside cell (-1 for the whole image, or 0..n-1 for a sliced cell) of relPath to canvas coordinates within canvasW x canvasH.
func (e *Editor) ImageImportPreviewCellPoint(relPath string, cell int, pt importsettings.PivotPoint, canvasW, canvasH float32) (x, y float32, ok bool) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return 0, 0, false
	}
	info, err := e.InspectAssetImport(relPath)
	if err != nil || info.Kind != AssetImportImage {
		return 0, 0, false
	}
	ox, oy, scale, ok := imagePreviewLayout(canvasW, canvasH, info.ImageWidth, info.ImageHeight)
	if !ok {
		return 0, 0, false
	}
	bounds := image.Rect(0, 0, info.ImageWidth, info.ImageHeight)
	if cell < 0 {
		x, y = previewPivotCanvasPos(ox, oy, scale, bounds, pt)
		return x, y, true
	}
	s, hasSlice := info.EffectiveImageSlice()
	if !hasSlice {
		return 0, 0, false
	}
	n, cols, _, err := s.GridCounts(bounds)
	if err != nil || cell >= n {
		return 0, 0, false
	}
	r, ok := s.CellRect(bounds, cols, cell)
	if !ok {
		return 0, 0, false
	}
	x, y = previewPivotCanvasPos(ox, oy, scale, r, pt)
	return x, y, true
}

// BeginImagePivotDrag starts dragging a pivot marker (or clicking inside a cell to place and drag its pivot) on the image import preview canvas for relPath.
// For a sliced sheet it drags that cell's per-cell pivot override; for an unsliced image it drags the default pivot.
func (e *Editor) BeginImagePivotDrag(relPath string, canvasX, canvasY, canvasW, canvasH float32) bool {
	if e == nil || e.playing || e.Project == nil || e.ProjectPath == "" {
		return false
	}
	if !finite32(canvasX) || !finite32(canvasY) || !finite32(canvasW) || !finite32(canvasH) {
		return false
	}
	info, err := e.InspectAssetImport(relPath)
	if err != nil || info.Kind != AssetImportImage {
		return false
	}
	ox, oy, scale, ok := imagePreviewLayout(canvasW, canvasH, info.ImageWidth, info.ImageHeight)
	if !ok {
		return false
	}
	bounds := image.Rect(0, 0, info.ImageWidth, info.ImageHeight)

	if s, hasSlice := info.EffectiveImageSlice(); hasSlice {
		n, cols, rows, err := s.GridCounts(bounds)
		if err != nil || n == 0 {
			return false
		}
		sel := e.SelectedImportSliceCell()
		if sel < 0 || sel >= n {
			sel = 0
		}
		if n > maxImportPreviewCells {
			// For dense grids, hit-test the selected cell's pivot marker first, then locate the clicked cell in O(1).
			if sr, ok := s.CellRect(bounds, cols, sel); ok {
				orig, _, _ := info.EffectiveCellImagePivot(sel)
				mx, my := previewPivotCanvasPos(ox, oy, scale, sr, orig)
				dx, dy := canvasX-mx, canvasY-my
				if dx*dx+dy*dy <= importPivotHitRadius*importPivotHitRadius {
					e.importPreview.selectedCell = sel
					e.importPreview.hasSelectedCell = true
					e.importPreview.drag = imagePivotDragState{
						active:    true,
						relPath:   info.Path,
						cell:      sel,
						origPivot: orig,
						livePivot: orig,
						moved:     false,
						canvasW:   canvasW,
						canvasH:   canvasH,
						imgW:      info.ImageWidth,
						imgH:      info.ImageHeight,
						cellRect:  sr,
					}
					e.notify()
					return true
				}
			}
			imgX := (canvasX - ox) / scale
			imgY := (canvasY - oy) / scale
			if idx, r, ok := hitSliceCellO1(*s, bounds, cols, rows, n, imgX, imgY); ok {
				orig, _, _ := info.EffectiveCellImagePivot(idx)
				live := previewCanvasToPivot(ox, oy, scale, r, canvasX, canvasY)
				e.importPreview.selectedCell = idx
				e.importPreview.hasSelectedCell = true
				e.importPreview.drag = imagePivotDragState{
					active:    true,
					relPath:   info.Path,
					cell:      idx,
					origPivot: orig,
					livePivot: live,
					moved:     true,
					canvasW:   canvasW,
					canvasH:   canvasH,
					imgW:      info.ImageWidth,
					imgH:      info.ImageHeight,
					cellRect:  r,
				}
				e.notify()
				return true
			}
			return false
		}
		rects, _, _, err := s.Cells(bounds)
		if err != nil || len(rects) == 0 {
			return false
		}
		bestCell := -1
		bestDist := float32(importPivotHitRadius * importPivotHitRadius)
		for i, r := range rects {
			pt, _, _ := info.EffectiveCellImagePivot(i)
			mx, my := previewPivotCanvasPos(ox, oy, scale, r, pt)
			dx, dy := canvasX-mx, canvasY-my
			d2 := dx*dx + dy*dy
			if d2 <= bestDist && (bestCell < 0 || d2 < bestDist || (d2 == bestDist && i == sel)) {
				bestDist = d2
				bestCell = i
			}
		}
		if bestCell >= 0 {
			orig, _, _ := info.EffectiveCellImagePivot(bestCell)
			e.importPreview.selectedCell = bestCell
			e.importPreview.hasSelectedCell = true
			e.importPreview.drag = imagePivotDragState{
				active:    true,
				relPath:   info.Path,
				cell:      bestCell,
				origPivot: orig,
				livePivot: orig,
				moved:     false,
				canvasW:   canvasW,
				canvasH:   canvasH,
				imgW:      info.ImageWidth,
				imgH:      info.ImageHeight,
				cellRect:  rects[bestCell],
			}
			e.notify()
			return true
		}
		// Not directly on a marker: check whether the pointer is inside a cell rectangle.
		imgX := (canvasX - ox) / scale
		imgY := (canvasY - oy) / scale
		for i, r := range rects {
			if imgX >= float32(r.Min.X) && imgX <= float32(r.Max.X) &&
				imgY >= float32(r.Min.Y) && imgY <= float32(r.Max.Y) {
				orig, _, _ := info.EffectiveCellImagePivot(i)
				live := previewCanvasToPivot(ox, oy, scale, r, canvasX, canvasY)
				e.importPreview.selectedCell = i
				e.importPreview.hasSelectedCell = true
				e.importPreview.drag = imagePivotDragState{
					active:    true,
					relPath:   info.Path,
					cell:      i,
					origPivot: orig,
					livePivot: live,
					moved:     true,
					canvasW:   canvasW,
					canvasH:   canvasH,
					imgW:      info.ImageWidth,
					imgH:      info.ImageHeight,
					cellRect:  r,
				}
				e.notify()
				return true
			}
		}
		return false
	}

	// Unsliced image: drag the image's default pivot.
	orig, _ := info.EffectiveDefaultImagePivot()
	mx, my := previewPivotCanvasPos(ox, oy, scale, bounds, orig)
	dx, dy := canvasX-mx, canvasY-my
	hitMarker := dx*dx+dy*dy <= importPivotHitRadius*importPivotHitRadius
	imgX := (canvasX - ox) / scale
	imgY := (canvasY - oy) / scale
	inImage := imgX >= 0 && imgX <= float32(info.ImageWidth) && imgY >= 0 && imgY <= float32(info.ImageHeight)
	if !hitMarker && !inImage {
		return false
	}
	live := orig
	moved := false
	if !hitMarker {
		live = previewCanvasToPivot(ox, oy, scale, bounds, canvasX, canvasY)
		moved = true
	}
	e.importPreview.drag = imagePivotDragState{
		active:    true,
		relPath:   info.Path,
		cell:      -1,
		origPivot: orig,
		livePivot: live,
		moved:     moved,
		canvasW:   canvasW,
		canvasH:   canvasH,
		imgW:      info.ImageWidth,
		imgH:      info.ImageHeight,
		cellRect:  bounds,
	}
	e.notify()
	return true
}

func hitSliceCellO1(s importsettings.Slice, bounds image.Rectangle, cols, rows, n int, imgX, imgY float32) (int, image.Rectangle, bool) {
	rx := float64(imgX) - float64(bounds.Min.X) - float64(s.Margin)
	ry := float64(imgY) - float64(bounds.Min.Y) - float64(s.Margin)
	if rx < 0 || ry < 0 {
		return 0, image.Rectangle{}, false
	}
	strideX := float64(s.Width) + float64(s.Spacing)
	strideY := float64(s.Height) + float64(s.Spacing)
	if strideX <= 0 || strideY <= 0 {
		return 0, image.Rectangle{}, false
	}
	col := int(math.Floor(rx / strideX))
	row := int(math.Floor(ry / strideY))
	if col < 0 || col >= cols || row < 0 || row >= rows {
		return 0, image.Rectangle{}, false
	}
	idx := row*cols + col
	if idx < 0 || idx >= n {
		return 0, image.Rectangle{}, false
	}
	r, ok := s.CellRect(bounds, cols, idx)
	if !ok {
		return 0, image.Rectangle{}, false
	}
	if imgX < float32(r.Min.X) || imgX > float32(r.Max.X) || imgY < float32(r.Min.Y) || imgY > float32(r.Max.Y) {
		return 0, image.Rectangle{}, false
	}
	return idx, r, true
}

// UpdateImagePivotDrag moves the active dragged pivot marker to canvas coordinates (canvasX, canvasY) without writing the sidecar until EndImagePivotDrag.
func (e *Editor) UpdateImagePivotDrag(canvasX, canvasY float32) {
	if e == nil || !e.importPreview.drag.active || !finite32(canvasX) || !finite32(canvasY) {
		return
	}
	d := &e.importPreview.drag
	ox, oy, scale, ok := imagePreviewLayout(d.canvasW, d.canvasH, d.imgW, d.imgH)
	if !ok {
		return
	}
	next := previewCanvasToPivot(ox, oy, scale, d.cellRect, canvasX, canvasY)
	if d.moved && next == d.livePivot {
		return
	}
	d.livePivot = next
	d.moved = true
	e.notify()
}

// EndImagePivotDrag commits an active pivot drag to the image's *.import.yaml sidecar as one undoable History step.
func (e *Editor) EndImagePivotDrag() error {
	if e == nil || !e.importPreview.drag.active {
		return nil
	}
	d := e.importPreview.drag
	e.importPreview.drag = imagePivotDragState{}
	if !d.moved || d.livePivot == d.origPivot {
		e.notify()
		return nil
	}
	pt := d.livePivot
	if d.cell >= 0 {
		return e.SetImageImportCellPivot(d.relPath, d.cell, &pt)
	}
	return e.SetImageImportDefaultPivot(d.relPath, &pt)
}

// CancelImagePivotDrag aborts an active pivot drag without modifying the sidecar or History.
func (e *Editor) CancelImagePivotDrag() {
	if e == nil || !e.importPreview.drag.active {
		return
	}
	e.importPreview.drag = imagePivotDragState{}
	e.notify()
}

// DrawImageImportPreview draws the image asset, checkerboard background, slice grid overlay, and interactive pivot markers for relPath.
func (e *Editor) DrawImageImportPreview(d *core.Drawing, size core.Size, relPath string) {
	cw, ch := float32(size.Width), float32(size.Height)
	if cw <= 0 || ch <= 0 {
		return
	}

	spec := e.activeSpecPalette()
	// Draw alpha checkerboard background.
	d.SetFillColor(spec.Viewport).FillRect(0, 0, cw, ch)
	const tile float32 = 8
	d.SetFillColor(spec.LineRaised)
	for y := float32(0); y < ch; y += tile {
		row := int(y / tile)
		for x := float32(0); x < cw; x += tile {
			col := int(x / tile)
			if (row+col)%2 == 1 {
				d.FillRect(x, y, min(tile, cw-x), min(tile, ch-y))
			}
		}
	}

	if e == nil {
		return
	}
	rgba, imgW, imgH, err := e.loadImportPreviewImage(relPath)
	if err != nil || rgba == nil {
		return
	}
	ox, oy, scale, ok := imagePreviewLayout(cw, ch, imgW, imgH)
	if !ok {
		return
	}
	dw, dh := float32(imgW)*scale, float32(imgH)*scale
	d.DrawImageFrom(core.ImageSource{Pixels: rgba}, 0, 0, float32(imgW), float32(imgH), ox, oy, dw, dh)
	d.SetStrokeColor(specAlpha(spec.TextFaint, 0.5)).SetLineWidth(1).StrokeRect(ox, oy, dw, dh)

	info, err := e.InspectAssetImport(relPath)
	if err != nil {
		return
	}
	drag := e.importPreview.drag
	dragForThis := drag.active && drag.relPath == info.Path
	bounds := image.Rect(0, 0, imgW, imgH)

	if s, hasSlice := info.EffectiveImageSlice(); hasSlice {
		n, cols, _, err := s.GridCounts(bounds)
		if err != nil || n == 0 {
			return
		}
		sel := e.SelectedImportSliceCell()
		if sel < 0 || sel >= n {
			sel = 0
		}
		if n > maxImportPreviewCells {
			// Dense grids skip drawing millions of cell outlines/markers and show only the selected cell.
			if sr, ok := s.CellRect(bounds, cols, sel); ok {
				d.SetStrokeColor(spec.Accent.Edge).SetLineWidth(1.5).StrokeRect(
					ox+float32(sr.Min.X)*scale,
					oy+float32(sr.Min.Y)*scale,
					float32(sr.Dx())*scale,
					float32(sr.Dy())*scale,
				)
				pt, overridden, _ := info.EffectiveCellImagePivot(sel)
				draggingCell := dragForThis && drag.cell == sel
				if draggingCell {
					pt = drag.livePivot
				}
				mx, my := previewPivotCanvasPos(ox, oy, scale, sr, pt)
				drawPivotMarker(d, spec, mx, my, overridden, true, draggingCell)
			}
			return
		}
		rects, _, _, err := s.Cells(bounds)
		if err != nil || len(rects) == 0 {
			return
		}
		// Draw slice cell outlines first so the selected cell border and pivot markers sit on top.
		for i, r := range rects {
			rx := ox + float32(r.Min.X)*scale
			ry := oy + float32(r.Min.Y)*scale
			rw := float32(r.Dx()) * scale
			rh := float32(r.Dy()) * scale
			if i == sel {
				continue
			}
			d.SetStrokeColor(specAlpha(spec.Accent.Edge, 0.65)).SetLineWidth(1).StrokeRect(rx, ry, rw, rh)
		}
		sr := rects[sel]
		d.SetStrokeColor(spec.Accent.Edge).SetLineWidth(1.5).StrokeRect(
			ox+float32(sr.Min.X)*scale,
			oy+float32(sr.Min.Y)*scale,
			float32(sr.Dx())*scale,
			float32(sr.Dy())*scale,
		)

		// Draw pivot markers for every sliced cell.
		for i, r := range rects {
			pt, overridden, _ := info.EffectiveCellImagePivot(i)
			draggingCell := dragForThis && drag.cell == i
			if draggingCell {
				pt = drag.livePivot
			}
			mx, my := previewPivotCanvasPos(ox, oy, scale, r, pt)
			drawPivotMarker(d, spec, mx, my, overridden, i == sel, draggingCell)
		}
		return
	}

	// Unsliced image: draw its single default pivot marker.
	pt, explicit := info.EffectiveDefaultImagePivot()
	draggingDefault := dragForThis && drag.cell < 0
	if draggingDefault {
		pt = drag.livePivot
	}
	mx, my := previewPivotCanvasPos(ox, oy, scale, bounds, pt)
	drawPivotMarker(d, spec, mx, my, explicit, true, draggingDefault)
}

func drawPivotMarker(d *core.Drawing, spec SpecPalette, x, y float32, overridden, selected, dragging bool) {
	fill := spec.Warning.Fg // amber for inherited/default pivot
	if overridden {
		fill = spec.Accent.Fg // accent for per-cell override or explicit default
	}
	if dragging {
		fill = spec.Removed.Fg // red while dragging
	}
	r := importPivotMarkRadius
	cross := importPivotCrossRadius
	if selected || dragging {
		r += 1
		cross += 1.5
	}

	// Contrast dark crosshair halo then colored crosshair lines.
	d.SetStrokeColor(specAlpha(spec.Bg, 0.9)).SetLineWidth(2.5)
	d.BeginPath().MoveTo(x-cross, y).LineTo(x+cross, y).MoveTo(x, y-cross).LineTo(x, y+cross).Stroke()
	d.SetStrokeColor(fill).SetLineWidth(1.25)
	d.BeginPath().MoveTo(x-cross, y).LineTo(x+cross, y).MoveTo(x, y-cross).LineTo(x, y+cross).Stroke()

	// Circle marker with dark outline and colored center.
	d.SetFillColor(fill).BeginPath().Arc(x, y, r, 0, float32(2*math.Pi), false).Fill()
	stroke := spec.Bg
	if selected || dragging {
		stroke = spec.TextPrimary
	}
	d.SetStrokeColor(stroke).SetLineWidth(1.25).BeginPath().Arc(x, y, r, 0, float32(2*math.Pi), false).Stroke()
}
