// Package debug provides deterministic gameplay debug categories and a
// fixed-step visual log suitable for editor inspection.
package debug

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type Field struct {
	Name, Value string `json:"name"`
}
type Category interface {
	Name() string
	Snapshot(entity string) []Field
}
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

type Entry struct {
	Step             uint64 `json:"step"`
	Entity, Category string
	Level            Level
	Message          string `json:"message"`
}
type Shape struct {
	Step         uint64 `json:"step"`
	Entity, Kind string
	Points       [][3]float32 `json:"points,omitempty"`
}
type VLog struct {
	mu      sync.RWMutex
	max     int
	entries []Entry
	shapes  []Shape
}

func New(max int) *VLog {
	if max <= 0 {
		max = 4096
	}
	return &VLog{max: max}
}
func (l *VLog) Log(step uint64, entity, category string, level Level, message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, Entry{step, entity, category, level, message})
	if len(l.entries) > l.max {
		l.entries = l.entries[len(l.entries)-l.max:]
	}
}
func (l *VLog) Shape(step uint64, entity, kind string, points [][3]float32) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.shapes = append(l.shapes, Shape{step, entity, kind, points})
	if len(l.shapes) > l.max {
		l.shapes = l.shapes[len(l.shapes)-l.max:]
	}
}
func (l *VLog) Entries(entity, category string, from, to uint64) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := []Entry{}
	for _, e := range l.entries {
		if entity != "" && e.Entity != entity || category != "" && e.Category != category || e.Step < from || to > 0 && e.Step > to {
			continue
		}
		out = append(out, e)
	}
	return out
}
func (l *VLog) WriteJSONL(w io.Writer) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	enc := json.NewEncoder(w)
	for _, e := range l.entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	for _, s := range l.shapes {
		if err := enc.Encode(s); err != nil {
			return err
		}
	}
	return nil
}
func ReadJSONL(r io.Reader) ([]Entry, []Shape, error) {
	scan := bufio.NewScanner(r)
	entries := []Entry{}
	shapes := []Shape{}
	for scan.Scan() {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			return nil, nil, err
		}
		if _, ok := raw["message"]; ok {
			var e Entry
			if err := json.Unmarshal(scan.Bytes(), &e); err != nil {
				return nil, nil, err
			}
			entries = append(entries, e)
		} else {
			var s Shape
			if err := json.Unmarshal(scan.Bytes(), &s); err != nil {
				return nil, nil, err
			}
			shapes = append(shapes, s)
		}
	}
	return entries, shapes, scan.Err()
}
func (l *VLog) String() string {
	return fmt.Sprintf("debug log entries=%d shapes=%d", len(l.entries), len(l.shapes))
}
