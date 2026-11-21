//go:build !js

package net

import (
	"encoding/binary"
	stdnet "net"
	"slices"
	"sync"
)

const (
	udpHeaderSize     = 9 // from(4) + channel(1) + seq(4)
	maxUDPPayloadSize = 65000
)

// UDPTransport is an unreliable/sequenced native UDP Transport.
// Each datagram carries a 9-byte header (from PeerID + channel + sequence number)
// so peers can exchange packets directly without a TCP handshake.
type UDPTransport struct {
	conn    stdnet.PacketConn
	localID PeerID

	mu     sync.Mutex
	peers  map[PeerID]stdnet.Addr
	seq    map[seqKey]uint32
	inbox  []Packet
	closed bool
}

// ListenUDP binds a UDP socket on addr for localID and starts receiving datagrams.
func ListenUDP(addr string, localID PeerID) (*UDPTransport, error) {
	conn, err := stdnet.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	u := &UDPTransport{
		conn:    conn,
		localID: localID,
		peers:   make(map[PeerID]stdnet.Addr),
		seq:     make(map[seqKey]uint32),
	}
	go u.readLoop()
	return u, nil
}

// Addr returns the local bound UDP address.
func (u *UDPTransport) Addr() stdnet.Addr {
	return u.conn.LocalAddr()
}

// AddPeer registers or updates a remote peer's UDP address.
func (u *UDPTransport) AddPeer(id PeerID, addr string) error {
	resolved, err := stdnet.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return ErrClosed
	}
	u.peers[id] = resolved
	return nil
}

// RemovePeer removes a remote peer from the routing table.
func (u *UDPTransport) RemovePeer(id PeerID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.peers, id)
}

func (u *UDPTransport) readLoop() {
	buf := make([]byte, 65535)
	for {
		n, remoteAddr, err := u.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if n < udpHeaderSize {
			continue
		}
		from := PeerID(binary.BigEndian.Uint32(buf[0:4]))
		ch, seq, payload, decErr := decodeWireFrame(buf[4:n])
		if decErr != nil {
			continue
		}
		cp := make([]byte, len(payload))
		copy(cp, payload)

		u.mu.Lock()
		if !u.closed {
			if from != 0 && from != u.localID {
				u.peers[from] = remoteAddr
			}
			u.inbox = append(u.inbox, Packet{
				From:    from,
				Channel: ch,
				Payload: cp,
				Seq:     seq,
			})
		}
		u.mu.Unlock()
	}
}

// Send transmits payload in a single UDP datagram to the target peer
// (or all registered peers when to == PeerBroadcast).
func (u *UDPTransport) Send(to PeerID, ch Channel, payload []byte) error {
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return ErrInvalidPacket
	}
	if len(payload) > maxUDPPayloadSize {
		return ErrPayloadTooLarge
	}

	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return ErrClosed
	}
	type target struct {
		id   PeerID
		addr stdnet.Addr
		seq  uint32
	}
	var targets []target
	if to == PeerBroadcast {
		targets = make([]target, 0, len(u.peers))
		for id, addr := range u.peers {
			sk := seqKey{to: id, ch: ch}
			u.seq[sk]++
			targets = append(targets, target{id: id, addr: addr, seq: u.seq[sk]})
		}
	} else {
		addr, ok := u.peers[to]
		if !ok {
			u.mu.Unlock()
			return ErrUnknownPeer
		}
		sk := seqKey{to: to, ch: ch}
		u.seq[sk]++
		targets = []target{{id: to, addr: addr, seq: u.seq[sk]}}
	}
	u.mu.Unlock()

	var firstErr error
	for _, tgt := range targets {
		dgram := make([]byte, udpHeaderSize+len(payload))
		binary.BigEndian.PutUint32(dgram[0:4], uint32(u.localID))
		dgram[4] = byte(ch)
		binary.BigEndian.PutUint32(dgram[5:9], tgt.seq)
		copy(dgram[udpHeaderSize:], payload)
		if _, err := u.conn.WriteTo(dgram, tgt.addr); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Recv pops the next received UDP packet without blocking.
func (u *UDPTransport) Recv() (Packet, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.inbox) == 0 {
		return Packet{}, false
	}
	pkt := u.inbox[0]
	copy(u.inbox, u.inbox[1:])
	u.inbox = u.inbox[:len(u.inbox)-1]
	return pkt, true
}

// LocalID returns this endpoint's PeerID.
func (u *UDPTransport) LocalID() PeerID {
	return u.localID
}

// Peers returns the sorted list of known remote PeerIDs.
func (u *UDPTransport) Peers() []PeerID {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return nil
	}
	out := make([]PeerID, 0, len(u.peers))
	for id := range u.peers {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Close closes the underlying UDP socket.
func (u *UDPTransport) Close() error {
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return nil
	}
	u.closed = true
	u.mu.Unlock()
	return u.conn.Close()
}
