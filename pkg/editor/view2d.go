package editor

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// View2D is the 2D viewport's own view of the world: the editor's camera, which the author pans and zooms to reach any part of a level. It is separate from the scene's camera (SceneDoc.Camera), which says what the player sees, and is never saved to the scene nor recorded in History.
type View2D struct {
	// X and Y are the world point shown at the center of the viewport's canvas.
	X, Y float32
	// Zoom is how many canvas points one world unit spans, between MinView2DZoom and MaxView2DZoom.
	Zoom float32
	// Rotation turns the view in radians, as CameraDoc.Rotation turns the scene's camera: the world turns by -Rotation on the canvas. Panning and zooming keep it; only the scene camera's view (ResetView2D) sets it.
	Rotation float32
}

// The range View2D.Zoom is clamped to.
const (
	MinView2DZoom float32 = 1.0 / 64
	MaxView2DZoom float32 = 64
)

// frameMargin is the space, in canvas points, FrameSelection2D and FrameAll2D leave between what they frame and each edge of the canvas.
const frameMargin float32 = 32

// maxFrameZoom caps the zoom FrameSelection2D and FrameAll2D pick, so framing a small marker object does not fill the canvas with it.
const maxFrameZoom float32 = 4

// CanvasSize returns the size in points of the viewport's canvas, which fills its panel. The viewport sets it as it lays out (SetCanvasSize); until then it is the scene's virtual size (ViewportSize).
func (e *Editor) CanvasSize() (float32, float32) {
	if e != nil && e.canvasW > 0 && e.canvasH > 0 {
		return e.canvasW, e.canvasH
	}
	return e.ViewportSize()
}

// SetCanvasSize records the size in points of the viewport's canvas, which CanvasToWorld and the 3D viewport map presses through. The viewport calls it as it lays out; a size that is not positive and finite is ignored.
func (e *Editor) SetCanvasSize(w, h float32) {
	if e == nil || !(w > 0 && h > 0) || !isFiniteFloat32(w) || !isFiniteFloat32(h) {
		return
	}
	e.canvasW, e.canvasH = w, h
}

// View2D returns the 2D viewport's view. Until the author pans or zooms (or after ResetView2D) it is the scene camera's view: the scene's virtual screen centered on the canvas at the camera's zoom (clamped to [MinView2DZoom, MaxView2DZoom]) and rotation, following the camera as it is edited. While the scene plays it is the scene camera's view too, so Play shows the player's frame as the game does; the author's own view comes back on Stop.
func (e *Editor) View2D() View2D {
	if e == nil {
		return View2D{Zoom: 1}
	}
	if e.cameraDrag.active && !e.playing {
		return e.cameraDrag.startView
	}
	if e.cameraBoundsDrag.active && !e.playing {
		return e.cameraBoundsDrag.startView
	}
	if e.view2DFree && !e.playing {
		return e.view2D
	}
	return e.cameraView2D()
}

// View2DFollowsCamera reports whether the 2D view is the scene camera's view, which it is until the author pans or zooms, again after ResetView2D, and always while the scene plays.
func (e *Editor) View2DFollowsCamera() bool {
	return e != nil && (!e.view2DFree || e.playing)
}

// SetView2D sets the 2D viewport's view, its zoom clamped to [MinView2DZoom, MaxView2DZoom]. It reports false, and changes nothing, for a non-finite value, a zoom that is not positive or while the scene plays, when the view is the scene camera's.
func (e *Editor) SetView2D(v View2D) bool {
	if e == nil || e.playing || !isFiniteFloat32(v.X) || !isFiniteFloat32(v.Y) || !isFiniteFloat32(v.Rotation) || !isFiniteFloat32(v.Zoom) || v.Zoom <= 0 {
		return false
	}
	v.Zoom = clampView2DZoom(v.Zoom)
	e.view2D = v
	e.view2DFree = true
	e.notify()
	return true
}

// ResetView2D returns the 2D view to the scene camera's view, which it then follows as the camera is edited. It reports false when the view already follows the camera or the scene plays.
func (e *Editor) ResetView2D() bool {
	if e == nil || e.playing || !e.view2DFree {
		return false
	}
	e.view2DFree = false
	e.notify()
	return true
}

// PanView2D moves what the 2D viewport shows by (dx, dy) canvas points, as a drag that grabs the world does: a positive dx brings into view what was to the left. It reports false for a zero or non-finite move and while the scene plays.
func (e *Editor) PanView2D(dx, dy float32) bool {
	if e == nil || e.playing || !isFiniteFloat32(dx) || !isFiniteFloat32(dy) || (dx == 0 && dy == 0) {
		return false
	}
	v := e.View2D()
	d := math2d.Rotate(v.Rotation).TransformVector(math2d.Vec2{X: dx / v.Zoom, Y: dy / v.Zoom})
	v.X -= d.X
	v.Y -= d.Y
	return e.SetView2D(v)
}

// ZoomView2DAt multiplies the 2D view's zoom by factor about the canvas point (x, y), which keeps showing the same world point, as a wheel or pinch zooms about the pointer. The zoom stays within [MinView2DZoom, MaxView2DZoom]; it reports false when factor is not positive and finite, the zoom is already at the limit it moves toward or the scene plays.
func (e *Editor) ZoomView2DAt(x, y, factor float32) bool {
	if e == nil || e.playing || !isFiniteFloat32(x) || !isFiniteFloat32(y) || !isFiniteFloat32(factor) || factor <= 0 {
		return false
	}
	v := e.View2D()
	zoom := clampView2DZoom(v.Zoom * factor)
	if zoom == v.Zoom {
		return false
	}
	wx, wy := e.CanvasToWorld(x, y)
	cw, ch := e.CanvasSize()
	d := math2d.Rotate(v.Rotation).TransformVector(math2d.Vec2{X: (x - cw/2) / zoom, Y: (y - ch/2) / zoom})
	v.X, v.Y, v.Zoom = wx-d.X, wy-d.Y, zoom
	return e.SetView2D(v)
}

// FrameSelection2D pans and zooms the 2D view to show the selected object(s) whole, centered with a margin, keeping the view's rotation. It frames everything (FrameAll2D) when nothing is selected, and reports false when there is nothing to frame or the scene plays.
func (e *Editor) FrameSelection2D() bool {
	if e == nil || e.playing {
		return false
	}
	scene := e.ActiveScene()
	ids := e.SelectedIDs()
	if scene == nil || len(ids) == 0 {
		return e.FrameAll2D()
	}
	var pts []math2d.Vec2
	for _, id := range ids {
		bo := scene.Object(id)
		if bo == nil {
			continue
		}
		space := sceneformat.SpaceWorld
		if layer := scene.Layer(bo.Layer); layer != nil {
			space = layer.Space
		}
		pts = e.objectWorldCorners(bo, space, pts)
	}
	if len(pts) == 0 {
		return e.FrameAll2D()
	}
	return e.frameWorldPoints(pts)
}

// FrameAll2D pans and zooms the 2D view to show every object of the visible layers, their inline tilemaps and the scene camera's frame, centered with a margin, keeping the view's rotation. It reports false while the scene plays.
func (e *Editor) FrameAll2D() bool {
	if e == nil || e.playing {
		return false
	}
	pts := e.screenFrameWorldCorners(nil)
	if scene := e.ActiveScene(); scene != nil {
		for _, layer := range scene.Layers {
			if !layer.Visible {
				continue
			}
			for _, bo := range scene.Objects {
				if bo.Layer == layer.Name {
					pts = e.objectWorldCorners(bo, layer.Space, pts)
				}
			}
			if _, tm, err := e.resolveInlineTilemap(layer.Name); err == nil && tm != nil {
				x0, y0 := tm.OffsetX, tm.OffsetY
				w, h := float32(tm.Width)*float32(tm.TileWidth), float32(tm.Height)*float32(tm.TileHeight)
				for _, c := range [4][2]float32{{x0, y0}, {x0 + w, y0}, {x0 + w, y0 + h}, {x0, y0 + h}} {
					x, y := e.layerToWorld(layer.Space, c[0], c[1])
					pts = append(pts, math2d.Vec2{X: x, Y: y})
				}
			}
		}
	}
	return e.frameWorldPoints(pts)
}

// frameWorldPoints fits the world points pts in the canvas, as FrameSelection2D says.
func (e *Editor) frameWorldPoints(pts []math2d.Vec2) bool {
	if len(pts) == 0 {
		return false
	}
	v := e.View2D()
	// The bounds are taken in the view's own turned axes, so a rotated view frames tightly.
	toView := math2d.Rotate(-v.Rotation)
	first := toView.TransformVector(pts[0])
	minX, maxX, minY, maxY := first.X, first.X, first.Y, first.Y
	for _, p := range pts[1:] {
		q := toView.TransformVector(p)
		minX, maxX = min(minX, q.X), max(maxX, q.X)
		minY, maxY = min(minY, q.Y), max(maxY, q.Y)
	}
	if !isFiniteFloat32(minX) || !isFiniteFloat32(maxX) || !isFiniteFloat32(minY) || !isFiniteFloat32(maxY) {
		return false
	}
	cw, ch := e.CanvasSize()
	aw, ah := cw-2*frameMargin, ch-2*frameMargin
	if aw <= 0 || ah <= 0 {
		aw, ah = cw, ch
	}
	zoom := maxFrameZoom
	if w := maxX - minX; w > 0 {
		zoom = min(zoom, aw/w)
	}
	if h := maxY - minY; h > 0 {
		zoom = min(zoom, ah/h)
	}
	center := math2d.Rotate(v.Rotation).TransformVector(math2d.Vec2{X: (minX + maxX) / 2, Y: (minY + maxY) / 2})
	return e.SetView2D(View2D{X: center.X, Y: center.Y, Zoom: zoom, Rotation: v.Rotation})
}

// objectWorldCorners appends the world-space corners of bo's outline, which HitTest and the selection outline use, to pts.
func (e *Editor) objectWorldCorners(bo *sceneformat.BuiltObject, space sceneformat.Space, pts []math2d.Vec2) []math2d.Vec2 {
	if bo == nil || bo.Doc == nil {
		return pts
	}
	rx, ry, rw, rh := objectOutline(bo)
	for _, c := range [4][2]float32{{rx, ry}, {rx + rw, ry}, {rx + rw, ry + rh}, {rx, ry + rh}} {
		p := bo.World.TransformPoint(math2d.Vec2{X: c[0], Y: c[1]})
		x, y := e.layerToWorld(space, p.X, p.Y)
		pts = append(pts, math2d.Vec2{X: x, Y: y})
	}
	return pts
}

// screenFrameWorldCorners appends the world-space corners of the scene's virtual screen, the frame the scene camera shows the player, to pts.
func (e *Editor) screenFrameWorldCorners(pts []math2d.Vec2) []math2d.Vec2 {
	vw, vh := e.ViewportSize()
	for _, c := range [4][2]float32{{0, 0}, {vw, 0}, {vw, vh}, {0, vh}} {
		x, y := e.ScreenToWorld(c[0], c[1])
		pts = append(pts, math2d.Vec2{X: x, Y: y})
	}
	return pts
}

// objectOutline returns bo's outline in its local space: its sprite frame less its origin, or the default marker box for an object without a sprite.
func objectOutline(bo *sceneformat.BuiltObject) (x, y, w, h float32) {
	if bo.Sprite != nil && bo.Sprite.Frame.Width > 0 && bo.Sprite.Frame.Height > 0 {
		return -bo.Doc.OriginX, -bo.Doc.OriginY, bo.Sprite.Frame.Width, bo.Sprite.Frame.Height
	}
	return -defaultMarkerHalfSize, -defaultMarkerHalfSize, defaultMarkerHalfSize * 2, defaultMarkerHalfSize * 2
}

// cameraView2D is the scene camera's view: the virtual screen's center at the canvas's center, at the camera's zoom and rotation. The zoom is clamped like any view's, so the first wheel notch or pan from it does not jump the other way.
func (e *Editor) cameraView2D() View2D {
	vw, vh := e.ViewportSize()
	x, y := e.ScreenToWorld(vw/2, vh/2)
	v := View2D{X: x, Y: y, Zoom: 1}
	if e.Doc != nil && e.Doc.Camera != nil {
		if z := e.Doc.Camera.EffectiveZoom(); z > 0 && isFiniteFloat32(z) {
			v.Zoom = clampView2DZoom(z)
		}
		v.Rotation = e.Doc.Camera.Rotation
	}
	return v
}

// viewAffine maps world space to canvas space through the 2D view.
func (e *Editor) viewAffine() math2d.Affine {
	v := e.View2D()
	cw, ch := e.CanvasSize()
	return math2d.Translate(cw/2, ch/2).
		Mul(math2d.Scale(v.Zoom, v.Zoom)).
		Mul(math2d.Rotate(-v.Rotation)).
		Mul(math2d.Translate(-v.X, -v.Y))
}

// cameraAffine maps world space to the scene's virtual screen through its camera, as BuiltScene.Draw draws world-space layers; it is the identity without a camera.
func (e *Editor) cameraAffine() math2d.Affine {
	if e.Doc == nil || e.Doc.Camera == nil {
		return math2d.Identity()
	}
	cam := e.Doc.Camera
	vw, vh := e.Doc.VirtualWidth, e.Doc.VirtualHeight
	zoom := cam.EffectiveZoom()
	if zoom <= 0 {
		zoom = 1
	}
	camX, camY := clampCameraEditor(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)
	return math2d.Translate(vw/2, vh/2).
		Mul(math2d.Scale(zoom, zoom)).
		Mul(math2d.Rotate(-cam.Rotation)).
		Mul(math2d.Translate(-camX, -camY))
}

// screenAffine maps the scene's virtual screen to canvas space: back through the scene camera into the world, then through the 2D view.
func (e *Editor) screenAffine() math2d.Affine {
	inv, ok := e.cameraAffine().Invert()
	if !ok {
		inv = math2d.Identity()
	}
	return e.viewAffine().Mul(inv)
}

// layerAffine maps a layer's space to canvas space with default (1, 1) parallax.
func (e *Editor) layerAffine(space sceneformat.Space) math2d.Affine {
	return e.layerAffineFor(space, 1, 1)
}

// layerAffineFor maps a layer's space to canvas space in edit mode, applying parallax factors (px, py) against the 2D view for world-space layers.
func (e *Editor) layerAffineFor(space sceneformat.Space, px, py float32) math2d.Affine {
	if space == sceneformat.SpaceScreen {
		return e.screenAffine()
	}
	if px == 1 && py == 1 {
		return e.viewAffine()
	}
	v := e.View2D()
	cw, ch := e.CanvasSize()
	return math2d.Translate(cw/2, ch/2).
		Mul(math2d.Scale(v.Zoom, v.Zoom)).
		Mul(math2d.Rotate(-v.Rotation)).
		Mul(math2d.Translate(-v.X*px, -v.Y*py))
}

// layerToWorld converts a point in a layer's space to world space.
func (e *Editor) layerToWorld(space sceneformat.Space, x, y float32) (float32, float32) {
	if space == sceneformat.SpaceScreen {
		return e.ScreenToWorld(x, y)
	}
	return x, y
}

// canvasToLayer converts a canvas point to a layer's space with default (1, 1) parallax.
func (e *Editor) canvasToLayer(space sceneformat.Space, x, y float32) (float32, float32) {
	return e.canvasToLayerFor(space, 1, 1, x, y)
}

// canvasToLayerFor converts a canvas point to a layer's space, inverting the layer's parallax transform for world-space layers.
func (e *Editor) canvasToLayerFor(space sceneformat.Space, px, py, x, y float32) (float32, float32) {
	if space == sceneformat.SpaceScreen {
		return e.CanvasToScreen(x, y)
	}
	if px == 1 && py == 1 {
		return e.CanvasToWorld(x, y)
	}
	if e == nil {
		return x, y
	}
	v := e.View2D()
	cw, ch := e.CanvasSize()
	rx := (x - cw/2) / v.Zoom
	ry := (y - ch/2) / v.Zoom
	if v.Rotation == 0 {
		return v.X*px + rx, v.Y*py + ry
	}
	sin, cos := math.Sincos(float64(v.Rotation))
	lx := v.X*px + float32(float64(rx)*cos-float64(ry)*sin)
	ly := v.Y*py + float32(float64(rx)*sin+float64(ry)*cos)
	return lx, ly
}

// layerSpaceAndParallax returns the effective Space and parallax factors for layerName, preferring the resolved ActiveScene layer (so inherited layers use the base scene's or layerOverrides' settings) and falling back to e.Doc.Layers.
func (e *Editor) layerSpaceAndParallax(layerName string) (sceneformat.Space, float32, float32) {
	if e == nil {
		return sceneformat.SpaceWorld, 1, 1
	}
	if scene := e.ActiveScene(); scene != nil {
		if bl := scene.Layer(layerName); bl != nil {
			return bl.Space, bl.ParallaxX, bl.ParallaxY
		}
	}
	if li := e.layerIndex(layerName); li >= 0 {
		ld := &e.Doc.Layers[li]
		return ld.EffectiveSpace(), ld.EffectiveParallaxX(), ld.EffectiveParallaxY()
	}
	return sceneformat.SpaceWorld, 1, 1
}

// strokeScale is how many canvas points one unit of m spans, which a hairline drawn under m divides its width by.
func strokeScale(m math2d.Affine) float32 {
	s := float32(math.Sqrt(math.Abs(float64(m.A*m.D - m.B*m.C))))
	if s <= 0 || !isFiniteFloat32(s) {
		return 1
	}
	return s
}

func clampView2DZoom(z float32) float32 {
	return min(max(z, MinView2DZoom), MaxView2DZoom)
}
