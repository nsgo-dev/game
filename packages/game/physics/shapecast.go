package physics

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// ShapeCastHit describes one contact encountered when sweeping a Shape along a displacement vector.
type ShapeCastHit struct {
	// Fraction is the normalized time of impact in [0, 1] along the displacement.
	Fraction float32
	// Point is the world-space contact point on the struck surface at the time of impact.
	Point core.Point
	// NormalX and NormalY form the unit surface normal at Point, pointing from the struck
	// target toward the cast shape.
	NormalX, NormalY float32
	// Body is non-nil when the hit is a body from World.Bodies.
	Body *Body
	// RigidBody is non-nil when the hit is a body from RigidWorld.Bodies.
	RigidBody *RigidBody
	// Tile is true when the hit is a tile from World.Tiles at (Col, Row).
	Tile      bool
	Col, Row  int
	TileFlags TileFlags
}

// ShapeCastOptions configures filtering for World.ShapeCastWith, World.ShapeCastAllWith,
// RigidWorld.ShapeCastWith, and RigidWorld.ShapeCastAllWith.
type ShapeCastOptions struct {
	// QueryMask selects which target kinds (bodies, solid tiles, one-way tiles) to inspect.
	// Zero defaults to QueryAll.
	QueryMask QueryMask
	// LayerMask filters bodies by their Layer bitmask. Zero defaults to LayerAll.
	LayerMask LayerMask
	// IgnoreBody optionally excludes one World body from the cast.
	IgnoreBody *Body
	// IgnoreRigidBody optionally excludes one RigidWorld body from the cast.
	IgnoreRigidBody *RigidBody
}

// ShapeCastShape sweeps moving by (dx, dy) against stationary target and returns the exact
// time of impact, contact point, and outward unit normal pointing from target toward moving.
// Initial overlaps report Fraction == 0 with the overlap contact from TestShapes.
func ShapeCastShape(moving Shape, dx, dy float32, target Shape) (ShapeCastHit, bool) {
	if moving.Empty() || target.Empty() || !isFinite32(dx) || !isFinite32(dy) {
		return ShapeCastHit{}, false
	}
	return sweepShapeVsShape(moving, float64(dx), float64(dy), target)
}

// ShapeCast sweeps shape by (dx, dy) against targets in w selected by mask and returns the
// earliest hit. Equal fractions break ties deterministically: bodies in World.Bodies order
// before tiles in row-major (Row, Col) order.
func (w *World) ShapeCast(shape Shape, dx, dy float32, mask QueryMask) (ShapeCastHit, bool) {
	return w.ShapeCastWith(shape, dx, dy, ShapeCastOptions{QueryMask: mask})
}

// ShapeCastAll sweeps shape by (dx, dy) against targets in w selected by mask and returns
// every hit sorted by ascending Fraction in [0, 1]. Equal fractions keep bodies (in World.Bodies
// order) before tiles (in row-major Row, Col order).
func (w *World) ShapeCastAll(shape Shape, dx, dy float32, mask QueryMask) []ShapeCastHit {
	return w.ShapeCastAllWith(shape, dx, dy, ShapeCastOptions{QueryMask: mask})
}

// ShapeCastLayer sweeps shape by (dx, dy) against targets in w matching mask and layerMask
// and returns the earliest hit. A zero layerMask defaults to LayerAll.
func (w *World) ShapeCastLayer(shape Shape, dx, dy float32, mask QueryMask, layerMask LayerMask) (ShapeCastHit, bool) {
	return w.ShapeCastWith(shape, dx, dy, ShapeCastOptions{QueryMask: mask, LayerMask: layerMask})
}

// ShapeCastAllLayer sweeps shape by (dx, dy) against targets in w matching mask and layerMask
// and returns every hit sorted by ascending Fraction in [0, 1]. A zero layerMask defaults to LayerAll.
func (w *World) ShapeCastAllLayer(shape Shape, dx, dy float32, mask QueryMask, layerMask LayerMask) []ShapeCastHit {
	return w.ShapeCastAllWith(shape, dx, dy, ShapeCastOptions{QueryMask: mask, LayerMask: layerMask})
}

// ShapeCastWith sweeps shape by (dx, dy) against targets in w matching opts and returns the
// earliest hit with deterministic tie-breaking.
func (w *World) ShapeCastWith(shape Shape, dx, dy float32, opts ShapeCastOptions) (ShapeCastHit, bool) {
	if w == nil || shape.Empty() || !isFinite32(dx) || !isFinite32(dy) {
		return ShapeCastHit{}, false
	}
	mask := opts.QueryMask
	if mask == 0 {
		mask = QueryAll
	}
	layerMask := effectiveLayer(opts.LayerMask)
	fx, fy := float64(dx), float64(dy)

	var best ShapeCastHit
	hasBest := false
	maxT := 1.0

	if mask&QueryBodies != 0 {
		for _, b := range w.Bodies {
			if b == nil || b == opts.IgnoreBody || b.Shape.Empty() || (b.layer()&layerMask) == 0 {
				continue
			}
			if h, ok := sweepShapeVsShape(shape, fx, fy, b.Shape); ok {
				if !hasBest || h.Fraction < best.Fraction {
					h.Body = b
					best = h
					hasBest = true
					maxT = float64(h.Fraction)
				}
			}
		}
	}

	if w.Tiles != nil && mask&(QueryTiles|QueryOneWay) != 0 {
		if th, ok := sweepTilesFirst(shape, fx, fy, w.Tiles, mask, maxT); ok {
			if !hasBest || th.Fraction < best.Fraction {
				best = th
				hasBest = true
			}
		}
	}

	return best, hasBest
}

// ShapeCastAllWith sweeps shape by (dx, dy) against targets in w matching opts and returns
// every hit sorted by ascending Fraction in [0, 1]. Equal fractions keep bodies (in World.Bodies
// order) before tiles (in row-major Row, Col order).
func (w *World) ShapeCastAllWith(shape Shape, dx, dy float32, opts ShapeCastOptions) []ShapeCastHit {
	if w == nil || shape.Empty() || !isFinite32(dx) || !isFinite32(dy) {
		return nil
	}
	mask := opts.QueryMask
	if mask == 0 {
		mask = QueryAll
	}
	layerMask := effectiveLayer(opts.LayerMask)
	fx, fy := float64(dx), float64(dy)

	var hits []ShapeCastHit
	if mask&QueryBodies != 0 {
		for _, b := range w.Bodies {
			if b == nil || b == opts.IgnoreBody || b.Shape.Empty() || (b.layer()&layerMask) == 0 {
				continue
			}
			if h, ok := sweepShapeVsShape(shape, fx, fy, b.Shape); ok {
				h.Body = b
				hits = append(hits, h)
			}
		}
	}

	if w.Tiles != nil && mask&(QueryTiles|QueryOneWay) != 0 {
		tileHits := sweepTilesAll(shape, fx, fy, w.Tiles, mask)
		hits = append(hits, tileHits...)
	}

	slices.SortStableFunc(hits, func(a, b ShapeCastHit) int {
		return cmp.Compare(a.Fraction, b.Fraction)
	})
	return hits
}

// ShapeCast sweeps shape by (dx, dy) against rigid bodies in w matching layerMask and returns
// the earliest hit. A zero layerMask defaults to LayerAll. Ties break in RigidWorld.Bodies order.
func (w *RigidWorld) ShapeCast(shape Shape, dx, dy float32, layerMask LayerMask) (ShapeCastHit, bool) {
	return w.ShapeCastWith(shape, dx, dy, ShapeCastOptions{LayerMask: layerMask})
}

// ShapeCastAll sweeps shape by (dx, dy) against rigid bodies in w matching layerMask and returns
// all hits sorted by ascending Fraction in [0, 1]. A zero layerMask defaults to LayerAll.
// Equal fractions preserve RigidWorld.Bodies order.
func (w *RigidWorld) ShapeCastAll(shape Shape, dx, dy float32, layerMask LayerMask) []ShapeCastHit {
	return w.ShapeCastAllWith(shape, dx, dy, ShapeCastOptions{LayerMask: layerMask})
}

// ShapeCastWith sweeps shape by (dx, dy) against rigid bodies in w matching opts and returns
// the earliest hit. Ties break in RigidWorld.Bodies order.
func (w *RigidWorld) ShapeCastWith(shape Shape, dx, dy float32, opts ShapeCastOptions) (ShapeCastHit, bool) {
	if w == nil || shape.Empty() || !isFinite32(dx) || !isFinite32(dy) {
		return ShapeCastHit{}, false
	}
	if opts.QueryMask != 0 && opts.QueryMask&QueryBodies == 0 {
		return ShapeCastHit{}, false
	}
	layerMask := effectiveLayer(opts.LayerMask)
	fx, fy := float64(dx), float64(dy)

	var best ShapeCastHit
	hasBest := false
	for _, b := range w.Bodies {
		if b == nil || b == opts.IgnoreRigidBody || b.Shape.Empty() || (effectiveLayer(b.Layer)&layerMask) == 0 {
			continue
		}
		if h, ok := sweepShapeVsRigidBody(shape, fx, fy, b); ok {
			if !hasBest || h.Fraction < best.Fraction {
				h.RigidBody = b
				best = h
				hasBest = true
			}
		}
	}
	return best, hasBest
}

// ShapeCastAllWith sweeps shape by (dx, dy) against rigid bodies in w matching opts and returns
// all hits sorted by ascending Fraction in [0, 1]. Equal fractions preserve RigidWorld.Bodies order.
func (w *RigidWorld) ShapeCastAllWith(shape Shape, dx, dy float32, opts ShapeCastOptions) []ShapeCastHit {
	if w == nil || shape.Empty() || !isFinite32(dx) || !isFinite32(dy) {
		return nil
	}
	if opts.QueryMask != 0 && opts.QueryMask&QueryBodies == 0 {
		return nil
	}
	layerMask := effectiveLayer(opts.LayerMask)
	fx, fy := float64(dx), float64(dy)

	var hits []ShapeCastHit
	for _, b := range w.Bodies {
		if b == nil || b == opts.IgnoreRigidBody || b.Shape.Empty() || (effectiveLayer(b.Layer)&layerMask) == 0 {
			continue
		}
		if h, ok := sweepShapeVsRigidBody(shape, fx, fy, b); ok {
			h.RigidBody = b
			hits = append(hits, h)
		}
	}
	slices.SortStableFunc(hits, func(a, b ShapeCastHit) int {
		return cmp.Compare(a.Fraction, b.Fraction)
	})
	return hits
}

func sweepShapeVsShape(moving Shape, dx, dy float64, target Shape) (ShapeCastHit, bool) {
	switch moving.Kind {
	case ShapeBox:
		switch target.Kind {
		case ShapeBox:
			return sweepBoxVsBox2D(moving.Box, dx, dy, target.Box)
		case ShapeCircle:
			return sweepBoxVsCircle2D(moving.Box, dx, dy, target.Circle)
		}
	case ShapeCircle:
		switch target.Kind {
		case ShapeBox:
			return sweepCircleVsBox2D(moving.Circle, dx, dy, target.Box)
		case ShapeCircle:
			return sweepCircleVsCircle2D(moving.Circle, dx, dy, target.Circle)
		}
	}
	return ShapeCastHit{}, false
}

func sweepShapeVsRigidBody(moving Shape, dx, dy float64, b *RigidBody) (ShapeCastHit, bool) {
	switch b.Shape.Kind {
	case ShapeCircle:
		c := Circle{X: b.X, Y: b.Y, R: b.Shape.Circle.R}
		return sweepShapeVsShape(moving, dx, dy, CircleShape(c))
	case ShapeBox:
		w, h := b.Shape.Box.W, b.Shape.Box.H
		if w <= 0 || h <= 0 {
			return ShapeCastHit{}, false
		}
		if b.Angle == 0 {
			box := Box{X: b.X - w*0.5, Y: b.Y - h*0.5, W: w, H: h}
			return sweepShapeVsShape(moving, dx, dy, BoxShape(box))
		}
		switch moving.Kind {
		case ShapeCircle:
			return sweepCircleVsOBB2D(moving.Circle, dx, dy, float64(b.X), float64(b.Y), float64(w)*0.5, float64(h)*0.5, float64(b.Angle))
		case ShapeBox:
			return sweepBoxVsOBB2D(moving.Box, dx, dy, float64(b.X), float64(b.Y), float64(w)*0.5, float64(h)*0.5, float64(b.Angle))
		}
	}
	return ShapeCastHit{}, false
}

func sweepBoxVsBox2D(a Box, dx, dy float64, b Box) (ShapeCastHit, bool) {
	if a.Empty() || b.Empty() {
		return ShapeCastHit{}, false
	}
	if c := TestBoxBox(a, b); c.Hit {
		return ShapeCastHit{
			Fraction: 0,
			Point:    core.Point{X: core.Points(c.PointX), Y: core.Points(c.PointY)},
			NormalX:  c.NormalX,
			NormalY:  c.NormalY,
		}, true
	}
	if dx == 0 && dy == 0 {
		return ShapeCastHit{}, false
	}

	ax, ay := float64(a.X), float64(a.Y)
	aw, ah := float64(a.W), float64(a.H)
	bx, by := float64(b.X), float64(b.Y)
	bw, bh := float64(b.W), float64(b.H)

	// Minkowski-expanded box for a's top-left corner (ax, ay).
	minX, maxX := bx-aw, bx+bw
	minY, maxY := by-ah, by+bh

	tEnter := math.Inf(-1)
	tExit := math.Inf(1)
	var nx, ny float32

	if dx == 0 {
		// Strict interior check prevents grazing side edges from snagging.
		if ax <= minX || ax >= maxX {
			return ShapeCastHit{}, false
		}
	} else {
		inv := 1.0 / dx
		t1 := (minX - ax) * inv
		t2 := (maxX - ax) * inv
		sign := float32(-1)
		if t1 > t2 {
			t1, t2 = t2, t1
			sign = 1
		}
		if t1 > tEnter {
			tEnter = t1
			nx, ny = sign, 0
		}
		if t2 < tExit {
			tExit = t2
		}
	}

	if dy == 0 {
		if ay <= minY || ay >= maxY {
			return ShapeCastHit{}, false
		}
	} else {
		inv := 1.0 / dy
		t1 := (minY - ay) * inv
		t2 := (maxY - ay) * inv
		sign := float32(-1)
		if t1 > t2 {
			t1, t2 = t2, t1
			sign = 1
		}
		if t1 > tEnter {
			tEnter = t1
			nx, ny = 0, sign
		}
		if t2 < tExit {
			tExit = t2
		}
	}

	// Reject misses, tangent corner kisses (tEnter >= tExit), and hits outside [0, 1].
	if tEnter >= tExit || tExit <= 0 || tEnter < 0 || tEnter > 1 || (nx == 0 && ny == 0) {
		return ShapeCastHit{}, false
	}

	axT := ax + dx*tEnter
	ayT := ay + dy*tEnter
	var px, py float64
	if nx < 0 {
		px = bx
		py = (max(ayT, by) + min(ayT+ah, by+bh)) * 0.5
	} else if nx > 0 {
		px = bx + bw
		py = (max(ayT, by) + min(ayT+ah, by+bh)) * 0.5
	} else if ny < 0 {
		py = by
		px = (max(axT, bx) + min(axT+aw, bx+bw)) * 0.5
	} else {
		py = by + bh
		px = (max(axT, bx) + min(axT+aw, bx+bw)) * 0.5
	}

	return ShapeCastHit{
		Fraction: float32(tEnter),
		Point:    core.Point{X: core.Points(px), Y: core.Points(py)},
		NormalX:  nx,
		NormalY:  ny,
	}, true
}

func sweepCircleVsCircle2D(a Circle, dx, dy float64, b Circle) (ShapeCastHit, bool) {
	if a.Empty() || b.Empty() {
		return ShapeCastHit{}, false
	}
	if c := TestCircleCircle(a, b); c.Hit {
		return ShapeCastHit{
			Fraction: 0,
			Point:    core.Point{X: core.Points(c.PointX), Y: core.Points(c.PointY)},
			NormalX:  c.NormalX,
			NormalY:  c.NormalY,
		}, true
	}
	if dx == 0 && dy == 0 {
		return ShapeCastHit{}, false
	}

	ax, ay := float64(a.X), float64(a.Y)
	bx, by, br := float64(b.X), float64(b.Y), float64(b.R)
	sumR := float64(a.R) + br

	t, nx, ny, ok := sweepPointVsDisk2D(ax, ay, dx, dy, bx, by, sumR)
	if !ok {
		return ShapeCastHit{}, false
	}
	return ShapeCastHit{
		Fraction: float32(t),
		Point:    core.Point{X: core.Points(bx + nx*br), Y: core.Points(by + ny*br)},
		NormalX:  float32(nx),
		NormalY:  float32(ny),
	}, true
}

// sweepPointVsDisk2D sweeps point (px, py) by (dx, dy) against the exterior of disk (cx, cy, r),
// assuming the point is not strictly inside the disk. Grazing tangents (disc <= 0) do not hit.
func sweepPointVsDisk2D(px, py, dx, dy, cx, cy, r float64) (t, nx, ny float64, ok bool) {
	ox, oy := px-cx, py-cy
	rSq := r * r
	distSq := ox*ox + oy*oy
	qa := dx*dx + dy*dy
	if qa == 0 {
		return 0, 0, 0, false
	}
	qb := ox*dx + oy*dy
	if qb >= 0 {
		return 0, 0, 0, false
	}
	qc := distSq - rSq
	if qc <= 0 {
		// Touching the disk surface at t == 0 and moving inward (qb < 0).
		l := math.Hypot(ox, oy)
		if l > 0 {
			nx, ny = ox/l, oy/l
		}
		return 0, nx, ny, true
	}
	disc := qb*qb - qa*qc
	if disc <= 0 {
		return 0, 0, 0, false
	}
	t = (-qb - math.Sqrt(disc)) / qa
	if t < 0 || t > 1 {
		return 0, 0, 0, false
	}
	hx := ox + dx*t
	hy := oy + dy*t
	if l := math.Hypot(hx, hy); l > 0 {
		nx, ny = hx/l, hy/l
	}
	return t, nx, ny, true
}

func sweepCircleVsBox2D(c Circle, dx, dy float64, b Box) (ShapeCastHit, bool) {
	if c.Empty() || b.Empty() {
		return ShapeCastHit{}, false
	}
	if ct := TestCircleBox(c, b); ct.Hit {
		return ShapeCastHit{
			Fraction: 0,
			Point:    core.Point{X: core.Points(ct.PointX), Y: core.Points(ct.PointY)},
			NormalX:  ct.NormalX,
			NormalY:  ct.NormalY,
		}, true
	}
	if dx == 0 && dy == 0 {
		return ShapeCastHit{}, false
	}

	cx, cy, r := float64(c.X), float64(c.Y), float64(c.R)
	minX, maxX := float64(b.X), float64(b.X)+float64(b.W)
	minY, maxY := float64(b.Y), float64(b.Y)+float64(b.H)

	bestT := 2.0
	var bestPX, bestPY, bestNX, bestNY float64
	found := false

	consider := func(t, px, py, nx, ny float64) {
		if t < 0 || t > 1 {
			return
		}
		if !found || t < bestT {
			bestT = t
			bestPX, bestPY = px, py
			bestNX, bestNY = nx, ny
			found = true
		}
	}

	// 1. Vertical faces (left/right edges of box shifted by ±r).
	if dx > 0 {
		t := (minX - r - cx) / dx
		if t >= 0 && t <= 1 {
			hy := cy + dy*t
			if hy >= minY && hy <= maxY {
				consider(t, minX, hy, -1, 0)
			}
		}
	} else if dx < 0 {
		t := (maxX + r - cx) / dx
		if t >= 0 && t <= 1 {
			hy := cy + dy*t
			if hy >= minY && hy <= maxY {
				consider(t, maxX, hy, 1, 0)
			}
		}
	}

	// 2. Horizontal faces (top/bottom edges of box shifted by ±r).
	if dy > 0 {
		t := (minY - r - cy) / dy
		if t >= 0 && t <= 1 {
			hx := cx + dx*t
			if hx >= minX && hx <= maxX {
				consider(t, hx, minY, 0, -1)
			}
		}
	} else if dy < 0 {
		t := (maxY + r - cy) / dy
		if t >= 0 && t <= 1 {
			hx := cx + dx*t
			if hx >= minX && hx <= maxX {
				consider(t, hx, maxY, 0, 1)
			}
		}
	}

	// 3. Four rounded corner disks of radius r.
	corners := [4][2]float64{
		{minX, minY},
		{maxX, minY},
		{minX, maxY},
		{maxX, maxY},
	}
	for _, corner := range corners {
		if t, nx, ny, ok := sweepPointVsDisk2D(cx, cy, dx, dy, corner[0], corner[1], r); ok {
			consider(t, corner[0], corner[1], nx, ny)
		}
	}

	if !found {
		return ShapeCastHit{}, false
	}
	return ShapeCastHit{
		Fraction: float32(bestT),
		Point:    core.Point{X: core.Points(bestPX), Y: core.Points(bestPY)},
		NormalX:  float32(bestNX),
		NormalY:  float32(bestNY),
	}, true
}

func sweepBoxVsCircle2D(a Box, dx, dy float64, c Circle) (ShapeCastHit, bool) {
	if a.Empty() || c.Empty() {
		return ShapeCastHit{}, false
	}
	if ct := TestBoxCircle(a, c); ct.Hit {
		return ShapeCastHit{
			Fraction: 0,
			Point:    core.Point{X: core.Points(ct.PointX), Y: core.Points(ct.PointY)},
			NormalX:  ct.NormalX,
			NormalY:  ct.NormalY,
		}, true
	}
	if dx == 0 && dy == 0 {
		return ShapeCastHit{}, false
	}

	h, ok := sweepCircleVsBox2D(c, -dx, -dy, a)
	if !ok {
		return ShapeCastHit{}, false
	}
	nx, ny := -h.NormalX, -h.NormalY
	if nx == 0 {
		nx = 0
	}
	if ny == 0 {
		ny = 0
	}
	return ShapeCastHit{
		Fraction: h.Fraction,
		Point: core.Point{
			X: core.Points(float64(c.X) + float64(nx)*float64(c.R)),
			Y: core.Points(float64(c.Y) + float64(ny)*float64(c.R)),
		},
		NormalX: nx,
		NormalY: ny,
	}, true
}

func sweepCircleVsOBB2D(c Circle, dx, dy, bx, by, hw, hh, angle float64) (ShapeCastHit, bool) {
	if c.Empty() || hw <= 0 || hh <= 0 {
		return ShapeCastHit{}, false
	}
	cosA, sinA := math.Cos(angle), math.Sin(angle)
	relX := float64(c.X) - bx
	relY := float64(c.Y) - by

	// Transform circle center and displacement into OBB local frame.
	lcx := relX*cosA + relY*sinA
	lcy := -relX*sinA + relY*cosA
	ldx := dx*cosA + dy*sinA
	ldy := -dx*sinA + dy*cosA

	localCircle := Circle{X: float32(lcx), Y: float32(lcy), R: c.R}
	localBox := Box{X: float32(-hw), Y: float32(-hh), W: float32(hw * 2), H: float32(hh * 2)}
	h, ok := sweepCircleVsBox2D(localCircle, ldx, ldy, localBox)
	if !ok {
		return ShapeCastHit{}, false
	}

	lpx, lpy := float64(h.Point.X), float64(h.Point.Y)
	lnx, lny := float64(h.NormalX), float64(h.NormalY)
	wpx := bx + lpx*cosA - lpy*sinA
	wpy := by + lpx*sinA + lpy*cosA
	wnx := lnx*cosA - lny*sinA
	wny := lnx*sinA + lny*cosA
	return ShapeCastHit{
		Fraction: h.Fraction,
		Point:    core.Point{X: core.Points(wpx), Y: core.Points(wpy)},
		NormalX:  float32(wnx),
		NormalY:  float32(wny),
	}, true
}

// sweepBoxVsOBB2D performs an exact 4-axis moving 2D SAT sweep for axis-aligned box a
// translating by (dx, dy) against oriented box (bx, by, hw, hh, angle).
func sweepBoxVsOBB2D(a Box, dx, dy, bx, by, hBx, hBy, angle float64) (ShapeCastHit, bool) {
	if a.Empty() || hBx <= 0 || hBy <= 0 {
		return ShapeCastHit{}, false
	}
	hAx, hAy := float64(a.W)*0.5, float64(a.H)*0.5
	aCX, aCY := float64(a.X)+hAx, float64(a.Y)+hAy

	cosB, sinB := math.Cos(angle), math.Sin(angle)
	uA, vA := vec2{1, 0}, vec2{0, 1}
	uB, vB := vec2{cosB, sinB}, vec2{-sinB, cosB}

	// Check initial overlap via 2D SAT manifold (normal in collideRigidOBBOBB points A -> B,
	// so we flip to B -> A).
	tmpA := RigidBody{Shape: BoxShape(a), X: float32(aCX), Y: float32(aCY)}
	tmpB := RigidBody{Shape: BoxShape(Box{W: float32(hBx * 2), H: float32(hBy * 2)}), X: float32(bx), Y: float32(by), Angle: float32(angle)}
	if m, hit := collideRigidOBBOBB(&tmpA, &tmpB); hit {
		px, py := m.points[0].px, m.points[0].py
		if m.count == 2 {
			px = (m.points[0].px + m.points[1].px) * 0.5
			py = (m.points[0].py + m.points[1].py) * 0.5
		}
		return ShapeCastHit{
			Fraction: 0,
			Point:    core.Point{X: core.Points(px), Y: core.Points(py)},
			NormalX:  float32(-m.nx),
			NormalY:  float32(-m.ny),
		}, true
	}
	if dx == 0 && dy == 0 {
		return ShapeCastHit{}, false
	}

	axes := [4]vec2{uA, vA, uB, vB}
	rel := vec2{aCX - bx, aCY - by}
	vel := vec2{dx, dy}

	tEnter := math.Inf(-1)
	tExit := math.Inf(1)
	var hitNormal vec2

	for _, axis := range axes {
		rA := hAx*math.Abs(dot2(uA, axis)) + hAy*math.Abs(dot2(vA, axis))
		rB := hBx*math.Abs(dot2(uB, axis)) + hBy*math.Abs(dot2(vB, axis))
		rSum := rA + rB
		d0 := dot2(rel, axis)
		dv := dot2(vel, axis)

		if math.Abs(dv) <= 1e-12 {
			if d0 <= -rSum || d0 >= rSum {
				return ShapeCastHit{}, false
			}
			continue
		}
		inv := 1.0 / dv
		t1 := (-rSum - d0) * inv
		t2 := (rSum - d0) * inv
		n := vec2{-axis.x, -axis.y}
		if t1 > t2 {
			t1, t2 = t2, t1
			n = axis
		}
		if t1 > tEnter {
			tEnter = t1
			hitNormal = n
		}
		if t2 < tExit {
			tExit = t2
		}
		if tEnter >= tExit {
			return ShapeCastHit{}, false
		}
	}

	if tEnter >= tExit || tExit <= 0 || tEnter < 0 || tEnter > 1 || (hitNormal.x == 0 && hitNormal.y == 0) {
		return ShapeCastHit{}, false
	}

	// Compute the exact contact point at tEnter along the support features facing hitNormal.
	aAt := vec2{aCX + dx*tEnter, aCY + dy*tEnter}
	bPos := vec2{bx, by}
	px, py := obb2DContactPoint(aAt, uA, vA, hAx, hAy, bPos, uB, vB, hBx, hBy, hitNormal)
	return ShapeCastHit{
		Fraction: float32(tEnter),
		Point:    core.Point{X: core.Points(px), Y: core.Points(py)},
		NormalX:  float32(hitNormal.x),
		NormalY:  float32(hitNormal.y),
	}, true
}

// obb2DContactPoint returns the world-space contact point between touching 2D OBBs A and B
// along unit normal n (pointing from B toward A).
func obb2DContactPoint(cA, uA, vA vec2, hAx, hAy float64, cB, uB, vB vec2, hBx, hBy float64, n vec2) (float64, float64) {
	tangent := vec2{-n.y, n.x}
	aMin, aMax, aPts := obb2DSupportRange(cA, uA, vA, hAx, hAy, vec2{-n.x, -n.y}, tangent)
	bMin, bMax, bPts := obb2DSupportRange(cB, uB, vB, hBx, hBy, n, tangent)

	ovLo := max(aMin, bMin)
	ovHi := min(aMax, bMax)
	tMid := (ovLo + ovHi) * 0.5

	// Project onto B's support plane along n.
	planeOffset := dot2(bPts[0], n)
	if len(bPts) == 1 && len(aPts) == 1 {
		return bPts[0].x, bPts[0].y
	}
	return n.x*planeOffset + tangent.x*tMid, n.y*planeOffset + tangent.y*tMid
}

func obb2DSupportRange(c, u, v vec2, hx, hy float64, dir, tangent vec2) (lo, hi float64, pts []vec2) {
	corners := [4]vec2{
		{c.x - u.x*hx - v.x*hy, c.y - u.y*hx - v.y*hy},
		{c.x + u.x*hx - v.x*hy, c.y + u.y*hx - v.y*hy},
		{c.x + u.x*hx + v.x*hy, c.y + u.y*hx + v.y*hy},
		{c.x - u.x*hx + v.x*hy, c.y - u.y*hx + v.y*hy},
	}
	maxProj := math.Inf(-1)
	var projs [4]float64
	for i, p := range corners {
		projs[i] = dot2(p, dir)
		if projs[i] > maxProj {
			maxProj = projs[i]
		}
	}
	tol := 1e-5 * max(1.0, hx+hy)
	lo = math.Inf(1)
	hi = math.Inf(-1)
	for i, p := range corners {
		if projs[i] >= maxProj-tol {
			pts = append(pts, p)
			tp := dot2(p, tangent)
			if tp < lo {
				lo = tp
			}
			if tp > hi {
				hi = tp
			}
		}
	}
	return lo, hi, pts
}

func tileHitMatches(flags TileFlags, mask QueryMask, shape Box, dy float64, tileTop float64, hit ShapeCastHit) bool {
	if flags&TileSolid != 0 && mask&QueryTiles != 0 {
		return true
	}
	if flags&TileOneWay != 0 && mask&QueryOneWay != 0 {
		oneWayEps := max(1e-4, 1e-5*math.Abs(tileTop))
		if dy > 0 && float64(shape.Y)+float64(shape.H) <= tileTop+oneWayEps && hit.NormalY < 0 {
			return true
		}
	}
	return false
}

func tileNormalOccluded(grid TileGrid, mask QueryMask, col, row int, nx, ny float32) bool {
	if mask&QueryTiles == 0 {
		return false
	}
	if nx == -1 && ny == 0 {
		return grid.TileFlagsAt(col-1, row)&TileSolid != 0
	}
	if nx == 1 && ny == 0 {
		return grid.TileFlagsAt(col+1, row)&TileSolid != 0
	}
	if nx == 0 && ny == -1 {
		return grid.TileFlagsAt(col, row-1)&TileSolid != 0
	}
	if nx == 0 && ny == 1 {
		return grid.TileFlagsAt(col, row+1)&TileSolid != 0
	}
	return false
}

// forEachSweptTile visits every grid cell overlapped by the swept AABB of shape along (dx, dy)
// at most once, walking along the dominant motion axis in O(distance / tileSize) time.
// If maxT is provided (> 0 and < 1), stopEarly can prune cells beyond the earliest hit.
func forEachSweptTile(bounds Box, dx, dy float64, grid TileGrid, visit func(col, row int, minPossibleT float64) bool) {
	tileW, tileH := grid.TileSize()
	cols, rows := grid.GridSize()
	if tileW <= 0 || tileH <= 0 || cols <= 0 || rows <= 0 {
		return
	}
	tw, th := float64(tileW), float64(tileH)
	ox, oy := tileGridOrigin(grid)

	x0 := float64(bounds.X) - ox
	y0 := float64(bounds.Y) - oy
	w := float64(bounds.W)
	h := float64(bounds.H)

	sMinX := min(x0, x0+dx)
	sMaxX := max(x0, x0+dx) + w
	sMinY := min(y0, y0+dy)
	sMaxY := max(y0, y0+dy) + h

	minCol := max(0, int(math.Floor(sMinX/tw)))
	maxCol := min(cols-1, int(math.Ceil(sMaxX/tw))-1)
	minRow := max(0, int(math.Floor(sMinY/th)))
	maxRow := min(rows-1, int(math.Ceil(sMaxY/th))-1)
	if minCol > maxCol || minRow > maxRow {
		return
	}

	// For zero or short sweeps, scan row-major directly.
	if dx == 0 && dy == 0 {
		for row := minRow; row <= maxRow; row++ {
			for col := minCol; col <= maxCol; col++ {
				if !visit(col, row, 0) {
					return
				}
			}
		}
		return
	}

	if math.Abs(dx) >= math.Abs(dy) {
		colStart, colEnd, colStep := minCol, maxCol, 1
		if dx < 0 {
			colStart, colEnd, colStep = maxCol, minCol, -1
		}
		for col := colStart; ; col += colStep {
			cellLoX := float64(col) * tw
			cellHiX := float64(col+1) * tw
			tLo, tHi := intervalOverlap1D(x0, w, dx, cellLoX, cellHiX)
			if tLo <= tHi {
				ry0 := y0 + dy*tLo
				ry1 := y0 + dy*tHi
				rMin := max(minRow, int(math.Floor(min(ry0, ry1)/th)))
				rMax := min(maxRow, int(math.Ceil((max(ry0, ry1)+h)/th))-1)
				for row := rMin; row <= rMax; row++ {
					if !visit(col, row, tLo) {
						return
					}
				}
			}
			if col == colEnd {
				break
			}
		}
		return
	}

	rowStart, rowEnd, rowStep := minRow, maxRow, 1
	if dy < 0 {
		rowStart, rowEnd, rowStep = maxRow, minRow, -1
	}
	for row := rowStart; ; row += rowStep {
		cellLoY := float64(row) * th
		cellHiY := float64(row+1) * th
		tLo, tHi := intervalOverlap1D(y0, h, dy, cellLoY, cellHiY)
		if tLo <= tHi {
			rx0 := x0 + dx*tLo
			rx1 := x0 + dx*tHi
			cMin := max(minCol, int(math.Floor(min(rx0, rx1)/tw)))
			cMax := min(maxCol, int(math.Ceil((max(rx0, rx1)+w)/tw))-1)
			for col := cMin; col <= cMax; col++ {
				if !visit(col, row, tLo) {
					return
				}
			}
		}
		if row == rowEnd {
			break
		}
	}
}

// intervalOverlap1D returns the sub-interval [tLo, tHi] ⊆ [0, 1] during which
// [p0 + d*t, p0 + size + d*t] overlaps [cellLo, cellHi]. Returns (1, 0) if disjoint.
func intervalOverlap1D(p0, size, d, cellLo, cellHi float64) (float64, float64) {
	minP := cellLo - size
	maxP := cellHi
	if d == 0 {
		if p0 > maxP || p0 < minP {
			return 1, 0
		}
		return 0, 1
	}
	inv := 1.0 / d
	t1 := (minP - p0) * inv
	t2 := (maxP - p0) * inv
	if t1 > t2 {
		t1, t2 = t2, t1
	}
	tLo := max(0.0, t1)
	tHi := min(1.0, t2)
	return tLo, tHi
}

func sweepTilesFirst(shape Shape, dx, dy float64, grid TileGrid, mask QueryMask, maxT float64) (ShapeCastHit, bool) {
	tileW, tileH := grid.TileSize()
	ox, oy := tileGridOrigin(grid)
	ox32, oy32 := float32(ox), float32(oy)
	bounds := shape.Bounds()

	var best ShapeCastHit
	found := false
	bestT := maxT

	forEachSweptTile(bounds, dx, dy, grid, func(col, row int, minPossibleT float64) bool {
		if minPossibleT > bestT+1e-6 {
			return false
		}
		flags := grid.TileFlagsAt(col, row)
		if flags == 0 || !tileMatchesOverlap(flags, mask) {
			return true
		}
		tileBox := Box{
			X: ox32 + float32(col)*tileW,
			Y: oy32 + float32(row)*tileH,
			W: tileW,
			H: tileH,
		}
		h, ok := sweepShapeVsShape(shape, dx, dy, BoxShape(tileBox))
		if !ok {
			return true
		}
		if !tileHitMatches(flags, mask, bounds, dy, float64(tileBox.Y), h) {
			return true
		}
		if h.Fraction > 0 && tileNormalOccluded(grid, mask, col, row, h.NormalX, h.NormalY) {
			return true
		}
		if float64(h.Fraction) > bestT {
			return true
		}
		h.Tile = true
		h.Col = col
		h.Row = row
		h.TileFlags = flags
		if !found || h.Fraction < best.Fraction ||
			(h.Fraction == best.Fraction && (h.Row < best.Row || (h.Row == best.Row && h.Col < best.Col))) {
			best = h
			found = true
			bestT = float64(h.Fraction)
		}
		return true
	})

	return best, found
}

func sweepTilesAll(shape Shape, dx, dy float64, grid TileGrid, mask QueryMask) []ShapeCastHit {
	tileW, tileH := grid.TileSize()
	ox, oy := tileGridOrigin(grid)
	ox32, oy32 := float32(ox), float32(oy)
	bounds := shape.Bounds()

	var hits []ShapeCastHit
	forEachSweptTile(bounds, dx, dy, grid, func(col, row int, _ float64) bool {
		flags := grid.TileFlagsAt(col, row)
		if flags == 0 || !tileMatchesOverlap(flags, mask) {
			return true
		}
		tileBox := Box{
			X: ox32 + float32(col)*tileW,
			Y: oy32 + float32(row)*tileH,
			W: tileW,
			H: tileH,
		}
		h, ok := sweepShapeVsShape(shape, dx, dy, BoxShape(tileBox))
		if !ok {
			return true
		}
		if !tileHitMatches(flags, mask, bounds, dy, float64(tileBox.Y), h) {
			return true
		}
		if h.Fraction > 0 && tileNormalOccluded(grid, mask, col, row, h.NormalX, h.NormalY) {
			return true
		}
		h.Tile = true
		h.Col = col
		h.Row = row
		h.TileFlags = flags
		hits = append(hits, h)
		return true
	})

	slices.SortStableFunc(hits, func(a, b ShapeCastHit) int {
		if c := cmp.Compare(a.Fraction, b.Fraction); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Row, b.Row); c != 0 {
			return c
		}
		return cmp.Compare(a.Col, b.Col)
	})
	return hits
}
