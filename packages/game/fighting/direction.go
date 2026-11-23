// Package fighting buffers per-step fighting-game input and recognizes motion inputs (quarter circles, dragon punches, charges, double taps), button presses and combos from YAML move sets.
//
// Directions use numpad notation: 5 is neutral, 6 is forward, 4 is back, 2 is down and 8 is up, with the diagonals on 1, 3, 7 and 9. A Recognizer samples one step at a time from any Source, such as *input.Actions or a *replay.Player, mirrors the direction when the character faces left, and reports the moves and combos completed on that step.
package fighting

import "strconv"

// Direction is a stick or pad direction in numpad notation, from 1 (down-back) to 9 (up-forward), with 5 for neutral. The zero value is invalid; Neutral is the resting direction.
type Direction uint8

// Numpad directions relative to a character facing right, so Back is left and Forward is right.
const (
	DownBack    Direction = 1
	Down        Direction = 2
	DownForward Direction = 3
	Back        Direction = 4
	Neutral     Direction = 5
	Forward     Direction = 6
	UpBack      Direction = 7
	Up          Direction = 8
	UpForward   Direction = 9
)

// DirectionOf returns the direction for a horizontal sign x and a vertical sign y, where x > 0 is right, x < 0 is left, y > 0 is up and y < 0 is down; zero is centered on that axis.
func DirectionOf(x, y int) Direction {
	col := 1
	switch {
	case x < 0:
		col = 0
	case x > 0:
		col = 2
	}
	row := 1
	switch {
	case y < 0:
		row = 0
	case y > 0:
		row = 2
	}
	return Direction(row*3 + col + 1)
}

// Valid reports whether d is one of the nine numpad directions.
func (d Direction) Valid() bool { return d >= 1 && d <= 9 }

// X returns -1 when d points back (left), 1 when it points forward (right) and 0 otherwise, including for an invalid direction.
func (d Direction) X() int {
	if !d.Valid() {
		return 0
	}
	return int((d-1)%3) - 1
}

// Y returns -1 when d points down, 1 when it points up and 0 otherwise, including for an invalid direction.
func (d Direction) Y() int {
	if !d.Valid() {
		return 0
	}
	return int((d-1)/3) - 1
}

// Mirror returns d flipped horizontally (4 and 6, 1 and 3, 7 and 9 swap). It converts between absolute directions and directions relative to a character facing left. An invalid direction is returned unchanged.
func (d Direction) Mirror() Direction {
	if !d.Valid() {
		return d
	}
	return DirectionOf(-d.X(), d.Y())
}

// String returns the numpad digit for d, or "?" for an invalid direction.
func (d Direction) String() string {
	if !d.Valid() {
		return "?"
	}
	return strconv.Itoa(int(d))
}

// Facing is the side a character faces. The zero value, FaceRight, leaves directions unchanged; FaceLeft mirrors them so 6 always means toward the opponent.
type Facing uint8

const (
	// FaceRight means forward is right: absolute and relative directions are the same.
	FaceRight Facing = iota
	// FaceLeft means forward is left: directions are mirrored before matching.
	FaceLeft
)

// Relative returns the absolute direction d as seen by a character with facing f.
func (f Facing) Relative(d Direction) Direction {
	if f == FaceLeft {
		return d.Mirror()
	}
	return d
}

// dirSet is a bit set of directions, bit d for direction d.
type dirSet uint16

func (s dirSet) has(d Direction) bool { return s&(1<<d) != 0 }

// parseDirSet parses a string of distinct numpad digits such as "2" or "123" into a set.
func parseDirSet(s string) (dirSet, bool) {
	if s == "" {
		return 0, false
	}
	var set dirSet
	for _, r := range s {
		if r < '1' || r > '9' {
			return 0, false
		}
		bit := dirSet(1) << (r - '0')
		if set&bit != 0 {
			return 0, false
		}
		set |= bit
	}
	return set, true
}
