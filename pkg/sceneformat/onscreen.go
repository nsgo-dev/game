package sceneformat

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/tilemap"
)

// OnScreenComponentVersion is the schema version of the built-in "on_screen" component.
const OnScreenComponentVersion = 1

// OnScreenComponentName is the built-in on-screen notifier component name in scene YAML files.
const OnScreenComponentName = "on_screen"

// The values the "on_screen" component's pause list accepts: the parts of the object BuiltScene.Update stops while the notifier is off-screen.
const (
	onScreenPauseParticles = "particles"
	onScreenPauseSkeleton  = "skeleton"
)

// OnScreenComponent returns the ComponentType schema for the built-in "on_screen" component, which reports through its entered and exited events when the object's rectangle enters or leaves every camera view, and pauses the parts its pause list names (the object's particle emitter or skeletal rig) while it is off-screen.
//
// The rectangle is in the object's local space and is transformed by its composed world transform, so moving or rotating the object (or a parent of it) moves the watched rectangle; a rotated rectangle is covered by the axis-aligned rectangle holding its corners.
//
// Register it on a ComponentRegistry via RegisterOnScreen so ParseWith validates on_screen fields and scene connections may wire the entered and exited events.
func OnScreenComponent() ComponentType {
	return ComponentType{
		Name: OnScreenComponentName,
		Doc:  "On-screen notifier: emits entered and exited when the object's rectangle enters or leaves every camera view, and pauses listed parts while off-screen.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: OnScreenComponentVersion, Min: Float64(OnScreenComponentVersion), Max: Float64(OnScreenComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "x", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local X of the watched rectangle's top-left corner."},
			{Name: "y", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local Y of the watched rectangle's top-left corner."},
			{Name: "width", Kind: FieldFloat, Default: 16.0, Min: Float64(0), Step: 1, Doc: "Watched rectangle width in points (an empty rectangle never enters)."},
			{Name: "height", Kind: FieldFloat, Default: 16.0, Min: Float64(0), Step: 1, Doc: "Watched rectangle height in points (an empty rectangle never enters)."},
			{Name: "margin", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Step: 1, Doc: "Extra points around a view's bounds an on-screen object may reach before exited fires (hysteresis band)."},
			{Name: "pause", Kind: FieldStringList, Doc: `Parts BuiltScene.Update skips while off-screen: "particles" (the object's emitter) or "skeleton" (its rig); empty notifies only.`},
		},
		Events: []Event{
			{Name: "entered", Doc: "Emitted when the object's rectangle starts overlapping a camera view (or the first update sees it inside one)."},
			{Name: "exited", Doc: "Emitted when the object's rectangle stops overlapping every camera view, margin included."},
		},
	}
}

// RegisterOnScreen registers the built-in "on_screen" component type on reg.
func RegisterOnScreen(reg *ComponentRegistry) error {
	return reg.Register(OnScreenComponent())
}

// OnScreenComponentRegistry returns a new ComponentRegistry pre-populated with the built-in "on_screen" component.
func OnScreenComponentRegistry() *ComponentRegistry {
	reg := NewComponentRegistry()
	if err := RegisterOnScreen(reg); err != nil {
		panic(err)
	}
	return reg
}

var builtinOnScreenReg = OnScreenComponentRegistry()

type decodedOnScreen struct {
	local          math2d.Rect
	margin         float32
	pauseParticles bool
	pauseSkeleton  bool
}

func decodeOnScreen(objectID string, raw any) (decodedOnScreen, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return decodedOnScreen{}, &ComponentError{
			Object:    objectID,
			Component: OnScreenComponentName,
			Msg:       fmt.Sprintf("must be a mapping of fields, got %s", describeValue(raw)),
		}
	}
	resolved, err := builtinOnScreenReg.Resolve(OnScreenComponentName, m)
	if err != nil {
		var ce *ComponentError
		if errors.As(err, &ce) {
			ce.Object = objectID
		}
		return decodedOnScreen{}, err
	}
	var d decodedOnScreen
	for _, p := range resolved["pause"].([]string) {
		switch p {
		case onScreenPauseParticles:
			d.pauseParticles = true
		case onScreenPauseSkeleton:
			d.pauseSkeleton = true
		default:
			return decodedOnScreen{}, &ComponentError{
				Object:    objectID,
				Component: OnScreenComponentName,
				Field:     "pause",
				Msg:       fmt.Sprintf("value %q must be one of %s, %s", p, onScreenPauseParticles, onScreenPauseSkeleton),
			}
		}
	}
	d.local = math2d.Rect{
		X: float32(resolved["x"].(float64)),
		Y: float32(resolved["y"].(float64)),
		W: float32(resolved["width"].(float64)),
		H: float32(resolved["height"].(float64)),
	}
	d.margin = float32(resolved["margin"].(float64))
	return d, nil
}

// ScreenNotifier is the runtime state of one built-in "on_screen" component on a built object. BuiltScene.Update steps it against the camera views (its own camera's plus any extra views passed to UpdateWithViews), fires the component's entered and exited events through ScreenSignals, and pauses the object's particle emitter or skeletal rig while the notifier is off-screen.
type ScreenNotifier struct {
	scene.ScreenNotifier
	// Object is the built object carrying the "on_screen" component.
	Object *BuiltObject
	// PauseParticles stops the object's particle emitter updates while off-screen.
	PauseParticles bool
	// PauseSkeleton stops the object's skeletal rig updates while off-screen.
	PauseSkeleton bool

	local math2d.Rect
}

// extractScreenNotifiers builds the runtime notifiers of the scene's "on_screen" components in document order and wires their events to the scene's ScreenSignals. It is called by Build.
func extractScreenNotifiers(built *BuiltScene) ([]*ScreenNotifier, error) {
	var out []*ScreenNotifier
	for li := range built.Doc.Layers {
		var err error
		WalkObjects(built.Doc.Layers[li].Objects, func(o, _ *ObjectDoc) bool {
			raw, has := o.Components[OnScreenComponentName]
			if !has {
				return true
			}
			d, e := decodeOnScreen(o.ID, raw)
			if e != nil {
				err = e
				return false
			}
			bo := built.Objects[o.ID]
			if bo == nil {
				return true
			}
			id := o.ID
			n := &ScreenNotifier{
				Object:         bo,
				PauseParticles: d.pauseParticles,
				PauseSkeleton:  d.pauseSkeleton,
				local:          d.local,
			}
			n.Margin = d.margin
			n.OnEnter = func() {
				if sig := built.ScreenSignals; sig != nil {
					_ = sig.Emit(id, "on_screen.entered", nil)
				}
			}
			n.OnExit = func() {
				if sig := built.ScreenSignals; sig != nil {
					_ = sig.Emit(id, "on_screen.exited", nil)
				}
			}
			out = append(out, n)
			return true
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// syncRect recomputes the notifier's watched rectangle from the object's current world transform and stores it in Rect. A rotated or sheared transform is covered by the axis-aligned rectangle holding the local rectangle's transformed corners.
func (n *ScreenNotifier) syncRect() {
	w := n.Object.World
	r := n.local
	if r.W <= 0 || r.H <= 0 {
		n.Rect = math2d.Rect{}.Bounds()
		return
	}
	p := w.TransformPoint(math2d.V2(r.X, r.Y))
	minX, maxX, minY, maxY := p.X, p.X, p.Y, p.Y
	for _, c := range [3][2]float32{{r.X + r.W, r.Y}, {r.X, r.Y + r.H}, {r.X + r.W, r.Y + r.H}} {
		p = w.TransformPoint(math2d.V2(c[0], c[1]))
		minX, maxX = min(minX, p.X), max(maxX, p.X)
		minY, maxY = min(minY, p.Y), max(maxY, p.Y)
	}
	n.Rect = math2d.Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}.Bounds()
}

// notifierView returns the camera view the scene itself shows, its virtual viewport through the scene camera, or nil when the scene has no virtual size.
func (s *BuiltScene) notifierView() *tilemap.Camera {
	if s.Doc == nil {
		return nil
	}
	vw, vh := s.Doc.VirtualWidth, s.Doc.VirtualHeight
	if vw <= 0 || vh <= 0 {
		return nil
	}
	if s.Camera != nil {
		cam := s.Camera
		zoom := cam.EffectiveZoom()
		x, y := clampCamera(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)
		return &tilemap.Camera{X: x, Y: y, Zoom: zoom, Rotation: cam.Rotation, Width: vw, Height: vh}
	}
	return &tilemap.Camera{X: vw / 2, Y: vh / 2, Zoom: 1, Width: vw, Height: vh}
}

// notifierViews returns the scene's own camera view followed by extra (nil views dropped).
func (s *BuiltScene) notifierViews(extra []*tilemap.Camera) []*tilemap.Camera {
	views := make([]*tilemap.Camera, 0, len(extra)+1)
	if v := s.notifierView(); v != nil {
		views = append(views, v)
	}
	for _, v := range extra {
		if v != nil {
			views = append(views, v)
		}
	}
	return views
}

// paused reports whether the notifier of bo currently pauses the part kind names: "particles" or "skeleton".
func (s *BuiltScene) paused(notifiers map[*BuiltObject]*ScreenNotifier, bo *BuiltObject, kind string) bool {
	n := notifiers[bo]
	if n == nil || n.OnScreen() {
		return false
	}
	switch kind {
	case onScreenPauseParticles:
		return n.PauseParticles
	case onScreenPauseSkeleton:
		return n.PauseSkeleton
	}
	return false
}
