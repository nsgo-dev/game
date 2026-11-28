package editor

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/skeletal"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// RigHandle identifies an interactive handle in the 2D skeletal rig editor canvas.
type RigHandle int

const (
	// RigHandleNone is no handle.
	RigHandleNone RigHandle = iota
	// RigHandleBoneMove is the joint circle at the selected bone's origin: dragging moves the bone in its parent's space.
	RigHandleBoneMove
	// RigHandleBoneRotate is the rotation ring around the selected bone's origin: dragging rotates the bone.
	RigHandleBoneRotate
	// RigHandleBoneLength is the diamond at the selected bone's tip: dragging sets the bone's length.
	RigHandleBoneLength
	// RigHandleAttachmentOrigin is the pivot crosshair on the selected slot's active attachment: dragging edits OriginX and OriginY.
	RigHandleAttachmentOrigin
)

// String returns a human-readable label for h.
func (h RigHandle) String() string {
	switch h {
	case RigHandleBoneMove:
		return "Move Bone"
	case RigHandleBoneRotate:
		return "Rotate Bone"
	case RigHandleBoneLength:
		return "Bone Length"
	case RigHandleAttachmentOrigin:
		return "Attachment Origin"
	default:
		return "None"
	}
}

const (
	rigJointPickRadius  float32 = 8
	rigTipPickRadius    float32 = 8
	rigRingMinRadius    float32 = 26
	rigRingGap          float32 = 12
	rigRingPickRadius   float32 = 6
	rigOriginPickRadius float32 = 8
	rigMinTipCanvas     float32 = 18
	rigBoneHitDist      float32 = 8
)

type rigDragState struct {
	handle           RigHandle
	boneName         string
	slotName         string
	skinName         string
	startSnap        *rigState
	startBone        skeletal.BoneData
	startAttach      skeletal.Attachment
	parentWorld      skeletal.BoneTransform
	attachWorld      skeletal.BoneTransform
	grabWorld        math2d.Vec2
	prevAngle        float64
	accumAngle       float64
	animateMode      bool
	animName         string
	animTime         time.Duration
	startTransKey    [2]float32
	startTransCurve  skeletal.CurveType
	startTransBezier [4]float32
	startRotKey      float32
	startRotCurve    skeletal.CurveType
	startRotBezier   [4]float32
}

// SetRigCanvasSize records the rig editor canvas's laid-out size in points.
func (e *Editor) SetRigCanvasSize(w, h float32) {
	if !e.RigOpen() || !isFiniteFloat32(w) || !isFiniteFloat32(h) || w <= 0 || h <= 0 {
		return
	}
	e.rig.canvasW, e.rig.canvasH = w, h
}

func (e *Editor) rigCanvasSize() (float32, float32) {
	if e.RigOpen() && e.rig.canvasW > 0 && e.rig.canvasH > 0 {
		return e.rig.canvasW, e.rig.canvasH
	}
	return 360, 260
}

func (e *Editor) rigZoom() float32 {
	if e.RigOpen() && e.rig.viewZoom > 0 {
		return e.rig.viewZoom
	}
	return 2
}

// RigWorldToCanvas converts a point in rig world coordinates to rig editor canvas points.
func (e *Editor) RigWorldToCanvas(wx, wy float32) (float32, float32) {
	if !e.RigOpen() {
		return wx, wy
	}
	cw, ch := e.rigCanvasSize()
	z := e.rigZoom()
	return cw/2 + (wx-e.rig.viewX)*z, ch/2 + (wy-e.rig.viewY)*z
}

// RigCanvasToWorld converts a point on the rig editor canvas to rig world coordinates.
func (e *Editor) RigCanvasToWorld(cx, cy float32) (float32, float32) {
	if !e.RigOpen() {
		return cx, cy
	}
	cw, ch := e.rigCanvasSize()
	z := e.rigZoom()
	return e.rig.viewX + (cx-cw/2)/z, e.rig.viewY + (cy-ch/2)/z
}

// PanRigView pans the rig editor canvas by (dx, dy) canvas points.
func (e *Editor) PanRigView(dx, dy float32) {
	if !e.RigOpen() || !isFiniteFloat32(dx) || !isFiniteFloat32(dy) {
		return
	}
	z := e.rigZoom()
	e.rig.viewX -= dx / z
	e.rig.viewY -= dy / z
	e.notify()
}

// ZoomRigView scales the rig editor canvas zoom by factor, clamped to [0.25, 16].
func (e *Editor) ZoomRigView(factor float32) {
	if !e.RigOpen() || !isFiniteFloat32(factor) || factor <= 0 {
		return
	}
	z := min(max(e.rigZoom()*factor, 0.25), 16)
	e.rig.viewZoom = z
	e.notify()
}

// ResetRigView centers the rig editor canvas on the rig origin at 2x zoom.
func (e *Editor) ResetRigView() {
	if !e.RigOpen() {
		return
	}
	e.rig.viewX = 0
	e.rig.viewY = 0
	e.rig.viewZoom = 2
	e.notify()
}

func boneTipWorld(wb skeletal.BoneTransform, length float32) math2d.Vec2 {
	sn, cs := math.Sincos(float64(wb.Rotation))
	dx := float32(cs) * length * wb.ScaleX
	dy := float32(sn) * length * wb.ScaleX
	return math2d.Vec2{X: wb.X + dx, Y: wb.Y + dy}
}

// RigBoneHandlePos returns the canvas positions of bone's origin joint, length tip handle,
// and rotation ring radius, or false when the bone is not found.
func (e *Editor) RigBoneHandlePos(bone string) (origin, tip math2d.Vec2, ringRadius float32, ok bool) {
	if !e.RigOpen() || e.rig.skel == nil {
		return math2d.Vec2{}, math2d.Vec2{}, 0, false
	}
	idx := findRigBone(e.rig.data, bone)
	if idx < 0 {
		return math2d.Vec2{}, math2d.Vec2{}, 0, false
	}
	wb, exists := e.rig.skel.WorldBone(bone)
	if !exists {
		return math2d.Vec2{}, math2d.Vec2{}, 0, false
	}
	ox, oy := e.RigWorldToCanvas(wb.X, wb.Y)
	origin = math2d.Vec2{X: ox, Y: oy}
	bd := e.rig.data.Bones[idx]
	z := e.rigZoom()
	tw := boneTipWorld(wb, bd.Length)
	tx, ty := e.RigWorldToCanvas(tw.X, tw.Y)
	tip = math2d.Vec2{X: tx, Y: ty}
	dist := vecLen(tip.Sub(origin))
	if dist < rigMinTipCanvas {
		sn, cs := math.Sincos(float64(wb.Rotation))
		dir := math2d.Vec2{X: float32(cs), Y: float32(sn)}
		if wb.ScaleX < 0 {
			dir = dir.Scale(-1)
		}
		tip = origin.Add(dir.Scale(rigMinTipCanvas))
		dist = rigMinTipCanvas
	}
	ringRadius = max(rigRingMinRadius, dist+rigRingGap)
	_ = z
	return origin, tip, ringRadius, true
}

func (e *Editor) resolveRigSlotAttachment(slot string) (skinName string, att skeletal.Attachment, boneWorld skeletal.BoneTransform, ok bool) {
	if !e.RigOpen() || e.rig.skel == nil || slot == "" {
		return "", skeletal.Attachment{}, skeletal.BoneTransform{}, false
	}
	si := findRigSlot(e.rig.data, slot)
	if si < 0 {
		return "", skeletal.Attachment{}, skeletal.BoneTransform{}, false
	}
	wb, exists := e.rig.skel.WorldBone(e.rig.data.Slots[si].Bone)
	if !exists {
		return "", skeletal.Attachment{}, skeletal.BoneTransform{}, false
	}
	if e.rig.skin != "" {
		if sk, has := e.rig.data.Skins[e.rig.skin]; has {
			if a, hasAtt := sk[slot]; hasAtt {
				return e.rig.skin, a, wb, true
			}
		}
	}
	if sk, has := e.rig.data.Skins["default"]; has {
		if a, hasAtt := sk[slot]; hasAtt {
			return "default", a, wb, true
		}
	}
	return "", skeletal.Attachment{}, skeletal.BoneTransform{}, false
}

func composeBoneLocal(p skeletal.BoneTransform, lx, ly, lrot, lsx, lsy float32) skeletal.BoneTransform {
	sx := lx * p.ScaleX
	sy := ly * p.ScaleY
	sn, cs := math.Sincos(float64(p.Rotation))
	wx := p.X + float32(cs)*sx - float32(sn)*sy
	wy := p.Y + float32(sn)*sx + float32(cs)*sy
	rot := p.Rotation + lrot
	if p.ScaleX*p.ScaleY < 0 {
		rot = p.Rotation - lrot
	}
	return skeletal.BoneTransform{
		X:        wx,
		Y:        wy,
		Rotation: rot,
		ScaleX:   p.ScaleX * lsx,
		ScaleY:   p.ScaleY * lsy,
	}
}

// RigAttachmentOriginCanvasPos returns the canvas position of slot's attachment origin handle
// (drawn offset along the attachment's local axes from the frame's top-left corner at the pivot),
// or false when the slot has no skin attachment.
func (e *Editor) RigAttachmentOriginCanvasPos(slot string) (math2d.Vec2, bool) {
	_, att, wb, ok := e.resolveRigSlotAttachment(slot)
	if !ok {
		return math2d.Vec2{}, false
	}
	wt := composeBoneLocal(wb, att.X, att.Y, att.Rotation, att.ScaleX, att.ScaleY)
	cx, cy := e.RigWorldToCanvas(wt.X, wt.Y)
	return math2d.Vec2{X: cx, Y: cy}, true
}

// PickRigHandle returns the rig editor handle under canvas point (x, y).
// Priority: attachment origin when a slot is selected and either the slot/attachment focus is active
// or the attachment origin is offset from the selected bone's joint; otherwise bone move joint,
// bone length tip, bone rotation ring, then attachment origin.
func (e *Editor) PickRigHandle(x, y float32) RigHandle {
	if !e.RigOpen() || e.rig.skel == nil {
		return RigHandleNone
	}
	p := math2d.Vec2{X: x, Y: y}
	var boneOrigin, boneTip math2d.Vec2
	var boneRing float32
	hasBone := false
	if e.rig.selectedBone != "" {
		boneOrigin, boneTip, boneRing, hasBone = e.RigBoneHandlePos(e.rig.selectedBone)
	}
	if e.rig.selectedSlot != "" {
		if pos, ok := e.RigAttachmentOriginCanvasPos(e.rig.selectedSlot); ok {
			if vecLen(p.Sub(pos)) <= rigOriginPickRadius {
				overlapsJoint := hasBone && vecLen(pos.Sub(boneOrigin)) <= 1
				if e.rig.preferAttachOrigin || !overlapsJoint {
					return RigHandleAttachmentOrigin
				}
			}
		}
	}
	if hasBone {
		if vecLen(p.Sub(boneOrigin)) <= rigJointPickRadius {
			return RigHandleBoneMove
		}
		if vecLen(p.Sub(boneTip)) <= rigTipPickRadius {
			return RigHandleBoneLength
		}
		if abs32(vecLen(p.Sub(boneOrigin))-boneRing) <= rigRingPickRadius {
			return RigHandleBoneRotate
		}
	}
	if e.rig.selectedSlot != "" {
		if pos, ok := e.RigAttachmentOriginCanvasPos(e.rig.selectedSlot); ok {
			if vecLen(p.Sub(pos)) <= rigOriginPickRadius {
				return RigHandleAttachmentOrigin
			}
		}
	}
	return RigHandleNone
}

// HitTestRigBone returns the name of the bone under rig canvas point (x, y), or "".
func (e *Editor) HitTestRigBone(x, y float32) string {
	if !e.RigOpen() || e.rig.skel == nil {
		return ""
	}
	p := math2d.Vec2{X: x, Y: y}
	bestName := ""
	bestDist := rigBoneHitDist
	// Walk in reverse so child bones drawn on top win ties.
	for i := len(e.rig.data.Bones) - 1; i >= 0; i-- {
		b := e.rig.data.Bones[i]
		origin, tip, _, ok := e.RigBoneHandlePos(b.Name)
		if !ok {
			continue
		}
		d := rigPointToSegmentDist(p, origin, tip)
		if d <= bestDist {
			bestDist = d
			bestName = b.Name
		}
	}
	return bestName
}

func rigPointToSegmentDist(p, a, b math2d.Vec2) float32 {
	ab := b.Sub(a)
	l2 := ab.X*ab.X + ab.Y*ab.Y
	if l2 <= 1e-6 {
		return vecLen(p.Sub(a))
	}
	t := ((p.X-a.X)*ab.X + (p.Y-a.Y)*ab.Y) / l2
	t = min(max(t, 0), 1)
	proj := math2d.Vec2{X: a.X + ab.X*t, Y: a.Y + ab.Y*t}
	return vecLen(p.Sub(proj))
}

// DraggingRig returns the active rig handle drag, or RigHandleNone.
func (e *Editor) DraggingRig() RigHandle {
	if !e.RigOpen() {
		return RigHandleNone
	}
	return e.rig.drag.handle
}

// BeginRigDrag begins dragging a rig handle at rig canvas point (x, y).
// If no handle of the current selection is hit, pressing on another bone selects it and starts moving it.
// Pass a handle directly via BeginRigHandleDrag to test or force a specific handle (such as RigHandleAttachmentOrigin when overlapping a bone joint).
func (e *Editor) BeginRigDrag(x, y float32) RigHandle {
	if !e.RigOpen() || e.playing || e.rig.drag.handle != RigHandleNone || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return RigHandleNone
	}
	h := e.PickRigHandle(x, y)
	if h == RigHandleNone {
		if hit := e.HitTestRigBone(x, y); hit != "" {
			e.rig.selectedBone = hit
			h = RigHandleBoneMove
		} else {
			return RigHandleNone
		}
	}
	return e.beginRigDragWithHandle(h, x, y)
}

// BeginRigHandleDrag begins dragging a specific handle h at rig canvas point (x, y),
// useful when the attachment origin handle sits on top of the selected bone's joint.
func (e *Editor) BeginRigHandleDrag(h RigHandle, x, y float32) RigHandle {
	if !e.RigOpen() || e.playing || e.rig.drag.handle != RigHandleNone || h == RigHandleNone || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return RigHandleNone
	}
	return e.beginRigDragWithHandle(h, x, y)
}

func (e *Editor) beginRigDragWithHandle(h RigHandle, x, y float32) RigHandle {
	if e.rig.skel == nil {
		return RigHandleNone
	}
	e.stopRigLoop()
	wx, wy := e.RigCanvasToWorld(x, y)
	st := rigDragState{
		handle:    h,
		startSnap: e.rig.snapshot(),
		grabWorld: math2d.Vec2{X: wx, Y: wy},
	}

	switch h {
	case RigHandleAttachmentOrigin:
		skinName, att, wb, ok := e.resolveRigSlotAttachment(e.rig.selectedSlot)
		if !ok {
			return RigHandleNone
		}
		st.slotName = e.rig.selectedSlot
		st.skinName = skinName
		st.startAttach = att
		st.attachWorld = composeBoneLocal(wb, att.X, att.Y, att.Rotation, att.ScaleX, att.ScaleY)
	case RigHandleBoneMove, RigHandleBoneRotate, RigHandleBoneLength:
		idx := findRigBone(e.rig.data, e.rig.selectedBone)
		if idx < 0 {
			return RigHandleNone
		}
		bd := e.rig.data.Bones[idx]
		st.boneName = bd.Name
		st.startBone = bd
		st.parentWorld = skeletal.BoneTransform{ScaleX: 1, ScaleY: 1}
		if bd.Parent != "" {
			if pw, ok := e.rig.skel.WorldBone(bd.Parent); ok {
				st.parentWorld = pw
			}
		}
		wb, _ := e.rig.skel.WorldBone(bd.Name)
		st.prevAngle = math.Atan2(float64(wy-wb.Y), float64(wx-wb.X))
		if e.rig.animation != "" && h != RigHandleBoneLength {
			st.animateMode = true
			st.animName = e.rig.animation
			st.animTime = e.rig.time
			// Capture current animated delta relative to bind pose.
			if lx, ly, lrot, _, _, ok := worldToBoneLocal(st.parentWorld, wb); ok {
				st.startTransKey = [2]float32{lx - bd.X, ly - bd.Y}
				st.startRotKey = lrot - bd.Rotation
			}
			// Preserve existing keyframe easing when editing at an existing timestamp.
			if a := e.rig.data.Animations[st.animName]; a != nil {
				if tl, ok := a.Bones[st.boneName]; ok {
					for _, k := range tl.Translate {
						if k.Time == st.animTime {
							st.startTransCurve = k.Curve
							st.startTransBezier = k.Bezier
							break
						}
					}
					for _, k := range tl.Rotate {
						if k.Time == st.animTime {
							st.startRotCurve = k.Curve
							st.startRotBezier = k.Bezier
							break
						}
					}
				}
			}
		}
	default:
		return RigHandleNone
	}

	e.rig.drag = st
	e.Status = h.String()
	e.notify()
	return h
}

// UpdateRigDrag updates the active rig handle drag to rig canvas point (x, y).
func (e *Editor) UpdateRigDrag(x, y float32) {
	if !e.RigOpen() || e.rig.drag.handle == RigHandleNone || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return
	}
	st := &e.rig.drag
	wx, wy := e.RigCanvasToWorld(x, y)

	switch st.handle {
	case RigHandleBoneMove:
		idx := findRigBone(e.rig.data, st.boneName)
		if idx < 0 {
			return
		}
		// Convert world delta into parent bone's local space.
		dx, dy := float64(wx-st.grabWorld.X), float64(wy-st.grabWorld.Y)
		sn, cs := math.Sincos(float64(-st.parentWorld.Rotation))
		rx := cs*dx - sn*dy
		ry := sn*dx + cs*dy
		if st.parentWorld.ScaleX == 0 || st.parentWorld.ScaleY == 0 {
			return
		}
		ldx := float32(rx / float64(st.parentWorld.ScaleX))
		ldy := float32(ry / float64(st.parentWorld.ScaleY))
		if !isFiniteFloat32(ldx) || !isFiniteFloat32(ldy) {
			return
		}
		if st.animateMode {
			if a := e.rig.data.Animations[st.animName]; a != nil {
				if a.Bones == nil {
					a.Bones = make(map[string]skeletal.BoneTimeline)
				}
				tl := a.Bones[st.boneName]
				tl.Translate = upsertVec2Key(tl.Translate, skeletal.Vec2Keyframe{
					Time:   st.animTime,
					X:      st.startTransKey[0] + ldx,
					Y:      st.startTransKey[1] + ldy,
					Curve:  st.startTransCurve,
					Bezier: st.startTransBezier,
				})
				a.Bones[st.boneName] = tl
				if st.animTime > a.Duration {
					a.Duration = st.animTime
				}
			}
		} else {
			e.rig.data.Bones[idx].X = st.startBone.X + ldx
			e.rig.data.Bones[idx].Y = st.startBone.Y + ldy
		}

	case RigHandleBoneRotate:
		idx := findRigBone(e.rig.data, st.boneName)
		if idx < 0 {
			return
		}
		wb, ok := e.rig.skel.WorldBone(st.boneName)
		if !ok {
			return
		}
		ox, oy := float64(wx-wb.X), float64(wy-wb.Y)
		if math.Hypot(ox, oy) < 1e-5 {
			return
		}
		ang := math.Atan2(oy, ox)
		step := math.Remainder(ang-st.prevAngle, 2*math.Pi)
		st.accumAngle += step
		st.prevAngle = ang
		delta := float32(st.accumAngle)
		if st.parentWorld.ScaleX*st.parentWorld.ScaleY < 0 {
			delta = -delta
		}
		if !isFiniteFloat32(delta) {
			return
		}
		if st.animateMode {
			if a := e.rig.data.Animations[st.animName]; a != nil {
				if a.Bones == nil {
					a.Bones = make(map[string]skeletal.BoneTimeline)
				}
				tl := a.Bones[st.boneName]
				tl.Rotate = upsertFloatKey(tl.Rotate, skeletal.FloatKeyframe{
					Time:   st.animTime,
					Value:  st.startRotKey + delta,
					Curve:  st.startRotCurve,
					Bezier: st.startRotBezier,
				})
				a.Bones[st.boneName] = tl
				if st.animTime > a.Duration {
					a.Duration = st.animTime
				}
			}
		} else {
			e.rig.data.Bones[idx].Rotation = st.startBone.Rotation + delta
		}

	case RigHandleBoneLength:
		idx := findRigBone(e.rig.data, st.boneName)
		if idx < 0 {
			return
		}
		wb, ok := e.rig.skel.WorldBone(st.boneName)
		if !ok {
			return
		}
		dx, dy := float64(wx-wb.X), float64(wy-wb.Y)
		sn, cs := math.Sincos(float64(wb.Rotation))
		proj := dx*cs + dy*sn
		scaleX := float64(wb.ScaleX)
		if math.Abs(scaleX) < 1e-6 {
			return
		}
		length := float32(max(proj/scaleX, 0))
		if !isFiniteFloat32(length) {
			return
		}
		e.rig.data.Bones[idx].Length = length

	case RigHandleAttachmentOrigin:
		sk := e.rig.data.Skins[st.skinName]
		if sk == nil {
			return
		}
		att, ok := sk[st.slotName]
		if !ok {
			return
		}
		dx, dy := float64(wx-st.grabWorld.X), float64(wy-st.grabWorld.Y)
		sn, cs := math.Sincos(float64(-st.attachWorld.Rotation))
		rx := cs*dx - sn*dy
		ry := sn*dx + cs*dy
		if st.attachWorld.ScaleX == 0 || st.attachWorld.ScaleY == 0 {
			return
		}
		// Moving the pivot handle right by +dx in frame space shifts OriginX by +dx / scaleX.
		dox := float32(rx / float64(st.attachWorld.ScaleX))
		doy := float32(ry / float64(st.attachWorld.ScaleY))
		if !isFiniteFloat32(dox) || !isFiniteFloat32(doy) {
			return
		}
		att.OriginX = st.startAttach.OriginX + dox
		att.OriginY = st.startAttach.OriginY + doy
		sk[st.slotName] = att
	}

	e.rig.dirty = true
	e.rebuildRigPreview()
	e.notify()
}

// EndRigDrag commits the active rig handle drag as a single undoable command on Editor.History.
func (e *Editor) EndRigDrag() bool {
	if !e.RigOpen() || e.rig.drag.handle == RigHandleNone {
		return false
	}
	st := e.rig.drag
	e.rig.drag = rigDragState{}
	before := st.startSnap
	after := e.rig.snapshot()
	after.dirty = true

	beforeBytes, errB := skeletal.Encode(before.data)
	afterBytes, errA := skeletal.Encode(after.data)
	if errA != nil || (errB == nil && string(beforeBytes) == string(afterBytes)) {
		e.applyRigState(before, "Ready")
		e.notify()
		return false
	}

	label := st.handle.String()
	if st.boneName != "" {
		label += " " + st.boneName
	} else if st.slotName != "" {
		label += " " + st.slotName
	}
	// Restore before-state so History.Execute applies after cleanly.
	e.applyRigState(before, label)
	e.History.Execute(&swapRigCmd{
		ed:     e,
		label:  label,
		before: before,
		after:  after,
	})
	return true
}

// CancelRigDrag aborts the active rig handle drag and restores the rig to its pre-drag state without recording history.
func (e *Editor) CancelRigDrag() bool {
	if !e.RigOpen() || e.rig.drag.handle == RigHandleNone {
		return false
	}
	st := e.rig.drag
	e.rig.drag = rigDragState{}
	if st.startSnap != nil {
		e.applyRigState(st.startSnap, "Cancelled "+st.handle.String())
		e.notify()
	}
	return true
}

// DrawRigViewport draws the open skeletal rig's attachments (via the runtime skeletal.Skeleton.Draw),
// bone hierarchy shapes, and interactive bone and attachment-origin handles into d.
func (e *Editor) DrawRigViewport(d *core.Drawing, size core.Size) {
	if d == nil || !e.RigOpen() {
		return
	}
	cw, ch := float32(size.Width), float32(size.Height)
	e.SetRigCanvasSize(cw, ch)

	spec := e.activePalette()
	// Background and subtle grid.
	d.Save()
	d.SetFillColor(spec.Viewport).FillRect(0, 0, cw, ch)

	ox, oy := e.RigWorldToCanvas(0, 0)
	z := e.rigZoom()
	step := 16 * z
	if step < 8 {
		step = 8
	}
	d.SetStrokeColor(spec.HoverOverlay).SetLineWidth(1)
	d.BeginPath()
	for x := ox - float32(math.Ceil(float64(ox/step)))*step; x <= cw; x += step {
		d.MoveTo(x, 0).LineTo(x, ch)
	}
	for y := oy - float32(math.Ceil(float64(oy/step)))*step; y <= ch; y += step {
		d.MoveTo(0, y).LineTo(cw, y)
	}
	d.Stroke()

	// Origin axes.
	d.SetStrokeColor(ui.Alpha(spec.AxisX, 0.35)).SetLineWidth(1)
	d.BeginPath().MoveTo(0, oy).LineTo(cw, oy).Stroke()
	d.SetStrokeColor(ui.Alpha(spec.AxisY, 0.35)).SetLineWidth(1)
	d.BeginPath().MoveTo(ox, 0).LineTo(ox, ch).Stroke()

	// Draw live runtime skeletal attachments.
	if e.rig.skel != nil {
		d.Save()
		d.Translate(cw/2, ch/2)
		d.Scale(z, z)
		d.Translate(-e.rig.viewX, -e.rig.viewY)
		e.rig.skel.Draw(d)
		d.Restore()
	}

	// Draw parent-child dashed connectors and tapered bone shapes.
	if e.rig.skel != nil {
		for _, b := range e.rig.data.Bones {
			wb, ok := e.rig.skel.WorldBone(b.Name)
			if !ok {
				continue
			}
			bx, by := e.RigWorldToCanvas(wb.X, wb.Y)
			if b.Parent != "" {
				if pw, pok := e.rig.skel.WorldBone(b.Parent); pok {
					px, py := e.RigWorldToCanvas(pw.X, pw.Y)
					d.SetStrokeColor(ui.Alpha(spec.TextFaint, 0.45)).SetLineWidth(1).SetLineDash(3, 3)
					d.BeginPath().MoveTo(px, py).LineTo(bx, by).Stroke()
					d.SetLineDash()
				}
			}

			selected := b.Name == e.rig.selectedBone
			fill := ui.Alpha(spec.Accent.Fg, 0.25)
			stroke := ui.Alpha(spec.Accent.Fg, 0.85)
			if selected {
				fill = ui.Alpha(spec.Warning.Fg, 0.35)
				stroke = spec.Warning.Fg
			}

			origin, tip, _, _ := e.RigBoneHandlePos(b.Name)
			if b.Length > 0 {
				tw := boneTipWorld(wb, b.Length)
				tx, ty := e.RigWorldToCanvas(tw.X, tw.Y)
				tVec := math2d.Vec2{X: tx, Y: ty}
				dir := tVec.Sub(origin)
				l := vecLen(dir)
				if l > 2 {
					u := dir.Scale(1 / l)
					perp := math2d.Vec2{X: -u.Y, Y: u.X}
					w := min(max(l*0.14, 3), 7)
					mid := origin.Add(u.Scale(min(l*0.25, 10)))
					p1 := mid.Add(perp.Scale(w))
					p2 := mid.Sub(perp.Scale(w))
					d.SetFillColor(fill).SetStrokeColor(stroke).SetLineWidth(1.5)
					d.BeginPath().MoveTo(origin.X, origin.Y).LineTo(p1.X, p1.Y).LineTo(tVec.X, tVec.Y).LineTo(p2.X, p2.Y).ClosePath().Fill().Stroke()
				}
			} else {
				// Zero-length bone drawn as a small directional nub.
				d.SetStrokeColor(stroke).SetLineWidth(1.5)
				d.BeginPath().MoveTo(origin.X, origin.Y).LineTo(tip.X, tip.Y).Stroke()
			}
			// Joint circle.
			d.SetFillColor(spec.Bg).SetStrokeColor(stroke).SetLineWidth(1.5)
			d.BeginPath().Ellipse(origin.X, origin.Y, 4.5, 4.5).Fill().Stroke()
		}

		// Draw interactive handles for the selected bone.
		if e.rig.selectedBone != "" {
			if origin, tip, ring, ok := e.RigBoneHandlePos(e.rig.selectedBone); ok {
				active := e.rig.drag.handle
				ringCol := ui.Alpha(spec.Accent.Fg, 0.75)
				if active == RigHandleBoneRotate {
					ringCol = spec.Warning.Fg
				}
				d.SetStrokeColor(ringCol).SetLineWidth(1.5)
				d.BeginPath().Arc(origin.X, origin.Y, ring, 0, 2*math.Pi, false).Stroke()

				moveCol := spec.TextPrimary
				if active == RigHandleBoneMove {
					moveCol = spec.Warning.Fg
				}
				d.SetFillColor(moveCol).SetStrokeColor(spec.Bg).SetLineWidth(1.5)
				d.BeginPath().Ellipse(origin.X, origin.Y, 5.5, 5.5).Fill().Stroke()

				tipCol := spec.Added.Fg
				if active == RigHandleBoneLength {
					tipCol = spec.Warning.Fg
				}
				const r float32 = 5
				d.SetFillColor(tipCol).SetStrokeColor(spec.Bg).SetLineWidth(1.5)
				d.BeginPath().
					MoveTo(tip.X, tip.Y-r).
					LineTo(tip.X+r, tip.Y).
					LineTo(tip.X, tip.Y+r).
					LineTo(tip.X-r, tip.Y).
					ClosePath().Fill().Stroke()
			}
		}

		// Draw selected slot's attachment frame outline and origin handle.
		if e.rig.selectedSlot != "" {
			if _, att, wb, ok := e.resolveRigSlotAttachment(e.rig.selectedSlot); ok {
				wt := composeBoneLocal(wb, att.X, att.Y, att.Rotation, att.ScaleX, att.ScaleY)
				if atlas := e.resolveRigAtlas(e.rig.atlasName); atlas != nil {
					if fr, exists := atlas.Frames[att.Frame]; exists && fr.Width > 0 && fr.Height > 0 {
						corners := [4][2]float32{
							{-att.OriginX, -att.OriginY},
							{fr.Width - att.OriginX, -att.OriginY},
							{fr.Width - att.OriginX, fr.Height - att.OriginY},
							{-att.OriginX, fr.Height - att.OriginY},
						}
						pts := make([]math2d.Vec2, 4)
						for i, c := range corners {
							pt := composeBoneLocal(wt, c[0], c[1], 0, 1, 1)
							px, py := e.RigWorldToCanvas(pt.X, pt.Y)
							pts[i] = math2d.Vec2{X: px, Y: py}
						}
						d.SetStrokeColor(ui.Alpha(spec.Agent.Fg, 0.85)).SetLineWidth(1).SetLineDash(4, 3)
						strokePolygon(d, pts)
						d.SetLineDash()
					}
				}
				px, py := e.RigWorldToCanvas(wt.X, wt.Y)
				origCol := spec.Agent.Fg
				if e.rig.drag.handle == RigHandleAttachmentOrigin {
					origCol = spec.Warning.Fg
				}
				const arm float32 = 8
				d.SetStrokeColor(origCol).SetLineWidth(1.5)
				d.BeginPath().
					MoveTo(px-arm, py).LineTo(px+arm, py).
					MoveTo(px, py-arm).LineTo(px, py+arm).
					Stroke()
				d.SetFillColor(origCol)
				d.BeginPath().Ellipse(px, py, 3, 3).Fill()
			}
		}
	}

	d.Restore()
}

// RigBoneSummary returns a formatted readout for the rig status bar.
func (e *Editor) RigBoneSummary() string {
	if !e.RigOpen() {
		return ""
	}
	b := e.rig.selectedBone
	if b == "" {
		return fmt.Sprintf("Bones: %d | Slots: %d", len(e.rig.data.Bones), len(e.rig.data.Slots))
	}
	if idx := findRigBone(e.rig.data, b); idx >= 0 {
		bd := e.rig.data.Bones[idx]
		return fmt.Sprintf("Bone %s (%.1f, %.1f) rot %.2f len %.1f", bd.Name, bd.X, bd.Y, bd.Rotation, bd.Length)
	}
	return ""
}
