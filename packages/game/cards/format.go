package cards

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"go.yaml.in/yaml/v3"
)

// FormatVersion is the BoardDoc schema version this package reads and writes.
const FormatVersion = 1

// BoardDoc is a board's slots written as YAML: a schema version, the card size and each slot's area, layout and rules.
//
//	version: 1
//	card_size: {w: 63, h: 88}
//	slots:
//	  - id: hand
//	    rect: {x: 40, y: 400, w: 560, h: 120}
//	    layout:
//	      fan: {arc: 24, sag: 14}
//	  - id: discard
//	    rect: {x: 360, y: 200, w: 63, h: 88}
//	    layout:
//	      stack: {offset: {x: 0, y: -0.5}, depth: 6}
//	    accepts: [red, black]
//	    grab: top
//
// ParseBoardDoc refuses unknown fields, and EncodeBoardDoc writes the fields in a fixed order, so a document read and written again is unchanged.
type BoardDoc struct {
	// Version must equal FormatVersion.
	Version int `yaml:"version"`
	// CardSize is every card's width and height; both must be positive.
	CardSize SizeDoc `yaml:"card_size"`
	// Slots are the board's slots, bottom first; their IDs must be unique.
	Slots []SlotDoc `yaml:"slots,omitempty"`
}

// SizeDoc is a width and a height in board coordinates.
type SizeDoc struct {
	W float32 `yaml:"w"`
	H float32 `yaml:"h"`
}

// VecDoc is a 2D offset in board coordinates.
type VecDoc struct {
	X float32 `yaml:"x"`
	Y float32 `yaml:"y"`
}

// RectDoc is a rectangle in board coordinates, its origin at the top-left corner.
type RectDoc struct {
	X float32 `yaml:"x"`
	Y float32 `yaml:"y"`
	W float32 `yaml:"w"`
	H float32 `yaml:"h"`
}

// SlotDoc is one Slot: Grab is "any" (or omitted), "top" or "none".
type SlotDoc struct {
	ID       string    `yaml:"id"`
	Rect     RectDoc   `yaml:"rect"`
	Layout   LayoutDoc `yaml:"layout"`
	Accepts  []string  `yaml:"accepts,omitempty"`
	Capacity int       `yaml:"capacity,omitempty"`
	Grab     string    `yaml:"grab,omitempty"`
}

// LayoutDoc names a slot's layout: exactly one of its fields is set.
type LayoutDoc struct {
	Fan   *FanDoc   `yaml:"fan,omitempty"`
	Stack *StackDoc `yaml:"stack,omitempty"`
	Grid  *GridDoc  `yaml:"grid,omitempty"`
}

// FanDoc is a Fan; Arc is in degrees.
type FanDoc struct {
	Spacing float32 `yaml:"spacing,omitempty"`
	Arc     float32 `yaml:"arc,omitempty"`
	Sag     float32 `yaml:"sag,omitempty"`
}

// StackDoc is a Stack.
type StackDoc struct {
	Offset VecDoc `yaml:"offset,omitempty"`
	Depth  int    `yaml:"depth,omitempty"`
}

// GridDoc is a Grid.
type GridDoc struct {
	Columns int    `yaml:"columns,omitempty"`
	Gap     VecDoc `yaml:"gap,omitempty"`
}

// ErrCustomLayout reports encoding a slot whose Layout is not a Fan, Stack or Grid.
var ErrCustomLayout = errors.New("cards: a custom layout cannot be written to a BoardDoc")

// ParseBoardDoc decodes one YAML BoardDoc and validates it. Unknown fields and trailing documents are refused.
func ParseBoardDoc(data []byte) (*BoardDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("cards: empty board document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc BoardDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("cards: decode board: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("cards: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("cards: decode board: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// EncodeBoardDoc validates doc and writes it as 2-space YAML with its fields in a fixed order.
func EncodeBoardDoc(doc *BoardDoc) ([]byte, error) {
	if doc == nil {
		return nil, errors.New("cards: nil board document")
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("cards: encode board: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("cards: encode board: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks doc against the version 1 schema: positive finite sizes, finite coordinates, unique non-empty slot IDs, exactly one layout per slot with non-negative counts and a Fan arc under 360 degrees, unique non-empty tags, a non-negative capacity and a known grab rule.
func (doc *BoardDoc) Validate() error {
	if doc.Version != FormatVersion {
		return fmt.Errorf("cards: unsupported board version %d (want %d)", doc.Version, FormatVersion)
	}
	if !positive(doc.CardSize.W) || !positive(doc.CardSize.H) {
		return fmt.Errorf("cards: card_size must be positive and finite, got %gx%g", doc.CardSize.W, doc.CardSize.H)
	}
	seen := make(map[string]bool, len(doc.Slots))
	for i := range doc.Slots {
		s := &doc.Slots[i]
		if s.ID == "" {
			return fmt.Errorf("cards: slots[%d] has no id", i)
		}
		if seen[s.ID] {
			return fmt.Errorf("cards: duplicate slot id %q", s.ID)
		}
		seen[s.ID] = true
		if err := s.validate(); err != nil {
			return fmt.Errorf("cards: slot %q: %w", s.ID, err)
		}
	}
	return nil
}

func (s *SlotDoc) validate() error {
	r := s.Rect
	if !finite(r.X) || !finite(r.Y) || !positive(r.W) || !positive(r.H) {
		return fmt.Errorf("rect must have finite coordinates and a positive size, got %+v", r)
	}
	if _, err := s.Layout.layout(); err != nil {
		return err
	}
	tags := make(map[string]bool, len(s.Accepts))
	for _, t := range s.Accepts {
		if t == "" || tags[t] {
			return fmt.Errorf("accepts has an empty or repeated tag %q", t)
		}
		tags[t] = true
	}
	if s.Capacity < 0 {
		return fmt.Errorf("capacity %d is negative", s.Capacity)
	}
	if _, err := parseGrab(s.Grab); err != nil {
		return err
	}
	return nil
}

func (l LayoutDoc) layout() (Layout, error) {
	set := 0
	for _, ok := range []bool{l.Fan != nil, l.Stack != nil, l.Grid != nil} {
		if ok {
			set++
		}
	}
	if set != 1 {
		return nil, fmt.Errorf("layout must name exactly one of fan, stack or grid, got %d", set)
	}
	switch {
	case l.Fan != nil:
		f := l.Fan
		if !finite(f.Spacing) || f.Spacing < 0 || !finite(f.Arc) || f.Arc < 0 || f.Arc >= 360 || !finite(f.Sag) {
			return nil, fmt.Errorf("fan needs a finite non-negative spacing, an arc in [0, 360) and a finite sag, got %+v", *f)
		}
		return Fan{Spacing: f.Spacing, Arc: f.Arc, Sag: f.Sag}, nil
	case l.Stack != nil:
		s := l.Stack
		if !finite(s.Offset.X) || !finite(s.Offset.Y) || s.Depth < 0 {
			return nil, fmt.Errorf("stack needs a finite offset and a non-negative depth, got %+v", *s)
		}
		return Stack{Offset: math2d.Vec2(s.Offset), Depth: s.Depth}, nil
	default:
		g := l.Grid
		if g.Columns < 0 || !finite(g.Gap.X) || !finite(g.Gap.Y) || g.Gap.X < 0 || g.Gap.Y < 0 {
			return nil, fmt.Errorf("grid needs non-negative columns and a finite non-negative gap, got %+v", *g)
		}
		return Grid{Columns: g.Columns, Gap: math2d.Vec2(g.Gap)}, nil
	}
}

func parseGrab(s string) (GrabRule, error) {
	switch s {
	case "", "any":
		return GrabAny, nil
	case "top":
		return GrabTop, nil
	case "none":
		return GrabNone, nil
	}
	return 0, fmt.Errorf("unknown grab rule %q (want any, top or none)", s)
}

// LoadBoard validates doc and returns a Board with its card size and its slots, empty and in order. The board's other fields are left for the game to set.
func LoadBoard(doc *BoardDoc) (*Board, error) {
	if doc == nil {
		return nil, errors.New("cards: nil board document")
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	b := &Board{CardSize: math2d.Vec2{X: doc.CardSize.W, Y: doc.CardSize.H}}
	for _, sd := range doc.Slots {
		layout, _ := sd.Layout.layout()
		grab, _ := parseGrab(sd.Grab)
		s := &Slot{
			ID:       sd.ID,
			Rect:     math2d.Rect{X: sd.Rect.X, Y: sd.Rect.Y, W: sd.Rect.W, H: sd.Rect.H},
			Layout:   layout,
			Accepts:  append([]string(nil), sd.Accepts...),
			Capacity: sd.Capacity,
			Grab:     grab,
		}
		if err := b.AddSlot(s); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// Doc returns the board's card size and slots as a BoardDoc, writing the default grab rule by omitting it. A slot's Accept function and the board's callbacks and tuning are code and are not written; a slot whose Layout is not a Fan, Stack or Grid (or a pointer to one) fails with ErrCustomLayout.
func (b *Board) Doc() (*BoardDoc, error) {
	size := b.Size()
	doc := &BoardDoc{Version: FormatVersion, CardSize: SizeDoc{W: size.X, H: size.Y}}
	for _, s := range b.slots {
		var ld LayoutDoc
		switch l := s.Layout.(type) {
		case nil:
			ld.Stack = &StackDoc{}
		case Fan:
			ld.Fan = &FanDoc{Spacing: l.Spacing, Arc: l.Arc, Sag: l.Sag}
		case *Fan:
			if l == nil {
				return nil, fmt.Errorf("%w: slot %q has a nil *Fan", ErrCustomLayout, s.ID)
			}
			ld.Fan = &FanDoc{Spacing: l.Spacing, Arc: l.Arc, Sag: l.Sag}
		case Stack:
			ld.Stack = &StackDoc{Offset: VecDoc(l.Offset), Depth: l.Depth}
		case *Stack:
			if l == nil {
				return nil, fmt.Errorf("%w: slot %q has a nil *Stack", ErrCustomLayout, s.ID)
			}
			ld.Stack = &StackDoc{Offset: VecDoc(l.Offset), Depth: l.Depth}
		case Grid:
			ld.Grid = &GridDoc{Columns: l.Columns, Gap: VecDoc(l.Gap)}
		case *Grid:
			if l == nil {
				return nil, fmt.Errorf("%w: slot %q has a nil *Grid", ErrCustomLayout, s.ID)
			}
			ld.Grid = &GridDoc{Columns: l.Columns, Gap: VecDoc(l.Gap)}
		default:
			return nil, fmt.Errorf("%w: slot %q", ErrCustomLayout, s.ID)
		}
		sd := SlotDoc{
			ID:       s.ID,
			Rect:     RectDoc{X: s.Rect.X, Y: s.Rect.Y, W: s.Rect.W, H: s.Rect.H},
			Layout:   ld,
			Accepts:  append([]string(nil), s.Accepts...),
			Capacity: s.Capacity,
		}
		if s.Grab != GrabAny {
			sd.Grab = s.Grab.String()
		}
		doc.Slots = append(doc.Slots, sd)
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return doc, nil
}

func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

func positive(v float32) bool { return finite(v) && v > 0 }
