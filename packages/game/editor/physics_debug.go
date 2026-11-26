package editor

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func physicsDebugColor(isTrigger bool, bodyKind string) core.Color {
	if isTrigger {
		return core.RGBA(0xF59E0B, 0.95)
	}
	switch bodyKind {
	case "dynamic":
		return core.RGBA(0x22C55E, 0.95)
	case "kinematic":
		return core.RGBA(0x38BDF8, 0.95)
	default:
		return core.RGBA(0x60A5FA, 0.9)
	}
}

func (e *Editor) drawPhysicsDebug2D(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if d == nil || scene == nil {
		return
	}
	if e.drawOverlayOverride == nil && !e.ShowPhysicsDebug {
		return
	}
	showCollision := e.playing || e.overlayActive(OverlayCollisionShapes)
	showTriggers := e.playing || e.overlayActive(OverlayTriggers)
	if !showCollision && !showTriggers {
		return
	}
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		var shapes []sceneformat.PhysicsDebugShape2D
		var walk func(objs []*sceneformat.BuiltObject)
		walk = func(objs []*sceneformat.BuiltObject) {
			for _, bo := range objs {
				if bo == nil || bo.Doc == nil {
					continue
				}
				for _, item := range sceneformat.ExtractPhysicsDebug2D(bo.Doc, &bo.World) {
					if item.IsTrigger && !showTriggers {
						continue
					}
					if !item.IsTrigger && !showCollision {
						continue
					}
					shapes = append(shapes, item)
				}
				if len(bo.Children) > 0 {
					walk(bo.Children)
				}
			}
		}
		walk(layer.Objects)
		if len(shapes) == 0 {
			continue
		}

		d.Save()
		if e.playing && scene.Camera == nil {
			applyAffine(d, e.layerAffine(layer.Space))
		} else {
			applyAffine(d, e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY))
		}
		for _, item := range shapes {
			drawPhysicsShape2D(d, item)
		}
		d.Restore()
	}
}

func drawPhysicsShape2D(d *core.Drawing, item sceneformat.PhysicsDebugShape2D) {
	if item.Shape.Empty() {
		return
	}
	col := physicsDebugColor(item.IsTrigger, item.BodyKind)
	d.Save()
	d.SetStrokeColor(col).SetLineWidth(1.5)
	switch item.Shape.Kind {
	case physics.ShapeBox:
		b := item.Shape.Box
		cx, cy := b.Center()
		if item.Angle != 0 {
			d.Translate(cx, cy).Rotate(item.Angle).StrokeRect(-b.W*0.5, -b.H*0.5, b.W, b.H)
		} else {
			d.StrokeRect(b.X, b.Y, b.W, b.H)
		}
		if item.IsTrigger {
			// Draw a subtle diagonal cross inside trigger boxes to distinguish sensors from solid colliders.
			d.BeginPath()
			if item.Angle != 0 {
				d.MoveTo(-b.W*0.5, -b.H*0.5).LineTo(b.W*0.5, b.H*0.5)
			} else {
				d.MoveTo(b.X, b.Y).LineTo(b.X+b.W, b.Y+b.H)
			}
			d.Stroke()
		}
	case physics.ShapeCircle:
		c := item.Shape.Circle
		d.BeginPath().Arc(c.X, c.Y, c.R, 0, float32(2*math.Pi), false)
		if !item.IsTrigger {
			cosA, sinA := float32(math.Cos(float64(item.Angle))), float32(math.Sin(float64(item.Angle)))
			d.MoveTo(c.X, c.Y).LineTo(c.X+c.R*cosA, c.Y+c.R*sinA)
		}
		d.Stroke()
	}
	d.Restore()
}

func (e *Editor) drawPhysicsDebug3D(d *core.Drawing, w, h float32) {
	if !e.ShowPhysicsDebug || d == nil || e.Doc == nil {
		return
	}
	resolver := e.physicsResolver()
	var walk func(objs []sceneformat.ObjectDoc, parentWorld math3d.Transform)
	walk = func(objs []sceneformat.ObjectDoc, parentWorld math3d.Transform) {
		for oi := range objs {
			o := &objs[oi]
			if e.clipScene != nil {
				if bo := e.clipScene.Object(o.ID); bo != nil && bo.Doc != nil {
					o = bo.Doc
				}
			}
			world := parentWorld.Mul(sceneformat.ObjectLocalTransform3D(o))
			for _, item := range sceneformat.ExtractPhysicsDebug3DAtWithResolver(o, world, resolver) {
				e.drawPhysicsShape3D(d, item, w, h)
			}
			if len(o.Children) > 0 {
				walk(o.Children, world)
			}
		}
	}
	doc := e.resolvedDoc()
	if doc == nil {
		return
	}
	for li := range doc.Layers {
		layer := &doc.Layers[li]
		if !layer.IsVisible() || layer.EffectiveSpace() == sceneformat.SpaceScreen {
			continue
		}
		walk(layer.Objects, math3d.TransformIdentity())
	}
}

const maxPhysicsDebugEdges3D = 4096

func (e *Editor) drawPhysicsShape3D(d *core.Drawing, item sceneformat.PhysicsDebugShape3D, w, h float32) {
	var edges [][2]physics3d.Vec3
	switch {
	case item.TriMesh != nil:
		edges = triMeshDebugEdges(item.TriMesh, maxPhysicsDebugEdges3D)
	case item.Heightfield != nil:
		edges = heightfieldDebugEdges(item.Heightfield, 32)
	default:
		switch item.Shape.Kind {
		case physics3d.ShapeBox:
			var boxEdges [12][2]physics3d.Vec3
			if !item.Shape.Rot.IsIdentity() {
				boxEdges = physics3d.OrientedBoxEdges(item.Shape.Box, item.Shape.Rot)
			} else {
				boxEdges = physics3d.BoxEdges(item.Shape.Box)
			}
			edges = boxEdges[:]
		case physics3d.ShapeSphere:
			edges = physics3d.SphereEdges(item.Shape.Sphere, 16)
		case physics3d.ShapeCapsule:
			edges = physics3d.CapsuleEdges(item.Shape.Capsule, 16)
		case physics3d.ShapeCylinder:
			edges = physics3d.CylinderEdges(item.Shape.Cylinder, 16)
		case physics3d.ShapeConvexHull:
			edges = physics3d.ConvexHullEdges(item.Shape.Hull, item.Shape.Rot)
		}
	}
	if len(edges) == 0 {
		return
	}
	col := physicsDebugColor(item.IsTrigger, item.BodyKind)
	d.Save().SetStrokeColor(col).SetLineWidth(1.5).BeginPath()
	hasSeg := false
	for _, seg := range edges {
		a := [3]float32{seg[0].X, seg[0].Y, seg[0].Z}
		b := [3]float32{seg[1].X, seg[1].Y, seg[1].Z}
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(a, b, w, h); vis {
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasSeg = true
		}
	}
	if hasSeg {
		d.Stroke()
	}
	d.Restore()
}

func triMeshDebugEdges(tm *physics3d.TriMeshCollider, limit int) [][2]physics3d.Vec3 {
	if tm == nil || len(tm.Triangles) == 0 {
		return nil
	}
	edges := make([][2]physics3d.Vec3, 0, min(len(tm.Triangles)*3, limit))
	for _, tri := range tm.Triangles {
		for _, seg := range [3][2]physics3d.Vec3{{tri.A, tri.B}, {tri.B, tri.C}, {tri.C, tri.A}} {
			if len(edges) >= limit {
				return edges
			}
			edges = append(edges, seg)
		}
	}
	return edges
}

func heightfieldDebugEdges(hf *physics3d.HeightfieldCollider, maxSteps int) [][2]physics3d.Vec3 {
	if hf == nil || hf.Cols < 2 || hf.Rows < 2 || len(hf.Heights) < hf.Cols*hf.Rows {
		return nil
	}
	stepX := max(1, (hf.Cols-1)/maxSteps)
	stepZ := max(1, (hf.Rows-1)/maxSteps)
	vtx := func(cx, cz int) physics3d.Vec3 {
		return physics3d.Vec3{
			X: hf.Origin.X + float32(cx)*hf.CellSize,
			Y: hf.Origin.Y + hf.Heights[cz*hf.Cols+cx],
			Z: hf.Origin.Z + float32(cz)*hf.CellSize,
		}
	}
	var edges [][2]physics3d.Vec3
	for cz := 0; cz < hf.Rows; cz += stepZ {
		for cx := 0; cx < hf.Cols-1; cx += stepX {
			nx := min(cx+stepX, hf.Cols-1)
			edges = append(edges, [2]physics3d.Vec3{vtx(cx, cz), vtx(nx, cz)})
		}
		if cz+stepZ >= hf.Rows && cz != hf.Rows-1 {
			lastZ := hf.Rows - 1
			for cx := 0; cx < hf.Cols-1; cx += stepX {
				nx := min(cx+stepX, hf.Cols-1)
				edges = append(edges, [2]physics3d.Vec3{vtx(cx, lastZ), vtx(nx, lastZ)})
			}
		}
	}
	for cx := 0; cx < hf.Cols; cx += stepX {
		for cz := 0; cz < hf.Rows-1; cz += stepZ {
			nz := min(cz+stepZ, hf.Rows-1)
			edges = append(edges, [2]physics3d.Vec3{vtx(cx, cz), vtx(cx, nz)})
		}
		if cx+stepX >= hf.Cols && cx != hf.Cols-1 {
			lastX := hf.Cols - 1
			for cz := 0; cz < hf.Rows-1; cz += stepZ {
				nz := min(cz+stepZ, hf.Rows-1)
				edges = append(edges, [2]physics3d.Vec3{vtx(lastX, cz), vtx(lastX, nz)})
			}
		}
	}
	return edges
}
