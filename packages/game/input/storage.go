package input

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

const storageVersion = 1

type savedBindings struct {
	Version     int                         `json:"version"`
	Actions     map[string][]savedInput     `json:"actions,omitempty"`
	Axes        map[string]savedAxis        `json:"axes,omitempty"`
	MotionAxes  map[string]savedMotionAxis  `json:"motion_axes,omitempty"`
	PointerAxes map[string]savedPointerAxis `json:"pointer_axes,omitempty"`
	GamepadAxes map[string]savedGamepadAxis `json:"gamepad_axes,omitempty"`
}

type savedPointerAxis struct {
	Axis        string  `json:"axis"`
	Sensitivity float64 `json:"sensitivity,omitempty"`
	Invert      bool    `json:"invert,omitempty"`
}

type savedGamepadAxis struct {
	Axis        string  `json:"axis"`
	DeadZone    float64 `json:"dead_zone,omitempty"`
	Axial       bool    `json:"axial,omitempty"`
	Sensitivity float64 `json:"sensitivity,omitempty"`
	Invert      bool    `json:"invert,omitempty"`
}

type savedMotionAxis struct {
	Axis        string  `json:"axis"`
	DeadZone    float64 `json:"dead_zone,omitempty"`
	Sensitivity float64 `json:"sensitivity,omitempty"`
	MaxRange    float64 `json:"max_range,omitempty"`
	Smoothing   float64 `json:"smoothing,omitempty"`
	Invert      bool    `json:"invert,omitempty"`
}

type savedInput struct {
	Kind   string       `json:"kind"`
	Bounds *savedBounds `json:"bounds,omitempty"`
	Button string       `json:"button,omitempty"`
	Key    string       `json:"key,omitempty"`
	Code   string       `json:"code,omitempty"`
}

type savedBounds struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	W float32 `json:"w"`
	H float32 `json:"h"`
}

type savedAxis struct {
	Negative string `json:"negative,omitempty"`
	Positive string `json:"positive,omitempty"`
}

// Save writes all of a's action, axis, motion axis, pointer axis and game controller stick axis bindings as JSON into store under key. It fails with *core.StorageFullError when the storage is full.
func (a *Actions) Save(store core.Storage, key string) error {
	doc := savedBindings{Version: storageVersion}
	if len(a.bindings) > 0 {
		doc.Actions = make(map[string][]savedInput, len(a.bindings))
		for name, list := range a.bindings {
			out := make([]savedInput, len(list))
			for i, in := range list {
				enc, err := encodeInput(in)
				if err != nil {
					return fmt.Errorf("input: save %q: action %q: %w", key, name, err)
				}
				out[i] = enc
			}
			doc.Actions[name] = out
		}
	}
	if len(a.axes) > 0 {
		doc.Axes = make(map[string]savedAxis, len(a.axes))
		for name, ax := range a.axes {
			doc.Axes[name] = savedAxis{Negative: ax.negative, Positive: ax.positive}
		}
	}
	if len(a.motionAxes) > 0 {
		doc.MotionAxes = make(map[string]savedMotionAxis, len(a.motionAxes))
		for name, mb := range a.motionAxes {
			axName, err := encodeMotionAxis(mb.axis)
			if err != nil {
				return fmt.Errorf("input: save %q: motion axis %q: %w", key, name, err)
			}
			doc.MotionAxes[name] = savedMotionAxis{
				Axis:        axName,
				DeadZone:    mb.opts.DeadZone,
				Sensitivity: mb.opts.Sensitivity,
				MaxRange:    mb.opts.MaxRange,
				Smoothing:   mb.opts.Smoothing,
				Invert:      mb.opts.Invert,
			}
		}
	}
	if len(a.pointerAxes) > 0 {
		doc.PointerAxes = make(map[string]savedPointerAxis, len(a.pointerAxes))
		for name, pb := range a.pointerAxes {
			axName, err := encodePointerAxis(pb.axis)
			if err != nil {
				return fmt.Errorf("input: save %q: pointer axis %q: %w", key, name, err)
			}
			doc.PointerAxes[name] = savedPointerAxis{Axis: axName, Sensitivity: pb.opts.Sensitivity, Invert: pb.opts.Invert}
		}
	}
	if len(a.gpAxes) > 0 {
		doc.GamepadAxes = make(map[string]savedGamepadAxis, len(a.gpAxes))
		for name, gb := range a.gpAxes {
			axName, err := encodeGamepadAxis(gb.axis)
			if err != nil {
				return fmt.Errorf("input: save %q: gamepad axis %q: %w", key, name, err)
			}
			doc.GamepadAxes[name] = savedGamepadAxis{
				Axis:        axName,
				DeadZone:    gb.opts.DeadZone,
				Axial:       gb.opts.Axial,
				Sensitivity: gb.opts.Sensitivity,
				Invert:      gb.opts.Invert,
			}
		}
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("input: save %q: %w", key, err)
	}
	return store.SetItem(key, string(data))
}

// Load replaces a's action and axis bindings with those saved in store under key, and reports whether key was present.
//
// When key is absent, Load leaves a's existing bindings as they are and returns false, nil. When the stored value is invalid JSON, has a field Load does not know or uses an unknown format, Load leaves a's bindings untouched and returns false and an error.
func (a *Actions) Load(store core.Storage, key string) (bool, error) {
	raw, ok := store.GetItem(key)
	if !ok {
		return false, nil
	}
	var doc savedBindings
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return false, fmt.Errorf("input: load %q: %w", key, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return false, fmt.Errorf("input: load %q: trailing data after the bindings", key)
	}
	if doc.Version != storageVersion {
		return false, fmt.Errorf("input: load %q: unsupported version %d", key, doc.Version)
	}
	var bindings map[string][]Input
	if len(doc.Actions) > 0 {
		bindings = make(map[string][]Input, len(doc.Actions))
		for name, list := range doc.Actions {
			if len(list) == 0 {
				continue
			}
			decoded := make([]Input, len(list))
			for i, item := range list {
				in, err := decodeInput(item)
				if err != nil {
					return false, fmt.Errorf("input: load %q: action %q: %w", key, name, err)
				}
				decoded[i] = in
			}
			bindings[name] = decoded
		}
	}
	var axes map[string]axisBinding
	if len(doc.Axes) > 0 {
		axes = make(map[string]axisBinding, len(doc.Axes))
		for name, ax := range doc.Axes {
			if ax.Negative == "" && ax.Positive == "" {
				continue
			}
			axes[name] = axisBinding{negative: ax.Negative, positive: ax.Positive}
		}
	}

	var motionAxes map[string]motionAxisBinding
	if len(doc.MotionAxes) > 0 {
		motionAxes = make(map[string]motionAxisBinding, len(doc.MotionAxes))
		for name, ma := range doc.MotionAxes {
			ax, err := decodeMotionAxis(ma.Axis)
			if err != nil {
				return false, fmt.Errorf("input: load %q: motion axis %q: %w", key, name, err)
			}
			if ax == MotionAxisNone {
				continue
			}
			motionAxes[name] = motionAxisBinding{
				axis: ax,
				opts: MotionAxisOptions{
					DeadZone:    ma.DeadZone,
					Sensitivity: ma.Sensitivity,
					MaxRange:    ma.MaxRange,
					Smoothing:   ma.Smoothing,
					Invert:      ma.Invert,
				},
			}
		}
	}

	var pointerAxes map[string]pointerAxisBinding
	if len(doc.PointerAxes) > 0 {
		pointerAxes = make(map[string]pointerAxisBinding, len(doc.PointerAxes))
		for name, pa := range doc.PointerAxes {
			ax, err := decodePointerAxis(pa.Axis)
			if err != nil {
				return false, fmt.Errorf("input: load %q: pointer axis %q: %w", key, name, err)
			}
			if ax == PointerAxisNone {
				continue
			}
			pointerAxes[name] = pointerAxisBinding{axis: ax, opts: PointerAxisOptions{Sensitivity: pa.Sensitivity, Invert: pa.Invert}}
		}
	}

	var gamepadAxes map[string]gamepadAxisBinding
	if len(doc.GamepadAxes) > 0 {
		gamepadAxes = make(map[string]gamepadAxisBinding, len(doc.GamepadAxes))
		for name, ga := range doc.GamepadAxes {
			ax, err := decodeGamepadAxis(ga.Axis)
			if err != nil {
				return false, fmt.Errorf("input: load %q: gamepad axis %q: %w", key, name, err)
			}
			gamepadAxes[name] = gamepadAxisBinding{
				axis: ax,
				opts: GamepadAxisOptions{
					DeadZone:    ga.DeadZone,
					Axial:       ga.Axial,
					Sensitivity: ga.Sensitivity,
					Invert:      ga.Invert,
				},
			}
		}
	}

	a.bindings = bindings
	a.axes = axes
	a.motionAxes = motionAxes
	a.pointerAxes = pointerAxes
	a.gpAxes = gamepadAxes
	clear(a.gpSnap)
	clear(a.motionSnap)
	clear(a.motionInit)
	clear(a.latched)
	clear(a.rawActive)
	if len(a.bindings) > 0 {
		if a.rawActive == nil {
			a.rawActive = make(map[string]bool, len(a.bindings))
		}
		for name := range a.bindings {
			a.rawActive[name] = a.activeNow(name)
		}
	}
	return true, nil
}

func encodeInput(in Input) (savedInput, error) {
	switch in.Kind {
	case InputNone:
		return savedInput{Kind: "none"}, nil
	case InputTouchRegion:
		return savedInput{Kind: "touch_region", Bounds: encodeBounds(in.Bounds)}, nil
	case InputMouseButton:
		btn, err := encodeMouseButton(in.Button)
		if err != nil {
			return savedInput{}, err
		}
		return savedInput{Kind: "mouse_button", Button: btn}, nil
	case InputMouseRegion:
		btn, err := encodeMouseButton(in.Button)
		if err != nil {
			return savedInput{}, err
		}
		return savedInput{Kind: "mouse_region", Bounds: encodeBounds(in.Bounds), Button: btn}, nil
	case InputKey:
		if in.Key == "" {
			return savedInput{}, fmt.Errorf("key input has no key")
		}
		return savedInput{Kind: "key", Key: in.Key}, nil
	case InputKeyCode:
		if in.Code == "" {
			return savedInput{}, fmt.Errorf("key_code input has no code")
		}
		return savedInput{Kind: "key_code", Code: in.Code}, nil
	case InputGamepadButton:
		btn, err := encodeGamepadButton(in.PadButton)
		if err != nil {
			return savedInput{}, err
		}
		return savedInput{Kind: "gamepad_button", Button: btn}, nil
	default:
		return savedInput{}, fmt.Errorf("unknown input kind %d", in.Kind)
	}
}

func decodeInput(in savedInput) (Input, error) {
	switch in.Kind {
	case "none":
		return Input{Kind: InputNone}, nil
	case "touch_region":
		if in.Bounds == nil {
			return Input{}, fmt.Errorf("touch_region is missing bounds")
		}
		return TouchRegion(decodeBounds(*in.Bounds)), nil
	case "mouse_button":
		btn, err := decodeMouseButton(in.Button)
		if err != nil {
			return Input{}, err
		}
		return MouseButton(btn), nil
	case "mouse_region":
		if in.Bounds == nil {
			return Input{}, fmt.Errorf("mouse_region is missing bounds")
		}
		btn, err := decodeMouseButton(in.Button)
		if err != nil {
			return Input{}, err
		}
		return MouseRegion(decodeBounds(*in.Bounds), btn), nil
	case "key":
		if in.Key == "" {
			return Input{}, fmt.Errorf("key is missing its key")
		}
		return Key(in.Key), nil
	case "key_code":
		if in.Code == "" {
			return Input{}, fmt.Errorf("key_code is missing its code")
		}
		return KeyCode(in.Code), nil
	case "gamepad_button":
		btn, err := decodeGamepadButton(in.Button)
		if err != nil {
			return Input{}, err
		}
		return GamepadButton(btn), nil
	default:
		return Input{}, fmt.Errorf("unknown input kind %q", in.Kind)
	}
}

func encodeBounds(b core.Bounds) *savedBounds {
	return &savedBounds{
		X: float32(b.Origin.X),
		Y: float32(b.Origin.Y),
		W: float32(b.Size.Width),
		H: float32(b.Size.Height),
	}
}

func decodeBounds(b savedBounds) core.Bounds {
	return core.Bounds{
		Origin: core.Point{X: core.Points(b.X), Y: core.Points(b.Y)},
		Size:   core.Size{Width: core.Points(b.W), Height: core.Points(b.H)},
	}
}

func encodeMouseButton(b core.MouseButton) (string, error) {
	switch b {
	case core.MouseButtonNone:
		return "", nil
	case core.MouseButtonLeft:
		return "left", nil
	case core.MouseButtonRight:
		return "right", nil
	case core.MouseButtonMiddle:
		return "middle", nil
	case core.MouseButtonBack:
		return "back", nil
	case core.MouseButtonForward:
		return "forward", nil
	default:
		return "", fmt.Errorf("unknown mouse button %d", b)
	}
}

func decodeMouseButton(s string) (core.MouseButton, error) {
	switch s {
	case "":
		return core.MouseButtonNone, nil
	case "left":
		return core.MouseButtonLeft, nil
	case "right":
		return core.MouseButtonRight, nil
	case "middle":
		return core.MouseButtonMiddle, nil
	case "back":
		return core.MouseButtonBack, nil
	case "forward":
		return core.MouseButtonForward, nil
	default:
		return core.MouseButtonNone, fmt.Errorf("unknown mouse button %q", s)
	}
}
