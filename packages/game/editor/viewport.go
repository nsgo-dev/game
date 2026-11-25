package editor

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/world3d"
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

// CanvasToWorld converts a point on the viewport's canvas into world coordinates through the 2D view (View2D), which the author pans and zooms independently of the scene's camera.
func (e *Editor) CanvasToWorld(x, y float32) (float32, float32) {
	if e == nil {
		return x, y
	}
	v := e.View2D()
	cw, ch := e.CanvasSize()
	rx := (x - cw/2) / v.Zoom
	ry := (y - ch/2) / v.Zoom
	if v.Rotation == 0 {
		return v.X + rx, v.Y + ry
	}
	sin, cos := math.Sincos(float64(v.Rotation))
	wx := v.X + float32(float64(rx)*cos-float64(ry)*sin)
	wy := v.Y + float32(float64(rx)*sin+float64(ry)*cos)
	return wx, wy
}

// WorldToCanvas converts a world-space coordinate into a point on the viewport's canvas through the 2D view (View2D).
func (e *Editor) WorldToCanvas(wx, wy float32) (float32, float32) {
	if e == nil {
		return wx, wy
	}
	v := e.View2D()
	cw, ch := e.CanvasSize()
	dx, dy := wx-v.X, wy-v.Y
	if v.Rotation == 0 {
		return cw/2 + dx*v.Zoom, ch/2 + dy*v.Zoom
	}
	sin, cos := math.Sincos(float64(-v.Rotation))
	rx := float32(float64(dx)*cos - float64(dy)*sin)
	ry := float32(float64(dx)*sin + float64(dy)*cos)
	return cw/2 + rx*v.Zoom, ch/2 + ry*v.Zoom
}

// CanvasToScreen converts a point on the viewport's canvas into the scene's virtual screen, the space screen-space layers draw in, through the 2D view and back out through the scene's camera.
func (e *Editor) CanvasToScreen(x, y float32) (float32, float32) {
	wx, wy := e.CanvasToWorld(x, y)
	return e.WorldToScreen(wx, wy)
}

// ScreenToCanvas converts a point on the scene's virtual screen, where screen-space layers draw, into a point on the viewport's canvas.
func (e *Editor) ScreenToCanvas(x, y float32) (float32, float32) {
	wx, wy := e.ScreenToWorld(x, y)
	return e.WorldToCanvas(wx, wy)
}

// ScreenToWorld converts a point on the scene's virtual screen into world coordinates by inverting the scene's camera, as the game sees the world.
func (e *Editor) ScreenToWorld(x, y float32) (float32, float32) {
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

// WorldToScreen converts a world-space coordinate into a point on the scene's virtual screen through the scene's camera, as the game draws world-space layers.
func (e *Editor) WorldToScreen(wx, wy float32) (float32, float32) {
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

// CanvasToTile converts a point on the viewport's canvas into (col, row) on layer's inline tilemap.
func (e *Editor) CanvasToTile(layer string, canvasX, canvasY float32) (col, row int, ok bool) {
	layerName, tm, err := e.resolveInlineTilemap(layer)
	if err != nil {
		return 0, 0, false
	}
	space, px, py := e.layerSpaceAndParallax(layerName)
	lx, ly := e.canvasToLayerFor(space, px, py, canvasX, canvasY)
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

// HitTest returns the ID of the topmost visible object under the point (canvasX, canvasY) on the viewport's canvas, seen through the 2D view, nested children included, or "" if no object is hit. Objects hidden or locked in the editor (SetObjectHidden, SetObjectLocked), with their descendants, and the objects of hidden and locked layers (SetLayerVisible, SetLayerLocked) are passed over, so the press reaches what lies under them. An object that only an instanced scene adds is reported as the nearest of its ancestors that the open scene holds, the instance it belongs to.
func (e *Editor) HitTest(canvasX, canvasY float32) string {
	scene := e.ActiveScene()
	if scene == nil {
		return ""
	}
	// Walk layers from topmost (drawn last) to bottommost.
	for i := len(scene.Layers) - 1; i >= 0; i-- {
		layer := scene.Layers[i]
		if !layer.Visible || e.layerLocked[layer.Name] {
			continue
		}
		lx, ly := e.canvasToLayerFor(layer.Space, layer.ParallaxX, layer.ParallaxY, canvasX, canvasY)
		// Find the topmost object in this layer: the highest Z, ties broken by the later in draw order, which is depth first with each parent before its children.
		var best *sceneformat.BuiltObject
		bestZ := math.MinInt
		var walk func(objs []*sceneformat.BuiltObject)
		walk = func(objs []*sceneformat.BuiltObject) {
			for _, bo := range objs {
				if e.hidden[bo.ID] || e.locked[bo.ID] {
					continue
				}
				if hitTestBuiltObject(bo, lx, ly) {
					if z := bo.Doc.Z; best == nil || z >= bestZ {
						best, bestZ = bo, z
					}
				}
				walk(bo.Children)
			}
		}
		walk(layer.Objects)
		if best != nil {
			for bo := best; bo != nil; bo = bo.Parent {
				if obj, _, _ := e.FindObject(bo.ID); obj != nil {
					return bo.ID
				}
			}
			return best.ID
		}
	}
	return ""
}

// hitTestBuiltObject reports whether the layer-space point (lx, ly) falls on bo: inside its sprite's frame, or for an object without a sprite inside the marker box the selection outline draws, both in bo's own space under its world transform.
func hitTestBuiltObject(bo *sceneformat.BuiltObject, lx, ly float32) bool {
	if bo == nil || bo.Doc == nil {
		return false
	}
	o := bo.Doc
	inv, ok := bo.World.Invert()
	if !ok {
		return false
	}
	p := inv.TransformPoint(math2d.Vec2{X: lx, Y: ly})
	dx, dy := p.X, p.Y

	if bo.Sprite != nil && bo.Sprite.Frame.Width > 0 && bo.Sprite.Frame.Height > 0 {
		u := dx + o.OriginX
		v := dy + o.OriginY
		return u >= 0 && u <= bo.Sprite.Frame.Width && v >= 0 && v <= bo.Sprite.Frame.Height
	}

	// Non-sprite marker object: default centered hit box, or inside an occluder2d shape.
	if dx >= -defaultMarkerHalfSize && dx <= defaultMarkerHalfSize &&
		dy >= -defaultMarkerHalfSize && dy <= defaultMarkerHalfSize {
		return true
	}
	return hitTestOccluder2D(o, dx, dy)
}

// HandleCanvasPress dispatches a click/tap at canvas coordinates (x, y) according to the active ViewMode and Tool. On a locked active layer (SetLayerLocked) the Place, Paint, Erase and Fill tools change nothing and the status bar says why. With the Pick tool, a press on a painted cell makes its tile the active tile and Paint the active tool.
func (e *Editor) HandleCanvasPress(x, y float32) {
	if e == nil || e.playing {
		return
	}
	if e.ViewMode == ViewMode3D {
		cw, ch := e.CanvasSize()
		if e.gridMap.tool != GridMapToolNone {
			if e.BeginGridMapStroke(e.gridMap.tool, x, y, cw, ch) {
				e.EndGridMapStroke()
			}
			return
		}
		e.HandleViewport3DPress(x, y, cw, ch)
		return
	}
	if e.connPick != nil {
		if hit := e.HitTest(x, y); hit != "" {
			e.finishConnectionPick(hit)
		}
		return
	}
	switch e.Tool {
	case ToolSelect:
		if e.handleShapePress2D(x, y, false) {
			return
		}
		hit := e.HitTest(x, y)
		e.SelectObject(hit)
	case ToolMove:
		if e.handleShapePress2D(x, y, false) {
			return
		}
		hit := e.HitTest(x, y)
		if hit != "" && hit != e.SelectedID {
			e.SelectObject(hit)
			return
		}
		if e.SelectedID != "" && !e.viewportFrozen(e.SelectedID) {
			wx, wy := e.layerCoordsForSelected(x, y)
			// A nested child's position is in its parent's space.
			path := objectPath(e.Doc, e.SelectedID)
			if inv, ok := chain2(path[:max(len(path)-1, 0)]).invert(); ok {
				lx, ly := inv.apply(float64(wx), float64(wy))
				_ = e.MoveObject(e.SelectedID, float32(lx), float32(ly))
			}
		}
	case ToolPlace, ToolPaintTile, ToolEraseTile, ToolFillTile, ToolTerrain:
		if err := e.lockedLayerErr(); err != nil {
			e.report(err)
			return
		}
		e.pressActiveLayer(x, y)
	case ToolPickTile:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok && e.PickTile(e.ActiveLayer, col, row) {
			e.SetTool(ToolPaintTile)
		}
	case ToolPlugin:
		if t := e.activePluginTool(); t != nil && t.Press != nil {
			t.Press(e, x, y)
		}
	}
}

// pressActiveLayer runs the Place, Paint, Erase or Fill tool at canvas point (x, y) on the active layer.
func (e *Editor) pressActiveLayer(x, y float32) {
	switch e.Tool {
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
	case ToolTerrain:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok {
			if err := e.PaintTerrain(e.ActiveLayer, col, row, e.ActiveTerrain); err != nil {
				e.report(err)
			}
		}
	}
}

func (e *Editor) layerCoordsForActive(x, y float32) (float32, float32) {
	if e.layerIndex(e.ActiveLayer) >= 0 {
		space, px, py := e.layerSpaceAndParallax(e.ActiveLayer)
		return e.canvasToLayerFor(space, px, py, x, y)
	}
	return e.CanvasToWorld(x, y)
}

func (e *Editor) layerCoordsForSelected(x, y float32) (float32, float32) {
	if _, li, _ := e.FindObject(e.SelectedID); li >= 0 {
		space, px, py := e.layerSpaceAndParallax(e.Doc.Layers[li].Name)
		return e.canvasToLayerFor(space, px, py, x, y)
	}
	return e.CanvasToWorld(x, y)
}

// DrawViewport renders the active scene in 2D or 3D mode and, when in edit mode, the appropriate editor overlays (the scene camera's frame and bounds, tile grid, selection outline, the selection's transform gizmo and the active plugin tool's Draw in 2D; grid, axes, boxes and gizmo in 3D). In 2D edit mode the scene draws through the 2D view (View2D) over the whole canvas, previewing each world-space layer's parallax offset and repeat tiling as the 2D view pans; in Play mode the player's frame, the scene's virtual screen, is all that shows. size is the canvas's size, which it records (SetCanvasSize).
func (e *Editor) DrawViewport(d *core.Drawing, size core.Size) {
	if e == nil || d == nil {
		return
	}
	e.SetCanvasSize(float32(size.Width), float32(size.Height))
	if e.ViewMode == ViewMode3D {
		e.DrawViewport3D(d, size)
		return
	}
	scene := e.ActiveScene()
	if scene != nil {
		if !e.playing && scene.HasBackground {
			// The scene fills its background over the camera frame only; in edit mode the view can be panned or zoomed past that frame, so the whole canvas gets the scene's background and objects away from the camera are judged against the game's clear color.
			d.Save().SetFillColor(scene.BackgroundColor).FillRect(0, 0, float32(size.Width), float32(size.Height)).Restore()
		}
		if e.playing {
			d.Save()
			applyAffine(d, e.screenAffine())
			vw, vh := e.ViewportSize()
			d.BeginPath().Rect(0, 0, vw, vh).Clip()
			scene.Draw(d)
			d.Restore()
		} else {
			e.drawEditScene(d, scene, size)
		}
	}
	if scene == nil {
		return
	}
	e.drawPhysicsDebug2D(d, scene)
	if e.playing {
		return
	}
	e.drawLightingOverlays2D(d, scene)
	e.drawCameraFrameOverlay(d)
	e.drawTileGridOverlay(d)
	e.drawTileHoverOverlay(d)
	e.drawSelectionOverlay(d, scene)
	e.drawComponentShapes2D(d, scene)
	e.drawGizmo2D(d)
	e.drawComponentShapeHandles2D(d)
	if t := e.activePluginTool(); t != nil && t.Draw != nil {
		// Save and Restore keep a transform or style the tool leaves set from reaching the next frame's drawing.
		d.Save()
		t.Draw(e, d)
		d.Restore()
	}
}

// applyAffine multiplies d's transform by m, leaving it alone for the identity.
func applyAffine(d *core.Drawing, m math2d.Affine) {
	if m == math2d.Identity() {
		return
	}
	d.Transform(m.A, m.B, m.C, m.D, m.Tx, m.Ty)
}

// strokePolygon strokes the closed polygon through pts, given in canvas points.
func strokePolygon(d *core.Drawing, pts []math2d.Vec2) {
	if len(pts) == 0 {
		return
	}
	d.BeginPath().MoveTo(pts[0].X, pts[0].Y)
	for _, p := range pts[1:] {
		d.LineTo(p.X, p.Y)
	}
	d.ClosePath().Stroke()
}

// drawCameraFrameOverlay outlines what the player sees: the scene's virtual screen through
// the scene's camera, and, dashed, the camera's bounds. When scene settings are shown in the
// inspector (no object selected), the start view frame and bounds are highlighted and a
// draggable handle marks the camera's world position.
func (e *Editor) drawCameraFrameOverlay(d *core.Drawing) {
	corners := func(x, y, w, h float32, to func(x, y float32) (float32, float32)) []math2d.Vec2 {
		pts := make([]math2d.Vec2, 0, 4)
		for _, c := range [4][2]float32{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}} {
			cx, cy := to(c[0], c[1])
			pts = append(pts, math2d.Vec2{X: cx, Y: cy})
		}
		return pts
	}
	vw, vh := e.ViewportSize()
	highlight := e.ShowingSceneSettings()
	d.Save()
	if highlight {
		d.SetStrokeColor(core.RGBA(0x3B7BFF, 1)).SetLineWidth(1.5)
	} else {
		d.SetStrokeColor(core.RGBA(0xF8FAFC, 0.7)).SetLineWidth(1)
	}
	strokePolygon(d, corners(0, 0, vw, vh, e.ScreenToCanvas))
	if e.Doc != nil && e.Doc.Camera != nil && e.Doc.Camera.Bounds != nil {
		b := e.Doc.Camera.Bounds
		if highlight {
			d.SetStrokeColor(core.RGBA(0xE9A84A, 0.95)).SetLineWidth(1.5).SetLineDash(6, 4)
		} else {
			d.SetStrokeColor(core.RGBA(0xFBBF24, 0.8)).SetLineDash(6, 4)
		}
		strokePolygon(d, corners(b.X, b.Y, b.Width, b.Height, e.WorldToCanvas))
	}
	if hx, hy, ok := e.CameraHandleCanvasPos(); ok {
		// If bounds clamp the visible frame away from the camera's configured position,
		// draw a dashed tether from the handle to the clamped frame center.
		fcx, fcy := e.ScreenToCanvas(vw/2, vh/2)
		if math.Hypot(float64(hx-fcx), float64(hy-fcy)) > 1 {
			d.SetStrokeColor(core.RGBA(0x3B7BFF, 0.6)).SetLineWidth(1).SetLineDash(4, 3)
			d.BeginPath().MoveTo(hx, hy).LineTo(fcx, fcy).Stroke()
		}
		// Reset dash for the solid crosshair and circular handle.
		d.SetLineDash()
		const arm float32 = 10
		d.SetStrokeColor(core.RGBA(0x3B7BFF, 1)).SetLineWidth(1.5)
		d.BeginPath().MoveTo(hx-arm, hy).LineTo(hx+arm, hy).MoveTo(hx, hy-arm).LineTo(hx, hy+arm).Stroke()
		r := float32(5)
		fill := core.RGBA(0xFFFFFF, 1)
		stroke := core.RGBA(0x3B7BFF, 1)
		if e.cameraDrag.active {
			r = 6
			fill = core.RGBA(0x3B7BFF, 1)
			stroke = core.RGBA(0xFFFFFF, 1)
		}
		d.SetFillColor(fill).SetStrokeColor(stroke).SetLineWidth(1.5)
		d.BeginPath().Ellipse(hx, hy, r, r).Fill().Stroke()
	}
	d.Restore()
}

func (e *Editor) drawTileGridOverlay(d *core.Drawing) {
	if !isTileTool(e.Tool) {
		return
	}
	layerName, tm, err := e.resolveInlineTilemap(e.ActiveLayer)
	if err != nil || tm == nil {
		return
	}
	space, px, py := e.layerSpaceAndParallax(layerName)
	m := e.layerAffineFor(space, px, py)
	d.Save()
	applyAffine(d, m)
	// The grid is a hairline at any zoom.
	d.SetStrokeColor(core.RGBA(0xFFFFFF, 0.2)).SetLineWidth(1 / strokeScale(m))
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

// drawEditScene renders scene into d in edit mode: materials are synced, editor-hidden objects are suppressed, each visible layer draws through the 2D view with its parallax factors and repeat tiling applied, and when ShowLightingPreview is enabled, scene.Lights2D renders after the last visible world-space layer using the visible 2D canvas bounds.
func (e *Editor) drawEditScene(d *core.Drawing, scene *sceneformat.BuiltScene, size core.Size) {
	scene.SyncMaterials()
	type faded struct {
		bo    *sceneformat.BuiltObject
		alpha float32
	}
	var restore []faded
	if len(e.hidden) > 0 {
		for _, layer := range scene.Layers {
			var walk func(objs []*sceneformat.BuiltObject, hidden bool)
			walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
				for _, bo := range objs {
					h := hidden || e.hidden[bo.ID]
					if h && bo.Sprite != nil {
						restore = append(restore, faded{bo, bo.Sprite.Alpha})
						bo.Sprite.Alpha = 0
					}
					walk(bo.Children, h)
				}
			}
			walk(layer.Objects, false)
		}
	}

	lastWorld := -1
	for i, layer := range scene.Layers {
		if layer.Visible && layer.Space == sceneformat.SpaceWorld {
			lastWorld = i
		}
	}
	lightsDrawn := false
	drawLights := func() {
		if lightsDrawn || !e.ShowLightingPreview || scene.SkipLights2D || scene.Lights2D == nil {
			return
		}
		lightsDrawn = true
		lb := e.editorLightingBounds(size)
		if lb.Size.Width <= 0 || lb.Size.Height <= 0 {
			return
		}
		d.Save()
		applyAffine(d, e.layerAffine(sceneformat.SpaceWorld))
		scene.Lights2D.Draw(d, lb)
		d.Restore()
	}
	if lastWorld < 0 {
		drawLights()
	}

	v := e.View2D()
	cw, ch := e.CanvasSize()
	for i, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		if layer.Tilemap != nil || layer.Map != nil || len(layer.Batches) > 0 {
			m := e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY)
			d.Save()
			applyAffine(d, m)
			if layer.Space == sceneformat.SpaceWorld && (layer.RepeatX || layer.RepeatY) {
				minCol, maxCol, minRow, maxRow, rx, ry := layer.RepeatTileRanges(
					v.X*layer.ParallaxX, v.Y*layer.ParallaxY, cw, ch, v.Zoom, v.Rotation,
				)
				if !rx && !ry {
					drawBuiltLayer(d, layer)
				} else {
					for row := minRow; row <= maxRow; row++ {
						var ty float32
						if ry {
							ty = float32(row) * layer.RepeatHeight
						}
						for col := minCol; col <= maxCol; col++ {
							var tx float32
							if rx {
								tx = float32(col) * layer.RepeatWidth
							}
							if tx != 0 || ty != 0 {
								d.Save()
								d.Translate(tx, ty)
							}
							drawBuiltLayer(d, layer)
							if tx != 0 || ty != 0 {
								d.Restore()
							}
						}
					}
				}
			} else {
				drawBuiltLayer(d, layer)
			}
			d.Restore()
		}
		if i == lastWorld {
			drawLights()
		}
	}
	drawLights()
	for _, f := range restore {
		f.bo.Sprite.Alpha = f.alpha
	}
}

func drawBuiltLayer(d *core.Drawing, layer *sceneformat.BuiltLayer) {
	if layer.Tilemap != nil {
		layer.Tilemap.Draw(d)
	}
	if layer.Map != nil {
		layer.Map.Draw(d, nil)
	}
	for _, b := range layer.Batches {
		b.Draw(d)
	}
}

func (e *Editor) drawSelectionOverlay(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e.SelectedID == "" {
		return
	}
	bo := scene.Object(e.SelectedID)
	if bo == nil || bo.Doc == nil || viewFlagged(e.hidden, bo) {
		return
	}
	space := sceneformat.SpaceWorld
	var px, py float32 = 1, 1
	if layer := scene.Layer(bo.Layer); layer != nil {
		space = layer.Space
		px, py = layer.ParallaxX, layer.ParallaxY
	}
	m := e.layerAffineFor(space, px, py).Mul(bo.World)
	rx, ry, rw, rh := objectOutline(bo)
	corners := []math2d.Vec2{
		m.TransformPoint(math2d.Vec2{X: rx, Y: ry}),
		m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry}),
		m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry + rh}),
		m.TransformPoint(math2d.Vec2{X: rx, Y: ry + rh}),
	}
	d.Save()
	d.SetStrokeColor(core.RGBA(0x38BDF8, 1)).SetLineWidth(1.5)
	strokePolygon(d, corners)
	d.Restore()
}

// wheelZoomPoints is how far a wheel moves the content, in points, to double or halve the 2D view's zoom: a notch of 40 points zooms by about 15%.
const wheelZoomPoints = 200

// viewportGestures is the 2D viewport's navigation and camera-handle drag in progress:
// a middle-button drag, a Space-drag, a pinch, a two-finger touch pan, and dragging the
// scene camera handle while scene settings are shown.
type viewportGestures struct {
	middle        bool
	middleLast    core.Point
	spacePan      bool
	panLast       core.Point
	pinchLast     float32
	touchPan      bool
	touchLast     core.Point
	cameraDrag    bool
	touchCamPos   core.Point
	hasTouchCam   bool
	panCamDrag    bool
	suppressPress bool
}

// wheel pans the 2D view by a touchpad's scroll and zooms it about the pointer by a mouse wheel's turn or a scroll with Control or Command held. A mouse wheel's sideways scroll (Shift+wheel, or a tilting wheel) pans sideways.
func (g *viewportGestures) wheel(ed *Editor, ev core.WheelEvent) {
	if ev.Touchpad && !ev.Modifiers.Control && !ev.Modifiers.Platform {
		ed.PanView2D(float32(ev.Delta.X), float32(ev.Delta.Y))
		return
	}
	if !ev.Touchpad && ev.Delta.X != 0 {
		ed.PanView2D(float32(ev.Delta.X), 0)
	}
	if dy := float64(ev.Delta.Y); dy != 0 {
		ed.ZoomView2DAt(float32(ev.Position.X), float32(ev.Position.Y), float32(math.Exp2(dy/wheelZoomPoints)))
	}
}

// pan follows a one-finger or primary-button drag in 2D: with Space held, or during a middle-button drag, it pans the view; otherwise, with the Select or Move tool, a touch drag on the camera handle moves the camera (BeginCameraDrag) and a drag that starts on the selection's gizmo or an object moves, turns or scales it (BeginDrag2D), ending as one undoable step, or as nothing when the gesture is cancelled.
func (g *viewportGestures) pan(ed *Editor, ev core.PanEvent, spaceHeld, shiftHeld func() bool) {
	at := func() (float32, float32) { return float32(ev.Position.X), float32(ev.Position.Y) }
	switch ev.Phase {
	case core.GestureBegan:
		g.spacePan = spaceHeld()
		g.panLast = ev.Translation
		g.panCamDrag = false
		if g.spacePan || g.middle || g.touchPan || g.cameraDrag || ed.PickingConnectionTarget() {
			return
		}
		// The pan begins once the pointer passes the platform's slop, so the drag starts where it went down: its position less how far it has come.
		downX, downY := float32(ev.Position.X-ev.Translation.X), float32(ev.Position.Y-ev.Translation.Y)
		if ed.Tool == ToolPlugin {
			if ed.BeginPluginDrag2D(downX, downY) {
				x, y := at()
				ed.UpdatePluginDrag2D(x, y)
			}
			return
		}
		if _, ok := ed.BeginShapeDrag2D(downX, downY); ok {
			x, y := at()
			ed.UpdateShapeDrag2D(x, y)
			return
		}
		if ed.BeginDrag2D(downX, downY) != Handle2DNone {
			x, y := at()
			ed.UpdateDrag2D(x, y, shiftHeld())
			return
		}
		if g.hasTouchCam && ed.BeginCameraDrag(float32(g.touchCamPos.X), float32(g.touchCamPos.Y)) {
			g.panCamDrag = true
			ed.UpdateCameraDrag(
				float32(g.touchCamPos.X)+float32(ev.Translation.X),
				float32(g.touchCamPos.Y)+float32(ev.Translation.Y),
			)
		}
	case core.GestureChanged:
		if g.panCamDrag {
			if !ed.cameraDrag.active {
				g.panCamDrag = false
			} else {
				ed.UpdateCameraDrag(
					float32(g.touchCamPos.X)+float32(ev.Translation.X),
					float32(g.touchCamPos.Y)+float32(ev.Translation.Y),
				)
			}
		} else if g.spacePan {
			ed.PanView2D(float32(ev.Translation.X-g.panLast.X), float32(ev.Translation.Y-g.panLast.Y))
		} else if ed.PluginDragging2D() {
			x, y := at()
			ed.UpdatePluginDrag2D(x, y)
		} else if ed.DraggingShape2D() {
			x, y := at()
			ed.UpdateShapeDrag2D(x, y)
		} else if ed.Dragging2D() != Handle2DNone {
			x, y := at()
			ed.UpdateDrag2D(x, y, shiftHeld())
		}
		g.panLast = ev.Translation
	case core.GestureEnded:
		if g.panCamDrag {
			g.panCamDrag = false
			if ed.EndCameraDrag() {
				g.suppressPress = true
			}
		} else if !g.spacePan && ed.PluginDragging2D() {
			x, y := at()
			if (math2d.Vec2{X: x, Y: y}) != ed.pluginDragLast {
				ed.UpdatePluginDrag2D(x, y)
			}
			ed.EndPluginDrag2D(x, y)
			g.suppressPress = true
		} else if !g.spacePan && ed.DraggingShape2D() {
			x, y := at()
			ed.UpdateShapeDrag2D(x, y)
			if ed.EndShapeDrag2D() {
				g.suppressPress = true
			}
		} else if !g.spacePan && ed.Dragging2D() != Handle2DNone {
			x, y := at()
			ed.UpdateDrag2D(x, y, shiftHeld())
			ed.EndDrag2D()
		}
		g.spacePan = false
		g.panLast = core.Point{}
	default:
		if g.panCamDrag {
			g.panCamDrag = false
			ed.CancelCameraDrag()
		}
		ed.CancelPluginDrag2D()
		ed.CancelShapeDrag2D()
		ed.CancelDrag2D()
		g.spacePan = false
		g.panLast = core.Point{}
	}
}

// pinch zooms the 2D view about the pinch's center by how much the pinch scaled since its last report.
func (g *viewportGestures) pinch(ed *Editor, ev core.GestureEvent) {
	switch ev.Phase {
	case core.GestureBegan:
		g.pinchLast = ev.Value
	case core.GestureChanged, core.GestureEnded:
		if g.pinchLast > 0 && ev.Value > 0 {
			ed.ZoomView2DAt(float32(ev.Position.X), float32(ev.Position.Y), ev.Value/g.pinchLast)
		}
		g.pinchLast = ev.Value
		if ev.Phase == core.GestureEnded {
			g.pinchLast = 0
		}
	default:
		g.pinchLast = 0
	}
}

// touch pans the 2D view by the movement of the center of two fingers, and puts back a drag of an object (CancelDrag2D) or camera handle (CancelCameraDrag) when a second finger comes down or the touches are cancelled.
func (g *viewportGestures) touch(ed *Editor, ev core.TouchEvent) {
	if ev.Phase == core.TouchCancel {
		// The system took the touches: a drag of an object or camera they made is put back, whether or not the pan reports its own cancel. nsgotest's Finger.Cancel (nsgo v0.3.0 nsgotest/pointer.go:259) reports only the touch cancel and no GestureCancelled to an active OnPan, so the touch cancel is the one this drag can rely on in tests.
		ed.CancelPluginDrag2D()
		ed.CancelShapeDrag2D()
		ed.CancelDrag2D()
		ed.CancelCameraDrag()
		g.panCamDrag = false
		g.cameraDrag = false
	}
	if len(ev.Touches) != 2 || ev.Phase == core.TouchEnd || ev.Phase == core.TouchCancel {
		g.touchPan = false
		return
	}
	// A second finger turns a one-finger drag of an object or camera into navigation, so the drag is put back.
	ed.CancelPluginDrag2D()
	ed.CancelShapeDrag2D()
	ed.CancelDrag2D()
	ed.CancelCameraDrag()
	g.panCamDrag = false
	g.cameraDrag = false
	c := core.Point{
		X: (ev.Touches[0].Position.X + ev.Touches[1].Position.X) / 2,
		Y: (ev.Touches[0].Position.Y + ev.Touches[1].Position.Y) / 2,
	}
	if g.touchPan {
		ed.PanView2D(float32(c.X-g.touchLast.X), float32(c.Y-g.touchLast.Y))
	}
	g.touchPan = true
	g.touchLast = c
}

// touchStroke draws a tile stroke with one finger: it begins as the finger goes down, follows it and ends as it lifts. A second finger turns the touch into a two-finger pan, so the stroke is cancelled and its cells put back, as is a stroke whose touches the system takes.
func touchStroke(ev core.TouchEvent, begin func(at core.Point, secondary, alt bool), end func(at core.Point), ed *Editor) {
	if ev.Phase == core.TouchCancel {
		ed.CancelTileStroke()
		return
	}
	_, active := ed.TileStrokeActive()
	if len(ev.Touches) != 1 {
		if active && len(ev.Touches) > 1 {
			ed.CancelTileStroke()
		}
		return
	}
	at := ev.Touches[0].Position
	switch ev.Phase {
	case core.TouchStart:
		begin(at, false, false)
	case core.TouchMove:
		ed.ContinueTileStroke(float32(at.X), float32(at.Y))
	case core.TouchEnd:
		end(at)
	}
}

// ViewportView renders the center interactive core.Canvas viewport panel, which fills the panel.
//
// In 2D, with the Select or Move tool, a drag on an object moves it, and a drag on the selection's gizmo moves it along one of its axes, turns it with the ring or scales it with a corner or edge handle (Shift keeps the aspect ratio), each recorded as one undoable step when the drag ends; Escape or a cancelled gesture puts the object back (BeginDrag2D). A mouse wheel, a pinch or a touchpad scroll with Control or Command held zooms the 2D view (View2D) about the pointer, and a touchpad's scroll, a mouse wheel's sideways scroll, a middle-button drag, a drag with Space held or a two-finger drag pans it; while the scene plays the view is the scene camera's and does not move; F frames the selection (FrameSelection2D), Home frames everything (FrameAll2D) and Mod+0 returns to the scene camera's view (ResetView2D). With a tile tool a drag with the mouse or one finger is a tile stroke (BeginTileStroke) over every cell it crosses, recorded as one undo step: Paint paints and Erase erases, a right-button drag erases with Paint, Alt with any tile tool picks the tile under the pointer, and Pick picks and hands over to Paint; the cell under the resting mouse is outlined (HoveredTile). In 3D a drag orbits the camera or drags the gizmo; a middle-button drag, a Shift-drag, a touchpad's scroll and a two-finger drag pan it; the wheel and a pinch zoom it; holding the right button flies it, the pointer turning it and W, A, S, D, Q and E moving it (BeginFly3D); F frames the selection; and a bar, an axis widget and the controls' help line over the scene switch the projection, turn to the axis views, orbit by 15 degrees and reset the view.
func ViewportView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	var g viewportGestures
	var g3 viewport3DGestures
	// frame is the view the 3D viewport's flight locks the pointer to.
	var frame core.Ref

	// The first read of the held keys asks the platform to report every key, so Space is known to be held when a drag begins.
	spaceHeld := func() bool { return core.Untracked(cx, func() bool { return core.KeyHeld(cx, "space") }) }
	spaceHeld()
	// A pan event carries no modifiers, so Shift, which keeps the aspect ratio while scaling, is read from the held keys.
	shiftHeld := func() bool { return core.Untracked(cx, func() bool { return core.KeyHeld(cx, "shift") }) }
	altHeld := func() bool { return core.Untracked(cx, func() bool { return core.KeyHeld(cx, "alt") }) }
	in2D := func() bool { return ed.ViewMode == ViewMode2D && !ed.Playing() }
	in3DGridMap := func() bool { return ed.ViewMode == ViewMode3D && ed.GridMapTool() != GridMapToolNone && !ed.Playing() }
	// strokeTookPress says the tile or GridMap stroke that a pointer drew has ended, so the press its button or finger completes, which may come after it, edits nothing more. Every pointer-down clears it, so a stroke that no press followed, such as a drag the frame's pan took or a right-button drag, cannot swallow the next click.
	var strokeTookPress bool
	beginStroke := func(at core.Point, secondary, alt bool) {
		strokeTookPress = false
		// A press with Space held navigates rather than edits, and connection target pick mode intercepts clicks instead of painting tiles.
		if !in2D() || spaceHeld() || ed.PickingConnectionTarget() {
			return
		}
		if mode, ok := ed.toolStrokeMode(secondary, alt || altHeld()); ok {
			ed.BeginTileStroke(mode, float32(at.X), float32(at.Y))
		}
	}
	endStroke := func(at core.Point, mayPress bool) {
		if _, ok := ed.TileStrokeActive(); !ok {
			return
		}
		ed.ContinueTileStroke(float32(at.X), float32(at.Y))
		ed.EndTileStroke()
		strokeTookPress = mayPress
	}
	cx.OnKey("f", func() {
		switch {
		case ed.ViewMode == ViewMode3D:
			ed.FrameSelection()
		case in2D():
			ed.FrameSelection2D()
		}
	})
	cx.OnKey("home", func() {
		if in2D() {
			ed.FrameAll2D()
		}
	})
	cx.OnKey("mod+0", func() {
		if in2D() {
			ed.ResetView2D()
		}
	})
	canvas := core.Canvas{
		TestID: "editor-viewport",
		Label:  "Scene Viewport",
		Style: core.Style{
			FlexGrow: 1,
		},
		OnLayout: func(b core.Bounds) {
			ed.SetCanvasSize(float32(b.Size.Width), float32(b.Size.Height))
		},
		OnPress: func(ev core.PressEvent) {
			if g.suppressPress {
				g.suppressPress = false
				return
			}
			// A press with Space held ends a Space-drag that did not move, which navigates rather than edits.
			if spaceHeld() {
				return
			}
			if ed.ViewMode == ViewMode2D && !ev.Keyboard {
				// The mouse button or finger that completes the press drew a stroke already, which edits the cell, or is still drawing one.
				if _, ok := ed.TileStrokeActive(); ok || strokeTookPress {
					strokeTookPress = false
					return
				}
			}
			if in3DGridMap() && !ev.Keyboard {
				if _, ok := ed.GridMapStrokeActive(); ok || strokeTookPress {
					strokeTookPress = false
					return
				}
			}
			x, y := float32(ev.Position.X), float32(ev.Position.Y)
			if ev.Modifiers.Alt && ed.ViewMode == ViewMode2D && !ed.Playing() && !ed.PickingConnectionTarget() {
				if ed.handleShapePress2D(x, y, true) {
					return
				}
			}
			// A press no pointer drew a stroke for, such as an accessibility action, makes the stroke of one cell unless picking a connection target.
			if !ed.PickingConnectionTarget() {
				if mode, ok := ed.toolStrokeMode(false, ev.Modifiers.Alt); ok && ed.ViewMode == ViewMode2D && ed.BeginTileStroke(mode, x, y) {
					ed.EndTileStroke()
					return
				}
				if in3DGridMap() {
					cw, ch := ed.CanvasSize()
					tool := ed.GridMapTool()
					if ev.Modifiers.Alt {
						tool = GridMapToolPick
					}
					if ed.BeginGridMapStroke(tool, x, y, cw, ch) {
						ed.EndGridMapStroke()
					}
					return
				}
			}
			ed.HandleCanvasPress(x, y)
		},
		Draw: func(d *core.Drawing, size core.Size) {
			_ = rev.Get()
			ed.DrawViewport(d, size)
		},
	}
	// 3D GridMap floor height and 24-orientation rotation keys, active only while a GridMap tool is chosen in 3D and no text field has focus.
	gridKey := core.KeyOptions{When: core.KeyNotEditing}
	cx.OnKeyWith("]", gridKey, func() {
		if in3DGridMap() {
			ed.StepGridMapFloor(1)
		}
	})
	cx.OnKeyWith("pageup", gridKey, func() {
		if in3DGridMap() {
			ed.StepGridMapFloor(1)
		}
	})
	cx.OnKeyWith("[", gridKey, func() {
		if in3DGridMap() {
			ed.StepGridMapFloor(-1)
		}
	})
	cx.OnKeyWith("pagedown", gridKey, func() {
		if in3DGridMap() {
			ed.StepGridMapFloor(-1)
		}
	})
	cx.OnKeyWith("x", gridKey, func() {
		if in3DGridMap() {
			ed.report(ed.RotateGridMapOrientation(world3d.GridAxisX, 1))
		}
	})
	cx.OnKeyWith("shift+x", gridKey, func() {
		if in3DGridMap() {
			ed.report(ed.RotateGridMapOrientation(world3d.GridAxisX, -1))
		}
	})
	cx.OnKeyWith("y", gridKey, func() {
		if in3DGridMap() {
			ed.report(ed.RotateGridMapOrientation(world3d.GridAxisY, 1))
		}
	})
	cx.OnKeyWith("shift+y", gridKey, func() {
		if in3DGridMap() {
			ed.report(ed.RotateGridMapOrientation(world3d.GridAxisY, -1))
		}
	})
	cx.OnKeyWith("z", gridKey, func() {
		if in3DGridMap() {
			ed.report(ed.RotateGridMapOrientation(world3d.GridAxisZ, 1))
		}
	})
	cx.OnKeyWith("shift+z", gridKey, func() {
		if in3DGridMap() {
			ed.report(ed.RotateGridMapOrientation(world3d.GridAxisZ, -1))
		}
	})
	cx.OnKeyWith("q", gridKey, func() {
		if in3DGridMap() {
			ed.report(ed.StepGridMapOrientation(1))
		}
	})
	cx.OnKeyWith("shift+q", gridKey, func() {
		if in3DGridMap() {
			ed.report(ed.StepGridMapOrientation(-1))
		}
	})

	return core.View{
		TestID: "editor-viewport-container",
		Style: core.Style{
			FlexGrow:   1,
			Position:   core.Relative,
			Background: colors.Surface(),
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			// Bind Escape only while a viewport drag or GridMap stroke is active so Escape still
			// reaches Popover/ColorWell/Menu OnCancel handlers at rest.
			core.Show{
				When: func() bool {
					_ = rev.Get()
					_, gmActive := ed.GridMapStrokeActive()
					return ed.Dragging2D() != Handle2DNone || ed.DraggingShape2D() || ed.PluginDragging2D() || ed.cameraDrag.active || gmActive
				},
				Then: func(cx *core.Scope) core.Node {
					cx.OnKey("escape", func() {
						ed.CancelPluginDrag2D()
						ed.CancelShapeDrag2D()
						ed.CancelDrag2D()
						ed.CancelCameraDrag()
						ed.CancelGridMapStroke()
						g.cameraDrag = false
						g.panCamDrag = false
					})
					return core.View{
						PointerEvents: core.PointerEventsNone,
						Style: core.Style{
							Position: core.Absolute,
							Width:    core.Pt(0),
							Height:   core.Pt(0),
						},
					}
				},
			},
			modelDropTarget(cx, ed, core.View{
				TestID:     "editor-viewport-frame",
				Ref:        &frame,
				Style:      core.Style{FlexGrow: 1},
				OnFileDrop: func(uris []string) { ed.dropModelFiles(uris) },
				OnWheel: func(ev core.WheelEvent) {
					if ed.ViewMode == ViewMode3D {
						g3.wheel(ed, ev)
						return
					}
					g.wheel(ed, ev)
				},
				OnPinch: func(ev core.GestureEvent) {
					switch ed.ViewMode {
					case ViewMode2D:
						g.pinch(ed, ev)
					case ViewMode3D:
						g3.pinch(ed, ev)
					}
				},
				OnPointerDelta: func(dx, dy float64) { g3.pointerDelta(ed, dx, dy) },
				OnMouseDown: func(ev core.MouseButtonEvent) {
					if ed.ViewMode == ViewMode3D {
						if in3DGridMap() && ev.Button == core.MouseButtonLeft && !spaceHeld() {
							strokeTookPress = false
							cw, ch := ed.CanvasSize()
							x, y := float32(ev.Position.X), float32(ev.Position.Y)
							tool := ed.GridMapTool()
							if ev.Modifiers.Alt || altHeld() {
								tool = GridMapToolPick
							}
							ed.BeginGridMapStroke(tool, x, y, cw, ch)
							return
						}
						g3.mouseDown(cx, ed, &frame, ev)
						return
					}
					if ed.ViewMode != ViewMode2D {
						return
					}
					switch ev.Button {
					case core.MouseButtonMiddle:
						g.middle, g.middleLast = true, ev.Position
					case core.MouseButtonLeft:
						g.suppressPress = false
						if !spaceHeld() && ed.BeginCameraDrag(float32(ev.Position.X), float32(ev.Position.Y)) {
							g.cameraDrag = true
							return
						}
						beginStroke(ev.Position, false, ev.Modifiers.Alt)
					case core.MouseButtonRight:
						if !spaceHeld() && !ed.PickingConnectionTarget() && ed.HandleCanvasSecondaryPress(float32(ev.Position.X), float32(ev.Position.Y)) {
							return
						}
						beginStroke(ev.Position, true, ev.Modifiers.Alt)
					}
				},
				OnMouseMove: func(ev core.MouseEvent) {
					if g.cameraDrag {
						if !ed.cameraDrag.active {
							g.cameraDrag = false
						} else if ev.PressedButton != core.MouseButtonLeft || ed.ViewMode != ViewMode2D {
							g.cameraDrag = false
							if ed.EndCameraDrag() {
								g.suppressPress = true
							}
							return
						} else {
							ed.UpdateCameraDrag(float32(ev.Position.X), float32(ev.Position.Y))
							return
						}
					}
					if ed.ViewMode == ViewMode3D {
						cw, ch := ed.CanvasSize()
						x, y := float32(ev.Position.X), float32(ev.Position.Y)
						if in3DGridMap() {
							ed.SetGridMapHover(x, y, cw, ch, ev.Modifiers.Alt)
						}
						if _, ok := ed.GridMapStrokeActive(); ok {
							if ev.PressedButton == core.MouseButtonNone {
								ed.ContinueGridMapStroke(x, y)
								ed.EndGridMapStroke()
							} else {
								ed.ContinueGridMapStroke(x, y)
							}
							return
						}
						g3.mouseMove(ed, ev)
						return
					}
					if ed.ViewMode == ViewMode2D {
						ed.SetTileHover(float32(ev.Position.X), float32(ev.Position.Y), ev.Modifiers.Alt)
						ed.SetPluginHover2D(float32(ev.Position.X), float32(ev.Position.Y))
					}
					if _, ok := ed.TileStrokeActive(); ok {
						if ev.PressedButton == core.MouseButtonNone {
							// The button came up where the viewport did not hear it.
							endStroke(ev.Position, false)
						} else {
							ed.ContinueTileStroke(float32(ev.Position.X), float32(ev.Position.Y))
						}
					}
					if !g.middle {
						return
					}
					if ev.PressedButton != core.MouseButtonMiddle || ed.ViewMode != ViewMode2D {
						g.middle = false
						return
					}
					ed.PanView2D(float32(ev.Position.X-g.middleLast.X), float32(ev.Position.Y-g.middleLast.Y))
					g.middleLast = ev.Position
				},
				OnMouseUp: func(ev core.MouseButtonEvent) {
					// A pan or flight that began in 3D ends whatever the view is now.
					g3.mouseUp(ed, ev)
					if ed.ViewMode == ViewMode3D {
						if _, ok := ed.GridMapStrokeActive(); ok {
							ed.ContinueGridMapStroke(float32(ev.Position.X), float32(ev.Position.Y))
							ed.EndGridMapStroke()
							strokeTookPress = ev.Button == core.MouseButtonLeft
						}
						return
					}
					switch ev.Button {
					case core.MouseButtonMiddle:
						g.middle = false
					case core.MouseButtonLeft:
						if g.cameraDrag {
							g.cameraDrag = false
							if ed.EndCameraDrag() {
								g.suppressPress = true
							}
							return
						}
						endStroke(ev.Position, true)
					case core.MouseButtonRight:
						endStroke(ev.Position, false)
					}
				},
				OnMouseLeave: func(core.MouseEvent) {
					ed.ClearTileHover()
					ed.ClearGridMapHover()
					ed.ClearPluginHover2D()
				},
				OnTouch: func(ev core.TouchEvent) {
					if ed.ViewMode != ViewMode3D {
						g.touch(ed, ev)
						switch ev.Phase {
						case core.TouchStart:
							if len(ev.Touches) == 1 {
								g.suppressPress = false
								g.touchCamPos = ev.Touches[0].Position
								g.hasTouchCam = true
							} else {
								g.hasTouchCam = false
							}
						case core.TouchEnd, core.TouchCancel:
							g.hasTouchCam = false
						}
						touchStroke(ev, beginStroke, func(at core.Point) { endStroke(at, true) }, ed)
						return
					}
					g3.touch(ed, ev)
				},
				OnPan: func(ev core.PanEvent) {
					if ed.ViewMode != ViewMode3D {
						g.pan(ed, ev, spaceHeld, shiftHeld)
						return
					}
					if _, active := ed.GridMapStrokeActive(); active {
						return
					}
					if in3DGridMap() && !spaceHeld() {
						return
					}
					g3.pan(ed, ev, shiftHeld)
				},
				Children: []core.Node{
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.ViewMode == ViewMode3D
						},
						// In 3D the canvas draws its overlays over the GPU view that draws the scene's geometry, or the whole wireframe view where the target has no GPU.
						Then: func(cx *core.Scope) core.Node {
							return viewport3DView(cx, ed, core.View{
								Style: core.Style{FlexGrow: 1},
								Children: append([]core.Node{
									canvas,
									viewport3DErrorView(cx, ed, rev),
									core.Show{
										When: func() bool {
											_ = rev.Get()
											return ed.Flying3D()
										},
										Then: func(cx *core.Scope) core.Node { return flyView3D(cx, ed) },
									},
								}, viewport3DOverlay(cx, ed, rev)...),
							})
						},
						Else: func(cx *core.Scope) core.Node { return canvas },
					},
				},
			}),
			viewport3DRenderStatsOverlay(cx, ed, rev),
		},
	}
}

// viewport3DErrorView shows, in a corner of the 3D viewport, why it cannot show the scene's geometry (Viewport3DError), and nothing when it can.
func viewport3DErrorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	message := func() string {
		_ = rev.Get()
		if err := ed.Viewport3DError(); err != nil {
			return err.Error()
		}
		return ""
	}
	return core.Show{
		When: func() bool { return message() != "" },
		Then: func(cx *core.Scope) core.Node {
			return core.Text{
				TestID:      "editor-viewport-3d-error",
				ContentFunc: message,
				Style: core.Style{
					Position:     core.Absolute,
					Left:         core.Pt(8),
					Top:          core.Pt(44),
					Right:        core.Pt(96),
					Padding:      core.Symmetric(4, 8),
					CornerRadius: 4,
					FontSize:     12,
					Color:        colors.Danger(),
					Background:   colors.Raised(),
				},
			}
		},
	}
}
