package quest

import (
	"errors"
	"fmt"
	"slices"
)

// SnapshotVersion is the Snapshot schema version this package writes and reads.
const SnapshotVersion = 1

// ErrInvalidSnapshot reports a Snapshot that Restore refuses: a wrong version, an unknown or duplicate quest or objective, progress outside an objective's range, or a combination of states and progress that a Log cannot reach.
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
	// State is Active, Completed, Failed or Abandoned.
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
// Besides IDs and ranges, Restore refuses progress a Log cannot reach: a recorded quest whose prerequisite is unrecorded, or is not Completed when it is not Repeatable (a completed prerequisite stays completed unless it repeats); an abandoned quest with progress; progress on an objective of an ordered quest while an earlier required objective is incomplete; and an active quest whose required objectives are all complete.
//
// The zero Snapshot (version 0 and no quests) means no progress: Restore resets every quest to Inactive. That is what a save slot written before the game tracked quests decodes to.
//
// A snapshot written before a content change that removes a quest or objective, lowers a count, adds a prerequisite, or turns objectives optional so an active quest would already be complete is refused; migrate it with a save.Migration.
func (l *Log) Restore(s Snapshot) error {
	if s.Version == 0 && len(s.Quests) == 0 {
		l.quests = l.emptyProgress()
		return nil
	}
	if s.Version != SnapshotVersion {
		return fmt.Errorf("%w: unsupported version %d (want %d)", ErrInvalidSnapshot, s.Version, SnapshotVersion)
	}
	next := l.emptyProgress()
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
		case Active, Completed, Failed, Abandoned:
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
		if rec.State == Abandoned && slices.ContainsFunc(next[i].counts, func(n int) bool { return n != 0 }) {
			return fmt.Errorf("%w: abandoned quest %q has progress", ErrInvalidSnapshot, rec.ID)
		}
		if q.Ordered {
			if err := checkOrderedProgress(q, next[i].counts); err != nil {
				return err
			}
		}
		if rec.State == Active && requiredDone(q, next[i].counts) {
			return fmt.Errorf("%w: active quest %q has every required objective complete", ErrInvalidSnapshot, rec.ID)
		}
	}
	// Prerequisites are checked once every record is known, since a record may name a quest recorded after it.
	for i := range next {
		if next[i].state == Inactive {
			continue
		}
		q := &l.book.Quests[i]
		for _, req := range q.Requires {
			r := l.index[req]
			switch st := next[r].state; {
			case st == Inactive:
				return fmt.Errorf("%w: quest %q is %s but its prerequisite %q never started", ErrInvalidSnapshot, q.ID, next[i].state, req)
			case st != Completed && !l.book.Quests[r].Repeatable:
				return fmt.Errorf("%w: quest %q is %s but its prerequisite %q is %s", ErrInvalidSnapshot, q.ID, next[i].state, req, st)
			}
		}
	}
	l.quests = next
	return nil
}

func (l *Log) emptyProgress() []progress {
	out := make([]progress, len(l.quests))
	for i := range out {
		out[i].counts = make([]int, len(l.book.Quests[i].Objectives))
	}
	return out
}

// checkOrderedProgress refuses progress on an objective of an ordered quest while a required objective before it is incomplete, which Log.objectiveState keeps locked.
func checkOrderedProgress(q *Quest, counts []int) error {
	for j, n := range counts {
		if n == 0 {
			continue
		}
		for k := range j {
			if !q.Objectives[k].Optional && counts[k] < q.Objectives[k].Required() {
				return fmt.Errorf("%w: ordered quest %q has progress on objective %q before %q is complete", ErrInvalidSnapshot, q.ID, q.Objectives[j].ID, q.Objectives[k].ID)
			}
		}
	}
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
