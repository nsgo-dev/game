// Package tilemap draws tiled worlds, queries tile flags and rays against them, loads Tiled maps, and moves a 2D camera over the world.
package tilemap

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Camera maps world coordinates to a view of size Width by Height centered on (X, Y), zoomed by Zoom and turned clockwise by Rotation.
//
// A zero or negative Zoom behaves as 1. When Bounds is set, Clamp and Follow keep the visible view inside it, centering any axis where the world is smaller than the view.
type Camera struct {
	// X and Y are the world point at the center of the view.
	X, Y float32
	// Zoom scales the world in the view; values <= 0 behave as 1.
	Zoom float32
	// Rotation turns the camera clockwise in the world, in radians.
	Rotation float32
	// Width and Height are the view's size in points.
	Width, Height float32
	// Bounds, when non-nil, limits where Clamp and Follow let the visible view reach.
	Bounds *core.Bounds
	// ShakeSeed seeds the deterministic shake offset; 0 uses a fixed default seed.
	ShakeSeed uint64

	shakeX, shakeY float32
	shakeIntensity float32
	shakeDuration  time.Duration
	shakeElapsed   time.Duration
	shakeRNG       *rand.Rand
}

func (c *Camera) zoom() float64 {
	if c.Zoom <= 0 {
		return 1
	}
	return float64(c.Zoom)
}

func (c *Camera) halfExtents() (halfW, halfH float64) {
	z := c.zoom()
	hw := math.Max(0, float64(c.Width)/2)
	hh := math.Max(0, float64(c.Height)/2)
	if c.Rotation == 0 {
		return hw / z, hh / z
	}
	sin, cos := math.Sincos(float64(c.Rotation))
	acos, asin := math.Abs(cos), math.Abs(sin)
	return (hw*acos + hh*asin) / z, (hw*asin + hh*acos) / z
}

// WorldToView converts a point in world coordinates to its position in the view.
func (c *Camera) WorldToView(p core.Point) core.Point {
	z := c.zoom()
	dx := float64(p.X) - float64(c.X+c.shakeX)
	dy := float64(p.Y) - float64(c.Y+c.shakeY)
	rx, ry := dx, dy
	if c.Rotation != 0 {
		sin, cos := math.Sincos(float64(c.Rotation))
		rx = dx*cos + dy*sin
		ry = -dx*sin + dy*cos
	}
	return core.Point{
		X: core.Points(float64(c.Width)/2 + rx*z),
		Y: core.Points(float64(c.Height)/2 + ry*z),
	}
}

// ViewToWorld converts a point in the view to its position in world coordinates.
func (c *Camera) ViewToWorld(p core.Point) core.Point {
	z := c.zoom()
	rx := (float64(p.X) - float64(c.Width)/2) / z
	ry := (float64(p.Y) - float64(c.Height)/2) / z
	dx, dy := rx, ry
	if c.Rotation != 0 {
		sin, cos := math.Sincos(float64(c.Rotation))
		dx = rx*cos - ry*sin
		dy = rx*sin + ry*cos
	}
	return core.Point{
		X: core.Points(float64(c.X+c.shakeX) + dx),
		Y: core.Points(float64(c.Y+c.shakeY) + dy),
	}
}

// VisibleBounds returns the axis-aligned world rectangle covering all four corners of the view at the camera's current position, zoom, rotation and shake offset.
func (c *Camera) VisibleBounds() core.Bounds {
	halfW, halfH := c.halfExtents()
	cx := float64(c.X + c.shakeX)
	cy := float64(c.Y + c.shakeY)
	return core.Bounds{
		Origin: core.Point{X: core.Points(cx - halfW), Y: core.Points(cy - halfH)},
		Size:   core.Size{Width: core.Points(2 * halfW), Height: core.Points(2 * halfH)},
	}
}

// Apply multiplies d's transform by the world-to-view transform, including any active shake offset.
func (c *Camera) Apply(d *core.Drawing) {
	z := float32(c.zoom())
	if c.Width != 0 || c.Height != 0 {
		d.Translate(c.Width/2, c.Height/2)
	}
	if z != 1 {
		d.Scale(z, z)
	}
	if c.Rotation != 0 {
		d.Rotate(-c.Rotation)
	}
	if tx, ty := -(c.X + c.shakeX), -(c.Y + c.shakeY); tx != 0 || ty != 0 {
		d.Translate(tx, ty)
	}
}

// Clamp keeps the camera's visible view inside Bounds when Bounds is non-nil. On any axis where Bounds is smaller than the view, it centers the camera on Bounds.
func (c *Camera) Clamp() {
	if c.Bounds == nil {
		return
	}
	b := *c.Bounds
	halfW, halfH := c.halfExtents()

	minX := float64(b.Origin.X)
	maxX := minX + float64(b.Size.Width)
	if maxX-minX <= 2*halfW {
		c.X = float32((minX + maxX) / 2)
	} else {
		lo, hi := minX+halfW, maxX-halfW
		x := float64(c.X)
		if x < lo {
			x = lo
		} else if x > hi {
			x = hi
		}
		c.X = float32(x)
	}

	minY := float64(b.Origin.Y)
	maxY := minY + float64(b.Size.Height)
	if maxY-minY <= 2*halfH {
		c.Y = float32((minY + maxY) / 2)
	} else {
		lo, hi := minY+halfH, maxY-halfH
		y := float64(c.Y)
		if y < lo {
			y = lo
		} else if y > hi {
			y = hi
		}
		c.Y = float32(y)
	}
}

// Follow moves the camera toward target with exponential smoothing over a fixed Loop step dt, then clamps to Bounds if set.
//
// A smoothing <= 0 snaps directly to target. Higher positive values converge faster (factor 1 - exp(-smoothing*dt)).
func (c *Camera) Follow(target core.Point, smoothing float32, dt time.Duration) {
	if smoothing <= 0 {
		c.X = float32(target.X)
		c.Y = float32(target.Y)
	} else if dt > 0 {
		t := 1 - math.Exp(-float64(smoothing)*dt.Seconds())
		c.X = float32(float64(c.X) + (float64(target.X)-float64(c.X))*t)
		c.Y = float32(float64(c.Y) + (float64(target.Y)-float64(c.Y))*t)
	}
	c.Clamp()
}

// Shake starts a deterministic screen shake with maximum displacement intensity in world points that decays to zero over duration.
//
// A non-positive intensity or duration cancels any active shake.
func (c *Camera) Shake(intensity float32, duration time.Duration) {
	if intensity <= 0 || duration <= 0 {
		c.shakeX, c.shakeY = 0, 0
		c.shakeIntensity = 0
		c.shakeDuration = 0
		c.shakeElapsed = 0
		return
	}
	seed := c.ShakeSeed
	if seed == 0 {
		seed = 1
	}
	c.shakeRNG = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	c.shakeIntensity = intensity
	c.shakeDuration = duration
	c.shakeElapsed = 0
	c.sampleShake(1)
}

// Update advances an active screen shake by dt, returning the shake offset to (0, 0) once its duration elapses.
func (c *Camera) Update(dt time.Duration) {
	if dt <= 0 || c.shakeDuration <= 0 {
		return
	}
	c.shakeElapsed += dt
	if c.shakeElapsed >= c.shakeDuration {
		c.shakeX, c.shakeY = 0, 0
		c.shakeIntensity = 0
		c.shakeDuration = 0
		c.shakeElapsed = 0
		return
	}
	rem := 1 - float64(c.shakeElapsed)/float64(c.shakeDuration)
	c.sampleShake(rem * rem)
}

// ShakeOffset returns the current screen shake displacement in world points, or (0, 0) when idle.
func (c *Camera) ShakeOffset() core.Point {
	return core.Point{X: core.Points(c.shakeX), Y: core.Points(c.shakeY)}
}

// Parallax returns a Camera with c's zoom, rotation, and viewport size whose position (including any active shake offset) is scaled by factorX and factorY.
func (c *Camera) Parallax(factorX, factorY float32) Camera {
	if c == nil {
		return Camera{}
	}
	return Camera{
		X:        (c.X + c.shakeX) * factorX,
		Y:        (c.Y + c.shakeY) * factorY,
		Zoom:     c.Zoom,
		Rotation: c.Rotation,
		Width:    c.Width,
		Height:   c.Height,
	}
}

func (c *Camera) sampleShake(decay float64) {
	r := (0.5 + 0.5*c.shakeRNG.Float64()) * float64(c.shakeIntensity) * decay
	theta := c.shakeRNG.Float64() * 2 * math.Pi
	sin, cos := math.Sincos(theta)
	c.shakeX = float32(r * cos)
	c.shakeY = float32(r * sin)
}
