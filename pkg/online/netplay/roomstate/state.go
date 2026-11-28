// Package roomstate provides deterministic, view-filtered room state deltas.
package roomstate

import (
	"encoding/json"
	"sort"
)

// View selects which recipients may see a field.
type View uint8

const (
	ViewAll View = iota
	ViewOwner
	ViewTeam
	ViewServer
)

// Field is one state value and its visibility contract.
type Field struct {
	Value any
	View  View
	Owner string
	Team  string
}

// State stores JSON-compatible fields and dirty versions.
type State struct {
	fields  map[string]Field
	dirty   map[string]uint64
	version uint64
}

// New returns an empty state.
func New() *State { return &State{fields: make(map[string]Field), dirty: make(map[string]uint64)} }

// Set publishes a field value and marks it dirty. Values must be JSON-marshalable.
func (s *State) Set(name string, value any, view View, owner, team string) {
	if s == nil || name == "" {
		return
	}
	if s.fields == nil {
		s.fields = make(map[string]Field)
		s.dirty = make(map[string]uint64)
	}
	s.version++
	s.fields[name] = Field{Value: value, View: view, Owner: owner, Team: team}
	s.dirty[name] = s.version
}

// Delete removes a field and includes a null marker in the next delta.
func (s *State) Delete(name string) {
	if s == nil {
		return
	}
	s.version++
	delete(s.fields, name)
	s.dirty[name] = s.version
}

// Delta returns deterministic JSON containing fields changed after version for a viewer.
func (s *State) Delta(after uint64, owner, team string) ([]byte, uint64, error) {
	if s == nil {
		return []byte(`{"version":0,"fields":{}}`), 0, nil
	}
	keys := make([]string, 0)
	for k, v := range s.dirty {
		if v > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	fields := make(map[string]any)
	for _, k := range keys {
		f, ok := s.fields[k]
		if !ok {
			fields[k] = nil
			continue
		}
		if f.View == ViewOwner && f.Owner != owner || f.View == ViewTeam && f.Team != team {
			continue
		}
		fields[k] = f.Value
	}
	b, err := json.Marshal(struct {
		Version uint64         `json:"version"`
		Fields  map[string]any `json:"fields"`
	}{s.version, fields})
	return b, s.version, err
}
