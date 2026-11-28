package gameserver

import (
	"errors"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

// ErrRoomInboxFull reports a ChannelReliableOrdered packet refused because its member already has RoomLimits.PerPeer packets queued in the room and none of them is unreliable. Dropping a reliable packet would break its ordering guarantee, so the packet loop should disconnect that peer, as a WebSocketServer ends a client past its reliable cap.
var ErrRoomInboxFull = errors.New("gameserver: room inbox full")

const (
	// DefaultRoomInboxPerPeer is the most packets a room queues for one member when RoomLimits.PerPeer is zero.
	DefaultRoomInboxPerPeer = 256
	// DefaultRoomInboxWarning is the room backlog, across all members, that counts a warning when RoomLimits.Warning is zero.
	DefaultRoomInboxWarning = 1024
)

// RoomLimits bounds the packets each room keeps until Recv or Drain takes them, so a match that falls behind its packet loop holds a fixed amount of memory instead of growing for as long as it stays slow.
//
// Each member's queue holds at most PerPeer packets. A packet that arrives at a full queue evicts that member's oldest unreliable packet; when the queue holds only reliable packets, an arriving unreliable packet is the one dropped and an arriving reliable packet is refused with ErrRoomInboxFull. One member's flood therefore evicts only its own packets.
type RoomLimits struct {
	// PerPeer is the most packets a room queues for one member. Zero uses DefaultRoomInboxPerPeer; a negative value removes the cap.
	PerPeer int
	// Warning is the backlog, across all of a room's members, that counts a warning in RoomInboxStats.Warnings: once when the backlog grows past it, and again only after it has fallen to half of it. Zero uses DefaultRoomInboxWarning; a negative value turns the warning off.
	Warning int
}

// RoomInboxStats reports a room's inbox. The counters only grow.
type RoomInboxStats struct {
	// Queued is the number of packets waiting for Recv or Drain, across all members.
	Queued int
	// Dropped counts the unreliable packets dropped because their member's queue was full.
	Dropped uint64
	// Refused counts the reliable packets Deliver refused with ErrRoomInboxFull.
	Refused uint64
	// Warnings counts the times Queued grew past RoomLimits.Warning: once per crossing, not once per packet.
	Warnings uint64
	// Backlog is true from the moment Queued passes RoomLimits.Warning until it falls to half of it.
	Backlog bool
}

// SetLimits changes the inbox limits of every open room and of the rooms Create opens later. Lowering PerPeer below what a member has queued drops that member's oldest unreliable packets at once and counts them; reliable packets already queued are kept.
func (rs *Rooms) SetLimits(l RoomLimits) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.limits = l
	for _, r := range rs.rooms {
		r.mu.Lock()
		r.applyLimits(l)
		r.mu.Unlock()
	}
}

// Drain hands up to budget packets per member to fn, round robin in join order: each member's oldest packet, then each member's next, so a member that floods the room cannot take the turn of the others. It returns how many packets it handed. A budget of zero or less hands none. fn runs without the room's lock held and may call the room's methods.
func (r *Room) Drain(budget int, fn func(netplay.Packet)) int {
	if budget <= 0 {
		return 0
	}
	r.mu.Lock()
	var batch []netplay.Packet
	for round := 0; round < budget; round++ {
		took := false
		for _, peer := range r.order {
			if p, ok := r.popLocked(peer); ok {
				batch = append(batch, p)
				took = true
			}
		}
		if !took {
			break
		}
	}
	r.mu.Unlock()
	for _, p := range batch {
		fn(p)
	}
	return len(batch)
}

// InboxStats reads the room's inbox counters.
func (r *Room) InboxStats() RoomInboxStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return RoomInboxStats{Queued: r.queued, Dropped: r.dropped, Refused: r.refused, Warnings: r.warnings, Backlog: r.backlog}
}

// applyLimits resolves l into the room and trims queues past the new cap. The caller holds r.mu.
func (r *Room) applyLimits(l RoomLimits) {
	r.perPeer = resolveRoomLimit(l.PerPeer, DefaultRoomInboxPerPeer)
	r.warning = resolveRoomLimit(l.Warning, DefaultRoomInboxWarning)
	if r.perPeer == 0 {
		return
	}
	for peer, queue := range r.inbox {
		for len(queue) > r.perPeer {
			i := oldestUnreliable(queue)
			if i < 0 {
				break
			}
			queue = append(queue[:i], queue[i+1:]...)
			r.queued--
			r.dropped++
		}
		r.inbox[peer] = queue
	}
	r.updateBacklog()
}

// enqueueLocked queues p for member from within the per-member cap. The caller holds r.mu.
func (r *Room) enqueueLocked(from netplay.PeerID, p netplay.Packet) error {
	queue := r.inbox[from]
	if r.perPeer > 0 && len(queue) >= r.perPeer {
		i := oldestUnreliable(queue)
		switch {
		case i >= 0:
			queue = append(queue[:i], queue[i+1:]...)
			r.queued--
			r.dropped++
		case p.Channel == netplay.ChannelUnreliable:
			r.dropped++
			return nil
		default:
			r.refused++
			return ErrRoomInboxFull
		}
	}
	r.inbox[from] = append(queue, p)
	r.queued++
	r.updateBacklog()
	return nil
}

// popLocked removes and returns peer's oldest queued packet. The caller holds r.mu.
func (r *Room) popLocked(peer netplay.PeerID) (netplay.Packet, bool) {
	queue := r.inbox[peer]
	if len(queue) == 0 {
		return netplay.Packet{}, false
	}
	p := queue[0]
	if len(queue) == 1 {
		delete(r.inbox, peer)
	} else {
		queue[0] = netplay.Packet{}
		r.inbox[peer] = queue[1:]
	}
	r.queued--
	r.updateBacklog()
	return p, true
}

// dropQueueLocked discards peer's queued packets without counting them as dropped. The caller holds r.mu.
func (r *Room) dropQueueLocked(peer netplay.PeerID) {
	r.queued -= len(r.inbox[peer])
	delete(r.inbox, peer)
	r.updateBacklog()
}

// clearInboxLocked discards every queued packet. The caller holds r.mu.
func (r *Room) clearInboxLocked() {
	clear(r.inbox)
	r.queued = 0
	r.updateBacklog()
}

// updateBacklog counts a warning when the backlog passes the threshold and rearms once it has fallen to half of it. The caller holds r.mu.
func (r *Room) updateBacklog() {
	if r.warning == 0 {
		r.backlog = false
		return
	}
	switch {
	case !r.backlog && r.queued > r.warning:
		r.backlog = true
		r.warnings++
	case r.backlog && r.queued <= r.warning/2:
		r.backlog = false
	}
}

// oldestUnreliable returns the index of the first ChannelUnreliable packet in queue, or -1.
func oldestUnreliable(queue []netplay.Packet) int {
	for i, p := range queue {
		if p.Channel == netplay.ChannelUnreliable {
			return i
		}
	}
	return -1
}

// resolveRoomLimit returns v, def when v is zero, or 0 for none when v is negative.
func resolveRoomLimit(v, def int) int {
	switch {
	case v < 0:
		return 0
	case v == 0:
		return def
	}
	return v
}
