package server

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

var (
	// ErrRoomNotFound reports a room ID or code that names no open room.
	ErrRoomNotFound = errors.New("server: room not found")
	// ErrRoomFull reports a join to a room already at capacity.
	ErrRoomFull = errors.New("server: room full")
	// ErrRoomEnded reports an action on a room whose match has ended.
	ErrRoomEnded = errors.New("server: room match ended")
	// ErrNotInRoom reports a packet from a peer that is not a room member.
	ErrNotInRoom = errors.New("server: peer is not in this room")
)

// RoomState is a room's match lifecycle.
type RoomState uint8

const (
	RoomWaiting RoomState = iota // open, match not started
	RoomRunning                  // match in progress
	RoomEnded                    // match ended, next Start begins a new one
)

func (s RoomState) String() string {
	switch s {
	case RoomWaiting:
		return "waiting"
	case RoomRunning:
		return "running"
	case RoomEnded:
		return "ended"
	default:
		return "unknown"
	}
}

// RoomInfo is a snapshot of a room for listing.
type RoomInfo struct {
	ID       uint64
	Capacity int
	State    RoomState
	Epoch    uint64
	Tick     uint64
	Members  int
	Private  bool
}

// Join reports a peer's admission and, for a running match, the full snapshot
// it must apply before accepting deltas.
type Join struct {
	Room     *Room
	Epoch    uint64
	Tick     uint64
	Snapshot []byte
}

// Room groups peers for one match. A room owns its members and inbox: packets
// delivered through one room are never visible to another.
type Room struct {
	id       uint64
	code     string
	capacity int
	private  bool
	created  time.Time
	emptyAt  time.Time // when the last member left; zero while occupied

	mu      sync.Mutex
	state   RoomState
	epoch   uint64
	tick    uint64
	members map[netplay.PeerID]struct{}
	inbox   map[netplay.PeerID][]netplay.Packet
}

// ID returns the room's manager-assigned ID.
func (r *Room) ID() uint64 { return r.id }

// Code returns the join code of a private room, or "" for a public one.
func (r *Room) Code() string { return r.code }

// Capacity returns the maximum number of peers.
func (r *Room) Capacity() int { return r.capacity }

// State returns the match lifecycle state.
func (r *Room) State() RoomState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Epoch returns the current match epoch. Every Start begins a new epoch.
func (r *Room) Epoch() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.epoch
}

// Tick returns the room's current fixed-step tick.
func (r *Room) Tick() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tick
}

// Advance increments the room's tick while a match runs.
func (r *Room) Advance() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == RoomRunning {
		r.tick++
	}
}

// Members returns the current peers in admission order not kept.
func (r *Room) Members() []netplay.PeerID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]netplay.PeerID, 0, len(r.members))
	for p := range r.members {
		out = append(out, p)
	}
	return out
}

// Start begins a match, or starts the next one after End: the epoch advances
// and the tick resets to zero so every client can drop the previous match's
// tick state without waiting for the counter to wrap past it.
func (r *Room) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.members) == 0 {
		return ErrRoomNotFound
	}
	r.state = RoomRunning
	r.epoch++
	r.tick = 0
	return nil
}

// End ends the running match. The room stays open so its peers can see the
// result; a later Start begins the next epoch.
func (r *Room) End() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = RoomEnded
}

// Deliver routes a packet from a member into this room's inbox. A peer outside
// the room cannot inject packets, so one room's traffic never reaches another.
func (r *Room) Deliver(from netplay.PeerID, p netplay.Packet) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == RoomEnded {
		return ErrRoomEnded
	}
	if _, ok := r.members[from]; !ok {
		return ErrNotInRoom
	}
	r.inbox[from] = append(r.inbox[from], p)
	return nil
}

// Recv pops the next packet delivered by peer. Non-members and empty inboxes
// report false.
func (r *Room) Recv(peer netplay.PeerID) (netplay.Packet, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	queue := r.inbox[peer]
	if len(queue) == 0 {
		return netplay.Packet{}, false
	}
	p := queue[0]
	if len(queue) == 1 {
		delete(r.inbox, peer)
	} else {
		r.inbox[peer] = queue[1:]
	}
	return p, true
}

// Rooms manages the rooms of one server process.
type Rooms struct {
	// EmptyTimeout is how long an empty room stays open before Tick closes it.
	EmptyTimeout time.Duration
	// Snapshot, when set, returns the full state a late joiner needs before
	// deltas. It is called for a join to a running match.
	Snapshot func(*Room) []byte
	// CodeGenerator makes private room codes; nil uses six random letters.
	CodeGenerator func() string
	// Now is the clock for empty-room timeouts; nil uses time.Now.
	Now func() time.Time

	mu     sync.Mutex
	nextID uint64
	rooms  map[uint64]*Room
	codes  map[string]*Room
}

// NewRooms returns an empty room manager.
func NewRooms() *Rooms {
	return &Rooms{rooms: make(map[uint64]*Room), codes: make(map[string]*Room)}
}

// Create opens a room. capacity <= 0 uses four players. A private room gets a
// code that JoinCode accepts.
func (rs *Rooms) Create(capacity int, private bool) (*Room, error) {
	if capacity <= 0 {
		capacity = 4
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.rooms == nil {
		rs.rooms = make(map[uint64]*Room)
		rs.codes = make(map[string]*Room)
	}
	rs.nextID++
	r := &Room{
		id:       rs.nextID,
		capacity: capacity,
		private:  private,
		created:  rs.now(),
		state:    RoomWaiting,
		members:  make(map[netplay.PeerID]struct{}),
		inbox:    make(map[netplay.PeerID][]netplay.Packet),
	}
	if private {
		for attempt := 0; attempt < 8; attempt++ {
			code := rs.code()
			if code == "" {
				return nil, errors.New("server: empty room code")
			}
			if _, exists := rs.codes[code]; !exists {
				r.code = code
				break
			}
		}
		if r.code == "" {
			return nil, errors.New("server: could not allocate a room code")
		}
	}
	rs.rooms[r.id] = r
	if r.code != "" {
		rs.codes[r.code] = r
	}
	return r, nil
}

// Join admits peer to room id. A join to a running match carries the full
// snapshot from Snapshot, so the joiner applies it before any delta.
func (rs *Rooms) Join(id uint64, peer netplay.PeerID) (Join, error) {
	rs.mu.Lock()
	r, ok := rs.rooms[id]
	rs.mu.Unlock()
	if !ok {
		return Join{}, ErrRoomNotFound
	}
	return rs.join(r, peer)
}

// JoinCode admits peer to the private room with code.
func (rs *Rooms) JoinCode(code string, peer netplay.PeerID) (Join, error) {
	rs.mu.Lock()
	r, ok := rs.codes[code]
	rs.mu.Unlock()
	if !ok {
		return Join{}, ErrRoomNotFound
	}
	return rs.join(r, peer)
}

func (rs *Rooms) join(r *Room, peer netplay.PeerID) (Join, error) {
	r.mu.Lock()
	if r.state == RoomEnded {
		r.mu.Unlock()
		return Join{}, ErrRoomEnded
	}
	if _, exists := r.members[peer]; !exists {
		if len(r.members) >= r.capacity {
			r.mu.Unlock()
			return Join{}, fmt.Errorf("%w: room %d seats %d", ErrRoomFull, r.id, r.capacity)
		}
		r.members[peer] = struct{}{}
	}
	r.emptyAt = time.Time{}
	state, epoch, tick := r.state, r.epoch, r.tick
	r.mu.Unlock()

	join := Join{Room: r, Epoch: epoch, Tick: tick}
	if state == RoomRunning && rs.Snapshot != nil {
		join.Snapshot = rs.Snapshot(r)
	}
	return join, nil
}

// Leave removes peer from room id and reports whether it was a member. The
// room closes after EmptyTimeout once its last member leaves.
func (rs *Rooms) Leave(id uint64, peer netplay.PeerID) bool {
	rs.mu.Lock()
	r, ok := rs.rooms[id]
	rs.mu.Unlock()
	if !ok {
		return false
	}
	r.mu.Lock()
	_, member := r.members[peer]
	delete(r.members, peer)
	delete(r.inbox, peer)
	if member && len(r.members) == 0 {
		r.emptyAt = rs.now()
	}
	r.mu.Unlock()
	return member
}

// List returns open rooms ordered by ID.
func (rs *Rooms) List() []RoomInfo {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]RoomInfo, 0, len(rs.rooms))
	for _, r := range rs.rooms {
		r.mu.Lock()
		out = append(out, RoomInfo{
			ID: r.id, Capacity: r.capacity, State: r.state, Epoch: r.epoch,
			Tick: r.tick, Members: len(r.members), Private: r.private,
		})
		r.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Tick closes rooms that have been empty for at least EmptyTimeout and returns
// their IDs. A zero EmptyTimeout closes them as soon as Tick next runs.
func (rs *Rooms) Tick() []uint64 {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	now := rs.now()
	var closed []uint64
	for id, r := range rs.rooms {
		r.mu.Lock()
		empty := len(r.members) == 0 && !r.emptyAt.IsZero() && now.Sub(r.emptyAt) >= rs.EmptyTimeout
		r.mu.Unlock()
		if !empty {
			continue
		}
		delete(rs.rooms, id)
		if r.code != "" {
			delete(rs.codes, r.code)
		}
		closed = append(closed, id)
	}
	return closed
}

func (rs *Rooms) now() time.Time {
	if rs.Now != nil {
		return rs.Now()
	}
	return time.Now()
}

func (rs *Rooms) code() string {
	if rs.CodeGenerator != nil {
		return rs.CodeGenerator()
	}
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, len(b))
	for i := range b {
		out[i] = letters[int(b[i])%len(letters)]
	}
	return string(out)
}
