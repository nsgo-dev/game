// Package net provides cross-target multiplayer primitives for NSGo games:
//   - Transport abstraction with deterministic in-memory loopback, pure-Go RFC 6455
//     WebSocket (native + browser WebAssembly), and native UDP transports.
//   - Server-authoritative state replication with bitmask delta compression,
//     snapshot interpolation, and client-side prediction/reconciliation.
//   - Deterministic lockstep and GGPO-style rollback netcode with periodic
//     checksum verification and desync detection.
//   - A Relay that forwards peer-to-peer sessions between RelayTransports over WebSocket or UDP for players who cannot reach each other directly.
package net

import (
	"encoding/binary"
	"errors"
)

// Channel selects delivery semantics for a packet.
type Channel uint8

const (
	// ChannelReliableOrdered guarantees delivery in send order. A transport that cannot provide that guarantee refuses it with ErrUnsupportedChannel rather than silently dropping packets; UDP has no retransmission, so lockstep and rollback need WebSocket, loopback or a RelayTransport, which retransmits end to end over either protocol.
	ChannelReliableOrdered Channel = 0
	// ChannelUnreliable delivers best-effort packets that may be dropped or reordered.
	ChannelUnreliable Channel = 1
)

// String returns a human-readable channel name.
func (c Channel) String() string {
	switch c {
	case ChannelReliableOrdered:
		return "reliable-ordered"
	case ChannelUnreliable:
		return "unreliable"
	default:
		return "unknown"
	}
}

// PeerID uniquely identifies a participant in a multiplayer session.
type PeerID uint32

// PeerBroadcast targets all connected remote peers when passed to Transport.Send.
const PeerBroadcast PeerID = 0

// Packet is a single datagram received from a remote peer.
type Packet struct {
	From    PeerID
	Channel Channel
	Payload []byte
	Seq     uint32
}

// Transport is the cross-target messaging interface consumed by replication
// and lockstep/rollback sessions.
//
// Recv is strictly non-blocking so games can drain incoming packets directly
// inside a fixed-step game.Loop Update callback without stalling a frame.
type Transport interface {
	Send(to PeerID, ch Channel, payload []byte) error
	Recv() (Packet, bool)
	LocalID() PeerID
	Peers() []PeerID
	Close() error
}

var (
	// ErrClosed is returned when operating on a closed transport.
	ErrClosed = errors.New("net: transport closed")
	// ErrUnknownPeer is returned when sending to a peer that is not connected.
	ErrUnknownPeer = errors.New("net: unknown peer")
	// ErrInvalidPacket is returned when a wire frame is malformed or truncated.
	ErrInvalidPacket = errors.New("net: invalid packet")
	// ErrPayloadTooLarge is returned when a payload exceeds transport limits.
	ErrPayloadTooLarge = errors.New("net: payload too large")
	// ErrUnsupportedChannel is returned when a transport cannot provide the
	// delivery guarantees the requested channel asks for.
	ErrUnsupportedChannel = errors.New("net: channel not supported by transport")
)

const wireHeaderSize = 5

// encodeWireFrame prepends the compact 5-byte transport header (channel + seq)
// to payload, allocating a fresh slice safe for asynchronous transmission.
func encodeWireFrame(ch Channel, seq uint32, payload []byte) []byte {
	frame := make([]byte, wireHeaderSize+len(payload))
	frame[0] = byte(ch)
	binary.BigEndian.PutUint32(frame[1:5], seq)
	copy(frame[wireHeaderSize:], payload)
	return frame
}

// decodeWireFrame parses the 5-byte transport header and returns a copy-free
// slice view of the payload.
func decodeWireFrame(frame []byte) (ch Channel, seq uint32, payload []byte, err error) {
	if len(frame) < wireHeaderSize {
		return 0, 0, nil, ErrInvalidPacket
	}
	ch = Channel(frame[0])
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return 0, 0, nil, ErrInvalidPacket
	}
	seq = binary.BigEndian.Uint32(frame[1:5])
	payload = frame[wireHeaderSize:]
	return ch, seq, payload, nil
}
