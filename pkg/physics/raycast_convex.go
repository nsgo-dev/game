package physics

import (
	"math"

	"github.com/nsgo-dev/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

// rayPiece is the nearest entry of a ray into one piece of a shape.
type rayPiece struct {
	t      float64
	nx, ny float64
	ok     bool
}

func (r *rayPiece) keep(t, nx, ny float64) {
	if !r.ok || t < r.t {
		*r = rayPiece{t: t, nx: nx, ny: ny, ok: true}
	}
}

func rayHitAt(fx, fy, dx, dy float64, p rayPiece) RaycastHit {
	return RaycastHit{
		T:       float32(p.t),
		Point:   core.Point{X: core.Points(fx + float64(dx*p.t)), Y: core.Points(fy + float64(dy*p.t))},
		NormalX: float32(p.nx),
		NormalY: float32(p.ny),
	}
}

func rayStartHit(fx, fy float64) RaycastHit {
	return RaycastHit{T: 0, Point: core.Point{X: core.Points(fx), Y: core.Points(fy)}}
}

// raycastLine intersects the ray with segment ab from either side, returning the normal facing the ray's origin. A ray parallel to the segment misses it, as in Box2D.
func raycastLine(fx, fy, dx, dy float64, a, b vec2) (rayPiece, bool) {
	ex, ey := b.x-a.x, b.y-a.y
	den := float64(dx*ey) - float64(dy*ex)
	if den == 0 {
		return rayPiece{}, false
	}
	wx, wy := a.x-fx, a.y-fy
	t := (float64(wx*ey) - float64(wy*ex)) / den
	u := (float64(wx*dy) - float64(wy*dx)) / den
	if t < 0 || t > 1 || u < 0 || u > 1 {
		return rayPiece{}, false
	}
	l := detmath.Hypot(ex, ey)
	nx, ny := ey/l, -ex/l
	if float64(nx*dx)+float64(ny*dy) > 0 {
		nx, ny = -nx, -ny
	}
	return rayPiece{t: t, nx: nx, ny: ny, ok: true}, true
}

// raycastDisk returns where the ray enters the disk at c with radius r, for a ray starting outside it.
func raycastDisk(fx, fy, dx, dy float64, c vec2, r float64) (rayPiece, bool) {
	ox, oy := fx-c.x, fy-c.y
	a := float64(dx*dx) + float64(dy*dy)
	if a == 0 {
		return rayPiece{}, false
	}
	b := float64(ox*dx) + float64(oy*dy)
	if b >= 0 {
		return rayPiece{}, false
	}
	disc := float64(b*b) - float64(a*(float64(ox*ox)+float64(oy*oy)-float64(r*r)))
	if disc < 0 {
		return rayPiece{}, false
	}
	t := (-b - math.Sqrt(disc)) / a
	if t < 0 || t > 1 {
		return rayPiece{}, false
	}
	nx, ny := (ox+float64(dx*t))/r, (oy+float64(dy*t))/r
	return rayPiece{t: t, nx: nx, ny: ny, ok: true}, true
}

// raycastHull casts the ray from (fx, fy) along (dx, dy) for t in [0, 1] against a rounded hull. A ray starting inside reports T == 0 with a zero normal, as raycastBox does.
func raycastHull(fx, fy, dx, dy float64, h *hull) (RaycastHit, bool) {
	if h.r > 0 && h.coreDistance(vec2{fx, fy}) < h.r {
		return rayStartHit(fx, fy), true
	}
	var best rayPiece
	switch {
	case h.r == 0 && h.count >= 3:
		return raycastCorePolygon(fx, fy, dx, dy, h)
	case h.r == 0:
		// A bare segment.
		if p, ok := raycastLine(fx, fy, dx, dy, h.v[0], h.v[1]); ok {
			return rayHitAt(fx, fy, dx, dy, p), true
		}
		return RaycastHit{}, false
	default:
		// The rounded hull is the union of a disk at every vertex and every face pushed out by the radius.
		for i := range h.count {
			if p, ok := raycastDisk(fx, fy, dx, dy, h.v[i], h.r); ok {
				best.keep(p.t, p.nx, p.ny)
			}
		}
		for i := range h.edgeCount() {
			n := h.n[i]
			if float64(n.x*dx)+float64(n.y*dy) >= 0 {
				continue
			}
			a, b := h.edge(i)
			oa := vec2{a.x + float64(n.x*h.r), a.y + float64(n.y*h.r)}
			ob := vec2{b.x + float64(n.x*h.r), b.y + float64(n.y*h.r)}
			if p, ok := raycastLine(fx, fy, dx, dy, oa, ob); ok {
				best.keep(p.t, n.x, n.y)
			}
		}
	}
	if !best.ok {
		return RaycastHit{}, false
	}
	return rayHitAt(fx, fy, dx, dy, best), true
}

// raycastCorePolygon clips the ray against every face of a sharp convex polygon (Cyrus-Beck).
func raycastCorePolygon(fx, fy, dx, dy float64, h *hull) (RaycastHit, bool) {
	tNear, tFar := math.Inf(-1), math.Inf(1)
	var nx, ny float64
	for i := range h.count {
		n := h.n[i]
		num := dot2(n, vec2{h.v[i].x - fx, h.v[i].y - fy})
		den := float64(n.x*dx) + float64(n.y*dy)
		if den == 0 {
			if num < 0 {
				return RaycastHit{}, false
			}
			continue
		}
		t := num / den
		if den < 0 {
			if t > tNear {
				tNear, nx, ny = t, n.x, n.y
			}
		} else if t < tFar {
			tFar = t
		}
	}
	if tNear > tFar || tFar < 0 || tNear > 1 {
		return RaycastHit{}, false
	}
	if tNear < 0 {
		if tFar == 0 {
			return RaycastHit{}, false
		}
		return rayStartHit(fx, fy), true
	}
	return rayHitAt(fx, fy, dx, dy, rayPiece{t: tNear, nx: nx, ny: ny}), true
}

// raycastChain returns the nearest crossing of the ray with any segment of c.
func raycastChain(fx, fy, dx, dy float64, c *Chain) (RaycastHit, bool) {
	if c.Empty() {
		return RaycastHit{}, false
	}
	var best rayPiece
	n := len(c.Points)
	for i := range c.SegmentCount() {
		a := vec2{float64(c.Points[i].X), float64(c.Points[i].Y)}
		b := vec2{float64(c.Points[(i+1)%n].X), float64(c.Points[(i+1)%n].Y)}
		if p, ok := raycastLine(fx, fy, dx, dy, a, b); ok {
			best.keep(p.t, p.nx, p.ny)
		}
	}
	if !best.ok {
		return RaycastHit{}, false
	}
	return rayHitAt(fx, fy, dx, dy, best), true
}
