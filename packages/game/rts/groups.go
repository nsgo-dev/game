package rts

import (
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// GroupCount is the number of control groups, one per digit key 0 to 9.
const GroupCount = 10

// GroupAction is what a digit key did to the control groups.
type GroupAction uint8

// The control group actions.
const (
	// NoGroupAction means the key was not a control group key.
	NoGroupAction GroupAction = iota
	// GroupAssigned means the group was set to the selection (Control or Command with a digit).
	GroupAssigned
	// GroupAdded means the selection was added to the group and the whole group selected (Shift with a digit).
	GroupAdded
	// GroupRecalled means the selection was replaced by the group (a digit alone).
	GroupRecalled
	// GroupFocused means the group was recalled twice in quick succession; the game should center its camera on the group.
	GroupFocused
)

// String returns the action's name.
func (a GroupAction) String() string {
	switch a {
	case NoGroupAction:
		return "none"
	case GroupAssigned:
		return "assigned"
	case GroupAdded:
		return "added"
	case GroupRecalled:
		return "recalled"
	case GroupFocused:
		return "focused"
	}
	return "unknown"
}

// GroupEvent reports what HandleKey did.
type GroupEvent struct {
	Action GroupAction
	// Group is the digit of the group acted on.
	Group int
}

// ControlGroups holds the ten numbered groups a player binds units to and recalls them by.
//
// Double-tap detection counts game steps rather than wall-clock time, so it replays exactly: pass the same step counter (Crowd.Steps, or the game's own) to every HandleKey or Recall. The zero value holds ten empty groups and uses DefaultDoubleTapSteps. ControlGroups is not safe for concurrent use.
type ControlGroups struct {
	// DoubleTapSteps is how many steps apart two recalls of the same group count as a double tap; 0 or less uses DefaultDoubleTapSteps.
	DoubleTapSteps uint64

	groups   [GroupCount][]UnitID
	lastKey  int
	lastStep uint64
	tapped   bool
}

// DefaultDoubleTapSteps is the double-tap window, 18 steps or 0.3 seconds at 60 steps per second.
const DefaultDoubleTapSteps = 18

func validGroup(n int) bool { return n >= 0 && n < GroupCount }

// Group returns the units bound to group n in the order they were added, or nil for a group outside 0 to 9. The slice is the groups' own.
func (g *ControlGroups) Group(n int) []UnitID {
	if !validGroup(n) {
		return nil
	}
	return g.groups[n]
}

// Assign binds group n to ids, replacing what it held. Repeats are dropped.
func (g *ControlGroups) Assign(n int, ids []UnitID) {
	if !validGroup(n) {
		return
	}
	g.groups[n] = g.groups[n][:0]
	g.Append(n, ids)
}

// Append adds the units of ids that group n does not hold yet to its end.
func (g *ControlGroups) Append(n int, ids []UnitID) {
	if !validGroup(n) {
		return
	}
	for _, id := range ids {
		if id != 0 && !slices.Contains(g.groups[n], id) {
			g.groups[n] = append(g.groups[n], id)
		}
	}
}

// Forget removes id from every group, as when the unit dies.
func (g *ControlGroups) Forget(id UnitID) {
	for n := range g.groups {
		g.groups[n] = slices.DeleteFunc(g.groups[n], func(x UnitID) bool { return x == id })
	}
}

// Retain keeps in every group only the units keep reports true for.
func (g *ControlGroups) Retain(keep func(UnitID) bool) {
	for n := range g.groups {
		g.groups[n] = slices.DeleteFunc(g.groups[n], func(x UnitID) bool { return !keep(x) })
	}
}

// Recall replaces sel with group n at game step step and reports GroupFocused when the same group was recalled within DoubleTapSteps before, GroupRecalled otherwise. Recalling an empty group leaves sel unchanged and reports NoGroupAction.
func (g *ControlGroups) Recall(n int, sel *Selection, step uint64) GroupAction {
	if !validGroup(n) || len(g.groups[n]) == 0 {
		return NoGroupAction
	}
	window := g.DoubleTapSteps
	if window == 0 {
		window = DefaultDoubleTapSteps
	}
	double := g.tapped && g.lastKey == n && step >= g.lastStep && step-g.lastStep <= window
	g.lastKey, g.lastStep, g.tapped = n, step, !double
	if sel != nil {
		sel.Set(g.groups[n])
	}
	if double {
		return GroupFocused
	}
	return GroupRecalled
}

// HandleKey applies the usual strategy-game bindings for a digit key at game step step: Control or Command with a digit assigns the selection to that group, Shift with a digit adds the selection to it and selects the whole group, and a digit alone recalls it (twice quickly to focus). Keys other than unmodified or so-modified digits, and auto-repeats, report NoGroupAction and change nothing.
func (g *ControlGroups) HandleKey(ev core.KeyEvent, sel *Selection, step uint64) GroupEvent {
	if ev.Repeat || len(ev.Key) != 1 || ev.Key[0] < '0' || ev.Key[0] > '9' || ev.Modifiers.Alt {
		return GroupEvent{}
	}
	n := int(ev.Key[0] - '0')
	var current []UnitID
	if sel != nil {
		current = sel.IDs()
	}
	switch {
	case ev.Modifiers.Control || ev.Modifiers.Platform:
		g.Assign(n, current)
		g.tapped = false
		return GroupEvent{Action: GroupAssigned, Group: n}
	case ev.Modifiers.Shift:
		g.Append(n, current)
		if sel != nil {
			sel.Set(g.groups[n])
		}
		g.tapped = false
		return GroupEvent{Action: GroupAdded, Group: n}
	}
	if a := g.Recall(n, sel, step); a != NoGroupAction {
		return GroupEvent{Action: a, Group: n}
	}
	return GroupEvent{}
}
