package particles

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultTrailMaxPoints = 64
	defaultTrailLifetime  = float32(0.5)
	defaultTrailWidth     = float32(4)
)

// TrailConfig configures a 2D ribbon trail.
type TrailConfig struct {
	// MaxPoints caps the number of recorded polyline vertices; defaults to 64 when <= 0.
	MaxPoints int
	// Lifetime is how long each vertex lives in seconds before expiring; defaults to 0.5 when <= 0.
	Lifetime float32
	// MinDistance is the minimum distance in points from the newest vertex before appending a new vertex.
	// Zero or negative records every distinct point (exact duplicates refresh the newest vertex's age).
	MinDistance float32
	// Width is the base ribbon width in points; defaults to 4 when <= 0.
	Width float32
	// WidthCurve scales Width along the trail from head (t=0, newest) to tail (t=1, oldest). Empty keeps 1.
	WidthCurve Curve
	// ColorGradient evaluates color along the trail from head (t=0, newest) to tail (t=1, oldest).
	// When empty, Color is used (defaulting to opaque white when 0).
	ColorGradient ColorCurve
	// Color is the fallback solid color when ColorGradient is empty; defaults to opaque white when 0.
	Color core.Color
	// Texture is an optional image mapped across each ribbon segment when non-empty.
	Texture core.ImageSource
}

func (c TrailConfig) maxPoints() int {
	if c.MaxPoints > 0 {
		return c.MaxPoints
	}
	return defaultTrailMaxPoints
}

func (c TrailConfig) lifetime() float32 {
	if c.Lifetime > 0 {
		return c.Lifetime
	}
	return defaultTrailLifetime
}

func (c TrailConfig) baseWidth() float32 {
	if c.Width > 0 {
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
		out.WidthCurve = append(Curve(nil), c.WidthCurve...)
	}
	if len(c.ColorGradient) > 0 {
		out.ColorGradient = append(ColorCurve(nil), c.ColorGradient...)
	}
	return out
}

// TrailPoint is one recorded vertex along a Trail polyline.
type TrailPoint struct {
	X, Y   float32
	Age    float32
	Length float32
}

type trailVec2 struct {
	x, y float32
}

// Trail builds and draws a smooth 2D ribbon from a sequence of sampled positions.
type Trail struct {
	cfg      TrailConfig
	points   []TrailPoint
	attachFn func() (float32, float32)

	// Reusable draw scratch buffers.
	left   []trailVec2
	right  []trailVec2
	halfW  []float32
	colors []core.Color
}

// NewTrail creates a new Trail with the given configuration.
func NewTrail(cfg TrailConfig) *Trail {
	t := &Trail{cfg: cfg.clone()}
	t.points = make([]TrailPoint, 0, cfg.maxPoints())
	return t
}

// Config returns a copy of the trail's configuration.
func (t *Trail) Config() TrailConfig {
	return t.cfg.clone()
}

// SetConfig updates the trail's configuration without clearing live points.
func (t *Trail) SetConfig(cfg TrailConfig) {
	t.cfg = cfg.clone()
	maxPts := t.cfg.maxPoints()
	if len(t.points) > maxPts {
		trim := len(t.points) - maxPts
		copy(t.points, t.points[trim:])
		clear(t.points[maxPts:])
		t.points = t.points[:maxPts]
		t.recomputeLengths()
	}
}

// Attach binds a position callback sampled automatically on each Update call.
// Passing nil detaches any existing callback.
func (t *Trail) Attach(posFn func() (float32, float32)) {
	t.attachFn = posFn
	if posFn != nil {
		x, y := posFn()
		t.AddPoint(x, y)
	}
}

// Detach removes any attached position callback.
func (t *Trail) Detach() {
	t.attachFn = nil
}

// Follow records the current position of a tracked object into the trail.
func (t *Trail) Follow(x, y float32) {
	t.AddPoint(x, y)
}

// AddPoint appends (x, y) to the trail head if it is at least MinDistance from the previous point.
func (t *Trail) AddPoint(x, y float32) {
	if isBadFloat(x) || isBadFloat(y) {
		return
	}
	n := len(t.points)
	var dist float32
	if n > 0 {
		last := &t.points[n-1]
		dx := x - last.X
		dy := y - last.Y
		distSq := dx*dx + dy*dy
		if distSq == 0 {
			last.Age = 0
			return
		}
		if t.cfg.MinDistance > 0 && distSq < t.cfg.MinDistance*t.cfg.MinDistance {
			return
		}
		dist = float32(math.Sqrt(float64(distSq)))
	}

	maxPts := t.cfg.maxPoints()
	if n >= maxPts {
		copy(t.points, t.points[1:])
		t.points = t.points[:maxPts-1]
		n = len(t.points)
		if n > 0 {
			baseLen := t.points[0].Length
			if baseLen != 0 {
				for i := range t.points {
					t.points[i].Length -= baseLen
				}
			}
		}
	}

	var cumLen float32
	if n > 0 {
		cumLen = t.points[n-1].Length + dist
	}
	t.points = append(t.points, TrailPoint{
		X:      x,
		Y:      y,
		Age:    0,
		Length: cumLen,
	})
}

// Update advances all point ages by dt seconds, removes expired points, and samples any attached callback.
// Non-positive dt does nothing.
func (t *Trail) Update(dt float32) {
	if dt <= 0 || isBadFloat(dt) {
		return
	}
	life := t.cfg.lifetime()
	firstAlive := 0
	for i := range t.points {
		t.points[i].Age += dt
		if t.points[i].Age >= life {
			firstAlive = i + 1
		}
	}
	if firstAlive > 0 {
		if firstAlive >= len(t.points) {
			clear(t.points)
			t.points = t.points[:0]
		} else {
			rem := len(t.points) - firstAlive
			copy(t.points, t.points[firstAlive:])
			clear(t.points[rem:])
			t.points = t.points[:rem]
			baseLen := t.points[0].Length
			if baseLen != 0 {
				for i := range t.points {
					t.points[i].Length -= baseLen
				}
			}
		}
	}
	if t.attachFn != nil {
		x, y := t.attachFn()
		t.AddPoint(x, y)
	}
}

// Clear removes all recorded points from the trail.
func (t *Trail) Clear() {
	clear(t.points)
	t.points = t.points[:0]
}

// Points returns the active trail vertices from oldest (tail, index 0) to newest (head, last index).
// The returned slice is backed by internal storage; copy it to retain a snapshot.
func (t *Trail) Points() []TrailPoint {
	return t.points
}

// PointCount returns the number of active vertices in the trail.
func (t *Trail) PointCount() int {
	return len(t.points)
}

// EvaluateAt evaluates the ribbon width and color at normalized parameter u in [0, 1],
// where 0 is the newest head and 1 is the oldest tail.
func (t *Trail) EvaluateAt(u float32) (width float32, color core.Color) {
	u = clamp01(u)
	w := t.cfg.baseWidth() * t.cfg.WidthCurve.At(u)
	if w < 0 {
		w = 0
	}
	if len(t.cfg.ColorGradient) > 0 {
		color = t.cfg.ColorGradient.At(u)
	} else {
		color = t.cfg.baseColor()
	}
	return w, color
}

// Draw builds the ribbon strip along perpendicular normals and draws it into d.
// Does nothing if fewer than 2 points are active.
func (t *Trail) Draw(d *core.Drawing) {
	n := len(t.points)
	if d == nil || n < 2 {
		return
	}

	t.buildGeometry()

	textured := !t.cfg.Texture.Empty()
	hasGradient := len(t.cfg.ColorGradient) > 0

	if !textured && !hasGradient {
		col := t.cfg.baseColor()
		if col&0xff == 0 {
			return
		}
		anyVisible := false
		for i := range n {
			if t.halfW[i] > 0 {
				anyVisible = true
				break
			}
		}
		if !anyVisible {
			return
		}
		d.Save()
		d.SetFillColor(col)
		d.BeginPath()
		d.MoveTo(t.left[0].x, t.left[0].y)
		for i := 1; i < n; i++ {
			d.LineTo(t.left[i].x, t.left[i].y)
		}
		for i := n - 1; i >= 0; i-- {
			d.LineTo(t.right[i].x, t.right[i].y)
		}
		d.ClosePath()
		d.Fill()
		d.Restore()
		return
	}

	d.Save()
	for i := range n - 1 {
		hw0, hw1 := t.halfW[i], t.halfW[i+1]
		if hw0 <= 0 && hw1 <= 0 {
			continue
		}
		c0, c1 := t.colors[i], t.colors[i+1]
		if (c0&0xff) == 0 && (c1&0xff) == 0 {
			continue
		}
		l0, l1 := t.left[i], t.left[i+1]
		r0, r1 := t.right[i], t.right[i+1]

		if textured {
			p0, p1 := t.points[i], t.points[i+1]
			dx, dy := p1.X-p0.X, p1.Y-p0.Y
			segLen := float32(math.Hypot(float64(dx), float64(dy)))
			if segLen <= 1e-6 {
				continue
			}
			halfMax := max(hw0, hw1)
			alpha := (float32(c0&0xff) + float32(c1&0xff)) / (2 * 255)
			if alpha <= 0 {
				continue
			}
			ang := float32(math.Atan2(float64(dy), float64(dx)))
			d.Save()
			d.BeginPath()
			d.MoveTo(l0.x, l0.y)
			d.LineTo(l1.x, l1.y)
			d.LineTo(r1.x, r1.y)
			d.LineTo(r0.x, r0.y)
			d.ClosePath()
			d.Clip()
			if alpha < 1 {
				d.SetAlpha(alpha)
			}
			d.Translate(p0.X, p0.Y)
			if ang != 0 {
				d.Rotate(ang)
			}
			d.DrawImageIn(t.cfg.Texture, 0, -halfMax, segLen, 2*halfMax)
			d.Restore()
			continue
		}

		p0, p1 := t.points[i], t.points[i+1]
		if c0 == c1 || (p0.X == p1.X && p0.Y == p1.Y) {
			d.SetFillColor(c0)
		} else {
			d.SetFillGradient(core.CanvasLinear(
				p0.X, p0.Y,
				p1.X, p1.Y,
				core.GradientStop{Offset: 0, Color: c0},
				core.GradientStop{Offset: 1, Color: c1},
			))
		}
		d.BeginPath()
		d.MoveTo(l0.x, l0.y)
		d.LineTo(l1.x, l1.y)
		d.LineTo(r1.x, r1.y)
		d.LineTo(r0.x, r0.y)
		d.ClosePath()
		d.Fill()
	}
	d.Restore()
}

func (t *Trail) buildGeometry() {
	n := len(t.points)
	if cap(t.left) < n {
		t.left = make([]trailVec2, n)
		t.right = make([]trailVec2, n)
		t.halfW = make([]float32, n)
		t.colors = make([]core.Color, n)
	} else {
		t.left = t.left[:n]
		t.right = t.right[:n]
		t.halfW = t.halfW[:n]
		t.colors = t.colors[:n]
	}

	life := t.cfg.lifetime()
	totalLen := t.points[n-1].Length - t.points[0].Length
	baseLen := t.points[0].Length

	for i := range n {
		var tx, ty float32
		switch {
		case i == 0:
			tx, ty = normDir(t.points[1].X-t.points[0].X, t.points[1].Y-t.points[0].Y)
		case i == n-1:
			tx, ty = normDir(t.points[n-1].X-t.points[n-2].X, t.points[n-1].Y-t.points[n-2].Y)
		default:
			d1x, d1y := normDir(t.points[i].X-t.points[i-1].X, t.points[i].Y-t.points[i-1].Y)
			d2x, d2y := normDir(t.points[i+1].X-t.points[i].X, t.points[i+1].Y-t.points[i].Y)
			tx, ty = normDir(d1x+d2x, d1y+d2y)
			if tx == 0 && ty == 0 {
				tx, ty = d2x, d2y
			}
		}
		// Perpendicular normal rotated 90 degrees from tangent.
		nx, ny := -ty, tx

		var tLen float32
		if totalLen > 1e-6 {
			tLen = 1 - (t.points[i].Length-baseLen)/totalLen
		} else if n > 1 {
			tLen = 1 - float32(i)/float32(n-1)
		}
		tAge := clamp01(t.points[i].Age / life)
		u := max(clamp01(tLen), tAge)

		w, col := t.EvaluateAt(u)
		hw := w * 0.5
		t.halfW[i] = hw
		t.colors[i] = col
		px, py := t.points[i].X, t.points[i].Y
		t.left[i] = trailVec2{x: px + nx*hw, y: py + ny*hw}
		t.right[i] = trailVec2{x: px - nx*hw, y: py - ny*hw}
	}
}

func (t *Trail) recomputeLengths() {
	if len(t.points) == 0 {
		return
	}
	t.points[0].Length = 0
	for i := 1; i < len(t.points); i++ {
		dx := t.points[i].X - t.points[i-1].X
		dy := t.points[i].Y - t.points[i-1].Y
		t.points[i].Length = t.points[i-1].Length + float32(math.Hypot(float64(dx), float64(dy)))
	}
}

func normDir(dx, dy float32) (float32, float32) {
	lenSq := dx*dx + dy*dy
	if lenSq <= 1e-12 {
		return 0, 0
	}
	inv := float32(1 / math.Sqrt(float64(lenSq)))
	return dx * inv, dy * inv
}

func isBadFloat(v float32) bool {
	return math.IsNaN(float64(v)) || math.IsInf(float64(v), 0)
}

// TrailManager updates and draws a collection of trails (such as moving sprite or projectile ribbons).
type TrailManager struct {
	trails []*Trail
}

// NewTrailManager creates an empty TrailManager.
func NewTrailManager() *TrailManager {
	return &TrailManager{}
}

// Add creates a new Trail with cfg, registers it with the manager, and returns it.
func (m *TrailManager) Add(cfg TrailConfig) *Trail {
	tr := NewTrail(cfg)
	m.trails = append(m.trails, tr)
	return tr
}

// AddTrail registers an existing Trail with the manager.
func (m *TrailManager) AddTrail(tr *Trail) {
	if tr == nil {
		return
	}
	m.trails = append(m.trails, tr)
}

// Remove unregisters tr from the manager.
func (m *TrailManager) Remove(tr *Trail) {
	for i, existing := range m.trails {
		if existing == tr {
			copy(m.trails[i:], m.trails[i+1:])
			m.trails[len(m.trails)-1] = nil
			m.trails = m.trails[:len(m.trails)-1]
			return
		}
	}
}

// Update advances all managed trails by dt seconds.
func (m *TrailManager) Update(dt float32) {
	for _, tr := range m.trails {
		tr.Update(dt)
	}
}

// Draw draws all managed trails into d in registration order.
func (m *TrailManager) Draw(d *core.Drawing) {
	for _, tr := range m.trails {
		tr.Draw(d)
	}
}

// Clear removes all points from all managed trails without unregistering them.
func (m *TrailManager) Clear() {
	for _, tr := range m.trails {
		tr.Clear()
	}
}

// Count returns the number of trails registered with the manager.
func (m *TrailManager) Count() int {
	return len(m.trails)
}

// Trails returns the slice of managed trails.
func (m *TrailManager) Trails() []*Trail {
	return m.trails
}

// AttachTrail enables automatic per-particle ribbon trails on the emitter using cfg.
func (e *Emitter) AttachTrail(cfg TrailConfig) {
	copied := cfg.clone()
	e.Trail = &copied
	// Update config on any already active particle trails.
	for _, tr := range e.particleTrails {
		if tr != nil {
			tr.SetConfig(copied)
		}
	}
}

// DetachTrail disables automatic per-particle ribbon trails on the emitter.
// Existing particle trails fade out naturally as their points expire.
func (e *Emitter) DetachTrail() {
	e.Trail = nil
	for _, tr := range e.particleTrails {
		if tr != nil && tr.PointCount() > 0 {
			e.fadingTrails = append(e.fadingTrails, tr)
		} else if tr != nil {
			e.releaseTrail(tr)
		}
	}
	clear(e.particleTrails)
	e.particleTrails = e.particleTrails[:0]
}

// TrailCount returns the total number of active and fading particle trails owned by the emitter.
func (e *Emitter) TrailCount() int {
	n := len(e.fadingTrails)
	for _, tr := range e.particleTrails {
		if tr != nil {
			n++
		}
	}
	return n
}

// ParticleTrails returns the currently live particle trails in spawn order.
func (e *Emitter) ParticleTrails() []*Trail {
	return e.particleTrails
}

func (e *Emitter) acquireTrail(cfg TrailConfig) *Trail {
	if n := len(e.trailPool); n > 0 {
		tr := e.trailPool[n-1]
		e.trailPool[n-1] = nil
		e.trailPool = e.trailPool[:n-1]
		tr.Clear()
		tr.SetConfig(cfg)
		return tr
	}
	return NewTrail(cfg)
}

func (e *Emitter) releaseTrail(tr *Trail) {
	if tr == nil {
		return
	}
	tr.Clear()
	tr.Detach()
	if len(e.trailPool) < e.maxParticles()*2 {
		e.trailPool = append(e.trailPool, tr)
	}
}
