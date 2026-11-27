package render3d

import (
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultTrailMaxPoints = 64
	defaultTrailLifetime  = 500 * time.Millisecond
	defaultTrailWidth     = float32(0.25)
)

// TrailConfig configures a 3D ribbon [Trail] (#281). It mirrors the MaxPoints, Lifetime, MinDistance, Width, WidthCurve, ColorGradient and Color options of [particles.TrailConfig] in world units and durations; it has no Texture, and [Trail] has no Attach, Detach or Follow, so feed it positions with [Trail.AddPoint].
type TrailConfig struct {
	// MaxPoints caps the number of recorded points; defaults to 64 when <= 0.
	MaxPoints int
	// Lifetime is how long each point lives before it expires; defaults to 500ms when <= 0.
	Lifetime time.Duration
	// MinDistance is the minimum world distance from the newest point before a new point is appended. Zero or negative records every distinct point; an exact duplicate refreshes the newest point's age.
	MinDistance float32
	// Width is the ribbon's base width in world units; defaults to 0.25 when <= 0.
	Width float32
	// WidthCurve scales Width along the trail from head (u=0, newest) to tail (u=1, oldest). Empty keeps 1.
	WidthCurve particles.Curve
	// ColorGradient evaluates color along the trail from head (u=0) to tail (u=1). When empty, Color is used.
	ColorGradient particles.ColorCurve
	// Color is the solid color used when ColorGradient is empty; zero means opaque white.
	Color core.Color
}

func (c TrailConfig) maxPoints() int {
	if c.MaxPoints > 0 {
		return c.MaxPoints
	}
	return defaultTrailMaxPoints
}

func (c TrailConfig) lifetime() time.Duration {
	if c.Lifetime > 0 {
		return c.Lifetime
	}
	return defaultTrailLifetime
}

func (c TrailConfig) baseWidth() float32 {
	if c.Width > 0 && !isInf32(c.Width) {
		return c.Width
	}
	return defaultTrailWidth
}

func (c TrailConfig) baseColor() core.Color {
	if c.Color != 0 {
		return c.Color
	}
	return core.RGB(0xffffff)
}

func (c TrailConfig) clone() TrailConfig {
	out := c
	if len(c.WidthCurve) > 0 {
		out.WidthCurve = append(particles.Curve(nil), c.WidthCurve...)
	}
	if len(c.ColorGradient) > 0 {
		out.ColorGradient = append(particles.ColorCurve(nil), c.ColorGradient...)
	}
	return out
}

// TrailPoint is one recorded point of a [Trail].
type TrailPoint struct {
	// Pos is the recorded world-space position.
	Pos math3d.Vec3
	// Age is how long ago the point was recorded or last refreshed.
	Age time.Duration
	// Length is the distance along the trail from the oldest point.
	Length float32
}

// TrailVertex is one edge vertex of a built ribbon.
type TrailVertex struct {
	// Pos is the vertex's world-space position.
	Pos math3d.Vec3
	// Normal is the ribbon's unit surface normal at this vertex, turned toward the camera.
	Normal math3d.Vec3
	// Color is the ribbon color evaluated at U.
	Color core.Color
	// U runs along the trail from 0 at the head (newest) to 1 at the tail (oldest); V is 0 on the left edge and 1 on the right.
	U, V float32
}

// Trail records a moving object's world positions and builds a camera-facing ribbon through them (#281).
//
// Each point's ribbon edge runs along side = tangent × toCamera, so the strip's surface turns about its own centerline to face the viewer at every angle, like a 3D laser, sword swing or projectile streak.
type Trail struct {
	cfg    TrailConfig
	points []TrailPoint
	verts  []TrailVertex
	screen [maxClipVerts]screenPoint
}

// NewTrail creates an empty Trail with cfg.
func NewTrail(cfg TrailConfig) *Trail {
	t := &Trail{cfg: cfg.clone()}
	t.points = make([]TrailPoint, 0, t.cfg.maxPoints())
	return t
}

// Config returns a copy of the trail's configuration.
func (t *Trail) Config() TrailConfig {
	return t.cfg.clone()
}

// SetConfig replaces the configuration, keeping live points and dropping the oldest ones beyond the new MaxPoints.
func (t *Trail) SetConfig(cfg TrailConfig) {
	t.cfg = cfg.clone()
	if over := len(t.points) - t.cfg.maxPoints(); over > 0 {
		t.dropOldest(over)
	}
}

// AddPoint appends p as the newest point when it is at least MinDistance from the previous one. Non-finite points are ignored.
func (t *Trail) AddPoint(p math3d.Vec3) {
	if !p.IsFinite() {
		return
	}
	n := len(t.points)
	var dist float32
	if n > 0 {
		last := &t.points[n-1]
		distSq := p.DistSq(last.Pos)
		if distSq == 0 {
			last.Age = 0
			return
		}
		if t.cfg.MinDistance > 0 && distSq < t.cfg.MinDistance*t.cfg.MinDistance {
			return
		}
		dist = p.Dist(last.Pos)
		if isInf32(dist) {
			return
		}
	}
	if n >= t.cfg.maxPoints() {
		t.dropOldest(n - t.cfg.maxPoints() + 1)
		n = len(t.points)
	}
	var length float32
	if n > 0 {
		length = t.points[n-1].Length + dist
	}
	t.points = append(t.points, TrailPoint{Pos: p, Length: length})
}

// Update ages every point by dt and removes the expired ones. Non-positive dt does nothing.
func (t *Trail) Update(dt time.Duration) {
	if dt <= 0 {
		return
	}
	life := t.cfg.lifetime()
	expired := 0
	for i := range t.points {
		// Compare before adding so a huge dt saturates at life instead of wrapping the int64 age negative.
		if dt >= life-t.points[i].Age {
			t.points[i].Age = life
			expired = i + 1
			continue
		}
		t.points[i].Age += dt
	}
	if expired > 0 {
		t.dropOldest(expired)
	}
}

func (t *Trail) dropOldest(k int) {
	if k >= len(t.points) {
		t.points = t.points[:0]
		return
	}
	rem := copy(t.points, t.points[k:])
	t.points = t.points[:rem]
	base := t.points[0].Length
	for i := range t.points {
		t.points[i].Length -= base
	}
}

// Clear removes every recorded point.
func (t *Trail) Clear() {
	t.points = t.points[:0]
}

// Points returns the live points from oldest (index 0) to newest. The slice aliases internal storage until the next mutation.
func (t *Trail) Points() []TrailPoint {
	return t.points
}

// PointCount returns the number of live points.
func (t *Trail) PointCount() int {
	return len(t.points)
}

// EvaluateAt returns the ribbon width and color at u in [0, 1], where 0 is the head and 1 the tail.
func (t *Trail) EvaluateAt(u float32) (width float32, color core.Color) {
	u = clamp01(u)
	width = max(t.cfg.baseWidth()*t.cfg.WidthCurve.At(u), 0)
	if len(t.cfg.ColorGradient) > 0 {
		return width, t.cfg.ColorGradient.At(u)
	}
	return width, t.cfg.baseColor()
}

// maxHalfWidth bounds half the ribbon width anywhere along the trail.
func (t *Trail) maxHalfWidth() float32 {
	scale := float32(1)
	if len(t.cfg.WidthCurve) > 0 {
		scale = 0
		for _, s := range t.cfg.WidthCurve {
			scale = max(scale, s.Value)
		}
	}
	return t.cfg.baseWidth() * scale * 0.5
}

// Bounds returns a world-space AABB that contains the ribbon for any camera, or an empty box when fewer than 2 points are live.
func (t *Trail) Bounds() math3d.AABB {
	b := math3d.AABBEmpty()
	if len(t.points) < 2 {
		return b
	}
	for _, p := range t.points {
		b = b.ExpandPoint(p.Pos)
	}
	hw := t.maxHalfWidth()
	pad := math3d.Vec3{X: hw, Y: hw, Z: hw}
	return math3d.AABB{Min: b.Min.Sub(pad), Max: b.Max.Add(pad)}
}

// DrawItem returns template with an identity Transform and WorldBounds, Triangles and Vertices taken from the trail, so it can be sorted by [Queue.Build]. A zero template Blend is replaced by [BlendAlphaBlend].
func (t *Trail) DrawItem(template DrawItem) DrawItem {
	template.Transform = math3d.Mat4Identity()
	template.WorldBounds = t.Bounds()
	template.Triangles, template.Vertices = 0, 0
	if n := len(t.points); n >= 2 {
		template.Triangles = uint32(2 * (n - 1))
		template.Vertices = uint32(2 * n)
	}
	if template.Blend == BlendOpaque {
		template.Blend = BlendAlphaBlend
	}
	return template
}

// Build returns the ribbon facing cam as a triangle strip: two vertices per point from tail to head, left edge first. It returns nil when fewer than 2 points are live. The slice aliases internal storage until the next Build.
func (t *Trail) Build(cam CameraBasis) []TrailVertex {
	n := len(t.points)
	if n < 2 {
		t.verts = t.verts[:0]
		return nil
	}
	t.verts = growVerts(t.verts, 2*n)
	camRight, camUp, _ := cameraAxes(cam)
	life := t.cfg.lifetime()
	total := t.points[n-1].Length
	var prevSide math3d.Vec3
	for i, p := range t.points {
		tangent := t.tangent(i)
		toCam := cam.ToCamera(p.Pos)
		side, ok := unitVec(tangent.Cross(toCam))
		if !ok {
			// The trail heads straight at the camera here: keep the previous edge direction, else any camera axis across the tangent.
			side, ok = unitVec(prevSide.Sub(tangent.Scale(prevSide.Dot(tangent))))
			if !ok {
				side, ok = unitVec(camUp.Sub(tangent.Scale(camUp.Dot(tangent))))
			}
			if !ok {
				side, _ = unitVec(camRight.Sub(tangent.Scale(camRight.Dot(tangent))))
			}
		}
		prevSide = side
		normal, ok := unitVec(side.Cross(tangent))
		if !ok {
			normal = toCam
		}

		var uLen float32
		if total > 1e-6 {
			uLen = 1 - p.Length/total
		} else {
			uLen = 1 - float32(i)/float32(n-1)
		}
		uAge := float32(p.Age) / float32(life)
		u := max(clamp01(uLen), clamp01(uAge))
		w, col := t.EvaluateAt(u)
		hw := side.Scale(w * 0.5)
		t.verts[2*i] = TrailVertex{Pos: p.Pos.Add(hw), Normal: normal, Color: col, U: u, V: 0}
		t.verts[2*i+1] = TrailVertex{Pos: p.Pos.Sub(hw), Normal: normal, Color: col, U: u, V: 1}
	}
	return t.verts
}

// tangent returns the unit direction of travel at point i, averaging the adjacent segments at interior points.
func (t *Trail) tangent(i int) math3d.Vec3 {
	n := len(t.points)
	switch i {
	case 0:
		d, _ := unitVec(t.points[1].Pos.Sub(t.points[0].Pos))
		return d
	case n - 1:
		d, _ := unitVec(t.points[n-1].Pos.Sub(t.points[n-2].Pos))
		return d
	}
	d1, _ := unitVec(t.points[i].Pos.Sub(t.points[i-1].Pos))
	d2, _ := unitVec(t.points[i+1].Pos.Sub(t.points[i].Pos))
	if d, ok := unitVec(d1.Add(d2)); ok {
		return d
	}
	return d2
}

// Draw builds the ribbon facing cam and fills it into d through viewProj and viewport, clipping each segment against the near plane. It returns the number of segments drawn.
func (t *Trail) Draw(d *core.Drawing, cam CameraBasis, viewProj math3d.Mat4, viewport core.Bounds) int {
	if d == nil || viewport.Size.Width <= 0 || viewport.Size.Height <= 0 {
		return 0
	}
	verts := t.Build(cam)
	if len(verts) < 4 {
		return 0
	}
	drawn := 0
	d.Save()
	for i := 0; i+3 < len(verts); i += 2 {
		l0, r0, l1, r1 := verts[i], verts[i+1], verts[i+2], verts[i+3]
		if l0.Pos == r0.Pos && l1.Pos == r1.Pos {
			continue // zero width at both ends
		}
		c0, c1 := l0.Color, l1.Color
		if c0&0xff == 0 && c1&0xff == 0 {
			continue
		}
		quad := [4]math3d.Vec3{l0.Pos, l1.Pos, r1.Pos, r0.Pos}
		pts, _ := projectPolygon(quad[:], viewProj, viewport, t.screen[:0])
		if len(pts) < 3 {
			continue
		}
		if c0 == c1 {
			d.SetFillColor(c0)
		} else {
			a, okA := projectPoint(t.points[i/2].Pos, viewProj, viewport)
			b, okB := projectPoint(t.points[i/2+1].Pos, viewProj, viewport)
			if okA && okB && (a.x != b.x || a.y != b.y) {
				d.SetFillGradient(core.CanvasLinear(a.x, a.y, b.x, b.y,
					core.GradientStop{Offset: 0, Color: c0},
					core.GradientStop{Offset: 1, Color: c1},
				))
			} else {
				d.SetFillColor(c0)
			}
		}
		d.BeginPath()
		d.MoveTo(pts[0].x, pts[0].y)
		for _, p := range pts[1:] {
			d.LineTo(p.x, p.y)
		}
		d.ClosePath()
		d.Fill()
		drawn++
	}
	d.Restore()
	return drawn
}

// projectPoint maps p into viewport, reporting false when it lies behind the near plane.
func projectPoint(p math3d.Vec3, viewProj math3d.Mat4, viewport core.Bounds) (screenPoint, bool) {
	c := viewProj.MulVec4(p.Vec4(1))
	if c.Z < 0 || c.W < 1e-5 {
		return screenPoint{}, false
	}
	vx, vy := float32(viewport.Origin.X), float32(viewport.Origin.Y)
	vw, vh := float32(viewport.Size.Width), float32(viewport.Size.Height)
	return screenPoint{vx + (c.X/c.W*0.5+0.5)*vw, vy + (0.5-c.Y/c.W*0.5)*vh}, true
}

func growVerts(s []TrailVertex, n int) []TrailVertex {
	if cap(s) < n {
		return make([]TrailVertex, n)
	}
	return s[:n]
}

func clamp01(v float32) float32 {
	if !(v > 0) {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}
