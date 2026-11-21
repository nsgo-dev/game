package tween

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// Curve is a 2D parametric curve with arc-length parameterization.
type Curve interface {
	// Point evaluates the curve at parameter t in [0, 1] (clamped).
	Point(t float32) core.Point
	// Tangent returns the unit-length tangent vector at parameter t in [0, 1] (clamped),
	// or the zero point if the curve is degenerate (zero length).
	Tangent(t float32) core.Point
	// Length returns the total arc length of the curve.
	Length() float32
	// PointAtDistance returns the point at arc-length distance dist along the curve (clamped to [0, Length()]).
	PointAtDistance(dist float32) core.Point
	// TangentAtDistance returns the unit-length tangent vector at arc-length distance dist along the curve (clamped to [0, Length()]).
	TangentAtDistance(dist float32) core.Point
	// Closed reports whether the curve forms a closed loop.
	Closed() bool
}

type vec2 struct {
	x, y float64
}

func vec2FromPoint(p core.Point) vec2 {
	return vec2{x: float64(p.X), y: float64(p.Y)}
}

func (v vec2) point() core.Point {
	return core.Point{X: core.Points(v.x), Y: core.Points(v.y)}
}

func (v vec2) add(o vec2) vec2      { return vec2{x: v.x + o.x, y: v.y + o.y} }
func (v vec2) sub(o vec2) vec2      { return vec2{x: v.x - o.x, y: v.y - o.y} }
func (v vec2) scale(s float64) vec2 { return vec2{x: v.x * s, y: v.y * s} }
func (v vec2) len() float64         { return math.Hypot(v.x, v.y) }

func (v vec2) unit() (vec2, bool) {
	l := v.len()
	if l <= 1e-12 || math.IsNaN(l) || math.IsInf(l, 0) {
		return vec2{}, false
	}
	return vec2{x: v.x / l, y: v.y / l}, true
}

func clamp01(t float64) float64 {
	if math.IsNaN(t) || t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return t
}

func clamp01f32(t float32) float64 {
	return clamp01(float64(t))
}

// 5-point Gauss-Legendre nodes and weights on [-1, 1].
var (
	gl5Nodes = [5]float64{
		-0.9061798459386640,
		-0.5384693101056831,
		0.0,
		0.5384693101056831,
		0.9061798459386640,
	}
	gl5Weights = [5]float64{
		0.2369268850561891,
		0.4786286704993665,
		0.5688888888888889,
		0.4786286704993665,
		0.2369268850561891,
	}
)

func gaussLegendre5(a, b float64, f func(float64) float64) float64 {
	if b <= a {
		return 0
	}
	mid := 0.5 * (a + b)
	half := 0.5 * (b - a)
	var sum float64
	for i := range gl5Nodes {
		sum += gl5Weights[i] * f(mid+half*gl5Nodes[i])
	}
	return half * sum
}

// arcLUT stores a cumulative arc-length table over t in [0, 1] and supports
// fast binary-search + safeguarded Newton-bisection inverse arc-length lookups.
type arcLUT struct {
	ts     []float64
	cumLen []float64
	total  float64
	speed  func(t float64) float64
}

func buildArcLUT(intervals int, speed func(t float64) float64) arcLUT {
	if intervals < 1 {
		intervals = 1
	}
	ts := make([]float64, intervals+1)
	cum := make([]float64, intervals+1)
	for i := 0; i < intervals; i++ {
		a := float64(i) / float64(intervals)
		b := float64(i+1) / float64(intervals)
		ts[i+1] = b
		cum[i+1] = cum[i] + gaussLegendre5(a, b, speed)
	}
	return arcLUT{
		ts:     ts,
		cumLen: cum,
		total:  cum[intervals],
		speed:  speed,
	}
}

func (l *arcLUT) tAtDistance(dist float64) float64 {
	if l.total <= 0 || math.IsNaN(dist) || dist <= 0 {
		return 0
	}
	if dist >= l.total {
		return 1
	}
	lo, hi := 0, len(l.cumLen)-1
	for lo+1 < hi {
		mid := int(uint(lo+hi) >> 1)
		if l.cumLen[mid] <= dist {
			lo = mid
		} else {
			hi = mid
		}
	}
	segLen := l.cumLen[lo+1] - l.cumLen[lo]
	tLo, tHi := l.ts[lo], l.ts[lo+1]
	if segLen <= 0 {
		return tLo
	}
	target := dist - l.cumLen[lo]
	t := tLo + (target/segLen)*(tHi-tLo)

	if l.speed == nil {
		return t
	}
	a, b := tLo, tHi
	tol := 1e-9 * l.total
	for range 6 {
		s := gaussLegendre5(tLo, t, l.speed)
		err := s - target
		if math.Abs(err) <= tol {
			break
		}
		if err < 0 {
			a = t
		} else {
			b = t
		}
		if b-a <= 1e-12 {
			t = 0.5 * (a + b)
			break
		}
		sp := l.speed(t)
		next := t
		if sp > 1e-12 {
			next = t - err/sp
		}
		if sp <= 1e-12 || next <= a || next >= b {
			t = 0.5 * (a + b)
		} else {
			t = next
		}
	}
	return t
}

const bezierLUTIntervals = 128

// QuadraticBezierCurve is a quadratic Bézier curve defined by three control points.
type QuadraticBezierCurve struct {
	p0, p1, p2 vec2
	lut        arcLUT
}

// QuadraticBezier returns a quadratic Bézier curve from p0 to p2 controlled by p1.
func QuadraticBezier(p0, p1, p2 core.Point) *QuadraticBezierCurve {
	c := &QuadraticBezierCurve{
		p0: vec2FromPoint(p0),
		p1: vec2FromPoint(p1),
		p2: vec2FromPoint(p2),
	}
	c.lut = buildArcLUT(bezierLUTIntervals, func(t float64) float64 {
		return c.deriv(t).len()
	})
	return c
}

func (c *QuadraticBezierCurve) eval(t float64) vec2 {
	if t <= 0 {
		return c.p0
	}
	if t >= 1 {
		return c.p2
	}
	u := 1 - t
	return vec2{
		x: u*u*c.p0.x + 2*u*t*c.p1.x + t*t*c.p2.x,
		y: u*u*c.p0.y + 2*u*t*c.p1.y + t*t*c.p2.y,
	}
}

func (c *QuadraticBezierCurve) deriv(t float64) vec2 {
	u := 1 - t
	return vec2{
		x: 2*u*(c.p1.x-c.p0.x) + 2*t*(c.p2.x-c.p1.x),
		y: 2*u*(c.p1.y-c.p0.y) + 2*t*(c.p2.y-c.p1.y),
	}
}

func (c *QuadraticBezierCurve) tangent(t float64) core.Point {
	if u, ok := c.deriv(t).unit(); ok {
		return u.point()
	}
	const eps = 1e-4
	if t+eps <= 1 {
		if u, ok := c.deriv(t + eps).unit(); ok {
			return u.point()
		}
	}
	if t-eps >= 0 {
		if u, ok := c.deriv(t - eps).unit(); ok {
			return u.point()
		}
	}
	if u, ok := c.p2.sub(c.p0).unit(); ok {
		return u.point()
	}
	return core.Point{}
}

// Point evaluates the quadratic Bézier curve at parameter t in [0, 1].
func (c *QuadraticBezierCurve) Point(t float32) core.Point {
	return c.eval(clamp01f32(t)).point()
}

// Tangent returns the unit-length tangent vector at parameter t in [0, 1].
func (c *QuadraticBezierCurve) Tangent(t float32) core.Point {
	return c.tangent(clamp01f32(t))
}

// Length returns the total arc length of the quadratic Bézier curve.
func (c *QuadraticBezierCurve) Length() float32 {
	return float32(c.lut.total)
}

// PointAtDistance returns the point at arc-length distance dist along the curve.
func (c *QuadraticBezierCurve) PointAtDistance(dist float32) core.Point {
	return c.eval(c.lut.tAtDistance(float64(dist))).point()
}

// TangentAtDistance returns the unit-length tangent vector at arc-length distance dist along the curve.
func (c *QuadraticBezierCurve) TangentAtDistance(dist float32) core.Point {
	return c.tangent(c.lut.tAtDistance(float64(dist)))
}

// Closed reports false because a quadratic Bézier curve is open.
func (c *QuadraticBezierCurve) Closed() bool { return false }

// CubicBezierCurve is a cubic Bézier curve defined by four control points.
type CubicBezierCurve struct {
	p0, p1, p2, p3 vec2
	lut            arcLUT
}

// CubicBezier returns a cubic Bézier curve from p0 to p3 with control points p1 and p2.
func CubicBezier(p0, p1, p2, p3 core.Point) *CubicBezierCurve {
	c := &CubicBezierCurve{
		p0: vec2FromPoint(p0),
		p1: vec2FromPoint(p1),
		p2: vec2FromPoint(p2),
		p3: vec2FromPoint(p3),
	}
	c.lut = buildArcLUT(bezierLUTIntervals, func(t float64) float64 {
		return c.deriv(t).len()
	})
	return c
}

func (c *CubicBezierCurve) eval(t float64) vec2 {
	if t <= 0 {
		return c.p0
	}
	if t >= 1 {
		return c.p3
	}
	u := 1 - t
	u2 := u * u
	t2 := t * t
	return vec2{
		x: u2*u*c.p0.x + 3*u2*t*c.p1.x + 3*u*t2*c.p2.x + t2*t*c.p3.x,
		y: u2*u*c.p0.y + 3*u2*t*c.p1.y + 3*u*t2*c.p2.y + t2*t*c.p3.y,
	}
}

func (c *CubicBezierCurve) deriv(t float64) vec2 {
	u := 1 - t
	return vec2{
		x: 3*u*u*(c.p1.x-c.p0.x) + 6*u*t*(c.p2.x-c.p1.x) + 3*t*t*(c.p3.x-c.p2.x),
		y: 3*u*u*(c.p1.y-c.p0.y) + 6*u*t*(c.p2.y-c.p1.y) + 3*t*t*(c.p3.y-c.p2.y),
	}
}

func (c *CubicBezierCurve) tangent(t float64) core.Point {
	if u, ok := c.deriv(t).unit(); ok {
		return u.point()
	}
	if t == 0 {
		if u, ok := c.p2.sub(c.p0).unit(); ok {
			return u.point()
		}
		if u, ok := c.p3.sub(c.p0).unit(); ok {
			return u.point()
		}
	}
	if t == 1 {
		if u, ok := c.p3.sub(c.p1).unit(); ok {
			return u.point()
		}
		if u, ok := c.p3.sub(c.p0).unit(); ok {
			return u.point()
		}
	}
	const eps = 1e-4
	if t+eps <= 1 {
		if u, ok := c.deriv(t + eps).unit(); ok {
			return u.point()
		}
	}
	if t-eps >= 0 {
		if u, ok := c.deriv(t - eps).unit(); ok {
			return u.point()
		}
	}
	if u, ok := c.p3.sub(c.p0).unit(); ok {
		return u.point()
	}
	return core.Point{}
}

// Point evaluates the cubic Bézier curve at parameter t in [0, 1].
func (c *CubicBezierCurve) Point(t float32) core.Point {
	return c.eval(clamp01f32(t)).point()
}

// Tangent returns the unit-length tangent vector at parameter t in [0, 1].
func (c *CubicBezierCurve) Tangent(t float32) core.Point {
	return c.tangent(clamp01f32(t))
}

// Length returns the total arc length of the cubic Bézier curve.
func (c *CubicBezierCurve) Length() float32 {
	return float32(c.lut.total)
}

// PointAtDistance returns the point at arc-length distance dist along the curve.
func (c *CubicBezierCurve) PointAtDistance(dist float32) core.Point {
	return c.eval(c.lut.tAtDistance(float64(dist))).point()
}

// TangentAtDistance returns the unit-length tangent vector at arc-length distance dist along the curve.
func (c *CubicBezierCurve) TangentAtDistance(dist float32) core.Point {
	return c.tangent(c.lut.tAtDistance(float64(dist)))
}

// Closed reports false because a cubic Bézier curve is open.
func (c *CubicBezierCurve) Closed() bool { return false }

type cubicSeg struct {
	a, b, c, d vec2
}

func (s cubicSeg) eval(u float64) vec2 {
	return vec2{
		x: ((s.a.x*u+s.b.x)*u+s.c.x)*u + s.d.x,
		y: ((s.a.y*u+s.b.y)*u+s.c.y)*u + s.d.y,
	}
}

func (s cubicSeg) deriv(u float64) vec2 {
	return vec2{
		x: (3*s.a.x*u+2*s.b.x)*u + s.c.x,
		y: (3*s.a.y*u+2*s.b.y)*u + s.c.y,
	}
}

func knotDelta(p, q vec2, alpha float64) float64 {
	if alpha <= 0 {
		return 1
	}
	d := q.sub(p).len()
	if d <= 1e-12 {
		return 0
	}
	return math.Pow(d, alpha)
}

func makeCatmullRomSegment(p0, p1, p2, p3 vec2, alpha float64) cubicSeg {
	d01 := knotDelta(p0, p1, alpha)
	d12 := knotDelta(p1, p2, alpha)
	d23 := knotDelta(p2, p3, alpha)

	p12 := p2.sub(p1)

	var t1 vec2
	if d01 <= 1e-12 || d12 <= 1e-12 {
		t1 = p12
	} else {
		// m1 = ((d12/(d01+d12))*(p1-p0)/d01 + (d01/(d01+d12))*(p2-p1)/d12)
		// T1 = d12 * m1
		w0 := (d12 * d12) / (d01 * (d01 + d12))
		w1 := d01 / (d01 + d12)
		t1 = p1.sub(p0).scale(w0).add(p12.scale(w1))
	}

	var t2 vec2
	if d23 <= 1e-12 || d12 <= 1e-12 {
		t2 = p12
	} else {
		// m2 = ((d23/(d12+d23))*(p2-p1)/d12 + (d12/(d12+d23))*(p3-p2)/d23)
		// T2 = d12 * m2
		w1 := d23 / (d12 + d23)
		w2 := (d12 * d12) / (d23 * (d12 + d23))
		t2 = p12.scale(w1).add(p3.sub(p2).scale(w2))
	}

	return cubicSeg{
		a: p12.scale(-2).add(t1).add(t2),
		b: p12.scale(3).sub(t1.scale(2)).sub(t2),
		c: t1,
		d: p1,
	}
}

// CatmullRomCurve is a C1-continuous Catmull-Rom spline passing through all control points.
type CatmullRomCurve struct {
	pts      []vec2
	segments []cubicSeg
	closed   bool
	lut      arcLUT
}

// NewCatmullRom returns a centripetal Catmull-Rom spline (alpha = 0.5) passing through points.
func NewCatmullRom(points []core.Point, closed bool) *CatmullRomCurve {
	return CatmullRom(points, closed, 0.5)
}

// CatmullRom returns a Catmull-Rom spline through points with parameterization exponent alpha
// (0 = uniform, 0.5 = centripetal, 1 = chordal). Negative alpha is clamped to 0.
func CatmullRom(points []core.Point, closed bool, alpha float32) *CatmullRomCurve {
	a := float64(alpha)
	if math.IsNaN(a) || a < 0 {
		a = 0
	}

	// Copy and drop consecutive coincident points to prevent zero-length knot spans.
	pts := make([]vec2, 0, len(points))
	for _, pt := range points {
		v := vec2FromPoint(pt)
		if len(pts) > 0 && v.sub(pts[len(pts)-1]).len() <= 1e-12 {
			continue
		}
		pts = append(pts, v)
	}
	if closed && len(pts) > 1 && pts[len(pts)-1].sub(pts[0]).len() <= 1e-12 {
		pts = pts[:len(pts)-1]
	}

	c := &CatmullRomCurve{
		pts:    pts,
		closed: closed,
	}
	if len(pts) < 2 {
		return c
	}

	n := len(pts)
	if closed {
		c.segments = make([]cubicSeg, n)
		for i := range n {
			p0 := pts[(i-1+n)%n]
			p1 := pts[i]
			p2 := pts[(i+1)%n]
			p3 := pts[(i+2)%n]
			c.segments[i] = makeCatmullRomSegment(p0, p1, p2, p3, a)
		}
	} else {
		c.segments = make([]cubicSeg, n-1)
		for i := range n - 1 {
			var p0, p3 vec2
			p1 := pts[i]
			p2 := pts[i+1]
			if i == 0 {
				p0 = p1.scale(2).sub(p2)
			} else {
				p0 = pts[i-1]
			}
			if i+2 >= n {
				p3 = p2.scale(2).sub(p1)
			} else {
				p3 = pts[i+2]
			}
			c.segments[i] = makeCatmullRomSegment(p0, p1, p2, p3, a)
		}
	}

	// Choose a multiple of len(segments) so every LUT bucket stays within one smooth cubic segment.
	const subPerSeg = 32
	c.lut = buildArcLUT(len(c.segments)*subPerSeg, func(t float64) float64 {
		return c.deriv(t).len()
	})
	return c
}

func (c *CatmullRomCurve) locate(t float64) (seg int, u float64) {
	m := len(c.segments)
	if t <= 0 {
		return 0, 0
	}
	if t >= 1 {
		return m - 1, 1
	}
	scaled := t * float64(m)
	seg = int(scaled)
	if seg >= m {
		seg = m - 1
		return seg, 1
	}
	return seg, scaled - float64(seg)
}

func (c *CatmullRomCurve) eval(t float64) vec2 {
	if len(c.pts) == 0 {
		return vec2{}
	}
	if len(c.segments) == 0 {
		return c.pts[0]
	}
	if t <= 0 {
		return c.pts[0]
	}
	if t >= 1 {
		if c.closed {
			return c.pts[0]
		}
		return c.pts[len(c.pts)-1]
	}
	seg, u := c.locate(t)
	return c.segments[seg].eval(u)
}

func (c *CatmullRomCurve) deriv(t float64) vec2 {
	m := len(c.segments)
	if m == 0 {
		return vec2{}
	}
	seg, u := c.locate(t)
	return c.segments[seg].deriv(u).scale(float64(m))
}

func (c *CatmullRomCurve) tangent(t float64) core.Point {
	if len(c.segments) == 0 {
		return core.Point{}
	}
	if u, ok := c.deriv(t).unit(); ok {
		return u.point()
	}
	const eps = 1e-4
	if t+eps <= 1 {
		if u, ok := c.deriv(t + eps).unit(); ok {
			return u.point()
		}
	}
	if t-eps >= 0 {
		if u, ok := c.deriv(t - eps).unit(); ok {
			return u.point()
		}
	}
	return core.Point{}
}

// Point evaluates the Catmull-Rom spline at parameter t in [0, 1].
func (c *CatmullRomCurve) Point(t float32) core.Point {
	return c.eval(clamp01f32(t)).point()
}

// Tangent returns the unit-length tangent vector at parameter t in [0, 1].
func (c *CatmullRomCurve) Tangent(t float32) core.Point {
	return c.tangent(clamp01f32(t))
}

// Length returns the total arc length of the Catmull-Rom spline.
func (c *CatmullRomCurve) Length() float32 {
	return float32(c.lut.total)
}

// PointAtDistance returns the point at arc-length distance dist along the spline.
func (c *CatmullRomCurve) PointAtDistance(dist float32) core.Point {
	if len(c.segments) == 0 {
		return c.eval(0).point()
	}
	return c.eval(c.lut.tAtDistance(float64(dist))).point()
}

// TangentAtDistance returns the unit-length tangent vector at arc-length distance dist along the spline.
func (c *CatmullRomCurve) TangentAtDistance(dist float32) core.Point {
	if len(c.segments) == 0 {
		return core.Point{}
	}
	return c.tangent(c.lut.tAtDistance(float64(dist)))
}

// Closed reports whether the Catmull-Rom spline forms a closed loop.
func (c *CatmullRomCurve) Closed() bool { return c.closed }

// PolylineCurve is a piecewise-linear curve through a sequence of vertices with exact arc-length lookup.
type PolylineCurve struct {
	verts    []vec2
	cumLen   []float64
	tangents []vec2
	total    float64
	closed   bool
}

// Polyline returns a piecewise-linear curve through points. When closed is true and
// the last point differs from the first, an explicit closing segment back to points[0] is added.
func Polyline(points []core.Point, closed bool) *PolylineCurve {
	c := &PolylineCurve{closed: closed}
	if len(points) == 0 {
		return c
	}
	verts := make([]vec2, len(points))
	for i, pt := range points {
		verts[i] = vec2FromPoint(pt)
	}
	if closed && len(verts) >= 2 && verts[len(verts)-1].sub(verts[0]).len() > 1e-12 {
		verts = append(verts, verts[0])
	}
	c.verts = verts
	if len(verts) < 2 {
		return c
	}

	nSeg := len(verts) - 1
	c.cumLen = make([]float64, len(verts))
	c.tangents = make([]vec2, nSeg)
	valid := make([]bool, nSeg)

	for i := range nSeg {
		delta := verts[i+1].sub(verts[i])
		segLen := delta.len()
		c.cumLen[i+1] = c.cumLen[i] + segLen
		if u, ok := delta.unit(); ok {
			c.tangents[i] = u
			valid[i] = true
		}
	}
	c.total = c.cumLen[nSeg]

	// Fill any zero-length segment tangents from the nearest non-degenerate neighbor.
	for i := range nSeg {
		if valid[i] {
			continue
		}
		for off := 1; off < nSeg; off++ {
			if i+off < nSeg && valid[i+off] {
				c.tangents[i] = c.tangents[i+off]
				break
			}
			if i-off >= 0 && valid[i-off] {
				c.tangents[i] = c.tangents[i-off]
				break
			}
		}
	}
	return c
}

func (c *PolylineCurve) segAtDistance(dist float64) (seg int, frac float64) {
	nSeg := len(c.tangents)
	if nSeg == 0 || c.total <= 0 || math.IsNaN(dist) || dist <= 0 {
		for i := range nSeg {
			if c.cumLen[i+1] > c.cumLen[i] {
				return i, 0
			}
		}
		return 0, 0
	}
	if dist >= c.total {
		for i := nSeg - 1; i >= 0; i-- {
			if c.cumLen[i+1] > c.cumLen[i] {
				return i, 1
			}
		}
		return nSeg - 1, 1
	}
	lo, hi := 0, nSeg
	for lo+1 < hi {
		mid := int(uint(lo+hi) >> 1)
		if c.cumLen[mid] <= dist {
			lo = mid
		} else {
			hi = mid
		}
	}
	segLen := c.cumLen[lo+1] - c.cumLen[lo]
	if segLen <= 0 {
		return lo, 0
	}
	return lo, (dist - c.cumLen[lo]) / segLen
}

// Point evaluates the polyline at arc-length fraction t in [0, 1].
func (c *PolylineCurve) Point(t float32) core.Point {
	return c.PointAtDistance(float32(clamp01f32(t) * c.total))
}

// Tangent returns the unit-length tangent vector at arc-length fraction t in [0, 1].
func (c *PolylineCurve) Tangent(t float32) core.Point {
	return c.TangentAtDistance(float32(clamp01f32(t) * c.total))
}

// Length returns the exact total length of the polyline.
func (c *PolylineCurve) Length() float32 {
	return float32(c.total)
}

// PointAtDistance returns the exact point at arc-length distance dist along the polyline.
func (c *PolylineCurve) PointAtDistance(dist float32) core.Point {
	if len(c.verts) == 0 {
		return core.Point{}
	}
	if len(c.tangents) == 0 || c.total <= 0 {
		return c.verts[0].point()
	}
	d := float64(dist)
	if math.IsNaN(d) || d <= 0 {
		return c.verts[0].point()
	}
	if d >= c.total {
		return c.verts[len(c.verts)-1].point()
	}
	seg, frac := c.segAtDistance(d)
	a, b := c.verts[seg], c.verts[seg+1]
	return vec2{
		x: a.x + (b.x-a.x)*frac,
		y: a.y + (b.y-a.y)*frac,
	}.point()
}

// TangentAtDistance returns the unit-length tangent vector at arc-length distance dist along the polyline.
func (c *PolylineCurve) TangentAtDistance(dist float32) core.Point {
	if len(c.tangents) == 0 || c.total <= 0 {
		return core.Point{}
	}
	seg, _ := c.segAtDistance(float64(dist))
	return c.tangents[seg].point()
}

// Closed reports whether the polyline forms a closed loop.
func (c *PolylineCurve) Closed() bool { return c.closed }

// PolylineFromTiledObject converts a Tiled polyline (open) or polygon (closed) object
// translated by (obj.X, obj.Y) into a PolylineCurve.
func PolylineFromTiledObject(obj tilemap.Object) *PolylineCurve {
	return Polyline(obj.WorldPoints(), obj.Shape == tilemap.ObjectPolygon)
}

// CatmullRomFromTiledObject converts a Tiled polyline (open) or polygon (closed) object
// translated by (obj.X, obj.Y) into a centripetal CatmullRomCurve.
func CatmullRomFromTiledObject(obj tilemap.Object) *CatmullRomCurve {
	return NewCatmullRom(obj.WorldPoints(), obj.Shape == tilemap.ObjectPolygon)
}

// CurveFromTiledObject converts a Tiled polyline (open) or polygon (closed) object
// translated by (obj.X, obj.Y) into a Curve: a centripetal Catmull-Rom spline when
// smooth is true, or a Polyline otherwise.
func CurveFromTiledObject(obj tilemap.Object, smooth bool) Curve {
	if smooth {
		return CatmullRomFromTiledObject(obj)
	}
	return PolylineFromTiledObject(obj)
}

// EaseFunc maps normalized pass progress t in [0, 1] to an eased progress value.
type EaseFunc func(t float32) float32

// EaseFromCurve adapts an NSGo core.Curve easing curve into an EaseFunc.
func EaseFromCurve(c core.Curve) EaseFunc {
	return func(t float32) float32 {
		return float32(c.At(float64(t)))
	}
}

// LoopMode controls what a PathFollower does on reaching the end of a curve.
type LoopMode int

const (
	// LoopNone stops at the end of the curve and fires OnComplete once.
	LoopNone LoopMode = iota
	// LoopRepeat wraps back to the beginning of the curve and continues indefinitely.
	LoopRepeat
	// LoopPingPong reverses direction at each end of the curve and oscillates indefinitely.
	LoopPingPong
)

// FollowerOptions configures a PathFollower.
type FollowerOptions struct {
	// Speed is the constant travel speed in points per second along the curve's arc length.
	// Used when Duration <= 0.
	Speed float32
	// Duration is the total travel time for one pass along the curve.
	// When Duration > 0, it takes precedence over Speed.
	Duration time.Duration
	// Ease maps normalized pass progress [0, 1] to eased arc-length fraction.
	// When nil and Curve is zero, motion proceeds linearly by arc length.
	Ease EaseFunc
	// Curve is an optional NSGo easing curve used when Ease is nil.
	Curve core.Curve
	// Loop controls playback behavior on reaching the end of the curve.
	Loop LoopMode
	// OrientToTangent computes Angle() from the curve tangent (atan2(ty, tx)) plus RotationOffset.
	// When false, Angle() returns RotationOffset.
	OrientToTangent bool
	// RotationOffset is added in radians to the heading angle.
	RotationOffset float32
	// OnUpdate is invoked whenever the follower moves or seeks with the current position and angle (radians).
	OnUpdate func(pos core.Point, angle float32)
	// OnComplete is invoked once when a LoopNone follower finishes the curve.
	OnComplete func()
}

// PathFollower advances deterministically along a Curve by arc length, maintaining
// constant speed or easing cleanly over a fixed Duration.
type PathFollower struct {
	curve Curve
	opts  FollowerOptions

	elapsed     time.Duration
	customPhase bool
	phase       float64 // monotonically increasing pass units; [0,1] for first pass
	progress    float64 // current position along curve in [0, 1]
	distance    float64
	forward     bool
	done        bool

	pos   core.Point
	tan   core.Point
	angle float32
}

// NewPathFollower returns a PathFollower positioned at the start of curve.
func NewPathFollower(curve Curve, opts FollowerOptions) *PathFollower {
	f := &PathFollower{
		curve:   curve,
		opts:    opts,
		forward: true,
	}
	f.sample(0, true)
	return f
}

func (f *PathFollower) evalEase(u float64) float64 {
	if f.opts.Ease != nil {
		return clamp01(float64(f.opts.Ease(float32(u))))
	}
	if f.opts.Curve != (core.Curve{}) {
		return clamp01(f.opts.Curve.At(u))
	}
	return u
}

func (f *PathFollower) passDurationSeconds() (float64, bool) {
	if f.opts.Duration > 0 {
		return f.opts.Duration.Seconds(), true
	}
	if f.opts.Speed > 0 {
		if f.curve == nil {
			return 0, true
		}
		length := float64(f.curve.Length())
		if length <= 0 {
			return 0, true
		}
		return length / float64(f.opts.Speed), true
	}
	return 0, false
}

func (f *PathFollower) sample(progress float64, forward bool) {
	u := clamp01(progress)
	f.progress = u
	f.forward = forward

	if f.curve == nil {
		f.distance = 0
		f.pos = core.Point{}
		f.tan = core.Point{}
		f.angle = f.opts.RotationOffset
		return
	}

	eased := f.evalEase(u)
	length := float64(f.curve.Length())
	dist := eased * length
	f.distance = dist
	f.pos = f.curve.PointAtDistance(float32(dist))
	tan := f.curve.TangentAtDistance(float32(dist))
	if f.opts.Loop == LoopPingPong && !forward {
		tan = core.Point{X: -tan.X, Y: -tan.Y}
	}
	f.tan = tan

	if f.opts.OrientToTangent && (tan.X != 0 || tan.Y != 0) {
		f.angle = float32(math.Atan2(float64(tan.Y), float64(tan.X))) + f.opts.RotationOffset
	} else {
		f.angle = f.opts.RotationOffset
	}
}

func (f *PathFollower) applyPhase(phase float64) (completed bool) {
	if math.IsNaN(phase) || phase < 0 {
		phase = 0
	}
	f.phase = phase
	switch f.opts.Loop {
	case LoopRepeat:
		u := math.Mod(phase, 1.0)
		f.sample(u, true)
		return false
	case LoopPingPong:
		cycle := math.Mod(phase, 2.0)
		if cycle <= 1.0 {
			f.sample(cycle, true)
		} else {
			f.sample(2.0-cycle, false)
		}
		return false
	default:
		if phase >= 1.0-1e-12 {
			f.phase = 1.0
			f.done = true
			f.sample(1.0, true)
			return true
		}
		f.sample(phase, true)
		return false
	}
}

// Update advances the follower by dt.
func (f *PathFollower) Update(dt time.Duration) {
	if f.done || dt <= 0 {
		return
	}
	if f.opts.Duration > 0 && !f.customPhase {
		f.elapsed = addDuration(f.elapsed, dt)
		var nextPhase float64
		if f.opts.Loop == LoopNone && f.elapsed >= f.opts.Duration {
			nextPhase = 1.0
		} else {
			nextPhase = float64(f.elapsed) / float64(f.opts.Duration)
		}
		completed := f.applyPhase(nextPhase)
		if f.opts.OnUpdate != nil {
			f.opts.OnUpdate(f.pos, f.angle)
		}
		if completed && f.opts.OnComplete != nil {
			f.opts.OnComplete()
		}
		return
	}
	f.advanceSeconds(dt.Seconds())
}

// Step advances the follower by dt seconds.
func (f *PathFollower) Step(dt float32) {
	if f.done || dt <= 0 || math.IsNaN(float64(dt)) {
		return
	}
	f.advanceSeconds(float64(dt))
}

func (f *PathFollower) advanceSeconds(dtSec float64) {
	passSec, ok := f.passDurationSeconds()
	if !ok {
		return
	}
	f.customPhase = true
	var nextPhase float64
	if passSec <= 0 {
		// Positive speed on a zero-length curve completes immediately when not looping.
		if f.opts.Loop == LoopNone {
			nextPhase = 1.0
		} else {
			nextPhase = 0.0
		}
	} else {
		nextPhase = f.phase + dtSec/passSec
	}
	completed := f.applyPhase(nextPhase)
	if f.opts.OnUpdate != nil {
		f.opts.OnUpdate(f.pos, f.angle)
	}
	if completed && f.opts.OnComplete != nil {
		f.opts.OnComplete()
	}
}

// Position returns the follower's current position along the curve.
func (f *PathFollower) Position() core.Point { return f.pos }

// Tangent returns the follower's current unit-length tangent vector.
func (f *PathFollower) Tangent() core.Point { return f.tan }

// Angle returns the follower's current heading angle in radians.
func (f *PathFollower) Angle() float32 { return f.angle }

// Distance returns the follower's current arc-length distance from the start of the curve.
func (f *PathFollower) Distance() float32 { return float32(f.distance) }

// Progress returns the follower's normalized progress in [0, 1] along the curve.
func (f *PathFollower) Progress() float32 { return float32(f.progress) }

// Done reports whether a non-looping follower has reached the end of the curve.
func (f *PathFollower) Done() bool { return f.done }

// Seek moves the follower directly to normalized progress in [0, 1] without firing OnComplete.
func (f *PathFollower) Seek(progress float32) {
	f.seekProgress(clamp01f32(progress), true)
}

func (f *PathFollower) seekProgress(u float64, notify bool) {
	u = clamp01(u)
	f.customPhase = true
	f.phase = u
	f.done = f.opts.Loop == LoopNone && u >= 1.0
	f.sample(u, true)
	if notify && f.opts.OnUpdate != nil {
		f.opts.OnUpdate(f.pos, f.angle)
	}
}

// Reset rewinds the follower back to the start of the curve so it can run again.
func (f *PathFollower) Reset() {
	f.elapsed = 0
	f.customPhase = false
	f.phase = 0
	f.done = false
	f.sample(0, true)
}

// Tween returns a *Tween backed by this follower so it integrates directly with Group and Timeline.
// It uses the follower's Duration (or Length/Speed when Duration <= 0), Loop mode, easing, and callbacks.
func (f *PathFollower) Tween() *Tween {
	var dur time.Duration
	if f.opts.Duration > 0 {
		dur = f.opts.Duration
	} else if f.opts.Speed > 0 && f.curve != nil && f.curve.Length() > 0 {
		sec := float64(f.curve.Length()) / float64(f.opts.Speed)
		dur = time.Duration(math.Round(sec * float64(time.Second)))
	}
	prevU := 0.0
	tw := &Tween{
		dur:   dur,
		curve: core.Linear,
		apply: func(t float64) {
			u := clamp01(t)
			forward := true
			if f.opts.Loop == LoopPingPong && u < prevU {
				forward = false
			}
			prevU = u
			f.customPhase = true
			f.phase = u
			f.done = f.opts.Loop == LoopNone && u >= 1.0
			f.sample(u, forward)
			if f.opts.OnUpdate != nil {
				f.opts.OnUpdate(f.pos, f.angle)
			}
		},
	}
	switch f.opts.Loop {
	case LoopRepeat:
		tw.Repeat(RepeatForever, false)
	case LoopPingPong:
		tw.Repeat(RepeatForever, true)
	default:
		if f.opts.OnComplete != nil {
			tw.OnComplete(f.opts.OnComplete)
		}
	}
	return tw
}

// Follow returns a Tween that moves along curve by arc length over d with ease,
// calling set with the current position and tangent angle (in radians) on each step.
// A zero ease uses core.Linear. Negative d is treated as 0.
func Follow(curve Curve, d time.Duration, ease core.Curve, set func(pos core.Point, angle float32)) *Tween {
	if d < 0 {
		d = 0
	}
	return &Tween{
		dur:   d,
		curve: ease,
		apply: func(t float64) {
			if set == nil {
				return
			}
			if curve == nil {
				set(core.Point{}, 0)
				return
			}
			u := clamp01(t)
			dist := float32(u * float64(curve.Length()))
			pos := curve.PointAtDistance(dist)
			tan := curve.TangentAtDistance(dist)
			var angle float32
			if tan.X != 0 || tan.Y != 0 {
				angle = float32(math.Atan2(float64(tan.Y), float64(tan.X)))
			}
			set(pos, angle)
		},
	}
}
