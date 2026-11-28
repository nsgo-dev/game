package social

import (
	"errors"
	"sort"
	"sync"
	"time"
)

type EdgeState string

const (
	StateFriend         EdgeState = "friend"
	StateInviteSent     EdgeState = "invite_sent"
	StateInviteReceived EdgeState = "invite_received"
	StateBlocked        EdgeState = "blocked"
)

type Edge struct {
	Account, Other string
	State          EdgeState
	Updated        time.Time
}

var ErrBlocked = errors.New("social: account blocked")
var ErrLimit = errors.New("social: friend limit")

type Graph struct {
	mu                sync.Mutex
	edges             map[string]map[string]Edge
	MaxFriends        int
	MaxPendingInvites int
}

func NewGraph() *Graph {
	return &Graph{edges: map[string]map[string]Edge{}, MaxFriends: 100, MaxPendingInvites: 50}
}
func (g *Graph) AddFriend(a, b string) error {
	if a == "" || b == "" || a == b {
		return errors.New("social: invalid accounts")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.edges[a] == nil {
		g.edges[a] = map[string]Edge{}
	}
	if g.edges[a][b].State == StateBlocked || g.edges[b][a].State == StateBlocked {
		return ErrBlocked
	}
	if g.edges[b][a].State == StateInviteSent {
		g.set(a, b, StateFriend)
		g.set(b, a, StateFriend)
		return nil
	}
	if g.count(a, StateFriend) >= g.MaxFriends {
		return ErrLimit
	}
	g.set(a, b, StateInviteSent)
	g.set(b, a, StateInviteReceived)
	return nil
}
func (g *Graph) Block(a, b string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.set(a, b, StateBlocked)
	g.set(b, a, StateBlocked)
}
func (g *Graph) Unblock(a, b string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.edges[a], b)
	delete(g.edges[b], a)
}
func (g *Graph) Friends(a string, state EdgeState) []Edge {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := []Edge{}
	for _, e := range g.edges[a] {
		if state == "" || e.State == state {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Other < out[j].Other })
	return out
}
func (g *Graph) Blocked(a, b string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.edges[a][b].State == StateBlocked
}
func (g *Graph) set(a, b string, s EdgeState) {
	if g.edges[a] == nil {
		g.edges[a] = map[string]Edge{}
	}
	g.edges[a][b] = Edge{a, b, s, time.Now().UTC()}
}
func (g *Graph) count(a string, s EdgeState) int {
	n := 0
	for _, e := range g.edges[a] {
		if e.State == s {
			n++
		}
	}
	return n
}
