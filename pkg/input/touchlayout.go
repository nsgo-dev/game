package input

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// TouchLayoutVersion is the touch layout schema version this package reads and writes.
const TouchLayoutVersion = 1

// TouchLayoutSchemaURL is where the JSON Schema of version 1 touch layouts is published. EncodeTouchLayout writes it in a yaml-language-server comment on the first line so editors and AI tools validate and complete layout files as they are written.
const TouchLayoutSchemaURL = "https://game.nsgo.dev/schemas/touchlayout.v1.json"

// TouchLayoutSchemaComment is the first line EncodeTouchLayout writes: a YAML comment pointing a YAML language server at TouchLayoutSchemaURL. ParseTouchLayout ignores it like any other comment.
const TouchLayoutSchemaComment = "# yaml-language-server: $schema=" + TouchLayoutSchemaURL

// MinTouchTarget is the smallest width and height, in points, TouchControls gives a control however small the screen, the 44-point minimum Apple's and Google's guidelines set for a touch target.
const MinTouchTarget = 44

// The anchors a TouchControlSpec is placed against: a corner, the middle of an edge, or the center of the safe area.
const (
	AnchorTopLeft     = "topLeft"
	AnchorTop         = "top"
	AnchorTopRight    = "topRight"
	AnchorLeft        = "left"
	AnchorCenter      = "center"
	AnchorRight       = "right"
	AnchorBottomLeft  = "bottomLeft"
	AnchorBottom      = "bottom"
	AnchorBottomRight = "bottomRight"
)

// The control kinds this package registers; RegisterTouchControlKind adds others.
const (
	// TouchKindButton is a VirtualButton: action, with text or icon, mode and shape.
	TouchKindButton = "button"
	// TouchKindDPad is a VirtualDPad: up, down, left and right actions, axisX and axisY axes, ways and deadZone.
	TouchKindDPad = "dpad"
	// TouchKindStick is a VirtualStick with an up-positive Y axis: axisX and axisY axes, deadZone, floating and radius.
	TouchKindStick = "stick"
)

// The modes and shapes a button control takes.
const (
	TouchModeHold   = "hold"
	TouchModeToggle = "toggle"
	TouchModeTap    = "tap"

	TouchShapeCircle = "circle"
	TouchShapeRect   = "rect"
)

// TouchLayout is the root of a versioned YAML document that declares a game's on-screen touch controls, which NewTouchControls builds.
//
// Lengths are in points on a screen of Reference size. TouchControls scales them by the smaller of the safe area's width and height over Reference's, so the layout keeps its proportions on any screen, and never makes a control smaller than MinTouchTarget.
type TouchLayout struct {
	// Version is the schema version; it must equal TouchLayoutVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Reference is the screen size, in points, the layout's offsets and sizes are written for, such as a landscape phone's 844 by 390.
	Reference TouchSize `yaml:"reference,flow" schema:"required"`
	// MaxScale caps how much the layout grows on a screen larger than Reference, such as a tablet's; 0 leaves it uncapped.
	MaxScale float64 `yaml:"maxScale,omitempty" schema:"minimum=0"`
	// IdleAfter is how long the controls stay fully opaque after the last touch on them before fading to IdleAlpha, as a Go duration such as "3s"; 0 never fades them.
	IdleAfter time.Duration `yaml:"idleAfter,omitempty"`
	// IdleAlpha is the opacity in [0, 1] the controls fade to once idle.
	IdleAlpha float64 `yaml:"idleAlpha,omitempty" schema:"minimum=0,maximum=1"`
	// Mirrored swaps every control between the left and right of the screen, for left-handed play.
	Mirrored bool `yaml:"mirrored,omitempty"`
	// Controls lists the controls, drawn in order; a later control draws over an earlier one.
	Controls []TouchControlSpec `yaml:"controls,omitempty"`
}

// TouchSize is a width and a height in reference points.
type TouchSize struct {
	Width  float64 `yaml:"width" schema:"required,exclusiveMinimum=0"`
	Height float64 `yaml:"height" schema:"required,exclusiveMinimum=0"`
}

// TouchOffset moves a control from its anchor, in reference points, toward the inside of the screen: X from a left or right anchor's edge and Y from a top or bottom anchor's edge, and right and down from a centered anchor.
type TouchOffset struct {
	X float64 `yaml:"x,omitempty"`
	Y float64 `yaml:"y,omitempty"`
}

// TouchControlSpec declares one control of a TouchLayout. The fields a kind does not take are refused for the kinds this package registers; a kind a plugin registers reads the fields it documents, and Options for anything else.
type TouchControlSpec struct {
	// ID names the control uniquely within the layout, for TouchControls.Control and for a player's saved edits.
	ID string `yaml:"id" schema:"required,minLength=1"`
	// Kind is the control's kind: button, dpad, stick, or a name registered with RegisterTouchControlKind.
	Kind string `yaml:"kind" schema:"required,minLength=1"`
	// Anchor is the point of the safe area the control is placed against.
	Anchor string `yaml:"anchor" schema:"required,enum=topLeft|top|topRight|left|center|right|bottomLeft|bottom|bottomRight"`
	// Offset moves the control from its anchor toward the inside of the screen.
	Offset TouchOffset `yaml:"offset,omitempty,flow"`
	// Size is the control's width and height; a round control fits a circle inside it.
	Size TouchSize `yaml:"size,flow" schema:"required"`
	// Label is the control's accessibility label, read by screen readers and shown by TouchControls.Controls.
	Label string `yaml:"label,omitempty"`
	// Action is the action a button presses.
	Action string `yaml:"action,omitempty"`
	// Text is the caption drawn on a button.
	Text string `yaml:"text,omitempty"`
	// Icon names a glyph registered with RegisterTouchIcon that a button draws in place of Text.
	Icon string `yaml:"icon,omitempty"`
	// Mode is a button's behavior: hold (the default), toggle or tap.
	Mode string `yaml:"mode,omitempty" schema:"enum=hold|toggle|tap"`
	// Shape is a button's shape: circle (the default) or rect.
	Shape string `yaml:"shape,omitempty" schema:"enum=circle|rect"`
	// Ways is 4 (the default) or 8 for a d-pad that also selects diagonals.
	Ways int `yaml:"ways,omitempty" schema:"enum=4|8"`
	// Up, Down, Left and Right are the actions a d-pad's directions press.
	Up    string `yaml:"up,omitempty"`
	Down  string `yaml:"down,omitempty"`
	Left  string `yaml:"left,omitempty"`
	Right string `yaml:"right,omitempty"`
	// AxisX and AxisY are the axes a d-pad or stick drives, AxisY positive up.
	AxisX string `yaml:"axisX,omitempty"`
	AxisY string `yaml:"axisY,omitempty"`
	// DeadZone is the fraction of a d-pad's or stick's radius in [0, 1) that reads as rest; 0 keeps the control's default.
	DeadZone float64 `yaml:"deadZone,omitempty" schema:"minimum=0,exclusiveMaximum=1"`
	// Floating makes a stick re-center on the finger that touches down anywhere in its frame.
	Floating bool `yaml:"floating,omitempty"`
	// Radius is a stick's knob travel in reference points; 0 uses half the smaller side of Size.
	Radius float64 `yaml:"radius,omitempty" schema:"minimum=0"`
	// Options holds settings for a kind a plugin registers, as strings: a value the YAML would read as a number, boolean or null is written quoted, such as speed: "3", so it reads back the same. The kinds this package registers refuse it.
	Options map[string]string `yaml:"options,omitempty"`
}

const fixTouchLayout = "correct the field as the touch layout schema describes (version 1, at https://game.nsgo.dev/schemas/touchlayout.v1.json)"

// ParseTouchLayout decodes a YAML touch layout and validates it with ValidateTouchLayout. Unknown fields, a second YAML document and any version other than TouchLayoutVersion are refused.
//
// Errors are *loaderr.Error values carrying the line, column, field path and a fix; a caller that read the layout from a file names it with loaderr.WithFile.
func ParseTouchLayout(data []byte) (TouchLayout, error) {
	l, err := parseTouchLayout(data)
	if err != nil {
		return TouchLayout{}, loaderr.Locate(err, "", data, fixTouchLayout)
	}
	return l, nil
}

func parseTouchLayout(data []byte) (TouchLayout, error) {
	var l TouchLayout
	if len(bytes.TrimSpace(data)) == 0 {
		return l, &loaderr.Error{Problem: "empty touch layout", Fix: "write a document with version: 1, a reference size and its controls"}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&l); err != nil {
		return l, err
	}
	if err := checkTouchOptions(data); err != nil {
		return l, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return l, &loaderr.Error{Problem: "a second YAML document follows the touch layout", Fix: "keep one document in the file; remove what follows the first"}
		}
		return l, err
	}
	if err := ValidateTouchLayout(l); err != nil {
		return l, err
	}
	return l, nil
}

// checkTouchOptions refuses an options value that is not a YAML string, such as an unquoted number, which would decode into the string map and then encode back quoted, changing the document on its first save.
func checkTouchOptions(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return err
	}
	controls := mappingValue(doc.Content[0], "controls")
	if controls == nil || controls.Kind != yaml.SequenceNode {
		return nil
	}
	for i, c := range controls.Content {
		opts := mappingValue(c, "options")
		if opts == nil || opts.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(opts.Content); j += 2 {
			key, v := opts.Content[j], opts.Content[j+1]
			if v.Kind == yaml.AliasNode && v.Alias != nil {
				v = v.Alias
			}
			if v.Kind == yaml.ScalarNode && v.ShortTag() == "!!str" {
				continue
			}
			fix := "write the value as a string"
			if v.Kind == yaml.ScalarNode {
				fix = fmt.Sprintf("quote the value, as in %s: %q", key.Value, v.Value)
			}
			return &loaderr.Error{
				Field:    loaderr.Path("controls", i, "options", key.Value),
				Line:     v.Line,
				Column:   v.Column,
				Problem:  fmt.Sprintf("option %q is not a string", key.Value),
				Expected: "a string",
				Fix:      fix,
			}
		}
	}
	return nil
}

// mappingValue returns the value of key in mapping node n, or nil.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// EncodeTouchLayout validates l and writes it as deterministic 2-space YAML prefixed with TouchLayoutSchemaComment: fields in a fixed order, Options sorted by key, durations in Go's shortest form such as "4s", and reference, offset and size in flow style. The same layout always encodes to the same bytes, and a canonical document, one EncodeTouchLayout wrote, round-trips through ParseTouchLayout and EncodeTouchLayout byte for byte; another document is normalized to that form, losing its comments other than the schema line.
func EncodeTouchLayout(l TouchLayout) ([]byte, error) {
	if err := ValidateTouchLayout(l); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(TouchLayoutSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(l); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("input: encode touch layout: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("input: encode touch layout: %w", err)
	}
	return buf.Bytes(), nil
}

// ValidateTouchLayout checks that l follows the version 1 schema: the version, a positive reference size, a valid fade, and controls with unique IDs, known anchors, positive sizes and, for the kinds this package registers, only the fields their kind takes. A kind no one registered is accepted here and refused by NewTouchControls, so a layout parses before the plugins that define its kinds load.
//
// Errors are *loaderr.Error values naming the field.
func ValidateTouchLayout(l TouchLayout) error {
	if l.Version != TouchLayoutVersion {
		return &loaderr.Error{Field: "version", Problem: fmt.Sprintf("unsupported version %d", l.Version), Expected: fmt.Sprint(TouchLayoutVersion), Fix: fmt.Sprintf("write version: %d, the touch layout version this build reads", TouchLayoutVersion)}
	}
	if err := validateTouchSize(l.Reference, "reference"); err != nil {
		return err
	}
	if !finite(l.MaxScale) || l.MaxScale < 0 {
		return loaderr.Errorf("maxScale", "write 0 for no cap, or a positive scale", "maxScale %v is not a non-negative number", l.MaxScale)
	}
	if l.IdleAfter < 0 {
		return loaderr.Errorf("idleAfter", `write a positive duration such as "3s", or 0 to never fade`, "idleAfter %v is negative", l.IdleAfter)
	}
	if !finite(l.IdleAlpha) || l.IdleAlpha < 0 || l.IdleAlpha > 1 {
		return loaderr.Errorf("idleAlpha", "write an opacity from 0 to 1", "idleAlpha %v is outside [0, 1]", l.IdleAlpha)
	}
	ids := make(map[string]int, len(l.Controls))
	for i, c := range l.Controls {
		if err := validateTouchControl(c, i); err != nil {
			return err
		}
		if prev, ok := ids[c.ID]; ok {
			return loaderr.Errorf(loaderr.Path("controls", i, "id"), "give each control its own id", "id %q is also controls[%d]'s", c.ID, prev)
		}
		ids[c.ID] = i
	}
	return nil
}

func validateTouchControl(c TouchControlSpec, i int) error {
	field := func(name string) string { return loaderr.Path("controls", i, name) }
	if c.ID == "" {
		return loaderr.Errorf(field("id"), "name the control with a non-empty id", "missing id")
	}
	if c.Kind == "" {
		return loaderr.Errorf(field("kind"), "write button, dpad, stick or a registered kind", "missing kind")
	}
	switch c.Anchor {
	case AnchorTopLeft, AnchorTop, AnchorTopRight, AnchorLeft, AnchorCenter, AnchorRight, AnchorBottomLeft, AnchorBottom, AnchorBottomRight:
	default:
		return &loaderr.Error{Field: field("anchor"), Problem: fmt.Sprintf("unknown anchor %q", c.Anchor), Expected: "topLeft, top, topRight, left, center, right, bottomLeft, bottom or bottomRight", Fix: "write one of the nine anchors"}
	}
	if !finite(c.Offset.X) || !finite(c.Offset.Y) {
		return loaderr.Errorf(field("offset"), "write finite numbers", "offset (%v, %v) is not finite", c.Offset.X, c.Offset.Y)
	}
	if err := validateTouchSize(c.Size, field("size")); err != nil {
		return err
	}
	if !finite(c.DeadZone) || c.DeadZone < 0 || c.DeadZone >= 1 {
		return loaderr.Errorf(field("deadZone"), "write a fraction from 0 up to, but not including, 1", "deadZone %v is outside [0, 1)", c.DeadZone)
	}
	if !finite(c.Radius) || c.Radius < 0 {
		return loaderr.Errorf(field("radius"), "write a non-negative radius, or 0 for half the smaller side of size", "radius %v is negative", c.Radius)
	}
	if c.Ways != 0 && c.Ways != 4 && c.Ways != 8 {
		return &loaderr.Error{Field: field("ways"), Problem: fmt.Sprintf("ways %d", c.Ways), Expected: "4 or 8", Fix: "write 4 or 8"}
	}
	switch c.Mode {
	case "", TouchModeHold, TouchModeToggle, TouchModeTap:
	default:
		return &loaderr.Error{Field: field("mode"), Problem: fmt.Sprintf("unknown mode %q", c.Mode), Expected: "hold, toggle or tap", Fix: "write hold, toggle or tap"}
	}
	switch c.Shape {
	case "", TouchShapeCircle, TouchShapeRect:
	default:
		return &loaderr.Error{Field: field("shape"), Problem: fmt.Sprintf("unknown shape %q", c.Shape), Expected: "circle or rect", Fix: "write circle or rect"}
	}

	// takes lists the kind-specific fields a built-in kind accepts; nil means the kind is not built in.
	var takes map[string]bool
	switch c.Kind {
	case TouchKindButton:
		if c.Action == "" {
			return loaderr.Errorf(field("action"), "name the action the button presses", "a button needs an action")
		}
		takes = map[string]bool{"action": true, "text": true, "icon": true, "mode": true, "shape": true}
	case TouchKindDPad:
		if c.Up == "" && c.Down == "" && c.Left == "" && c.Right == "" && c.AxisX == "" && c.AxisY == "" {
			return loaderr.Errorf(field("up"), "name the actions of its directions (up, down, left, right) or its axes (axisX, axisY)", "a d-pad needs at least one action or axis")
		}
		takes = map[string]bool{"ways": true, "up": true, "down": true, "left": true, "right": true, "axisX": true, "axisY": true, "deadZone": true}
	case TouchKindStick:
		if c.AxisX == "" && c.AxisY == "" {
			return loaderr.Errorf(field("axisX"), "name the axes the stick drives with axisX and axisY", "a stick needs axisX or axisY")
		}
		takes = map[string]bool{"axisX": true, "axisY": true, "deadZone": true, "floating": true, "radius": true}
	}
	if takes == nil {
		return nil
	}
	set := map[string]bool{
		"action": c.Action != "", "text": c.Text != "", "icon": c.Icon != "", "mode": c.Mode != "", "shape": c.Shape != "",
		"ways": c.Ways != 0, "up": c.Up != "", "down": c.Down != "", "left": c.Left != "", "right": c.Right != "",
		"axisX": c.AxisX != "", "axisY": c.AxisY != "", "deadZone": c.DeadZone != 0, "floating": c.Floating, "radius": c.Radius != 0,
		"options": len(c.Options) > 0,
	}
	// Checked in the schema's field order, so the same field is reported first on every run.
	for _, name := range [...]string{"action", "text", "icon", "mode", "shape", "ways", "up", "down", "left", "right", "axisX", "axisY", "deadZone", "floating", "radius", "options"} {
		if set[name] && !takes[name] {
			return loaderr.Errorf(field(name), "remove the field; this kind does not take it", "a %s does not take %s", c.Kind, name)
		}
	}
	return nil
}

func validateTouchSize(s TouchSize, field string) error {
	if !finite(s.Width) || !finite(s.Height) || s.Width <= 0 || s.Height <= 0 {
		return loaderr.Errorf(field, "write a positive width and height", "size %v by %v is not positive", s.Width, s.Height)
	}
	return nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
