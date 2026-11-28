// Package debug provides opt-in gameplay diagnostics and a deterministic visual log.
package debug

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Entry is one JSON Lines visual-log record.
type Entry struct {
	Step     int64              `json:"step"`
	Entity   string             `json:"entity"`
	Category string             `json:"category"`
	Level    string             `json:"level,omitempty"`
	Message  string             `json:"message,omitempty"`
	Kind     string             `json:"kind,omitempty"`
	Values   map[string]float64 `json:"values,omitempty"`
}

// VLog is a bounded per-step visual log safe for recording and replay inspection.
type VLog struct {
	mu      sync.Mutex
	max     int
	entries []Entry
}

// NewVLog creates a log retaining at most max entries. A non-positive max uses 4096.
func NewVLog(max int) *VLog {
	if max <= 0 {
		max = 4096
	}
	return &VLog{max: max}
}

// Logf records a formatted diagnostic message.
func (v *VLog) Logf(step int64, entity, category, level, format string, args ...any) {
	v.add(Entry{Step: step, Entity: entity, Category: category, Level: level, Message: fmt.Sprintf(format, args...)})
}

// Shape records a named debug shape and numeric values.
func (v *VLog) Shape(step int64, entity, category, kind string, values map[string]float64) {
	v.add(Entry{Step: step, Entity: entity, Category: category, Kind: kind, Values: values})
}
func (v *VLog) add(e Entry) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.entries) >= v.max {
		copy(v.entries, v.entries[1:])
		v.entries = v.entries[:v.max-1]
	}
	v.entries = append(v.entries, e)
}

// Entries returns a copy filtered to one entity and inclusive step range.
func (v *VLog) Entries(entity string, from, to int64) []Entry {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []Entry
	for _, e := range v.entries {
		if (entity == "" || e.Entity == entity) && (from < 0 || e.Step >= from) && (to < 0 || e.Step <= to) {
			out = append(out, e)
		}
	}
	return out
}

// WriteJSONL writes entries in recording order.
func (v *VLog) WriteJSONL(w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, e := range v.Entries("", -1, -1) {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// ReadJSONL loads entries from JSON Lines.
func ReadJSONL(r io.Reader) ([]Entry, error) {
	s := bufio.NewScanner(r)
	var out []Entry
	for s.Scan() {
		var e Entry
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, s.Err()
}
