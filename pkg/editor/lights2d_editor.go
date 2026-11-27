package editor

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"

	"github.com/nsgo-dev/game/pkg/lights2d"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// lightHandlePickRadius is how close in canvas points a press must be to take a light radius, spot angle, or direction handle.
	lightHandlePickRadius float32 = 8
	// occluderVertexPickRadius is how close in canvas points a press must be to take a polygon vertex or rect edge handle.
	occluderVertexPickRadius float32 = 8
	// occluderEdgeMidPickRadius is how close in canvas points a press must be to take an edge-midpoint insertion handle.
	occluderEdgeMidPickRadius float32 = 6
	// occluderEdgeMidMinLen is the shortest polygon edge in canvas points that offers a midpoint insertion handle.
	occluderEdgeMidMinLen float32 = 24
	// minLightRadius is the smallest radius the viewport handle sets so the handle stays grabbable.
	minLightRadius = 1.0
	// minSpotAngle and maxSpotAngle bound the spotlight cone spread dragged with the cone edge handles.
	minSpotAngle = 0.05
	maxSpotAngle = 2*math.Pi - 0.02
	// minOccluderRectSide is the smallest width or height a rect occluder handle sets.
	minOccluderRectSide = 2.0
)

// AmbientColor returns the scene's 2D lighting ambient color CSS string, or empty when unset.
func (e *Editor) AmbientColor() string {
	if e == nil || e.Doc == nil {
		return ""
	}
	return e.Doc.AmbientColor
}

// SetAmbientColor sets the scene's 2D lighting ambient color (SceneDoc.AmbientColor) through History.
// Empty clears it; any non-empty value must be a CSS color accepted by core.ParseColor.
func (e *Editor) SetAmbientColor(color string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	if color != "" {
		if _, err := core.ParseColor(color); err != nil {
			return fmt.Errorf("editor: ambientColor %q: %w", color, err)
		}
	}
	if e.Doc.AmbientColor == color {
		return nil
	}
	e.History.Execute(&ambientColorCmd{
		ed:     e,
		before: e.Doc.AmbientColor,
		after:  color,
	})
	return nil
}

type ambientColorCmd struct {
	ed            *Editor
	before, after string
}

func (c *ambientColorCmd) Label() string {
	if c.after == "" {
		return "Clear ambient color"
	}
	return "Set ambient color " + c.after
}

func (c *ambientColorCmd) Do() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.AmbientColor = c.after
	c.ed.Status = c.Label()
}

func (c *ambientColorCmd) Undo() {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.AmbientColor = c.before
}

// SetShowLightingPreview toggles live 2D lighting and shadow preview in the 2D edit viewport.
func (e *Editor) SetShowLightingPreview(show bool) {
	if e == nil || e.ShowLightingPreview == show {
		return
	}
	e.ShowLightingPreview = show
	if show {
		e.Status = "2D lighting preview on"
	} else {
		e.Status = "2D lighting preview off"
	}
	e.notify()
}

// componentMapCmd replaces one component's map on objectID as a single undoable step.
type componentMapCmd struct {
	ed          *Editor
	objectID    string
	component   string
	label       string
	wasNilComps bool
	existed     bool
	before      map[string]any
	after       map[string]any
}

func (c *componentMapCmd) Label() string { return c.label }

func (c *componentMapCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if obj.Components == nil {
		obj.Components = make(map[string]any)
	}
	obj.Components[c.component] = cloneMap(c.after)
	c.ed.Status = c.label
}

func (c *componentMapCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil || obj.Components == nil {
		return
	}
	if !c.existed {
		delete(obj.Components, c.component)
		if c.wasNilComps && len(obj.Components) == 0 {
			obj.Components = nil
		}
		return
	}
	obj.Components[c.component] = cloneMap(c.before)
}

func (e *Editor) executeComponentMapEdit(objectID, component, label string, mutate func(m map[string]any)) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	var before map[string]any
	existed := false
	if obj.Components != nil {
		if raw, ok := obj.Components[component]; ok {
			existed = true
			m, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("editor: component %q on %q is not a map", component, objectID)
			}
			before = cloneMap(m)
		}
	}
	after := cloneMap(before)
	if after == nil {
		after = make(map[string]any)
	}
	mutate(after)
	if e.components != nil {
		if err := e.components.CheckComponent(component, after); err != nil {
			var ce *sceneformat.ComponentError
			if errors.As(err, &ce) {
				ce.Object = objectID
			}
			return err
		}
	}
	if existed && reflect.DeepEqual(before, after) {
		return nil
	}
	e.History.Execute(&componentMapCmd{
		ed:          e,
		objectID:    objectID,
		component:   component,
		label:       label,
		wasNilComps: obj.Components == nil,
		existed:     existed,
		before:      before,
		after:       after,
	})
	return nil
}

// SetOccluderPolygonPoints replaces the local polygon vertices of objectID's occluder2d component through History (at least 3 finite points).
func (e *Editor) SetOccluderPolygonPoints(objectID string, points [][2]float64) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	if len(points) < 3 {
		return errors.New("editor: occluder2d polygon requires at least 3 points")
	}
	for i, p := range points {
		if math.IsNaN(p[0]) || math.IsInf(p[0], 0) || math.IsNaN(p[1]) || math.IsInf(p[1], 0) {
			return fmt.Errorf("editor: occluder2d point %d must be finite", i)
		}
	}
	cp := slices.Clone(points)
	return e.executeComponentMapEdit(objectID, sceneformat.Occluder2DComponentName, "Edit occluder polygon of "+objectID, func(m map[string]any) {
		m["shape"] = "polygon"
		m["points"] = cp
	})
}

// InsertOccluderVertex inserts a new local vertex (x, y) after vertex index afterIndex in objectID's polygon occluder2d component through History.
func (e *Editor) InsertOccluderVertex(objectID string, afterIndex int, x, y float64) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
		return errors.New("editor: occluder2d vertex must be finite")
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	occ, ok, err := sceneformat.ExtractOccluder2D(obj, nil)
	if err != nil {
		return err
	}
	if !ok || occ.Shape != "polygon" || len(occ.Points) < 2 {
		return fmt.Errorf("editor: object %q does not have a polygon occluder2d", objectID)
	}
	if afterIndex < 0 || afterIndex >= len(occ.Points) {
		return fmt.Errorf("editor: vertex index %d out of range [0, %d)", afterIndex, len(occ.Points))
	}
	next := slices.Insert(slices.Clone(occ.Points), afterIndex+1, [2]float64{x, y})
	return e.executeComponentMapEdit(objectID, sceneformat.Occluder2DComponentName, "Insert occluder vertex on "+objectID, func(m map[string]any) {
		m["points"] = next
	})
}

// RemoveOccluderVertex removes the vertex at index from objectID's polygon occluder2d component through History.
// Removing below 3 vertices is refused.
func (e *Editor) RemoveOccluderVertex(objectID string, index int) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	occ, ok, err := sceneformat.ExtractOccluder2D(obj, nil)
	if err != nil {
		return err
	}
	if !ok || occ.Shape != "polygon" {
		return fmt.Errorf("editor: object %q does not have a polygon occluder2d", objectID)
	}
	if len(occ.Points) <= 3 {
		return errors.New("editor: occluder2d polygon must keep at least 3 vertices")
	}
	if index < 0 || index >= len(occ.Points) {
		return fmt.Errorf("editor: vertex index %d out of range [0, %d)", index, len(occ.Points))
	}
	next := slices.Delete(slices.Clone(occ.Points), index, index+1)
	return e.executeComponentMapEdit(objectID, sceneformat.Occluder2DComponentName, "Remove occluder vertex on "+objectID, func(m map[string]any) {
		m["points"] = next
	})
}

// ConvertOccluderToPolygon converts objectID's occluder2d component to shape "polygon", seeding its points from the current rect dimensions when points is empty, in one undoable step.
func (e *Editor) ConvertOccluderToPolygon(objectID string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	occ, ok, err := sceneformat.ExtractOccluder2D(obj, nil)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("editor: object %q has no occluder2d component", objectID)
	}
	pts := slices.Clone(occ.Points)
	if len(pts) < 3 {
		hw := float64(occ.Width) * 0.5
		hh := float64(occ.Height) * 0.5
		if hw <= 0 {
			hw = 16
		}
		if hh <= 0 {
			hh = 16
		}
		pts = [][2]float64{
			{-hw, -hh},
			{hw, -hh},
			{hw, hh},
			{-hw, hh},
		}
	}
	return e.executeComponentMapEdit(objectID, sceneformat.Occluder2DComponentName, "Convert occluder to polygon on "+objectID, func(m map[string]any) {
		m["shape"] = "polygon"
		m["points"] = pts
	})
}

// editorLightingBounds returns the world-space rectangle covering the entire 2D viewport canvas.
func (e *Editor) editorLightingBounds(size core.Size) core.Bounds {
	cw, ch := float32(size.Width), float32(size.Height)
	if cw <= 0 || ch <= 0 {
		cw, ch = e.CanvasSize()
	}
	corners := [4][2]float32{{0, 0}, {cw, 0}, {cw, ch}, {0, ch}}
	minX, minY := e.CanvasToWorld(corners[0][0], corners[0][1])
	maxX, maxY := minX, minY
	for _, c := range corners[1:] {
		wx, wy := e.CanvasToWorld(c[0], c[1])
		minX, maxX = min(minX, wx), max(maxX, wx)
		minY, maxY = min(minY, wy), max(maxY, wy)
	}
	return core.Bounds{
		Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
		Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
	}
}

// hitTestOccluder2D reports whether the object-local point (dx, dy) lies inside o's rect or polygon occluder2d, so clicking anywhere inside a non-sprite occluder selects it.
func hitTestOccluder2D(o *sceneformat.ObjectDoc, dx, dy float32) bool {
	occ, ok, err := sceneformat.ExtractOccluder2D(o, nil)
	if err != nil || !ok {
		return false
	}
	lx, ly := dx-occ.OffsetX, dy-occ.OffsetY
	switch occ.Shape {
	case "rect":
		hw, hh := occ.Width*0.5, occ.Height*0.5
		return hw > 0 && hh > 0 && lx >= -hw && lx <= hw && ly >= -hh && ly <= hh
	case "polygon":
		if len(occ.Points) < 3 {
			return false
		}
		var buf [16]core.Point
		var poly []core.Point
		if len(occ.Points) <= len(buf) {
			poly = buf[:len(occ.Points)]
		} else {
			poly = make([]core.Point, len(occ.Points))
		}
		for i, pt := range occ.Points {
			poly[i] = core.Point{X: core.Points(pt[0]), Y: core.Points(pt[1])}
		}
		return lights2d.PointInPolygon(core.Point{X: core.Points(lx), Y: core.Points(ly)}, poly)
	default:
		return false
	}
}

type light2DGizmo struct {
	center       math2d.Vec2
	radiusHandle math2d.Vec2
	spotAnglePos [2]math2d.Vec2
	spotDirPos   math2d.Vec2
	hasSpot      bool
	hasSpotDir   bool
	dbg          sceneformat.Light2DDebug
	toCanvas     math2d.Affine
}

type occluder2DGizmo struct {
	dbg      sceneformat.Occluder2DDebug
	toCanvas math2d.Affine
	// verts are polygon vertices in canvas space (shape: polygon).
	verts []math2d.Vec2
	// edgeMids are polygon edge midpoints in canvas space (only offered when edge >= occluderEdgeMidMinLen).
	edgeMids   []math2d.Vec2
	edgeOffers []bool
	// rectHandles are [N, E, S, W] midpoint handles in canvas space (shape: rect).
	rectHandles [4]math2d.Vec2
	hasRect     bool
}

func (e *Editor) selectedLightingContext() (bo *sceneformat.BuiltObject, space sceneformat.Space, toCanvas math2d.Affine, ok bool) {
	scene := e.ActiveScene()
	if scene == nil || e.SelectedID == "" {
		return nil, "", math2d.Affine{}, false
	}
	bo = scene.Object(e.SelectedID)
	if bo == nil || bo.Doc == nil {
		return nil, "", math2d.Affine{}, false
	}
	space = sceneformat.SpaceWorld
	var px, py float32 = 1, 1
	if layer := scene.Layer(bo.Layer); layer != nil {
		if !layer.Visible {
			return nil, "", math2d.Affine{}, false
		}
		space = layer.Space
		px, py = layer.ParallaxX, layer.ParallaxY
	}
	return bo, space, e.layerAffineFor(space, px, py).Mul(bo.World), true
}

func (e *Editor) selectedLight2DGizmo() (light2DGizmo, bool) {
	bo, _, m, ok := e.selectedLightingContext()
	if !ok {
		return light2DGizmo{}, false
	}
	l, dbg, err := sceneformat.ExtractLight2D(bo.Doc, &bo.World)
	if err != nil || l == nil {
		return light2DGizmo{}, false
	}
	g := light2DGizmo{
		center:   m.TransformPoint(math2d.Vec2{X: dbg.OffsetX, Y: dbg.OffsetY}),
		dbg:      dbg,
		toCanvas: m,
	}
	if dbg.Kind == "spot" {
		g.hasSpot = true
		sinD, cosD := math.Sincos(float64(dbg.Direction))
		g.radiusHandle = m.TransformPoint(math2d.Vec2{
			X: dbg.OffsetX + dbg.Radius*float32(cosD),
			Y: dbg.OffsetY + dbg.Radius*float32(sinD),
		})
		half := float64(dbg.Angle) * 0.5
		for i, sign := range [2]float64{-1, 1} {
			s, c := math.Sincos(float64(dbg.Direction) + sign*half)
			g.spotAnglePos[i] = m.TransformPoint(math2d.Vec2{
				X: dbg.OffsetX + dbg.Radius*float32(c),
				Y: dbg.OffsetY + dbg.Radius*float32(s),
			})
		}
		dirPos := m.TransformPoint(math2d.Vec2{
			X: dbg.OffsetX + dbg.Radius*0.55*float32(cosD),
			Y: dbg.OffsetY + dbg.Radius*0.55*float32(sinD),
		})
		if vecLen(dirPos.Sub(g.center)) >= 18 && vecLen(g.radiusHandle.Sub(dirPos)) >= 14 {
			g.spotDirPos = dirPos
			g.hasSpotDir = true
		}
	} else {
		g.radiusHandle = m.TransformPoint(math2d.Vec2{
			X: dbg.OffsetX + dbg.Radius,
			Y: dbg.OffsetY,
		})
	}
	return g, true
}

func (e *Editor) selectedOccluder2DGizmo() (occluder2DGizmo, bool) {
	bo, _, m, ok := e.selectedLightingContext()
	if !ok {
		return occluder2DGizmo{}, false
	}
	dbg, has, err := sceneformat.ExtractOccluder2D(bo.Doc, &bo.World)
	if err != nil || !has {
		return occluder2DGizmo{}, false
	}
	g := occluder2DGizmo{dbg: dbg, toCanvas: m}
	switch dbg.Shape {
	case "rect":
		if dbg.Width > 0 && dbg.Height > 0 {
			hw, hh := dbg.Width*0.5, dbg.Height*0.5
			ox, oy := dbg.OffsetX, dbg.OffsetY
			g.rectHandles = [4]math2d.Vec2{
				m.TransformPoint(math2d.Vec2{X: ox, Y: oy - hh}), // N
				m.TransformPoint(math2d.Vec2{X: ox + hw, Y: oy}), // E
				m.TransformPoint(math2d.Vec2{X: ox, Y: oy + hh}), // S
				m.TransformPoint(math2d.Vec2{X: ox - hw, Y: oy}), // W
			}
			g.hasRect = true
		}
	case "polygon":
		n := len(dbg.Points)
		if n >= 2 {
			g.verts = make([]math2d.Vec2, n)
			for i, pt := range dbg.Points {
				g.verts[i] = m.TransformPoint(math2d.Vec2{
					X: dbg.OffsetX + float32(pt[0]),
					Y: dbg.OffsetY + float32(pt[1]),
				})
			}
			if n >= 3 {
				g.edgeMids = make([]math2d.Vec2, n)
				g.edgeOffers = make([]bool, n)
				for i := range n {
					a, b := g.verts[i], g.verts[(i+1)%n]
					g.edgeMids[i] = math2d.Vec2{X: (a.X + b.X) * 0.5, Y: (a.Y + b.Y) * 0.5}
					g.edgeOffers[i] = vecLen(b.Sub(a)) >= occluderEdgeMidMinLen
				}
			}
		}
	}
	return g, true
}

// PickOccluderVertex2D returns the polygon vertex index under the canvas point (x, y) on the selected object's occluder2d polygon, or -1 if none.
func (e *Editor) PickOccluderVertex2D(x, y float32) int {
	if !e.Gizmo2DShown() {
		return -1
	}
	og, ok := e.selectedOccluder2DGizmo()
	if !ok || len(og.verts) == 0 {
		return -1
	}
	p := math2d.Vec2{X: x, Y: y}
	for i, v := range og.verts {
		if vecLen(p.Sub(v)) <= occluderVertexPickRadius {
			return i
		}
	}
	return -1
}

// PickOccluderEdge2D returns the polygon edge index whose midpoint handle lies under the canvas point (x, y) on the selected object's occluder2d polygon, or -1 if none.
func (e *Editor) PickOccluderEdge2D(x, y float32) int {
	if !e.Gizmo2DShown() {
		return -1
	}
	og, ok := e.selectedOccluder2DGizmo()
	if !ok || len(og.edgeMids) == 0 {
		return -1
	}
	p := math2d.Vec2{X: x, Y: y}
	for i, mid := range og.edgeMids {
		if og.edgeOffers[i] && vecLen(p.Sub(mid)) <= occluderEdgeMidPickRadius {
			return i
		}
	}
	return -1
}

func (e *Editor) pickLightingHandle2D(x, y float32) (GizmoHandle2D, int) {
	p := math2d.Vec2{X: x, Y: y}
	if lg, ok := e.selectedLight2DGizmo(); ok {
		if vecLen(p.Sub(lg.radiusHandle)) <= lightHandlePickRadius {
			return Handle2DLightRadius, 0
		}
		if lg.hasSpot {
			for i, sp := range lg.spotAnglePos {
				if vecLen(p.Sub(sp)) <= lightHandlePickRadius {
					return Handle2DLightSpotAngle, i
				}
			}
			if lg.hasSpotDir && vecLen(p.Sub(lg.spotDirPos)) <= lightHandlePickRadius {
				return Handle2DLightSpotDirection, 0
			}
		}
	}
	if og, ok := e.selectedOccluder2DGizmo(); ok {
		for i, v := range og.verts {
			if vecLen(p.Sub(v)) <= occluderVertexPickRadius {
				return Handle2DOccluderVertex, i
			}
		}
		for i, mid := range og.edgeMids {
			if og.edgeOffers[i] && vecLen(p.Sub(mid)) <= occluderEdgeMidPickRadius {
				return Handle2DOccluderEdgeMid, i
			}
		}
		if og.hasRect {
			handles := [4]GizmoHandle2D{
				Handle2DOccluderRectN,
				Handle2DOccluderRectE,
				Handle2DOccluderRectS,
				Handle2DOccluderRectW,
			}
			for i, h := range handles {
				if vecLen(p.Sub(og.rectHandles[i])) <= occluderVertexPickRadius {
					return h, 0
				}
			}
		}
	}
	return Handle2DNone, -1
}

func (e *Editor) beginLightingDrag2D(h GizmoHandle2D, idx int, x, y float32) GizmoHandle2D {
	obj, li, _ := e.FindObject(e.SelectedID)
	if obj == nil || e.viewportFrozen(e.SelectedID) {
		return Handle2DNone
	}
	compName := sceneformat.Light2DComponentName
	if h.IsOccluder() {
		compName = sceneformat.Occluder2DComponentName
	}
	raw, ok := obj.Components[compName]
	if !ok {
		return Handle2DNone
	}
	cm, ok := raw.(map[string]any)
	if !ok {
		return Handle2DNone
	}
	path := objectPath(e.Doc, e.SelectedID)
	toLocal, ok := chain2(path).invert()
	if !ok {
		return Handle2DNone
	}
	space, px, py := e.layerSpaceAndParallax(e.Doc.Layers[li].Name)
	lx, ly := e.canvasToLayerFor(space, px, py, x, y)
	gx, gy := toLocal.apply(float64(lx), float64(ly))

	st := drag2DState{
		handle:      h,
		objectID:    e.SelectedID,
		space:       space,
		parallaxX:   px,
		parallaxY:   py,
		toLocal:     toLocal,
		grab:        math2d.Vec2{X: float32(gx), Y: float32(gy)},
		compName:    compName,
		compBefore:  cloneMap(cm),
		vertexIndex: idx,
	}

	// If dragging an edge midpoint, insert the new vertex immediately so UpdateDrag2D moves it in place.
	if h == Handle2DOccluderEdgeMid {
		occ, has, err := sceneformat.ExtractOccluder2D(obj, nil)
		if err != nil || !has || occ.Shape != "polygon" || idx < 0 || idx >= len(occ.Points) {
			return Handle2DNone
		}
		a := occ.Points[idx]
		b := occ.Points[(idx+1)%len(occ.Points)]
		mid := [2]float64{(a[0] + b[0]) * 0.5, (a[1] + b[1]) * 0.5}
		nextPts := slices.Insert(slices.Clone(occ.Points), idx+1, mid)
		cm["points"] = nextPts
		st.vertexIndex = idx + 1
	}

	e.drag2D = st
	e.Status = h.String() + " " + st.objectID
	e.notify()
	return h
}

func (e *Editor) updateLightingDrag2D(x, y float32, keepAspect bool) {
	st := &e.drag2D
	obj, _, _ := e.FindObject(st.objectID)
	if obj == nil || e.playing {
		e.drag2D = drag2DState{}
		e.notify()
		return
	}
	cm, ok := obj.Components[st.compName].(map[string]any)
	if !ok || cm == nil {
		return
	}
	lx, ly := e.canvasToLayerFor(st.space, st.parallaxX, st.parallaxY, x, y)
	px, py := st.toLocal.apply(float64(lx), float64(ly))
	if math.IsNaN(px) || math.IsInf(px, 0) || math.IsNaN(py) || math.IsInf(py, 0) {
		return
	}

	switch st.handle {
	case Handle2DLightRadius:
		_, dbg, err := sceneformat.ExtractLight2D(obj, nil)
		if err != nil {
			return
		}
		dx, dy := px-float64(dbg.OffsetX), py-float64(dbg.OffsetY)
		r := math.Max(minLightRadius, e.SnapValue(math.Hypot(dx, dy)))
		cm["radius"] = r
		if keepAspect && dbg.Kind == "spot" && r > 1e-6 {
			cm["direction"] = e.SnapRotationRadians(math.Atan2(dy, dx))
		}
	case Handle2DLightSpotAngle:
		_, dbg, err := sceneformat.ExtractLight2D(obj, nil)
		if err != nil {
			return
		}
		dx, dy := px-float64(dbg.OffsetX), py-float64(dbg.OffsetY)
		if math.Hypot(dx, dy) < 1e-6 {
			return
		}
		a := math.Atan2(dy, dx)
		diff := math.Abs(math.Remainder(a-float64(dbg.Direction), 2*math.Pi))
		spread := min(max(e.SnapRotationRadians(2*diff), minSpotAngle), maxSpotAngle)
		cm["angle"] = spread
	case Handle2DLightSpotDirection:
		_, dbg, err := sceneformat.ExtractLight2D(obj, nil)
		if err != nil {
			return
		}
		dx, dy := px-float64(dbg.OffsetX), py-float64(dbg.OffsetY)
		if math.Hypot(dx, dy) < 1e-6 {
			return
		}
		cm["direction"] = e.SnapRotationRadians(math.Atan2(dy, dx))
	case Handle2DOccluderVertex, Handle2DOccluderEdgeMid:
		occ, has, err := sceneformat.ExtractOccluder2D(obj, nil)
		if err != nil || !has || st.vertexIndex < 0 || st.vertexIndex >= len(occ.Points) {
			return
		}
		pts := slices.Clone(occ.Points)
		vx, vy := e.SnapPosition2D(px-float64(occ.OffsetX), py-float64(occ.OffsetY))
		pts[st.vertexIndex] = [2]float64{vx, vy}
		cm["points"] = pts
	case Handle2DOccluderRectE, Handle2DOccluderRectW:
		occ, has, err := sceneformat.ExtractOccluder2D(obj, nil)
		if err != nil || !has {
			return
		}
		w := math.Max(minOccluderRectSide, e.SnapAxis2D(2*math.Abs(px-float64(occ.OffsetX)), 0))
		cm["width"] = w
	case Handle2DOccluderRectN, Handle2DOccluderRectS:
		occ, has, err := sceneformat.ExtractOccluder2D(obj, nil)
		if err != nil || !has {
			return
		}
		h := math.Max(minOccluderRectSide, e.SnapAxis2D(2*math.Abs(py-float64(occ.OffsetY)), 1))
		cm["height"] = h
	}

	e.Rebuild()
	e.notify()
}

func (e *Editor) endLightingDrag2D() bool {
	st := e.drag2D
	e.drag2D = drag2DState{}
	obj, _, _ := e.FindObject(st.objectID)
	if obj == nil {
		e.notify()
		return false
	}
	cm, ok := obj.Components[st.compName].(map[string]any)
	if !ok {
		e.notify()
		return false
	}
	after := cloneMap(cm)
	if reflect.DeepEqual(st.compBefore, after) {
		e.Status = "Selected " + st.objectID
		e.notify()
		return false
	}
	// Restore the pre-drag component map so History executes the edit cleanly and Undo/Redo are exact.
	obj.Components[st.compName] = cloneMap(st.compBefore)
	label := st.handle.String() + " " + st.objectID
	e.History.Execute(&componentMapCmd{
		ed:          e,
		objectID:    st.objectID,
		component:   st.compName,
		label:       label,
		wasNilComps: false,
		existed:     true,
		before:      cloneMap(st.compBefore),
		after:       after,
	})
	return true
}

func (e *Editor) cancelLightingDrag2D() bool {
	st := e.drag2D
	e.drag2D = drag2DState{}
	if obj, _, _ := e.FindObject(st.objectID); obj != nil && obj.Components != nil {
		obj.Components[st.compName] = cloneMap(st.compBefore)
		e.Rebuild()
	}
	e.Status = "Cancelled " + st.handle.String()
	e.notify()
	return true
}

// drawLightingOverlays2D draws light origin markers and occluder outlines for all visible objects in edit mode so lights and occluders without sprites remain easy to locate and select.
func (e *Editor) drawLightingOverlays2D(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if d == nil || scene == nil || e.playing {
		return
	}
	pal := e.activeSpecPalette()
	lightOverlayColor := specAlpha(pal.Warning.Fg, 0.85)
	lightOverlaySubtleColor := specAlpha(pal.Warning.Fg, 0.45)
	occluderOverlayColor := specAlpha(pal.Agent.Fg, 0.9)
	occluderOverlaySubtle := specAlpha(pal.Agent.Fg, 0.45)
	for _, layer := range scene.Layers {
		if !layer.Visible || layer.Space != sceneformat.SpaceWorld {
			continue
		}
		toCanvas := e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY)
		var walk func(objs []*sceneformat.BuiltObject)
		walk = func(objs []*sceneformat.BuiltObject) {
			for _, bo := range objs {
				if bo == nil || bo.Doc == nil || viewFlagged(e.hidden, bo) {
					continue
				}
				selected := bo.ID == e.SelectedID
				if l, dbg, err := sceneformat.ExtractLight2D(bo.Doc, &bo.World); err == nil && l != nil {
					c := toCanvas.TransformPoint(math2d.Vec2{X: dbg.WorldX, Y: dbg.WorldY})
					d.Save()
					col := lightOverlaySubtleColor
					if selected {
						col = lightOverlayColor
					}
					d.SetStrokeColor(col).SetLineWidth(1.5)
					d.BeginPath().Arc(c.X, c.Y, 6, 0, 2*math.Pi, false).Stroke()
					// Small radial rays around the light marker.
					for k := range 4 {
						a := float64(k) * math.Pi / 2
						s, cs := math.Sincos(a)
						d.BeginPath().
							MoveTo(c.X+float32(cs)*8, c.Y+float32(s)*8).
							LineTo(c.X+float32(cs)*11, c.Y+float32(s)*11).
							Stroke()
					}
					d.Restore()
				}
				if occ, ok, err := sceneformat.ExtractOccluder2D(bo.Doc, &bo.World); err == nil && ok && len(occ.WorldVertices) >= 2 {
					pts := make([]math2d.Vec2, len(occ.WorldVertices))
					for i, wp := range occ.WorldVertices {
						pts[i] = toCanvas.TransformPoint(math2d.Vec2{X: float32(wp.X), Y: float32(wp.Y)})
					}
					d.Save()
					col := occluderOverlaySubtle
					if selected {
						col = occluderOverlayColor
					}
					d.SetStrokeColor(col).SetLineWidth(1.5)
					if !selected {
						d.SetLineDash(4, 3)
					}
					strokePolygon(d, pts)
					d.Restore()
				}
				if len(bo.Children) > 0 {
					walk(bo.Children)
				}
			}
		}
		walk(layer.Objects)
	}
}

// drawSelectedLightingHandles2D draws the interactive light radius/spot-angle handles and occluder rect/polygon shape handles for the currently selected object.
func (e *Editor) drawSelectedLightingHandles2D(d *core.Drawing) {
	pal := e.activeSpecPalette()
	lightOverlayColor := specAlpha(pal.Warning.Fg, 0.85)
	lightOverlaySubtleColor := specAlpha(pal.Warning.Fg, 0.45)
	occluderOverlayColor := specAlpha(pal.Agent.Fg, 0.9)
	occluderEdgeMidFillColor := CompositeOver(specAlpha(pal.TextPrimary, 0.65), pal.Agent.Fg)
	gizmoColorRing := specAlpha(pal.Accent.Edge, 0.8)
	gizmoColorCenter := pal.TextPrimary
	gizmoColorActive := pal.Warning.Fg

	active := e.drag2D.handle
	pick := func(h GizmoHandle2D, c core.Color) core.Color {
		if h == active {
			return gizmoColorActive
		}
		return c
	}

	if lg, ok := e.selectedLight2DGizmo(); ok {
		d.Save()
		dbg := lg.dbg
		// Draw dashed radius ring (or spotlight cone boundary) in object-local coordinates projected onto canvas.
		const segs = 48
		d.SetStrokeColor(lightOverlayColor).SetLineWidth(1.5).SetLineDash(5, 4)
		if lg.hasSpot {
			half := float64(dbg.Angle) * 0.5
			startA := float64(dbg.Direction) - half
			endA := float64(dbg.Direction) + half
			d.BeginPath()
			d.MoveTo(lg.center.X, lg.center.Y)
			for i := 0; i <= segs; i++ {
				a := startA + (endA-startA)*float64(i)/float64(segs)
				s, c := math.Sincos(a)
				p := lg.toCanvas.TransformPoint(math2d.Vec2{
					X: dbg.OffsetX + dbg.Radius*float32(c),
					Y: dbg.OffsetY + dbg.Radius*float32(s),
				})
				d.LineTo(p.X, p.Y)
			}
			d.ClosePath().Stroke()
			// Center aim line.
			d.SetStrokeColor(lightOverlaySubtleColor).SetLineWidth(1)
			d.BeginPath().MoveTo(lg.center.X, lg.center.Y).LineTo(lg.radiusHandle.X, lg.radiusHandle.Y).Stroke()
		} else {
			d.BeginPath()
			for i := 0; i <= segs; i++ {
				a := 2 * math.Pi * float64(i) / float64(segs)
				s, c := math.Sincos(a)
				p := lg.toCanvas.TransformPoint(math2d.Vec2{
					X: dbg.OffsetX + dbg.Radius*float32(c),
					Y: dbg.OffsetY + dbg.Radius*float32(s),
				})
				if i == 0 {
					d.MoveTo(p.X, p.Y)
				} else {
					d.LineTo(p.X, p.Y)
				}
			}
			d.ClosePath().Stroke()
		}
		d.SetLineDash()

		// Radius handle (diamond).
		drawDiamondHandle(d, lg.radiusHandle, 6, pick(Handle2DLightRadius, lightOverlayColor), gizmoColorCenter)
		if lg.hasSpot {
			for _, sp := range lg.spotAnglePos {
				drawCircleHandle(d, sp, 5, pick(Handle2DLightSpotAngle, lightOverlayColor), gizmoColorCenter)
			}
			if lg.hasSpotDir {
				drawCircleHandle(d, lg.spotDirPos, 4.5, pick(Handle2DLightSpotDirection, gizmoColorRing), gizmoColorCenter)
			}
		}
		d.Restore()
	}

	if og, ok := e.selectedOccluder2DGizmo(); ok {
		d.Save()
		if og.hasRect {
			handles := [4]GizmoHandle2D{
				Handle2DOccluderRectN,
				Handle2DOccluderRectE,
				Handle2DOccluderRectS,
				Handle2DOccluderRectW,
			}
			for i, h := range handles {
				p := og.rectHandles[i]
				d.SetFillColor(pick(h, gizmoColorCenter))
				d.FillRect(p.X-gizmoScaleHalf, p.Y-gizmoScaleHalf, 2*gizmoScaleHalf, 2*gizmoScaleHalf)
				d.SetStrokeColor(occluderOverlayColor).SetLineWidth(1.5)
				d.StrokeRect(p.X-gizmoScaleHalf, p.Y-gizmoScaleHalf, 2*gizmoScaleHalf, 2*gizmoScaleHalf)
			}
		}
		for i, mid := range og.edgeMids {
			if !og.edgeOffers[i] {
				continue
			}
			col := occluderEdgeMidFillColor
			if active == Handle2DOccluderEdgeMid && e.drag2D.vertexIndex == i+1 {
				col = gizmoColorActive
			}
			drawCircleHandle(d, mid, 3.5, occluderOverlayColor, col)
		}
		for i, v := range og.verts {
			fill := gizmoColorCenter
			if (active == Handle2DOccluderVertex || active == Handle2DOccluderEdgeMid) && e.drag2D.vertexIndex == i {
				fill = gizmoColorActive
			}
			d.SetFillColor(fill)
			d.FillRect(v.X-gizmoScaleHalf, v.Y-gizmoScaleHalf, 2*gizmoScaleHalf, 2*gizmoScaleHalf)
			d.SetStrokeColor(occluderOverlayColor).SetLineWidth(1.5)
			d.StrokeRect(v.X-gizmoScaleHalf, v.Y-gizmoScaleHalf, 2*gizmoScaleHalf, 2*gizmoScaleHalf)
		}
		d.Restore()
	}
}

func drawDiamondHandle(d *core.Drawing, c math2d.Vec2, r float32, stroke, fill core.Color) {
	d.SetFillColor(fill).SetStrokeColor(stroke).SetLineWidth(1.5)
	d.BeginPath().
		MoveTo(c.X, c.Y-r).
		LineTo(c.X+r, c.Y).
		LineTo(c.X, c.Y+r).
		LineTo(c.X-r, c.Y).
		ClosePath().
		Fill().
		Stroke()
}

func drawCircleHandle(d *core.Drawing, c math2d.Vec2, r float32, stroke, fill core.Color) {
	d.SetFillColor(fill).SetStrokeColor(stroke).SetLineWidth(1.5)
	d.BeginPath().Arc(c.X, c.Y, r, 0, 2*math.Pi, false).Fill().Stroke()
}
