package main

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
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

// shrineGlowLocal is where the shrine's glow sits in the ShrineCore node's space: the center of the crystal bipyramid tools/genassets builds for that mesh.
var shrineGlowLocal = math3d.Vec3{X: 0, Y: 1.48, Z: 0}

// Scene holds the 3D level built from its scene file (levelScenePath), its orbit camera rig, animated node handles, and dynamic lighting state.
type Scene struct {
	// World holds the level's node tree, and Level the geometry and materials scenemodel.Build attached to its nodes.
	World *scene3d.Scene
	Level *scenemodel.Built
	// Model is the glTF diorama the level's scene file names.
	Model *gltf.Document

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

	// wisp and core are the nodes Build made for the diorama's LightWisp and ShrineCore nodes.
	wisp, core *scene3d.Node
	meshCount  int
	triCount   int
}

// NewScene builds the 3D level from its embedded scene file, loading the glTF 2.0 shrine diorama it names, and initializes its orbit camera and lights.
func NewScene() (*Scene, error) {
	data, err := fs.ReadFile(projectFiles, levelScenePath)
	if err != nil {
		return nil, fmt.Errorf("lantern3d: %w", err)
	}
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: levelScenePath})
	if err != nil {
		return nil, fmt.Errorf("lantern3d: %w", err)
	}
	assets := scenemodel.NewAssets(projectFiles)
	world := scene3d.NewScene()
	level, err := scenemodel.Build(doc, world.Root(), assets)
	if err != nil {
		return nil, fmt.Errorf("lantern3d: build %s: %w", levelScenePath, err)
	}
	var obj *sceneformat.ObjectDoc
	diorama := level.Objects["diorama"]
	if diorama != nil {
		obj, _ = diorama.UserData.(*sceneformat.ObjectDoc)
	}
	if obj == nil || obj.Model == "" {
		return nil, errors.New("lantern3d: the level has no diorama object with a model")
	}
	model, err := assets.Model(obj.Model)
	if err != nil {
		return nil, fmt.Errorf("lantern3d: %w", err)
	}

	s := &Scene{
		World:         world,
		Level:         level,
		Model:         model,
		AutoOrbit:     true,
		AnimateLights: true,
		wisp:          diorama.FindByName("LightWisp"),
		core:          diorama.FindByName("ShrineCore"),
	}
	for _, d := range level.Drawables {
		for _, p := range d.Parts {
			s.meshCount++
			if len(p.Mesh.Indices) > 0 {
				s.triCount += len(p.Mesh.Indices) / 3
			} else {
				s.triCount += len(p.Mesh.Positions) / 3
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

	if s.wisp == nil || s.core == nil {
		return nil, errors.New("lantern3d: the diorama lacks the LightWisp or ShrineCore node")
	}

	s.ResetCamera()
	if err := s.updateLightsAndNodes(); err != nil {
		return nil, err
	}
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

// SetViewport updates the perspective camera aspect ratio to match the GPU surface dimensions.
func (s *Scene) SetViewport(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	s.Camera.SetViewport(float32(width), float32(height))
	s.PrevCamera.SetViewport(float32(width), float32(height))
}

// Step advances the fixed-step camera damping, auto-orbit, orbiting warm point light, and animated diorama nodes. It returns an error when the node hierarchy no longer yields world matrices.
func (s *Scene) Step(dt time.Duration) error {
	if dt <= 0 {
		return nil
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
	return s.updateLightsAndNodes()
}

// CameraFor returns the camera pose interpolated between the previous and current fixed step by alpha in [0, 1].
func (s *Scene) CameraFor(alpha float32) scene3d.Camera {
	if alpha <= 0 {
		return s.PrevCamera
	}
	if alpha >= 1 {
		return *s.Camera
	}
	return s.PrevCamera.Lerp(s.Camera, alpha)
}

// MeshCount returns the number of parts the level's nodes draw, one per glTF primitive.
func (s *Scene) MeshCount() int { return s.meshCount }

// TriangleCount returns the total triangle count across the parts the level's nodes draw.
func (s *Scene) TriangleCount() int { return s.triCount }

// updateLightsAndNodes moves the wisp and the shrine core nodes for the current phases and places the two point lights where those nodes are in world space.
func (s *Scene) updateLightsAndNodes() error {
	ang := s.LightPhase
	const orbitRadius = 1.95
	wispPos := math3d.Vec3{
		X: float32(orbitRadius * math.Cos(ang)),
		Y: float32(1.92 + 0.24*math.Sin(ang*2.0)),
		Z: float32(orbitRadius * math.Sin(ang)),
	}
	pulse := float32(0.92 + 0.12*math.Sin(s.LightPhase*3.3))

	local, err := worldToParent(s.wisp, wispPos)
	if err != nil {
		return err
	}
	s.wisp.SetPosition(local)
	s.wisp.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), float32(ang*2.2)))

	s.core.SetPosition(math3d.Vec3{X: 0, Y: float32(0.04 * math.Sin(s.CorePhase*2.0)), Z: 0})
	s.core.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), float32(s.CorePhase)))

	s.Lights = render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.045, Y: 0.06, Z: 0.10}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{
			Direction: math3d.Vec3{X: -0.52, Y: -0.80, Z: -0.36},
			Color:     math3d.Vec3{X: 0.38, Y: 0.52, Z: 1.00},
			Intensity: 2.2,
		}},
		Point: []render3d.PointLight{
			wispLight: {
				Position:  s.wisp.WorldMatrix().TransformPoint(math3d.Vec3{}),
				Color:     math3d.Vec3{X: 1.00, Y: 0.55, Z: 0.18},
				Intensity: 9 * pulse,
				Range:     8,
			},
			shrineLight: {
				Position:  s.core.WorldMatrix().TransformPoint(shrineGlowLocal),
				Color:     math3d.Vec3{X: 1.00, Y: 0.45, Z: 0.12},
				Intensity: 4 * (0.94 + 0.08*float32(math.Cos(s.CorePhase*2.7))),
				Range:     5.5,
			},
		},
	}
	return nil
}

// worldToParent converts a world-space point into the space of n's parent, so it can be the node's local translation whatever transforms its ancestors carry.
func worldToParent(n *scene3d.Node, p math3d.Vec3) (math3d.Vec3, error) {
	parent := n.Parent()
	if parent == nil {
		return p, nil
	}
	inv, ok := parent.WorldMatrix().Inverse()
	if !ok {
		return math3d.Vec3{}, fmt.Errorf("lantern3d: node %q has a singular parent transform", n.Name)
	}
	return inv.TransformPoint(p), nil
}
