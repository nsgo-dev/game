package server

import (
	"errors"
	"time"

	netpkg "github.com/nsgo-dev/game/packages/game/net"
)

var (
	// ErrMessageTooLarge reports a packet larger than Limits.MaxMessageSize.
	ErrMessageTooLarge = errors.New("server: message too large")
	// ErrPendingLimit reports reliable bytes past Limits.MaxPendingReliable.
	ErrPendingLimit = errors.New("server: pending reliable data limit")
	// ErrInvalidInput reports input refused by Guard.Validate.
	ErrInvalidInput = errors.New("server: invalid input")
	// ErrPeerDisconnected reports a peer the guard has disconnected.
	ErrPeerDisconnected = errors.New("server: peer disconnected")
)

// LimitPolicy is what a guard does with a peer that exceeds its limits.
type LimitPolicy uint8

const (
	// LimitThrottle drops over-limit packets and keeps the peer connected.
	LimitThrottle LimitPolicy = iota
	// LimitDrop silently drops over-limit packets without a disconnect.
	LimitDrop
	// LimitDisconnect drops the packet and refuses the peer from then on.
	LimitDisconnect
)

// Limits bounds one peer's traffic. Zero values disable that bound.
type Limits struct {
	// PacketsPerSecond and BytesPerSecond are the sustained rates; Burst*
	// allow a short burst on top, defaulting to one second's rate.
	PacketsPerSecond float64
	BytesPerSecond   float64
	BurstPackets     float64
	BurstBytes       float64
	// MaxMessageSize rejects a single packet before it is buffered.
	MaxMessageSize int
	// MaxPendingReliable bounds reliable bytes reserved but not yet released.
	MaxPendingReliable int
}

// GuardAction is the result of Guard.Check.
type GuardAction uint8

const (
	GuardAllow GuardAction = iota
	GuardThrottle
	GuardDisconnect
)

// PeerStats counts one peer's traffic and refusals.
type PeerStats struct {
	Packets      uint64
	Bytes        uint64
	Dropped      uint64
	Violations   uint64
	PendingBytes int
}

// Guard enforces per-peer limits and validates inputs before a simulation sees
// them. It is safe on the server's packet loop and independent per peer.
type Guard struct {
	Limits Limits
	Policy LimitPolicy
	// Validate rejects an input with an error before the simulation sees it.
	// A non-nil error counts one violation for the peer.
	Validate func(netpkg.PeerID, netpkg.Packet) error
	// Now is the token-bucket clock; nil uses time.Now.
	Now func() time.Time

	peers map[netpkg.PeerID]*peerGuard
}

type peerGuard struct {
	stats     PeerStats
	last      time.Time
	packets   float64
	bytes     float64
	connected bool
	initial   bool
}

// NewGuard returns a guard with limits and a policy.
func NewGuard(limits Limits, policy LimitPolicy) *Guard {
	return &Guard{Limits: limits, Policy: policy, peers: make(map[netpkg.PeerID]*peerGuard)}
}

// Check accounts packet for peer and returns what the caller should do. It
// rejects an oversized packet before any buffering and runs Validate only for
// an admitted packet; a rejected input never reaches the caller's simulation.
func (g *Guard) Check(peer netpkg.PeerID, packet netpkg.Packet) (GuardAction, error) {
	if g == nil {
		return GuardAllow, nil
	}
	pg := g.peer(peer)
	if !pg.connected {
		return GuardDisconnect, ErrPeerDisconnected
	}
	size := len(packet.Payload)
	if g.Limits.MaxMessageSize > 0 && size > g.Limits.MaxMessageSize {
		pg.stats.Dropped++
		pg.stats.Violations++
		return g.reject(pg, ErrMessageTooLarge)
	}
	now := g.now()
	elapsed := now.Sub(pg.last).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	pg.last = now
	if g.Limits.PacketsPerSecond > 0 {
		pg.packets += g.Limits.PacketsPerSecond * elapsed
		burst := g.burstPackets()
		if pg.packets > burst {
			pg.packets = burst
		}
		if pg.packets < 1 {
			pg.stats.Dropped++
			return g.reject(pg, errors.New("server: packet rate limited"))
		}
	}
	if g.Limits.BytesPerSecond > 0 {
		pg.bytes += g.Limits.BytesPerSecond * elapsed
		burst := g.burstBytes()
		if pg.bytes > burst {
			pg.bytes = burst
		}
		if float64(size) > pg.bytes {
			pg.stats.Dropped++
			return g.reject(pg, errors.New("server: byte rate limited"))
		}
	}
	if g.Validate != nil {
		if err := g.Validate(peer, packet); err != nil {
			pg.stats.Dropped++
			pg.stats.Violations++
			return GuardThrottle, ErrInvalidInput
		}
	}
	if g.Limits.PacketsPerSecond > 0 {
		pg.packets--
	}
	if g.Limits.BytesPerSecond > 0 {
		pg.bytes -= float64(size)
	}
	pg.stats.Packets++
	pg.stats.Bytes += uint64(size)
	return GuardAllow, nil
}

func (g *Guard) reject(pg *peerGuard, err error) (GuardAction, error) {
	if g.Policy == LimitDisconnect {
		pg.connected = false
		return GuardDisconnect, err
	}
	return GuardThrottle, err
}

// ReserveReliable reserves n reliable bytes for peer, refusing past
// MaxPendingReliable.
func (g *Guard) ReserveReliable(peer netpkg.PeerID, n int) error {
	if g == nil || n <= 0 {
		return nil
	}
	pg := g.peer(peer)
	if g.Limits.MaxPendingReliable > 0 && pg.stats.PendingBytes+n > g.Limits.MaxPendingReliable {
		return ErrPendingLimit
	}
	pg.stats.PendingBytes += n
	return nil
}

// ReleaseReliable returns n reserved reliable bytes for peer.
func (g *Guard) ReleaseReliable(peer netpkg.PeerID, n int) {
	if g == nil || n <= 0 {
		return
	}
	pg := g.peer(peer)
	pg.stats.PendingBytes -= n
	if pg.stats.PendingBytes < 0 {
		pg.stats.PendingBytes = 0
	}
}

// Stats returns a peer's counters.
func (g *Guard) Stats(peer netpkg.PeerID) PeerStats {
	if g == nil {
		return PeerStats{}
	}
	if pg, ok := g.peers[peer]; ok {
		return pg.stats
	}
	return PeerStats{}
}

// Reset forgets a peer's buckets and counters, for a reconnecting peer.
func (g *Guard) Reset(peer netpkg.PeerID) {
	if g != nil {
		delete(g.peers, peer)
	}
}

func (g *Guard) peer(peer netpkg.PeerID) *peerGuard {
	if g.peers == nil {
		g.peers = make(map[netpkg.PeerID]*peerGuard)
	}
	pg := g.peers[peer]
	if pg == nil {
		pg = &peerGuard{connected: true, last: g.now()}
		if g.Limits.PacketsPerSecond > 0 {
			pg.packets = g.burstPackets()
		}
		if g.Limits.BytesPerSecond > 0 {
			pg.bytes = g.burstBytes()
		}
		g.peers[peer] = pg
	}
	return pg
}

func (g *Guard) burstPackets() float64 {
	if g.Limits.BurstPackets > 0 {
		return g.Limits.BurstPackets
	}
	if g.Limits.PacketsPerSecond > 0 {
		return g.Limits.PacketsPerSecond
	}
	return 0
}

func (g *Guard) burstBytes() float64 {
	if g.Limits.BurstBytes > 0 {
		return g.Limits.BurstBytes
	}
	if g.Limits.BytesPerSecond > 0 {
		return g.Limits.BytesPerSecond
	}
	return 0
}

func (g *Guard) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}
