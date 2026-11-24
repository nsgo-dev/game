//go:build !js

package net

import (
	"encoding/binary"
	"errors"
	"fmt"
	stdnet "net"
	"net/netip"
	"slices"
	"sync"
	"syscall"
)

const (
	udpHeaderSize     = 9 // from(4) + channel(1) + seq(4)
	maxUDPPayloadSize = 65000
)

// UDPTransport is an unreliable/sequenced native UDP Transport between peers that know each other's addresses in advance.
// Each datagram carries a 9-byte header (from PeerID + channel + sequence number)
// so peers can exchange packets directly without a TCP handshake.
//
// Both sides register each other with AddPeer, and a datagram is accepted only from the address registered for the PeerID it claims, so a spoofed From header can neither add a peer nor move one. A server whose clients are not known in advance uses ListenUDPServer and DialUDP, which authenticate each client and bind it to its address.
type UDPTransport struct {
	conn    stdnet.PacketConn
	localID PeerID

	mu     sync.Mutex
	peers  map[PeerID]stdnet.Addr
	seq    map[seqKey]uint32
	inbox  packetQueue
	closed bool
	err    error
}

// ListenUDP binds a UDP socket on addr for localID and starts receiving datagrams.
func ListenUDP(addr string, localID PeerID) (*UDPTransport, error) {
	conn, err := stdnet.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	return newUDPTransport(conn, localID), nil
}

func newUDPTransport(conn stdnet.PacketConn, localID PeerID) *UDPTransport {
	u := &UDPTransport{
		conn:    conn,
		localID: localID,
		peers:   make(map[PeerID]stdnet.Addr),
		seq:     make(map[seqKey]uint32),
	}
	go u.readLoop()
	return u
}

// udpReadRetry reports whether a read error on a UDP socket concerns one datagram rather than the socket, so reading goes on: a timeout, or an ICMP error such as port unreachable that the system reports for an earlier send.
func udpReadRetry(err error) bool {
	var ne stdnet.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET)
}

// udpReadError wraps a socket error that stopped a receive loop.
func udpReadError(err error) error {
	return fmt.Errorf("%w: %w", ErrReceiveFailed, err)
}

// Addr returns the local bound UDP address.
func (u *UDPTransport) Addr() stdnet.Addr {
	return u.conn.LocalAddr()
}

// AddPeer registers or updates a remote peer's UDP address. Datagrams claiming to be from id are accepted only from this address.
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
			if udpReadRetry(err) {
				continue
			}
			u.mu.Lock()
			if !u.closed {
				u.err = udpReadError(err)
			}
			u.mu.Unlock()
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
		// Only a registered peer's own address may speak for it: the From header is chosen by the sender, so it never registers or moves a peer.
		if addr, ok := u.peers[from]; ok && !u.closed && sameUDPAddr(addr, remoteAddr) {
			u.inbox.push(Packet{
				From:    from,
				Channel: ch,
				Payload: cp,
				Seq:     seq,
			})
		}
		u.mu.Unlock()
	}
}

// Send transmits payload in a single UDP datagram to the target peer (or all registered peers when to == PeerBroadcast).
//
// UDP has no acknowledgement or retransmission, so ChannelReliableOrdered is refused with ErrUnsupportedChannel instead of quietly losing packets. Use a WebSocket or loopback transport, or a RelayTransport, for lockstep and rollback sessions.
func (u *UDPTransport) Send(to PeerID, ch Channel, payload []byte) error {
	if ch == ChannelReliableOrdered {
		return ErrUnsupportedChannel
	}
	if ch != ChannelUnreliable {
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
	if u.err != nil {
		err := u.err
		u.mu.Unlock()
		return err
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
	return u.inbox.pop()
}

// Err returns why the transport stopped receiving: ErrReceiveFailed wrapping the socket's error. Send returns the same error from then on. It is nil while the transport receives, and after Close. Errors about a single datagram, such as an ICMP port unreachable for an earlier send, do not stop it.
func (u *UDPTransport) Err() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.err
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

// sameUDPAddr reports whether two UDP addresses name the same IP and port, treating an IPv4-mapped IPv6 address as its IPv4 form.
func sameUDPAddr(a, b stdnet.Addr) bool {
	ua, ok1 := a.(*stdnet.UDPAddr)
	ub, ok2 := b.(*stdnet.UDPAddr)
	if !ok1 || !ok2 {
		return a.String() == b.String()
	}
	pa, pb := ua.AddrPort(), ub.AddrPort()
	return netip.AddrPortFrom(pa.Addr().Unmap(), pa.Port()) == netip.AddrPortFrom(pb.Addr().Unmap(), pb.Port())
}
