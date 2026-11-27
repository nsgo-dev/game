package editor

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/lights2d"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/navbake"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// navRegionVertexPickRadius is how close in canvas points a press must be to take a navregion2d polygon vertex handle.
	navRegionVertexPickRadius float32 = 8
	// navRegionEdgeMidPickRadius is how close in canvas points a press must be to take a navregion2d edge-midpoint insertion handle.
	navRegionEdgeMidPickRadius float32 = 6
	// navRegionEdgeMidMinLen is the shortest polygon edge in canvas points that offers a midpoint insertion handle.
	navRegionEdgeMidMinLen float32 = 24
)

// NavPathPreview2D holds the active 2D navigation test-path endpoints and the latest path returned by ai.NavMesh.FindPath.
type NavPathPreview2D struct {
	HasStart bool
	HasEnd   bool
	Start    core.Point
	End      core.Point
	Path     []core.Point
	Found    bool
}

type cachedNavRegionOutline struct {
	bo            *sceneformat.BuiltObject
	space         sceneformat.Space
	parallaxX     float32
	parallaxY     float32
	mode          string
	enabled       bool
	worldVertices []core.Point
}

type nav2DState struct {
	preview       NavPathPreview2D
	cachedMesh    *navbake.Mesh
	cachedPortals []navbake.Portal
	cachedScene   *sceneformat.BuiltScene
	cachedOutline []cachedNavRegionOutline
	scratchPts    []math2d.Vec2
}

func (s *nav2DState) portalsFor(m *navbake.Mesh) []navbake.Portal {
	if m == nil {
		return nil
	}
	if s.cachedMesh != m {
		s.cachedMesh = m
		s.cachedPortals = m.Portals()
	}
	return s.cachedPortals
}

func (s *nav2DState) outlinesFor(scene *sceneformat.BuiltScene) []cachedNavRegionOutline {
	if scene == nil {
		return nil
	}
	if s.cachedScene == scene {
		return s.cachedOutline
	}
	s.cachedScene = scene
	s.cachedOutline = s.cachedOutline[:0]
	for _, layer := range scene.Layers {
		if !layer.Visible || layer.Space != sceneformat.SpaceWorld {
			continue
		}
		var walk func(objs []*sceneformat.BuiltObject)
		walk = func(objs []*sceneformat.BuiltObject) {
			for _, bo := range objs {
				if bo == nil || bo.Doc == nil {
					continue
				}
				if reg, ok, err := sceneformat.ExtractNavRegion2D(bo.Doc, &bo.World); err == nil && ok && len(reg.WorldVertices) >= 2 {
					s.cachedOutline = append(s.cachedOutline, cachedNavRegionOutline{
						bo:            bo,
						space:         layer.Space,
						parallaxX:     layer.ParallaxX,
						parallaxY:     layer.ParallaxY,
						mode:          reg.Mode,
						enabled:       reg.Enabled,
						worldVertices: reg.WorldVertices,
					})
				}
				if len(bo.Children) > 0 {
					walk(bo.Children)
				}
			}
		}
		walk(layer.Objects)
	}
	return s.cachedOutline
}

// SetShowNavMesh2D toggles drawing the built 2D navigation mesh overlay in the 2D edit viewport.
func (e *Editor) SetShowNavMesh2D(show bool) {
	if e == nil || e.ShowNavMesh2D == show {
		return
	}
	e.ShowNavMesh2D = show
	if show {
		e.Status = "2D NavMesh overlay on"
	} else {
		e.Status = "2D NavMesh overlay off"
	}
	if e.settings.ViewportOverlays != nil || e.SettingsPath != "" {
		set := e.effectiveOverlaySet()
		if show {
			set[OverlayNavigation] = true
		} else {
			delete(set, OverlayNavigation)
		}
		_ = e.commitOverlaySet(set)
		return
	}
	e.notify()
}

// NavPathPreview2D returns a snapshot of the active 2D test-path endpoints and computed path.
func (e *Editor) NavPathPreview2D() NavPathPreview2D {
	if e == nil {
		return NavPathPreview2D{}
	}
	p := e.nav2D.preview
	p.Path = slices.Clone(p.Path)
	return p
}

// ClearNavPathPreview2D clears the active 2D navigation test-path endpoints and preview line.
func (e *Editor) ClearNavPathPreview2D() {
	if e == nil {
		return
	}
	if !e.nav2D.preview.HasStart && !e.nav2D.preview.HasEnd {
		return
	}
	e.nav2D.preview = NavPathPreview2D{}
	e.Status = "Cleared 2D nav path preview"
	e.notify()
}

// SetNavTestPathPoints sets both endpoints of the 2D navigation test path in world space and immediately evaluates ai.NavMesh.FindPath on the active built scene.
func (e *Editor) SetNavTestPathPoints(start, end core.Point) ([]core.Point, bool) {
	if e == nil || !isFiniteFloat32(float32(start.X)) || !isFiniteFloat32(float32(start.Y)) || !isFiniteFloat32(float32(end.X)) || !isFiniteFloat32(float32(end.Y)) {
		return nil, false
	}
	e.nav2D.preview = NavPathPreview2D{
		HasStart: true,
		HasEnd:   true,
		Start:    start,
		End:      end,
	}
	e.recomputeNavPath2D()
	if e.nav2D.preview.Found {
		e.Status = fmt.Sprintf("Nav path found (%d points)", len(e.nav2D.preview.Path))
	} else {
		e.Status = "No nav path between start and end"
	}
	e.notify()
	return slices.Clone(e.nav2D.preview.Path), e.nav2D.preview.Found
}

func (e *Editor) recomputeNavPath2D() {
	if e == nil {
		return
	}
	p := &e.nav2D.preview
	if !p.HasStart || !p.HasEnd {
		p.Path = nil
		p.Found = false
		return
	}
	scene := e.ActiveScene()
	if scene == nil || scene.NavMesh2D == nil {
		p.Path = nil
		p.Found = false
		return
	}
	path, ok := scene.NavMesh2D.FindPath(p.Start, p.End)
	p.Path = path
	p.Found = ok
}

func (e *Editor) handleNavTestPathPress(x, y float32) {
	if e == nil || e.playing || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return
	}
	wx, wy := e.CanvasToWorld(x, y)
	if !isFiniteFloat32(wx) || !isFiniteFloat32(wy) {
		return
	}
	pt := core.Point{X: core.Points(wx), Y: core.Points(wy)}
	p := &e.nav2D.preview
	if !p.HasStart || p.HasEnd {
		*p = NavPathPreview2D{
			HasStart: true,
			Start:    pt,
		}
		e.Status = "Nav test path: start set — press end point"
		e.notify()
		return
	}
	p.HasEnd = true
	p.End = pt
	e.recomputeNavPath2D()
	if p.Found {
		e.Status = fmt.Sprintf("Nav path found (%d points)", len(p.Path))
	} else {
		e.Status = "No nav path between start and end"
	}
	e.notify()
}

func defaultNavRegionPoints() [][2]float64 {
	return [][2]float64{
		{-64, -64},
		{64, -64},
		{64, 64},
		{-64, 64},
	}
}

// PlaceNavRegion2D places a new object with a built-in navregion2d component on layer (or ActiveLayer when empty) through History.
// When points is empty, a default 128x128 square centered on the object origin is used; otherwise points must contain at least 3 finite [x, y] vertices.
func (e *Editor) PlaceNavRegion2D(layer string, points [][2]float64) (string, error) {
	if e == nil || e.Doc == nil {
		return "", errors.New("editor: nil scene document")
	}
	if e.playing {
		return "", ErrPlaying
	}
	if layer == "" {
		layer = e.ActiveLayer
	}
	var pts [][2]float64
	if len(points) == 0 {
		pts = defaultNavRegionPoints()
	} else {
		if len(points) < 3 {
			return "", errors.New("editor: navregion2d polygon requires at least 3 points")
		}
		for i, p := range points {
			if math.IsNaN(p[0]) || math.IsInf(p[0], 0) || math.IsNaN(p[1]) || math.IsInf(p[1], 0) {
				return "", fmt.Errorf("editor: navregion2d point %d must be finite", i)
			}
		}
		pts = slices.Clone(points)
	}
	return e.PlaceObject(layer, sceneformat.ObjectDoc{
		Name: "NavRegion2D",
		Components: map[string]any{
			sceneformat.NavRegion2DComponentName: map[string]any{
				"version": sceneformat.NavRegion2DComponentVersion,
				"mode":    "walkable",
				"points":  pts,
			},
		},
	})
}

// InitNavRegionPolygon seeds objectID's navregion2d component with a default 128x128 square polygon through History.
func (e *Editor) InitNavRegionPolygon(objectID string) error {
	return e.SetNavRegionPoints(objectID, defaultNavRegionPoints())
}

// SetNavRegionPoints replaces the local polygon vertices of objectID's navregion2d component through History (at least 3 finite points).
func (e *Editor) SetNavRegionPoints(objectID string, points [][2]float64) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	if len(points) < 3 {
		return errors.New("editor: navregion2d polygon requires at least 3 points")
	}
	for i, p := range points {
		if math.IsNaN(p[0]) || math.IsInf(p[0], 0) || math.IsNaN(p[1]) || math.IsInf(p[1], 0) {
			return fmt.Errorf("editor: navregion2d point %d must be finite", i)
		}
	}
	cp := slices.Clone(points)
	return e.executeComponentMapEdit(objectID, sceneformat.NavRegion2DComponentName, "Edit nav region polygon of "+objectID, func(m map[string]any) {
		m["points"] = cp
	})
}

// InsertNavRegionVertex inserts a new local vertex (x, y) after vertex index afterIndex in objectID's navregion2d polygon through History.
func (e *Editor) InsertNavRegionVertex(objectID string, afterIndex int, x, y float64) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
		return errors.New("editor: navregion2d vertex must be finite")
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	reg, ok, err := sceneformat.ExtractNavRegion2D(obj, nil)
	if err != nil {
		return err
	}
	if !ok || len(reg.Points) < 2 {
		return fmt.Errorf("editor: object %q does not have a navregion2d polygon", objectID)
	}
	if afterIndex < 0 || afterIndex >= len(reg.Points) {
		return fmt.Errorf("editor: vertex index %d out of range [0, %d)", afterIndex, len(reg.Points))
	}
	next := slices.Insert(slices.Clone(reg.Points), afterIndex+1, [2]float64{x, y})
	return e.executeComponentMapEdit(objectID, sceneformat.NavRegion2DComponentName, "Insert nav region vertex on "+objectID, func(m map[string]any) {
		m["points"] = next
	})
}

// RemoveNavRegionVertex removes the vertex at index from objectID's navregion2d polygon through History.
// Removing below 3 vertices is refused.
func (e *Editor) RemoveNavRegionVertex(objectID string, index int) error {
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
	reg, ok, err := sceneformat.ExtractNavRegion2D(obj, nil)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("editor: object %q does not have a navregion2d component", objectID)
	}
	if len(reg.Points) <= 3 {
		return errors.New("editor: navregion2d polygon must keep at least 3 vertices")
	}
	if index < 0 || index >= len(reg.Points) {
		return fmt.Errorf("editor: vertex index %d out of range [0, %d)", index, len(reg.Points))
	}
	next := slices.Delete(slices.Clone(reg.Points), index, index+1)
	return e.executeComponentMapEdit(objectID, sceneformat.NavRegion2DComponentName, "Remove nav region vertex on "+objectID, func(m map[string]any) {
		m["points"] = next
	})
}

// hitTestNavRegion2D reports whether the object-local point (dx, dy) lies inside o's navregion2d polygon so clicking anywhere inside a non-sprite region selects it.
func hitTestNavRegion2D(o *sceneformat.ObjectDoc, dx, dy float32) bool {
	reg, ok, err := sceneformat.ExtractNavRegion2D(o, nil)
	if err != nil || !ok || len(reg.Points) < 3 {
		return false
	}
	lx, ly := dx-reg.OffsetX, dy-reg.OffsetY
	var buf [16]core.Point
	var poly []core.Point
	if len(reg.Points) <= len(buf) {
		poly = buf[:len(reg.Points)]
	} else {
		poly = make([]core.Point, len(reg.Points))
	}
	for i, pt := range reg.Points {
		poly[i] = core.Point{X: core.Points(pt[0]), Y: core.Points(pt[1])}
	}
	return lights2d.PointInPolygon(core.Point{X: core.Points(lx), Y: core.Points(ly)}, poly)
}

type navRegion2DGizmo struct {
	dbg        sceneformat.NavRegion2DDebug
	toCanvas   math2d.Affine
	verts      []math2d.Vec2
	edgeMids   []math2d.Vec2
	edgeOffers []bool
}

func (e *Editor) selectedNavRegion2DGizmo() (navRegion2DGizmo, bool) {
	bo, _, m, ok := e.selectedLightingContext()
	if !ok {
		return navRegion2DGizmo{}, false
	}
	dbg, has, err := sceneformat.ExtractNavRegion2D(bo.Doc, &bo.World)
	if err != nil || !has {
		return navRegion2DGizmo{}, false
	}
	g := navRegion2DGizmo{dbg: dbg, toCanvas: m}
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
				g.edgeOffers[i] = vecLen(b.Sub(a)) >= navRegionEdgeMidMinLen
			}
		}
	}
	return g, true
}

// PickNavRegionVertex2D returns the polygon vertex index under the canvas point (x, y) on the selected object's navregion2d polygon, or -1 if none.
func (e *Editor) PickNavRegionVertex2D(x, y float32) int {
	if !e.Gizmo2DShown() {
		return -1
	}
	ng, ok := e.selectedNavRegion2DGizmo()
	if !ok || len(ng.verts) == 0 {
		return -1
	}
	p := math2d.Vec2{X: x, Y: y}
	for i, v := range ng.verts {
		if vecLen(p.Sub(v)) <= navRegionVertexPickRadius {
			return i
		}
	}
	return -1
}

// PickNavRegionEdge2D returns the polygon edge index whose midpoint handle lies under the canvas point (x, y) on the selected object's navregion2d polygon, or -1 if none.
func (e *Editor) PickNavRegionEdge2D(x, y float32) int {
	if !e.Gizmo2DShown() {
		return -1
	}
	ng, ok := e.selectedNavRegion2DGizmo()
	if !ok || len(ng.edgeMids) == 0 {
		return -1
	}
	p := math2d.Vec2{X: x, Y: y}
	for i, mid := range ng.edgeMids {
		if ng.edgeOffers[i] && vecLen(p.Sub(mid)) <= navRegionEdgeMidPickRadius {
			return i
		}
	}
	return -1
}

func (e *Editor) pickNavRegionHandle2D(x, y float32) (GizmoHandle2D, int) {
	ng, ok := e.selectedNavRegion2DGizmo()
	if !ok {
		return Handle2DNone, -1
	}
	p := math2d.Vec2{X: x, Y: y}
	for i, v := range ng.verts {
		if vecLen(p.Sub(v)) <= navRegionVertexPickRadius {
			return Handle2DNavRegionVertex, i
		}
	}
	for i, mid := range ng.edgeMids {
		if ng.edgeOffers[i] && vecLen(p.Sub(mid)) <= navRegionEdgeMidPickRadius {
			return Handle2DNavRegionEdgeMid, i
		}
	}
	return Handle2DNone, -1
}

func (e *Editor) beginNavRegionDrag2D(h GizmoHandle2D, idx int, x, y float32) GizmoHandle2D {
	obj, li, _ := e.FindObject(e.SelectedID)
	if obj == nil || e.viewportFrozen(e.SelectedID) {
		return Handle2DNone
	}
	raw, ok := obj.Components[sceneformat.NavRegion2DComponentName]
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
		compName:    sceneformat.NavRegion2DComponentName,
		compBefore:  cloneMap(cm),
		vertexIndex: idx,
	}

	if h == Handle2DNavRegionEdgeMid {
		reg, has, err := sceneformat.ExtractNavRegion2D(obj, nil)
		if err != nil || !has || idx < 0 || idx >= len(reg.Points) {
			return Handle2DNone
		}
		a := reg.Points[idx]
		b := reg.Points[(idx+1)%len(reg.Points)]
		mid := [2]float64{(a[0] + b[0]) * 0.5, (a[1] + b[1]) * 0.5}
		nextPts := slices.Insert(slices.Clone(reg.Points), idx+1, mid)
		cm["points"] = nextPts
		st.vertexIndex = idx + 1
		e.nav2D.cachedScene = nil
	}

	e.drag2D = st
	e.Status = h.String() + " " + st.objectID
	e.notify()
	return h
}

func (e *Editor) updateNavRegionDrag2D(x, y float32) {
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
	reg, has, err := sceneformat.ExtractNavRegion2D(obj, nil)
	if err != nil || !has || st.vertexIndex < 0 || st.vertexIndex >= len(reg.Points) {
		return
	}
	pts := slices.Clone(reg.Points)
	vx, vy := e.SnapPosition2D(px-float64(reg.OffsetX), py-float64(reg.OffsetY))
	pts[st.vertexIndex] = [2]float64{vx, vy}
	cm["points"] = pts

	e.Rebuild()
	e.notify()
}

// drawNavMesh2DOverlay draws the baked 2D navigation mesh polygons and portals, authored navregion2d outlines, and the active test-path preview in the 2D edit viewport.
func (e *Editor) drawNavMesh2DOverlay(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if d == nil || scene == nil || e.playing {
		return
	}
	showMesh := e.overlayActive(OverlayNavigation)
	showTool := e.Tool == ToolNavTestPath
	if !showMesh && !showTool {
		return
	}
	pal := e.activeSpecPalette()
	navMesh2DFillColor := specAlpha(pal.Accent.Fg, 0.2)
	navMesh2DEdgeColor := specAlpha(pal.Accent.Edge, 0.78)
	navMesh2DPortalColor := specAlpha(pal.Agent.Fg, 0.45)
	navRegionWalkableColor := specAlpha(pal.Added.Fg, 0.92)
	navRegionWalkableSubtle := specAlpha(pal.Added.Fg, 0.5)
	navRegionObstacleColor := specAlpha(pal.Removed.Fg, 0.92)
	navRegionObstacleSubtle := specAlpha(pal.Removed.Fg, 0.5)

	// 1. Built convex navigation mesh overlay (filled polygons, outer edges, shared portals).
	if showMesh && scene.NavBake2D != nil && len(scene.NavBake2D.Polygons) > 0 {
		toCanvas := e.layerAffineFor(sceneformat.SpaceWorld, 1, 1)
		d.Save()
		applyAffine(d, toCanvas)
		lw := float32(1.25) / strokeScale(toCanvas)
		d.BeginPath()
		for _, poly := range scene.NavBake2D.Polygons {
			for k, v := range poly {
				if k == 0 {
					d.MoveTo(float32(v.X), float32(v.Y))
				} else {
					d.LineTo(float32(v.X), float32(v.Y))
				}
			}
			d.ClosePath()
		}
		d.SetFillColor(navMesh2DFillColor).Fill()
		d.SetStrokeColor(navMesh2DEdgeColor).SetLineWidth(lw).Stroke()

		portals := e.nav2D.portalsFor(scene.NavBake2D)
		if len(portals) > 0 {
			d.BeginPath()
			for _, pr := range portals {
				d.MoveTo(float32(pr.A.X), float32(pr.A.Y)).LineTo(float32(pr.B.X), float32(pr.B.Y))
			}
			d.SetStrokeColor(navMesh2DPortalColor).SetLineWidth(lw * 0.9).Stroke()
		}
		d.Restore()
	}

	// 2. Authored navregion2d polygon outlines (visible even when disabled or fully carved out).
	if showMesh {
		outlines := e.nav2D.outlinesFor(scene)
		for _, out := range outlines {
			if viewFlagged(e.hidden, out.bo) {
				continue
			}
			toCanvas := e.layerAffineFor(out.space, out.parallaxX, out.parallaxY)
			pts := e.nav2D.scratchPts[:0]
			for _, wp := range out.worldVertices {
				pts = append(pts, toCanvas.TransformPoint(math2d.Vec2{X: float32(wp.X), Y: float32(wp.Y)}))
			}
			e.nav2D.scratchPts = pts
			selected := out.bo.ID == e.SelectedID
			col := navRegionWalkableSubtle
			if out.mode == "obstacle" {
				col = navRegionObstacleSubtle
				if selected {
					col = navRegionObstacleColor
				}
			} else if selected {
				col = navRegionWalkableColor
			}
			d.Save()
			d.SetStrokeColor(col).SetLineWidth(1.5)
			if !selected || !out.enabled {
				d.SetLineDash(5, 3)
			}
			strokePolygon(d, pts)
			d.Restore()
		}
	}

	// 3. Test-path preview (start/end pins and the path ai.NavMesh.FindPath found).
	e.drawNavPathPreview2D(d)
}

func (e *Editor) drawNavPathPreview2D(d *core.Drawing) {
	p := e.nav2D.preview
	if !p.HasStart && !p.HasEnd {
		return
	}
	pal := e.activeSpecPalette()
	navPathLineColor := specAlpha(pal.Warning.Fg, 0.95)
	navPathBlockedColor := specAlpha(pal.Removed.Fg, 0.85)
	navPathStartColor := pal.Added.Fg
	navPathEndColor := pal.Changed.Fg
	gizmoColorCenter := pal.TextPrimary
	d.Save()
	if p.HasStart && p.HasEnd {
		if p.Found && len(p.Path) >= 2 {
			d.SetStrokeColor(navPathLineColor).SetLineWidth(2.5)
			d.BeginPath()
			for i, wp := range p.Path {
				cx, cy := e.WorldToCanvas(float32(wp.X), float32(wp.Y))
				if i == 0 {
					d.MoveTo(cx, cy)
				} else {
					d.LineTo(cx, cy)
				}
			}
			d.Stroke()
			for i := 1; i < len(p.Path)-1; i++ {
				cx, cy := e.WorldToCanvas(float32(p.Path[i].X), float32(p.Path[i].Y))
				drawCircleHandle(d, math2d.Vec2{X: cx, Y: cy}, 3.5, navPathLineColor, gizmoColorCenter)
			}
		} else {
			sx, sy := e.WorldToCanvas(float32(p.Start.X), float32(p.Start.Y))
			ex, ey := e.WorldToCanvas(float32(p.End.X), float32(p.End.Y))
			d.SetStrokeColor(navPathBlockedColor).SetLineWidth(2).SetLineDash(6, 4)
			d.BeginPath().MoveTo(sx, sy).LineTo(ex, ey).Stroke()
			d.SetLineDash()
		}
	}
	if p.HasStart {
		sx, sy := e.WorldToCanvas(float32(p.Start.X), float32(p.Start.Y))
		drawCircleHandle(d, math2d.Vec2{X: sx, Y: sy}, 5.5, gizmoColorCenter, navPathStartColor)
	}
	if p.HasEnd {
		ex, ey := e.WorldToCanvas(float32(p.End.X), float32(p.End.Y))
		drawCircleHandle(d, math2d.Vec2{X: ex, Y: ey}, 5.5, gizmoColorCenter, navPathEndColor)
	}
	d.Restore()
}

// drawSelectedNavRegionHandles2D draws interactive polygon vertex and edge-midpoint handles for the selected object's navregion2d component.
func (e *Editor) drawSelectedNavRegionHandles2D(d *core.Drawing) {
	ng, ok := e.selectedNavRegion2DGizmo()
	if !ok {
		return
	}
	pal := e.activeSpecPalette()
	gizmoColorCenter := pal.TextPrimary
	gizmoColorActive := pal.Warning.Fg
	active := e.drag2D.handle
	stroke := specAlpha(pal.Added.Fg, 0.92)
	midFill := CompositeOver(specAlpha(pal.TextPrimary, 0.6), pal.Added.Fg)
	if ng.dbg.Mode == "obstacle" {
		stroke = specAlpha(pal.Removed.Fg, 0.92)
		midFill = CompositeOver(specAlpha(pal.TextPrimary, 0.6), pal.Removed.Fg)
	}
	d.Save()
	for i, mid := range ng.edgeMids {
		if !ng.edgeOffers[i] {
			continue
		}
		col := midFill
		if active == Handle2DNavRegionEdgeMid && e.drag2D.vertexIndex == i+1 {
			col = gizmoColorActive
		}
		drawCircleHandle(d, mid, 3.5, stroke, col)
	}
	for i, v := range ng.verts {
		fill := gizmoColorCenter
		if (active == Handle2DNavRegionVertex || active == Handle2DNavRegionEdgeMid) && e.drag2D.vertexIndex == i {
			fill = gizmoColorActive
		}
		d.SetFillColor(fill)
		d.FillRect(v.X-gizmoScaleHalf, v.Y-gizmoScaleHalf, 2*gizmoScaleHalf, 2*gizmoScaleHalf)
		d.SetStrokeColor(stroke).SetLineWidth(1.5)
		d.StrokeRect(v.X-gizmoScaleHalf, v.Y-gizmoScaleHalf, 2*gizmoScaleHalf, 2*gizmoScaleHalf)
	}
	d.Restore()
}
