package flow

import (
	"errors"
	"fmt"
	"time"
)

// MachineSnapshotVersion is the [MachineSnapshot] schema version written and read
// by [Machine.Snapshot] and [Machine.Restore].
const MachineSnapshotVersion = 1

// ErrInvalidSnapshot reports a snapshot that [Machine.Restore] refuses because its
// version, recorded states, or timing do not match the target machine.
var ErrInvalidSnapshot = errors.New("flow: invalid snapshot")

// MachineSnapshot holds a [Machine]'s runtime state as plain data that marshals
// deterministically to YAML and JSON, for save slots, rollback netcode, and state dumps.
type MachineSnapshot[S comparable] struct {
	// Version is MachineSnapshotVersion.
	Version int `json:"version" yaml:"version"`
	// Current is the machine's active state (or its initial state if not yet started).
	Current S `json:"current" yaml:"current"`
	// Previous is the state before the last transition, or the zero value of S before any transition.
	Previous S `json:"previous,omitempty" yaml:"previous,omitempty"`
	// TimeInState is the game time spent in Current since entering it.
	TimeInState time.Duration `json:"timeInState,omitempty" yaml:"timeInState,omitempty"`
	// Started reports whether the machine has entered its initial state.
	Started bool `json:"started,omitempty" yaml:"started,omitempty"`
}

// Started reports whether the machine has been started (either via [Machine.Start],
// [Machine.SetState], [Machine.Update], or [Machine.Restore]).
func (m *Machine[S]) Started() bool { return m.started }

// Snapshot returns m's runtime state as plain data. Restoring it into a fresh
// [Machine] with the same states and transitions resumes from the exact state and
// TimeInState without running Exit or Enter hooks.
func (m *Machine[S]) Snapshot() MachineSnapshot[S] {
	if m == nil {
		return MachineSnapshot[S]{Version: MachineSnapshotVersion}
	}
	return MachineSnapshot[S]{
		Version:     MachineSnapshotVersion,
		Current:     m.current,
		Previous:    m.previous,
		TimeInState: m.timeInState,
		Started:     m.started,
	}
}

// Restore validates snap against m's known states and replaces m's runtime state
// without calling any Exit or Enter callbacks.
//
// Restore refuses snapshots with an unsupported version, unknown Current or non-zero
// Previous state, negative TimeInState, or non-zero TimeInState/Previous on an unstarted
// machine, leaving m unchanged and returning an error wrapping [ErrInvalidSnapshot].
func (m *Machine[S]) Restore(snap MachineSnapshot[S]) error {
	if err := m.validateRestore(snap); err != nil {
		return err
	}
	m.commitRestore(snap)
	return nil
}

func (m *Machine[S]) validateRestore(snap MachineSnapshot[S]) error {
	if m == nil {
		return fmt.Errorf("%w: nil machine", ErrInvalidSnapshot)
	}
	if snap.Version != MachineSnapshotVersion {
		return fmt.Errorf("%w: unsupported version %d (want %d)", ErrInvalidSnapshot, snap.Version, MachineSnapshotVersion)
	}
	if !m.hasState(snap.Current) {
		return fmt.Errorf("%w: unknown current state %v", ErrInvalidSnapshot, snap.Current)
	}
	var zero S
	if snap.Previous != zero && !m.hasState(snap.Previous) {
		return fmt.Errorf("%w: unknown previous state %v", ErrInvalidSnapshot, snap.Previous)
	}
	if snap.TimeInState < 0 {
		return fmt.Errorf("%w: negative timeInState %v", ErrInvalidSnapshot, snap.TimeInState)
	}
	if !snap.Started {
		if snap.TimeInState != 0 {
			return fmt.Errorf("%w: unstarted machine cannot have timeInState %v", ErrInvalidSnapshot, snap.TimeInState)
		}
		if snap.Previous != zero {
			return fmt.Errorf("%w: unstarted machine cannot have previous state %v", ErrInvalidSnapshot, snap.Previous)
		}
	}
	return nil
}

func (m *Machine[S]) commitRestore(snap MachineSnapshot[S]) {
	var zero S
	m.current = snap.Current
	m.previous = snap.Previous
	m.hasPrevious = snap.Previous != zero
	m.timeInState = snap.TimeInState
	m.started = snap.Started
	m.step = 0
	m.ClearHistory()
}

func (m *Machine[S]) resetState() {
	var zero S
	if m.hasInitial {
		m.current = m.initial
	} else {
		m.current = zero
	}
	m.previous = zero
	m.hasPrevious = false
	m.timeInState = 0
	m.started = false
	m.step = 0
	m.ClearHistory()
}

// hasState reports whether id is a state known to m via AddState, NewMachine's initial
// state, or a registered transition endpoint.
func (m *Machine[S]) hasState(id S) bool {
	if _, ok := m.states[id]; ok {
		return true
	}
	if m.hasInitial && m.initial == id {
		return true
	}
	for _, tr := range m.transitions {
		if tr.to == id || (!tr.any && tr.from == id) {
			return true
		}
	}
	return false
}
