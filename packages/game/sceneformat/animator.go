package sceneformat

import (
	"errors"
	"fmt"
)

// AnimatorComponentName is the built-in sprite animation state machine component type name in scene and prefab YAML files.
const AnimatorComponentName = "animator"

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
			{Name: "stateChanged", Doc: "Emitted when the animator transitions to a new state."},
			{Name: "clipEvent", Doc: "Emitted when the active clip crosses a named cue event."},
		},
		Handlers: []Handler{
			{
				Name: "setState",
				Doc:  "Switches directly to a named state.",
				Params: []Field{
					{Name: "state", Kind: FieldString, Default: "", Doc: "Name of the target state."},
				},
			},
			{
				Name: "trigger",
				Doc:  "Pulses a named trigger parameter.",
				Params: []Field{
					{Name: "name", Kind: FieldString, Default: "", Doc: "Name of the trigger parameter."},
				},
			},
		},
	}
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
