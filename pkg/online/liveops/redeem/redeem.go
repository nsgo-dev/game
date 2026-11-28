// Package redeem provides a concurrency-safe in-memory code redemption core.
package redeem

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrCodeUnknown = errors.New("redeem: unknown code")
	ErrCodeUsed    = errors.New("redeem: code used")
	ErrCodeExpired = errors.New("redeem: code expired")
	ErrCodeLimit   = errors.New("redeem: account limit reached")
)

type Code struct {
	Value         string
	Reward        string
	Expires       time.Time
	MaxUses       int
	MaxPerAccount int
}
type Store struct {
	mu    sync.Mutex
	codes map[string]Code
	used  map[string]map[string]int
}

func New() *Store { return &Store{codes: map[string]Code{}, used: map[string]map[string]int{}} }
func (s *Store) Add(c Code) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.Value = strings.ToUpper(strings.TrimSpace(c.Value))
	s.codes[c.Value] = c
}
func (s *Store) Redeem(account, code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return "", ErrCodeUnknown
	}
	if !c.Expires.IsZero() && !time.Now().Before(c.Expires) {
		return "", ErrCodeExpired
	}
	if s.used[c.Value] == nil {
		s.used[c.Value] = map[string]int{}
	}
	n := s.used[c.Value][account]
	total := 0
	for _, x := range s.used[c.Value] {
		total += x
	}
	if c.MaxUses > 0 && total >= c.MaxUses {
		return "", ErrCodeUsed
	}
	if c.MaxPerAccount > 0 && n >= c.MaxPerAccount {
		return "", ErrCodeLimit
	}
	s.used[c.Value][account]++
	return c.Reward, nil
}
