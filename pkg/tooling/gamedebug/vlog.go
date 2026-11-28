//go:build !release

package gamedebug

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

// VLog is a bounded visual log of entity messages, Category snapshots and debug shapes, each tagged with the fixed step it was recorded at. Once full, recording a new entry drops the oldest entry, and likewise for shapes. A VLog is safe for concurrent use, and every method on a nil *VLog is a no-op.
type VLog struct {
	mu      sync.RWMutex
	max     int
	entries vlogRing[VLogEntry]
	shapes  vlogRing[VLogShape]
}

// vlogRing keeps the newest max values; once full, a push overwrites the oldest at start.
type vlogRing[T any] struct {
	buf   []T
	start int
}

func (r *vlogRing[T]) push(v T, max int) {
	if len(r.buf) < max {
		r.buf = append(r.buf, v)
		return
	}
	r.buf[r.start] = v
	r.start = (r.start + 1) % len(r.buf)
}

// all yields the values oldest first.
func (r *vlogRing[T]) all(yield func(T) bool) {
	for i := range r.buf {
		if !yield(r.buf[(r.start+i)%len(r.buf)]) {
			return
		}
	}
}

// NewVLog returns a VLog keeping at most capacity entries and capacity shapes; DefaultVLogCapacity when capacity is 0 or less.
func NewVLog(capacity int) *VLog {
	if capacity <= 0 {
		capacity = DefaultVLogCapacity
	}
	return &VLog{max: capacity}
}

// Log records a message for entity under category at step.
func (l *VLog) Log(step uint64, entity, category string, level VLogLevel, message string) {
	l.addEntry(VLogEntry{Step: step, Entity: entity, Category: category, Level: level, Message: message})
}

// Snapshot records what c reports for entity at step as an info entry under c's name.
func (l *VLog) Snapshot(step uint64, entity string, c Category) {
	if l == nil {
		return
	}
	l.addEntry(VLogEntry{Step: step, Entity: entity, Category: c.Name(), Level: VLogInfo, Fields: slices.Clone(c.Snapshot(entity))})
}

// Shape records a debug shape of kind for entity at step. The points are copied.
func (l *VLog) Shape(step uint64, entity, kind string, points [][3]float32) {
	if l == nil {
		return
	}
	s := VLogShape{Step: step, Entity: entity, Kind: kind, Points: slices.Clone(points)}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.shapes.push(s, l.max)
}

func (l *VLog) addEntry(e VLogEntry) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries.push(e, l.max)
}

// Entries returns, in recorded order, the entries whose step is in [from, to] and that match entity and category. An empty entity or category matches any, and a to of 0 means no upper bound.
func (l *VLog) Entries(entity, category string, from, to uint64) []VLogEntry {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	var out []VLogEntry
	for e := range l.entries.all {
		if (entity == "" || e.Entity == entity) && (category == "" || e.Category == category) && inSteps(e.Step, from, to) {
			out = append(out, e)
		}
	}
	return out
}

// Shapes returns, in recorded order, the shapes whose step is in [from, to] and that match entity. An empty entity matches any, and a to of 0 means no upper bound.
func (l *VLog) Shapes(entity string, from, to uint64) []VLogShape {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	var out []VLogShape
	for s := range l.shapes.all {
		if (entity == "" || s.Entity == entity) && inSteps(s.Step, from, to) {
			out = append(out, s)
		}
	}
	return out
}

func inSteps(step, from, to uint64) bool {
	return step >= from && (to == 0 || step <= to)
}

// Len reports how many entries and shapes the log holds.
func (l *VLog) Len() (entries, shapes int) {
	if l == nil {
		return 0, 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries.buf), len(l.shapes.buf)
}

type vlogHeader struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
}

type vlogEntryLine struct {
	Type string `json:"type"`
	VLogEntry
}

type vlogShapeLine struct {
	Type string `json:"type"`
	VLogShape
}

// WriteJSONL writes the log as JSON Lines: a header line naming VLogFormat and VLogVersion, then every entry and then every shape in recorded order, each tagged with "type": "entry" or "shape".
func (l *VLog) WriteJSONL(w io.Writer) error {
	enc := json.NewEncoder(w)
	if err := enc.Encode(vlogHeader{Format: VLogFormat, Version: VLogVersion}); err != nil {
		return err
	}
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	for e := range l.entries.all {
		if err := enc.Encode(vlogEntryLine{Type: "entry", VLogEntry: e}); err != nil {
			return err
		}
	}
	for s := range l.shapes.all {
		if err := enc.Encode(vlogShapeLine{Type: "shape", VLogShape: s}); err != nil {
			return err
		}
	}
	return nil
}

// ReadVLog reads a log written by WriteJSONL. It refuses a missing or unknown header, an unknown record type or level, and unknown fields.
func ReadVLog(r io.Reader) ([]VLogEntry, []VLogShape, error) {
	dec := json.NewDecoder(r)
	var header vlogHeader
	if err := decodeStrict(dec, &header); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil, errors.New("gamedebug: visual log has no header")
		}
		return nil, nil, fmt.Errorf("gamedebug: visual log header: %w", err)
	}
	if header.Format != VLogFormat || header.Version != VLogVersion {
		return nil, nil, fmt.Errorf("gamedebug: visual log header is %q version %d, want %q version %d", header.Format, header.Version, VLogFormat, VLogVersion)
	}
	var entries []VLogEntry
	var shapes []VLogShape
	for n := 2; ; n++ {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return entries, shapes, nil
			}
			return nil, nil, fmt.Errorf("gamedebug: visual log record %d: %w", n, err)
		}
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &kind); err != nil {
			return nil, nil, fmt.Errorf("gamedebug: visual log record %d: %w", n, err)
		}
		rec := json.NewDecoder(bytes.NewReader(raw))
		switch kind.Type {
		case "entry":
			var line vlogEntryLine
			if err := decodeStrict(rec, &line); err != nil {
				return nil, nil, fmt.Errorf("gamedebug: visual log record %d: %w", n, err)
			}
			switch line.Level {
			case VLogInfo, VLogWarn, VLogError:
			default:
				return nil, nil, fmt.Errorf("gamedebug: visual log record %d: unknown level %q", n, line.Level)
			}
			entries = append(entries, line.VLogEntry)
		case "shape":
			var line vlogShapeLine
			if err := decodeStrict(rec, &line); err != nil {
				return nil, nil, fmt.Errorf("gamedebug: visual log record %d: %w", n, err)
			}
			shapes = append(shapes, line.VLogShape)
		default:
			return nil, nil, fmt.Errorf("gamedebug: visual log record %d: unknown type %q", n, kind.Type)
		}
	}
}

func decodeStrict(dec *json.Decoder, v any) error {
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// String summarizes the log's size.
func (l *VLog) String() string {
	entries, shapes := l.Len()
	return fmt.Sprintf("gamedebug.VLog entries=%d shapes=%d", entries, shapes)
}
