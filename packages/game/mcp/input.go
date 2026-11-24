package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
)

// Input injection tool names registered by Bridge with github.com/nsgo-dev/nsgo/automation.
const (
	// ToolInputAction injects a press, release, tap, or analog value for a named input action.
	ToolInputAction = "game_input_action"
	// ToolInputAxis injects or clears one or more analog axis values in [-1, 1].
	ToolInputAxis = "game_input_axis"
	// ToolInputPointer injects a pointer press, release, move, or click in game (or world) coordinates mapped through the viewport.
	ToolInputPointer = "game_input_pointer"
	// ToolInputActionsList lists configured actions and axes along with their current step state.
	ToolInputActionsList = "game_input_actions_list"
)

// PointerAction identifies the kind of pointer event delivered to Config.OnPointer.
type PointerAction string

const (
	// PointerPress reports a pointer button press.
	PointerPress PointerAction = "press"
	// PointerRelease reports a pointer button release.
	PointerRelease PointerAction = "release"
	// PointerMove reports pointer movement.
	PointerMove PointerAction = "move"
)

// PointerEvent describes a pointer event injected through game_input_pointer after viewport mapping and scene hit testing.
type PointerEvent struct {
	// Action is PointerPress, PointerRelease, or PointerMove. A "click" tool call emits PointerPress followed by PointerRelease.
	Action PointerAction
	// Button is the mouse button for the event (MouseButtonLeft, MouseButtonRight, MouseButtonMiddle, or MouseButtonNone when hovering).
	Button core.MouseButton
	// GamePoint is the event position in virtual game canvas coordinates.
	GamePoint core.Point
	// ViewPoint is GamePoint mapped through the configured screen.Viewport into view coordinates (equal to GamePoint when no viewport is configured).
	ViewPoint core.Point
	// WorldPoint is GamePoint converted into world coordinates through the active scene's Camera when present, or the supplied world point when space="world".
	WorldPoint core.Point
	// HasWorldPoint reports whether WorldPoint is populated.
	HasWorldPoint bool
	// InViewport reports whether ViewPoint lies inside the visible viewport area (true when no viewport is configured).
	InViewport bool
	// Hit is the topmost hit returned by the active scene's HitTest(GamePoint) when InViewport is true, or nil if nothing was hit.
	Hit *scene.HitResult
	// Dispatched reports whether Scene.DispatchPress invoked a sprite or drawable OnPress callback for this event.
	Dispatched bool
}

// InputActionResult is the JSON result of game_input_action.
type InputActionResult struct {
	Name         string   `json:"name"`
	Mode         string   `json:"mode"`
	Value        *float64 `json:"value,omitempty"`
	Pressed      bool     `json:"pressed"`
	JustPressed  bool     `json:"just_pressed,omitempty"`
	JustReleased bool     `json:"just_released,omitempty"`
	Axis         float64  `json:"axis,omitempty"`
}

// InputAxisResult is the JSON result of game_input_axis.
type InputAxisResult struct {
	Axes    map[string]float64 `json:"axes"`
	Cleared []string           `json:"cleared,omitempty"`
}

// InputPointerResult is the JSON result of game_input_pointer.
type InputPointerResult struct {
	Action           string   `json:"action"`
	Space            string   `json:"space"`
	Button           string   `json:"button"`
	GameX            float32  `json:"game_x"`
	GameY            float32  `json:"game_y"`
	ViewX            float32  `json:"view_x"`
	ViewY            float32  `json:"view_y"`
	WorldX           *float32 `json:"world_x,omitempty"`
	WorldY           *float32 `json:"world_y,omitempty"`
	InViewport       bool     `json:"in_viewport"`
	Dispatched       bool     `json:"dispatched,omitempty"`
	Hit              bool     `json:"hit"`
	HitLayer         string   `json:"hit_layer,omitempty"`
	HitSpace         string   `json:"hit_space,omitempty"`
	HitKind          string   `json:"hit_kind,omitempty"`
	HitFrame         string   `json:"hit_frame,omitempty"`
	HitBatchIndex    *int     `json:"hit_batch_index,omitempty"`
	HitSpriteIndex   *int     `json:"hit_sprite_index,omitempty"`
	HitDrawableIndex *int     `json:"hit_drawable_index,omitempty"`
	HitX             *float32 `json:"hit_x,omitempty"`
	HitY             *float32 `json:"hit_y,omitempty"`
}

// InputActionInfo describes one action entry returned by game_input_actions_list.
type InputActionInfo struct {
	Name         string `json:"name"`
	Pressed      bool   `json:"pressed"`
	JustPressed  bool   `json:"just_pressed,omitempty"`
	JustReleased bool   `json:"just_released,omitempty"`
	BindingCount int    `json:"binding_count,omitempty"`
}

// InputAxisInfo describes one axis entry returned by game_input_actions_list.
type InputAxisInfo struct {
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Negative string  `json:"negative,omitempty"`
	Positive string  `json:"positive,omitempty"`
}

// InputActionsListResult is the JSON result of game_input_actions_list.
type InputActionsListResult struct {
	Configured bool              `json:"configured"`
	Actions    []InputActionInfo `json:"actions"`
	Axes       []InputAxisInfo   `json:"axes"`
}

var (
	schemaInputAction = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["name"],
		"properties": {
			"name": {
				"type": "string",
				"minLength": 1,
				"description": "Action (or axis) name to inject into input.Actions."
			},
			"mode": {
				"type": "string",
				"enum": ["press", "release", "tap"],
				"description": "Action injection mode: press (hold across steps until release), release (end hold), or tap (default, single-step trigger)."
			},
			"value": {
				"type": "number",
				"minimum": -1,
				"maximum": 1,
				"description": "Optional analog axis value in [-1, 1] to set for name."
			}
		}
	}`)

	schemaInputAxis = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"name": {
				"type": "string",
				"minLength": 1,
				"description": "Single axis name to set or clear."
			},
			"value": {
				"type": "number",
				"minimum": -1,
				"maximum": 1,
				"description": "Analog axis value in [-1, 1] for name."
			},
			"axes": {
				"type": "object",
				"additionalProperties": {
					"type": "number",
					"minimum": -1,
					"maximum": 1
				},
				"description": "Map of axis names to analog values in [-1, 1] (for 1D/2D stick injection)."
			},
			"clear": {
				"type": "boolean",
				"description": "Clear injected values for name and/or the keys in axes."
			}
		}
	}`)

	schemaInputPointer = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["x", "y"],
		"properties": {
			"action": {
				"type": "string",
				"enum": ["press", "release", "move", "click"],
				"description": "Pointer event to inject: press, release, move, or click (default, press followed by release)."
			},
			"x": {
				"type": "number",
				"description": "Horizontal coordinate in game space (or world space when space=\"world\")."
			},
			"y": {
				"type": "number",
				"description": "Vertical coordinate in game space (or world space when space=\"world\")."
			},
			"space": {
				"type": "string",
				"enum": ["game", "world"],
				"description": "Coordinate space of (x, y): \"game\" (default, virtual canvas coordinates) or \"world\" (mapped through the active scene's Camera)."
			},
			"button": {
				"type": "string",
				"enum": ["left", "right", "middle"],
				"description": "Mouse button for press, release, click, or drag move (default \"left\" for press/release/click; preserves held button for move)."
			}
		}
	}`)

	schemaInputActionsList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {}
	}`)
)

func (b *Bridge) inputTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolInputAction,
			Description: "Inject a press, release, tap, or analog value for a named action on input.Actions.",
			InputSchema: schemaInputAction,
			Handler:     b.handleInputAction,
		},
		{
			Name:        ToolInputAxis,
			Description: "Inject or clear 1D or multi-axis analog values in [-1, 1] on input.Actions.",
			InputSchema: schemaInputAxis,
			Handler:     b.handleInputAxis,
		},
		{
			Name:        ToolInputPointer,
			Description: "Inject a pointer press, release, move, or click in game (or camera world) coordinates, mapped through screen.Viewport and delivered to input.Actions, scene hit-testing/DispatchPress, and Config.OnPointer.",
			InputSchema: schemaInputPointer,
			Handler:     b.handleInputPointer,
		},
		{
			Name:        ToolInputActionsList,
			Description: "List configured input action and axis names along with their current step state.",
			InputSchema: schemaInputActionsList,
			Handler:     b.handleInputActionsList,
		},
	}
}

func (b *Bridge) activeActions() *input.Actions {
	if b == nil {
		return nil
	}
	if b.cfg.ActionsFunc != nil {
		if a := b.cfg.ActionsFunc(); a != nil {
			return a
		}
	}
	return b.cfg.Actions
}

func (b *Bridge) requireActions() (*input.Actions, error) {
	a := b.activeActions()
	if a == nil {
		return nil, errors.New("mcp: no input actions configured")
	}
	return a, nil
}

func (b *Bridge) activeViewport() (screen.Viewport, bool) {
	if b == nil {
		return screen.Viewport{}, false
	}
	if b.cfg.ViewportFunc != nil {
		vp := b.cfg.ViewportFunc()
		if vp.Width > 0 && vp.Height > 0 {
			return vp, true
		}
	}
	if b.cfg.Viewport != nil && b.cfg.Viewport.Width > 0 && b.cfg.Viewport.Height > 0 {
		return *b.cfg.Viewport, true
	}
	return screen.Viewport{}, false
}

func (b *Bridge) activeViewSize(vp screen.Viewport, hasVP bool) core.Size {
	if b != nil && b.cfg.ViewSizeFunc != nil {
		if sz := b.cfg.ViewSizeFunc(); sz.Width > 0 && sz.Height > 0 {
			return sz
		}
	}
	if b != nil && b.sub != nil && !b.sub.Disposed() {
		if sz := b.sub.Env().Window(); sz.Width > 0 && sz.Height > 0 {
			return sz
		}
	}
	if hasVP {
		return core.Size{Width: core.Points(vp.Width), Height: core.Points(vp.Height)}
	}
	return core.Size{}
}

func (b *Bridge) currentActiveScene() *scene.Scene {
	_, resolved := b.collectScenes()
	for i := len(resolved) - 1; i >= 0; i-- {
		if resolved[i].info.Current {
			return resolved[i].scene
		}
	}
	return nil
}

type inputActionArgs struct {
	Name  string   `json:"name"`
	Mode  string   `json:"mode"`
	Value *float64 `json:"value"`
}

func (b *Bridge) handleInputAction(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[inputActionArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Name == "" {
		return nil, errors.New("mcp: name is required")
	}
	if args.Value != nil {
		if err := validateAxisValue("value", *args.Value); err != nil {
			return nil, err
		}
	}

	mode := args.Mode
	if mode == "" {
		if args.Value != nil {
			mode = "axis"
		} else {
			mode = "tap"
		}
	}
	switch mode {
	case "press", "release", "tap", "axis":
	default:
		return nil, fmt.Errorf("mcp: invalid mode %q (want press, release, or tap)", args.Mode)
	}

	actions, err := b.requireActions()
	if err != nil {
		return nil, err
	}

	switch mode {
	case "press":
		actions.Press(args.Name)
		if args.Value != nil {
			actions.SetAxis(args.Name, *args.Value)
		}
	case "release":
		actions.Release(args.Name)
		if args.Value != nil {
			actions.SetAxis(args.Name, *args.Value)
		} else {
			actions.ClearAxis(args.Name)
		}
	case "tap":
		actions.Trigger(args.Name)
		if args.Value != nil {
			actions.SetAxis(args.Name, *args.Value)
		}
	case "axis":
		actions.SetAxis(args.Name, *args.Value)
	}

	return InputActionResult{
		Name:         args.Name,
		Mode:         mode,
		Value:        args.Value,
		Pressed:      actions.Pressed(args.Name),
		JustPressed:  actions.JustPressed(args.Name),
		JustReleased: actions.JustReleased(args.Name),
		Axis:         actions.Axis(args.Name),
	}, nil
}

type inputAxisArgs struct {
	Name  string             `json:"name"`
	Value *float64           `json:"value"`
	Axes  map[string]float64 `json:"axes"`
	Clear bool               `json:"clear"`
}

func (b *Bridge) handleInputAxis(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[inputAxisArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Name == "" && len(args.Axes) == 0 {
		return nil, errors.New("mcp: name or axes is required")
	}
	if args.Name != "" && args.Value == nil && !args.Clear {
		return nil, errors.New("mcp: value is required unless clear is true")
	}
	if args.Value != nil {
		if args.Name == "" {
			return nil, errors.New("mcp: name is required when value is set")
		}
		if err := validateAxisValue(args.Name, *args.Value); err != nil {
			return nil, err
		}
	}
	for k, v := range args.Axes {
		if k == "" {
			return nil, errors.New("mcp: axis name in axes must be non-empty")
		}
		if !args.Clear {
			if err := validateAxisValue(k, v); err != nil {
				return nil, err
			}
		}
	}

	actions, err := b.requireActions()
	if err != nil {
		return nil, err
	}

	res := InputAxisResult{
		Axes: make(map[string]float64),
	}
	if args.Clear {
		cleared := make(map[string]struct{})
		if args.Name != "" {
			actions.ClearAxis(args.Name)
			cleared[args.Name] = struct{}{}
		}
		for k := range args.Axes {
			actions.ClearAxis(k)
			cleared[k] = struct{}{}
		}
		res.Cleared = make([]string, 0, len(cleared))
		for k := range cleared {
			res.Cleared = append(res.Cleared, k)
		}
		slices.Sort(res.Cleared)
		return res, nil
	}

	if args.Name != "" && args.Value != nil {
		actions.SetAxis(args.Name, *args.Value)
		res.Axes[args.Name] = *args.Value
	}
	for k, v := range args.Axes {
		actions.SetAxis(k, v)
		res.Axes[k] = v
	}
	return res, nil
}

type inputPointerArgs struct {
	Action string   `json:"action"`
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
	Space  string   `json:"space"`
	Button string   `json:"button"`
}

func (b *Bridge) handleInputPointer(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[inputPointerArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.X == nil || args.Y == nil {
		return nil, errors.New("mcp: x and y are required")
	}
	if err := validateCoord("x", *args.X); err != nil {
		return nil, err
	}
	if err := validateCoord("y", *args.Y); err != nil {
		return nil, err
	}

	action := args.Action
	if action == "" {
		action = "click"
	}
	switch action {
	case "press", "release", "move", "click":
	default:
		return nil, fmt.Errorf("mcp: invalid pointer action %q (want press, release, move, or click)", args.Action)
	}

	space := args.Space
	if space == "" {
		space = "game"
	}
	switch space {
	case "game", "world":
	default:
		return nil, fmt.Errorf("mcp: invalid space %q (want game or world)", args.Space)
	}

	btn, btnName, err := resolvePointerButton(action, args.Button, b.pointerButton)
	if err != nil {
		return nil, err
	}

	sc := b.currentActiveScene()
	var (
		gamePt   core.Point
		worldPt  core.Point
		hasWorld bool
	)
	if space == "world" {
		if sc == nil {
			return nil, errors.New(`mcp: space "world" requires an active scene`)
		}
		worldPt = core.Point{X: core.Points(*args.X), Y: core.Points(*args.Y)}
		hasWorld = true
		if sc.Camera != nil {
			gamePt = sc.Camera.WorldToView(worldPt)
		} else {
			gamePt = worldPt
		}
	} else {
		gamePt = core.Point{X: core.Points(*args.X), Y: core.Points(*args.Y)}
		if sc != nil && sc.Camera != nil {
			worldPt = sc.Camera.ViewToWorld(gamePt)
			hasWorld = true
		}
	}

	vp, hasVP := b.activeViewport()
	viewPt := gamePt
	inViewport := true
	if hasVP {
		winSize := b.activeViewSize(vp, true)
		pl := vp.Layout(winSize)
		if pl.ScaleX <= 0 || pl.ScaleY <= 0 {
			inViewport = false
		} else {
			viewPt = vp.GameToView(winSize, gamePt)
			_, inViewport = vp.ViewToGame(winSize, viewPt)
		}
	}

	var hit *scene.HitResult
	if sc != nil && inViewport {
		hit, _ = sc.HitTest(gamePt)
	}

	var phases []PointerAction
	switch action {
	case "press":
		phases = []PointerAction{PointerPress}
	case "release":
		phases = []PointerAction{PointerRelease}
	case "move":
		phases = []PointerAction{PointerMove}
	case "click":
		phases = []PointerAction{PointerPress, PointerRelease}
	}

	actions := b.activeActions()
	dispatched := false
	for _, phase := range phases {
		phaseDispatched := false
		if actions != nil {
			deliverPointerToActions(actions, phase, btn, viewPt)
		}
		switch phase {
		case PointerPress:
			b.pointerButton = btn
			if sc != nil && inViewport && btn == core.MouseButtonLeft {
				phaseDispatched = sc.DispatchPress(core.PressEvent{
					Position:     gamePt,
					PagePosition: viewPt,
					Clicks:       1,
				})
				if phaseDispatched {
					dispatched = true
				}
			}
		case PointerRelease:
			b.pointerButton = core.MouseButtonNone
		}
		if b.cfg.OnPointer != nil {
			b.cfg.OnPointer(PointerEvent{
				Action:        phase,
				Button:        btn,
				GamePoint:     gamePt,
				ViewPoint:     viewPt,
				WorldPoint:    worldPt,
				HasWorldPoint: hasWorld,
				InViewport:    inViewport,
				Hit:           hit,
				Dispatched:    phaseDispatched,
			})
		}
	}

	res := InputPointerResult{
		Action:     action,
		Space:      space,
		Button:     btnName,
		GameX:      float32(gamePt.X),
		GameY:      float32(gamePt.Y),
		ViewX:      float32(viewPt.X),
		ViewY:      float32(viewPt.Y),
		InViewport: inViewport,
		Dispatched: dispatched,
	}
	if hasWorld {
		wx, wy := float32(worldPt.X), float32(worldPt.Y)
		res.WorldX = &wx
		res.WorldY = &wy
	}
	if hit != nil {
		populateHitResult(&res, hit)
	}
	return res, nil
}

func (b *Bridge) handleInputActionsList(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	res := InputActionsListResult{
		Actions: []InputActionInfo{},
		Axes:    []InputAxisInfo{},
	}
	actions := b.activeActions()
	if actions == nil {
		return res, nil
	}
	res.Configured = true
	for _, name := range actions.ActionNames() {
		res.Actions = append(res.Actions, InputActionInfo{
			Name:         name,
			Pressed:      actions.Pressed(name),
			JustPressed:  actions.JustPressed(name),
			JustReleased: actions.JustReleased(name),
			BindingCount: len(actions.Bindings(name)),
		})
	}
	for _, name := range actions.AxisNames() {
		neg, pos, _ := actions.AxisBinding(name)
		res.Axes = append(res.Axes, InputAxisInfo{
			Name:     name,
			Value:    actions.Axis(name),
			Negative: neg,
			Positive: pos,
		})
	}
	return res, nil
}

func validateAxisValue(name string, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("mcp: %s must be finite", name)
	}
	if v < -1 || v > 1 {
		return fmt.Errorf("mcp: %s must be in [-1, 1] (got %g)", name, v)
	}
	return nil
}

func validateCoord(name string, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > math.MaxFloat32 {
		return fmt.Errorf("mcp: %s must be a finite float32 coordinate", name)
	}
	return nil
}

func resolvePointerButton(action, raw string, held core.MouseButton) (core.MouseButton, string, error) {
	if raw == "" {
		if action == "move" {
			return held, pointerButtonName(held), nil
		}
		return core.MouseButtonLeft, "left", nil
	}
	switch raw {
	case "left":
		return core.MouseButtonLeft, "left", nil
	case "right":
		return core.MouseButtonRight, "right", nil
	case "middle":
		return core.MouseButtonMiddle, "middle", nil
	default:
		return core.MouseButtonNone, "", fmt.Errorf("mcp: invalid button %q (want left, right, or middle)", raw)
	}
}

func pointerButtonName(btn core.MouseButton) string {
	switch btn {
	case core.MouseButtonLeft:
		return "left"
	case core.MouseButtonRight:
		return "right"
	case core.MouseButtonMiddle:
		return "middle"
	default:
		return "none"
	}
}

func deliverPointerToActions(a *input.Actions, phase PointerAction, btn core.MouseButton, viewPt core.Point) {
	switch phase {
	case PointerPress:
		if btn == core.MouseButtonLeft {
			a.OnPressDown(core.PressEvent{
				Position:     viewPt,
				PagePosition: viewPt,
				Clicks:       1,
			})
			return
		}
		a.OnMouseMove(core.MouseEvent{
			Position:      viewPt,
			PagePosition:  viewPt,
			PressedButton: btn,
		})
	case PointerRelease:
		if btn == core.MouseButtonLeft {
			a.OnPressUp(core.PressEvent{
				Position:     viewPt,
				PagePosition: viewPt,
				Clicks:       1,
			})
			return
		}
		a.OnMouseMove(core.MouseEvent{
			Position:      viewPt,
			PagePosition:  viewPt,
			PressedButton: core.MouseButtonNone,
		})
	case PointerMove:
		a.OnMouseMove(core.MouseEvent{
			Position:      viewPt,
			PagePosition:  viewPt,
			PressedButton: btn,
		})
	}
}

func populateHitResult(res *InputPointerResult, hit *scene.HitResult) {
	res.Hit = true
	res.HitSpace = spaceName(hit.Space)
	hx, hy := float32(hit.Point.X), float32(hit.Point.Y)
	res.HitX = &hx
	res.HitY = &hy
	if hit.Layer != nil {
		res.HitLayer = hit.Layer.Name
	}
	if hit.Sprite != nil {
		res.HitKind = "sprite"
		res.HitFrame = hit.Sprite.Frame.Name
		if hit.Batch != nil {
			if si := slices.Index(hit.Batch.Sprites, hit.Sprite); si >= 0 {
				res.HitSpriteIndex = &si
			}
			if hit.Layer != nil {
				bi := 0
				for _, it := range hit.Layer.Items {
					if it.Batch != nil {
						if it.Batch == hit.Batch {
							idx := bi
							res.HitBatchIndex = &idx
							break
						}
						bi++
					}
				}
			}
		}
		return
	}
	if hit.Drawable != nil {
		res.HitKind = "drawable"
		if hit.Layer != nil {
			di := 0
			for _, it := range hit.Layer.Items {
				if it.Drawable != nil {
					if it.Drawable == hit.Drawable {
						idx := di
						res.HitDrawableIndex = &idx
						break
					}
					di++
				}
			}
		}
	}
}
