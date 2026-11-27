package input

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
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
	Assists     *savedAssists               `json:"assists,omitempty"`
}

type savedAssists struct {
	Toggle        []string `json:"toggle,omitempty"`
	MashToHold    []string `json:"mash_to_hold,omitempty"`
	MinPressMs    int64    `json:"min_press_ms,omitempty"`
	AcceptDelayMs int64    `json:"accept_delay_ms,omitempty"`
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

// Save writes all of a's action, axis, motion axis, pointer axis and game controller stick axis bindings as JSON into store under key, including explicit unbindings made with Unbind, UnbindAxis, UnbindMotionAxis, UnbindPointerAxis or UnbindGamepadAxis so a later Load keeps them unbound rather than restoring defaults. It fails with *core.StorageFullError when the storage is full.
func (a *Actions) Save(store core.Storage, key string) error {
	doc := savedBindings{Version: storageVersion}
	if len(a.bindings) > 0 || len(a.unboundActions) > 0 {
		doc.Actions = make(map[string][]savedInput, len(a.bindings)+len(a.unboundActions))
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
		for name := range a.unboundActions {
			if _, bound := a.bindings[name]; bound {
				continue
			}
			doc.Actions[name] = []savedInput{{Kind: "none"}}
		}
	}
	if len(a.axes) > 0 || len(a.unboundAxes) > 0 {
		doc.Axes = make(map[string]savedAxis, len(a.axes)+len(a.unboundAxes))
		for name, ax := range a.axes {
			doc.Axes[name] = savedAxis{Negative: ax.negative, Positive: ax.positive}
		}
		for name := range a.unboundAxes {
			if _, bound := a.axes[name]; bound {
				continue
			}
			doc.Axes[name] = savedAxis{}
		}
	}
	if len(a.motionAxes) > 0 || len(a.unboundMotionAxes) > 0 {
		doc.MotionAxes = make(map[string]savedMotionAxis, len(a.motionAxes)+len(a.unboundMotionAxes))
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
		for name := range a.unboundMotionAxes {
			if _, bound := a.motionAxes[name]; bound {
				continue
			}
			doc.MotionAxes[name] = savedMotionAxis{Axis: "none"}
		}
	}
	if len(a.pointerAxes) > 0 || len(a.unboundPointerAxes) > 0 {
		doc.PointerAxes = make(map[string]savedPointerAxis, len(a.pointerAxes)+len(a.unboundPointerAxes))
		for name, pb := range a.pointerAxes {
			axName, err := encodePointerAxis(pb.axis)
			if err != nil {
				return fmt.Errorf("input: save %q: pointer axis %q: %w", key, name, err)
			}
			doc.PointerAxes[name] = savedPointerAxis{Axis: axName, Sensitivity: pb.opts.Sensitivity, Invert: pb.opts.Invert}
		}
		for name := range a.unboundPointerAxes {
			if _, bound := a.pointerAxes[name]; bound {
				continue
			}
			doc.PointerAxes[name] = savedPointerAxis{Axis: "none"}
		}
	}
	if len(a.gpAxes) > 0 || len(a.unboundGpAxes) > 0 {
		doc.GamepadAxes = make(map[string]savedGamepadAxis, len(a.gpAxes)+len(a.unboundGpAxes))
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
		for name := range a.unboundGpAxes {
			if _, bound := a.gpAxes[name]; bound {
				continue
			}
			doc.GamepadAxes[name] = savedGamepadAxis{Axis: "none"}
		}
	}
	if !a.assists.IsZero() {
		doc.Assists = &savedAssists{
			Toggle:        slices.Clone(a.assists.Toggle),
			MashToHold:    slices.Clone(a.assists.MashToHold),
			MinPressMs:    a.assists.MinPress.Milliseconds(),
			AcceptDelayMs: a.assists.AcceptDelay.Milliseconds(),
		}
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("input: save %q: %w", key, err)
	}
	return store.SetItem(key, string(data))
}

// Load merges action and axis bindings saved in store under key into a, and reports whether key was present.
//
// Each action, axis, motion axis, pointer axis or gamepad axis present in the saved value replaces a's binding for that name; an entry saved as explicitly unbound (an empty or "none"-only action list, an empty axis, or axis "none") unbinds that name. Names absent from the saved value keep their current bindings, so defaults added in a newer build stay bound when loading an older save. Use LoadReplace to replace all of a's bindings with the saved snapshot.
//
// When key is absent, Load leaves a's existing bindings as they are and returns false, nil. When the stored value is invalid JSON, has a field Load does not know or uses an unknown format, Load leaves a's bindings untouched and returns false and an error: a *loaderr.Error that names key as its file, with the line and field of the problem and how to fix it.
func (a *Actions) Load(store core.Storage, key string) (bool, error) {
	return a.loadKey(store, key, false)
}

// LoadReplace replaces all of a's action and axis bindings with those saved in store under key, dropping any binding absent from the saved value, and reports whether key was present.
//
// When key is absent or the stored value fails to decode, LoadReplace behaves like Load: it leaves a's bindings untouched and returns false with nil or a *loaderr.Error.
func (a *Actions) LoadReplace(store core.Storage, key string) (bool, error) {
	return a.loadKey(store, key, true)
}

func (a *Actions) loadKey(store core.Storage, key string, replace bool) (bool, error) {
	raw, ok := store.GetItem(key)
	if !ok {
		return false, nil
	}
	loaded, err := a.load(raw, replace)
	return loaded, loaderr.Locate(err, key, []byte(raw), "save the bindings again with Actions.Save, or remove the key so the defaults apply")
}

type decodedBindings struct {
	actions            map[string][]Input
	unboundActions     map[string]bool
	axes               map[string]axisBinding
	unboundAxes        map[string]bool
	motionAxes         map[string]motionAxisBinding
	unboundMotionAxes  map[string]bool
	pointerAxes        map[string]pointerAxisBinding
	unboundPointerAxes map[string]bool
	gpAxes             map[string]gamepadAxisBinding
	unboundGpAxes      map[string]bool
	hasAssists         bool
	assists            Assists
}

func allNoneInputs(list []Input) bool {
	for _, in := range list {
		if in.Kind != InputNone {
			return false
		}
	}
	return true
}

func decodeBindings(raw string) (decodedBindings, error) {
	var doc savedBindings
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return decodedBindings{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return decodedBindings{}, &loaderr.Error{Problem: "trailing data after the bindings", Fix: "keep one JSON object in the value; remove what follows it"}
	}
	if doc.Version != storageVersion {
		return decodedBindings{}, &loaderr.Error{Field: "version", Problem: fmt.Sprintf("unsupported version %d", doc.Version), Expected: fmt.Sprint(storageVersion), Fix: fmt.Sprintf("save the bindings again with this build's Actions.Save, which writes version %d", storageVersion)}
	}
	var out decodedBindings
	if len(doc.Actions) > 0 {
		// Sorted, so the first bad binding reported is the same on every load.
		for _, name := range slices.Sorted(maps.Keys(doc.Actions)) {
			list := doc.Actions[name]
			decoded := make([]Input, len(list))
			for i, item := range list {
				in, err := decodeInput(item)
				if err != nil {
					return decodedBindings{}, loaderr.Errorf(loaderr.Path("actions", name, i), `use kind "none", "touch_region" or "mouse_region" with bounds, "mouse_button" with button left, right, middle, back or forward, "key" with key, "key_code" with code, or "gamepad_button" with button`, "action %q: %w", name, err)
				}
				decoded[i] = in
			}
			if allNoneInputs(decoded) {
				if name != "" {
					if out.unboundActions == nil {
						out.unboundActions = make(map[string]bool)
					}
					out.unboundActions[name] = true
				}
				continue
			}
			if out.actions == nil {
				out.actions = make(map[string][]Input, len(doc.Actions))
			}
			out.actions[name] = decoded
		}
	}
	if len(doc.Axes) > 0 {
		for name, ax := range doc.Axes {
			if ax.Negative == "" && ax.Positive == "" {
				if name != "" {
					if out.unboundAxes == nil {
						out.unboundAxes = make(map[string]bool)
					}
					out.unboundAxes[name] = true
				}
				continue
			}
			if out.axes == nil {
				out.axes = make(map[string]axisBinding, len(doc.Axes))
			}
			out.axes[name] = axisBinding{negative: ax.Negative, positive: ax.Positive}
		}
	}

	if len(doc.MotionAxes) > 0 {
		for _, name := range slices.Sorted(maps.Keys(doc.MotionAxes)) {
			ma := doc.MotionAxes[name]
			ax, err := decodeMotionAxis(ma.Axis)
			if err != nil {
				return decodedBindings{}, loaderr.Errorf(loaderr.Path("motion_axes", name, "axis"), "use none, tilt_x, tilt_y, tilt_z, gyro_x, gyro_y or gyro_z", "motion axis %q: %w", name, err)
			}
			if ax == MotionAxisNone {
				if name != "" {
					if out.unboundMotionAxes == nil {
						out.unboundMotionAxes = make(map[string]bool)
					}
					out.unboundMotionAxes[name] = true
				}
				continue
			}
			if out.motionAxes == nil {
				out.motionAxes = make(map[string]motionAxisBinding, len(doc.MotionAxes))
			}
			out.motionAxes[name] = motionAxisBinding{
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

	if len(doc.PointerAxes) > 0 {
		for _, name := range slices.Sorted(maps.Keys(doc.PointerAxes)) {
			pa := doc.PointerAxes[name]
			ax, err := decodePointerAxis(pa.Axis)
			if err != nil {
				return decodedBindings{}, loaderr.Errorf(loaderr.Path("pointer_axes", name, "axis"), "use none, pointer_x or pointer_y", "pointer axis %q: %w", name, err)
			}
			if ax == PointerAxisNone {
				if name != "" {
					if out.unboundPointerAxes == nil {
						out.unboundPointerAxes = make(map[string]bool)
					}
					out.unboundPointerAxes[name] = true
				}
				continue
			}
			if out.pointerAxes == nil {
				out.pointerAxes = make(map[string]pointerAxisBinding, len(doc.PointerAxes))
			}
			out.pointerAxes[name] = pointerAxisBinding{axis: ax, opts: PointerAxisOptions{Sensitivity: pa.Sensitivity, Invert: pa.Invert}}
		}
	}

	if len(doc.GamepadAxes) > 0 {
		for _, name := range slices.Sorted(maps.Keys(doc.GamepadAxes)) {
			ga := doc.GamepadAxes[name]
			if ga.Axis == "" || ga.Axis == "none" {
				if name != "" {
					if out.unboundGpAxes == nil {
						out.unboundGpAxes = make(map[string]bool)
					}
					out.unboundGpAxes[name] = true
				}
				continue
			}
			ax, err := decodeGamepadAxis(ga.Axis)
			if err != nil {
				return decodedBindings{}, loaderr.Errorf(loaderr.Path("gamepad_axes", name, "axis"), "use none or one of "+strings.Join(gamepadAxisNames[:], ", "), "gamepad axis %q: %w", name, err)
			}
			if out.gpAxes == nil {
				out.gpAxes = make(map[string]gamepadAxisBinding, len(doc.GamepadAxes))
			}
			out.gpAxes[name] = gamepadAxisBinding{
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
	if doc.Assists != nil {
		if doc.Assists.MinPressMs < 0 {
			return decodedBindings{}, loaderr.Errorf(loaderr.Path("assists", "min_press_ms"), "use 0 or a positive number of milliseconds", "assists min_press_ms %d must not be negative", doc.Assists.MinPressMs)
		}
		if doc.Assists.AcceptDelayMs < 0 {
			return decodedBindings{}, loaderr.Errorf(loaderr.Path("assists", "accept_delay_ms"), "use 0 or a positive number of milliseconds", "assists accept_delay_ms %d must not be negative", doc.Assists.AcceptDelayMs)
		}
		out.hasAssists = true
		out.assists = normalizeAssists(Assists{
			Toggle:      doc.Assists.Toggle,
			MashToHold:  doc.Assists.MashToHold,
			MinPress:    time.Duration(doc.Assists.MinPressMs) * time.Millisecond,
			AcceptDelay: time.Duration(doc.Assists.AcceptDelayMs) * time.Millisecond,
		})
	}
	return out, nil
}

func (a *Actions) load(raw string, replace bool) (bool, error) {
	d, err := decodeBindings(raw)
	if err != nil {
		return false, err
	}
	if replace {
		a.bindings = d.actions
		a.unboundActions = d.unboundActions
		a.axes = d.axes
		a.unboundAxes = d.unboundAxes
		a.motionAxes = d.motionAxes
		a.unboundMotionAxes = d.unboundMotionAxes
		a.pointerAxes = d.pointerAxes
		a.unboundPointerAxes = d.unboundPointerAxes
		a.gpAxes = d.gpAxes
		a.unboundGpAxes = d.unboundGpAxes
		if d.hasAssists {
			a.SetAssists(d.assists)
		} else {
			a.SetAssists(Assists{})
		}
	} else {
		mergeBindings(&a.bindings, &a.unboundActions, d.actions, d.unboundActions)
		mergeBindings(&a.axes, &a.unboundAxes, d.axes, d.unboundAxes)
		mergeBindings(&a.motionAxes, &a.unboundMotionAxes, d.motionAxes, d.unboundMotionAxes)
		mergeBindings(&a.pointerAxes, &a.unboundPointerAxes, d.pointerAxes, d.unboundPointerAxes)
		mergeBindings(&a.gpAxes, &a.unboundGpAxes, d.gpAxes, d.unboundGpAxes)
		if d.hasAssists {
			a.SetAssists(d.assists)
		}
	}
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

func mergeBindings[T any](dst *map[string]T, dstUnbound *map[string]bool, src map[string]T, srcUnbound map[string]bool) {
	if len(src) > 0 {
		if *dst == nil {
			*dst = make(map[string]T, len(src))
		}
		for name, v := range src {
			(*dst)[name] = v
			delete(*dstUnbound, name)
		}
	}
	if len(srcUnbound) > 0 {
		if *dstUnbound == nil {
			*dstUnbound = make(map[string]bool, len(srcUnbound))
		}
		for name := range srcUnbound {
			delete(*dst, name)
			(*dstUnbound)[name] = true
		}
	}
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
