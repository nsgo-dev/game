package main

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
)

const (
	stepRate = 60

	defaultOrbitYaw   = 34.0 * math.Pi / 180.0
	defaultOrbitPitch = 24.0 * math.Pi / 180.0
	defaultOrbitDist  = 6.4
	defaultFOVDeg     = 45.0

	minOrbitPitch = 8.0 * math.Pi / 180.0
	maxOrbitPitch = 80.0 * math.Pi / 180.0
	minOrbitDist  = 2.8
	maxOrbitDist  = 12.5

	autoOrbitSpeed = 0.24 // radians/sec
	lightOrbitRate = 0.75 // radians/sec
	coreSpinRate   = 0.55 // radians/sec

	// Indices into Scene.Lights.Point.
	wispLight   = 0
	shrineLight = 1
)

var defaultOrbitTarget = math3d.Vec3{X: 0, Y: 1.25, Z: 0}

// Scene holds the loaded glTF 2.0 diorama, its orbit camera rig, animated node
// handles, and dynamic lighting state.
type Scene struct {
	Doc *gltf.Document

	Camera     *scene3d.Camera
	PrevCamera scene3d.Camera
	Orbit      *scene3d.OrbitController

	AutoOrbit     bool
	AnimateLights bool
	Dragging      bool

	Time       float64
	LightPhase float64
	CorePhase  float64
	Steps      uint64

	// Lights is the scene's render3d.LightRig: a cool moonlight key, the orbiting wisp (Point[wispLight]), the shrine glow (Point[shrineLight]) and a dim twilight ambient, all in linear light.
	Lights render3d.LightRig

	wispNodeIdx int
	coreNodeIdx int
	meshCount   int
	triCount    int
}

// NewScene parses the embedded glTF 2.0 shrine diorama and initializes its
// orbit camera and lights.
func NewScene() (*Scene, error) {
	doc, err := gltf.Load(lanternShrineGLB, nil)
	if err != nil {
		return nil, fmt.Errorf("lantern3d: load glb: %w", err)
	}
	if err := doc.ComputeWorldMatrices(); err != nil {
		return nil, fmt.Errorf("lantern3d: compute world matrices: %w", err)
	}

	s := &Scene{
		Doc:           doc,
		AutoOrbit:     true,
		AnimateLights: true,
		wispNodeIdx:   -1,
		coreNodeIdx:   -1,
	}

	for i := range doc.Nodes {
		n := &doc.Nodes[i]
		switch n.Name {
		case "LightWisp":
			s.wispNodeIdx = i
		case "ShrineCore":
			s.coreNodeIdx = i
		}
		if n.Mesh >= 0 && n.Mesh < len(doc.Meshes) {
			for _, prim := range doc.Meshes[n.Mesh].Primitives {
				if prim.Mode != gltf.ModeTriangles {
					continue
				}
				s.meshCount++
				if len(prim.Indices) > 0 {
					s.triCount += len(prim.Indices) / 3
				} else {
					s.triCount += len(prim.Positions) / 3
				}
			}
		}
	}

	s.Camera = scene3d.NewPerspectiveCamera(
		math3d.Vec3{X: 3.2, Y: 2.8, Z: 5.2},
		math3d.DegToRad(defaultFOVDeg),
		16.0/9.0,
		0.1,
		60.0,
	)
	s.Orbit = scene3d.NewOrbitController(defaultOrbitTarget, defaultOrbitDist)
	s.Orbit.MinPitch = minOrbitPitch
	s.Orbit.MaxPitch = maxOrbitPitch
	s.Orbit.MinDistance = minOrbitDist
	s.Orbit.MaxDistance = maxOrbitDist
	s.Orbit.Damping = 10.0

	s.ResetCamera()
	s.updateLightsAndNodes()
	return s, nil
}

// ResetCamera snaps the orbit camera back to its default vantage point.
func (s *Scene) ResetCamera() {
	s.Orbit.SetTarget(defaultOrbitTarget)
	s.Orbit.SetAngles(defaultOrbitYaw, defaultOrbitPitch)
	s.Orbit.SetDistance(defaultOrbitDist)
	s.Orbit.Snap()
	s.Orbit.Apply(s.Camera)
	s.PrevCamera = *s.Camera
}

// SetViewport updates the perspective camera aspect ratio to match the GPU
// surface dimensions.
func (s *Scene) SetViewport(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	s.Camera.SetViewport(float32(width), float32(height))
	s.PrevCamera.SetViewport(float32(width), float32(height))
}

// Step advances the fixed-step camera damping, auto-orbit, orbiting warm point
// light, and animated glTF scene nodes.
func (s *Scene) Step(dt time.Duration) {
	if dt <= 0 {
		return
	}
	sec := dt.Seconds()
	s.Time += sec
	s.Steps++

	s.PrevCamera = *s.Camera
	if s.AutoOrbit && !s.Dragging {
		s.Orbit.Orbit(float32(autoOrbitSpeed*sec), 0)
	}
	s.Orbit.Update(s.Camera, dt)

	if s.AnimateLights {
		s.LightPhase += lightOrbitRate * sec
		s.CorePhase += coreSpinRate * sec
	}
	s.updateLightsAndNodes()
}

// CameraFor returns the camera pose interpolated between the previous and
// current fixed step by alpha in [0, 1].
func (s *Scene) CameraFor(alpha float32) scene3d.Camera {
	if alpha <= 0 {
		return s.PrevCamera
	}
	if alpha >= 1 {
		return *s.Camera
	}
	return s.PrevCamera.Lerp(s.Camera, alpha)
}

// MeshCount returns the number of drawable glTF primitives instantiated by the
// scene's nodes.
func (s *Scene) MeshCount() int { return s.meshCount }

// TriangleCount returns the total triangle count across all instantiated glTF
// primitives.
func (s *Scene) TriangleCount() int { return s.triCount }

func (s *Scene) updateLightsAndNodes() {
	ang := s.LightPhase
	const orbitRadius = 1.95
	wispX := float32(orbitRadius * math.Cos(ang))
	wispZ := float32(orbitRadius * math.Sin(ang))
	wispY := float32(1.92 + 0.24*math.Sin(ang*2.0))

	pulse := float32(0.92 + 0.12*math.Sin(s.LightPhase*3.3))

	wispPos := math3d.Vec3{X: wispX, Y: wispY, Z: wispZ}
	s.Lights = render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.045, Y: 0.06, Z: 0.10}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{
			Direction: math3d.Vec3{X: -0.52, Y: -0.80, Z: -0.36},
			Color:     math3d.Vec3{X: 0.38, Y: 0.52, Z: 1.00},
			Intensity: 2.2,
		}},
		Point: []render3d.PointLight{
			wispLight: {
				Position:  wispPos,
				Color:     math3d.Vec3{X: 1.00, Y: 0.55, Z: 0.18},
				Intensity: 9 * pulse,
				Range:     8,
			},
			shrineLight: {
				Position:  math3d.Vec3{X: 0, Y: 1.82, Z: 0},
				Color:     math3d.Vec3{X: 1.00, Y: 0.45, Z: 0.12},
				Intensity: 4 * (0.94 + 0.08*float32(math.Cos(s.CorePhase*2.7))),
				Range:     5.5,
			},
		},
	}

	if s.wispNodeIdx >= 0 {
		n := &s.Doc.Nodes[s.wispNodeIdx]
		n.Translation = wispPos
		n.Rotation = math3d.QuatFromAxisAngle(math3d.Vec3Up(), float32(ang*2.2))
		n.HasMatrix = false
	}
	if s.coreNodeIdx >= 0 {
		n := &s.Doc.Nodes[s.coreNodeIdx]
		n.Translation = math3d.Vec3{X: 0, Y: float32(0.04 * math.Sin(s.CorePhase*2.0)), Z: 0}
		n.Rotation = math3d.QuatFromAxisAngle(math3d.Vec3Up(), float32(s.CorePhase))
		n.HasMatrix = false
	}
	_ = s.Doc.ComputeWorldMatrices()
}
