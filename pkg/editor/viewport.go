package editor

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/world3d"
	"github.com/nsgo-dev/nsgo/base"
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
	return tileAt(tm, lx, ly)
}

// HitTest returns the ID of the topmost visible object under the point (canvasX, canvasY) on the viewport's canvas, seen through the 2D view, nested, instanced and inherited objects included, or "" if no object is hit. Objects hidden or locked in the editor (SetObjectHidden, SetObjectLocked), with their descendants, and the objects of hidden and locked layers (SetLayerVisible, SetLayerLocked) are passed over, so the press reaches what lies under them.
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

	// Non-sprite marker object: default centered hit box, or inside an occluder2d or navregion2d shape.
	if dx >= -defaultMarkerHalfSize && dx <= defaultMarkerHalfSize &&
		dy >= -defaultMarkerHalfSize && dy <= defaultMarkerHalfSize {
		return true
	}
	return hitTestOccluder2D(o, dx, dy) || hitTestNavRegion2D(o, dx, dy)
}

// HandleCanvasPress dispatches a click/tap at canvas coordinates (x, y) according to the active ViewMode and Tool. On a locked active layer (SetLayerLocked) the Place, Paint, Erase and Fill tools change nothing and the status bar says why. With the Pick tool, a press on a painted cell makes its tile the active tile and Paint the active tool.
func (e *Editor) HandleCanvasPress(x, y float32) {
	e.HandleCanvasPressWith(x, y, false, false)
}

// HandleCanvasPressWith dispatches a click/tap at canvas coordinates (x, y) with optional
// Shift (add to selection) and Mod (toggle selection membership) modifiers.
func (e *Editor) HandleCanvasPressWith(x, y float32, shift, mod bool) {
	e.HandleCanvasPressWithModifiers(x, y, shift, mod, false)
}

// HandleCanvasPressWithModifiers dispatches a click/tap at canvas coordinates (x, y) with optional
// Shift (add), Mod (toggle), and Alt/Option (in 3D, select the hit child's parent or instance root) modifiers.
func (e *Editor) HandleCanvasPressWithModifiers(x, y float32, shift, mod, alt bool) {
	if e == nil || e.playing || e.HistoryPreviewActive() {
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
		e.HandleViewport3DPressWithModifiers(x, y, cw, ch, shift, mod, alt)
		return
	}
	if e.AgentPointingActive() {
		wx, wy := e.CanvasToWorld(x, y)
		e.PickAgentPointWorld(wx, wy)
		return
	}
	if e.connPick != nil {
		if hit := e.HitTest(x, y); hit != "" {
			e.finishConnectionPick(hit)
		}
		return
	}
	if e.RoomCreateMode() {
		return
	}
	switch e.Tool {
	case ToolSelect, ToolRotate, ToolScale:
		if !shift && !mod && e.SelectionCount() <= 1 && e.handleShapePress2D(x, y, false) {
			return
		}
		if hit := e.HitTest(x, y); hit != "" {
			switch {
			case mod:
				e.ToggleSelection(hit)
			case shift:
				e.AddToSelection(hit)
			default:
				e.SelectObject(hit)
			}
		} else if !shift && !mod {
			if rid := e.HitTestRoom(x, y); rid != "" {
				e.SelectRoom(rid)
			} else {
				e.ClearSelection()
			}
		}
	case ToolPan:
		// Pan tool drags the camera; bare clicks do not alter selection.
		return
	case ToolMove:
		if !shift && !mod && e.SelectionCount() <= 1 && e.handleShapePress2D(x, y, false) {
			return
		}
		hit := e.HitTest(x, y)
		if hit != "" {
			switch {
			case mod:
				e.ToggleSelection(hit)
				return
			case shift:
				e.AddToSelection(hit)
				return
			case !e.IsSelected(hit):
				e.SelectObject(hit)
				return
			case e.SelectionCount() > 1:
				return
			}
		}
		if shift || mod {
			return
		}
		draggable := e.draggableSelectedIDs()
		if len(draggable) == 1 {
			id := draggable[0]
			wx, wy := e.layerCoordsForObject(id, x, y)
			// A nested child's position is in its parent's space.
			path := e.resolvedObjectPath(id)
			if inv, ok := chain2(path[:max(len(path)-1, 0)]).invert(); ok {
				lx, ly := inv.apply(float64(wx), float64(wy))
				sx, sy := e.SnapPosition2D(lx, ly)
				e.report(e.MoveObject(id, float32(sx), float32(sy)))
			}
		} else if len(draggable) > 1 {
			if g, ok := e.gizmo2D(); ok {
				anchorID := draggable[0]
				if slices.Contains(draggable, e.SelectedID) {
					anchorID = e.SelectedID
				}
				var snappedLDX, snappedLDY float64
				useSnappedDelta := false
				if grid, pixel := e.positionSnapMode(); grid || pixel {
					if aObj, aLI, _ := e.FindObject(anchorID); aObj != nil {
						aPath := objectPath(e.Doc, anchorID)
						aChain := chain2(aPath[:max(len(aPath)-1, 0)])
						alx0, aly0 := aChain.apply(float64(aObj.X), float64(aObj.Y))
						aSpace, apx, apy := e.layerSpaceAndParallax(e.Doc.Layers[aLI].Name)
						gx, gy := e.canvasToLayerFor(aSpace, apx, apy, g.origin.X, g.origin.Y)
						dx, dy := e.canvasToLayerFor(aSpace, apx, apy, x, y)
						nx, ny := e.SnapPosition2D(alx0+float64(dx-gx), aly0+float64(dy-gy))
						snappedLDX, snappedLDY = nx-alx0, ny-aly0
						useSnappedDelta = true
					}
				}
				_ = e.History.Group(fmt.Sprintf("Move %d objects", len(draggable)), func() error {
					for _, id := range draggable {
						obj, layerName := e.FindResolvedObject(id)
						if obj == nil {
							continue
						}
						path := e.resolvedObjectPath(id)
						parentChain := chain2(path[:max(len(path)-1, 0)])
						inv, ok := parentChain.invert()
						if !ok {
							continue
						}
						var nx, ny float64
						if useSnappedDelta {
							lx0, ly0 := parentChain.apply(float64(obj.X), float64(obj.Y))
							nx, ny = inv.apply(lx0+snappedLDX, ly0+snappedLDY)
						} else {
							space, px, py := e.layerSpaceAndParallax(layerName)
							gx, gy := e.canvasToLayerFor(space, px, py, g.origin.X, g.origin.Y)
							dx, dy := e.canvasToLayerFor(space, px, py, x, y)
							gpx, gpy := inv.apply(float64(gx), float64(gy))
							dpx, dpy := inv.apply(float64(dx), float64(dy))
							nx, ny = float64(obj.X)+(dpx-gpx), float64(obj.Y)+(dpy-gpy)
						}
						if err := e.MoveObject(id, float32(nx), float32(ny)); err != nil {
							return err
						}
					}
					return nil
				})
			}
		}
	case ToolPlace, ToolPaintTile, ToolEraseTile, ToolFillTile, ToolTerrain, ToolRectTile, ToolLineTile, ToolStampTile, ToolRandomTile:
		if err := e.lockedLayerErr(); err != nil {
			e.report(err)
			return
		}
		e.pressActiveLayer(x, y)
	case ToolPickTile:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok && e.PickTile(e.ActiveLayer, col, row) {
			e.SetTool(ToolPaintTile)
		}
	case ToolNavTestPath:
		e.handleNavTestPathPress(x, y)
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
		sx, sy := e.SnapPosition2D(float64(wx), float64(wy))
		px, py := float32(sx), float32(sy)
		if e.SelectedPrefab != "" {
			if _, err := e.PlacePrefab(e.ActiveLayer, e.SelectedPrefab, px, py); err != nil {
				e.report(err)
			}
		} else if e.SelectedAsset != nil {
			_, _ = e.DropAtlasFrame(e.SelectedAsset.Atlas, e.SelectedAsset.Frame, px, py)
		} else {
			_, _ = e.PlaceObject(e.ActiveLayer, sceneformat.ObjectDoc{X: px, Y: py})
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
	case ToolRectTile:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok {
			_ = e.FillTileRect(e.ActiveLayer, col, row, col, row, e.ActiveTileID)
		}
	case ToolLineTile:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok {
			_ = e.DrawTileLine(e.ActiveLayer, col, row, col, row, e.ActiveTileID)
		}
	case ToolStampTile:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok {
			if stamp, ok := e.effectiveStamp(); ok {
				_ = e.StampTiles(e.ActiveLayer, col, row, stamp)
			}
		}
	case ToolRandomTile:
		if col, row, ok := e.CanvasToTile(e.ActiveLayer, x, y); ok {
			if tid := randomPickTile(e.randomSeed, col, row, e.effectiveRandomPool()); tid > 0 {
				_ = e.PaintTile(e.ActiveLayer, col, row, tid)
			}
		}
	}
}

func (e *Editor) layerCoordsForActive(x, y float32) (float32, float32) {
	if e.layerIndex(e.ActiveLayer) >= 0 || e.inheritedLayers()[e.ActiveLayer] {
		space, px, py := e.layerSpaceAndParallax(e.ActiveLayer)
		return e.canvasToLayerFor(space, px, py, x, y)
	}
	return e.CanvasToWorld(x, y)
}

func (e *Editor) layerCoordsForSelected(x, y float32) (float32, float32) {
	return e.layerCoordsForObject(e.SelectedID, x, y)
}

func (e *Editor) layerCoordsForObject(id string, x, y float32) (float32, float32) {
	if _, layerName := e.FindResolvedObject(id); layerName != "" {
		space, px, py := e.layerSpaceAndParallax(layerName)
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
	if !e.playing && (scene == nil || !scene.HasBackground) {
		e.drawViewportDotGrid(d, size)
	}
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
			e.drawNonSpriteMarkers2D(d, scene)
		}
	}
	if scene == nil {
		return
	}
	e.drawPhysicsDebug2D(d, scene)
	if e.playing {
		return
	}
	if e.HistoryPreviewActive() {
		e.drawCameraFrameOverlay(d)
		return
	}
	e.drawLightingOverlays2D(d, scene)
	e.drawRoomsOverlay(d)
	e.drawNavMesh2DOverlay(d, scene)
	e.drawSnapGridOverlay(d)
	e.drawCameraFrameOverlay(d)
	e.drawTileGridOverlay(d)
	e.drawTileReviewOverlay(d)
	e.drawPendingReviewOverlay(d, scene)
	e.drawTileHoverOverlay(d)
	e.drawParticleEmitterOverlays(d, scene)
	e.drawSelectionOverlay(d, scene)
	e.drawAgentTaskOverlays(d, scene)
	e.drawAgentLiveOverlays(d, scene)
	e.drawSceneOverlays2D(d, scene)
	e.drawComponentShapes2D(d, scene)
	e.drawViewportOverlays(d, scene)
	e.drawPlanGhostsOverlay(d)
	e.drawProblemOverlays2D(d, scene)
	e.drawSketchOverlay(d)
	e.drawGizmo2D(d)
	e.drawComponentShapeHandles2D(d)
	e.drawMarqueeOverlay(d)
	e.drawAssetDragOverlay(d, scene)
	if t := e.activePluginTool(); t != nil && t.Draw != nil {
		// Save and Restore keep a transform or style the tool leaves set from reaching the next frame's drawing.
		d.Save()
		t.Draw(e, d)
		d.Restore()
	}
}

// drawViewportDotGrid draws the Spec 16x16 neutral dot-grid surround matching design/editor/Main.dc.html
// whenever the 2D edit viewport has no explicit scene clear color.
// Uses a single dashed-stroke path (1.2px dash every 16px) so a 1440×900 canvas emits ~50 line
// segments and one Stroke op instead of thousands of individual FillRect ops per frame.
func (e *Editor) drawViewportDotGrid(d *core.Drawing, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	if !(w > 0 && h > 0) || !isFiniteFloat32(w) || !isFiniteFloat32(h) || w > 8192 || h > 8192 {
		return
	}
	spec := e.Palette(nil)
	const (
		step float32 = 16
		dot  float32 = 1.2
	)
	d.Save()
	d.SetStrokeColor(spec.ViewportDot)
	d.SetLineWidth(dot)
	d.SetLineDash(dot, step-dot)
	d.BeginPath()
	for y := float32(8); y < h; y += step {
		d.MoveTo(8, y)
		d.LineTo(w, y)
	}
	d.Stroke()
	d.Restore()
}

// drawNonSpriteMarkers2D renders subtle, neutral Spec-token markers for visible 2D objects
// that have no sprite, skeleton, particle emitter, or 2D light visual so authors can see and
// pick empty nodes, prefabs without sprites, and triggers directly on the canvas.
// Uses strictly neutral Spec tokens so markers never collide with semantic overlay legend colors.
func (e *Editor) drawNonSpriteMarkers2D(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e == nil || d == nil || scene == nil || e.playing {
		return
	}
	spec := e.Palette(nil)
	fill := spec.Raised
	stroke := ui.Alpha(spec.TextFaint, 0.72)
	centerDot := spec.TextMuted
	d.Save()
	d.SetLineDash()
	d.SetLineWidth(1)
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		m := e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY)
		var walk func(objs []*sceneformat.BuiltObject, hidden bool)
		walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
			for _, bo := range objs {
				if bo == nil || bo.Doc == nil {
					continue
				}
				h := hidden || e.hidden[bo.ID]
				if !h && bo.Sprite == nil && bo.Skeleton == nil && bo.Emitter == nil && bo.Doc.Light == nil {
					pos := bo.World.TransformPoint(math2d.Vec2{})
					cp := m.TransformPoint(pos)
					cx, cy := cp.X, cp.Y
					if isFiniteFloat32(cx) && isFiniteFloat32(cy) {
						half := defaultMarkerHalfSize
						d.SetFillColor(fill).BeginPath().RoundRect(cx-half, cy-half, half*2, half*2, 3).Fill()
						d.SetStrokeColor(stroke).BeginPath().RoundRect(cx-half, cy-half, half*2, half*2, 3).Stroke()
						d.SetFillColor(centerDot).FillRect(cx-1.5, cy-1.5, 3, 3)
					}
				}
				walk(bo.Children, h)
			}
		}
		walk(layer.Objects, false)
	}
	d.Restore()
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

// worldRectCanvasCorners returns the 4 canvas-space corners of world rectangle (x, y, w, h).
func worldRectCanvasCorners(e *Editor, x, y, w, h float32) []math2d.Vec2 {
	pts := make([]math2d.Vec2, 0, 4)
	for _, c := range [4][2]float32{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}} {
		cx, cy := e.WorldToCanvas(c[0], c[1])
		pts = append(pts, math2d.Vec2{X: cx, Y: cy})
	}
	return pts
}

// drawWorldRectHandles draws the 8 corner and edge-midpoint resize handles of world rectangle
// (x, y, w, h) at fixed canvas point size.
func drawWorldRectHandles(e *Editor, d *core.Drawing, x, y, w, h float32, active RectHandle, fill, stroke, activeFill core.Color) {
	d.Save()
	d.SetLineDash()
	d.SetLineWidth(1.5)
	for _, handle := range rectResizeHandles {
		wx, wy := rectHandleWorldPos(handle, x, y, w, h)
		cx, cy := e.WorldToCanvas(wx, wy)
		half := rectHandleHalf
		f := fill
		s := stroke
		if handle == active {
			half = rectHandleHalf + 1
			f = activeFill
			s = e.activePalette().TextPrimary
		}
		d.SetFillColor(f).FillRect(cx-half, cy-half, 2*half, 2*half)
		d.SetStrokeColor(s).StrokeRect(cx-half, cy-half, 2*half, 2*half)
	}
	d.Restore()
}

// drawCameraFrameOverlay outlines what the player sees: the scene's virtual screen through
// the scene's camera, and, dashed, the camera's bounds. When scene settings are shown in the
// inspector (no object or room selected), the start view frame and bounds are highlighted,
// interactive resize handles appear on the bounds rectangle, and a draggable handle marks
// the camera's world position.
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
	spec := e.Palette(nil)
	d.Save()
	// Camera frame, camera bounds, and camera handle use neutral Spec tokens in a 1 px hairline/dotted
	// style distinct from all semantic overlay legend kinds (never Accent, Agent, Added, Changed, Removed, Problem).
	if highlight {
		d.SetStrokeColor(ui.Alpha(spec.TextMuted, 0.9)).SetLineWidth(1)
	} else {
		d.SetStrokeColor(ui.Alpha(spec.TextFaint, 0.65)).SetLineWidth(1)
	}
	strokePolygon(d, corners(0, 0, vw, vh, e.ScreenToCanvas))
	if e.Doc != nil && e.Doc.Camera != nil && e.Doc.Camera.Bounds != nil && e.drawOverlayOverride == nil {
		b := e.Doc.Camera.Bounds
		if highlight {
			d.SetStrokeColor(ui.Alpha(spec.TextMuted, 0.85)).SetLineWidth(1).SetLineDash(2, 4)
			strokePolygon(d, corners(b.X, b.Y, b.Width, b.Height, e.WorldToCanvas))
			if e.Tool == ToolSelect || e.Tool == ToolMove {
				drawWorldRectHandles(e, d, b.X, b.Y, b.Width, b.Height, e.cameraBoundsDrag.handle, spec.Raised, spec.TextMuted, spec.TextPrimary)
			}
		}
	}
	if hx, hy, ok := e.CameraHandleCanvasPos(); ok {
		// If bounds clamp the visible frame away from the camera's configured position,
		// draw a fine dotted tether from the handle to the clamped frame center.
		fcx, fcy := e.ScreenToCanvas(vw/2, vh/2)
		if math.Hypot(float64(hx-fcx), float64(hy-fcy)) > 1 {
			d.SetStrokeColor(ui.Alpha(spec.TextFaint, 0.7)).SetLineWidth(1).SetLineDash(2, 4)
			d.BeginPath().MoveTo(hx, hy).LineTo(fcx, fcy).Stroke()
		}
		// Reset dash for the solid neutral crosshair and circular handle.
		d.SetLineDash()
		const arm float32 = 10
		d.SetStrokeColor(spec.TextMuted).SetLineWidth(1)
		d.BeginPath().MoveTo(hx-arm, hy).LineTo(hx+arm, hy).MoveTo(hx, hy-arm).LineTo(hx, hy+arm).Stroke()
		r := float32(5)
		fill := spec.Raised
		stroke := spec.TextMuted
		if e.cameraDrag.active {
			r = 6
			fill = spec.TextPrimary
			stroke = spec.Raised
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
	spec := e.Palette(nil)
	d.Save()
	applyAffine(d, m)
	// The tile grid is a neutral hairline using Spec tokens, distinct from every legend kind.
	d.SetStrokeColor(ui.Alpha(spec.TextFaint, 0.22)).SetLineWidth(1 / strokeScale(m))
	ox, oy := tm.OffsetX, tm.OffsetY
	totalW := float32(tm.Width) * float32(tm.TileWidth)
	totalH := float32(tm.Height) * float32(tm.TileHeight)
	d.BeginPath()
	for c := 0; c <= tm.Width; c++ {
		x := ox + float32(c)*float32(tm.TileWidth)
		d.MoveTo(x, oy).LineTo(x, oy+totalH)
	}
	for r := 0; r <= tm.Height; r++ {
		y := oy + float32(r)*float32(tm.TileHeight)
		d.MoveTo(ox, y).LineTo(ox+totalW, y)
	}
	d.Stroke().Restore()
}

// drawEditScene renders scene into d in edit mode: materials are synced, editor-hidden objects are suppressed, each visible layer draws through the 2D view with its parallax factors and repeat tiling applied, and when ShowLightingPreview is enabled, scene.Lights2D renders after the last visible world-space layer using the visible 2D canvas bounds.
func (e *Editor) drawEditScene(d *core.Drawing, scene *sceneformat.BuiltScene, size core.Size) {
	scene.SyncMaterials()
	type faded struct {
		bo     *sceneformat.BuiltObject
		alpha  float32
		isSkel bool
	}
	type hiddenEmitter struct {
		bo *sceneformat.BuiltObject
		em *particles.Emitter
	}
	var restore []faded
	var restoreEmitters []hiddenEmitter
	if len(e.hidden) > 0 {
		for _, layer := range scene.Layers {
			var walk func(objs []*sceneformat.BuiltObject, hidden bool)
			walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
				for _, bo := range objs {
					h := hidden || e.hidden[bo.ID]
					if h && bo.Sprite != nil {
						restore = append(restore, faded{bo: bo, alpha: bo.Sprite.Alpha})
						bo.Sprite.Alpha = 0
					}
					if h && bo.Emitter != nil {
						restoreEmitters = append(restoreEmitters, hiddenEmitter{bo, bo.Emitter})
						bo.Emitter = nil
					}
					if h && bo.Skeleton != nil {
						restore = append(restore, faded{bo: bo, alpha: bo.Skeleton.Alpha, isSkel: true})
						bo.Skeleton.Alpha = 0
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
		if layer.Tilemap != nil || layer.Map != nil || len(layer.Batches) > 0 || len(layer.Emitters) > 0 || len(layer.Skeletons) > 0 {
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
		if f.isSkel {
			f.bo.Skeleton.Alpha = f.alpha
		} else {
			f.bo.Sprite.Alpha = f.alpha
		}
	}
	for _, he := range restoreEmitters {
		he.bo.Emitter = he.em
	}
}

func drawBuiltLayer(d *core.Drawing, layer *sceneformat.BuiltLayer) {
	layer.Draw(d, nil)
}

func (e *Editor) drawSelectionOverlay(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if !e.OverlayKindVisible(OverlayKindSelection) {
		return
	}
	ids := e.SelectedIDs()
	if len(ids) == 0 {
		return
	}
	spec := e.Palette(nil)
	canvas := core.Size{Width: core.Points(e.canvasW), Height: core.Points(e.canvasH)}
	d.Save()
	drawn := 0
	var minX, maxX, minY, maxY float32
	for _, id := range ids {
		bo := scene.Object(id)
		bounds, corners, ok := e.builtObjectCanvasBounds(scene, bo)
		if !ok {
			continue
		}
		for _, c := range corners {
			if drawn == 0 {
				minX, maxX, minY, maxY = c.X, c.X, c.Y, c.Y
			} else {
				minX, maxX = min(minX, c.X), max(maxX, c.X)
				minY, maxY = min(minY, c.Y), max(maxY, c.Y)
			}
		}
		DrawObjectOverlay(d, spec, ObjectOverlay{
			Kind:       OverlayKindSelection,
			Label:      e.objectDisplayName(bo, id),
			Bounds:     bounds,
			Corners:    corners,
			CanvasSize: canvas,
		})
		drawn++
	}
	if drawn > 1 {
		d.SetStrokeColor(ui.Alpha(spec.Accent.Edge, 0.55)).SetLineWidth(1).SetLineDash(4, 3)
		d.StrokeRect(minX, minY, maxX-minX, maxY-minY)
	}
	d.Restore()
}

// drawAgentTaskOverlays draws numbered violet overlays (editing solid pulsing 1.6 s between 100% and 45%,
// holding at 45% dashed with clock glyph and "waiting on <object>" pill) for active background agent tasks
// on the open scene through DrawObjectOverlay, respecting the Agent working legend toggle and ReduceMotion.
func (e *Editor) drawAgentTaskOverlays(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if !e.ShowAgentChanges() || !e.OverlayKindVisible(OverlayKindAgent) || e.HistoryPreviewActive() {
		return
	}
	ovs := e.SceneTaskOverlays()
	if len(ovs) == 0 || scene == nil {
		return
	}
	spec := e.Palette(nil)
	now := e.overlayNow()
	rm := e.ReduceMotion()
	canvas := core.Size{Width: core.Points(e.canvasW), Height: core.Points(e.canvasH)}

	type objBox struct {
		ov     ObjectTaskOverlay
		bounds core.Bounds
		paired bool
	}
	var boxes []objBox
	for _, ov := range ovs {
		bo := scene.Object(ov.ObjectID)
		bounds, _, ok := e.builtObjectCanvasBounds(scene, bo)
		if !ok {
			continue
		}
		paired := e.IsSelected(ov.ObjectID) && e.OverlayKindVisible(OverlayKindSelection)
		boxes = append(boxes, objBox{ov: ov, bounds: bounds, paired: paired})
	}

	// Pick one anchor box per (taskID, holding) group — preferring the highest object so holding
	// pills sit cleanly above upper targets — and stagger any pills that would otherwise overlap.
	type pillGroupKey struct {
		taskID  string
		holding bool
	}
	var groupOrder []pillGroupKey
	anchorIdxByGroup := make(map[pillGroupKey]int)
	for i, b := range boxes {
		k := pillGroupKey{taskID: b.ov.TaskID, holding: b.ov.Holding}
		prevIdx, exists := anchorIdxByGroup[k]
		if !exists {
			groupOrder = append(groupOrder, k)
			anchorIdxByGroup[k] = i
		} else if b.bounds.Origin.Y < boxes[prevIdx].bounds.Origin.Y {
			anchorIdxByGroup[k] = i
		}
	}
	isAnchor := make([]bool, len(boxes))
	for _, idx := range anchorIdxByGroup {
		isAnchor[idx] = true
	}

	// Draw outline-only on non-anchor target objects (anchors draw outline + pill once below so
	// semi-transparent pulsing/holding outlines are never double-stroked).
	for i, b := range boxes {
		if isAnchor[i] {
			continue
		}
		DrawObjectOverlay(d, spec, ObjectOverlay{
			Kind:                OverlayKindAgent,
			TaskNumber:          b.ov.TaskNumber,
			Holding:             b.ov.Holding,
			Bounds:              b.bounds,
			CanvasSize:          canvas,
			PairedWithSelection: b.paired,
			Elapsed:             now.Sub(e.overlayEpoch),
			HasElapsed:          true,
			ReduceMotion:        rm,
		})
	}

	overlapsAny := func(r core.Bounds, placed []core.Bounds) bool {
		for _, o := range placed {
			if r.Origin.X < o.Origin.X+o.Size.Width+6 &&
				r.Origin.X+r.Size.Width+6 > o.Origin.X &&
				r.Origin.Y < o.Origin.Y+o.Size.Height+4 &&
				r.Origin.Y+r.Size.Height+4 > o.Origin.Y {
				return true
			}
		}
		return false
	}
	var placed []core.Bounds

	for _, k := range groupOrder {
		b := boxes[anchorIdxByGroup[k]]
		stackOff := float32(0)
		if b.paired {
			stackOff = 24
		}
		for range 8 {
			estY := float32(b.bounds.Origin.Y) - OverlayBaseOffset - 22 - 8 - stackOff
			est := core.Bounds{
				Origin: core.Point{X: b.bounds.Origin.X, Y: core.Points(estY)},
				Size:   core.Size{Width: core.Points(max(float32(len([]rune(b.ov.Label)))*6.2+40, 80)), Height: 22},
			}
			if !overlapsAny(est, placed) {
				break
			}
			stackOff += 28
		}
		pill := DrawObjectOverlay(d, spec, ObjectOverlay{
			Kind:                OverlayKindAgent,
			Label:               b.ov.Label,
			TaskNumber:          b.ov.TaskNumber,
			Holding:             b.ov.Holding,
			Bounds:              b.bounds,
			CanvasSize:          canvas,
			PairedWithSelection: b.paired,
			PillStackOffset:     stackOff,
			Elapsed:             now.Sub(e.overlayEpoch),
			HasElapsed:          true,
			ReduceMotion:        rm,
		})
		if pill.Size.Width > 0 && pill.Size.Height > 0 {
			placed = append(placed, pill)
		}
	}
}

// wheelZoomPoints is how far a wheel moves the content, in points, to double or halve the 2D view's zoom: a notch of 40 points zooms by about 15%.
const wheelZoomPoints = 200

// viewportGestures is the 2D viewport's navigation and handle drags in progress:
// a middle-button drag, a Space-drag, a pinch, a two-finger touch pan, and dragging
// the scene camera handle, camera.bounds handles, or room-map handles.
type viewportGestures struct {
	middle        bool
	middleLast    core.Point
	spacePan      bool
	panLast       core.Point
	pinchLast     float32
	touchPan      bool
	touchLast     core.Point
	cameraDrag    bool
	boundsDrag    bool
	roomDrag      bool
	sketchDrag    bool
	touchCamPos   core.Point
	hasTouchCam   bool
	panCamDrag    bool
	panBoundsDrag bool
	panRoomDrag   bool
	suppressPress bool
	snapInvert    func() bool
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

// pan follows a one-finger or primary-button drag in 2D: with Space held, or during a middle-button drag, it pans the view; otherwise, with the Select or Move tool, a touch drag on the camera handle, camera bounds handles, or room handles moves or resizes them, a drag that starts on the selection's gizmo or an object moves, turns or scales it (BeginDrag2D), ending as one undoable step, and a drag starting on empty canvas draws a marquee box selection (BeginMarquee2DWith).
func (g *viewportGestures) pan(ed *Editor, ev core.PanEvent, spaceHeld, shiftHeld, modHeld, altHeld func() bool) {
	at := func() (float32, float32) { return float32(ev.Position.X), float32(ev.Position.Y) }
	marqueeMode := func() MarqueeMode {
		if altHeld() {
			return MarqueeTouch
		}
		return MarqueeEnclose
	}
	invert := func() bool { return g.snapInvert != nil && g.snapInvert() }
	switch ev.Phase {
	case core.GestureBegan:
		g.spacePan = spaceHeld() || ed.Tool == ToolPan
		g.panLast = ev.Translation
		g.panCamDrag = false
		g.panBoundsDrag = false
		g.panRoomDrag = false
		if g.spacePan || g.middle || g.touchPan || g.cameraDrag || g.boundsDrag || g.roomDrag || g.sketchDrag || ed.SketchActive() || ed.PickingConnectionTarget() {
			return
		}
		if g.hasTouchCam {
			sx, sy := float32(g.touchCamPos.X), float32(g.touchCamPos.Y)
			cx, cy := sx+float32(ev.Translation.X), sy+float32(ev.Translation.Y)
			if ed.RoomCreateMode() && ed.BeginRoomDrag(sx, sy) {
				g.panRoomDrag = true
				ed.UpdateRoomDrag(cx, cy)
				return
			}
			if ed.SelectedRoomID() != "" && ed.BeginRoomDrag(sx, sy) {
				g.panRoomDrag = true
				ed.UpdateRoomDrag(cx, cy)
				return
			}
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
		if ed.SelectionCount() <= 1 {
			if _, ok := ed.BeginShapeDrag2D(downX, downY); ok {
				x, y := at()
				ed.withSnapInvert(invert(), func() { ed.UpdateShapeDrag2D(x, y) })
				return
			}
		}
		if ed.BeginDrag2D(downX, downY) != Handle2DNone {
			x, y := at()
			ed.withSnapInvert(invert(), func() { ed.UpdateDrag2D(x, y, shiftHeld()) })
			return
		}
		if g.hasTouchCam {
			sx, sy := float32(g.touchCamPos.X), float32(g.touchCamPos.Y)
			cx, cy := sx+float32(ev.Translation.X), sy+float32(ev.Translation.Y)
			if ed.BeginCameraDrag(sx, sy) {
				g.panCamDrag = true
				ed.UpdateCameraDrag(cx, cy)
				return
			}
			if ed.BeginCameraBoundsDrag(sx, sy) != RectHandleNone {
				g.panBoundsDrag = true
				ed.UpdateCameraBoundsDrag(cx, cy)
				return
			}
			if ed.SelectedID == "" && ed.BeginRoomDrag(sx, sy) {
				g.panRoomDrag = true
				ed.UpdateRoomDrag(cx, cy)
				return
			}
		}
		if ed.BeginMarquee2DWith(downX, downY, shiftHeld(), modHeld(), marqueeMode()) {
			x, y := at()
			ed.UpdateMarquee2D(x, y, marqueeMode())
			return
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
		} else if g.panBoundsDrag {
			if !ed.cameraBoundsDrag.active {
				g.panBoundsDrag = false
			} else {
				ed.UpdateCameraBoundsDrag(
					float32(g.touchCamPos.X)+float32(ev.Translation.X),
					float32(g.touchCamPos.Y)+float32(ev.Translation.Y),
				)
			}
		} else if g.panRoomDrag {
			if !ed.roomDrag.active {
				g.panRoomDrag = false
			} else {
				ed.UpdateRoomDrag(
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
			ed.withSnapInvert(invert(), func() { ed.UpdateShapeDrag2D(x, y) })
		} else if ed.Dragging2D() != Handle2DNone {
			x, y := at()
			ed.withSnapInvert(invert(), func() { ed.UpdateDrag2D(x, y, shiftHeld()) })
		} else if ed.MarqueeActive() {
			x, y := at()
			ed.UpdateMarquee2D(x, y, marqueeMode())
		}
		g.panLast = ev.Translation
	case core.GestureEnded:
		if g.panCamDrag {
			g.panCamDrag = false
			if ed.EndCameraDrag() {
				g.suppressPress = true
			}
		} else if g.panBoundsDrag {
			g.panBoundsDrag = false
			if ed.EndCameraBoundsDrag() {
				g.suppressPress = true
			}
		} else if g.panRoomDrag {
			g.panRoomDrag = false
			wasCreating := ed.roomDrag.creating
			if ed.EndRoomDrag() || wasCreating {
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
			ed.withSnapInvert(invert(), func() { ed.UpdateShapeDrag2D(x, y) })
			if ed.EndShapeDrag2D() {
				g.suppressPress = true
			}
		} else if !g.spacePan && ed.Dragging2D() != Handle2DNone {
			x, y := at()
			ed.withSnapInvert(invert(), func() { ed.UpdateDrag2D(x, y, shiftHeld()) })
			ed.EndDrag2D()
		} else if !g.spacePan && ed.MarqueeActive() {
			x, y := at()
			ed.UpdateMarquee2D(x, y, marqueeMode())
			ed.EndMarquee2D()
			g.suppressPress = true
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
		if g.panBoundsDrag {
			g.panBoundsDrag = false
			ed.CancelCameraBoundsDrag()
		}
		if g.panRoomDrag {
			g.panRoomDrag = false
			ed.CancelRoomDrag()
		}
		ed.CancelDrag2D()
		ed.CancelMarquee2D()
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

// touch pans the 2D view by the movement of the center of two fingers, and puts back any drag in progress when a second finger comes down or the touches are cancelled.
func (g *viewportGestures) touch(ed *Editor, ev core.TouchEvent) {
	cancelAll := func() {
		ed.CancelPluginDrag2D()
		ed.CancelShapeDrag2D()
		ed.CancelDrag2D()
		ed.CancelMarquee2D()
		ed.CancelCameraDrag()
		ed.CancelCameraBoundsDrag()
		ed.CancelRoomDrag()
		ed.CancelSketchDrag()
		g.panCamDrag = false
		g.cameraDrag = false
		g.panBoundsDrag = false
		g.boundsDrag = false
		g.panRoomDrag = false
		g.roomDrag = false
		g.sketchDrag = false
	}
	if ev.Phase == core.TouchCancel {
		// The system took the touches: a drag of an object or handle they made is put back, whether or not the pan reports its own cancel. nsgotest's Finger.Cancel (nsgo v0.3.0 nsgotest/pointer.go:259) reports only the touch cancel and no GestureCancelled to an active OnPan, so the touch cancel is the one this drag can rely on in tests.
		cancelAll()
	}
	if len(ev.Touches) != 2 || ev.Phase == core.TouchEnd || ev.Phase == core.TouchCancel {
		g.touchPan = false
		return
	}
	// A second finger turns a one-finger drag into navigation, so the drag is put back.
	cancelAll()
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
	tileAnimRev := core.NewSignal(cx, ed.TileAnimRevision())
	unsubTileAnim := ed.SubscribeTileAnim(func() {
		tileAnimRev.Set(ed.TileAnimRevision())
	})
	cx.Cleanup(unsubTileAnim)
	agentPulseRev := core.NewSignal(cx, ed.AgentPulseRevision())
	unsubAgentPulse := ed.SubscribeAgentPulse(func() {
		agentPulseRev.Set(ed.AgentPulseRevision())
	})
	cx.Cleanup(unsubAgentPulse)

	// Animate active Agent working pulses (1.6 s) and change overlay fade-ins (240 ms).
	// Disabled completely when ReduceMotion is enabled so nothing fades or pulses.
	// Uses an explicit running flag so a subsequent change fade re-arms the timer cleanly
	// after an earlier 240 ms fade has completed.
	overlayAnimRev := core.NewSignal(cx, uint64(0))
	{
		var stop func() bool
		running := false
		disposed := false
		stopTimer := func() {
			if stop != nil {
				stop()
				stop = nil
			}
			running = false
		}
		var tick func()
		tick = func() {
			if disposed {
				return
			}
			overlayAnimRev.Set(overlayAnimRev.Get() + 1)
			if !cx.Env().ReduceMotion() && ed.hasAnimatedOverlays() {
				stop = cx.AfterFunc(33*time.Millisecond, tick)
			} else {
				stop = nil
				running = false
			}
		}
		cx.Effect(func() {
			_ = rev.Get()
			if cx.Env().ReduceMotion() || !ed.hasAnimatedOverlays() {
				stopTimer()
				return
			}
			if !running {
				running = true
				stop = cx.AfterFunc(33*time.Millisecond, tick)
			}
		})
		cx.Cleanup(func() {
			disposed = true
			stopTimer()
		})
	}
	var g viewportGestures
	var g3 viewport3DGestures
	// frame is the view the 3D viewport's flight locks the pointer to.
	var frame core.Ref

	// The first read of the held keys asks the platform to report every key, so Space is known to be held when a drag begins.
	spaceHeld := func() bool { return core.Untracked(cx, func() bool { return core.KeyHeld(cx, "space") }) }
	spaceHeld()
	// A pan event carries no modifiers, so Shift (aspect ratio / 3D pan / multi-select), Mod/Ctrl (toggle / invert snapping), and Alt (tile pick / marquee touch) are read from the held keys.
	shiftHeld := func() bool { return core.Untracked(cx, func() bool { return core.KeyHeld(cx, "shift") }) }
	modHeld := func() bool { return core.Untracked(cx, func() bool { return core.KeyHeld(cx, "mod") }) }
	altHeld := func() bool { return core.Untracked(cx, func() bool { return core.KeyHeld(cx, "alt") }) }
	snapInvertHeld := func() bool {
		return core.Untracked(cx, func() bool { return core.KeyHeld(cx, "ctrl") || core.KeyHeld(cx, "mod") })
	}
	snapInvertHeld()
	g.snapInvert = snapInvertHeld
	g3.snapInvert = snapInvertHeld
	in2D := func() bool { return ed.ViewMode == ViewMode2D && !ed.Playing() }
	in3DGridMap := func() bool { return ed.ViewMode == ViewMode3D && ed.GridMapTool() != GridMapToolNone && !ed.Playing() }
	// strokeTookPress says the tile or GridMap stroke that a pointer drew has ended, so the press its button or finger completes, which may come after it, edits nothing more. Every pointer-down clears it, so a stroke that no press followed, such as a drag the frame's pan took or a right-button drag, cannot swallow the next click.
	var strokeTookPress bool
	// lastPointer and hasPointer track the most recent pointer coordinate inside the viewport frame so secondary-click and Control-click context menus hit-test at the exact pointer location, falling back to viewport center when dispatched without pointer motion (e.g. nsgotest RightClick).
	var lastPointer core.Point
	var lastPointerMode ViewMode
	var hasPointer bool
	var rightConsumed bool
	isFiniteCoord := func(x, y float32) bool {
		return !math.IsNaN(float64(x)) && !math.IsInf(float64(x), 0) &&
			!math.IsNaN(float64(y)) && !math.IsInf(float64(y), 0)
	}
	hitViewportObject := func(x, y float32) string {
		if ed.Playing() || !isFiniteCoord(x, y) {
			return ""
		}
		if ed.ViewMode == ViewMode3D {
			cw, ch := ed.CanvasSize()
			id, _ := ed.Pick3DObject(x, y, cw, ch)
			return id
		}
		return ed.HitTest(x, y)
	}
	openViewportContextMenu := func(x, y float32) bool {
		if ed.contextMenuBlocked() || ed.PickingConnectionTarget() || !isFiniteCoord(x, y) {
			return false
		}
		hit := hitViewportObject(x, y)
		if hit == "" {
			return false
		}
		if !ed.IsObjectSelected(hit) {
			ed.SelectObject(hit)
		}
		return ed.OpenObjectContextMenu(hit)
	}
	viewportMenuKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if !ed.ObjectContextMenuOpen() {
			return ""
		}
		return ed.objectContextMenuDigest(ed.ObjectContextMenuTarget(), cx.Dark())
	})
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
			if g3.suppressPress {
				g3.suppressPress = false
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
			if ed.SketchActive() {
				if ed.SketchTool() == SketchToolText {
					if hit := ed.HitTestSketchMark(x, y); hit > 0 {
						ed.SelectSketchMark(hit)
					} else {
						wx, wy := ed.CanvasToWorld(x, y)
						ed.AddSketchText(wx, wy, "")
					}
				} else if hit := ed.HitTestSketchMark(x, y); hit > 0 {
					ed.SelectSketchMark(hit)
				}
				return
			}
			if ev.Modifiers.Alt && ed.ViewMode == ViewMode2D && !ed.Playing() && !ed.PickingConnectionTarget() {
				if ed.handleShapePress2D(x, y, true) {
					return
				}
			}
			// A press no pointer drew a stroke for, such as an accessibility action, makes the stroke of one cell unless picking a connection target.
			if !ed.PickingConnectionTarget() && !ed.HistoryPreviewActive() {
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
			shift := ev.Modifiers.Shift || shiftHeld()
			mod := ev.Modifiers.Platform || ev.Modifiers.Control || modHeld()
			alt := ev.Modifiers.Alt || altHeld()
			ed.withSnapInvert(ev.Modifiers.Control || ev.Modifiers.Platform || snapInvertHeld(), func() {
				ed.HandleCanvasPressWithModifiers(x, y, shift, mod, alt)
			})
		},
		Draw: func(d *core.Drawing, size core.Size) {
			_ = rev.Get()
			_ = tileAnimRev.Get()
			_ = agentPulseRev.Get()
			_ = overlayAnimRev.Get()
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

	spec := ed.Palette(cx)
	if ed.focus.ctrl != nil {
		ed.focus.ctrl.registerCandidate(cx, RegionViewport, &frame, func() bool {
			return !ed.Playing()
		})
	}
	return core.View{
		TestID: "editor-viewport-container",
		OnKeyDown: func(ev core.KeyEvent) bool {
			return ed.TileReviewHandleKeyEvent(ev, true)
		},
		OnKeyUp: func(ev core.KeyEvent) {
			ed.TileReviewHandleKeyEvent(ev, false)
		},
		Style: core.Style{
			FlexGrow:   1,
			Position:   core.Relative,
			Background: spec.Viewport,
			Overflow:   core.Hidden,
		},
		Children: append([]core.Node{
			// Bind Escape while a viewport drag, marquee, tile/GridMap/terrain stroke, visible tile selection,
			// or object selection is active (and Command Palette / connection pick are inactive), using
			// KeyNotEditing so text inputs (such as Scene Tree rename or Inspector fields) keep Escape.
			core.Show{
				When: func() bool {
					_ = rev.Get()
					if ed.CommandPaletteOpen() || ed.PickingConnectionTarget() || ed.AskInPlaceOpen() || ed.HistoryPreviewActive() || ed.SettingsOpen() || ed.ReviewOpen() || ed.DataTableReviewOpen() || ed.CodeReviewOpen() {
						return false
					}
					if ed.AgentPointingActive() {
						return true
					}
					_, gmActive := ed.GridMapStrokeActive()
					_, strokeActive := ed.TileStrokeActive()
					selVisible := ed.ViewMode == ViewMode2D && isTileTool(ed.Tool) && ed.hasTileSel
					navPreview := ed.Tool == ToolNavTestPath && (ed.nav2D.preview.HasStart || ed.nav2D.preview.HasEnd)
					activeOp := ed.Dragging2D() != Handle2DNone || ed.MarqueeActive() || ed.AssetDragActive() || ed.DraggingGizmo3D() || ed.DraggingHandle3D() || ed.DraggingComponentGizmo3D() || ed.PluginDragging3D() || ed.DraggingShape2D() || ed.PluginDragging2D() || ed.cameraDrag.active || ed.cameraBoundsDrag.active || ed.roomDrag.active || ed.SketchDragging() || gmActive || (ed.terrain != nil && ed.terrain.stroke != nil) || strokeActive || selVisible || navPreview
					return activeOp || ed.SketchActive() || (!ed.Playing() && ed.SelectionCount() > 0)
				},
				Then: func(cx *core.Scope) core.Node {
					cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
						if ed.CancelAgentPointing() {
							return
						}
						if ed.SketchDragging() {
							ed.CancelSketchDrag()
							g.sketchDrag = false
							return
						}
						if ed.SketchActive() {
							ed.FinishSketch()
							return
						}
						_, gmActive := ed.GridMapStrokeActive()
						_, strokeActive := ed.TileStrokeActive()
						selVisible := ed.ViewMode == ViewMode2D && isTileTool(ed.Tool) && ed.hasTileSel
						navPreview := ed.Tool == ToolNavTestPath && (ed.nav2D.preview.HasStart || ed.nav2D.preview.HasEnd)
						activeOp := ed.Dragging2D() != Handle2DNone || ed.MarqueeActive() || ed.AssetDragActive() || ed.DraggingGizmo3D() || ed.DraggingHandle3D() || ed.DraggingComponentGizmo3D() || ed.PluginDragging3D() || ed.DraggingShape2D() || ed.PluginDragging2D() || ed.cameraDrag.active || ed.cameraBoundsDrag.active || ed.roomDrag.active || gmActive || (ed.terrain != nil && ed.terrain.stroke != nil) || strokeActive || selVisible
						if activeOp {
							ed.CancelPluginDrag2D()
							ed.CancelShapeDrag2D()
							ed.CancelAssetDrag()
							ed.CancelPluginDrag3D()
							ed.CancelComponentGizmoDrag3D()
							if strokeActive {
								ed.CancelTileStroke()
								strokeTookPress = true
							} else if selVisible {
								ed.ClearTileSelection()
							}
							ed.CancelDrag2D()
							ed.CancelMarquee2D()
							ed.CancelGizmoDrag3D()
							ed.CancelCameraDrag()
							ed.CancelGridMapStroke()
							ed.CancelTerrainStroke()
							ed.CancelCameraBoundsDrag()
							ed.CancelRoomDrag()
							g.cameraDrag = false
							g.panCamDrag = false
							g.boundsDrag = false
							g.panBoundsDrag = false
							g.roomDrag = false
							g.panRoomDrag = false
							return
						}
						if navPreview {
							ed.ClearNavPathPreview2D()
							return
						}
						if !ed.Playing() && ed.SelectionCount() > 0 {
							ed.ClearSelection()
						}
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
			ui.ContextMenu{
				TestID: "object-context-menu",
				Style:  core.Style{FlexGrow: 1},
				Open: func() bool {
					_ = rev.Get()
					return ed.ObjectContextMenuOpen()
				},
				OnOpenChange: func(open bool, reason base.Reason) {
					if !open {
						ed.CloseObjectContextMenu()
						return
					}
					if ed.Playing() || ed.PickingConnectionTarget() {
						return
					}
					if rightConsumed {
						rightConsumed = false
						return
					}
					if ed.ViewMode == ViewMode3D {
						if ed.fly3DMoved || g3.rightDollyMoved {
							g3.rightDollyMoved = false
							return
						}
						g3.endFly(ed)
						g3.endRightDolly(ed)
					}
					var hx, hy float32
					if hasPointer && lastPointerMode == ed.ViewMode {
						hx, hy = float32(lastPointer.X), float32(lastPointer.Y)
					} else {
						cw, ch := ed.CanvasSize()
						hx, hy = cw/2, ch/2
					}
					hasPointer = false
					openViewportContextMenu(hx, hy)
				},
				Trigger: modelDropTarget(cx, ed, core.View{
					TestID: "editor-viewport-frame",
					Ref:    &frame,
					// In the viewport, focus is the selection: no focus ring on the canvas
					// itself (the RegionViewport wrapper draws the inset -2 px ring when
					// entered via F6), and Tab / Shift-Tab select the next and previous object.
					Focusable: func() bool { return !ed.Playing() },
					OnKeyDown: func(ev core.KeyEvent) bool {
						if ev.Key != "tab" || ev.Modifiers.Platform || ev.Modifiers.Control || ev.Modifiers.Alt || ed.Playing() || ed.shortcutCaptureActive || ed.ObjectContextMenuOpen() {
							return false
						}
						if ev.Modifiers.Shift {
							return ed.SelectPreviousObject() != ""
						}
						return ed.SelectNextObject() != ""
					},
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
						if !isFiniteCoord(float32(ev.Position.X), float32(ev.Position.Y)) {
							return
						}
						lastPointer, lastPointerMode, hasPointer = ev.Position, ed.ViewMode, true
						rightConsumed = false
						if ev.Button == core.MouseButtonLeft && ev.Modifiers.Control && !spaceHeld() && !ed.PickingConnectionTarget() {
							hasPointer = false
							if openViewportContextMenu(float32(ev.Position.X), float32(ev.Position.Y)) {
								g.suppressPress = true
								g3.suppressPress = true
								return
							}
						}
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
							if ed.HistoryPreviewActive() {
								return
							}
							px, py := float32(ev.Position.X), float32(ev.Position.Y)
							if !spaceHeld() && !ed.PickingConnectionTarget() && !ed.AgentPointingActive() {
								if ed.SketchActive() && ed.BeginSketchDrag(px, py) {
									g.sketchDrag = true
									g.suppressPress = true
									return
								}
								if ed.RoomCreateMode() && ed.BeginRoomDrag(px, py) {
									g.roomDrag = true
									return
								}
								if ed.BeginCameraDrag(px, py) {
									g.cameraDrag = true
									return
								}
								if ed.BeginCameraBoundsDrag(px, py) != RectHandleNone {
									g.boundsDrag = true
									return
								}
								if ed.SelectedRoomID() != "" && ed.BeginRoomDrag(px, py) {
									g.roomDrag = true
									return
								}
								if ed.SelectedID == "" && ed.HitTest(px, py) == "" && ed.BeginRoomDrag(px, py) {
									g.roomDrag = true
									return
								}
							}
							beginStroke(ev.Position, false, ev.Modifiers.Alt)
						case core.MouseButtonRight:
							if ed.HistoryPreviewActive() {
								return
							}
							if !spaceHeld() && !ed.PickingConnectionTarget() && ed.HandleCanvasSecondaryPress(float32(ev.Position.X), float32(ev.Position.Y)) {
								rightConsumed = true
								return
							}
							if in2D() && !spaceHeld() && !ed.PickingConnectionTarget() {
								if _, ok := ed.toolStrokeMode(true, ev.Modifiers.Alt || altHeld()); ok {
									rightConsumed = true
								}
							}
							beginStroke(ev.Position, true, ev.Modifiers.Alt)
						}
					},
					OnMouseMove: func(ev core.MouseEvent) {
						lastPointer, lastPointerMode, hasPointer = ev.Position, ed.ViewMode, true
						if g.sketchDrag {
							if !ed.SketchDragging() {
								g.sketchDrag = false
							} else if ev.PressedButton != core.MouseButtonLeft || ed.ViewMode != ViewMode2D {
								g.sketchDrag = false
								ed.EndSketchDrag()
								g.suppressPress = true
								return
							} else {
								ed.SetViewportPointer2D(float32(ev.Position.X), float32(ev.Position.Y))
								ed.UpdateSketchDrag(float32(ev.Position.X), float32(ev.Position.Y))
								return
							}
						}
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
						if g.boundsDrag {
							if !ed.cameraBoundsDrag.active {
								g.boundsDrag = false
							} else if ev.PressedButton != core.MouseButtonLeft || ed.ViewMode != ViewMode2D {
								g.boundsDrag = false
								if ed.EndCameraBoundsDrag() {
									g.suppressPress = true
								}
								return
							} else {
								ed.UpdateCameraBoundsDrag(float32(ev.Position.X), float32(ev.Position.Y))
								return
							}
						}
						if g.roomDrag {
							if !ed.roomDrag.active {
								g.roomDrag = false
							} else if ev.PressedButton != core.MouseButtonLeft || ed.ViewMode != ViewMode2D {
								g.roomDrag = false
								wasCreating := ed.roomDrag.creating
								if ed.EndRoomDrag() || wasCreating {
									g.suppressPress = true
								}
								return
							} else {
								ed.UpdateRoomDrag(float32(ev.Position.X), float32(ev.Position.Y))
								return
							}
						}
						if ed.ViewMode == ViewMode3D {
							cw, ch := ed.CanvasSize()
							x, y := float32(ev.Position.X), float32(ev.Position.Y)
							ed.SetViewportPointer3D(x, y)
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
							if ed.TerrainOpen() {
								ed.SetTerrainHoverAt(x, y, cw, ch)
							}
							ed.SetPluginHover3D(x, y, cw, ch)
							g3.mouseMove(ed, ev)
							return
						}
						if ed.ViewMode == ViewMode2D {
							ed.SetViewportPointer2D(float32(ev.Position.X), float32(ev.Position.Y))
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
							if g.sketchDrag {
								g.sketchDrag = false
								ed.EndSketchDrag()
								g.suppressPress = true
								return
							}
							if g.cameraDrag {
								g.cameraDrag = false
								if ed.EndCameraDrag() {
									g.suppressPress = true
								}
								return
							}
							if g.boundsDrag {
								g.boundsDrag = false
								if ed.EndCameraBoundsDrag() {
									g.suppressPress = true
								}
								return
							}
							if g.roomDrag {
								g.roomDrag = false
								wasCreating := ed.roomDrag.creating
								if ed.EndRoomDrag() || wasCreating {
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
						hasPointer = false
						ed.ClearViewportPointer()
						ed.ClearTileHover()
						ed.ClearGridMapHover()
						ed.ClearPluginHover2D()
						ed.ClearPluginHover3D()
						ed.ClearTerrainHover()
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
							g.pan(ed, ev, spaceHeld, shiftHeld, modHeld, altHeld)
							return
						}
						if _, active := ed.GridMapStrokeActive(); active {
							return
						}
						if in3DGridMap() && !spaceHeld() {
							return
						}
						g3.pan(ed, ev, shiftHeld, altHeld)
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
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						if viewportMenuKey.Get() == "" {
							return core.Group{}
						}
						id := ed.ObjectContextMenuTarget()
						if id == "" {
							return core.Group{}
						}
						return core.Group(objectContextMenuItems(cx, ed, rev, id, objectContextMenuOpts{}))
					}),
				},
			},
			assetDragViewportOverlay(cx, ed, rev),
			viewport3DRenderStatsOverlay(cx, ed, rev),
			viewportCameraCaptionView(cx, ed, rev),
			viewportOverlaysBarView(cx, ed, rev),
			viewportOverlayLegendView(cx, ed, rev),
			planViewportLegendView(cx, ed, rev),
			playViewportOverlaysView(cx, ed, rev),
			AskInPlaceComposerView(cx, ed, rev),
			viewportTaskLegendOverlay(cx, ed, rev),
			viewportHistoryPreviewBannerView(cx, ed, rev),
			PlayTaskDotView(cx, ed, rev),
			viewportSketchOverlayView(cx, ed, rev),
			viewportAgentPointingOverlay(cx, ed, rev),
			viewportWorkspaceStateOverlay(cx, ed, rev),
		}, append(viewportTileOverlays(cx, ed, rev), ToastView(cx, ed, rev))...),
	}
}

// viewport3DErrorView shows, in a corner of the 3D viewport, why it cannot show the scene's geometry (Viewport3DError), and nothing when it can.
func viewport3DErrorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
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
			p := ed.Palette(cx)
			return core.Text{
				TestID:      "editor-viewport-3d-error",
				ContentFunc: message,
				Style: core.Style{
					Position:     core.Absolute,
					Left:         core.Pt(16),
					Top:          core.Pt(96),
					Right:        core.Pt(116),
					Padding:      core.Symmetric(4, 8),
					CornerRadius: 6,
					FontSize:     12,
					Color:        p.Problem().Fg,
					Background:   p.PillBg,
					BorderWidth:  1,
					BorderColor:  p.Problem().Edge,
				},
			}
		},
	}
}
