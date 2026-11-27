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

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

const (
	fixMap      = "write the room map as version: 1 YAML with at least one valid room"
	fixReadMap  = "check that the room map file exists and is readable"
	fixDuration = `write a Go duration string with a unit, such as "400ms" or "1.5s"`
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
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: fmt.Sprintf("%v: duration must be a string with a unit, such as 400ms", ErrInvalidMap),
			Fix:     fixDuration,
			Err:     ErrInvalidMap,
		}
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: fmt.Sprintf("%v: %v", ErrInvalidMap, err),
			Fix:     fixDuration,
			Err:     ErrInvalidMap,
		}
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
//
// Errors are *loaderr.Error values wrapping ErrInvalidMap with line, field and fix; callers that read from a file name it with loaderr.WithFile.
func Parse(data []byte) (*Map, error) {
	m, err := parseMap(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixMap)
	}
	return m, nil
}

func parseMap(data []byte) (*Map, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixMap, "%w: empty document", ErrInvalidMap)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m Map
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, ErrInvalidMap) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: decode: %w", ErrInvalidMap, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in the room map; remove the trailing --- document", "%w: unexpected trailing YAML document", ErrInvalidMap)
		}
		return nil, fmt.Errorf("%w: decode: %w", ErrInvalidMap, err)
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
		return nil, loaderr.Locate(fmt.Errorf("rooms: read %s: %w", name, err), name, nil, fixReadMap)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, loaderr.WithFile(err, name)
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
		return loaderr.Errorf("", fixMap, "%w: nil map", ErrInvalidMap)
	}
	if m.Version != FormatVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", FormatVersion), "%w: unsupported version %d (want %d)", ErrInvalidMap, m.Version, FormatVersion)
	}
	if m.Transition != nil {
		if err := validTransition("transition", "map transition", *m.Transition); err != nil {
			return err
		}
	}
	if len(m.Rooms) == 0 {
		return loaderr.Errorf("rooms", "add at least one room under rooms", "%w: no rooms", ErrInvalidMap)
	}
	seen := make(map[string]struct{}, len(m.Rooms))
	for i := range m.Rooms {
		r := &m.Rooms[i]
		rPath := loaderr.Path("rooms", i)
		if err := validID(r.ID); err != nil {
			return loaderr.Errorf(loaderr.Join(rPath, "id"), "give the room a non-empty id without leading or trailing spaces", "%w: room %d id: %v", ErrInvalidMap, i, err)
		}
		if _, dup := seen[r.ID]; dup {
			return loaderr.Errorf(loaderr.Join(rPath, "id"), "give each room a unique id", "%w: duplicate room id %q", ErrInvalidMap, r.ID)
		}
		seen[r.ID] = struct{}{}
		if err := validRectAt(rPath, r.ID, r.X, r.Y, r.Width, r.Height); err != nil {
			return err
		}
		if r.Transition != nil {
			if err := validTransition(loaderr.Join(rPath, "transition"), fmt.Sprintf("room %q transition", r.ID), *r.Transition); err != nil {
				return err
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

func validRectAt(rPath, id string, x, y, w, h float32) error {
	if !finite(x) {
		return loaderr.Errorf(loaderr.Join(rPath, "x"), "set x to a finite number", "%w: room %q: non-finite rectangle (%v, %v, %v, %v)", ErrInvalidMap, id, x, y, w, h)
	}
	if !finite(y) {
		return loaderr.Errorf(loaderr.Join(rPath, "y"), "set y to a finite number", "%w: room %q: non-finite rectangle (%v, %v, %v, %v)", ErrInvalidMap, id, x, y, w, h)
	}
	if !finite(w) {
		return loaderr.Errorf(loaderr.Join(rPath, "width"), "set width to a positive finite number", "%w: room %q: non-finite rectangle (%v, %v, %v, %v)", ErrInvalidMap, id, x, y, w, h)
	}
	if !finite(h) {
		return loaderr.Errorf(loaderr.Join(rPath, "height"), "set height to a positive finite number", "%w: room %q: non-finite rectangle (%v, %v, %v, %v)", ErrInvalidMap, id, x, y, w, h)
	}
	if w <= 0 {
		return loaderr.Errorf(loaderr.Join(rPath, "width"), "set width to a positive number", "%w: room %q: size %vx%v must be positive", ErrInvalidMap, id, w, h)
	}
	if h <= 0 {
		return loaderr.Errorf(loaderr.Join(rPath, "height"), "set height to a positive number", "%w: room %q: size %vx%v must be positive", ErrInvalidMap, id, w, h)
	}
	if !finite(x+w) || !finite(y+h) {
		return loaderr.Errorf(rPath, "keep x + width and y + height within float32 range", "%w: room %q: rectangle (%v, %v, %v, %v) overflows", ErrInvalidMap, id, x, y, w, h)
	}
	return nil
}

func validTransition(tPath, where string, t Transition) error {
	if !t.Kind.Valid() {
		return loaderr.Errorf(loaderr.Join(tPath, "kind"), "set kind to cut, slide or fade", "%w: %s: unknown kind %q (want cut, slide or fade)", ErrInvalidMap, where, t.Kind)
	}
	if t.Duration < 0 {
		return loaderr.Errorf(loaderr.Join(tPath, "duration"), "set duration to a non-negative duration such as 400ms", "%w: %s: negative duration %s", ErrInvalidMap, where, time.Duration(t.Duration))
	}
	if t.Easing != "" {
		if _, ok := t.Easing.Curve(); !ok {
			return loaderr.Errorf(loaderr.Join(tPath, "easing"), "set easing to linear, ease, easeIn, easeOut or easeInOut", "%w: %s: unknown easing %q (want linear, ease, easeIn, easeOut or easeInOut)", ErrInvalidMap, where, t.Easing)
		}
	}
	if t.Kind == KindCut && (t.Duration != 0 || t.Easing != "") {
		field := "duration"
		if t.Duration == 0 {
			field = "easing"
		}
		return loaderr.Errorf(loaderr.Join(tPath, field), "remove duration and easing from a cut transition, or change kind to slide or fade", "%w: %s: a cut takes no duration or easing", ErrInvalidMap, where)
	}
	if t.Color != "" {
		if t.Kind != KindFade {
			return loaderr.Errorf(loaderr.Join(tPath, "color"), "remove color or change kind to fade", "%w: %s: only a fade takes a color, not a %s", ErrInvalidMap, where, t.Kind)
		}
		if _, err := core.ParseColor(t.Color); err != nil {
			return loaderr.Errorf(loaderr.Join(tPath, "color"), `set color to a CSS color such as "#000" or "rgb(20 10 40)"`, "%w: %s: color %q: %v", ErrInvalidMap, where, t.Color, err)
		}
	}
	return nil
}
