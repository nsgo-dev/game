package quest

import (
	"errors"
	"fmt"
	"slices"
)

// SnapshotVersion is the Snapshot schema version this package writes and reads.
const SnapshotVersion = 1

// ErrInvalidSnapshot reports a Snapshot that Restore refuses: a wrong version, an unknown or duplicate quest or objective, or progress outside an objective's range.
var ErrInvalidSnapshot = errors.New("quest: invalid snapshot")

// Snapshot is a Log's progress as plain data that encodes to stable JSON or YAML, for storing in a save slot with save.Store.
type Snapshot struct {
	// Version is SnapshotVersion.
	Version int `json:"version" yaml:"version"`
	// Quests holds every quest that is not Inactive, in book order.
	Quests []QuestRecord `json:"quests,omitempty" yaml:"quests,omitempty"`
}

// QuestRecord is one quest's state and objective progress in a Snapshot.
type QuestRecord struct {
	// ID is the quest's ID.
	ID string `json:"id" yaml:"id"`
	// State is Active, Completed or Failed.
	State State `json:"state" yaml:"state"`
	// Progress maps objective IDs to their progress; objectives with no progress are left out.
	Progress map[string]int `json:"progress,omitempty" yaml:"progress,omitempty"`
}

// Snapshot returns l's progress. It shares no memory with l.
func (l *Log) Snapshot() Snapshot {
	s := Snapshot{Version: SnapshotVersion}
	for i := range l.quests {
		p := &l.quests[i]
		if p.state == Inactive {
			continue
		}
		rec := QuestRecord{ID: l.book.Quests[i].ID, State: p.state}
		for j, n := range p.counts {
			if n == 0 {
				continue
			}
			if rec.Progress == nil {
				rec.Progress = make(map[string]int)
			}
			rec.Progress[l.book.Quests[i].Objectives[j].ID] = n
		}
		s.Quests = append(s.Quests, rec)
	}
	return s
}

// Restore replaces l's progress with s, which must come from a Log running the same book. It checks s first and leaves l unchanged when s is invalid (errors wrap ErrInvalidSnapshot). Restore delivers no updates and starts no quests; call StartReady afterwards if the book has auto-start quests.
//
// A snapshot written before a content change that removes a quest or objective, lowers a count, or turns objectives optional so an active quest would already be complete is refused; migrate it with a save.Migration.
func (l *Log) Restore(s Snapshot) error {
	if s.Version != SnapshotVersion {
		return fmt.Errorf("%w: unsupported version %d (want %d)", ErrInvalidSnapshot, s.Version, SnapshotVersion)
	}
	next := make([]progress, len(l.quests))
	for i := range next {
		next[i].counts = make([]int, len(l.book.Quests[i].Objectives))
	}
	seen := make([]bool, len(l.quests))
	for _, rec := range s.Quests {
		i, ok := l.index[rec.ID]
		if !ok {
			return fmt.Errorf("%w: %w: %q", ErrInvalidSnapshot, ErrUnknownQuest, rec.ID)
		}
		if seen[i] {
			return fmt.Errorf("%w: quest %q recorded twice", ErrInvalidSnapshot, rec.ID)
		}
		seen[i] = true
		switch rec.State {
		case Active, Completed, Failed:
		default:
			return fmt.Errorf("%w: quest %q has state %s", ErrInvalidSnapshot, rec.ID, rec.State)
		}
		q := &l.book.Quests[i]
		next[i].state = rec.State
		for objID, n := range rec.Progress {
			j := slices.IndexFunc(q.Objectives, func(o Objective) bool { return o.ID == objID })
			if j < 0 {
				return fmt.Errorf("%w: %w: %q in quest %q", ErrInvalidSnapshot, ErrUnknownObjective, objID, rec.ID)
			}
			if n < 0 || n > q.Objectives[j].Required() {
				return fmt.Errorf("%w: quest %q objective %q progress %d is outside [0, %d]", ErrInvalidSnapshot, rec.ID, objID, n, q.Objectives[j].Required())
			}
			next[i].counts[j] = n
		}
		if rec.State == Active && requiredDone(q, next[i].counts) {
			return fmt.Errorf("%w: active quest %q has every required objective complete", ErrInvalidSnapshot, rec.ID)
		}
	}
	l.quests = next
	return nil
}

func requiredDone(q *Quest, counts []int) bool {
	for j, o := range q.Objectives {
		if !o.Optional && counts[j] < o.Required() {
			return false
		}
	}
	return true
}
