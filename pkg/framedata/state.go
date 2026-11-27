package framedata

import "fmt"

// WorldState is a copy of everything World.Step reads and changes in a World's fighters, for replay and rollback net code to save a match and restore it later.
//
// It names moves by name and fighters by their index in World.Fighters, so it holds no pointers and a game can encode it, compare it or hash it for a checksum. The fighters' configuration (Moves, Neutral and Team) is not part of it: a restore expects the World to have the same fighters with the same configuration.
type WorldState struct {
	// Fighters holds one state per entry of World.Fighters, in the same order. A nil fighter has a zero state.
	Fighters []FighterState
}

// FighterState is the state of one Fighter in a WorldState.
type FighterState struct {
	// X, Y, FacingLeft and Blocking are the fighter's fields of the same names.
	X, Y       float32
	FacingLeft bool
	Blocking   bool
	// Move names the move the fighter plays, or is empty when it has none.
	Move string
	// Frame is the frame of Move the fighter shows, from 1, or 0 when it has no move.
	Frame int
	// Fresh reports that Move began after the last Step advanced the fighter, by Start, Force or a hit, so the next Step resolves its first frame instead of advancing it.
	Fresh bool
	// Stun is the steps of hitstun or blockstun left.
	Stun int
	// Hit and Blocked are the current move instance's contact, as Fighter.Contact reports it.
	Hit, Blocked bool
	// Landed lists, per opponent, the hit groups the current move instance has already struck it with.
	Landed []Landed
}

// Landed records the hit groups a move instance has struck one opponent with.
type Landed struct {
	// Victim is the opponent's index in World.Fighters.
	Victim int
	// Groups has bit g set when hit group g has struck the opponent.
	Groups uint64
}

// Save copies the state of every fighter into dst, reusing dst's slices so that saving on every step allocates nothing once warm. The copy shares no memory with the World.
func (w *World) Save(dst *WorldState) {
	if cap(dst.Fighters) < len(w.Fighters) {
		dst.Fighters = append(dst.Fighters[:cap(dst.Fighters)], make([]FighterState, len(w.Fighters)-cap(dst.Fighters))...)
	}
	dst.Fighters = dst.Fighters[:len(w.Fighters)]
	for i, f := range w.Fighters {
		s := &dst.Fighters[i]
		landed := s.Landed[:0]
		if f == nil {
			*s = FighterState{Landed: landed}
			continue
		}
		*s = FighterState{X: f.X, Y: f.Y, FacingLeft: f.FacingLeft, Blocking: f.Blocking, Frame: f.frame, Fresh: f.fresh, Stun: f.stun, Hit: f.hit, Blocked: f.blocked}
		if f.move != nil {
			s.Move = f.move.Name
		}
		for _, l := range f.landed {
			if v := w.index(l.victim); v >= 0 {
				landed = append(landed, Landed{Victim: v, Groups: l.groups})
			}
		}
		s.Landed = landed
	}
}

func (w *World) index(f *Fighter) int {
	for i, g := range w.Fighters {
		if g == f {
			return i
		}
	}
	return -1
}

// Restore sets every fighter to the state in src, which Save took from this World or from one with the same fighters. It copies src, which the caller may keep and restore again.
//
// It returns an error, and changes nothing, when src has a different number of fighters, a state for a nil fighter, a move its fighter's Moves does not have, a frame outside that move, or a Landed victim that is not another fighter of the World.
func (w *World) Restore(src *WorldState) error {
	if len(src.Fighters) != len(w.Fighters) {
		return fmt.Errorf("framedata: state has %d fighters, world has %d", len(src.Fighters), len(w.Fighters))
	}
	for i, f := range w.Fighters {
		s := &src.Fighters[i]
		if f == nil {
			if s.Move != "" || len(s.Landed) != 0 {
				return fmt.Errorf("framedata: state of fighter %d is for a nil fighter", i)
			}
			continue
		}
		if s.Move == "" {
			if s.Frame != 0 {
				return fmt.Errorf("framedata: fighter %d has frame %d and no move", i, s.Frame)
			}
		} else if m, ok := f.Moves.Move(s.Move); !ok {
			return fmt.Errorf("framedata: fighter %d has no move %q", i, s.Move)
		} else if s.Frame < 1 || s.Frame > m.Length {
			return fmt.Errorf("framedata: fighter %d frame %d is outside move %q of %d frames", i, s.Frame, s.Move, m.Length)
		}
		if s.Stun < 0 {
			return fmt.Errorf("framedata: fighter %d has negative stun %d", i, s.Stun)
		}
		for _, l := range s.Landed {
			if l.Victim < 0 || l.Victim >= len(w.Fighters) || l.Victim == i || w.Fighters[l.Victim] == nil {
				return fmt.Errorf("framedata: fighter %d landed on fighter %d, which is not an opponent", i, l.Victim)
			}
		}
	}
	for i, f := range w.Fighters {
		if f == nil {
			continue
		}
		s := &src.Fighters[i]
		f.X, f.Y, f.FacingLeft, f.Blocking = s.X, s.Y, s.FacingLeft, s.Blocking
		f.move, f.frame = nil, 0
		if s.Move != "" {
			f.move, _ = f.Moves.Move(s.Move)
			f.frame = s.Frame
		}
		f.fresh, f.stun, f.hit, f.blocked = s.Fresh, s.Stun, s.Hit, s.Blocked
		f.landed = f.landed[:0]
		for _, l := range s.Landed {
			f.landed = append(f.landed, landing{victim: w.Fighters[l.Victim], groups: l.Groups})
		}
	}
	return nil
}
