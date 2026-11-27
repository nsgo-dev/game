package main

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// The scene's virtual size in points; Draw scales it to fit the canvas.
const (
	VirtualWidth  = 960
	VirtualHeight = 540
)

// Particle targets: the scene starts at DefaultTarget and the arrow keys move it by TargetStep between MinTarget and MaxTarget.
const (
	DefaultTarget = 5000
	TargetStep    = 1000
	MinTarget     = 1000
	MaxTarget     = 20000
)

// DefaultSeed seeds the scene's emitters when the app starts.
const DefaultSeed uint64 = 37

// StepRate is the fixed update rate of the scene's game.Loop.
const StepRate = 60

const (
	fountainCount = 4
	// swayPeriod is how many fixed steps one sway of the fountains takes.
	swayPeriod = 4 * StepRate
	// swayAngle is how far, in radians, a fountain leans either side of straight up.
	swayAngle = 0.3
)

// Scene is a deterministic particle stress scene: four fountains whose particle counts are topped up to Target after every step.
type Scene struct {
	// Atlas is the procedural atlas every emitter draws from.
	Atlas *sprite.Atlas
	// Emitters are the fountains, left to right.
	Emitters []*particles.Emitter
	// Target is how many particles the fountains hold together after each Step.
	Target int
	// Steps counts the fixed steps run since the scene was made or reset.
	Steps uint64
	// Seed is the seed the emitters were made from.
	Seed uint64
}

// NewScene builds a scene with DefaultTarget particles whose emitters are seeded from seed.
func NewScene(seed uint64) *Scene {
	atlas := NewAtlas()
	s := &Scene{Atlas: atlas, Seed: seed}
	glows := [fountainCount]string{"cyan", "amber", "magenta", "lime"}
	for i := range fountainCount {
		s.Emitters = append(s.Emitters, &particles.Emitter{
			Atlas:              atlas,
			Frames:             []sprite.Frame{atlas.Frames[glows[i]], atlas.Frames["spark"]},
			X:                  float32(VirtualWidth) * float32(2*i+1) / (2 * fountainCount),
			Y:                  VirtualHeight - 40,
			Shape:              particles.Circle(6),
			LifetimeMin:        1200 * time.Millisecond,
			LifetimeMax:        2200 * time.Millisecond,
			SpeedMin:           260,
			SpeedMax:           430,
			AngleMin:           -math.Pi/2 - 0.22,
			AngleMax:           -math.Pi/2 + 0.22,
			GravityY:           380,
			InitialRotationMin: 0,
			InitialRotationMax: 2 * math.Pi,
			RotationSpeedMin:   -4,
			RotationSpeedMax:   4,
			SizeCurve: particles.FloatCurve{
				{T: 0, Value: 0.6},
				{T: 0.2, Value: 1.3},
				{T: 1, Value: 0.4},
			},
			AlphaCurve: particles.FloatCurve{
				{T: 0, Value: 1},
				{T: 0.7, Value: 0.8},
				{T: 1, Value: 0},
			},
			// Each fountain gets its own stream of the scene's seed, so fountains differ while the scene replays exactly.
			Seed: seed + uint64(i)*0x9e3779b97f4a7c15,
			Z:    i,
		})
	}
	s.SetTarget(DefaultTarget)
	return s
}

// SetTarget sets how many particles the fountains hold, clamped to [MinTarget, MaxTarget] and split evenly between them. Lowering it lets the extra particles live out their lifetimes.
func (s *Scene) SetTarget(n int) {
	n = min(max(n, MinTarget), MaxTarget)
	s.Target = n
	per, rest := n/len(s.Emitters), n%len(s.Emitters)
	for i, e := range s.Emitters {
		e.MaxParticles = per
		if i < rest {
			e.MaxParticles++
		}
	}
}

// Step sways the fountains, advances their particles by dt and tops each fountain up to its share of Target.
func (s *Scene) Step(dt time.Duration) {
	s.Steps++
	phase := 2 * math.Pi * float64(s.Steps%swayPeriod) / swayPeriod
	for i, e := range s.Emitters {
		// Neighbouring fountains sway in opposite directions.
		lean := swayAngle * math.Sin(phase+float64(i)*math.Pi/2)
		e.Rotation = float32(lean)
		e.Update(dt)
		e.Burst(e.MaxParticles - e.Count())
	}
}

// Reset clears every fountain, re-seeds it from Seed and restarts the step count, keeping Target.
func (s *Scene) Reset() {
	s.Steps = 0
	for _, e := range s.Emitters {
		e.Reset()
		e.Rotation = 0
	}
}

// Count returns how many particles are live across all fountains.
func (s *Scene) Count() int {
	n := 0
	for _, e := range s.Emitters {
		n += e.Count()
	}
	return n
}

// Draw fills the background and draws every fountain, scaled to fit size with the scene centered.
func (s *Scene) Draw(d *core.Drawing, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	d.SetFillColor(core.RGB(0x05070D)).FillRect(0, 0, w, h)
	scale := min(w/VirtualWidth, h/VirtualHeight)
	d.Save()
	d.Translate((w-VirtualWidth*scale)/2, (h-VirtualHeight*scale)/2)
	d.Scale(scale, scale)
	for _, e := range s.Emitters {
		e.Draw(d)
	}
	d.Restore()
}

// NewAtlas builds the scene's 64x16 premultiplied atlas in memory: four colored 12x12 glows and a 6x6 white spark.
func NewAtlas() *sprite.Atlas {
	img := image.NewRGBA(image.Rect(0, 0, 64, 16))
	glows := []struct {
		name    string
		r, g, b uint8
	}{
		{"cyan", 0x38, 0xBD, 0xF8},
		{"amber", 0xFB, 0xBF, 0x24},
		{"magenta", 0xF4, 0x72, 0xB6},
		{"lime", 0xA3, 0xE6, 0x35},
	}
	frames := map[string]sprite.Frame{}
	for i, g := range glows {
		x := i * 12
		drawGlow(img, x, 0, 12, g.r, g.g, g.b)
		frames[g.name] = sprite.Frame{Name: g.name, X: float32(x), Y: 0, W: 12, H: 12, Width: 12, Height: 12}
	}
	drawGlow(img, 48, 0, 6, 0xFF, 0xFF, 0xFF)
	frames["spark"] = sprite.Frame{Name: "spark", X: 48, Y: 0, W: 6, H: 6, Width: 6, Height: 6}
	return &sprite.Atlas{
		Image:  core.ImageSource{Pixels: img, Size: core.Size{Width: 64, Height: 16}},
		Frames: frames,
	}
}

// drawGlow paints a radial falloff of the given color into the size x size square at (ox, oy), premultiplied.
func drawGlow(img *image.RGBA, ox, oy, size int, r, g, b uint8) {
	c := float64(size) / 2
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			t := 1 - math.Sqrt(dx*dx+dy*dy)/c
			if t <= 0 {
				continue
			}
			a := t * t
			img.SetRGBA(ox+x, oy+y, color.RGBA{
				R: uint8(float64(r) * a),
				G: uint8(float64(g) * a),
				B: uint8(float64(b) * a),
				A: uint8(255 * a),
			})
		}
	}
}
