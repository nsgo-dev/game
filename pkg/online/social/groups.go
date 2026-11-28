package social

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrGroupNotFound   = errors.New("social: group not found")
	ErrGroupPermission = errors.New("social: permission denied")
	ErrGroupBanned     = errors.New("social: account banned")
	ErrGroupFull       = errors.New("social: group full")
)

type Role uint8

const (
	Owner Role = iota + 1
	Admin
	Member
	Requested
	Banned
)

type Group struct {
	ID                      uint64
	Name, Description, Lang string
	Open                    bool
	MaxMembers              int
	Members                 map[string]Role
}
type Groups struct {
	mu     sync.Mutex
	next   uint64
	groups map[uint64]*Group
}

func NewGroups() *Groups { return &Groups{groups: make(map[uint64]*Group)} }
func (s *Groups) Create(owner, name string, max int, open bool) (*Group, error) {
	if owner == "" || name == "" {
		return nil, errors.New("social: invalid group")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	g := &Group{ID: s.next, Name: name, MaxMembers: max, Open: open, Members: map[string]Role{owner: Owner}}
	s.groups[g.ID] = g
	return g, nil
}
func (s *Groups) Join(id uint64, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[id]
	if g == nil {
		return ErrGroupNotFound
	}
	if g.Members[account] == Banned {
		return ErrGroupBanned
	}
	if len(g.Members) >= g.MaxMembers && g.MaxMembers > 0 {
		return ErrGroupFull
	}
	if g.Open {
		g.Members[account] = Member
	} else {
		g.Members[account] = Requested
	}
	return nil
}
func (s *Groups) Promote(id uint64, actor, target string) error {
	return s.setRole(id, actor, target, Admin)
}
func (s *Groups) Leave(id uint64, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[id]
	if g == nil {
		return ErrGroupNotFound
	}
	if g.Members[account] == Owner {
		return ErrGroupPermission
	}
	delete(g.Members, account)
	return nil
}
func (s *Groups) setRole(id uint64, actor, target string, role Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[id]
	if g == nil {
		return ErrGroupNotFound
	}
	if g.Members[actor] != Owner && g.Members[actor] != Admin {
		return ErrGroupPermission
	}
	if g.Members[target] == Banned {
		return ErrGroupBanned
	}
	g.Members[target] = role
	return nil
}
func (s *Groups) Search(prefix string) []Group {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Group{}
	for _, g := range s.groups {
		if strings.HasPrefix(strings.ToLower(g.Name), strings.ToLower(prefix)) {
			c := *g
			c.Members = map[string]Role{}
			for a, r := range g.Members {
				c.Members[a] = r
			}
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
