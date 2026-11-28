package selfhost

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// Record is an account's best score in one period of a board, as a Store keeps it.
type Record struct {
	// Board is the board's name.
	Board string
	// Period is the period's ID.
	Period string
	// Account is the account ID.
	Account string
	// Score is the best score.
	Score    int64
	Subscore int64
	Metadata json.RawMessage
	// Achieved is when the score was submitted; an earlier achievement ranks above a later one with the same score.
	Achieved time.Time
}

// Store keeps the service's records so a restart finds the boards as they were. The service calls it with one board's calls at a time, from many goroutines across boards.
type Store interface {
	// Load calls fn for every record kept, in the order they were saved, stopping at fn's first error. A later record for the same board, period and account replaces an earlier one.
	Load(ctx context.Context, fn func(Record) error) error
	// Save keeps rec as the account's best score in the period, replacing the one before.
	Save(ctx context.Context, rec Record) error
	// Drop removes every record of a board's period, once the board no longer keeps it.
	Drop(ctx context.Context, board, period string) error
}

// MemoryStore is a Store in memory, for tests and prototypes: its records last as long as the process. It is safe for concurrent use.
type MemoryStore struct {
	mu      sync.Mutex
	records []Record
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

// Load calls fn for every record in the order they were saved.
func (m *MemoryStore) Load(ctx context.Context, fn func(Record) error) error {
	m.mu.Lock()
	records := append([]Record(nil), m.records...)
	m.mu.Unlock()
	for _, r := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}

// Save appends rec.
func (m *MemoryStore) Save(ctx context.Context, rec Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rec)
	return nil
}

// Drop removes the records of a board's period.
func (m *MemoryStore) Drop(ctx context.Context, board, period string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.records[:0]
	for _, r := range m.records {
		if r.Board != board || r.Period != period {
			kept = append(kept, r)
		}
	}
	clear(m.records[len(kept):])
	m.records = kept
	return nil
}

// Periods returns the board and period pairs the store holds records for, sorted, as "board/period".
func (m *MemoryStore) Periods() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, r := range m.records {
		k := r.Board + "/" + r.Period
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
