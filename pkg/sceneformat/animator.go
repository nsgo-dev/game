package sceneformat

import (
	"errors"
	"fmt"
)

const (
	// AnimatorComponentName is the built-in sprite animation state machine component type name in scene and prefab YAML files.
	AnimatorComponentName = "animator"

	// AnimatorEventStateChanged is the event emitted when an animator transitions to a new state.
	AnimatorEventStateChanged = "stateChanged"
	// AnimatorEventClipEvent is the event emitted when an animator's active clip crosses a named cue event.
	AnimatorEventClipEvent = "clipEvent"

	// AnimatorHandlerSetState is the handler that switches an animator directly to a named state.
	AnimatorHandlerSetState = "setState"
	// AnimatorHandlerTrigger is the handler that pulses a named trigger parameter on an animator.
	AnimatorHandlerTrigger = "trigger"
)

// AnimatorStateChangedEvent is the payload emitted with an object's "animator.stateChanged" event.
type AnimatorStateChangedEvent struct {
	// ObjectID is the ID of the object whose animator transitioned.
	ObjectID string
	// From is the state left.
	From string
	// To is the state entered.
	To string
}

// AnimatorClipEvent is the payload emitted with an object's "animator.clipEvent" event.
type AnimatorClipEvent struct {
	// ObjectID is the ID of the object whose active clip crossed the cue.
	ObjectID string
	// State is the name of the active state whose clip emitted the cue.
	State string
	// Name is the cue event name declared in the clip.
	Name string
}

// AnimatorController is the runtime state machine interface driven by the built-in "animator" component's setState and trigger handlers.
// *animclip.StateMachine implements it.
type AnimatorController interface {
	SetState(name string) bool
	Trigger(name string)
}

// AnimatorProvider may be implemented by Call.Context (as *BuiltScene does) to supply the AnimatorController for an object ID when none was bound directly on Signals.
type AnimatorProvider interface {
	Animator(objectID string) AnimatorController
}

// AnimatorComponent returns the ComponentType schema for the built-in "animator" component.
// An object with an animator component names a *.states.yaml file and an optional starting state or clip.
func AnimatorComponent() ComponentType {
	return ComponentType{
		Name: AnimatorComponentName,
		Doc:  "2D sprite animation state machine driven by a *.states.yaml graph and named parameters.",
		Fields: []Field{
			{Name: "states", Kind: FieldString, Default: "", Doc: "Project-relative path to a *.states.yaml sprite state machine file."},
			{Name: "startClip", Kind: FieldString, Default: "", Doc: "Optional starting state or clip name override; empty starts at the state machine's entry state."},
			{Name: "autoPlay", Kind: FieldBool, Default: true, Doc: "When true, playback starts automatically on the fixed loop."},
			{Name: "speed", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.1, Doc: "Playback speed multiplier applied to every state (>= 0)."},
		},
		Events: []Event{
			{Name: AnimatorEventStateChanged, Doc: "Emitted when the animator transitions to a new state."},
			{Name: AnimatorEventClipEvent, Doc: "Emitted when the active clip crosses a named cue event."},
		},
		Handlers: []Handler{
			{
				Name: AnimatorHandlerSetState,
				Doc:  "Switches directly to a named state.",
				Params: []Field{
					{Name: "state", Kind: FieldString, Default: "", Doc: "Name of the target state."},
				},
				Func: handleAnimatorSetState,
			},
			{
				Name: AnimatorHandlerTrigger,
				Doc:  "Pulses a named trigger parameter.",
				Params: []Field{
					{Name: "name", Kind: FieldString, Default: "", Doc: "Name of the trigger parameter."},
				},
				Func: handleAnimatorTrigger,
			},
		},
	}
}

func handleAnimatorSetState(c Call) {
	ctrl := resolveAnimatorController(c)
	if ctrl == nil {
		return
	}
	state, _ := c.Args["state"].(string)
	if state == "" {
		return
	}
	ctrl.SetState(state)
}

func handleAnimatorTrigger(c Call) {
	ctrl := resolveAnimatorController(c)
	if ctrl == nil {
		return
	}
	name, _ := c.Args["name"].(string)
	if name == "" {
		return
	}
	ctrl.Trigger(name)
}

func resolveAnimatorController(c Call) AnimatorController {
	if c.Target == nil {
		return nil
	}
	id := c.Target.ID
	if c.Signals != nil {
		if ctrl := c.Signals.Animator(id); ctrl != nil {
			return ctrl
		}
	}
	if p, ok := c.Context.(AnimatorProvider); ok && p != nil {
		if ctrl := p.Animator(id); ctrl != nil {
			return ctrl
		}
	}
	return nil
}

// RegisterAnimatorComponent registers the built-in "animator" component type on reg.
func RegisterAnimatorComponent(reg *ComponentRegistry) error {
	return reg.Register(AnimatorComponent())
}

// AnimatorComponentRegistry returns a new ComponentRegistry containing the built-in "animator" component.
func AnimatorComponentRegistry() *ComponentRegistry {
	reg := NewComponentRegistry()
	if err := RegisterAnimatorComponent(reg); err != nil {
		panic(err)
	}
	return reg
}

var builtinAnimatorReg = AnimatorComponentRegistry()

// AnimatorConfig holds the resolved fields of an object's built-in "animator" component.
type AnimatorConfig struct {
	// States is the project-relative path to the *.states.yaml file.
	States string
	// StartClip is the optional starting state or clip override.
	StartClip string
	// AutoPlay reports whether playback starts automatically.
	AutoPlay bool
	// Speed is the playback speed multiplier (>= 0).
	Speed float64
}

// ResolveAnimatorComponent resolves the built-in "animator" component on obj, returning (cfg, true, nil) when present, (zero, false, nil) when absent, or an error when its fields are invalid.
func ResolveAnimatorComponent(obj *ObjectDoc) (AnimatorConfig, bool, error) {
	if obj == nil || obj.Components == nil {
		return AnimatorConfig{}, false, nil
	}
	raw, ok := obj.Components[AnimatorComponentName]
	if !ok {
		return AnimatorConfig{}, false, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return AnimatorConfig{}, true, &ComponentError{
			Object:    obj.ID,
			Component: AnimatorComponentName,
			Msg:       fmt.Sprintf("must be a mapping of fields, got %s", describeValue(raw)),
		}
	}
	resolved, err := builtinAnimatorReg.Resolve(AnimatorComponentName, m)
	if err != nil {
		var ce *ComponentError
		if errors.As(err, &ce) {
			ce.Object = obj.ID
		}
		return AnimatorConfig{}, true, err
	}
	return AnimatorConfig{
		States:    resolved["states"].(string),
		StartClip: resolved["startClip"].(string),
		AutoPlay:  resolved["autoPlay"].(bool),
		Speed:     resolved["speed"].(float64),
	}, true, nil
}
