package cloudsave

import (
	"cmp"
	"context"
	"slices"
	"sync"
)

// MemoryStore is a Store held in memory, for tests and single-process prototypes. Its records are lost when the process ends.
type MemoryStore struct {
	mu       sync.Mutex
	accounts map[string]map[string][]Version // account -> slot -> versions, oldest first
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{accounts: map[string]map[string][]Version{}}
}

// Head implements Store.
func (m *MemoryStore) Head(_ context.Context, account, slot string) (Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	vs := m.accounts[account][slot]
	if len(vs) == 0 {
		return Version{}, ErrNotFound
	}
	return withData(vs[len(vs)-1]), nil
}

// Version implements Store.
func (m *MemoryStore) Version(_ context.Context, account, slot string, number int64) (Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.accounts[account][slot] {
		if v.Number == number {
			return withData(v), nil
		}
	}
	return Version{}, ErrNotFound
}

// Versions implements Store.
func (m *MemoryStore) Versions(_ context.Context, account, slot string) ([]Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	vs := m.accounts[account][slot]
	if len(vs) == 0 {
		return nil, ErrNotFound
	}
	out := make([]Version, 0, len(vs))
	for i := len(vs) - 1; i >= 0; i-- {
		out = append(out, withoutData(vs[i]))
	}
	return out, nil
}

// Slots implements Store.
func (m *MemoryStore) Slots(_ context.Context, account string) ([]Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Version, 0, len(m.accounts[account]))
	for _, vs := range m.accounts[account] {
		out = append(out, withoutData(vs[len(vs)-1]))
	}
	slices.SortFunc(out, func(a, b Version) int { return cmp.Compare(a.Slot, b.Slot) })
	return out, nil
}

// Append implements Store.
func (m *MemoryStore) Append(_ context.Context, account, slot string, w Write) (Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	slots := m.accounts[account]
	vs := slots[slot]
	var head Version
	ok := len(vs) > 0
	if ok {
		head = vs[len(vs)-1]
	}
	if !w.Match.Holds(head, ok) {
		return Version{}, ErrChanged
	}
	live := ok && !head.Deleted
	if w.Delete && !live {
		return Version{}, ErrNotFound
	}
	if !w.Delete && !live {
		n := 0
		for _, s := range slots {
			if !s[len(s)-1].Deleted {
				n++
			}
		}
		if n >= w.MaxSlots {
			return Version{}, ErrTooManySlots
		}
	}
	v := newVersion(slot, head.Number+1, w)
	if slots == nil {
		slots = map[string][]Version{}
		m.accounts[account] = slots
	}
	vs = append(vs, v)
	if keep := max(w.Keep, 1); len(vs) > keep {
		vs = slices.Clone(vs[len(vs)-keep:])
	}
	slots[slot] = vs
	if w.Delete {
		m.purgeDeleted(slots, w.MaxSlots)
	}
	return withoutData(v), nil
}

// purgeDeleted removes the oldest deleted slots of an account beyond limit.
func (m *MemoryStore) purgeDeleted(slots map[string][]Version, limit int) {
	var deleted []Version
	for _, vs := range slots {
		if h := vs[len(vs)-1]; h.Deleted {
			deleted = append(deleted, h)
		}
	}
	if len(deleted) <= limit {
		return
	}
	slices.SortFunc(deleted, compareAge)
	for _, h := range deleted[:len(deleted)-limit] {
		delete(slots, h.Slot)
	}
}

// Purge implements Store.
func (m *MemoryStore) Purge(_ context.Context, account, slot string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.accounts[account], slot)
	if len(m.accounts[account]) == 0 {
		delete(m.accounts, account)
	}
	return nil
}

// compareAge orders versions oldest first, by time and then by slot name, the order deleted slots are purged in.
func compareAge(a, b Version) int {
	if c := a.Created.Compare(b.Created); c != 0 {
		return c
	}
	return cmp.Compare(a.Slot, b.Slot)
}

// newVersion builds version number of slot from w.
func newVersion(slot string, number int64, w Write) Version {
	v := Version{Slot: slot, Number: number, Deleted: w.Delete, Created: w.Created.UTC().Truncate(1e6)}
	if !w.Delete {
		v.Data = slices.Clone(w.Data)
		if v.Data == nil {
			v.Data = []byte{}
		}
		v.Hash, v.Size = Hash(v.Data), int64(len(v.Data))
	}
	return v
}

func withData(v Version) Version {
	if v.Data != nil {
		v.Data = slices.Clone(v.Data)
	}
	return v
}

func withoutData(v Version) Version {
	v.Data = nil
	return v
}
