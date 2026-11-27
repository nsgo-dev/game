package sceneformat

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
)

// spriteTriggerGroup coordinates one or more physics.Trigger parts belonging to a single
// sprite-shaped trigger object so enter, stay, and exit fire once per overlapping body
// even when a concave frame splits into multiple convex polygons.
type spriteTriggerGroup struct {
	ps         *PhysicsScene
	objID      string
	compName   string
	layer      physics.LayerMask
	mask       physics.LayerMask
	partBodies map[*physics.Trigger]map[*physics.Body]struct{}
	bodyCount  map[*physics.Body]int
	stayed     map[*physics.Body]struct{}
}

func newSpriteTriggerGroup(ps *PhysicsScene, objID, compName string, layer, mask physics.LayerMask) *spriteTriggerGroup {
	return &spriteTriggerGroup{
		ps:         ps,
		objID:      objID,
		compName:   compName,
		layer:      layer,
		mask:       mask,
		partBodies: make(map[*physics.Trigger]map[*physics.Body]struct{}),
		bodyCount:  make(map[*physics.Body]int),
		stayed:     make(map[*physics.Body]struct{}),
	}
}

func (g *spriteTriggerGroup) beginStep() {
	clear(g.stayed)
}

func (g *spriteTriggerGroup) emit(kind PhysicsTriggerEventKind, b *physics.Body) {
	bodyID := ""
	if b != nil {
		bodyID = b.Tag
	}
	ev := PhysicsTriggerEvent{
		Kind:      kind,
		TriggerID: g.objID,
		BodyID:    bodyID,
		Is3D:      false,
	}
	g.ps.triggerEvents = append(g.ps.triggerEvents, ev)
	if g.ps.OnTrigger != nil {
		g.ps.OnTrigger(ev)
	}
	if g.ps.Signals != nil {
		_ = g.ps.Signals.Emit(g.objID, g.compName+"."+kind.String(), ev)
	}
}

func (g *spriteTriggerGroup) makePart(shape physics.Shape) *physics.Trigger {
	var tr *physics.Trigger
	tr = &physics.Trigger{
		Tag:   g.objID,
		Shape: shape,
		Layer: g.layer,
		Mask:  g.mask,
		OnEnter: func(b *physics.Body) {
			if g.partBodies[tr] == nil {
				g.partBodies[tr] = make(map[*physics.Body]struct{})
			}
			if _, already := g.partBodies[tr][b]; already {
				return
			}
			g.partBodies[tr][b] = struct{}{}
			prev := g.bodyCount[b]
			g.bodyCount[b] = prev + 1
			if prev == 0 {
				g.emit(TriggerEnter, b)
			}
		},
		OnStay: func(b *physics.Body) {
			if _, done := g.stayed[b]; done {
				return
			}
			g.stayed[b] = struct{}{}
			g.emit(TriggerStay, b)
		},
		OnExit: func(b *physics.Body) {
			if pb := g.partBodies[tr]; pb != nil {
				if _, present := pb[b]; !present {
					return
				}
				delete(pb, b)
			}
			g.bodyCount[b]--
			if g.bodyCount[b] <= 0 {
				delete(g.bodyCount, b)
				g.emit(TriggerExit, b)
			}
		},
	}
	return tr
}

func (g *spriteTriggerGroup) removePart(tr *physics.Trigger) {
	pb := g.partBodies[tr]
	delete(g.partBodies, tr)
	for b := range pb {
		g.bodyCount[b]--
		if g.bodyCount[b] <= 0 {
			delete(g.bodyCount, b)
		}
	}
}

func firstShapePtr(shapes [][]math2d.Vec2) *math2d.Vec2 {
	if len(shapes) == 0 || len(shapes[0]) == 0 {
		return nil
	}
	return &shapes[0][0]
}

func spriteScaledOffset(bo *BuiltObject, baseOffX, baseOffY float32) (float32, float32) {
	if bo == nil || bo.Sprite == nil {
		return baseOffX, baseOffY
	}
	asx := float32(math.Abs(float64(bo.Sprite.ScaleX)))
	asy := float32(math.Abs(float64(bo.Sprite.ScaleY)))
	if asx == 0 {
		asx = 1
	}
	if asy == 0 {
		asy = 1
	}
	return baseOffX * asx, baseOffY * asy
}

func buildSpriteWorldShapes(bo *BuiltObject, baseOffX, baseOffY float32) ([]physics.Shape, float32, float32) {
	offX, offY := spriteScaledOffset(bo, baseOffX, baseOffY)
	if bo == nil || bo.Sprite == nil {
		return nil, offX, offY
	}
	polys := bo.Sprite.ShapesWorld()
	if len(polys) == 0 {
		return nil, offX, offY
	}
	shapes := make([]physics.Shape, 0, len(polys))
	for _, poly := range polys {
		if len(poly) < 3 {
			continue
		}
		pts := make([]core.Point, len(poly))
		for i, v := range poly {
			pts[i] = core.Point{X: core.Points(v.X + offX), Y: core.Points(v.Y + offY)}
		}
		if p, ok := physics.NewPolygon(pts, 0); ok {
			shapes = append(shapes, physics.PolygonShape(p))
		}
	}
	return shapes, offX, offY
}

// dynamicHullShape collapses multiple convex polygons into one convex physics.Polygon (<= MaxPolygonVertices)
// for a single dynamic physics.RigidBody while World2D retains every exact convex part.
func dynamicHullShape(shapes []physics.Shape) physics.Shape {
	if len(shapes) == 0 {
		return physics.Shape{}
	}
	if len(shapes) == 1 {
		return shapes[0]
	}
	var all []core.Point
	for _, s := range shapes {
		if s.Kind == physics.ShapePolygon {
			all = append(all, s.Polygon.Vertices[:s.Polygon.Count]...)
		}
	}
	if p, ok := physics.NewPolygon(all, 0); ok {
		return physics.PolygonShape(p)
	}
	// Hull had more than MaxPolygonVertices vertices: compute the hull and reduce to MaxPolygonVertices.
	hull := hullPoints2D(all)
	for len(hull) > physics.MaxPolygonVertices && len(hull) > 3 {
		bestIdx := 0
		bestArea := math.Inf(1)
		n := len(hull)
		for i := range n {
			a := hull[(i+n-1)%n]
			b := hull[i]
			c := hull[(i+1)%n]
			area := math.Abs((float64(b.X-a.X))*(float64(c.Y-a.Y))-(float64(b.Y-a.Y))*(float64(c.X-a.X))) * 0.5
			if area < bestArea {
				bestArea = area
				bestIdx = i
			}
		}
		hull = slices.Delete(hull, bestIdx, bestIdx+1)
	}
	if p, ok := physics.NewPolygon(hull, 0); ok {
		return physics.PolygonShape(p)
	}
	return shapes[0]
}

func hullPoints2D(pts []core.Point) []core.Point {
	sorted := slices.Clone(pts)
	slices.SortFunc(sorted, func(a, b core.Point) int {
		if a.X < b.X {
			return -1
		}
		if a.X > b.X {
			return 1
		}
		if a.Y < b.Y {
			return -1
		}
		if a.Y > b.Y {
			return 1
		}
		return 0
	})
	cross := func(o, a, b core.Point) float64 {
		return float64(a.X-o.X)*float64(b.Y-o.Y) - float64(a.Y-o.Y)*float64(b.X-o.X)
	}
	hull := make([]core.Point, 0, len(sorted)*2)
	for _, p := range sorted {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	lower := len(hull) + 1
	for i := len(sorted) - 2; i >= 0; i-- {
		p := sorted[i]
		for len(hull) >= lower && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	if len(hull) > 1 {
		hull = hull[:len(hull)-1]
	}
	return hull
}

func (b2 *physicsBinding2D) recordSpriteSnapshot(offX, offY float32) {
	if b2.bo == nil || b2.bo.Sprite == nil {
		return
	}
	sp := b2.bo.Sprite
	b2.lastFrameName = sp.Frame.Name
	b2.lastShapeCount = len(sp.Frame.Shapes)
	b2.lastShapePtr = firstShapePtr(sp.Frame.Shapes)
	b2.lastTransform = sp.Transform()
	b2.lastOffX = offX
	b2.lastOffY = offY
}

func (b2 *physicsBinding2D) spriteChanged(offX, offY float32) bool {
	if b2.bo == nil || b2.bo.Sprite == nil {
		return false
	}
	sp := b2.bo.Sprite
	return sp.Frame.Name != b2.lastFrameName ||
		len(sp.Frame.Shapes) != b2.lastShapeCount ||
		firstShapePtr(sp.Frame.Shapes) != b2.lastShapePtr ||
		sp.Transform() != b2.lastTransform ||
		offX != b2.lastOffX ||
		offY != b2.lastOffY
}

func computeLocalCentroid(wx, wy, wrot, cx, cy float32) (float32, float32) {
	dx := float64(cx - wx)
	dy := float64(cy - wy)
	s, c := math.Sincos(float64(-wrot))
	return float32(dx*c - dy*s), float32(dx*s + dy*c)
}

func originFromCentroid(cx, cy, angle, localCX, localCY float32) (float32, float32) {
	s, c := math.Sincos(float64(angle))
	rx := float64(localCX)*c - float64(localCY)*s
	ry := float64(localCX)*s + float64(localCY)*c
	return float32(float64(cx) - rx), float32(float64(cy) - ry)
}

// SyncSpriteColliders refreshes every 2D collider2d and trigger2d configured with shape: "sprite"
// whenever its sprite's frame, flips, scale, rotation, or position changed. Step calls this
// automatically before stepping physics; games querying World2D directly between steps can call it
// after swapping a sprite's frame or flip flags.
func (ps *PhysicsScene) SyncSpriteColliders() {
	if ps == nil {
		return
	}
	for _, b2 := range ps.bindings2D {
		if !b2.spriteShape || b2.bo == nil || b2.bo.Sprite == nil {
			continue
		}
		offX, offY := spriteScaledOffset(b2.bo, b2.baseOffsetX, b2.baseOffsetY)
		if !b2.spriteChanged(offX, offY) {
			continue
		}
		ps.applySpriteShapes2D(b2)
	}
}

func (ps *PhysicsScene) applySpriteShapes2D(b2 *physicsBinding2D) {
	shapes, offX, offY := buildSpriteWorldShapes(b2.bo, b2.baseOffsetX, b2.baseOffsetY)
	b2.offsetX = offX
	b2.offsetY = offY

	var primary physics.Shape
	if len(shapes) > 0 {
		primary = shapes[0]
	}

	if b2.isTrigger {
		if b2.trigger != nil {
			b2.trigger.Shape = primary
		}
		extra := shapes
		if len(extra) > 0 {
			extra = extra[1:]
		}
		ps.syncExtraTriggers(b2, extra)
		b2.recordSpriteSnapshot(offX, offY)
		return
	}

	if b2.body != nil {
		b2.body.Shape = primary
	}
	extra := shapes
	if len(extra) > 0 {
		extra = extra[1:]
	}
	ps.syncExtraBodies(b2, extra)

	if b2.rigid != nil {
		if b2.kind == "dynamic" {
			rs := dynamicHullShape(shapes)
			b2.rigid.Shape = rs
			if !rs.Empty() {
				cx, cy := rs.Centroid()
				b2.rigid.X = cx
				b2.rigid.Y = cy
				b2.rigid.Angle = b2.bo.Sprite.Rotation
				b2.centroidLocalX, b2.centroidLocalY = computeLocalCentroid(b2.bo.Sprite.X, b2.bo.Sprite.Y, b2.bo.Sprite.Rotation, cx, cy)
			} else {
				b2.centroidLocalX, b2.centroidLocalY = 0, 0
			}
		} else {
			b2.rigid.Shape = primary
			if !primary.Empty() {
				b2.rigid.X, b2.rigid.Y = primary.Centroid()
			}
			b2.rigid.Angle = b2.bo.Sprite.Rotation
			ps.syncExtraRigids(b2, extra)
		}
	}

	b2.recordSpriteSnapshot(offX, offY)
}

func (ps *PhysicsScene) syncExtraBodies(b2 *physicsBinding2D, extra []physics.Shape) {
	for i := len(extra); i < len(b2.extraBodies); i++ {
		ps.World2D.Remove(b2.extraBodies[i])
	}
	if len(b2.extraBodies) > len(extra) {
		b2.extraBodies = b2.extraBodies[:len(extra)]
	}
	for i, sh := range extra {
		if i < len(b2.extraBodies) {
			b2.extraBodies[i].Shape = sh
			continue
		}
		eb := &physics.Body{
			Tag:      b2.body.Tag,
			Shape:    sh,
			Layer:    b2.body.Layer,
			Mask:     b2.body.Mask,
			VX:       b2.body.VX,
			VY:       b2.body.VY,
			UserData: b2.bo,
		}
		ps.World2D.Add(eb)
		b2.extraBodies = append(b2.extraBodies, eb)
	}
}

func (ps *PhysicsScene) syncExtraRigids(b2 *physicsBinding2D, extra []physics.Shape) {
	for i := len(extra); i < len(b2.extraRigids); i++ {
		ps.Rigid2D.RemoveBody(b2.extraRigids[i])
	}
	if len(b2.extraRigids) > len(extra) {
		b2.extraRigids = b2.extraRigids[:len(extra)]
	}
	for i, sh := range extra {
		if i < len(b2.extraRigids) {
			b2.extraRigids[i].Shape = sh
			if !sh.Empty() {
				b2.extraRigids[i].X, b2.extraRigids[i].Y = sh.Centroid()
			}
			b2.extraRigids[i].Angle = b2.rigid.Angle
			continue
		}
		erb := physics.NewRigidBody(sh, 0)
		erb.Tag = b2.rigid.Tag
		erb.Angle = b2.rigid.Angle
		erb.Layer = b2.rigid.Layer
		erb.Mask = b2.rigid.Mask
		erb.Friction = b2.rigid.Friction
		erb.Restitution = b2.rigid.Restitution
		erb.LinearDamping = b2.rigid.LinearDamping
		erb.AngularDamping = b2.rigid.AngularDamping
		erb.UserData = b2.bo
		ps.Rigid2D.AddBody(erb)
		b2.extraRigids = append(b2.extraRigids, erb)
	}
}

func (ps *PhysicsScene) syncExtraTriggers(b2 *physicsBinding2D, extra []physics.Shape) {
	// Clear excess parts to an empty Shape rather than removing them immediately so
	// World2D.StepTriggers fires OnExit cleanly (and avoids enter/exit churn if part 0 still overlaps).
	for i := len(extra); i < len(b2.extraTriggers); i++ {
		b2.extraTriggers[i].Shape = physics.Shape{}
	}
	for i, sh := range extra {
		if i < len(b2.extraTriggers) {
			b2.extraTriggers[i].Shape = sh
			continue
		}
		etr := b2.triggerGroup.makePart(sh)
		ps.World2D.AddTrigger(etr)
		ps.Rigid2D.AddTrigger(etr)
		b2.extraTriggers = append(b2.extraTriggers, etr)
	}
}
