package netplay

const (
	// DefaultOutboxFrames is the most frames a WebSocketServer or WebTransportServer holds for one client when OutboxLimits.Frames is zero, as many as a Relay holds for one member.
	DefaultOutboxFrames = 256
	// DefaultOutboxBytes is the most bytes of frames a WebSocketServer or WebTransportServer holds for one client when OutboxLimits.Bytes is zero.
	DefaultOutboxBytes = 4 << 20
)

// OutboxLimits bounds the frames a WebSocketServer or WebTransportServer holds for one client before they are written to its connection.
//
// Send never writes to a client's connection itself: it queues the frame in the client's outbox and returns, and a goroutine of the client's own writes the queue out, so a client that reads slowly, or stops reading, delays only its own frames and never the caller's tick or the other clients. A WebTransportServer gives each client a second outbox, with the same limits, for its ChannelUnreliable datagrams, which QUIC sends apart from the reliable stream. A frame that would take a client's outbox past Frames frames or Bytes bytes first makes room by dropping that client's oldest queued ChannelUnreliable frames. A ChannelUnreliable frame that still does not fit is dropped itself, and a ChannelReliableOrdered frame that does not fit ends the client's session: dropping it would break the channel's order, so NextEvent reports PeerLeft with the reason "outbox full" and Send returns ErrDisconnected for that client. A frame always fits an empty outbox, whatever its size.
//
// Send therefore reports only what it knows when it queues: ErrDisconnected, ErrUnknownPeer, ErrPayloadTooLarge and ErrClosed. A write that fails, or that takes longer than the write timeout (10 seconds), closes the client's connection rather than leave part of a frame on the wire, and NextEvent reports PeerDropped as for any lost connection; the frames still queued for it are discarded.
//
// WebSocketServer.Kick and Close let each client's writer flush the frames already queued for it before the close frame, for at most a second, and cut off the connection of a client that has not taken them by then.
type OutboxLimits struct {
	// Frames is the most frames one client's outbox holds. Zero uses DefaultOutboxFrames; a negative value removes the cap.
	Frames int
	// Bytes is the most bytes of frames one client's outbox holds, counting each frame's payload and its 5-byte header. Zero uses DefaultOutboxBytes; a negative value removes the cap.
	Bytes int
}

// frameCap returns the resolved per-client frame cap, or 0 for none.
func (l OutboxLimits) frameCap() int { return resolveLimit(l.Frames, DefaultOutboxFrames) }

// byteCap returns the resolved per-client byte cap, or 0 for none.
func (l OutboxLimits) byteCap() int { return resolveLimit(l.Bytes, DefaultOutboxBytes) }

// OutboxStats reports what a server transport's outboxes hold and what their limits dropped.
type OutboxStats struct {
	// Frames is the number of frames queued across every connected client's outbox.
	Frames int
	// Bytes is the number of bytes those frames hold.
	Bytes int
	// UnreliableDropped counts the ChannelUnreliable frames dropped because their client's outbox was full.
	UnreliableDropped uint64
	// Overflows counts the sessions ended because a ChannelReliableOrdered frame did not fit its client's outbox.
	Overflows uint64
}

// OutboxReporter is implemented by the server transports that queue each client's outgoing frames: WebSocketServer and WebTransportServer. SetOutboxLimits changes the limits at any time; frames already queued stay, and the new limits apply to the frames Send queues afterwards. OutboxStats reads the outboxes' counters.
type OutboxReporter interface {
	SetOutboxLimits(OutboxLimits)
	OutboxStats() OutboxStats
}

// reasonOutboxFull is the PeerLeft reason for a client whose reliable frames no longer fit its outbox.
const reasonOutboxFull = "outbox full"
