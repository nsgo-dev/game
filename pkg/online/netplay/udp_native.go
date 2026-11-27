//go:build !js

package netplay

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

type udpPeer struct {
	addr       stdnet.Addr
	keyed      bool
	sendSeq    uint32
	sendCipher udpCipher
	recvCipher udpCipher
	recv       replayWindow
}

// UDPTransport is an unreliable/sequenced native UDP Transport between peers that know each other's addresses in advance.
//
// Peers registered with AddPeer use an unauthenticated 9-byte cleartext header (from PeerID + channel + sequence number) suited only for trusted LAN use and tests: a datagram is accepted if its source address matches the address registered for the PeerID it claims, which does not protect against IP spoofing or on-path observers.
//
// Peers registered with AddPeerKey share a 32-byte pre-shared key and use the same ChaCha20-Poly1305 AEAD framing, 10-byte associated-data header, implicit nonce, 64-entry replay window and sequence-wrap refusal as UDPServer and UDPClient. A server whose clients are not known in advance uses ListenUDPServer and DialUDP, which run a Noise NK key exchange per session.
type UDPTransport struct {
	conn    stdnet.PacketConn
	localID PeerID

	mu     sync.Mutex
	peers  map[PeerID]*udpPeer
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
		peers:   make(map[PeerID]*udpPeer),
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

// AddPeer registers or updates a remote peer's UDP address without cryptography (unauthenticated, for LAN use and tests). Datagrams claiming to be from id are accepted only from this address. Use AddPeerKey for authenticated encryption between peers with a pre-shared key.
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
	if p := u.peers[id]; p != nil && !p.keyed {
		p.addr = resolved
		return nil
	}
	u.peers[id] = &udpPeer{addr: resolved}
	return nil
}

// AddPeerKey registers or updates a remote peer's UDP address with a 32-byte pre-shared ChaCha20-Poly1305 key. Datagrams exchanged with id use the same 26-byte AEAD framing, replay window, reflection protection and sequence-wrap refusal as UDPServer and UDPClient, and plaintext datagrams claiming to be from id are dropped. Calling AddPeerKey again resets the peer's sequence counter and replay window, allowing callers to rekey with a fresh key. An all-zero key returns ErrInvalidKey; id == 0 or id == LocalID returns ErrInvalidPacket.
func (u *UDPTransport) AddPeerKey(id PeerID, addr string, key [32]byte) error {
	if key == [32]byte{} {
		return ErrInvalidKey
	}
	if id == 0 || id == u.localID {
		return ErrInvalidPacket
	}
	resolved, err := stdnet.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	sendCipher := newUDPCipher(key)
	recvCipher := newUDPCipher(key)
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return ErrClosed
	}
	u.peers[id] = &udpPeer{
		addr:       resolved,
		keyed:      true,
		sendCipher: sendCipher,
		recvCipher: recvCipher,
	}
	return nil
}

// RemovePeer removes a remote peer from the routing table.
func (u *UDPTransport) RemovePeer(id PeerID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.peers, id)
	delete(u.seq, seqKey{to: id, ch: ChannelUnreliable})
}

func (u *UDPTransport) readLoop() {
	buf := make([]byte, 65535)
	plainBuf := make([]byte, 0, 65535)
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
		b := buf[:n]

		if n >= udpSealedHeader && b[0] == udpPeerData {
			hdr, ok := parseSealed(b)
			if ok {
				u.mu.Lock()
				p := u.peers[hdr.peer]
				if !u.closed && p != nil && p.keyed {
					if hdr.ch == ChannelUnreliable && sameUDPAddr(p.addr, remoteAddr) && p.recv.wouldAccept(hdr.seq) {
						if pt, valid := p.recvCipher.open(plainBuf, uint32(u.localID), b); valid {
							p.recv.accept(hdr.seq)
							u.inbox.push(Packet{
								From:    hdr.peer,
								Channel: hdr.ch,
								Payload: slices.Clone(pt),
								Seq:     hdr.seq,
							})
						}
					}
					u.mu.Unlock()
					continue
				}
				u.mu.Unlock()
			}
		}

		if n < udpHeaderSize {
			continue
		}
		from := PeerID(binary.BigEndian.Uint32(b[0:4]))
		ch, seq, payload, decErr := decodeWireFrame(b[4:n])
		if decErr != nil || ch != ChannelUnreliable {
			continue // UDPTransport never sends ChannelReliableOrdered, so a datagram claiming it is forged and would otherwise sit in the inbox's uncapped reliable FIFO (#546)
		}

		u.mu.Lock()
		// Only a registered unkeyed peer's own address may speak for it in plaintext: keyed peers reject plaintext, and the From header never registers or moves a peer.
		if p := u.peers[from]; p != nil && !p.keyed && !u.closed && sameUDPAddr(p.addr, remoteAddr) {
			u.inbox.push(Packet{
				From:    from,
				Channel: ch,
				Payload: slices.Clone(payload),
				Seq:     seq,
			})
		}
		u.mu.Unlock()
	}
}

// Send transmits payload in a single UDP datagram to the target peer (or all registered peers when to == PeerBroadcast). Keyed peers registered with AddPeerKey receive AEAD-sealed datagrams and return ErrSequenceExhausted once their 32-bit sequence counter would wrap.
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
		addr  stdnet.Addr
		dgram []byte
	}
	var ids []PeerID
	if to == PeerBroadcast {
		ids = make([]PeerID, 0, len(u.peers))
		for id := range u.peers {
			ids = append(ids, id)
		}
	} else {
		if u.peers[to] == nil {
			u.mu.Unlock()
			return ErrUnknownPeer
		}
		ids = []PeerID{to}
	}
	var firstErr error
	targets := make([]target, 0, len(ids))
	for _, id := range ids {
		p := u.peers[id]
		if p.keyed {
			seq, ok := nextSeq(&p.sendSeq)
			if !ok {
				if to != PeerBroadcast {
					u.mu.Unlock()
					return ErrSequenceExhausted
				}
				if firstErr == nil {
					firstErr = ErrSequenceExhausted
				}
				continue
			}
			dgram := p.sendCipher.seal(nil, uint32(id), udpPeerData, u.localID, seq, ch, payload)
			targets = append(targets, target{addr: p.addr, dgram: dgram})
			continue
		}
		sk := seqKey{to: id, ch: ch}
		u.seq[sk]++
		dgram := make([]byte, udpHeaderSize+len(payload))
		binary.BigEndian.PutUint32(dgram[0:4], uint32(u.localID))
		dgram[4] = byte(ch)
		binary.BigEndian.PutUint32(dgram[5:9], u.seq[sk])
		copy(dgram[udpHeaderSize:], payload)
		targets = append(targets, target{addr: p.addr, dgram: dgram})
	}
	u.mu.Unlock()

	for _, tgt := range targets {
		if _, err := u.conn.WriteTo(tgt.dgram, tgt.addr); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SetInboxLimits bounds the packets the transport keeps until Recv takes them (see InboxLimits).
func (u *UDPTransport) SetInboxLimits(l InboxLimits) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.inbox.setLimits(l)
}

// InboxStats reports what the transport's inbox holds and what its limits dropped or warned about.
func (u *UDPTransport) InboxStats() InboxStats {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.inbox.stats()
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
	u.err = nil
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
