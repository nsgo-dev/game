package server

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

var (
	// ErrInvalidTicket reports a ticket with no player or a non-positive room size.
	ErrInvalidTicket = errors.New("server: invalid matchmaking ticket")
	// ErrDuplicateTicket reports a ticket ID already waiting in the queue.
	ErrDuplicateTicket = errors.New("server: duplicate matchmaking ticket")
)

// Ticket is one matchmaking request. A party stays together in a room.
type Ticket struct {
	// ID identifies the ticket for cancellation and in a Match.
	ID uint64
	// Mode and Region group tickets that may meet.
	Mode, Region string
	// Rating is the party's skill rating; the window widens from it as the
	// ticket waits.
	Rating float64
	// Party lists the players in the ticket. It must hold at least one player
	// and is placed whole or not at all.
	Party []string
	// EnqueuedAt is when the ticket started waiting; zero uses the time it was
	// enqueued.
	EnqueuedAt time.Time
}

// Match is a set of tickets assigned to one room.
type Matchup struct {
	Mode, Region string
	TicketIDs    []uint64
	Players      []string
}

// Rule chooses matches from a group of tickets that share a mode and region.
// window reports a ticket's current skill window at the rule's time. The
// default rule fills rooms with parties whose ratings fit; a game can replace
// it for custom matching.
type Rule func(tickets []*Ticket, roomSize int, window func(*Ticket) float64) []Matchup

// Matchmaker queues tickets per mode and region and groups them into rooms.
// The zero value is not ready; construct one with NewMatchmaker.
type Matchmaker struct {
	// RoomSize is how many players one match seats.
	RoomSize int
	// BaseWindow is the initial allowed rating difference (default 100).
	BaseWindow float64
	// WindowGrowthPerSecond widens the window per second waited (default 10).
	WindowGrowthPerSecond float64
	// MaxWindow caps the widened window when positive.
	MaxWindow float64
	// Now is the clock used for waiting time; nil uses time.Now.
	Now func() time.Time
	// Rule replaces the default matching rule when set.

	Rule  Rule
	queue map[uint64]*Ticket
}

// NewMatchmaker returns a matchmaker for rooms of roomSize players.
func NewMatchmaker(roomSize int) *Matchmaker {
	return &Matchmaker{
		RoomSize:              roomSize,
		BaseWindow:            100,
		WindowGrowthPerSecond: 10,
		queue:                 make(map[uint64]*Ticket),
	}
}

// Len reports how many tickets are waiting.
func (m *Matchmaker) Len() int {
	if m == nil {
		return 0
	}
	return len(m.queue)
}

// Enqueue adds a ticket, defaulting its EnqueuedAt to the matchmaker's clock.
func (m *Matchmaker) Enqueue(t Ticket) error {
	if m == nil || m.RoomSize <= 0 {
		return ErrInvalidTicket
	}
	if len(t.Party) == 0 {
		return fmt.Errorf("%w: ticket %d has no players", ErrInvalidTicket, t.ID)
	}
	if _, ok := m.queue[t.ID]; ok {
		return fmt.Errorf("%w: %d", ErrDuplicateTicket, t.ID)
	}
	if t.EnqueuedAt.IsZero() {
		t.EnqueuedAt = m.now()
	}
	if t.Rating != t.Rating { // NaN can never enter a window
		return fmt.Errorf("%w: ticket %d has a NaN rating", ErrInvalidTicket, t.ID)
	}
	if m.queue == nil {
		m.queue = make(map[uint64]*Ticket)
	}
	cp := t
	cp.Party = append([]string(nil), t.Party...)
	m.queue[t.ID] = &cp
	return nil
}

// Cancel removes ticket id and reports whether it was waiting.
func (m *Matchmaker) Cancel(id uint64) bool {
	if m == nil {
		return false
	}
	if _, ok := m.queue[id]; !ok {
		return false
	}
	delete(m.queue, id)
	return true
}

// Waiting returns copies of the queued tickets, oldest first.
func (m *Matchmaker) Waiting() []Ticket {
	if m == nil {
		return nil
	}
	out := make([]Ticket, 0, len(m.queue))
	for _, t := range m.queue {
		cp := *t
		cp.Party = append([]string(nil), t.Party...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].EnqueuedAt.Equal(out[j].EnqueuedAt) {
			return out[i].EnqueuedAt.Before(out[j].EnqueuedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Match forms rooms from the waiting tickets, removes their tickets and
// returns the matches. Tickets that cannot fill a room stay queued.
func (m *Matchmaker) Match() []Matchup {
	if m == nil || len(m.queue) == 0 {
		return nil
	}
	now := m.now()
	window := func(t *Ticket) float64 {
		wait := now.Sub(t.EnqueuedAt).Seconds()
		if wait < 0 {
			wait = 0
		}
		w := m.BaseWindow + m.WindowGrowthPerSecond*wait
		if w < 0 {
			w = 0
		}
		if m.MaxWindow > 0 && w > m.MaxWindow {
			w = m.MaxWindow
		}
		return w
	}

	groups := make(map[[2]string][]*Ticket)
	var order [][2]string
	for _, t := range m.queue {
		key := [2]string{t.Mode, t.Region}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], t)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i][0] != order[j][0] {
			return order[i][0] < order[j][0]
		}
		return order[i][1] < order[j][1]
	})

	var out []Matchup
	for _, key := range order {
		rule := m.Rule
		if rule == nil {
			rule = defaultRule
		}
		for _, room := range rule(groups[key], m.RoomSize, window) {
			out = append(out, room)
			for _, id := range room.TicketIDs {
				delete(m.queue, id)
			}
		}
	}
	return out
}

// defaultRule fills rooms oldest-first, adding whole parties whose ratings fit
// within both the oldest ticket's current window and their own.
func defaultRule(tickets []*Ticket, roomSize int, window func(*Ticket) float64) []Matchup {
	if roomSize <= 0 || len(tickets) == 0 {
		return nil
	}
	remaining := append([]*Ticket(nil), tickets...)
	sort.SliceStable(remaining, func(i, j int) bool {
		if !remaining[i].EnqueuedAt.Equal(remaining[j].EnqueuedAt) {
			return remaining[i].EnqueuedAt.Before(remaining[j].EnqueuedAt)
		}
		return remaining[i].ID < remaining[j].ID
	})

	var matches []Matchup
	for i := 0; i < len(remaining); {
		anchor := remaining[i]
		if len(anchor.Party) > roomSize {
			i++
			continue
		}
		room := []*Ticket{anchor}
		count := len(anchor.Party)
		for j := i + 1; j < len(remaining) && count < roomSize; j++ {
			t := remaining[j]
			if len(t.Party) == 0 || count+len(t.Party) > roomSize {
				continue
			}
			limit := math.Min(window(anchor), window(t))
			if math.Abs(t.Rating-anchor.Rating) > limit {
				continue
			}
			room = append(room, t)
			count += len(t.Party)
			remaining = append(remaining[:j], remaining[j+1:]...)
			j--
		}
		if count != roomSize {
			i++
			continue
		}
		matches = append(matches, combineMatchup(room))
		remaining = append(remaining[:i], remaining[i+1:]...)
	}
	return matches
}

func combineMatchup(room []*Ticket) Matchup {
	result := Matchup{Mode: room[0].Mode, Region: room[0].Region}
	for _, t := range room {
		result.TicketIDs = append(result.TicketIDs, t.ID)
		result.Players = append(result.Players, t.Party...)
	}
	return result
}

func (m *Matchmaker) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
