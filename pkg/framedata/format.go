package framedata

import (
	"bytes"
	"fmt"
	"io"
	"math"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

const fixFrameData = "write the frame data file as version: 1 YAML with valid moves, boxes and cancels"

// CurrentVersion is the frame data schema version this package reads and writes.
const CurrentVersion = 1

// MaxLength is the most frames a move may last: a minute at 60 steps a second.
const MaxLength = 3600

// MaxGroup is the highest hit group a hitbox may use; groups run from 0 to MaxGroup.
const MaxGroup = 63

// Kind says whether a box can be hit or strikes.
type Kind string

const (
	// KindHurt is a hurtbox: where the fighter can be hit.
	KindHurt Kind = "hurt"
	// KindHit is a hitbox: where the fighter strikes.
	KindHit Kind = "hit"
)

// Condition says when a cancel window is open.
type Condition string

const (
	// OnAny opens the window whether or not the move connected. It is the empty string, the default.
	OnAny Condition = ""
	// OnHit opens the window only after the move hit an opponent that was not blocking.
	OnHit Condition = "hit"
	// OnBlock opens the window only after the move was blocked.
	OnBlock Condition = "block"
	// OnContact opens the window after the move hit or was blocked.
	OnContact Condition = "contact"
)

// Doc is the root of a versioned YAML frame data document: one character's moves.
type Doc struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version"`
	// Moves are the character's moves. Names are unique.
	Moves []Move `yaml:"moves,omitempty"`
}

// Move is one animation's frame data: how long it lasts, its boxes on each frame and where it may be cancelled.
type Move struct {
	// Name identifies the move within its Doc.
	Name string `yaml:"name"`
	// Animation optionally names the sprite animation the game plays alongside the move, such as an entry of a sprite.Atlas's Animations.
	Animation string `yaml:"animation,omitempty"`
	// Length is how many frames the move lasts, from 1 to MaxLength.
	Length int `yaml:"length"`
	// Loop starts the move over after its last frame instead of ending it, for stances such as idle, walk and crouch.
	Loop bool `yaml:"loop,omitempty"`
	// Boxes are the move's hurtboxes and hitboxes, each active on a range of frames. Their order is the order hits are tested in.
	Boxes []Box `yaml:"boxes,omitempty"`
	// Cancels are the windows where the move may be cut short into other moves.
	Cancels []Cancel `yaml:"cancels,omitempty"`
}

// Box is a rectangle active on the frames Start to End inclusive, relative to the fighter's origin when it faces right.
type Box struct {
	// Kind is KindHurt or KindHit.
	Kind Kind `yaml:"kind"`
	// Start and End are the first and last frames the box is active on, with 1 <= Start <= End <= the move's Length.
	Start int `yaml:"start"`
	End   int `yaml:"end"`
	// X and Y are the box's top-left corner in points from the fighter's origin; Y grows downward.
	X float32 `yaml:"x,omitempty"`
	Y float32 `yaml:"y,omitempty"`
	// Width and Height are the box's size in points; both must be positive.
	Width  float32 `yaml:"width"`
	Height float32 `yaml:"height"`
	// Group is a hitbox's hit group, from 0 to MaxGroup. A move instance hits each opponent once per group, so hitboxes of one hit share a group and each hit of a multi-hit move has its own. Hurtboxes leave it 0.
	Group int `yaml:"group,omitempty"`
	// Damage is how much damage a hitbox deals. Hurtboxes leave it 0.
	Damage int `yaml:"damage,omitempty"`
	// Hitstun is how many frames an opponent hit by the hitbox cannot act. Hurtboxes leave it 0.
	Hitstun int `yaml:"hitstun,omitempty"`
	// Blockstun is how many frames an opponent that blocks the hitbox cannot act. Hurtboxes leave it 0.
	Blockstun int `yaml:"blockstun,omitempty"`
	// KnockbackX and KnockbackY push an opponent hit by the hitbox, in points per frame; positive X is away from the attacker. Hurtboxes leave them 0.
	KnockbackX float32 `yaml:"knockbackX,omitempty"`
	KnockbackY float32 `yaml:"knockbackY,omitempty"`
}

// Active reports whether the box is active on frame.
func (b Box) Active(frame int) bool { return frame >= b.Start && frame <= b.End }

// Cancel is a window of frames, Start to End inclusive, in which a move may be cut short into the moves named in Into.
type Cancel struct {
	// Start and End are the first and last frames of the window, with 1 <= Start <= End <= the move's Length.
	Start int `yaml:"start"`
	End   int `yaml:"end"`
	// When is when the window is open: OnAny (the default), OnHit, OnBlock or OnContact.
	When Condition `yaml:"when,omitempty"`
	// Into names the moves the window may cancel into; each must be a move of the same Doc.
	Into []string `yaml:"into,flow"`
}

// FrameData summarizes a move the way fighting game frame data tables do.
type FrameData struct {
	// Startup is the first frame a hitbox is active on.
	Startup int
	// Active is how many frames pass from the first frame a hitbox is active on to the last, inclusive.
	Active int
	// Recovery is how many frames the move lasts after its last active frame.
	Recovery int
	// OnHit and OnBlock are the attacker's frame advantage when the earliest hitbox connects on its first frame and the opponent is hit or blocks: positive means the attacker can act first.
	OnHit   int
	OnBlock int
}

// FrameData returns the move's startup, active and recovery frames and its advantage, and false when the move has no hitbox.
func (m *Move) FrameData() (FrameData, bool) {
	first, last := 0, 0
	var earliest *Box
	for i := range m.Boxes {
		b := &m.Boxes[i]
		if b.Kind != KindHit {
			continue
		}
		if earliest == nil || b.Start < first {
			first, earliest = b.Start, b
		}
		last = max(last, b.End)
	}
	if earliest == nil {
		return FrameData{}, false
	}
	// The attacker can act Length-Start frames after connecting on frame Start, the opponent after its stun.
	left := m.Length - earliest.Start
	return FrameData{
		Startup:  first,
		Active:   last - first + 1,
		Recovery: m.Length - last,
		OnHit:    earliest.Hitstun - left,
		OnBlock:  earliest.Blockstun - left,
	}, true
}

// Move returns the move named name and true, or nil and false when the document has none.
func (d *Doc) Move(name string) (*Move, bool) {
	if d == nil {
		return nil, false
	}
	for i := range d.Moves {
		if d.Moves[i].Name == name {
			return &d.Moves[i], true
		}
	}
	return nil, false
}

// Parse decodes a YAML frame data document and validates it. Unknown fields and trailing YAML documents are refused.
func Parse(data []byte) (*Doc, error) {
	doc, err := parseDoc(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixFrameData)
	}
	return doc, nil
}

func parseDoc(data []byte) (*Doc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixFrameData, "framedata: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc Doc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("framedata: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in the frame data file; remove the trailing --- document", "framedata: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("framedata: decode: %w", err)
	}
	if err := Validate(&doc); err != nil {
		return nil, err
	}
	// An empty list and an absent one mean the same and Encode omits both, so keep them identical in memory for a lossless round trip.
	if len(doc.Moves) == 0 {
		doc.Moves = nil
	}
	for i := range doc.Moves {
		m := &doc.Moves[i]
		if len(m.Boxes) == 0 {
			m.Boxes = nil
		}
		if len(m.Cancels) == 0 {
			m.Cancels = nil
		}
	}
	yamlzero.Normalize(&doc)
	return &doc, nil
}

// Encode validates doc and writes it as deterministic 2-space YAML that Parse reads back unchanged.
func Encode(doc *Doc) ([]byte, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(doc)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("framedata: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("framedata: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks that doc follows the version 1 schema: unique move names, lengths from 1 to MaxLength, boxes and cancel windows inside their moves, positive finite box sizes, hit properties only on hitboxes, and cancels into moves that exist.
func Validate(doc *Doc) error {
	if doc == nil {
		return loaderr.Errorf("", fixFrameData, "framedata: nil document")
	}
	if doc.Version != CurrentVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", CurrentVersion), "framedata: unsupported version %d (want %d)", doc.Version, CurrentVersion)
	}
	names := make(map[string]struct{}, len(doc.Moves))
	for i := range doc.Moves {
		m := &doc.Moves[i]
		if m.Name == "" {
			return loaderr.Errorf(loaderr.Path("moves", i, "name"), "give the move a non-empty name", "framedata: move %d has an empty name", i)
		}
		if _, dup := names[m.Name]; dup {
			return loaderr.Errorf(loaderr.Path("moves", i, "name"), "give each move a unique name", "framedata: duplicate move name %q", m.Name)
		}
		names[m.Name] = struct{}{}
	}
	for i := range doc.Moves {
		if err := validateMove(i, &doc.Moves[i], names); err != nil {
			return err
		}
	}
	return nil
}

func validateMove(mi int, m *Move, names map[string]struct{}) error {
	mPath := loaderr.Path("moves", mi)
	if m.Length < 1 || m.Length > MaxLength {
		return loaderr.Errorf(loaderr.Join(mPath, "length"), fmt.Sprintf("set length to an integer from 1 to %d", MaxLength), "framedata: move %q length %d must be from 1 to %d", m.Name, m.Length, MaxLength)
	}
	for i := range m.Boxes {
		if err := validateBox(mPath, m, i); err != nil {
			return err
		}
	}
	for i := range m.Cancels {
		c := &m.Cancels[i]
		cPath := loaderr.Join(mPath, "cancels", i)
		where := fmt.Sprintf("framedata: move %q cancel %d", m.Name, i)
		if c.Start < 1 || c.End < c.Start || c.End > m.Length {
			field := "start"
			if c.Start >= 1 {
				field = "end"
			}
			return loaderr.Errorf(loaderr.Join(cPath, field), fmt.Sprintf("set start and end so 1 <= start <= end <= %d", m.Length), "%s frames %d to %d must satisfy 1 <= start <= end <= %d", where, c.Start, c.End, m.Length)
		}
		switch c.When {
		case OnAny, OnHit, OnBlock, OnContact:
		default:
			return loaderr.Errorf(loaderr.Join(cPath, "when"), fmt.Sprintf("set when to %q, %q or %q, or omit it for any", OnHit, OnBlock, OnContact), "%s has unknown condition %q (want %q, %q or %q)", where, c.When, OnHit, OnBlock, OnContact)
		}
		if len(c.Into) == 0 {
			return loaderr.Errorf(loaderr.Join(cPath, "into"), "list at least one target move name under into", "%s names no moves to cancel into", where)
		}
		seen := make(map[string]struct{}, len(c.Into))
		for j, name := range c.Into {
			if _, ok := names[name]; !ok {
				return loaderr.Errorf(loaderr.Join(cPath, "into", j), "name a move declared in this document", "%s cancels into unknown move %q", where, name)
			}
			if _, dup := seen[name]; dup {
				return loaderr.Errorf(loaderr.Join(cPath, "into", j), "remove the duplicate target move name", "%s names move %q twice", where, name)
			}
			seen[name] = struct{}{}
		}
	}
	return nil
}

func validateBox(mPath string, m *Move, i int) error {
	b := &m.Boxes[i]
	bPath := loaderr.Join(mPath, "boxes", i)
	where := fmt.Sprintf("framedata: move %q box %d", m.Name, i)
	switch b.Kind {
	case KindHurt, KindHit:
	default:
		return loaderr.Errorf(loaderr.Join(bPath, "kind"), fmt.Sprintf("set kind to %q or %q", KindHurt, KindHit), "%s has unknown kind %q (want %q or %q)", where, b.Kind, KindHurt, KindHit)
	}
	if b.Start < 1 || b.End < b.Start || b.End > m.Length {
		field := "start"
		if b.Start >= 1 {
			field = "end"
		}
		return loaderr.Errorf(loaderr.Join(bPath, field), fmt.Sprintf("set start and end so 1 <= start <= end <= %d", m.Length), "%s frames %d to %d must satisfy 1 <= start <= end <= %d", where, b.Start, b.End, m.Length)
	}
	if !finite(b.X) || !finite(b.Y) || !finite(b.Width) || !finite(b.Height) {
		return loaderr.Errorf(bPath, "set x, y, width and height to finite numbers", "%s position and size must be finite", where)
	}
	if b.Width <= 0 || b.Height <= 0 {
		field := "width"
		if b.Width > 0 {
			field = "height"
		}
		return loaderr.Errorf(loaderr.Join(bPath, field), "set width and height to positive numbers", "%s size (%v, %v) must be positive", where, b.Width, b.Height)
	}
	if !finite(b.KnockbackX) || !finite(b.KnockbackY) {
		field := "knockbackX"
		if finite(b.KnockbackX) {
			field = "knockbackY"
		}
		return loaderr.Errorf(loaderr.Join(bPath, field), "set knockbackX and knockbackY to finite numbers", "%s knockback must be finite", where)
	}
	if b.Kind == KindHurt {
		if b.Group != 0 || b.Damage != 0 || b.Hitstun != 0 || b.Blockstun != 0 || b.KnockbackX != 0 || b.KnockbackY != 0 {
			return loaderr.Errorf(bPath, "remove group, damage, hitstun, blockstun and knockback from a hurtbox, or change kind to hit", "%s is a hurtbox and cannot set group, damage, hitstun, blockstun or knockback", where)
		}
		return nil
	}
	if b.Group < 0 || b.Group > MaxGroup {
		return loaderr.Errorf(loaderr.Join(bPath, "group"), fmt.Sprintf("set group to an integer from 0 to %d", MaxGroup), "%s group %d must be from 0 to %d", where, b.Group, MaxGroup)
	}
	if b.Damage < 0 || b.Hitstun < 0 || b.Blockstun < 0 {
		field := "damage"
		if b.Damage >= 0 {
			if b.Hitstun < 0 {
				field = "hitstun"
			} else {
				field = "blockstun"
			}
		}
		return loaderr.Errorf(loaderr.Join(bPath, field), "set damage, hitstun and blockstun to non-negative integers", "%s damage, hitstun and blockstun must not be negative", where)
	}
	if b.Hitstun > MaxLength || b.Blockstun > MaxLength {
		field := "hitstun"
		if b.Hitstun <= MaxLength {
			field = "blockstun"
		}
		return loaderr.Errorf(loaderr.Join(bPath, field), fmt.Sprintf("set hitstun and blockstun to at most %d", MaxLength), "%s hitstun and blockstun must not exceed %d frames", where, MaxLength)
	}
	return nil
}

func finite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
