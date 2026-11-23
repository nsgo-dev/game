package framedata

import (
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math2d"
)

// Fighter is one character in a World: where it stands, which way it faces and which move of its Doc it is playing on which frame.
//
// A Fighter plays its Neutral move when it has no other, so it always has hurtboxes to be hit on. Start begins a move when the fighter can act or when a cancel window of its current move allows it; Force begins one regardless.
type Fighter struct {
	// Moves is the fighter's frame data. It should have passed Validate, as Parse ensures: a hitbox whose Group is outside 0 to MaxGroup never hits.
	Moves *Doc
	// Neutral names the looping move the fighter returns to when a move that does not loop ends or when it is hit, such as "idle". Empty, or a name Moves does not have, leaves the fighter with no move and so no boxes.
	Neutral string
	// X and Y place the fighter's origin in world points.
	X, Y float32
	// FacingLeft mirrors the fighter's boxes and knockback about its origin.
	FacingLeft bool
	// Team keeps fighters from hitting each other: two fighters with the same non-zero Team never hit. Fighters on team 0 can hit everyone.
	Team int
	// Blocking turns hits on the fighter into blocks: blockstun instead of hitstun. The game sets it each step from input when the fighter may block.
	Blocking bool

	move    *Move
	frame   int
	fresh   bool
	stun    int
	hit     bool
	blocked bool
	landed  []landing
}

// landing records the hit groups of the current move instance that have already struck one opponent.
type landing struct {
	victim *Fighter
	groups uint64
}

// Move returns the move the fighter is playing, or nil when it has none.
func (f *Fighter) Move() *Move { return f.move }

// Frame returns the frame of the current move the fighter shows, from 1, or 0 when it has no move.
func (f *Fighter) Frame() int {
	if f.move == nil {
		return 0
	}
	return f.frame
}

// Stun returns how many more steps the fighter stays in hitstun or blockstun.
func (f *Fighter) Stun() int { return f.stun }

// Contact reports whether the current move instance has hit an opponent and whether it has been blocked.
func (f *Fighter) Contact() (hit, blocked bool) { return f.hit, f.blocked }

// Actionable reports whether the fighter may start any move: it is not stunned and it has no move, a looping move or a move on its last frame.
func (f *Fighter) Actionable() bool {
	if f.stun > 0 {
		return false
	}
	return f.move == nil || f.move.Loop || f.frame >= f.move.Length
}

// CanStart reports whether Start(name) would begin the move: the move exists and the fighter is actionable, or a cancel window of its current move open on this frame lists the move.
func (f *Fighter) CanStart(name string) bool {
	if _, ok := f.Moves.Move(name); !ok || f.stun > 0 {
		return false
	}
	if f.Actionable() {
		return true
	}
	for i := range f.move.Cancels {
		c := &f.move.Cancels[i]
		if f.frame < c.Start || f.frame > c.End || !f.met(c.When) {
			continue
		}
		if slices.Contains(c.Into, name) {
			return true
		}
	}
	return false
}

func (f *Fighter) met(on Condition) bool {
	switch on {
	case OnHit:
		return f.hit
	case OnBlock:
		return f.blocked
	case OnContact:
		return f.hit || f.blocked
	default:
		return true
	}
}

// Start begins the move named name on its first frame, which the next World.Step resolves, when CanStart allows it, and reports whether it did.
func (f *Fighter) Start(name string) bool {
	if !f.CanStart(name) {
		return false
	}
	m, _ := f.Moves.Move(name)
	f.begin(m, true)
	return true
}

// Force begins the move named name on its first frame regardless of stun and cancel windows, for hit reactions, throws and scripted moves. It returns an error when Moves has no such move.
func (f *Fighter) Force(name string) error {
	m, ok := f.Moves.Move(name)
	if !ok {
		return fmt.Errorf("framedata: no move %q", name)
	}
	f.begin(m, true)
	return nil
}

func (f *Fighter) begin(m *Move, fresh bool) {
	f.move, f.frame, f.fresh = m, 1, fresh
	f.hit, f.blocked = false, false
	f.landed = f.landed[:0]
}

func (f *Fighter) neutral(fresh bool) {
	if m, ok := f.Moves.Move(f.Neutral); ok {
		f.begin(m, fresh)
		return
	}
	f.move, f.frame, f.fresh = nil, 0, false
	f.hit, f.blocked = false, false
	f.landed = f.landed[:0]
}

// advance moves the fighter on one frame at the start of a step.
func (f *Fighter) advance() {
	if f.stun > 0 {
		f.stun--
	}
	switch {
	case f.move == nil:
		f.neutral(false)
	case f.fresh:
		f.fresh = false
	case f.frame < f.move.Length:
		f.frame++
	case f.move.Loop:
		f.begin(f.move, false)
	default:
		f.neutral(false)
	}
}

// place returns b placed in world points, mirrored about the fighter's origin when it faces left.
func (f *Fighter) place(b *Box) math2d.Rect {
	x := f.X + b.X
	if f.FacingLeft {
		x = f.X - b.X - b.Width
	}
	return math2d.Rect{X: x, Y: f.Y + b.Y, W: b.Width, H: b.Height}
}

// WorldBox is one active box of a fighter placed in world points.
type WorldBox struct {
	// Kind is KindHurt or KindHit.
	Kind Kind
	// Rect is the box in world points, mirrored when the fighter faces left.
	Rect math2d.Rect
	// Index is the box's index in the current move's Boxes.
	Index int
}

// Boxes appends the boxes of the current move active on the fighter's current frame to dst, in document order, and returns the extended slice.
func (f *Fighter) Boxes(dst []WorldBox) []WorldBox {
	if f.move == nil {
		return dst
	}
	for i := range f.move.Boxes {
		b := &f.move.Boxes[i]
		if b.Active(f.frame) {
			dst = append(dst, WorldBox{Kind: b.Kind, Rect: f.place(b), Index: i})
		}
	}
	return dst
}

func (f *Fighter) landedOn(v *Fighter, group int) bool {
	for _, l := range f.landed {
		if l.victim == v {
			return l.groups&(1<<group) != 0
		}
	}
	return false
}

func (f *Fighter) markLanded(v *Fighter, group int) {
	for i := range f.landed {
		if f.landed[i].victim == v {
			f.landed[i].groups |= 1 << group
			return
		}
	}
	f.landed = append(f.landed, landing{victim: v, groups: 1 << group})
}

// Hit is one hitbox connecting with an opponent during a World.Step.
type Hit struct {
	// Attacker and Victim are the fighters that struck and were struck.
	Attacker, Victim *Fighter
	// Move is the attacker's move and Frame the frame of it that connected.
	Move  *Move
	Frame int
	// Box is the index of the connecting hitbox in Move.Boxes, and Group its hit group.
	Box   int
	Group int
	// Blocked reports whether the victim was blocking.
	Blocked bool
	// Damage is the hitbox's damage, dealt in full on a hit and left to the game to scale on a block.
	Damage int
	// Stun is the frames of stun the victim took: the hitbox's hitstun, or its blockstun when Blocked.
	Stun int
	// KnockbackX and KnockbackY are the hitbox's knockback in world space, with X pointing away from the attacker: toward the side of the attacker's origin the victim's origin is on, or the attacker's facing when both share an X.
	KnockbackX, KnockbackY float32
	// X and Y are the center of the overlap of the hitbox and the hurtbox it struck, where a game draws a hit spark.
	X, Y float32
}

// World resolves the hits between the fighters of a match, one fixed step at a time.
//
// A World is not safe for concurrent use. A Fighter belongs to at most one World.
type World struct {
	// Fighters are the fighters of the match. Their order is the order hits are found and reported in.
	Fighters []*Fighter

	hits []Hit
}

// Step advances every fighter one frame, finds every hitbox that overlaps an opponent's hurtbox on that frame, and applies the hits: the attacker's contact opens its cancel windows, and the victim takes hitstun or blockstun and returns to its Neutral move.
//
// Hits are found from the fighters' positions and moves before any hit of the step is applied, so fighters that strike each other on the same frame trade. A move instance hits each opponent once per hit group and at most once per step. A victim struck by several hits in one step takes the longest stun.
//
// The returned hits are ordered by attacker, then victim, in Fighters order, and stay valid until the next Step.
func (w *World) Step() []Hit {
	w.hits = w.hits[:0]
	for _, f := range w.Fighters {
		if f != nil {
			f.advance()
		}
	}
	for _, a := range w.Fighters {
		if a == nil || a.move == nil {
			continue
		}
		for _, v := range w.Fighters {
			if v == nil || v == a || v.move == nil || (a.Team != 0 && a.Team == v.Team) {
				continue
			}
			if h, ok := strike(a, v); ok {
				w.hits = append(w.hits, h)
			}
		}
	}
	for i := range w.hits {
		h := &w.hits[i]
		h.Attacker.markLanded(h.Victim, h.Group)
		if h.Blocked {
			h.Attacker.blocked = true
		} else {
			h.Attacker.hit = true
		}
	}
	// A hit replaces the stun its victim had left, and the longest of a step's hits on one victim wins.
	for i := range w.hits {
		w.hits[i].Victim.stun = 0
	}
	for i := range w.hits {
		h := &w.hits[i]
		v := h.Victim
		v.stun = max(v.stun, h.Stun)
		v.neutral(true)
	}
	return w.hits
}

// strike finds the first hitbox of a's current frame, in document order, whose group has not yet struck v and which overlaps one of v's active hurtboxes.
func strike(a, v *Fighter) (Hit, bool) {
	am, vm := a.move, v.move
	for i := range am.Boxes {
		hb := &am.Boxes[i]
		if hb.Kind != KindHit || hb.Group < 0 || hb.Group > MaxGroup || !hb.Active(a.frame) || a.landedOn(v, hb.Group) {
			continue
		}
		hr := a.place(hb)
		for j := range vm.Boxes {
			ub := &vm.Boxes[j]
			if ub.Kind != KindHurt || !ub.Active(v.frame) {
				continue
			}
			overlap, ok := hr.Intersection(v.place(ub))
			if !ok {
				continue
			}
			h := Hit{
				Attacker:   a,
				Victim:     v,
				Move:       am,
				Frame:      a.frame,
				Box:        i,
				Group:      hb.Group,
				Blocked:    v.Blocking,
				Damage:     hb.Damage,
				Stun:       hb.Hitstun,
				KnockbackX: hb.KnockbackX,
				KnockbackY: hb.KnockbackY,
				X:          overlap.X + overlap.W/2,
				Y:          overlap.Y + overlap.H/2,
			}
			if h.Blocked {
				h.Stun = hb.Blockstun
			}
			if v.X < a.X || (v.X == a.X && a.FacingLeft) {
				h.KnockbackX = -h.KnockbackX
			}
			return h, true
		}
	}
	return Hit{}, false
}
