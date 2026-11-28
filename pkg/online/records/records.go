// Package records provides a server-authoritative, versioned JSON record core.
package records

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrChanged = errors.New("records: version conflict")
var ErrNotFound = errors.New("records: not found")
var ErrPermission = errors.New("records: permission denied")

type Permission uint8

const (
	Private Permission = iota
	OwnerRead
	PublicRead
)

type Record struct {
	Owner, Collection, Key string
	Value                  json.RawMessage
	Version                int64
	Read, Write            Permission
	Updated                time.Time
}
type Put struct {
	Record   Record
	Expected int64
}
type Delete struct {
	Owner, Collection, Key string
	Expected               int64
}
type Tx struct {
	Puts    []Put
	Deletes []Delete
}
type Store struct {
	mu   sync.RWMutex
	data map[string]Record
	now  func() time.Time
}

func New(now func() time.Time) *Store { return &Store{data: map[string]Record{}, now: now} }
func (s *Store) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
func id(o, c, k string) string { return o + "\x00" + c + "\x00" + k }
func (s *Store) Get(ctx context.Context, o, c, k, reader string) (Record, error) {
	if e := ctx.Err(); e != nil {
		return Record{}, e
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data[id(o, c, k)]
	if !ok {
		return Record{}, ErrNotFound
	}
	if r.Owner != reader && r.Read != PublicRead {
		return Record{}, ErrPermission
	}
	r.Value = append(json.RawMessage(nil), r.Value...)
	return r, nil
}
func (s *Store) Apply(ctx context.Context, tx Tx) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range tx.Puts {
		cur, ok := s.data[id(p.Record.Owner, p.Record.Collection, p.Record.Key)]
		if (p.Expected == 0 && ok) || (p.Expected > 0 && (!ok || cur.Version != p.Expected)) {
			return ErrChanged
		}
	}
	for _, d := range tx.Deletes {
		cur, ok := s.data[id(d.Owner, d.Collection, d.Key)]
		if d.Expected == 0 && ok || d.Expected > 0 && (!ok || cur.Version != d.Expected) {
			return ErrChanged
		}
	}
	for _, p := range tx.Puts {
		r := p.Record
		if cur, ok := s.data[id(r.Owner, r.Collection, r.Key)]; ok {
			r.Version = cur.Version + 1
		} else {
			r.Version = 1
		}
		r.Updated = s.clock()
		r.Value = append(json.RawMessage(nil), r.Value...)
		s.data[id(r.Owner, r.Collection, r.Key)] = r
	}
	for _, d := range tx.Deletes {
		delete(s.data, id(d.Owner, d.Collection, d.Key))
	}
	return nil
}
func (s *Store) List(ctx context.Context, o, c, reader string) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Record{}
	for _, r := range s.data {
		if r.Owner == o && r.Collection == c && (r.Owner == reader || r.Read == PublicRead) {
			r.Value = append(json.RawMessage(nil), r.Value...)
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
