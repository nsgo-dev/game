//go:build !js

package net

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	stdnet "net"
	"slices"
	"sync"
	"time"
)

// After the handshake every UDP datagram is sealed with the session's key:
//
//	type(1) peer(4) seq(4) channel(1) tag(8) payload
//
// tag is the first 8 bytes of HMAC-SHA256(key, header without tag || payload). type also fixes the direction, so a datagram cannot be reflected back to its sender.
const (
	udpData       = 0xB1 // client to server
	udpServerData = 0xB2 // server to client
	udpPing       = 0xB3 // client to server keepalive
	udpPong       = 0xB4 // server to client keepalive reply
	udpBye        = 0xB5 // client to server goodbye

	udpSealedHeader = 18
	udpTagSize      = 8

	udpHandshakeTimeout = 10 * time.Second
	udpHelloRetry       = 250 * time.Millisecond
	udpKeepAlive        = time.Second
	udpClientIdle       = 10 * time.Second
)

// sealUDP builds a sealed datagram.
func sealUDP(key *[hsKeySize]byte, typ byte, peer PeerID, seq uint32, ch Channel, payload []byte) []byte {
	b := make([]byte, udpSealedHeader+len(payload))
	b[0] = typ
	binary.BigEndian.PutUint32(b[1:5], uint32(peer))
	binary.BigEndian.PutUint32(b[5:9], seq)
	b[9] = byte(ch)
	copy(b[udpSealedHeader:], payload)
	copy(b[10:18], udpTag(key, b))
	return b
}

func udpTag(key *[hsKeySize]byte, b []byte) []byte {
	m := hmac.New(sha256.New, key[:])
	m.Write(b[:10])
	m.Write(b[udpSealedHeader:])
	return m.Sum(nil)[:udpTagSize]
}

// sealedHeader is a parsed, not yet verified, sealed datagram.
type sealedHeader struct {
	typ  byte
	peer PeerID
	seq  uint32
	ch   Channel
}

func parseSealed(b []byte) (sealedHeader, bool) {
	if len(b) < udpSealedHeader {
		return sealedHeader{}, false
	}
	return sealedHeader{
		typ:  b[0],
		peer: PeerID(binary.BigEndian.Uint32(b[1:5])),
		seq:  binary.BigEndian.Uint32(b[5:9]),
		ch:   Channel(b[9]),
	}, true
}

// openUDP reports whether b carries a valid tag for key.
func openUDP(key *[hsKeySize]byte, b []byte) bool {
	return len(b) >= udpSealedHeader && hmac.Equal(b[10:18], udpTag(key, b))
}

// replayWindow refuses a sequence number seen before or older than the last 64, so a captured datagram cannot be played again.
type replayWindow struct {
	top  uint32
	seen uint64
}

func (w *replayWindow) accept(seq uint32) bool {
	switch {
	case seq == 0:
		return false
	case seq > w.top:
		shift := seq - w.top
		if shift >= 64 {
			w.seen = 0
		} else {
			w.seen <<= shift
		}
		w.seen |= 1
		w.top = seq
		return true
	case w.top-seq >= 64:
		return false
	default:
		bit := uint64(1) << (w.top - seq)
		if w.seen&bit != 0 {
			return false
		}
		w.seen |= bit
		return true
	}
}

// udpLink binds a UDP session to the address that completed its handshake.
type udpLink struct {
	addr    stdnet.Addr
	nonce   [hsNonceSize]byte
	welcome []byte
	sendSeq uint32
	recv    replayWindow
}

// UDPServer is a native UDP Transport for a server whose clients connect with DialUDP. Each client runs the connection handshake before its packets reach Recv: Auth.Verify checks its token and the server assigns its ID. After that the client is bound to the address it shook hands from and to a per-session key, so a datagram from another address, or without the key, is dropped instead of reaching Recv or moving the peer.
//
// UDP has no connection, so a client that stays silent for Auth.IdleTimeout counts as dropped; DialUDP clients ping every second while idle. Datagrams are authenticated, not encrypted.
//
// The binding stops attackers who cannot see the traffic. The handshake itself travels in the clear: the hello carries the token, and the welcome carries the session key and resume secret. Anyone who can observe it, on shared Wi-Fi or any hop on the path, can forge datagrams for the session and resume it from their own address, taking it over. Issue UDP tokens that are short-lived and single-use, and use WebSocket over wss:// where an on-path observer is a concern.
type UDPServer struct {
	conn     stdnet.PacketConn
	serverID PeerID

	mu      sync.Mutex
	table   *sessionTable
	byNonce map[[hsNonceSize]byte]PeerID
	inbox   []Packet
	closed  bool
	changed chan struct{}
}

// ListenUDPServer binds a UDP socket on addr for a server with ID serverID (1 when zero) and starts accepting DialUDP clients under auth.
func ListenUDPServer(addr string, serverID PeerID, auth Auth) (*UDPServer, error) {
	if serverID == 0 {
		serverID = 1
	}
	conn, err := stdnet.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	s := &UDPServer{
		conn:     conn,
		serverID: serverID,
		table:    newSessionTable(auth, serverID, true),
		byNonce:  make(map[[hsNonceSize]byte]PeerID),
		changed:  make(chan struct{}, 1),
	}
	go s.readLoop()
	return s, nil
}

// Addr returns the local bound UDP address.
func (s *UDPServer) Addr() stdnet.Addr {
	return s.conn.LocalAddr()
}

func (s *UDPServer) readLoop() {
	buf := make([]byte, 65535)
	for {
		n, from, err := s.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		b := buf[:n]
		switch b[0] {
		case hsHello:
			s.handleHello(b, from)
		case udpData, udpPing, udpBye:
			s.handleSealed(b, from)
		}
	}
}

func (s *UDPServer) handleHello(b []byte, from stdnet.Addr) {
	if len(b) < udpMinHello {
		return
	}
	h, err := decodeHello(b)
	if err != nil {
		if errors.Is(err, errHandshakeVersion) {
			_, _ = s.conn.WriteTo(encodeReject(h.nonce, hsReasonVersion, len(b)), from)
		}
		return
	}
	if reply := s.admit(h, from, len(b)); reply != nil {
		_, _ = s.conn.WriteTo(reply, from)
	}
}

// admit runs the session side of the handshake and returns the reply.
func (s *UDPServer) admit(h hello, from stdnet.Addr, limit int) []byte {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	now := s.table.now()
	s.table.expire(now)

	// A retransmitted hello whose welcome was lost gets the same welcome again rather than a second session.
	if id, ok := s.byNonce[h.nonce]; ok {
		if sess := s.table.sessions[id]; sess != nil {
			if link, ok := sess.link.(*udpLink); ok && link.nonce == h.nonce && sameUDPAddr(link.addr, from) {
				s.mu.Unlock()
				return link.welcome
			}
		}
		delete(s.byNonce, h.nonce)
	}

	if sess := s.table.resume(h, now); sess != nil {
		reply := s.bindLocked(sess, h.nonce, from, true)
		s.mu.Unlock()
		s.notify()
		return reply
	}
	id, ok := s.table.reserve()
	s.mu.Unlock()
	if !ok {
		return encodeReject(h.nonce, reasonServerFull, limit)
	}

	reason, ok := s.table.verify(id, h.token)

	s.mu.Lock()
	if !ok || s.closed {
		s.table.release(id)
		s.mu.Unlock()
		if !ok {
			return encodeReject(h.nonce, reason, limit)
		}
		return nil
	}
	sess := s.table.commit(id, s.table.now())
	reply := s.bindLocked(sess, h.nonce, from, false)
	s.mu.Unlock()
	s.notify()
	return reply
}

// bindLocked binds sess to from with a fresh replay window and records its welcome for retransmits.
func (s *UDPServer) bindLocked(sess *session, nonce [hsNonceSize]byte, from stdnet.Addr, resumed bool) []byte {
	if old, ok := sess.link.(*udpLink); ok {
		delete(s.byNonce, old.nonce)
	}
	reply := encodeWelcome(s.table.welcomeFor(sess, nonce, resumed))
	sess.link = &udpLink{addr: from, nonce: nonce, welcome: reply}
	s.byNonce[nonce] = sess.id
	s.pruneNoncesLocked()
	return reply
}

// pruneNoncesLocked forgets hello nonces of sessions that have gone or moved on, once they outnumber the sessions.
func (s *UDPServer) pruneNoncesLocked() {
	if len(s.byNonce) <= 2*len(s.table.sessions)+64 {
		return
	}
	for nonce, id := range s.byNonce {
		sess := s.table.sessions[id]
		if link, ok := linkOf(sess); !ok || link.nonce != nonce {
			delete(s.byNonce, nonce)
		}
	}
}

func linkOf(sess *session) (*udpLink, bool) {
	if sess == nil {
		return nil, false
	}
	link, ok := sess.link.(*udpLink)
	return link, ok
}

func (s *UDPServer) handleSealed(b []byte, from stdnet.Addr) {
	hdr, ok := parseSealed(b)
	if !ok {
		return
	}
	s.mu.Lock()
	sess := s.table.sessions[hdr.peer]
	link, linked := linkOf(sess)
	if s.closed || !linked || !sess.connected || !sameUDPAddr(link.addr, from) || !openUDP(&sess.key, b) || !link.recv.accept(hdr.seq) {
		s.mu.Unlock()
		return
	}
	sess.lastSeen = s.table.now()
	var reply []byte
	switch hdr.typ {
	case udpData:
		if hdr.ch == ChannelUnreliable {
			s.inbox = append(s.inbox, Packet{
				From:    hdr.peer,
				Channel: hdr.ch,
				Payload: slices.Clone(b[udpSealedHeader:]),
				Seq:     hdr.seq,
			})
		}
	case udpPing:
		link.sendSeq++
		reply = sealUDP(&sess.key, udpPong, sess.id, link.sendSeq, ChannelUnreliable, nil)
	case udpBye:
		delete(s.byNonce, link.nonce)
		s.table.leave(sess)
	}
	s.mu.Unlock()
	s.notify()
	if reply != nil {
		_, _ = s.conn.WriteTo(reply, from)
	}
}

// notify wakes a waiter on changed without blocking.
func (s *UDPServer) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Send seals payload with the client's session key and sends it in one datagram to the client (or all connected clients when to == PeerBroadcast). It refuses ChannelReliableOrdered with ErrUnsupportedChannel, returns ErrDisconnected for a client inside its grace period and ErrUnknownPeer for an ID with no session.
func (s *UDPServer) Send(to PeerID, ch Channel, payload []byte) error {
	if ch == ChannelReliableOrdered {
		return ErrUnsupportedChannel
	}
	if ch != ChannelUnreliable {
		return ErrInvalidPacket
	}
	if len(payload) > maxUDPPayloadSize {
		return ErrPayloadTooLarge
	}
	type target struct {
		addr  stdnet.Addr
		dgram []byte
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	var ids []PeerID
	if to == PeerBroadcast {
		ids = s.table.connectedIDs()
	} else {
		if err := s.table.sendable(to); err != nil {
			s.mu.Unlock()
			return err
		}
		ids = []PeerID{to}
	}
	targets := make([]target, 0, len(ids))
	for _, id := range ids {
		sess := s.table.sessions[id]
		link := sess.link.(*udpLink)
		link.sendSeq++
		targets = append(targets, target{addr: link.addr, dgram: sealUDP(&sess.key, udpServerData, id, link.sendSeq, ch, payload)})
	}
	s.mu.Unlock()

	var firstErr error
	for _, t := range targets {
		if _, err := s.conn.WriteTo(t.dgram, t.addr); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Recv pops the next authenticated packet without blocking.
func (s *UDPServer) Recv() (Packet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inbox) == 0 {
		return Packet{}, false
	}
	pkt := s.inbox[0]
	copy(s.inbox, s.inbox[1:])
	s.inbox = s.inbox[:len(s.inbox)-1]
	return pkt, true
}

// NextEvent pops the next join, drop, resume or leave without blocking, after dropping clients silent for Auth.IdleTimeout and ending sessions whose grace period has passed.
func (s *UDPServer) NextEvent() (PeerEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.table.expire(s.table.now())
	return s.table.popEvent()
}

// LocalID returns the server's PeerID.
func (s *UDPServer) LocalID() PeerID {
	return s.serverID
}

// Peers returns the sorted IDs of connected clients. Clients inside their grace period are left out.
func (s *UDPServer) Peers() []PeerID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.table.expire(s.table.now())
	return s.table.connectedIDs()
}

// Close closes the UDP socket.
func (s *UDPServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	return s.conn.Close()
}

// UDPClient is a native UDP Transport connected to a UDPServer. DialUDP runs the connection handshake, after which every datagram is sealed with the session's key. The client pings the server while idle and reports itself disconnected when the server stays silent for 10 seconds; Reconnect resumes the session with the same ID within the server's grace period.
type UDPClient struct {
	addr  string
	token []byte

	dialMu sync.Mutex // serializes Reconnect

	mu       sync.Mutex
	conn     stdnet.Conn
	stop     chan struct{}
	localID  PeerID
	serverID PeerID
	key      [hsKeySize]byte
	sendSeq  uint32
	recv     replayWindow
	lastSend time.Time
	lastRecv time.Time
	state    ConnState
	err      error
	resume   resumeState
	resumed  bool
	inbox    []Packet
	changed  chan struct{}

	// admissions counts handshakes that admitted the client as a new peer rather than resuming, so a RelayTransport can tell its relay session was lost even when the new ID equals the old one.
	admissions uint64
}

// DialUDP connects to the UDPServer at addr, presents token in the connection handshake and returns once the server accepts it. The server assigns the client's ID, which LocalID reports. A refusal returns a *RejectError that matches ErrRejected.
func DialUDP(addr string, token []byte) (*UDPClient, error) {
	if len(token) > MaxTokenSize {
		return nil, ErrTokenTooLarge
	}
	c := &UDPClient{addr: addr, token: slices.Clone(token), changed: make(chan struct{}, 1)}
	if err := c.connect(); err != nil {
		return nil, err
	}
	return c, nil
}

// Reconnect drops the current socket without a goodbye, opens a new one and resumes the session: within the server's grace period the client keeps its ID, and Resumed reports true. Past it, the server admits the client as a new peer when its token is still valid, and LocalID changes.
func (c *UDPClient) Reconnect() error {
	c.dialMu.Lock()
	defer c.dialMu.Unlock()
	c.mu.Lock()
	if c.state == ConnClosed {
		c.mu.Unlock()
		return ErrClosed
	}
	old := c.detachLocked()
	c.state = ConnConnecting
	c.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return c.connect()
}

// detachLocked forgets the current socket and stops its keepalive, returning the socket for the caller to close.
func (c *UDPClient) detachLocked() stdnet.Conn {
	old := c.conn
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
	c.conn = nil
	return old
}

func (c *UDPClient) connect() error {
	c.mu.Lock()
	if c.state == ConnClosed {
		c.mu.Unlock()
		return ErrClosed
	}
	res := c.resume
	c.mu.Unlock()

	conn, err := stdnet.Dial("udp", c.addr)
	if err != nil {
		return c.fail(err)
	}
	w, err := udpHandshake(conn, c.token, res)
	if err != nil {
		_ = conn.Close()
		return c.fail(err)
	}
	_ = conn.SetReadDeadline(time.Time{})

	c.mu.Lock()
	if c.state == ConnClosed {
		c.mu.Unlock()
		_ = conn.Close()
		return ErrClosed
	}
	now := time.Now()
	stop := make(chan struct{})
	c.conn, c.stop = conn, stop
	c.localID, c.serverID, c.key = w.peer, w.server, w.key
	c.sendSeq, c.recv = 0, replayWindow{}
	c.lastSend, c.lastRecv = now, now
	c.resume = resumeState{peer: w.peer, secret: w.secret}
	c.resumed = w.resumed
	if !w.resumed {
		c.admissions++
	}
	c.state, c.err = ConnOpen, nil
	c.mu.Unlock()
	go c.readLoop(conn)
	go c.keepAlive(conn, stop)
	return nil
}

// udpHandshake sends the hello, resending it until the server answers or the handshake times out.
func udpHandshake(conn stdnet.Conn, token []byte, res resumeState) (welcome, error) {
	h, err := newHello(token, res)
	if err != nil {
		return welcome{}, err
	}
	dgram := encodeHello(h, udpMinHello)
	deadline := time.Now().Add(udpHandshakeTimeout)
	buf := make([]byte, 2048)
	retry := udpHelloRetry
	for {
		if _, err := conn.Write(dgram); err != nil {
			return welcome{}, err
		}
		wait := time.Now().Add(retry)
		if wait.After(deadline) {
			wait = deadline
		}
		_ = conn.SetReadDeadline(wait)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				var ne stdnet.Error
				if errors.As(err, &ne) && ne.Timeout() {
					break
				}
				return welcome{}, err
			}
			if n == 0 || (buf[0] != hsWelcome && buf[0] != hsReject) {
				continue
			}
			w, err := decodeServerHandshake(buf[:n])
			if w.nonce != h.nonce || errors.Is(err, ErrInvalidPacket) {
				continue // a late reply to an earlier dial, or noise
			}
			return w, err
		}
		if !time.Now().Before(deadline) {
			return welcome{}, fmt.Errorf("net: udp handshake with %s timed out", conn.RemoteAddr())
		}
		retry = min(2*retry, time.Second)
	}
}

func (c *UDPClient) fail(err error) error {
	c.mu.Lock()
	if c.state != ConnClosed {
		c.state, c.err = ConnDisconnected, err
	}
	c.mu.Unlock()
	return err
}

func (c *UDPClient) readLoop(conn stdnet.Conn) {
	buf := make([]byte, 65535)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			c.lost(conn, fmt.Errorf("%w: %w", ErrDisconnected, err))
			return
		}
		b := buf[:n]
		hdr, ok := parseSealed(b)
		if !ok || (hdr.typ != udpServerData && hdr.typ != udpPong) {
			continue
		}
		c.mu.Lock()
		if c.conn != conn || c.state != ConnOpen || hdr.peer != c.localID || !openUDP(&c.key, b) || !c.recv.accept(hdr.seq) {
			c.mu.Unlock()
			continue
		}
		c.lastRecv = time.Now()
		if hdr.typ == udpServerData && hdr.ch == ChannelUnreliable {
			c.inbox = append(c.inbox, Packet{
				From:    c.serverID,
				Channel: hdr.ch,
				Payload: slices.Clone(b[udpSealedHeader:]),
				Seq:     hdr.seq,
			})
		}
		c.mu.Unlock()
		c.notify()
	}
}

// lost marks the client disconnected if conn is still its socket.
func (c *UDPClient) lost(conn stdnet.Conn, cause error) {
	c.mu.Lock()
	current := c.conn == conn
	if current {
		c.detachLocked()
		if c.state == ConnOpen {
			c.state, c.err = ConnDisconnected, cause
		}
	}
	c.mu.Unlock()
	if current {
		_ = conn.Close()
	}
	c.notify()
}

// keepAlive pings the server while the client is idle and gives up on a server that stays silent.
func (c *UDPClient) keepAlive(conn stdnet.Conn, stop chan struct{}) {
	ticker := time.NewTicker(udpKeepAlive)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			c.mu.Lock()
			if c.conn != conn {
				c.mu.Unlock()
				return
			}
			if now.Sub(c.lastRecv) >= udpClientIdle {
				c.mu.Unlock()
				c.lost(conn, fmt.Errorf("%w: server silent for %v", ErrDisconnected, udpClientIdle))
				return
			}
			var ping []byte
			if now.Sub(c.lastSend) >= udpKeepAlive {
				c.sendSeq++
				c.lastSend = now
				ping = sealUDP(&c.key, udpPing, c.localID, c.sendSeq, ChannelUnreliable, nil)
			}
			c.mu.Unlock()
			if ping != nil {
				_, _ = conn.Write(ping)
			}
		}
	}
}

// notify wakes a waiter on changed without blocking.
func (c *UDPClient) notify() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

// Send seals payload with the session key and sends it to the server in one datagram. It refuses ChannelReliableOrdered with ErrUnsupportedChannel and returns ErrDisconnected while the connection is down.
func (c *UDPClient) Send(to PeerID, ch Channel, payload []byte) error {
	if ch == ChannelReliableOrdered {
		return ErrUnsupportedChannel
	}
	if ch != ChannelUnreliable {
		return ErrInvalidPacket
	}
	if len(payload) > maxUDPPayloadSize {
		return ErrPayloadTooLarge
	}
	c.mu.Lock()
	switch {
	case c.state == ConnClosed:
		c.mu.Unlock()
		return ErrClosed
	case c.state != ConnOpen || c.conn == nil:
		c.mu.Unlock()
		return ErrDisconnected
	case to != PeerBroadcast && to != c.serverID:
		c.mu.Unlock()
		return ErrUnknownPeer
	}
	c.sendSeq++
	c.lastSend = time.Now()
	dgram := sealUDP(&c.key, udpData, c.localID, c.sendSeq, ch, payload)
	conn := c.conn
	c.mu.Unlock()
	_, err := conn.Write(dgram)
	return err
}

// Recv pops the next authenticated packet from the server without blocking.
func (c *UDPClient) Recv() (Packet, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.inbox) == 0 {
		return Packet{}, false
	}
	pkt := c.inbox[0]
	copy(c.inbox, c.inbox[1:])
	c.inbox = c.inbox[:len(c.inbox)-1]
	return pkt, true
}

// LocalID returns the PeerID the server assigned this client.
func (c *UDPClient) LocalID() PeerID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.localID
}

// Peers returns the server's PeerID, or nil while disconnected or closed.
func (c *UDPClient) Peers() []PeerID {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != ConnOpen {
		return nil
	}
	return []PeerID{c.serverID}
}

// State returns the connection state.
func (c *UDPClient) State() ConnState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Err returns why the client is disconnected: a *RejectError, a handshake timeout, or ErrDisconnected wrapping the cause. It is nil while the connection is open.
func (c *UDPClient) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Resumed reports whether the last handshake resumed an existing session, keeping the client's ID.
func (c *UDPClient) Resumed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resumed
}

// relayAdmissions counts the handshakes that admitted the client as a new peer.
func (c *UDPClient) relayAdmissions() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.admissions
}

// Close sends the server a goodbye, so it reports PeerLeft at once rather than holding the session for a reconnect, and closes the socket.
func (c *UDPClient) Close() error {
	c.mu.Lock()
	if c.state == ConnClosed {
		c.mu.Unlock()
		return nil
	}
	open := c.state == ConnOpen
	c.state = ConnClosed
	var bye []byte
	if open && c.conn != nil {
		c.sendSeq++
		bye = sealUDP(&c.key, udpBye, c.localID, c.sendSeq, ChannelUnreliable, nil)
	}
	conn := c.detachLocked()
	c.mu.Unlock()
	if conn == nil {
		return nil
	}
	if bye != nil {
		_, _ = conn.Write(bye)
	}
	return conn.Close()
}

// relayWake lets a Relay serving this server wait for packets and events instead of polling.
func (s *UDPServer) relayWake() <-chan struct{} {
	return s.changed
}
