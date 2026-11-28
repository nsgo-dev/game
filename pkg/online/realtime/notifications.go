package realtime

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrNotFound = errors.New("realtime: notification not found")

type Notification struct {
	ID                        uint64 `json:"id"`
	To, Code, Subject, Sender string
	Content                   json.RawMessage
	Persistent                bool
	Created                   time.Time
}
type Store struct {
	mu          sync.Mutex
	next        uint64
	online      map[string]bool
	persistent  map[string][]Notification
	subscribers map[string][]chan Notification
}

func NewStore() *Store {
	return &Store{online: make(map[string]bool), persistent: make(map[string][]Notification), subscribers: make(map[string][]chan Notification)}
}
func (s *Store) SetOnline(account string, online bool) {
	s.mu.Lock()
	s.online[account] = online
	s.mu.Unlock()
}
func (s *Store) Notify(n Notification) (Notification, error) {
	if s == nil || n.To == "" || n.Code == "" {
		return Notification{}, errors.New("realtime: invalid notification")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	n.ID = s.next
	if n.Created.IsZero() {
		n.Created = time.Now().UTC()
	}
	if n.Persistent {
		s.persistent[n.To] = append(s.persistent[n.To], n)
	}
	if s.online[n.To] {
		for _, ch := range s.subscribers[n.To] {
			select {
			case ch <- n:
			default:
			}
		}
	}
	return n, nil
}
func (s *Store) History(account string, before uint64, limit int) []Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := append([]Notification(nil), s.persistent[account]...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	out := rows[:0]
	for _, n := range rows {
		if before > 0 && n.ID >= before {
			continue
		}
		if limit > 0 && len(out) >= limit {
			break
		}
		out = append(out, n)
	}
	return out
}
func (s *Store) Delete(account string, id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.persistent[account]
	for i, n := range rows {
		if n.ID == id {
			s.persistent[account] = append(rows[:i], rows[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
func (s *Store) Subscribe(account string, buffer int) (<-chan Notification, func()) {
	if buffer <= 0 {
		buffer = 16
	}
	ch := make(chan Notification, buffer)
	s.mu.Lock()
	s.subscribers[account] = append(s.subscribers[account], ch)
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, c := range s.subscribers[account] {
			if c == ch {
				s.subscribers[account] = append(s.subscribers[account][:i], s.subscribers[account][i+1:]...)
				break
			}
		}
		close(ch)
	}
}
