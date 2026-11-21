package render3d

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
)

// ProjectSegment projects the 3D world-space line segment (a, b) through
// viewProj into 2D screen coordinates within viewport, clipping against the
// near plane (clip z >= 0, w >= eps) so segments crossing behind the camera
// never invert across the screen.
func ProjectSegment(a, b math3d.Vec3, viewProj math3d.Mat4, viewport core.Bounds) (ax, ay, bx, by float32, visible bool) {
	if viewport.Size.Width <= 0 || viewport.Size.Height <= 0 {
		return 0, 0, 0, 0, false
	}
	ca := viewProj.MulVec4(a.Vec4(1))
	cb := viewProj.MulVec4(b.Vec4(1))

	// Clip against near plane (clip.Z >= 0 for [0, 1] depth projections).
	if ca.Z < 0 && cb.Z < 0 {
		return 0, 0, 0, 0, false
	}
	if ca.Z < 0 {
		t := -ca.Z / (cb.Z - ca.Z)
		ca = ca.Lerp(cb, t)
	} else if cb.Z < 0 {
		t := -ca.Z / (cb.Z - ca.Z)
		cb = ca.Lerp(cb, t)
	}

	// Guard against non-positive homogeneous W.
	const minW = 1e-5
	if ca.W < minW && cb.W < minW {
		return 0, 0, 0, 0, false
	}
	if ca.W < minW {
		t := (minW - ca.W) / (cb.W - ca.W)
		ca = ca.Lerp(cb, t)
	} else if cb.W < minW {
		t := (minW - ca.W) / (cb.W - ca.W)
		cb = ca.Lerp(cb, t)
	}

	ndcAX, ndcAY := ca.X/ca.W, ca.Y/ca.W
	ndcBX, ndcBY := cb.X/cb.W, cb.Y/cb.W

	vx := float32(viewport.Origin.X)
	vy := float32(viewport.Origin.Y)
	vw := float32(viewport.Size.Width)
	vh := float32(viewport.Size.Height)

	ax = vx + (ndcAX*0.5+0.5)*vw
	ay = vy + (0.5-ndcAY*0.5)*vh
	bx = vx + (ndcBX*0.5+0.5)*vw
	by = vy + (0.5-ndcBY*0.5)*vh
	return ax, ay, bx, by, true
}

// DebugLine is a single 3D wireframe segment queued in [DebugDraw].
type DebugLine struct {
	A, B  math3d.Vec3
	Color core.Color
	Width float32
}

// DebugDraw collects 3D wireframe debug primitives (lines, boxes, spheres,
// rays, coordinate axes, grids, and camera frustums) and projects them into
// [*core.Drawing] (#286). Its backing slice is reused across frames via [DebugDraw.Clear].
type DebugDraw struct {
	// DefaultWidth is the stroke width in points used when a line has Width <= 0
	// (defaults to 1.5 when zero).
	DefaultWidth float32

	lines []DebugLine
}

// Clear resets the recorded debug lines while retaining slice capacity.
func (dd *DebugDraw) Clear() {
	dd.lines = dd.lines[:0]
}

// Lines returns the currently queued 3D wireframe segments.
func (dd *DebugDraw) Lines() []DebugLine {
	return dd.lines
}

func (dd *DebugDraw) defaultWidth() float32 {
	if dd.DefaultWidth > 0 {
		return dd.DefaultWidth
	}
	return 1.5
}

// Line appends a 3D line segment from a to b in color.
func (dd *DebugDraw) Line(a, b math3d.Vec3, color core.Color) {
	dd.lines = append(dd.lines, DebugLine{A: a, B: b, Color: color, Width: dd.defaultWidth()})
}

// LineWidth appends a 3D line segment with an explicit stroke width in points.
func (dd *DebugDraw) LineWidth(a, b math3d.Vec3, color core.Color, width float32) {
	if width <= 0 {
		width = dd.defaultWidth()
	}
	dd.lines = append(dd.lines, DebugLine{A: a, B: b, Color: color, Width: width})
}

// AABB appends the 12 wireframe edges of box. Empty boxes emit no lines.
func (dd *DebugDraw) AABB(box math3d.AABB, color core.Color) {
	if box.Empty() {
		return
	}
	lo, hi := box.Min, box.Max
	c := [8]math3d.Vec3{
		{X: lo.X, Y: lo.Y, Z: lo.Z},
		{X: hi.X, Y: lo.Y, Z: lo.Z},
		{X: hi.X, Y: hi.Y, Z: lo.Z},
		{X: lo.X, Y: hi.Y, Z: lo.Z},
		{X: lo.X, Y: lo.Y, Z: hi.Z},
		{X: hi.X, Y: lo.Y, Z: hi.Z},
		{X: hi.X, Y: hi.Y, Z: hi.Z},
		{X: lo.X, Y: hi.Y, Z: hi.Z},
	}
	edges := [12][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0}, // min Z face
		{4, 5}, {5, 6}, {6, 7}, {7, 4}, // max Z face
		{0, 4}, {1, 5}, {2, 6}, {3, 7}, // connecting edges
	}
	for _, e := range edges {
		dd.Line(c[e[0]], c[e[1]], color)
	}
}

// Sphere appends three orthogonal great circles (XY, XZ, YZ) centered at
// center with radius. Segments defaults to 24 when <= 0.
func (dd *DebugDraw) Sphere(center math3d.Vec3, radius float32, color core.Color, segments int) {
	if radius <= 0 {
		return
	}
	if segments < 6 {
		segments = 24
	}
	step := (2 * math.Pi) / float64(segments)
	for i := range segments {
		a0 := float64(i) * step
		a1 := float64(i+1) * step
		c0, s0 := float32(math.Cos(a0))*radius, float32(math.Sin(a0))*radius
		c1, s1 := float32(math.Cos(a1))*radius, float32(math.Sin(a1))*radius

		// XY circle
		dd.Line(
			center.Add(math3d.Vec3{X: c0, Y: s0}),
			center.Add(math3d.Vec3{X: c1, Y: s1}),
			color,
		)
		// XZ circle
		dd.Line(
			center.Add(math3d.Vec3{X: c0, Z: s0}),
			center.Add(math3d.Vec3{X: c1, Z: s1}),
			color,
		)
		// YZ circle
		dd.Line(
			center.Add(math3d.Vec3{Y: c0, Z: s0}),
			center.Add(math3d.Vec3{Y: c1, Z: s1}),
			color,
		)
	}
}

// Ray appends a line segment along ray of length length.
func (dd *DebugDraw) Ray(ray math3d.Ray, length float32, color core.Color) {
	if length <= 0 || ray.Dir.LenSq() == 0 {
		return
	}
	dir := ray.Dir.Norm()
	dd.Line(ray.Origin, ray.Origin.Add(dir.Scale(length)), color)
}

// Axes appends RGB right-handed X/Y/Z coordinate axes at origin.
func (dd *DebugDraw) Axes(origin math3d.Vec3, length float32) {
	if length <= 0 {
		return
	}
	dd.Line(origin, origin.Add(math3d.Vec3{X: length}), gizmoColorX)
	dd.Line(origin, origin.Add(math3d.Vec3{Y: length}), gizmoColorY)
	dd.Line(origin, origin.Add(math3d.Vec3{Z: length}), gizmoColorZ)
}

// Grid appends an XZ reference grid centered at center spanning
// [-halfExtent, +halfExtent] with divisions cells per side.
func (dd *DebugDraw) Grid(center math3d.Vec3, halfExtent float32, divisions int, color core.Color) {
	if halfExtent <= 0 || divisions <= 0 {
		return
	}
	step := (2 * halfExtent) / float32(divisions)
	for i := 0; i <= divisions; i++ {
		offset := -halfExtent + float32(i)*step
		dd.Line(
			math3d.Vec3{X: center.X - halfExtent, Y: center.Y, Z: center.Z + offset},
			math3d.Vec3{X: center.X + halfExtent, Y: center.Y, Z: center.Z + offset},
			color,
		)
		dd.Line(
			math3d.Vec3{X: center.X + offset, Y: center.Y, Z: center.Z - halfExtent},
			math3d.Vec3{X: center.X + offset, Y: center.Y, Z: center.Z + halfExtent},
			color,
		)
	}
}

// Frustum appends the 12 wireframe edges of a camera frustum given its inverse
// view-projection matrix (assuming WebGPU/Metal [0, 1] NDC depth).
func (dd *DebugDraw) Frustum(invViewProj math3d.Mat4, color core.Color) {
	ndc := [8]math3d.Vec3{
		{X: -1, Y: -1, Z: 0}, {X: 1, Y: -1, Z: 0}, {X: 1, Y: 1, Z: 0}, {X: -1, Y: 1, Z: 0}, // near plane (z = 0)
		{X: -1, Y: -1, Z: 1}, {X: 1, Y: -1, Z: 1}, {X: 1, Y: 1, Z: 1}, {X: -1, Y: 1, Z: 1}, // far plane (z = 1)
	}
	var c [8]math3d.Vec3
	for i, p := range ndc {
		c[i] = invViewProj.TransformPoint(p)
	}
	edges := [12][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{4, 5}, {5, 6}, {6, 7}, {7, 4},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
	}
	for _, e := range edges {
		dd.Line(c[e[0]], c[e[1]], color)
	}
}

// Flush projects all recorded 3D debug lines through viewProj into d, batching
// consecutive segments that share the same color and stroke width.
func (dd *DebugDraw) Flush(d *core.Drawing, viewProj math3d.Mat4, viewport core.Bounds) {
	if d == nil || len(dd.lines) == 0 || viewport.Size.Width <= 0 || viewport.Size.Height <= 0 {
		return
	}
	d.Save()
	d.SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)

	var (
		open     bool
		curColor core.Color
		curWidth float32
	)
	flushPath := func() {
		if open {
			d.Stroke()
			open = false
		}
	}
	for _, ln := range dd.lines {
		ax, ay, bx, by, ok := ProjectSegment(ln.A, ln.B, viewProj, viewport)
		if !ok {
			continue
		}
		w := ln.Width
		if w <= 0 {
			w = dd.defaultWidth()
		}
		if !open || ln.Color != curColor || w != curWidth {
			flushPath()
			curColor = ln.Color
			curWidth = w
			d.SetStrokeColor(curColor).SetLineWidth(curWidth).BeginPath()
			open = true
		}
		d.MoveTo(ax, ay).LineTo(bx, by)
	}
	flushPath()
	d.Restore()
}

// FrameSample records timing and render telemetry for a single frame (#286).
type FrameSample struct {
	FrameTime time.Duration
	DrawCalls int
	Triangles uint64
}

// FrameProfiler maintains a fixed-capacity ring buffer of recent [FrameSample]s
// for 3D performance overlays and regression checks (#286).
type FrameProfiler struct {
	samples []FrameSample
	sorted  []time.Duration
	head    int
	count   int
}

// NewFrameProfiler creates a FrameProfiler retaining up to capacity recent
// frames (defaulting to 120 when capacity <= 0).
func NewFrameProfiler(capacity int) *FrameProfiler {
	if capacity <= 0 {
		capacity = 120
	}
	return &FrameProfiler{
		samples: make([]FrameSample, capacity),
		sorted:  make([]time.Duration, 0, capacity),
	}
}

// Record stores one frame's duration and [RenderStats] without allocating.
func (p *FrameProfiler) Record(frameTime time.Duration, stats RenderStats) {
	if len(p.samples) == 0 {
		return
	}
	p.samples[p.head] = FrameSample{
		FrameTime: frameTime,
		DrawCalls: stats.TotalDrawCalls,
		Triangles: stats.Triangles,
	}
	p.head = (p.head + 1) % len(p.samples)
	if p.count < len(p.samples) {
		p.count++
	}
}

// Reset clears all recorded samples while retaining buffer capacity.
func (p *FrameProfiler) Reset() {
	p.head = 0
	p.count = 0
}

// Count returns the number of recorded frames currently in the window.
func (p *FrameProfiler) Count() int { return p.count }

// AverageFrameTime returns the arithmetic mean frame duration in the window.
func (p *FrameProfiler) AverageFrameTime() time.Duration {
	if p.count == 0 {
		return 0
	}
	var sum time.Duration
	for i := range p.count {
		sum += p.samples[i].FrameTime
	}
	return sum / time.Duration(p.count)
}

// P95FrameTime returns the 95th-percentile frame duration in the window.
func (p *FrameProfiler) P95FrameTime() time.Duration {
	if p.count == 0 {
		return 0
	}
	p.sorted = p.sorted[:0]
	for i := range p.count {
		p.sorted = append(p.sorted, p.samples[i].FrameTime)
	}
	slices.Sort(p.sorted)
	idx := (95*(p.count-1) + 99) / 100
	if idx >= p.count {
		idx = p.count - 1
	}
	return p.sorted[idx]
}

// FPS returns the average frames per second across the window, or 0 if empty.
func (p *FrameProfiler) FPS() float64 {
	avg := p.AverageFrameTime()
	if avg <= 0 {
		return 0
	}
	return float64(time.Second) / float64(avg)
}

// PeakDrawCalls returns the highest draw call count recorded in the window.
func (p *FrameProfiler) PeakDrawCalls() int {
	peak := 0
	for i := range p.count {
		if p.samples[i].DrawCalls > peak {
			peak = p.samples[i].DrawCalls
		}
	}
	return peak
}

// PeakTriangles returns the highest triangle count recorded in the window.
func (p *FrameProfiler) PeakTriangles() uint64 {
	var peak uint64
	for i := range p.count {
		if p.samples[i].Triangles > peak {
			peak = p.samples[i].Triangles
		}
	}
	return peak
}
