package sceneformat

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/motion"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/game/pkg/tilemap/tileshape"
	"github.com/nsgo-dev/nsgo/core"
)

// BehaviorComponentVersion is the schema version of the built-in 2D motion behavior components.
const BehaviorComponentVersion = 1

// Built-in 2D motion behavior component type names in scene YAML files.
const (
	// BulletComponentName moves an object linearly with speed, acceleration, gravity, optional rotation and solid bouncing.
	BulletComponentName = "bullet"
	// SineComponentName oscillates an object's x, y, angle, scale or alpha periodically.
	SineComponentName = "sine"
	// RotateComponentName spins an object continuously at an angular speed and acceleration.
	RotateComponentName = "rotate"
	// OrbitComponentName revolves an object around a target object or its initial position.
	OrbitComponentName = "orbit"
	// MoveToComponentName drives an object toward a target coordinate with optional acceleration and solid stopping.
	MoveToComponentName = "move_to"
	// WrapComponentName wraps an object leaving one side of the scene or view onto the opposite side.
	WrapComponentName = "wrap"
	// BoundComponentName clamps an object inside the scene bounds by its edge or origin.
	BoundComponentName = "bound"
	// DestroyOutsideComponentName frees an object once it travels past the scene or view bounds plus a margin.
	DestroyOutsideComponentName = "destroy_outside"
	// FadeComponentName drives an opacity fade-in, wait and fade-out envelope, optionally freeing the object at the end.
	FadeComponentName = "fade"
	// FlashComponentName blinks an object's visibility on and off for a requested duration.
	FlashComponentName = "flash"
)

var (
	sineProperties      = []string{"x", "y", "angle", "scale", "alpha"}
	sineWaves           = []string{string(motion.WaveSine), string(motion.WaveTriangle), string(motion.WaveSawtooth), string(motion.WaveReverseSawtooth), string(motion.WaveSquare)}
	boundsModes         = []string{"scene", "view"}
	boundClampModes     = []string{"edge", "origin"}
	behaviorNamesSorted = []string{
		BoundComponentName,
		BulletComponentName,
		DestroyOutsideComponentName,
		FadeComponentName,
		FlashComponentName,
		MoveToComponentName,
		OrbitComponentName,
		RotateComponentName,
		SineComponentName,
		WrapComponentName,
	}
)

// BehaviorComponentNames returns the names of all ten built-in 2D motion behavior component types in sorted order.
func BehaviorComponentNames() []string {
	return slices.Clone(behaviorNamesSorted)
}

// IsBehaviorComponent reports whether name is one of the ten built-in 2D motion behavior components.
func IsBehaviorComponent(name string) bool {
	switch name {
	case BulletComponentName, SineComponentName, RotateComponentName, OrbitComponentName,
		MoveToComponentName, WrapComponentName, BoundComponentName, DestroyOutsideComponentName,
		FadeComponentName, FlashComponentName:
		return true
	default:
		return false
	}
}

// BehaviorSceneProvider may be implemented by Call.Context to supply the *BuiltScene
// driven by motion behavior connection handlers when Call.Context is not a *BuiltScene directly.
// *BuiltScene implements BehaviorSceneProvider.
type BehaviorSceneProvider interface {
	BehaviorScene() *BuiltScene
}

// BehaviorScene returns s so *BuiltScene satisfies BehaviorSceneProvider.
func (s *BuiltScene) BehaviorScene() *BuiltScene { return s }

// BulletBouncedEvent is the payload emitted with an object's "bullet.bounced" event.
type BulletBouncedEvent struct {
	ObjectID string
	X, Y     float32
	NormalX  float32
	NormalY  float32
	Angle    float64
}

// MoveToArrivedEvent is the payload emitted with an object's "move_to.arrived" event.
type MoveToArrivedEvent struct {
	ObjectID string
	X, Y     float32
}

// WrapEvent is the payload emitted with an object's "wrap.wrapped" event.
type WrapEvent struct {
	ObjectID string
	X, Y     float32
}

// DestroyOutsideEvent is the payload emitted with an object's "destroy_outside.destroyed" event.
type DestroyOutsideEvent struct {
	ObjectID string
	X, Y     float32
}

// FadeFinishedEvent is the payload emitted with an object's "fade.finished" event.
type FadeFinishedEvent struct {
	ObjectID  string
	Destroyed bool
}

// FlashFinishedEvent is the payload emitted with an object's "flash.finished" event.
type FlashFinishedEvent struct {
	ObjectID string
}

func behaviorVersionField() Field {
	return Field{
		Name:    "version",
		Kind:    FieldInt,
		Default: BehaviorComponentVersion,
		Min:     Float64(BehaviorComponentVersion),
		Max:     Float64(BehaviorComponentVersion),
		Step:    1,
		Doc:     "Component schema version (1).",
	}
}

func behaviorEnabledField() Field {
	return Field{
		Name:    "enabled",
		Kind:    FieldBool,
		Default: true,
		Doc:     "When false, BuiltScene.Step pauses this behavior.",
	}
}

// BulletComponent returns the ComponentType schema for the built-in "bullet" motion behavior.
func BulletComponent() ComponentType {
	return ComponentType{
		Name: BulletComponentName,
		Doc:  "Linear 2D projectile motion with speed, acceleration, gravity, angle alignment and solid bouncing.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "speed", Kind: FieldFloat, Default: 240.0, Step: 10, Doc: "Initial travel speed in points per second along angle."},
			{Name: "acceleration", Kind: FieldFloat, Default: 0.0, Step: 10, Doc: "Linear acceleration along travel direction in points/s^2."},
			{Name: "gravity", Kind: FieldFloat, Default: 0.0, Step: 10, Doc: "Downward (+Y) gravity acceleration in points/s^2."},
			{Name: "angle", Kind: FieldFloat, Default: 0.0, Step: 5, Doc: "Initial travel angle in degrees (0 = right +X, 90 = down +Y)."},
			{Name: "setAngle", Kind: FieldBool, Default: true, Doc: "When true, updates the object's rotation to face its velocity."},
			{Name: "bounceOffSolids", Kind: FieldBool, Default: false, Doc: "When true, bounces off solid physics bodies and solid tilemap tiles."},
			behaviorEnabledField(),
		},
		Events: []Event{
			{Name: "bounced", Doc: "Emitted when the bullet bounces off a solid surface."},
		},
		Handlers: []Handler{
			{
				Name: "setSpeed",
				Doc:  "Sets the bullet's travel speed in points per second.",
				Params: []Field{
					{Name: "speed", Kind: FieldFloat, Default: 0.0, Step: 10, Doc: "New speed in points per second."},
				},
				Func: handleBulletSetSpeed,
			},
			{
				Name: "setAngle",
				Doc:  "Sets the bullet's travel angle in degrees.",
				Params: []Field{
					{Name: "angle", Kind: FieldFloat, Default: 0.0, Step: 5, Doc: "New angle in degrees (0 = +X, 90 = +Y)."},
				},
				Func: handleBulletSetAngle,
			},
			{Name: "start", Doc: "Resumes bullet motion.", Func: handleBehaviorStart(BulletComponentName)},
			{Name: "stop", Doc: "Pauses bullet motion.", Func: handleBehaviorStop(BulletComponentName)},
		},
	}
}

// SineComponent returns the ComponentType schema for the built-in "sine" motion behavior.
func SineComponent() ComponentType {
	return ComponentType{
		Name: SineComponentName,
		Doc:  "Periodic oscillation of x, y, angle, scale or alpha with configurable wave, period, magnitude and phase.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "property", Kind: FieldString, Default: "y", Enum: sineProperties, Doc: `Property to oscillate: "x", "y", "angle" (degrees), "scale", or "alpha".`},
			{Name: "period", Kind: FieldFloat, Default: 1.5, Min: Float64(0), Step: 0.1, Doc: "Duration of one full cycle in seconds (0 pauses oscillation)."},
			{Name: "magnitude", Kind: FieldFloat, Default: 6.0, Step: 1, Doc: "Peak oscillation amplitude in the property's units."},
			{Name: "wave", Kind: FieldString, Default: string(motion.WaveSine), Enum: sineWaves, Doc: `Waveform shape: "sine", "triangle", "sawtooth", "reverse_sawtooth", or "square".`},
			{Name: "phase", Kind: FieldFloat, Default: 0.0, Step: 0.05, Doc: "Initial phase offset in cycles (0.25 = quarter period, 0.5 = half period)."},
			behaviorEnabledField(),
		},
		Handlers: []Handler{
			{Name: "start", Doc: "Resumes sine oscillation.", Func: handleBehaviorStart(SineComponentName)},
			{Name: "stop", Doc: "Pauses sine oscillation.", Func: handleBehaviorStop(SineComponentName)},
		},
	}
}

// RotateComponent returns the ComponentType schema for the built-in "rotate" motion behavior.
func RotateComponent() ComponentType {
	return ComponentType{
		Name: RotateComponentName,
		Doc:  "Continuous 2D rotation at an angular speed and optional angular acceleration.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "speed", Kind: FieldFloat, Default: 180.0, Step: 5, Doc: "Angular speed in degrees per second (positive = clockwise)."},
			{Name: "acceleration", Kind: FieldFloat, Default: 0.0, Step: 5, Doc: "Angular acceleration in degrees per second squared."},
			behaviorEnabledField(),
		},
		Handlers: []Handler{
			{
				Name: "setSpeed",
				Doc:  "Sets the rotation speed in degrees per second.",
				Params: []Field{
					{Name: "speed", Kind: FieldFloat, Default: 0.0, Step: 5, Doc: "New angular speed in degrees per second."},
				},
				Func: handleRotateSetSpeed,
			},
			{Name: "start", Doc: "Resumes rotation.", Func: handleBehaviorStart(RotateComponentName)},
			{Name: "stop", Doc: "Pauses rotation.", Func: handleBehaviorStop(RotateComponentName)},
		},
	}
}

// OrbitComponent returns the ComponentType schema for the built-in "orbit" motion behavior.
func OrbitComponent() ComponentType {
	return ComponentType{
		Name: OrbitComponentName,
		Doc:  "Revolves the object at a fixed radius around a target object ID or its initial position.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "target", Kind: FieldString, Default: "", Doc: "Optional ID of the scene object to orbit; empty orbits the object's starting position."},
			{Name: "radius", Kind: FieldFloat, Default: 32.0, Min: Float64(0), Step: 2, Doc: "Orbit radius in points (>= 0)."},
			{Name: "speed", Kind: FieldFloat, Default: 90.0, Step: 5, Doc: "Angular orbit speed in degrees per second."},
			{Name: "angle", Kind: FieldFloat, Default: 0.0, Step: 5, Doc: "Initial orbit angle in degrees (0 = +X, 90 = +Y)."},
			{Name: "setAngle", Kind: FieldBool, Default: false, Doc: "When true, sets the object's rotation to its orbit angle."},
			behaviorEnabledField(),
		},
		Handlers: []Handler{
			{
				Name: "setSpeed",
				Doc:  "Sets the orbit angular speed in degrees per second.",
				Params: []Field{
					{Name: "speed", Kind: FieldFloat, Default: 0.0, Step: 5, Doc: "New orbit angular speed in degrees per second."},
				},
				Func: handleOrbitSetSpeed,
			},
			{Name: "start", Doc: "Resumes orbiting.", Func: handleBehaviorStart(OrbitComponentName)},
			{Name: "stop", Doc: "Pauses orbiting.", Func: handleBehaviorStop(OrbitComponentName)},
		},
	}
}

// MoveToComponent returns the ComponentType schema for the built-in "move_to" motion behavior.
func MoveToComponent() ComponentType {
	return ComponentType{
		Name: MoveToComponentName,
		Doc:  "Moves the object directly toward a destination coordinate with optional acceleration and solid stopping.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "maxSpeed", Kind: FieldFloat, Default: 120.0, Min: Float64(0), Step: 10, Doc: "Maximum movement speed in points per second (>= 0)."},
			{Name: "acceleration", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Step: 10, Doc: "Acceleration in points/s^2 (0 reaches maxSpeed immediately)."},
			{Name: "stopOnSolids", Kind: FieldBool, Default: false, Doc: "When true, halts movement upon hitting a solid physics collider or tile."},
			{Name: "targetX", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Initial destination X when active is true."},
			{Name: "targetY", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Initial destination Y when active is true."},
			{Name: "active", Kind: FieldBool, Default: false, Doc: "When true, begins moving toward (targetX, targetY) immediately."},
			behaviorEnabledField(),
		},
		Events: []Event{
			{Name: "arrived", Doc: "Emitted when the object reaches its moveTo target."},
		},
		Handlers: []Handler{
			{
				Name: "moveTo",
				Doc:  "Starts moving the object toward (x, y).",
				Params: []Field{
					{Name: "x", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Target X coordinate."},
					{Name: "y", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Target Y coordinate."},
				},
				Func: handleMoveToTarget,
			},
			{
				Name: "setSpeed",
				Doc:  "Sets the maximum movement speed in points per second.",
				Params: []Field{
					{Name: "speed", Kind: FieldFloat, Default: 120.0, Min: Float64(0), Step: 10, Doc: "New maxSpeed in points per second (>= 0)."},
				},
				Func: handleMoveToSetSpeed,
			},
			{Name: "stop", Doc: "Stops movement immediately.", Func: handleMoveToStop},
		},
	}
}

// WrapComponent returns the ComponentType schema for the built-in "wrap" motion behavior.
func WrapComponent() ComponentType {
	return ComponentType{
		Name: WrapComponentName,
		Doc:  "Wraps the object from one edge of the scene or camera view to the opposite edge.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "mode", Kind: FieldString, Default: "scene", Enum: boundsModes, Doc: `Bounds to wrap across: "scene" (virtual size) or "view" (active camera view).`},
			{Name: "margin", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Step: 1, Doc: "Extra points outside the bounds before wrapping (>= 0)."},
			behaviorEnabledField(),
		},
		Events: []Event{
			{Name: "wrapped", Doc: "Emitted when the object wraps across a boundary."},
		},
		Handlers: []Handler{
			{Name: "start", Doc: "Enables edge wrapping.", Func: handleBehaviorStart(WrapComponentName)},
			{Name: "stop", Doc: "Disables edge wrapping.", Func: handleBehaviorStop(WrapComponentName)},
		},
	}
}

// BoundComponent returns the ComponentType schema for the built-in "bound" motion behavior.
func BoundComponent() ComponentType {
	return ComponentType{
		Name: BoundComponentName,
		Doc:  "Keeps the object clamped inside the scene bounds by its edge box or origin.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "mode", Kind: FieldString, Default: "edge", Enum: boundClampModes, Doc: `Clamp mode: "edge" (keep sprite/collider box inside) or "origin" (keep origin inside).`},
			behaviorEnabledField(),
		},
		Handlers: []Handler{
			{Name: "start", Doc: "Enables scene bounds clamping.", Func: handleBehaviorStart(BoundComponentName)},
			{Name: "stop", Doc: "Disables scene bounds clamping.", Func: handleBehaviorStop(BoundComponentName)},
		},
	}
}

// DestroyOutsideComponent returns the ComponentType schema for the built-in "destroy_outside" motion behavior.
func DestroyOutsideComponent() ComponentType {
	return ComponentType{
		Name: DestroyOutsideComponentName,
		Doc:  "Frees the object once it leaves the scene or view bounds by more than margin points.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "margin", Kind: FieldFloat, Default: 64.0, Min: Float64(0), Step: 4, Doc: "Distance outside the bounds before the object is freed (>= 0)."},
			{Name: "mode", Kind: FieldString, Default: "scene", Enum: boundsModes, Doc: `Bounds to check against: "scene" (virtual size) or "view" (active camera view).`},
			behaviorEnabledField(),
		},
		Events: []Event{
			{Name: "destroyed", Doc: "Emitted just before the object is freed for leaving the bounds."},
		},
		Handlers: []Handler{
			{Name: "start", Doc: "Enables outside-bounds destruction.", Func: handleBehaviorStart(DestroyOutsideComponentName)},
			{Name: "stop", Doc: "Disables outside-bounds destruction.", Func: handleBehaviorStop(DestroyOutsideComponentName)},
		},
	}
}

// FadeComponent returns the ComponentType schema for the built-in "fade" motion behavior.
func FadeComponent() ComponentType {
	return ComponentType{
		Name: FadeComponentName,
		Doc:  "Animates opacity through fade-in, wait and fade-out phases, optionally freeing the object when finished.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "fadeIn", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Step: 0.1, Doc: "Fade-in duration in seconds (>= 0)."},
			{Name: "wait", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.1, Doc: "Hold duration at full opacity in seconds (>= 0)."},
			{Name: "fadeOut", Kind: FieldFloat, Default: 0.5, Min: Float64(0), Step: 0.1, Doc: "Fade-out duration in seconds (>= 0)."},
			{Name: "destroy", Kind: FieldBool, Default: true, Doc: "When true, frees the object once fadeOut completes."},
			{Name: "autostart", Kind: FieldBool, Default: true, Doc: "When true, begins fading immediately on the first step."},
			behaviorEnabledField(),
		},
		Events: []Event{
			{Name: "finished", Doc: "Emitted when the fade envelope reaches the end of fadeOut."},
		},
		Handlers: []Handler{
			{Name: "start", Doc: "Restarts the fade envelope from the beginning.", Func: handleFadeStart},
			{Name: "stop", Doc: "Pauses the fade envelope.", Func: handleFadeStop},
		},
	}
}

// FlashComponent returns the ComponentType schema for the built-in "flash" motion behavior.
func FlashComponent() ComponentType {
	return ComponentType{
		Name: FlashComponentName,
		Doc:  "Toggles visibility on and off for a requested duration, then restores the object's opacity.",
		Fields: []Field{
			behaviorVersionField(),
			{Name: "on", Kind: FieldFloat, Default: 0.05, Min: Float64(0), Step: 0.01, Doc: "Visible interval duration in seconds (>= 0)."},
			{Name: "off", Kind: FieldFloat, Default: 0.05, Min: Float64(0), Step: 0.01, Doc: "Hidden interval duration in seconds (>= 0)."},
			{Name: "duration", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Step: 0.05, Doc: "Optional initial flash duration in seconds (0 waits for the flash handler)."},
			behaviorEnabledField(),
		},
		Events: []Event{
			{Name: "finished", Doc: "Emitted when the flash duration completes and visibility is restored."},
		},
		Handlers: []Handler{
			{
				Name: "flash",
				Doc:  "Starts flashing for duration seconds.",
				Params: []Field{
					{Name: "duration", Kind: FieldFloat, Default: 0.5, Min: Float64(0), Step: 0.05, Doc: "Flash duration in seconds (>= 0)."},
				},
				Func: handleFlashTrigger,
			},
			{Name: "stop", Doc: "Stops flashing immediately and restores visibility.", Func: handleFlashStop},
		},
	}
}

// BehaviorComponents returns all ten built-in 2D motion behavior ComponentTypes in canonical order.
func BehaviorComponents() []ComponentType {
	return []ComponentType{
		BulletComponent(),
		SineComponent(),
		RotateComponent(),
		OrbitComponent(),
		MoveToComponent(),
		WrapComponent(),
		BoundComponent(),
		DestroyOutsideComponent(),
		FadeComponent(),
		FlashComponent(),
	}
}

// RegisterBehaviorComponents registers all ten built-in 2D motion behavior component types on reg.
func RegisterBehaviorComponents(reg *ComponentRegistry) error {
	for _, ct := range BehaviorComponents() {
		if err := reg.Register(ct); err != nil {
			return err
		}
	}
	return nil
}

// BehaviorComponentRegistry returns a new ComponentRegistry pre-populated with all ten built-in 2D motion behavior components.
func BehaviorComponentRegistry() *ComponentRegistry {
	reg := NewComponentRegistry()
	if err := RegisterBehaviorComponents(reg); err != nil {
		panic(err)
	}
	return reg
}

var builtinBehaviorReg = BehaviorComponentRegistry()

// objectBehaviors holds all built-in motion behavior runtime states attached to one BuiltObject.
type objectBehaviors struct {
	bo *BuiltObject

	hasBullet bool
	bullet    motion.Bullet

	hasMoveTo bool
	moveTo    motion.MoveTo

	hasOrbit     bool
	orbit        motion.Orbit
	orbitTarget  string
	orbitCenterX float64
	orbitCenterY float64

	hasRotate bool
	rotate    motion.Rotate

	hasSine          bool
	sine             motion.Sine
	sineProp         string
	sineUnclampAlpha float64

	hasWrap  bool
	wrap     motion.Wrap
	wrapMode string

	hasBound bool
	bound    motion.Bound

	hasDestroyOutside  bool
	destroyOutside     motion.DestroyOutside
	destroyOutsideMode string

	hasFade bool
	fade    motion.Fade

	hasFlash       bool
	flash          motion.Flash
	flashHidden    bool
	flashBaseAlpha float32
}

func decodeBehaviorComponent(objectID, compName string, raw any) (map[string]any, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, &ComponentError{
			Object:    objectID,
			Component: compName,
			Msg:       fmt.Sprintf("must be a mapping of fields, got %s", describeValue(raw)),
		}
	}
	resolved, err := builtinBehaviorReg.Resolve(compName, m)
	if err != nil {
		var ce *ComponentError
		if errors.As(err, &ce) {
			ce.Object = objectID
		}
		return nil, err
	}
	return resolved, nil
}

// extractBehaviors builds the runtime state for all built-in motion behaviors in document order.
func extractBehaviors(built *BuiltScene) ([]*objectBehaviors, map[string]*objectBehaviors, error) {
	var list []*objectBehaviors
	var byID map[string]*objectBehaviors

	for li := range built.Doc.Layers {
		var walkErr error
		WalkObjects(built.Doc.Layers[li].Objects, func(o, _ *ObjectDoc) bool {
			if len(o.Components) == 0 {
				return true
			}
			bo := built.Objects[o.ID]
			if bo == nil {
				return true
			}
			var ob *objectBehaviors
			ensure := func() *objectBehaviors {
				if ob == nil {
					ob = &objectBehaviors{bo: bo}
				}
				return ob
			}

			if raw, has := o.Components[BulletComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, BulletComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasBullet = true
				b.bullet = motion.NewBullet(
					res["speed"].(float64),
					res["acceleration"].(float64),
					res["gravity"].(float64),
					res["angle"].(float64),
					res["setAngle"].(bool),
					res["bounceOffSolids"].(bool),
				)
				b.bullet.Enabled = res["enabled"].(bool)
			}

			if raw, has := o.Components[SineComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, SineComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasSine = true
				b.sineProp = res["property"].(string)
				b.sine = motion.NewSine(
					res["period"].(float64),
					res["magnitude"].(float64),
					motion.WaveKind(res["wave"].(string)),
					res["phase"].(float64),
				)
				b.sine.Enabled = res["enabled"].(bool)
				b.sineUnclampAlpha = float64(o.EffectiveAlpha())
			}

			if raw, has := o.Components[RotateComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, RotateComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasRotate = true
				b.rotate = motion.NewRotate(
					res["speed"].(float64),
					res["acceleration"].(float64),
				)
				b.rotate.Enabled = res["enabled"].(bool)
			}

			if raw, has := o.Components[OrbitComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, OrbitComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasOrbit = true
				b.orbitTarget = res["target"].(string)
				b.orbitCenterX = float64(o.X)
				b.orbitCenterY = float64(o.Y)
				b.orbit = motion.NewOrbit(
					res["radius"].(float64),
					res["speed"].(float64),
					res["angle"].(float64),
					res["setAngle"].(bool),
				)
				b.orbit.Enabled = res["enabled"].(bool)
			}

			if raw, has := o.Components[MoveToComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, MoveToComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasMoveTo = true
				b.moveTo = motion.NewMoveTo(
					res["maxSpeed"].(float64),
					res["acceleration"].(float64),
					res["stopOnSolids"].(bool),
				)
				b.moveTo.TargetX = res["targetX"].(float64)
				b.moveTo.TargetY = res["targetY"].(float64)
				b.moveTo.Active = res["active"].(bool)
				b.moveTo.Enabled = res["enabled"].(bool)
			}

			if raw, has := o.Components[WrapComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, WrapComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasWrap = true
				b.wrapMode = res["mode"].(string)
				b.wrap = motion.NewWrap(res["margin"].(float64))
				b.wrap.Enabled = res["enabled"].(bool)
			}

			if raw, has := o.Components[BoundComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, BoundComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasBound = true
				b.bound = motion.NewBound(res["mode"].(string) == "edge")
				b.bound.Enabled = res["enabled"].(bool)
			}

			if raw, has := o.Components[DestroyOutsideComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, DestroyOutsideComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasDestroyOutside = true
				b.destroyOutsideMode = res["mode"].(string)
				b.destroyOutside = motion.NewDestroyOutside(res["margin"].(float64))
				b.destroyOutside.Enabled = res["enabled"].(bool)
			}

			if raw, has := o.Components[FadeComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, FadeComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasFade = true
				b.fade = motion.NewFade(
					res["fadeIn"].(float64),
					res["wait"].(float64),
					res["fadeOut"].(float64),
					res["destroy"].(bool),
					res["autostart"].(bool),
				)
				b.fade.BaseAlpha = float64(o.EffectiveAlpha())
				b.fade.Enabled = res["enabled"].(bool)
			}

			if raw, has := o.Components[FlashComponentName]; has {
				res, err := decodeBehaviorComponent(o.ID, FlashComponentName, raw)
				if err != nil {
					walkErr = err
					return false
				}
				b := ensure()
				b.hasFlash = true
				b.flash = motion.NewFlash(res["on"].(float64), res["off"].(float64))
				b.flash.Enabled = res["enabled"].(bool)
				b.flashBaseAlpha = o.EffectiveAlpha()
				if dur := res["duration"].(float64); dur > 0 {
					b.flash.Trigger(dur)
				}
			}

			if ob != nil {
				list = append(list, ob)
				if byID == nil {
					byID = make(map[string]*objectBehaviors)
				}
				byID[o.ID] = ob
			}
			return true
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	}
	return list, byID, nil
}

// Step advances all built-in 2D motion behavior components on s by dt on the fixed loop
// (deterministic, no wall clock). It updates each object's transform and opacity, syncs
// sprites/emitters/skeletons/children, emits behavior events through Signals (or ScreenSignals),
// and frees objects whose destroy_outside or fade (destroy: true) completes.
func (s *BuiltScene) Step(dt time.Duration) {
	s.StepWithViews(dt)
}

// StepWithViews is Step with extra camera views used by behaviors whose mode is "view"
// (wrap and destroy_outside), matching BuiltScene.UpdateWithViews.
func (s *BuiltScene) StepWithViews(dt time.Duration, views ...*tilemap.Camera) {
	if s == nil || dt <= 0 || len(s.behaviors) == 0 {
		return
	}

	sceneBounds := s.sceneBounds()
	var viewBounds motion.Bounds
	viewBoundsComputed := false
	getBounds := func(mode string) motion.Bounds {
		if mode == "view" {
			if !viewBoundsComputed {
				viewBounds = s.viewBounds(views)
				viewBoundsComputed = true
			}
			if viewBounds.Valid() {
				return viewBounds
			}
		}
		return sceneBounds
	}

	for _, ob := range s.behaviors {
		bo := ob.bo
		if bo == nil || bo.freed || bo.Doc == nil {
			continue
		}
		od := bo.Doc
		changed := false
		var solidCast motion.SolidCastFunc
		getSolidCast := func() motion.SolidCastFunc {
			if solidCast == nil {
				solidCast = s.solidCastFor(bo)
			}
			return solidCast
		}

		// 1. Bullet
		if ob.hasBullet && ob.bullet.Enabled {
			var cast motion.SolidCastFunc
			if ob.bullet.BounceOffSolids {
				cast = getSolidCast()
			}
			res := ob.bullet.Step(float64(od.X), float64(od.Y), dt, cast)
			od.X = float32(res.X)
			od.Y = float32(res.Y)
			if res.SetRot {
				od.Rotation = float32(res.Rotation)
			}
			changed = true
			if res.Bounced {
				s.emitBehaviorEvent(bo.ID, BulletComponentName+".bounced", BulletBouncedEvent{
					ObjectID: bo.ID,
					X:        od.X,
					Y:        od.Y,
					NormalX:  float32(res.HitNormal[0]),
					NormalY:  float32(res.HitNormal[1]),
					Angle:    ob.bullet.Angle,
				})
				if bo.freed {
					continue
				}
			}
		}

		// 2. MoveTo
		if ob.hasMoveTo && ob.moveTo.Enabled && ob.moveTo.Active {
			var cast motion.SolidCastFunc
			if ob.moveTo.StopOnSolids {
				cast = getSolidCast()
			}
			res := ob.moveTo.Step(float64(od.X), float64(od.Y), dt, cast)
			od.X = float32(res.X)
			od.Y = float32(res.Y)
			changed = true
			if res.Arrived {
				s.emitBehaviorEvent(bo.ID, MoveToComponentName+".arrived", MoveToArrivedEvent{
					ObjectID: bo.ID,
					X:        od.X,
					Y:        od.Y,
				})
				if bo.freed {
					continue
				}
			}
		}

		// 3. Orbit
		if ob.hasOrbit && ob.orbit.Enabled {
			cx, cy := ob.orbitCenterX, ob.orbitCenterY
			if ob.orbitTarget != "" {
				if tgt := s.Objects[ob.orbitTarget]; tgt != nil && !tgt.freed && tgt.Doc != nil {
					cx, cy = float64(tgt.Doc.X), float64(tgt.Doc.Y)
				}
			}
			res := ob.orbit.Step(cx, cy, dt)
			od.X = float32(res.X)
			od.Y = float32(res.Y)
			if res.SetRot {
				od.Rotation = float32(res.Rotation)
			}
			changed = true
		}

		// 4. Rotate
		if ob.hasRotate && ob.rotate.Enabled {
			dRad := ob.rotate.Step(dt)
			if dRad != 0 {
				od.Rotation += float32(dRad)
				changed = true
			}
		}

		// 5. Sine (additive delta)
		if ob.hasSine && ob.sine.Enabled {
			delta := ob.sine.Step(dt)
			if delta != 0 {
				switch ob.sineProp {
				case "x":
					od.X += float32(delta)
				case "y":
					od.Y += float32(delta)
				case "angle":
					od.Rotation += float32(delta * math.Pi / 180.0)
				case "scale":
					sx := od.EffectiveScaleX() + float32(delta)
					sy := od.EffectiveScaleY() + float32(delta)
					od.ScaleX = Float32(sx)
					od.ScaleY = Float32(sy)
				case "alpha":
					ob.sineUnclampAlpha += delta
					a := float32(ob.sineUnclampAlpha)
					if a < 0 {
						a = 0
					} else if a > 1 {
						a = 1
					}
					od.Alpha = Float32(a)
				}
				changed = true
			}
		}

		// 6. Wrap
		if ob.hasWrap && ob.wrap.Enabled {
			nx, ny, wrapped := ob.wrap.Apply(float64(od.X), float64(od.Y), getBounds(ob.wrapMode))
			if wrapped {
				od.X = float32(nx)
				od.Y = float32(ny)
				changed = true
				s.emitBehaviorEvent(bo.ID, WrapComponentName+".wrapped", WrapEvent{
					ObjectID: bo.ID,
					X:        od.X,
					Y:        od.Y,
				})
				if bo.freed {
					continue
				}
			}
		}

		// 7. Bound
		if ob.hasBound && ob.bound.Enabled {
			left, top, right, bottom := s.objectExtents(bo)
			nx, ny := ob.bound.Apply(float64(od.X), float64(od.Y), sceneBounds, left, top, right, bottom)
			if float32(nx) != od.X || float32(ny) != od.Y {
				od.X = float32(nx)
				od.Y = float32(ny)
				changed = true
			}
		}

		// 8. DestroyOutside
		if ob.hasDestroyOutside && ob.destroyOutside.Enabled {
			if ob.destroyOutside.Outside(float64(od.X), float64(od.Y), getBounds(ob.destroyOutsideMode)) {
				s.emitBehaviorEvent(bo.ID, DestroyOutsideComponentName+".destroyed", DestroyOutsideEvent{
					ObjectID: bo.ID,
					X:        od.X,
					Y:        od.Y,
				})
				s.Free(bo.ID)
				continue
			}
		}

		// 9. Fade
		if ob.hasFade && ob.fade.Enabled && ob.fade.Running {
			res := ob.fade.Step(dt)
			a := float32(res.Alpha)
			od.Alpha = Float32(a)
			if ob.hasFlash && !ob.flashHidden {
				ob.flashBaseAlpha = a
			}
			changed = true
			if res.Finished {
				s.emitBehaviorEvent(bo.ID, FadeComponentName+".finished", FadeFinishedEvent{
					ObjectID:  bo.ID,
					Destroyed: res.ShouldDestroy,
				})
				if res.ShouldDestroy {
					s.Free(bo.ID)
					continue
				}
			}
		}

		// 10. Flash
		if ob.hasFlash && ob.flash.Enabled && ob.flash.Active() {
			if !ob.flashHidden {
				ob.flashBaseAlpha = od.EffectiveAlpha()
			}
			res := ob.flash.Step(dt)
			if res.Visible {
				if ob.flashHidden {
					ob.flashHidden = false
					od.Alpha = Float32(ob.flashBaseAlpha)
					changed = true
				}
			} else {
				if !ob.flashHidden {
					ob.flashHidden = true
					od.Alpha = Float32(0)
					changed = true
				}
			}
			if res.Finished {
				s.emitBehaviorEvent(bo.ID, FlashComponentName+".finished", FlashFinishedEvent{
					ObjectID: bo.ID,
				})
				if bo.freed {
					continue
				}
			}
		}

		if changed && !bo.freed {
			bo.Sync()
			if s.Physics != nil {
				s.Physics.syncMovedObject2D(bo)
			}
		}
	}
}

func (s *BuiltScene) emitBehaviorEvent(objID, event string, payload any) {
	sig := s.Signals
	if sig == nil {
		sig = s.ScreenSignals
	}
	if sig == nil {
		return
	}
	_ = sig.Emit(objID, event, payload)
}

func (s *BuiltScene) sceneBounds() motion.Bounds {
	if s == nil {
		return motion.Bounds{}
	}
	if s.Doc != nil && s.Doc.VirtualWidth > 0 && s.Doc.VirtualHeight > 0 {
		return motion.Bounds{
			MinX: 0,
			MinY: 0,
			MaxX: float64(s.Doc.VirtualWidth),
			MaxY: float64(s.Doc.VirtualHeight),
		}
	}
	if s.Camera != nil && s.Camera.Bounds != nil {
		b := s.Camera.Bounds
		return motion.Bounds{
			MinX: float64(b.X),
			MinY: float64(b.Y),
			MaxX: float64(b.X + b.Width),
			MaxY: float64(b.Y + b.Height),
		}
	}
	return motion.Bounds{}
}

func (s *BuiltScene) viewBounds(extra []*tilemap.Camera) motion.Bounds {
	vs := s.notifierViews(extra)
	if len(vs) == 0 {
		return motion.Bounds{}
	}
	var out motion.Bounds
	first := true
	for _, cam := range vs {
		if cam == nil {
			continue
		}
		vb := cam.VisibleBounds()
		b := motion.Bounds{
			MinX: float64(vb.Origin.X),
			MinY: float64(vb.Origin.Y),
			MaxX: float64(vb.Origin.X + vb.Size.Width),
			MaxY: float64(vb.Origin.Y + vb.Size.Height),
		}
		if !b.Valid() {
			continue
		}
		if first {
			out = b
			first = false
		} else {
			out.MinX = min(out.MinX, b.MinX)
			out.MinY = min(out.MinY, b.MinY)
			out.MaxX = max(out.MaxX, b.MaxX)
			out.MaxY = max(out.MaxY, b.MaxY)
		}
	}
	if first {
		return motion.Bounds{}
	}
	return out
}

func (s *BuiltScene) objectExtents(bo *BuiltObject) (left, top, right, bottom float64) {
	if bo == nil || bo.Doc == nil {
		return 0, 0, 0, 0
	}
	if bo.Sprite != nil && bo.Sprite.Frame.Width > 0 && bo.Sprite.Frame.Height > 0 {
		sx := math.Abs(float64(bo.Doc.EffectiveScaleX()))
		sy := math.Abs(float64(bo.Doc.EffectiveScaleY()))
		w := float64(bo.Sprite.Frame.Width) * sx
		h := float64(bo.Sprite.Frame.Height) * sy
		ox := float64(bo.Doc.OriginX) * sx
		oy := float64(bo.Doc.OriginY) * sy
		return max(0, ox), max(0, oy), max(0, w-ox), max(0, h-oy)
	}
	if s.Physics != nil {
		if body := s.Physics.Bodies2D[bo.ID]; body != nil && !body.Shape.Empty() {
			b := body.Bounds()
			ox, oy := float64(bo.Doc.X), float64(bo.Doc.Y)
			return max(0, ox-float64(b.X)), max(0, oy-float64(b.Y)),
				max(0, float64(b.X+b.W)-ox), max(0, float64(b.Y+b.H)-oy)
		}
	}
	return 0, 0, 0, 0
}

// ensureTileSolidViews lazily constructs cached tileshape.Views for layers with a BuiltLayer.Map
// so motion solid queries see solid tile flags without rebuilding per step.
func (s *BuiltScene) ensureTileSolidViews() []*tileshape.View {
	if s.tileSolidsReady {
		return s.tileSolids
	}
	s.tileSolidsReady = true
	for _, l := range s.Layers {
		if l.Map != nil {
			s.tileSolids = append(s.tileSolids, tileshape.New(l.Map, tileshape.Options{ShapedSolid: true}))
		}
	}
	return s.tileSolids
}

// solidCastFor returns a SolidCastFunc testing against s.Physics.World2D (bodies + tiles)
// and any solid tilemap flags on the scene when s.Physics was built by BuildPhysics.
func (s *BuiltScene) solidCastFor(bo *BuiltObject) motion.SolidCastFunc {
	if s == nil || s.Physics == nil || s.Physics.World2D == nil {
		return nil
	}
	w2 := s.Physics.World2D
	selfBody := s.Physics.Bodies2D[bo.ID]
	var tileViews []*tileshape.View
	if w2.Tiles == nil {
		tileViews = s.ensureTileSolidViews()
	}

	var selfShape physics.Shape
	var shapeOffX, shapeOffY float64
	hasShape := false
	if selfBody != nil && !selfBody.Shape.Empty() {
		selfShape = selfBody.Shape
		b := selfShape.Bounds()
		shapeOffX = float64(b.X+b.W*0.5) - float64(bo.Doc.X)
		shapeOffY = float64(b.Y+b.H*0.5) - float64(bo.Doc.Y)
		hasShape = true
	}

	return func(x, y, dx, dy float64) (motion.SolidHit, bool) {
		var best motion.SolidHit
		hasBest := false

		consider := func(frac, nx, ny float64) {
			if !hasBest || frac < best.Fraction {
				best = motion.SolidHit{Fraction: frac, NormalX: nx, NormalY: ny}
				hasBest = true
			}
		}

		if hasShape {
			// Position shape centered at (x + shapeOffX, y + shapeOffY).
			cur := selfShape
			cb := cur.Bounds()
			cx := float64(cb.X + cb.W*0.5)
			cy := float64(cb.Y + cb.H*0.5)
			cur = cur.Translated(float32((x+shapeOffX)-cx), float32((y+shapeOffY)-cy))

			opts := physics.ShapeCastOptions{
				QueryMask:  physics.QueryBodies | physics.QueryTiles,
				IgnoreBody: selfBody,
			}
			if h, ok := w2.ShapeCastWith(cur, float32(dx), float32(dy), opts); ok {
				consider(float64(h.Fraction), float64(h.NormalX), float64(h.NormalY))
			}
			for _, tv := range tileViews {
				tw := physics.World{Tiles: tv}
				if h, ok := tw.ShapeCast(cur, float32(dx), float32(dy), physics.QueryTiles); ok {
					consider(float64(h.Fraction), float64(h.NormalX), float64(h.NormalY))
				}
			}
			return best, hasBest
		}

		from := core.Point{X: core.Points(x), Y: core.Points(y)}
		to := core.Point{X: core.Points(x + dx), Y: core.Points(y + dy)}
		for _, h := range w2.Raycast(from, to, physics.QueryBodies|physics.QueryTiles) {
			if h.Body != nil && h.Body == selfBody {
				continue
			}
			consider(float64(h.T), float64(h.NormalX), float64(h.NormalY))
			break
		}
		for _, tv := range tileViews {
			tw := physics.World{Tiles: tv}
			for _, h := range tw.Raycast(from, to, physics.QueryTiles) {
				consider(float64(h.T), float64(h.NormalX), float64(h.NormalY))
				break
			}
		}
		return best, hasBest
	}
}

// Freed reports whether bo has been removed from its BuiltScene via BuiltScene.Free.
func (bo *BuiltObject) Freed() bool {
	return bo == nil || bo.freed
}

// Free removes the built object with ID id and all of its descendants from s so they are no longer
// drawn, updated, stepped, or queried in physics. When s.Signals (or s.ScreenSignals) was created
// with SpawnConnected, any live ECS entities for those objects are destroyed as well. Free returns
// false when s is nil or has no live object with ID id.
func (s *BuiltScene) Free(id string) bool {
	if s == nil || id == "" {
		return false
	}
	bo := s.Objects[id]
	if bo == nil || bo.freed {
		return false
	}

	// Free descendants first so bottom-up cleanup is deterministic.
	// Iterate over a copy: a child Free cannot find (already freed, or missing from Objects) would otherwise never leave bo.Children and loop forever.
	children := append([]*BuiltObject(nil), bo.Children...)
	for i := len(children) - 1; i >= 0; i-- {
		s.Free(children[i].ID)
	}
	bo.Children = nil

	bo.freed = true
	delete(s.Objects, id)

	if bo.Parent != nil {
		bo.Parent.Children = removeBuiltObject(bo.Parent.Children, bo)
		bo.Parent = nil
	} else if layer := s.Layer(bo.Layer); layer != nil {
		layer.Objects = removeBuiltObject(layer.Objects, bo)
	}

	if layer := s.Layer(bo.Layer); layer != nil {
		if bo.Sprite != nil {
			for _, batch := range layer.Batches {
				batch.Sprites = removeSprite(batch.Sprites, bo.Sprite)
			}
		}
		if bo.Emitter != nil {
			layer.Emitters = removeBuiltObject(layer.Emitters, bo)
		}
		if bo.Skeleton != nil {
			layer.Skeletons = removeBuiltObject(layer.Skeletons, bo)
		}
		layer.drawItems = removeDrawItem(layer.drawItems, bo)
	}

	s.materialed = removeBuiltObject(s.materialed, bo)

	if n := s.notifierByObject[bo]; n != nil {
		delete(s.notifierByObject, bo)
		s.Notifiers = removeNotifier(s.Notifiers, n)
	}

	delete(s.behaviorByID, id)

	if s.Physics != nil {
		s.Physics.removeObject2D(id)
	}

	sig := s.Signals
	if sig == nil {
		sig = s.ScreenSignals
	}
	if sig != nil && sig.world != nil {
		if e, ok := sig.Entity(id); ok && sig.world.Alive(e) {
			sig.world.Destroy(e)
		}
	}

	return true
}

func removeBuiltObject(slice []*BuiltObject, target *BuiltObject) []*BuiltObject {
	for i, item := range slice {
		if item == target {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func removeSprite[T comparable](slice []T, target T) []T {
	for i, item := range slice {
		if item == target {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func removeDrawItem(items []layerDrawItem, target *BuiltObject) []layerDrawItem {
	out := items[:0]
	for _, it := range items {
		if it.emitter == target || it.skel == target {
			continue
		}
		out = append(out, it)
	}
	return out
}

func removeNotifier(slice []*ScreenNotifier, target *ScreenNotifier) []*ScreenNotifier {
	for i, n := range slice {
		if n == target {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// Connection handler helpers.

func resolveBehaviorScene(c Call) (*BuiltScene, *objectBehaviors) {
	if c.Target == nil {
		return nil, nil
	}
	var bs *BuiltScene
	if p, ok := c.Context.(BehaviorSceneProvider); ok && p != nil {
		bs = p.BehaviorScene()
	}
	if bs == nil || bs.behaviorByID == nil {
		return nil, nil
	}
	ob := bs.behaviorByID[c.Target.ID]
	if ob == nil || ob.bo == nil || ob.bo.freed {
		return bs, nil
	}
	return bs, ob
}

func handleBehaviorStart(compName string) HandlerFunc {
	return func(c Call) {
		_, ob := resolveBehaviorScene(c)
		if ob == nil {
			return
		}
		ob.setEnabled(compName, true)
	}
}

func handleBehaviorStop(compName string) HandlerFunc {
	return func(c Call) {
		_, ob := resolveBehaviorScene(c)
		if ob == nil {
			return
		}
		ob.setEnabled(compName, false)
	}
}

func (ob *objectBehaviors) setEnabled(compName string, enabled bool) {
	switch compName {
	case BulletComponentName:
		if ob.hasBullet {
			ob.bullet.Enabled = enabled
		}
	case SineComponentName:
		if ob.hasSine {
			ob.sine.Enabled = enabled
		}
	case RotateComponentName:
		if ob.hasRotate {
			ob.rotate.Enabled = enabled
		}
	case OrbitComponentName:
		if ob.hasOrbit {
			ob.orbit.Enabled = enabled
		}
	case MoveToComponentName:
		if ob.hasMoveTo {
			ob.moveTo.Enabled = enabled
		}
	case WrapComponentName:
		if ob.hasWrap {
			ob.wrap.Enabled = enabled
		}
	case BoundComponentName:
		if ob.hasBound {
			ob.bound.Enabled = enabled
		}
	case DestroyOutsideComponentName:
		if ob.hasDestroyOutside {
			ob.destroyOutside.Enabled = enabled
		}
	case FadeComponentName:
		if ob.hasFade {
			ob.fade.Enabled = enabled
		}
	case FlashComponentName:
		if ob.hasFlash {
			ob.flash.Enabled = enabled
		}
	}
}

func handleBulletSetSpeed(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasBullet {
		return
	}
	if speed, ok := c.Args["speed"].(float64); ok {
		ob.bullet.SetSpeed(speed)
	}
}

func handleBulletSetAngle(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasBullet {
		return
	}
	if angle, ok := c.Args["angle"].(float64); ok {
		ob.bullet.SetHeading(angle)
	}
}

func handleRotateSetSpeed(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasRotate {
		return
	}
	if speed, ok := c.Args["speed"].(float64); ok {
		ob.rotate.Speed = speed
	}
}

func handleOrbitSetSpeed(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasOrbit {
		return
	}
	if speed, ok := c.Args["speed"].(float64); ok {
		ob.orbit.Speed = speed
	}
}

func handleMoveToTarget(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasMoveTo {
		return
	}
	tx, _ := c.Args["x"].(float64)
	ty, _ := c.Args["y"].(float64)
	ob.moveTo.SetTarget(tx, ty)
}

func handleMoveToSetSpeed(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasMoveTo {
		return
	}
	if speed, ok := c.Args["speed"].(float64); ok && speed >= 0 {
		ob.moveTo.MaxSpeed = speed
	}
}

func handleMoveToStop(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasMoveTo {
		return
	}
	ob.moveTo.Stop()
}

func handleFadeStart(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasFade {
		return
	}
	ob.fade.Enabled = true
	ob.fade.Start()
}

func handleFadeStop(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasFade {
		return
	}
	ob.fade.Stop()
}

func handleFlashTrigger(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasFlash {
		return
	}
	dur, _ := c.Args["duration"].(float64)
	if !ob.flashHidden && ob.bo != nil && ob.bo.Doc != nil {
		ob.flashBaseAlpha = ob.bo.Doc.EffectiveAlpha()
	}
	ob.flash.Enabled = true
	ob.flash.Trigger(dur)
}

func handleFlashStop(c Call) {
	_, ob := resolveBehaviorScene(c)
	if ob == nil || !ob.hasFlash {
		return
	}
	ob.flash.Stop()
	if ob.flashHidden && ob.bo != nil && ob.bo.Doc != nil {
		ob.flashHidden = false
		ob.bo.Doc.Alpha = Float32(ob.flashBaseAlpha)
		ob.bo.Sync()
	}
}

// Programmatic runtime control helpers on BuiltScene so Go code can also pause, resume,
// retarget, or trigger built-in motion behaviors without wiring a connection.

// SetBehaviorEnabled enables or disables the named motion behavior component on objectID.
// It returns false when s has no live objectID with that behavior.
func (s *BuiltScene) SetBehaviorEnabled(objectID, component string, enabled bool) bool {
	if s == nil || s.behaviorByID == nil {
		return false
	}
	ob := s.behaviorByID[objectID]
	if ob == nil || ob.bo == nil || ob.bo.freed || !IsBehaviorComponent(component) {
		return false
	}
	ob.setEnabled(component, enabled)
	return true
}

// MoveTo commands objectID's built-in "move_to" behavior to move toward (x, y).
func (s *BuiltScene) MoveTo(objectID string, x, y float64) bool {
	if s == nil || s.behaviorByID == nil {
		return false
	}
	ob := s.behaviorByID[objectID]
	if ob == nil || ob.bo == nil || ob.bo.freed || !ob.hasMoveTo {
		return false
	}
	ob.moveTo.SetTarget(x, y)
	return true
}

// TriggerFlash commands objectID's built-in "flash" behavior to blink for duration seconds.
func (s *BuiltScene) TriggerFlash(objectID string, duration float64) bool {
	if s == nil || s.behaviorByID == nil {
		return false
	}
	ob := s.behaviorByID[objectID]
	if ob == nil || ob.bo == nil || ob.bo.freed || !ob.hasFlash {
		return false
	}
	if !ob.flashHidden && ob.bo.Doc != nil {
		ob.flashBaseAlpha = ob.bo.Doc.EffectiveAlpha()
	}
	ob.flash.Enabled = true
	ob.flash.Trigger(duration)
	return true
}

// SetBulletSpeed updates objectID's built-in "bullet" travel speed in points per second.
func (s *BuiltScene) SetBulletSpeed(objectID string, speed float64) bool {
	if s == nil || s.behaviorByID == nil {
		return false
	}
	ob := s.behaviorByID[objectID]
	if ob == nil || ob.bo == nil || ob.bo.freed || !ob.hasBullet {
		return false
	}
	ob.bullet.SetSpeed(speed)
	return true
}
