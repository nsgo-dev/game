package gameui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// ScreenFormatVersion is the *.screen.yaml schema version this package reads and writes.
const ScreenFormatVersion = 1

// ScreenSchemaURL is where the JSON Schema of version 1 screen documents is published.
// EncodeScreen writes it in a yaml-language-server comment on the first line so editors and AI tools validate and complete screen files as they are written.
const ScreenSchemaURL = "https://game.nsgo.dev/schemas/screen.v1.json"

// ScreenSchemaComment is the first line EncodeScreen writes: a YAML comment pointing a YAML language server at ScreenSchemaURL. ParseScreen ignores it like any other comment.
const ScreenSchemaComment = "# yaml-language-server: $schema=" + ScreenSchemaURL

// ScreenNodeType identifies the widget or container kind of a ScreenNodeDoc.
type ScreenNodeType string

const (
	// NodeVBox stacks children vertically.
	NodeVBox ScreenNodeType = "vbox"
	// NodeHBox arranges children horizontally.
	NodeHBox ScreenNodeType = "hbox"
	// NodeLabel displays localized non-interactive text.
	NodeLabel ScreenNodeType = "label"
	// NodeButton displays a focusable, clickable button.
	NodeButton ScreenNodeType = "button"
	// NodeSpriteRow displays a horizontal row of repeated sprite icons (such as health hearts).
	NodeSpriteRow ScreenNodeType = "sprite_row"
	// NodeColorRect draws a filled rectangle (such as a modal dim backdrop or HUD plate).
	NodeColorRect ScreenNodeType = "color_rect"
	// NodeTouchStick displays an on-screen virtual thumbstick for touch devices.
	NodeTouchStick ScreenNodeType = "touch_stick"
	// NodeTouchButton displays an on-screen circular/rounded touch action button.
	NodeTouchButton ScreenNodeType = "touch_button"
	// NodeContainer groups child nodes with optional device-class filtering or anchored sub-bounds.
	NodeContainer ScreenNodeType = "container"
	// NodeImageProgress displays a read-only progress meter built from layered under/progress/over images with a directional or radial fill.
	NodeImageProgress ScreenNodeType = "image_progress"
)

// ScreenAnchor positions a node against one of nine reference points or fills its parent bounds.
type ScreenAnchor string

const (
	AnchorTopLeft      ScreenAnchor = "top_left"
	AnchorTopCenter    ScreenAnchor = "top_center"
	AnchorTopRight     ScreenAnchor = "top_right"
	AnchorCenterLeft   ScreenAnchor = "center_left"
	AnchorCenter       ScreenAnchor = "center"
	AnchorCenterRight  ScreenAnchor = "center_right"
	AnchorBottomLeft   ScreenAnchor = "bottom_left"
	AnchorBottomCenter ScreenAnchor = "bottom_center"
	AnchorBottomRight  ScreenAnchor = "bottom_right"
	AnchorFill         ScreenAnchor = "fill"
)

// DeviceClass identifies a target device form factor for conditional visibility and reference layout checks.
type DeviceClass string

const (
	DevicePhone     DeviceClass = "phone"
	DeviceTablet    DeviceClass = "tablet"
	DeviceDesktop   DeviceClass = "desktop"
	DeviceUltrawide DeviceClass = "ultrawide"
)

// SizeMode controls whether a node uses a fixed width or sizes horizontally to fit localized text.
type SizeMode string

const (
	// SizeFixed keeps the node's declared Width (the default when omitted).
	SizeFixed SizeMode = "fixed"
	// SizeFitText expands or contracts width to fit the node's localized text plus horizontal padding.
	SizeFitText SizeMode = "fit_text"
)

// ScreenScaleMode selects the Viewport scaling behavior for a screen file.
type ScreenScaleMode string

const (
	// ScreenScaleFit uniformly scales the design canvas to fit inside the window (default).
	ScreenScaleFit ScreenScaleMode = "fit"
	// ScreenScaleFill uniformly scales the design canvas to cover the window.
	ScreenScaleFill ScreenScaleMode = "fill"
	// ScreenScaleStretch scales X and Y independently to fill the window.
	ScreenScaleStretch ScreenScaleMode = "stretch"
	// ScreenScaleInteger uniformly scales by the largest integer factor >= 1 that fits.
	ScreenScaleInteger ScreenScaleMode = "integer"
)

// ScreenDoc is the root of a versioned *.screen.yaml HUD or menu screen definition.
type ScreenDoc struct {
	// Version is the schema version; it must equal ScreenFormatVersion (1).
	Version int `yaml:"version" schema:"required"`
	// UID is an optional stable ID for the screen file, kept across renames by game index and game mv.
	UID string `yaml:"uid,omitempty"`
	// Name is the screen's human-readable name (for example "hud" or "pause").
	Name string `yaml:"name,omitempty"`
	// DesignWidth is the authored reference canvas width in virtual points (for example 640).
	DesignWidth float32 `yaml:"designWidth" schema:"required,exclusiveMinimum=0"`
	// DesignHeight is the authored reference canvas height in virtual points (for example 360).
	DesignHeight float32 `yaml:"designHeight" schema:"required,exclusiveMinimum=0"`
	// ScaleMode controls how the screen scales to different window sizes: fit (default), fill, stretch, or integer.
	ScaleMode ScreenScaleMode `yaml:"scaleMode,omitempty" schema:"enum=fit|fill|stretch|integer"`
	// Nodes are the top-level UI nodes of the screen, rendered in slice order.
	Nodes []ScreenNodeDoc `yaml:"nodes,omitempty"`
}

// ScreenOffsetDoc is an offset from a node's anchor point in virtual points.
// For right and bottom anchors, positive X and Y values move inward away from that edge.
type ScreenOffsetDoc struct {
	// X is the horizontal offset in virtual points.
	X float32 `yaml:"x,omitempty"`
	// Y is the vertical offset in virtual points.
	Y float32 `yaml:"y,omitempty"`
}

// ScreenPaddingDoc holds inner padding in virtual points.
type ScreenPaddingDoc struct {
	// Top is the top inner padding in virtual points.
	Top float32 `yaml:"top,omitempty" schema:"minimum=0"`
	// Right is the right inner padding in virtual points.
	Right float32 `yaml:"right,omitempty" schema:"minimum=0"`
	// Bottom is the bottom inner padding in virtual points.
	Bottom float32 `yaml:"bottom,omitempty" schema:"minimum=0"`
	// Left is the left inner padding in virtual points.
	Left float32 `yaml:"left,omitempty" schema:"minimum=0"`
}

// Edges converts p into core.Edges (returning zero edges when p is nil).
func (p *ScreenPaddingDoc) Edges() core.Edges {
	if p == nil {
		return core.Edges{}
	}
	return core.Edges{
		Top:    core.Points(p.Top),
		Right:  core.Points(p.Right),
		Bottom: core.Points(p.Bottom),
		Left:   core.Points(p.Left),
	}
}

// ScreenNodeDoc is one node in a *.screen.yaml tree.
type ScreenNodeDoc struct {
	// ID uniquely identifies this node within the screen.
	ID string `yaml:"id" schema:"required,minLength=1"`
	// Type selects the widget or container kind.
	Type ScreenNodeType `yaml:"type" schema:"required,enum=vbox|hbox|label|button|sprite_row|color_rect|touch_stick|touch_button|container|image_progress"`
	// Anchor positions the node relative to the screen or safe area: one of the nine anchor points or fill.
	Anchor ScreenAnchor `yaml:"anchor,omitempty" schema:"enum=top_left|top_center|top_right|center_left|center|center_right|bottom_left|bottom_center|bottom_right|fill"`
	// Offset shifts the node from its anchor in virtual points. For right and bottom anchors, positive values inset inward from that edge.
	Offset *ScreenOffsetDoc `yaml:"offset,omitempty"`
	// SafeArea keeps the node inside the display safe area (away from notches and home indicators). Nil defaults to true for non-fill anchors and false for fill.
	SafeArea *bool `yaml:"safeArea,omitempty"`
	// SizeMode selects whether width is fixed ("fixed", the default) or sizes to fit localized text ("fit_text") on label and button nodes.
	SizeMode SizeMode `yaml:"sizeMode,omitempty" schema:"enum=fixed|fit_text"`
	// Width is the node's fixed width in virtual points (0 uses intrinsic or container-determined width).
	Width float32 `yaml:"width,omitempty" schema:"minimum=0"`
	// Height is the node's fixed height in virtual points (0 uses intrinsic or container-determined height).
	Height float32 `yaml:"height,omitempty" schema:"minimum=0"`
	// Padding is the node's inner padding in virtual points (vbox, hbox, label, button).
	Padding *ScreenPaddingDoc `yaml:"padding,omitempty"`
	// Spacing is the gap between children of a vbox, hbox, or sprite_row in virtual points.
	Spacing float32 `yaml:"spacing,omitempty" schema:"minimum=0"`
	// TextKey names a localized string in the project's l10n tables (such as "hud.hint.lever") for label, button, and touch_button nodes. Screen files store localization keys rather than literal text.
	TextKey string `yaml:"textKey,omitempty"`
	// Bind maps supported widget properties to registered game value paths. The
	// binding is declarative metadata consumed by the host or generated UI code.
	Bind map[string]string `yaml:"bind,omitempty"`
	// FocusOrder sets the explicit keyboard/gamepad focus traversal order (1-based; 0 means not explicitly ordered) on button and touch_button nodes.
	FocusOrder int `yaml:"focusOrder,omitempty" schema:"minimum=0"`
	// TouchHitSize is the touch target diameter in virtual points for touch_stick, touch_button, or touch-interactive buttons.
	TouchHitSize float32 `yaml:"touchHitSize,omitempty" schema:"minimum=0"`
	// DeviceClasses optionally restricts the node and its subtree to the listed device classes (for example [phone, tablet]). Empty shows on all devices; nested nodes must overlap their parent's allowed classes.
	DeviceClasses []DeviceClass `yaml:"deviceClasses,omitempty"`
	// Sprite is a project-relative path to a sprite image or atlas file (for example "art/ui/heart.png") for sprite_row nodes, followed by refs and game mv.
	Sprite string `yaml:"sprite,omitempty"`
	// SpriteFrame optionally names a frame inside a sprite atlas when Sprite points to an atlas JSON file on a sprite_row node.
	SpriteFrame string `yaml:"spriteFrame,omitempty"`
	// SpriteCount is the number of repeated icons in a sprite_row (defaults to 1 when Sprite is set).
	SpriteCount int `yaml:"spriteCount,omitempty" schema:"minimum=0"`
	// Under is a project-relative path to the image_progress background layer image, drawn in full first.
	Under string `yaml:"under,omitempty"`
	// Progress is a project-relative path to the image_progress fill layer image, revealed by the value along Fill.
	Progress string `yaml:"progress,omitempty"`
	// Over is a project-relative path to the image_progress foreground layer image, drawn in full last.
	Over string `yaml:"over,omitempty"`
	// Fill picks how an image_progress reveals its progress image: left_to_right (the default), right_to_left, top_to_bottom, bottom_to_top, center_h, center_v, clockwise, or counter_clockwise.
	Fill string `yaml:"fill,omitempty" schema:"enum=left_to_right|right_to_left|top_to_bottom|bottom_to_top|center_h|center_v|clockwise|counter_clockwise"`
	// StartAngle is the image_progress radial fill's starting angle in degrees (0 points right, positive is clockwise).
	StartAngle float32 `yaml:"startAngle,omitempty"`
	// Span is the image_progress radial fill's full sweep in degrees (0 defaults to 360).
	Span float32 `yaml:"span,omitempty" schema:"minimum=0,maximum=360"`
	// Min is the image_progress range's lower bound (defaults to 0).
	Min float32 `yaml:"min,omitempty"`
	// Max is the image_progress range's upper bound (defaults to 1 when both min and max are 0).
	Max float32 `yaml:"max,omitempty"`
	// Value is the image_progress node's initial value; game code updates the widget's Value at runtime.
	Value float32 `yaml:"value,omitempty"`
	// FontSize overrides the text font size in virtual points for label, button, and touch_button nodes.
	FontSize float32 `yaml:"fontSize,omitempty" schema:"minimum=0"`
	// TextColor is an optional CSS color string parsed with core.ParseColor for text (label, button, touch_button) or the thumbstick knob (touch_stick).
	TextColor string `yaml:"textColor,omitempty"`
	// Background is an optional CSS color string parsed with core.ParseColor for the node's fill (all node types except image_progress; icon fallback tint on sprite_row and ring base on touch_stick).
	Background string `yaml:"background,omitempty"`
	// CornerRadius is an optional corner radius in virtual points (vbox, hbox, container, label, button, color_rect).
	CornerRadius float32 `yaml:"cornerRadius,omitempty" schema:"minimum=0"`
	// Children are nested child nodes for container types (vbox, hbox, container).
	Children []ScreenNodeDoc `yaml:"children,omitempty"`
}

// KeepInSafeArea reports whether the node stays inside the display safe area.
// When SafeArea is nil, non-fill anchors default to true and AnchorFill defaults to false.
func (n *ScreenNodeDoc) KeepInSafeArea() bool {
	if n == nil {
		return true
	}
	if n.SafeArea != nil {
		return *n.SafeArea
	}
	return n.Anchor != AnchorFill
}

// EffectiveAnchor returns n.Anchor, defaulting to AnchorTopLeft when empty.
func (n *ScreenNodeDoc) EffectiveAnchor() ScreenAnchor {
	if n == nil || n.Anchor == "" {
		return AnchorTopLeft
	}
	return n.Anchor
}

// EffectiveScaleMode maps doc.ScaleMode to a gameui.ScaleMode, defaulting to ScaleFit.
func (doc *ScreenDoc) EffectiveScaleMode() ScaleMode {
	if doc == nil {
		return ScaleFit
	}
	switch doc.ScaleMode {
	case ScreenScaleFill:
		return ScaleFill
	case ScreenScaleStretch:
		return ScaleStretch
	case ScreenScaleInteger:
		return ScaleInteger
	default:
		return ScaleFit
	}
}

const (
	maxScreenNodeDepth = 64
	fixScreenDoc       = "write the screen file as version: 1 with positive designWidth and designHeight and valid UI nodes"
	fixScreenVersion   = "set version: 1 at the top of the screen file"
	fixScreenYAMLRef   = "expand the value inline; screen files do not allow YAML anchors, aliases, or merge keys"
)

// ParseScreen decodes and validates a versioned *.screen.yaml document.
// Unknown fields, fields unsupported by a node's type, disjoint nested deviceClasses, trailing YAML documents,
// YAML anchors/aliases/merge keys, missing or unsupported versions, duplicate node IDs, duplicate focus orders,
// invalid enums or colors, and non-finite numbers are refused.
//
// Errors are *loaderr.Error values carrying line, column, field path, and fix guidance; callers that read from a file
// can attach the file path with loaderr.WithFile.
func ParseScreen(data []byte) (*ScreenDoc, error) {
	doc, err := parseScreenDoc(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixScreenDoc)
	}
	return doc, nil
}

// LoadScreenFS reads and parses a *.screen.yaml file from fsys, attaching name to any returned error.
func LoadScreenFS(fsys fs.FS, name string) (*ScreenDoc, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("gameui: read screen %s: %w", name, err)
	}
	doc, err := ParseScreen(data)
	if err != nil {
		return nil, loaderr.WithFile(err, name)
	}
	return doc, nil
}

func parseScreenDoc(data []byte) (*ScreenDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("gameui: empty screen document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc ScreenDoc
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("gameui: empty screen document")
		}
		return nil, fmt.Errorf("gameui: decode screen: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("gameui: unexpected trailing YAML document in screen file")
		}
		return nil, fmt.Errorf("gameui: decode screen: %w", err)
	}
	if err := ValidateScreen(&doc); err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("gameui: decode screen: %w", err)
	}
	if err := checkPlainScreenNode(&root, doc.Version); err != nil {
		return nil, err
	}
	return &doc, nil
}

func checkPlainScreenNode(n *yaml.Node, version int) error {
	if err := checkNoScreenYAMLRefs(n); err != nil {
		return err
	}
	doc := n
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		val := doc.Content[i+1]
		if key == "version" {
			if want := strconv.Itoa(version); val.Value != want {
				return loaderr.Errorf("version", "write the version as "+want, "gameui: screen version %q must be written as %s", val.Value, want)
			}
		}
	}
	return nil
}

func checkNoScreenYAMLRefs(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("gameui: screen line %d: YAML aliases are not allowed", n.Line), Fix: fixScreenYAMLRef}
	}
	if n.Anchor != "" {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("gameui: screen line %d: YAML anchors are not allowed", n.Line), Fix: fixScreenYAMLRef}
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return &loaderr.Error{Line: c.Line, Column: c.Column, Problem: fmt.Sprintf("gameui: screen line %d: YAML merge keys are not allowed", c.Line), Fix: fixScreenYAMLRef}
		}
		if err := checkNoScreenYAMLRefs(c); err != nil {
			return err
		}
	}
	return nil
}

// ValidateScreen validates doc against the version 1 *.screen.yaml schema and invariants.
func ValidateScreen(doc *ScreenDoc) error {
	if doc == nil {
		return errors.New("gameui: nil screen document")
	}
	switch doc.Version {
	case ScreenFormatVersion:
	case 0:
		return loaderr.Errorf("version", fixScreenVersion, "gameui: screen is missing version (want %d)", ScreenFormatVersion)
	default:
		return loaderr.Errorf("version", fixScreenVersion, "gameui: unsupported screen version %d (want %d)", doc.Version, ScreenFormatVersion)
	}
	if doc.UID != "" && !validScreenUID(doc.UID) {
		return loaderr.Errorf("uid", "remove the uid or generate one with game index", "gameui: invalid screen uid %q", doc.UID)
	}
	if !isPositiveFinite32(doc.DesignWidth) {
		return loaderr.Errorf("designWidth", "set designWidth to a positive number such as 640", "gameui: designWidth must be positive, got %v", doc.DesignWidth)
	}
	if !isPositiveFinite32(doc.DesignHeight) {
		return loaderr.Errorf("designHeight", "set designHeight to a positive number such as 360", "gameui: designHeight must be positive, got %v", doc.DesignHeight)
	}
	if doc.ScaleMode != "" && !isValidScaleMode(doc.ScaleMode) {
		return loaderr.Errorf("scaleMode", "use one of fit, fill, stretch, or integer", "gameui: unknown scaleMode %q", doc.ScaleMode)
	}

	seenIDs := make(map[string]bool)
	seenFocus := make(map[int]string)
	for i := range doc.Nodes {
		if err := validateScreenNode(&doc.Nodes[i], []any{"nodes", i}, 0, deviceClassFilter{}, seenIDs, seenFocus); err != nil {
			return err
		}
	}
	return nil
}

func validateScreenNode(n *ScreenNodeDoc, path []any, depth int, parentClasses deviceClassFilter, seenIDs map[string]bool, seenFocus map[int]string) error {
	if depth > maxScreenNodeDepth {
		return loaderr.Errorf(loaderr.Path(path...), "flatten the node hierarchy to stay within the maximum nesting depth", "gameui: node nesting exceeds maximum depth %d", maxScreenNodeDepth)
	}
	fieldPath := func(sub ...any) string {
		all := make([]any, 0, len(path)+len(sub))
		all = append(all, path...)
		all = append(all, sub...)
		return loaderr.Path(all...)
	}

	if n.ID == "" || strings.TrimSpace(n.ID) != n.ID || strings.IndexFunc(n.ID, unicode.IsControl) >= 0 {
		return loaderr.Errorf(fieldPath("id"), "give each node a non-empty id without surrounding spaces or control characters", "gameui: invalid node id %q", n.ID)
	}
	if seenIDs[n.ID] {
		return loaderr.Errorf(fieldPath("id"), "give each node in the screen a unique id", "gameui: duplicate node id %q", n.ID)
	}
	seenIDs[n.ID] = true

	if !isValidNodeType(n.Type) {
		return loaderr.Errorf(fieldPath("type"), "use one of vbox, hbox, label, button, sprite_row, color_rect, touch_stick, touch_button, container, or image_progress", "gameui: node %q has unknown type %q", n.ID, n.Type)
	}
	if n.Anchor != "" && !isValidAnchor(n.Anchor) {
		return loaderr.Errorf(fieldPath("anchor"), "use one of top_left, top_center, top_right, center_left, center, center_right, bottom_left, bottom_center, bottom_right, or fill", "gameui: node %q has unknown anchor %q", n.ID, n.Anchor)
	}
	if n.Offset != nil {
		if !isFinite32(n.Offset.X) {
			return loaderr.Errorf(fieldPath("offset", "x"), "set offset.x to a finite number", "gameui: node %q offset.x must be finite", n.ID)
		}
		if !isFinite32(n.Offset.Y) {
			return loaderr.Errorf(fieldPath("offset", "y"), "set offset.y to a finite number", "gameui: node %q offset.y must be finite", n.ID)
		}
	}
	if n.SizeMode != "" {
		if !nodeSupportsSizeMode(n.Type) {
			return loaderr.Errorf(fieldPath("sizeMode"), "remove sizeMode; only label and button nodes support sizeMode", "gameui: node %q of type %q does not support sizeMode", n.ID, n.Type)
		}
		if n.SizeMode != SizeFixed && n.SizeMode != SizeFitText {
			return loaderr.Errorf(fieldPath("sizeMode"), "use fixed or fit_text", "gameui: node %q has unknown sizeMode %q", n.ID, n.SizeMode)
		}
	}
	if !isNonNegFinite32(n.Width) {
		return loaderr.Errorf(fieldPath("width"), "set width to a non-negative number", "gameui: node %q width must be non-negative, got %v", n.ID, n.Width)
	}
	if !isNonNegFinite32(n.Height) {
		return loaderr.Errorf(fieldPath("height"), "set height to a non-negative number", "gameui: node %q height must be non-negative, got %v", n.ID, n.Height)
	}
	if n.Padding != nil {
		if !nodeSupportsPadding(n.Type) {
			return loaderr.Errorf(fieldPath("padding"), "remove padding; only vbox, hbox, label, and button nodes support padding", "gameui: node %q of type %q does not support padding", n.ID, n.Type)
		}
		for _, side := range []struct {
			name string
			val  float32
		}{
			{"top", n.Padding.Top},
			{"right", n.Padding.Right},
			{"bottom", n.Padding.Bottom},
			{"left", n.Padding.Left},
		} {
			if !isNonNegFinite32(side.val) {
				return loaderr.Errorf(fieldPath("padding", side.name), "set padding sides to non-negative numbers", "gameui: node %q padding.%s must be non-negative, got %v", n.ID, side.name, side.val)
			}
		}
	}
	if n.Spacing != 0 && !nodeSupportsSpacing(n.Type) {
		return loaderr.Errorf(fieldPath("spacing"), "remove spacing; only vbox, hbox, and sprite_row nodes support spacing", "gameui: node %q of type %q does not support spacing", n.ID, n.Type)
	}
	if !isNonNegFinite32(n.Spacing) {
		return loaderr.Errorf(fieldPath("spacing"), "set spacing to a non-negative number", "gameui: node %q spacing must be non-negative, got %v", n.ID, n.Spacing)
	}
	if n.TextKey != "" {
		if !nodeSupportsText(n.Type) {
			return loaderr.Errorf(fieldPath("textKey"), "remove textKey; only label, button, and touch_button nodes support textKey", "gameui: node %q of type %q does not support textKey", n.ID, n.Type)
		}
		if strings.TrimSpace(n.TextKey) != n.TextKey || strings.IndexFunc(n.TextKey, unicode.IsControl) >= 0 {
			return loaderr.Errorf(fieldPath("textKey"), "set textKey to a localization key without surrounding spaces or control characters", "gameui: node %q has invalid textKey %q", n.ID, n.TextKey)
		}
	}
	for property, source := range n.Bind {
		if strings.TrimSpace(property) != property || property == "" || strings.IndexFunc(property, unicode.IsControl) >= 0 {
			return loaderr.Errorf(fieldPath("bind", property), "use a non-empty binding property", "gameui: node %q has invalid binding property %q", n.ID, property)
		}
		if strings.TrimSpace(source) != source || source == "" || strings.IndexFunc(source, unicode.IsControl) >= 0 {
			return loaderr.Errorf(fieldPath("bind", property), "set the binding source to a dotted game value path", "gameui: node %q has invalid binding source %q", n.ID, source)
		}
	}
	if n.FocusOrder != 0 && !nodeSupportsFocusOrder(n.Type) {
		return loaderr.Errorf(fieldPath("focusOrder"), "remove focusOrder; only button and touch_button nodes support focusOrder", "gameui: node %q of type %q does not support focusOrder", n.ID, n.Type)
	}
	if n.FocusOrder < 0 {
		return loaderr.Errorf(fieldPath("focusOrder"), "set focusOrder to a positive integer (1, 2, ...) or omit it", "gameui: node %q focusOrder must be non-negative, got %d", n.ID, n.FocusOrder)
	}
	if n.FocusOrder > 0 {
		if prev, dup := seenFocus[n.FocusOrder]; dup {
			return loaderr.Errorf(fieldPath("focusOrder"), "give each focus-ordered node a distinct positive focusOrder", "gameui: node %q repeats focusOrder %d already used by %q", n.ID, n.FocusOrder, prev)
		}
		seenFocus[n.FocusOrder] = n.ID
	}
	if n.TouchHitSize != 0 && !nodeSupportsTouchHitSize(n.Type) {
		return loaderr.Errorf(fieldPath("touchHitSize"), "remove touchHitSize; only button, touch_stick, and touch_button nodes support touchHitSize", "gameui: node %q of type %q does not support touchHitSize", n.ID, n.Type)
	}
	if !isNonNegFinite32(n.TouchHitSize) {
		return loaderr.Errorf(fieldPath("touchHitSize"), "set touchHitSize to a non-negative number (at least 44 pt for touch targets)", "gameui: node %q touchHitSize must be non-negative, got %v", n.ID, n.TouchHitSize)
	}
	seenClasses := make(map[DeviceClass]bool, len(n.DeviceClasses))
	for i, dc := range n.DeviceClasses {
		if !isValidDeviceClass(dc) {
			return loaderr.Errorf(fieldPath("deviceClasses", i), "use phone, tablet, desktop, or ultrawide", "gameui: node %q has unknown deviceClass %q", n.ID, dc)
		}
		if seenClasses[dc] {
			return loaderr.Errorf(fieldPath("deviceClasses", i), "list each deviceClass at most once", "gameui: node %q repeats deviceClass %q", n.ID, dc)
		}
		seenClasses[dc] = true
	}
	effClasses := intersectDeviceClasses(parentClasses, n.DeviceClasses)
	if effClasses.isEmpty() {
		return loaderr.Errorf(
			fieldPath("deviceClasses"),
			"remove deviceClasses or include at least one class allowed by the parent node",
			"gameui: node %q deviceClasses %s do not overlap parent deviceClasses %s",
			n.ID, formatDeviceClasses(n.DeviceClasses), formatDeviceClasses(parentClasses.allowed),
		)
	}
	if n.Sprite != "" {
		if !nodeSupportsSprite(n.Type) {
			return loaderr.Errorf(fieldPath("sprite"), "remove sprite; only sprite_row nodes support sprite", "gameui: node %q of type %q does not support sprite", n.ID, n.Type)
		}
		if strings.TrimSpace(n.Sprite) != n.Sprite || strings.IndexFunc(n.Sprite, unicode.IsControl) >= 0 {
			return loaderr.Errorf(fieldPath("sprite"), "set sprite to a project-relative path such as art/ui/heart.png", "gameui: node %q has invalid sprite path %q", n.ID, n.Sprite)
		}
	}
	if n.SpriteFrame != "" {
		if !nodeSupportsSprite(n.Type) {
			return loaderr.Errorf(fieldPath("spriteFrame"), "remove spriteFrame; only sprite_row nodes support spriteFrame", "gameui: node %q of type %q does not support spriteFrame", n.ID, n.Type)
		}
		if n.Sprite == "" {
			return loaderr.Errorf(fieldPath("spriteFrame"), "set sprite to the atlas path when spriteFrame is specified", "gameui: node %q sets spriteFrame %q without sprite", n.ID, n.SpriteFrame)
		}
	}
	if n.SpriteCount != 0 && !nodeSupportsSprite(n.Type) {
		return loaderr.Errorf(fieldPath("spriteCount"), "remove spriteCount; only sprite_row nodes support spriteCount", "gameui: node %q of type %q does not support spriteCount", n.ID, n.Type)
	}
	if n.SpriteCount < 0 {
		return loaderr.Errorf(fieldPath("spriteCount"), "set spriteCount to a non-negative integer", "gameui: node %q spriteCount must be non-negative, got %d", n.ID, n.SpriteCount)
	}
	for _, layer := range []struct {
		name string
		path string
	}{
		{"under", n.Under},
		{"over", n.Over},
		{"progress", n.Progress},
	} {
		if layer.path == "" {
			continue
		}
		if !nodeSupportsImageProgress(n.Type) {
			return loaderr.Errorf(fieldPath(layer.name), fmt.Sprintf("remove %s; only image_progress nodes support %s", layer.name, layer.name), "gameui: node %q of type %q does not support %s", n.ID, n.Type, layer.name)
		}
		if strings.TrimSpace(layer.path) != layer.path || strings.IndexFunc(layer.path, unicode.IsControl) >= 0 {
			return loaderr.Errorf(fieldPath(layer.name), "set %s to a project-relative path such as art/ui/bar_fill.png", "gameui: node %q has invalid %s path %q", n.ID, layer.name, layer.path)
		}
	}
	if n.Fill != "" {
		if !nodeSupportsImageProgress(n.Type) {
			return loaderr.Errorf(fieldPath("fill"), "remove fill; only image_progress nodes support fill", "gameui: node %q of type %q does not support fill", n.ID, n.Type)
		}
		if _, ok := ParseFillMode(n.Fill); !ok {
			return loaderr.Errorf(fieldPath("fill"), "use one of left_to_right, right_to_left, top_to_bottom, bottom_to_top, center_h, center_v, clockwise, or counter_clockwise", "gameui: node %q has unknown fill %q", n.ID, n.Fill)
		}
	}
	if n.StartAngle != 0 && !nodeSupportsImageProgress(n.Type) {
		return loaderr.Errorf(fieldPath("startAngle"), "remove startAngle; only image_progress nodes support startAngle", "gameui: node %q of type %q does not support startAngle", n.ID, n.Type)
	}
	if !isFinite32(n.StartAngle) {
		return loaderr.Errorf(fieldPath("startAngle"), "set startAngle to a finite angle in degrees", "gameui: node %q startAngle must be finite, got %v", n.ID, n.StartAngle)
	}
	if n.Span != 0 && !nodeSupportsImageProgress(n.Type) {
		return loaderr.Errorf(fieldPath("span"), "remove span; only image_progress nodes support span", "gameui: node %q of type %q does not support span", n.ID, n.Type)
	}
	if !isNonNegFinite32(n.Span) || n.Span > 360 {
		return loaderr.Errorf(fieldPath("span"), "set span to a sweep between 0 (the default 360) and 360 degrees", "gameui: node %q span must be in [0, 360], got %v", n.ID, n.Span)
	}
	if n.Min != 0 && !nodeSupportsImageProgress(n.Type) {
		return loaderr.Errorf(fieldPath("min"), "remove min; only image_progress nodes support min", "gameui: node %q of type %q does not support min", n.ID, n.Type)
	}
	if !isFinite32(n.Min) {
		return loaderr.Errorf(fieldPath("min"), "set min to a finite number", "gameui: node %q min must be finite, got %v", n.ID, n.Min)
	}
	if n.Max != 0 && !nodeSupportsImageProgress(n.Type) {
		return loaderr.Errorf(fieldPath("max"), "remove max; only image_progress nodes support max", "gameui: node %q of type %q does not support max", n.ID, n.Type)
	}
	if !isFinite32(n.Max) {
		return loaderr.Errorf(fieldPath("max"), "set max to a finite number", "gameui: node %q max must be finite, got %v", n.ID, n.Max)
	}
	if n.Value != 0 && !nodeSupportsImageProgress(n.Type) {
		return loaderr.Errorf(fieldPath("value"), "remove value; only image_progress nodes support value", "gameui: node %q of type %q does not support value", n.ID, n.Type)
	}
	if !isFinite32(n.Value) {
		return loaderr.Errorf(fieldPath("value"), "set value to a finite number", "gameui: node %q value must be finite, got %v", n.ID, n.Value)
	}
	if n.FontSize != 0 && !nodeSupportsText(n.Type) {
		return loaderr.Errorf(fieldPath("fontSize"), "remove fontSize; only label, button, and touch_button nodes support fontSize", "gameui: node %q of type %q does not support fontSize", n.ID, n.Type)
	}
	if !isNonNegFinite32(n.FontSize) {
		return loaderr.Errorf(fieldPath("fontSize"), "set fontSize to a non-negative number", "gameui: node %q fontSize must be non-negative, got %v", n.ID, n.FontSize)
	}
	if n.TextColor != "" {
		if !nodeSupportsTextColor(n.Type) {
			return loaderr.Errorf(fieldPath("textColor"), "remove textColor; only label, button, touch_button, and touch_stick nodes support textColor", "gameui: node %q of type %q does not support textColor", n.ID, n.Type)
		}
		if _, err := core.ParseColor(n.TextColor); err != nil {
			return loaderr.Errorf(fieldPath("textColor"), "write textColor as a CSS color such as #ffffff or rgba(255,255,255,0.9)", "gameui: node %q invalid textColor %q: %v", n.ID, n.TextColor, err)
		}
	}
	if n.Background != "" {
		if !nodeSupportsBackground(n.Type) {
			return loaderr.Errorf(fieldPath("background"), "remove background; image_progress uses under, progress, and over image layers instead of background", "gameui: node %q of type %q does not support background", n.ID, n.Type)
		}
		if _, err := core.ParseColor(n.Background); err != nil {
			return loaderr.Errorf(fieldPath("background"), "write background as a CSS color such as #101216 or rgba(16,18,22,0.78)", "gameui: node %q invalid background %q: %v", n.ID, n.Background, err)
		}
	}
	if n.CornerRadius != 0 && !nodeSupportsCornerRadius(n.Type) {
		return loaderr.Errorf(fieldPath("cornerRadius"), "remove cornerRadius; only vbox, hbox, container, label, button, and color_rect nodes support cornerRadius", "gameui: node %q of type %q does not support cornerRadius", n.ID, n.Type)
	}
	if !isNonNegFinite32(n.CornerRadius) {
		return loaderr.Errorf(fieldPath("cornerRadius"), "set cornerRadius to a non-negative number", "gameui: node %q cornerRadius must be non-negative, got %v", n.ID, n.CornerRadius)
	}
	if len(n.Children) > 0 && !n.Type.IsContainer() {
		return loaderr.Errorf(fieldPath("children"), "only vbox, hbox, and container nodes may have children", "gameui: node %q of type %q cannot have children", n.ID, n.Type)
	}
	for i := range n.Children {
		childPath := make([]any, 0, len(path)+2)
		childPath = append(childPath, path...)
		childPath = append(childPath, "children", i)
		if err := validateScreenNode(&n.Children[i], childPath, depth+1, effClasses, seenIDs, seenFocus); err != nil {
			return err
		}
	}
	return nil
}

func nodeSupportsSizeMode(t ScreenNodeType) bool {
	return t == NodeLabel || t == NodeButton
}

func nodeSupportsPadding(t ScreenNodeType) bool {
	return t == NodeVBox || t == NodeHBox || t == NodeLabel || t == NodeButton
}

func nodeSupportsSpacing(t ScreenNodeType) bool {
	return t == NodeVBox || t == NodeHBox || t == NodeSpriteRow
}

func nodeSupportsText(t ScreenNodeType) bool {
	return t == NodeLabel || t == NodeButton || t == NodeTouchButton
}

func nodeSupportsTextColor(t ScreenNodeType) bool {
	return t == NodeLabel || t == NodeButton || t == NodeTouchButton || t == NodeTouchStick
}

func nodeSupportsFocusOrder(t ScreenNodeType) bool {
	return isFocusableKind(t)
}

func nodeSupportsTouchHitSize(t ScreenNodeType) bool {
	return t == NodeButton || t == NodeTouchStick || t == NodeTouchButton
}

func nodeSupportsSprite(t ScreenNodeType) bool {
	return t == NodeSpriteRow
}

func nodeSupportsImageProgress(t ScreenNodeType) bool {
	return t == NodeImageProgress
}

func nodeSupportsBackground(t ScreenNodeType) bool {
	return t != NodeImageProgress
}

func nodeSupportsCornerRadius(t ScreenNodeType) bool {
	return t == NodeVBox || t == NodeHBox || t == NodeContainer || t == NodeLabel || t == NodeButton || t == NodeColorRect
}

func formatDeviceClasses(classes []DeviceClass) string {
	parts := make([]string, len(classes))
	for i, c := range classes {
		parts[i] = string(c)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// IsContainer reports whether t can hold child nodes.
func (t ScreenNodeType) IsContainer() bool {
	return t == NodeVBox || t == NodeHBox || t == NodeContainer
}

func isValidNodeType(t ScreenNodeType) bool {
	switch t {
	case NodeVBox, NodeHBox, NodeLabel, NodeButton, NodeSpriteRow, NodeColorRect, NodeTouchStick, NodeTouchButton, NodeContainer, NodeImageProgress:
		return true
	default:
		return false
	}
}

func isValidAnchor(a ScreenAnchor) bool {
	switch a {
	case AnchorTopLeft, AnchorTopCenter, AnchorTopRight,
		AnchorCenterLeft, AnchorCenter, AnchorCenterRight,
		AnchorBottomLeft, AnchorBottomCenter, AnchorBottomRight,
		AnchorFill:
		return true
	default:
		return false
	}
}

func isValidDeviceClass(c DeviceClass) bool {
	switch c {
	case DevicePhone, DeviceTablet, DeviceDesktop, DeviceUltrawide:
		return true
	default:
		return false
	}
}

func deviceClassRank(c DeviceClass) int {
	switch c {
	case DevicePhone:
		return 0
	case DeviceTablet:
		return 1
	case DeviceDesktop:
		return 2
	case DeviceUltrawide:
		return 3
	default:
		return 4
	}
}

func isValidScaleMode(m ScreenScaleMode) bool {
	switch m {
	case ScreenScaleFit, ScreenScaleFill, ScreenScaleStretch, ScreenScaleInteger:
		return true
	default:
		return false
	}
}

func isFinite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

func isNonNegFinite32(v float32) bool {
	return isFinite32(v) && v >= 0
}

func isPositiveFinite32(v float32) bool {
	return isFinite32(v) && v > 0
}

// validScreenUID matches resource.ValidUID (13 lowercase Crockford base-32 chars) without importing package resource, which imports gameui.
func validScreenUID(s string) bool {
	if len(s) != 13 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z' || c == 'i' || c == 'l' || c == 'o' || c == 'u') {
			return false
		}
	}
	return true
}

// Clone returns a deep copy of doc.
func (doc *ScreenDoc) Clone() *ScreenDoc {
	if doc == nil {
		return nil
	}
	out := *doc
	if len(doc.Nodes) > 0 {
		out.Nodes = make([]ScreenNodeDoc, len(doc.Nodes))
		for i := range doc.Nodes {
			out.Nodes[i] = cloneScreenNode(doc.Nodes[i])
		}
	}
	return &out
}

func cloneScreenNode(n ScreenNodeDoc) ScreenNodeDoc {
	out := n
	if n.Offset != nil {
		off := *n.Offset
		out.Offset = &off
	}
	if n.SafeArea != nil {
		v := *n.SafeArea
		out.SafeArea = &v
	}
	if n.Padding != nil {
		pad := *n.Padding
		out.Padding = &pad
	}
	if len(n.DeviceClasses) > 0 {
		out.DeviceClasses = slices.Clone(n.DeviceClasses)
	}
	if len(n.Bind) > 0 {
		out.Bind = maps.Clone(n.Bind)
	}
	if len(n.Children) > 0 {
		out.Children = make([]ScreenNodeDoc, len(n.Children))
		for i := range n.Children {
			out.Children[i] = cloneScreenNode(n.Children[i])
		}
	}
	return out
}

func canonicalizeScreenNode(n ScreenNodeDoc) ScreenNodeDoc {
	out := cloneScreenNode(n)
	if out.Offset != nil && *out.Offset == (ScreenOffsetDoc{}) {
		out.Offset = nil
	}
	if out.Padding != nil && *out.Padding == (ScreenPaddingDoc{}) {
		out.Padding = nil
	}
	if len(out.DeviceClasses) > 1 {
		slices.SortFunc(out.DeviceClasses, func(a, b DeviceClass) int {
			return deviceClassRank(a) - deviceClassRank(b)
		})
	}
	for i := range out.Children {
		out.Children[i] = canonicalizeScreenNode(out.Children[i])
	}
	return out
}

// EncodeScreen validates doc and writes it as deterministic 2-space YAML prefixed with ScreenSchemaComment.
// A document EncodeScreen writes loads with ParseScreen and saves back byte-for-byte unchanged.
func EncodeScreen(doc *ScreenDoc) ([]byte, error) {
	if err := ValidateScreen(doc); err != nil {
		return nil, err
	}
	out := doc.Clone()
	for i := range out.Nodes {
		out.Nodes[i] = canonicalizeScreenNode(out.Nodes[i])
	}

	var buf bytes.Buffer
	buf.WriteString(ScreenSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("gameui: encode screen: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("gameui: encode screen: %w", err)
	}
	return buf.Bytes(), nil
}
