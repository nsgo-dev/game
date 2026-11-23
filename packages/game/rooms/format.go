package rooms

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// FormatVersion is the room map schema version this package reads and writes.
const FormatVersion = 1

// Default transition lengths, used when a slide or fade leaves Duration at 0.
const (
	// DefaultSlideDuration is how long a slide lasts when its Duration is 0.
	DefaultSlideDuration = 400 * time.Millisecond
	// DefaultFadeDuration is how long a fade lasts, out and back in, when its Duration is 0.
	DefaultFadeDuration = 600 * time.Millisecond
)

// ErrInvalidMap reports that a room map breaks the schema: a wrong version, no rooms, a missing or duplicate ID, an empty or non-finite rectangle, or a malformed transition.
var ErrInvalidMap = errors.New("rooms: invalid map")

// Kind is how the camera moves into a room.
type Kind string

const (
	// KindCut snaps the camera into the new room at once.
	KindCut Kind = "cut"
	// KindSlide pans the camera into the new room over the transition's duration.
	KindSlide Kind = "slide"
	// KindFade covers the view with a color, cuts to the new room at the midpoint and uncovers it.
	KindFade Kind = "fade"
)

// Valid reports whether k is KindCut, KindSlide or KindFade.
func (k Kind) Valid() bool {
	switch k {
	case KindCut, KindSlide, KindFade:
		return true
	}
	return false
}

// Easing names one of NSGo's standard easing curves in a room map.
type Easing string

const (
	// EasingLinear is core.Linear.
	EasingLinear Easing = "linear"
	// EasingEase is core.Ease.
	EasingEase Easing = "ease"
	// EasingEaseIn is core.EaseIn.
	EasingEaseIn Easing = "easeIn"
	// EasingEaseOut is core.EaseOut.
	EasingEaseOut Easing = "easeOut"
	// EasingEaseInOut is core.EaseInOut.
	EasingEaseInOut Easing = "easeInOut"
)

// Curve returns the core.Curve e names, and false when e is not one of the Easing constants.
func (e Easing) Curve() (core.Curve, bool) {
	switch e {
	case EasingLinear:
		return core.Linear, true
	case EasingEase:
		return core.Ease, true
	case EasingEaseIn:
		return core.EaseIn, true
	case EasingEaseOut:
		return core.EaseOut, true
	case EasingEaseInOut:
		return core.EaseInOut, true
	}
	return core.Curve{}, false
}

// Duration is a time.Duration written in YAML as a Go duration string with a unit, such as "400ms" or "1.5s".
type Duration time.Duration

// MarshalYAML writes d as its time.Duration string.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// UnmarshalYAML reads a duration string with a unit; a bare number is refused so its unit is never guessed.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return fmt.Errorf("line %d: duration must be a string with a unit, such as 400ms", n.Line)
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %v", n.Line, err)
	}
	*d = Duration(v)
	return nil
}

// Transition is how the camera moves into a room.
type Transition struct {
	// Kind is cut, slide or fade.
	Kind Kind `yaml:"kind"`
	// Duration is how long a slide or fade lasts; 0 uses DefaultSlideDuration or DefaultFadeDuration. A cut must leave it 0.
	Duration Duration `yaml:"duration,omitempty"`
	// Easing eases a slide's pan or a fade's cover; empty uses easeInOut for a slide and linear for a fade. A cut must leave it empty.
	Easing Easing `yaml:"easing,omitempty"`
	// Color is a fade's cover color as a CSS color, such as "#000" or "rgb(20 10 40)"; empty is black. Only a fade may set it.
	Color string `yaml:"color,omitempty"`
}

// Length returns how long t lasts: Duration, or the kind's default when Duration is 0. A cut lasts 0.
func (t Transition) Length() time.Duration {
	switch {
	case t.Kind == KindCut || t.Kind == "":
		return 0
	case t.Duration > 0:
		return time.Duration(t.Duration)
	case t.Kind == KindFade:
		return DefaultFadeDuration
	default:
		return DefaultSlideDuration
	}
}

// Curve returns t's easing curve: the one Easing names, or core.EaseInOut for a slide and core.Linear otherwise.
func (t Transition) Curve() core.Curve {
	if c, ok := t.Easing.Curve(); ok {
		return c
	}
	if t.Kind == KindSlide {
		return core.EaseInOut
	}
	return core.Linear
}

// FadeColor returns the color a fade covers the view with: Color parsed as a CSS color, or opaque black when Color is empty or malformed.
func (t Transition) FadeColor() core.Color {
	if t.Color != "" {
		if c, err := core.ParseColor(t.Color); err == nil {
			return c
		}
	}
	return core.RGB(0x000000)
}

// Map is the root of a versioned YAML room map.
type Map struct {
	// Version is the schema version; it must equal FormatVersion.
	Version int `yaml:"version"`
	// Transition is played when entering a room that sets none; nil cuts.
	Transition *Transition `yaml:"transition,omitempty"`
	// Rooms lists the rooms. Where rooms overlap, a target entering the overlap from outside both belongs to the first one listed.
	Rooms []Room `yaml:"rooms"`
}

// Room is one rectangle of the world with its own camera bounds.
type Room struct {
	// ID uniquely identifies the room within the map and in saves.
	ID string `yaml:"id"`
	// Name is the room's display name or a localization key; the package does not interpret it.
	Name string `yaml:"name,omitempty"`
	// X is the room's left edge in world points.
	X float32 `yaml:"x"`
	// Y is the room's top edge in world points.
	Y float32 `yaml:"y"`
	// Width is the room's width in world points; it must be positive.
	Width float32 `yaml:"width"`
	// Height is the room's height in world points; it must be positive.
	Height float32 `yaml:"height"`
	// Transition is played when entering this room, overriding the map's.
	Transition *Transition `yaml:"transition,omitempty"`
}

// Bounds returns r's rectangle as core.Bounds, ready for tilemap.Camera.Bounds.
func (r *Room) Bounds() core.Bounds {
	return core.Bounds{
		Origin: core.Point{X: core.Points(r.X), Y: core.Points(r.Y)},
		Size:   core.Size{Width: core.Points(r.Width), Height: core.Points(r.Height)},
	}
}

// Contains reports whether p lies in r, including its left and top edges and excluding its right and bottom ones, so rooms that share an edge never both hold a point on it.
func (r *Room) Contains(p core.Point) bool {
	x, y := float64(p.X), float64(p.Y)
	return x >= float64(r.X) && x < float64(r.X)+float64(r.Width) &&
		y >= float64(r.Y) && y < float64(r.Y)+float64(r.Height)
}

// Room returns the room with the given ID, or nil when m has none.
func (m *Map) Room(id string) *Room {
	if m == nil {
		return nil
	}
	for i := range m.Rooms {
		if m.Rooms[i].ID == id {
			return &m.Rooms[i]
		}
	}
	return nil
}

// RoomAt returns the first room in list order that contains p, or nil when none does.
func (m *Map) RoomAt(p core.Point) *Room {
	if m == nil {
		return nil
	}
	for i := range m.Rooms {
		if m.Rooms[i].Contains(p) {
			return &m.Rooms[i]
		}
	}
	return nil
}

// TransitionInto returns the transition played when entering r: r's own, else the map's, else a cut.
func (m *Map) TransitionInto(r *Room) Transition {
	switch {
	case r != nil && r.Transition != nil:
		return *r.Transition
	case m != nil && m.Transition != nil:
		return *m.Transition
	default:
		return Transition{Kind: KindCut}
	}
}

// Parse decodes a YAML room map and validates it. Unknown fields and trailing YAML documents are refused.
func Parse(data []byte) (*Map, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("%w: empty document", ErrInvalidMap)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m Map
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrInvalidMap, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: unexpected trailing YAML document", ErrInvalidMap)
		}
		return nil, fmt.Errorf("%w: decode: %v", ErrInvalidMap, err)
	}
	if err := Validate(&m); err != nil {
		return nil, err
	}
	for i := range m.Rooms {
		positiveZero(&m.Rooms[i])
	}
	return &m, nil
}

// LoadFS reads and parses the room map at name in fsys.
func LoadFS(fsys fs.FS, name string) (*Map, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("rooms: read %s: %w", name, err)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return m, nil
}

// Encode validates m and writes it as deterministic YAML with 2-space indentation, fields in declaration order and no generated values, so Parse(Encode(m)) equals m.
func Encode(m *Map) ([]byte, error) {
	if err := Validate(m); err != nil {
		return nil, err
	}
	// Negative zero would be written as -0, which YAML reads back as 0, so it is written as 0 for the output to stay the same across a load and save.
	out := *m
	out.Rooms = slices.Clone(m.Rooms)
	for i := range out.Rooms {
		positiveZero(&out.Rooms[i])
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("rooms: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("rooms: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks m against the schema: the version, at least one room, unique non-empty IDs, finite rectangles with positive sizes, and well-formed transitions. Errors wrap ErrInvalidMap.
func Validate(m *Map) error {
	if m == nil {
		return fmt.Errorf("%w: nil map", ErrInvalidMap)
	}
	if m.Version != FormatVersion {
		return fmt.Errorf("%w: unsupported version %d (want %d)", ErrInvalidMap, m.Version, FormatVersion)
	}
	if m.Transition != nil {
		if err := validTransition(*m.Transition); err != nil {
			return fmt.Errorf("%w: map transition: %v", ErrInvalidMap, err)
		}
	}
	if len(m.Rooms) == 0 {
		return fmt.Errorf("%w: no rooms", ErrInvalidMap)
	}
	seen := make(map[string]struct{}, len(m.Rooms))
	for i := range m.Rooms {
		r := &m.Rooms[i]
		if err := validID(r.ID); err != nil {
			return fmt.Errorf("%w: room %d id: %v", ErrInvalidMap, i, err)
		}
		if _, dup := seen[r.ID]; dup {
			return fmt.Errorf("%w: duplicate room id %q", ErrInvalidMap, r.ID)
		}
		seen[r.ID] = struct{}{}
		if err := validRect(r.X, r.Y, r.Width, r.Height); err != nil {
			return fmt.Errorf("%w: room %q: %v", ErrInvalidMap, r.ID, err)
		}
		if r.Transition != nil {
			if err := validTransition(*r.Transition); err != nil {
				return fmt.Errorf("%w: room %q transition: %v", ErrInvalidMap, r.ID, err)
			}
		}
	}
	return nil
}

func validID(id string) error {
	if id == "" {
		return errors.New("empty")
	}
	if strings.TrimSpace(id) != id {
		return fmt.Errorf("%q has leading or trailing space", id)
	}
	return nil
}

// positiveZero turns a negative zero in r's rectangle into zero.
func positiveZero(r *Room) {
	for _, v := range []*float32{&r.X, &r.Y, &r.Width, &r.Height} {
		if *v == 0 {
			*v = 0
		}
	}
}

func finite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

func validRect(x, y, w, h float32) error {
	if !finite(x) || !finite(y) || !finite(w) || !finite(h) {
		return fmt.Errorf("non-finite rectangle (%v, %v, %v, %v)", x, y, w, h)
	}
	if w <= 0 || h <= 0 {
		return fmt.Errorf("size %vx%v must be positive", w, h)
	}
	if !finite(x+w) || !finite(y+h) {
		return fmt.Errorf("rectangle (%v, %v, %v, %v) overflows", x, y, w, h)
	}
	return nil
}

func validTransition(t Transition) error {
	if !t.Kind.Valid() {
		return fmt.Errorf("unknown kind %q (want cut, slide or fade)", t.Kind)
	}
	if t.Duration < 0 {
		return fmt.Errorf("negative duration %s", time.Duration(t.Duration))
	}
	if t.Easing != "" {
		if _, ok := t.Easing.Curve(); !ok {
			return fmt.Errorf("unknown easing %q (want linear, ease, easeIn, easeOut or easeInOut)", t.Easing)
		}
	}
	if t.Kind == KindCut && (t.Duration != 0 || t.Easing != "") {
		return errors.New("a cut takes no duration or easing")
	}
	if t.Color != "" {
		if t.Kind != KindFade {
			return fmt.Errorf("only a fade takes a color, not a %s", t.Kind)
		}
		if _, err := core.ParseColor(t.Color); err != nil {
			return fmt.Errorf("color %q: %v", t.Color, err)
		}
	}
	return nil
}
