package gameserver

import (
	"context"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

// LeaveReason explains why a peer left a room.
type LeaveReason uint8

const (
	LeaveQuit LeaveReason = iota
	LeaveDropped
	LeaveKicked
	LeaveReplaced
)

func (r LeaveReason) String() string {
	switch r {
	case LeaveQuit:
		return "quit"
	case LeaveDropped:
		return "dropped"
	case LeaveKicked:
		return "kicked"
	case LeaveReplaced:
		return "replaced"
	default:
		return "unknown"
	}
}

// MatchHandler observes and controls a room match lifecycle. Implementations
// must be safe for callbacks from the room's owning server loop.
type MatchHandler interface {
	JoinAttempt(peer netplay.PeerID, account string, meta []byte) (accept bool, reason string)
	Joined(peer netplay.PeerID)
	Left(peer netplay.PeerID, reason LeaveReason)
	Signal(data []byte) (reply []byte, err error)
	Terminate(grace time.Duration)
}

// Signal invokes a room handler's external signal and returns its reply.
func (r *Room) Signal(ctx context.Context, data []byte) ([]byte, error) {
	if r == nil || r.handler == nil {
		return nil, ErrRoomEnded
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return r.handler.Signal(append([]byte(nil), data...))
}

// SetHandler attaches the lifecycle handler. It should be called before joins.
func (r *Room) SetHandler(handler MatchHandler) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.handler = handler
	r.mu.Unlock()
}

// Terminate gives the match a shutdown grace period before ending the room.
func (r *Room) Terminate(grace time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	h := r.handler
	r.mu.Unlock()
	if h != nil {
		h.Terminate(grace)
	}
	r.End()
}
