package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// ShapeHandleKind identifies what part of a component shape field a 2D viewport handle edits.
type ShapeHandleKind int

const (
	// ShapeHandleNone is no handle.
	ShapeHandleNone ShapeHandleKind = iota
	// ShapeHandlePoint moves a FieldPoint's (x, y).
	ShapeHandlePoint
	// ShapeHandleRectCenter moves a FieldRect's position without resizing it.
	ShapeHandleRectCenter
	// ShapeHandleRectN moves the top edge of a FieldRect.
	ShapeHandleRectN
	// ShapeHandleRectE moves the right edge of a FieldRect.
	ShapeHandleRectE
	// ShapeHandleRectS moves the bottom edge of a FieldRect.
	ShapeHandleRectS
	// ShapeHandleRectW moves the left edge of a FieldRect.
	ShapeHandleRectW
	// ShapeHandleRectNE moves the top-right corner of a FieldRect.
	ShapeHandleRectNE
	// ShapeHandleRectSE moves the bottom-right corner of a FieldRect.
	ShapeHandleRectSE
	// ShapeHandleRectSW moves the bottom-left corner of a FieldRect.
	ShapeHandleRectSW
	// ShapeHandleRectNW moves the top-left corner of a FieldRect.
	ShapeHandleRectNW
	// ShapeHandleCircleCenter moves a FieldCircle's center.
	ShapeHandleCircleCenter
	// ShapeHandleCircleRadius adjusts a FieldCircle's radius from a cardinal rim handle.
	ShapeHandleCircleRadius
	// ShapeHandlePolygonVertex moves vertex Index of a FieldPolygon.
	ShapeHandlePolygonVertex
	// ShapeHandlePolygonMidpoint inserts a new vertex on the edge from Index to (Index+1)%N of a FieldPolygon.
	ShapeHandlePolygonMidpoint
)

// String returns a human-readable name for k.
func (k ShapeHandleKind) String() string {
	switch k {
	case ShapeHandlePoint:
		return "Point"
	case ShapeHandleRectCenter:
		return "Rect Center"
	case ShapeHandleRectN:
		return "Rect N"
	case ShapeHandleRectE:
		return "Rect E"
	case ShapeHandleRectS:
		return "Rect S"
	case ShapeHandleRectW:
		return "Rect W"
	case ShapeHandleRectNE:
		return "Rect NE"
	case ShapeHandleRectSE:
		return "Rect SE"
	case ShapeHandleRectSW:
		return "Rect SW"
	case ShapeHandleRectNW:
		return "Rect NW"
	case ShapeHandleCircleCenter:
		return "Circle Center"
	case ShapeHandleCircleRadius:
		return "Circle Radius"
	case ShapeHandlePolygonVertex:
		return "Polygon Vertex"
	case ShapeHandlePolygonMidpoint:
		return "Polygon Midpoint"
	default:
		return "None"
	}
}

// ShapeHandle2D describes one interactive handle on a component shape field of the selected object.
type ShapeHandle2D struct {
	ObjectID  string
	Component string
	Field     string
	Kind      ShapeHandleKind
	// Index is the vertex index for ShapeHandlePolygonVertex, the starting vertex index for ShapeHandlePolygonMidpoint, or 0..3 for cardinal circle radius handles (+X, +Y, -X, -Y).
	Index int
	// Pos is the handle's position in canvas points.
	Pos math2d.Vec2
}

// DrawnShape2D describes one component shape field rendered in the 2D viewport.
type DrawnShape2D struct {
	ObjectID  string
	Component string
	Field     string
	Kind      sceneformat.FieldKind
	Selected  bool
	Value     any
	// Points holds the shape's canvas-space control points: 1 point for FieldPoint, 4 corners for FieldRect, center + rim sample points for FieldCircle, or all vertices for FieldPolygon.
	Points []math2d.Vec2
}

const (
	shapeHandleHalf float32 = 5
	shapeHandlePick float32 = 7
	shapeMidHalf    float32 = 4
	circleSegments          = 48
	minPolygonVerts         = 3
)

type shapeDragState struct {
	active       bool
	objectID     string
	component    string
	field        string
	space        sceneformat.Space
	handle       ShapeHandle2D
	hadComps     bool
	compExisted  bool
	fieldExisted bool
	origVal      any
	startShape   any
	current      any
	grabLocal    math2d.Vec2
	inserted     bool
	changed      bool
}

// SetShowAllShapes toggles whether the 2D viewport draws shape component fields of all visible objects (true) or only the selected object (false).
func (e *Editor) SetShowAllShapes(show bool) {
	if e == nil || e.ShowAllShapes == show {
		return
	}
	e.ShowAllShapes = show
	e.notify()
}

func (e *Editor) shapeFieldValue(obj *sceneformat.ObjectDoc, compName string, f sceneformat.Field) (any, bool) {
	if obj == nil {
		return nil, false
	}
	rawComp, ok := obj.Components[compName]
	if !ok {
		return nil, false
	}
	var val any
	if m, ok := rawComp.(map[string]any); ok {
		if v, exists := m[f.Name]; exists {
			val = v
		}
	}
	if val == nil {
		val = f.Default
	}
	switch f.Kind {
	case sceneformat.FieldPoint:
		p, err := sceneformat.ParsePoint(val)
		if err != nil {
			return nil, false
		}
		return p, true
	case sceneformat.FieldRect:
		r, err := sceneformat.ParseRect(val)
		if err != nil {
			return nil, false
		}
		return r, true
	case sceneformat.FieldCircle:
		c, err := sceneformat.ParseCircle(val)
		if err != nil {
			return nil, false
		}
		return c, true
	case sceneformat.FieldPolygon:
		pts, err := sceneformat.ParsePolygon(val)
		if err != nil {
			return nil, false
		}
		return pts, true
	default:
		return nil, false
	}
}

type objectShapeEntry struct {
	bo        *sceneformat.BuiltObject
	space     sceneformat.Space
	toCanvas  math2d.Affine
	component string
	field     sceneformat.Field
	value     any
	selected  bool
}

func (e *Editor) collectVisibleShapes(scene *sceneformat.BuiltScene) []objectShapeEntry {
	if e == nil || scene == nil || e.playing || e.ViewMode != ViewMode2D {
		return nil
	}
	var out []objectShapeEntry
	visitObj := func(bo *sceneformat.BuiltObject, space sceneformat.Space, selected bool) {
		if bo == nil || bo.Doc == nil || len(bo.Doc.Components) == 0 {
			return
		}
		m := e.layerAffine(space).Mul(bo.World)
		for _, compName := range sortedMapKeys(bo.Doc.Components) {
			ct, ok := e.lookupComponentType(compName)
			if !ok {
				continue
			}
			for _, f := range ct.Fields {
				if !f.Kind.IsShape() {
					continue
				}
				val, ok := e.shapeFieldValue(bo.Doc, compName, f)
				if !ok {
					continue
				}
				out = append(out, objectShapeEntry{
					bo:        bo,
					space:     space,
					toCanvas:  m,
					component: compName,
					field:     f,
					value:     val,
					selected:  selected,
				})
			}
		}
	}

	if !e.ShowAllShapes {
		if e.SelectedID == "" {
			return nil
		}
		bo := scene.Object(e.SelectedID)
		if bo == nil || viewFlagged(e.hidden, bo) {
			return nil
		}
		space := sceneformat.SpaceWorld
		if layer := scene.Layer(bo.Layer); layer != nil {
			if !layer.Visible {
				return nil
			}
			space = layer.Space
		}
		visitObj(bo, space, true)
		return out
	}

	// Draw non-selected objects first, then the selected object last so it renders on top.
	var selectedBO *sceneformat.BuiltObject
	var selectedSpace sceneformat.Space
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		var walk func(objs []*sceneformat.BuiltObject, hidden bool)
		walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
			for _, bo := range objs {
				h := hidden || e.hidden[bo.ID]
				if !h {
					if bo.ID == e.SelectedID {
						selectedBO = bo
						selectedSpace = layer.Space
					} else {
						visitObj(bo, layer.Space, false)
					}
				}
				walk(bo.Children, h)
			}
		}
		walk(layer.Objects, false)
	}
	if selectedBO != nil {
		visitObj(selectedBO, selectedSpace, true)
	}
	return out
}

// VisibleShapes2D returns the component shapes currently drawn in the 2D viewport in draw order.
func (e *Editor) VisibleShapes2D() []DrawnShape2D {
	scene := e.ActiveScene()
	entries := e.collectVisibleShapes(scene)
	if len(entries) == 0 {
		return nil
	}
	out := make([]DrawnShape2D, 0, len(entries))
	for _, ent := range entries {
		var pts []math2d.Vec2
		switch v := ent.value.(type) {
		case sceneformat.Point:
			pts = []math2d.Vec2{ent.toCanvas.TransformPoint(math2d.Vec2{X: float32(v.X), Y: float32(v.Y)})}
		case sceneformat.Rect:
			x0, y0 := float32(v.X), float32(v.Y)
			x1, y1 := float32(v.X+v.Width), float32(v.Y+v.Height)
			pts = []math2d.Vec2{
				ent.toCanvas.TransformPoint(math2d.Vec2{X: x0, Y: y0}),
				ent.toCanvas.TransformPoint(math2d.Vec2{X: x1, Y: y0}),
				ent.toCanvas.TransformPoint(math2d.Vec2{X: x1, Y: y1}),
				ent.toCanvas.TransformPoint(math2d.Vec2{X: x0, Y: y1}),
			}
		case sceneformat.Circle:
			pts = circleCanvasPoints(ent.toCanvas, v)
		case []sceneformat.Point:
			pts = make([]math2d.Vec2, len(v))
			for i, p := range v {
				pts[i] = ent.toCanvas.TransformPoint(math2d.Vec2{X: float32(p.X), Y: float32(p.Y)})
			}
		}
		out = append(out, DrawnShape2D{
			ObjectID:  ent.bo.ID,
			Component: ent.component,
			Field:     ent.field.Name,
			Kind:      ent.field.Kind,
			Selected:  ent.selected,
			Value:     cloneAny(ent.value),
			Points:    pts,
		})
	}
	return out
}

func circleCanvasPoints(m math2d.Affine, c sceneformat.Circle) []math2d.Vec2 {
	if c.Radius <= 0 {
		return []math2d.Vec2{m.TransformPoint(math2d.Vec2{X: float32(c.X), Y: float32(c.Y)})}
	}
	pts := make([]math2d.Vec2, circleSegments)
	for i := range circleSegments {
		angle := 2 * math.Pi * float64(i) / circleSegments
		lx := float32(c.X + c.Radius*math.Cos(angle))
		ly := float32(c.Y + c.Radius*math.Sin(angle))
		pts[i] = m.TransformPoint(math2d.Vec2{X: lx, Y: ly})
	}
	return pts
}

func (e *Editor) shapeHandlesEnabled() bool {
	return e != nil && !e.playing && !e.HistoryPreviewActive() && e.ViewMode == ViewMode2D &&
		(e.Tool == ToolSelect || e.Tool == ToolMove) &&
		e.SelectedID != "" && !e.viewportFrozen(e.SelectedID)
}

// ShapeHandles2D returns all interactive 2D viewport handles for the selected object's component shape fields.
func (e *Editor) ShapeHandles2D() []ShapeHandle2D {
	if !e.shapeHandlesEnabled() {
		return nil
	}
	scene := e.ActiveScene()
	if scene == nil {
		return nil
	}
	bo := scene.Object(e.SelectedID)
	if bo == nil || bo.Doc == nil || len(bo.Doc.Components) == 0 {
		return nil
	}
	space := sceneformat.SpaceWorld
	if layer := scene.Layer(bo.Layer); layer != nil {
		if !layer.Visible {
			return nil
		}
		space = layer.Space
	}
	m := e.layerAffine(space).Mul(bo.World)
	originCanvas := m.TransformPoint(math2d.Vec2{})
	centerMinDist := gizmoCenterHalf + shapeHandleHalf

	var out []ShapeHandle2D
	for _, compName := range sortedMapKeys(bo.Doc.Components) {
		ct, ok := e.lookupComponentType(compName)
		if !ok {
			continue
		}
		for _, f := range ct.Fields {
			if !f.Kind.IsShape() {
				continue
			}
			val, ok := e.shapeFieldValue(bo.Doc, compName, f)
			if !ok {
				continue
			}
			add := func(kind ShapeHandleKind, idx int, lx, ly float64) {
				p := m.TransformPoint(math2d.Vec2{X: float32(lx), Y: float32(ly)})
				if !isFiniteVec(p) {
					return
				}
				out = append(out, ShapeHandle2D{
					ObjectID:  bo.ID,
					Component: compName,
					Field:     f.Name,
					Kind:      kind,
					Index:     idx,
					Pos:       p,
				})
			}
			switch v := val.(type) {
			case sceneformat.Point:
				add(ShapeHandlePoint, 0, v.X, v.Y)
			case sceneformat.Rect:
				cx, cy := v.X+v.Width/2, v.Y+v.Height/2
				cp := m.TransformPoint(math2d.Vec2{X: float32(cx), Y: float32(cy)})
				if isFiniteVec(cp) && vecLen(cp.Sub(originCanvas)) > centerMinDist {
					add(ShapeHandleRectCenter, 0, cx, cy)
				}
				add(ShapeHandleRectNW, 0, v.X, v.Y)
				add(ShapeHandleRectNE, 0, v.X+v.Width, v.Y)
				add(ShapeHandleRectSE, 0, v.X+v.Width, v.Y+v.Height)
				add(ShapeHandleRectSW, 0, v.X, v.Y+v.Height)
				add(ShapeHandleRectN, 0, cx, v.Y)
				add(ShapeHandleRectE, 0, v.X+v.Width, cy)
				add(ShapeHandleRectS, 0, cx, v.Y+v.Height)
				add(ShapeHandleRectW, 0, v.X, cy)
			case sceneformat.Circle:
				cp := m.TransformPoint(math2d.Vec2{X: float32(v.X), Y: float32(v.Y)})
				if isFiniteVec(cp) && vecLen(cp.Sub(originCanvas)) > centerMinDist {
					add(ShapeHandleCircleCenter, 0, v.X, v.Y)
				}
				if v.Radius > 0 {
					add(ShapeHandleCircleRadius, 0, v.X+v.Radius, v.Y)
					add(ShapeHandleCircleRadius, 1, v.X, v.Y+v.Radius)
					add(ShapeHandleCircleRadius, 2, v.X-v.Radius, v.Y)
					add(ShapeHandleCircleRadius, 3, v.X, v.Y-v.Radius)
				} else {
					add(ShapeHandleCircleRadius, 0, v.X, v.Y)
				}
			case []sceneformat.Point:
				for i, pt := range v {
					add(ShapeHandlePolygonVertex, i, pt.X, pt.Y)
				}
				if len(v) >= 2 {
					edgeCount := len(v)
					if edgeCount == 2 {
						edgeCount = 1
					}
					for i := range edgeCount {
						a := v[i]
						b := v[(i+1)%len(v)]
						add(ShapeHandlePolygonMidpoint, i, (a.X+b.X)/2, (a.Y+b.Y)/2)
					}
				}
			}
		}
	}
	return out
}

func shapeHandlePriority(k ShapeHandleKind) int {
	switch k {
	case ShapeHandlePolygonMidpoint:
		return 1
	case ShapeHandleRectCenter, ShapeHandleCircleCenter:
		return 2
	default:
		return 0
	}
}

// PickShapeHandle2D returns the interactive shape handle under canvas point (x, y), or false when none is hit.
func (e *Editor) PickShapeHandle2D(x, y float32) (ShapeHandle2D, bool) {
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return ShapeHandle2D{}, false
	}
	handles := e.ShapeHandles2D()
	if len(handles) == 0 {
		return ShapeHandle2D{}, false
	}
	p := math2d.Vec2{X: x, Y: y}
	bestIdx := -1
	bestPrio := 99
	bestDist := float32(math.MaxFloat32)
	for i, h := range handles {
		d := vecLen(p.Sub(h.Pos))
		if d > shapeHandlePick {
			continue
		}
		prio := shapeHandlePriority(h.Kind)
		if bestIdx < 0 || prio < bestPrio || (prio == bestPrio && d < bestDist) {
			bestIdx = i
			bestPrio = prio
			bestDist = d
		}
	}
	if bestIdx < 0 {
		return ShapeHandle2D{}, false
	}
	return handles[bestIdx], true
}

// DraggingShape2D reports whether a 2D viewport component shape handle drag is currently active.
func (e *Editor) DraggingShape2D() bool {
	return e != nil && e.shapeDrag.active
}

// ActiveShapeHandle2D returns the handle currently being dragged, or false if no shape drag is active.
func (e *Editor) ActiveShapeHandle2D() (ShapeHandle2D, bool) {
	if e == nil || !e.shapeDrag.active {
		return ShapeHandle2D{}, false
	}
	return e.shapeDrag.handle, true
}

func (e *Editor) objectToLocalAffine(objectID string, space sceneformat.Space) (math2d.Affine, bool) {
	scene := e.ActiveScene()
	if scene == nil {
		return math2d.Affine{}, false
	}
	bo := scene.Object(objectID)
	if bo == nil {
		return math2d.Affine{}, false
	}
	return e.layerAffine(space).Mul(bo.World).Invert()
}

// BeginShapeDrag2D starts dragging a component shape handle at canvas point (x, y).
// Dragging a polygon edge midpoint inserts a new vertex at that edge and drags it as a single undoable SetComponentField step.
func (e *Editor) BeginShapeDrag2D(x, y float32) (ShapeHandle2D, bool) {
	if e == nil || e.shapeDrag.active || !e.shapeHandlesEnabled() || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return ShapeHandle2D{}, false
	}
	h, ok := e.PickShapeHandle2D(x, y)
	if !ok {
		return ShapeHandle2D{}, false
	}
	obj, li, _ := e.FindObject(h.ObjectID)
	if obj == nil || li < 0 || e.checkAgentTouchLock(h.ObjectID) != nil {
		return ShapeHandle2D{}, false
	}
	ct, ok := e.lookupComponentType(h.Component)
	if !ok {
		return ShapeHandle2D{}, false
	}
	f, ok := ct.Field(h.Field)
	if !ok || !f.Kind.IsShape() {
		return ShapeHandle2D{}, false
	}
	startVal, ok := e.shapeFieldValue(obj, h.Component, f)
	if !ok {
		return ShapeHandle2D{}, false
	}
	space := e.Doc.Layers[li].EffectiveSpace()
	inv, ok := e.objectToLocalAffine(h.ObjectID, space)
	if !ok {
		return ShapeHandle2D{}, false
	}
	grabLocal := inv.TransformPoint(math2d.Vec2{X: x, Y: y})
	if !isFiniteVec(grabLocal) {
		return ShapeHandle2D{}, false
	}

	hadComps := obj.Components != nil
	var compExisted, fieldExisted bool
	var origVal any
	if obj.Components != nil {
		if raw, exists := obj.Components[h.Component]; exists {
			compExisted = true
			if m, ok := raw.(map[string]any); ok {
				if fv, fExists := m[h.Field]; fExists {
					fieldExisted = true
					origVal = cloneAny(fv)
				}
			}
		}
	}

	st := shapeDragState{
		active:       true,
		objectID:     h.ObjectID,
		component:    h.Component,
		field:        h.Field,
		space:        space,
		handle:       h,
		hadComps:     hadComps,
		compExisted:  compExisted,
		fieldExisted: fieldExisted,
		origVal:      origVal,
		startShape:   cloneAny(startVal),
		current:      cloneAny(startVal),
		grabLocal:    grabLocal,
	}

	if h.Kind == ShapeHandlePolygonMidpoint {
		pts := slices.Clone(startVal.([]sceneformat.Point))
		if len(pts) >= 2 && h.Index >= 0 && h.Index < len(pts) {
			a := pts[h.Index]
			b := pts[(h.Index+1)%len(pts)]
			mid := sceneformat.Point{
				X: e.SnapValue((a.X + b.X) / 2),
				Y: e.SnapValue((a.Y + b.Y) / 2),
			}
			insertAt := h.Index + 1
			pts = slices.Insert(pts, insertAt, mid)
			st.startShape = slices.Clone(pts)
			st.current = slices.Clone(pts)
			st.handle.Kind = ShapeHandlePolygonVertex
			st.handle.Index = insertAt
			st.grabLocal = math2d.Vec2{X: float32(mid.X), Y: float32(mid.Y)}
			st.inserted = true
			st.changed = true
			writeShapeFieldInPlace(obj, st.component, st.field, st.current)
			e.Rebuild()
		}
	}

	e.shapeDrag = st
	e.Status = "Edit " + st.component + "." + st.field + " (" + st.handle.Kind.String() + ")"
	e.notify()
	return st.handle, true
}

// UpdateShapeDrag2D updates the active shape handle drag to canvas point (x, y), applying grid snapping when SnapToGrid is enabled.
func (e *Editor) UpdateShapeDrag2D(x, y float32) {
	if e == nil || !e.shapeDrag.active || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return
	}
	st := &e.shapeDrag
	obj, _, _ := e.FindObject(st.objectID)
	if obj == nil || e.playing {
		e.CancelShapeDrag2D()
		return
	}
	inv, ok := e.objectToLocalAffine(st.objectID, st.space)
	if !ok {
		return
	}
	cur := inv.TransformPoint(math2d.Vec2{X: x, Y: y})
	if !isFiniteVec(cur) {
		return
	}
	dx := float64(cur.X) - float64(st.grabLocal.X)
	dy := float64(cur.Y) - float64(st.grabLocal.Y)
	snapX := func(v float64) float64 { return e.SnapAxis2D(v, 0) }
	snapY := func(v float64) float64 { return e.SnapAxis2D(v, 1) }
	snap := e.SnapValue

	var next any
	switch st.handle.Kind {
	case ShapeHandlePoint:
		s := st.startShape.(sceneformat.Point)
		next = sceneformat.Point{
			X: snapX(s.X + dx),
			Y: snapY(s.Y + dy),
		}
	case ShapeHandleRectCenter:
		s := st.startShape.(sceneformat.Rect)
		next = sceneformat.Rect{
			X:      snapX(s.X + dx),
			Y:      snapY(s.Y + dy),
			Width:  s.Width,
			Height: s.Height,
		}
	case ShapeHandleRectN, ShapeHandleRectE, ShapeHandleRectS, ShapeHandleRectW,
		ShapeHandleRectNE, ShapeHandleRectSE, ShapeHandleRectSW, ShapeHandleRectNW:
		s := st.startShape.(sceneformat.Rect)
		x0, y0 := s.X, s.Y
		x1, y1 := s.X+s.Width, s.Y+s.Height
		nx0, ny0, nx1, ny1 := x0, y0, x1, y1
		switch st.handle.Kind {
		case ShapeHandleRectE:
			tx := snapX(x1 + dx)
			nx0, nx1 = min(x0, tx), max(x0, tx)
		case ShapeHandleRectW:
			tx := snapX(x0 + dx)
			nx0, nx1 = min(tx, x1), max(tx, x1)
		case ShapeHandleRectS:
			ty := snapY(y1 + dy)
			ny0, ny1 = min(y0, ty), max(y0, ty)
		case ShapeHandleRectN:
			ty := snapY(y0 + dy)
			ny0, ny1 = min(ty, y1), max(ty, y1)
		case ShapeHandleRectNE:
			tx, ty := snapX(x1+dx), snapY(y0+dy)
			nx0, nx1 = min(x0, tx), max(x0, tx)
			ny0, ny1 = min(ty, y1), max(ty, y1)
		case ShapeHandleRectSE:
			tx, ty := snapX(x1+dx), snapY(y1+dy)
			nx0, nx1 = min(x0, tx), max(x0, tx)
			ny0, ny1 = min(y0, ty), max(y0, ty)
		case ShapeHandleRectSW:
			tx, ty := snapX(x0+dx), snapY(y1+dy)
			nx0, nx1 = min(tx, x1), max(tx, x1)
			ny0, ny1 = min(y0, ty), max(y0, ty)
		case ShapeHandleRectNW:
			tx, ty := snapX(x0+dx), snapY(y0+dy)
			nx0, nx1 = min(tx, x1), max(tx, x1)
			ny0, ny1 = min(ty, y1), max(ty, y1)
		}
		next = sceneformat.Rect{
			X:      normZero(nx0),
			Y:      normZero(ny0),
			Width:  normZero(nx1 - nx0),
			Height: normZero(ny1 - ny0),
		}
	case ShapeHandleCircleCenter:
		s := st.startShape.(sceneformat.Circle)
		next = sceneformat.Circle{
			X:      snapX(s.X + dx),
			Y:      snapY(s.Y + dy),
			Radius: s.Radius,
		}
	case ShapeHandleCircleRadius:
		s := st.startShape.(sceneformat.Circle)
		grabDist := math.Hypot(float64(st.grabLocal.X)-s.X, float64(st.grabLocal.Y)-s.Y)
		curDist := math.Hypot(float64(cur.X)-s.X, float64(cur.Y)-s.Y)
		r := snap(max(0.0, s.Radius+(curDist-grabDist)))
		if r < 0 {
			r = 0
		}
		next = sceneformat.Circle{
			X:      s.X,
			Y:      s.Y,
			Radius: normZero(r),
		}
	case ShapeHandlePolygonVertex:
		pts := slices.Clone(st.startShape.([]sceneformat.Point))
		if st.handle.Index < 0 || st.handle.Index >= len(pts) {
			return
		}
		pts[st.handle.Index] = sceneformat.Point{
			X: snapX(pts[st.handle.Index].X + dx),
			Y: snapY(pts[st.handle.Index].Y + dy),
		}
		next = pts
	default:
		return
	}

	st.current = next
	st.changed = true
	writeShapeFieldInPlace(obj, st.component, st.field, next)
	e.Rebuild()
	e.notify()
}

// EndShapeDrag2D finishes the active shape drag and commits it via SetComponentField as one undoable step.
// It returns true when a change was recorded.
func (e *Editor) EndShapeDrag2D() bool {
	if e == nil || !e.shapeDrag.active {
		return false
	}
	st := e.shapeDrag
	e.shapeDrag = shapeDragState{}
	obj, _, _ := e.FindObject(st.objectID)
	if obj == nil {
		e.notify()
		return false
	}
	restoreShapeFieldInPlace(obj, st)
	if !st.changed {
		e.Rebuild()
		e.notify()
		return false
	}
	beforeLen := e.History.Len()
	if err := e.SetComponentField(st.objectID, st.component, st.field, st.current); err != nil {
		e.Rebuild()
		e.report(err)
		return false
	}
	return e.History.Len() > beforeLen
}

// CancelShapeDrag2D aborts any active shape handle drag, restoring the pre-drag component value and recording nothing.
func (e *Editor) CancelShapeDrag2D() bool {
	if e == nil || !e.shapeDrag.active {
		return false
	}
	st := e.shapeDrag
	e.shapeDrag = shapeDragState{}
	if obj, _, _ := e.FindObject(st.objectID); obj != nil {
		restoreShapeFieldInPlace(obj, st)
		e.Rebuild()
	}
	e.Status = "Cancelled " + st.handle.Kind.String()
	e.notify()
	return true
}

// AddPolygonVertex inserts pt into the FieldPolygon component field after afterIndex (or appends when afterIndex < 0 or >= len(poly)) as one undoable SetComponentField step.
func (e *Editor) AddPolygonVertex(objectID, component, field string, afterIndex int, pt sceneformat.Point) error {
	if e.playing {
		return ErrPlaying
	}
	e.CancelShapeDrag2D()
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	ct, ok := e.lookupComponentType(component)
	if !ok {
		return fmt.Errorf("editor: component %q is not registered", component)
	}
	f, ok := ct.Field(field)
	if !ok || f.Kind != sceneformat.FieldPolygon {
		return fmt.Errorf("editor: %s.%s is not a polygon field", component, field)
	}
	cur, ok := e.shapeFieldValue(obj, component, f)
	if !ok {
		return fmt.Errorf("editor: object %q has no component %q", objectID, component)
	}
	pts := slices.Clone(cur.([]sceneformat.Point))
	pt, err := sceneformat.ParsePoint(pt)
	if err != nil {
		return err
	}
	if afterIndex < 0 || afterIndex >= len(pts) {
		pts = append(pts, pt)
	} else {
		pts = slices.Insert(pts, afterIndex+1, pt)
	}
	return e.SetComponentField(objectID, component, field, pts)
}

// RemovePolygonVertex removes the vertex at index from a FieldPolygon component field as one undoable SetComponentField step.
// Removing a vertex that would leave fewer than 3 vertices is refused.
func (e *Editor) RemovePolygonVertex(objectID, component, field string, index int) error {
	if e.playing {
		return ErrPlaying
	}
	e.CancelShapeDrag2D()
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	ct, ok := e.lookupComponentType(component)
	if !ok {
		return fmt.Errorf("editor: component %q is not registered", component)
	}
	f, ok := ct.Field(field)
	if !ok || f.Kind != sceneformat.FieldPolygon {
		return fmt.Errorf("editor: %s.%s is not a polygon field", component, field)
	}
	cur, ok := e.shapeFieldValue(obj, component, f)
	if !ok {
		return fmt.Errorf("editor: object %q has no component %q", objectID, component)
	}
	pts := slices.Clone(cur.([]sceneformat.Point))
	if index < 0 || index >= len(pts) {
		return fmt.Errorf("editor: polygon vertex index %d out of range [0, %d)", index, len(pts))
	}
	if len(pts) <= minPolygonVerts {
		return fmt.Errorf("editor: polygon must keep at least %d vertices", minPolygonVerts)
	}
	pts = slices.Delete(pts, index, index+1)
	return e.SetComponentField(objectID, component, field, pts)
}

// HandleCanvasSecondaryPress handles a secondary (right-button) press at canvas coordinates (x, y) in 2D edit mode.
// When the press hits a polygon vertex handle on the selected object with the Select or Move tool, it removes that vertex as one undoable step and reports true.
func (e *Editor) HandleCanvasSecondaryPress(x, y float32) bool {
	if !e.shapeHandlesEnabled() {
		return false
	}
	return e.handleShapePress2D(x, y, true)
}

func (e *Editor) handleShapePress2D(x, y float32, removeVertex bool) bool {
	if !e.shapeHandlesEnabled() {
		return false
	}
	h, ok := e.PickShapeHandle2D(x, y)
	if !ok {
		return false
	}
	if removeVertex {
		if h.Kind == ShapeHandlePolygonVertex {
			e.report(e.RemovePolygonVertex(h.ObjectID, h.Component, h.Field, h.Index))
			return true
		}
		return false
	}
	if h.Kind == ShapeHandlePolygonMidpoint {
		obj, _, _ := e.FindObject(h.ObjectID)
		ct, ok := e.lookupComponentType(h.Component)
		if obj == nil || !ok {
			return false
		}
		f, ok := ct.Field(h.Field)
		if !ok {
			return false
		}
		cur, ok := e.shapeFieldValue(obj, h.Component, f)
		if !ok {
			return false
		}
		pts := cur.([]sceneformat.Point)
		if h.Index < 0 || h.Index >= len(pts) {
			return false
		}
		a := pts[h.Index]
		b := pts[(h.Index+1)%len(pts)]
		mid := sceneformat.Point{
			X: e.SnapValue((a.X + b.X) / 2),
			Y: e.SnapValue((a.Y + b.Y) / 2),
		}
		e.report(e.AddPolygonVertex(h.ObjectID, h.Component, h.Field, h.Index, mid))
		return true
	}
	// Clicking on a shape handle of the selected object with Select keeps the selection instead of deselecting when the shape extends outside the sprite bounds.
	return e.Tool == ToolSelect
}

func writeShapeFieldInPlace(obj *sceneformat.ObjectDoc, comp, field string, val any) {
	if obj.Components == nil {
		obj.Components = make(map[string]any)
	}
	m, ok := obj.Components[comp].(map[string]any)
	if !ok || m == nil {
		m = make(map[string]any)
		obj.Components[comp] = m
	}
	m[field] = cloneAny(val)
}

func restoreShapeFieldInPlace(obj *sceneformat.ObjectDoc, st shapeDragState) {
	if !st.compExisted {
		delete(obj.Components, st.component)
		if !st.hadComps && len(obj.Components) == 0 {
			obj.Components = nil
		}
		return
	}
	m, ok := obj.Components[st.component].(map[string]any)
	if !ok || m == nil {
		return
	}
	if !st.fieldExisted {
		delete(m, st.field)
		return
	}
	m[st.field] = cloneAny(st.origVal)
}

func normZero(v float64) float64 {
	if v == 0 {
		return 0
	}
	return v
}

// --- 2D Viewport Drawing ---

func shapeBaseColor(p SpecPalette, compName string) core.Color {
	lc := strings.ToLower(compName)
	switch {
	case strings.Contains(lc, "trigger"), strings.Contains(lc, "sensor"), strings.Contains(lc, "area"):
		return p.Warning.Fg
	case strings.Contains(lc, "nav"), strings.Contains(lc, "path"), strings.Contains(lc, "spawn"):
		return p.Added.Fg
	case strings.Contains(lc, "light"), strings.Contains(lc, "occlud"), strings.Contains(lc, "shadow"):
		return p.Agent.Fg
	default:
		return p.Accent.Fg
	}
}

func fillAndStrokePolygon(d *core.Drawing, pts []math2d.Vec2, fill, stroke core.Color) {
	if len(pts) == 0 {
		return
	}
	d.BeginPath().MoveTo(pts[0].X, pts[0].Y)
	for _, p := range pts[1:] {
		d.LineTo(p.X, p.Y)
	}
	if len(pts) >= 3 {
		d.ClosePath()
		d.SetFillColor(fill).Fill()
		d.BeginPath().MoveTo(pts[0].X, pts[0].Y)
		for _, p := range pts[1:] {
			d.LineTo(p.X, p.Y)
		}
		d.ClosePath()
	}
	d.SetStrokeColor(stroke).Stroke()
}

func (e *Editor) drawComponentShapes2D(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e.drawOverlayOverride != nil {
		return
	}
	shapes := e.VisibleShapes2D()
	if len(shapes) == 0 {
		return
	}
	showCollision := e.overlayActive(OverlayCollisionShapes)
	showTriggers := e.overlayActive(OverlayTriggers)
	showPatrol := e.overlayActive(OverlayPatrolPaths)
	saved := false
	for _, s := range shapes {
		// Component shape fields are rendered by the Collision shapes, Triggers, and Patrol
		// paths viewport overlays (covering both unselected and selected objects). Skip them
		// here when their overlay is active to avoid double-drawing, and skip non-selected
		// ShowAllShapes entries when their overlay is off so toggling the overlay off hides them
		// in the live viewport just as viewport_image does.
		var overlayOn bool
		switch {
		case isPatrolComponentName(s.Component):
			overlayOn = showPatrol
		case isTriggerComponentName(s.Component):
			overlayOn = showTriggers
		default:
			overlayOn = showCollision
		}
		if overlayOn || !s.Selected {
			continue
		}
		if !saved {
			d.Save()
			saved = true
		}
		base := shapeBaseColor(e.activeSpecPalette(), s.Component)
		strokeAlpha := float32(0.55)
		fillAlpha := float32(0.07)
		lineW := float32(1.25)
		if s.Selected {
			strokeAlpha = 0.95
			fillAlpha = 0.15
			lineW = 1.75
		}
		stroke := specAlpha(base, strokeAlpha)
		fill := specAlpha(base, fillAlpha)
		d.SetLineWidth(lineW)
		switch s.Kind {
		case sceneformat.FieldPoint:
			if len(s.Points) == 1 {
				p := s.Points[0]
				const arm float32 = 8
				d.SetStrokeColor(stroke)
				d.BeginPath().MoveTo(p.X-arm, p.Y).LineTo(p.X+arm, p.Y).MoveTo(p.X, p.Y-arm).LineTo(p.X, p.Y+arm).Stroke()
				d.SetFillColor(fill)
				d.BeginPath().Ellipse(p.X, p.Y, 4, 4).Fill().Stroke()
			}
		case sceneformat.FieldRect, sceneformat.FieldCircle, sceneformat.FieldPolygon:
			if len(s.Points) == 1 {
				p := s.Points[0]
				d.SetFillColor(stroke)
				d.BeginPath().Ellipse(p.X, p.Y, 3, 3).Fill()
			} else if len(s.Points) >= 2 {
				fillAndStrokePolygon(d, s.Points, fill, stroke)
			}
		}
	}
	if saved {
		d.Restore()
	}
}

func (e *Editor) drawComponentShapeHandles2D(d *core.Drawing) {
	handles := e.ShapeHandles2D()
	if len(handles) == 0 {
		return
	}
	active, hasActive := e.ActiveShapeHandle2D()
	d.Save()
	d.SetLineWidth(1.5)
	spec := e.activeSpecPalette()
	for _, h := range handles {
		isActive := hasActive &&
			h.ObjectID == active.ObjectID &&
			h.Component == active.Component &&
			h.Field == active.Field &&
			h.Kind == active.Kind &&
			h.Index == active.Index
		stroke := shapeBaseColor(spec, h.Component)
		fill := spec.TextPrimary
		if isActive {
			fill = spec.Warning.Fg
			stroke = spec.Bg
		}
		p := h.Pos
		switch h.Kind {
		case ShapeHandlePolygonMidpoint:
			d.SetFillColor(specAlpha(spec.Bg, 0.75))
			d.SetStrokeColor(stroke)
			d.BeginPath().Ellipse(p.X, p.Y, shapeMidHalf, shapeMidHalf).Fill().Stroke()
		case ShapeHandlePolygonVertex, ShapeHandlePoint, ShapeHandleCircleRadius, ShapeHandleCircleCenter:
			d.SetFillColor(fill)
			d.SetStrokeColor(stroke)
			d.BeginPath().Ellipse(p.X, p.Y, shapeHandleHalf, shapeHandleHalf).Fill().Stroke()
		default:
			d.SetFillColor(fill)
			d.SetStrokeColor(stroke)
			d.FillRect(p.X-shapeHandleHalf, p.Y-shapeHandleHalf, 2*shapeHandleHalf, 2*shapeHandleHalf)
			d.StrokeRect(p.X-shapeHandleHalf, p.Y-shapeHandleHalf, 2*shapeHandleHalf, 2*shapeHandleHalf)
		}
	}
	d.Restore()
}
