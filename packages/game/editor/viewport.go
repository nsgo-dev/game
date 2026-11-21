package editor

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const defaultMarkerHalfSize float32 = 8

// ViewportSize returns the scene's virtual viewport size in points, defaulting to 640x360 when unset.
func (e *Editor) ViewportSize() (float32, float32) {
	if e != nil && e.Doc != nil && e.Doc.VirtualWidth > 0 && e.Doc.VirtualHeight > 0 {
		return e.Doc.VirtualWidth, e.Doc.VirtualHeight
	}
	return 640, 360
}

// CanvasToWorld converts a point in canvas coordinates into world coordinates by inverting the scene's camera.
func (e *Editor) CanvasToWorld(x, y float32) (float32, float32) {
	if e == nil || e.Doc == nil || e.Doc.Camera == nil {
		return x, y
	}
	cam := e.Doc.Camera
	vw, vh := e.Doc.VirtualWidth, e.Doc.VirtualHeight
	zoom := cam.EffectiveZoom()
	if zoom <= 0 {
		zoom = 1
	}
	cx, cy := vw/2, vh/2
	camX, camY := clampCameraEditor(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)

	rx := (x - cx) / zoom
	ry := (y - cy) / zoom
	if cam.Rotation == 0 {
		return camX + rx, camY + ry
	}
	sin, cos := math.Sincos(float64(cam.Rotation))
	wx := camX + float32(float64(rx)*cos-float64(ry)*sin)
	wy := camY + float32(float64(rx)*sin+float64(ry)*cos)
	return wx, wy
}

// WorldToCanvas converts a world-space coordinate into canvas coordinates using the scene's camera.
func (e *Editor) WorldToCanvas(wx, wy float32) (float32, float32) {
	if e == nil || e.Doc == nil || e.Doc.Camera == nil {
		return wx, wy
	}
	cam := e.Doc.Camera
	vw, vh := e.Doc.VirtualWidth, e.Doc.VirtualHeight
	zoom := cam.EffectiveZoom()
	if zoom <= 0 {
		zoom = 1
	}
	cx, cy := vw/2, vh/2
	camX, camY := clampCameraEditor(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)

	dx, dy := wx-camX, wy-camY
	if cam.Rotation == 0 {
		return cx + dx*zoom, cy + dy*zoom
	}
	sin, cos := math.Sincos(float64(-cam.Rotation))
	rx := float32(float64(dx)*cos - float64(dy)*sin)
	ry := float32(float64(dx)*sin + float64(dy)*cos)
	return cx + rx*zoom, cy + ry*zoom
}

func clampCameraEditor(x, y, vw, vh, zoom float32, b *sceneformat.BoundsDoc) (float32, float32) {
	if b == nil || zoom <= 0 {
		return x, y
	}
	halfW := (vw / 2) / zoom
	halfH := (vh / 2) / zoom

	minX, maxX := b.X+halfW, b.X+b.Width-halfW
	if minX > maxX {
		x = b.X + b.Width/2
	} else {
		x = min(max(x, minX), maxX)
	}

	minY, maxY := b.Y+halfH, b.Y+b.Height-halfH
	if minY > maxY {
		y = b.Y + b.Height/2
	} else {
		y = min(max(y, minY), maxY)
	}
	return x, y
}

// CanvasToTile converts a canvas point into (col, row) on layer's inline tilemap.
func (e *Editor) CanvasToTile(layer string, canvasX, canvasY float32) (col, row int, ok bool) {
	layerName, tm, err := e.resolveInlineTilemap(layer)
	if err != nil {
		return 0, 0, false
	}
	li := e.layerIndex(layerName)
	lx, ly := canvasX, canvasY
	if li >= 0 && e.Doc.Layers[li].EffectiveSpace() == sceneformat.SpaceWorld {
		lx, ly = e.CanvasToWorld(canvasX, canvasY)
	}
	if lx < 0 || ly < 0 || tm.TileWidth <= 0 || tm.TileHeight <= 0 {
		return 0, 0, false
	}
	col = int(lx) / tm.TileWidth
	row = int(ly) / tm.TileHeight
	if col < 0 || col >= tm.Width || row < 0 || row >= tm.Height {
		return 0, 0, false
	}
	return col, row, true
}

// HitTest returns the ID of the topmost visible object under canvas coordinates (canvasX, canvasY),
// or "" if no object is hit.
func (e *Editor) HitTest(canvasX, canvasY float32) string {
	scene := e.ActiveScene()
	if scene == nil {
		return ""
	}
	// Walk layers from topmost (drawn last) to bottommost.
	for i := len(scene.Layers) - 1; i >= 0; i-- {
		layer := scene.Layers[i]
		if !layer.Visible {
			continue
		}
		lx, ly := canvasX, canvasY
		if layer.Space == sceneformat.SpaceWorld {
			lx, ly = e.CanvasToWorld(canvasX, canvasY)
		}
		// Find topmost object in this layer (highest Z, ties broken by later slice index).
		bestID := ""
		bestZ := math.MinInt
		for _, bo := range layer.Objects {
			if !hitTestBuiltObject(bo, lx, ly) {
				continue
			}
			z := bo.Doc.Z
			if bestID == "" || z >= bestZ {
				bestID = bo.ID
				bestZ = z
			}
		}
		if bestID != "" {
			return bestID
		}
	}
	return ""
}

func hitTestBuiltObject(bo *sceneformat.BuiltObject, lx, ly float32) bool {
	if bo == nil || bo.Doc == nil {
		return false
	}
	o := bo.Doc
	dx := lx - o.X
	dy := ly - o.Y

	// Undo rotation about object origin.
	if o.Rotation != 0 {
		sin, cos := math.Sincos(float64(-o.Rotation))
		rx := float32(float64(dx)*cos - float64(dy)*sin)
		ry := float32(float64(dx)*sin + float64(dy)*cos)
		dx, dy = rx, ry
	}

	sx := o.EffectiveScaleX()
	sy := o.EffectiveScaleY()
	if sx == 0 || sy == 0 {
		return false
	}
	dx /= sx
	dy /= sy

	if bo.Sprite != nil && bo.Sprite.Frame.Width > 0 && bo.Sprite.Frame.Height > 0 {
		u := dx + o.OriginX
		v := dy + o.OriginY
		return u >= 0 && u <= bo.Sprite.Frame.Width && v >= 0 && v <= bo.Sprite.Frame.Height
	}

	// Non-sprite marker object: default centered hit box.
	return dx >= -defaultMarkerHalfSize && dx <= defaultMarkerHalfSize &&
		dy >= -defaultMarkerHalfSize && dy <= defaultMarkerHalfSize
}

// HandleCanvasPress dispatches a click/tap at canvas coordinates (x, y) according to the active ViewMode and Tool.
func (e *Editor) HandleCanvasPress(x, y float32) {
	if e == nil || e.playing {
		return
	}
	if e.ViewMode == ViewMode3D {
		vw, vh := e.ViewportSize()
		e.HandleViewport3DPress(x, y, vw, vh)
		return
	}
	switch e.Tool {
	case ToolSelect:
		hit := e.HitTest(x, y)
		e.SelectObject(hit)
	case ToolMove:
		hit := e.HitTest(x, y)
		if hit != "" && hit != e.SelectedID {
			e.SelectObject(hit)
			return
		}
		if e.SelectedID != "" {
			wx, wy := e.layerCoordsForSelected(x, y)
			_ = e.MoveObject(e.SelectedID, wx, wy)
		}
	case ToolPlace:
		wx, wy := e.layerCoordsForActive(x, y)
		if e.SelectedAsset != nil {
			_, _ = e.DropAtlasFrame(e.SelectedAsset.Atlas, e.SelectedAsset.Frame, wx, wy)
		} else {
			_, _ = e.PlaceObject(e.ActiveLayer, sceneformat.ObjectDoc{X: wx, Y: wy})
		}
	case ToolPaintTile:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok {
			_ = e.PaintTile(e.ActiveLayer, col, row, e.ActiveTileID)
		}
	case ToolEraseTile:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok {
			_ = e.EraseTile(e.ActiveLayer, col, row)
		}
	case ToolFillTile:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok {
			_ = e.FloodFillTile(e.ActiveLayer, col, row, e.ActiveTileID)
		}
	}
}

func (e *Editor) layerCoordsForActive(x, y float32) (float32, float32) {
	li := e.layerIndex(e.ActiveLayer)
	if li >= 0 && e.Doc.Layers[li].EffectiveSpace() == sceneformat.SpaceScreen {
		return x, y
	}
	return e.CanvasToWorld(x, y)
}

func (e *Editor) layerCoordsForSelected(x, y float32) (float32, float32) {
	if _, li, _ := e.FindObject(e.SelectedID); li >= 0 && e.Doc.Layers[li].EffectiveSpace() == sceneformat.SpaceScreen {
		return x, y
	}
	return e.CanvasToWorld(x, y)
}

// DrawViewport renders the active scene in 2D or 3D mode and, when in edit mode,
// the appropriate editor overlays (tile grid + selection outline in 2D, grid + axes + boxes + gizmo in 3D).
func (e *Editor) DrawViewport(d *core.Drawing, size core.Size) {
	if e == nil || d == nil {
		return
	}
	if e.ViewMode == ViewMode3D {
		e.DrawViewport3D(d, size)
		return
	}
	scene := e.ActiveScene()
	if scene != nil {
		scene.Draw(d)
	}
	if e.playing || scene == nil {
		return
	}
	e.drawTileGridOverlay(d)
	e.drawSelectionOverlay(d, scene)
}

func (e *Editor) drawTileGridOverlay(d *core.Drawing) {
	if e.Tool != ToolPaintTile && e.Tool != ToolEraseTile && e.Tool != ToolFillTile {
		return
	}
	layerName, tm, err := e.resolveInlineTilemap(e.ActiveLayer)
	if err != nil || tm == nil {
		return
	}
	li := e.layerIndex(layerName)
	d.Save()
	if li >= 0 && e.Doc.Layers[li].EffectiveSpace() == sceneformat.SpaceWorld && e.Doc.Camera != nil {
		e.applyCameraTransform(d)
	}
	d.SetStrokeColor(core.RGBA(0xFFFFFF, 0.2)).SetLineWidth(1)
	totalW := float32(tm.Width * tm.TileWidth)
	totalH := float32(tm.Height * tm.TileHeight)
	d.BeginPath()
	for c := 0; c <= tm.Width; c++ {
		x := float32(c * tm.TileWidth)
		d.MoveTo(x, 0).LineTo(x, totalH)
	}
	for r := 0; r <= tm.Height; r++ {
		y := float32(r * tm.TileHeight)
		d.MoveTo(0, y).LineTo(totalW, y)
	}
	d.Stroke().Restore()
}

func (e *Editor) drawSelectionOverlay(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e.SelectedID == "" {
		return
	}
	bo := scene.Object(e.SelectedID)
	if bo == nil || bo.Doc == nil {
		return
	}
	layer := scene.Layer(bo.Layer)
	o := bo.Doc

	d.Save()
	if layer != nil && layer.Space == sceneformat.SpaceWorld && scene.Camera != nil {
		e.applyCameraTransform(d)
	}
	d.Translate(o.X, o.Y)
	if o.Rotation != 0 {
		d.Rotate(o.Rotation)
	}
	sx, sy := o.EffectiveScaleX(), o.EffectiveScaleY()
	if sx != 1 || sy != 1 {
		d.Scale(sx, sy)
	}

	var rx, ry, rw, rh float32
	if bo.Sprite != nil && bo.Sprite.Frame.Width > 0 && bo.Sprite.Frame.Height > 0 {
		rx = -o.OriginX
		ry = -o.OriginY
		rw = bo.Sprite.Frame.Width
		rh = bo.Sprite.Frame.Height
	} else {
		rx = -defaultMarkerHalfSize
		ry = -defaultMarkerHalfSize
		rw = defaultMarkerHalfSize * 2
		rh = defaultMarkerHalfSize * 2
	}
	d.SetStrokeColor(core.RGBA(0x38BDF8, 1)).SetLineWidth(1.5).StrokeRect(rx, ry, rw, rh).Restore()
}

func (e *Editor) applyCameraTransform(d *core.Drawing) {
	if e.Doc == nil || e.Doc.Camera == nil {
		return
	}
	cam := e.Doc.Camera
	vw, vh := e.Doc.VirtualWidth, e.Doc.VirtualHeight
	zoom := cam.EffectiveZoom()
	cx, cy := vw/2, vh/2
	camX, camY := clampCameraEditor(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)
	if zoom == 1 && cam.Rotation == 0 {
		d.Translate(cx-camX, cy-camY)
		return
	}
	if cx != 0 || cy != 0 {
		d.Translate(cx, cy)
	}
	if zoom != 1 {
		d.Scale(zoom, zoom)
	}
	if cam.Rotation != 0 {
		d.Rotate(-cam.Rotation)
	}
	if camX != 0 || camY != 0 {
		d.Translate(-camX, -camY)
	}
}

// ViewportView renders the center interactive core.Canvas viewport panel.
func ViewportView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	vw, vh := ed.ViewportSize()
	var lastPan core.Point

	return core.View{
		TestID: "editor-viewport-container",
		Style: core.Style{
			FlexGrow:       1,
			Background:     colors.Surface(),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Padding:        core.All(16),
			Overflow:       core.Hidden,
		},
		Children: []core.Node{
			core.View{
				TestID: "editor-viewport-frame",
				Style: core.Style{
					Width:  core.Pt(core.Points(vw)),
					Height: core.Pt(core.Points(vh)),
				},
				OnWheel: func(ev core.WheelEvent) {
					if ed.ViewMode == ViewMode3D {
						ed.HandleViewport3DWheel(float32(ev.Delta.Y))
					}
				},
				OnPan: func(ev core.PanEvent) {
					if ed.ViewMode != ViewMode3D {
						return
					}
					switch ev.Phase {
					case core.GestureBegan:
						lastPan = ev.Translation
					case core.GestureChanged:
						dx := float32(ev.Translation.X - lastPan.X)
						dy := float32(ev.Translation.Y - lastPan.Y)
						lastPan = ev.Translation
						ed.Camera3D.Orbit(-dx*0.01, dy*0.01)
						ed.notify()
					case core.GestureEnded, core.GestureCancelled:
						lastPan = core.Point{}
					}
				},
				Children: []core.Node{
					core.Canvas{
						TestID: "editor-viewport",
						Label:  "Scene Viewport",
						Style: core.Style{
							Width:       core.Pt(core.Points(vw)),
							Height:      core.Pt(core.Points(vh)),
							BorderWidth: 1,
							BorderColor: colors.Line(),
						},
						OnPress: func(ev core.PressEvent) {
							ed.HandleCanvasPress(float32(ev.Position.X), float32(ev.Position.Y))
						},
						Draw: func(d *core.Drawing, size core.Size) {
							_ = rev.Get()
							ed.DrawViewport(d, size)
						},
					},
				},
			},
		},
	}
}
