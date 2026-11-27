package editor

import (
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// cameraHandleHitRadius is the hit radius in canvas points around the 2D viewport's
// camera position handle (diameter 28 pt, matching the editor spec's minimum hit area).
const cameraHandleHitRadius float32 = 14

// rectHandleHalf is half the drawn side length of a corner/edge resize handle square
// for world-space rectangles (camera bounds and rooms) in canvas points.
const rectHandleHalf float32 = 4.5

// rectHandleHitRadius is the hit radius in canvas points around a corner/edge resize handle.
const rectHandleHitRadius float32 = 10

// rectBorderHitDist is the hit distance in canvas points from a rectangle's perimeter edge.
const rectBorderHitDist float32 = 8

// minCameraBoundsSize is the minimum width/height in world units allowed when dragging
// camera.bounds handles, keeping bounds valid for sceneformat.Validate.
const minCameraBoundsSize float32 = 1

// RectHandle names an interactive handle on a world-space axis-aligned rectangle in the
// 2D viewport, shared by camera.bounds and room-map rectangles.
type RectHandle int

const (
	// RectHandleNone means no handle was hit.
	RectHandleNone RectHandle = iota
	// RectHandleBody moves the entire rectangle without changing its size.
	RectHandleBody
	// RectHandleN resizes the top (-Y) edge.
	RectHandleN
	// RectHandleE resizes the right (+X) edge.
	RectHandleE
	// RectHandleS resizes the bottom (+Y) edge.
	RectHandleS
	// RectHandleW resizes the left (-X) edge.
	RectHandleW
	// RectHandleNE resizes the top-right corner.
	RectHandleNE
	// RectHandleSE resizes the bottom-right corner.
	RectHandleSE
	// RectHandleSW resizes the bottom-left corner.
	RectHandleSW
	// RectHandleNW resizes the top-left corner.
	RectHandleNW
)

// String returns a human-readable label for h.
func (h RectHandle) String() string {
	switch h {
	case RectHandleBody:
		return "Body"
	case RectHandleN:
		return "N"
	case RectHandleE:
		return "E"
	case RectHandleS:
		return "S"
	case RectHandleW:
		return "W"
	case RectHandleNE:
		return "NE"
	case RectHandleSE:
		return "SE"
	case RectHandleSW:
		return "SW"
	case RectHandleNW:
		return "NW"
	default:
		return "None"
	}
}

// IsResize reports whether h is one of the 8 edge or corner resize handles.
func (h RectHandle) IsResize() bool {
	return h >= RectHandleN && h <= RectHandleNW
}

var rectResizeHandles = [8]RectHandle{
	RectHandleNW, RectHandleNE, RectHandleSE, RectHandleSW,
	RectHandleN, RectHandleE, RectHandleS, RectHandleW,
}

func rectHandleWorldPos(h RectHandle, x, y, w, bh float32) (float32, float32) {
	switch h {
	case RectHandleN:
		return x + w/2, y
	case RectHandleE:
		return x + w, y + bh/2
	case RectHandleS:
		return x + w/2, y + bh
	case RectHandleW:
		return x, y + bh/2
	case RectHandleNE:
		return x + w, y
	case RectHandleSE:
		return x + w, y + bh
	case RectHandleSW:
		return x, y + bh
	case RectHandleNW:
		return x, y
	default:
		return x + w/2, y + bh/2
	}
}

// pointToSegmentDist returns the Euclidean distance from point (px, py) to segment (ax, ay)-(bx, by).
func pointToSegmentDist(px, py, ax, ay, bx, by float32) float32 {
	dx, dy := float64(bx-ax), float64(by-ay)
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return float32(math.Hypot(float64(px-ax), float64(py-ay)))
	}
	t := ((float64(px-ax))*dx + (float64(py-ay))*dy) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	qx := float64(ax) + t*dx
	qy := float64(ay) + t*dy
	return float32(math.Hypot(float64(px)-qx, float64(py)-qy))
}

// hitTestWorldRect tests canvas point (canvasX, canvasY) against the 8 resize handles
// (corners before edge midpoints) and optionally the border/interior of world rectangle
// (x, y, w, bh).
func (e *Editor) hitTestWorldRect(x, y, w, bh, canvasX, canvasY float32, hitHandles, hitBorder, hitInterior bool) RectHandle {
	if hitHandles {
		for _, h := range rectResizeHandles {
			wx, wy := rectHandleWorldPos(h, x, y, w, bh)
			cx, cy := e.WorldToCanvas(wx, wy)
			if float32(math.Hypot(float64(canvasX-cx), float64(canvasY-cy))) <= rectHandleHitRadius {
				return h
			}
		}
	}
	if hitBorder {
		var c [4][2]float32
		for i, pt := range [4][2]float32{{x, y}, {x + w, y}, {x + w, y + bh}, {x, y + bh}} {
			c[i][0], c[i][1] = e.WorldToCanvas(pt[0], pt[1])
		}
		for i := range 4 {
			j := (i + 1) % 4
			if pointToSegmentDist(canvasX, canvasY, c[i][0], c[i][1], c[j][0], c[j][1]) <= rectBorderHitDist {
				return RectHandleBody
			}
		}
	}
	if hitInterior {
		wx, wy := e.CanvasToWorld(canvasX, canvasY)
		if wx >= x && wx <= x+w && wy >= y && wy <= y+bh {
			return RectHandleBody
		}
	}
	return RectHandleNone
}

func snapCoord(v, step float32) float32 {
	if step <= 0 {
		return normalizeZero(v)
	}
	return normalizeZero(float32(math.Round(float64(v)/float64(step)) * float64(step)))
}

func normalizeZero(v float32) float32 {
	if v == 0 {
		return 0
	}
	return v
}

// applyRectDrag computes the updated world rectangle for handle h given the drag start
// rectangle, drag start world point, current world point, minimum dimensions, and
// optional grid snap increments (0 disables snapping).
func applyRectDrag(h RectHandle, origX, origY, origW, origH, startWX, startWY, curWX, curWY, minW, minH, snapW, snapH float32) (x, y, w, bh float32) {
	dx := curWX - startWX
	dy := curWY - startWY
	left, top := origX, origY
	right, bottom := origX+origW, origY+origH

	if h == RectHandleBody {
		nx := snapCoord(origX+dx, snapW)
		ny := snapCoord(origY+dy, snapH)
		return nx, ny, normalizeZero(origW), normalizeZero(origH)
	}

	effMinW := minW
	if snapW > 0 && effMinW < snapW {
		effMinW = snapW
	}
	effMinH := minH
	if snapH > 0 && effMinH < snapH {
		effMinH = snapH
	}

	switch h {
	case RectHandleW, RectHandleNW, RectHandleSW:
		left = snapCoord(origX+dx, snapW)
		if right-left < effMinW {
			left = right - effMinW
		}
	}
	switch h {
	case RectHandleE, RectHandleNE, RectHandleSE:
		right = snapCoord(origX+origW+dx, snapW)
		if right-left < effMinW {
			right = left + effMinW
		}
	}
	switch h {
	case RectHandleN, RectHandleNW, RectHandleNE:
		top = snapCoord(origY+dy, snapH)
		if bottom-top < effMinH {
			top = bottom - effMinH
		}
	}
	switch h {
	case RectHandleS, RectHandleSW, RectHandleSE:
		bottom = snapCoord(origY+origH+dy, snapH)
		if bottom-top < effMinH {
			bottom = top + effMinH
		}
	}
	return normalizeZero(left), normalizeZero(top), normalizeZero(right - left), normalizeZero(bottom - top)
}

type cameraDragState struct {
	active      bool
	moved       bool
	startView   View2D
	startWorldX float32
	startWorldY float32
	startCamX   float32
	startCamY   float32
	origCamera  *sceneformat.CameraDoc
}

type cameraBoundsDragState struct {
	active      bool
	moved       bool
	handle      RectHandle
	startView   View2D
	startWorldX float32
	startWorldY float32
	origCamera  *sceneformat.CameraDoc
	origBounds  sceneformat.BoundsDoc
}

type bgPreviewState struct {
	active  bool
	before  string
	pending string
}

func (e *Editor) previewBackground(color string) {
	if e == nil || e.playing || e.Doc == nil {
		return
	}
	if !e.bgPreview.active {
		e.bgPreview.active = true
		e.bgPreview.before = e.Doc.BackgroundColor
	}
	e.bgPreview.pending = color
	e.Doc.BackgroundColor = color
	e.Rebuild()
	e.notify()
}

func (e *Editor) flushBackgroundPreview() error {
	if e == nil || !e.bgPreview.active {
		return nil
	}
	return e.finishBackgroundPreview(e.bgPreview.pending)
}

func (e *Editor) finishBackgroundPreview(formatted string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		e.cancelBackgroundPreview()
		return ErrPlaying
	}
	return e.SetBackground(formatted)
}

func (e *Editor) cancelBackgroundPreview() {
	if e == nil || !e.bgPreview.active {
		return
	}
	orig := e.bgPreview.before
	e.bgPreview = bgPreviewState{}
	if e.Doc != nil {
		e.Doc.BackgroundColor = orig
		e.Rebuild()
	}
	e.notify()
}

// ShowingSceneSettings reports whether the inspector is showing the open scene's own
// settings (a scene document is loaded, no object, room, or layer is selected for
// inspection, and no asset file's import settings are being inspected).
func (e *Editor) ShowingSceneSettings() bool {
	if e == nil || e.Doc == nil || e.SelectedID != "" {
		return false
	}
	if e.rooms != nil && e.rooms.selectedRoomID != "" {
		return false
	}
	if e.inspectLayer && e.layerIndex(e.ActiveLayer) >= 0 {
		return false
	}
	rel, _ := e.inspectedImportAsset()
	if rel != "" {
		return false
	}
	if _, _, ok := e.inspectedTilesetTile(); ok {
		return false
	}
	return true
}

// effectiveCamera returns a cloned CameraDoc representing the scene's active start camera:
// Doc.Camera when set, or a default camera centered on the scene's virtual viewport
// (VirtualWidth/2, VirtualHeight/2) at zoom 1 and rotation 0 when Doc.Camera is nil.
func (e *Editor) effectiveCamera() sceneformat.CameraDoc {
	if e != nil && e.Doc != nil {
		if e.Doc.Camera != nil {
			return *cloneCameraDoc(e.Doc.Camera)
		}
		return sceneformat.CameraDoc{
			X: e.Doc.VirtualWidth / 2,
			Y: e.Doc.VirtualHeight / 2,
		}
	}
	return sceneformat.CameraDoc{}
}

func (e *Editor) validateSceneEdit(mutate func(d *sceneformat.SceneDoc)) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	candidate := *e.Doc
	mutate(&candidate)
	return sceneformat.Validate(&candidate)
}

// SetSceneName updates the open scene's Name through History, checked by sceneformat.Validate.
func (e *Editor) SetSceneName(name string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.Name = name
	}); err != nil {
		return err
	}
	if e.Doc.Name == name {
		return nil
	}
	e.History.Execute(&setSceneNameCmd{
		ed:      e,
		oldName: e.Doc.Name,
		newName: name,
	})
	return nil
}

// SetVirtualSize updates the open scene's VirtualWidth and VirtualHeight through History.
// Both dimensions must be finite and positive; non-positive sizes are refused before
// modifying the document or History.
func (e *Editor) SetVirtualSize(width, height float32) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.VirtualWidth = width
		d.VirtualHeight = height
	}); err != nil {
		return err
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("editor: virtual size (%v, %v) must be positive", width, height)
	}
	if e.Doc.VirtualWidth == width && e.Doc.VirtualHeight == height {
		return nil
	}
	e.History.Execute(&setVirtualSizeCmd{
		ed:   e,
		oldW: e.Doc.VirtualWidth,
		oldH: e.Doc.VirtualHeight,
		newW: width,
		newH: height,
	})
	return nil
}

// SetBackground updates the open scene's BackgroundColor CSS string (or clears it when empty)
// through History, validated by sceneformat.Validate.
func (e *Editor) SetBackground(color string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	hadPreview := e.bgPreview.active
	if hadPreview {
		orig := e.bgPreview.before
		e.bgPreview = bgPreviewState{}
		if e.Doc != nil {
			e.Doc.BackgroundColor = orig
		}
	}
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.BackgroundColor = color
	}); err != nil {
		if hadPreview && e.Doc != nil {
			e.Rebuild()
			e.notify()
		}
		return err
	}
	if e.Doc.BackgroundColor == color {
		if hadPreview {
			e.Rebuild()
			e.notify()
		}
		return nil
	}
	e.History.Execute(&setBackgroundCmd{
		ed:       e,
		oldColor: e.Doc.BackgroundColor,
		newColor: color,
	})
	return nil
}

// SetCamera replaces the open scene's Camera (or clears it when nil) through History,
// validated by sceneformat.Validate.
func (e *Editor) SetCamera(cam *sceneformat.CameraDoc) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	next := cloneCameraDoc(cam)
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.Camera = next
	}); err != nil {
		return err
	}
	if cameraDocEqual(e.Doc.Camera, next) {
		return nil
	}
	e.History.Execute(&setCameraCmd{
		ed:     e,
		label:  "Set camera",
		status: "Updated camera",
		oldCam: cloneCameraDoc(e.Doc.Camera),
		newCam: next,
	})
	return nil
}

// SetCameraPosition updates the open scene camera's world-space (X, Y) position through
// History, initializing Camera from the scene's default center if it was previously unset.
func (e *Editor) SetCameraPosition(x, y float32) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	next := e.effectiveCamera()
	next.X, next.Y = x, y
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.Camera = &next
	}); err != nil {
		return err
	}
	if e.Doc.Camera != nil && e.Doc.Camera.X == x && e.Doc.Camera.Y == y {
		return nil
	}
	e.History.Execute(&setCameraCmd{
		ed:     e,
		label:  "Move camera",
		status: "Updated camera position",
		oldCam: cloneCameraDoc(e.Doc.Camera),
		newCam: cloneCameraDoc(&next),
	})
	return nil
}

// SetCameraZoom updates the open scene camera's zoom factor through History.
// Non-positive or non-finite values are refused by sceneformat.Validate.
func (e *Editor) SetCameraZoom(zoom float32) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	next := e.effectiveCamera()
	next.Zoom = sceneformat.Float32(zoom)
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.Camera = &next
	}); err != nil {
		return err
	}
	if e.Doc.Camera != nil && e.Doc.Camera.Zoom != nil && *e.Doc.Camera.Zoom == zoom {
		return nil
	}
	e.History.Execute(&setCameraCmd{
		ed:     e,
		label:  "Set camera zoom",
		status: "Updated camera zoom",
		oldCam: cloneCameraDoc(e.Doc.Camera),
		newCam: cloneCameraDoc(&next),
	})
	return nil
}

// SetCameraRotation updates the open scene camera's rotation in radians through History.
func (e *Editor) SetCameraRotation(radians float32) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	next := e.effectiveCamera()
	next.Rotation = radians
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.Camera = &next
	}); err != nil {
		return err
	}
	if e.Doc.Camera != nil && e.Doc.Camera.Rotation == radians {
		return nil
	}
	e.History.Execute(&setCameraCmd{
		ed:     e,
		label:  "Set camera rotation",
		status: "Updated camera rotation",
		oldCam: cloneCameraDoc(e.Doc.Camera),
		newCam: cloneCameraDoc(&next),
	})
	return nil
}

// SetCameraBounds sets or clears (when bounds is nil) the open scene camera's world-space
// clamp rectangle through History, validated by sceneformat.Validate.
func (e *Editor) SetCameraBounds(bounds *sceneformat.BoundsDoc) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	if bounds == nil {
		if e.Doc.Camera == nil || e.Doc.Camera.Bounds == nil {
			return nil
		}
		next := cloneCameraDoc(e.Doc.Camera)
		next.Bounds = nil
		e.History.Execute(&setCameraCmd{
			ed:     e,
			label:  "Clear camera bounds",
			status: "Cleared camera bounds",
			oldCam: cloneCameraDoc(e.Doc.Camera),
			newCam: next,
		})
		return nil
	}
	next := e.effectiveCamera()
	next.Bounds = cloneBoundsDoc(bounds)
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.Camera = &next
	}); err != nil {
		return err
	}
	if e.Doc.Camera != nil && boundsDocEqual(e.Doc.Camera.Bounds, bounds) {
		return nil
	}
	e.History.Execute(&setCameraCmd{
		ed:     e,
		label:  "Set camera bounds",
		status: "Updated camera bounds",
		oldCam: cloneCameraDoc(e.Doc.Camera),
		newCam: cloneCameraDoc(&next),
	})
	return nil
}

// ClearCameraBounds removes the open scene camera's clamp bounds through History.
func (e *Editor) ClearCameraBounds() error {
	return e.SetCameraBounds(nil)
}

// CameraHandleCanvasPos returns the canvas coordinates of the draggable camera position
// handle when scene settings are active in 2D edit mode.
func (e *Editor) CameraHandleCanvasPos() (x, y float32, ok bool) {
	if !e.ShowingSceneSettings() || e.playing || e.ViewMode != ViewMode2D {
		return 0, 0, false
	}
	cam := e.effectiveCamera()
	hx, hy := e.WorldToCanvas(cam.X, cam.Y)
	return hx, hy, true
}

// HitTestCameraHandle reports whether canvas point (canvasX, canvasY) falls on the 2D
// viewport's camera position handle.
func (e *Editor) HitTestCameraHandle(canvasX, canvasY float32) bool {
	hx, hy, ok := e.CameraHandleCanvasPos()
	if !ok {
		return false
	}
	return float32(math.Hypot(float64(canvasX-hx), float64(canvasY-hy))) <= cameraHandleHitRadius
}

// BeginCameraDrag starts dragging the scene camera's position handle in the 2D viewport
// if scene settings are active, the tool is Select or Move, and (canvasX, canvasY) hits
// the handle. The 2D view stays anchored at its pre-drag state while dragging so the
// handle moves smoothly under the pointer without feedback loops.
func (e *Editor) BeginCameraDrag(canvasX, canvasY float32) bool {
	if !e.ShowingSceneSettings() || e.playing || e.ViewMode != ViewMode2D {
		return false
	}
	if e.Tool != ToolSelect && e.Tool != ToolMove {
		return false
	}
	if !e.HitTestCameraHandle(canvasX, canvasY) {
		return false
	}
	startView := e.View2D()
	wx, wy := e.CanvasToWorld(canvasX, canvasY)
	eff := e.effectiveCamera()
	e.cameraDrag = cameraDragState{
		active:      true,
		startView:   startView,
		startWorldX: wx,
		startWorldY: wy,
		startCamX:   eff.X,
		startCamY:   eff.Y,
		origCamera:  cloneCameraDoc(e.Doc.Camera),
	}
	e.notify()
	return true
}

// UpdateCameraDrag moves the scene camera live to follow the pointer at (canvasX, canvasY)
// during an active camera handle drag.
func (e *Editor) UpdateCameraDrag(canvasX, canvasY float32) bool {
	if e == nil || !e.cameraDrag.active || e.playing || e.Doc == nil {
		return false
	}
	wx, wy := e.CanvasToWorld(canvasX, canvasY)
	newX := e.cameraDrag.startCamX + (wx - e.cameraDrag.startWorldX)
	newY := e.cameraDrag.startCamY + (wy - e.cameraDrag.startWorldY)
	if !isFiniteFloat32(newX) || !isFiniteFloat32(newY) {
		return false
	}
	next := cloneCameraDoc(e.cameraDrag.origCamera)
	if next == nil {
		next = &sceneformat.CameraDoc{}
	}
	next.X, next.Y = newX, newY
	if newX != e.cameraDrag.startCamX || newY != e.cameraDrag.startCamY {
		e.cameraDrag.moved = true
	}
	e.Doc.Camera = next
	e.Rebuild()
	e.notify()
	return true
}

// EndCameraDrag finishes an active camera handle drag and commits the new position as a
// single undoable command on History. It returns true when a change was committed.
func (e *Editor) EndCameraDrag() bool {
	if e == nil || !e.cameraDrag.active {
		return false
	}
	st := e.cameraDrag
	e.cameraDrag = cameraDragState{}

	if e.Doc == nil || !st.moved || e.Doc.Camera == nil {
		if e.Doc != nil {
			e.Doc.Camera = cloneCameraDoc(st.origCamera)
			e.Rebuild()
		}
		e.notify()
		return false
	}
	newCam := cloneCameraDoc(e.Doc.Camera)
	// Restore pre-drag camera before committing through History so Undo restores exact state.
	e.Doc.Camera = cloneCameraDoc(st.origCamera)
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.Camera = newCam
	}); err != nil {
		e.Rebuild()
		e.report(err)
		return false
	}
	if cameraDocEqual(e.Doc.Camera, newCam) {
		e.Rebuild()
		e.notify()
		return false
	}
	e.History.Execute(&setCameraCmd{
		ed:     e,
		label:  "Move camera",
		status: "Updated camera position",
		oldCam: cloneCameraDoc(st.origCamera),
		newCam: newCam,
	})
	return true
}

// CancelCameraDrag aborts any active camera handle drag and restores the pre-drag camera.
func (e *Editor) CancelCameraDrag() {
	if e == nil || !e.cameraDrag.active {
		return
	}
	orig := e.cameraDrag.origCamera
	e.cameraDrag = cameraDragState{}
	if e.Doc != nil {
		e.Doc.Camera = cloneCameraDoc(orig)
		e.Rebuild()
	}
	e.notify()
}

// CameraBoundsHandleCanvasPos returns the canvas coordinates of handle h on the scene
// camera's bounds rectangle when scene settings are active, bounds are set, and the
// editor is in 2D edit mode.
func (e *Editor) CameraBoundsHandleCanvasPos(h RectHandle) (x, y float32, ok bool) {
	if !e.ShowingSceneSettings() || e.playing || e.ViewMode != ViewMode2D {
		return 0, 0, false
	}
	if e.Doc == nil || e.Doc.Camera == nil || e.Doc.Camera.Bounds == nil {
		return 0, 0, false
	}
	if h == RectHandleNone {
		return 0, 0, false
	}
	b := e.Doc.Camera.Bounds
	wx, wy := rectHandleWorldPos(h, b.X, b.Y, b.Width, b.Height)
	cx, cy := e.WorldToCanvas(wx, wy)
	return cx, cy, true
}

// HitTestCameraBoundsHandle tests canvas point (canvasX, canvasY) against the 8 resize
// handles and perimeter border (or empty interior) of camera.bounds when scene settings
// are active in 2D edit mode. Resize handles are checked before the border, and the
// interior yields to objects and rooms so objects inside camera.bounds remain clickable.
func (e *Editor) HitTestCameraBoundsHandle(canvasX, canvasY float32) RectHandle {
	if !e.ShowingSceneSettings() || e.playing || e.ViewMode != ViewMode2D {
		return RectHandleNone
	}
	if e.Doc == nil || e.Doc.Camera == nil || e.Doc.Camera.Bounds == nil {
		return RectHandleNone
	}
	b := e.Doc.Camera.Bounds
	if h := e.hitTestWorldRect(b.X, b.Y, b.Width, b.Height, canvasX, canvasY, true, true, false); h != RectHandleNone {
		return h
	}
	// Allow dragging empty interior space inside camera.bounds to move it, without stealing clicks on objects or rooms.
	if e.HitTest(canvasX, canvasY) == "" && e.HitTestRoom(canvasX, canvasY) == "" {
		return e.hitTestWorldRect(b.X, b.Y, b.Width, b.Height, canvasX, canvasY, false, false, true)
	}
	return RectHandleNone
}

// DraggingCameraBounds reports the camera.bounds handle currently being dragged, or RectHandleNone.
func (e *Editor) DraggingCameraBounds() RectHandle {
	if e == nil || !e.cameraBoundsDrag.active {
		return RectHandleNone
	}
	return e.cameraBoundsDrag.handle
}

// BeginCameraBoundsDrag starts dragging a resize handle or the body/border of camera.bounds
// in the 2D viewport if scene settings are active, the tool is Select or Move, and
// (canvasX, canvasY) hits camera.bounds. View2D stays anchored at its pre-drag state while
// dragging so clamping changes do not feed back into pointer coordinates.
func (e *Editor) BeginCameraBoundsDrag(canvasX, canvasY float32) RectHandle {
	if !e.ShowingSceneSettings() || e.playing || e.ViewMode != ViewMode2D {
		return RectHandleNone
	}
	if e.Tool != ToolSelect && e.Tool != ToolMove {
		return RectHandleNone
	}
	if e.Doc == nil || e.Doc.Camera == nil || e.Doc.Camera.Bounds == nil {
		return RectHandleNone
	}
	if !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return RectHandleNone
	}
	h := e.HitTestCameraBoundsHandle(canvasX, canvasY)
	if h == RectHandleNone {
		return RectHandleNone
	}
	startView := e.View2D()
	wx, wy := e.CanvasToWorld(canvasX, canvasY)
	e.cameraBoundsDrag = cameraBoundsDragState{
		active:      true,
		handle:      h,
		startView:   startView,
		startWorldX: wx,
		startWorldY: wy,
		origCamera:  cloneCameraDoc(e.Doc.Camera),
		origBounds:  *e.Doc.Camera.Bounds,
	}
	e.notify()
	return h
}

// UpdateCameraBoundsDrag updates camera.bounds live to follow the pointer at (canvasX, canvasY)
// during an active camera.bounds drag.
func (e *Editor) UpdateCameraBoundsDrag(canvasX, canvasY float32) bool {
	if e == nil || !e.cameraBoundsDrag.active || e.playing || e.Doc == nil || e.Doc.Camera == nil {
		return false
	}
	if !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return false
	}
	wx, wy := e.CanvasToWorld(canvasX, canvasY)
	orig := e.cameraBoundsDrag.origBounds
	nx, ny, nw, nh := applyRectDrag(
		e.cameraBoundsDrag.handle,
		orig.X, orig.Y, orig.Width, orig.Height,
		e.cameraBoundsDrag.startWorldX, e.cameraBoundsDrag.startWorldY,
		wx, wy,
		minCameraBoundsSize, minCameraBoundsSize,
		0, 0,
	)
	if !isFiniteFloat32(nx) || !isFiniteFloat32(ny) || !isFiniteFloat32(nw) || !isFiniteFloat32(nh) || nw <= 0 || nh <= 0 {
		return false
	}
	nextCam := cloneCameraDoc(e.cameraBoundsDrag.origCamera)
	if nextCam == nil {
		nextCam = &sceneformat.CameraDoc{}
	}
	nextCam.Bounds = &sceneformat.BoundsDoc{X: nx, Y: ny, Width: nw, Height: nh}
	if *nextCam.Bounds != orig {
		e.cameraBoundsDrag.moved = true
	}
	e.Doc.Camera = nextCam
	e.Rebuild()
	e.notify()
	return true
}

// EndCameraBoundsDrag finishes an active camera.bounds drag and commits the new bounds as a
// single undoable command on History. Returns true when a change was committed.
func (e *Editor) EndCameraBoundsDrag() bool {
	if e == nil || !e.cameraBoundsDrag.active {
		return false
	}
	st := e.cameraBoundsDrag
	e.cameraBoundsDrag = cameraBoundsDragState{}

	if e.Doc == nil || !st.moved || e.Doc.Camera == nil || e.Doc.Camera.Bounds == nil {
		if e.Doc != nil {
			e.Doc.Camera = cloneCameraDoc(st.origCamera)
			e.Rebuild()
		}
		e.notify()
		return false
	}
	newCam := cloneCameraDoc(e.Doc.Camera)
	// Restore pre-drag camera before committing through History so Undo restores exact state.
	e.Doc.Camera = cloneCameraDoc(st.origCamera)
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.Camera = newCam
	}); err != nil {
		e.Rebuild()
		e.report(err)
		return false
	}
	if cameraDocEqual(e.Doc.Camera, newCam) {
		e.Rebuild()
		e.notify()
		return false
	}
	label := "Resize camera bounds"
	status := "Resized camera bounds"
	if st.handle == RectHandleBody {
		label = "Move camera bounds"
		status = "Moved camera bounds"
	}
	e.History.Execute(&setCameraCmd{
		ed:     e,
		label:  label,
		status: status,
		oldCam: cloneCameraDoc(st.origCamera),
		newCam: newCam,
	})
	return true
}

// CancelCameraBoundsDrag aborts any active camera.bounds handle drag and restores the
// pre-drag camera bounds without modifying History.
func (e *Editor) CancelCameraBoundsDrag() {
	if e == nil || !e.cameraBoundsDrag.active {
		return
	}
	orig := e.cameraBoundsDrag.origCamera
	e.cameraBoundsDrag = cameraBoundsDragState{}
	if e.Doc != nil {
		e.Doc.Camera = cloneCameraDoc(orig)
		e.Rebuild()
	}
	e.notify()
}

// --- Commands ---

type setSceneNameCmd struct {
	ed      *Editor
	oldName string
	newName string
}

func (c *setSceneNameCmd) Label() string { return "Set scene name" }

func (c *setSceneNameCmd) Do() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.Name = c.newName
	c.ed.Status = "Updated scene name"
}

func (c *setSceneNameCmd) Undo() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.Name = c.oldName
}

type setVirtualSizeCmd struct {
	ed         *Editor
	oldW, oldH float32
	newW, newH float32
}

func (c *setVirtualSizeCmd) Label() string { return "Set virtual size" }

func (c *setVirtualSizeCmd) Do() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.VirtualWidth = c.newW
	c.ed.Doc.VirtualHeight = c.newH
	c.ed.Status = "Updated virtual size"
}

func (c *setVirtualSizeCmd) Undo() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.VirtualWidth = c.oldW
	c.ed.Doc.VirtualHeight = c.oldH
}

type setBackgroundCmd struct {
	ed       *Editor
	oldColor string
	newColor string
}

func (c *setBackgroundCmd) Label() string { return "Set background color" }

func (c *setBackgroundCmd) Do() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.BackgroundColor = c.newColor
	c.ed.Status = "Updated background color"
}

func (c *setBackgroundCmd) Undo() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.BackgroundColor = c.oldColor
}

type setCameraCmd struct {
	ed     *Editor
	label  string
	status string
	oldCam *sceneformat.CameraDoc
	newCam *sceneformat.CameraDoc
}

func (c *setCameraCmd) Label() string { return c.label }

func (c *setCameraCmd) Do() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.Camera = cloneCameraDoc(c.newCam)
	c.ed.Status = c.status
}

func (c *setCameraCmd) Undo() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.Camera = cloneCameraDoc(c.oldCam)
}

func cloneCameraDoc(c *sceneformat.CameraDoc) *sceneformat.CameraDoc {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Zoom = cloneFloat32Ptr(c.Zoom)
	cp.Bounds = cloneBoundsDoc(c.Bounds)
	return &cp
}

func cloneBoundsDoc(b *sceneformat.BoundsDoc) *sceneformat.BoundsDoc {
	if b == nil {
		return nil
	}
	cp := *b
	return &cp
}

func boundsDocEqual(a, b *sceneformat.BoundsDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func cameraDocEqual(a, b *sceneformat.CameraDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.X == b.X &&
		a.Y == b.Y &&
		a.Rotation == b.Rotation &&
		equalFloat32Ptr(a.Zoom, b.Zoom) &&
		boundsDocEqual(a.Bounds, b.Bounds)
}
