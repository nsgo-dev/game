package editor

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// ViewProj returns the right-handed Y-up [0, 1] NDC view-projection matrix
// matching OrbitCamera3D.SceneCamera (perspective or orthographic) for a
// viewport of size w x h.
func (c OrbitCamera3D) ViewProj(w, h float32) math3d.Mat4 {
	if w <= 0 || h <= 0 {
		w, h = 640, 360
	}
	return c.SceneCamera(w, h).ViewProj()
}

type viewport3DState struct {
	queue     render3d.Queue
	occ       *render3d.OcclusionBuffer
	items     []render3d.DrawItem
	occluders []render3d.Occluder

	planValid     bool
	planGen       uint64
	planDocRev    uint64
	planImpRev    uint64
	planCam       OrbitCamera3D
	planW, planH  float32
	planHiddenSig uint64
	planClipTime  int64
	planPlaying   bool

	stats            render3d.RenderStats
	lodByObject      map[string]int
	lodCountByObject map[string]int
	occludedObjects  map[string]bool
	objectBounds     map[string]math3d.AABB
}

func (e *Editor) invalidate3DBuild() {
	if e == nil {
		return
	}
	e.s3d.files = nil
	e.s3d.stale = true
	e.vp3d.planValid = false
	e.vp3d.queue.ResetLODHistory()
}

func (e *Editor) hiddenSignature3D() uint64 {
	if len(e.hidden) == 0 {
		return 0
	}
	keys := make([]string, 0, len(e.hidden))
	for k, v := range e.hidden {
		if v {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	h := fnv.New64a()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// ensureViewport3DPlan evaluates scenemodel LOD selection and occlusion culling
// against Camera3D at viewport size (w, h) using the shared Scene3D build,
// caching the result until scene, sidecars, camera, viewport size, or
// visibility changes.
func (e *Editor) ensureViewport3DPlan(w, h float32) {
	if e == nil {
		return
	}
	if w <= 0 || h <= 0 {
		w, h = e.CanvasSize()
		if w <= 0 || h <= 0 {
			w, h = e.ViewportSize()
		}
	}
	s3 := e.scene3D()
	built := s3.built
	if e.vp3d.planGen != s3.gen {
		e.vp3d.planValid = false
		e.vp3d.queue.ResetLODHistory()
	}
	hiddenSig := e.hiddenSignature3D()
	clipTime := int64(-1)
	if e.clipScene != nil {
		clipTime = int64(e.clipTime)
	}
	if e.vp3d.planValid &&
		e.vp3d.planGen == s3.gen &&
		e.vp3d.planDocRev == e.revision &&
		e.vp3d.planImpRev == e.importRev &&
		e.vp3d.planCam == e.Camera3D &&
		e.vp3d.planW == w &&
		e.vp3d.planH == h &&
		e.vp3d.planHiddenSig == hiddenSig &&
		e.vp3d.planClipTime == clipTime &&
		e.vp3d.planPlaying == e.playing {
		return
	}

	e.vp3d.planValid = true
	e.vp3d.planGen = s3.gen
	e.vp3d.planDocRev = e.revision
	e.vp3d.planImpRev = e.importRev
	e.vp3d.planCam = e.Camera3D
	e.vp3d.planW = w
	e.vp3d.planH = h
	e.vp3d.planHiddenSig = hiddenSig
	e.vp3d.planClipTime = clipTime
	e.vp3d.planPlaying = e.playing

	if e.vp3d.lodByObject == nil {
		e.vp3d.lodByObject = make(map[string]int)
	} else {
		clear(e.vp3d.lodByObject)
	}
	if e.vp3d.lodCountByObject == nil {
		e.vp3d.lodCountByObject = make(map[string]int)
	} else {
		clear(e.vp3d.lodCountByObject)
	}
	if e.vp3d.occludedObjects == nil {
		e.vp3d.occludedObjects = make(map[string]bool)
	} else {
		clear(e.vp3d.occludedObjects)
	}
	if e.vp3d.objectBounds == nil {
		e.vp3d.objectBounds = make(map[string]math3d.AABB)
	} else {
		clear(e.vp3d.objectBounds)
	}

	if built == nil {
		e.vp3d.stats = render3d.RenderStats{}
		return
	}

	// Track LOD level counts and world bounds per scene object.
	for _, d := range built.Drawables {
		for pi := range d.Parts {
			if n := len(d.Parts[pi].LODs); n > e.vp3d.lodCountByObject[d.Object] {
				e.vp3d.lodCountByObject[d.Object] = n
			}
		}
		if d.Node.EffectiveVisible() && d.Node.EffectiveEnabled() {
			wb := d.Node.LocalBounds.Transform(d.Node.WorldMatrix())
			if prev, ok := e.vp3d.objectBounds[d.Object]; ok {
				e.vp3d.objectBounds[d.Object] = prev.Union(wb)
			} else {
				e.vp3d.objectBounds[d.Object] = wb
			}
		}
	}

	viewProj := e.Camera3D.ViewProj(w, h)
	eye3 := e.Camera3D.Eye()
	camPos := math3d.Vec3{X: eye3[0], Y: eye3[1], Z: eye3[2]}

	e.vp3d.queue.LODHysteresis = built.LODHysteresis
	e.vp3d.items = built.AppendDrawItems(e.vp3d.items[:0])

	var occ *render3d.OcclusionBuffer
	if len(built.Occluders) > 0 {
		if e.vp3d.occ == nil {
			e.vp3d.occ = render3d.NewOcclusionBuffer(0, 0)
		}
		e.vp3d.occluders = built.AppendOccluders(e.vp3d.occluders[:0])
		e.vp3d.occ.Clear()
		if len(e.vp3d.occluders) > 0 {
			e.vp3d.occ.Rasterize(viewProj, e.vp3d.occluders...)
			e.vp3d.occ.BuildPyramid()
			occ = e.vp3d.occ
		}
	}

	plan := e.vp3d.queue.BuildWithOcclusion(viewProj, camPos, e.vp3d.items, occ)
	e.vp3d.stats = plan.Stats

	visibleObjects := make(map[string]bool)
	recordVisible := func(draws []render3d.ResolvedDraw) {
		for _, rd := range draws {
			d, _, ok := built.Part(rd.ID)
			if !ok {
				continue
			}
			visibleObjects[d.Object] = true
			if rd.LODIndex >= 0 {
				if prev, exists := e.vp3d.lodByObject[d.Object]; !exists || rd.LODIndex < prev {
					e.vp3d.lodByObject[d.Object] = rd.LODIndex
				}
			}
		}
	}
	recordVisible(plan.Opaque)
	recordVisible(plan.Transparent)

	if occ != nil {
		frustum := math3d.FrustumFromMat4NO(viewProj)
		for i := range e.vp3d.items {
			it := &e.vp3d.items[i]
			if !frustum.IntersectsAABB(it.WorldBounds) {
				continue
			}
			if !occ.TestAABB(it.WorldBounds, viewProj) {
				if d, _, ok := built.Part(it.ID); ok && !visibleObjects[d.Object] {
					e.vp3d.occludedObjects[d.Object] = true
				}
			}
		}
	}
}

// SetShowLODOverlay toggles per-object active LOD labels and color-coding in the 3D viewport.
func (e *Editor) SetShowLODOverlay(show bool) {
	if e == nil || e.ShowLODOverlay == show {
		return
	}
	e.ShowLODOverlay = show
	e.notify()
}

// SetShowOccluders toggles occluder volume wireframes in the 3D viewport.
func (e *Editor) SetShowOccluders(show bool) {
	if e == nil || e.ShowOccluders == show {
		return
	}
	e.ShowOccluders = show
	e.notify()
}

// SetShowRenderStats toggles the RenderStats HUD overlay in the 3D viewport.
func (e *Editor) SetShowRenderStats(show bool) {
	if e == nil || e.ShowRenderStats == show {
		return
	}
	e.ShowRenderStats = show
	e.notify()
}

// RenderStats3D returns the latest render3d.RenderStats evaluated for the 3D
// viewport at the current Camera3D and canvas size.
func (e *Editor) RenderStats3D() render3d.RenderStats {
	if e == nil {
		return render3d.RenderStats{}
	}
	w, h := e.CanvasSize()
	e.ensureViewport3DPlan(w, h)
	return e.vp3d.stats
}

// ObjectLOD3D returns the currently selected LOD index (0 for finest, 1 for
// _LOD1, ...) drawn for scene object id in the 3D viewport, or (-1, false) when
// id has no LODs or is not currently visible.
func (e *Editor) ObjectLOD3D(id string) (int, bool) {
	if e == nil || id == "" {
		return -1, false
	}
	w, h := e.CanvasSize()
	e.ensureViewport3DPlan(w, h)
	lv, ok := e.vp3d.lodByObject[id]
	if !ok {
		return -1, false
	}
	return lv, true
}

// ObjectLODCount3D returns how many LOD levels scene object id has in the built
// 3D scene (0 when it has only one detail level or no 3D geometry).
func (e *Editor) ObjectLODCount3D(id string) int {
	if e == nil || id == "" {
		return 0
	}
	w, h := e.CanvasSize()
	e.ensureViewport3DPlan(w, h)
	return e.vp3d.lodCountByObject[id]
}

// ObjectOccluded3D reports whether scene object id is currently culled behind
// an occluder in the 3D viewport.
func (e *Editor) ObjectOccluded3D(id string) bool {
	if e == nil || id == "" {
		return false
	}
	w, h := e.CanvasSize()
	e.ensureViewport3DPlan(w, h)
	return e.vp3d.occludedObjects[id]
}

// SetObjectOccluder sets or clears (when nil) object id's 3D occluder definition
// through History. Setting an occluder on a version 1 scene raises it to version 2.
func (e *Editor) SetObjectOccluder(id string, occ *sceneformat.OccluderDoc) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Set occluder of "+id, 2, func(ov *sceneformat.ObjectOverride, resolved, base *sceneformat.ObjectDoc) error {
				if occluderEqual(resolved.Occluder, occ) {
					return nil
				}
				if occ == nil {
					if base != nil && base.Occluder != nil {
						return fmt.Errorf("editor: cannot clear inherited occluder on %q; revert override or edit the source scene", id)
					}
					ov.Occluder = nil
					return nil
				}
				probe := resolved.Clone()
				probe.Occluder = cloneOccluderDoc(occ)
				if err := sceneformat.CheckObjectModel(&probe); err != nil {
					return err
				}
				ov.Occluder = cloneOccluderDoc(occ)
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", id)
	}
	probe := obj.Clone()
	probe.Occluder = cloneOccluderDoc(occ)
	if err := sceneformat.CheckObjectModel(&probe); err != nil {
		return err
	}
	if occluderEqual(obj.Occluder, occ) {
		return nil
	}
	e.History.Execute(&occluderObjectCmd{
		ed:          e,
		id:          id,
		oldOcc:      cloneOccluderDoc(obj.Occluder),
		newOcc:      cloneOccluderDoc(occ),
		prevVersion: e.Doc.Version,
	})
	return nil
}

type occluderObjectCmd struct {
	ed          *Editor
	id          string
	oldOcc      *sceneformat.OccluderDoc
	newOcc      *sceneformat.OccluderDoc
	prevVersion int
}

func (c *occluderObjectCmd) Label() string { return "Set occluder of " + c.id }

func (c *occluderObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		if c.newOcc != nil && c.ed.Doc.Version < sceneformat.CurrentVersion {
			c.ed.Doc.Version = sceneformat.CurrentVersion
		}
		obj.Occluder = cloneOccluderDoc(c.newOcc)
		c.ed.Status = c.Label()
	}
}

func (c *occluderObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Occluder = cloneOccluderDoc(c.oldOcc)
		if c.prevVersion > 0 {
			c.ed.Doc.Version = c.prevVersion
		}
	}
}

func cloneOccluderDoc(o *sceneformat.OccluderDoc) *sceneformat.OccluderDoc {
	if o == nil {
		return nil
	}
	c := *o
	if o.Center != nil {
		v := *o.Center
		c.Center = &v
	}
	if o.Size != nil {
		v := *o.Size
		c.Size = &v
	}
	return &c
}

func occluderEqual(a, b *sceneformat.OccluderDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Mode != b.Mode || a.DoubleSided != b.DoubleSided {
		return false
	}
	if (a.Center == nil) != (b.Center == nil) || (a.Center != nil && *a.Center != *b.Center) {
		return false
	}
	if (a.Size == nil) != (b.Size == nil) || (a.Size != nil && *a.Size != *b.Size) {
		return false
	}
	return true
}

func hasOccluder(o *sceneformat.ObjectDoc) bool {
	if o.Occluder != nil {
		return true
	}
	for i := range o.Children {
		if hasOccluder(&o.Children[i]) {
			return true
		}
	}
	return false
}

// lodColor3D returns a distinct color for each LOD index so level transitions
// stand out clearly in the 3D viewport.
func lodColor3D(level int) core.Color {
	switch level {
	case 0:
		return core.RGBA(0x22C55E, 0.95) // green: LOD0 (finest)
	case 1:
		return core.RGBA(0xF59E0B, 0.95) // amber: LOD1
	case 2:
		return core.RGBA(0xF97316, 0.95) // orange: LOD2
	default:
		return core.RGBA(0xEF4444, 0.95) // red: LOD3+
	}
}

// drawObjectLODBadge3D draws a compact "LOD<n>" label at the projected center of
// an object with multiple LOD levels.
func (e *Editor) drawObjectLODBadge3D(d *core.Drawing, obj *sceneformat.ObjectDoc, lod int, w, h float32) {
	pos := e.worldPosition3D(obj)
	if wb, ok := e.vp3d.objectBounds[obj.ID]; ok && !wb.Empty() {
		c := wb.Center()
		pos = [3]float32{c.X, wb.Max.Y, c.Z}
	}
	sx, sy, _, vis := e.Camera3D.Project(pos, w, h)
	if !vis {
		return
	}
	label := fmt.Sprintf("LOD%d", lod)
	col := lodColor3D(lod)
	d.Save().
		SetFillColor(core.RGBA(0x0F172A, 0.78)).
		FillRect(sx-18, sy-15, 36, 14).
		SetFillColor(col).
		FillText(label, sx-14, sy-4, core.Font{Size: 10}).
		Restore()
}

// drawOccluders3D draws visible occluder volumes (boxes and model occluder
// triangle wireframes) in magenta so authors can inspect occlusion walls.
func (e *Editor) drawOccluders3D(d *core.Drawing, w, h float32) {
	if !e.ShowOccluders || d == nil || e.Doc == nil {
		return
	}
	occColor := core.RGBA(0xD946EF, 0.95)
	occLabelBg := core.RGBA(0x2E1065, 0.78)

	// Track which object IDs were drawn from built scenemodel.Occluders so
	// fallback doc walking does not double-draw them.
	drawnFromBuilt := make(map[string]bool)
	if e.s3d.built != nil {
		for i := range e.s3d.built.Occluders {
			occ := &e.s3d.built.Occluders[i]
			if occ.Node == nil || !occ.Node.EffectiveVisible() || !occ.Node.EffectiveEnabled() {
				continue
			}
			drawnFromBuilt[occ.Object] = true
			ro := occ.RenderOccluder()
			e.drawRenderOccluder3D(d, ro, occ.Mode == sceneformat.OccluderBox, occColor, occLabelBg, w, h)
		}
	}

	// Fallback for box occluders when built3D failed (e.g. an unrelated missing model).
	for li := range e.Doc.Layers {
		layer := &e.Doc.Layers[li]
		if !layer.IsVisible() || layer.EffectiveSpace() == sceneformat.SpaceScreen {
			continue
		}
		var walk func(objs []sceneformat.ObjectDoc, parentWorld math3d.Transform)
		walk = func(objs []sceneformat.ObjectDoc, parentWorld math3d.Transform) {
			for oi := range objs {
				o := &objs[oi]
				if e.hidden[o.ID] {
					continue
				}
				posed := e.posed3D(o)
				world := parentWorld.Mul(sceneformat.ObjectLocalTransform3D(posed))
				if posed.Occluder != nil && posed.Occluder.Mode == sceneformat.OccluderBox && !drawnFromBuilt[o.ID] {
					lo, hi := posed.Occluder.LocalBounds()
					aabb := math3d.AABB{
						Min: math3d.Vec3{X: lo[0], Y: lo[1], Z: lo[2]},
						Max: math3d.Vec3{X: hi[0], Y: hi[1], Z: hi[2]},
					}
					ro := render3d.NewBoxOccluder(aabb, world.ToMat4())
					e.drawRenderOccluder3D(d, ro, true, occColor, occLabelBg, w, h)
				}
				if len(o.Children) > 0 {
					walk(o.Children, world)
				}
			}
		}
		walk(layer.Objects, math3d.TransformIdentity())
	}
}

func (e *Editor) drawRenderOccluder3D(d *core.Drawing, ro render3d.Occluder, isBox bool, col, labelBg core.Color, w, h float32) {
	if len(ro.Vertices) < 3 || len(ro.Indices) < 3 {
		return
	}
	xf := ro.Transform
	if xf == (math3d.Mat4{}) {
		xf = math3d.Mat4Identity()
	}

	type edgePair struct{ a, b uint32 }
	const maxEdges = 1536
	seen := make(map[edgePair]struct{}, min(len(ro.Indices), maxEdges))

	d.Save().SetStrokeColor(col).SetLineWidth(2.0).BeginPath()
	hasSeg := false
	triCount := len(ro.Indices) / 3
	for t := 0; t < triCount && len(seen) < maxEdges; t++ {
		i0, i1, i2 := ro.Indices[t*3], ro.Indices[t*3+1], ro.Indices[t*3+2]
		if int(i0) >= len(ro.Vertices) || int(i1) >= len(ro.Vertices) || int(i2) >= len(ro.Vertices) {
			continue
		}
		pairs := [3][2]uint32{{i0, i1}, {i1, i2}, {i2, i0}}
		for _, pr := range pairs {
			a, b := pr[0], pr[1]
			if a > b {
				a, b = b, a
			}
			key := edgePair{a, b}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			pa := xf.TransformPoint(ro.Vertices[a])
			pb := xf.TransformPoint(ro.Vertices[b])
			x0, y0, x1, y1, vis := e.Camera3D.projectSegment(
				[3]float32{pa.X, pa.Y, pa.Z},
				[3]float32{pb.X, pb.Y, pb.Z},
				w, h,
			)
			if !vis {
				continue
			}
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasSeg = true
		}
	}
	if hasSeg {
		d.Stroke()
	}
	d.Restore()

	// Draw a compact "OCC" tag at the occluder's world center so it stands out immediately.
	if !ro.Bounds.Empty() {
		c := ro.Bounds.Center()
		if sx, sy, _, vis := e.Camera3D.Project([3]float32{c.X, c.Y, c.Z}, w, h); vis {
			tag := "OCC"
			if isBox {
				tag = "OCC BOX"
			}
			tw := float32(len(tag)*6 + 8)
			d.Save().
				SetFillColor(labelBg).
				FillRect(sx-tw*0.5, sy-10, tw, 14).
				SetFillColor(col).
				FillText(tag, sx-tw*0.5+4, sy+1, core.Font{Size: 10}).
				Restore()
		}
	}
}

func (e *Editor) renderStats3DText() string {
	stats := e.RenderStats3D()
	var b strings.Builder
	fmt.Fprintf(&b, "Draws: %d | Visible: %d/%d | Occluded: %d | Tris: %d | Verts: %d",
		stats.TotalDrawCalls, stats.VisibleItems, stats.TotalItems, stats.Occluded, stats.Triangles, stats.Vertices)
	if len(e.vp3d.lodByObject) > 0 {
		ids := make([]string, 0, len(e.vp3d.lodByObject))
		for id := range e.vp3d.lodByObject {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		b.WriteString("\nLOD:")
		for _, id := range ids {
			fmt.Fprintf(&b, " %s=LOD%d", id, e.vp3d.lodByObject[id])
		}
	}
	return b.String()
}

// viewport3DRenderStatsOverlay renders a non-blocking RenderStats + LOD HUD card
// in the bottom-right corner of the 3D viewport (clear of the top bar, orientation widget, and bottom center pill).
func viewport3DRenderStatsOverlay(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.ViewMode == ViewMode3D && ed.ShowRenderStats
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "viewport3d-render-stats-panel",
				Style: core.Style{
					Position:      core.Absolute,
					Bottom:        core.Pt(60),
					Right:         core.Pt(16),
					ZIndex:        10,
					PointerEvents: core.PointerEventsNone,
					Background:    specViewportHUDSoftBg,
					Borders:       core.All(1),
					BorderColor:   specLineRaised,
					CornerRadius:  8,
					Padding:       core.Symmetric(5, 8),
				},
				Children: []core.Node{
					core.Text{
						TestID: "viewport3d-render-stats",
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.renderStats3DText()
						},
						Style: core.Style{
							FontSize: 11,
							Color:    specText,
						},
					},
				},
			}
		},
	}
}

func isFiniteFloat32Slice(vs []float32) bool {
	for _, v := range vs {
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	return true
}
